package rl

import (
	"fmt"
	"math"
)

// ErrInvalidRewardConfig indicates an unusable RewardConfig.
var ErrInvalidRewardConfig = fmt.Errorf("rl: invalid reward config")

// RewardConfig configures the scalar reward computed from the downloader's
// observations. It is deliberately explicit and small: every field has a clear
// meaning, a validation rule, and documented semantics. The values below are
// the CURRENT RESEARCH BASELINE (frozen for the next experiment) but are NOT
// claimed to be scientifically optimal.
//
// The reward combines a sustained-throughput benefit, a throughput-improvement
// benefit, and non-negative costs:
//
//	Value = wThroughput  * Throughput
//	      + wImprovement * ThroughputImprovement
//	      - wLatency     * LatencyCost
//	      - wRetry       * RetryCost
//	      - wFailure     * FailureCost
//	      - wAction      * ActionCost
//
// The two throughput components are computed from the state's AGGREGATE
// (wall-clock) throughput observable, so they respond to concurrency changes;
// see docs/PROJECT_CONTEXT.md (RL State Observability Audit).
//
// Sign convention: the two throughput components are benefits (absolute
// throughput is >= 0; improvement is signed); every cost component is
// non-negative and subtracted.
type RewardConfig struct {
	// ThroughputScale is the reference throughput (bytes/sec) used to normalize
	// BOTH the absolute throughput component (throughput / ThroughputScale) and
	// the improvement component
	// ((throughput - previous) / ThroughputScale). Its earlier meaning covered
	// only the improvement signal; it now normalizes both, which is a documented
	// semantic broadening (the name is unchanged because it remains a throughput
	// scale).
	ThroughputScale float64
	// LatencyScale normalizes request duration. It is the reference duration
	// (in seconds) at which the latency cost saturates at 1.
	LatencyScale float64
	// RetryPenalty is the cost applied per retry observed since the previous
	// decision.
	RetryPenalty float64
	// FailurePenalty is the cost applied per failed attempt observed since the
	// previous decision.
	FailurePenalty float64
	// ActionCost is the fixed cost applied when an action actually changes the
	// corresponding controller target. ActionNone never incurs it, and neither
	// does an action that is a no-op because the controller is already at a
	// bound.
	ActionCost float64

	// Weights applied to each component. Must be finite and >= 0.
	WThroughput  float64
	WImprovement float64
	WLatency     float64
	WRetry       float64
	WFailure     float64
	WAction      float64
}

// DefaultRewardConfig returns the current research baseline. The normalizers
// are chosen so that observed values land in a roughly unit range for the
// default experiment scenario (1 MiB/s reference throughput, 1 s reference
// duration), and the weights keep the throughput benefits dominant.
func DefaultRewardConfig() RewardConfig {
	return RewardConfig{
		ThroughputScale: 1 << 20, // 1 MiB/s
		LatencyScale:    1.0,     // 1 s
		RetryPenalty:    0.5,
		FailurePenalty:  1.0,
		ActionCost:      0.05,

		WThroughput:  1.0,
		WImprovement: 1.0,
		WLatency:     0.5,
		WRetry:       0.5,
		WFailure:     1.0,
		WAction:      1.0,
	}
}

// Validate checks the reward configuration.
func (c RewardConfig) Validate() error {
	nonNegative := []struct {
		name  string
		value float64
	}{
		{"throughput scale", c.ThroughputScale},
		{"latency scale", c.LatencyScale},
		{"retry penalty", c.RetryPenalty},
		{"failure penalty", c.FailurePenalty},
		{"action cost", c.ActionCost},
		{"throughput weight", c.WThroughput},
		{"improvement weight", c.WImprovement},
		{"latency weight", c.WLatency},
		{"retry weight", c.WRetry},
		{"failure weight", c.WFailure},
		{"action weight", c.WAction},
	}
	for _, f := range nonNegative {
		if math.IsNaN(f.value) || math.IsInf(f.value, 0) {
			return fmt.Errorf("%w: %s must be finite, got %v", ErrInvalidRewardConfig, f.name, f.value)
		}
		if f.value < 0 {
			return fmt.Errorf("%w: %s must be >= 0, got %v", ErrInvalidRewardConfig, f.name, f.value)
		}
	}
	if c.ThroughputScale == 0 && (c.WThroughput != 0 || c.WImprovement != 0) {
		return fmt.Errorf("%w: throughput scale must be > 0 when a throughput weight is non-zero", ErrInvalidRewardConfig)
	}
	if c.LatencyScale == 0 && c.WLatency != 0 {
		return fmt.Errorf("%w: latency scale must be > 0 when its weight is non-zero", ErrInvalidRewardConfig)
	}
	return nil
}

// RewardCalculator computes a Reward from a consecutive pair of observations.
//
// It is deterministic and side-effect free. It is NOT part of the learner:
// LinUCB only ever sees the resulting Reward.Value, so reward shaping can change
// without touching the learning algorithm.
//
// Credit assignment: the reward produced for a transition describes the effect
// of the action taken at the EARLIER observation. The downloader's policyRunner
// therefore calls Calculate with (previous context, current context, action
// that was chosen at the previous context). See the timing note in the package
// documentation and docs/PROJECT_CONTEXT.md.
type RewardCalculator struct {
	cfg RewardConfig
}

// NewRewardCalculator validates cfg and returns a calculator.
func NewRewardCalculator(cfg RewardConfig) (*RewardCalculator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &RewardCalculator{cfg: cfg}, nil
}

// Config returns the calculator's configuration.
func (c *RewardCalculator) Config() RewardConfig { return c.cfg }

// Calculate returns the reward for the outcome observed between two consecutive
// observations, attributed to the action chosen at the previous observation.
//
// previous is the observation at which action was selected; current is the
// observation produced after that action was applied. havePrevious is false for
// the very first observation, in which case the absolute throughput component
// is still computed from current.Throughput, the improvement component is 0 (no
// fabricated baseline), and ActionCost is 0 (the state change is unknown).
//
// Invalid inputs (non-finite contexts) yield Reward{Value: 0} rather than a
// corrupted reward.
//
// Worked example with the default config (scale 1 MiB/s, latency scale 1 s,
// weights all 1 except latency 0.5): previous aggregate 1 MiB/s, current
// aggregate 3 MiB/s, current request duration 0.4 s, no retries or failures, and
// a real controller change. Then throughput = 3, improvement = 2, latency cost
// = 0.4, action cost = 0.05, so Value = 3 + 2 - 0.5*0.4 - 0.05 = 4.75.
func (c *RewardCalculator) Calculate(previous, current State, action ActionIndex, havePrevious bool) Reward {
	if err := current.Validate(); err != nil {
		return Reward{}
	}
	if havePrevious {
		if err := previous.Validate(); err != nil {
			return Reward{}
		}
	}

	// Absolute throughput benefit: sustained useful throughput, normalized to
	// the reference scale. This is available from the first observation and is
	// the primary signal (a stable high throughput is rewarded more than a
	// stable low one).
	//
	// It uses the AGGREGATE (wall-clock) throughput observable, not the
	// per-request one: the per-request rate is essentially independent of
	// concurrency under a server-capped transfer, so it cannot reward a
	// concurrency change. See docs/PROJECT_CONTEXT.md (RL State Observability
	// Audit).
	throughput := current.AggregateThroughput / c.cfg.ThroughputScale

	// Improvement benefit: the change in throughput relative to the previous
	// observation, normalized to the same scale. Signed (improvement positive,
	// degradation negative); zero for the first observation (no fabricated
	// baseline).
	improvement := 0.0
	if havePrevious {
		improvement = (current.AggregateThroughput - previous.AggregateThroughput) / c.cfg.ThroughputScale
	}

	// Latency cost: normalized request duration, clamped to [0, 1] so a single
	// very slow request cannot dominate the reward.
	latencyCost := clamp01(current.RequestDuration.Seconds() / c.cfg.LatencyScale)

	// Retry cost and failure cost: counters accumulated between the two
	// observations. They are non-negative deltas (guarded against counter
	// resets / out-of-order input), so a retry is not double-counted as a
	// failure and vice versa.
	retryCost := retryDelta(previous, current, havePrevious) * c.cfg.RetryPenalty
	failureCost := failureDelta(previous, current, havePrevious) * c.cfg.FailurePenalty

	// Action cost: charged only when the action actually changed the
	// corresponding controller target. A no-op action (for example an increase
	// requested while already at the bound) causes no control churn and is not
	// charged.
	actionCost := 0.0
	if havePrevious && actionChangedState(action, previous, current) {
		actionCost = c.cfg.ActionCost
	}

	components := RewardComponents{
		Throughput:            throughput,
		ThroughputImprovement: improvement,
		LatencyCost:           latencyCost,
		RetryCost:             retryCost,
		FailureCost:           failureCost,
		ActionCost:            actionCost,
	}

	// The formula is intentionally written out in one place so it is easy to
	// audit; Reward.Value is exactly this weighted combination.
	value := c.cfg.WThroughput*components.Throughput +
		c.cfg.WImprovement*components.ThroughputImprovement -
		c.cfg.WLatency*components.LatencyCost -
		c.cfg.WRetry*components.RetryCost -
		c.cfg.WFailure*components.FailureCost -
		c.cfg.WAction*components.ActionCost

	reward := Reward{Value: value, Components: components}
	if err := reward.Validate(); err != nil {
		return Reward{}
	}
	return reward
}

// actionChangedState reports whether action actually changed the corresponding
// controller target, inferred from the before/after observations. This is the
// basis for charging ActionCost only for real control churn: an increase
// requested while the controller is already at its bound is a no-op.
func actionChangedState(action ActionIndex, previous, current State) bool {
	switch action {
	case ActionIncreaseConcurrency, ActionDecreaseConcurrency:
		return current.Concurrency != previous.Concurrency
	case ActionIncreaseChunkSize, ActionDecreaseChunkSize:
		return current.ChunkSize != previous.ChunkSize
	default:
		// ActionNone never changes state; unknown indices are not charged.
		return false
	}
}

// retryDelta returns the number of retries observed since the previous
// observation (0 when there is no previous observation).
func retryDelta(previous, current State, havePrevious bool) float64 {
	if !havePrevious {
		return 0
	}
	d := current.Retries - previous.Retries
	if d < 0 {
		return 0
	}
	return float64(d)
}

// failureDelta returns the number of failed attempts observed since the previous
// observation (0 when there is no previous observation).
func failureDelta(previous, current State, havePrevious bool) float64 {
	if !havePrevious {
		return 0
	}
	d := current.FailedRequests - previous.FailedRequests
	if d < 0 {
		return 0
	}
	return float64(d)
}
