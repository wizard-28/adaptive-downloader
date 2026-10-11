// Command wallclock-throttle runs the experimental server-throttle-wallclock
// scenario: a 4 MiB download whose server rate transitions 512 KiB/s -> 4 MiB/s
// after 1 second of elapsed wall-clock time (independent of request count),
// across all six internal modes. It writes a new dataset and the raw
// server-side request log for diagnostics. It does not modify any frozen
// dataset or production behavior.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"adaptivedl/internal/experiment"
	"adaptivedl/internal/experiment/network"
)

const scenarioName = "server-throttle-wallclock"

// wallclockScenario builds the experimental scenario from the locked v5 base
// configuration, adding only the wall-clock rate schedule and request logging.
func wallclockScenario() experiment.ScenarioConfig {
	cfg := experiment.DefaultScenarioConfig()
	cfg.FileSize = 4194304
	cfg.ChunkSize = 32768
	cfg.MinChunkSize = 32768
	cfg.MaxChunkSize = 131072
	cfg.InitialChunkSize = 32768
	cfg.ChunkSizeStep = 32768
	cfg.InitialConcurrency = 1
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4
	cfg.ImprovementThreshold = 0.2
	cfg.DegradationThreshold = 0.2
	cfg.PolicyWindowSize = 4
	cfg.MaxRetries = 3
	cfg.Timeout = 30 * time.Second
	cfg.LinUCBAlpha = 0.2
	cfg.LinUCBExplorationInterval = 0
	// Fixed rate fields are unused while the schedule is active, but keep them
	// coherent with the schedule.
	cfg.InitialRate = 512 << 10
	cfg.ChangedRate = 4 << 20
	cfg.RateChangeAfterRequests = 0
	cfg.ScheduleInitialRate = 512 << 10
	cfg.ScheduleTransitionAfter = time.Second
	cfg.ScheduleSubsequentRate = 4 << 20
	cfg.EnableRequestLog = true
	return cfg
}

func main() {
	reps := flag.Int("reps", 10, "repetitions per mode")
	out := flag.String("out", filepath.Join("testdata", "benchmark_v6", "matrix_wallclock_throttle_6mode_reps10.json"), "JSON output path")
	flag.Parse()

	mc := experiment.MatrixConfig{
		Scenarios:         []experiment.MatrixScenario{{Name: scenarioName, Config: wallclockScenario()}},
		Repetitions:       *reps,
		Modes:             experiment.AllModesWithBothLinUCB(),
		NetworkController: network.NewController(),
	}
	res, err := experiment.RunMatrix(mc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wallclock-throttle:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	txt := (*out)[:len(*out)-len(filepath.Ext(*out))] + ".txt"
	f, err := os.Create(txt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create txt:", err)
		os.Exit(1)
	}
	defer f.Close()
	res.WriteText(f)
	fmt.Printf("wrote %s and %s\n", *out, txt)
}
