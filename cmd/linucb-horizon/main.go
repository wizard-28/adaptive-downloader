// Command linucb-horizon runs a SEPARATE, LABELED LinUCB learning-horizon study.
// It varies only the file size (the decision budget) while holding every other
// DefaultMatrixConfig parameter fixed, and reports how many post-warm-up
// decisions each candidate horizon provides. It does not tune the learner and
// does not touch any frozen benchmark dataset.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"adaptivedl/internal/diagnostics"
	"adaptivedl/internal/experiment"
	"adaptivedl/internal/experiment/network"
)

func main() {
	sizes := flag.String("sizes", "262144,524288,1048576,2097152", "comma-separated candidate file sizes (bytes)")
	scenarios := flag.String("scenarios", "baseline,server-throttle,latency,combined", "comma-separated DefaultMatrixConfig scenario names")
	modesFlag := flag.String("modes", "linucb,fixed", "comma-separated modes")
	reps := flag.Int("reps", 3, "repetitions per (file size, scenario, mode)")
	useSudo := flag.Bool("sudo", false, "apply tc/netem through sudo -n")
	out := flag.String("out", filepath.Join("testdata", "diagnostics", "linucb_horizon", "report.json"), "JSON output path")
	flag.Parse()

	fileSizes, err := parseSizes(*sizes)
	if err != nil {
		fail(err)
	}
	modes, err := parseModes(*modesFlag)
	if err != nil {
		fail(err)
	}

	cfg := diagnostics.HorizonConfig{
		FileSizes:   fileSizes,
		Scenarios:   splitCSV(*scenarios),
		Modes:       modes,
		Repetitions: *reps,
	}
	if *useSudo {
		cfg.NetworkController = &network.Controller{Prefix: []string{"sudo", "-n"}}
	} else {
		cfg.NetworkController = network.NewController()
	}

	report, err := diagnostics.RunHorizonStudy(cfg)
	if err != nil {
		fail(err)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fail(err)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fail(err)
	}

	txtPath := (*out)[:len(*out)-len(filepath.Ext(*out))] + ".txt"
	f, err := os.Create(txtPath)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	writeText(f, report)
	fmt.Printf("wrote %s and %s\n", *out, txtPath)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "linucb-horizon:", err)
	os.Exit(1)
}

func writeText(f *os.File, r diagnostics.HorizonReport) {
	fmt.Fprintln(f, "LinUCB learning-horizon study (SEPARATE diagnostic; descriptive only)")
	fmt.Fprintf(f, "warm-up = %d fixed selections (InitialExplorationRounds=1 x NumActions=5); reps=%d\n", diagnostics.WarmupSelections, r.Repetitions)
	fmt.Fprintln(f, "only the file size varies; all other DefaultMatrixConfig parameters are unchanged")
	fmt.Fprintln(f, "file-size in bytes; durations in ms; wallclock in B/s")
	fmt.Fprintln(f)
	fmt.Fprintf(f, "%-18s %-12s %9s %6s %6s %6s %5s %5s %6s %6s %6s %8s %8s\n",
		"scenario", "mode", "fileSize", "total", "warm", "post", "uniq", "cChg", "kChg", "finC", "finK", "durMs", "wallBps")
	for _, a := range r.Aggregates {
		fmt.Fprintf(f, "%-18s %-12s %9d %6.1f %6.1f %6.1f %5.1f %5.1f %6.0f %6.2f %6.0f %8.1f %8.0f\n",
			a.Scenario, a.Mode, a.FileSize, a.MeanTotal, a.MeanWarmup, a.MeanPost, a.MeanUniquePost,
			a.MeanConcChanges, a.MeanChunkChanges, a.MeanFinalConc, a.MeanFinalChunk, a.MeanDurationMs, a.MeanWallclockBps)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "post-warm-up action counts (pooled over runs):")
	for _, a := range r.Aggregates {
		fmt.Fprintf(f, "  %-18s %-12s size=%-9d total=%.1f warm=%.1f post=%.1f postRange=%d..%d counts=%v\n",
			a.Scenario, a.Mode, a.FileSize, a.MeanTotal, a.MeanWarmup, a.MeanPost, a.MinPost, a.MaxPost, a.PostCounts)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "valid runs (Err=='' && output/range correctness):")
	for _, a := range r.Aggregates {
		fmt.Fprintf(f, "  %-18s %-12s size=%-9d valid=%d/%d\n", a.Scenario, a.Mode, a.FileSize, a.ValidRuns, a.Runs)
	}
}

func parseSizes(s string) ([]int64, error) {
	var out []int64
	for _, raw := range splitCSV(s) {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("invalid file size %q", raw)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no file sizes given")
	}
	return out, nil
}

func parseModes(s string) ([]experiment.Mode, error) {
	var out []experiment.Mode
	for _, raw := range splitCSV(s) {
		m := experiment.Mode(raw)
		switch m {
		case experiment.ModeFixed, experiment.ModeConcurrencyAdaptive, experiment.ModeChunkSizeAdaptive, experiment.ModeJointAdaptive, experiment.ModeLinUCB:
			out = append(out, m)
		default:
			return nil, fmt.Errorf("unknown mode %q", raw)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no modes given")
	}
	return out, nil
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
