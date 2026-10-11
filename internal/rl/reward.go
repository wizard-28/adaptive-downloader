package rl

import (
	"errors"
	"fmt"
	"math"
)

// ErrInvalidReward indicates a reward containing NaN or an infinity.
var ErrInvalidReward = errors.New("rl: invalid reward")

// RewardComponents is the data contract for reward terms. NO composite formula,
// weights, or tuning are defined here: the individual components are recorded
// so the combination can be audited and later changed.
//
// Meaning:
//   - Throughput:             sustained useful throughput (aggregate/wall-clock,
//     normalized, >= 0)
//   - ThroughputImprovement:  change in aggregate throughput vs. the previous
//     observation (normalized, signed)
//   - LatencyCost:            request-duration penalty (higher is worse)
//   - RetryCost:              penalty for retried attempts
//   - FailureCost:            penalty for failed attempts
//   - ActionCost:             penalty for control changes that actually changed
//     the controller target
type RewardComponents struct {
	Throughput            float64
	ThroughputImprovement float64
	LatencyCost           float64
	RetryCost             float64
	FailureCost           float64
	ActionCost            float64
}

// Validate rejects non-finite components.
func (c RewardComponents) Validate() error {
	fields := []struct {
		name  string
		value float64
	}{
		{"throughput", c.Throughput},
		{"throughput_improvement", c.ThroughputImprovement},
		{"latency_cost", c.LatencyCost},
		{"retry_cost", c.RetryCost},
		{"failure_cost", c.FailureCost},
		{"action_cost", c.ActionCost},
	}
	for _, f := range fields {
		if math.IsNaN(f.value) || math.IsInf(f.value, 0) {
			return fmt.Errorf("%w: %s is not finite (%v)", ErrInvalidReward, f.name, f.value)
		}
	}
	return nil
}

// Reward is the reward passed to an RL policy's Update. Value is the (future)
// scalar reward; Components holds the uncombined terms.
type Reward struct {
	Value      float64
	Components RewardComponents
}

// Validate rejects a non-finite value or any non-finite component.
func (r Reward) Validate() error {
	if math.IsNaN(r.Value) || math.IsInf(r.Value, 0) {
		return fmt.Errorf("%w: value is not finite (%v)", ErrInvalidReward, r.Value)
	}
	return r.Components.Validate()
}
