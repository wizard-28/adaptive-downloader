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

- **A1 (2026-10-18 12:05 IST, during S1; informational, no design change).**
  The access network (Tata) disconnects nightly at about 01:30 IST. No slot
  window overlaps it: each slot takes about 75 min and the latest starts at
  21:00. The client's public IP may change after reconnection. Any run hit by
  an outage is kept as invalid with its error, per the validity rules.
- **A2 (2026-10-18 ~13:20 IST, after S1; implementation change, with the
  reason found in the data).** In S1, all 16 project-downloader runs against
  `tuna` failed. The server returned **403** to every range request,
  including N=1, while curl and aria2 succeeded. A direct check showed that
  `tuna` rejects the Go default User-Agent `Go-http-client/1.1` with 403 and
  accepts any other value (`stage0_ua_check.txt`). This is a real-world
  observation in its own right: a stock Go client is refused by a major
  public mirror.
  - **Fix:** an optional `UserAgent` field on `fetcher.Config` and
    `downloader.Config`. Empty keeps the old behaviour exactly; a regression
    test covers both cases. From S2 onward the downloader sends
    `adaptivedl-research-pilot/1.0 (live_pilot_l1)` to every server
    (`manifest.json` `user_agent`). This also meets the pilot's intent of an
    identifying UA. aria2 and curl keep their own defaults.
  - **Data handling:** the S1 `tuna` runs stay in `raw/S1.jsonl` as invalid,
    and the `tuna`/S1 condition is INCOMPLETE. To recover a midday `tuna`
    condition, a **supplementary slot `S1T`** (`tuna` only, the full 10 arms
    × 2 reps, new UA) runs right after this amendment (~13:25 IST). It is
    analysed as its own condition (`tuna`/S1T) and labeled as such.
  - **Operator error during S1T:** the first S1T launch ran on all servers,
    because the scheduler had no server filter. It was stopped after two
    `kcdn` runs, which stay in `raw/S1T.jsonl`. A third, in-flight `kcdn` run
    was aborted without a record. `kcdn`/S1T is therefore INCOMPLETE and is
    excluded automatically. The scheduler gained an `@server` filter and S1T
    was restarted with `tuna` only (`logs/scheduler.log`).
  - **Caveat:** in S1 the other three servers saw the Go default UA, and in
    later slots they see the new one. UA is not expected to affect
    throughput on servers that accept both, but this is an uncontrolled
    change between S1 and later slots, and it is stated here.
- **A3 (2026-10-19, after S3; data routing, decided before any headroom
  computation).** The operator reports that **S3 ran on a different access
  network**, not the Tata line in `manifest.json` `client`. The operator
  confirmed this after being asked about S3's anomalies, and was back on Tata
  afterwards. S3 itself showed:
  - 68 of 80 valid runs;
  - a window from about 08:08 to 08:40 IST with DNS failures and
    retry-exhausted ranges;
  - peaks of about 12 MB/s, where S1 and S2 stayed near 2.4 MB/s.

  The predeclared conditions assume a single client access network, so:
  - **Data handling:** `raw/S3.jsonl` is moved unchanged to
    `raw_other_network/S3.jsonl`, with all 80 records kept, invalid ones
    included. The primary analysis (`raw/`) therefore covers Tata slots only.
    S3 is analysed separately with the same frozen script
    (`--raw raw_other_network --out analysis_other_network`). It is reported as
    a supplementary, descriptive, single-slot result on an unidentified
    second access network, and is never pooled with the Tata conditions.
  - **S5 becomes required:** S5 (Tue 10-20 ~08:00 IST, already scheduled)
    replaces S3 as the Tata morning slot. If S5 cannot run, the primary
    analysis has no morning slot, and that is stated.
  - **Caveat:** this routing was decided after seeing S3's raw run times, but
    before any headroom or ranking computation over S3 or the Tata set. The
    cue was the operator's report, not the results.
- **A4 (2026-10-20 ~08:05 IST, during S5; re-run of an interrupted slot).**
  The operator reports that the internet dropped briefly as S5 began (Tata,
  08:00 IST). S5 wrote 8 records, only 2 of them valid. The invalid ones are
  retry-exhausted ranges on `kcdn`. One in-flight `kcdn` curl run was killed
  without a record, which was the operator's stop. The scheduler and runner
  were stopped by PID (`logs/scheduler.log`).
  - **Data handling:** the 8 S5 records stay in `raw/S5.jsonl`, untouched.
    `kcdn`/S5 is INCOMPLETE and is excluded automatically. The other servers
    have no S5 records.
  - **Replacement slot S5R:** S5R runs the full schedule (4 servers × 10 arms ×
    2 reps) from about 08:06 IST on the same Tata line. It is analysed as its
    own condition set and is the Tata morning slot that A3 requires.
