package fetcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"time"

	"adaptive-downloader/pkg/rangetracker"
)

// ChunkWriter defines the interface for writing byte chunks to an offset.
// Matches pkg/assembler.Writer signature.
type ChunkWriter interface {
	WriteChunk(offset int64, data []byte) (int, error)
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
	Throughput   float64 // bytes per second
}

// Client handles HTTP byte-range requests and client connection pooling.
type Client struct {
	client *http.Client
}

type headerTransport struct {
	base http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone request header map to avoid mutation races across goroutines
	reqClone := req.Clone(req.Context())

	reqClone.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	reqClone.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	reqClone.Header.Set("Accept-Language", "en-US,en;q=0.9")
	reqClone.Header.Set("Sec-Ch-Ua", `"Chromium";v="128", "Not=A?Brand";v="24", "Google Chrome";v="128"`)
	reqClone.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	reqClone.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	reqClone.Header.Set("Sec-Fetch-Dest", "document")
	reqClone.Header.Set("Sec-Fetch-Mode", "navigate")
	reqClone.Header.Set("Sec-Fetch-Site", "none")
	reqClone.Header.Set("Sec-Fetch-User", "?1")
	reqClone.Header.Set("Upgrade-Insecure-Requests", "1")

	return t.base.RoundTrip(reqClone)
}

// New creates a Fetcher with an http.Client tuned for concurrent chunk downloads.
func New(maxWorkers int) *Client {
	if maxWorkers <= 0 {
		panic("maxWorkers cannot be less than or equal to 0")
	}

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

	return &Client{
		client: &http.Client{
			Transport: &headerTransport{base: transport},
		},
	}
}

// Fetch downloads a specific byte range, streams data to w, and returns performance metrics.
func (c *Client) Fetch(ctx context.Context, req FetchRequest, w ChunkWriter) (Metrics, error) {
	if req.Interval.End < req.Interval.Start {
		panic(fmt.Sprintf("developer bug: invalid fetch interval start %d > end %d", req.Interval.Start, req.Interval.End))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return Metrics{}, fmt.Errorf("create HTTP request: %w", err)
	}
	httpReq.Header.Set("Range", req.Interval.String())

	var (
		start         = time.Now()
		firstByteTime time.Time
	)

	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() {
			firstByteTime = time.Now()
		},
	}
	httpReq = httpReq.WithContext(httptrace.WithClientTrace(httpReq.Context(), trace))

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return Metrics{
			Duration: time.Since(start),
		}, fmt.Errorf("execute HTTP request: %w", err)
	}

	// We are doing this as Go's http.client requires two conditions to be met to
	// *reuse* an existing connection via HTTP Keep-Alives
	// to avoid the performance penalty of creating a *new* connection
	// - Response body should be fully drained
	// - Response body should be closed
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	var ttfb time.Duration
	// Fallback in case firstByteTime is zero
	if firstByteTime.IsZero() {
		ttfb = time.Since(start)
	} else {
		ttfb = firstByteTime.Sub(start)
	}

	if resp.StatusCode != http.StatusPartialContent {
		return Metrics{
			TTFB:     ttfb,
			Duration: time.Since(start),
		}, fmt.Errorf("unexpected status code: %d (expected 206 Partial Content)", resp.StatusCode)
	}

	// Stream data off the network in 32 KB buffers
	buf := make([]byte, 32*1024)
	var (
		bytesWritten int64
		offset       = req.Interval.Start
	)

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			written, writeErr := w.WriteChunk(offset, buf[:n])
			if writeErr != nil {
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
				}, fmt.Errorf("write chunk at offset %d: %w", offset, writeErr)
			}
			bytesWritten += int64(written)
			offset += int64(written)
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
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
			}, fmt.Errorf("read response body: %w", readErr)
		}
	}

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
	}, nil
}
