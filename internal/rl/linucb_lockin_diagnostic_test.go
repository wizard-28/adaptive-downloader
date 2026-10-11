package rl

// This file is a DIAGNOSTIC, not a behavioral test. It replays the frozen
// benchmark v3 LinUCB trajectories through the REAL LinUCB implementation
// (score/update/SelectAction) to explain the post-warm-up single-arm lock-in.
// It writes artifacts under testdata/diagnostics/linucb_lockin/ and does not
// change production behavior.
//
// Reconstruction note: benchmark v3 records 8 of the 10 raw State fields per
// decision (Concurrency, ChunkSize, Throughput, AggregateThroughput,
// RequestDuration, FailedRequests, Retries, TotalAttempts=RequestIndex). It does
// not record BytesTransferred or SuccessfulRequests. Since failures and retries
// are 0 throughout v3, SuccessfulRequests == RequestIndex. BytesTransferred is
// set to 32768; the bytes feature is clamp(bytes/1000) so any value >= 1000
// yields the same feature (1.0). The replay is validated by checking that the
// re-selected action matches the recorded action for every decision.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"gonum.org/v1/gonum/stat"
)

const lockinWarmup = 5

type v3Matrix struct {
	Scenarios []v3Scenario `json:"Scenarios"`
}
type v3Scenario struct {
	Name   string   `json:"Name"`
	Config v3Config `json:"Config"`
	LinUCB []v3Run  `json:"LinUCB"`
}
type v3Config struct {
	MinConcurrency int   `json:"MinConcurrency"`
	MaxConcurrency int   `json:"MaxConcurrency"`
	MinChunkSize   int64 `json:"MinChunkSize"`
	MaxChunkSize   int64 `json:"MaxChunkSize"`
}
type v3Run struct {
	Repetition       int          `json:"Repetition"`
	FileSize         int64        `json:"FileSize"`
	FinalConcurrency int          `json:"FinalConcurrency"`
	FinalChunkSize   int64        `json:"FinalChunkSize"`
	Decisions        []v3Decision `json:"Decisions"`
}
type v3Decision struct {
	RequestIndex        int64   `json:"RequestIndex"`
	ActionIndex         int     `json:"ActionIndex"`
	ActionName          string  `json:"ActionName"`
	Reward              float64 `json:"Reward"`
	Concurrency         int     `json:"Concurrency"`
	ChunkSize           int64   `json:"ChunkSize"`
	ConcurrencyAfter    int     `json:"ConcurrencyAfter"`
	ChunkSizeAfter      int64   `json:"ChunkSizeAfter"`
	Throughput          float64 `json:"Throughput"`
	AggregateThroughput float64 `json:"AggregateThroughput"`
	RequestDuration     int64   `json:"RequestDuration"`
	Retries             int64   `json:"Retries"`
	FailedRequests      int64   `json:"FailedRequests"`
}

type lockinDecision struct {
	Index            int       `json:"index"`
	Recorded         int       `json:"recorded_action"`
	Selected         int       `json:"selected_action"`
	Match            bool      `json:"match"`
	Reward           float64   `json:"reward"`
	Concurrency      int       `json:"conc_before"`
	ConcurrencyAfter int       `json:"conc_after"`
	ChunkSize        int64     `json:"chunk_before"`
	ChunkSizeAfter   int64     `json:"chunk_after"`
	Means            []float64 `json:"means"`
	Bonuses          []float64 `json:"bonuses"`
	Scores           []float64 `json:"scores"`
	Features         []float64 `json:"features"`
}

type lockinRun struct {
	Scenario  string           `json:"scenario"`
	Rep       int              `json:"rep"`
	Decisions []lockinDecision `json:"decisions"`
	Matches   int              `json:"matches"`
	Total     int              `json:"total"`
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func stateFromV3(d v3Decision) State {
	return State{
		Concurrency:         d.Concurrency,
		ChunkSize:           d.ChunkSize,
		Throughput:          d.Throughput,
		AggregateThroughput: d.AggregateThroughput,
		RequestDuration:     time.Duration(d.RequestDuration),
		BytesTransferred:    32768, // clamps to feature 1.0; exact for bytes >= 1000
		SuccessfulRequests:  d.RequestIndex,
		FailedRequests:      d.FailedRequests,
		Retries:             d.Retries,
		TotalAttempts:       d.RequestIndex,
	}
}

// replayRun drives the production LinUCB over a recorded trajectory, capturing
// the posterior (mean + bonus + total) for all five arms at every decision.
func replayRun(t *testing.T, scenario string, rep int, decs []v3Decision) lockinRun {
	t.Helper()
	l, err := NewLinUCB(LinUCBConfig{Alpha: 0.2, NumActions: NumActions, Dimension: len(FeatureNames()), InitialExplorationRounds: 1})
	if err != nil {
		t.Fatalf("NewLinUCB: %v", err)
	}
	alpha := l.alpha
	out := lockinRun{Scenario: scenario, Rep: rep, Total: len(decs)}
	var prevState State
	var prevAction ActionIndex
	have := false
	for i, d := range decs {
		s := stateFromV3(d)
		if have {
			l.Update(prevState, prevAction, Reward{Value: decs[i-1].Reward})
		}
		x, err := s.Features(defaultFeatureConfig())
		if err != nil {
			t.Fatalf("features: %v", err)
		}
		means := make([]float64, NumActions)
		bonuses := make([]float64, NumActions)
		scores := make([]float64, NumActions)
		for a := 0; a < NumActions; a++ {
			scores[a] = l.score(a, x)
			l.alpha = 0
			means[a] = l.score(a, x)
			l.alpha = alpha
			bonuses[a] = scores[a] - means[a]
		}
		sel := l.SelectAction(s)
		rd := lockinDecision{
			Index: i, Recorded: d.ActionIndex, Selected: int(sel), Match: int(sel) == d.ActionIndex,
			Reward: d.Reward, Concurrency: d.Concurrency, ConcurrencyAfter: d.ConcurrencyAfter,
			ChunkSize: d.ChunkSize, ChunkSizeAfter: d.ChunkSizeAfter,
			Means: means, Bonuses: bonuses, Scores: scores, Features: x,
		}
		if rd.Match {
			out.Matches++
		}
		out.Decisions = append(out.Decisions, rd)
		prevState, prevAction, have = s, sel, true
	}
	return out
}

func mean(x []float64) float64 { return stat.Mean(x, nil) }

func sd(x []float64) float64 {
	if len(x) < 2 {
		return 0
	}
	return stat.StdDev(x, nil)
}

func minmax(x []float64) (float64, float64) {
	if len(x) == 0 {
		return 0, 0
	}
	lo, hi := x[0], x[0]
	for _, v := range x {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo, hi
}

func argmax(x []float64) int {
	best := 0
	for i := 1; i < len(x); i++ {
		if x[i] > x[best] {
			best = i
		}
	}
	return best
}

func TestLinUCBLockinDiagnostic(t *testing.T) {
	root := findRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "benchmark_v3", "matrix_5mode_4MiB_reps10.json"))
	if err != nil {
		t.Fatalf("read v3 json: %v", err)
	}
	var m v3Matrix
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal v3 json: %v", err)
	}

	var allRuns []lockinRun
	for _, sc := range m.Scenarios {
		for _, r := range sc.LinUCB {
			allRuns = append(allRuns, replayRun(t, sc.Name, r.Repetition, r.Decisions))
		}
	}

	// Validate reconstruction: every re-selected action must match the recording.
	totalDec, totalMatch := 0, 0
	for _, r := range allRuns {
		totalDec += r.Total
		totalMatch += r.Matches
	}
	if totalMatch != totalDec {
		t.Fatalf("replay mismatch: %d/%d actions matched; reconstruction assumption is wrong", totalMatch, totalDec)
	}

	report := buildReport(root, m, allRuns)

	dir := filepath.Join(root, "testdata", "diagnostics", "linucb_lockin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeJSON(t, filepath.Join(dir, "report.json"), report)
	writeJSON(t, filepath.Join(dir, "score_trajectory.json"), buildScoreTrajectory(allRuns))
	writeJSON(t, filepath.Join(dir, "feature_analysis.json"), report.Feature)
	writeText(t, filepath.Join(dir, "report.txt"), report)
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ---- report structures ----

type featureStat struct {
	Name         string  `json:"name"`
	Min          float64 `json:"min"`
	Max          float64 `json:"max"`
	Mean         float64 `json:"mean"`
	SD           float64 `json:"sd"`
	MeanSquare   float64 `json:"mean_square"`
	ShareOfNorm2 float64 `json:"share_of_norm2"`
	FracAtZero   float64 `json:"frac_at_zero"`
	FracAtOne    float64 `json:"frac_at_one"`
	AlwaysOne    bool    `json:"always_one"`
}

type featureReport struct {
	Names    []string      `json:"names"`
	Stats    []featureStat `json:"stats"`
	NormMean float64       `json:"norm_mean"`
	NormMin  float64       `json:"norm_min"`
	NormMax  float64       `json:"norm_max"`
	NormSD   float64       `json:"norm_sd"`
	Pearson  []pearsonPair `json:"pearson"`
}

type pearsonPair struct {
	A    string  `json:"a"`
	B    string  `json:"b"`
	Corr float64 `json:"corr"`
}

type rewardReport struct {
	OverallCount int            `json:"overall_count"`
	OverallMean  float64        `json:"overall_mean"`
	OverallSD    float64        `json:"overall_sd"`
	OverallMin   float64        `json:"overall_min"`
	OverallMax   float64        `json:"overall_max"`
	WarmupMean   float64        `json:"warmup_mean"`
	LearnedMean  float64        `json:"learned_mean"`
	LearnedSD    float64        `json:"learned_sd"`
	ByAction     []actionReward `json:"by_action"`
}

type actionReward struct {
	Action string  `json:"action"`
	Count  int     `json:"count"`
	Mean   float64 `json:"mean"`
	SD     float64 `json:"sd"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

type warmupReport struct {
	OrderOK      bool      `json:"order_ok"`
	Actions      []string  `json:"actions"`
	Rewards      []float64 `json:"rewards"`
	Effective    []bool    `json:"effective"`
	NoopCount    int       `json:"noop_count"`
	PosteriorTop string    `json:"posterior_best_arm"`
	Margins      []float64 `json:"uscb_scores_at_decision6"`
}

type explorationReport struct {
	FirstGreedyDecision int       `json:"first_greedy_decision"`
	SelectedArm         string    `json:"selected_arm"`
	SelectedMean        float64   `json:"selected_mean"`
	SelectedBonus       float64   `json:"selected_bonus"`
	MaxOtherBonus       float64   `json:"max_other_bonus"`
	MaxOtherScore       float64   `json:"max_other_score"`
	Margin              float64   `json:"margin"`
	MeanGTBonus         bool      `json:"selected_mean_exceeds_any_possible_bonus"`
	SelectedMeanTrace   []float64 `json:"selected_mean_over_decisions"`
	MaxOtherBonusTrace  []float64 `json:"max_other_bonus_over_decisions"`
	MaxOtherScoreTrace  []float64 `json:"max_other_score_over_decisions"`
}

type lockinReport struct {
	DominantPerRun      map[string]int `json:"dominant_arm_counts_by_name"`
	FirstDominant       string         `json:"first_dominant_arm"`
	FinalDominant       string         `json:"final_dominant_arm"`
	LockDecisionMin     int            `json:"lock_decision_min"`
	LockDecisionMax     int            `json:"lock_decision_max"`
	LockDecisionMean    float64        `json:"lock_decision_mean"`
	RunsWithEscape      int            `json:"runs_with_post_warmup_switch"`
	UniquePostWarmupMin int            `json:"unique_post_warmup_arms_min"`
	UniquePostWarmupMax int            `json:"unique_post_warmup_arms_max"`
}

type noopReport struct {
	Action     string  `json:"action"`
	Selections int     `json:"selections"`
	Effective  int     `json:"effective_changes"`
	Noop       int     `json:"noop"`
	NoopFrac   float64 `json:"noop_fraction"`
}

type crossRow struct {
	Scenario         string  `json:"scenario"`
	Runs             int     `json:"runs"`
	TotalDecisions   int     `json:"total_decisions"`
	Warmup           int     `json:"warmup_decisions"`
	LearnedMean      float64 `json:"learned_decisions_mean"`
	LearnedMin       int     `json:"learned_decisions_min"`
	LearnedMax       int     `json:"learned_decisions_max"`
	DominantArm      string  `json:"post_warmup_dominant_arm"`
	UniqueArmsMean   float64 `json:"unique_post_warmup_arms_mean"`
	ConcChanges      int     `json:"post_warmup_concurrency_changes"`
	ChunkChanges     int     `json:"post_warmup_chunk_changes"`
	RewardMin        float64 `json:"learned_reward_min"`
	RewardMax        float64 `json:"learned_reward_max"`
	LockDecisionMean float64 `json:"lock_decision_mean"`
	RunsWithEscape   int     `json:"runs_with_post_warmup_switch"`
}

type runRow struct {
	Scenario      string `json:"scenario"`
	Rep           int    `json:"rep"`
	Total         int    `json:"total_decisions"`
	Learned       int    `json:"learned_decisions"`
	FirstDominant string `json:"first_dominant_arm"`
	FinalDominant string `json:"final_dominant_arm"`
	LockDecision  int    `json:"lock_decision"`
	UniquePost    int    `json:"unique_post_warmup_arms"`
	ConcChanges   int    `json:"post_warmup_concurrency_changes"`
	ChunkChanges  int    `json:"post_warmup_chunk_changes"`
}

type reconInfo struct {
	RecordedFields []string `json:"recorded_fields"`
	MissingFields  []string `json:"missing_fields"`
	Assumptions    []string `json:"assumptions"`
	TotalActions   int      `json:"total_actions"`
	MatchedActions int      `json:"matched_actions"`
}

type diagReport struct {
	Source         string                       `json:"source"`
	Reconstruction reconInfo                    `json:"reconstruction"`
	Feature        featureReport                `json:"feature_analysis"`
	Reward         map[string]rewardReport      `json:"reward_analysis"`
	Warmup         map[string]warmupReport      `json:"warmup"`
	Lockin         map[string]lockinReport      `json:"lockin"`
	Exploration    map[string]explorationReport `json:"exploration"`
	Noop           []noopReport                 `json:"post_warmup_action_effectiveness"`
	CrossScenario  []crossRow                   `json:"cross_scenario"`
	Runs           []runRow                     `json:"runs"`
}

func buildReport(root string, m v3Matrix, runs []lockinRun) diagReport {
	rep := diagReport{
		Source: "testdata/benchmark_v3/matrix_5mode_4MiB_reps10.json",
		Reconstruction: reconInfo{
			RecordedFields: []string{"Concurrency", "ChunkSize", "Throughput", "AggregateThroughput", "RequestDuration", "FailedRequests", "Retries", "TotalAttempts(RequestIndex)"},
			MissingFields:  []string{"BytesTransferred", "SuccessfulRequests"},
			Assumptions: []string{
				"SuccessfulRequests = RequestIndex (failures==0 and retries==0 in all v3 runs)",
				"BytesTransferred = 32768; the bytes feature clamps to 1.0 for any bytes >= CounterScale(1000), so the feature is exact for full chunks",
			},
		},
		Feature:       featureAnalysis(runs),
		Reward:        rewardAnalysis(runs),
		Warmup:        warmupAnalysis(runs),
		Lockin:        lockinAnalysis(runs),
		Exploration:   explorationAnalysis(runs),
		Noop:          noopAnalysis(runs),
		CrossScenario: crossScenario(runs),
		Runs:          runRows(runs),
	}
	for _, r := range runs {
		rep.Reconstruction.TotalActions += r.Total
		rep.Reconstruction.MatchedActions += r.Matches
	}
	return rep
}

func featureAnalysis(runs []lockinRun) featureReport {
	names := FeatureNames()
	cols := make([][]float64, len(names))
	var norms []float64
	var all [][]float64
	for _, r := range runs {
		for _, d := range r.Decisions {
			all = append(all, d.Features)
			var n2 float64
			for j, v := range d.Features {
				cols[j] = append(cols[j], v)
				n2 += v * v
			}
			norms = append(norms, math.Sqrt(n2))
		}
	}
	meanNormSq := 0.0
	for _, d := range all {
		for _, v := range d {
			meanNormSq += v * v
		}
	}
	meanNormSq /= float64(len(all))
	fr := featureReport{Names: names}
	for j, name := range names {
		col := cols[j]
		lo, hi := minmax(col)
		ms := 0.0
		for _, v := range col {
			ms += v * v
		}
		ms /= float64(len(col))
		z, one := 0, 0
		for _, v := range col {
			if v == 0 {
				z++
			}
			if v == 1 {
				one++
			}
		}
		fr.Stats = append(fr.Stats, featureStat{
			Name: name, Min: lo, Max: hi, Mean: mean(col), SD: sd(col),
			MeanSquare: ms, ShareOfNorm2: ms / meanNormSq,
			FracAtZero: float64(z) / float64(len(col)), FracAtOne: float64(one) / float64(len(col)),
			AlwaysOne: one == len(col),
		})
	}
	fr.NormMean = mean(norms)
	fr.NormMin, fr.NormMax = minmax(norms)
	fr.NormSD = sd(norms)
	pairs := [][2]int{{2, 3}, {6, 9}, {8, 7}, {5, 9}, {0, 1}}
	for _, p := range pairs {
		c := stat.Correlation(cols[p[0]], cols[p[1]], nil)
		if math.IsNaN(c) || math.IsInf(c, 0) {
			// Undefined when a feature is constant (zero variance), e.g. failures.
			c = 0
		}
		fr.Pearson = append(fr.Pearson, pearsonPair{A: names[p[0]], B: names[p[1]], Corr: c})
	}
	return fr
}

func rewardAnalysis(runs []lockinRun) map[string]rewardReport {
	out := map[string]rewardReport{}
	byScen := map[string][]lockinRun{}
	for _, r := range runs {
		byScen[r.Scenario] = append(byScen[r.Scenario], r)
	}
	names := []string{"none", "increase-concurrency", "decrease-concurrency", "increase-chunk-size", "decrease-chunk-size"}
	for scen, rs := range byScen {
		rr := rewardReport{}
		var overall, warm, learned []float64
		byAction := make([][]float64, NumActions)
		for _, r := range rs {
			for i, d := range r.Decisions {
				// The last decision has no measured reward (0 by construction).
				if i == len(r.Decisions)-1 {
					continue
				}
				overall = append(overall, d.Reward)
				if i < lockinWarmup {
					warm = append(warm, d.Reward)
				} else {
					learned = append(learned, d.Reward)
					if d.Recorded >= 0 && d.Recorded < NumActions {
						byAction[d.Recorded] = append(byAction[d.Recorded], d.Reward)
					}
				}
			}
		}
		rr.OverallCount = len(overall)
		rr.OverallMean, rr.OverallSD = mean(overall), sd(overall)
		rr.OverallMin, rr.OverallMax = minmax(overall)
		rr.WarmupMean = mean(warm)
		rr.LearnedMean, rr.LearnedSD = mean(learned), sd(learned)
		for a := 0; a < NumActions; a++ {
			if len(byAction[a]) == 0 {
				continue
			}
			lo, hi := minmax(byAction[a])
			rr.ByAction = append(rr.ByAction, actionReward{Action: names[a], Count: len(byAction[a]), Mean: mean(byAction[a]), SD: sd(byAction[a]), Min: lo, Max: hi})
		}
		out[scen] = rr
	}
	return out
}

func warmupAnalysis(runs []lockinRun) map[string]warmupReport {
	out := map[string]warmupReport{}
	byScen := map[string][]lockinRun{}
	for _, r := range runs {
		byScen[r.Scenario] = append(byScen[r.Scenario], r)
	}
	names := []string{"none", "increase-concurrency", "decrease-concurrency", "increase-chunk-size", "decrease-chunk-size"}
	for scen, rs := range byScen {
		wr := warmupReport{OrderOK: true}
		var margins []float64
		bestCounts := map[string]int{}
		for _, r := range rs {
			if len(r.Decisions) < lockinWarmup+1 {
				wr.OrderOK = false
				continue
			}
			for i := 0; i < lockinWarmup; i++ {
				wr.Actions = append(wr.Actions, names[r.Decisions[i].Recorded])
			}
			// effectiveness from recorded state transitions
			for i := 0; i < lockinWarmup; i++ {
				d := r.Decisions[i]
				eff := false
				switch d.Recorded {
				case 0:
					eff = true // None: intended effect = no change
				case 1:
					eff = d.ConcurrencyAfter > d.Concurrency
				case 2:
					eff = d.ConcurrencyAfter < d.Concurrency
				case 3:
					eff = d.ChunkSizeAfter > d.ChunkSize
				case 4:
					eff = d.ChunkSizeAfter < d.ChunkSize
				}
				wr.Effective = append(wr.Effective, eff)
				if !eff && d.Recorded != 0 {
					wr.NoopCount++
				}
			}
			// posterior at the first greedy decision (index 5): best arm by score
			d5 := r.Decisions[lockinWarmup]
			best := argmax(d5.Scores)
			bestCounts[names[best]]++
			wr.PosteriorTop = names[best]
			margins = append(margins, d5.Scores[best]-secondBest(d5.Scores))
		}
		wr.Margins = margins
		out[scen] = wr
	}
	return out
}

func secondBest(x []float64) float64 {
	best, second := math.Inf(-1), math.Inf(-1)
	for _, v := range x {
		if v > best {
			second = best
			best = v
		} else if v > second {
			second = v
		}
	}
	return second
}

func lockinAnalysis(runs []lockinRun) map[string]lockinReport {
	out := map[string]lockinReport{}
	names := []string{"none", "increase-concurrency", "decrease-concurrency", "increase-chunk-size", "decrease-chunk-size"}
	byScen := map[string][]lockinRun{}
	for _, r := range runs {
		byScen[r.Scenario] = append(byScen[r.Scenario], r)
	}
	for scen, rs := range byScen {
		lr := lockinReport{DominantPerRun: map[string]int{}, LockDecisionMin: 1 << 30}
		var locks []float64
		uniqMin := 1 << 30
		for _, r := range rs {
			post := r.Decisions[lockinWarmup:]
			unique := map[int]bool{}
			for _, d := range post {
				unique[d.Recorded] = true
			}
			if len(unique) > 1 {
				lr.RunsWithEscape++
			}
			if len(unique) < uniqMin {
				uniqMin = len(unique)
			}
			if len(unique) > lr.UniquePostWarmupMax {
				lr.UniquePostWarmupMax = len(unique)
			}
			final := post[len(post)-1].Recorded
			first := post[0].Recorded
			lr.DominantPerRun[names[final]]++
			if lr.FirstDominant == "" {
				lr.FirstDominant = names[first]
			}
			lr.FinalDominant = names[final]
			lock := lockDecision(post)
			locks = append(locks, float64(lock))
			if lock < lr.LockDecisionMin {
				lr.LockDecisionMin = lock
			}
			if lock > lr.LockDecisionMax {
				lr.LockDecisionMax = lock
			}
		}
		lr.LockDecisionMean = mean(locks)
		lr.UniquePostWarmupMin = uniqMin
		out[scen] = lr
	}
	return out
}

// lockDecision returns the first absolute decision index from which all later
// selections are identical (the lock-on point).
func lockDecision(post []lockinDecision) int {
	final := post[len(post)-1].Recorded
	for i := len(post) - 1; i >= 0; i-- {
		if post[i].Recorded != final {
			return post[i+1].Index
		}
	}
	return post[0].Index
}

func explorationAnalysis(runs []lockinRun) map[string]explorationReport {
	out := map[string]explorationReport{}
	names := []string{"none", "increase-concurrency", "decrease-concurrency", "increase-chunk-size", "decrease-chunk-size"}
	byScen := map[string][]lockinRun{}
	for _, r := range runs {
		byScen[r.Scenario] = append(byScen[r.Scenario], r)
	}
	for scen, rs := range byScen {
		er := explorationReport{FirstGreedyDecision: lockinWarmup}
		var selMeans, otherBonuses, otherScores []float64
		for _, r := range rs {
			d5 := r.Decisions[lockinWarmup]
			sel := d5.Recorded
			if er.SelectedArm == "" {
				er.SelectedArm = names[sel]
			}
			er.SelectedMean += d5.Means[sel]
			er.SelectedBonus += d5.Bonuses[sel]
			maxOtherBonus, maxOtherScore := math.Inf(-1), math.Inf(-1)
			for a := 0; a < NumActions; a++ {
				if a == sel {
					continue
				}
				if d5.Bonuses[a] > maxOtherBonus {
					maxOtherBonus = d5.Bonuses[a]
				}
				if d5.Scores[a] > maxOtherScore {
					maxOtherScore = d5.Scores[a]
				}
			}
			er.MaxOtherBonus += maxOtherBonus
			er.MaxOtherScore += maxOtherScore
			er.Margin += d5.Scores[sel] - maxOtherScore
			for i := lockinWarmup; i < len(r.Decisions); i++ {
				d := r.Decisions[i]
				selMeans = append(selMeans, d.Means[d.Recorded])
				mb, ms := math.Inf(-1), math.Inf(-1)
				for a := 0; a < NumActions; a++ {
					if a == d.Recorded {
						continue
					}
					if d.Bonuses[a] > mb {
						mb = d.Bonuses[a]
					}
					if d.Scores[a] > ms {
						ms = d.Scores[a]
					}
				}
				otherBonuses = append(otherBonuses, mb)
				otherScores = append(otherScores, ms)
			}
		}
		n := float64(len(rs))
		er.SelectedMean /= n
		er.SelectedBonus /= n
		er.MaxOtherBonus /= n
		er.MaxOtherScore /= n
		er.Margin /= n
		er.MeanGTBonus = er.SelectedMean > er.MaxOtherBonus
		er.SelectedMeanTrace = selMeans
		er.MaxOtherBonusTrace = otherBonuses
		er.MaxOtherScoreTrace = otherScores
		out[scen] = er
	}
	return out
}

func noopAnalysis(runs []lockinRun) []noopReport {
	names := []string{"none", "increase-concurrency", "decrease-concurrency", "increase-chunk-size", "decrease-chunk-size"}
	out := make([]noopReport, NumActions)
	for a := range out {
		out[a].Action = names[a]
	}
	for _, r := range runs {
		for i, d := range r.Decisions {
			if i < lockinWarmup {
				continue
			}
			out[d.Recorded].Selections++
			eff := false
			switch d.Recorded {
			case 0:
				eff = true
			case 1:
				eff = d.ConcurrencyAfter > d.Concurrency
			case 2:
				eff = d.ConcurrencyAfter < d.Concurrency
			case 3:
				eff = d.ChunkSizeAfter > d.ChunkSize
			case 4:
				eff = d.ChunkSizeAfter < d.ChunkSize
			}
			if eff {
				out[d.Recorded].Effective++
			} else {
				out[d.Recorded].Noop++
			}
		}
	}
	for a := range out {
		if out[a].Selections > 0 {
			out[a].NoopFrac = float64(out[a].Noop) / float64(out[a].Selections)
		}
	}
	return out
}

func crossScenario(runs []lockinRun) []crossRow {
	names := []string{"none", "increase-concurrency", "decrease-concurrency", "increase-chunk-size", "decrease-chunk-size"}
	byScen := map[string][]lockinRun{}
	for _, r := range runs {
		byScen[r.Scenario] = append(byScen[r.Scenario], r)
	}
	order := []string{"baseline", "server-throttle", "latency", "loss", "combined"}
	var out []crossRow
	for _, scen := range order {
		rs := byScen[scen]
		if len(rs) == 0 {
			continue
		}
		row := crossRow{Scenario: scen, Runs: len(rs)}
		dominant := map[int]int{}
		var learned []int
		var rewards []float64
		var locks []float64
		var uniq []float64
		for _, r := range rs {
			row.TotalDecisions += r.Total
			row.Warmup += lockinWarmup
			learned = append(learned, r.Total-lockinWarmup)
			post := r.Decisions[lockinWarmup:]
			dset := map[int]bool{}
			for _, d := range post {
				dset[d.Recorded] = true
				if d.ConcurrencyAfter != d.Concurrency {
					row.ConcChanges++
				}
				if d.ChunkSizeAfter != d.ChunkSize {
					row.ChunkChanges++
				}
			}
			uniq = append(uniq, float64(len(dset)))
			if len(dset) > 1 {
				row.RunsWithEscape++
			}
			final := post[len(post)-1].Recorded
			dominant[final]++
			locks = append(locks, float64(lockDecision(post)))
			for i, d := range r.Decisions {
				if i == len(r.Decisions)-1 {
					continue
				}
				if i >= lockinWarmup {
					rewards = append(rewards, d.Reward)
				}
			}
		}
		row.LearnedMean = mean(float64s(learned))
		sort.Ints(learned)
		row.LearnedMin, row.LearnedMax = learned[0], learned[len(learned)-1]
		// dominant arm = most common final arm
		best, bestN := -1, -1
		for a, c := range dominant {
			if c > bestN {
				best, bestN = a, c
			}
		}
		row.DominantArm = names[best]
		row.UniqueArmsMean = mean(uniq)
		row.LockDecisionMean = mean(locks)
		if len(rewards) > 0 {
			row.RewardMin, row.RewardMax = minmax(rewards)
		}
		out = append(out, row)
	}
	return out
}

func float64s(xs []int) []float64 {
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = float64(x)
	}
	return out
}

func runRows(runs []lockinRun) []runRow {
	names := []string{"none", "increase-concurrency", "decrease-concurrency", "increase-chunk-size", "decrease-chunk-size"}
	var out []runRow
	for _, r := range runs {
		post := r.Decisions[lockinWarmup:]
		dset := map[int]bool{}
		conc, chunk := 0, 0
		for _, d := range post {
			dset[d.Recorded] = true
			if d.ConcurrencyAfter != d.Concurrency {
				conc++
			}
			if d.ChunkSizeAfter != d.ChunkSize {
				chunk++
			}
		}
		out = append(out, runRow{
			Scenario: r.Scenario, Rep: r.Rep, Total: r.Total, Learned: r.Total - lockinWarmup,
			FirstDominant: names[post[0].Recorded], FinalDominant: names[post[len(post)-1].Recorded],
			LockDecision: lockDecision(post), UniquePost: len(dset),
			ConcChanges: conc, ChunkChanges: chunk,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scenario != out[j].Scenario {
			return out[i].Scenario < out[j].Scenario
		}
		return out[i].Rep < out[j].Rep
	})
	return out
}

type scoreTrajectory struct {
	Source    string                    `json:"source"`
	Warmup    int                       `json:"warmup"`
	Scenarios []scoreTrajectoryScenario `json:"scenarios"`
}
type scoreTrajectoryScenario struct {
	Name string               `json:"name"`
	Runs []scoreTrajectoryRun `json:"runs"`
}
type scoreTrajectoryRun struct {
	Rep       int                       `json:"rep"`
	Decisions []scoreTrajectoryDecision `json:"decisions"`
}
type scoreTrajectoryDecision struct {
	Index    int       `json:"index"`
	Recorded int       `json:"recorded"`
	Reward   float64   `json:"reward"`
	Means    []float64 `json:"means"`
	Bonuses  []float64 `json:"bonuses"`
	Scores   []float64 `json:"scores"`
}

func buildScoreTrajectory(runs []lockinRun) scoreTrajectory {
	st := scoreTrajectory{Source: "testdata/benchmark_v3/matrix_5mode_4MiB_reps10.json", Warmup: lockinWarmup}
	byScen := map[string][]lockinRun{}
	order := []string{}
	for _, r := range runs {
		if _, ok := byScen[r.Scenario]; !ok {
			order = append(order, r.Scenario)
		}
		byScen[r.Scenario] = append(byScen[r.Scenario], r)
	}
	for _, scen := range order {
		sc := scoreTrajectoryScenario{Name: scen}
		for _, r := range byScen[scen] {
			sr := scoreTrajectoryRun{Rep: r.Rep}
			for _, d := range r.Decisions {
				sr.Decisions = append(sr.Decisions, scoreTrajectoryDecision{
					Index: d.Index, Recorded: d.Recorded, Reward: d.Reward,
					Means: d.Means, Bonuses: d.Bonuses, Scores: d.Scores,
				})
			}
			sc.Runs = append(sc.Runs, sr)
		}
		st.Scenarios = append(st.Scenarios, sc)
	}
	return st
}

func writeText(t *testing.T, path string, r diagReport) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	names := []string{"none", "increase-concurrency", "decrease-concurrency", "increase-chunk-size", "decrease-chunk-size"}
	fmt.Fprintln(f, "LinUCB post-warm-up lock-in diagnostic (SEPARATE; replay of frozen benchmark v3)")
	fmt.Fprintf(f, "source: %s\n", r.Source)
	fmt.Fprintf(f, "reconstruction: %d/%d replayed actions match the recording; missing fields=%v\n",
		r.Reconstruction.MatchedActions, r.Reconstruction.TotalActions, r.Reconstruction.MissingFields)
	fmt.Fprintln(f)
	fmt.Fprintln(f, "FEATURE ANALYSIS (all LinUCB decisions)")
	fmt.Fprintf(f, "  ||x||: mean=%.4f min=%.4f max=%.4f sd=%.4f\n", r.Feature.NormMean, r.Feature.NormMin, r.Feature.NormMax, r.Feature.NormSD)
	fmt.Fprintf(f, "  %-20s %8s %8s %8s %8s %10s %8s %8s\n", "feature", "min", "max", "mean", "sd", "shareNorm2", "atZero", "atOne")
	for _, s := range r.Feature.Stats {
		fmt.Fprintf(f, "  %-20s %8.4f %8.4f %8.4f %8.4f %9.1f%% %7.1f%% %7.1f%%\n",
			s.Name, s.Min, s.Max, s.Mean, s.SD, 100*s.ShareOfNorm2, 100*s.FracAtZero, 100*s.FracAtOne)
	}
	for _, p := range r.Feature.Pearson {
		fmt.Fprintf(f, "  corr(%s,%s)=%.3f\n", p.A, p.B, p.Corr)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "WARM-UP (first 5 actions) and posterior at the first greedy decision")
	for _, scen := range []string{"baseline", "server-throttle", "latency", "loss", "combined"} {
		w := r.Warmup[scen]
		e := r.Exploration[scen]
		fmt.Fprintf(f, "  == %s == order_ok=%v noops=%d posterior_best=%s\n", scen, w.OrderOK, w.NoopCount, w.PosteriorTop)
		fmt.Fprintf(f, "     selected=%s mean=%.3f bonus=%.3f maxOtherBonus=%.3f maxOtherScore=%.3f margin=%.3f mean>bonus=%v\n",
			e.SelectedArm, e.SelectedMean, e.SelectedBonus, e.MaxOtherBonus, e.MaxOtherScore, e.Margin, e.MeanGTBonus)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "LOCK-IN")
	for _, scen := range []string{"baseline", "server-throttle", "latency", "loss", "combined"} {
		l := r.Lockin[scen]
		fmt.Fprintf(f, "  %-16s first=%s final=%s lockDecisions=[%d..%d] mean=%.1f escapes=%d/%d uniqueArms=%d..%d\n",
			scen, l.FirstDominant, l.FinalDominant, l.LockDecisionMin, l.LockDecisionMax, l.LockDecisionMean,
			l.RunsWithEscape, 10, l.UniquePostWarmupMin, l.UniquePostWarmupMax)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "REWARD (learned phase; last decision excluded)")
	for _, scen := range []string{"baseline", "server-throttle", "latency", "loss", "combined"} {
		rr := r.Reward[scen]
		fmt.Fprintf(f, "  %-16s overall mean=%.3f sd=%.3f [%.3f,%.3f] warmupMean=%.3f learnedMean=%.3f\n",
			scen, rr.OverallMean, rr.OverallSD, rr.OverallMin, rr.OverallMax, rr.WarmupMean, rr.LearnedMean)
		for _, a := range rr.ByAction {
			fmt.Fprintf(f, "      %-22s n=%4d mean=%.3f sd=%.3f [%.3f,%.3f]\n", a.Action, a.Count, a.Mean, a.SD, a.Min, a.Max)
		}
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "POST-WARM-UP ACTION EFFECTIVENESS")
	for _, n := range r.Noop {
		fmt.Fprintf(f, "  %-22s selections=%4d effective=%4d noop=%4d (%.0f%%)\n", n.Action, n.Selections, n.Effective, n.Noop, 100*n.NoopFrac)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "CROSS-SCENARIO")
	fmt.Fprintf(f, "  %-16s %6s %6s %8s %-22s %8s %6s %6s\n", "scenario", "runs", "warm", "learned", "dominant", "uniq", "concCh", "chunkCh")
	for _, c := range r.CrossScenario {
		fmt.Fprintf(f, "  %-16s %6d %6d %8.1f %-22s %8.2f %6d %6d\n", c.Scenario, c.Runs, c.Warmup, c.LearnedMean, c.DominantArm, c.UniqueArmsMean, c.ConcChanges, c.ChunkChanges)
	}
	_ = names
}
