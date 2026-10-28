### 22.14 Live Periodic-Exploration Benchmark (standard vs periodic)

**Status: completed.** A live (real-download) controlled comparison of standard
LinUCB vs periodic-exploration LinUCB at the 4 MiB horizon. Descriptive only —
**no ranking, no performance claim**. Nothing was tuned.

- **Research question:** does deterministic periodic exploration produce
  meaningful action-dependent adaptation when LinUCB interacts with the live
  downloader, or does it only increase action coverage?
- **Variants:** `linucb-standard` (`InitialExplorationRounds=1`,
  `ExplorationInterval=0`) vs `linucb-periodic` (`InitialExplorationRounds=1`,
  `ExplorationInterval=5`). Everything else identical: 4 MiB, chunk 32 KiB
  (min/max 32/128 KiB, step 32 KiB), concurrency 1 (1–4), thresholds 0.2/0.2,
  window 4, retries 3, timeout 30 s, alpha 0.2, `DefaultRewardConfig`, 5-action
  warm-up. 5 scenarios × 2 variants × 10 reps = **100 runs**.
- **New knob:** `ScenarioConfig.LinUCBExplorationInterval` (default 0; validated
  `>= 0`) + CLI `-linucb-exploration-interval` (applied to matrix scenarios via
  `applyLinUCBIntervalOverride`). Defaults and the frozen v1/v2/v3 behavior are
  unchanged.
- **Datasets:** raw `testdata/benchmark_v4/matrix_linucb_standard_reps10.json`,
  `.../matrix_linucb_periodic_reps10.json`; combined
  `testdata/benchmark_v4/matrix_linucb_exploration_reps10.json` +
  `..._reps10.txt`; posterior replay
  `testdata/diagnostics/linucb_periodic_exploration_live/{report.json,report.txt}`
  (deterministic replay of the frozen v4 runs).
- **Reproduce:** `go run ./cmd/experiment -matrix -sudo -reps 10 -json
  -file-size 4194304 -modes linucb -linucb-exploration-interval {0|5}`.
- **Correctness:** **100/100 valid** (50 standard, 50 periodic), 0 failed; every
  run `Err==""`, `OutputMatches`, `NoDuplicateRanges`, `NoMissingRanges`. Network
  `applied` for latency/loss/combined; tc clean. SHA-256 not implemented.
- **Decision horizons differ (periodic changes chunk allocation):** standard vs
  periodic mean totals — baseline 127→109, server-throttle 127→123, latency
  37→38, loss 103→90, combined 64.5→45; warm-up is 5 in both; periodic forces
  ~5–19 actions/run.
- **Action coverage (post-warm-up):** standard unique arms **1.0–1.4**;
  periodic **5.0** in every scenario (all arms exercised). The live forced-action
  schedule matched the expected cycle exactly (609 forced events, 0 mismatches).
- **Effective vs no-op:** standard is almost entirely no-ops (baseline
  `decrease-concurrency` 1220/1220 no-op; throttle `decrease-chunk-size`
  1220/1220 no-op; latency `increase-chunk-size` 30 effective / 290 no-op).
  Periodic produces many effective actions, e.g. loss `increase-concurrency`
  42 effective, effective concurrency changes 71; forced-only effective/no-op is
  reported separately (some forced actions are bound no-ops).
- **Concurrency:** standard made **0 effective concurrency changes** except loss
  (3), final concurrency 1.0 elsewhere (loss 1.30). Periodic made **effective
  concurrency changes 80/80/20/71/25** (baseline/throttle/latency/loss/combined),
  max concurrency 2 (loss 4), final concurrency 1.0/1.0/1.0/**2.30**/1.10. So in
  **loss** the policy actually investigated and held concurrency (final 2.30);
  elsewhere forced increases were reverted by the greedy `decrease` preference.
- **Chunk size:** periodic oscillates chunk via forced increase/decrease; final
  chunk similar to standard. Per §22.7 E the harness models no chunk-size
  tradeoff, so this is **not** interpreted as optimization.
- **Posterior evolution (deterministic replay):** the replay reproduces the
  recorded action sequence (standard 4586/4586; periodic per-scenario 100 %).
  Forced exploratory actions **do** move the posterior: mean score delta
  +0.06–0.54, 77–88 % of forced events increased the forced arm's UCB score,
  41–80 % improved its rank, and the forced arm reached **rank 1** in every
  scenario. Example (loss, `increase-concurrency`, decision 16): score
  1.803→2.396, rank 2→2, reward 3.458.
- **Does it change subsequent greedy choices?** Scenario-dependent. In **loss**,
  normal `increase-concurrency` selections rose from 58 (standard) to ~196
  (periodic) and final concurrency from 1.30 to 2.30 — posterior change altered
  behaviour. In baseline/throttle/latency/combined the **dominant arm preference
  was unchanged** (dominant fraction 1.000→0.78–0.88, but the same arm still
  dominates normal decisions); there the effect is **coverage only**.
- **Reward response:** aggregate throughput around effective concurrency changes
  (periodic): baseline 3.64M→4.23M, throttle 5.39M→4.24M, latency
  2.37M→2.11M, loss 4.64M→4.84M, combined 0.93M→0.77M — mixed, and one-step
  credit remains approximate, so no causal success claim.
- **Performance (mean completion ms; not ranked):** standard → periodic —
  baseline 1158→1108, server-throttle 1325→1209, latency 2645→2453, loss
  1543→1176, combined 7243→5689. These differ, but the horizons and action
  trajectories differ too; n=10, descriptive, **no superiority claim**.
- **Interpretation:** the measurements support **"exploration + posterior
  adaptation"** (forced actions update scores/ranks, and in `loss` change the
  subsequent greedy behaviour and held concurrency), with **scenario-dependent
  observable controller adaptation**. They do **not** support "exploration only"
  (the posterior does move) nor a general performance claim. Coverage alone is
  explicitly distinguished from meaningful adaptation.
- **Limitations:** deterministic (not learned) exploration; approximate credit
  assignment; chunk-size tradeoff not modelled; n=10; no seeds; descriptive
  statistics only; no ranking.

### 22.15 Final Six-Mode 4 MiB Benchmark

**Status: completed.** The end-to-end comparison placing both LinUCB variants
alongside the four deterministic controllers under the identical 4 MiB matrix.
Descriptive only — **no winner, no optimality, no superiority claim**; nothing
was tuned.

- **Dataset:** `testdata/benchmark_v5/matrix_6mode_4MiB_reps10.json` +
  `..._reps10.txt`. New `linucb-periodic` mode (`ModeLinUCBPeriodic`,
  `LinUCBPeriodicInterval=5`) added for the unified six-mode run; the default
  four-mode and opt-in-linucb behavior is unchanged. v1–v4, baseline,
  calibration and all diagnostics are frozen and byte-identical.
- **Reproduce:** `go run ./cmd/experiment -matrix -sudo -reps 10 -json
  -file-size 4194304 -modes fixed,concurrency-adaptive,chunk-size-adaptive,
  joint-adaptive,linucb,linucb-periodic`.
- **Configuration:** 5 scenarios × 6 modes × 10 reps = **300 runs**; file 4 MiB;
  chunk 32 KiB (min/max 32/128 KiB, step 32 KiB); concurrency 1 (1–4);
  thresholds 0.2/0.2; window 4; retries 3; timeout 30 s; alpha 0.2;
  `DefaultRewardConfig`; warm-up 5. `linucb` = standard (interval 0);
  `linucb-periodic` = interval 5. No seeds.
- **Correctness / network:** **300/300 valid**, 0 failed; network `applied` for
  latency/loss/combined, `none` otherwise; tc clean. SHA-256 not implemented.

**BEHAVIORAL FINDINGS** (descriptive):

- **Decision horizon (mean total):** fixed 128 everywhere; concurrency-adaptive
  ≈128; chunk-size-adaptive 35–80; joint-adaptive 49–121; linucb 37–127;
  linucb-periodic 38–123. Horizons differ because chunk changes alter range
  count — completion times must not be compared without this context.
- **Controller behavior:** concurrency-adaptive reaches concurrency 4 in
  server-throttle and 3 in latency (concurrency-adaptive is the only
  non-RL mode that robustly raises concurrency); chunk-size-adaptive drives
  chunk to 128 KiB; joint-adaptive does both partly. Standard LinUCB mostly
  keeps concurrency 1 and either holds chunk (baseline/throttle) or raises it
  (latency/combined). Periodic LinUCB additionally cycles concurrency/chunk via
  forced actions (`maxC=2`, oscillating chunk).
- **LinUCB standard vs periodic (post-warm-up):** standard explores 1.0–1.3
  unique arms (mostly no-ops); periodic explores **5.0** unique arms with many
  effective actions. Dominant arm is the **same** in baseline/throttle/latency/
  combined (e.g. `decrease-concurrency`, `decrease-chunk-size`,
  `increase-chunk-size`); in `loss` standard's `increase-concurrency` (121
  selections, 3 effective) differs from periodic (29 selections, 29 effective,
  final concurrency lower). So periodic changes coverage and controller state
  but not the dominant preference in most scenarios.

**PERFORMANCE MEASUREMENTS** (mean completion ms; wall-clock B/s from the
summary; n=10; **not ranked**):

| Scenario | fixed | conc-adaptive | chunk-adaptive | joint-adaptive | linucb | linucb-periodic |
|---|---|---|---|---|---|---|
| baseline | 1155.1 | 1156.3 | 1160.7 | 1147.6 | 1162.5 | 1113.6 |
| server-throttle | 1382.3 | 569.7 | 1343.3 | 647.6 | 1328.7 | 1215.7 |
| latency | 6340.1 | 2302.4 | 2559.6 | 2190.5 | 2635.0 | 2456.6 |
| loss (high var) | 1971.6 | 1198.2 | 1564.4 | 1263.7 | 1537.5 | 1648.5 |
| combined | 10154.9 | 5328.4 | 5928.8 | 5652.8 | 7186.8 | 6119.7 |

Retries/failures were 0 in all runs. Full median/SD/95% t-CI and throughput are
in the summary. Differences are descriptive only: modes use different decision
horizons and controller trajectories.

- **Chunk-size limitation (preserved):** the server models no meaningful
  chunk-size performance tradeoff, so observed chunk changes (including
  periodic cycling) are **not** evidence of chunk-size optimization.
- **Credit assignment (preserved):** one-step reward under concurrent in-flight
  requests is approximate.
- **Comparison to v3/v4 (behavioral):** v3 established the standard-LinUCB
  lock-in; v4 established that periodic exploration moves the posterior and, in
  `loss`, changes behaviour; v5 reproduces both and places them beside the
  deterministic controllers. These are configuration-identical LinUCB cells;
  cross-generation completion times are not treated as an improvement claim.
- **Limitations:** deterministic exploration; approximate credit; unmodelled
  chunk-size tradeoff; n=10; no seeds; descriptive statistics; no ranking.

### 22.16 aria2 External Baseline

**Status: completed.** A separate external-baseline measurement of `aria2c`
against the same controlled server/scenarios as benchmark v5. No internal code
was changed; frozen v1–v5 and all diagnostics are untouched. Descriptive only —
**no ranking, no superiority claim**.

- **Tool:** `aria2 version 1.37.0`. Harness: `cmd/aria2-benchmark/` (new; starts
  the existing `experiment/server` with the scenario's rate knobs, applies the
  existing tc/netem, runs `aria2c` once, verifies output byte-for-byte, and reads
  the server-observed HTTP range requests). **No** modification to the server,
  downloader, LinUCB, reward, features, or exploration.
- **Configuration (locked across all scenarios):** `aria2c --no-conf
  --file-allocation=none --allow-overwrite=true --auto-file-renaming=false
  --enable-color=false --summary-interval=1 --console-log-level=notice
  --max-connection-per-server=4 --split=4 --min-split-size=1048576 --max-tries=3
  --retry-wait=1 --timeout=30 --connect-timeout=30 --http-accept-gzip=false
  --check-certificate=false --disable-ipv6=true --conditional-get=false
  --interface=lo <url>`. **aria2 requires `--min-split-size >= 1 MiB`**, so its
  request granularity cannot be set to the internal 32 KiB chunk; the minimum is
  1 MiB.
- **Scenarios/reps:** the five v5 scenarios; 5 × 10 = **50 runs**.
- **Datasets:** `testdata/benchmark_aria2/matrix_aria2_4MiB_reps10.json`
  (+ `.txt`); request trace `testdata/diagnostics/aria2/request_trace.txt`;
  descriptive v5 comparison `testdata/diagnostics/aria2/comparison_v5.txt`.
- **Reproduce:** `go run ./cmd/aria2-benchmark -reps 10`.

**MEASURED FACTS**

- **Correctness:** **50/50 valid** — every run exit 0, output exists, size
  exactly 4194304 B, byte-for-byte equal to the served content. 0 failed
  downloads. Network `applied` for latency/loss/combined; tc clean.
- **Observed request behavior (every run):** 1 request **without a usable Range
  header** (initial full-body 200, aborted → 1 server-recorded failed request),
  then **3 range requests of exactly 1 MiB** each covering bytes 1–4 MiB;
  server-observed **max simultaneous in-flight requests = 4**; **0 retries**;
  range sizes **constant** (no within-run split-size adaptation observed).
- **Performance (mean / median / SD / 95 % t-CI ms; wall-clock B/s):**

  | Scenario | mean ms | median | SD | CI95 | wall-clock |
  |---|---|---|---|---|---|
  | baseline | 320.9 | 320.4 | 3.4 | 2.4 | 13.07 MB/s |
  | server-throttle | 2117.9 | 2118.3 | 9.8 | 7.0 | 1.98 MB/s |
  | latency | 853.2 | 852.4 | 2.8 | 2.0 | 4.92 MB/s |
  | loss | 362.4 | 322.7 | 82.8 | 59.2 | 12.04 MB/s |
  | combined | 1282.5 | 1275.9 | 15.2 | 10.8 | 3.27 MB/s |

  Retries/failures: 0 (the 1 server-recorded failed request per run is the
  aborted initial full-body request, not an aria2 failure).
- **Descriptive comparison to frozen v5 (mean completion ms; not a leaderboard):**

  | Scenario | fixed | conc-adapt | chunk-adapt | joint | linucb-std | linucb-per | aria2 |
  |---|---|---|---|---|---|---|---|
  | baseline | 1155.1 | 1156.3 | 1160.7 | 1147.6 | 1162.5 | 1113.6 | 320.9 |
  | server-throttle | 1382.3 | 569.7 | 1343.3 | 647.6 | 1328.7 | 1215.7 | 2117.9 |
  | latency | 6340.1 | 2302.4 | 2559.6 | 2190.5 | 2635.0 | 2456.6 | 853.2 |
  | loss | 1971.6 | 1198.2 | 1564.4 | 1263.7 | 1537.5 | 1648.5 | 362.4 |
  | combined | 10154.9 | 5328.4 | 5928.8 | 5652.8 | 7186.8 | 6119.7 | 1282.5 |

**INFERENCES (not directly measured, offered for interpretation)**

- The completion-time differences are confounded by **request granularity**:
  aria2 used 4 requests of ~1 MiB (4 concurrent connections), whereas the
  internal modes used 32 KiB chunks (up to 128 requests) with concurrency 1–4.
  Fewer, larger requests reduce per-request latency overhead (helps latency/
  loss/combined) and change exposure to the **request-count-based** rate switch
  (hurts server-throttle: aria2's 4 requests all land at the throttled 512 KiB/s
  rate, while the internal modes' later requests reach 4 MiB/s).
- aria2 exposed **no within-run adaptation**; the fixed 4-connection, 1 MiB-split
  behavior was constant. Whether it would adapt on other servers/ranges is not
  established here.

- **IDM status:** **not evaluated.** No IDM measurement path exists in this Linux
  repository/container (IDM is Windows-only and there are no IDM scripts). A
  separate Windows measurement would be required; no IDM numbers are claimed.
- **Limitations:** synthetic single-server test server; controlled
  throttle/latency/loss conditions not representative of all networks; aria2
  configuration differs from the internal model (1 MiB minimum split vs 32 KiB
  chunk; different retry/timeout semantics); request-timing/parallelism measured
  via a server-side in-flight wrapper and the server's range log (the aria2
  `CN:` console field is unreliable on sub-second runs); n=10; descriptive
  statistics only; no ranking.

### 22.17 Request-Granularity Sensitivity Study

**Status: completed.** A small, controlled experiment to estimate how much of
the aria2-vs-internal completion-time difference is plausibly explained by
**request granularity** alone. Measurement-only; no internal code, controllers,
or frozen datasets changed.

- **Motivation:** the aria2 baseline (§22.16) used 4 concurrent ~1 MiB requests
  while the internal modes used 32 KiB chunks (up to 128 requests) at
  concurrency 1–4, so completion-time differences confounded controller behavior
  with request size.
- **Matched internal configuration (experiment-only):** fixed policy (no
  adaptive controller), `chunk = 1048576 B` with `min = max = initial = step =
  1 MiB`, `concurrency = 4` with `min = max = 4`; 4 MiB file; the five v5
  scenarios; 10 reps = **50 runs**. Harness: new `cmd/granularity-study/` (uses
  the internal downloader + the existing controlled server and tc/netem; no
  production change).
- **Correctness:** **50/50 valid** — output byte-match, no duplicate/missing
  ranges, 0 failures; tc clean.
- **Observed request behavior (every run):** exactly **4 requests of 1 MiB**,
  **server-observed max in-flight = 4**, 0 non-range requests, range sizes
  constant. This matches aria2's observed granularity (4 × 1 MiB, max in-flight
  4) — except aria2 additionally makes one initial non-range request.
- **Performance (mean/median/SD/95 % t-CI ms; wall-clock; n=10):**

  | Scenario | mean | median | SD | CI95 | wall-clock |
  |---|---|---|---|---|---|
  | baseline | 285.5 | 284.6 | 2.4 | 1.7 | 14.69 MB/s |
  | server-throttle | 2052.9 | 2054.0 | 6.5 | 4.7 | 2.04 MB/s |
  | latency | 376.5 | 368.8 | 23.6 | 16.9 | 11.18 MB/s |
  | loss | 306.5 | 286.1 | 61.8 | 44.2 | 14.06 MB/s |
  | combined | 1232.3 | 1129.3 | 310.5 | 222.1 | 3.54 MB/s |

- **Comparison (mean completion ms; descriptive, no ranking):**

  | Scenario | aria2 | matched 1 MiB/4C | v5 fixed | matched / aria2 |
  |---|---|---|---|---|
  | baseline | 320.9 | 285.5 | 1155.1 | 0.89 |
  | server-throttle | 2117.9 | 2052.9 | 1382.3 | 0.97 |
  | latency | 853.2 | 376.5 | 6340.1 | 0.44 |
  | loss | 362.4 | 306.5 | 1971.6 | 0.85 |
  | combined | 1282.5 | 1232.3 | 10154.9 | 0.96 |

- **Server-throttle interaction (observed):** the server switches to 4 MiB/s
  after request **4**. aria2 and the matched 1 MiB/4C config issue exactly 4
  requests, so **all four run at the initial 512 KiB/s** and the switch never
  benefits them (~2.05–2.12 s). v5 fixed issues ~128 × 32 KiB requests, so
  requests 5–128 run at 4 MiB/s (~1.38 s). This is a request-count/rate-switch
  interaction, not an algorithmic property.
- **What the study can and cannot isolate:** holding granularity and concurrency
  fixed, the matched internal config reproduces aria2 closely in **4 of 5
  scenarios** (matched/aria2 = 0.85–0.97), supporting the interpretation that
  those differences are largely granularity/concurrency effects. In **latency**
  the matched config is ~2.3× faster than aria2 (0.44), indicating residual
  non-granularity aria2 overhead (e.g. its initial non-range request / connection
  handling) that this study does **not** explain. The study cannot attribute the
  remaining differences to a specific aria2 internal behavior.
- **Conclusion:** the experiment **substantially reduces** the granularity
  confound (it can now be mostly set aside in baseline/throttle/loss/combined) but
  **does not fully resolve it**; latency retains a material unexplained aria2
  overhead, and aria2's configuration still differs (initial non-range request,
  connection/retry semantics, 1 MiB minimum split).
- **Limitations:** synthetic server; a single fixed 1 MiB request size and fixed
  concurrency; no adaptive controller evaluated; n=10; descriptive statistics;
  remaining aria2 configuration differences; the matched internal config uses Go
  HTTP keep-alive while aria2 manages connections differently.

### 22.18 Wall-Clock Throttle Benchmark

**Status: completed.** A focused controlled experiment using the wall-clock rate
schedule (§14.1) to test whether the controller behavior previously observed
under the request-count throttle persists when the rate transition is independent
of request count and chunk granularity. Descriptive only — **no ranking, no
superiority/causality claim**. No controller, reward, feature, or frozen dataset
was changed.

- **Scenario `server-throttle-wallclock`:** 4 MiB file; server `512 KiB/s →
  4 MiB/s` after **1 second elapsed wall-clock** via `SetRateScheduleAt`
  (anchored to download start; request-count switch disabled). Essentially the
  v5 `server-throttle` scenario with only the trigger changed; no tc/netem.
- **Six modes × 10 reps = 60 runs**; all other config = v5 (chunk 32 KiB, min/max
  32/128 KiB, concurrency 1 (1–4), thresholds 0.2/0.2, window 4, retries 3,
  timeout 30 s, alpha 0.2, `DefaultRewardConfig`, warm-up 5, periodic interval 5).
- **Dataset:** `testdata/benchmark_v6/matrix_wallclock_throttle_6mode_reps10.json`
  (+ `.txt`); diagnostics `testdata/diagnostics/wallclock_throttle/`
  (`report.json`, `report.txt`, `request_trace.json`, `comparison_v5_old_throttle.txt`).
- **Reproduce:** `go run ./cmd/wallclock-throttle -reps 10` (new experiment-only
  command; `DefaultMatrixConfig` unchanged).
- **Correctness:** **60/60 valid**, 0 failures; no qdisc residue.
- **Rate-transition validation (server request log):** the first request paced at
  the subsequent rate begins at **≈1.040 s elapsed in every mode/run**
  (range 1.037–1.058 s), while the **request count differs materially** (Fixed/
  concurrency-adaptive/LinUCB ≈127–128 requests; chunk-size-adaptive 46;
  periodic LinUCB ≈69). So the transition is governed by elapsed time, not
  request count.
- **Performance (mean completion ms; descriptive, not ranked):**

  | Mode | mean | median | SD | 95 % CI | wall-clock |
  |---|---|---|---|---|---|
  | fixed | 2057.9 | 2058.0 | 2.3 | 1.7 | 2.04 MB/s |
  | concurrency-adaptive | 1324.5 | 1325.8 | 3.0 | 2.2 | 3.17 MB/s |
  | chunk-size-adaptive | 2029.0 | 2024.2 | 16.7 | 12.0 | 2.07 MB/s |
  | joint-adaptive | 1404.7 | 1403.9 | 5.7 | 4.0 | 2.99 MB/s |
  | linucb-standard | 2061.3 | 2061.3 | 4.5 | 3.2 | 2.03 MB/s |
  | linucb-periodic | 1323.4 | 1322.4 | 4.5 | 3.2 | 3.17 MB/s |

  Retries/failures: 0 in all 60 runs.
- **Controller behavior:** concurrency-adaptive reaches concurrency 4 (final
  4.00); joint-adaptive ~3; chunk-size-adaptive stays at concurrency 1 and
  varies chunk; standard LinUCB stays at concurrency 1.
- **LinUCB behavior:** **standard** still locks onto `decrease-concurrency`
  (unique arms 1.0; 1220/1220 no-ops; final concurrency 1). **periodic** is
  materially different here: unique arms 5.0, dominant **`increase-concurrency`**
  (380/641 selections; 49 effective), effective concurrency changes 69, chunk
  changes 67, **final concurrency 3.90** (max 4), and its completion time
  (1323.4 ms) is close to concurrency-adaptive (1324.5 ms).
- **Comparison with the request-count throttle (v5 `server-throttle`):**

  | Mode | old (after req 4) | new (after 1 s) |
  |---|---|---|
  | fixed | 1382.3 | 2057.9 |
  | concurrency-adaptive | 569.7 | 1324.5 |
  | chunk-size-adaptive | 1343.3 | 2029.0 |
  | joint-adaptive | 647.6 | 1404.7 |
  | linucb-standard | 1328.7 | 2061.3 |
  | linucb-periodic | 1215.7 | 1323.4 |

  Absolute times are not comparable across schedules; the behavioral finding is
  that the periodic LinUCB **preference changed** from `decrease-chunk-size`
  (old) to `increase-concurrency` (new), while standard LinUCB's lock-in and the
  deterministic controllers' qualitative behavior persisted. All modes are slower
  under the 1 s schedule because the first second is throttled to 512 KiB/s.
- **Confound assessment:** the experiment **substantially reduces the
  throttle/granularity confound** — the rate transition now occurs at a fixed
  elapsed time for all granularities (verified in the request log), so
  granularity no longer determines *when* the higher rate takes effect. It does
  **not** remove other differences (chunk size still changes request count and
  per-request overhead; aria2 was not re-run under this schedule).
- **Limitations:** synthetic server; a single fixed 1-second transition; n=10;
  descriptive statistics; approximate credit assignment under concurrency > 1;
  the chunk-size performance tradeoff remains unmodelled; aria2 was subsequently
  measured under the wall-clock schedule in §22.19, and a granularity-matched
  internal control in §22.20.

### 22.19 aria2 Wall-Clock Throttle Baseline

**Status: completed.** The external aria2 baseline was re-measured under the
experimental wall-clock throttle (§14.1, §22.18) using the identical frozen aria2
configuration. Measurement-only; no internal code, controller, or frozen dataset
changed.

- **aria2 version:** `aria2 version 1.37.0` (recorded again).
- **Scenario:** `server-throttle-wallclock` — 4 MiB file; server `512 KiB/s →
  4 MiB/s after 1 s elapsed` via `SetRateScheduleAt` anchored just before aria2
  starts; no tc/netem. 10 repetitions.
- **Configuration (locked, unchanged):** `--no-conf --file-allocation=none
  --allow-overwrite=true --auto-file-renaming=false --enable-color=false
  --summary-interval=1 --console-log-level=notice
  --max-connection-per-server=4 --split=4 --min-split-size=1048576 --max-tries=3
  --retry-wait=1 --timeout=30 --connect-timeout=30 --http-accept-gzip=false
  --check-certificate=false --disable-ipv6=true --conditional-get=false
  --interface=lo`.
- **Datasets:** `testdata/benchmark_aria2_v2/aria2_wallclock_reps10.json` (+ `.txt`);
  trace `testdata/diagnostics/aria2_wallclock/request_trace.json`; comparison
  `testdata/diagnostics/aria2_wallclock/report.{json,txt}`.
- **Reproduce:** `go run ./cmd/aria2-wallclock -reps 10`.
- **Correctness:** **10/10 valid** — exit 0, size 4194304 B, byte-for-byte match.
  No qdisc residue.
- **Request behavior (server-observed):** every run made **4 requests** (1 initial
  full-body probe with no usable Range header + **3 × exactly 1 MiB** range
  requests), **max in-flight = 4**, **0 retries**, **no within-run split-size
  adaptation**.
- **Wall-clock transition:** all four aria2 requests start within **~0.07 s** of
  the download start and each is paced for its whole **~2.06 s** duration at the
  rate selected at its start (512 KiB/s). Because the requests were already in
  flight and aria2 issued no further requests, **no aria2 request began at/after
  1 s** — the transition occurred mid-transfer without changing any request's
  rate. Mean completion ≈ **2129.7 ms** (median 2120.9, SD 35.7, 95 % CI 25.6;
  wall-clock ≈ 1.97 MB/s).
- **Comparison with internal v6 (mean completion ms; descriptive, not ranked):**
  aria2 2129.7; fixed 2057.9; concurrency-adaptive 1324.5; chunk-size-adaptive
  2029.0; joint-adaptive 1404.7; linucb-standard 2061.3; linucb-periodic 1323.4.
- **Interpretation (supported by the trace):**
  1. aria2 does **not** behave differently under the wall-clock schedule than it
     did under the request-count schedule: with 4 large in-flight requests it
     still completes the 4 MiB transfer at the initial 512 KiB/s effective rate,
     because it makes no new request after the 1 s transition.
  2. Removing the request-count trigger **does** remove the *trigger* confound
     (the transition is now time-based for every downloader), but it **does not
     equalize** the downloaders: the remaining difference is **request
     granularity/concurrency** — the internal modes keep issuing 32 KiB requests
     after 1 s and therefore actually use the 4 MiB/s rate, while aria2's
     four 1 MiB requests all predate the transition.
  3. Descriptively, aria2's completion (2129.7 ms) is close to the internal
     **fixed** (2057.9) and **linucb-standard** (2061.3) modes and well above
     **concurrency-adaptive** (1324.5) and **linucb-periodic** (1323.4); this is
     consistent with aria2 not using the post-transition rate on this scenario.
  4. It remains impossible to attribute the residual difference purely to
     "algorithm" vs "granularity": aria2's 1 MiB minimum split, 4-connection
     model, and connection management differ from the internal downloader.
- **Granularity context (unchanged from §22.17):** the request-granularity study
  is frozen and not re-run; its conclusion (request-count throttle created a
  granularity confound) stands. This experiment removes the *request-count*
  trigger, not granularity itself.
- **IDM:** remains **unevaluated** (Windows-only; no Linux measurement path). No
  IDM numbers are claimed.
- **Limitations:** synthetic server; fixed 1-second transition; aria2's 1 MiB
  minimum split and different connection management; n=10; descriptive
  statistics; no within-run aria2 adaptation observed; IDM unavailable.

### 22.20 Wall-Clock Granularity-Matched aria2 Control

**Status: completed.** A small controlled comparison measuring the internal
downloader (fixed policy, 1 MiB chunks, concurrency 4) under the same
wall-clock throttle as the aria2 baseline, to isolate request granularity and
parallelism from connection-management differences. Measurement-only; no
controller, production behavior, or frozen dataset changed.

- **Matched internal config:** 4 MiB; chunk = min = max = initial = step =
  1 MiB; concurrency = min = max = 4; fixed policy; retries 3; timeout 30 s.
  Wall-clock schedule `512 KiB/s → 4 MiB/s after 1 s` via `SetRateScheduleAt`;
  request-count switch disabled; no tc/netem; 10 reps.
- **Reproduce:** `go run ./cmd/granularity-study -scenario wallclock -reps 10`
  (the `-scenario` flag was added additively; the default still runs the five
  original scenarios, and the frozen `testdata/diagnostics/request_granularity/`
  artifacts are unchanged).
- **Dataset / diagnostics:** `testdata/diagnostics/request_granularity_wallclock/`
  (`report.json`, `report.txt`, `request_trace.json`, `comparison.txt`).
- **Correctness:** **10/10 valid** — output byte-match, no duplicate/missing
  ranges, 0 failures. No qdisc residue.
- **Observed request behavior (every run):** exactly **4 requests of 1 MiB**
  (no non-range probe), **max in-flight 4**, **0 retries**; **all 4 requests
  started before 1 s** (pre/post = 4/0), so **no request received the 4 MiB/s
  rate**. This is the same request shape as aria2 minus its initial non-range
  probe.
- **Performance:** mean **2055.7 ms**, median 2052.5, SD 17.3, 95 % CI 12.4;
  wall-clock ≈ 2.04 MB/s.
- **Primary controlled comparison (granularity + parallelism matched):**

  | Implementation | mean ms | median | requests | range size | max in-flight | non-range probe |
  |---|---|---|---|---|---|---|
  | aria2 (frozen, §22.19) | 2129.7 | 2120.9 | 4 | 1 MiB | 4 | 1 |
  | matched internal 1 MiB/4C | 2055.7 | 2052.5 | 4 | 1 MiB | 4 | 0 |

  Difference: aria2 − matched = **+74.0 ms (+3.6 %)**.
- **Context (v6 wall-clock, not granularity-matched; descriptive, not ranked):**
  fixed 2057.9; concurrency-adaptive 1324.5; chunk-size-adaptive 2029.0;
  joint-adaptive 1404.7; linucb-standard 2061.3; linucb-periodic 1323.4.
- **Interpretation (supported by the traces):** matching **request size and
  parallelism** reproduces the aria2 wall-clock behavior closely — both issue
  four 1 MiB requests that all start before the transition and therefore complete
  at the initial 512 KiB/s rate, with completion within ~4 %. The residual ~74 ms
  is consistent with aria2's extra initial non-range probe (one extra request /
  connection setup) and connection-management differences, but the trace cannot
  attribute it precisely. It does **not** support a claim that aria2 and the
  internal downloader are implementation-equivalent: the internal matched config
  makes 0 non-range probes vs aria2's 1, and the two HTTP clients differ.
- **Remaining confounds:** aria2's initial non-range probe; keep-alive/connection
  reuse and HTTP implementation differences; request-start timing within the
  first ~70 ms; aria2's 1 MiB minimum split (matched here, but a constraint its
  config imposes).
- **Limitations:** synthetic server; a single fixed 1-second transition; n=10;
  descriptive statistics; only the fixed/static granularity-matched config was
  used (no adaptive controller evaluated); IDM remains unevaluated.

### 22.21 Final Research Evidence Audit

**Status: completed (synthesis only; no new experiment).** A deterministic audit
of the frozen evidence chain, produced by `scripts/final_research_audit.py` into
`testdata/final_research_audit/` (`final_evidence_audit.{json,txt}`,
`final_results_tables.txt`, `final_claim_audit.txt`, `figures/`). All numbers were
re-derived from the raw frozen JSON; no summary/raw discrepancy was found.

- **Verified dataset integrity (from raw JSON):** v1 250/250, v2 250/250,
  v3 250/250, v5 300/300, v6 60/60 valid; v4 50 standard + 50 periodic; aria2
  50/50; aria2-wallclock 10/10; matched wall-clock 10/10. No invalid runs.
- **Experimental chain:** v1 (`ActionNone` lock-in) → v2 (warm-up; ~2 learned
  decisions) → v3 (32–122 learned decisions; standard lock-in persists) →
  lock-in diagnosis (4582/4582 replay; posterior dominance, frozen unselected
  arms, capped bonus, constant `bytes_transferred`) → periodic-exploration
  diagnostic (coverage 1→5; forced arms updated; preference unchanged) → live
  periodic benchmark (effective actions; posterior movement; scenario-dependent)
  → v5 (six-mode campaign) → wall-clock infrastructure → v6 (time-based
  transition; periodic sustains concurrency ~3.9) → aria2 old throttle → matched
  granularity study → aria2 wall-clock → matched wall-clock control.
- **Supported conclusions (directly demonstrated):** periodic exploration breaks
  frozen-arm starvation; forced/periodic actions cause posterior updates and
  effective controller changes (scenario-dependent).
- **Supported but limited:** request granularity/parallelism explains a
  substantial portion of the original aria2 difference (~74 ms residual
  unexplained); the wall-clock schedule removes the request-count trigger for all
  granularities without equalizing downloaders.
- **Not supported:** broad novelty of RL joint adaptation; optimal/converged
  LinUCB; consistent LinUCB performance improvement; successful chunk-size
  optimization; internal superiority over aria2; aria2 inferiority on consumer
  networks; generalization to real networks; fairness; "RL-based adaptation"
  (adaptation was produced by **deterministic** periodic exploration); causal
  one-step action effectiveness.
- **LinUCB final interpretation:** the safest accurate statement is *"adaptive RL
  controller **infrastructure** demonstrated, with adaptation observed only under
  deterministic periodic exploration in the tested scenarios"* — **not**
  "learned optimal policy", "convergence", or "generalization".
- **Performance vs behavior:** behavioral findings (coverage, controller
  changes, posterior movement, scenario dependence) are distinct from descriptive
  performance measurements; completion-time differences are not used as proof of
  superiority and are confounded by granularity, horizon, trajectory, and the
  synthetic server.
- **aria2 conclusion:** request granularity and request scheduling explain a
  substantial portion of the observed aria2-vs-internal differences; it is **not**
  established *why* aria2 is slower in any mechanistic sense, and no
  consumer-network or IDM claim is made.
- **Recommended next phase:** the evidence chain is sufficient to proceed to
  **paper/report writing** (and thesis/presentation preparation) for the scope
  "controlled, synthetic-server evaluation of deterministic vs contextual-bandit
  concurrency/chunk controllers, plus a granularity-controlled external
  comparison." It is **not** sufficient to claim real-network generalization,
  chunk-size optimization, fairness, or an external win.

### 22.22 Sequential RL / Double DQN (architecture + sanity only)

**Status: RL controller/training architecture implemented and sanity-validated;
no performance claim; no benchmark.** This begins the sequential-RL phase. LinUCB
is **retained unchanged** as a contextual-bandit baseline (§20.6, §22.21); it is
not deleted or retrained.

- **Why sequential RL:** the §22.12 diagnosis showed standard disjoint LinUCB
  locks onto one arm, unselected arms stay frozen, the UCB bonus cannot overcome
  the first learned arm, and one-step credit is inadequate when actions affect
  future requests. Periodic exploration produced real adaptation but is
  deterministic, not learned. A sequential (episodic) controller addresses
  temporal credit directly.
- **Algorithm:** Double DQN (online + target MLP, uniform replay, epsilon-greedy,
  Huber loss, gradient clipping, periodic target sync, terminal episodes,
  deterministic greedy inference). No PPO/SAC/actor-critic/recurrent/prioritized/
  distributional/multi-agent. Training is PyTorch (CPU-only); inference is Go.
- **Package split:** Go contract + inference in `internal/rl/dqn/` (`state.go`,
  `model.go`, `env.go`, `sim.go`, `policy.go`); Python training in `python/rl/`
  (`state.py`, `sim_env.py`, `dqn.py`, `model.py`), CLI `python/train_dqn.py`.
- **State (13 features, current + temporal; NOT the LinUCB state):**
  `concurrency_norm`, `chunk_size_norm`, `remaining_bytes_frac`,
  `aggregate_throughput_norm`, `aggregate_throughput_ewma_norm`,
  `aggregate_throughput_slope`, `aggregate_throughput_volatility`,
  `request_duration_norm`, `request_duration_slope`, `recent_failure_rate`,
  `recent_retry_rate`, `since_concurrency_change_norm`,
  `since_chunk_change_norm`. There is **no saturated cumulative-bytes feature**;
  `remaining_bytes_frac` stays informative through the episode. `StateSpec`
  (`version=1`) carries explicit bounds/constants; a model records the spec and
  feature names and inference rejects a mismatch.
- **Reward:** `r_t = -(elapsed wall-clock seconds between decision t and
  observation t+1)`, `gamma = 1.0`, so the undiscounted episode return
  approximates negative completion time. No throughput/retry/action weights;
  failures/retries raise elapsed time naturally. Truncation: a configured
  `max_episode_steps` bounds episodes and is reported as `truncated`.
- **Transition:** `(s_t, a_t, r_t, s_(t+1), done)` with genuine sequential credit
  (no "next-request reward").
- **Action semantics:** identical to the existing `rl.ActionIndex` order (0..4);
  Set actions remain unsupported; only future allocations/controller targets
  change; requested/effective/no-op are recorded.
- **Training environment:** a fast analytical simulator (`python/rl/sim_env.py`)
  modeling file progress, concurrency, chunk size, per-request service time
  (latency + chunk/rate), rate changes, latency, loss/retries; randomized
  episodes over ranges of bandwidth, latency, loss, rate-change point, initial
  concurrency, and file size. **Training conditions are explicitly NOT the final
  evaluation conditions.** Simulator assumptions and a calibration sanity check
  are recorded in `testdata/diagnostics/dqn/simulator_spec.json` (sim vs frozen
  server: 32 KiB@512 KiB/s 0.0625 vs ~0.064 s; 32 KiB@4 MiB/s 0.0078 vs ~0.008 s;
  1 MiB@512 KiB/s 2.00 vs ~2.05 s).
- **Model export/inference:** deterministic portable JSON (`model_version`,
  state spec, feature names, action mapping, layer weights/biases, training
  metadata). Go `dqn.LoadModel` / `ParseModel` validate structure, dimensions,
  finite weights, and feature order; `Forward`/`SelectAction` are deterministic
  greedy (epsilon = 0). No training code is in the inference path.
- **Sanity training (DEVELOPMENT / SANITY MODEL, `testdata/diagnostics/dqn/`):**
  seed 0, 300 episodes, 3927 steps; all 300 episodes terminal (0 truncated);
  `loss_finite=true`, final loss 0.00536, mean last-100 loss 0.00810;
  `q_finite=true`, Q range [-0.947, 0.276]; final epsilon 0.254; mean return
  -1.848; model SHA-256 `db8602b4b01478884d30a481077db3f91ee884fd07182aea2b10c712fca25da9`.
  **Same seed reproduces the identical hash; a different seed produces a
  different model** (`0bef3710…`). Go loads the artifact and infers
  deterministically.
- **Dependency note:** `torch==2.9.0+cpu` (CPU-only, **no nvidia/CUDA packages**)
  and `numpy==2.5.3` (binary wheel; numpy 2.2.6 had no Python-3.14 wheel and
  would have been built from source). Training-only; production inference is Go.
- **Limitations:** the simulator-to-live gap is unquantified (the simulator is
  not the real downloader); no held-out evaluation, no aria2 comparison, and **no
  performance claim**; the real benchmark still does not model a chunk-size
  tradeoff; no throttle detector; deterministic-replay/credit differences from
  the real downloader remain. The stored model is a **sanity** model, not a final
  policy.
- **Next phase (not started):** train on the randomized-condition distribution,
  then define a held-out evaluation and a head-to-head aria2 comparison under
  predefined unstable conditions. Those are separate, explicitly-scoped tasks.

### 22.23 DQN Training and Held-Out Validation

**Status: completed (simulator training + held-out simulator validation + a
25-run live transfer check). FINAL DQN CANDIDATE v1 frozen. No performance
claim; no aria2 comparison.** This distinguishes three models: the **sanity
model** (`testdata/diagnostics/dqn/`, seed 0/300 ep), the **seed-selection
candidates** (5 seeds), and the **FINAL DQN CANDIDATE v1**
(`testdata/diagnostics/dqn_validation/final_dqn_candidate.json`).

- **Train / validation / test separation:** TRAIN = randomized simulator episodes
  (`rand_*` ranges: bandwidth 512 KiB–8 MiB/s, secondary 256 KiB–4 MiB/s, latency
  0–80 ms, loss 0–4 %, switch 0–12 requests, file 256–1536 KiB). VALIDATION = 3
  held-out condition dicts (unseen latency 90 ms / loss 4.5 % / mixed) used only
  for checkpoint selection. TEST = 8 fully held-out conditions (unseen
  bandwidths 384 KiB/s and 12 MiB/s, latency 140 ms, loss 7 %, latency+loss,
  throttle-up/down, combined) never used for training or selection.
- **Seeds:** 0,1,2,3,4; 800 episodes each (120-step max; ~64 s total). All 5 seeds
  completed training with finite loss and finite Q-values (recorded in
  `seed_results.json`).
- **Selection criterion:** lowest **mean validation completion time**, tie-break
  lowest seed (deterministic; the test set is never consulted). **Seed 4**
  selected (val mean 0.8228 s).
- **Held-out simulator test (mean completion s, 4 episodes/condition; not
  ranked):** DQN 1.440; fixed 2.029; concurrency-adaptive 2.029; joint-adaptive
  2.029. The scripted simulator baselines tie because the analytical
  per-wave throughput is constant, so their relative-threshold rules never fire
  — i.e. the simulator does not reproduce the real controller behavior of those
  baselines, so this is a weak baseline comparison. Per-condition, DQN is better
  under latency (2.57 vs 3.55), latency+loss (1.54 vs 3.26) and combined (2.34 vs
  4.24), equal under high-bandwidth/loss-only, and **worse** under
  low-bandwidth (2.81 vs 2.67). So the learned advantage is real but mixed.
- **Ablations (mean completion s):** learned policy 1.440; random policy 1.519;
  shuffled actions 1.659; corrupted temporal state 1.500. Degradation occurs as
  expected (learned < random/shuffled/corrupted), though the margin is modest.
- **FINAL DQN CANDIDATE v1:** SHA-256
  `83427c5345d1863a62902e176398c360c4299b530931a31440563bede4a913db`; state spec
  version 1; actions `none, increase-concurrency, decrease-concurrency,
  increase-chunk-size, decrease-chunk-size`. Same seed reproduces this hash
  exactly (verified twice); different seeds produce different models.
- **Live transfer (25 runs: 5 scenarios × 5 reps, real controlled server, 4 MiB,
  locked config):** **25/25 valid**, 0 qdisc residue. Behavior is
  scenario-dependent and sensible: baseline stays at concurrency 1 (chunk
  settles at 64 KiB); **latency** raises concurrency (final mean 3.6) and chunk
  (~85 KiB); **combined** drives chunk to the 128 KiB max and some concurrency;
  **loss** stays at concurrency 1; **server-throttle-wallclock** reaches final
  concurrency 1.4. The policy avoids out-of-bounds actions.
- **Sim-to-real:** transferred — raising concurrency under latency, increasing
  chunk under latency/combined, staying at low concurrency where extra
  concurrency did not help, and avoiding bound violations. Did not transfer
  cleanly — under `server-throttle-wallclock` the live policy frequently
  decreased chunk without benefit (the simulator trained on request-count
  switching, not the wall-clock schedule), and simulator magnitudes differ from
  the live server. The policy remains **behaviorally sensible**, not
  demonstrably optimal.
- **Reproducibility:** `train_config.json` records seeds, ranges, distributions,
  Python 3.14.7, torch 2.9.0+cpu, numpy 2.5.3, architecture, hyperparameters,
  and the selection criterion.
- **Artifacts:** `testdata/diagnostics/dqn_validation/{train_config.json,
  seed_results.json, validation_results.json, heldout_test_results.json,
  ablations.json, live_transfer_results.json, final_dqn_candidate.json,
  final_dqn_candidate.sha256, sim_to_real.json, report.json, report.txt}`. Code:
  `python/train_validate.py`; Go live path `internal/rl/dqn/live.go`,
  `cmd/dqn-transfer/`.
- **Limitations:** the simulator does not reproduce the deterministic
  controllers (weak baseline comparison) and does not model the wall-clock
  schedule; the held-out advantage is mixed (worse under low bandwidth); live
  n=5 only; no throttle classifier; the real benchmark still models no
  chunk-size tradeoff; **no aria2 comparison and no performance claim yet** —
  the candidate is frozen for a future, explicitly-scoped head-to-head.

### 22.24 DQN Environment Correction and Candidate v2

**Status: completed.** Corrects the v1 training environment, retrains the DQN,
re-validates, and freezes **FINAL DQN CANDIDATE v2**. **No performance claim; no
aria2 comparison.** **Candidate v1 is retained as a historical development
model** (`testdata/diagnostics/dqn_validation/`, unchanged).

- **Why v1 was not suitable:** (1) the v1 simulator's per-wave throughput was
  constant, so fixed/concurrency/joint baselines all tied and the chunk tradeoff
  was absent; (2) v1 trained on request-count switching and transferred imperfectly
  to the real **wall-clock** throttle (frequent unhelpful chunk decreases under
  `server-throttle-wallclock`).
- **Corrected simulator (`python/rl/sim_env.py`):**
  - **Shared bandwidth:** per-request rate = `min(current_link_rate / concurrency,
    per_connection_rate)`; a wave's wall-clock is the slowest concurrent request.
    Concurrency now changes aggregate throughput (verified: link 8 MiB/cap 1 MiB
    gives C=1→3.41 s, C=2→1.70 s, C=4→0.85 s), and the shared link makes higher
    concurrency stop helping (diminishing returns).
  - **Request overhead:** `latency + overhead per request`; larger chunks amortize
    it (verified: 32 KiB total 0.477 s vs 128 KiB 0.213 s on a fixed 512 KiB file).
    No arbitrary chunk reward bonus; the tradeoff comes from the mechanics.
  - **Two throttle models:** `none`, `request-count`, and **`wall-clock`** on a
    continuous simulated clock; the wall-clock rate depends on elapsed time, not
    request count (verified: same 1.0 s transition time but different request
    indices for 32 KiB vs 1 MiB chunks).
  - Explicitly documented distinction: the **training** simulator adds
    chunk-size mechanics the **real** evaluation server deliberately does not
    model.
- **Baseline fidelity:** the scripted baselines now react — on the held-out test,
  concurrency-adaptive makes max-concurrency changes (mean concChg 0.19), chunk-size-adaptive
  makes chunk changes (0.22), joint-adaptive makes both (0.14/0.14). The v1
  "all baselines tie" failure is gone; held-out means differ (fixed 1.764,
  concurrency 1.706, chunk 1.755, joint 1.710 s).
- **Training distribution (recorded in `train_config_v2.json`):** link 512 KiB–8 MiB/s,
  per-connection 512 KiB–8 MiB/s, secondary rate 256 KiB–4 MiB/s, latency 0–80 ms,
  loss 0–4 %, file 256 KiB–4 MiB, switch kind ∈ {none, request-count, wall-clock}
  (requests 0–12, seconds 0.2–2.0), random initial concurrency/chunk.
- **Validation / test separation:** VALIDATION = 4 distinct conditions including
  wall-clock schedules, used only for checkpoint selection (min mean validation
  completion time, tie-break lowest seed). TEST = 9 fully held-out conditions
  (unseen bandwidths/latency/loss, both throttle kinds, unseen combinations),
  never used for training or selection.
- **Multi-seed (0–4, 1200 episodes each):** all converged with finite loss/Q;
  validation means 1.048/1.042/1.117/1.123/1.083 s; **seed 1 selected**.
- **Held-out simulator (mean completion s; not ranked):** dqn **1.233**; fixed
  1.764; concurrency-adaptive 1.706; chunk-size-adaptive 1.755; joint-adaptive
  1.710. DQN leads descriptively and is much better under latency (0.76 vs 2.01),
  latency+loss (0.98 vs 2.19) and combined (1.35 vs 2.75); it is **slightly worse**
  on low-bandwidth (2.84 vs 2.72).
- **Ablations (mean completion s):** learned **1.233** < random 1.468 < shuffled
  1.581; corrupted-state 1.254 is close to learned — the learned advantage is
  real but modest, and largely explained by temporal concurrency decisions.
- **FINAL DQN CANDIDATE v2:** SHA-256
  `59cbc5e6e18f428358b6233ba0e350c35668f2ebad3ba4d23e5e4c85de9b3a0e`; state spec
  v1 (unchanged 13 features); actions unchanged; seed 1. Same seed reproduces the
  hash.
- **Live transfer v2 (25 runs, real controlled server, locked 4 MiB config):**
  **25/25 valid**, tc clean. Compared with v1: under `server-throttle-wallclock`
  unhelpful decrease-chunk selections fell **260 → 46** and effective concurrency
  changes rose 18 → 144 (chunk ~105 KiB); under `combined` v2 holds **concurrency
  4** (v1 1.6); under `latency` v2 favors max chunk (124 KiB) with 183 effective
  concurrency changes; under `loss` v2 uses concurrency 2. Remaining mismatch:
  under `baseline` v2 issues many decrease-chunk selections that are **no-ops at
  the minimum bound** (169 no-op / 5 effective).
- **Sim-to-real (v2):** improved — reduced unhelpful chunk decreases under the
  wall-clock throttle, stronger concurrency use under combined/latency, no bound
  violations; remains mismatched — simulator magnitudes differ, no throttle
  classifier, and the baseline no-op spamming. Behavior is **behaviorally
  sensible, not demonstrably optimal**.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v2/{simulator_spec_v2.json,
  train_config_v2.json, seed_results_v2.json, validation_results_v2.json,
  heldout_test_results_v2.json, baseline_validation_v2.json, ablations_v2.json,
  live_transfer_results_v2.json, final_dqn_candidate_v2.json,
  final_dqn_candidate_v2.sha256, sim_to_real_v2.json, report.json, report.txt}`.
  Code: `python/train_validate_v2.py`, corrected `python/rl/sim_env.py`.
- **Limitations:** the simulator is still an abstraction (not the real server);
  the real benchmark still models no chunk-size tradeoff; live observability and
  credit are approximate; live n=5; **no aria2 comparison and no performance
  claim** — v2 is frozen for a future, explicitly-scoped head-to-head.

### 22.25 aria2 Baseline Calibration

**Status: completed.** Selects and freezes ONE global aria2 configuration using
a separate VALIDATION set, so the future head-to-head uses a fairly-chosen
baseline. Calibration only: the DQN/Candidate v2, LinUCB, downloader, server,
and all frozen datasets are untouched; **no final-test conditions were created or
inspected**.

- **aria2 version:** `aria2 version 1.37.0` (unchanged).
- **Candidate configurations (global, identical otherwise):** A = 1 connection /
  split 1; B = 2 / 2; C = 4 / 4. All share the same flags (`--no-conf`,
  `--file-allocation=none`, `--allow-overwrite`, `--min-split-size=1048576`,
  `--max-tries=3`, `--retry-wait=1`, `--timeout=30`, `--connect-timeout=30`,
  `--http-accept-gzip=false`, `--check-certificate=false`, `--disable-ipv6`,
  `--conditional-get=false`, `--interface=lo`).
- **Validation profiles (wall-clock transitions only; no request-count
  switching):** V1 512 KiB→4 MiB/s @0.75 s; V2 1 MiB→4 MiB @1.25 s; V3 4 MiB +
  20 ms + 1 % loss; V4 2 MiB + 40 ms + 1 % loss; V5 768 KiB→3 MiB @1.5 s + 20 ms
  + 1 % loss. All 4 MiB, controlled server.
- **Repetitions:** 3 configs × 5 profiles × 5 reps = **75 runs**.
- **Selection criterion (documented):** lowest **pooled mean completion time**
  across all validation profiles; tie-break lower pooled median, then lower
  pooled retries.
- **Correctness:** **75/75 valid** (exit 0, size 4194304 B, byte-for-byte match);
  tc clean.
- **Result:** **C selected** — pooled mean **1496.3 ms**, median 1602.5 ms,
  SD 426.7, wall-clock ~3.06 MB/s. B: mean 2697.6 ms, median 2830.9. A: mean
  4889.2 ms, median 4213.5. C is faster in **every** profile; no tie-break was
  needed. Retries/failures: 0 for all configs.
- **Request behavior (server-observed):** A issues 1 request (plus 1 initial
  non-range probe), max in-flight 1; B issues 2 (~2 MiB ranges), max in-flight 2;
  C issues 4 (1 MiB ranges), max in-flight 4. In **every** config and every
  wall-clock profile all requests began **before** the transition (post-transition
  requests = 0), so none exploits the subsequent higher rate; C's advantage is
  parallel transfer, not rate-transition capture.
- **FINAL ARIA2 BASELINE CONFIG v1 (frozen):**
  `aria2c … --max-connection-per-server=4 --split=4 --min-split-size=1048576 …`
  (that is, configuration C, unchanged from the earlier frozen aria2 runs).
  Metadata hash `6b8cf33eb896ecf7c13de71434250ad9611cdd501043075212fb6bd0b1ffcf72`
  (`selected_baseline.json` / `.sha256`). **This configuration is now FROZEN for
  the final head-to-head evaluation and must not change.**
- **Artifacts:** `testdata/diagnostics/aria2_calibration/{validation_config.json,
  validation_results.json, validation_results.txt, request_trace.json,
  selected_baseline.json, selected_baseline.sha256, report.json, report.txt}`.
  Code: `cmd/aria2-calibration/`.
- **Limitations:** synthetic validation server; aria2's 1 MiB minimum split;
  connection-management differences; n=5 per calibration cell; descriptive
  calibration statistics; the validation set is not the final test set.

**Update — FINAL ARIA2 BASELINE CONFIG v2 (16 MiB evaluation scale):**
`testdata/diagnostics/aria2_calibration_v2/selected_baseline_v2.json` (metadata
SHA-256 `b72498176a48f889cd2d1dbc9d9ef6b9afc668f2cf0d902e44d51dee0dedca02`)
records the selection of **candidate F** (`--max-connection-per-server=16
--split=16 --min-split-size=1048576`) on pooled validation means A 13136.7 ms,
B 6769.8, C 3502.8, D 3540.4, E 3463.2, **F 1101.9 ms** (25 runs per config, 0
retries, 0 failures; criterion lowest pooled mean, tie-break median then
retries). Config v1 (§22.25, 4 connections) is retained as history; **v2 is the
frozen baseline for future comparisons.** No candidate was modified.

### 22.26 Final DQN vs aria2 Evaluation

**Status: completed. The pre-specified outperformance claim was NOT supported.
Reported honestly; the experiment and conditions were not altered to rescue it.**

- **Frozen systems:** DQN **FINAL CANDIDATE v2** SHA-256
  `59cbc5e6e18f428358b6233ba0e350c35668f2ebad3ba4d23e5e4c85de9b3a0e`; aria2
  **FINAL BASELINE CONFIG v1** metadata SHA-256
  `6b8cf33eb896ecf7c13de71434250ad9611cdd501043075212fb6bd0b1ffcf72`
  (`aria2c 1.37.0`, `--max-connection-per-server=4 --split=4`). Both hashes
  verified before the run; neither was modified.
- **Test matrix (locked + hashed before any run; sha256
  `01b85677…` canonical / `335ab4fd…` file-bytes):** 8 wall-clock unstable
  profiles U1–U8 (U1 768 KiB→3 MiB @0.8 s; U2 1.5→6 MiB @1.3 s; U3 4 MiB+15 ms+
  0.5 %; U4 2 MiB+35 ms+1.5 %; U5 3 MiB+60 ms+0.75 %; U6 1.25→5 MiB @1.1 s+25 ms;
  U7 2→6 MiB @1.4 s+1 %; U8 1→4 MiB @0.9 s+40 ms+1.25 %). No profile exactly
  duplicates calibration V1–V5, v6, or DQN validation conditions (checked).
- **Design:** 30 matched pairs per condition = **240 pairs / 480 downloads**;
  randomized execution order per pair (deterministic seed); fresh server,
  output, controller, and schedule anchor per run; wall-clock transitions only
  (no request-count switching). 4 MiB file.
- **Correctness:** **480/480 valid**, all 240 pair IDs accounted for, no
  duplicates. tc clean at end.
- **Harness incident (documented):** the first campaign omitted per-run netem
  cleanup, leaving qdisc residue that could contaminate later impaired runs. That
  run was **archived and discarded** as `*_CONTAMINATED*`, the harness was fixed
  (per-run `Clear`), and the full campaign was re-run with the **same locked
  matrix**. Only the clean re-run is analysed.
- **Aggregate paired result (primary unit = matched pair):** DQN mean
  **2276.8 ms**, aria2 mean **1309.1 ms**; geometric ratio **1.757**
  (95 % CI [1.669, 1.850]); relative change **/−75.7 % for DQN** (i.e. DQN is
  75.7 % slower); one-sided paired permutation p = 1.0; DQN faster in
  **31/240 pairs (12.9 %)**, aria2 faster in 209, 0 ties.
- **Per-condition (DQN vs aria2 mean ms, relative reduction, Holm p):**
  U1 1257 vs 1439 (**+13.0 %**, p_holm 4e-5); U2 1098 vs 770 (−42.3 %); U3 1819
  vs 626 (−193.5 %); U4 3261 vs 1675 (−95.0 %); U5 4102 vs 2665 (−54.3 %); U6
  2143 vs 1108 (−91.4 %); U7 1295 vs 626 (−109.8 %); U8 3238 vs 1563 (−107.0 %).
  DQN was faster in **1 of 8** conditions (U1); all others were substantially
  slower (all Holm p = 1 in the DQN-faster direction).
- **Pre-specified claim criteria:** all four **failed** — (1) ratio < 1: no;
  (2) CI excludes zero DQN-faster: no; (3) paired test supports DQN-faster: no;
  (4) point estimate DQN-faster in a clear majority: no. **Claim not supported.**
- **Behavioral explanation:** aria2 used **4 concurrent ~1 MiB requests**
  (4 requests total, max in-flight 4) in every condition. The DQN made **57–87
  decisions** with 32 KiB up to 128 KiB chunks and final concurrency 1.6–3.6
  under latency/loss. Under latency/loss and combined instability, the DQN's far
  higher per-decision request count and finer granularity cost many round-trips,
  which dominates; its concurrency adaptation only wins in the throttle-only U1
  (13 % faster). This is consistent with the earlier transfer observations and
  with the real server not modelling a chunk-size tradeoff while aria2's 1 MiB
  granularity is efficient here.
- **FINAL CLAIM (unsupported):** *"The final evaluation does not support an
  outperformance claim for the proposed DQN-based adaptive downloader against
  aria2c under the tested controlled unstable-network conditions."*
- **Artifacts:** `testdata/final_evaluation/{final_test_matrix.json,
  final_test_matrix.sha256, execution_schedule.json, raw_results.json,
  summary.txt, statistical_analysis.json, claim_assessment.json,
  request_traces.json, report.txt, run.log, fig1–fig5.svg,
  raw_results_CONTAMINATED_missing_netem_cleanup.json,
  request_traces_CONTAMINATED.json, run_CONTAMINATED.log}`. Code:
  `cmd/final-evaluation/`, `python/analyze_final_evaluation.py`.
- **Limitations:** synthetic controlled server; 4 MiB file; n=30 pairs per
  condition; aria2's 1 MiB minimum split; DQN trained in an abstract simulator
  (sim-to-real gap); no real ISP/Internet deployment; IDM unavailable; the real
  server models no chunk-size tradeoff; no fairness objective. The result holds
  only for the evaluated controlled conditions.
- **IDM:** not evaluated (Windows-only; no Linux measurement path); excluded from
  the analysis.

