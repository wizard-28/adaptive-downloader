package rl

// Diagnostic for the periodic-exploration correction. It replays the frozen
// benchmark v3 per-decision states through the production LinUCB with
// ExplorationInterval = 0 and = 5, using the production RewardCalculator to
// score each transition for the action the (replayed) policy selects. The
// environment states are held at their v3 observations, so this isolates the
// exploration/posterior mechanism rather than download dynamics.
//
// Deterministic: no wall-clock or randomness. Writes
// testdata/diagnostics/linucb_periodic_exploration/{report.json,report.txt}.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

type peScenarioStats struct {
	Name                 string             `json:"name"`
	Runs                 int                `json:"runs"`
	TotalDecisionsMean   float64            `json:"total_decisions_mean"`
	Warmup               int                `json:"warmup_decisions"`
	NormalMean           float64            `json:"normal_decisions_mean"`
	ForcedMean           float64            `json:"forced_exploration_mean"`
	UniqueArmsMean       float64            `json:"unique_post_warmup_arms_mean"`
	ActionCounts         map[string]int     `json:"post_warmup_action_counts"`
	ActionFracs          map[string]float64 `json:"post_warmup_action_fractions"`
	PredictedEffective   map[string]int     `json:"predicted_effective"`
	PredictedNoop        map[string]int     `json:"predicted_noop"`
	ForcedEffective      map[string]int     `json:"forced_effective"`
	ForcedNoop           map[string]int     `json:"forced_noop"`
	SimConcChangesMean   float64            `json:"simulated_concurrency_changes_mean"`
	SimChunkChangesMean  float64            `json:"simulated_chunk_changes_mean"`
	SimFinalConcMean     float64            `json:"simulated_final_concurrency_mean"`
	SimFinalChunkMean    float64            `json:"simulated_final_chunk_mean"`
	DominantArm          string             `json:"dominant_arm"`
	DominantFractionMean float64            `json:"dominant_arm_fraction_mean"`
	RecordedMatch        int                `json:"recorded_action_matches"`
	RecordedTotal        int                `json:"recorded_action_total"`
}

type peComparison struct {
	Scenario              string  `json:"scenario"`
	Interval0Dominant     string  `json:"interval0_dominant_arm"`
	Interval0Unique       float64 `json:"interval0_unique_arms_mean"`
	Interval0DominantFrac float64 `json:"interval0_dominant_fraction_mean"`
	Interval0IncConc      int     `json:"interval0_increase_concurrency_count"`
	Interval5Dominant     string  `json:"interval5_dominant_arm"`
	Interval5Unique       float64 `json:"interval5_unique_arms_mean"`
	Interval5DominantFrac float64 `json:"interval5_dominant_fraction_mean"`
	Interval5IncConc      int     `json:"interval5_increase_concurrency_count"`
}

type pePosteriorEvent struct {
	Scenario    string  `json:"scenario"`
	Run         int     `json:"run"`
	Decision    int     `json:"decision"`
	Arm         string  `json:"arm"`
	Reward      float64 `json:"reward"`
	MeanBefore  float64 `json:"mean_before"`
	MeanAfter   float64 `json:"mean_after"`
	ScoreBefore float64 `json:"score_before"`
	ScoreAfter  float64 `json:"score_after"`
	ADiagBefore float64 `json:"a_diag_before"`
	ADiagAfter  float64 `json:"a_diag_after"`
}

type peReport struct {
	Source          string                       `json:"source"`
	Intervals       []int                        `json:"intervals"`
	Warmup          int                          `json:"warmup_decisions"`
	ByInterval      map[string][]peScenarioStats `json:"scenarios_by_interval"`
	Comparison      []peComparison               `json:"comparison"`
	PosteriorEvents []pePosteriorEvent           `json:"forced_exploration_posterior_events"`
}

func TestLinUCBPeriodicExplorationDiagnostic(t *testing.T) {
	root := findRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "benchmark_v3", "matrix_5mode_4MiB_reps10.json"))
	if err != nil {
		t.Fatalf("read v3: %v", err)
	}
	var m v3Matrix
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal v3: %v", err)
	}

	intervals := []int{0, 5}
	report := peReport{Source: "testdata/benchmark_v3/matrix_5mode_4MiB_reps10.json", Intervals: intervals, Warmup: lockinWarmup, ByInterval: map[string][]peScenarioStats{}}
	eventsByScen := map[string][]pePosteriorEvent{}

	var matches0, total0 int
	for _, iv := range intervals {
		for _, sc := range m.Scenarios {
			st, evs := replayPeriodicScenario(sc, iv)
			report.ByInterval[fmt.Sprint(iv)] = append(report.ByInterval[fmt.Sprint(iv)], st)
			if iv == 5 {
				eventsByScen[sc.Name] = evs
			}
			if iv == 0 {
				matches0 += st.RecordedMatch
				total0 += st.RecordedTotal
			}
		}
	}
	if matches0 != total0 {
		t.Fatalf("interval=0 replay mismatch: %d/%d recorded actions matched", matches0, total0)
	}
	report.PosteriorEvents = selectPosteriorEvents(eventsByScen)

	idx := map[string]map[int]peScenarioStats{}
	for _, sc := range m.Scenarios {
		idx[sc.Name] = map[int]peScenarioStats{}
	}
	for _, iv := range intervals {
		for _, st := range report.ByInterval[fmt.Sprint(iv)] {
			idx[st.Name][iv] = st
		}
	}
	for _, sc := range m.Scenarios {
		a, b := idx[sc.Name][0], idx[sc.Name][5]
		report.Comparison = append(report.Comparison, peComparison{
			Scenario:          sc.Name,
			Interval0Dominant: a.DominantArm, Interval0Unique: a.UniqueArmsMean, Interval0DominantFrac: a.DominantFractionMean, Interval0IncConc: a.ActionCounts["increase-concurrency"],
			Interval5Dominant: b.DominantArm, Interval5Unique: b.UniqueArmsMean, Interval5DominantFrac: b.DominantFractionMean, Interval5IncConc: b.ActionCounts["increase-concurrency"],
		})
	}

	dir := filepath.Join(root, "testdata", "diagnostics", "linucb_periodic_exploration")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeJSON(t, filepath.Join(dir, "report.json"), report)
	writePeriodicText(t, filepath.Join(dir, "report.txt"), report)
}

var peActionNames = []string{"none", "increase-concurrency", "decrease-concurrency", "increase-chunk-size", "decrease-chunk-size"}

func boundsOf(c v3Config) (minC, maxC int, minK, maxK int64) {
	minC, maxC = c.MinConcurrency, c.MaxConcurrency
	if minC == 0 {
		minC = 1
	}
	if maxC == 0 {
		maxC = 4
	}
	minK, maxK = c.MinChunkSize, c.MaxChunkSize
	if minK == 0 {
		minK = 32768
	}
	if maxK == 0 {
		maxK = 131072
	}
	return
}

// wouldChange predicts whether an action changes its controller target from the
// observed state and the scenario bounds (used because replayed states are held
// fixed at their v3 observations).
func wouldChange(a ActionIndex, s State, minC, maxC int, minK, maxK int64) bool {
	switch a {
	case ActionIncreaseConcurrency:
		return s.Concurrency < maxC
	case ActionDecreaseConcurrency:
		return s.Concurrency > minC
	case ActionIncreaseChunkSize:
		return s.ChunkSize < maxK
	case ActionDecreaseChunkSize:
		return s.ChunkSize > minK
	default:
		return true // None's intended effect is no change
	}
}

func applySim(c int, k int64, a ActionIndex, minC, maxC int, minK, maxK int64) (int, int64) {
	switch a {
	case ActionIncreaseConcurrency:
		if c < maxC {
			c++
		}
	case ActionDecreaseConcurrency:
		if c > minC {
			c--
		}
	case ActionIncreaseChunkSize:
		if k < maxK {
			k += 32768
			if k > maxK {
				k = maxK
			}
		}
	case ActionDecreaseChunkSize:
		if k > minK {
			k -= 32768
			if k < minK {
				k = minK
			}
		}
	}
	return c, k
}

func replayPeriodicScenario(sc v3Scenario, interval int) (peScenarioStats, []pePosteriorEvent) {
	minC, maxC, minK, maxK := boundsOf(sc.Config)
	st := peScenarioStats{Name: sc.Name, Warmup: lockinWarmup, ActionCounts: map[string]int{}, PredictedEffective: map[string]int{}, PredictedNoop: map[string]int{}, ForcedEffective: map[string]int{}, ForcedNoop: map[string]int{}}
	var events []pePosteriorEvent
	var forcedTotal, normalTotal, totalDec, uniqueSum, domFracSum, concChangesSum, chunkChangesSum, finalConcSum, finalChunkSum float64
	calc, err := NewRewardCalculator(DefaultRewardConfig())
	if err != nil {
		panic(err)
	}

	for runIdx, r := range sc.LinUCB {
		l, err := NewLinUCB(LinUCBConfig{Alpha: 0.2, NumActions: NumActions, Dimension: len(FeatureNames()), InitialExplorationRounds: 1, ExplorationInterval: interval})
		if err != nil {
			panic(err)
		}
		st.Runs++
		st.RecordedTotal += len(r.Decisions)

		var prevState State
		var prevAction ActionIndex
		have := false
		var pending *pePosteriorEvent
		postUnique := map[int]bool{}
		postCounts := map[int]int{}
		concSim, chunkSim := -1, int64(-1)
		concChanges, chunkChanges := 0, 0
		for i, d := range r.Decisions {
			totalDec++
			s := stateFromV3(d)
			var x []float64
			var means, scores []float64
			if have {
				rv := calc.Calculate(prevState, s, prevAction, true).Value
				l.Update(prevState, prevAction, Reward{Value: rv})
				if pending != nil {
					pending.Reward = rv
				}
			}
			x, err = s.Features(defaultFeatureConfig())
			if err != nil {
				panic(err)
			}
			means = make([]float64, NumActions)
			scores = make([]float64, NumActions)
			alpha := l.alpha
			for a := 0; a < NumActions; a++ {
				scores[a] = l.score(a, x)
				l.alpha = 0
				means[a] = l.score(a, x)
				l.alpha = alpha
			}
			if pending != nil {
				arm := actionIndexFromName(pending.Arm)
				pending.MeanAfter = means[arm]
				pending.ScoreAfter = scores[arm]
				pending.ADiagAfter = l.a[arm].At(5, 5)
				events = append(events, *pending)
				pending = nil
			}

			cursorBefore := l.periodicCursor
			sel := l.SelectAction(s)
			forced := i >= lockinWarmup && l.periodicCursor != cursorBefore
			if sel == ActionIndex(d.ActionIndex) {
				st.RecordedMatch++
			}

			if i < lockinWarmup {
				// warm-up; nothing aggregated here
			} else {
				st.ActionCounts[peActionNames[sel]]++
				postCounts[int(sel)]++
				postUnique[int(sel)] = true
				if wouldChange(sel, s, minC, maxC, minK, maxK) {
					st.PredictedEffective[peActionNames[sel]]++
					if forced {
						st.ForcedEffective[peActionNames[sel]]++
					}
				} else {
					st.PredictedNoop[peActionNames[sel]]++
					if forced {
						st.ForcedNoop[peActionNames[sel]]++
					}
				}
				if forced {
					forcedTotal++
				} else {
					normalTotal++
				}
				if concSim < 0 {
					concSim, chunkSim = s.Concurrency, s.ChunkSize
				}
				nc, nk := applySim(concSim, chunkSim, sel, minC, maxC, minK, maxK)
				if nc != concSim {
					concChanges++
				}
				if nk != chunkSim {
					chunkChanges++
				}
				concSim, chunkSim = nc, nk
			}

			if forced {
				pending = &pePosteriorEvent{
					Scenario: sc.Name, Run: runIdx, Decision: i, Arm: peActionNames[sel],
					MeanBefore: means[sel], ScoreBefore: scores[sel], ADiagBefore: l.a[sel].At(5, 5),
				}
			}
			prevState, prevAction, have = s, sel, true
		}
		uniqueSum += float64(len(postUnique))
		total := 0
		for _, c := range postCounts {
			total += c
		}
		dom := 0
		for a, c := range postCounts {
			if c > postCounts[dom] {
				dom = a
			}
		}
		if total > 0 {
			domFracSum += float64(postCounts[dom]) / float64(total)
		}
		if st.DominantArm == "" {
			st.DominantArm = peActionNames[dom]
		}
		if chunkSim >= 0 {
			finalConcSum += float64(concSim)
			finalChunkSum += float64(chunkSim)
		}
		concChangesSum += float64(concChanges)
		chunkChangesSum += float64(chunkChanges)
	}

	n := float64(st.Runs)
	st.TotalDecisionsMean = float64(totalDec) / n
	st.ForcedMean = forcedTotal / n
	st.NormalMean = normalTotal / n
	st.UniqueArmsMean = uniqueSum / n
	st.DominantFractionMean = domFracSum / n
	st.SimConcChangesMean = concChangesSum / n
	st.SimChunkChangesMean = chunkChangesSum / n
	st.SimFinalConcMean = finalConcSum / n
	st.SimFinalChunkMean = finalChunkSum / n
	post := 0
	for _, c := range st.ActionCounts {
		post += c
	}
	st.ActionFracs = map[string]float64{}
	for _, name := range peActionNames {
		if post > 0 {
			st.ActionFracs[name] = float64(st.ActionCounts[name]) / float64(post)
		}
	}
	// dominant arm = most common pooled post-warm-up action
	best, bestN := "", -1
	for _, name := range peActionNames {
		if st.ActionCounts[name] > bestN {
			best, bestN = name, st.ActionCounts[name]
		}
	}
	st.DominantArm = best
	return st, events
}

func actionIndexFromName(name string) ActionIndex {
	for i, n := range peActionNames {
		if n == name {
			return ActionIndex(i)
		}
	}
	return ActionNone
}

// selectPosteriorEvents picks, per scenario, the first forced event on each of
// the concurrency and chunk arms (the arms the lock-in froze), plus a sample.
func selectPosteriorEvents(byScen map[string][]pePosteriorEvent) []pePosteriorEvent {
	order := []string{"baseline", "server-throttle", "latency", "loss", "combined"}
	var out []pePosteriorEvent
	for _, scen := range order {
		evs := byScen[scen]
		sort.Slice(evs, func(i, j int) bool { return evs[i].Decision < evs[j].Decision })
		seen := map[string]int{}
		for _, e := range evs {
			if seen[e.Arm] < 1 {
				seen[e.Arm]++
				out = append(out, e)
			}
		}
	}
	return out
}

func writePeriodicText(t *testing.T, path string, r peReport) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	fmt.Fprintln(f, "LinUCB periodic-exploration diagnostic (SEPARATE; replay of frozen benchmark v3 states)")
	fmt.Fprintf(f, "source: %s\n", r.Source)
	fmt.Fprintf(f, "periodic intervals compared: %v; initial warm-up = %d (InitialExplorationRounds=1)\n", r.Intervals, r.Warmup)
	fmt.Fprintln(f, "NOTE: v3 states are held fixed; this isolates the exploration/posterior mechanism, not download dynamics.")
	fmt.Fprintln(f)
	for _, iv := range r.Intervals {
		fmt.Fprintf(f, "\n=== ExplorationInterval=%d ===\n", iv)
		fmt.Fprintf(f, "  %-16s %7s %7s %7s %7s %7s %7s %7s %7s %7s\n", "scenario", "dec", "normal", "forced", "uniqArm", "domFrac", "concCh", "chunkCh", "finC", "finChunk")
		for _, s := range r.ByInterval[fmt.Sprint(iv)] {
			fmt.Fprintf(f, "  %-16s %7.1f %7.1f %7.1f %7.2f %7.3f %7.1f %7.1f %7.2f %7.1f\n",
				s.Name, s.TotalDecisionsMean, s.NormalMean, s.ForcedMean, s.UniqueArmsMean, s.DominantFractionMean,
				s.SimConcChangesMean, s.SimChunkChangesMean, s.SimFinalConcMean, s.SimFinalChunkMean)
		}
		fmt.Fprintf(f, "  %-16s %s\n", "action counts:", " ")
		for _, s := range r.ByInterval[fmt.Sprint(iv)] {
			fmt.Fprintf(f, "    %-16s dominant=%-20s counts=%v\n", s.Name, s.DominantArm, s.ActionCounts)
			fmt.Fprintf(f, "    %-16s effective=%v noop=%v\n", "", s.PredictedEffective, s.PredictedNoop)
			fmt.Fprintf(f, "    %-16s forcedEffective=%v forcedNoop=%v\n", "", s.ForcedEffective, s.ForcedNoop)
		}
	}
	fmt.Fprintln(f, "\nCOMPARISON (interval 0 vs 5)")
	fmt.Fprintf(f, "  %-16s %-20s %6s %6s | %-20s %6s %6s\n", "scenario", "i0 dominant", "uniq", "incC", "i5 dominant", "uniq", "incC")
	for _, c := range r.Comparison {
		fmt.Fprintf(f, "  %-16s %-20s %6.2f %6d | %-20s %6.2f %6d\n",
			c.Scenario, c.Interval0Dominant, c.Interval0Unique, c.Interval0IncConc, c.Interval5Dominant, c.Interval5Unique, c.Interval5IncConc)
	}
	fmt.Fprintln(f, "\nFORCED-EXPLORATION POSTERIOR EVENTS (arm previously frozen -> updated by forced selection)")
	fmt.Fprintf(f, "  %-16s %4s %5s %-20s %8s %10s %10s %10s %10s %8s %8s\n", "scenario", "run", "dec", "arm", "reward", "meanBefore", "meanAfter", "scoreBefore", "scoreAfter", "A_before", "A_after")
	for _, e := range r.PosteriorEvents {
		fmt.Fprintf(f, "  %-16s %4d %5d %-20s %8.3f %10.3f %10.3f %10.3f %10.3f %8.3f %8.3f\n",
			e.Scenario, e.Run, e.Decision, e.Arm, e.Reward, e.MeanBefore, e.MeanAfter, e.ScoreBefore, e.ScoreAfter, e.ADiagBefore, e.ADiagAfter)
	}
}
