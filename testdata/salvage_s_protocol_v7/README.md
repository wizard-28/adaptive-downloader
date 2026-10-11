# Salvage Protocol v7 — Final Specification Freeze (documentation only)

**Status: `FINAL_S_PROTOCOL_FROZEN — READY_FOR_STAGE_0S`** (`ready_for_stage_0s = true`)

**No experiment has been run. Stage 0S execution is a separate task — do not start it from here.**

This directory is the prospective, frozen specification for the salvage study:

> When does adaptive HTTP range downloading benefit from changing future chunk size
> under finite-file, tail-aware conditions?

It is **not** an RL study, and no benefit claim may be made before the experiment is
run. The completed cellular concurrency/oracle study (Stages A/B/C,
`oracle_preregistration_v4`, `oracle_stage_v3`, `final_oracle_analysis`) is unchanged,
immutable, and separate from this study.

**Path note:** the task named `/workspace/mvp`; the repository is `/workspaces/mvp`.
These artifacts live under `/workspaces/mvp/testdata/salvage_s_protocol_v7/`.

## Frozen mechanism package (Candidate A — human-approved)

| Parameter | Frozen value |
|---|---|
| `R` | 4 Mbps = 500,000 bytes/s |
| `D` | 40 ms |
| `FileSize` | 512 MiB = 536,870,912 bytes |
| `N` | 4 |

Candidates B, C, D are **UNSELECTED** and retained in `PARAMETER_DECISION_MEMO.md` for
provenance only (B is analytically inadmissible: `T_tail_primary < 20 s`).

## Frozen quantities

| Quantity | Value |
|---|---|
| `T_ref = FileSize/(N·R)` | **268.435456 s** |
| `delta_T = 0.05·T_ref` (time-scale) | **13.4217728 s** (a 5% reduction in completion time) |
| `delta_log = log(0.95)` (log-scale decision threshold) | **−0.05129329438755058** (rounded −0.051293294); `−0.05` is **not** used |
| `T_tail_primary = D + C_max/R` (`C_max` = 16 MiB) | **33.594432 s** |
| `delta_tail = 0.05·T_tail_primary` | **1.6797216 s** |
| Tail minimum | `T_tail_primary >= 20 s` — satisfied |
| Tail-window data | `N·C_max` = 64 MiB = 12.5% of the file |

*Arithmetic note:* the decision text stated `T_tail_primary = 33.594304 s`; independent
recalculation of the frozen formula gives **33.594432 s** (0.128 ms transcription
difference). The formula-derived value is operative; the stated decimal is retained for
provenance. Both satisfy the ≥ 20 s requirement.

## Frozen design (summary)

- **Arms:** 256 KiB, 1 MiB, 4 MiB, 16 MiB (no additional arms).
- **Confirmatory strategies:** `16 MiB → 4 MiB`, `4 MiB → 1 MiB`, `1 MiB → 256 KiB`;
  **no exploratory strategy** (the duplicated case was removed).
- **Trigger:** `B_pending = TotalSize − CompletedBytes − InFlightBytes` from tracker
  state, evaluated immediately before allocation; `B_switch = N·S0` = **64 MiB / 16 MiB /
  4 MiB**; one switch maximum; in-flight ranges never resized; `S` fixed after switching.
- **Primary model:** `y_cb = log(T_cb/T_ref) = mu_c + u_b + eps_cb`; priors
  `Normal(0,1.5)`, `HalfNormal(0,0.5)`, `HalfNormal(0,0.5)`; sensitivity `Normal(0,3)`,
  `HalfNormal(0,1)`, `HalfNormal(0,1)`; `mu_fixed* = min(mu_256K, mu_1M, mu_4M, mu_16M)`;
  decision `P(Delta_k <= delta_log | data) >= 0.95`; no ranking/winner/best-policy.
- **Fixed sequence:** `16→4`, `4→1`, `1→256K`; stop at the first failure.
- **Pilot (Stage 0S):** 8 blocks × 4 arms = 32 runs, feasibility/mechanism only.
- **Stage 0S GO/NO-GO:** mechanism presence ±25% of `D+s/R`; strictly increasing service
  times; realized tail ≥ 20 s within [0.8, 1.25]·`T_tail_primary`; ≥ 28/32 clean runs,
  ≤ 30 min/run, within-block residual SD ≤ 0.25. Not a treatment test.
- **Confirmatory sample size:** n ≤ 12 per condition/strategy; no post-hoc increase.
- **SSR grid:** 3 effects × 2 `sigma_block` × 3 `sigma_resid` = 18 scenarios × 1000
  datasets = **18,000** synthetic datasets; decision threshold `P(Delta_k <= delta_log) >= 0.95`.
- **Two-stage separation:** pilot data are **not** pooled into the confirmatory fit (no
  pooling rule exists in v7).
- **Trace-backed validation:** out of scope for v7.

## Artifacts

| File | Contents |
|---|---|
| `final_s_protocol.json` | Machine-readable frozen specification (source of truth) |
| `final_s_protocol.txt` | Human-readable rendering of the same |
| `PARAMETER_DECISION_MEMO.md` | Candidate comparison (A frozen; B/C/D unselected) + frozen decisions |
| `README.md` | This overview |

## Safety

No mm-link process was started, no server/client was started, no network replay was
executed, no Stage 0S pilot was executed, no new experiment directory was created, no
source code was changed, and no frozen oracle/Stage A/B/C evidence was modified. Do not
delete or overwrite evidence.
