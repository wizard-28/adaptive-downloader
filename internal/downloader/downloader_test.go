package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"adaptivedl/internal/chunksize"
	"adaptivedl/internal/concurrency"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl"
)

// pattern returns n deterministic bytes.
func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*31 + 7) % 256)
	}
	return b
}

// parseRangeHeader parses "bytes=START-END".
func parseRangeHeader(header string) (start, end int64, ok bool) {
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return 0, 0, false
	}
	startStr, endStr, found := strings.Cut(strings.TrimPrefix(header, prefix), "-")
	if !found {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end, err = strconv.ParseInt(endStr, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return start, end, true
}

// rangeServer is a deterministic HTTP range server whose failure and latency
// behavior can be controlled per starting offset.
type rangeServer struct {
	content []byte

	mu       sync.Mutex
	attempts map[int64]int
	failFor  map[int64]int
	delay    map[int64]time.Duration
	order    []int64
}

func newRangeServer(content []byte) *rangeServer {
	return &rangeServer{
		content:  content,
		attempts: make(map[int64]int),
		failFor:  make(map[int64]int),
		delay:    make(map[int64]time.Duration),
	}
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start, end, ok := parseRangeHeader(r.Header.Get("Range"))
	if !ok || start < 0 || start >= int64(len(s.content)) || end < start {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if end >= int64(len(s.content)) {
		end = int64(len(s.content)) - 1
	}

	s.mu.Lock()
	s.attempts[start]++
	remaining := s.failFor[start]
	if remaining > 0 {
		s.failFor[start] = remaining - 1
	}
	delay := s.delay[start]
	s.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	if remaining > 0 {
		http.Error(w, "transient failure", http.StatusInternalServerError)
		return
	}

	body := s.content[start : end+1]
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.content)))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)

	s.mu.Lock()
	s.order = append(s.order, start)
	s.mu.Unlock()
}

func (s *rangeServer) attemptCount(start int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts[start]
}

func (s *rangeServer) completionOrder() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.order...)
}

func newTestServer(content []byte) (*rangeServer, *httptest.Server) {
	rs := newRangeServer(content)
	return rs, httptest.NewServer(rs)
}

func assertFileEquals(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) = %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("output file does not match source (%d bytes vs %d bytes)", len(got), len(want))
	}
}

func baseConfig(url, out string, size int64) Config {
	return Config{
		URL:         url,
		OutputPath:  out,
		TotalSize:   size,
		Concurrency: 4,
		ChunkSize:   64,
		Timeout:     5 * time.Second,
		MaxRetries:  3,
	}
}

func TestDownloadConcurrency1(t *testing.T) {
	content := pattern(1000)
	_, srv := newTestServer(content)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 64

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}
	assertFileEquals(t, out, content)
}

func TestDownloadConcurrencyN(t *testing.T) {
	content := pattern(2000)
	_, srv := newTestServer(content)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 8
	cfg.ChunkSize = 32

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}
	assertFileEquals(t, out, content)
}

func TestDownloadOutOfOrder(t *testing.T) {
	content := pattern(1000)
	rs, srv := newTestServer(content)
	t.Cleanup(srv.Close)
	// Make the first range slow so later ranges finish first.
	rs.delay[0] = 150 * time.Millisecond

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 4
	cfg.ChunkSize = 100

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}
	assertFileEquals(t, out, content)

	order := rs.completionOrder()
	if len(order) < 2 {
		t.Fatalf("expected multiple completions, got %v", order)
	}
	if order[0] == 0 {
		t.Fatalf("expected the delayed range 0 to finish late, order = %v", order)
	}
}

func TestDownloadFinalPartialChunk(t *testing.T) {
	content := pattern(250)
	_, srv := newTestServer(content)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 3
	cfg.ChunkSize = 100 // 250 = 100 + 100 + 50

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}
	assertFileEquals(t, out, content)
}

func TestDownloadRetryThenSuccess(t *testing.T) {
	content := pattern(100)
	rs, srv := newTestServer(content)
	t.Cleanup(srv.Close)
	rs.failFor[0] = 1

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 100
	cfg.MaxRetries = 3

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}
	assertFileEquals(t, out, content)

	if got := rs.attemptCount(0); got != 2 {
		t.Fatalf("attempts for range 0 = %d, want 2 (one failure + one success)", got)
	}
}

func TestDownloadRetryLimitExceeded(t *testing.T) {
	content := pattern(100)
	rs, srv := newTestServer(content)
	t.Cleanup(srv.Close)
	rs.failFor[0] = 1000 // effectively always fail

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 100
	cfg.MaxRetries = 2

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	err = dl.Download(context.Background())
	if !errors.Is(err, ErrRetryLimitExceeded) {
		t.Fatalf("Download = %v, want ErrRetryLimitExceeded", err)
	}
	if got := rs.attemptCount(0); got != 3 {
		t.Fatalf("attempts for range 0 = %d, want 3 (maxRetries+1)", got)
	}
}

func TestDownloadContextCancellation(t *testing.T) {
	blocking := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(blocking.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(blocking.URL, out, 100)
	cfg.Concurrency = 2
	cfg.ChunkSize = 50

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = dl.Download(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Download = %v, want context.DeadlineExceeded", err)
	}
}

func TestDownloadEmptyFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig("http://example.invalid/never-called", out, 0)

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("Stat = %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("size = %d, want 0", info.Size())
	}
}

func TestNewInvalidConfig(t *testing.T) {
	valid := Config{
		URL:         "http://example.invalid",
		OutputPath:  "out.bin",
		TotalSize:   10,
		Concurrency: 1,
		ChunkSize:   10,
		Timeout:     time.Second,
		MaxRetries:  1,
	}

	cases := map[string]func(*Config){
		"empty URL":        func(c *Config) { c.URL = "" },
		"empty output":     func(c *Config) { c.OutputPath = "" },
		"negative size":    func(c *Config) { c.TotalSize = -1 },
		"zero concurrency": func(c *Config) { c.Concurrency = 0 },
		"zero chunk":       func(c *Config) { c.ChunkSize = 0 },
		"negative timeout": func(c *Config) { c.Timeout = -time.Second },
		"negative retries": func(c *Config) { c.MaxRetries = -1 },
		"min without max":  func(c *Config) { c.MinConcurrency = 2 },
		"min below one":    func(c *Config) { c.MaxConcurrency = 3 },
		"min above max":    func(c *Config) { c.MinConcurrency = 5; c.MaxConcurrency = 3 },
		"negative max":     func(c *Config) { c.MaxConcurrency = -1 },
		"negative min":     func(c *Config) { c.MinConcurrency = -1 },

		"chunk min without max":     func(c *Config) { c.MinChunkSize = 32 },
		"chunk initial without max": func(c *Config) { c.InitialChunkSize = 32 },
		"chunk step without max":    func(c *Config) { c.ChunkSizeStep = 32 },
		"chunk min zero": func(c *Config) {
			c.MaxChunkSize = 100
			c.MinChunkSize = 0
			c.InitialChunkSize = 50
			c.ChunkSizeStep = 10
		},
		"chunk initial out of range": func(c *Config) {
			c.MaxChunkSize = 100
			c.MinChunkSize = 50
			c.InitialChunkSize = 200
			c.ChunkSizeStep = 10
		},
		"chunk zero step": func(c *Config) { c.MaxChunkSize = 100; c.MinChunkSize = 50; c.InitialChunkSize = 50 },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := valid
			mutate(&cfg)
			if _, err := New(cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("New = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

// assertRangesCover checks that the successful records' inclusive ranges tile
// [0, size) exactly using the given fixed chunk size.
func assertRangesCover(t *testing.T, records []metrics.Record, size, chunk int64) {
	t.Helper()
	sorted := append([]metrics.Record(nil), records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].RangeStart < sorted[j].RangeStart })

	next := int64(0)
	for _, r := range sorted {
		if r.RangeStart != next {
			t.Fatalf("range gap/overlap at %d: %+v", next, r)
		}
		wantEnd := r.RangeStart + chunk - 1
		if wantEnd >= size {
			wantEnd = size - 1
		}
		if r.RangeEnd != wantEnd {
			t.Fatalf("range end = %d, want %d for %+v", r.RangeEnd, wantEnd, r)
		}
		if r.Bytes != r.RangeEnd-r.RangeStart+1 {
			t.Fatalf("record bytes = %d, want %d for %+v", r.Bytes, r.RangeEnd-r.RangeStart+1, r)
		}
		next = r.RangeEnd + 1
	}
	if next != size {
		t.Fatalf("covered %d bytes, want %d", next, size)
	}
}

func TestDownloadMetricsSuccess(t *testing.T) {
	content := pattern(1000)
	_, srv := newTestServer(content)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 64

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	snap := dl.Metrics()
	wantChunks := int64((len(content) + 63) / 64)
	if int64(len(snap.Records)) != wantChunks {
		t.Fatalf("len(Records) = %d, want %d", len(snap.Records), wantChunks)
	}
	agg := snap.Aggregate
	if agg.TotalRequests != wantChunks || agg.TotalAttempts != wantChunks {
		t.Fatalf("TotalRequests = %d, TotalAttempts = %d, want %d", agg.TotalRequests, agg.TotalAttempts, wantChunks)
	}
	if agg.SuccessfulRequests != wantChunks || agg.FailedRequests != 0 {
		t.Fatalf("Successful = %d, Failed = %d, want %d, 0", agg.SuccessfulRequests, agg.FailedRequests, wantChunks)
	}
	if agg.TotalBytes != int64(len(content)) {
		t.Fatalf("TotalBytes = %d, want %d", agg.TotalBytes, len(content))
	}
	for _, r := range snap.Records {
		if !r.Success || r.Attempt != 1 || r.StatusCode != http.StatusPartialContent {
			t.Fatalf("unexpected record: %+v", r)
		}
	}
	assertRangesCover(t, snap.Records, int64(len(content)), 64)
}

func TestDownloadMetricsConcurrentWorkers(t *testing.T) {
	content := pattern(2000)
	_, srv := newTestServer(content)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 8
	cfg.ChunkSize = 32

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	snap := dl.Metrics()
	wantChunks := int64((len(content) + 31) / 32)
	if agg := snap.Aggregate; agg.TotalAttempts != wantChunks || agg.SuccessfulRequests != wantChunks || agg.FailedRequests != 0 {
		t.Fatalf("aggregate = %+v, want %d successful attempts", agg, wantChunks)
	}
	if agg := snap.Aggregate; agg.TotalBytes != int64(len(content)) {
		t.Fatalf("TotalBytes = %d, want %d", agg.TotalBytes, len(content))
	}
	assertRangesCover(t, snap.Records, int64(len(content)), 32)
}

func TestDownloadMetricsFailedAttemptThenSuccess(t *testing.T) {
	content := pattern(100)
	rs, srv := newTestServer(content)
	t.Cleanup(srv.Close)
	rs.failFor[0] = 1

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 100
	cfg.MaxRetries = 3

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	snap := dl.Metrics()
	if len(snap.Records) != 2 {
		t.Fatalf("len(Records) = %d, want 2", len(snap.Records))
	}
	byAttempt := map[int]metrics.Record{}
	for _, r := range snap.Records {
		byAttempt[r.Attempt] = r
	}
	first, ok := byAttempt[1]
	if !ok || first.Success || first.StatusCode != http.StatusInternalServerError {
		t.Fatalf("attempt 1 = %+v, want failed 500", first)
	}
	second, ok := byAttempt[2]
	if !ok || !second.Success || second.StatusCode != http.StatusPartialContent {
		t.Fatalf("attempt 2 = %+v, want successful 206", second)
	}
	for _, r := range snap.Records {
		if r.RangeStart != 0 || r.RangeEnd != 99 {
			t.Fatalf("record range = [%d,%d], want [0,99]", r.RangeStart, r.RangeEnd)
		}
	}
	agg := snap.Aggregate
	if agg.TotalAttempts != 2 || agg.SuccessfulRequests != 1 || agg.FailedRequests != 1 {
		t.Fatalf("aggregate = %+v, want 2 attempts, 1 success, 1 failure", agg)
	}
	if second.Bytes != 100 {
		t.Fatalf("successful attempt bytes = %d, want 100", second.Bytes)
	}
	// TotalBytes counts bytes read on every attempt, including error bodies.
	var sum int64
	for _, r := range snap.Records {
		sum += r.Bytes
	}
	if agg.TotalBytes != sum {
		t.Fatalf("TotalBytes = %d, want %d (sum of records)", agg.TotalBytes, sum)
	}
}

func TestDownloadMetricsRetryLimitExceeded(t *testing.T) {
	content := pattern(100)
	rs, srv := newTestServer(content)
	t.Cleanup(srv.Close)
	rs.failFor[0] = 1000

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 100
	cfg.MaxRetries = 2

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); !errors.Is(err, ErrRetryLimitExceeded) {
		t.Fatalf("Download = %v, want ErrRetryLimitExceeded", err)
	}

	snap := dl.Metrics()
	if len(snap.Records) != 3 {
		t.Fatalf("len(Records) = %d, want 3", len(snap.Records))
	}
	attempts := []int{}
	for _, r := range snap.Records {
		if r.Success {
			t.Fatalf("record unexpectedly succeeded: %+v", r)
		}
		attempts = append(attempts, r.Attempt)
	}
	sort.Ints(attempts)
	if fmt.Sprint(attempts) != "[1 2 3]" {
		t.Fatalf("attempt numbers = %v, want [1 2 3]", attempts)
	}
	agg := snap.Aggregate
	if agg.TotalAttempts != 3 || agg.FailedRequests != 3 || agg.SuccessfulRequests != 0 {
		t.Fatalf("aggregate = %+v, want 3 failed attempts", agg)
	}
}

func TestDownloadMetricsOutOfOrder(t *testing.T) {
	content := pattern(1000)
	rs, srv := newTestServer(content)
	t.Cleanup(srv.Close)
	rs.delay[0] = 150 * time.Millisecond

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 4
	cfg.ChunkSize = 100

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}
	assertFileEquals(t, out, content)

	snap := dl.Metrics()
	if agg := snap.Aggregate; agg.TotalAttempts != 10 || agg.SuccessfulRequests != 10 || agg.TotalBytes != 1000 {
		t.Fatalf("aggregate = %+v, want 10 successful attempts / 1000 bytes", agg)
	}
	assertRangesCover(t, snap.Records, int64(len(content)), 100)
}

func TestDownloadMetricsContextCancellation(t *testing.T) {
	blocking := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(blocking.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(blocking.URL, out, 100)
	cfg.Concurrency = 2
	cfg.ChunkSize = 50

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := dl.Download(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Download = %v, want context.DeadlineExceeded", err)
	}

	snap := dl.Metrics()
	if len(snap.Records) == 0 {
		t.Fatal("no metrics recorded for cancelled run")
	}
	for _, r := range snap.Records {
		if r.Success {
			t.Fatalf("record unexpectedly succeeded: %+v", r)
		}
		if r.Attempt != 1 {
			t.Fatalf("attempt = %d, want 1 for %+v", r.Attempt, r)
		}
		if r.RangeStart < 0 || r.RangeEnd >= 100 {
			t.Fatalf("record range out of bounds: %+v", r)
		}
	}
	if snap.Aggregate.FailedRequests != int64(len(snap.Records)) {
		t.Fatalf("FailedRequests = %d, want %d", snap.Aggregate.FailedRequests, len(snap.Records))
	}
}

func TestDownloadMetricsSnapshotIsolation(t *testing.T) {
	content := pattern(100)
	_, srv := newTestServer(content)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 100

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	first := dl.Metrics()
	if len(first.Records) != 1 {
		t.Fatalf("len(Records) = %d, want 1", len(first.Records))
	}
	first.Records[0].Bytes = 9999
	first.Aggregate.TotalBytes = 9999

	second := dl.Metrics()
	if second.Aggregate.TotalBytes != 100 {
		t.Fatalf("TotalBytes = %d, want 100", second.Aggregate.TotalBytes)
	}
	if second.Records[0].Bytes != 100 {
		t.Fatalf("record bytes = %d, want 100", second.Records[0].Bytes)
	}
}

func TestMetricsBeforeDownload(t *testing.T) {
	cfg := baseConfig("http://example.invalid/never-called", filepath.Join(t.TempDir(), "out.bin"), 10)
	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	snap := dl.Metrics()
	if len(snap.Records) != 0 || snap.Aggregate.TotalRequests != 0 {
		t.Fatalf("expected empty metrics before Download, got %+v", snap.Aggregate)
	}
}

// gatedServer is a deterministic range server that blocks each request until
// released, so tests can observe and control concurrency without sleeps.
type gatedServer struct {
	content []byte

	mu        sync.Mutex
	changed   chan struct{}
	active    int
	maxActive int
	open      bool
	pending   []chan struct{}
	requests  [][2]int64
	attempts  int64

	started chan int64
}

func newGatedServer(content []byte) *gatedServer {
	return &gatedServer{
		content: content,
		changed: make(chan struct{}),
		started: make(chan int64, 4096),
	}
}

// broadcastLocked wakes anyone waiting on a state change. Callers hold mu.
func (s *gatedServer) broadcastLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *gatedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start, end, ok := parseRangeHeader(r.Header.Get("Range"))
	if !ok || start < 0 || start >= int64(len(s.content)) || end < start {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if end >= int64(len(s.content)) {
		end = int64(len(s.content)) - 1
	}

	s.mu.Lock()
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.attempts++
	s.requests = append(s.requests, [2]int64{start, end})
	var gate chan struct{}
	if !s.open {
		gate = make(chan struct{})
		s.pending = append(s.pending, gate)
	}
	s.broadcastLocked()
	s.mu.Unlock()

	s.started <- start

	if gate != nil {
		<-gate
	}

	body := s.content[start : end+1]
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.content)))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)

	s.mu.Lock()
	s.active--
	s.broadcastLocked()
	s.mu.Unlock()
}

// releaseCurrent unblocks the requests currently waiting, but keeps the gate
// closed so future requests still block.
func (s *gatedServer) releaseCurrent() {
	s.mu.Lock()
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()
	for _, gate := range pending {
		close(gate)
	}
}

// releaseAll opens the gate permanently.
func (s *gatedServer) releaseAll() {
	s.mu.Lock()
	s.open = true
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()
	for _, gate := range pending {
		close(gate)
	}
}

func (s *gatedServer) waitForActive(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		s.mu.Lock()
		got := s.active
		if got == n {
			s.mu.Unlock()
			return
		}
		ch := s.changed
		s.mu.Unlock()
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("timed out waiting for active=%d (got %d)", n, got)
		}
	}
}

func (s *gatedServer) waitForStarted(t *testing.T) {
	t.Helper()
	select {
	case <-s.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a request to start")
	}
}

func (s *gatedServer) snapshot() (maxActive int, attempts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxActive, s.attempts
}

func (s *gatedServer) requestedRanges() [][2]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]int64(nil), s.requests...)
}

// assertRangesExact checks there are no duplicate requests and that the ranges
// tile [0, size) exactly using the fixed chunk size.
func assertRangesExact(t *testing.T, ranges [][2]int64, size, chunk int64) {
	t.Helper()
	seen := make(map[[2]int64]bool, len(ranges))
	for _, r := range ranges {
		if seen[r] {
			t.Fatalf("duplicate range fetched: %v", r)
		}
		seen[r] = true
	}
	sorted := append([][2]int64(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })

	next := int64(0)
	for _, r := range sorted {
		if r[0] != next {
			t.Fatalf("range gap/overlap at %d: %v", next, r)
		}
		wantEnd := r[0] + chunk - 1
		if wantEnd >= size {
			wantEnd = size - 1
		}
		if r[1] != wantEnd {
			t.Fatalf("range end = %d, want %d for %v", r[1], wantEnd, r)
		}
		next = r[1] + 1
	}
	if next != size {
		t.Fatalf("covered %d bytes, want %d", next, size)
	}
}

func TestAdaptiveIncreaseConcurrency(t *testing.T) {
	content := pattern(1000)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 3
	cfg.ChunkSize = 100

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if got := dl.Concurrency(); got != 1 {
		t.Fatalf("initial Concurrency() = %d, want 1", got)
	}

	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	gs.waitForStarted(t)
	gs.waitForActive(t, 1)

	if got := dl.SetConcurrency(3); got != 3 {
		t.Fatalf("SetConcurrency(3) = %d, want 3", got)
	}
	gs.waitForActive(t, 3)

	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	assertFileEquals(t, out, content)

	maxActive, attempts := gs.snapshot()
	if maxActive != 3 {
		t.Fatalf("maxActive = %d, want 3", maxActive)
	}
	snap := dl.Metrics()
	if agg := snap.Aggregate; agg.TotalAttempts != 10 || agg.SuccessfulRequests != 10 || agg.FailedRequests != 0 {
		t.Fatalf("aggregate = %+v, want 10 successful attempts", agg)
	}
	if attempts != snap.Aggregate.TotalAttempts {
		t.Fatalf("server attempts = %d, metrics attempts = %d", attempts, snap.Aggregate.TotalAttempts)
	}
	assertRangesExact(t, gs.requestedRanges(), int64(len(content)), 100)
}

func TestAdaptiveDecreaseConcurrency(t *testing.T) {
	content := pattern(1000)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 3
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 3
	cfg.ChunkSize = 100

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	gs.waitForActive(t, 3)

	if got := dl.SetConcurrency(1); got != 1 {
		t.Fatalf("SetConcurrency(1) = %d, want 1", got)
	}
	// Release the in-flight requests; they must complete, not be cancelled.
	gs.releaseCurrent()
	// Workers drain to the new target of one.
	gs.waitForActive(t, 1)

	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	assertFileEquals(t, out, content)

	snap := dl.Metrics()
	if agg := snap.Aggregate; agg.TotalAttempts != 10 || agg.SuccessfulRequests != 10 || agg.FailedRequests != 0 {
		t.Fatalf("aggregate = %+v, want 10 successful attempts (no cancelled in-flight)", agg)
	}
	maxActive, attempts := gs.snapshot()
	if maxActive > 3 {
		t.Fatalf("maxActive = %d, want <= 3", maxActive)
	}
	if attempts != snap.Aggregate.TotalAttempts {
		t.Fatalf("server attempts = %d, metrics attempts = %d", attempts, snap.Aggregate.TotalAttempts)
	}
	assertRangesExact(t, gs.requestedRanges(), int64(len(content)), 100)
}

func TestAdaptiveIncreaseDecreaseMultipleTimes(t *testing.T) {
	content := pattern(1000)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 3
	cfg.ChunkSize = 100

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	gs.waitForActive(t, 1)

	dl.SetConcurrency(3)
	gs.waitForActive(t, 3)

	dl.SetConcurrency(1)
	gs.releaseCurrent()
	gs.waitForActive(t, 1)

	dl.SetConcurrency(2)
	gs.waitForActive(t, 2)

	dl.SetConcurrency(1)
	gs.releaseCurrent()
	gs.waitForActive(t, 1)

	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	assertFileEquals(t, out, content)

	snap := dl.Metrics()
	if agg := snap.Aggregate; agg.TotalAttempts != 10 || agg.SuccessfulRequests != 10 || agg.FailedRequests != 0 {
		t.Fatalf("aggregate = %+v, want 10 successful attempts", agg)
	}
	assertRangesExact(t, gs.requestedRanges(), int64(len(content)), 100)
}

func TestFixedConcurrencyIgnoresSetConcurrency(t *testing.T) {
	content := pattern(300)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 2 // no min/max: fixed mode
	cfg.ChunkSize = 64

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if got := dl.SetConcurrency(8); got != 2 {
		t.Fatalf("SetConcurrency(8) = %d, want 2 in fixed mode", got)
	}

	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	gs.waitForActive(t, 2)
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	assertFileEquals(t, out, content)
	maxActive, _ := gs.snapshot()
	if maxActive != 2 {
		t.Fatalf("maxActive = %d, want 2 in fixed mode", maxActive)
	}
}

func TestSetConcurrencyClampedToBounds(t *testing.T) {
	cfg := baseConfig("http://example.invalid/never-called", filepath.Join(t.TempDir(), "out.bin"), 100)
	cfg.Concurrency = 2
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if got := dl.SetConcurrency(100); got != 4 {
		t.Fatalf("SetConcurrency(100) = %d, want 4 (max)", got)
	}
	if got := dl.SetConcurrency(0); got != 1 {
		t.Fatalf("SetConcurrency(0) = %d, want 1 (min)", got)
	}
}

// testPolicy is a controllable Policy used to exercise the integration.
type testPolicy struct {
	mu       sync.Mutex
	contexts []policy.Context
	updates  int

	selectFn func(policy.Context) policy.Action

	inFlight    int32
	sawParallel int32
}

func (p *testPolicy) SelectAction(ctx policy.Context) policy.Action {
	if atomic.AddInt32(&p.inFlight, 1) > 1 {
		atomic.StoreInt32(&p.sawParallel, 1)
	}
	defer atomic.AddInt32(&p.inFlight, -1)

	p.mu.Lock()
	p.contexts = append(p.contexts, ctx)
	p.mu.Unlock()

	if p.selectFn != nil {
		return p.selectFn(ctx)
	}
	return policy.NoChange()
}

func (p *testPolicy) Update(policy.Context, policy.Action, float64) {
	p.mu.Lock()
	p.updates++
	p.mu.Unlock()
}

func (p *testPolicy) snapshot() (contexts []policy.Context, updates int, sawParallel bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]policy.Context(nil), p.contexts...), p.updates, atomic.LoadInt32(&p.sawParallel) == 1
}

// throughputServer serves ranges with a per-request delay controlled by the
// test, letting it produce deterministic throughput phases.
type throughputServer struct {
	content  []byte
	delayFor func(n int) time.Duration

	mu       sync.Mutex
	count    int
	requests [][2]int64
}

func (s *throughputServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start, end, ok := parseRangeHeader(r.Header.Get("Range"))
	if !ok || start < 0 || start >= int64(len(s.content)) || end < start {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if end >= int64(len(s.content)) {
		end = int64(len(s.content)) - 1
	}

	s.mu.Lock()
	s.count++
	n := s.count
	s.requests = append(s.requests, [2]int64{start, end})
	delay := time.Duration(0)
	if s.delayFor != nil {
		delay = s.delayFor(n)
	}
	s.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}

	body := s.content[start : end+1]
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.content)))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)
}

func (s *throughputServer) requestedRanges() [][2]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]int64(nil), s.requests...)
}

func (s *throughputServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func newThroughputPolicy(t *testing.T, min, max int, improve, degrade float64) policy.Policy {
	t.Helper()
	p, err := policy.NewThroughputPolicy(policy.ThroughputPolicyConfig{
		MinConcurrency:       min,
		MaxConcurrency:       max,
		ImprovementThreshold: improve,
		DegradationThreshold: degrade,
	})
	if err != nil {
		t.Fatalf("NewThroughputPolicy = %v", err)
	}
	return p
}

func TestPolicyNoneKeepsFixedBehavior(t *testing.T) {
	content := pattern(1000)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 3 // fixed mode, no policy

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if dl.cfg.Policy != nil {
		t.Fatal("expected no policy configured")
	}

	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()
	gs.waitForActive(t, 3)
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	assertFileEquals(t, out, content)
	if got := dl.Concurrency(); got != 3 {
		t.Fatalf("Concurrency() = %d, want 3 (fixed)", got)
	}
	if maxActive, _ := gs.snapshot(); maxActive != 3 {
		t.Fatalf("maxActive = %d, want 3", maxActive)
	}
}

func TestPolicyFixedPolicyKeepsBaseline(t *testing.T) {
	content := pattern(1000)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 2
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4
	cfg.ChunkSize = 100
	cfg.Policy = policy.FixedPolicy{}

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()
	gs.waitForActive(t, 2)
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	assertFileEquals(t, out, content)
	if got := dl.Concurrency(); got != 2 {
		t.Fatalf("Concurrency() = %d, want 2 (FixedPolicy)", got)
	}
	if maxActive, _ := gs.snapshot(); maxActive != 2 {
		t.Fatalf("maxActive = %d, want 2", maxActive)
	}
}

func TestPolicyThroughputIncrease(t *testing.T) {
	content := pattern(600)
	ts := &throughputServer{content: content, delayFor: func(n int) time.Duration {
		if n <= 4 {
			return 40 * time.Millisecond
		}
		return 2 * time.Millisecond
	}}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4
	cfg.ChunkSize = 50
	cfg.PolicyWindowSize = 4
	cfg.Policy = newThroughputPolicy(t, 1, 4, 0.2, 0.2)

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	assertFileEquals(t, out, content)

	got := dl.Concurrency()
	if got < 2 {
		t.Fatalf("Concurrency() = %d, want >= 2 after throughput increase", got)
	}
	if got > 4 {
		t.Fatalf("Concurrency() = %d, want <= 4", got)
	}
	snap := dl.Metrics()
	if agg := snap.Aggregate; agg.TotalAttempts != 12 || agg.SuccessfulRequests != 12 || agg.FailedRequests != 0 {
		t.Fatalf("aggregate = %+v, want 12 successful attempts", agg)
	}
	if int64(ts.requestCount()) != snap.Aggregate.TotalAttempts {
		t.Fatalf("server requests = %d, metrics attempts = %d", ts.requestCount(), snap.Aggregate.TotalAttempts)
	}
	assertRangesExact(t, ts.requestedRanges(), int64(len(content)), 50)
}

func TestPolicyThroughputDecrease(t *testing.T) {
	content := pattern(600)
	ts := &throughputServer{content: content, delayFor: func(n int) time.Duration {
		if n <= 4 {
			return 10 * time.Millisecond
		}
		return 50 * time.Millisecond
	}}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 2
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4
	cfg.ChunkSize = 50
	cfg.PolicyWindowSize = 4
	cfg.Policy = newThroughputPolicy(t, 1, 4, 0.5, 0.2)

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	assertFileEquals(t, out, content)

	if got := dl.Concurrency(); got >= 2 {
		t.Fatalf("Concurrency() = %d, want < 2 after throughput decrease", got)
	}
	snap := dl.Metrics()
	if agg := snap.Aggregate; agg.TotalAttempts != 12 || agg.SuccessfulRequests != 12 || agg.FailedRequests != 0 {
		t.Fatalf("aggregate = %+v, want 12 successful attempts", agg)
	}
	assertRangesExact(t, ts.requestedRanges(), int64(len(content)), 50)
}

func TestPolicyActionsRespectBounds(t *testing.T) {
	t.Run("increase clamps at max", func(t *testing.T) {
		content := pattern(500)
		ts := &throughputServer{content: content}
		srv := httptest.NewServer(ts)
		t.Cleanup(srv.Close)

		out := filepath.Join(t.TempDir(), "out.bin")
		cfg := baseConfig(srv.URL, out, int64(len(content)))
		cfg.Concurrency = 1
		cfg.MinConcurrency = 1
		cfg.MaxConcurrency = 3
		cfg.ChunkSize = 50
		cfg.Policy = &testPolicy{selectFn: func(policy.Context) policy.Action {
			return policy.IncreaseConcurrency()
		}}

		dl, err := New(cfg)
		if err != nil {
			t.Fatalf("New = %v", err)
		}
		if err := dl.Download(context.Background()); err != nil {
			t.Fatalf("Download = %v, want nil", err)
		}
		assertFileEquals(t, out, content)
		if got := dl.Concurrency(); got != 3 {
			t.Fatalf("Concurrency() = %d, want 3 (max)", got)
		}
	})

	t.Run("decrease clamps at min", func(t *testing.T) {
		content := pattern(500)
		ts := &throughputServer{content: content}
		srv := httptest.NewServer(ts)
		t.Cleanup(srv.Close)

		out := filepath.Join(t.TempDir(), "out.bin")
		cfg := baseConfig(srv.URL, out, int64(len(content)))
		cfg.Concurrency = 3
		cfg.MinConcurrency = 1
		cfg.MaxConcurrency = 3
		cfg.ChunkSize = 50
		cfg.Policy = &testPolicy{selectFn: func(policy.Context) policy.Action {
			return policy.DecreaseConcurrency()
		}}

		dl, err := New(cfg)
		if err != nil {
			t.Fatalf("New = %v", err)
		}
		if err := dl.Download(context.Background()); err != nil {
			t.Fatalf("Download = %v, want nil", err)
		}
		assertFileEquals(t, out, content)
		if got := dl.Concurrency(); got != 1 {
			t.Fatalf("Concurrency() = %d, want 1 (min)", got)
		}
	})
}

func TestPolicyChunkActionsIgnored(t *testing.T) {
	content := pattern(500)
	ts := &throughputServer{content: content}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 2
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4
	cfg.ChunkSize = 50
	cfg.Policy = &testPolicy{selectFn: func(policy.Context) policy.Action {
		return policy.IncreaseChunkSize()
	}}

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}
	assertFileEquals(t, out, content)

	// Chunk size must be unchanged: every requested range is exactly 50 bytes.
	assertRangesExact(t, ts.requestedRanges(), int64(len(content)), 50)
	if got := dl.Concurrency(); got != 2 {
		t.Fatalf("Concurrency() = %d, want 2 (chunk action must not change concurrency)", got)
	}
}

func TestPolicyDecisionsAreSerialized(t *testing.T) {
	content := pattern(2000)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 8
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 8
	cfg.ChunkSize = 50
	rec := &testPolicy{}
	cfg.Policy = rec

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()
	gs.waitForActive(t, 8)
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	assertFileEquals(t, out, content)
	contexts, updates, sawParallel := rec.snapshot()
	if sawParallel {
		t.Fatal("SelectAction was called concurrently; policy calls are not serialized")
	}
	// Correct credit assignment: each observed outcome rewards the PREVIOUS
	// action, so the first SelectAction has no matching Update. Update count is
	// therefore SelectAction count - 1.
	if updates != len(contexts)-1 {
		t.Fatalf("Update calls = %d, SelectAction calls = %d; want Update == SelectAction-1", updates, len(contexts))
	}
	if len(contexts) != 40 {
		t.Fatalf("SelectAction calls = %d, want 40 (one per attempt)", len(contexts))
	}
}

// TestPolicyRunnerAggregateThroughput proves the aggregate-throughput
// observable uses a WALL-CLOCK span rather than summed request durations: two
// identical samples produce the same per-request throughput but a different
// aggregate depending on whether they overlap in wall-clock time. This is the
// observable that makes concurrency visible to the learner.
func TestPolicyRunnerAggregateThroughput(t *testing.T) {
	newRunner := func() (*policyRunner, *testPolicy, func(time.Duration)) {
		conc, err := concurrency.New(1, 4)
		if err != nil {
			t.Fatalf("concurrency.New = %v", err)
		}
		rec := &testPolicy{}
		r := newPolicyRunner(rec, conc, nil, 64, 4, nil, nil)
		now := time.Unix(0, 0)
		r.now = func() time.Time { return now }
		return r, rec, func(d time.Duration) { now = now.Add(d) }
	}
	rec := metrics.Record{Bytes: 1000, Duration: time.Second, Success: true, Attempt: 1}

	// Sequential: the clock advances by each request's duration.
	seq, seqRec, advance := newRunner()
	seq.observe(rec)
	advance(time.Second)
	seq.observe(rec)
	seqCtxs, _, _ := seqRec.snapshot()

	// Overlapping: both requests complete at the same wall-clock instant.
	over, overRec, _ := newRunner()
	over.observe(rec)
	over.observe(rec)
	overCtxs, _, _ := overRec.snapshot()

	if seqCtxs[1].Throughput != overCtxs[1].Throughput {
		t.Fatalf("per-request throughput should be equal: seq=%v overlap=%v",
			seqCtxs[1].Throughput, overCtxs[1].Throughput)
	}
	// First observation: a single sample makes aggregate == per-request.
	if math.Abs(seqCtxs[0].AggregateThroughput-1000) > 1e-9 {
		t.Fatalf("first aggregate = %v, want 1000", seqCtxs[0].AggregateThroughput)
	}
	// Sequential: 2000 bytes over 2s = 1000 B/s.
	if math.Abs(seqCtxs[1].AggregateThroughput-1000) > 1e-9 {
		t.Fatalf("sequential aggregate = %v, want 1000", seqCtxs[1].AggregateThroughput)
	}
	// Overlapping: 2000 bytes over 1s = 2000 B/s.
	if math.Abs(overCtxs[1].AggregateThroughput-2000) > 1e-9 {
		t.Fatalf("overlapping aggregate = %v, want 2000", overCtxs[1].AggregateThroughput)
	}
}

func TestPolicyContextPopulated(t *testing.T) {
	content := pattern(500)
	ts := &throughputServer{content: content}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 2
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4
	cfg.ChunkSize = 50
	cfg.PolicyWindowSize = 4
	rec := &testPolicy{}
	cfg.Policy = rec

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v, want nil", err)
	}

	contexts, updates, _ := rec.snapshot()
	if len(contexts) != 10 {
		t.Fatalf("SelectAction calls = %d, want 10", len(contexts))
	}
	// One Update per observed outcome, rewarding the previous action: the first
	// SelectAction has no prior outcome to be credited for.
	if updates != len(contexts)-1 {
		t.Fatalf("Update calls = %d, want %d (SelectAction-1)", updates, len(contexts)-1)
	}

	last := contexts[len(contexts)-1]
	if last.ChunkSize != 50 {
		t.Errorf("ChunkSize = %d, want 50", last.ChunkSize)
	}
	if last.Concurrency < 1 {
		t.Errorf("Concurrency = %d, want >= 1", last.Concurrency)
	}
	if last.TotalAttempts != int64(len(contexts)) {
		t.Errorf("TotalAttempts = %d, want %d", last.TotalAttempts, len(contexts))
	}
	if last.SuccessfulRequests != 10 || last.FailedRequests != 0 {
		t.Errorf("success/fail = %d/%d, want 10/0", last.SuccessfulRequests, last.FailedRequests)
	}
	if last.BytesTransferred != 50 {
		t.Errorf("BytesTransferred = %d, want 50", last.BytesTransferred)
	}
	if last.RequestDuration <= 0 {
		t.Errorf("RequestDuration = %v, want > 0", last.RequestDuration)
	}
	if last.Throughput <= 0 {
		t.Errorf("Throughput = %v, want > 0", last.Throughput)
	}
	if last.AggregateThroughput <= 0 {
		t.Errorf("AggregateThroughput = %v, want > 0", last.AggregateThroughput)
	}
}

// adaptiveChunkConfig returns a config with a dynamic chunk-size controller.
func adaptiveChunkConfig(url, out string, size int64) Config {
	cfg := baseConfig(url, out, size)
	cfg.Concurrency = 1
	cfg.ChunkSize = 32 // required > 0; unused in adaptive mode
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 256
	cfg.InitialChunkSize = 32
	cfg.ChunkSizeStep = 32
	return cfg
}

func rangeLengths(ranges [][2]int64) []int64 {
	out := make([]int64, len(ranges))
	for i, r := range ranges {
		out[i] = r[1] - r[0] + 1
	}
	return out
}

// assertRangesTile checks the ranges are duplicate-free and exactly cover
// [0, size), without assuming a fixed chunk size.
func assertRangesTile(t *testing.T, ranges [][2]int64, size int64) {
	t.Helper()
	sorted := append([][2]int64(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })
	seen := make(map[[2]int64]bool, len(sorted))
	next := int64(0)
	for _, r := range sorted {
		if seen[r] {
			t.Fatalf("duplicate range %v", r)
		}
		seen[r] = true
		if r[0] != next {
			t.Fatalf("range gap/overlap at %d: %v", next, r)
		}
		next = r[1] + 1
	}
	if next != size {
		t.Fatalf("covered %d bytes, want %d", next, size)
	}
}

func equalInt64s(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestChunkSizeFixedMode(t *testing.T) {
	content := pattern(400)
	ts := &throughputServer{content: content}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 50 // no adaptive chunk config

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}
	assertFileEquals(t, out, content)
	assertRangesExact(t, ts.requestedRanges(), int64(len(content)), 50)

	if got := dl.ChunkSize(); got != 50 {
		t.Fatalf("ChunkSize() = %d, want 50", got)
	}
	// Fixed mode: SetChunkSize is a no-op.
	if got := dl.SetChunkSize(999); got != 50 {
		t.Fatalf("SetChunkSize(999) in fixed mode = %d, want 50", got)
	}
}

func TestChunkSizeDynamic(t *testing.T) {
	content := pattern(400)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := adaptiveChunkConfig(srv.URL, out, int64(len(content)))

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	gs.waitForStarted(t) // first range allocated at the initial 32
	if got := dl.ChunkSize(); got != 32 {
		t.Fatalf("initial ChunkSize() = %d, want 32", got)
	}

	dl.SetChunkSize(64)
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	ranges := gs.requestedRanges()
	assertRangesTile(t, ranges, int64(len(content)))
	if got, want := rangeLengths(ranges), []int64{32, 64, 64, 64, 64, 64, 48}; !equalInt64s(got, want) {
		t.Fatalf("range lengths = %v, want %v", got, want)
	}
}

func TestChunkSizeMultipleChanges(t *testing.T) {
	content := pattern(256)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := adaptiveChunkConfig(srv.URL, out, int64(len(content)))

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	gs.waitForStarted(t) // 32
	dl.SetChunkSize(64)
	gs.releaseCurrent()
	gs.waitForStarted(t) // 64
	dl.SetChunkSize(96)
	gs.releaseCurrent()
	gs.waitForStarted(t) // 96
	dl.SetChunkSize(32)
	gs.releaseCurrent()
	gs.waitForStarted(t) // 32
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	ranges := gs.requestedRanges()
	assertRangesTile(t, ranges, int64(len(content)))
	if got, want := rangeLengths(ranges), []int64{32, 64, 96, 32, 32}; !equalInt64s(got, want) {
		t.Fatalf("range lengths = %v, want %v", got, want)
	}
}

func TestChunkSizeInFlightImmutable(t *testing.T) {
	content := pattern(128)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := adaptiveChunkConfig(srv.URL, out, int64(len(content)))

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	gs.waitForStarted(t) // [0,31] allocated at 32, in flight
	dl.SetChunkSize(64)  // change while it is in flight
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	ranges := gs.requestedRanges()
	assertRangesTile(t, ranges, int64(len(content)))
	if ranges[0] != [2]int64{0, 31} {
		t.Fatalf("in-flight range = %v, want [0 31] (must not be resized)", ranges[0])
	}
	if got, want := rangeLengths(ranges), []int64{32, 64, 32}; !equalInt64s(got, want) {
		t.Fatalf("range lengths = %v, want %v", got, want)
	}

	// Metrics naturally show the different range sizes.
	seen := map[int64]bool{}
	for _, rec := range dl.Metrics().Records {
		seen[rec.RangeEnd-rec.RangeStart+1] = true
	}
	if !seen[32] || !seen[64] {
		t.Fatalf("metrics range sizes = %v, want both 32 and 64", seen)
	}
}

func TestChunkSizeBounds(t *testing.T) {
	cfg := baseConfig("http://example.invalid/never-called", filepath.Join(t.TempDir(), "out.bin"), 100)
	cfg.ChunkSize = 100
	cfg.MinChunkSize = 50
	cfg.MaxChunkSize = 200
	cfg.InitialChunkSize = 100
	cfg.ChunkSizeStep = 50

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if got := dl.SetChunkSize(1); got != 50 {
		t.Fatalf("SetChunkSize(1) = %d, want 50 (min)", got)
	}
	if got := dl.SetChunkSize(1 << 20); got != 200 {
		t.Fatalf("SetChunkSize(1<<20) = %d, want 200 (max)", got)
	}
	if got := dl.SetChunkSize(150); got != 150 {
		t.Fatalf("SetChunkSize(150) = %d, want 150", got)
	}
	if got := dl.ChunkSize(); got != 150 {
		t.Fatalf("ChunkSize() = %d, want 150", got)
	}
}

func TestChunkSizeFinalPartial(t *testing.T) {
	content := pattern(250)
	ts := &throughputServer{content: content}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 100
	cfg.MinChunkSize = 100
	cfg.MaxChunkSize = 200
	cfg.InitialChunkSize = 100
	cfg.ChunkSizeStep = 100

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}
	assertFileEquals(t, out, content)
	ranges := ts.requestedRanges()
	assertRangesTile(t, ranges, int64(len(content)))
	if got, want := rangeLengths(ranges), []int64{100, 100, 50}; !equalInt64s(got, want) {
		t.Fatalf("range lengths = %v, want %v", got, want)
	}
}

func TestChunkSizeConcurrencyIndependence(t *testing.T) {
	content := pattern(400)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := adaptiveChunkConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 3
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 3

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	gs.waitForActive(t, 3)
	dl.SetChunkSize(128)
	if got := dl.Concurrency(); got != 3 {
		t.Fatalf("Concurrency() = %d after SetChunkSize, want 3", got)
	}
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	if got := dl.Concurrency(); got != 3 {
		t.Fatalf("final Concurrency() = %d, want 3", got)
	}
	if maxActive, _ := gs.snapshot(); maxActive != 3 {
		t.Fatalf("maxActive = %d, want 3 (chunk changes must not alter workers)", maxActive)
	}
	assertRangesTile(t, gs.requestedRanges(), int64(len(content)))
}

// retryBoundaryServer fails the first request after the test releases it, so
// the test can change the chunk size between the failure and the retry.
type retryBoundaryServer struct {
	content []byte

	mu           sync.Mutex
	requests     [][2]int64
	firstStarted chan struct{}
	releaseFail  chan struct{}
	firstOnce    sync.Once
	releaseOnce  sync.Once
}

func (s *retryBoundaryServer) release() {
	s.releaseOnce.Do(func() { close(s.releaseFail) })
}

func newRetryBoundaryServer(content []byte) *retryBoundaryServer {
	return &retryBoundaryServer{
		content:      content,
		firstStarted: make(chan struct{}),
		releaseFail:  make(chan struct{}),
	}
}

func (s *retryBoundaryServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start, end, ok := parseRangeHeader(r.Header.Get("Range"))
	if !ok || start < 0 || start >= int64(len(s.content)) || end < start {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if end >= int64(len(s.content)) {
		end = int64(len(s.content)) - 1
	}

	s.mu.Lock()
	s.requests = append(s.requests, [2]int64{start, end})
	n := len(s.requests)
	s.mu.Unlock()

	if n == 1 {
		s.firstOnce.Do(func() { close(s.firstStarted) })
		<-s.releaseFail
		http.Error(w, "transient failure", http.StatusInternalServerError)
		return
	}

	body := s.content[start : end+1]
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.content)))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)
}

func (s *retryBoundaryServer) requested() [][2]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]int64(nil), s.requests...)
}

func TestChunkSizeRetryPreservesBoundaries(t *testing.T) {
	content := pattern(200)
	rs := newRetryBoundaryServer(content)
	srv := httptest.NewServer(rs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 50
	cfg.MinChunkSize = 50
	cfg.MaxChunkSize = 200
	cfg.InitialChunkSize = 50
	cfg.ChunkSizeStep = 50
	cfg.MaxRetries = 2

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	defer rs.release()

	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	select {
	case <-rs.firstStarted: // first request [0,49] is in flight
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the first request")
	}
	dl.SetChunkSize(100)
	rs.release() // first attempt fails; worker retries the same range
	if err := <-done; err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	got := rs.requested()
	want := [][2]int64{{0, 49}, {0, 49}, {50, 149}, {150, 199}}
	if len(got) != len(want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request %d = %v, want %v (retry must keep original boundaries)", i, got[i], want[i])
		}
	}

	// Metrics: two attempts for start 0, attempt 1 failed, attempt 2 succeeded.
	var attempts []int
	for _, rec := range dl.Metrics().Records {
		if rec.RangeStart == 0 {
			attempts = append(attempts, rec.Attempt)
		}
	}
	sort.Ints(attempts)
	if fmt.Sprint(attempts) != "[1 2]" {
		t.Fatalf("start-0 attempts = %v, want [1 2]", attempts)
	}
}

// scriptedPolicy returns a fixed sequence of actions (cycling) and records the
// Context seen on each decision.
type scriptedPolicy struct {
	mu       sync.Mutex
	actions  []policy.Action
	calls    int
	contexts []policy.Context
}

func (p *scriptedPolicy) SelectAction(ctx policy.Context) policy.Action {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.contexts = append(p.contexts, ctx)
	if len(p.actions) == 0 {
		return policy.NoChange()
	}
	a := p.actions[p.calls%len(p.actions)]
	p.calls++
	return a
}

func (p *scriptedPolicy) Update(policy.Context, policy.Action, float64) {}

func (p *scriptedPolicy) recorded() []policy.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]policy.Context(nil), p.contexts...)
}

// TestPolicyRunnerAppliesActionsIndependently drives the single policy runner
// directly and checks each ActionKind affects only its controller and that the
// next Context observes the updated target.
func TestPolicyRunnerAppliesActionsIndependently(t *testing.T) {
	conc, err := concurrency.New(1, 4)
	if err != nil {
		t.Fatalf("concurrency.New = %v", err)
	}
	conc.Set(2)
	chunk, err := chunksize.New(32, 128, 64, 32)
	if err != nil {
		t.Fatalf("chunksize.New = %v", err)
	}

	var applied []int
	script := []policy.Action{
		policy.IncreaseConcurrency(),
		policy.IncreaseChunkSize(),
		policy.DecreaseConcurrency(),
		policy.DecreaseChunkSize(),
	}
	sp := &scriptedPolicy{actions: script}
	runner := newPolicyRunner(sp, conc, chunk, 64, 4, func(n int) { applied = append(applied, n) }, nil)

	for i := 0; i < len(script); i++ {
		runner.observe(metrics.Record{
			RangeStart: int64(i), RangeEnd: int64(i),
			Bytes: 10, Duration: time.Millisecond, Success: true, Attempt: 1,
		})
	}

	ctxs := sp.recorded()
	if len(ctxs) != 4 {
		t.Fatalf("decisions = %d, want 4", len(ctxs))
	}
	want := []struct {
		conc  int
		chunk int64
	}{
		{2, 64}, // before IncreaseConcurrency
		{3, 64}, // before IncreaseChunkSize
		{3, 96}, // before DecreaseConcurrency
		{2, 96}, // before DecreaseChunkSize
	}
	for i, w := range want {
		if ctxs[i].Concurrency != w.conc || ctxs[i].ChunkSize != w.chunk {
			t.Fatalf("ctx[%d] = {conc %d, chunk %d}, want {%d, %d}", i, ctxs[i].Concurrency, ctxs[i].ChunkSize, w.conc, w.chunk)
		}
	}
	if conc.Current() != 2 {
		t.Fatalf("concurrency = %d, want 2", conc.Current())
	}
	if chunk.Current() != 64 {
		t.Fatalf("chunk size = %d, want 64", chunk.Current())
	}
	// The pool is notified only for concurrency changes.
	if fmt.Sprint(applied) != "[3 2]" {
		t.Fatalf("concurrency notifications = %v, want [3 2]", applied)
	}
}

func chunkSizePolicyConfig(min, max int64) policy.Policy {
	p, err := policy.NewChunkSizePolicy(policy.ChunkSizePolicyConfig{
		MinChunkSize:         min,
		MaxChunkSize:         max,
		ImprovementThreshold: 0.1,
		DegradationThreshold: 0.1,
	})
	if err != nil {
		panic(err)
	}
	return p
}

func TestChunkSizePolicyIncreases(t *testing.T) {
	content := pattern(600)
	ts := &throughputServer{content: content, delayFor: func(n int) time.Duration {
		if n <= 4 {
			return 40 * time.Millisecond
		}
		return 2 * time.Millisecond
	}}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 32
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 256
	cfg.InitialChunkSize = 32
	cfg.ChunkSizeStep = 32
	cfg.PolicyWindowSize = 4
	cfg.Policy = chunkSizePolicyConfig(32, 256)

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	assertRangesTile(t, ts.requestedRanges(), int64(len(content)))
	if got := dl.ChunkSize(); got <= 32 {
		t.Fatalf("ChunkSize() = %d, want > 32 after throughput increase", got)
	}
	if got := dl.Concurrency(); got != 1 {
		t.Fatalf("Concurrency() = %d, want 1 (chunk policy must not change concurrency)", got)
	}
	maxLen := int64(0)
	for _, l := range rangeLengths(ts.requestedRanges()) {
		if l > maxLen {
			maxLen = l
		}
	}
	if maxLen <= 32 {
		t.Fatalf("max range length = %d, want > 32", maxLen)
	}
}

func TestChunkSizePolicyDecreases(t *testing.T) {
	content := pattern(600)
	ts := &throughputServer{content: content, delayFor: func(n int) time.Duration {
		if n <= 4 {
			return 2 * time.Millisecond
		}
		return 40 * time.Millisecond
	}}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 128
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 128
	cfg.InitialChunkSize = 128
	cfg.ChunkSizeStep = 32
	cfg.PolicyWindowSize = 4
	cfg.Policy = chunkSizePolicyConfig(32, 128)

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	assertRangesTile(t, ts.requestedRanges(), int64(len(content)))
	if got := dl.ChunkSize(); got >= 128 {
		t.Fatalf("ChunkSize() = %d, want < 128 after throughput decrease", got)
	}
	if got := dl.Concurrency(); got != 1 {
		t.Fatalf("Concurrency() = %d, want 1", got)
	}
}

func TestChunkSizeActionAtMinimum(t *testing.T) {
	content := pattern(300)
	ts := &throughputServer{content: content}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 32
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 128
	cfg.InitialChunkSize = 32
	cfg.ChunkSizeStep = 32
	cfg.Policy = &scriptedPolicy{actions: []policy.Action{policy.DecreaseChunkSize()}}

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}
	assertFileEquals(t, out, content)
	if got := dl.ChunkSize(); got != 32 {
		t.Fatalf("ChunkSize() = %d, want 32 (clamped at min)", got)
	}
	for _, l := range rangeLengths(ts.requestedRanges()) {
		if l > 32 {
			t.Fatalf("range length = %d, want <= 32", l)
		}
	}
}

func TestChunkSizeActionAtMaximum(t *testing.T) {
	content := pattern(300)
	ts := &throughputServer{content: content}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 32
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 64
	cfg.InitialChunkSize = 32
	cfg.ChunkSizeStep = 32
	cfg.Policy = &scriptedPolicy{actions: []policy.Action{policy.IncreaseChunkSize()}}

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}
	assertFileEquals(t, out, content)
	if got := dl.ChunkSize(); got != 64 {
		t.Fatalf("ChunkSize() = %d, want 64 (clamped at max)", got)
	}
	for _, l := range rangeLengths(ts.requestedRanges()) {
		if l > 64 {
			t.Fatalf("range length = %d, want <= 64", l)
		}
	}
}

func TestChunkSizeActionDoesNotChangeConcurrency(t *testing.T) {
	content := pattern(400)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 2
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4
	cfg.ChunkSize = 32
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 256
	cfg.InitialChunkSize = 32
	cfg.ChunkSizeStep = 32
	cfg.Policy = &scriptedPolicy{actions: []policy.Action{policy.IncreaseChunkSize()}}

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	gs.waitForActive(t, 2)
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	if got := dl.Concurrency(); got != 2 {
		t.Fatalf("Concurrency() = %d, want 2 (chunk action must not change concurrency)", got)
	}
	if maxActive, _ := gs.snapshot(); maxActive != 2 {
		t.Fatalf("maxActive = %d, want 2", maxActive)
	}
}

func TestPolicyChunkActionInFlightImmutable(t *testing.T) {
	content := pattern(400)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 2
	cfg.MinConcurrency = 2
	cfg.MaxConcurrency = 2
	cfg.ChunkSize = 32
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 256
	cfg.InitialChunkSize = 32
	cfg.ChunkSizeStep = 32
	cfg.Policy = &scriptedPolicy{actions: []policy.Action{policy.IncreaseChunkSize()}}

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	// Two ranges are allocated at 32 and in flight before any decision.
	gs.waitForActive(t, 2)
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	ranges := gs.requestedRanges()
	sorted := append([][2]int64(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })
	if sorted[0] != [2]int64{0, 31} || sorted[1] != [2]int64{32, 63} {
		t.Fatalf("first two ranges = %v, %v; want [0 31] and [32 63] (immutable in flight)", sorted[0], sorted[1])
	}
	assertRangesTile(t, ranges, int64(len(content)))
}

func TestPolicyChunkActionRetryPreservesBoundaries(t *testing.T) {
	content := pattern(200)
	rs := newRetryBoundaryServer(content)
	srv := httptest.NewServer(rs)
	t.Cleanup(srv.Close)
	defer rs.release()

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.ChunkSize = 32
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 128
	cfg.InitialChunkSize = 32
	cfg.ChunkSizeStep = 32
	cfg.MaxRetries = 2
	cfg.Policy = &scriptedPolicy{actions: []policy.Action{policy.IncreaseChunkSize()}}

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()

	select {
	case <-rs.firstStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the first request")
	}
	rs.release() // first attempt fails; the policy raises the target, but the
	// retry must reuse the original boundaries.
	if err := <-done; err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	got := rs.requested()
	if len(got) < 2 {
		t.Fatalf("requests = %v, want at least 2", got)
	}
	if got[0] != [2]int64{0, 31} || got[1] != [2]int64{0, 31} {
		t.Fatalf("first two requests = %v, %v; want [0 31] twice (retry keeps boundaries)", got[0], got[1])
	}
}

func TestFixedPolicyLeavesBothControllersUnchanged(t *testing.T) {
	content := pattern(300)
	ts := &throughputServer{content: content}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 2
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4
	cfg.ChunkSize = 64
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 128
	cfg.InitialChunkSize = 64
	cfg.ChunkSizeStep = 32
	cfg.Policy = policy.FixedPolicy{}

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}
	assertFileEquals(t, out, content)
	if got := dl.Concurrency(); got != 2 {
		t.Fatalf("Concurrency() = %d, want 2", got)
	}
	if got := dl.ChunkSize(); got != 64 {
		t.Fatalf("ChunkSize() = %d, want 64", got)
	}
}

// recordingActionPolicy wraps a Policy and records each decision and Context.
type recordingActionPolicy struct {
	inner policy.Policy

	mu       sync.Mutex
	actions  []policy.Action
	contexts []policy.Context
}

func (p *recordingActionPolicy) SelectAction(ctx policy.Context) policy.Action {
	a := p.inner.SelectAction(ctx)
	p.mu.Lock()
	p.actions = append(p.actions, a)
	p.contexts = append(p.contexts, ctx)
	p.mu.Unlock()
	return a
}

func (p *recordingActionPolicy) Update(ctx policy.Context, a policy.Action, reward float64) {
	p.inner.Update(ctx, a, reward)
}

func (p *recordingActionPolicy) recorded() ([]policy.Action, []policy.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]policy.Action(nil), p.actions...), append([]policy.Context(nil), p.contexts...)
}

// onceSequencePolicy returns a fixed sequence of actions once, then NoChange.
type onceSequencePolicy struct {
	mu       sync.Mutex
	actions  []policy.Action
	calls    int
	contexts []policy.Context
}

func (p *onceSequencePolicy) SelectAction(ctx policy.Context) policy.Action {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.contexts = append(p.contexts, ctx)
	if p.calls >= len(p.actions) {
		return policy.NoChange()
	}
	a := p.actions[p.calls]
	p.calls++
	return a
}

func (p *onceSequencePolicy) Update(policy.Context, policy.Action, float64) {}

func (p *onceSequencePolicy) recorded() []policy.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]policy.Context(nil), p.contexts...)
}

// TestJointPolicyIntegrationExactSequence drives the downloader with a scripted
// alternating action sequence and verifies each dimension is applied
// independently and that Context reflects the live controller values.
func TestJointPolicyIntegrationExactSequence(t *testing.T) {
	content := pattern(400)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 1
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4
	cfg.ChunkSize = 32
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 256
	cfg.InitialChunkSize = 32
	cfg.ChunkSizeStep = 32
	seq := &onceSequencePolicy{actions: []policy.Action{
		policy.IncreaseConcurrency(),
		policy.IncreaseChunkSize(),
		policy.IncreaseConcurrency(),
	}}
	cfg.Policy = seq

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- dl.Download(context.Background()) }()
	gs.releaseAll()
	if err := <-done; err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	assertRangesTile(t, gs.requestedRanges(), int64(len(content)))

	if got := dl.Concurrency(); got != 3 {
		t.Fatalf("Concurrency() = %d, want 3 (two concurrency increases)", got)
	}
	if got := dl.ChunkSize(); got != 64 {
		t.Fatalf("ChunkSize() = %d, want 64 (one chunk-size increase)", got)
	}

	ctxs := seq.recorded()
	if len(ctxs) < 3 {
		t.Fatalf("decisions = %d, want >= 3", len(ctxs))
	}
	want := []struct {
		conc  int
		chunk int64
	}{
		{1, 32}, // before IncreaseConcurrency
		{2, 32}, // before IncreaseChunkSize
		{2, 64}, // before the second IncreaseConcurrency
	}
	for i, w := range want {
		if ctxs[i].Concurrency != w.conc || ctxs[i].ChunkSize != w.chunk {
			t.Fatalf("ctx[%d] = {conc %d, chunk %d}, want {%d, %d}", i, ctxs[i].Concurrency, ctxs[i].ChunkSize, w.conc, w.chunk)
		}
	}

	// Metrics show both the old and the new range sizes.
	seen := map[int64]bool{}
	for _, rec := range dl.Metrics().Records {
		seen[rec.RangeEnd-rec.RangeStart+1] = true
	}
	if !seen[32] || !seen[64] {
		t.Fatalf("metrics range sizes = %v, want both 32 and 64", seen)
	}
}

// TestJointPolicyIntegrationAlternatesDimensions uses the real JointPolicy with
// a decreasing-delay server and verifies the actionable decisions alternate
// between concurrency and chunk size, that both controllers change, and that
// output and metrics stay correct.
func TestJointPolicyIntegrationAlternatesDimensions(t *testing.T) {
	content := pattern(2000)
	ts := &throughputServer{content: content, delayFor: func(n int) time.Duration {
		d := 60*time.Millisecond - time.Duration(n)*time.Millisecond
		if d < time.Millisecond {
			d = time.Millisecond
		}
		return d
	}}
	srv := httptest.NewServer(ts)
	t.Cleanup(srv.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	cfg := baseConfig(srv.URL, out, int64(len(content)))
	cfg.Concurrency = 2
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 64
	cfg.ChunkSize = 64
	cfg.MinChunkSize = 32
	cfg.MaxChunkSize = 1 << 20
	cfg.InitialChunkSize = 64
	cfg.ChunkSizeStep = 32
	cfg.PolicyWindowSize = 1

	jp, err := policy.NewJointPolicy(policy.JointPolicyConfig{
		MinConcurrency:       1,
		MaxConcurrency:       64,
		MinChunkSize:         32,
		MaxChunkSize:         1 << 20,
		ImprovementThreshold: 0.05,
		DegradationThreshold: 0.05,
	})
	if err != nil {
		t.Fatalf("NewJointPolicy = %v", err)
	}
	rec := &recordingActionPolicy{inner: jp}
	cfg.Policy = rec

	dl, err := New(cfg)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}

	assertFileEquals(t, out, content)
	assertRangesTile(t, ts.requestedRanges(), int64(len(content)))

	actions, contexts := rec.recorded()
	var actionable []policy.ActionKind
	for _, a := range actions {
		switch a.Kind {
		case policy.ActionIncreaseConcurrency, policy.ActionDecreaseConcurrency,
			policy.ActionIncreaseChunkSize, policy.ActionDecreaseChunkSize:
			actionable = append(actionable, a.Kind)
		}
	}
	if len(actionable) < 2 {
		t.Fatalf("actionable decisions = %d, want >= 2", len(actionable))
	}
	isConcurrency := func(k policy.ActionKind) bool {
		return k == policy.ActionIncreaseConcurrency || k == policy.ActionDecreaseConcurrency
	}
	// The first actionable decision targets concurrency, the second chunk size;
	// neither dimension is at a bound initially, so no fallback can occur here.
	if !isConcurrency(actionable[0]) {
		t.Fatalf("actionable[0] = %v, want a concurrency action", actionable[0])
	}
	if isConcurrency(actionable[1]) {
		t.Fatalf("actionable[1] = %v, want a chunk-size action", actionable[1])
	}

	// Contexts reflect the live controllers: both dimensions changed at some
	// point (later decisions may hit bounds and fall back).
	sawConcChange, sawChunkChange := false, false
	for _, c := range contexts {
		if c.Concurrency != 2 {
			sawConcChange = true
		}
		if c.ChunkSize != 64 {
			sawChunkChange = true
		}
	}
	if !sawConcChange || !sawChunkChange {
		t.Fatalf("controllers did not change: sawConcChange=%v sawChunkChange=%v", sawConcChange, sawChunkChange)
	}
}

// rewardRecordingPolicy records each Update call's (context, action, reward) so
// tests can verify credit assignment: the (context, action) passed to Update
// must be the PREVIOUS decision's, not the newly selected one.
type rewardRecordingPolicy struct {
	mu       sync.Mutex
	actions  []policy.Action
	updates  []policy.Context
	upActs   []policy.Action
	upReward []float64
	calls    int
}

func (p *rewardRecordingPolicy) SelectAction(policy.Context) policy.Action {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.actions) == 0 {
		return policy.NoChange()
	}
	a := p.actions[p.calls%len(p.actions)]
	p.calls++
	return a
}

func (p *rewardRecordingPolicy) Update(ctx policy.Context, action policy.Action, reward float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.updates = append(p.updates, ctx)
	p.upActs = append(p.upActs, action)
	p.upReward = append(p.upReward, reward)
}

func (p *rewardRecordingPolicy) snapshot() (ctxs []policy.Context, acts []policy.Action, rewards []float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]policy.Context(nil), p.updates...),
		append([]policy.Action(nil), p.upActs...),
		append([]float64(nil), p.upReward...)
}

func defaultTestRewardConfig() rl.RewardConfig {
	return rl.RewardConfig{
		ThroughputScale: 1000,
		LatencyScale:    1.0,
		RetryPenalty:    0.5,
		FailurePenalty:  1.0,
		ActionCost:      0.1,
		WThroughput:     1.0,
		WImprovement:    1.0,
		WLatency:        0.5,
		WRetry:          0.5,
		WFailure:        1.0,
		WAction:         1.0,
	}
}

// TestPolicyRunnerRewardCreditAssignment proves that Update at step t is called
// with the context/action chosen at step t-1, and that the reward reflects the
// change between those two observations. It makes it impossible to accidentally
// credit the newly selected action.
func TestPolicyRunnerRewardCreditAssignment(t *testing.T) {
	conc, err := concurrency.New(1, 4)
	if err != nil {
		t.Fatalf("concurrency.New = %v", err)
	}
	rec := &rewardRecordingPolicy{
		actions: []policy.Action{
			policy.IncreaseConcurrency(), // decision 0
			policy.IncreaseChunkSize(),   // decision 1
			policy.NoChange(),            // decision 2
		},
	}
	rewarder, err := rl.NewRewardCalculator(defaultTestRewardConfig())
	if err != nil {
		t.Fatalf("NewRewardCalculator = %v", err)
	}
	runner := newPolicyRunner(rec, conc, nil, 64, 4, nil, rewarder)

	// Deterministic wall-clock for the aggregate-throughput observable: the
	// fake clock advances by each attempt's duration, so the samples are
	// sequential in wall-clock terms and aggregate == per-request throughput.
	base := time.Unix(1_000_000, 0)
	now := base
	runner.now = func() time.Time { return now }

	// Three observations. Each observe() first rewards the previous action (if
	// any) and then selects a new one.
	runner.observe(metrics.Record{Bytes: 1000, Duration: time.Second, Success: true, Attempt: 1}) // t0
	now = base.Add(time.Second)
	runner.observe(metrics.Record{Bytes: 3000, Duration: time.Second, Success: true, Attempt: 1}) // t1
	now = base.Add(2 * time.Second)
	runner.observe(metrics.Record{Bytes: 3000, Duration: time.Second, Success: true, Attempt: 1}) // t2

	ctxs, acts, rewards := rec.snapshot()

	// t0's observe selects decision 0 but does NOT call Update (no previous).
	// t1's observe calls Update with t0's context and decision 0's action.
	// t2's observe calls Update with t1's context and decision 1's action.
	if len(acts) != 2 {
		t.Fatalf("Update calls = %d, want 2", len(acts))
	}
	if acts[0].Kind != policy.ActionIncreaseConcurrency {
		t.Fatalf("first Update action = %v, want IncreaseConcurrency (t0's action)", acts[0])
	}
	if acts[1].Kind != policy.ActionIncreaseChunkSize {
		t.Fatalf("second Update action = %v, want IncreaseChunkSize (t1's action)", acts[1])
	}

	// The context passed to the first Update must be the t0 context: its
	// concurrency is the pre-action value (1), NOT the post-increase value (2).
	if ctxs[0].Concurrency != 1 {
		t.Fatalf("first Update context concurrency = %d, want 1 (pre-action)", ctxs[0].Concurrency)
	}
	// The second Update's context is t1, by which point decision 0's
	// IncreaseConcurrency has taken effect in the controller read.
	if ctxs[1].Concurrency != 2 {
		t.Fatalf("second Update context concurrency = %d, want 2 (post-decision-0)", ctxs[1].Concurrency)
	}

	// Reward for the first Update uses the AGGREGATE throughput over the window
	// (sequential samples, so it equals the per-request value): at t0 the window
	// is [1000B over 1s] = 1000 B/s; at t1 it is [4000B over 2s] = 2000 B/s.
	// Absolute = 2.0, improvement = +1.0. The action (IncreaseConcurrency)
	// changed concurrency 1 -> 2, so action cost 0.1 applies; latency cost =
	// 0.5*1.0. No retries.
	wantFirst := 1.0*2.0 + 1.0*1.0 - 0.5*1.0 - 1.0*0.1
	if diff := rewards[0] - wantFirst; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("first reward = %v, want %v", rewards[0], wantFirst)
	}

	// Reward for the second Update: the window (size 4) holds all three samples,
	// [7000B over 3s] = 2333.33 B/s. Absolute = 7/3, improvement = 1/3. The
	// action (IncreaseChunkSize) was a no-op because no chunk controller is
	// configured (chunk size stayed 64), so ActionCost is 0; latency = 0.5.
	wantSecond := 1.0*(7000.0/3.0)/1000.0 + 1.0*(7000.0/3.0-2000.0)/1000.0 - 0.5*1.0
	if diff := rewards[1] - wantSecond; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("second reward = %v, want %v", rewards[1], wantSecond)
	}
}

// TestPolicyRunnerNoRewarderKeepsZero verifies that without a reward config the
// runner still calls Update (backward compatible) with reward 0.
func TestPolicyRunnerNoRewarderKeepsZero(t *testing.T) {
	conc, _ := concurrency.New(1, 2)
	rec := &rewardRecordingPolicy{actions: []policy.Action{policy.NoChange()}}
	runner := newPolicyRunner(rec, conc, nil, 64, 4, nil, nil)

	runner.observe(metrics.Record{Bytes: 1000, Duration: time.Second, Success: true, Attempt: 1})
	runner.observe(metrics.Record{Bytes: 3000, Duration: time.Second, Success: true, Attempt: 1})

	_, _, rewards := rec.snapshot()
	if len(rewards) != 1 || rewards[0] != 0 {
		t.Fatalf("rewards = %v, want [0]", rewards)
	}
}

// TestDownloaderInvalidRewardConfig rejects a malformed reward configuration at
// construction time.
func TestDownloaderInvalidRewardConfig(t *testing.T) {
	bad := defaultTestRewardConfig()
	bad.WThroughput = -1
	cfg := baseConfig("http://example.invalid/never-called", "out.bin", 100)
	cfg.RewardConfig = &bad
	if _, err := New(cfg); err == nil {
		t.Fatal("New with invalid reward config = nil, want error")
	}
}

// TestPolicyRunnerLatencyIsWindowMean checks that Context.Latency is the mean
// response-header latency over the recent window, ignoring attempts that got
// no response (latency 0).
func TestPolicyRunnerLatencyIsWindowMean(t *testing.T) {
	conc, err := concurrency.New(1, 4)
	if err != nil {
		t.Fatalf("concurrency.New = %v", err)
	}
	rec := &testPolicy{}
	r := newPolicyRunner(rec, conc, nil, 64, 3, nil, nil) // window of 3 attempts
	r.now = func() time.Time { return time.Unix(0, 0) }
	for _, l := range []time.Duration{100 * time.Millisecond, 0, 200 * time.Millisecond, 400 * time.Millisecond} {
		r.observe(metrics.Record{Bytes: 10, Duration: time.Second, Latency: l, Success: l > 0, Attempt: 1})
	}
	ctxs, _, _ := rec.snapshot()
	want := []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 150 * time.Millisecond, 300 * time.Millisecond}
	for i, w := range want {
		if ctxs[i].Latency != w {
			t.Fatalf("observation %d: Latency = %v, want %v", i, ctxs[i].Latency, w)
		}
	}
}
