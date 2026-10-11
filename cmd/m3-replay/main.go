// Command m3-replay runs the M3 throughput-envelope replay: a real NYU-METS
// raw trace (1 Hz, Mbits/sec) is applied as a time-varying tc/netem aggregate
// rate limit on "lo" while the internal downloader fetches a fixed workload at
// fixed concurrency with 1 MiB chunks. The trace value is measured end-to-end
// throughput, NOT path capacity; this is an empirical envelope replay.
// Experiment-only; no RL training; frozen artifacts untouched.
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
	"sort"
	"strings"
	"sync"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
)

type sample struct {
	Second  int     `json:"second"`
	RateBps int64   `json:"rate_bps"`
	Target  float64 `json:"target_bytes_per_s"`
}

type result struct {
	Label               string   `json:"label"`
	Trace               string   `json:"trace"`
	Concurrency         int      `json:"concurrency"`
	WorkloadBytes       int64    `json:"workload_bytes"`
	ChunkBytes          int64    `json:"chunk_bytes"`
	CompletionMs        float64  `json:"completion_ms"`
	OutputSize          int64    `json:"output_size"`
	SHAOK               bool     `json:"sha_ok"`
	Err                 string   `json:"err"`
	TraceSeconds        int      `json:"trace_seconds_available"`
	TraceConsumedS      float64  `json:"trace_seconds_consumed"`
	TraceConsumedF      float64  `json:"trace_fraction_consumed"`
	Requests            int64    `json:"requests"`
	Retries             int64    `json:"retries"`
	Failures            int64    `json:"failures"`
	ReplayThroughputBps float64  `json:"replay_throughput_bytes_per_s"`
	TraceMeanBps        float64  `json:"trace_consumed_mean_bytes_per_s"`
	AppliedSamples      []sample `json:"applied_samples"`
}

func main() {
	tracePath := flag.String("trace", "", "NYU-METS raw CSV trace")
	c := flag.Int("c", 8, "fixed concurrency")
	sizeMib := flag.Int64("size-mib", 48, "workload MiB")
	maxS := flag.Int("max-seconds", 180, "max trace seconds available for the run")
	label := flag.String("label", "m3", "run label")
	out := flag.String("out", "testdata/diagnostics/m3_mobile_replay/runs", "output dir")
	useSudo := flag.Bool("sudo", true, "use sudo -n for tc")
	flag.Parse()
	if *tracePath == "" {
		fatal(fmt.Errorf("-trace is required"))
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	trace := parseNYU(*tracePath)
	if len(trace) > *maxS && *maxS > 0 {
		trace = trace[:*maxS]
	}
	if len(trace) == 0 {
		fatal(fmt.Errorf("empty trace %s", *tracePath))
	}
	size := *sizeMib << 20
	content := contentPattern(size)
	expected := sha256Bytes(content)

	srv := server.New(content)
	srv.SetRate(0) // stable server: the only shaping is the replay envelope
	ts := httptest.NewServer(srv)
	defer ts.Close()
	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}
	applyRate := func(rateBps int64) error {
		if rateBps < 8000 {
			rateBps = 8000 // floor: zero samples replay as an outage-like crawl
		}
		return nc.Apply("lo", network.Config{BandwidthBps: rateBps})
	}
	var mu sync.Mutex
	applied := []sample{}
	record := func(i int) {
		r := int64(8000)
		if i < len(trace) && trace[i] > 0 {
			r = int64(trace[i] * 8)
		}
		mu.Lock()
		applied = append(applied, sample{Second: i, RateBps: r, Target: trace[i]})
		mu.Unlock()
	}
	if err := applyRate(sampleRate(trace, 0)); err != nil {
		fatal(fmt.Errorf("tc apply: %w", err))
	}
	record(0)
	start := time.Now()
	done := make(chan struct{})
	go func() {
		tk := time.NewTicker(time.Second)
		defer tk.Stop()
		i := 1
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				if err := applyRate(sampleRate(trace, i)); err != nil {
					mu.Lock()
					applied = append(applied, sample{Second: i, RateBps: -1})
					mu.Unlock()
				}
				record(i)
				i++
			}
		}
	}()
	dir, err := os.MkdirTemp("", "m3-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")
	dl, err := downloader.New(downloader.Config{URL: ts.URL, OutputPath: outPath,
		TotalSize: size, ChunkSize: 1 << 20, Concurrency: *c, MinConcurrency: *c,
		MaxConcurrency: *c, Timeout: 600 * time.Second, MaxRetries: 3})
	if err != nil {
		fatal(err)
	}
	runErr := dl.Download(context.Background())
	dur := time.Since(start)
	close(done)
	_ = nc.Clear("lo")

	rr := result{Label: *label, Trace: *tracePath, Concurrency: *c, WorkloadBytes: size,
		ChunkBytes: 1 << 20, CompletionMs: float64(dur.Microseconds()) / 1000.0,
		TraceSeconds: len(trace), TraceConsumedS: dur.Seconds(),
		TraceConsumedF: dur.Seconds() / float64(len(trace))}
	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputSize = int64(len(got))
		rr.SHAOK = sha256Bytes(got) == expected
	}
	if runErr != nil {
		rr.Err = runErr.Error()
	}
	snap := dl.Metrics()
	rr.Requests = int64(len(snap.Records))
	for _, rec := range snap.Records {
		if !rec.Success {
			rr.Failures++
		}
		if rec.Attempt > 1 {
			rr.Retries++
		}
	}
	if dur.Seconds() > 0 {
		rr.ReplayThroughputBps = float64(size) / dur.Seconds()
	}
	consumed := int(dur.Seconds()) + 1
	if consumed > len(trace) {
		consumed = len(trace)
	}
	sum := 0.0
	for i := 0; i < consumed; i++ {
		sum += trace[i]
	}
	if consumed > 0 {
		rr.TraceMeanBps = sum / float64(consumed)
	}
	rr.AppliedSamples = applied
	writeJSON(filepath.Join(*out, fmt.Sprintf("%s_C%d.json", *label, *c)), rr)
	fmt.Printf("[%s C%d] %.1fms replay=%.0f B/s traceMean=%.0f B/s consumed=%.1f%% sha=%v\n",
		*label, *c, rr.CompletionMs, rr.ReplayThroughputBps, rr.TraceMeanBps, 100*rr.TraceConsumedF, rr.SHAOK)
}

func parseNYU(path string) []float64 {
	var out []float64
	for _, line := range strings.Split(readFile(path), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		tok := strings.Split(line, ",")[0]
		var v float64
		if _, err := fmt.Sscanf(tok, "%f", &v); err != nil {
			continue
		}
		if v < 0 {
			v = 0
		}
		out = append(out, v*1e6/8.0)
	}
	return out
}

func sampleRate(trace []float64, i int) int64 {
	if i >= len(trace) || trace[i] <= 0 {
		return 8000
	}
	return int64(trace[i] * 8)
}

func readFile(p string) string { b, _ := os.ReadFile(p); return string(b) }

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
}

func sha256Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "m3-replay:", err)
	os.Exit(1)
}

var _ = sort.Ints
