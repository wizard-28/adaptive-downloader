package fetcher

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLimitShrinkRules(t *testing.T) {
	l := NewLimit(100, 199)
	if !l.Shrink(150) {
		t.Fatal("shrink inside range rejected")
	}
	if l.Shrink(150) || l.Shrink(180) {
		t.Fatal("shrink must make the range strictly shorter")
	}
	buf, done := l.accept(nil, make([]byte, 30))
	if done || len(buf) != 30 {
		t.Fatalf("accept: len=%d done=%v", len(buf), done)
	}
	if l.Shrink(128) {
		t.Fatal("shrink below received bytes accepted")
	}
	if !l.Shrink(129) {
		t.Fatal("shrink to exactly the received bytes rejected")
	}
	if recv, end := l.Snapshot(); recv != 30 || end != 129 {
		t.Fatalf("snapshot = %d,%d", recv, end)
	}
	if _, done := l.accept(buf, []byte{1}); !done {
		t.Fatal("range should be complete")
	}
}

func TestFetchLimitedNilMatchesFetch(t *testing.T) {
	content := pattern(4096)
	srv := newRangeServer(t, content)
	f := New(Config{})
	a := f.Fetch(context.Background(), "a", srv.URL, 100, 999)
	b := f.FetchLimited(context.Background(), "b", srv.URL, 100, 999, nil)
	if !a.Success || !b.Success || !bytes.Equal(a.Data, b.Data) || a.Range != b.Range {
		t.Fatalf("Fetch %v/%v vs FetchLimited(nil) %v/%v", a.Success, a.Err, b.Success, b.Err)
	}
}

func TestFetchLimitedFullRange(t *testing.T) {
	content := pattern(1 << 20)
	srv := newRangeServer(t, content)
	lim := NewLimit(10, 700000)
	res := New(Config{}).FetchLimited(context.Background(), "r", srv.URL, 10, 700000, lim)
	if !res.Success || !bytes.Equal(res.Data, content[10:700001]) {
		t.Fatalf("success=%v err=%v len=%d", res.Success, res.Err, len(res.Data))
	}
}

// TestFetchLimitedShrinkMidBody: the server sends part of the body, the test
// shrinks the limit while the server is blocked, and the fetch must return
// exactly the shortened range without waiting for the rest.
func TestFetchLimitedShrinkMidBody(t *testing.T) {
	content := pattern(1 << 20)
	const first = 64 << 10
	sent := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(content)-1, len(content)))
		w.Header().Set("Content-Length", fmt.Sprint(len(content)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(content[:first])
		w.(http.Flusher).Flush()
		close(sent)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Write(content[first:])
	}))
	defer srv.Close()
	defer close(release)

	lim := NewLimit(0, int64(len(content))-1)
	done := make(chan Result, 1)
	go func() {
		done <- New(Config{}).FetchLimited(context.Background(), "r", srv.URL, 0, int64(len(content))-1, lim)
	}()
	<-sent
	// The server has sent only the first burst, so the reader cannot have
	// accepted more and the shrink to exactly that point must succeed.
	if !lim.Shrink(first - 1) {
		t.Fatal("shrink to the first burst rejected")
	}
	res := <-done
	if !res.Success || res.Range.End != first-1 || !bytes.Equal(res.Data, content[:first]) {
		t.Fatalf("success=%v err=%v end=%d len=%d", res.Success, res.Err, res.Range.End, len(res.Data))
	}
}
