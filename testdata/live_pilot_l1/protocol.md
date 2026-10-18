# L-P1 live-internet pilot: protocol (Descriptive)

Status: **FROZEN before slot S1** (commit recorded in `PROJECT_CONTEXT.md` §22.106).
Any later change is an amendment, appended below with date and reason.

## Question

On real public HTTPS mirrors reached over the author's real access network,
does the best **fixed** (N, S) configuration of the project downloader change
across servers and across time-of-day slots? How much would a clairvoyant
per-condition choice gain over one global fixed configuration? That gain is an
upper bound on what any adaptive selection across these conditions could add.

This is a **descriptive pilot**. No RL or adaptive policy is run, and no mode
is ranked. All results are labeled *Descriptive*, and none is a confirmatory
claim.

## Design

- **Client:** the devcontainer on the author's machine, on a single access
  network for the whole pilot. The network type and ISP AS are recorded in
  `manifest.json` (`client`). A network change mid-pilot is recorded as an
  amendment, and the affected slots are reported separately.
- **File:** `linux-3.0.tar.xz` (63,798,008 bytes), with SHA-256 taken from
  kernel.org `sha256sums.asc`. Every run is hashed and must match.
- **Servers:** those in `manifest.json` that passed the Stage 0 check: a
  `206` response to `Range: bytes=0-0` with the correct total size, plus
  non-zero 4 MiB single-stream throughput. Probe output is in
  `stage0_probe.txt`. Five of thirteen candidates passed. The pilot uses
  `kcdn` (cdn.kernel.org, a CDN), `kedge` (mirrors.edge.kernel.org, the
  origin mirror), `riken` (JP university) and `tuna` (CN university), with
  `jaist` held in reserve. The reserve replaces a server only if that server
  becomes unreachable, and any replacement is recorded as an amendment.
- **Smoke:** one rep against `kcdn` before S1, written to `smoke/`. It checks
  the implementation only and is excluded from analysis.
- **Arms (10):** the downloader with fixed N ∈ {1, 4, 8, 16} × S ∈ {1, 4 MiB};
  aria2 1.37.0 with `-x16 -s16 -k1M` (the frozen aria2-F concurrency and
  split settings; for the live internet the timeout is 60 s instead of 30 s
  and certificate checking stays on, see `manifest.json` `aria2_args`); and a
  single-stream `curl` 8.5.0 reference. curl may negotiate HTTP/2, while the
  downloader and aria2 use HTTP/1.1, so curl is a capacity marker only.
- **Downloader settings:** watchdog-only timeouts (`NoRequestTimeout`, run
  deadline `run_timeout_s`), 3 retries per range, and a 100 ms byte timeline
  via `ByteObserver`. Downloader, controllers and policies are unmodified.
- **Order:** for each rep, the server order is shuffled. Each (rep, server) is
  a block, and the arms are shuffled within it. The RNG is seeded from
  `seed` and the slot label (`cmd/live-pilot` `Schedule`). There is a 5 s gap
  between runs.
- **Slots (IST, start times):** S1 Sun 2026-10-18 ~12:30 (midday), S2 Sun
  10-18 ~21:00 (evening peak), S3 Mon 10-19 ~08:00 (morning), S4 Mon 10-19
  ~20:00 (evening peak), and optionally S5 Tue 10-20 ~08:00. A slot that
  cannot be completed is reported as incomplete, not dropped. Each slot runs
  all servers × 10 arms × 2 reps (80 runs, about 1 h).
- **Etiquette:** at most 16 connections to any server. If a server returns
  429 or 503, it is skipped for the rest of the slot and the skip is recorded.
  Volume is about 64 MB × 10 arms × reps × servers per slot. The Go
  downloader sends the default Go User-Agent (the fetcher has no UA hook, and
  it was not changed under scope discipline).

## Validity

- A run is **valid** if it exits cleanly and its checksum and size match.
  Invalid runs are kept in `raw/` with the error, never silently dropped or
  re-run.
- A (server, slot) **condition** is complete when every fixed cell has at
  least one valid run. Incomplete conditions are listed and excluded from the
  headroom computation.
- A complete condition whose fixed-cell times all lie within 5 %
  (max/min ≤ 1.05) is labeled **ACCESS_LINK_BOUND**. It is reported, and
  headroom is computed both with and without these conditions.

## Predeclared analysis (`tools/analyze_live_pilot.py`)

1. For each condition, report the best fixed cell (lowest geometric-mean
   completion time over valid reps), the full ranking, and the cells within
   5 % of the best. The headline is whether the top cell changes across slots
   (per server) and across servers.
2. **Selection headroom:** the global cell minimises the geometric mean over
   conditions of (cell time / condition best). Headroom is that geometric
   mean minus 1, with a 95 % percentile bootstrap over conditions (10,000
   resamples, seed 20261018, global cell re-chosen inside each resample).
   Also report the number of conditions where the global cell is within 5 %
   of the condition best.
3. **Within-run variability:** 1 s throughput bins from the downloader
   timelines (first and last bin dropped). Report the median coefficient of
   variation and the median fraction of bins below 50 % of the run median,
   per server.
4. aria2 and curl are reported as their time ratio over the condition's best
   fixed cell, as context only.

## Interpretation rules (fixed in advance)

- Small headroom (bootstrap upper bound < 5 %) means that in this pilot one
  fixed configuration was near-best across all observed conditions. This
  supports, but does not prove, the thesis that real-condition variability
  did not move the best setting.
- Large headroom means the best fixed setting did move across conditions.
  That motivates adaptive selection as future work. It is not evidence that
  any particular adaptive or RL method would capture the headroom.
- In either case: one client and one access network, small n, and no
  long-term reproducibility of public CDN and mirror behaviour.

## Amendments

(none)
