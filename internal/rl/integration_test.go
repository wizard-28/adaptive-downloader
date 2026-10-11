package rl_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"adaptivedl/internal/downloader"
	expserver "adaptivedl/internal/experiment/server"
	"adaptivedl/internal/rl"
)

func contentPattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*31 + 7) % 256)
	}
	return b
}

func integrationConfig() rl.Config {
	return rl.Config{
		MinConcurrency:     1,
		MaxConcurrency:     4,
		MinChunkSize:       50,
		MaxChunkSize:       200,
		MaxThroughput:      1e9,
		MaxRequestDuration: 5 * time.Second,
		CounterScale:       1000,
	}
}

// runWithAction downloads content using the RL adapter wrapping
// AlwaysActionPolicy(action), and returns the downloader, the server, and the
// output path.
func runWithAction(t *testing.T, content []byte, action rl.ActionIndex) (*downloader.Downloader, *expserver.Server, string) {
	t.Helper()

	srv := expserver.New(content)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	out := filepath.Join(t.TempDir(), "out.bin")
	adapter, err := rl.NewAdapter(rl.AlwaysActionPolicy{Action: action}, integrationConfig())
	if err != nil {
		t.Fatalf("NewAdapter = %v", err)
	}
	dl, err := downloader.New(downloader.Config{
		URL:              ts.URL,
		OutputPath:       out,
		TotalSize:        int64(len(content)),
		Concurrency:      1,
		ChunkSize:        50,
		MinConcurrency:   1,
		MaxConcurrency:   4,
		MinChunkSize:     50,
		MaxChunkSize:     200,
		InitialChunkSize: 50,
		ChunkSizeStep:    50,
		MaxRetries:       3,
		Timeout:          5 * time.Second,
		PolicyWindowSize: 4,
		Policy:           adapter,
	})
	if err != nil {
		t.Fatalf("downloader.New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}
	return dl, srv, out
}

func assertOutputAndRanges(t *testing.T, out string, content []byte, srv *expserver.Server) {
	t.Helper()

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile = %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("output does not match source content")
	}

	ranges := srv.Stats().RequestedRanges
	sorted := append([]expserver.Range(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	seen := make(map[expserver.Range]bool, len(sorted))
	next := int64(0)
	for _, r := range sorted {
		if seen[r] {
			t.Fatalf("duplicate range %v", r)
		}
		seen[r] = true
		if r.Start != next {
			t.Fatalf("range gap/overlap at %d: %v", next, r)
		}
		next = r.End + 1
	}
	if next != int64(len(content)) {
		t.Fatalf("covered %d bytes, want %d", next, len(content))
	}
}

func TestIntegrationIncreaseChunkSize(t *testing.T) {
	content := contentPattern(400)
	dl, srv, out := runWithAction(t, content, rl.ActionIncreaseChunkSize)

	assertOutputAndRanges(t, out, content, srv)

	if dl.ChunkSize() <= 50 {
		t.Fatalf("ChunkSize() = %d, want > 50 (adapter should increase chunk)", dl.ChunkSize())
	}
	if dl.Concurrency() != 1 {
		t.Fatalf("Concurrency() = %d, want 1 (chunk action must not change concurrency)", dl.Concurrency())
	}
	if agg := dl.Metrics().Aggregate; agg.TotalAttempts == 0 || agg.SuccessfulRequests != agg.TotalAttempts {
		t.Fatalf("metrics = %+v, want all attempts successful", agg)
	}
}

func TestIntegrationIncreaseConcurrency(t *testing.T) {
	content := contentPattern(400)
	dl, srv, out := runWithAction(t, content, rl.ActionIncreaseConcurrency)

	assertOutputAndRanges(t, out, content, srv)

	if dl.Concurrency() <= 1 {
		t.Fatalf("Concurrency() = %d, want > 1 (adapter should increase concurrency)", dl.Concurrency())
	}
	if dl.ChunkSize() != 50 {
		t.Fatalf("ChunkSize() = %d, want 50 (concurrency action must not change chunk)", dl.ChunkSize())
	}
}

func TestIntegrationNoChange(t *testing.T) {
	content := contentPattern(400)
	dl, srv, out := runWithAction(t, content, rl.ActionNone)

	assertOutputAndRanges(t, out, content, srv)

	if dl.Concurrency() != 1 {
		t.Fatalf("Concurrency() = %d, want 1", dl.Concurrency())
	}
	if dl.ChunkSize() != 50 {
		t.Fatalf("ChunkSize() = %d, want 50", dl.ChunkSize())
	}
}
