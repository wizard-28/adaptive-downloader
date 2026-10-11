// Command bench_ns runs ONE Track-1 D2 benchmark run: the exact frozen D2
// workload (512 MiB, per-response pacing 640 KiB/s -> 160 KiB/s at 2 s ->
// 2500 KiB/s at 4 s, tc/netem 40 ms + 0.4 %, HTTP/1.1, logical server) with a
// configurable mode:
//
//	fixed  fixed policy, N=16, S=4 MiB (the frozen best fixed configuration)
//	n-only adaptive concurrency only (ThroughputPolicy)
//	s-only adaptive chunk size only (ChunkSizePolicy)
//	joint  adaptive concurrency AND chunk size (JointPolicy)
//	aria2  frozen aria2c configuration (environment consistency checks only)
//
// It records the full per-decision policy trajectory, the per-attempt metrics,
// and the server request log, and verifies the output content.
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
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/policy"
)

const (
	d2InitialRate   = 640 << 10
	d2ThrottleRate  = 160 << 10
	d2RecoveryRate  = 2500 << 10
	phaseSeconds    = 2.0
	logicalSeed     = uint64(0x9E3779B97F4A7C15)
	frozenAria2Args = "--no-conf --file-allocation=none --allow-overwrite=true " +
		"--auto-file-renaming=false --enable-color=false --summary-interval=1 " +
		"--console-log-level=notice --min-split-size=1M --max-tries=3 " +
		"--retry-wait=1 --timeout=30 --connect-timeout=30 --http-accept-gzip=false " +
		"--check-certificate=false --disable-ipv6=true --conditional-get=false " +
		"--max-connection-per-server=16 --split=16"
)

type reqRecord struct {
	Index      int64 `json:"index"`
	StartMs    int64 `json:"start_ms"`
	EndMs      int64 `json:"end_ms"`
	Rate       int64 `json:"rate_bytes_per_second"`
	RangeStart int64 `json:"range_start"`
	RangeEnd   int64 `json:"range_end"`
	HasRange   bool  `json:"has_range"`
}

type attemptRecord struct {
	RequestID  string  `json:"request_id"`
	RangeStart int64   `json:"range_start"`
	RangeEnd   int64   `json:"range_end"`
	Bytes      int64   `json:"bytes"`
	DurationMs float64 `json:"duration_ms"`
	Throughput float64 `json:"throughput_bytes_per_second"`
	Success    bool    `json:"success"`
	StatusCode int     `json:"status_code"`
	Attempt    int     `json:"attempt"`
}

type decisionEvent struct {
	Seq                 int64   `json:"seq"`
	TimeMs              float64 `json:"time_ms"`
	Concurrency         int     `json:"concurrency"`
	ChunkSize           int64   `json:"chunk_size"`
	Throughput          float64 `json:"throughput"`
	AggregateThroughput float64 `json:"aggregate_throughput"`
	RequestDurationMs   float64 `json:"request_duration_ms"`
	BytesTransferred    int64   `json:"bytes_transferred"`
	SuccessfulRequests  int64   `json:"successful_requests"`
	FailedRequests      int64   `json:"failed_requests"`
	Retries             int64   `json:"retries"`
	TotalAttempts       int64   `json:"total_attempts"`
	ActionKind          string  `json:"action_kind"`
	ActionValue         int64   `json:"action_value"`
}

type updateEvent struct {
	Seq    int64   `json:"seq"`
	TimeMs float64 `json:"time_ms"`
	Reward float64 `json:"reward"`
	Kind   string  `json:"prev_action_kind"`
	Value  int64   `json:"prev_action_value"`
}

type result struct {
	RunID           string          `json:"run_id"`
	Mode            string          `json:"mode"`
	Config          map[string]any  `json:"config"`
	ScheduleEvents  []string        `json:"schedule_events"`
	NetemStatus     string          `json:"netem_status"`
	CompletionMs    float64         `json:"completion_ms"`
	WallclockBps    float64         `json:"wallclock_bytes_per_second"`
	OutputSize      int64           `json:"output_size_bytes"`
	OutputSHA256    string          `json:"output_sha256"`
	ContentVerified bool            `json:"content_verified"`
	ClientRequests  int64           `json:"client_requests"`
	ClientSuccess   int64           `json:"client_successful"`
	ClientFailed    int64           `json:"client_failed"`
	ClientRetries   int64           `json:"client_retries"`
	ClientBytes     int64           `json:"client_bytes"`
	Attempts        []attemptRecord `json:"attempts"`
	Decisions       []decisionEvent `json:"decisions"`
	Updates         []updateEvent   `json:"updates"`
	ServerRequests  []reqRecord     `json:"server_requests"`
	TotalRequests   int64           `json:"total_requests"`
	SuccessfulReqs  int64           `json:"successful_requests"`
	FailedReqs      int64           `json:"failed_requests"`
	ServerBytes     int64           `json:"server_bytes"`
	Aria2Exit       int             `json:"aria2_exit"`
	Aria2Tail       string          `json:"aria2_output_tail"`
	Err             string          `json:"err"`
}

// recordingPolicy wraps a policy and records every decision and update.
type recordingPolicy struct {
	inner     policy.Policy
	start     time.Time
	mu        sync.Mutex
	seq       int64
	decisions []decisionEvent
	updates   []updateEvent
}

func (r *recordingPolicy) SelectAction(ctx policy.Context) policy.Action {
	a := r.inner.SelectAction(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	r.decisions = append(r.decisions, decisionEvent{
		Seq: r.seq, TimeMs: float64(time.Since(r.start).Microseconds()) / 1000.0,
		Concurrency: ctx.Concurrency, ChunkSize: ctx.ChunkSize,
		Throughput: ctx.Throughput, AggregateThroughput: ctx.AggregateThroughput,
		RequestDurationMs:  float64(ctx.RequestDuration.Microseconds()) / 1000.0,
		BytesTransferred:   ctx.BytesTransferred,
		SuccessfulRequests: ctx.SuccessfulRequests, FailedRequests: ctx.FailedRequests,
		Retries: ctx.Retries, TotalAttempts: ctx.TotalAttempts,
		ActionKind: actionKindString(a.Kind), ActionValue: a.Value,
	})
	return a
}

func (r *recordingPolicy) Update(ctx policy.Context, a policy.Action, reward float64) {
	r.inner.Update(ctx, a, reward)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, updateEvent{
		Seq: int64(len(r.updates) + 1), TimeMs: float64(time.Since(r.start).Microseconds()) / 1000.0,
		Reward: reward, Kind: actionKindString(a.Kind), Value: a.Value,
	})
}

func actionKindString(k policy.ActionKind) string {
	switch k {
	case policy.ActionNone:
		return "none"
	case policy.ActionIncreaseConcurrency:
		return "increase-concurrency"
	case policy.ActionDecreaseConcurrency:
		return "decrease-concurrency"
	case policy.ActionIncreaseChunkSize:
		return "increase-chunk-size"
	case policy.ActionDecreaseChunkSize:
		return "decrease-chunk-size"
	case policy.ActionSetConcurrency:
		return "set-concurrency"
	case policy.ActionSetChunkSize:
		return "set-chunk-size"
	default:
		return "unknown"
	}
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

func fail(res result, out, msg string) {
	res.Err = msg
	data, _ := json.MarshalIndent(res, "", "  ")
	_ = os.WriteFile(out, append(data, '\n'), 0o644)
	fmt.Fprintln(os.Stderr, "bench_ns:", msg)
	os.Exit(3)
}

func main() {
	mode := flag.String("mode", "", "fixed|n-only|s-only|joint|aria2")
	out := flag.String("out", "", "result JSON path")
	runID := flag.String("run-id", "", "run identifier")
	fileSize := flag.Int64("file-size", 512<<20, "file size bytes")
	initialN := flag.Int("initial-n", 4, "initial concurrency")
	minN := flag.Int("min-n", 1, "minimum concurrency")
	maxN := flag.Int("max-n", 16, "maximum concurrency")
	initialChunk := flag.Int64("initial-chunk", 4<<20, "initial chunk size bytes")
	minChunk := flag.Int64("min-chunk", 256<<10, "minimum chunk size bytes")
	maxChunk := flag.Int64("max-chunk", 16<<20, "maximum chunk size bytes")
	chunkStep := flag.Int64("chunk-step", 1<<20, "chunk size step bytes")
	improve := flag.Float64("improve", 0.10, "improvement threshold")
	degrade := flag.Float64("degrade", 0.10, "degradation threshold")
	window := flag.Int("window", 4, "policy throughput window (attempts)")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	retries := flag.Int("retries", 3, "max retries")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	aria2Args := flag.String("aria2-args", frozenAria2Args, "aria2 flags (URL appended)")
	aria2Timeout := flag.Duration("aria2-timeout", 300*time.Second, "aria2 process timeout")
	flag.Parse()
	if *mode == "" || *out == "" || *runID == "" {
		fmt.Fprintln(os.Stderr, "bench_ns: -mode, -out, -run-id are required")
		os.Exit(2)
	}

	res := result{RunID: *runID, Mode: *mode, NetemStatus: "applied"}
	res.Config = map[string]any{
		"mode": *mode, "file_size_bytes": *fileSize,
		"initial_n": *initialN, "min_n": *minN, "max_n": *maxN,
		"initial_chunk_bytes": *initialChunk, "min_chunk_bytes": *minChunk,
		"max_chunk_bytes": *maxChunk, "chunk_step_bytes": *chunkStep,
		"improve_threshold": *improve, "degrade_threshold": *degrade,
		"policy_window": *window, "timeout_ms": timeout.Milliseconds(), "max_retries": *retries,
		"schedule": "d2 (640K -> 160K @2s -> 2500K @4s)",
		"netem":    "lo 40ms + 0.4%",
	}

	srv, err := server.NewLogical(*fileSize)
	if err != nil {
		fail(res, *out, "server: "+err.Error())
	}
	srv.SetRate(d2InitialRate)
	srv.SetRequestLogEnabled(true)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}
	iface, err := nc.InterfaceForHost("127.0.0.1")
	if err != nil {
		fail(res, *out, "interface: "+err.Error())
	}
	if err := nc.Apply(iface, network.Config{Latency: 40 * time.Millisecond, LossPercent: 0.4}); err != nil {
		res.NetemStatus = "unavailable"
		fail(res, *out, "netem apply: "+err.Error())
	}
	defer func() { _ = nc.Clear(iface) }()

	dir, err := os.MkdirTemp("", "jns-d2-")
	if err != nil {
		fail(res, *out, err.Error())
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	stop := make(chan struct{})
	events := make(chan string, 4)
	start := time.Now()
	go func() {
		for _, ph := range []struct {
			at   time.Duration
			rate int64
		}{{2 * time.Second, d2ThrottleRate}, {4 * time.Second, d2RecoveryRate}} {
			wait := ph.at - time.Since(start)
			if wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-timer.C:
				case <-stop:
					timer.Stop()
					return
				}
			}
			srv.SetRate(ph.rate)
			events <- fmt.Sprintf("rate=%d at %s", ph.rate, ph.at)
		}
	}()

	var runErr error
	switch *mode {
	case "fixed", "n-only", "s-only", "joint":
		cfg := downloader.Config{
			URL: ts.URL, OutputPath: outPath, TotalSize: *fileSize,
			Concurrency: *initialN, ChunkSize: *initialChunk,
			Timeout: *timeout, MaxRetries: *retries,
			PolicyWindowSize: *window,
		}
		switch *mode {
		case "fixed":
			cfg.Concurrency = 16
			cfg.ChunkSize = 4 << 20
		case "n-only":
			cfg.MinConcurrency = *minN
			cfg.MaxConcurrency = *maxN
			p, perr := policy.NewThroughputPolicy(policy.ThroughputPolicyConfig{
				MinConcurrency: *minN, MaxConcurrency: *maxN,
				ImprovementThreshold: *improve, DegradationThreshold: *degrade,
			})
			if perr != nil {
				fail(res, *out, perr.Error())
			}
			cfg.Policy = p
		case "s-only":
			cfg.MinChunkSize = *minChunk
			cfg.MaxChunkSize = *maxChunk
			cfg.InitialChunkSize = *initialChunk
			cfg.ChunkSizeStep = *chunkStep
			p, perr := policy.NewChunkSizePolicy(policy.ChunkSizePolicyConfig{
				MinChunkSize: *minChunk, MaxChunkSize: *maxChunk,
				ImprovementThreshold: *improve, DegradationThreshold: *degrade,
			})
			if perr != nil {
				fail(res, *out, perr.Error())
			}
			cfg.Policy = p
		case "joint":
			cfg.MinConcurrency = *minN
			cfg.MaxConcurrency = *maxN
			cfg.MinChunkSize = *minChunk
			cfg.MaxChunkSize = *maxChunk
			cfg.InitialChunkSize = *initialChunk
			cfg.ChunkSizeStep = *chunkStep
			p, perr := policy.NewJointPolicy(policy.JointPolicyConfig{
				MinConcurrency: *minN, MaxConcurrency: *maxN,
				MinChunkSize: *minChunk, MaxChunkSize: *maxChunk,
				ImprovementThreshold: *improve, DegradationThreshold: *degrade,
			})
			if perr != nil {
				fail(res, *out, perr.Error())
			}
			cfg.Policy = p
		}
		if cfg.Policy != nil {
			cfg.Policy = &recordingPolicy{inner: cfg.Policy, start: start}
		}
		dl, derr := downloader.New(cfg)
		if derr != nil {
			fail(res, *out, derr.Error())
		}
		runErr = dl.Download(context.Background())
		snap := dl.Metrics()
		res.ClientRequests = snap.Aggregate.TotalRequests
		res.ClientSuccess = snap.Aggregate.SuccessfulRequests
		res.ClientFailed = snap.Aggregate.FailedRequests
		res.ClientBytes = snap.Aggregate.TotalBytes
		for _, r := range snap.Records {
			if r.Attempt > 1 {
				res.ClientRetries++
			}
			res.Attempts = append(res.Attempts, attemptRecord{
				RequestID: r.RequestID, RangeStart: r.RangeStart, RangeEnd: r.RangeEnd,
				Bytes: r.Bytes, DurationMs: float64(r.Duration.Microseconds()) / 1000.0,
				Throughput: r.Throughput, Success: r.Success, StatusCode: r.StatusCode,
				Attempt: r.Attempt,
			})
		}
		if rp, ok := cfg.Policy.(*recordingPolicy); ok {
			rp.mu.Lock()
			res.Decisions = append(res.Decisions, rp.decisions...)
			res.Updates = append(res.Updates, rp.updates...)
			rp.mu.Unlock()
		}
	case "aria2":
		args := strings.Fields(*aria2Args)
		args = append(args, "-d", dir, "-o", "out.bin", "--interface=lo", ts.URL)
		ctx, cancel := context.WithTimeout(context.Background(), *aria2Timeout)
		defer cancel()
		outBytes, aerr := exec.CommandContext(ctx, "aria2c", args...).CombinedOutput()
		if aerr != nil {
			runErr = fmt.Errorf("aria2: %v", aerr)
		}
		if ee, ok := aerr.(*exec.ExitError); ok {
			res.Aria2Exit = ee.ExitCode()
		}
		tail := string(outBytes)
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		res.Aria2Tail = tail
	default:
		fail(res, *out, "unknown mode "+*mode)
	}

	res.CompletionMs = float64(time.Since(start).Microseconds()) / 1000.0
	close(stop)
	close(events)
	for e := range events {
		res.ScheduleEvents = append(res.ScheduleEvents, e)
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
		fmt.Fprintln(os.Stderr, "bench_ns: write result:", werr)
		os.Exit(2)
	}
	fmt.Printf("run %s mode=%s completion=%.3fs output=%d verified=%v retries=%d decisions=%d err=%q\n",
		*runID, *mode, res.CompletionMs/1000, res.OutputSize, res.ContentVerified,
		res.ClientRetries, len(res.Decisions), res.Err)
	if res.Err != "" || res.OutputSize != *fileSize {
		os.Exit(3)
	}
}
