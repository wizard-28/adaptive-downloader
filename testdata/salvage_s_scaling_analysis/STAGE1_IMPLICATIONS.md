# STAGE1_IMPLICATIONS — Salvage Protocol v7 scaling / headroom analysis

**Verdict: ANALYTICALLY INSUFFICIENT UNDER FROZEN MECHANISM**

This document reports the analytical simulation result only. It does not change the frozen
protocol and it does not authorize any Stage 1 run.

## Result summary

- Frozen strategy gains (relative to the model-implied best fixed arm) across the entire
  file-size grid are **negative**: 16MiB->4MiB max -0.0007%, 4MiB->1MiB max -0.3579%,
  1MiB->256KiB max -1.7867% (Model A; Model B agrees qualitatively).
- At the frozen baseline (F = 512 MiB) the 16MiB->4MiB strategy yields -0.0447%.
- Even the EXPLORATORY optimally timed single switch is <= 0 on the main grid and reaches at
  most ~0.3545% in a dense residue scan — far below the frozen 5% practical threshold.
- Increasing the file size does **not** increase the relative headroom: the one-time tail effect
  is diluted by the growing bulk (relative tail share falls as 1/F, see Fig. 7), and the frozen
  thresholds fire at exactly full bursts where switching only adds per-request overhead.

## Why

Under the frozen static mechanism the per-request service time is `D + s/R`. The completion
time of an exactly-aligned file is `(F/(N*s)) * (D + s/R)`, so the fixed-arm cost differences
are per-byte overheads: `T/T_ref = 1 + R*D/s` — scale-invariant. The only one-time effect is
the final tail (<= ~D + S0/R); it is bounded and therefore contributes a relative gain that
decays with F. Because B_switch = N*S0 fires exactly when the remaining work equals one full
burst of the starting arm, replacing it with smaller chunks cannot shorten the tail and adds
D overhead. A well-timed switch can capture at most the bulk overhead difference (~0.36% for
16->4), and only at file sizes where a smaller fixed arm happens to be the reference.

## Consequence for Stage 1

- The current frozen Stage 1 design (512 MiB, the three frozen strategies, the frozen trigger)
  is **not** mechanistically capable of producing a practically meaningful (5%) effect under
  the frozen mechanism.
- **No amendment is proposed here.** A larger file size is not justified either: it reduces the
  relative headroom rather than increasing it. Any change to the mechanism, trigger timing, or
  design would require a separately frozen protocol decision; this analysis deliberately does
  not modify the frozen protocol.

## Evidence

- `analysis/scaling_analysis.json`, `analysis/fixed_s.csv`, `analysis/frozen_switches.csv`,
  `analysis/envelope.csv`, `analysis/dense_modular.csv`
- `reports/scaling_headroom_report.md`, `figures/fig1..fig7`
- Deterministic simulator: `tools/scaling_analysis.py` (no RNG, no network I/O).

## Integrity

No empirical network experiment was performed: zero downloads, zero mm-link processes, zero
server/client runs, zero tc/netem, zero packet replay. The frozen protocol and all Stage 0S /
oracle / Stage A/B/C artifacts were not modified.
