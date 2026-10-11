# Correlated Loss / Range-Retry Mechanism — Analytical Gate Report

**Status: `NO_JOINT_N+S_OPPORTUNITY_UNDER_CORRELATED_LOSS`.** The correlated-loss /
range-retry mechanism produces a **material S-retry amplification** under the burst
loss condition, but the concurrency dimension remains **monotone/work-conserving** in
every condition and every predeclared parameter variant. The joint (N*,S*) optimum
therefore does **not** move (N* = 16 in both loss states). The analytical gate fails
condition 1; per the frozen rules no live mechanism gate, no adaptive execution and
no confirmatory protocol were created.

ANALYTICAL SIMULATION ONLY: no network experiment was run. All numbers are
deterministic simulator output (seeded two-state Markov loss traces), not empirical
evidence.

Generated: 2026-09-29T10:12:11Z

## 1. Prior mechanism closures (unchanged)

- Fixed D2 aria2 win: fixed N=16/S=4 MiB beats fixed aria2 split=16 by 62.4 %
  (30/30 fresh pairs). Not an adaptive result.
- Static rate+RTT S headroom: non-positive at every file size (Stage 0S GO, scaling
  and pivot closed; no state-dependent S optimum).
- Phase-locked pacing joint N+S screen: N*=16 in every regime, S only, no causal
  joint headroom (gate FAIL).
- Live shared FIFO bottleneck/queueing screen: real contention cost, work-conserving,
  no state-dependent N optimum (`NO_LIVE-GROUNDED_JOINT_N+S_OPPORTUNITY`).
- T1/M3: aggregate throughput monotone in N; no live concurrency reversal.

## 2. Why correlated loss / retry amplification is materially different

The screened mechanisms so far acted on the *transfer rate* of independent requests.
This mechanism acts on the **request failure path**: a burst-loss interval can stall a
range request past the downloader's 30 s timeout, the failed range is retried from
scratch with its original boundaries, and the wasted work grows with S (a larger
range contains more packets and has a longer transfer window, so it has a larger
exposure to bursts and a smaller timeout margin). That is a different cost channel
from per-response pacing or FIFO queueing, and it is the last mechanism branch
before the mechanism search is closed.

## 3. Parameter grounding (see `parameter_grounding/parameter_grounding.md`)

- Clean-state loss 0.4 % (D2 netem, project) / 1 % (project scenarios); severe-state
  time-averaged loss 2.5–7 % (project L1/L4 phases and DQN test conditions).
- Burst dynamics: two-state Gilbert–Elliott model, explicitly grounded in
  NTIA TM-23-565 (model controls; Gilbert's h = 0.5) and Haßlinger–Hohlfeld (MMB
  2008) measured GE parameter ranges; burst-run literature (Paxson 1997; Yajnik et
  al. 1999). The project contains **no** correlated-loss measurements; this is
  recorded explicitly and the burst parameters are swept as a predeclared
  robustness range rather than fitted.
- Frozen retry semantics: 30 s request timeout, 3 attempts, same-range retry,
  ~0.1 s retry gap, in-flight immutability; rate R = 640 KiB/s primary (the D2
  initial rate, the documented regime where the timeout actually triggered range
  retries); per-request overhead h = 0.088 s (calibrated project simulator);
  MSS = 1448 B (live-study MTU 1500).

## 4. Analytical model

Deterministic event-driven simulator (`tools/loss_sim.py`): time-indexed two-state
Markov loss process (G: loss p_G, mean sojourn T_G; B: loss p_B, mean sojourn T_B);
one contiguous range per worker; per-response pacing at R; during B the connection
stalls and recovers after the TCP RTO backoff for the burst duration; during G it
progresses at R(1-p_G). Frozen retry semantics as above. F = 512 MiB;
N ∈ {1,2,4,8,16}; S ∈ {256K,1M,4M,16M}; 10 seeds per cell; identical seed set across
cells; viability rule: a cell is viable if ≥ 50 % of seeds complete.

Conditions: **GOOD** = T_B = 0.15 s (time-averaged loss 0.42 %); **BAD** = T_B = 2.0 s
(time-averaged loss 4.6 %); p_G = 0.4 %, p_B = 0.5, T_G = 20 s in both.

## 5. Stationary N results

| condition | N=1 | N=2 | N=4 | N=8 | N=16 | N* | monotone |
|---|---|---|---|---|---|---|---|
| GOOD | 838 | 419 | 210 | 105 | 53 | 16 | yes |
| BAD | 973 | 476 | 236 | 118 | 60 | 16 | yes |

The completion is monotone decreasing in N in both conditions (ratio ≈ N per
doubling), i.e. the N dimension is work-conserving. Loss-recovery overhead (stall
time) and burst-event counts grow with the download duration, not with N; the
per-request burst exposure is N-independent. **N\* = 16 in every condition and in
all 9 predeclared robustness variants.**

## 6. Stationary S results and the range-retry mechanism check

BAD condition, aggregated over N and seeds:

| S | requests/download | packets/request | per-attempt failure rate | retries/download | retry bytes/download | download abort rate |
|---|---|---|---|---|---|---|
| 256K | 2048 | 181 | 0.0000 | 0.0 | 0.0 MiB | 0.00 |
| 1M | 512 | 724 | 0.0000 | 0.0 | 0.0 MiB | 0.00 |
| 4M | 128 | 2896 | 0.0006 | 0.1 | 0.3 MiB | 0.00 |
| 16M | 32 | 11586 | 0.3274 | 10.6 | 169.0 MiB | 0.48 |

The required qualitative pattern (larger S → higher failed-range exposure → larger
retry penalty) **is present in BAD**: the failure exposure appears at S = 16 MiB
(32.7 % per-attempt failure rate, 10.6 retries and 169 MiB re-downloaded per
download, 48 % of downloads aborting after exhausting the 3-attempt retry limit).
In GOOD no S fails (0.4 % loss with 0.15 s bursts is recovered well inside the
timeout margin), so the S exposure is a *loss-state-dependent* effect, not a
generic S cost.

## 7. Joint-optimum result

| condition | optimum | completion | second best | margin |
|---|---|---|---|---|
| GOOD | (N=16, S=16M) | 52.6 s | (16/4194304) | +0.8% |
| BAD | (N=16, S=4M) | 60.0 s | (16/1048576) | +3.4% |

Materiality (completion penalty at N=16 when the other state's optimal S is used):

- BAD using the GOOD optimum S=16M: **+36.3 %** (81.8 s vs 60.0 s) → material.
- GOOD using the BAD optimum S=4M: **+0.8 %** (53.0 s vs 52.6 s) → immaterial.

So the **S component moves materially** (S* = 16M in GOOD → 4M in BAD), but the
**N component does not move at all** (N* = 16 in both). By the task's own rule this
is a **single-dimensional (S-only) opportunity, not a joint N+S opportunity**.

## 8. Live mechanism gate

**NOT REACHED.** The analytical gate requires condition 1 (N* differs between loss
states) to hold before any live execution; it fails, so no live mechanism gate was
run and no live loss-shaping experiment was performed.

## 9. Causal adaptive result

**NOT REACHED.** Conditions 4–6 (within-download transition, causal detection,
positive causal joint headroom) were not evaluated because conditions 1–3 failed.
No adaptive controller was implemented or run; the frozen starting configuration
(4, 4 MiB) and one-switch semantics were never exercised.

## 10. aria2 result

**NOT REACHED.** aria2 was reserved for the practical comparison after the causal
gates; its frozen configuration was not touched.

## 11. Stationary controls

The two stationary loss conditions (GOOD/BAD) are themselves the stationary
controls for this mechanism. Neither shows a state-dependent N optimum, so there
is no adaptive advantage to control for; the stationary control requirement is
moot because the joint gate never passed.

## 12. Causal/oracle headroom

**NOT MEASURED** (no adaptive runs). The structural reason is explicit: under
per-response pacing the per-request burst exposure and the aggregate progress are
N-independent, so the only state-dependent dimension is S — and an S-only
adaptation is not a joint N+S result.

## 13. Failure modes and robustness

- **Threshold-like S effect.** The failure exposure appears only at S = 16 MiB under
  BAD; S ≤ 4 MiB never fails. The S* movement is therefore driven by one grid point.
- **Variant dependence.** Across the 9 predeclared variants the S* moves between
  conditions in 4/9 cases (primary, T_G=10 s, g=0.25, g=0.5) and not in the others
  (T_B=1 s/p_B=1.0, T_B=5 s, T_G=60 s, R=1250 KiB/s, R=2500 KiB/s). At faster base
  rates the timeout margin is large enough that no S fails — the S-retry
  amplification is **rate-conditional**.
- **Structural N insufficiency.** N* = 16 in 9/9 variants and both conditions;
  monotone decreasing completion in 9/9. This matches the project's live T1 result
  and the live shared-bottleneck screen: no live-grounded concurrency cost exists.
- **Model abstractions.** The B-state stall abstraction and the constant rate are
  the main modelling choices; both were predeclared with sensitivity axes (g, R)
  and the conclusion (no N movement) holds across all of them.
- **No result shopping:** parameters, grid, seeds, conditions, viability rule and
  robustness variants were frozen in `parameter_grounding/` before the runs; failed
  (aborted) downloads are recorded, not discarded.

## 14. Exact claim supported

**Within the frozen analytical model and its predeclared parameter ranges:**
correlated burst loss combined with the downloader's range-retry semantics creates a
material, loss-state-dependent **chunk-size** effect (S = 16 MiB is fragile under
burst loss: 32.7 % per-attempt failure, 48 % download aborts, +36.3 % completion
penalty), but **no loss-state-dependent concurrency effect**: N* = 16 in every loss
state and every parameter variant. There is therefore **no joint N+S adaptation
opportunity under this mechanism**. Per the task's stop condition, the mechanism
search is closed; no live/adaptive execution and no confirmatory protocol follow.
The project's defensible competitive result remains the **fixed N=16, S=4 MiB D2
aria2 win (62.4 %)**, unchanged.

## 15. Figures

- `figures/fig1_completion_vs_N.png` — completion vs N by loss condition
- `figures/fig2_completion_vs_S.png` — completion vs S by loss condition (N=16)
- `figures/fig3_surface_GOOD.png` — T(N,S) surface, GOOD
- `figures/fig4_surface_BAD.png` — T(N,S) surface, BAD
- `figures/fig5_optimum_movement.png` — the key figure: optimum movement + materiality
- `figures/fig6_failure_vs_S.png` — failure/abort exposure vs S
- `figures/fig7_retry_cost_vs_S.png` — retries and retry bytes vs S
- `figures/fig8_robustness.png` — optima across the 9 predeclared variants

## 16. Artifacts

- `parameter_grounding/parameter_grounding.md`; `analytical_gate/stationary_screen.json`,
  `analytical_gate/robustness_sweep.json`; `analysis/analytical_gate_analysis.json`;
  `figures/fig1..fig8`; `tools/loss_sim.py`, `tools/run_screen.py`,
  `tools/run_robustness.py`, `tools/analyze_gate.py`, `tools/make_figures.py`,
  `tools/make_report.py`; `manifest/manifest.json`.

