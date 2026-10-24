# Online concurrency tuner: does a FastBioDL-style tuner beat a fixed setting? (protocol)

Frozen before the first run on 2026-10-24. Amendments go at the end, dated,
and never edit the sections above them.

## Why

The Review 1 proposal cited FastBioDL (Swargo, Arslan and Arifuzzaman, 2025)
and Falcon-style online optimizers, which tune the number of streams online
from measured throughput. The study never ran such a tuner as a baseline. This
experiment adds one, so the "adaptive control did not pay off" conclusion is
checked against the prior-work technique and not only against our own simple
rules.

## Policy under test

`internal/policy.OnlineTunerPolicy` (tests in `online_test.go`). It is a
re-implementation of the idea, not of either system's code:

- It measures aggregate throughput, T, over fixed 2 s windows. T is the bytes
  of all attempts completed in the window divided by the window length.
- Each window gets a utility score U = T / K^N. Here K = 1.0, so there is no
  per-connection penalty and the score is throughput alone, as in FastBioDL's
  throughput objective.
- N keeps moving the same way while U is no more than 5 % worse than the
  previous window. Otherwise the direction reverses and the step halves,
  never going below 1.
- It starts at N = 4 with a step of 4, and N is bounded to [1, 16]. At a bound
  it holds until a worse window turns it around.
- S is fixed at 1 MiB, so the tuner adapts concurrency only, as FastBioDL
  does.
- The engine settings are the fixed ones from the split ablation: no
  whole-request deadline, and straggler splitting on.

## Design

- **Harness:** `tools/tuner_bench.go`, a copy of
  `testdata/split_ablation/tools/split_bench.go` with an additive
  `-mode online`. The server model, cap (C = 8 MiB/s, γ = 0.05), t1 schedule,
  512 MiB file, netem settings (40 ms each way, i.e. an 80 ms round trip, and
  0.4 % loss) and frozen aria2 flags are unchanged.
- **Scenarios:**
  - CAP8: the cap switches on at 8 s, so the best N changes mid-download.
  - HEALTHY: no cap, so more connections should help.
- **Arms:** `online_tuner`, `fixed_16_1M_NT_split` (the same engine at the
  upper bound), `fixed_4_1M_NT_split` (the same engine at the start point) and
  `aria2`.
- **Blocks and order:** 8 blocks per scenario. Arms are shuffled within each
  block (seed 20261025), and CAP8 runs before HEALTHY. Every run uses a fresh
  server, output file and netem setup.
- **Validity:** the same rules as the split ablation. A run is valid when it
  exits 0, its content is verified and it has no error. Invalid runs are kept
  and reported, and are excluded from ratios.
- **Smoke:** one tuner run per scenario, kept in `smoke/`. These runs are not
  data.

## Predeclared comparisons (`tools/analyze_online_tuner.py`)

All comparisons are paired by block. Each reports the geometric-mean ratio
with a 95 % percentile bootstrap (10,000 resamples, seed 20261025).

- **T1:** `online_tuner` against each stationary arm, and against the better
  of the two, chosen after the fact as an oracle stationary choice.
- **T2:** `online_tuner` against `aria2`.
- **Descriptive:** the tuner's final N, its number of moves, and the N values
  it visited.

## Interpretation rules (fixed in advance)

- If the CI upper bound is below 1, the tuner is "faster". If the lower bound
  is above 1, it is "slower". Otherwise the two are "not distinguishable".
- If the tuner is not faster than the best stationary arm in either scenario,
  the reports must say that an online tuner of the kind used in prior work also
  did not beat a well-chosen fixed setting on this platform.
- If it is faster in CAP8, the reports must say so. They must also say that
  this is the one scenario built so that the best N changes during a download.
- No ranking beyond these comparisons. This is one constructed server model,
  and one tuner configuration chosen before any run. It was not tuned
  afterwards.

## Amendment A1 (2026-10-24, before any data run)

The harness selects a policy with `-policy`, not `-mode`. Because of this the
two smoke runs exited with a flag error before downloading anything, and no
run produced data. The manifest now passes `-policy online`. Nothing else
changes. The failed smoke attempts are kept in `index_smoke_attempt1_flagerror.jsonl`.
