// Package downloader implements a fixed-concurrency HTTP range downloader built
// on the rangetracker, fetcher, assembler, and metrics packages.
//
// The number of active workers can be scaled at runtime through a concurrency
// Controller, either explicitly or via an optional policy. Chunk size remains
// fixed.
package downloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"adaptivedl/internal/assembler"
	"adaptivedl/internal/chunksize"
	"adaptivedl/internal/concurrency"
	"adaptivedl/internal/fetcher"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/policy"
	"adaptivedl/internal/rangetracker"
	"adaptivedl/internal/rl"
)

var (
	// ErrInvalidConfig indicates an unusable Config.
	ErrInvalidConfig = errors.New("downloader: invalid configuration")
	// ErrRetryLimitExceeded indicates a range failed more times than allowed.
	ErrRetryLimitExceeded = errors.New("downloader: retry limit exceeded")
	// ErrIncomplete indicates the run finished without covering the whole file.
	ErrIncomplete = errors.New("downloader: download incomplete")
)

// Config configures a Downloader.
type Config struct {
	URL         string
	OutputPath  string
	TotalSize   int64
	Concurrency int
	ChunkSize   int64
	// Timeout bounds the entire request. Zero uses the fetcher default;
	// NoRequestTimeout explicitly disables the deadline instead.
	Timeout time.Duration
	// NoRequestTimeout explicitly disables the per-request deadline
	// (watchdog-only replay policy): requests are bounded only by the run
	// context. Mutually exclusive with Timeout > 0.
	NoRequestTimeout bool
	// DialTimeout bounds establishing a new connection; zero uses the fetcher
	// default. NoDialTimeout explicitly disables the dial deadline instead.
	DialTimeout time.Duration
	// NoDialTimeout explicitly disables the connection-establishment
	// deadline. Mutually exclusive with DialTimeout > 0.
	NoDialTimeout bool
	// MaxRetries is the number of retries allowed after a range's first
	// attempt. A value of 0 means a single attempt per range.
	MaxRetries int

	// MinConcurrency and MaxConcurrency bound the adaptive worker count.
	// Leave both zero for the fixed behaviour, where concurrency stays at
	// Concurrency for the whole run. When MaxConcurrency > 0, MinConcurrency
	// must be >= 1 and <= MaxConcurrency, and Concurrency is the initial
	// target (clamped into range).
	MinConcurrency int
	MaxConcurrency int

	// ByteObserver optionally receives incremental received-byte events from
	// the response-body read path of every attempt (timestamped at the read,
	// with request and connection identity). Nil disables byte instrumentation
	// and leaves download behavior unchanged. The observer is shared by all
	// workers, so it must be safe for concurrent use (see fetcher.ByteMeter).
	ByteObserver fetcher.ByteObserver
	// UserAgent optionally sets the User-Agent header on every range request.
	// Empty keeps the Go default.
	UserAgent string

	// Policy optionally drives concurrency changes. When nil the downloader
	// behaves exactly as the fixed/adaptive-concurrency baseline. Policies are
	// consulted once per completed attempt, and only concurrency actions are
	// applied.
	Policy policy.Policy
	// PolicyWindowSize is the number of most recent attempts used to compute
	// recent throughput for policy.Context. Zero uses a sensible default.
	PolicyWindowSize int
	// RewardConfig optionally configures the reward computed for RL policies.
	// When nil, no reward is computed and the policy's Update receives 0.
	// Deterministic policies ignore reward entirely, so setting this only
	// affects policies that consume it (e.g. an RL adapter).
	RewardConfig *rl.RewardConfig

	// SplitStragglers enables aria2-style dynamic segmentation: when no
	// unallocated bytes remain, an idle worker takes the second half of the
	// unreceived part of the in-flight range with the most bytes left. Off by
	// default; with it off, in-flight ranges are immutable as before.
	SplitStragglers bool
	// SplitMinBytes is the smallest piece a split may create (both halves
	// are at least this long). Zero uses 1 MiB, matching aria2's -k1M. It
	// requires SplitStragglers.
	SplitMinBytes int64

	// Adaptive chunk size (optional). When MaxChunkSize > 0 the downloader
	// reads a chunk-size controller at each allocation; otherwise ChunkSize is
	// fixed for the whole run. All four values must be set together.
	MinChunkSize     int64
	MaxChunkSize     int64
	InitialChunkSize int64
	ChunkSizeStep    int64
}

func (c Config) validate() error {
	switch {
	case c.URL == "":
		return fmt.Errorf("%w: URL is required", ErrInvalidConfig)
	case c.OutputPath == "":
		return fmt.Errorf("%w: output path is required", ErrInvalidConfig)
	case c.TotalSize < 0:
		return fmt.Errorf("%w: total size must be >= 0", ErrInvalidConfig)
	case c.Concurrency < 1:
		return fmt.Errorf("%w: concurrency must be >= 1", ErrInvalidConfig)
	case c.ChunkSize < 1:
		return fmt.Errorf("%w: chunk size must be >= 1", ErrInvalidConfig)
	case c.Timeout < 0:
		return fmt.Errorf("%w: timeout must be >= 0", ErrInvalidConfig)
	case c.DialTimeout < 0:
		return fmt.Errorf("%w: dial timeout must be >= 0", ErrInvalidConfig)
	case c.MaxRetries < 0:
		return fmt.Errorf("%w: max retries must be >= 0", ErrInvalidConfig)
	case c.SplitMinBytes < 0:
		return fmt.Errorf("%w: split min bytes must be >= 0", ErrInvalidConfig)
	case c.SplitMinBytes > 0 && !c.SplitStragglers:
		return fmt.Errorf("%w: split min bytes requires split stragglers", ErrInvalidConfig)
	case c.MinConcurrency < 0:
		return fmt.Errorf("%w: min concurrency must be >= 0", ErrInvalidConfig)
	case c.MaxConcurrency < 0:
		return fmt.Errorf("%w: max concurrency must be >= 0", ErrInvalidConfig)
	case c.MaxConcurrency == 0 && c.MinConcurrency != 0:
		return fmt.Errorf("%w: min concurrency requires max concurrency", ErrInvalidConfig)
	case c.MaxConcurrency > 0 && c.MinConcurrency < 1:
		return fmt.Errorf("%w: min concurrency must be >= 1 when max is set", ErrInvalidConfig)
	case c.MaxConcurrency > 0 && c.MinConcurrency > c.MaxConcurrency:
		return fmt.Errorf("%w: min concurrency must be <= max concurrency", ErrInvalidConfig)
	case c.MinChunkSize < 0:
		return fmt.Errorf("%w: min chunk size must be >= 0", ErrInvalidConfig)
	case c.MaxChunkSize < 0:
		return fmt.Errorf("%w: max chunk size must be >= 0", ErrInvalidConfig)
	case c.InitialChunkSize < 0:
		return fmt.Errorf("%w: initial chunk size must be >= 0", ErrInvalidConfig)
	case c.ChunkSizeStep < 0:
		return fmt.Errorf("%w: chunk size step must be >= 0", ErrInvalidConfig)
	case c.MaxChunkSize == 0 && (c.MinChunkSize != 0 || c.InitialChunkSize != 0 || c.ChunkSizeStep != 0):
		return fmt.Errorf("%w: min/initial/step chunk size require max chunk size", ErrInvalidConfig)
	case c.MaxChunkSize > 0 && c.MinChunkSize < 1:
		return fmt.Errorf("%w: min chunk size must be >= 1 when max is set", ErrInvalidConfig)
	case c.MaxChunkSize > 0 && (c.InitialChunkSize < c.MinChunkSize || c.InitialChunkSize > c.MaxChunkSize):
		return fmt.Errorf("%w: initial chunk size must be within [min, max]", ErrInvalidConfig)
	case c.MaxChunkSize > 0 && c.ChunkSizeStep < 1:
		return fmt.Errorf("%w: chunk size step must be >= 1 when max is set", ErrInvalidConfig)
	}
	if err := (fetcher.Config{
		Timeout:          c.Timeout,
		NoRequestTimeout: c.NoRequestTimeout,
		DialTimeout:      c.DialTimeout,
		NoDialTimeout:    c.NoDialTimeout,
	}).Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if c.RewardConfig != nil {
		if err := c.RewardConfig.Validate(); err != nil {
			return fmt.Errorf("%w: reward config: %w", ErrInvalidConfig, err)
		}
	}
	return nil
}

// Downloader runs an HTTP range download with a scalable worker count and an
// optional dynamic chunk size for future allocations.
type Downloader struct {
	cfg        Config
	fetcher    *fetcher.Fetcher
	controller *concurrency.Controller
	chunkSize  *chunksize.Controller

	mu        sync.Mutex
	collector *metrics.Collector
	pool      *workerPool
	splits    int64 // straggler splits in the most recent Download
}

// New validates cfg and returns a Downloader. It has no side effects; the
// output file is created by Download.
func New(cfg Config) (*Downloader, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	min, max := cfg.Concurrency, cfg.Concurrency
	if cfg.MaxConcurrency > 0 {
		min, max = cfg.MinConcurrency, cfg.MaxConcurrency
	}
	controller, err := concurrency.New(min, max)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	controller.Set(cfg.Concurrency)

	var chunkCtl *chunksize.Controller
	if cfg.MaxChunkSize > 0 {
		chunkCtl, err = chunksize.New(cfg.MinChunkSize, cfg.MaxChunkSize, cfg.InitialChunkSize, cfg.ChunkSizeStep)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
		}
	}

	return &Downloader{
		cfg: cfg,
		fetcher: fetcher.New(fetcher.Config{
			Timeout:          cfg.Timeout,
			NoRequestTimeout: cfg.NoRequestTimeout,
			DialTimeout:      cfg.DialTimeout,
			NoDialTimeout:    cfg.NoDialTimeout,
			ByteObserver:     cfg.ByteObserver,
			UserAgent:        cfg.UserAgent,
		}),
		controller: controller,
		chunkSize:  chunkCtl,
		collector:  metrics.New(),
	}, nil
}

// currentChunkSize returns the chunk size to use for the next range allocation.
// It reads the controller at call time; when no adaptive controller is
// configured it returns the fixed Config.ChunkSize, preserving the old
// behaviour exactly.
func (d *Downloader) currentChunkSize() int64 {
	if d.chunkSize != nil {
		return d.chunkSize.Current()
	}
	return d.cfg.ChunkSize
}

// ChunkSize returns the current target chunk size used for future allocations.
func (d *Downloader) ChunkSize() int64 { return d.currentChunkSize() }

// SetChunkSize requests a new target chunk size for FUTURE range allocations.
// The value is clamped to the configured [min, max] bounds and the applied
// value is returned. In fixed mode (no adaptive chunk-size configuration) this
// is a no-op that returns Config.ChunkSize.
func (d *Downloader) SetChunkSize(target int64) int64 {
	if d.chunkSize != nil {
		return d.chunkSize.Set(target)
	}
	return d.cfg.ChunkSize
}

// Metrics returns a snapshot of the metrics collected during the most recent
// Download. The returned Snapshot is a copy and is safe to retain or mutate.
// Call it after Download returns.
func (d *Downloader) Metrics() metrics.Snapshot {
	d.mu.Lock()
	c := d.collector
	d.mu.Unlock()
	return c.Snapshot()
}

// Splits returns how many straggler splits the most recent Download made
// (always 0 unless Config.SplitStragglers is set). Call it after Download.
func (d *Downloader) Splits() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.splits
}

// splitMinBytes returns the smallest piece a straggler split may create.
func (d *Downloader) splitMinBytes() int64 {
	if d.cfg.SplitMinBytes > 0 {
		return d.cfg.SplitMinBytes
	}
	return 1 << 20
}

// Concurrency returns the current target concurrency.
func (d *Downloader) Concurrency() int { return d.controller.Current() }

// SetConcurrency requests a new target concurrency. The value is clamped to
// the configured [min, max] range. If a Download is running, its worker count
// scales toward the new target without interrupting in-flight requests; the
// actual value applied is returned.
func (d *Downloader) SetConcurrency(target int) int {
	newTarget := d.controller.Set(target)
	d.mu.Lock()
	pool := d.pool
	d.mu.Unlock()
	if pool != nil {
		pool.SetTarget(newTarget)
	}
	return newTarget
}

// Download fetches every byte of the configured file and writes it to
// OutputPath. It returns nil only when every range completed and the output
// file has the expected size.
//
// If ctx is cancelled, workers stop taking new ranges, in-flight requests
// observe the cancellation, and the context error is returned. The Assembler
// is closed exactly once on every path.
func (d *Downloader) Download(ctx context.Context) (err error) {
	tracker := rangetracker.New(d.cfg.TotalSize)
	asm, err := assembler.New(d.cfg.OutputPath, d.cfg.TotalSize)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := asm.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if d.cfg.TotalSize == 0 {
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	collector := metrics.New()
	d.mu.Lock()
	d.collector = collector
	d.mu.Unlock()

	state := &runState{
		attempts:    make(map[int64]int),
		maxAttempts: d.cfg.MaxRetries + 1,
		cancel:      cancel,
	}

	var reqCounter atomic.Int64
	pool := newWorkerPool(d, tracker, asm, state, collector, &reqCounter, d.controller.Current())

	if d.cfg.Policy != nil {
		var rewarder *rl.RewardCalculator
		if d.cfg.RewardConfig != nil {
			// Config validation already succeeded in New, so this cannot fail.
			rewarder, _ = rl.NewRewardCalculator(*d.cfg.RewardConfig)
		}
		pool.runner = newPolicyRunner(d.cfg.Policy, d.controller, d.chunkSize, d.cfg.ChunkSize, d.cfg.PolicyWindowSize, pool.SetTarget, rewarder)
	}

	d.mu.Lock()
	d.pool = pool
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.pool = nil
		d.splits = pool.splits.Load()
		d.mu.Unlock()
	}()

	pool.run(runCtx)
	pool.wg.Wait()

	if ferr := state.fatal(); ferr != nil {
		return ferr
	}
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	if !tracker.Done() {
		return fmt.Errorf("%w: pending=%d inFlight=%d", ErrIncomplete, len(tracker.Pending()), len(tracker.InFlight()))
	}
	info, statErr := os.Stat(d.cfg.OutputPath)
	if statErr != nil {
		return fmt.Errorf("downloader: stat output: %w", statErr)
	}
	if info.Size() != d.cfg.TotalSize {
		return fmt.Errorf("%w: output size %d, want %d", ErrIncomplete, info.Size(), d.cfg.TotalSize)
	}
	return nil
}

// runState holds shared per-download state: retry counters and the first fatal
// error, which cancels the run so other workers stop promptly.
type runState struct {
	mu          sync.Mutex
	attempts    map[int64]int
	maxAttempts int
	fatalErr    error
	cancel      context.CancelFunc
}

func (s *runState) recordAttempt(start int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[start]++
	return s.attempts[start]
}

func (s *runState) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fatalErr == nil {
		s.fatalErr = err
		s.cancel()
	}
}

func (s *runState) fatal() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fatalErr
}
