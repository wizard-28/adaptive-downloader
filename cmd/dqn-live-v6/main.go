// Command dqn-live-v6 validates the Candidate v6 DQN on the REAL controlled
// downloader/server under the pre-registered dynamic trajectories, beside fixed
// concurrency C=1..16. No aria2 comparison; no performance claim.
//
// Each condition has three wall-clock phases. Server-side pacing is changed with
// SetRate at phase boundaries; shared bandwidth/latency/loss are changed by
// replacing the tc/netem root qdisc on "lo" (a genuinely shared interface
// bottleneck). The scheduler runs phase 0 before the download and phases 1..n-1
// at the pre-registered wall-clock offsets, stopping when the download finishes.
//
// Authoritative controller state is used for concurrency (after-value of action
// k = ConcurrencyBefore of decision k+1, or the final target).
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
	fileSize       = int64(16 << 20)
	fixedChunkSize = int64(1 << 20)
	minConcurrency = 1
	maxConcurrency = 16
	maxRetries     = 3
	runTimeout     = 30 * time.Second
	policyWindow   = 4
	expectedSHA    = "daa7176793093bea102965700774609fd70d4bbb60523d2417302e2a7f0d9887"
	phaseSeconds   = 2.0
)

// PhaseSpec is one wall-clock phase: server per-response rate, shared interface
// bandwidth (bytes/s), latency (ms), packet loss (%).
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

// Conditions A-E mirror the held-out simulator test conditions.
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

type PolicySpec struct {
	Name  string
	Fixed int // 0 = v6
}

func policies() []PolicySpec {
	out := []PolicySpec{{Name: "v6"}}
	for c := 1; c <= 16; c++ {
		out = append(out, PolicySpec{Name: fmt.Sprintf("fixed-%d", c), Fixed: c})
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
	RunID          string        `json:"run_id"`
	Condition      string        `json:"condition"`
	Policy         string        `json:"policy"`
	Rep            int           `json:"rep"`
	CompletionMs   float64       `json:"completion_ms"`
	WallclockBps   float64       `json:"wallclock_bps"`
	Valid          bool          `json:"valid"`
	OutputSize     int64         `json:"output_size"`
	OutputSHA256   string        `json:"output_sha256"`
	NetemStatus    string        `json:"netem_status"`
	TotalRequests  int64         `json:"total_requests"`
	SuccessfulReqs int64         `json:"successful_requests"`
	FailedRequests int64         `json:"failed_requests"`
	Retries        int64         `json:"retries"`
	SuccessBytes   int64         `json:"success_bytes"`
	MaxInFlight    int64         `json:"max_in_flight"`
	InitialConc    int           `json:"initial_concurrency"`
	FinalConc      int           `json:"final_concurrency"`
	MaxConc        int           `json:"max_concurrency"`
	TwConc         float64       `json:"time_weighted_concurrency"`
	ConcChanges    int           `json:"effective_concurrency_changes"`
	Actions        []ActionEvent `json:"actions,omitempty"`
	PhaseEvents    []PhaseEvent  `json:"phase_events"`
	PhaseMeanConc  []float64     `json:"phase_mean_concurrency"` // time-weighted per phase
	PhaseChanges   []int         `json:"phase_change_counts"`
	ReactionMs     []float64     `json:"transition_reaction_ms"` // -1 when no change
	ConstantRun    bool          `json:"constant_run"`
	RampToMax      bool          `json:"ramp_to_max"`
	Err            string        `json:"err"`
}

type aggregate struct {
	N          int     `json:"n"`
	ValidN     int     `json:"valid_n"`
	MeanMs     float64 `json:"mean_ms"`
	MedianMs   float64 `json:"median_ms"`
	SdMs       float64 `json:"sd_ms"`
	MeanReqs   float64 `json:"mean_requests"`
	MeanRetry  float64 `json:"mean_retries"`
	MeanFinalC float64 `json:"mean_final_concurrency"`
	MeanTwC    float64 `json:"mean_time_weighted_concurrency"`
}

func summarize(runs []RunResult) aggregate {
	var comp, reqs, retry, fc, tw []float64
	for _, r := range runs {
		if !r.Valid {
			continue
		}
		comp = append(comp, r.CompletionMs)
		reqs = append(reqs, float64(r.TotalRequests))
		retry = append(retry, float64(r.Retries))
		fc = append(fc, float64(r.FinalConc))
		tw = append(tw, r.TwConc)
	}
	a := aggregate{N: len(runs), ValidN: len(comp)}
	if len(comp) == 0 {
		return a
	}
	a.MeanMs = mean(comp)
	a.MedianMs = median(comp)
	a.SdMs = pstdev(comp)
	a.MeanReqs = mean(reqs)
	a.MeanRetry = mean(retry)
	a.MeanFinalC = mean(fc)
	a.MeanTwC = mean(tw)
	return a
}

func main() {
	modelPath := flag.String("model", filepath.Join("testdata", "diagnostics", "dqn_validation_v6", "final_dqn_candidate_v6.json"), "frozen Candidate v6 model")
	out := flag.String("out", filepath.Join("testdata", "diagnostics", "dqn_validation_v6_live"), "output directory")
	reps := flag.Int("reps", 3, "repetitions per condition x policy")
	condFlag := flag.String("conditions", "", "comma-separated condition subset")
	polFlag := flag.String("policies", "", "comma-separated policy subset")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	orderSeed := flag.Int64("order-seed", 20260925, "deterministic order seed")
	flag.Parse()

	if got, err := sha256File(*modelPath); err != nil || got != expectedSHA {
		fatal(fmt.Errorf("v6 model hash mismatch: got %s err %v", got, err))
	}
	model, err := dqn.LoadModel(*modelPath)
	if err != nil {
		fatal(err)
	}
	if model.StateSpec.Version != dqn.StateSpecVersionV6 || model.NumActions != dqn.NumConcurrencyActions {
		fatal(fmt.Errorf("model is not a v6 concurrency model (version=%d actions=%d)", model.StateSpec.Version, model.NumActions))
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	conds := filterConditions(conditions(), *condFlag)
	pols := filterPolicies(policies(), *polFlag)
	if len(conds) == 0 || len(pols) == 0 || *reps < 1 {
		fatal(fmt.Errorf("empty matrix"))
	}
	type task struct {
		cond Condition
		pol  PolicySpec
		rep  int
	}
	var order []task
	for _, c := range conds {
		for _, p := range pols {
			for r := 0; r < *reps; r++ {
				order = append(order, task{c, p, r})
			}
		}
	}
	// deterministic shuffle
	rng := newRand(*orderSeed)
	for i := len(order) - 1; i > 0; i-- {
		j := rng(i + 1)
		order[i], order[j] = order[j], order[i]
	}

	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}
	content := contentPattern(fileSize)
	expectedContentSHA := sha256Bytes(content)

	manifest := map[string]any{
		"label":        "Candidate v6 live transfer validation (no aria2 comparison)",
		"model_sha256": expectedSHA,
		"file_size":    fileSize, "chunk_size": fixedChunkSize,
		"concurrency_bounds": []int{minConcurrency, maxConcurrency},
		"phase_seconds":      phaseSeconds, "reps": *reps,
		"conditions": conditions(), "policies": policies(),
		"order_seed":  *orderSeed,
		"gate_d_rule": "v6 live constant fraction < 0.9 AND ramp-to-max fraction < 0.9 AND >=2/5 conditions with phase-to-phase time-weighted concurrency spread >= 2.0",
		"note":        "shared bandwidth/latency/loss via tc/netem on lo (mid-run qdisc replace); server rate per phase; authoritative controller concurrency",
	}
	writeJSON(filepath.Join(*out, "manifest_v6_live.json"), manifest)

	var results []RunResult
	total := len(order)
	for i, t := range order {
		rr := runOne(i, t.cond, t.pol, t.rep, model, content, expectedContentSHA, nc)
		results = append(results, rr)
		fmt.Printf("[%3d/%3d] %-3s %-8s rep=%d valid=%-5v %8.1fms C%d->C%d changes=%d netem=%s\n",
			i+1, total, t.cond.ID, t.pol.Name, t.rep, rr.Valid, rr.CompletionMs,
			rr.InitialConc, rr.FinalConc, rr.ConcChanges, rr.NetemStatus)
	}
	writeJSON(filepath.Join(*out, "live_runs_v6.json"), results)

	// aggregates per condition x policy
	byCond := map[string]map[string]aggregate{}
	for _, c := range conds {
		byCond[c.ID] = map[string]aggregate{}
		for _, p := range pols {
			var sub []RunResult
			for _, r := range results {
				if r.Condition == c.ID && r.Policy == p.Name {
					sub = append(sub, r)
				}
			}
			byCond[c.ID][p.Name] = summarize(sub)
		}
	}
	writeJSON(filepath.Join(*out, "live_aggregate_v6.json"), byCond)

	// adaptation diagnostics + GATE D
	v6 := []RunResult{}
	for _, r := range results {
		if r.Policy == "v6" && r.Valid {
			v6 = append(v6, r)
		}
	}
	constant, ramp, resp := 0, 0, 0
	adapt := map[string]any{}
	perCond := map[string]any{}
	for _, c := range conds {
		var sub []RunResult
		for _, r := range v6 {
			if r.Condition == c.ID {
				sub = append(sub, r)
			}
		}
		if len(sub) == 0 {
			continue
		}
		var mc, changes, reactions []float64
		for _, r := range sub {
			if r.ConstantRun {
				constant++
			}
			if r.RampToMax {
				ramp++
			}
			for _, x := range r.PhaseMeanConc {
				mc = append(mc, x)
			}
			for _, x := range r.ReactionMs {
				reactions = append(reactions, x)
			}
			changes = append(changes, float64(r.ConcChanges))
		}
		// phase spread = mean over runs of (max phase meanC - min phase meanC)
		spreadSum := 0.0
		for _, r := range sub {
			vals := []float64{}
			for _, x := range r.PhaseMeanConc {
				if x >= 0 {
					vals = append(vals, x)
				}
			}
			if len(vals) > 0 {
				lo, hi := vals[0], vals[0]
				for _, x := range vals {
					if x < lo {
						lo = x
					}
					if x > hi {
						hi = x
					}
				}
				spreadSum += hi - lo
			}
		}
		spread := spreadSum / float64(len(sub))
		if spread >= 2.0 {
			resp++
		}
		perCond[c.ID] = map[string]any{
			"runs": len(sub), "phase_mean_concurrency": mean(mc),
			"phase_spread": spread, "mean_changes": mean(changes),
			"reactions_ms": reactions,
		}
	}
	n := float64(len(v6))
	gateD := map[string]any{
		"rule":                  "constant fraction < 0.9 AND ramp-to-max fraction < 0.9 AND >=2/5 conditions with phase spread >= 2.0",
		"v6_valid_runs":         len(v6),
		"constant_fraction":     safeDiv(float64(constant), n),
		"ramp_to_max_fraction":  safeDiv(float64(ramp), n),
		"responsive_conditions": resp,
	}
	gateD["passed"] = len(v6) > 0 && safeDiv(float64(constant), n) < 0.9 &&
		safeDiv(float64(ramp), n) < 0.9 && resp >= 2
	adapt["per_condition"] = perCond
	writeJSON(filepath.Join(*out, "live_adaptation_v6.json"), adapt)
	writeJSON(filepath.Join(*out, "gates_v6_live.json"), map[string]any{"GATE_D_live_transfer": gateD})

	lines := []string{
		"Candidate v6 live transfer validation (controlled server; NO aria2 comparison)",
		"authoritative controller concurrency; shared tc/netem bottleneck with mid-run phase changes",
		"",
		fmt.Sprintf("v6 model sha256: %s", expectedSHA),
		fmt.Sprintf("conditions: %s; policies: %d; reps: %d", strings.Join(condIDs(conds), ","), len(pols), *reps),
		fmt.Sprintf("GATE D: %s", passFail(gateD["passed"].(bool))),
		fmt.Sprintf("  constant=%.3f ramp-to-max=%.3f responsive_conditions=%d/5", gateD["constant_fraction"], gateD["ramp_to_max_fraction"], resp),
		"",
		fmt.Sprintf("%-8s %-4s %10s %10s %10s %8s %8s", "policy", "cond", "mean_ms", "med_ms", "sd_ms", "meanC", "finalC"),
	}
	for _, c := range conds {
		for _, p := range pols {
			a := byCond[c.ID][p.Name]
			lines = append(lines, fmt.Sprintf("%-8s %-4s %10.1f %10.1f %10.1f %8.2f %8.2f",
				p.Name, c.ID, a.MeanMs, a.MedianMs, a.SdMs, a.MeanTwC, a.MeanFinalC))
		}
	}
	lines = append(lines, "", "v6 per-condition phase behavior:")
	for _, c := range conds {
		m := perCond[c.ID].(map[string]any)
		lines = append(lines, fmt.Sprintf("  %-3s phase_spread=%.2f mean_changes=%.2f reactions_ms=%v",
			c.ID, m["phase_spread"], m["mean_changes"], m["reactions_ms"]))
	}
	lines = append(lines, "", "Caveats: controlled synthetic server; controlled unstable profiles; no real-network claim.")
	writeText(filepath.Join(*out, "report_v6_live.txt"), strings.Join(lines, "\n")+"\n")
	fmt.Printf("GATE D %s; wrote %s\n", passFail(gateD["passed"].(bool)), *out)
}

func runOne(orderIdx int, cond Condition, pol PolicySpec, rep int, model *dqn.Model, content []byte, expectedContentSHA string, nc *network.Controller) RunResult {
	rr := RunResult{RunID: fmt.Sprintf("%s__%s__rep%d", cond.ID, pol.Name, rep),
		Condition: cond.ID, Policy: pol.Name, Rep: rep, NetemStatus: "none"}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRequestLogEnabled(true)
	srv.SetRate(cond.Phase[0].Rate)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Phase 0 netem applied before the run.
	if err := applyNetPhase(nc, cond.Phase[0]); err != nil {
		rr.NetemStatus = "unavailable"
		rr.Err = "netem phase0: " + err.Error()
		return rr
	}
	if cond.Phase[0].Link > 0 || cond.Phase[0].LatencyMs > 0 || cond.Phase[0].LossPercent > 0 {
		rr.NetemStatus = "applied"
	}
	defer func() { _ = nc.Clear("lo") }()

	dir, err := os.MkdirTemp("", "dqn-live-v6-")
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	cfg := downloader.Config{
		URL: ts.URL, OutputPath: outPath, TotalSize: fileSize,
		ChunkSize: fixedChunkSize, Timeout: runTimeout, MaxRetries: maxRetries,
		PolicyWindowSize: policyWindow,
	}
	var tp *timedPolicy
	if pol.Fixed > 0 {
		cfg.Concurrency = pol.Fixed
		cfg.MinConcurrency = pol.Fixed
		cfg.MaxConcurrency = pol.Fixed
	} else {
		lp, err := dqn.NewLiveConcurrencyPolicyV6(model, fileSize)
		if err != nil {
			rr.Err = "v6 policy: " + err.Error()
			return rr
		}
		tp = &timedPolicy{inner: lp}
		cfg.Concurrency = minConcurrency
		cfg.MinConcurrency = minConcurrency
		cfg.MaxConcurrency = maxConcurrency
		cfg.Policy = tp
	}
	dl, err := downloader.New(cfg)
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	rr.InitialConc = dl.Concurrency()
	start := time.Now()
	if tp != nil {
		tp.start = start
	}

	// Schedule phases 1..n-1 at fixed wall-clock offsets; stop at completion.
	done := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	events := []PhaseEvent{}
	for i := 1; i < len(cond.Phase); i++ {
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
				srv.SetRate(cond.Phase[i].Rate)
				err := applyNetPhase(nc, cond.Phase[i])
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

	runErr := dl.Download(context.Background())
	rr.CompletionMs = float64(time.Since(start).Microseconds()) / 1000.0
	close(done)
	wg.Wait()
	rr.PhaseEvents = events
	if rr.CompletionMs > 0 {
		rr.WallclockBps = float64(fileSize) / (rr.CompletionMs / 1000.0)
	}
	rr.FinalConc = dl.Concurrency()
	if runErr != nil {
		rr.Err = runErr.Error()
	}

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
	rr.MaxInFlight, _ = inFlightStats(srv.RequestLog())
	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputSize = int64(len(got))
		rr.OutputSHA256 = sha256Bytes(got)
	}
	if tp != nil {
		if lp, ok := tp.inner.(*dqn.LivePolicy); ok {
			actions := deriveActions(rr.FinalConc, lp.Log(), tp.times, start)
			rr.Actions = actions
			rr.ConcChanges = countChanges(actions)
			rr.MaxConc = maxConc(rr.InitialConc, actions)
			rr.TwConc = weightedMean(concurrencySegments(rr.InitialConc, actions, rr.CompletionMs))
			rr.PhaseMeanConc, rr.PhaseChanges = phaseStats(rr.InitialConc, actions, rr.CompletionMs, len(cond.Phase))
			rr.ReactionMs = reactionLatencies(actions, len(cond.Phase))
			constant, ramp, _ := classify(actions)
			rr.ConstantRun, rr.RampToMax = constant, ramp
		}
	} else {
		rr.MaxConc = pol.Fixed
		rr.TwConc = float64(pol.Fixed)
		rr.PhaseMeanConc = make([]float64, len(cond.Phase))
		rr.PhaseChanges = make([]int, len(cond.Phase))
		for i := range rr.PhaseMeanConc {
			rr.PhaseMeanConc[i] = float64(pol.Fixed)
		}
		rr.ReactionMs = []float64{-1, -1}
	}
	for _, ev := range events {
		if ev.Err != "" {
			rr.NetemStatus = "unavailable"
			rr.Err = "netem phase: " + ev.Err
		}
	}
	rr.Valid = rr.Err == "" && rr.OutputSize == fileSize && rr.OutputSHA256 == expectedContentSHA && rr.NetemStatus != "unavailable"
	return rr
}

// ---- phase / netem helpers ----

func applyNetPhase(nc *network.Controller, p PhaseSpec) error {
	if p.Link == 0 && p.LatencyMs == 0 && p.LossPercent == 0 {
		_ = nc.Clear("lo") // clearing an absent qdisc is fine
		return nil
	}
	cfg := network.Config{BandwidthBps: p.Link * 8, Latency: time.Duration(p.LatencyMs) * time.Millisecond, LossPercent: p.LossPercent}
	if err := cfg.Validate(); err != nil {
		return err
	}
	return nc.Apply("lo", cfg)
}

func phaseStats(initial int, actions []ActionEvent, completionMs float64, nPhases int) ([]float64, []int) {
	means := make([]float64, nPhases)
	changes := make([]int, nPhases)
	for i := 0; i < nPhases; i++ {
		lo := float64(i) * phaseSeconds * 1000.0
		hi := float64(i+1) * phaseSeconds * 1000.0
		if completionMs < hi {
			hi = completionMs
		}
		if hi <= lo {
			means[i] = -1
			continue
		}
		means[i] = windowMean(initial, actions, lo, hi)
		for _, a := range actions {
			if a.Changed && a.TimeSinceStartMs >= lo && a.TimeSinceStartMs < hi {
				changes[i]++
			}
		}
	}
	return means, changes
}

func reactionLatencies(actions []ActionEvent, nPhases int) []float64 {
	out := []float64{}
	for i := 1; i < nPhases; i++ {
		tr := float64(i) * phaseSeconds * 1000.0
		lat := -1.0
		for _, a := range actions {
			if a.TimeSinceStartMs >= tr && a.Changed {
				lat = a.TimeSinceStartMs - tr
				break
			}
		}
		out = append(out, lat)
	}
	return out
}

// ---- shared helpers (same authoritative derivation as the v5 live harness) ----

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
			ConcurrencyAfter: after, Changed: after != d.ConcurrencyBefore,
			RemainingBytes: d.RemainingBytes})
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

func maxConc(initial int, actions []ActionEvent) int {
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

func classify(actions []ActionEvent) (constant, rampToMax, monoDec bool) {
	inc, dec := 0, 0
	for _, a := range actions {
		if a.ConcurrencyAfter > a.ConcurrencyBefore {
			inc++
		} else if a.ConcurrencyAfter < a.ConcurrencyBefore {
			dec++
		}
	}
	constant = inc == 0 && dec == 0
	last := 0
	if len(actions) > 0 {
		last = actions[len(actions)-1].ConcurrencyAfter
	}
	rampToMax = dec == 0 && inc > 0 && last == maxConcurrency
	monoDec = inc == 0 && dec > 0
	return
}

func concurrencySegments(initial int, actions []ActionEvent, completionMs float64) map[int]float64 {
	d := map[int]float64{}
	if completionMs <= 0 {
		return d
	}
	cur, prev := initial, 0.0
	for _, a := range actions {
		t := a.TimeSinceStartMs
		if t < 0 {
			continue
		}
		if t > completionMs {
			t = completionMs
		}
		if t > prev {
			d[cur] += (t - prev) / 1000.0
		}
		if a.ConcurrencyAfter >= 1 && a.ConcurrencyAfter <= maxConcurrency {
			cur = a.ConcurrencyAfter
		}
		prev = t
	}
	if completionMs > prev {
		d[cur] += (completionMs - prev) / 1000.0
	}
	return d
}

// windowMean returns the time-weighted mean concurrency within [lo,hi] ms,
// using the authoritative action timeline (relative to run start).
func windowMean(initial int, actions []ActionEvent, lo, hi float64) float64 {
	cur := initial
	for _, a := range actions {
		if a.TimeSinceStartMs >= 0 && a.TimeSinceStartMs <= lo {
			cur = a.ConcurrencyAfter
		}
	}
	area, prev := 0.0, lo
	for _, a := range actions {
		t := a.TimeSinceStartMs
		if t <= lo || t >= hi {
			continue
		}
		area += float64(cur) * (t - prev)
		cur = a.ConcurrencyAfter
		prev = t
	}
	area += float64(cur) * (hi - prev)
	if hi <= lo {
		return 0
	}
	return area / (hi - lo)
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

// ---- small utilities ----

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

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func pstdev(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := mean(v)
	s := 0.0
	for _, x := range v {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(v)))
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
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

func filterPolicies(all []PolicySpec, csv string) []PolicySpec {
	if csv == "" {
		return all
	}
	want := map[string]bool{}
	for _, s := range strings.Split(csv, ",") {
		want[strings.TrimSpace(s)] = true
	}
	var out []PolicySpec
	for _, p := range all {
		if want[p.Name] {
			out = append(out, p)
		}
	}
	return out
}

func condIDs(cs []Condition) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func passFail(b bool) string {
	if b {
		return "PASS"
	}
	return "FAIL"
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

func writeText(path, text string) {
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		fatal(err)
	}
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

// deterministic shuffle helper (math/rand not needed for a fixed seed sequence)
func newRand(seed int64) func(n int) int {
	s := uint64(seed)*6364136223846793005 + 1442695040888963407
	return func(n int) int {
		s = s*6364136223846793005 + 1442695040888963407
		return int((s >> 33) % uint64(n))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "dqn-live-v6:", err)
	os.Exit(1)
}
