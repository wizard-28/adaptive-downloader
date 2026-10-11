package policy

import (
	"errors"
	"math"
	"testing"
)

func testChunkPolicyConfig() ChunkSizePolicyConfig {
	return ChunkSizePolicyConfig{
		MinChunkSize:         16,
		MaxChunkSize:         128,
		ImprovementThreshold: 0.10,
		DegradationThreshold: 0.10,
	}
}

func mustChunkPolicy(t *testing.T, cfg ChunkSizePolicyConfig) *ChunkSizePolicy {
	t.Helper()
	p, err := NewChunkSizePolicy(cfg)
	if err != nil {
		t.Fatalf("NewChunkSizePolicy(%+v) = %v", cfg, err)
	}
	return p
}

// chunkAct feeds one throughput observation at the given chunk size.
func chunkAct(p *ChunkSizePolicy, chunkSize int64, throughput float64) Action {
	return p.SelectAction(Context{ChunkSize: chunkSize, Throughput: throughput})
}

func assertChunkKind(t *testing.T, got Action, want ActionKind) {
	t.Helper()
	if got.Kind != want {
		t.Fatalf("action = %v, want kind %v", got, want)
	}
}

func TestChunkSizePolicyFirstObservation(t *testing.T) {
	p := mustChunkPolicy(t, testChunkPolicyConfig())
	assertChunkKind(t, chunkAct(p, 32, 100), ActionNone)
	assertChunkKind(t, chunkAct(p, 32, 100), ActionNone)
}

func TestChunkSizePolicyIncrease(t *testing.T) {
	p := mustChunkPolicy(t, testChunkPolicyConfig())
	chunkAct(p, 32, 100) // baseline
	assertChunkKind(t, chunkAct(p, 32, 130), ActionIncreaseChunkSize)
}

func TestChunkSizePolicyDecrease(t *testing.T) {
	p := mustChunkPolicy(t, testChunkPolicyConfig())
	chunkAct(p, 32, 100) // baseline
	assertChunkKind(t, chunkAct(p, 32, 70), ActionDecreaseChunkSize)
}

func TestChunkSizePolicySmallChangesIgnored(t *testing.T) {
	t.Run("small increase", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		chunkAct(p, 32, 100)
		assertChunkKind(t, chunkAct(p, 32, 105), ActionNone)
	})
	t.Run("small decrease", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		chunkAct(p, 32, 100)
		assertChunkKind(t, chunkAct(p, 32, 95), ActionNone)
	})
}

func TestChunkSizePolicyRespectsBounds(t *testing.T) {
	t.Run("at max never increases", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		chunkAct(p, 128, 100)
		assertChunkKind(t, chunkAct(p, 128, 1000), ActionNone)
	})
	t.Run("at min never decreases", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		chunkAct(p, 16, 100)
		assertChunkKind(t, chunkAct(p, 16, 1), ActionNone)
	})
}

func TestChunkSizePolicyZeroThroughput(t *testing.T) {
	t.Run("zero to zero", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		assertChunkKind(t, chunkAct(p, 32, 0), ActionNone)
		assertChunkKind(t, chunkAct(p, 32, 0), ActionNone)
	})
	t.Run("zero to positive increases", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		assertChunkKind(t, chunkAct(p, 32, 0), ActionNone)
		assertChunkKind(t, chunkAct(p, 32, 50), ActionIncreaseChunkSize)
	})
	t.Run("zero to positive at max stays", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		assertChunkKind(t, chunkAct(p, 128, 0), ActionNone)
		assertChunkKind(t, chunkAct(p, 128, 50), ActionNone)
	})
	t.Run("positive to zero decreases", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		assertChunkKind(t, chunkAct(p, 32, 100), ActionNone)
		assertChunkKind(t, chunkAct(p, 32, 0), ActionDecreaseChunkSize)
	})
	t.Run("positive to zero at min stays", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		assertChunkKind(t, chunkAct(p, 16, 100), ActionNone)
		assertChunkKind(t, chunkAct(p, 16, 0), ActionNone)
	})
}

func TestChunkSizePolicyThresholdBoundary(t *testing.T) {
	t.Run("exact improvement threshold increases", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		chunkAct(p, 32, 100)
		assertChunkKind(t, chunkAct(p, 32, 110), ActionIncreaseChunkSize)
	})
	t.Run("just below improvement threshold ignored", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		chunkAct(p, 32, 100)
		assertChunkKind(t, chunkAct(p, 32, 109.99), ActionNone)
	})
	t.Run("exact degradation threshold decreases", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		chunkAct(p, 32, 100)
		assertChunkKind(t, chunkAct(p, 32, 90), ActionDecreaseChunkSize)
	})
	t.Run("just above degradation threshold ignored", func(t *testing.T) {
		p := mustChunkPolicy(t, testChunkPolicyConfig())
		chunkAct(p, 32, 100)
		assertChunkKind(t, chunkAct(p, 32, 90.01), ActionNone)
	})
	t.Run("zero thresholds react to any change", func(t *testing.T) {
		cfg := testChunkPolicyConfig()
		cfg.ImprovementThreshold = 0
		cfg.DegradationThreshold = 0
		p := mustChunkPolicy(t, cfg)
		chunkAct(p, 32, 100)
		assertChunkKind(t, chunkAct(p, 32, 100.001), ActionIncreaseChunkSize)

		q := mustChunkPolicy(t, cfg)
		chunkAct(q, 32, 100)
		assertChunkKind(t, chunkAct(q, 32, 99.999), ActionDecreaseChunkSize)
	})
}

func TestChunkSizePolicyRepeatedObservations(t *testing.T) {
	p := mustChunkPolicy(t, testChunkPolicyConfig())
	steps := []struct {
		throughput float64
		want       ActionKind
	}{
		{100, ActionNone},              // baseline
		{130, ActionIncreaseChunkSize}, // +30%
		{130, ActionNone},              // 0%
		{90, ActionDecreaseChunkSize},  // -30.8%
		{91, ActionNone},               // +1.1%
	}
	for _, step := range steps {
		assertChunkKind(t, chunkAct(p, 32, step.throughput), step.want)
	}
}

func TestChunkSizePolicyNeverReturnsConcurrencyActions(t *testing.T) {
	p := mustChunkPolicy(t, testChunkPolicyConfig())
	for _, tp := range []float64{0, 100, 130, 130, 90, 91, 0, 50, 200, 10} {
		a := chunkAct(p, 32, tp)
		switch a.Kind {
		case ActionNone, ActionIncreaseChunkSize, ActionDecreaseChunkSize:
		default:
			t.Fatalf("throughput %v produced disallowed action %v", tp, a)
		}
	}
}

func TestChunkSizePolicyUpdateIsNoOp(t *testing.T) {
	p := mustChunkPolicy(t, testChunkPolicyConfig())
	chunkAct(p, 32, 100) // baseline

	// Update must not change the comparison point.
	p.Update(Context{ChunkSize: 32, Throughput: 500}, IncreaseChunkSize(), 1e9)
	p.Update(Context{}, Action{}, math.NaN())
	p.Update(Context{}, Action{}, math.Inf(1))

	assertChunkKind(t, chunkAct(p, 32, 130), ActionIncreaseChunkSize)
}

func TestNewChunkSizePolicyInvalidConfig(t *testing.T) {
	cases := map[string]func(*ChunkSizePolicyConfig){
		"min zero":             func(c *ChunkSizePolicyConfig) { c.MinChunkSize = 0 },
		"min negative":         func(c *ChunkSizePolicyConfig) { c.MinChunkSize = -1 },
		"max below min":        func(c *ChunkSizePolicyConfig) { c.MaxChunkSize = c.MinChunkSize - 1 },
		"negative improvement": func(c *ChunkSizePolicyConfig) { c.ImprovementThreshold = -0.1 },
		"negative degradation": func(c *ChunkSizePolicyConfig) { c.DegradationThreshold = -0.1 },
		"nan improvement":      func(c *ChunkSizePolicyConfig) { c.ImprovementThreshold = math.NaN() },
		"inf degradation":      func(c *ChunkSizePolicyConfig) { c.DegradationThreshold = math.Inf(1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testChunkPolicyConfig()
			mutate(&cfg)
			if _, err := NewChunkSizePolicy(cfg); !errors.Is(err, ErrInvalidChunkSizePolicyConfig) {
				t.Fatalf("NewChunkSizePolicy = %v, want ErrInvalidChunkSizePolicyConfig", err)
			}
		})
	}
}

func TestChunkSizePolicyImplementsPolicy(t *testing.T) {
	var p Policy = mustChunkPolicy(t, testChunkPolicyConfig())
	if got := p.SelectAction(Context{ChunkSize: 32, Throughput: 1}); !got.IsNoChange() {
		t.Fatalf("SelectAction = %v, want no change on first observation", got)
	}
}
