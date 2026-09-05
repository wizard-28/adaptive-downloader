package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"

	"adaptive-downloader/pkg/rangetracker"
)

// ChunkWriter defines the interface for writing byte chunks to an offset.
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
	Throughput   float64
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

	return streamResponse(start, ttfb, req.Interval.Start, resp.Body, w)
}

func closeResponse(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func calculateTTFB(start, firstByte time.Time) time.Duration {
	if !firstByte.IsZero() {
		return firstByte.Sub(start)
	}

	return time.Since(start)
}
