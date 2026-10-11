// Command final-evaluation-v6 runs the FINAL pre-registered head-to-head:
// frozen DQN Candidate v6 vs frozen aria2 baseline v2 (candidate F) on the
// pre-registered v6 live trajectories A-E (PROJECT_CONTEXT §22.33), with the
// established matched-pair methodology (30 pairs per trajectory, deterministic
// per-pair order, fresh server/output/netem per run, wall-clock phase changes).
// Statistics reuse python/analyze_final_evaluation.py. No tuning, no post-hoc
// scenario changes, no modification of any frozen artifact.
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
	"strings"
	"sync"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl/dqn"
)

const (
	fixedChunkSize = int64(1 << 20)
	minConcurrency = 1
	maxConcurrency = 16
	maxRetries     = 3
	runTimeout     = 30 * time.Second
	policyWindow   = 4
	phaseSeconds   = 2.0
	reps           = 30
	scheduleSeed   = 20260925

	v6SHA        = "daa7176793093bea102965700774609fd70d4bbb60523d2417302e2a7f0d9887"
	aria2SHA     = "b72498176a48f889cd2d1dbc9d9ef6b9afc668f2cf0d902e44d51dee0dedca02"
	aria2Version = "aria2 version 1.37.0"
)

// fileSize is a harness-only diagnostic parameter. The default (16 MiB)
// preserves the frozen final-evaluation matrix exactly; larger values support
// the long-duration diagnostic without touching any frozen artifact.
var fileSize = int64(16 << 20)

type PhaseSpec struct {
	Rate        int64   `json:"rate"`
	Link        int64   `json:"link"`
	LatencyMs   int     `json:"latency_ms"`
	LossPercent float64 `json:"loss_percent"`
}

type Condition struct {
	ID    string      `json:"id"`
	Phase []PhaseSpec `json:"phases"`
}

func conditions() []Condition {
	return []Condition{
		{ID: "A2", Phase: []PhaseSpec{
			{Rate: 8 << 20, Link: 5 << 20, LatencyMs: 50, LossPercent: 0.4},
			{Rate: 8 << 20, Link: 900 << 10, LatencyMs: 50, LossPercent: 0.4},
			{Rate: 8 << 20, Link: 4 << 20, LatencyMs: 50, LossPercent: 0.4},
		}},
		{ID: "B2", Phase: []PhaseSpec{
			{Rate: 8 << 20, Link: 4500 << 10, LatencyMs: 20, LossPercent: 0.4},
			{Rate: 8 << 20, Link: 4500 << 10, LatencyMs: 110, LossPercent: 0.4},
			{Rate: 8 << 20, Link: 4500 << 10, LatencyMs: 20, LossPercent: 0.4},
		}},
		{ID: "C2", Phase: []PhaseSpec{
			{Rate: 8 << 20, Link: 4500 << 10, LatencyMs: 40, LossPercent: 0.4},
			{Rate: 8 << 20, Link: 4500 << 10, LatencyMs: 40, LossPercent: 3.2},
			{Rate: 8 << 20, Link: 4500 << 10, LatencyMs: 40, LossPercent: 0.4},
		}},
		{ID: "D2", Phase: []PhaseSpec{
			{Rate: 640 << 10, LatencyMs: 40, LossPercent: 0.4},
			{Rate: 160 << 10, LatencyMs: 40, LossPercent: 0.4},
			{Rate: 2500 << 10, LatencyMs: 40, LossPercent: 0.4},
		}},
		{ID: "E2", Phase: []PhaseSpec{
			{Rate: 8 << 20, Link: 4500 << 10, LatencyMs: 20, LossPercent: 0.4},
			{Rate: 8 << 20, Link: 1800 << 10, LatencyMs: 110, LossPercent: 3.8},
			{Rate: 8 << 20, Link: 3500 << 10, LatencyMs: 60, LossPercent: 1.7},
		}},
	}
}

type PairSchedule struct {
	PairID     string `json:"pair_id"`
	Condition  string `json:"condition"`
	OrderFirst string `json:"first"`
	V6RunID    string `json:"v6_run_id"`
	Aria2RunID string `json:"aria2_run_id"`
}

func buildSchedule(conds []Condition, pairs int, seed int64) []PairSchedule {
	next := lcg(seed)
	var out []PairSchedule
	for _, c := range conds {
		for r := 0; r < pairs; r++ {
			pid := fmt.Sprintf("%s-p%02d", c.ID, r)
			first := "v6"
			if next(2) == 0 {
				first = "aria2"
			}
			out = append(out, PairSchedule{PairID: pid, Condition: c.ID, OrderFirst: first,
				V6RunID: pid + "-v6", Aria2RunID: pid + "-aria2"})
		}
	}
	return out
}

type ActionEvent struct {
	DecisionIndex        int     `json:"decision_index"`
	TimeSinceStartMs     float64 `json:"time_since_start_ms"`
	RequestedActionIndex int     `json:"requested_action_index"`
	ConcurrencyBefore    int     `json:"concurrency_before"`
	ConcurrencyAfter     int     `json:"concurrency_after"`
	Changed              bool    `json:"changed"`
	RemainingBytes       int64   `json:"remaining_bytes"`
}

type PhaseEvent struct {
	Index int     `json:"index"`
	AtMs  float64 `json:"at_ms"`
	Err   string  `json:"err,omitempty"`
}

type RunResult struct {
	RunID          string                `json:"run_id"`
	PairID         string                `json:"pair_id"`
	Condition      string                `json:"condition"`
	System         string                `json:"system"` // "dqn" (v6) | "aria2"
	Policy         string                `json:"policy"` // "v6" | "aria2-f"
	Rep            int                   `json:"rep"`
	OrderIndex     int                   `json:"order_index"`
	PairFirst      string                `json:"pair_first_system"`
	DurationMs     float64               `json:"duration_ms"`
	WallclockBps   float64               `json:"wallclock_bps"`
	Valid          bool                  `json:"valid"`
	OutputSize     int64                 `json:"output_size"`
	OutputSHA256   string                `json:"output_sha256"`
	ExpectedSHA256 string                `json:"expected_sha256"`
	NetemStatus    string                `json:"netem_status"`
	TotalRequests  int64                 `json:"total_requests"`
	SuccessfulReqs int64                 `json:"successful_requests"`
	FailedRequests int64                 `json:"failed_requests"`
	Retries        int64                 `json:"retries"`
	SuccessBytes   int64                 `json:"success_bytes"`
	ServerBytes    int64                 `json:"server_bytes_served"`
	MaxInFlight    int64                 `json:"max_in_flight"`
	MeanInFlight   float64               `json:"mean_in_flight"`
	NonRangeReqs   int                   `json:"non_range_requests"`
	RangeCount     int                   `json:"range_count"`
	RangeMinBytes  int64                 `json:"range_min_bytes"`
	RangeMaxBytes  int64                 `json:"range_max_bytes"`
	InitialConc    int                   `json:"initial_concurrency,omitempty"`
	FinalConc      int                   `json:"final_concurrency,omitempty"`
	MaxConc        int                   `json:"max_concurrency,omitempty"`
	TwConc         float64               `json:"time_weighted_concurrency,omitempty"`
	ConcChanges    int                   `json:"effective_concurrency_changes,omitempty"`
	ConstantRun    bool                  `json:"constant_run"`
	RampToMax      bool                  `json:"ramp_to_max"`
	Actions        []ActionEvent         `json:"actions,omitempty"`
	PhaseMeanConc  []float64             `json:"phase_mean_concurrency"`
	PhaseChanges   []int                 `json:"phase_change_counts"`
	ReactionMs     []float64             `json:"transition_reaction_ms"`
	PhaseEvents    []PhaseEvent          `json:"phase_events"`
	PreTransition  int                   `json:"requests_before_transition"`
	PostTransition int                   `json:"requests_at_or_after_transition"`
	ModelSHA       string                `json:"model_sha256,omitempty"`
	Aria2ConfigSHA string                `json:"aria2_config_sha256,omitempty"`
	Err            string                `json:"err"`
	ServerRequests []server.RequestEvent `json:"server_requests,omitempty"`
}

type Matrix struct {
	Label          string      `json:"label"`
	Conditions     []Condition `json:"conditions"`
	Repetitions    int         `json:"repetitions"`
	Pairs          int         `json:"pairs"`
	TotalDownloads int         `json:"total_downloads"`
	FileSize       int64       `json:"file_size"`
	ChunkSize      int64       `json:"chunk_size"`
	V6SHA256       string      `json:"v6_sha256"`
	Aria2SHA256    string      `json:"aria2_sha256"`
	Aria2Version   string      `json:"aria2_version"`
	Seed           int64       `json:"seed"`
	Note           string      `json:"note"`
}

type timedPolicy struct {
	inner policy.Policy
	start time.Time
	times []time.Time
}

func (t *timedPolicy) SelectAction(ctx policy.Context) policy.Action {
	t.times = append(t.times, time.Now())
	return t.inner.SelectAction(ctx)
}

func (t *timedPolicy) Update(ctx policy.Context, a policy.Action, reward float64) {
	t.inner.Update(ctx, a, reward)
}

func main() {
	modelPath := flag.String("model", filepath.Join("testdata", "diagnostics", "dqn_validation_v6", "final_dqn_candidate_v6.json"), "frozen v6 model")
	baselinePath := flag.String("baseline", filepath.Join("testdata", "diagnostics", "aria2_calibration_v2", "selected_baseline_v2.json"), "frozen aria2 v2 baseline metadata")
	outDir := flag.String("out", filepath.Join("testdata", "final_evaluation_v6"), "output directory")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	pairs := flag.Int("pairs", reps, "matched pairs per trajectory")
	condFlag := flag.String("conditions", "", "comma-separated trajectory subset")
	fileSizeFlag := flag.Int64("file-size", fileSize, "file size in bytes (diagnostic only; default preserves the frozen 16 MiB matrix)")
	flag.Parse()
	fileSize = *fileSizeFlag

	if got, err := sha256File(*modelPath); err != nil || got != v6SHA {
		fatal(fmt.Errorf("v6 hash mismatch: %s err=%v", got, err))
	}
	baseline, err := os.ReadFile(*baselinePath)
	if err != nil {
		fatal(err)
	}
	if got := sha256Bytes(baseline); got != aria2SHA {
		fatal(fmt.Errorf("aria2 metadata hash mismatch: %s", got))
	}
	var bl struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(baseline, &bl); err != nil {
		fatal(err)
	}
	for _, req := range []string{"--max-connection-per-server=16", "--split=16", "--min-split-size=1048576"} {
		if !strings.Contains(bl.Command, req) {
			fatal(fmt.Errorf("baseline command missing %q", req))
		}
	}
	version, err := aria2VersionString()
	if err != nil || version != aria2Version {
		fatal(fmt.Errorf("aria2 version %q, want %q (%v)", version, aria2Version, err))
	}
	aria2Args := aria2ArgsFromCommand(bl.Command)
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}
	conds := filterConditions(conditions(), *condFlag)
	if len(conds) == 0 || *pairs < 1 {
		fatal(fmt.Errorf("empty matrix"))
	}

	m := Matrix{
		Label:      "FINAL pre-registered head-to-head (DQN Candidate v6 vs aria2-F) on v6 trajectories A-E",
		Conditions: conds, Repetitions: *pairs, Pairs: len(conds) * *pairs,
		TotalDownloads: len(conds) * *pairs * 2, FileSize: fileSize, ChunkSize: fixedChunkSize,
		V6SHA256: v6SHA, Aria2SHA256: aria2SHA, Aria2Version: aria2Version, Seed: scheduleSeed,
		Note: "pre-registered §22.33 trajectories; wall-clock phases; matched pairs; 30 pairs/trajectory per the established final-evaluation methodology",
	}
	mBytes := mustJSON(m)
	if err := os.WriteFile(filepath.Join(*outDir, "final_test_matrix_v6.json"), append(mBytes, '\n'), 0o644); err != nil {
		fatal(err)
	}
	msum := sha256Bytes(mBytes)
	_ = os.WriteFile(filepath.Join(*outDir, "final_test_matrix_v6.sha256"), []byte(msum+"\n"), 0o644)

	sched := buildSchedule(conds, *pairs, scheduleSeed)
	writeJSON(filepath.Join(*outDir, "execution_schedule.json"), map[string]any{"seed": scheduleSeed, "pairs": sched})

	content := contentPattern(fileSize)
	expectedSHA := sha256Bytes(content)
	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}
	model, err := dqn.LoadModel(*modelPath)
	if err != nil {
		fatal(err)
	}
	condByID := map[string]Condition{}
	for _, c := range conds {
		condByID[c.ID] = c
	}

	var results []RunResult
	idx := 0
	for _, p := range sched {
		c := condByID[p.Condition]
		order := []string{p.OrderFirst}
		if p.OrderFirst == "v6" {
			order = append(order, "aria2")
		} else {
			order = append(order, "v6")
		}
		for _, sys := range order {
			idx++
			var rr RunResult
			if sys == "v6" {
				rr = runV6(model, c, p, content, expectedSHA, nc, idx)
			} else {
				rr = runAria2(aria2Args, c, p, content, expectedSHA, nc, idx)
			}
			results = append(results, rr)
			fmt.Printf("[%3d/%d] %s %-3s %-6s dur=%8.1fms valid=%-5v C%d->C%d\n",
				idx, m.TotalDownloads, p.PairID, c.ID, sys, rr.DurationMs, rr.Valid, rr.InitialConc, rr.FinalConc)
		}
	}

	// Analyzer input: valid pairs only, established schema.
	byPair := map[string]map[string]RunResult{}
	for _, r := range results {
		if byPair[r.PairID] == nil {
			byPair[r.PairID] = map[string]RunResult{}
		}
		byPair[r.PairID][r.System] = r
	}
	var lean []map[string]any
	validPairs := 0
	for _, s := range sched {
		d, okD := byPair[s.PairID]["dqn"]
		a, okA := byPair[s.PairID]["aria2"]
		if !okD || !okA || !d.Valid || !a.Valid {
			continue
		}
		validPairs++
		for _, r := range []RunResult{d, a} {
			lean = append(lean, map[string]any{"run_id": r.RunID, "pair_id": r.PairID,
				"condition": r.Condition, "system": r.System, "policy": r.Policy,
				"duration_ms": r.DurationMs, "wallclock_bps": r.WallclockBps, "ok": r.Valid})
		}
	}
	writeJSON(filepath.Join(*outDir, "raw_results.json"), map[string]any{
		"matrix_sha256": msum, "v6_sha256": v6SHA, "aria2_sha256": aria2SHA, "aria2_version": aria2Version,
		"systems": map[string]string{"dqn": "Candidate v6 (state v3, 1 MiB chunk, C 1-16)", "aria2": "aria2-F"},
		"results": lean})
	writeJSON(filepath.Join(*outDir, "raw_results_all.json"), map[string]any{
		"matrix_sha256": msum, "expected_content_sha256": expectedSHA,
		"aria2_command": bl.Command, "aria2_args": aria2Args, "results": results})

	valid, invalid := 0, 0
	var exclusions []map[string]any
	for _, r := range results {
		if r.Valid {
			valid++
		} else {
			invalid++
			exclusions = append(exclusions, map[string]any{"run_id": r.RunID, "system": r.System, "err": r.Err})
		}
	}
	writeJSON(filepath.Join(*outDir, "validity_report.json"), map[string]any{
		"expected_runs": m.TotalDownloads, "recorded_runs": len(results),
		"valid_runs": valid, "invalid_runs": invalid,
		"expected_pairs": m.Pairs, "valid_pairs": validPairs, "exclusions": exclusions})
	writeJSON(filepath.Join(*outDir, "manifest_v6.json"), map[string]any{
		"label": m.Label, "matrix_sha256": msum, "v6_sha256": v6SHA,
		"aria2_baseline_sha256": aria2SHA, "aria2_version": aria2Version, "aria2_command": bl.Command,
		"file_size": fileSize, "chunk_size": fixedChunkSize, "v6_concurrency_bounds": []int{1, 16},
		"conditions": conds, "pairs_per_condition": *pairs, "pairs": m.Pairs,
		"total_downloads": m.TotalDownloads, "schedule_seed": scheduleSeed,
		"source_methodology": "matched pairs; log-ratio effect; paired permutation test; Holm; via python/analyze_final_evaluation.py",
	})
	fmt.Printf("wrote %d runs (%d valid pairs) to %s\n", len(results), validPairs, *outDir)
}

// ---------------------------------------------------------------------------

func runV6(model *dqn.Model, c Condition, p PairSchedule, content []byte, expectedSHA string, nc *network.Controller, idx int) RunResult {
	rr := RunResult{RunID: p.V6RunID, PairID: p.PairID, Condition: c.ID, System: "dqn", Policy: "v6",
		Rep: repOf(p), OrderIndex: idx, PairFirst: p.OrderFirst, ExpectedSHA256: expectedSHA, ModelSHA: v6SHA}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRequestLogEnabled(true)
	srv.SetRate(c.Phase[0].Rate)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	if err := applyNetPhase(nc, c.Phase[0]); err != nil {
		rr.NetemStatus = "unavailable"
		rr.Err = err.Error()
		return rr
	}
	rr.NetemStatus = netemStatus(c.Phase[0])
	defer func() { _ = nc.Clear("lo") }()

	dir, err := os.MkdirTemp("", "final-v6-")
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")
	lp, err := dqn.NewLiveConcurrencyPolicyV6(model, fileSize)
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	tp := &timedPolicy{inner: lp}
	dl, err := downloader.New(downloader.Config{
		URL: ts.URL, OutputPath: outPath, TotalSize: fileSize,
		Concurrency: minConcurrency, ChunkSize: fixedChunkSize,
		MinConcurrency: minConcurrency, MaxConcurrency: maxConcurrency,
		Timeout: runTimeout, MaxRetries: maxRetries, PolicyWindowSize: policyWindow, Policy: tp,
	})
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	rr.InitialConc = dl.Concurrency()
	start := time.Now()
	tp.start = start
	_, wait := schedulePhases(srv, nc, c, start)
	runErr := dl.Download(context.Background())
	rr.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	rr.PhaseEvents = wait()
	if rr.DurationMs > 0 {
		rr.WallclockBps = float64(fileSize) / (rr.DurationMs / 1000.0)
	}
	if runErr != nil {
		rr.Err = runErr.Error()
	}
	rr.FinalConc = dl.Concurrency()
	snap := dl.Metrics()
	rr.TotalRequests = int64(len(snap.Records))
	for _, rec := range snap.Records {
		if rec.Success {
			rr.SuccessfulReqs++
			rr.SuccessBytes += rec.Bytes
		} else {
			rr.FailedRequests++
		}
		if rec.Attempt > 1 {
			rr.Retries++
		}
	}
	rr.ServerRequests = srv.RequestLog()
	st := srv.Stats()
	rr.ServerBytes = st.BytesServed
	rr.MaxInFlight, rr.MeanInFlight = inFlightStats(rr.ServerRequests)
	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputSize = int64(len(got))
		rr.OutputSHA256 = sha256Bytes(got)
	}
	log := lp.Log()
	rr.Actions = deriveActions(rr.FinalConc, log, tp.times, start)
	rr.ConcChanges = countChanges(rr.Actions)
	rr.MaxConc = maxConc(rr.InitialConc, rr.Actions)
	rr.TwConc = weightedMean(concurrencySegments(rr.InitialConc, rr.Actions, rr.DurationMs))
	rr.PhaseMeanConc, rr.PhaseChanges = phaseStats(rr.InitialConc, rr.Actions, rr.DurationMs, len(c.Phase))
	rr.ReactionMs = reactionLatencies(rr.Actions, len(c.Phase))
	rr.ConstantRun, rr.RampToMax, _ = classify(rr.Actions)
	rr.PreTransition, rr.PostTransition = prePost(rr.ServerRequests)
	for _, ev := range rr.PhaseEvents {
		if ev.Err != "" {
			rr.NetemStatus = "unavailable"
			rr.Err = "netem phase: " + ev.Err
		}
	}
	rr.Valid = rr.Err == "" && rr.OutputSize == fileSize && rr.OutputSHA256 == expectedSHA &&
		rr.NetemStatus != "unavailable" && rr.SuccessBytes == fileSize
	return rr
}

func runAria2(aria2Args []string, c Condition, p PairSchedule, content []byte, expectedSHA string, nc *network.Controller, idx int) RunResult {
	rr := RunResult{RunID: p.Aria2RunID, PairID: p.PairID, Condition: c.ID, System: "aria2", Policy: "aria2-f",
		Rep: repOf(p), OrderIndex: idx, PairFirst: p.OrderFirst, ExpectedSHA256: expectedSHA, Aria2ConfigSHA: aria2SHA}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRequestLogEnabled(true)
	srv.SetRate(c.Phase[0].Rate)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	if err := applyNetPhase(nc, c.Phase[0]); err != nil {
		rr.NetemStatus = "unavailable"
		rr.Err = err.Error()
		return rr
	}
	rr.NetemStatus = netemStatus(c.Phase[0])
	defer func() { _ = nc.Clear("lo") }()
	dir, err := os.MkdirTemp("", "final-v6-aria2-")
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")
	args := append(append([]string(nil), aria2Args...), "-d", dir, "-o", "out.bin", "--interface=lo", ts.URL)
	start := time.Now()
	_, wait := schedulePhases(srv, nc, c, start)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	outBytes, runErr := exec.CommandContext(ctx, "aria2c", args...).CombinedOutput()
	rr.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	rr.PhaseEvents = wait()
	if runErr != nil {
		rr.Err = runErr.Error()
	}
	if rr.DurationMs > 0 {
		rr.WallclockBps = float64(fileSize) / (rr.DurationMs / 1000.0)
	}
	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputSize = int64(len(got))
		rr.OutputSHA256 = sha256Bytes(got)
	}
	st := srv.Stats()
	rr.TotalRequests = st.TotalRequests
	rr.SuccessfulReqs = st.Successful
	rr.FailedRequests = st.Failed
	rr.ServerBytes = st.BytesServed
	rr.NonRangeReqs = int(st.TotalRequests) - len(st.RequestedRanges)
	rr.RangeCount = len(st.RequestedRanges)
	for i, rg := range st.RequestedRanges {
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
	rr.Retries = int64(countRetries(string(outBytes)))
	if rr.OutputSHA256 == expectedSHA {
		rr.SuccessBytes = fileSize
	}
	rr.ServerRequests = srv.RequestLog()
	rr.MaxInFlight, rr.MeanInFlight = inFlightStats(rr.ServerRequests)
	rr.PreTransition, rr.PostTransition = prePost(rr.ServerRequests)
	for _, ev := range rr.PhaseEvents {
		if ev.Err != "" {
			rr.NetemStatus = "unavailable"
			rr.Err = "netem phase: " + ev.Err
		}
	}
	rr.Valid = rr.Err == "" && rr.OutputSize == fileSize && rr.OutputSHA256 == expectedSHA && rr.NetemStatus != "unavailable"
	return rr
}

// schedulePhases applies phase 0 immediately (caller does) and phases 1..n-1 at
// fixed wall-clock offsets; wait() blocks until the goroutines stop.
func schedulePhases(srv *server.Server, nc *network.Controller, c Condition, start time.Time) ([]PhaseEvent, func() []PhaseEvent) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var events []PhaseEvent
	for i := 1; i < len(c.Phase); i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTimer(time.Duration(float64(i) * phaseSeconds * float64(time.Second)))
			defer t.Stop()
			select {
			case <-done:
				return
			case now := <-t.C:
				srv.SetRate(c.Phase[i].Rate)
				err := applyNetPhase(nc, c.Phase[i])
				ev := PhaseEvent{Index: i, AtMs: float64(now.Sub(start).Microseconds()) / 1000.0}
				if err != nil {
					ev.Err = err.Error()
				}
				mu.Lock()
				events = append(events, ev)
				mu.Unlock()
			}
		}()
	}
	wait := func() []PhaseEvent {
		close(done)
		wg.Wait()
		return events
	}
	return events, wait
}

func applyNetPhase(nc *network.Controller, p PhaseSpec) error {
	if p.Link == 0 && p.LatencyMs == 0 && p.LossPercent == 0 {
		_ = nc.Clear("lo")
		return nil
	}
	cfg := network.Config{BandwidthBps: p.Link * 8, Latency: time.Duration(p.LatencyMs) * time.Millisecond, LossPercent: p.LossPercent}
	if err := cfg.Validate(); err != nil {
		return err
	}
	return nc.Apply("lo", cfg)
}

func netemStatus(p PhaseSpec) string {
	if p.Link == 0 && p.LatencyMs == 0 && p.LossPercent == 0 {
		return "none"
	}
	return "applied"
}

// ---------------------------------------------------------------------------
// Helpers (authoritative derivation)
// ---------------------------------------------------------------------------

func deriveActions(final int, decisions []dqn.Decision, times []time.Time, start time.Time) []ActionEvent {
	out := make([]ActionEvent, 0, len(decisions))
	for i, d := range decisions {
		after := final
		if i+1 < len(decisions) {
			after = decisions[i+1].ConcurrencyBefore
		}
		ts := -1.0
		if i < len(times) && !start.IsZero() {
			ts = float64(times[i].Sub(start).Microseconds()) / 1000.0
		}
		out = append(out, ActionEvent{DecisionIndex: i, TimeSinceStartMs: ts,
			RequestedActionIndex: d.ActionIndex, ConcurrencyBefore: d.ConcurrencyBefore,
			ConcurrencyAfter: after, Changed: after != d.ConcurrencyBefore, RemainingBytes: d.RemainingBytes})
	}
	return out
}

func countChanges(a []ActionEvent) int {
	n := 0
	for _, x := range a {
		if x.Changed {
			n++
		}
	}
	return n
}

func maxConc(initial int, a []ActionEvent) int {
	m := initial
	for _, x := range a {
		if x.ConcurrencyAfter > m {
			m = x.ConcurrencyAfter
		}
		if x.ConcurrencyBefore > m {
			m = x.ConcurrencyBefore
		}
	}
	return m
}

func classify(a []ActionEvent) (constant, rampToMax, monoDec bool) {
	inc, dec := 0, 0
	for _, x := range a {
		if x.ConcurrencyAfter > x.ConcurrencyBefore {
			inc++
		} else if x.ConcurrencyAfter < x.ConcurrencyBefore {
			dec++
		}
	}
	constant = inc == 0 && dec == 0
	last := 0
	if len(a) > 0 {
		last = a[len(a)-1].ConcurrencyAfter
	}
	rampToMax = dec == 0 && inc > 0 && last == maxConcurrency
	monoDec = inc == 0 && dec > 0
	return
}

func concurrencySegments(initial int, a []ActionEvent, completionMs float64) map[int]float64 {
	d := map[int]float64{}
	if completionMs <= 0 {
		return d
	}
	cur, prev := initial, 0.0
	for _, x := range a {
		t := x.TimeSinceStartMs
		if t < 0 {
			continue
		}
		if t > completionMs {
			t = completionMs
		}
		if t > prev {
			d[cur] += (t - prev) / 1000.0
		}
		if x.ConcurrencyAfter >= 1 && x.ConcurrencyAfter <= maxConcurrency {
			cur = x.ConcurrencyAfter
		}
		prev = t
	}
	if completionMs > prev {
		d[cur] += (completionMs - prev) / 1000.0
	}
	return d
}

func weightedMean(seg map[int]float64) float64 {
	total, w := 0.0, 0.0
	for c, d := range seg {
		total += d
		w += float64(c) * d
	}
	if total <= 0 {
		return 0
	}
	return w / total
}

func windowMean(initial int, a []ActionEvent, lo, hi float64) float64 {
	cur := initial
	for _, x := range a {
		if x.TimeSinceStartMs >= 0 && x.TimeSinceStartMs <= lo {
			cur = x.ConcurrencyAfter
		}
	}
	area, prev := 0.0, lo
	for _, x := range a {
		t := x.TimeSinceStartMs
		if t <= lo || t >= hi {
			continue
		}
		area += float64(cur) * (t - prev)
		cur = x.ConcurrencyAfter
		prev = t
	}
	area += float64(cur) * (hi - prev)
	if hi <= lo {
		return 0
	}
	return area / (hi - lo)
}

func phaseStats(initial int, a []ActionEvent, completionMs float64, n int) ([]float64, []int) {
	means := make([]float64, n)
	changes := make([]int, n)
	for i := 0; i < n; i++ {
		lo := float64(i) * phaseSeconds * 1000.0
		hi := float64(i+1) * phaseSeconds * 1000.0
		if completionMs < hi {
			hi = completionMs
		}
		if hi <= lo {
			means[i] = -1
			continue
		}
		means[i] = windowMean(initial, a, lo, hi)
		for _, x := range a {
			if x.Changed && x.TimeSinceStartMs >= lo && x.TimeSinceStartMs < hi {
				changes[i]++
			}
		}
	}
	return means, changes
}

func reactionLatencies(a []ActionEvent, n int) []float64 {
	out := []float64{}
	for i := 1; i < n; i++ {
		tr := float64(i) * phaseSeconds * 1000.0
		lat := -1.0
		for _, x := range a {
			if x.TimeSinceStartMs >= tr && x.Changed {
				lat = x.TimeSinceStartMs - tr
				break
			}
		}
		out = append(out, lat)
	}
	return out
}

func inFlightStats(events []server.RequestEvent) (int64, float64) {
	type point struct {
		t     time.Time
		delta int
	}
	pts := []point{}
	for _, e := range events {
		if e.End.IsZero() || !e.End.After(e.Start) {
			continue
		}
		pts = append(pts, point{e.Start, +1}, point{e.End, -1})
	}
	if len(pts) == 0 {
		return 0, 0
	}
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].t.Equal(pts[j].t) {
			return pts[i].delta < pts[j].delta
		}
		return pts[i].t.Before(pts[j].t)
	})
	cur, max, area := 0, 0, 0.0
	var prev time.Time
	first := true
	for _, p := range pts {
		if !first {
			area += float64(cur) * p.t.Sub(prev).Seconds()
		}
		cur += p.delta
		if cur > max {
			max = cur
		}
		prev = p.t
		first = false
	}
	span := pts[len(pts)-1].t.Sub(pts[0].t).Seconds()
	if span <= 0 {
		return int64(max), float64(max)
	}
	return int64(max), area / span
}

func prePost(events []server.RequestEvent) (pre, post int) {
	if len(events) == 0 {
		return
	}
	t0 := events[0].Start
	for _, e := range events {
		if e.Start.Sub(t0).Seconds() >= phaseSeconds {
			post++
		} else {
			pre++
		}
	}
	return
}

func repOf(p PairSchedule) int {
	i := strings.LastIndex(p.PairID, "-p")
	if i < 0 {
		return 0
	}
	var r int
	_, _ = fmt.Sscanf(p.PairID[i+2:], "%d", &r)
	return r
}

func filterConditions(all []Condition, csv string) []Condition {
	if csv == "" {
		return all
	}
	want := map[string]bool{}
	for _, s := range strings.Split(csv, ",") {
		want[strings.TrimSpace(s)] = true
	}
	var out []Condition
	for _, c := range all {
		if want[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

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

func aria2VersionString() (string, error) {
	out, err := exec.Command("aria2c", "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]), nil
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

func lcg(seed int64) func(n int) int {
	s := uint64(seed)*6364136223846793005 + 1442695040888963407
	return func(n int) int {
		s = s*6364136223846793005 + 1442695040888963407
		return int((s >> 33) % uint64(n))
	}
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

func sha256File(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Bytes(b), nil
}

func sha256Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

var _ = math.Sqrt // reserved for future report math

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "final-evaluation-v6:", err)
	os.Exit(1)
}
