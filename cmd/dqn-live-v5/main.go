// Command dqn-live-v5 runs the Candidate v5 DQN transfer validation against the
// REAL controlled downloader/server (not the simulator), beside fixed
// concurrency C=1..4. It does not compare against aria2 and makes no
// superiority claim.
//
// Design (documented, locked before the run):
//
//	scenarios : baseline, server-throttle-wallclock, latency, loss, combined
//	policies  : fixed-1 .. fixed-4, v5 (frozen candidate, SHA below)
//	matrix    : 5 scenarios x 5 policies x N reps (default 5)
//	file      : 16 MiB; chunk pinned at 1 MiB; concurrency 1..4; retries 3
//
// Adaptation logging uses AUTHORITATIVE runtime state, not the model's own
// requested-action labels: the runner hands the policy the controller's current
// target on every decision, so the applied value after action k is the
// `ConcurrencyBefore` observed at decision k+1 (or the final controller target
// for the last action). The model's dqn.Decision.ConcurrencyAfter field is
// intentionally NOT used for the analysis (it is logged before application).
//
// Pre-specified gates:
//
//	GATE A (technical validity): every required run is present and valid, and
//	  no required cell has unavailable network shaping.
//	GATE B (genuine adaptation, v5 valid runs only): NOT READY if any of
//	  constant-run fraction >= 0.9, ramp-to-max fraction >= 0.9,
//	  monotone-decrease fraction >= 0.9, dominant single-concurrency time
//	  fraction (worst scenario) >= 0.9, or fewer than 2 distinct dominant
//	  actual concurrency values across scenarios.
//	GATE C (reaction observability): for every valid v5 run in a wall-clock
//	  transition scenario, the logs establish the concurrency in effect at the
//	  transition, the first decision after it, whether it changed the
//	  concurrency, and the reaction latency.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
	fileSize       = int64(16 << 20)
	fixedChunkSize = int64(1 << 20)
	minConcurrency = 1
	maxConcurrency = 4
	maxRetries     = 3
	runTimeout     = 30 * time.Second
	policyWindow   = 4

	// expectedModelSHA is the frozen Candidate v5 artifact hash (also present
	// in the .sha256 sidecar; both are checked).
	expectedModelSHA = "3a927a58baa7f9b5f150827ba0bd81defc95adb25e5328eb037f5edf9925da75"
)

// Scenario is one live controlled condition. All rate transitions are
// wall-clock based (server rate schedule), never request-count based.
type Scenario struct {
	Name            string `json:"name"`
	InitialRate     int64  `json:"initial_rate"`
	SubsequentRate  int64  `json:"subsequent_rate"`
	TransitionAfter string `json:"transition_after"` // wall-clock; "" = none
	Network         *network.Config
}

func (s Scenario) transition() time.Duration {
	if s.TransitionAfter == "" {
		return 0
	}
	d, _ := time.ParseDuration(s.TransitionAfter)
	return d
}

func scenarios() []Scenario {
	return []Scenario{
		{Name: "baseline", InitialRate: 4 << 20, SubsequentRate: 4 << 20},
		{
			Name: "server-throttle-wallclock", InitialRate: 512 << 10,
			SubsequentRate: 4 << 20, TransitionAfter: "1s",
		},
		{
			Name: "latency", InitialRate: 4 << 20, SubsequentRate: 4 << 20,
			Network: &network.Config{Latency: 20 * time.Millisecond},
		},
		{
			Name: "loss", InitialRate: 4 << 20, SubsequentRate: 4 << 20,
			Network: &network.Config{LossPercent: 1},
		},
		{
			Name: "combined", InitialRate: 1 << 20, SubsequentRate: 1 << 20,
			Network: &network.Config{
				Latency: 20 * time.Millisecond, LossPercent: 1,
			},
		},
	}
}

// PolicySpec identifies a policy: Fixed > 0 is a fixed-concurrency run with no
// policy; Fixed == 0 is the trained v5 DQN.
type PolicySpec struct {
	Name  string
	Fixed int
}

func policies() []PolicySpec {
	return []PolicySpec{
		{Name: "fixed-1", Fixed: 1},
		{Name: "fixed-2", Fixed: 2},
		{Name: "fixed-3", Fixed: 3},
		{Name: "fixed-4", Fixed: 4},
		{Name: "v5"},
	}
}

// ActionEvent is one authoritative-v5 decision. ConcurrencyAfter is derived
// from the next observed controller target (or the final target), not from the
// model's pre-application field.
type ActionEvent struct {
	DecisionIndex            int     `json:"decision_index"`
	TimeSinceScheduleStartMs float64 `json:"time_since_schedule_start_ms"`
	RequestedActionIndex     int     `json:"requested_action_index"`
	RequestedAction          string  `json:"requested_action"`
	ConcurrencyBefore        int     `json:"concurrency_before"`
	ConcurrencyAfter         int     `json:"concurrency_after"`
	Changed                  bool    `json:"changed"`
	Effective                bool    `json:"effective"`
	NoOp                     bool    `json:"no_op"`
	RemainingBytes           int64   `json:"remaining_bytes"`
}

// ScheduleRecord captures the applied wall-clock rate schedule.
type ScheduleRecord struct {
	InitialRate       int64   `json:"initial_rate"`
	SubsequentRate    int64   `json:"subsequent_rate"`
	TransitionAfterMs float64 `json:"transition_after_ms"`
	StartUnixNano     int64   `json:"start_unix_nano"`
}

// Validation records the per-run correctness checks.
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
	RetryRecordsConsistent bool     `json:"retry_records_consistent"`
	Valid                  bool     `json:"valid"`
	Notes                  []string `json:"notes,omitempty"`
}

type RunResult struct {
	RunID              string                `json:"run_id"`
	OrderIndex         int                   `json:"order_index"`
	Scenario           string                `json:"scenario"`
	Policy             string                `json:"policy"`
	Rep                int                   `json:"rep"`
	OrderSeed          int64                 `json:"order_seed"`
	FileSize           int64                 `json:"file_size"`
	ChunkSize          int64                 `json:"chunk_size"`
	RunStartUnixNano   int64                 `json:"run_start_unix_nano"`
	CompletionMs       float64               `json:"completion_ms"`
	WallclockBps       float64               `json:"wallclock_bps"`
	OutputSize         int64                 `json:"output_size"`
	OutputSHA256       string                `json:"output_sha256"`
	ExpectedSHA256     string                `json:"expected_sha256"`
	TotalRequests      int                   `json:"total_requests"`
	SuccessfulRequests int                   `json:"successful_requests"`
	FailedRequests     int                   `json:"failed_requests"`
	Retries            int                   `json:"retries"`
	SuccessBytes       int64                 `json:"success_bytes"`
	InitialConcurrency int                   `json:"initial_concurrency"`
	FinalConcurrency   int                   `json:"final_concurrency"`
	MaxInFlight        int                   `json:"max_in_flight"`
	MeanInFlight       float64               `json:"mean_in_flight"`
	Actions            []ActionEvent         `json:"actions"`
	Decisions          []dqn.Decision        `json:"decisions"`
	Metrics            []metrics.Record      `json:"metrics"`
	ServerRequests     []server.RequestEvent `json:"server_requests"`
	ServerTotalReqs    int64                 `json:"server_total_requests"`
	NetworkStatus      string                `json:"netem_status"`
	Schedule           *ScheduleRecord       `json:"schedule,omitempty"`
	Validation         Validation            `json:"validation"`
	Err                string                `json:"err"`
}

// timedPolicy records the wall-clock time of each SelectAction call so action
// timestamps can be related to the schedule. It adds no behavior.
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

// ---------------------------------------------------------------------------
// Pure analysis helpers (unit-tested)
// ---------------------------------------------------------------------------

// deriveActions builds authoritative action events. For decision k the applied
// concurrency after the action is decisions[k+1].ConcurrencyBefore, because the
// runner reports controller.Current() at every decision and only one action is
// applied between consecutive decisions. The last action's after-value is the
// final controller target.
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
			DecisionIndex:            i,
			TimeSinceScheduleStartMs: ts,
			RequestedActionIndex:     d.ActionIndex,
			RequestedAction:          d.Action,
			ConcurrencyBefore:        d.ConcurrencyBefore,
			ConcurrencyAfter:         after,
			Changed:                  after != d.ConcurrencyBefore,
			Effective:                d.Effective,
			NoOp:                     d.NoOp,
			RemainingBytes:           d.RemainingBytes,
		})
	}
	return out
}

// validateRangeTiling checks that successful records tile [0,size) exactly.
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
		if s.s != prev+1 {
			return false
		}
		if s.e < s.s {
			return false
		}
		prev = s.e
	}
	return prev == size-1
}

// inFlightStats derives actual overlap of server-observed requests: maximum and
// time-weighted mean number of requests in flight.
func inFlightStats(events []server.RequestEvent) (int, float64) {
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
			return pts[i].delta < pts[j].delta // ends before starts at equal times
		}
		return pts[i].t.Before(pts[j].t)
	})
	cur, max := 0, 0
	area := 0.0
	var prev time.Time
	weighted := false
	for _, p := range pts {
		if weighted {
			area += float64(cur) * p.t.Sub(prev).Seconds()
		}
		cur += p.delta
		if cur > max {
			max = cur
		}
		prev = p.t
		weighted = true
	}
	span := pts[len(pts)-1].t.Sub(pts[0].t).Seconds()
	if span <= 0 {
		return max, float64(max)
	}
	return max, area / span
}

// checkProgress verifies the model's reported remaining bytes against the
// successful bytes actually recorded, in order (the runner observes one
// completed attempt per decision).
func checkProgress(decisions []dqn.Decision, records []metrics.Record, size int64) (monotone, within, matches, consistent bool) {
	monotone, within, matches, consistent = true, true, true, true
	if len(decisions) != len(records) {
		matches = false
	}
	var cum int64
	prevRemaining := size
	for i, d := range decisions {
		if d.RemainingBytes < 0 || d.RemainingBytes > size {
			within = false
		}
		if d.RemainingBytes > prevRemaining {
			monotone = false
		}
		prevRemaining = d.RemainingBytes
		if i < len(records) {
			if records[i].Success {
				cum += records[i].Bytes
			}
			if d.RemainingBytes != size-cum {
				matches = false
			}
		}
	}
	return monotone, within, matches, consistent
}

// concurrencySegments returns a time-weighted concurrency distribution between
// the schedule start (t=0) and completionMs, using the authoritative events.
func concurrencySegments(initial int, actions []ActionEvent, completionMs float64, maxC int) map[int]float64 {
	durations := map[int]float64{}
	if completionMs <= 0 {
		return durations
	}
	cur := initial
	prev := 0.0
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

func dominantConcurrency(segments map[int]float64) (int, float64) {
	total := 0.0
	bestC, best := 0, -1.0
	for c, d := range segments {
		total += d
		if d > best || (d == best && c < bestC) {
			best, bestC = d, c
		}
	}
	if total <= 0 {
		return bestC, 0
	}
	return bestC, best / total
}

// classifyTrajectory classifies the actual (authoritative) trajectory.
func classifyTrajectory(actions []ActionEvent, maxC int) (constant, rampToMax, monotoneDecrease bool) {
	increases, decreases := 0, 0
	last := 0
	for _, a := range actions {
		if a.ConcurrencyAfter != a.ConcurrencyBefore {
			if a.ConcurrencyAfter > a.ConcurrencyBefore {
				increases++
			} else {
				decreases++
			}
		}
		last = a.ConcurrencyAfter
	}
	constant = increases == 0 && decreases == 0
	if len(actions) > 0 {
		last = actions[len(actions)-1].ConcurrencyAfter
	}
	rampToMax = decreases == 0 && increases > 0 && last == maxC
	monotoneDecrease = increases == 0 && decreases > 0
	return constant, rampToMax, monotoneDecrease
}

// Reaction captures the transition analysis for one run.
type Reaction struct {
	TransitionMs            float64      `json:"transition_ms"`
	AtTransitionConcurrency int          `json:"concurrency_at_transition"`
	FirstDecisionAfter      *ActionEvent `json:"first_decision_after,omitempty"`
	FirstChangeAfter        *ActionEvent `json:"first_change_after,omitempty"`
	Reacted                 bool         `json:"reacted"`
	ReactionLatencyMs       float64      `json:"reaction_latency_ms"` // -1 when no change
	PreChanges              int          `json:"pre_transition_changes"`
	PostChanges             int          `json:"post_transition_changes"`
	PreMeanConcurrency      float64      `json:"pre_transition_mean_concurrency"`
	PostMeanConcurrency     float64      `json:"post_transition_mean_concurrency"`
}

func analyzeReaction(initial int, actions []ActionEvent, completionMs, transitionMs float64, maxC int) Reaction {
	r := Reaction{
		TransitionMs:       transitionMs,
		ReactionLatencyMs:  -1,
		PreMeanConcurrency: float64(initial),
	}
	if transitionMs <= 0 {
		return r
	}
	// Concurrency in effect at the transition, from the authoritative timeline.
	cur := initial
	for _, a := range actions {
		if a.TimeSinceScheduleStartMs >= 0 && a.TimeSinceScheduleStartMs <= transitionMs {
			cur = a.ConcurrencyAfter
		}
	}
	r.AtTransitionConcurrency = cur

	for i := range actions {
		a := &actions[i]
		if a.TimeSinceScheduleStartMs < 0 {
			continue
		}
		if a.TimeSinceScheduleStartMs >= transitionMs {
			if r.FirstDecisionAfter == nil {
				r.FirstDecisionAfter = a
			}
			if a.Changed {
				r.PostChanges++
				if r.FirstChangeAfter == nil {
					r.FirstChangeAfter = a
					r.ReactionLatencyMs = a.TimeSinceScheduleStartMs - transitionMs
				}
			}
		} else if a.Changed {
			r.PreChanges++
		}
	}
	r.Reacted = r.FirstChangeAfter != nil

	// Time-weighted mean concurrency before/after the transition. The post
	// window is rebased to its own zero so timestamps line up with its length.
	preSeg := concurrencySegments(initial, withoutAfter(actions, transitionMs), transitionMs, maxC)
	postActions := afterOnly(actions, transitionMs)
	rebased := make([]ActionEvent, len(postActions))
	copy(rebased, postActions)
	for i := range rebased {
		rebased[i].TimeSinceScheduleStartMs -= transitionMs
	}
	postSeg := concurrencySegments(cur, rebased, completionMs-transitionMs, maxC)
	r.PreMeanConcurrency = weightedMean(preSeg)
	r.PostMeanConcurrency = weightedMean(postSeg)
	return r
}

func withoutAfter(actions []ActionEvent, t float64) []ActionEvent {
	out := make([]ActionEvent, 0, len(actions))
	for _, a := range actions {
		if a.TimeSinceScheduleStartMs < t {
			out = append(out, a)
		}
	}
	return out
}

func afterOnly(actions []ActionEvent, t float64) []ActionEvent {
	out := make([]ActionEvent, 0, len(actions))
	for _, a := range actions {
		if a.TimeSinceScheduleStartMs >= t {
			out = append(out, a)
		}
	}
	return out
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

// ---------------------------------------------------------------------------
// Aggregation
// ---------------------------------------------------------------------------

type Distribution struct {
	N      int     `json:"n"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	SD     float64 `json:"sd"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

func summarize(values []float64) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}
	d := Distribution{N: len(values), Mean: mean(values), Min: math.Inf(1), Max: math.Inf(-1)}
	for _, v := range values {
		if v < d.Min {
			d.Min = v
		}
		if v > d.Max {
			d.Max = v
		}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	d.Median = medianSorted(sorted)
	d.SD = populationSD(values)
	return d
}

func mean(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func medianSorted(v []float64) float64 {
	n := len(v)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

func populationSD(v []float64) float64 {
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

type PolicyStats struct {
	N                             int               `json:"n"`
	ValidN                        int               `json:"valid_n"`
	InvalidN                      int               `json:"invalid_n"`
	CompletionMs                  Distribution      `json:"completion_ms"`
	WallclockBps                  Distribution      `json:"wallclock_bps"`
	MeanRequests                  float64           `json:"mean_requests"`
	MeanSuccessful                float64           `json:"mean_successful_requests"`
	MeanFailed                    float64           `json:"mean_failed_requests"`
	MeanRetries                   float64           `json:"mean_retries"`
	MeanSuccessBytes              float64           `json:"mean_success_bytes"`
	MeanFinalConcurrency          float64           `json:"mean_final_concurrency"`
	MeanTimeWeightedConcurrency   float64           `json:"mean_time_weighted_concurrency"`
	MeanMaxInFlight               float64           `json:"mean_max_in_flight"`
	MeanEffectiveChanges          float64           `json:"mean_effective_concurrency_changes"`
	ConstantFraction              float64           `json:"constant_run_fraction"`
	RampToMaxFraction             float64           `json:"ramp_to_max_fraction"`
	MonotoneDecreaseFraction      float64           `json:"monotone_decrease_fraction"`
	DominantConcurrencyWorstFrac  float64           `json:"dominant_concurrency_worst_fraction"`
	DominantConcurrencyByScenario map[string]string `json:"dominant_concurrency_by_scenario,omitempty"`
}

func statsFor(runs []RunResult, maxC int) PolicyStats {
	var ps PolicyStats
	ps.N = len(runs)
	if len(runs) == 0 {
		return ps
	}
	var comp, bps []float64
	var reqs, ok, fail, retries, bytes, finalC, twc, inFlight, changes float64
	constant, ramp, mono := 0, 0, 0
	worstDominant := 0.0
	ps.DominantConcurrencyByScenario = map[string]string{}
	dominantByScenario := map[string]map[int]float64{}
	for _, r := range runs {
		if !r.Validation.Valid {
			ps.InvalidN++
			continue
		}
		ps.ValidN++
		comp = append(comp, r.CompletionMs)
		bps = append(bps, r.WallclockBps)
		reqs += float64(r.TotalRequests)
		ok += float64(r.SuccessfulRequests)
		fail += float64(r.FailedRequests)
		retries += float64(r.Retries)
		bytes += float64(r.SuccessBytes)
		finalC += float64(r.FinalConcurrency)
		inFlight += float64(r.MaxInFlight)
		c, rm, md := classifyTrajectory(r.Actions, maxC)
		if c {
			constant++
		}
		if rm {
			ramp++
		}
		if md {
			mono++
		}
		changes += float64(countChanges(r.Actions))
		seg := concurrencySegments(r.InitialConcurrency, r.Actions, r.CompletionMs, maxC)
		_, frac := dominantConcurrency(seg)
		if frac > worstDominant {
			worstDominant = frac
		}
		if dominantByScenario[r.Scenario] == nil {
			dominantByScenario[r.Scenario] = map[int]float64{}
		}
		for k, v := range seg {
			dominantByScenario[r.Scenario][k] += v
		}
		twc += weightedMean(seg)
	}
	ps.CompletionMs = summarize(comp)
	ps.WallclockBps = summarize(bps)
	if ps.ValidN > 0 {
		n := float64(ps.ValidN)
		ps.MeanRequests = reqs / n
		ps.MeanSuccessful = ok / n
		ps.MeanFailed = fail / n
		ps.MeanRetries = retries / n
		ps.MeanSuccessBytes = bytes / n
		ps.MeanFinalConcurrency = finalC / n
		ps.MeanTimeWeightedConcurrency = twc / n
		ps.MeanMaxInFlight = inFlight / n
		ps.MeanEffectiveChanges = changes / n
		ps.ConstantFraction = float64(constant) / n
		ps.RampToMaxFraction = float64(ramp) / n
		ps.MonotoneDecreaseFraction = float64(mono) / n
		ps.DominantConcurrencyWorstFrac = worstDominant
	}
	for sc, segs := range dominantByScenario {
		dc, _ := dominantConcurrency(segs)
		ps.DominantConcurrencyByScenario[sc] = fmt.Sprintf("C%d", dc)
	}
	return ps
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

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	modelPath := flag.String("model", filepath.Join("testdata", "diagnostics", "dqn_validation_v5", "final_dqn_candidate_v5.json"), "frozen Candidate v5 model JSON")
	shaPath := flag.String("sha", filepath.Join("testdata", "diagnostics", "dqn_validation_v5", "final_dqn_candidate_v5.sha256"), "expected model SHA-256 file")
	out := flag.String("out", filepath.Join("testdata", "diagnostics", "dqn_validation_v5_live"), "output directory")
	reps := flag.Int("reps", 5, "repetitions per scenario x policy cell")
	scenarioFlag := flag.String("scenarios", "", "comma-separated scenario subset (default all)")
	policyFlag := flag.String("policies", "", "comma-separated policy subset (default all)")
	orderSeed := flag.Int64("order-seed", 20260925, "deterministic run-order seed")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}

	model, err := dqn.LoadModel(*modelPath)
	if err != nil {
		fatal(fmt.Errorf("load model: %w", err))
	}
	actualSHA, err := sha256File(*modelPath)
	if err != nil {
		fatal(err)
	}
	expectedSHA := strings.TrimSpace(firstField(*shaPath))
	if expectedSHA == "" {
		fatal(fmt.Errorf("empty sha file %s", *shaPath))
	}
	if actualSHA != expectedSHA {
		fatal(fmt.Errorf("model SHA mismatch: sidecar %s, actual %s", expectedSHA, actualSHA))
	}
	if actualSHA != expectedModelSHA {
		fatal(fmt.Errorf("model SHA is not the frozen Candidate v5 hash: got %s want %s", actualSHA, expectedModelSHA))
	}
	if model.StateSpec.Version != dqn.StateSpecVersionV5 || model.NumActions != dqn.NumConcurrencyActions {
		fatal(fmt.Errorf("model is not a v5 concurrency model (version=%d actions=%d)", model.StateSpec.Version, model.NumActions))
	}

	selectedScenarios := filterScenarios(scenarios(), *scenarioFlag)
	selectedPolicies := filterPolicies(policies(), *policyFlag)
	if len(selectedScenarios) == 0 || len(selectedPolicies) == 0 || *reps < 1 {
		fatal(fmt.Errorf("empty run matrix: scenarios=%d policies=%d reps=%d", len(selectedScenarios), len(selectedPolicies), *reps))
	}
	expectedRuns := len(selectedScenarios) * len(selectedPolicies) * *reps

	content := contentPattern(fileSize)
	expectedContentSHA := sha256Bytes(content)

	var order []runTask
	for _, sc := range selectedScenarios {
		for _, ps := range selectedPolicies {
			for rep := 0; rep < *reps; rep++ {
				order = append(order, runTask{sc, ps, rep})
			}
		}
	}
	rng := rand.New(rand.NewSource(*orderSeed))
	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })

	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}

	// Experiment manifest is written before any run.
	manifest := map[string]any{
		"label":                 "Candidate v5 live transfer validation (simulator-trained; no aria2 comparison)",
		"model_path":            *modelPath,
		"model_sha256":          actualSHA,
		"model_sha256_expected": expectedModelSHA,
		"state_spec_version":    model.StateSpec.Version,
		"num_actions":           model.NumActions,
		"action_names":          model.ActionNames,
		"file_size":             fileSize,
		"chunk_size":            fixedChunkSize,
		"concurrency_bounds":    []int{minConcurrency, maxConcurrency},
		"max_retries":           maxRetries,
		"timeout":               runTimeout.String(),
		"reps":                  *reps,
		"expected_runs":         expectedRuns,
		"scenarios":             scenarioConfigJSON(selectedScenarios),
		"policies":              policyConfigJSON(selectedPolicies),
		"order_seed":            *orderSeed,
		"run_order":             runOrderJSON(order),
		"sudo":                  *useSudo,
		"go_version":            runtime.Version(),
		"started_utc":           time.Now().UTC().Format(time.RFC3339Nano),
		"gates": map[string]string{
			"A": "every required run present and valid; no required cell has unavailable netem",
			"B": "NOT READY if constant fraction >= 0.9, ramp-to-max fraction >= 0.9, monotone-decrease fraction >= 0.9, worst-scenario dominant single-concurrency time fraction >= 0.9, or < 2 distinct dominant actual concurrency values across scenarios",
			"C": "for every valid v5 run in a wall-clock transition scenario the logs establish transition concurrency, first subsequent decision, whether concurrency changed, and reaction latency",
		},
	}
	writeJSON(filepath.Join(*out, "live_manifest_v5.json"), manifest)

	fmt.Printf("dqn-live-v5: %d runs (model %s)\n", expectedRuns, actualSHA[:12])

	runs := make([]RunResult, 0, expectedRuns)
	for i, t := range order {
		rr := runOne(i, t.sc, t.ps, t.rep, model, expectedContentSHA, content, nc, *orderSeed)
		runs = append(runs, rr)
		fmt.Printf("[%3d/%3d] %-26s %-8s rep=%d valid=%-5v %8.1fms C%d->C%d changes=%d netem=%s\n",
			i+1, expectedRuns, t.sc.Name, t.ps.Name, t.rep, rr.Validation.Valid,
			rr.CompletionMs, rr.InitialConcurrency, rr.FinalConcurrency,
			countChanges(rr.Actions), rr.NetworkStatus)
	}

	writeJSON(filepath.Join(*out, "live_runs_v5.json"), runs)
	rawSHA := sha256FileMust(filepath.Join(*out, "live_runs_v5.json"))
	_ = os.WriteFile(filepath.Join(*out, "live_runs_v5.sha256"), []byte(rawSHA+"\n"), 0o644)

	valid, invalid := 0, 0
	netemUnavailable := 0
	exclusions := []map[string]string{}
	for _, r := range runs {
		if r.Validation.Valid {
			valid++
		} else {
			invalid++
			exclusions = append(exclusions, map[string]string{
				"run_id": r.RunID, "scenario": r.Scenario, "policy": r.Policy,
				"rep": fmt.Sprint(r.Rep), "reason": r.Validation.Notes[0],
			})
		}
		if r.NetworkStatus == "unavailable" {
			netemUnavailable++
		}
	}

	// Aggregation by scenario x policy, plus paired per-rep observations.
	aggregate := map[string]any{}
	var orderedScenarios, orderedPolicies []string
	for _, sc := range selectedScenarios {
		orderedScenarios = append(orderedScenarios, sc.Name)
	}
	for _, ps := range selectedPolicies {
		orderedPolicies = append(orderedPolicies, ps.Name)
	}
	byScenario := map[string]any{}
	for _, scName := range orderedScenarios {
		byPolicy := map[string]any{}
		for _, psName := range orderedPolicies {
			subset := filterRuns(runs, scName, psName)
			byPolicy[psName] = statsFor(subset, maxConcurrency)
		}
		pairs := map[string]map[string]float64{}
		for rep := 0; rep < *reps; rep++ {
			cell := map[string]float64{}
			for _, psName := range orderedPolicies {
				for _, r := range runs {
					if r.Scenario == scName && r.Policy == psName && r.Rep == rep && r.Validation.Valid {
						cell[psName] = r.CompletionMs
					}
				}
			}
			if len(cell) > 0 {
				pairs[fmt.Sprintf("rep-%d", rep)] = cell
			}
		}
		byScenario[scName] = map[string]any{
			"policies": byPolicy,
			"pairs":    pairs,
		}
	}
	aggregate["by_scenario"] = byScenario
	aggregate["scenarios"] = orderedScenarios
	aggregate["policies"] = orderedPolicies
	aggregate["reps"] = *reps
	writeJSON(filepath.Join(*out, "live_aggregate_v5.json"), aggregate)

	// Adaptation / reaction diagnostics (v5 runs only).
	adaptation := buildAdaptation(runs, selectedScenarios, *reps)
	writeJSON(filepath.Join(*out, "live_adaptation_v5.json"), adaptation)

	// Gates.
	v5Valid := filterPolicy(runs, "v5", true)
	transitionScenario := ""
	for _, sc := range selectedScenarios {
		if sc.transition() > 0 {
			transitionScenario = sc.Name
			break
		}
	}
	gates := evaluateGates(runs, v5Valid, expectedRuns, valid, invalid, netemUnavailable, transitionScenario, adaptation)
	writeJSON(filepath.Join(*out, "live_gates_v5.json"), gates)

	// Validation summary.
	validation := map[string]any{
		"expected_runs":     expectedRuns,
		"runs_recorded":     len(runs),
		"valid_runs":        valid,
		"invalid_runs":      invalid,
		"netem_unavailable": netemUnavailable,
		"exclusions":        exclusions,
		"check_definitions": map[string]string{
			"range_tiling_ok":          "successful ranges tile [0,file_size) exactly (no gaps/overlaps)",
			"success_bytes_ok":         "sum(success bytes) == file_size and each success bytes == range length",
			"progress_matches_records": "model RemainingBytes equals file_size - cumulative successful bytes per attempt",
			"progress_monotone":        "model RemainingBytes never increases",
			"concurrency_bounds_ok":    "authoritative before/after concurrency in [1,4]",
			"actions_in_space":         "requested actions in {none, increase, decrease}",
			"retry_records_consistent": "attempt numbers within [1, max_retries+1]",
		},
	}
	writeJSON(filepath.Join(*out, "live_validation_v5.json"), validation)

	writeReport(filepath.Join(*out, "live_report_v5.txt"), manifest, runs, aggregate, adaptation, gates, validation)

	fmt.Printf("\nvalid=%d/%d invalid=%d netem_unavailable=%d\n", valid, expectedRuns, invalid, netemUnavailable)
	for _, g := range gates {
		fmt.Printf("%s: %s\n", g.Name, passFail(g.Passed))
	}
	fmt.Printf("wrote %s\n", *out)
}

func passFail(b bool) string {
	if b {
		return "PASS"
	}
	return "FAIL"
}

func buildAdaptation(runs []RunResult, scens []Scenario, reps int) map[string]any {
	var v5 []RunResult
	for _, r := range runs {
		if r.Policy == "v5" && r.Validation.Valid {
			v5 = append(v5, r)
		}
	}
	perRun := []map[string]any{}
	reactionByScenario := map[string][]Reaction{}
	for _, r := range v5 {
		var tr time.Duration
		for _, sc := range scens {
			if sc.Name == r.Scenario {
				tr = sc.transition()
			}
		}
		seg := concurrencySegments(r.InitialConcurrency, r.Actions, r.CompletionMs, maxConcurrency)
		dc, frac := dominantConcurrency(seg)
		constant, ramp, mono := classifyTrajectory(r.Actions, maxConcurrency)
		entry := map[string]any{
			"run_id":                         r.RunID,
			"scenario":                       r.Scenario,
			"rep":                            r.Rep,
			"initial_concurrency":            r.InitialConcurrency,
			"final_concurrency":              r.FinalConcurrency,
			"effective_changes":              countChanges(r.Actions),
			"constant_run":                   constant,
			"ramp_to_max":                    ramp,
			"monotone_decrease":              mono,
			"dominant_concurrency":           fmt.Sprintf("C%d", dc),
			"dominant_time_fraction":         frac,
			"mean_concurrency_time_weighted": weightedMean(seg),
			"max_in_flight":                  r.MaxInFlight,
			"actions":                        r.Actions,
		}
		if tr > 0 {
			react := analyzeReaction(r.InitialConcurrency, r.Actions, r.CompletionMs, tr.Seconds()*1000.0, maxConcurrency)
			entry["reaction"] = react
			reactionByScenario[r.Scenario] = append(reactionByScenario[r.Scenario], react)
		}
		perRun = append(perRun, entry)
	}

	transitionSummary := map[string]any{}
	for sc, reactions := range reactionByScenario {
		var latencies, posMeans []float64
		reacted := 0
		for _, r := range reactions {
			if r.Reacted {
				reacted++
				latencies = append(latencies, r.ReactionLatencyMs)
			}
			posMeans = append(posMeans, r.PostMeanConcurrency)
		}
		transitionSummary[sc] = map[string]any{
			"runs":                             len(reactions),
			"runs_with_a_change_after":         reacted,
			"reaction_latency_ms":              summarize(latencies),
			"post_transition_mean_concurrency": summarize(posMeans),
		}
	}
	return map[string]any{
		"per_run":            perRun,
		"transition_summary": transitionSummary,
		"max_concurrency":    maxConcurrency,
		"reps":               reps,
	}
}

type GateResult struct {
	Name   string         `json:"name"`
	Passed bool           `json:"passed"`
	Rule   string         `json:"rule"`
	Detail map[string]any `json:"detail"`
}

func evaluateGates(all []RunResult, v5Valid []RunResult, expectedRuns, valid, invalid, netemUnavailable int, transitionScenario string, adaptation map[string]any) []GateResult {
	gateA := GateResult{
		Name:   "GATE_A_technical_validity",
		Passed: len(all) == expectedRuns && valid == expectedRuns && invalid == 0 && netemUnavailable == 0,
		Rule:   "all required runs recorded and valid; no required cell has unavailable netem",
		Detail: map[string]any{
			"expected_runs": expectedRuns, "recorded_runs": len(all),
			"valid_runs": valid, "invalid_runs": invalid,
			"netem_unavailable": netemUnavailable,
		},
	}

	var constant, ramp, mono float64
	worstDominant := 0.0
	dominantSet := map[string]bool{}
	perScenario := map[string]any{}
	if len(v5Valid) > 0 {
		for _, r := range v5Valid {
			c, rm, md := classifyTrajectory(r.Actions, maxConcurrency)
			if c {
				constant++
			}
			if rm {
				ramp++
			}
			if md {
				mono++
			}
			seg := concurrencySegments(r.InitialConcurrency, r.Actions, r.CompletionMs, maxConcurrency)
			dc, frac := dominantConcurrency(seg)
			key := fmt.Sprintf("C%d", dc)
			dominantSet[key] = true
			if frac > worstDominant {
				worstDominant = frac
			}
			if perScenario[r.Scenario] == nil {
				perScenario[r.Scenario] = map[string]any{"dominant": key, "dominant_fraction": frac, "runs": 0}
			}
			ps := perScenario[r.Scenario].(map[string]any)
			ps["runs"] = ps["runs"].(int) + 1
		}
	}
	n := float64(len(v5Valid))
	gateB := GateResult{
		Name: "GATE_B_genuine_adaptation",
		Passed: n > 0 &&
			constant/n < 0.9 && ramp/n < 0.9 && mono/n < 0.9 &&
			worstDominant < 0.9 && len(dominantSet) >= 2,
		Rule: "v5 must not be constant, ramp-to-max, monotone-decrease, or dominated by a single concurrency (>=0.9 of time in the worst scenario), and must show >=2 distinct dominant actual concurrency values across scenarios; adaptation must be observable in the live logs",
		Detail: map[string]any{
			"v5_valid_runs":                 len(v5Valid),
			"constant_run_fraction":         safeDiv(constant, n),
			"ramp_to_max_fraction":          safeDiv(ramp, n),
			"monotone_decrease_fraction":    safeDiv(mono, n),
			"worst_dominant_time_fraction":  worstDominant,
			"distinct_dominant_concurrency": len(dominantSet),
			"dominant_concurrency_values":   sortedKeys(dominantSet),
			"per_scenario":                  perScenario,
		},
	}

	transitionRuns, complete := 0, 0
	for _, r := range v5Valid {
		if r.Scenario != transitionScenario {
			continue
		}
		transitionRuns++
		if reaction, ok := findReaction(adaptation, r.RunID); ok && reaction.FirstDecisionAfter != nil {
			complete++
		}
	}
	gateC := GateResult{
		Name:   "GATE_C_reaction_observability",
		Passed: transitionRuns > 0 && complete == transitionRuns,
		Rule:   "for every valid v5 run in a wall-clock transition scenario the logs establish transition concurrency, the first subsequent decision, whether concurrency changed, and the reaction latency",
		Detail: map[string]any{
			"transition_scenario":     transitionScenario,
			"transition_runs":         transitionRuns,
			"runs_with_complete_logs": complete,
		},
	}
	return []GateResult{gateA, gateB, gateC}
}

func findReaction(adaptation map[string]any, runID string) (Reaction, bool) {
	perRun, _ := adaptation["per_run"].([]map[string]any)
	for _, e := range perRun {
		if e["run_id"] == runID {
			if r, ok := e["reaction"].(Reaction); ok {
				return r, true
			}
		}
	}
	return Reaction{}, false
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Run execution
// ---------------------------------------------------------------------------

func runOne(orderIdx int, sc Scenario, ps PolicySpec, rep int, model *dqn.Model, expectedContentSHA string, content []byte, nc *network.Controller, orderSeed int64) RunResult {
	runID := fmt.Sprintf("%s__%s__rep%d", sc.Name, ps.Name, rep)
	rr := RunResult{
		RunID: runID, OrderIndex: orderIdx, Scenario: sc.Name, Policy: ps.Name,
		Rep: rep, OrderSeed: orderSeed, FileSize: fileSize, ChunkSize: fixedChunkSize,
		ExpectedSHA256: expectedContentSHA, NetworkStatus: "none",
	}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRequestLogEnabled(true)
	srv.SetRate(sc.InitialRate)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	if sc.Network != nil {
		if err := nc.Apply("lo", *sc.Network); err != nil {
			rr.NetworkStatus = "unavailable"
			rr.Err = "netem apply: " + err.Error()
			rr.Validation = invalidValidation(rr.Err)
			return rr
		}
		defer func() { _ = nc.Clear("lo") }()
		rr.NetworkStatus = "applied"
	}

	dir, err := os.MkdirTemp("", "dqn-live-v5-")
	if err != nil {
		rr.Err = err.Error()
		rr.Validation = invalidValidation(rr.Err)
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
	if ps.Fixed > 0 {
		cfg.Concurrency = ps.Fixed
		cfg.MinConcurrency = ps.Fixed
		cfg.MaxConcurrency = ps.Fixed
		cfg.ChunkSize = fixedChunkSize
	} else {
		lp, err := dqn.NewLiveConcurrencyPolicyV5(model, fileSize)
		if err != nil {
			rr.Err = "v5 policy: " + err.Error()
			rr.Validation = invalidValidation(rr.Err)
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
		rr.Validation = invalidValidation(rr.Err)
		return rr
	}

	rr.InitialConcurrency = dl.Concurrency()
	runStart := time.Now()
	rr.RunStartUnixNano = runStart.UnixNano()
	if tp != nil {
		tp.start = runStart
	}
	if sc.transition() > 0 {
		sched := server.RateSchedule{
			InitialRate:     sc.InitialRate,
			TransitionAfter: sc.transition(),
			SubsequentRate:  sc.SubsequentRate,
		}
		if err := srv.SetRateScheduleAt(runStart, sched); err != nil {
			rr.Err = "schedule: " + err.Error()
			rr.Validation = invalidValidation(rr.Err)
			return rr
		}
		rr.Schedule = &ScheduleRecord{
			InitialRate: sc.InitialRate, SubsequentRate: sc.SubsequentRate,
			TransitionAfterMs: sched.TransitionAfter.Seconds() * 1000.0,
			StartUnixNano:     runStart.UnixNano(),
		}
	}

	runErr := dl.Download(context.Background())
	rr.CompletionMs = float64(time.Since(runStart).Microseconds()) / 1000.0
	if rr.CompletionMs > 0 {
		rr.WallclockBps = float64(fileSize) / (rr.CompletionMs / 1000.0)
	}
	rr.FinalConcurrency = dl.Concurrency()
	if runErr != nil {
		rr.Err = runErr.Error()
	}

	snap := dl.Metrics()
	rr.Metrics = snap.Records
	rr.TotalRequests = len(snap.Records)
	for _, rec := range snap.Records {
		if rec.Success {
			rr.SuccessfulRequests++
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
	rr.ServerTotalReqs = st.TotalRequests
	rr.MaxInFlight, rr.MeanInFlight = inFlightStats(rr.ServerRequests)

	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputSize = int64(len(got))
		sum := sha256.Sum256(got)
		rr.OutputSHA256 = hex.EncodeToString(sum[:])
	}

	if tp != nil {
		if lp, ok := tp.inner.(*dqn.LivePolicy); ok {
			decisions := lp.Log()
			rr.Decisions = decisions
			rr.Actions = deriveActions(rr.FinalConcurrency, decisions, tp.times, runStart)
		}
	}

	rr.Validation = runValidation(rr)
	return rr
}

func runValidation(rr RunResult) Validation {
	v := Validation{
		OutputSizeOK:           rr.OutputSize == fileSize,
		OutputSHAOK:            rr.OutputSHA256 == rr.ExpectedSHA256,
		RangeTilingOK:          validateRangeTiling(rr.Metrics, fileSize),
		ConcurrencyBoundsOK:    true,
		ActionsInSpace:         true,
		RetryRecordsConsistent: true,
	}
	var successBytes int64
	for _, rec := range rr.Metrics {
		if rec.Success {
			successBytes += rec.Bytes
			if rec.Bytes != rec.RangeEnd-rec.RangeStart+1 {
				v.SuccessBytesOK = false
				v.Notes = append(v.Notes, "success record bytes != range length")
			}
		}
		if rec.Attempt < 1 || rec.Attempt > maxRetries+1 {
			v.RetryRecordsConsistent = false
			v.Notes = append(v.Notes, "attempt number out of range")
		}
		if rec.Success && rec.Bytes <= 0 {
			v.SuccessBytesOK = false
			v.Notes = append(v.Notes, "non-positive success bytes")
		}
	}
	v.SuccessBytesOK = successBytes == fileSize

	if len(rr.Decisions) > 0 {
		mono, within, matches, _ := checkProgress(rr.Decisions, rr.Metrics, fileSize)
		v.ProgressMonotone, v.ProgressWithinTotal, v.ProgressMatchesRecords = mono, within, matches
		if !matches {
			v.Notes = append(v.Notes, "model reported progress does not match recorded bytes")
		}
	} else {
		v.ProgressMonotone, v.ProgressWithinTotal, v.ProgressMatchesRecords = true, true, true
	}

	for _, a := range rr.Actions {
		if a.RequestedActionIndex < 0 || a.RequestedActionIndex >= dqn.NumConcurrencyActions {
			v.ActionsInSpace = false
			v.Notes = append(v.Notes, "action outside {none, increase, decrease}")
		}
		if a.ConcurrencyBefore < minConcurrency || a.ConcurrencyBefore > maxConcurrency ||
			a.ConcurrencyAfter < minConcurrency || a.ConcurrencyAfter > maxConcurrency {
			v.ConcurrencyBoundsOK = false
			v.Notes = append(v.Notes, "actual concurrency left [1,4]")
		}
	}
	if rr.FinalConcurrency < minConcurrency || rr.FinalConcurrency > maxConcurrency {
		v.ConcurrencyBoundsOK = false
		v.Notes = append(v.Notes, "final concurrency left [1,4]")
	}
	if rr.Err != "" {
		v.Notes = append(v.Notes, "run error: "+rr.Err)
	}
	if rr.NetworkStatus == "unavailable" {
		v.Notes = append(v.Notes, "network condition unavailable")
	}
	v.Valid = v.OutputSizeOK && v.OutputSHAOK && v.RangeTilingOK && v.SuccessBytesOK &&
		v.ProgressMonotone && v.ProgressWithinTotal && v.ProgressMatchesRecords &&
		v.ConcurrencyBoundsOK && v.ActionsInSpace && v.RetryRecordsConsistent &&
		rr.Err == "" && rr.NetworkStatus != "unavailable"
	if len(v.Notes) == 0 && !v.Valid {
		v.Notes = append(v.Notes, "unknown validation failure")
	}
	return v
}

func invalidValidation(reason string) Validation {
	return Validation{Valid: false, Notes: []string{reason}}
}

// ---------------------------------------------------------------------------
// Report / IO helpers
// ---------------------------------------------------------------------------

func writeReport(path string, manifest map[string]any, runs []RunResult, aggregate, adaptation map[string]any, gates []GateResult, validation map[string]any) {
	var b strings.Builder
	b.WriteString("Candidate v5 live transfer validation (simulator-trained; controlled server)\n")
	b.WriteString("NO aria2 comparison; NO real-network claim; NO superiority claim.\n\n")
	fmt.Fprintf(&b, "model_sha256 : %v\n", manifest["model_sha256"])
	fmt.Fprintf(&b, "file_size    : %d (chunk %d, concurrency %v)\n", fileSize, fixedChunkSize, manifest["concurrency_bounds"])
	fmt.Fprintf(&b, "matrix       : %v scenarios x %v policies x %v reps = %v expected runs\n\n",
		len(manifest["scenarios"].([]map[string]any)), len(manifest["policies"].([]map[string]any)), manifest["reps"], manifest["expected_runs"])

	for _, g := range gates {
		fmt.Fprintf(&b, "%-32s %s\n", g.Name, passFail(g.Passed))
	}
	b.WriteString("\n")

	bs, _ := aggregate["by_scenario"].(map[string]any)
	for _, scName := range aggregate["scenarios"].([]string) {
		fmt.Fprintf(&b, "== %s ==\n", scName)
		sc := bs[scName].(map[string]any)
		pols := sc["policies"].(map[string]any)
		fmt.Fprintf(&b, "%-9s %8s %8s %8s %6s %6s %6s %8s %8s\n",
			"policy", "mean_ms", "med_ms", "sd_ms", "att", "retr", "fail", "finalC", "twMeanC")
		for _, psName := range aggregate["policies"].([]string) {
			ps := pols[psName].(PolicyStats)
			fmt.Fprintf(&b, "%-9s %8.1f %8.1f %8.1f %6.1f %6.1f %6.1f %8.2f %8.2f\n",
				psName, ps.CompletionMs.Mean, ps.CompletionMs.Median, ps.CompletionMs.SD,
				ps.MeanRequests, ps.MeanRetries, ps.MeanFailed,
				ps.MeanFinalConcurrency, ps.MeanTimeWeightedConcurrency)
		}
		b.WriteString("\n")
	}

	b.WriteString("v5 actual concurrency behavior (valid runs):\n")
	perRun := adaptation["per_run"].([]map[string]any)
	scenarioSet := map[string]bool{}
	for _, e := range perRun {
		scenarioSet[e["scenario"].(string)] = true
	}
	for scName := range scenarioSet {
		var changes, constantN, rampN, n int
		var finals []float64
		var doms = map[string]int{}
		for _, e := range perRun {
			if e["scenario"].(string) != scName {
				continue
			}
			n++
			if e["constant_run"].(bool) {
				constantN++
			}
			if e["ramp_to_max"].(bool) {
				rampN++
			}
			changes += e["effective_changes"].(int)
			finals = append(finals, float64(e["final_concurrency"].(int)))
			doms[e["dominant_concurrency"].(string)]++
		}
		fmt.Fprintf(&b, "  %-26s n=%d mean_changes=%.2f constant=%d ramp=%d finalC(mean)=%.2f dominant=%v\n",
			scName, n, float64(changes)/float64(n), constantN, rampN, mean(finals), doms)
	}

	b.WriteString("\ntransition reactions (v5):\n")
	ts, _ := adaptation["transition_summary"].(map[string]any)
	for scName, raw := range ts {
		m := raw.(map[string]any)
		lat := m["reaction_latency_ms"].(Distribution)
		fmt.Fprintf(&b, "  %-26s runs=%v changed_after=%v latency_ms(mean/median)=%.1f/%.1f postMeanC=%.2f\n",
			scName, m["runs"], m["runs_with_a_change_after"], lat.Mean, lat.Median,
			m["post_transition_mean_concurrency"].(Distribution).Mean)
	}

	b.WriteString("\nvalidation:\n")
	fmt.Fprintf(&b, "  expected=%v recorded=%v valid=%v invalid=%v netem_unavailable=%v\n",
		validation["expected_runs"], validation["runs_recorded"], validation["valid_runs"],
		validation["invalid_runs"], validation["netem_unavailable"])
	for _, e := range validation["exclusions"].([]map[string]string) {
		fmt.Fprintf(&b, "  EXCLUDED %s %s/%s rep=%s: %s\n", e["run_id"], e["scenario"], e["policy"], e["rep"], e["reason"])
	}
	b.WriteString("\nCaveats: controlled synthetic server; simulator-trained model; no aria2 comparison; no real-network generalization; performance differences are descriptive, not a ranking.\n")
	writeFileMust(path, b.String())
}

func scenarioConfigJSON(scens []Scenario) []map[string]any {
	out := make([]map[string]any, 0, len(scens))
	for _, s := range scens {
		e := map[string]any{
			"name": s.Name, "initial_rate": s.InitialRate,
			"subsequent_rate": s.SubsequentRate, "transition_after": s.TransitionAfter,
		}
		if s.Network != nil {
			e["network"] = s.Network
		}
		out = append(out, e)
	}
	return out
}

func policyConfigJSON(pols []PolicySpec) []map[string]any {
	out := make([]map[string]any, 0, len(pols))
	for _, p := range pols {
		e := map[string]any{"name": p.Name}
		if p.Fixed > 0 {
			e["kind"] = "fixed"
			e["concurrency"] = p.Fixed
		} else {
			e["kind"] = "trained-v5"
			e["initial_concurrency"] = minConcurrency
			e["bounds"] = []int{minConcurrency, maxConcurrency}
		}
		out = append(out, e)
	}
	return out
}

type runTask struct {
	sc  Scenario
	ps  PolicySpec
	rep int
}

func runOrderJSON(order []runTask) []map[string]any {
	out := make([]map[string]any, 0, len(order))
	for i, t := range order {
		out = append(out, map[string]any{
			"order_index": i, "scenario": t.sc.Name, "policy": t.ps.Name, "rep": t.rep,
		})
	}
	return out
}

func filterScenarios(all []Scenario, csv string) []Scenario {
	if csv == "" {
		return all
	}
	want := map[string]bool{}
	for _, s := range strings.Split(csv, ",") {
		want[strings.TrimSpace(s)] = true
	}
	var out []Scenario
	for _, s := range all {
		if want[s.Name] {
			out = append(out, s)
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

func filterRuns(runs []RunResult, scenario, policy string) []RunResult {
	var out []RunResult
	for _, r := range runs {
		if r.Scenario == scenario && r.Policy == policy {
			out = append(out, r)
		}
	}
	return out
}

func filterPolicy(runs []RunResult, policy string, validOnly bool) []RunResult {
	var out []RunResult
	for _, r := range runs {
		if r.Policy == policy && (!validOnly || r.Validation.Valid) {
			out = append(out, r)
		}
	}
	return out
}

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

func sha256File(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Bytes(b), nil
}

func sha256FileMust(path string) string {
	s, err := sha256File(path)
	if err != nil {
		fatal(err)
	}
	return s
}

func firstField(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func writeJSON(path string, obj any) {
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		fatal(err)
	}
	writeFileMust(path, string(data)+"\n")
}

func writeFileMust(path, text string) {
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "dqn-live-v5:", err)
	os.Exit(1)
}
