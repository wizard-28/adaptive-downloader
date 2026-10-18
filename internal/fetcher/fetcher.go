// Package fetcher downloads exactly one HTTP byte range and returns the
// received bytes together with timing and throughput metadata.
//
// A Fetcher has no knowledge of RangeTracker, assembly, policy, or retry
// logic: it performs a single request and never writes to disk. Callers are
// responsible for deciding whether to retry a failed range.
package fetcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout is used when Config.Timeout is not positive and no explicit
// no-request-timeout policy is configured.
const DefaultTimeout = 30 * time.Second

// defaultDialTimeout is used when Config.DialTimeout is not positive and no
// explicit no-dial-timeout policy is configured.
const defaultDialTimeout = 30 * time.Second

// maxIdleConnsPerHost bounds the dedicated transport's idle connection pool.
// It matches the project's intended maximum concurrency (Cmax = 16) so the
// transport can retain one idle persistent HTTP/1.1 connection per possible
// flow instead of churning connections once C exceeds the stdlib default of 2.
const maxIdleConnsPerHost = 16

var (
	// ErrInvalidRange indicates a malformed request range (negative start or
	// end before start).
	ErrInvalidRange = errors.New("fetcher: invalid byte range")
	// ErrRangeNotSupported indicates the server ignored the Range header and
	// responded with 200 OK instead of 206 Partial Content.
	ErrRangeNotSupported = errors.New("fetcher: server does not support range requests")
	// ErrUnexpectedStatus indicates an HTTP status other than 206 or 200.
	ErrUnexpectedStatus = errors.New("fetcher: unexpected HTTP status")
	// ErrInvalidContentRange indicates a malformed Content-Range header or one
	// that does not match the requested range.
	ErrInvalidContentRange = errors.New("fetcher: invalid content-range header")
	// ErrIncompleteResponse indicates the server returned fewer bytes than the
	// requested range.
	ErrIncompleteResponse = errors.New("fetcher: incomplete response")
	// ErrUnexpectedLength indicates the server returned more bytes than the
	// requested range.
	ErrUnexpectedLength = errors.New("fetcher: unexpected response length")
	// ErrTimeout indicates the request exceeded its deadline or was cancelled
	// by a timeout.
	ErrTimeout = errors.New("fetcher: request timed out")
)

// ByteRange is an inclusive HTTP byte range [Start, End], matching the
// semantics of the "Range: bytes=Start-End" header.
type ByteRange struct {
	Start int64
	End   int64
}

// Length returns the number of bytes covered by the range.
func (r ByteRange) Length() int64 {
	if r.End < r.Start {
		return 0
	}
	return r.End - r.Start + 1
}

// String renders the range in HTTP header form, e.g. "10-19".
func (r ByteRange) String() string {
	return fmt.Sprintf("%d-%d", r.Start, r.End)
}

// Config configures a Fetcher.
type Config struct {
	// Timeout bounds the entire request. Non-positive values use
	// DefaultTimeout. NoRequestTimeout explicitly disables the deadline
	// instead; the two settings are mutually exclusive (see Validate).
	Timeout time.Duration
	// NoRequestTimeout explicitly disables the whole-request deadline, so a
	// request is bounded only by its context (the replay harness watchdog or
	// trace-EOF cancellation). It must not be combined with Timeout > 0. The
	// zero value (false) keeps the Timeout/DefaultTimeout behavior.
	NoRequestTimeout bool
	// DialTimeout bounds establishing a new connection. Non-positive values
	// use defaultDialTimeout. NoDialTimeout explicitly disables the dial
	// deadline instead; the two settings are mutually exclusive (see
	// Validate). Dials remain cancelable through the request context.
	DialTimeout time.Duration
	// NoDialTimeout explicitly disables the connection-establishment
	// deadline. It must not be combined with DialTimeout > 0.
	NoDialTimeout bool
	// Client optionally supplies a custom HTTP client. When set, it is used
	// as-is and the timeout fields above are ignored (they cannot be applied
	// to a caller-owned client).
	Client *http.Client
	// ByteObserver optionally receives an incremental event for every
	// response-body read (timestamped at the read, with request and
	// connection identity). Nil disables byte instrumentation and leaves the
	// fetch path unchanged.
	ByteObserver ByteObserver
	// UserAgent optionally sets the User-Agent request header. Empty keeps
	// the Go default ("Go-http-client/1.1"), so existing behavior is
	// unchanged. Some public mirrors reject the Go default with 403.
	UserAgent string
}

// Validate rejects ambiguous timeout configurations so "no deadline" can
// never be confused with "default deadline".
func (c Config) Validate() error {
	switch {
	case c.Timeout < 0:
		return fmt.Errorf("fetcher: timeout must be >= 0")
	case c.NoRequestTimeout && c.Timeout != 0:
		return fmt.Errorf("fetcher: no-request-timeout must not be combined with a positive timeout")
	case c.DialTimeout < 0:
		return fmt.Errorf("fetcher: dial timeout must be >= 0")
	case c.NoDialTimeout && c.DialTimeout != 0:
		return fmt.Errorf("fetcher: no-dial-timeout must not be combined with a positive dial timeout")
	case c.Client != nil && (c.NoRequestTimeout || c.NoDialTimeout):
		return fmt.Errorf("fetcher: no-timeout settings require the internal client (Client must be nil)")
	}
	return nil
}

// Fetcher performs single HTTP range requests.
type Fetcher struct {
	client    *http.Client
	observer  ByteObserver
	userAgent string
}

// New returns a Fetcher configured by cfg.
//
// When cfg.Client is nil, the Fetcher gets a dedicated HTTP/1.1-only
// transport (see newHTTP1Transport); http.DefaultTransport is never used or
// mutated. When cfg.Client is set, it is used as-is.
//
// A NoRequestTimeout config sets http.Client.Timeout to 0 (no deadline), and
// a NoDialTimeout config builds the transport with a zero dialer deadline;
// requests remain cancelable through their context in both cases.
func New(cfg Config) *Fetcher {
	client := cfg.Client
	if client == nil {
		timeout := cfg.Timeout
		if cfg.NoRequestTimeout {
			timeout = 0 // http.Client.Timeout == 0 means no deadline
		} else if timeout <= 0 {
			timeout = DefaultTimeout
		}
		dialTimeout := cfg.DialTimeout
		if cfg.NoDialTimeout {
			dialTimeout = 0 // net.Dialer.Timeout == 0 means no deadline
		} else if dialTimeout <= 0 {
			dialTimeout = defaultDialTimeout
		}
		client = &http.Client{
			Transport: newHTTP1Transport(dialTimeout),
			Timeout:   timeout,
		}
	}
	return &Fetcher{client: client, observer: cfg.ByteObserver, userAgent: cfg.UserAgent}
}

// newHTTP1Transport returns a dedicated transport that can only speak
// HTTP/1.1 and can retain up to maxIdleConnsPerHost idle persistent
// connections per host.
//
// HTTP/1.1-only enforcement uses Go 1.24+ http.Transport.Protocols rather than
// ForceAttemptHTTP2: a non-nil Protocols set is authoritative, so HTTP/2 is
// also excluded from TLS ALPN negotiation and HTTPS cannot upgrade to h2.
// The remaining fields mirror the net/http defaults so behavior (proxies,
// timeouts, keep-alive) is unchanged.
func newHTTP1Transport(dialTimeout time.Duration) *http.Transport {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(false)
	protocols.SetUnencryptedHTTP2(false)

	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           newDialer(dialTimeout).DialContext,
		Protocols:             protocols,
		MaxIdleConns:          maxIdleConnsPerHost,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// newDialer builds the transport dialer. A zero timeout means "no dial
// deadline"; the dial still aborts when the request context is canceled.
func newDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}
}

// Result describes the outcome of a single range request. Data holds the
// bytes read from the response body and may be partial when Err is set.
type Result struct {
	RequestID  string
	URL        string
	Range      ByteRange
	StatusCode int
	Data       []byte
	Bytes      int64
	StartTime  time.Time
	EndTime    time.Time
	Duration   time.Duration
	Throughput float64 // bytes per second
	Success    bool
	Err        error
}

// Fetch requests the inclusive byte range [start, end] from rawURL.
//
// It returns a Result whose Success field reports the outcome; a non-nil Err
// accompanies every failure. Fetch never retries and never writes to disk.
func (f *Fetcher) Fetch(ctx context.Context, requestID, rawURL string, start, end int64) (res Result) {
	res = Result{
		RequestID: requestID,
		URL:       rawURL,
		Range:     ByteRange{Start: start, End: end},
		StartTime: time.Now(),
	}
	defer func() {
		res.EndTime = time.Now()
		res.Duration = res.EndTime.Sub(res.StartTime)
		res.Bytes = int64(len(res.Data))
		if res.Duration > 0 {
			res.Throughput = float64(res.Bytes) / res.Duration.Seconds()
		}
	}()

	if start < 0 || end < start {
		res.Err = fmt.Errorf("%w: start=%d end=%d", ErrInvalidRange, start, end)
		return res
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		res.Err = fmt.Errorf("fetcher: build request: %w", err)
		return res
	}
	req.Header.Set("Range", "bytes="+res.Range.String())
	if f.userAgent != "" {
		req.Header.Set("User-Agent", f.userAgent)
	}

	// When byte instrumentation is enabled, capture the actual connection
	// identity from httptrace. WithClientTrace composes with any trace already
	// in ctx, so caller-supplied traces keep working.
	var connID connIDHolder
	if f.observer != nil {
		trace := &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) {
				if info.Conn != nil {
					connID.set(info.Conn.LocalAddr().String())
				}
			},
		}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	}

	resp, err := f.client.Do(req)
	if err != nil {
		res.Err = requestError(err)
		return res
	}
	defer resp.Body.Close()

	res.StatusCode = resp.StatusCode

	expected := res.Range.Length()
	var reader io.Reader = resp.Body
	if f.observer != nil {
		reader = &countingReader{
			r:         resp.Body,
			requestID: res.RequestID,
			conn:      &connID,
			observer:  f.observer,
		}
	}
	body, err := io.ReadAll(io.LimitReader(reader, expected+1))
	res.Data = body
	if err != nil {
		res.Err = readError(err)
		return res
	}

	switch resp.StatusCode {
	case http.StatusPartialContent:
		// Expected for a satisfied range request.
	case http.StatusOK:
		res.Err = fmt.Errorf("%w: got 200 OK", ErrRangeNotSupported)
		return res
	default:
		res.Err = fmt.Errorf("%w: got %d %s", ErrUnexpectedStatus, resp.StatusCode, http.StatusText(resp.StatusCode))
		return res
	}

	if header := resp.Header.Get("Content-Range"); header != "" {
		crStart, crEnd, err := parseContentRange(header)
		if err != nil {
			res.Err = fmt.Errorf("%w: %q: %w", ErrInvalidContentRange, header, err)
			return res
		}
		if crStart != start || crEnd != end {
			res.Err = fmt.Errorf("%w: %q does not match requested bytes=%s", ErrInvalidContentRange, header, res.Range)
			return res
		}
	}

	switch got := int64(len(res.Data)); {
	case got < expected:
		res.Err = fmt.Errorf("%w: got %d bytes, want %d", ErrIncompleteResponse, got, expected)
		return res
	case got > expected:
		res.Err = fmt.Errorf("%w: got %d bytes, want %d", ErrUnexpectedLength, got, expected)
		return res
	}

	res.Success = true
	return res
}

func requestError(err error) error {
	if isTimeout(err) {
		return fmt.Errorf("%w: %w", ErrTimeout, err)
	}
	return fmt.Errorf("fetcher: request failed: %w", err)
}

func readError(err error) error {
	if isTimeout(err) {
		return fmt.Errorf("%w: %w", ErrTimeout, err)
	}
	return fmt.Errorf("fetcher: read body: %w", err)
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// parseContentRange parses a "bytes START-END/TOTAL" header. TOTAL may be "*".
func parseContentRange(header string) (start, end int64, err error) {
	fields := strings.Fields(strings.TrimSpace(header))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "bytes") {
		return 0, 0, errors.New("malformed content-range")
	}

	bounds, total, ok := strings.Cut(fields[1], "/")
	if !ok {
		return 0, 0, errors.New("missing total size")
	}
	if total != "*" {
		if _, err := strconv.ParseInt(total, 10, 64); err != nil {
			return 0, 0, fmt.Errorf("invalid total size %q", total)
		}
	}

	startStr, endStr, ok := strings.Cut(bounds, "-")
	if !ok {
		return 0, 0, errors.New("malformed byte range")
	}
	start, err = strconv.ParseInt(strings.TrimSpace(startStr), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range start %q", startStr)
	}
	end, err = strconv.ParseInt(strings.TrimSpace(endStr), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range end %q", endStr)
	}
	if start < 0 || end < start {
		return 0, 0, fmt.Errorf("invalid byte range %d-%d", start, end)
	}
	return start, end, nil
}
