package policy

import (
	"errors"
	"math"
	"testing"
)

func testJointConfig() JointPolicyConfig {
	return JointPolicyConfig{
		MinConcurrency:       1,
		MaxConcurrency:       4,
		MinChunkSize:         16,
		MaxChunkSize:         128,
		ImprovementThreshold: 0.2,
		DegradationThreshold: 0.2,
	}
}

func mustJoint(t *testing.T, cfg JointPolicyConfig) *JointPolicy {
	t.Helper()
	p, err := NewJointPolicy(cfg)
	if err != nil {
		t.Fatalf("NewJointPolicy(%+v) = %v", cfg, err)
	}
	return p
}

func jointAct(p *JointPolicy, concurrency int, chunkSize int64, throughput float64) Action {
	return p.SelectAction(Context{Concurrency: concurrency, ChunkSize: chunkSize, Throughput: throughput})
}

func assertJointKind(t *testing.T, got Action, want ActionKind) {
	t.Helper()
	if got.Kind != want {
		t.Fatalf("action = %v, want %v", got, want)
	}
}

func TestJointPolicyFirstObservation(t *testing.T) {
	p := mustJoint(t, testJointConfig())
	assertJointKind(t, jointAct(p, 2, 32, 100), ActionNone)
}

func TestJointPolicyAlternatingImprovements(t *testing.T) {
	p := mustJoint(t, testJointConfig())
	jointAct(p, 2, 32, 100) // baseline

	assertJointKind(t, jointAct(p, 2, 32, 130), ActionIncreaseConcurrency)
	assertJointKind(t, jointAct(p, 3, 32, 170), ActionIncreaseChunkSize)
	assertJointKind(t, jointAct(p, 3, 64, 221), ActionIncreaseConcurrency)
}

func TestJointPolicyAlternatingDegradations(t *testing.T) {
	p := mustJoint(t, testJointConfig())
	jointAct(p, 3, 64, 100) // baseline

	assertJointKind(t, jointAct(p, 3, 64, 70), ActionDecreaseConcurrency)
	assertJointKind(t, jointAct(p, 2, 64, 49), ActionDecreaseChunkSize)
	assertJointKind(t, jointAct(p, 2, 32, 34.3), ActionDecreaseConcurrency)
}

func TestJointPolicyFallbacks(t *testing.T) {
	t.Run("concurrency at max falls back to chunk increase", func(t *testing.T) {
		cfg := testJointConfig()
		cfg.MaxConcurrency = 2
		p := mustJoint(t, cfg)
		jointAct(p, 2, 32, 100) // baseline, concurrency at max
		assertJointKind(t, jointAct(p, 2, 32, 130), ActionIncreaseChunkSize)
	})

	t.Run("chunk at max falls back to concurrency increase", func(t *testing.T) {
		cfg := testJointConfig()
		cfg.MaxChunkSize = 64
		p := mustJoint(t, cfg)
		jointAct(p, 2, 64, 100) // baseline
		// prefer concurrency first -> increase concurrency, advance preference
		assertJointKind(t, jointAct(p, 2, 64, 130), ActionIncreaseConcurrency)
		// prefer chunk, but chunk is at max -> fall back to concurrency
		assertJointKind(t, jointAct(p, 3, 64, 170), ActionIncreaseConcurrency)
	})

	t.Run("concurrency at min falls back to chunk decrease", func(t *testing.T) {
		cfg := testJointConfig()
		cfg.MinConcurrency = 1
		p := mustJoint(t, cfg)
		jointAct(p, 1, 64, 100) // baseline, concurrency at min
		assertJointKind(t, jointAct(p, 1, 64, 70), ActionDecreaseChunkSize)
	})

	t.Run("chunk at min falls back to concurrency decrease", func(t *testing.T) {
		cfg := testJointConfig()
		cfg.MinChunkSize = 16
		p := mustJoint(t, cfg)
		jointAct(p, 3, 16, 100) // baseline
		// prefer concurrency first -> decrease concurrency, advance preference
		assertJointKind(t, jointAct(p, 3, 16, 70), ActionDecreaseConcurrency)
		// prefer chunk, but chunk is at min -> fall back to concurrency
		assertJointKind(t, jointAct(p, 3, 16, 49), ActionDecreaseConcurrency)
	})

	t.Run("both at max returns no change", func(t *testing.T) {
		cfg := testJointConfig()
		cfg.MaxConcurrency = 2
		cfg.MaxChunkSize = 32
		p := mustJoint(t, cfg)
		jointAct(p, 2, 32, 100)
		assertJointKind(t, jointAct(p, 2, 32, 130), ActionNone)
	})

	t.Run("both at min returns no change", func(t *testing.T) {
		cfg := testJointConfig()
		cfg.MinConcurrency = 1
		cfg.MinChunkSize = 16
		p := mustJoint(t, cfg)
		jointAct(p, 1, 16, 100)
		assertJointKind(t, jointAct(p, 1, 16, 70), ActionNone)
	})
}

func TestJointPolicyZeroThroughput(t *testing.T) {
	t.Run("zero to zero", func(t *testing.T) {
		p := mustJoint(t, testJointConfig())
		assertJointKind(t, jointAct(p, 2, 32, 0), ActionNone)
		assertJointKind(t, jointAct(p, 2, 32, 0), ActionNone)
	})
	t.Run("zero to positive increases concurrency first", func(t *testing.T) {
		p := mustJoint(t, testJointConfig())
		assertJointKind(t, jointAct(p, 2, 32, 0), ActionNone)
		assertJointKind(t, jointAct(p, 2, 32, 50), ActionIncreaseConcurrency)
	})
	t.Run("positive to zero decreases concurrency first", func(t *testing.T) {
		p := mustJoint(t, testJointConfig())
		assertJointKind(t, jointAct(p, 3, 64, 100), ActionNone)
		assertJointKind(t, jointAct(p, 3, 64, 0), ActionDecreaseConcurrency)
	})
}

func TestJointPolicyThresholdBoundaries(t *testing.T) {
	cfg := testJointConfig()

	t.Run("exact +20% actionable", func(t *testing.T) {
		p := mustJoint(t, cfg)
		jointAct(p, 2, 32, 100)
		assertJointKind(t, jointAct(p, 2, 32, 120), ActionIncreaseConcurrency)
	})
	t.Run("+19.99% not actionable", func(t *testing.T) {
		p := mustJoint(t, cfg)
		jointAct(p, 2, 32, 100)
		assertJointKind(t, jointAct(p, 2, 32, 119.99), ActionNone)
	})
	t.Run("exact -20% actionable", func(t *testing.T) {
		p := mustJoint(t, cfg)
		jointAct(p, 3, 64, 100)
		assertJointKind(t, jointAct(p, 3, 64, 80), ActionDecreaseConcurrency)
	})
	t.Run("-19.99% not actionable", func(t *testing.T) {
		p := mustJoint(t, cfg)
		jointAct(p, 3, 64, 100)
		assertJointKind(t, jointAct(p, 3, 64, 80.01), ActionNone)
	})
}

func TestJointPolicyContextIsAuthoritative(t *testing.T) {
	cfg := testJointConfig()
	p := mustJoint(t, cfg)
	jointAct(p, 1, 32, 100) // baseline; context says concurrency is at min
	// Preference is concurrency; concurrency is at min -> fall back to chunk.
	assertJointKind(t, jointAct(p, 1, 64, 70), ActionDecreaseChunkSize)

	q := mustJoint(t, cfg)
	jointAct(q, 2, 128, 100) // baseline; context says chunk is at max
	// Preference is concurrency, which is available -> increase concurrency.
	assertJointKind(t, jointAct(q, 2, 128, 130), ActionIncreaseConcurrency)
}

func TestJointPolicyUpdateIsNoOp(t *testing.T) {
	p := mustJoint(t, testJointConfig())
	jointAct(p, 2, 32, 100) // baseline

	p.Update(Context{Concurrency: 2, ChunkSize: 32, Throughput: 500}, IncreaseConcurrency(), 1e9)
	p.Update(Context{}, Action{}, math.NaN())
	p.Update(Context{}, Action{}, math.Inf(1))

	assertJointKind(t, jointAct(p, 2, 32, 130), ActionIncreaseConcurrency)
}

func TestJointPolicyNeverReturnsSetActions(t *testing.T) {
	p := mustJoint(t, testJointConfig())
	obs := []struct {
		conc  int
		chunk int64
		tp    float64
	}{
		{2, 32, 100}, {2, 32, 130}, {3, 64, 170}, {4, 128, 221},
		{4, 128, 100}, {4, 128, 70}, {1, 16, 49},
	}
	for i, o := range obs {
		a := jointAct(p, o.conc, o.chunk, o.tp)
		switch a.Kind {
		case ActionNone, ActionIncreaseConcurrency, ActionDecreaseConcurrency,
			ActionIncreaseChunkSize, ActionDecreaseChunkSize:
		default:
			t.Fatalf("observation %d produced disallowed action %v", i, a)
		}
	}
}

func TestNewJointPolicyInvalidConfig(t *testing.T) {
	cases := map[string]func(*JointPolicyConfig){
		"min concurrency zero":      func(c *JointPolicyConfig) { c.MinConcurrency = 0 },
		"max concurrency below min": func(c *JointPolicyConfig) { c.MaxConcurrency = c.MinConcurrency - 1 },
		"min chunk zero":            func(c *JointPolicyConfig) { c.MinChunkSize = 0 },
		"max chunk below min":       func(c *JointPolicyConfig) { c.MaxChunkSize = c.MinChunkSize - 1 },
		"negative improvement":      func(c *JointPolicyConfig) { c.ImprovementThreshold = -0.1 },
		"negative degradation":      func(c *JointPolicyConfig) { c.DegradationThreshold = -0.1 },
		"nan improvement":           func(c *JointPolicyConfig) { c.ImprovementThreshold = math.NaN() },
		"inf degradation":           func(c *JointPolicyConfig) { c.DegradationThreshold = math.Inf(1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testJointConfig()
			mutate(&cfg)
			if _, err := NewJointPolicy(cfg); !errors.Is(err, ErrInvalidJointPolicyConfig) {
				t.Fatalf("NewJointPolicy = %v, want ErrInvalidJointPolicyConfig", err)
			}
		})
	}
}

func TestJointPolicyImplementsPolicy(t *testing.T) {
	var p Policy = mustJoint(t, testJointConfig())
	if got := p.SelectAction(Context{Concurrency: 2, ChunkSize: 32, Throughput: 1}); !got.IsNoChange() {
		t.Fatalf("SelectAction = %v, want no change on first observation", got)
	}
}
