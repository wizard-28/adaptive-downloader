package fetcher

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strconv"
	"sync"
	"testing"
	"time"
)

// connIDKey carries the server-assigned connection identity through the
// request context.
type connIDKey struct{}

// observedRequest is one request as seen by the test server.
type observedRequest struct {
	connID int64
	proto  string
	remote string
	start  int64
	end    int64
}

// connObservingServer is a range server that assigns a stable identity to each
// accepted TCP connection (via http.Server.ConnContext) and records which
// connection served every request. The remote address is the client's local
// address, which lets the test cross-check server-side identity against the
// net.Conn identity reported by httptrace.
type connObservingServer struct {
	mu       sync.Mutex
	nextID   int64
	requests []observedRequest
}

func newConnObservingServer(t *testing.T, content []byte) (*httptest.Server, *connObservingServer) {
	t.Helper()
	obs := &connObservingServer{}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end, ok := parseTestRange(r.Header.Get("Range"))
		if !ok || start < 0 || end < start || start >= int64(len(content)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= int64(len(content)) {
			end = int64(len(content)) - 1
		}

		id, _ := r.Context().Value(connIDKey{}).(int64)
		obs.mu.Lock()
		obs.requests = append(obs.requests, observedRequest{
			connID: id,
			proto:  r.Proto,
			remote: r.RemoteAddr,
			start:  start,
			end:    end,
		})
		obs.mu.Unlock()

		body := content[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	srv.Config.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
		obs.mu.Lock()
		obs.nextID++
		id := obs.nextID
		obs.mu.Unlock()
		return context.WithValue(ctx, connIDKey{}, id)
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, obs
}

func (s *connObservingServer) snapshot() []observedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]observedRequest(nil), s.requests...)
}

func (s *connObservingServer) totalConns() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextID
}

// TestSequentialRangeRequestsReuseSingleHTTP11Connection issues several
// sequential contiguous Range requests through the same Fetcher and verifies
// with both client-side httptrace instrumentation and server-side connection
// identity that exactly one HTTP/1.1 TCP connection is created and reused.
func TestSequentialRangeRequestsReuseSingleHTTP11Connection(t *testing.T) {
	const (
		contentSize = 4096
		span        = 256
		requests    = 8
	)
	content := pattern(contentSize)
	srv, obs := newConnObservingServer(t, content)
	f := New(Config{Timeout: 3 * time.Second})

	var (
		mu       sync.Mutex
		got      []httptrace.GotConnInfo
		connects int
	)
	trace := &httptrace.ClientTrace{
		ConnectStart: func(network, addr string) {
			mu.Lock()
			connects++
			mu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			got = append(got, info)
			mu.Unlock()
		},
	}

	for i := 0; i < requests; i++ {
		ctx := httptrace.WithClientTrace(context.Background(), trace)
		start := int64(i * span)
		res := f.Fetch(ctx, fmt.Sprintf("req-%d", i), srv.URL, start, start+span-1)
		if !res.Success {
			t.Fatalf("request %d: Success = false, Err = %v", i, res.Err)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if len(got) != requests {
		t.Fatalf("GotConn events = %d, want %d", len(got), requests)
	}
	if connects != 1 {
		t.Fatalf("TCP dials = %d, want 1 (no connection churn)", connects)
	}
	first := got[0]
	if first.Reused {
		t.Fatalf("first request reused a connection, want a new one")
	}
	if first.WasIdle {
		t.Fatalf("first request used an idle connection, want a freshly dialed one")
	}
	firstAddr := first.Conn.LocalAddr().String()
	for i, info := range got[1:] {
		if !info.Reused {
			t.Fatalf("request %d did not reuse a connection", i+1)
		}
		if !info.WasIdle {
			t.Fatalf("request %d reused a connection that was not idle", i+1)
		}
		if addr := info.Conn.LocalAddr().String(); addr != firstAddr {
			t.Fatalf("request %d used connection %s, want the first connection %s", i+1, addr, firstAddr)
		}
	}

	reqs := obs.snapshot()
	if len(reqs) != requests {
		t.Fatalf("server observed %d requests, want %d", len(reqs), requests)
	}
	if n := obs.totalConns(); n != 1 {
		t.Fatalf("server accepted %d TCP connections, want 1", n)
	}
	for i, r := range reqs {
		if r.proto != "HTTP/1.1" {
			t.Errorf("request %d: server observed proto %q, want HTTP/1.1", i, r.proto)
		}
		if r.connID != 1 {
			t.Errorf("request %d: server conn ID = %d, want 1", i, r.connID)
		}
		if r.remote != firstAddr {
			t.Errorf("request %d: server remote %s does not match client connection %s", i, r.remote, firstAddr)
		}
	}
}
