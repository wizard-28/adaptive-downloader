package fetcher

import (
	"context"
	"net/http"
	"strings"
)

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
