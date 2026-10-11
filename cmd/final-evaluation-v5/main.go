// Command final-evaluation-v5 runs the FINAL head-to-head:
// DQN Candidate v5 vs FINAL ARIA2 BASELINE CONFIG v2 (candidate F), on the
// established locked 8-condition U1-U8 matrix with 30 matched pairs per
// condition (480 downloads), at the frozen 16 MiB / 1 MiB-granularity scale.
//
// Methodology is reused from cmd/final-evaluation (same conditions, matched-pair
// structure, deterministic order seed, fresh server/output/netem per run,
// per-run netem cleanup) and from python/analyze_final_evaluation.py for the
// statistics. The raw analyzer schema keeps the established system keys
// ("dqn" = the learned policy, i.e. Candidate v5 here, and "aria2").
//
// STRUCTURAL ASYMMETRY (must not be hidden): Candidate v5 is bounded to 4
// concurrent requests, while frozen aria2 candidate F uses up to 16
// connections. Candidate F was selected by the v2 calibration precisely because
// the server's per-response pacing means only >=16 connections yield the
// intended ~16 x 1 MiB range split at min-split-size = 1 MiB. The controlled
// server paces each response independently, so aria2-F can exploit up to 16
// independently paced responses while v5 can exploit at most 4. This is a
// comparison of two complete frozen configurations, not a capability-matched
// comparison.
//
// No tuning, no post-hoc changes, no modification of any frozen artifact.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl/dqn"
)

const (
	fileSize       = int64(16 * 1024 * 1024)
	fixedChunkSize = int64(1 << 20)
	minConcurrency = 1
	maxConcurrency = 4
	maxRetries     = 3
	runTimeout     = 30 * time.Second
	policyWindow   = 4

	// Frozen artifacts.
	v5SHA256     = "3a927a58baa7f9b5f150827ba0bd81defc95adb25e5328eb037f5edf9925da75"
	aria2SHA256  = "b72498176a48f889cd2d1dbc9d9ef6b9afc668f2cf0d902e44d51dee0dedca02"
	aria2Version = "aria2 version 1.37.0"

	reps         = 30
	scheduleSeed = 20260925
)

// Condition is one unstable-network profile (wall-clock transitions only),
// reused verbatim from the established final-evaluation matrix (cmd/final-evaluation).
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

// System keys: the established Python analyzer reads "dqn" and "aria2".
const (
	sysLearned = "dqn"   // Candidate v5
	sysAria2   = "aria2" // frozen candidate F
)

type PairSchedule struct {
	PairID     string `json:"pair_id"`
	Condition  string `json:"condition"`
	OrderFirst string `json:"first"`
	V5RunID    string `json:"v5_run_id"`
	Aria2RunID string `json:"aria2_run_id"`
}

func buildSchedule(conds []Condition, pairs int, seed int64) []PairSchedule {
	rng := rand.New(rand.NewSource(seed))
	var sched []PairSchedule
	for _, c := range conds {
		for r := 0; r < pairs; r++ {
			pid := fmt.Sprintf("%s-p%02d", c.ID, r)
			first := "v5"
			if rng.Intn(2) == 0 {
				first = "aria2"
			}
			sched = append(sched, PairSchedule{
				PairID: pid, Condition: c.ID, OrderFirst: first,
				V5RunID: pid + "-v5", Aria2RunID: pid + "-aria2",
			})
		}
	}
	return sched
}

// Validation holds per-run correctness checks.
type Validation struct {
	OutputSizeOK           bool     `json:"output_size_ok"`
	OutputSHAOK            bool     `json:"output_sha_ok"`
	RangeTilingOK          bool     `json:"range_tiling_ok"`
	SuccessBytesOK         bool     `json:"success_bytes_ok"`
	ProgressMonotone       bool     `json:"progress_monotone"`
	ProgressWithinTotal    bool     `json:"progress_within_total"`
	ProgressMatchesRecords bool     `json:"progress_matches_records"`
	ConcurrencyBoundsOK    bool     `json:"concurrency_bounds_ok"`
	ActionsInSpace         bool     `json:"actions_in_space"`
	NetemOK                bool     `json:"netem_ok"`
	Valid                  bool     `json:"valid"`
	Notes                  []string `json:"notes,omitempty"`
}

// ActionEvent is one authoritative v5 decision (after-value from the next
// observed controller target, never the model's pre-application field).
type ActionEvent struct {
	DecisionIndex            int     `json:"decision_index"`
	TimeSinceScheduleStartMs float64 `json:"time_since_schedule_start_ms"`
	RequestedActionIndex     int     `json:"requested_action_index"`
	RequestedAction          string  `json:"requested_action"`
	ConcurrencyBefore        int     `json:"concurrency_before"`
	ConcurrencyAfter         int     `json:"concurrency_after"`
	Changed                  bool    `json:"changed"`
	RemainingBytes           int64   `json:"remaining_bytes"`
}

type RunResult struct {
	RunID          string  `json:"run_id"`
	PairID         string  `json:"pair_id"`
	Condition      string  `json:"condition"`
	System         string  `json:"system"` // "dqn" (Candidate v5) | "aria2"
	Policy         string  `json:"policy"` // "v5" | "aria2-f"
	OrderIndex     int     `json:"order_index"`
	PairFirst      string  `json:"pair_first_system"`
	DurationMs     float64 `json:"duration_ms"`
	WallclockBps   float64 `json:"wallclock_bps"`
	OK             bool    `json:"ok"`
	OutputSize     int64   `json:"output_size"`
	OutputSHA256   string  `json:"output_sha256"`
	ExpectedSHA256 string  `json:"expected_sha256"`
	OutputMatches  bool    `json:"output_matches"`
	NetemStatus    string  `json:"netem_status"`
	TotalRequests  int64   `json:"total_requests"`
	SuccessfulReqs int64   `json:"successful_requests"`
	FailedRequests int64   `json:"failed_requests"`
	Retries        int64   `json:"retries"`
	SuccessBytes   int64   `json:"success_bytes"`
	ServerBytes    int64   `json:"server_bytes_served"`
	MaxInFlight    int64   `json:"max_in_flight"`
	MeanInFlight   float64 `json:"mean_in_flight"`
	NonRangeReqs   int     `json:"non_range_requests"`
	RangeCount     int     `json:"range_count"`
	RangeMinBytes  int64   `json:"range_min_bytes"`
	RangeMaxBytes  int64   `json:"range_max_bytes"`
	// v5 behavior (empty omits for aria2).
	InitialConcurrency      int            `json:"initial_concurrency,omitempty"`
	FinalConcurrency        int            `json:"final_concurrency,omitempty"`
	MaxConcurrency          int            `json:"max_concurrency,omitempty"`
	TimeWeightedConcurrency float64        `json:"time_weighted_concurrency,omitempty"`
	EffectiveConcChanges    int            `json:"effective_concurrency_changes,omitempty"`
	Decisions               int            `json:"decisions,omitempty"`
	ConstantRun             bool           `json:"constant_run"`
	RampToMax               bool           `json:"ramp_to_max"`
	Actions                 []ActionEvent  `json:"actions,omitempty"`
	ModelDecisions          []dqn.Decision `json:"model_decisions,omitempty"`
	// Transition observability.
	PreTransition   int     `json:"requests_before_transition"`
	PostTransition  int     `json:"requests_at_or_after_transition"`
	FirstPostS      float64 `json:"first_post_transition_elapsed_s"`
	FirstDecisionS  float64 `json:"first_v5_decision_after_transition_s,omitempty"`
	FirstChangeS    float64 `json:"first_v5_change_after_transition_s,omitempty"`
	ReactionLatency float64 `json:"reaction_latency_ms,omitempty"`
	// Config identity.
	ModelSHA256     string     `json:"model_sha256,omitempty"`
	Aria2ConfigHash string     `json:"aria2_config_sha256,omitempty"`
	Err             string     `json:"err"`
	Validation      Validation `json:"validation"`

	ServerRequests []server.RequestEvent `json:"server_requests,omitempty"`
}

// Matrix is locked and hashed before any run.
type Matrix struct {
	Label          string      `json:"label"`
	Conditions     []Condition `json:"conditions"`
	Repetitions    int         `json:"repetitions"`
	Pairs          int         `json:"pairs"`
	TotalDownloads int         `json:"total_downloads"`
	FileSize       int64       `json:"file_size"`
	V5SHA256       string      `json:"v5_sha256"`
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
	modelPath := flag.String("model", filepath.Join("testdata", "diagnostics", "dqn_validation_v5", "final_dqn_candidate_v5.json"), "frozen Candidate v5 model")
	baselinePath := flag.String("baseline", filepath.Join("testdata", "diagnostics", "aria2_calibration_v2", "selected_baseline_v2.json"), "frozen aria2 v2 baseline metadata")
	outDir := flag.String("out", filepath.Join("testdata", "final_evaluation_v5"), "output directory")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	seed := flag.Int64("seed", scheduleSeed, "deterministic schedule seed")
	condFilter := flag.String("conditions", "", "comma-separated condition subset (default all)")
	pairs := flag.Int("pairs", reps, "matched pairs per condition")
	flag.Parse()

	// ---- freeze checks ----
	if got, err := sha256File(*modelPath); err != nil || got != v5SHA256 {
		fatal(fmt.Errorf("v5 model hash mismatch: got %s err %v", got, err))
	}
	baseline, err := os.ReadFile(*baselinePath)
	if err != nil {
		fatal(err)
	}
	if got := sha256Bytes(baseline); got != aria2SHA256 {
		fatal(fmt.Errorf("aria2 v2 baseline metadata hash mismatch: got %s want %s", got, aria2SHA256))
	}
	var bl struct {
		Command      string `json:"command"`
		Aria2Version string `json:"aria2_version"`
		Label        string `json:"label"`
		Selected     struct {
			Name     string `json:"name"`
			MaxConns int    `json:"max_connection_per_server"`
			Split    int    `json:"split"`
			MinSplit int64  `json:"min_split_size"`
		} `json:"selected_config"`
	}
	if err := json.Unmarshal(baseline, &bl); err != nil {
		fatal(err)
	}
	if bl.Selected.Name != "F" || bl.Selected.MaxConns != 16 || bl.Selected.Split != 16 || bl.Selected.MinSplit != 1<<20 {
		fatal(fmt.Errorf("baseline metadata is not candidate F: %+v", bl.Selected))
	}
	for _, required := range []string{"--max-connection-per-server=16", "--split=16", "--min-split-size=1048576"} {
		if !strings.Contains(bl.Command, required) {
			fatal(fmt.Errorf("baseline command missing %q: %s", required, bl.Command))
		}
	}
	version, err := aria2VersionString()
	if err != nil {
		fatal(err)
	}
	if version != aria2Version {
		fatal(fmt.Errorf("aria2 version is %q, frozen baseline expects %q", version, aria2Version))
	}
	aria2Args := aria2ArgsFromCommand(bl.Command)

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}
	logFile, err := os.Create(filepath.Join(*outDir, "run.log"))
	if err != nil {
		fatal(err)
	}
	defer logFile.Close()
	logw := bufio.NewWriter(logFile)
	defer logw.Flush()
	logf := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		fmt.Println(line)
		fmt.Fprintln(logw, line)
		logw.Flush()
	}

	allConditions := filterConditions(conditions(), *condFilter)
	if len(allConditions) == 0 || *pairs < 1 {
		fatal(fmt.Errorf("empty matrix"))
	}

	// ---- lock the matrix BEFORE any run ----
	m := Matrix{
		Label:      "FINAL head-to-head test matrix (DQN Candidate v5 vs aria2 baseline v2 / candidate F)",
		Conditions: allConditions, Repetitions: *pairs, Pairs: len(allConditions) * *pairs,
		TotalDownloads: len(allConditions) * *pairs * 2, FileSize: fileSize,
		V5SHA256: v5SHA256, Aria2SHA256: aria2SHA256, Aria2Version: aria2Version, Seed: *seed,
		Note: "U1-U8 reused verbatim from the established final-evaluation matrix; 16 MiB scale per the frozen v5/aria2-v2 contract, applied identically to both systems; wall-clock schedules only; " +
			"STRUCTURAL ASYMMETRY: v5 max concurrency 4 vs aria2-F max connections 16 with independent per-response server pacing.",
	}
	matrixBytes := mustJSON(m)
	if err := os.WriteFile(filepath.Join(*outDir, "final_test_matrix_v5.json"), append(matrixBytes, '\n'), 0o644); err != nil {
		fatal(err)
	}
	msum := sha256Bytes(matrixBytes)
	if err := os.WriteFile(filepath.Join(*outDir, "final_test_matrix_v5.sha256"), []byte(msum+"\n"), 0o644); err != nil {
		fatal(err)
	}
	logf("matrix locked: %d conditions, %d pairs, %d downloads, sha256=%s", len(m.Conditions), m.Pairs, m.TotalDownloads, msum)

	sched := buildSchedule(allConditions, *pairs, *seed)
	writeJSON(filepath.Join(*outDir, "execution_schedule.json"), map[string]any{"seed": *seed, "pairs": sched})

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
	for _, c := range allConditions {
		condByID[c.ID] = c
	}

	var results []RunResult
	idx := 0
	for _, p := range sched {
		c := condByID[p.Condition]
		order := []string{p.OrderFirst}
		if p.OrderFirst == "v5" {
			order = append(order, "aria2")
		} else {
			order = append(order, "v5")
		}
		for _, sys := range order {
			idx++
			var rr RunResult
			if sys == "v5" {
				rr = runV5(model, c, p, content, expectedSHA, nc, idx)
			} else {
				rr = runAria2(aria2Args, c, p, content, expectedSHA, nc, idx)
			}
			results = append(results, rr)
			logf("[%3d/%d] %s %-3s %-5s dur=%8.1fms valid=%-5v netem=%s", idx, m.TotalDownloads, p.PairID, c.ID, sys, rr.DurationMs, rr.Validation.Valid, rr.NetemStatus)
		}
	}

	// ---- analyzer input: only pairs where both runs are valid ----
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
		mPair := byPair[s.PairID]
		d, okD := mPair[sysLearned]
		a, okA := mPair["aria2"]
		if !okD || !okA || !d.Validation.Valid || !a.Validation.Valid {
			continue
		}
		validPairs++
		for _, r := range []RunResult{d, a} {
			lean = append(lean, map[string]any{
				"run_id": r.RunID, "pair_id": r.PairID, "condition": r.Condition,
				"system": r.System, "policy": r.Policy, "duration_ms": r.DurationMs,
				"wallclock_bps": r.WallclockBps, "ok": r.OK,
			})
		}
	}
	writeJSON(filepath.Join(*outDir, "raw_results.json"), map[string]any{
		"matrix_sha256": msum, "aria2_version": aria2Version,
		"v5_sha256": v5SHA256, "aria2_baseline_sha256": aria2SHA256,
		"systems": map[string]string{
			sysLearned: "Candidate v5 (state spec v2, 1 MiB fixed chunk, concurrency 1-4)",
			sysAria2:   "FINAL ARIA2 BASELINE CONFIG v2 (candidate F: 16 connections, split 16, min-split 1 MiB)",
		},
		"results": lean,
	})
	writeJSON(filepath.Join(*outDir, "raw_results_all.json"), map[string]any{
		"matrix_sha256": msum, "expected_content_sha256": expectedSHA,
		"v5_sha256": v5SHA256, "aria2_baseline_sha256": aria2SHA256,
		"aria2_command": bl.Command, "aria2_args": aria2Args, "aria2_version": aria2Version,
		"results": results,
	})

	// ---- traces ----
	writeJSON(filepath.Join(*outDir, "request_traces.json"), map[string]any{
		"runs": tracesFrom(results),
	})

	// ---- validity report ----
	{
		valid, invalid := 0, 0
		var exclusions []map[string]any
		for _, r := range results {
			if r.Validation.Valid {
				valid++
			} else {
				invalid++
				exclusions = append(exclusions, map[string]any{
					"run_id": r.RunID, "pair_id": r.PairID, "condition": r.Condition,
					"system": r.System, "policy": r.Policy, "notes": r.Validation.Notes, "err": r.Err,
				})
			}
		}
		writeJSON(filepath.Join(*outDir, "validity_report.json"), map[string]any{
			"expected_runs": m.TotalDownloads, "recorded_runs": len(results),
			"valid_runs": valid, "invalid_runs": invalid,
			"expected_pairs": m.Pairs, "valid_pairs": validPairs,
			"excluded_pairs": m.Pairs - validPairs,
			"exclusions":     exclusions,
			"rule":           "a run is valid only if it has no error, the output is exactly 16 MiB and SHA-256 matches the served content, v5 accounting checks pass, and any requested netem condition was applied",
		})
	}

	// ---- asymmetry report ----
	writeJSON(filepath.Join(*outDir, "asymmetry_report.json"), asymmetryReport(bl.Command, results))

	// ---- manifest ----
	writeJSON(filepath.Join(*outDir, "manifest_v5.json"), map[string]any{
		"label":                   m.Label,
		"matrix_sha256":           msum,
		"v5_model_path":           *modelPath,
		"v5_sha256":               v5SHA256,
		"aria2_baseline_path":     *baselinePath,
		"aria2_baseline_sha256":   aria2SHA256,
		"aria2_version":           aria2Version,
		"aria2_command":           bl.Command,
		"aria2_args":              aria2Args,
		"file_size":               fileSize,
		"chunk_size":              fixedChunkSize,
		"v5_concurrency_bounds":   []int{minConcurrency, maxConcurrency},
		"aria2_max_connections":   16,
		"conditions":              allConditions,
		"pairs_per_condition":     *pairs,
		"pairs":                   m.Pairs,
		"total_downloads":         m.TotalDownloads,
		"schedule_seed":           *seed,
		"expected_content_sha256": expectedSHA,
		"source_methodology":      "cmd/final-evaluation (U1-U8 matrix, matched pairs, order seed) + python/analyze_final_evaluation.py (paired log-ratio analysis)",
		"structural_asymmetry":    "v5 max concurrency 4 vs aria2-F max connections 16; the server paces each response independently, so aria2-F can exploit up to 16 independently paced responses while v5 can exploit at most 4; not a capability-matched comparison",
	})
	writeJSON(filepath.Join(*outDir, "artifact_hashes.json"), artifactHashes(*outDir))

	logf("wrote %d runs (%d valid pairs) to %s", len(results), validPairs, *outDir)
}

// ---------------------------------------------------------------------------
// Run execution
// ---------------------------------------------------------------------------

func runV5(model *dqn.Model, c Condition, p PairSchedule, content []byte, expectedSHA string, nc *network.Controller, orderIdx int) RunResult {
	rr := RunResult{
		RunID: p.V5RunID, PairID: p.PairID, Condition: c.ID, System: sysLearned,
		Policy: "v5", OrderIndex: orderIdx, PairFirst: p.OrderFirst,
		ExpectedSHA256: expectedSHA, ModelSHA256: v5SHA256, NetemStatus: "none",
	}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRate(c.InitialRate)
	srv.SetRequestLogEnabled(true)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	if !applyNet(nc, c, &rr) {
		rr.Validation = invalidValidation("netem apply failed")
		return rr
	}
	defer clearNet(nc, c)
	dir, err := os.MkdirTemp("", "final-v5-")
	if err != nil {
		rr.Err = err.Error()
		rr.Validation = invalidValidation(rr.Err)
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	pol, err := dqn.NewLiveConcurrencyPolicyV5(model, fileSize)
	if err != nil {
		rr.Err = err.Error()
		rr.Validation = invalidValidation(rr.Err)
		return rr
	}
	tp := &timedPolicy{inner: pol}
	dl, err := downloader.New(downloader.Config{
		URL: ts.URL, OutputPath: outPath, TotalSize: fileSize,
		Concurrency: minConcurrency, ChunkSize: fixedChunkSize,
		MinConcurrency: minConcurrency, MaxConcurrency: maxConcurrency,
		Timeout: runTimeout, MaxRetries: maxRetries, PolicyWindowSize: policyWindow,
		Policy: tp,
	})
	if err != nil {
		rr.Err = err.Error()
		rr.Validation = invalidValidation(rr.Err)
		return rr
	}
	rr.InitialConcurrency = dl.Concurrency()
	start := time.Now()
	tp.start = start
	if c.TransitionAfter > 0 {
		if err := srv.SetRateScheduleAt(start, server.RateSchedule{
			InitialRate: c.InitialRate, TransitionAfter: dur(c.TransitionAfter), SubsequentRate: c.SubsequentRate,
		}); err != nil {
			rr.Err = err.Error()
			rr.Validation = invalidValidation(rr.Err)
			return rr
		}
	}
	runErr := dl.Download(context.Background())
	rr.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	if rr.DurationMs > 0 {
		rr.WallclockBps = float64(fileSize) / (rr.DurationMs / 1000.0)
	}
	if runErr != nil {
		rr.Err = runErr.Error()
	}
	rr.FinalConcurrency = dl.Concurrency()

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
		l := rec.RangeEnd - rec.RangeStart + 1
		if rr.RangeCount == 0 {
			rr.RangeMinBytes, rr.RangeMaxBytes = l, l
		}
		rr.RangeCount++
		if l < rr.RangeMinBytes {
			rr.RangeMinBytes = l
		}
		if l > rr.RangeMaxBytes {
			rr.RangeMaxBytes = l
		}
	}
	rr.ServerRequests = srv.RequestLog()
	st := srv.Stats()
	rr.ServerBytes = st.BytesServed
	rr.MaxInFlight, rr.MeanInFlight = inFlightStats(rr.ServerRequests)

	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputSize = int64(len(got))
		rr.OutputSHA256 = sha256Bytes(got)
		rr.OutputMatches = rr.OutputSHA256 == expectedSHA
	}
	log := pol.Log()
	rr.ModelDecisions = log
	rr.Decisions = len(log)
	rr.Actions = deriveActions(rr.FinalConcurrency, log, tp.times, start)
	rr.EffectiveConcChanges = countChanges(rr.Actions)
	rr.MaxConcurrency = maxConcurrencyObserved(rr.InitialConcurrency, rr.Actions)
	rr.TimeWeightedConcurrency = weightedMean(concurrencySegments(rr.InitialConcurrency, rr.Actions, rr.DurationMs, maxConcurrency))
	constant, ramp, _ := classifyTrajectory(rr.Actions, maxConcurrency)
	rr.ConstantRun, rr.RampToMax = constant, ramp
	rr.PreTransition, rr.PostTransition, rr.FirstPostS = transitionCounts(rr.ServerRequests, c.TransitionAfter)
	rr.FirstDecisionS, rr.FirstChangeS, rr.ReactionLatency = firstReactionAfter(rr.Actions, c.TransitionAfter)

	// accounting checks
	mono, within, matches := checkProgress(log, snap.Records, fileSize)
	rr.Validation = Validation{
		OutputSizeOK:           rr.OutputSize == fileSize,
		OutputSHAOK:            rr.OutputSHA256 == expectedSHA,
		RangeTilingOK:          validateRangeTiling(snap.Records, fileSize),
		SuccessBytesOK:         rr.SuccessBytes == fileSize,
		ProgressMonotone:       mono,
		ProgressWithinTotal:    within,
		ProgressMatchesRecords: matches,
		ConcurrencyBoundsOK:    rr.MaxConcurrency <= maxConcurrency && rr.FinalConcurrency >= minConcurrency && rr.InitialConcurrency >= minConcurrency,
		ActionsInSpace:         actionsInSpace(rr.Actions),
		NetemOK:                rr.NetemStatus != "unavailable",
	}
	for _, a := range rr.Actions {
		if a.ConcurrencyBefore < minConcurrency || a.ConcurrencyBefore > maxConcurrency || a.ConcurrencyAfter < minConcurrency || a.ConcurrencyAfter > maxConcurrency {
			rr.Validation.ConcurrencyBoundsOK = false
			rr.Validation.Notes = append(rr.Validation.Notes, "concurrency left [1,4]")
		}
	}
	if rr.Err != "" {
		rr.Validation.Notes = append(rr.Validation.Notes, "run error: "+rr.Err)
	}
	rr.Validation.Valid = rr.Validation.OutputSizeOK && rr.Validation.OutputSHAOK &&
		rr.Validation.RangeTilingOK && rr.Validation.SuccessBytesOK &&
		rr.Validation.ProgressMonotone && rr.Validation.ProgressWithinTotal &&
		rr.Validation.ProgressMatchesRecords && rr.Validation.ConcurrencyBoundsOK &&
		rr.Validation.ActionsInSpace && rr.Validation.NetemOK && rr.Err == ""
	rr.OK = rr.Validation.Valid
	return rr
}

func runAria2(aria2Args []string, c Condition, p PairSchedule, content []byte, expectedSHA string, nc *network.Controller, orderIdx int) RunResult {
	rr := RunResult{
		RunID: p.Aria2RunID, PairID: p.PairID, Condition: c.ID, System: sysAria2,
		Policy: "aria2-f", OrderIndex: orderIdx, PairFirst: p.OrderFirst,
		ExpectedSHA256: expectedSHA, Aria2ConfigHash: aria2SHA256, NetemStatus: "none",
	}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRate(c.InitialRate)
	srv.SetRequestLogEnabled(true)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	if !applyNet(nc, c, &rr) {
		rr.Validation = invalidValidation("netem apply failed")
		return rr
	}
	defer clearNet(nc, c)
	dir, err := os.MkdirTemp("", "final-aria2-")
	if err != nil {
		rr.Err = err.Error()
		rr.Validation = invalidValidation(rr.Err)
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")
	args := append(append([]string(nil), aria2Args...), "-d", dir, "-o", "out.bin", "--interface=lo", ts.URL)
	start := time.Now()
	if c.TransitionAfter > 0 {
		if err := srv.SetRateScheduleAt(start, server.RateSchedule{
			InitialRate: c.InitialRate, TransitionAfter: dur(c.TransitionAfter), SubsequentRate: c.SubsequentRate,
		}); err != nil {
			rr.Err = err.Error()
			rr.Validation = invalidValidation(rr.Err)
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
	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputSize = int64(len(got))
		rr.OutputSHA256 = sha256Bytes(got)
		rr.OutputMatches = rr.OutputSHA256 == expectedSHA
	}
	stats := srv.Stats()
	rr.TotalRequests = stats.TotalRequests
	rr.SuccessfulReqs = stats.Successful
	rr.FailedRequests = stats.Failed
	rr.ServerBytes = stats.BytesServed
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
	rr.Retries = int64(countRetries(string(outBytes)))
	rr.SuccessBytes = 0
	if rr.OutputMatches {
		rr.SuccessBytes = fileSize
	}
	rr.ServerRequests = srv.RequestLog()
	rr.MaxInFlight, rr.MeanInFlight = inFlightStats(rr.ServerRequests)
	rr.PreTransition, rr.PostTransition, rr.FirstPostS = transitionCounts(rr.ServerRequests, c.TransitionAfter)
	rr.Validation = Validation{
		OutputSizeOK: rr.OutputSize == fileSize,
		OutputSHAOK:  rr.OutputSHA256 == expectedSHA,
		NetemOK:      rr.NetemStatus != "unavailable",
		// v5-specific checks do not apply to aria2.
		RangeTilingOK: true, SuccessBytesOK: true,
		ProgressMonotone: true, ProgressWithinTotal: true, ProgressMatchesRecords: true,
		ConcurrencyBoundsOK: true, ActionsInSpace: true,
	}
	if rr.Err != "" {
		rr.Validation.Notes = append(rr.Validation.Notes, "run error: "+rr.Err)
	}
	rr.Validation.Valid = rr.Validation.OutputSizeOK && rr.Validation.OutputSHAOK &&
		rr.Validation.NetemOK && rr.Err == ""
	rr.OK = rr.Validation.Valid
	return rr
}

func applyNet(nc *network.Controller, c Condition, rr *RunResult) bool {
	if c.LatencyMs == 0 && c.LossPercent == 0 {
		return true
	}
	if err := nc.Apply("lo", network.Config{
		Latency:     time.Duration(c.LatencyMs) * time.Millisecond,
		LossPercent: c.LossPercent,
	}); err != nil {
		rr.NetemStatus = "unavailable"
		rr.Err = "netem apply: " + err.Error()
		return false
	}
	rr.NetemStatus = "applied"
	return true
}

func clearNet(nc *network.Controller, c Condition) {
	if c.LatencyMs == 0 && c.LossPercent == 0 {
		return
	}
	_ = nc.Clear("lo")
}

// ---------------------------------------------------------------------------
// Pure helpers (unit-tested)
// ---------------------------------------------------------------------------

func deriveActions(finalConcurrency int, decisions []dqn.Decision, times []time.Time, start time.Time) []ActionEvent {
	out := make([]ActionEvent, 0, len(decisions))
	for i, d := range decisions {
		after := finalConcurrency
		if i+1 < len(decisions) {
			after = decisions[i+1].ConcurrencyBefore
		}
		ts := -1.0
		if i < len(times) && !start.IsZero() {
			ts = float64(times[i].Sub(start).Microseconds()) / 1000.0
		}
		out = append(out, ActionEvent{
			DecisionIndex: i, TimeSinceScheduleStartMs: ts,
			RequestedActionIndex: d.ActionIndex, RequestedAction: d.Action,
			ConcurrencyBefore: d.ConcurrencyBefore, ConcurrencyAfter: after,
			Changed: after != d.ConcurrencyBefore, RemainingBytes: d.RemainingBytes,
		})
	}
	return out
}

func countChanges(actions []ActionEvent) int {
	n := 0
	for _, a := range actions {
		if a.Changed {
			n++
		}
	}
	return n
}

func maxConcurrencyObserved(initial int, actions []ActionEvent) int {
	m := initial
	for _, a := range actions {
		if a.ConcurrencyAfter > m {
			m = a.ConcurrencyAfter
		}
		if a.ConcurrencyBefore > m {
			m = a.ConcurrencyBefore
		}
	}
	return m
}

func actionsInSpace(actions []ActionEvent) bool {
	for _, a := range actions {
		if a.RequestedActionIndex < 0 || a.RequestedActionIndex >= dqn.NumConcurrencyActions {
			return false
		}
	}
	return true
}

func classifyTrajectory(actions []ActionEvent, maxC int) (constant, rampToMax, monotoneDecrease bool) {
	increases, decreases := 0, 0
	for _, a := range actions {
		if a.ConcurrencyAfter > a.ConcurrencyBefore {
			increases++
		} else if a.ConcurrencyAfter < a.ConcurrencyBefore {
			decreases++
		}
	}
	constant = increases == 0 && decreases == 0
	last := 0
	if len(actions) > 0 {
		last = actions[len(actions)-1].ConcurrencyAfter
	}
	rampToMax = decreases == 0 && increases > 0 && last == maxC
	monotoneDecrease = increases == 0 && decreases > 0
	return
}

func concurrencySegments(initial int, actions []ActionEvent, completionMs float64, maxC int) map[int]float64 {
	durations := map[int]float64{}
	if completionMs <= 0 {
		return durations
	}
	cur, prev := initial, 0.0
	for _, a := range actions {
		t := a.TimeSinceScheduleStartMs
		if t < 0 {
			continue
		}
		if t > completionMs {
			t = completionMs
		}
		if t > prev {
			durations[cur] += (t - prev) / 1000.0
		}
		if a.ConcurrencyAfter >= 1 && a.ConcurrencyAfter <= maxC {
			cur = a.ConcurrencyAfter
		}
		prev = t
	}
	if completionMs > prev {
		durations[cur] += (completionMs - prev) / 1000.0
	}
	return durations
}

func weightedMean(segments map[int]float64) float64 {
	total, weighted := 0.0, 0.0
	for c, d := range segments {
		total += d
		weighted += float64(c) * d
	}
	if total <= 0 {
		return 0
	}
	return weighted / total
}

func validateRangeTiling(records []metrics.Record, size int64) bool {
	type seg struct{ s, e int64 }
	var segs []seg
	for _, r := range records {
		if r.Success {
			segs = append(segs, seg{r.RangeStart, r.RangeEnd})
		}
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].s < segs[j].s })
	var prev int64 = -1
	for _, s := range segs {
		if s.s != prev+1 || s.e < s.s {
			return false
		}
		prev = s.e
	}
	return prev == size-1
}

func checkProgress(decisions []dqn.Decision, records []metrics.Record, size int64) (monotone, within, matches bool) {
	monotone, within, matches = true, true, true
	if len(decisions) != len(records) {
		matches = false
	}
	var cum int64
	prev := size
	for i, d := range decisions {
		if d.RemainingBytes < 0 || d.RemainingBytes > size {
			within = false
		}
		if d.RemainingBytes > prev {
			monotone = false
		}
		prev = d.RemainingBytes
		if i < len(records) {
			if records[i].Success {
				cum += records[i].Bytes
			}
			if d.RemainingBytes != size-cum {
				matches = false
			}
		}
	}
	return
}

// inFlightStats returns the maximum and time-weighted mean number of
// server-observed requests in flight, from the per-request start/end log.
func inFlightStats(events []server.RequestEvent) (int64, float64) {
	type point struct {
		t     time.Time
		delta int
	}
	pts := make([]point, 0, len(events)*2)
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

func transitionCounts(events []server.RequestEvent, transitionAfter float64) (pre, post int, firstPost float64) {
	if transitionAfter <= 0 || len(events) == 0 {
		return 0, 0, 0
	}
	t0 := events[0].Start
	for _, e := range events {
		el := e.Start.Sub(t0).Seconds()
		if el >= transitionAfter {
			post++
			if firstPost == 0 {
				firstPost = el
			}
		} else {
			pre++
		}
	}
	return
}

// firstReactionAfter reports the first v5 decision at/after the transition, the
// first effective concurrency change after it, and the reaction latency (ms).
func firstReactionAfter(actions []ActionEvent, transitionAfter float64) (firstDecisionS, firstChangeS, latencyMs float64) {
	if transitionAfter <= 0 {
		return -1, -1, -1
	}
	firstDecisionS, firstChangeS, latencyMs = -1, -1, -1
	for _, a := range actions {
		if a.TimeSinceScheduleStartMs < 0 || a.TimeSinceScheduleStartMs < transitionAfter*1000.0 {
			continue
		}
		if firstDecisionS < 0 {
			firstDecisionS = a.TimeSinceScheduleStartMs / 1000.0
		}
		if a.Changed && firstChangeS < 0 {
			firstChangeS = a.TimeSinceScheduleStartMs / 1000.0
			latencyMs = firstChangeS*1000.0 - transitionAfter*1000.0
		}
	}
	return
}

func invalidValidation(reason string) Validation {
	return Validation{Valid: false, Notes: []string{reason}}
}

// ---------------------------------------------------------------------------
// Reports
// ---------------------------------------------------------------------------

func tracesFrom(results []RunResult) []map[string]any {
	var out []map[string]any
	for _, r := range results {
		e := map[string]any{
			"run_id": r.RunID, "pair_id": r.PairID, "condition": r.Condition,
			"system": r.System, "policy": r.Policy, "valid": r.Validation.Valid,
			"duration_ms": r.DurationMs, "total_requests": r.TotalRequests,
			"successful_requests": r.SuccessfulReqs, "failed_requests": r.FailedRequests,
			"retries": r.Retries, "success_bytes": r.SuccessBytes,
			"max_in_flight": r.MaxInFlight, "mean_in_flight": r.MeanInFlight,
			"non_range_requests": r.NonRangeReqs, "range_count": r.RangeCount,
			"range_min_bytes": r.RangeMinBytes, "range_max_bytes": r.RangeMaxBytes,
			"pre_transition": r.PreTransition, "post_transition": r.PostTransition,
			"first_post_transition_elapsed_s": r.FirstPostS,
			"rate_histogram":                  rateHistogram(r.ServerRequests),
		}
		if r.System == sysLearned {
			e["initial_concurrency"] = r.InitialConcurrency
			e["final_concurrency"] = r.FinalConcurrency
			e["max_concurrency"] = r.MaxConcurrency
			e["time_weighted_concurrency"] = r.TimeWeightedConcurrency
			e["effective_concurrency_changes"] = r.EffectiveConcChanges
			e["decisions"] = r.Decisions
			e["constant_run"] = r.ConstantRun
			e["ramp_to_max"] = r.RampToMax
			e["first_v5_decision_after_transition_s"] = r.FirstDecisionS
			e["first_v5_change_after_transition_s"] = r.FirstChangeS
			e["reaction_latency_ms"] = r.ReactionLatency
			e["actions"] = r.Actions
		}
		out = append(out, e)
	}
	return out
}

func rateHistogram(events []server.RequestEvent) map[string]int {
	m := map[string]int{}
	for _, e := range events {
		m[fmt.Sprintf("%d", e.Rate)]++
	}
	return m
}

func asymmetryReport(frozenCommand string, results []RunResult) map[string]any {
	aria2InFlight := []float64{}
	aria2Ranges := []int{}
	aria2NonRange := []int{}
	v5MaxC := []float64{}
	v5TwC := []float64{}
	v5Changes := []float64{}
	for _, r := range results {
		if !r.Validation.Valid {
			continue
		}
		if r.System == sysAria2 {
			aria2InFlight = append(aria2InFlight, float64(r.MaxInFlight))
			aria2Ranges = append(aria2Ranges, r.RangeCount)
			aria2NonRange = append(aria2NonRange, r.NonRangeReqs)
		} else {
			v5MaxC = append(v5MaxC, float64(r.MaxConcurrency))
			v5TwC = append(v5TwC, r.TimeWeightedConcurrency)
			v5Changes = append(v5Changes, float64(r.EffectiveConcChanges))
		}
	}
	return map[string]any{
		"v5_max_concurrency":                4,
		"aria2_max_configured_connections":  16,
		"aria2_baseline_selection_reason":   "candidate F was selected by the v2 calibration because with --min-split-size=1048576 only >=16 connections yield the intended ~16 x 1 MiB range split at the 16 MiB evaluation scale (candidates A-E yielded larger split sizes)",
		"server_pacing":                     "the controlled server paces each response independently at the selected rate, so additional concurrent responses increase aggregate throughput",
		"consequence":                       "aria2-F can exploit up to 16 independently paced responses while v5 can exploit at most 4; this compares two complete frozen configurations and is NOT a capability-matched comparison; the asymmetry is not normalized away",
		"aria2_frozen_command":              frozenCommand,
		"observed_aria2_max_in_flight_mean": mean(aria2InFlight),
		"observed_aria2_max_in_flight_max":  maxFloat(aria2InFlight),
		"observed_aria2_range_count_mean":   meanInt(aria2Ranges),
		"observed_aria2_non_range_mean":     meanInt(aria2NonRange),
		"observed_v5_max_concurrency_mean":  mean(v5MaxC),
		"observed_v5_time_weighted_mean":    mean(v5TwC),
		"observed_v5_changes_mean":          mean(v5Changes),
	}
}

func artifactHashes(dir string) map[string]string {
	out := map[string]string{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "artifact_hashes.json" {
			continue
		}
		if h, err := sha256File(filepath.Join(dir, name)); err == nil {
			out[name] = h
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Small utilities
// ---------------------------------------------------------------------------

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

// aria2ArgsFromCommand turns the frozen baseline command string into argv: it
// drops the leading "aria2c" and the trailing "<url>" placeholder and keeps
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

func aria2VersionString() (string, error) {
	out, err := exec.Command("aria2c", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("aria2c --version: %w", err)
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	return strings.TrimSpace(line), nil
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

func dur(seconds float64) time.Duration { return time.Duration(seconds * float64(time.Second)) }

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func meanInt(v []int) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += float64(x)
	}
	return s / float64(len(v))
}

func maxFloat(v []float64) float64 {
	m := 0.0
	for _, x := range v {
		if x > m {
			m = x
		}
	}
	return m
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

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "final-evaluation-v5:", err)
	os.Exit(1)
}
