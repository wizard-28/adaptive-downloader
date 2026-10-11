package rl

import (
	"reflect"
	"testing"
)

// The controller bounds below mirror the experiment's locked configuration, so
// the simulated controller effect in these tests matches the real downloader.
const (
	testMaxConcurrency = 4
	testMaxChunkSize   = int64(128 << 10)
	testMinChunkSize   = int64(32 << 10)
	testChunkStep      = int64(32 << 10)
)

func explorationConfig(rounds int) LinUCBConfig {
	return LinUCBConfig{
		Alpha:                    0.2,
		NumActions:               NumActions,
		Dimension:                featureDim(),
		InitialExplorationRounds: rounds,
	}
}

func explorationState() State {
	return State{
		Concurrency:         1,
		ChunkSize:           testMinChunkSize,
		Throughput:          3.5e6,
		AggregateThroughput: 3.5e6,
		BytesTransferred:    32768,
		SuccessfulRequests:  1,
		TotalAttempts:       1,
	}
}

// applyActionToState simulates the concurrency/chunk-size controllers applying
// an action (with clamping at the experiment bounds).
func applyActionToState(s State, a ActionIndex) State {
	switch a {
	case ActionIncreaseConcurrency:
		if s.Concurrency < testMaxConcurrency {
			s.Concurrency++
		}
	case ActionDecreaseConcurrency:
		if s.Concurrency > 1 {
			s.Concurrency--
		}
	case ActionIncreaseChunkSize:
		if s.ChunkSize < testMaxChunkSize {
			s.ChunkSize += testChunkStep
		}
	case ActionDecreaseChunkSize:
		if s.ChunkSize > testMinChunkSize {
			s.ChunkSize -= testChunkStep
		}
	}
	return s
}

// runCorrection drives a learner through n decisions with a simulated
// controller, using a constant reward, and returns the selected action sequence
// plus a copy of the warm-up effectiveness/no-op bookkeeping.
func runCorrection(t *testing.T, rounds, n int, start State, reward float64) ([]ActionIndex, []bool, []bool) {
	t.Helper()
	l := mustLinUCB(t, explorationConfig(rounds))
	s := start
	actions := make([]ActionIndex, 0, n)
	for i := 0; i < n; i++ {
		a := l.SelectAction(s)
		actions = append(actions, a)
		l.Update(s, a, Reward{Value: reward})
		s = applyActionToState(s, a)
	}
	eff := append([]bool(nil), l.warmupEffective...)
	noop := append([]bool(nil), l.warmupNoop...)
	return actions, eff, noop
}

// TestInitialExplorationCoversEveryAction proves that one warm-up pass forces
// every action exactly once, in ActionIndex order, and then normal LinUCB takes
// over.
func TestInitialExplorationCoversEveryAction(t *testing.T) {
	l := mustLinUCB(t, explorationConfig(1))
	s := explorationState()

	var got []ActionIndex
	for i := 0; i < NumActions; i++ {
		a := l.SelectAction(s)
		got = append(got, a)
		l.Update(s, a, Reward{Value: 0})
		s = applyActionToState(s, a)
	}
	want := []ActionIndex{
		ActionNone,
		ActionIncreaseConcurrency,
		ActionDecreaseConcurrency,
		ActionIncreaseChunkSize,
		ActionDecreaseChunkSize,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warm-up actions = %v, want %v", got, want)
	}
	if l.exploreRemaining != 0 {
		t.Fatalf("exploreRemaining = %d, want 0", l.exploreRemaining)
	}

	// The next selection resolves the last warm-up arm and ends exploration.
	_ = l.SelectAction(s)
	if l.exploring {
		t.Fatal("exploration did not end after one full pass")
	}

	// Every action received at least one update during the warm-up. The
	// bytes_transferred feature (index 5) is 1.0 for this state, so the update
	// x xᵀ strictly increases A[5][5] for each arm.
	for a := ActionIndex(0); a < NumActions; a++ {
		if l.a[a].At(5, 5) <= 1 {
			t.Fatalf("arm %d was never updated during warm-up", a)
		}
	}
}

// TestInitialExplorationMultipleRounds checks that the configured number of
// passes is honoured before normal selection.
func TestInitialExplorationMultipleRounds(t *testing.T) {
	const rounds = 2
	l := mustLinUCB(t, explorationConfig(rounds))
	if l.exploreRemaining != rounds*NumActions {
		t.Fatalf("exploreRemaining = %d, want %d", l.exploreRemaining, rounds*NumActions)
	}
	s := explorationState()
	for i := 0; i < rounds*NumActions; i++ {
		a := l.SelectAction(s)
		l.Update(s, a, Reward{Value: 0})
		s = applyActionToState(s, a)
	}
	if l.exploreRemaining != 0 {
		t.Fatalf("exploreRemaining = %d, want 0", l.exploreRemaining)
	}
	_ = l.SelectAction(s)
	if l.exploring {
		t.Fatal("exploration did not end after the configured rounds")
	}
}

// TestInitialExplorationDeterministic proves the warm-up is reproducible: the
// same state/reward sequence yields the same action sequence and bookkeeping.
func TestInitialExplorationDeterministic(t *testing.T) {
	a1, e1, n1 := runCorrection(t, 1, 12, explorationState(), 1.0)
	a2, e2, n2 := runCorrection(t, 1, 12, explorationState(), 1.0)
	if !reflect.DeepEqual(a1, a2) {
		t.Fatalf("action sequences differ: %v vs %v", a1, a2)
	}
	if !reflect.DeepEqual(e1, e2) || !reflect.DeepEqual(n1, n2) {
		t.Fatalf("bookkeeping differs: %v/%v vs %v/%v", e1, n1, e2, n2)
	}
}

// TestInitialExplorationTransitionToNormal proves normal (greedy) selection is
// used once the warm-up completes: a strongly rewarded arm is then chosen.
func TestInitialExplorationTransitionToNormal(t *testing.T) {
	cfg := explorationConfig(1)
	cfg.Alpha = 0 // mean-only selection, so the trained arm is unambiguous
	l := mustLinUCB(t, cfg)

	s := explorationState()
	for i := 0; i < NumActions; i++ {
		a := l.SelectAction(s)
		l.Update(s, a, Reward{Value: 0})
		s = applyActionToState(s, a)
	}
	_ = l.SelectAction(s) // resolves the last arm; ends warm-up
	if l.exploring {
		t.Fatal("warm-up did not end")
	}

	target := ActionIncreaseChunkSize
	for i := 0; i < 5; i++ {
		l.Update(s, target, Reward{Value: 10})
	}
	if got := l.SelectAction(s); got != target {
		t.Fatalf("after warm-up normal scoring selected %d, want %d", got, target)
	}
}

// TestWarmupNoopNotCountedAsEvidence proves a bound-clamped (no-op) action is
// attempted but recorded as non-effective, while actions that do change their
// target are recorded as effective.
func TestWarmupNoopNotCountedAsEvidence(t *testing.T) {
	l := mustLinUCB(t, explorationConfig(1))

	// Start at both upper bounds so IncreaseConcurrency and IncreaseChunkSize
	// are no-ops.
	s := State{Concurrency: testMaxConcurrency, ChunkSize: testMaxChunkSize, Throughput: 3.5e6, AggregateThroughput: 3.5e6, BytesTransferred: 32768, SuccessfulRequests: 1, TotalAttempts: 1}
	for i := 0; i < NumActions; i++ {
		a := l.SelectAction(s)
		l.Update(s, a, Reward{Value: 0})
		s = applyActionToState(s, a)
	}
	_ = l.SelectAction(s) // resolve the final arm

	if !l.warmupEffective[ActionNone] {
		t.Error("ActionNone should be recorded as effective (baseline)")
	}
	if !l.warmupNoop[ActionIncreaseConcurrency] || l.warmupEffective[ActionIncreaseConcurrency] {
		t.Errorf("IncreaseConcurrency at max: effective=%v noop=%v, want false/true",
			l.warmupEffective[ActionIncreaseConcurrency], l.warmupNoop[ActionIncreaseConcurrency])
	}
	if !l.warmupEffective[ActionDecreaseConcurrency] {
		t.Error("DecreaseConcurrency from max should be effective")
	}
	if !l.warmupNoop[ActionIncreaseChunkSize] || l.warmupEffective[ActionIncreaseChunkSize] {
		t.Errorf("IncreaseChunkSize at max: effective=%v noop=%v, want false/true",
			l.warmupEffective[ActionIncreaseChunkSize], l.warmupNoop[ActionIncreaseChunkSize])
	}
	if !l.warmupEffective[ActionDecreaseChunkSize] {
		t.Error("DecreaseChunkSize from max should be effective")
	}
}

// TestInitialExplorationInvalidStateUnchanged proves malformed states still
// return ActionNone and do not advance the warm-up.
func TestInitialExplorationInvalidStateUnchanged(t *testing.T) {
	l := mustLinUCB(t, explorationConfig(1))
	remaining := l.exploreRemaining
	if got := l.SelectAction(State{Concurrency: -1}); got != ActionNone {
		t.Fatalf("malformed state selection = %d, want ActionNone", got)
	}
	if !l.exploring || l.exploreRemaining != remaining {
		t.Fatalf("malformed state advanced warm-up: exploring=%v remaining=%d", l.exploring, l.exploreRemaining)
	}
}

// TestInitialExplorationMismatchedDimensionUnchanged proves the dimension
// fallback is unchanged when exploration is enabled.
func TestInitialExplorationMismatchedDimensionUnchanged(t *testing.T) {
	l := mustLinUCB(t, LinUCBConfig{Alpha: 0.2, NumActions: NumActions, Dimension: featureDim() + 3, InitialExplorationRounds: 1})
	s := explorationState()
	if got := l.SelectAction(s); got != ActionNone {
		t.Fatalf("mismatched dimension selection = %d, want ActionNone", got)
	}
}

// TestInitialExplorationUpdateUnchanged proves the standard A/b update is still
// applied while exploration is enabled.
func TestInitialExplorationUpdateUnchanged(t *testing.T) {
	l := mustLinUCB(t, explorationConfig(1))
	s := explorationState()
	before := l.a[ActionIncreaseConcurrency].At(5, 5)
	l.Update(s, ActionIncreaseConcurrency, Reward{Value: 3})
	if got := l.a[ActionIncreaseConcurrency].At(5, 5); got <= before {
		t.Fatalf("A diagonal after Update = %v, want > %v", got, before)
	}
	nonZero := false
	for i := 0; i < featureDim(); i++ {
		if l.b[ActionIncreaseConcurrency].AtVec(i) != 0 {
			nonZero = true
		}
	}
	if !nonZero {
		t.Fatal("b after Update is all zero, want non-zero")
	}
}

// TestInitialExplorationDisabledByDefault proves the zero value preserves the
// previous behaviour: no warm-up, and the first selection is the lowest index.
func TestInitialExplorationDisabledByDefault(t *testing.T) {
	l := mustLinUCB(t, linUCBConfig()) // InitialExplorationRounds = 0
	if l.exploring {
		t.Fatal("exploration should be disabled by default")
	}
	if l.warmupEffective != nil || l.warmupNoop != nil {
		t.Fatal("exploration bookkeeping should not be allocated when disabled")
	}
	if got := l.SelectAction(explorationState()); got != ActionNone {
		t.Fatalf("default first selection = %d, want ActionNone", got)
	}
}
