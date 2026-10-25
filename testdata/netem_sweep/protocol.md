# Delay and loss sweep (protocol)

Frozen 2026-10-25 before the first data run. Amendments go at the end, dated,
and never edit the sections above them.

## Why

Every controlled comparison so far used one network setting: 40 ms each way
(80 ms RTT) and 0.4 % loss. Review 3 lists this as a limitation. This screen
tests whether the two main comparisons against aria2 keep their direction at
other delays and loss rates. Those comparisons are:

- the paced D2 workload (ours 16×4 MiB);
- the capped server with the cap from 8 s (ours 16×32 MiB, corrected
  engine).

## Design

- **Harness:** `testdata/rl_capped/tools/rl_bench.go`, unchanged. The 512 MiB
  file, the server models and the frozen aria2 flags are as in
  `testdata/split_ablation`.
- **Grid:** one-way netem delay of 10, 40 or 100 ms, times loss of 0, 0.4 or
  2 %. That gives 9 cells per workload and 18 cells in all. The (40 ms, 0.4 %)
  cell repeats the existing setting.
- **Workloads and arms:**
  - D2: `ours_16_4M` against aria2;
  - CAP8: `ours_16_32M` against aria2.
- **Blocks:** 3 per cell, with the two arms shuffled within each block (seed
  20261029). That is 108 runs, a screen.
- **Validity:** as in the split ablation, including netem being applied.
  Invalid runs are kept and reported.

## Predeclared analysis (`tools/analyze.py`)

- Per cell: the paired ours/aria2 ratio for each block, their geometric mean,
  and the minimum and maximum. With 3 blocks, no bootstrap interval is
  reported.
- Per workload: how many of the 9 cells have all 3 block ratios below 1
  (ours faster in every block), all above 1, or mixed.

## Interpretation rules (fixed in advance)

- **"The direction holds across the grid"** may be said for a workload only
  if every one of its 9 cells has all block ratios on the same side of 1 as
  the (40 ms, 0.4 %) cell. Otherwise the reports name the cells where it
  changes or is mixed.
- **Descriptive screen only.** One machine, emulated network, 3 blocks per
  cell. No claim about real networks.
