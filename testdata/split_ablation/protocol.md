# Split ablation: is aria2's win under the cap an engine artifact? (protocol)

Frozen before the first ablation run. Amendments go at the end, dated, and
never edit the sections above them.

## Background (what was known before this protocol)

The published capped-server results (`testdata/n_dependent_server_joint_ns/`,
paper §6–§7) have aria2 faster than every configuration of our downloader
under the soft concurrency cap: P1-capped aria2 68.3 s vs selector 84.2 s, and
P2 aria2 53.6 s vs causal controller 58.7 s. An audit on 2026-10-24 found two
engine differences, neither of which is adaptive control:

1. **Whole-request timeout.** Our runs used a 30 s deadline covering the whole
   request, body included. Under the cap, 16 MiB and 32 MiB ranges at 16
   connections mostly hit it and restarted from zero (for example
   `gate_capped_n16_s33554432_r0`: 36 of 50 requests failed, run aborted).
   aria2's `--timeout=30` acts on a stalled connection, not on a slow but
   progressing one.
2. **No dynamic segmentation.** The server log of `p1_capped_aria2_r00` shows
   aria2 starting 16 pieces of 32 MiB at 0.4 s. From 53 s on it repeatedly
   splits the unfinished part of slow pieces and hands it to free
   connections. Our downloader keeps in-flight ranges immutable (AGENTS §5),
   so its slowest range sets the finish time.

A pre-protocol diagnostic, in the scratchpad and not part of this study's
data, ran our downloader as 16 × 32 MiB with no request deadline and no
splitting. It took 114.7 s and 113.9 s at CAP0 (aria2 68.1 s), and 56.4 s at
CAP8 (aria2 53.2 s). This motivated the design below and is disclosed here.

## Engine change under test

`downloader.Config.SplitStragglers` is opt-in and off by default, so every
earlier result is unchanged. When no unallocated bytes remain, an idle worker
takes the in-flight range with the most unreceived bytes. If at least
2 × 1 MiB remain, it moves that request's end to the middle of the unreceived
part, splits the range in the tracker, and fetches the second half. Bytes
already received stay with the original request. A retry of either part uses
its current (possibly shortened) boundaries. This is a documented exception
to AGENTS §5, and it applies only with the option on. Tests:
`internal/downloader/split_test.go`, `internal/fetcher/limit_test.go`,
`internal/rangetracker` `TestSplit*`.

`-no-request-timeout` (`NoRequestTimeout`) removes the whole-request deadline.
The run is still bounded by the harness, and the dial timeout is unchanged.

## Design

- **Harness:** `tools/split_bench.go`, a copy of the frozen `ns_bench.go` with
  the two flags added. The server model, cap (C = 8 MiB/s, γ = 0.05),
  schedule t1, 512 MiB file, netem settings and frozen aria2 flags are
  unchanged. Netem is `delay 40ms loss 0.4%` on `lo`. Measured 2026-10-24
  with `ping` and 0 % loss, that gives an **80 ms round trip** (80.1 ms,
  against 0.04 ms without netem), because both directions cross `lo`'s
  egress. The 0.4 % loss likewise applies in each direction. Earlier papers'
  text calling this "40 ms RTT" is corrected separately.
- **Scenarios:** CAP0 (cap from the start, as P1-capped), CAP8 (cap from 8 s,
  as P2), HEALTHY (no cap, as P1-healthy), and D2 (the D2 pacing schedule
  inside this harness; this is not the frozen D2 benchmark).
- **Arms and order:** `manifest.json` lists the arms per scenario.
  - `_T30` means the original 30 s whole-request deadline, with no split. It
    reproduces earlier results as an environment anchor.
  - `_NT` means no request deadline; `_split` means SplitStragglers on.
- **Blocks:** 10 blocks per scenario. Arms are shuffled within each block
  (seed 20261024), and scenarios run in the order CAP8, CAP0, HEALTHY, D2.
  Each run uses a fresh server, output and netem.
- **Validity:** a run is valid when it exits 0, its content is verified and
  it has no error. Invalid runs stay in `runs/` and `index.jsonl` and are
  excluded from ratios, with the count reported. A block missing either arm
  of a comparison is dropped from that comparison only, and the drop is
  reported.
- **Smoke:** before freezing, two smoke runs (`fixed_16_32M_NT_split` at CAP0
  and CAP8) check that the harness works. They live in `smoke/`, are not
  data, and their times are reported.

## Predeclared comparisons (`tools/analyze_split_ablation.py`)

All are paired by block. Each reports the geometric-mean ratio with a 95 %
percentile bootstrap over blocks (10,000 resamples, seed 20261024).

- **C1, timeout artifact (CAP0, CAP8):** `fixed_16_32M_NT` / `fixed_16_1M_T30`.
- **C2, splitting (CAP0, CAP8):** `fixed_16_32M_NT_split` / `fixed_16_32M_NT`.
- **C3, engine vs aria2 (all scenarios):** every ours arm / `aria2`. The
  ours arm with the lowest geometric mean is labeled "best ours (selected
  after the fact)".
- **C4, adaptive claim, re-checked:**
  - CAP8: `causal_joint_NT_split` / each stationary `_NT_split` arm, and
    against the best of them (an oracle stationary choice). Also
    `causal_joint_T30` / `fixed_16_1M_T30`, which replicates the published
    +23.4 %.
  - CAP0: `selector_NT_split` / each stationary `_NT_split` arm and the best
    of them.
- **C5, no harm (HEALTHY, D2):** each `_NT_split` arm / its `_T30`
  counterpart with the same N and S (HEALTHY `fixed_16_1M`, D2
  `fixed_16_4M`).

## Interpretation rules (fixed in advance)

- A ratio below 1 whose CI upper bound is below 1 means "faster", and the
  mirror case means "slower". A CI that includes 1 means "not
  distinguishable".
- The paper's "aria2 wins under the cap" statement is revised only according
  to C3:
  - if the best ours arm is faster than aria2 or not distinguishable from it,
    the statement is replaced with the engine explanation;
  - if aria2 is still faster, the statement stays, with the remaining gap
    quantified.
- The published adaptive claims (+23.4 % within-download, selector results)
  stay as stated for their predeclared comparison sets. The paper adds C4. If
  the adaptive arm is not faster than the best stationary `_NT_split` arm,
  the paper must say that once the engine is fixed, a stationary
  configuration matches or beats the adaptive controller in that scenario.
- No ranking beyond these comparisons. This is a controlled ablation on one
  constructed server model.
