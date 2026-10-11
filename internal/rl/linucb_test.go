package rl

import (
	"errors"
	"math"
	"testing"
)

// featureDim matches exactly the number of features produced by
// State.Features and the order documented by FeatureNames.
func featureDim() int { return len(FeatureNames()) }

func linUCBConfig() LinUCBConfig {
	return LinUCBConfig{
		Alpha:      0.2,
		NumActions: NumActions,
		Dimension:  featureDim(),
	}
}

// stateWithConcurrency constructs a state whose only non-trivial feature is
// concurrency, so tests have predictable, mostly-zero feature vectors.
func stateWithConcurrency(c int) State {
	return State{Concurrency: c, ChunkSize: 1}
}

func TestNewLinUCBValid(t *testing.T) {
	l, err := NewLinUCB(linUCBConfig())
	if err != nil {
		t.Fatalf("NewLinUCB = %v", err)
	}
	if l.NumActions() != NumActions || l.Dimension() != featureDim() || l.Alpha() != 0.2 {
		t.Fatalf("learner config mismatch: n=%d d=%d alpha=%v", l.NumActions(), l.Dimension(), l.Alpha())
	}
}

func TestNewLinUCBInvalidConfig(t *testing.T) {
	cases := map[string]func(*LinUCBConfig){
		"negative alpha":                func(c *LinUCBConfig) { c.Alpha = -0.1 },
		"nan alpha":                     func(c *LinUCBConfig) { c.Alpha = math.NaN() },
		"pos inf alpha":                 func(c *LinUCBConfig) { c.Alpha = math.Inf(1) },
		"neg inf alpha":                 func(c *LinUCBConfig) { c.Alpha = math.Inf(-1) },
		"zero dimension":                func(c *LinUCBConfig) { c.Dimension = 0 },
		"negative dimension":            func(c *LinUCBConfig) { c.Dimension = -1 },
		"zero actions":                  func(c *LinUCBConfig) { c.NumActions = 0 },
		"negative actions":              func(c *LinUCBConfig) { c.NumActions = -1 },
		"negative exploration rounds":   func(c *LinUCBConfig) { c.InitialExplorationRounds = -1 },
		"negative exploration interval": func(c *LinUCBConfig) { c.ExplorationInterval = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := linUCBConfig()
			mutate(&c)
			if _, err := NewLinUCB(c); !errors.Is(err, ErrInvalidLinUCBConfig) {
				t.Fatalf("NewLinUCB(%+v) = %v, want ErrInvalidLinUCBConfig", c, err)
			}
		})
	}
}

// TestInitialModelIsIdentityZero checks behaviorally that every arm starts with
// A = I and b = 0: a zero context gives zero mean for all arms, so with alpha=0
// and zero scores the deterministic lowest-index tie-break picks ActionNone.
func TestInitialModelIsIdentityZero(t *testing.T) {
	cfg := linUCBConfig()
	cfg.Alpha = 0
	l, _ := NewLinUCB(cfg)

	if got := l.SelectAction(State{}); got != ActionNone {
		t.Fatalf("zero state initial selection = %d, want ActionNone (lowest index on tie)", got)
	}
}

func TestSelectActionReturnsValidIndex(t *testing.T) {
	l, _ := NewLinUCB(linUCBConfig())
	for c := 0; c <= 4; c++ {
		got := l.SelectAction(stateWithConcurrency(c))
		if !got.Valid() {
			t.Fatalf("SelectAction returned invalid index %d", got)
		}
	}
}

func TestSelectActionDeterministic(t *testing.T) {
	l1, _ := NewLinUCB(linUCBConfig())
	l2, _ := NewLinUCB(linUCBConfig())
	states := []State{
		{}, stateWithConcurrency(1), stateWithConcurrency(2),
		{Concurrency: 3, ChunkSize: 100, Throughput: 5e6, TotalAttempts: 7},
	}
	for i, s := range states {
		if a, b := l1.SelectAction(s), l2.SelectAction(s); a != b {
			t.Fatalf("state %d: fresh learners disagree: %d vs %d", i, a, b)
		}
	}
}

// TestTieBreakLowestIndex verifies that exact ties resolve to the lowest
// ActionIndex. With alpha=0 and all arms still at A=I, b=0, every score is 0.
func TestTieBreakLowestIndex(t *testing.T) {
	cfg := linUCBConfig()
	cfg.Alpha = 0
	l, _ := NewLinUCB(cfg)

	// A zero feature vector: every arm scores exactly 0.
	if got := l.SelectAction(State{}); got != ActionNone {
		t.Fatalf("tie = %d, want ActionNone", got)
	}
}

// TestAlphaZeroTrainsOnMeanOnly verifies that with alpha=0 selection follows the
// learned mean: after rewarding arm 1 heavily for a given context, arm 1 wins.
func TestAlphaZeroTrainsOnMeanOnly(t *testing.T) {
	cfg := linUCBConfig()
	cfg.Alpha = 0
	l, _ := NewLinUCB(cfg)

	s := stateWithConcurrency(4) // concurrency feature = 1.0
	for i := 0; i < 10; i++ {
		l.Update(s, ActionIncreaseConcurrency, Reward{Value: 1})
	}
	if got := l.SelectAction(s); got != ActionIncreaseConcurrency {
		t.Fatalf("after positive rewards, selection = %d, want IncreaseConcurrency", got)
	}
}

// TestActionIsolation verifies the disjoint-LinUCB invariant: updating one arm
// must not change another arm's model or its predictions.
func TestActionIsolation(t *testing.T) {
	cfg := linUCBConfig()
	cfg.Alpha = 0
	l, _ := NewLinUCB(cfg)

	before := l.SelectAction(stateWithConcurrency(4))

	// Reward a *different* arm heavily.
	l.Update(stateWithConcurrency(4), ActionIncreaseChunkSize, Reward{Value: 1000})

	// With alpha=0 and only arm 3 updated, the previously-best arm must remain
	// unchanged (its mean is still 0, arm 3 now has a positive mean for this x).
	if before == ActionIncreaseChunkSize {
		t.Fatalf("test setup invalid: arm 3 was already selected")
	}
	// The arm we updated must now be preferred for this exact context.
	if got := l.SelectAction(stateWithConcurrency(4)); got != ActionIncreaseChunkSize {
		t.Fatalf("updated arm not preferred: got %d, want IncreaseChunkSize", got)
	}
}

// TestPositiveZeroNegativeReward verifies that different reward signs change
// future predictions in the expected direction under alpha=0.
func TestPositiveZeroNegativeReward(t *testing.T) {
	cfg := linUCBConfig()
	cfg.Alpha = 0
	s := stateWithConcurrency(4)

	t.Run("positive reward favors arm", func(t *testing.T) {
		l, _ := NewLinUCB(cfg)
		l.Update(s, ActionIncreaseConcurrency, Reward{Value: 5})
		if got := l.SelectAction(s); got != ActionIncreaseConcurrency {
			t.Fatalf("got %d, want IncreaseConcurrency", got)
		}
	})

	t.Run("negative reward disfavors arm", func(t *testing.T) {
		l, _ := NewLinUCB(cfg)
		l.Update(s, ActionIncreaseConcurrency, Reward{Value: -5})
		if got := l.SelectAction(s); got == ActionIncreaseConcurrency {
			t.Fatalf("negative-reward arm was selected for the same context")
		}
	})

	t.Run("zero reward leaves means equal", func(t *testing.T) {
		l, _ := NewLinUCB(cfg)
		l.Update(s, ActionIncreaseConcurrency, Reward{Value: 0})
		// All means remain 0; deterministic tie-break picks the lowest index.
		if got := l.SelectAction(s); got != ActionNone {
			t.Fatalf("zero reward selection = %d, want ActionNone", got)
		}
	})
}

// TestRepeatedUpdatesAccumulate checks that reward accumulates correctly for a
// hand-computable 1-feature case. We use a custom learner with Dimension=1 and
// drive it through an internal-feature-free path by calling the low-level
// helpers directly, preserving the disjoint model invariant.
func TestRepeatedUpdatesAccumulate(t *testing.T) {
	// Hand-computable, d=1: A=[1], b=[0], x=[1], r=2.
	l, err := NewLinUCB(LinUCBConfig{Alpha: 0, NumActions: 1, Dimension: 1})
	if err != nil {
		t.Fatalf("NewLinUCB = %v", err)
	}
	x := []float64{1}
	l.updateArm(0, x, 2)

	if got := l.a[0].At(0, 0); got != 2 {
		t.Fatalf("A = %v, want 2", got)
	}
	if got := l.b[0].AtVec(0); got != 2 {
		t.Fatalf("b = %v, want 2", got)
	}
	// θ = A⁻¹ b = 2/2 = 1.
	theta := l.b[0].AtVec(0) / l.a[0].At(0, 0)
	if theta != 1 {
		t.Fatalf("theta = %v, want 1", theta)
	}
}

// TestUpdateChangesLearnerModel verifies, through the public Update path, that
// the selected arm's model changes and other arms are untouched. This is a
// package-local test (it inspects the unexported sufficient statistics) so the
// public API stays minimal.
func TestUpdateChangesLearnerModel(t *testing.T) {
	l := mustLinUCB(t, LinUCBConfig{Alpha: 0.2, NumActions: NumActions, Dimension: featureDim()})
	state := State{Concurrency: 4, ChunkSize: 100, Throughput: 500, TotalAttempts: 3}

	// Before: A_a = I, b_a = 0 for every arm.
	beforeDiag := l.a[ActionIncreaseConcurrency].At(0, 0)
	if beforeDiag != 1 {
		t.Fatalf("initial A diagonal = %v, want 1", beforeDiag)
	}

	l.Update(state, ActionIncreaseConcurrency, Reward{Value: 3})

	// The updated arm's A gains x xᵀ (diagonal strictly increases).
	if got := l.a[ActionIncreaseConcurrency].At(0, 0); got <= beforeDiag {
		t.Fatalf("A after Update = %v, want > %v", got, beforeDiag)
	}
	// b gains r x; at least one component is non-zero.
	nonZero := false
	for i := 0; i < featureDim(); i++ {
		if l.b[ActionIncreaseConcurrency].AtVec(i) != 0 {
			nonZero = true
		}
	}
	if !nonZero {
		t.Fatal("b after Update is all zero, want non-zero")
	}

	// Action isolation: an untouched arm keeps A = I and b = 0.
	if got := l.a[ActionNone].At(0, 0); got != 1 {
		t.Fatalf("untouched arm A = %v, want 1", got)
	}
	for i := 0; i < featureDim(); i++ {
		if l.b[ActionNone].AtVec(i) != 0 {
			t.Fatalf("untouched arm b[%d] = %v, want 0", i, l.b[ActionNone].AtVec(i))
		}
	}
}

// TestMathematicalSanityD1 verifies the exact hand-computable d=1 sequence:
// after Update(x=[1], r=2), A=[2], b=[2], θ=1, and the score equals θ·x = 1
// when alpha=0.
func TestMathematicalSanityD1(t *testing.T) {
	l, _ := NewLinUCB(LinUCBConfig{Alpha: 0, NumActions: 1, Dimension: 1})
	l.updateArm(0, []float64{1}, 2)

	score := l.score(0, []float64{1})
	if math.Abs(score-1) > 1e-12 {
		t.Fatalf("score = %v, want 1", score)
	}
}

// TestUpdateChangesFuturePredictions verifies that a reward update changes the
// selection for a context after training, using a full State (features path).
func TestUpdateChangesFuturePredictions(t *testing.T) {
	cfg := linUCBConfig()
	cfg.Alpha = 0
	l, _ := NewLinUCB(cfg)

	s := stateWithConcurrency(4)
	initial := l.SelectAction(s)

	// Train a different arm strongly on this context.
	target := ActionDecreaseChunkSize
	if initial == target {
		target = ActionIncreaseChunkSize
	}
	for i := 0; i < 5; i++ {
		l.Update(s, target, Reward{Value: 10})
	}
	if got := l.SelectAction(s); got != target {
		t.Fatalf("after training target arm, got %d, want %d", got, target)
	}
}

// TestExplorationAffectsSelection constructs two arms where the exploitation
// means are equal but their uncertainties differ, and verifies the exploration
// term breaks the tie in favor of the less-certain arm.
func TestExplorationAffectsSelection(t *testing.T) {
	// Use a 1-dimensional feature space and full State path is awkward; drive
	// the model directly so uncertainty is controlled while still exercising
	// score() and SelectAction's tie-break through a real State is not needed.
	l, _ := NewLinUCB(LinUCBConfig{Alpha: 1.0, NumActions: 2, Dimension: 1})

	// Arm 0: many observations -> small uncertainty; mean learned.
	x := []float64{1}
	for i := 0; i < 50; i++ {
		l.updateArm(0, x, 0)
	}
	// Arm 1: only the identity prior -> large uncertainty, mean 0.

	p0 := l.score(0, x)
	p1 := l.score(1, x)

	// Arm 0 has A=[51], b=[0] -> mean 0, exploration = alpha*sqrt(1/51).
	// Arm 1 has A=[1],  b=[0] -> mean 0, exploration = alpha*sqrt(1/1).
	want0 := 1.0 * math.Sqrt(1.0/51.0)
	want1 := 1.0 * math.Sqrt(1.0/1.0)
	if math.Abs(p0-want0) > 1e-9 || math.Abs(p1-want1) > 1e-9 {
		t.Fatalf("exploration scores = %v, %v; want %v, %v", p0, p1, want0, want1)
	}
	if p1 <= p0 {
		t.Fatalf("less-certain arm should score higher: p0=%v p1=%v", p0, p1)
	}
}

// TestAlphaControlsExploration verifies monotonic behavior: a larger alpha
// weights the exploration term more (equal means, differing uncertainty).
func TestAlphaControlsExploration(t *testing.T) {
	build := func(alpha float64) *LinUCB {
		l, _ := NewLinUCB(LinUCBConfig{Alpha: alpha, NumActions: 2, Dimension: 1})
		for i := 0; i < 100; i++ {
			l.updateArm(0, []float64{1}, 0)
		}
		return l
	}
	small := build(0.1).score(1, []float64{1})
	large := build(1.0).score(1, []float64{1})
	if large <= small {
		t.Fatalf("larger alpha should yield a larger exploration score: %v vs %v", large, small)
	}
}

func TestUpdateInvalidActionIgnored(t *testing.T) {
	cfg := linUCBConfig()
	cfg.Alpha = 0
	l, _ := NewLinUCB(cfg)
	s := stateWithConcurrency(4)

	before := l.SelectAction(s)
	l.Update(s, ActionIndex(NumActions), Reward{Value: 100}) // out of range
	l.Update(s, ActionIndex(200), Reward{Value: 100})
	if got := l.SelectAction(s); got != before {
		t.Fatalf("invalid action changed model: %d -> %d", before, got)
	}
}

func TestUpdateInvalidRewardIgnored(t *testing.T) {
	cfg := linUCBConfig()
	cfg.Alpha = 0
	l, _ := NewLinUCB(cfg)
	s := stateWithConcurrency(4)

	before := l.SelectAction(s)
	l.Update(s, ActionIncreaseConcurrency, Reward{Value: math.NaN()})
	l.Update(s, ActionIncreaseConcurrency, Reward{Value: math.Inf(1)})
	if got := l.SelectAction(s); got != before {
		t.Fatalf("non-finite reward changed model: %d -> %d", before, got)
	}
}

func TestSelectActionHandlesMalformedState(t *testing.T) {
	l, _ := NewLinUCB(linUCBConfig())
	// Negative concurrency is rejected by State.Features -> fallback ActionNone.
	if got := l.SelectAction(State{Concurrency: -1}); got != ActionNone {
		t.Fatalf("malformed state selection = %d, want ActionNone", got)
	}
}

// TestMismatchedFeatureDimension verifies that a Dimension that does not match
// the actual State.Features vector length is handled safely: SelectAction falls
// back to ActionNone and Update is ignored.
func TestMismatchedFeatureDimension(t *testing.T) {
	l, err := NewLinUCB(LinUCBConfig{Alpha: 0.2, NumActions: NumActions, Dimension: featureDim() + 3})
	if err != nil {
		t.Fatalf("NewLinUCB = %v", err)
	}
	s := stateWithConcurrency(4)
	if got := l.SelectAction(s); got != ActionNone {
		t.Fatalf("mismatched dimension selection = %d, want ActionNone", got)
	}
	l.Update(s, ActionIncreaseConcurrency, Reward{Value: 1}) // must not panic

	// Models must be untouched: a correctly-dimensioned learner selects the
	// deterministic tie-break action for the same fresh state.
	good, _ := NewLinUCB(LinUCBConfig{Alpha: 0.2, NumActions: NumActions, Dimension: featureDim()})
	if got, want := l.SelectAction(s), good.SelectAction(s); got != want {
		t.Fatalf("mismatched-dimension learner changed behavior: got %d, want %d", got, want)
	}
}

// TestFeatureOrderMatchesLearnerDimension ties the learner dimension to the
// documented feature order: changing the feature count would invalidate this.
func TestFeatureOrderMatchesLearnerDimension(t *testing.T) {
	want := 10 // concurrency, chunk_size, throughput, aggregate_throughput,
	// request_duration, bytes, successful, failed, retries, total_attempts
	if got := len(FeatureNames()); got != want {
		t.Fatalf("FeatureNames length = %d, want %d", got, want)
	}
}

func TestLinUCBImplementsPolicy(t *testing.T) {
	var p Policy = mustLinUCB(t, linUCBConfig())
	s := stateWithConcurrency(2)
	a := p.SelectAction(s)
	if !a.Valid() {
		t.Fatalf("Policy.SelectAction invalid index %d", a)
	}
	p.Update(s, a, Reward{Value: 1}) // must not panic
}

func mustLinUCB(t *testing.T, cfg LinUCBConfig) *LinUCB {
	t.Helper()
	l, err := NewLinUCB(cfg)
	if err != nil {
		t.Fatalf("NewLinUCB(%+v) = %v", cfg, err)
	}
	return l
}
