# Real-server check of the adaptive arms on public mirrors (protocol)

Frozen 2026-10-25, before any run of this check. Amendments go at the end,
dated, and never edit the sections above them.

## Why

Every adaptive result in this project comes from constructed server models
(loopback, netem, a server we wrote). This check asks a narrower question
on real servers: **do the v2 agent and the drop rule cost anything on
public mirrors, whose behaviour we did not design?**

On these mirrors the live pilot (`testdata/live_pilot_l1`) found little
headroom: the global best fixed setting was within 3.7 % of the
per-condition best. So this is mainly a **no-harm check**, not a search for
wins. A win here would be unexpected, and is reported as descriptive only.

## What is fixed

- **Client and link:** the live pilot's devcontainer and home broadband
  (one client, one access link).
- **Servers:** `kcdn` (cdn.kernel.org), `kedge` (mirrors.edge.kernel.org)
  and `riken` (ftp.riken.jp), with the same URLs as the live pilot. Any
  server that fails a 1-byte range probe (`206`) before the first block is
  dropped and recorded. It is not replaced.
- **File:** `linux-3.0.tar.xz`, 63,798,008 bytes, checked against
  kernel.org's published SHA-256 (as in the live pilot).
- **Etiquette (as in the live pilot):**
  - an identifying User-Agent;
  - no more than 16 connections per server;
  - a 5 s gap between runs;
  - a server is aborted for the rest of the check after any `429` or `503`.
- **Engine:** our downloader with no request deadline and with straggler
  splitting, in every arm (the corrected engine). Range size 1 MiB.
- **Arms:**
  - `v2`: the v2 model (`testdata/rl_cap_train_v2/model/dqn_cap_v2_candidate.json`,
    sha256 prefix 27657be21038), start N = 16, window 16;
  - `drop`: the C=8-tuned drop rule, N-only, start N = 16, k = 3,
    ratio 0.5, capped N = 4;
  - `fixed_16_1M` and `fixed_8_1M`. 8 × 1 MiB was within 3.7 % of the best
    per-condition setting in the live pilot.
- **Blocks:** 5 per server, with the arms shuffled within each block (seed
  20261101). That is 3 × 4 × 5 = 60 runs, about 3.8 GB in all.
- **Validity:** a run is valid if it exits cleanly and the SHA-256 matches.
  Invalid runs are kept and reported, never re-run silently.

## Predeclared analysis (`tools/analyze.py`)

Per server, paired by block: the geometric-mean ratio with a 95 % percentile
bootstrap (10,000 resamples, seed 1) and a paired t-interval on the log
ratios. Where the two disagree, the less favourable verdict is used.

- `v2` against the better fixed arm (the lower live geometric mean), and
  against each fixed arm;
- `drop` against the better fixed arm;
- `v2` against `drop`;
- **Descriptive:** per arm, the median final N and number of moves; for
  `drop`, the number of runs in which it switched to N = 4.

## Interpretation rules (fixed in advance)

- **No harm:** for an arm, "no harm detected on server X" if its ratio
  against the better fixed arm is not "slower" and its point estimate is at
  most 1.05. Otherwise "slower than the best fixed setting on server X by
  r".
- **Summary sentence:** "On k of 3 public mirrors, the v2 agent showed no
  harm against the best fixed 1 MiB setting (and the drop rule on j of
  3)."
- Any "faster" result is reported as descriptive and unexpected, with the
  live pilot's 3.7 % headroom stated beside it.
- **Scope:** descriptive; one client, one access link, one time window, 5
  blocks. The servers' behaviour is opaque and may change. This does not
  test whether adaptation helps on real servers in general.

## Amendment A1 (2026-10-25, before any run): range-probe attempts

The protocol did not say how many attempts the range probe allows. It is
now fixed at up to 3 attempts of 20 s each, with any `206` passing. No run
data existed when this was written.

Probe results at 21:5x UTC:
- `kcdn`: `206`;
- `riken`: `206`;
- `kedge`: timed out on the first attempt, then returned `206` in 9.8 s
  on the second.

All three are kept. kedge's slow first response is disclosed here, and its
results are read with that in mind.
