package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"adaptivedl/internal/experiment/server"
)

// Config configures one Stage 0 replay harness run.
type Config struct {
	// UpTrace and DownTrace are Mahimahi delivery-opportunity trace files
	// (synthetic for this stage).
	UpTrace, DownTrace string
	// WorkloadSize is the logical object size served by the controlled server.
	// It must exceed everything the replay can possibly transfer and is never
	// allocated in memory.
	WorkloadSize int64
	// ChunkSize is the downloader's fixed range size (the Stage 0 value is
	// 1 MiB, but it is not frozen here).
	ChunkSize int64
	// Concurrency is the fixed number of simultaneous HTTP/1.1 flows.
	Concurrency int
	// Timeout is the per-request timeout inside the downloader child.
	Timeout time.Duration
	// MaxRuntime is the independent hard watchdog for the whole run. It
	// covers startup, replay, and teardown; mm-link --once exits at trace EOF
	// by itself, but a child that defers its EOF signal can make mm-link wait,
	// so the watchdog is mandatory.
	MaxRuntime time.Duration
	// TerminationGrace is how long graceful process termination is given
	// before SIGKILL.
	TerminationGrace time.Duration
	// Child builds the child argv and environment once the server port is
	// known. The child is expected to invoke RunDownloaderChild.
	Child func(port int) (argv []string, env []string)
	// MMLinkBinary overrides the mm-link executable (tests use this).
	MMLinkBinary string
	// ServerAddr is the HTTP listener address; empty means "0.0.0.0:0".
	ServerAddr string
	// SummaryPath is where the child writes its JSON ChildSummary. Empty
	// disables summary reading.
	SummaryPath string
	// UplinkLogPath and DownlinkLogPath optionally capture mm-link packet
	// logs (trace-relative timestamps) for anchor cross-checks.
	UplinkLogPath, DownlinkLogPath string
	// Log optionally receives captured mm-link output.
	Log io.Writer
}

// Result reports the outcome of one harness run.
type Result struct {
	Outcome       Outcome
	Err           error
	TraceDuration time.Duration
	Port          int
	ChildSummary  *ChildSummary
	ServerStats   server.Stats
	MMLink        MMLinkResult
	// Anchor is the trace-time anchor record for this run (nil only if the
	// run failed before an anchor could be formed).
	Anchor *Anchor

	// Evidence-based EOF fields. SUCCESS_TRACE_REACHED_EOF requires all of:
	// mm-link exited normally (MMLinkExit == 0), no fatal child error
	// (ChildStatus != fatal), and the downlink-log endpoint reached the
	// downlink trace duration within AlignmentTolerance (EndpointValid).
	TraceExpectedDuration time.Duration `json:"trace_expected_duration"`
	TraceObservedEndpoint time.Duration `json:"trace_observed_endpoint"`
	TraceEndpointResidual time.Duration `json:"trace_endpoint_residual"`
	EndpointValid         bool          `json:"endpoint_valid"`
	MMLinkExit            int           `json:"mm_link_exit"`
	ChildStatus           ChildStatus   `json:"child_status"`
	ChildError            string        `json:"child_error,omitempty"`
}

// InvalidConfigError wraps configuration validation failures.
var InvalidConfigError = errors.New("replay: invalid configuration")

// Run starts the logical workload server, launches mm-link --once with the
// configured downloader child, stops measurement at the scheduled trace EOF,
// tears the process tree down, and returns the explicit outcome. It never
// waits indefinitely for the logical workload to finish after trace EOF.
func Run(ctx context.Context, cfg Config) (Result, error) {
	res := Result{}
	if err := validateConfig(cfg); err != nil {
		res.Outcome = OutcomeInvalidConfiguration
		res.Err = err
		return res, err
	}
	if cfg.TerminationGrace <= 0 {
		cfg.TerminationGrace = 3 * time.Second
	}
	if cfg.ServerAddr == "" {
		cfg.ServerAddr = "0.0.0.0:0"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	up, err := ReadTrace(cfg.UpTrace)
	if err != nil {
		res.Outcome = OutcomeInvalidConfiguration
		res.Err = err
		return res, err
	}
	down, err := ReadTrace(cfg.DownTrace)
	if err != nil {
		res.Outcome = OutcomeInvalidConfiguration
		res.Err = err
		return res, err
	}
	res.TraceDuration = up.Duration
	if down.Duration > res.TraceDuration {
		res.TraceDuration = down.Duration
	}

	workload, err := server.NewLogical(cfg.WorkloadSize)
	if err != nil {
		res.Outcome = OutcomeInvalidConfiguration
		res.Err = err
		return res, err
	}
	ln, err := net.Listen("tcp", cfg.ServerAddr)
	if err != nil {
		res.Outcome = OutcomeInvalidConfiguration
		res.Err = fmt.Errorf("replay: listen: %w", err)
		return res, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	res.Port = port
	httpSrv := &http.Server{Handler: workload}
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = httpSrv.Serve(ln)
	}()
	defer func() {
		_ = httpSrv.Close()
		<-serveDone
	}()

	argv, env := cfg.Child(port)
	if len(argv) == 0 {
		res.Outcome = OutcomeInvalidConfiguration
		res.Err = fmt.Errorf("%w: child argv is empty", InvalidConfigError)
		return res, res.Err
	}

	res.MMLink = runMMLink(ctx, MMLinkConfig{
		Binary:           cfg.MMLinkBinary,
		UpTrace:          cfg.UpTrace,
		DownTrace:        cfg.DownTrace,
		Child:            argv,
		Env:              env,
		Watchdog:         cfg.MaxRuntime,
		TerminationGrace: cfg.TerminationGrace,
		UplinkLog:        cfg.UplinkLogPath,
		DownlinkLog:      cfg.DownlinkLogPath,
		Log:              cfg.Log,
	})
	res.ServerStats = workload.Stats()
	if cfg.SummaryPath != "" {
		if summary, err := readChildSummary(cfg.SummaryPath); err == nil {
			res.ChildSummary = summary
		}
	}
	res.Anchor = buildAnchor(res.MMLink, res.TraceDuration, res.ChildSummary, cfg.DownlinkLogPath)

	res.MMLinkExit = res.MMLink.ExitCode
	res.ChildStatus, res.ChildError = classifyChild(res.ChildSummary)
	// The endpoint evidence belongs to the downlink direction: the last
	// packet record in the mm-link --downlink-log must reach the downlink
	// trace's own duration. The result's TraceDuration is max(up, down) for
	// epoch slicing and must not be used here (the .up trace is typically a
	// little longer than the .down trace).
	res.TraceExpectedDuration = down.Duration
	haveEndpointRecords := res.Anchor != nil && res.Anchor.DownlinkLogLines > 0
	if haveEndpointRecords {
		res.TraceObservedEndpoint = time.Duration(res.Anchor.DownlinkLogLastMs) * time.Millisecond
	}
	res.TraceEndpointResidual, res.EndpointValid = endpointEvidence(
		res.TraceExpectedDuration, res.TraceObservedEndpoint, haveEndpointRecords)

	res.Outcome = mapOutcome(res.MMLink.Outcome, res.ChildSummary, res.ChildStatus, res.EndpointValid)
	res.Err = outcomeError(res, res.MMLink.Err)
	return res, res.Err
}

// endpointEvidence computes the downlink endpoint residual and validity.
// haveRecords is false when the downlink log is absent or has no packet
// records: missing endpoint evidence is never valid.
func endpointEvidence(expected, observed time.Duration, haveRecords bool) (time.Duration, bool) {
	residual := expected - observed
	if !haveRecords {
		return residual, false
	}
	return residual, WithinTolerance(residual)
}

func validateConfig(cfg Config) error {
	switch {
	case cfg.UpTrace == "" || cfg.DownTrace == "":
		return fmt.Errorf("%w: uplink and downlink traces are required", InvalidConfigError)
	case cfg.WorkloadSize <= 0:
		return fmt.Errorf("%w: workload size must be > 0", InvalidConfigError)
	case cfg.ChunkSize <= 0:
		return fmt.Errorf("%w: chunk size must be > 0", InvalidConfigError)
	case cfg.Concurrency < 1:
		return fmt.Errorf("%w: concurrency must be >= 1", InvalidConfigError)
	case cfg.MaxRuntime <= 0:
		return fmt.Errorf("%w: max runtime (watchdog) must be > 0", InvalidConfigError)
	case cfg.Child == nil:
		return fmt.Errorf("%w: child builder is required", InvalidConfigError)
	}
	return nil
}

// mapOutcome converts a raw mm-link outcome plus the child summary and the
// endpoint evidence into the harness outcome. A status-0 mm-link exit is not
// enough for SUCCESS_TRACE_REACHED_EOF: the child must not have failed
// fatally, and the downlink endpoint must have been observed within the
// frozen AlignmentTolerance of the downlink trace duration.
func mapOutcome(raw Outcome, summary *ChildSummary, status ChildStatus, endpointValid bool) Outcome {
	switch raw {
	case OutcomeSuccessTraceEOF:
		switch {
		case summary == nil || !summary.Final:
			return OutcomeSummaryMissing
		case status == ChildStatusFatal:
			return OutcomeDownloaderFatalError
		case summary.WorkloadExhausted:
			return OutcomeWorkloadExhausted
		case !endpointValid:
			return OutcomeEndpointNotReached
		default:
			return OutcomeSuccessTraceEOF
		}
	case OutcomeDownloaderStartFailed:
		switch {
		case summary != nil && status == ChildStatusFatal:
			return OutcomeDownloaderFatalError
		case summary != nil && (summary.Requests > 0 || summary.WorkloadExhausted):
			return OutcomeDownloaderExited
		default:
			return OutcomeDownloaderStartFailed
		}
	default:
		return raw
	}
}

// outcomeError derives a descriptive error for failure outcomes that did not
// originate from mm-link itself (for example a fatal child after a status-0
// mm-link exit or missing endpoint evidence).
func outcomeError(res Result, base error) error {
	if res.Outcome == OutcomeDownloaderFatalError && res.ChildError != "" {
		if base != nil {
			return fmt.Errorf("replay: downloader fatal error: %s: %w", res.ChildError, base)
		}
		return fmt.Errorf("replay: downloader fatal error: %s", res.ChildError)
	}
	if base != nil {
		return base
	}
	switch res.Outcome {
	case OutcomeSummaryMissing:
		return errors.New("replay: trace EOF claimed but the child left no complete summary")
	case OutcomeDownloaderFatalError:
		return errors.New("replay: downloader fatal error")
	case OutcomeEndpointNotReached:
		return fmt.Errorf(
			"replay: downlink endpoint not reached: observed=%v expected=%v residual=%v tolerance=%v",
			res.TraceObservedEndpoint, res.TraceExpectedDuration, res.TraceEndpointResidual, AlignmentTolerance)
	default:
		return nil
	}
}

// ExitCode maps an outcome to a distinct process exit code.
func ExitCode(o Outcome) int {
	switch o {
	case OutcomeSuccessTraceEOF:
		return 0
	case OutcomeInvalidConfiguration:
		return 2
	case OutcomeMMLinkStartFailed:
		return 3
	case OutcomeDownloaderStartFailed:
		return 4
	case OutcomeWatchdogTimeout:
		return 5
	case OutcomeWorkloadExhausted:
		return 6
	case OutcomeCleanupFailed:
		return 7
	case OutcomeDownloaderExited:
		return 8
	case OutcomeSummaryMissing:
		return 9
	case OutcomeCanceled:
		return 10
	case OutcomeDownloaderFatalError:
		return 11
	case OutcomeEndpointNotReached:
		return 12
	default:
		return 1
	}
}

func readChildSummary(path string) (*ChildSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s ChildSummary
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
