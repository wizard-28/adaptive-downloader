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

var (
	ErrProbeFailed      = errors.New("HTTP range probe failed")
	ErrRangeUnsupported = errors.New("server does not support HTTP byte-range requests")
)

// ProbeResult holds the results of probing an HTTP server.
type ProbeResult struct {
	ContentLength int64
}

// DefaultClient is a global single-worker fetcher instance used for
// package-level Probe calls.
var DefaultClient = New(1)

// Probe inspects the remote URL using the default fetcher to determine file
// size and whether it supports HTTP byte-range requests.
func Probe(ctx context.Context, url string) (ProbeResult, error) {
	return DefaultClient.Probe(ctx, url)
}

// Probe inspects the remote URL using this fetcher's HTTP client.
//
// A successful probe guarantees that the server supports HTTP byte-range
// requests and that a valid positive content length was determined.
func (c *Client) Probe(ctx context.Context, url string) (ProbeResult, error) {
	// First try HEAD. Some servers provide both Content-Length and
	// Accept-Ranges here, which avoids downloading any file data.
	headReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodHead,
		url,
		nil,
	)
	if err == nil {
		headResp, headErr := c.client.Do(headReq)
		if headErr == nil {
			defer func() {
				_ = headResp.Body.Close()
			}()

			if headResp.StatusCode == http.StatusOK {
				acceptsRanges := strings.Contains(
					strings.ToLower(headResp.Header.Get("Accept-Ranges")),
					"bytes",
				)

				if acceptsRanges && headResp.ContentLength > 0 {
					return ProbeResult{
						ContentLength: headResp.ContentLength,
					}, nil
				}
			}
		}
	}

	// Fall back to a one-byte range request when HEAD does not provide
	// sufficient information.
	getReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return ProbeResult{}, fmt.Errorf(
			"%w: create fallback GET request: %w",
			ErrProbeFailed,
			err,
		)
	}

	getReq.Header.Set("Range", "bytes=0-0")

	getResp, err := c.client.Do(getReq)
	if err != nil {
		return ProbeResult{}, fmt.Errorf(
			"%w: execute fallback GET request: %w",
			ErrProbeFailed,
			err,
		)
	}

	defer func() {
		_, _ = io.Copy(io.Discard, getResp.Body)
		_ = getResp.Body.Close()
	}()

	switch getResp.StatusCode {
	case http.StatusPartialContent:
		contentRange := getResp.Header.Get("Content-Range")

		totalSize, err := parseContentRangeTotal(contentRange)
		if err != nil {
			return ProbeResult{}, fmt.Errorf(
				"%w: parse Content-Range %q: %w",
				ErrProbeFailed,
				contentRange,
				err,
			)
		}

		return ProbeResult{
			ContentLength: totalSize,
		}, nil

	case http.StatusOK:
		// The server ignored the Range request and returned the full object.
		return ProbeResult{}, fmt.Errorf(
			"%w: server ignored byte-range request",
			ErrRangeUnsupported,
		)

	default:
		return ProbeResult{}, fmt.Errorf(
			"%w: unexpected status code %d during probe",
			ErrProbeFailed,
			getResp.StatusCode,
		)
	}
}

// parseContentRangeTotal extracts the total file size from a Content-Range
// header. For example, "bytes 0-0/1048576" returns 1048576.
func parseContentRangeTotal(header string) (int64, error) {
	if header == "" {
		return 0, errors.New("empty Content-Range header")
	}

	parts := strings.Split(header, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf(
			"malformed Content-Range header: %q",
			header,
		)
	}

	totalStr := strings.TrimSpace(parts[1])

	if totalStr == "*" {
		return 0, errors.New(
			"wildcard '*' total size in Content-Range header",
		)
	}

	total, err := strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf(
			"invalid total size %q in Content-Range: %w",
			totalStr,
			err,
		)
	}

	if total <= 0 {
		return 0, fmt.Errorf(
			"non-positive total size %d in Content-Range",
			total,
		)
	}

	return total, nil
}
