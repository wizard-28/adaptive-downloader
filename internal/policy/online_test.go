package policy

import (
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newTestTuner(t *testing.T, clk *fakeClock) *OnlineTunerPolicy {
	t.Helper()
	p, err := NewOnlineTunerPolicy(OnlineTunerConfig{
		MinConcurrency: 1, MaxConcurrency: 16, Interval: 2 * time.Second,
		InitialStep: 4, Penalty: 1, Tolerance: 0.05, Now: clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// window feeds one attempt at the start of a window and one at its end, so
// the window's aggregate throughput is bytes/2s.
func window(p *OnlineTunerPolicy, clk *fakeClock, n int, bytes int64) Action {
	p.SelectAction(Context{Concurrency: n})
	clk.t = clk.t.Add(2 * time.Second)
	return p.SelectAction(Context{Concurrency: n, BytesTransferred: bytes})
}

func TestOnlineTunerNoChangeInsideWindow(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := newTestTuner(t, clk)
	p.SelectAction(Context{Concurrency: 4, BytesTransferred: 100})
	clk.t = clk.t.Add(time.Second)
	if a := p.SelectAction(Context{Concurrency: 4, BytesTransferred: 100}); a.Kind != ActionNone {
		t.Fatalf("inside the window got %v, want none", a)
	}
}

func TestOnlineTunerClimbsWhileImproving(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := newTestTuner(t, clk)
	a := window(p, clk, 4, 1000)
	if a.Kind != ActionIncreaseConcurrency || a.Value != 4 {
		t.Fatalf("first window got %+v, want +4", a)
	}
	a = window(p, clk, 8, 2000)
	if a.Kind != ActionIncreaseConcurrency || a.Value != 4 {
		t.Fatalf("improving window got %+v, want +4", a)
	}
}

func TestOnlineTunerReversesAndHalvesOnWorse(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := newTestTuner(t, clk)
	window(p, clk, 4, 1000) // -> 8
	a := window(p, clk, 8, 500)
	if a.Kind != ActionDecreaseConcurrency || a.Value != 2 {
		t.Fatalf("worse window got %+v, want -2", a)
	}
	// A drop inside the tolerance keeps the direction and step.
	a = window(p, clk, 6, 480)
	if a.Kind != ActionDecreaseConcurrency || a.Value != 2 {
		t.Fatalf("within-tolerance window got %+v, want -2", a)
	}
}

func TestOnlineTunerHoldsAtBound(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := newTestTuner(t, clk)
	if a := window(p, clk, 16, 1000); a.Kind != ActionNone {
		t.Fatalf("at max got %+v, want none", a)
	}
	if a := window(p, clk, 16, 1000); a.Kind != ActionNone {
		t.Fatalf("steady at max got %+v, want none", a)
	}
	if a := window(p, clk, 16, 100); a.Kind != ActionDecreaseConcurrency || a.Value != 2 {
		t.Fatalf("worse at max got %+v, want -2", a)
	}
}

func TestOnlineTunerPenaltyPrefersFewerConnections(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	p, err := NewOnlineTunerPolicy(OnlineTunerConfig{
		MinConcurrency: 1, MaxConcurrency: 16, Interval: 2 * time.Second,
		InitialStep: 4, Penalty: 1.2, Tolerance: 0.05, Now: clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	window(p, clk, 4, 1000) // -> 8
	// Same throughput at 8 connections scores 1.2^4 ~ 2x worse: reverse.
	if a := window(p, clk, 8, 1000); a.Kind != ActionDecreaseConcurrency {
		t.Fatalf("penalized window got %+v, want a decrease", a)
	}
}

func TestOnlineTunerNeverChangesChunkSize(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := newTestTuner(t, clk)
	for i, b := range []int64{1000, 3000, 100, 5000, 50} {
		a := window(p, clk, 8, b)
		if a.Kind == ActionIncreaseChunkSize || a.Kind == ActionDecreaseChunkSize {
			t.Fatalf("window %d returned a chunk action %+v", i, a)
		}
	}
}

func TestNewOnlineTunerInvalidConfig(t *testing.T) {
	base := OnlineTunerConfig{MinConcurrency: 1, MaxConcurrency: 16, Interval: time.Second, InitialStep: 1, Penalty: 1}
	bad := []func(*OnlineTunerConfig){
		func(c *OnlineTunerConfig) { c.MinConcurrency = 0 },
		func(c *OnlineTunerConfig) { c.MaxConcurrency = 0 },
		func(c *OnlineTunerConfig) { c.Interval = 0 },
		func(c *OnlineTunerConfig) { c.InitialStep = 0 },
		func(c *OnlineTunerConfig) { c.Penalty = 0.9 },
		func(c *OnlineTunerConfig) { c.Tolerance = -1 },
	}
	for i, mut := range bad {
		c := base
		mut(&c)
		if _, err := NewOnlineTunerPolicy(c); !errors.Is(err, ErrInvalidOnlineConfig) {
			t.Errorf("case %d: got %v, want ErrInvalidOnlineConfig", i, err)
		}
	}
}
