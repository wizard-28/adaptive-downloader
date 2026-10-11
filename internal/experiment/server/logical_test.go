package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"testing"
)

// expectedLogical returns the deterministic bytes of the logical object in
// [start, start+n).
func expectedLogical(start int64, n int) []byte {
	b := make([]byte, n)
	fillLogical(b, start)
	return b
}

// fetchLogicalRange issues a Range request and requires a 206 response.
func fetchLogicalRange(t *testing.T, client *http.Client, url string, start, end int64) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return body
}

// discardWriter is an http.ResponseWriter that counts body bytes without
// storing them, so server-side memory use can be measured independently of
// response buffering.
type discardWriter struct {
	header http.Header
	status int
	n      int64
}

func (d *discardWriter) Header() http.Header {
	if d.header == nil {
		d.header = make(http.Header)
	}
	return d.header
}

func (d *discardWriter) Write(p []byte) (int, error) {
	d.n += int64(len(p))
	return len(p), nil
}

func (d *discardWriter) WriteHeader(status int) { d.status = status }

func TestNewLogicalRejectsNonPositiveSize(t *testing.T) {
	for _, size := range []int64{0, -1, math.MinInt64} {
		if _, err := NewLogical(size); !errors.Is(err, ErrInvalidLogicalSize) {
			t.Fatalf("NewLogical(%d) err = %v, want ErrInvalidLogicalSize", size, err)
		}
	}
}

func TestDefaultLogicalSizeConstructible(t *testing.T) {
	srv, err := NewLogical(DefaultLogicalSize)
	if err != nil {
		t.Fatalf("NewLogical(DefaultLogicalSize): %v", err)
	}
	if got := srv.Size(); got != DefaultLogicalSize {
		t.Fatalf("Size = %d, want %d", got, DefaultLogicalSize)
	}
}

// TestLogicalServerRangeSemantics covers small/large/adjacent/overlapping/
// single-byte/end-reaching ranges, Content-Length, Content-Range, and
// end-beyond-size clamping.
func TestLogicalServerRangeSemantics(t *testing.T) {
	const size = int64(1)<<20 + 123
	srv, err := NewLogical(size)
	if err != nil {
		t.Fatalf("NewLogical: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	client := ts.Client()

	cases := []struct {
		name       string
		start, end int64
	}{
		{"small near zero", 0, 63},
		{"large offset", 1 << 19, 1<<19 + 255},
		{"adjacent left", 4096, 8191},
		{"adjacent right", 8192, 12287},
		{"overlapping", 5000, 6999},
		{"single byte", 12345, 12345},
		{"reaches logical end", size - 10, size - 1},
		{"end beyond size is clamped", size - 10, size + 1_000_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantEnd := tc.end
			if wantEnd >= size {
				wantEnd = size - 1
			}
			want := expectedLogical(tc.start, int(wantEnd-tc.start+1))

			req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", tc.start, tc.end))
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusPartialContent {
				t.Fatalf("status = %d, want 206", resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Length"); got != strconv.Itoa(len(want)) {
				t.Fatalf("Content-Length = %q, want %d", got, len(want))
			}
			wantCR := fmt.Sprintf("bytes %d-%d/%d", tc.start, wantEnd, size)
			if got := resp.Header.Get("Content-Range"); got != wantCR {
				t.Fatalf("Content-Range = %q, want %q", got, wantCR)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if !bytes.Equal(body, want) {
				t.Fatalf("body does not match deterministic content (%d bytes)", len(body))
			}
		})
	}
}

// TestLogicalServerDeterministicContent verifies repeated reads, offset
// variation, offset stability, and adjacent-range composition.
func TestLogicalServerDeterministicContent(t *testing.T) {
	const size = int64(1) << 20
	srv, err := NewLogical(size)
	if err != nil {
		t.Fatalf("NewLogical: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	client := ts.Client()

	first := fetchLogicalRange(t, client, ts.URL, 0, 255)
	second := fetchLogicalRange(t, client, ts.URL, 0, 255)
	if !bytes.Equal(first, second) {
		t.Fatal("same range returned different bytes on repeat")
	}
	other := fetchLogicalRange(t, client, ts.URL, 256, 511)
	if bytes.Equal(first, other) {
		t.Fatal("different offsets returned identical bytes")
	}

	single := fetchLogicalRange(t, client, ts.URL, 100, 100)
	if len(single) != 1 || single[0] != first[100] {
		t.Fatal("single-byte read is not offset-stable")
	}

	left := fetchLogicalRange(t, client, ts.URL, 1000, 1999)
	right := fetchLogicalRange(t, client, ts.URL, 2000, 2999)
	combined := fetchLogicalRange(t, client, ts.URL, 1000, 2999)
	if !bytes.Equal(append(left, right...), combined) {
		t.Fatal("adjacent ranges do not compose into the combined range")
	}
}

// TestLogicalServerInvalidRanges verifies invalid requests keep the finite
// server's error semantics: 416 with "bytes */SIZE", no silent clamping.
func TestLogicalServerInvalidRanges(t *testing.T) {
	const size = int64(4096)
	srv, err := NewLogical(size)
	if err != nil {
		t.Fatalf("NewLogical: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	client := ts.Client()

	headers := []string{
		fmt.Sprintf("bytes=%d-%d", size, size+10), // start >= size
		"bytes=10-5",    // end < start
		"bytes=abc-def", // malformed
		"bytes=0-1,5-6", // multiple ranges are not supported
		"bytes=-10",     // suffix ranges are not supported
		"items=0-10",    // non-bytes unit
	}
	for _, h := range headers {
		req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Range", h)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do(%q): %v", h, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
			t.Errorf("Range %q: status = %d, want 416", h, resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Range"); got != fmt.Sprintf("bytes */%d", size) {
			t.Errorf("Range %q: Content-Range = %q, want %q", h, got, fmt.Sprintf("bytes */%d", size))
		}
	}
}

// TestLogicalServerNoRangeStreamsFullObject preserves the finite server's
// no-Range 200 semantics for the logical object (small size for test speed).
func TestLogicalServerNoRangeStreamsFullObject(t *testing.T) {
	const size = int64(4096)
	srv, err := NewLogical(size)
	if err != nil {
		t.Fatalf("NewLogical: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Get(ts.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Length"); got != strconv.FormatInt(size, 10) {
		t.Fatalf("Content-Length = %q, want %d", got, size)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(body, expectedLogical(0, int(size))) {
		t.Fatal("full-body response does not match deterministic content")
	}
}

// TestNewLogicalDoesNotAllocateProportionalMemory proves construction allocates
// a bounded amount independent of the logical size (16 TiB object).
func TestNewLogicalDoesNotAllocateProportionalMemory(t *testing.T) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	srv, err := NewLogical(DefaultLogicalSize)
	if err != nil {
		t.Fatalf("NewLogical: %v", err)
	}
	if got := srv.Size(); got != DefaultLogicalSize {
		t.Fatalf("Size = %d, want %d", got, DefaultLogicalSize)
	}

	runtime.ReadMemStats(&after)
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 1<<20 {
		t.Fatalf("constructing a %d-byte logical object allocated %d bytes; want bounded (< 1 MiB)",
			DefaultLogicalSize, alloc)
	}
}

// TestLogicalServerLargeRangeBoundedMemory streams a 64 MiB range at a huge
// offset through a non-buffering ResponseWriter and requires both the exact
// bytes count and a bounded allocation.
func TestLogicalServerLargeRangeBoundedMemory(t *testing.T) {
	srv, err := NewLogical(DefaultLogicalSize)
	if err != nil {
		t.Fatalf("NewLogical: %v", err)
	}
	const (
		start  = int64(1)<<42 + 7
		length = int64(64) << 20
	)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, start+length-1))

	dw := &discardWriter{}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	srv.ServeHTTP(dw, req)

	runtime.ReadMemStats(&after)
	if dw.status != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", dw.status)
	}
	if dw.n != length {
		t.Fatalf("body bytes = %d, want %d", dw.n, length)
	}
	if got := dw.header.Get("Content-Length"); got != strconv.FormatInt(length, 10) {
		t.Fatalf("Content-Length = %q, want %d", got, length)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 1<<20 {
		t.Fatalf("serving %d bytes allocated %d bytes; want bounded generation (< 1 MiB)", length, alloc)
	}
}

// TestFillLogicalIsOffsetStable verifies the generator's key invariant: the
// byte at an absolute offset is independent of read grouping, alignment, and
// the size of the requested prefix.
func TestFillLogicalIsOffsetStable(t *testing.T) {
	const base = int64(1)<<33 + 5
	full := make([]byte, 1024)
	fillLogical(full, base)

	for _, off := range []int64{0, 1, 7, 8, 9, 63, 511, 1023} {
		one := make([]byte, 1)
		fillLogical(one, base+off)
		if one[0] != full[off] {
			t.Fatalf("byte at offset %d differs between grouped and single reads", base+off)
		}
	}

	chunks := [][]byte{make([]byte, 3), make([]byte, 10), make([]byte, 300), make([]byte, 711)}
	off := int64(0)
	for _, c := range chunks {
		fillLogical(c, base+off)
		off += int64(len(c))
	}
	if joined := bytes.Join(chunks, nil); !bytes.Equal(joined, full) {
		t.Fatal("read grouping changed the generated bytes")
	}
}

// TestFiniteServerPrivateCopyAndSemantics is a focused compatibility check that
// New([]byte) still copies its input and serves finite content unchanged.
func TestFiniteServerPrivateCopyAndSemantics(t *testing.T) {
	src := []byte("0123456789abcdef")
	srv := New(src)
	src[0] = 'X' // the server must keep its private copy

	if got := srv.Size(); got != 16 {
		t.Fatalf("Size = %d, want 16", got)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=4-7")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got := rec.Body.String(); got != "4567" {
		t.Fatalf("body = %q, want %q", got, "4567")
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 4-7/16" {
		t.Fatalf("Content-Range = %q, want %q", got, "bytes 4-7/16")
	}
	if got := rec.Header().Get("Content-Length"); got != "4" {
		t.Fatalf("Content-Length = %q, want %q", got, "4")
	}
}
