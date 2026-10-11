// Command final-evaluation runs the FROZEN head-to-head: FINAL DQN CANDIDATE v2
// vs FINAL ARIA2 BASELINE CONFIG v1 on a locked, hashed 8-condition test matrix
// with 30 matched pairs per condition (480 downloads). It verifies the frozen
// hashes, locks the matrix before any run, randomizes execution order with a
// deterministic seed, uses a fresh controlled server per run, and writes raw
// per-run results. It performs NO tuning and NO post-hoc changes.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/rl/dqn"
)

const fileSize = int64(4194304)

// Condition is one unstable-network profile (wall-clock transitions only).
type Condition struct {
	ID              string  `json:"id"`
	InitialRate     int64   `json:"initial_rate"`
	SubsequentRate  int64   `json:"subsequent_rate"`
	TransitionAfter float64 `json:"transition_after_s"`
	LatencyMs       int     `json:"latency_ms"`
	LossPercent     float64 `json:"loss_percent"`
}

func conditions() []Condition {
	return []Condition{
		{ID: "U1", InitialRate: 768 * 1024, SubsequentRate: 3 * 1024 * 1024, TransitionAfter: 0.80},
		{ID: "U2", InitialRate: 1536 * 1024, SubsequentRate: 6 * 1024 * 1024, TransitionAfter: 1.30},
		{ID: "U3", InitialRate: 4 * 1024 * 1024, SubsequentRate: 4 * 1024 * 1024, LatencyMs: 15, LossPercent: 0.5},
		{ID: "U4", InitialRate: 2 * 1024 * 1024, SubsequentRate: 2 * 1024 * 1024, LatencyMs: 35, LossPercent: 1.5},
		{ID: "U5", InitialRate: 3 * 1024 * 1024, SubsequentRate: 3 * 1024 * 1024, LatencyMs: 60, LossPercent: 0.75},
		{ID: "U6", InitialRate: 1280 * 1024, SubsequentRate: 5 * 1024 * 1024, TransitionAfter: 1.10, LatencyMs: 25},
		{ID: "U7", InitialRate: 2 * 1024 * 1024, SubsequentRate: 6 * 1024 * 1024, TransitionAfter: 1.40, LossPercent: 1.0},
		{ID: "U8", InitialRate: 1 * 1024 * 1024, SubsequentRate: 4 * 1024 * 1024, TransitionAfter: 0.90, LatencyMs: 40, LossPercent: 1.25},
	}
}

const (
	dqnSHA256   = "59cbc5e6e18f428358b6233ba0e350c35668f2ebad3ba4d23e5e4c85de9b3a0e"
	aria2SHA256 = "6b8cf33eb896ecf7c13de71434250ad9611cdd501043075212fb6bd0b1ffcf72"
	reps        = 30
)

type PairSchedule struct {
	PairID     string `json:"pair_id"`
	Condition  string `json:"condition"`
	OrderFirst string `json:"first"`
	DQNRunID   string `json:"dqn_run_id"`
	Aria2RunID string `json:"aria2_run_id"`
}

type RunResult struct {
	RunID          string  `json:"run_id"`
	PairID         string  `json:"pair_id"`
	Condition      string  `json:"condition"`
	System         string  `json:"system"` // "dqn" | "aria2"
	DurationMs     float64 `json:"duration_ms"`
	WallclockBps   float64 `json:"wallclock_bps"`
	OK             bool    `json:"ok"`
	OutputSize     int64   `json:"output_size"`
	OutputMatches  bool    `json:"output_matches"`
	NoDuplicate    bool    `json:"no_duplicate_ranges"`
	NoMissing      bool    `json:"no_missing_ranges"`
	TotalRequests  int64   `json:"total_requests"`
	MaxInFlight    int64   `json:"max_in_flight"`
	NonRangeReqs   int     `json:"non_range_requests"`
	RangeCount     int     `json:"range_count"`
	RangeMinBytes  int64   `json:"range_min_bytes"`
	RangeMaxBytes  int64   `json:"range_max_bytes"`
	Retries        int     `json:"retries"`
	PreTransition  int     `json:"requests_before_transition"`
	PostTransition int     `json:"requests_at_or_after_transition"`
	FirstPostS     float64 `json:"first_post_transition_elapsed_s"`
	// DQN behavioral fields.
	FinalConcurrency int    `json:"final_concurrency,omitempty"`
	MaxConcurrency   int    `json:"max_concurrency,omitempty"`
	EffConcChanges   int    `json:"effective_concurrency_changes,omitempty"`
	FinalChunk       int64  `json:"final_chunk_size,omitempty"`
	EffChunkChanges  int    `json:"effective_chunk_changes,omitempty"`
	NoOpActions      int    `json:"no_op_actions,omitempty"`
	Decisions        int    `json:"decisions,omitempty"`
	ConcChanges      int    `json:"concurrency_changes,omitempty"`
	ChunkChanges     int    `json:"chunk_size_changes,omitempty"`
	Err              string `json:"err"`
}

type Matrix struct {
	Label          string      `json:"label"`
	Conditions     []Condition `json:"conditions"`
	Repetitions    int         `json:"repetitions"`
	Pairs          int         `json:"pairs"`
	TotalDownloads int         `json:"total_downloads"`
	FileSize       int64       `json:"file_size"`
	DQNSHA256      string      `json:"dqn_sha256"`
	Aria2SHA256    string      `json:"aria2_sha256"`
	Seed           int64       `json:"seed"`
	Note           string      `json:"note"`
}

func main() {
	modelPath := flag.String("model", "testdata/diagnostics/dqn_validation_v2/final_dqn_candidate_v2.json", "frozen DQN model")
	baselinePath := flag.String("baseline", "testdata/diagnostics/aria2_calibration/selected_baseline.json", "frozen aria2 baseline metadata")
	outDir := flag.String("out", "testdata/final_evaluation", "output directory")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	seed := flag.Int64("seed", 20260925, "deterministic schedule seed")
	flag.Parse()

	// ---- freeze checks ----
	if got := sha256File(*modelPath); got != dqnSHA256 {
		fatal(fmt.Errorf("DQN hash mismatch: %s", got))
	}
	baseline, err := os.ReadFile(*baselinePath)
	if err != nil {
		fatal(err)
	}
	if got := sha256Canonical(baseline); got != aria2SHA256 {
		fatal(fmt.Errorf("aria2 baseline metadata hash mismatch: %s", got))
	}
	var bl struct {
		Command        string `json:"command"`
		Aria2Version   string `json:"aria2_version"`
		SelectedConfig struct {
			MaxConns int `json:"max_connection_per_server"`
			Split    int `json:"split"`
		} `json:"selected_config"`
	}
	if err := json.Unmarshal(baseline, &bl); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}

	// ---- lock the matrix BEFORE any run ----
	m := Matrix{
		Label:      "FINAL head-to-head test matrix (DQN Candidate v2 vs aria2 baseline v1)",
		Conditions: conditions(), Repetitions: reps, Pairs: len(conditions()) * reps,
		TotalDownloads: len(conditions()) * reps * 2, FileSize: fileSize,
		DQNSHA256: dqnSHA256, Aria2SHA256: aria2SHA256, Seed: *seed,
		Note: "wall-clock rate schedules only; paired design; conditions do not duplicate calibration V1-V5, v6, or DQN validation",
	}
	matrixBytes := mustJSON(m)
	if err := os.WriteFile(filepath.Join(*outDir, "final_test_matrix.json"), append(matrixBytes, '\n'), 0o644); err != nil {
		fatal(err)
	}
	msum := sha256bytes(matrixBytes)
	if err := os.WriteFile(filepath.Join(*outDir, "final_test_matrix.sha256"), []byte(msum+"\n"), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("matrix locked: %d conditions, %d pairs, %d downloads, sha256=%s\n", len(m.Conditions), m.Pairs, m.TotalDownloads, msum)

	// ---- build the randomized-pair schedule ----
	rng := rand.New(rand.NewSource(*seed))
	var sched []PairSchedule
	for _, c := range conditions() {
		for r := 0; r < reps; r++ {
			pid := fmt.Sprintf("%s-p%02d", c.ID, r)
			first := "dqn"
			if rng.Intn(2) == 0 {
				first = "aria2"
			}
			sched = append(sched, PairSchedule{PairID: pid, Condition: c.ID, OrderFirst: first,
				DQNRunID: pid + "-dqn", Aria2RunID: pid + "-aria2"})
		}
	}
	writeJSON(filepath.Join(*outDir, "execution_schedule.json"), map[string]any{"seed": *seed, "pairs": sched})

	// ---- run ----
	content := contentPattern(fileSize)
	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}
	model, err := dqn.LoadModel(*modelPath)
	if err != nil {
		fatal(err)
	}
	condByID := map[string]Condition{}
	for _, c := range conditions() {
		condByID[c.ID] = c
	}

	var results []RunResult
	traces := []map[string]any{}
	total := 0
	for _, p := range sched {
		c := condByID[p.Condition]
		order := []string{p.OrderFirst}
		if p.OrderFirst == "dqn" {
			order = append(order, "aria2")
		} else {
			order = append(order, "dqn")
		}
		for _, sys := range order {
			total++
			var rr RunResult
			if sys == "dqn" {
				rr = runDQN(model, c, p, content, nc)
			} else {
				rr = runAria2(bl.Command, c, p, content, nc)
			}
			results = append(results, rr)
			fmt.Printf("[%3d/%d] %s %-5s %-5s dur=%8.1fms ok=%v\n", total, m.TotalDownloads, p.PairID, p.Condition, sys, rr.DurationMs, rr.OK)
			traces = append(traces, map[string]any{
				"run_id": rr.RunID, "condition": rr.Condition, "system": rr.System,
				"total_requests": rr.TotalRequests, "max_in_flight": rr.MaxInFlight,
				"range_count": rr.RangeCount, "range_min": rr.RangeMinBytes, "range_max": rr.RangeMaxBytes,
				"pre_transition": rr.PreTransition, "post_transition": rr.PostTransition,
				"first_post_elapsed_s": rr.FirstPostS, "retries": rr.Retries,
			})
		}
	}
	writeJSON(filepath.Join(*outDir, "raw_results.json"), map[string]any{
		"matrix_sha256": msum, "aria2_version": bl.Aria2Version, "results": results,
	})
	writeJSON(filepath.Join(*outDir, "request_traces.json"), map[string]any{"runs": traces})
	fmt.Printf("wrote raw results for %d runs\n", len(results))
}

func runDQN(model *dqn.Model, c Condition, p PairSchedule, content []byte, nc *network.Controller) RunResult {
	rr := RunResult{RunID: p.DQNRunID, PairID: p.PairID, Condition: c.ID, System: "dqn"}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRate(c.InitialRate)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	if !applyNet(nc, c, &rr) {
		return rr
	}
	defer clearNet(nc, c)
	dir, err := os.MkdirTemp("", "final-")
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")
	pol, err := dqn.NewLivePolicy(model, fileSize)
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	dl, err := downloader.New(downloader.Config{
		URL: ts.URL, OutputPath: outPath, TotalSize: fileSize,
		Concurrency: 1, ChunkSize: 32768,
		MinConcurrency: 1, MaxConcurrency: 4,
		MinChunkSize: 32768, MaxChunkSize: 131072, InitialChunkSize: 32768, ChunkSizeStep: 32768,
		Timeout: 30 * time.Second, MaxRetries: 3, PolicyWindowSize: 4, Policy: pol,
	})
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	start := time.Now()
	if c.TransitionAfter > 0 {
		if err := srv.SetRateScheduleAt(start, server.RateSchedule{InitialRate: c.InitialRate, TransitionAfter: dur(c.TransitionAfter), SubsequentRate: c.SubsequentRate}); err != nil {
			rr.Err = err.Error()
			return rr
		}
	}
	runErr := dl.Download(context.Background())
	rr.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	if runErr != nil {
		rr.Err = runErr.Error()
	}
	if rr.DurationMs > 0 {
		rr.WallclockBps = float64(fileSize) / (rr.DurationMs / 1000.0)
	}
	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputMatches = string(got) == string(contentCopy)
	}
	stats := srv.Stats()
	snap := dl.Metrics()
	rr.TotalRequests = snap.Aggregate.TotalAttempts
	rr.NoDuplicate, rr.NoMissing = verifyRanges(snap.Records, fileSize)
	rr.FinalConcurrency = dl.Concurrency()
	rr.MaxConcurrency = dl.Concurrency()
	rr.FinalChunk = dl.ChunkSize()
	for _, rec := range snap.Records {
		if rec.Attempt > 1 {
			rr.Retries++
		}
	}
	log := pol.Log()
	rr.Decisions = len(log)
	for _, d := range log {
		if d.ConcurrencyBefore > rr.MaxConcurrency {
			rr.MaxConcurrency = d.ConcurrencyBefore
		}
		if d.Effective {
			if d.Action == "increase-concurrency" || d.Action == "decrease-concurrency" {
				rr.EffConcChanges++
			}
			if d.Action == "increase-chunk-size" || d.Action == "decrease-chunk-size" {
				rr.EffChunkChanges++
			}
		}
		if d.NoOp {
			rr.NoOpActions++
		}
	}
	rr.OK = rr.Err == "" && rr.OutputMatches && rr.NoDuplicate && rr.NoMissing
	rr.OutputSize = fileSize
	_ = stats
	return rr
}

func runAria2(command string, c Condition, p PairSchedule, content []byte, nc *network.Controller) RunResult {
	rr := RunResult{RunID: p.Aria2RunID, PairID: p.PairID, Condition: c.ID, System: "aria2"}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRate(c.InitialRate)
	srv.SetRequestLogEnabled(true)
	tracker := &inflightTracker{inner: srv}
	ts := httptest.NewServer(tracker)
	defer ts.Close()
	if !applyNet(nc, c, &rr) {
		return rr
	}
	defer clearNet(nc, c)
	dir, err := os.MkdirTemp("", "final-aria2-")
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")
	args := aria2ArgsFromCommand(command)
	args = append(args, "-d", dir, "-o", "out.bin", "--interface=lo", ts.URL)
	start := time.Now()
	if c.TransitionAfter > 0 {
		if err := srv.SetRateScheduleAt(start, server.RateSchedule{InitialRate: c.InitialRate, TransitionAfter: dur(c.TransitionAfter), SubsequentRate: c.SubsequentRate}); err != nil {
			rr.Err = err.Error()
			return rr
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	outBytes, runErr := exec.CommandContext(ctx, "aria2c", args...).CombinedOutput()
	rr.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	if runErr != nil {
		rr.Err = runErr.Error()
	}
	if rr.DurationMs > 0 {
		rr.WallclockBps = float64(fileSize) / (rr.DurationMs / 1000.0)
	}
	if info, err := os.Stat(outPath); err == nil {
		rr.OutputSize = info.Size()
	}
	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputMatches = string(got) == string(contentCopy)
	}
	stats := srv.Stats()
	rr.TotalRequests = stats.TotalRequests
	rr.MaxInFlight = tracker.max()
	rr.NonRangeReqs = int(stats.TotalRequests) - len(stats.RequestedRanges)
	rr.RangeCount = len(stats.RequestedRanges)
	for i, rg := range stats.RequestedRanges {
		l := rg.Length()
		if i == 0 {
			rr.RangeMinBytes, rr.RangeMaxBytes = l, l
		}
		if l < rr.RangeMinBytes {
			rr.RangeMinBytes = l
		}
		if l > rr.RangeMaxBytes {
			rr.RangeMaxBytes = l
		}
	}
	rr.Retries = countRetries(string(outBytes))
	rr.OK = rr.Err == "" && rr.OutputMatches && rr.OutputSize == fileSize
	if c.TransitionAfter > 0 {
		log := srv.RequestLog()
		if len(log) > 0 {
			t0 := log[0].Start
			for _, e := range log {
				el := e.Start.Sub(t0)
				if el >= dur(c.TransitionAfter) {
					rr.PostTransition++
					if rr.FirstPostS == 0 {
						rr.FirstPostS = el.Seconds()
					}
				} else {
					rr.PreTransition++
				}
			}
		}
	}
	return rr
}

func applyNet(nc *network.Controller, c Condition, rr *RunResult) bool {
	if c.LatencyMs == 0 && c.LossPercent == 0 {
		return true
	}
	if err := nc.Apply("lo", network.Config{Latency: time.Duration(c.LatencyMs) * time.Millisecond, LossPercent: c.LossPercent}); err != nil {
		rr.Err = "netem apply: " + err.Error()
		return false
	}
	return true
}

// clearNet removes any qdisc residue for this run. It is called on every path
// (including failures) so no shaping leaks into another run.
func clearNet(nc *network.Controller, c Condition) {
	if c.LatencyMs == 0 && c.LossPercent == 0 {
		return
	}
	_ = nc.Clear("lo")
}

type inflightTracker struct {
	inner       http.Handler
	inFlight    int64
	maxInFlight int64
}

func (h *inflightTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := atomic.AddInt64(&h.inFlight, 1)
	for {
		m := atomic.LoadInt64(&h.maxInFlight)
		if n <= m || atomic.CompareAndSwapInt64(&h.maxInFlight, m, n) {
			break
		}
	}
	defer atomic.AddInt64(&h.inFlight, -1)
	h.inner.ServeHTTP(w, r)
}

func (h *inflightTracker) max() int64 { return atomic.LoadInt64(&h.maxInFlight) }

func verifyRanges(records []metrics.Record, size int64) (noDuplicates, noMissing bool) {
	type seg struct{ s, e int64 }
	var segs []seg
	for _, r := range records {
		if r.Success {
			segs = append(segs, seg{r.RangeStart, r.RangeEnd})
		}
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].s < segs[j].s })
	noDuplicates, noMissing = true, true
	var prev int64 = -1
	for _, s := range segs {
		if s.s <= prev {
			noDuplicates = false
		}
		if s.s != prev+1 {
			noMissing = false
		}
		if s.e > prev {
			prev = s.e
		}
	}
	if prev != size-1 {
		noMissing = false
	}
	return
}

func dur(seconds float64) time.Duration { return time.Duration(seconds * float64(time.Second)) }

// aria2ArgsFromCommand turns the frozen baseline command string into argv:
// it drops the leading "aria2c" and the trailing "<url>" placeholder and keeps
// every flag exactly as frozen.
func aria2ArgsFromCommand(command string) []string {
	fields := strings.Fields(command)
	var args []string
	for _, f := range fields {
		if f == "aria2c" || f == "<url>" {
			continue
		}
		args = append(args, f)
	}
	return args
}

func countRetries(log string) int {
	n := 0
	for _, line := range strings.Split(log, "\n") {
		low := strings.ToLower(line)
		if strings.Contains(low, "retry") || strings.Contains(low, "restarting") {
			n++
		}
	}
	return n
}

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "final-evaluation:", err)
	os.Exit(1)
}

func writeJSON(path string, v any) {
	b := mustJSON(v)
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		fatal(err)
	}
}

func mustJSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatal(err)
	}
	return b
}

func sha256File(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	return sha256bytes(b)
}

// sha256Canonical hashes JSON content without a trailing newline, matching the
// frozen aria2 metadata hash convention.
func sha256Canonical(b []byte) string {
	return sha256bytes([]byte(strings.TrimRight(string(b), "\n")))
}

func sha256bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

var _ = strconv.Itoa
