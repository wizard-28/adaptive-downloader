# Live Joint N+S Queueing Mechanism — Gate Report

**Status: `NO_LIVE-GROUNDED_JOINT_N+S_OPPORTUNITY`.** The live shared-bottleneck
mechanism gate FAILED: the mechanism produces a measurable queueing/contention cost
that grows with N, but it does **not** produce a finite/interior concurrency optimum
and no material state-dependent N*. Per the frozen stop rule, no S sweep, no adaptive
controller, and no confirmatory protocol were created.

Generated: 2026-09-29T06:27:49Z

## 1. Why the fixed D2 win is not an adaptive result

The frozen aria2-competitive benchmark established a reproducible 62.4 % completion-
time advantage for a **fixed** configuration (N=16, S=4 MiB) over fixed aria2
(split=16) under the D2 workload (geometric ratio 0.3756, 30/30 pairs). The
configuration never changes during a download, so the result is a fixed-scheduler
result, not adaptation. The joint N+S analytical screen (§22.90) then showed that the
D2 per-response pacing mechanism has no causal joint-adaptation headroom (N*=16 in
every regime; only S varies; every causal controller lost to the best fixed config).
This study tested one final mechanism branch — a live shared bottleneck with
queueing — before deciding whether genuine joint N+S adaptation is defensible.

## 2. Shared-bottleneck mechanism

Traffic path: internal downloader + independent background traffic -> one finite-
capacity FIFO bottleneck -> queueing delay, RTT inflation, drops, retransmissions.
The bottleneck is a real `tc tbf` qdisc on `lo` (rate 40 Mbit/s, burst 32 kb,
limit 256 kb; maximum queueing delay ~46 ms), with the loopback MTU temporarily set
to 1500 to emulate a realistic Ethernet-like path (restored after the study). The
mechanism is NOT merely bandwidth sharing: the required phenomenon was an increasing
queueing/inefficiency penalty with N, i.e. a finite interior N optimum.

## 3. Infrastructure (all live; no simulator)

- `tools/gate_run.go` — one real download (internal downloader, fixed N/S, logical
  32 MiB object, no application-level pacing), content-verified, with the server
  request log.
- `tools/run_gate.py` — driver: tbf apply/clear, fresh iperf3 background server+client
  per cell (retried and explicitly verified), concurrent ping RTT sampling, qdisc
  backlog/drop counters, nstat retransmission/timeout deltas, load.
- Contention states (predeclared, grounded in C): S0 B=0; S1 B=0.5C=20 Mbit/s;
  S2 B=0.85C=34 Mbit/s. Background: iperf3, 4 paced TCP streams.
- Download: 32 MiB, S=1 MiB, N in {1,2,4,8,16}, 3 reps/cell, round-robin; 45/45
  valid runs (content-verified, background verified).
- Infrastructure failures found and fixed **before** the gate measurements: lo MTU
  burst mismatch (downloads stalled), iperf3 `-b` is per stream (units), and
  intermittent iperf3 control-connection resets (cells without background). Invalid
  runs are preserved under `mechanism_gate/invalid/` and excluded.

## 4. Fixed-N gate (live results)

| state | N=1 | N=2 | N=4 | N=8 | N=16 | N* | margin |
|---|---|---|---|---|---|---|---|
| S0_no_background | 7.17 | 7.16 | 7.10 | 7.05 | 7.07 | 8 | +0.3% | |
| S1_moderate | 14.86 | 14.99 | 15.06 | 15.13 | 14.28 | 16 | +3.9% | |
| S2_heavy | 62.49 | 67.89 | 51.96 | 30.30 | 15.89 | 16 | +47.6% | |

Queueing cost (median p95 RTT, ms):

| state | N=1 | N=2 | N=4 | N=8 | N=16 | N=16/N=1 |
|---|---|---|---|---|---|---|
| S0_no_background | 57.3 | 77.0 | 78.9 | 84.5 | 93.7 | 1.64x |
| S1_moderate | 56.4 | 57.2 | 58.7 | 62.4 | 87.2 | 1.55x |
| S2_heavy | 80.0 | 73.3 | 84.7 | 88.2 | 91.7 | 1.15x |

Drops / retransmissions (medians):

| state | metric | N=1 | N=2 | N=4 | N=8 | N=16 |
|---|---|---|---|---|---|---|
| S0_no_background | qdisc drops | 0 | 44 | 111 | 204 | 338 |
| | TCP retrans | 0 | 44 | 103 | 53 | 103 |
| S1_moderate | qdisc drops | 381 | 349 | 429 | 305 | 421 |
| | TCP retrans | 269 | 289 | 185 | 243 | 321 |
| S2_heavy | qdisc drops | 812 | 859 | 612 | 421 | 315 |
| | TCP retrans | 576 | 551 | 462 | 248 | 201 |

**Interpretation.** The contention cost is real and measurable: the queue inflates
the median RTT from a 0.04 ms no-qdisc baseline to 33-86 ms, p95 up to 94 ms; drops
reach 0-860 per run; retransmissions 0-576. But the **completion time does not show
a finite interior optimum**:

- S0 (no background): flat, 7.05-7.17 s across N (N*=8 with a 0.3 % margin — noise);
  the queueing/RTT/drop cost grows with N while the completion is work-conserving.
- S1 (0.5C): flat-to-slightly-increasing 14.28-15.13 s; N*=16 with a 3.9 % margin
  (below the 5 % materiality threshold).
- S2 (0.85C): completion falls strongly with N (62.5 s at N=1 to 15.9 s at N=16):
  more downloader flows win a larger TCP share against the paced background (its
  achieved rate falls from 33.4 to 22.9 Mbit/s), so the optimum is at the boundary
  N=16, not interior.

## 5. Fixed-S gate

**NOT REACHED.** The N gate failed (no interior optimum, no material state-
dependent N*), so per the frozen stop rule the S sweep was not run.

## 6. Joint-optimum gate

**NOT REACHED** (requires the N gate and S gate to pass). No fixed-policy (N,S)
surface was measured live.

## 7. Within-download transition

**NOT REACHED.** No background start/stop transition schedule was frozen or run,
because no mechanism passes the N gate.

## 8. Live measurements (raw evidence)

- 45 valid runs in `mechanism_gate/runs/` + `mechanism_gate/meta/`; per-run ping
  samples, iperf3 interval rates, qdisc backlog/drops, nstat deltas, load.
- Background-only calibration per state in `mechanism_gate/background_only_*.json`:
  - S0_no_background: offered 0.0 Mbit/s, achieved 0.0 Mbit/s.
  - S1_moderate: offered 20.0 Mbit/s, achieved 20.1 Mbit/s.
  - S2_heavy: offered 34.0 Mbit/s, achieved 33.8 Mbit/s.
- Figures: `figures/fig1_completion_vs_N.png` (completion vs N + work-conserving
  prediction), `fig2_rtt_vs_N.png`, `fig3_drops_retrans_vs_N.png`,
  `fig4_background.png`, `fig5_model_vs_live.png`.

## 9. Analytical calibration

A deterministic shared-bottleneck model (`tools/queue_model.py`) predicts
`T(N,S|C,B) = F/(C-B_ach(N)) + S/(N(C-B_ach(N))) + k_loss*retrans*MSS/(C-B_ach(N)) + RTT_q`
with one fitted loss coefficient `k_loss=21.8`. Fitted on the 15 live cell medians:
relative RMSE **8.2 %**, max absolute relative error **18.7 %**.
The model reproduces the observed pattern: flat/boundary N* with no interior
optimum (predicted N*: S0=1, S1=4, S2=16 vs observed S0=8, S1=16, S2=16).
The work-conserving bulk term is N-independent, so no interior N optimum is
structurally expected from a shared FIFO bottleneck under TCP; the only N-dependence
is loss recovery (which grows with N but stays small relative to the bulk) and the
TCP share competition (which favors more flows under background contention).

## 10. Causal adaptive result

**NOT REACHED.** The frozen rule forbids adaptive execution when the mechanism gate
fails. No adaptive N+S controller was implemented or run, and the frozen action
space / one-switch semantics were never exercised.

## 11. aria2 comparison

**NOT REACHED** (reserved for the causal comparison only if the joint gate passes;
aria2 was not run and its frozen configuration was not touched).

## 12. Stationary control

S0 (no background) is the stationary no-contention control: the completion time is
flat across N (7.05-7.17 s, <= 1.7 %% spread) and the measured optimum margin is
0.3 %%. There is no adaptation gain to be had in the stationary state either, and
the queueing cost there (RTT 47->86 ms, drops 0->338) is not reflected in completion
time — the defining failure of this mechanism.

## 13. Exact success/failure gate

- **G-N1 interior optimum** (measurable contention cost + winning N not the largest
  + >= 5 %% margin + consistent winner): **FAIL** (no state has an interior material
  optimum; states with measurable contention cost: S0_no_background, S1_moderate, S2_heavy).
- **G-N2 state dependence** (N* differs across >= 2 states with materiality +
  consistency): **FAIL** (no material state pair; the S0 vs S1/S2 difference is a
  flat-vs-boundary artifact with < 5 %% margins on the flat side).
- **Overall: FAIL -> `NO_LIVE-GROUNDED_JOINT_N+S_OPPORTUNITY` -> STOP.**

## 14. Limitations

- One mechanism instantiation: TCP over a single FIFO token-bucket bottleneck on
  loopback (40 Mbit/s, 256 kb queue, 1500-byte MTU), with paced TCP background
  traffic. Other background classes (UDP floods, many short flows, bursty/ON-OFF)
  or smaller queues could behave differently, but the work-conserving argument that
  removes the interior N optimum is structural for a shared FIFO bottleneck.
- The container shares the host network namespace; shaping was applied only to `lo`,
  was cleared after every cell, and the MTU was restored after the study.
- The queue/MTU choices were fixed during infrastructure validation, before any gate
  measurement was used; all invalid runs are preserved and excluded.
- The analytical model is semi-empirical (one fitted loss coefficient) and is not
  used to justify any adaptive claim.

## 15. What this means for the project

The live shared-bottleneck/queueing mechanism is **INSUFFICIENT** for a genuine joint
N+S adaptation study: increasing N produces measurable queueing, RTT inflation, drops
and retransmissions, but no completion-time penalty (work conservation), so N has no
finite interior optimum and no state-dependent N*. Per the task's failure path, no
further mechanism was invented in this task. The remaining defensible competitive
result is the existing **fixed 62.4 % aria2 benchmark** (unchanged).

## 16. Artifacts

- `mechanism_gate/` (45 valid runs + metas, background-only calibrations, invalid
  infrastructure runs), `analysis/gate_analysis.json`, `calibration/model_calibration.json`,
  `figures/fig1..fig5`, `manifest/gate_design.json`, `manifest/manifest.json`,
  `tools/gate_run.go`, `tools/run_gate.py`, `tools/analyze_gate.py`,
  `tools/queue_model.py`, `tools/make_figures.py`, `tools/make_report.py`.

