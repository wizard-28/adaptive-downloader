package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// stragglerServer serves ranges normally, except that the first request for
// offset 0 sends a small burst and then blocks until a request starting at
// an offset no plain allocation could produce (a split tail) arrives. With
// abortHead set, that first request is then aborted instead of finished.
type stragglerServer struct {
	content   []byte
	chunk     int64
	abortHead bool

	mu       sync.Mutex
	requests [][2]int64
	head0    int
	release  chan struct{}
	once     sync.Once
}

func (s *stragglerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start, end, ok := parseRangeHeader(r.Header.Get("Range"))
	if !ok || end >= int64(len(s.content)) {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, [2]int64{start, end})
	first := start == 0 && s.head0 == 0
	if start == 0 {
		s.head0++
	}
	s.mu.Unlock()
	if start%s.chunk != 0 {
		s.once.Do(func() { close(s.release) })
	}

	body := s.content[start : end+1]
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.content)))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusPartialContent)
	if !first {
		_, _ = w.Write(body)
		return
	}
	const burst = 256 << 10
	_, _ = w.Write(body[:burst])
	w.(http.Flusher).Flush()
	select {
	case <-s.release:
	case <-r.Context().Done():
		return
	}
	if s.abortHead {
		panic(http.ErrAbortHandler)
	}
	_, _ = w.Write(body[burst:])
}

func (s *stragglerServer) snapshot() [][2]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]int64(nil), s.requests...)
}

func runSplitDownload(t *testing.T, s *stragglerServer, split bool) *Downloader {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	out := filepath.Join(t.TempDir(), "out.bin")
	d, err := New(Config{
		URL: srv.URL, OutputPath: out, TotalSize: int64(len(s.content)),
		Concurrency: 2, ChunkSize: s.chunk, MaxRetries: 2, SplitStragglers: split,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Download(context.Background()); err != nil {
		t.Fatalf("Download: %v", err)
	}
	assertFileEquals(t, out, s.content)
	return d
}

func TestSplitStragglerTakesTail(t *testing.T) {
	s := &stragglerServer{content: pattern(8 << 20), chunk: 4 << 20, release: make(chan struct{})}
	d := runSplitDownload(t, s, true)
	if d.Splits() < 1 {
		t.Fatalf("Splits() = %d, want >= 1", d.Splits())
	}
	var tails int
	for _, r := range s.snapshot() {
		if r[0]%s.chunk != 0 {
			tails++
			if r[0] <= 0 || r[0] >= s.chunk || r[1] != s.chunk-1 {
				t.Fatalf("first split tail %v must lie inside [0,%d) and keep its end", r, s.chunk)
			}
			break
		}
	}
	if tails == 0 {
		t.Fatalf("no split tail request seen: %v", s.snapshot())
	}
	for _, rec := range d.Metrics().Records {
		if !rec.Success {
			t.Fatalf("unexpected failed attempt %+v", rec)
		}
	}
}

// TestSplitHeadRetryUsesShortenedBounds: after a split, the original request
// fails; its retry must ask for the shortened range only, ending just before
// the tail that another worker took.
func TestSplitHeadRetryUsesShortenedBounds(t *testing.T) {
	s := &stragglerServer{content: pattern(8 << 20), chunk: 4 << 20, release: make(chan struct{}), abortHead: true}
	runSplitDownload(t, s, true)
	var tailStart int64 = -1
	var heads [][2]int64
	for _, r := range s.snapshot() {
		if r[0]%s.chunk != 0 && tailStart < 0 {
			tailStart = r[0]
		}
		if r[0] == 0 {
			heads = append(heads, r)
		}
	}
	if tailStart < 0 || len(heads) < 2 {
		t.Fatalf("tail=%d heads=%v", tailStart, heads)
	}
	if heads[0][1] != s.chunk-1 || heads[1][1] != tailStart-1 {
		t.Fatalf("head requests %v; want first to end at %d, retry at %d", heads, s.chunk-1, tailStart-1)
	}
}

func TestSplitOffByDefault(t *testing.T) {
	content := pattern(8 << 20)
	_, srv := newTestServer(content)
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "out.bin")
	d, err := New(Config{URL: srv.URL, OutputPath: out, TotalSize: int64(len(content)), Concurrency: 4, ChunkSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Download(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.Splits() != 0 {
		t.Fatalf("Splits() = %d with splitting off", d.Splits())
	}
	if _, err := New(Config{URL: srv.URL, OutputPath: out, TotalSize: 1, Concurrency: 1, ChunkSize: 1, SplitMinBytes: 1}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SplitMinBytes without SplitStragglers: err = %v", err)
	}
}
