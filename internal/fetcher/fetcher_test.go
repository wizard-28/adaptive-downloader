package fetcher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// pattern returns n deterministic bytes.
func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

// parseTestRange parses an HTTP "bytes=START-END" header.
func parseTestRange(header string) (start, end int64, ok bool) {
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

// newRangeServer serves content with Range support.
func newRangeServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Range")
		if header == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}
		start, end, ok := parseTestRange(header)
		if !ok || start < 0 || end < start || start >= int64(len(content)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= int64(len(content)) {
			end = int64(len(content)) - 1
		}
		body := content[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchSuccess(t *testing.T) {
	content := pattern(1000)
	srv := newRangeServer(t, content)
	f := New(Config{Timeout: 5 * time.Second})

	res := f.Fetch(context.Background(), "req-1", srv.URL, 100, 199)

	if !res.Success {
		t.Fatalf("Success = false, Err = %v", res.Err)
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if res.RequestID != "req-1" {
		t.Errorf("RequestID = %q, want %q", res.RequestID, "req-1")
	}
	if res.URL != srv.URL {
		t.Errorf("URL = %q, want %q", res.URL, srv.URL)
	}
	if want := (ByteRange{Start: 100, End: 199}); res.Range != want {
		t.Errorf("Range = %v, want %v", res.Range, want)
	}
	if res.StatusCode != http.StatusPartialContent {
		t.Errorf("StatusCode = %d, want %d", res.StatusCode, http.StatusPartialContent)
	}
	if want := content[100:200]; !bytes.Equal(res.Data, want) {
		t.Errorf("Data does not match requested bytes")
	}
	if res.Bytes != 100 {
		t.Errorf("Bytes = %d, want 100", res.Bytes)
	}
	if res.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", res.Duration)
	}
	if res.StartTime.IsZero() || res.EndTime.IsZero() {
		t.Errorf("StartTime/EndTime must be set")
	}
	if res.EndTime.Before(res.StartTime) {
		t.Errorf("EndTime %v before StartTime %v", res.EndTime, res.StartTime)
	}
	if res.Throughput <= 0 {
		t.Errorf("Throughput = %v, want > 0", res.Throughput)
	}
}

func TestFetchSendsRangeHeader(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("Range")
		w.Header().Set("Content-Range", "bytes 10-19/100")
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, 10))
	}))
	t.Cleanup(srv.Close)

	res := New(Config{Timeout: time.Second}).Fetch(context.Background(), "id", srv.URL, 10, 19)
	if !res.Success {
		t.Fatalf("Success = false, Err = %v", res.Err)
	}
	if header := <-got; header != "bytes=10-19" {
		t.Fatalf("Range header = %q, want %q", header, "bytes=10-19")
	}
}

func TestFetchUserAgent(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("User-Agent")
		w.Header().Set("Content-Range", "bytes 0-9/100")
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, 10))
	}))
	t.Cleanup(srv.Close)

	for _, tc := range []struct{ set, want string }{
		{"", "Go-http-client/1.1"}, // empty keeps the Go default
		{"adaptivedl-test/1.0", "adaptivedl-test/1.0"},
	} {
		res := New(Config{Timeout: time.Second, UserAgent: tc.set}).Fetch(context.Background(), "id", srv.URL, 0, 9)
		if !res.Success {
			t.Fatalf("Success = false, Err = %v", res.Err)
		}
		if ua := <-got; ua != tc.want {
			t.Fatalf("UserAgent %q: header = %q, want %q", tc.set, ua, tc.want)
		}
	}
}

func TestFetchContentRangeMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-4/10")
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, 5))
	}))
	t.Cleanup(srv.Close)

	res := New(Config{Timeout: time.Second}).Fetch(context.Background(), "id", srv.URL, 5, 9)
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if !errors.Is(res.Err, ErrInvalidContentRange) {
		t.Fatalf("Err = %v, want ErrInvalidContentRange", res.Err)
	}
}

func TestFetchMalformedContentRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes not-a-range")
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, 5))
	}))
	t.Cleanup(srv.Close)

	res := New(Config{Timeout: time.Second}).Fetch(context.Background(), "id", srv.URL, 0, 4)
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if !errors.Is(res.Err, ErrInvalidContentRange) {
		t.Fatalf("Err = %v, want ErrInvalidContentRange", res.Err)
	}
}

func TestFetchServerReturns200(t *testing.T) {
	content := pattern(100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	res := New(Config{Timeout: time.Second}).Fetch(context.Background(), "id", srv.URL, 0, 9)
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if !errors.Is(res.Err, ErrRangeNotSupported) {
		t.Fatalf("Err = %v, want ErrRangeNotSupported", res.Err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", res.StatusCode)
	}
}

func TestFetchIncompleteResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-9/100")
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, 5))
	}))
	t.Cleanup(srv.Close)

	res := New(Config{Timeout: time.Second}).Fetch(context.Background(), "id", srv.URL, 0, 9)
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if !errors.Is(res.Err, ErrIncompleteResponse) {
		t.Fatalf("Err = %v, want ErrIncompleteResponse", res.Err)
	}
	if res.Bytes != 5 {
		t.Fatalf("Bytes = %d, want 5", res.Bytes)
	}
}

func TestFetchOverlongResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-9/100")
		w.Header().Set("Content-Length", "15")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, 15))
	}))
	t.Cleanup(srv.Close)

	res := New(Config{Timeout: time.Second}).Fetch(context.Background(), "id", srv.URL, 0, 9)
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if !errors.Is(res.Err, ErrUnexpectedLength) {
		t.Fatalf("Err = %v, want ErrUnexpectedLength", res.Err)
	}
}

func TestFetchMissingContentRange(t *testing.T) {
	content := pattern(20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[5:15])
	}))
	t.Cleanup(srv.Close)

	res := New(Config{Timeout: time.Second}).Fetch(context.Background(), "id", srv.URL, 5, 14)
	if !res.Success {
		t.Fatalf("Success = false, Err = %v", res.Err)
	}
	if !bytes.Equal(res.Data, content[5:15]) {
		t.Fatalf("Data does not match requested bytes")
	}
}

func TestFetchServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	res := New(Config{Timeout: time.Second}).Fetch(context.Background(), "id", srv.URL, 0, 9)
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if !errors.Is(res.Err, ErrUnexpectedStatus) {
		t.Fatalf("Err = %v, want ErrUnexpectedStatus", res.Err)
	}
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("StatusCode = %d, want 500", res.StatusCode)
	}
}

func TestFetchConnectionFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	res := New(Config{Timeout: time.Second}).Fetch(context.Background(), "id", url, 0, 9)
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if res.Err == nil {
		t.Fatal("Err = nil, want a connection error")
	}
	if errors.Is(res.Err, ErrTimeout) {
		t.Fatalf("Err = %v, want a non-timeout connection error", res.Err)
	}
}

func TestFetchTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	f := New(Config{Timeout: 50 * time.Millisecond})
	res := f.Fetch(context.Background(), "id", srv.URL, 0, 9)
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if !errors.Is(res.Err, ErrTimeout) {
		t.Fatalf("Err = %v, want ErrTimeout", res.Err)
	}
}

func TestFetchContextCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	res := New(Config{Timeout: 5 * time.Second}).Fetch(ctx, "id", srv.URL, 0, 9)
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if res.Err == nil {
		t.Fatal("Err = nil, want a cancellation error")
	}
}

func TestFetchInvalidRange(t *testing.T) {
	hit := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit <- struct{}{}
	}))
	t.Cleanup(srv.Close)

	f := New(Config{Timeout: time.Second})
	for _, tc := range []struct {
		name       string
		start, end int64
	}{
		{"negative start", -1, 9},
		{"end before start", 10, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := f.Fetch(context.Background(), "id", srv.URL, tc.start, tc.end)
			if res.Success {
				t.Fatal("Success = true, want false")
			}
			if !errors.Is(res.Err, ErrInvalidRange) {
				t.Fatalf("Err = %v, want ErrInvalidRange", res.Err)
			}
		})
	}

	select {
	case <-hit:
		t.Fatal("server was contacted for an invalid range")
	default:
	}
}

func TestFetchMultipleRanges(t *testing.T) {
	content := pattern(1000)
	srv := newRangeServer(t, content)
	f := New(Config{Timeout: 5 * time.Second})

	cases := []ByteRange{
		{Start: 0, End: 0},
		{Start: 0, End: 99},
		{Start: 100, End: 199},
		{Start: 500, End: 999},
		{Start: 999, End: 999},
	}

	for i, r := range cases {
		res := f.Fetch(context.Background(), fmt.Sprintf("req-%d", i), srv.URL, r.Start, r.End)
		if !res.Success {
			t.Fatalf("range %v: Success = false, Err = %v", r, res.Err)
		}
		if res.Range != r {
			t.Errorf("range %v: got Range %v", r, res.Range)
		}
		if want := content[r.Start : r.End+1]; !bytes.Equal(res.Data, want) {
			t.Errorf("range %v: data mismatch", r)
		}
		if res.Bytes != r.Length() {
			t.Errorf("range %v: Bytes = %d, want %d", r, res.Bytes, r.Length())
		}
	}
}
