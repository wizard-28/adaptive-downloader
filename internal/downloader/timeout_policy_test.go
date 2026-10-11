package downloader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestWatchdogOnlyPolicyRetriesGenuineFailures proves that removing request
// deadlines keeps in-place retries for genuine transport failures.
func TestWatchdogOnlyPolicyRetriesGenuineFailures(t *testing.T) {
	content := pattern(100)
	rs, srv := newTestServer(content)
	t.Cleanup(srv.Close)
	rs.failFor[0] = 1

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 100
	cfg.MaxRetries = 1
	cfg.Timeout = 0
	cfg.NoRequestTimeout = true
	cfg.NoDialTimeout = true

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}
	assertFileEquals(t, out, content)
	if got := rs.attemptCount(0); got != 2 {
		t.Fatalf("attempts = %d, want 2 (genuine failure retried)", got)
	}
}

// TestWatchdogOnlyPolicyDoesNotRetryCancellation proves context cancellation
// is never retried, even with no request deadline configured.
func TestWatchdogOnlyPolicyDoesNotRetryCancellation(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		once.Do(func() { close(started) })
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, 100)
	cfg.Concurrency = 1
	cfg.ChunkSize = 100
	cfg.MaxRetries = 3
	cfg.Timeout = 0
	cfg.NoRequestTimeout = true
	cfg.NoDialTimeout = true

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- dl.Download(ctx) }()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("request never started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Download = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("download did not stop after cancellation")
	}
	if got := dl.Metrics().Aggregate.TotalAttempts; got != 1 {
		t.Fatalf("attempts = %d, want 1 (cancellation must not be retried)", got)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("HTTP requests = %d, want 1", got)
	}
}
