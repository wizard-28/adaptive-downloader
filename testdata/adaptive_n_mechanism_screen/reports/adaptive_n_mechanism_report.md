# Adaptive-N Mechanism Screen — Analytical Report

**ANALYTICAL SIMULATION ONLY.** No empirical network experiment was run: no downloads, no
mm-link, no server/client processes, no tc/netem, no replay. All numbers are deterministic
simulator output and are not empirical evidence.

Generated: 2026-09-28T16:44:08Z

## 1. Current S-study closure

The static chunk-size (S) mechanism is closed for confirmation: Stage 0S passed its
feasibility gate, but the scaling analysis showed the frozen S switches have non-positive
relative gain at every file size, and the mechanism-pivot screen found no robust
state-dependent S optimum (all candidates MECHANISTICALLY INSUFFICIENT). No S confirmatory
run was performed. The S evidence is untouched by this study.

## 2. Previous concurrency evidence (unchanged)

- **T1 (live per-response pacing):** aggregate throughput scales monotonically with C in
  every phase; no live concurrency reversal at equal 1 MiB granularity (GATE B FAIL).
- **L3 (static per-phase sweep):** phase optima vary, but the action-constrained oracle
  reached only 8.1% simulated headroom (< 10% gate) -> STOP.
- **L4 (simulated over-concurrency penalty):** 17.6% simulated headroom, but the live
  representative sweep did NOT reproduce the penalty (GATE E FAIL;
  `NOT_READY_LIVE_MISMATCH`). This is decisive for the current screen.
- **M3 (aggregate throughput envelope replay):** every C is capped at the same instantaneous
  rate; no C sensitivity.

## 3. Candidate mechanisms

| id | mechanism | screened model | live grounding |
|---|---|---|---|
| A | per-flow / per-connection bandwidth sharing | fair | M3 aggregate throughput envelope: no C sensitivity |
| B | server-side concurrency cap or queueing | cap | T1: per-response pacing, no live reversal at equal granularity |
| C | queueing/overhead that grows with concurrent requests | degrade | L4 GATE E: the simulated over-concurrency penalty did NOT survive live (NOT_READY_LIVE_MISMATCH) |
| D | background traffic competing for a shared bottleneck | fair_bg | no live background-traffic study |
| E | contention where additional flows increase queueing delay | cap/degrade | T1/L4: no live concurrency penalty observed |
| F | server request-processing capacity that saturates as N increases | cap | T1: aggregate scales monotonically with C |
| G | changing available capacity + per-flow sharing | fair (changing C) | S pivot shared model: no state-dependent preference |
| H | changing server availability + concurrency-dependent queueing | cap/degrade | L4 GATE E: live mismatch |

## 4. Trace inventory

The 8 native Mahimahi delivery-opportunity traces represent time-varying **aggregate
capacity** (strong variability, p90/p10 5.5-27.4). They do **not** contain per-flow
fairness, queueing, or per-connection cost information, so they cannot establish a
concurrency-sensitive mechanism. The 23 throughput-trajectory traces (NYU-METS/UGent) are
measured end-to-end throughput, not capacity, and the M3 envelope limitation applies.
**No existing trace establishes the necessary concurrency mechanism**; the mechanism must
come from the server/system model.

## 5. Analytical models

Deterministic event-driven finite-file simulator (`tools/n_sim.py`): N workers, one
contiguous 1 MiB request per worker, F = 512 MiB, per-request RTT 40 ms; request transfer
rates depend on the current number of active requests and the current state (in-flight
requests experience state evolution). Models: `direct` (fixed per-request rate; T1
`fair` (aggregate C shared among k flows), `fair_bg` (fair sharing with B background
`cap` (server parallelism K with FIFO wait), `degrade` (server aggregate capacity
C/(1+gamma*(k-1))). N changes add workers or retire them (in-flight requests finish).

## 6. Stationary N optima

| model | preferred N across states | state-dependent? |
|---|---|---|
| direct (R = 0.25-2 MB/s) | 16 at every R | no (monotone) |
| fair (C = 0.5-32 MB/s) | 16 at every C | no (monotone) |
| fair_bg (B = 0, 4, 16) | 16 at every B | no (monotone) |
| cap (K = 1, 2, 4, 8) | {'K=1': 2, 'K=2': 4, 'K=4': 8, 'K=8': 16} | yes (tracks the plateau edge; completion flat above it) |
| degrade (C x gamma grid) | {'C=0.50MB/s,g=0.01': 1, 'C=0.50MB/s,g=0.05': 1, 'C=0.50MB/s,g=0.10': 1, 'C=2.00MB/s,g=0.01': 2, 'C=2.00MB/s,g=0.05': 1, 'C=2.00MB/s,g=0.10': 1, 'C=8.00MB/s,g=0.01': 8, 'C=8.00MB/s,g=0.05': 2, 'C=8.00MB/s,g=0.10': 2, 'C=32.00MB/s,g=0.01': 8, 'C=32.00MB/s,g=0.05': 4, 'C=32.00MB/s,g=0.10': 4} | yes (interior optimum rises with C, shifts with gamma) |

Only `cap` and `degrade` show a state-dependent preferred N. `cap` is a plateau (the
model-implied best fixed N is already robust across states), while `degrade` has a true
interior optimum whose location depends on the state.

## 7. Within-download state transitions

| case | state-1 opt N | state-2 opt N | best fixed N | causal gain | oracle gain |
|---|---|---|---|---|---|
| degrade C high->low | 4 | 1 | 2 | +6.659% | +5.825% |
| degrade C low->high | 1 | 4 | 4 | +0.225% | +1.224% |
| degrade gamma low->high | 8 | 2 | 2 | +0.510% | +17.445% |
| cap K 8->2 | 16 | 4 | 16 | +0.000% | +0.000% |
| cap K 2->8 | 4 | 16 | 16 | -128.531% | +0.000% |

## 8. Causal adaptive-N results

The causal policy uses only observed completed-request throughput (median of the last 3
samples) and one switch per download. Results: causal gains range -128.531% .. +6.659% across all
transition cases; median +1.501%. The `cap` model gives no causal gain (plateau; the state is
not observable in the per-request rate), and one `cap` case is strongly negative because
the causal policy mis-times an unobservable change.

## 9. Oracle comparison

The oracle is a coarse-grid diagnostic (one future-only switch with perfect knowledge) and
is not a policy. Oracle gains range +0.000% .. +18.691%. The causal/oracle capture ratio is
inconsistent: near 100% in severe `degrade` transitions (ratios >= 8) but far lower in
mild transitions and in the gamma transition (0.5% vs 17.4%).

## 10. Parameter robustness

Predeclared sweep: C in {0.5, 2, 8, 32} MB/s x gamma in {0.005, 0.01, 0.02, 0.05, 0.1,
0.2}. The `degrade` preferred N is state-dependent for **every** gamma (argmins over C:

- gamma=0.005: [2, 4, 8, 16]
- gamma=0.010: [1, 2, 8, 8]
- gamma=0.020: [1, 2, 4, 8]
- gamma=0.050: [1, 1, 2, 4]
- gamma=0.100: [1, 1, 2, 4]
- gamma=0.200: [1, 1, 1, 2]

So the analytical state-dependence is broad, not narrow. However the mechanism itself
requires a server-side per-connection overhead (gamma) that is **not present in the
current controlled infrastructure** and was already rejected live (L4 GATE E).

## 11. Stationary-vs-changing control

In every stationary control the causal gain is -1.089% .. +0.000% (i.e., no intrinsic state-change
advantage); the changing-state gains are zero or positive. The effect therefore weakens
materially when the state is stationary.

## 12. 5% headroom analysis

- maximum causal headroom: **+6.659%** (degrade, severe capacity drop)
- median causal headroom: +1.501%
- maximum oracle headroom: +18.691%
- fraction of transition cases with causal gain >= 5%: 2/10
- stationary-control headroom: ~0

The 5%% level is reachable **only analytically** in the `degrade` model at severe
capacity drops; it is not demonstrated live and must not be treated as an empirical result.

## 13. Mechanism classifications

| candidate | state-dependent preferred N | causal adaptation | residue-free | stationary control | parameter sweep | live grounding | classification |
|---|---|---|---|---|---|---|---|
| A per-flow sharing (fair) | no | n/a | yes | n/a | n/a | mm-link yes | **MECHANISTICALLY INSUFFICIENT** |
| B server cap (cap) | yes (plateau edge) | no (0%; plateau) | yes | n/a | n/a | not in the live stack | **MECHANISTICALLY INSUFFICIENT** |
| C concurrency-dependent server cost (degrade) | yes | yes (up to +6.7%) | yes | yes | yes (all gamma) | **NO (L4 GATE E live mismatch)** | **PLAUSIBLE - FRAGILE** |
| D background traffic (fair_bg) | no | n/a | yes | n/a | n/a | no live study | **MECHANISTICALLY INSUFFICIENT** |
| E queueing/contention (cap/degrade) | yes | mixed | yes | yes | yes | NO (L4/T1) | **PLAUSIBLE - FRAGILE** |
| F server saturation (cap) | yes (plateau edge) | no | yes | n/a | n/a | not live | **MECHANISTICALLY INSUFFICIENT** |
| G changing capacity + sharing (fair) | no | n/a | yes | n/a | n/a | mm-link yes | **MECHANISTICALLY INSUFFICIENT** |
| H changing server availability + queueing (cap/degrade) | yes | mixed | yes | yes | yes | NO (L4) | **PLAUSIBLE - FRAGILE** |

The `degrade`-class mechanisms pass the six analytical ROBUST conditions but fail the
physical-grounding requirement: the only live validation of this mechanism class (L4 GATE
E) rejected it, and the current controlled server has no concurrency-dependent capacity
cost. They are therefore classified **FRAGILE**, not ROBUST.

## 14. Does a robust adaptive-N opportunity exist?

**No.** Under the mechanisms available in the current project infrastructure, the
preferred N is monotone or plateau-flat (N=16), and the only analytically state-dependent
mechanism (concurrency-dependent server capacity) is not live-grounded and was already
rejected by live validation. No robust adaptive-N opportunity is established, so no
empirical protocol is justified at this time.

## 15. Exact requirements for any future experimental protocol

1. **Establish the mechanism live first.** A controlled experiment must show a reproducible
   concurrency-dependent server/system cost (the current stack does not; L4 GATE E).
2. **Ground the parameters.** The mechanism's parameters must be measured, not tuned; no
   parameter may be chosen to produce a desired gain.
3. **Freeze a causal policy before execution.** One switch per download, adjacent N steps,
   triggered by an observed-throughput threshold and a pre-registered N mapping.
4. **Pre-register the control and target.** The model-implied best fixed N as the baseline
   and the 5% practical reference (not tuned to the result).
5. **Fresh evidence at a new path.** A new preregistered protocol; the frozen S/oracle/
   Stage A/B/C artifacts and this screen remain untouched.

## Statement

Zero empirical network experiments were performed for this analysis.
