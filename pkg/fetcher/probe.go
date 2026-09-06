package fetcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// ProbeResult holds the results of probing an HTTP server for byte-range support and file size.
type ProbeResult struct {
	ContentLength int64
	AcceptsRanges bool
}

// DefaultFetcher is a global single-worker fetcher instance used for package-level Probe calls.
var DefaultFetcher = New(1)

// Probe inspects the remote URL using the default fetcher to determine file size and range capabilities.
func Probe(ctx context.Context, url string) (ProbeResult, error) {
	return DefaultFetcher.Probe(ctx, url)
}

// Probe inspects the remote URL using this fetcher's HTTP client.
func (f *Fetcher) Probe(ctx context.Context, url string) (ProbeResult, error) {
	// Step 1: Attempt HEAD request.
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err == nil {
		headResp, headErr := f.client.Do(headReq)
		if headErr == nil {
			defer func() {
				_ = headResp.Body.Close()
			}()

			if headResp.StatusCode == http.StatusOK {
				acceptsRanges := strings.Contains(strings.ToLower(headResp.Header.Get("Accept-Ranges")), "bytes")
				if acceptsRanges && headResp.ContentLength > 0 {
					return ProbeResult{
						ContentLength: headResp.ContentLength,
						AcceptsRanges: true,
					}, nil
				}
			}
		}
	}

	// Step 2: Fallback GET with Range: bytes=0-0 if HEAD fails, returns non-200, or lacks range headers.
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("create fallback GET request: %w", err)
	}
	getReq.Header.Set("Range", "bytes=0-0")

	getResp, err := f.client.Do(getReq)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("execute fallback GET request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, getResp.Body)
		_ = getResp.Body.Close()
	}()

	if getResp.StatusCode == http.StatusPartialContent {
		contentRange := getResp.Header.Get("Content-Range")
		totalSize, parseErr := parseContentRangeTotal(contentRange)
		if parseErr != nil {
			return ProbeResult{}, fmt.Errorf("parse Content-Range %q: %w", contentRange, parseErr)
		}

		return ProbeResult{
			ContentLength: totalSize,
			AcceptsRanges: true,
		}, nil
	}

	if getResp.StatusCode == http.StatusOK {
		return ProbeResult{
			ContentLength: getResp.ContentLength,
			AcceptsRanges: false,
		}, nil
	}

	return ProbeResult{}, fmt.Errorf("unexpected status code %d during probe", getResp.StatusCode)
}

// parseContentRangeTotal extracts the total file size from a Content-Range header.
// E.g., "bytes 0-0/1048576" -> 1048576.
func parseContentRangeTotal(header string) (int64, error) {
	if header == "" {
		return 0, errors.New("empty Content-Range header")
	}

	parts := strings.Split(header, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("malformed Content-Range header: %q", header)
	}

	totalStr := strings.TrimSpace(parts[1])
	if totalStr == "*" {
		return 0, errors.New("wildcard '*' total size in Content-Range header")
	}

	total, err := strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid total size %q in Content-Range: %w", totalStr, err)
	}

	if total <= 0 {
		return 0, fmt.Errorf("non-positive total size %d in Content-Range", total)
	}

	return total, nil
}
