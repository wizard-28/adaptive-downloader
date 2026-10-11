package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*13 + 5) % 256)
	}
	return b
}

// rawGet performs a GET and returns status and body without failing the test,
// so it is safe to call from goroutines.
func rawGet(client *http.Client, url, rangeHeader string, headers map[string]string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

func doRequest(t *testing.T, client *http.Client, url, rangeHeader string, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return resp.StatusCode, resp.Header, body
}

func newTestServer(t *testing.T, content []byte) (*Server, *httptest.Server) {
	t.Helper()
	srv := New(content)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestNormalRangeRequest(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)

	status, header, body := doRequest(t, ts.Client(), ts.URL, "bytes=10-19", nil)

	if status != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", status)
	}
	if got, want := header.Get("Content-Range"), "bytes 10-19/100"; got != want {
		t.Fatalf("Content-Range = %q, want %q", got, want)
	}
	if got := header.Get("Content-Length"); got != "10" {
		t.Fatalf("Content-Length = %q, want 10", got)
	}
	if !bytes.Equal(body, content[10:20]) {
		t.Fatalf("body does not match requested range")
	}

	stats := srv.Stats()
	if stats.TotalRequests != 1 || stats.Successful != 1 || stats.Failed != 0 {
		t.Fatalf("stats = %+v, want 1 total / 1 success / 0 failed", stats)
	}
	if stats.BytesServed != 10 {
		t.Fatalf("BytesServed = %d, want 10", stats.BytesServed)
	}
	if len(stats.RequestedRanges) != 1 || stats.RequestedRanges[0] != (Range{Start: 10, End: 19}) {
		t.Fatalf("RequestedRanges = %+v, want [{10 19}]", stats.RequestedRanges)
	}
}

func TestNoRangeReturnsFullFile(t *testing.T) {
	content := pattern(100)
	_, ts := newTestServer(t, content)

	status, header, body := doRequest(t, ts.Client(), ts.URL, "", nil)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := header.Get("Content-Length"); got != "100" {
		t.Fatalf("Content-Length = %q, want 100", got)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("body does not match full content")
	}
}

func TestInvalidRange(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)

	for _, header := range []string{"bytes=200-300", "bytes=50-10", "bytes=abc", "bytes=10-20,30-40"} {
		t.Run(header, func(t *testing.T) {
			status, hdr, _ := doRequest(t, ts.Client(), ts.URL, header, nil)
			if status != http.StatusRequestedRangeNotSatisfiable {
				t.Fatalf("status = %d, want 416", status)
			}
			if got := hdr.Get("Content-Range"); got != "bytes */100" {
				t.Fatalf("Content-Range = %q, want %q", got, "bytes */100")
			}
		})
	}

	stats := srv.Stats()
	if stats.TotalRequests != 4 || stats.Failed != 4 || stats.Successful != 0 {
		t.Fatalf("stats = %+v, want 4 total / 0 success / 4 failed", stats)
	}
	if stats.BytesServed != 0 {
		t.Fatalf("BytesServed = %d, want 0", stats.BytesServed)
	}
}

func TestConfiguredDelay(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)
	srv.SetDelay(40 * time.Millisecond)

	start := time.Now()
	status, _, body := doRequest(t, ts.Client(), ts.URL, "bytes=0-9", nil)
	elapsed := time.Since(start)

	if status != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", status)
	}
	if !bytes.Equal(body, content[0:10]) {
		t.Fatalf("body mismatch")
	}
	if elapsed < 25*time.Millisecond {
		t.Fatalf("elapsed = %v, want >= 25ms with configured delay", elapsed)
	}
}

func TestRateLimiting(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)
	srv.SetRate(2000) // 100 bytes ~= 50ms

	start := time.Now()
	status, _, _ := doRequest(t, ts.Client(), ts.URL, "bytes=0-99", nil)
	elapsed := time.Since(start)

	if status != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", status)
	}
	if elapsed < 25*time.Millisecond {
		t.Fatalf("elapsed = %v, want >= 25ms at 2000 B/s", elapsed)
	}
}

func TestChangeRateAtRuntime(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)

	srv.SetRate(1000) // 100 bytes ~= 100ms
	start := time.Now()
	doRequest(t, ts.Client(), ts.URL, "bytes=0-99", nil)
	slow := time.Since(start)

	srv.SetRate(200000) // 100 bytes ~= 0.5ms
	start = time.Now()
	doRequest(t, ts.Client(), ts.URL, "bytes=0-99", nil)
	fast := time.Since(start)

	if slow < 40*time.Millisecond {
		t.Fatalf("slow elapsed = %v, want >= 40ms", slow)
	}
	if fast >= slow {
		t.Fatalf("fast elapsed = %v, want < slow elapsed %v after SetRate", fast, slow)
	}
}

func TestDeterministicFailureNext(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)
	srv.FailNext(2)

	if status, _, _ := doRequest(t, ts.Client(), ts.URL, "bytes=0-9", nil); status != http.StatusInternalServerError {
		t.Fatalf("request 1 status = %d, want 500", status)
	}
	if status, _, _ := doRequest(t, ts.Client(), ts.URL, "bytes=0-9", nil); status != http.StatusInternalServerError {
		t.Fatalf("request 2 status = %d, want 500", status)
	}
	if status, _, body := doRequest(t, ts.Client(), ts.URL, "bytes=0-9", nil); status != http.StatusPartialContent {
		t.Fatalf("request 3 status = %d, want 206", status)
	} else if !bytes.Equal(body, content[0:10]) {
		t.Fatalf("request 3 body mismatch")
	}

	stats := srv.Stats()
	if stats.TotalRequests != 3 || stats.Failed != 2 || stats.Successful != 1 {
		t.Fatalf("stats = %+v, want 3 total / 1 success / 2 failed", stats)
	}
	if stats.BytesServed != 10 {
		t.Fatalf("BytesServed = %d, want 10", stats.BytesServed)
	}
}

func TestFailRange(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)
	srv.FailRange(10, 19)

	if status, _, _ := doRequest(t, ts.Client(), ts.URL, "bytes=10-19", nil); status != http.StatusInternalServerError {
		t.Fatalf("matching range status = %d, want 500", status)
	}
	if status, _, _ := doRequest(t, ts.Client(), ts.URL, "bytes=20-29", nil); status != http.StatusPartialContent {
		t.Fatalf("other range status = %d, want 206", status)
	}

	stats := srv.Stats()
	if stats.Failed != 1 || stats.Successful != 1 {
		t.Fatalf("stats = %+v, want 1 failed / 1 success", stats)
	}
}

func TestFailHeader(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)
	srv.FailHeader("X-Request-ID", "abc")

	if status, _, _ := doRequest(t, ts.Client(), ts.URL, "bytes=0-9", map[string]string{"X-Request-ID": "abc"}); status != http.StatusInternalServerError {
		t.Fatalf("matching header status = %d, want 500", status)
	}
	if status, _, _ := doRequest(t, ts.Client(), ts.URL, "bytes=0-9", nil); status != http.StatusPartialContent {
		t.Fatalf("no header status = %d, want 206", status)
	}
}

func TestClearFailures(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)
	srv.FailNext(5)
	srv.ClearFailures()

	if status, _, _ := doRequest(t, ts.Client(), ts.URL, "bytes=0-9", nil); status != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206 after ClearFailures", status)
	}
}

func TestPerRangeDelay(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)
	srv.SetRangeDelay(0, 9, 40*time.Millisecond)

	start := time.Now()
	doRequest(t, ts.Client(), ts.URL, "bytes=0-9", nil)
	slow := time.Since(start)

	start = time.Now()
	doRequest(t, ts.Client(), ts.URL, "bytes=10-19", nil)
	fast := time.Since(start)

	if slow < 25*time.Millisecond {
		t.Fatalf("slow elapsed = %v, want >= 25ms for delayed range", slow)
	}
	if fast >= slow {
		t.Fatalf("fast elapsed = %v, want < slow elapsed %v", fast, slow)
	}
}

func TestConcurrentRequestsAndStats(t *testing.T) {
	const (
		contentSize = 1000
		rangeSize   = 10
		requests    = 50
	)
	content := pattern(contentSize)
	srv, ts := newTestServer(t, content)

	// Concurrently read stats while requests run, to exercise locking.
	stopStats := make(chan struct{})
	var statsWG sync.WaitGroup
	statsWG.Add(1)
	go func() {
		defer statsWG.Done()
		for {
			select {
			case <-stopStats:
				return
			default:
				_ = srv.Stats()
				time.Sleep(time.Millisecond)
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start := int64(i * rangeSize)
			header := fmt.Sprintf("bytes=%d-%d", start, start+rangeSize-1)
			status, body, err := rawGet(ts.Client(), ts.URL, header, nil)
			if err != nil {
				errs <- err
				return
			}
			if status != http.StatusPartialContent {
				errs <- &statusError{status: status}
				return
			}
			if !bytes.Equal(body, content[start:start+rangeSize]) {
				errs <- errBodyMismatch
			}
		}(i)
	}
	wg.Wait()
	close(stopStats)
	statsWG.Wait()

	select {
	case err := <-errs:
		t.Fatalf("concurrent request failed: %v", err)
	default:
	}

	stats := srv.Stats()
	if stats.TotalRequests != requests || stats.Successful != requests || stats.Failed != 0 {
		t.Fatalf("stats = %+v, want %d total / %d success / 0 failed", stats, requests, requests)
	}
	if stats.BytesServed != requests*rangeSize {
		t.Fatalf("BytesServed = %d, want %d", stats.BytesServed, requests*rangeSize)
	}
	if len(stats.RequestedRanges) != requests {
		t.Fatalf("len(RequestedRanges) = %d, want %d", len(stats.RequestedRanges), requests)
	}
	seen := make(map[Range]bool, requests)
	for _, r := range stats.RequestedRanges {
		if seen[r] {
			t.Fatalf("duplicate requested range %v", r)
		}
		seen[r] = true
	}
}

func TestStatsAccounting(t *testing.T) {
	content := pattern(100)
	srv, ts := newTestServer(t, content)
	srv.FailNext(1)

	doRequest(t, ts.Client(), ts.URL, "bytes=0-9", nil)     // 500
	doRequest(t, ts.Client(), ts.URL, "bytes=10-19", nil)   // 206
	doRequest(t, ts.Client(), ts.URL, "bytes=500-600", nil) // 416
	doRequest(t, ts.Client(), ts.URL, "", nil)              // 200 full file

	stats := srv.Stats()
	if stats.TotalRequests != 4 {
		t.Fatalf("TotalRequests = %d, want 4", stats.TotalRequests)
	}
	if stats.Successful != 2 || stats.Failed != 2 {
		t.Fatalf("success/failed = %d/%d, want 2/2", stats.Successful, stats.Failed)
	}
	if stats.TotalRequests != stats.Successful+stats.Failed {
		t.Fatalf("accounting mismatch: total %d != success %d + failed %d", stats.TotalRequests, stats.Successful, stats.Failed)
	}
	if stats.BytesServed != 10+100 {
		t.Fatalf("BytesServed = %d, want 110", stats.BytesServed)
	}
	// Only valid ranged requests are recorded (the 500 and the 206).
	if len(stats.RequestedRanges) != 2 {
		t.Fatalf("len(RequestedRanges) = %d, want 2", len(stats.RequestedRanges))
	}
}

type statusError struct{ status int }

func (e *statusError) Error() string { return fmt.Sprintf("unexpected status %d", e.status) }

var errBodyMismatch = &bodyMismatchError{}

type bodyMismatchError struct{}

func (*bodyMismatchError) Error() string { return "body mismatch" }

// --- Wall-clock rate schedule (experimental) -------------------------------

// fakeClock is a deterministic injectable clock for schedule tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func scheduleTestServer(t *testing.T) (*Server, *fakeClock, time.Time) {
	t.Helper()
	srv := New(pattern(64))
	base := time.Unix(1_700_000_000, 0)
	clk := &fakeClock{t: base}
	srv.now = clk.now
	return srv, clk, base
}

func testSchedule() RateSchedule {
	return RateSchedule{InitialRate: 512 << 10, TransitionAfter: 100 * time.Millisecond, SubsequentRate: 4 << 20}
}

func TestRateScheduleValidation(t *testing.T) {
	cases := map[string]RateSchedule{
		"zero initial":        {InitialRate: 0, SubsequentRate: 4 << 20},
		"negative initial":    {InitialRate: -1, SubsequentRate: 4 << 20},
		"zero subsequent":     {InitialRate: 512 << 10, SubsequentRate: 0},
		"negative subsequent": {InitialRate: 512 << 10, SubsequentRate: -1},
		"negative transition": {InitialRate: 512 << 10, SubsequentRate: 4 << 20, TransitionAfter: -time.Millisecond},
	}
	for name, sc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := sc.Validate(); !errors.Is(err, ErrInvalidRateSchedule) {
				t.Fatalf("Validate() = %v, want ErrInvalidRateSchedule", err)
			}
			srv, _, _ := scheduleTestServer(t)
			if err := srv.SetRateSchedule(sc); !errors.Is(err, ErrInvalidRateSchedule) {
				t.Fatalf("SetRateSchedule() = %v, want ErrInvalidRateSchedule", err)
			}
			if _, _, active := srv.Schedule(); active {
				t.Fatal("invalid schedule became active")
			}
		})
	}
	if err := testSchedule().Validate(); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
}

func TestRateScheduleBeforeAndAfterTransition(t *testing.T) {
	srv, clk, _ := scheduleTestServer(t)
	if err := srv.SetRateSchedule(testSchedule()); err != nil {
		t.Fatalf("SetRateSchedule = %v", err)
	}
	if got := srv.CurrentRate(); got != 512<<10 {
		t.Fatalf("rate before transition = %d, want %d", got, 512<<10)
	}
	clk.add(99 * time.Millisecond)
	if got := srv.CurrentRate(); got != 512<<10 {
		t.Fatalf("rate at 99ms = %d, want %d", got, 512<<10)
	}
	clk.add(1 * time.Millisecond) // exactly 100ms
	if got := srv.CurrentRate(); got != 4<<20 {
		t.Fatalf("rate at exactly transition = %d, want %d (>= transition uses subsequent)", got, 4<<20)
	}
	clk.add(time.Second)
	if got := srv.CurrentRate(); got != 4<<20 {
		t.Fatalf("rate after transition = %d, want %d", got, 4<<20)
	}
}

// TestRateScheduleIndependentOfRequestCount verifies that when a schedule is
// active the selected rate depends only on elapsed time, not on how many
// requests have been served (even if a request-count switch is configured).
func TestRateScheduleIndependentOfRequestCount(t *testing.T) {
	srv, clk, _ := scheduleTestServer(t)
	srv.SetRate(1_000_000)
	srv.SetRateAfter(1, 2_000_000) // would switch after 1 request, if consulted
	if err := srv.SetRateSchedule(testSchedule()); err != nil {
		t.Fatalf("SetRateSchedule = %v", err)
	}
	_, ts := newTestServerFrom(t, srv)
	for i := 0; i < 5; i++ {
		doRequest(t, ts.Client(), ts.URL, "bytes=0-7", nil)
	}
	stats := srv.Stats()
	if stats.TotalRequests != 5 {
		t.Fatalf("TotalRequests = %d, want 5", stats.TotalRequests)
	}
	if !stats.RateScheduleActive {
		t.Fatal("schedule not reported active")
	}
	// The request-count switch would now return 2_000_000, but the schedule
	// must still return the initial rate because no wall-clock time elapsed.
	if stats.CurrentRate != 512<<10 || srv.CurrentRate() != 512<<10 {
		t.Fatalf("rate with 5 requests served = %d/%d, want %d", stats.CurrentRate, srv.CurrentRate(), 512<<10)
	}
	clk.add(100 * time.Millisecond)
	if got := srv.CurrentRate(); got != 4<<20 {
		t.Fatalf("rate after transition = %d, want %d", got, 4<<20)
	}
}

// TestRateScheduleSameElapsedSameRateAcrossSizes verifies that two servers with
// identical schedules but different served request sizes select the same rate at
// the same elapsed time.
func TestRateScheduleSameElapsedSameRateAcrossSizes(t *testing.T) {
	mk := func() (*Server, *fakeClock) {
		srv, clk, _ := scheduleTestServer(t)
		if err := srv.SetRateSchedule(testSchedule()); err != nil {
			t.Fatalf("SetRateSchedule = %v", err)
		}
		return srv, clk
	}
	small, smallClk := mk()
	smallClk.add(50 * time.Millisecond) // elapsed 50ms
	large, largeClk := mk()
	largeClk.add(50 * time.Millisecond) // same elapsed, no request-size input exists
	if a, b := small.CurrentRate(), large.CurrentRate(); a != b || a != 512<<10 {
		t.Fatalf("rates at same elapsed = %d/%d, want %d/%d", a, b, 512<<10, 512<<10)
	}
	smallClk.add(50 * time.Millisecond)
	largeClk.add(50 * time.Millisecond)
	if a, b := small.CurrentRate(), large.CurrentRate(); a != b || a != 4<<20 {
		t.Fatalf("rates at transition = %d/%d, want %d/%d", a, b, 4<<20, 4<<20)
	}
}

// paceSequence simulates issuing fixed-size requests and returns the request
// index and elapsed time at which the subsequent rate is first observed, plus
// the elapsed time at the start of the last request that still saw the initial
// rate.
func paceSequence(srv *Server, clk *fakeClock, base time.Time, reqSize, total int64, initial int64) (firstSubsequent int, elapsedAtFirst time.Duration, lastInitialElapsed time.Duration) {
	firstSubsequent = -1
	served := int64(0)
	idx := 0
	for served < total {
		rate := srv.CurrentRate()
		elapsed := clk.t.Sub(base)
		if rate != initial {
			if firstSubsequent < 0 {
				firstSubsequent = idx
				elapsedAtFirst = elapsed
			}
		} else {
			lastInitialElapsed = elapsed
		}
		clk.add(time.Duration(float64(reqSize) / float64(rate) * float64(time.Second)))
		served += reqSize
		idx++
	}
	return
}

// TestRateScheduleSequenceDiagnostic is the small deterministic diagnostic: the
// same 512KiB/s -> 4MiB/s wall-clock schedule is reached at a different request
// index for a 32KiB sequence and a 1MiB sequence, but the transition occurs at
// the same elapsed time (100ms) in both.
func TestRateScheduleSequenceDiagnostic(t *testing.T) {
	const transition = 100 * time.Millisecond
	const total = 4 << 20

	run := func(reqSize int64) (int, time.Duration, time.Duration) {
		srv, clk, base := scheduleTestServer(t)
		if err := srv.SetRateSchedule(testSchedule()); err != nil {
			t.Fatalf("SetRateSchedule = %v", err)
		}
		return paceSequence(srv, clk, base, reqSize, total, 512<<10)
	}

	smallIdx, smallElapsed, smallLastInitial := run(32 << 10)
	largeIdx, largeElapsed, largeLastInitial := run(1 << 20)

	if smallIdx == largeIdx {
		t.Fatalf("expected different crossing request indices, got %d and %d", smallIdx, largeIdx)
	}
	for _, c := range []struct {
		name                     string
		idx                      int
		elapsed, lastInitialTime time.Duration
	}{{"32KiB", smallIdx, smallElapsed, smallLastInitial}, {"1MiB", largeIdx, largeElapsed, largeLastInitial}} {
		if c.lastInitialTime >= transition {
			t.Fatalf("%s: last initial-rate request started at %v, want < %v", c.name, c.lastInitialTime, transition)
		}
		if c.elapsed < transition {
			t.Fatalf("%s: first subsequent request started at %v, want >= %v", c.name, c.elapsed, transition)
		}
	}
	t.Logf("32KiB sequence: transition observed at request %d (elapsed %v); 1MiB sequence: request %d (elapsed %v); schedule transition time = %v",
		smallIdx, smallElapsed, largeIdx, largeElapsed, transition)
}

// TestSetRateAfterRequestCountUnchanged verifies the existing request-count
// switch semantics are unchanged by the schedule work.
func TestSetRateAfterRequestCountUnchanged(t *testing.T) {
	srv, ts := newTestServer(t, pattern(8))
	srv.SetRate(1_000_000)
	srv.SetRateAfter(2, 2_000_000)

	doRequest(t, ts.Client(), ts.URL, "bytes=0-7", nil) // request 1
	if got := srv.CurrentRate(); got != 1_000_000 {
		t.Fatalf("rate after 1 request = %d, want 1000000", got)
	}
	doRequest(t, ts.Client(), ts.URL, "bytes=0-7", nil) // request 2
	if got := srv.CurrentRate(); got != 1_000_000 {
		t.Fatalf("rate after 2 requests = %d, want 1000000 (switch after 2)", got)
	}
	doRequest(t, ts.Client(), ts.URL, "bytes=0-7", nil) // request 3
	if got := srv.CurrentRate(); got != 2_000_000 {
		t.Fatalf("rate after 3 requests = %d, want 2000000", got)
	}
}

func TestClearRateScheduleRestoresPreviousBehavior(t *testing.T) {
	srv, _, _ := scheduleTestServer(t)
	srv.SetRate(1_000_000)
	srv.SetRateAfter(0, 3_000_000)
	if err := srv.SetRateSchedule(testSchedule()); err != nil {
		t.Fatalf("SetRateSchedule = %v", err)
	}
	if got := srv.CurrentRate(); got != 512<<10 {
		t.Fatalf("scheduled rate = %d, want %d", got, 512<<10)
	}
	srv.ClearRateSchedule()
	if _, _, active := srv.Schedule(); active {
		t.Fatal("schedule still active after ClearRateSchedule")
	}
	// rateSwitchAt=0 means no request-count switch, so the fixed rate applies.
	if got := srv.CurrentRate(); got != 1_000_000 {
		t.Fatalf("rate after clear = %d, want 1000000", got)
	}
}

// TestRateScheduleConcurrentConsistency exercises concurrent CurrentRate/Stats
// reads (race detector) and checks the selected rate is consistent.
func TestRateScheduleConcurrentConsistency(t *testing.T) {
	srv, clk, _ := scheduleTestServer(t)
	if err := srv.SetRateSchedule(testSchedule()); err != nil {
		t.Fatalf("SetRateSchedule = %v", err)
	}
	clk.add(50 * time.Millisecond) // before transition

	const workers = 64
	results := make([]int64, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				results[i] = srv.CurrentRate()
			} else {
				results[i] = srv.Stats().CurrentRate
			}
		}(i)
	}
	wg.Wait()
	for i, got := range results {
		if got != 512<<10 {
			t.Fatalf("worker %d rate = %d, want %d", i, got, 512<<10)
		}
	}
}

// newTestServerFrom wraps an already-configured server in an httptest server.
func newTestServerFrom(t *testing.T, srv *Server) (*Server, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestRequestLogDisabledByDefault(t *testing.T) {
	srv, ts := newTestServer(t, pattern(8))
	doRequest(t, ts.Client(), ts.URL, "bytes=0-7", nil)
	if got := srv.RequestLog(); len(got) != 0 {
		t.Fatalf("RequestLog = %d events, want 0 when disabled", len(got))
	}
}

func TestRequestLogRecordsTimingRateAndRange(t *testing.T) {
	srv, clk, base := scheduleTestServer(t)
	if err := srv.SetRateSchedule(testSchedule()); err != nil {
		t.Fatalf("SetRateSchedule = %v", err)
	}
	srv.SetRequestLogEnabled(true)
	_, ts := newTestServerFrom(t, srv)

	doRequest(t, ts.Client(), ts.URL, "bytes=0-7", nil)
	clk.add(100 * time.Millisecond)
	doRequest(t, ts.Client(), ts.URL, "bytes=8-15", nil)

	log := srv.RequestLog()
	if len(log) != 2 {
		t.Fatalf("RequestLog = %d events, want 2", len(log))
	}
	if log[0].Index != 1 || log[1].Index != 2 {
		t.Fatalf("event indexes = %d,%d, want 1,2", log[0].Index, log[1].Index)
	}
	if log[0].Rate != 512<<10 || log[1].Rate != 4<<20 {
		t.Fatalf("event rates = %d,%d, want %d,%d", log[0].Rate, log[1].Rate, 512<<10, 4<<20)
	}
	if !log[0].HasRange || log[0].Range != (Range{Start: 0, End: 7}) {
		t.Fatalf("event 0 range = %+v hasRange=%v", log[0].Range, log[0].HasRange)
	}
	if log[1].Start.Sub(base) != 100*time.Millisecond {
		t.Fatalf("event 1 start = %v after base, want 100ms", log[1].Start.Sub(base))
	}
	if log[0].End.Before(log[0].Start) {
		t.Fatalf("event 0 end %v before start %v", log[0].End, log[0].Start)
	}
}
