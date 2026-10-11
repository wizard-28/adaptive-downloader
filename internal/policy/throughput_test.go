package policy

import (
	"errors"
	"math"
	"testing"
)

func testThroughputConfig() ThroughputPolicyConfig {
	return ThroughputPolicyConfig{
		MinConcurrency:       1,
		MaxConcurrency:       4,
		ImprovementThreshold: 0.10,
		DegradationThreshold: 0.10,
	}
}

func mustThroughputPolicy(t *testing.T, cfg ThroughputPolicyConfig) *ThroughputPolicy {
	t.Helper()
	p, err := NewThroughputPolicy(cfg)
	if err != nil {
		t.Fatalf("NewThroughputPolicy(%+v) = %v", cfg, err)
	}
	return p
}

// act feeds one throughput observation at the given concurrency and returns
// the resulting action.
func act(p *ThroughputPolicy, concurrency int, throughput float64) Action {
	return p.SelectAction(Context{Concurrency: concurrency, Throughput: throughput})
}

func assertKind(t *testing.T, got Action, want ActionKind) {
	t.Helper()
	if got.Kind != want {
		t.Fatalf("action = %v, want kind %v", got, want)
	}
}

func TestThroughputPolicyFirstObservation(t *testing.T) {
	p := mustThroughputPolicy(t, testThroughputConfig())

	assertKind(t, act(p, 2, 100), ActionNone)
	// Repeating the same throughput is a 0% change -> still no change.
	assertKind(t, act(p, 2, 100), ActionNone)
}

func TestThroughputPolicyIncrease(t *testing.T) {
	p := mustThroughputPolicy(t, testThroughputConfig())
	act(p, 2, 100) // baseline
	assertKind(t, act(p, 2, 130), ActionIncreaseConcurrency)
}

func TestThroughputPolicyDecrease(t *testing.T) {
	p := mustThroughputPolicy(t, testThroughputConfig())
	act(p, 2, 100) // baseline
	assertKind(t, act(p, 2, 70), ActionDecreaseConcurrency)
}

func TestThroughputPolicySmallChangesIgnored(t *testing.T) {
	t.Run("small increase", func(t *testing.T) {
		p := mustThroughputPolicy(t, testThroughputConfig())
		act(p, 2, 100)
		assertKind(t, act(p, 2, 105), ActionNone)
	})
	t.Run("small decrease", func(t *testing.T) {
		p := mustThroughputPolicy(t, testThroughputConfig())
		act(p, 2, 100)
		assertKind(t, act(p, 2, 95), ActionNone)
	})
}

func TestThroughputPolicyRespectsBounds(t *testing.T) {
	t.Run("at max never increases", func(t *testing.T) {
		cfg := testThroughputConfig()
		cfg.MaxConcurrency = 3
		p := mustThroughputPolicy(t, cfg)
		act(p, 3, 100)
		assertKind(t, act(p, 3, 1000), ActionNone)
	})
	t.Run("at min never decreases", func(t *testing.T) {
		cfg := testThroughputConfig()
		cfg.MinConcurrency = 2
		cfg.MaxConcurrency = 5
		p := mustThroughputPolicy(t, cfg)
		act(p, 2, 100)
		assertKind(t, act(p, 2, 1), ActionNone)
	})
}

func TestThroughputPolicyZeroThroughput(t *testing.T) {
	t.Run("zero to zero", func(t *testing.T) {
		p := mustThroughputPolicy(t, testThroughputConfig())
		assertKind(t, act(p, 2, 0), ActionNone) // baseline
		assertKind(t, act(p, 2, 0), ActionNone)
	})
	t.Run("zero to positive increases", func(t *testing.T) {
		p := mustThroughputPolicy(t, testThroughputConfig())
		assertKind(t, act(p, 2, 0), ActionNone) // baseline
		assertKind(t, act(p, 2, 50), ActionIncreaseConcurrency)
	})
	t.Run("zero to positive at max stays", func(t *testing.T) {
		cfg := testThroughputConfig()
		cfg.MaxConcurrency = 3
		p := mustThroughputPolicy(t, cfg)
		assertKind(t, act(p, 3, 0), ActionNone)
		assertKind(t, act(p, 3, 50), ActionNone)
	})
	t.Run("positive to zero decreases", func(t *testing.T) {
		p := mustThroughputPolicy(t, testThroughputConfig())
		assertKind(t, act(p, 2, 100), ActionNone)
		assertKind(t, act(p, 2, 0), ActionDecreaseConcurrency)
	})
	t.Run("positive to zero at min stays", func(t *testing.T) {
		p := mustThroughputPolicy(t, testThroughputConfig()) // min = 1
		assertKind(t, act(p, 1, 100), ActionNone)
		assertKind(t, act(p, 1, 0), ActionNone)
	})
}

func TestThroughputPolicyRepeatedObservations(t *testing.T) {
	p := mustThroughputPolicy(t, testThroughputConfig())

	steps := []struct {
		throughput float64
		want       ActionKind
	}{
		{100, ActionNone},                // baseline
		{130, ActionIncreaseConcurrency}, // +30%
		{130, ActionNone},                // 0%
		{90, ActionDecreaseConcurrency},  // -30.8%
		{91, ActionNone},                 // +1.1%
	}
	for _, step := range steps {
		assertKind(t, act(p, 2, step.throughput), step.want)
	}
}

func TestThroughputPolicyThresholdBoundary(t *testing.T) {
	t.Run("exact improvement threshold increases", func(t *testing.T) {
		p := mustThroughputPolicy(t, testThroughputConfig())
		act(p, 2, 100)
		assertKind(t, act(p, 2, 110), ActionIncreaseConcurrency)
	})
	t.Run("just below improvement threshold ignored", func(t *testing.T) {
		p := mustThroughputPolicy(t, testThroughputConfig())
		act(p, 2, 100)
		assertKind(t, act(p, 2, 109.99), ActionNone)
	})
	t.Run("exact degradation threshold decreases", func(t *testing.T) {
		p := mustThroughputPolicy(t, testThroughputConfig())
		act(p, 2, 100)
		assertKind(t, act(p, 2, 90), ActionDecreaseConcurrency)
	})
	t.Run("just above degradation threshold ignored", func(t *testing.T) {
		p := mustThroughputPolicy(t, testThroughputConfig())
		act(p, 2, 100)
		assertKind(t, act(p, 2, 90.01), ActionNone)
	})
	t.Run("zero thresholds react to any change", func(t *testing.T) {
		cfg := testThroughputConfig()
		cfg.ImprovementThreshold = 0
		cfg.DegradationThreshold = 0
		p := mustThroughputPolicy(t, cfg)
		act(p, 2, 100)
		assertKind(t, act(p, 2, 100.001), ActionIncreaseConcurrency)

		q := mustThroughputPolicy(t, cfg)
		act(q, 2, 100)
		assertKind(t, act(q, 2, 99.999), ActionDecreaseConcurrency)
	})
}

func TestThroughputPolicyNeverReturnsChunkActions(t *testing.T) {
	p := mustThroughputPolicy(t, testThroughputConfig())
	throughputs := []float64{0, 100, 130, 130, 90, 91, 0, 50, 200, 10}
	for _, tp := range throughputs {
		a := act(p, 2, tp)
		switch a.Kind {
		case ActionNone, ActionIncreaseConcurrency, ActionDecreaseConcurrency:
		default:
			t.Fatalf("throughput %v produced disallowed action %v", tp, a)
		}
	}
}

func TestThroughputPolicyUpdateIsNoOp(t *testing.T) {
	p := mustThroughputPolicy(t, testThroughputConfig())
	act(p, 2, 100) // baseline

	// Update must not change the comparison point. If it did, comparing 130
	// against an injected 500 would look like a large decrease.
	p.Update(Context{Concurrency: 2, Throughput: 500}, IncreaseConcurrency(), 1e9)
	p.Update(Context{}, Action{}, math.NaN())
	p.Update(Context{}, Action{}, math.Inf(1))

	assertKind(t, act(p, 2, 130), ActionIncreaseConcurrency)
}

func TestNewThroughputPolicyInvalidConfig(t *testing.T) {
	cases := map[string]func(*ThroughputPolicyConfig){
		"min zero":             func(c *ThroughputPolicyConfig) { c.MinConcurrency = 0 },
		"min negative":         func(c *ThroughputPolicyConfig) { c.MinConcurrency = -1 },
		"max below min":        func(c *ThroughputPolicyConfig) { c.MaxConcurrency = c.MinConcurrency - 1 },
		"negative improvement": func(c *ThroughputPolicyConfig) { c.ImprovementThreshold = -0.1 },
		"negative degradation": func(c *ThroughputPolicyConfig) { c.DegradationThreshold = -0.1 },
		"nan improvement":      func(c *ThroughputPolicyConfig) { c.ImprovementThreshold = math.NaN() },
		"inf degradation":      func(c *ThroughputPolicyConfig) { c.DegradationThreshold = math.Inf(1) },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testThroughputConfig()
			mutate(&cfg)
			if _, err := NewThroughputPolicy(cfg); !errors.Is(err, ErrInvalidPolicyConfig) {
				t.Fatalf("NewThroughputPolicy = %v, want ErrInvalidPolicyConfig", err)
			}
		})
	}
}
