// Command tau-injection runs the pre-registered Stage 0 transition-cost
// (tau) switch-injection measurements and applies the frozen tau aggregation.
//
// Modes:
//
//	parent (default): run one switch-injection replay (source C until the
//	  preselected trace-time epoch boundary, then force the target C) and
//	  truncate the run right after the measured 120 s window via the watchdog.
//	-child: the in-namespace downloader child.
//	-plan: print the frozen native-regime classification and preselected
//	  injection timestamps for one trace (deterministic; no replay).
//	-aggregate FILE: apply the frozen tau aggregation to an observations JSONL.
//
// It never computes headroom and never modifies the formal fixed-C pilot.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"adaptivedl/internal/experiment/replay"
	"adaptivedl/internal/experiment/tau"
)

func main() {
	var (
		child        = flag.Bool("child", false, "internal: run as the mm-link downloader child")
		port         = flag.Int("port", 0, "internal (child): controlled-server port")
		output       = flag.String("output", "", "internal (child): downloader output path")
		summary      = flag.String("summary", "", "internal (child): child summary path")
		events       = flag.String("events", "", "internal (child): event timeline path")
		up           = flag.String("up", "", "uplink Mahimahi trace file")
		down         = flag.String("down", "", "downlink Mahimahi trace file")
		traceID      = flag.String("trace-id", "", "trace identifier for artifacts/plan")
		sourceC      = flag.Int("c", 1, "source/initial concurrency")
		switchAt     = flag.Duration("switch-at", 0, "child-elapsed switch time (0 = no switch)")
		switchTo     = flag.Int("switch-to", 0, "target concurrency after the switch")
		window       = flag.Duration("window", tau.EpochDuration, "measured dwell window (one epoch)")
		margin       = flag.Duration("margin", 15*time.Second, "watchdog margin after the window")
		watchdog     = flag.Duration("watchdog", 0, "hard watchdog (0 = switch-at + window + margin)")
		workloadSize = flag.Int64("workload-size", 64<<30, "logical workload size in bytes")
		chunkSize    = flag.Int64("chunk", 1<<20, "fixed range chunk size in bytes")
		timeout      = flag.String("timeout", "none", `per-request deadline: "none" or a positive duration`)
		dialTimeout  = flag.String("dial-timeout", "none", `connection dial deadline: "none" or a positive duration`)
		minConc      = flag.Int("min-concurrency", 1, "adaptive concurrency minimum")
		maxConc      = flag.Int("max-concurrency", 16, "adaptive concurrency maximum")
		outputDir    = flag.String("output-dir", "", "run output directory")
		plan         = flag.Bool("plan", false, "print the frozen regime classification and injection plan and exit")
		aggregate    = flag.String("aggregate", "", "apply the frozen tau aggregation to an observations JSONL and exit")
		measure      = flag.String("measure", "", "measure window bytes from an events JSONL and exit")
		measureStart = flag.Duration("measure-start", 0, "window start (trace time)")
		measureEnd   = flag.Duration("measure-end", tau.EpochDuration, "window end (trace time, exclusive)")
		traceStart   = flag.Int64("trace-start-wall", 0, "frozen anchor origin (mm-link launch wall + StartupOffset)")
		threshold    = flag.Float64("threshold", tau.RegimeDependenceThreshold, "frozen regime-dependence threshold")
	)
	flag.Parse()

	if *aggregate != "" {
		if err := runAggregate(*aggregate, *threshold); err != nil {
			fmt.Fprintln(os.Stderr, "tau-injection:", err)
			os.Exit(2)
		}
		return
	}

	if *measure != "" {
		if *traceStart == 0 {
			fmt.Fprintln(os.Stderr, "tau-injection: -trace-start-wall is required for -measure")
			os.Exit(2)
		}
		if err := runMeasure(*measure, *traceStart, *measureStart, *measureEnd); err != nil {
			fmt.Fprintln(os.Stderr, "tau-injection:", err)
			os.Exit(2)
		}
		return
	}

	if *plan {
		if err := runPlan(*down, *traceID); err != nil {
			fmt.Fprintln(os.Stderr, "tau-injection:", err)
			os.Exit(2)
		}
		return
	}

	requestTimeout, noRequestTimeout, err := replay.ParseDeadline(*timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tau-injection:", err)
		os.Exit(2)
	}
	dialTimeoutValue, noDialTimeout, err := replay.ParseDeadline(*dialTimeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tau-injection:", err)
		os.Exit(2)
	}

	if *child {
		// The child self-watchdog is deliberately later than the parent
		// watchdog so the parent truncation always wins and the child is
		// canceled gracefully (non-fatal), never self-deadline-fatal.
		childWatchdog := *watchdog + 2*time.Minute
		_, err := replay.RunDownloaderChild(context.Background(), replay.ChildConfig{
			Port:             *port,
			OutputPath:       *output,
			SummaryPath:      *summary,
			EventsPath:       *events,
			TotalSize:        *workloadSize,
			ChunkSize:        *chunkSize,
			Concurrency:      *sourceC,
			Timeout:          requestTimeout,
			NoRequestTimeout: noRequestTimeout,
			DialTimeout:      dialTimeoutValue,
			NoDialTimeout:    noDialTimeout,
			MinConcurrency:   *minConc,
			MaxConcurrency:   *maxConc,
			SwitchAt:         *switchAt,
			SwitchTo:         *switchTo,
			MaxRuntime:       childWatchdog,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "tau-injection child:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if *up == "" || *down == "" {
		fmt.Fprintln(os.Stderr, "tau-injection: -up and -down are required")
		os.Exit(2)
	}
	if *outputDir == "" {
		fmt.Fprintln(os.Stderr, "tau-injection: -output-dir is required")
		os.Exit(2)
	}
	if *window <= 0 || *margin < 0 {
		fmt.Fprintln(os.Stderr, "tau-injection: window must be positive and margin non-negative")
		os.Exit(2)
	}
	if *switchAt < 0 {
		fmt.Fprintln(os.Stderr, "tau-injection: switch-at must be >= 0")
		os.Exit(2)
	}
	if *switchAt > 0 && (*switchTo < *minConc || *switchTo > *maxConc) {
		fmt.Fprintln(os.Stderr, "tau-injection: switch-to outside adaptive bounds")
		os.Exit(2)
	}
	if *watchdog <= 0 {
		*watchdog = *switchAt + *window + *margin
	}
	if *watchdog <= *switchAt+*window {
		fmt.Fprintln(os.Stderr, "tau-injection: watchdog must exceed switch-at + window")
		os.Exit(2)
	}
	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "tau-injection:", err)
		os.Exit(2)
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tau-injection:", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	eventsPath := filepath.Join(*outputDir, "events.jsonl")
	summaryPath := filepath.Join(*outputDir, "summary.json")
	outputPath := filepath.Join(*outputDir, "output.bin")
	childArgv := []string{
		exe, "-child",
		"-port", "PORT",
		"-output", outputPath,
		"-summary", summaryPath,
		"-events", eventsPath,
		"-workload-size", strconv.FormatInt(*workloadSize, 10),
		"-chunk", strconv.FormatInt(*chunkSize, 10),
		"-c", strconv.Itoa(*sourceC),
		"-min-concurrency", strconv.Itoa(*minConc),
		"-max-concurrency", strconv.Itoa(*maxConc),
		"-switch-at", switchAt.String(),
		"-switch-to", strconv.Itoa(*switchTo),
		"-timeout", replay.DeadlineString(requestTimeout, noRequestTimeout),
		"-dial-timeout", replay.DeadlineString(dialTimeoutValue, noDialTimeout),
		"-watchdog", watchdog.String(),
	}
	res, runErr := replay.Run(ctx, replay.Config{
		UpTrace:          *up,
		DownTrace:        *down,
		WorkloadSize:     *workloadSize,
		ChunkSize:        *chunkSize,
		Concurrency:      *sourceC,
		Timeout:          requestTimeout,
		MaxRuntime:       *watchdog,
		TerminationGrace: 5 * time.Second,
		ServerAddr:       "0.0.0.0:0",
		SummaryPath:      summaryPath,
		UplinkLogPath:    filepath.Join(*outputDir, "up.log"),
		DownlinkLogPath:  filepath.Join(*outputDir, "down.log"),
		Log:              os.Stderr,
		Child: func(port int) ([]string, []string) {
			argv := append([]string(nil), childArgv...)
			for i, a := range argv {
				if a == "PORT" {
					argv[i] = strconv.Itoa(port)
				}
			}
			return argv, nil
		},
	})

	record := runRecord{
		TraceID:     *traceID,
		UpTrace:     *up,
		DownTrace:   *down,
		SourceC:     *sourceC,
		TargetC:     *switchTo,
		SwitchAtNs:  int64(*switchAt),
		WindowNs:    int64(*window),
		WatchdogNs:  int64(*watchdog),
		Outcome:     string(res.Outcome),
		Err:         errString(runErr),
		Port:        res.Port,
		TraceDurNs:  int64(res.TraceDuration),
		MMLinkExit:  res.MMLinkExit,
		ChildStatus: string(res.ChildStatus),
		ChildError:  res.ChildError,
	}
	if res.Anchor != nil {
		record.LaunchWallUnixNano = res.Anchor.LaunchParentMono.UnixNano()
		record.ExitWallUnixNano = res.Anchor.ExitParentMono.UnixNano()
		record.StartupOffsetNs = int64(res.Anchor.StartupOffset)
		record.TraceStartWallUnixNano = res.Anchor.LaunchParentMono.UnixNano() + int64(res.Anchor.StartupOffset)
	}
	if res.ChildSummary != nil {
		record.SwitchApplied = res.ChildSummary.SwitchApplied
		record.Requests = res.ChildSummary.Requests
		record.Successful = res.ChildSummary.Successful
		record.Failed = res.ChildSummary.Failed
		record.BytesReceived = res.ChildSummary.BytesReceived
	}
	_ = writeJSON(filepath.Join(*outputDir, "run_result.json"), record)

	// A tau run is valid when the watchdog truncated it after the measured
	// window, the child was canceled non-fatally, and the switch (if any)
	// applied. Truncation is expected: tau runs do not require trace EOF.
	valid := res.Outcome == replay.OutcomeWatchdogTimeout &&
		res.ChildSummary != nil && !res.ChildSummary.Fatal &&
		(*switchAt == 0 || res.ChildSummary.SwitchApplied)
	fmt.Printf("tau run: trace=%s %d->%d switch_at=%v outcome=%s child=%s switch_applied=%v valid=%v bytes=%d\n",
		*traceID, *sourceC, *switchTo, *switchAt, res.Outcome, res.ChildStatus, record.SwitchApplied, valid, record.BytesReceived)
	if !valid {
		os.Exit(2)
	}
}

type runRecord struct {
	TraceID                string `json:"trace_id"`
	UpTrace                string `json:"up_trace"`
	DownTrace              string `json:"down_trace"`
	SourceC                int    `json:"source_c"`
	TargetC                int    `json:"target_c"`
	SwitchAtNs             int64  `json:"switch_at_ns"`
	WindowNs               int64  `json:"window_ns"`
	WatchdogNs             int64  `json:"watchdog_ns"`
	Outcome                string `json:"outcome"`
	Err                    string `json:"err,omitempty"`
	Port                   int    `json:"port"`
	TraceDurNs             int64  `json:"trace_duration_ns"`
	MMLinkExit             int    `json:"mm_link_exit"`
	ChildStatus            string `json:"child_status"`
	ChildError             string `json:"child_error,omitempty"`
	LaunchWallUnixNano     int64  `json:"launch_wall_unix_nano"`
	ExitWallUnixNano       int64  `json:"exit_wall_unix_nano"`
	StartupOffsetNs        int64  `json:"startup_offset_ns"`
	TraceStartWallUnixNano int64  `json:"trace_start_wall_unix_nano"`
	SwitchApplied          bool   `json:"switch_applied"`
	Requests               int64  `json:"requests"`
	Successful             int64  `json:"successful"`
	Failed                 int64  `json:"failed"`
	BytesReceived          int64  `json:"bytes_received"`
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

type planOutput struct {
	TraceID         string          `json:"trace_id"`
	DownTrace       string          `json:"down_trace"`
	EpochDurationMs int64           `json:"epoch_duration_ms"`
	ByteFloor       int64           `json:"byte_floor"`
	PacketBytes     int64           `json:"packet_bytes"`
	RecoveringRatio float64         `json:"recovering_ratio"`
	RegimeThreshold float64         `json:"regime_dependence_threshold"`
	Epochs          []planEpoch     `json:"epochs"`
	Injections      []tau.Injection `json:"injections"`
}

type planEpoch struct {
	Index              int        `json:"epoch_index"`
	StartMs            int64      `json:"start_ms"`
	EndMs              int64      `json:"end_ms"`
	Opportunities      int        `json:"opportunities"`
	Bytes              int64      `json:"bytes"`
	RateBytesPerSecond float64    `json:"rate_bytes_per_second"`
	Regime             tau.Regime `json:"regime"`
}

func runPlan(down, traceID string) error {
	if down == "" {
		return fmt.Errorf("-down is required for -plan")
	}
	if traceID == "" {
		return fmt.Errorf("-trace-id is required for -plan")
	}
	rates, err := tau.ReadNativeEpochs(down, tau.EpochDuration)
	if err != nil {
		return err
	}
	labels := tau.Classify(rates)
	out := planOutput{
		TraceID:         traceID,
		DownTrace:       down,
		EpochDurationMs: tau.EpochDuration.Milliseconds(),
		ByteFloor:       tau.ByteFloor,
		PacketBytes:     tau.PacketBytes,
		RecoveringRatio: tau.RecoveringRatio,
		RegimeThreshold: tau.RegimeDependenceThreshold,
	}
	for i, r := range rates {
		out.Epochs = append(out.Epochs, planEpoch{
			Index: r.Index, StartMs: r.StartMs, EndMs: r.EndMs,
			Opportunities: r.Opportunities, Bytes: r.Bytes,
			RateBytesPerSecond: r.RateBytesPerSecond, Regime: labels[i],
		})
	}
	out.Injections = tau.SelectInjections(traceID, rates, labels)
	return writeJSONStdout(out)
}

func writeJSONStdout(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func runMeasure(eventsPath string, traceStartWall int64, start, end time.Duration) error {
	if end <= start {
		return fmt.Errorf("measure-end must exceed measure-start")
	}
	events, err := tau.LoadEvents(eventsPath)
	if err != nil {
		return err
	}
	bytes := tau.WindowBytes(events, traceStartWall, start, end)
	return writeJSONStdout(map[string]any{
		"events":          len(events),
		"window_bytes":    bytes,
		"window_start_ns": int64(start),
		"window_end_ns":   int64(end),
	})
}

func runAggregate(path string, threshold float64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var obs []tau.Observation
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var o tau.Observation
		if err := json.Unmarshal(line, &o); err != nil {
			return fmt.Errorf("decode observation: %w", err)
		}
		obs = append(obs, o)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	aggs, err := tau.Aggregate(obs, threshold)
	if err != nil {
		return err
	}
	return writeJSONStdout(map[string]any{
		"threshold":    threshold,
		"observations": len(obs),
		"directions":   aggs,
	})
}
