package rl

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/mat"
)

// ErrInvalidLinUCBConfig indicates an unusable LinUCBConfig.
var ErrInvalidLinUCBConfig = fmt.Errorf("rl: invalid linucb config")

// LinUCBConfig configures a disjoint LinUCB learner.
type LinUCBConfig struct {
	// Alpha >= 0 is the exploration parameter in the UCB term
	// alpha * sqrt(xᵀ A⁻¹ x).
	Alpha float64
	// NumActions is the number of discrete arms. It must be >= 1; when using
	// the canonical discrete action space pass NumActions (= 5).
	NumActions int
	// Dimension is the length of the feature vector produced by State.Features.
	Dimension int
	// InitialExplorationRounds, when > 0, makes the learner take that many
	// deterministic passes over the action space (in ActionIndex order) before
	// normal LinUCB selection begins. A single pass guarantees that every arm
	// receives at least one update, so the disjoint posterior cannot lock onto
	// the arm selected first (see docs/PROJECT_CONTEXT.md "LinUCB Exploration
	// Correction"). Actions that cannot change their controller target (for
	// example a decrease already at the lower bound) are still attempted but are
	// recorded as non-effective and do not count as evidence of that action's
	// effect. Zero (the default) disables initial exploration and preserves the
	// previous behaviour exactly.
	InitialExplorationRounds int
	// ExplorationInterval, when > 0, forces one deterministic exploratory action
	// after every ExplorationInterval NORMAL LinUCB decisions. Forced actions
	// cycle through the action space in ActionIndex order using a separate
	// cursor; the cursor advances only when a forced action is actually
	// selected. The initial warm-up (InitialExplorationRounds) is separate and
	// unchanged. Zero (the default) disables periodic exploration and preserves
	// the previous behaviour exactly. See docs/PROJECT_CONTEXT.md "Periodic
	// Exploration Diagnostic".
	ExplorationInterval int
}

// Validate checks the configuration.
func (c LinUCBConfig) Validate() error {
	switch {
	case c.NumActions < 1:
		return fmt.Errorf("%w: num actions must be >= 1, got %d", ErrInvalidLinUCBConfig, c.NumActions)
	case c.Dimension < 1:
		return fmt.Errorf("%w: dimension must be >= 1, got %d", ErrInvalidLinUCBConfig, c.Dimension)
	case math.IsNaN(c.Alpha) || math.IsInf(c.Alpha, 0):
		return fmt.Errorf("%w: alpha must be finite, got %v", ErrInvalidLinUCBConfig, c.Alpha)
	case c.Alpha < 0:
		return fmt.Errorf("%w: alpha must be >= 0, got %v", ErrInvalidLinUCBConfig, c.Alpha)
	case c.InitialExplorationRounds < 0:
		return fmt.Errorf("%w: initial exploration rounds must be >= 0, got %d", ErrInvalidLinUCBConfig, c.InitialExplorationRounds)
	case c.ExplorationInterval < 0:
		return fmt.Errorf("%w: exploration interval must be >= 0, got %d", ErrInvalidLinUCBConfig, c.ExplorationInterval)
	}
	return nil
}

// LinUCB is a disjoint LinUCB contextual bandit over the existing finite
// ActionIndex space.
//
// Model (per action a):
//
//	A_a (d×d) initialised to the identity
//	b_a (d)   initialised to zero
//	θ_a = A_a⁻¹ b_a
//
// For a normalized context x it scores:
//
//	p_a = θ_aᵀ x + alpha * sqrt(xᵀ A_a⁻¹ x)
//
// and selects the action with the largest p_a. Ties are broken by the lowest
// ActionIndex for deterministic, reproducible behaviour.
//
// Initial exploration (opt-in via LinUCBConfig.InitialExplorationRounds): before
// normal selection, the learner takes a deterministic pass over the arms in
// ActionIndex order. Each arm is therefore updated at least once, so an arm
// selected first cannot dominate purely because the others were never tried.
// After the pass, normal UCB selection takes over; the warm-up is not a
// permanent cycling policy. A bound-clamped action is attempted but recorded as
// non-effective (it produced no controller change) and is not counted as
// evidence of that action's effect.
//
// Periodic exploration (opt-in via LinUCBConfig.ExplorationInterval): after the
// warm-up, once every N normal decisions the learner forces the next arm of a
// deterministic cycle (ActionIndex order). It is an experimental correction for
// the post-warm-up frozen-arm lock-in; between forced events selection is
// standard UCB.
//
// Update for the selected arm uses the scalar reward only:
//
//	A_a ← A_a + x xᵀ
//	b_a ← b_a + r x
//
// RewardComponents are intentionally NOT consumed here: reward shaping belongs
// to the experiment/integration layer, not the learner.
//
// The dense linear algebra (SPD factorisation, solving A_a θ_a = b_a, the
// quadratic form xᵀ A_a⁻¹ x, and the rank-one update A_a + x xᵀ) is delegated to
// Gonum's mat package; the bandit algorithm, feature handling, tie-breaking and
// validation remain project-owned. The learner is deterministic and not safe
// for concurrent use; the downloader's serialized policyRunner calls it under a
// single lock.
type LinUCB struct {
	alpha     float64
	numAction int
	dim       int

	// a[aIdx] and b[aIdx] are the per-arm sufficient statistics. Symmetric
	// positive definite A_a (identity plus rank-one updates) and vector b_a.
	a []*mat.SymDense
	b []*mat.VecDense

	// Initial-exploration warm-up state. When exploring is true, SelectAction
	// forces one deterministic pass over the arms (in ActionIndex order) before
	// normal scoring. The warm-up records, per arm, whether the forced attempt
	// actually changed its controller target (warmupEffective) or was a no-op at
	// a bound (warmupNoop); a no-op is not counted as evidence of that arm's
	// effect. The two slices are only allocated when initial exploration is on.
	exploreRounds    int
	exploreRemaining int
	exploreCursor    int
	exploring        bool

	haveWarmupPrev   bool
	warmupPrevState  State
	warmupPrevAction ActionIndex
	warmupEffective  []bool
	warmupNoop       []bool

	// Periodic-exploration state (independent of the warm-up). After the warm-up,
	// normalSinceExplore counts consecutive normal selections; once it reaches
	// exploreInterval the next selection is forced, the cursor advances, and the
	// counter resets. periodicEffective/periodicNoop record, per arm, whether a
	// forced attempt actually changed its controller target. Allocated only when
	// exploreInterval > 0.
	exploreInterval    int
	normalSinceExplore int
	periodicCursor     int
	havePeriodicPrev   bool
	periodicPrevState  State
	periodicPrevAction ActionIndex
	periodicEffective  []bool
	periodicNoop       []bool
}

// NewLinUCB validates cfg and returns an initialized learner with A_a = I and
// b_a = 0 for every action.
func NewLinUCB(cfg LinUCBConfig) (*LinUCB, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	l := &LinUCB{
		alpha:     cfg.Alpha,
		numAction: cfg.NumActions,
		dim:       cfg.Dimension,
	}
	l.a = make([]*mat.SymDense, cfg.NumActions)
	l.b = make([]*mat.VecDense, cfg.NumActions)
	for action := 0; action < cfg.NumActions; action++ {
		l.a[action] = symIdentity(cfg.Dimension)
		l.b[action] = mat.NewVecDense(cfg.Dimension, nil)
	}
	if cfg.InitialExplorationRounds > 0 {
		l.exploreRounds = cfg.InitialExplorationRounds
		l.exploreRemaining = cfg.InitialExplorationRounds * cfg.NumActions
		l.exploring = true
		l.warmupEffective = make([]bool, cfg.NumActions)
		l.warmupNoop = make([]bool, cfg.NumActions)
	}
	if cfg.ExplorationInterval > 0 {
		l.exploreInterval = cfg.ExplorationInterval
		l.periodicEffective = make([]bool, cfg.NumActions)
		l.periodicNoop = make([]bool, cfg.NumActions)
	}
	return l, nil
}

// NumActions returns the configured number of arms.
func (l *LinUCB) NumActions() int { return l.numAction }

// Dimension returns the configured feature dimension.
func (l *LinUCB) Dimension() int { return l.dim }

// Alpha returns the configured exploration parameter.
func (l *LinUCB) Alpha() float64 { return l.alpha }

// SelectAction returns the action to select for x. During the initial
// exploration warm-up it returns the forced arm for this step. After the
// warm-up, if periodic exploration is enabled and ExplorationInterval normal
// decisions have elapsed, it forces the next arm of the periodic cycle; all
// other decisions use the standard UCB score. If x is malformed (wrong length
// or non-finite) it falls back to ActionNone, which is always a valid, no-op
// choice; malformed states do not advance the warm-up or the exploration cursor.
func (l *LinUCB) SelectAction(state State) ActionIndex {
	x, ok := l.features(state)
	if !ok {
		return ActionNone
	}
	if l.exploring {
		if a, forced := l.explorationAction(state); forced {
			return a
		}
	}
	if l.havePeriodicPrev {
		l.resolvePeriodic(state)
	}
	if l.exploreInterval > 0 && l.normalSinceExplore >= l.exploreInterval {
		return l.periodicAction(state)
	}
	l.normalSinceExplore++
	return l.greedy(x)
}

// periodicAction selects the next forced exploratory action, advancing the
// periodic cursor and resetting the normal-decision counter.
func (l *LinUCB) periodicAction(state State) ActionIndex {
	a := ActionIndex(l.periodicCursor)
	l.periodicCursor++
	if l.periodicCursor >= l.numAction {
		l.periodicCursor = 0
	}
	l.normalSinceExplore = 0
	l.periodicPrevState = state
	l.periodicPrevAction = a
	l.havePeriodicPrev = true
	return a
}

// resolvePeriodic records whether the previously forced periodic action actually
// changed its controller target. A bound-clamped no-op is recorded as such and
// is never counted as effective exploration.
func (l *LinUCB) resolvePeriodic(state State) {
	a := int(l.periodicPrevAction)
	if a >= 0 && a < l.numAction && l.periodicEffective != nil {
		if actionExercised(l.periodicPrevAction, l.periodicPrevState, state) {
			l.periodicEffective[a] = true
		} else {
			l.periodicNoop[a] = true
		}
	}
	l.havePeriodicPrev = false
}

// greedy returns the arm with the highest UCB score for x, breaking exact ties
// by the lowest ActionIndex.
func (l *LinUCB) greedy(x []float64) ActionIndex {
	best := ActionNone
	bestScore := math.Inf(-1)
	for action := 0; action < l.numAction; action++ {
		score := l.score(action, x)
		if score > bestScore {
			bestScore = score
			best = ActionIndex(action)
		}
	}
	// Strict '>' keeps the lowest ActionIndex on exact ties.
	return best
}

// explorationAction advances the initial-exploration warm-up. It first resolves
// the previously forced arm (recording whether it actually changed its target
// controller), then returns the next forced arm, or (ActionNone, false) once the
// warm-up is complete so normal scoring takes over.
func (l *LinUCB) explorationAction(state State) (ActionIndex, bool) {
	if l.haveWarmupPrev {
		a := int(l.warmupPrevAction)
		if a >= 0 && a < l.numAction {
			if actionExercised(l.warmupPrevAction, l.warmupPrevState, state) {
				l.warmupEffective[a] = true
			} else {
				l.warmupNoop[a] = true
			}
		}
		l.haveWarmupPrev = false
	}
	if l.exploreRemaining <= 0 {
		l.exploring = false
		return ActionNone, false
	}
	a := ActionIndex(l.exploreCursor)
	l.exploreCursor++
	if l.exploreCursor >= l.numAction {
		l.exploreCursor = 0
	}
	l.exploreRemaining--
	l.warmupPrevState = state
	l.warmupPrevAction = a
	l.haveWarmupPrev = true
	return a, true
}

// actionExercised reports whether an action's intended effect was observed
// between two consecutive states. ActionNone's intended effect (no controller
// change) is always exercised. Concurrency/chunk actions are effective only if
// their target actually changed, so a bound-clamped no-op is never counted as
// evidence of that action's effect.
func actionExercised(a ActionIndex, prev, cur State) bool {
	switch a {
	case ActionIncreaseConcurrency, ActionDecreaseConcurrency:
		return cur.Concurrency != prev.Concurrency
	case ActionIncreaseChunkSize, ActionDecreaseChunkSize:
		return cur.ChunkSize != prev.ChunkSize
	default:
		return true
	}
}

// Update updates the selected arm's model with the scalar reward. It ignores
// invalid actions, malformed contexts, and non-finite rewards (no silent
// corruption of the model), and it never touches other arms.
func (l *LinUCB) Update(state State, action ActionIndex, reward Reward) {
	if !action.Valid() || int(action) >= l.numAction {
		return
	}
	if err := reward.Validate(); err != nil {
		return
	}
	x, ok := l.features(state)
	if !ok {
		return
	}
	l.updateArm(int(action), x, reward.Value)
}

// features extracts and validates the normalized feature vector for a State.
// It uses the existing State.Features via the package's canonical State config,
// so there is a single normalization path. The LinUCBConfig.Dimension must match
// the produced vector length.
func (l *LinUCB) features(state State) ([]float64, bool) {
	x, err := state.Features(defaultFeatureConfig())
	if err != nil {
		return nil, false
	}
	if len(x) != l.dim || !allFinite(x) {
		return nil, false
	}
	return x, true
}

// score computes p_a = θ_aᵀx + alpha*sqrt(xᵀ A_a⁻¹ x), where θ_a solves
// A_a θ_a = b_a. The quadratic form is evaluated as xᵀ (A_a⁻¹ x) via Cholesky.
func (l *LinUCB) score(action int, x []float64) float64 {
	xv := mat.NewVecDense(l.dim, x)

	var chol mat.Cholesky
	if !chol.Factorize(l.a[action]) {
		// A_a is SPD by construction (identity plus rank-one updates); this is
		// unreachable for valid input. Fall back to no score rather than
		// propagating NaN.
		return math.Inf(-1)
	}

	// θ_a = A_a⁻¹ b_a. SolveVecTo computes A_a θ = b_a.
	theta := mat.NewVecDense(l.dim, nil)
	if err := chol.SolveVecTo(theta, l.b[action]); err != nil {
		return math.Inf(-1)
	}
	mean := mat.Dot(theta, xv)

	explore := 0.0
	if l.alpha > 0 {
		// xᵀ A_a⁻¹ x, evaluated through the Cholesky factor.
		aInvX := mat.NewVecDense(l.dim, nil)
		if err := chol.SolveVecTo(aInvX, xv); err != nil {
			return math.Inf(-1)
		}
		v := mat.Dot(xv, aInvX)
		if v < 0 {
			v = 0 // guard against tiny negative values from round-off
		}
		explore = l.alpha * math.Sqrt(v)
	}
	return mean + explore
}

// updateArm applies the rank-one update to the selected arm:
//
//	A_a ← A_a + x xᵀ
//	b_a ← b_a + r x
//
// The update is applied to the stored sufficient statistics; the score method
// re-factorises A_a on each decision (d is small). This avoids maintaining a
// hand-rolled matrix inverse and keeps the numerics delegated to Gonum.
func (l *LinUCB) updateArm(action int, x []float64, r float64) {
	xv := mat.NewVecDense(l.dim, x)

	// A_a + x xᵀ. SymRankOne requires a distinct destination backing.
	var updated mat.SymDense
	updated.SymRankOne(l.a[action], 1, xv)
	l.a[action] = &updated

	// b_a + r x.
	scaled := mat.NewVecDense(l.dim, nil)
	scaled.ScaleVec(r, xv)
	l.b[action].AddVec(l.b[action], scaled)
}

func symIdentity(n int) *mat.SymDense {
	m := mat.NewSymDense(n, nil)
	for i := 0; i < n; i++ {
		m.SetSym(i, i, 1)
	}
	return m
}

// allFinite reports whether every value is finite (not NaN and not ±Inf).
func allFinite(v []float64) bool {
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

// defaultFeatureConfig returns the canonical normalization Config used by the RL
// package. It mirrors the RL test/experiment bounds so that LinUCB and the
// Adapter normalize features identically.
func defaultFeatureConfig() Config {
	return Config{
		MinConcurrency:     1,
		MaxConcurrency:     4,
		MinChunkSize:       1,
		MaxChunkSize:       1 << 20,
		MaxThroughput:      1e9,
		MaxRequestDuration: 1 << 40,
		CounterScale:       1000,
	}
}

var _ Policy = (*LinUCB)(nil)
