package diagnostics

import (
	"fmt"
	"testing"

	"adaptivedl/internal/rl"
)

// This test demonstrates the diagnostic finding and the correction. It uses the
// public LinUCB API only and does not touch the formal benchmark. Run with
// `go test -run TestExplorationCorrection -v ./internal/diagnostics/` to capture
// the before/after sequences.

const (
	corrMaxConcurrency = 4
	corrMaxChunk       = int64(128 << 10)
	corrMinChunk       = int64(32 << 10)
	corrChunkStep      = int64(32 << 10)
	corrReward         = 3.4 // measured baseline ActionNone reward
)

func corrState() rl.State {
	return rl.State{
		Concurrency:         1,
		ChunkSize:           corrMinChunk,
		Throughput:          3.5e6,
		AggregateThroughput: 3.5e6,
		BytesTransferred:    32768,
		SuccessfulRequests:  1,
		TotalAttempts:       1,
	}
}

func corrApply(s rl.State, a rl.ActionIndex) rl.State {
	switch a {
	case rl.ActionIncreaseConcurrency:
		if s.Concurrency < corrMaxConcurrency {
			s.Concurrency++
		}
	case rl.ActionDecreaseConcurrency:
		if s.Concurrency > 1 {
			s.Concurrency--
		}
	case rl.ActionIncreaseChunkSize:
		if s.ChunkSize < corrMaxChunk {
			s.ChunkSize += corrChunkStep
		}
	case rl.ActionDecreaseChunkSize:
		if s.ChunkSize > corrMinChunk {
			s.ChunkSize -= corrChunkStep
		}
	}
	return s
}

func corrSequence(t *testing.T, rounds, n int) []rl.ActionIndex {
	t.Helper()
	cfg := rl.LinUCBConfig{Alpha: 0.2, NumActions: rl.NumActions, Dimension: len(rl.FeatureNames()), InitialExplorationRounds: rounds}
	l, err := rl.NewLinUCB(cfg)
	if err != nil {
		t.Fatalf("NewLinUCB = %v", err)
	}
	s := corrState()
	actions := make([]rl.ActionIndex, 0, n)
	for i := 0; i < n; i++ {
		a := l.SelectAction(s)
		actions = append(actions, a)
		l.Update(s, a, rl.Reward{Value: corrReward})
		s = corrApply(s, a)
	}
	return actions
}

func TestExplorationCorrectionBeforeAndAfter(t *testing.T) {
	before := corrSequence(t, 0, 8) // no initial exploration (previous behaviour)
	after := corrSequence(t, 1, 8)  // one initial-exploration pass

	fmt.Printf("LinUCB exploration correction diagnostic (public API; no download)\n")
	fmt.Printf("state: concurrency=1 chunk=32768 reward=%.1f per step, alpha=0.2\n", corrReward)
	fmt.Printf("BEFORE (InitialExplorationRounds=0): %v\n", before)
	fmt.Printf("AFTER  (InitialExplorationRounds=1): %v\n", after)

	// Before: every selection is ActionNone (lock-in).
	for i, a := range before {
		if a != rl.ActionNone {
			t.Fatalf("before correction, selection %d = %s, want none (lock-in)", i, a)
		}
	}
	// After: the first NumActions decisions cover every action exactly once.
	seen := map[rl.ActionIndex]bool{}
	for _, a := range after[:rl.NumActions] {
		seen[a] = true
	}
	if len(seen) != rl.NumActions {
		t.Fatalf("after correction, warm-up covered %d actions, want %d", len(seen), rl.NumActions)
	}
	for a := rl.ActionIndex(0); a < rl.NumActions; a++ {
		if !seen[a] {
			t.Fatalf("after correction, action %s never selected during warm-up", a)
		}
	}
}
