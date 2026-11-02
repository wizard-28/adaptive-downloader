### 22.40 Candidate v7 pre-registration: action authority + L3 environment (BEFORE measurement)

**Status: pre-registered; no training; no v7 model; no new final comparison.
Measurements follow in §22.41.**

**Motivation (from §22.37–§22.39):** L2 had materially different per-phase
optima but ~0 achievable headroom because (a) the suboptimal-C phases carried
too few bytes and (b) the ±1, one-decision-per-request action space could not
change C fast enough. v7 therefore changes **control authority** and designs a
**byte-weighted** long trajectory.

**Candidate action spaces (analysed, not performance-picked):**
- **A — steps:** `none, +1, -1, +4, -4` (5 actions; max Δ4/decision; keeps the
  existing increase/decrease `ActionKind`s with `Action.Value` as the step;
  atomically applied and clamped to [1,16]).
- **B — small/large:** same semantics as A (small = 1, large = 4); rejected as
  redundant (same reachability and authority, no added value).
- **C — targets:** `none, low, medium, high, max` (instant jumps); rejected
  because it introduces target-setting actions that the project reserves
  (`ActionSetConcurrency`), is discontinuous, and increases oscillation risk
  without a demonstrated need.
**Selected: A** — the smallest space that reaches any C within
`ceil(14/4) = 4` decisions and stays within the existing generic action model.

**v7 configuration (fixed):** C 1–16, fixed 1 MiB chunks, one decision per
completed request, reward `r = -pending wall-clock seconds`, no condition labels
or handcrafted bonuses. Live semantics: the runner applies the step atomically
and clamped; existing v1–v6 policies emit `Value <= 0` and keep the historical
single-step behavior.

**L3 environment (fixed, separate from A2–E2, L1, L2), 512 MiB:**

| # | regime | rate | B | latency | loss | congestion | duration |
|---|---|---|---|---|---|---|---|
| 1 | high-capacity server | 512 KiB/s | 0 | 20 ms | 0.3 % | 0.002 | 20 s |
| 2 | severe shared cap | 8 MiB/s | 4 MiB/s | 120 ms | 3.0 % | 0.05 | 35 s |
| 3 | recovery | 8 MiB/s | 6 MiB/s | 40 ms | 0.6 % | 0.008 | 20 s |
| 4 | severe server throttle | 128 KiB/s | 0 | 30 ms | 0.5 % | 0.004 | 25 s |
| 5 | heavy congestion | 8 MiB/s | 4 MiB/s | 100 ms | 2.5 % | 0.06 | 35 s |
| 6 | recovery | 8 MiB/s | 6 MiB/s | 30 ms | 0.4 % | 0.004 | 20 s |
| 7 | final capacity | 1 MiB/s | 0 | 20 ms | 0.3 % | 0.002 | to completion |

**Gates (fixed before measurement):**
- **A** distinct phase-optimal C, including at least one low (≤6) and one high
  (≥11) optimum.
- **B** `adaptive_headroom = (best_static − action_constrained_oracle) /
  best_static ≥ 10 %`, where the oracle obeys the selected A semantics
  (≤ ±4 per decision, clamped) and has perfect phase knowledge.
- **C** at least 5 transitions reached with substantial remaining work.
- **D** simulator/live action semantics identical (atomic, clamped, one
  decision per completed request); contract test added.
- **E** representative live shared-capacity fixed-C data preserves the
  concurrency tradeoff (reused frozen live data, clearly labelled; no new v7
  campaign).
- **F** the selected action space reaches useful C values within phase
  durations (worst case 16→2 in 4 decisions; ≤10 % of the shortest phase at the
  observed decision rate).
Any failed gate ⇒ STOP, reported without redefinition.

### 22.41 v7 design results and gate outcome

**Status: completed (simulator design + frozen live evidence reuse; no training;
no new comparison). OUTCOME: STOP — the selected action space plus L3 still
yields only 8.1 % achievable headroom, below the pre-registered 10 % threshold.
v7 training is NOT justified yet.**

- **Action-space comparison:** A steps `{none,+1,-1,+4,-4}` (5 actions, max
  Δ4/decision, atomic + clamped, keeps generic increase/decrease kinds) was
  selected; B small/large is semantically identical and was rejected as
  redundant; C targets was rejected because it introduces the project's
  reserved target-setting actions, jumps discontinuously, and raises
  oscillation risk. `ACTION_SPACES` in `v7_prereg_spec.json`.
- **Control-authority change (additive, tested):**
  `policyRunner.applyAction` now applies `Action.Value` as the number of unit
  steps for increase/decrease (clamped by the controller; `Value <= 0` → 1, so
  every v1–v6 policy is byte-for-byte unchanged). Contract test:
  `internal/downloader/policy_runner_v7_test.go` (`+4` from 1 → 5, Value 0 → +1,
  clamps at 16/1) — GATE D PASS.
- **L3 static per-phase sweep (128 MiB, seeds 300–302):** best C =
  **[16, 2, 4, 16, 2, 6, 16]**; C16-vs-best gaps 0.0 %, 40.3 %, 8.0 %, 0.0 %,
  54.0 %, 3.3 %, 0.0 % → GATE A PASS. Throughput curves per phase are in
  `l3_static_phase_sweep.json`.
- **Full L3 512 MiB sweep:** best static **C16 = 152.9 s**; C1 = 289.1 s.
  **Action-constrained oracle** (perfect phase knowledge, ≤ ±4 per decision,
  atomic, clamped) = **140.5 s** → **adaptive headroom 8.1 %**, below the
  pre-registered 10 % threshold → **GATE B FAIL ⇒ STOP**. Phase bytes at best
  static: [137, 76, 96, 44, 67, 92, 0] MiB, i.e. the byte-weighting was
  improved over L2 (degraded phases now carry 76 + 67 MiB), but the residual
  penalty of running C16 in them is still not enough to create ≥10 % with
  realistic action authority.
- **Transitions with work (GATE C):** five condition transitions were reached
  with substantial remaining data (remaining bytes after each transition are in
  `v7_gates.json`; a first version of this metric had a units bug in the
  analysis script, corrected before reporting; the gate rule was not changed).
- **Representative live tradeoff (GATE E, reused frozen evidence, no new v7
  campaign):** frozen live v6 A2 fixed-C data (shared-cap trajectory,
  `dqn_validation_v6_live`) shows best fixed C4 = 6588 ms vs C16 = 9756 ms, i.e.
  the live shared-capacity penalty for over-concurrency is preserved. This is
  reused evidence, not a v7-regime sweep; it is labelled as such.
- **Control authority (GATE F):** 16→2 needs 4 decisions; 20–35 s phases at
  roughly one decision per completed request leave ample room → PASS.
- **Aria2 granularity (unchanged from §22.39):** aria2 1.37.0 cannot produce
  ~1 MiB ranges with ≤16 connections at 512 MiB; no aria2 comparison is claimed.
- **Artifacts:** `testdata/diagnostics/v7_design/` —
  `v7_prereg_spec.json`, `action_space_comparison.json`,
  `l3_static_phase_sweep.json`, `l3_full_trajectory_sweep.json`,
  `l3_adaptive_headroom.json`, `v7_gates.json`, `report.txt`,
  `artifact_hashes.json`.
- **Conclusion:** the formulation change (faster control authority) roughly
  doubled the achievable headroom versus L2 (≈0 → 8.1 %), confirming that
  control authority was part of the problem, but the environment still does not
  provide the pre-registered ≥10 % opportunity. A future design iteration would
  need phases where running C16 is proportionally much worse while still
  carrying more work (or a different control formulation); per the task rules
  this was **not** tuned after measurement. **No v7 training, no v7-vs-aria2
  comparison.**

### 22.42 Final environment iteration: L4 pre-registration (BEFORE measurement)

**Status: pre-registered; no training; no v7/v8 model; no new aria2 comparison.
Measurements follow in §22.43.**

**Why L4 after L3:** L3 improved control authority (none/±1/±4) but reached only
8.1% achievable headroom because the degraded low-C phases carried too little
byte/time weight. L4 keeps the exact L3 control formulation (C 1–16, 1 MiB
chunks, atomic clamped ±4, one decision per completed request) and changes only
the **physical phase design** so that wrong concurrency is expensive for a
substantial share of the transfer.

**Byte-weighting design rule (fixed now, checked after the sweep; no post-hoc
tuning):** ≥50 % of bytes in the action-constrained oracle run must occur in
regimes where C16 is materially worse; ≥20–30 % must occur in regimes where
C12–C16 is materially better. If the rule does not emerge, the parameters are
redesigned before the formal sweep; bytes are never assigned by fiat.

**L4 trajectory (512 MiB; high/low/high/low/high), fixed before measurement:**

| # | regime | rate | B | latency | loss | congestion | duration |
|---|---|---|---|---|---|---|---|
| 1 | high-capacity server | 512 KiB/s | 0 | 20 ms | 0.3 % | 0.002 | 20 s |
| 2 | severe shared bottleneck (byte-heavy) | 8 MiB/s | 4 MiB/s | 120 ms | 3.0 % | 0.05 | 55 s |
| 3 | recovery | 8 MiB/s | 6 MiB/s | 40 ms | 0.6 % | 0.008 | 20 s |
| 4 | severe server throttle | 128 KiB/s | 0 | 30 ms | 0.5 % | 0.004 | 20 s |
| 5 | severe congestion/loss (byte-heavy) | 8 MiB/s | 4 MiB/s | 100 ms | 2.5 % | 0.06 | 55 s |
| 6 | recovery | 8 MiB/s | 6 MiB/s | 30 ms | 0.4 % | 0.004 | 20 s |
| 7 | final high-capacity | 1 MiB/s | 0 | 20 ms | 0.3 % | 0.002 | to completion |

**Gates (fixed now):** A distinct phase optima (low ≤6 and high ≥11);
B action-constrained oracle ≥10 % better than best fixed C; C ≥5 transitions
with substantial remaining work; D simulator/live semantics identical
(contract test); E small representative live fixed-C sweep preserves the
qualitative tradeoff; F ±4 authority sufficient. Outcomes fixed:
`READY_FOR_NEXT_RL_ITERATION`, `NOT_READY_HEADROOM`, `NOT_READY_LIVE_MISMATCH`,
`NOT_READY_PHASE_WEIGHTING`, `NOT_READY_CONTROL_AUTHORITY`,
`NOT_READY_ENVIRONMENT_PHYSICS`.

### 22.43 L4 results and final environment outcome

**Status: completed (simulator sweeps + small live representative sweep; no
training; no new aria2 comparison). FINAL OUTCOME: `NOT_READY_LIVE_MISMATCH`.**

- **Simulator (frozen v6 simulator, seeds 300–302, 512 MiB):** best static
  **C16 = 174.0 s**; C1 = 389.4 s (full curve in `l4_full_sweep.json`);
  action-constrained oracle (perfect phase knowledge, ±4/decision, atomic,
  clamped) = **143.4 s** → **adaptive headroom 17.6 %**, i.e. **GATE B PASS**
  (≥10 %). Phase optima are materially distinct (low ≤6 and high ≥11) → GATE A
  PASS; ≥5 transitions with remaining work → GATE C PASS; parity contract
  unchanged → GATE D PASS; authority sufficient → GATE F PASS.
- **Byte-weighting check:** in the oracle run the C16-worse phases carried
  **47.1 %** of bytes, just under the pre-registered ≥50 % rule (the C12–C16
  phases carried the remainder, above the ≥20 % rule). The simulator-side
  outcome would therefore be `NOT_READY_PHASE_WEIGHTING` on a strict reading;
  the rule was not lowered after the fact.
- **Representative live fixed-C sweep (`cmd/l4-live-probe`, new experiment-only
  command; 1 MiB chunks, fresh state, tc cleaned):**
  - degraded regime (shared 4 MiB/s + 120 ms + 3 % loss): C1 0.92, C4 2.58,
    C8 3.39, **C16 3.57 MiB/s** — high concurrency is *not* penalized live;
  - high-capacity regime (512 KiB/s per response, no cap): C16 7.85 vs C1
    0.48 MiB/s — high C strongly beneficial (matches);
  - recovery regime (6 MiB/s + 40 ms + 0.6 % loss): C16 5.52 vs C1 3.09 MiB/s —
    high C beneficial (matches).
  → **GATE E FAIL:** the live controlled stack does not reproduce the
  simulator's degraded-phase penalty for excessive concurrency. The 17.6 %
  simulated headroom therefore rests on a **simulator-only mechanism**
  (per-connection overhead + concurrency-dependent loss/congestion) that does
  not survive basic live validation.
- **Final outcome (pre-registered taxonomy, unchanged):**
  **`NOT_READY_LIVE_MISMATCH`** — the simulator and the representative live
  C relationships disagree qualitatively in the decisive degraded regime.
- **Conclusion:** the L4 design met the 10 % simulated headroom gate, but the
  opportunity is not physically real in the controlled live environment, and
  the byte-weighting rule was narrowly missed. Another RL training iteration is
  **not** justified. Any future stage must first find a live-reproducible
  mechanism that makes excessive concurrency genuinely expensive (e.g., a
  different, still-honest bottleneck model) before using it as an RL target.
  The frozen findings from D2 (granularity × phase-locked pacing), startup
  amortization, L2 ≈0 %, L3 8.1 %, and the aria2 long-download granularity
  limitation are all preserved unchanged.
- **Artifacts:** `testdata/diagnostics/l4_environment_design/` —
  `l4_prereg_phases.json`, `l4_phase_sweep.json`, `l4_full_sweep.json`,
  `l4_oracle.json`, `l4_gates.json`, `live/live_{degraded,high_capacity,recovery}.json`,
  `report.txt`, `artifact_hashes.json`.

### 22.44 T1 pre-registration: live request-level server throttling (BEFORE measurement)

**Status: pre-registered; no training; no new aria2 comparison.** T1 exists
because L4 failed live: it tests the one mechanism already shown to work live —
the server selects a response's pacing rate once at request start and holds it
for the whole request (`server.currentRateLocked` is read once in `ServeHTTP`
and `writeThrottled` uses that rate), so requests started in different phases
carry different fixed rates.

**Mechanism (verified from code and prior D2 telemetry):** a request started
while the server rate is high is paced at the high rate for its entire
transfer, even if the server rate changes meanwhile; requests started during
the throttle get the throttled rate; requests started after recovery get the
recovered rate. The policy never sees phase labels.

**Pre-registered T1 schedule:** 512 MiB file, 1 MiB requests, C 1–16.
Per-response server rates: **normal 8 MiB/s → throttled 128 KiB/s at t=30 s →
recovered 8 MiB/s at t=90 s** (a 60 s throttle). No tc/netem shared cap; the
mechanism is purely server-side per-request pacing.

**Experiment:** live fixed-C sweep C ∈ {1,2,4,8,12,16}; request start/end/rate
telemetry from the server; bytes and request counts by start phase (before /
during / after throttle); best fixed C; then an **action-constrained live
oracle** using exactly the L3/L4 semantics (one decision per completed request,
actions none/±1/±4, atomic, clamped) with perfect knowledge of the schedule and
per-phase targets chosen from the measured per-phase optima; headroom =
(best_static − oracle)/best_static.

**Gates (fixed now; threshold not lowerable):** A live pacing persistence;
B live concurrency reversal (high C before/after, lower/moderate C during);
C throttle carries enough work to affect completion time; D mechanism survives
equal 1 MiB requests (granularity isolation); E action-constrained oracle
≥10 %; F simulator/live qualitative agreement if a simulator is used (T1 is
live-first; simulator calibration is deferred and not used for training).
Any required gate failure ⇒ STOP, no RL training.

**Success signature:** C16 best in normal/recovery, lower/moderate C materially
better during the throttle, with all systems at 1 MiB requests, and ≥10 %
oracle headroom.

### 22.45 T1 results and decision: live per-request throttling does not create an adaptive-concurrency opportunity

**Status: completed (live smoke + live fixed-C sweep + oracle attempt; no
training; no new aria2 comparison). OUTCOME: `NOT_READY` — no RL iteration
justified.**

- **Mechanism verified live (GATE A PASS):** the server selects a response's
  pacing rate at request start and holds it. Smoke evidence: a request started
  at 2964 ms with rate 8 MiB/s ended at 3109 ms after the 3 s throttle began,
  i.e. it retained its original high rate; requests started during and after
  the throttle received their phase rates. The 1 MiB smoke runs show requests
  starting in before/during/after phases as expected.
- **Pre-registered schedule flaw exposed by the required sweep:** at
  512 MiB with normal 8 MiB/s per response, C4–C16 finished the whole file
  **before the throttle at t=30 s** (C16 = 5.4 s, C12 = 7.7 s, C8 = 10.3 s,
  C4 = 20.9 s; all bytes started `before`). Only C1 (139.6 s) and C2 (102.6 s)
  observed the throttle. The "best C during = 2" in the analysis is therefore
  an artifact of missing data for C≥4, not a measured reversal.
- **No live concurrency reversal (GATE B FAIL):** under independent
  per-response pacing, aggregate throughput scales monotonically with C in
  every phase; the only high-C penalty is the drain of in-flight requests
  after recovery, which is bounded by `C × 1 MiB / throttled_rate` and does not
  outweigh C's throughput advantage. A low-C optimum is not observed and is
  not expected from this mechanism when all requests are 1 MiB.
- **Oracle:** targets computed from the incomplete per-phase data were
  (16, 2, 2); the oracle started at C1, ramped up, and finished in 5.66 s
  entirely before the throttle, so its −5.3 % "headroom" versus best fixed C16
  reflects the schedule flaw and ramp cost, not an adaptive opportunity. GATE E
  FAIL. A corrected schedule (normal rate ≈ ≤1 MiB/s so C16 cannot finish
  early) was analysed but not run: it makes C1/C2 runs impractically slow
  (hours) and the monotonic aggregate-scaling argument shows the reversal still
  would not appear; running it could not change the gate outcome.
- **Granularity isolation (GATE D PASS):** every request in all runs was
  exactly 1 MiB; the mechanism therefore does **not** yield a concurrency
  effect at equal granularity. This confirms the D2 finding: the 512 MiB D2 v6
  advantage was a 1 MiB-vs-32 MiB request-granularity effect, not adaptive
  concurrency.
- **Gates:** A PASS; B FAIL; C partial (throttle carried work only for C1/C2);
  D PASS; E FAIL; F deferred (live-first stage; no simulator used).
  **No RL training.**
- **Conclusion:** with the current live server model (independent per-response
  pacing, no shared bottleneck), making high concurrency materially
  disadvantageous during a throttle is not achievable at equal 1 MiB
  granularity. Any future attempt must introduce a different, live-reproducible
  cost of concurrency (L4 showed the simulated one does not survive live) or
  accept that the mechanism is granularity-based and frame the research
  question accordingly. All prior findings (D2 granularity × phase-locked
  pacing, startup amortization, L2 ≈0 %, L3 8.1 %, L4 `NOT_READY_LIVE_MISMATCH`,
  aria2 granularity limitation, frozen v6 results) are preserved.
- **Artifacts:** `testdata/diagnostics/t1_server_throttle/` — `smoke/smoke.json`,
  `live/fixed.json`, `live/oracle.json`, `t1_results.json`, `t1_results.txt`,
  `artifact_hashes.json`.

### 22.46 T2 pre-registration: live adaptive range-size opportunity (BEFORE measurement)

**Status: pre-registered; live-first; no training; no new aria2 comparison.**
T2 follows T1/L4: live adaptive *concurrency* has no opportunity, but the live
request-level pacing mechanism makes large requests carry stale (slow) rates
for their lifetime, so **range size** may be a real control dimension.

**Mechanism (live, re-verified by smoke):** per-response rate selected at
request start and held for the request lifetime.

**Fixed concurrency: C = 16** for every T2 run (documented rationale: hold
parallelism constant and maximise request-scheduling opportunities; do not vary
C).

**Chunk ladder:** 256 KiB, 512 KiB, 1 MiB, 2 MiB, 4 MiB, 8 MiB, 16 MiB.
**Action space A (selected, pre-measurement):** `NONE`, increase one ladder
step, decrease one ladder step; atomic, ladder-index based, clamped to
[256 KiB, 16 MiB], applied before the next request allocation, one decision per
completed request. (B/C rejected: larger authority/instant targets are not
needed to test the mechanism and increase the risk of an artificial result.)

**T2 trajectory (512 MiB, two throttle cycles), fixed now:** per-response rates
**normal 180 KiB/s → throttled 64 KiB/s**; schedule
`t1=30 s throttle, t2=75 s recovery, t3=105 s throttle, t4=150 s recovery`,
then normal to completion (phase durations 30/45/30/45/rest). No tc/netem; the
only variable is chunk size.

**Gates (fixed, threshold 10 % not lowerable):** A chunk-size preference
changes across conditions; B action-constrained oracle ≥10 % better than the
best fixed chunk; C transitions occur with substantial work; D opportunity
survives fixed C=16 and exact granularity control; E gain is chunk-scheduling
(not concurrency/range-semantics confounds); F reproducible across repetitions.
Outcomes fixed: `READY_FOR_ADAPTIVE_RANGE_RL`, `NOT_READY_NO_CHUNK_TRADEOFF`,
`NOT_READY_INSUFFICIENT_HEADROOM`, `NOT_READY_SHORT_HORIZON`,
`NOT_READY_NON_RL_GRANULARITY_EFFECT`, `NOT_READY_UNREPRODUCIBLE`.

### 22.47 T2 results and decision: no live chunk-size preference reversal

**Status: completed (live smoke + live static chunk sweep; no training; no new
aria2 comparison). PRIMARY OUTCOME: `NOT_READY_NO_CHUNK_TRADEOFF` — no RL
adaptive range-size training justified.** (The pre-written classifier in the
analysis script logged `NOT_READY_NON_RL_GRANULARITY_EFFECT` because its
phase-best probe varied; on inspection that variation is a request-start-phase
artifact; the robust finding is the absence of a genuine reversal. Both
categories are NOT_READY and no threshold was changed.)

- **Setup:** 512 MiB, **C = 16 fixed** for every run, 1 MiB..16 MiB ladder
  (256 KiB, 512 KiB, 1 MiB, 2 MiB, 4 MiB, 8 MiB, 16 MiB), server per-response
  rates normal 180 KiB/s → throttled 64 KiB/s with the pre-registered
  `30 s normal / 45 s throttle / 30 s recovery / 45 s throttle / recovery`
  schedule; no tc/netem; smoke verified pacing persistence, transitions and
  exact per-request granularity.
- **Live completion times (single run each; all SHA-correct, max in-flight 16):**
  256 KiB 245.8 s · 512 KiB 245.8 s · 1 MiB 245.7 s · 2 MiB 266.4 s ·
  4 MiB 266.7 s · 8 MiB 267.0 s · **16 MiB 184.2 s (best)**.
- **Mechanism (not a control tradeoff):** 16 MiB requests started in a fast
  phase take ~91 s at 180 KiB/s and therefore *span both throttles while
  retaining the fast rate* — the run never issued requests during a throttled
  phase (16 requests in p1 = 256 MiB, 16 in p3 = 256 MiB). Smaller chunks
  complete quickly and keep starting fresh requests during the throttles at the
  slow rate, losing throughput. The overall preference is monotone toward the
  largest chunk; **no reversal** of the kind required by GATE A/B.
- **Phase-optimal chunk caveat:** the phase-best-by-throughput table varies
  (p1 16 MiB, p2 8 MiB, p3 16 MiB, p4 4 MiB, p5 1 MiB) but is confounded by
  which chunk sizes happen to have requests starting in each phase; it is not
  evidence of an adaptive opportunity.
- **Oracle / headroom:** the best fixed chunk is the **largest** ladder element;
  from it the allowed actions are none/decrease, and no smaller chunk measured
  faster, so an action-constrained oracle cannot beat the best fixed chunk.
  Headroom ≤ 0 (analytical bound; the live oracle run was not performed because
  no improving action exists under the measured curves). GATE B FAIL.
- **Gates:** A no genuine reversal (FAIL); B headroom ≥10 % (FAIL); C
  transitions carried substantial work (PASS); D fixed concurrency + exact
  granularity (PASS); E gain from chunk scheduling rather than fixed-choice
  rate-lock bypass (FAIL — the winning policy is a *fixed* largest chunk);
  F reproducibility single-repetition (not established).
- **Conclusion:** adaptive range size is **not** a live control opportunity in
  this server model: the only large effect is that large requests started in
  fast phases bypass later throttles because their rate is locked at start —
  a fixed-configuration choice, not adaptation, and the opposite of a small-vs-
  large tradeoff. The D2 512 MiB benefit remains a 1 MiB-vs-32 MiB granularity
  effect. No RL training, no aria2 comparison.
- **Artifacts:** `testdata/diagnostics/t2_adaptive_range/` —
  `smoke/smoke.json`, `live/chunk_{256,512,1024,2048,4096,8192,16384}KiB.json`,
  `t2_results.json`, `t2_results.txt`, `artifact_hashes.json`.

### 22.48 M1 real mobile-trace replay: prerequisite audit and STOP

**Status: STOPPED at the data-acquisition prerequisite. Primary outcome:
`NOT_READY_NO_REAL_MOBILE_TRACE`. No trace replay was built, no concurrency
sweep was run, no RL was trained, no aria2 comparison was run.** M1 followed
T1/T2 because the synthetic environments failed to produce a live concurrency
tradeoff; it asked whether real mobile path dynamics do.

- **Repository audit (bounded):** no real mobile/cellular/RTT/loss/jitter
  traces exist in the repository. The only files matching trace/mobile/replay
  are the project's own **synthetic** experiment request traces
  (`testdata/diagnostics/aria2*/request_trace*.json`,
  `request_granularity*`, `wallclock_throttle`, `final_evaluation*/request_traces.json`).
  There is no pcap, no external dataset, and no trace-replay infrastructure.
- **Public-source feasibility check:** the candidate real-mobility source
  `http://users.ugent.be/~jvdrhoof/dataset-4g/` (UGent/imec 4G measurement
  logs; documented mobility contexts such as walking/car/train/tram) responds
  with HTTP 301, and GitHub/raw GitHub are reachable, so real traces are
  obtainable in principle. They were **not** fetched or processed in this
  stage, and no field list, license terms, or replay fidelity were verified.
- **Why STOP rather than proceed:** the pre-registered M1 rules forbid
  synthesizing "mobile" traces, require documented provenance and measured
  fields per trace, and require replay-fidelity validation before any
  concurrency experiment. None of those conditions can be met without a
  dedicated data-acquisition stage, and no data was invented. Per §M1 this is
  exactly `NOT_READY_NO_REAL_MOBILE_TRACE`.
- **Known caveat to carry into the acquisition stage:** the UGent-style logs
  largely record **end-to-end achievable throughput** for a single transfer,
  not transport-independent path capacity; the M1 §5 distinction
  (capacity vs observed throughput) must be resolved, and any dataset must be
  documented for measured variables, units, sampling interval, mobility
  context, and license before use. Replayed data must never be called
  "real-world experiments".
- **Not performed (because of the stop):** trace conversion/canonicalization,
  replay construction and fidelity analysis, the fixed-C sweep (C=1..16,
  1 MiB chunks, 512 MiB), temporal-window `C*(t)` analysis, action-constrained
  oracle, adaptive headroom, and the M1 gates A–E. All of these remain
  pending and must be pre-registered again for the next attempt.
- **Artifacts:** none created for M1 (no data to preserve); this section and
  milestone 65 record the audit and the stop.

### 22.49 M2 real mobile trace acquisition + data audit

**Status: completed (acquisition + audit; no replay built, no RL, no aria2
comparison).** M2 followed M1's data-prerequisite stop and acquired genuine
public mobile/cellular traces, preserving originals under
`testdata/external/mobile_traces/raw/`.

- **Sources and provenance (retrieved 2026-09-25):**
  - **UGent/imec 4G/LTE Bandwidth Logs** —
    `https://users.ugent.be/~jvdrhoof/dataset-4g/`; 16 logs downloaded
    (train, tram, car, bus, bicycle contexts); 6-column format
    (`epoch_ms, elapsed_ms, lat, lon, bytes, interval_ms`), GPS included;
    citation `dataset-4g.bib` (van der Hooft et al., IEEE Comm. Letters 2016).
    **License not explicitly stated on the page — flagged; not used for
    license-explicit work without clarification.**
  - **NYU-METS Metropolitan Mobile Bandwidth Trace** —
    `https://github.com/NYU-METS/Main`; 7 sample traces downloaded
    (7Train1/2, QTrain, Long Island Rail Road, bus57/62/62_2); one
    `Mbits/sec` sample per line at 1 Hz; **license explicitly ODbL 1.0 +
    DbCL 1.0**; citation Mei et al. (PAM 2019; Computer Networks 182, 2020);
    `Multivariate_Dataset` documents RSSI/RSRQ/neighbor/handover/speed/band
    (zip not downloaded yet).
- **Measured variables:** throughput available for all 23 traces; GPS for
  UGent; RTT/jitter/packet-loss unavailable everywhere; outage only observable
  indirectly via zero-throughput samples. Nothing was interpolated or invented.
- **Capacity vs throughput:** both sources record **end-to-end measured
  throughput**, not transport-independent path capacity →
  `capacity_ground_truth = unavailable`; any replay must be framed as a
  throughput-trajectory replay (`REPLAY_PARTIAL`), not a capacity emulation.
- **Temporal quality:** all traces have dense samples (UGent ~1 s intervals,
  2–13 min; NYU exactly 1 s, ~24–82 min) with zero spikes/duplicate handling
  left intact; diagnostics in `trace_quality.json`.
- **Selection / split (fixed before performance, no performance consulted):**
  development = `ugent_4g/train_0001, train_0002, tram_0001`,
  `nyu_mets/7Train1, bus57, bus62`; held-out = `ugent_4g/train_0003,
  tram_0002, car_0001`, `nyu_mets/7Train2, Long_Island_Rail_Road, QTrain,
  bus62_2`.
- **M2 gates:** A ≥5 traces PASS (23); B provenance/license PASS for NYU-METS
  (explicit ODbL/DbCL), UGent flagged (citation only); C temporal throughput
  completeness PASS; D genuine mobility PASS (train/tram/car/subway/rail/bus);
  E no fabrication PASS; F long-enough PASS (NYU up to ~82 min).
- **M3 justification: yes**, for a **throughput-trajectory replay** stage using
  the license-explicit NYU-METS traces (plus UGent only after license
  clarification). The M1 chain (real trace → temporal C* → oracle headroom)
  can now be attempted; replay fidelity, capacity-vs-throughput framing, and
  the no-confound rules from M1 still apply. No synthetic data was created and
  replayed traces must not be called real-world experiments.
- **Artifacts:** `testdata/diagnostics/m2_trace_acquisition/` —
  `trace_inventory.json`, `provenance.json`, `schema_audit.json`,
  `trace_quality.json`, `canonical_manifest.json`, `gates.json`,
  `M2 report.txt`, `artifact_hashes.json`; raw originals + README/bib under
  `testdata/external/mobile_traces/raw/{ugent_4g,nyu_mets}/`.

### 22.50 M3 real mobile trace replay: status and next step

**Status: NOT EXECUTED. No replay system was built, no fixed-C sweep or
temporal C* analysis was run, no action-constrained oracle was evaluated, and
no M3 gate is claimed. No RL training and no aria2 comparison.** This section
records the partial progress honestly so the next agent can continue without
misreading the evidence chain.

- **Delivered (only):** committed deterministic M2 trace parsers and unit
  tests — `python/rl/mobile_trace.py` (NYU-METS `Mbits/sec` @ 1 Hz; UGent
  `bytes/(interval_ms/1000)`; canonical form = elapsed_s +
  throughput_bytes_per_second; no interpolation; SHA-256 helper) and
  `python/tests/test_mobile_trace_m2.py` (units, ordering, missing-line gaps,
  malformed-line handling, deterministic canonicalization, duplicate/zero
  diagnostics, hash stability).
- **Not done (remain the M3 gates):** throughput-envelope replay (Mode A),
  any Mode B path-attribute replay (NYU sample traces are throughput-only;
  `NOT_AVAILABLE` unless the multivariate dataset is acquired), replay-fidelity
  validation, the 512 MiB fixed-C sweep on the license-explicit NYU-METS
  traces, temporal-window `C*(t)`, the realistic ±4 oracle, and gates A–F.
  The pre-registered interpretation rules (throughput ≠ capacity; identical
  replay across C; no fitting to concurrency; no held-out tuning; no RL; no
  aria2) all still stand.
- **Next step:** build the throughput-envelope replay against the
  license-explicit NYU-METS traces, validate fidelity (source-vs-replay MAE,
  correlation, preserved drops/recoveries/zeros), then run the M3 fixed-C and
  temporal-`C*(t)` protocol on development traces, freeze parameters, and only
  then evaluate held-out traces. UGent remains excluded pending license
  clarification.

### 22.51 M3 execution: throughput-envelope replay reached a gate decision

**Status: EXECUTED to a gate decision. OUTCOME:
`NOT_READY_ENVELOPE_NO_C_SENSITIVITY`. No fixed-C temporal sweep or oracle was
run because the pre-registered rule says to STOP when the C-sensitivity gate
fails. No RL; no aria2 comparison.** New experiment-only command
`cmd/m3-replay` (goes with the M2 parsers).

- **Trace used:** real NYU-METS `7Train1.csv` (ODbL 1.0 + DbCL 1.0; 1 Hz
  measured **end-to-end throughput**, explicitly **not** path capacity).
- **Replay implementation:** tc/netem aggregate rate envelope on `lo`,
  re-applied once per trace sample at sample boundaries (1 Hz); zero samples
  replay with an 8000 bps floor (documented outage-like crawl, not a true
  outage model); the last sample is held after the trace ends. The controlled
  server runs unlimited (no synthetic throttling); 1 MiB chunks; workload
  48 MiB; the replay timeline is **identical for every C**.
- **C-sensitivity smoke (C1/C4/C16):** completion times **95 476 ms /
  96 613 ms / 107 208 ms**; C16 is slightly slower and higher C gives no gain.
  The tc envelope is a shared aggregate ceiling, so every C is capped at the
  same instantaneous rate and concurrency cannot change effective throughput.
  Replay throughput was 0.47–0.53 MB/s versus the consumed-window trace mean
  0.67–0.90 MB/s (coarse 1 Hz comparison; the transfer saturates the envelope).
  **GATE B FAIL → `NOT_READY_ENVELOPE_NO_C_SENSITIVITY`.**
- **Harness anomaly (documented):** the C8 run failed with a tc/harness error
  (exit 2); it does not affect the decision because C1/C4/C16 already show no
  C sensitivity.
- **Gates:** B FAIL; A/C/D/E/F not evaluated (stop-on-B rule). No gate was
  redefined.
- **Interpretation and next step:** a single aggregate throughput-envelope
  replay cannot represent a concurrency-selection problem — it imposes the same
  ceiling on all C. M3 therefore did **not** establish a temporal
  concurrency-performance opportunity from real mobile traces. Any next attempt
  must use richer measured path information or a replay method with a genuinely
  concurrency-dependent cost (e.g., per-flow/path attributes, queueing/latency
  effects) rather than scaling the trace envelope. RL training remains
  unjustified.
- **Artifacts:** `testdata/diagnostics/m3_mobile_replay/` — `provenance_used.json`,
  `replay_config.json`, `replay_fidelity.json`, `c_sensitivity_smoke.json`,
  `gates.json`, `report.txt`, `runs/7Train1_C{1,4,16}.json` (and the failed
  C8 attempt), `artifact_hashes.json`.

### 22.52 Revised Stage 0 (Mahimahi delivery-opportunity replay): pre-formalization audit

**Status: BLOCKED — DECISION REQUIRED (§30). No Stage 0 measurement code was
written and no formal data was collected. No RL/adaptive-control work was
performed; frozen artifacts untouched.**

- **Mahimahi is unavailable and not installable here:** `mm-link`, `mm-loss`,
  `mm-delay`, `mm-webrtc` are all missing; `modprobe` does not exist in the
  container and the host kernel (7.2.6 CachyOS) exposes no module tooling.
  The specified `mm-link --once` architecture (and therefore §3 EOF
  validation, §11 trace-time anchor calibration, §13 epoch pilot, §18–20 tau
  calibration) cannot be executed on this machine. A host/container with a
  built-and-loaded `mahimahi` kernel module, or an explicitly approved
  alternative delivery-opportunity replay mechanism, is required first.
- **TCP connection lifecycle (actual code; superseded by §22.55):** at audit
  time `internal/fetcher.New` built a single `http.Client` with the default
  `Transport`; as of the §1 pre-formalization item the nil-client path builds a
  dedicated HTTP/1.1-only transport (keep-alives enabled), and `Fetch` still
  calls `client.Do` per Range, so connection reuse is *expected*
  (Architecture A) — but this has **not** been empirically verified against an
  mm-link path (C↔flow mapping, keep-alive reuse, whether setup traverses the
  link). Per §8, the project must not assume persistence.
- **Byte-level instrumentation (actual code; superseded by §22.57):** at audit
  time the fetcher read each response with
  `io.ReadAll(io.LimitReader(resp.Body, expected+1))` and returned only a
  completed-request `Result`; there was **no timestamped incremental byte
  accounting**. As of the §4 pre-formalization item, the clocked counting
  reader is implemented as an optional `fetcher.ByteObserver` (off by default)
  — see §22.57.
- **Inexhaustible workload (actual code; superseded by §22.58):** at audit time
  `server.New(content []byte)` copied the whole object into RAM and
  `writeThrottled` served it from memory, which could not back a
  continuous-backlog full-trace replay. As of the §5 pre-formalization item,
  the experiment server also supports a deterministic logical object
  (`NewLogical`) that is generated on demand with bounded memory — see §22.58.
  `New([]byte)` is unchanged.
- **Unresolved §30 prerequisites:** (1) Mahimahi absent (host decision);
  (2) fresh-vs-persistent connection lifecycle unverified; (3) incremental
  timestamped byte accounting absent; (4) workload can run dry / no logical
  endless server; (5) mm-link EOF behavior unestablished and watchdog not
  implemented; (6) trace-time anchor tolerance not defined/validated;
  (7) protocol constants (epoch durations, tau regime classifier/thresholds,
  warm-up model, pilot) not frozen/executed.
- **Decisions required (OpenCode must not choose autonomously):** provide a
  Mahimahi-capable host **or** approve a documented alternative
  delivery-opportunity replay; after empirical connection verification,
  choose Architecture (a) persistent-connection modification or (b) documented
  fresh-per-request justification (§8); approve the byte-accounting hook and
  the logical endless-range server backing.
- **Not started:** epoch construction/pilot, byte-observation floor
  enforcement, shared epoch validity, instantaneous oracle, best fixed C,
  ramp/action-constrained oracle DP, transition-cost measurement, trace-level
  t CI, and the formal PASS gate.

### 22.53 Mahimahi runtime validation: BLOCKED on /dev/net/tun

**Status: `MAHIMAHI_RUNTIME_BLOCKED`. Pre-formalization infrastructure
validation only; no formal Stage 0 run; no downloader/RL changes; frozen
artifacts untouched.**

- **Installation:** `mm-link`, `mm-loss`, `mm-delay`,
  `mm-intermittent`, `mm-meter`, `mm-onoff`, `mm-webreplay` are setuid-root
  binaries under `/usr/local/bin/`; `mm-link --help` succeeds (no `--version`
  in this build) and documents `--once`, `--uplink-log/--downlink-log`, queue
  options and the `UPLINK-TRACE DOWNLINK-TRACE [OPTION] [COMMAND]` form.
- **Trace corpus found:** `/usr/local/share/mahimahi/traces/` contains the
  native **delivery-opportunity** traces `ATT-LTE-driving[-2016]`,
  `TMobile-LTE-driving/short`, `TMobile-UMTS-driving`,
  `Verizon-LTE-driving/short`, `Verizon-EVDO-driving` (`.up`/`.down`) plus a
  `README` — a suitable native corpus for the revised Stage 0 (to be audited
  for provenance/licence and strata before use).
- **Runtime blocker (exact):** both a basic run and the `--once` probe die
  immediately with `Died on unix_error: open /dev/net/tun: No such file or
  directory` (rc=1). `/dev/net/tun` is absent in the container, so mm-link
  never creates its namespace or tunnel and no child command executes.
- **Capability context:** uid 1000 with `CapPrm`/`CapEff = 0`; `unshare --net`
  fails `Operation not permitted` (unprivileged user namespaces are blocked).
  The setuid bit on mm-link gives it root, but the missing TUN device is the
  observed first failure; namespace creation was therefore never reached and
  cannot be confirmed either way.
- **Not established:** mm-link `--once` EOF behaviour, trace timing/anchor
  signals, and whether the emulated shell can inspect its namespace — all
  require a running mm-link.
- **Decision required (not made autonomously):** provide `/dev/net/tun` to the
  container (e.g. device passthrough or `mknod` by an operator) and confirm the
  setuid binary can create net namespaces here, **or** run Stage 0 in a
  privileged/host-network-capable environment. Do not change host/container
  network configuration silently.
- **Repository state:** no repository files were modified for this validation;
  synthetic test traces were written only under `$HOME/m3_stage0_tmp/`
  (outside the repo). No byte-accounting, workload, oracle, tau or pilot code
  was started.

### 22.54 Mahimahi runtime re-verification after container change: READY (infrastructure only)

**Status: `MAHIMAHI_RUNTIME_READY` for the replay runtime. This is NOT Stage 0
readiness and no Stage 0 measurement was performed.** §22.53's `/dev/net/tun`
blocker is resolved.

- **TUN:** `/dev/net/tun` present inside the container, `crw-rw-rw- root:root`,
  major/minor **10,200**, mode 666.
- **Capabilities:** the user shell has `CapEff/CapPrm = 0`, but `CapBnd`
  includes **CAP_NET_ADMIN, CAP_NET_RAW, CAP_SYS_ADMIN** (plus NET_BIND_SERVICE,
  MKNOD, etc.), and `mm-link` is **setuid root**; its successful namespace
  creation demonstrates the effective capability path. `unshare --net` directly
  as uid 1000 still fails `Operation not permitted` (unprivileged user
  namespaces are disabled) — Stage 0 must rely on the mm-link setuid path, not
  on `unshare`.
- **Basic mm-link run:** `mm-link TRACE TRACE -- sh -c '...'` succeeds (rc=0);
  the child runs inside the emulated namespace and sees
  `ingress 10.0.0.2 peer 10.0.0.1/32` plus `lo`.
- **Trace format (verified from the shipped corpus README):** one line per
  delivery opportunity; the value is a **timestamp in ms from trace start**,
  repeated once per 1500-byte packet when multiple packets fit in a
  millisecond. A single-line fixed-rate file repeats by default; `--once`
  disables wrapping. (My first synthetic files conflated packet counts with
  timestamps; that was a harness error, corrected.)
- **`--once` EOF behavior (CORRECTED in §22.59; this probe note was wrong):**
  the trace EOF stops delivery and — contrary to this note — with `--once`
  `mm-link` **exits by itself at trace EOF** (rc=0, verified: a 2 s trace with
  a 30 s child returned at 2.11 s). At EOF it signals its child and waits for
  it to die; a child that defers the signal makes `mm-link` wait. Child output
  written after link teardown is not captured, so Stage 0 treats trace EOF as
  the measurement endpoint and uses the independent watchdog; see §22.59.
- **Log/timing:** `--uplink-log/--downlink-log` produce ASCII text (not pcap):
  header lines starting `#` (tool, trace path, command line, queue) followed by
  packet records of the form `<trace_ms> # <packet_bytes>` (e.g. `80 # 1504`);
  a 0.6 s burst trace with a small transfer produced ~2.4k records per
  direction. The ms timestamps are trace-relative, sufficient to design the
  canonical trace-time anchor together with the child's monotonic timestamps.
- **Cleanup / host safety:** `ip link`, `ip addr`, `tc qdisc`, and `ip netns`
  were identical before and after the runs; final state shows clean `lo`/`eth0`
  `noqueue` with no `mm-*` processes. The only stray process was a local test
  `python3 -m http.server` started for the logging probe, which was stopped
  afterwards. No host-level networking changes were made.
- **Stage 0 operational prerequisites now resolved:** mm-link execution,
  namespace/child execution, `--once` EOF characterization (with the
  post-EOF-output caveat), log/timing signals, and cleanup. At the time of this
  re-verification the following were still pending: connection-lifecycle
  verification, incremental timestamped byte accounting, the logical
  endless-range server, the frozen trace-time anchor tolerance, and the
  epoch/tau/pilot protocol constants. Since then, connection lifecycle (§1–§3,
  §22.55–§22.56), byte accounting (§4, §22.57), and the logical workload (§5,
  §22.58) were implemented; the trace-time anchor and the epoch/tau/pilot
  protocol constants remain pending.

### 22.55 Pre-formalization item §1: dedicated HTTP/1.1-only fetcher transport

**Status: implemented and unit-tested (infrastructure only). This is NOT Stage
0 readiness; §2+ items (connection-reuse measurement, C↔flow mapping,
incremental byte accounting, logical workload, harness/watchdog, anchor) remain
unimplemented. No formal data was collected; frozen artifacts untouched.**

- **Change:** `fetcher.New` with `cfg.Client == nil` now builds a dedicated
  `*http.Transport` via `newHTTP1Transport()`; it no longer falls back to
  `http.DefaultTransport`. `http.DefaultTransport` is never mutated, and an
  explicitly supplied `cfg.Client` is still used as-is (verified by test).
  `Fetch` request semantics, timeouts, Range validation, retries, and error
  behavior are unchanged; the whole existing fetcher suite now exercises the
  new transport.
- **HTTP/1.1 enforcement mechanism:** Go 1.27's `http.Transport.Protocols` with
  `SetHTTP1(true)`, `SetHTTP2(false)`, `SetUnencryptedHTTP2(false)`. A non-nil
  `Protocols` set is authoritative (not `ForceAttemptHTTP2`), so HTTP/2 is also
  removed from TLS ALPN and HTTPS cannot negotiate h2. No HTTP/2, h2c, QUIC,
  multiplexing, multipart or multi-source behavior is introduced.
- **Pool settings (Cmax = 16):** `MaxIdleConns = 16`,
  `MaxIdleConnsPerHost = 16` (`maxIdleConnsPerHost` const), keep-alives
  enabled (`DisableKeepAlives` false), `IdleConnTimeout = 90s`; dial (30s /
  30s keepalive), TLS handshake (10s) and Expect-continue (1s) timeouts mirror
  the net/http defaults. This removes the stdlib `MaxIdleConnsPerHost` default
  of 2, which caused connection churn for C > 2.
- **Tests** (`internal/fetcher/transport_test.go`): real plaintext request
  returns `Proto 1.1` with `resp.Close == false`; real request against an
  h2-enabled `httptest` TLS server (asserted to offer `h2`) is still observed
  server-side as HTTP/1.1 and never negotiates ALPN `h2` (the test also sets a
  custom `TLSClientConfig`, proving `Protocols` still wins); transport config
  assertions (HTTP/1-only set, keep-alive, pool ≥ 16); dedicated transport is
  not `http.DefaultTransport` and does not mutate it; caller-supplied client
  identity/transport/Timeout left untouched.
- **Results:** dedicated transport tests pass; `go test ./...`, `-race`,
  `-count=5` on the package, `gofmt -l .`, `go vet ./...`, `go build ./...` all
  clean.
- **Not claimed:** connection reuse and the C→flow mapping were *not* verified
  in §22.55; they were subsequently measured locally in §22.56. Do not describe
  C as "number of connections" without the §22.56 qualification (`C` is the
  worker count and the upper bound on simultaneous HTTP/1.1 flows).

### 22.56 Pre-formalization items §2–§3: connection reuse and C→flow mapping (local, verified)

**Status: `CONNECTION_MODEL_READY` for the local deterministic model. Measured
against local `httptest` range servers, NOT over mm-link; this is not Stage 0
data, no formal run was performed, and §4 was subsequently implemented in
§22.57 (logical workload in §22.58; harness/EOF/watchdog in §22.59; trace-time
anchor remains unimplemented). No frozen artifacts touched. No production code
was changed for this measurement.**

- **Instrumentation (test-only):** client-side `httptrace.ClientTrace`
  (`ConnectStart`, `GotConn` with `Reused`/`WasIdle` and the actual
  `net.Conn`'s `LocalAddr`) is attached to the request context, which
  `Fetcher.Fetch` already forwards to `http.NewRequestWithContext`; server-side,
  `http.Server.ConnContext` assigns a stable integer identity per accepted TCP
  connection and the handler records conn id, protocol, remote address and
  Range per request. `http.DefaultTransport` and production code are untouched.
- **Fetcher reuse (8 sequential 256-byte ranges, one Fetcher):** exactly
  **1 TCP dial** (`ConnectStart`), first `GotConn` `Reused=false / WasIdle=false`,
  the other 7 `Reused=true / WasIdle=true`, all with the **same client local
  address**; server saw **1 connection** for all 8 requests, every request
  `HTTP/1.1`, and each server-observed remote address equalled the client
  connection address (independent cross-check). No churn.
- **C→flow measurement:** deterministic barrier server holds each request until
  all C workers have one in flight (no sleeps); per C, `ChunkSize=1024` and
  `rangesPerWorker=8` gave `requests=8C`. Representative run:
  | C | max in-flight requests | max active TCP conns | total TCP conns | requests | conns used >1× | max requests/conn |
  |---|------------------------|----------------------|-----------------|----------|-----------------|-------------------|
  | 1 | 1 | 1 | 1 | 8 | 1 | 8 |
  | 2 | 2 | 2 | 2 | 16 | 2 | 8 |
  | 4 | 4 | 4 | 4 | 32 | 4 | 9 |
  | 8 | 8 | 8 | 8 | 64 | 8 | 11 |
  | 16 | 16 | 16 | 16 | 128 | 15 | 16 |
- **Verified statement:** under the current downloader architecture and the §1
  transport, configured `C` yields **up to C simultaneous persistent HTTP/1.1
  flows**: max simultaneous in-flight Range requests and max simultaneously
  active TCP connections both equal C for every measured C∈{1,2,4,8,16};
  max in-flight requests on one connection is always **1** (HTTP/1.1, no
  multiplexing); each completed Range returns its connection to the shared idle
  pool and subsequent ranges reuse pooled connections, so total TCP connections
  created remained **exactly C** (no idle-pool churn, including C=16 where the
  §1 `MaxIdleConnsPerHost=16` matters).
- **Worker↔connection relation (actual observed semantics):** workers are not
  bound to connections. All workers share the downloader's single
  `*http.Client`; the transport assigns any idle pooled connection to the next
  request, so a worker may be served by different connections over time and a
  connection may serve several workers. Per-connection request counts are
  therefore uneven (C=16 above: one connection served a single request while
  another served 16). `C` is a worker/concurrency bound, not a
  connection-ownership property.
- **Limitations:** measurements are local (loopback `httptest`), synthetic, and
  retry-free; they do not cover the mm-link path, loss/RTT effects, or
  connection death/re-dial under failure. Worker-level connection migration is
  not observable on the wire (HTTP requests carry no worker id); the statement
  above is based on server-side connection identity plus the single shared
  client/pool structure.
- **Tests:** `internal/fetcher/reuse_test.go` (one-connection reuse with
  client+server cross-check), `internal/downloader/flow_test.go`
  (`TestConcurrencyToFlowMapping`, C∈{1,2,4,8,16}, deterministic barrier).
  Results: focused tests pass with `-count=5`; `go test -race` on both packages
  clean; `go test ./...`, `gofmt -l .`, `go vet ./...`, `go build ./...` clean.

### 22.57 Pre-formalization item §4: incremental timestamped byte accounting

**Status: `BYTE_ACCOUNTING_READY`. Implemented and tested (infrastructure
only). Bytes are attributed at the instant of the body read, not at request
completion. This is NOT Stage 0 readiness: §5 was subsequently implemented in
§22.58 and the harness/EOF/watchdog in §22.59; the trace-time anchor,
epoch/tau/pilot, and formal Stage 0 remain unimplemented. No frozen artifacts
touched; fetch semantics are unchanged when the observer is nil.**

- **Hook location:** `internal/fetcher/bytes.go` — `countingReader` wraps
  `resp.Body` immediately before the existing `io.LimitReader`/`io.ReadAll`
  call in `Fetcher.Fetch`. The wrapper delegates `Read` unchanged and, when a
  read returns n > 0, emits one `ByteEvent` **after the underlying read and
  before the n/err pair is returned**. Fetch's Range handling, validation,
  retries, timeouts, and error behavior are untouched; the wrapper and the
  trace are only installed when `Config.ByteObserver != nil`.
- **Event record:** `ByteEvent{Time, RequestID, ConnID, Delta,
  RequestCumulative, AggregateCumulative}`. `Time` is `time.Now()` taken in the
  read path (monotonic reading); `Delta` > 0; `RequestCumulative` is the bytes
  read so far for that request; `AggregateCumulative` is stamped in append order
  by the meter.
- **Aggregate accounting:** `fetcher.ByteMeter` (concurrency-safe
  `ByteObserver`, events retained in memory) implements the stream;
  `Snapshot()` returns append-order events and `Total()` the cumulative count.
  Later Stage 0 code can reconstruct `bytes_received(t_start, t_end)` by
  summing deltas whose timestamps fall in the interval. No epoch logic is
  implemented here. There is **no global counter**: the meter is owned by the
  caller (per download/experiment) and `downloader.Config.ByteObserver` passes
  it through to the fetcher.
- **Per-connection identity:** when the observer is configured, Fetch attaches
  an `httptrace.ClientTrace` to the request context and captures the actual
  connection's `LocalAddr` from `GotConn`; `ConnID` is that stable
  per-TCP-connection value (the §2/§3 instrumentation). `WithClientTrace`
  composes with traces already in the context. With no observer, no trace is
  attached.
- **Read/error/EOF semantics:** `(n>0, err)` counts n before the error is
  returned; `(n>0, io.EOF)` counts; `(0, err)` creates no event. A response
  that fails after partial delivery keeps its bytes (verified: 50 of a declared
  100 counted, `Result.Bytes == 50`, the error is preserved). For successful
  requests the sum of deltas equals `Result.Bytes` and the requested range
  length.
- **Timestamps:** `ByteMeter.ObserveBytes` clamps an event timestamp earlier
  than the previous event's to the previous timestamp, so the append-ordered
  stream is monotonically nondecreasing even when concurrent reads complete
  slightly out of scheduler order; per-request read timestamps are otherwise
  preserved. Request completion time is never used for byte attribution.
- **Concurrency:** shared state is under the meter's mutex; the connection
  holder uses `atomic.Pointer`; observations happen synchronously in each
  request's read goroutine, so workers only briefly share the append critical
  section. Verified race-clean at gated C=4 and with simultaneous client
  requests.
- **Tests:** `internal/fetcher/bytes_test.go` — two-interval attribution
  (bytes in A > 0 and B > 0, sum = body, request completes only after B),
  nonzero bytes with zero completed requests, wrapper `(n>0, err)` and
  `(n>0, io.EOF)` cases, failed response retaining partial bytes, totals vs
  `Result.Bytes`, monotonic timestamps, and concurrent-request
  identification/connection separation; `internal/downloader/byteaccounting_test.go`
  — C=4 gated download: aggregate total, per-request totals cross-checked
  against the metrics attempt records, per-request cumulative monotonicity,
  exactly 4 distinct connection identities, output byte-match. Results: focused
  tests pass with `-count=5`; `go test ./...`, `go test -race
  ./internal/fetcher/... ./internal/downloader/...`, `gofmt -l .`,
  `go vet ./...`, `go build ./...` all clean.
- **Remaining for §5+:** the logical workload (§22.58) and the mm-link
  harness/EOF/watchdog (§22.59) were subsequently implemented; the trace-time
  anchor and calibration, epoch construction, real trace corpus use, tau,
  oracle/DP, and RL were not started.

### 22.58 Pre-formalization item §5: logical bounded-memory HTTP Range workload

**§5 logical workload: READY.** Implemented and tested (experiment
infrastructure only). This is NOT Stage 0 readiness: the harness/EOF/watchdog
were subsequently implemented in §22.59; trace-time anchor and calibration,
epoch/tau/pilot, and formal Stage 0 remain unimplemented. No frozen artifacts
touched; existing finite-server behavior is unchanged.

- **Representation:** `internal/experiment/server/logical.go` adds a
  `contentSource` abstraction (`size()` + `write(ctx, w, start, end, rate)`).
  `New([]byte)` is unchanged: it still copies into a private `[]byte`
  (`finiteContent`) and serves through the original `writeThrottled` call.
  `NewLogical(size int64)` returns a `Server` backed by `logicalContent`, a
  stateless generator; no `make([]byte, size)` is ever created. The
  experiment-only logical source is opt-in.
- **Configuration / size:** the logical size is explicit per server
  (`NewLogical(size)`; `size > 0` required, else `ErrInvalidLogicalSize`). The
  package exposes `DefaultLogicalSize = 1<<44` (16 TiB) as the recommended
  Stage 0 full-trace envelope. Rationale (a safety envelope, not a performance
  result): the longest native traces are ~82 min (~4920 s), so even an absurd
  sustained 2 GiB/s replay for two hours (~14.1 TiB) does not exhaust it. The
  later harness may pass any explicit size and must request contiguous 1 MiB
  ranges until trace EOF.
- **Deterministic content:** each absolute byte offset maps to a byte via the
  splitmix64 finalizer over its 8-byte word index (`mixLogicalWord` /
  `fillLogical`), so the same offset always yields the same byte regardless of
  reading order, grouping, alignment, or prefix size, and different offsets
  vary. The generator is stateless, O(1) memory, and direct random access:
  generating `[start,end]` costs O(end-start+1) time and never touches the
  prefix. It is a content generator, not a cryptographic primitive.
- **Range/HTTP semantics:** shared with the finite server through one
  `ServeHTTP` path: single contiguous ranges only (no multipart/disjoint);
  `start >= size`, `end < start`, malformed, suffix, or multi-range requests →
  **416** with `Content-Range: bytes */SIZE`; an `end` beyond the object is
  clamped to `size-1` (existing finite semantics); a valid range returns
  **206** with exact `Content-Range` and `Content-Length`; a no-Range request
  returns **200** with the full logical object (streamed). Delays, rate
  schedules, per-range delays, failure injection, request log, and stats behave
  identically to the finite server.
- **Streaming / bounded memory:** logical bodies are generated and written in
  16 KiB chunks (matching `writeThrottled`'s pacing granularity) using a
  per-request buffer, so serving an N-byte range allocates 16 KiB + O(1), not
  N.
- **Tests** (`internal/experiment/server/logical_test.go`): construction
  validation; `DefaultLogicalSize`; small/large-offset/adjacent/overlapping/
  single-byte/end-reaching/clamped ranges with exact Content-Length and
  Content-Range; deterministic repeat, offset variation, offset stability and
  adjacent composition; invalid ranges → 416 with `bytes */SIZE`; no-Range 200
  full object; constructing a 16 TiB object allocating < 1 MiB; serving a
  64 MiB range at offset ~4 TiB allocating < 1 MiB; generator
  offset-stability across grouping/alignment; finite-server private-copy and
  semantics regression. Exact results: focused tests pass; the package passes
  with `-count=5`; `go test ./...`, `go test -race ./...`, `gofmt -l .`,
  `go vet ./...`, `go build ./...` all clean.
- **Not claimed / remaining:** the logical object is finite and sufficiently
  large, not mathematically infinite; no trace anchor or epoch pilot exists
  (the Mahimahi harness/watchdog now exists, §22.59); no formal Stage 0 data
  was produced; `RangeTracker`/downloader behavior is untouched.

