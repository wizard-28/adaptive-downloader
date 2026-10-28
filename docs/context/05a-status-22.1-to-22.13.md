## 22. Current RL status

**Implemented:** the RL contract (State; config + validation; explicit
`Features` normalization; `ActionIndex` + encode/decode; `Reward` /
`RewardComponents` + validation; `Policy` interface; `Adapter`;
`AlwaysActionPolicy`; integration tests through the real downloader) **and the
first learner: disjoint LinUCB** (`LinUCBConfig`, `NewLinUCB`, `SelectAction`,
`Update`, Gonum-backed SPD linear algebra, deterministic lowest-index
tie-break, action isolation, adapter compatibility test).

**Implemented:** the reward computation layer — `RewardCalculator` +
`RewardConfig` (`internal/rl/reward_calculator.go`), wired into
`policyRunner.observe` with correct credit assignment
(`downloader.Config.RewardConfig`). The reward was reviewed and is now the
**current baseline: Candidate C (absolute + signed improvement throughput;
state-change `ActionCost`; `ThroughputImprovement` component/weight)** — see
§20.7, §22.2. When `RewardConfig` is nil, reward remains `0` (backward
compatible).

**Implemented (integration):** LinUCB is available as an **opt-in fifth
experiment mode** (`ModeLinUCB`) via the existing `rl.Adapter` + `policyRunner` +
`RewardCalculator`, selectable through `RunSelected`, `MatrixConfig.Modes`, and
the CLI `-modes linucb`. The default four-mode behavior and the baseline dataset
are unchanged (§16.1). The complete causal loop
`State_t → Action_t → execution → Observation_{t+1} → Reward_t →
Update(State_t, Action_t, Reward_t)` is validated by deterministic integration
tests (`internal/rl/causal_integration_test.go` and downloader runner tests).

**Implemented (benchmark):** the first formal 5-mode benchmark (four baselines
+ LinUCB) has been run with the locked `DefaultMatrixConfig` at 10 repetitions
and documented in §22.6 (`testdata/benchmark/`). It is descriptive only.

**Not implemented:** empirical tuning / scientific validation of reward weights
(they are an explicit, configurable current baseline, §20.7); fairness objective
(no observable fairness signal exists yet — see §20.7); a **meaningful LinUCB
performance evaluation** — the benchmark ran but LinUCB made no controller
changes (exploration lock-in, §22.6). The lock-in is now **corrected** (§22.8)
but the correction is **not yet benchmarked**, so **no performance claim is
made**; persistence/checkpointing; any more advanced RL algorithm.

### 22.1 LinUCB calibration and reward diagnostics

A small, deterministic calibration workflow answers "is the reward signal and
LinUCB behaviour sensible enough to proceed to formal evaluation?" It is
**diagnostic only**: it does not rank modes, tune weights, or claim performance.

- **Code:** `internal/experiment/calibration.go` (pure diagnostics +
  `RunCalibration`), `cmd/calibration/main.go` (CLI).
- **Scenarios** (`CalibrationScenarios()`, all privilege-free and deterministic;
  they use the server's existing rate-transition, `SetDelay`, and `FailNext`
  mechanisms, not tc/netem):
  - A `calibration-a-stable` — constant rate.
  - B `calibration-b-throttle` — 256 KiB/s → 2 MiB/s after 4 requests.
  - C `calibration-c-latency` — deterministic 20 ms server delay.
  - D `calibration-d-failure` — first 3 requests fail with 500.
  - E `calibration-e-combined` — throttle + 10 ms delay + 2 failures.
- **Base config:** file 128 KiB, chunk 8 KiB, min/max chunk 4/32 KiB, step
  4 KiB, concurrency 1 (min 1, max 3), thresholds 0.2, window 4, retries 3,
  timeout 10 s, `LinUCBAlpha` 0.2, `DefaultRewardConfig`. Repetitions 2 by
  default (`-reps`).
- **Diagnostics per run:** action distribution (counts/fractions, unique,
  concurrency/chunk exploration), reward stats (mean/median/min/max/sd,
  pos/zero/neg), component means (raw and weighted), churn (action transitions,
  changes/decision, longest same-run, actual concurrency/chunk changes),
  early/late action distributions, cumulative reward, and reward
  **reconstruction cross-check** (recomputed reward vs. the runner's recorded
  reward; any mismatch marks the run numerically unhealthy).
- **Output:** `testdata/calibration/calibration.json` (machine-readable) and
  `testdata/calibration/calibration.txt` (concise summary). The four-mode
  baseline dataset `testdata/baseline/` is unchanged.

### 22.2 Reward design review (original → current baseline)

The **original** first reward used `Throughput` as a *change* signal only
(`(current − previous)/ThroughputScale`). Under a stationary rate that term is
~0, so the reward was dominated by costs (`latency_cost`, `action_cost`), and a
stable high-throughput state was indistinguishable from a stable
low-throughput state. Three formulations were considered:

- **A. Improvement only** — original; rewards adaptation, cannot reward
  sustained throughput; cost-dominated under stationary conditions.
- **B. Absolute only** — rewards sustained throughput; adaptation is implicit.
- **C. Absolute + improvement** — rewards sustained throughput *and* the
  change that produced it. **Chosen.**

Candidate C was chosen because the stated objective is *sustained high
throughput with adaptation*: absolute throughput makes a stable high-throughput
state score above a stable low-throughput state, while the improvement term
credits actions that raise throughput. `ActionCost` was simultaneously changed
from "charged for any non-`ActionNone` action" to "charged only when the action
**actually changed** the corresponding controller target", so it now reflects
real control churn rather than button presses. No learner math, State,
ActionIndex, adapter, range/controller/worker logic, or deterministic policy was
modified; no weights were tuned.

### 22.3 Current calibration findings (factual observations only)

From a diagnostic run (`go run ./cmd/calibration -reps 2`) with the **current
(C) reward driven by the aggregate-throughput observable** (§22.5); see the
saved report for exact numbers. **No conclusion about quality is drawn.** The
pre-audit (per-request reward) run is kept only for contrast in
`/tmp/calibration_perrequest.*` (outside the repo).

- **Reconstruction cross-check passed everywhere**
  (`reconstruction_mismatches=0`, `numerically_healthy=true`).
- **Rewards reflect aggregate throughput level.** Means: stable ≈ 0.46,
  throttle ≈ 1.23, latency ≈ 0.19, failure ≈ 0.32, combined ≈ 0.73; component
  raw means e.g. combined `throughput` 0.78, `improvement` 0.06.
- **Exploration remains scenario-dependent.** Stable / throttle / latency:
  `ActionNone = 100%` (`unique_actions=1`, 0 churn). Failure / combined:
  `unique_actions=3` / `2`; failure drifts to `ActionDecreaseConcurrency`
  (late ≈ 20 of ~30), combined drifts to `ActionIncreaseConcurrency`
  (late ≈ 18 of ~30).
- **The correction changed learner behaviour where throughput is actionable:**
  with the aggregate reward, `calibration-e-combined` now favours
  **increase-concurrency (94.4%)**, whereas the per-request reward favoured
  decrease-concurrency (83.3%). This is consistent with the aggregate observable
  rising with concurrency. No weights were changed.
- **Stable/throttle/latency still lock to `ActionNone`.** The state can now
  *represent* a concurrency benefit, but LinUCB does not explore it there: the
  absolute-throughput reward (~0.2–1.2) dominates the UCB exploration bonus, so
  untried arms never win. This is a reward-scale/exploration interaction, **not**
  an observability defect, and it is **not** tuned here.
- **Chunk-size is still not exercised** (`explored_chunk_size=false`). The
  harness models no chunk-size performance tradeoff (the server paces each
  request at a fixed rate, so chunk size affects request count/duration but not
  throughput), so no throughput-based observable can identify a chunk-size
  benefit. This is a **harness limitation**, recorded for the benchmark design.

### 22.4 Research status

- The formal 5-mode benchmark **has been run** once (§22.6); it is descriptive
  and produced no validated performance conclusion.
- The **LinUCB Exploration Diagnostic** (§22.7) explains the `ActionNone=100 %`
  result: primarily an **exploration/reward-scale** problem (plus the
  initialization tie-break), **not** a concurrency-observability defect; chunk
  size remains unidentifiable because the harness models no chunk-size tradeoff.
- The current (C) reward is **frozen as the next baseline** but is **not
  scientifically validated**; it was **not** tuned to improve outcomes. Its
  throughput components are based on the aggregate observable (§22.5).
- Calibration is **diagnostic** (implementation/reward sanity), not a
  performance evaluation; no ranking is produced.
- **No performance conclusion** about LinUCB (or any mode) has been established;
  in the benchmark LinUCB made no controller changes (§22.6).
- The **exploration lock-in is corrected** (§22.8): LinUCB now takes a bounded,
  deterministic warm-up pass over the action space before normal UCB selection.
  This is a correctness change; Benchmark v2 (§22.9) ran it descriptively but
  made **no performance claim** (the warm-up dominates the short runs).
- **Formal Benchmark v3** (§22.11, 4 MiB horizon, 32–122 learned decisions per
  scenario) found **no meaningful post-warm-up policy evolution**: LinUCB locked
  onto a single arm after the warm-up and never changed concurrency. Descriptive
  only; no superiority claim. This is the current frontier of the RL evaluation.
- The **post-warm-up lock-in diagnostic** (§22.12) numerically attributes the
  lock to posterior dominance, the capped exploration bonus (unselected arms are
  never updated), and reward/credit lag; reward separability is not the cause and
  the chunk-size limitation is only partial. No fix is proposed.
- An **experimental periodic-exploration correction** (§22.13,
  `LinUCBConfig.ExplorationInterval`, default 0) is implemented and diagnostically
  evaluated: it raises action coverage from 1 to 5 unique arms and updates
  previously frozen arms, but does not change the dominant arm preference in the
  frozen-state replay. **No performance claim; no benchmark yet.**
- The **live periodic-exploration benchmark** (§22.14) confirmed in real
  downloads that periodic exploration raises unique post-warm-up arms to 5 and
  moves the posterior, and in `loss` changed greedy behaviour and held
  concurrency; in the other scenarios the dominant preference was unchanged.
  Behavioral only; **no ranking or performance claim**.
- The **final six-mode benchmark** (§22.15) placed standard and periodic LinUCB
  beside the four deterministic controllers under the identical 4 MiB matrix
  (300/300 valid). Behaviorally, periodic exploration changes action coverage and
  controller state but not the dominant arm preference in most scenarios;
  performance is descriptive only. **No winner or superiority claim.**
- An **aria2c external baseline** (§22.16) was measured under the same scenarios
  (50/50 valid): 4 concurrent connections, fixed 1 MiB splits, no within-run
  adaptation, 0 retries. Completion times differ from the internal modes, but the
  comparison is confounded by request granularity; **no ranking**. IDM not
  evaluated.
- The **request-granularity sensitivity study** (§22.17) matched the internal
  downloader to 1 MiB/4-concurrency and reproduced aria2 closely in 4/5
  scenarios (0.85–0.97×), substantially reducing the granularity confound;
  latency retains ~2.3× unexplained aria2 overhead. Descriptive only.
- Fairness is still **not implemented** (no measurable signal — see §20.7).

### 22.5 RL State Observability Audit

**Question.** Before the formal benchmark, can the RL State distinguish the
consequences of changing concurrency and chunk size? Traced through the actual
data flow and calibration trajectories.

**Current state fields (all from `policy.Context`, one decision per completed
attempt):**

| Field | Source | Meaning | Action-sensitive? |
|---|---|---|---|
| `Concurrency` | `concurrency.Controller.Current()` | target worker count | reflects the action, not its outcome |
| `ChunkSize` | `chunksize.Controller.Current()` / fixed | target for future allocations | reflects the action |
| `Throughput` | runner window | `sum(window bytes)/sum(window durations)` — **per-request** rate | no (server-paced: ≈ rate regardless of concurrency) |
| `AggregateThroughput` | runner window | `sum(window bytes)/wall-clock span` — **aggregate** rate | **yes (concurrency)** |
| `RequestDuration` | last `metrics.Record` | just-completed request duration | chunk size (duration = bytes/rate) |
| `BytesTransferred` | last `metrics.Record` | just-completed request bytes | chunk size |
| `SuccessfulRequests`/`FailedRequests`/`Retries`/`TotalAttempts` | runner counters | cumulative, +1 per observation | no (failures/concurrency only indirectly) |

**Throughput semantics (pre-audit).** `fetcher.Result.Throughput =
len(Data)/Duration`; the runner's `Context.Throughput =
sum(recent bytes)/sum(recent durations)` over `window` attempts. Because the
experiment server paces **each** response independently at `rate` bytes/s
(`server.writeThrottled`), every request reports ≈ `rate`, so `Context.Throughput`
is a per-request-rate proxy. It is **not** a wall-clock aggregate; even
`metrics.Aggregate.AverageThroughput` is `successBytes/successElapsed`
(per-request-duration-weighted), and no wall-clock timestamp was retained. The
`workerPool.active` count (actual parallelism) was not observable.

**Concurrency observability (pre-audit).** Increasing concurrency 1→N raised
the true aggregate (≈ N·rate) and shortened wall-clock duration, but changed
**no** observed field except the `Concurrency` target itself: `Throughput`,
`RequestDuration`, and `BytesTransferred` were unchanged under server pacing.
The reward was therefore action-independent w.r.t. concurrency — a fundamental
identifiability gap, not merely a learning-speed issue. Calibration traces
confirmed: stable 512 KiB/s held `throughput` ≈ 0.45–0.47 MiB/s with
`Concurrency=1` and `ActionNone=100%`; throttle rose only when the *server rate*
changed (0.24→1.65 MiB/s), never through concurrency.

**Chunk-size observability.** `IncreaseChunkSize` is visible in `ChunkSize`,
`BytesTransferred`, and `RequestDuration` (and hence `LatencyCost`), but has no
throughput effect in this harness. The reward could therefore learn only to
*avoid* long requests, not a genuine chunk-size benefit.

**Temporal resolution / credit assignment.** Decisions are **serialized**, one
per completed attempt (`policyRunner.observe` under one mutex). With concurrency
N, N workers complete ~simultaneously and produce N serialized decisions. The
reward for the action at decision k is computed from `(state_k, state_{k+1})`,
where `state_{k+1}` is the *next completed attempt* — which may have been
in-flight when the action was applied (and, for chunk size, allocated at the old
size). Credit assignment is therefore **approximate under concurrency > 1** and
**approximate for chunk size while old-size requests are in flight**. It is not
redesigned here.

**Conclusion — the pre-audit state was insufficient** for the project's
objective (aggregate throughput): it could not expose the throughput effect of
concurrency, and the harness models no chunk-size performance effect at all.

**Correction (minimum observable added).** `AggregateThroughput`, a rolling
wall-clock aggregate over the same `window`:

- **Formula:** `sum(last window bytes) / (max(sample.end) − min(sample.start))`,
  where `sample.start = sample.end − record.Duration` and `sample.end` is the
  wall-clock time the attempt was observed. Units: bytes/sec. First
  observation: window = 1 sample → equals the per-request rate. Non-positive span
  → 0. Computed in `policyRunner.aggregateThroughput` using an injectable
  `now func() time.Time` for deterministic tests.
- **Context/State:** added `policy.Context.AggregateThroughput` and
  `rl.State.AggregateThroughput`; the existing `Throughput` is **unchanged**.
- **Features:** new feature `aggregate_throughput` at **index 3**, normalized
  `AggregateThroughput / MaxThroughput` (shared bound), clamped `[0,1]`;
  dimension 9 → **10**, still derived from `len(FeatureNames())`.
- **Reward:** the `Throughput` and `ThroughputImprovement` components now use
  `AggregateThroughput` (same components, weights, and formula; only the source
  observable changed). Per-request throughput remains in the State/features.
- **Deterministic policies:** unchanged logic; they ignore the new field. The
  four-mode baseline dataset is unaffected (`testdata/baseline/` untouched).
- **Tests:** runner-level per-request-vs-aggregate discrimination with a fake
  clock; State feature normalization/order; dimension; reward-uses-aggregate;
  policy Context validation; calibration reconstruction.

**Remaining (documented, not fixed):** (1) the stable/throttle/latency lock-in
is an exploration/reward-scale issue; (2) chunk size has no modelled performance
tradeoff, so the harness cannot evaluate a chunk-size preference. Server/network
pacing differences from real links are a further caveat.

### 22.6 Formal 5-mode benchmark (first formal run)

**Status: completed.** Descriptive only — no ranking, no winner, no performance
claim. The RL architecture and reward were not changed and no weights/alpha were
tuned for this run.

- **Dataset (new, does not overwrite history):**
  - `testdata/benchmark/matrix_5mode_reps10.json` — raw `MatrixResult` (all 250
    runs, trajectories, summaries).
  - `testdata/benchmark/matrix_5mode_reps10.txt` — human-readable summary
    (config, correctness, performance table, LinUCB behavior, aggregate
    validation, statistical method, caveats).
  - `testdata/baseline/` and `testdata/calibration/` are **unchanged and kept
    separate**.
- **Reproduce:** `go run ./cmd/experiment -matrix -sudo -reps 10 -json
  -modes fixed,concurrency-adaptive,chunk-size-adaptive,joint-adaptive,linucb >
  matrix_5mode_reps10.json`.
- **Configuration (locked = `DefaultMatrixConfig`, reps=10):** 5 scenarios × 5
  modes × 10 repetitions = **250 runs**. File 256 KiB; chunk 32 KiB, min/max
  32/128 KiB, initial 32 KiB, step 32 KiB; concurrency 1 (min 1, max 4);
  thresholds 0.2/0.2; window 4; retries 3; timeout 30 s; LinUCB alpha 0.2,
  `DefaultRewardConfig` (aggregate-throughput based). Scenarios: `baseline`
  (4 MiB/s), `server-throttle` (512 KiB/s → 4 MiB/s after 4 req), `latency`
  (4 MiB/s + 20 ms), `loss` (4 MiB/s + 1 %), `combined` (1 MiB/s + 20 ms + 1 %).
- **Correctness / validity:** **250/250 valid**, 0 invalid, 0 failed downloads.
  Per run: `Err==""`, `OutputMatches` (byte-for-byte), `NoDuplicateRanges`,
  `NoMissingRanges`. Network status `none` for baseline/server-throttle,
  `applied` for latency/loss/combined; **0 network-unavailable runs**. SHA-256
  is **not** implemented in the framework (byte compare is used instead). tc
  cleanup verified after the run (no residual qdisc).
- **Aggregate-throughput validation (benchmark data):**
  - Windowed `Context.AggregateThroughput` vs per-request `Throughput` across the
    400 LinUCB decisions (all at concurrency 1): mean relative difference
    **0.59 %** (max 3.1 %) — with no overlap the two agree, as expected.
  - Wall-clock run rate (`FileSize/Duration`) vs the framework's per-request
    weighted `AverageThroughput` on runs with concurrency > 1:
    `latency` concurrency-adaptive **1.45×**, `combined` concurrency-adaptive
    **1.42×** / joint **1.48×**; at concurrency 1 the ratio is ≈ 0.98–1.00.
    This confirms the wall-clock aggregate captures overlap and is distinct from
    the per-request metric. (The windowed observable itself is covered by the
    deterministic fake-clock runner test; LinUCB never overlapped in this run.)
- **LinUCB behavior:** in **all five scenarios** LinUCB selected `ActionNone`
  100 % (80 decisions per scenario; `unique_actions=1`), performed **0**
  controller changes (0 concurrency, 0 chunk size), and never exceeded
  concurrency 1. Concrete per-scenario action counts and final controller state
  are in the summary dataset. This is the documented exploration/reward-scale
  lock-in (§22.3), **not** a state bug; it was not tuned away.
- **Observed performance (mean completion time, ms; not ranked):**

  | Scenario | fixed | concurrency-adaptive | chunk-size-adaptive | joint-adaptive | linucb |
  |---|---|---|---|---|---|
  | baseline | 73.5 | 73.3 | 73.4 | 73.7 | 74.2 |
  | server-throttle | 296.0 | 287.8 | 296.2 | 288.0 | 297.4 |
  | latency | 434.1 | 327.1 | 312.5 | 326.8 | 434.9 |
  | loss (high variance) | 135.4 | 132.0 | 174.2 | 94.4 | 115.4 |
  | combined | 710.4 | 591.3 | 539.1 | 470.4 | 682.0 |

  Retries were **0** in every run; attempts were 8 in most runs (7 for some
  chunk-size runs). Because LinUCB made no controller changes, its completion
  times are close to `fixed` in every scenario; the deterministic adaptive modes
  did change controllers (`server-throttle`/`latency`/`combined` show
  concurrency and/or chunk-size actions). These are measurements only.
- **Statistical analysis:** independently seeded runs (fresh server/output per
  run, no shared seed → **unpaired** design), n=10 per cell. Reported: mean,
  median, standard deviation, and a descriptive 95 % t CI of the mean
  (df=9, t=2.262). **No significance tests** were run (no pairing; ranking
  forbidden by policy). The `loss` scenario has large wall-clock variance.
- **Caveats:** chunk-size performance tradeoff not modelled; credit assignment
  approximate under concurrency>1; reward weights and alpha untuned; fair
  reporting is descriptive only; fairness not implemented; no aria2/IDM
  comparison. The benchmark establishes **what the policy did and the measured
  numbers**, not that any mode is superior.

### 22.7 LinUCB Exploration Diagnostic

**Status: completed, diagnostic only.** A separate, labeled study into **why
LinUCB selected `ActionNone` 100 % of the time** in §22.6 and whether the
environment can distinguish the non-None actions. It changes **no** production
behavior: LinUCB, the reward, the deterministic policies, feature normalization,
the server, and the frozen formal benchmark dataset are all untouched.

- **Why necessary:** the formal benchmark ran but LinUCB never acted, so a
  performance comparison was not interpretable. The benchmark dataset was
  frozen; this study has its own dataset.
- **Dataset (separate):** `testdata/diagnostics/linucb_exploration/` —
  `report.json` (per-scenario/action stats), `report.txt` (human-readable),
  `initial_scores.txt` (white-box score output). `testdata/benchmark/`,
  `testdata/baseline/`, and `testdata/calibration/` are unchanged.
- **Reproduce:** `go run ./cmd/linucb-diagnostic -reps 5 -sudo` (5 scenarios × 5
  forced actions × 5 reps; `-sudo` only for the loss scenario's tc/netem).
  Integrity check: **`recon_mismatches=0`** everywhere (reward reconstructed from
  the recorded contexts equals the reward the runner attributed).

**Initial LinUCB scores.** With `A_a = I`, `b_a = 0`, the score is
`alpha·sqrt(xᵀIx) = alpha·‖x‖` for **every** arm, so the five actions are
**exactly tied** at initialization and the lowest-index tie-break selects
`ActionNone`. Measured (alpha=0.2): every action scored **0.200101** on the
hand-built baseline first state (‖x‖=1.000507) and **≈0.2062** on the real
first-decision states of all five scenarios (‖x‖≈1.0308).

**Feature scale (10 features, representative baseline first state):**
`[concurrency 0, chunk 0.0312, throughput 0.0042, aggregate 0.0042, duration
0.000007, bytes 1.0, successes 0.001, failures 0, retries 0, attempts 0.001]`,
‖x‖≈1.03. `bytes_transferred` saturates at 1.0 (`32768/1000`, clamped), so it
**dominates the norm**; throughput features are ~1e-3–4e-3 because
`MaxThroughput=1e9`; duration ~1e-5. The context therefore discriminates states
weakly, and the clamp caps the exploration bonus at ≈alpha. (Normalization was
**not** changed.)

**Reward scale (frozen benchmark LinUCB decisions, all `ActionNone`):**

| Scenario | mean | median | sd | pos/zero/neg | dominant components (raw mean) |
|---|---|---|---|---|---|
| baseline | 3.398 | 3.407 | 0.072 | 70/0/0 | throughput 3.396, improvement 0.006, latency 0.009 |
| server-throttle | 1.502 | 0.740 | 1.711 | 70/0/0 | throughput 1.097, improvement 0.421, latency 0.033 |
| latency | 0.590 | 0.607 | 0.066 | 70/0/0 | throughput 0.573, improvement 0.041, latency 0.049 |
| loss | 3.029 | 3.397 | 1.315 | 68/0/2 | throughput 3.074, improvement −0.038, latency 0.015 |
| combined | 0.359 | 0.385 | 0.089 | 69/0/1 | throughput 0.377, improvement 0.021, latency 0.077 |

Retry/failure/action costs are ≈0. Rewards are numerically healthy and
**dominated by the absolute aggregate-throughput benefit**; in every scenario the
reward magnitude (0.36–3.4) **exceeds** the exploration bonus ≈0.21.

**Greedy lock-in (white-box simulation, baseline x, reward 3.4):** arm 0's score
after 1 update = **1.842** vs **0.2001** for every untried arm; after 8 updates
= **3.089** vs 0.2001 (untried arms keep `A=I`, so their bonus never grows).
`ActionNone` is the argmax at every step — it can never be displaced once tried.

**Forced-action feedback** (mean reward over decisions; `Δt` = decision-0
reward; `aggΔ` = mean per-decision aggregate-throughput change; `chg` =
controller changes):

| Scenario | metric | none | inc-conc | dec-conc | inc-chunk | dec-chunk |
|---|---|---|---|---|---|---|
| baseline | reward / Δt / aggΔ | 3.52 / 3.52 / 17k | **6.96** / 3.30 / 513k | 3.47 / 3.49 / 11k | 3.48 / 3.51 / 60k | 3.45 / 3.43 / 17k |
| server-throttle | reward / Δt / aggΔ | 1.52 / 0.45 / 451k | **2.20** / 0.41 / 217k | 1.52 / 0.45 / 448k | **0.36** / 0.37 / 0 | 1.53 / 0.45 / 456k |
| latency | reward / Δt / aggΔ | 1.05 / 1.05 / 0.7k | **2.19** / 1.00 / 157k | 1.06 / 1.05 / 0.8k | 1.69 / 1.66 / 205k | 1.05 / 1.05 / 0.6k |
| loss | reward / Δt / aggΔ | 3.11 / 2.17 / −0.03k | **6.37** / 3.32 / 303k | 2.54 / 3.49 / −249k | 3.44 / 3.50 / 25k | 3.48 / 3.46 / 10k |
| combined | reward / Δt / aggΔ | 0.51 / 0.56 / −8k | **1.18** / 0.50 / 87k | 0.56 / 0.56 / 0.4k | 0.56 / 0.60 / 44k | 0.56 / 0.56 / 0.3k |

`ActionNone` action cost = 0; `inc-conc`/`inc-chunk` are charged only when the
controller actually changed (mean 0.021 / 0.050), and `dec-*` at the lower bound
are no-ops (cost 0). So `ActionNone` has **no positive reward bias beyond the
tie-break and avoidance of the churn cost**.

**Action separability.**
- **Concurrency: clearly separable.** Sustained `IncreaseConcurrency` raises the
  mean reward above `ActionNone` in **every** scenario (+3.44 baseline, +0.68
  throttle, +1.14 latency, +3.26 loss, +0.67 combined), driven by the wall-clock
  aggregate observable. This is **not** an identifiability failure.
- **Chunk size: not meaningfully separable.** `IncreaseChunkSize` vs `ActionNone`
  is inconsistent (baseline −0.03, throttle **−1.16**, latency +0.64, loss +0.33,
  combined +0.05). The throttle result is a **harness artifact**: the rate switch
  is request-count-based, so larger chunks mean fewer requests and the rate never
  steps up. The "latency" difference is just request duration (∝ chunk/rate).
- **Immediate credit is weak.** The decision-0 reward is nearly identical across
  forced actions (e.g. baseline 3.30–3.52) because the next completed attempt was
  already in flight; the action's effect only becomes visible over several
  sustained decisions.

**Concurrency observability (confirmed).** The diagnostic records
`overlap≈29–30` of ≈35 decisions for forced `IncreaseConcurrency` (concurrency>1
with aggregate > 1.2× per-request) and large positive `aggΔ`. The formal
benchmark independently shows wall-clock run rate exceeding per-request
`AverageThroughput` by 1.45× (latency) and 1.42–1.48× (combined) at
concurrency>1. So `AggregateThroughput` does capture overlap.

**Chunk-size observability (not meaningful).** No modelled chunk-size performance
tradeoff exists; observed differences come from request-count/latency artifacts,
not a chunk-size objective. This is an **environment limitation**, not a learner
bug.

**Root causes (evidence-based):**
- **B. Exploration (primary):** disjoint LinUCB gives an untried arm a *constant*
  bonus `alpha·‖x‖≈0.21`, which any positive-reward arm exceeds after one update;
  the explored arm is never displaced.
- **A. Initialization/tie-breaking (contributing):** all arms tie exactly at
  init; lowest-index tie-break picks `ActionNone` first, which then entrenches.
- **C. Reward scale (contributing):** the throughput-dominated reward
  (0.36–3.4) dwarfs the ≈0.21 bonus, so there is no exploration pressure.
- **F. Credit assignment (contributing):** the decision-0 ("first try") reward is
  nearly action-independent, so even a rare exploratory try is poorly
  informative until an action is sustained.
- **E. Environment limitation (chunk size only):** no modelled chunk-size effect.
- **D. State/reward identifiability: not the cause for concurrency** (forced
  concurrency is clearly rewarded); it applies only to chunk size via E.

**Limitations:** 5 reps, wall-clock/TCP variance (loss especially); forced runs
diverge after the first action, so this is not a perfectly paired counterfactual;
"latency" uses the server's `SetDelay` (privilege-free proxy) and only `loss`
uses tc/netem; results are descriptive and rank nothing.

**Code/data changed:** added `internal/diagnostics/linucb_exploration.go`
(+test) and `cmd/linucb-diagnostic/`, plus the white-box diagnostic test
`internal/rl/linucb_exploration_diagnostic_test.go`; new dataset under
`testdata/diagnostics/linucb_exploration/`. `AGENTS.md` gained one general
experiment-discipline rule (separate exploration failure from
environment/identifiability failure before tuning). That diagnostic task changed
**no production behavior**; the fix is implemented separately in §22.8.

### 22.8 LinUCB Exploration Correction

**Status: implemented and unit-tested.** The corrected mechanism is active for
the experiment's LinUCB mode. The **formal benchmark is frozen and was not
rerun**; no performance improvement is claimed (that requires a future benchmark
under §27).

**Diagnosed failure mode (§22.7).** `ActionNone` locked in because (B) disjoint
LinUCB gives an untried arm a *constant* bonus `alpha·‖x‖≈0.21` while the first
arm's posterior mean reached 0.36–3.4, (A) the initial all-arms tie is broken to
`ActionNone` first, (C) the reward scale dwarfs the bonus, and (F) one-step
credit is weak.

**Selected mechanism — explicit initial exploration (warm-up).** A bounded,
deterministic pass over the action space before normal UCB selection. It is the
smallest mechanism that directly guarantees the diagnosed precondition (every
arm gets evidence) and then hands control back to standard LinUCB.

**Configuration.** `LinUCBConfig.InitialExplorationRounds int`: default `0`
(disabled, preserves prior behavior and all existing tests); validated `>= 0`;
`internal/experiment.runOnce` sets it to `1` for `ModeLinUCB`. No other
configuration was added.

**Alternatives rejected.**
- *Alpha tuning* — does not structurally guarantee coverage, is an arbitrary
  knob, and masks the diagnosed cause; also explicitly out of scope.
- *Epsilon-greedy* — arbitrary `epsilon`, introduces randomness (breaking
  determinism/reproducibility) and changes the policy class; it can keep
  selecting bad arms forever rather than warming up once.
- *Optimistic initialization / unobserved-arm prior* — requires a
  reward-scale-dependent optimism hyperparameter, changes the score function,
  and is scale-sensitive; more distortion for no extra guarantee.

**Exact semantics.**
- With `R > 0`, the learner forces the first `R * NumActions` selections in
  `ActionIndex` order (`0..NumActions-1`, repeating for `R > 1`).
- The forced action flows through the **existing** causal path
  (`Select_t → execute → Observation_{t+1} → Reward_t →
  Update(State_t, Action_t, Reward_t) → Select_{t+1}`); it is updated with the
  runner-computed reward. After `R` passes, `exploring` becomes false and normal
  UCB scoring is used for every later decision. There is no permanent cycling.
- **No-op / bound handling:** at the next selection the learner compares the
  relevant target (`Concurrency` for concurrency actions, `ChunkSize` for
  chunk actions) between the selection state and the observed next state. A
  change marks the arm *effective*; no change marks it *no-op*
  (`warmupEffective` / `warmupNoop`). A bound-clamped action is therefore never
  counted as evidence of that action's effect. `ActionNone` is always recorded
  as effective (its intended effect is "no change"). The standard `A`/`b`
  update for the attempted arm is **unchanged** (required to preserve the causal
  mechanism); the warm-up only governs coverage, it does not suppress or rewrite
  the update.
- **Locked initial configuration:** with concurrency at its minimum and chunk at
  its minimum, the canonical order forces `none`, `increase-concurrency`,
  `decrease-concurrency`, `increase-chunk-size`, `decrease-chunk-size`, and the
  increases make their decreases effective — so all five arms get effective
  evidence and no warm-up decision is a bound no-op.
- **Invalid state:** a malformed/mismatched-dimension state still returns
  `ActionNone` and does not advance the warm-up (existing behavior preserved).

**Causal interaction / credit.** No second runner, no duplicate state/reward
computation, no change to the reward formula, weights, state, or feature
normalization. Credit assignment remains **approximate under concurrency > 1**
(one serialized decision per completed attempt); the warm-up provides evidence
through the existing observation/reward mechanism and does not claim perfect
causal attribution.

**Tests.**
`internal/rl/linucb_exploration_correction_test.go`: coverage of every action
once (and multiple rounds), determinism, transition to normal greedy selection,
no-op-not-counted effectiveness bookkeeping, invalid-state and mismatched-
dimension fallbacks, standard `A`/`b` update unchanged, and default-disabled
behavior. `internal/diagnostics/exploration_correction_test.go` demonstrates
before/after via the public API (artifact:
`testdata/diagnostics/linucb_exploration_fix/coverage.txt`). The existing
`TestNewLinUCBInvalidConfig` now also covers negative rounds.

**Limitations (preserved).**
- The **chunk-size** environment limitation is unchanged: the harness models no
  meaningful chunk-size performance tradeoff, so chunk-arm evidence may remain
  uninformative. This task does not address it.
- Credit assignment remains approximate under concurrency.
- The warm-up consumes the first `R * NumActions` decisions (with `R=1`, five
  decisions; in the 8-chunk benchmark scenario that is a substantial share).
- The correction is a **correctness** change (no lock-in); its effect on
  download performance is **not** evaluated here and must not be claimed.

### 22.9 Formal Benchmark v2 — Post-Exploration Correction

**Status: completed.** Descriptive only — no ranking, no winner, no performance
claim. This run uses the §22.8 corrected LinUCB (`InitialExplorationRounds=1`);
nothing was tuned.

- **Dataset (new; the v1 benchmark is frozen and untouched):**
  - `testdata/benchmark_v2/matrix_5mode_reps10_corrected.json` — raw
    `MatrixResult`, 250 runs.
  - `testdata/benchmark_v2/matrix_5mode_reps10_corrected.txt` — human-readable
    summary (configuration, correctness, warm-up, post-warm-up, performance,
    v1/v2 comparison, aggregate validation, limitations).
  - `testdata/benchmark/`, `testdata/baseline/`, `testdata/calibration/` are
    **byte-identical** to their frozen versions (sha256 unchanged; e.g. v1 json
    `9a69e97e…`, v1 txt `61709456…`).
- **Reproduce:** `go run ./cmd/experiment -matrix -sudo -reps 10 -json
  -modes fixed,concurrency-adaptive,chunk-size-adaptive,joint-adaptive,linucb
  > testdata/benchmark_v2/matrix_5mode_reps10_corrected.json`.
- **Configuration:** identical to v1 — `DefaultMatrixConfig`, reps=10, 5
  scenarios × 5 modes = 250 runs; file 256 KiB, chunk 32 KiB (min/max 32/128 KiB,
  step 32 KiB), concurrency 1 (min 1, max 4), thresholds 0.2/0.2, window 4,
  retries 3, timeout 30 s, LinUCB alpha 0.2, `DefaultRewardConfig`,
  `InitialExplorationRounds=1`. No random seeds exist; runs are independent
  fresh runs.
- **Correctness / validity:** **250/250 valid**, 0 invalid, 0 failed downloads;
  every run `Err==""`, `OutputMatches`, `NoDuplicateRanges`, `NoMissingRanges`.
  Network `none` for baseline/server-throttle and `applied` for
  latency/loss/combined; **0 unavailable runs**. SHA-256 is not implemented. tc
  cleanup verified (no residual qdisc).
- **LinUCB warm-up (first `R·NumActions = 5` selections):** in **all 5 scenarios
  and all 10 runs**, the exact expected order
  `none → increase-concurrency → decrease-concurrency → increase-chunk-size →
  decrease-chunk-size`, and **every non-None action was effective** (0 no-ops)
  — the locked initial state (concurrency min, chunk min) makes the increases
  move the controllers so the decreases bite. Warm-up mean rewards (descriptive)
  ranged e.g. baseline `none 3.46`, `increase-concurrency 3.34`,
  `decrease-concurrency 5.60`, `increase-chunk-size 4.12`,
  `decrease-chunk-size 4.30`.
- **LinUCB post-warm-up (learned) behavior:** the benchmark file yields
  **7 decisions per run**, so only **2 decisions per run (20 per scenario)** are
  learned. In that small sample normal LinUCB selected a single action per
  scenario: baseline/loss `decrease-concurrency` 100 % (no-op at the min),
  server-throttle `decrease-chunk-size` 100 % (no-op at the min), latency
  `increase-chunk-size` 100 % (effective, chunk 32→98 KiB), combined
  `increase-chunk-size` 90 % / `decrease-chunk-size` 10 %. **Concurrency changes
  after warm-up: 0** in every scenario; chunk changes only in latency (20) and
  combined (18). Max concurrency in the post phase was 1; final concurrency 1 in
  all scenarios. The post phase is too short to characterize sustained learning.
- **Observed performance (mean completion ms; not ranked; full table + CI in
  the summary):**

  | Scenario | fixed | concurrency-adaptive | chunk-size-adaptive | joint-adaptive | linucb |
  |---|---|---|---|---|---|
  | baseline | 72.9 | 72.8 | 72.9 | 72.9 | 63.7 |
  | server-throttle | 296.4 | 287.8 | 295.7 | 287.8 | 231.0 |
  | latency | 434.8 | 327.2 | 313.3 | 327.1 | 386.6 |
  | loss (high variance) | 113.6 | 171.0 | 92.7 | 94.2 | 64.6 |
  | combined | 638.8 | 489.0 | 542.2 | 475.2 | 557.0 |

  Retries and failed attempts were **0** in every run.
- **Comparison with the frozen pre-correction benchmark (behavioral, not
  superiority):** in v1 LinUCB selected `ActionNone` 100 % (8 decisions/run, 0
  controller changes). In v2 it performs 5 deterministic warm-up selections and
  then ~2 learned decisions; the warm-up makes all non-None actions effective at
  least once. v2 LinUCB completion times are lower than v1 in every scenario,
  but this is a **behavioral observation**: the forced warm-up itself changes
  concurrency/chunk size (and the learned chunk actions in latency/combined
  reduce request count), and the post-warm-up sample is only 2 decisions. **No
  performance improvement is claimed.**
- **Aggregate-throughput validation (benchmark):** wall-clock run rate
  (`FileSize/Duration`) vs the framework's per-request `AverageThroughput` on
  runs with concurrency > 1: latency concurrency-adaptive **1.45×**, combined
  concurrency-adaptive **1.52×** / joint **1.48×** (confirming the wall-clock
  aggregate captures overlap and differs from the per-request metric). The
  windowed `Context.AggregateThroughput` vs `Throughput` distinction is covered
  by the deterministic fake-clock runner test `TestPolicyRunnerAggregateThroughput`.
- **Statistical analysis:** independent fresh runs, no shared seed → unpaired,
  n=10/cell; mean, median, SD, descriptive 95 % t-CI (df=9). **No significance
  tests, no ranking.**
- **Limitations / anomalies:** (1) the 5-selection warm-up dominates the
  7-decision run, so the learned phase is only 2 decisions — the dominant
  limitation of this benchmark; (2) chunk-size performance tradeoff still not
  modelled, so chunk actions may be uninformative; (3) credit assignment remains
  approximate under concurrency > 1; (4) loss scenario high wall-clock variance;
  (5) fairness not implemented; no aria2/IDM comparison. No correctness
  anomalies occurred.

### 22.10 LinUCB Learning-Horizon Study

**Status: completed (design study).** A separate, labeled diagnostic to determine
how long a benchmark trajectory must be for the fixed 5-selection LinUCB warm-up
to leave enough post-warm-up decisions to evaluate learning. It changes **only
the file size**; every other parameter is the locked `DefaultMatrixConfig` value.
No frozen dataset was touched.

- **Why v2 was insufficient (§22.9):** the 256 KiB file yields 7 decisions, of
  which 5 are the forced warm-up, leaving only **2** learned decisions.
- **Dataset / reproduce:** `testdata/diagnostics/linucb_horizon/report.json` +
  `.txt`; `go run ./cmd/linucb-horizon -sizes
  262144,524288,1048576,2097152,4194304 -scenarios
  baseline,server-throttle,latency,combined -modes linucb,fixed -reps 3 -sudo`.
  New code: `internal/diagnostics/horizon.go`, `cmd/linucb-horizon/`, plus a
  test; **no production code changed** and `DefaultMatrixConfig` is untouched.
- **Decision-count semantics (measured, not assumed):** each completed attempt
  produces one policy decision, so decisions = number of allocated ranges (+
  retries; retries were 0 here). With a fixed chunk the total is `S / 32768`
  (fixed mode: 8/16/32/64/128 for 256 KiB…4 MiB). With adaptive chunk it is
  path-dependent: the warm-up's single 64 KiB allocation removes one range, and
  if the policy later drives chunk to its 128 KiB maximum (latency/combined) the
  total collapses by ~3.5×. Counts were deterministic across the 3 repetitions
  in every cell.
- **Candidate horizons (LinUCB, mean over 3 runs; post = after the 5 warm-up):**

  | File size | baseline total / post | server-throttle total / post | latency total / post | combined total / post |
  |---|---|---|---|---|
  | 262144 (control) | 7 / 2 | 7 / 2 | 7 / 2 | 7 / 2 |
  | 524288 | 15 / 10 | 15 / 10 | 9 / 4 | 9 / 4 |
  | 1048576 | 31 / 26 | 31 / 26 | 13 / 8 | 13 / 8 |
  | 2097152 | 63 / 58 | 63 / 58 | 21 / 16 | 21 / 16 |
  | **4194304** | **127 / 122** | **127 / 122** | **37 / 32** | **37 / 32** |

  Warm-up is exactly 5 in every cell (fixed). The 262144 control reproduces v2
  (7 total, 5 warm, 2 post), validating the harness. `fixed` mode (no policy)
  gives the intrinsic range count `S/32768` for reference.
- **Behavioral observation:** at every horizon the post-warm-up policy **stayed
  on a single action** for the whole learned phase (baseline/throttle:
  `decrease-concurrency` at the min bound, 100 %; latency/combined:
  `increase-chunk-size` to the max, 100 %); concurrency was never changed after
  the warm-up. The longer horizons show this lock is stable rather than
  revealing switching. This is a policy observation, not a horizon defect.
- **Selected horizon for the next formal benchmark: 4194304 B (4 MiB).**
  Rationale: it provides **≥32 post-warm-up decisions even in the worst case**
  (latency/combined, where chunk grows to 128 KiB) and ~122 in
  baseline/server-throttle — enough to split the learned phase into two ~15+
  decision halves and to observe several policy updates per arm; 2 MiB was
  rejected as marginal (16 worst-case). Estimated runtime of a full
  5-mode × 5-scenario × 10-rep benchmark is ~13–15 min (vs ~1 min at 256 KiB),
  which is acceptable for a one-off evaluation.
- **Side effects of the larger horizon:** the `server-throttle` transition stays
  at request 4, so the learned phase runs at the post-transition steady rate
  (4 MiB/s) and the throttled phase is a small early fraction; netem conditions
  are unchanged (applied for the whole run); retries remained 0; no new phases
  or bound changes appeared. Larger chunk sizes (not larger files) are what
  shrink the horizon, and that is accounted for in the selected size.
- **Limitations preserved:** the horizon study does **not** make chunk size
  scientifically identifiable (§22.7 E) and does not prove learning; it only
  establishes that the next benchmark has enough post-warm-up observations to
  attempt an evaluation. Approximate credit assignment under concurrency > 1
  still holds. The `loss` scenario was excluded from the study because it does
  not change the decision count (retries 0), but it remains in the benchmark
  scenario set.

### 22.11 Formal Benchmark v3 — 4 MiB Learning Horizon

**Status: completed.** The first formal evaluation with corrected
aggregate-throughput observability, the bounded warm-up, and a long enough
trajectory. Descriptive only — **no ranking, no performance claim**. Nothing was
tuned.

- **Dataset (new; v1/v2 and all diagnostics frozen and untouched):**
  - `testdata/benchmark_v3/matrix_5mode_4MiB_reps10.json` — raw `MatrixResult`,
    250 runs.
  - `testdata/benchmark_v3/matrix_5mode_4MiB_reps10.txt` — human-readable
    summary (config, correctness, horizon, warm-up, early/late learning,
    trajectory, performance, aggregate validation, limitations).
- **Reproduce:** `go run ./cmd/experiment -matrix -sudo -reps 10 -json
  -file-size 4194304 -modes fixed,concurrency-adaptive,chunk-size-adaptive,
  joint-adaptive,linucb > testdata/benchmark_v3/matrix_5mode_4MiB_reps10.json`.
  The `-file-size` matrix override is a small, explicit experiment-level flag
  (`cmd/experiment.applyFileSizeOverride`); the default `DefaultMatrixConfig`
  (256 KiB) is unchanged.
- **Configuration:** `DefaultMatrixConfig` with **FileSize = 4194304 B (4 MiB)**;
  everything else locked (chunk 32 KiB, min/max 32/128 KiB, step 32 KiB;
  concurrency 1 (1–4); thresholds 0.2/0.2; window 4; retries 3; timeout 30 s;
  LinUCB alpha 0.2, `DefaultRewardConfig`, `InitialExplorationRounds=1`).
  Scenarios unchanged (baseline/server-throttle/latency/loss/combined).
- **Correctness / validity:** **250/250 valid**, 0 invalid, 0 failed downloads;
  every run `Err==""`, `OutputMatches`, `NoDuplicateRanges`, `NoMissingRanges`.
  Network `applied` for latency/loss/combined, `none` otherwise, **0
  unavailable**. SHA-256 not implemented. tc cleanup verified.
- **Decision horizon (actual):** warm-up exactly 5 everywhere; learned decisions
  are scenario-dependent because chunk growth shrinks the horizon —
  baseline/server-throttle **122**, latency/combined **32**, loss **64 or 122**
  (some loss runs kept chunk small, others grew it), as warned in §22.10.
- **Warm-up:** exact expected order in all 50 LinUCB runs
  (`none → increase-concurrency → decrease-concurrency → increase-chunk-size →
  decrease-chunk-size`), **all non-None actions effective (0 no-ops)**. Warm-up
  mean rewards seed the posterior (e.g. baseline `decrease-concurrency` 5.60 vs
  `increase-concurrency` 3.33).
- **Early vs late learned phases** (early = post decisions 6–20; late = final
  15): the distributions are effectively identical in every scenario.
  - baseline: `decrease-concurrency` 100 % / 100 %, 0 concurrency or chunk
    changes.
  - server-throttle: `decrease-chunk-size` 100 % / 100 %, 0 changes.
  - latency: `increase-chunk-size` 100 % / 100 % (chunk driven to its 128 KiB
    max; 30 chunk changes early, 0 late).
  - loss: `decrease-concurrency` 95 % / 90 %, a little `decrease-chunk-size`
    (3 % / 10 %), 1 chunk change.
  - combined: `increase-chunk-size` 90 % / 10 % `none` in both phases (chunk to
    max; 27 changes early, 0 late).
- **Concurrency trajectory:** LinUCB **never selected `increase-concurrency`
  after the warm-up in any scenario**; post-warm-up concurrency changes = 0; max
  concurrency = 2 (the transient warm-up increase, reverted by the warm-up
  decrease); final concurrency = 1 everywhere. The corrected
  `AggregateThroughput` did not translate into any learned concurrency change on
  this horizon.
- **Chunk-size trajectory:** latency/combined drive chunk to the 128 KiB max and
  then keep selecting `increase-chunk-size` as a no-op; loss mostly keeps
  `decrease-concurrency` (chunk stays near 32–64 KiB); baseline/throttle keep
  chunk at 32 KiB. Per §22.7 E the harness models no chunk-size performance
  tradeoff, so this is **not** interpreted as successful chunk-size
  optimization.
- **Observed performance (mean completion ms; not ranked):**

  | Scenario | fixed | concurrency-adaptive | chunk-size-adaptive | joint-adaptive | linucb |
  |---|---|---|---|---|---|
  | baseline | 1155.5 | 1156.8 | 1154.0 | 1157.3 | 1162.7 |
  | server-throttle | 1376.9 | 572.9 | 1343.1 | 650.0 | 1322.5 |
  | latency | 6334.1 | 2303.1 | 2564.5 | 2069.2 | 2633.4 |
  | loss (high var) | 1709.5 | 1312.4 | 1534.1 | 1186.3 | 1635.3 |
  | combined | 9895.4 | 5357.1 | 6312.6 | 5548.5 | 6367.9 |

  Retries and failed attempts were **0** in every run. Full median/SD/95 % t-CI
  and wall-clock throughput are in the summary.
- **Aggregate-throughput validation (real C>1):** wall-clock run rate vs the
  framework's per-request `AverageThroughput`: server-throttle
  concurrency-adaptive **2.50×**, latency concurrency-adaptive **2.78×**,
  combined concurrency-adaptive **1.94×** (joint 2.12×/1.93×/1.78×); ratios ≈ 1.0
  where concurrency stays 1 (including LinUCB, which never increased
  concurrency). This confirms the wall-clock observable captures overlap; the
  windowed field itself is covered by the fake-clock runner test.
- **Policy evolution (v3 conclusion):** **no meaningful post-warm-up policy
  evolution was observed.** After the warm-up, LinUCB locked onto a single arm
  (usually the one with the highest warm-up reward) and held it for the entire
  learned phase in every scenario; early and late distributions match, and
  concurrency was never changed. The longer horizon did not produce observable
  adaptation — it made this lock-in clearly measurable rather than revealing
  switching. (Credit assignment remains approximate under concurrency > 1, so
  this is a behavioral observation, not a causal proof.)
- **Comparison with v1/v2 (behavioral):** v1 (256 KiB) `ActionNone` 100 %;
  v2 (256 KiB) warmed up then had only ~2 learned decisions; v3 (4 MiB) has
  32–122 learned decisions and shows a stable single-arm lock. v3 completion
  times differ from v1/v2 but the file size differs, so this is **not** an
  algorithmic-improvement claim.
- **Limitations:** chunk-size tradeoff not modelled; credit assignment
  approximate under concurrency > 1; n=10; no seeds; descriptive statistics;
  horizon varies by scenario (32–122 learned decisions); no fairness; no
  aria2/IDM comparison.

### 22.12 LinUCB Post-Warm-Up Lock-In Diagnostic

**Status: completed, diagnostic only.** Explains *numerically* why LinUCB locks
onto one arm immediately after the warm-up despite 32–122 learned decisions.
No production behavior was changed; v3 is a frozen input; no fix is proposed.

- **Research question:** "Why does LinUCB lock onto a single arm after warm-up?"
- **Data source:** the frozen `testdata/benchmark_v3/matrix_5mode_4MiB_reps10.json`
  (all 50 LinUCB runs, all 4 582 decisions), replayed. Artifacts:
  `testdata/diagnostics/linucb_lockin/{report.json,report.txt,score_trajectory.json,feature_analysis.json}`
  (deterministic; generated by
  `internal/rl/linucb_lockin_diagnostic_test.go`).
- **Posterior reconstruction method:** a white-box replay drives the **production**
  `LinUCB` (`Update`, `SelectAction`, and the unexported `score`) with the recorded
  per-decision state and reward, decomposing each arm's score into
  `mean = θ_aᵀx` and `bonus = α·sqrt(xᵀA_a⁻¹x)`. The v3 `DecisionEvent` records 8
  of 10 raw state fields; `SuccessfulRequests = RequestIndex` (failures/retries
  are 0) and `BytesTransferred = 32768` (its feature clamps to 1.0 for any bytes
  ≥ 1000). **Validation: the replay re-selects the recorded action for
  4 582/4 582 decisions (100 %)**, so the reconstruction is exact.
- **Feature analysis (all decisions):** `‖x‖ = 1.0066 ± 0.0068` (min 1.0005, max
  1.0546). `bytes_transferred` is **1.0 in 100 %** of decisions and contributes
  **98.7 %** of `‖x‖²`; the informative features are tiny — `concurrency` 0.1 %
  (98.9 % at 0), `chunk_size` 0.3 %, `throughput`/`aggregate_throughput` ≈ 0.0 %,
  `request_duration` ≈ 0.0 %, `successful_requests`/`total_attempts` 0.5 % each;
  `failed_requests`/`retries` are constant 0. `throughput` and
  `aggregate_throughput` are **0.993** correlated and `successful_requests` and
  `total_attempts` are **1.000** correlated (redundant). So the feature vector is
  nearly constant across decisions; `bytes_transferred` dominates the norm and
  (via the clamp) caps the exploration bonus at ≈ α·‖x‖ ≈ 0.20.
- **Exploration-bonus vs mean (the lock mechanism):** at the **first greedy
  decision** (decision 6, index 5) in every scenario the selected arm's mean is
  far larger than every other arm's score, and its own bonus is small:

  | Scenario | first greedy arm | mean | bonus | max other bonus | margin |
  |---|---|---|---|---|---|
  | baseline | decrease-concurrency | 2.71 | 0.145 | 0.141 | +0.52 |
  | server-throttle | decrease-chunk-size | 1.17 | 0.141 | 0.145 | +0.44 |
  | latency | increase-chunk-size | 0.48 | 0.141 | 0.145 | +0.12 |
  | loss | decrease-concurrency | 2.70 | 0.145 | 0.141 | +0.52 |
  | combined | increase-chunk-size | 0.28 | 0.141 | 0.145 | +0.06 |

  Because non-selected arms are **never updated**, their scores are **frozen**
  (e.g. baseline rep 0: N=1.909, IC=1.824, IS=2.194, DS=2.310 stay constant from
  decision 6 on) while the selected arm's mean keeps rising (2.71→3.16 by
  decision 11) and its bonus shrinks (0.145→0.076). The untried-arm bonus cannot
  grow (disjoint arms, `A_a = I + x xᵀ` updated only when selected), so once a
  mean exceeds ≈0.2 the arm can never be overtaken.
- **Warm-up effect:** the five warm-up actions are exact and all effective
  (0 no-ops). The warm-up **seeds** the lock: the arm with the highest warm-up
  reward becomes the first greedy choice. Warm-up mean rewards (baseline) were
  `none 3.458, increase-concurrency 3.325, decrease-concurrency 5.598,
  increase-chunk 4.073, decrease-chunk 4.269`; `decrease-concurrency` (a **no-op**
  at the minimum) therefore won the first greedy decision.
- **Action effectiveness (post-warm-up):** of 4 332 learned selections, **4 151
  (95.8 %) were no-ops**. `decrease-concurrency` 2 268 selections / 0 effective
  (100 % no-op); `decrease-chunk-size` 1 332 / 0 (100 % no-op);
  `increase-chunk-size` 609 / 58 effective (90 % no-op after saturating at
  128 KiB); `none` 123 (always effective by definition);
  `increase-concurrency` **0 selections**.
- **Credit-assignment observations:** the reward attributed to a no-op action is
  indistinguishable from `none` (it reflects the environment, not the action),
  and the warm-up reward ordering does not match action effectiveness: the no-op
  `decrease-concurrency` earned the **highest** warm-up reward in
  baseline/loss (5.598/5.610 vs `none` 3.458/3.443). This is the one-step
  `Select → execute → observe → reward` alignment: the reward for action *i*
  reflects the *next* completed request, which may predate the action's effect.
  In-flight misattribution cannot be proven from the recorded data alone, but the
  no-op reward inflation is direct evidence of temporal misalignment.
- **Cross-scenario / all-repetition results:**

  | Scenario | dominant arm | learned decisions (mean) | unique arms | conc changes | chunk changes | lock decision |
  |---|---|---|---|---|---|---|
  | baseline | decrease-concurrency | 122 | 1 | 0 | 0 | 5 (10/10) |
  | server-throttle | decrease-chunk-size | 122 | 1 | 0 | 0 | 5 (10/10) |
  | latency | increase-chunk-size | 32 | 1 | 0 | 30 | 5 (10/10) |
  | loss | decrease-concurrency | 116.2 | 1.5 (1–4) | 0 | 1 | 5–17 (3/10 switch) |
  | combined | increase-chunk-size | 41 | 1 | 0 | 27 | 5 (10/10) |

  Lock-in is immediate (decision 6) and identical across **all 10 repetitions**
  in 4 of 5 scenarios; `loss` is the only scenario with any post-warm-up
  switching (3/10 runs), consistent with its much higher reward variance
  (SD 1.07–1.38 vs 0.10–0.27).
- **Root causes supported by evidence:**
  - **A posterior dominance — CONFIRMED.** At decision 6 the selected arm's mean
    (0.27–2.71) exceeds every other arm's total score, the margin is positive
    (0.06–0.52), and non-selected arms' scores freeze while the winner's mean
    grows.
  - **B feature dominance — PARTIAL.** `bytes_transferred` is constant 1.0 and
    98.7 % of `‖x‖²`; the context is nearly constant and the clamp caps the
    bonus at ≈0.2. It does not rank arms directly (it is a constant), so it is a
    scale/identification issue rather than the ranking cause.
  - **C reward separability — NOT SUPPORTED.** Warm-up rewards differ materially
    across arms (e.g. baseline 3.33–5.73) and the earlier forced-action study
    showed concurrency is reward-separable; the lock is not caused by
    indistinguishable rewards.
  - **D exploration bonus — CONFIRMED.** The max bonus (\~0.145) is 2–19× smaller
    than the selected mean at the first greedy decision, and untried/unselected
    arms' bonuses cannot grow, so the first winner is permanent.
  - **E credit assignment — CONFIRMED.** A no-op arm receives the highest
    warm-up reward and 95.8 % of learned selections are rewarded no-ops; the
    one-step reward lags the action.
  - **F environment limitation — PARTIAL.** Confirmed for chunk size (increase
    saturates, 90 % no-op; no modelled tradeoff); **not** the blocker for
    concurrency (a real tradeoff exists and is rewarded, but the policy never
    selects it).
- **Unresolved / not established:** whether a change of context feature scale, an
  ever-growing exploration bonus, or a different credit scheme would remove the
  lock is untested; in-flight misattribution under concurrency > 1 cannot be
  isolated from the recorded data alone. **No fix is proposed here.**

### 22.13 Periodic Exploration Diagnostic (experimental correction)

**Status: experimental correction implemented and diagnostically evaluated; no
performance claim and no formal benchmark yet.** It addresses the §22.12
post-warm-up freeze: unselected arms are never updated, so their scores are
frozen and the first greedy arm wins permanently. This is **not** a final
algorithm redesign.

- **Mechanism (opt-in, default disabled):** new `LinUCBConfig.ExplorationInterval int`
  (validated `>= 0`, default `0`). When `> 0`, after every `ExplorationInterval`
  **normal** LinUCB decisions the learner forces one exploratory action; forced
  actions cycle through the action space in `ActionIndex` order
  (`none → increase-concurrency → decrease-concurrency → increase-chunk-size →
  decrease-chunk-size → …`) via a **separate deterministic cursor** that advances
  only when a forced action is selected. Between forced events selection is
  standard UCB. The initial warm-up is unchanged. `ExplorationInterval=0`
  reproduces the previous behavior exactly (no periodic state allocated).
- **Effective/no-op handling:** a forced action that cannot change its controller
  target (e.g. `decrease-concurrency` at minimum, `increase-chunk-size` at
  maximum) is still attempted but recorded as non-effective
  (`periodicEffective`/`periodicNoop`); it is never counted as evidence of that
  action's effect. The reward formula, `A`/`b` update, score equation, features,
  alpha, action space, downloader and controller semantics are all unchanged.
- **Diagnostic (deterministic replay of frozen v3, `interval ∈ {0,5}`):** the
  `interval=0` replay re-selects the recorded action for **4 582/4 582**
  decisions, validating the harness. Artifacts:
  `testdata/diagnostics/linucb_periodic_exploration/{report.json,report.txt}`
  (generated by `internal/rl/linucb_periodic_exploration_diagnostic_test.go`).
  The v3 environment states are held fixed, so this isolates the
  exploration/posterior mechanism, not download dynamics.
- **Action coverage (interval 0 → 5):** unique post-warm-up arms **1.0–1.5 →
  5.0** in every scenario; dominant-arm fraction **1.000 → 0.76–0.87**;
  `increase-concurrency` selections **0 → 10–53** per scenario; forced
  exploration events per scenario ≈5–20 (proportional to horizon).
- **Posterior mechanism confirmed:** forcing a previously frozen arm updates it.
  Examples (before → after the forced observation): baseline
  `increase-concurrency` mean 1.639→2.237, score 1.781→2.352, `A` diag 2→3;
  server-throttle `increase-concurrency` mean 0.200→1.277, score 0.342→1.392,
  `A` 2→3; baseline `increase-chunk-size` mean 2.077→2.517, score 2.219→2.632,
  `A` 2→3. So arms that were frozen now receive evidence and their scores move.
- **Forced no-ops reported separately:** per cycle roughly 3 of 5 forced actions
  are effective and 2 are bound no-ops (e.g. baseline forced: `none`,
  `increase-concurrency`, `increase-chunk-size` effective; `decrease-concurrency`,
  `decrease-chunk-size` no-ops). These are recorded and not treated as
  intervention evidence.
- **Lock-in effect:** on this frozen-state replay, periodic exploration
  **substantially increases action coverage** (all five arms are exercised and
  updated) but the **dominant arm preference is unchanged** — the same arm still
  receives the majority of selections (76–87%). So it breaks the *frozen-arm*
  problem (no arm is starved of updates) without, in this diagnostic, changing
  which arm the posterior prefers.
- **Limitations:** deterministic cycling is not learned exploration; the
  diagnostic replays frozen v3 states (not action-dependent download dynamics);
  the chunk-size environment limitation (§22.7 E) and approximate
  credit assignment under concurrency > 1 remain; **no performance conclusion**.
  The next step is a controlled benchmark comparing baseline LinUCB vs
  periodically-exploring LinUCB.

