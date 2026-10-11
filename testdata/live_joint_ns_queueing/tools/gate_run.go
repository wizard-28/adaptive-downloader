// Command gate_run executes ONE live mechanism-gate download on the shared
// bottleneck: a fixed-policy internal downloader (N, S) fetching a logical
// 32 MiB object from a local HTTP server with NO application-level pacing.
// The bottleneck and background traffic are controlled by the Python driver
// (tc tbf on lo + iperf3); this tool only runs the download and records
// measurements.
//
// Execution artifact only; no repository source is modified.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/server"
)

const logicalSeed = uint64(0x9E3779B97F4A7C15)

type reqRecord struct {
	Index      int64 `json:"index"`
	StartMs    int64 `json:"start_ms"`
	EndMs      int64 `json:"end_ms"`
	Rate       int64 `json:"rate_bytes_per_second"`
	RangeStart int64 `json:"range_start"`
	RangeEnd   int64 `json:"range_end"`
	HasRange   bool  `json:"has_range"`
}

type result struct {
	RunID           string         `json:"run_id"`
	System          string         `json:"system"`
	Config          map[string]any `json:"config"`
	CompletionMs    float64        `json:"completion_ms"`
	WallclockBps    float64        `json:"wallclock_bytes_per_second"`
	OutputSize      int64          `json:"output_size_bytes"`
	OutputSHA256    string         `json:"output_sha256"`
	ContentVerified bool           `json:"content_verified"`
	ClientRequests  int64          `json:"client_requests"`
	ClientSuccess   int64          `json:"client_successful"`
	ClientFailed    int64          `json:"client_failed"`
	ClientRetries   int64          `json:"client_retries"`
	ClientBytes     int64          `json:"client_bytes"`
	ServerRequests  []reqRecord    `json:"server_requests"`
	TotalRequests   int64          `json:"total_requests"`
	SuccessfulReqs  int64          `json:"successful_requests"`
	FailedReqs      int64          `json:"failed_requests"`
	ServerBytes     int64          `json:"server_bytes"`
	Err             string         `json:"err"`
}

func mixLogicalWord(word uint64) uint64 {
	z := word*0x9E3779B97F4A7C15 + logicalSeed
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func fillLogical(dst []byte, off int64) {
	for len(dst) > 0 {
		z := mixLogicalWord(uint64(off >> 3))
		shift := uint(off&7) * 8
		n := 8 - int(off&7)
		if n > len(dst) {
			n = len(dst)
		}
		for i := 0; i < n; i++ {
			dst[i] = byte(z >> (shift + uint(i)*8))
		}
		dst = dst[n:]
		off += int64(n)
	}
}

func verifyContent(path string, size int64) (bool, string) {
	f, err := os.Open(path)
	if err != nil {
		return false, ""
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	exp := make([]byte, 1<<20)
	var off int64
	for off < size {
		n, err := f.Read(buf)
		if n > 0 {
			fillLogical(exp[:n], off)
			h.Write(buf[:n])
			if !equalBytes(buf[:n], exp[:n]) {
				return false, hex.EncodeToString(h.Sum(nil))
			}
			off += int64(n)
		}
		if err != nil {
			break
		}
	}
	return off == size, hex.EncodeToString(h.Sum(nil))
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func main() {
	c := flag.Int("n", 4, "concurrency")
	chunk := flag.Int64("chunk", 1<<20, "chunk size bytes")
	fileSize := flag.Int64("file-size", 32<<20, "file size bytes")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	retries := flag.Int("retries", 3, "max retries")
	out := flag.String("out", "", "result JSON path")
	runID := flag.String("run-id", "", "run identifier")
	flag.Parse()
	if *out == "" || *runID == "" {
		fmt.Fprintln(os.Stderr, "gate_run: -out and -run-id are required")
		os.Exit(2)
	}

	res := result{RunID: *runID, System: "ours"}
	res.Config = map[string]any{
		"concurrency": *c, "chunk_bytes": *chunk, "file_size_bytes": *fileSize,
		"timeout_ms": timeout.Milliseconds(), "max_retries": *retries,
	}

	srv, err := server.NewLogical(*fileSize)
	if err != nil {
		fail(res, *out, "server: "+err.Error())
	}
	srv.SetRate(0) // no application-level pacing: the tc bottleneck shapes the traffic
	srv.SetRequestLogEnabled(true)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	dir, err := os.MkdirTemp("", "jns-gate-")
	if err != nil {
		fail(res, *out, err.Error())
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	start := time.Now()
	dl, derr := downloader.New(downloader.Config{
		URL: ts.URL, OutputPath: outPath, TotalSize: *fileSize,
		Concurrency: *c, ChunkSize: *chunk, Timeout: *timeout, MaxRetries: *retries,
	})
	if derr != nil {
		fail(res, *out, derr.Error())
	}
	runErr := dl.Download(context.Background())
	res.CompletionMs = float64(time.Since(start).Microseconds()) / 1000.0

	snap := dl.Metrics()
	res.ClientRequests = snap.Aggregate.TotalRequests
	res.ClientSuccess = snap.Aggregate.SuccessfulRequests
	res.ClientFailed = snap.Aggregate.FailedRequests
	res.ClientBytes = snap.Aggregate.TotalBytes
	for _, r := range snap.Records {
		if r.Attempt > 1 {
			res.ClientRetries++
		}
	}
	if runErr != nil {
		res.Err = runErr.Error()
	}
	if st, serr := os.Stat(outPath); serr == nil {
		res.OutputSize = st.Size()
	}
	if res.OutputSize == *fileSize {
		ok, hash := verifyContent(outPath, *fileSize)
		res.ContentVerified = ok
		res.OutputSHA256 = hash
	}
	for _, e := range srv.RequestLog() {
		res.ServerRequests = append(res.ServerRequests, reqRecord{
			Index: e.Index, StartMs: e.Start.Sub(start).Milliseconds(),
			EndMs: e.End.Sub(start).Milliseconds(), Rate: e.Rate,
			RangeStart: e.Range.Start, RangeEnd: e.Range.End, HasRange: e.HasRange,
		})
	}
	st := srv.Stats()
	res.TotalRequests = st.TotalRequests
	res.SuccessfulReqs = st.Successful
	res.FailedReqs = st.Failed
	res.ServerBytes = st.BytesServed
	if res.CompletionMs > 0 {
		res.WallclockBps = float64(*fileSize) / (res.CompletionMs / 1000.0)
	}

	data, _ := json.MarshalIndent(res, "", "  ")
	if werr := os.WriteFile(*out, append(data, '\n'), 0o644); werr != nil {
		fmt.Fprintln(os.Stderr, "gate_run: write result:", werr)
		os.Exit(2)
	}
	fmt.Printf("run %s n=%d completion=%.3fs output=%d verified=%v retries=%d err=%q\n",
		*runID, *c, res.CompletionMs/1000, res.OutputSize, res.ContentVerified, res.ClientRetries, res.Err)
	if res.Err != "" || res.OutputSize != *fileSize {
		os.Exit(3)
	}
}

func fail(res result, out, msg string) {
	res.Err = msg
	data, _ := json.MarshalIndent(res, "", "  ")
	_ = os.WriteFile(out, append(data, '\n'), 0o644)
	fmt.Fprintln(os.Stderr, "gate_run:", msg)
	os.Exit(3)
}
