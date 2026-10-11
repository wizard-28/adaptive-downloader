package dqn

import (
	"testing"

	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl"
)

func TestActionMapping(t *testing.T) {
	want := []struct {
		idx int
		act policy.Action
	}{
		{0, policy.NoChange()},
		{1, policy.IncreaseConcurrency()},
		{2, policy.DecreaseConcurrency()},
		{3, policy.IncreaseChunkSize()},
		{4, policy.DecreaseChunkSize()},
	}
	for _, w := range want {
		a, err := IndexToAction(w.idx)
		if err != nil {
			t.Fatalf("IndexToAction(%d) = %v", w.idx, err)
		}
		if a != w.act {
			t.Fatalf("IndexToAction(%d) = %v, want %v", w.idx, a, w.act)
		}
		got, err := ActionToIndex(w.act)
		if err != nil || got != w.idx {
			t.Fatalf("ActionToIndex(%v) = %d/%v, want %d", w.act, got, err, w.idx)
		}
	}
	// Set actions remain unsupported.
	if _, err := ActionToIndex(policy.SetConcurrency(2)); err == nil {
		t.Fatal("ActionToIndex(SetConcurrency) = nil, want error")
	}
	if _, err := IndexToAction(99); err == nil {
		t.Fatal("IndexToAction(99) = nil, want error")
	}
	if NumActions() != rl.NumActions {
		t.Fatalf("NumActions = %d, want %d", NumActions(), rl.NumActions)
	}
}

func TestPolicyAdapterGreedy(t *testing.T) {
	m := toyModel(t)
	p, err := NewPolicy(m)
	if err != nil {
		t.Fatalf("NewPolicy = %v", err)
	}
	obs := Observation{Concurrency: 4, ChunkSize: DefaultStateSpec().MinChunkSize, TotalBytes: 100, DoneBytes: 10}
	act, err := p.SelectAction(obs)
	if err != nil {
		t.Fatalf("SelectAction = %v", err)
	}
	if act.Kind != policy.ActionIncreaseConcurrency {
		t.Fatalf("action = %v, want increase-concurrency", act)
	}
	if _, _, ok := p.Last(); !ok {
		t.Fatal("Last() not populated")
	}
}

func TestPolicyRejectsNil(t *testing.T) {
	if _, err := NewPolicy(nil); err == nil {
		t.Fatal("NewPolicy(nil) = nil, want error")
	}
}
