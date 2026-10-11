# Track 2 — N-Dependent Server Mechanism: Analytical Pre-Screen Report

**Final status: `PRE_SCREEN_FAILED_NO_QUALIFYING_SCENARIO`** (maps to
`LIVE_MECHANISM_GATE_FAILED_JOINT`; the live gate was **not reached**). Per the
frozen process, no live server extension, no live fixed-grid gate, no adaptive
experiment and no aria2 comparison were performed. The mechanism does not produce a
genuine state-dependent joint optimum under its own faithful semantics; per the
task's stop rule it was not tuned until it did.

Generated: 2026-09-29T12:05:22Z

## 1. The mechanism and its relationship to `sim_env_v5.py`

The pre-screen reproduces the exact semantics of the project's existing simulator
(`python/rl/sim_env_v5.py`), which is the mechanism the submitted proposal promised
to implement as controlled N-dependent server throttling:

| simulator semantics | live correspondence (pre-screen model) |
|---|---|
| `p_eff(k) = min(0.9, loss + congestion*(k-1))` per request | per-attempt failure probability at the active-request count `k` |
| `svc = latency + overhead*(k-1) + n/per` | service time; `overhead*(k-1)` is the per-extra-connection delay |
| `per = min(rate, link/k)` | fair-share per-response rate sampled at request start |
| `failed -> svc += latency + n/per` (one full retransmission) | the failed attempt consumes the full service time, then fails; the downloader retries the **same range** with its existing semantics (max 3 attempts, then the download aborts) |
| (simulator has no timeout/abort) | 30 s per-attempt client timeout, 0.1 s retry delay, client cycle overhead 0.088 s |

The explicitly rejected substitute `R_eff = R*(1-loss_eff)` was **not** used: the
mechanism is probabilistic request failure, not rate reduction.

## 2. Predeclared family and selection rule (frozen before evaluation)

- **GOOD:** rate 2.5 MiB/s, link 0, loss 0.4 %, congestion 0, overhead 20 ms, latency 20 ms.
- **DEGRADED candidates (16):** rate {0.4, 0.5} MiB/s x loss {2 %, 3 %} x congestion
  {0.02, 0.04, 0.06} with link 0 (12), plus 4 link variants (rate 1.0 MiB/s, link
  {2.0, 3.0} MiB/s, congestion {0.04, 0.06}, loss 3 %).
- **Grid:** N in {1,2,4,8,16}; S = the exact frozen Track-1 action set (256 KiB plus
  1..16 MiB); 20 seeds per cell; timeout/retry semantics as above.
- **Selection rule:** a pair qualifies iff (1) N* and (2) S* differ between states,
  (3) material cross-state penalty >= 10 %, (4) coordinate-wise margins >= 5 % in
  both states, (5) the optima are reachable (abort rate <= 20 %, second-best
  completes in >= 50 % of seeds). Tie-break: smallest file size, then max
  min(penalty), then larger congestion, then larger rate.
- **Stop rule:** no qualifying candidate -> STOP TRACK 2, no tuning.

**Pre-screen revisions (evaluation-design only; the candidate family was never
changed):** revision 0 used F = 32 MiB and a second-best-cell separation metric and
was degenerate (F/S < N at large S; exact ties). Revision 1 used F = 64 MiB with
coordinate-wise margins. Revision 2 evaluated the same family at the predeclared
workload scales F in {64, 128, 256} MiB (the N-degeneracy is a function of F/S vs N).
All revisions are recorded in `manifest/track2_manifest.json`.

## 3. Pre-screen results (all 16 candidates x 3 file sizes)

| file size | GOOD optimum | best degraded candidate | N* | S* | N margin | S margin | penalties | qualifies |
|---|---|---|---|---|---|---|---|---|
| 64 MiB | (N=16,S=4M) 1.9s | D_link3.0_c0.06 | 8 | 15 MiB | 0% | 6% | +217%/+76% | no |
| 128 MiB | (N=16,S=8M) 3.5s | D_link2.0_c0.06 | 2 | 16 MiB | 15% | 2% | +639%/+inf% | no |
| 256 MiB | (N=16,S=16M) 6.7s | D_r0.5_l0.02_c0.04 | 1 | 14 MiB | 0% | 0% | +1453%/+inf% | no |

**No candidate qualifies at any predeclared file size.** The strongest candidate is
`D_r0.4_l0.02_c0.04` at F = 64 MiB.

## 4. The strongest candidate and why it fails the joint gate

| | GOOD | DEGRADED |
|---|---|---|
| optimum | (N=16, S=4 MiB), 1.92 s | (N=8, S=11 MiB), 27.60 s |
| N margin (best vs other N at the same S) | 46.8 %% | **0.0 %%** |
| S margin (best vs other S at the same N) | 14.3 %% | **1.0 %%** |
| cross-state penalty | using DEGRADED's optimum costs +134.4 %% | using GOOD's optimum costs +10.7 %% |
| abort rate at the optimum | 0 %% | 0 %% (the optimum sits just below the cliff) |

Degraded completions at the optimum S = 11 MiB: N=1 160.6 s, N=2 82.7 s,
N=4 55.1 s, **N=8 27.6 s, N=16 27.6 s**. The N* "movement" (16 -> 8) is
**degenerate**: N=8 and N=16 are exactly equivalent because only six requests
exist at that S; the argmin picks N=8 by iteration order, not by performance.
The S* movement is a **cliff boundary**: the optimum is the largest S below the
30 s timeout cliff (S > per x 30 s = 12 MiB aborts), and its margin over the next
smaller S is 1.0 %.

Additionally, the best **single** fixed configuration across the two states
(N=16, S=4 MiB) is within **10.7 %** of the degraded optimum, so even an S-only
controller gains at most ~10 % over the best fixed config in the degraded state —
and no N change is required to obtain it.

## 5. Structural reasons (why the mechanism does not produce a joint opportunity)

1. **Abort cliff.** The failure-based concurrency penalty (`p_eff(16)=0.62` at
   congestion 0.04) plus the downloader's 3-attempt retry limit makes high-
   concurrency cells **abort** (P(3 failures) = 23.8 % per range) rather than
   become smoothly slower. The simulator's smooth interior N optimum (its
   one-retransmission cost model, L4 bestC=2-6) does not survive the live
   retry/abort semantics.
2. **Per-request, size-independent failure cost.** The retry cost per byte is
   constant, so the degraded state favours fewer, larger requests; S* is pushed
   to the timeout boundary rather than balancing against a size-dependent cost.
3. **N degeneracy at that boundary.** At the timeout-boundary S* the request
   count is small (F/S = 6), so N beyond it is irrelevant: the N optimum is a
   plateau, not a separated interior point.
4. **Marginal adaptive value.** The best single fixed configuration is within
   10.7 % of the degraded optimum, and an S-only switch suffices; the joint
   movement is neither necessary nor cleanly separated.

## 6. Fixed-grid live gate, adaptive experiment, aria2

**NOT REACHED / NOT RUN.** The frozen rule stops Track 2 when no candidate
qualifies; the live server extension was therefore not implemented (implementing
it would only reproduce an already-characterised negative), and no live fixed
grid, adaptive controller, or aria2 comparison was executed. No Track-2
environment changes were made; no frozen artifact was touched.

## 7. Limitations

- Analytical only: the pre-screen models the proposed live semantics exactly, but
  no live confirmation was performed (per the frozen stop rule).
- The candidate family is finite (16 degraded candidates x 3 workload scales); a
  different family (e.g., a size-dependent failure probability or a server that
  degrades without hard request failures) could behave differently, but inventing
  one is out of scope for this task.
- The 3-attempt retry limit is the frozen downloader semantics; relaxing it is not
  permitted and would change the mechanism's character.

## 8. What this means

The mechanism the proposal promised (configurable N-dependent server throttling)
was reproduced exactly in analysis and does **not** create a genuine joint N+S
opportunity: its concurrency penalty is a retry-limit cliff, its S preference is
pushed to the timeout boundary, and the N optimum there is degenerate. The
defensible competitive result remains the fixed **N=16, S=4 MiB D2 aria2 win
(62.4 %)** and the Track-1 finding that the existing adaptive heuristics do not
add value on D2 (`BEATS_ARIA2_ONLY`).

## 9. Figures

- `figures/fig1_surface_good.png` — GOOD state completion surface T(N,S)
- `figures/fig2_surface_degraded.png` — DEGRADED state completion surface (timeout cliff visible)
- `figures/fig3_completion_vs_N.png` — state-dependent N optima (degraded N=8/N=16 tie)
- `figures/fig4_completion_vs_S.png` — completion vs S with the timeout cliff
- `figures/fig5_abort_surface.png` — degraded abort-rate surface (retry-limit cliff)
- `figures/fig6_candidates.png` — N*/S* across all predeclared candidates and file sizes

## 10. Artifacts

- `manifest/track2_manifest.json` (frozen family/rule, revisions, stop decision);
  `analysis/prescreen.json` (+ revision snapshots), `analysis/track2_analysis.json`;
  `figures/fig1..fig6`; `tools/ns_sim.py`, `tools/prescreen.py`,
  `tools/analyze_track2.py`, `tools/make_figures.py`, `tools/make_report.py`.

