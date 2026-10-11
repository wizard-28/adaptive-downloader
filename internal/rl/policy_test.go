package rl

import (
	"sync"
	"testing"
	"time"

	"adaptivedl/internal/policy"
)

type updateCall struct {
	state  State
	action ActionIndex
	reward Reward
}

type recordingRLPolicy struct {
	mu           sync.Mutex
	action       ActionIndex
	selectStates []State
	updates      []updateCall
}

func (p *recordingRLPolicy) SelectAction(s State) ActionIndex {
	p.mu.Lock()
	p.selectStates = append(p.selectStates, s)
	p.mu.Unlock()
	return p.action
}

func (p *recordingRLPolicy) Update(s State, a ActionIndex, r Reward) {
	p.mu.Lock()
	p.updates = append(p.updates, updateCall{state: s, action: a, reward: r})
	p.mu.Unlock()
}

func (p *recordingRLPolicy) snapshot() ([]State, []updateCall) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]State(nil), p.selectStates...), append([]updateCall(nil), p.updates...)
}

func sampleContext() policy.Context {
	return policy.Context{
		Concurrency: 2, ChunkSize: 32, Throughput: 500,
		RequestDuration: 250 * time.Millisecond, BytesTransferred: 40,
		SuccessfulRequests: 5, FailedRequests: 1, Retries: 1, TotalAttempts: 6,
	}
}

func TestAdapterSelectActionConverts(t *testing.T) {
	rec := &recordingRLPolicy{action: ActionIncreaseChunkSize}
	adapter, err := NewAdapter(rec, testConfig())
	if err != nil {
		t.Fatalf("NewAdapter = %v", err)
	}

	action := adapter.SelectAction(sampleContext())
	if action.Kind != policy.ActionIncreaseChunkSize {
		t.Fatalf("action kind = %d, want IncreaseChunkSize", action.Kind)
	}

	states, _ := rec.snapshot()
	if len(states) != 1 {
		t.Fatalf("select states = %d, want 1", len(states))
	}
	if states[0].Concurrency != 2 || states[0].ChunkSize != 32 || states[0].Throughput != 500 {
		t.Fatalf("converted state = %+v", states[0])
	}
	if states[0].RequestDuration != 250*time.Millisecond || states[0].BytesTransferred != 40 {
		t.Fatalf("converted state = %+v", states[0])
	}

	last, index, ok := adapter.Last()
	if !ok || index != ActionIncreaseChunkSize || last != states[0] {
		t.Fatalf("Last() = %+v, %d, %v", last, index, ok)
	}
}

func TestAdapterUpdateConverts(t *testing.T) {
	rec := &recordingRLPolicy{}
	adapter, err := NewAdapter(rec, testConfig())
	if err != nil {
		t.Fatalf("NewAdapter = %v", err)
	}

	adapter.Update(sampleContext(), policy.IncreaseConcurrency(), 0.5)

	_, updates := rec.snapshot()
	if len(updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(updates))
	}
	if updates[0].action != ActionIncreaseConcurrency {
		t.Fatalf("update action = %d, want IncreaseConcurrency", updates[0].action)
	}
	if updates[0].reward.Value != 0.5 {
		t.Fatalf("update reward value = %v, want 0.5", updates[0].reward.Value)
	}
	if updates[0].state.ChunkSize != 32 {
		t.Fatalf("update state = %+v", updates[0].state)
	}
}

func TestAdapterUpdateIgnoresUnsupportedAction(t *testing.T) {
	rec := &recordingRLPolicy{}
	adapter, _ := NewAdapter(rec, testConfig())

	adapter.Update(sampleContext(), policy.SetConcurrency(4), 0)
	adapter.Update(sampleContext(), policy.SetChunkSize(64), 0)

	if _, updates := rec.snapshot(); len(updates) != 0 {
		t.Fatalf("updates = %d, want 0 for unsupported actions", len(updates))
	}
}

func TestAdapterFallbackOnInvalidState(t *testing.T) {
	rec := &recordingRLPolicy{action: ActionIncreaseConcurrency}
	adapter, _ := NewAdapter(rec, testConfig())

	action := adapter.SelectAction(policy.Context{Concurrency: -1})
	if !action.IsNoChange() {
		t.Fatalf("action = %v, want no change on invalid state", action)
	}
	if states, _ := rec.snapshot(); len(states) != 0 {
		t.Fatalf("RL policy called with invalid state: %+v", states)
	}
}

func TestAdapterInvalidConfig(t *testing.T) {
	if _, err := NewAdapter(nil, testConfig()); err == nil {
		t.Fatal("NewAdapter(nil) = nil, want error")
	}
	bad := testConfig()
	bad.MaxConcurrency = bad.MinConcurrency - 1
	if _, err := NewAdapter(AlwaysActionPolicy{}, bad); err == nil {
		t.Fatal("NewAdapter(bad cfg) = nil, want error")
	}
}

func TestAlwaysActionPolicy(t *testing.T) {
	p := AlwaysActionPolicy{Action: ActionDecreaseConcurrency}
	if got := p.SelectAction(State{}); got != ActionDecreaseConcurrency {
		t.Fatalf("SelectAction = %d, want DecreaseConcurrency", got)
	}
	p.Update(State{}, ActionNone, Reward{}) // must not panic
}
