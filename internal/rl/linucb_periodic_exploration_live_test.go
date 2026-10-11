package rl

// Posterior evidence for the live periodic-exploration benchmark (benchmark v4).
// Replays the recorded per-decision states/rewards of the live periodic and
// standard LinUCB runs through the production LinUCB, validates that the replay
// reproduces the recorded action sequence, and measures whether forced
// exploratory actions change the forced arm's UCB score/rank. Writes
// testdata/diagnostics/linucb_periodic_exploration_live/report.{json,txt}.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

type liveEvent struct {
	Scenario    string  `json:"scenario"`
	Decision    int     `json:"decision"`
	Arm         string  `json:"arm"`
	Reward      float64 `json:"reward"`
	ScoreBefore float64 `json:"score_before"`
	ScoreAfter  float64 `json:"score_after"`
	RankBefore  int     `json:"rank_before"`
	RankAfter   int     `json:"rank_after"`
}

type liveScenario struct {
	Name             string  `json:"name"`
	Runs             int     `json:"runs"`
	Decisions        int     `json:"decisions"`
	ForcedEvents     int     `json:"forced_events"`
	MeanScoreDelta   float64 `json:"mean_score_delta"`
	FracScoreUp      float64 `json:"fraction_score_increased"`
	FracRankImproved float64 `json:"fraction_rank_improved"`
	BestRankReached  int     `json:"best_rank_reached"`
	RecordedMatch    int     `json:"recorded_action_matches"`
	RecordedTotal    int     `json:"recorded_action_total"`
}

type liveReport struct {
	Source        string         `json:"source"`
	StandardMatch int            `json:"standard_recorded_matches"`
	StandardTotal int            `json:"standard_recorded_total"`
	Scenarios     []liveScenario `json:"periodic_by_scenario"`
	Events        []liveEvent    `json:"forced_events_sample"`
}

func refRank(scores []float64, arm int) int {
	r := 1
	for i, v := range scores {
		if i == arm {
			continue
		}
		if v > scores[arm] {
			r++
		}
	}
	return r
}

func replayLive(t *testing.T, rawPath string, interval int) (map[string]*liveScenario, int, int, []liveEvent) {
	t.Helper()
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read %s: %v", rawPath, err)
	}
	var m v3Matrix
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", rawPath, err)
	}
	calc, err := NewRewardCalculator(DefaultRewardConfig())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*liveScenario{}
	var events []liveEvent
	match, total := 0, 0
	for _, sc := range m.Scenarios {
		ls := &liveScenario{Name: sc.Name}
		for runIdx, r := range sc.LinUCB {
			ls.Runs++
			l, err := NewLinUCB(LinUCBConfig{Alpha: 0.2, NumActions: NumActions, Dimension: len(FeatureNames()), InitialExplorationRounds: 1, ExplorationInterval: interval})
			if err != nil {
				t.Fatal(err)
			}
			var prev State
			var prevA ActionIndex
			have := false
			var pending *liveEvent
			for i, d := range r.Decisions {
				ls.Decisions++
				total++
				s := stateFromV3(d)
				if have {
					rv := calc.Calculate(prev, s, prevA, true).Value
					l.Update(prev, prevA, Reward{Value: rv})
				}
				x, err := s.Features(defaultFeatureConfig())
				if err != nil {
					t.Fatal(err)
				}
				scores := make([]float64, NumActions)
				alpha := l.alpha
				for a := 0; a < NumActions; a++ {
					scores[a] = l.score(a, x)
					l.alpha = 0
					_ = l.score(a, x)
					l.alpha = alpha
				}
				if pending != nil {
					pending.ScoreAfter = scores[actionIndexFromName(pending.Arm)]
					pending.RankAfter = refRank(scores, int(actionIndexFromName(pending.Arm)))
					events = append(events, *pending)
					pending = nil
				}
				cursorBefore := l.periodicCursor
				sel := l.SelectAction(s)
				if int(sel) == d.ActionIndex {
					match++
					ls.RecordedMatch++
				}
				ls.RecordedTotal++
				if interval > 0 && i >= lockinWarmup && l.periodicCursor != cursorBefore {
					pending = &liveEvent{
						Scenario: sc.Name, Decision: i, Arm: peActionNames[sel], Reward: d.Reward,
						ScoreBefore: scores[sel], RankBefore: refRank(scores, int(sel)),
					}
					_ = runIdx
				}
				prev, prevA, have = s, sel, true
			}
		}
		out[sc.Name] = ls
	}
	return out, match, total, events
}

func TestLinUCBPeriodicExplorationLiveDiagnostic(t *testing.T) {
	root := findRepoRoot(t)
	perPath := filepath.Join(root, "testdata", "benchmark_v4", "matrix_linucb_periodic_reps10.json")
	stdPath := filepath.Join(root, "testdata", "benchmark_v4", "matrix_linucb_standard_reps10.json")

	per, _, _, events := replayLive(t, perPath, 5)
	_, sMatch, sTotal, _ := replayLive(t, stdPath, 0)
	if sMatch != sTotal {
		t.Fatalf("standard replay mismatch: %d/%d", sMatch, sTotal)
	}

	// Aggregate forced-event evidence per scenario.
	byScen := map[string][]liveEvent{}
	for _, e := range events {
		byScen[e.Scenario] = append(byScen[e.Scenario], e)
	}
	var scenOut []liveScenario
	for _, name := range []string{"baseline", "server-throttle", "latency", "loss", "combined"} {
		ls := per[name]
		evs := byScen[name]
		ls.ForcedEvents = len(evs)
		up, improved := 0, 0
		best := 99
		var dsum float64
		for _, e := range evs {
			dsum += e.ScoreAfter - e.ScoreBefore
			if e.ScoreAfter > e.ScoreBefore {
				up++
			}
			if e.RankAfter < e.RankBefore {
				improved++
			}
			if e.RankAfter < best {
				best = e.RankAfter
			}
		}
		if len(evs) > 0 {
			ls.MeanScoreDelta = dsum / float64(len(evs))
			ls.FracScoreUp = float64(up) / float64(len(evs))
			ls.FracRankImproved = float64(improved) / float64(len(evs))
		}
		if best == 99 {
			best = 0
		}
		ls.BestRankReached = best
		scenOut = append(scenOut, *ls)
	}
	// Sample: for loss, the first forced increase-concurrency event.
	var sample []liveEvent
	for _, e := range events {
		if e.Scenario == "loss" && e.Arm == "increase-concurrency" {
			sample = append(sample, e)
			if len(sample) >= 1 {
				break
			}
		}
	}
	sort.Slice(sample, func(i, j int) bool { return sample[i].Decision < sample[j].Decision })

	rep := liveReport{Source: "testdata/benchmark_v4/matrix_linucb_periodic_reps10.json", StandardMatch: sMatch, StandardTotal: sTotal, Scenarios: scenOut, Events: sample}
	dir := filepath.Join(root, "testdata", "diagnostics", "linucb_periodic_exploration_live")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "report.json"), rep)
	writeLiveText(t, filepath.Join(dir, "report.txt"), rep)
}

func writeLiveText(t *testing.T, path string, r liveReport) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintln(f, "Live periodic-exploration posterior replay (frozen benchmark v4)")
	fmt.Fprintf(f, "source: %s\n", r.Source)
	fmt.Fprintf(f, "standard replay action match: %d/%d (validates reconstruction)\n", r.StandardMatch, r.StandardTotal)
	fmt.Fprintln(f, "Forced exploratory actions: does the forced arm's UCB score/rank move?")
	fmt.Fprintf(f, "  %-16s %6s %7s %9s %9s %11s %9s %8s\n", "scenario", "runs", "events", "dScore", "fracUp", "fracRankUp", "bestRank", "recon")
	for _, s := range r.Scenarios {
		fmt.Fprintf(f, "  %-16s %6d %7d %9.4f %9.3f %11.3f %9d %8d/%d\n",
			s.Name, s.Runs, s.ForcedEvents, s.MeanScoreDelta, s.FracScoreUp, s.FracRankImproved, s.BestRankReached, s.RecordedMatch, s.RecordedTotal)
	}
	fmt.Fprintln(f, "Sample forced events (loss, increase-concurrency):")
	for _, e := range r.Events {
		fmt.Fprintf(f, "  decision %d arm=%s score %.4f -> %.4f rank %d -> %d reward=%.3f\n", e.Decision, e.Arm, e.ScoreBefore, e.ScoreAfter, e.RankBefore, e.RankAfter, e.Reward)
	}
}
