package fetcher_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"adaptive-downloader/pkg/fetcher"
	"adaptive-downloader/pkg/rangetracker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.GreaterOrEqual(t, metrics.TTFB, time.Duration(0))
	assert.GreaterOrEqual(t, metrics.Duration, metrics.TTFB)
	assert.Greater(t, metrics.Throughput, float64(0))

	assert.Equal(t, int64(16), writer.TotalBytesWritten())
	assert.Equal(t, payload[10:26], writer.buf[10:26])
}

func TestFetch_MultipleBufferChunks(t *testing.T) {
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
		w.WriteHeader(http.StatusOK)
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
			End:   20,
		},
	}

	assert.PanicsWithValue(t, "developer bug: invalid fetch interval start 50 > end 20", func() {
		_, _ = client.Fetch(context.Background(), req, writer)
	})
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
	cancel()

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

func TestFetch_ContextCancelledMidFlight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusPartialContent)

		_, _ = w.Write([]byte("initial chunk of bytes"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("second chunk of bytes"))
	}))
	defer server.Close()

	client := fetcher.New(1)
	writer := &cancelOnWriteWriter{
		mockWriter: newMockWriter(1000),
		cancel:     cancel,
	}

	req := fetcher.FetchRequest{
		ReqID: "req-cancelled-midflight",
		URL:   server.URL,
		Interval: rangetracker.Interval{
			Start: 0,
			End:   999,
		},
	}

	metrics, err := client.Fetch(ctx, req, writer)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "context canceled")
	assert.Greater(t, metrics.BytesWritten, int64(0))
	assert.Equal(t, metrics.BytesWritten, writer.TotalBytesWritten())
}
