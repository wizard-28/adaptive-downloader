package replay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Outcome is an explicit harness result. Failures are not collapsed into one
// generic status.
type Outcome string

const (
	// OutcomeSuccessTraceEOF: mm-link exited with status 0 AND the child
	// reached the trace endpoint with no fatal error: the downlink-log
	// endpoint is within AlignmentTolerance of the downlink trace duration
	// and the child summary shows no fatal downloader error. A status-0
	// mm-link exit alone is NOT sufficient (with --once, mm-link exits with
	// its child, so an early child death can still produce status 0).
	OutcomeSuccessTraceEOF Outcome = "SUCCESS_TRACE_REACHED_EOF"
	// OutcomeMMLinkStartFailed: mm-link could not be started at all.
	OutcomeMMLinkStartFailed Outcome = "MM_LINK_START_FAILED"
	// OutcomeDownloaderStartFailed: mm-link ran but its child command failed
	// (exec failure or non-zero exit before the endpoint).
	OutcomeDownloaderStartFailed Outcome = "DOWNLOADER_START_FAILED"
	// OutcomeDownloaderExited: the child/mm-link exited normally before the
	// endpoint (for the replay harness this is usually the downloader having
	// exhausted the logical workload).
	OutcomeDownloaderExited Outcome = "DOWNLOADER_EXITED"
	// OutcomeWatchdogTimeout: the independent hard watchdog fired.
	OutcomeWatchdogTimeout Outcome = "WATCHDOG_TIMEOUT"
	// OutcomeWorkloadExhausted: the downloader completed the logical object.
	OutcomeWorkloadExhausted Outcome = "WORKLOAD_EXHAUSTED"
	// OutcomeCleanupFailed: processes survived teardown.
	OutcomeCleanupFailed Outcome = "CLEANUP_FAILED"
	// OutcomeInvalidConfiguration: the harness configuration was rejected.
	OutcomeInvalidConfiguration Outcome = "INVALID_CONFIGURATION"
	// OutcomeSummaryMissing: trace EOF was reached but the child left no
	// summary (the child output channel is unreliable after EOF).
	OutcomeSummaryMissing Outcome = "DOWNLOADER_SUMMARY_MISSING"
	// OutcomeCanceled: the caller's context was canceled.
	OutcomeCanceled Outcome = "CANCELED"
	// OutcomeDownloaderFatalError: the downloader child failed fatally (for
	// example a retry limit exceeded). This is never trace EOF, even when
	// mm-link itself exited with status 0.
	OutcomeDownloaderFatalError Outcome = "DOWNLOADER_FATAL_ERROR"
	// OutcomeEndpointNotReached: mm-link exited normally but the downlink-log
	// endpoint did not reach the downlink trace duration within the frozen
	// AlignmentTolerance. Missing endpoint evidence is never trace EOF.
	OutcomeEndpointNotReached Outcome = "TRACE_ENDPOINT_NOT_REACHED"
)

// MMLinkConfig configures a single mm-link --once replay process.
type MMLinkConfig struct {
	// Binary is the mm-link executable; empty means "mm-link".
	Binary string
	// UpTrace and DownTrace are Mahimahi delivery-opportunity trace files.
	UpTrace, DownTrace string
	// Child is the full argv of the process to run inside the namespace.
	Child []string
	// Env entries are appended to the inherited environment.
	Env []string
	// Watchdog is the independent hard limit covering startup, replay, and
	// teardown. It must not be replaced by mm-link's own EOF behavior: a
	// child that defers or ignores mm-link's EOF signal makes mm-link wait.
	Watchdog time.Duration
	// TerminationGrace is how long graceful termination is given before
	// SIGKILL.
	TerminationGrace time.Duration
	// Dir optionally sets the working directory for mm-link.
	Dir string
	// UplinkLog and DownlinkLog optionally capture mm-link packet logs.
	UplinkLog, DownlinkLog string
	// Log optionally receives a copy of the captured (bounded) output.
	Log io.Writer
}

// MMLinkResult reports what happened to one mm-link replay.
type MMLinkResult struct {
	Outcome      Outcome
	Err          error
	LaunchedAt   time.Time
	EndedAt      time.Time
	ExitCode     int
	LeftoverPIDs []int
	Output       string
}

const outputCaptureLimit = 64 << 10

// capWriter is a bounded, concurrency-safe output capture.
type capWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() < w.limit {
		rem := w.limit - w.buf.Len()
		if rem > len(p) {
			rem = len(p)
		}
		w.buf.Write(p[:rem])
	}
	return len(p), nil
}

func (w *capWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// runMMLink launches mm-link --once and enforces the endpoint and watchdog.
// The child runs inside mm-link's network namespace.
func runMMLink(ctx context.Context, cfg MMLinkConfig) MMLinkResult {
	res := MMLinkResult{}
	if cfg.Binary == "" {
		cfg.Binary = "mm-link"
	}
	if cfg.TerminationGrace <= 0 {
		cfg.TerminationGrace = 3 * time.Second
	}
	if len(cfg.Child) == 0 {
		res.Outcome = OutcomeInvalidConfiguration
		res.Err = errors.New("replay: child argv is required")
		return res
	}
	if cfg.Watchdog <= 0 {
		res.Outcome = OutcomeInvalidConfiguration
		res.Err = errors.New("replay: watchdog must be > 0")
		return res
	}

	args := []string{cfg.UpTrace, cfg.DownTrace, "--once"}
	if cfg.UplinkLog != "" {
		args = append(args, "--uplink-log="+cfg.UplinkLog)
	}
	if cfg.DownlinkLog != "" {
		args = append(args, "--downlink-log="+cfg.DownlinkLog)
	}
	args = append(args, "--")
	args = append(args, cfg.Child...)
	cmd := exec.Command(cfg.Binary, args...)
	cmd.Dir = cfg.Dir
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	capture := &capWriter{limit: outputCaptureLimit}
	cmd.Stdout = capture
	cmd.Stderr = capture
	// Own process group so the whole tree can be signaled as a last resort.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	res.LaunchedAt = time.Now()
	if err := cmd.Start(); err != nil {
		res.Outcome = OutcomeMMLinkStartFailed
		res.Err = err
		res.EndedAt = time.Now()
		return res
	}
	pid := cmd.Process.Pid

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	watchdog := time.NewTimer(cfg.Watchdog)
	defer watchdog.Stop()

	outcome := Outcome("")
	reaped := false
	select {
	case err := <-exited:
		// With --once, mm-link exits by itself at trace EOF (status 0). A
		// non-zero status means the child command failed.
		reaped = true
		res.ExitCode = exitCode(err)
		res.Err = err
		if err == nil {
			outcome = OutcomeSuccessTraceEOF
		} else {
			outcome = OutcomeDownloaderStartFailed
		}
	case <-watchdog.C:
		outcome = OutcomeWatchdogTimeout
		res.Err = errors.New("replay: watchdog timeout")
	case <-ctx.Done():
		outcome = OutcomeCanceled
		res.Err = ctx.Err()
	}

	if !reaped {
		leftover, waitReaped := terminateTree(pid, exited, cfg.TerminationGrace)
		res.LeftoverPIDs = leftover
		if len(leftover) > 0 {
			res.Outcome = OutcomeCleanupFailed
			if res.Err == nil {
				res.Err = fmt.Errorf("replay: processes survived teardown: %v", leftover)
			}
			res.EndedAt = time.Now()
			res.Output = capture.String()
			if cfg.Log != nil {
				_, _ = cfg.Log.Write([]byte(res.Output))
			}
			return res
		}
		_ = waitReaped
	}

	res.Outcome = outcome
	res.EndedAt = time.Now()
	res.Output = capture.String()
	if cfg.Log != nil {
		_, _ = cfg.Log.Write([]byte(res.Output))
	}
	return res
}

// terminateTree asks the downloader child (and other descendants) to exit
// first so it can finish its summary, then mm-link; if anything survives the
// grace period it kills the process group and tracked descendants. It returns
// the PIDs that are still alive afterwards.
func terminateTree(pid int, exited <-chan error, grace time.Duration) (leftover []int, reaped bool) {
	tracked := descendantPIDs(pid)
	for _, p := range tracked {
		_ = syscall.Kill(p, syscall.SIGTERM)
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)

	select {
	case <-exited:
		return alivePIDs(tracked), true
	case <-time.After(grace):
	}

	if len(alivePIDs(append(append([]int(nil), tracked...), pid))) > 0 {
		if processGroupAlive(pid) {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
		for _, p := range tracked {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}

	select {
	case <-exited:
		return alivePIDs(tracked), true
	case <-time.After(grace):
		return alivePIDs(tracked), false
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
