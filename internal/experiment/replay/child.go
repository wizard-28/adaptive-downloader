package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/fetcher"
)

// childFlushInterval bounds how stale the on-disk child summary can be. The
// summary file is the reliable measurement channel: with mm-link --once, the
// link ends at trace EOF and the child is signaled immediately, so stdout
// captured after EOF and a single end-of-run write are both unreliable.
const childFlushInterval = 25 * time.Millisecond

// ChildConfig configures the downloader process that runs inside the mm-link
// namespace.
type ChildConfig struct {
	// Host overrides the server address; empty means $MAHIMAHI_BASE, then
	// 127.0.0.1 (direct/local use).
	Host string
	// Port is the controlled server's listener port.
	Port int
	// OutputPath, SummaryPath, TotalSize, ChunkSize, Concurrency, and Timeout
	// configure the downloader.
	OutputPath  string
	SummaryPath string
	// EventsPath optionally receives a JSONL byte-event timeline (raw
	// monotonic since-start and wall timestamps) for anchor reconstruction.
	EventsPath string
	TotalSize  int64
	ChunkSize  int64
	// Concurrency is the fixed number of simultaneous HTTP/1.1 flows.
	Concurrency int
	// Timeout is the per-request deadline. NoRequestTimeout explicitly
	// disables it (the watchdog-only formal replay policy), so requests are
	// bounded only by the child run context.
	Timeout          time.Duration
	NoRequestTimeout bool
	// DialTimeout bounds connection establishment. NoDialTimeout explicitly
	// disables it; dials remain cancelable through the request context.
	DialTimeout   time.Duration
	NoDialTimeout bool
	// MinConcurrency and MaxConcurrency optionally enable adaptive
	// concurrency bounds so a harness diagnostic can change the target
	// mid-run. Both zero keeps the fixed behavior (Concurrency as min=max).
	MinConcurrency int
	MaxConcurrency int
	// SwitchAt and SwitchTo are a harness-only switch-injection diagnostic:
	// at SwitchAt (child elapsed), the concurrency target is forced to
	// SwitchTo. Zero SwitchAt disables the switch. This is used only by the
	// tau measurement campaign, never by the formal fixed-C pilot.
	SwitchAt time.Duration
	SwitchTo int
	// MaxRuntime optionally bounds the child independently (in addition to
	// the parent watchdog).
	MaxRuntime time.Duration
}

// ChildStatus classifies the downloader child's final state from its summary.
// It is evidence for the harness outcome: a fatal child must never be
// classified as having reached the trace EOF.
type ChildStatus string

const (
	// ChildStatusMissing: no complete final summary exists.
	ChildStatusMissing ChildStatus = "missing"
	// ChildStatusClean: the child finished its download without error.
	ChildStatusClean ChildStatus = "clean"
	// ChildStatusCanceled: the child was canceled by the harness (trace-EOF
	// teardown or a signal). This is not a downloader failure.
	ChildStatusCanceled ChildStatus = "canceled"
	// ChildStatusFatal: the child failed (for example a retry limit exceeded).
	ChildStatusFatal ChildStatus = "fatal"
)

// classifyChild maps a child summary to its status and error text.
func classifyChild(summary *ChildSummary) (ChildStatus, string) {
	switch {
	case summary == nil:
		return ChildStatusMissing, ""
	case !summary.Final:
		return ChildStatusMissing, summary.Err
	case summary.Fatal:
		return ChildStatusFatal, summary.Err
	case summary.Canceled:
		return ChildStatusCanceled, summary.Err
	case summary.Err != "":
		return ChildStatusFatal, summary.Err
	default:
		return ChildStatusClean, ""
	}
}

// ChildSummary is written to SummaryPath periodically and once more when the
// child stops, so the parent can inspect measurement even when the child is
// terminated at trace EOF.
type ChildSummary struct {
	URL                 string `json:"url"`
	Requests            int64  `json:"requests"`
	Successful          int64  `json:"successful"`
	Failed              int64  `json:"failed"`
	BytesReceived       int64  `json:"bytes_received"`
	Events              int64  `json:"events"`
	DistinctConnections int    `json:"distinct_connections"`
	OutputSize          int64  `json:"output_size"`
	// Canceled is true when the final download error was a context
	// cancellation (mm-link's trace-EOF teardown or a parent signal). It is
	// not a downloader failure.
	Canceled bool `json:"canceled"`
	// Fatal is true when the final download error was a real downloader
	// failure (for example a retry limit exceeded) rather than a
	// harness-initiated cancellation. Fatal runs must never be treated as
	// having reached trace EOF.
	Fatal             bool   `json:"fatal"`
	WorkloadExhausted bool   `json:"workload_exhausted"`
	Final             bool   `json:"final"`
	Err               string `json:"err,omitempty"`

	// Switch-injection diagnostic evidence (tau campaign only).
	SwitchAtNanos    int64 `json:"switch_at_ns,omitempty"`
	SwitchTo         int   `json:"switch_to,omitempty"`
	SwitchApplied    bool  `json:"switch_applied,omitempty"`
	FinalConcurrency int   `json:"final_concurrency,omitempty"`

	// Anchor raw timestamps: child process start (wall only, for the
	// cross-process bridge) and the first byte event (monotonic since child
	// start plus wall metadata).
	StartWallUnixNano        int64 `json:"start_wall_unix_nano"`
	HasFirstByte             bool  `json:"has_first_byte"`
	FirstByteSinceStartNanos int64 `json:"first_byte_since_start_ns"`
	FirstByteWallUnixNano    int64 `json:"first_byte_wall_unix_nano"`
}

// recordingObserver meters byte events, retains the first-byte timestamp, and
// optionally appends every raw event to a JSONL file.
type recordingObserver struct {
	meter *fetcher.ByteMeter
	start time.Time

	mu         sync.Mutex
	enc        *json.Encoder
	first      bool
	firstSince int64
	firstWall  int64
}

func (r *recordingObserver) ObserveBytes(e fetcher.ByteEvent) {
	r.meter.ObserveBytes(e)
	elapsed := int64(e.Time.Sub(r.start))
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.first {
		r.first = true
		r.firstSince = elapsed
		r.firstWall = e.Time.UnixNano()
	}
	if r.enc != nil {
		_ = r.enc.Encode(Event{
			SinceStartNanos: elapsed,
			WallUnixNano:    e.Time.UnixNano(),
			Delta:           e.Delta,
			Cumulative:      e.RequestCumulative,
			ConnID:          e.ConnID,
		})
	}
}

func (r *recordingObserver) firstByte() (ok bool, since, wall int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.first, r.firstSince, r.firstWall
}

// RunDownloaderChild runs the HTTP/1.1 range downloader against the controlled
// logical workload until it finishes, its context is canceled (mm-link's trace
// EOF teardown or the parent watchdog), or its optional self-limit expires.
// It handles SIGINT/SIGTERM/SIGHUP for graceful cancellation and keeps the
// summary file up to date while running.
func RunDownloaderChild(ctx context.Context, cfg ChildConfig) (ChildSummary, error) {
	var sum ChildSummary
	switch {
	case cfg.Port <= 0:
		return sum, errors.New("replay: child port must be > 0")
	case cfg.OutputPath == "":
		return sum, errors.New("replay: child output path is required")
	case cfg.SummaryPath == "":
		return sum, errors.New("replay: child summary path is required")
	case cfg.TotalSize <= 0 || cfg.ChunkSize <= 0 || cfg.Concurrency < 1:
		return sum, errors.New("replay: invalid child download configuration")
	case cfg.MinConcurrency < 0 || cfg.MaxConcurrency < 0:
		return sum, errors.New("replay: concurrency bounds must be >= 0")
	case (cfg.MinConcurrency == 0) != (cfg.MaxConcurrency == 0):
		return sum, errors.New("replay: adaptive concurrency requires both min and max")
	case cfg.MaxConcurrency > 0 && (cfg.MinConcurrency < 1 || cfg.MinConcurrency > cfg.MaxConcurrency):
		return sum, errors.New("replay: invalid adaptive concurrency bounds")
	case cfg.MaxConcurrency > 0 && (cfg.Concurrency < cfg.MinConcurrency || cfg.Concurrency > cfg.MaxConcurrency):
		return sum, errors.New("replay: initial concurrency outside adaptive bounds")
	case cfg.SwitchAt < 0:
		return sum, errors.New("replay: switch time must be >= 0")
	case cfg.SwitchAt > 0 && cfg.MaxConcurrency == 0:
		return sum, errors.New("replay: switch injection requires adaptive concurrency bounds")
	case cfg.SwitchAt > 0 && (cfg.SwitchTo < cfg.MinConcurrency || cfg.SwitchTo > cfg.MaxConcurrency):
		return sum, errors.New("replay: switch target outside adaptive bounds")
	}

	host := cfg.Host
	if host == "" {
		host = os.Getenv("MAHIMAHI_BASE")
	}
	if host == "" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s:%d/", host, cfg.Port)

	processStart := time.Now()
	meter := fetcher.NewByteMeter()
	recorder := &recordingObserver{meter: meter, start: processStart}
	var evFile *os.File
	if cfg.EventsPath != "" {
		f, err := os.OpenFile(cfg.EventsPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return sum, fmt.Errorf("replay: open events file: %w", err)
		}
		evFile = f
		recorder.enc = json.NewEncoder(f)
	}
	dlCfg := downloader.Config{
		URL:              url,
		OutputPath:       cfg.OutputPath,
		TotalSize:        cfg.TotalSize,
		Concurrency:      cfg.Concurrency,
		ChunkSize:        cfg.ChunkSize,
		Timeout:          cfg.Timeout,
		NoRequestTimeout: cfg.NoRequestTimeout,
		DialTimeout:      cfg.DialTimeout,
		NoDialTimeout:    cfg.NoDialTimeout,
		MaxRetries:       1,
		ByteObserver:     recorder,
	}
	if cfg.MaxConcurrency > 0 {
		dlCfg.MinConcurrency = cfg.MinConcurrency
		dlCfg.MaxConcurrency = cfg.MaxConcurrency
	}
	d, err := downloader.New(dlCfg)
	if err != nil {
		if evFile != nil {
			_ = evFile.Close()
		}
		return sum, err
	}
	var switchApplied atomic.Bool

	buildSummary := func(derr error, final bool) ChildSummary {
		canceled := final && derr != nil && errors.Is(derr, context.Canceled)
		s := ChildSummary{
			URL:               url,
			OutputSize:        outputSize(cfg.OutputPath),
			WorkloadExhausted: final && derr == nil,
			Canceled:          canceled,
			Fatal:             final && derr != nil && !canceled,
			Final:             final,
		}
		s.StartWallUnixNano = processStart.UnixNano()
		s.HasFirstByte, s.FirstByteSinceStartNanos, s.FirstByteWallUnixNano = recorder.firstByte()
		if derr != nil {
			s.Err = derr.Error()
		}
		s.SwitchAtNanos = int64(cfg.SwitchAt)
		s.SwitchTo = cfg.SwitchTo
		s.SwitchApplied = switchApplied.Load()
		s.FinalConcurrency = d.Concurrency()
		ms := d.Metrics()
		s.Requests = ms.Aggregate.TotalRequests
		s.Successful = ms.Aggregate.SuccessfulRequests
		s.Failed = ms.Aggregate.FailedRequests
		conns := make(map[string]struct{})
		for _, e := range meter.Snapshot() {
			s.Events++
			s.BytesReceived += e.Delta
			conns[e.ConnID] = struct{}{}
		}
		s.DistinctConnections = len(conns)
		_ = writeChildSummary(cfg.SummaryPath, s)
		return s
	}

	buildSummary(nil, false) // the summary file exists from the start

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if cfg.MaxRuntime > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, cfg.MaxRuntime)
		defer cancel()
	}

	stopFlush := make(chan struct{})
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		ticker := time.NewTicker(childFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopFlush:
				return
			case <-ticker.C:
				_ = buildSummary(nil, false)
			}
		}
	}()

	switchDone := make(chan struct{})
	if cfg.SwitchAt > 0 {
		go func() {
			defer close(switchDone)
			timer := time.NewTimer(cfg.SwitchAt - time.Since(processStart))
			defer timer.Stop()
			select {
			case <-timer.C:
				d.SetConcurrency(cfg.SwitchTo)
				switchApplied.Store(true)
			case <-runCtx.Done():
			}
		}()
	} else {
		close(switchDone)
	}

	downloadErr := d.Download(runCtx)
	<-switchDone
	close(stopFlush)
	<-flushDone
	sum = buildSummary(downloadErr, true)
	if evFile != nil {
		_ = evFile.Close()
	}
	if sum.Fatal {
		// The on-disk summary already carries the error; a fatal downloader
		// error must make the child process exit non-zero so mm-link and any
		// wrapper cannot mistake it for a clean trace-EOF cancellation.
		return sum, fmt.Errorf("replay: downloader failed: %w", downloadErr)
	}
	return sum, nil
}

func outputSize(path string) int64 {
	if st, err := os.Stat(path); err == nil {
		return st.Size()
	}
	return 0
}

func writeChildSummary(path string, sum ChildSummary) error {
	data, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("replay: write child summary: %w", err)
	}
	return nil
}
