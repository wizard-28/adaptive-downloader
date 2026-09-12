package downloader_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"adaptive-downloader/pkg/downloader"
	"adaptive-downloader/pkg/policy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingPolicy struct {
	mu sync.Mutex

	action policy.Action

	observeCalls int
	selectCalls  int

	observedContexts []policy.Context
}

func newRecordingPolicy(action policy.Action) *recordingPolicy {
	return &recordingPolicy{
		action: action,
	}
}

func (p *recordingPolicy) Observe(ctx policy.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.observeCalls++
	p.observedContexts = append(p.observedContexts, ctx)
}

func (p *recordingPolicy) SelectAction() policy.Action {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.selectCalls++
	return p.action
}

func (p *recordingPolicy) stats() (
	selectCalls int,
	observeCalls int,
	contexts []policy.Context,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	contexts = append([]policy.Context(nil), p.observedContexts...)

	return p.selectCalls, p.observeCalls, contexts
}

var _ policy.Policy = (*recordingPolicy)(nil)

func TestDownloader_Download(t *testing.T) {
	payload := makeTestPayload(128 * 1024)

	t.Run("successful download", func(t *testing.T) {
		server := newRangeServer(t, payload)
		defer server.Close()

		outputPath := filepath.Join(
			t.TempDir(),
			"download.bin",
		)

		p := newRecordingPolicy(policy.Action{
			Concurrency: 3,
			ChunkSize:   16 * 1024,
		})

		d, err := downloader.New(downloader.Config{
			OutputPath:  outputPath,
			Policy:      p,
			Concurrency: 3,
			ChunkSize:   16 * 1024,
			NoProgress:  true,
		})
		require.NoError(t, err)

		err = d.Download(
			context.Background(),
			server.URL,
		)

		require.NoError(t, err)

		actual, err := os.ReadFile(outputPath)
		require.NoError(t, err)
		assert.Equal(t, payload, actual)

		selectCalls, observeCalls, contexts := p.stats()

		assert.Equal(t, 1, selectCalls)
		assert.Equal(t, 1, observeCalls)

		require.Len(t, contexts, 1)
		assert.Equal(t, 3, contexts[0].CurrentConcurrency)
		assert.Equal(t, int64(16*1024), contexts[0].CurrentChunkSize)
	})

	t.Run("policy action is used by downloader", func(t *testing.T) {
		server := newRangeServer(t, payload)
		defer server.Close()

		outputPath := filepath.Join(
			t.TempDir(),
			"download.bin",
		)

		// Configured values differ from the action returned by the policy.
		// The downloader should use the policy's action.
		p := newRecordingPolicy(policy.Action{
			Concurrency: 1,
			ChunkSize:   4 * 1024,
		})

		d, err := downloader.New(downloader.Config{
			OutputPath:  outputPath,
			Policy:      p,
			Concurrency: 8,
			ChunkSize:   32 * 1024,
			NoProgress:  true,
		})
		require.NoError(t, err)

		err = d.Download(
			context.Background(),
			server.URL,
		)

		require.NoError(t, err)

		actual, err := os.ReadFile(outputPath)
		require.NoError(t, err)
		assert.Equal(t, payload, actual)

		selectCalls, observeCalls, contexts := p.stats()

		assert.Equal(t, 1, selectCalls)
		assert.Equal(t, 1, observeCalls)

		require.Len(t, contexts, 1)

		// The initial context comes from Config.
		assert.Equal(t, 8, contexts[0].CurrentConcurrency)
		assert.Equal(t, int64(32*1024), contexts[0].CurrentChunkSize)
	})

	t.Run("checksum mismatch returns error", func(t *testing.T) {
		server := newRangeServer(t, payload)
		defer server.Close()

		outputPath := filepath.Join(
			t.TempDir(),
			"download.bin",
		)

		p := newRecordingPolicy(policy.Action{
			Concurrency: 2,
			ChunkSize:   16 * 1024,
		})

		d, err := downloader.New(downloader.Config{
			OutputPath: outputPath,
			Policy: newRecordingPolicy(policy.Action{
				Concurrency: 2,
				ChunkSize:   16 * 1024,
			}),
			Concurrency: 2,
			ChunkSize:   16 * 1024,
			SHA256:      strings.Repeat("0", 64),
			NoProgress:  true,
		})
		require.NoError(t, err)

		err = d.Download(
			context.Background(),
			server.URL,
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "checksum verification failed")

		_ = p
	})

	t.Run("valid checksum succeeds", func(t *testing.T) {
		server := newRangeServer(t, payload)
		defer server.Close()

		hash := sha256.Sum256(payload)

		outputPath := filepath.Join(
			t.TempDir(),
			"download.bin",
		)

		d, err := downloader.New(downloader.Config{
			OutputPath: outputPath,
			Policy: newRecordingPolicy(policy.Action{
				Concurrency: 2,
				ChunkSize:   16 * 1024,
			}),
			Concurrency: 2,
			ChunkSize:   16 * 1024,
			SHA256:      hex.EncodeToString(hash[:]),
			NoProgress:  true,
		})
		require.NoError(t, err)

		err = d.Download(
			context.Background(),
			server.URL,
		)

		require.NoError(t, err)
	})

	t.Run("rejects server without byte-range support", func(t *testing.T) {
		server := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(
					"Content-Length",
					strconv.Itoa(len(payload)),
				)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(payload)
			}),
		)
		defer server.Close()

		d, err := downloader.New(downloader.Config{
			OutputPath: filepath.Join(
				t.TempDir(),
				"download.bin",
			),
			Policy: newRecordingPolicy(policy.Action{
				Concurrency: 2,
				ChunkSize:   16 * 1024,
			}),
			Concurrency: 2,
			ChunkSize:   16 * 1024,
			NoProgress:  true,
		})
		require.NoError(t, err)

		err = d.Download(
			context.Background(),
			server.URL,
		)

		require.Error(t, err)
		assert.Contains(
			t,
			err.Error(),
			"server does not support HTTP byte-range requests",
		)
	})

	t.Run("cancellation is propagated", func(t *testing.T) {
		server := newSlowRangeServer(t, payload)
		defer server.Close()

		outputPath := filepath.Join(
			t.TempDir(),
			"download.bin",
		)

		d, err := downloader.New(downloader.Config{
			OutputPath: outputPath,
			Policy: newRecordingPolicy(policy.Action{
				Concurrency: 2,
				ChunkSize:   16 * 1024,
			}),
			Concurrency: 2,
			ChunkSize:   16 * 1024,
			NoProgress:  true,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(25 * time.Millisecond)
			cancel()
		}()

		err = d.Download(ctx, server.URL)

		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestDownloader_DownloadWithRetry(t *testing.T) {
	payload := makeTestPayload(64 * 1024)

	var (
		mu          sync.Mutex
		getRequests int
		failedOnce  bool
	)

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Set("Accept-Ranges", "bytes")
				w.Header().Set(
					"Content-Length",
					strconv.Itoa(len(payload)),
				)
				w.WriteHeader(http.StatusOK)
				return
			}

			if r.Method != http.MethodGet {
				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)
				return
			}

			mu.Lock()
			getRequests++

			shouldFail := !failedOnce
			if shouldFail {
				failedOnce = true
			}
			mu.Unlock()

			if shouldFail {
				http.Error(
					w,
					"temporary failure",
					http.StatusServiceUnavailable,
				)
				return
			}

			writeRangeResponse(w, r, payload)
		}),
	)
	defer server.Close()

	outputPath := filepath.Join(
		t.TempDir(),
		"download.bin",
	)

	d, err := downloader.New(downloader.Config{
		OutputPath: outputPath,
		Policy: newRecordingPolicy(policy.Action{
			Concurrency: 1,
			ChunkSize:   16 * 1024,
		}),
		Concurrency: 1,
		ChunkSize:   16 * 1024,
		NoProgress:  true,
	})
	require.NoError(t, err)

	err = d.Download(
		context.Background(),
		server.URL,
	)

	require.NoError(t, err)

	actual, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Equal(t, payload, actual)

	mu.Lock()
	requestCount := getRequests
	mu.Unlock()

	assert.GreaterOrEqual(t, requestCount, 2)
}

func newRangeServer(
	t *testing.T,
	payload []byte,
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Set("Accept-Ranges", "bytes")
				w.Header().Set(
					"Content-Length",
					strconv.Itoa(len(payload)),
				)
				w.WriteHeader(http.StatusOK)
				return
			}

			if r.Method == http.MethodGet {
				writeRangeResponse(w, r, payload)
				return
			}

			http.Error(
				w,
				"method not allowed",
				http.StatusMethodNotAllowed,
			)
		}),
	)
}

func newSlowRangeServer(
	t *testing.T,
	payload []byte,
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Set("Accept-Ranges", "bytes")
				w.Header().Set(
					"Content-Length",
					strconv.Itoa(len(payload)),
				)
				w.WriteHeader(http.StatusOK)
				return
			}

			if r.Method != http.MethodGet {
				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)
				return
			}

			time.Sleep(250 * time.Millisecond)
			writeRangeResponse(w, r, payload)
		}),
	)
}

func writeRangeResponse(
	w http.ResponseWriter,
	r *http.Request,
	payload []byte,
) {
	rangeHeader := r.Header.Get("Range")

	if rangeHeader == "" {
		w.Header().Set(
			"Content-Length",
			strconv.Itoa(len(payload)),
		)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
		return
	}

	start, end, ok := parseRange(
		rangeHeader,
		int64(len(payload)),
	)
	if !ok {
		http.Error(
			w,
			"invalid range",
			http.StatusRequestedRangeNotSatisfiable,
		)
		return
	}

	body := payload[start : end+1]

	w.Header().Set(
		"Content-Range",
		fmt.Sprintf(
			"bytes %d-%d/%d",
			start,
			end,
			len(payload),
		),
	)

	w.Header().Set(
		"Content-Length",
		strconv.Itoa(len(body)),
	)

	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)
}

func parseRange(
	header string,
	total int64,
) (int, int, bool) {
	const prefix = "bytes="

	if !strings.HasPrefix(header, prefix) {
		return 0, 0, false
	}

	parts := strings.Split(
		strings.TrimPrefix(header, prefix),
		"-",
	)
	if len(parts) != 2 {
		return 0, 0, false
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}

	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}

	if start < 0 || start > end || end >= total {
		return 0, 0, false
	}

	return int(start), int(end), true
}

func makeTestPayload(size int) []byte {
	payload := make([]byte, size)

	for i := range payload {
		payload[i] = byte((i * 31) % 251)
	}

	return payload
}
