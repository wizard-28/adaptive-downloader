// Package rl defines the reinforcement-learning environment contract for the
// adaptive downloader: an observable State, a finite ActionIndex space mapped
// onto the existing policy.Action, a Reward data contract, and an adapter that
// implements the existing downloader policy.Policy interface.
//
// This package contains NO learning algorithm. It only fixes the interface a
// future learner will implement, so that a learned policy can replace
// JointPolicy without changing the downloader architecture.
package rl

import (
	"errors"
	"fmt"
	"math"
	"time"

	"adaptivedl/internal/policy"
)

var (
	// ErrInvalidConfig indicates an unusable Config.
	ErrInvalidConfig = errors.New("rl: invalid config")
	// ErrInvalidState indicates a State with impossible (negative) values.
	ErrInvalidState = errors.New("rl: invalid state")
)

// Config fixes the explicit normalization bounds used by State.Features. It
// mirrors the scenario configuration; nothing is normalized implicitly.
type Config struct {
	MinConcurrency int
	MaxConcurrency int

	MinChunkSize int64
	MaxChunkSize int64

	// MaxThroughput is the reference throughput (bytes/sec) at or above which
	// the normalized throughput feature is 1.
	MaxThroughput float64
	// MaxRequestDuration is the reference request duration at or above which
	// the normalized duration feature is 1.
	MaxRequestDuration time.Duration
	// CounterScale is the reference count at or above which counter features
	// (bytes, successes, failures, retries, attempts) are 1.
	CounterScale float64
}

// Validate checks that the normalization bounds are usable.
func (c Config) Validate() error {
	switch {
	case c.MinConcurrency < 1:
		return fmt.Errorf("%w: min concurrency must be >= 1, got %d", ErrInvalidConfig, c.MinConcurrency)
	case c.MaxConcurrency < c.MinConcurrency:
		return fmt.Errorf("%w: max concurrency (%d) must be >= min (%d)", ErrInvalidConfig, c.MaxConcurrency, c.MinConcurrency)
	case c.MinChunkSize < 1:
		return fmt.Errorf("%w: min chunk size must be >= 1, got %d", ErrInvalidConfig, c.MinChunkSize)
	case c.MaxChunkSize < c.MinChunkSize:
		return fmt.Errorf("%w: max chunk size (%d) must be >= min (%d)", ErrInvalidConfig, c.MaxChunkSize, c.MinChunkSize)
	case !isFinitePositive(c.MaxThroughput):
		return fmt.Errorf("%w: max throughput must be finite and > 0, got %v", ErrInvalidConfig, c.MaxThroughput)
	case c.MaxRequestDuration <= 0:
		return fmt.Errorf("%w: max request duration must be > 0, got %v", ErrInvalidConfig, c.MaxRequestDuration)
	case !isFinitePositive(c.CounterScale):
		return fmt.Errorf("%w: counter scale must be finite and > 0, got %v", ErrInvalidConfig, c.CounterScale)
	}
	return nil
}

func isFinitePositive(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

// State is the observable RL state at the moment an action is selected. Every
// field is derived from the downloader's policy.Context and is available
// immediately after an attempt completes and its metrics are recorded
// (i.e. before the action is applied). No future information is included.
//
// Availability of each field:
//   - Concurrency: controller target before this decision (current).
//   - ChunkSize: controller target before this decision (current).
//   - Throughput: windowed PER-REQUEST throughput over attempts already
//     completed (rate one request achieved; not sensitive to concurrency).
//   - AggregateThroughput: windowed AGGREGATE (wall-clock) throughput over the
//     same window; reflects achieved parallelism and responds to concurrency.
//   - RequestDuration: duration of the just-completed attempt.
//   - BytesTransferred: bytes of the just-completed attempt.
//   - SuccessfulRequests/FailedRequests/Retries/TotalAttempts: counters over
//     attempts already completed.
//
// State is raw (lossless); use Features to obtain a normalized vector.
type State struct {
	Concurrency         int
	ChunkSize           int64
	Throughput          float64
	AggregateThroughput float64
	RequestDuration     time.Duration
	BytesTransferred    int64
	SuccessfulRequests  int64
	FailedRequests      int64
	Retries             int64
	TotalAttempts       int64
}

// StateFromContext converts an existing policy.Context into a State. It is
// deterministic, performs no I/O, and never mutates a controller.
func StateFromContext(ctx policy.Context, cfg Config) (State, error) {
	if err := cfg.Validate(); err != nil {
		return State{}, err
	}
	s := State{
		Concurrency:         ctx.Concurrency,
		ChunkSize:           ctx.ChunkSize,
		Throughput:          ctx.Throughput,
		AggregateThroughput: ctx.AggregateThroughput,
		RequestDuration:     ctx.RequestDuration,
		BytesTransferred:    ctx.BytesTransferred,
		SuccessfulRequests:  ctx.SuccessfulRequests,
		FailedRequests:      ctx.FailedRequests,
		Retries:             ctx.Retries,
		TotalAttempts:       ctx.TotalAttempts,
	}
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	return s, nil
}

// Validate rejects impossible state values. Zero is allowed; negatives are not.
func (s State) Validate() error {
	switch {
	case s.Concurrency < 0:
		return fmt.Errorf("%w: negative concurrency %d", ErrInvalidState, s.Concurrency)
	case s.ChunkSize < 0:
		return fmt.Errorf("%w: negative chunk size %d", ErrInvalidState, s.ChunkSize)
	case s.Throughput < 0 || math.IsNaN(s.Throughput) || math.IsInf(s.Throughput, 0):
		return fmt.Errorf("%w: invalid throughput %v", ErrInvalidState, s.Throughput)
	case s.AggregateThroughput < 0 || math.IsNaN(s.AggregateThroughput) || math.IsInf(s.AggregateThroughput, 0):
		return fmt.Errorf("%w: invalid aggregate throughput %v", ErrInvalidState, s.AggregateThroughput)
	case s.RequestDuration < 0:
		return fmt.Errorf("%w: negative request duration %v", ErrInvalidState, s.RequestDuration)
	case s.BytesTransferred < 0:
		return fmt.Errorf("%w: negative bytes %d", ErrInvalidState, s.BytesTransferred)
	case s.SuccessfulRequests < 0:
		return fmt.Errorf("%w: negative successful requests %d", ErrInvalidState, s.SuccessfulRequests)
	case s.FailedRequests < 0:
		return fmt.Errorf("%w: negative failed requests %d", ErrInvalidState, s.FailedRequests)
	case s.Retries < 0:
		return fmt.Errorf("%w: negative retries %d", ErrInvalidState, s.Retries)
	case s.TotalAttempts < 0:
		return fmt.Errorf("%w: negative total attempts %d", ErrInvalidState, s.TotalAttempts)
	}
	return nil
}

// FeatureNames returns the ordered feature names produced by Features.
func FeatureNames() []string {
	return []string{
		"concurrency",
		"chunk_size",
		"throughput",
		"aggregate_throughput",
		"request_duration",
		"bytes_transferred",
		"successful_requests",
		"failed_requests",
		"retries",
		"total_attempts",
	}
}

// Features returns the deterministic, normalized feature vector for the state.
//
// Normalization is linear against the explicit Config bounds, clamped to
// [0, 1]:
//
//	concurrency      = (c - MinConcurrency) / (MaxConcurrency - MinConcurrency)
//	chunk_size       = (n - MinChunkSize)   / (MaxChunkSize   - MinChunkSize)
//	throughput       = throughput / MaxThroughput
//	aggregate_throughput = aggregate_throughput / MaxThroughput
//	request_duration = duration   / MaxRequestDuration
//	counters         = count      / CounterScale
//
// MaxThroughput is the shared reference rate for both the per-request and the
// aggregate throughput features; the aggregate may exceed it when several
// requests overlap, in which case it clamps to 1.
//
// When a denominator is zero (e.g. Min == Max) the corresponding feature is 0.
//
// Example: with bounds Concurrency [1,16] and ChunkSize [1, 33 MiB],
// Concurrency 4 gives 3/15 = 0.2, ChunkSize 1 MiB gives 1048575/34603007 =
// 0.030, aggregate throughput 2 MiB/s against MaxThroughput 8 MiB/s gives
// 0.25, and 10 successful requests against CounterScale 1000 gives 0.01. The
// vector order is the one listed in FeatureNames.
func (s State) Features(cfg Config) ([]float64, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return []float64{
		normalize(float64(s.Concurrency), float64(cfg.MinConcurrency), float64(cfg.MaxConcurrency)),
		normalize(float64(s.ChunkSize), float64(cfg.MinChunkSize), float64(cfg.MaxChunkSize)),
		normalize(s.Throughput, 0, cfg.MaxThroughput),
		normalize(s.AggregateThroughput, 0, cfg.MaxThroughput),
		normalize(float64(s.RequestDuration), 0, float64(cfg.MaxRequestDuration)),
		normalize(float64(s.BytesTransferred), 0, cfg.CounterScale),
		normalize(float64(s.SuccessfulRequests), 0, cfg.CounterScale),
		normalize(float64(s.FailedRequests), 0, cfg.CounterScale),
		normalize(float64(s.Retries), 0, cfg.CounterScale),
		normalize(float64(s.TotalAttempts), 0, cfg.CounterScale),
	}, nil
}

func normalize(value, min, max float64) float64 {
	if max <= min {
		return 0
	}
	v := (value - min) / (max - min)
	return clamp01(v)
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
