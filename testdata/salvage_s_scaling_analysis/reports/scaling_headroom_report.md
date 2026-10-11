# Pre-Stage-1 File-Size Scaling / Headroom Analysis — Salvage Protocol v7

**ANALYTICAL SIMULATION ONLY.** No empirical network experiment was run: no mm-link,
no server/client downloads, no tc/netem, no packet replay, no Stage 1 runs. All results
below are deterministic model output under the frozen mechanism.

Generated: 2026-09-28T14:58:40Z

## 1. Research question

Under the frozen static rate + RTT mechanism, can increasing the file size plausibly
increase the **relative** completion-time headroom available to chunk-size adaptation?
The three frozen switching strategies and an exploratory single-switch upper envelope are
examined across a file-size grid.

## 2. Frozen mechanism

- R = 500000 bytes/s (4 Mbps), D = 40 ms, N = 4
- arms: 256KiB (262144 B), 1MiB (1048576 B), 4MiB (4194304 B), 16MiB (16777216 B)
- strategies and thresholds: 16MiB->4MiB at B_pending <= 67108864; 4MiB->1MiB at B_pending <= 16777216; 1MiB->256KiB at B_pending <= 4194304
- one switch maximum; evaluated immediately before allocation; in-flight ranges never resized.

## 3. Analytical model

MODEL A: `t(s) = D + s/R` (frozen request-level model).

The simulator is a deterministic work-conserving event simulation of the frozen semantics
(N=4 workers, contiguous ranges allocated in order, future-only chunk size, B_pending sampled
immediately before each allocation, one switch, final partial range, EOF).

## 4. Stage 0S calibration

MODEL B: `t(s) = a + b*s` with a = 0.041837515 s and b = 2.049600672081e-06 s/byte
(OLS fit to the four frozen Stage 0S pooled median service times; R^2 = 0.999999988509).
The models are never blended.

Stage 0S validation at F = 512 MiB (predicted vs observed median):

| Arm | Model A predicted | Model B predicted | Stage 0S observed median | B rel. error |
|---|---|---|---|---|
| 256KiB | 288.915 s | 296.514 s | 296.385 s | +0.0432% |
| 1MiB | 273.555 s | 280.448 s | 280.794 s | -0.1232% |
| 4MiB | 269.715 s | 276.432 s | 276.869 s | -0.1581% |
| 16MiB | 268.755 s | 275.427 s | 276.397 s | -0.3507% |

Model B reproduces the Stage 0S fixed-S observations to within 0.4%; Model A underestimates
them by ~2.5% because the observed service times include a small scheduling overhead.

## 5. File-size grid

Main grid: 64 MiB, 128 MiB, 256 MiB, 512 MiB, 1 GiB, 2 GiB, 4 GiB, 8 GiB, 16 GiB, 32 GiB
plus a dense residue scan (512–640 MiB in 4 MiB steps and larger-F residues).

## 6. Fixed-S scaling results

For exact full bursts the normalized fixed-S completion is scale-invariant:
`T_S/T_ref = 1 + R*D/s` (Model A), i.e. +0.119% (16 MiB), +0.477% (4 MiB),
+1.907% (1 MiB), +7.629% (256 KiB) — independent of F. The model-implied lowest
completion-time fixed condition is 16 MiB on the entire main grid.

| File size | model-implied best fixed | T_ref (s) | best fixed (s) | 256KiB (s) | 1MiB (s) | 4MiB (s) | 16MiB (s) |
|---|---|---|---|---|---|---|---|
| 64MiB | 16MiB | 33.554 | 33.594 | 36.114 | 34.194 | 33.714 | 33.594 |
| 128MiB | 16MiB | 67.109 | 67.189 | 72.229 | 68.389 | 67.429 | 67.189 |
| 256MiB | 16MiB | 134.218 | 134.378 | 144.458 | 136.778 | 134.858 | 134.378 |
| 512MiB | 16MiB | 268.435 | 268.755 | 288.915 | 273.555 | 269.715 | 268.755 |
| 1GiB | 16MiB | 536.871 | 537.511 | 577.831 | 547.111 | 539.431 | 537.511 |
| 2GiB | 16MiB | 1073.742 | 1075.022 | 1155.662 | 1094.222 | 1078.862 | 1075.022 |
| 4GiB | 16MiB | 2147.484 | 2150.044 | 2311.324 | 2188.444 | 2157.724 | 2150.044 |
| 8GiB | 16MiB | 4294.967 | 4300.087 | 4622.647 | 4376.887 | 4315.447 | 4300.087 |
| 16GiB | 16MiB | 8589.935 | 8600.175 | 9245.295 | 8753.775 | 8630.895 | 8600.175 |
| 32GiB | 16MiB | 17179.869 | 17200.349 | 18490.589 | 17507.549 | 17261.789 | 17200.349 |

## 7. Frozen-switch scaling results

Relative gain vs the model-implied best fixed condition (`relative_gain = (T_ref_fixed - T_switch)/T_ref_fixed`):

| File size | 16MiB->4MiB | 4MiB->1MiB | 1MiB->256KiB |
|---|---|---|---|
| 64MiB | -0.3572% | -0.7144% | -2.1432% |
| 128MiB | -0.1786% | -0.5358% | -1.9646% |
| 256MiB | -0.0893% | -0.4465% | -1.8753% |
| 512MiB | -0.0447% | -0.4019% | -1.8307% |
| 1GiB | -0.0223% | -0.3795% | -1.8083% |
| 2GiB | -0.0112% | -0.3684% | -1.7972% |
| 4GiB | -0.0056% | -0.3628% | -1.7916% |
| 8GiB | -0.0028% | -0.3600% | -1.7888% |
| 16GiB | -0.0014% | -0.3586% | -1.7874% |
| 32GiB | -0.0007% | -0.3579% | -1.7867% |

Maximum frozen-strategy relative gains across the grid:

| Strategy | Model A max | Model A at | Model B max |
|---|---|---|---|
| 16MiB->4MiB | -0.0007% | 32GiB | -0.0007% |
| 4MiB->1MiB | -0.3579% | 32GiB | -0.3653% |
| 1MiB->256KiB | -1.7867% | 32GiB | -1.8235% |

All frozen-strategy gains are **negative** on the entire grid. The switch fires at exactly
a full final burst (B_switch = N*S0), where replacing S0 chunks with S1 chunks adds
per-request D overhead without shortening the tail. At F = 512 MiB the gains are:
- 16MiB->4MiB: -0.0447% (absolute -0.120 s; switch fired=True)
- 4MiB->1MiB: -0.4019% (absolute -1.080 s; switch fired=True)
- 1MiB->256KiB: -1.8307% (absolute -4.920 s; switch fired=True)

## 8. Analytical upper-envelope results (EXPLORATORY ONLY)

For each starting arm the switch point was scanned over feasible B_pending thresholds;
this is **not** the frozen Stage 1 policy and must not be treated as evidence.

| Strategy | Model A best envelope | at | Model B best envelope |
|---|---|---|---|
| 16MiB->4MiB | -0.0007% | 32GiB | -0.0007% |
| 4MiB->1MiB | -0.3579% | 32GiB | -0.3653% |
| 1MiB->256KiB | -1.7867% | 32GiB | -1.8235% |

On the main grid even the optimally timed single switch is **<= 0** (the aligned power-of-two
file sizes have no tail-packing defect). The dense residue scan (16MiB->4MiB, Model A) finds a
maximum of **0.3545% at 8224MiB** (threshold 33554432 B) — i.e. at most ~0.36%, the bulk per-request overhead
difference between 16 MiB and 4 MiB — and it occurs only at residues where a smaller fixed arm
is the reference; the larger-F residues stay <= 0.3545% (at 8224MiB).

## 9. Absolute vs relative gain

Absolute time saved (frozen strategies) is <= 0 on the grid; for the exploratory envelope the
absolute saving at favourable residues grows with F (a fixed ~0.36% of a growing T_ref), while
the relative gain stays bounded at ~0.36%. The two quantities must not be conflated: a larger
file can show a larger absolute difference while the percentage improvement does not increase.

## 10. 5% threshold analysis

| File size | Frozen 16->4 | Frozen 4->1 | Frozen 1->256K | Analytical single-switch upper envelope (16->4, dense scan) |
|---|---|---|---|---|
| 64MiB | -0.3572% | -0.7144% | -2.1432% | <= 0 |
| 128MiB | -0.1786% | -0.5358% | -1.9646% | <= 0 |
| 256MiB | -0.0893% | -0.4465% | -1.8753% | <= 0 |
| 512MiB | -0.0447% | -0.4019% | -1.8307% | -0.0447% |
| 1GiB | -0.0223% | -0.3795% | -1.8083% | <= 0 |
| 2GiB | -0.0112% | -0.3684% | -1.7972% | <= 0 |
| 4GiB | -0.0056% | -0.3628% | -1.7916% | <= 0 |
| 8GiB | -0.0028% | -0.3600% | -1.7888% | <= 0 |
| 16GiB | -0.0014% | -0.3586% | -1.7874% | <= 0 |
| 32GiB | -0.0007% | -0.3579% | -1.7867% | <= 0 |

**No frozen strategy reaches 5% at any grid file size, and the analytical single-switch
upper envelope does not reach 5% either** (maximum ~0.3545% in the dense scan).

## 11. Analytical vs calibrated-model comparison

Both models agree qualitatively: fixed-S normalized costs are scale-invariant, the model-implied
best fixed arm is 16 MiB, frozen-switch gains are negative, and the envelope is far below 5%.
Model B shifts all times up by ~2.5% and slightly changes the negative asymptotes
(16->4: -0.0007% vs -0.0007%; 4->1: -0.3579% vs -0.3653%; 1->256K: -1.7867% vs -1.8235%). No qualitative disagreement.

## 12. Figures

- `figures/fig1_completion_vs_filesize.png` — Completion time vs file size (fixed arms, both models).
- `figures/fig2_normalized_vs_filesize.png` — Normalized completion time T/T_ref vs file size.
- `figures/fig3_relative_gain_vs_filesize.png` — Frozen strategies: relative gain/loss (negative values shown; 5% threshold).
- `figures/fig4_absolute_saved_vs_filesize.png` — Frozen strategies: absolute time saved.
- `figures/fig5_envelope_vs_filesize.png` — EXPLORATORY single-switch upper envelope (grid + dense residue scan; 5% threshold).
- `figures/fig6_models_comparison.png` — Analytical vs Stage 0S-calibrated fixed-S curves + Stage 0S observed medians.
- `figures/fig7_tail_contribution_vs_filesize.png` — Tail contribution vs total completion time as F grows.

All figures are model output except the explicitly marked Stage 0S observed medians in Fig. 6;
no analytical uncertainty bars are drawn (the model is deterministic).

## 13. Stage 1 implication

**ANALYTICALLY INSUFFICIENT UNDER FROZEN MECHANISM.** See `STAGE1_IMPLICATIONS.md`.

## 14. Limitations

- Deterministic model: no timing noise; Stage 0S residual SD was 0.0008 in log time, so noise
  is not expected to create systematic headroom.
- Model B extrapolates the linear fit to partial range sizes (the fit is nearly perfect, R^2 = 0.9999999885).
- The simulator assumes work-conserving allocation and identical service times per size; it
  does not model TCP effects, connection reuse, or server-side scheduling beyond the frozen model.
- The upper envelope is exploratory and was not used to change the frozen protocol.

## 15. Statement

No empirical network experiment was performed for this analysis. No mm-link, server, client,
tc/netem, or replay process was started; no confirmatory Stage 1 run was created.
