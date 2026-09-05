package fetcher

import (
	"net"
	"net/http"
	"time"
)

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
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}

			applyDefaultHeaders(req)
			return nil
		},
	}
}
