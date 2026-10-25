package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveRateNowFollowsBaseRateAndCurrentK(t *testing.T) {
	const mib = 1 << 20
	s := New([]byte("abc"))
	s.SetRate(4 * mib)
	if got := s.liveRateNow(); got != 4*mib {
		t.Fatalf("no cap: rate = %d, want %d", got, 4*mib)
	}
	if err := s.SetConcurrencyCap(8*mib, 0.05); err != nil {
		t.Fatal(err)
	}
	s.activeRequests.Add(1)
	if got := s.liveRateNow(); got != 4*mib { // cap(1) = 8 MiB/s > base
		t.Fatalf("k=1: rate = %d, want base %d", got, 4*mib)
	}
	s.activeRequests.Add(7)
	if got, want := s.liveRateNow(), concurrencyCapRate(8*mib, 0.05, 8); got != want {
		t.Fatalf("k=8: rate = %d, want cap %d", got, want)
	}
	s.SetRate(64 << 10) // base drops below the cap: base wins again
	if got := s.liveRateNow(); got != 64<<10 {
		t.Fatalf("after base drop: rate = %d, want %d", got, 64<<10)
	}
	s.activeRequests.Add(-8)
}

func TestWritePacedLiveReadsRateEveryChunk(t *testing.T) {
	calls := 0
	rate := func() int64 { calls++; return 0 } // unlimited: no sleeping
	rec := httptest.NewRecorder()
	const start, end = 100, 100 + 40<<10 - 1 // 40 KiB: chunks of 16, 16, 8 KiB
	if !writePacedLive(context.Background(), rec, start, end, rate, fillLogical) {
		t.Fatal("writePacedLive failed")
	}
	if calls != 3 {
		t.Fatalf("rate read %d times, want 3 (once per chunk)", calls)
	}
	want := make([]byte, end-start+1)
	fillLogical(want, start)
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatal("live-paced body differs from the logical content")
	}
}

func TestLiveRateModeServesCorrectRangesAndReleasesK(t *testing.T) {
	s, err := NewLogical(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	s.SetLiveRate(true)
	if err := s.SetConcurrencyCap(1<<30, 0.05); err != nil { // cap far above any rate
		t.Fatal(err)
	}
	s.SetRequestLogEnabled(true)
	ts := httptest.NewServer(s)
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Set("Range", "bytes=1000-50999")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	want := make([]byte, 50000)
	fillLogical(want, 1000)
	if !bytes.Equal(body, want) {
		t.Fatal("live-rate body differs from the logical content")
	}
	if got := resp.Header.Get("Content-Range"); got != fmt.Sprintf("bytes 1000-50999/%d", 1<<20) {
		t.Fatalf("Content-Range = %q", got)
	}
	if k := s.ActiveRequests(); k != 0 {
		t.Fatalf("active requests after response = %d, want 0", k)
	}
	if log := s.RequestLog(); len(log) != 1 || log[0].ActiveK != 1 {
		t.Fatalf("request log = %+v, want one entry with k = 1", log)
	}
}

func TestLiveRateOffByDefault(t *testing.T) {
	s := New([]byte("abc"))
	if s.liveRate {
		t.Fatal("live-rate mode must be off by default")
	}
	s.SetLiveRate(true)
	s.SetLiveRate(false)
	if s.liveRate {
		t.Fatal("SetLiveRate(false) must turn it off")
	}
}
