// Command linucb-diagnostic runs the SEPARATE, LABELED "LinUCB Exploration
// Diagnostic". It forces each action deterministically and measures the reward
// feedback the environment produces for it. It is diagnostic only: it does not
// modify LinUCB, the reward, the deterministic policies, or the server, and it
// never touches the frozen formal benchmark dataset.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"adaptivedl/internal/diagnostics"
	"adaptivedl/internal/experiment/network"
)

func main() {
	reps := flag.Int("reps", 5, "repetitions per (scenario, forced action)")
	out := flag.String("out", filepath.Join("testdata", "diagnostics", "linucb_exploration", "report.json"), "JSON output path")
	useSudo := flag.Bool("sudo", false, "apply tc/netem through sudo -n (loss scenario)")
	withLoss := flag.Bool("loss", true, "include the loss scenario (requires tc/netem)")
	flag.Parse()

	scenarios := diagnostics.FormalScenarios()
	if !*withLoss {
		var kept []diagnostics.Scenario
		for _, s := range scenarios {
			if s.LossPercent == 0 {
				kept = append(kept, s)
			}
		}
		scenarios = kept
	}

	cfg := diagnostics.Config{Scenarios: scenarios, Repetitions: *reps}
	if *useSudo {
		cfg.NetworkController = &network.Controller{Prefix: []string{"sudo", "-n"}}
	} else {
		cfg.NetworkController = network.NewController()
	}

	report, err := diagnostics.Run(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "linucb-diagnostic:", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write json:", err)
		os.Exit(1)
	}

	txtPath := (*out)[:len(*out)-len(filepath.Ext(*out))] + ".txt"
	f, err := os.Create(txtPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create txt:", err)
		os.Exit(1)
	}
	defer f.Close()
	writeText(f, report)
	fmt.Printf("wrote %s and %s\n", *out, txtPath)
}

func writeText(f *os.File, r diagnostics.Report) {
	fmt.Fprintln(f, "LinUCB Exploration Diagnostic (SEPARATE from the formal benchmark; descriptive only)")
	fmt.Fprintf(f, "alpha=%.3f reps=%d reward=DefaultRewardConfig (aggregate-throughput based)\n", r.Alpha, r.Repetitions)
	fmt.Fprintln(f, "purpose: force each action and measure whether the environment produces distinguishable reward feedback")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "INITIAL STATE (first decision, concurrency=1) and initial LinUCB score")
	fmt.Fprintln(f, "  all actions tie at alpha*||x|| at initialization (A=I, b=0); lowest index (none) wins the tie-break")
	for _, sc := range r.Scenarios {
		if sc.InitState == nil {
			continue
		}
		is := sc.InitState
		fmt.Fprintf(f, "  %-16s netem=%-11s throughput=%.0f agg=%.0f dur=%v bytes=%d ||x||=%.6f initial_score=%.6f\n",
			sc.Name, sc.Netem, is.Throughput, is.AggregateThroughput, is.RequestDuration, is.BytesTransferred,
			is.FeatureNorm, is.InitialScore)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "FORCED-ACTION FEEDBACK (mean over decisions; per-request Throughput vs wall-clock AggregateThroughput)")
	for _, sc := range r.Scenarios {
		fmt.Fprintf(f, "\n== %s (netem=%s) ==\n", sc.Name, sc.Netem)
		fmt.Fprintf(f, "  %-22s %8s %8s %9s %9s %9s %9s %8s %8s %8s %8s %7s %7s\n",
			"action", "reward", "first", "thrΔ", "aggΔ", "latency", "impr", "retry", "fail", "action", "overlap", "finalC", "chg")
		mism := 0
		for _, a := range sc.Actions {
			mism += a.ReconMismatches
			fmt.Fprintf(f, "  %-22s %8.4f %8.4f %9.0f %9.0f %9.5f %9.5f %8.5f %8.5f %8.5f %8d %7.2f %7d\n",
				a.Action, a.RewardMean, a.FirstReward, a.DeltaThroughput, a.DeltaAggregate,
				a.Latency, a.Improvement, a.Retry, a.Failure, a.ActionCost, a.OverlapDecisions,
				a.FinalConcurrency, a.ControllerChanged)
		}
		fmt.Fprintf(f, "  recon_mismatches=%d (reward reconstruction vs runner; must be 0)\n", mism)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "Notes: reward mean includes the absolute aggregate-throughput benefit; 'first' is the decision-0 reward")
	fmt.Fprintln(f, "(immediate credit); thrΔ/aggΔ are mean per-decision changes; action=mean ActionCost. No ranking.")
}
