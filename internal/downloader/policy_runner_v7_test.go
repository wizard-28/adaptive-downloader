package downloader

import (
	"testing"
	"time"

	"adaptivedl/internal/concurrency"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/policy"
)

type fixedActionPolicy struct {
	action policy.Action
}

func (p *fixedActionPolicy) SelectAction(policy.Context) policy.Action     { return p.action }
func (p *fixedActionPolicy) Update(policy.Context, policy.Action, float64) {}

func observeOnce(t *testing.T, p policy.Policy, ctrl *concurrency.Controller) {
	t.Helper()
	r := newPolicyRunner(p, ctrl, nil, 1<<20, 4, nil, nil)
	r.observe(metrics.Record{
		RequestID: "a", RangeStart: 0, RangeEnd: 0, Bytes: 1,
		Duration: time.Millisecond, Success: true, Attempt: 1,
	})
}

// TestPolicyRunnerActionStepV7 locks the v7 control-authority contract: a
// positive Action.Value is applied atomically as that many controller steps
// (clamped by the controller), while Value <= 0 keeps the historical single
// step used by every v1-v6 policy.
func TestPolicyRunnerActionStepV7(t *testing.T) {
	ctrl, err := concurrency.New(1, 16)
	if err != nil {
		t.Fatalf("concurrency.New = %v", err)
	}
	observeOnce(t, &fixedActionPolicy{action: policy.Action{Kind: policy.ActionIncreaseConcurrency, Value: 4}}, ctrl)
	if got := ctrl.Current(); got != 5 {
		t.Fatalf("step +4 from 1 -> %d, want 5", got)
	}
	observeOnce(t, &fixedActionPolicy{action: policy.Action{Kind: policy.ActionIncreaseConcurrency, Value: 0}}, ctrl)
	if got := ctrl.Current(); got != 6 {
		t.Fatalf("Value 0 must be a single step -> %d, want 6", got)
	}
	observeOnce(t, &fixedActionPolicy{action: policy.Action{Kind: policy.ActionIncreaseConcurrency, Value: 4}}, ctrl)
	observeOnce(t, &fixedActionPolicy{action: policy.Action{Kind: policy.ActionIncreaseConcurrency, Value: 4}}, ctrl)
	observeOnce(t, &fixedActionPolicy{action: policy.Action{Kind: policy.ActionIncreaseConcurrency, Value: 4}}, ctrl)
	if got := ctrl.Current(); got != 16 {
		t.Fatalf("steps must clamp at max -> %d, want 16", got)
	}
	observeOnce(t, &fixedActionPolicy{action: policy.Action{Kind: policy.ActionDecreaseConcurrency, Value: 4}}, ctrl)
	if got := ctrl.Current(); got != 12 {
		t.Fatalf("step -4 -> %d, want 12", got)
	}
	observeOnce(t, &fixedActionPolicy{action: policy.Action{Kind: policy.ActionDecreaseConcurrency, Value: 4}}, ctrl)
	observeOnce(t, &fixedActionPolicy{action: policy.Action{Kind: policy.ActionDecreaseConcurrency, Value: 4}}, ctrl)
	observeOnce(t, &fixedActionPolicy{action: policy.Action{Kind: policy.ActionDecreaseConcurrency, Value: 4}}, ctrl)
	if got := ctrl.Current(); got != 1 {
		t.Fatalf("steps must clamp at min -> %d, want 1", got)
	}
}
