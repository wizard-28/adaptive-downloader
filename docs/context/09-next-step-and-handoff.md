## 27. Current next step

> **Current (2026-10-25):** see §22.109 for the live next step. The text
> below this note is the historical LinUCB-era record and is kept unchanged.

LinUCB (the first learner) and the first `RewardCalculator` are implemented and
unit-tested behind `rl.Policy` and `policyRunner` respectively (§20.6, §20.7,
§21, §22). The architecture is unchanged:

```
Downloader
  -> existing Policy interface
  -> RL Adapter
  -> LinUCB
  -> ActionIndex
  -> existing policy.Action
  -> existing policyRunner (computes reward, correct credit assignment)
  -> existing controllers
```

LinUCB is integrated as the **opt-in fifth experiment mode** (§16.1), the full
causal loop is validated by deterministic integration tests, a diagnostic
calibration workflow exists (§22.1–§22.5), **Formal Benchmark v1** (pre-correction)
is frozen (§22.6), the **LinUCB Exploration Diagnostic** (§22.7) explained the
`ActionNone=100 %` result, the **LinUCB Exploration Correction** (§22.8) is
implemented, **Formal Benchmark v2** and the **learning-horizon study**
(§22.9–§22.10) are complete, **Formal Benchmark v3** at 4 MiB has been run
(§22.11), the **post-warm-up lock-in has been diagnosed** (§22.12), the
**periodic exploration correction** is implemented and evaluated (§22.13), the
**live standard-vs-periodic benchmark** is complete (§22.14), the **final
six-mode 4 MiB benchmark** has been run (§22.15), an **aria2c external baseline**
has been measured (§22.16), the **request-granularity sensitivity study**
(§22.17) is complete, the **wall-clock throttle benchmark** (§22.18) has
been run, and the **aria2 wall-clock + granularity-matched controls** (§22.19–
§22.20) are complete. The architecture is unchanged.

Final benchmark outcome (§22.15): 300/300 valid. Placed beside the four
deterministic controllers, periodic LinUCB increases action coverage and changes
controller state but does not change the dominant arm preference in most
scenarios; performance differences are descriptive only. Across v3–v5, standard
LinUCB locks onto one arm, and deterministic periodic exploration moves the
posterior without generally changing the preference. **No winner or superiority
has been established.** The aria2 baseline (§22.16) measured 4 concurrent
connections with fixed 1 MiB splits and no within-run adaptation; the
granularity study (§22.17) showed a matched internal 1 MiB/4C config reproduces
aria2 closely in 4/5 scenarios (0.85–0.97×), substantially reducing — but not
eliminating — the request-granularity confound (latency retains ~2.3× unexplained
aria2 overhead). The wall-clock throttle benchmark (§22.18) removed request count
as the rate trigger (transition at ≈1.04 s elapsed in every mode) and showed
that **periodic LinUCB now prefers `increase-concurrency` and reaches
concurrency 3.90**, matching concurrency-adaptive, while standard LinUCB still
locks onto a no-op; the preference change is environment/schedule-dependent.

The current RL evaluation is **complete for the specified controller**, the
external aria2 baseline is complete, the granularity confound is largely
characterized, and the wall-clock throttle study is complete. The genuinely
remaining items are separate research questions, not tuning:

- **Preference lock-in design study:** the live/six-mode/request-count benchmarks
  show coverage and posterior movement are insufficient to change which arm
  dominates in most scenarios, but the wall-clock study (§22.18) shows the
  preference is environment/schedule-dependent (periodic LinUCB chose
  `increase-concurrency` there). Any mechanism change must be a falsifiable,
  versioned experiment (own dataset; all frozen datasets preserved) and must not
  tune alpha/reward/features to a desired outcome.
- **Wall-clock throttle follow-ups:** applying the wall-clock scenario to the
  other four impairments, or a longer/multi-transition schedule, are separate,
  explicitly-scoped experiments (not run here). The aria2 wall-clock baseline is
  now done (§22.19).
- **Chunk-size tradeoff modelling:** the harness models no meaningful chunk-size
  performance tradeoff, so chunk adaptation cannot be evaluated until one is
  added (a deliberate environment change with its own justification).
- **Credit assignment under concurrency:** one-step reward attribution remains
  approximate; redesigning it is a separate task.
- **Fairness:** no measurable fairness signal exists yet.
- **IDM baseline:** requires a separate Windows measurement; no Linux path.
- **aria2 latency overhead:** the residual ~2.3× latency gap is unexplained by
  granularity/concurrency and would need a separate, more invasive trace.
- **Wall-clock throttle benchmark:** the experiment server now supports a
  wall-clock rate schedule (§14.1); §22.18 ran it, and §22.19–§22.20 added the
  aria2 and granularity-matched controls. Still open (not run): applying the
  wall-clock schedule to the other four impairments.

**Status: the internal experimental campaign is complete and the evidence chain
has been synthesized and audited (§22.21).** The **sequential RL phase has
begun** (§22.22): a Double DQN controller/training architecture is implemented and
sanity-validated. LinUCB remains the retained contextual-bandit baseline. **No
performance claim** is made for the DQN; the stored model is a development/sanity
model, the training simulator is not the final test, and no aria2 comparison has
been run.

The **final head-to-head has been completed (§22.26) and the outperformance claim
was NOT supported**: under the locked 8-condition unstable suite, the frozen DQN
Candidate v2 was ~75.7 % slower than the frozen aria2 baseline (geometric ratio
1.757, 95 % CI [1.669, 1.850]), faster in only 1/8 conditions and 31/240 pairs.
All four pre-specified criteria failed. The experiment and conditions were not
altered to rescue the claim.

**Status:** the empirical campaign is complete for its original framing. The
supported write-up scope is now: the controlled synthetic-server evaluation of
§22.21, the DQN architecture/validation of §22.22–§22.24, the fairly-chosen
aria2 baseline of §22.25, and the **negative** final result of §22.26. The
outperformance claim must **not** appear in any write-up.

The **v3 request-efficiency correction (§22.27)** addressed the fragmentation
cause but produced a **mixed** result: it fixed request efficiency yet regressed
the concurrency adaptation that v2 had, because the v3 simulator over-favoured
large chunks. Candidate v3 is therefore an **experimental** formulation, not a
validated candidate.

The **v4 adaptive-concurrency formulation (§22.28)** is the clean final RL
formulation: chunk pinned at 1 MiB, concurrency-only learned control, and a live
transfer that matches aria2's request granularity (4 × 1 MiB) while holding
concurrency 3. Candidate v4 is the candidate intended for a future, explicitly
scoped head-to-head.

The **next genuinely remaining work** (not started; explicitly scoped) is the
**final head-to-head of frozen Candidate v4 against the frozen aria2 baseline**,
run with the same rigor as §22.26 (locked matrix, paired design, pre-specified
criteria, no post-hoc changes), with the v4 model evaluated at **1 MiB chunks**
so the remaining algorithmic difference is **learned adaptive concurrency vs
aria2's fixed concurrency**. It must (a) use fresh, clearly-versioned datasets,
(b) keep all frozen artifacts and prior candidates/results, (c) not tune v4 or
aria2 after seeing results, and (d) report honestly whether the pre-specified
outperformance criteria are met (the §22.26 negative result stands as history).
Real-network, chunk-size-tradeoff, learned-exploration, fairness, and IDM work
remain separate projects. **No outperformance claim against aria2 is supported by
any current evidence.**

**Update (current experimental state):** the frozen external baseline is now
**FINAL ARIA2 BASELINE CONFIG v2** (candidate F: 16 connections / split 16 /
1 MiB minimum split; `testdata/diagnostics/aria2_calibration_v2/`, SHA-256
`b7249817…`), selected on a 16 MiB evaluation scale; config v1 is retained as
history. The active experimental formulation is **DQN Candidate v5**
(concurrency-only, state spec version 2, 16 MiB default horizon, fixed 1 MiB
chunk, decision interval candidates {1,2,4}); it is **not frozen**, no v5 model
artifact exists, and no v5 training/validation/live-transfer/final comparison
has been run. The v5 simulator/state/live contract was audited and corrected
(§22.29), and the v5 training/validation pipeline has now been run (§22.30): one
experimental Candidate v5 model exists (SHA-256 `3a927a58…`, reproducible) and
passes the pre-specified constant/ramp NOT-READY gate, but it is generally
slower than the best fixed concurrency on the held-out simulator conditions. The
v4 head-to-head described above has **not** been run. The **live transfer** of
Candidate v5 on the real controlled downloader (16 MiB, 1 MiB fixed chunk) has
now been completed (§22.31): 125/125 valid, pre-specified gates pass, v5 tracks
fixed-C3 but is slower than fixed-C4 and the observed adaptation is shallow.
The **final head-to-head of Candidate v5 against the frozen aria2 v2 baseline
has now been completed (§22.32)**: under the reused methodology and the
mandatory 4-vs-16 concurrency-asymmetry caveat, v5 was slower in all 8 tested
conditions (pooled geometric ratio 2.3217, 95 % CI [2.2269, 2.4206]; v5 faster
in 1/240 pairs and 0/8 conditions). No outperformance claim is supported. The
empirical campaign for this framing is complete; remaining items (real-network
deployment, chunk-size tradeoff, learned exploration, fairness, IDM) are
separate projects.

**Candidate v6 update (§22.33–§22.34):** a new experimental study with
concurrency 1–16 under a shared time-varying bottleneck has been pre-registered,
trained, held-out validated, and live-validated. All four pre-registered gates
(A environment, B training, C condition-dependence, D live transfer) pass; v6's
behavior is genuinely phase-responsive (it lowers/moderates concurrency under
shared-capacity degradation and holds high concurrency under server throttling).
However, on the live controlled server v6 was slower than the best fixed
concurrency in every tested trajectory, because the live server's independent
per-response pacing still rewards high concurrency. The pre-registered frozen
aria2-F comparison is the next stage; it has NOT been run and no superiority
claim is supported.

**Final v6-vs-aria2-F update (§22.35):** the pre-registered comparison has now
been run — 150 matched pairs, 300/300 valid. v6 was slower pooled (geometric
ratio 1.456, 95 % CI [1.300, 1.630]) and on A2/B2/C2/D2; it was ~5.2 % faster on
E2 (Holm p = 0.128, not significant). The stated RL-under-instability hypothesis
is **not supported** by this controlled experiment. No further training or
design iteration is performed in this session; any next experiment must be
separately designed and documented.

**Diagnostic update (§22.36–§22.37):** the long-duration diagnostic showed
startup amortization but a growing disadvantage on A2/E2; the follow-up
replication reproduced the D2/512 MiB reversal (20/20 pairs, ratio 0.5816) and
explained it as a request-granularity × phase-locked-pacing interaction, not
generic adaptive superiority. A new 7-phase long multi-transition trajectory
(L1) has different per-phase preferred concurrency ({1,2,3,5,16}) but only a
~4 % full-run completion-time spread at 512 MiB, so a v7 design is not yet
justified by this environment; the next step would be to strengthen the
long-duration environment (larger per-phase concurrency sensitivity) before any
v7 training.

**L2 environment-design update (§22.38–§22.39):** the stronger L2 design was
pre-registered, swept, and adjudicated: per-phase optima differ substantially
(best C [16,2,4,16,2,6,16]; C16 up to 62 % worse in degraded phases), but the
**achievable** full-run headroom under the frozen ±1/one-decision-per-request
action semantics is ≈0 (−0.3 % ramp-aware oracle vs best static C16). In
addition, the installed aria2 1.37.0 cannot provide ~1 MiB ranges with ≤16
connections at 512 MiB, so a granularity-matched long-duration aria2 baseline
cannot currently be constructed. Gates: **NOT_READY_ENVIRONMENT_TOO_WEAK** and
**NOT_READY_ARIA2_GRANULARITY**. No v7 training is justified; any next step must
first strengthen the byte-weighted concurrency tradeoff (or change control
authority as a deliberate v7 formulation decision) and resolve the
granularity-matched baseline question.

**v7 design update (§22.40–§22.41):** changing the action space to ±4 steps
(selected from the A/B/C candidates) roughly doubled achievable headroom versus
L2 (≈0 → 8.1 %) but still missed the pre-registered ≥10 % threshold on the new
byte-weighted L3 trajectory, so the pre-registered gate outcome is **STOP**:
v7 training is not justified until a future, separately pre-registered
environment creates a larger byte-weighted concurrency penalty. The runner's
additive step semantics (Value as step, Value ≤ 0 unchanged) and its contract
test are retained; no v7 model exists and no v7-vs-aria2 comparison was run.

**L4 final update (§22.42–§22.43):** the byte-weighted L4 environment reached
17.6 % action-constrained simulated headroom (≥10 % gate), but a small
representative live fixed-C sweep showed that the decisive degraded regime does
**not** penalize excessive concurrency live (C16 ≈ C8 > C4), while the
high-capacity and recovery regimes did match. The pre-registered outcome is
**`NOT_READY_LIVE_MISMATCH`**; the simulated opportunity rests on a
simulator-only mechanism and the byte-weighting rule (≥50 %) was narrowly
missed (47.1 %). **Another RL training iteration is not justified** until a
live-reproducible, physically honest mechanism makes excessive concurrency
genuinely expensive. All frozen candidates, datasets, and findings are
preserved.

**T1 update (§22.44–§22.45):** the live per-request server-throttle mechanism
was pre-registered and measured. Pacing persistence is real live (GATE A), but
with equal 1 MiB requests and independent per-response pacing there is no
concurrency reversal: C4–C16 finish before the throttle at normal 8 MiB/s and
aggregate throughput scales monotonically with C, so the D2 benefit is confirmed
to be a request-granularity effect rather than an adaptive-concurrency
opportunity. Outcome `NOT_READY`; no RL training is justified by the current
live environment. Any future stage must find a live-reproducible cost of
concurrency or explicitly reframe the research question.

**T2 update (§22.46–§22.47):** adaptive **range size** was tested live as the
alternative control dimension at fixed C=16. The 16 MiB chunk was fastest
(184.2 s vs 245.7–267.0 s), because requests started in fast phases retain the
fast rate across the throttles and bypass them; smaller chunks keep sampling
the slow rate. There is no small-vs-large preference reversal, so adaptive
range size has no exploitable headroom here (outcome
`NOT_READY_NO_CHUNK_TRADEOFF`). No RL training is justified by either the
concurrency or range-size direction in the current live environment; the D2
512 MiB result remains a request-granularity artifact.

**M1 update (§22.48):** the real-mobile-trace stage stopped at its
data-acquisition prerequisite: no real mobile/cellular trace exists in the
repository, and synthesizing one is forbidden, so the outcome is
`NOT_READY_NO_REAL_MOBILE_TRACE`. Real public sources are reachable, so the
next viable step is a dedicated stage to acquire ≥5 documented real mobility
traces, canonicalize them, build and validate a trace replayer, and only then
repeat the pre-registered fixed-C/temporal-C* analysis. No RL training and no
aria2 comparison are justified before that chain is established.

**Early-replay corrective update (§22.63, §22.64):** the §22.62 root cause has
been fixed in code and tests. The child now exits non-zero on fatal downloader
errors and persists `fatal`/`err`; `SUCCESS_TRACE_REACHED_EOF` requires a
normal mm-link exit, no fatal child error, **and** a downlink-log endpoint
within the frozen 100 ms `AlignmentTolerance` of the `.down` trace duration
(new outcomes `DOWNLOADER_FATAL_ERROR`, `TRACE_ENDPOINT_NOT_REACHED`); the
pilot resolves `<family>.up`/`<family>.down` pairs and rejects
missing/mismatched pairs; the pilot request timeout is explicit `-timeout`
(default 120 s) with the 45 m watchdog unchanged.

**The two native diagnostics ran and passed (§22.64, 2026-09-26):** EVDO C=1
(17 m 45 s) and LTE C=16 (22 m 47 s) both reached `SUCCESS_TRACE_REACHED_EOF`
with `endpoint_valid=true`, `mm_link_exit=0`, child status `canceled`, no fatal
error, endpoint residual −35 ms, and complete downlink delivery. Outcome:
`NATIVE_DIAGNOSTICS_PASS — READY FOR CORRECTED PILOT`.

**The corrected formal pilot then ran and is INVALID (§22.65, 2026-09-26):**
the 10-run matrix was launched in `/home/vscode/epoch_pilot_corrected`. EVDO
C=1 and C=2 completed valid trace EOF; EVDO C=4, C=8 and C=16 died early with
fatal `retry limit exceeded` after 2 attempts (trace times 512.3 s / 240.1 s /
150.0 s of 1 062.0 s). The gate correctly rejected them
(`DOWNLOADER_FATAL_ERROR`, `endpoint_valid=false`), the pilot was stopped per
the pre-registered stop rule, and the remaining LTE runs were not completed.
No epoch extraction, shared validity, coverage, duration selection, oracle,
tau, or headroom was computed. Outcome: `BLOCKED — FORMAL EPOCH PILOT INVALID`.
`EPOCH_DURATION_BLOCKED` still stands; no duration is frozen.

**Mechanism diagnosed (§22.66, 2026-09-26):** the EVDO early deaths are an
unintended **C-dependent censoring** effect of the formal 120 s per-request
timeout under the shared low-rate link. Frozen artifacts show the C8 death at
exactly 2×120 s and the C16 death at 120 s + the fixed 30 s transport dial
timeout; a controlled 120 s vs 600 s sweep on EVDO C=4/8/16 showed C4 and C8
reaching genuine trace EOF at 600 s (4 and 24 completed requests had needed
>120 s), while C16 at 600 s still died (600 s request timeout at ≈940 s
followed by a 30 s retry dial timeout at ≈971 s). Status:
`TIMEOUT_CENSORING_SUPPORTED`. The formal 120 s timeout is therefore **not
safe for the fixed-C EVDO measurement**; the pilot revision rule only permits
candidate-list and byte-floor changes, so this is outside its scope.

**Timeout policy implemented (§22.67–§22.68):** the formal replacement is the
**watchdog-only** policy — no HTTP request deadline and no connection dial
deadline, with the 45 m parent and child watchdogs as the only time bounds
(trace EOF remains the measurement endpoint) and retry semantics unchanged.
It is implemented as protocol version 2 (`epoch.DefaultProtocol()`), explicit
`NoRequestTimeout`/`NoDialTimeout` configuration in `internal/fetcher`/
`internal/downloader`, and pilot `-timeout`/`-dial-timeout` flags defaulting to
`none`; the historical version-1 120 s/30 s policy remains representable via
`epoch.LegacyProtocolV1()` and the diagnostic CLI overrides. Synthetic tests
and the full validation suite pass, and a bounded EVDO C=1 smoke with no
deadlines ended in `WATCHDOG_TIMEOUT` with clean teardown. No full pilot has
been run yet.

**Formal v2 pilot completed (§22.69, 2026-09-27):** the fresh 10-run
watchdog-only matrix in `/home/vscode/epoch_pilot_v2` finished with all 10
runs valid trace EOF (residuals −35…−58 ms, no fatal error, no workload
exhaustion, `-timeout none -dial-timeout none` confirmed per run). Epoch
extraction and shared validity completed; only the **120 s** candidate passed
the frozen coverage rule (aggregate survival 85.7 %, min-trace 7 shared-valid
epochs) and was selected (`2m0s`); 30/60/90 s are reported unsuitable under
the existing thresholds and were not tuned.

**Oracle stage blocked (§22.70, 2026-09-27):** the frozen v2 input was
verified (protocol v2, selected 120 s, 21 candidate / 18 shared-valid / 3
excluded epochs; EVDO 7, LTE 11) and preserved under
`/home/vscode/oracle_stage_v2/`, but formal headroom could **not** be
computed: the repository has no pre-registered tau regime classifier/
threshold, no tau (transition-cost) measurement design or frozen measurement,
and no completion-time/workload mapping — exactly the prerequisites §22.52
records as not frozen. Outcome: `BLOCKED — ORACLE PREREQUISITE UNRESOLVED`
(specific gate: `BLOCKED — TAU REGIME RULE NOT PRE-REGISTERED`).

**Tau pre-registered and measured (§22.71, 2026-09-27):** the oracle
objective, native-regime classifier, 2.0 regime threshold, injection selection,
and tau aggregation are frozen in `testdata/oracle_preregistration_v2/`. The
72-run switch-injection campaign completed 72/72 valid; all eight directions
have aggregated tau (only 1→2 regime-specific; pooled |tau| ≤ 0.57 s
elsewhere). No headroom was computed.

**Formal oracle analysis executed (§22.72, 2026-09-27):** the frozen DP
computed fixed-C completion equivalents, the instantaneous diagnostic, and the
constrained oracle with persisted sequences. Descriptive two-trace headroom:
EVDO 1.61 %, LTE 0.44 % (nonnegative-tau sensitivity: 1.58 % / 0.047 %;
zero-tau: 1.64 % / 0.17 %). The task's pre-registered sanity bound
`T_instantaneous <= T_constrained` fails for LTE because the frozen tau rule
preserves negative measured taus; the DP was verified against exhaustive
enumeration and no value was clamped. Status:
`BLOCKED — ORACLE COMPUTATION INCONSISTENT`.

**Nonnegative formal tau ready (§22.73, 2026-09-27):** oracle protocol
version 2 (`testdata/oracle_preregistration_v3/`) defines
`tau_formal = max(0, estimator)` after the unchanged median/regime rule, with
raw signed observations preserved. The fresh v3 campaign
(`/home/vscode/tau_campaign_v3/`, same design/timestamps, plan hashes matching
v2) completed 72/72 valid; formal tau is ≥ 0 for all eight directions
(2→1 and 8→4 floored; no regime-specific directions in the fresh data).
Headroom was not recomputed.

**Formal v3 oracle complete (§22.74, 2026-09-27):** with the nonnegative
formal tau table, all sanity inequalities hold and the descriptive two-trace
headroom is EVDO 1.6075 %, LTE 0.0320 % (historical signed-tau reproduction:
1.6072 % / 0.4436 %; zero-tau diagnostic: 1.6408 % / 0.1740 %). Artifacts are
byte-identical across two runs.

**Trace-expansion gate audited (§22.75, 2026-09-27):** the repository does
**not** freeze the final ≥5-trace protocol — the count/threshold exist only as
an aspiration in analysis output/docs, and diversity, admissibility, trace
identities, tau scope, reuse rules, and the statistical formula were UNFROZEN at
audit time; they are now frozen in §22.76 (`testdata/oracle_preregistration_v4/`).
Status at audit time: `BLOCKED — FINAL TRACE PROTOCOL INCOMPLETE` (superseded by
the frozen protocol below); audit artifacts under
`/home/vscode/final_trace_expansion_decision/`.

**Final trace protocol frozen (§22.76, 2026-09-27):** the prospective
pre-registration `testdata/oracle_preregistration_v4/final_trace_protocol.json`
freezes the five-trace population, eligibility, existing replay/oracle rules,
**per-trace tau**, reuse rules, the statistical design (mean H, sample SD,
one-sided 95 % t LCB, df = n−1, pass iff `LCB_95 >= 0.10`), and invalid/
missing handling. Planning artifacts are consistent with the freeze. Status:
`FINAL_TRACE_PROTOCOL_FROZEN — READY FOR FINAL TRACE EXECUTION`.

**Immediate next step:** the frozen five-trace campaign is complete. Stage A
(15/15 fixed-C replays, §22.77), Stage B (88/88 per-trace tau runs, §22.79),
and Stage C (frozen oracle + final gate, §22.80) have all run under the
amended preregistration. The pre-registered gate **FAILED**
(`LCB_95 = −0.0026077 < 0.10`); the five trace-level H values and all
artifacts are in `/home/vscode/final_oracle_analysis/`. No further
experimental or analysis work is planned for this campaign. Any write-up must
use the supported scope (controlled synthetic-server evaluation, DQN
architecture/validation, fairly-chosen aria2 baseline, negative final result)
and must not claim RL/adaptive/aria2 superiority or causal real-world
improvement; remaining items (real-network deployment, chunk-size tradeoff,
learned exploration, fairness, IDM) are separate projects.

**Salvage study (separate, frozen; Stage 0S complete):** the chunk-size-tradeoff
salvage study is specified in `testdata/salvage_s_protocol_v7/`
(§22.81–§22.83) and is now `FINAL_S_PROTOCOL_FROZEN — READY_FOR_STAGE_0S`.
The human froze Candidate A (`R=4 Mbps`, `D=40 ms`, `FileSize=512 MiB`,
`N=4`), the log-scale `delta_log = log(0.95) = −0.05129329438755058`, the
tail definition `T_tail_primary = D + C_max/R = 33.594432 s` (with a noted
0.128 ms transcription difference from the decision text), the trigger
`B_switch = N·S0` = 64/16/4 MiB, the Stage 0S GO/NO-GO rule, and the SSR grid;
the exploratory duplicate was removed and the fixed sequence frozen. **Stage
0S is now complete (§22.84): 32/32 clean and all four criteria PASS → GO.**
The next step is the confirmatory Stage 1 (fresh data; frozen arms,
strategies, trigger, priors, effect threshold, and analysis) as a **separate
task** under the frozen protocol; it has not been started. A GO does not mean
chunk-size adaptation is beneficial — it only justifies the next pre-specified
stage. However, the pre-Stage-1 scaling/headroom analysis (§22.85) now shows
that the frozen design is **analytically insufficient under the frozen
mechanism** at 512 MiB: the frozen strategies' relative gains are negative on
the whole 64 MiB–32 GiB grid (the trigger fires at a full final burst) and even
an exploratory optimally-timed single switch stays ≤ +0.3545 % (≪ 5 %), with
relative headroom not increasing with file size. **No amendment is proposed**
(larger files do not help); proceeding to Stage 1 as frozen would require an
explicit human decision, and any design/mechanism change requires a separately
frozen protocol decision. The follow-up **mechanism pivot screening (§22.86)**
then tested whether a different realistic mechanism could create a moving
chunk-size preference: the preferred S does not change between stationary
states for any screened candidate, causal within-download switches are zero or
harmful, and the only positive opportunity is a fragile, clairvoyant,
phase-dependent lock-in artifact. **All candidates are
`MECHANISTICALLY INSUFFICIENT` and no new preregistered protocol is warranted
under the screened mechanisms**; the frozen v7 protocol remains unchanged and
no Stage 1 run was performed. The follow-up **adaptive-N mechanism screen
(§22.87)** then asked the same question for concurrency: `direct`/`fair`/
`fair_bg` are monotone (preferred N = 16), `cap` is a flat plateau, and only
`degrade` (concurrency-dependent server capacity) shows a state-dependent
interior optimum with causal gains up to +6.66 % — but that mechanism class
was already rejected live (L4 GATE E), so it is **PLAUSIBLE — FRAGILE**. No
mechanism reaches ROBUST and **no adaptive-N protocol is justified** in the
current infrastructure; no adaptive-N experiment was run. The final
**evidence synthesis (§22.88)** consolidated all of the above into the
paper-ready mechanistic/negative-result story: the contribution is a
pre-registered, headroom-first methodology, RL is deferred (not abandoned),
the aria2 evidence is bounded to its tested configurations, and no further
empirical work is required for the negative-result paper. Status:
`PAPER-READY — MECHANISTIC / NEGATIVE-RESULT STORY`. The follow-up
**aria2-competitive benchmark (§22.89)** then asked the bounded positive
question the synthesis left open — can a fixed configuration reproducibly beat
aria2 under the frozen D2 workload? — and answered it: the full aria2 split
frontier (strongest split=16) and a 40-config our-frontier grid (best C=16,
4 MiB) were explored, both configurations frozen before a predeclared fresh
30-pair block, and the final comparison is 30/30 wins at geometric ratio 0.3756
CI [0.3728, 0.3785] (ours 20.12 s vs aria2 53.56 s mean), content-verified. The
mechanism is a request-granularity x phase-locked-pacing interaction (3840 x
4 MiB ranges vs 450 x 32 MiB pieces all held at the 640 KiB/s phase); controls
(granularity-matched ratio 0.862; constant-rate collapse of the gap) bound it.
This is now the project's **primary competitive result**, scoped strictly to
the frozen D2/512 MiB family. Status: `ARIA2_COMPETITIVE_WIN_REPRODUCED`. The
follow-up **joint N+S phase-locked pacing screen (§22.90)** asked whether that
mechanism can be turned into a genuine within-download adaptation result: a
calibrated deterministic simulator (D2 fit within 3 %) evaluated a predeclared
two-phase regime family, the frozen 8-action space, and a frozen (4,4M) start.
The fixed optimum varies only in S (N*=16 always, max wrong-config penalty
7.9 %); every primary causal controller loses (−16 % to −28 %), and the
strongest diagnostic causal variants (probe, oracle-tuned blind ramp,
optimistic causal bound) peak at +1.1 / +3.7 / +3.4 % — below the 5 %
materiality threshold in every regime. The predeclared gate FAILED (no
positive causal headroom) → STOP: no preregistration and **no empirical
execution** (the frozen stop condition). Conclusion: no robust joint adaptation
opportunity under this mechanism; the fixed 62.4 % aria2 result is unchanged
and remains the primary competitive result. Status:
`ANALYTICAL_SCREEN_FAILED — NO CAUSAL JOINT HEADROOM`. The final mechanism
branch, the **live shared-bottleneck/queueing gate (§22.91)**, was then tested
live (real `tc tbf` on `lo` + iperf3 background): the contention cost is
measurable and grows with N (RTT 0.04 ms -> 33-86 ms median, drops 0-860,
retrans 0-576), but the completion time is work-conserving: no finite interior
N optimum (flat at B=0; boundary N=16 under background, where more flows win a
larger TCP share) and no material state-dependent N*. The predeclared gate
FAILED -> STOP: no S sweep, no adaptive controller, no aria2 comparison, no
confirmatory protocol. Status: `NO_LIVE-GROUNDED_JOINT_N+S_OPPORTUNITY`. The
final mechanism branch — **correlated loss + range-retry amplification
(§22.92)** — was then screened analytically (two-state Gilbert–Elliott loss,
frozen retry semantics, grounded parameters + predeclared robustness sweep):
the S-retry amplification is real and material under burst loss (S=16 MiB
fragile: 32.7 % per-attempt failure, 48 % download aborts, +36.3 % penalty),
but the N dimension is monotone in both loss states and all 9 variants
(N*=16 everywhere), so the joint optimum does not move. The analytical gate
failed condition 1 -> STOP: no live gate, no adaptive execution, no protocol.
Status: `NO_JOINT_N+S_OPPORTUNITY_UNDER_CORRELATED_LOSS`. Per the task's final
stop condition the mechanism search is **closed**; the fixed 62.4 % aria2
benchmark remains the project's primary competitive result and is unchanged.
The subsequent **Track 1 fresh joint N+S D2 benchmark (§22.93)** then tested
whether the *existing* adaptive policies add value on the already-demonstrated
D2 mechanism: 20 blocks × {fixed, N-only, S-only, joint}, 80/80 valid. The
joint policy adapted both N and S in 20/20 runs but was 2.32× slower than the
best fixed (16, 4 MiB) and 12.4 % faster than the frozen aria2 reference; the
mechanism analysis shows the per-completed-attempt decision cadence and the
saturating per-request throughput signal freeze the controllers at (6, 6 MiB).
Classification C = `BEATS_ARIA2_ONLY`; the adaptation-value test failed and the
aria2 margin is not adaptation evidence. The subsequent **Track 2 pre-screen
(§22.94)** then reproduced the proposal-promised N-dependent server throttling
exactly from `python/rl/sim_env_v5.py` and evaluated the predeclared
16-candidate degraded family over the full grid at three workload scales:
**no candidate qualifies** — the strongest moves S* to the timeout-cliff
boundary and the N argmin only degenerately (N=8/N=16 tie), because the
failure-based concurrency penalty plus the 3-attempt retry limit is an abort
cliff rather than a smooth interior N optimum. Per the frozen stop rule no live
server extension, live gate, adaptive run, or aria2 comparison was performed.
Status: `PRE_SCREEN_FAILED_NO_QUALIFYING_SCENARIO`. The fixed 62.4 % aria2 D2
benchmark remains the project's primary competitive result.
The **Track 2-B soft-cap platform (§22.95-§22.98)** then produced the project's
first clean joint-adaptation result under a live N-dependent server mechanism:
the frozen live gate passed on N*/S* movement and cross-state materiality
(+31.8 % / +254.4 %) with the documented capped S-margin caveat (3.8 % < 5 %),
and the causal joint controller reduced completion time 23.4 % vs the best
stationary fixed configuration (geo 0.766, 10/10), while aria2 wins in the
capped state via its burst-start cap escape (the honest reversal). Per the
remaining-month plan (2026-10-08), P1 is now reported with both baselines
explicitly (§22.99), the S-only ablation confirms the joint target (N carries
the gain; S adds a consistent 5.3 %; S-only alone hurts), and a six-workload
family was designed analytically and frozen as
`manifest/w1_w6_manifest.json` (§22.100) **before any live W1-W6 run**. The
six-workload study is now complete: the live grid validated 5/6 model optima
(the W5 mismatch is disclosed, not retuned) and the frozen band selector
matches the per-workload clairvoyant best on average (geo 0.999 [0.969, 1.029])
while the pooled ratio vs the family single fixed configuration is 0.770
(six-cluster CI [0.455, 1.304]; not robust across workloads, see §22.101) — a **(B)** workload-selection result over
**six predeclared controlled workloads** (§22.100-§22.101,
`reports/w1_w6_report.md`, figures `figW1..figW4`). The timeout sensitivity
((4,4M) invariant at 20/30/60 s) and the capped-S precision (margin 3.74 %,
CI [3.09, 4.45], criterion unchanged) are folded into
`reports/track2b_soft_cap_report.md`. The remaining work is the manuscript
(IEEE Access primary; mechanism-dependent-granularity thesis): update
`reports/paper_draft.md` with the six-workload section (done), harmonize
figures, and refresh `docs/team_status_2026-09-29.md`. mm-link trace anchoring
remains low priority unless time permits.
Historical artifacts (`/home/vscode/epoch_pilot`,
`/home/vscode/epoch_pilot_corrected`, `/home/vscode/evdo_timeout_diag`,
`/home/vscode/tau_campaign_v2`) remain untouched and must not be reused for
selection; `/home/vscode/epoch_pilot` is untouched forensic evidence.

First files to inspect: `testdata/final_evaluation/` (frozen negative result),
`testdata/diagnostics/dqn_validation_v4/` (Candidate v4),
`testdata/diagnostics/dqn_validation_v3/` (v3 experiment),
`internal/rl/dqn/` (§22.22–§22.24, §22.28), `python/rl/`,
`python/train_dqn.py`, `python/train_validate.py`, `python/train_validate_v2.py`,
`python/train_validate_v3.py`, `python/train_validate_v4.py`,
`python/rl/sim_env_v4.py`, `python/train_validate_v5.py`,
`python/rl/sim_env_v5.py`, `python/rl/state_v5.py`,
`testdata/diagnostics/dqn_validation_v5/`,
`cmd/dqn-live-v5/`, `testdata/diagnostics/dqn_validation_v5_live/`,
`cmd/final-evaluation-v5/`, `testdata/final_evaluation_v5/`,
`python/report_final_evaluation_v5.py`,
`testdata/diagnostics/dqn_validation_v2/` (frozen Candidate v2),
`testdata/diagnostics/dqn_validation/` (historical Candidate v1),
`testdata/diagnostics/dqn/`, `cmd/dqn-transfer/`,
`testdata/diagnostics/aria2_calibration/` (frozen aria2 baseline),
`cmd/aria2-calibration/`,
`testdata/final_research_audit/` (audit artifacts),
`docs/PROJECT_CONTEXT.md` §22.21, `internal/rl/linucb.go`,
`internal/experiment/matrix.go` and `experiment.go`, `cmd/experiment/`,
`cmd/aria2-benchmark/`, `cmd/aria2-wallclock/`, `cmd/wallclock-throttle/`,
`cmd/granularity-study/`, `testdata/benchmark_v6/`, `testdata/benchmark_aria2_v2/`,
`testdata/diagnostics/wallclock_throttle/`, `testdata/diagnostics/aria2_wallclock/`,
`testdata/diagnostics/request_granularity/`,
`testdata/diagnostics/request_granularity_wallclock/`,
`cmd/epoch-pilot/`, `internal/experiment/pilot/`,
`internal/experiment/replay/`, `internal/experiment/epoch/` (§22.59–§22.63),
`testdata/diagnostics/linucb_lockin/`,
`testdata/diagnostics/linucb_periodic_exploration/`.

---

## 28. How to continue from another client

1. Read `AGENTS.md`.
2. Read this file (`docs/PROJECT_CONTEXT.md`).
3. Inspect relevant source and tests; treat them as authoritative.
4. Run the appropriate test suite before major changes.
5. Update this document after substantive work
   (`PROJECT_CONTEXT.md updated: yes`).

Useful commands (repository root):

```bash
go test ./...
go test -race ./...
go test -count=5 ./...
go vet ./...
gofmt -l .
go build ./...
```

Single-scenario experiment (four modes; flags verified via
`go run ./cmd/experiment -h`):

```bash
go run ./cmd/experiment -reps 2 -file-size 262144 \
  -chunk 32768 -min-chunk 32768 -max-chunk 131072 \
  -initial-chunk 32768 -chunk-step 32768 \
  -concurrency 1 -min-concurrency 1 -max-concurrency 4 \
  -initial-rate 524288 -changed-rate 4194304 -switch-after 4 \
  -improve 0.2 -degrade 0.2 -window 4 -retries 3
```

Matrix (add `-sudo` for tc/netem in this container; `-json` for machine-readable
output; `-file-size` overrides every scenario's file size, default matches
`DefaultMatrixConfig`):

```bash
go run ./cmd/experiment -matrix -sudo -reps 10
go run ./cmd/experiment -matrix -sudo -reps 10 -json > matrix.json
go run ./cmd/experiment -matrix -sudo -reps 10 -json -file-size 4194304 > matrix_4MiB.json
```

Mahimahi replay harness smoke (§22.59, §22.63; synthetic traces only, sparse
output, mandatory watchdog). Since §22.63, `--downlink-log` evidence is
required for `SUCCESS_TRACE_REACHED_EOF`: without it the outcome is
`TRACE_ENDPOINT_NOT_REACHED` (by design, never EOF):

```bash
go run ./cmd/mm-replay -up /tmp/up.link -down /tmp/down.link -c 4 \
  -timeout 120s -workload-size 67108864 -chunk 262144 -max-runtime 30s \
  -output /tmp/mm-out.bin -summary /tmp/mm-summary.json \
  -downlink-log /tmp/mm-down.log
```

Opt-in LinUCB mode (deterministic smoke; does not run the research matrix):

```bash
go run ./cmd/experiment -modes linucb -reps 1 -linucb-alpha 0.2
go run ./cmd/experiment -modes fixed,linucb -reps 1
```

LinUCB calibration and reward diagnostics (writes
`testdata/calibration/calibration.json`; flags verified via
`go run ./cmd/calibration -h`):

```bash
go run ./cmd/calibration -reps 2 -alpha 0.2
go run ./cmd/calibration -reps 1 -json
```

LinUCB exploration diagnostic (separate forced-action study; writes
`testdata/diagnostics/linucb_exploration/report.json` + `.txt`; `-sudo` only for
the loss scenario's tc/netem):

```bash
go run ./cmd/linucb-diagnostic -reps 5 -sudo
```

LinUCB learning-horizon study (separate; varies only file size; writes
`testdata/diagnostics/linucb_horizon/report.json` + `.txt`):

```bash
go run ./cmd/linucb-horizon -sizes 262144,524288,1048576,2097152,4194304 \
  -scenarios baseline,server-throttle,latency,combined -modes linucb,fixed -reps 3 -sudo
```

Available experiment flags: `-changed-rate -chunk -chunk-step -concurrency
-degrade -file-size -improve -initial-chunk -initial-rate -json -linucb-alpha
-matrix -max-chunk -max-concurrency -min-chunk -min-concurrency -modes -reps
-retries -sudo -switch-after -timeout -window`. Calibration flags: `-alpha -json
-out -reps`. Diagnostic flags: `-reps -out -sudo -loss`.

