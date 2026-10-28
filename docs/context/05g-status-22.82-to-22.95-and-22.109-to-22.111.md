### 22.82 Salvage Protocol v7 parameter-resolution pass (documentation only, 2026-09-28): proposals recorded, still BLOCKED

**Status: `FINAL_S_PROTOCOL_FROZEN — BLOCKED_PENDING_HUMAN_PARAMETERS`
(unchanged).** A specification-resolution pass evaluated coherent candidate
mechanism packages, produced exact analytic proposals for every previously
missing rule, and resolved the two task-directed items. **Nothing was
approved or frozen except the two task-directed resolutions; no experiment was
run.** New artifact: `PARAMETER_DECISION_MEMO.md` in
`testdata/salvage_s_protocol_v7/` (the JSON/TXT/README were updated to match).

- **Candidate packages (analytically evaluated, none approved):**
  A `R=4 Mbps, D=40 ms, FileSize=512 MiB, N=4` → `T_ref` 268.4355 s,
  `T_tail_primary` 33.5944 s (admissible);
  B `R=8 Mbps, D=40 ms` → `T_ref` 134.2177 s, `T_tail_primary` 16.8172 s
  (**inadmissible: tail < 20 s**);
  C `R=4 Mbps, D=80 ms` → `T_ref` 268.4355 s, `T_tail_primary` 33.6344 s
  (admissible);
  D (project precedent: `matrix.go` server-throttle 512 KiB/s + 20 ms latency)
  `R=512 KiB/s, D=20 ms` → `T_ref` 256.0000 s, `T_tail_primary` 32.0200 s
  (admissible). Tail-window data = `N × C_max` = 64 MiB (12.5 % of the file);
  the memo has per-arm service-time/efficiency tables.
- **Proposed (pending approval):** `T_tail_primary = D + C_max/R`;
  `delta_tail = 0.05·T_tail_primary`; trigger
  `B_switch = N × S0` (64/16/4 MiB for 16→4 / 4→1 / 1→256K) evaluated
  immediately before allocation with the frozen one-switch/edge-case
  semantics; Stage 0S go/no-go rule (mechanism presence within ±25 % of
  `D + s/R`, strictly increasing per-arm service times, realized 16 MiB tail
  ≥ 20 s, reliability/variance feasibility); SSR grid (3 effects × 2
  `sigma_block` × 3 `sigma_resid` = 18 scenarios × 1000 datasets).
- **`delta_T` interpretation (recommendation only):** use the log-scale
  threshold `delta_log = log(0.95) = −0.051293294` in
  `P(Δ_k ≤ delta_log | data) ≥ 0.95` to preserve the intended statement
  "a 5 % reduction in completion time"; the literal `−0.05` alternative is
  recorded but not chosen. Not approved.
- **Resolved (task-directed, frozen):** the duplicated exploratory switching
  case (`1 MiB → 256 KiB`) is removed — v7 defines no exploratory switching
  condition; the fixed-sequence order is frozen as `16→4`, `4→1`, `1→256K`
  with stop-at-first-failure semantics and no ranking/best-policy conclusion.
- **Validation record now:** 13 PASS, 2 PROPOSED-pending-approval,
  3 BLOCKED (the blocked checks are the unapproved core parameters, the
  `delta_T` interpretation, and the package-dependent `T_tail_primary ≥ 20 s`
  check). The remaining human decisions are the six listed in §22.82's memo
  and the protocol JSON (`remaining_human_decisions`).
- **No experiment:** no mm-link, server/client, network replay, pilot, or new
  experiment directory; no source change; no frozen oracle/Stage A/B/C
  evidence change. Stage 0S must not start until all six decisions are
  explicitly approved and the validation is re-run.

### 22.83 Salvage Protocol v7 final human decision (documentation only, 2026-09-28): FROZEN and READY_FOR_STAGE_0S

**Status: `FINAL_S_PROTOCOL_FROZEN — READY_FOR_STAGE_0S`
(`ready_for_stage_0s = true`).** The human approved every outstanding decision;
the protocol artifacts were updated to the frozen state. No experiment was run
and Stage 0S execution remains a separate task.

- **Frozen mechanism package (Candidate A):** `R = 4 Mbps = 500,000 B/s`,
  `D = 40 ms`, `FileSize = 512 MiB = 536,870,912 B`, `N = 4`. Candidates
  B/C/D are unselected and retained in `PARAMETER_DECISION_MEMO.md` for
  provenance (B analytically inadmissible: tail < 20 s).
- **Frozen quantities:** `T_ref = 268.435456 s`;
  `delta_T = 0.05·T_ref = 13.4217728 s` (time-scale statement, a 5 %
  completion-time reduction); `delta_log = log(0.95) = −0.05129329438755058`
  (the primary decision uses this exact log-scale threshold; `−0.05` is
  explicitly **not** used); `T_tail_primary = D + C_max/R = 33.594432 s`;
  `delta_tail = 0.05·T_tail_primary = 1.6797216 s`; tail minimum
  `T_tail_primary >= 20 s` satisfied; tail-window data `N·C_max = 64 MiB`
  (12.5 % of the file).
- **Arithmetic note (transcription):** the decision text stated
  `T_tail_primary = 33.594304 s`; independent recalculation of the frozen
  formula gives `33.594432 s` (0.128 ms difference). Per the section-15
  independent-recalculation requirement the formula-derived value is operative;
  the stated decimal is retained for provenance. Both satisfy the ≥ 20 s
  requirement.
- **Other frozen decisions:** arms 256 KiB / 1 MiB / 4 MiB / 16 MiB;
  confirmatory strategies `16→4`, `4→1`, `1→256K` with **no** exploratory
  strategy; trigger `B_pending = TotalSize − CompletedBytes − InFlightBytes`
  evaluated immediately before allocation with `B_switch = N·S0` =
  64/16/4 MiB, one switch maximum, in-flight ranges never resized; Bayesian
  model/priors/sensitivity unchanged; `mu_fixed*` model-based; decision
  `P(Δ_k ≤ delta_log | data) ≥ 0.95`; fixed sequence `16→4`, `4→1`, `1→256K`
  with stop-at-first-failure; Stage 0S 32-run pilot (8 blocks × 4 arms) for
  feasibility only with the frozen GO/NO-GO rule; confirmatory `n ≤ 12`; SSR
  grid 18 scenarios × 1000 datasets = 18,000; pilot data not pooled; trace
  validation out of scope.
- **Validation:** 19/19 checks PASS (including the independent recalculation
  and cross-artifact consistency); all six human decisions approved; no
  blocking items remain.
- **Artifacts:** `testdata/salvage_s_protocol_v7/` — `final_s_protocol.json`,
  `final_s_protocol.txt`, `PARAMETER_DECISION_MEMO.md`, `README.md`.
- **Safety:** no mm-link/server/client/network replay, no pilot, no new
  experiment directory, no source change, no frozen oracle/Stage A/B/C
  evidence change. Stage 0S execution must not start from this task.

### 22.84 Stage 0S pilot execution (executed 2026-09-28): GO

**Status: `STAGE_0S_COMPLETE — GO`.** The frozen 32-run Stage 0S feasibility
pilot ran under the frozen mechanism; all four pre-specified criteria pass, so
the confirmatory stage is justified by the protocol. **This is not a
confirmatory adaptive-effect result** — no switching strategy was run, no
winner is reported, and no chunk-size-adaptation benefit is claimed. Window:
2026-09-28 10:19 → 12:52 UTC. Artifacts:
`/workspaces/mvp/testdata/salvage_s_stage0s/`.

- **Design executed:** 8 replicate blocks × 4 fixed arms (256 KiB, 1 MiB,
  4 MiB, 16 MiB) = **32/32 planned, 32 attempted, 32 clean, 0 technical
  failures, 0 reruns** (the frozen protocol contains no technical-rerun
  authorization, so none were performed). Randomized order within each block
  (seed 20260928, recorded in `manifest/manifest.json`); sequential execution;
  fresh server/client state per run.
- **Mechanism:** `R = 500,000 B/s`, `D = 40 ms`, `FileSize = 512 MiB`,
  `N = 4`; fixed chunk size per run (no switching). Reused existing
  infrastructure: `internal/experiment/server.NewLogical` (deterministic
  generated content, bounded memory — essential because the host had only
  ~0.7 GB RAM available; identical range semantics and application-level
  pacing to the materialized server) and `internal/downloader` (fixed mode) +
  `internal/metrics` per-attempt records. HTTP/1.1; no tc/netem.
- **Criterion 1 — mechanism presence: PASS.** Pooled median server-side
  per-request service time vs `D + s/R`: 256 KiB 0.577717 s (+2.38 %),
  1 MiB 2.190526 s (+2.50 %), 4 MiB 8.640930 s (+2.52 %), 16 MiB 34.427871 s
  (+2.48 %) — all within ±25 %.
- **Criterion 2 — direction: PASS.** Strictly increasing medians
  0.577717 < 2.190526 < 8.640930 < 34.427871.
- **Criterion 3 — tail sufficiency: PASS.** Realized 16 MiB-arm tail
  (client end − start of the first request in the final 64 MiB region) pooled
  median **34.977589 s** ≥ 20 s and within
  `[0.8·T_tail_primary, 1.25·T_tail_primary] = [26.8755456, 41.99304] s`.
- **Criterion 4 — feasibility/reliability: PASS.** Clean runs 32/32 (≥ 28);
  maximum wall-clock 296.629 s (≤ 1800 s); within-block residual SD of
  `log(T/T_ref)` 0.000795 (≤ 0.25), df = 21.
- **Formal decision:** all four criteria PASS → **GO**. A GO means only that
  the frozen mechanism and finite-file tail were sufficiently realized under
  the pre-specified feasibility criteria to justify the next pre-specified
  stage.
- **Artifacts:** `manifest/` (manifest + run ledger), `raw/` (32 raw run
  artifacts with client records and server request logs), `per_run/` (32
  per-run summaries), `analysis/` (`stage0s_analysis.json`, `per_run.csv`,
  `per_arm.json`, `direction.json`, `tail.json`, `reliability.json`,
  `criteria.json`), `final_report/stage0s_report.md`, `logs/`, `tools/`
  (`stage0s_run.go`, `run_pilot.py`, `analyze_stage0s.py`,
  `preflight/preflight_smoke.go`).
- **Integrity/safety:** no repository source file changed (execution tools
  live under `testdata/`, which Go tooling ignores); the frozen protocol and
  all oracle/Stage A/B/C evidence unchanged; no mm-link/experiment processes
  or qdisc/netns residue remain; no SSR simulation or confirmatory Bayesian
  analysis was run.
- **Next:** the confirmatory Stage 1 is justified under the protocol but
  **not started**; it requires a separate task with fresh data and the frozen
  arms/strategies/trigger/priors/effect threshold/analysis.

### 22.85 Pre-Stage-1 file-size scaling / headroom analysis (analytical simulation only, 2026-09-28)

**Status: `STAGE1_SCALING_ANALYSIS_COMPLETE — ANALYTICALLY INSUFFICIENT
UNDER FROZEN MECHANISM`.** A deterministic analytical simulation asked whether
increasing the file size can increase the **relative** completion-time
headroom available to chunk-size adaptation under the frozen static
rate + RTT mechanism. **No empirical network experiment was run.** Artifacts:
`testdata/salvage_s_scaling_analysis/`.

- **Method:** deterministic work-conserving finite-file scheduler simulator of
  the frozen semantics (N=4, contiguous ranges, future-only chunk size,
  `B_pending = TotalSize − CompletedBytes − InFlightBytes` sampled immediately
  before each allocation, one switch, no in-flight resizing, final partial
  range, EOF), under two never-blended service models: MODEL A `t(s)=D+s/R`
  and MODEL B `t(s)=a+b·s` (OLS fit to the four frozen Stage 0S pooled median
  service times; `a=0.041837515 s`, `b=2.049600672e-6 s/byte`,
  R²=0.9999999885). Grid 64 MiB–32 GiB (plus a dense 512–640 MiB residue
  scan). Model B reproduces the Stage 0S observed fixed-S medians within
  0.04–0.35 %; Model A underestimates them by ~2.5 %.
- **Fixed-S scaling:** for exact full bursts `T_S/T_ref = 1 + R·D/s` is
  scale-invariant (+0.119 % for 16 MiB, +0.477 % for 4 MiB, +1.907 % for
  1 MiB, +7.629 % for 256 KiB), and the model-implied lowest completion-time
  fixed condition is 16 MiB on the entire grid.
- **Frozen strategies:** all relative gains are **negative** on the entire
  grid — 16→4 max −0.0007 %, 4→1 max −0.3579 %, 1→256K max −1.7867 %
  (Model A; Model B qualitatively identical). At the frozen 512 MiB baseline:
  −0.0447 %, −0.4019 %, −1.8307 %. The frozen trigger `B_switch = N·S0` fires
  exactly at a full final burst, where replacing S0 chunks with S1 chunks adds
  per-request D overhead without shortening the tail.
- **Exploratory single-switch upper envelope (not the frozen policy):** ≤ 0 on
  the entire main grid (aligned power-of-two files have no tail-packing
  defect); the dense residue scan reaches at most **+0.3545 %** (≈ the
  16 MiB→4 MiB bulk overhead difference), far below the frozen 5 % threshold.
- **Answers:** (A) increasing F does **not** increase relative headroom — the
  bounded one-time tail effect is diluted (relative tail share falls as 1/F)
  and the frozen strategies converge to their negative bulk-overhead
  asymptotes; (B) absolute savings are ≤ 0 for the frozen strategies (the
  exploratory envelope can show a growing absolute saving at a fixed ~0.36 %
  relative gain); (C) no frozen strategy reaches 5 % anywhere on the grid;
  (D) the exploratory envelope does not reach 5 % either; (E) the current
  frozen Stage 1 design is **not** mechanistically capable of a practically
  meaningful effect at 512 MiB.
- **Implication:** `STAGE1_IMPLICATIONS.md` states
  **ANALYTICALLY INSUFFICIENT UNDER FROZEN MECHANISM**. **No amendment is
  proposed** — a larger file size is not justified (it reduces relative
  headroom); any change to the mechanism, trigger timing, or design requires a
  separately frozen protocol decision. The frozen protocol was not modified.
- **Artifacts:** `manifest/manifest.json`; `model/model_definition.json`;
  `simulation/simulation_assumptions.md`; `analysis/` (`scaling_analysis.json`,
  `fixed_s.csv`, `frozen_switches.csv`, `envelope.csv`, `dense_modular.csv`);
  `figures/fig1..fig7`; `reports/scaling_headroom_report.md`;
  `STAGE1_IMPLICATIONS.md`; `tools/` (deterministic simulator, figures,
  report generator).
- **Safety:** zero empirical download runs, zero mm-link/server/client/tc
  processes, zero network replay; no source-code changes outside `testdata/`;
  the frozen protocol and all Stage 0S / oracle / Stage A/B/C artifacts were
  not modified.

### 22.86 Adaptive-S mechanism pivot screening (analytical simulation only, 2026-09-28): no plausible mechanism found

**Status: `MECHANISM_PIVOT_SCREENING_COMPLETE — NO PLAUSIBLE MECHANISM
(MECHANISTICALLY INSUFFICIENT FOR ALL SCREENED CANDIDATES)`.** With the static
rate + RTT mechanism closed for adaptive-S confirmation (§22.85), this study
asked under what realistic network/server mechanism the preferred chunk size
could change *during* a finite download. **No empirical network experiment was
run.** Artifacts: `testdata/salvage_s_mechanism_pivot/`.

- **Trace inventory:** 8 native Mahimahi **delivery-opportunity** traces
  (capacity schedules; mm-link loops at EOF) with strong genuine variability
  (p90/p10 = 5.5–27.4; 4–18 % of seconds below 25 % of the median), plus 23
  **throughput-trajectory** traces (NYU-METS/UGent) that are measured
  end-to-end throughput, **not** path capacity (the M3 envelope limitation
  applies), used only for context.
- **Analytical model:** deterministic event-driven finite-file scheduler
  simulator with four request-level service models — `shared` (aggregate
  capacity shared among in-flight requests; every request experiences every
  transition), `lockin_full` (per-request rate fixed at request start and held
  — the existing server's scheduled-rate semantics), `serialized`, and the
  closed v7 `constant` control. Traces are 1 s binned with the documented
  8000 bps M3 outage floor.
- **Moving-optimum test:** the preferred S does **not** change between
  stationary states. argmin_S over 0.25–8 MB/s aggregate is 16 MiB at every
  rate for the shared and lock-in models at 512 MiB (the 520/544 MiB variation
  is a file-size-residue effect) and a fixed 256 KiB for the serialized model;
  the real-trace low/high-rate regimes likewise give 16 MiB everywhere.
- **Causal vs stationary:** causal, observation-driven within-download
  switches are **zero or harmful** in every case (range −61.2 % … +0.0 %);
  under shared/serialized they never fire (no state-dependent preference), and
  under lock-in they fire only after volatility is visible and underperform the
  best fixed arm by 2–61 %. Every stationary-control causal gain is exactly
  0.000 %.
- **Headroom:** the only positive opportunity is a **clairvoyant** single
  switch in a lock-in stable→volatile case (oracle gains 0–32 %, varying with
  the alternation period: 0 % at 60 s, 7–23 % at 17 s, 11–32 % at 5 s). It
  requires perfect knowledge of the alternation phase and vanishes at longer
  periods — a fragile diagnostic artifact, not a causal opportunity. No
  screened mechanism provides a robust causal within-download S-switch
  headroom.
- **Classification (five-condition rule):** candidates A/B/C/D (shared
  time-varying capacity), E (server serialization / concurrency-dependent
  rate), and F (per-request lock-in + volatility) are all
  **MECHANISTICALLY INSUFFICIENT**; condition 1 (preferred S changes between
  states) fails for all, and causal adaptation (condition 3) fails or is
  harmful. **No new preregistered protocol is warranted** under the screened
  mechanisms; the frozen v7 protocol was not modified.
- **Additional evidence required before any future execution** (from the
  report §13): a documented/measured mechanism with a robust, reproducible
  state-dependent argmin (not residue- or phase-dependent); a pre-fixed causal
  policy capturing a material fraction of the oracle bound; a pre-declared
  eligibility rule over the trace population; and a **new** preregistered
  protocol at a new path.
- **Artifacts:** `inventory/` (trace inventory JSON/MD), `analysis/`
  (`screening.json`, `e1_stationary.csv`, `e2_transitions.csv`,
  `e2c_volatility.csv`, `e3_traces.csv`), `figures/fig1..fig7`,
  `reports/mechanism_pivot_report.md`, `manifest/manifest.json`, `tools/`
  (inventory, simulator, screening, figures, report), `model/`, `simulation/`.
- **Safety:** zero empirical download runs, zero mm-link/server/client/tc
  processes, zero network replay; no source changes outside `testdata/`; the
  frozen v7 protocol and all Stage 0S / scaling / oracle / Stage A/B/C
  artifacts were not modified.

### 22.87 Adaptive-N mechanism screen (analytical simulation only, 2026-09-28): no robust mechanism, no protocol justified

**Status: `ADAPTIVE_N_SCREEN_COMPLETE — NO ROBUST MECHANISM (NO PROTOCOL
JUSTIFIED)`.** With the S mechanism closed (§22.85–§22.86), this screen asked
whether adaptive concurrency N can have a genuine, causally exploitable
state-dependent optimum under a realistic download mechanism. **No empirical
network experiment was run.** Artifacts:
`testdata/adaptive_n_mechanism_screen/`.

- **Prior concurrency evidence (unchanged):** T1 (live per-response pacing) —
  aggregate throughput scales monotonically with C, no live reversal at equal
  1 MiB granularity; L3 — simulated oracle headroom 8.1 % (< 10 % gate);
  L4 — 17.6 % simulated headroom but the live sweep did **not** reproduce the
  over-concurrency penalty (`NOT_READY_LIVE_MISMATCH`, GATE E FAIL); M3 — no
  C sensitivity under the aggregate throughput envelope. These are decisive
  for this screen.
- **Trace inventory:** the 8 native delivery-opportunity traces represent
  time-varying **aggregate capacity** only; the 23 throughput-trajectory
  traces are measured throughput, not capacity (M3 limitation). **No existing
  trace contains per-flow fairness, queueing, or per-connection cost
  information**, so no trace can establish a concurrency-sensitive mechanism.
- **Models screened** (deterministic event-driven simulator, F = 512 MiB,
  S = 1 MiB fixed, RTT 40 ms, N ∈ {1,2,4,8,16}): `direct` (T1 control),
  `fair` (aggregate C shared), `fair_bg` (background flows B), `cap` (server
  parallelism K), `degrade` (server aggregate capacity C/(1+γ(k−1))).
- **Stationary optima:** `direct`, `fair`, `fair_bg` are monotone (preferred
  N = 16 at every state); `cap` has a state-dependent preferred N but a flat
  plateau (the model-implied best fixed N is already robust; oracle headroom
  0 %); `degrade` has a true interior optimum whose preferred N rises with C
  (1 → 4 across 0.5–32 MB/s) and shifts with γ, and the state-dependence
  survives the entire predeclared C × γ grid.
- **Transitions and causality:** causal, observation-driven single switches
  (median of the last 3 completed-request throughput samples) give gains of
  **−128.5 % … +6.66 %** (median +1.50 %); the coarse clairvoyant oracle gives
  0 % … +18.69 %; every stationary-control gain is ~0. Causal ≥ 5 % occurs in
  **2/10** transition cases, all in the `degrade` model at severe capacity
  drops (up to +6.66 %); the causal/oracle capture is inconsistent
  (near 100 % at severe `degrade` drops, 0.5 % vs 17.4 % in the γ transition).
- **Classification:** A (per-flow sharing), B (server cap), D (background
  traffic), F (server saturation), G (changing capacity + sharing) are
  **MECHANISTICALLY INSUFFICIENT** (monotone or plateau; no causal gain).
  C/E/H (concurrency-dependent server cost / queueing) are **PLAUSIBLE —
  FRAGILE**: they pass the six analytical ROBUST conditions but fail physical
  grounding — the only live validation of this mechanism class (L4 GATE E)
  rejected it, and the current controlled server has no concurrency-dependent
  capacity cost; the causal capture is also severity-dependent.
- **Conclusion:** no mechanism reaches **MECHANISTICALLY PLAUSIBLE — ROBUST**
  in the current infrastructure, so **no new empirical protocol is justified**
  and no adaptive-N experiment was run. The exact requirements for any future
  protocol are in the report §15: establish the mechanism live first (the
  current stack does not), ground the parameters by measurement (never tune),
  freeze a causal policy (one adjacent-step switch, observed-throughput
  trigger, pre-registered N mapping), pre-register the model-implied best
  fixed N baseline and the 5 % reference, and use fresh evidence at a new
  path.
- **Artifacts:** `analysis/` (`screening.json`, `e1_stationary.csv`,
  `e2_transitions.csv`, `e3_robustness.csv`, `e4_severity.csv`),
  `figures/fig1..fig8`, `reports/adaptive_n_mechanism_report.md`,
  `inventory/n_trace_inventory.md`, `mechanisms/`, `model/`, `simulation/`,
  `manifest/manifest.json`, `tools/` (simulator, screening, figures, report).
- **Safety:** zero empirical download runs, zero mm-link/server/client/tc
  processes, zero network replay; no source changes outside `testdata/`; all
  frozen S/oracle/Stage A/B/C and historical concurrency evidence unchanged.

### 22.88 Final project evidence synthesis / paper-viability audit (documentation only, 2026-09-28)

**Status: `PAPER-READY — MECHANISTIC / NEGATIVE-RESULT STORY`.** A synthesis
of the completed evidence only (zero new experiments) determined what the
project actually establishes and what the final paper may claim. Artifacts:
`testdata/final_project_synthesis/` (six reports, seven final figures, and a
manifest).

- **Final research question:** under realistic, live-grounded mechanisms, when
  does adaptation of HTTP range concurrency (N) or future chunk size (S) have
  sufficient **causal headroom** to justify an adaptive policy at all — and
  what mechanism must first exist for that headroom to be real? This is the
  original proposal's scope (adaptive N/S, ablations, joint, policies, RL,
  controlled server, aria2 baseline) narrowed to a mechanism-first gate.
- **Strongest contribution:** a pre-registered, mechanism-first methodology for
  determining whether adaptive HTTP range scheduling has sufficient causal
  headroom **before** introducing policy complexity — supported by the S and N
  closure chains and the frozen negative results.
- **Evidence chains (unchanged, synthesised):** S — static rate+RTT → Stage 0S
  GO → frozen switching rules non-positive → file-size scaling does not rescue
  relative gain → broader S screening finds no robust causal opportunity. N —
  direct throttle monotone → M3 envelope no C sensitivity → alternative
  analytical mechanisms → some create an interior N optimum → those fail live
  physical grounding (L4 GATE E; T1) → no robust adaptive-N mechanism.
- **Evidence-type discipline:** every claim is mapped to LIVE EXPERIMENTAL,
  TRACE REPLAY, DETERMINISTIC ANALYTICAL MODEL, SIMULATION, or
  DIAGNOSTIC/EXPLORATORY. The analytical +6.66 % N headroom is explicitly not
  empirical; oracles are not policies; the S envelope is not a measured
  speedup; the aria2 runs are empirical only for their tested configurations.
- **RL status:** **deferred, not abandoned** — no robust causal N/S opportunity
  exists to optimize; the executed RL results were negative/descriptive (DQN
  v2 75.7 % slower than aria2, ratio 1.757 [1.669, 1.850]; v5 2.32× slower).
  Revisit only after a live-grounded mechanism is established.
- **aria2 status:** fairly configured (calibrated v1/v2); the granularity-
  matched wall-clock control differs by +3.6 % (n=10); the final head-to-heads
  are negative. No ranking or general comparison claim is supported.
- **Remaining evidence gap:** no live-grounded mechanism creates a robust,
  causal, state-dependent optimum for N or S in the controlled environment.
  Additional empirical work is **not** required for the negative-result paper;
  any future positive study must first satisfy the five conditions in
  `reports/future_work_boundary.md` (live-grounded mechanism, measured
  parameters, pre-registered causal policy, pre-registered baseline/target,
  fresh evidence at a new path).
- **Reports:** `reports/final_evidence_matrix.md`,
  `reports/paper_viability_audit.md`, `reports/figure_selection.md`,
  `reports/proposal_alignment.md`, `reports/claims_we_can_make.md`,
  `reports/future_work_boundary.md`; final figures
  `figures/fig1_system_methodology.png` … `fig7_practical_baseline_evidence.png`;
  `manifest/manifest.json`.
- **Safety:** zero new experiments; no downloads, mm-link, server/client,
  tc/netem, or replay; all historical evidence and frozen protocols unchanged.

### 22.89 aria2-competitive benchmark (executed 2026-09-28): WIN REPRODUCED

**Status: `ARIA2_COMPETITIVE_WIN_REPRODUCED`.** A memory-safe replication
(deterministic logical server, identical per-response pacing semantics) of the
historical D2 workload established, through predeclared discovery blocks and a
fresh 30-pair final block, that our fixed-policy downloader (C=16, 4 MiB
chunks) reproducibly beats the strongest aria2 1.37.0 configuration under the
frozen D2/512 MiB workload family.

- **Workload (frozen):** 512 MiB (536,870,912 B); per-response pacing sampled at
  request start and held: 640 KiB/s (0–2 s) -> 160 KiB/s (2–4 s) -> 2500 KiB/s
  (>=4 s); tc/netem on `lo`, 40 ms latency + 0.4% loss; identical for both
  systems.
- **D2 reproduction (20 fresh pairs, historical configs):** v6 mean 27.96 s vs
  aria2-F 53.45 s; geo ratio **0.5230** CI95 [0.5200, 0.5261], 20/20. The
  historical result (ratio 0.5816 [0.566, 0.598]) is reproduced in direction and
  magnitude class.
- **aria2 frontier (discovery; actual served ranges recorded):** split/max-conn
  1/2/4/8/16 -> median 833/419/210/106/53 s; strongest is **split=16,
  max-connection-per-server=16, min-split 1M** (15 x 32 MiB pieces, all starting
  <2 s). The min-split dimension is degenerate at 512 MiB for split <= 16
  (spot-checked 1M vs 32M). Not a strawman: the full split frontier was run and
  16 is the strongest.
- **Our frontier (discovery; 40-config grid C in {1,2,4,8,16} x chunk in
  {256K,512K,1M,2M,4M,8M,16M,32M}):** best is **C=16, chunk 4 MiB**; corrected
  3-run median **19.99 s** (the driver bookkeeping bug recorded the 2-run median
  20.30 s; the frozen selection is top-1 under both; top-3: 4M 19.99, 8M 24.52,
  512K 24.53 s).
- **Final block (30 fresh pairs, frozen configs, predeclared order):** ours mean
  20.12 s / median 20.02 s vs aria2 mean 53.56 s / median 53.32 s; geometric
  ratio **0.3756** CI95 [0.3728, 0.3785]; relative improvement **62.4%**;
  **30/30 wins**; paired sign-flip permutation p ~ 5e-5; all 30 runs
  content-verified; zero config changes during the block.
- **Mechanism:** totals over the 30 runs: ours issues 3840 ranges of 4 MiB (480
  start <2 s, 3360 >=4 s) = 128 per run; aria2 issues 450 ranges of 32 MiB plus
  30 unranged probes (480 requests; 16 per run), all starting <2 s at the
  640 KiB/s phase and held there (~51 s each). (Corrected 2026-10-11: the earlier
  wording read as per-run counts.) The win is a **request-granularity x
  phase-locked-pacing interaction**, not generic adaptive superiority.
- **Controls:** granularity-matched control A (ours C=16, 32 MiB on D2) ratio
  **0.862** CI [0.839, 0.885] (10 pairs) — the win shrinks as granularity is
  matched; constant-rate control (2.5 MB/s, no transition): ours-4M 15.3 s,
  ours-32M 16.7 s, aria2 24.8 s — the 4M-vs-32M gap collapses (2.3x -> 1.1x),
  confirming the effect is phase-locked rather than a bare range-size artifact.
- **Scope:** only the frozen D2/512 MiB workload family; no universal
  aria2-superiority, consumer-network, or out-of-family claim. This becomes the
  project's **primary competitive result** (a bounded positive), complementing
  the mechanism-first negative-result story of §22.88.
- **Artifacts:** `testdata/aria2_competitive_benchmark/` (`tools/bench_run.go`,
  `tools/run_benchmark.py`, `tools/analyze_benchmark.py`, `manifest/`,
  `reproduction_d2/`, `discovery_aria2/`, `discovery_ours/`,
  `final_reproduction/`, `mechanism/`, `analysis/`, `figures/fig1..fig9`,
  `reports/win_mechanism.md`, `reports/tables.md`). Historical D2 evidence and
  all frozen study artifacts unchanged.
- **Integrity:** the aria2 frontier split 1/2/4 runs first hit the tool's 180 s
  timeout (invalid; retained under `discovery_aria2/invalid_timeout/`) and were
  re-run with `-aria2-timeout 1500s` (valid); no invalid final pairs; qdisc
  cleanup checked.

### 22.90 Joint N+S phase-locked pacing screen (executed 2026-09-29, analytical only): SCREEN FAILED

**Status: `ANALYTICAL_SCREEN_FAILED — NO CAUSAL JOINT HEADROOM`.** The fixed
62.4 % aria2 benchmark win motivated a bounded follow-up question: can a causal
controller that changes **both** N and S during a download exploit a
predeclared family of phase-locked pacing regimes and beat the best fixed
configuration? A deterministic event-driven simulator calibrated to the
measured D2 evidence (40/40 fixed configs within 3 %; aria2 anchor −0.8 %) was
used for the predeclared analytical screen. **No empirical experiment was
run** — the analytical gate failed, which is the frozen stop condition.

- **Frozen design:** two-phase family slow 640 KiB/s -> fast 2500 KiB/s,
  transition T in {1,2,4,8} s, 512 MiB; fixed frontier N in {1,2,4,8,16} x
  S in {256K,1M,4M,16M,32M}; frozen 8-action joint action space; frozen start
  (4,4M); primary one-switch causal family (median-throughput ratio signal;
  thresholds {1.25,1.5,2.0}; targets {(16,16M),(16,4M),(4,16M)}); diagnostic
  two-switch probe, blind-ramp scan, optimistic causal bound, clairvoyant
  oracle; stationary controls; predeclared gate G1-G4.
- **Fixed frontier:** **N\* = 16 in every regime** (per-response pacing is
  independent -> monotone, consistent with the live T1 result); S\* varies
  (1 MiB at T=1,2,8 s; 4 MiB at T=4 s); max wrong-config penalty 7.9 %. The
  variation is single-dimensional (S only) — no genuine *joint* opportunity.
- **Causal results:** every primary one-switch controller loses in every regime
  (−16.2 % to −28.1 %); the diagnostic two-switch probe peaks at **+1.1 %**
  (T=8 s); the oracle-tuned blind ramp peaks at **+3.7 %** (T=2 s); the
  optimistic causal bound (clairvoyant from the first completion onward) is
  −14.7 / −7.8 / −6.3 / +3.4 % — no causal class reaches the 5 % materiality
  threshold in even one regime.
- **Oracle:** unconstrained +6.3..+14.5 %, same-start <= +2.3 % — the regime
  information value is real but lives in the *starting configuration*, not in
  within-download adaptation (causal/oracle capture ~ 0).
- **Structural causes:** (1) the frozen (4,4M) start fetches only ~16 MiB in
  the first ~6.6 s vs ~66 MiB for the best fixed (~50 MiB ≈ 7 % handicap,
  unrecoverable even with perfect knowledge); (2) request-completion
  observation latency: a post-transition sample requires a request that
  *started* after T.
- **Verdict:** G1 PASS (weak), **G2 FAIL**, G3 PASS, G4 PASS -> overall FAIL ->
  STOP. No preregistration was created; no empirical execution.
- **Artifacts:** `testdata/joint_ns_phase_pacing_screen/` (`model/`,
  `simulation/`, `tools/` jns_sim.py + calibrate.py + run_screen.py +
  make_figures.py + make_report.py, `analysis/calibration.json` +
  `analysis/screen.json`, `figures/fig1..fig8`,
  `reports/joint_ns_phase_pacing_screen.md`, `manifest/`). The completed aria2
  benchmark and all frozen studies are unchanged.
- **Interpretation:** no robust joint adaptation opportunity under this
  mechanism; running LinUCB/RL here would search for an advantage the screen
  shows is not there. A future positive study requires a mechanism where a
  causal signal can act before the starting-phase loss is irrecoverable (or an
  N-varying optimum); see `reports/joint_ns_phase_pacing_screen.md` §9.

---

### 22.91 Live shared-bottleneck / queueing mechanism gate (executed 2026-09-29, LIVE): NO JOINT OPPORTUNITY

**Status: `NO_LIVE-GROUNDED_JOINT_N+S_OPPORTUNITY`.** The final mechanism branch
before deciding whether genuine joint N+S adaptation is defensible was tested
**live**: a real `tc tbf` bottleneck on `lo` (40 Mbit/s, burst 32 kb, limit
256 kb, ~46 ms max queueing delay, MTU temporarily 1500) shared by the internal
downloader and independently generated `iperf3` background traffic (4 paced
TCP streams) in three predeclared contention states (B = 0 / 0.5C / 0.85C).
Fixed configurations only: 32 MiB file, S = 1 MiB, N in {1,2,4,8,16},
3 reps/cell; 45/45 valid runs (content-verified, background verified).

- **Measurable contention cost: YES.** Median RTT rises from a 0.04 ms
  no-qdisc baseline to 33-86 ms (p95 up to 94 ms); qdisc drops 0-860 per run;
  TCP retransmissions 0-576; all grow with N (p95 N=16/N=1 = 1.64x/1.55x/1.15x
  for S0/S1/S2).
- **Finite/interior N optimum: NO.** S0 (no background) is flat (7.05-7.17 s,
  N*=8 with a 0.3 % margin = noise); S1 (0.5C) is flat-to-slightly-increasing
  (14.28-15.13 s, N*=16 with a 3.9 % margin, below the 5 % threshold);
  S2 (0.85C) falls strongly with N (62.5 s at N=1 -> 15.9 s at N=16) because
  more downloader flows win a larger TCP share against the paced background
  (its achieved rate falls 33.4 -> 22.9 Mbit/s) — a **boundary** optimum, not
  interior.
- **State-dependent N*: NO material difference.** N* differs only between the
  flat S0 and the boundary S1/S2, with sub-5 % margins on the flat side; the
  predeclared gate FAILED (G-N1 and G-N2), so per the frozen stop rule the S
  sweep, joint gate, within-download transition, causal controller, aria2
  comparison and confirmatory protocol were **not reached/created**.
- **Analytical calibration:** a deterministic shared-bottleneck model
  (`tools/queue_model.py`: work-conserving bulk + finite-file tail + fitted
  loss term + measured queueing RTT) fits the 15 live cell medians with 8.2 %
  RMSE (max 18.7 %) and predicts the same flat/boundary N* pattern; the bulk
  term is N-independent, so no interior optimum is structurally expected from
  a shared FIFO bottleneck under TCP.
- **Infrastructure:** failures found and fixed before the gate measurements
  (default 65536-byte loopback MTU bursts stalled downloads; iperf3 `-b` is
  per stream; intermittent background control-connection resets). Invalid runs
  are preserved under `mechanism_gate/invalid/` and excluded; the loopback MTU
  and qdisc were restored after the study.
- **Artifacts:** `testdata/live_joint_ns_queueing/` (`mechanism_gate/`,
  `analysis/gate_analysis.json`, `calibration/model_calibration.json`,
  `figures/fig1..fig5`, `reports/joint_ns_queueing_mechanism_report.md`,
  `manifest/`, `tools/`). No previous benchmark/evidence directory was modified.
- **Interpretation:** the shared-bottleneck/queueing mechanism is
  **INSUFFICIENT** for joint adaptation (work conservation removes the
  completion-time interior optimum); no further mechanism was invented in this
  task. The fixed 62.4 % aria2 benchmark remains the project's primary
  competitive result and is unchanged.

---

### 22.92 Correlated loss / range-retry mechanism screen (executed 2026-09-29, analytical only): NO JOINT OPPORTUNITY

**Status: `NO_JOINT_N+S_OPPORTUNITY_UNDER_CORRELATED_LOSS`.** The final
mechanism branch — correlated burst loss + the downloader's range-retry
semantics — was screened analytically before any live execution. A deterministic
event-driven simulator (`tools/loss_sim.py`) drives a time-indexed two-state
Gilbert–Elliott loss process (G: 0.4 % loss, 20 s sojourn; B: 50 % loss,
0.15 s/2.0 s sojourn) through the frozen retry semantics (30 s request timeout,
3 same-range attempts, 0.1 s retry gap, in-flight immutability) with
per-response pacing at the D2 initial rate (640 KiB/s). F = 512 MiB;
N ∈ {1,2,4,8,16}; S ∈ {256K,1M,4M,16M}; 10 seeds/cell.

- **Parameter grounding:** loss rates from immutable project netem evidence
  (0.4 % clean; 2.5–7 % severe phases); burst dynamics explicitly grounded in
  the two-state Markov literature (NTIA TM-23-565; Haßlinger–Hohlfeld MMB 2008;
  Paxson 1997; Yajnik et al. 1999) because the project contains **no**
  correlated-loss measurements — recorded in
  `parameter_grounding/parameter_grounding.md` and swept as a predeclared
  robustness range.
- **S-retry amplification: PRESENT and material in the BAD condition.** At
  S = 16 MiB the per-attempt failure rate is 32.7 %, with 10.6 retries and
  169 MiB re-downloaded per download and 48 % of downloads aborting (retry
  limit); S ≤ 4 MiB never fails. Using the GOOD-optimal S = 16 MiB under BAD
  costs **+36.3 %** completion (81.8 s vs 60.0 s).
- **N dimension: INSUFFICIENT.** Completion is monotone decreasing in N
  (≈ N per doubling) in both conditions and **N\* = 16 in all 9 predeclared
  robustness variants**; the per-request burst exposure and aggregate progress
  are N-independent under per-response pacing. This matches the live T1 result
  and the live shared-bottleneck screen.
- **Joint optimum: does NOT move** (N* = 16 in both loss states; only S* moves
  16M -> 4M). The analytical gate fails condition 1; conditions 4–8 (transition,
  causal detection, causal headroom, file-size residue, stationary control) were
  not evaluated. **No live gate, no adaptive execution, no aria2 comparison, no
  confirmatory protocol.**
- **Robustness:** the S* movement appears in 4/9 variants (primary, T_G = 10 s,
  g = 0.25, g = 0.5) and not at faster base rates (the S-retry amplification is
  rate-conditional); the N monotonicity holds in 9/9.
- **Artifacts:** `testdata/joint_ns_loss_mechanism/` (`parameter_grounding/`,
  `analytical_gate/stationary_screen.json` + `robustness_sweep.json`,
  `analysis/analytical_gate_analysis.json`, `figures/fig1..fig8`,
  `reports/joint_ns_loss_mechanism_report.md`, `manifest/`, `tools/`).
  No previous evidence was modified; no live experiment was run.
- **Per the task's final stop condition:** the mechanism search is **closed**.
  No further invented mechanism will be created without a separate
  research/design decision. The defensible competitive result remains the fixed
  **N=16, S=4 MiB D2 aria2 win (62.4 %)**, unchanged.

---


**Re-check without the request deadline (2026-10-25).**
`tools/loss_sim_nodeadline.py` is a copy of the frozen `loss_sim.py` with a
configurable deadline. At 30 s it reproduces `analytical_gate/
stationary_screen.json` and `robustness_sweep.json` byte for byte. With no
deadline (`nodeadline/no_deadline/`), N* = 16 and S* = 16 MiB in both states
in all 9 variants, with 0 failures and retries. The S-retry amplification
(32.7 % per-attempt failure, 48 % aborts, +36.3 %) and the 16M→4M shift in
4/9 variants were artefacts of the modelled 30 s deadline. The verdict is
unchanged: NO_JOINT_N+S_OPPORTUNITY_UNDER_CORRELATED_LOSS.
### 22.93 Track 1 fresh joint N+S D2 benchmark (executed 2026-09-29, LIVE): BEATS_ARIA2_ONLY

**Status: `JOINT_ADAPTIVE_BEATS_ARIA2_BUT_NOT_FIXED` — classification
C = `BEATS_ARIA2_ONLY`.** A fresh predeclared 20-block benchmark on the exact
frozen D2 workload compared four modes — fixed (16, 4 MiB), N-only adaptive,
S-only adaptive, joint adaptive N+S (the existing heuristic policies with
frozen bounds/steps/thresholds, initial (4, 4 MiB), one serialized decision
per completed attempt, ±10 % thresholds, 30 s timeout, 3 retries) — with the
frozen aria2 configuration as the practical reference. 80/80 runs valid,
byte-exact content verification, zero retries, 3 aria2 consistency runs
(53.27 s vs the immutable 30-pair mean 53.56 s, −0.5 %).

- **Results:** fixed mean 20.25 s / median 20.19 s; N-only 36.93 s; S-only
  64.97 s; joint 46.91 s.
- **Primary (adaptation-value) comparison:** joint vs best fixed — geometric
  ratio **2.3161** CI [2.3023, 2.3289], **0/20 wins** → the joint policy is
  2.32× slower; adaptation added no value on D2.
- **Practical comparison:** joint vs frozen aria2 (immutable 30-pair
  reference) — ratio **0.8759** CI [0.8707, 0.8801] → 12.4 % faster, but the
  fixed best beats the same reference by ~62 %. Beating aria2 alone is not
  adaptation evidence.
- **Adaptation evidence:** 20/20 joint runs changed **both** N and S (100 %);
  median final state (6, 6 MiB), i.e. away from the optimum (16, 4 MiB);
  N-only ends at (8, 4 MiB); S-only at (4, 8 MiB).
- **Mechanism:** decisions require a completed attempt, so with the frozen
  initial 4 MiB chunks at 640 KiB/s the first actionable decisions occur at
  ~8.6 s (after the pacing recovery); the windowed per-request throughput
  signal rises 607 → ~2184 KiB/s and then saturates, the ±10 % rule stops
  firing, and each policy freezes after ~4 applied actions within a ~30 ms
  window. Zero retries/timeouts. The final S increases (6-8 MiB) also raise
  each request's service time and reduce scheduling flexibility.
- **Classification:** C = `BEATS_ARIA2_ONLY` by the predeclared A–E rules;
  adaptation was demonstrated mechanically but did not produce a performance
  win over the best fixed configuration.
- **Artifacts:** `testdata/joint_ns_d2/` (manifest frozen before execution,
  80 per-run results, raw logs + validity metadata, analysis + mechanism
  JSONs, figures fig1–fig8, tools); report
  `reports/joint_ns_d2_report.md` (copy in the study's `reports/`). The
  frozen 62.4 % benchmark and all prior evidence are unchanged.
- **Track 2 (controlled N-dependent server throttling, the proposal's
  promised mechanism) was NOT started** — it requires a separate instruction;
  it remains the only identified path to a genuine joint-adaptation value
  result. The fixed 62.4 % aria2 win remains the benchmark anchor.

---

### 22.94 Track 2 N-dependent server mechanism pre-screen (executed 2026-09-29, analytical only): STOPPED

**Status: `PRE_SCREEN_FAILED_NO_QUALIFYING_SCENARIO`** (maps to
`LIVE_MECHANISM_GATE_FAILED_JOINT`; the live gate was **not reached**). The
mechanism the submitted proposal promised — configurable N-dependent server
throttling — was reproduced exactly from `python/rl/sim_env_v5.py`:
per-request failure `p_eff(k) = min(0.9, loss + congestion*(k-1))`,
per-extra-connection overhead `0.02 s*(k-1)`, fair share
`per = min(rate, link/k)`, plus the frozen downloader retry semantics (30 s
timeout, 3 same-range attempts, then the download aborts). The rejected
`R_eff = R*(1-loss_eff)` substitute was **not** used.

- **Predeclared family:** GOOD (2.5 MiB/s, loss 0.4 %, congestion 0) plus 16
  degraded candidates (rate {0.4, 0.5} MiB/s x loss {2 %, 3 %} x congestion
  {0.02, 0.04, 0.06}, plus 4 link variants); N in {1,2,4,8,16}; S = the exact
  frozen Track-1 action set; 20 seeds/cell; three predeclared workload scales
  F in {64, 128, 256} MiB.
- **Frozen selection rule:** N* and S* must move, material cross-state penalty
  >= 10 %, coordinate-wise margins >= 5 % in both states, optima reachable;
  tie-break smallest F, then max min(penalty); if none qualifies -> STOP, no
  tuning.
- **Result: no candidate qualifies at any file size.** The strongest candidate
  (rate 0.4 MiB/s, congestion 0.04, F=64 MiB) moves S* (4 -> 11 MiB) and the N
  argmin (16 -> 8) with large cross-state penalties (+134 % / +10.7 %), but the
  N* movement is **degenerate** (N=8 and N=16 tie exactly: only six requests
  exist at the degraded optimum), the S* margin is **1.0 %** (the optimum is the
  last viable point below the 30 s timeout cliff at S = per x 30 s = 12 MiB),
  and the best single fixed config (16, 4 MiB) is within 10.7 % of the degraded
  optimum (an S-only switch suffices).
- **Structural reasons:** (1) the failure-based concurrency penalty plus the
  3-attempt retry limit creates an **abort cliff** (p_eff(16)=0.62 -> 23.8 %
  per-range abort) instead of a smooth interior N optimum; (2) the failure cost
  is per-request and size-independent, so the degraded state favours fewer,
  larger requests and pushes S* to the timeout boundary; (3) at that boundary
  the request count is small, so the N optimum is a plateau. The simulator's
  analytical interior N optimum (L4 bestC=2-6) does **not** survive the live
  retry/abort semantics.
- **Not executed (frozen stop rule):** live server extension, live fixed-grid
  gate, adaptive experiment, aria2 comparison. No environment changes; no frozen
  artifact touched. Pre-screen revisions (workload scale and separation metric;
  candidate family unchanged) are recorded in `manifest/track2_manifest.json`.
- **Artifacts:** `testdata/n_dependent_server_joint_ns/` (`manifest/`,
  `analysis/prescreen.json` + revision snapshots + `track2_analysis.json`,
  `figures/fig1..fig6`, `tools/ns_sim.py`, `tools/prescreen.py`, ...); report
  `reports/n_dependent_server_joint_ns_report.md` (copy in the study's
  `reports/`).
- **Conclusion:** the promised N-dependent server throttling does not create a
  genuine state-dependent joint N+S opportunity. The defensible results remain
  the fixed **62.4 % aria2 D2 win** and the Track-1 `BEATS_ARIA2_ONLY` finding.

---

### 22.95 Exploratory combined-mechanism screen (2026-09-29, analytical, EXPLORATORY): first clean joint movement

**Status: `EXPLORATORY — soft N-dependent server throttle + pacing creates a clean state-dependent JOINT (N*,S*) movement`.** While scoping the final-month
positive result, an exploratory screen tested the combination the proposal
promised but no single prior study covered: a **soft** concurrency-dependent
server throttle (a rate cap, no hard request failures) combined with the
time-varying per-response pacing schedule.

- **Model:** `rate_eff = min(schedule.rate(start), C/(k*(1+gamma*(k-1))))`,
  k = in-flight requests at the request's server start; otherwise the frozen
  jns_sim semantics (per-response rate sampled and held, timeout/retry, 512 MiB,
  N ∈ {1,2,4,8,16}, S ∈ {256K,1M,4M,16M,32M}). Tools:
  `testdata/n_dependent_server_joint_ns/tools/combined_sim.py`,
  `tools/explore_combined.py`; results:
  `analysis/exploratory_combined_soft_cap.json`.
- **Result:** of 27 (schedule, cap) pairs, exactly one passes the strict
  Track-2 selection rule (both N* and S* move, coordinate margins >= 5 %,
  cross-state penalties >= 10 % both ways, no degeneracy):
  **T = 1 s schedule, C = 8 MiB/s, gamma = 0.05** →
  no-cap state **(16, 1 MiB) 18.42 s** vs capped state **(4, 4 MiB) 85.51 s**;
  the capped state pays **+35.8 %** if it uses the no-cap optimum, and the
  no-cap state pays **+252.9 %** if it uses the capped optimum. N margin in the
  capped state +6.4 %, S margin +5.9 %.
- **Why this is different from the failed Track-2 model:** the earlier
  pre-screen used the sim_env_v5 *failure* semantics (probabilistic request
  failure + the 3-attempt retry limit), which produced an abort cliff and a
  degenerate S*-at-timeout-boundary optimum. The **soft rate cap** has no hard
  failures, so the interior N optimum survives with clean margins. This is the
  first mechanism in the project with a genuine state-dependent joint movement.
- **Caveats:** exploratory only (the family was swept before a frozen
  pre-screen); no causal-controller check yet; no live implementation yet. The
  next steps are (1) a frozen pre-screen with a predeclared family/selection
  rule reporting all candidates, (2) the live server rate-cap extension with
  unit/concurrency tests, (3) the live fixed-grid gate, (4) the causal joint
  controller vs best fixed vs aria2. This substantially improves the odds of
  the planned "B" result (the controlled N-dependent scenario).
- **Not modified:** no frozen artifact or prior study was touched.

---

### 22.111 Paper update for the viva and the presentation deck (2026-10-28)

`reports/paper_draft.md` now states the question as "does adapting N and S
help when conditions are not optimal, against aria2 and other tools, and is
the gain from adapting or from how the file is split" (abstract, §1), with a
"what is not new" line and an updated contribution list. New §9e: adaptive
arms against aria2 under changing conditions (faster only when the server rate
rose, where our fixed configuration was faster still; slower whenever the
server degraded). §8 now says the Mahimahi replay varied capacity only
(mm-link alone, no mm-delay/mm-loss) and gives the per-trace downlink swing,
p90/p10 6.5–25.3 (`tools/trace_swing.py` →
`testdata/final_oracle_analysis/trace_swing.json`; the trace files themselves
are not in git). Also: k includes the arriving request; C/γ from the 27-pair
screen; live-rate mode; tuner compared with same-range-size fixed arms;
mirror-check cause marked as interpretation; naming note (cap-trained v1/v2
vs early DQN v1–v6); limitations (capacity-only traces, no live test with
changing loss/delay, fairness); authors and contributions in Declarations.
Checker: 231/231, 0 unverified (Track 1 aria2 ratio and trace swings added).
`tools/paper_to_pdf.py` renders the draft to `reports/paper/paper_draft.pdf`
(pdflatex; no pandoc here). Presentation deck: Slides artifact
[private viva slide link removed] (30 slides; figures from
`reports/handbook/make_deck_figs.py`). The handbook still quotes the earlier
"about 5–27×" (p90/p10 over the 8-trace S-pivot set), which is a different set.

Deck render check (2026-10-28): every slide rendered in headless Chromium and
checked for text under 24 px, overlaps, overflow and code names. Fixes: demo
slide "220 numbers" -> 231; "v2" removed from the limitations notes; label
collisions fixed in five charts (`make_deck_figs.py`: fig1, trace_capacity,
joint, live, forest). Deck republished; checker still 231/231.

Paper and deck, later on 2026-10-28: the paper is built in the IEEE Access
template (`python3 tools/paper_to_ieee.py` -> `reports/paper/paper_access.pdf`,
official class in `reports/ieee_access/template/`), with a question-first title,
a short abstract (the old one is kept as "Extended abstract" in the Markdown),
the answer to the central question as contribution 1, and figures 4-8
(`reports/ieee_access/make_paper_figs.py`). The deck gained slide 12, "Other
download tools" (axel, lftp; wget2 and IDM excluded), so it has 32 slides.
Checker still 231/231.

### 22.110 Team handbook (2026-10-26, COMPLETE)

`reports/handbook/handbook.tex` → `handbook.pdf` (93 pages, LaTeX, pdflatex):
a study guide for the viva and presentation covering background, system,
method, timeline and reviews, all results (incl. the post-review studies),
novelty, limitations, ~70 viva Q&As, the demo, a glossary, a numbers cheat
sheet and an inventory of every `testdata/` folder. New figures are drawn by
`reports/handbook/make_figs.py` from committed analysis JSON; others are
copied from reports/review2, review3 and ieee_access. Work division is the
Review 2 "Individual Contribution" table (the Review 1 docx has no
per-member table). Three independent fact-check passes (~810 claims) found
wrong facts and overclaims that were fixed before commit; notable
corrections also relevant elsewhere:
- `reports/thursday/demo.md` says 8×1 MiB was "within 3.7 %" of the best:
  it was *on average* 3.7 % slower (11/16 within 5 %, max 15.3 %).
- `reports/ieee_access/README.md` lists Mahimahi/trace/LinUCB/DQN/netem
  citations as unresolved; `reports/references.md` shows them verified
  2026-10-18. Liu 2018 and Swargo & Arifuzzaman (elastic) remain unresolved
  and are cited only in the reviews.
- The soft cap's k includes the arriving request (server.go `activeRequests.Add(1)`).
- C = 8 MiB/s, γ = 0.05 came from a 27-pair exploratory screen; the 72
  analytic candidates were the W1–W6 design.

### 22.109 DQN retrained for the capped server, with simulator parity and held-out generalization (2026-10-25, RETRAIN COMPLETE; follow-ups in progress)

**Status: `COMPLETE` for the retrain (A2 1ef2c77, B 9b4e45f, C f510dd5, D a68d511).** Protocol
`testdata/rl_cap_train/protocol.md` (stages A simulator parity, B training,
C live evaluation, D held-out generalization; wording rules fixed in advance).

- **Why v6 could not do well live on the capped server:** its simulator
  (`sim_env_v6`) had no arrival-time soft cap, and its aggregate observable
  was C·n/svc, while the live `policyRunner.aggregateThroughput` is bytes of
  the last `-window` attempts over their span. With `-window 4` the live value
  saturates near N = 4 (HEALTHY N = 4/16: 4.4/6.3 MiB/s vs 6.2/14.8 at
  window 16). The new agent uses `-window 16` in training and live runs.
- **Simulator:** `python/rl/sim_env_cap.py` (tests
  `python/tests/test_sim_env_cap.py`, 13). Event-driven: rate fixed at server
  arrival, k = capped requests in service (pre-cap requests not counted), one
  decision per completed attempt, increase starts one worker, decrease retires
  the next completer, observables built exactly as `policyRunner.observe` +
  `LivePolicy.observeV5`. 1 MiB ranges never split (needs 2 MiB unreceived).
  Jitter: per-request server excess and client overhead sampled from
  `testdata/rl_cap_train/calibration.json` (percentiles from rl_capped request
  logs; completion times not used). Retrospective parity: fixed 16x1 MiB CAP8
  77.7 sim vs 76.6 live, HEALTHY 18.8 vs 18.4; v6 closed loop within 3 %
  except CAP8 start16 (79.7 vs 71.3; live runs inside the sim range).
- **Headroom (sim):** CAP8 best fixed N at 1 MiB is 16 (76.9 s); a 16->3
  switch at the cap gives 64.0 s. 16x32 MiB (52.6 s live) is out of reach for
  any concurrency-only 1 MiB agent because its ranges all start pre-cap.
- **Held-out live scenarios (Stage D):** G_C16 (C = 16 MiB/s), G_RTT (80 ms
  each way), G_D2 (d2 schedule), all outside the training ranges; each needs
  a different post-cap N (7, 5, 4).
- **Tooling:** `python/train_cap.py` (5 seeds, selection on validation only,
  evaluation through the exported model's numpy forward pass, exports model +
  `parity_fixture.json`), `internal/rl/dqn/cap_candidate_parity_test.go`
  (Go features and actions must match Python), `testdata/rl_cap_train/tools/
  run.py STAGE`, `tools/analyze.py STAGE`. Harness: unchanged
  `testdata/rl_capped/tools/rl_bench.go`.
- **Do not run training while live runs are in progress** (CPU load distorts
  live timing).
- **Results (rl_cap_train):**
  - A2 blind live parity PASS: all 4 fixed-N predictions within 1.4 %.
  - B: seed 0 selected (validation 1.076). Simulator gates: G1 pass, G2
    FAIL (HEALTHY 1.080), G3 pass. Go/Python parity passes; model sha
    be70c7ab.
  - C (120/120 valid), primary: CAP8 retrained agent vs 16x1 MiB = 0.907
    [0.893, 0.921], FASTER (simulator 0.912). Against 16x32 MiB 1.298 and
    aria2 1.310, slower; against v6 0.949, not distinguishable. HEALTHY 1.108
    slower. Start at N = 1 slower in both scenarios. The agent settles at
    N ~ 7 after the cap (oracle 3-4).
  - D (65/65 valid; protocol said 80, amendment A1): faster than the best
    fixed 1 MiB in 0 of 3 held-out scenarios. G_C16 1.177 slower; G_RTT
    1.016 and G_D2 1.015 (vs 4x1 MiB) not distinguishable. Frozen wording:
    beat the best fixed 1 MiB setting on the server it was trained for, but
    did not generalize.
- **Follow-ups (see queue.md):**
  - baselines_ext: axel, lftp, aria2, ours. wget2 removed by amendment A2
    (upstream issue #195: one thread per file).
  - netem_sweep: delay x loss grid, ours vs aria2.
  - rl_joint (COMPLETE; C fd7130e, L 5e2acee; protocol
    `testdata/rl_joint/protocol.md`): joint N+S DQN on `sim_env_joint`, plus
    a live-rate control server (`Server.SetLiveRate`, opt-in, default off).
    A2 blind parity passed (+1.5 to +7.0 %). Retrained under A1 (747k-824k
    steps per seed); simulator CAP8 1.513 of best fixed, J1/J2 fail, J3
    passes, although a scripted 1 MiB -> 33 MiB rule scores 0.352.
    - Stage C (150/150 valid): CAP8 dqnj 74.8 s vs 16x32 MiB 50.9 s, ratio
      1.471 [1.405, 1.546], SLOWER; HEALTHY 1.081, CAP0 1.359, D2 1.372 (vs
      16x5 MiB), all slower. vs aria2: CAP8 1.422 slower, HEALTHY 0.388
      faster. Live medians within -1 to +5 % of the frozen A2 predictions.
      The agent was still at 1 MiB at 8 s in every scenario and grew ranges
      to 33 MiB only after the cap.
    - Stage L (40/40 valid): CAP8L 1.029 [0.989, 1.081] vs 16x32 MiB, not
      distinguishable; HEALTHYL 1.128 slower. Without the arrival rule,
      16x32 MiB under the cap slows from 50.9 s to 67.4 s.
    - Frozen wording: "a joint agent trained on a checked simulator did not
      beat the best fixed setting live". The simulator was accurate; the
      learner did not find the available headroom (as with the one-threshold
      rule earlier), which motivates the v2 drop-rule arm.
  - rl_cap_train_v2 (COMPLETE; protocol `testdata/rl_cap_train_v2/protocol.md`,
    frozen 4b3076c after an Opus review; A1 b634a51, A2 bc9917b, A3 1492f23;
    C 2368d56, D 3515a44). Broader training (C 4-24 MiB/s, delay 10-120 ms,
    early/late dips), calibration at 20/40/80/120 ms with four rate classes
    (A1: pooled classes mispredicted CAP8 120 ms N=16 in-sample; residual
    -7.7 % disclosed). Blind A at 80/120 ms passed (+1.4/-3.3/+5.6/+5.6 %).
    New held-out H_C12, H_RTT110, H_DIP, H_C20 inside the ranges, sim
    headroom 9.9-17.4 %. Seed 1 selected (27657be21038); other seeds mostly
    >= 1 on H (disclosed in A3).
    - C (64/64): CAP8 v2 0.845 [0.840, 0.850] vs fixed 16x1 MiB, faster;
      vs v1 0.929; vs drop 1.048 slower; HEALTHY 1.047 slower.
    - D (160/160): H_C12 0.882, H_RTT110 0.819, H_DIP 0.952 faster; H_C20
      1.007 [1.000, 1.014] slower (t-interval n.d.; less favourable used).
      v1: faster 1, slower 2. v2 faster than v1 in 6/6. vs C=8-tuned drop:
      faster 3 (two where the rule's tuning was wrong), slower 2, n.d. 1.
      H_RTT110 v2 live 68.6 s vs sim 76.4 s (unexplained).
    - Frozen verdict (`analysis/verdict.json`): "v2 generalized partially:
      faster in 3, slower in 1 of 4 held-out scenarios, all inside the v2
      training ranges"; "learned control adds something over a C=8-tuned
      drop rule" (3 vs 2).
  - live_mirror_check (COMPLETE; frozen 5b360b2; A1 probe attempts; A2
    outage, 77e19f9; M2 a4c6666): v2, drop, fixed 16x1 and 8x1 MiB on kcdn,
    kedge, riken, 5 blocks; no-harm check. Slot M1 lost to a client-side
    outage from 22:10 UTC (20/60 valid; reported, not used). Slot M2 after a
    link gate: 60/60 valid. v2 vs best fixed: kcdn 1.401 [1.102, 1.773]
    slower, kedge 1.654, riken 1.215 (n.d.); drop 1.125, 1.063, 1.332. Frozen
    sentence: "On 0 of 3 public mirrors, the v2 agent showed no harm against
    the best fixed 1 MiB setting (and the drop rule on 0 of 3)." v2 ended at
    median N 6-7 on kcdn/kedge; drop false-tripped 4/5 on kedge and riken.
    Paper §8a.
  - claims_check incident (2026-10-25): `_run_main` captured json.dump but the
    analysis scripts open their output with "w" first, truncating four
    committed analysis files; committed by a `git commit -a` (ee87a2f),
    restored byte-identical (452bb6b) and the checker now redirects open().
- **Process files:** `queue.md` (work queue) and `CLAUDE.md` (delegate side
  investigations to subagents; maintain the queue).
- **Immediate next step:** see queue.md. Baselines, sweep, joint study and
  v2 are done and in the paper (§4.3, §4.4, §9c, §9d; checker 213/213). The
  mirror check is done (§8a; checker 220/220). Remaining: rehearsal and the
  IEEE Access conversion.

