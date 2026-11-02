### 22.70 Oracle stage (prepared 2026-09-27): BLOCKED — tau regime rule not pre-registered

**Status: `BLOCKED — ORACLE PREREQUISITE UNRESOLVED` (specific gate:
`BLOCKED — TAU REGIME RULE NOT PRE-REGISTERED`). The frozen v2 input was
verified; no tau, static baseline, instantaneous oracle, constrained oracle,
or headroom was computed, and no measurement or threshold was fabricated.
The frozen pilot artifacts were not modified.**

- **Input verified (read-only, from `/home/vscode/epoch_pilot_v2`):**
  `pilot_manifest.json` confirms protocol version 2, request deadline none,
  dial deadline none, parent/child watchdog 45 m; `pilot_report.json` confirms
  `passed=true`, `selected_duration="2m0s"`. The 120 s shared-valid epoch set
  was recomputed from the per-run `result.json` records:
  **21 candidate / 18 shared-valid / 3 excluded** (EVDO 7, LTE 11), exactly as
  expected. Excluded rows: EVDO 2 epochs × 5 C `below_byte_floor`; LTE 1 final
  partial epoch × 5 C `below_observed_fraction`. Retained epoch fields
  (trace, epoch index, start/end, C, bytes, observed duration, observed
  fraction, R, per-C validity, exclusion) are preserved without interpolation
  in `oracle_stage_v2/shared_valid_epochs.jsonl` (90 rows = 18 epochs × 5 C).
- **Blocking prerequisites absent from the repository (verified by search):**
  1. **tau regime classification rule + regime-dependence threshold** — no
     such classifier or threshold exists anywhere in the repository;
     §22.52 already records "protocol constants (epoch durations, tau regime
     classifier/thresholds, warm-up model, pilot) not frozen/executed".
  2. **transition-cost (tau) measurement design ("switch-injection") and any
     frozen tau measurement** — §22.52 lists "transition-cost measurement"
     under "Not started"; no tau artifact exists under `testdata/` and no
     switch-injection code exists.
  3. **completion-time / workload mapping from `R[C,e]`** — no Stage 0 oracle
     specification exists; §22.52 lists "instantaneous oracle, best fixed C,
     ramp/action-constrained oracle DP" as not started.
  4. The frozen Stage 0 constrained-oracle convention (action ladder, dwell
     rule, tie-breaking) is likewise not present in the repository protocol
     (only in the task prompt), so it could not be treated as pre-registered.
  The task's stop rules require stopping here ("if the exact threshold has not
  yet been frozen in the repository, STOP before formal headroom … Do not
  invent a threshold"; "STOP … if tau cannot be measured according to the
  protocol … completion-time mapping is ambiguous"), and §22.52 states that
  such decisions must not be chosen autonomously.
- **Not done / not claimed:** tau was not measured; `T(C)`, `T_best_fixed`,
  the instantaneous diagnostic, the constrained oracle, and `H_j` were not
  computed; no headroom, confidence interval, RL, adaptive controller, aria2,
  or IDM was run; no fixed-C pilot was rerun; no protocol parameter was
  changed.
- **What must be pre-registered before the oracle stage can proceed:** the tau
  measurement design (objective regimes, switch-injection procedure, both
  transition directions, equivalent-completion-time loss estimator), the
  regime classification rule and regime-dependence threshold (written before
  any tau measurement or headroom inspection), the completion-time/workload
  mapping, and the exact constrained-oracle convention. Then measure tau,
  freeze it, build the oracle, and only then compute `H_j`.
- **Sanity checks reserved for after unblocking (not applied now):** for
  completion-time minimisation, `T_constrained <= T_best_fixed` (a static C is
  feasible) and `T_instantaneous <= T_constrained` (the unconstrained oracle
  is a lower bound), hence `H_j >= 0` unless a documented numerical edge case
  occurs; negative headroom must be reported, not clamped.
- **Statistical limitation (unchanged):** the pilot has only **n=2 traces**,
  which cannot satisfy the final trace-level statistical gate; any future
  two-trace headroom is descriptive only and the final gate remains pending
  additional eligible traces.
- **Artifacts:** `/home/vscode/oracle_stage_v2/` —
  `oracle_manifest.json` (status, missing prerequisites, 24 pilot input
  hashes), `input_verification.json`, `shared_valid_epochs.jsonl`,
  `tau_measurements.json` / `fixed_c_completion.json` /
  `instantaneous_oracle.json` / `constrained_oracle.json` / `headroom.json`
  (explicit `NOT_COMPUTED` placeholders with the blocked reason),
  `oracle_tables.txt` (empty tables with blocked markers),
  `oracle_analysis.txt` (human-readable). Input-hash re-check after the stage:
  all 24 tracked pilot files unchanged.
- **Validation (this task):** `gofmt -l .` clean; `go test ./...` pass;
  `go test -race ./...` pass; `go vet ./...` pass; `go build ./...` pass; no
  `mm-link`/child/namespace/qdisc residue.

### 22.71 Oracle/tau pre-registration and native tau measurement (executed 2026-09-27)

**Status: `TAU_PREREGISTERED_AND_MEASURED`. The §22.70 blockers are resolved:
the oracle objective, the native-trace regime classifier, the injection design,
the 2.0 regime threshold, and the tau aggregation rule are frozen in the
repository, and all eight transition directions were measured with dedicated
switch-injection replays. No headroom/oracle was computed.**

- **Why the oracle was blocked (§22.70):** no pre-registered tau regime
  classifier/threshold, no tau measurement design or frozen tau, and no
  completion-time/workload mapping existed anywhere in the repository; the
  task rules forbade inventing them.
- **Pre-registration artifact:** `testdata/oracle_preregistration_v2/`
  (`oracle_preregistration.json` + `.txt`), frozen before any measurement:
  oracle protocol version 1, fixed-C protocol version 2, selected epoch
  120 s, action ladder `1<->2<->4<->8<->16` (stay or one step per boundary;
  no direct jumps), dwell rule (after a change at boundary e, `C_{e+1} = C_e`),
  static-C feasibility, headroom formula
  `H_j = (T_best_fixed,j - T_constrained,j)/T_best_fixed,j`, statistical unit =
  trace, and the tau/classifier/aggregation definitions below.
- **B_e mapping:** every retained shared-valid epoch uses the same workload
  `B_e = 5 MiB` (the frozen byte floor) for every C; `t[c,e] = B_e/R[c,e]`.
  The global workload scale cancels in the dimensionless headroom ratio.
- **Regime classifier (native downlink trace only; never downloader
  throughput):** full 120 s epochs over the downlink trace (final partial
  epoch excluded); rate = opportunities × 1500 B / 120 s;
  `k = max(1, floor(n/3))`; degraded = k lowest rates (ties: lower index);
  good = k highest rates not already degraded (ties: lower index); recovering
  = `rate[e] >= 1.25*rate[e-1]` and not degraded; precedence degraded →
  recovering → good → middle. Deterministic (ties resolved by index).
  Classification on the pilot traces: EVDO 8 full epochs → degraded {3,4},
  recovering {1,5,6}, no good epochs; LTE 11 full epochs → degraded {2,4,5},
  good {1}, recovering {3,6,9}.
- **Injection timestamps (frozen before tau):** for regimes in the order
  degraded, good, recovering, the earliest full epoch and the next distinct
  one when available; absent regimes contribute nothing. Frozen plan:
  EVDO → degraded e3 (360 s), degraded e4 (480 s), recovering e1 (120 s),
  recovering e5 (600 s); LTE → degraded e2 (240 s), degraded e4 (480 s),
  good e1 (120 s), recovering e3 (360 s), recovering e6 (720 s). Plans
  persisted in `tau_campaign_v2/plan/*.json` (hash-recorded).
- **Switch-injection design:** one continuous replay per direction at each
  preselected timestamp: run at source C until the epoch boundary, force the
  target C (`downloader.SetConcurrency` with adaptive bounds 1–16), and
  measure the following complete 120 s epoch. The run is truncated by the
  parent watchdog immediately after the window (`switch_at + 120 s + 15 s`);
  the child self-watchdog is 2 min later so the parent truncation always wins
  and cancellation is non-fatal. The matched control is the protocol-v2
  fixed-C pilot run at the target C over the identical trace-time window
  (read-only; same traces, same protocol, same windows). Tau formula:
  `tau = (control_bytes - switched_bytes)/(control_bytes/120 s)` in seconds of
  equivalent 5 MiB completion time; negative values preserved; a zero-byte
  window or zero control bytes invalidates the observation (reported, never
  divided).
- **Implementation:** `internal/experiment/tau` (classifier, injection
  selector, ladder/dwell validation, `t[c,e]` mapping, window byte
  accounting, median/aggregation with the frozen 2.0 threshold) and
  `cmd/tau-injection` (`-plan`, `-measure`, `-aggregate`, switch-injection
  parent/child modes). `replay.ChildConfig` gained harness-only adaptive
  bounds and a scheduled switch (`MinConcurrency`, `MaxConcurrency`,
  `SwitchAt`, `SwitchTo`) with summary evidence (`switch_at_ns`, `switch_to`,
  `switch_applied`, `final_concurrency`); fixed-C behavior is unchanged when
  these are zero. No change to the fixed-C downloader semantics, C set,
  chunk, epoch duration, byte floor, observed-fraction threshold, trace set,
  or anchors.
- **Campaign:** 9 timestamps × 8 directions = **72 switch-injection runs**
  under protocol v2 (request/dial deadlines none) in
  `/home/vscode/tau_campaign_v2/runs/`, all 72 valid, 0 failures, 0 retries
  (2026-09-27 ~14:05–16:41 UTC). Harness note: simultaneous mm-link launches
  can collide on the host-side `10.0.0.x` address (observed once: two
  instances picked `10.0.0.9`, producing a zero-byte hang); the runner now
  gates launches ≥ 5 s apart, which eliminated the race (72/72 clean). The
  zero-byte case is also excluded by the pre-registered validity rule.
- **Tau aggregation (frozen rule):** per direction, pooled tau = median of all
  valid observations; per-regime medians over valid observations;
  regime-specific tau iff ≥ 2 regimes have valid observations, all regime
  medians > 0, and `max/min >= 2.0`; otherwise pooled. A non-positive minimum
  regime median makes the ratio undefined (reported explicitly).
- **Measured tau (72/72 valid observations; 9 per direction; seconds of
  equivalent 5 MiB completion time):**

  | Direction | Pooled tau | Regime medians (degraded / good / recovering) | Aggregation |
  |---|---|---|---|
  | 1→2 | 0.123 | 0.017 / 0.123 / 1.064 | **regime-specific** (ratio 63.5) |
  | 2→1 | 0.569 | 0.341 / −0.097 / 5.928 | pooled (ratio undefined) |
  | 2→4 | 0.096 | 0.548 / −0.072 / 0.048 | pooled (ratio undefined) |
  | 4→2 | 0.009 | 0.014 / −0.444 / 0.340 | pooled (ratio undefined) |
  | 4→8 | 0.157 | 1.823 / −0.122 / −0.502 | pooled (ratio undefined) |
  | 8→4 | −0.130 | −1.434 / −0.165 / −0.011 | pooled (ratio undefined) |
  | 8→16 | −0.125 | −1.710 / 0.845 / −0.132 | pooled (ratio undefined) |
  | 16→8 | 0.026 | 0.619 / −0.132 / −0.017 | pooled (ratio undefined) |

  Only 1→2 is regime-dependent under the frozen 2.0 rule; its largest
  per-regime median is 1.06 s. All other directions use pooled taus with
  |tau| ≤ 0.57 s. Individual observations range from −16.5 s to +23.8 s
  (scheduling/in-flight noise in single windows; negative values are
  preserved, never clamped). The small magnitudes are consistent with the
  shared-link observation that per-epoch delivered bytes are nearly
  C-independent: switching C costs little beyond in-flight drain/ramp.
- **No headroom yet:** `T_fixed(C)`, `T_best_fixed`, the instantaneous
  diagnostic, the constrained oracle, and `H_j` were **not** computed; the
  next task performs the oracle/headroom analysis with these frozen tau
  values. No RL/adaptive control/aria2 was run, and no fixed-C pilot was
  rerun.
- **Artifacts:** `testdata/oracle_preregistration_v2/` (protocol);
  `/home/vscode/tau_campaign_v2/` — `plan/` (frozen plans + hashes), `bin/`,
  `runs/<trace>_e<idx>_<regime>_<from>to<to>/` (72 run dirs with
  `run_result.json`, `summary.json`, `events.jsonl`, `up.log`, `down.log`,
  `output.bin`), `campaign_jobs.json`, `campaign_log.jsonl`,
  `tau_observations.jsonl`, `tau_aggregation.json`, `tau_measurements.json`,
  `tau_report.txt`, `tau_manifest.json`. `/home/vscode/oracle_stage_v2/
  tau_measurements.json` was updated from the blocked placeholder to the
  measured summary (headroom still absent).
- **Tests / validation:** new deterministic tests cover B_e identity across C,
  `t = B_e/R`, the allowed ladder, direct-jump rejection, one-step and dwell
  enforcement, classifier determinism and tie handling, injection selection,
  the exact 2.0 threshold, pooled and regime-specific medians, negative-tau
  preservation, the window formula, the zero-denominator case, read-only
  inputs, and the child switch mechanism. `gofmt -l .` clean; `go test ./...`,
  `go test -race ./...`, `go vet ./...`, `go build ./...` all pass. Cleanup
  verified (no mm-link/child/namespace/qdisc residue); the 24 tracked v2
  pilot files and the historical diagnostic directories are unchanged.
- **Limitations (unchanged):** n=2 traces → any headroom from these traces is
  descriptive only and cannot satisfy the final trace-level statistical gate;
  controls come from the protocol-v2 fixed-C pilot (identical windows,
  read-only) rather than a fresh control campaign, as §15 instructed to run
  only the switch-injection measurements; single-observation windows are
  noisy.

### 22.72 Formal oracle/headroom analysis (executed 2026-09-27): BLOCKED — instantaneous-bound inconsistency from frozen negative taus

**Status: `BLOCKED — ORACLE COMPUTATION INCONSISTENT`.** The formal
oracle/headroom computation was executed exactly under the frozen
pre-registration and produced the required artifacts, but the pre-registered
sanity inequality `T_instantaneous <= T_constrained` fails for the LTE trace.
The failure is a **model property of the frozen negative measured taus**, not
an implementation error (the DP was cross-checked against exhaustive
enumeration and reproduced independently). Per the task rules no value was
clamped and no formula changed. Descriptive two-trace numbers are recorded
below; no headroom claim and no statistical-gate claim is made.

- **Inputs verified:** protocol-v2 fixed-C pilot (`selected=2m0s`,
  `passed=true`), 18 shared-valid 120 s epochs (EVDO 7, LTE 11), frozen
  preregistration v1, frozen tau aggregation (72/72 valid observations). All
  24 tracked v2 pilot files unchanged. No new network experiment, no fixed-C
  rerun, no tau rerun.
- **Workload interpretation:** counterfactual completion-time equivalents for
  a common per-epoch workload `B_e = 5 MiB` (same for every C); they are not
  measured end-to-end download times and the constrained oracle is not an
  adaptive execution.
- **Fixed-C completion equivalents (seconds):**

  | Trace | C=1 | C=2 | C=4 | C=8 | C=16 | T_best_fixed | best C (descriptive) |
  |---|---|---|---|---|---|---|---|
  | EVDO | 548.142 | 546.228 | 545.684 | **543.982** | 547.817 | 543.982 | C8 |
  | LTE | 70.423 | 68.811 | 68.769 | 68.880 | **68.520** | 68.520 | C16 |

- **Diagnostic instantaneous oracle:** EVDO 534.806 s; LTE 68.261 s
  (diagnostic only; ignores tau/ladder/dwell).
- **Formal constrained oracle (frozen ladder/dwell; regime label of the
  entered epoch):** EVDO T_constrained **535.240 s**, 3 transitions, sequence
  `[4,4,8,8,4,4,8]`; LTE T_constrained **68.216 s**, 5 transitions, sequence
  `[8,16,16,8,8,16,16,16,8,8,4]`. Both sequences validate against the ladder,
  one-step limit, and dwell rule.
- **Tau values actually used (frozen):** 1→2 regime-specific (degraded
  0.0168 s, good 0.1228 s, recovering 1.0643 s; middle epochs fall back to
  the pooled 0.1228 s); all other directions pooled (2→1 0.5690, 2→4 0.0963,
  4→2 0.0089, 4→8 0.1566, **8→4 −0.1303**, **8→16 −0.1249**, 16→8 0.0256).
  Negatives were preserved, never clamped.
- **Descriptive two-trace pilot headroom:** EVDO **H = 1.61 %**; LTE
  **H = 0.44 %** (trace = statistical unit; no pooling across epochs).
- **Secondary negative-tau sensitivity (not the formal result):** replacing
  every negative tau with 0 gives EVDO H = 1.58 % (delta −0.02 pp) and LTE
  H = 0.047 % (delta −0.40 pp). The zero-tau diagnostic gives EVDO 1.64 %
  and LTE 0.17 %. The LTE formal headroom therefore depends materially on the
  negative measured taus.
- **Sanity diagnosis (why blocked):** `T_constrained <= T_best_fixed` holds
  for both traces, and the selected sequences are ladder/dwell-valid. The
  diagnostic `T_instantaneous <= T_constrained` bound holds for EVDO but
  fails for LTE (68.2607 > 68.2160). Mathematically, the instantaneous oracle
  is a lower bound only when transition costs are non-negative:
  `T_constrained = Σ t[C_e,e] + Σ tau`, and the frozen negatives (e.g.
  8→4, 8→16) let the DP trade a small rate loss for a negative switching
  "gain". The exhaustive brute-force audit reproduces the Go DP objectives
  exactly (EVDO 535.239802396, LTE 68.215955037), so the computation is
  correct; the inconsistency is between the frozen negative-tau rule and the
  task's assumed bound. The nonnegative-tau sensitivity restores the bound.
- **Statistical status:** n = 2 traces. `FINAL_STATISTICAL_GATE = PENDING`
  (requires ≥5 usable diverse traces and `CI_95_lower(H) >= 0.10`). The
  two-trace result is descriptive feasibility evidence only; no generalization
  is claimed.
- **Artifacts:** `/home/vscode/oracle_stage_v2/` — `fixed_c_completion.json`,
  `instantaneous_oracle.json`, `constrained_oracle.json`, `headroom.json`,
  `tau_used.json`, `oracle_sequences.json`,
  `sensitivity_nonnegative_tau.json`, `sensitivity_zero_tau.json`,
  `oracle_tables.txt`, `oracle_analysis.txt`, `oracle_manifest.json`
  (input + preregistration hashes, protocol, epoch, trace IDs), plus the
  retained `input_verification.json` / `shared_valid_epochs.jsonl`. The
  analysis is byte-identical across two runs.
- **Implementation:** `internal/experiment/tau/oracle.go` (fixed,
  instantaneous, exact DP with ladder/dwell, regime-aware tau lookup,
  nonnegative/zero-tau variants) + `cmd/oracle` (analysis CLI); tests include
  an exhaustive brute-force cross-check over random small instances.
- **What must happen next (separately pre-registered; not decided here):** a
  decision on how the formal oracle treats negative measured taus (for
  example a pre-registered floor at zero before the oracle, or redefining the
  diagnostic bound) so that the sanity inequality is restored without
  silently clamping; then the descriptive analysis can be rerun and the
  ≥5-trace statistical gate pursued. Historical sections are unchanged.

### 22.73 Nonnegative formal transition-cost model, protocol version 2 (executed 2026-09-27)

**Status: `TAU_COST_MODEL_V2_READY_FOR_ORACLE`.** The signed-tau
specification was replaced by an explicitly versioned nonnegative formal-cost
rule, frozen before a fresh switch-injection campaign. All formal tau values
are ≥ 0; raw signed observations are preserved; no headroom was computed and
the fixed-C v2 pilot was not rerun. Historical results (§22.71 signed-tau
estimates, §22.72 signed-tau oracle analysis) are unchanged and remain
historical/exploratory.

- **Why the signed model was rejected as a formal cost:** `tau` is defined as
  a transition *cost*, and the version-1 estimator permitted negative
  estimates (v2 campaign: 8→4 = −0.1303 s, 8→16 = −0.1249 s). A negative cost
  acts as a switching reward, which made the diagnostic lower bound fail
  (§22.72: LTE `T_instantaneous` 68.2607 s > `T_constrained` 68.2160 s). The
  DP was verified correct by exhaustive enumeration, so the fix is a
  specification revision, not a computation fix.
- **New formal rule (protocol version 2):** the estimator is unchanged
  (pooled = median of valid observations; regime-specific only if the frozen
  rule fires: ≥2 regimes, all medians > 0, max/min ≥ 2.0). The nonnegative
  transform is applied **after** the estimator:
  `tau_formal = max(0, estimated_tau)`, and
  `tau_formal(from,to,regime) = max(0, regime_median)` where regime-specific.
  Individual raw observations are never clamped; no absolute values; no
  means; raw negatives stay visible as diagnostics. This guarantees
  `tau_formal >= 0`, so the oracle can enforce
  `T_instantaneous <= T_constrained <= T_best_fixed`.
- **New pre-registration:** `testdata/oracle_preregistration_v3/`
  (`oracle_preregistration.json` + `.txt`), `oracle_protocol_version = 2`,
  superseding version 1 (`testdata/oracle_preregistration_v2/`, retained).
  All other definitions are unchanged: B_e = 5 MiB, `t[c,e] = B_e/R[c,e]`,
  120 s epoch, ladder `1↔2↔4↔8↔16`, one step per boundary, dwell rule,
  instantaneous/best-fixed/constrained oracle definitions, H formula,
  trace-level statistical unit, native regime classifier, injection
  timestamps, 2.0 threshold, median definition.
- **Fresh campaign (formal dataset):** `/home/vscode/tau_campaign_v3/` — the
  same switch-injection design, frozen timestamps and 8 directions, 120 s
  window, protocol-v2 networking (no request/dial deadlines, 45 m watchdog),
  matched protocol-v2 fixed-C target control (read-only). Plan JSONs are
  byte-identical to the v2 plans (same classifier/timestamps). **72/72 runs
  valid, 0 failures, 0 retries**, finished 2026-09-27 19:47 UTC.
  `/home/vscode/tau_campaign_v2/` is retained as signed-tau
  historical/exploratory evidence and was not modified.
- **Raw evidence:** 72 valid observations, **32 raw negative observations**
  preserved per direction (1→2: 3, 2→1: 5, 2→4: 4, 4→2: 4, 4→8: 4, 8→4: 5,
  8→16: 4, 16→8: 3). Raw negatives are diagnostics only; they are never
  clamped before the estimator.
- **Formal tau (v3, seconds of equivalent 5 MiB completion time):**

  | Direction | v3 signed pooled estimate | Formal tau (max(0,·)) | v2 signed estimate (historical) | Regime rule in v3 |
  |---|---|---|---|---|
  | 1→2 | 0.0615 | 0.0615 | 0.1228 | pooled (ratio undefined: good median −0.3147) |
  | 2→1 | −0.1555 | **0.0000** | 0.5690 | pooled (ratio undefined) |
  | 2→4 | 0.0021 | 0.0021 | 0.0963 | pooled (ratio undefined) |
  | 4→2 | 0.0184 | 0.0184 | 0.0089 | pooled (ratio undefined) |
  | 4→8 | 0.0905 | 0.0905 | 0.1566 | pooled (ratio undefined) |
  | 8→4 | −0.2468 | **0.0000** | −0.1303 | pooled (ratio undefined) |
  | 8→16 | 0.0031 | 0.0031 | −0.1249 | pooled (ratio undefined) |
  | 16→8 | 0.0327 | 0.0327 | 0.0256 | pooled (ratio undefined) |

  In the fresh campaign **no direction is regime-specific**: every direction
  has at least one non-positive regime median, so the frozen ratio rule is
  undefined and falls back to pooled. Two directions (2→1, 8→4) are affected
  by `max(0,·)`; all formal values are ≥ 0. The differences versus the v2
  estimates include sampling variation between campaigns (fresh raw runs) and
  the transform.
- **No headroom yet:** `T_best_fixed`, `T_instantaneous`, `T_constrained`,
  and `H` were **not** recomputed in this task. The next stage reruns the
  frozen oracle analysis using the v3 formal tau table; with nonnegative
  costs the sanity inequalities are guaranteed.
- **Artifacts:** `testdata/oracle_preregistration_v3/` (protocol version 2);
  `/home/vscode/tau_campaign_v3/` — `plan/` (plans + binary/plan hashes),
  `bin/`, `runs/` (72 run dirs), `campaign_jobs.json`, `campaign_log.jsonl`,
  `campaign_done.json`, `tau_observations.jsonl` (signed raw evidence),
  `tau_aggregation.json` (signed estimates + formal values),
  `tau_measurements.json`, `tau_report.txt`, `tau_manifest.json`,
  `tau_comparison_v2_vs_v3.json`/`.txt` (old vs revised), runner/analyzer/
  comparison scripts.
- **Tests / validation:** new unit tests prove negative estimated tau → formal
  0 and positive estimated tau unchanged, that aggregation preserves the
  signed median while setting the formal value, and that `TauTable.Formalized`
  floors estimates without touching the original table. `gofmt -l .` clean;
  `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...` all
  pass. Cleanup verified (no mm-link/child/namespace/qdisc residue); the v2
  pilot (24 hashed files) and the v2 tau campaign are unchanged.
- **Limitations:** n = 2 traces (descriptive only; final ≥5-trace statistical
  gate still pending); fresh raw observations differ from the historical
  campaign, so old-vs-new differences mix sampling variation with the
  transform; single-window observations remain noisy.

### 22.74 Formal oracle/headroom under protocol version 2 (executed 2026-09-27): descriptive two-trace headroom

**Status: `ORACLE_STAGE_V3_COMPLETE — DESCRIPTIVE_HEADROOM_COMPUTED`.** The
formal oracle/headroom computation ran with the frozen nonnegative formal tau
table (oracle protocol version 2). All sanity inequalities hold for both
traces; the result is descriptive only and the final statistical gate is
pending. No network experiment, no fixed-C rerun, no tau rerun.

- **Inputs verified:** protocol-v2 fixed-C pilot (`selected=2m0s`,
  `passed=true`; 24 hashed files unchanged), 18 shared-valid 120 s epochs
  (EVDO 7, LTE 11), preregistration v3 hash matches the manifest, v3 campaign
  plans match their hashes, 72/72 valid tau observations. Formal tau is the
  v3 `formal_tau_seconds` table (nonnegative); the historical v2 signed table
  is used only for the labelled diagnostic.
- **Fixed-C completion equivalents (seconds; B_e = 5 MiB; counterfactual
  completion-time equivalents, not measured download times):**

  | Trace | C=1 | C=2 | C=4 | C=8 | C=16 | T_best_fixed | best C (descriptive) |
  |---|---|---|---|---|---|---|---|
  | EVDO | 548.137 | 546.228 | 545.677 | **543.982** | 547.825 | 543.982 | C8 |
  | LTE | 70.416 | 68.814 | 68.772 | 68.876 | **68.520** | 68.520 | C16 |

  (Full-precision values from `oracle_stage_v3/fixed_c_completion.json`; the
  §22.72 section's table was rounded at the third decimal and is left as
  historical record — T_best_fixed is identical.)

- **Diagnostic instantaneous oracle:** EVDO 534.806 s; LTE 68.261 s
  (ignores tau/ladder/dwell; not the formal gate).
- **Formal tau (v3, seconds):** 1→2 0.0615, 2→1 0.0000, 2→4 0.0021,
  4→2 0.0184, 4→8 0.0905, 8→4 0.0000, 8→16 0.0031, 16→8 0.0327 (all pooled;
  no direction is regime-specific in the fresh data).
- **Formal constrained oracle:** EVDO T_constrained **535.238 s**, 3
  transitions, sequence `[4,4,8,8,4,4,8]`; LTE T_constrained **68.498 s**, 3
  transitions, sequence `[8,16,16,16,16,16,8,8,4,4,4]`. Both sequences obey
  ladder adjacency, the one-step rule, and dwell.
- **Sanity inequalities (both traces pass):**
  `T_instantaneous <= T_constrained <= T_best_fixed` — EVDO
  534.806 ≤ 535.238 ≤ 543.982; LTE 68.261 ≤ 68.498 ≤ 68.520. With the
  nonnegative formal tau the §22.72 inconsistency no longer occurs.
- **Descriptive two-trace headroom (trace = statistical unit):** EVDO
  **H = 1.6075 %**; LTE **H = 0.0320 %**. Both are below the preregistered
  10 % target; descriptively only.
- **Historical signed-tau diagnostic (NOT FORMAL):** reproducing §22.72 with
  the v2 signed table gives EVDO H = 1.6072 %, LTE H = 0.4436 % (exact
  historical match). Using the fresh v3 signed estimates gives EVDO 1.6529 %,
  LTE 0.9046 %. The formal nonnegative model gives a much smaller LTE value
  because it cannot exploit negative transition estimates.
- **Zero-tau diagnostic (NOT FORMAL):** EVDO H = 1.6408 %, LTE H = 0.1740 %
  (rate-only headroom with costless switching).
- **Statistical status:** n = 2 traces → **FINAL_STATISTICAL_GATE =
  PENDING** (requires ≥5 usable diverse traces and `CI_95_lower(H) >= 0.10`).
  No generalization, significance, or Stage 0 PASS is claimed.
- **Artifacts:** `/home/vscode/oracle_stage_v3/` — `fixed_c_completion.json`,
  `instantaneous_oracle.json`, `constrained_oracle.json`, `headroom.json`,
  `tau_used.json`, `oracle_sequences.json`, `sensitivity_signed_tau.json`
  (historical v2 + fresh v3 signed diagnostics), `sensitivity_zero_tau.json`,
  `oracle_tables.txt` (Tables 1–7), `oracle_analysis.txt`,
  `oracle_manifest.json` (fixed-C input hashes, prereg hash, protocol
  versions, epoch, trace IDs). The complete analysis is byte-identical across
  two runs. `cmd/oracle` was extended to consume the formal v3 table and emit
  the signed/zero diagnostics; the §22.72/§22.73 artifacts are untouched.
- **Validation:** `gofmt -l .` clean; `go test ./...`, `go test -race ./...`,
  `go vet ./...`, `go build ./...` all pass; no mm-link/child/namespace/qdisc
  residue; the v2 pilot, v3 campaign inputs, and historical directories are
  unchanged.
- **Interpretation:** the analysis asks whether the measured fixed-C rate
  variation leaves theoretically available adaptive headroom after the
  measured nonnegative transition-cost model. Under this model the
  descriptive answer is ~1.6 % (EVDO) and ~0.03 % (LTE), both far below the
  10 % target; the constrained sequence is a counterfactual oracle, not a
  learned or executed policy.

### 22.75 Final Stage-0 trace-expansion decision gate (preparation/audit only; superseded by §22.76)

**Superseded by §22.76:** every item recorded below as UNFROZEN is now frozen
in the final trace protocol (`testdata/oracle_preregistration_v4/
final_trace_protocol.json`). The audit text describes the pre-freeze state and
is retained as historical record.

**Status at audit time: `BLOCKED — FINAL TRACE PROTOCOL INCOMPLETE`.** A
read-only audit of the repository determined what is actually frozen for the
final ≥5-trace statistical gate. No network experiment, no fixed-C replay, no
tau replay, and no new headroom were performed; the two-trace descriptive
results (EVDO 1.6075 %, LTE 0.0320 %) are unchanged. Planning artifacts are
under `/home/vscode/final_trace_expansion_decision/`.

- **Frozen evidence (valid):** protocol-v2 fixed-C pilot
  (`/home/vscode/epoch_pilot_v2`, 18 shared-valid 120 s epochs on the two
  current traces); oracle protocol version 2
  (`testdata/oracle_preregistration_v3/`: B_e = 5 MiB, `t = B_e/R`, 120 s
  epoch, ladder/dwell, native regime classifier, 2.0 threshold, tau estimator
  + `max(0, estimate)` formal rule, `H_j` formula, statistical unit = trace);
  formal tau campaign `/home/vscode/tau_campaign_v3/` measured **on the two
  current traces only**.
- **Audit results (question by question):**
  - **Trace count — NOT PREREGISTERED at audit time (now frozen at 5 in §22.76).** The string "≥5 usable diverse traces
    with `CI_95_lower(H) >= 0.10`" appears only in analysis output
    (`cmd/oracle` strings, `oracle_stage_v3/headroom.json`) and in
    §22.72/§22.74/§27 of this document. `oracle_preregistration_v3` itself
    says only "final trace-level gate pending more traces" — no frozen
    minimum count and no definition of "usable".
  - **Diversity — UNFROZEN at audit time (now frozen in §22.76).** No frozen strata, no carrier/radio/mobility
    requirement, no duplicate-family rule, and no rule stating whether the two
    current traces count toward diversity.
  - **Additional trace identities — UNFROZEN at audit time (now frozen in §22.76).** No authoritative list exists.
    The installed native corpus contains five full-length (≥450 s) families:
    the two current (`Verizon-EVDO-driving`, `Verizon-LTE-driving`) plus
    `ATT-LTE-driving` (786.1 s), `TMobile-LTE-driving` (474.6 s),
    `TMobile-UMTS-driving` (928.4 s). The short variants
    (`ATT-LTE-driving-2016` 120 s, `TMobile-LTE-short` 140 s,
    `Verizon-LTE-short` 140 s) fall below the epoch pilot's ≥450 s
    trace-selection rule; that rule is a **pilot** rule, not a frozen
    final-population rule. Candidate set is recorded as
    **PROPOSED — NOT YET FROZEN**; nothing was selected.
  - **Tau scope — UNFROZEN at audit time (now frozen as PER-TRACE in §22.76).** The estimator and the nonnegative formal rule
    are frozen, but the protocol is silent on whether tau for an expanded
    population is per-trace, pooled across traces, measured on a calibration
    subset, or another design. The existing v3 campaign covers only the two
    current traces, so its external validity for new traces is unresolved.
  - **Regime classifier — FROZEN and applicable.** The classifier (native
    downlink only; full 120 s epochs; k = max(1, floor(n/3)); degraded = k
    lowest; good = k highest not degraded; recovering = `rate[e] ≥
    1.25·rate[e−1]` and not degraded; precedence degraded → recovering → good
    → middle; injection = earliest + next distinct per regime) applies
    deterministically to the proposed candidates: `ATT-LTE-driving` 4
    timestamps, `TMobile-LTE-driving` 2, `TMobile-UMTS-driving` 5.
  - **Final statistical gate — NOT PREREGISTERED.** Only `H_j` and unit =
    trace are frozen. Missing: population-mean estimator, sample standard
    deviation, t-based 95 % CI definition (one- vs two-sided, df),
    lower-confidence-bound definition, missing/invalid-trace handling, and
    multiplicity rules.
  - **Reuse rules — UNFROZEN at audit time (now frozen in §22.76).** `epoch_pilot_v2` is valid for the two current
    traces; `tau_campaign_v3` is valid for the two current traces. Whether
    either may be reused for the expanded population (and under which tau
    scope) is not frozen. Historical datasets
    (`epoch_pilot`, `epoch_pilot_corrected`, `evdo_timeout_diag`,
    `tau_campaign_v2`) remain excluded from formal calculations.
  - **Admissibility / duration / contamination — UNFROZEN at audit time (now frozen in §22.76).** No
    final-population admissibility criteria, duration requirement, or
    contamination/exclusion rules are recorded.
- **Minimum new work (conditional only, if the recorded aspiration held and
  the two current traces were retained):** 3 additional trace families ×
  5 C = **15 protocol-v2 full-trace fixed-C replays**; tau work depends on the
  unfrozen scope — **0** new runs if tau were pooled from v3 (external
  validity unresolved) or **88** switch-injection runs if per-trace
  (32 + 16 + 40 for the proposed candidates). No wall-clock estimate is made.
- **Note on a different requirement:** the M1/M2 line's "≥5 documented real
  mobility traces" (§22.48–§22.50) belongs to the separate, stopped
  mobile-trace acquisition/replay line and must not be conflated with this
  Mahimahi Stage 0 final gate.
- **Interpretation (unchanged):** the two-trace headroom (EVDO ≈ 1.61 %,
  LTE ≈ 0.03 %) is descriptive only. Additional diverse traces would test
  whether this small headroom is trace-specific or persists; no outcome is
  predicted, and the project framing is **not** changed on this basis.
- **Artifacts:** `/home/vscode/final_trace_expansion_decision/` —
  `protocol_audit.json`/`.txt`, `trace_candidates.json` (corpus scan +
  PROPOSED candidates), `reuse_rules.json`, `tau_scope.json`,
  `final_statistical_gate.json`, `experiment_matrix.json`. Planning/audit
  only; no measured data.
- **Required before any expansion run:** pre-register (in the repository) the
  final trace count, diversity strata, admissibility/duration/contamination
  rules, the exact trace identities, the tau scope, the reuse rules, and the
  exact statistical formula and pass criterion. Until then the gate remained
  `PENDING` and expansion was blocked. **(Now resolved: §22.76 freezes all of
  these; expansion is ready.)**

### 22.76 Final trace protocol (frozen, prospective; documentation only)

**Status: `FINAL_TRACE_PROTOCOL_FROZEN — READY FOR FINAL TRACE EXECUTION`.**
This section converts the §22.75 audit into an explicit prospective
pre-registration. It is documentation only: no network experiment, no fixed-C
replay, no tau campaign, no headroom recomputation, and no source change were
performed. The frozen artifact is
`testdata/oracle_preregistration_v4/final_trace_protocol.json` (+ `.txt`);
`oracle_preregistration_v3` (oracle protocol version 2) remains unchanged and
is referenced, not modified. Existing descriptive results
(EVDO 1.6075 %, LTE 0.0320 %) are unchanged.

- **Final trace population (frozen, n = 5):**
  1. `Verizon-EVDO-driving`
  2. `Verizon-LTE-driving`
  3. `ATT-LTE-driving`
  4. `TMobile-LTE-driving`
  5. `TMobile-UMTS-driving`

  Excluded: `ATT-LTE-driving-2016` (120 s), `TMobile-LTE-short` (140 s),
  `Verizon-LTE-short` (140 s), all historical datasets, and any other corpus
  member. The population spans Verizon EVDO/LTE, ATT LTE, and TMobile
  LTE/UMTS; each family is counted once, and the two existing traces are
  retained.
- **Trace eligibility (frozen):** full-length duration ≥ 450 s; valid paired
  `.up`/`.down` family; genuine mm-link EOF/endpoint validation must pass;
  no fatal child error; no trace contamination per the existing checks
  (`epoch.ExclusionTraceContamination`, `anchor_alignment`); one count per
  family; technical execution failures may be rerun under the same protocol;
  a scientifically ineligible trace is not silently replaced; no post-hoc
  substitution based on observed H.
- **Replay / oracle rules (frozen, existing):** protocol-v2 watchdog-only
  transport; HTTP/1.1; C = {1,2,4,8,16}; 1 MiB fixed chunk; full-trace
  uninterrupted fixed-C replay with continuous backlog from t=0 to EOF;
  timestamped byte-delivery measurement through the frozen anchor
  (StartupOffset 60 ms, AlignmentTolerance 100 ms); 120 s epochs; 5 MiB byte
  floor per C per epoch; minimum observed-duration fraction 0.80; shared epoch
  validity across all C; final partial epoch excluded; exact existing regime
  classifier; exact existing injection timestamp selection; one ladder step
  per boundary; dwell of at least one complete following epoch after a change.
- **Tau scope (frozen): PER-TRACE.** Rationale: tau is a transition-cost
  property of the replayed trace/environment, so the existing two-trace tau
  measurements are reusable only for those two traces; each newly added trace
  requires its own tau measurement rather than assuming pooled cross-trace
  transferability. The estimator is the existing frozen one (retain signed
  raw observations; median rule; regime-specific iff ≥2 regimes with valid
  observations, all medians > 0, max/min ≥ 2.0; `formal tau = max(0, estimated
  tau)`) and must not be altered after seeing new results.
- **Reuse rules (frozen):** `epoch_pilot_v2` fixed-C data is reused for
  `Verizon-EVDO-driving` and `Verizon-LTE-driving`; `tau_campaign_v3` is
  reused only for those same two traces. `ATT-LTE-driving`,
  `TMobile-LTE-driving`, and `TMobile-UMTS-driving` require fresh protocol-v2
  fixed-C replays and fresh per-trace tau campaigns under the frozen
  estimator/classifier/injection rules. `tau_campaign_v2` is excluded, as are
  `epoch_pilot`, `epoch_pilot_corrected`, and `evdo_timeout_diag`.
- **Headroom (frozen):** `H_j = (T_best_fixed,j − T_constrained,j) /
  T_best_fixed,j`.
- **Statistical design (frozen):** one trace = one statistical observation;
  population estimator = arithmetic mean of the five trace-level `H_j`;
  variability = ordinary sample standard deviation across the five `H_j`;
  confidence bound = one-sided 95 % Student-t lower confidence bound with
  `df = n − 1` (n = 5, df = 4); exact lower bound
  `LCB_95 = mean(H) − t_(0.95, df=n−1) · s(H)/√n`; pass criterion
  `LCB_95 >= 0.10`, interpreted as a predeclared 10 % minimum
  population-level exploitable-headroom requirement.
- **Invalid / missing handling (frozen):** a technical run failure may be
  rerun under the same protocol; a trace failing a scientific eligibility
  criterion is not substituted or selectively deleted; the final statistical
  gate is not evaluated unless all five predefined families yield usable
  trace-level `H` values; if fewer than five usable traces remain, the study
  is reported as failing to reach the preregistered sample rather than
  changing n or replacing traces post hoc.
- **Experiment matrix (frozen workload counts):** 3 new traces × 5 C = **15
  protocol-v2 fixed-C full-trace replays**; per-trace tau for the three new
  traces: `ATT-LTE-driving` 4 timestamps × 8 directions = **32**,
  `TMobile-LTE-driving` 2 × 8 = **16**, `TMobile-UMTS-driving` 5 × 8 = **40**
  → **88 tau runs**; **total 103 new runs**. Timestamp counts reuse the
  already verified classifier outputs (§22.75); no timestamps were invented.
- **Current descriptive interpretation (unchanged):** the two-trace headroom
  is descriptive only; the five-trace study tests whether it is trace-specific
  or persists. No outcome is predicted and no framing change is made here.
- **Planning artifacts updated to match this freeze:**
  `/home/vscode/final_trace_expansion_decision/` —
  `protocol_audit.json`/`.txt` (`READY FOR FINAL TRACE EXECUTION`, with the
  pre-freeze audit retained), `trace_candidates.json` (frozen five-trace
  population vs excluded short variants), `reuse_rules.json`,
  `tau_scope.json` (PER_TRACE), `final_statistical_gate.json`,
  `experiment_matrix.json` (15 + 88 = 103).
- **No code or experiment changes:** this freeze is documentation only; no
  source file was modified, no measurement was rerun, and no existing frozen
  artifact was altered.

### 22.77 Stage A execution: final-trace fixed-C replays (executed 2026-09-27/28)

**Status: `STAGE_A_COMPLETE — 15/15 FIXED-C REPLAYS VALID`.** The fixed-C
portion of the frozen final trace expansion ran exactly as pre-registered.
No tau campaign, switch injection, oracle/headroom, or statistical analysis was
performed. Output dir: `/home/vscode/final_fixed_c_stage_a/` (fresh; all other
evidence untouched).

- **Protocol used (frozen v2):** watchdog-only transport (request deadline
  none, dial deadline none), parent/child watchdog 45 m, HTTP/1.1, chunk
  1 MiB, 64 GiB logical workload, C = {1,2,4,8,16}, one fresh full-trace
  replay per C, timestamped byte-delivery measurement, existing endpoint/EOF
  validation and child-error classification, existing cleanup. Commands:
  `go run ./cmd/epoch-pilot -trace-good <family>.down -trace-degraded
  <other>.down -only-trace <family> -output-dir /home/vscode/final_fixed_c_stage_a`
  for `ATT-LTE-driving`, `TMobile-LTE-driving`, `TMobile-UMTS-driving`.
  Window: 2026-09-27 21:17:05 → 2026-09-28 00:38:47 UTC.
- **Matrix (15 planned, 15 executed, 0 technical failures, 0 reruns):**

  | Trace | C | Outcome | Residual ms | Endpoint valid | mm-link exit | Fatal | Requests (succ/fail) |
  |---|---|---|---|---|---|---|---|
  | ATT-LTE-driving | 1 | `SUCCESS_TRACE_REACHED_EOF` | −35 | true | 0 | false | 457 (455/2) |
  | ATT-LTE-driving | 2 | `SUCCESS_TRACE_REACHED_EOF` | −56 | true | 0 | false | 467 (463/4) |
  | ATT-LTE-driving | 4 | `SUCCESS_TRACE_REACHED_EOF` | −56 | true | 0 | false | 472 (464/8) |
  | ATT-LTE-driving | 8 | `SUCCESS_TRACE_REACHED_EOF` | −57 | true | 0 | false | 476 (463/13) |
  | ATT-LTE-driving | 16 | `SUCCESS_TRACE_REACHED_EOF` | −57 | true | 0 | false | 485 (459/26) |
  | TMobile-LTE-driving | 1 | `SUCCESS_TRACE_REACHED_EOF` | −35 | true | 0 | false | 675 (674/1) |
  | TMobile-LTE-driving | 2 | `SUCCESS_TRACE_REACHED_EOF` | −34 | true | 0 | false | 693 (691/2) |
  | TMobile-LTE-driving | 4 | `SUCCESS_TRACE_REACHED_EOF` | −35 | true | 0 | false | 695 (691/4) |
  | TMobile-LTE-driving | 8 | `SUCCESS_TRACE_REACHED_EOF` | −36 | true | 0 | false | 696 (688/8) |
  | TMobile-LTE-driving | 16 | `SUCCESS_TRACE_REACHED_EOF` | −56 | true | 0 | false | 701 (685/16) |
  | TMobile-UMTS-driving | 1 | `SUCCESS_TRACE_REACHED_EOF` | −35 | true | 0 | false | 214 (213/1) |
  | TMobile-UMTS-driving | 2 | `SUCCESS_TRACE_REACHED_EOF` | −35 | true | 0 | false | 217 (215/2) |
  | TMobile-UMTS-driving | 4 | `SUCCESS_TRACE_REACHED_EOF` | −34 | true | 0 | false | 218 (214/4) |
  | TMobile-UMTS-driving | 8 | `SUCCESS_TRACE_REACHED_EOF` | −55 | true | 0 | false | 220 (212/8) |
  | TMobile-UMTS-driving | 16 | `SUCCESS_TRACE_REACHED_EOF` | −34 | true | 0 | false | 224 (208/16) |

- **Protocol verification:** every run's downlink-log header contains
  `'-timeout' 'none' '-dial-timeout' 'none'`; the driver banner shows
  `watchdog: 45m0s (watchdog-only)`, chunk 1 MiB, workload 64 GiB. No
  `result.failed.*` records; no reruns needed.
- **Eligibility:** all three new families are full-length (replay ends
  1012.5 s ATT, 475.0 s TMobile-LTE, 931.2 s TMobile-UMTS — all ≥ 450 s),
  have valid paired `.up`/`.down` files, passed genuine EOF/endpoint
  validation (residuals −34…−57 ms, all within ±100 ms), show no fatal child
  error and no workload exhaustion; each family counted once. Epoch-level
  contamination (`anchor_alignment`, `trace_contamination`) is evaluated in the
  next (per-trace oracle) stage.
- **Artifacts:** `/home/vscode/final_fixed_c_stage_a/` — per-run
  `<trace>_C<c>/{result.json, anchor.json, summary.json, events.jsonl,
  up.log, down.log, output.bin}`, per-trace driver logs,
  `stage_a_driver.log`, `run_stage_a.sh`, and the Stage-A report
  `stage_a_report.json`/`.txt`.
- **Integrity:** `epoch_pilot_v2` (24 hashed files), `tau_campaign_v3`
  (plans/manifest), `oracle_stage_v3`, and `oracle_preregistration_v4`
  (sha256 `bc418557…`) are unchanged; no source file changed during Stage A;
  cleanup verified (no mm-link/child/runner/namespace; `tc qdisc` unchanged).
- **Not done (explicitly):** no per-trace tau campaigns, no switch injection,
  no oracle/headroom, no statistical analysis, no RL/adaptive control, no
  aria2/IDM, no Verizon-EVDO/LTE reruns, no protocol/threshold change, no
  trace substitution. `H`, `T_fixed`, `T_constrained`, `tau`, CI, and the
  final gate were **not** computed.
- **Next stage:** per-trace tau campaigns for `ATT-LTE-driving` (4 timestamps
  × 8 directions = 32), `TMobile-LTE-driving` (2 × 8 = 16), and
  `TMobile-UMTS-driving` (5 × 8 = 40) under the frozen estimator/classifier/
  injection rules; then per-trace oracle analysis and the pre-registered
  five-trace `LCB_95 >= 0.10` gate.

### 22.78 Stage B boundary-0 amendment (frozen before execution, 2026-09-28)

**Status: `BOUNDARY-0 AMENDMENT FROZEN BEFORE EXECUTION — STAGE B READY`.**
The per-trace tau campaign (Stage B) was prepared but not started: inspection
of the frozen plan against the frozen harness exposed an execution
incompatibility for one preregistered injection, which was resolved
prospectively by an explicit protocol amendment. **No Stage-B run had started
when the amendment was frozen (0 of 88)**; no source file was modified, no
frozen result was altered, and no existing rule was changed except the added
boundary-0 realization.

- **Original boundary-0 specification:** the frozen injection-selection rule
  ("per regime in the order degraded, good, recovering, the earliest full
  epoch and, if available, the next distinct one") selects the
  `ATT-LTE-driving` good epoch 0 with `start_ms = 0`; the frozen matrix counts
  ATT as 4 timestamps × 8 directions = 32 runs, so a switch injection at
  trace-time 0 ms is preregistered.
- **Implementation incompatibility discovered before execution:** the frozen
  switch-injection harness cannot represent the mathematical boundary t = 0.
  The child applies a scheduled switch only when `SwitchAt > 0` and treats
  `SwitchAt = 0` as "no switch" (`internal/experiment/replay/child.go`), so
  executing the boundary-0 injection with `-switch-at 0` would leave the run
  at the source C and record `switch_applied = false`, i.e. the measured
  window would not be governed by the target C. 8 of the 88 preregistered
  runs (ATT epoch 0 × 8 directions) are affected; the blocker is recorded in
  `/home/vscode/final_tau_stage_b/stage_b_blocker.json`/`.txt`.
- **Prospective boundary-0 rule (amendment 1, frozen before execution):** a
  preregistered injection timestamp of exactly 0 ms is executed with the
  earliest fixed positive switch instant of **1 ms** (a fixed protocol
  constant, not chosen from observed results); the run still starts at the
  source C; the switch must actually be applied and recorded as
  `switch_applied = true`; the target C therefore governs the complete
  post-switch measurement window `[0 ms, 120 000 ms)`; the run remains the
  preregistered boundary-0 injection, not a substituted timestamp. No other
  rule changed: the remaining three ATT timestamps, all 8 directions, the
  estimator, classifier, dwell rule, epoch rule, and statistical gate are
  unchanged; the matrix remains ATT 4 × 8 = 32 and 88 tau runs in total.
- **Amendment artifacts:** `testdata/oracle_preregistration_v4/
  final_trace_protocol.json` — new top-level `protocol_amendment`
  (`amendment_version: 1`, `status: FROZEN_BEFORE_EXECUTION`,
  `original_protocol_sha256: bc418557…`, `stage_b_runs_started_before_amendment:
  0`); `.txt` — appended amendment section (original protocol text preserved
  as historical record); `/home/vscode/final_trace_expansion_decision/
  boundary_zero_amendment.json` (new planning record);
  `protocol_audit.json`/`.txt`, `experiment_matrix.json`, and `tau_scope.json`
  updated with the amendment reference. The recorded original JSON hash is
  `bc4185579321420efe43e13234e23ceb08d44abb731805de2ed75e011a8c4c51`.
- **Not done:** no tau run was executed (0/88), no fixed-C replay was rerun, no
  source code was modified, no frozen result was altered, and no oracle/H/CI/
  gate computation was performed. Amended protocol status:
  `FINAL_TRACE_PROTOCOL_FROZEN (v1 + amendment 1) — READY FOR STAGE B`.

### 22.79 Stage B execution: per-trace tau campaign (executed 2026-09-28)

**Status: `STAGE_B_COMPLETE — 88/88 VALID`.** The preregistered per-trace tau
campaign for the three new traces ran exactly under protocol v2 plus
amendment 1. Window: 2026-09-28 02:18:40 → 04:49:17 UTC. Output dir:
`/home/vscode/final_tau_stage_b/` (fresh; all other evidence untouched). No
oracle/headroom, CI, or statistical-gate computation was performed.

- **Matrix (88 planned, 88 valid, 0 technical failures, 0 reruns):**
  `ATT-LTE-driving` 4 timestamps × 8 directions = 32/32 valid;
  `TMobile-LTE-driving` 2 × 8 = 16/16; `TMobile-UMTS-driving` 5 × 8 = 40/40.
  All 8 frozen directions occurred for every selected timestamp; the observed
  (trace, epoch, regime, start_ms, direction) set equals the frozen plan set —
  no substitution.
- **Boundary-0 rule applied (amendment 1):** the 8 `ATT-LTE-driving` good
  epoch-0 runs (preregistered `start_ms = 0`) executed with the fixed 1 ms
  switch instant; every one recorded `switch_at_ns = 1 000 000`,
  `switch_applied = true`, and the target C governed the complete
  `[0, 120 s)` window. They remain the preregistered boundary-0 injections
  (not treated as failures or substitutions).
- **Execution evidence:** every run ended `WATCHDOG_TIMEOUT` with a non-fatal
  child cancellation (`child_status = canceled`, `mm_link_exit = 0`), no fatal
  child error, no workload exhaustion, and watchdog = switch + 120 s + 15 s
  (child self-watchdog +2 min). Request/dial deadlines were the frozen
  defaults (`none`); the runner passed no timeout flags.
- **Raw signed observations:** preserved in
  `tau_observations.jsonl` and per-trace files; 35 of 88 observations are
  negative (range −6.979 s to +38.965 s), never clamped or rerun.
- **Formal per-trace tau (frozen median + `max(0, estimate)`, no cross-trace
  pooling; seconds of equivalent 5 MiB completion time):**

  | Trace | 1→2 | 2→1 | 2→4 | 4→2 | 4→8 | 8→4 | 8→16 | 16→8 |
  |---|---|---|---|---|---|---|---|---|
  | ATT-LTE-driving | 0.722 | 0.087 | 0.154* | 0.136 | 0.233 | 0.000 | 0.000 | 0.114 |
  | TMobile-LTE-driving | 0.155 | 1.219* | 0.071* | 0.000 | 0.365* | 0.078 | 0.000 | 0.001 |
  | TMobile-UMTS-driving | 0.000 | 0.281* | 0.000 | 0.029 | 0.442 | 0.000 | 0.148 | 0.221 |

  Values are the frozen `formal_tau_seconds`; `*` marks directions the frozen
  estimator resolved regime-specifically (ATT 2→4 ratio 4.96; TMobile-LTE 2→1
  3.36, 2→4 3.49, 4→8 3.84; UMTS 2→1 69.54; per-regime medians are recorded
  in `tau_aggregation_<trace>.json`). Pooled estimates for the zeroed
  directions were negative (−0.008 to −0.215 s).
- **Artifacts:** `/home/vscode/final_tau_stage_b/` — 88 run dirs
  (`run_result.json`, `summary.json`, `events.jsonl`, `up.log`, `down.log`,
  `output.bin`, `runner.log`), `campaign_jobs.json`,
  `campaign_manifest.json`, `campaign_log.jsonl`, `campaign_done.json`,
  `tau_observations.jsonl`, per-trace `tau_observations_*.jsonl`,
  per-trace `tau_aggregation_*.json`, `stage_b_report.json`/`.txt`
  (34/34 verification checks pass), plus the retained blocker record and the
  amended-plan copy.
- **Integrity:** amended preregistration unchanged during Stage B
  (json `3b371c3f…`, txt `1062f993…`); Stage-B harness binary is the frozen
  `dde3c742…`; `tau_campaign_v3` (binary/plans/manifest) and Stage-A evidence
  show no file newer than the Stage-B start; no `.go` source changed; cleanup
  verified (no mm-link/tau-injection processes, no network namespaces, no
  qdisc residue, host links unchanged).
- **Not done (explicitly):** no oracle DP, no `T_fixed`/`T_constrained`, no
  `H`, no confidence intervals, no LCB gate, no cross-trace pooling, no
  fixed-C reruns, no `tau_campaign_v3` reruns, no timestamp substitution.
- **Next stage (Stage C, not started):** per-trace oracle analysis for the
  three new traces and the pre-registered five-trace `LCB_95 >= 0.10` gate.

### 22.80 Stage C final analysis: frozen oracle + final statistical gate (executed 2026-09-28)

**Status: `FINAL_ORACLE_ANALYSIS_COMPLETE — FINAL STATISTICAL GATE FAIL`
(`LCB_95 = −0.0026077 < 0.10`).** The pre-registered final analysis ran under
oracle protocol version 2 + final trace protocol v1 + amendment 1 using only
the frozen evidence (no experiment, no source change, no preregistration
change). Output dir: `/home/vscode/final_oracle_analysis/`. The two-trace
descriptive results (§22.74) are unchanged and remain historical.

- **Epoch rule applied (documented frozen-rule interpretation):** the retained
  oracle epoch set is the shared-valid **full 120 s epochs of the native
  downlink trace** ("native downlink trace semantics" + "final partial epoch
  excluded"). `ATT-LTE-driving` e6 and `TMobile-LTE-driving` e3 are
  shared-valid *replay* epochs (slicing is over `max(up,down)` per §22.63) but
  are the native traces' final partial epochs and therefore have **no frozen
  regime label**, so they cannot enter the constrained oracle (the frozen
  implementation rejects unlabeled retained epochs). They are documented as
  excluded; the choice was surfaced before finalizing and the gate outcome is
  FAIL under either reading.
- **Per-trace tau (PER-TRACE scope, no cross-trace pooling):** `EVDO` and
  `LTE` re-aggregated per trace from the v3 raw signed observations with the
  frozen median/`max(0,·)` estimator; the three new traces use the Stage-B
  per-trace aggregations. Raw signed observations preserved (counts:
  EVDO 32/18 negative, LTE 40/14, ATT 32/12, TMobile-LTE 16/4, UMTS 40/19;
  ranges as low as −16.50 s, as high as +38.97 s).
- **Results (seconds; B_e = 5 MiB; counterfactual completion-time
  equivalents):**

  | Trace | retained | T_best_fixed (C) | T_instantaneous | T_constrained | trans. | H |
  |---|---|---|---|---|---|---|
  | Verizon-EVDO-driving | 7 | 543.982429 (C8) | 534.805587 | 535.057012 | 3 | 0.0164075465 |
  | Verizon-LTE-driving | 11 | 68.519878 (C16) | 68.260657 | 68.506557 | 3 | 0.0001943981 |
  | ATT-LTE-driving | 6 | 53.180538 (C4) | 53.063101 | 53.172222 | 1 | 0.0001563742 |
  | TMobile-LTE-driving | 3 | 12.165657 (C4) | 12.150952 | 12.158184 | 1 | 0.0006142775 |
  | TMobile-UMTS-driving | 7 | 173.111726 (C2) | 171.733363 | 172.603064 | 2 | 0.0029383433 |

  The sanity bound `T_instantaneous <= T_constrained <= T_best_fixed` holds
  for all five traces. Excluded epochs and reasons are recorded per trace in
  `final_oracle_results.json` (byte floor, observed fraction, and the native
  final partial epochs).
- **Final statistical gate (n = 5, df = 4, one-sided 95 % Student-t,
  t = 2.1318467863):** H values
  `[0.0164075465, 0.0001943981, 0.0001563742, 0.0006142775, 0.0029383433]`;
  mean `0.0040621879`; sample SD `0.0069959662`;
  `LCB_95 = 0.0040621879 − 2.1318467863 · 0.0069959662 / √5 = −0.0026077024`;
  threshold `0.10`; **FAIL**. The pre-declared 10 % population-level
  exploitable-headroom requirement is not met; no RL/adaptive/aria2
  superiority or causal real-world claim is made.
- **Validation / integrity (17/17 checks pass):** the analysis DP is a
  faithful replica of the frozen Go oracle (`oracle_replica.py` reproduces
  `oracle_stage_v3` fixed/instantaneous/constrained values to 1e-9); exactly
  five H values feed the CI; `epoch_pilot_v2`, `tau_campaign_v3`, Stage-A,
  Stage-B, and `oracle_stage_v3` evidence unchanged; preregistration v4 +
  amendment 1 hashes unchanged (`3b371c3f…` / `1062f993…`); no `.go` source
  changed; no experiment process started; no netns/qdisc residue; raw signed
  taus unaltered; no epoch excluded for rate/H reasons.
- **Artifacts:** `/home/vscode/final_oracle_analysis/` —
  `final_oracle_results.json`/`.txt`, `final_statistical_gate.json`/`.txt`,
  `final_trace_summary.json`/`.txt`, `oracle_replica.py`, `final_analysis.py`,
  per-trace `tau_aggregation_*.json` and `tau_observations_*.jsonl`
  (EVDO/LTE filtered per trace), `stage_c_decision_required.json` (the
  epoch-semantics decision record), and `scratch/` (intermediate inputs).
- **Not done (explicitly):** no network experiment, no fixed-C/tau rerun, no
  oracle reinterpretation of invalid epochs, no cross-trace tau pooling, no
  parameter tuning, no claim beyond the frozen counterfactual model.

### 22.81 Salvage Protocol v7 frozen (documentation only, 2026-09-28): BLOCKED pending human parameters

**Status: `FINAL_S_PROTOCOL_FROZEN — BLOCKED_PENDING_HUMAN_PARAMETERS`.**
A new, separate prospective study was specified for the question "When does
adaptive HTTP range downloading benefit from changing future chunk size under
finite-file, tail-aware conditions?". It is documentation only: no experiment,
no pilot, no source change, and no modification of the completed cellular
concurrency/oracle study. Artifacts:
`testdata/salvage_s_protocol_v7/` (`final_s_protocol.json`, `.txt`,
`README.md`) — deliberately outside the oracle preregistration family.

- **Frozen scope/design (complete):** single-server HTTP/1.1 contiguous
  byte-range downloads; future chunk size affects new allocations only;
  in-flight ranges keep their original boundaries. Mechanism = per-request
  server-side rate limiting at rate `R` + fixed nonzero RTT `D` (no packet
  loss, no dynamic/authored bandwidth schedules, no hidden penalties; the
  existing `internal/experiment/server` already supports a fixed rate and a
  base delay). Chunk arms 256 KiB / 1 MiB / 4 MiB / 16 MiB. Primary endpoint =
  finite-file completion time (the earlier weighted `J` objective is removed);
  secondary diagnostics listed. Bayesian hierarchical model
  `y_cb = mu_c + u_b + eps_cb` on `log(T/T_ref)` with frozen primary priors
  (`Normal(0,1.5)`, `HalfNormal(0,0.5)`, `HalfNormal(0,0.5)`) and frozen prior
  sensitivity (`Normal(0,3)`, `HalfNormal(0,1)`, `HalfNormal(0,1)`);
  `mu_fixed* = min(mu_256K, mu_1M, mu_4M, mu_16M)`; decision
  `P(Delta_k <= -delta_T | data) >= 0.95`; switching family `16->4`, `4->1`,
  `1->256K`; one switch per run with chunk size fixed afterwards; trigger
  `B_pending = TotalSize - CompletedBytes - InFlightBytes` evaluated from
  tracker state immediately before allocation; randomization within blocks;
  fixed-sequence multiplicity; confirmatory `n <= 12`; SSR via 1000 synthetic
  datasets per frozen scenario; strict pilot/confirmatory separation (no
  pooling pre-registered); trace-backed validation out of scope for v7.
- **Blocked items (no value chosen or inferred):** human-owned `R`, `D`,
  `FileSize`, `N`; the trigger firing rule/switching point; the analytic
  definition of `T_tail_primary` (and therefore `delta_tail = 0.05 *
  T_tail_primary` and the `T_tail_primary >= 20 s` check); the Stage 0S
  go/no-go rule and its designated feasibility quantities; the SSR scenario
  grid. Clarifications also required for the `delta_T` log-scale application
  (the log-scale equivalent of 5 % is `log(0.95) = -0.051293`), the
  exploratory switching case (as written it duplicates the third confirmatory
  member), and the fixed-sequence order.
- **Validation record (§24 of the task):** 11 PASS, 1 PASS-with-clarification,
  5 BLOCKED — all blocked checks correspond exactly to the missing items
  above. No parameter was substituted and no "reasonable" value was invented.
- **Path note:** the task named `/workspace/mvp`; the repository is
  `/workspaces/mvp`, and the artifacts live under
  `/workspaces/mvp/testdata/salvage_s_protocol_v7/`.
- **Next stage (not started):** the human supplies/accepts the missing
  parameters and specifications, the validation is re-run, and only then can
  the status become `FINAL_S_PROTOCOL_FROZEN — READY FOR STAGE 0S`. Stage 0S
  (the 32-run pilot) must not start before that.

