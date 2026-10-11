# Track 1 — Fresh Joint N+S D2 Benchmark Report

**Classification: C = `BEATS_ARIA2_ONLY`.** Under the frozen D2 workload, the joint
adaptive N+S policy (existing heuristic `JointPolicy`, frozen configuration) adapted
both N and S online in 20/20 runs, beat the frozen aria2 configuration by 12.4 %,
but was **2.32x slower than the best fixed configuration (N=16, S=4 MiB)**. Beating
aria2 alone is not adaptation evidence; the adaptation-value test (joint vs best
fixed) failed.

Generated: 2026-09-29T11:51:36Z

## 1. Scope and frozen design

This is Track 1 only (no Track 2 work, no server changes, no RL). The design was
frozen in `manifest/track1_manifest.json` before execution:

- **Workload:** exact D2 mechanism — 512 MiB logical object, HTTP/1.1, per-response
  pacing 640 KiB/s (0-2 s) -> 160 KiB/s (2-4 s) -> 2500 KiB/s (>=4 s), rate sampled
  at request start and held, tc/netem 40 ms + 0.4 % on `lo`, full content/hash
  verification, fresh server/output per run.
- **Modes:** fixed (16, 4 MiB); N-only (`ThroughputPolicy`); S-only
  (`ChunkSizePolicy`); joint (`JointPolicy`). LinUCB intentionally not run.
- **Frozen controller config:** initial (N=4, S=4 MiB); N in [1,16], step +/-1;
  S transition `S' = clamp(S +/- 1 MiB, 256 KiB, 16 MiB)` (reachable set
  {256 KiB} union {1..16 MiB}); one serialized decision per completed attempt;
  thresholds +/-10 %; policy window 4 attempts; 30 s timeout; 3 same-range retries.
- **Design:** 20 blocks, all four modes per block in independently randomized order,
  80 planned runs, no early stopping, no tuning after results; invalid runs
  preserved and never replaced.

## 2. Execution summary

| | |
|---|---|
| Valid runs | **80 / 80** (0 invalid) |
| Blocks | 20 (4 modes each) |
| Content verification | 80/80 byte-exact against the logical generator |
| Retries / timeouts | 0 across all 80 runs |
| aria2 consistency runs (frozen flags, outside matrix) | 3; mean 53.27 s vs frozen 30-pair mean 53.56 s (-0.5 %) |

## 3. Completion-time results

| mode | n | mean | median | min | max |
|---|---|---|---|---|---|
| fixed (N=16, S=4 MiB) | 20 | 20.25 s | 20.19 s | 19.88 s | 20.88 s |
| N-only adaptive | 20 | 36.93 s | 36.90 s | 36.56 s | 37.74 s |
| S-only adaptive | 20 | 64.97 s | 65.05 s | 64.21 s | 65.56 s |
| joint adaptive N+S | 20 | 46.91 s | 46.85 s | 46.60 s | 47.40 s |

## 4. Primary comparisons (frozen rules)

| comparison | n pairs | geometric ratio | 95 % CI | wins/losses | beats? |
|---|---|---|---|---|---|
| **joint vs best fixed** (primary) | 20 | **2.3161** | [2.3023, 2.3289] | 0/20 | False |
| joint vs N-only | 20 | 1.2700 | [1.2653, 1.2745] | 0/20 | False |
| joint vs S-only | 20 | 0.7220 | [0.7195, 0.7246] | 20/0 | True |
| **joint vs frozen aria2** | 20/30 | **0.8759** | [0.8707, 0.8801] | n/a (unpaired) | True |

The `beats` rule (predeclared): geometric ratio <= 0.95 **and** 95 % CI upper bound
< 1.0. The joint policy is **2.32x slower** than the best fixed configuration and
**1.27x slower** than the N-only adaptive policy, but **12.4 % faster** than the
frozen aria2 configuration.

**The aria2 win is not adaptation evidence.** The fixed (16, 4 MiB) configuration
beats the same frozen aria2 reference by ~62 %; the joint policy's 12.4 % margin is
a weaker exploitation of the same phase-locked-pacing mechanism, not a gain from
adaptation.

## 5. Adaptation evidence (joint mode)

- **N changed** in 20/20 runs; **S changed** in 20/20 runs; **both changed** in
  **20/20 runs** (100 %) — genuine joint movement by the task's definition.
- Median final state **(N=6, S=6 MiB)**; all 20 runs reached exactly (6, 6) (the
  policy is deterministic and the signal is identical across runs).
- N trajectory: (4,4) -> (5,4) -> (6,4) -> (6,5) -> (6,6), i.e. +2 concurrency and
  +2 MiB chunk steps; 4 applied actions total, then 85 no-ops.
- All actions occur in a **~30 ms window at ~8.62-8.65 s** (first action median
  8.624 s, last action median 8.636 s); no action occurs before 8.6 s or after
  8.65 s.
- **Movement is away from the optimum**: the best fixed state is (16, 4 MiB); the
  joint policy stops at (6, 6 MiB) — N too low and S too large.

N-only final state: median **(8, 4 MiB)**; S-only final state: median **(4, 8 MiB)**.
Both adapt but stop far from the optimum as well.

## 6. Mechanism analysis (from recorded trajectories and request logs)

1. **Decision cadence + initial granularity delay the first decision.** Decisions
   require a completed attempt; with the frozen initial S=4 MiB at 640 KiB/s the
   first four attempts complete at ~6.6 s, so the first policy decision happens at
   ~6.7 s and the first *actionable* decisions at ~8.6 s — after the pacing
   recovery. The controller cannot act during the phase that determines the D2
   mechanism.
2. **The policy signal saturates.** The policies react to the windowed *per-request*
   throughput. It rises from ~607 KiB/s (slow phase) to ~2184 KiB/s at the recovery
   and then stays flat (~2210 KiB/s) for the remaining ~85 decisions. Once the
   +/-10 % rule stops firing, no further actions occur: the controllers freeze at
   (6,6) / (8,4) / (4,8). The recorded signal profile is in
   `analysis/mechanism_track1.json` (`joint_signal`).
3. **Adaptation lag vs file progress.** By the first actionable decision (~8.6 s)
   most of the 512 MiB remains, but the policy performs only +/-1 steps per
   actionable decision and has no further signal to continue; it cannot ramp N from
   4 to 16.
4. **Granularity/phase interaction.** Final request counts: fixed 128 (all 4 MiB),
   N-only 128, S-only 69 (8 MiB), joint 89 (6 MiB). Request start phases: fixed 16
   requests <2 s and 112 >=4 s; the adaptive modes have only 4 requests <2 s and
   the rest >=4 s (their first 4 MiB requests straddle the 2-4 s dip). Larger final
   S increases each request's service time (6 MiB at 2.5 MiB/s = 2.4 s) and reduces
   scheduling flexibility; combined with N=6 this yields the 46.9 s completion.
5. **Retries/timeouts played no role:** 0 retries and 0 failures in all 80 runs.

## 7. Classification (predeclared A-E)

| check | result |
|---|---|
| joint adaptation demonstrated (>=50 % of runs changed both N and S) | yes (100 %) |
| joint beats best fixed (ratio <= 0.95, CI upper < 1) | **no** (ratio 2.3161) |
| joint beats frozen aria2 | **yes** (ratio 0.8759, CI upper 0.8801) |
| **classification** | **C = `BEATS_ARIA2_ONLY`** |

## 8. Empirical results vs interpretation

**Empirical (measured, 80/80 valid):** the completion times, ratios, CIs, win/loss
counts, the recorded trajectories, final states, action times, request logs, and the
zero retry count above.

**Interpretation (not directly measured):** the explanation that the per-request
throughput signal saturation plus the decision cadence prevents the existing
heuristic policies from reaching the optimal concurrency. It is supported by the
recorded signal profile and action timestamps but is a mechanistic reading, not a
controlled experiment on the policy's signal.

## 9. Limitations

- The heuristic policies are the *existing* implementations with frozen bounds,
  step sizes, thresholds, and initial state; the study measures their behavior, not
  the best achievable adaptive policy.
- The aria2 comparison uses the immutable frozen 30-pair aria2 distribution
  (unpaired); three fresh aria2 runs under the same session conditions agreed
  within 0.5 %, supporting cross-session comparability, but the comparison remains
  unpaired.
- 20 blocks give narrow CIs for the observed deterministic behavior; the result is
  specific to the frozen D2 workload and controller configuration.
- No LinUCB, no Track 2 mechanism, no server changes were part of this pass.

## 10. Figures

- `figures/fig1_n_trajectory.png` — N trajectories (joint, N-only) vs the fixed optimum
- `figures/fig2_s_trajectory.png` — S trajectories (joint, S-only) vs the fixed optimum
- `figures/fig3_completion_comparison.png` — completion by mode and per-block pairing
- `figures/fig4_cumulative_bytes.png` — cumulative bytes vs time (median run per mode)
- `figures/fig5_request_sizes.png` — request-size distributions by mode
- `figures/fig6_phase_mechanism.png` — D2 phase mechanism: request start vs sampled rate
- `figures/fig7_signal_saturation.png` — the policy signal saturates after the recovery
- `figures/fig8_paired_ratios.png` — paired per-block ratios

## 11. Artifacts

- `manifest/track1_manifest.json` (frozen before execution),
  `manifest/run_schedule.json`; `per_run/` (80 results), `raw/` (runner logs +
  validity metadata), `logs/`; `analysis/track1_analysis.json`,
  `analysis/mechanism_track1.json`; `figures/fig1..fig8`; `tools/bench_ns.go`,
  `tools/run_track1.py`, `tools/analyze_track1.py`, `tools/mechanism_track1.py`,
  `tools/make_figures.py`, `tools/make_report.py`.
- Frozen prior evidence untouched: the 30-pair aria2 competitive benchmark, all
  mechanism screens, and the historical results are unchanged.

