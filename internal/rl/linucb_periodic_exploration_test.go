package rl

import (
	"reflect"
	"testing"
)

// This file tests the optional periodic-exploration correction for the
// post-warm-up lock-in. Zero (the default) must preserve all existing behavior.

func periodicConfig(interval int) LinUCBConfig {
	return LinUCBConfig{
		Alpha:                    0.2,
		NumActions:               NumActions,
		Dimension:                featureDim(),
		InitialExplorationRounds: 1,
		ExplorationInterval:      interval,
	}
}

// runPeriodic drives a learner for n decisions with a constant reward, simulating
// the controller effect of each selected action, and returns the action sequence.
func runPeriodic(t *testing.T, l *LinUCB, n int, start State, reward float64) []ActionIndex {
	t.Helper()
	var prev State
	var prevA ActionIndex
	have := false
	s := start
	actions := make([]ActionIndex, 0, n)
	for i := 0; i < n; i++ {
		if have {
			l.Update(prev, prevA, Reward{Value: reward})
		}
		a := l.SelectAction(s)
		actions = append(actions, a)
		prev, prevA, have = s, a, true
		s = applyActionToState(s, a)
	}
	return actions
}

// TestPeriodicDisabledByDefault proves ExplorationInterval=0 changes nothing:
// no periodic bookkeeping is allocated and the zero-config behavior is unchanged
// (no warm-up, no periodic exploration, deterministic lowest-index selection).
func TestPeriodicDisabledByDefault(t *testing.T) {
	l, err := NewLinUCB(linUCBConfig()) // zero value for both exploration fields
	if err != nil {
		t.Fatalf("NewLinUCB = %v", err)
	}
	if l.exploreInterval != 0 || l.periodicEffective != nil || l.periodicNoop != nil {
		t.Fatalf("interval=0 allocated periodic state: interval=%d eff=%v noop=%v", l.exploreInterval, l.periodicEffective, l.periodicNoop)
	}
	got := runPeriodic(t, l, 20, explorationState(), 1.0)
	for i, a := range got {
		if a != ActionNone {
			t.Fatalf("zero-config decision %d = %d, want None", i, a)
		}
	}
}

// TestPeriodicIntervalZeroWithWarmup proves that with the warm-up enabled but
// the interval zero, no periodic forced actions occur after the warm-up.
func TestPeriodicIntervalZeroWithWarmup(t *testing.T) {
	l := mustLinUCB(t, periodicConfig(0))
	if l.periodicEffective != nil || l.periodicNoop != nil {
		t.Fatal("interval=0 allocated periodic state")
	}
	got := runPeriodic(t, l, 20, explorationState(), 1.0)
	want := []ActionIndex{ActionNone, ActionIncreaseConcurrency, ActionDecreaseConcurrency, ActionIncreaseChunkSize, ActionDecreaseChunkSize}
	if !reflect.DeepEqual(got[:5], want) {
		t.Fatalf("warm-up = %v, want %v", got[:5], want)
	}
	// All post-warm-up decisions are greedy (the first chosen arm dominates).
	for i := 5; i < len(got); i++ {
		if got[i] != got[5] {
			t.Fatalf("unexpected periodic action at %d: %v", i, got)
		}
	}
}

// TestPeriodicIntervalOne: after every normal decision, one forced action.
func TestPeriodicIntervalOne(t *testing.T) {
	l := mustLinUCB(t, periodicConfig(1))
	got := runPeriodic(t, l, 18, explorationState(), 1.0)
	// decisions 6,8,10,12,14,16 are forced, cycling 0,1,2,3,4,0.
	forced := []ActionIndex{got[6], got[8], got[10], got[12], got[14], got[16]}
	wantForced := []ActionIndex{ActionNone, ActionIncreaseConcurrency, ActionDecreaseConcurrency, ActionIncreaseChunkSize, ActionDecreaseChunkSize, ActionNone}
	if !reflect.DeepEqual(forced, wantForced) {
		t.Fatalf("forced actions = %v, want %v", forced, wantForced)
	}
}

// TestPeriodicIntervalFive: forced after exactly five normal decisions.
func TestPeriodicIntervalFive(t *testing.T) {
	l := mustLinUCB(t, periodicConfig(5))
	got := runPeriodic(t, l, 24, explorationState(), 1.0)
	// First forced index is 10 (warm-up 0..4, normal 5..9), then 16, 22.
	if got[10] != ActionNone {
		t.Fatalf("decision 10 = %d, want None (first forced)", got[10])
	}
	if got[16] != ActionIncreaseConcurrency {
		t.Fatalf("decision 16 = %d, want IncreaseConcurrency (second forced)", got[16])
	}
	if got[22] != ActionDecreaseConcurrency {
		t.Fatalf("decision 22 = %d, want DecreaseConcurrency (third forced)", got[22])
	}
	// Decisions 5..9 must be normal (not forced): they must equal the first five
	// post-warm-up decisions of an interval=0 learner.
	base := mustLinUCB(t, periodicConfig(0))
	baseGot := runPeriodic(t, base, 24, explorationState(), 1.0)
	if !reflect.DeepEqual(got[:10], baseGot[:10]) {
		t.Fatalf("normal decisions diverged before the first forced event: %v vs %v", got[:10], baseGot[:10])
	}
}

// TestPeriodicCursorCyclesDeterministically checks the forced cycle order.
func TestPeriodicCursorCyclesDeterministically(t *testing.T) {
	l := mustLinUCB(t, periodicConfig(1))
	got := runPeriodic(t, l, 30, explorationState(), 1.0)
	var forced []ActionIndex
	for i := 6; i < len(got); i += 2 {
		forced = append(forced, got[i])
	}
	want := []ActionIndex{0, 1, 2, 3, 4, 0, 1, 2, 3, 4, 0, 1}
	if !reflect.DeepEqual(forced, want) {
		t.Fatalf("forced cycle = %v, want %v", forced, want)
	}
}

// TestPeriodicWarmupStillOccursOnce checks the warm-up is unchanged and precedes
// periodic exploration.
func TestPeriodicWarmupStillOccursOnce(t *testing.T) {
	l := mustLinUCB(t, periodicConfig(3))
	if !l.exploring || l.exploreRemaining != NumActions {
		t.Fatalf("warm-up not armed: exploring=%v remaining=%d", l.exploring, l.exploreRemaining)
	}
	got := runPeriodic(t, l, 12, explorationState(), 1.0)
	want := []ActionIndex{ActionNone, ActionIncreaseConcurrency, ActionDecreaseConcurrency, ActionIncreaseChunkSize, ActionDecreaseChunkSize}
	if !reflect.DeepEqual(got[:5], want) {
		t.Fatalf("warm-up = %v, want %v", got[:5], want)
	}
	if l.exploring {
		t.Fatal("warm-up did not finish after five selections")
	}
}

// TestPeriodicForcedUpdateUsesStandardEquations proves the forced action still
// goes through the standard A/b update (no special path).
func TestPeriodicForcedUpdateUsesStandardEquations(t *testing.T) {
	l := mustLinUCB(t, periodicConfig(1))
	s := explorationState()
	before := l.a[ActionNone].At(5, 5)
	l.Update(s, ActionNone, Reward{Value: 2})
	if got := l.a[ActionNone].At(5, 5); got <= before {
		t.Fatalf("A diagonal = %v, want > %v", got, before)
	}
	if l.b[ActionNone].AtVec(5) == 0 {
		t.Fatal("b component = 0, want non-zero after Update")
	}
}

// TestPeriodicGreedyBetweenEvents proves non-forced decisions are standard
// greedy LinUCB (they match an interval=0 learner until the first forced event).
func TestPeriodicGreedyBetweenEvents(t *testing.T) {
	exp := mustLinUCB(t, periodicConfig(5))
	base := mustLinUCB(t, periodicConfig(0))
	expGot := runPeriodic(t, exp, 15, explorationState(), 1.0)
	baseGot := runPeriodic(t, base, 15, explorationState(), 1.0)
	if !reflect.DeepEqual(expGot[:10], baseGot[:10]) {
		t.Fatalf("greedy phase diverged before the first forced event: %v vs %v", expGot[:10], baseGot[:10])
	}
}

// TestPeriodicInvalidStateDoesNotAdvanceCursor proves malformed states are
// ignored and do not advance the periodic cursor or counter.
func TestPeriodicInvalidStateDoesNotAdvanceCursor(t *testing.T) {
	l := mustLinUCB(t, periodicConfig(1))
	// Reach the first forced point: warm-up (0..4) + one normal (5).
	runPeriodic(t, l, 6, explorationState(), 1.0)
	if l.periodicCursor != 0 || l.normalSinceExplore != 1 {
		t.Fatalf("pre-state cursor=%d counter=%d, want 0/1", l.periodicCursor, l.normalSinceExplore)
	}
	if got := l.SelectAction(State{Concurrency: -1}); got != ActionNone {
		t.Fatalf("invalid state selection = %d, want ActionNone", got)
	}
	if l.periodicCursor != 0 || l.normalSinceExplore != 1 {
		t.Fatalf("invalid state advanced periodic state: cursor=%d counter=%d", l.periodicCursor, l.normalSinceExplore)
	}
	// The valid call must now force the cursor-0 action (None), proving the
	// cursor was not advanced by the invalid call.
	if got := l.SelectAction(explorationState()); got != ActionNone {
		t.Fatalf("first forced action after invalid state = %d, want None (cursor 0)", got)
	}
	if l.periodicCursor != 1 {
		t.Fatalf("cursor after forced action = %d, want 1", l.periodicCursor)
	}
}

// TestPeriodicEffectivenessBookkeeping checks that a bound no-op is recorded as
// a no-op and a real change as effective.
func TestPeriodicEffectivenessBookkeeping(t *testing.T) {
	l := mustLinUCB(t, periodicConfig(1))
	sMax := State{Concurrency: testMaxConcurrency, ChunkSize: testMinChunkSize, Throughput: 3.5e6, AggregateThroughput: 3.5e6, BytesTransferred: 32768, SuccessfulRequests: 1, TotalAttempts: 1}

	// First forced action: cursor 0 = None. None is effective by definition.
	if a := l.periodicAction(sMax); a != ActionNone {
		t.Fatalf("first forced action = %d, want None", a)
	}
	l.resolvePeriodic(sMax)
	if !l.periodicEffective[ActionNone] {
		t.Error("ActionNone should be recorded effective")
	}

	// Second forced action: cursor 1 = IncreaseConcurrency at max -> no-op.
	if a := l.periodicAction(sMax); a != ActionIncreaseConcurrency {
		t.Fatalf("second forced action = %d, want IncreaseConcurrency", a)
	}
	l.resolvePeriodic(sMax) // state unchanged
	if !l.periodicNoop[ActionIncreaseConcurrency] || l.periodicEffective[ActionIncreaseConcurrency] {
		t.Errorf("IncreaseConcurrency at max: effective=%v noop=%v, want false/true",
			l.periodicEffective[ActionIncreaseConcurrency], l.periodicNoop[ActionIncreaseConcurrency])
	}

	// Third forced action: cursor 2 = DecreaseConcurrency from max -> effective.
	if a := l.periodicAction(sMax); a != ActionDecreaseConcurrency {
		t.Fatalf("third forced action = %d, want DecreaseConcurrency", a)
	}
	l.resolvePeriodic(State{Concurrency: testMaxConcurrency - 1, ChunkSize: testMinChunkSize})
	if !l.periodicEffective[ActionDecreaseConcurrency] {
		t.Error("DecreaseConcurrency from max should be effective")
	}
}
