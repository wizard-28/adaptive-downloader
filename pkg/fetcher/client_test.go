package fetcher_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"adaptive-downloader/pkg/fetcher"
	"adaptive-downloader/pkg/rangetracker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type writeRecord struct {
	offset int64
	data   []byte
}

type mockWriter struct {
	mu          sync.Mutex
	writes      []writeRecord
	buf         []byte
	errToReturn error
}

func newMockWriter(size int64) *mockWriter {
	return &mockWriter{
		buf: make([]byte, size),
	}
}

func (m *mockWriter) WriteChunk(offset int64, data []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.errToReturn != nil {
		return 0, m.errToReturn
	}

	record := writeRecord{
		offset: offset,
		data:   append([]byte(nil), data...),
	}
	m.writes = append(m.writes, record)

	if offset+int64(len(data)) <= int64(len(m.buf)) {
		copy(m.buf[offset:], data)
	}

	return len(data), nil
}

func (m *mockWriter) TotalBytesWritten() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	var total int64
	for _, w := range m.writes {
		total += int64(len(w.data))
	}
	return total
}

func TestProbe_HeadSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", "50000")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	client := fetcher.New(2)
	res, err := client.Probe(context.Background(), server.URL)
	require.NoError(t, err)
	assert.True(t, res.AcceptsRanges)
	assert.Equal(t, int64(50000), res.ContentLength)

	// Test package-level helper
	resPkg, err := fetcher.Probe(context.Background(), server.URL)
	require.NoError(t, err)
	assert.True(t, resPkg.AcceptsRanges)
	assert.Equal(t, int64(50000), resPkg.ContentLength)
}

func TestProbe_HeadFails_FallbackGet206(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			// Simulate server rejecting HEAD requests
			http.Error(w, "HEAD forbidden", http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodGet {
			assert.Equal(t, "bytes=0-0", r.Header.Get("Range"))
			w.Header().Set("Content-Range", "bytes 0-0/123456")
			w.Header().Set("Content-Length", "1")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("X"))
			return
		}
		http.Error(w, "unexpected", http.StatusBadRequest)
	}))
	defer server.Close()

	client := fetcher.New(2)
	res, err := client.Probe(context.Background(), server.URL)
	require.NoError(t, err)
	assert.True(t, res.AcceptsRanges)
	assert.Equal(t, int64(123456), res.ContentLength)
}

func TestProbe_NoRangeSupport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			// HEAD does not return Accept-Ranges
			w.Header().Set("Content-Length", "8000")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodGet {
			// Server ignores Range header and returns full content 200 OK
			w.Header().Set("Content-Length", "8000")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, 8000))
			return
		}
	}))
	defer server.Close()

	client := fetcher.New(1)
	res, err := client.Probe(context.Background(), server.URL)
	require.NoError(t, err)
	assert.False(t, res.AcceptsRanges)
	assert.Equal(t, int64(8000), res.ContentLength)
}

func TestProbe_Errors(t *testing.T) {
	t.Run("server 500 error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}))
		defer server.Close()

		client := fetcher.New(1)
		_, err := client.Probe(context.Background(), server.URL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("malformed content-range header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				http.Error(w, "no head", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Range", "bytes invalid")
			w.WriteHeader(http.StatusPartialContent)
		}))
		defer server.Close()

		client := fetcher.New(1)
		_, err := client.Probe(context.Background(), server.URL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse Content-Range")
	})
}

func TestFetch_Success(t *testing.T) {
	payload := []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		assert.Equal(t, "bytes=10-25", rangeHeader)

		w.Header().Set("Content-Range", fmt.Sprintf("bytes 10-25/%d", len(payload)))
		w.Header().Set("Content-Length", "16")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[10:26])
	}))
	defer server.Close()

	client := fetcher.New(2)
	writer := newMockWriter(int64(len(payload)))

	req := fetcher.FetchRequest{
		ReqID: "req-1",
		URL:   server.URL,
		Interval: rangetracker.Interval{
			Start: 10,
			End:   25,
		},
	}

	metrics, err := client.Fetch(context.Background(), req, writer)
	require.NoError(t, err)

	assert.Equal(t, int64(16), metrics.BytesWritten)
	assert.Greater(t, metrics.TTFB, time.Duration(0))
	assert.GreaterOrEqual(t, metrics.Duration, metrics.TTFB)
	assert.Greater(t, metrics.Throughput, float64(0))

	assert.Equal(t, int64(16), writer.TotalBytesWritten())
	assert.Equal(t, payload[10:26], writer.buf[10:26])
}

func TestFetch_MultipleBufferChunks(t *testing.T) {
	// Generate payload larger than 32KB buffer (e.g. 70KB)
	largePayload := make([]byte, 70*1024)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, fmt.Sprintf("bytes=0-%d", len(largePayload)-1), r.Header.Get("Range"))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(largePayload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(largePayload)
	}))
	defer server.Close()

	client := fetcher.New(2)
	writer := newMockWriter(int64(len(largePayload)))

	req := fetcher.FetchRequest{
		ReqID: "req-large",
		URL:   server.URL,
		Interval: rangetracker.Interval{
			Start: 0,
			End:   int64(len(largePayload) - 1),
		},
	}

	metrics, err := client.Fetch(context.Background(), req, writer)
	require.NoError(t, err)
	assert.Equal(t, int64(len(largePayload)), metrics.BytesWritten)
	assert.Equal(t, largePayload, writer.buf)
	assert.Greater(t, len(writer.writes), 1, "expected multiple write operations for payload > 32KB")
}

func TestFetch_Non206Rejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // returns 200 instead of 206
		_, _ = w.Write([]byte("whole file content"))
	}))
	defer server.Close()

	client := fetcher.New(1)
	writer := newMockWriter(100)

	req := fetcher.FetchRequest{
		ReqID: "req-200",
		URL:   server.URL,
		Interval: rangetracker.Interval{
			Start: 0,
			End:   15,
		},
	}

	metrics, err := client.Fetch(context.Background(), req, writer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 206 Partial Content")
	assert.Equal(t, int64(0), metrics.BytesWritten)
}

func TestFetch_WriterError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("some data"))
	}))
	defer server.Close()

	client := fetcher.New(1)
	writer := newMockWriter(100)
	writer.errToReturn = errors.New("disk full error")

	req := fetcher.FetchRequest{
		ReqID: "req-err",
		URL:   server.URL,
		Interval: rangetracker.Interval{
			Start: 0,
			End:   8,
		},
	}

	_, err := client.Fetch(context.Background(), req, writer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full error")
}

func TestFetch_InvalidInterval(t *testing.T) {
	client := fetcher.New(1)
	writer := newMockWriter(100)

	req := fetcher.FetchRequest{
		ReqID: "req-inv",
		URL:   "http://localhost:1234",
		Interval: rangetracker.Interval{
			Start: 50,
			End:   20, // Start > End
		},
	}

	_, err := client.Fetch(context.Background(), req, writer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid interval")
}

func TestFetch_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer server.Close()

	client := fetcher.New(1)
	writer := newMockWriter(100)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := fetcher.FetchRequest{
		ReqID: "req-cancelled",
		URL:   server.URL,
		Interval: rangetracker.Interval{
			Start: 0,
			End:   10,
		},
	}

	_, err := client.Fetch(ctx, req, writer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}
