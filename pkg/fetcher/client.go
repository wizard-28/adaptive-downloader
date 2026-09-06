package fetcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"adaptive-downloader/pkg/rangetracker"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"

var ErrProbeFailed = errors.New(
	"HEAD probe failed or did not provide valid range information",
)

// Fetcher handles HTTP byte-range requests and client connection pooling.
type Fetcher struct {
	client *http.Client
}

// ProbeResult holds the results of probing an HTTP server.
type ProbeResult struct {
	ContentLength int64
	AcceptsRanges bool
}

// FetchRequest represents a byte-range fetch request.
type FetchRequest struct {
	ReqID    string
	URL      string
	Interval rangetracker.Interval
}

// Metrics captures telemetry data collected during a chunk download.
type Metrics struct {
	BytesWritten int64
	TTFB         time.Duration
	Duration     time.Duration
	Throughput   float64
}

// ChunkWriter defines the interface for writing byte chunks to an offset.
type ChunkWriter interface {
	WriteChunk(offset int64, data []byte) (int, error)
}

// New creates a Fetcher with an HTTP client tuned for concurrent chunk downloads.
func New(maxWorkers int) *Fetcher {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}

	return &Fetcher{
		client: newHTTPClient(maxWorkers),
	}
}

// DefaultFetcher is the package-level fetcher used by Probe.
var DefaultFetcher = New(1)

// Probe inspects the remote URL using the default fetcher.
func Probe(ctx context.Context, url string) (ProbeResult, error) {
	return DefaultFetcher.Probe(ctx, url)
}

// Probe inspects the remote URL for file size and byte-range support.
//
// It uses a HEAD request to determine whether the server supports
// byte-range requests and to obtain the total file size.
func (f *Fetcher) Probe(
	ctx context.Context,
	url string,
) (ProbeResult, error) {
	result, ok := f.probeHEAD(ctx, url)

	if !ok {
		return ProbeResult{}, ErrProbeFailed
	}

	return result, nil
}

func (f *Fetcher) probeHEAD(
	ctx context.Context,
	url string,
) (ProbeResult, bool) {
	req, err := newRequest(ctx, http.MethodHead, url)
	if err != nil {
		return ProbeResult{}, false
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return ProbeResult{}, false
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ProbeResult{}, false
	}

	acceptsRanges := strings.Contains(
		strings.ToLower(resp.Header.Get("Accept-Ranges")),
		"bytes",
	)

	if !acceptsRanges || resp.ContentLength <= 0 {
		return ProbeResult{}, false
	}

	return ProbeResult{
		ContentLength: resp.ContentLength,
		AcceptsRanges: true,
	}, true
}

// Fetch downloads a specific byte range, streams data to w,
// and returns performance metrics.
func (f *Fetcher) Fetch(
	ctx context.Context,
	req FetchRequest,
	w ChunkWriter,
) (Metrics, error) {
	if req.Interval.End < req.Interval.Start {
		return Metrics{}, fmt.Errorf(
			"invalid interval: start %d > end %d",
			req.Interval.Start,
			req.Interval.End,
		)
	}

	httpReq, err := newRequest(ctx, http.MethodGet, req.URL)
	if err != nil {
		return Metrics{}, fmt.Errorf("create HTTP request: %w", err)
	}

	httpReq.Header.Set("Range", req.Interval.String())

	start := time.Now()
	var firstByteTime time.Time

	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() {
			firstByteTime = time.Now()
		},
	}

	httpReq = httpReq.WithContext(
		httptrace.WithClientTrace(httpReq.Context(), trace),
	)

	resp, err := f.client.Do(httpReq)
	if err != nil {
		return Metrics{
			Duration: time.Since(start),
		}, fmt.Errorf("execute HTTP request: %w", err)
	}

	defer closeResponse(resp)

	ttfb := calculateTTFB(start, firstByteTime)

	if resp.StatusCode != http.StatusPartialContent {
		return Metrics{
			TTFB:     ttfb,
			Duration: time.Since(start),
		}, fmt.Errorf(
			"unexpected status code: %d (expected 206 Partial Content)",
			resp.StatusCode,
		)
	}

	return streamResponse(
		start,
		ttfb,
		req.Interval.Start,
		resp.Body,
		w,
	)
}

func closeResponse(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func calculateTTFB(
	start,
	firstByte time.Time,
) time.Duration {
	if !firstByte.IsZero() {
		return firstByte.Sub(start)
	}

	return time.Since(start)
}

func streamResponse(
	start time.Time,
	ttfb time.Duration,
	offset int64,
	body io.Reader,
	w ChunkWriter,
) (Metrics, error) {
	buf := make([]byte, 32*1024)

	var bytesWritten int64

	for {
		n, readErr := body.Read(buf)

		if n > 0 {
			written, writeErr := w.WriteChunk(
				offset,
				buf[:n],
			)

			if writeErr != nil {
				return buildMetrics(
						start,
						ttfb,
						bytesWritten,
					), fmt.Errorf(
						"write chunk at offset %d: %w",
						offset,
						writeErr,
					)
			}

			bytesWritten += int64(written)
			offset += int64(written)
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}

			return buildMetrics(
					start,
					ttfb,
					bytesWritten,
				), fmt.Errorf(
					"read response body: %w",
					readErr,
				)
		}
	}

	return buildMetrics(
		start,
		ttfb,
		bytesWritten,
	), nil
}

func buildMetrics(
	start time.Time,
	ttfb time.Duration,
	bytesWritten int64,
) Metrics {
	duration := time.Since(start)

	var throughput float64

	if duration > 0 {
		throughput = float64(bytesWritten) / duration.Seconds()
	}

	return Metrics{
		BytesWritten: bytesWritten,
		TTFB:         ttfb,
		Duration:     duration,
		Throughput:   throughput,
	}
}

func applyDefaultHeaders(req *http.Request) {
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
}

func newRequest(
	ctx context.Context,
	method string,
	url string,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		method,
		url,
		nil,
	)

	if err != nil {
		return nil, err
	}

	applyDefaultHeaders(req)

	return req, nil
}

func newHTTPClient(maxWorkers int) *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,

		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,

		MaxIdleConns:        maxWorkers,
		MaxIdleConnsPerHost: maxWorkers,
		IdleConnTimeout:     90 * time.Second,
	}

	return &http.Client{
		Transport: transport,

		// Some download servers redirect from a landing/download URL
		// to the actual file location.
		CheckRedirect: func(
			req *http.Request,
			via []*http.Request,
		) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}

			applyDefaultHeaders(req)

			return nil
		},
	}
}
