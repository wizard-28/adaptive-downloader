# More open-source baselines in the same testbed (protocol)

Frozen 2026-10-25 before the first data run. Amendments go at the end, dated,
and never edit the sections above them.

## Why

Review 3 (Section 10) lists more baselines as the next extension. aria2 has
been the only external downloader so far. This screen adds three open-source
segmented downloaders that run in the Linux testbed, under the same server
models and the same method. IDM is excluded: it is closed source, runs only
on Windows, and cannot be scripted for randomised runs here.

## Systems

All tools come from the Ubuntu 24.04 packages, at 16 connections each, which
matches the frozen aria2 flags (`-x16 -s16`):

| System | Version | Invocation (`tools/ext_bench.go`) |
|---|---|---|
| axel | 2.17.13 | `-q -n 16 -T 30` |
| wget2 | 2.1.0 | `--no-config -q --max-threads=16 --chunk-size=1M --tries=3 --timeout=30`, writing into the run directory |
| lftp | 4.9.2 | `pget -n 16`, with `net:timeout 30`, `net:max-retries 3` |
| aria2 | 1.37.0 | the frozen flags |
| ours | — | the best fixed setting on the fixed engine from the split ablation: CAP8 and CAP0 16×32 MiB, HEALTHY 16×1 MiB, D2 16×4 MiB |

**Known before the screen (functional smoke, 32 MiB, no netem; not data):**

- **wget2 2.1.0 used one connection at a time** for a single file over
  HTTP/1.1 in this testbed. This happened with both `-O` and directory output,
  despite `--max-threads=16 --chunk-size=1M`. It is run as configured, and its
  observed peak concurrency is reported.
- **wget2 probes with HEAD.** The controlled server paces every response body,
  including the discarded body of a HEAD, which real servers do not. The
  harness therefore answers HEAD at once with `Accept-Ranges` and
  `Content-Length`. GET handling is unchanged, and no other system sends HEAD.
- axel and lftp opened 16 parallel connections, and all three outputs matched
  byte for byte.

## Design

- **Harness:** `tools/ext_bench.go`. It is a copy of
  `testdata/rl_capped/tools/rl_bench.go` with the three additive systems and
  the HEAD answer. The server model, schedules, cap (C = 8 MiB/s,
  γ = 0.05), 512 MiB file and netem (40 ms each way, 0.4 % loss) are
  unchanged.
- **Scenarios:** CAP8, CAP0, HEALTHY and D2, defined as in
  `testdata/split_ablation`.
- **Blocks:** 5 per scenario, with the 5 systems shuffled within each block
  (seed 20261028). That is 100 runs, a screen.
- **Validity:** as in the split ablation: content verified, no error, netem
  applied. Invalid runs are kept and reported, never re-run silently. A tool
  that exceeds the 600 s process timeout is recorded as invalid with its
  error.

## Predeclared comparisons (`tools/analyze.py`)

All comparisons are paired by block, using the geometric-mean ratio with a
95 % percentile bootstrap (10,000 resamples, seed 1).

- Ours against each tool, per scenario.
- Each tool against aria2, per scenario.
- Descriptive: each system's peak number of concurrent requests at the server
  and its request count.

## Interpretation rules (fixed in advance)

- The CI rule from the other studies applies: "faster" if the CI upper bound
  is below 1, "slower" if the lower bound is above 1, "not distinguishable"
  otherwise.
- This is a 5-block screen on constructed server models. The reports may say
  how each tool compared in this testbed, and may explain a gap only through
  a mechanism visible in the server logs, such as concurrency, request count,
  or ranges started before a rate change.
- No general ranking of the tools is claimed. The tools were run at one
  configuration each, and other flags could change the results.

## Amendment A1 (2026-10-25, before any data run): wget2 single-connection behaviour verified

The single-connection observation was checked outside our harness and
against wget2's documentation and source.

- **Independent reproduction.** `tools/wget2_concurrency_check.py` is a
  throttled Python range server, about 2 MiB/s per response, that logs the
  number of requests in flight. With
  `wget2 --no-config --max-threads=16 --chunk-size=1M -O out.bin <url> -d`
  (16 MiB), wget2 sent one HEAD and then 16 range GETs. All 16 used the same
  client port, and the in-flight count never exceeded 1. In the debug log,
  only downloader thread `[0]` acts, and the chunks are dequeued one at a time
  ("dequeue chunk 1/16", then "2/16", and so on).
- **Documentation.** The wget2 manual (`docs/wget2.md`) describes
  `--chunk-size` as "Download large files in multithreaded chunks" and
  `--max-threads` as "the maximum number of concurrent download threads for
  a resource" (default 5). So parallel chunks are the documented intent.
- **Source (`src/wget.c`, master).** The main loop starts downloader threads
  only while `nthreads < config.max_threads && nthreads < queue_size()`. The
  chunk path after the HEAD response only signals existing workers
  (`wget_thread_cond_signal(worker_cond)`) and does not start threads. At
  start-up the queue holds one job, so one thread is started. In our runs that
  one thread then fetched every chunk. We infer that the chunks did not raise
  `queue_size()` enough to start more threads. This is our reading of the
  code, not a confirmed upstream bug report. No matching issue was found in
  the wget2 tracker.
- **Consequence for the screen (unchanged design).** wget2 runs as
  configured, and its peak concurrency is reported. The reports must call
  wget2 "single connection in this configuration (documented as
  multithreaded)", never "slow".
