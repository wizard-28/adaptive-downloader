## 24. Completed milestones (from repository state)

1. `RangeTracker` (half-open allocation/lifecycle) + tests.
2. `Fetcher` (single HTTP Range request) + tests.
3. `Assembler` (random-access range writes) + tests.
4. Fixed-concurrency end-to-end downloader + CLI.
5. `Metrics` collector (per-attempt records/aggregate).
6. Dynamic concurrency (worker pool + `concurrency.Controller`).
7. `Policy` abstraction (Context/Action/ActionKind, `FixedPolicy`).
8. `ThroughputPolicy` (concurrency-only rule-based).
9. Policy runner integration (serialized decisions; windowed throughput).
10. Controlled HTTP range experiment server.
11. Fixed-vs-adaptive experiment runner/matrix.
12. `tc`/`netem` network-condition abstraction + privileged integration test.
13. `chunksize.Controller` (future allocation target).
14. Chunk-size integration in the downloader (lazy allocation, in-place retry).
15. `ChunkSizePolicy` (chunk-size-only rule-based).
16. Generic joint action application in the runner.
17. `JointPolicy` (deterministic alternating dimension baseline).
18. Four-mode experiment framework + CLI.
19. Control-trajectory validation (forward + reverse) of the four modes.
20. Four-mode baseline dataset (`testdata/baseline/`).
21. RL environment contract: State/action/reward + adapter, no learner.
22. First learner: disjoint LinUCB behind `rl.Policy` (deterministic;
    Gonum-backed SPD linear algebra; not wired into the downloader or the
    experiment matrix).
23. First reward computation layer: `RewardCalculator` + `RewardConfig`, wired
    into `policyRunner` with correct credit assignment (reward the previous
    action from the next observation); configurable, tested, first baseline
    (not empirically tuned or validated).
24. Dependency refactor: replaced the project's hand-written numerical linear
    algebra (`internal/rl/linalg.go`) with Gonum (`mat.Cholesky`, `SymDense`,
    `SymRankOne`); one direct dependency, zero transitive; behavior preserved.
25. LinUCB integrated as an opt-in fifth experiment mode (`ModeLinUCB`) through
    the existing Adapter + policyRunner + RewardCalculator; causal loop
    validated by deterministic integration tests; default four-mode behavior and
    the baseline dataset unchanged; not benchmarked.
26. LinUCB calibration and reward-diagnostics layer (`internal/experiment/
    calibration.go`, `cmd/calibration`) with deterministic server delay/fail
    scenario knobs; diagnostic only; output in `testdata/calibration/`.
27. Reward design review + new baseline (Candidate C): absolute + signed
    improvement throughput, state-change-based `ActionCost`, new
    `ThroughputImprovement` component/weight; calibration re-run; no learner or
    policy changes; not tuned or benchmarked.
28. RL State Observability Audit + minimum correction: identified that the
    per-request `Context.Throughput` cannot expose the throughput effect of
    concurrency; added a windowed wall-clock `AggregateThroughput` observable to
    `policy.Context`/`rl.State`/`Features` (dim 9 → 10) and based the reward's
    throughput components on it; diagnostic calibration regenerated. No weight
    tuning, no benchmark.
29. Formal 5-mode benchmark (first formal run): 5 scenarios × 5 modes × 10
    repetitions = 250 runs with the locked `DefaultMatrixConfig`; 250/250 valid;
    dataset in `testdata/benchmark/`; descriptive analysis only (no ranking).
    LinUCB selected `ActionNone` in every run (documented exploration lock-in).
30. LinUCB Exploration Diagnostic (§22.7): a separate, labeled forced-action
    study (`internal/diagnostics`, `cmd/linucb-diagnostic`,
    `testdata/diagnostics/linucb_exploration/`) establishing that `ActionNone`
    dominates due to the exploration/reward-scale interaction plus the
    initialization tie-break, that concurrency *is* observable, and that chunk
    size is not meaningfully observable. No production behavior changed; the fix
    was intentionally not implemented.
31. LinUCB Exploration Correction (§22.8): `LinUCBConfig.InitialExplorationRounds`
    (default 0) plus a deterministic warm-up pass; `runOnce` sets 1 for LinUCB.
    Every arm gets at least one update before normal UCB; bound-clamped no-ops
    are recorded as non-effective. Tests + a before/after diagnostic artifact
    (`testdata/diagnostics/linucb_exploration_fix/`). Correctness only; the
    frozen benchmark was not rerun and no performance claim is made.
32. Formal Benchmark v2 — post-exploration correction (§22.9): 5 scenarios × 5
    modes × 10 repetitions = 250 runs with `InitialExplorationRounds=1`;
    250/250 valid; warm-up order exact and all actions effective in every run;
    new dataset `testdata/benchmark_v2/`; v1/baseline/calibration frozen.
    Descriptive only (the 5-selection warm-up leaves only ~2 learned
    decisions/run); no performance claim.
33. LinUCB learning-horizon study (§22.10): a separate diagnostic
    (`internal/diagnostics/horizon.go`, `cmd/linucb-horizon/`,
    `testdata/diagnostics/linucb_horizon/`) varying only file size; measured
    decision counts and selected **4 MiB (4194304 B)** for the next benchmark
    (≥32 post-warm-up decisions worst-case, ~122 typical). No production change;
    no frozen dataset touched.
34. Formal Benchmark v3 — 4 MiB learning horizon (§22.11): 5 scenarios × 5 modes
    × 10 repetitions = 250 runs at FileSize=4194304; 250/250 valid; warm-up
    exact and all actions effective; 32–122 learned decisions/scenario. Result:
    **no meaningful post-warm-up policy evolution** (single-arm lock-in, no
    concurrency change). New dataset `testdata/benchmark_v3/`; explicit
    `-file-size` matrix override in `cmd/experiment` (default unchanged). No
    tuning; no performance claim.
35. LinUCB post-warm-up lock-in diagnostic (§22.12): replayed the frozen v3 data
    through the production LinUCB (4582/4582 actions matched) and quantified the
    lock-in. Confirmed causes: posterior dominance (A), exploration-bonus cap
    (D), and credit-assignment lag (E); partial: feature-norm dominance by a
    constant `bytes_transferred` (B) and the chunk-size environment limitation
    (F); reward separability (C) not supported. Artifacts in
    `testdata/diagnostics/linucb_lockin/`. No production change; no fix.
36. Periodic exploration experimental correction (§22.13): added
    `LinUCBConfig.ExplorationInterval` (default 0) — a deterministic forced action
    every N normal decisions, cycling the five arms with a separate cursor; the
    warm-up is unchanged; bounds no-ops are tracked and not counted. Unit tests
    + deterministic frozen-v3 replay diagnostic
    (`testdata/diagnostics/linucb_periodic_exploration/`). Result: coverage rises
    from 1 to 5 unique arms and frozen arms receive posterior updates, but the
    dominant arm preference is unchanged. No performance claim; no benchmark yet.
37. Live periodic-exploration benchmark (§22.14): 5 scenarios × 2 LinUCB variants
    × 10 reps = 100 live runs (`ScenarioConfig.LinUCBExplorationInterval` +
    `-linucb-exploration-interval`), 100/100 valid. Periodic exploration raised
    unique post-warm-up arms 1.0–1.4 → 5.0 and moved the posterior (forced arm
    reached rank 1); in `loss` it changed greedy behaviour (increase-concurrency
    58→227; final concurrency 1.30→2.30), elsewhere the dominant preference was
    unchanged. Behavioral only; no ranking or performance claim. Dataset
    `testdata/benchmark_v4/`.
38. Final six-mode 4 MiB benchmark (§22.15): added `ModeLinUCBPeriodic`
    (`LinUCBPeriodicInterval=5`) and ran 5 scenarios × 6 modes × 10 reps = 300
    live runs, 300/300 valid. Both LinUCB variants evaluated beside the four
    deterministic controllers. Behavioral + descriptive performance only; no
    ranking/superiority claim. Dataset `testdata/benchmark_v5/`.
39. aria2 external baseline (§22.16): new `cmd/aria2-benchmark/` measured
    `aria2c 1.37.0` against the same controlled scenarios (50/50 valid). Observed
    4 concurrent connections with fixed 1 MiB splits (aria2's minimum) and one
    initial non-range request; no within-run adaptation; 0 retries. Descriptive
    v5 comparison recorded with a request-granularity confound; no ranking. IDM
    not evaluated (Windows-only). Datasets `testdata/benchmark_aria2/`,
    `testdata/diagnostics/aria2/`.
40. Request-granularity sensitivity study (§22.17): new `cmd/granularity-study/`
    ran the internal fixed downloader at a granularity-matched config (1 MiB
    chunks, concurrency 4) over the five scenarios (50/50 valid; 4×1 MiB
    requests, max in-flight 4). The matched config reproduces aria2 closely in
    4/5 scenarios (0.85–0.97×), substantially reducing the granularity confound;
    latency retains ~2.3× unexplained aria2 overhead. Descriptive only.
    Artifacts `testdata/diagnostics/request_granularity/`.
41. Wall-clock server rate schedule (§14.1): additive `RateSchedule` API
    (`SetRateSchedule`/`SetRateScheduleAt`/`ClearRateSchedule`/`Schedule`/
    `CurrentRate`, `Stats.CurrentRate`/`RateScheduleActive`, injectable clock) on
    the experiment server, with centralized rate selection. Time-based transition
    independent of request count/granularity; existing fixed-rate and
    `SetRateAfter` behavior unchanged. Tests + a deterministic fake-clock
    diagnostic (32 KiB vs 1 MiB sequences cross at different request indices but
    the same elapsed time). Experimental infrastructure only; no scenario or
    dataset changed.
42. Wall-clock throttle benchmark (§22.18): experiment-only
    `server-throttle-wallclock` scenario (`cmd/wallclock-throttle/`) — 512 KiB/s →
    4 MiB/s after 1 s elapsed; 6 modes × 10 reps = 60 runs, 60/60 valid. The
    transition occurred at ≈1.04 s elapsed for every mode while request counts
    varied 46–128. Standard LinUCB still locked onto `decrease-concurrency`;
    periodic LinUCB changed preference to `increase-concurrency` (final
    concurrency 3.90) and matched concurrency-adaptive (1323 vs 1325 ms).
    Descriptive only; no ranking. Datasets `testdata/benchmark_v6/`,
    `testdata/diagnostics/wallclock_throttle/`. No frozen dataset changed.
43. aria2 wall-clock throttle baseline (§22.19): `cmd/aria2-wallclock/` measured
    aria2 1.37.0 under the wall-clock throttle (same locked config), 10/10
    valid. aria2 made 4 requests (3×1 MiB) all starting within ~0.07 s, so none
    began at/after the 1 s transition and the whole transfer ran at 512 KiB/s
    (mean 2129.7 ms). Confirms the time trigger removes the request-count
    confound but does not equalize granularity. Datasets
    `testdata/benchmark_aria2_v2/`, `testdata/diagnostics/aria2_wallclock/`.
    IDM still unevaluated.
44. Wall-clock granularity-matched aria2 control (§22.20): extended
    `cmd/granularity-study` with an additive `-scenario wallclock` (default
    unchanged) matching aria2's 1 MiB/4-connection shape under the wall-clock
    throttle, 10/10 valid. All 4 requests started before 1 s (none reached
    4 MiB/s); mean 2055.7 ms vs aria2 2129.7 ms (+3.6 %). Matching request size
    and parallelism reproduces aria2's wall-clock behavior closely; the residual
    is consistent with aria2's extra non-range probe and connection management.
    Artifacts `testdata/diagnostics/request_granularity_wallclock/`. No frozen
    dataset changed.
45. Final research evidence audit (§22.21): deterministic synthesis
    (`scripts/final_research_audit.py`) of the frozen chain into
    `testdata/final_research_audit/`. Verified all dataset validity counts from
    raw JSON (no summary/raw discrepancy). Supported: periodic exploration breaks
    frozen-arm starvation, causes posterior updates and effective controller
    changes. Supported-but-limited: granularity explains much of the aria2
    difference; wall-clock removes the request-count trigger. Not supported:
    novelty, optimality, consistent performance gain, chunk-size optimization,
    internal-vs-aria2 superiority, real-network generalization, fairness, RL
    adaptation (it is deterministic periodic), causal one-step effectiveness.
    Recommended next phase: paper/report/thesis/presentation for the controlled
    synthetic-server scope only.
46. Sequential RL / Double DQN architecture + sanity (§22.22): a new episodic
    Double DQN controller with a current/temporal 13-feature state (no saturated
    bytes feature), a time-based reward (`r=-elapsed s`, gamma=1), Python
    (CPU-only PyTorch) training against a randomized analytical simulator, and
    deterministic Go inference loading a portable JSON model. LinUCB retained
    unchanged as baseline. Sanity training (seed 0, 300 episodes) produced finite
    loss/Q values, all-terminal episodes, and a reproducible model hash
    (`db8602b4…`); same seed = identical hash, different seed differs. **No
    performance claim; no benchmark; simulator≠final conditions; model is a
    sanity model.** Artifacts `testdata/diagnostics/dqn/`; code
    `internal/rl/dqn/`, `python/rl/`.
47. DQN training + held-out validation + live transfer (§22.23): trained
    Double DQN across seeds 0–4 (800 ep), selected via held-out VALIDATION
    (seed 4), evaluated on 8 unseen TEST conditions vs simulator baselines and
    ablations, and froze **FINAL DQN CANDIDATE v1**
    (`83427c53…`). Held-out simulator: DQN 1.440 s vs fixed/conc/joint 2.029 s
    (baselines tie in the analytical simulator) with mixed per-condition results;
    ablations degrade as expected. A 25-run live transfer (5 scenarios × 5 reps)
    was 25/25 valid and behaviorally sensible (latency → concurrency 3.6; combined
    → max chunk). **No performance claim; no aria2 comparison.** Artifacts
    `testdata/diagnostics/dqn_validation/`; code `python/train_validate.py`,
    `internal/rl/dqn/live.go`, `cmd/dqn-transfer/`.
48. DQN environment correction + Candidate v2 (§22.24): corrected the simulator
    (shared-bandwidth concurrency, per-request overhead chunk tradeoff, and
    request-count + wall-clock throttles on a continuous clock), which fixed
    baseline fidelity (scripted baselines now change controllers and no longer
    tie). Retrained seeds 0–4 (1200 ep), selected via validation (seed 1), and
    froze **FINAL DQN CANDIDATE v2** (`59cbc5e6…`). Held-out: DQN 1.233 s vs
    fixed 1.764 / conc 1.706 / chunk 1.755 / joint 1.710; ablations degrade. Live
    transfer v2 25/25 valid; under the wall-clock throttle unhelpful chunk
    decreases fell 260→46 and effective concurrency changes rose 18→144, and
    combined holds concurrency 4. Candidate v1 retained as historical. **No
    performance claim; no aria2 comparison.** Artifacts
    `testdata/diagnostics/dqn_validation_v2/`; code `python/train_validate_v2.py`,
    corrected `python/rl/sim_env.py`.
49. aria2 baseline calibration (§22.25): selected and froze ONE global aria2
    configuration from a separate 75-run validation set (3 configs × 5
    wall-clock profiles × 5 reps; 75/75 valid). C (4 connections / split 4)
    selected on lowest pooled mean completion time (1496.3 ms vs B 2697.6, A
    4889.2); no tie-break needed; 0 retries/failures. All requests began before
    the rate transition in every config. **FINAL ARIA2 BASELINE CONFIG v1**
    (metadata hash `6b8cf33e…`) is now frozen for the head-to-head. Calibration
    only; DQN/Candidate v2 untouched; no final-test conditions used. Artifacts
    `testdata/diagnostics/aria2_calibration/`; code `cmd/aria2-calibration/`.
50. Final DQN vs aria2 evaluation (§22.26): locked 8-condition wall-clock test
    matrix (240 matched pairs / 480 downloads, randomized order, fresh per run),
    frozen candidates verified, 480/480 valid. **The pre-specified outperformance
    claim was NOT supported:** DQN geometric ratio 1.757 (95% CI [1.669, 1.850]),
    i.e. ~75.7% slower, faster in 1/8 conditions and 31/240 pairs; all four
    criteria failed. A first campaign missing per-run netem cleanup was archived
    and discarded; the clean re-run is analysed. Reported honestly; no tuning.
    Artifacts `testdata/final_evaluation/`; code `cmd/final-evaluation/`,
    `python/analyze_final_evaluation.py`.
51. DQN v3 request-efficiency correction (§22.27): separated decision frequency
    from chunk size (decision interval 1/4/8, selected 4), extended the chunk
    ladder to {32..1024} KiB, mechanically costed requests, and expanded the
    training distribution. Candidate v3 (`96361535…`, seed 0) uses 13.8 req/MiB
    in held-out simulation (~2–3× faster than scripted baselines). Live transfer
    is **mixed**: fragmentation improved (throttle attempts 75→37, chunk to
    ~449 KiB) but concurrency adaptation regressed (stays at concurrency 1;
    slower under latency/combined). Experimental, **not** a validated candidate
    for the claim; Candidate v2 untouched; no aria2 comparison; no performance
    claim. Artifacts `testdata/diagnostics/dqn_validation_v3/`; code
    `python/train_validate_v3.py`, `python/rl/sim_env_v3.py`, `cmd/dqn-transfer`
    (`-max-chunk`).
52. DQN v4 adaptive-concurrency formulation (§22.28): final RL formulation —
    chunk fixed at 1 MiB, concurrency-only action space, parallel-wave simulator
    with a real concurrency optimum, decision interval 8 selected. Candidate v4
    (`227d9845…`, seed 1) is deterministic and makes **4 × 1 MiB requests** live
    (aria2's granularity) holding concurrency 3, 25/25 valid — removing the v2/v3
    fragmentation/chunk-drift failures with a clean sim-to-real transfer. This is
    **concurrency-only, not joint chunk/concurrency**. No aria2 comparison; no
    performance claim. Artifacts `testdata/diagnostics/dqn_validation_v4/`; code
    `python/train_validate_v4.py`, `python/rl/sim_env_v4.py`,
    `internal/rl/dqn/{policy,live}.go`, `cmd/dqn-transfer` (`-fixed-chunk`).
53. DQN Candidate v5 simulator/state audit (§22.29): audited the v5
    concurrency-only formulation (state spec version 2) and fixed six
    correctness defects — decision-interval concurrency suppression, unapplied
    mid-step wall-clock phase transitions, a dead `request_duration_slope`,
    per-interval instead of per-attempt window counters, fabricated live
    progress (`successes × last bytes`), and a Python export contract that could
    not produce a Go-valid v5 model. Added deterministic Python/Go regression
    tests including a shared exact feature-vector contract. No v5 model exists
    and no training/validation/live-transfer run has been performed; the v1–v4
    candidates and the whole `testdata/` manifest are byte-identical.
54. DQN Candidate v5 training/validation pipeline (§22.30): implemented
    `python/train_validate_v5.py` (static concurrency sweep prerequisite,
    validation-only decision-interval selection, 5-seed training,
    validation-only seed selection, held-out test vs fixed C=1..4, behavior
    diagnostics, pre-specified constant/ramp NOT-READY gate, one export) plus
    the Go artifact smoke test and the `cmd/dqn-transfer` version-aware
    dispatch. The static sweep shows distinct best fixed C {1,2,3,4} across 8
    regimes; the gate passes (constant fraction 0.062, ramp-to-max 0.219, 64
    effective changes) but the model is slower than the best fixed C on 7/8
    held-out conditions. Candidate v5 SHA-256 `3a927a58…`, reproducible. No
    live transfer, no aria2 comparison, no performance claim.
55. DQN Candidate v5 live transfer validation (§22.31): new experiment-only
    `cmd/dqn-live-v5/` (authoritative controller-target before/after logging,
    wall-clock transitions, per-run netem apply/clear, full accounting checks)
    ran 5 scenarios × 5 policies (fixed C=1..4 + v5) × 5 reps = 125 live runs
    on the controlled server at 16 MiB / 1 MiB chunks: 125/125 valid, 0
    network-unavailable, qdisc clean. v5 tracks fixed-C3 closely and is slower
    than fixed-C4 everywhere; it is non-constant but oscillatory (noisy ramp to
    C3–C4). The three pre-specified gates pass; the qualitative reading is
    shallow adaptation. Candidate hash unchanged; v1–v4 and aria2 v2 untouched;
    no aria2 comparison, no performance claim.
56. Final v5-vs-aria2-v2 head-to-head (§22.32): new experiment-only
    `cmd/final-evaluation-v5/` reused the established U1–U8 / 30-pair /
    order-seed methodology at the frozen 16 MiB scale and ran 480 downloads
    (240 matched pairs) with 480/480 valid. v5 was slower in all 8 conditions
    (pooled geometric ratio 2.3217, 95 % CI [2.2269, 2.4206]; v5 faster in
    1/240 pairs and 0/8 conditions; all pre-specified criteria false). The
    mandatory 4-vs-16 concurrency asymmetry (v5 cap vs aria2-F's 16 connections
    with independent server pacing) is preserved in the artifacts. Statistics
    reused `python/analyze_final_evaluation.py` unchanged; the human report is
    generated by `python/report_final_evaluation_v5.py`. All prior frozen
    artifacts unchanged; no tuning, no post-hoc changes; no superiority claim.
57. Candidate v6 pre-registered unstable-network study (§22.33–§22.34): additive
    v6 state/simulator/live code (state spec version 3, concurrency 1–16, one
    decision per completed request); static C=1..16 sweep with distinct optima
    {1,2,5,9,16} (GATE A); 5-seed Double DQN training and validation-only
    selection (GATE B: 4.476 s vs best global fixed C12 4.867 s on held-out
    dynamic simulator conditions); condition-dependent phase behavior (GATE C:
    constant/ramp 0, 3/5 responsive); and 255/255 valid live runs on the
    controlled server under the same 5 trajectories (GATE D: 4/5 responsive,
    non-constant, non-ramp). v6 was slower than the best fixed C in every live
    condition (v6 7578 ms vs best global fixed C11 5012 ms); the live
    per-response pacing rewards high concurrency and the shared-bottleneck
    tradeoff transfers only partially. Candidate SHA-256 `daa71767…`,
    reproducible. No aria2 comparison; no performance/superiority claim.
58. Final pre-registered v6-vs-aria2-F head-to-head (§22.35): 150 matched pairs
    (300 runs, 300/300 valid) on the five v6 trajectories A2–E2. Pooled
    geometric ratio 1.4560 (95 % CI [1.3004, 1.6303]) → v6 slower; v6 faster
    only on E2 (ratio 0.948, Holm p = 0.128, not significant); all
    pre-specified criteria false → claim not supported. v6 remained
    condition-responsive, but the adaptation did not beat aria2-F under these
    controlled unstable trajectories. No tuning, no post-hoc changes; all
    frozen artifacts unchanged; no general-superiority or mobile-network claim.
59. D2 replication + long-trajectory diagnostic (§22.37): 20 matched pairs at
    512 MiB/D2 reproduced the v6 advantage (ratio 0.5816, 95 % CI
    [0.566,0.598], v6 faster 20/20) and identified the mechanism as a
    request-granularity × phase-locked-pacing interaction (aria2's 32 MiB pieces
    trapped at the initial 640 KiB/s rate; v6's 1 MiB pieces mostly issued after
    recovery). A new 7-phase long multi-transition trajectory L1 was designed
    (12–18 s phases) and swept in the frozen simulator: per-phase best C ∈
    {1,2,3,5,16}, but the full 512 MiB trajectory spread is only ~4 % (best
    fixed C≈15), so the duration hypothesis is not yet strongly promising.
    512 MiB remains the largest RAM-safe live size. No training, no v7, no
    change to §22.35 or any frozen artifact.
60. L2 environment design + V7 gate (§22.38–§22.39): pre-registered seven-phase
    512 MiB L2 trajectory with 18–25 s phases; per-phase best C = [16,2,4,16,2,
    6,16] with C16 penalties up to 62 % in degraded phases, but the full-run
    achievable headroom (ramp-aware oracle vs best static C16) is −0.3 %
    (idealized instant oracle 11.4 %, unreachable under ±1 actions); live probe
    shows aria2 1.37.0 cannot deliver ~1 MiB ranges with ≤16 connections at
    512 MiB (split=512 still yields 15×32 MiB pieces). Gates:
    NOT_READY_ENVIRONMENT_TOO_WEAK and NOT_READY_ARIA2_GRANULARITY. No v7
    training; frozen artifacts unchanged.
61. v7 formulation + L3 environment design (§22.40–§22.41): pre-registered and
    measured a ±4 control-authority action space (selected from A/B/C) plus a
    byte-weighted 7-phase L3 trajectory. Phase optima [16,2,4,16,2,6,16] (GATE A
    pass), action-constrained oracle 140.5 s vs best static C16 152.9 s →
    8.1 % headroom (GATE B fail, threshold 10 %), transitions reached with work
    (corrected metric), additive runner step semantics with a contract test
    (GATE D pass), live shared-cap tradeoff confirmed from frozen v6 live data
    (GATE E pass), authority sufficient (GATE F pass). **OUTCOME: STOP — v7
    training not justified.** The only code change is the additive
    `policyRunner` step semantics (Value ≤ 0 unchanged for v1–v6); all frozen
    artifacts and the frozen v6/aria2-F unchanged.
62. L4 environment design + final outcome (§22.42–§22.43): pre-registered
    byte-weighted 7-phase L4 trajectory keeping the L3 ±4 control formulation.
    Simulator: best static C16 = 174.0 s, action-constrained oracle = 143.4 s →
    17.6 % headroom (GATE B PASS), distinct phase optima (A), transitions with
    work (C), parity (D), authority (F); byte-weighting 47.1 % vs ≥50 % rule.
    Representative live fixed-C sweep (`cmd/l4-live-probe`): high-capacity and
    recovery regimes match (C16 ≫ C1), but the degraded shared-cap regime does
    not penalize high C live (C16 3.57 ≈ C8 3.39 > C4 2.58 MiB/s), contradicting
    the simulator's low-C optimum. **FINAL OUTCOME: `NOT_READY_LIVE_MISMATCH`.**
    Another RL training iteration is NOT justified. No training, no new aria2
    comparison; frozen artifacts unchanged.
63. T1 live request-level server throttling (§22.44–§22.45): pre-registered
    512 MiB normal→throttled→recovered schedule with 1 MiB requests. Live smoke
    verified request-level pacing persistence (GATE A), but the required
    fixed-C sweep showed C4–C16 finish before the throttle at normal 8 MiB/s
    and aggregate throughput increases monotonically with C, so there is no
    live concurrency reversal at equal 1 MiB granularity (GATE B/E FAIL;
    granularity-isolation D PASS). The oracle attempt was invalidated by the
    schedule flaw (ran entirely pre-throttle, headroom −5.3 %). **Outcome:
    `NOT_READY` — no RL iteration justified.** Confirms D2's v6 benefit was a
    request-granularity effect, not adaptive concurrency. No training, no new
    aria2 comparison; frozen artifacts unchanged.
64. T2 adaptive range-size diagnostic (§22.46–§22.47): live static chunk sweep
    at fixed C=16 over a two-throttle 512 MiB trajectory. The largest 16 MiB
    chunk was fastest (184.2 s vs 245.7–267.0 s for smaller chunks) because
    requests started in fast phases retain the fast rate across throttles and
    bypass them; smaller chunks keep sampling the slow rate. No chunk-size
    preference reversal, and no fixed change beat the best fixed chunk with
    adaptive control. **Outcome: `NOT_READY_NO_CHUNK_TRADEOFF`** (classifier
    logged `NOT_READY_NON_RL_GRANULARITY_EFFECT` due to a phase-start
    artifact). No RL range-size training justified; no training, no aria2
    comparison; frozen artifacts unchanged.
65. M1 real mobile-trace replay (§22.48): prerequisite audit only. No real
    mobile/cellular trace exists in the repository (only synthetic request
    traces), and the pre-registered rules forbid fabricating mobile data, so
    the stage stopped before replay construction or any concurrency
    experiment. **Outcome: `NOT_READY_NO_REAL_MOBILE_TRACE`.** Public real
    sources are reachable (UGent/imec 4G logs and GitHub returned 200/301),
    so a dedicated data-acquisition + replay-fidelity stage is the next viable
    step; the capacity-vs-observed-throughput caveat must be resolved then.
    No training, no aria2 comparison; all frozen artifacts unchanged.
66. M2 mobile trace parsers + tests; M3 deferred (§22.50): added
    `python/rl/mobile_trace.py` and `python/tests/test_mobile_trace_m2.py`
    (deterministic NYU-METS/UGent parsing, canonicalization, quality and hash
    helpers). M3's throughput-envelope replay, fidelity validation, fixed-C
    sweep, temporal `C*(t)`, oracle and all gates were NOT executed and are
    not claimed. No RL, no aria2; frozen artifacts unchanged.
67. Early-replay corrective fixes (§22.63; code/tests only, no native runs):
    child downloader errors now exit the child non-zero and are persisted
    (`fatal`/`canceled`/`err` in `summary.json`); `SUCCESS_TRACE_REACHED_EOF`
    now requires a normal mm-link exit, no fatal child error, **and** a
    downlink-log endpoint within the frozen 100 ms `AlignmentTolerance` of the
    `.down` trace duration, exposed through explicit `Result` evidence fields
    and new outcomes `DOWNLOADER_FATAL_ERROR` / `TRACE_ENDPOINT_NOT_REACHED`;
    the pilot resolves `<family>.up`/`<family>.down` pairs and rejects
    missing/mismatched pairs; the pilot request timeout is explicit
    (`-timeout`, default 120 s) and the 45 m watchdog is unchanged. Tests cover
    synthetic CASES A–F plus child status/exit and pairing, and the full
    `go test`/`-race`/`-count=5`/`vet`/`gofmt`/`build` suite passes. The native
    diagnostics were subsequently run and passed in milestone 68; no duration
    is frozen and `/home/vscode/epoch_pilot` is untouched.
68. Native replay diagnostics (§22.64): the two pre-pilot validations
    (`Verizon-EVDO-driving` C=1 and `Verizon-LTE-driving` C=16, 120 s request
    timeout, 45 m watchdog, fresh isolated output dirs) were executed as full
    real-time replays and both returned `SUCCESS_TRACE_REACHED_EOF` with
    `endpoint_valid=true`, `mm_link_exit=0`, child status `canceled` (no fatal
    error), endpoint residual **−35 ms** (within the frozen 100 ms
    `AlignmentTolerance`), and complete downlink delivery. No full pilot, no
    epoch-duration selection, no headroom/oracle/tau, no RL/adaptive control,
    no aria2 comparison, no protocol change; cleanup verified (no mm-link, no
    namespaces, qdisc clean). Outcome: `NATIVE_DIAGNOSTICS_PASS — READY FOR
    CORRECTED PILOT` (does not imply `EPOCH_DURATION_READY`).
69. Corrected formal epoch pilot (§22.65): the 10-run matrix was launched
    (`/home/vscode/epoch_pilot_corrected`) with the registered protocol. EVDO
    C=1 and C=2 completed valid trace EOF; EVDO C=4, C=8 and C=16 died early
    with fatal `retry limit exceeded` (2 attempts) at trace times 512.3 s /
    240.1 s / 150.0 s of 1 062.0 s, and the per-run gate correctly rejected
    them (`DOWNLOADER_FATAL_ERROR`, `endpoint_valid=false`). Per the
    pre-registered stop rule the pilot was stopped; LTE C=1 was
    operator-aborted and LTE C=2 was killed with the driver (both recorded
    artefacts), LTE C=4/8/16 were not run. No epoch extraction, shared
    validity, candidate coverage, duration selection, oracle, tau, or headroom
    was computed. Outcome: `BLOCKED — FORMAL EPOCH PILOT INVALID`. No protocol
    parameter changed; `/home/vscode/epoch_pilot` untouched.
70. EVDO timeout-censoring diagnosis (§22.66): diagnosis-only, no code or
    protocol change. Frozen-artifact re-analysis (trace max gap 11.6 s; C1/C2
    service-time scaling; C8's exact 2×120 s death; C16's 120 s + 30 s dial
    death) plus a controlled 120 s vs 600 s sensitivity sweep on EVDO
    C=4/8/16 (six fresh runs) established that the 120 s per-request timeout
    censors high-C requests under the shared low-rate link: C4/C8 reach
    genuine trace EOF at 600 s (4 and 24 completed requests had needed >120 s;
    max 183.9 s / 421.0 s), while C16 at 600 s advances to 62.0 MB / 969.5 s
    but still dies when the 600 s request timeout (~940 s) is followed by the
    fixed 30 s transport dial timeout on the retry (~971 s). Outcome:
    `TIMEOUT_CENSORING_SUPPORTED`; the formal 120 s timeout is unsafe for the
    fixed-C EVDO measurement, and any remedy requires a separately
    pre-registered protocol/harness decision. Diagnostic artifacts under
    `/home/vscode/evdo_timeout_diag/`; frozen pilot dirs untouched.
71. Timeout-policy decision gate (§22.67; implemented in §22.68). Inventoried
    the full deadline stack (120 s `http.Client.Timeout`, fixed 30 s transport
    dial timeout, unused TLS/Expect timers, 90 s idle timeout, 45 m parent and
    child watchdogs, trace-EOF signalling, retry semantics) and verified from
    code plus the existing synthetic tests that the 45 m watchdog cleanly
    bounds and cleans up a blocked replay. Recommended a **watchdog-only**
    formal policy (no HTTP request deadline, no dial deadline; 45 m watchdogs
    retained; retry semantics unchanged), rejecting 120 s, 600 s, a
    watchdog-derived deadline, and dial-timeout retention. No code or protocol
    change; a versioned pre-registration plus a fresh complete pilot is
    required. Outcome: `TIMEOUT_POLICY_READY_FOR_PREREGISTERED_REVISION`.
72. Watchdog-only timeout policy implemented (§22.68): explicit no-request /
    no-dial deadline configuration in `internal/fetcher` (with ambiguity
    validation) and `internal/downloader`, `replay.ParseDeadline`, pilot and
    mm-replay `-timeout`/`-dial-timeout` flags whose formal default is
    `none`/`none`, and a versioned protocol revision (`epoch.Protocol`
    version 2, `LegacyProtocolV1`, `ValidateTimeoutPolicyRevision`). The
    request context chain and retry semantics are unchanged; trace-EOF,
    watchdog, external-cancellation and fatal-error outcomes remain distinct.
    All required synthetic tests pass, full `go test`/`-race`/`vet`/`gofmt`/
    `build` pass, and one bounded EVDO C=1 native smoke with
    `-timeout none -dial-timeout none` returned `WATCHDOG_TIMEOUT` with clean
    teardown. No full pilot was run; a fresh complete 10-run version-2 pilot
    in a new output dir is the next step. Outcome:
    `WATCHDOG_ONLY_POLICY_IMPLEMENTED_AND_VALIDATED`.
73. Formal Stage 0 pilot under protocol v2 (§22.69): the fresh 10-run
    watchdog-only matrix (`/home/vscode/epoch_pilot_v2`, 2026-09-27
    08:53–12:15 UTC) completed with all 10 runs valid trace EOF
    (`endpoint_valid=true`, residuals −35…−58 ms, no fatal error, no workload
    exhaustion, `-timeout none -dial-timeout none` per run). Epoch extraction
    and shared validity completed; coverage: 30 s 43/82 (52.4 %), 60 s 25/41
    (61.0 %), 90 s 21/28 (75.0 %), 120 s 18/21 (85.7 %, min-trace 7) — only
    **120 s** passes the frozen rule and was selected (`2m0s`). Cleanup
    verified; historical v1/v2 diagnostic artifacts untouched. Outcome:
    `EPOCH_PILOT_V2_COMPLETE — READY_FOR_ORACLE_STAGE`; no oracle/headroom/tau
    computed.
74. Oracle stage preparation (§22.70): the frozen v2 input was verified
    (protocol v2, selected 120 s, 21 candidate / 18 shared-valid / 3 excluded
    epochs; EVDO 7, LTE 11) and preserved as deterministic analysis artifacts
    under `/home/vscode/oracle_stage_v2/`. Formal headroom was **not**
    computed: the repository contains no pre-registered tau regime classifier/
    threshold, no tau measurement (switch-injection) design, and no
    completion-time/workload mapping, as §22.52 already records. No threshold
    or measurement was invented. Outcome: `BLOCKED — ORACLE PREREQUISITE
    UNRESOLVED` (specific gate: `BLOCKED — TAU REGIME RULE NOT
    PRE-REGISTERED`). Validation suite passes; pilot artifacts unchanged.
75. Oracle/tau pre-registration + native tau measurement (§22.71): the oracle
    objective (B_e = 5 MiB, t = B_e/R, constrained-oracle ladder/dwell, H_j),
    the native-regime classifier, the 2.0 regime threshold, the injection
    selection, and the tau aggregation rule are frozen in
    `testdata/oracle_preregistration_v2/`. `internal/experiment/tau` and
    `cmd/tau-injection` implement them; `replay.ChildConfig` gained
    harness-only adaptive bounds and a scheduled switch. The 72-run
    switch-injection campaign (9 timestamps × 8 directions, protocol v2)
    completed 72/72 valid with 0 failures; all eight directions are measured
    and aggregated (only 1→2 is regime-specific under the frozen rule; pooled
    |tau| ≤ 0.57 s elsewhere). No headroom/oracle was computed. Outcome:
    `TAU_PREREGISTERED_AND_MEASURED`. A simultaneous-launch mm-link address
    race was found and mitigated with a 5 s launch gate.
76. Formal oracle/headroom analysis (§22.72): the exact frozen DP produced
    T_best_fixed (EVDO 543.98 s at C8; LTE 68.52 s at C16), T_instantaneous
    (534.81 / 68.26 s), and T_constrained (535.24 / 68.22 s) with persisted
    ladder/dwell-valid sequences; descriptive two-trace headroom is EVDO
    1.61 %, LTE 0.44 %. The pre-registered sanity bound
    `T_instantaneous <= T_constrained` fails for LTE because the frozen taus
    preserve negative measured values (8→4 −0.1303 s, 8→16 −0.1249 s); the
    DP was verified by exhaustive brute force (exact match) and no value was
    clamped. Outcome: `BLOCKED — ORACLE COMPUTATION INCONSISTENT`; the
    nonnegative-tau sensitivity (EVDO 1.58 %, LTE 0.047 %) and zero-tau
    diagnostic (1.64 % / 0.17 %) are recorded as secondary. n=2 →
    `FINAL_STATISTICAL_GATE = PENDING`. Artifacts under
    `/home/vscode/oracle_stage_v2/`, byte-identical across two runs.
77. Nonnegative formal tau, oracle protocol version 2 (§22.73): the signed
    cost model was superseded by `tau_formal = max(0, estimator)` (estimator
    and 2.0 regime rule unchanged; raw negatives preserved), frozen in
    `testdata/oracle_preregistration_v3/` before a fresh campaign. The v3
    switch-injection campaign (`/home/vscode/tau_campaign_v3/`, identical
    design/timestamps, plan hashes matching v2) completed 72/72 valid with 0
    failures; 32 raw negative observations retained; formal tau is ≥ 0 for
    every direction (2→1 and 8→4 floored to 0; no regime-specific directions
    in the fresh data). Headroom was **not** recomputed; the v2 pilot and v2
    signed-tau campaign are untouched historical evidence. Outcome:
    `TAU_COST_MODEL_V2_READY_FOR_ORACLE`.
78. Formal oracle/headroom under oracle protocol version 2 (§22.74): using the
    frozen nonnegative formal tau table, the exact DP produced T_best_fixed
    (EVDO 543.98 s at C8; LTE 68.52 s at C16), T_instantaneous (534.81 /
    68.26 s) and T_constrained (535.24 / 68.50 s) with persisted
    ladder/dwell-valid sequences; all sanity inequalities hold. Descriptive
    two-trace headroom: **EVDO 1.6075 %, LTE 0.0320 %**, both below the 10 %
    target. Historical signed-tau reproduction (§22.72) and zero-tau
    diagnostics are recorded separately. n=2 → final statistical gate
    `PENDING`. Artifacts under `/home/vscode/oracle_stage_v3/`, byte-identical
    across two runs; outcome `ORACLE_STAGE_V3_COMPLETE —
    DESCRIPTIVE_HEADROOM_COMPUTED`.
79. Final trace-expansion decision gate (§22.75, preparation/audit only): a
    read-only audit found that the final ≥5-trace protocol is **not frozen**.
    The "≥5 usable diverse traces, CI_95_lower(H) ≥ 0.10" string exists only
    in analysis output/docs, not in `oracle_preregistration_v3`; diversity
    strata, admissibility/duration/contamination rules, additional trace
    identities, tau scope for new traces, reuse rules, and the exact
    statistical formula were all UNFROZEN at audit time (now frozen in §22.76).
    The classifier is frozen and
    applies to the proposed candidates (`ATT-LTE-driving`,
    `TMobile-LTE-driving`, `TMobile-UMTS-driving`; 4/2/5 injection
    timestamps). Conditional minimum work if the aspiration held: 3 additional
    traces × 5 C = 15 fixed-C replays, plus 0–88 tau runs depending on the
    unfrozen scope. No runs, no headroom recomputation; audit artifacts under
    `/home/vscode/final_trace_expansion_decision/`. Outcome:
    `BLOCKED — FINAL TRACE PROTOCOL INCOMPLETE`.
80. Final trace protocol frozen (§22.76, documentation only): the
    prospective pre-registration `testdata/oracle_preregistration_v4/
    final_trace_protocol.json` (+ `.txt`) freezes the five-trace population
    (Verizon-EVDO-driving, Verizon-LTE-driving, ATT-LTE-driving,
    TMobile-LTE-driving, TMobile-UMTS-driving), eligibility (≥450 s, paired
    `.up`/`.down`, EOF/endpoint validation, no fatal error, existing
    contamination checks, one count per family, no post-hoc substitution),
    the existing replay/oracle rules, **per-trace tau**, reuse rules
    (`epoch_pilot_v2`/`tau_campaign_v3` for the two current traces only;
    fresh fixed-C and per-trace tau for the three new traces;
    `tau_campaign_v2` excluded), the headroom definition, the statistical
    design (mean H, sample SD, one-sided 95 % t LCB with df = n−1, pass iff
    `LCB_95 >= 0.10`), and invalid/missing handling. The experiment matrix is
    15 fixed-C + 88 tau = 103 new runs. Planning artifacts updated; no source
    or measurement change. Outcome: `FINAL_TRACE_PROTOCOL_FROZEN — READY FOR
    FINAL TRACE EXECUTION`.
81. Stage A executed (§22.77, measurement only): the 15 fixed-C full-trace
    protocol-v2 replays for the three new traces ran in a fresh output dir
    (`/home/vscode/final_fixed_c_stage_a/`, 2026-09-27 21:17:05 →
    2026-09-28 00:38:47 UTC). 15/15 valid (`SUCCESS_TRACE_REACHED_EOF`,
    endpoint residuals −34…−57 ms, no fatal/child error, no workload
    exhaustion, `'-timeout' 'none' '-dial-timeout' 'none'` in every run
    header), 0 technical failures, 0 reruns. All three new families eligible
    (≥450 s, paired, EOF-validated). No tau/oracle/statistical computation;
    all frozen artifacts unchanged; no source change. Outcome:
    `STAGE_A_COMPLETE — 15/15 FIXED-C REPLAYS VALID`. Next: 88 per-trace tau
    runs, then the per-trace oracle and the pre-registered five-trace gate.
82. Stage B boundary-0 amendment frozen before execution (§22.78,
    documentation only): the frozen `ATT-LTE-driving` plan's good epoch-0
    injection (`start_ms = 0`) cannot be executed exactly by the frozen
    harness (`SwitchAt > 0` required; `SwitchAt = 0` disables the switch).
    With no Stage-B run started (0/88), the protocol was prospectively
    amended: boundary-0 injections execute with the fixed 1 ms switch
    instant, start at source C, record `switch_applied = true`, and let the
    target C govern `[0, 120 s)`. No other rule changed; ATT remains
    4 × 8 = 32 and the Stage-B matrix remains 88 runs; no source change, no
    frozen result altered, no run executed. Outcome:
    `BOUNDARY-0 AMENDMENT FROZEN BEFORE EXECUTION — STAGE B READY`.
83. Stage B executed (§22.79, measurement only): the 88 preregistered per-trace
    tau switch-injection runs for the three new traces ran under protocol v2 +
    amendment 1 (2026-09-28 02:18:40 → 04:49:17 UTC; output
    `/home/vscode/final_tau_stage_b/`). 88/88 valid (ATT 32/32, TMobile-LTE
    16/16, UMTS 40/40), 0 technical failures, 0 reruns; the 8 ATT boundary-0
    runs used exactly the 1 ms switch instant with `switch_applied = true` and
    the target C governing `[0, 120 s)`. Raw signed observations preserved;
    per-trace frozen median aggregation with `formal tau = max(0, estimate)`;
    no cross-trace pooling; no oracle/H/CI/gate; frozen artifacts and source
    unchanged; host cleanup verified. Outcome: `STAGE_B_COMPLETE — 88/88
    VALID`. Next: per-trace oracle analysis and the pre-registered five-trace
    LCB gate (Stage C, not started).
84. Stage C final analysis executed (§22.80, analysis only): the frozen
    oracle ran on the five-trace population under oracle protocol v2 +
    final trace protocol v1 + amendment 1 (output
    `/home/vscode/final_oracle_analysis/`). Retained epochs = shared-valid
    full 120 s native-downlink epochs; native final partials (ATT e6,
    TMobile-LTE e3) excluded (no frozen regime label). Five trace-level H
    values: EVDO 0.0164075465, LTE 0.0001943981, ATT 0.0001563742,
    TMobile-LTE 0.0006142775, UMTS 0.0029383433; mean 0.0040621879, sample SD
    0.0069959662, df = 4, t = 2.1318467863, `LCB_95 = −0.0026077024`,
    threshold 0.10 → **FAIL**. Sanity bound holds for all five traces;
    17/17 integrity checks pass; no experiment, no source change, no frozen
    artifact modified. Outcome: `FINAL_ORACLE_ANALYSIS_COMPLETE — FINAL
    STATISTICAL GATE FAIL`.
85. Salvage Protocol v7 frozen (§22.81, documentation only): a separate
    prospective study of chunk-size adaptation under finite-file, tail-aware
    conditions was specified in `testdata/salvage_s_protocol_v7/` (scope,
    mechanism, arms, pilot, endpoint, Bayesian model/priors, best-fixed
    estimand, switching family, trigger variable/semantics, randomization,
    fixed-sequence multiplicity, SSR, Stage 0S rule requirements,
    pilot/confirmatory separation; trace validation out of scope). Status:
    `FINAL_S_PROTOCOL_FROZEN — BLOCKED_PENDING_HUMAN_PARAMETERS` — the
    human-owned parameters `R`, `D`, `FileSize`, `N` and the trigger firing
    rule, `T_tail_primary` definition, Stage 0S go/no-go rule, and SSR scenario
    grid are missing and were **not** invented. No experiment, no pilot, no
    source change; the completed cellular study is untouched.
86. Salvage Protocol v7 parameter-resolution pass (§22.82, documentation
    only): four candidate mechanism packages were evaluated analytically
    (A/C/D admissible, B inadmissible with `T_tail_primary` 16.82 s < 20 s),
    exact proposals were recorded for `T_tail_primary = D + C_max/R`, the
    `B_switch = N × S0` trigger, the Stage 0S go/no-go rule, and the
    18-scenario SSR grid, and the log-scale `delta_T` interpretation
    (`log(0.95) = −0.051293294`) was recommended. The exploratory-switch
    duplicate was removed and the fixed sequence (`16→4`, `4→1`, `1→256K`)
    was frozen (both task-directed). **No candidate or proposal is
    approved**; status remains
    `FINAL_S_PROTOCOL_FROZEN — BLOCKED_PENDING_HUMAN_PARAMETERS`. No
    experiment, pilot, source change, or evidence change.
87. Salvage Protocol v7 final human decision frozen (§22.83, documentation
    only): Candidate A frozen (`R=4 Mbps`, `D=40 ms`, `FileSize=512 MiB`,
    `N=4`); `T_ref=268.435456 s`, `delta_T=13.4217728 s`,
    `delta_log=log(0.95)=−0.05129329438755058`, `T_tail_primary=33.594432 s`,
    `delta_tail=1.6797216 s`; trigger `B_switch=N·S0` = 64/16/4 MiB; no
    exploratory strategy; fixed sequence `16→4`, `4→1`, `1→256K`; Stage 0S
    GO/NO-GO rule and SSR grid (18 × 1000 = 18,000 datasets) frozen;
    confirmatory `n ≤ 12`; pilot data not pooled; trace validation out of
    scope. Validation 19/19 PASS; one transcription difference noted
    (`33.594304` stated vs `33.594432` formula-derived; formula operative).
    Outcome: `FINAL_S_PROTOCOL_FROZEN — READY_FOR_STAGE_0S`. No experiment,
    pilot, source change, or evidence change.
88. Stage 0S pilot executed (§22.84, measurement only): the frozen 32-run
    feasibility pilot ran 2026-09-28 10:19–12:52 UTC under the frozen
    mechanism (R = 500,000 B/s, D = 40 ms, FileSize = 512 MiB, N = 4; four
    fixed arms; randomized within blocks, seed 20260928). 32/32 clean, 0
    technical failures, 0 reruns. All four frozen criteria PASS: mechanism
    presence (+2.38…+2.52 % of D+s/R), strict direction, realized tail median
    34.9776 s within [26.8755, 41.9930] s, reliability (32/32 clean, max
    wall-clock 296.6 s, within-block residual SD 0.000795). Formal decision:
    **GO** — the confirmatory stage is justified under the protocol.
    Artifacts: `testdata/salvage_s_stage0s/`. No source, protocol, or prior
    evidence changed; environment cleaned; no confirmatory analysis run.
    Outcome: `STAGE_0S_COMPLETE — GO`.
89. Pre-Stage-1 scaling/headroom analysis executed (§22.85, analytical
    simulation only): a deterministic simulator of the frozen finite-file
    semantics (two never-blended models; Stage 0S-calibrated Model B validated
    within 0.4 %) over 64 MiB–32 GiB shows that (a) fixed-S normalized costs
    are scale-invariant (`1 + R·D/s`), (b) all frozen switching strategies have
    negative relative gains on the grid (max −0.0007 % / −0.3579 % / −1.7867 %)
    because the frozen trigger fires at a full final burst, (c) the exploratory
    single-switch upper envelope is ≤ 0 on the grid and ≤ +0.3545 % in a dense
    residue scan (≪ 5 %), and (d) relative headroom does not increase with F
    (the bounded tail effect is diluted as 1/F). Outcome:
    `STAGE1_SCALING_ANALYSIS_COMPLETE — ANALYTICALLY INSUFFICIENT UNDER FROZEN
    MECHANISM`. No amendment proposed; no empirical experiment run; protocol
    and prior evidence unchanged.
90. Adaptive-S mechanism pivot screening executed (§22.86, analytical
    simulation only): six candidate mechanisms were screened with a
    deterministic finite-file scheduler simulator under four service models
    and the eight native delivery-opportunity traces. The preferred S does not
    change between stationary states (16 MiB at every rate/regime for the
    shared and lock-in models; fixed 256 KiB for serialized); causal
    within-download switches are zero or harmful (−61.2 % … +0.0 %); the only
    positive opportunity is a fragile, clairvoyant, phase-dependent lock-in
    artifact (0–32 % varying with the volatility period). All candidates are
    `MECHANISTICALLY INSUFFICIENT`; no new preregistered protocol is warranted
    under the screened mechanisms. Zero empirical experiments; frozen v7 and
    all prior artifacts unchanged. Outcome:
    `MECHANISM_PIVOT_SCREENING_COMPLETE — NO PLAUSIBLE MECHANISM`.
91. Adaptive-N mechanism screen executed (§22.87, analytical simulation only):
    eight candidate mechanisms were screened with a deterministic finite-file
    simulator (F = 512 MiB, S = 1 MiB, N ∈ {1,2,4,8,16}) across the
    project's prior concurrency evidence (T1/L3/L4/M3). `direct`/`fair`/
    `fair_bg` are monotone (preferred N = 16); `cap` is a flat plateau with
    zero oracle headroom; only `degrade` (concurrency-dependent server
    capacity) shows a state-dependent interior optimum (preferred N rises
    with C, robust across the whole C × γ grid) with causal gains up to
    +6.66 % at severe drops. It is classified **PLAUSIBLE — FRAGILE** because
    the only live validation of that mechanism class (L4 GATE E) rejected it
    and the current controlled server has no concurrency-dependent cost. No
    mechanism reaches ROBUST, so **no new empirical protocol is justified**
    and no adaptive-N experiment was run. Outcome:
    `ADAPTIVE_N_SCREEN_COMPLETE — NO ROBUST MECHANISM (NO PROTOCOL
    JUSTIFIED)`.
92. Final evidence synthesis / paper-viability audit (§22.88, documentation
    only): the completed evidence was synthesised into six reports (evidence
    matrix, paper-viability audit, figure selection, proposal alignment,
    claims, future-work boundary), seven final figures, and a manifest. The
    final research question was reconstructed (mechanism-first causal
    headroom), the strongest defensible contribution identified (headroom-first
    methodology), the evidence-type discipline enforced (live / replay /
    analytical / simulation / diagnostic), RL deferred (not abandoned), and the
    aria2 evidence bounded to its tested configurations. Outcome:
    `PAPER-READY — MECHANISTIC / NEGATIVE-RESULT STORY`. Zero new experiments;
    all historical evidence and frozen protocols unchanged.

93. aria2-competitive benchmark executed (§22.89, live experimental): a
    memory-safe logical-server replication of the frozen D2 workload reproduced
    the historical result (20/20, ratio 0.5230 CI [0.5200, 0.5261]), explored
    the full aria2 split frontier (1/2/4/8/16 -> 833/419/210/106/53 s; strongest
    split=16) and a 40-config our-frontier grid (best C=16, 4 MiB), froze both
    configurations, and ran a predeclared fresh 30-pair final block: ours mean
    20.12 s vs aria2 53.56 s, geometric ratio 0.3756 CI [0.3728, 0.3785],
    30/30 wins, p ~ 5e-5, all content-verified. Mechanism: 4 MiB ranges (3840
    vs aria2's 450 x 32 MiB) exploit the phase-locked pacing; controls confirm
    granularity-matched ratio 0.862 and a collapsed constant-rate difference.
    Scope bounded to the frozen D2/512 MiB family; this is now the project's
    primary competitive result. Outcome: `ARIA2_COMPETITIVE_WIN_REPRODUCED`.

94. Joint N+S phase-locked pacing screen executed (§22.90, analytical only): a
    simulator calibrated to the measured D2 evidence (40/40 configs within 3 %,
    aria2 anchor −0.8 %) tested whether a causal controller changing both N and
    S can beat the best fixed configuration across a predeclared two-phase
    regime family (640 -> 2500 KiB/s at T in {1,2,4,8} s). Fixed optimum varies
    only in S (N*=16 always; max wrong-config penalty 7.9 %). All primary
    one-switch causal controllers lose (−16 % to −28 %); the diagnostic probe
    peaks at +1.1 %, the oracle-tuned blind ramp at +3.7 %, the optimistic
    causal bound at +3.4 % — no causal class reaches the 5 % threshold in any
    regime. Predeclared gate FAIL (G2) -> STOP: no preregistration, no empirical
    execution. The fixed 62.4 % aria2 benchmark result is unchanged and remains
    the primary competitive result. Outcome:
    `ANALYTICAL_SCREEN_FAILED — NO CAUSAL JOINT HEADROOM`.

95. Live shared-bottleneck / queueing mechanism gate executed (§22.91, LIVE): a
    real `tc tbf` bottleneck on `lo` (40 Mbit/s, 256 kb queue, MTU 1500) shared
    with iperf3 background traffic (B = 0/0.5C/0.85C) was screened with fixed
    configurations (32 MiB, S=1 MiB, N in {1,2,4,8,16}, 3 reps, 45/45 valid).
    Queueing/RTT/drops/retrans grow with N (RTT 0.04 ms -> 33-86 ms median;
    drops 0-860; retrans 0-576), but the completion time has **no finite
    interior N optimum** (flat at S0; boundary N=16 at S1/S2, where more flows
    win a larger TCP share). Predeclared gate FAIL (G-N1, G-N2) -> STOP: no S
    sweep, no adaptive controller, no aria2 comparison, no confirmatory
    protocol. A calibrated work-conserving queueing model (RMSE 8.2 %) predicts
    the same pattern. Outcome: `NO_LIVE-GROUNDED_JOINT_N+S_OPPORTUNITY`. The
    fixed 62.4 % aria2 benchmark remains the primary competitive result.

96. Correlated loss / range-retry mechanism screen executed (§22.92, analytical
    only): a deterministic two-state Gilbert–Elliott loss simulator with the
    frozen retry semantics showed a material **S-retry amplification** in the
    burst-loss condition (S=16 MiB: 32.7 % per-attempt failure, 48 % download
    aborts, 169 MiB re-downloaded per download, +36.3 % penalty for using the
    GOOD-optimal S), but the **N dimension is monotone in both loss states and
    all 9 predeclared robustness variants (N*=16 everywhere)**. The joint
    optimum does not move; the analytical gate fails condition 1 -> STOP: no
    live gate, no adaptive runs, no protocol. Outcome:
    `NO_JOINT_N+S_OPPORTUNITY_UNDER_CORRELATED_LOSS`. The mechanism search is
    closed per the task's final stop condition; the fixed 62.4 % aria2 D2 result
    remains the primary competitive result.

97. Track 1 fresh joint N+S D2 benchmark executed (§22.93, LIVE): a predeclared
    20-block × 4-mode benchmark on the exact frozen D2 workload (80/80 valid,
    zero retries) compared fixed (16, 4 MiB), N-only, S-only and joint adaptive
    N+S (existing heuristics, frozen config) against the frozen aria2
    configuration. Joint adapted both N and S in 20/20 runs (median final
    (6, 6 MiB)) but was 2.32× slower than the best fixed config (ratio 2.3161
    CI [2.3023, 2.3289], 0/20 wins) and 12.4 % faster than frozen aria2 (ratio
    0.8759). Mechanism: per-completed-attempt decision cadence + 4 MiB initial
    chunks delay the first actionable decision to ~8.6 s, and the windowed
    per-request throughput signal saturates after the pacing recovery, so the
    ±10 % rule stops firing and the controllers freeze. Classification
    C = `BEATS_ARIA2_ONLY`; the aria2 margin is not adaptation evidence.
    Outcome: `JOINT_ADAPTIVE_BEATS_ARIA2_BUT_NOT_FIXED`. Track 2 (controlled
    N-dependent server throttling) not started; the fixed 62.4 % aria2 D2
    result remains the benchmark anchor.

98. Track 2 N-dependent server mechanism pre-screen executed (§22.94, analytical
    only): the proposal-promised mechanism was reproduced exactly from
    `python/rl/sim_env_v5.py` and evaluated over a predeclared 16-candidate
    degraded family x N {1,2,4,8,16} x the frozen Track-1 S set at F in
    {64,128,256} MiB. **No candidate qualifies**: the strongest candidate moves
    S* (4->11 MiB) and the N argmin (16->8) with large cross-state penalties,
    but the N movement is degenerate (N=8/N=16 tie at the degraded optimum), the
    S* margin is 1 % (timeout-cliff boundary), and the best single fixed config
    is within 10.7 % of the degraded optimum. Structural cause: the failure-based
    concurrency penalty plus the 3-attempt retry limit is an abort cliff, not a
    smooth interior N optimum. Per the frozen stop rule: no live server
    extension, no live gate, no adaptive run, no aria2 comparison. Outcome:
    `PRE_SCREEN_FAILED_NO_QUALIFYING_SCENARIO`.

103. W1-W6 six-workload study completed (§22.100 live grid + §22.101
     adaptive, LIVE): 240 paired runs, 0 invalid. The frozen band selector
     matches the per-workload clairvoyant best fixed on average (geo 0.999
     [0.969, 1.029]) and beats the best single fixed configuration across the
     family single config (geo 0.770; six-cluster CI [0.455, 1.304]; see §22.101); aria2 still wins
     the degraded states via its cap escape. One model-vs-live mismatch (W5,
     retry/k feedback) disclosed; the frozen mapping was not retuned. Outcome:
     `SIX_WORKLOAD_SELECTION_DEMONSTRATED`.

102. W1-W6 six-workload family designed analytically and frozen before any
     live run (§22.100): 13/72 candidates qualify (all t in {4,8}); W2-W6
     selected with model optima (2,4 MiB)/(4,4 MiB) and base-side penalties
     231-579 %; band selector frozen (25 s classification window, levels
     105/210/315 KiB/s). Live fixed grids + adaptive comparisons not started.

101. P1 baseline terminology corrected from raw data and P2 S-only ablation
     executed (§22.99, LIVE): joint vs N-only 10/10 at +5.3 %, S-only alone
     worse than no switch (82.1 s vs 76.7 s); both P1 baselines reported
     explicitly; timeout sensitivity + capped-S precision replication in
     flight.

100. Track 2-B adaptive experiments executed (§22.98, LIVE): the soft-cap
     platform produced two positive adaptive results. P1 (cross-workload joint
     selection): the causal selector beats the best single fixed configuration
     on both workloads (mean +27.9 %, worst +8.2 %), matches the best fixed in
     the healthy state and beats aria2 2.8x there, and beats the healthy-tuned
     fixed by 27 % in the capped state (aria2 wins there via its burst-start cap
     escape). P2 (within-download): the causal joint controller switches to
     (4,4M) when the server degrades and beats the best fixed by 23.4 %
     (geo 0.766, 10/10 wins), with the joint target better than the N-only
     control. Outcome: `JOINT_ADAPTATION_DEMONSTRATED_UNDER_CONTROLLED_SERVER_DEGRADATION`.

99. Exploratory combined-mechanism screen executed (§22.95, analytical,
    EXPLORATORY): a **soft** N-dependent server throttle
    (`rate_eff = min(schedule, C/(k(1+γ(k−1))))`, no hard failures) combined
    with the phase-locked pacing schedule produced the project's first clean
    state-dependent **joint** (N*,S*) movement: T=1 s / C=8 MiB/s / γ=0.05 gives
    no-cap (16, 1 MiB) 18.42 s vs capped (4, 4 MiB) 85.51 s with cross-state
    penalties +35.8 % / +252.9 % and coordinate margins +6.4 % / +5.9 %
    (1 of 27 screened pairs passes the strict rule). This revives the planned
    "B" controlled N-dependent scenario (frozen pre-screen, live server rate-cap
    extension, live gate, causal controller next). No frozen artifact touched.

---

