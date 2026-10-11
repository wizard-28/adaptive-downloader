package rl

import (
	"errors"
	"math"
	"testing"
	"time"
)

func testRewardConfig() RewardConfig {
	return RewardConfig{
		ThroughputScale: 1000,
		LatencyScale:    1.0,
		RetryPenalty:    0.5,
		FailurePenalty:  1.0,
		ActionCost:      0.1,
		WThroughput:     1.0,
		WImprovement:    1.0,
		WLatency:        0.5,
		WRetry:          0.5,
		WFailure:        1.0,
		WAction:         1.0,
	}
}

func mustRewardCalc(t *testing.T, cfg RewardConfig) *RewardCalculator {
	t.Helper()
	c, err := NewRewardCalculator(cfg)
	if err != nil {
		t.Fatalf("NewRewardCalculator = %v", err)
	}
	return c
}

func TestRewardConfigValid(t *testing.T) {
	if err := DefaultRewardConfig().Validate(); err != nil {
		t.Fatalf("DefaultRewardConfig().Validate = %v", err)
	}
	if err := testRewardConfig().Validate(); err != nil {
		t.Fatalf("testRewardConfig().Validate = %v", err)
	}
}

func TestRewardConfigInvalid(t *testing.T) {
	cases := map[string]func(*RewardConfig){
		"negative throughput weight":  func(c *RewardConfig) { c.WThroughput = -1 },
		"negative improvement weight": func(c *RewardConfig) { c.WImprovement = -1 },
		"negative latency penalty":    func(c *RewardConfig) { c.WLatency = -0.1 },
		"negative retry penalty":      func(c *RewardConfig) { c.RetryPenalty = -1 },
		"negative action cost":        func(c *RewardConfig) { c.ActionCost = -1 },
		"nan failure penalty":         func(c *RewardConfig) { c.FailurePenalty = math.NaN() },
		"inf scale":                   func(c *RewardConfig) { c.ThroughputScale = math.Inf(1) },
		"nan weight":                  func(c *RewardConfig) { c.WAction = math.NaN() },
		"zero throughput scale used":  func(c *RewardConfig) { c.ThroughputScale = 0 },
		"zero scale improvement only": func(c *RewardConfig) { c.ThroughputScale = 0; c.WThroughput = 0 },
		"zero latency scale used":     func(c *RewardConfig) { c.LatencyScale = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := testRewardConfig()
			mutate(&c)
			if _, err := NewRewardCalculator(c); !errors.Is(err, ErrInvalidRewardConfig) {
				t.Fatalf("NewRewardCalculator(%+v) = %v, want ErrInvalidRewardConfig", c, err)
			}
		})
	}
}

// TestRewardFirstObservation verifies that without a previous observation the
// absolute throughput component is still computed, the improvement component is
// zero (no fabricated baseline), and ActionCost is zero (state change unknown).
func TestRewardFirstObservation(t *testing.T) {
	calc := mustRewardCalc(t, testRewardConfig())
	current := State{AggregateThroughput: 5000, RequestDuration: 500 * time.Millisecond}

	r := calc.Calculate(State{}, current, ActionIncreaseConcurrency, false)
	if r.Components.Throughput != 5.0 {
		t.Fatalf("first-observation absolute throughput = %v, want 5.0", r.Components.Throughput)
	}
	if r.Components.ThroughputImprovement != 0 {
		t.Fatalf("first-observation improvement = %v, want 0", r.Components.ThroughputImprovement)
	}
	if r.Components.ActionCost != 0 {
		t.Fatalf("first-observation action cost = %v, want 0", r.Components.ActionCost)
	}
	if r.Components.LatencyCost != 0.5 {
		t.Fatalf("latency cost = %v, want 0.5", r.Components.LatencyCost)
	}
	// Value = 1.0*5.0 + 0 - 0.5*0.5 = 4.75
	if math.Abs(r.Value-4.75) > 1e-12 {
		t.Fatalf("value = %v, want 4.75", r.Value)
	}
}

// TestRewardStableHighVsLow is the central design check: a stable high
// throughput must score higher than a stable low throughput (improvement is zero
// in both cases).
func TestRewardStableHighVsLow(t *testing.T) {
	calc := mustRewardCalc(t, testRewardConfig())

	high := calc.Calculate(State{AggregateThroughput: 4000}, State{AggregateThroughput: 4000}, ActionNone, true)
	low := calc.Calculate(State{AggregateThroughput: 500}, State{AggregateThroughput: 500}, ActionNone, true)

	if high.Components.Throughput != 4.0 || low.Components.Throughput != 0.5 {
		t.Fatalf("absolute throughput = %v / %v, want 4.0 / 0.5", high.Components.Throughput, low.Components.Throughput)
	}
	if high.Components.ThroughputImprovement != 0 || low.Components.ThroughputImprovement != 0 {
		t.Fatalf("stable improvement should be 0: %v / %v", high.Components.ThroughputImprovement, low.Components.ThroughputImprovement)
	}
	if high.Value <= low.Value {
		t.Fatalf("stable high reward (%v) should exceed stable low reward (%v)", high.Value, low.Value)
	}
}

// TestRewardUsesAggregateThroughput proves the throughput components are driven
// by the aggregate (wall-clock) observable, not the per-request one: a high
// per-request rate with zero aggregate earns no throughput benefit, and the
// aggregate change drives the improvement term.
func TestRewardUsesAggregateThroughput(t *testing.T) {
	calc := mustRewardCalc(t, testRewardConfig())

	// Per-request throughput is high but aggregate is zero.
	noAgg := calc.Calculate(State{Throughput: 5000}, State{Throughput: 5000}, ActionNone, true)
	if noAgg.Components.Throughput != 0 || noAgg.Components.ThroughputImprovement != 0 {
		t.Fatalf("per-request throughput leaked into reward: %+v", noAgg.Components)
	}

	// Aggregate rises 2000 -> 3000 B/s (scale 1000): absolute 3.0, improvement 1.0.
	agg := calc.Calculate(
		State{Throughput: 100, AggregateThroughput: 2000},
		State{Throughput: 100, AggregateThroughput: 3000},
		ActionNone, true,
	)
	if agg.Components.Throughput != 3.0 || agg.Components.ThroughputImprovement != 1.0 {
		t.Fatalf("aggregate components = %v / %v, want 3.0 / 1.0",
			agg.Components.Throughput, agg.Components.ThroughputImprovement)
	}
}

func TestRewardThroughputComponents(t *testing.T) {
	calc := mustRewardCalc(t, testRewardConfig())

	t.Run("improvement positive", func(t *testing.T) {
		prev := State{AggregateThroughput: 1000}
		cur := State{AggregateThroughput: 2000}
		r := calc.Calculate(prev, cur, ActionNone, true)
		if r.Components.Throughput != 2.0 {
			t.Fatalf("absolute throughput = %v, want 2.0", r.Components.Throughput)
		}
		if r.Components.ThroughputImprovement != 1.0 {
			t.Fatalf("improvement = %v, want 1.0", r.Components.ThroughputImprovement)
		}
	})

	t.Run("degradation negative", func(t *testing.T) {
		prev := State{AggregateThroughput: 2000}
		cur := State{AggregateThroughput: 1000}
		r := calc.Calculate(prev, cur, ActionNone, true)
		if r.Components.Throughput != 1.0 {
			t.Fatalf("absolute throughput = %v, want 1.0", r.Components.Throughput)
		}
		if r.Components.ThroughputImprovement != -1.0 {
			t.Fatalf("improvement = %v, want -1.0", r.Components.ThroughputImprovement)
		}
	})

	t.Run("equal neutral improvement", func(t *testing.T) {
		s := State{AggregateThroughput: 1500}
		r := calc.Calculate(s, s, ActionNone, true)
		if r.Components.Throughput != 1.5 {
			t.Fatalf("absolute throughput = %v, want 1.5", r.Components.Throughput)
		}
		if r.Components.ThroughputImprovement != 0 {
			t.Fatalf("improvement = %v, want 0", r.Components.ThroughputImprovement)
		}
	})

	t.Run("zero throughput", func(t *testing.T) {
		r := calc.Calculate(State{}, State{}, ActionNone, true)
		if r.Components.Throughput != 0 || r.Components.ThroughputImprovement != 0 {
			t.Fatalf("zero-throughput components = %v / %v, want 0 / 0", r.Components.Throughput, r.Components.ThroughputImprovement)
		}
	})
}

func TestRewardLatencyMonotonic(t *testing.T) {
	calc := mustRewardCalc(t, testRewardConfig())
	prev := State{AggregateThroughput: 1000}

	low := calc.Calculate(prev, State{AggregateThroughput: 1000, RequestDuration: 100 * time.Millisecond}, ActionNone, true)
	high := calc.Calculate(prev, State{AggregateThroughput: 1000, RequestDuration: 900 * time.Millisecond}, ActionNone, true)

	if low.Components.LatencyCost >= high.Components.LatencyCost {
		t.Fatalf("latency cost not monotonic: low=%v high=%v", low.Components.LatencyCost, high.Components.LatencyCost)
	}
	if high.Components.LatencyCost > 1 {
		t.Fatalf("latency cost = %v, want clamped to <= 1", high.Components.LatencyCost)
	}
	// Even a huge duration saturates at 1.
	extreme := calc.Calculate(prev, State{AggregateThroughput: 1000, RequestDuration: time.Hour}, ActionNone, true)
	if extreme.Components.LatencyCost != 1 {
		t.Fatalf("extreme latency cost = %v, want 1", extreme.Components.LatencyCost)
	}
}

func TestRewardRetryAndFailureDelta(t *testing.T) {
	calc := mustRewardCalc(t, testRewardConfig())
	prev := State{AggregateThroughput: 1000, Retries: 2, FailedRequests: 1}

	t.Run("no change gives zero cost", func(t *testing.T) {
		cur := State{AggregateThroughput: 1000, Retries: 2, FailedRequests: 1}
		r := calc.Calculate(prev, cur, ActionNone, true)
		if r.Components.RetryCost != 0 || r.Components.FailureCost != 0 {
			t.Fatalf("retry/failure cost = %v/%v, want 0/0", r.Components.RetryCost, r.Components.FailureCost)
		}
	})

	t.Run("new retries and failures increase cost", func(t *testing.T) {
		cur := State{AggregateThroughput: 1000, Retries: 5, FailedRequests: 3}
		r := calc.Calculate(prev, cur, ActionNone, true)
		// 3 new retries * 0.5 = 1.5; 2 new failures * 1.0 = 2.0
		if r.Components.RetryCost != 1.5 {
			t.Fatalf("retry cost = %v, want 1.5", r.Components.RetryCost)
		}
		if r.Components.FailureCost != 2.0 {
			t.Fatalf("failure cost = %v, want 2.0", r.Components.FailureCost)
		}
	})

	t.Run("counter reset yields zero not negative", func(t *testing.T) {
		cur := State{AggregateThroughput: 1000, Retries: 0, FailedRequests: 0}
		r := calc.Calculate(prev, cur, ActionNone, true)
		if r.Components.RetryCost < 0 || r.Components.FailureCost < 0 {
			t.Fatalf("negative delta cost: %v/%v", r.Components.RetryCost, r.Components.FailureCost)
		}
	})
}

// TestRewardActionCost verifies that ActionCost is charged only for actions that
// actually changed the corresponding controller target (state-change semantics).
func TestRewardActionCost(t *testing.T) {
	calc := mustRewardCalc(t, testRewardConfig())

	if r := calc.Calculate(State{}, State{}, ActionNone, true); r.Components.ActionCost != 0 {
		t.Fatalf("ActionNone cost = %v, want 0", r.Components.ActionCost)
	}

	t.Run("concurrency action that changes state is charged", func(t *testing.T) {
		r := calc.Calculate(State{Concurrency: 1}, State{Concurrency: 2}, ActionIncreaseConcurrency, true)
		if r.Components.ActionCost != 0.1 {
			t.Fatalf("action cost = %v, want 0.1", r.Components.ActionCost)
		}
	})

	t.Run("concurrency action at a bound is not charged", func(t *testing.T) {
		r := calc.Calculate(State{Concurrency: 4}, State{Concurrency: 4}, ActionIncreaseConcurrency, true)
		if r.Components.ActionCost != 0 {
			t.Fatalf("no-op action cost = %v, want 0", r.Components.ActionCost)
		}
	})

	t.Run("chunk action that changes state is charged", func(t *testing.T) {
		r := calc.Calculate(State{ChunkSize: 100}, State{ChunkSize: 200}, ActionIncreaseChunkSize, true)
		if r.Components.ActionCost != 0.1 {
			t.Fatalf("action cost = %v, want 0.1", r.Components.ActionCost)
		}
	})

	t.Run("chunk action at a bound is not charged", func(t *testing.T) {
		r := calc.Calculate(State{ChunkSize: 100}, State{ChunkSize: 100}, ActionDecreaseChunkSize, true)
		if r.Components.ActionCost != 0 {
			t.Fatalf("no-op action cost = %v, want 0", r.Components.ActionCost)
		}
	})

	t.Run("concurrency action does not react to chunk change", func(t *testing.T) {
		// A concurrency action is judged by concurrency, not chunk size.
		r := calc.Calculate(State{Concurrency: 2, ChunkSize: 100}, State{Concurrency: 2, ChunkSize: 200}, ActionIncreaseConcurrency, true)
		if r.Components.ActionCost != 0 {
			t.Fatalf("action cost = %v, want 0 (concurrency unchanged)", r.Components.ActionCost)
		}
	})
}

// TestRewardFormulaHandComputable verifies Value equals the documented weighted
// combination of the components.
func TestRewardFormulaHandComputable(t *testing.T) {
	cfg := testRewardConfig()
	calc := mustRewardCalc(t, cfg)

	prev := State{Concurrency: 1, ChunkSize: 100, AggregateThroughput: 1000, Retries: 0, FailedRequests: 0}
	cur := State{Concurrency: 2, ChunkSize: 100, AggregateThroughput: 2000, RequestDuration: 500 * time.Millisecond, Retries: 2, FailedRequests: 1}

	r := calc.Calculate(prev, cur, ActionIncreaseConcurrency, true)

	// components
	// absolute throughput = 2000/1000 = 2.0
	// improvement         = (2000-1000)/1000 = 1.0
	// latency             = 0.5/1.0 = 0.5
	// retry               = 2 * 0.5 = 1.0
	// failure             = 1 * 1.0 = 1.0
	// action              = 0.1 (concurrency changed 1 -> 2)
	want := 1.0*2.0 + 1.0*1.0 - 0.5*0.5 - 0.5*1.0 - 1.0*1.0 - 1.0*0.1
	if math.Abs(r.Value-want) > 1e-12 {
		t.Fatalf("value = %v, want %v", r.Value, want)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("reward invalid: %v", err)
	}
}

func TestRewardDeterministic(t *testing.T) {
	calc := mustRewardCalc(t, testRewardConfig())
	prev := State{Concurrency: 1, AggregateThroughput: 1000, Retries: 1, FailedRequests: 1}
	cur := State{Concurrency: 2, AggregateThroughput: 3000, RequestDuration: 250 * time.Millisecond, Retries: 2, FailedRequests: 1}

	a := calc.Calculate(prev, cur, ActionIncreaseChunkSize, true)
	b := calc.Calculate(prev, cur, ActionIncreaseChunkSize, true)
	if a != b {
		t.Fatalf("non-deterministic reward: %+v vs %+v", a, b)
	}
}

func TestRewardEdgeCases(t *testing.T) {
	calc := mustRewardCalc(t, testRewardConfig())

	t.Run("zero observation", func(t *testing.T) {
		r := calc.Calculate(State{}, State{}, ActionNone, true)
		if r.Value != 0 || r.Components != (RewardComponents{}) {
			t.Fatalf("zero observation reward = %+v", r)
		}
	})

	t.Run("large finite values stay finite", func(t *testing.T) {
		prev := State{AggregateThroughput: 1e15}
		cur := State{AggregateThroughput: 1e15 + 1e12, RequestDuration: time.Second}
		r := calc.Calculate(prev, cur, ActionNone, true)
		if err := r.Validate(); err != nil {
			t.Fatalf("large-value reward invalid: %v", err)
		}
	})

	t.Run("invalid current state yields zero reward", func(t *testing.T) {
		r := calc.Calculate(State{}, State{Concurrency: -1}, ActionNone, true)
		if r.Value != 0 || r.Components != (RewardComponents{}) {
			t.Fatalf("invalid current state reward = %+v", r)
		}
	})

	t.Run("invalid previous state yields zero reward", func(t *testing.T) {
		r := calc.Calculate(State{Concurrency: -1}, State{}, ActionNone, true)
		if r.Value != 0 || r.Components != (RewardComponents{}) {
			t.Fatalf("invalid previous state reward = %+v", r)
		}
	})
}
