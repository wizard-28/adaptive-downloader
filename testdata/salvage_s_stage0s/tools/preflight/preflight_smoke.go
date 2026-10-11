// Command preflight_smoke is a Stage 0S infrastructure smoke test.
//
// It exercises the exact same code path as stage0s_run.go (logical server with
// per-request rate + fixed delay + request log; fixed-chunk fixed-concurrency
// downloader) but with an 8 MiB object instead of the frozen 512 MiB, so it
// completes in a few seconds. It is NOT a pilot observation and is excluded
// from the Stage 0S run ledger; it exists only to verify the infrastructure
// before Run 1.
package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/server"
)

func main() {
	const (
		smokeSize   int64 = 8 << 20
		smokeRate   int64 = 500000
		smokeDelay        = 40 * time.Millisecond
		smokeChunk  int64 = 1 << 20
		smokeConc         = 4
	)
	srv, err := server.NewLogical(smokeSize)
	if err != nil {
		fmt.Fprintln(os.Stderr, "preflight: server:", err)
		os.Exit(1)
	}
	srv.SetRate(smokeRate)
	srv.SetDelay(smokeDelay)
	srv.SetRequestLogEnabled(true)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	dir, err := os.MkdirTemp("", "salvage-s0s-preflight-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "preflight: temp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	dl, err := downloader.New(downloader.Config{
		URL: ts.URL, OutputPath: filepath.Join(dir, "out.bin"), TotalSize: smokeSize,
		Concurrency: smokeConc, ChunkSize: smokeChunk,
		Timeout: 10 * time.Minute, DialTimeout: 30 * time.Second, MaxRetries: 3,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "preflight: downloader:", err)
		os.Exit(1)
	}
	start := time.Now()
	runErr := dl.Download(context.Background())
	dur := time.Since(start)
	snap := dl.Metrics()
	log := srv.RequestLog()
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "preflight: download:", runErr)
		os.Exit(1)
	}
	if snap.Aggregate.TotalBytes != smokeSize {
		fmt.Fprintf(os.Stderr, "preflight: bytes %d want %d\n", snap.Aggregate.TotalBytes, smokeSize)
		os.Exit(1)
	}
	if len(log) == 0 {
		fmt.Fprintln(os.Stderr, "preflight: empty server request log")
		os.Exit(1)
	}
	first := log[0]
	fmt.Printf("preflight OK: size=%d duration=%.3fs attempts=%d server_requests=%d first_service=%.3fs\n",
		smokeSize, dur.Seconds(), snap.Aggregate.TotalAttempts, len(log), first.End.Sub(first.Start).Seconds())
}
