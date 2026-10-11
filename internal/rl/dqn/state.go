// Package dqn implements a sequential reinforcement-learning controller for the
// adaptive downloader: a Double DQN whose state contains current + temporal
// information, whose objective is minimizing download completion time, and whose
// trained weights are exported to a portable artifact loaded deterministically
// by Go inference.
//
// The package intentionally does NOT reuse the LinUCB state (which contains a
// saturating cumulative bytes feature) and does NOT contain any training code:
// training lives in python/rl. LinUCB remains a separate, retained contextual-
// bandit baseline.
package dqn

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// StateSpecVersion identifies the state feature layout. A trained model records
// the version it was trained with; inference rejects a mismatch.
const StateSpecVersion = 1

var (
	// ErrInvalidState indicates a malformed or non-finite state.
	ErrInvalidState = errors.New("dqn: invalid state")
	// ErrStateSpecMismatch indicates a state/feature-vector layout mismatch.
	ErrStateSpecMismatch = errors.New("dqn: state spec mismatch")
)

// FeatureNames returns the ordered feature names of the DQN state. The order is
// part of the model contract and must not change without bumping
// StateSpecVersion.
func FeatureNames() []string {
	return []string{
		"concurrency_norm",                // normalized current concurrency
		"chunk_size_norm",                 // normalized current chunk size
		"remaining_bytes_frac",            // progress: remaining/total, informative all episode
		"aggregate_throughput_norm",       // current aggregate throughput / scale
		"aggregate_throughput_ewma_norm",  // EWMA of aggregate throughput / scale
		"aggregate_throughput_slope",      // change vs previous observation / scale
		"aggregate_throughput_volatility", // EWMA of |delta| / scale
		"request_duration_norm",           // request duration / scale
		"request_duration_slope",          // change vs previous duration / scale
		"recent_failure_rate",             // failures in window / window attempts, [0,1]
		"recent_retry_rate",               // retries in window / window attempts, [0,1]
		"since_concurrency_change_norm",   // time since last effective concurrency change / scale
		"since_chunk_change_norm",         // time since last effective chunk-size change / scale
	}
}

// NumFeatures is the length of the feature vector.
func NumFeatures() int { return len(FeatureNames()) }

// StateSpec is the explicit normalization contract for the DQN state.
type StateSpec struct {
	Version int `json:"version"`
	// Bounds used for normalization.
	MinConcurrency  int     `json:"min_concurrency"`
	MaxConcurrency  int     `json:"max_concurrency"`
	MinChunkSize    int64   `json:"min_chunk_size"`
	MaxChunkSize    int64   `json:"max_chunk_size"`
	ThroughputScale float64 `json:"throughput_scale"` // bytes/sec at feature 1.0
	DurationScale   float64 `json:"duration_scale"`   // seconds at feature 1.0
	StalenessScale  float64 `json:"staleness_scale"`  // seconds at feature 1.0
	EWMAAlpha       float64 `json:"ewma_alpha"`       // EWMA smoothing for throughput
	CounterWindow   int     `json:"counter_window"`   // attempts window for failure/retry rate
}

// DefaultStateSpec returns the canonical normalization bounds. These are part of
// the model contract: a trained model records the spec it used.
func DefaultStateSpec() StateSpec {
	return StateSpec{
		Version:         StateSpecVersion,
		MinConcurrency:  1,
		MaxConcurrency:  4,
		MinChunkSize:    32768,
		MaxChunkSize:    1 << 20,
		ThroughputScale: 8 << 20, // 8 MiB/s reference
		DurationScale:   1.0,     // seconds
		StalenessScale:  1.0,     // seconds
		EWMAAlpha:       0.3,
		CounterWindow:   8,
	}
}

// Validate checks the spec bounds and normalization constants. It accepts both
// the v1 layout (StateSpecVersion, Candidates v1-v4) and the v2 layout
// (StateSpecVersionV5, Candidate v5); the two share normalization fields but
// differ in feature layout.
func (s StateSpec) Validate() error {
	switch {
	case s.Version != StateSpecVersion && s.Version != StateSpecVersionV5 && s.Version != StateSpecVersionV6:
		return fmt.Errorf("%w: unsupported state spec version %d", ErrStateSpecMismatch, s.Version)
	case s.MinConcurrency < 1:
		return fmt.Errorf("%w: min concurrency must be >= 1", ErrInvalidState)
	case s.MaxConcurrency < s.MinConcurrency:
		return fmt.Errorf("%w: max concurrency < min concurrency", ErrInvalidState)
	case s.MinChunkSize < 1:
		return fmt.Errorf("%w: min chunk size must be >= 1", ErrInvalidState)
	case s.MaxChunkSize < s.MinChunkSize:
		return fmt.Errorf("%w: max chunk size < min chunk size", ErrInvalidState)
	case !finitePositive(s.ThroughputScale):
		return fmt.Errorf("%w: throughput scale must be finite and > 0", ErrInvalidState)
	case !finitePositive(s.DurationScale):
		return fmt.Errorf("%w: duration scale must be finite and > 0", ErrInvalidState)
	case !finitePositive(s.StalenessScale):
		return fmt.Errorf("%w: staleness scale must be finite and > 0", ErrInvalidState)
	case s.EWMAAlpha <= 0 || s.EWMAAlpha > 1:
		return fmt.Errorf("%w: ewma alpha must be in (0,1]", ErrInvalidState)
	case s.CounterWindow < 1:
		return fmt.Errorf("%w: counter window must be >= 1", ErrInvalidState)
	}
	return nil
}

func finitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

// Observation is the raw environment observation used to build a DQN state. It
// is deliberately current/temporal: there is no saturating cumulative-bytes
// feature.
type Observation struct {
	Concurrency int
	ChunkSize   int64
	TotalBytes  int64
	DoneBytes   int64

	AggregateThroughput float64       // current windowed wall-clock throughput (bytes/sec)
	RequestDuration     time.Duration // most recent request duration
	FailedRequests      int64         // cumulative counters (window rates are derived)
	Retries             int64
	TotalAttempts       int64

	// Temporal trackers maintained by the environment/adapter.
	ThroughputEWMA   float64       // EWMA of aggregate throughput
	PrevThroughput   float64       // previous aggregate throughput
	ThroughputVol    float64       // EWMA of |throughput delta|
	PrevDuration     time.Duration // previous request duration
	SinceConcChange  time.Duration // time since last effective concurrency change
	SinceChunkChange time.Duration // time since last effective chunk-size change
	WindowFailures   int64         // failures in the recent window
	WindowRetries    int64         // retries in the recent window
	WindowAttempts   int64         // attempts in the recent window
}

// Validate rejects malformed observations.
func (o Observation) Validate() error {
	switch {
	case o.Concurrency < 0:
		return fmt.Errorf("%w: negative concurrency", ErrInvalidState)
	case o.ChunkSize < 0:
		return fmt.Errorf("%w: negative chunk size", ErrInvalidState)
	case o.TotalBytes < 0 || o.DoneBytes < 0:
		return fmt.Errorf("%w: negative bytes", ErrInvalidState)
	case o.DoneBytes > o.TotalBytes && o.TotalBytes > 0:
		return fmt.Errorf("%w: done bytes exceed total bytes", ErrInvalidState)
	case o.AggregateThroughput < 0 || !finite(o.AggregateThroughput):
		return fmt.Errorf("%w: invalid aggregate throughput", ErrInvalidState)
	case o.RequestDuration < 0:
		return fmt.Errorf("%w: negative duration", ErrInvalidState)
	case o.FailedRequests < 0 || o.Retries < 0 || o.TotalAttempts < 0:
		return fmt.Errorf("%w: negative counters", ErrInvalidState)
	case o.ThroughputEWMA < 0 || o.PrevThroughput < 0 || o.ThroughputVol < 0:
		return fmt.Errorf("%w: invalid throughput trackers", ErrInvalidState)
	case o.SinceConcChange < 0 || o.SinceChunkChange < 0:
		return fmt.Errorf("%w: negative staleness", ErrInvalidState)
	case o.WindowFailures < 0 || o.WindowRetries < 0 || o.WindowAttempts < 0:
		return fmt.Errorf("%w: negative window counters", ErrInvalidState)
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Features builds the deterministic normalized feature vector for an
// observation under spec. It returns an error for a malformed observation or
// spec; it never panics.
func (o Observation) Features(spec StateSpec) ([]float64, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}

	remaining := 0.0
	if o.TotalBytes > 0 {
		remaining = float64(o.TotalBytes-o.DoneBytes) / float64(o.TotalBytes)
	}

	slope := (o.AggregateThroughput - o.PrevThroughput) / spec.ThroughputScale
	durSlope := (o.RequestDuration.Seconds() - o.PrevDuration.Seconds()) / spec.DurationScale

	failRate, retryRate := 0.0, 0.0
	if o.WindowAttempts > 0 {
		failRate = clamp01(float64(o.WindowFailures) / float64(o.WindowAttempts))
		retryRate = clamp01(float64(o.WindowRetries) / float64(o.WindowAttempts))
	}

	x := []float64{
		normalize(float64(o.Concurrency), float64(spec.MinConcurrency), float64(spec.MaxConcurrency)),
		normalize(float64(o.ChunkSize), float64(spec.MinChunkSize), float64(spec.MaxChunkSize)),
		clamp01(remaining),
		clamp01(o.AggregateThroughput / spec.ThroughputScale),
		clamp01(o.ThroughputEWMA / spec.ThroughputScale),
		clampNeg1to1(slope),
		clamp01(o.ThroughputVol / spec.ThroughputScale),
		clamp01(o.RequestDuration.Seconds() / spec.DurationScale),
		clampNeg1to1(durSlope),
		failRate,
		retryRate,
		clamp01(o.SinceConcChange.Seconds() / spec.StalenessScale),
		clamp01(o.SinceChunkChange.Seconds() / spec.StalenessScale),
	}
	if len(x) != NumFeatures() {
		return nil, fmt.Errorf("%w: built %d features, want %d", ErrStateSpecMismatch, len(x), NumFeatures())
	}
	for i, v := range x {
		if !finite(v) {
			return nil, fmt.Errorf("%w: feature %d not finite", ErrInvalidState, i)
		}
	}
	return x, nil
}

func normalize(value, min, max float64) float64 {
	if max <= min {
		return 0
	}
	return clamp01((value - min) / (max - min))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampNeg1to1(v float64) float64 {
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}
