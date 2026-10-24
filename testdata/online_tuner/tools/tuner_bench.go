// Command tuner_bench is a copy of testdata/split_ablation/tools/split_bench.go
// with one additive policy, -policy online (policy.OnlineTunerPolicy, flags
// -online-*). split_bench itself copies the frozen ns_bench.go and adds
// -split (downloader.Config.SplitStragglers) and
// -no-request-timeout (downloader.Config.NoRequestTimeout, so a request is
// bounded only by the run). The split count is recorded as config "splits".
// Everything else, including the server model and the frozen aria2 flags, is
// byte-identical to ns_bench.
//
// Original description: runs ONE Track-2-B live run: the controlled server with a
// soft N-dependent concurrency cap and a phase-locked per-response pacing
// schedule, driving the internal downloader (fixed config, probe classifier, or
// drop-detecting causal controller) or the frozen aria2 configuration.
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
	"math"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/policy"
)

const logicalSeed = uint64(0x9E3779B97F4A7C15)

const frozenAria2Args = "--no-conf --file-allocation=none --allow-overwrite=true " +
	"--auto-file-renaming=false --enable-color=false --summary-interval=1 " +
	"--console-log-level=notice --min-split-size=1M --max-tries=3 " +
	"--retry-wait=1 --timeout=30 --connect-timeout=30 --http-accept-gzip=false " +
	"--check-certificate=false --disable-ipv6=true --conditional-get=false " +
	"--max-connection-per-server=16 --split=16"

type reqRecord struct {
	Index      int64 `json:"index"`
	StartMs    int64 `json:"start_ms"`
	EndMs      int64 `json:"end_ms"`
	Rate       int64 `json:"rate_bytes_per_second"`
	BaseRate   int64 `json:"base_rate_bytes_per_second"`
	CapRate    int64 `json:"cap_rate_bytes_per_second"`
	ActiveK    int64 `json:"active_k"`
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
	Seq         int64   `json:"seq"`
	TimeMs      float64 `json:"time_ms"`
	Concurrency int     `json:"concurrency"`
	ChunkSize   int64   `json:"chunk_size"`
	Throughput  float64 `json:"throughput"`
	ActionKind  string  `json:"action_kind"`
	ActionValue int64   `json:"action_value"`
}

type result struct {
	RunID           string          `json:"run_id"`
	System          string          `json:"system"`
	Policy          string          `json:"policy"`
	Config          map[string]any  `json:"config"`
	ScheduleEvents  []string        `json:"schedule_events"`
	CapEvents       []string        `json:"cap_events"`
	CompletionMs    float64         `json:"completion_ms"`
	OutputSize      int64           `json:"output_size_bytes"`
	OutputSHA256    string          `json:"output_sha256"`
	ContentVerified bool            `json:"content_verified"`
	ClientRequests  int64           `json:"client_requests"`
	ClientSuccess   int64           `json:"client_successful"`
	ClientFailed    int64           `json:"client_failed"`
	ClientRetries   int64           `json:"client_retries"`
	ClientBytes     int64           `json:"client_bytes"`
	FinalN          int             `json:"final_n"`
	FinalS          int64           `json:"final_s"`
	Attempts        []attemptRecord `json:"attempts"`
	Decisions       []decisionEvent `json:"decisions"`
	ServerRequests  []reqRecord     `json:"server_requests"`
	TotalRequests   int64           `json:"total_requests"`
	SuccessfulReqs  int64           `json:"successful_requests"`
	FailedReqs      int64           `json:"failed_requests"`
	Aria2Exit       int             `json:"aria2_exit"`
	Aria2Tail       string          `json:"aria2_output_tail"`
	Err             string          `json:"err"`
}

// ------------------------- policies -------------------------

// targetPolicy implements two predeclared causal controllers:
//
//	classifier: after k samples, select the healthy or capped target from the
//	            absolute per-request throughput (one-shot; P1 selector).
//	drop:       track the running maximum of the windowed median; when the
//	            median falls below drop x maximum, select the capped target
//	            (one-shot; P2 causal controller).
//	band:       same running-maximum drop detector; after a fixed post-drop
//	            window (-band-window seconds, >= 1 post-drop sample), map the
//	            median post-drop throughput to the nearest predeclared level
//	            and select the matching target (one-shot; W1-W6 workload
//	            selector).  If no drop occurs by -deadline seconds, fall back
//	            to the healthy target.  The window is time-based because
//	            post-drop request durations reach ~10 s under the strongest
//	            frozen cap, making sample-count windows impractical.
//
// Both move N and S toward the selected target using the existing bounded
// controllers (one action per completed attempt).
type targetPolicy struct {
	mode          string
	healthyN      int
	healthyS      int64
	cappedN       int
	cappedS       int64
	threshold     float64
	drop          float64
	k             int
	classifyAfter int

	levels     []float64
	bandN      []int
	bandS      []int64
	bandWindow float64
	deadline   float64
	collecting bool
	dropAt     int
	dropTimeS  float64

	start time.Time

	mu       sync.Mutex
	samples  []float64
	baseline float64
	targetN  int
	targetS  int64
	decided  bool
	events   []decisionEvent
	seq      int64
}

func bandDecide(samples []float64, levels []float64) int {
	m := median(samples)
	best, bestd := 0, math.Inf(1)
	for i, l := range levels {
		d := math.Abs(m - l)
		if d < bestd {
			best, bestd = i, d
		}
	}
	return best
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	return c[len(c)/2]
}

func (p *targetPolicy) SelectAction(ctx policy.Context) policy.Action {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	p.samples = append(p.samples, ctx.Throughput)
	if !p.decided {
		switch p.mode {
		case "classifier":
			if len(p.samples) >= p.classifyAfter {
				mn := p.samples[0]
				for _, v := range p.samples {
					if v < mn {
						mn = v
					}
				}
				if mn < p.threshold {
					p.targetN, p.targetS = p.cappedN, p.cappedS
				} else {
					p.targetN, p.targetS = p.healthyN, p.healthyS
				}
				p.decided = true
			}
		case "drop":
			if len(p.samples) < p.k {
				break
			}
			med := median(p.samples[len(p.samples)-p.k:])
			if med > p.baseline {
				p.baseline = med
			}
			if p.baseline > 0 && med < p.drop*p.baseline {
				p.targetN, p.targetS = p.cappedN, p.cappedS
				p.decided = true
			}
		case "band":
			if len(p.samples) < p.k {
				break
			}
			med := median(p.samples[len(p.samples)-p.k:])
			if med > p.baseline {
				p.baseline = med
			}
			if !p.collecting && p.baseline > 0 && med < p.drop*p.baseline {
				p.collecting = true
				p.dropAt = len(p.samples)
				p.dropTimeS = time.Since(p.start).Seconds()
			}
			if p.collecting && len(p.samples) > p.dropAt &&
				time.Since(p.start).Seconds()-p.dropTimeS >= p.bandWindow {
				i := bandDecide(p.samples[p.dropAt:], p.levels)
				p.targetN, p.targetS = p.bandN[i], p.bandS[i]
				p.decided = true
			} else if p.deadline > 0 && time.Since(p.start).Seconds() > p.deadline {
				p.targetN, p.targetS = p.healthyN, p.healthyS
				p.decided = true
			}
		}
	}
	var action policy.Action
	if p.decided {
		switch {
		case ctx.Concurrency > p.targetN:
			d := int64(ctx.Concurrency - p.targetN)
			if d > 16 {
				d = 16
			}
			action = policy.Action{Kind: policy.ActionDecreaseConcurrency, Value: d}
		case ctx.Concurrency < p.targetN:
			d := int64(p.targetN - ctx.Concurrency)
			if d > 16 {
				d = 16
			}
			action = policy.Action{Kind: policy.ActionIncreaseConcurrency, Value: d}
		case ctx.ChunkSize > p.targetS:
			action = policy.DecreaseChunkSize()
		case ctx.ChunkSize < p.targetS:
			action = policy.IncreaseChunkSize()
		default:
			action = policy.NoChange()
		}
	} else {
		action = policy.NoChange()
	}
	p.events = append(p.events, decisionEvent{
		Seq: p.seq, TimeMs: float64(time.Since(p.start).Microseconds()) / 1000.0,
		Concurrency: ctx.Concurrency, ChunkSize: ctx.ChunkSize,
		Throughput: ctx.Throughput,
		ActionKind: actionKindString(action.Kind), ActionValue: action.Value,
	})
	return action
}

func (p *targetPolicy) Update(policy.Context, policy.Action, float64) {}

// recordingPolicy wraps a project policy and records each decision.
type recordingPolicy struct {
	inner  policy.Policy
	start  time.Time
	mu     sync.Mutex
	seq    int64
	events []decisionEvent
}

func (p *recordingPolicy) SelectAction(ctx policy.Context) policy.Action {
	a := p.inner.SelectAction(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	p.events = append(p.events, decisionEvent{
		Seq: p.seq, TimeMs: float64(time.Since(p.start).Microseconds()) / 1000.0,
		Concurrency: ctx.Concurrency, ChunkSize: ctx.ChunkSize, Throughput: ctx.Throughput,
		ActionKind: actionKindString(a.Kind), ActionValue: a.Value,
	})
	return a
}

func (p *recordingPolicy) Update(ctx policy.Context, a policy.Action, r float64) {
	p.inner.Update(ctx, a, r)
}

func (p *recordingPolicy) decisions() []decisionEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]decisionEvent(nil), p.events...)
}

func (p *targetPolicy) decisions() []decisionEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]decisionEvent(nil), p.events...)
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
	default:
		return "other"
	}
}

func parseBandLevels(s string) ([]float64, error) {
	var out []float64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", part, err)
		}
		if v <= 0 {
			return nil, fmt.Errorf("level must be positive: %v", v)
		}
		out = append(out, v)
	}
	return out, nil
}

func parseBandTargets(s string) ([]int, []int64, error) {
	var ns []int
	var ss []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pair := strings.SplitN(part, ":", 2)
		if len(pair) != 2 {
			return nil, nil, fmt.Errorf("bad target %q (want n:bytes)", part)
		}
		n, err := strconv.Atoi(strings.TrimSpace(pair[0]))
		if err != nil {
			return nil, nil, fmt.Errorf("bad N in %q: %w", part, err)
		}
		b, err := strconv.ParseInt(strings.TrimSpace(pair[1]), 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("bad bytes in %q: %w", part, err)
		}
		ns = append(ns, n)
		ss = append(ss, b)
	}
	return ns, ss, nil
}

// ------------------------- content verification -------------------------

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
			for i := 0; i < n; i++ {
				if buf[i] != exp[i] {
					return false, hex.EncodeToString(h.Sum(nil))
				}
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
	fmt.Fprintln(os.Stderr, "ns_bench:", msg)
	os.Exit(3)
}

func main() {
	system := flag.String("system", "ours", "ours|aria2|curl|wget")
	mode := flag.String("policy", "none", "none|classifier|drop|band|online")
	n := flag.Int("n", 16, "initial concurrency")
	chunk := flag.Int64("chunk", 1<<20, "initial chunk size bytes")
	healthyN := flag.Int("healthy-n", 16, "classifier healthy target N")
	healthyS := flag.Int64("healthy-s", 1<<20, "classifier healthy target S")
	cappedN := flag.Int("capped-n", 4, "classifier capped target N")
	cappedS := flag.Int64("capped-s", 4<<20, "classifier capped target S")
	threshold := flag.Float64("threshold", 450*1024, "classifier threshold bytes/s")
	classifyAfter := flag.Int("classify-after", 20, "classifier samples before deciding")
	drop := flag.Float64("drop", 0.5, "drop detector ratio")
	bandLevels := flag.String("band-levels", "", "comma-separated predicted post-drop levels bytes/s (band policy)")
	bandTargets := flag.String("band-targets", "", "comma-separated n:bytes targets matching -band-levels (band policy)")
	bandWindow := flag.Float64("band-window", 25, "band policy post-drop classification window seconds (min 1 sample)")
	deadline := flag.Float64("deadline", 60, "band policy fallback deadline seconds (no drop => healthy target)")
	window := flag.Int("window", 3, "samples per decision window")
	minN := flag.Int("min-n", 1, "min concurrency")
	maxN := flag.Int("max-n", 16, "max concurrency")
	onlineInterval := flag.Duration("online-interval", 2*time.Second, "online tuner measurement window")
	onlineStep := flag.Int("online-step", 4, "online tuner initial step")
	onlinePenalty := flag.Float64("online-penalty", 1.0, "online tuner per-connection penalty K in U=T/K^N")
	onlineTol := flag.Float64("online-tol", 0.05, "online tuner relative tolerance")
	minChunk := flag.Int64("min-chunk", 256<<10, "min chunk bytes")
	maxChunk := flag.Int64("max-chunk", 16<<20, "max chunk bytes")
	chunkStep := flag.Int64("chunk-step", 1<<20, "chunk step bytes")
	schedule := flag.String("schedule", "t1", "t1|t2|t4|d2")
	capC := flag.Int64("cap-c", 0, "concurrency cap C bytes/s (0 disables)")
	capGamma := flag.Float64("cap-gamma", 0.05, "concurrency cap gamma")
	capAt := flag.Float64("cap-at", 0, "cap activation time seconds (-1 = never, 0 = from start)")
	fileSize := flag.Int64("file-size", 512<<20, "file size bytes")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	split := flag.Bool("split", false, "enable straggler splitting (downloader.Config.SplitStragglers)")
	noReqTimeout := flag.Bool("no-request-timeout", false, "disable the whole-request deadline (overrides -timeout)")
	retries := flag.Int("retries", 3, "max retries")
	out := flag.String("out", "", "result JSON path")
	runID := flag.String("run-id", "", "run identifier")
	aria2Args := flag.String("aria2-args", frozenAria2Args, "aria2 flags")
	aria2Timeout := flag.Duration("aria2-timeout", 600*time.Second, "aria2 process timeout")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	netemLatency := flag.Int("netem-latency-ms", 0, "netem latency ms (0 = no shaping)")
	netemLoss := flag.Float64("netem-loss", 0.4, "netem loss percent")
	flag.Parse()
	if *out == "" || *runID == "" {
		fmt.Fprintln(os.Stderr, "ns_bench: -out and -run-id are required")
		os.Exit(2)
	}

	res := result{RunID: *runID, System: *system, Policy: *mode}
	res.Config = map[string]any{
		"system": *system, "policy": *mode, "file_size_bytes": *fileSize,
		"initial_n": *n, "initial_chunk_bytes": *chunk,
		"healthy_n": *healthyN, "healthy_s": *healthyS,
		"capped_n": *cappedN, "capped_s": *cappedS,
		"threshold": *threshold, "drop": *drop, "window": *window,
		"min_n": *minN, "max_n": *maxN, "min_chunk_bytes": *minChunk,
		"max_chunk_bytes": *maxChunk, "chunk_step_bytes": *chunkStep,
		"schedule": *schedule, "cap_c": *capC, "cap_gamma": *capGamma, "cap_at": *capAt,
		"timeout_ms": timeout.Milliseconds(), "max_retries": *retries,
	}

	srv, err := server.NewLogical(*fileSize)
	if err != nil {
		fail(res, *out, "server: "+err.Error())
	}
	var netemClear func()
	if *netemLatency > 0 {
		nc := network.NewController()
		if *useSudo {
			nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
		}
		iface, ierr := nc.InterfaceForHost("127.0.0.1")
		if ierr != nil {
			fail(res, *out, "interface: "+ierr.Error())
		}
		if aerr := nc.Apply(iface, network.Config{
			Latency:     time.Duration(*netemLatency) * time.Millisecond,
			LossPercent: *netemLoss,
		}); aerr != nil {
			fail(res, *out, "netem apply: "+aerr.Error())
		}
		netemClear = func() { _ = nc.Clear(iface) }
		defer netemClear()
	}
	res.Config["split"] = *split
	res.Config["no_request_timeout"] = *noReqTimeout
	res.Config["netem_latency_ms"] = *netemLatency
	res.Config["netem_loss_percent"] = *netemLoss
	initialRate := int64(640 << 10)
	srv.SetRate(initialRate)
	srv.SetRequestLogEnabled(true)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	start := time.Now()
	stop := make(chan struct{})
	events := make(chan string, 8)
	// per-response pacing schedule
	go func() {
		switch *schedule {
		case "t1", "t2", "t4":
			var d time.Duration
			switch *schedule {
			case "t1":
				d = 1 * time.Second
			case "t2":
				d = 2 * time.Second
			case "t4":
				d = 4 * time.Second
			}
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-timer.C:
				srv.SetRate(2500 << 10)
				events <- fmt.Sprintf("rate=2560000 at %s", d)
			case <-stop:
				return
			}
		case "d2":
			for _, ph := range []struct {
				at   time.Duration
				rate int64
			}{{2 * time.Second, 160 << 10}, {4 * time.Second, 2500 << 10}} {
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
		}
	}()
	// concurrency cap schedule
	if *capC > 0 && *capAt >= 0 {
		if *capAt == 0 {
			if err := srv.SetConcurrencyCap(*capC, *capGamma); err != nil {
				fail(res, *out, "cap: "+err.Error())
			}
			events <- "cap on at 0s"
		} else {
			go func() {
				timer := time.NewTimer(time.Duration(*capAt * float64(time.Second)))
				defer timer.Stop()
				select {
				case <-timer.C:
					_ = srv.SetConcurrencyCap(*capC, *capGamma)
					events <- fmt.Sprintf("cap on at %.0fs", *capAt)
				case <-stop:
					return
				}
			}()
		}
	}

	dir, err := os.MkdirTemp("", "ns-bench-")
	if err != nil {
		fail(res, *out, err.Error())
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	var runErr error
	var tp *targetPolicy
	var rp *recordingPolicy
	switch *system {
	case "ours":
		cfg := downloader.Config{
			URL: ts.URL, OutputPath: outPath, TotalSize: *fileSize,
			Concurrency: *n, ChunkSize: *chunk,
			Timeout: *timeout, MaxRetries: *retries,
			MinConcurrency: *minN, MaxConcurrency: *maxN,
			MinChunkSize: *minChunk, MaxChunkSize: *maxChunk,
			InitialChunkSize: *chunk, ChunkSizeStep: *chunkStep,
			PolicyWindowSize: *window,
			SplitStragglers:  *split,
		}
		if *noReqTimeout {
			cfg.Timeout, cfg.NoRequestTimeout = 0, true
		}
		if *mode == "online" {
			op, oerr := policy.NewOnlineTunerPolicy(policy.OnlineTunerConfig{
				MinConcurrency: *minN, MaxConcurrency: *maxN, Interval: *onlineInterval,
				InitialStep: *onlineStep, Penalty: *onlinePenalty, Tolerance: *onlineTol,
			})
			if oerr != nil {
				fail(res, *out, "online tuner: "+oerr.Error())
			}
			rp = &recordingPolicy{inner: op, start: start}
			cfg.Policy = rp
			res.Config["online_interval_ms"] = onlineInterval.Milliseconds()
			res.Config["online_step"] = *onlineStep
			res.Config["online_penalty"] = *onlinePenalty
			res.Config["online_tol"] = *onlineTol
		} else if *mode != "none" {
			tp = &targetPolicy{
				mode: *mode, healthyN: *healthyN, healthyS: *healthyS,
				cappedN: *cappedN, cappedS: *cappedS,
				threshold: *threshold, drop: *drop, k: *window,
				classifyAfter: *classifyAfter, start: start,
			}
			if *mode == "band" {
				lv, err := parseBandLevels(*bandLevels)
				if err != nil {
					fail(res, *out, "band levels: "+err.Error())
				}
				bN, bS, err := parseBandTargets(*bandTargets)
				if err != nil {
					fail(res, *out, "band targets: "+err.Error())
				}
				if len(lv) == 0 || len(lv) != len(bN) {
					fail(res, *out, "band levels/targets must be non-empty and equal length")
				}
				tp.levels, tp.bandN, tp.bandS = lv, bN, bS
				tp.bandWindow, tp.deadline = *bandWindow, *deadline
			}
			cfg.Policy = tp
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
		res.FinalN = dl.Concurrency()
		res.FinalS = dl.ChunkSize()
		res.Config["splits"] = dl.Splits()
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
		if tp != nil {
			res.Decisions = tp.decisions()
		}
		if rp != nil {
			res.Decisions = rp.decisions()
		}
	case "curl", "wget":
		var cmd *exec.Cmd
		if *system == "curl" {
			cmd = exec.CommandContext(context.Background(), "curl", "-s", "-o", outPath, ts.URL)
		} else {
			cmd = exec.CommandContext(context.Background(), "wget", "-q", "-O", outPath, ts.URL)
		}
		outBytes, cerr := cmd.CombinedOutput()
		if cerr != nil {
			runErr = fmt.Errorf("%s: %v (%s)", *system, cerr, strings.TrimSpace(string(outBytes)))
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
		fail(res, *out, "unknown system "+*system)
	}

	res.CompletionMs = float64(time.Since(start).Microseconds()) / 1000.0
	close(stop)
	close(events)
	for e := range events {
		if strings.HasPrefix(e, "cap") {
			res.CapEvents = append(res.CapEvents, e)
		} else {
			res.ScheduleEvents = append(res.ScheduleEvents, e)
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
			BaseRate: e.BaseRate, CapRate: e.CapRate, ActiveK: e.ActiveK,
			RangeStart: e.Range.Start, RangeEnd: e.Range.End, HasRange: e.HasRange,
		})
	}
	st := srv.Stats()
	res.TotalRequests = st.TotalRequests
	res.SuccessfulReqs = st.Successful
	res.FailedReqs = st.Failed

	data, _ := json.MarshalIndent(res, "", "  ")
	if werr := os.WriteFile(*out, append(data, '\n'), 0o644); werr != nil {
		fmt.Fprintln(os.Stderr, "ns_bench: write result:", werr)
		os.Exit(2)
	}
	fmt.Printf("run %s system=%s policy=%s completion=%.3fs output=%d verified=%v retries=%d finalN=%d finalS=%d err=%q\n",
		*runID, *system, *mode, res.CompletionMs/1000, res.OutputSize, res.ContentVerified,
		res.ClientRetries, res.FinalN, res.FinalS, res.Err)
	if res.Err != "" || res.OutputSize != *fileSize {
		os.Exit(3)
	}
}
