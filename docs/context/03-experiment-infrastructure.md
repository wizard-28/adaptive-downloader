## 14. Controlled experiment server (`internal/experiment/server`)

Deterministic HTTP Range test server; independent of the downloader.

- `New(content []byte) *Server` (serves a private copy);
  `Size()`; `ServeHTTP`.
- Ranges: `bytes=START-END`, open-ended `START-` (clamps end); returns **206**
  with `Content-Range: bytes START-END/TOTAL`, correct `Content-Length`, exact
  bytes. No `Range` → **200** with full body. Invalid/unsatisfiable → **416**
  with `Content-Range: bytes */TOTAL`.
- Runtime knobs (mutex-guarded, safe while serving): `SetDelay`,
  `SetRate(bytes/sec)` (application-level pacing, *not* network shaping),
  `SetRangeDelay(start,end,d)`, `SetRateAfter(n, rate)` (deterministic
  request-count rate transition).
- Deterministic failures: `FailNext(n)`, `FailNextStatus(n,status)`,
  `FailRange(start,end)`, `FailHeader(name,value)`, `ClearFailures()`.
- `Stats()` returns `TotalRequests`, `Successful`, `Failed`, `BytesServed`,
  `RequestedRanges` (copy), `CurrentRate`, `RateScheduleActive` — safe under
  concurrency.
- The rate model paces in 16 KiB chunks; it is server-side application
  throttling, not TCP emulation.

### 14.1 Wall-Clock Server Rate Schedule (experimental)

**Motivation.** The existing `server-throttle` scenario switches rate after a
fixed **request count**, which confounds the transition with request
granularity: a 32 KiB configuration makes ~128 requests (so requests 5+ see the
higher rate) while a 1 MiB configuration makes 4 requests (all at the initial
rate). This was exposed by the aria2 comparison and the granularity study
(§22.16–§22.17). A wall-clock schedule removes that confound.

**API (additive).**
```go
type RateSchedule struct {
    InitialRate     int64
    TransitionAfter time.Duration
    SubsequentRate  int64
}
func (r RateSchedule) Validate() error
func (s *Server) SetRateSchedule(schedule RateSchedule) error        // start = now
func (s *Server) SetRateScheduleAt(start time.Time, schedule RateSchedule) error
func (s *Server) ClearRateSchedule()
func (s *Server) Schedule() (RateSchedule, time.Time, bool)
func (s *Server) CurrentRate() int64
```
`Validate`/`SetRateSchedule` reject `InitialRate <= 0`, `SubsequentRate <= 0`,
and `TransitionAfter < 0` (`ErrInvalidRateSchedule`).

**Semantics.** While a schedule is active, the pacing rate is
`InitialRate` for `elapsed < TransitionAfter` and `SubsequentRate` for
`elapsed >= TransitionAfter`, where `elapsed = now - start` (the transition at
exactly `TransitionAfter` uses the subsequent rate). The rate is selected once
per request at request start and is **independent of request count, bytes,
chunk size, and concurrency**. A request that begins before the transition is
paced entirely at the rate selected at its start (matching the existing
request-count behavior).

**Precedence / compatibility.** An active schedule takes precedence over
`SetRateAfter` and the fixed rate; `ClearRateSchedule` restores them. Existing
behavior is unchanged when no schedule is set: `SetRate`, `SetRateAfter`, range
handling, failure injection, pacing, request logging, and stats accounting are
untouched. Rate selection is centralized in `currentRateLocked`
(schedule → request-count switch → fixed rate).

**Clock handling.** The server holds an injectable `now func() time.Time`
(default `time.Now`), so tests exercise schedules deterministically without
sleeping.

**Tests.** `internal/experiment/server/server_test.go`: validation; before/at/
after transition (boundary at exactly `TransitionAfter` → subsequent);
independence from request count (schedule wins even with a request-count switch
configured and 5 requests served); same elapsed time → same rate; the
deterministic sequence diagnostic (below); unchanged `SetRateAfter` semantics;
clear/restore; and concurrent `CurrentRate`/`Stats` consistency under `-race`.

**Diagnostic (deterministic, fake clock).** With `512 KiB/s → 4 MiB/s after
100 ms` and a 4 MiB file: a 32 KiB request sequence first observes the
subsequent rate at request **2** (elapsed 125 ms) and a 1 MiB sequence at
request **1** (elapsed 2 s). The request index differs, but both cross at the
same wall-clock transition of 100 ms — the transition is time-based, not
request-count-based.

**Status: experimental infrastructure only; no performance conclusions are
drawn from it, it is not part of `DefaultMatrixConfig`, and no existing
benchmark scenario or dataset was changed.**

---

## 15. Network / tc-netem (`internal/experiment/network`)

- `Config{BandwidthBps, Latency, Jitter, LossPercent}`; `Validate` requires
  `jitter` only with `latency`, loss in `[0,100]`, and at least one non-zero
  field (`ErrEmptyConfig`).
- `Controller{Prefix, Exec}` with `Apply`, `Clear`, `Show`,
  `InterfaceForHost`; helpers `InterfaceExists` and package-level
  `Apply`/`Clear`/`Show`.
- Shells out to `tc` (`qdisc replace dev IFACE root netem …`) and `ip`
  (`route get`). `Prefix` allows `sudo -n`. Errors include combined
  stdout/stderr and classify `ErrTCUnavailable` / `ErrPermission` /
  `ErrNoQdisc` / `ErrInvalidInterface`.
- Interface resolution: loopback → `lo` without invoking `ip`; otherwise parsed
  from `ip route get`.
- **Safety rule:** shaping is applied only when explicitly configured; a failed
  apply is recorded as `NetworkStatusUnavailable`, the run is not a valid
  measurement of that condition, and it is excluded from condition aggregates.
  Shaping is always cleared afterward (`ErrNoQdisc` tolerated). In the current
  container, `tc` requires privileges (`sudo -n`).

---

## 16. Experiment modes (`internal/experiment`)

`AllModes()` returns the **four baseline modes** in order: `ModeFixed`
(`"fixed"`), `ModeConcurrencyAdaptive` (`"concurrency-adaptive"`),
`ModeChunkSizeAdaptive` (`"chunk-size-adaptive"`), `ModeJointAdaptive`
(`"joint-adaptive"`). A deprecated alias `ModeAdaptive ==
ModeConcurrencyAdaptive` exists. `AllModesWithLinUCB()` appends the opt-in
`ModeLinUCB` (`"linucb"`); `IsLinUCBMode(mode)` tests for it.

Each mode differs **only** in `downloader.Config.Policy`; everything else is
identical within a scenario: file contents/size, server behavior, concurrency
bounds, chunk bounds/step, thresholds, window, retries, timeout, and network
condition (verified in `runOnce`):

| Mode | Policy |
|---|---|
| fixed | none |
| concurrency-adaptive | `policy.NewThroughputPolicy(...)` |
| chunk-size-adaptive | `policy.NewChunkSizePolicy(...)` |
| joint-adaptive | `policy.NewJointPolicy(...)` |
| **linucb (opt-in)** | `rl.NewLinUCB(...)` wrapped by `rl.NewAdapter(...)` |

### 16.1 LinUCB integration (opt-in fifth mode)

`ModeLinUCB` runs the full existing causal loop without any new downloader,
runner, worker pool, adapter, or reward path:

```
State_t -> rl.LinUCB.SelectAction -> ActionIndex -> rl.Adapter -> policy.Action
        -> policyRunner.applyAction -> controllers -> downloader executes
        -> Observation_{t+1} -> RewardCalculator -> Reward_t
        -> rl.Adapter.Update -> rl.LinUCB.Update(State_t, Action_t, Reward_t)
        -> next decision
```

- **Construction:** `runOnce` builds `rl.NewLinUCB(LinUCBConfig{Alpha:
  cfg.LinUCBAlpha, NumActions: rl.NumActions, Dimension: len(rl.FeatureNames()),
  InitialExplorationRounds: 1})`, wraps it with the **existing** `rl.NewAdapter`,
  and sets `dlCfg.Policy = adapter` plus `dlCfg.RewardConfig`. The action count
  and feature dimension are derived from the RL contract, not duplicated config.
  `InitialExplorationRounds: 1` is the §22.8 exploration correction.
- **Configuration:** only `ScenarioConfig.LinUCBAlpha` (default 0.2) and the
  existing `ScenarioConfig.RewardConfig` (defaults to
  `rl.DefaultRewardConfig()` when nil for LinUCB runs). No reward formula or
  weights were added or tuned.
- **Opt-in / default unchanged:** `Run(cfg)` and `RunMatrix` with no mode
  selection run only the four baseline modes. LinUCB requires
  `RunSelected(cfg, AllModesWithLinUCB())`, `MatrixConfig.Modes`, or the CLI
  `-modes linucb`. `ExperimentResult`, `Summary`, `ScenarioResult`,
  `ScenarioSummary`, and their `Runs`/`Aggregate` helpers carry a `LinUCB`
  field populated only when selected.
- **Failure handling:** an invalid LinUCB configuration fails validation and is
  returned as an error; there is no silent fallback to another policy.

`NetworkStatus` per run: `none` (no condition requested), `applied`,
`unavailable` (requested but tc/netem failed).

---

## 17. Experiment matrix (`internal/experiment/matrix.go`)

`DefaultMatrixConfig()` (verified values; modest local-test values, not an ISP
model):

| Scenario | Server rate | tc/netem |
|---|---|---|
| baseline | 4 MiB/s constant | none |
| server-throttle | 512 KiB/s → 4 MiB/s after 4 requests | none |
| latency | 4 MiB/s constant | latency 20 ms |
| loss | 4 MiB/s constant | loss 1% |
| combined | 1 MiB/s constant | latency 20 ms + loss 1% |

Common base: file 256 KiB, chunk 32 KiB, min/max chunk 32/128 KiB, initial
chunk 32 KiB, step 32 KiB, concurrency 1 (min 1, max 4), thresholds 0.2/0.2,
window 4, retries 3, timeout 30 s, repetitions 5.

`RunMatrix` runs each scenario × mode × repetition with **fresh** server,
output, downloader, metrics, policy, and controllers per run, and clears
network shaping per run. `MatrixConfig` allows overriding `Repetitions`, the
`NetworkController`, and `Modes` (nil → the four baseline modes; pass
`AllModesWithLinUCB()` to include the opt-in LinUCB mode).

Aggregation (`ModeAggregate`, valid runs only): `Runs`, `ValidRuns`,
`FailedRuns`, `NetworkUnavailableRuns`, mean/median/min/max duration,
`StdDevDuration`, `IQRDuration`, mean/median throughput, `StdDevThroughput`,
`IQRThroughput`, mean attempts/retries, mean final/max concurrency, mean
final/min/max chunk size, mean concurrency/chunk-size actions. Failed and
network-unavailable runs are excluded from these means. Text output is a
per-run table plus an aggregates table; `-json` serializes the full
`MatrixResult` (raw runs + trajectories + summaries).

---

## 18. Baseline dataset (`testdata/baseline/`)

Saved four-mode matrix outputs (not regenerated during this documentation task):

| File | Contents |
|---|---|
| `matrix_run1.json` | full `MatrixResult`, run 1 |
| `matrix_run2.json` | full `MatrixResult`, run 2 (same config) |
| `matrix_text.txt` | text summary of a reps=10 run |

Verified structure of `matrix_run1.json`:

- 5 scenarios × 4 modes × 10 repetitions = **200 runs**.
- All runs `ValidRuns` (no failed / unavailable runs recorded in the saved file).
- Network statuses: baseline/server-throttle `none`; latency/loss/combined
  `applied`.
- Each run stores raw fields including `Scenario`, `Mode`, `Repetition`,
  `Duration`, attempt counts, throughput, initial/final/max concurrency,
  initial/final/min/max chunk size, `ConcurrencyActions`, `ChunkSizeActions`,
  `OutputMatches`, `NoDuplicateRanges`, `NoMissingRanges`,
  `NetworkConditionStatus`, the raw `ConcurrencyTrajectory` /
  `ChunkSizeTrajectory`, and `Err`.
- Aggregates include the variability fields described in §17.

These are the current **baseline measurements**, preserved for future
comparison. They are not evidence that any mode is superior and should not be
interpreted as such.

A separate **5-mode formal benchmark** dataset (adding the opt-in LinUCB mode)
lives in `testdata/benchmark/` (v1, pre-exploration-correction),
`testdata/benchmark_v2/` (post-correction, 256 KiB, §22.9), and
`testdata/benchmark_v3/` (post-correction, 4 MiB horizon, §22.11). They are kept
separate from this four-mode baseline and from `testdata/calibration/`, and none
overwrites this dataset.

---

## 19. Control-trajectory semantics

The recording policy appends one event per **policy decision**, keyed by request
index (`Context.TotalAttempts`):

- `ConcurrencyTrajectory []ConcurrencyEvent{RequestIndex, Concurrency, Action string}`
- `ChunkSizeTrajectory []ChunkSizeEvent{RequestIndex, ChunkSize, Action policy.ActionKind}`

Interpretation:

- `Action == "none"` / `ActionKind == ActionNone` → **observation event**, no
  controller change.
- `Action != "none"` → **actual control action** applied to the corresponding
  controller.
- `ConcurrencyActions` / `ChunkSizeActions` count only applied actions
  (increase/decrease) for their dimension.

Consequences: a mode that never changes chunk size still has a *constant*
chunk-size trajectory; fixed mode has empty trajectories. The recorded value is
the controller target at decision time (post-change value is observed in the
next event / final value).

---

