package rl_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"adaptivedl/internal/downloader"
	expserver "adaptivedl/internal/experiment/server"
	"adaptivedl/internal/rl"
)

// recordingLearner is a deterministic RL policy that records the State passed
// to SelectAction, the ActionIndex it returned, and every (State, ActionIndex,
// Reward) passed to Update. It performs no learning; it exists to prove the
// downloader's causal ordering and reward association through the real Adapter.
type recordingLearner struct {
	mu      sync.Mutex
	actions []rl.ActionIndex
	call    int

	selectStates []rl.State
	returned     []rl.ActionIndex
	updates      []learnerUpdate
}

type learnerUpdate struct {
	state  rl.State
	action rl.ActionIndex
	reward rl.Reward
}

func (l *recordingLearner) SelectAction(s rl.State) rl.ActionIndex {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.selectStates = append(l.selectStates, s)
	a := rl.ActionNone
	if len(l.actions) > 0 {
		a = l.actions[l.call%len(l.actions)]
	}
	l.call++
	l.returned = append(l.returned, a)
	return a
}

func (l *recordingLearner) Update(s rl.State, a rl.ActionIndex, reward rl.Reward) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.updates = append(l.updates, learnerUpdate{state: s, action: a, reward: reward})
}

func (l *recordingLearner) snapshot() (selects []rl.State, returned []rl.ActionIndex, updates []learnerUpdate) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]rl.State(nil), l.selectStates...),
		append([]rl.ActionIndex(nil), l.returned...),
		append([]learnerUpdate(nil), l.updates...)
}

// TestAdapterCausalOrderingAndRewardAssociation proves the full chain through
// the real Adapter and downloader:
//
//	Select_t -> execute -> Observation_{t+1} -> Reward_t -> Update(State_t, Action_t, Reward_t)
//
// It is constructed so that an off-by-one (crediting the newly selected action)
// would fail: Update_i must carry the State and ActionIndex of Select_i, not
// Select_{i+1}.
func TestAdapterCausalOrderingAndRewardAssociation(t *testing.T) {
	content := contentPattern(512)
	srv := expserver.New(content)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	// A scripted, deterministic action sequence. Distinct actions let the test
	// detect misattribution.
	learner := &recordingLearner{actions: []rl.ActionIndex{
		rl.ActionNone,
		rl.ActionIncreaseConcurrency,
		rl.ActionIncreaseChunkSize,
		rl.ActionNone,
	}}
	adapter, err := rl.NewAdapter(learner, integrationConfig())
	if err != nil {
		t.Fatalf("NewAdapter = %v", err)
	}

	rewardCfg := rl.DefaultRewardConfig()
	out := filepath.Join(t.TempDir(), "out.bin")
	dl, err := downloader.New(downloader.Config{
		URL:              ts.URL,
		OutputPath:       out,
		TotalSize:        int64(len(content)),
		Concurrency:      1,
		ChunkSize:        64,
		MinConcurrency:   1,
		MaxConcurrency:   4,
		MinChunkSize:     64,
		MaxChunkSize:     256,
		InitialChunkSize: 64,
		ChunkSizeStep:    64,
		MaxRetries:       3,
		Timeout:          5 * time.Second,
		PolicyWindowSize: 4,
		Policy:           adapter,
		RewardConfig:     &rewardCfg,
	})
	if err != nil {
		t.Fatalf("downloader.New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}

	selects, returned, updates := learner.snapshot()
	if len(selects) < 2 {
		t.Fatalf("SelectAction calls = %d, want >= 2", len(selects))
	}
	// First observation selects an action but produces no Update yet.
	if len(updates) != len(selects)-1 {
		t.Fatalf("Update calls = %d, want SelectAction-1 = %d", len(updates), len(selects)-1)
	}

	for i, u := range updates {
		// The state credited must be the state at which the action was chosen,
		// not the next observation's state.
		if u.state != selects[i] {
			t.Fatalf("Update[%d] state = %+v, want Select[%d] state %+v", i, u.state, i, selects[i])
		}
		// The action credited must be the action chosen at that state.
		if u.action != returned[i] {
			t.Fatalf("Update[%d] action = %v, want Select[%d] action %v", i, u.action, i, returned[i])
		}
		// The reward must be finite (the calculator never emits NaN/Inf).
		if err := u.reward.Validate(); err != nil {
			t.Fatalf("Update[%d] reward invalid: %v", i, err)
		}
	}

	// Distinguishable actions must not be misattributed: if Update carried the
	// *next* action, at least one pair below would mismatch.
	for i := 0; i+1 < len(returned); i++ {
		if updates[i].action != returned[i] {
			t.Fatalf("credit-assignment off-by-one at %d", i)
		}
	}
}

// TestAdapterFirstObservationNoUpdate verifies that a single-observation run
// performs exactly one SelectAction and zero Update calls (no fabricated
// previous observation). A one-byte range yields exactly one attempt.
func TestAdapterFirstObservationNoUpdate(t *testing.T) {
	content := contentPattern(8)
	srv := expserver.New(content)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	learner := &recordingLearner{}
	adapter, err := rl.NewAdapter(learner, integrationConfig())
	if err != nil {
		t.Fatalf("NewAdapter = %v", err)
	}
	rewardCfg := rl.DefaultRewardConfig()
	out := filepath.Join(t.TempDir(), "out.bin")
	dl, err := downloader.New(downloader.Config{
		URL:              ts.URL,
		OutputPath:       out,
		TotalSize:        int64(len(content)),
		Concurrency:      1,
		ChunkSize:        int64(len(content)), // one range -> one attempt
		MinConcurrency:   1,
		MaxConcurrency:   4,
		MinChunkSize:     int64(len(content)),
		MaxChunkSize:     int64(len(content)),
		InitialChunkSize: int64(len(content)),
		ChunkSizeStep:    1,
		MaxRetries:       0,
		Timeout:          5 * time.Second,
		PolicyWindowSize: 4,
		Policy:           adapter,
		RewardConfig:     &rewardCfg,
	})
	if err != nil {
		t.Fatalf("downloader.New = %v", err)
	}
	if err := dl.Download(context.Background()); err != nil {
		t.Fatalf("Download = %v", err)
	}

	selects, _, updates := learner.snapshot()
	if len(selects) != 1 {
		t.Fatalf("SelectAction calls = %d, want 1", len(selects))
	}
	if len(updates) != 0 {
		t.Fatalf("Update calls = %d, want 0 (no previous observation)", len(updates))
	}
}
