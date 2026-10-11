// Command calibration runs a small, deterministic LinUCB calibration and
// reward-diagnostics experiment. It is DESCRIPTIVE ONLY: it reports reward and
// learner behaviour across a handful of controlled scenarios. It does not rank
// modes, tune reward weights, or make any performance claim.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"adaptivedl/internal/experiment"
	"adaptivedl/internal/rl"
)

func main() {
	reps := flag.Int("reps", 2, "repetitions per calibration scenario")
	alpha := flag.Float64("alpha", 0.2, "LinUCB exploration alpha")
	outDir := flag.String("out", filepath.Join("testdata", "calibration"), "output directory for the JSON report")
	asJSON := flag.Bool("json", false, "print the report as JSON to stdout instead of a text summary")
	flag.Parse()

	rewardCfg := rl.DefaultRewardConfig()
	report, err := experiment.RunCalibration(experiment.CalibrationConfig{
		Scenarios:    experiment.CalibrationScenarios(),
		Repetitions:  *reps,
		Alpha:        *alpha,
		RewardConfig: rewardCfg,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "calibration:", err)
		os.Exit(1)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(os.Stderr, "encode:", err)
			os.Exit(1)
		}
		return
	}

	if err := writeJSON(filepath.Join(*outDir, "calibration.json"), report); err != nil {
		fmt.Fprintln(os.Stderr, "write json:", err)
		os.Exit(1)
	}
	printReport(os.Stdout, report)
	fmt.Printf("\nJSON report written to %s\n", filepath.Join(*outDir, "calibration.json"))
}

func writeJSON(path string, report experiment.CalibrationReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func printReport(w io.Writer, report experiment.CalibrationReport) {
	fmt.Fprintln(w, "LinUCB calibration (diagnostic only; no ranking, no performance claim)")
	fmt.Fprintf(w, "alpha=%.4f\n", report.Alpha)
	printRewardConfig(w, report.RewardConfig)

	for _, sc := range report.Scenarios {
		printScenario(w, sc)
	}
}

func printRewardConfig(w io.Writer, c rl.RewardConfig) {
	fmt.Fprintf(w, "reward config: throughputScale=%.0f latencyScale=%.3f retryPenalty=%.3f failurePenalty=%.3f actionCost=%.3f weights(wT=%.2f wI=%.2f wL=%.2f wR=%.2f wF=%.2f wA=%.2f)\n",
		c.ThroughputScale, c.LatencyScale, c.RetryPenalty, c.FailurePenalty, c.ActionCost,
		c.WThroughput, c.WImprovement, c.WLatency, c.WRetry, c.WFailure, c.WAction)
}

func printScenario(w io.Writer, sc experiment.ScenarioCalibration) {
	p := sc.Pooled
	fmt.Fprintf(w, "\nScenario: %s (reps=%d, decisions=%d, valid=%v, numerically_healthy=%v)\n",
		sc.Name, len(sc.Runs), p.Decisions, p.Valid, p.NumericallyHealthy)
	fmt.Fprintf(w, "  reconstruction_mismatches=%d cumulative_reward=%.4f\n", p.ReconstructionMismatches, p.CumulativeReward)

	fmt.Fprintln(w, "  action distribution:")
	for i := 0; i < rl.NumActions; i++ {
		a := rl.ActionIndex(i)
		fmt.Fprintf(w, "    %-20s %4d  (%.1f%%)\n", a.String(), p.Actions.Count(a), 100*p.Actions.Fraction(a))
	}
	fmt.Fprintf(w, "  unique_actions=%d explored_concurrency=%v explored_chunk_size=%v\n",
		p.Actions.Unique(), p.Actions.ExploredConcurrency(), p.Actions.ExploredChunkSize())

	printRewardStats(w, p.Rewards)
	printComponents(w, p.Components)

	fmt.Fprintf(w, "  churn: transitions=%d changes/decision=%.3f longest_same_run=%d concurrency_changes=%d chunk_size_changes=%d\n",
		p.Churn.ActionTransitions, p.Churn.ChangesPerDecision, p.Churn.LongestSameRun,
		p.Churn.ConcurrencyChanges, p.Churn.ChunkSizeChanges)

	fmt.Fprintf(w, "  early distribution: %s\n", distributionString(p.EarlyActions))
	fmt.Fprintf(w, "  late distribution:  %s\n", distributionString(p.LateActions))
}

func printRewardStats(w io.Writer, s experiment.RewardStats) {
	fmt.Fprintf(w, "  reward: count=%d mean=%.4f median=%.4f min=%.4f max=%.4f sd=%.4f pos/zero/neg=%d/%d/%d\n",
		s.Count, s.Mean, s.Median, s.Min, s.Max, s.StdDev, s.Positive, s.Zero, s.Negative)
}

func printComponents(w io.Writer, c experiment.ComponentStats) {
	fmt.Fprintln(w, "  reward components (mean raw / mean weighted):")
	fmt.Fprintf(w, "    throughput             %.4f / %.4f\n", c.MeanThroughput, c.MeanWeightedThroughput)
	fmt.Fprintf(w, "    throughput_improvement %.4f / %.4f\n", c.MeanThroughputImprovement, c.MeanWeightedThroughputImprovement)
	fmt.Fprintf(w, "    latency_cost           %.4f / %.4f\n", c.MeanLatencyCost, c.MeanWeightedLatencyCost)
	fmt.Fprintf(w, "    retry_cost             %.4f / %.4f\n", c.MeanRetryCost, c.MeanWeightedRetryCost)
	fmt.Fprintf(w, "    failure_cost           %.4f / %.4f\n", c.MeanFailureCost, c.MeanWeightedFailureCost)
	fmt.Fprintf(w, "    action_cost            %.4f / %.4f\n", c.MeanActionCost, c.MeanWeightedActionCost)
}

func distributionString(d experiment.ActionDistribution) string {
	if d.Total == 0 {
		return "(no decisions)"
	}
	out := ""
	for i := 0; i < rl.NumActions; i++ {
		a := rl.ActionIndex(i)
		if d.Count(a) == 0 {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += fmt.Sprintf("%s=%d", a.String(), d.Count(a))
	}
	return out
}
