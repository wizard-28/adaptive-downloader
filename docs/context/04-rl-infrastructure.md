## 20. RL infrastructure (`internal/rl`)

**Contract + first learner (disjoint LinUCB).** Depends on `internal/policy` + stdlib.
See §20.6 for LinUCB; the rest of this section is the contract it fits into.

### 20.1 State (`state.go`)
```go
type State struct {
    Concurrency         int
    ChunkSize           int64
    Throughput          float64       // per-request, windowed
    AggregateThroughput float64       // wall-clock aggregate, windowed
    RequestDuration     time.Duration
    BytesTransferred    int64
    SuccessfulRequests  int64
    FailedRequests      int64
    Retries             int64
    TotalAttempts       int64
}
func StateFromContext(ctx policy.Context, cfg Config) (State, error)
func (s State) Validate() error
func FeatureNames() []string
func (s State) Features(cfg Config) ([]float64, error)
```
- Derived only from the existing `policy.Context`; deterministic; validates
  config (`Config{MinConcurrency, MaxConcurrency, MinChunkSize, MaxChunkSize,
  MaxThroughput, MaxRequestDuration, CounterScale}`) and rejects negatives /
  NaN / Inf. `State` is **raw**.
- `Throughput` is the windowed **per-request** rate; `AggregateThroughput` is the
  windowed **wall-clock aggregate** rate (see §20.7, §22.5). Both are kept;
  neither replaces the other.

### 20.2 Normalization
Normalization is explicit via `Features(cfg)` and is **not** applied silently.
Linear against `Config` bounds, clamped to `[0,1]`; zero denominator (e.g.
min == max) yields 0. Feature order (`FeatureNames`, now 10 features):
`concurrency`, `chunk_size`, `throughput`, `aggregate_throughput`,
`request_duration`, `bytes_transferred`, `successful_requests`,
`failed_requests`, `retries`, `total_attempts`. `MaxThroughput` is the shared
reference rate for both throughput features (the aggregate may exceed it and
clamps to 1). Dimension is derived from `len(FeatureNames())` (now **10**).

### 20.3 ActionIndex (`action.go`)
```
0 -> policy.ActionNone
1 -> policy.ActionIncreaseConcurrency
2 -> policy.ActionDecreaseConcurrency
3 -> policy.ActionIncreaseChunkSize
4 -> policy.ActionDecreaseChunkSize
```
`NumActions = 5`. `ActionFromIndex` / `ActionIndexFromAction` are deterministic
and round-trip for supported actions; invalid indices return
`ErrInvalidActionIndex`; the reserved `ActionSetConcurrency`/`ActionSetChunkSize`
return `ErrUnsupportedAction` (not part of the learning action space).

### 20.4 Reward contract (`reward.go`)
```go
type RewardComponents struct { Throughput, ThroughputImprovement, LatencyCost, RetryCost, FailureCost, ActionCost float64 }
type Reward struct { Value float64; Components RewardComponents }
```
`Value` and every component must be finite (`ErrInvalidReward` on NaN/±Inf).
This is the *data contract*; the computation lives in `RewardCalculator`
(§20.7). LinUCB consumes only `Reward.Value`.

### 20.7 Reward computation (`reward_calculator.go`) — CURRENT REWARD BASELINE

```go
type RewardConfig struct {
    // normalizers
    ThroughputScale, LatencyScale float64
    RetryPenalty, FailurePenalty, ActionCost float64
    // weights
    WThroughput, WImprovement, WLatency, WRetry, WFailure, WAction float64
}
func DefaultRewardConfig() RewardConfig
func (c RewardConfig) Validate() error
type RewardCalculator struct { ... }
func NewRewardCalculator(cfg RewardConfig) (*RewardCalculator, error)
func (c *RewardCalculator) Calculate(previous, current State, action ActionIndex, havePrevious bool) Reward
```

**Components.** `RewardComponents` now carries six components (one added:
`ThroughputImprovement`):

- `Throughput` (benefit, absolute): `current.AggregateThroughput / ThroughputScale`.
  Uses the **aggregate** (wall-clock) throughput observable, so a stable
  high-throughput state scores above a stable low-throughput state *and* the
  term responds to concurrency (a per-request signal would not; see §22.5).
- `ThroughputImprovement` (benefit, signed): `(current.AggregateThroughput −
  previous.AggregateThroughput) / ThroughputScale`. Rewards the *change* in
  aggregate throughput (adaptation), and is negative on a regression. First
  observation → 0 (`havePrevious == false`).
- `LatencyCost`: `clamp01(current.RequestDuration.Seconds() / LatencyScale)` —
  normalized to `[0,1]` so one slow request cannot dominate.
- `RetryCost`: `(current.Retries − previous.Retries) * RetryPenalty`, clamped
  at ≥ 0 (counter-reset safe).
- `FailureCost`: `(current.FailedRequests − previous.FailedRequests) * FailurePenalty`,
  clamped at ≥ 0. Retries and failures are counted from separate metrics
  counters, so a retried failed attempt is not double-counted.
- `ActionCost`: `ActionCost` **only if the action actually changed the
  corresponding controller target** (state-change based), else 0. `ActionNone`
  is never charged, and no action is charged on the first observation
  (`havePrevious == false`). This is a real control-churn cost: a no-op or a
  clamped (no-effect) action costs nothing. There is no arbitrary "good/bad"
  action ranking.

`ThroughputScale` normalizes **both** the absolute and the improvement term
(name kept for continuity; semantics documented here, not changed silently).

**Formula (sign convention: benefits positive, costs non-negative and subtracted):**

```
Value = WThroughput*Throughput + WImprovement*ThroughputImprovement
        − WLatency*LatencyCost − WRetry*RetryCost
        − WFailure*FailureCost − WAction*ActionCost
```

`Reward.Value` is exactly this combination of `Reward.Components`. The formula
is written in one place in `Calculate` for auditability. Invalid inputs
(non-finite state, wrong shape) yield `Reward{}` (zero) rather than a corrupted
reward.

**Configuration.** Explicit and validated: all fields finite and ≥ 0; a
non-zero weight on a component requires a non-zero scale
(`ErrInvalidRewardConfig`). `DefaultRewardConfig()` is a modest baseline
(`ThroughputScale = 1<<20`, `LatencyScale = 1.0`, `RetryPenalty = 0.5`,
`FailurePenalty = 1.0`, `ActionCost = 0.05`, all weights `1.0` except
`WLatency = WRetry = 0.5`); it is **not** claimed to be optimal. The downloader
exposes it via `downloader.Config.RewardConfig *rl.RewardConfig` (nil → reward
stays 0 for backward compatibility).

**Why separate from LinUCB.** The learner must not know how throughput, latency,
retries, failures, or churn are turned into a reward. The pipeline is
`Metrics/Context → RewardCalculator → Reward → LinUCB(Value)`. Reward shaping can
change without touching the learner.

**Fairness is intentionally NOT in the reward.** The repository measures no
background traffic, competing flows, link utilisation, RTT inflation, or share
of bandwidth, so no defensible fairness signal exists. `ActionCost` is a churn
cost, **not** fairness. Fairness is left as a future extension requiring a new
measurable signal.

### 20.5 Adapter (`policy.go`)
```go
type Policy interface {
    SelectAction(state State) ActionIndex
    Update(state State, action ActionIndex, reward Reward)
}
func NewAdapter(p Policy, cfg Config) (*Adapter, error)
type AlwaysActionPolicy struct { Action ActionIndex } // test-only, no learning
```
`Adapter` implements the existing `policy.Policy`:
`SelectAction(ctx)`: `StateFromContext → Policy.SelectAction → ActionFromIndex →
policy.Action` (falls back to `NoChange` on conversion error, since the
downloader interface returns no error). `Update(ctx, action, reward)`:
`StateFromContext + ActionIndexFromAction → Policy.Update(..., Reward{Value:
reward})`. `Adapter.Last()` returns the most recent `(State, ActionIndex)`.
`AlwaysActionPolicy` always returns its configured action and is used only to
exercise the adapter.

### 20.6 LinUCB learner (`linucb.go`)

Disjoint LinUCB contextual bandit implementing the existing `rl.Policy`
interface; **deterministic**. Dense linear algebra is delegated to Gonum's
`mat` package (see §20.8); the bandit algorithm remains project-owned.

```go
type LinUCBConfig struct {
    Alpha                    float64
    NumActions               int
    Dimension                int
    InitialExplorationRounds int // 0 = disabled (default); 1 = one warm-up pass
    ExplorationInterval      int // 0 = disabled (default); N = force 1 action per N normal decisions
}
func NewLinUCB(cfg LinUCBConfig) (*LinUCB, error)
func (l *LinUCB) SelectAction(state State) ActionIndex
func (l *LinUCB) Update(state State, action ActionIndex, reward Reward)
func (l *LinUCB) NumActions() int
func (l *LinUCB) Dimension() int
func (l *LinUCB) Alpha() float64
```

**Initial exploration (warm-up).** `InitialExplorationRounds` (validated `>= 0`,
default `0`) optionally makes the learner take that many deterministic passes
over the action space in `ActionIndex` order before normal UCB selection. One
pass guarantees every arm receives at least one update. See §22.8.

**Periodic exploration.** `ExplorationInterval` (validated `>= 0`, default `0`)
optionally forces one deterministic exploratory action after every N normal
decisions, cycling the arms in `ActionIndex` order with a separate cursor. It is
an experimental correction for the post-warm-up lock-in and is **not** active in
the experiment path by default. See §22.13.

**Model.** Per action `a`, maintain `A_a` (d×d, initialised to `I`) and `b_a`
(d, initialised to `0`); `θ_a = A_a⁻¹ b_a`. For a normalized context `x`:

```
p_a = θ_aᵀ x + alpha * sqrt(xᵀ A_a⁻¹ x)      // select argmax_a p_a
```

**Feature handling.** LinUCB consumes the existing normalized feature vector
produced by `State.Features(...)`; it does **not** re-implement normalization.
`LinUCBConfig.Dimension` must equal `len(State.Features(...))`
(= `len(FeatureNames())` = 10). A mismatched dimension or a non-finite/ malformed
feature vector makes `SelectAction` fall back to `ActionNone` and `Update` a
no-op, so the model is never corrupted. The learner uses a fixed internal
normalization `Config` (bounds in `defaultFeatureConfig`) identical in shape to
the adapter/experiment bounds.

**Action space.** The existing finite `ActionIndex` space (`NumActions = 5`);
pass `NumActions` for the canonical space. No `SetConcurrency`/`SetChunkSize`
arms exist. Invalid `ActionIndex` values are ignored by `Update`.

**Reward.** `Update` uses only `reward.Value`:

```
A_a ← A_a + x xᵀ
b_a ← b_a + r x
```

`RewardComponents` are intentionally **not** consumed here; reward shaping
belongs to the experiment/integration layer. Non-finite rewards (NaN/±Inf) are
rejected by `Reward.Validate` before touching the model.

**Tie-break.** Exact ties resolve to the **lowest `ActionIndex`** (strict `>`
comparison during argmax), making selection reproducible.

**Numerical approach.** Dense linear algebra is delegated to **Gonum**
(`gonum.org/v1/gonum/mat`): per-arm sufficient statistics are stored as
`*mat.SymDense` (A_a, SPD, start as identity) and `*mat.VecDense` (b_a, start
zero). `score` factorises A_a with `mat.Cholesky`, solves `A_a θ_a = b_a`
(`SolveVecTo`) and evaluates `xᵀ A_a⁻¹ x` through the factor; `updateArm`
applies `mat.SymRankOne` (A_a + x xᵀ) and a scaled vector add (b_a + r x). The
former hand-written `linalg.go` (Cholesky solver, quadratic form, dot, finite
checks, Sherman–Morrison inverse maintenance) was **removed**; the factorisation
is recomputed per decision, which is more numerically robust and is cheap at the
feature dimension used here (d = 10). Project-owned parts remain in `linucb.go`:
feature extraction, dimension validation, `allFinite`, tie-breaking, score
formula, action validation, and the `Policy` implementation. Handles
`alpha = 0`, negative/zero rewards, and 1-dimensional contexts.

**Tests** (`linucb_test.go`, `linucb_integration_test.go`): config validation;
initial identity/zero model; valid deterministic selection; lowest-index
tie-break; `alpha=0` mean-only training; disjoint **action isolation**;
positive/zero/negative reward effects; repeated-update accumulation; the exact
hand-computable `d=1` case (`A=[2]`, `b=[2]`, `θ=1`); exploration-term math and
monotonicity in `alpha`; malformed state/dimension fallback; invalid
action/reward ignored; `Policy` interface satisfaction; and adapter
compatibility (`State → LinUCB → ActionIndex → Adapter → policy.Action`).
Integration as the opt-in experiment mode is described in §16.1.

---

## 21. RL decision timing, reward credit assignment, leakage prevention

`policyRunner.observe(rec)` (single serialized path, under one mutex) now runs:

```
request completes -> metrics recorded
  -> update counters/window, build Context_t
  -> if a previous action exists:
       State_{t-1} = StateFromContext(prevContext)
       State_t     = StateFromContext(Context_t)
       reward_t    = RewardCalculator.Calculate(State_{t-1}, State_t, prevAction, havePrevious)
       policy.Update(prevContext, prevAction, reward_t)   // credit to the PREVIOUS action
  -> Action_t = policy.SelectAction(Context_t)
  -> applyAction(Action_t) -> controllers change
  -> remember prevContext = Context_t, prevAction = Action_t
  -> future allocation sees the new target
```

**Credit assignment (critical).** The outcome observed at step *t* is the
consequence of the action chosen at step *t−1*, so `Update` is called with the
**previous** context and previous action, and the reward is the change between
the previous and current observations. It is never called with the
just-selected action (which has no outcome yet). The first observation selects
an action but calls no `Update`; therefore `Update` count = `SelectAction`
count − 1. Deterministic policies ignore reward entirely, so this change does
not alter their decisions.

**Pipeline:** `Context_t → State` (raw) → `RewardCalculator` → `Reward` →
learner `Update`. LinUCB uses `reward.Value`; the calculator never talks to the
learner.

Available at decision time (all from completed attempts): current concurrency,
current chunk size, windowed per-request throughput, windowed aggregate
throughput, just-completed request duration and bytes, and cumulative counters.
**Not** available/included: future throughput/duration, final download duration,
future controller state, final output correctness, or any uncompleted request.
No leakage.

---

