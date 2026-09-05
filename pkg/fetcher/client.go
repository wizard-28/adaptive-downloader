package fetcher

import "net/http"

// Fetcher handles HTTP byte-range requests and client connection pooling.
type Fetcher struct {
	client *http.Client
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
