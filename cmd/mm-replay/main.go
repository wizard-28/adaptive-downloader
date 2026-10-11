// Command mm-replay runs one experiment-only Stage 0 Mahimahi replay: it
// serves the logical HTTP Range workload, launches mm-link --once with a
// downloader child, ends measurement at the scheduled trace EOF, and enforces
// an independent watchdog and cleanup.
//
// It is infrastructure, not formal Stage 0: it performs no trace-time
// anchoring, epoch analysis, or headroom calculation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"adaptivedl/internal/experiment/replay"
)

func main() {
	var (
		child        = flag.Bool("child", false, "internal: run as the mm-link downloader child")
		port         = flag.Int("port", 0, "internal (child): controlled-server port")
		up           = flag.String("up", "", "uplink Mahimahi trace file")
		down         = flag.String("down", "", "downlink Mahimahi trace file")
		concurrency  = flag.Int("c", 1, "fixed concurrency (simultaneous HTTP/1.1 flows)")
		workloadSize = flag.Int64("workload-size", 1<<36, "logical workload size in bytes (sparse output)")
		chunkSize    = flag.Int64("chunk", 1<<20, "fixed range chunk size in bytes")
		timeout      = flag.String("timeout", "30s", `per-request deadline: "none" or a positive duration`)
		dialTimeout  = flag.String("dial-timeout", "30s", `connection dial deadline: "none" or a positive duration`)
		maxRuntime   = flag.Duration("max-runtime", 5*time.Minute, "independent hard watchdog")
		termGrace    = flag.Duration("termination-grace", 3*time.Second, "grace before SIGKILL")
		output       = flag.String("output", "mm-replay-output.bin", "downloader output path")
		summary      = flag.String("summary", "mm-replay-summary.json", "child summary path")
		events       = flag.String("events", "mm-replay-events.jsonl", "child byte-event timeline path")
		uplinkLog    = flag.String("uplink-log", "", "optional mm-link uplink log path")
		downlinkLog  = flag.String("downlink-log", "", "mm-link downlink log path (required for SUCCESS_TRACE_REACHED_EOF endpoint evidence)")
		serverAddr   = flag.String("server-addr", "0.0.0.0:0", "workload HTTP listener address")
		mmlinkBin    = flag.String("mmlink", "mm-link", "mm-link executable")
		jsonOut      = flag.Bool("json", false, "print the result as JSON")
	)
	flag.Parse()

	requestTimeout, noRequestTimeout, err := replay.ParseDeadline(*timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mm-replay:", err)
		os.Exit(replay.ExitCode(replay.OutcomeInvalidConfiguration))
	}
	dialTimeoutValue, noDialTimeout, err := replay.ParseDeadline(*dialTimeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mm-replay:", err)
		os.Exit(replay.ExitCode(replay.OutcomeInvalidConfiguration))
	}

	if *child {
		_, err := replay.RunDownloaderChild(context.Background(), replay.ChildConfig{
			Port:             *port,
			OutputPath:       *output,
			SummaryPath:      *summary,
			EventsPath:       *events,
			TotalSize:        *workloadSize,
			ChunkSize:        *chunkSize,
			Concurrency:      *concurrency,
			Timeout:          requestTimeout,
			NoRequestTimeout: noRequestTimeout,
			DialTimeout:      dialTimeoutValue,
			NoDialTimeout:    noDialTimeout,
			MaxRuntime:       *maxRuntime,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "mm-replay child:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mm-replay:", err)
		os.Exit(replay.ExitCode(replay.OutcomeInvalidConfiguration))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := replay.Config{
		UpTrace:          *up,
		DownTrace:        *down,
		WorkloadSize:     *workloadSize,
		ChunkSize:        *chunkSize,
		Concurrency:      *concurrency,
		Timeout:          requestTimeout,
		MaxRuntime:       *maxRuntime,
		TerminationGrace: *termGrace,
		MMLinkBinary:     *mmlinkBin,
		ServerAddr:       *serverAddr,
		SummaryPath:      *summary,
		UplinkLogPath:    *uplinkLog,
		DownlinkLogPath:  *downlinkLog,
		Log:              os.Stderr,
		Child: func(port int) ([]string, []string) {
			return []string{
				exe,
				"-child",
				"-port", strconv.Itoa(port),
				"-output", *output,
				"-summary", *summary,
				"-events", *events,
				"-workload-size", strconv.FormatInt(*workloadSize, 10),
				"-chunk", strconv.FormatInt(*chunkSize, 10),
				"-c", strconv.Itoa(*concurrency),
				"-timeout", replay.DeadlineString(requestTimeout, noRequestTimeout),
				"-dial-timeout", replay.DeadlineString(dialTimeoutValue, noDialTimeout),
				"-max-runtime", maxRuntime.String(),
			}, nil
		},
	}
	res, runErr := replay.Run(ctx, cfg)
	if *jsonOut {
		if data, err := json.MarshalIndent(res, "", "  "); err == nil {
			fmt.Println(string(data))
		}
	}
	if res.Outcome != replay.OutcomeSuccessTraceEOF {
		fmt.Fprintf(os.Stderr, "mm-replay: outcome=%s trace=%s err=%v\n",
			res.Outcome, res.TraceDuration, runErr)
	} else {
		fmt.Printf("mm-replay: outcome=%s trace=%s port=%d ",
			res.Outcome, res.TraceDuration, res.Port)
		if res.ChildSummary != nil {
			fmt.Printf(" bytes=%d events=%d connections=%d requests=%d\n",
				res.ChildSummary.BytesReceived, res.ChildSummary.Events,
				res.ChildSummary.DistinctConnections, res.ChildSummary.Requests)
		}
	}
	os.Exit(replay.ExitCode(res.Outcome))
}
