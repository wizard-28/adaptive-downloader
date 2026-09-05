package fetcher

import (
	"context"
	"errors"
)

var ErrProbeFailed = errors.New(
	"HEAD probe failed or did not provide valid range information",
)

// ProbeResult holds the results of probing an HTTP server.
type ProbeResult struct {
	ContentLength int64
	AcceptsRanges bool
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
func (f *Fetcher) Probe(ctx context.Context, url string) (ProbeResult, error) {
	result, ok := f.probeHEAD(ctx, url)

	if !ok {
		return ProbeResult{}, ErrProbeFailed
	}

	return result, nil
}
