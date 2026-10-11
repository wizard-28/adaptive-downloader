package downloader

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// flowConnKey carries the server-assigned connection identity through the
// request context.
type flowConnKey struct{}

// flowServer serves deterministic Range responses but holds the first
// requests open until release is called. This makes simultaneous activity
// observable deterministically instead of relying on scheduler timing.
type flowServer struct {
	content []byte

	releaseOnce sync.Once
	releaseCh   chan struct{}

	mu               sync.Mutex
	changed          chan struct{}
	inFlight         int
	maxInFlight      int
	activeConns      map[int64]int
	maxActiveConns   int
	maxActivePerConn int
	totalConns       int64
	connRequests     map[int64]int
	requests         int64
	nonHTTP11        int64
}

func newFlowServer(content []byte) *flowServer {
	return &flowServer{
		content:      content,
		releaseCh:    make(chan struct{}),
		changed:      make(chan struct{}),
		activeConns:  make(map[int64]int),
		connRequests: make(map[int64]int),
	}
}

// release unblocks all currently held and future requests. Idempotent.
func (s *flowServer) release() {
	s.releaseOnce.Do(func() { close(s.releaseCh) })
}

// broadcastLocked wakes anyone waiting on a state change. Callers hold mu.
func (s *flowServer) broadcastLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}

// connContext assigns a stable identity to each accepted TCP connection.
func (s *flowServer) connContext(ctx context.Context, c net.Conn) context.Context {
	s.mu.Lock()
	s.totalConns++
	id := s.totalConns
	s.mu.Unlock()
	return context.WithValue(ctx, flowConnKey{}, id)
}

func (s *flowServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start, end, ok := parseRangeHeader(r.Header.Get("Range"))
	if !ok || start < 0 || start >= int64(len(s.content)) || end < start {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if end >= int64(len(s.content)) {
		end = int64(len(s.content)) - 1
	}
	id, _ := r.Context().Value(flowConnKey{}).(int64)

	s.mu.Lock()
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	s.activeConns[id]++
	if n := len(s.activeConns); n > s.maxActiveConns {
		s.maxActiveConns = n
	}
	if n := s.activeConns[id]; n > s.maxActivePerConn {
		s.maxActivePerConn = n
	}
	s.connRequests[id]++
	s.requests++
	if r.ProtoMajor != 1 || r.ProtoMinor != 1 {
		s.nonHTTP11++
	}
	s.broadcastLocked()
	s.mu.Unlock()

	select {
	case <-s.releaseCh:
	case <-r.Context().Done():
	}

	body := s.content[start : end+1]
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.content)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)

	s.mu.Lock()
	s.inFlight--
	s.activeConns[id]--
	if s.activeConns[id] == 0 {
		delete(s.activeConns, id)
	}
	s.broadcastLocked()
	s.mu.Unlock()
}

// waitForInFlight blocks until at least n requests are simultaneously in
// flight, or fails the test after a deadline.
func (s *flowServer) waitForInFlight(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		s.mu.Lock()
		got := s.inFlight
		if got >= n {
			s.mu.Unlock()
			return
		}
		ch := s.changed
		s.mu.Unlock()
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("timed out waiting for %d simultaneous in-flight requests (observed %d)", n, got)
		}
	}
}

// flowStats is a snapshot of server-side flow observations.
type flowStats struct {
	maxInFlight      int
	maxActiveConns   int
	maxActivePerConn int
	totalConns       int64
	requests         int64
	nonHTTP11        int64
	connRequests     map[int64]int
}

func (s *flowServer) snapshot() flowStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	perConn := make(map[int64]int, len(s.connRequests))
	for id, n := range s.connRequests {
		perConn[id] = n
	}
	return flowStats{
		maxInFlight:      s.maxInFlight,
		maxActiveConns:   s.maxActiveConns,
		maxActivePerConn: s.maxActivePerConn,
		totalConns:       s.totalConns,
		requests:         s.requests,
		nonHTTP11:        s.nonHTTP11,
		connRequests:     perConn,
	}
}

// TestConcurrencyToFlowMapping measures the configured-concurrency-to-flow
// mapping for C in {1,2,4,8,16}. The server holds requests open until all C
// workers have one in flight, so simultaneous flow counts are deterministic.
//
// Verified per C: in-flight Range requests, simultaneously active TCP
// connections (server-side connection identity), total TCP connections
// created, requests served per connection, and HTTP protocol. Exactly C
// connections must be created and reused: extra connections would indicate
// idle-pool churn, fewer would indicate flow collapse.
func TestConcurrencyToFlowMapping(t *testing.T) {
	const (
		chunkSize       = 1024
		rangesPerWorker = 8
	)

	for _, c := range []int{1, 2, 4, 8, 16} {
		c := c
		t.Run(fmt.Sprintf("C=%d", c), func(t *testing.T) {
			totalSize := int64(c) * chunkSize * rangesPerWorker
			content := pattern(int(totalSize))
			fs := newFlowServer(content)
			srv := httptest.NewUnstartedServer(fs)
			srv.Config.ConnContext = fs.connContext
			srv.Start()
			t.Cleanup(srv.Close)

			out := filepath.Join(t.TempDir(), "out.bin")
			d, err := New(Config{
				URL:         srv.URL,
				OutputPath:  out,
				TotalSize:   totalSize,
				Concurrency: c,
				ChunkSize:   chunkSize,
				Timeout:     10 * time.Second,
				MaxRetries:  1,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			done := make(chan error, 1)
			go func() { done <- d.Download(context.Background()) }()
			// On failure paths this unblocks the server so cleanup cannot hang.
			defer fs.release()

			fs.waitForInFlight(t, c)
			fs.release()

			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("Download: %v", err)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("Download did not finish after release")
			}

			st := fs.snapshot()
			if st.maxInFlight != c {
				t.Errorf("max simultaneous in-flight Range requests = %d, want %d", st.maxInFlight, c)
			}
			if st.maxActiveConns != c {
				t.Errorf("max simultaneously active TCP connections = %d, want %d", st.maxActiveConns, c)
			}
			if st.maxActivePerConn != 1 {
				t.Errorf("max in-flight requests on one connection = %d, want 1 (HTTP/1.1 has one request per connection)", st.maxActivePerConn)
			}
			if got := int(st.totalConns); got != c {
				t.Errorf("total TCP connections created = %d, want %d (idle pool must not churn)", got, c)
			}
			wantRequests := int64(c * rangesPerWorker)
			if st.requests != wantRequests {
				t.Errorf("requests served = %d, want %d", st.requests, wantRequests)
			}
			if st.nonHTTP11 != 0 {
				t.Errorf("%d responses were not HTTP/1.1", st.nonHTTP11)
			}

			// The idle pool is shared and LIFO, so per-connection request
			// counts may be uneven; reuse means requests beyond the first C
			// are served on already-used connections without new dials.
			reused := 0
			maxPerConn := 0
			for _, n := range st.connRequests {
				if n > 1 {
					reused++
				}
				if n > maxPerConn {
					maxPerConn = n
				}
			}
			if reused < 1 {
				t.Error("no connection served more than one request; reuse did not occur")
			}
			if maxPerConn < 2 {
				t.Errorf("max requests per connection = %d, want >= 2 (no reuse observed)", maxPerConn)
			}

			assertFileEquals(t, out, content)
			t.Logf("C=%d: maxInFlight=%d maxActiveConns=%d totalConns=%d requests=%d reusedConns=%d maxPerConn=%d",
				c, st.maxInFlight, st.maxActiveConns, st.totalConns, st.requests, reused, maxPerConn)
		})
	}
}
