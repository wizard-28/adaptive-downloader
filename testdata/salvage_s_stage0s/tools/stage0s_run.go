// Command stage0s_run executes one frozen Stage 0S pilot run.
//
// It is a thin composition of existing validated project infrastructure:
//
//   - internal/experiment/server.NewLogical: deterministic generated content
//     that is never materialized (bounded memory), with the same HTTP Range
//     semantics and application-level pacing as the materialized server;
//   - internal/downloader: fixed-chunk, fixed-concurrency client;
//   - internal/metrics: per-attempt client records.
//
// Frozen mechanism (Salvage Protocol v7): R = 4 Mbps = 500,000 B/s, D = 40 ms,
// FileSize = 512 MiB, N = 4. There is no adaptive switching in Stage 0S: the
// chunk size is fixed for the whole run.
//
// This file lives under testdata/salvage_s_stage0s/tools/ as an execution
// artifact; it does not modify repository source code.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/server"
)

const (
	fileSize    int64 = 536870912 // 512 MiB
	rateBps     int64 = 500000    // 4 Mbps
	delay             = 40 * time.Millisecond
	concurrency       = 4
	timeout           = 10 * time.Minute
	dialTimeout       = 30 * time.Second
	maxRetries        = 3
)

var validArms = map[int64]bool{262144: true, 1048576: true, 4194304: true, 16777216: true}

type recordJSON struct {
	RequestID  string  `json:"request_id"`
	RangeStart int64   `json:"range_start"`
	RangeEnd   int64   `json:"range_end"`
	Bytes      int64   `json:"bytes"`
	DurationNs int64   `json:"duration_ns"`
	Throughput float64 `json:"throughput_bytes_per_second"`
	Success    bool    `json:"success"`
	StatusCode int     `json:"status_code"`
	Attempt    int     `json:"attempt"`
}

type requestEventJSON struct {
	Index      int64 `json:"index"`
	StartNs    int64 `json:"start_unix_nano"`
	EndNs      int64 `json:"end_unix_nano"`
	Rate       int64 `json:"rate_bytes_per_second"`
	RangeStart int64 `json:"range_start"`
	RangeEnd   int64 `json:"range_end"`
	HasRange   bool  `json:"has_range"`
}

type runArtifact struct {
	RunID     string `json:"run_id"`
	Block     int    `json:"block"`
	ArmBytes  int64  `json:"arm_bytes"`
	Order     int    `json:"order_in_block"`
	Protocol  string `json:"protocol"`
	Config    map[string]any `json:"config"`

	StartWallUnixNano int64 `json:"start_wall_unix_nano"`
	EndWallUnixNano   int64 `json:"end_wall_unix_nano"`
	DurationNs        int64 `json:"duration_ns"`

	RunErr             string `json:"run_err"`
	TransferredBytes   int64  `json:"transferred_bytes"`
	TotalAttempts      int64  `json:"total_attempts"`
	SuccessfulAttempts int64  `json:"successful_attempts"`
	FailedAttempts     int64  `json:"failed_attempts"`
	RetryAttempts      int64  `json:"retry_attempts"`

	OutputSizeBytes     int64 `json:"output_size_bytes"`
	CoverageNoMissing   bool  `json:"coverage_no_missing"`
	CoverageNoDuplicate bool  `json:"coverage_no_duplicates"`
	EOFReached          bool  `json:"eof_reached"`
	WorkloadExhausted   bool  `json:"workload_exhausted"`

	Clean         bool   `json:"clean"`
	FailureReason string `json:"failure_reason"`

	ClientRecords    []recordJSON       `json:"client_records"`
	ServerRequestLog []requestEventJSON `json:"server_request_log"`

	GoVersion string `json:"go_version"`
}

func verifyCoverage(records []recordJSON, size int64) (noDuplicates, noMissing bool) {
	type segment struct{ start, end int64 }
	segments := make([]segment, 0, len(records))
	for _, r := range records {
		if r.Success {
			segments = append(segments, segment{start: r.RangeStart, end: r.RangeEnd})
		}
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].start < segments[j].start })
	noDuplicates, noMissing = true, true
	prevEnd := int64(-1)
	for _, s := range segments {
		if s.start <= prevEnd {
			noDuplicates = false
		}
		if s.start != prevEnd+1 {
			noMissing = false
		}
		if s.end > prevEnd {
			prevEnd = s.end
		}
	}
	if prevEnd != size-1 {
		noMissing = false
	}
	return noDuplicates, noMissing
}

func main() {
	block := flag.Int("block", -1, "replicate block index (0-7)")
	arm := flag.Int64("arm", 0, "fixed chunk size in bytes")
	order := flag.Int("order", -1, "position of this condition within the block (0-3)")
	runID := flag.String("run-id", "", "planned run identifier")
	out := flag.String("out", "", "raw artifact output path")
	flag.Parse()

	if *block < 0 || *arm <= 0 || *order < 0 || *runID == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "stage0s_run: -block, -arm, -order, -run-id and -out are required")
		os.Exit(2)
	}
	if !validArms[*arm] {
		fmt.Fprintf(os.Stderr, "stage0s_run: arm %d is not one of the four frozen arms\n", *arm)
		os.Exit(2)
	}

	srv, err := server.NewLogical(fileSize)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stage0s_run: server:", err)
		os.Exit(2)
	}
	srv.SetRate(rateBps)
	srv.SetDelay(delay)
	srv.SetRequestLogEnabled(true)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	dir, err := os.MkdirTemp("", "salvage-s0s-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "stage0s_run: temp dir:", err)
		os.Exit(2)
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	dl, err := downloader.New(downloader.Config{
		URL:         ts.URL,
		OutputPath:  outPath,
		TotalSize:   fileSize,
		Concurrency: concurrency,
		ChunkSize:   *arm,
		Timeout:     timeout,
		DialTimeout: dialTimeout,
		MaxRetries:  maxRetries,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "stage0s_run: downloader:", err)
		os.Exit(2)
	}

	art := runArtifact{
		RunID:    *runID,
		Block:    *block,
		ArmBytes: *arm,
		Order:    *order,
		Protocol: "salvage_s_protocol_v7",
		Config: map[string]any{
			"file_size_bytes":   fileSize,
			"rate_bytes_per_sec": rateBps,
			"delay_ms":          delay.Milliseconds(),
			"concurrency":       concurrency,
			"chunk_bytes":       *arm,
			"timeout_ms":        timeout.Milliseconds(),
			"dial_timeout_ms":   dialTimeout.Milliseconds(),
			"max_retries":       maxRetries,
			"content_source":    "server.NewLogical (deterministic generated, bounded memory)",
			"http":              "HTTP/1.1 (fetcher transport)",
		},
		GoVersion: runtime.Version(),
	}

	start := time.Now()
	runErr := dl.Download(context.Background())
	end := time.Now()
	snap := dl.Metrics()

	art.StartWallUnixNano = start.UnixNano()
	art.EndWallUnixNano = end.UnixNano()
	art.DurationNs = end.Sub(start).Nanoseconds()
	if runErr != nil {
		art.RunErr = runErr.Error()
	}
	art.TransferredBytes = snap.Aggregate.TotalBytes
	art.TotalAttempts = snap.Aggregate.TotalAttempts
	art.SuccessfulAttempts = snap.Aggregate.SuccessfulRequests
	art.FailedAttempts = snap.Aggregate.FailedRequests
	for _, r := range snap.Records {
		if r.Attempt > 1 {
			art.RetryAttempts++
		}
		art.ClientRecords = append(art.ClientRecords, recordJSON{
			RequestID: r.RequestID, RangeStart: r.RangeStart, RangeEnd: r.RangeEnd,
			Bytes: r.Bytes, DurationNs: r.Duration.Nanoseconds(), Throughput: r.Throughput,
			Success: r.Success, StatusCode: r.StatusCode, Attempt: r.Attempt,
		})
	}
	for _, e := range srv.RequestLog() {
		art.ServerRequestLog = append(art.ServerRequestLog, requestEventJSON{
			Index: e.Index, StartNs: e.Start.UnixNano(), EndNs: e.End.UnixNano(),
			Rate: e.Rate, RangeStart: e.Range.Start, RangeEnd: e.Range.End, HasRange: e.HasRange,
		})
	}
	if st, serr := os.Stat(outPath); serr == nil {
		art.OutputSizeBytes = st.Size()
	}
	art.CoverageNoDuplicate, art.CoverageNoMissing = verifyCoverage(art.ClientRecords, fileSize)
	art.EOFReached = runErr == nil && art.TransferredBytes == fileSize &&
		art.OutputSizeBytes == fileSize && art.CoverageNoMissing && art.CoverageNoDuplicate
	art.Clean = art.EOFReached

	switch {
	case runErr != nil:
		art.FailureReason = "download error: " + runErr.Error()
	case art.TransferredBytes != fileSize:
		art.FailureReason = fmt.Sprintf("transferred %d bytes, want %d", art.TransferredBytes, fileSize)
	case art.OutputSizeBytes != fileSize:
		art.FailureReason = fmt.Sprintf("output size %d bytes, want %d", art.OutputSizeBytes, fileSize)
	case !art.CoverageNoMissing || !art.CoverageNoDuplicate:
		art.FailureReason = "range coverage verification failed"
	}

	data, err := json.MarshalIndent(art, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "stage0s_run: marshal:", err)
		os.Exit(2)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "stage0s_run: write:", err)
		os.Exit(2)
	}

	fmt.Printf("run %s block=%d arm=%d order=%d clean=%v duration=%.3fs attempts=%d retries=%d\n",
		*runID, *block, *arm, *order, art.Clean, float64(art.DurationNs)/1e9,
		art.TotalAttempts, art.RetryAttempts)
	if !art.Clean {
		os.Exit(3)
	}
}
