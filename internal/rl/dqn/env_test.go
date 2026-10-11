package dqn

import (
	"testing"
	"time"
)

func simConfig() SimConfig {
	return SimConfig{
		FileSize:        512 << 10,
		MinConcurrency:  1,
		MaxConcurrency:  4,
		MinChunkSize:    32 << 10,
		MaxChunkSize:    128 << 10,
		ChunkStep:       32 << 10,
		Bandwidth:       4 << 20,
		Bandwidth2:      1 << 20,
		SwitchAfter:     4,
		Latency:         20 * time.Millisecond,
		LossPercent:     0,
		MaxEpisodeSteps: 200,
	}
}

func TestSimResetAndStep(t *testing.T) {
	env, err := NewSimEnv(simConfig(), 1)
	if err != nil {
		t.Fatalf("NewSimEnv = %v", err)
	}
	s0, err := env.Reset()
	if err != nil {
		t.Fatalf("Reset = %v", err)
	}
	if len(s0) != NumFeatures() {
		t.Fatalf("state len = %d", len(s0))
	}
	res, s1, err := env.Step(1) // increase concurrency
	if err != nil {
		t.Fatalf("Step = %v", err)
	}
	if res.Reward >= 0 {
		t.Fatalf("reward = %v, want negative elapsed seconds", res.Reward)
	}
	if res.Done {
		t.Fatal("episode done too early")
	}
	if len(s1) != NumFeatures() {
		t.Fatalf("next state len = %d", len(s1))
	}
}

func TestSimDeterministic(t *testing.T) {
	run := func() []float64 {
		env, _ := NewSimEnv(simConfig(), 42)
		_, _ = env.Reset()
		for i := 0; i < 10; i++ {
			_, _, _ = env.Step(i % NumActions())
		}
		s, _ := env.State().Features(DefaultStateSpec())
		return s
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sim not deterministic at %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestSimTerminatesAndRewardIsTime(t *testing.T) {
	env, _ := NewSimEnv(simConfig(), 7)
	_, _ = env.Reset()
	total := 0.0
	steps := 0
	for !env.Done() && steps < 1000 {
		res, _, err := env.Step(0)
		if err != nil {
			t.Fatalf("Step = %v", err)
		}
		total += res.Reward
		steps++
	}
	if !env.Done() {
		t.Fatal("episode never terminated")
	}
	if total >= 0 {
		t.Fatalf("undiscounted return = %v, want negative (negative download time)", total)
	}
	if s := env.State(); s.DoneBytes != s.TotalBytes {
		t.Fatalf("done bytes = %d, want total %d at completion", s.DoneBytes, s.TotalBytes)
	}
}

func TestSimTruncation(t *testing.T) {
	cfg := simConfig()
	cfg.FileSize = 100 << 20 // huge so it cannot finish in the step bound
	cfg.MaxEpisodeSteps = 5
	env, _ := NewSimEnv(cfg, 3)
	_, _ = env.Reset()
	for i := 0; i < 5; i++ {
		if _, _, err := env.Step(0); err != nil {
			t.Fatalf("Step = %v", err)
		}
	}
	if !env.Done() {
		t.Fatal("episode not truncated at max steps")
	}
}

func TestSimMalformedStep(t *testing.T) {
	env, _ := NewSimEnv(simConfig(), 1)
	_, _ = env.Reset()
	if _, _, err := env.Step(99); err == nil {
		t.Fatal("Step(99) = nil, want error")
	}
}

func TestSimRandomizedConditions(t *testing.T) {
	// Different seeds produce different initial concurrency / trajectories.
	seen := map[int]bool{}
	for seed := int64(0); seed < 20; seed++ {
		env, _ := NewSimEnv(simConfig(), seed)
		_, _ = env.Reset()
		seen[env.concurrency] = true
	}
	if len(seen) < 2 {
		t.Fatalf("randomized initial concurrency did not vary: %v", seen)
	}
}
