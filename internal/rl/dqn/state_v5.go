package dqn

// State spec version 2 (Candidate v5) support.
//
// Candidate v5 keeps the concurrency-only action space but replaces the v1 state
// with a concurrency-focused TEMPORAL state for a longer (16 MiB) horizon. Chunk
// size is fixed at 1 MiB and is no longer a control variable, so the v2 state
// carries no chunk-size feature. The v2 layout has 13 features, like v1, but a
// different feature set and ordering; models record their state spec version so
// inference rejects a mismatch.
//
// This file is purely additive: it does not alter the v1 StateSpec/Observation
// behavior used by Candidates v1-v4.

import (
	"fmt"
	"time"
)

// StateSpecVersionV5 is the state feature layout version for Candidate v5.
const StateSpecVersionV5 = 2

// FeatureNamesV5 returns the ordered v5 feature names. The order is part of the
// model contract and must not change without bumping the version.
func FeatureNamesV5() []string {
	return []string{
		"concurrency_norm",
		"remaining_bytes_frac",
		"aggregate_throughput_norm",
		"aggregate_throughput_ewma_norm",
		"aggregate_throughput_slope",
		"aggregate_throughput_volatility",
		"request_duration_norm",
		"request_duration_slope",
		"recent_failure_rate",
		"recent_retry_rate",
		"since_concurrency_change_norm",
		"recent_completed_norm",
		"throughput_improvement",
	}
}

// NumFeaturesV5 is the length of the v5 feature vector.
func NumFeaturesV5() int { return len(FeatureNamesV5()) }

// DefaultStateSpecV5 returns the canonical v5 normalization bounds.
func DefaultStateSpecV5() StateSpec {
	return StateSpec{
		Version:         StateSpecVersionV5,
		MinConcurrency:  1,
		MaxConcurrency:  4,
		MinChunkSize:    1 << 20, // informational: chunk is fixed at 1 MiB
		MaxChunkSize:    1 << 20,
		ThroughputScale: 8 << 20,
		DurationScale:   1.0,
		StalenessScale:  1.0,
		EWMAAlpha:       0.3,
		CounterWindow:   8,
	}
}

// ObservationV5 is the raw v5 environment observation. It is concurrency-focused
// and temporal; there is no chunk-size and no cumulative-bytes feature.
type ObservationV5 struct {
	Concurrency int
	TotalBytes  int64
	DoneBytes   int64

	AggregateThroughput float64       // current windowed wall-clock throughput (bytes/sec)
	RequestDuration     time.Duration // most recent request duration

	// Temporal trackers maintained by the adapter.
	ThroughputEWMA  float64
	PrevThroughput  float64
	ThroughputVol   float64
	PrevDuration    time.Duration
	SinceConcChange time.Duration

	// Recent window (last CounterWindow attempts).
	WindowFailures  int64
	WindowRetries   int64
	WindowAttempts  int64
	WindowCompleted int64
}

// ValidateV5 rejects malformed v5 observations.
func (o ObservationV5) ValidateV5() error {
	switch {
	case o.Concurrency < 0:
		return fmt.Errorf("%w: negative concurrency", ErrInvalidState)
	case o.TotalBytes < 0 || o.DoneBytes < 0:
		return fmt.Errorf("%w: negative bytes", ErrInvalidState)
	case o.DoneBytes > o.TotalBytes && o.TotalBytes > 0:
		return fmt.Errorf("%w: done bytes exceed total bytes", ErrInvalidState)
	case o.AggregateThroughput < 0 || !finite(o.AggregateThroughput):
		return fmt.Errorf("%w: invalid aggregate throughput", ErrInvalidState)
	case o.RequestDuration < 0:
		return fmt.Errorf("%w: negative duration", ErrInvalidState)
	case o.ThroughputEWMA < 0 || o.PrevThroughput < 0 || o.ThroughputVol < 0:
		return fmt.Errorf("%w: invalid throughput trackers", ErrInvalidState)
	case o.SinceConcChange < 0:
		return fmt.Errorf("%w: negative staleness", ErrInvalidState)
	case o.WindowFailures < 0 || o.WindowRetries < 0 || o.WindowAttempts < 0 || o.WindowCompleted < 0:
		return fmt.Errorf("%w: negative window counters", ErrInvalidState)
	}
	return nil
}

// FeaturesV5 builds the deterministic normalized v5 feature vector.
func (o ObservationV5) FeaturesV5(spec StateSpec) ([]float64, error) {
	return o.featuresForVersion(spec, StateSpecVersionV5)
}

// featuresForVersion is the shared feature math for the 13-feature v5/v6
// layout. wantVersion selects which state spec version is accepted; the
// concurrency normalization bounds come from the spec itself.
func (o ObservationV5) featuresForVersion(spec StateSpec, wantVersion int) ([]float64, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.Version != wantVersion {
		return nil, fmt.Errorf("%w: features need state version %d, got %d", ErrStateSpecMismatch, wantVersion, spec.Version)
	}
	if err := o.ValidateV5(); err != nil {
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
	completedNorm := 0.0
	if spec.CounterWindow > 0 {
		completedNorm = clamp01(float64(o.WindowCompleted) / float64(spec.CounterWindow))
	}
	improvement := 0.0
	if o.PrevThroughput > 0 {
		improvement = clampNeg1to1((o.AggregateThroughput - o.PrevThroughput) / o.PrevThroughput)
	}

	x := []float64{
		normalize(float64(o.Concurrency), float64(spec.MinConcurrency), float64(spec.MaxConcurrency)),
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
		completedNorm,
		improvement,
	}
	if len(x) != NumFeaturesV5() {
		return nil, fmt.Errorf("%w: built %d features, want %d", ErrStateSpecMismatch, len(x), NumFeaturesV5())
	}
	for i, v := range x {
		if !finite(v) {
			return nil, fmt.Errorf("%w: feature %d not finite", ErrInvalidState, i)
		}
	}
	return x, nil
}
