package rl_test

import (
	"testing"
	"time"

	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl"
)

func mustLinUCB(t *testing.T, cfg rl.LinUCBConfig) *rl.LinUCB {
	t.Helper()
	l, err := rl.NewLinUCB(cfg)
	if err != nil {
		t.Fatalf("NewLinUCB(%+v) = %v", cfg, err)
	}
	return l
}

// TestLinUCBAdapterCompatibility verifies that a LinUCB learner wrapped by the
// existing Adapter produces valid policy.Action values and that the Adapter's
// Update path feeds the learner without corrupting it. This is a compatibility
// test only; LinUCB is NOT wired into the downloader's default policy.
func TestLinUCBAdapterCompatibility(t *testing.T) {
	adapter, err := rl.NewAdapter(mustLinUCB(t, rl.LinUCBConfig{
		Alpha:      0.2,
		NumActions: rl.NumActions,
		Dimension:  len(rl.FeatureNames()),
	}), integrationConfig())
	if err != nil {
		t.Fatalf("NewAdapter = %v", err)
	}

	ctxs := []policy.Context{
		{Concurrency: 1, ChunkSize: 1},
		{Concurrency: 2, ChunkSize: 100, Throughput: 1e6, RequestDuration: 100 * time.Millisecond, SuccessfulRequests: 3, TotalAttempts: 3},
		{Concurrency: 4, ChunkSize: 1 << 20, Throughput: 5e6, TotalAttempts: 9, Retries: 1},
	}

	for i, ctx := range ctxs {
		action := adapter.SelectAction(ctx)
		if _, err := rl.ActionIndexFromAction(action); err != nil {
			t.Fatalf("ctx %d: adapter produced non-decodable action %v: %v", i, action, err)
		}
		// The downloader interface's Update must not panic or corrupt the model.
		adapter.Update(ctx, action, 1.0)
	}

	// After training, selection must still be within the supported action space.
	action := adapter.SelectAction(ctxs[1])
	if _, err := rl.ActionIndexFromAction(action); err != nil {
		t.Fatalf("post-update action not decodable: %v", err)
	}
	if _, _, ok := adapter.Last(); !ok {
		t.Fatal("Adapter.Last() reported no decision")
	}
}
