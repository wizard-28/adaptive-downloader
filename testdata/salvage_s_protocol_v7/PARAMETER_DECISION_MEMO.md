# Salvage Protocol v7 — Parameter Decision Memo

**Final human decision recorded:** 2026-09-28T09:31:30Z  
**Status:** `FINAL_S_PROTOCOL_FROZEN — READY_FOR_STAGE_0S`  
**Scope:** documentation only. No experiment, pilot, or network run has been performed.

## 0. FINAL HUMAN DECISION (frozen)

| Item | Frozen value |
|---|---|
| Mechanism package | **Candidate A** |
| R | 4 Mbps = 500,000 bytes/s |
| D | 40 ms |
| FileSize | 512 MiB = 536,870,912 bytes |
| N | 4 |
| T_ref = FileSize/(N*R) | **268.435456 s** |
| delta_T = 0.05*T_ref (time-scale) | **13.4217728 s** (a 5% reduction in completion time) |
| delta_log = log(0.95) (log-scale decision threshold) | **-0.051293294387551** (rounded -0.051293294); -0.05 is NOT used |
| T_tail_primary = D + C_max/R | **33.594432 s** |
| delta_tail = 0.05*T_tail_primary | **1.6797216 s** |
| Tail minimum | T_tail_primary >= 20 s (satisfied: 33.594432 s) |
| Tail-window data | N * C_max = 64 MiB (12.5% of the file) |
| Decision rule | P(Delta_k <= delta_log \| data) >= 0.95 |

**Arithmetic note:** the decision text stated `T_tail_primary = 33.594304 s`; independent
recalculation of the frozen formula `D + C_max/R = 0.040 + 16 MiB / 500,000 B/s` gives
**33.594432 s** (a 0.128 ms transcription difference). Per the section-15 independent-recalculation
requirement the formula-derived value is operative; the stated decimal is retained for provenance.
Both satisfy `T_tail_primary >= 20 s`.

## 1. Candidate mechanism packages (provenance; A selected, B/C/D unselected)

| Package | R | D | FileSize | N | T_ref | delta_T | T_tail_primary | tail >= 20 s | Status |
|---|---|---|---|---|---|---|---|---|---|
| A | 4 Mbps (500000 B/s) | 40 ms | 512 MiB | 4 | 268.4355 s | 13.4218 s | 33.5944 s | YES | **HUMAN-APPROVED / FROZEN (selected)** |
| B | 8 Mbps (1000000 B/s) | 40 ms | 512 MiB | 4 | 134.2177 s | 6.7109 s | 16.8172 s | NO | **UNSELECTED (analytically inadmissible: tail < 20 s)** |
| C | 4 Mbps (500000 B/s) | 80 ms | 512 MiB | 4 | 268.4355 s | 13.4218 s | 33.6344 s | YES | **UNSELECTED** |
| D | 4.19 Mbps (524288 B/s) | 20 ms | 512 MiB | 4 | 256.0000 s | 12.8000 s | 32.0200 s | YES | **UNSELECTED** |

## 2. Per-arm implications for the frozen package A (R = 500,000 B/s, D = 40 ms)

| Arm | bytes | service time D+s/R | efficiency (s/(D+s/R))/R | share of T_tail_primary |
|---|---|---|---|---|
| 256 KiB | 262144 | 0.5643 s | 92.91% | 1.7% |
| 1 MiB | 1048576 | 2.1372 s | 98.13% | 6.4% |
| 4 MiB | 4194304 | 8.4286 s | 99.53% | 25.1% |
| 16 MiB | 16777216 | 33.5944 s | 99.88% | 100.0% |

The bulk-transfer penalty of small chunks is the per-request RTT overhead; the tail penalty of large
chunks is C_max/R = 33.5544 s and dominates near EOF. This is the analytically predicted tradeoff the
study probes; **no positive adaptive-S effect is claimed or predicted**.

## 3. Frozen decisions (all HUMAN-APPROVED)

| Parameter | Frozen value | Basis | HUMAN APPROVAL STATUS |
|---|---|---|---|
| R, D, FileSize, N | 4 Mbps, 40 ms, 512 MiB, 4 (package A) | human decision | **HUMAN-APPROVED / FROZEN** |
| delta_T interpretation | delta_T = 0.05*T_ref (time-scale); decision uses delta_log = log(0.95) | preserves '5% reduction in completion time' | **HUMAN-APPROVED / FROZEN** |
| T_tail_primary | D + C_max/R = 33.594432 s | frozen formula | **HUMAN-APPROVED / FROZEN** |
| delta_tail | 0.05*T_tail_primary = 1.6797216 s | frozen relation | **HUMAN-APPROVED / FROZEN** |
| B_pending trigger | B_switch = N*S0: 64 MiB / 16 MiB / 4 MiB | frozen formula and start arms | **HUMAN-APPROVED / FROZEN** |
| Stage 0S go/no-go | see §4 | mechanism/feasibility only | **HUMAN-APPROVED / FROZEN** |
| SSR grid | 3 x 2 x 3 = 18 scenarios x 1000 datasets = 18,000 | pre-frozen model/priors | **HUMAN-APPROVED / FROZEN** |
| Exploratory switch | removed (duplicate of the third confirmatory member) | task-directed | **HUMAN-APPROVED / FROZEN** |
| Fixed-sequence order | 16->4, 4->1, 1->256K; stop at first failure | task-directed | **HUMAN-APPROVED / FROZEN** |

## 4. Frozen Stage 0S go/no-go rule

GO iff ALL hold; otherwise NO-GO:
1. Mechanism presence: pooled median per-request service time within +/-25% of D + s/R for every arm.
2. Direction: median service time strictly increasing 256 KiB < 1 MiB < 4 MiB < 16 MiB.
3. Tail sufficiency: pooled median realized 16 MiB tail >= 20 s AND within [0.8*T_tail_primary, 1.25*T_tail_primary].
4. Feasibility/reliability: >= 28/32 clean runs; <= 30 minutes wall-clock per run; within-block residual SD of log(T/T_ref) <= 0.25.

Feasibility/mechanism gate only; not a treatment-effect test; not modifiable after pilot results.

## 5. Frozen SSR scenario grid

Effects {0.00, log(0.95) = -0.051293294, -0.10} x sigma_block {0.05, 0.15} x sigma_resid {0.05, 0.10, 0.20}
= 18 scenarios x 1000 synthetic datasets = 18,000. Decision threshold P(Delta_k <= delta_log | data) >= 0.95.
Outputs: posterior coverage, type-I error behavior, power, posterior-probability distribution, expected
sample size, simulation runtime. The simulation cannot tune the real experiment after pilot outcomes.

## 6. Remaining human decisions

None. All six decisions from the resolution pass were approved in the final human decision.

## 7. No-experiment confirmation

No mm-link process was started, no server/client was started, no network replay was executed, no Stage 0S
pilot was executed, no new experiment directory was created, no source code was changed, and no frozen
oracle/Stage A/B/C evidence was modified.
