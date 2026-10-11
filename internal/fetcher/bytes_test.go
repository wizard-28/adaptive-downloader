package fetcher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// thresholdObserver records events into a ByteMeter and lets tests wait
// deterministically until a byte threshold has been observed (no sleeps).
type thresholdObserver struct {
	meter *ByteMeter

	mu      sync.Mutex
	changed chan struct{}
	total   int64
}

func newThresholdObserver() *thresholdObserver {
	return &thresholdObserver{meter: NewByteMeter(), changed: make(chan struct{})}
}

func (o *thresholdObserver) ObserveBytes(e ByteEvent) {
	o.meter.ObserveBytes(e)
	o.mu.Lock()
	o.total += e.Delta
	close(o.changed)
	o.changed = make(chan struct{})
	o.mu.Unlock()
}

func (o *thresholdObserver) waitAtLeast(t *testing.T, n int64) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		o.mu.Lock()
		got := o.total
		if got >= n {
			o.mu.Unlock()
			return
		}
		ch := o.changed
		o.mu.Unlock()
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("timed out waiting for %d observed bytes (got %d)", n, got)
		}
	}
}

// stagedRangeServer serves a Range response in two halves: it writes the first
// half, flushes and signals firstHalf, then blocks until releaseAll before
// writing the second half. This makes an artificial interval boundary
// deterministic.
type stagedRangeServer struct {
	content   []byte
	firstHalf chan struct{}
	release   chan struct{}
	halfOnce  sync.Once
	relOnce   sync.Once
}

func newStagedRangeServer(content []byte) *stagedRangeServer {
	return &stagedRangeServer{
		content:   content,
		firstHalf: make(chan struct{}),
		release:   make(chan struct{}),
	}
}

func (s *stagedRangeServer) releaseAll() { s.relOnce.Do(func() { close(s.release) }) }

func (s *stagedRangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start, end, ok := parseTestRange(r.Header.Get("Range"))
	if !ok || start < 0 || end < start || start >= int64(len(s.content)) {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if end >= int64(len(s.content)) {
		end = int64(len(s.content)) - 1
	}
	body := s.content[start : end+1]
	split := len(body) / 2

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.content)))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body[:split])
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	s.halfOnce.Do(func() { close(s.firstHalf) })
	<-s.release
	_, _ = w.Write(body[split:])
}

// TestFetchByteAccountingAcrossIntervals delivers one Range response across
// two controlled intervals and verifies bytes are attributed to both: interval
// A (before release) and interval B (after release). The request completes only
// after B, so completion time does not determine attribution.
func TestFetchByteAccountingAcrossIntervals(t *testing.T) {
	content := pattern(600)
	staged := newStagedRangeServer(content)
	srv := httptest.NewServer(staged)
	t.Cleanup(srv.Close)
	t.Cleanup(staged.releaseAll)

	obs := newThresholdObserver()
	f := New(Config{Timeout: 5 * time.Second, ByteObserver: obs})

	done := make(chan Result, 1)
	go func() {
		done <- f.Fetch(context.Background(), "req-epochs", srv.URL, 0, int64(len(content))-1)
	}()

	<-staged.firstHalf
	const split = 300
	obs.waitAtLeast(t, split)

	bytesA := obs.meter.Total()
	if bytesA != split {
		t.Fatalf("interval A bytes = %d, want %d", bytesA, split)
	}
	if bytesA == 0 {
		t.Fatal("interval A received no bytes")
	}
	select {
	case res := <-done:
		t.Fatalf("request completed during interval A: success=%v err=%v", res.Success, res.Err)
	default:
	}

	staged.releaseAll()

	var res Result
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Fetch did not complete after release")
	}
	bytesB := obs.meter.Total() - bytesA
	if bytesB != int64(len(content))-split {
		t.Fatalf("interval B bytes = %d, want %d", bytesB, int64(len(content))-split)
	}
	if bytesB == 0 {
		t.Fatal("interval B received no bytes")
	}
	if !res.Success {
		t.Fatalf("Success = false, Err = %v", res.Err)
	}
	if res.Bytes != int64(len(content)) {
		t.Fatalf("Result.Bytes = %d, want %d", res.Bytes, len(content))
	}
	if bytesA+bytesB != res.Bytes {
		t.Fatalf("interval bytes %d+%d != Result.Bytes %d", bytesA, bytesB, res.Bytes)
	}
}

// TestFetchByteAccountingBeforeCompletion proves bytes are observable while the
// request is still in flight: received bytes > 0 with zero completed requests.
func TestFetchByteAccountingBeforeCompletion(t *testing.T) {
	content := pattern(600)
	staged := newStagedRangeServer(content)
	srv := httptest.NewServer(staged)
	t.Cleanup(srv.Close)
	t.Cleanup(staged.releaseAll)

	obs := newThresholdObserver()
	f := New(Config{Timeout: 5 * time.Second, ByteObserver: obs})

	done := make(chan Result, 1)
	go func() {
		done <- f.Fetch(context.Background(), "req-inflight", srv.URL, 0, int64(len(content))-1)
	}()

	<-staged.firstHalf
	obs.waitAtLeast(t, 300)
	if got := obs.meter.Total(); got != 300 {
		t.Fatalf("observed bytes = %d, want 300 before completion", got)
	}
	// Zero completed requests, nonzero bytes.
	select {
	case res := <-done:
		t.Fatalf("Fetch completed early: success=%v err=%v", res.Success, res.Err)
	default:
	}

	staged.releaseAll()
	select {
	case res := <-done:
		if !res.Success {
			t.Fatalf("Success = false, Err = %v", res.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Fetch did not complete after release")
	}
}

// readStep is one scripted Read result.
type readStep struct {
	data []byte
	err  error
}

// scriptedReader returns the scripted steps in order, then io.EOF.
type scriptedReader struct {
	steps []readStep
}

func (s *scriptedReader) Read(p []byte) (int, error) {
	if len(s.steps) == 0 {
		return 0, io.EOF
	}
	st := s.steps[0]
	s.steps = s.steps[1:]
	return copy(p, st.data), st.err
}

// TestCountingReaderCountsPartialReadThenError verifies that n > 0 bytes are
// counted before a read error is returned and the error stays visible.
func TestCountingReaderCountsPartialReadThenError(t *testing.T) {
	sentinel := errors.New("stream failure")
	src := &scriptedReader{steps: []readStep{{data: []byte("hello world"), err: sentinel}}}
	meter := NewByteMeter()
	var conn connIDHolder
	r := &countingReader{r: src, requestID: "req-partial", conn: &conn, observer: meter}

	buf := make([]byte, 64)
	n, err := r.Read(buf)
	if n != 11 {
		t.Fatalf("Read n = %d, want 11", n)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Read err = %v, want sentinel", err)
	}
	if !bytes.Equal(buf[:n], []byte("hello world")) {
		t.Fatalf("Read data = %q, want %q", buf[:n], "hello world")
	}

	events := meter.Snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	e := events[0]
	if e.Delta != 11 || e.RequestCumulative != 11 || e.AggregateCumulative != 11 {
		t.Fatalf("event = %+v, want delta/cumulative 11", e)
	}
	if e.RequestID != "req-partial" {
		t.Fatalf("RequestID = %q, want req-partial", e.RequestID)
	}
	if meter.Total() != 11 {
		t.Fatalf("Total = %d, want 11", meter.Total())
	}
}

// TestCountingReaderCountsBytesReturnedWithEOF verifies n > 0 with io.EOF still
// counts, and a zero-byte error read does not create an event.
func TestCountingReaderCountsBytesReturnedWithEOF(t *testing.T) {
	src := &scriptedReader{steps: []readStep{{data: []byte("tail"), err: io.EOF}}}
	meter := NewByteMeter()
	var conn connIDHolder
	r := &countingReader{r: src, requestID: "req-eof", conn: &conn, observer: meter}

	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if n != 4 || !errors.Is(err, io.EOF) {
		t.Fatalf("Read = (%d, %v), want (4, EOF)", n, err)
	}
	if meter.Total() != 4 {
		t.Fatalf("Total = %d, want 4", meter.Total())
	}

	n, err = r.Read(buf)
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("second Read = (%d, %v), want (0, EOF)", n, err)
	}
	if got := len(meter.Snapshot()); got != 1 {
		t.Fatalf("events = %d, want 1 (zero-byte read must not be recorded)", got)
	}
}

// TestFetchByteAccountingKeepsBytesFromFailedResponse verifies bytes read from
// a response that later fails are not lost: the server declares 100 bytes but
// sends 50, so Fetch fails while the meter must still hold 50.
func TestFetchByteAccountingKeepsBytesFromFailedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-99/100")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(bytes.Repeat([]byte{7}, 50))
	}))
	t.Cleanup(srv.Close)

	meter := NewByteMeter()
	f := New(Config{Timeout: 3 * time.Second, ByteObserver: meter})
	res := f.Fetch(context.Background(), "req-failed", srv.URL, 0, 99)
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if res.Err == nil {
		t.Fatal("Err = nil, want a read error")
	}
	if res.Bytes != 50 {
		t.Fatalf("Result.Bytes = %d, want 50", res.Bytes)
	}
	if meter.Total() != 50 {
		t.Fatalf("meter Total = %d, want 50 (bytes must survive the failure)", meter.Total())
	}
}

// TestFetchByteAccountingMatchesResult verifies request totals, stream
// monotonicity and connection identity for a successful Range.
func TestFetchByteAccountingMatchesResult(t *testing.T) {
	content := pattern(1000)
	srv := newRangeServer(t, content)
	meter := NewByteMeter()
	f := New(Config{Timeout: 2 * time.Second, ByteObserver: meter})

	res := f.Fetch(context.Background(), "req-final", srv.URL, 100, 399)
	if !res.Success {
		t.Fatalf("Success = false, Err = %v", res.Err)
	}
	if res.Bytes != 300 {
		t.Fatalf("Result.Bytes = %d, want 300", res.Bytes)
	}
	if meter.Total() != res.Bytes {
		t.Fatalf("meter Total = %d, want %d", meter.Total(), res.Bytes)
	}

	events := meter.Snapshot()
	if len(events) == 0 {
		t.Fatal("no byte events recorded")
	}
	var cumulative int64
	for i, e := range events {
		if e.RequestID != "req-final" {
			t.Fatalf("event %d RequestID = %q", i, e.RequestID)
		}
		if e.Delta <= 0 {
			t.Fatalf("event %d Delta = %d, want > 0", i, e.Delta)
		}
		if e.ConnID == "" {
			t.Fatalf("event %d has empty ConnID", i)
		}
		cumulative += e.Delta
		if e.RequestCumulative != cumulative {
			t.Fatalf("event %d RequestCumulative = %d, want %d", i, e.RequestCumulative, cumulative)
		}
		if e.AggregateCumulative != cumulative {
			t.Fatalf("event %d AggregateCumulative = %d, want %d", i, e.AggregateCumulative, cumulative)
		}
		if i > 0 && e.Time.Before(events[i-1].Time) {
			t.Fatalf("event %d timestamp went backwards", i)
		}
	}
	if cumulative != res.Bytes {
		t.Fatalf("sum of deltas = %d, want %d", cumulative, res.Bytes)
	}
}

// TestFetchByteAccountingDistinguishesConcurrentRequests runs two simultaneous
// HTTP/1.1 Range requests behind a server-side pair barrier and verifies no
// bytes are attributed to the wrong request, none are lost, request identity
// is preserved, and the stream timestamps are nondecreasing.
func TestFetchByteAccountingDistinguishesConcurrentRequests(t *testing.T) {
	content := pattern(1000)
	var mu sync.Mutex
	arrived := 0
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end, ok := parseTestRange(r.Header.Get("Range"))
		if !ok || start < 0 || end < start || start >= int64(len(content)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= int64(len(content)) {
			end = int64(len(content)) - 1
		}
		mu.Lock()
		arrived++
		if arrived == 2 {
			close(ready)
		}
		mu.Unlock()
		<-ready

		body := content[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	meter := NewByteMeter()
	f := New(Config{Timeout: 5 * time.Second, ByteObserver: meter})

	cases := []struct {
		id         string
		start, end int64
		want       int64
	}{
		{"req-a", 0, 199, 200},
		{"req-b", 500, 999, 500},
	}
	errs := make(chan error, len(cases))
	var wg sync.WaitGroup
	for _, tc := range cases {
		tc := tc
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := f.Fetch(context.Background(), tc.id, srv.URL, tc.start, tc.end)
			if !res.Success {
				errs <- fmt.Errorf("%s: Success=false, Err=%v", tc.id, res.Err)
				return
			}
			if res.Bytes != tc.want {
				errs <- fmt.Errorf("%s: Bytes=%d, want %d", tc.id, res.Bytes, tc.want)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}

	events := meter.Snapshot()
	perRequest := make(map[string]int64)
	connByRequest := make(map[string]string)
	var total int64
	for i, e := range events {
		perRequest[e.RequestID] += e.Delta
		total += e.Delta
		if e.ConnID == "" {
			t.Errorf("event %d has empty ConnID", i)
		}
		if prev := connByRequest[e.RequestID]; prev != "" && prev != e.ConnID {
			t.Errorf("request %s used connections %s and %s", e.RequestID, prev, e.ConnID)
		}
		connByRequest[e.RequestID] = e.ConnID
		if i > 0 && e.Time.Before(events[i-1].Time) {
			t.Errorf("timestamp went backwards at event %d", i)
		}
	}
	for _, tc := range cases {
		if got := perRequest[tc.id]; got != tc.want {
			t.Errorf("request %s accounted bytes = %d, want %d", tc.id, got, tc.want)
		}
	}
	if total != 700 {
		t.Errorf("aggregate bytes = %d, want 700", total)
	}
	if connByRequest["req-a"] == connByRequest["req-b"] {
		t.Error("simultaneous HTTP/1.1 requests were attributed to the same connection")
	}
}
