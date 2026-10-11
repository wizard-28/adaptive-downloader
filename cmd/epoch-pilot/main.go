// Command epoch-pilot runs the pre-registered epoch-duration pilot driver:
// a 2-trace x C={1,2,4,8,16} sequential full-trace replay matrix, with all
// four candidate durations evaluated post-hoc from each replay. It performs no
// tau/oracle/headroom analysis and never changes the registered protocol.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"adaptivedl/internal/experiment/epoch"
	"adaptivedl/internal/experiment/pilot"
	"adaptivedl/internal/experiment/replay"
)

const (
	defaultGoodTrace     = "/usr/local/share/mahimahi/traces/Verizon-LTE-driving.down"
	defaultDegradedTrace = "/usr/local/share/mahimahi/traces/Verizon-EVDO-driving.down"
)

func main() {
	var (
		child        = flag.Bool("child", false, "internal: run as an mm-link downloader child")
		port         = flag.Int("port", 0, "internal (child): controlled-server port")
		c            = flag.Int("c", 1, "internal (child): fixed concurrency")
		events       = flag.String("events", "", "internal (child): event timeline path")
		timeout      = flag.String("timeout", "none", `per-request deadline: "none" (watchdog-only formal policy) or a positive duration (diagnostic override only)`)
		dialTimeout  = flag.String("dial-timeout", "none", `connection dial deadline: "none" (watchdog-only formal policy) or a positive duration (diagnostic override only)`)
		traceGood    = flag.String("trace-good", defaultGoodTrace, "good-capacity pilot trace family (.down/.up resolved)")
		traceDeg     = flag.String("trace-degraded", defaultDegradedTrace, "degraded-capacity pilot trace family (.down/.up resolved)")
		outputDir    = flag.String("output-dir", "testdata/diagnostics/epoch_pilot", "pilot output directory")
		workloadSize = flag.Int64("workload-size", 64<<30, "logical workload size (bytes)")
		chunkSize    = flag.Int64("chunk", 1<<20, "fixed range chunk size (bytes)")
		watchdog     = flag.Duration("watchdog", 45*time.Minute, "hard watchdog per replay")
		resume       = flag.Bool("resume", true, "skip trace/C runs that already have a valid result.json")
		dryRun       = flag.Bool("dry-run", false, "print the resolved plan and exit without replaying")
		onlyTrace    = flag.String("only-trace", "", "diagnostic: restrict to one resolved trace ID (not the registered pilot matrix)")
		onlyC        = flag.Int("only-c", 0, "diagnostic: restrict to one concurrency (not the registered pilot matrix)")
		output       = flag.String("output", "", "internal (child): downloader output path")
		summary      = flag.String("summary", "", "internal (child): child summary path")
	)
	flag.Parse()

	if *onlyC < 0 {
		fmt.Fprintln(os.Stderr, "epoch-pilot: -only-c must be >= 0")
		os.Exit(2)
	}

	policy, err := parseDeadlines(*timeout, *dialTimeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "epoch-pilot:", err)
		os.Exit(2)
	}

	if *child {
		_, err := replay.RunDownloaderChild(context.Background(), replay.ChildConfig{
			Port: *port, OutputPath: *output, SummaryPath: *summary, EventsPath: *events,
			TotalSize: *workloadSize, ChunkSize: *chunkSize, Concurrency: *c,
			Timeout: policy.request, NoRequestTimeout: policy.noRequest,
			DialTimeout: policy.dial, NoDialTimeout: policy.noDial,
			MaxRuntime: *watchdog,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "epoch-pilot child:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	goodPair, err := pilot.ResolveTracePair(*traceGood)
	if err != nil {
		fmt.Fprintln(os.Stderr, "epoch-pilot:", err)
		os.Exit(2)
	}
	degPair, err := pilot.ResolveTracePair(*traceDeg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "epoch-pilot:", err)
		os.Exit(2)
	}
	if goodPair.DownPath == degPair.DownPath {
		fmt.Fprintf(os.Stderr, "epoch-pilot: good and degraded traces resolve to the same family %q\n", goodPair.ID)
		os.Exit(2)
	}
	specs := pilot.SortedSpecs(pilot.Plan(goodPair, degPair, pilot.DefaultCSet))
	diagnostic := *onlyTrace != "" || *onlyC != 0
	if diagnostic {
		specs = filterSpecs(specs, *onlyTrace, *onlyC)
		if len(specs) == 0 {
			fmt.Fprintf(os.Stderr, "epoch-pilot: no planned run matches only-trace=%q only-c=%d\n", *onlyTrace, *onlyC)
			os.Exit(2)
		}
	}
	proto := epoch.DefaultProtocol()

	if diagnostic {
		fmt.Printf("epoch-pilot DIAGNOSTIC RUN (not the registered pilot matrix; no duration selection performed)\n")
	} else {
		fmt.Printf("epoch-duration pilot driver (registered protocol, no headroom analysis)\n")
	}
	fmt.Printf("  traces: good=%s (up=%s down=%s) degraded=%s (up=%s down=%s)\n",
		goodPair.ID, goodPair.UpPath, goodPair.DownPath, degPair.ID, degPair.UpPath, degPair.DownPath)
	fmt.Printf("  concurrency: %v\n", proto.Concurrency)
	fmt.Printf("  chunk: %d bytes  workload: %d bytes  request deadline: %s  dial deadline: %s  watchdog: %s%s\n",
		proto.ChunkSize, *workloadSize,
		replay.DeadlineString(policy.request, policy.noRequest),
		replay.DeadlineString(policy.dial, policy.noDial),
		*watchdog, watchdogOnlySuffix(policy))
	fmt.Printf("  candidates: %v\n", proto.Candidates)
	fmt.Printf("  byte floor: %d bytes/C/epoch  min observed fraction: %.2f\n", proto.ByteFloor, proto.MinObservedFraction)
	fmt.Printf("  output dir: %s  resume: %v  dry-run: %v\n", *outputDir, *resume, *dryRun)
	for _, s := range specs {
		dir := pilot.RunDir(*outputDir, s.TraceID, s.C)
		fmt.Printf("  plan: trace=%-18s C=%-2d dir=%s\n", s.TraceID, s.C, dir)
	}
	if *dryRun {
		fmt.Println("dry-run: no replay performed")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	perTrace := map[string]map[int]*pilot.RunResult{goodPair.ID: {}, degPair.ID: {}}
	var incomplete []string

	for _, spec := range specs {
		dir := pilot.RunDir(*outputDir, spec.TraceID, spec.C)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "epoch-pilot: mkdir %s: %v\n", dir, err)
			os.Exit(2)
		}
		if *resume {
			if existing, err := pilot.LoadResult(dir); err == nil && existing != nil && existing.Status == "ok" {
				fmt.Printf("resume: %s C=%d already complete\n", spec.TraceID, spec.C)
				perTrace[spec.TraceID][spec.C] = existing
				continue
			}
		}
		res, evidence, err := runOne(ctx, spec, dir, *workloadSize, *watchdog, policy)
		if err != nil || res == nil {
			fmt.Fprintf(os.Stderr, "epoch-pilot: run failed trace=%s C=%d dir=%s err=%v\n", spec.TraceID, spec.C, dir, err)
			_ = pilot.WriteJSON(pilot.FailurePath(dir, 1), map[string]any{
				"trace_id": spec.TraceID, "up_trace_path": spec.UpPath, "down_trace_path": spec.DownPath,
				"c": spec.C, "status": "failed", "err": fmt.Sprint(err), "at": time.Now(),
				"evidence": evidence,
			})
			incomplete = append(incomplete, fmt.Sprintf("%s_C%d", spec.TraceID, spec.C))
			continue
		}
		perTrace[spec.TraceID][spec.C] = res
	}

	if diagnostic {
		if len(incomplete) > 0 {
			fmt.Fprintf(os.Stderr, "epoch-pilot: diagnostic runs incomplete: %v\n", incomplete)
			os.Exit(4)
		}
		fmt.Println("epoch-pilot: diagnostic run(s) complete; no pilot duration selection performed")
		return
	}

	var summaries []pilot.TraceSummary
	for _, id := range []string{goodPair.ID, degPair.ID} {
		if len(perTrace[id]) != len(pilot.DefaultCSet) {
			continue
		}
		ts, err := pilot.CombineTrace(id, perTrace[id], pilot.DefaultCandidates)
		if err != nil {
			fmt.Fprintf(os.Stderr, "epoch-pilot: combine %s: %v\n", id, err)
			incomplete = append(incomplete, id+"_combine")
			continue
		}
		summaries = append(summaries, ts)
	}
	report := pilot.SummarizePilot(summaries, pilot.DefaultCandidates, incomplete)
	if err := pilot.WriteJSON(filepath.Join(*outputDir, "pilot_report.json"), report); err != nil {
		fmt.Fprintf(os.Stderr, "epoch-pilot: write report: %v\n", err)
	}
	fmt.Printf("pilot: incomplete=%v passed=%v selected=%q\n", report.Incomplete, report.Passed, report.Selected)
	switch {
	case len(report.Incomplete) > 0:
		os.Exit(4)
	case !report.Passed:
		fmt.Println("EPOCH_DURATION_BLOCKED")
		os.Exit(3)
	default:
		fmt.Println("EPOCH_DURATION_READY", report.Selected)
	}
}

// deadlinePolicy is the parsed request/dial deadline policy of one pilot run.
// The explicit disabled flags distinguish the watchdog-only formal policy
// from an unset/default duration.
type deadlinePolicy struct {
	request   time.Duration
	noRequest bool
	dial      time.Duration
	noDial    bool
}

// watchdogOnly reports whether both request and dial deadlines are disabled.
func (p deadlinePolicy) watchdogOnly() bool { return p.noRequest && p.noDial }

// parseDeadlines parses the CLI deadline policy values. "none" disables the
// corresponding deadline; a positive duration is a diagnostic override.
func parseDeadlines(requestFlag, dialFlag string) (deadlinePolicy, error) {
	req, noReq, err := replay.ParseDeadline(requestFlag)
	if err != nil {
		return deadlinePolicy{}, err
	}
	dial, noDial, err := replay.ParseDeadline(dialFlag)
	if err != nil {
		return deadlinePolicy{}, err
	}
	return deadlinePolicy{request: req, noRequest: noReq, dial: dial, noDial: noDial}, nil
}

// watchdogOnlySuffix annotates the banner when the watchdog-only policy is
// active.
func watchdogOnlySuffix(p deadlinePolicy) string {
	if p.watchdogOnly() {
		return " (watchdog-only)"
	}
	return ""
}

func runOne(ctx context.Context, spec pilot.RunSpec, dir string, workload int64, watchdog time.Duration, policy deadlinePolicy) (*pilot.RunResult, pilot.RunEvidence, error) {
	evidence := pilot.RunEvidence{}
	exe, err := os.Executable()
	if err != nil {
		return nil, evidence, err
	}
	eventsPath := filepath.Join(dir, "events.jsonl")
	summaryPath := filepath.Join(dir, "summary.json")
	outputPath := filepath.Join(dir, "output.bin")
	res, err := replay.Run(ctx, replay.Config{
		UpTrace:          spec.UpPath,
		DownTrace:        spec.DownPath,
		WorkloadSize:     workload,
		ChunkSize:        epoch.DefaultProtocol().ChunkSize,
		Concurrency:      spec.C,
		Timeout:          policy.request,
		MaxRuntime:       watchdog,
		TerminationGrace: 5 * time.Second,
		Child: func(port int) ([]string, []string) {
			return []string{
				exe, "-child", "-port", strconv.Itoa(port),
				"-output", outputPath, "-summary", summaryPath, "-events", eventsPath,
				"-workload-size", strconv.FormatInt(workload, 10),
				"-chunk", strconv.FormatInt(epoch.DefaultProtocol().ChunkSize, 10),
				"-c", strconv.Itoa(spec.C),
				"-timeout", replay.DeadlineString(policy.request, policy.noRequest),
				"-dial-timeout", replay.DeadlineString(policy.dial, policy.noDial),
				"-watchdog", watchdog.String(),
			}, nil
		},
		SummaryPath:     summaryPath,
		ServerAddr:      "0.0.0.0:0",
		DownlinkLogPath: filepath.Join(dir, "down.log"),
		UplinkLogPath:   filepath.Join(dir, "up.log"),
		Log:             os.Stderr,
	})
	evidence = pilot.EvidenceFromResult(res)
	if err != nil {
		return nil, evidence, fmt.Errorf("outcome=%s err=%w", res.Outcome, err)
	}
	if res.Outcome != replay.OutcomeSuccessTraceEOF {
		return nil, evidence, fmt.Errorf("outcome=%s", res.Outcome)
	}
	anchor := pilot.AnchorFromResult(res.Anchor)
	_ = pilot.WriteJSON(filepath.Join(dir, "anchor.json"), anchor)
	runStart := time.Now()
	rr, err := pilot.BuildRunResult(spec, res.TraceDuration, anchor, eventsPath, pilot.DefaultCandidates, epoch.ExclusionNone)
	if err != nil {
		return nil, evidence, err
	}
	rr.Evidence = evidence
	rr.StartedAt = runStart
	rr.CompletedAt = time.Now()
	if err := pilot.WriteJSON(pilot.ResultPath(dir), rr); err != nil {
		return nil, evidence, err
	}
	fmt.Printf("run complete: %s C=%d bytes=%d out_of_range=%d endpoint_residual=%v\n",
		spec.TraceID, spec.C, rr.TotalBytes, rr.OutOfRange, res.TraceEndpointResidual)
	return rr, evidence, nil
}

// filterSpecs returns the planned runs matching the diagnostic selectors.
// Empty selectors are ignored; onlyTrace matches the resolved trace ID and
// onlyC > 0 matches the concurrency.
func filterSpecs(specs []pilot.RunSpec, onlyTrace string, onlyC int) []pilot.RunSpec {
	var out []pilot.RunSpec
	for _, s := range specs {
		if onlyTrace != "" && s.TraceID != onlyTrace {
			continue
		}
		if onlyC > 0 && s.C != onlyC {
			continue
		}
		out = append(out, s)
	}
	return out
}
