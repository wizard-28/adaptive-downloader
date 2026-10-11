package downloader

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"adaptivedl/internal/fetcher"
)

// TestDownloadByteAccountingAggregateUnderConcurrency runs a real download with
// C=4 simultaneous HTTP/1.1 flows against a deterministically gated server and
// verifies the incremental accounting: aggregate total, per-request totals
// against the metrics records, per-request cumulative monotonicity, distinct
// connection identities, stream timestamp monotonicity, and output correctness.
func TestDownloadByteAccountingAggregateUnderConcurrency(t *testing.T) {
	const (
		concurrency = 4
		chunkSize   = 64
	)
	content := pattern(1024)
	gs := newGatedServer(content)
	srv := httptest.NewServer(gs)
	t.Cleanup(srv.Close)

	meter := fetcher.NewByteMeter()
	out := filepath.Join(t.TempDir(), "out.bin")
	d, err := New(Config{
		URL:          srv.URL,
		OutputPath:   out,
		TotalSize:    int64(len(content)),
		Concurrency:  concurrency,
		ChunkSize:    chunkSize,
		Timeout:      5 * time.Second,
		MaxRetries:   2,
		ByteObserver: meter,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- d.Download(context.Background()) }()

	// Deterministically hold C requests in flight, then release all.
	gs.waitForActive(t, concurrency)
	gs.releaseAll()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Download: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Download did not finish after release")
	}

	if maxActive, _ := gs.snapshot(); maxActive != concurrency {
		t.Errorf("max simultaneous requests = %d, want %d", maxActive, concurrency)
	}
	if got := meter.Total(); got != int64(len(content)) {
		t.Errorf("meter Total = %d, want %d", got, len(content))
	}

	events := meter.Snapshot()
	var running int64
	perRequest := make(map[string]int64)
	connByRequest := make(map[string]string)
	conns := make(map[string]bool)
	for i, e := range events {
		if e.Delta <= 0 {
			t.Errorf("event %d Delta = %d, want > 0", i, e.Delta)
		}
		running += e.Delta
		if e.AggregateCumulative != running {
			t.Errorf("event %d AggregateCumulative = %d, want %d", i, e.AggregateCumulative, running)
		}
		if want := perRequest[e.RequestID] + e.Delta; e.RequestCumulative != want {
			t.Errorf("event %d RequestCumulative = %d, want %d", i, e.RequestCumulative, want)
		}
		perRequest[e.RequestID] += e.Delta
		if e.ConnID == "" {
			t.Errorf("event %d has empty ConnID", i)
		}
		if prev := connByRequest[e.RequestID]; prev != "" && prev != e.ConnID {
			t.Errorf("request %s used connections %s and %s", e.RequestID, prev, e.ConnID)
		}
		connByRequest[e.RequestID] = e.ConnID
		conns[e.ConnID] = true
		if i > 0 && e.Time.Before(events[i-1].Time) {
			t.Errorf("timestamp went backwards at event %d", i)
		}
	}
	if running != int64(len(content)) {
		t.Errorf("sum of deltas = %d, want %d", running, len(content))
	}
	if n := len(conns); n != concurrency {
		t.Errorf("distinct connection identities = %d, want %d", n, concurrency)
	}

	records := d.Metrics().Records
	if want := len(content) / chunkSize; len(records) != want {
		t.Fatalf("attempt records = %d, want %d", len(records), want)
	}
	for _, rec := range records {
		if !rec.Success {
			t.Fatalf("attempt %s not successful: status=%d", rec.RequestID, rec.StatusCode)
		}
		if got := perRequest[rec.RequestID]; got != rec.Bytes {
			t.Errorf("request %s accounted bytes = %d, metrics record = %d", rec.RequestID, got, rec.Bytes)
		}
	}
	assertFileEquals(t, out, content)
}
