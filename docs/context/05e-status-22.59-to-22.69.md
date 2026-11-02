### 22.59 Pre-formalization harness: Mahimahi replay + EOF handling + watchdog/cleanup

**Status: `MAHIMAHI_HARNESS_READY` for the infrastructure/smoke harness.
Synthetic traces only; NOT formal Stage 0 and NOT trace-time anchored. No
epoch/pilot/oracle/tau/RL/adaptive/aria2 work was performed. §1–§5 remain
READY. No frozen artifacts touched.**

- **Entry points:** `cmd/mm-replay` (CLI) over `internal/experiment/replay`
  (harness) and `internal/experiment/server` (logical workload).
  `replay.Run` starts the controlled server, launches mm-link, ends at trace
  EOF, tears the tree down, and returns an explicit outcome.
  `replay.RunDownloaderChild` is the in-namespace downloader entry (child
  mode); the CLI re-executes itself with `-child`.
- **Process topology:** parent harness (logical HTTP server on the host,
  `0.0.0.0:PORT`) → `mm-link UP DOWN --once -- <child argv>` (setuid; creates
  the namespace) → downloader child inside the namespace fetching
  `http://$MAHIMAHI_BASE:PORT/`. Verified: `MAHIMAHI_BASE=10.0.0.1`, child
  ingress `10.0.0.2 peer 10.0.0.1/32`, host server reachable. HTTP/1.1 only,
  contiguous `bytes=START-END`, fixed C, 1 MiB default chunk (not frozen).
- **Corrected `--once` EOF behavior (ground truth 2026-09-26; supersedes the
  §22.54 probe note):** with `--once`, mm-link **exits by itself at trace
  EOF** (rc=0) — a 2 s trace with a 30 s sleeping child returned at 2.11 s. At
  EOF it signals its child and waits for it to actually die; a child that
  **defers the signal** makes mm-link wait (verified; it had to be killed).
  Post-EOF child stdout is lost. Therefore trace EOF = mm-link exit, and the
  independent watchdog is mandatory.
- **Measurement channel:** the child writes its JSON `ChildSummary` to
  `SummaryPath` immediately, then every 25 ms, and again on graceful
  cancellation; the parent reads it after teardown. This survives abrupt
  teardown (stdout after EOF is unreliable). The summary carries request
  counts, incremental read-time byte totals from the §4 `fetcher.ByteMeter`,
  event count, distinct connection identities, output size, and
  canceled/exhausted/final flags.
- **Watchdog:** `replay.Config.MaxRuntime` is a mandatory independent hard
  timer covering startup, replay, and teardown. On expiry: signal the
  downloader child (SIGTERM; it cancels and flushes), signal mm-link, then
  SIGKILL the process group and tracked descendants; surviving PIDs yield
  `CLEANUP_FAILED`. A deferring child cannot hang the harness (tested:
  watchdog fired at ~0.8 s).
- **Cleanup:** descendants are tracked via `/proc` (non-zombie), signaled
  first, then group-killed only if needed; the harness never kills processes
  it did not create. Smoke tests compare `ip -o link`, `ip -o addr`,
  `tc qdisc show`, and `ip netns list` before/after and assert no `mm-link`
  remains; all clean.
- **Logical workload integration:** the child downloads from
  `server.NewLogical` (bounded memory); tests use 128–256 MiB workloads and
  prove the object is not exhausted. Synthetic traces live under `t.TempDir()`
  (outside the repo); the real cellular corpus is untouched.
- **Exit/result model (explicit, not collapsed):** `SUCCESS_TRACE_REACHED_EOF`,
  `MM_LINK_START_FAILED`, `DOWNLOADER_START_FAILED`, `DOWNLOADER_EXITED`,
  `WATCHDOG_TIMEOUT`, `WORKLOAD_EXHAUSTED`, `CLEANUP_FAILED`,
  `INVALID_CONFIGURATION`, `DOWNLOADER_SUMMARY_MISSING`, `CANCELED`;
  `replay.ExitCode` maps them to distinct process codes
  (0,2,3,4,8,5,6,7,9,10).
- **Tests** (`internal/experiment/replay/replay_test.go`): trace parsing incl.
  zero-duration rejection; invalid configuration; mm-link start failure;
  downloader start failure; watchdog timeout with no leftovers; local child
  download against the logical server (byte-exact output); synthetic
  C=1/4/16 mm-link smoke (EOF reached, incremental bytes > 0, workload not
  exhausted, ≤ C distinct connections, host network unchanged); workload
  exhaustion mapping; distinct exit codes. Results: package passes `-count=3`
  and `-race`; full `go test ./...`, `go test -race ./...`, `gofmt -l .`,
  `go vet ./...`, `go build ./...` clean. CLI end-to-end smoke:
  `go run ./cmd/mm-replay -up T -down T -c 4 -workload-size 67108864 -chunk
  262144 -output OUT -summary SUM` on a 299 ms synthetic trace returned
  `SUCCESS_TRACE_REACHED_EOF` with 413 412 bytes / 315 events / 4 connections,
  a sparse 64 MiB output, and no netns/mm-link residue.
- **Remaining (not started):** the trace-time anchor + calibration were
  subsequently implemented in §22.60; epoch construction/tau/pilot, formal
  Stage 0, oracle/DP, RL, adaptive control, and aria2 were not started.

### 22.60 Pre-formalization item: trace-time anchor + anchor calibration

**Status: `TRACE_ANCHOR_READY` for the anchor mechanism and its measured
precision. Synthetic calibration only; NOT an epoch pilot and NOT formal Stage
0. No oracle/tau/RL/adaptive/aria2 work. §1–§5 and the harness remain READY.
No frozen artifacts touched.**

- **Canonical t=0:** `trace_start (t=0) = mm-link launch (parent monotonic) +
  StartupOffset`. It is explicitly **not** first-byte arrival. First-byte
  arrival is recorded as a measurement and mapped through the anchor to trace
  time; in the final calibration campaigns it landed within a few ms on either
  side of the predicted t=0 (startup jitter), never used as the origin.
- **Mechanism** (`internal/experiment/replay/anchor.go`): the parent records
  `LaunchParentMono` immediately before exec and `ExitParentMono` at mm-link
  exit; the child records `StartWallUnixNano` and the first byte event's
  monotonic since-start and wall time; the harness optionally captures
  `--uplink-log`/`--downlink-log` and records their line counts and last
  trace-ms. Trace time of an application instant = `event_mono −
  trace_start`. Wall timestamps bridge process clocks only; all duration
  arithmetic is monotonic.
- **Raw timestamps retained:** `Anchor` keeps launch/exit parent monotonic
  times, child start wall, first-byte since-start/wall, trace duration,
  downlink-log lines/last-ms, plus derived `TraceStartParentMono`, `TraceEOF`,
  `FirstByteTraceTime`, `StartupOffset`, `AlignmentTolerance`. Raw child byte
  events can be written to a JSONL timeline (`EventsPath`), preserving
  read-time monotonic and wall timestamps.
- **EOF endpoint:** `Anchor.TraceEOF` is the trace-file duration and is
  represented independently of child completion; `ExitParentMono` records the
  mm-link exit at EOF, and the downlink log's last trace-ms is retained as an
  independent cross-check. EOF is never estimated from the watchdog.
- **Calibration trace design:** synthetic trace with known delivery windows
  and two gaps — active [0,199] ms, gap, active [1000,1199] ms, gap, active
  [2000,2199] ms. Per run, the launch→t=0 offset is derived from the first
  post-gap edge (trace t=1000) and validated against the second edge
  (t=2000). 3 repetitions per C∈{1,4,16} per campaign; 4 campaigns (36 runs)
  were executed.
- **Observed alignment error:** campaign 1 (9 runs): offset mean 59.6 ms,
  median 52.2, min 45.4, max 132.5, SD 26.0 ms; worst second-edge residual
  73.6 ms. Later campaigns (18 + 18 runs): offset means 50.6–59 ms, SD ~4 ms,
  worst absolute residual < 15 ms; the 132 ms value was a first-run
  cold-start outlier. Across all 36 measured runs the worst absolute
  alignment residual was **73.6 ms**.
- **Frozen constants (from calibration only):** `StartupOffset = 60 ms`,
  `AlignmentTolerance = 100 ms` (covers the worst observed residual with
  margin). The tolerance is boundary-inclusive (`WithinTolerance`) and must
  not be relaxed for later epoch/oracle convenience.
- **C-dependence:** offsets and residuals for C={1,4,16} are within a few ms
  of each other within each campaign; the anchor mechanism is not strongly
  concurrency-dependent.
- **Tests:** unit A–H in `anchor_test.go` (trace-time arithmetic; startup
  offset independent of child start; first byte as measurement; EOF separate
  from child completion; monotonic arithmetic; calibration statistics;
  tolerance boundary accept/reject; raw-vs-derived reproducibility;
  `ParseTraceLog`) plus `TestAnchorCalibration` (repeated C={1,4,16},
  second-edge residual within tolerance, downlink log cross-checks). Results:
  replay package passes (calibration ~23 s), repeated calibration campaigns
  pass; `go test ./...`, `go test -race ./...`, `gofmt -l .`, `go vet ./...`,
  `go build ./...` clean.
- **Remaining (not started):** epoch construction was subsequently implemented
  in §22.61; tau, oracle/DP, formal Stage 0, RL, adaptive control, and aria2
  were not started.

### 22.61 Pre-formalization item: epoch construction + epoch-duration pilot

**Status: `EPOCH_DURATION_BLOCKED` — the epoch machinery and the pre-registered
pilot protocol are implemented and unit-tested, but the native-trace pilot has
not been executed, so NO epoch duration is frozen. Reason below. No
tau/oracle/headroom/formal-Stage-0/RL/adaptive/aria2 work. §1–§5, harness, and
anchor remain READY. No frozen artifacts touched.**

- **Epoch representation** (`internal/experiment/epoch/epoch.go`): `Slice`
  windows over `[0, traceDuration)` in the §22.60 calibrated trace-time
  coordinate, identical for every C (half-open; last epoch partial). `Epoch`
  records `trace_id`, `c`, `epoch_index`, trace start/end, nominal and
  observed durations, observed fraction, bytes received, `R =
  bytes/observed_duration`, validity and exclusion reason.
- **Byte attribution:** `Attribute` assigns each read-time event's bytes to
  the epoch containing the read timestamp (half-open); a request spanning
  epochs contributes to each; completion time is never used. `R` uses the
  **observed** duration as divisor.
- **Byte floor:** `ByteFloor = 5 MiB` received bytes per C per epoch.
  `Evaluate` excludes below-floor epochs (`below_byte_floor`).
- **Observed-duration rule:** `MinObservedFraction = 0.80`; below it the epoch
  is excluded (`below_observed_fraction`), mainly dropping the truncated final
  epoch. No rate correction.
- **Shared validity:** `ApplyShared` marks an epoch valid only if valid for
  every C; an epoch excluded for one C is marked `excluded_by_other_c` for all
  C. Tests assert the shared index set is identical across C.
- **Pilot accounting:** `Candidate`/`NewCandidate` compute candidate epochs,
  valid epochs, survival fraction, byte-floor and total exclusions;
  `SelectDuration` picks the **shortest** candidate with survival ≥ 80 % and
  ≥ 3 valid epochs. No headroom/best-C/oracle inspection.
- **Pre-registered protocol** (`DefaultProtocol`): candidates 30/60/90/120 s;
  C={1,2,4,8,16}; chunk 1 MiB; floor 5 MiB; fraction 0.80. `ValidateRevision`
  permits only candidate-list and byte-floor changes and rejects changes to
  the concurrency set, chunk size, observed-duration threshold, epoch
  definition, or an empty candidate list; any permitted revision requires the
  entire pilot to be rerun and a new frozen protocol.
- **Pilot trace-selection rule (frozen, reproducible):** scan installed
  `*.down` traces, compute mean delivery rate = `lines*1500/duration`;
  eligible traces must be ≥ 450 s so every candidate (including 120 s) can
  yield ≥ 3 valid epochs; classify the highest-rate eligible trace as `good`
  and the lowest-rate eligible trace as `degraded`. Scanned corpus
  (`/usr/local/share/mahimahi/traces`, originals untouched): eligible goods →
  `Verizon-LTE-driving` (1363 s, ~1.21 MB/s) vs `Verizon-EVDO-driving`
  (1062 s, ~0.065 MB/s); the 140 s `*-short` traces are excluded by the
  duration rule.
- **Blocked reason (exact):** `mm-link` replays in real time, so the pilot is
  5 C × (1363 s + 1062 s) ≈ **3.4 hours** of wall-clock replay per campaign
  (the pre-registered rule requires normal full-trace replays; truncated
  excerpts were not substituted). This stage implemented and validated the
  machinery but did not run a partial or post-hoc-modified pilot, so no
  duration was selected and no protocol parameter was changed. The pilot must
  run as its own wall-clock-bounded stage using the pre-registered protocol.
- **Tests** (`epoch_test.go`, all deterministic): A exact boundaries + partial
  tail; B read-time cross-epoch attribution; C byte-floor boundary (exact
  pass, below fail); D fraction boundary (0.80 pass, below fail); E observed
  divisor; F shared exclusion across C; G trace-time-anchored boundaries; H
  determinism; I exclusion accounting and shortest-passing selection; J
  revision rules; plus `SelectPilotTraces` and `ScanTraceCorpus`. Results:
  package passes; `go test ./...`, `go test -race ./...`, `gofmt -l .`,
  `go vet ./...`, `go build ./...` clean.
### 22.62 Diagnostic: early replay termination in the completed epoch pilot

**Status: `EARLY_REPLAY_ROOT_CAUSE_IDENTIFIED`. Diagnostic only; no code or
protocol change; the 10-run pilot is INVALID for epoch-duration selection.**

- **Root cause (evidence):** the downloader child aborted with a fatal
  `retry limit exceeded` error (per-request `Timeout=10s` in the driver vs
  native-trace delivery rates), exited with code 0 (the child CLI only exits
  non-zero on config errors), mm-link exited 0 with the child, and the harness
  classified `SUCCESS_TRACE_REACHED_EOF` solely from mm-link's exit code 0.
  Summaries: EVDO C1 `requests=2 failed=2 err="retry limit exceeded: range
  [0,1048575]"`, `canceled=false`; LTE C1 `437/440` success then retry-limit at
  range `[458227712,459276287]`. Downlink logs end at trace-ms 19 908 / 360 694
  / 40 331 (trace durations 1 062 016 / 1 363 038), exactly when the child died.
- **Not trace shortening or parser error:** raw traces first=0, last=duration
  (1 062 016 / 1 363 038 ms), monotonic with 0 violations (duplicate timestamps
  are the documented multiple-packets-per-ms case). `mm-link --once -- /bin/true`
  exited rc=0 in 183 ms, proving mm-link follows child exit rather than skipping
  trace time.
- **Classification bug:** `runMMLink` maps `cmd.Wait()==nil` to
  `SUCCESS_TRACE_REACHED_EOF` without requiring `summary.Err==""`, child
  completion semantics, or `DownlinkLogLastMs ≈ TraceDuration`.
- **Contributor:** the driver passes the same `.down` trace as both uplink and
  downlink; `.up` should be used for uplink (not changed here).
- **Minimal corrective candidates (NOT applied at diagnostic time; implemented
  in §22.63):** child CLI must exit non-zero on a fatal downloader error;
  harness must verify downlink-log endpoint vs trace duration before calling EOF
  success; driver timeout must be compatible with native-trace rates; use
  `.up`/`.down` respectively.
- **Artifacts:** `/home/vscode/epoch_pilot` (10 runs) — invalid for selection.
  Left untouched as forensic evidence; never reuse or merge it.

- **Remaining (not started):** the actual pilot run and frozen epoch duration,
  tau, oracle/DP, formal Stage 0, RL, adaptive control, aria2.
- **Pilot driver implemented (status still `EPOCH_DURATION_BLOCKED`):**
  `cmd/epoch-pilot` over `internal/experiment/pilot` executes the registered
  10-run matrix (2 traces × C={1,2,4,8,16}) sequentially via the existing
  `replay.Run` harness, one full-trace replay per trace/C, and evaluates all
  four candidate durations post-hoc from a single streaming pass over the
  child JSONL event timeline (`StreamEvents`, bounded memory: one int64 slice
  per candidate; events outside `[0, traceDuration)` are counted, not
  dropped). Each run writes a unique directory `<trace>_C<c>/` with
  `result.json` (all four durations), `anchor.json` (compact `AnchorMeta`
  derived from the frozen anchor; wall is used only as the cross-process
  epoch), the raw `events.jsonl`, `summary.json`, `downlink`/`uplink` logs,
  and the sparse output; missing/invalid anchors mark every epoch
  `anchor_alignment` (no fabricated alignment). `CombineTrace` applies
  shared cross-C validity per duration with per-C byte-floor diagnostics;
  `SummarizePilot` computes survival from the shared epoch set over both
  traces and selects the shortest duration meeting ≥80% survival and ≥3
  valid epochs. Resume skips any run whose `result.json` status is `ok`;
  failures are preserved as `result.failed.<n>.json` and reported as
  `Incomplete` without being converted into exclusions. `-dry-run` prints the
  resolved configuration and the 10 planned run directories without
  replaying. Tests cover the 10-run plan, run-dir/resume, streaming
  accumulation across all four durations, shared validity + shortest
  selection, insufficient-byte blocking, and missing-anchor non-fabrication.
  The 3.4-hour real pilot was NOT run and no duration is frozen.

### 22.63 Early-replay corrective fixes (implemented; native diagnostics executed and passed in §22.64)

**Status: `EARLY_REPLAY_FIX_IMPLEMENTED` — the four §22.62 corrective fixes are
implemented and covered by deterministic tests with short synthetic traces. The
two native diagnostics (EVDO C=1, LTE C=16) were subsequently executed and
passed (§22.64); the full 10-run pilot has **not** been run, so
`EPOCH_DURATION_BLOCKED` still stands and no epoch duration is frozen.
`/home/vscode/epoch_pilot` remains untouched and invalid.**

- **1. Child exit status.** `replay.RunDownloaderChild` now returns a fatal
  error when the final download failed and was not a harness-initiated
  cancellation (`context.Canceled`); a clean completion and a trace-EOF/watchdog
  signal cancellation return no error. The child CLIs (`cmd/epoch-pilot`,
  `cmd/mm-replay` `-child`) therefore exit non-zero on fatal downloader errors
  (e.g. retry-limit exceeded) and 0 on clean completion or trace-EOF
  cancellation. The final `summary.json` is written **before** returning and
  now carries explicit `canceled` and `fatal` flags plus `err`, so the error is
  always preserved. `context.DeadlineExceeded` (the child's own `-watchdog`
  self-limit) is classified fatal, not a trace-EOF cancellation.
- **2. Evidence-based EOF.** `SUCCESS_TRACE_REACHED_EOF` now requires **all** of:
  (a) mm-link exited normally (status 0), (b) no fatal child error, and
  (c) the mm-link `--downlink-log` last packet record reached the **downlink**
  trace duration within the frozen `AlignmentTolerance` (100 ms,
  boundary-inclusive). Missing/absent endpoint records are never valid.
  `replay.Result` exposes `TraceExpectedDuration`, `TraceObservedEndpoint`,
  `TraceEndpointResidual`, `EndpointValid`, `MMLinkExit`, `ChildStatus`,
  `ChildError`, and `Outcome`; two new outcomes make the non-EOF failures
  explicit: `DOWNLOADER_FATAL_ERROR` (exit 11) and `TRACE_ENDPOINT_NOT_REACHED`
  (exit 12). A status-0 mm-link exit alone is never EOF.
  - **Log-end semantics (verified from the forensic logs and code).** Downlink
    log lines are `<trace_ms> # <bytes>` packet records plus
    `<trace_ms> + <bytes>` non-packet records; `ParseTraceLog` counts only `#`
    records and takes their maximum timestamp. The expected endpoint is the
    **`.down` trace's own duration — not `max(up, down)`** — because the
    installed `.up` traces are ~2.3–2.7 s longer than their `.down` partners
    (LTE 1 363 371 ms vs 1 363 038 ms; EVDO 1 064 718 ms vs 1 062 016 ms); using
    the max would make the criterion unsatisfiable. `Result.TraceDuration`
    remains `max(up, down)` for epoch slicing.
  - **Verified against real mm-link.** The existing synthetic C={1,4,16} smoke
    runs and the 9-run `TestAnchorCalibration` now pass the endpoint check with
    the real `--downlink-log`. The forensic early deaths (`down.log` ending at
    19 908 / 340 901 / 40 331 ms vs durations 1 062 016 / 1 363 038 ms) are
    exactly what `TRACE_ENDPOINT_NOT_REACHED` rejects.
- **3. `.up`/`.down` pairing.** `pilot.ResolveTracePair` resolves a trace family
  (bare stem or a `.up`/`.down` path) to `<family>.up` (uplink) and
  `<family>.down` (downlink), requires both as regular files, and rejects
  missing or mismatched pairs. `pilot.TracePairFromPaths` enforces same
  directory + same family for explicit pairs. `pilot.RunSpec` carries
  `UpPath`/`DownPath`, `pilot.RunResult` persists both, and `cmd/epoch-pilot`
  never passes one file as both directions. Trace identity is unchanged
  (`Verizon-LTE-driving`, `Verizon-EVDO-driving`). No corpus paths were
  hard-coded in `internal/experiment/replay`.
- **4. Request timeout.** `cmd/epoch-pilot` gained an explicit `-timeout` flag
  (default **120 s**) passed to `replay.Config.Timeout` and to the child argv;
  the previous hidden 10 s is gone. The watchdog default remains **45 m**
  (`-watchdog`). No timeout participates in EOF detection: request timeouts
  surface as retry exhaustion → fatal child → non-EOF, and the watchdog
  surfaces as `WATCHDOG_TIMEOUT`.
- **Diagnostic selectors (harness-only, additive).** `cmd/epoch-pilot
  -only-trace <id> -only-c <n>` restricts the driver to one resolved run for
  diagnostics; it prints a `DIAGNOSTIC RUN` banner, performs **no** pilot
  report or duration selection, and the registered 10-run protocol/plan is
  byte-for-byte unchanged when the flags are omitted. `DefaultProtocol`,
  candidate durations, C set, chunk size, byte floor, observed-fraction rule,
  and trace selection are unchanged.
- **Forensics per run.** `result.json` now embeds the harness evidence
  (`evidence`: outcome, expected/observed endpoint, residual, endpoint_valid,
  mm_link_exit, child_status, child_error); failure records
  (`result.failed.<n>.json`) embed the same evidence, so a failed diagnostic is
  not reduced to a bare error string.
- **Tests (deterministic, seconds; synthetic traces/scripted mm-link only).**
  `internal/experiment/replay/replay_test.go`, synthetic cases A–F:
  `TestSyntheticEarlyReplayCasesAtoD` (A: real EOF + clean child → EOF; B:
  early clean child exit + mm-link rc=0 → NOT EOF; C: fatal child + mm-link
  rc=0 → NOT EOF; D: missing endpoint evidence → NOT EOF),
  `TestSyntheticEarlyReplayCaseE` (per-request timeout then successful retry →
  child success) and `TestSyntheticEarlyReplayCaseF` (per-request timeouts +
  retry exhaustion → child non-zero and replay `DOWNLOADER_FATAL_ERROR`, NOT
  EOF). Also: endpoint residual arithmetic
  (exact/inside/boundary/outside/observed-beyond/missing); missing summary →
  NOT EOF; child statuses (normal success → no error, fatal retry-limit →
  non-nil error with persisted `fatal`/`err`, cancellation → not fatal). The
  real mm-link smoke/calibration tests now require and assert endpoint
  evidence. `internal/experiment/pilot/pilot_test.go`: resolver accepted inputs
  (stem/`.up`/`.down`), missing `.up`, missing pair, mismatched families,
  cross-directory pair, missing member, and plan pairing.
- **Validation (this task):** `go test ./...`, `go test -race ./...`,
  `go test -count=5 ./...`, `go vet ./...`, `gofmt -l .`, `go build ./...` all
  pass. After the synthetic runs: no `mm-link` processes, no network
  namespaces, `tc qdisc` clean, no host network changes.
- **Prepared native diagnostic commands (executed 2026-09-26; results in
  §22.64; each is a single full-trace real-time replay, ~18 min for EVDO C=1
  and ~23 min for LTE C=16).** The default
  `-workload-size`/`-chunk`/candidate/floor/fraction protocol values are
  untouched; only `-timeout` is new. Each run directory preserves `down.log`,
  `up.log`, `summary.json`, `anchor.json`, `events.jsonl`, `result.json`, and
  the sparse `output.bin`:

  ```bash
  # EVDO C=1 diagnostic
  go run ./cmd/epoch-pilot \
    -only-trace Verizon-EVDO-driving -only-c 1 \
    -timeout 120s -watchdog 45m \
    -output-dir /home/vscode/epoch_pilot_fix_diag/EVDO-driving_C1

  # LTE C=16 diagnostic
  go run ./cmd/epoch-pilot \
    -only-trace Verizon-LTE-driving -only-c 16 \
    -timeout 120s -watchdog 45m \
    -output-dir /home/vscode/epoch_pilot_fix_diag/LTE-driving_C16
  ```

  Acceptance for each: the command prints
  `diagnostic run(s) complete; no pilot duration selection performed` and exits
  0; `evidence.outcome == SUCCESS_TRACE_REACHED_EOF`,
  `evidence.endpoint_valid == true`, `evidence.child_status` ∈
  {`clean`,`canceled`}, `evidence.mm_link_exit == 0`, and
  `summary.json` contains no `fatal` error. If any run is
  `TRACE_ENDPOINT_NOT_REACHED` / `DOWNLOADER_FATAL_ERROR` / `WATCHDOG_TIMEOUT`,
  the corrective fix is not validated and the full pilot must not start.
- **Not done / not claimed (unchanged):** the full 10-run pilot was **not**
  run; no epoch duration is frozen; `EPOCH_DURATION_BLOCKED` still stands; no
  tau/oracle/headroom/formal Stage 0, no RL/adaptive control, no aria2.
  `/home/vscode/epoch_pilot` is untouched forensic evidence.

### 22.64 Native replay diagnostics (executed 2026-09-26): both PASS

**Status: `NATIVE_DIAGNOSTICS_PASS — READY FOR CORRECTED PILOT`. Two single
full-trace real-time replays were run with the §22.63 corrective harness. No
full pilot, no epoch-duration selection, no epoch aggregation, no
headroom/oracle/tau, no RL/adaptive control, and no aria2 comparison were
performed. No protocol parameter was changed and `/home/vscode/epoch_pilot`
was not modified.**

- **Commands (exactly as prepared in §22.63):**

  ```bash
  go run ./cmd/epoch-pilot \
    -only-trace Verizon-EVDO-driving -only-c 1 \
    -timeout 120s -watchdog 45m \
    -output-dir /home/vscode/epoch_pilot_fix_diag/EVDO-driving_C1

  go run ./cmd/epoch-pilot \
    -only-trace Verizon-LTE-driving -only-c 16 \
    -timeout 120s -watchdog 45m \
    -output-dir /home/vscode/epoch_pilot_fix_diag/LTE-driving_C16
  ```

- **Run results (each exit 0; diagnostic mode reported
  `diagnostic run(s) complete; no pilot duration selection performed`):**

  | field | EVDO C=1 | LTE C=16 |
  |---|---|---|
  | trace pair (up / down) | `Verizon-EVDO-driving.up` / `.down` | `Verizon-LTE-driving.up` / `.down` |
  | wall-clock replay | 16:14:24 → 16:32:09 UTC (17 m 45 s) | 16:34:17 → 16:57:04 UTC (22 m 47 s) |
  | expected duration (`.down` last stamp) | 1 062 016 ms | 1 363 038 ms |
  | observed endpoint (`down.log` last `#` record) | 1 062 051 ms | 1 363 073 ms |
  | endpoint residual | **−35 ms** | **−35 ms** |
  | `endpoint_valid` | true | true |
  | `mm_link_exit` | 0 | 0 |
  | child status / error | `canceled` / `context canceled` | `canceled` / `context canceled` |
  | child fatal | false | false |
  | outcome | `SUCCESS_TRACE_REACHED_EOF` | `SUCCESS_TRACE_REACHED_EOF` |
  | attempts: requests/successful/failed | 64 / 63 / 1 | 1 522 / 1 504 / 18 |
  | bytes received | 66 093 056 B | 1 586 269 366 B |
  | events / out-of-range | 33 007 / 0 | 417 935 / 0 |
  | distinct connections | 1 (C=1) | 20 (C=16 flow pool) |
  | `downlink_log_lines` | 46 065 (= down trace line count) | 1 103 688 (= down trace line count) |

- **Verified facts:**
  - The 120 s request timeout was actually in effect: the mm-link command line
    in each `down.log` header contains `'-timeout' '2m0s'` (and no 10 s
    timeout). The slow EVDO C=1 run completed 63/64 attempts without the old
    retry-exhaustion failure mode; one attempt failed and was retried to
    success, and no fatal error occurred.
  - The endpoint reference is the **downlink** trace duration (1 062 016 /
    1 363 038 ms), not the uplink duration (1 064 718 / 1 365 371 ms) and not
    wall-clock time. The residual is −35 ms in both runs: mm-link replays
    until `max(up, down)`, so the final downlink records can land ~35 ms past
    the `.down` file's last stamp; this is within the frozen 100 ms
    `AlignmentTolerance` and is accepted boundary-inclusively.
  - Every downlink packet record of the full `.down` trace was delivered
    (`downlink_log_lines` equals the `.down` line count in both runs).
  - Per-run `result.json` contains the post-hoc epoch records for all four
    candidate durations (the driver's per-run evaluation), but no cross-C
    shared-validity aggregation, survival computation, or duration selection
    was run.
- **Cleanup / host safety (checked after each run and at the end):** no
  `mm-link` process, no child process, no network namespace, `tc qdisc`
  unchanged (`noqueue` on `lo`/`eth0`), no link changes, no persistent
  qdisc. `output.bin` files are sparse (63 MB / 1.5 GB actual for 64 GiB
  apparent).
- **Artifacts (fresh, isolated, distinct from `/home/vscode/epoch_pilot`):**
  - `/home/vscode/epoch_pilot_fix_diag/EVDO-driving_C1/Verizon-EVDO-driving_C1/{result.json, anchor.json, summary.json, events.jsonl, up.log, down.log, output.bin}` and the driver log `../driver.log`.
  - `/home/vscode/epoch_pilot_fix_diag/LTE-driving_C16/Verizon-LTE-driving_C16/{...}` and the driver log `../driver.log`.
- **Consequence / immediate next step:** the corrective harness reached the
  real downlink trace EOF cleanly on both the slowest single-flow case and the
  highest-concurrency case, so the corrected 10-run epoch-duration pilot
  (registered protocol, fresh output dir) may proceed. This diagnostic does
  **not** select an epoch duration and does **not** establish
  `EPOCH_DURATION_READY`; that requires the full pilot to run to completion.
  The full pilot was subsequently run and is INVALID (§22.65).

### 22.65 Corrected formal epoch pilot (executed 2026-09-26): INVALID — 3/10 runs failed the gate

**Status: `BLOCKED — FORMAL EPOCH PILOT INVALID`. The corrected 10-run pilot
was launched with the registered protocol and fresh output dir. EVDO C=1 and
C=2 reached valid trace EOF; EVDO C=4, C=8 and C=16 terminated early with
fatal `retry limit exceeded` errors and failed the per-run validity gate. Per
the pre-registered stop rule ("a run that terminates early must not be
silently accepted; if any run fails the gate: STOP"), remaining runs were
stopped: LTE C=1 was operator-aborted mid-run, LTE C=2 was aborted during
start-up when the driver was killed, and LTE C=4/C=8/C=16 were never run. No
epoch extraction, shared validity, candidate-duration coverage, duration
selection, oracle, tau, or headroom was computed. No protocol parameter was
changed. `/home/vscode/epoch_pilot` remains untouched and invalid.**

- **Command (fresh output dir; default workload/chunk/candidates/floor/
  fraction):**
  `go run ./cmd/epoch-pilot -timeout 120s -watchdog 45m -output-dir /home/vscode/epoch_pilot_corrected`
- **Deterministic order:** EVDO C1, C2, C4, C8, C16, then LTE C1, C2, C4,
  C8, C16 (the driver's `SortedSpecs` order).
- **Run matrix and outcome** (expected endpoint = `.down` last stamp,
  1 062 016 ms for EVDO / 1 363 038 ms for LTE):

  | # | Trace | C | Outcome | Expected (ms) | Observed (ms) | Residual (ms) | Endpoint valid | Attempts req/succ/fail | Bytes | Child | Note |
  |---|---|---|---|---|---|---|---|---|---|---|
  | 1 | EVDO | 1 | `SUCCESS_TRACE_REACHED_EOF` | 1 062 016 | 1 062 051 | −35 | true | 64/63/1 | 66 100 296 | canceled | full trace |
  | 2 | EVDO | 2 | `SUCCESS_TRACE_REACHED_EOF` | 1 062 016 | 1 062 050 | −34 | true | 65/62/3 | 66 160 200 | canceled | full trace |
  | 3 | EVDO | 4 | `DOWNLOADER_FATAL_ERROR` | 1 062 016 | 512 264 | 549 752 | false | 32/27/5 | 32 126 744 | fatal | early death |
  | 4 | EVDO | 8 | `DOWNLOADER_FATAL_ERROR` | 1 062 016 | 240 052 | 821 964 | false | 20/5/15 | 15 262 776 | fatal | early death |
  | 5 | EVDO | 16 | `DOWNLOADER_FATAL_ERROR` | 1 062 016 | 150 032 | 911 984 | false | 32/0/32 | 6 351 164 | fatal | early death |
  | 6 | LTE | 1 | `TRACE_ENDPOINT_NOT_REACHED` | 1 363 038 | 144 916 | 1 218 122 | false | 284/283/1 | 297 144 568 | canceled | operator stop (artifact) |
  | 7 | LTE | 2 | `DOWNLOADER_FATAL_ERROR` | 1 363 038 | 3 120 | 1 359 918 | false | 4/1/3 | 1 643 278 | fatal | startup abort artifact |
  | 8–10 | LTE | 4, 8, 16 | not run | — | — | — | — | — | — | — | stop rule |

- **Exact failure evidence (genuine gate failures, rows 3–5):**
  - EVDO C=4: at `2026-09-26T17:47:01Z`, range `[27262976,28311551]` after 2
    attempts; child lifetime 512.6 s; last byte event at +512.2 s; fatal summary
    `err="downloader: retry limit exceeded: range [27262976,28311551] after 2 attempts"`,
    `fatal=true`, `canceled=false`, `workload_exhausted=false`.
  - EVDO C=8: at `2026-09-26T17:51:01Z`, range `[5242880,6291455]` after 2
    attempts; child lifetime 240.1 s; last byte event at +240.0 s.
  - EVDO C=16: at `2026-09-26T17:53:31Z`, range `[8388608,9437183]` after 2
    attempts; child lifetime 150.1 s; last byte event at +120.0 s; **0
    successful ranges**.
  - All three: `mm_link_exit=1`, `child_status=fatal`,
    `endpoint_valid=false` with the residuals above; `result.failed.1.json`
    in each run dir preserves the full evidence.
- **Failure-matrix rows 6–7 are artefacts of the §8 stop, not replay
  defects.** LTE C=1 was healthy (283 successes, ~297 MB in 142.7 s) when the
  operator stopped it; its mm-link exited 0 early, so the harness correctly
  reported `TRACE_ENDPOINT_NOT_REACHED`. LTE C=2 had started when the driver
  was killed; its in-process HTTP server died with the driver, so its child
  saw connection failures and a fatal summary. Neither row may be counted as a
  valid run or as a replay failure.
- **Measured pattern (facts) and supported interpretation (not a proven
  per-attempt classification).** The EVDO `.down` trace carries at most
  46 065 × 1500 B ≈ 65.9 MiB over 1 062 s (≈62.4 KiB/s average). Under the
  single shared replay link a 1 MiB request's steady-state completion time
  scales with C (≈17 s at C1, ≈34 s at C2, ≈67 s at C4, ≈134 s at C8, ≈268 s
  at C16). C=8/16 exceed the 120 s per-request timeout budget on average;
  C=4 is near the budget and died in a degraded window after 27 MiB. All three
  failed runs stayed link-saturated until death (delivered bytes / lifetime
  ≈62.7 KB/s at C4, 63.6 KB/s at C8, 42.3 KB/s at C16), so the failures were
  not idle timeouts. The child persists per-attempt counts but not per-attempt
  error types, so "per-request timeout → retry exhaustion" is a timing/
  capacity-supported interpretation, not a directly recorded error class. The
  fatal condition itself (`retry limit exceeded` after 2 attempts) is
  measured, not inferred.
- **Corrective harness behaved as designed:** all three early terminations
  were classified `DOWNLOADER_FATAL_ERROR` with invalid endpoint evidence;
  none was misclassified as `SUCCESS_TRACE_REACHED_EOF`. The §22.62
  classification bug did not recur.
- **Workload exhaustion check:** pre-flight physical upper bounds
  (LTE `.down` = 1 103 688 × 1500 ≈ 1.54 GiB; EVDO `.down` ≈ 65.9 MiB) are far
  below the 64 GiB logical workload; every started run summary has
  `workload_exhausted=false`, and the largest transfer was ~297 MB. No run
  exhausted the logical object.
- **Epoch extraction / candidate coverage: NOT PERFORMED.** Shared validity
  requires all five C per trace; only EVDO C1/C2 produced valid `result.json`
  epoch records, and no LTE C reached EOF. Consequently there are no
  shared-valid epoch counts, exclusion tallies, coverage numbers, or duration
  selection for 30/60/90/120 s. The driver stopped before aggregation and did
  **not** write `pilot_report.json`. No partial coverage may be presented as
  pilot coverage, and the invalid `EPOCH_DURATION_BLOCKED` state stands.
- **Cleanup / host safety:** after the §8 stop, no `mm-link` process, no
  `epoch-pilot` child, no `run_pilot.sh`, no network namespace, `tc qdisc`
  unchanged (`noqueue` on `lo`/`eth0`), no link changes. Outputs are sparse
  (486 MB actual / 449 GB apparent). `/home/vscode/epoch_pilot` mtime
  unchanged (`2026-09-26 14:07:34`).
- **Artifacts:** `/home/vscode/epoch_pilot_corrected/` — `driver.log`,
  `run_pilot.sh`, `Verizon-EVDO-driving_C{1,2,4,8,16}/` and
  `Verizon-LTE-driving_C{1,2}/` with the standard per-run files
  (`result.json` only for the two valid runs; `result.failed.1.json`,
  `summary.json`, `anchor.json`, `events.jsonl`, `up.log`, `down.log`,
  `output.bin` as applicable). LTE C4/C8/C16 dirs were never created.
- **Not done / not claimed:** no epoch duration selected
  (`EPOCH_DURATION_BLOCKED`), no oracle/tau/headroom/CI/hypothesis tests, no
  RL/adaptive control, no LinUCB/DQN, no aria2/IDM, no protocol parameter or
  threshold change. The pilot is **invalid for epoch-duration selection**.
  The observed failure mechanism is outside the two parameters the revision
  rule allows to change (candidate list, byte floor), so no revision was made;
  resolving it requires a separately pre-registered protocol/harness decision
  (with explicit justification), not a silent change. **The mechanism was
  subsequently diagnosed in §22.66.**

### 22.66 EVDO concurrency/timeout censoring diagnosis (executed 2026-09-26)

**Status: `TIMEOUT_CENSORING_SUPPORTED` — diagnostic only. No source code,
formal protocol parameter, trace, or frozen artifact was modified. The formal
120 s request timeout was not changed; the 600 s value exists only in the
diagnostic directories. No headroom/oracle/tau/RL/aria2 work was done and no
pilot was rerun.**

- **Question:** are the §22.65 EVDO C=4/C=8/C=16 early deaths caused by the
  120 s per-request timeout interacting with C concurrent 1 MiB ranges sharing
  the low-rate EVDO replay?
- **Frozen-artifact re-analysis (no new runs needed for this part):**
  - **Trace gaps:** the EVDO `.down` trace's largest inter-opportunity gap is
    **11.6 s**; there are **no** gaps ≥30 s. The trace cannot cause a 120 s
    stall.
  - **Measured C-scaling on the valid runs:** C=1 completed 63 × 1 MiB
    requests with read-window mean **16.8 s** (max 72.7 s); C=2 mean
    **26.1 s** (max 76.3 s). A request's service time grows with C under the
    single shared bottleneck (≈63.5 KiB/s average).
  - **C=4 failure:** fatal range `[27262976,28311551]`; 27 completed / 4
    partial attempts (807 KB–1.02 MB); child lifetime 512.6 s, last read
    event +512.2 s, death 0.4 s later.
  - **C=8 failure:** fatal range `[5242880,6291455]`; clear two-wave
    structure — first wave reads 0–118 s, then no reads, retry connections
    read 145–239 s, death at **240.1 s = exactly 2 × 120 s**; 13 partial
    attempts (616–970 KB).
  - **C=16 failure:** fatal range `[8388608,9437183]`; all 16 first-wave
    requests read 0–119 s, then **zero byte events** until death at **150.1 s**
    (≈120 s request timeout + 30 s transport dial timeout); 0 successes.
  - Caveat: the persisted artifacts contain aggregate counts and the final
    fatal error only — **no per-attempt request IDs, per-attempt error strings,
    or HTTP status codes** (metrics are in-memory). Attempt timing above is
    reconstructed from the byte-event timelines and the fixed timeout budgets.
- **Controlled timeout-sensitivity diagnostic** (fresh dirs, EVDO only, chunk
  1 MiB, watchdog 45 m; 600 s is diagnostic only):

  | C | timeout | outcome | endpoint valid | failed attempts | child | key read-window evidence |
  |---|---|---|---|---|---|---|
  | 4 | 120 s (formal) | `DOWNLOADER_FATAL_ERROR` `[32505856,33554431]` | false | 9 | fatal | 0 completed requests >120 s |
  | 4 | 600 s | `SUCCESS_TRACE_REACHED_EOF` (residual −35 ms) | true | 4 | canceled | **4** completed requests needed >120 s (max 183.9 s) |
  | 8 | 120 s | `DOWNLOADER_FATAL_ERROR` `[4194304,5242879]` | false | 15 | fatal | 0 completed >120 s; death at 240.0 s |
  | 8 | 600 s | `SUCCESS_TRACE_REACHED_EOF` (residual −35 ms) | true | 8 | canceled | **24** completed requests needed >120 s (max 421.0 s) |
  | 16 | 120 s | `DOWNLOADER_FATAL_ERROR` `[5242880,6291455]` | false | 32 | fatal | 0 successes; death at 150.0 s |
  | 16 | 600 s | `DOWNLOADER_FATAL_ERROR` `[35651584,36700159]` | false | 17 | fatal | **31** completed >120 s (max 381.3 s); progressed to 62.0 MB / 969.5 s of 1 062.0 s |

- **Mechanism (evidence-supported):**
  1. The EVDO link is a single shared bottleneck (≈63.5 KiB/s average; max
     trace gap 11.6 s).
  2. Per-request service time grows with C (measured read windows: C1 mean
     16.8 s; C2 26.1 s; C4 600 s-run p50 39.5 s, max 183.9 s; C8 600 s-run
     p50 66.4 s, max 421.0 s; C16 600 s-run p50 158.1 s, max 381.3 s), with a
     heavy tail from bursty delivery opportunities.
  3. The 120 s `http.Client.Timeout` cancels any request whose service time
     exceeds 120 s. The worker retries the same 1 MiB range from scratch (no
     partial progress is retained). Under sustained congestion the retry also
     exceeds 120 s (C=8 dies at exactly 240 s), or the retry's fresh TCP dial
     exceeds the fixed 30 s transport dial timeout and fails with zero bytes
     (C=16 dies at ≈120 s + 30 s = 150 s).
  4. Raising only the timeout to 600 s removes the failure at C=4 and C=8:
     both reach genuine downlink EOF, and the requests that genuinely needed
     >120 s (4 at C=4, 24 at C=8) complete normally.
  5. At C=16, 600 s is still insufficient: the fatal range (allocated
     ≈340 s) hit the 600 s request timeout at ≈940 s, and its retry could not
     complete a TCP handshake within the fixed 30 s dial timeout, dying at
     ≈971 s. That residual is still timeout-driven (600 s request timeout +
     30 s dial timeout) under the same C-way sharing; no non-timeout
     mechanism is required to explain any observed death.
  6. No trace gap, server error class, or workload exhaustion contributes;
     every started run has `workload_exhausted=false`.
- **Formal-pilot implication:** the formal **120 s request timeout is NOT safe
  for the fixed-C EVDO measurement**: it is an unintended **C-dependent
  censoring mechanism** — it truncates exactly the requests that high
  concurrency makes slow, then the retry path exhausts. This fully explains
  the §22.65 C=4/C=8/C=16 invalid runs. Simply raising the timeout to 600 s
  fixes C=4/C=8 but not C=16 (retry dial/transport timeouts under 16-way
  congestion), so the resolution must be a **separately pre-registered
  protocol/harness decision**, not a silent timeout change. No change was made
  in this task.
- **Exact diagnostic directories:**
  `/home/vscode/evdo_timeout_diag/{C4_120s,C4_600s,C8_120s,C8_600s,C16_120s,C16_600s}/Verizon-EVDO-driving_C<c>/`
  with `result.json` or `result.failed.1.json`, `summary.json`, `events.jsonl`,
  `up.log`, `down.log`, `output.bin`, plus `driver.log` and `run_diag.sh` at the
  top level. `/home/vscode/epoch_pilot` and `/home/vscode/epoch_pilot_corrected`
  were not modified.
- **Not done / claimed:** no source/protocol change, no full pilot, no
  epoch-duration selection, no headroom/oracle/tau/CI, no RL/adaptive control,
  no LinUCB/DQN, no aria2/IDM.

### 22.67 Protocol decision gate: watchdog-only formal replay deadlines (design; implemented in §22.68)

**Status: `TIMEOUT_POLICY_READY_FOR_PREREGISTERED_REVISION` — design/diagnosis
only. No source code and no formal protocol parameter was changed. No pilot
was run. No epoch/headroom/oracle/tau/RL/aria2 work.**

- **Finding restated (§22.66):** the formal 120 s request deadline is an
  unintended C-dependent censoring mechanism (C=8 dies at exactly 2×120 s;
  C=16 dies at 120 s + the fixed 30 s transport dial timeout; 600 s fixes
  C=4/C=8 but not C=16, whose residual failure is a 600 s request timeout
  followed by a 30 s retry dial timeout). The EVDO trace's maximum delivery
  gap is 11.6 s, so no trace property justifies a per-request deadline.
- **Current timeout/deadline stack (actual code):**

  | Mechanism | Current value | Scope | Can terminate a range? |
  |---|---|---|---|
  | `http.Client.Timeout` (pilot `-timeout` → `downloader.Config.Timeout` → `fetcher.Config.Timeout`) | 120 s formal (600 s diagnostic only); `fetcher.DefaultTimeout = 30 s` when ≤0 | whole request incl. dial/headers/body | **yes — verified censoring** |
  | transport `DialContext` `net.Dialer.Timeout` | 30 s fixed constant (`newHTTP1Transport`) | TCP connect per new connection | **yes — C=16 retry death** |
  | `TLSHandshakeTimeout` | 10 s | TLS handshake (HTTP-only transport; never used) | no (plaintext) |
  | `ExpectContinueTimeout` | 1 s | 100-continue wait (no `Expect` header sent) | no |
  | `ResponseHeaderTimeout` | unset (0) | — | no |
  | `IdleConnTimeout` | 90 s | pooled idle connections between requests | no (never aborts an in-flight request; next request re-dials) |
  | TCP keepalive | 30 s interval | socket probes | no |
  | request context deadline inside `fetcher` | none (ctx inherited) | request lifetime | only via ctx cancellation |
  | child self-watchdog `ChildConfig.MaxRuntime` (`-watchdog`) | 45 m | child run ctx (`context.WithTimeout`) | yes — whole-run, C-independent, non-EOF |
  | parent replay watchdog `replay.Config.MaxRuntime` | 45 m | startup+replay+teardown | yes — whole-run, C-independent, non-EOF |
  | mm-link `--once` trace EOF | trace duration (1 062 / 1 363 s) | measurement endpoint | intended endpoint |
  | retry budget (`MaxRetries=1`, 2 attempts) | 2 | per range | only with a deadline or genuine error |
  | `terminateTree` grace | 5 s pilot (3 s default) | teardown | cleanup only |
- **Proposed formal values (recommendation, not applied):**

  | Mechanism | Proposed formal value | Purpose |
  |---|---|---|
  | HTTP request deadline | **none** | remove C-dependent censoring |
  | connection dial deadline | **none** | remove C-dependent censoring; dials are context-cancelable |
  | child self-watchdog | 45 m (unchanged) | defense-in-depth whole-run bound |
  | parent watchdog | 45 m (unchanged) | hard safety boundary |
  | TLS/Expect/Idle/keepalive | unchanged (unused or unable to abort in-flight work) | implementation detail |
  | retry semantics | unchanged (in-place retry only on genuine errors; cancellation is never retried) | preserve cancellation ≠ retryable failure |
- **Candidates considered:** (1) keep 120 s — rejected by §22.66; (2) raise to
  600 s — rejected (C=16 still fails, and any finite constant can censor a
  heavy tail; not C-independent); (3) **watchdog-only — recommended**;
  (4) watchdog-derived per-request deadline — rejected as redundant (the
  request context is already canceled by the same watchdog path);
  (5) no request timeout but keep the 30 s dial timeout — rejected (the C=16
  retry dial censoring remains).
- **Safety argument (verified by code inspection and existing tests):**
  - The parent watchdog's `terminateTree` SIGTERMs descendants (child first),
    then mm-link, then SIGKILLs the group and reports leftovers;
    `TestRunWatchdogTimeout` passes (outcome `WATCHDOG_TIMEOUT`, no leftovers,
    no mm-link).
  - The child derives its run context from `signal.NotifyContext` plus an
    optional 45 m `context.WithTimeout`; `downloader.Download(runCtx)` passes
    it to every worker and `fetcher.Fetch` uses
    `http.NewRequestWithContext(ctx, ...)`, so an outstanding HTTP request is
    canceled by trace-EOF signalling or the watchdog;
    `TestChildExitStatuses/cancellation_is_not_fatal` passes (a blocked HTTP
    request is canceled and the child exits non-fatally).
  - A hung request therefore cannot hang the harness: it is bounded by trace
    EOF or the 45 m watchdog, both of which yield a non-EOF or intended-EOF
    outcome and trigger cleanup.
- **Genuine hung request under the proposed policy:** stays in flight (no
  retry); at trace EOF mm-link signals the child, the request is canceled, the
  range stays partial, and the run still reaches
  `SUCCESS_TRACE_REACHED_EOF` with all timestamped byte events preserved. If
  the run itself hangs before EOF, the 45 m watchdog produces
  `WATCHDOG_TIMEOUT` (non-EOF) and cleans up.
- **Implementation gap (for the revision task, not done here):** the code
  cannot currently express "no deadline": `fetcher.New` maps `Timeout <= 0` to
  `DefaultTimeout = 30 s`, `downloader.Config.validate` rejects negative
  timeouts, and the transport dial timeout is a fixed 30 s constant. The
  revision needs an explicit no-timeout policy (e.g., a dedicated config field
  or sentinel) for both the HTTP client and the dialer, plus a pilot flag.
- **Required protocol revision:** old rule = pilot request timeout 120 s
  (§22.63 item 4) + fixed 30 s transport dial timeout; new rule =
  **watchdog-only** as above. Affected protocol surfaces: `epoch.Protocol` /
  `DefaultProtocol` (add the explicit timeout policy), `epoch.ValidateRevision`
  (permit it as a versioned pre-registration), §22.61/§22.63 documentation, and
  the pilot/`mm-replay` CLI defaults. **A fresh complete 10-run pilot from
  scratch in a new output dir is required after the revision**; no existing
  pilot or diagnostic result may be reused for selection, and the replacement
  has not been validated on the native matrix.
- **Synthetic watchdog check run here (no source changes):**
  `go test ./internal/experiment/replay/ -run 'TestRunWatchdogTimeout|TestChildExitStatuses'`
  → both pass. A true no-deadline replay cannot be executed until the
  no-timeout policy is implemented.

### 22.68 Watchdog-only timeout policy implemented (versioned protocol revision)

**Status: `WATCHDOG_ONLY_POLICY_IMPLEMENTED_AND_VALIDATED` — the §22.67
revision is implemented and covered by synthetic tests plus one bounded native
smoke. No full pilot was run, no epoch/headroom/oracle/tau work was done, and
no historical artifact was modified. The replacement has not been validated
across the complete native matrix; a fresh complete pilot is required.**

- **Old policy (version 1, §22.65):** request deadline 120 s, transport dial
  deadline 30 s, parent/child watchdog 45 m. Representable as
  `epoch.LegacyProtocolV1()`.
- **New policy (version 2):** HTTP request deadline **none**, transport dial
  deadline **none**, parent/child watchdog **45 m**, retry semantics
  unchanged. `epoch.DefaultProtocol()` now returns version 2 with an explicit
  `TimeoutPolicy{RequestDisabled: true, DialDisabled: true, watchdogs 45 m}`.
- **Implementation (no new dependencies):**
  - `internal/fetcher`: `Config` gained explicit `NoRequestTimeout`,
    `DialTimeout`, `NoDialTimeout` fields and a `Validate()` that rejects
    ambiguous combinations (flag + positive duration, negative durations,
    no-timeout flags with a caller-supplied client). `New` sets
    `http.Client.Timeout = 0` for no-request-timeout and builds the transport
    via `newHTTP1Transport(dialTimeout)` / `newDialer(timeout)`; a zero dialer
    timeout means no dial deadline, while the dial remains context-cancelable.
    HTTP/1.1-only, keep-alive, `MaxIdleConns(PerHost)`, `IdleConnTimeout`,
    TLS/Expect timers are unchanged; no response-header timeout is added.
  - `internal/downloader`: `Config` mirrors the four policy fields, validates
    them through `fetcher.Config.Validate`, and passes them to the fetcher.
  - `internal/experiment/replay`: `ParseDeadline`/`DeadlineString` implement
    the explicit `"none"` vs positive-duration syntax; `ChildConfig` carries
    the policy into `downloader.Config`.
  - Context propagation is unchanged: `signal.NotifyContext` + optional
    watchdog `context.WithTimeout` → `Download(runCtx)` → `Fetch(ctx)` →
    `http.NewRequestWithContext`; the transport dialer honors ctx. No
    request-local deadline is created in watchdog-only mode.
  - Retry semantics are unchanged: in-place retry of the original 1 MiB range
    only after a genuine fetch failure, `MaxRetries = 1` (2 attempts), and the
    existing `ctx.Err()` check before retrying, so cancellation is never
    retried.
  - CLIs: `cmd/epoch-pilot` `-timeout` and `-dial-timeout` now take
    `"none"` or a positive duration; **the formal default is `none`/`none`**
    and the banner prints `request deadline: none  dial deadline: none
    watchdog: 45m0s (watchdog-only)`. `cmd/mm-replay` has the same two flags
    (numeric defaults `30s` for generic smoke use, `"none"` available). The
    historical diagnostic commands (`-timeout 120s`, `-timeout 600s`) still
    parse and remain diagnostic-only; 600 s never became a formal default.
- **Protocol revision:** `epoch.Protocol` gained `Version` and `Timeout`;
  `DefaultProtocol()` is version 2 watchdog-only, `LegacyProtocolV1()`
  preserves the historical 120 s/30 s policy. `ValidateRevision` now also
  freezes the timeout policy and version for ordinary (candidate/floor)
  revisions, and `ValidateTimeoutPolicyRevision` requires a version bump, a
  watchdog-only replacement, unchanged watchdogs, and unchanged frozen
  dimensions (C set, chunk, candidate epochs, byte floor, observed fraction).
- **EOF/cancellation semantics (reviewed, unchanged and tested):**
  trace EOF cancels outstanding requests through the child context and can
  yield `SUCCESS_TRACE_REACHED_EOF` only with a normal mm-link exit, valid
  downlink endpoint evidence, and a non-fatal canceled child; watchdog
  cancellation before EOF yields `WATCHDOG_TIMEOUT`; external/operator
  cancellation yields `CANCELED`; a downloader fatal error yields
  `DOWNLOADER_FATAL_ERROR`. No outstanding range is required to complete.
- **Synthetic tests (all pass; short requests only):**
  1. `fetcher.TestWatchdogOnlyConfigDisablesRequestAndDialDeadlines` —
     `client.Timeout == 0`, `ResponseHeaderTimeout == 0`, dialer timeout 0,
     defaults unchanged.
  2. `replay.TestWatchdogOnlyWatchdogCancelsBlockedRequest` — blocked HTTP
     request + parent watchdog → `WATCHDOG_TIMEOUT`, non-EOF, no leftovers.
  3. `replay.TestRunSmokeConcurrencies` (and `TestAnchorCalibration`) now run
     their synthetic mm-link children in `none`/`none` mode and still reach
     `SUCCESS_TRACE_REACHED_EOF` with valid endpoint evidence while in-flight
     requests are canceled at EOF.
  4. `replay.TestCanceledChildBeforeTraceEOFFails` — canceled child + short
     endpoint → `TRACE_ENDPOINT_NOT_REACHED` (not EOF).
  5. `replay.TestSyntheticEarlyReplayCasesAtoD/C` and
     `TestSyntheticEarlyReplayCaseF` — fatal child → `DOWNLOADER_FATAL_ERROR`
     (not EOF).
  6. `downloader.TestWatchdogOnlyPolicyRetriesGenuineFailures` — a 500 then
     206 with the no-deadline policy still retries (2 attempts, success).
  7. `downloader.TestWatchdogOnlyPolicyDoesNotRetryCancellation` — canceling
     during a blocked request records exactly 1 attempt and 1 HTTP request.
  8. `fetcher.TestNoRequestTimeoutHasNoClientLocalDeadline` — a 200 ms
     delayed response succeeds with no deadline while a 20 ms timeout fails;
     `fetcher.TestTimeoutConfigValidation` rejects ambiguous configs;
     `epoch.TestWatchdogOnlyProtocolRevision` covers the versioned policy.
     Plus `replay.TestParseDeadline`, `TestExternalCancellationBeforeTraceEOF
     IsNotSuccess` (`CANCELED`, not EOF) and `cmd/epoch-pilot` deadline
     parsing tests.
- **Validation (this task):** `gofmt -l .` clean; `go test ./...` pass;
  `go test -race ./...` pass; `go vet ./...` pass; `go build ./...` pass. The
  replay package also passes `-count=5` after fixing a teardown race in the
  new external-cancellation test (its fake mm-link now `exec`s the sleeper so
  there is no shell grandchild). No mm-link/child/namespace/qdisc residue
  after the runs.
- **Bounded native smoke (not Stage 0 evidence):** one EVDO C=1 run via
  `cmd/mm-replay` with `-timeout none -dial-timeout none` and
  `-max-runtime 30s` on the real (read-only) traces returned
  `WATCHDOG_TIMEOUT` (exit 5) after ~30 s; the child argv in the downlink-log
  header shows `'-timeout' 'none' '-dial-timeout' 'none'`; the child summary
  is final/canceled/non-fatal (354 KB read before cancellation); cleanup was
  clean. Full trace was not replayed; C=4/8/16 and LTE were not run.
- **Fresh-pilot requirement:** the next formal pilot must run the complete
  10-run matrix under version 2 from scratch in a new output dir; no prior
  pilot or diagnostic result (including `/home/vscode/epoch_pilot`,
  `/home/vscode/epoch_pilot_corrected`, `/home/vscode/evdo_timeout_diag`) may
  be reused for selection. The new protocol has **not** been validated across
  the full native matrix.

### 22.69 Formal Stage 0 pilot under protocol v2 (executed 2026-09-27): PASS — 120 s epoch selected

**Status: `EPOCH_PILOT_V2_COMPLETE — READY_FOR_ORACLE_STAGE`.** The full
10-run fixed-C matrix ran under the version-2 watchdog-only protocol in a
brand-new output dir. All 10 runs reached valid trace EOF; epoch extraction
and shared validity completed; only the 120 s candidate met the frozen
coverage rule and was selected. No headroom/oracle/tau/RL/aria2 work was done
and no historical artifact was touched.**

- **Protocol:** version 2 — request deadline **none**, dial deadline
  **none**, parent/child watchdog **45 m**, retry `MaxRetries=1` unchanged.
  Frozen dimensions unchanged: C={1,2,4,8,16}, 1 MiB chunk, candidates
  30/60/90/120 s, byte floor 5 MiB, observed fraction 0.80, anchor
  60 ms/100 ms, the same two traces. Trace SHA-256 recorded in
  `pilot_manifest.json`.
- **Command / window:**
  `go run ./cmd/epoch-pilot -output-dir /home/vscode/epoch_pilot_v2`
  (2026-09-27 08:53:08 → 12:15:46 UTC, ~3 h 23 m; 10 sequential full
  replays).
- **Run matrix** (all `SUCCESS_TRACE_REACHED_EOF`, `endpoint_valid=true`,
  `mm_link_exit=0`, child `canceled` non-fatal, no fatal error, no workload
  exhaustion; expected endpoint = `.down` duration 1 062 016 ms EVDO /
  1 363 038 ms LTE):

  | Trace | C | Expected ms | Observed ms | Residual ms | Requests | Failed attempts (retries) | Bytes |
  |---|---|---|---|---|---|---|---|
  | EVDO | 1 | 1 062 016 | 1 062 073 | −57 | 62 | 1 | 64 181 920 |
  | EVDO | 2 | 1 062 016 | 1 062 054 | −38 | 64 | 2 | 66 364 880 |
  | EVDO | 4 | 1 062 016 | 1 062 055 | −39 | 66 | 4 | 66 453 048 |
  | EVDO | 8 | 1 062 016 | 1 062 051 | −35 | 69 | 8 | 66 442 152 |
  | EVDO | 16 | 1 062 016 | 1 062 051 | −35 | 72 | 16 | 66 279 496 |
  | LTE | 1 | 1 363 038 | 1 363 075 | −37 | 1 466 | 1 | 1 536 486 720 |
  | LTE | 2 | 1 363 038 | 1 363 075 | −37 | 1 513 | 2 | 1 585 473 968 |
  | LTE | 4 | 1 363 038 | 1 363 074 | −36 | 1 514 | 4 | 1 585 857 048 |
  | LTE | 8 | 1 363 038 | 1 363 077 | −39 | 1 517 | 8 | 1 587 233 376 |
  | LTE | 16 | 1 363 038 | 1 363 096 | −58 | 1 524 | 16 | 1 587 822 346 |

  The v1 failures (EVDO C=4/8/16) no longer occur under v2: all five EVDO
  runs reached the full trace endpoint.
- **Timeout verification:** every run's downlink-log command line contains
  `'-timeout' 'none' '-dial-timeout' 'none'`; the driver banner printed
  `request deadline: none  dial deadline: none  watchdog: 45m0s
  (watchdog-only)`. No 30 s / 120 s / 600 s fallback occurred.
- **Retries/failures:** failed attempts equal C in every run (1/2/4/8/16),
  each retried in place to success; no retry-limit error occurred. These are
  genuine transport-level retries (no request deadline exists), not timeout
  censoring.
- **Workload exhaustion:** none. All summaries have
  `workload_exhausted=false`; physical upper bounds (LTE `.down` ≈ 1.54 GiB,
  EVDO ≈ 65.9 MiB) are far below the 64 GiB logical workload.
- **Epoch coverage (shared validity across C; frozen thresholds):**

  | Duration | Candidate epochs | Shared-valid | Excluded | Aggregate survival | Min-trace valid | Passes |
  |---|---|---|---|---|---|---|
  | 30 s | 82 | 43 | 39 | 52.4 % | 0 | no |
  | 60 s | 41 | 25 | 16 | 61.0 % | 3 | no |
  | 90 s | 28 | 21 | 7 | 75.0 % | 6 | no |
  | 120 s | 21 | 18 | 3 | **85.7 %** | **7** | **yes** |

  Per trace: EVDO 30 s 0/36 valid (all below the 5 MiB floor), 60 s 3/18,
  90 s 6/12, 120 s 7/9; LTE 30 s 43/46, 60 s 22/23, 90 s 15/16, 120 s 11/12.
  Exclusion reasons: EVDO is `below_byte_floor`-limited (≈62 KiB/s average
  trace rate means sub-120 s windows usually carry <5 MiB); LTE has two
  startup 30 s floor exclusions and one `below_observed_fraction` final
  partial epoch per duration (5 C each).
- **Candidate-duration validity / selection:** under the frozen rule
  (aggregate survival ≥ 0.80 and ≥ 3 shared-valid epochs per trace) only
  **120 s** is suitable; the driver selected `2m0s` (`passed=true`,
  `incomplete=[]`). 30/60/90 s are reported unsuitable and were **not**
  revised or tuned.
- **Artifacts:** `/home/vscode/epoch_pilot_v2/` —
  `pilot_manifest.json` (protocol/trace hashes/parameters),
  `driver.log`, `run_pilot.sh`, `pilot_report.json` (driver selection),
  `epochs_all.jsonl` (complete machine-readable per-epoch rows: trace, C,
  duration, index, bytes, observed duration/fraction, R, per-C validity,
  exclusion), `epoch_coverage.json`/`.txt` (per-duration and per-trace
  coverage, per-C validity, exclusion reasons, full per-epoch table), and
  `Verizon-{EVDO,LTE}-driving_C{1,2,4,8,16}/` each with `result.json`,
  `anchor.json`, `summary.json`, `events.jsonl`, `up.log`, `down.log`,
  `output.bin`.
- **Cleanup:** no `mm-link`, no child, no `run_pilot.sh`, no network
  namespace, `tc qdisc` unchanged (`noqueue` on `lo`/`eth0`), no link changes.
- **Readiness:** the 120 s epoch duration is frozen by the protocol's own
  selection rule, so the next stage may proceed to the oracle/headroom work.
  This pilot contains **no** oracle, tau, headroom, confidence-interval, RL,
  adaptive-control, or aria2 evidence.

