package rl

import (
	"errors"
	"math"
	"testing"
)

func TestRewardValidateValid(t *testing.T) {
	r := Reward{
		Value: 1.5,
		Components: RewardComponents{
			Throughput:            100,
			ThroughputImprovement: -5,
			LatencyCost:           0.1,
			RetryCost:             0.2,
			FailureCost:           0,
			ActionCost:            0.05,
		},
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	if err := (Reward{}).Validate(); err != nil {
		t.Fatalf("zero reward Validate = %v", err)
	}
}

func TestRewardValidateNonFinite(t *testing.T) {
	bad := []float64{math.NaN(), math.Inf(1), math.Inf(-1)}

	for _, v := range bad {
		if err := (Reward{Value: v}).Validate(); !errors.Is(err, ErrInvalidReward) {
			t.Fatalf("Reward{Value:%v} = %v, want ErrInvalidReward", v, err)
		}
	}

	mutators := map[string]func(*RewardComponents, float64){
		"throughput":             func(c *RewardComponents, v float64) { c.Throughput = v },
		"throughput_improvement": func(c *RewardComponents, v float64) { c.ThroughputImprovement = v },
		"latency_cost":           func(c *RewardComponents, v float64) { c.LatencyCost = v },
		"retry_cost":             func(c *RewardComponents, v float64) { c.RetryCost = v },
		"failure_cost":           func(c *RewardComponents, v float64) { c.FailureCost = v },
		"action_cost":            func(c *RewardComponents, v float64) { c.ActionCost = v },
	}
	for name, mutate := range mutators {
		for _, v := range bad {
			var c RewardComponents
			mutate(&c, v)
			if err := (Reward{Components: c}).Validate(); !errors.Is(err, ErrInvalidReward) {
				t.Fatalf("component %s = %v: Validate = %v, want ErrInvalidReward", name, v, err)
			}
		}
	}
}
