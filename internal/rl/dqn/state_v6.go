package dqn

// State spec version 3 (Candidate v6) support.
//
// Candidate v6 reuses the validated 13-feature concurrency-focused temporal
// layout from v5, but normalizes concurrency over [1,16] and records state spec
// version 3 so a v5 model can never be loaded as a v6 model. The observation
// layout is identical to v5; only the concurrency bounds and version differ.
//
// This file is purely additive: it does not alter v1-v5 behavior.

// StateSpecVersionV6 is the state feature layout version for Candidate v6.
const StateSpecVersionV6 = 3

// FeatureNamesV6 returns the ordered v6 feature names. The layout matches v5;
// the order is part of the model contract and must not change without bumping
// the version.
func FeatureNamesV6() []string {
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

// NumFeaturesV6 is the length of the v6 feature vector.
func NumFeaturesV6() int { return len(FeatureNamesV6()) }

// DefaultStateSpecV6 returns the canonical v6 normalization bounds.
func DefaultStateSpecV6() StateSpec {
	return StateSpec{
		Version:         StateSpecVersionV6,
		MinConcurrency:  1,
		MaxConcurrency:  16,
		MinChunkSize:    1 << 20, // informational: chunk is fixed at 1 MiB
		MaxChunkSize:    1 << 20,
		ThroughputScale: 8 << 20,
		DurationScale:   1.0,
		StalenessScale:  1.0,
		EWMAAlpha:       0.3,
		CounterWindow:   8,
	}
}

// ObservationV6 is the raw v6 environment observation. The layout is identical
// to ObservationV5 (13 fields); only the concurrency bounds differ.
type ObservationV6 = ObservationV5

// ValidateV6 rejects malformed v6 observations (same checks as v5).
func (o ObservationV5) ValidateV6() error { return o.ValidateV5() }

// FeaturesV6 builds the deterministic normalized v6 feature vector.
func (o ObservationV5) FeaturesV6(spec StateSpec) ([]float64, error) {
	return o.featuresForVersion(spec, StateSpecVersionV6)
}
