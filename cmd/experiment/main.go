// Command experiment runs the controlled four-mode comparison (fixed,
// concurrency-adaptive, chunk-size-adaptive, joint-adaptive) and prints the raw
// per-run results and aggregate summaries. It does not rank the modes.
//
// Each mode runs inside the same scenario: the same server model, rate schedule,
// fault injection and file size, with a fresh output and fresh policy state per
// run, in randomised order across repetitions. That is what makes a per-run time
// ratio meaningful. The opt-in learning modes (linucb, linucb-periodic) are not
// part of AllModes() and must be selected explicitly.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"adaptivedl/internal/experiment"
	"adaptivedl/internal/experiment/network"
)

func main() {
	def := experiment.DefaultScenarioConfig()

	reps := flag.Int("reps", def.Repetitions, "number of runs per mode")
	fileSize := flag.Int64("file-size", def.FileSize, "file size in bytes (also overrides every matrix scenario's file size when -matrix is used; the default matches DefaultMatrixConfig)")
	chunk := flag.Int64("chunk", def.ChunkSize, "chunk size in bytes")
	minChunk := flag.Int64("min-chunk", def.MinChunkSize, "minimum chunk size in bytes")
	maxChunk := flag.Int64("max-chunk", def.MaxChunkSize, "maximum chunk size in bytes")
	initialChunk := flag.Int64("initial-chunk", def.InitialChunkSize, "initial chunk size in bytes")
	chunkStep := flag.Int64("chunk-step", def.ChunkSizeStep, "chunk-size step in bytes")
	initialRate := flag.Int64("initial-rate", def.InitialRate, "server rate before the transition (bytes/sec)")
	changedRate := flag.Int64("changed-rate", def.ChangedRate, "server rate after the transition (bytes/sec)")
	switchAfter := flag.Int64("switch-after", def.RateChangeAfterRequests, "switch rate after this many requests")
	concurrency := flag.Int("concurrency", def.InitialConcurrency, "initial concurrency")
	minC := flag.Int("min-concurrency", def.MinConcurrency, "minimum concurrency")
	maxC := flag.Int("max-concurrency", def.MaxConcurrency, "maximum concurrency")
	improve := flag.Float64("improve", def.ImprovementThreshold, "policy improvement threshold (relative)")
	degrade := flag.Float64("degrade", def.DegradationThreshold, "policy degradation threshold (relative)")
	window := flag.Int("window", def.PolicyWindowSize, "policy recent-throughput window (attempts)")
	retries := flag.Int("retries", def.MaxRetries, "max retries per range")
	timeout := flag.Duration("timeout", def.Timeout, "per-request HTTP timeout")
	alpha := flag.Float64("linucb-alpha", def.LinUCBAlpha, "LinUCB exploration alpha (linucb mode only)")
	linucbInterval := flag.Int("linucb-exploration-interval", def.LinUCBExplorationInterval, "LinUCB periodic exploration interval: 0=off, N=force one action per N normal decisions (linucb mode only; also applied to matrix scenarios)")
	asJSON := flag.Bool("json", false, "print results as JSON")
	matrix := flag.Bool("matrix", false, "run the baseline characterization matrix")
	useSudo := flag.Bool("sudo", false, "run tc/netem through sudo -n (matrix only)")
	modes := flag.String("modes", "", "comma-separated modes to run (default: the four baseline modes; add 'linucb' to opt in)")
	flag.Parse()

	selectedModes, err := parseModes(*modes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "experiment:", err)
		os.Exit(2)
	}

	if *matrix {
		runMatrix(*reps, *asJSON, *useSudo, selectedModes, *fileSize, *linucbInterval)
		return
	}

	cfg := experiment.ScenarioConfig{
		FileSize:                  *fileSize,
		ChunkSize:                 *chunk,
		InitialConcurrency:        *concurrency,
		MinConcurrency:            *minC,
		MaxConcurrency:            *maxC,
		InitialRate:               *initialRate,
		ChangedRate:               *changedRate,
		RateChangeAfterRequests:   *switchAfter,
		MinChunkSize:              *minChunk,
		MaxChunkSize:              *maxChunk,
		InitialChunkSize:          *initialChunk,
		ChunkSizeStep:             *chunkStep,
		ImprovementThreshold:      *improve,
		DegradationThreshold:      *degrade,
		PolicyWindowSize:          *window,
		MaxRetries:                *retries,
		Timeout:                   *timeout,
		Repetitions:               *reps,
		LinUCBAlpha:               *alpha,
		LinUCBExplorationInterval: *linucbInterval,
	}

	res, err := experiment.RunSelected(cfg, selectedModes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "experiment:", err)
		os.Exit(1)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintln(os.Stderr, "encode:", err)
			os.Exit(1)
		}
		return
	}
	printText(res)
}

func runMatrix(reps int, asJSON, useSudo bool, modes []experiment.Mode, fileSize int64, linucbInterval int) {
	mc := experiment.DefaultMatrixConfig()
	mc.Repetitions = reps
	mc.Modes = modes
	applyFileSizeOverride(&mc, fileSize)
	applyLinUCBIntervalOverride(&mc, linucbInterval)
	if useSudo {
		mc.NetworkController = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}

	res, err := experiment.RunMatrix(mc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "matrix:", err)
		os.Exit(1)
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintln(os.Stderr, "encode:", err)
			os.Exit(1)
		}
		return
	}
	res.WriteText(os.Stdout)
}

// applyFileSizeOverride sets every matrix scenario's FileSize to fileSize when
// fileSize > 0. This is an explicit experiment-level override (for example the
// 4 MiB learning horizon) and leaves DefaultMatrixConfig's own default
// untouched; fileSize <= 0 is a no-op.
func applyFileSizeOverride(mc *experiment.MatrixConfig, fileSize int64) {
	if fileSize <= 0 {
		return
	}
	for i := range mc.Scenarios {
		mc.Scenarios[i].Config.FileSize = fileSize
	}
}

// applyLinUCBIntervalOverride sets every matrix scenario's
// LinUCBExplorationInterval. It is always applied (0 is the locked default), so
// the matrix behavior is unchanged unless the flag is set explicitly.
func applyLinUCBIntervalOverride(mc *experiment.MatrixConfig, interval int) {
	for i := range mc.Scenarios {
		mc.Scenarios[i].Config.LinUCBExplorationInterval = interval
	}
}

func printText(res experiment.ExperimentResult) {
	c := res.Config
	fmt.Printf("scenario: file=%d chunk=%d (initial %d step %d min %d max %d) concurrency=%d..%d rates=%d->%d after=%d reps=%d\n",
		c.FileSize, c.ChunkSize, c.InitialChunkSize, c.ChunkSizeStep, c.MinChunkSize, c.MaxChunkSize,
		c.MinConcurrency, c.MaxConcurrency,
		c.InitialRate, c.ChangedRate, c.RateChangeAfterRequests, c.Repetitions)

	modes := modesWithRuns(res)
	for _, mode := range modes {
		printRuns(string(mode), res.Runs(mode))
	}

	s := res.Summary()
	fmt.Println("summary (aggregate of individual runs):")
	for _, mode := range modes {
		printSummary(string(mode), s.Aggregate(mode))
	}
}

// modesWithRuns returns the modes that produced runs, in canonical order.
func modesWithRuns(res experiment.ExperimentResult) []experiment.Mode {
	var modes []experiment.Mode
	for _, mode := range experiment.AllModesWithBothLinUCB() {
		if len(res.Runs(mode)) > 0 {
			modes = append(modes, mode)
		}
	}
	if len(modes) == 0 {
		return experiment.AllModes()
	}
	return modes
}

// parseModes parses a comma-separated mode list. An empty string means the four
// baseline modes. "linucb" opts into the learning mode. Unknown names error.
func parseModes(spec string) ([]experiment.Mode, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return experiment.AllModes(), nil
	}
	var modes []experiment.Mode
	seen := make(map[experiment.Mode]bool)
	for _, raw := range strings.Split(spec, ",") {
		name := experiment.Mode(strings.TrimSpace(raw))
		switch name {
		case experiment.ModeFixed, experiment.ModeConcurrencyAdaptive,
			experiment.ModeChunkSizeAdaptive, experiment.ModeJointAdaptive,
			experiment.ModeLinUCB, experiment.ModeLinUCBPeriodic:
		default:
			return nil, fmt.Errorf("unknown mode %q", raw)
		}
		if !seen[name] {
			seen[name] = true
			modes = append(modes, name)
		}
	}
	if len(modes) == 0 {
		return experiment.AllModes(), nil
	}
	return modes, nil
}

func printRuns(name string, runs []experiment.RunResult) {
	fmt.Printf("%s runs:\n", name)
	for i, r := range runs {
		fmt.Printf("  [%d] duration=%v attempts=%d success=%d failed=%d retries=%d bytes=%d throughput(avg/min/max)=%.0f/%.0f/%.0f concurrency=%d->%d(max %d) chunk=%d->%d(min %d max %d) output_ok=%v no_dup=%v no_missing=%v",
			i, r.Duration, r.TotalAttempts, r.SuccessfulAttempts, r.FailedAttempts, r.RetryAttempts, r.TransferredBytes,
			r.AverageThroughput, r.MinThroughput, r.MaxThroughput,
			r.InitialConcurrency, r.FinalConcurrency, r.MaxConcurrency,
			r.InitialChunkSize, r.FinalChunkSize, r.MinChunkSize, r.MaxChunkSize,
			r.OutputMatches, r.NoDuplicateRanges, r.NoMissingRanges)
		if r.Err != "" {
			fmt.Printf(" err=%q", r.Err)
		}
		fmt.Println()
	}
}

func printSummary(name string, s experiment.ModeAggregate) {
	fmt.Printf("  %-20s runs=%d valid=%d failed=%d net-unavailable=%d mean-duration=%v (median %v sd %v iqr %v min %v max %v) mean-throughput=%.0f (median %.0f sd %.0f iqr %.0f) mean-attempts=%.1f mean-retries=%.2f mean-final-concurrency=%.2f mean-max-concurrency=%.2f mean-final-chunk=%.0f mean-min-chunk=%.0f mean-max-chunk=%.0f mean-conc-actions=%.2f mean-chunk-actions=%.2f\n",
		name, s.Runs, s.ValidRuns, s.FailedRuns, s.NetworkUnavailableRuns,
		s.MeanDuration, s.MedianDuration, s.StdDevDuration, s.IQRDuration, s.MinDuration, s.MaxDuration,
		s.MeanThroughput, s.MedianThroughput, s.StdDevThroughput, s.IQRThroughput,
		s.MeanAttempts, s.MeanRetries,
		s.MeanFinalConcurrency, s.MeanMaxConcurrency,
		s.MeanFinalChunkSize, s.MeanMinChunkSize, s.MeanMaxChunkSize,
		s.MeanConcurrencyActions, s.MeanChunkSizeActions)
}
