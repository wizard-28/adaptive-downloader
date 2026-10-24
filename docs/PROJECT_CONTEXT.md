# PROJECT_CONTEXT.md

**Living project handoff document.** This document describes the current state of
the repository: architecture, implemented functionality, invariants, completed
milestones, experiment status, RL status, known caveats, and the immediate next
step. It is maintained as project state, not as a log of any conversation.

> **Source of truth (read in this order)**
>
> 1. **Source code and tests** — the authoritative implementation. If this
>    document conflicts with code or tests, the code/tests win.
> 2. **`docs/PROJECT_CONTEXT.md`** — current architectural and research handoff
>    (this file).
> 3. **`AGENTS.md`** — persistent instructions for coding agents.
>
> If this document becomes stale, inspect the repository, determine the actual
> behavior, and update this file. Never invent behavior from documentation alone.

`AGENTS.md` requires that this file be updated after every substantive project
change (`PROJECT_CONTEXT.md updated: yes`).

---

## 1. Project overview

**Module:** `adaptivedl` (see `go.mod`; Go 1.27.1). The repository has no
separate marketing name beyond the module and the `cmd/` command names
(`downloader`, `experiment`).

**Objective.** Build and evaluate an adaptive HTTP range download manager that
adjusts two control dimensions during a download:

1. **concurrency** — the number of simultaneously active HTTP range requests;
2. **future chunk size** — the byte size used for *new* range allocations.

**Research problem.** Under volatile, server-side-throttled and
network-constrained ("consumer") conditions, how should a client schedule
concurrency and future request granularity to make good use of the available
bandwidth, without disrupting in-flight work?

**Intended contribution (current framing).** A controlled, instrumented
environment in which deterministic rule-based baselines (fixed,
concurrency-only, chunk-size-only, joint) and, later, a learned policy can be
compared on the *same* scenarios with the *same* reward definition. The project
does **not** claim that joint adaptation of concurrency and chunk size is
universally novel; it studies this specific client-side setting with a
reproducible harness and makes no superiority claim from small tests.

**Target environment / use case.** Single-source, single-server, consumer-style
HTTP downloads where the client uses **HTTP Range requests**, transfer is
subject to server-side throttling and shared/contended bandwidth, and latency
and packet loss are relevant. Client-side adaptation must operate in a
request/response environment where a request, once started, should not be
cancelled merely because the target changed.

**Current technical scope (verified in code).**

- HTTP `Range` requests; one request fetches exactly one contiguous byte range
  (`internal/fetcher`).
- Single-server, HTTP/1.1-style request/response downloads through Go's
  `net/http`.
- Adaptive concurrency and adaptive future chunk size, driven by deterministic
  policies or (contract-ready only) an RL adapter.

**Explicitly excluded scope.** There is no HTTP/2, multiplexing, multi-source /
multi-mirror / CDN-aware downloading, connection pooling heuristics,
  resume-from-disk, retry of the whole file, server-push, or parity/checksum
  verification. An external **aria2c baseline** is now measured (§22.16); IDM
  is **not** evaluated (Windows-only, no Linux path). The only external Go
  dependency is Gonum, used strictly for LinUCB's dense linear algebra
  (§3.2).

---

## 2. Research positioning

- **Established prior work (context, not implemented here):** adaptive
  concurrency and adaptive chunk/segment sizing for parallel downloads are
  well-established ideas; many download managers tune them. This project does
  not claim to have invented them.
- **Project research question:** in a client-observed, Range-based,
  single-source download under server throttling and network impairments, what
  are clean, measurable control policies for concurrency and *future* chunk
  size, and what would a learned (contextual-bandit) policy need to observe and
  optimize to be comparable to deterministic baselines?
- **Current implementation:** deterministic policies only
  (`FixedPolicy`, `ThroughputPolicy`, `ChunkSizePolicy`, `JointPolicy`), an
  experiment harness, and an RL *contract* (state/action/reward/adapter) with
  no learner.
- **Future hypotheses (not yet tested):** that a contextual-bandit learner over
  a finite action space, using the defined state and reward contract, can match
  or exceed the deterministic joint baseline on the baseline scenarios. This is
  a hypothesis to be tested, not a result.

---

## 3. Repository structure

```
.
├── AGENTS.md                      # persistent agent workflow instructions
├── docs/
│   └── PROJECT_CONTEXT.md         # this living handoff document
├── go.mod                         # module adaptivedl, Go 1.27.1
├── cmd/
│   ├── downloader/main.go         # thin CLI over internal/downloader
│   ├── experiment/main.go         # single-scenario + matrix experiment CLI
│   ├── calibration/main.go        # LinUCB calibration/reward diagnostics CLI
│   ├── linucb-diagnostic/main.go  # separate forced-action exploration diagnostic
│   ├── linucb-horizon/main.go     # separate learning-horizon study CLI
│   ├── aria2-benchmark/main.go    # external aria2c baseline measurement CLI
│   ├── aria2-wallclock/main.go    # aria2c under wall-clock throttle CLI
│   ├── granularity-study/main.go  # granularity sensitivity study CLI (5 fixed + wallclock)
│   ├── wallclock-throttle/main.go # wall-clock throttle six-mode experiment CLI
│   ├── dqn-transfer/main.go       # frozen-DQN real-downloader transfer validation
│   ├── dqn-live-v5/main.go        # Candidate v5 live transfer validation (fixed C=1..4 + v5)
│   ├── final-evaluation-v5/main.go # FINAL v5 vs aria2-v2 matched-pair head-to-head
│   ├── dqn-live-v6/main.go        # Candidate v6 live transfer under dynamic trajectories
│   ├── final-evaluation-v6/main.go # FINAL pre-registered v6 vs aria2-F (trajectories A-E)
│   └── aria2-calibration/main.go  # aria2 baseline selection on a validation set
├── internal/
│   ├── rangetracker/              # byte-range partition + lifecycle (half-open)
│   ├── fetcher/                   # one HTTP Range request -> Result
│   ├── assembler/                 # random-access writes of completed ranges
│   ├── metrics/                   # per-attempt records + aggregate snapshot
│   ├── concurrency/               # concurrency target controller
│   ├── chunksize/                 # chunk-size target controller
│   ├── downloader/                # config, worker pool, policy runner
│   ├── policy/                    # Policy interface + deterministic policies
│   ├── experiment/                # four-mode runner + matrix
│   │   ├── server/                # controllable HTTP range test server
│   │   └── network/               # tc/netem abstraction
│   ├── diagnostics/               # separate, labeled diagnostic experiments
│   └── rl/                        # RL contract: state/action/reward + adapter
│       │                          #   + LinUCB learner (linucb.go, Gonum-based)
│       └── dqn/                   # sequential Double DQN contract + Go inference
├── python/                        # training-only (CPU PyTorch); not needed at inference
│   ├── rl/                        # state/sim/DQN/model (v1-v5; sim_env_v5/state_v5)
│   ├── train_dqn.py               # sanity-training CLI
│   ├── train_validate_v5.py       # Candidate v5 training/validation pipeline
│   └── tests/                     # Python unit tests
└── testdata/
    ├── baseline/                  # saved four-mode baseline matrix outputs
    ├── calibration/               # LinUCB calibration/reward diagnostics
    ├── benchmark/                 # 5-mode formal benchmark v1 (pre-correction)
    ├── benchmark_v2/              # 5-mode formal benchmark v2 (post-correction)
    ├── benchmark_v3/              # 5-mode formal benchmark v3 (4 MiB horizon)
    ├── benchmark_v4/              # live standard vs periodic LinUCB benchmark
    ├── benchmark_v5/              # final six-mode 4 MiB benchmark
    ├── benchmark_aria2/           # aria2c external baseline (4 MiB)
    ├── benchmark_aria2_v2/        # aria2c under wall-clock throttle
    ├── benchmark_v6/              # wall-clock throttle, six modes (4 MiB)
    ├── diagnostics/               # separate exploration-diagnostic outputs
    └── final_research_audit/      # deterministic synthesis of the frozen chain
```

`testdata/diagnostics/dqn/` holds the DQN sanity-training artifacts (labeled
DEVELOPMENT / SANITY MODEL).

Package imports show `internal/rl` depends on `internal/policy`, Gonum, and the
standard library; `internal/downloader` depends on the small packages plus
`internal/rl` (for reward computation), not on `experiment`.

### 3.1 Dependency policy

The project prefers a **focused, mature third-party library over
project-maintained generic infrastructure**, provided the dependency materially
improves correctness, maintainability, reliability, or implementation quality.
The boundary is kept narrow: a dependency is an implementation detail where
possible, its types do not leak across packages, and **project-specific research
and domain logic stays project-owned** (range semantics, downloader, worker
pool, controllers, policies, reward design, RL contracts, and the LinUCB
algorithm itself). See `AGENTS.md` §15a for the full policy.

### 3.2 Direct dependencies

| Dependency | Version | Purpose | License |
|---|---|---|---|
| `gonum.org/v1/gonum` | v0.17.0 | Dense numerical linear algebra for LinUCB (`mat.Cholesky`, `mat.SymDense`, `mat.SymRankOne`, `mat.Dot`) | BSD-3-Clause (Gonum License) |

Gonum is the **only** direct dependency; it has **zero transitive
dependencies** (`go.sum` contains only Gonum). It was selected because the
project previously maintained its own Cholesky / quadratic-form / matrix-inverse
infrastructure (`internal/rl/linalg.go`), which is generic numerical code and
not part of the research contribution. Gonum is actively released (v0.17.0,
2026-01-10; six-month cadence aligned with Go releases), well documented, and
compatible with the project's Go version. The experiment's descriptive
statistics, CLI, config, logging, HTTP, and JSON handling remain **standard
library / project-owned** — no frameworks were introduced.

---

## 4. Architecture

```
                          cmd/downloader/main.go
                                   │
                                   ▼
                       internal/downloader.Downloader
                                   │
      ┌───────────────┬───────────┼───────────────┬────────────────┐
      ▼               ▼           ▼               ▼                ▼
 RangeTracker     workerPool   Assembler       Metrics       policyRunner
 (half-open)          │       (inclusive)   (per-attempt)         │
                      │                                            │
                      ▼                                            ▼
                   Fetcher                              concurrency.Controller
                 (inclusive)                             chunksize.Controller
                      │
                      ▼
                 HTTP Range request (single server)
```

- **`internal/rangetracker`** — owns the byte-space partition and the
  pending / in-flight / completed lifecycle. Half-open ranges.
- **`internal/fetcher`** — performs exactly one HTTP Range request per call and
  returns bytes plus timing/throughput/status metadata. Inclusive ranges.
- **`internal/assembler`** — writes completed ranges at their exact offsets with
  `WriteAt`; creates/truncates and extends the output file to its final size.
- **`internal/metrics`** — thread-safe collector of one `Record` per attempt
  and an `Aggregate`/`Snapshot`.
- **`internal/concurrency`** — concurrency target controller (min/max/current,
  Increase/Decrease/Set). Represents intent only; creates no goroutines.
- **`internal/chunksize`** — chunk-size target controller (min/max/initial/step).
  Affects future allocations only; never resizes existing ranges.
- **`internal/downloader`** — `Downloader`, `workerPool`, `policyRunner`.
- **`internal/policy`** — `Policy` interface, `Context`, `Action`/`ActionKind`,
  and the deterministic policies.
- **`internal/experiment`** — four-mode runner and matrix.
- **`internal/experiment/server`** — controllable HTTP Range test server.
- **`internal/experiment/network`** — `tc`/`netem` abstraction.
- **`internal/rl`** — RL state/action/reward contract, reward calculator, the
  adapter to the existing `policy.Policy`, and the LinUCB learner (numeric
  linear algebra via Gonum).

---

## 5. Download data flow

Per attempt, inside `workerPool.worker` → `processRange`:

1. A worker reads the **current chunk size** (`d.currentChunkSize()`) and calls
   `RangeTracker.Allocate(chunkSize)` (half-open `[Start, End)`).
2. If no range is available, the worker stops.
3. The worker converts to inclusive bounds (`end := r.End - 1`) and calls
   `Fetcher.Fetch(ctx, reqID, URL, r.Start, end)`.
4. The `fetcher.Result` is recorded into `metrics.Collector.Record(rec)` (one
   record per attempt, success or failure).
5. If a `policyRunner` is configured, `policyRunner.observe(rec)` runs one
   serialized policy decision (see §11).
6. **Success:** `Assembler.WriteRange(start, end, data)` then
   `RangeTracker.Complete(r)`; the worker loops.
7. **Failure:** `processRange` retries the **same** range in place with its
   original boundaries until success or the retry limit; on exceeding the limit
   it sets a fatal error and cancels the run.
8. A failed range is not requeued through `RangeTracker.Fail` during normal
   operation (see §8 for why); `Fail` remains available and is tested.

Chunk-size decisions affect step 1 (future allocations). Concurrency decisions
affect how many workers exist (not any in-flight request).

---

## 6. Range semantics (CRITICAL)

Verified from `internal/rangetracker/rangetracker.go`,
`internal/fetcher/fetcher.go`, `internal/assembler/assembler.go`, and
`internal/downloader/worker_pool.go`:

| Component | Convention |
|---|---|
| `RangeTracker` | **half-open** `[Start, End)`, `Length = End-Start` |
| `Fetcher` | **inclusive** `[start, end]` (HTTP `bytes=start-end`) |
| `Assembler` | **inclusive** `[start, end]` |
| `Metrics` | **inclusive** `RangeStart`/`RangeEnd` |
| Downloader | converts: `end := r.End - 1` |

Tracker state:

- `pending` — sorted by `Start`, disjoint, non-adjacent (adjacent ranges are
  merged).
- `inFlight` — a set of allocated-but-unfinished ranges; disjoint from pending
  and from each other.
- `completed` — sorted by `Start`, adjacent/overlapping merged;
  `completedBytes` equals the sum of completed lengths.
- `Allocate(chunkSize)` takes up to `chunkSize` bytes from the **front** of
  pending, clamps to the remaining bytes, never overlaps, never exceeds
  `TotalSize`.
- `Complete(r)` merges `r` into completed; `Fail(r)` returns `r` to pending and
  merges with neighbors. Both return `ErrRangeNotInFlight` for unknown ranges.
- `Done()` is true when pending and in-flight are both empty.

---

## 7. Critical retry / in-flight invariants

Verified from `worker_pool.processRange` and the chunk-size controller doc:

- **An allocated range is immutable.** Once `Allocate` returns, its boundaries
  are fixed for the whole attempt/retry cycle.
- **Changing the chunk-size target affects future allocations only.** A future
  `Allocate` reads `Current()`; an already-handled range is unaffected.
- Example: if the target is 32 KiB and `[0, 32767]` was allocated, increasing
  the target to 64 KiB must **not** modify that range. If it fails, the retry
  remains `[0, 32767]` (inclusive) and must not be resized or re-split.
- **Why:** a request represents a real in-flight transfer; silently resizing or
  re-splitting it would waste work, break range accounting, and risk overlap
  gaps. Retrying in place keeps `Metrics`/assembler offsets stable.
- **In-place retry:** `processRange` loops on the same `r` (bounded by
  `MaxRetries`). This deliberately avoids `RangeTracker.Fail` re-allocation,
  which merges adjacent pending ranges and could otherwise re-split a failed
  range at a changed target size. `RangeTracker.Fail` is still exercised by its
  own unit tests.

---

## 8. Controllers

### 8.1 Concurrency controller (`internal/concurrency`)

```go
func New(min, max int) (*Controller, error)   // ErrInvalidConfig unless min >= 1, max >= min; target starts at min
func (c *Controller) Current() int
func (c *Controller) Min() int
func (c *Controller) Max() int
func (c *Controller) Increase() int           // +1, capped at max
func (c *Controller) Decrease() int           // -1, capped at min
func (c *Controller) Set(target int) int       // clamped to [min,max]
```

- Mutex-guarded (`sync.Mutex`); safe for concurrent use.
- Represents the desired number of workers; it creates/destroys no goroutines.
- **Increasing:** the pool's `SetTarget`/reconcile spawns more workers (each
  takes part in normal `RangeTracker.Allocate`).
- **Decreasing:** workers are **not** killed mid-request; excess workers retire
  between ranges (drain). Unstarted work stays pending in the tracker.

### 8.2 Chunk-size controller (`internal/chunksize`)

```go
func New(min, max, initial, step int64) (*Controller, error) // min>0, max>=min, initial in [min,max], step>0
func (c *Controller) Current() int64
func (c *Controller) Min() int64
func (c *Controller) Max() int64
func (c *Controller) Step() int64
func (c *Controller) Increase() int64   // +step, capped at max
func (c *Controller) Decrease() int64   // -step, capped at min
func (c *Controller) Set(target int64) int64 // clamped to [min,max]
```

- Mutex-guarded; safe for concurrent use.
- Represents the size for **future** allocations only; never touches in-flight,
  completed, or tracker internals.

---

## 9. Worker pool (`internal/downloader/worker_pool.go`)

- Fields: `target`, `active`, a buffered `wake` channel, and a `sync.WaitGroup`.
- A single manager goroutine (`run`) reconciles active workers toward `target`;
  it wakes on a coalescing non-blocking signal (target change, worker exit,
  failed-range requeue), not a timer.
- `reconcile` spawns workers only while `active < target` **and**
  `PendingBytes() > 0`, and reports completion when `active == 0` and no
  pending bytes remain. This prevents idle spawn/exit spinning.
- Workers read the current chunk size lazily at each allocation and self-retire
  between ranges when `active > target` (draining).
- **Cancellation:** on `ctx.Done()` the manager waits for workers, then the run
  returns the fatal error or context error. In-flight requests use the derived
  run context and observe cancellation.

---

## 10. Policy interface (`internal/policy`)

```go
type Policy interface {
    SelectAction(ctx Context) Action
    Update(ctx Context, action Action, reward float64)
}

type ActionKind uint8
const (
    ActionNone ActionKind = iota
    ActionIncreaseConcurrency      // +1 worker
    ActionDecreaseConcurrency      // -1 worker
    ActionIncreaseChunkSize        // larger future chunks
    ActionDecreaseChunkSize        // smaller future chunks
    ActionSetConcurrency           // RESERVED (explicit target in Action.Value)
    ActionSetChunkSize             // RESERVED (explicit target in Action.Value)
)
type Action struct { Kind ActionKind; Value int64 }
```

`Context` fields:

| Field | Meaning |
|---|---|
| `Concurrency` | current concurrency target |
| `ChunkSize` | current chunk-size target |
| `Throughput` | windowed recent throughput (bytes/sec) |
| `RequestDuration` | duration of the most recent request |
| `BytesTransferred` | bytes of the most recent request |
| `SuccessfulRequests` | cumulative successes |
| `FailedRequests` | cumulative failures |
| `Retries` | cumulative retries (attempt > 1) |
| `TotalAttempts` | cumulative attempts |

Implemented actions: `ActionNone`, `ActionIncreaseConcurrency`,
`ActionDecreaseConcurrency`, `ActionIncreaseChunkSize`,
`ActionDecreaseChunkSize`. `ActionSetConcurrency` and `ActionSetChunkSize` are
**reserved**: constructible/valid but **ignored** by `policyRunner.applyAction`.

---

## 11. Policy runner (`internal/downloader/policy_runner.go`)

One serialized path per configured run (the policy is not concurrency-safe):

```
observe(rec)                       [holds mu]
  -> update counters (successful/failed/retries/attempts)
  -> update recent window (default window 8; Config.PolicyWindowSize)
  -> throughput = sum(window bytes) / sum(window durations)
  -> build policy.Context
  -> policy.SelectAction(ctx)
  -> applyAction(action)           // concurrency and/or chunk-size controller
  -> policy.Update(ctx, action, 0) // placeholder reward
```

- **Serialization:** the whole sequence holds one mutex, so `SelectAction` is
  never called concurrently.
- **Recent throughput:** byte-weighted over the last `window` attempts;
  deliberately not the lifetime aggregate.
- **Action application (generic `ActionKind`, no policy-type special cases):**
  - `ActionIncreaseConcurrency`/`ActionDecreaseConcurrency` → concurrency
    controller, then `apply(controller.Current())` notifies the pool.
  - `ActionIncreaseChunkSize`/`ActionDecreaseChunkSize` → chunk-size controller
    only. The pool reads the target lazily at each allocation, so no
    notification is needed. No-op when no adaptive chunk controller is set.
  - `ActionSetConcurrency`/`ActionSetChunkSize` → ignored (reserved).
- Each `ActionKind` affects **only** its corresponding controller.
- Chunk-size changes affect future allocations only.

---

## 12. Existing policies (`internal/policy`)

### 12.1 `FixedPolicy`
`SelectAction` always returns `ActionNone`; `Update` is a no-op. Baseline.

### 12.2 `ThroughputPolicy` (`throughput.go`)
- Config: `MinConcurrency`, `MaxConcurrency`, `ImprovementThreshold` (relative),
  `DegradationThreshold` (relative).
- First call establishes `previous` throughput and returns `ActionNone`.
- Otherwise `change = (current - previous) / previous`:
  - `change >= ImprovementThreshold` → `ActionIncreaseConcurrency` (unless at
    max);
  - `change <= -DegradationThreshold` → `ActionDecreaseConcurrency` (unless at
    min);
  - else `ActionNone`.
- Zero-throughput semantics: `0→0` no change; `0→positive` increase (unless at
  max); `positive→0` decrease (unless at min). No division by zero.
- Only concurrency actions; chunk size untouched.

### 12.3 `ChunkSizePolicy` (`chunksize.go`)
- Config: `MinChunkSize`, `MaxChunkSize`, `ImprovementThreshold`,
  `DegradationThreshold`.
- Same relative-throughput rule as `ThroughputPolicy`, but emits
  `ActionIncreaseChunkSize` / `ActionDecreaseChunkSize` (bounds checked against
  `ctx.ChunkSize` and config min/max).
- Only chunk-size actions; concurrency untouched.

### 12.4 `JointPolicy` (`joint.go`)
- Config: `MinConcurrency`, `MaxConcurrency`, `MinChunkSize`, `MaxChunkSize`,
  `ImprovementThreshold`, `DegradationThreshold`.
- State: `havePrevious`, `previous` throughput, and `preferConcurrency`.
- Same relative-throughput improvement/degradation detection as above.
- **Deterministic alternating dimension preference:** each *actionable* decision
  targets the preferred dimension (initially concurrency), then flips; a
  decision that hits a bound falls back to the other dimension; if both are at a
  bound it returns `ActionNone` and the preference does not advance.
- `Context.Concurrency` and `Context.ChunkSize` are authoritative (no second
  copy).
- `Update` is a no-op (no reward).

**JointPolicy is a deterministic rule-based baseline, not an RL algorithm.**

---

## 13. Metrics (`internal/metrics`)

```go
type Record struct {
    RequestID  string
    RangeStart int64
    RangeEnd   int64   // inclusive
    Bytes      int64
    Duration   time.Duration
    Throughput float64 // bytes/sec
    Success    bool
    StatusCode int
    Attempt    int     // 1 for first attempt, 2 for first retry, ...
}
type Aggregate struct {
    TotalBytes, TotalRequests, SuccessfulRequests, FailedRequests, TotalAttempts int64
    TotalDuration time.Duration
    AverageThroughput, MinThroughput, MaxThroughput float64
}
type Snapshot struct { Records []Record; Aggregate Aggregate }
func New() *Collector
func (c *Collector) Record(r Record)
func (c *Collector) Snapshot() Snapshot
```

- **Invariant: every attempt produces exactly one Record.** The worker calls
  `collector.Record(rec)` once per `Fetcher.Fetch` result, before any success/
  failure branch, so failures, timeouts, cancellations, and incomplete
  responses are recorded too.
- `TotalBytes` counts bytes read on all attempts (including error bodies);
  `AverageThroughput`/`Min`/`Max` are computed from **successful** attempts only
  (`successBytes / successElapsed`).
- `Snapshot` deep-copies records and rebuilds the aggregate; collector is
  mutex-guarded. Zero value is usable.

---

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

## 20. RL infrastructure (`internal/rl`)

**Contract + first learner (disjoint LinUCB).** Depends on `internal/policy` + stdlib.
See §20.6 for LinUCB; the rest of this section is the contract it fits into.

### 20.1 State (`state.go`)
```go
type State struct {
    Concurrency         int
    ChunkSize           int64
    Throughput          float64       // per-request, windowed
    AggregateThroughput float64       // wall-clock aggregate, windowed
    RequestDuration     time.Duration
    BytesTransferred    int64
    SuccessfulRequests  int64
    FailedRequests      int64
    Retries             int64
    TotalAttempts       int64
}
func StateFromContext(ctx policy.Context, cfg Config) (State, error)
func (s State) Validate() error
func FeatureNames() []string
func (s State) Features(cfg Config) ([]float64, error)
```
- Derived only from the existing `policy.Context`; deterministic; validates
  config (`Config{MinConcurrency, MaxConcurrency, MinChunkSize, MaxChunkSize,
  MaxThroughput, MaxRequestDuration, CounterScale}`) and rejects negatives /
  NaN / Inf. `State` is **raw**.
- `Throughput` is the windowed **per-request** rate; `AggregateThroughput` is the
  windowed **wall-clock aggregate** rate (see §20.7, §22.5). Both are kept;
  neither replaces the other.

### 20.2 Normalization
Normalization is explicit via `Features(cfg)` and is **not** applied silently.
Linear against `Config` bounds, clamped to `[0,1]`; zero denominator (e.g.
min == max) yields 0. Feature order (`FeatureNames`, now 10 features):
`concurrency`, `chunk_size`, `throughput`, `aggregate_throughput`,
`request_duration`, `bytes_transferred`, `successful_requests`,
`failed_requests`, `retries`, `total_attempts`. `MaxThroughput` is the shared
reference rate for both throughput features (the aggregate may exceed it and
clamps to 1). Dimension is derived from `len(FeatureNames())` (now **10**).

### 20.3 ActionIndex (`action.go`)
```
0 -> policy.ActionNone
1 -> policy.ActionIncreaseConcurrency
2 -> policy.ActionDecreaseConcurrency
3 -> policy.ActionIncreaseChunkSize
4 -> policy.ActionDecreaseChunkSize
```
`NumActions = 5`. `ActionFromIndex` / `ActionIndexFromAction` are deterministic
and round-trip for supported actions; invalid indices return
`ErrInvalidActionIndex`; the reserved `ActionSetConcurrency`/`ActionSetChunkSize`
return `ErrUnsupportedAction` (not part of the learning action space).

### 20.4 Reward contract (`reward.go`)
```go
type RewardComponents struct { Throughput, ThroughputImprovement, LatencyCost, RetryCost, FailureCost, ActionCost float64 }
type Reward struct { Value float64; Components RewardComponents }
```
`Value` and every component must be finite (`ErrInvalidReward` on NaN/±Inf).
This is the *data contract*; the computation lives in `RewardCalculator`
(§20.7). LinUCB consumes only `Reward.Value`.

### 20.7 Reward computation (`reward_calculator.go`) — CURRENT REWARD BASELINE

```go
type RewardConfig struct {
    // normalizers
    ThroughputScale, LatencyScale float64
    RetryPenalty, FailurePenalty, ActionCost float64
    // weights
    WThroughput, WImprovement, WLatency, WRetry, WFailure, WAction float64
}
func DefaultRewardConfig() RewardConfig
func (c RewardConfig) Validate() error
type RewardCalculator struct { ... }
func NewRewardCalculator(cfg RewardConfig) (*RewardCalculator, error)
func (c *RewardCalculator) Calculate(previous, current State, action ActionIndex, havePrevious bool) Reward
```

**Components.** `RewardComponents` now carries six components (one added:
`ThroughputImprovement`):

- `Throughput` (benefit, absolute): `current.AggregateThroughput / ThroughputScale`.
  Uses the **aggregate** (wall-clock) throughput observable, so a stable
  high-throughput state scores above a stable low-throughput state *and* the
  term responds to concurrency (a per-request signal would not; see §22.5).
- `ThroughputImprovement` (benefit, signed): `(current.AggregateThroughput −
  previous.AggregateThroughput) / ThroughputScale`. Rewards the *change* in
  aggregate throughput (adaptation), and is negative on a regression. First
  observation → 0 (`havePrevious == false`).
- `LatencyCost`: `clamp01(current.RequestDuration.Seconds() / LatencyScale)` —
  normalized to `[0,1]` so one slow request cannot dominate.
- `RetryCost`: `(current.Retries − previous.Retries) * RetryPenalty`, clamped
  at ≥ 0 (counter-reset safe).
- `FailureCost`: `(current.FailedRequests − previous.FailedRequests) * FailurePenalty`,
  clamped at ≥ 0. Retries and failures are counted from separate metrics
  counters, so a retried failed attempt is not double-counted.
- `ActionCost`: `ActionCost` **only if the action actually changed the
  corresponding controller target** (state-change based), else 0. `ActionNone`
  is never charged, and no action is charged on the first observation
  (`havePrevious == false`). This is a real control-churn cost: a no-op or a
  clamped (no-effect) action costs nothing. There is no arbitrary "good/bad"
  action ranking.

`ThroughputScale` normalizes **both** the absolute and the improvement term
(name kept for continuity; semantics documented here, not changed silently).

**Formula (sign convention: benefits positive, costs non-negative and subtracted):**

```
Value = WThroughput*Throughput + WImprovement*ThroughputImprovement
        − WLatency*LatencyCost − WRetry*RetryCost
        − WFailure*FailureCost − WAction*ActionCost
```

`Reward.Value` is exactly this combination of `Reward.Components`. The formula
is written in one place in `Calculate` for auditability. Invalid inputs
(non-finite state, wrong shape) yield `Reward{}` (zero) rather than a corrupted
reward.

**Configuration.** Explicit and validated: all fields finite and ≥ 0; a
non-zero weight on a component requires a non-zero scale
(`ErrInvalidRewardConfig`). `DefaultRewardConfig()` is a modest baseline
(`ThroughputScale = 1<<20`, `LatencyScale = 1.0`, `RetryPenalty = 0.5`,
`FailurePenalty = 1.0`, `ActionCost = 0.05`, all weights `1.0` except
`WLatency = WRetry = 0.5`); it is **not** claimed to be optimal. The downloader
exposes it via `downloader.Config.RewardConfig *rl.RewardConfig` (nil → reward
stays 0 for backward compatibility).

**Why separate from LinUCB.** The learner must not know how throughput, latency,
retries, failures, or churn are turned into a reward. The pipeline is
`Metrics/Context → RewardCalculator → Reward → LinUCB(Value)`. Reward shaping can
change without touching the learner.

**Fairness is intentionally NOT in the reward.** The repository measures no
background traffic, competing flows, link utilisation, RTT inflation, or share
of bandwidth, so no defensible fairness signal exists. `ActionCost` is a churn
cost, **not** fairness. Fairness is left as a future extension requiring a new
measurable signal.

### 20.5 Adapter (`policy.go`)
```go
type Policy interface {
    SelectAction(state State) ActionIndex
    Update(state State, action ActionIndex, reward Reward)
}
func NewAdapter(p Policy, cfg Config) (*Adapter, error)
type AlwaysActionPolicy struct { Action ActionIndex } // test-only, no learning
```
`Adapter` implements the existing `policy.Policy`:
`SelectAction(ctx)`: `StateFromContext → Policy.SelectAction → ActionFromIndex →
policy.Action` (falls back to `NoChange` on conversion error, since the
downloader interface returns no error). `Update(ctx, action, reward)`:
`StateFromContext + ActionIndexFromAction → Policy.Update(..., Reward{Value:
reward})`. `Adapter.Last()` returns the most recent `(State, ActionIndex)`.
`AlwaysActionPolicy` always returns its configured action and is used only to
exercise the adapter.

### 20.6 LinUCB learner (`linucb.go`)

Disjoint LinUCB contextual bandit implementing the existing `rl.Policy`
interface; **deterministic**. Dense linear algebra is delegated to Gonum's
`mat` package (see §20.8); the bandit algorithm remains project-owned.

```go
type LinUCBConfig struct {
    Alpha                    float64
    NumActions               int
    Dimension                int
    InitialExplorationRounds int // 0 = disabled (default); 1 = one warm-up pass
    ExplorationInterval      int // 0 = disabled (default); N = force 1 action per N normal decisions
}
func NewLinUCB(cfg LinUCBConfig) (*LinUCB, error)
func (l *LinUCB) SelectAction(state State) ActionIndex
func (l *LinUCB) Update(state State, action ActionIndex, reward Reward)
func (l *LinUCB) NumActions() int
func (l *LinUCB) Dimension() int
func (l *LinUCB) Alpha() float64
```

**Initial exploration (warm-up).** `InitialExplorationRounds` (validated `>= 0`,
default `0`) optionally makes the learner take that many deterministic passes
over the action space in `ActionIndex` order before normal UCB selection. One
pass guarantees every arm receives at least one update. See §22.8.

**Periodic exploration.** `ExplorationInterval` (validated `>= 0`, default `0`)
optionally forces one deterministic exploratory action after every N normal
decisions, cycling the arms in `ActionIndex` order with a separate cursor. It is
an experimental correction for the post-warm-up lock-in and is **not** active in
the experiment path by default. See §22.13.

**Model.** Per action `a`, maintain `A_a` (d×d, initialised to `I`) and `b_a`
(d, initialised to `0`); `θ_a = A_a⁻¹ b_a`. For a normalized context `x`:

```
p_a = θ_aᵀ x + alpha * sqrt(xᵀ A_a⁻¹ x)      // select argmax_a p_a
```

**Feature handling.** LinUCB consumes the existing normalized feature vector
produced by `State.Features(...)`; it does **not** re-implement normalization.
`LinUCBConfig.Dimension` must equal `len(State.Features(...))`
(= `len(FeatureNames())` = 10). A mismatched dimension or a non-finite/ malformed
feature vector makes `SelectAction` fall back to `ActionNone` and `Update` a
no-op, so the model is never corrupted. The learner uses a fixed internal
normalization `Config` (bounds in `defaultFeatureConfig`) identical in shape to
the adapter/experiment bounds.

**Action space.** The existing finite `ActionIndex` space (`NumActions = 5`);
pass `NumActions` for the canonical space. No `SetConcurrency`/`SetChunkSize`
arms exist. Invalid `ActionIndex` values are ignored by `Update`.

**Reward.** `Update` uses only `reward.Value`:

```
A_a ← A_a + x xᵀ
b_a ← b_a + r x
```

`RewardComponents` are intentionally **not** consumed here; reward shaping
belongs to the experiment/integration layer. Non-finite rewards (NaN/±Inf) are
rejected by `Reward.Validate` before touching the model.

**Tie-break.** Exact ties resolve to the **lowest `ActionIndex`** (strict `>`
comparison during argmax), making selection reproducible.

**Numerical approach.** Dense linear algebra is delegated to **Gonum**
(`gonum.org/v1/gonum/mat`): per-arm sufficient statistics are stored as
`*mat.SymDense` (A_a, SPD, start as identity) and `*mat.VecDense` (b_a, start
zero). `score` factorises A_a with `mat.Cholesky`, solves `A_a θ_a = b_a`
(`SolveVecTo`) and evaluates `xᵀ A_a⁻¹ x` through the factor; `updateArm`
applies `mat.SymRankOne` (A_a + x xᵀ) and a scaled vector add (b_a + r x). The
former hand-written `linalg.go` (Cholesky solver, quadratic form, dot, finite
checks, Sherman–Morrison inverse maintenance) was **removed**; the factorisation
is recomputed per decision, which is more numerically robust and is cheap at the
feature dimension used here (d = 10). Project-owned parts remain in `linucb.go`:
feature extraction, dimension validation, `allFinite`, tie-breaking, score
formula, action validation, and the `Policy` implementation. Handles
`alpha = 0`, negative/zero rewards, and 1-dimensional contexts.

**Tests** (`linucb_test.go`, `linucb_integration_test.go`): config validation;
initial identity/zero model; valid deterministic selection; lowest-index
tie-break; `alpha=0` mean-only training; disjoint **action isolation**;
positive/zero/negative reward effects; repeated-update accumulation; the exact
hand-computable `d=1` case (`A=[2]`, `b=[2]`, `θ=1`); exploration-term math and
monotonicity in `alpha`; malformed state/dimension fallback; invalid
action/reward ignored; `Policy` interface satisfaction; and adapter
compatibility (`State → LinUCB → ActionIndex → Adapter → policy.Action`).
Integration as the opt-in experiment mode is described in §16.1.

---

## 21. RL decision timing, reward credit assignment, leakage prevention

`policyRunner.observe(rec)` (single serialized path, under one mutex) now runs:

```
request completes -> metrics recorded
  -> update counters/window, build Context_t
  -> if a previous action exists:
       State_{t-1} = StateFromContext(prevContext)
       State_t     = StateFromContext(Context_t)
       reward_t    = RewardCalculator.Calculate(State_{t-1}, State_t, prevAction, havePrevious)
       policy.Update(prevContext, prevAction, reward_t)   // credit to the PREVIOUS action
  -> Action_t = policy.SelectAction(Context_t)
  -> applyAction(Action_t) -> controllers change
  -> remember prevContext = Context_t, prevAction = Action_t
  -> future allocation sees the new target
```

**Credit assignment (critical).** The outcome observed at step *t* is the
consequence of the action chosen at step *t−1*, so `Update` is called with the
**previous** context and previous action, and the reward is the change between
the previous and current observations. It is never called with the
just-selected action (which has no outcome yet). The first observation selects
an action but calls no `Update`; therefore `Update` count = `SelectAction`
count − 1. Deterministic policies ignore reward entirely, so this change does
not alter their decisions.

**Pipeline:** `Context_t → State` (raw) → `RewardCalculator` → `Reward` →
learner `Update`. LinUCB uses `reward.Value`; the calculator never talks to the
learner.

Available at decision time (all from completed attempts): current concurrency,
current chunk size, windowed per-request throughput, windowed aggregate
throughput, just-completed request duration and bytes, and cumulative counters.
**Not** available/included: future throughput/duration, final download duration,
future controller state, final output correctness, or any uncompleted request.
No leakage.

---

## 22. Current RL status

**Implemented:** the RL contract (State; config + validation; explicit
`Features` normalization; `ActionIndex` + encode/decode; `Reward` /
`RewardComponents` + validation; `Policy` interface; `Adapter`;
`AlwaysActionPolicy`; integration tests through the real downloader) **and the
first learner: disjoint LinUCB** (`LinUCBConfig`, `NewLinUCB`, `SelectAction`,
`Update`, Gonum-backed SPD linear algebra, deterministic lowest-index
tie-break, action isolation, adapter compatibility test).

**Implemented:** the reward computation layer — `RewardCalculator` +
`RewardConfig` (`internal/rl/reward_calculator.go`), wired into
`policyRunner.observe` with correct credit assignment
(`downloader.Config.RewardConfig`). The reward was reviewed and is now the
**current baseline: Candidate C (absolute + signed improvement throughput;
state-change `ActionCost`; `ThroughputImprovement` component/weight)** — see
§20.7, §22.2. When `RewardConfig` is nil, reward remains `0` (backward
compatible).

**Implemented (integration):** LinUCB is available as an **opt-in fifth
experiment mode** (`ModeLinUCB`) via the existing `rl.Adapter` + `policyRunner` +
`RewardCalculator`, selectable through `RunSelected`, `MatrixConfig.Modes`, and
the CLI `-modes linucb`. The default four-mode behavior and the baseline dataset
are unchanged (§16.1). The complete causal loop
`State_t → Action_t → execution → Observation_{t+1} → Reward_t →
Update(State_t, Action_t, Reward_t)` is validated by deterministic integration
tests (`internal/rl/causal_integration_test.go` and downloader runner tests).

**Implemented (benchmark):** the first formal 5-mode benchmark (four baselines
+ LinUCB) has been run with the locked `DefaultMatrixConfig` at 10 repetitions
and documented in §22.6 (`testdata/benchmark/`). It is descriptive only.

**Not implemented:** empirical tuning / scientific validation of reward weights
(they are an explicit, configurable current baseline, §20.7); fairness objective
(no observable fairness signal exists yet — see §20.7); a **meaningful LinUCB
performance evaluation** — the benchmark ran but LinUCB made no controller
changes (exploration lock-in, §22.6). The lock-in is now **corrected** (§22.8)
but the correction is **not yet benchmarked**, so **no performance claim is
made**; persistence/checkpointing; any more advanced RL algorithm.

### 22.1 LinUCB calibration and reward diagnostics

A small, deterministic calibration workflow answers "is the reward signal and
LinUCB behaviour sensible enough to proceed to formal evaluation?" It is
**diagnostic only**: it does not rank modes, tune weights, or claim performance.

- **Code:** `internal/experiment/calibration.go` (pure diagnostics +
  `RunCalibration`), `cmd/calibration/main.go` (CLI).
- **Scenarios** (`CalibrationScenarios()`, all privilege-free and deterministic;
  they use the server's existing rate-transition, `SetDelay`, and `FailNext`
  mechanisms, not tc/netem):
  - A `calibration-a-stable` — constant rate.
  - B `calibration-b-throttle` — 256 KiB/s → 2 MiB/s after 4 requests.
  - C `calibration-c-latency` — deterministic 20 ms server delay.
  - D `calibration-d-failure` — first 3 requests fail with 500.
  - E `calibration-e-combined` — throttle + 10 ms delay + 2 failures.
- **Base config:** file 128 KiB, chunk 8 KiB, min/max chunk 4/32 KiB, step
  4 KiB, concurrency 1 (min 1, max 3), thresholds 0.2, window 4, retries 3,
  timeout 10 s, `LinUCBAlpha` 0.2, `DefaultRewardConfig`. Repetitions 2 by
  default (`-reps`).
- **Diagnostics per run:** action distribution (counts/fractions, unique,
  concurrency/chunk exploration), reward stats (mean/median/min/max/sd,
  pos/zero/neg), component means (raw and weighted), churn (action transitions,
  changes/decision, longest same-run, actual concurrency/chunk changes),
  early/late action distributions, cumulative reward, and reward
  **reconstruction cross-check** (recomputed reward vs. the runner's recorded
  reward; any mismatch marks the run numerically unhealthy).
- **Output:** `testdata/calibration/calibration.json` (machine-readable) and
  `testdata/calibration/calibration.txt` (concise summary). The four-mode
  baseline dataset `testdata/baseline/` is unchanged.

### 22.2 Reward design review (original → current baseline)

The **original** first reward used `Throughput` as a *change* signal only
(`(current − previous)/ThroughputScale`). Under a stationary rate that term is
~0, so the reward was dominated by costs (`latency_cost`, `action_cost`), and a
stable high-throughput state was indistinguishable from a stable
low-throughput state. Three formulations were considered:

- **A. Improvement only** — original; rewards adaptation, cannot reward
  sustained throughput; cost-dominated under stationary conditions.
- **B. Absolute only** — rewards sustained throughput; adaptation is implicit.
- **C. Absolute + improvement** — rewards sustained throughput *and* the
  change that produced it. **Chosen.**

Candidate C was chosen because the stated objective is *sustained high
throughput with adaptation*: absolute throughput makes a stable high-throughput
state score above a stable low-throughput state, while the improvement term
credits actions that raise throughput. `ActionCost` was simultaneously changed
from "charged for any non-`ActionNone` action" to "charged only when the action
**actually changed** the corresponding controller target", so it now reflects
real control churn rather than button presses. No learner math, State,
ActionIndex, adapter, range/controller/worker logic, or deterministic policy was
modified; no weights were tuned.

### 22.3 Current calibration findings (factual observations only)

From a diagnostic run (`go run ./cmd/calibration -reps 2`) with the **current
(C) reward driven by the aggregate-throughput observable** (§22.5); see the
saved report for exact numbers. **No conclusion about quality is drawn.** The
pre-audit (per-request reward) run is kept only for contrast in
`/tmp/calibration_perrequest.*` (outside the repo).

- **Reconstruction cross-check passed everywhere**
  (`reconstruction_mismatches=0`, `numerically_healthy=true`).
- **Rewards reflect aggregate throughput level.** Means: stable ≈ 0.46,
  throttle ≈ 1.23, latency ≈ 0.19, failure ≈ 0.32, combined ≈ 0.73; component
  raw means e.g. combined `throughput` 0.78, `improvement` 0.06.
- **Exploration remains scenario-dependent.** Stable / throttle / latency:
  `ActionNone = 100%` (`unique_actions=1`, 0 churn). Failure / combined:
  `unique_actions=3` / `2`; failure drifts to `ActionDecreaseConcurrency`
  (late ≈ 20 of ~30), combined drifts to `ActionIncreaseConcurrency`
  (late ≈ 18 of ~30).
- **The correction changed learner behaviour where throughput is actionable:**
  with the aggregate reward, `calibration-e-combined` now favours
  **increase-concurrency (94.4%)**, whereas the per-request reward favoured
  decrease-concurrency (83.3%). This is consistent with the aggregate observable
  rising with concurrency. No weights were changed.
- **Stable/throttle/latency still lock to `ActionNone`.** The state can now
  *represent* a concurrency benefit, but LinUCB does not explore it there: the
  absolute-throughput reward (~0.2–1.2) dominates the UCB exploration bonus, so
  untried arms never win. This is a reward-scale/exploration interaction, **not**
  an observability defect, and it is **not** tuned here.
- **Chunk-size is still not exercised** (`explored_chunk_size=false`). The
  harness models no chunk-size performance tradeoff (the server paces each
  request at a fixed rate, so chunk size affects request count/duration but not
  throughput), so no throughput-based observable can identify a chunk-size
  benefit. This is a **harness limitation**, recorded for the benchmark design.

### 22.4 Research status

- The formal 5-mode benchmark **has been run** once (§22.6); it is descriptive
  and produced no validated performance conclusion.
- The **LinUCB Exploration Diagnostic** (§22.7) explains the `ActionNone=100 %`
  result: primarily an **exploration/reward-scale** problem (plus the
  initialization tie-break), **not** a concurrency-observability defect; chunk
  size remains unidentifiable because the harness models no chunk-size tradeoff.
- The current (C) reward is **frozen as the next baseline** but is **not
  scientifically validated**; it was **not** tuned to improve outcomes. Its
  throughput components are based on the aggregate observable (§22.5).
- Calibration is **diagnostic** (implementation/reward sanity), not a
  performance evaluation; no ranking is produced.
- **No performance conclusion** about LinUCB (or any mode) has been established;
  in the benchmark LinUCB made no controller changes (§22.6).
- The **exploration lock-in is corrected** (§22.8): LinUCB now takes a bounded,
  deterministic warm-up pass over the action space before normal UCB selection.
  This is a correctness change; Benchmark v2 (§22.9) ran it descriptively but
  made **no performance claim** (the warm-up dominates the short runs).
- **Formal Benchmark v3** (§22.11, 4 MiB horizon, 32–122 learned decisions per
  scenario) found **no meaningful post-warm-up policy evolution**: LinUCB locked
  onto a single arm after the warm-up and never changed concurrency. Descriptive
  only; no superiority claim. This is the current frontier of the RL evaluation.
- The **post-warm-up lock-in diagnostic** (§22.12) numerically attributes the
  lock to posterior dominance, the capped exploration bonus (unselected arms are
  never updated), and reward/credit lag; reward separability is not the cause and
  the chunk-size limitation is only partial. No fix is proposed.
- An **experimental periodic-exploration correction** (§22.13,
  `LinUCBConfig.ExplorationInterval`, default 0) is implemented and diagnostically
  evaluated: it raises action coverage from 1 to 5 unique arms and updates
  previously frozen arms, but does not change the dominant arm preference in the
  frozen-state replay. **No performance claim; no benchmark yet.**
- The **live periodic-exploration benchmark** (§22.14) confirmed in real
  downloads that periodic exploration raises unique post-warm-up arms to 5 and
  moves the posterior, and in `loss` changed greedy behaviour and held
  concurrency; in the other scenarios the dominant preference was unchanged.
  Behavioral only; **no ranking or performance claim**.
- The **final six-mode benchmark** (§22.15) placed standard and periodic LinUCB
  beside the four deterministic controllers under the identical 4 MiB matrix
  (300/300 valid). Behaviorally, periodic exploration changes action coverage and
  controller state but not the dominant arm preference in most scenarios;
  performance is descriptive only. **No winner or superiority claim.**
- An **aria2c external baseline** (§22.16) was measured under the same scenarios
  (50/50 valid): 4 concurrent connections, fixed 1 MiB splits, no within-run
  adaptation, 0 retries. Completion times differ from the internal modes, but the
  comparison is confounded by request granularity; **no ranking**. IDM not
  evaluated.
- The **request-granularity sensitivity study** (§22.17) matched the internal
  downloader to 1 MiB/4-concurrency and reproduced aria2 closely in 4/5
  scenarios (0.85–0.97×), substantially reducing the granularity confound;
  latency retains ~2.3× unexplained aria2 overhead. Descriptive only.
- Fairness is still **not implemented** (no measurable signal — see §20.7).

### 22.5 RL State Observability Audit

**Question.** Before the formal benchmark, can the RL State distinguish the
consequences of changing concurrency and chunk size? Traced through the actual
data flow and calibration trajectories.

**Current state fields (all from `policy.Context`, one decision per completed
attempt):**

| Field | Source | Meaning | Action-sensitive? |
|---|---|---|---|
| `Concurrency` | `concurrency.Controller.Current()` | target worker count | reflects the action, not its outcome |
| `ChunkSize` | `chunksize.Controller.Current()` / fixed | target for future allocations | reflects the action |
| `Throughput` | runner window | `sum(window bytes)/sum(window durations)` — **per-request** rate | no (server-paced: ≈ rate regardless of concurrency) |
| `AggregateThroughput` | runner window | `sum(window bytes)/wall-clock span` — **aggregate** rate | **yes (concurrency)** |
| `RequestDuration` | last `metrics.Record` | just-completed request duration | chunk size (duration = bytes/rate) |
| `BytesTransferred` | last `metrics.Record` | just-completed request bytes | chunk size |
| `SuccessfulRequests`/`FailedRequests`/`Retries`/`TotalAttempts` | runner counters | cumulative, +1 per observation | no (failures/concurrency only indirectly) |

**Throughput semantics (pre-audit).** `fetcher.Result.Throughput =
len(Data)/Duration`; the runner's `Context.Throughput =
sum(recent bytes)/sum(recent durations)` over `window` attempts. Because the
experiment server paces **each** response independently at `rate` bytes/s
(`server.writeThrottled`), every request reports ≈ `rate`, so `Context.Throughput`
is a per-request-rate proxy. It is **not** a wall-clock aggregate; even
`metrics.Aggregate.AverageThroughput` is `successBytes/successElapsed`
(per-request-duration-weighted), and no wall-clock timestamp was retained. The
`workerPool.active` count (actual parallelism) was not observable.

**Concurrency observability (pre-audit).** Increasing concurrency 1→N raised
the true aggregate (≈ N·rate) and shortened wall-clock duration, but changed
**no** observed field except the `Concurrency` target itself: `Throughput`,
`RequestDuration`, and `BytesTransferred` were unchanged under server pacing.
The reward was therefore action-independent w.r.t. concurrency — a fundamental
identifiability gap, not merely a learning-speed issue. Calibration traces
confirmed: stable 512 KiB/s held `throughput` ≈ 0.45–0.47 MiB/s with
`Concurrency=1` and `ActionNone=100%`; throttle rose only when the *server rate*
changed (0.24→1.65 MiB/s), never through concurrency.

**Chunk-size observability.** `IncreaseChunkSize` is visible in `ChunkSize`,
`BytesTransferred`, and `RequestDuration` (and hence `LatencyCost`), but has no
throughput effect in this harness. The reward could therefore learn only to
*avoid* long requests, not a genuine chunk-size benefit.

**Temporal resolution / credit assignment.** Decisions are **serialized**, one
per completed attempt (`policyRunner.observe` under one mutex). With concurrency
N, N workers complete ~simultaneously and produce N serialized decisions. The
reward for the action at decision k is computed from `(state_k, state_{k+1})`,
where `state_{k+1}` is the *next completed attempt* — which may have been
in-flight when the action was applied (and, for chunk size, allocated at the old
size). Credit assignment is therefore **approximate under concurrency > 1** and
**approximate for chunk size while old-size requests are in flight**. It is not
redesigned here.

**Conclusion — the pre-audit state was insufficient** for the project's
objective (aggregate throughput): it could not expose the throughput effect of
concurrency, and the harness models no chunk-size performance effect at all.

**Correction (minimum observable added).** `AggregateThroughput`, a rolling
wall-clock aggregate over the same `window`:

- **Formula:** `sum(last window bytes) / (max(sample.end) − min(sample.start))`,
  where `sample.start = sample.end − record.Duration` and `sample.end` is the
  wall-clock time the attempt was observed. Units: bytes/sec. First
  observation: window = 1 sample → equals the per-request rate. Non-positive span
  → 0. Computed in `policyRunner.aggregateThroughput` using an injectable
  `now func() time.Time` for deterministic tests.
- **Context/State:** added `policy.Context.AggregateThroughput` and
  `rl.State.AggregateThroughput`; the existing `Throughput` is **unchanged**.
- **Features:** new feature `aggregate_throughput` at **index 3**, normalized
  `AggregateThroughput / MaxThroughput` (shared bound), clamped `[0,1]`;
  dimension 9 → **10**, still derived from `len(FeatureNames())`.
- **Reward:** the `Throughput` and `ThroughputImprovement` components now use
  `AggregateThroughput` (same components, weights, and formula; only the source
  observable changed). Per-request throughput remains in the State/features.
- **Deterministic policies:** unchanged logic; they ignore the new field. The
  four-mode baseline dataset is unaffected (`testdata/baseline/` untouched).
- **Tests:** runner-level per-request-vs-aggregate discrimination with a fake
  clock; State feature normalization/order; dimension; reward-uses-aggregate;
  policy Context validation; calibration reconstruction.

**Remaining (documented, not fixed):** (1) the stable/throttle/latency lock-in
is an exploration/reward-scale issue; (2) chunk size has no modelled performance
tradeoff, so the harness cannot evaluate a chunk-size preference. Server/network
pacing differences from real links are a further caveat.

### 22.6 Formal 5-mode benchmark (first formal run)

**Status: completed.** Descriptive only — no ranking, no winner, no performance
claim. The RL architecture and reward were not changed and no weights/alpha were
tuned for this run.

- **Dataset (new, does not overwrite history):**
  - `testdata/benchmark/matrix_5mode_reps10.json` — raw `MatrixResult` (all 250
    runs, trajectories, summaries).
  - `testdata/benchmark/matrix_5mode_reps10.txt` — human-readable summary
    (config, correctness, performance table, LinUCB behavior, aggregate
    validation, statistical method, caveats).
  - `testdata/baseline/` and `testdata/calibration/` are **unchanged and kept
    separate**.
- **Reproduce:** `go run ./cmd/experiment -matrix -sudo -reps 10 -json
  -modes fixed,concurrency-adaptive,chunk-size-adaptive,joint-adaptive,linucb >
  matrix_5mode_reps10.json`.
- **Configuration (locked = `DefaultMatrixConfig`, reps=10):** 5 scenarios × 5
  modes × 10 repetitions = **250 runs**. File 256 KiB; chunk 32 KiB, min/max
  32/128 KiB, initial 32 KiB, step 32 KiB; concurrency 1 (min 1, max 4);
  thresholds 0.2/0.2; window 4; retries 3; timeout 30 s; LinUCB alpha 0.2,
  `DefaultRewardConfig` (aggregate-throughput based). Scenarios: `baseline`
  (4 MiB/s), `server-throttle` (512 KiB/s → 4 MiB/s after 4 req), `latency`
  (4 MiB/s + 20 ms), `loss` (4 MiB/s + 1 %), `combined` (1 MiB/s + 20 ms + 1 %).
- **Correctness / validity:** **250/250 valid**, 0 invalid, 0 failed downloads.
  Per run: `Err==""`, `OutputMatches` (byte-for-byte), `NoDuplicateRanges`,
  `NoMissingRanges`. Network status `none` for baseline/server-throttle,
  `applied` for latency/loss/combined; **0 network-unavailable runs**. SHA-256
  is **not** implemented in the framework (byte compare is used instead). tc
  cleanup verified after the run (no residual qdisc).
- **Aggregate-throughput validation (benchmark data):**
  - Windowed `Context.AggregateThroughput` vs per-request `Throughput` across the
    400 LinUCB decisions (all at concurrency 1): mean relative difference
    **0.59 %** (max 3.1 %) — with no overlap the two agree, as expected.
  - Wall-clock run rate (`FileSize/Duration`) vs the framework's per-request
    weighted `AverageThroughput` on runs with concurrency > 1:
    `latency` concurrency-adaptive **1.45×**, `combined` concurrency-adaptive
    **1.42×** / joint **1.48×**; at concurrency 1 the ratio is ≈ 0.98–1.00.
    This confirms the wall-clock aggregate captures overlap and is distinct from
    the per-request metric. (The windowed observable itself is covered by the
    deterministic fake-clock runner test; LinUCB never overlapped in this run.)
- **LinUCB behavior:** in **all five scenarios** LinUCB selected `ActionNone`
  100 % (80 decisions per scenario; `unique_actions=1`), performed **0**
  controller changes (0 concurrency, 0 chunk size), and never exceeded
  concurrency 1. Concrete per-scenario action counts and final controller state
  are in the summary dataset. This is the documented exploration/reward-scale
  lock-in (§22.3), **not** a state bug; it was not tuned away.
- **Observed performance (mean completion time, ms; not ranked):**

  | Scenario | fixed | concurrency-adaptive | chunk-size-adaptive | joint-adaptive | linucb |
  |---|---|---|---|---|---|
  | baseline | 73.5 | 73.3 | 73.4 | 73.7 | 74.2 |
  | server-throttle | 296.0 | 287.8 | 296.2 | 288.0 | 297.4 |
  | latency | 434.1 | 327.1 | 312.5 | 326.8 | 434.9 |
  | loss (high variance) | 135.4 | 132.0 | 174.2 | 94.4 | 115.4 |
  | combined | 710.4 | 591.3 | 539.1 | 470.4 | 682.0 |

  Retries were **0** in every run; attempts were 8 in most runs (7 for some
  chunk-size runs). Because LinUCB made no controller changes, its completion
  times are close to `fixed` in every scenario; the deterministic adaptive modes
  did change controllers (`server-throttle`/`latency`/`combined` show
  concurrency and/or chunk-size actions). These are measurements only.
- **Statistical analysis:** independently seeded runs (fresh server/output per
  run, no shared seed → **unpaired** design), n=10 per cell. Reported: mean,
  median, standard deviation, and a descriptive 95 % t CI of the mean
  (df=9, t=2.262). **No significance tests** were run (no pairing; ranking
  forbidden by policy). The `loss` scenario has large wall-clock variance.
- **Caveats:** chunk-size performance tradeoff not modelled; credit assignment
  approximate under concurrency>1; reward weights and alpha untuned; fair
  reporting is descriptive only; fairness not implemented; no aria2/IDM
  comparison. The benchmark establishes **what the policy did and the measured
  numbers**, not that any mode is superior.

### 22.7 LinUCB Exploration Diagnostic

**Status: completed, diagnostic only.** A separate, labeled study into **why
LinUCB selected `ActionNone` 100 % of the time** in §22.6 and whether the
environment can distinguish the non-None actions. It changes **no** production
behavior: LinUCB, the reward, the deterministic policies, feature normalization,
the server, and the frozen formal benchmark dataset are all untouched.

- **Why necessary:** the formal benchmark ran but LinUCB never acted, so a
  performance comparison was not interpretable. The benchmark dataset was
  frozen; this study has its own dataset.
- **Dataset (separate):** `testdata/diagnostics/linucb_exploration/` —
  `report.json` (per-scenario/action stats), `report.txt` (human-readable),
  `initial_scores.txt` (white-box score output). `testdata/benchmark/`,
  `testdata/baseline/`, and `testdata/calibration/` are unchanged.
- **Reproduce:** `go run ./cmd/linucb-diagnostic -reps 5 -sudo` (5 scenarios × 5
  forced actions × 5 reps; `-sudo` only for the loss scenario's tc/netem).
  Integrity check: **`recon_mismatches=0`** everywhere (reward reconstructed from
  the recorded contexts equals the reward the runner attributed).

**Initial LinUCB scores.** With `A_a = I`, `b_a = 0`, the score is
`alpha·sqrt(xᵀIx) = alpha·‖x‖` for **every** arm, so the five actions are
**exactly tied** at initialization and the lowest-index tie-break selects
`ActionNone`. Measured (alpha=0.2): every action scored **0.200101** on the
hand-built baseline first state (‖x‖=1.000507) and **≈0.2062** on the real
first-decision states of all five scenarios (‖x‖≈1.0308).

**Feature scale (10 features, representative baseline first state):**
`[concurrency 0, chunk 0.0312, throughput 0.0042, aggregate 0.0042, duration
0.000007, bytes 1.0, successes 0.001, failures 0, retries 0, attempts 0.001]`,
‖x‖≈1.03. `bytes_transferred` saturates at 1.0 (`32768/1000`, clamped), so it
**dominates the norm**; throughput features are ~1e-3–4e-3 because
`MaxThroughput=1e9`; duration ~1e-5. The context therefore discriminates states
weakly, and the clamp caps the exploration bonus at ≈alpha. (Normalization was
**not** changed.)

**Reward scale (frozen benchmark LinUCB decisions, all `ActionNone`):**

| Scenario | mean | median | sd | pos/zero/neg | dominant components (raw mean) |
|---|---|---|---|---|---|
| baseline | 3.398 | 3.407 | 0.072 | 70/0/0 | throughput 3.396, improvement 0.006, latency 0.009 |
| server-throttle | 1.502 | 0.740 | 1.711 | 70/0/0 | throughput 1.097, improvement 0.421, latency 0.033 |
| latency | 0.590 | 0.607 | 0.066 | 70/0/0 | throughput 0.573, improvement 0.041, latency 0.049 |
| loss | 3.029 | 3.397 | 1.315 | 68/0/2 | throughput 3.074, improvement −0.038, latency 0.015 |
| combined | 0.359 | 0.385 | 0.089 | 69/0/1 | throughput 0.377, improvement 0.021, latency 0.077 |

Retry/failure/action costs are ≈0. Rewards are numerically healthy and
**dominated by the absolute aggregate-throughput benefit**; in every scenario the
reward magnitude (0.36–3.4) **exceeds** the exploration bonus ≈0.21.

**Greedy lock-in (white-box simulation, baseline x, reward 3.4):** arm 0's score
after 1 update = **1.842** vs **0.2001** for every untried arm; after 8 updates
= **3.089** vs 0.2001 (untried arms keep `A=I`, so their bonus never grows).
`ActionNone` is the argmax at every step — it can never be displaced once tried.

**Forced-action feedback** (mean reward over decisions; `Δt` = decision-0
reward; `aggΔ` = mean per-decision aggregate-throughput change; `chg` =
controller changes):

| Scenario | metric | none | inc-conc | dec-conc | inc-chunk | dec-chunk |
|---|---|---|---|---|---|---|
| baseline | reward / Δt / aggΔ | 3.52 / 3.52 / 17k | **6.96** / 3.30 / 513k | 3.47 / 3.49 / 11k | 3.48 / 3.51 / 60k | 3.45 / 3.43 / 17k |
| server-throttle | reward / Δt / aggΔ | 1.52 / 0.45 / 451k | **2.20** / 0.41 / 217k | 1.52 / 0.45 / 448k | **0.36** / 0.37 / 0 | 1.53 / 0.45 / 456k |
| latency | reward / Δt / aggΔ | 1.05 / 1.05 / 0.7k | **2.19** / 1.00 / 157k | 1.06 / 1.05 / 0.8k | 1.69 / 1.66 / 205k | 1.05 / 1.05 / 0.6k |
| loss | reward / Δt / aggΔ | 3.11 / 2.17 / −0.03k | **6.37** / 3.32 / 303k | 2.54 / 3.49 / −249k | 3.44 / 3.50 / 25k | 3.48 / 3.46 / 10k |
| combined | reward / Δt / aggΔ | 0.51 / 0.56 / −8k | **1.18** / 0.50 / 87k | 0.56 / 0.56 / 0.4k | 0.56 / 0.60 / 44k | 0.56 / 0.56 / 0.3k |

`ActionNone` action cost = 0; `inc-conc`/`inc-chunk` are charged only when the
controller actually changed (mean 0.021 / 0.050), and `dec-*` at the lower bound
are no-ops (cost 0). So `ActionNone` has **no positive reward bias beyond the
tie-break and avoidance of the churn cost**.

**Action separability.**
- **Concurrency: clearly separable.** Sustained `IncreaseConcurrency` raises the
  mean reward above `ActionNone` in **every** scenario (+3.44 baseline, +0.68
  throttle, +1.14 latency, +3.26 loss, +0.67 combined), driven by the wall-clock
  aggregate observable. This is **not** an identifiability failure.
- **Chunk size: not meaningfully separable.** `IncreaseChunkSize` vs `ActionNone`
  is inconsistent (baseline −0.03, throttle **−1.16**, latency +0.64, loss +0.33,
  combined +0.05). The throttle result is a **harness artifact**: the rate switch
  is request-count-based, so larger chunks mean fewer requests and the rate never
  steps up. The "latency" difference is just request duration (∝ chunk/rate).
- **Immediate credit is weak.** The decision-0 reward is nearly identical across
  forced actions (e.g. baseline 3.30–3.52) because the next completed attempt was
  already in flight; the action's effect only becomes visible over several
  sustained decisions.

**Concurrency observability (confirmed).** The diagnostic records
`overlap≈29–30` of ≈35 decisions for forced `IncreaseConcurrency` (concurrency>1
with aggregate > 1.2× per-request) and large positive `aggΔ`. The formal
benchmark independently shows wall-clock run rate exceeding per-request
`AverageThroughput` by 1.45× (latency) and 1.42–1.48× (combined) at
concurrency>1. So `AggregateThroughput` does capture overlap.

**Chunk-size observability (not meaningful).** No modelled chunk-size performance
tradeoff exists; observed differences come from request-count/latency artifacts,
not a chunk-size objective. This is an **environment limitation**, not a learner
bug.

**Root causes (evidence-based):**
- **B. Exploration (primary):** disjoint LinUCB gives an untried arm a *constant*
  bonus `alpha·‖x‖≈0.21`, which any positive-reward arm exceeds after one update;
  the explored arm is never displaced.
- **A. Initialization/tie-breaking (contributing):** all arms tie exactly at
  init; lowest-index tie-break picks `ActionNone` first, which then entrenches.
- **C. Reward scale (contributing):** the throughput-dominated reward
  (0.36–3.4) dwarfs the ≈0.21 bonus, so there is no exploration pressure.
- **F. Credit assignment (contributing):** the decision-0 ("first try") reward is
  nearly action-independent, so even a rare exploratory try is poorly
  informative until an action is sustained.
- **E. Environment limitation (chunk size only):** no modelled chunk-size effect.
- **D. State/reward identifiability: not the cause for concurrency** (forced
  concurrency is clearly rewarded); it applies only to chunk size via E.

**Limitations:** 5 reps, wall-clock/TCP variance (loss especially); forced runs
diverge after the first action, so this is not a perfectly paired counterfactual;
"latency" uses the server's `SetDelay` (privilege-free proxy) and only `loss`
uses tc/netem; results are descriptive and rank nothing.

**Code/data changed:** added `internal/diagnostics/linucb_exploration.go`
(+test) and `cmd/linucb-diagnostic/`, plus the white-box diagnostic test
`internal/rl/linucb_exploration_diagnostic_test.go`; new dataset under
`testdata/diagnostics/linucb_exploration/`. `AGENTS.md` gained one general
experiment-discipline rule (separate exploration failure from
environment/identifiability failure before tuning). That diagnostic task changed
**no production behavior**; the fix is implemented separately in §22.8.

### 22.8 LinUCB Exploration Correction

**Status: implemented and unit-tested.** The corrected mechanism is active for
the experiment's LinUCB mode. The **formal benchmark is frozen and was not
rerun**; no performance improvement is claimed (that requires a future benchmark
under §27).

**Diagnosed failure mode (§22.7).** `ActionNone` locked in because (B) disjoint
LinUCB gives an untried arm a *constant* bonus `alpha·‖x‖≈0.21` while the first
arm's posterior mean reached 0.36–3.4, (A) the initial all-arms tie is broken to
`ActionNone` first, (C) the reward scale dwarfs the bonus, and (F) one-step
credit is weak.

**Selected mechanism — explicit initial exploration (warm-up).** A bounded,
deterministic pass over the action space before normal UCB selection. It is the
smallest mechanism that directly guarantees the diagnosed precondition (every
arm gets evidence) and then hands control back to standard LinUCB.

**Configuration.** `LinUCBConfig.InitialExplorationRounds int`: default `0`
(disabled, preserves prior behavior and all existing tests); validated `>= 0`;
`internal/experiment.runOnce` sets it to `1` for `ModeLinUCB`. No other
configuration was added.

**Alternatives rejected.**
- *Alpha tuning* — does not structurally guarantee coverage, is an arbitrary
  knob, and masks the diagnosed cause; also explicitly out of scope.
- *Epsilon-greedy* — arbitrary `epsilon`, introduces randomness (breaking
  determinism/reproducibility) and changes the policy class; it can keep
  selecting bad arms forever rather than warming up once.
- *Optimistic initialization / unobserved-arm prior* — requires a
  reward-scale-dependent optimism hyperparameter, changes the score function,
  and is scale-sensitive; more distortion for no extra guarantee.

**Exact semantics.**
- With `R > 0`, the learner forces the first `R * NumActions` selections in
  `ActionIndex` order (`0..NumActions-1`, repeating for `R > 1`).
- The forced action flows through the **existing** causal path
  (`Select_t → execute → Observation_{t+1} → Reward_t →
  Update(State_t, Action_t, Reward_t) → Select_{t+1}`); it is updated with the
  runner-computed reward. After `R` passes, `exploring` becomes false and normal
  UCB scoring is used for every later decision. There is no permanent cycling.
- **No-op / bound handling:** at the next selection the learner compares the
  relevant target (`Concurrency` for concurrency actions, `ChunkSize` for
  chunk actions) between the selection state and the observed next state. A
  change marks the arm *effective*; no change marks it *no-op*
  (`warmupEffective` / `warmupNoop`). A bound-clamped action is therefore never
  counted as evidence of that action's effect. `ActionNone` is always recorded
  as effective (its intended effect is "no change"). The standard `A`/`b`
  update for the attempted arm is **unchanged** (required to preserve the causal
  mechanism); the warm-up only governs coverage, it does not suppress or rewrite
  the update.
- **Locked initial configuration:** with concurrency at its minimum and chunk at
  its minimum, the canonical order forces `none`, `increase-concurrency`,
  `decrease-concurrency`, `increase-chunk-size`, `decrease-chunk-size`, and the
  increases make their decreases effective — so all five arms get effective
  evidence and no warm-up decision is a bound no-op.
- **Invalid state:** a malformed/mismatched-dimension state still returns
  `ActionNone` and does not advance the warm-up (existing behavior preserved).

**Causal interaction / credit.** No second runner, no duplicate state/reward
computation, no change to the reward formula, weights, state, or feature
normalization. Credit assignment remains **approximate under concurrency > 1**
(one serialized decision per completed attempt); the warm-up provides evidence
through the existing observation/reward mechanism and does not claim perfect
causal attribution.

**Tests.**
`internal/rl/linucb_exploration_correction_test.go`: coverage of every action
once (and multiple rounds), determinism, transition to normal greedy selection,
no-op-not-counted effectiveness bookkeeping, invalid-state and mismatched-
dimension fallbacks, standard `A`/`b` update unchanged, and default-disabled
behavior. `internal/diagnostics/exploration_correction_test.go` demonstrates
before/after via the public API (artifact:
`testdata/diagnostics/linucb_exploration_fix/coverage.txt`). The existing
`TestNewLinUCBInvalidConfig` now also covers negative rounds.

**Limitations (preserved).**
- The **chunk-size** environment limitation is unchanged: the harness models no
  meaningful chunk-size performance tradeoff, so chunk-arm evidence may remain
  uninformative. This task does not address it.
- Credit assignment remains approximate under concurrency.
- The warm-up consumes the first `R * NumActions` decisions (with `R=1`, five
  decisions; in the 8-chunk benchmark scenario that is a substantial share).
- The correction is a **correctness** change (no lock-in); its effect on
  download performance is **not** evaluated here and must not be claimed.

### 22.9 Formal Benchmark v2 — Post-Exploration Correction

**Status: completed.** Descriptive only — no ranking, no winner, no performance
claim. This run uses the §22.8 corrected LinUCB (`InitialExplorationRounds=1`);
nothing was tuned.

- **Dataset (new; the v1 benchmark is frozen and untouched):**
  - `testdata/benchmark_v2/matrix_5mode_reps10_corrected.json` — raw
    `MatrixResult`, 250 runs.
  - `testdata/benchmark_v2/matrix_5mode_reps10_corrected.txt` — human-readable
    summary (configuration, correctness, warm-up, post-warm-up, performance,
    v1/v2 comparison, aggregate validation, limitations).
  - `testdata/benchmark/`, `testdata/baseline/`, `testdata/calibration/` are
    **byte-identical** to their frozen versions (sha256 unchanged; e.g. v1 json
    `9a69e97e…`, v1 txt `61709456…`).
- **Reproduce:** `go run ./cmd/experiment -matrix -sudo -reps 10 -json
  -modes fixed,concurrency-adaptive,chunk-size-adaptive,joint-adaptive,linucb
  > testdata/benchmark_v2/matrix_5mode_reps10_corrected.json`.
- **Configuration:** identical to v1 — `DefaultMatrixConfig`, reps=10, 5
  scenarios × 5 modes = 250 runs; file 256 KiB, chunk 32 KiB (min/max 32/128 KiB,
  step 32 KiB), concurrency 1 (min 1, max 4), thresholds 0.2/0.2, window 4,
  retries 3, timeout 30 s, LinUCB alpha 0.2, `DefaultRewardConfig`,
  `InitialExplorationRounds=1`. No random seeds exist; runs are independent
  fresh runs.
- **Correctness / validity:** **250/250 valid**, 0 invalid, 0 failed downloads;
  every run `Err==""`, `OutputMatches`, `NoDuplicateRanges`, `NoMissingRanges`.
  Network `none` for baseline/server-throttle and `applied` for
  latency/loss/combined; **0 unavailable runs**. SHA-256 is not implemented. tc
  cleanup verified (no residual qdisc).
- **LinUCB warm-up (first `R·NumActions = 5` selections):** in **all 5 scenarios
  and all 10 runs**, the exact expected order
  `none → increase-concurrency → decrease-concurrency → increase-chunk-size →
  decrease-chunk-size`, and **every non-None action was effective** (0 no-ops)
  — the locked initial state (concurrency min, chunk min) makes the increases
  move the controllers so the decreases bite. Warm-up mean rewards (descriptive)
  ranged e.g. baseline `none 3.46`, `increase-concurrency 3.34`,
  `decrease-concurrency 5.60`, `increase-chunk-size 4.12`,
  `decrease-chunk-size 4.30`.
- **LinUCB post-warm-up (learned) behavior:** the benchmark file yields
  **7 decisions per run**, so only **2 decisions per run (20 per scenario)** are
  learned. In that small sample normal LinUCB selected a single action per
  scenario: baseline/loss `decrease-concurrency` 100 % (no-op at the min),
  server-throttle `decrease-chunk-size` 100 % (no-op at the min), latency
  `increase-chunk-size` 100 % (effective, chunk 32→98 KiB), combined
  `increase-chunk-size` 90 % / `decrease-chunk-size` 10 %. **Concurrency changes
  after warm-up: 0** in every scenario; chunk changes only in latency (20) and
  combined (18). Max concurrency in the post phase was 1; final concurrency 1 in
  all scenarios. The post phase is too short to characterize sustained learning.
- **Observed performance (mean completion ms; not ranked; full table + CI in
  the summary):**

  | Scenario | fixed | concurrency-adaptive | chunk-size-adaptive | joint-adaptive | linucb |
  |---|---|---|---|---|---|
  | baseline | 72.9 | 72.8 | 72.9 | 72.9 | 63.7 |
  | server-throttle | 296.4 | 287.8 | 295.7 | 287.8 | 231.0 |
  | latency | 434.8 | 327.2 | 313.3 | 327.1 | 386.6 |
  | loss (high variance) | 113.6 | 171.0 | 92.7 | 94.2 | 64.6 |
  | combined | 638.8 | 489.0 | 542.2 | 475.2 | 557.0 |

  Retries and failed attempts were **0** in every run.
- **Comparison with the frozen pre-correction benchmark (behavioral, not
  superiority):** in v1 LinUCB selected `ActionNone` 100 % (8 decisions/run, 0
  controller changes). In v2 it performs 5 deterministic warm-up selections and
  then ~2 learned decisions; the warm-up makes all non-None actions effective at
  least once. v2 LinUCB completion times are lower than v1 in every scenario,
  but this is a **behavioral observation**: the forced warm-up itself changes
  concurrency/chunk size (and the learned chunk actions in latency/combined
  reduce request count), and the post-warm-up sample is only 2 decisions. **No
  performance improvement is claimed.**
- **Aggregate-throughput validation (benchmark):** wall-clock run rate
  (`FileSize/Duration`) vs the framework's per-request `AverageThroughput` on
  runs with concurrency > 1: latency concurrency-adaptive **1.45×**, combined
  concurrency-adaptive **1.52×** / joint **1.48×** (confirming the wall-clock
  aggregate captures overlap and differs from the per-request metric). The
  windowed `Context.AggregateThroughput` vs `Throughput` distinction is covered
  by the deterministic fake-clock runner test `TestPolicyRunnerAggregateThroughput`.
- **Statistical analysis:** independent fresh runs, no shared seed → unpaired,
  n=10/cell; mean, median, SD, descriptive 95 % t-CI (df=9). **No significance
  tests, no ranking.**
- **Limitations / anomalies:** (1) the 5-selection warm-up dominates the
  7-decision run, so the learned phase is only 2 decisions — the dominant
  limitation of this benchmark; (2) chunk-size performance tradeoff still not
  modelled, so chunk actions may be uninformative; (3) credit assignment remains
  approximate under concurrency > 1; (4) loss scenario high wall-clock variance;
  (5) fairness not implemented; no aria2/IDM comparison. No correctness
  anomalies occurred.

### 22.10 LinUCB Learning-Horizon Study

**Status: completed (design study).** A separate, labeled diagnostic to determine
how long a benchmark trajectory must be for the fixed 5-selection LinUCB warm-up
to leave enough post-warm-up decisions to evaluate learning. It changes **only
the file size**; every other parameter is the locked `DefaultMatrixConfig` value.
No frozen dataset was touched.

- **Why v2 was insufficient (§22.9):** the 256 KiB file yields 7 decisions, of
  which 5 are the forced warm-up, leaving only **2** learned decisions.
- **Dataset / reproduce:** `testdata/diagnostics/linucb_horizon/report.json` +
  `.txt`; `go run ./cmd/linucb-horizon -sizes
  262144,524288,1048576,2097152,4194304 -scenarios
  baseline,server-throttle,latency,combined -modes linucb,fixed -reps 3 -sudo`.
  New code: `internal/diagnostics/horizon.go`, `cmd/linucb-horizon/`, plus a
  test; **no production code changed** and `DefaultMatrixConfig` is untouched.
- **Decision-count semantics (measured, not assumed):** each completed attempt
  produces one policy decision, so decisions = number of allocated ranges (+
  retries; retries were 0 here). With a fixed chunk the total is `S / 32768`
  (fixed mode: 8/16/32/64/128 for 256 KiB…4 MiB). With adaptive chunk it is
  path-dependent: the warm-up's single 64 KiB allocation removes one range, and
  if the policy later drives chunk to its 128 KiB maximum (latency/combined) the
  total collapses by ~3.5×. Counts were deterministic across the 3 repetitions
  in every cell.
- **Candidate horizons (LinUCB, mean over 3 runs; post = after the 5 warm-up):**

  | File size | baseline total / post | server-throttle total / post | latency total / post | combined total / post |
  |---|---|---|---|---|
  | 262144 (control) | 7 / 2 | 7 / 2 | 7 / 2 | 7 / 2 |
  | 524288 | 15 / 10 | 15 / 10 | 9 / 4 | 9 / 4 |
  | 1048576 | 31 / 26 | 31 / 26 | 13 / 8 | 13 / 8 |
  | 2097152 | 63 / 58 | 63 / 58 | 21 / 16 | 21 / 16 |
  | **4194304** | **127 / 122** | **127 / 122** | **37 / 32** | **37 / 32** |

  Warm-up is exactly 5 in every cell (fixed). The 262144 control reproduces v2
  (7 total, 5 warm, 2 post), validating the harness. `fixed` mode (no policy)
  gives the intrinsic range count `S/32768` for reference.
- **Behavioral observation:** at every horizon the post-warm-up policy **stayed
  on a single action** for the whole learned phase (baseline/throttle:
  `decrease-concurrency` at the min bound, 100 %; latency/combined:
  `increase-chunk-size` to the max, 100 %); concurrency was never changed after
  the warm-up. The longer horizons show this lock is stable rather than
  revealing switching. This is a policy observation, not a horizon defect.
- **Selected horizon for the next formal benchmark: 4194304 B (4 MiB).**
  Rationale: it provides **≥32 post-warm-up decisions even in the worst case**
  (latency/combined, where chunk grows to 128 KiB) and ~122 in
  baseline/server-throttle — enough to split the learned phase into two ~15+
  decision halves and to observe several policy updates per arm; 2 MiB was
  rejected as marginal (16 worst-case). Estimated runtime of a full
  5-mode × 5-scenario × 10-rep benchmark is ~13–15 min (vs ~1 min at 256 KiB),
  which is acceptable for a one-off evaluation.
- **Side effects of the larger horizon:** the `server-throttle` transition stays
  at request 4, so the learned phase runs at the post-transition steady rate
  (4 MiB/s) and the throttled phase is a small early fraction; netem conditions
  are unchanged (applied for the whole run); retries remained 0; no new phases
  or bound changes appeared. Larger chunk sizes (not larger files) are what
  shrink the horizon, and that is accounted for in the selected size.
- **Limitations preserved:** the horizon study does **not** make chunk size
  scientifically identifiable (§22.7 E) and does not prove learning; it only
  establishes that the next benchmark has enough post-warm-up observations to
  attempt an evaluation. Approximate credit assignment under concurrency > 1
  still holds. The `loss` scenario was excluded from the study because it does
  not change the decision count (retries 0), but it remains in the benchmark
  scenario set.

### 22.11 Formal Benchmark v3 — 4 MiB Learning Horizon

**Status: completed.** The first formal evaluation with corrected
aggregate-throughput observability, the bounded warm-up, and a long enough
trajectory. Descriptive only — **no ranking, no performance claim**. Nothing was
tuned.

- **Dataset (new; v1/v2 and all diagnostics frozen and untouched):**
  - `testdata/benchmark_v3/matrix_5mode_4MiB_reps10.json` — raw `MatrixResult`,
    250 runs.
  - `testdata/benchmark_v3/matrix_5mode_4MiB_reps10.txt` — human-readable
    summary (config, correctness, horizon, warm-up, early/late learning,
    trajectory, performance, aggregate validation, limitations).
- **Reproduce:** `go run ./cmd/experiment -matrix -sudo -reps 10 -json
  -file-size 4194304 -modes fixed,concurrency-adaptive,chunk-size-adaptive,
  joint-adaptive,linucb > testdata/benchmark_v3/matrix_5mode_4MiB_reps10.json`.
  The `-file-size` matrix override is a small, explicit experiment-level flag
  (`cmd/experiment.applyFileSizeOverride`); the default `DefaultMatrixConfig`
  (256 KiB) is unchanged.
- **Configuration:** `DefaultMatrixConfig` with **FileSize = 4194304 B (4 MiB)**;
  everything else locked (chunk 32 KiB, min/max 32/128 KiB, step 32 KiB;
  concurrency 1 (1–4); thresholds 0.2/0.2; window 4; retries 3; timeout 30 s;
  LinUCB alpha 0.2, `DefaultRewardConfig`, `InitialExplorationRounds=1`).
  Scenarios unchanged (baseline/server-throttle/latency/loss/combined).
- **Correctness / validity:** **250/250 valid**, 0 invalid, 0 failed downloads;
  every run `Err==""`, `OutputMatches`, `NoDuplicateRanges`, `NoMissingRanges`.
  Network `applied` for latency/loss/combined, `none` otherwise, **0
  unavailable**. SHA-256 not implemented. tc cleanup verified.
- **Decision horizon (actual):** warm-up exactly 5 everywhere; learned decisions
  are scenario-dependent because chunk growth shrinks the horizon —
  baseline/server-throttle **122**, latency/combined **32**, loss **64 or 122**
  (some loss runs kept chunk small, others grew it), as warned in §22.10.
- **Warm-up:** exact expected order in all 50 LinUCB runs
  (`none → increase-concurrency → decrease-concurrency → increase-chunk-size →
  decrease-chunk-size`), **all non-None actions effective (0 no-ops)**. Warm-up
  mean rewards seed the posterior (e.g. baseline `decrease-concurrency` 5.60 vs
  `increase-concurrency` 3.33).
- **Early vs late learned phases** (early = post decisions 6–20; late = final
  15): the distributions are effectively identical in every scenario.
  - baseline: `decrease-concurrency` 100 % / 100 %, 0 concurrency or chunk
    changes.
  - server-throttle: `decrease-chunk-size` 100 % / 100 %, 0 changes.
  - latency: `increase-chunk-size` 100 % / 100 % (chunk driven to its 128 KiB
    max; 30 chunk changes early, 0 late).
  - loss: `decrease-concurrency` 95 % / 90 %, a little `decrease-chunk-size`
    (3 % / 10 %), 1 chunk change.
  - combined: `increase-chunk-size` 90 % / 10 % `none` in both phases (chunk to
    max; 27 changes early, 0 late).
- **Concurrency trajectory:** LinUCB **never selected `increase-concurrency`
  after the warm-up in any scenario**; post-warm-up concurrency changes = 0; max
  concurrency = 2 (the transient warm-up increase, reverted by the warm-up
  decrease); final concurrency = 1 everywhere. The corrected
  `AggregateThroughput` did not translate into any learned concurrency change on
  this horizon.
- **Chunk-size trajectory:** latency/combined drive chunk to the 128 KiB max and
  then keep selecting `increase-chunk-size` as a no-op; loss mostly keeps
  `decrease-concurrency` (chunk stays near 32–64 KiB); baseline/throttle keep
  chunk at 32 KiB. Per §22.7 E the harness models no chunk-size performance
  tradeoff, so this is **not** interpreted as successful chunk-size
  optimization.
- **Observed performance (mean completion ms; not ranked):**

  | Scenario | fixed | concurrency-adaptive | chunk-size-adaptive | joint-adaptive | linucb |
  |---|---|---|---|---|---|
  | baseline | 1155.5 | 1156.8 | 1154.0 | 1157.3 | 1162.7 |
  | server-throttle | 1376.9 | 572.9 | 1343.1 | 650.0 | 1322.5 |
  | latency | 6334.1 | 2303.1 | 2564.5 | 2069.2 | 2633.4 |
  | loss (high var) | 1709.5 | 1312.4 | 1534.1 | 1186.3 | 1635.3 |
  | combined | 9895.4 | 5357.1 | 6312.6 | 5548.5 | 6367.9 |

  Retries and failed attempts were **0** in every run. Full median/SD/95 % t-CI
  and wall-clock throughput are in the summary.
- **Aggregate-throughput validation (real C>1):** wall-clock run rate vs the
  framework's per-request `AverageThroughput`: server-throttle
  concurrency-adaptive **2.50×**, latency concurrency-adaptive **2.78×**,
  combined concurrency-adaptive **1.94×** (joint 2.12×/1.93×/1.78×); ratios ≈ 1.0
  where concurrency stays 1 (including LinUCB, which never increased
  concurrency). This confirms the wall-clock observable captures overlap; the
  windowed field itself is covered by the fake-clock runner test.
- **Policy evolution (v3 conclusion):** **no meaningful post-warm-up policy
  evolution was observed.** After the warm-up, LinUCB locked onto a single arm
  (usually the one with the highest warm-up reward) and held it for the entire
  learned phase in every scenario; early and late distributions match, and
  concurrency was never changed. The longer horizon did not produce observable
  adaptation — it made this lock-in clearly measurable rather than revealing
  switching. (Credit assignment remains approximate under concurrency > 1, so
  this is a behavioral observation, not a causal proof.)
- **Comparison with v1/v2 (behavioral):** v1 (256 KiB) `ActionNone` 100 %;
  v2 (256 KiB) warmed up then had only ~2 learned decisions; v3 (4 MiB) has
  32–122 learned decisions and shows a stable single-arm lock. v3 completion
  times differ from v1/v2 but the file size differs, so this is **not** an
  algorithmic-improvement claim.
- **Limitations:** chunk-size tradeoff not modelled; credit assignment
  approximate under concurrency > 1; n=10; no seeds; descriptive statistics;
  horizon varies by scenario (32–122 learned decisions); no fairness; no
  aria2/IDM comparison.

### 22.12 LinUCB Post-Warm-Up Lock-In Diagnostic

**Status: completed, diagnostic only.** Explains *numerically* why LinUCB locks
onto one arm immediately after the warm-up despite 32–122 learned decisions.
No production behavior was changed; v3 is a frozen input; no fix is proposed.

- **Research question:** "Why does LinUCB lock onto a single arm after warm-up?"
- **Data source:** the frozen `testdata/benchmark_v3/matrix_5mode_4MiB_reps10.json`
  (all 50 LinUCB runs, all 4 582 decisions), replayed. Artifacts:
  `testdata/diagnostics/linucb_lockin/{report.json,report.txt,score_trajectory.json,feature_analysis.json}`
  (deterministic; generated by
  `internal/rl/linucb_lockin_diagnostic_test.go`).
- **Posterior reconstruction method:** a white-box replay drives the **production**
  `LinUCB` (`Update`, `SelectAction`, and the unexported `score`) with the recorded
  per-decision state and reward, decomposing each arm's score into
  `mean = θ_aᵀx` and `bonus = α·sqrt(xᵀA_a⁻¹x)`. The v3 `DecisionEvent` records 8
  of 10 raw state fields; `SuccessfulRequests = RequestIndex` (failures/retries
  are 0) and `BytesTransferred = 32768` (its feature clamps to 1.0 for any bytes
  ≥ 1000). **Validation: the replay re-selects the recorded action for
  4 582/4 582 decisions (100 %)**, so the reconstruction is exact.
- **Feature analysis (all decisions):** `‖x‖ = 1.0066 ± 0.0068` (min 1.0005, max
  1.0546). `bytes_transferred` is **1.0 in 100 %** of decisions and contributes
  **98.7 %** of `‖x‖²`; the informative features are tiny — `concurrency` 0.1 %
  (98.9 % at 0), `chunk_size` 0.3 %, `throughput`/`aggregate_throughput` ≈ 0.0 %,
  `request_duration` ≈ 0.0 %, `successful_requests`/`total_attempts` 0.5 % each;
  `failed_requests`/`retries` are constant 0. `throughput` and
  `aggregate_throughput` are **0.993** correlated and `successful_requests` and
  `total_attempts` are **1.000** correlated (redundant). So the feature vector is
  nearly constant across decisions; `bytes_transferred` dominates the norm and
  (via the clamp) caps the exploration bonus at ≈ α·‖x‖ ≈ 0.20.
- **Exploration-bonus vs mean (the lock mechanism):** at the **first greedy
  decision** (decision 6, index 5) in every scenario the selected arm's mean is
  far larger than every other arm's score, and its own bonus is small:

  | Scenario | first greedy arm | mean | bonus | max other bonus | margin |
  |---|---|---|---|---|---|
  | baseline | decrease-concurrency | 2.71 | 0.145 | 0.141 | +0.52 |
  | server-throttle | decrease-chunk-size | 1.17 | 0.141 | 0.145 | +0.44 |
  | latency | increase-chunk-size | 0.48 | 0.141 | 0.145 | +0.12 |
  | loss | decrease-concurrency | 2.70 | 0.145 | 0.141 | +0.52 |
  | combined | increase-chunk-size | 0.28 | 0.141 | 0.145 | +0.06 |

  Because non-selected arms are **never updated**, their scores are **frozen**
  (e.g. baseline rep 0: N=1.909, IC=1.824, IS=2.194, DS=2.310 stay constant from
  decision 6 on) while the selected arm's mean keeps rising (2.71→3.16 by
  decision 11) and its bonus shrinks (0.145→0.076). The untried-arm bonus cannot
  grow (disjoint arms, `A_a = I + x xᵀ` updated only when selected), so once a
  mean exceeds ≈0.2 the arm can never be overtaken.
- **Warm-up effect:** the five warm-up actions are exact and all effective
  (0 no-ops). The warm-up **seeds** the lock: the arm with the highest warm-up
  reward becomes the first greedy choice. Warm-up mean rewards (baseline) were
  `none 3.458, increase-concurrency 3.325, decrease-concurrency 5.598,
  increase-chunk 4.073, decrease-chunk 4.269`; `decrease-concurrency` (a **no-op**
  at the minimum) therefore won the first greedy decision.
- **Action effectiveness (post-warm-up):** of 4 332 learned selections, **4 151
  (95.8 %) were no-ops**. `decrease-concurrency` 2 268 selections / 0 effective
  (100 % no-op); `decrease-chunk-size` 1 332 / 0 (100 % no-op);
  `increase-chunk-size` 609 / 58 effective (90 % no-op after saturating at
  128 KiB); `none` 123 (always effective by definition);
  `increase-concurrency` **0 selections**.
- **Credit-assignment observations:** the reward attributed to a no-op action is
  indistinguishable from `none` (it reflects the environment, not the action),
  and the warm-up reward ordering does not match action effectiveness: the no-op
  `decrease-concurrency` earned the **highest** warm-up reward in
  baseline/loss (5.598/5.610 vs `none` 3.458/3.443). This is the one-step
  `Select → execute → observe → reward` alignment: the reward for action *i*
  reflects the *next* completed request, which may predate the action's effect.
  In-flight misattribution cannot be proven from the recorded data alone, but the
  no-op reward inflation is direct evidence of temporal misalignment.
- **Cross-scenario / all-repetition results:**

  | Scenario | dominant arm | learned decisions (mean) | unique arms | conc changes | chunk changes | lock decision |
  |---|---|---|---|---|---|---|
  | baseline | decrease-concurrency | 122 | 1 | 0 | 0 | 5 (10/10) |
  | server-throttle | decrease-chunk-size | 122 | 1 | 0 | 0 | 5 (10/10) |
  | latency | increase-chunk-size | 32 | 1 | 0 | 30 | 5 (10/10) |
  | loss | decrease-concurrency | 116.2 | 1.5 (1–4) | 0 | 1 | 5–17 (3/10 switch) |
  | combined | increase-chunk-size | 41 | 1 | 0 | 27 | 5 (10/10) |

  Lock-in is immediate (decision 6) and identical across **all 10 repetitions**
  in 4 of 5 scenarios; `loss` is the only scenario with any post-warm-up
  switching (3/10 runs), consistent with its much higher reward variance
  (SD 1.07–1.38 vs 0.10–0.27).
- **Root causes supported by evidence:**
  - **A posterior dominance — CONFIRMED.** At decision 6 the selected arm's mean
    (0.27–2.71) exceeds every other arm's total score, the margin is positive
    (0.06–0.52), and non-selected arms' scores freeze while the winner's mean
    grows.
  - **B feature dominance — PARTIAL.** `bytes_transferred` is constant 1.0 and
    98.7 % of `‖x‖²`; the context is nearly constant and the clamp caps the
    bonus at ≈0.2. It does not rank arms directly (it is a constant), so it is a
    scale/identification issue rather than the ranking cause.
  - **C reward separability — NOT SUPPORTED.** Warm-up rewards differ materially
    across arms (e.g. baseline 3.33–5.73) and the earlier forced-action study
    showed concurrency is reward-separable; the lock is not caused by
    indistinguishable rewards.
  - **D exploration bonus — CONFIRMED.** The max bonus (\~0.145) is 2–19× smaller
    than the selected mean at the first greedy decision, and untried/unselected
    arms' bonuses cannot grow, so the first winner is permanent.
  - **E credit assignment — CONFIRMED.** A no-op arm receives the highest
    warm-up reward and 95.8 % of learned selections are rewarded no-ops; the
    one-step reward lags the action.
  - **F environment limitation — PARTIAL.** Confirmed for chunk size (increase
    saturates, 90 % no-op; no modelled tradeoff); **not** the blocker for
    concurrency (a real tradeoff exists and is rewarded, but the policy never
    selects it).
- **Unresolved / not established:** whether a change of context feature scale, an
  ever-growing exploration bonus, or a different credit scheme would remove the
  lock is untested; in-flight misattribution under concurrency > 1 cannot be
  isolated from the recorded data alone. **No fix is proposed here.**

### 22.13 Periodic Exploration Diagnostic (experimental correction)

**Status: experimental correction implemented and diagnostically evaluated; no
performance claim and no formal benchmark yet.** It addresses the §22.12
post-warm-up freeze: unselected arms are never updated, so their scores are
frozen and the first greedy arm wins permanently. This is **not** a final
algorithm redesign.

- **Mechanism (opt-in, default disabled):** new `LinUCBConfig.ExplorationInterval int`
  (validated `>= 0`, default `0`). When `> 0`, after every `ExplorationInterval`
  **normal** LinUCB decisions the learner forces one exploratory action; forced
  actions cycle through the action space in `ActionIndex` order
  (`none → increase-concurrency → decrease-concurrency → increase-chunk-size →
  decrease-chunk-size → …`) via a **separate deterministic cursor** that advances
  only when a forced action is selected. Between forced events selection is
  standard UCB. The initial warm-up is unchanged. `ExplorationInterval=0`
  reproduces the previous behavior exactly (no periodic state allocated).
- **Effective/no-op handling:** a forced action that cannot change its controller
  target (e.g. `decrease-concurrency` at minimum, `increase-chunk-size` at
  maximum) is still attempted but recorded as non-effective
  (`periodicEffective`/`periodicNoop`); it is never counted as evidence of that
  action's effect. The reward formula, `A`/`b` update, score equation, features,
  alpha, action space, downloader and controller semantics are all unchanged.
- **Diagnostic (deterministic replay of frozen v3, `interval ∈ {0,5}`):** the
  `interval=0` replay re-selects the recorded action for **4 582/4 582**
  decisions, validating the harness. Artifacts:
  `testdata/diagnostics/linucb_periodic_exploration/{report.json,report.txt}`
  (generated by `internal/rl/linucb_periodic_exploration_diagnostic_test.go`).
  The v3 environment states are held fixed, so this isolates the
  exploration/posterior mechanism, not download dynamics.
- **Action coverage (interval 0 → 5):** unique post-warm-up arms **1.0–1.5 →
  5.0** in every scenario; dominant-arm fraction **1.000 → 0.76–0.87**;
  `increase-concurrency` selections **0 → 10–53** per scenario; forced
  exploration events per scenario ≈5–20 (proportional to horizon).
- **Posterior mechanism confirmed:** forcing a previously frozen arm updates it.
  Examples (before → after the forced observation): baseline
  `increase-concurrency` mean 1.639→2.237, score 1.781→2.352, `A` diag 2→3;
  server-throttle `increase-concurrency` mean 0.200→1.277, score 0.342→1.392,
  `A` 2→3; baseline `increase-chunk-size` mean 2.077→2.517, score 2.219→2.632,
  `A` 2→3. So arms that were frozen now receive evidence and their scores move.
- **Forced no-ops reported separately:** per cycle roughly 3 of 5 forced actions
  are effective and 2 are bound no-ops (e.g. baseline forced: `none`,
  `increase-concurrency`, `increase-chunk-size` effective; `decrease-concurrency`,
  `decrease-chunk-size` no-ops). These are recorded and not treated as
  intervention evidence.
- **Lock-in effect:** on this frozen-state replay, periodic exploration
  **substantially increases action coverage** (all five arms are exercised and
  updated) but the **dominant arm preference is unchanged** — the same arm still
  receives the majority of selections (76–87%). So it breaks the *frozen-arm*
  problem (no arm is starved of updates) without, in this diagnostic, changing
  which arm the posterior prefers.
- **Limitations:** deterministic cycling is not learned exploration; the
  diagnostic replays frozen v3 states (not action-dependent download dynamics);
  the chunk-size environment limitation (§22.7 E) and approximate
  credit assignment under concurrency > 1 remain; **no performance conclusion**.
  The next step is a controlled benchmark comparing baseline LinUCB vs
  periodically-exploring LinUCB.

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

### 22.27 DQN v3 Request-Efficiency Correction

**Status: implemented and evaluated; mixed result, reported honestly. Candidate
v3 is an EXPERIMENTAL corrected formulation, not a validated candidate for the
final claim. No aria2 comparison; no performance claim.**

- **Why v2 failed (§22.26):** v2 made one RL decision per completed request
  (**57–87 decisions**) with a 32–128 KiB chunk ladder, producing far more
  requests/round-trips than aria2's **4 × 1 MiB** requests; the per-request
  overhead dominated and DQN was ~75.7 % slower.
- **v3 design changes:** chunk **levels** extended to
  {32, 64, 128, 256, 512, 1024} KiB (increase/decrease on the ladder, so 1 MiB is
  reachable without hard-coding it); a **decision interval** `{1,4,8}` completed
  requests per RL step (transition reward = negative wall-clock seconds of the
  whole interval, preserving `(s,a,r,s',done)`); chunk **cost arises mechanically**
  from `latency + per-request overhead + transfer time` (no reward bonus); the
  training distribution expanded to file 256 KiB–16 MiB, bandwidth 256 KiB–8 MiB/s,
  latency 0–80 ms, loss 0–4 %, both throttle kinds. State spec default
  `MaxChunkSize` raised to 1 MiB (13 features unchanged; Go integration test
  covers the exported model).
- **Decision-interval experiment (simulation only):** interval 1 → val mean
  11.73 s / 22.5 req/MiB; **interval 4 → 10.12 s / 13.4 req/MiB (selected)**;
  interval 8 → 11.79 s / 17.0 req/MiB.
- **Training / selection:** seeds 0–4 (1500 episodes, interval 4); selected
  **seed 0** by validation (test never used). Candidate SHA-256
  `96361535edd3216eabebf2d1fb72791b3551517bbb3a7bbaf44b7a8ee8eb492b`.
- **Held-out simulator (mean completion s / DQN req per MiB; not ranked):**
  DQN **12.70 s / 13.8 req·MiB⁻¹** vs fixed 25.65/32.0, concurrency-adaptive
  24.29/32.0, chunk-size-adaptive 24.60/29.5, joint-adaptive 23.76/30.6. Per
  condition DQN uses few requests where it pays (combined 3.6, latency 4.5,
  latency+loss 4.5 req/MiB) and is ~2–3× faster than the scripted baselines in
  most conditions. **Note:** the scripted baselines still show 29–32 req/MiB
  because their relative-throughput trigger rarely fires in the analytic
  simulator — a baseline-fidelity caveat at the chunk dimension.
- **Ablation (decision interval):** interval 1 → 13.68 s / 21.0 req/MiB;
  interval 8 → 15.71 s / 18.0 req/MiB; selected interval 4 → 12.70 s / 13.8.
- **Live transfer v3 vs v2 (25 runs each; real controlled server; v3 run with
  `-max-chunk 1048576`):**
  - **Improved:** the wall-clock throttle attempts fell **75 → 37** via chunk
    growth to **~449 KiB** (v2 ~102 KiB); chunk now reaches ~160–449 KiB across
    scenarios; fragmentation is substantially reduced.
  - **Regressed:** v3 stays at **concurrency 1** in every scenario (v2 used
    1.4–4.0); completion time is worse under latency (3723 vs 1993 ms) and
    combined (5997 vs 2573 ms). The simulator's per-request-rate model made large
    chunks strongly dominant, so the learned policy under-used concurrency.
  - **Net:** **mixed** — request efficiency improved, concurrency adaptation
    regressed; not a clean success on both axes. Per the task's failure
    condition this is documented rather than presented as a win.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v3/{simulator_spec_v3.json,
  train_config_v3.json, decision_interval_results.json, seed_results_v3.json,
  validation_results_v3.json, heldout_test_results_v3.json, ablations_v3.json,
  live_transfer_results_v3.json, final_dqn_candidate_v3.json,
  final_dqn_candidate_v3.sha256, sim_to_real_v3.json, report.json, report.txt}`.
  Code: `python/train_validate_v3.py`, `python/rl/sim_env_v3.py`; the DQN Go
  package (state/model/live) and `cmd/dqn-transfer` (now with `-max-chunk`) are
  reused. Candidate v2 is untouched.
- **Limitations:** test set not touched by tuning; the real server still models
  no chunk-size tradeoff (so live chunk behavior is not "optimization"); the v3
  simulator over-favoured large chunks, costing concurrency adaptation; live n=5;
  no aria2 comparison; **no performance claim.**

### 22.28 DQN v4 — Adaptive Concurrency Formulation

**Status: implemented and evaluated with a clean, positive transfer result.
Candidate v4 frozen. No aria2 comparison; no performance claim.** This is the
final RL formulation and is deliberately **concurrency-only** — it is **no longer
a joint chunk/concurrency RL claim**.

- **Why v2/v3 failed:** v2 fragmented (57–87 decisions, 32–128 KiB chunks; ~75.7 %
  slower than aria2, §22.26). v3 fixed fragmentation (chunk up to ~449 KiB) but the
  simulator rewarded larger chunks while the real server models **no chunk-size
  tradeoff**, so the learned chunk behavior was invalid and concurrency adaptation
  regressed (§22.27).
- **v4 design:** **chunk size is FIXED at 1 MiB** (fixed for every policy; matched
  to aria2's granularity), and the **only learned variable is concurrency (1–4)**.
  Action space is 3 actions `{none, increase-concurrency, decrease-concurrency}`
  (`ConcurrencyActionNames`/`ConcurrencyIndexToAction`); reward is unchanged
  (`r_t = −elapsed wall-clock seconds`, gamma 1); decision interval ∈ {2,4,8}
  requests per RL step. The 13-feature state is unchanged (chunk-size becomes a
  constant input); state spec `MaxChunkSize` 1 MiB.
- **Simulator (`python/rl/sim_env_v4.py`):** per-request time `latency + chunk/rate`
  (independent of concurrency); a wave of `concurrency` requests runs **in
  parallel** (wave time = slowest request), so aggregate throughput scales with
  concurrency; an optional `link_bandwidth` shared cap makes extra concurrency stop
  helping. Verified: without a cap C=1→2.16 s, C=2→1.08 s, C=4→0.54 s; with a
  2 MiB/s cap C=1..4 ≈ 4.0–4.2 s. Both throttles supported. Calibrated to the
  frozen server (`1 MiB@512 KiB/s ≈ 2.05 s`).
- **Decision-interval experiment:** interval 2 → 3.62 s / 0.42 churn·MiB⁻¹;
  **interval 4 → 2.89 s / 0.09**; interval 8 → **2.27 s / 0.06 (selected)**.
- **Training / selection:** seeds 0–4 (1500 episodes); selected **seed 1** by
  validation only. Candidate SHA-256
  `227d9845096b5d1382c58cc9080e5907fc0b81cba7dc04546c5eaa5b3ecf4ad5`;
  same-seed retrain is byte-identical (deterministic).
- **Held-out simulator (mean completion s, all policies at 1 req/MiB):** DQN
  **2.96 s (avg concurrency 2.97)** vs fixed 4.10 (static C≈2.5) and
  concurrency-adaptive 4.10. Caveat: the scripted baselines are inert in the
  analytic simulator (their throughput trigger rarely fires), so the comparison is
  mostly against a static concurrency. Ablations: DQN 2.96 < random 4.87 <
  shuffled 4.10; corrupted-state 2.92 ≈ DQN, indicating the learned advantage is
  largely "hold concurrency ≈3" rather than strongly state-conditional.
- **Request efficiency:** **1.0 request/MiB** for every policy (1 MiB chunks) — the
  v2/v3 fragmentation is eliminated by construction.
- **Live transfer (25 runs each, real controlled server, chunk pinned 1 MiB for
  v4):**

  | scenario | v2 dur/att/C | v3 dur/att/C/chunk | **v4 dur/att/C/chunk** |
  |---|---|---|---|
  | baseline | 1171/109/1.0 | 1160/87/1.0/32768 | **841/4/3.0/1048576** |
  | throttle-wallclock | 1499/75/1.4 | 1918/37/1.0/458752 | **2634/4/3.0/1048576** |
  | latency | 1993/66/1.6 | 3723/76/1.0/163840 | **1009/4/3.0/1048576** |
  | loss | 1227/71/2.0 | 1485/81/1.0/131072 | **845/4/3.0/1048576** |
  | combined | 2573/66/4.0 | 5997/75/1.0/91750 | **3355/4/3.2/1048576** |

  **25/25 valid.** v4 makes **exactly 4 requests of 1 MiB** (aria2's granularity)
  and holds **concurrency 3** (occasionally 4) — no fragmentation, no chunk drift,
  and faster than v3 everywhere and than v2 in baseline/latency/loss.
- **Success criteria (§18):** met — request granularity comparable to aria2;
  stable concurrency control; no artificial chunk drift; sensible latency/loss
  behavior; improved sim-to-real agreement over v3. The throttle-wallclock run is
  slower than v2 (2634 vs 1499 ms) because v4 pins 1 MiB chunks and cannot adapt
  granularity — expected and documented.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v4/{simulator_spec_v4.json,
  train_config_v4.json, decision_interval_results_v4.json, seed_results_v4.json,
  validation_results_v4.json, heldout_test_results_v4.json, ablations_v4.json,
  live_transfer_results_v4.json, final_dqn_candidate_v4.json,
  final_dqn_candidate_v4.sha256, sim_to_real_v4.json, report.json, report.txt}`.
  Code: `python/train_validate_v4.py`, `python/rl/sim_env_v4.py`; Go
  `internal/rl/dqn/{policy,live}.go` (concurrency-only action space) and
  `cmd/dqn-transfer` (`-fixed-chunk`). Candidates v1/v2/v3 untouched.
- **Limitations:** synthetic simulator; the real server models no chunk-size
  tradeoff (now irrelevant because chunk is pinned); live n=5; approximate live
  observability; the simulator baselines are weak; concurrency is held near 3
  rather than strongly condition-dependent in short runs; **no aria2 comparison
  and no performance claim yet.**

### 22.29 DQN Candidate v5 (concurrency-only, state spec v2) — simulator/state audit and fixes

**Status: experimental, NOT frozen. No v5 training/validation/live-transfer run
exists; no v5 model artifact has been produced.** This subsection records the
first full audit of the v5 simulator/state/live contract and the correctness
fixes it produced. The v5 formulation itself (16 MiB default horizon, fixed
1 MiB chunk, concurrency 1–4, actions `none / increase-concurrency /
decrease-concurrency`, Double DQN, state spec version 2 with 13 temporal
features, dynamic within-episode condition transitions, decision interval
candidates {1,2,4}) is unchanged.

- **Files:** `python/rl/sim_env_v5.py`, `python/rl/state_v5.py`,
  `internal/rl/dqn/state_v5.go`, `internal/rl/dqn/live.go`; tests
  `python/tests/test_sim_env_v5.py`, `python/tests/test_state_v5.py`, and the v5
  tests in `internal/rl/dqn/state_v5_test.go`.
- **No v5 artifacts yet:** there is no `testdata/diagnostics/dqn_validation_v5/`,
  no `simulator_spec_v5.json`, and no `static_concurrency_sweep_v5.json`; the
  simulator docstring references to the latter were corrected to state that the
  sweep/spec are still to be generated (no measured claim).
- **Bugs found and fixed (training/live contract only; frozen v1–v4 artifacts
  untouched):**
  1. **Decision interval suppressed concurrency.** The old step ran
     `min(concurrency, decision_interval)` requests as a fresh serial wave and
     charged `max(service_time)` per wave, so with `decision_interval <
     concurrency` extra workers were idle and strictly harmful, while the state
     feature `aggregate_throughput` still advertised `C*n/svc`. The step now
     accounts elapsed time as continuous-worker pipeline time
     `sum(service_times)/C` (using at most the remaining requests when the file
     finishes inside the step), so concurrency changes the aggregate rate for
     every candidate interval. Requests per decision remain
     `min(decision_interval, remaining_reqs)`.
  2. **Wall-clock transitions were not applied mid-step.** Phase lookup used
     `self.elapsed`, advanced only after the whole step, despite a per-wave
     re-evaluation comment. Phase selection now uses the elapsed time at each
     request's start (`self.elapsed + accrued`), and `transitions` counts
     boundaries crossed.
  3. **`request_duration_slope` was identically zero.** `_obs` returned
     `self.prev_dur` for both `request_duration_s` and `prev_duration_s`. The
     simulator now tracks `last_dur` (most recent request service time) and
     `prev_dur` (previous step) separately, matching Go `ObservationV5`.
  4. **Window counters were per decision interval, not per attempt.**
     `window_attempts` counted intervals and the failure/retry/completion
     windows stored 0/1 per interval. They now push one sample per request,
     matching the feature definitions and the Go live contract.
  5. **Live progress was fabricated.** `LivePolicy.doneBytes` used
     `SuccessfulRequests * BytesTransferred` (successes × last request bytes),
     which is wrong on failures, partial final ranges, and varying sizes, and it
     feeds `remaining_bytes_frac`. Live progress is now the accumulated
     `BytesTransferred` of successful attempts (one decision per completed
     attempt; a failed attempt never advances it).
  6. **Python export could not produce a Go-valid v5 model.**
     `python/rl/state_v5.py` omitted `min_chunk_size`/`max_chunk_size`, which Go
     `StateSpec.Validate` requires, and `python/rl/model.py` always exported the
     v1 spec/feature names. The v5 spec now carries 1 MiB chunk bounds and
     `export_model` selects spec/feature names from the environment config
     (`SimEnvV5` records its resolved spec on the config).
- **Verified consistent (unchanged):** feature order, normalization, and spec
  version match Go `FeatureNamesV5`/`FeaturesV5`/`DefaultStateSpecV5`; the
  concurrency-only action space is 3 actions mapped by
  `ConcurrencyIndexToAction`; reward remains `-elapsed` with gamma 1; the
  simulator is deterministic for a fixed seed. A shared exact feature-vector
  contract test pins Python and Go together.
- **Tests:** `TestV5FeatureParityContractExact`,
  `TestV5LivePolicyProgressFromSuccessBytes` (Go) and `test_state_v5.py` /
  `test_sim_env_v5.py` (Python: interval/no-suppression, pipeline accounting,
  requests-per-step, mid-step transition, per-attempt windows, duration slope,
  determinism/truncation, export contract).
- **Validation:** `go test ./...`, `go test -race ./...`,
  `go test -count=5 ./internal/rl/... ./internal/downloader/...
  ./internal/experiment/...`, `go vet ./...`, `gofmt -l .`, `go build ./...`,
  and `python3 -m pytest python/tests -q` (29 passed) all pass. Frozen candidates
  v1–v4 and the whole `testdata/` manifest (155 files) are byte-identical.
- **Next step (now done):** the v5 training/validation pipeline is implemented
  and run in §22.30 (multi-seed training, validation-only selection, held-out
  test vs fixed C, behavior diagnostics, pre-specified constant/ramp gate, one
  exported candidate). Live transfer and any comparison remain separate tasks.
  No v5 performance claim exists.

### 22.30 DQN Candidate v5 training + held-out validation (simulator only)

**Status: pipeline complete; ONE experimental Candidate v5 model exported and
audited. NOT a performance claim; no live transfer, no aria2 comparison.** This
stage implements the v5 training/validation pipeline on top of the audited
simulator/state (§22.29). The model is behaviorally adaptive (passes the
pre-specified constant-policy gate) but is **generally slower than the best
fixed concurrency** on the held-out conditions, so it is **not** ready for a
head-to-head claim.

- **Code:** `python/train_validate_v5.py` (new). Reuses the existing Double DQN
  (`python/rl/dqn.py`), export (`python/rl/model.py`), and the v5
  simulator/state; adds additive per-episode `returns`/`episode_steps` to the
  training report. `internal/rl/dqn/v5_candidate_smoke_test.go` (new) loads and
  validates the exported artifact and runs deterministic v5 greedy inference in
  Go (skips when absent; override with `DQN_V5_CANDIDATE`).
  `cmd/dqn-transfer` now dispatches on `StateSpec.Version` so a v5 model uses
  `NewLiveConcurrencyPolicyV5`; previously a 3-action v5 model would silently
  use the v1 observation layout (both accept 13 features), which is now covered
  by `TestNewLivePolicyForModelDispatchesV5`.
- **Reproduce:** `python3 python/train_validate_v5.py` (defaults: 1500
  episodes/seed, seeds 0–4, intervals 1,2,4, out
  `testdata/diagnostics/dqn_validation_v5`). Smoke:
  `python3 python/train_validate_v5.py --out testdata/diagnostics/dqn_validation_v5_smoke
  --episodes 30 --seeds 0 --intervals 2 --val-seeds 100 --test-seeds 200
  --sweep-seeds 300`.
- **Training configuration:** state spec version 2, fixed 1 MiB chunk,
  concurrency 1–4, 3 actions (`none`/`increase-concurrency`/
  `decrease-concurrency`), v5 randomized ranges (rate 256 KiB–8 MiB/s, link
  multiple 0–4× rate, latency 0–80 ms, loss 0–4 %, congestion 0–0.06, file
  4–32 MiB, 1–3 phases, phase durations 0.3–2.0 s). Training is not tuned on
  the live scenarios.
- **Seed separation (deterministic):** training 0–4; validation 100–102; test
  200–203; static sweep 300–302; randomized eval 400–404. The test seeds and
  conditions are never used for selection.
- **Selection:** decision interval chosen on VALIDATION only — interval 4
  (val mean 2.316 s) vs 1 (2.760 s) and 2 (2.850 s); seed 0 selected on
  validation mean (2.293 s), tie-break lowest seed.
- **Static concurrency sweep (prerequisite; simulator only):**
  `static_concurrency_sweep_v5.json`/`.txt` — best fixed C differs across the 8
  regimes: C=1 (`shared-cap-hard`), C=2 (`combined-mild`), C=3
  (`shared-cap-moderate`), C=4 (stable/low-bandwidth/latency/throttle), so
  distinct best C = {1,2,3,4}. Adaptive concurrency is therefore meaningful in
  the simulator; no simulator change was made to obtain this.
- **Held-out TEST (8 conditions × 4 seeds; mean completion ms; descriptive):**

  | condition | DQN | C=1 | C=2 | C=3 | C=4 | best | DQN−best |
  |---|---|---|---|---|---|---|---|
  | unseen-low-bandwidth | 7607.2 | 21333.3 | 10706.7 | 7164.4 | **5393.3** | C4 | +2213.9 |
  | unseen-high-bandwidth | 631.4 | 1333.3 | 826.7 | 657.8 | **573.3** | C4 | +58.1 |
  | unseen-latency | 1093.3 | 2960.0 | 1560.0 | 1093.3 | **860.0** | C4 | +233.3 |
  | unseen-loss | 936.7 | 2312.5 | 1236.2 | 877.5 | **698.1** | C4 | +238.5 |
  | unseen-latency+loss | 1547.8 | 3900.0 | 2084.2 | 1442.8 | **1122.1** | C4 | +425.7 |
  | unseen-throttle-up | 2351.7 | 5500.0 | 2910.0 | **2046.7** | 2590.0 | C3 | +305.0 |
  | unseen-throttle-down | 2837.5 | 22500.0 | 7610.0 | 3913.3 | **2065.0** | C4 | +772.5 |
  | unseen-three-phase | 4089.2 | 20396.7 | 10191.7 | 5456.7 | **4089.2** | C4 | +0.0 |

  DQN matches/beats the best fixed C on **1/8** conditions (a tie). Descriptive
  only (means; n=4 per condition).
- **Behavior diagnostics:** action fraction none/increase/decrease =
  0.152/0.707/0.141; concurrency time fractions C2 0.213 / C3 0.334 / C4 0.454
  (C1 0.000); mean effective changes/episode 2.00; constant-episode fraction
  0.062; ramp-to-max fraction 0.219; changes before/after first transition =
  51/13 (6 transition reactions within 3 decisions); dominant C by condition is
  C=3 in 6/8 and C=4 in 2/8 (three-phase, throttle-down) → **weakly
  condition-dependent**; the policy never settles on C=1/C=2 as a dominant
  value. Randomized-distribution eval (seeds 400–404): mean 3.254 s, constant
  fraction 0.20, 3.6 changes/episode, dominant C=4.
- **NOT-READY gate (pre-specified):** NOT READY if constant-episode fraction
  ≥ 0.9, or one C's time fraction ≥ 0.9, or ramp-to-max fraction ≥ 0.9, or zero
  effective changes. Result 0.062 / 0.454 / 0.219 / 64 changes →
  **READY_FOR_NEXT_STAGE** (behavioral gate only; not a performance claim).
- **Candidate v5 (experimental, simulator-selected):** SHA-256
  `3a927a58baa7f9b5f150827ba0bd81defc95adb25e5328eb037f5edf9925da75`; state
  spec v2, 13 features, 3 actions. An independent run
  (`--seeds 0 --intervals 4`) reproduces the byte-identical hash. The Go smoke
  test loads it, validates the contract and SHA, and infers deterministically.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v5/` —
  `final_dqn_candidate_v5.json`/`.sha256`, `train_config_v5.json`,
  `simulator_spec_v5.json`, `seed_results_v5.json`,
  `validation_results_v5.json`, `decision_interval_results_v5.json`,
  `training_episodes_v5.json`, `heldout_test_results_v5.json`,
  `diagnostics_v5.json`, `static_concurrency_sweep_v5.json`/`.txt`,
  `report.json`, `report.txt`; smoke copy in `dqn_validation_v5_smoke/`.
- **Caveats:** simulator only; no real-downloader transfer and no aria2
  comparison; the model underperforms the best fixed C on 7/8 held-out
  conditions; adaptation is weak (dominant C usually 3/4, never 1/2); the v5
  reward/credit and live-observability caveats from §22.22–§22.29 remain; means
  are descriptive (n=4).
- **Next step (now done):** live transfer of Candidate v5 on the real
  controlled downloader is completed in §22.31. A pre-specified head-to-head
  against the frozen aria2 v2 baseline remains separately scoped and is NOT run.

### 22.31 Candidate v5 live transfer validation (controlled server)

**Status: completed. 125/125 runs valid; the three pre-specified gates pass;
NO aria2 comparison, NO performance/superiority claim, NO real-network
generalization.** This validates whether the simulator-trained Candidate v5
transfers its behavior to the real controlled downloader/server (16 MiB file,
1 MiB fixed chunk, wall-clock conditions).

- **Harness:** `cmd/dqn-live-v5/` (new, experiment-only; it does not modify the
  downloader, policy runner, controllers, model, or simulator). It reuses the
  existing `downloader`, `experiment/server` (request log + wall-clock
  `RateSchedule`), `experiment/network` (tc/netem on `lo`, per-run apply/clear),
  `metrics`, and `dqn.LivePolicy` via `NewLiveConcurrencyPolicyV5`.
  Authoritative concurrency is derived from the controller target reported at
  each decision: applied-after(action k) = `ConcurrencyBefore` of decision k+1
  (or the final controller target); the model's own pre-application
  `Decision.ConcurrencyAfter` is deliberately NOT used.
- **Matrix:** 5 scenarios (`baseline`, `server-throttle-wallclock`,
  `latency` 20 ms, `loss` 1 %, `combined` 1 MiB/s + 20 ms + 1 %) × 5 policies
  (`fixed-1..4`, `v5`) × 5 reps = **125 runs**, deterministic run order
  (`-order-seed 20260925`), fresh server/output/downloader/netem per run. The
  rate transition is wall-clock (512 KiB/s → 4 MiB/s after 1 s), never
  request-count based. All policies share server, rates, netem, retries (3),
  timeout (30 s), and 1 MiB granularity.
- **Reproduce:** `go run ./cmd/dqn-live-v5 -out
  testdata/diagnostics/dqn_validation_v5_live -sudo`.
- **Validity (GATE A):** 125/125 valid, 0 invalid, 0 network-unavailable (all
  netem runs `applied`; qdisc clean afterwards). Per-run checks: exact 16 MiB
  size and SHA-256 content match; successful ranges tile `[0,16 MiB)` (no
  gaps/overlaps); `sum(success bytes) == 16 MiB`; model-reported remaining bytes
  match recorded successful bytes per attempt; concurrency stays in [1,4];
  requested actions stay in {none, increase, decrease}; no failed attempt
  contributes completed bytes; retries 0.
- **Observed request behavior:** every run issued exactly 16 × 1 MiB range
  requests (no remainder, 0 retries). In the transition scenario the server log
  shows exactly 1 request paced at 512 KiB/s and 15 at 4 MiB/s per run,
  confirming the wall-clock transition was observed, not inferred.
- **Fixed-C results (mean completion ms; descriptive, not a ranking):**

  | scenario | C=1 | C=2 | C=3 | C=4 | v5 |
  |---|---|---|---|---|---|
  | baseline | 4445.6 | 2262.2 | 1731.3 | **1170.0** | 1719.9 |
  | server-throttle-wallclock | 6248.0 | 4041.3 | 3499.1 | **2947.2** | 3494.9 |
  | latency | 5202.8 | 2695.6 | 1985.8 | **1391.6** | 2039.9 |
  | loss | 4614.4 | 2268.9 | 1724.6 | **1244.7** | 1735.2 |
  | combined | 17451.0 | 8994.3 | 6558.1 | **4486.0** | 6557.7 |

  v5 tracks fixed-C3 closely (within ~3 % in four scenarios) and is slower than
  fixed-C4 everywhere; SDs are small (v5 12–40 ms). The real server paces each
  response independently (no shared bottleneck), so C=4 is the per-scenario
  optimum here; this harness therefore cannot distinguish shared-cap regimes
  that the simulator models.
- **v5 actual concurrency behavior:** non-constant everywhere (0 constant
  runs), no ramp-to-max-only run, no monotone-decrease run; mean actual
  changes/run 5.2–11.2 (increases ≈ 60 %, decreases ≈ 40 %), i.e. a noisy ramp
  to C3–C4 followed by ±1 oscillation from the one-decision-per-completion
  credit lag. Final C is 3 in baseline/latency/throttle (5/5 each), 3 in loss
  (4/5; one run C2), and 4 in combined (4/5). Dominant actual concurrency by
  time is C4 in four scenarios (fraction 0.59–0.67) and C1 in the throttle
  scenario (0.59, because the single slow first request holds C1 for the first
  ~2.1 s).
- **Transition reaction (GATE C):** all 5 valid v5 transition runs establish the
  concurrency in effect at the transition (C1), the first decision after it
  (~2067 ms; requested `increase-concurrency`), the applied change (C1→C2), and
  the reaction latency (median 1067 ms). The latency floor is the in-flight
  1 MiB @ 512 KiB/s request duration minus the 1 s transition, i.e. the policy
  reacts at the first completion after the transition. Post-transition mean
  concurrency 2.49.
- **Gates (pre-specified in the harness before the campaign):**
  - **GATE A (technical validity): PASS** — 125/125 valid, 0 exclusions.
  - **GATE B (genuine adaptation): PASS by the pre-specified rule** —
    constant fraction 0.00, ramp-to-max 0.00, monotone-decrease 0.00, worst
    dominant time fraction 0.674, 2 distinct dominant actual concurrency
    values. **Qualitative Task-5 reading: the transfer is adaptive but shallow.**
    The policy ramps to C3–C4 and oscillates in every scenario; it does not
    hold low concurrency anywhere, and the between-scenario difference is
    driven by the transition scenario's slow first request (time-weighted C1),
    not by deliberate condition discrimination. The two distinct dominant
    values do not by themselves demonstrate condition-dependent adaptation.
  - **GATE C (reaction observability): PASS** — complete reaction logs for 5/5
    transition runs; all 5 reacted at the first post-transition completion.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v5_live/` —
  `live_manifest_v5.json` (locked matrix, model hash, gate definitions, run
  order), `live_runs_v5.json` (+ `.sha256`; raw per-run decisions with
  authoritative before/after concurrency and timestamps, metrics, server
  request log with per-request rate and start/end timestamps, validation),
  `live_aggregate_v5.json` (per-scenario/policy stats + paired per-rep
  observations), `live_adaptation_v5.json` (trajectory classes, dominant
  concurrency, reactions), `live_gates_v5.json`, `live_validation_v5.json`,
  `live_report_v5.txt`. Smoke copy in `dqn_validation_v5_live_smoke/`.
- **Frozen artifacts:** v1–v4 candidates and the pre-existing `testdata/`
  manifest (155 files) are byte-identical; aria2 v2 calibration untouched.
- **Next step (now done):** the pre-specified head-to-head of Candidate v5
  against the frozen aria2 v2 baseline is completed in §22.32. Passing these
  gates did not establish superiority over aria2.

### 22.32 Final head-to-head: DQN Candidate v5 vs frozen aria2 baseline v2

**Status: completed. The pre-specified outperformance criteria were NOT met;
v5 was slower than aria2-F in every tested condition. Reported as measured; no
tuning and no post-hoc scenario changes.** This is the final evaluation of the
two frozen configurations.

- **Frozen configurations:** DQN Candidate v5, SHA-256 `3a927a58…` (state spec
  v2, 13 features, 3 actions, fixed 1 MiB chunk, concurrency 1–4); FINAL ARIA2
  BASELINE CONFIG v2 / candidate F
  (`testdata/diagnostics/aria2_calibration_v2/selected_baseline_v2.json`,
  SHA-256 `b7249817…`; aria2 1.37.0, `--max-connection-per-server=16
  --split=16 --min-split-size=1048576`). Both hashes were verified before the
  campaign; neither was modified.
- **Matrix (reused methodology):** the established U1–U8 wall-clock unstable
  profiles from §22.26 at the frozen **16 MiB** v5/aria2-v2 scale (the only
  deliberate change vs §22.26's 4 MiB, applied identically to both systems);
  30 matched pairs per condition = **240 pairs / 480 downloads**; deterministic
  per-pair order (seed `20260925`); fresh server/output/netem per run; per-run
  netem apply/clear; wall-clock `RateSchedule` transitions only.
- **Harness:** new `cmd/final-evaluation-v5/` (experiment-only) reuses the
  §22.26 pair structure/order/conditions and the validated v5 live path
  (`NewLiveConcurrencyPolicyV5`, 1 MiB fixed chunk, C 1–4), and runs aria2-F from
  the hash-verified frozen command (parsed to argv; candidate-F flags asserted,
  version asserted). Statistics reuse
  `python/analyze_final_evaluation.py` unchanged (matched pairs; effect =
  log(v5/aria2); one-sided paired permutation test; 95 % t CI; Holm across 8
  conditions); the analyzer's established `dqn` key denotes Candidate v5.
  `python/report_final_evaluation_v5.py` writes the human-readable report from
  the frozen artifacts.
- **Reproduce:** `go run ./cmd/final-evaluation-v5 -out
  testdata/final_evaluation_v5 -sudo`; then
  `python3 python/analyze_final_evaluation.py --dir testdata/final_evaluation_v5`
  and `python3 python/report_final_evaluation_v5.py --dir testdata/final_evaluation_v5`.
- **Validity:** **480/480 runs valid, 0 invalid, 240/240 valid pairs, 0
  exclusions.** Every run: no error, exactly 16 MiB output with SHA-256 equal to
  the served content, v5 range-tiling/success-bytes/progress/action/concurrency
  checks pass, and every requested netem condition applied; qdisc clean.
- **Per-condition matched-pair results (mean ms; ratio = geometric v5/aria2):**

  | cond | v5 mean | aria2 mean | ratio | ratio 95 % CI | p_holm |
  |---|---|---|---|---|---|
  | U1 | 3262.9 | 1485.5 | 2.199 | [2.164, 2.234] | 1 |
  | U2 | 2336.6 | 785.2 | 2.977 | [2.943, 3.012] | 1 |
  | U3 | 1982.8 | 754.2 | 2.799 | [2.463, 3.181] | 1 |
  | U4 | 3922.6 | 1907.2 | 2.086 | [1.943, 2.240] | 1 |
  | U5 | 4221.9 | 2865.2 | 1.493 | [1.397, 1.594] | 1 |
  | U6 | 3004.9 | 1118.7 | 2.686 | [2.667, 2.705] | 1 |
  | U7 | 2288.9 | 891.2 | 2.808 | [2.411, 3.270] | 1 |
  | U8 | 3432.5 | 1806.7 | 1.962 | [1.787, 2.155] | 1 |

- **Pooled:** v5 mean 3056.6 ms vs aria2 1451.7 ms; geometric ratio **2.3217**
  (95 % CI [2.2269, 2.4206]); relative reduction −132.2 % (negative = v5
  slower); one-sided paired permutation p = 1.0; v5 faster in **1/240 pairs
  (0.4 %)** and **0/8 conditions**. All four pre-specified claim criteria false
  → `claim_supported = false`.
- **v5 live behavior (authoritative controller state):** max concurrency 4 in
  every run; time-weighted mean 2.702; mean 7.4 effective changes/run; final
  concurrency C3 180/240, C4 43, C2 17; constant-run fraction 0.000; ramp-to-max
  fraction 0.021. Transition reaction latencies (transition → first effective
  change): U1 566 ms, U2 152 ms, U6 733 ms, U7 665 ms, U8 316 ms.
- **aria2-F observed behavior:** max in-flight mean 15.82 / max 16 (204/240 runs
  reached 16); 15–16 × 1 MiB range requests per run plus 1 initial non-range
  probe; 0 aria2-reported retries; 245 server-recorded failed requests across
  480 runs (the aborted initial probe); outputs SHA-correct in all runs.
- **STRUCTURAL ASYMMETRY (mandatory):** v5 is capped at **4** concurrent
  requests while aria2-F uses up to **16** configured connections. Candidate F
  was selected by the v2 calibration precisely because
  `--min-split-size=1048576` requires ≥16 connections to obtain the intended
  ~16 × 1 MiB split at 16 MiB. The controlled server paces each response
  independently, so aria2-F can exploit up to 16 independently paced responses
  while v5 can exploit at most 4. This is a comparison of two complete frozen
  configurations, **not** a capability-matched comparison; the 2.32 ratio must
  not be read as an algorithmic effect isolated from the asymmetry.
- **Interpretation (evidence-supported):** under the tested controlled
  conditions and methodology, **v5 did not outperform aria2-F and was slower in
  all 8 conditions**; no universal, consumer-network, optimality, convergence,
  or real-network generalization claim is made. The negative §22.26 result
  (Candidate v2 vs aria2 v1) and the shallow v5 adaptation finding (§22.31)
  remain unchanged and are not reinterpreted.
- **Artifacts:** `testdata/final_evaluation_v5/` — `final_test_matrix_v5.json`
  (+ `.sha256`), `execution_schedule.json`, `raw_results.json` (analyzer input,
  valid pairs), `raw_results_all.json` (all 480 runs + telemetry),
  `request_traces.json`, `validity_report.json`, `asymmetry_report.json`,
  `manifest_v5.json`, `statistical_analysis.json`, `claim_assessment.json`,
  `summary.txt`, `per_condition_table.txt`, `report.txt`, `run.log`,
  `artifact_hashes.json`. The §22.26 `testdata/final_evaluation/` directory and
  all other frozen artifacts are untouched.
- **Limitations:** controlled synthetic server; single 16 MiB file; the 8
  U-profiles; 30 pairs per condition; the 4-vs-16 concurrency asymmetry;
  aria2's initial non-range probe; no real-network or IDM comparison; no
  fairness signal.

### 22.33 Candidate v6 pre-registration (written BEFORE training)

**Status: pre-registered; the pipeline has since run (see §22.34).** This section fixes the
conditions, baselines, metrics and gates before any v6 training or run, so they
cannot be tuned after seeing outcomes.

**v5 failure audit (evidence from §22.30–§22.32; v5 is NOT modified):**
- v5 concurrency was capped at **4**; frozen aria2-F used up to **16**
  connections.
- the live controlled server paces **each response independently** with no
  shared bottleneck, so 16 independently paced responses were strongly rewarded
  and fixed-C4 was fastest in every tested condition; there was no interior
  concurrency tradeoff for the policy to learn.
- v5 mostly **ramped to C3–C4 and oscillated** (time-weighted mean 2.70; mean
  7.4 changes/run; constant 0.000; ramp-to-max 0.021) rather than reacting
  discriminatively to regime changes; the only between-condition difference was
  timing-driven (the wall-clock transition scenario's slow first request).
- v5 trained with simulator `decision_interval=4` while the live runner decided
  once per completed attempt — a **cadence mismatch** (v5's timing mechanics
  were corrected, but the cadence was not aligned).
- v5's feature layout/normalization/parity contract is otherwise validated and
  is reused as the basis for v6 (with a new spec version for the wider range).

**v6 formulation (fixed before implementation):**
- 16 MiB evaluation horizon; 1 MiB fixed chunk; **concurrency 1–16**.
- actions `0 = none`, `1 = increase-concurrency`, `2 = decrease-concurrency`
  (±1, no new action kinds, no target-setting actions).
- Double DQN; temporal 13-feature state, **state spec version 3**, same
  documented feature layout as v5 but concurrency normalized over [1,16].
- reward = **negative wall-clock elapsed seconds** of the decision interval
  (`r_t = -elapsed_t`, gamma 1); no throughput/latency/loss bonuses, no
  condition labels, no hard-coded "latency ⇒ decrease" mapping; the policy sees
  only transfer outcomes.
- **decision cadence = one decision per completed request** in both simulator
  and live runner (`decision_interval = 1`). The live runner already decides
  once per completed attempt; the simulator is fixed to the same cadence so the
  two are identical by construction. Interval candidates {1,2,4} are therefore
  not evaluated for v6: the live runner's cadence pins the contract to 1.

**Environment (genuine shared bottleneck, not independent pacing):**
- per-phase shared capacity `B(t)` (the simulator's `link_bandwidth`): each
  active transfer receives `min(per-response rate, B/C)`, so increasing C has
  diminishing/negative value when B binds; per-connection overhead and
  concurrency-dependent loss/congestion add further cost.
- server-side rate schedules model throttling independent of request count.
- live experiments reuse the existing controlled server and tc/netem
  (bandwidth/latency/loss; mid-run re-apply at phase boundaries) rather than a
  separate framework. These are controlled unstable-network profiles, not a
  claim about all mobile networks.

**Pre-registered trajectories (3 wall-clock phases; training durations
randomized in a fixed 1.5–3.0 s range per phase; held-out durations fixed and
documented per condition):**
- **A** bandwidth collapse/recovery: B high → B low → B high.
- **B** latency spike/recovery: latency low → high → low.
- **C** burst-loss episode: loss low → elevated → low.
- **D** server throttle/release: server rate high → severe throttle → recovered.
- **E** combined deterioration/partial recovery: good → lower B + higher
  latency + loss → partial recovery.

**Gates (fixed now; not changed after results):**
- **GATE A — environment validity:** the static C=1..16 sweep over
  pre-registered regimes shows multiple meaningful concurrency optima (not all
  C16). If it fails, v6 training does not start.
- **GATE B — training validity:** v6 beats the best fixed-C baseline on
  held-out dynamic simulator conditions; selection uses validation data only.
- **GATE C — condition-dependent behavior:** v6 reacts differently to
  materially different trajectories and is neither C16/ramp-locked nor random
  adjacent oscillation.
- **GATE D — live transfer:** the condition-dependent behavior remains
  observable on the controlled live server/network.
Failure of any gate is reported as **NOT READY** with a diagnosis. The final
aria2-F comparison is out of scope until GATE A–D pass.

### 22.34 Candidate v6: training, held-out validation, and live transfer

**Status: the pre-registered pipeline (§22.33) ran to completion; GATES A–D
PASS. v6 is READY for the (not yet run) frozen aria2-F comparison per the
pre-registered gates. No performance/superiority claim; live performance was
worse than fixed concurrency.** All v1–v5 and aria2 artifacts remain untouched.

- **Formulation:** 16 MiB horizon, 1 MiB fixed chunk, concurrency 1–16,
  actions none/increase/decrease, Double DQN, state spec **version 3** (same
  13-feature layout as v5, bounds [1,16]), reward = −interval wall-clock
  seconds, one decision per completed request in both simulator and live
  runner.
- **Candidate:** `testdata/diagnostics/dqn_validation_v6/final_dqn_candidate_v6.json`,
  SHA-256 `daa7176793093bea102965700774609fd70d4bbb60523d2417302e2a7f0d9887`;
  selected seed 2, 1200 episodes/seed, seeds 0–4; Go loads/infers it via
  `NewLiveConcurrencyPolicyV6` (dispatch and Go smoke test added).
- **GATE A (environment):** static C=1..16 sweep over pre-registered regimes →
  best fixed C = {1, 2, 5, 9, 16} (distinct); PASS.
- **GATE B (training):** v6 held-out mean 4.476 s vs best globally-fixed C
  (C12, chosen on validation only) 4.867 s; PASS. Per-condition oracle best
  fixed C (C6/C6/C2/C16/C2) was 0.03–0.33 s faster than v6.
- **GATE C (condition dependence):** constant 0.000, ramp-to-max 0.000, 3/5
  conditions with phase-to-phase time-weighted concurrency spread ≥ 2.0; PASS.
  Simulator phase means: A2 9.55→8.67→6.44, C2 9.03→5.04→7.00, E2 8.63→6.50→
  5.39, D2 ~15 (server throttle holds high concurrency), B2 8.96→8.18 (many
  runs finish before phase 3).
- **GATE D (live transfer):** `cmd/dqn-live-v6` ran the 5 trajectories ×
  {fixed C=1..16, v6} × 3 reps = **255/255 valid** runs on the controlled
  server with mid-run tc/netem qdisc replacement and server rate phases. v6
  live phase means: A2 1.3→2.3→3.6, B2 1.8→~6.2→~6.9, C2 1.2→~2.9→~5.1,
  D2 1.1→2.4→4.0, E2 1.8→5.8→5.6; constant 0.000, ramp 0.000, 4/5 responsive;
  PASS. Transition reaction latencies were observed in every condition.
- **Live performance (descriptive, not a ranking):** v6 mean 7578 ms vs best
  globally-fixed live C (C11) 5012 ms; per-condition best fixed: A2 C4 6588,
  B2 C8 3992, C2 C4 4151, D2 C16 2158, E2 C4 5508 ms. v6 was slower than the
  best fixed C in every live condition; the largest gap was D2 (server
  per-response throttle), where v6 held low/moderate concurrency while C16 was
  far faster. The live server's per-response pacing again rewards high
  concurrency; the simulator's shared-bottleneck tradeoff transfers only
  partially.
- **Implementation notes/fixes:** v6 fixed the inherited tail-accounting for
  the one-decision-per-request cadence (the final request was charged a full
  service time when interval < C, penalizing high C); `model.py` exports v6 by
  spec version; `state_v6.py/go` and `sim_env_v6.py` are additive; the
  `cmd/dqn-transfer` dispatch selects v6 by spec version.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v6/` (static sweep,
  train config/results, held-out, adaptation, gates, report, candidate + sha)
  and `testdata/diagnostics/dqn_validation_v6_live/` (manifest, runs,
  aggregates, adaptation, gates, report); smoke copies under `*_smoke/`.
- **Next step (separate, not run):** the pre-registered frozen aria2-F
  comparison, with the structural asymmetry (v6 ≤16 vs aria2-F 16 connections)
  and the live fixed-C result documented; it must not be represented as a
  capability-matched comparison.

### 22.35 Final pre-registered head-to-head: v6 vs frozen aria2-F

**Status: completed. The pre-specified v6 advantage under unstable
trajectories was NOT supported; pooled and per-trajectory results are reported
as measured.** Frozen candidates only; no tuning; no post-hoc changes.

- **Frozen systems:** v6 SHA-256
  `daa7176793093bea102965700774609fd70d4bbb60523d2417302e2a7f0d9887`; aria2-F
  metadata SHA-256 `b72498176a48f889cd2d1dbc9d9ef6b9afc668f2cf0d902e44d51dee0dedca02`
  (aria2 1.37.0, 16 connections / split 16 / min-split 1 MiB). Both verified
  before the campaign; neither modified.
- **Matrix:** the five pre-registered v6 trajectories A2–E2 (three wall-clock
  phases each, §22.33) × 30 matched pairs = **150 pairs / 300 downloads**, at
  16 MiB / 1 MiB fixed chunk; deterministic per-pair order (seed 20260925);
  fresh server/output/netem per run with mid-run tc/netem qdisc replacement and
  server rate phases.
- **Harness/statistics:** `cmd/final-evaluation-v6` (v5 matched-pair structure
  + v6 phase scheduler); `python/analyze_final_evaluation.py` reused with an
  additive `--conditions` flag (default unchanged; majority criterion
  generalized to >half of the conditions); `python/report_final_evaluation_v6.py`
  writes the human-readable report.
- **Validity: 300/300 runs valid, 150/150 pairs, 0 exclusions** (exact 16 MiB +
  SHA-256 match, no errors, netem applied, qdisc clean).
- **Per-trajectory matched pairs (mean ms; ratio = geometric v6/aria2):**

  | traj | v6 | aria2 | ratio | ratio 95 % CI | p_holm |
  |---|---|---|---|---|---|
  | A2 (bandwidth collapse/recovery) | 8313.5 | 7855.4 | 1.065 | [1.017,1.116] | 1 |
  | B2 (latency spike/recovery) | 4932.6 | 4802.7 | 1.027 | [0.995,1.061] | 1 |
  | C2 (burst loss) | 5569.0 | 4828.4 | 1.153 | [1.118,1.190] | 1 |
  | D2 (server throttle/release) | 12043.9 | 2221.5 | 5.471 | [5.205,5.751] | 1 |
  | E2 (combined deterioration) | 6648.2 | 7055.1 | 0.948 | [0.897,1.002] | 0.128 |

- **Pooled:** v6 7501.5 ms vs aria2 5352.6 ms; geometric ratio **1.4560**
  (95 % CI [1.3004, 1.6303]); v6 faster in 37/150 pairs (24.7 %) and 1/5
  trajectories; all four pre-specified criteria false →
  `claim_supported = false`.
- **v6 adaptation (conclusion unchanged):** non-constant/non-ramp; phase mean C
  rises with recovery (A2 1.20→1.94→3.17; B2 1.84→6.15→6.37; C2 1.25→3.16→
  4.78; D2 1.10→2.40→3.93; E2 1.79→4.93→4.80); transition reaction medians
  213/196/299/1579/508 ms. Adaptation was observable but did not translate into
  lower completion time than aria2-F except (non-significantly) on E2.
- **aria2 behavior:** max in-flight mean **15.85** (max 16); 15.01 × 1 MiB
  range requests/run plus 1.00 non-range probe; 0 retries; 146 server-recorded
  failed requests (aborted probes); all outputs SHA-correct.
- **Remaining asymmetry (documented, not hidden):** both systems nominally
  allow 16 concurrent requests and both use ~1 MiB ranged requests, but aria2
  opens up to 16 connections immediately while v6 starts at C=1 and ramps;
  aria2 also issues one initial non-range probe.
- **Evidence-supported conclusion:** across the five pre-registered controlled
  unstable trajectories, **v6 did not show a measurable completion-time
  advantage over aria2-F** (pooled ratio 1.456; slower on A2/B2/C2/D2; ~5.2 %
  faster on E2, Holm p = 0.128, not significant). No general-superiority,
  mobile-network, or real-world generalization claim is made.
- **Artifacts:** `testdata/final_evaluation_v6/` — matrix + sha, schedule,
  `raw_results.json` (analyzer input), `raw_results_all.json` (full telemetry),
  `validity_report.json`, `manifest_v6.json`, `statistical_analysis.json`,
  `claim_assessment.json`, `summary.txt`, `per_condition_table.txt`,
  `report.txt`, `artifact_hashes.json`.

### 22.36 Long-duration diagnostic: frozen v6 vs aria2-F

**Status: completed (diagnostic only). It does NOT modify the v6 final result
(§22.35) and involved no training, no model/simulator changes, and no new
scenarios.** Question: does v6's disadvantage shrink as the download becomes
long enough to amortize its startup/adaptation cost?

- **Frozen systems:** v6 SHA-256
  `daa7176793093bea102965700774609fd70d4bbb60523d2417302e2a7f0d9887`; aria2-F
  metadata SHA-256 `b72498176a48f889cd2d1dbc9d9ef6b9afc668f2cf0d902e44d51dee0dedca02`
  (unchanged).
- **Design (fixed before results):** pre-registered trajectories **A2
  (bandwidth collapse/recovery), D2 (server throttle/release), E2
  (combined deterioration)**, with the **original 2 s phases unchanged**; sizes
  **16, 128, 512 MiB**; 5 matched pairs per size × trajectory = 45 pairs /
  90 downloads; seed 20260925; fresh environment per run. **1 GiB was not run:
  the container has ~1 GB RAM available and a 1 GiB object (plus the server's
  copy) would risk OOM; this resource exclusion is independent of results.**
- **Harness:** `cmd/final-evaluation-v6` gained an additive `-file-size` flag
  (default 16 MiB; the frozen matrix is unchanged); `python/analyze_long_duration_v6.py`
  aggregates the per-size established analyses.
- **Validity: 90/90 runs valid, 45/45 pairs, 0 exclusions** (exact size +
  SHA-256, no errors, netem applied, qdisc clean).
- **Primary metric (geometric v6/aria2 ratio; startup fraction = time to first
  effective v6 change / v6 total):**

  | size | traj | v6 ms | aria2 ms | ratio | 95 % CI | startup frac | post-startup ratio |
  |---|---|---|---|---|---|---|---|
  | 16 MiB | A2 | 8041.6 | 8635.1 | 0.943 | [0.773,1.150] | 0.148 | 0.820 |
  | 16 MiB | D2 | 12260.8 | 2041.0 | 6.004 | [5.835,6.178] | 0.147 | 5.123 |
  | 16 MiB | E2 | 6748.6 | 6480.4 | 1.039 | [0.949,1.139] | 0.080 | 0.960 |
  | 128 MiB | A2 | 38827.8 | 39947.7 | 0.973 | [0.892,1.061] | 0.026 | 0.951 |
  | 128 MiB | D2 | 15834.7 | 13650.2 | 1.160 | [1.139,1.182] | 0.114 | 1.028 |
  | 128 MiB | E2 | 46296.0 | 43549.1 | 1.062 | [0.989,1.141] | 0.013 | 1.051 |
  | 512 MiB | A2 | 149609.4 | 137043.1 | 1.092 | [1.065,1.119] | 0.009 | 1.083 |
  | 512 MiB | D2 | 30316.0 | 53313.7 | 0.568 | [0.554,0.583] | 0.060 | 0.535 |
  | 512 MiB | E2 | 194418.2 | 160454.1 | 1.211 | [1.188,1.235] | 0.004 | 1.207 |
  | 16 MiB | ALL | 9017.0 | 5718.8 | 1.805 | [1.128,2.888] | 0.125 | 2.301 |
  | 128 MiB | ALL | 33652.8 | 32382.3 | 1.062 | [1.008,1.120] | 0.051 | 1.010 |
  | 512 MiB | ALL | 124781.2 | 116937.0 | 0.909 | [0.757,1.092] | 0.024 | 0.941 |

- **Pre-defined verdict:** the pooled ratio moves 1.805 → 1.062 → 0.909 with
  startup falling to 2.4 % of runtime at 512 MiB → the encoded rule returns
  **STRONGLY_PROMISING** (long ratio ≤ 1.0). **Per-trajectory, however, the
  result is MIXED and not robust:** A2 goes 0.943 → 0.973 → **1.092** and E2
  goes 1.039 → 1.062 → **1.211** — v6's disadvantage *grows* with size and
  post-startup v6 remains slower, i.e. CASE B (starts badly and stays
  systematically worse), contradicting the amortization hypothesis for those
  trajectories. The pooled ≤1.0 result is driven entirely by **D2**, which
  reverses 6.004 → 1.160 → **0.568** (at 512 MiB aria2 is unexpectedly slow:
  53.3 s vs v6 30.3 s).
- **Startup-cost analysis:** startup time is indeed amortized (12.5 % → 5.1 %
  → 2.4 % of v6 runtime), so the diagnostic cleanly separates startup from
  steady state. The separation shows the pooled improvement is **not** a
  broadly amortized-startup effect: with startup removed, A2/E2 still worsen
  with size.
- **Caveat (unchanged trajectories):** with absolute 2 s phases the disturbed
  window is fixed, so the disturbed fraction shrinks with size and the
  recovered phase dominates long transfers. A duration diagnostic with
  size-proportional or repeated transitions remains a separately pre-registered
  design, not a change to these frozen trajectories.
- **Evidence-supported conclusion:** making the download longer **does** amortize
  v6's startup, and the pooled ratio crosses 1.0 at 512 MiB, but the advantage
  appears only in D2 while A2/E2 show a growing disadvantage. Longer duration is
  therefore **promising only if the D2 large-size behavior is reproduced and
  explained**; the current evidence does not support a general "longer is
  better" conclusion for v6. This does not modify §22.35.
- **Artifacts:** `testdata/diagnostics/dqn_long_duration_v6/` — per-size
  `size_{16,128,512}MiB/` (matrix, manifest, raw runs, established
  `statistical_analysis.json`/`summary.txt`), plus `long_duration_summary.json`/
  `.txt`, `per_pair_v6_vs_aria2.json`, `report.txt`, `artifact_hashes.json`;
  smoke dirs `dqn_long_duration_v6_smoke_{16,128}`.

### 22.37 D2 mechanism + long multi-transition trajectory diagnostic

**Status: completed. Diagnostic only; no training; no v7; no change to the
§22.35 v6 final result.**

- **D2/512 MiB replicated:** dedicated campaign of **20 matched pairs** (fresh
  server/output/netem per run, deterministic order seed 20260925): v6
  31.14 s vs aria2 53.45 s mean; v6/aria2 geometric ratio **0.5816**
  (95 % CI [0.566, 0.598]); v6 faster in **20/20** pairs; the established
  analysis gives `claim_supported = true` for this single condition.
  **D2 REPRODUCED.**
- **Mechanism (identified from telemetry, not assumed):** on 512 MiB aria2 with
  `--split=16` issues **15 × 32 MiB** range pieces (plus one probe); all start
  before the 2 s transition at the 640 KiB/s initial rate. The server selects a
  request's pacing rate at request start and holds it for the whole request, so
  each 32 MiB piece takes ~51 s. v6 allocates **1 MiB** ranges dynamically:
  only ~7 requests start before recovery and **505/512 start after 4 s** at
  2.5 MiB/s. The reversal is a **request-granularity × phase-locked-pacing
  interaction**, not generic adaptive superiority; at 16 MiB aria2's pieces are
  1 MiB and complete before the first transition, so the effect disappears.
- **New long trajectory family L1** (separate from the frozen A2–E2): seven
  predetermined phases — good (B=6 MiB), severe degradation (B=1 MiB, 3 % loss,
  high latency), recovery (B=4 MiB), server throttle (256 KiB/s per response, no
  shared cap), recovery (B=5 MiB), degradation (B=1.5 MiB), final good — with
  phase durations predetermined in **[12,18] s**, last phase to completion.
- **Static C=1..16 sweeps (frozen v6 simulator only):** per-phase steady optima
  are **C = 1, 2, 5, 16, 5, 3, 1** (distinct best C {1,2,3,5,16}): high C is
  optimal in the server-throttle phase; low/moderate C is optimal under shared
  caps with latency/loss. Full 512 MiB L1 (15 s phases): every policy traverses
  all seven phases; best fixed C=15 (137.8 s) vs C1 (143.7 s) — a **~4 % total
  completion-time spread**, much smaller than the per-phase differences because
  the shared caps bound each phase and all policies see the same sequence.
- **File size for any future live long-duration experiment: 512 MiB** (largest
  RAM-safe size in this container; the trajectory stays dynamic for ~90 s; the
  measured startup fraction at 512 MiB was 2.4 %); 1 GiB remains unsafe.
- **Interpretation:** D2 is reproducible and mechanistically explained. The L1
  environment has genuinely different per-phase preferred concurrency, but the
  full-run completion-time headroom is only ~4 %, so the duration hypothesis is
  **not yet shown to be strongly promising**. Any v7 design must demonstrate it
  can capture that headroom, and must not rely on the D2 granularity effect
  (which v6's fixed 1 MiB chunk already obtains for free).
- **Artifacts:** `testdata/diagnostics/long_duration_d2_diagnostic/` —
  `replication/` (raw runs + established statistics), `smoke/`,
  `d2_mechanism.json`, `long_trajectory_spec.json`,
  `static_sweep_long_trajectory.json`, `report.txt`, `artifact_hashes.json`.

### 22.38 Candidate v7 environment pre-registration: L2 (written BEFORE the sweep)

**Status: pre-registered; measurements follow below in §22.39. No v7 training,
no v6 changes, no new final comparison.**

**Why L1 was weak (quantified in §22.37):** L1's per-phase best C differed
({1,2,3,5,16}), but its full 512 MiB completion times spanned only
137.8 s (C16) – 143.7 s (C1), ≈4 %; the shared caps bound each phase, so all
policies paid the same phase-capacity cost and only overhead/loss terms were
C-dependent. The cumulative payoff from adapting C was therefore small.

**L2 design (fixed before measurement), separate from frozen A2–E2 and L1:**
512 MiB, 1 MiB chunks, C 1–16, seven predetermined phases with durations in
**[18, 25] s** (phases 1–6; phase 7 runs to completion):

| # | regime | rate | shared cap B | latency | loss | congestion | intent |
|---|---|---|---|---|---|---|---|
| 1 | server capacity | 512 KiB/s | 0 | 20 ms | 0.3 % | 0.002 | high C useful |
| 2 | severe shared bottleneck | 8 MiB/s | 1 MiB/s | 120 ms | 3.0 % | 0.03 | low C materially better |
| 3 | recovery | 8 MiB/s | 4 MiB/s | 40 ms | 0.5 % | 0.006 | moderate C |
| 4 | severe server throttle | 128 KiB/s | 0 | 30 ms | 0.5 % | 0.004 | high C useful |
| 5 | heavy congestion | 8 MiB/s | 2.5 MiB/s | 100 ms | 2.0 % | 0.06 | low C materially better |
| 6 | recovery | 8 MiB/s | 5 MiB/s | 30 ms | 0.4 % | 0.004 | moderate/high C |
| 7 | final server capacity | 1 MiB/s | 0 | 20 ms | 0.3 % | 0.002 | high C useful, to completion |

Each regime is a controlled abstraction of a plausible unstable/mobile-like
condition (capacity collapse, severe shared-bottleneck congestion, recovery,
server-side throttling, and a capacity-limited final phase); this is not claimed
to be a literal cellular/train link. No condition labels or reward shaping are
exposed to the policy.

**Sweep and metrics (fixed before measurement):** deterministic simulator
sweeps (seeds 300–302) of the frozen v6 simulator: per-phase steady sweeps
(128 MiB, C=1..16) and the full 512 MiB L2 trajectory (C=1..16) plus a
ramp-aware dynamic oracle (knows each phase's best C and moves C by ±1 per
decision, i.e. within the frozen action semantics). Definitions:
`best_static = min_C full-trajectory completion`;
`dynamic_oracle = completion under the ramp-aware oracle`;
`adaptive_headroom = (best_static − dynamic_oracle) / best_static`.
Threshold rationale: live/run-to-run dispersion in the v6 campaigns was
~1–5 %; headroom < 5 % is treated as too weak, 5–10 % as marginal, and
≥ 10 % as meaningful.

**New long-duration aria2 baseline (experimental, separate from frozen F):**
candidate `--max-connection-per-server=16 --split=512 --min-split-size=1048576`
intended to give ~1 MiB ranges with ≤16 simultaneous connections at 512 MiB.
Part 7 requires empirical verification: if the installed aria2 1.37.0 cannot
produce ~1 MiB ranges under this option set, the outcome is
`NOT_READY_ARIA2_GRANULARITY` and the comparison is not claimed to be
granularity-matched. Frozen aria2-F is unchanged.

### 22.39 L2 environment-design results and V7 decision gate

**Status: completed (simulator design sweeps + live aria2 granularity probe;
no training; no final comparison). Gates: `NOT_READY_ENVIRONMENT_TOO_WEAK`
(primary) and `NOT_READY_ARIA2_GRANULARITY` (blocking for a granularity-matched
comparison). Neither category was redefined after results.**

- **Transitions smoke:** all seven L2 phases were reached in a trajectory run
  (transitions [1..6]).
- **Per-phase steady C=1..16 sweep (128 MiB, seeds 300–302):** best C by phase =
  **[16, 2, 4, 16, 2, 6, 16]**; C16-vs-best completion gap = 0.0 %, 35.9 %,
  5.7 %, 0.0 %, 62.2 %, 3.5 %, 0.0 %. So the degraded phases materially dislike
  high C, while the capacity/throttle phases strongly require it — the
  pre-registered tradeoff exists per phase.
- **Full 512 MiB L2 trajectory (20 s phases, 3 seeds):** best static C = **16**
  (132.9 s); C1 = 422.8 s; **ramp-aware dynamic oracle = 133.2 s** →
  achievable adaptive headroom = **−0.3 %**. The idealized phase-wise oracle
  (instant C changes, perfect future knowledge) would be 117.7 s → 11.4 % upper
  bound, but that is **not reachable** under the frozen one-decision-per-request
  and ±1-concurrency action semantics. Phase bytes at best static
  ([137,13,67,37,25,85,148] MiB) and times (20.1/20.4/19.7/20.3/19.6/20.4/
  12.5 s) show all transitions occur while substantial work remains; the reason
  the headroom is ~0 is that the high-C phases carry most of the bytes while the
  low-C degraded phases carry few, and the ramp is too slow to exploit the
  transitions.
- **aria2 long-duration granularity probe (experiment-only
  `cmd/l2-aria2-probe`, 512 MiB, unlimited rate):** frozen-F-like
  `--split=16 --max-connection-per-server=16 --min-split-size=1MiB` → 15 ×
  32 MiB pieces (+ probe), max in-flight 16. Candidate
  `--split=512 --max-connection-per-server=16 --min-split-size=1MiB` → **still
  15 × 32 MiB pieces** (aria2 1.37.0 bounds the piece count by
  `--max-connection-per-server`). **~1 MiB ranges with ≤16 simultaneous
  connections at 512 MiB is not achievable**, so the granularity-matched
  long-duration aria2 baseline required by §22.38 does not exist with the
  installed aria2; the new baseline was not adopted. Frozen aria2-F is
  unchanged and no comparison is claimed.
- **Consequence:** a fair v7 test of adaptive concurrency would first need an
  environment with much larger *byte-weighted* wrong-C penalties (or faster
  control authority, which is a v7 formulation decision), and separately a
  granularity-matched aggressive baseline that aria2 1.37.0 cannot currently
  provide at this scale. v7 training is therefore **not** justified yet.
- **Artifacts:** `testdata/diagnostics/l2_environment_design/` —
  `l2_spec.json`, `l2_smoke.json`, `l2_static_phase_sweep.json`,
  `l2_full_trajectory_sweep.json`, `l2_adaptive_headroom.json`, `report.txt`,
  `aria2_probe/probe_split{16,512}_maxconn16.json`, `artifact_hashes.json`.

### 22.40 Candidate v7 pre-registration: action authority + L3 environment (BEFORE measurement)

**Status: pre-registered; no training; no v7 model; no new final comparison.
Measurements follow in §22.41.**

**Motivation (from §22.37–§22.39):** L2 had materially different per-phase
optima but ~0 achievable headroom because (a) the suboptimal-C phases carried
too few bytes and (b) the ±1, one-decision-per-request action space could not
change C fast enough. v7 therefore changes **control authority** and designs a
**byte-weighted** long trajectory.

**Candidate action spaces (analysed, not performance-picked):**
- **A — steps:** `none, +1, -1, +4, -4` (5 actions; max Δ4/decision; keeps the
  existing increase/decrease `ActionKind`s with `Action.Value` as the step;
  atomically applied and clamped to [1,16]).
- **B — small/large:** same semantics as A (small = 1, large = 4); rejected as
  redundant (same reachability and authority, no added value).
- **C — targets:** `none, low, medium, high, max` (instant jumps); rejected
  because it introduces target-setting actions that the project reserves
  (`ActionSetConcurrency`), is discontinuous, and increases oscillation risk
  without a demonstrated need.
**Selected: A** — the smallest space that reaches any C within
`ceil(14/4) = 4` decisions and stays within the existing generic action model.

**v7 configuration (fixed):** C 1–16, fixed 1 MiB chunks, one decision per
completed request, reward `r = -pending wall-clock seconds`, no condition labels
or handcrafted bonuses. Live semantics: the runner applies the step atomically
and clamped; existing v1–v6 policies emit `Value <= 0` and keep the historical
single-step behavior.

**L3 environment (fixed, separate from A2–E2, L1, L2), 512 MiB:**

| # | regime | rate | B | latency | loss | congestion | duration |
|---|---|---|---|---|---|---|---|
| 1 | high-capacity server | 512 KiB/s | 0 | 20 ms | 0.3 % | 0.002 | 20 s |
| 2 | severe shared cap | 8 MiB/s | 4 MiB/s | 120 ms | 3.0 % | 0.05 | 35 s |
| 3 | recovery | 8 MiB/s | 6 MiB/s | 40 ms | 0.6 % | 0.008 | 20 s |
| 4 | severe server throttle | 128 KiB/s | 0 | 30 ms | 0.5 % | 0.004 | 25 s |
| 5 | heavy congestion | 8 MiB/s | 4 MiB/s | 100 ms | 2.5 % | 0.06 | 35 s |
| 6 | recovery | 8 MiB/s | 6 MiB/s | 30 ms | 0.4 % | 0.004 | 20 s |
| 7 | final capacity | 1 MiB/s | 0 | 20 ms | 0.3 % | 0.002 | to completion |

**Gates (fixed before measurement):**
- **A** distinct phase-optimal C, including at least one low (≤6) and one high
  (≥11) optimum.
- **B** `adaptive_headroom = (best_static − action_constrained_oracle) /
  best_static ≥ 10 %`, where the oracle obeys the selected A semantics
  (≤ ±4 per decision, clamped) and has perfect phase knowledge.
- **C** at least 5 transitions reached with substantial remaining work.
- **D** simulator/live action semantics identical (atomic, clamped, one
  decision per completed request); contract test added.
- **E** representative live shared-capacity fixed-C data preserves the
  concurrency tradeoff (reused frozen live data, clearly labelled; no new v7
  campaign).
- **F** the selected action space reaches useful C values within phase
  durations (worst case 16→2 in 4 decisions; ≤10 % of the shortest phase at the
  observed decision rate).
Any failed gate ⇒ STOP, reported without redefinition.

### 22.41 v7 design results and gate outcome

**Status: completed (simulator design + frozen live evidence reuse; no training;
no new comparison). OUTCOME: STOP — the selected action space plus L3 still
yields only 8.1 % achievable headroom, below the pre-registered 10 % threshold.
v7 training is NOT justified yet.**

- **Action-space comparison:** A steps `{none,+1,-1,+4,-4}` (5 actions, max
  Δ4/decision, atomic + clamped, keeps generic increase/decrease kinds) was
  selected; B small/large is semantically identical and was rejected as
  redundant; C targets was rejected because it introduces the project's
  reserved target-setting actions, jumps discontinuously, and raises
  oscillation risk. `ACTION_SPACES` in `v7_prereg_spec.json`.
- **Control-authority change (additive, tested):**
  `policyRunner.applyAction` now applies `Action.Value` as the number of unit
  steps for increase/decrease (clamped by the controller; `Value <= 0` → 1, so
  every v1–v6 policy is byte-for-byte unchanged). Contract test:
  `internal/downloader/policy_runner_v7_test.go` (`+4` from 1 → 5, Value 0 → +1,
  clamps at 16/1) — GATE D PASS.
- **L3 static per-phase sweep (128 MiB, seeds 300–302):** best C =
  **[16, 2, 4, 16, 2, 6, 16]**; C16-vs-best gaps 0.0 %, 40.3 %, 8.0 %, 0.0 %,
  54.0 %, 3.3 %, 0.0 % → GATE A PASS. Throughput curves per phase are in
  `l3_static_phase_sweep.json`.
- **Full L3 512 MiB sweep:** best static **C16 = 152.9 s**; C1 = 289.1 s.
  **Action-constrained oracle** (perfect phase knowledge, ≤ ±4 per decision,
  atomic, clamped) = **140.5 s** → **adaptive headroom 8.1 %**, below the
  pre-registered 10 % threshold → **GATE B FAIL ⇒ STOP**. Phase bytes at best
  static: [137, 76, 96, 44, 67, 92, 0] MiB, i.e. the byte-weighting was
  improved over L2 (degraded phases now carry 76 + 67 MiB), but the residual
  penalty of running C16 in them is still not enough to create ≥10 % with
  realistic action authority.
- **Transitions with work (GATE C):** five condition transitions were reached
  with substantial remaining data (remaining bytes after each transition are in
  `v7_gates.json`; a first version of this metric had a units bug in the
  analysis script, corrected before reporting; the gate rule was not changed).
- **Representative live tradeoff (GATE E, reused frozen evidence, no new v7
  campaign):** frozen live v6 A2 fixed-C data (shared-cap trajectory,
  `dqn_validation_v6_live`) shows best fixed C4 = 6588 ms vs C16 = 9756 ms, i.e.
  the live shared-capacity penalty for over-concurrency is preserved. This is
  reused evidence, not a v7-regime sweep; it is labelled as such.
- **Control authority (GATE F):** 16→2 needs 4 decisions; 20–35 s phases at
  roughly one decision per completed request leave ample room → PASS.
- **Aria2 granularity (unchanged from §22.39):** aria2 1.37.0 cannot produce
  ~1 MiB ranges with ≤16 connections at 512 MiB; no aria2 comparison is claimed.
- **Artifacts:** `testdata/diagnostics/v7_design/` —
  `v7_prereg_spec.json`, `action_space_comparison.json`,
  `l3_static_phase_sweep.json`, `l3_full_trajectory_sweep.json`,
  `l3_adaptive_headroom.json`, `v7_gates.json`, `report.txt`,
  `artifact_hashes.json`.
- **Conclusion:** the formulation change (faster control authority) roughly
  doubled the achievable headroom versus L2 (≈0 → 8.1 %), confirming that
  control authority was part of the problem, but the environment still does not
  provide the pre-registered ≥10 % opportunity. A future design iteration would
  need phases where running C16 is proportionally much worse while still
  carrying more work (or a different control formulation); per the task rules
  this was **not** tuned after measurement. **No v7 training, no v7-vs-aria2
  comparison.**

### 22.42 Final environment iteration: L4 pre-registration (BEFORE measurement)

**Status: pre-registered; no training; no v7/v8 model; no new aria2 comparison.
Measurements follow in §22.43.**

**Why L4 after L3:** L3 improved control authority (none/±1/±4) but reached only
8.1% achievable headroom because the degraded low-C phases carried too little
byte/time weight. L4 keeps the exact L3 control formulation (C 1–16, 1 MiB
chunks, atomic clamped ±4, one decision per completed request) and changes only
the **physical phase design** so that wrong concurrency is expensive for a
substantial share of the transfer.

**Byte-weighting design rule (fixed now, checked after the sweep; no post-hoc
tuning):** ≥50 % of bytes in the action-constrained oracle run must occur in
regimes where C16 is materially worse; ≥20–30 % must occur in regimes where
C12–C16 is materially better. If the rule does not emerge, the parameters are
redesigned before the formal sweep; bytes are never assigned by fiat.

**L4 trajectory (512 MiB; high/low/high/low/high), fixed before measurement:**

| # | regime | rate | B | latency | loss | congestion | duration |
|---|---|---|---|---|---|---|---|
| 1 | high-capacity server | 512 KiB/s | 0 | 20 ms | 0.3 % | 0.002 | 20 s |
| 2 | severe shared bottleneck (byte-heavy) | 8 MiB/s | 4 MiB/s | 120 ms | 3.0 % | 0.05 | 55 s |
| 3 | recovery | 8 MiB/s | 6 MiB/s | 40 ms | 0.6 % | 0.008 | 20 s |
| 4 | severe server throttle | 128 KiB/s | 0 | 30 ms | 0.5 % | 0.004 | 20 s |
| 5 | severe congestion/loss (byte-heavy) | 8 MiB/s | 4 MiB/s | 100 ms | 2.5 % | 0.06 | 55 s |
| 6 | recovery | 8 MiB/s | 6 MiB/s | 30 ms | 0.4 % | 0.004 | 20 s |
| 7 | final high-capacity | 1 MiB/s | 0 | 20 ms | 0.3 % | 0.002 | to completion |

**Gates (fixed now):** A distinct phase optima (low ≤6 and high ≥11);
B action-constrained oracle ≥10 % better than best fixed C; C ≥5 transitions
with substantial remaining work; D simulator/live semantics identical
(contract test); E small representative live fixed-C sweep preserves the
qualitative tradeoff; F ±4 authority sufficient. Outcomes fixed:
`READY_FOR_NEXT_RL_ITERATION`, `NOT_READY_HEADROOM`, `NOT_READY_LIVE_MISMATCH`,
`NOT_READY_PHASE_WEIGHTING`, `NOT_READY_CONTROL_AUTHORITY`,
`NOT_READY_ENVIRONMENT_PHYSICS`.

### 22.43 L4 results and final environment outcome

**Status: completed (simulator sweeps + small live representative sweep; no
training; no new aria2 comparison). FINAL OUTCOME: `NOT_READY_LIVE_MISMATCH`.**

- **Simulator (frozen v6 simulator, seeds 300–302, 512 MiB):** best static
  **C16 = 174.0 s**; C1 = 389.4 s (full curve in `l4_full_sweep.json`);
  action-constrained oracle (perfect phase knowledge, ±4/decision, atomic,
  clamped) = **143.4 s** → **adaptive headroom 17.6 %**, i.e. **GATE B PASS**
  (≥10 %). Phase optima are materially distinct (low ≤6 and high ≥11) → GATE A
  PASS; ≥5 transitions with remaining work → GATE C PASS; parity contract
  unchanged → GATE D PASS; authority sufficient → GATE F PASS.
- **Byte-weighting check:** in the oracle run the C16-worse phases carried
  **47.1 %** of bytes, just under the pre-registered ≥50 % rule (the C12–C16
  phases carried the remainder, above the ≥20 % rule). The simulator-side
  outcome would therefore be `NOT_READY_PHASE_WEIGHTING` on a strict reading;
  the rule was not lowered after the fact.
- **Representative live fixed-C sweep (`cmd/l4-live-probe`, new experiment-only
  command; 1 MiB chunks, fresh state, tc cleaned):**
  - degraded regime (shared 4 MiB/s + 120 ms + 3 % loss): C1 0.92, C4 2.58,
    C8 3.39, **C16 3.57 MiB/s** — high concurrency is *not* penalized live;
  - high-capacity regime (512 KiB/s per response, no cap): C16 7.85 vs C1
    0.48 MiB/s — high C strongly beneficial (matches);
  - recovery regime (6 MiB/s + 40 ms + 0.6 % loss): C16 5.52 vs C1 3.09 MiB/s —
    high C beneficial (matches).
  → **GATE E FAIL:** the live controlled stack does not reproduce the
  simulator's degraded-phase penalty for excessive concurrency. The 17.6 %
  simulated headroom therefore rests on a **simulator-only mechanism**
  (per-connection overhead + concurrency-dependent loss/congestion) that does
  not survive basic live validation.
- **Final outcome (pre-registered taxonomy, unchanged):**
  **`NOT_READY_LIVE_MISMATCH`** — the simulator and the representative live
  C relationships disagree qualitatively in the decisive degraded regime.
- **Conclusion:** the L4 design met the 10 % simulated headroom gate, but the
  opportunity is not physically real in the controlled live environment, and
  the byte-weighting rule was narrowly missed. Another RL training iteration is
  **not** justified. Any future stage must first find a live-reproducible
  mechanism that makes excessive concurrency genuinely expensive (e.g., a
  different, still-honest bottleneck model) before using it as an RL target.
  The frozen findings from D2 (granularity × phase-locked pacing), startup
  amortization, L2 ≈0 %, L3 8.1 %, and the aria2 long-download granularity
  limitation are all preserved unchanged.
- **Artifacts:** `testdata/diagnostics/l4_environment_design/` —
  `l4_prereg_phases.json`, `l4_phase_sweep.json`, `l4_full_sweep.json`,
  `l4_oracle.json`, `l4_gates.json`, `live/live_{degraded,high_capacity,recovery}.json`,
  `report.txt`, `artifact_hashes.json`.

### 22.44 T1 pre-registration: live request-level server throttling (BEFORE measurement)

**Status: pre-registered; no training; no new aria2 comparison.** T1 exists
because L4 failed live: it tests the one mechanism already shown to work live —
the server selects a response's pacing rate once at request start and holds it
for the whole request (`server.currentRateLocked` is read once in `ServeHTTP`
and `writeThrottled` uses that rate), so requests started in different phases
carry different fixed rates.

**Mechanism (verified from code and prior D2 telemetry):** a request started
while the server rate is high is paced at the high rate for its entire
transfer, even if the server rate changes meanwhile; requests started during
the throttle get the throttled rate; requests started after recovery get the
recovered rate. The policy never sees phase labels.

**Pre-registered T1 schedule:** 512 MiB file, 1 MiB requests, C 1–16.
Per-response server rates: **normal 8 MiB/s → throttled 128 KiB/s at t=30 s →
recovered 8 MiB/s at t=90 s** (a 60 s throttle). No tc/netem shared cap; the
mechanism is purely server-side per-request pacing.

**Experiment:** live fixed-C sweep C ∈ {1,2,4,8,12,16}; request start/end/rate
telemetry from the server; bytes and request counts by start phase (before /
during / after throttle); best fixed C; then an **action-constrained live
oracle** using exactly the L3/L4 semantics (one decision per completed request,
actions none/±1/±4, atomic, clamped) with perfect knowledge of the schedule and
per-phase targets chosen from the measured per-phase optima; headroom =
(best_static − oracle)/best_static.

**Gates (fixed now; threshold not lowerable):** A live pacing persistence;
B live concurrency reversal (high C before/after, lower/moderate C during);
C throttle carries enough work to affect completion time; D mechanism survives
equal 1 MiB requests (granularity isolation); E action-constrained oracle
≥10 %; F simulator/live qualitative agreement if a simulator is used (T1 is
live-first; simulator calibration is deferred and not used for training).
Any required gate failure ⇒ STOP, no RL training.

**Success signature:** C16 best in normal/recovery, lower/moderate C materially
better during the throttle, with all systems at 1 MiB requests, and ≥10 %
oracle headroom.

### 22.45 T1 results and decision: live per-request throttling does not create an adaptive-concurrency opportunity

**Status: completed (live smoke + live fixed-C sweep + oracle attempt; no
training; no new aria2 comparison). OUTCOME: `NOT_READY` — no RL iteration
justified.**

- **Mechanism verified live (GATE A PASS):** the server selects a response's
  pacing rate at request start and holds it. Smoke evidence: a request started
  at 2964 ms with rate 8 MiB/s ended at 3109 ms after the 3 s throttle began,
  i.e. it retained its original high rate; requests started during and after
  the throttle received their phase rates. The 1 MiB smoke runs show requests
  starting in before/during/after phases as expected.
- **Pre-registered schedule flaw exposed by the required sweep:** at
  512 MiB with normal 8 MiB/s per response, C4–C16 finished the whole file
  **before the throttle at t=30 s** (C16 = 5.4 s, C12 = 7.7 s, C8 = 10.3 s,
  C4 = 20.9 s; all bytes started `before`). Only C1 (139.6 s) and C2 (102.6 s)
  observed the throttle. The "best C during = 2" in the analysis is therefore
  an artifact of missing data for C≥4, not a measured reversal.
- **No live concurrency reversal (GATE B FAIL):** under independent
  per-response pacing, aggregate throughput scales monotonically with C in
  every phase; the only high-C penalty is the drain of in-flight requests
  after recovery, which is bounded by `C × 1 MiB / throttled_rate` and does not
  outweigh C's throughput advantage. A low-C optimum is not observed and is
  not expected from this mechanism when all requests are 1 MiB.
- **Oracle:** targets computed from the incomplete per-phase data were
  (16, 2, 2); the oracle started at C1, ramped up, and finished in 5.66 s
  entirely before the throttle, so its −5.3 % "headroom" versus best fixed C16
  reflects the schedule flaw and ramp cost, not an adaptive opportunity. GATE E
  FAIL. A corrected schedule (normal rate ≈ ≤1 MiB/s so C16 cannot finish
  early) was analysed but not run: it makes C1/C2 runs impractically slow
  (hours) and the monotonic aggregate-scaling argument shows the reversal still
  would not appear; running it could not change the gate outcome.
- **Granularity isolation (GATE D PASS):** every request in all runs was
  exactly 1 MiB; the mechanism therefore does **not** yield a concurrency
  effect at equal granularity. This confirms the D2 finding: the 512 MiB D2 v6
  advantage was a 1 MiB-vs-32 MiB request-granularity effect, not adaptive
  concurrency.
- **Gates:** A PASS; B FAIL; C partial (throttle carried work only for C1/C2);
  D PASS; E FAIL; F deferred (live-first stage; no simulator used).
  **No RL training.**
- **Conclusion:** with the current live server model (independent per-response
  pacing, no shared bottleneck), making high concurrency materially
  disadvantageous during a throttle is not achievable at equal 1 MiB
  granularity. Any future attempt must introduce a different, live-reproducible
  cost of concurrency (L4 showed the simulated one does not survive live) or
  accept that the mechanism is granularity-based and frame the research
  question accordingly. All prior findings (D2 granularity × phase-locked
  pacing, startup amortization, L2 ≈0 %, L3 8.1 %, L4 `NOT_READY_LIVE_MISMATCH`,
  aria2 granularity limitation, frozen v6 results) are preserved.
- **Artifacts:** `testdata/diagnostics/t1_server_throttle/` — `smoke/smoke.json`,
  `live/fixed.json`, `live/oracle.json`, `t1_results.json`, `t1_results.txt`,
  `artifact_hashes.json`.

### 22.46 T2 pre-registration: live adaptive range-size opportunity (BEFORE measurement)

**Status: pre-registered; live-first; no training; no new aria2 comparison.**
T2 follows T1/L4: live adaptive *concurrency* has no opportunity, but the live
request-level pacing mechanism makes large requests carry stale (slow) rates
for their lifetime, so **range size** may be a real control dimension.

**Mechanism (live, re-verified by smoke):** per-response rate selected at
request start and held for the request lifetime.

**Fixed concurrency: C = 16** for every T2 run (documented rationale: hold
parallelism constant and maximise request-scheduling opportunities; do not vary
C).

**Chunk ladder:** 256 KiB, 512 KiB, 1 MiB, 2 MiB, 4 MiB, 8 MiB, 16 MiB.
**Action space A (selected, pre-measurement):** `NONE`, increase one ladder
step, decrease one ladder step; atomic, ladder-index based, clamped to
[256 KiB, 16 MiB], applied before the next request allocation, one decision per
completed request. (B/C rejected: larger authority/instant targets are not
needed to test the mechanism and increase the risk of an artificial result.)

**T2 trajectory (512 MiB, two throttle cycles), fixed now:** per-response rates
**normal 180 KiB/s → throttled 64 KiB/s**; schedule
`t1=30 s throttle, t2=75 s recovery, t3=105 s throttle, t4=150 s recovery`,
then normal to completion (phase durations 30/45/30/45/rest). No tc/netem; the
only variable is chunk size.

**Gates (fixed, threshold 10 % not lowerable):** A chunk-size preference
changes across conditions; B action-constrained oracle ≥10 % better than the
best fixed chunk; C transitions occur with substantial work; D opportunity
survives fixed C=16 and exact granularity control; E gain is chunk-scheduling
(not concurrency/range-semantics confounds); F reproducible across repetitions.
Outcomes fixed: `READY_FOR_ADAPTIVE_RANGE_RL`, `NOT_READY_NO_CHUNK_TRADEOFF`,
`NOT_READY_INSUFFICIENT_HEADROOM`, `NOT_READY_SHORT_HORIZON`,
`NOT_READY_NON_RL_GRANULARITY_EFFECT`, `NOT_READY_UNREPRODUCIBLE`.

### 22.47 T2 results and decision: no live chunk-size preference reversal

**Status: completed (live smoke + live static chunk sweep; no training; no new
aria2 comparison). PRIMARY OUTCOME: `NOT_READY_NO_CHUNK_TRADEOFF` — no RL
adaptive range-size training justified.** (The pre-written classifier in the
analysis script logged `NOT_READY_NON_RL_GRANULARITY_EFFECT` because its
phase-best probe varied; on inspection that variation is a request-start-phase
artifact; the robust finding is the absence of a genuine reversal. Both
categories are NOT_READY and no threshold was changed.)

- **Setup:** 512 MiB, **C = 16 fixed** for every run, 1 MiB..16 MiB ladder
  (256 KiB, 512 KiB, 1 MiB, 2 MiB, 4 MiB, 8 MiB, 16 MiB), server per-response
  rates normal 180 KiB/s → throttled 64 KiB/s with the pre-registered
  `30 s normal / 45 s throttle / 30 s recovery / 45 s throttle / recovery`
  schedule; no tc/netem; smoke verified pacing persistence, transitions and
  exact per-request granularity.
- **Live completion times (single run each; all SHA-correct, max in-flight 16):**
  256 KiB 245.8 s · 512 KiB 245.8 s · 1 MiB 245.7 s · 2 MiB 266.4 s ·
  4 MiB 266.7 s · 8 MiB 267.0 s · **16 MiB 184.2 s (best)**.
- **Mechanism (not a control tradeoff):** 16 MiB requests started in a fast
  phase take ~91 s at 180 KiB/s and therefore *span both throttles while
  retaining the fast rate* — the run never issued requests during a throttled
  phase (16 requests in p1 = 256 MiB, 16 in p3 = 256 MiB). Smaller chunks
  complete quickly and keep starting fresh requests during the throttles at the
  slow rate, losing throughput. The overall preference is monotone toward the
  largest chunk; **no reversal** of the kind required by GATE A/B.
- **Phase-optimal chunk caveat:** the phase-best-by-throughput table varies
  (p1 16 MiB, p2 8 MiB, p3 16 MiB, p4 4 MiB, p5 1 MiB) but is confounded by
  which chunk sizes happen to have requests starting in each phase; it is not
  evidence of an adaptive opportunity.
- **Oracle / headroom:** the best fixed chunk is the **largest** ladder element;
  from it the allowed actions are none/decrease, and no smaller chunk measured
  faster, so an action-constrained oracle cannot beat the best fixed chunk.
  Headroom ≤ 0 (analytical bound; the live oracle run was not performed because
  no improving action exists under the measured curves). GATE B FAIL.
- **Gates:** A no genuine reversal (FAIL); B headroom ≥10 % (FAIL); C
  transitions carried substantial work (PASS); D fixed concurrency + exact
  granularity (PASS); E gain from chunk scheduling rather than fixed-choice
  rate-lock bypass (FAIL — the winning policy is a *fixed* largest chunk);
  F reproducibility single-repetition (not established).
- **Conclusion:** adaptive range size is **not** a live control opportunity in
  this server model: the only large effect is that large requests started in
  fast phases bypass later throttles because their rate is locked at start —
  a fixed-configuration choice, not adaptation, and the opposite of a small-vs-
  large tradeoff. The D2 512 MiB benefit remains a 1 MiB-vs-32 MiB granularity
  effect. No RL training, no aria2 comparison.
- **Artifacts:** `testdata/diagnostics/t2_adaptive_range/` —
  `smoke/smoke.json`, `live/chunk_{256,512,1024,2048,4096,8192,16384}KiB.json`,
  `t2_results.json`, `t2_results.txt`, `artifact_hashes.json`.

### 22.48 M1 real mobile-trace replay: prerequisite audit and STOP

**Status: STOPPED at the data-acquisition prerequisite. Primary outcome:
`NOT_READY_NO_REAL_MOBILE_TRACE`. No trace replay was built, no concurrency
sweep was run, no RL was trained, no aria2 comparison was run.** M1 followed
T1/T2 because the synthetic environments failed to produce a live concurrency
tradeoff; it asked whether real mobile path dynamics do.

- **Repository audit (bounded):** no real mobile/cellular/RTT/loss/jitter
  traces exist in the repository. The only files matching trace/mobile/replay
  are the project's own **synthetic** experiment request traces
  (`testdata/diagnostics/aria2*/request_trace*.json`,
  `request_granularity*`, `wallclock_throttle`, `final_evaluation*/request_traces.json`).
  There is no pcap, no external dataset, and no trace-replay infrastructure.
- **Public-source feasibility check:** the candidate real-mobility source
  `http://users.ugent.be/~jvdrhoof/dataset-4g/` (UGent/imec 4G measurement
  logs; documented mobility contexts such as walking/car/train/tram) responds
  with HTTP 301, and GitHub/raw GitHub are reachable, so real traces are
  obtainable in principle. They were **not** fetched or processed in this
  stage, and no field list, license terms, or replay fidelity were verified.
- **Why STOP rather than proceed:** the pre-registered M1 rules forbid
  synthesizing "mobile" traces, require documented provenance and measured
  fields per trace, and require replay-fidelity validation before any
  concurrency experiment. None of those conditions can be met without a
  dedicated data-acquisition stage, and no data was invented. Per §M1 this is
  exactly `NOT_READY_NO_REAL_MOBILE_TRACE`.
- **Known caveat to carry into the acquisition stage:** the UGent-style logs
  largely record **end-to-end achievable throughput** for a single transfer,
  not transport-independent path capacity; the M1 §5 distinction
  (capacity vs observed throughput) must be resolved, and any dataset must be
  documented for measured variables, units, sampling interval, mobility
  context, and license before use. Replayed data must never be called
  "real-world experiments".
- **Not performed (because of the stop):** trace conversion/canonicalization,
  replay construction and fidelity analysis, the fixed-C sweep (C=1..16,
  1 MiB chunks, 512 MiB), temporal-window `C*(t)` analysis, action-constrained
  oracle, adaptive headroom, and the M1 gates A–E. All of these remain
  pending and must be pre-registered again for the next attempt.
- **Artifacts:** none created for M1 (no data to preserve); this section and
  milestone 65 record the audit and the stop.

### 22.49 M2 real mobile trace acquisition + data audit

**Status: completed (acquisition + audit; no replay built, no RL, no aria2
comparison).** M2 followed M1's data-prerequisite stop and acquired genuine
public mobile/cellular traces, preserving originals under
`testdata/external/mobile_traces/raw/`.

- **Sources and provenance (retrieved 2026-09-25):**
  - **UGent/imec 4G/LTE Bandwidth Logs** —
    `https://users.ugent.be/~jvdrhoof/dataset-4g/`; 16 logs downloaded
    (train, tram, car, bus, bicycle contexts); 6-column format
    (`epoch_ms, elapsed_ms, lat, lon, bytes, interval_ms`), GPS included;
    citation `dataset-4g.bib` (van der Hooft et al., IEEE Comm. Letters 2016).
    **License not explicitly stated on the page — flagged; not used for
    license-explicit work without clarification.**
  - **NYU-METS Metropolitan Mobile Bandwidth Trace** —
    `https://github.com/NYU-METS/Main`; 7 sample traces downloaded
    (7Train1/2, QTrain, Long Island Rail Road, bus57/62/62_2); one
    `Mbits/sec` sample per line at 1 Hz; **license explicitly ODbL 1.0 +
    DbCL 1.0**; citation Mei et al. (PAM 2019; Computer Networks 182, 2020);
    `Multivariate_Dataset` documents RSSI/RSRQ/neighbor/handover/speed/band
    (zip not downloaded yet).
- **Measured variables:** throughput available for all 23 traces; GPS for
  UGent; RTT/jitter/packet-loss unavailable everywhere; outage only observable
  indirectly via zero-throughput samples. Nothing was interpolated or invented.
- **Capacity vs throughput:** both sources record **end-to-end measured
  throughput**, not transport-independent path capacity →
  `capacity_ground_truth = unavailable`; any replay must be framed as a
  throughput-trajectory replay (`REPLAY_PARTIAL`), not a capacity emulation.
- **Temporal quality:** all traces have dense samples (UGent ~1 s intervals,
  2–13 min; NYU exactly 1 s, ~24–82 min) with zero spikes/duplicate handling
  left intact; diagnostics in `trace_quality.json`.
- **Selection / split (fixed before performance, no performance consulted):**
  development = `ugent_4g/train_0001, train_0002, tram_0001`,
  `nyu_mets/7Train1, bus57, bus62`; held-out = `ugent_4g/train_0003,
  tram_0002, car_0001`, `nyu_mets/7Train2, Long_Island_Rail_Road, QTrain,
  bus62_2`.
- **M2 gates:** A ≥5 traces PASS (23); B provenance/license PASS for NYU-METS
  (explicit ODbL/DbCL), UGent flagged (citation only); C temporal throughput
  completeness PASS; D genuine mobility PASS (train/tram/car/subway/rail/bus);
  E no fabrication PASS; F long-enough PASS (NYU up to ~82 min).
- **M3 justification: yes**, for a **throughput-trajectory replay** stage using
  the license-explicit NYU-METS traces (plus UGent only after license
  clarification). The M1 chain (real trace → temporal C* → oracle headroom)
  can now be attempted; replay fidelity, capacity-vs-throughput framing, and
  the no-confound rules from M1 still apply. No synthetic data was created and
  replayed traces must not be called real-world experiments.
- **Artifacts:** `testdata/diagnostics/m2_trace_acquisition/` —
  `trace_inventory.json`, `provenance.json`, `schema_audit.json`,
  `trace_quality.json`, `canonical_manifest.json`, `gates.json`,
  `M2 report.txt`, `artifact_hashes.json`; raw originals + README/bib under
  `testdata/external/mobile_traces/raw/{ugent_4g,nyu_mets}/`.

### 22.50 M3 real mobile trace replay: status and next step

**Status: NOT EXECUTED. No replay system was built, no fixed-C sweep or
temporal C* analysis was run, no action-constrained oracle was evaluated, and
no M3 gate is claimed. No RL training and no aria2 comparison.** This section
records the partial progress honestly so the next agent can continue without
misreading the evidence chain.

- **Delivered (only):** committed deterministic M2 trace parsers and unit
  tests — `python/rl/mobile_trace.py` (NYU-METS `Mbits/sec` @ 1 Hz; UGent
  `bytes/(interval_ms/1000)`; canonical form = elapsed_s +
  throughput_bytes_per_second; no interpolation; SHA-256 helper) and
  `python/tests/test_mobile_trace_m2.py` (units, ordering, missing-line gaps,
  malformed-line handling, deterministic canonicalization, duplicate/zero
  diagnostics, hash stability).
- **Not done (remain the M3 gates):** throughput-envelope replay (Mode A),
  any Mode B path-attribute replay (NYU sample traces are throughput-only;
  `NOT_AVAILABLE` unless the multivariate dataset is acquired), replay-fidelity
  validation, the 512 MiB fixed-C sweep on the license-explicit NYU-METS
  traces, temporal-window `C*(t)`, the realistic ±4 oracle, and gates A–F.
  The pre-registered interpretation rules (throughput ≠ capacity; identical
  replay across C; no fitting to concurrency; no held-out tuning; no RL; no
  aria2) all still stand.
- **Next step:** build the throughput-envelope replay against the
  license-explicit NYU-METS traces, validate fidelity (source-vs-replay MAE,
  correlation, preserved drops/recoveries/zeros), then run the M3 fixed-C and
  temporal-`C*(t)` protocol on development traces, freeze parameters, and only
  then evaluate held-out traces. UGent remains excluded pending license
  clarification.

### 22.51 M3 execution: throughput-envelope replay reached a gate decision

**Status: EXECUTED to a gate decision. OUTCOME:
`NOT_READY_ENVELOPE_NO_C_SENSITIVITY`. No fixed-C temporal sweep or oracle was
run because the pre-registered rule says to STOP when the C-sensitivity gate
fails. No RL; no aria2 comparison.** New experiment-only command
`cmd/m3-replay` (goes with the M2 parsers).

- **Trace used:** real NYU-METS `7Train1.csv` (ODbL 1.0 + DbCL 1.0; 1 Hz
  measured **end-to-end throughput**, explicitly **not** path capacity).
- **Replay implementation:** tc/netem aggregate rate envelope on `lo`,
  re-applied once per trace sample at sample boundaries (1 Hz); zero samples
  replay with an 8000 bps floor (documented outage-like crawl, not a true
  outage model); the last sample is held after the trace ends. The controlled
  server runs unlimited (no synthetic throttling); 1 MiB chunks; workload
  48 MiB; the replay timeline is **identical for every C**.
- **C-sensitivity smoke (C1/C4/C16):** completion times **95 476 ms /
  96 613 ms / 107 208 ms**; C16 is slightly slower and higher C gives no gain.
  The tc envelope is a shared aggregate ceiling, so every C is capped at the
  same instantaneous rate and concurrency cannot change effective throughput.
  Replay throughput was 0.47–0.53 MB/s versus the consumed-window trace mean
  0.67–0.90 MB/s (coarse 1 Hz comparison; the transfer saturates the envelope).
  **GATE B FAIL → `NOT_READY_ENVELOPE_NO_C_SENSITIVITY`.**
- **Harness anomaly (documented):** the C8 run failed with a tc/harness error
  (exit 2); it does not affect the decision because C1/C4/C16 already show no
  C sensitivity.
- **Gates:** B FAIL; A/C/D/E/F not evaluated (stop-on-B rule). No gate was
  redefined.
- **Interpretation and next step:** a single aggregate throughput-envelope
  replay cannot represent a concurrency-selection problem — it imposes the same
  ceiling on all C. M3 therefore did **not** establish a temporal
  concurrency-performance opportunity from real mobile traces. Any next attempt
  must use richer measured path information or a replay method with a genuinely
  concurrency-dependent cost (e.g., per-flow/path attributes, queueing/latency
  effects) rather than scaling the trace envelope. RL training remains
  unjustified.
- **Artifacts:** `testdata/diagnostics/m3_mobile_replay/` — `provenance_used.json`,
  `replay_config.json`, `replay_fidelity.json`, `c_sensitivity_smoke.json`,
  `gates.json`, `report.txt`, `runs/7Train1_C{1,4,16}.json` (and the failed
  C8 attempt), `artifact_hashes.json`.

### 22.52 Revised Stage 0 (Mahimahi delivery-opportunity replay): pre-formalization audit

**Status: BLOCKED — DECISION REQUIRED (§30). No Stage 0 measurement code was
written and no formal data was collected. No RL/adaptive-control work was
performed; frozen artifacts untouched.**

- **Mahimahi is unavailable and not installable here:** `mm-link`, `mm-loss`,
  `mm-delay`, `mm-webrtc` are all missing; `modprobe` does not exist in the
  container and the host kernel (7.2.6 CachyOS) exposes no module tooling.
  The specified `mm-link --once` architecture (and therefore §3 EOF
  validation, §11 trace-time anchor calibration, §13 epoch pilot, §18–20 tau
  calibration) cannot be executed on this machine. A host/container with a
  built-and-loaded `mahimahi` kernel module, or an explicitly approved
  alternative delivery-opportunity replay mechanism, is required first.
- **TCP connection lifecycle (actual code; superseded by §22.55):** at audit
  time `internal/fetcher.New` built a single `http.Client` with the default
  `Transport`; as of the §1 pre-formalization item the nil-client path builds a
  dedicated HTTP/1.1-only transport (keep-alives enabled), and `Fetch` still
  calls `client.Do` per Range, so connection reuse is *expected*
  (Architecture A) — but this has **not** been empirically verified against an
  mm-link path (C↔flow mapping, keep-alive reuse, whether setup traverses the
  link). Per §8, the project must not assume persistence.
- **Byte-level instrumentation (actual code; superseded by §22.57):** at audit
  time the fetcher read each response with
  `io.ReadAll(io.LimitReader(resp.Body, expected+1))` and returned only a
  completed-request `Result`; there was **no timestamped incremental byte
  accounting**. As of the §4 pre-formalization item, the clocked counting
  reader is implemented as an optional `fetcher.ByteObserver` (off by default)
  — see §22.57.
- **Inexhaustible workload (actual code; superseded by §22.58):** at audit time
  `server.New(content []byte)` copied the whole object into RAM and
  `writeThrottled` served it from memory, which could not back a
  continuous-backlog full-trace replay. As of the §5 pre-formalization item,
  the experiment server also supports a deterministic logical object
  (`NewLogical`) that is generated on demand with bounded memory — see §22.58.
  `New([]byte)` is unchanged.
- **Unresolved §30 prerequisites:** (1) Mahimahi absent (host decision);
  (2) fresh-vs-persistent connection lifecycle unverified; (3) incremental
  timestamped byte accounting absent; (4) workload can run dry / no logical
  endless server; (5) mm-link EOF behavior unestablished and watchdog not
  implemented; (6) trace-time anchor tolerance not defined/validated;
  (7) protocol constants (epoch durations, tau regime classifier/thresholds,
  warm-up model, pilot) not frozen/executed.
- **Decisions required (OpenCode must not choose autonomously):** provide a
  Mahimahi-capable host **or** approve a documented alternative
  delivery-opportunity replay; after empirical connection verification,
  choose Architecture (a) persistent-connection modification or (b) documented
  fresh-per-request justification (§8); approve the byte-accounting hook and
  the logical endless-range server backing.
- **Not started:** epoch construction/pilot, byte-observation floor
  enforcement, shared epoch validity, instantaneous oracle, best fixed C,
  ramp/action-constrained oracle DP, transition-cost measurement, trace-level
  t CI, and the formal PASS gate.

### 22.53 Mahimahi runtime validation: BLOCKED on /dev/net/tun

**Status: `MAHIMAHI_RUNTIME_BLOCKED`. Pre-formalization infrastructure
validation only; no formal Stage 0 run; no downloader/RL changes; frozen
artifacts untouched.**

- **Installation:** `mm-link`, `mm-loss`, `mm-delay`,
  `mm-intermittent`, `mm-meter`, `mm-onoff`, `mm-webreplay` are setuid-root
  binaries under `/usr/local/bin/`; `mm-link --help` succeeds (no `--version`
  in this build) and documents `--once`, `--uplink-log/--downlink-log`, queue
  options and the `UPLINK-TRACE DOWNLINK-TRACE [OPTION] [COMMAND]` form.
- **Trace corpus found:** `/usr/local/share/mahimahi/traces/` contains the
  native **delivery-opportunity** traces `ATT-LTE-driving[-2016]`,
  `TMobile-LTE-driving/short`, `TMobile-UMTS-driving`,
  `Verizon-LTE-driving/short`, `Verizon-EVDO-driving` (`.up`/`.down`) plus a
  `README` — a suitable native corpus for the revised Stage 0 (to be audited
  for provenance/licence and strata before use).
- **Runtime blocker (exact):** both a basic run and the `--once` probe die
  immediately with `Died on unix_error: open /dev/net/tun: No such file or
  directory` (rc=1). `/dev/net/tun` is absent in the container, so mm-link
  never creates its namespace or tunnel and no child command executes.
- **Capability context:** uid 1000 with `CapPrm`/`CapEff = 0`; `unshare --net`
  fails `Operation not permitted` (unprivileged user namespaces are blocked).
  The setuid bit on mm-link gives it root, but the missing TUN device is the
  observed first failure; namespace creation was therefore never reached and
  cannot be confirmed either way.
- **Not established:** mm-link `--once` EOF behaviour, trace timing/anchor
  signals, and whether the emulated shell can inspect its namespace — all
  require a running mm-link.
- **Decision required (not made autonomously):** provide `/dev/net/tun` to the
  container (e.g. device passthrough or `mknod` by an operator) and confirm the
  setuid binary can create net namespaces here, **or** run Stage 0 in a
  privileged/host-network-capable environment. Do not change host/container
  network configuration silently.
- **Repository state:** no repository files were modified for this validation;
  synthetic test traces were written only under `$HOME/m3_stage0_tmp/`
  (outside the repo). No byte-accounting, workload, oracle, tau or pilot code
  was started.

### 22.54 Mahimahi runtime re-verification after container change: READY (infrastructure only)

**Status: `MAHIMAHI_RUNTIME_READY` for the replay runtime. This is NOT Stage 0
readiness and no Stage 0 measurement was performed.** §22.53's `/dev/net/tun`
blocker is resolved.

- **TUN:** `/dev/net/tun` present inside the container, `crw-rw-rw- root:root`,
  major/minor **10,200**, mode 666.
- **Capabilities:** the user shell has `CapEff/CapPrm = 0`, but `CapBnd`
  includes **CAP_NET_ADMIN, CAP_NET_RAW, CAP_SYS_ADMIN** (plus NET_BIND_SERVICE,
  MKNOD, etc.), and `mm-link` is **setuid root**; its successful namespace
  creation demonstrates the effective capability path. `unshare --net` directly
  as uid 1000 still fails `Operation not permitted` (unprivileged user
  namespaces are disabled) — Stage 0 must rely on the mm-link setuid path, not
  on `unshare`.
- **Basic mm-link run:** `mm-link TRACE TRACE -- sh -c '...'` succeeds (rc=0);
  the child runs inside the emulated namespace and sees
  `ingress 10.0.0.2 peer 10.0.0.1/32` plus `lo`.
- **Trace format (verified from the shipped corpus README):** one line per
  delivery opportunity; the value is a **timestamp in ms from trace start**,
  repeated once per 1500-byte packet when multiple packets fit in a
  millisecond. A single-line fixed-rate file repeats by default; `--once`
  disables wrapping. (My first synthetic files conflated packet counts with
  timestamps; that was a harness error, corrected.)
- **`--once` EOF behavior (CORRECTED in §22.59; this probe note was wrong):**
  the trace EOF stops delivery and — contrary to this note — with `--once`
  `mm-link` **exits by itself at trace EOF** (rc=0, verified: a 2 s trace with
  a 30 s child returned at 2.11 s). At EOF it signals its child and waits for
  it to die; a child that defers the signal makes `mm-link` wait. Child output
  written after link teardown is not captured, so Stage 0 treats trace EOF as
  the measurement endpoint and uses the independent watchdog; see §22.59.
- **Log/timing:** `--uplink-log/--downlink-log` produce ASCII text (not pcap):
  header lines starting `#` (tool, trace path, command line, queue) followed by
  packet records of the form `<trace_ms> # <packet_bytes>` (e.g. `80 # 1504`);
  a 0.6 s burst trace with a small transfer produced ~2.4k records per
  direction. The ms timestamps are trace-relative, sufficient to design the
  canonical trace-time anchor together with the child's monotonic timestamps.
- **Cleanup / host safety:** `ip link`, `ip addr`, `tc qdisc`, and `ip netns`
  were identical before and after the runs; final state shows clean `lo`/`eth0`
  `noqueue` with no `mm-*` processes. The only stray process was a local test
  `python3 -m http.server` started for the logging probe, which was stopped
  afterwards. No host-level networking changes were made.
- **Stage 0 operational prerequisites now resolved:** mm-link execution,
  namespace/child execution, `--once` EOF characterization (with the
  post-EOF-output caveat), log/timing signals, and cleanup. At the time of this
  re-verification the following were still pending: connection-lifecycle
  verification, incremental timestamped byte accounting, the logical
  endless-range server, the frozen trace-time anchor tolerance, and the
  epoch/tau/pilot protocol constants. Since then, connection lifecycle (§1–§3,
  §22.55–§22.56), byte accounting (§4, §22.57), and the logical workload (§5,
  §22.58) were implemented; the trace-time anchor and the epoch/tau/pilot
  protocol constants remain pending.

### 22.55 Pre-formalization item §1: dedicated HTTP/1.1-only fetcher transport

**Status: implemented and unit-tested (infrastructure only). This is NOT Stage
0 readiness; §2+ items (connection-reuse measurement, C↔flow mapping,
incremental byte accounting, logical workload, harness/watchdog, anchor) remain
unimplemented. No formal data was collected; frozen artifacts untouched.**

- **Change:** `fetcher.New` with `cfg.Client == nil` now builds a dedicated
  `*http.Transport` via `newHTTP1Transport()`; it no longer falls back to
  `http.DefaultTransport`. `http.DefaultTransport` is never mutated, and an
  explicitly supplied `cfg.Client` is still used as-is (verified by test).
  `Fetch` request semantics, timeouts, Range validation, retries, and error
  behavior are unchanged; the whole existing fetcher suite now exercises the
  new transport.
- **HTTP/1.1 enforcement mechanism:** Go 1.27's `http.Transport.Protocols` with
  `SetHTTP1(true)`, `SetHTTP2(false)`, `SetUnencryptedHTTP2(false)`. A non-nil
  `Protocols` set is authoritative (not `ForceAttemptHTTP2`), so HTTP/2 is also
  removed from TLS ALPN and HTTPS cannot negotiate h2. No HTTP/2, h2c, QUIC,
  multiplexing, multipart or multi-source behavior is introduced.
- **Pool settings (Cmax = 16):** `MaxIdleConns = 16`,
  `MaxIdleConnsPerHost = 16` (`maxIdleConnsPerHost` const), keep-alives
  enabled (`DisableKeepAlives` false), `IdleConnTimeout = 90s`; dial (30s /
  30s keepalive), TLS handshake (10s) and Expect-continue (1s) timeouts mirror
  the net/http defaults. This removes the stdlib `MaxIdleConnsPerHost` default
  of 2, which caused connection churn for C > 2.
- **Tests** (`internal/fetcher/transport_test.go`): real plaintext request
  returns `Proto 1.1` with `resp.Close == false`; real request against an
  h2-enabled `httptest` TLS server (asserted to offer `h2`) is still observed
  server-side as HTTP/1.1 and never negotiates ALPN `h2` (the test also sets a
  custom `TLSClientConfig`, proving `Protocols` still wins); transport config
  assertions (HTTP/1-only set, keep-alive, pool ≥ 16); dedicated transport is
  not `http.DefaultTransport` and does not mutate it; caller-supplied client
  identity/transport/Timeout left untouched.
- **Results:** dedicated transport tests pass; `go test ./...`, `-race`,
  `-count=5` on the package, `gofmt -l .`, `go vet ./...`, `go build ./...` all
  clean.
- **Not claimed:** connection reuse and the C→flow mapping were *not* verified
  in §22.55; they were subsequently measured locally in §22.56. Do not describe
  C as "number of connections" without the §22.56 qualification (`C` is the
  worker count and the upper bound on simultaneous HTTP/1.1 flows).

### 22.56 Pre-formalization items §2–§3: connection reuse and C→flow mapping (local, verified)

**Status: `CONNECTION_MODEL_READY` for the local deterministic model. Measured
against local `httptest` range servers, NOT over mm-link; this is not Stage 0
data, no formal run was performed, and §4 was subsequently implemented in
§22.57 (logical workload in §22.58; harness/EOF/watchdog in §22.59; trace-time
anchor remains unimplemented). No frozen artifacts touched. No production code
was changed for this measurement.**

- **Instrumentation (test-only):** client-side `httptrace.ClientTrace`
  (`ConnectStart`, `GotConn` with `Reused`/`WasIdle` and the actual
  `net.Conn`'s `LocalAddr`) is attached to the request context, which
  `Fetcher.Fetch` already forwards to `http.NewRequestWithContext`; server-side,
  `http.Server.ConnContext` assigns a stable integer identity per accepted TCP
  connection and the handler records conn id, protocol, remote address and
  Range per request. `http.DefaultTransport` and production code are untouched.
- **Fetcher reuse (8 sequential 256-byte ranges, one Fetcher):** exactly
  **1 TCP dial** (`ConnectStart`), first `GotConn` `Reused=false / WasIdle=false`,
  the other 7 `Reused=true / WasIdle=true`, all with the **same client local
  address**; server saw **1 connection** for all 8 requests, every request
  `HTTP/1.1`, and each server-observed remote address equalled the client
  connection address (independent cross-check). No churn.
- **C→flow measurement:** deterministic barrier server holds each request until
  all C workers have one in flight (no sleeps); per C, `ChunkSize=1024` and
  `rangesPerWorker=8` gave `requests=8C`. Representative run:
  | C | max in-flight requests | max active TCP conns | total TCP conns | requests | conns used >1× | max requests/conn |
  |---|------------------------|----------------------|-----------------|----------|-----------------|-------------------|
  | 1 | 1 | 1 | 1 | 8 | 1 | 8 |
  | 2 | 2 | 2 | 2 | 16 | 2 | 8 |
  | 4 | 4 | 4 | 4 | 32 | 4 | 9 |
  | 8 | 8 | 8 | 8 | 64 | 8 | 11 |
  | 16 | 16 | 16 | 16 | 128 | 15 | 16 |
- **Verified statement:** under the current downloader architecture and the §1
  transport, configured `C` yields **up to C simultaneous persistent HTTP/1.1
  flows**: max simultaneous in-flight Range requests and max simultaneously
  active TCP connections both equal C for every measured C∈{1,2,4,8,16};
  max in-flight requests on one connection is always **1** (HTTP/1.1, no
  multiplexing); each completed Range returns its connection to the shared idle
  pool and subsequent ranges reuse pooled connections, so total TCP connections
  created remained **exactly C** (no idle-pool churn, including C=16 where the
  §1 `MaxIdleConnsPerHost=16` matters).
- **Worker↔connection relation (actual observed semantics):** workers are not
  bound to connections. All workers share the downloader's single
  `*http.Client`; the transport assigns any idle pooled connection to the next
  request, so a worker may be served by different connections over time and a
  connection may serve several workers. Per-connection request counts are
  therefore uneven (C=16 above: one connection served a single request while
  another served 16). `C` is a worker/concurrency bound, not a
  connection-ownership property.
- **Limitations:** measurements are local (loopback `httptest`), synthetic, and
  retry-free; they do not cover the mm-link path, loss/RTT effects, or
  connection death/re-dial under failure. Worker-level connection migration is
  not observable on the wire (HTTP requests carry no worker id); the statement
  above is based on server-side connection identity plus the single shared
  client/pool structure.
- **Tests:** `internal/fetcher/reuse_test.go` (one-connection reuse with
  client+server cross-check), `internal/downloader/flow_test.go`
  (`TestConcurrencyToFlowMapping`, C∈{1,2,4,8,16}, deterministic barrier).
  Results: focused tests pass with `-count=5`; `go test -race` on both packages
  clean; `go test ./...`, `gofmt -l .`, `go vet ./...`, `go build ./...` clean.

### 22.57 Pre-formalization item §4: incremental timestamped byte accounting

**Status: `BYTE_ACCOUNTING_READY`. Implemented and tested (infrastructure
only). Bytes are attributed at the instant of the body read, not at request
completion. This is NOT Stage 0 readiness: §5 was subsequently implemented in
§22.58 and the harness/EOF/watchdog in §22.59; the trace-time anchor,
epoch/tau/pilot, and formal Stage 0 remain unimplemented. No frozen artifacts
touched; fetch semantics are unchanged when the observer is nil.**

- **Hook location:** `internal/fetcher/bytes.go` — `countingReader` wraps
  `resp.Body` immediately before the existing `io.LimitReader`/`io.ReadAll`
  call in `Fetcher.Fetch`. The wrapper delegates `Read` unchanged and, when a
  read returns n > 0, emits one `ByteEvent` **after the underlying read and
  before the n/err pair is returned**. Fetch's Range handling, validation,
  retries, timeouts, and error behavior are untouched; the wrapper and the
  trace are only installed when `Config.ByteObserver != nil`.
- **Event record:** `ByteEvent{Time, RequestID, ConnID, Delta,
  RequestCumulative, AggregateCumulative}`. `Time` is `time.Now()` taken in the
  read path (monotonic reading); `Delta` > 0; `RequestCumulative` is the bytes
  read so far for that request; `AggregateCumulative` is stamped in append order
  by the meter.
- **Aggregate accounting:** `fetcher.ByteMeter` (concurrency-safe
  `ByteObserver`, events retained in memory) implements the stream;
  `Snapshot()` returns append-order events and `Total()` the cumulative count.
  Later Stage 0 code can reconstruct `bytes_received(t_start, t_end)` by
  summing deltas whose timestamps fall in the interval. No epoch logic is
  implemented here. There is **no global counter**: the meter is owned by the
  caller (per download/experiment) and `downloader.Config.ByteObserver` passes
  it through to the fetcher.
- **Per-connection identity:** when the observer is configured, Fetch attaches
  an `httptrace.ClientTrace` to the request context and captures the actual
  connection's `LocalAddr` from `GotConn`; `ConnID` is that stable
  per-TCP-connection value (the §2/§3 instrumentation). `WithClientTrace`
  composes with traces already in the context. With no observer, no trace is
  attached.
- **Read/error/EOF semantics:** `(n>0, err)` counts n before the error is
  returned; `(n>0, io.EOF)` counts; `(0, err)` creates no event. A response
  that fails after partial delivery keeps its bytes (verified: 50 of a declared
  100 counted, `Result.Bytes == 50`, the error is preserved). For successful
  requests the sum of deltas equals `Result.Bytes` and the requested range
  length.
- **Timestamps:** `ByteMeter.ObserveBytes` clamps an event timestamp earlier
  than the previous event's to the previous timestamp, so the append-ordered
  stream is monotonically nondecreasing even when concurrent reads complete
  slightly out of scheduler order; per-request read timestamps are otherwise
  preserved. Request completion time is never used for byte attribution.
- **Concurrency:** shared state is under the meter's mutex; the connection
  holder uses `atomic.Pointer`; observations happen synchronously in each
  request's read goroutine, so workers only briefly share the append critical
  section. Verified race-clean at gated C=4 and with simultaneous client
  requests.
- **Tests:** `internal/fetcher/bytes_test.go` — two-interval attribution
  (bytes in A > 0 and B > 0, sum = body, request completes only after B),
  nonzero bytes with zero completed requests, wrapper `(n>0, err)` and
  `(n>0, io.EOF)` cases, failed response retaining partial bytes, totals vs
  `Result.Bytes`, monotonic timestamps, and concurrent-request
  identification/connection separation; `internal/downloader/byteaccounting_test.go`
  — C=4 gated download: aggregate total, per-request totals cross-checked
  against the metrics attempt records, per-request cumulative monotonicity,
  exactly 4 distinct connection identities, output byte-match. Results: focused
  tests pass with `-count=5`; `go test ./...`, `go test -race
  ./internal/fetcher/... ./internal/downloader/...`, `gofmt -l .`,
  `go vet ./...`, `go build ./...` all clean.
- **Remaining for §5+:** the logical workload (§22.58) and the mm-link
  harness/EOF/watchdog (§22.59) were subsequently implemented; the trace-time
  anchor and calibration, epoch construction, real trace corpus use, tau,
  oracle/DP, and RL were not started.

### 22.58 Pre-formalization item §5: logical bounded-memory HTTP Range workload

**§5 logical workload: READY.** Implemented and tested (experiment
infrastructure only). This is NOT Stage 0 readiness: the harness/EOF/watchdog
were subsequently implemented in §22.59; trace-time anchor and calibration,
epoch/tau/pilot, and formal Stage 0 remain unimplemented. No frozen artifacts
touched; existing finite-server behavior is unchanged.

- **Representation:** `internal/experiment/server/logical.go` adds a
  `contentSource` abstraction (`size()` + `write(ctx, w, start, end, rate)`).
  `New([]byte)` is unchanged: it still copies into a private `[]byte`
  (`finiteContent`) and serves through the original `writeThrottled` call.
  `NewLogical(size int64)` returns a `Server` backed by `logicalContent`, a
  stateless generator; no `make([]byte, size)` is ever created. The
  experiment-only logical source is opt-in.
- **Configuration / size:** the logical size is explicit per server
  (`NewLogical(size)`; `size > 0` required, else `ErrInvalidLogicalSize`). The
  package exposes `DefaultLogicalSize = 1<<44` (16 TiB) as the recommended
  Stage 0 full-trace envelope. Rationale (a safety envelope, not a performance
  result): the longest native traces are ~82 min (~4920 s), so even an absurd
  sustained 2 GiB/s replay for two hours (~14.1 TiB) does not exhaust it. The
  later harness may pass any explicit size and must request contiguous 1 MiB
  ranges until trace EOF.
- **Deterministic content:** each absolute byte offset maps to a byte via the
  splitmix64 finalizer over its 8-byte word index (`mixLogicalWord` /
  `fillLogical`), so the same offset always yields the same byte regardless of
  reading order, grouping, alignment, or prefix size, and different offsets
  vary. The generator is stateless, O(1) memory, and direct random access:
  generating `[start,end]` costs O(end-start+1) time and never touches the
  prefix. It is a content generator, not a cryptographic primitive.
- **Range/HTTP semantics:** shared with the finite server through one
  `ServeHTTP` path: single contiguous ranges only (no multipart/disjoint);
  `start >= size`, `end < start`, malformed, suffix, or multi-range requests →
  **416** with `Content-Range: bytes */SIZE`; an `end` beyond the object is
  clamped to `size-1` (existing finite semantics); a valid range returns
  **206** with exact `Content-Range` and `Content-Length`; a no-Range request
  returns **200** with the full logical object (streamed). Delays, rate
  schedules, per-range delays, failure injection, request log, and stats behave
  identically to the finite server.
- **Streaming / bounded memory:** logical bodies are generated and written in
  16 KiB chunks (matching `writeThrottled`'s pacing granularity) using a
  per-request buffer, so serving an N-byte range allocates 16 KiB + O(1), not
  N.
- **Tests** (`internal/experiment/server/logical_test.go`): construction
  validation; `DefaultLogicalSize`; small/large-offset/adjacent/overlapping/
  single-byte/end-reaching/clamped ranges with exact Content-Length and
  Content-Range; deterministic repeat, offset variation, offset stability and
  adjacent composition; invalid ranges → 416 with `bytes */SIZE`; no-Range 200
  full object; constructing a 16 TiB object allocating < 1 MiB; serving a
  64 MiB range at offset ~4 TiB allocating < 1 MiB; generator
  offset-stability across grouping/alignment; finite-server private-copy and
  semantics regression. Exact results: focused tests pass; the package passes
  with `-count=5`; `go test ./...`, `go test -race ./...`, `gofmt -l .`,
  `go vet ./...`, `go build ./...` all clean.
- **Not claimed / remaining:** the logical object is finite and sufficiently
  large, not mathematically infinite; no trace anchor or epoch pilot exists
  (the Mahimahi harness/watchdog now exists, §22.59); no formal Stage 0 data
  was produced; `RangeTracker`/downloader behavior is untouched.

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

### 22.70 Oracle stage (prepared 2026-09-27): BLOCKED — tau regime rule not pre-registered

**Status: `BLOCKED — ORACLE PREREQUISITE UNRESOLVED` (specific gate:
`BLOCKED — TAU REGIME RULE NOT PRE-REGISTERED`). The frozen v2 input was
verified; no tau, static baseline, instantaneous oracle, constrained oracle,
or headroom was computed, and no measurement or threshold was fabricated.
The frozen pilot artifacts were not modified.**

- **Input verified (read-only, from `/home/vscode/epoch_pilot_v2`):**
  `pilot_manifest.json` confirms protocol version 2, request deadline none,
  dial deadline none, parent/child watchdog 45 m; `pilot_report.json` confirms
  `passed=true`, `selected_duration="2m0s"`. The 120 s shared-valid epoch set
  was recomputed from the per-run `result.json` records:
  **21 candidate / 18 shared-valid / 3 excluded** (EVDO 7, LTE 11), exactly as
  expected. Excluded rows: EVDO 2 epochs × 5 C `below_byte_floor`; LTE 1 final
  partial epoch × 5 C `below_observed_fraction`. Retained epoch fields
  (trace, epoch index, start/end, C, bytes, observed duration, observed
  fraction, R, per-C validity, exclusion) are preserved without interpolation
  in `oracle_stage_v2/shared_valid_epochs.jsonl` (90 rows = 18 epochs × 5 C).
- **Blocking prerequisites absent from the repository (verified by search):**
  1. **tau regime classification rule + regime-dependence threshold** — no
     such classifier or threshold exists anywhere in the repository;
     §22.52 already records "protocol constants (epoch durations, tau regime
     classifier/thresholds, warm-up model, pilot) not frozen/executed".
  2. **transition-cost (tau) measurement design ("switch-injection") and any
     frozen tau measurement** — §22.52 lists "transition-cost measurement"
     under "Not started"; no tau artifact exists under `testdata/` and no
     switch-injection code exists.
  3. **completion-time / workload mapping from `R[C,e]`** — no Stage 0 oracle
     specification exists; §22.52 lists "instantaneous oracle, best fixed C,
     ramp/action-constrained oracle DP" as not started.
  4. The frozen Stage 0 constrained-oracle convention (action ladder, dwell
     rule, tie-breaking) is likewise not present in the repository protocol
     (only in the task prompt), so it could not be treated as pre-registered.
  The task's stop rules require stopping here ("if the exact threshold has not
  yet been frozen in the repository, STOP before formal headroom … Do not
  invent a threshold"; "STOP … if tau cannot be measured according to the
  protocol … completion-time mapping is ambiguous"), and §22.52 states that
  such decisions must not be chosen autonomously.
- **Not done / not claimed:** tau was not measured; `T(C)`, `T_best_fixed`,
  the instantaneous diagnostic, the constrained oracle, and `H_j` were not
  computed; no headroom, confidence interval, RL, adaptive controller, aria2,
  or IDM was run; no fixed-C pilot was rerun; no protocol parameter was
  changed.
- **What must be pre-registered before the oracle stage can proceed:** the tau
  measurement design (objective regimes, switch-injection procedure, both
  transition directions, equivalent-completion-time loss estimator), the
  regime classification rule and regime-dependence threshold (written before
  any tau measurement or headroom inspection), the completion-time/workload
  mapping, and the exact constrained-oracle convention. Then measure tau,
  freeze it, build the oracle, and only then compute `H_j`.
- **Sanity checks reserved for after unblocking (not applied now):** for
  completion-time minimisation, `T_constrained <= T_best_fixed` (a static C is
  feasible) and `T_instantaneous <= T_constrained` (the unconstrained oracle
  is a lower bound), hence `H_j >= 0` unless a documented numerical edge case
  occurs; negative headroom must be reported, not clamped.
- **Statistical limitation (unchanged):** the pilot has only **n=2 traces**,
  which cannot satisfy the final trace-level statistical gate; any future
  two-trace headroom is descriptive only and the final gate remains pending
  additional eligible traces.
- **Artifacts:** `/home/vscode/oracle_stage_v2/` —
  `oracle_manifest.json` (status, missing prerequisites, 24 pilot input
  hashes), `input_verification.json`, `shared_valid_epochs.jsonl`,
  `tau_measurements.json` / `fixed_c_completion.json` /
  `instantaneous_oracle.json` / `constrained_oracle.json` / `headroom.json`
  (explicit `NOT_COMPUTED` placeholders with the blocked reason),
  `oracle_tables.txt` (empty tables with blocked markers),
  `oracle_analysis.txt` (human-readable). Input-hash re-check after the stage:
  all 24 tracked pilot files unchanged.
- **Validation (this task):** `gofmt -l .` clean; `go test ./...` pass;
  `go test -race ./...` pass; `go vet ./...` pass; `go build ./...` pass; no
  `mm-link`/child/namespace/qdisc residue.

### 22.71 Oracle/tau pre-registration and native tau measurement (executed 2026-09-27)

**Status: `TAU_PREREGISTERED_AND_MEASURED`. The §22.70 blockers are resolved:
the oracle objective, the native-trace regime classifier, the injection design,
the 2.0 regime threshold, and the tau aggregation rule are frozen in the
repository, and all eight transition directions were measured with dedicated
switch-injection replays. No headroom/oracle was computed.**

- **Why the oracle was blocked (§22.70):** no pre-registered tau regime
  classifier/threshold, no tau measurement design or frozen tau, and no
  completion-time/workload mapping existed anywhere in the repository; the
  task rules forbade inventing them.
- **Pre-registration artifact:** `testdata/oracle_preregistration_v2/`
  (`oracle_preregistration.json` + `.txt`), frozen before any measurement:
  oracle protocol version 1, fixed-C protocol version 2, selected epoch
  120 s, action ladder `1<->2<->4<->8<->16` (stay or one step per boundary;
  no direct jumps), dwell rule (after a change at boundary e, `C_{e+1} = C_e`),
  static-C feasibility, headroom formula
  `H_j = (T_best_fixed,j - T_constrained,j)/T_best_fixed,j`, statistical unit =
  trace, and the tau/classifier/aggregation definitions below.
- **B_e mapping:** every retained shared-valid epoch uses the same workload
  `B_e = 5 MiB` (the frozen byte floor) for every C; `t[c,e] = B_e/R[c,e]`.
  The global workload scale cancels in the dimensionless headroom ratio.
- **Regime classifier (native downlink trace only; never downloader
  throughput):** full 120 s epochs over the downlink trace (final partial
  epoch excluded); rate = opportunities × 1500 B / 120 s;
  `k = max(1, floor(n/3))`; degraded = k lowest rates (ties: lower index);
  good = k highest rates not already degraded (ties: lower index); recovering
  = `rate[e] >= 1.25*rate[e-1]` and not degraded; precedence degraded →
  recovering → good → middle. Deterministic (ties resolved by index).
  Classification on the pilot traces: EVDO 8 full epochs → degraded {3,4},
  recovering {1,5,6}, no good epochs; LTE 11 full epochs → degraded {2,4,5},
  good {1}, recovering {3,6,9}.
- **Injection timestamps (frozen before tau):** for regimes in the order
  degraded, good, recovering, the earliest full epoch and the next distinct
  one when available; absent regimes contribute nothing. Frozen plan:
  EVDO → degraded e3 (360 s), degraded e4 (480 s), recovering e1 (120 s),
  recovering e5 (600 s); LTE → degraded e2 (240 s), degraded e4 (480 s),
  good e1 (120 s), recovering e3 (360 s), recovering e6 (720 s). Plans
  persisted in `tau_campaign_v2/plan/*.json` (hash-recorded).
- **Switch-injection design:** one continuous replay per direction at each
  preselected timestamp: run at source C until the epoch boundary, force the
  target C (`downloader.SetConcurrency` with adaptive bounds 1–16), and
  measure the following complete 120 s epoch. The run is truncated by the
  parent watchdog immediately after the window (`switch_at + 120 s + 15 s`);
  the child self-watchdog is 2 min later so the parent truncation always wins
  and cancellation is non-fatal. The matched control is the protocol-v2
  fixed-C pilot run at the target C over the identical trace-time window
  (read-only; same traces, same protocol, same windows). Tau formula:
  `tau = (control_bytes - switched_bytes)/(control_bytes/120 s)` in seconds of
  equivalent 5 MiB completion time; negative values preserved; a zero-byte
  window or zero control bytes invalidates the observation (reported, never
  divided).
- **Implementation:** `internal/experiment/tau` (classifier, injection
  selector, ladder/dwell validation, `t[c,e]` mapping, window byte
  accounting, median/aggregation with the frozen 2.0 threshold) and
  `cmd/tau-injection` (`-plan`, `-measure`, `-aggregate`, switch-injection
  parent/child modes). `replay.ChildConfig` gained harness-only adaptive
  bounds and a scheduled switch (`MinConcurrency`, `MaxConcurrency`,
  `SwitchAt`, `SwitchTo`) with summary evidence (`switch_at_ns`, `switch_to`,
  `switch_applied`, `final_concurrency`); fixed-C behavior is unchanged when
  these are zero. No change to the fixed-C downloader semantics, C set,
  chunk, epoch duration, byte floor, observed-fraction threshold, trace set,
  or anchors.
- **Campaign:** 9 timestamps × 8 directions = **72 switch-injection runs**
  under protocol v2 (request/dial deadlines none) in
  `/home/vscode/tau_campaign_v2/runs/`, all 72 valid, 0 failures, 0 retries
  (2026-09-27 ~14:05–16:41 UTC). Harness note: simultaneous mm-link launches
  can collide on the host-side `10.0.0.x` address (observed once: two
  instances picked `10.0.0.9`, producing a zero-byte hang); the runner now
  gates launches ≥ 5 s apart, which eliminated the race (72/72 clean). The
  zero-byte case is also excluded by the pre-registered validity rule.
- **Tau aggregation (frozen rule):** per direction, pooled tau = median of all
  valid observations; per-regime medians over valid observations;
  regime-specific tau iff ≥ 2 regimes have valid observations, all regime
  medians > 0, and `max/min >= 2.0`; otherwise pooled. A non-positive minimum
  regime median makes the ratio undefined (reported explicitly).
- **Measured tau (72/72 valid observations; 9 per direction; seconds of
  equivalent 5 MiB completion time):**

  | Direction | Pooled tau | Regime medians (degraded / good / recovering) | Aggregation |
  |---|---|---|---|
  | 1→2 | 0.123 | 0.017 / 0.123 / 1.064 | **regime-specific** (ratio 63.5) |
  | 2→1 | 0.569 | 0.341 / −0.097 / 5.928 | pooled (ratio undefined) |
  | 2→4 | 0.096 | 0.548 / −0.072 / 0.048 | pooled (ratio undefined) |
  | 4→2 | 0.009 | 0.014 / −0.444 / 0.340 | pooled (ratio undefined) |
  | 4→8 | 0.157 | 1.823 / −0.122 / −0.502 | pooled (ratio undefined) |
  | 8→4 | −0.130 | −1.434 / −0.165 / −0.011 | pooled (ratio undefined) |
  | 8→16 | −0.125 | −1.710 / 0.845 / −0.132 | pooled (ratio undefined) |
  | 16→8 | 0.026 | 0.619 / −0.132 / −0.017 | pooled (ratio undefined) |

  Only 1→2 is regime-dependent under the frozen 2.0 rule; its largest
  per-regime median is 1.06 s. All other directions use pooled taus with
  |tau| ≤ 0.57 s. Individual observations range from −16.5 s to +23.8 s
  (scheduling/in-flight noise in single windows; negative values are
  preserved, never clamped). The small magnitudes are consistent with the
  shared-link observation that per-epoch delivered bytes are nearly
  C-independent: switching C costs little beyond in-flight drain/ramp.
- **No headroom yet:** `T_fixed(C)`, `T_best_fixed`, the instantaneous
  diagnostic, the constrained oracle, and `H_j` were **not** computed; the
  next task performs the oracle/headroom analysis with these frozen tau
  values. No RL/adaptive control/aria2 was run, and no fixed-C pilot was
  rerun.
- **Artifacts:** `testdata/oracle_preregistration_v2/` (protocol);
  `/home/vscode/tau_campaign_v2/` — `plan/` (frozen plans + hashes), `bin/`,
  `runs/<trace>_e<idx>_<regime>_<from>to<to>/` (72 run dirs with
  `run_result.json`, `summary.json`, `events.jsonl`, `up.log`, `down.log`,
  `output.bin`), `campaign_jobs.json`, `campaign_log.jsonl`,
  `tau_observations.jsonl`, `tau_aggregation.json`, `tau_measurements.json`,
  `tau_report.txt`, `tau_manifest.json`. `/home/vscode/oracle_stage_v2/
  tau_measurements.json` was updated from the blocked placeholder to the
  measured summary (headroom still absent).
- **Tests / validation:** new deterministic tests cover B_e identity across C,
  `t = B_e/R`, the allowed ladder, direct-jump rejection, one-step and dwell
  enforcement, classifier determinism and tie handling, injection selection,
  the exact 2.0 threshold, pooled and regime-specific medians, negative-tau
  preservation, the window formula, the zero-denominator case, read-only
  inputs, and the child switch mechanism. `gofmt -l .` clean; `go test ./...`,
  `go test -race ./...`, `go vet ./...`, `go build ./...` all pass. Cleanup
  verified (no mm-link/child/namespace/qdisc residue); the 24 tracked v2
  pilot files and the historical diagnostic directories are unchanged.
- **Limitations (unchanged):** n=2 traces → any headroom from these traces is
  descriptive only and cannot satisfy the final trace-level statistical gate;
  controls come from the protocol-v2 fixed-C pilot (identical windows,
  read-only) rather than a fresh control campaign, as §15 instructed to run
  only the switch-injection measurements; single-observation windows are
  noisy.

### 22.72 Formal oracle/headroom analysis (executed 2026-09-27): BLOCKED — instantaneous-bound inconsistency from frozen negative taus

**Status: `BLOCKED — ORACLE COMPUTATION INCONSISTENT`.** The formal
oracle/headroom computation was executed exactly under the frozen
pre-registration and produced the required artifacts, but the pre-registered
sanity inequality `T_instantaneous <= T_constrained` fails for the LTE trace.
The failure is a **model property of the frozen negative measured taus**, not
an implementation error (the DP was cross-checked against exhaustive
enumeration and reproduced independently). Per the task rules no value was
clamped and no formula changed. Descriptive two-trace numbers are recorded
below; no headroom claim and no statistical-gate claim is made.

- **Inputs verified:** protocol-v2 fixed-C pilot (`selected=2m0s`,
  `passed=true`), 18 shared-valid 120 s epochs (EVDO 7, LTE 11), frozen
  preregistration v1, frozen tau aggregation (72/72 valid observations). All
  24 tracked v2 pilot files unchanged. No new network experiment, no fixed-C
  rerun, no tau rerun.
- **Workload interpretation:** counterfactual completion-time equivalents for
  a common per-epoch workload `B_e = 5 MiB` (same for every C); they are not
  measured end-to-end download times and the constrained oracle is not an
  adaptive execution.
- **Fixed-C completion equivalents (seconds):**

  | Trace | C=1 | C=2 | C=4 | C=8 | C=16 | T_best_fixed | best C (descriptive) |
  |---|---|---|---|---|---|---|---|
  | EVDO | 548.142 | 546.228 | 545.684 | **543.982** | 547.817 | 543.982 | C8 |
  | LTE | 70.423 | 68.811 | 68.769 | 68.880 | **68.520** | 68.520 | C16 |

- **Diagnostic instantaneous oracle:** EVDO 534.806 s; LTE 68.261 s
  (diagnostic only; ignores tau/ladder/dwell).
- **Formal constrained oracle (frozen ladder/dwell; regime label of the
  entered epoch):** EVDO T_constrained **535.240 s**, 3 transitions, sequence
  `[4,4,8,8,4,4,8]`; LTE T_constrained **68.216 s**, 5 transitions, sequence
  `[8,16,16,8,8,16,16,16,8,8,4]`. Both sequences validate against the ladder,
  one-step limit, and dwell rule.
- **Tau values actually used (frozen):** 1→2 regime-specific (degraded
  0.0168 s, good 0.1228 s, recovering 1.0643 s; middle epochs fall back to
  the pooled 0.1228 s); all other directions pooled (2→1 0.5690, 2→4 0.0963,
  4→2 0.0089, 4→8 0.1566, **8→4 −0.1303**, **8→16 −0.1249**, 16→8 0.0256).
  Negatives were preserved, never clamped.
- **Descriptive two-trace pilot headroom:** EVDO **H = 1.61 %**; LTE
  **H = 0.44 %** (trace = statistical unit; no pooling across epochs).
- **Secondary negative-tau sensitivity (not the formal result):** replacing
  every negative tau with 0 gives EVDO H = 1.58 % (delta −0.02 pp) and LTE
  H = 0.047 % (delta −0.40 pp). The zero-tau diagnostic gives EVDO 1.64 %
  and LTE 0.17 %. The LTE formal headroom therefore depends materially on the
  negative measured taus.
- **Sanity diagnosis (why blocked):** `T_constrained <= T_best_fixed` holds
  for both traces, and the selected sequences are ladder/dwell-valid. The
  diagnostic `T_instantaneous <= T_constrained` bound holds for EVDO but
  fails for LTE (68.2607 > 68.2160). Mathematically, the instantaneous oracle
  is a lower bound only when transition costs are non-negative:
  `T_constrained = Σ t[C_e,e] + Σ tau`, and the frozen negatives (e.g.
  8→4, 8→16) let the DP trade a small rate loss for a negative switching
  "gain". The exhaustive brute-force audit reproduces the Go DP objectives
  exactly (EVDO 535.239802396, LTE 68.215955037), so the computation is
  correct; the inconsistency is between the frozen negative-tau rule and the
  task's assumed bound. The nonnegative-tau sensitivity restores the bound.
- **Statistical status:** n = 2 traces. `FINAL_STATISTICAL_GATE = PENDING`
  (requires ≥5 usable diverse traces and `CI_95_lower(H) >= 0.10`). The
  two-trace result is descriptive feasibility evidence only; no generalization
  is claimed.
- **Artifacts:** `/home/vscode/oracle_stage_v2/` — `fixed_c_completion.json`,
  `instantaneous_oracle.json`, `constrained_oracle.json`, `headroom.json`,
  `tau_used.json`, `oracle_sequences.json`,
  `sensitivity_nonnegative_tau.json`, `sensitivity_zero_tau.json`,
  `oracle_tables.txt`, `oracle_analysis.txt`, `oracle_manifest.json`
  (input + preregistration hashes, protocol, epoch, trace IDs), plus the
  retained `input_verification.json` / `shared_valid_epochs.jsonl`. The
  analysis is byte-identical across two runs.
- **Implementation:** `internal/experiment/tau/oracle.go` (fixed,
  instantaneous, exact DP with ladder/dwell, regime-aware tau lookup,
  nonnegative/zero-tau variants) + `cmd/oracle` (analysis CLI); tests include
  an exhaustive brute-force cross-check over random small instances.
- **What must happen next (separately pre-registered; not decided here):** a
  decision on how the formal oracle treats negative measured taus (for
  example a pre-registered floor at zero before the oracle, or redefining the
  diagnostic bound) so that the sanity inequality is restored without
  silently clamping; then the descriptive analysis can be rerun and the
  ≥5-trace statistical gate pursued. Historical sections are unchanged.

### 22.73 Nonnegative formal transition-cost model, protocol version 2 (executed 2026-09-27)

**Status: `TAU_COST_MODEL_V2_READY_FOR_ORACLE`.** The signed-tau
specification was replaced by an explicitly versioned nonnegative formal-cost
rule, frozen before a fresh switch-injection campaign. All formal tau values
are ≥ 0; raw signed observations are preserved; no headroom was computed and
the fixed-C v2 pilot was not rerun. Historical results (§22.71 signed-tau
estimates, §22.72 signed-tau oracle analysis) are unchanged and remain
historical/exploratory.

- **Why the signed model was rejected as a formal cost:** `tau` is defined as
  a transition *cost*, and the version-1 estimator permitted negative
  estimates (v2 campaign: 8→4 = −0.1303 s, 8→16 = −0.1249 s). A negative cost
  acts as a switching reward, which made the diagnostic lower bound fail
  (§22.72: LTE `T_instantaneous` 68.2607 s > `T_constrained` 68.2160 s). The
  DP was verified correct by exhaustive enumeration, so the fix is a
  specification revision, not a computation fix.
- **New formal rule (protocol version 2):** the estimator is unchanged
  (pooled = median of valid observations; regime-specific only if the frozen
  rule fires: ≥2 regimes, all medians > 0, max/min ≥ 2.0). The nonnegative
  transform is applied **after** the estimator:
  `tau_formal = max(0, estimated_tau)`, and
  `tau_formal(from,to,regime) = max(0, regime_median)` where regime-specific.
  Individual raw observations are never clamped; no absolute values; no
  means; raw negatives stay visible as diagnostics. This guarantees
  `tau_formal >= 0`, so the oracle can enforce
  `T_instantaneous <= T_constrained <= T_best_fixed`.
- **New pre-registration:** `testdata/oracle_preregistration_v3/`
  (`oracle_preregistration.json` + `.txt`), `oracle_protocol_version = 2`,
  superseding version 1 (`testdata/oracle_preregistration_v2/`, retained).
  All other definitions are unchanged: B_e = 5 MiB, `t[c,e] = B_e/R[c,e]`,
  120 s epoch, ladder `1↔2↔4↔8↔16`, one step per boundary, dwell rule,
  instantaneous/best-fixed/constrained oracle definitions, H formula,
  trace-level statistical unit, native regime classifier, injection
  timestamps, 2.0 threshold, median definition.
- **Fresh campaign (formal dataset):** `/home/vscode/tau_campaign_v3/` — the
  same switch-injection design, frozen timestamps and 8 directions, 120 s
  window, protocol-v2 networking (no request/dial deadlines, 45 m watchdog),
  matched protocol-v2 fixed-C target control (read-only). Plan JSONs are
  byte-identical to the v2 plans (same classifier/timestamps). **72/72 runs
  valid, 0 failures, 0 retries**, finished 2026-09-27 19:47 UTC.
  `/home/vscode/tau_campaign_v2/` is retained as signed-tau
  historical/exploratory evidence and was not modified.
- **Raw evidence:** 72 valid observations, **32 raw negative observations**
  preserved per direction (1→2: 3, 2→1: 5, 2→4: 4, 4→2: 4, 4→8: 4, 8→4: 5,
  8→16: 4, 16→8: 3). Raw negatives are diagnostics only; they are never
  clamped before the estimator.
- **Formal tau (v3, seconds of equivalent 5 MiB completion time):**

  | Direction | v3 signed pooled estimate | Formal tau (max(0,·)) | v2 signed estimate (historical) | Regime rule in v3 |
  |---|---|---|---|---|
  | 1→2 | 0.0615 | 0.0615 | 0.1228 | pooled (ratio undefined: good median −0.3147) |
  | 2→1 | −0.1555 | **0.0000** | 0.5690 | pooled (ratio undefined) |
  | 2→4 | 0.0021 | 0.0021 | 0.0963 | pooled (ratio undefined) |
  | 4→2 | 0.0184 | 0.0184 | 0.0089 | pooled (ratio undefined) |
  | 4→8 | 0.0905 | 0.0905 | 0.1566 | pooled (ratio undefined) |
  | 8→4 | −0.2468 | **0.0000** | −0.1303 | pooled (ratio undefined) |
  | 8→16 | 0.0031 | 0.0031 | −0.1249 | pooled (ratio undefined) |
  | 16→8 | 0.0327 | 0.0327 | 0.0256 | pooled (ratio undefined) |

  In the fresh campaign **no direction is regime-specific**: every direction
  has at least one non-positive regime median, so the frozen ratio rule is
  undefined and falls back to pooled. Two directions (2→1, 8→4) are affected
  by `max(0,·)`; all formal values are ≥ 0. The differences versus the v2
  estimates include sampling variation between campaigns (fresh raw runs) and
  the transform.
- **No headroom yet:** `T_best_fixed`, `T_instantaneous`, `T_constrained`,
  and `H` were **not** recomputed in this task. The next stage reruns the
  frozen oracle analysis using the v3 formal tau table; with nonnegative
  costs the sanity inequalities are guaranteed.
- **Artifacts:** `testdata/oracle_preregistration_v3/` (protocol version 2);
  `/home/vscode/tau_campaign_v3/` — `plan/` (plans + binary/plan hashes),
  `bin/`, `runs/` (72 run dirs), `campaign_jobs.json`, `campaign_log.jsonl`,
  `campaign_done.json`, `tau_observations.jsonl` (signed raw evidence),
  `tau_aggregation.json` (signed estimates + formal values),
  `tau_measurements.json`, `tau_report.txt`, `tau_manifest.json`,
  `tau_comparison_v2_vs_v3.json`/`.txt` (old vs revised), runner/analyzer/
  comparison scripts.
- **Tests / validation:** new unit tests prove negative estimated tau → formal
  0 and positive estimated tau unchanged, that aggregation preserves the
  signed median while setting the formal value, and that `TauTable.Formalized`
  floors estimates without touching the original table. `gofmt -l .` clean;
  `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...` all
  pass. Cleanup verified (no mm-link/child/namespace/qdisc residue); the v2
  pilot (24 hashed files) and the v2 tau campaign are unchanged.
- **Limitations:** n = 2 traces (descriptive only; final ≥5-trace statistical
  gate still pending); fresh raw observations differ from the historical
  campaign, so old-vs-new differences mix sampling variation with the
  transform; single-window observations remain noisy.

### 22.74 Formal oracle/headroom under protocol version 2 (executed 2026-09-27): descriptive two-trace headroom

**Status: `ORACLE_STAGE_V3_COMPLETE — DESCRIPTIVE_HEADROOM_COMPUTED`.** The
formal oracle/headroom computation ran with the frozen nonnegative formal tau
table (oracle protocol version 2). All sanity inequalities hold for both
traces; the result is descriptive only and the final statistical gate is
pending. No network experiment, no fixed-C rerun, no tau rerun.

- **Inputs verified:** protocol-v2 fixed-C pilot (`selected=2m0s`,
  `passed=true`; 24 hashed files unchanged), 18 shared-valid 120 s epochs
  (EVDO 7, LTE 11), preregistration v3 hash matches the manifest, v3 campaign
  plans match their hashes, 72/72 valid tau observations. Formal tau is the
  v3 `formal_tau_seconds` table (nonnegative); the historical v2 signed table
  is used only for the labelled diagnostic.
- **Fixed-C completion equivalents (seconds; B_e = 5 MiB; counterfactual
  completion-time equivalents, not measured download times):**

  | Trace | C=1 | C=2 | C=4 | C=8 | C=16 | T_best_fixed | best C (descriptive) |
  |---|---|---|---|---|---|---|---|
  | EVDO | 548.137 | 546.228 | 545.677 | **543.982** | 547.825 | 543.982 | C8 |
  | LTE | 70.416 | 68.814 | 68.772 | 68.876 | **68.520** | 68.520 | C16 |

  (Full-precision values from `oracle_stage_v3/fixed_c_completion.json`; the
  §22.72 section's table was rounded at the third decimal and is left as
  historical record — T_best_fixed is identical.)

- **Diagnostic instantaneous oracle:** EVDO 534.806 s; LTE 68.261 s
  (ignores tau/ladder/dwell; not the formal gate).
- **Formal tau (v3, seconds):** 1→2 0.0615, 2→1 0.0000, 2→4 0.0021,
  4→2 0.0184, 4→8 0.0905, 8→4 0.0000, 8→16 0.0031, 16→8 0.0327 (all pooled;
  no direction is regime-specific in the fresh data).
- **Formal constrained oracle:** EVDO T_constrained **535.238 s**, 3
  transitions, sequence `[4,4,8,8,4,4,8]`; LTE T_constrained **68.498 s**, 3
  transitions, sequence `[8,16,16,16,16,16,8,8,4,4,4]`. Both sequences obey
  ladder adjacency, the one-step rule, and dwell.
- **Sanity inequalities (both traces pass):**
  `T_instantaneous <= T_constrained <= T_best_fixed` — EVDO
  534.806 ≤ 535.238 ≤ 543.982; LTE 68.261 ≤ 68.498 ≤ 68.520. With the
  nonnegative formal tau the §22.72 inconsistency no longer occurs.
- **Descriptive two-trace headroom (trace = statistical unit):** EVDO
  **H = 1.6075 %**; LTE **H = 0.0320 %**. Both are below the preregistered
  10 % target; descriptively only.
- **Historical signed-tau diagnostic (NOT FORMAL):** reproducing §22.72 with
  the v2 signed table gives EVDO H = 1.6072 %, LTE H = 0.4436 % (exact
  historical match). Using the fresh v3 signed estimates gives EVDO 1.6529 %,
  LTE 0.9046 %. The formal nonnegative model gives a much smaller LTE value
  because it cannot exploit negative transition estimates.
- **Zero-tau diagnostic (NOT FORMAL):** EVDO H = 1.6408 %, LTE H = 0.1740 %
  (rate-only headroom with costless switching).
- **Statistical status:** n = 2 traces → **FINAL_STATISTICAL_GATE =
  PENDING** (requires ≥5 usable diverse traces and `CI_95_lower(H) >= 0.10`).
  No generalization, significance, or Stage 0 PASS is claimed.
- **Artifacts:** `/home/vscode/oracle_stage_v3/` — `fixed_c_completion.json`,
  `instantaneous_oracle.json`, `constrained_oracle.json`, `headroom.json`,
  `tau_used.json`, `oracle_sequences.json`, `sensitivity_signed_tau.json`
  (historical v2 + fresh v3 signed diagnostics), `sensitivity_zero_tau.json`,
  `oracle_tables.txt` (Tables 1–7), `oracle_analysis.txt`,
  `oracle_manifest.json` (fixed-C input hashes, prereg hash, protocol
  versions, epoch, trace IDs). The complete analysis is byte-identical across
  two runs. `cmd/oracle` was extended to consume the formal v3 table and emit
  the signed/zero diagnostics; the §22.72/§22.73 artifacts are untouched.
- **Validation:** `gofmt -l .` clean; `go test ./...`, `go test -race ./...`,
  `go vet ./...`, `go build ./...` all pass; no mm-link/child/namespace/qdisc
  residue; the v2 pilot, v3 campaign inputs, and historical directories are
  unchanged.
- **Interpretation:** the analysis asks whether the measured fixed-C rate
  variation leaves theoretically available adaptive headroom after the
  measured nonnegative transition-cost model. Under this model the
  descriptive answer is ~1.6 % (EVDO) and ~0.03 % (LTE), both far below the
  10 % target; the constrained sequence is a counterfactual oracle, not a
  learned or executed policy.

### 22.75 Final Stage-0 trace-expansion decision gate (preparation/audit only; superseded by §22.76)

**Superseded by §22.76:** every item recorded below as UNFROZEN is now frozen
in the final trace protocol (`testdata/oracle_preregistration_v4/
final_trace_protocol.json`). The audit text describes the pre-freeze state and
is retained as historical record.

**Status at audit time: `BLOCKED — FINAL TRACE PROTOCOL INCOMPLETE`.** A
read-only audit of the repository determined what is actually frozen for the
final ≥5-trace statistical gate. No network experiment, no fixed-C replay, no
tau replay, and no new headroom were performed; the two-trace descriptive
results (EVDO 1.6075 %, LTE 0.0320 %) are unchanged. Planning artifacts are
under `/home/vscode/final_trace_expansion_decision/`.

- **Frozen evidence (valid):** protocol-v2 fixed-C pilot
  (`/home/vscode/epoch_pilot_v2`, 18 shared-valid 120 s epochs on the two
  current traces); oracle protocol version 2
  (`testdata/oracle_preregistration_v3/`: B_e = 5 MiB, `t = B_e/R`, 120 s
  epoch, ladder/dwell, native regime classifier, 2.0 threshold, tau estimator
  + `max(0, estimate)` formal rule, `H_j` formula, statistical unit = trace);
  formal tau campaign `/home/vscode/tau_campaign_v3/` measured **on the two
  current traces only**.
- **Audit results (question by question):**
  - **Trace count — NOT PREREGISTERED at audit time (now frozen at 5 in §22.76).** The string "≥5 usable diverse traces
    with `CI_95_lower(H) >= 0.10`" appears only in analysis output
    (`cmd/oracle` strings, `oracle_stage_v3/headroom.json`) and in
    §22.72/§22.74/§27 of this document. `oracle_preregistration_v3` itself
    says only "final trace-level gate pending more traces" — no frozen
    minimum count and no definition of "usable".
  - **Diversity — UNFROZEN at audit time (now frozen in §22.76).** No frozen strata, no carrier/radio/mobility
    requirement, no duplicate-family rule, and no rule stating whether the two
    current traces count toward diversity.
  - **Additional trace identities — UNFROZEN at audit time (now frozen in §22.76).** No authoritative list exists.
    The installed native corpus contains five full-length (≥450 s) families:
    the two current (`Verizon-EVDO-driving`, `Verizon-LTE-driving`) plus
    `ATT-LTE-driving` (786.1 s), `TMobile-LTE-driving` (474.6 s),
    `TMobile-UMTS-driving` (928.4 s). The short variants
    (`ATT-LTE-driving-2016` 120 s, `TMobile-LTE-short` 140 s,
    `Verizon-LTE-short` 140 s) fall below the epoch pilot's ≥450 s
    trace-selection rule; that rule is a **pilot** rule, not a frozen
    final-population rule. Candidate set is recorded as
    **PROPOSED — NOT YET FROZEN**; nothing was selected.
  - **Tau scope — UNFROZEN at audit time (now frozen as PER-TRACE in §22.76).** The estimator and the nonnegative formal rule
    are frozen, but the protocol is silent on whether tau for an expanded
    population is per-trace, pooled across traces, measured on a calibration
    subset, or another design. The existing v3 campaign covers only the two
    current traces, so its external validity for new traces is unresolved.
  - **Regime classifier — FROZEN and applicable.** The classifier (native
    downlink only; full 120 s epochs; k = max(1, floor(n/3)); degraded = k
    lowest; good = k highest not degraded; recovering = `rate[e] ≥
    1.25·rate[e−1]` and not degraded; precedence degraded → recovering → good
    → middle; injection = earliest + next distinct per regime) applies
    deterministically to the proposed candidates: `ATT-LTE-driving` 4
    timestamps, `TMobile-LTE-driving` 2, `TMobile-UMTS-driving` 5.
  - **Final statistical gate — NOT PREREGISTERED.** Only `H_j` and unit =
    trace are frozen. Missing: population-mean estimator, sample standard
    deviation, t-based 95 % CI definition (one- vs two-sided, df),
    lower-confidence-bound definition, missing/invalid-trace handling, and
    multiplicity rules.
  - **Reuse rules — UNFROZEN at audit time (now frozen in §22.76).** `epoch_pilot_v2` is valid for the two current
    traces; `tau_campaign_v3` is valid for the two current traces. Whether
    either may be reused for the expanded population (and under which tau
    scope) is not frozen. Historical datasets
    (`epoch_pilot`, `epoch_pilot_corrected`, `evdo_timeout_diag`,
    `tau_campaign_v2`) remain excluded from formal calculations.
  - **Admissibility / duration / contamination — UNFROZEN at audit time (now frozen in §22.76).** No
    final-population admissibility criteria, duration requirement, or
    contamination/exclusion rules are recorded.
- **Minimum new work (conditional only, if the recorded aspiration held and
  the two current traces were retained):** 3 additional trace families ×
  5 C = **15 protocol-v2 full-trace fixed-C replays**; tau work depends on the
  unfrozen scope — **0** new runs if tau were pooled from v3 (external
  validity unresolved) or **88** switch-injection runs if per-trace
  (32 + 16 + 40 for the proposed candidates). No wall-clock estimate is made.
- **Note on a different requirement:** the M1/M2 line's "≥5 documented real
  mobility traces" (§22.48–§22.50) belongs to the separate, stopped
  mobile-trace acquisition/replay line and must not be conflated with this
  Mahimahi Stage 0 final gate.
- **Interpretation (unchanged):** the two-trace headroom (EVDO ≈ 1.61 %,
  LTE ≈ 0.03 %) is descriptive only. Additional diverse traces would test
  whether this small headroom is trace-specific or persists; no outcome is
  predicted, and the project framing is **not** changed on this basis.
- **Artifacts:** `/home/vscode/final_trace_expansion_decision/` —
  `protocol_audit.json`/`.txt`, `trace_candidates.json` (corpus scan +
  PROPOSED candidates), `reuse_rules.json`, `tau_scope.json`,
  `final_statistical_gate.json`, `experiment_matrix.json`. Planning/audit
  only; no measured data.
- **Required before any expansion run:** pre-register (in the repository) the
  final trace count, diversity strata, admissibility/duration/contamination
  rules, the exact trace identities, the tau scope, the reuse rules, and the
  exact statistical formula and pass criterion. Until then the gate remained
  `PENDING` and expansion was blocked. **(Now resolved: §22.76 freezes all of
  these; expansion is ready.)**

### 22.76 Final trace protocol (frozen, prospective; documentation only)

**Status: `FINAL_TRACE_PROTOCOL_FROZEN — READY FOR FINAL TRACE EXECUTION`.**
This section converts the §22.75 audit into an explicit prospective
pre-registration. It is documentation only: no network experiment, no fixed-C
replay, no tau campaign, no headroom recomputation, and no source change were
performed. The frozen artifact is
`testdata/oracle_preregistration_v4/final_trace_protocol.json` (+ `.txt`);
`oracle_preregistration_v3` (oracle protocol version 2) remains unchanged and
is referenced, not modified. Existing descriptive results
(EVDO 1.6075 %, LTE 0.0320 %) are unchanged.

- **Final trace population (frozen, n = 5):**
  1. `Verizon-EVDO-driving`
  2. `Verizon-LTE-driving`
  3. `ATT-LTE-driving`
  4. `TMobile-LTE-driving`
  5. `TMobile-UMTS-driving`

  Excluded: `ATT-LTE-driving-2016` (120 s), `TMobile-LTE-short` (140 s),
  `Verizon-LTE-short` (140 s), all historical datasets, and any other corpus
  member. The population spans Verizon EVDO/LTE, ATT LTE, and TMobile
  LTE/UMTS; each family is counted once, and the two existing traces are
  retained.
- **Trace eligibility (frozen):** full-length duration ≥ 450 s; valid paired
  `.up`/`.down` family; genuine mm-link EOF/endpoint validation must pass;
  no fatal child error; no trace contamination per the existing checks
  (`epoch.ExclusionTraceContamination`, `anchor_alignment`); one count per
  family; technical execution failures may be rerun under the same protocol;
  a scientifically ineligible trace is not silently replaced; no post-hoc
  substitution based on observed H.
- **Replay / oracle rules (frozen, existing):** protocol-v2 watchdog-only
  transport; HTTP/1.1; C = {1,2,4,8,16}; 1 MiB fixed chunk; full-trace
  uninterrupted fixed-C replay with continuous backlog from t=0 to EOF;
  timestamped byte-delivery measurement through the frozen anchor
  (StartupOffset 60 ms, AlignmentTolerance 100 ms); 120 s epochs; 5 MiB byte
  floor per C per epoch; minimum observed-duration fraction 0.80; shared epoch
  validity across all C; final partial epoch excluded; exact existing regime
  classifier; exact existing injection timestamp selection; one ladder step
  per boundary; dwell of at least one complete following epoch after a change.
- **Tau scope (frozen): PER-TRACE.** Rationale: tau is a transition-cost
  property of the replayed trace/environment, so the existing two-trace tau
  measurements are reusable only for those two traces; each newly added trace
  requires its own tau measurement rather than assuming pooled cross-trace
  transferability. The estimator is the existing frozen one (retain signed
  raw observations; median rule; regime-specific iff ≥2 regimes with valid
  observations, all medians > 0, max/min ≥ 2.0; `formal tau = max(0, estimated
  tau)`) and must not be altered after seeing new results.
- **Reuse rules (frozen):** `epoch_pilot_v2` fixed-C data is reused for
  `Verizon-EVDO-driving` and `Verizon-LTE-driving`; `tau_campaign_v3` is
  reused only for those same two traces. `ATT-LTE-driving`,
  `TMobile-LTE-driving`, and `TMobile-UMTS-driving` require fresh protocol-v2
  fixed-C replays and fresh per-trace tau campaigns under the frozen
  estimator/classifier/injection rules. `tau_campaign_v2` is excluded, as are
  `epoch_pilot`, `epoch_pilot_corrected`, and `evdo_timeout_diag`.
- **Headroom (frozen):** `H_j = (T_best_fixed,j − T_constrained,j) /
  T_best_fixed,j`.
- **Statistical design (frozen):** one trace = one statistical observation;
  population estimator = arithmetic mean of the five trace-level `H_j`;
  variability = ordinary sample standard deviation across the five `H_j`;
  confidence bound = one-sided 95 % Student-t lower confidence bound with
  `df = n − 1` (n = 5, df = 4); exact lower bound
  `LCB_95 = mean(H) − t_(0.95, df=n−1) · s(H)/√n`; pass criterion
  `LCB_95 >= 0.10`, interpreted as a predeclared 10 % minimum
  population-level exploitable-headroom requirement.
- **Invalid / missing handling (frozen):** a technical run failure may be
  rerun under the same protocol; a trace failing a scientific eligibility
  criterion is not substituted or selectively deleted; the final statistical
  gate is not evaluated unless all five predefined families yield usable
  trace-level `H` values; if fewer than five usable traces remain, the study
  is reported as failing to reach the preregistered sample rather than
  changing n or replacing traces post hoc.
- **Experiment matrix (frozen workload counts):** 3 new traces × 5 C = **15
  protocol-v2 fixed-C full-trace replays**; per-trace tau for the three new
  traces: `ATT-LTE-driving` 4 timestamps × 8 directions = **32**,
  `TMobile-LTE-driving` 2 × 8 = **16**, `TMobile-UMTS-driving` 5 × 8 = **40**
  → **88 tau runs**; **total 103 new runs**. Timestamp counts reuse the
  already verified classifier outputs (§22.75); no timestamps were invented.
- **Current descriptive interpretation (unchanged):** the two-trace headroom
  is descriptive only; the five-trace study tests whether it is trace-specific
  or persists. No outcome is predicted and no framing change is made here.
- **Planning artifacts updated to match this freeze:**
  `/home/vscode/final_trace_expansion_decision/` —
  `protocol_audit.json`/`.txt` (`READY FOR FINAL TRACE EXECUTION`, with the
  pre-freeze audit retained), `trace_candidates.json` (frozen five-trace
  population vs excluded short variants), `reuse_rules.json`,
  `tau_scope.json` (PER_TRACE), `final_statistical_gate.json`,
  `experiment_matrix.json` (15 + 88 = 103).
- **No code or experiment changes:** this freeze is documentation only; no
  source file was modified, no measurement was rerun, and no existing frozen
  artifact was altered.

### 22.77 Stage A execution: final-trace fixed-C replays (executed 2026-09-27/28)

**Status: `STAGE_A_COMPLETE — 15/15 FIXED-C REPLAYS VALID`.** The fixed-C
portion of the frozen final trace expansion ran exactly as pre-registered.
No tau campaign, switch injection, oracle/headroom, or statistical analysis was
performed. Output dir: `/home/vscode/final_fixed_c_stage_a/` (fresh; all other
evidence untouched).

- **Protocol used (frozen v2):** watchdog-only transport (request deadline
  none, dial deadline none), parent/child watchdog 45 m, HTTP/1.1, chunk
  1 MiB, 64 GiB logical workload, C = {1,2,4,8,16}, one fresh full-trace
  replay per C, timestamped byte-delivery measurement, existing endpoint/EOF
  validation and child-error classification, existing cleanup. Commands:
  `go run ./cmd/epoch-pilot -trace-good <family>.down -trace-degraded
  <other>.down -only-trace <family> -output-dir /home/vscode/final_fixed_c_stage_a`
  for `ATT-LTE-driving`, `TMobile-LTE-driving`, `TMobile-UMTS-driving`.
  Window: 2026-09-27 21:17:05 → 2026-09-28 00:38:47 UTC.
- **Matrix (15 planned, 15 executed, 0 technical failures, 0 reruns):**

  | Trace | C | Outcome | Residual ms | Endpoint valid | mm-link exit | Fatal | Requests (succ/fail) |
  |---|---|---|---|---|---|---|---|
  | ATT-LTE-driving | 1 | `SUCCESS_TRACE_REACHED_EOF` | −35 | true | 0 | false | 457 (455/2) |
  | ATT-LTE-driving | 2 | `SUCCESS_TRACE_REACHED_EOF` | −56 | true | 0 | false | 467 (463/4) |
  | ATT-LTE-driving | 4 | `SUCCESS_TRACE_REACHED_EOF` | −56 | true | 0 | false | 472 (464/8) |
  | ATT-LTE-driving | 8 | `SUCCESS_TRACE_REACHED_EOF` | −57 | true | 0 | false | 476 (463/13) |
  | ATT-LTE-driving | 16 | `SUCCESS_TRACE_REACHED_EOF` | −57 | true | 0 | false | 485 (459/26) |
  | TMobile-LTE-driving | 1 | `SUCCESS_TRACE_REACHED_EOF` | −35 | true | 0 | false | 675 (674/1) |
  | TMobile-LTE-driving | 2 | `SUCCESS_TRACE_REACHED_EOF` | −34 | true | 0 | false | 693 (691/2) |
  | TMobile-LTE-driving | 4 | `SUCCESS_TRACE_REACHED_EOF` | −35 | true | 0 | false | 695 (691/4) |
  | TMobile-LTE-driving | 8 | `SUCCESS_TRACE_REACHED_EOF` | −36 | true | 0 | false | 696 (688/8) |
  | TMobile-LTE-driving | 16 | `SUCCESS_TRACE_REACHED_EOF` | −56 | true | 0 | false | 701 (685/16) |
  | TMobile-UMTS-driving | 1 | `SUCCESS_TRACE_REACHED_EOF` | −35 | true | 0 | false | 214 (213/1) |
  | TMobile-UMTS-driving | 2 | `SUCCESS_TRACE_REACHED_EOF` | −35 | true | 0 | false | 217 (215/2) |
  | TMobile-UMTS-driving | 4 | `SUCCESS_TRACE_REACHED_EOF` | −34 | true | 0 | false | 218 (214/4) |
  | TMobile-UMTS-driving | 8 | `SUCCESS_TRACE_REACHED_EOF` | −55 | true | 0 | false | 220 (212/8) |
  | TMobile-UMTS-driving | 16 | `SUCCESS_TRACE_REACHED_EOF` | −34 | true | 0 | false | 224 (208/16) |

- **Protocol verification:** every run's downlink-log header contains
  `'-timeout' 'none' '-dial-timeout' 'none'`; the driver banner shows
  `watchdog: 45m0s (watchdog-only)`, chunk 1 MiB, workload 64 GiB. No
  `result.failed.*` records; no reruns needed.
- **Eligibility:** all three new families are full-length (replay ends
  1012.5 s ATT, 475.0 s TMobile-LTE, 931.2 s TMobile-UMTS — all ≥ 450 s),
  have valid paired `.up`/`.down` files, passed genuine EOF/endpoint
  validation (residuals −34…−57 ms, all within ±100 ms), show no fatal child
  error and no workload exhaustion; each family counted once. Epoch-level
  contamination (`anchor_alignment`, `trace_contamination`) is evaluated in the
  next (per-trace oracle) stage.
- **Artifacts:** `/home/vscode/final_fixed_c_stage_a/` — per-run
  `<trace>_C<c>/{result.json, anchor.json, summary.json, events.jsonl,
  up.log, down.log, output.bin}`, per-trace driver logs,
  `stage_a_driver.log`, `run_stage_a.sh`, and the Stage-A report
  `stage_a_report.json`/`.txt`.
- **Integrity:** `epoch_pilot_v2` (24 hashed files), `tau_campaign_v3`
  (plans/manifest), `oracle_stage_v3`, and `oracle_preregistration_v4`
  (sha256 `bc418557…`) are unchanged; no source file changed during Stage A;
  cleanup verified (no mm-link/child/runner/namespace; `tc qdisc` unchanged).
- **Not done (explicitly):** no per-trace tau campaigns, no switch injection,
  no oracle/headroom, no statistical analysis, no RL/adaptive control, no
  aria2/IDM, no Verizon-EVDO/LTE reruns, no protocol/threshold change, no
  trace substitution. `H`, `T_fixed`, `T_constrained`, `tau`, CI, and the
  final gate were **not** computed.
- **Next stage:** per-trace tau campaigns for `ATT-LTE-driving` (4 timestamps
  × 8 directions = 32), `TMobile-LTE-driving` (2 × 8 = 16), and
  `TMobile-UMTS-driving` (5 × 8 = 40) under the frozen estimator/classifier/
  injection rules; then per-trace oracle analysis and the pre-registered
  five-trace `LCB_95 >= 0.10` gate.

### 22.78 Stage B boundary-0 amendment (frozen before execution, 2026-09-28)

**Status: `BOUNDARY-0 AMENDMENT FROZEN BEFORE EXECUTION — STAGE B READY`.**
The per-trace tau campaign (Stage B) was prepared but not started: inspection
of the frozen plan against the frozen harness exposed an execution
incompatibility for one preregistered injection, which was resolved
prospectively by an explicit protocol amendment. **No Stage-B run had started
when the amendment was frozen (0 of 88)**; no source file was modified, no
frozen result was altered, and no existing rule was changed except the added
boundary-0 realization.

- **Original boundary-0 specification:** the frozen injection-selection rule
  ("per regime in the order degraded, good, recovering, the earliest full
  epoch and, if available, the next distinct one") selects the
  `ATT-LTE-driving` good epoch 0 with `start_ms = 0`; the frozen matrix counts
  ATT as 4 timestamps × 8 directions = 32 runs, so a switch injection at
  trace-time 0 ms is preregistered.
- **Implementation incompatibility discovered before execution:** the frozen
  switch-injection harness cannot represent the mathematical boundary t = 0.
  The child applies a scheduled switch only when `SwitchAt > 0` and treats
  `SwitchAt = 0` as "no switch" (`internal/experiment/replay/child.go`), so
  executing the boundary-0 injection with `-switch-at 0` would leave the run
  at the source C and record `switch_applied = false`, i.e. the measured
  window would not be governed by the target C. 8 of the 88 preregistered
  runs (ATT epoch 0 × 8 directions) are affected; the blocker is recorded in
  `/home/vscode/final_tau_stage_b/stage_b_blocker.json`/`.txt`.
- **Prospective boundary-0 rule (amendment 1, frozen before execution):** a
  preregistered injection timestamp of exactly 0 ms is executed with the
  earliest fixed positive switch instant of **1 ms** (a fixed protocol
  constant, not chosen from observed results); the run still starts at the
  source C; the switch must actually be applied and recorded as
  `switch_applied = true`; the target C therefore governs the complete
  post-switch measurement window `[0 ms, 120 000 ms)`; the run remains the
  preregistered boundary-0 injection, not a substituted timestamp. No other
  rule changed: the remaining three ATT timestamps, all 8 directions, the
  estimator, classifier, dwell rule, epoch rule, and statistical gate are
  unchanged; the matrix remains ATT 4 × 8 = 32 and 88 tau runs in total.
- **Amendment artifacts:** `testdata/oracle_preregistration_v4/
  final_trace_protocol.json` — new top-level `protocol_amendment`
  (`amendment_version: 1`, `status: FROZEN_BEFORE_EXECUTION`,
  `original_protocol_sha256: bc418557…`, `stage_b_runs_started_before_amendment:
  0`); `.txt` — appended amendment section (original protocol text preserved
  as historical record); `/home/vscode/final_trace_expansion_decision/
  boundary_zero_amendment.json` (new planning record);
  `protocol_audit.json`/`.txt`, `experiment_matrix.json`, and `tau_scope.json`
  updated with the amendment reference. The recorded original JSON hash is
  `bc4185579321420efe43e13234e23ceb08d44abb731805de2ed75e011a8c4c51`.
- **Not done:** no tau run was executed (0/88), no fixed-C replay was rerun, no
  source code was modified, no frozen result was altered, and no oracle/H/CI/
  gate computation was performed. Amended protocol status:
  `FINAL_TRACE_PROTOCOL_FROZEN (v1 + amendment 1) — READY FOR STAGE B`.

### 22.79 Stage B execution: per-trace tau campaign (executed 2026-09-28)

**Status: `STAGE_B_COMPLETE — 88/88 VALID`.** The preregistered per-trace tau
campaign for the three new traces ran exactly under protocol v2 plus
amendment 1. Window: 2026-09-28 02:18:40 → 04:49:17 UTC. Output dir:
`/home/vscode/final_tau_stage_b/` (fresh; all other evidence untouched). No
oracle/headroom, CI, or statistical-gate computation was performed.

- **Matrix (88 planned, 88 valid, 0 technical failures, 0 reruns):**
  `ATT-LTE-driving` 4 timestamps × 8 directions = 32/32 valid;
  `TMobile-LTE-driving` 2 × 8 = 16/16; `TMobile-UMTS-driving` 5 × 8 = 40/40.
  All 8 frozen directions occurred for every selected timestamp; the observed
  (trace, epoch, regime, start_ms, direction) set equals the frozen plan set —
  no substitution.
- **Boundary-0 rule applied (amendment 1):** the 8 `ATT-LTE-driving` good
  epoch-0 runs (preregistered `start_ms = 0`) executed with the fixed 1 ms
  switch instant; every one recorded `switch_at_ns = 1 000 000`,
  `switch_applied = true`, and the target C governed the complete
  `[0, 120 s)` window. They remain the preregistered boundary-0 injections
  (not treated as failures or substitutions).
- **Execution evidence:** every run ended `WATCHDOG_TIMEOUT` with a non-fatal
  child cancellation (`child_status = canceled`, `mm_link_exit = 0`), no fatal
  child error, no workload exhaustion, and watchdog = switch + 120 s + 15 s
  (child self-watchdog +2 min). Request/dial deadlines were the frozen
  defaults (`none`); the runner passed no timeout flags.
- **Raw signed observations:** preserved in
  `tau_observations.jsonl` and per-trace files; 35 of 88 observations are
  negative (range −6.979 s to +38.965 s), never clamped or rerun.
- **Formal per-trace tau (frozen median + `max(0, estimate)`, no cross-trace
  pooling; seconds of equivalent 5 MiB completion time):**

  | Trace | 1→2 | 2→1 | 2→4 | 4→2 | 4→8 | 8→4 | 8→16 | 16→8 |
  |---|---|---|---|---|---|---|---|---|
  | ATT-LTE-driving | 0.722 | 0.087 | 0.154* | 0.136 | 0.233 | 0.000 | 0.000 | 0.114 |
  | TMobile-LTE-driving | 0.155 | 1.219* | 0.071* | 0.000 | 0.365* | 0.078 | 0.000 | 0.001 |
  | TMobile-UMTS-driving | 0.000 | 0.281* | 0.000 | 0.029 | 0.442 | 0.000 | 0.148 | 0.221 |

  Values are the frozen `formal_tau_seconds`; `*` marks directions the frozen
  estimator resolved regime-specifically (ATT 2→4 ratio 4.96; TMobile-LTE 2→1
  3.36, 2→4 3.49, 4→8 3.84; UMTS 2→1 69.54; per-regime medians are recorded
  in `tau_aggregation_<trace>.json`). Pooled estimates for the zeroed
  directions were negative (−0.008 to −0.215 s).
- **Artifacts:** `/home/vscode/final_tau_stage_b/` — 88 run dirs
  (`run_result.json`, `summary.json`, `events.jsonl`, `up.log`, `down.log`,
  `output.bin`, `runner.log`), `campaign_jobs.json`,
  `campaign_manifest.json`, `campaign_log.jsonl`, `campaign_done.json`,
  `tau_observations.jsonl`, per-trace `tau_observations_*.jsonl`,
  per-trace `tau_aggregation_*.json`, `stage_b_report.json`/`.txt`
  (34/34 verification checks pass), plus the retained blocker record and the
  amended-plan copy.
- **Integrity:** amended preregistration unchanged during Stage B
  (json `3b371c3f…`, txt `1062f993…`); Stage-B harness binary is the frozen
  `dde3c742…`; `tau_campaign_v3` (binary/plans/manifest) and Stage-A evidence
  show no file newer than the Stage-B start; no `.go` source changed; cleanup
  verified (no mm-link/tau-injection processes, no network namespaces, no
  qdisc residue, host links unchanged).
- **Not done (explicitly):** no oracle DP, no `T_fixed`/`T_constrained`, no
  `H`, no confidence intervals, no LCB gate, no cross-trace pooling, no
  fixed-C reruns, no `tau_campaign_v3` reruns, no timestamp substitution.
- **Next stage (Stage C, not started):** per-trace oracle analysis for the
  three new traces and the pre-registered five-trace `LCB_95 >= 0.10` gate.

### 22.80 Stage C final analysis: frozen oracle + final statistical gate (executed 2026-09-28)

**Status: `FINAL_ORACLE_ANALYSIS_COMPLETE — FINAL STATISTICAL GATE FAIL`
(`LCB_95 = −0.0026077 < 0.10`).** The pre-registered final analysis ran under
oracle protocol version 2 + final trace protocol v1 + amendment 1 using only
the frozen evidence (no experiment, no source change, no preregistration
change). Output dir: `/home/vscode/final_oracle_analysis/`. The two-trace
descriptive results (§22.74) are unchanged and remain historical.

- **Epoch rule applied (documented frozen-rule interpretation):** the retained
  oracle epoch set is the shared-valid **full 120 s epochs of the native
  downlink trace** ("native downlink trace semantics" + "final partial epoch
  excluded"). `ATT-LTE-driving` e6 and `TMobile-LTE-driving` e3 are
  shared-valid *replay* epochs (slicing is over `max(up,down)` per §22.63) but
  are the native traces' final partial epochs and therefore have **no frozen
  regime label**, so they cannot enter the constrained oracle (the frozen
  implementation rejects unlabeled retained epochs). They are documented as
  excluded; the choice was surfaced before finalizing and the gate outcome is
  FAIL under either reading.
- **Per-trace tau (PER-TRACE scope, no cross-trace pooling):** `EVDO` and
  `LTE` re-aggregated per trace from the v3 raw signed observations with the
  frozen median/`max(0,·)` estimator; the three new traces use the Stage-B
  per-trace aggregations. Raw signed observations preserved (counts:
  EVDO 32/18 negative, LTE 40/14, ATT 32/12, TMobile-LTE 16/4, UMTS 40/19;
  ranges as low as −16.50 s, as high as +38.97 s).
- **Results (seconds; B_e = 5 MiB; counterfactual completion-time
  equivalents):**

  | Trace | retained | T_best_fixed (C) | T_instantaneous | T_constrained | trans. | H |
  |---|---|---|---|---|---|---|
  | Verizon-EVDO-driving | 7 | 543.982429 (C8) | 534.805587 | 535.057012 | 3 | 0.0164075465 |
  | Verizon-LTE-driving | 11 | 68.519878 (C16) | 68.260657 | 68.506557 | 3 | 0.0001943981 |
  | ATT-LTE-driving | 6 | 53.180538 (C4) | 53.063101 | 53.172222 | 1 | 0.0001563742 |
  | TMobile-LTE-driving | 3 | 12.165657 (C4) | 12.150952 | 12.158184 | 1 | 0.0006142775 |
  | TMobile-UMTS-driving | 7 | 173.111726 (C2) | 171.733363 | 172.603064 | 2 | 0.0029383433 |

  The sanity bound `T_instantaneous <= T_constrained <= T_best_fixed` holds
  for all five traces. Excluded epochs and reasons are recorded per trace in
  `final_oracle_results.json` (byte floor, observed fraction, and the native
  final partial epochs).
- **Final statistical gate (n = 5, df = 4, one-sided 95 % Student-t,
  t = 2.1318467863):** H values
  `[0.0164075465, 0.0001943981, 0.0001563742, 0.0006142775, 0.0029383433]`;
  mean `0.0040621879`; sample SD `0.0069959662`;
  `LCB_95 = 0.0040621879 − 2.1318467863 · 0.0069959662 / √5 = −0.0026077024`;
  threshold `0.10`; **FAIL**. The pre-declared 10 % population-level
  exploitable-headroom requirement is not met; no RL/adaptive/aria2
  superiority or causal real-world claim is made.
- **Validation / integrity (17/17 checks pass):** the analysis DP is a
  faithful replica of the frozen Go oracle (`oracle_replica.py` reproduces
  `oracle_stage_v3` fixed/instantaneous/constrained values to 1e-9); exactly
  five H values feed the CI; `epoch_pilot_v2`, `tau_campaign_v3`, Stage-A,
  Stage-B, and `oracle_stage_v3` evidence unchanged; preregistration v4 +
  amendment 1 hashes unchanged (`3b371c3f…` / `1062f993…`); no `.go` source
  changed; no experiment process started; no netns/qdisc residue; raw signed
  taus unaltered; no epoch excluded for rate/H reasons.
- **Artifacts:** `/home/vscode/final_oracle_analysis/` —
  `final_oracle_results.json`/`.txt`, `final_statistical_gate.json`/`.txt`,
  `final_trace_summary.json`/`.txt`, `oracle_replica.py`, `final_analysis.py`,
  per-trace `tau_aggregation_*.json` and `tau_observations_*.jsonl`
  (EVDO/LTE filtered per trace), `stage_c_decision_required.json` (the
  epoch-semantics decision record), and `scratch/` (intermediate inputs).
- **Not done (explicitly):** no network experiment, no fixed-C/tau rerun, no
  oracle reinterpretation of invalid epochs, no cross-trace tau pooling, no
  parameter tuning, no claim beyond the frozen counterfactual model.

### 22.81 Salvage Protocol v7 frozen (documentation only, 2026-09-28): BLOCKED pending human parameters

**Status: `FINAL_S_PROTOCOL_FROZEN — BLOCKED_PENDING_HUMAN_PARAMETERS`.**
A new, separate prospective study was specified for the question "When does
adaptive HTTP range downloading benefit from changing future chunk size under
finite-file, tail-aware conditions?". It is documentation only: no experiment,
no pilot, no source change, and no modification of the completed cellular
concurrency/oracle study. Artifacts:
`testdata/salvage_s_protocol_v7/` (`final_s_protocol.json`, `.txt`,
`README.md`) — deliberately outside the oracle preregistration family.

- **Frozen scope/design (complete):** single-server HTTP/1.1 contiguous
  byte-range downloads; future chunk size affects new allocations only;
  in-flight ranges keep their original boundaries. Mechanism = per-request
  server-side rate limiting at rate `R` + fixed nonzero RTT `D` (no packet
  loss, no dynamic/authored bandwidth schedules, no hidden penalties; the
  existing `internal/experiment/server` already supports a fixed rate and a
  base delay). Chunk arms 256 KiB / 1 MiB / 4 MiB / 16 MiB. Primary endpoint =
  finite-file completion time (the earlier weighted `J` objective is removed);
  secondary diagnostics listed. Bayesian hierarchical model
  `y_cb = mu_c + u_b + eps_cb` on `log(T/T_ref)` with frozen primary priors
  (`Normal(0,1.5)`, `HalfNormal(0,0.5)`, `HalfNormal(0,0.5)`) and frozen prior
  sensitivity (`Normal(0,3)`, `HalfNormal(0,1)`, `HalfNormal(0,1)`);
  `mu_fixed* = min(mu_256K, mu_1M, mu_4M, mu_16M)`; decision
  `P(Delta_k <= -delta_T | data) >= 0.95`; switching family `16->4`, `4->1`,
  `1->256K`; one switch per run with chunk size fixed afterwards; trigger
  `B_pending = TotalSize - CompletedBytes - InFlightBytes` evaluated from
  tracker state immediately before allocation; randomization within blocks;
  fixed-sequence multiplicity; confirmatory `n <= 12`; SSR via 1000 synthetic
  datasets per frozen scenario; strict pilot/confirmatory separation (no
  pooling pre-registered); trace-backed validation out of scope for v7.
- **Blocked items (no value chosen or inferred):** human-owned `R`, `D`,
  `FileSize`, `N`; the trigger firing rule/switching point; the analytic
  definition of `T_tail_primary` (and therefore `delta_tail = 0.05 *
  T_tail_primary` and the `T_tail_primary >= 20 s` check); the Stage 0S
  go/no-go rule and its designated feasibility quantities; the SSR scenario
  grid. Clarifications also required for the `delta_T` log-scale application
  (the log-scale equivalent of 5 % is `log(0.95) = -0.051293`), the
  exploratory switching case (as written it duplicates the third confirmatory
  member), and the fixed-sequence order.
- **Validation record (§24 of the task):** 11 PASS, 1 PASS-with-clarification,
  5 BLOCKED — all blocked checks correspond exactly to the missing items
  above. No parameter was substituted and no "reasonable" value was invented.
- **Path note:** the task named `/workspace/mvp`; the repository is
  `/workspaces/mvp`, and the artifacts live under
  `/workspaces/mvp/testdata/salvage_s_protocol_v7/`.
- **Next stage (not started):** the human supplies/accepts the missing
  parameters and specifications, the validation is re-run, and only then can
  the status become `FINAL_S_PROTOCOL_FROZEN — READY FOR STAGE 0S`. Stage 0S
  (the 32-run pilot) must not start before that.

### 22.82 Salvage Protocol v7 parameter-resolution pass (documentation only, 2026-09-28): proposals recorded, still BLOCKED

**Status: `FINAL_S_PROTOCOL_FROZEN — BLOCKED_PENDING_HUMAN_PARAMETERS`
(unchanged).** A specification-resolution pass evaluated coherent candidate
mechanism packages, produced exact analytic proposals for every previously
missing rule, and resolved the two task-directed items. **Nothing was
approved or frozen except the two task-directed resolutions; no experiment was
run.** New artifact: `PARAMETER_DECISION_MEMO.md` in
`testdata/salvage_s_protocol_v7/` (the JSON/TXT/README were updated to match).

- **Candidate packages (analytically evaluated, none approved):**
  A `R=4 Mbps, D=40 ms, FileSize=512 MiB, N=4` → `T_ref` 268.4355 s,
  `T_tail_primary` 33.5944 s (admissible);
  B `R=8 Mbps, D=40 ms` → `T_ref` 134.2177 s, `T_tail_primary` 16.8172 s
  (**inadmissible: tail < 20 s**);
  C `R=4 Mbps, D=80 ms` → `T_ref` 268.4355 s, `T_tail_primary` 33.6344 s
  (admissible);
  D (project precedent: `matrix.go` server-throttle 512 KiB/s + 20 ms latency)
  `R=512 KiB/s, D=20 ms` → `T_ref` 256.0000 s, `T_tail_primary` 32.0200 s
  (admissible). Tail-window data = `N × C_max` = 64 MiB (12.5 % of the file);
  the memo has per-arm service-time/efficiency tables.
- **Proposed (pending approval):** `T_tail_primary = D + C_max/R`;
  `delta_tail = 0.05·T_tail_primary`; trigger
  `B_switch = N × S0` (64/16/4 MiB for 16→4 / 4→1 / 1→256K) evaluated
  immediately before allocation with the frozen one-switch/edge-case
  semantics; Stage 0S go/no-go rule (mechanism presence within ±25 % of
  `D + s/R`, strictly increasing per-arm service times, realized 16 MiB tail
  ≥ 20 s, reliability/variance feasibility); SSR grid (3 effects × 2
  `sigma_block` × 3 `sigma_resid` = 18 scenarios × 1000 datasets).
- **`delta_T` interpretation (recommendation only):** use the log-scale
  threshold `delta_log = log(0.95) = −0.051293294` in
  `P(Δ_k ≤ delta_log | data) ≥ 0.95` to preserve the intended statement
  "a 5 % reduction in completion time"; the literal `−0.05` alternative is
  recorded but not chosen. Not approved.
- **Resolved (task-directed, frozen):** the duplicated exploratory switching
  case (`1 MiB → 256 KiB`) is removed — v7 defines no exploratory switching
  condition; the fixed-sequence order is frozen as `16→4`, `4→1`, `1→256K`
  with stop-at-first-failure semantics and no ranking/best-policy conclusion.
- **Validation record now:** 13 PASS, 2 PROPOSED-pending-approval,
  3 BLOCKED (the blocked checks are the unapproved core parameters, the
  `delta_T` interpretation, and the package-dependent `T_tail_primary ≥ 20 s`
  check). The remaining human decisions are the six listed in §22.82's memo
  and the protocol JSON (`remaining_human_decisions`).
- **No experiment:** no mm-link, server/client, network replay, pilot, or new
  experiment directory; no source change; no frozen oracle/Stage A/B/C
  evidence change. Stage 0S must not start until all six decisions are
  explicitly approved and the validation is re-run.

### 22.83 Salvage Protocol v7 final human decision (documentation only, 2026-09-28): FROZEN and READY_FOR_STAGE_0S

**Status: `FINAL_S_PROTOCOL_FROZEN — READY_FOR_STAGE_0S`
(`ready_for_stage_0s = true`).** The human approved every outstanding decision;
the protocol artifacts were updated to the frozen state. No experiment was run
and Stage 0S execution remains a separate task.

- **Frozen mechanism package (Candidate A):** `R = 4 Mbps = 500,000 B/s`,
  `D = 40 ms`, `FileSize = 512 MiB = 536,870,912 B`, `N = 4`. Candidates
  B/C/D are unselected and retained in `PARAMETER_DECISION_MEMO.md` for
  provenance (B analytically inadmissible: tail < 20 s).
- **Frozen quantities:** `T_ref = 268.435456 s`;
  `delta_T = 0.05·T_ref = 13.4217728 s` (time-scale statement, a 5 %
  completion-time reduction); `delta_log = log(0.95) = −0.05129329438755058`
  (the primary decision uses this exact log-scale threshold; `−0.05` is
  explicitly **not** used); `T_tail_primary = D + C_max/R = 33.594432 s`;
  `delta_tail = 0.05·T_tail_primary = 1.6797216 s`; tail minimum
  `T_tail_primary >= 20 s` satisfied; tail-window data `N·C_max = 64 MiB`
  (12.5 % of the file).
- **Arithmetic note (transcription):** the decision text stated
  `T_tail_primary = 33.594304 s`; independent recalculation of the frozen
  formula gives `33.594432 s` (0.128 ms difference). Per the section-15
  independent-recalculation requirement the formula-derived value is operative;
  the stated decimal is retained for provenance. Both satisfy the ≥ 20 s
  requirement.
- **Other frozen decisions:** arms 256 KiB / 1 MiB / 4 MiB / 16 MiB;
  confirmatory strategies `16→4`, `4→1`, `1→256K` with **no** exploratory
  strategy; trigger `B_pending = TotalSize − CompletedBytes − InFlightBytes`
  evaluated immediately before allocation with `B_switch = N·S0` =
  64/16/4 MiB, one switch maximum, in-flight ranges never resized; Bayesian
  model/priors/sensitivity unchanged; `mu_fixed*` model-based; decision
  `P(Δ_k ≤ delta_log | data) ≥ 0.95`; fixed sequence `16→4`, `4→1`, `1→256K`
  with stop-at-first-failure; Stage 0S 32-run pilot (8 blocks × 4 arms) for
  feasibility only with the frozen GO/NO-GO rule; confirmatory `n ≤ 12`; SSR
  grid 18 scenarios × 1000 datasets = 18,000; pilot data not pooled; trace
  validation out of scope.
- **Validation:** 19/19 checks PASS (including the independent recalculation
  and cross-artifact consistency); all six human decisions approved; no
  blocking items remain.
- **Artifacts:** `testdata/salvage_s_protocol_v7/` — `final_s_protocol.json`,
  `final_s_protocol.txt`, `PARAMETER_DECISION_MEMO.md`, `README.md`.
- **Safety:** no mm-link/server/client/network replay, no pilot, no new
  experiment directory, no source change, no frozen oracle/Stage A/B/C
  evidence change. Stage 0S execution must not start from this task.

### 22.84 Stage 0S pilot execution (executed 2026-09-28): GO

**Status: `STAGE_0S_COMPLETE — GO`.** The frozen 32-run Stage 0S feasibility
pilot ran under the frozen mechanism; all four pre-specified criteria pass, so
the confirmatory stage is justified by the protocol. **This is not a
confirmatory adaptive-effect result** — no switching strategy was run, no
winner is reported, and no chunk-size-adaptation benefit is claimed. Window:
2026-09-28 10:19 → 12:52 UTC. Artifacts:
`/workspaces/mvp/testdata/salvage_s_stage0s/`.

- **Design executed:** 8 replicate blocks × 4 fixed arms (256 KiB, 1 MiB,
  4 MiB, 16 MiB) = **32/32 planned, 32 attempted, 32 clean, 0 technical
  failures, 0 reruns** (the frozen protocol contains no technical-rerun
  authorization, so none were performed). Randomized order within each block
  (seed 20260928, recorded in `manifest/manifest.json`); sequential execution;
  fresh server/client state per run.
- **Mechanism:** `R = 500,000 B/s`, `D = 40 ms`, `FileSize = 512 MiB`,
  `N = 4`; fixed chunk size per run (no switching). Reused existing
  infrastructure: `internal/experiment/server.NewLogical` (deterministic
  generated content, bounded memory — essential because the host had only
  ~0.7 GB RAM available; identical range semantics and application-level
  pacing to the materialized server) and `internal/downloader` (fixed mode) +
  `internal/metrics` per-attempt records. HTTP/1.1; no tc/netem.
- **Criterion 1 — mechanism presence: PASS.** Pooled median server-side
  per-request service time vs `D + s/R`: 256 KiB 0.577717 s (+2.38 %),
  1 MiB 2.190526 s (+2.50 %), 4 MiB 8.640930 s (+2.52 %), 16 MiB 34.427871 s
  (+2.48 %) — all within ±25 %.
- **Criterion 2 — direction: PASS.** Strictly increasing medians
  0.577717 < 2.190526 < 8.640930 < 34.427871.
- **Criterion 3 — tail sufficiency: PASS.** Realized 16 MiB-arm tail
  (client end − start of the first request in the final 64 MiB region) pooled
  median **34.977589 s** ≥ 20 s and within
  `[0.8·T_tail_primary, 1.25·T_tail_primary] = [26.8755456, 41.99304] s`.
- **Criterion 4 — feasibility/reliability: PASS.** Clean runs 32/32 (≥ 28);
  maximum wall-clock 296.629 s (≤ 1800 s); within-block residual SD of
  `log(T/T_ref)` 0.000795 (≤ 0.25), df = 21.
- **Formal decision:** all four criteria PASS → **GO**. A GO means only that
  the frozen mechanism and finite-file tail were sufficiently realized under
  the pre-specified feasibility criteria to justify the next pre-specified
  stage.
- **Artifacts:** `manifest/` (manifest + run ledger), `raw/` (32 raw run
  artifacts with client records and server request logs), `per_run/` (32
  per-run summaries), `analysis/` (`stage0s_analysis.json`, `per_run.csv`,
  `per_arm.json`, `direction.json`, `tail.json`, `reliability.json`,
  `criteria.json`), `final_report/stage0s_report.md`, `logs/`, `tools/`
  (`stage0s_run.go`, `run_pilot.py`, `analyze_stage0s.py`,
  `preflight/preflight_smoke.go`).
- **Integrity/safety:** no repository source file changed (execution tools
  live under `testdata/`, which Go tooling ignores); the frozen protocol and
  all oracle/Stage A/B/C evidence unchanged; no mm-link/experiment processes
  or qdisc/netns residue remain; no SSR simulation or confirmatory Bayesian
  analysis was run.
- **Next:** the confirmatory Stage 1 is justified under the protocol but
  **not started**; it requires a separate task with fresh data and the frozen
  arms/strategies/trigger/priors/effect threshold/analysis.

### 22.85 Pre-Stage-1 file-size scaling / headroom analysis (analytical simulation only, 2026-09-28)

**Status: `STAGE1_SCALING_ANALYSIS_COMPLETE — ANALYTICALLY INSUFFICIENT
UNDER FROZEN MECHANISM`.** A deterministic analytical simulation asked whether
increasing the file size can increase the **relative** completion-time
headroom available to chunk-size adaptation under the frozen static
rate + RTT mechanism. **No empirical network experiment was run.** Artifacts:
`testdata/salvage_s_scaling_analysis/`.

- **Method:** deterministic work-conserving finite-file scheduler simulator of
  the frozen semantics (N=4, contiguous ranges, future-only chunk size,
  `B_pending = TotalSize − CompletedBytes − InFlightBytes` sampled immediately
  before each allocation, one switch, no in-flight resizing, final partial
  range, EOF), under two never-blended service models: MODEL A `t(s)=D+s/R`
  and MODEL B `t(s)=a+b·s` (OLS fit to the four frozen Stage 0S pooled median
  service times; `a=0.041837515 s`, `b=2.049600672e-6 s/byte`,
  R²=0.9999999885). Grid 64 MiB–32 GiB (plus a dense 512–640 MiB residue
  scan). Model B reproduces the Stage 0S observed fixed-S medians within
  0.04–0.35 %; Model A underestimates them by ~2.5 %.
- **Fixed-S scaling:** for exact full bursts `T_S/T_ref = 1 + R·D/s` is
  scale-invariant (+0.119 % for 16 MiB, +0.477 % for 4 MiB, +1.907 % for
  1 MiB, +7.629 % for 256 KiB), and the model-implied lowest completion-time
  fixed condition is 16 MiB on the entire grid.
- **Frozen strategies:** all relative gains are **negative** on the entire
  grid — 16→4 max −0.0007 %, 4→1 max −0.3579 %, 1→256K max −1.7867 %
  (Model A; Model B qualitatively identical). At the frozen 512 MiB baseline:
  −0.0447 %, −0.4019 %, −1.8307 %. The frozen trigger `B_switch = N·S0` fires
  exactly at a full final burst, where replacing S0 chunks with S1 chunks adds
  per-request D overhead without shortening the tail.
- **Exploratory single-switch upper envelope (not the frozen policy):** ≤ 0 on
  the entire main grid (aligned power-of-two files have no tail-packing
  defect); the dense residue scan reaches at most **+0.3545 %** (≈ the
  16 MiB→4 MiB bulk overhead difference), far below the frozen 5 % threshold.
- **Answers:** (A) increasing F does **not** increase relative headroom — the
  bounded one-time tail effect is diluted (relative tail share falls as 1/F)
  and the frozen strategies converge to their negative bulk-overhead
  asymptotes; (B) absolute savings are ≤ 0 for the frozen strategies (the
  exploratory envelope can show a growing absolute saving at a fixed ~0.36 %
  relative gain); (C) no frozen strategy reaches 5 % anywhere on the grid;
  (D) the exploratory envelope does not reach 5 % either; (E) the current
  frozen Stage 1 design is **not** mechanistically capable of a practically
  meaningful effect at 512 MiB.
- **Implication:** `STAGE1_IMPLICATIONS.md` states
  **ANALYTICALLY INSUFFICIENT UNDER FROZEN MECHANISM**. **No amendment is
  proposed** — a larger file size is not justified (it reduces relative
  headroom); any change to the mechanism, trigger timing, or design requires a
  separately frozen protocol decision. The frozen protocol was not modified.
- **Artifacts:** `manifest/manifest.json`; `model/model_definition.json`;
  `simulation/simulation_assumptions.md`; `analysis/` (`scaling_analysis.json`,
  `fixed_s.csv`, `frozen_switches.csv`, `envelope.csv`, `dense_modular.csv`);
  `figures/fig1..fig7`; `reports/scaling_headroom_report.md`;
  `STAGE1_IMPLICATIONS.md`; `tools/` (deterministic simulator, figures,
  report generator).
- **Safety:** zero empirical download runs, zero mm-link/server/client/tc
  processes, zero network replay; no source-code changes outside `testdata/`;
  the frozen protocol and all Stage 0S / oracle / Stage A/B/C artifacts were
  not modified.

### 22.86 Adaptive-S mechanism pivot screening (analytical simulation only, 2026-09-28): no plausible mechanism found

**Status: `MECHANISM_PIVOT_SCREENING_COMPLETE — NO PLAUSIBLE MECHANISM
(MECHANISTICALLY INSUFFICIENT FOR ALL SCREENED CANDIDATES)`.** With the static
rate + RTT mechanism closed for adaptive-S confirmation (§22.85), this study
asked under what realistic network/server mechanism the preferred chunk size
could change *during* a finite download. **No empirical network experiment was
run.** Artifacts: `testdata/salvage_s_mechanism_pivot/`.

- **Trace inventory:** 8 native Mahimahi **delivery-opportunity** traces
  (capacity schedules; mm-link loops at EOF) with strong genuine variability
  (p90/p10 = 5.5–27.4; 4–18 % of seconds below 25 % of the median), plus 23
  **throughput-trajectory** traces (NYU-METS/UGent) that are measured
  end-to-end throughput, **not** path capacity (the M3 envelope limitation
  applies), used only for context.
- **Analytical model:** deterministic event-driven finite-file scheduler
  simulator with four request-level service models — `shared` (aggregate
  capacity shared among in-flight requests; every request experiences every
  transition), `lockin_full` (per-request rate fixed at request start and held
  — the existing server's scheduled-rate semantics), `serialized`, and the
  closed v7 `constant` control. Traces are 1 s binned with the documented
  8000 bps M3 outage floor.
- **Moving-optimum test:** the preferred S does **not** change between
  stationary states. argmin_S over 0.25–8 MB/s aggregate is 16 MiB at every
  rate for the shared and lock-in models at 512 MiB (the 520/544 MiB variation
  is a file-size-residue effect) and a fixed 256 KiB for the serialized model;
  the real-trace low/high-rate regimes likewise give 16 MiB everywhere.
- **Causal vs stationary:** causal, observation-driven within-download
  switches are **zero or harmful** in every case (range −61.2 % … +0.0 %);
  under shared/serialized they never fire (no state-dependent preference), and
  under lock-in they fire only after volatility is visible and underperform the
  best fixed arm by 2–61 %. Every stationary-control causal gain is exactly
  0.000 %.
- **Headroom:** the only positive opportunity is a **clairvoyant** single
  switch in a lock-in stable→volatile case (oracle gains 0–32 %, varying with
  the alternation period: 0 % at 60 s, 7–23 % at 17 s, 11–32 % at 5 s). It
  requires perfect knowledge of the alternation phase and vanishes at longer
  periods — a fragile diagnostic artifact, not a causal opportunity. No
  screened mechanism provides a robust causal within-download S-switch
  headroom.
- **Classification (five-condition rule):** candidates A/B/C/D (shared
  time-varying capacity), E (server serialization / concurrency-dependent
  rate), and F (per-request lock-in + volatility) are all
  **MECHANISTICALLY INSUFFICIENT**; condition 1 (preferred S changes between
  states) fails for all, and causal adaptation (condition 3) fails or is
  harmful. **No new preregistered protocol is warranted** under the screened
  mechanisms; the frozen v7 protocol was not modified.
- **Additional evidence required before any future execution** (from the
  report §13): a documented/measured mechanism with a robust, reproducible
  state-dependent argmin (not residue- or phase-dependent); a pre-fixed causal
  policy capturing a material fraction of the oracle bound; a pre-declared
  eligibility rule over the trace population; and a **new** preregistered
  protocol at a new path.
- **Artifacts:** `inventory/` (trace inventory JSON/MD), `analysis/`
  (`screening.json`, `e1_stationary.csv`, `e2_transitions.csv`,
  `e2c_volatility.csv`, `e3_traces.csv`), `figures/fig1..fig7`,
  `reports/mechanism_pivot_report.md`, `manifest/manifest.json`, `tools/`
  (inventory, simulator, screening, figures, report), `model/`, `simulation/`.
- **Safety:** zero empirical download runs, zero mm-link/server/client/tc
  processes, zero network replay; no source changes outside `testdata/`; the
  frozen v7 protocol and all Stage 0S / scaling / oracle / Stage A/B/C
  artifacts were not modified.

### 22.87 Adaptive-N mechanism screen (analytical simulation only, 2026-09-28): no robust mechanism, no protocol justified

**Status: `ADAPTIVE_N_SCREEN_COMPLETE — NO ROBUST MECHANISM (NO PROTOCOL
JUSTIFIED)`.** With the S mechanism closed (§22.85–§22.86), this screen asked
whether adaptive concurrency N can have a genuine, causally exploitable
state-dependent optimum under a realistic download mechanism. **No empirical
network experiment was run.** Artifacts:
`testdata/adaptive_n_mechanism_screen/`.

- **Prior concurrency evidence (unchanged):** T1 (live per-response pacing) —
  aggregate throughput scales monotonically with C, no live reversal at equal
  1 MiB granularity; L3 — simulated oracle headroom 8.1 % (< 10 % gate);
  L4 — 17.6 % simulated headroom but the live sweep did **not** reproduce the
  over-concurrency penalty (`NOT_READY_LIVE_MISMATCH`, GATE E FAIL); M3 — no
  C sensitivity under the aggregate throughput envelope. These are decisive
  for this screen.
- **Trace inventory:** the 8 native delivery-opportunity traces represent
  time-varying **aggregate capacity** only; the 23 throughput-trajectory
  traces are measured throughput, not capacity (M3 limitation). **No existing
  trace contains per-flow fairness, queueing, or per-connection cost
  information**, so no trace can establish a concurrency-sensitive mechanism.
- **Models screened** (deterministic event-driven simulator, F = 512 MiB,
  S = 1 MiB fixed, RTT 40 ms, N ∈ {1,2,4,8,16}): `direct` (T1 control),
  `fair` (aggregate C shared), `fair_bg` (background flows B), `cap` (server
  parallelism K), `degrade` (server aggregate capacity C/(1+γ(k−1))).
- **Stationary optima:** `direct`, `fair`, `fair_bg` are monotone (preferred
  N = 16 at every state); `cap` has a state-dependent preferred N but a flat
  plateau (the model-implied best fixed N is already robust; oracle headroom
  0 %); `degrade` has a true interior optimum whose preferred N rises with C
  (1 → 4 across 0.5–32 MB/s) and shifts with γ, and the state-dependence
  survives the entire predeclared C × γ grid.
- **Transitions and causality:** causal, observation-driven single switches
  (median of the last 3 completed-request throughput samples) give gains of
  **−128.5 % … +6.66 %** (median +1.50 %); the coarse clairvoyant oracle gives
  0 % … +18.69 %; every stationary-control gain is ~0. Causal ≥ 5 % occurs in
  **2/10** transition cases, all in the `degrade` model at severe capacity
  drops (up to +6.66 %); the causal/oracle capture is inconsistent
  (near 100 % at severe `degrade` drops, 0.5 % vs 17.4 % in the γ transition).
- **Classification:** A (per-flow sharing), B (server cap), D (background
  traffic), F (server saturation), G (changing capacity + sharing) are
  **MECHANISTICALLY INSUFFICIENT** (monotone or plateau; no causal gain).
  C/E/H (concurrency-dependent server cost / queueing) are **PLAUSIBLE —
  FRAGILE**: they pass the six analytical ROBUST conditions but fail physical
  grounding — the only live validation of this mechanism class (L4 GATE E)
  rejected it, and the current controlled server has no concurrency-dependent
  capacity cost; the causal capture is also severity-dependent.
- **Conclusion:** no mechanism reaches **MECHANISTICALLY PLAUSIBLE — ROBUST**
  in the current infrastructure, so **no new empirical protocol is justified**
  and no adaptive-N experiment was run. The exact requirements for any future
  protocol are in the report §15: establish the mechanism live first (the
  current stack does not), ground the parameters by measurement (never tune),
  freeze a causal policy (one adjacent-step switch, observed-throughput
  trigger, pre-registered N mapping), pre-register the model-implied best
  fixed N baseline and the 5 % reference, and use fresh evidence at a new
  path.
- **Artifacts:** `analysis/` (`screening.json`, `e1_stationary.csv`,
  `e2_transitions.csv`, `e3_robustness.csv`, `e4_severity.csv`),
  `figures/fig1..fig8`, `reports/adaptive_n_mechanism_report.md`,
  `inventory/n_trace_inventory.md`, `mechanisms/`, `model/`, `simulation/`,
  `manifest/manifest.json`, `tools/` (simulator, screening, figures, report).
- **Safety:** zero empirical download runs, zero mm-link/server/client/tc
  processes, zero network replay; no source changes outside `testdata/`; all
  frozen S/oracle/Stage A/B/C and historical concurrency evidence unchanged.

### 22.88 Final project evidence synthesis / paper-viability audit (documentation only, 2026-09-28)

**Status: `PAPER-READY — MECHANISTIC / NEGATIVE-RESULT STORY`.** A synthesis
of the completed evidence only (zero new experiments) determined what the
project actually establishes and what the final paper may claim. Artifacts:
`testdata/final_project_synthesis/` (six reports, seven final figures, and a
manifest).

- **Final research question:** under realistic, live-grounded mechanisms, when
  does adaptation of HTTP range concurrency (N) or future chunk size (S) have
  sufficient **causal headroom** to justify an adaptive policy at all — and
  what mechanism must first exist for that headroom to be real? This is the
  original proposal's scope (adaptive N/S, ablations, joint, policies, RL,
  controlled server, aria2 baseline) narrowed to a mechanism-first gate.
- **Strongest contribution:** a pre-registered, mechanism-first methodology for
  determining whether adaptive HTTP range scheduling has sufficient causal
  headroom **before** introducing policy complexity — supported by the S and N
  closure chains and the frozen negative results.
- **Evidence chains (unchanged, synthesised):** S — static rate+RTT → Stage 0S
  GO → frozen switching rules non-positive → file-size scaling does not rescue
  relative gain → broader S screening finds no robust causal opportunity. N —
  direct throttle monotone → M3 envelope no C sensitivity → alternative
  analytical mechanisms → some create an interior N optimum → those fail live
  physical grounding (L4 GATE E; T1) → no robust adaptive-N mechanism.
- **Evidence-type discipline:** every claim is mapped to LIVE EXPERIMENTAL,
  TRACE REPLAY, DETERMINISTIC ANALYTICAL MODEL, SIMULATION, or
  DIAGNOSTIC/EXPLORATORY. The analytical +6.66 % N headroom is explicitly not
  empirical; oracles are not policies; the S envelope is not a measured
  speedup; the aria2 runs are empirical only for their tested configurations.
- **RL status:** **deferred, not abandoned** — no robust causal N/S opportunity
  exists to optimize; the executed RL results were negative/descriptive (DQN
  v2 75.7 % slower than aria2, ratio 1.757 [1.669, 1.850]; v5 2.32× slower).
  Revisit only after a live-grounded mechanism is established.
- **aria2 status:** fairly configured (calibrated v1/v2); the granularity-
  matched wall-clock control differs by +3.6 % (n=10); the final head-to-heads
  are negative. No ranking or general comparison claim is supported.
- **Remaining evidence gap:** no live-grounded mechanism creates a robust,
  causal, state-dependent optimum for N or S in the controlled environment.
  Additional empirical work is **not** required for the negative-result paper;
  any future positive study must first satisfy the five conditions in
  `reports/future_work_boundary.md` (live-grounded mechanism, measured
  parameters, pre-registered causal policy, pre-registered baseline/target,
  fresh evidence at a new path).
- **Reports:** `reports/final_evidence_matrix.md`,
  `reports/paper_viability_audit.md`, `reports/figure_selection.md`,
  `reports/proposal_alignment.md`, `reports/claims_we_can_make.md`,
  `reports/future_work_boundary.md`; final figures
  `figures/fig1_system_methodology.png` … `fig7_practical_baseline_evidence.png`;
  `manifest/manifest.json`.
- **Safety:** zero new experiments; no downloads, mm-link, server/client,
  tc/netem, or replay; all historical evidence and frozen protocols unchanged.

### 22.89 aria2-competitive benchmark (executed 2026-09-28): WIN REPRODUCED

**Status: `ARIA2_COMPETITIVE_WIN_REPRODUCED`.** A memory-safe replication
(deterministic logical server, identical per-response pacing semantics) of the
historical D2 workload established, through predeclared discovery blocks and a
fresh 30-pair final block, that our fixed-policy downloader (C=16, 4 MiB
chunks) reproducibly beats the strongest aria2 1.37.0 configuration under the
frozen D2/512 MiB workload family.

- **Workload (frozen):** 512 MiB (536,870,912 B); per-response pacing sampled at
  request start and held: 640 KiB/s (0–2 s) -> 160 KiB/s (2–4 s) -> 2500 KiB/s
  (>=4 s); tc/netem on `lo`, 40 ms latency + 0.4% loss; identical for both
  systems.
- **D2 reproduction (20 fresh pairs, historical configs):** v6 mean 27.96 s vs
  aria2-F 53.45 s; geo ratio **0.5230** CI95 [0.5200, 0.5261], 20/20. The
  historical result (ratio 0.5816 [0.566, 0.598]) is reproduced in direction and
  magnitude class.
- **aria2 frontier (discovery; actual served ranges recorded):** split/max-conn
  1/2/4/8/16 -> median 833/419/210/106/53 s; strongest is **split=16,
  max-connection-per-server=16, min-split 1M** (15 x 32 MiB pieces, all starting
  <2 s). The min-split dimension is degenerate at 512 MiB for split <= 16
  (spot-checked 1M vs 32M). Not a strawman: the full split frontier was run and
  16 is the strongest.
- **Our frontier (discovery; 40-config grid C in {1,2,4,8,16} x chunk in
  {256K,512K,1M,2M,4M,8M,16M,32M}):** best is **C=16, chunk 4 MiB**; corrected
  3-run median **19.99 s** (the driver bookkeeping bug recorded the 2-run median
  20.30 s; the frozen selection is top-1 under both; top-3: 4M 19.99, 8M 24.52,
  512K 24.53 s).
- **Final block (30 fresh pairs, frozen configs, predeclared order):** ours mean
  20.12 s / median 20.02 s vs aria2 mean 53.56 s / median 53.32 s; geometric
  ratio **0.3756** CI95 [0.3728, 0.3785]; relative improvement **62.4%**;
  **30/30 wins**; paired sign-flip permutation p ~ 5e-5; all 30 runs
  content-verified; zero config changes during the block.
- **Mechanism:** totals over the 30 runs: ours issues 3840 ranges of 4 MiB (480
  start <2 s, 3360 >=4 s) = 128 per run; aria2 issues 450 ranges of 32 MiB plus
  30 unranged probes (480 requests; 16 per run), all starting <2 s at the
  640 KiB/s phase and held there (~51 s each). (Corrected 2026-10-11: the earlier
  wording read as per-run counts.) The win is a **request-granularity x
  phase-locked-pacing interaction**, not generic adaptive superiority.
- **Controls:** granularity-matched control A (ours C=16, 32 MiB on D2) ratio
  **0.862** CI [0.839, 0.885] (10 pairs) — the win shrinks as granularity is
  matched; constant-rate control (2.5 MB/s, no transition): ours-4M 15.3 s,
  ours-32M 16.7 s, aria2 24.8 s — the 4M-vs-32M gap collapses (2.3x -> 1.1x),
  confirming the effect is phase-locked rather than a bare range-size artifact.
- **Scope:** only the frozen D2/512 MiB workload family; no universal
  aria2-superiority, consumer-network, or out-of-family claim. This becomes the
  project's **primary competitive result** (a bounded positive), complementing
  the mechanism-first negative-result story of §22.88.
- **Artifacts:** `testdata/aria2_competitive_benchmark/` (`tools/bench_run.go`,
  `tools/run_benchmark.py`, `tools/analyze_benchmark.py`, `manifest/`,
  `reproduction_d2/`, `discovery_aria2/`, `discovery_ours/`,
  `final_reproduction/`, `mechanism/`, `analysis/`, `figures/fig1..fig9`,
  `reports/win_mechanism.md`, `reports/tables.md`). Historical D2 evidence and
  all frozen study artifacts unchanged.
- **Integrity:** the aria2 frontier split 1/2/4 runs first hit the tool's 180 s
  timeout (invalid; retained under `discovery_aria2/invalid_timeout/`) and were
  re-run with `-aria2-timeout 1500s` (valid); no invalid final pairs; qdisc
  cleanup checked.

### 22.90 Joint N+S phase-locked pacing screen (executed 2026-09-29, analytical only): SCREEN FAILED

**Status: `ANALYTICAL_SCREEN_FAILED — NO CAUSAL JOINT HEADROOM`.** The fixed
62.4 % aria2 benchmark win motivated a bounded follow-up question: can a causal
controller that changes **both** N and S during a download exploit a
predeclared family of phase-locked pacing regimes and beat the best fixed
configuration? A deterministic event-driven simulator calibrated to the
measured D2 evidence (40/40 fixed configs within 3 %; aria2 anchor −0.8 %) was
used for the predeclared analytical screen. **No empirical experiment was
run** — the analytical gate failed, which is the frozen stop condition.

- **Frozen design:** two-phase family slow 640 KiB/s -> fast 2500 KiB/s,
  transition T in {1,2,4,8} s, 512 MiB; fixed frontier N in {1,2,4,8,16} x
  S in {256K,1M,4M,16M,32M}; frozen 8-action joint action space; frozen start
  (4,4M); primary one-switch causal family (median-throughput ratio signal;
  thresholds {1.25,1.5,2.0}; targets {(16,16M),(16,4M),(4,16M)}); diagnostic
  two-switch probe, blind-ramp scan, optimistic causal bound, clairvoyant
  oracle; stationary controls; predeclared gate G1-G4.
- **Fixed frontier:** **N\* = 16 in every regime** (per-response pacing is
  independent -> monotone, consistent with the live T1 result); S\* varies
  (1 MiB at T=1,2,8 s; 4 MiB at T=4 s); max wrong-config penalty 7.9 %. The
  variation is single-dimensional (S only) — no genuine *joint* opportunity.
- **Causal results:** every primary one-switch controller loses in every regime
  (−16.2 % to −28.1 %); the diagnostic two-switch probe peaks at **+1.1 %**
  (T=8 s); the oracle-tuned blind ramp peaks at **+3.7 %** (T=2 s); the
  optimistic causal bound (clairvoyant from the first completion onward) is
  −14.7 / −7.8 / −6.3 / +3.4 % — no causal class reaches the 5 % materiality
  threshold in even one regime.
- **Oracle:** unconstrained +6.3..+14.5 %, same-start <= +2.3 % — the regime
  information value is real but lives in the *starting configuration*, not in
  within-download adaptation (causal/oracle capture ~ 0).
- **Structural causes:** (1) the frozen (4,4M) start fetches only ~16 MiB in
  the first ~6.6 s vs ~66 MiB for the best fixed (~50 MiB ≈ 7 % handicap,
  unrecoverable even with perfect knowledge); (2) request-completion
  observation latency: a post-transition sample requires a request that
  *started* after T.
- **Verdict:** G1 PASS (weak), **G2 FAIL**, G3 PASS, G4 PASS -> overall FAIL ->
  STOP. No preregistration was created; no empirical execution.
- **Artifacts:** `testdata/joint_ns_phase_pacing_screen/` (`model/`,
  `simulation/`, `tools/` jns_sim.py + calibrate.py + run_screen.py +
  make_figures.py + make_report.py, `analysis/calibration.json` +
  `analysis/screen.json`, `figures/fig1..fig8`,
  `reports/joint_ns_phase_pacing_screen.md`, `manifest/`). The completed aria2
  benchmark and all frozen studies are unchanged.
- **Interpretation:** no robust joint adaptation opportunity under this
  mechanism; running LinUCB/RL here would search for an advantage the screen
  shows is not there. A future positive study requires a mechanism where a
  causal signal can act before the starting-phase loss is irrecoverable (or an
  N-varying optimum); see `reports/joint_ns_phase_pacing_screen.md` §9.

---

### 22.91 Live shared-bottleneck / queueing mechanism gate (executed 2026-09-29, LIVE): NO JOINT OPPORTUNITY

**Status: `NO_LIVE-GROUNDED_JOINT_N+S_OPPORTUNITY`.** The final mechanism branch
before deciding whether genuine joint N+S adaptation is defensible was tested
**live**: a real `tc tbf` bottleneck on `lo` (40 Mbit/s, burst 32 kb, limit
256 kb, ~46 ms max queueing delay, MTU temporarily 1500) shared by the internal
downloader and independently generated `iperf3` background traffic (4 paced
TCP streams) in three predeclared contention states (B = 0 / 0.5C / 0.85C).
Fixed configurations only: 32 MiB file, S = 1 MiB, N in {1,2,4,8,16},
3 reps/cell; 45/45 valid runs (content-verified, background verified).

- **Measurable contention cost: YES.** Median RTT rises from a 0.04 ms
  no-qdisc baseline to 33-86 ms (p95 up to 94 ms); qdisc drops 0-860 per run;
  TCP retransmissions 0-576; all grow with N (p95 N=16/N=1 = 1.64x/1.55x/1.15x
  for S0/S1/S2).
- **Finite/interior N optimum: NO.** S0 (no background) is flat (7.05-7.17 s,
  N*=8 with a 0.3 % margin = noise); S1 (0.5C) is flat-to-slightly-increasing
  (14.28-15.13 s, N*=16 with a 3.9 % margin, below the 5 % threshold);
  S2 (0.85C) falls strongly with N (62.5 s at N=1 -> 15.9 s at N=16) because
  more downloader flows win a larger TCP share against the paced background
  (its achieved rate falls 33.4 -> 22.9 Mbit/s) — a **boundary** optimum, not
  interior.
- **State-dependent N*: NO material difference.** N* differs only between the
  flat S0 and the boundary S1/S2, with sub-5 % margins on the flat side; the
  predeclared gate FAILED (G-N1 and G-N2), so per the frozen stop rule the S
  sweep, joint gate, within-download transition, causal controller, aria2
  comparison and confirmatory protocol were **not reached/created**.
- **Analytical calibration:** a deterministic shared-bottleneck model
  (`tools/queue_model.py`: work-conserving bulk + finite-file tail + fitted
  loss term + measured queueing RTT) fits the 15 live cell medians with 8.2 %
  RMSE (max 18.7 %) and predicts the same flat/boundary N* pattern; the bulk
  term is N-independent, so no interior optimum is structurally expected from
  a shared FIFO bottleneck under TCP.
- **Infrastructure:** failures found and fixed before the gate measurements
  (default 65536-byte loopback MTU bursts stalled downloads; iperf3 `-b` is
  per stream; intermittent background control-connection resets). Invalid runs
  are preserved under `mechanism_gate/invalid/` and excluded; the loopback MTU
  and qdisc were restored after the study.
- **Artifacts:** `testdata/live_joint_ns_queueing/` (`mechanism_gate/`,
  `analysis/gate_analysis.json`, `calibration/model_calibration.json`,
  `figures/fig1..fig5`, `reports/joint_ns_queueing_mechanism_report.md`,
  `manifest/`, `tools/`). No previous benchmark/evidence directory was modified.
- **Interpretation:** the shared-bottleneck/queueing mechanism is
  **INSUFFICIENT** for joint adaptation (work conservation removes the
  completion-time interior optimum); no further mechanism was invented in this
  task. The fixed 62.4 % aria2 benchmark remains the project's primary
  competitive result and is unchanged.

---

### 22.92 Correlated loss / range-retry mechanism screen (executed 2026-09-29, analytical only): NO JOINT OPPORTUNITY

**Status: `NO_JOINT_N+S_OPPORTUNITY_UNDER_CORRELATED_LOSS`.** The final
mechanism branch — correlated burst loss + the downloader's range-retry
semantics — was screened analytically before any live execution. A deterministic
event-driven simulator (`tools/loss_sim.py`) drives a time-indexed two-state
Gilbert–Elliott loss process (G: 0.4 % loss, 20 s sojourn; B: 50 % loss,
0.15 s/2.0 s sojourn) through the frozen retry semantics (30 s request timeout,
3 same-range attempts, 0.1 s retry gap, in-flight immutability) with
per-response pacing at the D2 initial rate (640 KiB/s). F = 512 MiB;
N ∈ {1,2,4,8,16}; S ∈ {256K,1M,4M,16M}; 10 seeds/cell.

- **Parameter grounding:** loss rates from immutable project netem evidence
  (0.4 % clean; 2.5–7 % severe phases); burst dynamics explicitly grounded in
  the two-state Markov literature (NTIA TM-23-565; Haßlinger–Hohlfeld MMB 2008;
  Paxson 1997; Yajnik et al. 1999) because the project contains **no**
  correlated-loss measurements — recorded in
  `parameter_grounding/parameter_grounding.md` and swept as a predeclared
  robustness range.
- **S-retry amplification: PRESENT and material in the BAD condition.** At
  S = 16 MiB the per-attempt failure rate is 32.7 %, with 10.6 retries and
  169 MiB re-downloaded per download and 48 % of downloads aborting (retry
  limit); S ≤ 4 MiB never fails. Using the GOOD-optimal S = 16 MiB under BAD
  costs **+36.3 %** completion (81.8 s vs 60.0 s).
- **N dimension: INSUFFICIENT.** Completion is monotone decreasing in N
  (≈ N per doubling) in both conditions and **N\* = 16 in all 9 predeclared
  robustness variants**; the per-request burst exposure and aggregate progress
  are N-independent under per-response pacing. This matches the live T1 result
  and the live shared-bottleneck screen.
- **Joint optimum: does NOT move** (N* = 16 in both loss states; only S* moves
  16M -> 4M). The analytical gate fails condition 1; conditions 4–8 (transition,
  causal detection, causal headroom, file-size residue, stationary control) were
  not evaluated. **No live gate, no adaptive execution, no aria2 comparison, no
  confirmatory protocol.**
- **Robustness:** the S* movement appears in 4/9 variants (primary, T_G = 10 s,
  g = 0.25, g = 0.5) and not at faster base rates (the S-retry amplification is
  rate-conditional); the N monotonicity holds in 9/9.
- **Artifacts:** `testdata/joint_ns_loss_mechanism/` (`parameter_grounding/`,
  `analytical_gate/stationary_screen.json` + `robustness_sweep.json`,
  `analysis/analytical_gate_analysis.json`, `figures/fig1..fig8`,
  `reports/joint_ns_loss_mechanism_report.md`, `manifest/`, `tools/`).
  No previous evidence was modified; no live experiment was run.
- **Per the task's final stop condition:** the mechanism search is **closed**.
  No further invented mechanism will be created without a separate
  research/design decision. The defensible competitive result remains the fixed
  **N=16, S=4 MiB D2 aria2 win (62.4 %)**, unchanged.

---

### 22.93 Track 1 fresh joint N+S D2 benchmark (executed 2026-09-29, LIVE): BEATS_ARIA2_ONLY

**Status: `JOINT_ADAPTIVE_BEATS_ARIA2_BUT_NOT_FIXED` — classification
C = `BEATS_ARIA2_ONLY`.** A fresh predeclared 20-block benchmark on the exact
frozen D2 workload compared four modes — fixed (16, 4 MiB), N-only adaptive,
S-only adaptive, joint adaptive N+S (the existing heuristic policies with
frozen bounds/steps/thresholds, initial (4, 4 MiB), one serialized decision
per completed attempt, ±10 % thresholds, 30 s timeout, 3 retries) — with the
frozen aria2 configuration as the practical reference. 80/80 runs valid,
byte-exact content verification, zero retries, 3 aria2 consistency runs
(53.27 s vs the immutable 30-pair mean 53.56 s, −0.5 %).

- **Results:** fixed mean 20.25 s / median 20.19 s; N-only 36.93 s; S-only
  64.97 s; joint 46.91 s.
- **Primary (adaptation-value) comparison:** joint vs best fixed — geometric
  ratio **2.3161** CI [2.3023, 2.3289], **0/20 wins** → the joint policy is
  2.32× slower; adaptation added no value on D2.
- **Practical comparison:** joint vs frozen aria2 (immutable 30-pair
  reference) — ratio **0.8759** CI [0.8707, 0.8801] → 12.4 % faster, but the
  fixed best beats the same reference by ~62 %. Beating aria2 alone is not
  adaptation evidence.
- **Adaptation evidence:** 20/20 joint runs changed **both** N and S (100 %);
  median final state (6, 6 MiB), i.e. away from the optimum (16, 4 MiB);
  N-only ends at (8, 4 MiB); S-only at (4, 8 MiB).
- **Mechanism:** decisions require a completed attempt, so with the frozen
  initial 4 MiB chunks at 640 KiB/s the first actionable decisions occur at
  ~8.6 s (after the pacing recovery); the windowed per-request throughput
  signal rises 607 → ~2184 KiB/s and then saturates, the ±10 % rule stops
  firing, and each policy freezes after ~4 applied actions within a ~30 ms
  window. Zero retries/timeouts. The final S increases (6-8 MiB) also raise
  each request's service time and reduce scheduling flexibility.
- **Classification:** C = `BEATS_ARIA2_ONLY` by the predeclared A–E rules;
  adaptation was demonstrated mechanically but did not produce a performance
  win over the best fixed configuration.
- **Artifacts:** `testdata/joint_ns_d2/` (manifest frozen before execution,
  80 per-run results, raw logs + validity metadata, analysis + mechanism
  JSONs, figures fig1–fig8, tools); report
  `reports/joint_ns_d2_report.md` (copy in the study's `reports/`). The
  frozen 62.4 % benchmark and all prior evidence are unchanged.
- **Track 2 (controlled N-dependent server throttling, the proposal's
  promised mechanism) was NOT started** — it requires a separate instruction;
  it remains the only identified path to a genuine joint-adaptation value
  result. The fixed 62.4 % aria2 win remains the benchmark anchor.

---

### 22.94 Track 2 N-dependent server mechanism pre-screen (executed 2026-09-29, analytical only): STOPPED

**Status: `PRE_SCREEN_FAILED_NO_QUALIFYING_SCENARIO`** (maps to
`LIVE_MECHANISM_GATE_FAILED_JOINT`; the live gate was **not reached**). The
mechanism the submitted proposal promised — configurable N-dependent server
throttling — was reproduced exactly from `python/rl/sim_env_v5.py`:
per-request failure `p_eff(k) = min(0.9, loss + congestion*(k-1))`,
per-extra-connection overhead `0.02 s*(k-1)`, fair share
`per = min(rate, link/k)`, plus the frozen downloader retry semantics (30 s
timeout, 3 same-range attempts, then the download aborts). The rejected
`R_eff = R*(1-loss_eff)` substitute was **not** used.

- **Predeclared family:** GOOD (2.5 MiB/s, loss 0.4 %, congestion 0) plus 16
  degraded candidates (rate {0.4, 0.5} MiB/s x loss {2 %, 3 %} x congestion
  {0.02, 0.04, 0.06}, plus 4 link variants); N in {1,2,4,8,16}; S = the exact
  frozen Track-1 action set; 20 seeds/cell; three predeclared workload scales
  F in {64, 128, 256} MiB.
- **Frozen selection rule:** N* and S* must move, material cross-state penalty
  >= 10 %, coordinate-wise margins >= 5 % in both states, optima reachable;
  tie-break smallest F, then max min(penalty); if none qualifies -> STOP, no
  tuning.
- **Result: no candidate qualifies at any file size.** The strongest candidate
  (rate 0.4 MiB/s, congestion 0.04, F=64 MiB) moves S* (4 -> 11 MiB) and the N
  argmin (16 -> 8) with large cross-state penalties (+134 % / +10.7 %), but the
  N* movement is **degenerate** (N=8 and N=16 tie exactly: only six requests
  exist at the degraded optimum), the S* margin is **1.0 %** (the optimum is the
  last viable point below the 30 s timeout cliff at S = per x 30 s = 12 MiB),
  and the best single fixed config (16, 4 MiB) is within 10.7 % of the degraded
  optimum (an S-only switch suffices).
- **Structural reasons:** (1) the failure-based concurrency penalty plus the
  3-attempt retry limit creates an **abort cliff** (p_eff(16)=0.62 -> 23.8 %
  per-range abort) instead of a smooth interior N optimum; (2) the failure cost
  is per-request and size-independent, so the degraded state favours fewer,
  larger requests and pushes S* to the timeout boundary; (3) at that boundary
  the request count is small, so the N optimum is a plateau. The simulator's
  analytical interior N optimum (L4 bestC=2-6) does **not** survive the live
  retry/abort semantics.
- **Not executed (frozen stop rule):** live server extension, live fixed-grid
  gate, adaptive experiment, aria2 comparison. No environment changes; no frozen
  artifact touched. Pre-screen revisions (workload scale and separation metric;
  candidate family unchanged) are recorded in `manifest/track2_manifest.json`.
- **Artifacts:** `testdata/n_dependent_server_joint_ns/` (`manifest/`,
  `analysis/prescreen.json` + revision snapshots + `track2_analysis.json`,
  `figures/fig1..fig6`, `tools/ns_sim.py`, `tools/prescreen.py`, ...); report
  `reports/n_dependent_server_joint_ns_report.md` (copy in the study's
  `reports/`).
- **Conclusion:** the promised N-dependent server throttling does not create a
  genuine state-dependent joint N+S opportunity. The defensible results remain
  the fixed **62.4 % aria2 D2 win** and the Track-1 `BEATS_ARIA2_ONLY` finding.

---

### 22.95 Exploratory combined-mechanism screen (2026-09-29, analytical, EXPLORATORY): first clean joint movement

**Status: `EXPLORATORY — soft N-dependent server throttle + pacing creates a clean state-dependent JOINT (N*,S*) movement`.** While scoping the final-month
positive result, an exploratory screen tested the combination the proposal
promised but no single prior study covered: a **soft** concurrency-dependent
server throttle (a rate cap, no hard request failures) combined with the
time-varying per-response pacing schedule.

- **Model:** `rate_eff = min(schedule.rate(start), C/(k*(1+gamma*(k-1))))`,
  k = in-flight requests at the request's server start; otherwise the frozen
  jns_sim semantics (per-response rate sampled and held, timeout/retry, 512 MiB,
  N ∈ {1,2,4,8,16}, S ∈ {256K,1M,4M,16M,32M}). Tools:
  `testdata/n_dependent_server_joint_ns/tools/combined_sim.py`,
  `tools/explore_combined.py`; results:
  `analysis/exploratory_combined_soft_cap.json`.
- **Result:** of 27 (schedule, cap) pairs, exactly one passes the strict
  Track-2 selection rule (both N* and S* move, coordinate margins >= 5 %,
  cross-state penalties >= 10 % both ways, no degeneracy):
  **T = 1 s schedule, C = 8 MiB/s, gamma = 0.05** →
  no-cap state **(16, 1 MiB) 18.42 s** vs capped state **(4, 4 MiB) 85.51 s**;
  the capped state pays **+35.8 %** if it uses the no-cap optimum, and the
  no-cap state pays **+252.9 %** if it uses the capped optimum. N margin in the
  capped state +6.4 %, S margin +5.9 %.
- **Why this is different from the failed Track-2 model:** the earlier
  pre-screen used the sim_env_v5 *failure* semantics (probabilistic request
  failure + the 3-attempt retry limit), which produced an abort cliff and a
  degenerate S*-at-timeout-boundary optimum. The **soft rate cap** has no hard
  failures, so the interior N optimum survives with clean margins. This is the
  first mechanism in the project with a genuine state-dependent joint movement.
- **Caveats:** exploratory only (the family was swept before a frozen
  pre-screen); no causal-controller check yet; no live implementation yet. The
  next steps are (1) a frozen pre-screen with a predeclared family/selection
  rule reporting all candidates, (2) the live server rate-cap extension with
  unit/concurrency tests, (3) the live fixed-grid gate, (4) the causal joint
  controller vs best fixed vs aria2. This substantially improves the odds of
  the planned "B" result (the controlled N-dependent scenario).
- **Not modified:** no frozen artifact or prior study was touched.

---

### 22.108 Review 1 gap closure (2026-10-24): online tuner, CLI probe/checksum, goodput + reliability, Review 2/3 reports (IN PROGRESS)

**Status: `ONLINE_TUNER_RUNNING`.** The user compared the work against the
Review 1 proposal (`Computer_Networks_Lab_Submission` docx, Assessment 8) and
asked to close the gaps before submitting Review 2 and Review 3 on
2026-10-24 (target 23:00 IST); physical review Thu 2026-10-29. Anything not yet
verified must be verified by Thursday morning.

- **Rationale found in the repo for Review 1 deviations:**
  - Joint RL dropped: DQN v2 fragmented ranges (32-128 KiB); v3's simulator
    rewarded larger chunks the live server did not, so v4+ learn N only with
    S = 1 MiB (§22.27-§22.28). v6 is concurrency-only.
  - No PPO: Double DQN was chosen after the LinUCB lock-in diagnosis (§22.12,
    §22.22); no recorded reason against PPO beyond scope.
  - Per-request decisions instead of a fixed interval T: the live runner
    decides per completed attempt; v6 pinned the simulator to it (§22.33).
  - FastBioDL / Falcon: the paper's related work positions them as online
    concurrency optimizers but none was run as a baseline (now added).
  - Liu et al. 2018 "Toward a smart data transfer node" (FGCS 89:10-18) IS
    real: Crossref confirms DOI 10.1016/j.future.2018.06.033. The earlier
    `reports/references.md` note calling it unresolved is wrong.
- **Code (tested):**
  - `internal/policy/online.go` `OnlineTunerPolicy`: FastBioDL/Falcon-style
    online concurrency tuner (aggregate throughput over fixed windows,
    U = T / K^N, reverse + halve step on a >tol drop, clamp at bounds, S
    untouched). Tests `online_test.go` (7, injected clock).
  - `cmd/downloader`: probe with `Range: bytes=0-0` (requires 206 and a total
    in Content-Range) when `-size` is 0; `-sha256` verification; `run()` split
    out for tests (`main_test.go`, 5 tests).
- **Metrics change (response latency):** `fetcher.Result.Latency` = time from
  sending the request until response headers arrived (0 when none arrived);
  copied to `metrics.Record.Latency`; `policy.Context.Latency` = mean over the
  recent window of attempts that got a response (`policyRunner`). Additive: no
  existing field or decision changes. Tests: `TestFetchRecordsLatency`,
  `TestFetchLatencyZeroOnConnectionFailure`, `TestPolicyRunnerLatencyIsWindowMean`,
  Context validation case. Closes the Review 1 "network monitor" latency gap.
- **Adaptation speed:** `tools/adaptation_speed.py` (from decision logs):
  causal controller cuts N 1.5 s after the 8 s cap; selector classifies at ~4 s.
- **Analyses:**
  - `tools/goodput_reliability.py` -> `testdata/split_ablation/analysis/
    goodput_reliability.json`: 230/230 ablation runs had 0 failed attempts,
    0 retries, 0 wasted bytes.
  - `tools/old_grid_reliability.py` -> `.../old_grid_reliability.json`
    (read-only over the frozen grid): under the 30 s whole-request timeout,
    11 of 50 cells had failures; capped 16 x 32 MiB re-downloaded 112 % of the
    file and aborted.
- **Online tuner experiment:** `testdata/online_tuner/` (protocol frozen in
  commit 7b438e4 before runs; amendment A1 fixed the `-policy` flag name after
  the first smoke attempt failed with a flag error, no data). 2 scenarios
  (CAP8, HEALTHY) x 4 arms x 8 blocks = 64 runs; harness `tools/
  tuner_bench.go`; analysis `tools/analyze_online_tuner.py`.
- **Reports:** `reports/review2/review2.tex` (-> `reports/
  Review2_Project_Review.pdf`) and `reports/review3/review3.tex` (figures from
  `reports/review3/make_figs.py`). Both are LaTeX, built with latexmk.
- **Immediate next step:** analyse the tuner runs; run the full section 11
  test suite (lo is busy while the tuner runs); capture the test screenshot;
  rebuild both PDFs. By Thursday: fold the ablation and tuner results into
  `reports/paper_draft.md` (abstract (iii), section 6/7, conclusion, claim
  map) and extend `tools/claims_check.py` to recompute them.

### 22.107 Study audit (2026-10-24): engine flaws behind "aria2 wins under the cap"; opt-in straggler splitting; split ablation (COMPLETE)

**Status: `SPLIT_ABLATION_COMPLETE`.** 230/230 runs valid (commit f258df7).
Results by the protocol's rules (`testdata/split_ablation/analysis/summary.json`):
  - C2 splitting at CAP0: 16 x 32 MiB with split / without = 0.599
    [0.598, 0.601].
  - C3 vs aria2: CAP0 best ours (16 x 32 MiB, split) 0.995 [0.992, 0.999]
    (level; statement replaced by the engine explanation). CAP8 best ours
    1.016 [1.006, 1.029] (aria2 still 1.6 % faster; statement kept with the
    gap quantified). HEALTHY 0.344, D2 0.370 (ours faster).
  - C4: CAP8 causal_joint_NT_split / best stationary (16 x 32 MiB split)
    1.071 [1.054, 1.085] slower; CAP0 selector / best stationary 1.229
    slower. Replication of the published claim: causal_joint_T30 /
    fixed_16_1M_T30 0.767. So once the engine is fixed, a stationary
    configuration beats the adaptive controllers in both capped scenarios.
  - C5 no harm: HEALTHY 0.981 [0.931, 1.011], D2 0.995.
  - The paper revision required by these rules is still pending (§22.108).

Earlier status: `SPLIT_ABLATION_PENDING`. Branch `exp/live-pilot-l1`. The user
asked for an audit of mistakes that keep us from beating aria2, then asked
for all flaws to be fixed by Wed 2026-10-28. The user chose opt-in splitting
over an explanation-only fix.

- **Audit findings:**
  1. **Whole-request timeout.** All capped-server runs (§22.96–§22.98) used a
     30 s deadline covering request and body. 16 MiB and 32 MiB ranges at
     N=16 mostly failed and restarted from zero. aria2's `--timeout` acts on
     stalls. The earlier timeout-sensitivity study only went up to 60 s.
  2. **No dynamic segmentation.** The server log of `p1_capped_aria2_r00`
     shows aria2 splitting the unfinished part of slow pieces from 53 s on.
     Ours had immutable in-flight ranges, so its slowest range set the
     finish time.
     - Scratchpad diagnostic (not data, disclosed in the protocol): ours
       16 × 32 MiB with no deadline and no split took 114.7/113.9 s at CAP0
       (aria2 68.1/68.4) and 56.4/54.2 s at CAP8 (aria2 53.2/53.2).
     - So at CAP8 a *stationary* configuration beat the published causal
       controller's 58.7 s. The published "+23.4 % vs best of three" holds
       only for its predeclared set, all of which had small S.
  3. **Netem RTT mislabel.** `delay 40ms` on `lo` gives an 80 ms RTT
     (measured 80.1 ms), and the loss also applies in both directions. The
     paper text said "40 ms RTT"; corrected in §4 and the abstract.
  4. **DQN head-to-head design.** The files were 4–16 MiB, and v6 started at
     C=1 while aria2 opened 16 connections at once. This is now stated in
     §9; nothing was rerun.
- **Code:**
  - `downloader.Config.SplitStragglers` and `SplitMinBytes` (default 1 MiB),
    plus `Downloader.Splits()`.
  - `fetcher.Limit` and `FetchLimited`: an in-flight end can be shrunk under
    a lock, never below the received bytes. With a nil `Limit` the path is
    unchanged.
  - `rangetracker.Split`.
  - Tests: `internal/downloader/split_test.go` (gated server; tail taken;
    head retry uses the shortened bounds; off by default),
    `internal/fetcher/limit_test.go`, and `TestSplit*`.
  - `AGENTS.md` §5 has a documented opt-in exception.
- **Split ablation:**
  - Protocol: `testdata/split_ablation/` (`protocol.md`, `manifest.json`;
    harness `tools/split_bench.go` is a copy of frozen `ns_bench.go` plus
    `-split` and `-no-request-timeout`).
  - Runner and analysis: `tools/run_split_ablation.py` and
    `tools/analyze_split_ablation.py`.
  - Size: 230 runs, CAP8/CAP0/HEALTHY/D2, 10 blocks, comparisons C1–C5.
- **Immediate next step:** commit and freeze the protocol, run the smoke,
  run the ablation (about 4.5 h, uses `lo` netem, so no tests in parallel),
  then analyse. Revise paper §6/§7, the abstract (iii), the conclusion and
  the claim map strictly by the protocol's interpretation rules.

### 22.106 Live-internet pilot L-P1 + Thursday-review materials (2026-10-18 to 2026-10-20; analysed 2026-10-24; COMPLETE)

**Status: `LIVE_PILOT_COMPLETE` (descriptive; no RL, no adaptive policy run
live).** Branch `exp/live-pilot-l1` (not merged). Protocol frozen before S1
in commit 3e643d2: `testdata/live_pilot_l1/protocol.md`, `manifest.json`,
`stage0_probe.txt`.

- **Question:** on real public mirrors over the author's real access network,
  does the best fixed (N,S) change across servers and time-of-day slots? The
  headline is the selection headroom of one global fixed cell against the
  per-condition clairvoyant best, an upper bound on what any cross-condition
  adaptive selection could add.
- **Design:**
  - Client: devcontainer, AS45820 Tata Teleservices, Katpadi IN; about
    2.4 MB/s single-stream to the CDN at Stage 0.
  - File: `linux-3.0.tar.xz` (63,798,008 B), SHA-256 from kernel.org.
  - Servers: `kcdn`, `kedge`, `riken`, `tuna` (5 of 13 candidates passed
    Stage 0; `jaist` is the reserve).
  - Arms: 8 fixed cells (N ∈ {1,4,8,16} × S ∈ {1,4} MiB) + aria2 1.37.0
    (`-x16 -s16 -k1M`, 60 s timeout) + curl; 2 reps per slot.
  - Order: seeded blocks.
  - Timeouts: watchdog-only (`NoRequestTimeout`, 900 s per-run deadline).
  - Slots (IST): S1 Sun 12:00, S1T Sun 13:15 (tuna only), S2 Sun 21:00,
    S3 Mon 08:00, S4 Mon 20:00, S5 Tue 08:00.
- **Code:**
  - `cmd/live-pilot` (runner and tests; one JSONL record per run, resumable,
    invalid runs kept)
  - `scripts/live_pilot_slots.sh` (detached slot scheduler, `@server`
    filter)
  - `tools/analyze_live_pilot.py` and `tools/test_analyze_live_pilot.py`
    (predeclared descriptive analysis)
  - **API addition:** an optional `UserAgent` field on `fetcher.Config` and
    `downloader.Config`. Empty keeps the Go default and unchanged behaviour;
    `TestFetchUserAgent` covers it.
- **Amendments:**
  - A1: the access network drops nightly at about 01:30 IST; no slot overlaps
    it.
  - A2: the `tuna` mirror returns 403 to Go's default User-Agent
    `Go-http-client/1.1` (`stage0_ua_check.txt`). All 16 S1 downloader runs
    against it failed and are kept as invalid. From S1T on, the downloader
    sends `adaptivedl-research-pilot/1.0 (live_pilot_l1)`. S1T also records
    an operator error: the first launch ran all servers; 2 `kcdn` runs are
    kept and one in-flight run was lost.
  - A3: S3 ran on a **different access network** (operator-reported). It
    was moved unchanged to `raw_other_network/S3.jsonl`, is analysed
    separately as a supplementary result, and is never pooled with the Tata
    conditions. S5 (Tue 08:00) is now **required**, as the Tata morning slot.
  - A4: an internet drop at the start of S5 (8 records, 2 valid, all
    `kcdn`; kept in `raw/S5.jsonl`, and `kcdn`/S5 is INCOMPLETE). The slot
    was re-run in full as **S5R** from 08:06 IST on Tata.
- **Data so far:**
  - S1: 80 runs, 64 valid; all invalid runs are `tuna`/403.
  - S1T: 22 of 22 valid.
  - S2: 80 of 80 valid.
  - S3 (other network): 68 of 80 valid. Its DNS and retry failures cluster
    between about 08:08 and 08:40 IST, and its peaks reach about 12 MB/s.
  - S4: 80 of 80 valid (peaks about 2.43 MB/s, the Tata ceiling).
  - S5: 8 records, 2 valid (outage, A4). S5R: 80 of 80 valid.
  - The S5R scheduler's output was sent to /dev/null by operator error, so a
    reconstructed note is in `logs/scheduler.log`.
- **Results (`analysis/summary.json`, `analysis_other_network/summary.json`;
  recomputed by `tools/claims_check.py`, `lp1.*`/`lp1x.*`):**
  - **Primary (Tata):** 350 records, 328 valid (16 `tuna`/S1 403 runs, 6 S5
    outage runs). 16 complete conditions (4 servers × S1/S1T(tuna)/S2/S4/S5R).
    - Global cell 8 × 1 MiB, headroom **3.7 %**, bootstrap 95 % [1.0, 4.8].
      The upper bound is below the predeclared 5 % line, so by the protocol
      rule "one fixed configuration was near-best", which supports but does
      not prove the thesis.
    - The global cell was within 5 % of the best in 11 of 16 conditions, and
      at most 15.3 % slower.
    - No condition is ACCESS_LINK_BOUND, because N = 1 was always more than
      5 % slower.
    - The top cell changed across slots on all 4 servers, but it always had
      N ≥ 8 except for N = 4 cells three times. On `kedge`, N = 4 was
      1.37–1.71× slower than the best, so "enough connections" is
      server-dependent.
    - aria2 over the best fixed cell: 1.00–1.26. Within-run CV: 0.11–0.29.
  - **Other network (S3, A3):** 68 of 80 valid, headroom 7.2 % [1.2, 12.2]
    over 4 conditions (global cell also 8 × 1 MiB), CV 1.16–1.77. Inconclusive,
    and reported separately.
  - **Correction:** an in-session remark (2026-10-18) that "every N ≥ 4 cell
    is close to best" was wrong for `kedge`.
- **Paper:**
  - `reports/paper_draft.md` §8a is filled with final numbers and no longer
    labeled DRAFT.
  - The §10 limitation now names the pilot.
  - New verified citations (Mahimahi, Sprout/Saturator trace provenance,
    LinUCB, DQN, Double DQN, tc-netem) are in `reports/references.bib` and
    `reports/references.md`.
  - Still open (human): the redistribution terms for the Mahimahi-bundled
    traces.
  - `tools/claims_check.py`: 144/144, 0 unverified tokens. It now has a live
    pilot block, and `paper_numbers.json`/`number_sources.json` are
    extended.
- **Thursday materials:**
  - Slide deck (15 slides, private claude.ai artifact
    [private viva slide link removed]). Slide 11 **still
    holds the pilot placeholders**; it was not republished after the
    analysis.
  - `reports/thursday/project_summary.md`
  - `reports/thursday/qa.md`
- **Tests (2026-10-24):** build, vet, gofmt (outside frozen `testdata/` and
  `.claude/`), `go test ./...`, `-p 1`, `-race -p 1`, pytest 70 + 5, the
  claims self-test and claims 144/144 all pass. `go test -count=5 -p 1
  ./...` failed once in `TestAnchorCalibration`, with a **new failure mode**:
  the warm-up replay returned `DOWNLOADER_START_FAILED`, meaning the mm-link
  child exited non-zero. That is different from the documented wall-clock
  residual. It passed in 6 of 6 isolated reruns and in a separate
  `-count=5` of the package. No Go code changed since the last green
  `-count=5`. It is treated as load-sensitivity of the replay test and is
  not investigated further.
- **Immediate next step:** human review of §8a and the amendments A1–A4.
  Decide whether to merge `exp/live-pilot-l1` into `main`, and whether the
  deck's slide 11 should be filled. Do not rank modes.

### 22.105 Finalization cycle: paper-rc1, hardened claims check, bounded model-based controller attempt (executed 2026-10-12)

**Status: manuscript/release candidate tagged (`paper-rc1`, commit d1d4afa);
model-based controller attempt STOPPED at its predeclared feasibility gate
(0/6 criteria); no live evaluation; not merged Final submission-candidate tag after the negative-result disclosure was added to
the manuscript: `paper-rc1.1` (it differs from `paper-rc1` only in
`reports/paper_draft.md` §10, `reports/model_based_attempt.md` and internal docs).** Not submission-ready: human
items below are open. Frozen evidence under `testdata/` is unchanged since the
import commit 8685eb7 (`git diff --name-status 8685eb7 HEAD -- testdata` shows
additions only).

- **Phase 1 (manuscript):** claims re-scoped (D2 fixed-config result, sweep
  28/30 pairs and 0.378-0.806 range, NGINX 0.863/0.597 with failed parity
  expectation, six-workload 0.770 [0.455, 1.304] not established, P2 0.766 vs
  aria2 slower, failed S-margin and trace gates). D2 mechanism claim downgraded
  to "partly attributable" because constant-fast pacing (S4 0.568) and NGINX N1
  (0.863) still favour ours. RL inventory has a valid/stale/non-comparable
  column; related work and `reports/references.bib` contain only entries
  verified against Crossref/arXiv/RFC Editor/aria2 manual (`reports/references.md`
  lists unresolved candidates: "Liu et al. smart DTN" (no match; likely a
  mislabel of DeLoad's first author), "elastic DTN", Mahimahi, trace dataset,
  LinUCB/DQN/netem refs). Figures regenerated from raw data
  (`reports/ieee_access/make_figures.py`).
- **Corrections found by the stronger checker:** frozen analysis scripts used a
  fixed t=2.045 for every n (and z=1.96 for the D2 analysis); paper intervals
  are now recomputed with the correct critical value (v2 [1.672, 1.847], v5
  [2.230, 2.417], v6 [1.305, 1.624], D2 [0.3726, 0.3787], long-duration 512 MiB
  [0.750, 1.102], control A [0.834, 0.890]); frozen artifacts untouched. The
  "model 95.7 s" W5 figure was wrong: model prediction 94.8 s, live (4,4M)
  95.9 s, live argmin (16,16M) 85.4 s (also true of §22.100).
- **Phase 2:** `tools/claims_check.py` recomputes 113 expected numbers plus a
  manuscript scan that fails on any number not reproduced from raw runs or
  listed in `tools/paper_constants.json`; `tools/test_claims_check.py` shows it
  fails on mutated numbers (found and fixed two loose-match weaknesses).
  Release script builds from a committed revision, runs a leak scan, writes
  SHA256SUMS and runs the checker inside the tree. Clean-checkout verification
  `reports/verification/verification_a985b00.txt`: claims 113/113, parity current,
  pytest 70, Go build/vet ok, race (replay, rl) ok, **default parallel
  `go test ./...` failed twice in internal/experiment/replay (wall-clock
  alignment tests, load sensitive); passes isolated and with `-p 1`** (documented
  in docs/REPRODUCING.md; no test weakened).
- **Phase 3 (branch `exp/model-based-ns`, not merged):** gate protocol committed
  (b22ad8b), one amendment A1 after an A0 smoke failure was diagnosed as a probe
  design flaw (size fixed => overhead collinear with size/b) plus a non-robust
  loss (35f2af5), single evidence run 560 rows: median regret 0.54 (<= 0.05),
  prediction error 0.46 (<= 0.10), total cost 1.77x (<= 1.25x), k-hat worse than
  naive k:=N, logC error 0.16 (<= 0.15), fallback 64 % (<= 30 %). The true-k
  ablation also failed. Stopped by rule; no live runs, no second controller.
  Summary: `reports/model_based_attempt.md`.
- **Human-only blockers:** authors/order/affiliations/ORCID; copyright holder
  and contributor consent for MIT; third-party trace redistribution terms and
  citation; AI-use disclosure sign-off; DOI issuance and data archive upload;
  unresolved citations (Mahimahi, trace dataset, standard RL/bandit refs);
  IEEE Access template conversion.

### 22.104 Publication-readiness pass: manuscript correction, pacing sweep, NGINX check, release (executed 2026-10-11)

**Status: manuscript corrected against raw data; two screening checks run under
frozen protocols; public release tooling added. Frozen evidence unchanged.**

- **Confirmed results (reproduced from raw runs by `tools/claims_check.py`, 113
  numbers, `tools/paper_numbers.json`):** D2 ours (16 x 4 MiB) vs aria2, 30/30,
  geo 0.3756; within-download joint controller vs best of three stationary
  configs, geo 0.766, 10/10; the same controller is slower than aria2 (geo 1.096,
  0/10).
- **Descriptive estimates:** six-workload selector vs family-best single fixed:
  point 0.770, workload-level interval [0.455, 1.304] (includes 1;
  consistent improvement NOT established); vs per-workload best fixed 0.999
  [0.969, 1.029] block-level, [0.873, 1.143] workload-level. Cross-workload P1
  gains (+47.5 % / +8.2 %) are against a baseline chosen for two workloads that
  were designed to have different optima.
- **Failed gates (reported as failed):** live S-margin 3.74 % vs predeclared
  5 % (5 of 6 checks passed); five-trace recorded-replay oracle gate LCB95
  = -0.26 % vs 10 %.
- **New screening evidence (n=5 pairs per cell, descriptive only):**
  - `testdata/d2_pacing_sweep/` (protocol committed as 0ff53da before any run;
    60/60 valid): ours/aria2 geo ratios S0 D2 0.378, S1 no-dip 0.425, S2
    long-dip 0.806, S3 partial-recovery 0.583, S4 constant-fast 0.568, S5
    constant-slow 0.998. Predeclared expectations (S0 reproduces D2; S5 parity)
    held. Ours faster in 28/30 pairs. S4 shows an advantage with no phase
    structure; its mechanism was not tested.
  - `testdata/nginx_limit_rate/` (protocol committed as b60b967 before any run;
    20/20 valid; NGINX 1.24.0 per-response `limit_rate`/`limit_rate_after`, NOT
    a shared or N-dependent cap): N1 constant 0.863 (5/5), N2 burst-then-pace
    0.597 (5/5). The predeclared N1 parity expectation was **not met**; cause
    untested.
- **Corrections made to earlier text:** paper draft abstract/section 6 (withdrawn
  "23 %" family claim; per-workload baseline labels; aria2 "beats" claim for the
  P2 controller removed); the "constant pacing removes the win" wording (false
  for constant fast pacing); D2 request counts are per-30-runs totals (128 vs 16
  requests per run); long-duration v6 table description (pooled ratio 1.805 ->
  1.062 -> 0.909 driven by D2 only); duplicate section number renumbered
  (22.99 -> 22.102).
- **Bug fixed:** the NGINX runner (`nginx_run.go`) initially left `tc netem` on
  `lo` because `os.Exit` skipped a deferred clear; found in smoke testing,
  cleared, fixed (explicit clear on every exit path) and verified on success and
  failure paths before any measured run. Qdisc clean after every run and after the
  campaign.
- **Pre-registration language:** `docs/PREREGISTRATION_NOTE.md` states that
  protocols were predeclared with recorded hashes but are not independently
  timestamped; the repository starts at one import commit; W1-W6 was amended
  twice post-hoc. Evidence records under `testdata/` were not edited.
- **Raw-trace oracle artifacts archived:** `testdata/final_oracle_analysis/`
  (copied from the original out-of-repo location so the trace gate is
  recomputable; `tau_raw_observations_path` fields still name the old path).
- **Release tooling:** `LICENSE` (MIT; copyright holder placeholder),
  `DATA_LICENSE.md` (proposed CC BY 4.0 pending ownership verification),
  `CITATION.cff` (authors placeholder), `docs/REPRODUCING.md`,
  `README.public.md`, `.gitattributes` (export-ignore for internal docs),
  `scripts/make_public_release.sh` (clean tree + SHA256SUMS).
- **Validation:** `go vet`, `go build`, `go test -count=1 ./...` (30 packages),
  `go test -race` on replay and rl, `pytest python/tests` (70),
  `gen_state_parity_v5.py --check`, offline analysis scripts re-run with no
  diff, `claims_check.py` 113/113.
- **Open (human decisions / not done):** authors, order, affiliations, ORCID,
  copyright holder, AI-use disclosure; bibliography verification; data archive
  DOI and `SHA256SUMS` upload; IEEE Access template conversion and figures; the
  model-based controller (deliberately deferred). No controller or mechanism
  search was run.

### 22.103 Research verification audit (executed 2026-10-11; no live experiments re-run)

**Status: headline results reproduce from raw data; three defects fixed.** Scope:
independent recomputation from raw run files (not via the repo's analysis
scripts), re-execution of the offline analysis scripts, a code/test pass, and a
documentation-consistency pass. No live experiment was re-run.

- **Independently recomputed from raw runs, all match the documented numbers:**
  - Track 2-B adaptive (`adaptive/`, 133 indexed runs, 0 invalid, one distinct
    output SHA-256 across all runs): P1 healthy selector/fixed geo 1.006
    (6/10), capped 0.730 (10/10), vs aria2 0.353 / 1.232; P2 joint vs best
    fixed 0.766 (10/10), joint vs N-only 0.947 (10/10), joint vs S-only 0.715,
    vs aria2 1.096 (0/10).
  - W1-W6 adaptive (`w1_w6_adaptive/`, 240 runs, 0 invalid, one output SHA-256):
    pooled selector / per-workload best fixed 0.999 [0.969, 1.029], 24/60;
    selector / family-best fixed 0.770 [0.684, 0.859], 40/60, six-cluster t-CI
    [0.455, 1.304]; vs aria2 1.660, 10/60; per-workload medians and live
    argmin configs as in §22.101; W2-W6 geometric mean vs family fixed 0.938.
  - `analyze_adaptive_b.py`, `analyze_gate_b.py`, `analyze_track2.py`,
    `robustness_w1_w6.py` re-run: committed analysis JSONs unchanged.
  - aria2 mechanism: per-request server logs of a capped aria2 run show the
    first nine pieces start at k <= 9 where the cap rate (>= 665 KB/s) exceeds
    the 640 KiB/s base rate and then hold 640 KiB/s for ~52 s, as documented.
  - aria2 fairness probe (local, unthrottled, 64 MiB): `-x16 -s16/64/256 -k1M`
    produce the same 1/2/4 MiB range-size mix, so split > 16 does not give
    aria2 finer pieces; the aria2-F frontier is not under-tuned on that axis.
  - `ns_bench` binary SHA-256 equals the manifest value; the unit tests in
    `tools/band_policy_test.go` pass. A fresh rebuild is not bit-identical
    (build path/VCS stamping), so the binary-to-source link rests on the
    manifest hash and mtimes only.
- **Defects found and fixed:**
  1. `reports/paper_draft.md` §6 still claimed the selector "beats any single
     fixed configuration ... by 23 %" (withdrawn in §22.101) and labelled
     per-workload ratios that are against the per-workload best fixed as
     "family single fixed" (self-contradictory: "beats ... on W1-W4 ... loses
     W3"). Replaced by a table with both baselines per workload and the
     non-robustness caveat.
  2. Section-number collision: a second `### 22.99` (v5 simulator audit) is now
     §22.102.
  3. `internal/experiment/replay` `TestAnchorCalibration` failed
     deterministically under a parallel `go test ./...` because the first
     replay in the process paid a 450-700 ms cold-start offset (passes alone).
     A discarded warm-up replay was added; tolerances and assertions are
     unchanged. Three consecutive full `go test -count=1 ./...` runs and
     `-race` on `replay` and `rl` pass.
- **Remaining limits (not fixed; stated, not hidden):**
  - P2's "best fixed" is the best of three fixed configurations run under the
    P2 schedule ((16,1M), (4,1M), (4,4M)); there is no fixed grid under the
    P2 (cap-at-8 s) schedule, so "best fixed" there is "best of those tried".
  - W2, W3, W4 share one target by construction (same classifier level), so
    they are not three independent confirmations of selection.
  - The soft cap fixes a request's rate at request start (server model);
    aria2's "cap escape" is a property of that server model, as §22.98 states.
  - Pooled CIs treat blocks as independent; the cluster-level interval is the
    appropriate one for any cross-workload claim.
  - The pre-live W1-W6 manifest amendments (§22.100 amendment log) are
    post-hoc design refinements, not prospective pre-registration.

### 22.102 v5 simulator correctness audit follow-up (executed 2026-10-11; simulator only)

**Status: audit complete; one accounting bug fixed; the exported Candidate v5
artifact is now STALE with respect to the corrected simulator.** No training,
no new candidate, no live run, no aria2 comparison. Frozen candidates v1-v5,
the aria2-F baseline, benchmarks and `final_evaluation` are byte-identical
(`git diff HEAD -- testdata` shows no tracked change; all `.sha256` files match).

- **Bug found/fixed (`python/rl/sim_env_v5.py`):** a failed range was logged as
  ONE window sample (failure=1, retry=1, completed=0). The live runner
  (`policy_runner.observe`) records two attempts: the failed attempt
  (failure) and the successful retry attempt (`Attempt>1` -> retry, success ->
  completed). The simulator now pushes both samples, so `window_attempts`,
  `recent_failure_rate`, `recent_retry_rate` and `recent_completed_norm` follow
  the live definition. Bytes (credited exactly once), elapsed time (includes one
  retransmission), reward, RNG draw order and the static sweep are unchanged.
- **Consequence:** `final_dqn_candidate_v5.json` was trained before this fix on
  the old window semantic. It is not modified; it must not be described as
  trained on the corrected state distribution. Any further use of the v5
  simulator for learning requires retraining (not done here).
- **Semantics verified by tests** (`python/tests/test_sim_env_v5_audit.py`, 22
  parametrized cases + the existing file): fixed 1 MiB chunk and partial final
  range; concurrency in [1,4]; invalid action raises with no state mutation;
  boundary actions are no-ops; a step is exactly `decision_interval` requests
  except the final one; C=4 runs 4 requests in parallel; reward == -elapsed and
  sum(step elapsed) == episode elapsed; wall-clock (not request-count) phase
  transitions, and the transition request index depends on per-request timing;
  failed range -> bytes once + retransmission time + per-attempt windows;
  terminal iff all bytes done, truncation only from `max_episode_steps`;
  seeded determinism including `randomize()`; stationary episodes never
  transition; >=50 % of multi-phase episodes transition.
- **Python<->Go parity:** `python/gen_state_parity_v5.py` writes
  `testdata/diagnostics/dqn_validation_v5/state_parity_v5.json` (6 observations
  incl. clamping/unknown-total edge cases, tolerance 1e-12, spec + feature order
  pinned). `TestV5StateParityFixture` (Go) recomputes every vector with
  `ObservationV5.FeaturesV5`; `test_parity_fixture_is_current` fails if the
  fixture is stale. Result: all 13 features match on all cases.
- **Static sweep:** recomputed by `python/audit_simulator_v5.py` and identical to
  the existing `static_concurrency_sweep_v5.*` (not overwritten): best fixed C
  takes all of {1,2,3,4} across regimes. Single-mechanism attribution (16 MiB,
  rate 4 MiB/s): no shared cap -> C=4; shared cap = 1x/2x/3x rate -> C=1/2/3;
  loss/congestion alone never lowers best C (needs a shared cap); latency
  keeps C=4. Modelled mechanics only; dedicated real-server C=1..4 measurements
  are UNAVAILABLE.
- **Calibration:** 1 MiB @ 512 KiB/s, C=1: simulator 2.000 s vs real-server
  reference ~2.053 s (gap 52.9 ms, 2.6 %, fixed per-request HTTP overhead not
  modelled). Recorded, not fitted; full fidelity is not claimed.
- **Artifacts:** `simulator_correctness_report.{json,txt}` (new),
  `state_parity_v5.json` (new). Reproduce: `python3 python/audit_simulator_v5.py`.
- **Validation:** `go vet`, `go build`, `go test -race ./...` pass;
  `go test ./...` passed except a load-dependent failure in
  `internal/experiment/replay` `TestAnchorCalibration` on the first replay of a
  process (fixed in §22.103). `pytest python/tests`: 70 passed. `gofmt -l` lists only three pre-existing `testdata/**/tools/*.go`
  files; `black --check python` fails on 17 pre-existing files (HEAD already
  fails, including `sim_env_v5.py`), left untouched for scope.
- **READY TO TRAIN (simulator correctness gate): yes** - correctness gate only;
  not a training run or candidate. Immediate next step: decide whether to
  retrain v5 under the corrected windows before any further v5 use.

### 22.101 W1-W6 adaptive comparisons (executed 2026-10-09, LIVE): selector matches per-workload clairvoyant on average; family-single-config comparison not robust across workloads

**Status: `SIX_WORKLOAD_SELECTION_DEMONSTRATED`.** 10 fresh paired blocks per
workload (240 runs, 0 invalid; netem 40 ms + 0.4 %, content-verified). Frozen
protocol (`manifest/w1_w6_manifest.json`): band selector (probe (16,1 MiB),
25 s post-drop window, nearest frozen level) vs per-workload best fixed (live
argmin) vs family-best single fixed (live (4,4 MiB)) vs aria2. Report
`reports/w1_w6_report.md` (root + study copy); figures `figW1_W6_...`
(see artifacts).

- **Pooled paired geometric ratios (95 % bootstrap, 60 workload-blocks):**
  - selector vs per-workload best fixed: **0.999 [0.969, 1.029]**, 24/60 wins
    — no detectable difference on average (the pooled CI includes 1; this is not
    an equivalence test). Robustness (`analysis/w1_w6_robustness.json`): stratified
    bootstrap [0.996, 1.002] (block noise only); six-cluster t-CI [0.873, 1.143]
    (workloads as the unit);
  - selector vs family-best single fixed: **0.770** pooled; stratified bootstrap
    [0.768, 0.772]; **six-cluster t-CI [0.455, 1.304]**. The pooled figure is
    dominated by W1 (geo 0.286: a healthy workload vs a degraded-state fixed
    config). Over W2-W6 the geometric mean is about 0.94, and the selector loses
    to the family fixed on W5 (1.058) and W6 (1.090). The "23 % faster" phrasing
    is withdrawn.
  - selector vs aria2: 1.660 [1.363, 1.988], 10/60 — aria2 wins every degraded
    workload via its burst-start cap escape; the selector wins the healthy
    state 2.8x.
- **Per workload (median of 10):** W1 18.4 vs 18.4 / 65.3 / 53.2; W2 152.3 vs
  162.8 / 175.1 / 53.2; W3 166.2 vs 163.0 / 175.0 / 53.4; W4 131.1 vs 162.1 /
  171.6 / 53.4 (probe-phase timing win, +19.2 % vs per-workload); W5 101.0 vs
  85.9 / 95.3 / 53.4 (-17.2 %, frozen model-target mismatch); W6 75.1 vs 68.8
  / 68.6 / 53.3 (-8.8 %).
- **W5 mismatch (disclosed, not retuned):** the live grid found W5's optimum at
  (16,16M) 85.4 s vs the model's (4,4M) 95.7 s (retry/k feedback raises rates
  for retried ranges); the selector's target stayed at the frozen model value
  per the stop rule (no tuning after live outcomes).
- **Claim discipline:** this is a **(B)** workload-level selection result over
  **six predeclared controlled workloads**, not within-download adaptation
  **(C)** and not the **(A)** D2 aria2 result; no Internet-wide generalization
  claim. The W1 continuous-fixed results were run under the same frozen
  protocol and count as one of the six.

### 22.100 W1-W6 six-workload family: analytical design + manifest freeze (executed 2026-10-08, analytical; pre-live)

**Status: `W1_W6_FAMILY_FROZEN`** (before any live W1-W6 run). Per the
remaining-month plan (mechanism-dependent-granularity thesis), the six-workload
expansion was designed analytically from the calibrated live-equivalent model
only (`tools/combined_sim.py`, Params h=0.088, timeout 30 s, 3 attempts); no
live W3-W6 outcome existed or was consulted. Frozen artifacts:
`manifest/w1_w6_manifest.json` + `analysis/w1_w6_design.json`
(`tools/design_w1_w6.py`).

- **Candidate family:** T in {1,2,4} x C in {4,8,12,16} MiB/s x gamma in
  {0.05,0.10} x cap activation t in {4,8,12} s (72 candidates) + W1 healthy.
- **Qualification (predeclared):** B (state-schedule optimum) vs A (T-matched
  no-cap optimum): N and S both move; B reachable; cap-side gain >= 20 %;
  >= 10 % right-config gain; detectable (model post-transition probe level
  < 0.40 x peak probe level — selector-identifiability precondition).
  **13/72 qualify**, all with t in {4,8}; no t=12 candidate qualifies, so the
  transition-coverage constraint is relaxed to >= 2 distinct transitions
  (documented in the manifest).
- **Selection (predeclared):** maximize sum(min(cap_gain, base_pen)) over
  5-subsets with coverage and lexicographic tie-breaks; selected **W2
  (T=1,C=4,g=.10,t=4), W3 (T=2,C=4,g=.10,t=4), W4 (T=2,C=4,g=.10,t=8),
  W5 (T=2,C=8,g=.10,t=4), W6 (T=2,C=12,g=.10,t=4)**. Model optima: W2-W4
  **(2,4 MiB)** (~160 s), W5-W6 **(4,4 MiB)** (94.8 s / 67.9 s); base-side
  penalties 231-579 % (the candidate optima are terrible in the healthy
  state — a genuine two-way trade-off).
- **Selector (predeclared):** `band` policy, probe (16,1 MiB), running-max
  drop detector (0.5x, window 3), fixed **25 s** post-drop classification
  window (>= 1 sample), nearest predicted level, deadline 60 s -> healthy
  (16,1 MiB). Levels 104857 x3 (W2-W4 share the class and the target) /
  209715 (W5) / 314572 (W6) B/s; targets (2,4 MiB) / (4,4 MiB). Pre-live
  review note: a sample-count window (20 samples) was rejected before any
  live W1-W6 run because post-drop request durations reach ~10 s under the
  strongest selected cap, making the window exceed the download duration.
- **Live protocol (frozen):** per workload fixed grid {4,8,16} x {1,4,16} MiB
  + predicted optimum, 2 reps, then 10 paired blocks of
  band_selector | per-workload best fixed (live argmin) | family best single
  fixed (live) | aria2; netem 40 ms + 0.4 %; invalid runs preserved.
- **Live fixed-grid validation (complete, 2026-10-08):** 114 runs (2 reps/cell,
  netem 40 ms + 0.4 %, timeout 30 s), invalid runs preserved. Live argmins vs
  the frozen model predictions: **W1 (16,1M) 18.5 s match; W2 (2,4M) 163.2 s
  match; W3 (2,4M) 163.0 s match; W4 (2,4M) 162.3 s match; W6 (4,4M) 68.6 s
  match; W5 mismatch** — the live optimum is **(16,16M) 85.4 s**, not the
  model's (4,4M) 95.7 s. Mechanism of the W5 mismatch: under C=8/gamma=0.10
  the first 16 x 16 MiB wave samples its rate pre-cap and completes; failed
  later ranges time out (30 s) and the retry lowers k, which raises the
  cap rate for retries, so (16,16M) completes (32 successes + 8 partial
  failures) at 85.4 s — a retry/k feedback the analytical model does not
  capture. The frozen selector (model-derived target (4,4M) for W5) is
  **not** retuned after this observation (stop rule); W5's honest expected
  penalty vs the clairvoyant live argmin is ~10 %. Family-best single fixed
  (live, common cells): **(4,4M), mean 125.2 s**.
- **Amendment log (added 2026-10-11 audit):** the manifest was amended twice
  after the first design run and before any live W1-W6 run, both after seeing
  design output: (a) the post-drop classification window changed from 20 samples
  to a fixed 25 s window (>= 1 sample), because 20 samples would exceed the
  download duration under the W2-W4 cap; (b) the transition-coverage constraint
  was relaxed from {4,8,12} to >= 2 distinct transitions, because no t=12
  candidate qualified. The first manifest was overwritten and is not preserved.
  These are post-hoc design refinements, not prospective pre-registration. The
  W2-W4 selector levels (104857 x3) are identical, so the three share one target
  by construction.
- **Adaptive comparisons (running at write time):** 10 fresh paired blocks per
  workload of band_selector | per-workload best fixed (live argmin) | family
  best single fixed (live) | aria2, frozen protocol; results land in
  `analysis/w1_w6_adaptive.json` and `reports/w1_w6_report.md`. Frozen
  D2/aria2 evidence and all prior screens untouched.

### 22.99 P1 baseline correction + P2 S-only ablation (executed 2026-10-08, LIVE)

**Status: complete.** Plan-phase-1 items, criteria unchanged.

- **P1 baselines (no data bug; terminology fix):** `analysis/adaptive_b.json`
  now reports both baselines explicitly from the raw gate/runs:
  - best **single** fixed configuration across the two predeclared controlled
    workloads = (8, 4 MiB), 36.3 s healthy / 91.7 s capped; the selector gains
    **+47.5 % / +8.2 %** (mean +27.9 %, worst +8.2 %);
  - per-workload best fixed (healthy (16,1M) 18.4 s; capped (4,4M) 86.4 s):
    selector **-3.5 %** in the healthy state (policy-path overhead; the
    in-block paired 19.1 vs 19.0 s is indistinguishable) and **+2.6 %** in the
    capped state.
  `reports/track2b_soft_cap_report.md` and `reports/paper_draft.md` reworded;
  P1 remains explicitly a **two predeclared controlled workloads** result.
- **P2 S-only ablation (10 fresh blocks):** `causal_s_only` (target
  (16,4 MiB); S moves, N stays) = **82.1 s** — worse than fixed (16,1M) 76.7 s
  (geo 1.070 vs per-rep best fixed, 0/10) and far worse than joint (58.7 s;
  joint vs S-only geo 0.715, 10/10). N-only 62.0 s; **joint vs N-only geo
  0.947 CI [0.943, 0.951], 10/10 wins** (smaller but consistent S-side gain).
  The N move carries the gain under this cap; S alone actively hurts.
- **Band selector policy added (pre-live):** `tools/ns_bench.go` mode `band`
  + `tools/band_policy_test.go` (nearest-level, tie-break, parsing), built and
  frozen (sha256 recorded in the W1-W6 manifest). `tools/design_w1_w6.py`,
  `tools/run_w1_w6_grid.py`, `tools/run_w1_w6_adaptive.py` added.
- **Report/analysis updates:** `tools/analyze_adaptive_b.py` +
  `tools/make_report_b.py` now emit the S-only row, the paired joint-vs-N-only
  and joint-vs-S-only comparisons, the two P1 baselines, and a dynamic run
  count.
- **Timeout sensitivity (complete):** across the capped grid {N=4,8,16} x
  {S=1,4,16 MiB} at 20/30/60 s per-request timeout (2 reps/cell), the argmin is
  **(4, 4 MiB) at every timeout** (86.5 s / 86.4 s / 86.5 s). The timeout changes
  only *reachability*: invalid cells 2 (20 s) / 1 (30 s) / 0 (60 s) — the
  high-N large-S aborts are a timeout artifact of the frozen retry limit, not a
  change in the mechanism's optimum. Full grid + abort reasons in
  `analysis/timeout_sensitivity.json`, raw runs in `timeout_sensitivity/`.
- **Capped S-row precision replication (complete):** 4 extra reps at N=4 pooled
  with the gate runs -> S=4 MiB 86.4 s (n=7), S=1 MiB 89.7 s (n=7), S=16 MiB
  97.0 s (n=5); local S margin **3.74 %** (95 % bootstrap CI [3.09 %, 4.45 %]).
  The preregistered 5 % bar is unchanged and still missed; the S* movement is
  statistically solid but carries the documented local-optimum robustness
  caveat (`analysis/precision_s_row.json`).
- **Band-selector smoke (pre-live, implementation check only):** one live W2
  run (T=1, C=4 MiB/s, gamma=0.10, t=4) with the frozen band parameters:
  drop detected and classified at 31.6 s (level 103.6 KiB/s -> target (2,4 MiB)),
  completion **155.2 s** (model 160.5 s; ~305 s without switching),
  content-verified, no retries. No frozen parameter was changed after this
  smoke (`smoke/smoke_w2_band.json`).

### 22.98 Track 2-B adaptive results (executed 2026-10-08, LIVE): joint adaptation demonstrated

**Status: `JOINT_ADAPTATION_DEMONSTRATED_UNDER_CONTROLLED_SERVER_DEGRADATION`.** Two
predeclared adaptive experiments were run on the live soft-cap platform (10
blocks each, tc/netem 40 ms + 0.4 %, content-verified; report
`reports/track2b_soft_cap_report.md`).

- **P1 — cross-workload joint (N,S) selection.** The causal probe selector
  classifies the workload after >= 20 samples and picks the predeclared (N,S):
  - healthy: selector 19.1 s vs fixed (16,1M) 19.0 s (geo 1.006, CI
    [0.939, 1.080], 6/10) vs aria2 53.9 s (geo 0.353, 10/10) — matches the best
    fixed and beats aria2 **2.8x**;
  - capped: selector 84.2 s vs fixed (16,1M) 115.3 s (geo 0.730, CI
    [0.728, 0.732], **10/10 wins**) vs aria2 68.3 s (geo 1.232, 0/10);
  - cross-workload: the best **single** fixed configuration (N=8, S=4 MiB)
    loses to the selector on **both** workloads — gains +47.5 % (healthy) and
    +8.2 % (capped), mean **+27.9 %**, worst **+8.2 %**.
- **P2 — within-download joint adaptation under degradation** (no cap [0,8 s),
  cap on [8 s, EOF)): the causal drop controller switches once to (4, 4 MiB):
  - causal joint **58.7 s** vs best fixed (16,1M) 76.7 s — geo **0.766** CI
    [0.763, 0.769], **10/10 wins** (+23.4 %); N-only control (target (4,1M))
    62.0 s (geo 0.808, 10/10) — so the win is genuinely joint (both N and S
    change and the joint target is the better one);
  - aria2 53.6 s (geo 1.096, 0/10): aria2 wins in this scenario.
- **aria2 mechanism contrast (documented honestly):** under the cap, aria2's
  16 x 32 MiB pieces all start at ~0.4 s while k is still low — the first nine
  get 640 KiB/s and hold it, so they **escape the cap**; ours re-allocates
  continuously and is capped again each time, and large ranges time out. This
  is the inverse of the D2 mechanism (where aria2's burst-start was the
  weakness). Ours beats aria2 by 62.4 % under D2 and 2.8x under the healthy
  schedule; aria2 wins under the cap.
- **Gate caveat (corrected):** the live gate **failed** its predeclared S-margin
  check (3.8 % < 5 %; `gate_b.json` `pass: false`); N/S movement and cross-state
  materiality passed (+31.8 % / +254.4 %). See §22.97.
- **Artifacts:** `testdata/n_dependent_server_joint_ns/` (manifest/b2_design.json,
  analysis/prescreen_b.json + gate_b.json + adaptive_b.json, fixed_grid/,
  adaptive/ 122 runs, figures/figB1..figB4, tools/); server extension + tests in
  `internal/experiment/server/`. The frozen D2/aria2 benchmark and all prior
  screens are unchanged.

### 22.97 Track 2-B live fixed-grid gate (executed 2026-10-07, LIVE): FAILED ONE PREDECLARED CHECK (S-margin)

**Status: `LIVE_GATE_FAILED` (`analysis/gate_b.json`: `"pass": false`; 5 of 6 checks pass).** The live fixed grid (both states,
full 5x5 (N,S) grid, 1 rep/cell + 2 extra reps on the top-3 cells per state,
tc/netem 40 ms + 0.4 % on lo to match the calibrated per-request overhead;
62 cells, 3 invalid high-N/large-S cells that abort under the cap — preserved
with reasons).

- **Healthy optimum (live): (N=16, S=1 MiB) 18.4 s**, N margin +48.3 %,
  S margin +8.4 % (matches the analytical 18.42 s).
- **Capped optimum (live): (N=4, S=4 MiB) 86.4 s**, N margin +5.8 %,
  S margin +3.8 % (the capped S curve peaks clearly at 4 MiB: 4M 86.4 s vs
  1M 89.8, 16M 96.8, 256K 104.5, 32M 109.7; the 3.4 s difference is ~7x the
  per-cell rep spread).
- **Gate checks:** N moves PASS, S moves PASS (1 MiB -> 4 MiB), N margin
  >= 5 % PASS, S margin >= 5 % **FAIL (3.8 %)**, cross-state materiality PASS
  (+31.8 % / +254.4 %). Overall: **FAILED** — the predeclared gate requires all
  checks; the S-margin check fails (3.8 % < 5 %). Correction (2026-10-11 audit):
  earlier text described this as "passed with caveat"; that was incorrect. The
  Track 2-B adaptive runs (§22.98-§22.99) were executed on a platform whose
  predeclared gate failed one check; they are exploratory with respect to the
  S-side mechanism, not confirmatory.
- **Platform note:** without netem the live S landscape is flat (the
  per-request RTT overhead is absent); the no-netem grid is preserved under
  `fixed_grid/invalid_no_netem/` as the documented model-live mismatch. All
  adaptive runs include the same netem.
- **Adaptive experiment running** (`tools/run_adaptive_b.py`): P1
  cross-workload selection (selector | fixed | aria2 | curl | wget) and P2
  within-download adaptation (causal joint | causal N-only | fixed variants |
  aria2), 10 blocks each, netem matched.

### 22.96 Track 2-B soft N-dependent server throttle: live implementation + smoke (executed 2026-09-29, LIVE)

**Status: `LIVE_MECHANISM_IMPLEMENTED_AND_SMOKE_VALIDATED`; the live fixed-grid
gate is running.** Following the exploratory finding (§22.95), the mechanism was
implemented in the controlled server and validated live.

- **Server extension** (`internal/experiment/server/server.go`): a soft
  concurrency cap — `SetConcurrencyCap(C, gamma)` makes a request starting
  while k requests are active (including itself) paced at
  `min(base rate, C/(k*(1+gamma*(k-1))))`, sampled at request start and held.
  It is a rate throttle only: it never changes status codes, failures, or retry
  behavior. Active-request tracking (`ActiveRequests()`), request-log fields
  (`ActiveK`, `BaseRate`, `CapRate`), default off (the D2 behaviour is
  unchanged). Tests: `internal/experiment/server/concurrency_cap_test.go`
  (rate formula, validation, default-off, a 4-way concurrency test verifying
  the observed k and rates, and the failure path).
- **Frozen design** (`manifest/b2_design.json`): the predeclared family (5
  schedules x 17 cap settings x the 5x5 grid) was screened; 5 of 80 pairs
  qualify under the strict rule; the selected pair is the T=1 s schedule with
  C=8 MiB/s, gamma=0.05 (the cap binds at the degraded optimum, so both
  transitions are detectable). Stationary optima: healthy (16, 1 MiB) 18.42 s
  vs capped (4, 4 MiB) 85.51 s; cross penalties +35.8 % / +252.9 %.
- **Positive results to measure live:** P1 = cross-workload joint selection
  (probe classifier picks (16,1 MiB) or (4,4 MiB); analytical gain ~23.6 % mean
  over the best single fixed); P2 = within-download adaptation under a cap
  transition at 8 s (analytical: joint target +11.5 %, N-only target +15.4 %
  over the best fixed; the measured post-switch optimum is (4, 1 MiB), so the
  within-download win is concurrency-driven — reported honestly).
- **Live smoke (content-verified, 0 retries):** healthy fixed (16,1M) 15.8 s;
  capped fixed (16,1M) 113.0 s; capped fixed (4,4M) 84.5 s; classifier healthy
  15.8 s final (16,1M); classifier capped 81.8 s final (4,4M); drop controller
  (cap at 8 s) 51.8 s final (4,1M). Live matches the analytical model within a
  few percent.
- **Running:** the live fixed-grid gate (both states, full 5x5 grid, 1 rep per
  cell + 2 extra reps on the top-3 cells per state; driver
  `tools/run_gate_b.py`, analysis `tools/analyze_gate_b.py`).
- **Next:** gate verdict; the adaptive experiment (`tools/run_adaptive_b.py`,
  20 blocks of P1 + P2 with aria2/curl/wget baselines); the paper.
- **Not modified:** Track 1, the aria2 competitive benchmark, and all prior
  screens are untouched. The only source change is the additive server cap.


---

## 23. Test coverage (by subsystem)

Test files exist for every package. Highlights (behavioral, not exhaustive):

- **RangeTracker:** sequential/variable-chunk allocation, final partial chunk,
  completion, requeue/merging, duplicate-allocation prevention, concurrency.
- **Fetcher:** success + metadata, `Range` header, 206/`Content-Range`
  validation, 200 rejection, incomplete/overlong bodies, server error,
  connection failure, timeout/cancellation, invalid range; dedicated
  HTTP/1.1-only transport (real plaintext and h2-enabled-TLS requests),
  Cmax=16 idle-pool configuration, `http.DefaultTransport` untouched,
  caller-supplied client passed through unchanged; single-connection reuse
  across sequential ranges verified via client `httptrace` plus server-side
  `ConnContext` identity (no dials beyond the first, all responses HTTP/1.1);
  incremental timestamped received-byte accounting (optional
  `ByteObserver`/`ByteMeter`: read-time attribution, per-connection identity,
  partial-read/error preservation, multi-interval, failed-response and
  concurrent-request coverage).
- **Assembler:** single/in-order/out-of-order writes, first/middle/final,
  invalid range, length mismatch, exact reconstruction, size, close semantics.
- **Downloader:** fixed/adaptive concurrency, out-of-order completion, retry
  then success, retry-limit error, context cancellation, metrics per mode,
  serialized decisions, chunk-size integration (fixed/dynamic/bounds/final
  partial), in-flight immutability, retry-boundary preservation; empirical
  C→flow mapping for C∈{1,2,4,8,16} against a deterministic barrier server
  (server-side connection identity: max in-flight requests and max active TCP
  connections equal C, exactly C total connections created, one request per
  connection at a time, reuse without idle-pool churn); optional
  `ByteObserver` wiring verified under gated C=4 concurrency (aggregate and
  per-request totals cross-checked against attempt records, cumulative
  monotonicity, distinct connection identities, output byte-match).
- **Metrics/Concurrency/Chunksize:** per-attempt records and aggregates;
  controller bounds/clamping/set/min==max/concurrency safety.
- **Policies:** each policy's decision rules, thresholds, zero-throughput,
  bounds, alternation/fallback (JointPolicy), interface implementation.
- **Experiment:** four-mode expansion/equivalence/fresh-state, aggregates and
  variability, JSON/text, network apply/clear and unavailable exclusion,
  matrix scenarios; LinUCB opt-in mode construction/validation/completion, the
  default `AllModes()` excluding LinUCB, and `MatrixConfig.Modes` selection;
  LinUCB calibration diagnostics (action distribution, reward stats, component
  aggregation, churn, reward reconstruction cross-check, determinism) and the
  `cmd/calibration` CLI (`distributionString`, JSON round-trip, report);
  `cmd/experiment` matrix file-size override (`applyFileSizeOverride` preserves
  the default and does not mutate `DefaultMatrixConfig`).
- **Experiment server / network:** range responses, invalid ranges, delay/rate/
  runtime rate change, deterministic failures, stats; logical bounded-memory
  workload (`NewLogical`, `DefaultLogicalSize`): deterministic offset-addressed
  content, exact ranges/headers, invalid-range 416s, bounded-memory construction
  and 64 MiB range streaming, offset stability, finite-server compatibility;
  tc command construction, error classification, interface detection, gated
  privileged integration test.
- **Mahimahi replay harness (§22.59):** trace parsing/validation; invalid
  configuration; mm-link start failure; downloader start failure; watchdog
  timeout with no leftover processes; local child download against the logical
  server (byte-exact); synthetic C={1,4,16} mm-link smoke with host-network
  before/after comparison; workload exhaustion mapping; distinct outcome exit
  codes. **Evidence-based EOF (§22.63):** endpoint residual arithmetic
  (exact/inside/boundary/outside/observed-beyond/missing evidence); synthetic
  CASES A–F — A real EOF + clean child → EOF; B early clean child exit +
  mm-link rc=0 → NOT EOF; C fatal child + mm-link rc=0 → NOT EOF; D missing
  endpoint evidence → NOT EOF; E timeout-then-retry-success → child success;
  F timeout + retry exhaustion → non-zero child and `DOWNLOADER_FATAL_ERROR`
  (NOT EOF); missing summary → NOT EOF (never EOF); child statuses
  (clean/fatal/canceled) and child exit contract (clean → no error, fatal
  retry-limit → non-nil error with persisted `fatal`+`err`, cancellation → not
  fatal). The real mm-link smoke and anchor-calibration tests also assert
  endpoint evidence.
- **Trace-pair resolution (§22.63):** `ResolveTracePair` accepts a bare stem,
  `.up`, or `.down` path and returns both members; missing `.up`/`.down`,
  cross-directory pairs, mismatched families, and missing explicit members are
  rejected; `Plan` carries the resolved `UpPath`/`DownPath` per run.
- **Watchdog-only timeout policy (§22.68):** explicit no-request/no-dial
  deadline configuration and ambiguity validation (`fetcher`), mirrored
  `downloader` policy fields, `replay.ParseDeadline`/`DeadlineString`,
  `cmd/epoch-pilot` and `cmd/mm-replay` `-timeout`/`-dial-timeout` parsing
  (`none` vs positive duration), versioned protocol revision
  (`epoch.DefaultProtocol` v2, `LegacyProtocolV1`,
  `ValidateTimeoutPolicyRevision`), no-client-local-deadline behavior, blocked
  request canceled by the parent watchdog (`WATCHDOG_TIMEOUT`, non-EOF, clean
  shutdown), canceled child before EOF (not EOF), external cancellation
  (`CANCELED`, not EOF), genuine-failure retry retained, and
  cancellation-not-retried.
- **Trace anchor (§22.60):** trace-time arithmetic; launch-based anchor
  independent of child start; first-byte-as-measurement; EOF represented
  separately from child completion; monotonic arithmetic; calibration
  statistics; tolerance boundary; raw/derived reproducibility; mm-link
  trace-log parsing; repeated C={1,4,16} synthetic calibration with two known
  gap edges and downlink-log cross-checks.
- **Epoch construction / pilot accounting (§22.61):** exact trace-time
  boundaries incl. partial tail; read-time cross-epoch attribution; 5 MiB
  byte-floor boundary; 0.80 observed-fraction boundary; observed-duration R
  divisor; shared cross-C exclusion; trace-time anchoring; determinism;
  exclusion accounting and shortest-passing candidate selection; revision
  rules; frozen native-trace corpus scan and good/degraded classification.
- **Epoch pilot driver (§22.61):** 10-run plan generation; unique run
  directories and resume semantics; failure-record preservation; streaming
  JSONL event accumulation into all four candidate durations in one pass;
  post-hoc multi-duration evaluation; shared validity and exclusion
  accounting; shortest-passing selection; missing-anchor non-fabrication;
  dry-run configuration/plan output (verified manually with `-dry-run`).
- **RL:** state conversion/validation/features, action mapping/round-trip,
  reward validation, adapter conversion, `AlwaysActionPolicy`, real-downloader
  integration for increase-chunk / increase-concurrency / none; LinUCB
  (config validation, initial model, deterministic/lowest-index selection,
  disjoint action isolation, reward-sign effects, hand-computable d=1 update,
  exploration math, malformed-input fallback, adapter compatibility, and a
  package-local check that `Update` changes the selected arm's model while
  leaving others untouched); reward computation (config validation,
  first-observation handling, throughput/latency/retry/failure/action
   components, aggregate-throughput source, hand-computable formula,
   determinism, edge cases); the new **aggregate-throughput observable**
   (runner per-request-vs-overlapping discrimination with a fake clock, feature
   normalization/order, and reward-uses-aggregate); runner-level
   **credit-assignment timing** (Update receives the previous context/action;
   Update count = SelectAction count − 1); and end-to-end **causal ordering and
   reward association** through the real Adapter + downloader
   (`internal/rl/causal_integration_test.go`) plus a first-observation no-update
   test.
- **Diagnostics:** `internal/diagnostics` (forced-action correctness for
  none / increase-concurrency / increase-chunk-size, report shape, and the
  before/after exploration-correction coverage test) and a white-box
  `internal/rl` diagnostic test that instruments the real LinUCB score/update to
  document the initialization tie and greedy lock-in.
- **Exploration correction (§22.8):** `internal/rl/linucb_exploration_correction_test.go`
  (per-action coverage, multiple rounds, determinism, transition to normal
  selection, no-op effectiveness bookkeeping, invalid/mismatched state,
  unchanged `A`/`b` update, default-disabled).
- **Learning-horizon study (§22.10):** `internal/diagnostics/horizon_test.go`
  (warm-up is 5, larger file yields more total and post-warm-up decisions,
  correctness across modes).
- **Post-warm-up lock-in diagnostic (§22.12):**
  `internal/rl/linucb_lockin_diagnostic_test.go` replays the frozen v3 data
  through the production LinUCB, asserts a 100 % action-replay match, and writes
  the deterministic artifacts under `testdata/diagnostics/linucb_lockin/`.
- **Periodic exploration (§22.13):** `internal/rl/linucb_periodic_exploration_test.go`
  (interval 0 preserves behavior; interval 1/5 timing; deterministic cursor
  cycle; warm-up once; standard `A`/`b` update; greedy between events;
  invalid state does not advance the cursor; effectiveness bookkeeping) and
  `linucb_periodic_exploration_diagnostic_test.go` (deterministic frozen-v3
  replay, writes `testdata/diagnostics/linucb_periodic_exploration/`).
- **Live periodic benchmark (§22.14):**
  `internal/rl/linucb_periodic_exploration_live_test.go` replays the frozen v4
  standard/periodic runs through the production LinUCB (action-match validated)
  and writes `testdata/diagnostics/linucb_periodic_exploration_live/`;
  `cmd/experiment` interval override covered by `TestApplyLinUCBIntervalOverride`.
- **Six-mode mode plumbing (§22.15):** `ModeLinUCBPeriodic` /
  `AllModesWithBothLinUCB` exercised by the experiment tests and the
  `cmd/experiment` mode parser.
- **aria2 baseline harness (§22.16):** `cmd/aria2-benchmark/main_test.go`
  (`parseAria2Log`, `summarizeRanges`, deterministic `contentPattern`).
- **Granularity study (§22.17):** `cmd/granularity-study/main_test.go`
  (`summarizeRanges`, `contentPattern`) plus an integration run asserting 4×
  1 MiB requests, byte-match output, and in-flight concurrency ≤ 4.
- **Wall-clock schedule (§14.1, §22.18):** `internal/experiment/server/server_test.go`
  (validation, before/at/after transition, request-count independence, same
  elapsed → same rate, sequence diagnostic, unchanged `SetRateAfter`,
  clear/restore, concurrent consistency, request log) and
  `internal/experiment/wallclock_schedule_test.go` (wired `ScenarioConfig`
  schedule runs correctly and transitions by elapsed time).
- **aria2 wall-clock baseline (§22.19):** `cmd/aria2-wallclock/main_test.go`
  (`summarizeRanges`, `countRetries`, `contentPattern`, locked-config flags).
- **Wall-clock granularity control (§22.20):** `cmd/granularity-study/main_test.go`
  (`-scenario wallclock` produces 4×1 MiB requests, max in-flight 4, all before
  the 1 s transition).
- **Sequential RL / DQN (§22.22):** `internal/rl/dqn/` tests cover state
  normalization/temporal features/remaining-bytes/malformed rejection/
  determinism; model forward pass/action dim/malformed/round-trip; simulator
  reset/step/determinism/termination/truncation/randomization; action mapping and
  policy adapter; and loading the Python-exported model with deterministic Go
  inference. Python tests (`python/tests/`) cover the mirrored state contract,
  simulator, replay buffer, finite-loss/target-update/seed-hash determinism, and
  model export.
- **DQN validation/transfer (§22.23):** `python/train_validate.py` (multi-seed
  train, validation selection, held-out test, ablations, candidate freeze);
  `internal/rl/dqn/live.go` (incremental live-policy state + greedy inference);
  `cmd/dqn-transfer/` (real-downloader transfer run).
- **DQN env correction / v2 (§22.24):** `python/train_validate_v2.py` (corrected
  simulator training/validation/freeze) and the corrected `python/rl/sim_env.py`
  dynamics; the DQN Go package and transfer command are reused unchanged.
- **DQN v3/v4 (§22.27–§22.28):** `python/train_validate_v3.py` /
  `python/rl/sim_env_v3.py` (chunk-ladder + decision interval) and
  `python/train_validate_v4.py` / `python/rl/sim_env_v4.py` (concurrency-only,
  1 MiB fixed). Go: `internal/rl/dqn/policy.go`   (`ConcurrencyActionNames`/
  `ConcurrencyIndexToAction`), `internal/rl/dqn/live.go`
  (`NewLiveConcurrencyPolicy`); `cmd/dqn-transfer` (`-max-chunk`, `-fixed-chunk`).
- **DQN v5 pipeline (§22.30):** `python/train_validate_v5.py` was exercised by a
  tiny deterministic smoke run and then the full campaign (static sweep,
  interval selection, 5-seed training, validation selection, held-out test vs
  fixed C=1..4, diagnostics, gate, export). `internal/rl/dqn/v5_candidate_smoke_test.go`
  loads the exported v5 artifact, validates its state/action contract and
  SHA-256, and runs deterministic v5 greedy inference (skips if absent).
  `cmd/dqn-transfer` has `TestNewLivePolicyForModelDispatchesV5`; Python tests
  assert the new per-episode `returns`/`episode_steps` report fields.
- **DQN v5 live transfer (§22.31):** `cmd/dqn-live-v5` unit tests cover
  authoritative after-action derivation, range tiling, in-flight overlap,
  progress accounting, trajectory classification, and reaction analysis; the
  125-run campaign plus a 4-run smoke record all required per-run fields and
  pass the pre-specified gates.
- **Final v5-vs-aria2-v2 head-to-head (§22.32):** `cmd/final-evaluation-v5`
  unit tests cover the reused U1–U8 conditions, deterministic pair schedule,
  frozen-command argv parsing, authoritative v5 after-action derivation,
  transition reaction timing, in-flight overlap and range/progress accounting;
  the reused analyzer and `python/report_final_evaluation_v5.py` operate only on
  the frozen artifacts.
- **Candidate v6 (§22.33–§22.34):** `python/tests/test_state_v6.py`,
  `python/tests/test_sim_env_v6.py` (including the one-decision-per-request
  timing contract and shared-bottleneck diminishing returns),
  `internal/rl/dqn/state_v6_test.go` (feature parity, spec separation, live
  cadence), `internal/rl/dqn/v6_candidate_smoke_test.go` (artifact load/infer),
  and the static sweep/training/live commands.
- **Final v6-vs-aria2-F head-to-head (§22.35):** `cmd/final-evaluation-v6`
  reuses the v5 matched-pair structure with the v6 wall-clock phase scheduler;
  `python/analyze_final_evaluation.py` gained an additive `--conditions` flag
  (default behavior unchanged); `python/report_final_evaluation_v6.py`
  generates the report from the frozen artifacts.

**Latest verified test status:** after the RL State Observability Audit,
`go test ./...`, `go test -race ./...`,
`go test -count=5 ./internal/rl/ ./internal/downloader/ ./internal/experiment/`,
`go vet ./...`, `gofmt -l .`, and `go build ./...` all pass. Statement coverage
from a prior run: `internal/rl` 88.7 %, `internal/downloader` 92.3 %; earlier
figures: downloader ~93 %, policy/chunksize/concurrency/metrics ~100 %,
rangetracker ~96 %, fetcher ~86 %, assembler ~79 %, experiment ~93 %. Re-run to
confirm; do not treat these as frozen. After the Candidate v5 simulator/state
audit (§22.29), the same Go commands plus `python3 -m pytest python/tests -q`
(29 passed) all pass again, and the frozen `testdata/` manifest (155 files) is
byte-identical.

---

## 24. Completed milestones (from repository state)

1. `RangeTracker` (half-open allocation/lifecycle) + tests.
2. `Fetcher` (single HTTP Range request) + tests.
3. `Assembler` (random-access range writes) + tests.
4. Fixed-concurrency end-to-end downloader + CLI.
5. `Metrics` collector (per-attempt records/aggregate).
6. Dynamic concurrency (worker pool + `concurrency.Controller`).
7. `Policy` abstraction (Context/Action/ActionKind, `FixedPolicy`).
8. `ThroughputPolicy` (concurrency-only rule-based).
9. Policy runner integration (serialized decisions; windowed throughput).
10. Controlled HTTP range experiment server.
11. Fixed-vs-adaptive experiment runner/matrix.
12. `tc`/`netem` network-condition abstraction + privileged integration test.
13. `chunksize.Controller` (future allocation target).
14. Chunk-size integration in the downloader (lazy allocation, in-place retry).
15. `ChunkSizePolicy` (chunk-size-only rule-based).
16. Generic joint action application in the runner.
17. `JointPolicy` (deterministic alternating dimension baseline).
18. Four-mode experiment framework + CLI.
19. Control-trajectory validation (forward + reverse) of the four modes.
20. Four-mode baseline dataset (`testdata/baseline/`).
21. RL environment contract: State/action/reward + adapter, no learner.
22. First learner: disjoint LinUCB behind `rl.Policy` (deterministic;
    Gonum-backed SPD linear algebra; not wired into the downloader or the
    experiment matrix).
23. First reward computation layer: `RewardCalculator` + `RewardConfig`, wired
    into `policyRunner` with correct credit assignment (reward the previous
    action from the next observation); configurable, tested, first baseline
    (not empirically tuned or validated).
24. Dependency refactor: replaced the project's hand-written numerical linear
    algebra (`internal/rl/linalg.go`) with Gonum (`mat.Cholesky`, `SymDense`,
    `SymRankOne`); one direct dependency, zero transitive; behavior preserved.
25. LinUCB integrated as an opt-in fifth experiment mode (`ModeLinUCB`) through
    the existing Adapter + policyRunner + RewardCalculator; causal loop
    validated by deterministic integration tests; default four-mode behavior and
    the baseline dataset unchanged; not benchmarked.
26. LinUCB calibration and reward-diagnostics layer (`internal/experiment/
    calibration.go`, `cmd/calibration`) with deterministic server delay/fail
    scenario knobs; diagnostic only; output in `testdata/calibration/`.
27. Reward design review + new baseline (Candidate C): absolute + signed
    improvement throughput, state-change-based `ActionCost`, new
    `ThroughputImprovement` component/weight; calibration re-run; no learner or
    policy changes; not tuned or benchmarked.
28. RL State Observability Audit + minimum correction: identified that the
    per-request `Context.Throughput` cannot expose the throughput effect of
    concurrency; added a windowed wall-clock `AggregateThroughput` observable to
    `policy.Context`/`rl.State`/`Features` (dim 9 → 10) and based the reward's
    throughput components on it; diagnostic calibration regenerated. No weight
    tuning, no benchmark.
29. Formal 5-mode benchmark (first formal run): 5 scenarios × 5 modes × 10
    repetitions = 250 runs with the locked `DefaultMatrixConfig`; 250/250 valid;
    dataset in `testdata/benchmark/`; descriptive analysis only (no ranking).
    LinUCB selected `ActionNone` in every run (documented exploration lock-in).
30. LinUCB Exploration Diagnostic (§22.7): a separate, labeled forced-action
    study (`internal/diagnostics`, `cmd/linucb-diagnostic`,
    `testdata/diagnostics/linucb_exploration/`) establishing that `ActionNone`
    dominates due to the exploration/reward-scale interaction plus the
    initialization tie-break, that concurrency *is* observable, and that chunk
    size is not meaningfully observable. No production behavior changed; the fix
    was intentionally not implemented.
31. LinUCB Exploration Correction (§22.8): `LinUCBConfig.InitialExplorationRounds`
    (default 0) plus a deterministic warm-up pass; `runOnce` sets 1 for LinUCB.
    Every arm gets at least one update before normal UCB; bound-clamped no-ops
    are recorded as non-effective. Tests + a before/after diagnostic artifact
    (`testdata/diagnostics/linucb_exploration_fix/`). Correctness only; the
    frozen benchmark was not rerun and no performance claim is made.
32. Formal Benchmark v2 — post-exploration correction (§22.9): 5 scenarios × 5
    modes × 10 repetitions = 250 runs with `InitialExplorationRounds=1`;
    250/250 valid; warm-up order exact and all actions effective in every run;
    new dataset `testdata/benchmark_v2/`; v1/baseline/calibration frozen.
    Descriptive only (the 5-selection warm-up leaves only ~2 learned
    decisions/run); no performance claim.
33. LinUCB learning-horizon study (§22.10): a separate diagnostic
    (`internal/diagnostics/horizon.go`, `cmd/linucb-horizon/`,
    `testdata/diagnostics/linucb_horizon/`) varying only file size; measured
    decision counts and selected **4 MiB (4194304 B)** for the next benchmark
    (≥32 post-warm-up decisions worst-case, ~122 typical). No production change;
    no frozen dataset touched.
34. Formal Benchmark v3 — 4 MiB learning horizon (§22.11): 5 scenarios × 5 modes
    × 10 repetitions = 250 runs at FileSize=4194304; 250/250 valid; warm-up
    exact and all actions effective; 32–122 learned decisions/scenario. Result:
    **no meaningful post-warm-up policy evolution** (single-arm lock-in, no
    concurrency change). New dataset `testdata/benchmark_v3/`; explicit
    `-file-size` matrix override in `cmd/experiment` (default unchanged). No
    tuning; no performance claim.
35. LinUCB post-warm-up lock-in diagnostic (§22.12): replayed the frozen v3 data
    through the production LinUCB (4582/4582 actions matched) and quantified the
    lock-in. Confirmed causes: posterior dominance (A), exploration-bonus cap
    (D), and credit-assignment lag (E); partial: feature-norm dominance by a
    constant `bytes_transferred` (B) and the chunk-size environment limitation
    (F); reward separability (C) not supported. Artifacts in
    `testdata/diagnostics/linucb_lockin/`. No production change; no fix.
36. Periodic exploration experimental correction (§22.13): added
    `LinUCBConfig.ExplorationInterval` (default 0) — a deterministic forced action
    every N normal decisions, cycling the five arms with a separate cursor; the
    warm-up is unchanged; bounds no-ops are tracked and not counted. Unit tests
    + deterministic frozen-v3 replay diagnostic
    (`testdata/diagnostics/linucb_periodic_exploration/`). Result: coverage rises
    from 1 to 5 unique arms and frozen arms receive posterior updates, but the
    dominant arm preference is unchanged. No performance claim; no benchmark yet.
37. Live periodic-exploration benchmark (§22.14): 5 scenarios × 2 LinUCB variants
    × 10 reps = 100 live runs (`ScenarioConfig.LinUCBExplorationInterval` +
    `-linucb-exploration-interval`), 100/100 valid. Periodic exploration raised
    unique post-warm-up arms 1.0–1.4 → 5.0 and moved the posterior (forced arm
    reached rank 1); in `loss` it changed greedy behaviour (increase-concurrency
    58→227; final concurrency 1.30→2.30), elsewhere the dominant preference was
    unchanged. Behavioral only; no ranking or performance claim. Dataset
    `testdata/benchmark_v4/`.
38. Final six-mode 4 MiB benchmark (§22.15): added `ModeLinUCBPeriodic`
    (`LinUCBPeriodicInterval=5`) and ran 5 scenarios × 6 modes × 10 reps = 300
    live runs, 300/300 valid. Both LinUCB variants evaluated beside the four
    deterministic controllers. Behavioral + descriptive performance only; no
    ranking/superiority claim. Dataset `testdata/benchmark_v5/`.
39. aria2 external baseline (§22.16): new `cmd/aria2-benchmark/` measured
    `aria2c 1.37.0` against the same controlled scenarios (50/50 valid). Observed
    4 concurrent connections with fixed 1 MiB splits (aria2's minimum) and one
    initial non-range request; no within-run adaptation; 0 retries. Descriptive
    v5 comparison recorded with a request-granularity confound; no ranking. IDM
    not evaluated (Windows-only). Datasets `testdata/benchmark_aria2/`,
    `testdata/diagnostics/aria2/`.
40. Request-granularity sensitivity study (§22.17): new `cmd/granularity-study/`
    ran the internal fixed downloader at a granularity-matched config (1 MiB
    chunks, concurrency 4) over the five scenarios (50/50 valid; 4×1 MiB
    requests, max in-flight 4). The matched config reproduces aria2 closely in
    4/5 scenarios (0.85–0.97×), substantially reducing the granularity confound;
    latency retains ~2.3× unexplained aria2 overhead. Descriptive only.
    Artifacts `testdata/diagnostics/request_granularity/`.
41. Wall-clock server rate schedule (§14.1): additive `RateSchedule` API
    (`SetRateSchedule`/`SetRateScheduleAt`/`ClearRateSchedule`/`Schedule`/
    `CurrentRate`, `Stats.CurrentRate`/`RateScheduleActive`, injectable clock) on
    the experiment server, with centralized rate selection. Time-based transition
    independent of request count/granularity; existing fixed-rate and
    `SetRateAfter` behavior unchanged. Tests + a deterministic fake-clock
    diagnostic (32 KiB vs 1 MiB sequences cross at different request indices but
    the same elapsed time). Experimental infrastructure only; no scenario or
    dataset changed.
42. Wall-clock throttle benchmark (§22.18): experiment-only
    `server-throttle-wallclock` scenario (`cmd/wallclock-throttle/`) — 512 KiB/s →
    4 MiB/s after 1 s elapsed; 6 modes × 10 reps = 60 runs, 60/60 valid. The
    transition occurred at ≈1.04 s elapsed for every mode while request counts
    varied 46–128. Standard LinUCB still locked onto `decrease-concurrency`;
    periodic LinUCB changed preference to `increase-concurrency` (final
    concurrency 3.90) and matched concurrency-adaptive (1323 vs 1325 ms).
    Descriptive only; no ranking. Datasets `testdata/benchmark_v6/`,
    `testdata/diagnostics/wallclock_throttle/`. No frozen dataset changed.
43. aria2 wall-clock throttle baseline (§22.19): `cmd/aria2-wallclock/` measured
    aria2 1.37.0 under the wall-clock throttle (same locked config), 10/10
    valid. aria2 made 4 requests (3×1 MiB) all starting within ~0.07 s, so none
    began at/after the 1 s transition and the whole transfer ran at 512 KiB/s
    (mean 2129.7 ms). Confirms the time trigger removes the request-count
    confound but does not equalize granularity. Datasets
    `testdata/benchmark_aria2_v2/`, `testdata/diagnostics/aria2_wallclock/`.
    IDM still unevaluated.
44. Wall-clock granularity-matched aria2 control (§22.20): extended
    `cmd/granularity-study` with an additive `-scenario wallclock` (default
    unchanged) matching aria2's 1 MiB/4-connection shape under the wall-clock
    throttle, 10/10 valid. All 4 requests started before 1 s (none reached
    4 MiB/s); mean 2055.7 ms vs aria2 2129.7 ms (+3.6 %). Matching request size
    and parallelism reproduces aria2's wall-clock behavior closely; the residual
    is consistent with aria2's extra non-range probe and connection management.
    Artifacts `testdata/diagnostics/request_granularity_wallclock/`. No frozen
    dataset changed.
45. Final research evidence audit (§22.21): deterministic synthesis
    (`scripts/final_research_audit.py`) of the frozen chain into
    `testdata/final_research_audit/`. Verified all dataset validity counts from
    raw JSON (no summary/raw discrepancy). Supported: periodic exploration breaks
    frozen-arm starvation, causes posterior updates and effective controller
    changes. Supported-but-limited: granularity explains much of the aria2
    difference; wall-clock removes the request-count trigger. Not supported:
    novelty, optimality, consistent performance gain, chunk-size optimization,
    internal-vs-aria2 superiority, real-network generalization, fairness, RL
    adaptation (it is deterministic periodic), causal one-step effectiveness.
    Recommended next phase: paper/report/thesis/presentation for the controlled
    synthetic-server scope only.
46. Sequential RL / Double DQN architecture + sanity (§22.22): a new episodic
    Double DQN controller with a current/temporal 13-feature state (no saturated
    bytes feature), a time-based reward (`r=-elapsed s`, gamma=1), Python
    (CPU-only PyTorch) training against a randomized analytical simulator, and
    deterministic Go inference loading a portable JSON model. LinUCB retained
    unchanged as baseline. Sanity training (seed 0, 300 episodes) produced finite
    loss/Q values, all-terminal episodes, and a reproducible model hash
    (`db8602b4…`); same seed = identical hash, different seed differs. **No
    performance claim; no benchmark; simulator≠final conditions; model is a
    sanity model.** Artifacts `testdata/diagnostics/dqn/`; code
    `internal/rl/dqn/`, `python/rl/`.
47. DQN training + held-out validation + live transfer (§22.23): trained
    Double DQN across seeds 0–4 (800 ep), selected via held-out VALIDATION
    (seed 4), evaluated on 8 unseen TEST conditions vs simulator baselines and
    ablations, and froze **FINAL DQN CANDIDATE v1**
    (`83427c53…`). Held-out simulator: DQN 1.440 s vs fixed/conc/joint 2.029 s
    (baselines tie in the analytical simulator) with mixed per-condition results;
    ablations degrade as expected. A 25-run live transfer (5 scenarios × 5 reps)
    was 25/25 valid and behaviorally sensible (latency → concurrency 3.6; combined
    → max chunk). **No performance claim; no aria2 comparison.** Artifacts
    `testdata/diagnostics/dqn_validation/`; code `python/train_validate.py`,
    `internal/rl/dqn/live.go`, `cmd/dqn-transfer/`.
48. DQN environment correction + Candidate v2 (§22.24): corrected the simulator
    (shared-bandwidth concurrency, per-request overhead chunk tradeoff, and
    request-count + wall-clock throttles on a continuous clock), which fixed
    baseline fidelity (scripted baselines now change controllers and no longer
    tie). Retrained seeds 0–4 (1200 ep), selected via validation (seed 1), and
    froze **FINAL DQN CANDIDATE v2** (`59cbc5e6…`). Held-out: DQN 1.233 s vs
    fixed 1.764 / conc 1.706 / chunk 1.755 / joint 1.710; ablations degrade. Live
    transfer v2 25/25 valid; under the wall-clock throttle unhelpful chunk
    decreases fell 260→46 and effective concurrency changes rose 18→144, and
    combined holds concurrency 4. Candidate v1 retained as historical. **No
    performance claim; no aria2 comparison.** Artifacts
    `testdata/diagnostics/dqn_validation_v2/`; code `python/train_validate_v2.py`,
    corrected `python/rl/sim_env.py`.
49. aria2 baseline calibration (§22.25): selected and froze ONE global aria2
    configuration from a separate 75-run validation set (3 configs × 5
    wall-clock profiles × 5 reps; 75/75 valid). C (4 connections / split 4)
    selected on lowest pooled mean completion time (1496.3 ms vs B 2697.6, A
    4889.2); no tie-break needed; 0 retries/failures. All requests began before
    the rate transition in every config. **FINAL ARIA2 BASELINE CONFIG v1**
    (metadata hash `6b8cf33e…`) is now frozen for the head-to-head. Calibration
    only; DQN/Candidate v2 untouched; no final-test conditions used. Artifacts
    `testdata/diagnostics/aria2_calibration/`; code `cmd/aria2-calibration/`.
50. Final DQN vs aria2 evaluation (§22.26): locked 8-condition wall-clock test
    matrix (240 matched pairs / 480 downloads, randomized order, fresh per run),
    frozen candidates verified, 480/480 valid. **The pre-specified outperformance
    claim was NOT supported:** DQN geometric ratio 1.757 (95% CI [1.669, 1.850]),
    i.e. ~75.7% slower, faster in 1/8 conditions and 31/240 pairs; all four
    criteria failed. A first campaign missing per-run netem cleanup was archived
    and discarded; the clean re-run is analysed. Reported honestly; no tuning.
    Artifacts `testdata/final_evaluation/`; code `cmd/final-evaluation/`,
    `python/analyze_final_evaluation.py`.
51. DQN v3 request-efficiency correction (§22.27): separated decision frequency
    from chunk size (decision interval 1/4/8, selected 4), extended the chunk
    ladder to {32..1024} KiB, mechanically costed requests, and expanded the
    training distribution. Candidate v3 (`96361535…`, seed 0) uses 13.8 req/MiB
    in held-out simulation (~2–3× faster than scripted baselines). Live transfer
    is **mixed**: fragmentation improved (throttle attempts 75→37, chunk to
    ~449 KiB) but concurrency adaptation regressed (stays at concurrency 1;
    slower under latency/combined). Experimental, **not** a validated candidate
    for the claim; Candidate v2 untouched; no aria2 comparison; no performance
    claim. Artifacts `testdata/diagnostics/dqn_validation_v3/`; code
    `python/train_validate_v3.py`, `python/rl/sim_env_v3.py`, `cmd/dqn-transfer`
    (`-max-chunk`).
52. DQN v4 adaptive-concurrency formulation (§22.28): final RL formulation —
    chunk fixed at 1 MiB, concurrency-only action space, parallel-wave simulator
    with a real concurrency optimum, decision interval 8 selected. Candidate v4
    (`227d9845…`, seed 1) is deterministic and makes **4 × 1 MiB requests** live
    (aria2's granularity) holding concurrency 3, 25/25 valid — removing the v2/v3
    fragmentation/chunk-drift failures with a clean sim-to-real transfer. This is
    **concurrency-only, not joint chunk/concurrency**. No aria2 comparison; no
    performance claim. Artifacts `testdata/diagnostics/dqn_validation_v4/`; code
    `python/train_validate_v4.py`, `python/rl/sim_env_v4.py`,
    `internal/rl/dqn/{policy,live}.go`, `cmd/dqn-transfer` (`-fixed-chunk`).
53. DQN Candidate v5 simulator/state audit (§22.29): audited the v5
    concurrency-only formulation (state spec version 2) and fixed six
    correctness defects — decision-interval concurrency suppression, unapplied
    mid-step wall-clock phase transitions, a dead `request_duration_slope`,
    per-interval instead of per-attempt window counters, fabricated live
    progress (`successes × last bytes`), and a Python export contract that could
    not produce a Go-valid v5 model. Added deterministic Python/Go regression
    tests including a shared exact feature-vector contract. No v5 model exists
    and no training/validation/live-transfer run has been performed; the v1–v4
    candidates and the whole `testdata/` manifest are byte-identical.
54. DQN Candidate v5 training/validation pipeline (§22.30): implemented
    `python/train_validate_v5.py` (static concurrency sweep prerequisite,
    validation-only decision-interval selection, 5-seed training,
    validation-only seed selection, held-out test vs fixed C=1..4, behavior
    diagnostics, pre-specified constant/ramp NOT-READY gate, one export) plus
    the Go artifact smoke test and the `cmd/dqn-transfer` version-aware
    dispatch. The static sweep shows distinct best fixed C {1,2,3,4} across 8
    regimes; the gate passes (constant fraction 0.062, ramp-to-max 0.219, 64
    effective changes) but the model is slower than the best fixed C on 7/8
    held-out conditions. Candidate v5 SHA-256 `3a927a58…`, reproducible. No
    live transfer, no aria2 comparison, no performance claim.
55. DQN Candidate v5 live transfer validation (§22.31): new experiment-only
    `cmd/dqn-live-v5/` (authoritative controller-target before/after logging,
    wall-clock transitions, per-run netem apply/clear, full accounting checks)
    ran 5 scenarios × 5 policies (fixed C=1..4 + v5) × 5 reps = 125 live runs
    on the controlled server at 16 MiB / 1 MiB chunks: 125/125 valid, 0
    network-unavailable, qdisc clean. v5 tracks fixed-C3 closely and is slower
    than fixed-C4 everywhere; it is non-constant but oscillatory (noisy ramp to
    C3–C4). The three pre-specified gates pass; the qualitative reading is
    shallow adaptation. Candidate hash unchanged; v1–v4 and aria2 v2 untouched;
    no aria2 comparison, no performance claim.
56. Final v5-vs-aria2-v2 head-to-head (§22.32): new experiment-only
    `cmd/final-evaluation-v5/` reused the established U1–U8 / 30-pair /
    order-seed methodology at the frozen 16 MiB scale and ran 480 downloads
    (240 matched pairs) with 480/480 valid. v5 was slower in all 8 conditions
    (pooled geometric ratio 2.3217, 95 % CI [2.2269, 2.4206]; v5 faster in
    1/240 pairs and 0/8 conditions; all pre-specified criteria false). The
    mandatory 4-vs-16 concurrency asymmetry (v5 cap vs aria2-F's 16 connections
    with independent server pacing) is preserved in the artifacts. Statistics
    reused `python/analyze_final_evaluation.py` unchanged; the human report is
    generated by `python/report_final_evaluation_v5.py`. All prior frozen
    artifacts unchanged; no tuning, no post-hoc changes; no superiority claim.
57. Candidate v6 pre-registered unstable-network study (§22.33–§22.34): additive
    v6 state/simulator/live code (state spec version 3, concurrency 1–16, one
    decision per completed request); static C=1..16 sweep with distinct optima
    {1,2,5,9,16} (GATE A); 5-seed Double DQN training and validation-only
    selection (GATE B: 4.476 s vs best global fixed C12 4.867 s on held-out
    dynamic simulator conditions); condition-dependent phase behavior (GATE C:
    constant/ramp 0, 3/5 responsive); and 255/255 valid live runs on the
    controlled server under the same 5 trajectories (GATE D: 4/5 responsive,
    non-constant, non-ramp). v6 was slower than the best fixed C in every live
    condition (v6 7578 ms vs best global fixed C11 5012 ms); the live
    per-response pacing rewards high concurrency and the shared-bottleneck
    tradeoff transfers only partially. Candidate SHA-256 `daa71767…`,
    reproducible. No aria2 comparison; no performance/superiority claim.
58. Final pre-registered v6-vs-aria2-F head-to-head (§22.35): 150 matched pairs
    (300 runs, 300/300 valid) on the five v6 trajectories A2–E2. Pooled
    geometric ratio 1.4560 (95 % CI [1.3004, 1.6303]) → v6 slower; v6 faster
    only on E2 (ratio 0.948, Holm p = 0.128, not significant); all
    pre-specified criteria false → claim not supported. v6 remained
    condition-responsive, but the adaptation did not beat aria2-F under these
    controlled unstable trajectories. No tuning, no post-hoc changes; all
    frozen artifacts unchanged; no general-superiority or mobile-network claim.
59. D2 replication + long-trajectory diagnostic (§22.37): 20 matched pairs at
    512 MiB/D2 reproduced the v6 advantage (ratio 0.5816, 95 % CI
    [0.566,0.598], v6 faster 20/20) and identified the mechanism as a
    request-granularity × phase-locked-pacing interaction (aria2's 32 MiB pieces
    trapped at the initial 640 KiB/s rate; v6's 1 MiB pieces mostly issued after
    recovery). A new 7-phase long multi-transition trajectory L1 was designed
    (12–18 s phases) and swept in the frozen simulator: per-phase best C ∈
    {1,2,3,5,16}, but the full 512 MiB trajectory spread is only ~4 % (best
    fixed C≈15), so the duration hypothesis is not yet strongly promising.
    512 MiB remains the largest RAM-safe live size. No training, no v7, no
    change to §22.35 or any frozen artifact.
60. L2 environment design + V7 gate (§22.38–§22.39): pre-registered seven-phase
    512 MiB L2 trajectory with 18–25 s phases; per-phase best C = [16,2,4,16,2,
    6,16] with C16 penalties up to 62 % in degraded phases, but the full-run
    achievable headroom (ramp-aware oracle vs best static C16) is −0.3 %
    (idealized instant oracle 11.4 %, unreachable under ±1 actions); live probe
    shows aria2 1.37.0 cannot deliver ~1 MiB ranges with ≤16 connections at
    512 MiB (split=512 still yields 15×32 MiB pieces). Gates:
    NOT_READY_ENVIRONMENT_TOO_WEAK and NOT_READY_ARIA2_GRANULARITY. No v7
    training; frozen artifacts unchanged.
61. v7 formulation + L3 environment design (§22.40–§22.41): pre-registered and
    measured a ±4 control-authority action space (selected from A/B/C) plus a
    byte-weighted 7-phase L3 trajectory. Phase optima [16,2,4,16,2,6,16] (GATE A
    pass), action-constrained oracle 140.5 s vs best static C16 152.9 s →
    8.1 % headroom (GATE B fail, threshold 10 %), transitions reached with work
    (corrected metric), additive runner step semantics with a contract test
    (GATE D pass), live shared-cap tradeoff confirmed from frozen v6 live data
    (GATE E pass), authority sufficient (GATE F pass). **OUTCOME: STOP — v7
    training not justified.** The only code change is the additive
    `policyRunner` step semantics (Value ≤ 0 unchanged for v1–v6); all frozen
    artifacts and the frozen v6/aria2-F unchanged.
62. L4 environment design + final outcome (§22.42–§22.43): pre-registered
    byte-weighted 7-phase L4 trajectory keeping the L3 ±4 control formulation.
    Simulator: best static C16 = 174.0 s, action-constrained oracle = 143.4 s →
    17.6 % headroom (GATE B PASS), distinct phase optima (A), transitions with
    work (C), parity (D), authority (F); byte-weighting 47.1 % vs ≥50 % rule.
    Representative live fixed-C sweep (`cmd/l4-live-probe`): high-capacity and
    recovery regimes match (C16 ≫ C1), but the degraded shared-cap regime does
    not penalize high C live (C16 3.57 ≈ C8 3.39 > C4 2.58 MiB/s), contradicting
    the simulator's low-C optimum. **FINAL OUTCOME: `NOT_READY_LIVE_MISMATCH`.**
    Another RL training iteration is NOT justified. No training, no new aria2
    comparison; frozen artifacts unchanged.
63. T1 live request-level server throttling (§22.44–§22.45): pre-registered
    512 MiB normal→throttled→recovered schedule with 1 MiB requests. Live smoke
    verified request-level pacing persistence (GATE A), but the required
    fixed-C sweep showed C4–C16 finish before the throttle at normal 8 MiB/s
    and aggregate throughput increases monotonically with C, so there is no
    live concurrency reversal at equal 1 MiB granularity (GATE B/E FAIL;
    granularity-isolation D PASS). The oracle attempt was invalidated by the
    schedule flaw (ran entirely pre-throttle, headroom −5.3 %). **Outcome:
    `NOT_READY` — no RL iteration justified.** Confirms D2's v6 benefit was a
    request-granularity effect, not adaptive concurrency. No training, no new
    aria2 comparison; frozen artifacts unchanged.
64. T2 adaptive range-size diagnostic (§22.46–§22.47): live static chunk sweep
    at fixed C=16 over a two-throttle 512 MiB trajectory. The largest 16 MiB
    chunk was fastest (184.2 s vs 245.7–267.0 s for smaller chunks) because
    requests started in fast phases retain the fast rate across throttles and
    bypass them; smaller chunks keep sampling the slow rate. No chunk-size
    preference reversal, and no fixed change beat the best fixed chunk with
    adaptive control. **Outcome: `NOT_READY_NO_CHUNK_TRADEOFF`** (classifier
    logged `NOT_READY_NON_RL_GRANULARITY_EFFECT` due to a phase-start
    artifact). No RL range-size training justified; no training, no aria2
    comparison; frozen artifacts unchanged.
65. M1 real mobile-trace replay (§22.48): prerequisite audit only. No real
    mobile/cellular trace exists in the repository (only synthetic request
    traces), and the pre-registered rules forbid fabricating mobile data, so
    the stage stopped before replay construction or any concurrency
    experiment. **Outcome: `NOT_READY_NO_REAL_MOBILE_TRACE`.** Public real
    sources are reachable (UGent/imec 4G logs and GitHub returned 200/301),
    so a dedicated data-acquisition + replay-fidelity stage is the next viable
    step; the capacity-vs-observed-throughput caveat must be resolved then.
    No training, no aria2 comparison; all frozen artifacts unchanged.
66. M2 mobile trace parsers + tests; M3 deferred (§22.50): added
    `python/rl/mobile_trace.py` and `python/tests/test_mobile_trace_m2.py`
    (deterministic NYU-METS/UGent parsing, canonicalization, quality and hash
    helpers). M3's throughput-envelope replay, fidelity validation, fixed-C
    sweep, temporal `C*(t)`, oracle and all gates were NOT executed and are
    not claimed. No RL, no aria2; frozen artifacts unchanged.
67. Early-replay corrective fixes (§22.63; code/tests only, no native runs):
    child downloader errors now exit the child non-zero and are persisted
    (`fatal`/`canceled`/`err` in `summary.json`); `SUCCESS_TRACE_REACHED_EOF`
    now requires a normal mm-link exit, no fatal child error, **and** a
    downlink-log endpoint within the frozen 100 ms `AlignmentTolerance` of the
    `.down` trace duration, exposed through explicit `Result` evidence fields
    and new outcomes `DOWNLOADER_FATAL_ERROR` / `TRACE_ENDPOINT_NOT_REACHED`;
    the pilot resolves `<family>.up`/`<family>.down` pairs and rejects
    missing/mismatched pairs; the pilot request timeout is explicit
    (`-timeout`, default 120 s) and the 45 m watchdog is unchanged. Tests cover
    synthetic CASES A–F plus child status/exit and pairing, and the full
    `go test`/`-race`/`-count=5`/`vet`/`gofmt`/`build` suite passes. The native
    diagnostics were subsequently run and passed in milestone 68; no duration
    is frozen and `/home/vscode/epoch_pilot` is untouched.
68. Native replay diagnostics (§22.64): the two pre-pilot validations
    (`Verizon-EVDO-driving` C=1 and `Verizon-LTE-driving` C=16, 120 s request
    timeout, 45 m watchdog, fresh isolated output dirs) were executed as full
    real-time replays and both returned `SUCCESS_TRACE_REACHED_EOF` with
    `endpoint_valid=true`, `mm_link_exit=0`, child status `canceled` (no fatal
    error), endpoint residual **−35 ms** (within the frozen 100 ms
    `AlignmentTolerance`), and complete downlink delivery. No full pilot, no
    epoch-duration selection, no headroom/oracle/tau, no RL/adaptive control,
    no aria2 comparison, no protocol change; cleanup verified (no mm-link, no
    namespaces, qdisc clean). Outcome: `NATIVE_DIAGNOSTICS_PASS — READY FOR
    CORRECTED PILOT` (does not imply `EPOCH_DURATION_READY`).
69. Corrected formal epoch pilot (§22.65): the 10-run matrix was launched
    (`/home/vscode/epoch_pilot_corrected`) with the registered protocol. EVDO
    C=1 and C=2 completed valid trace EOF; EVDO C=4, C=8 and C=16 died early
    with fatal `retry limit exceeded` (2 attempts) at trace times 512.3 s /
    240.1 s / 150.0 s of 1 062.0 s, and the per-run gate correctly rejected
    them (`DOWNLOADER_FATAL_ERROR`, `endpoint_valid=false`). Per the
    pre-registered stop rule the pilot was stopped; LTE C=1 was
    operator-aborted and LTE C=2 was killed with the driver (both recorded
    artefacts), LTE C=4/8/16 were not run. No epoch extraction, shared
    validity, candidate coverage, duration selection, oracle, tau, or headroom
    was computed. Outcome: `BLOCKED — FORMAL EPOCH PILOT INVALID`. No protocol
    parameter changed; `/home/vscode/epoch_pilot` untouched.
70. EVDO timeout-censoring diagnosis (§22.66): diagnosis-only, no code or
    protocol change. Frozen-artifact re-analysis (trace max gap 11.6 s; C1/C2
    service-time scaling; C8's exact 2×120 s death; C16's 120 s + 30 s dial
    death) plus a controlled 120 s vs 600 s sensitivity sweep on EVDO
    C=4/8/16 (six fresh runs) established that the 120 s per-request timeout
    censors high-C requests under the shared low-rate link: C4/C8 reach
    genuine trace EOF at 600 s (4 and 24 completed requests had needed >120 s;
    max 183.9 s / 421.0 s), while C16 at 600 s advances to 62.0 MB / 969.5 s
    but still dies when the 600 s request timeout (~940 s) is followed by the
    fixed 30 s transport dial timeout on the retry (~971 s). Outcome:
    `TIMEOUT_CENSORING_SUPPORTED`; the formal 120 s timeout is unsafe for the
    fixed-C EVDO measurement, and any remedy requires a separately
    pre-registered protocol/harness decision. Diagnostic artifacts under
    `/home/vscode/evdo_timeout_diag/`; frozen pilot dirs untouched.
71. Timeout-policy decision gate (§22.67; implemented in §22.68). Inventoried
    the full deadline stack (120 s `http.Client.Timeout`, fixed 30 s transport
    dial timeout, unused TLS/Expect timers, 90 s idle timeout, 45 m parent and
    child watchdogs, trace-EOF signalling, retry semantics) and verified from
    code plus the existing synthetic tests that the 45 m watchdog cleanly
    bounds and cleans up a blocked replay. Recommended a **watchdog-only**
    formal policy (no HTTP request deadline, no dial deadline; 45 m watchdogs
    retained; retry semantics unchanged), rejecting 120 s, 600 s, a
    watchdog-derived deadline, and dial-timeout retention. No code or protocol
    change; a versioned pre-registration plus a fresh complete pilot is
    required. Outcome: `TIMEOUT_POLICY_READY_FOR_PREREGISTERED_REVISION`.
72. Watchdog-only timeout policy implemented (§22.68): explicit no-request /
    no-dial deadline configuration in `internal/fetcher` (with ambiguity
    validation) and `internal/downloader`, `replay.ParseDeadline`, pilot and
    mm-replay `-timeout`/`-dial-timeout` flags whose formal default is
    `none`/`none`, and a versioned protocol revision (`epoch.Protocol`
    version 2, `LegacyProtocolV1`, `ValidateTimeoutPolicyRevision`). The
    request context chain and retry semantics are unchanged; trace-EOF,
    watchdog, external-cancellation and fatal-error outcomes remain distinct.
    All required synthetic tests pass, full `go test`/`-race`/`vet`/`gofmt`/
    `build` pass, and one bounded EVDO C=1 native smoke with
    `-timeout none -dial-timeout none` returned `WATCHDOG_TIMEOUT` with clean
    teardown. No full pilot was run; a fresh complete 10-run version-2 pilot
    in a new output dir is the next step. Outcome:
    `WATCHDOG_ONLY_POLICY_IMPLEMENTED_AND_VALIDATED`.
73. Formal Stage 0 pilot under protocol v2 (§22.69): the fresh 10-run
    watchdog-only matrix (`/home/vscode/epoch_pilot_v2`, 2026-09-27
    08:53–12:15 UTC) completed with all 10 runs valid trace EOF
    (`endpoint_valid=true`, residuals −35…−58 ms, no fatal error, no workload
    exhaustion, `-timeout none -dial-timeout none` per run). Epoch extraction
    and shared validity completed; coverage: 30 s 43/82 (52.4 %), 60 s 25/41
    (61.0 %), 90 s 21/28 (75.0 %), 120 s 18/21 (85.7 %, min-trace 7) — only
    **120 s** passes the frozen rule and was selected (`2m0s`). Cleanup
    verified; historical v1/v2 diagnostic artifacts untouched. Outcome:
    `EPOCH_PILOT_V2_COMPLETE — READY_FOR_ORACLE_STAGE`; no oracle/headroom/tau
    computed.
74. Oracle stage preparation (§22.70): the frozen v2 input was verified
    (protocol v2, selected 120 s, 21 candidate / 18 shared-valid / 3 excluded
    epochs; EVDO 7, LTE 11) and preserved as deterministic analysis artifacts
    under `/home/vscode/oracle_stage_v2/`. Formal headroom was **not**
    computed: the repository contains no pre-registered tau regime classifier/
    threshold, no tau measurement (switch-injection) design, and no
    completion-time/workload mapping, as §22.52 already records. No threshold
    or measurement was invented. Outcome: `BLOCKED — ORACLE PREREQUISITE
    UNRESOLVED` (specific gate: `BLOCKED — TAU REGIME RULE NOT
    PRE-REGISTERED`). Validation suite passes; pilot artifacts unchanged.
75. Oracle/tau pre-registration + native tau measurement (§22.71): the oracle
    objective (B_e = 5 MiB, t = B_e/R, constrained-oracle ladder/dwell, H_j),
    the native-regime classifier, the 2.0 regime threshold, the injection
    selection, and the tau aggregation rule are frozen in
    `testdata/oracle_preregistration_v2/`. `internal/experiment/tau` and
    `cmd/tau-injection` implement them; `replay.ChildConfig` gained
    harness-only adaptive bounds and a scheduled switch. The 72-run
    switch-injection campaign (9 timestamps × 8 directions, protocol v2)
    completed 72/72 valid with 0 failures; all eight directions are measured
    and aggregated (only 1→2 is regime-specific under the frozen rule; pooled
    |tau| ≤ 0.57 s elsewhere). No headroom/oracle was computed. Outcome:
    `TAU_PREREGISTERED_AND_MEASURED`. A simultaneous-launch mm-link address
    race was found and mitigated with a 5 s launch gate.
76. Formal oracle/headroom analysis (§22.72): the exact frozen DP produced
    T_best_fixed (EVDO 543.98 s at C8; LTE 68.52 s at C16), T_instantaneous
    (534.81 / 68.26 s), and T_constrained (535.24 / 68.22 s) with persisted
    ladder/dwell-valid sequences; descriptive two-trace headroom is EVDO
    1.61 %, LTE 0.44 %. The pre-registered sanity bound
    `T_instantaneous <= T_constrained` fails for LTE because the frozen taus
    preserve negative measured values (8→4 −0.1303 s, 8→16 −0.1249 s); the
    DP was verified by exhaustive brute force (exact match) and no value was
    clamped. Outcome: `BLOCKED — ORACLE COMPUTATION INCONSISTENT`; the
    nonnegative-tau sensitivity (EVDO 1.58 %, LTE 0.047 %) and zero-tau
    diagnostic (1.64 % / 0.17 %) are recorded as secondary. n=2 →
    `FINAL_STATISTICAL_GATE = PENDING`. Artifacts under
    `/home/vscode/oracle_stage_v2/`, byte-identical across two runs.
77. Nonnegative formal tau, oracle protocol version 2 (§22.73): the signed
    cost model was superseded by `tau_formal = max(0, estimator)` (estimator
    and 2.0 regime rule unchanged; raw negatives preserved), frozen in
    `testdata/oracle_preregistration_v3/` before a fresh campaign. The v3
    switch-injection campaign (`/home/vscode/tau_campaign_v3/`, identical
    design/timestamps, plan hashes matching v2) completed 72/72 valid with 0
    failures; 32 raw negative observations retained; formal tau is ≥ 0 for
    every direction (2→1 and 8→4 floored to 0; no regime-specific directions
    in the fresh data). Headroom was **not** recomputed; the v2 pilot and v2
    signed-tau campaign are untouched historical evidence. Outcome:
    `TAU_COST_MODEL_V2_READY_FOR_ORACLE`.
78. Formal oracle/headroom under oracle protocol version 2 (§22.74): using the
    frozen nonnegative formal tau table, the exact DP produced T_best_fixed
    (EVDO 543.98 s at C8; LTE 68.52 s at C16), T_instantaneous (534.81 /
    68.26 s) and T_constrained (535.24 / 68.50 s) with persisted
    ladder/dwell-valid sequences; all sanity inequalities hold. Descriptive
    two-trace headroom: **EVDO 1.6075 %, LTE 0.0320 %**, both below the 10 %
    target. Historical signed-tau reproduction (§22.72) and zero-tau
    diagnostics are recorded separately. n=2 → final statistical gate
    `PENDING`. Artifacts under `/home/vscode/oracle_stage_v3/`, byte-identical
    across two runs; outcome `ORACLE_STAGE_V3_COMPLETE —
    DESCRIPTIVE_HEADROOM_COMPUTED`.
79. Final trace-expansion decision gate (§22.75, preparation/audit only): a
    read-only audit found that the final ≥5-trace protocol is **not frozen**.
    The "≥5 usable diverse traces, CI_95_lower(H) ≥ 0.10" string exists only
    in analysis output/docs, not in `oracle_preregistration_v3`; diversity
    strata, admissibility/duration/contamination rules, additional trace
    identities, tau scope for new traces, reuse rules, and the exact
    statistical formula were all UNFROZEN at audit time (now frozen in §22.76).
    The classifier is frozen and
    applies to the proposed candidates (`ATT-LTE-driving`,
    `TMobile-LTE-driving`, `TMobile-UMTS-driving`; 4/2/5 injection
    timestamps). Conditional minimum work if the aspiration held: 3 additional
    traces × 5 C = 15 fixed-C replays, plus 0–88 tau runs depending on the
    unfrozen scope. No runs, no headroom recomputation; audit artifacts under
    `/home/vscode/final_trace_expansion_decision/`. Outcome:
    `BLOCKED — FINAL TRACE PROTOCOL INCOMPLETE`.
80. Final trace protocol frozen (§22.76, documentation only): the
    prospective pre-registration `testdata/oracle_preregistration_v4/
    final_trace_protocol.json` (+ `.txt`) freezes the five-trace population
    (Verizon-EVDO-driving, Verizon-LTE-driving, ATT-LTE-driving,
    TMobile-LTE-driving, TMobile-UMTS-driving), eligibility (≥450 s, paired
    `.up`/`.down`, EOF/endpoint validation, no fatal error, existing
    contamination checks, one count per family, no post-hoc substitution),
    the existing replay/oracle rules, **per-trace tau**, reuse rules
    (`epoch_pilot_v2`/`tau_campaign_v3` for the two current traces only;
    fresh fixed-C and per-trace tau for the three new traces;
    `tau_campaign_v2` excluded), the headroom definition, the statistical
    design (mean H, sample SD, one-sided 95 % t LCB with df = n−1, pass iff
    `LCB_95 >= 0.10`), and invalid/missing handling. The experiment matrix is
    15 fixed-C + 88 tau = 103 new runs. Planning artifacts updated; no source
    or measurement change. Outcome: `FINAL_TRACE_PROTOCOL_FROZEN — READY FOR
    FINAL TRACE EXECUTION`.
81. Stage A executed (§22.77, measurement only): the 15 fixed-C full-trace
    protocol-v2 replays for the three new traces ran in a fresh output dir
    (`/home/vscode/final_fixed_c_stage_a/`, 2026-09-27 21:17:05 →
    2026-09-28 00:38:47 UTC). 15/15 valid (`SUCCESS_TRACE_REACHED_EOF`,
    endpoint residuals −34…−57 ms, no fatal/child error, no workload
    exhaustion, `'-timeout' 'none' '-dial-timeout' 'none'` in every run
    header), 0 technical failures, 0 reruns. All three new families eligible
    (≥450 s, paired, EOF-validated). No tau/oracle/statistical computation;
    all frozen artifacts unchanged; no source change. Outcome:
    `STAGE_A_COMPLETE — 15/15 FIXED-C REPLAYS VALID`. Next: 88 per-trace tau
    runs, then the per-trace oracle and the pre-registered five-trace gate.
82. Stage B boundary-0 amendment frozen before execution (§22.78,
    documentation only): the frozen `ATT-LTE-driving` plan's good epoch-0
    injection (`start_ms = 0`) cannot be executed exactly by the frozen
    harness (`SwitchAt > 0` required; `SwitchAt = 0` disables the switch).
    With no Stage-B run started (0/88), the protocol was prospectively
    amended: boundary-0 injections execute with the fixed 1 ms switch
    instant, start at source C, record `switch_applied = true`, and let the
    target C govern `[0, 120 s)`. No other rule changed; ATT remains
    4 × 8 = 32 and the Stage-B matrix remains 88 runs; no source change, no
    frozen result altered, no run executed. Outcome:
    `BOUNDARY-0 AMENDMENT FROZEN BEFORE EXECUTION — STAGE B READY`.
83. Stage B executed (§22.79, measurement only): the 88 preregistered per-trace
    tau switch-injection runs for the three new traces ran under protocol v2 +
    amendment 1 (2026-09-28 02:18:40 → 04:49:17 UTC; output
    `/home/vscode/final_tau_stage_b/`). 88/88 valid (ATT 32/32, TMobile-LTE
    16/16, UMTS 40/40), 0 technical failures, 0 reruns; the 8 ATT boundary-0
    runs used exactly the 1 ms switch instant with `switch_applied = true` and
    the target C governing `[0, 120 s)`. Raw signed observations preserved;
    per-trace frozen median aggregation with `formal tau = max(0, estimate)`;
    no cross-trace pooling; no oracle/H/CI/gate; frozen artifacts and source
    unchanged; host cleanup verified. Outcome: `STAGE_B_COMPLETE — 88/88
    VALID`. Next: per-trace oracle analysis and the pre-registered five-trace
    LCB gate (Stage C, not started).
84. Stage C final analysis executed (§22.80, analysis only): the frozen
    oracle ran on the five-trace population under oracle protocol v2 +
    final trace protocol v1 + amendment 1 (output
    `/home/vscode/final_oracle_analysis/`). Retained epochs = shared-valid
    full 120 s native-downlink epochs; native final partials (ATT e6,
    TMobile-LTE e3) excluded (no frozen regime label). Five trace-level H
    values: EVDO 0.0164075465, LTE 0.0001943981, ATT 0.0001563742,
    TMobile-LTE 0.0006142775, UMTS 0.0029383433; mean 0.0040621879, sample SD
    0.0069959662, df = 4, t = 2.1318467863, `LCB_95 = −0.0026077024`,
    threshold 0.10 → **FAIL**. Sanity bound holds for all five traces;
    17/17 integrity checks pass; no experiment, no source change, no frozen
    artifact modified. Outcome: `FINAL_ORACLE_ANALYSIS_COMPLETE — FINAL
    STATISTICAL GATE FAIL`.
85. Salvage Protocol v7 frozen (§22.81, documentation only): a separate
    prospective study of chunk-size adaptation under finite-file, tail-aware
    conditions was specified in `testdata/salvage_s_protocol_v7/` (scope,
    mechanism, arms, pilot, endpoint, Bayesian model/priors, best-fixed
    estimand, switching family, trigger variable/semantics, randomization,
    fixed-sequence multiplicity, SSR, Stage 0S rule requirements,
    pilot/confirmatory separation; trace validation out of scope). Status:
    `FINAL_S_PROTOCOL_FROZEN — BLOCKED_PENDING_HUMAN_PARAMETERS` — the
    human-owned parameters `R`, `D`, `FileSize`, `N` and the trigger firing
    rule, `T_tail_primary` definition, Stage 0S go/no-go rule, and SSR scenario
    grid are missing and were **not** invented. No experiment, no pilot, no
    source change; the completed cellular study is untouched.
86. Salvage Protocol v7 parameter-resolution pass (§22.82, documentation
    only): four candidate mechanism packages were evaluated analytically
    (A/C/D admissible, B inadmissible with `T_tail_primary` 16.82 s < 20 s),
    exact proposals were recorded for `T_tail_primary = D + C_max/R`, the
    `B_switch = N × S0` trigger, the Stage 0S go/no-go rule, and the
    18-scenario SSR grid, and the log-scale `delta_T` interpretation
    (`log(0.95) = −0.051293294`) was recommended. The exploratory-switch
    duplicate was removed and the fixed sequence (`16→4`, `4→1`, `1→256K`)
    was frozen (both task-directed). **No candidate or proposal is
    approved**; status remains
    `FINAL_S_PROTOCOL_FROZEN — BLOCKED_PENDING_HUMAN_PARAMETERS`. No
    experiment, pilot, source change, or evidence change.
87. Salvage Protocol v7 final human decision frozen (§22.83, documentation
    only): Candidate A frozen (`R=4 Mbps`, `D=40 ms`, `FileSize=512 MiB`,
    `N=4`); `T_ref=268.435456 s`, `delta_T=13.4217728 s`,
    `delta_log=log(0.95)=−0.05129329438755058`, `T_tail_primary=33.594432 s`,
    `delta_tail=1.6797216 s`; trigger `B_switch=N·S0` = 64/16/4 MiB; no
    exploratory strategy; fixed sequence `16→4`, `4→1`, `1→256K`; Stage 0S
    GO/NO-GO rule and SSR grid (18 × 1000 = 18,000 datasets) frozen;
    confirmatory `n ≤ 12`; pilot data not pooled; trace validation out of
    scope. Validation 19/19 PASS; one transcription difference noted
    (`33.594304` stated vs `33.594432` formula-derived; formula operative).
    Outcome: `FINAL_S_PROTOCOL_FROZEN — READY_FOR_STAGE_0S`. No experiment,
    pilot, source change, or evidence change.
88. Stage 0S pilot executed (§22.84, measurement only): the frozen 32-run
    feasibility pilot ran 2026-09-28 10:19–12:52 UTC under the frozen
    mechanism (R = 500,000 B/s, D = 40 ms, FileSize = 512 MiB, N = 4; four
    fixed arms; randomized within blocks, seed 20260928). 32/32 clean, 0
    technical failures, 0 reruns. All four frozen criteria PASS: mechanism
    presence (+2.38…+2.52 % of D+s/R), strict direction, realized tail median
    34.9776 s within [26.8755, 41.9930] s, reliability (32/32 clean, max
    wall-clock 296.6 s, within-block residual SD 0.000795). Formal decision:
    **GO** — the confirmatory stage is justified under the protocol.
    Artifacts: `testdata/salvage_s_stage0s/`. No source, protocol, or prior
    evidence changed; environment cleaned; no confirmatory analysis run.
    Outcome: `STAGE_0S_COMPLETE — GO`.
89. Pre-Stage-1 scaling/headroom analysis executed (§22.85, analytical
    simulation only): a deterministic simulator of the frozen finite-file
    semantics (two never-blended models; Stage 0S-calibrated Model B validated
    within 0.4 %) over 64 MiB–32 GiB shows that (a) fixed-S normalized costs
    are scale-invariant (`1 + R·D/s`), (b) all frozen switching strategies have
    negative relative gains on the grid (max −0.0007 % / −0.3579 % / −1.7867 %)
    because the frozen trigger fires at a full final burst, (c) the exploratory
    single-switch upper envelope is ≤ 0 on the grid and ≤ +0.3545 % in a dense
    residue scan (≪ 5 %), and (d) relative headroom does not increase with F
    (the bounded tail effect is diluted as 1/F). Outcome:
    `STAGE1_SCALING_ANALYSIS_COMPLETE — ANALYTICALLY INSUFFICIENT UNDER FROZEN
    MECHANISM`. No amendment proposed; no empirical experiment run; protocol
    and prior evidence unchanged.
90. Adaptive-S mechanism pivot screening executed (§22.86, analytical
    simulation only): six candidate mechanisms were screened with a
    deterministic finite-file scheduler simulator under four service models
    and the eight native delivery-opportunity traces. The preferred S does not
    change between stationary states (16 MiB at every rate/regime for the
    shared and lock-in models; fixed 256 KiB for serialized); causal
    within-download switches are zero or harmful (−61.2 % … +0.0 %); the only
    positive opportunity is a fragile, clairvoyant, phase-dependent lock-in
    artifact (0–32 % varying with the volatility period). All candidates are
    `MECHANISTICALLY INSUFFICIENT`; no new preregistered protocol is warranted
    under the screened mechanisms. Zero empirical experiments; frozen v7 and
    all prior artifacts unchanged. Outcome:
    `MECHANISM_PIVOT_SCREENING_COMPLETE — NO PLAUSIBLE MECHANISM`.
91. Adaptive-N mechanism screen executed (§22.87, analytical simulation only):
    eight candidate mechanisms were screened with a deterministic finite-file
    simulator (F = 512 MiB, S = 1 MiB, N ∈ {1,2,4,8,16}) across the
    project's prior concurrency evidence (T1/L3/L4/M3). `direct`/`fair`/
    `fair_bg` are monotone (preferred N = 16); `cap` is a flat plateau with
    zero oracle headroom; only `degrade` (concurrency-dependent server
    capacity) shows a state-dependent interior optimum (preferred N rises
    with C, robust across the whole C × γ grid) with causal gains up to
    +6.66 % at severe drops. It is classified **PLAUSIBLE — FRAGILE** because
    the only live validation of that mechanism class (L4 GATE E) rejected it
    and the current controlled server has no concurrency-dependent cost. No
    mechanism reaches ROBUST, so **no new empirical protocol is justified**
    and no adaptive-N experiment was run. Outcome:
    `ADAPTIVE_N_SCREEN_COMPLETE — NO ROBUST MECHANISM (NO PROTOCOL
    JUSTIFIED)`.
92. Final evidence synthesis / paper-viability audit (§22.88, documentation
    only): the completed evidence was synthesised into six reports (evidence
    matrix, paper-viability audit, figure selection, proposal alignment,
    claims, future-work boundary), seven final figures, and a manifest. The
    final research question was reconstructed (mechanism-first causal
    headroom), the strongest defensible contribution identified (headroom-first
    methodology), the evidence-type discipline enforced (live / replay /
    analytical / simulation / diagnostic), RL deferred (not abandoned), and the
    aria2 evidence bounded to its tested configurations. Outcome:
    `PAPER-READY — MECHANISTIC / NEGATIVE-RESULT STORY`. Zero new experiments;
    all historical evidence and frozen protocols unchanged.

93. aria2-competitive benchmark executed (§22.89, live experimental): a
    memory-safe logical-server replication of the frozen D2 workload reproduced
    the historical result (20/20, ratio 0.5230 CI [0.5200, 0.5261]), explored
    the full aria2 split frontier (1/2/4/8/16 -> 833/419/210/106/53 s; strongest
    split=16) and a 40-config our-frontier grid (best C=16, 4 MiB), froze both
    configurations, and ran a predeclared fresh 30-pair final block: ours mean
    20.12 s vs aria2 53.56 s, geometric ratio 0.3756 CI [0.3728, 0.3785],
    30/30 wins, p ~ 5e-5, all content-verified. Mechanism: 4 MiB ranges (3840
    vs aria2's 450 x 32 MiB) exploit the phase-locked pacing; controls confirm
    granularity-matched ratio 0.862 and a collapsed constant-rate difference.
    Scope bounded to the frozen D2/512 MiB family; this is now the project's
    primary competitive result. Outcome: `ARIA2_COMPETITIVE_WIN_REPRODUCED`.

94. Joint N+S phase-locked pacing screen executed (§22.90, analytical only): a
    simulator calibrated to the measured D2 evidence (40/40 configs within 3 %,
    aria2 anchor −0.8 %) tested whether a causal controller changing both N and
    S can beat the best fixed configuration across a predeclared two-phase
    regime family (640 -> 2500 KiB/s at T in {1,2,4,8} s). Fixed optimum varies
    only in S (N*=16 always; max wrong-config penalty 7.9 %). All primary
    one-switch causal controllers lose (−16 % to −28 %); the diagnostic probe
    peaks at +1.1 %, the oracle-tuned blind ramp at +3.7 %, the optimistic
    causal bound at +3.4 % — no causal class reaches the 5 % threshold in any
    regime. Predeclared gate FAIL (G2) -> STOP: no preregistration, no empirical
    execution. The fixed 62.4 % aria2 benchmark result is unchanged and remains
    the primary competitive result. Outcome:
    `ANALYTICAL_SCREEN_FAILED — NO CAUSAL JOINT HEADROOM`.

95. Live shared-bottleneck / queueing mechanism gate executed (§22.91, LIVE): a
    real `tc tbf` bottleneck on `lo` (40 Mbit/s, 256 kb queue, MTU 1500) shared
    with iperf3 background traffic (B = 0/0.5C/0.85C) was screened with fixed
    configurations (32 MiB, S=1 MiB, N in {1,2,4,8,16}, 3 reps, 45/45 valid).
    Queueing/RTT/drops/retrans grow with N (RTT 0.04 ms -> 33-86 ms median;
    drops 0-860; retrans 0-576), but the completion time has **no finite
    interior N optimum** (flat at S0; boundary N=16 at S1/S2, where more flows
    win a larger TCP share). Predeclared gate FAIL (G-N1, G-N2) -> STOP: no S
    sweep, no adaptive controller, no aria2 comparison, no confirmatory
    protocol. A calibrated work-conserving queueing model (RMSE 8.2 %) predicts
    the same pattern. Outcome: `NO_LIVE-GROUNDED_JOINT_N+S_OPPORTUNITY`. The
    fixed 62.4 % aria2 benchmark remains the primary competitive result.

96. Correlated loss / range-retry mechanism screen executed (§22.92, analytical
    only): a deterministic two-state Gilbert–Elliott loss simulator with the
    frozen retry semantics showed a material **S-retry amplification** in the
    burst-loss condition (S=16 MiB: 32.7 % per-attempt failure, 48 % download
    aborts, 169 MiB re-downloaded per download, +36.3 % penalty for using the
    GOOD-optimal S), but the **N dimension is monotone in both loss states and
    all 9 predeclared robustness variants (N*=16 everywhere)**. The joint
    optimum does not move; the analytical gate fails condition 1 -> STOP: no
    live gate, no adaptive runs, no protocol. Outcome:
    `NO_JOINT_N+S_OPPORTUNITY_UNDER_CORRELATED_LOSS`. The mechanism search is
    closed per the task's final stop condition; the fixed 62.4 % aria2 D2 result
    remains the primary competitive result.

97. Track 1 fresh joint N+S D2 benchmark executed (§22.93, LIVE): a predeclared
    20-block × 4-mode benchmark on the exact frozen D2 workload (80/80 valid,
    zero retries) compared fixed (16, 4 MiB), N-only, S-only and joint adaptive
    N+S (existing heuristics, frozen config) against the frozen aria2
    configuration. Joint adapted both N and S in 20/20 runs (median final
    (6, 6 MiB)) but was 2.32× slower than the best fixed config (ratio 2.3161
    CI [2.3023, 2.3289], 0/20 wins) and 12.4 % faster than frozen aria2 (ratio
    0.8759). Mechanism: per-completed-attempt decision cadence + 4 MiB initial
    chunks delay the first actionable decision to ~8.6 s, and the windowed
    per-request throughput signal saturates after the pacing recovery, so the
    ±10 % rule stops firing and the controllers freeze. Classification
    C = `BEATS_ARIA2_ONLY`; the aria2 margin is not adaptation evidence.
    Outcome: `JOINT_ADAPTIVE_BEATS_ARIA2_BUT_NOT_FIXED`. Track 2 (controlled
    N-dependent server throttling) not started; the fixed 62.4 % aria2 D2
    result remains the benchmark anchor.

98. Track 2 N-dependent server mechanism pre-screen executed (§22.94, analytical
    only): the proposal-promised mechanism was reproduced exactly from
    `python/rl/sim_env_v5.py` and evaluated over a predeclared 16-candidate
    degraded family x N {1,2,4,8,16} x the frozen Track-1 S set at F in
    {64,128,256} MiB. **No candidate qualifies**: the strongest candidate moves
    S* (4->11 MiB) and the N argmin (16->8) with large cross-state penalties,
    but the N movement is degenerate (N=8/N=16 tie at the degraded optimum), the
    S* margin is 1 % (timeout-cliff boundary), and the best single fixed config
    is within 10.7 % of the degraded optimum. Structural cause: the failure-based
    concurrency penalty plus the 3-attempt retry limit is an abort cliff, not a
    smooth interior N optimum. Per the frozen stop rule: no live server
    extension, no live gate, no adaptive run, no aria2 comparison. Outcome:
    `PRE_SCREEN_FAILED_NO_QUALIFYING_SCENARIO`.

103. W1-W6 six-workload study completed (§22.100 live grid + §22.101
     adaptive, LIVE): 240 paired runs, 0 invalid. The frozen band selector
     matches the per-workload clairvoyant best fixed on average (geo 0.999
     [0.969, 1.029]) and beats the best single fixed configuration across the
     family single config (geo 0.770; six-cluster CI [0.455, 1.304]; see §22.101); aria2 still wins
     the degraded states via its cap escape. One model-vs-live mismatch (W5,
     retry/k feedback) disclosed; the frozen mapping was not retuned. Outcome:
     `SIX_WORKLOAD_SELECTION_DEMONSTRATED`.

102. W1-W6 six-workload family designed analytically and frozen before any
     live run (§22.100): 13/72 candidates qualify (all t in {4,8}); W2-W6
     selected with model optima (2,4 MiB)/(4,4 MiB) and base-side penalties
     231-579 %; band selector frozen (25 s classification window, levels
     105/210/315 KiB/s). Live fixed grids + adaptive comparisons not started.

101. P1 baseline terminology corrected from raw data and P2 S-only ablation
     executed (§22.99, LIVE): joint vs N-only 10/10 at +5.3 %, S-only alone
     worse than no switch (82.1 s vs 76.7 s); both P1 baselines reported
     explicitly; timeout sensitivity + capped-S precision replication in
     flight.

100. Track 2-B adaptive experiments executed (§22.98, LIVE): the soft-cap
     platform produced two positive adaptive results. P1 (cross-workload joint
     selection): the causal selector beats the best single fixed configuration
     on both workloads (mean +27.9 %, worst +8.2 %), matches the best fixed in
     the healthy state and beats aria2 2.8x there, and beats the healthy-tuned
     fixed by 27 % in the capped state (aria2 wins there via its burst-start cap
     escape). P2 (within-download): the causal joint controller switches to
     (4,4M) when the server degrades and beats the best fixed by 23.4 %
     (geo 0.766, 10/10 wins), with the joint target better than the N-only
     control. Outcome: `JOINT_ADAPTATION_DEMONSTRATED_UNDER_CONTROLLED_SERVER_DEGRADATION`.

99. Exploratory combined-mechanism screen executed (§22.95, analytical,
    EXPLORATORY): a **soft** N-dependent server throttle
    (`rate_eff = min(schedule, C/(k(1+γ(k−1))))`, no hard failures) combined
    with the phase-locked pacing schedule produced the project's first clean
    state-dependent **joint** (N*,S*) movement: T=1 s / C=8 MiB/s / γ=0.05 gives
    no-cap (16, 1 MiB) 18.42 s vs capped (4, 4 MiB) 85.51 s with cross-state
    penalties +35.8 % / +252.9 % and coordinate margins +6.4 % / +5.9 %
    (1 of 27 screened pairs passes the strict rule). This revives the planned
    "B" controlled N-dependent scenario (frozen pre-screen, live server rate-cap
    extension, live gate, causal controller next). No frozen artifact touched.

---

## 25. Important design decisions

- Half-open `RangeTracker` ranges; inclusive `Fetcher`/`Assembler`/metrics;
  explicit conversion in the downloader.
- One request = one contiguous range.
- In-flight ranges are immutable; chunk-size changes are future-only.
- Failed ranges retry in place with original boundaries (rather than
  `Tracker.Fail` re-allocation) to avoid re-splitting under a changed target.
- Chunk size is read lazily at each allocation.
- Exactly one serialized policy runner; no duplicate runners.
- Policies return generic `ActionKind` values; no policy-type branches in the
  downloader; one dimension per action.
- Deterministic rule-based `JointPolicy` before any learning.
- Raw RL `State` + explicit, documented `Features` normalization.
- Finite discrete RL action space; explicit-target `Set` actions reserved and
  unsupported for learning.
- Reward structure/validation defined **and computed** by `RewardCalculator`;
  one explicit additive formula (benefit minus costs); the learner consumes only
  `Reward.Value`.
- `policy.Context` exposes two throughputs: per-request `Throughput`
  (`sum bytes/sum durations`) and wall-clock `AggregateThroughput`
  (`sum bytes/wall-clock span`), both windowed. The reward's throughput
  components use the aggregate one, because the per-request signal is
  action-independent w.r.t. concurrency under server pacing (see §22.5); the
  per-request signal is retained in the State/features. The deterministic
  policies keep reading the per-request `Throughput` and are unchanged.
- Correct credit assignment: `policyRunner.Update` is called with the
  **previous** context/action (the outcome observed now belongs to the action
  chosen before); first decision has no `Update`.
- LinUCB behind the existing `rl.Policy`; it consumes only `State.Features`,
  uses the finite `ActionIndex` space, updates with `reward.Value` only, and
  breaks ties by the lowest `ActionIndex`.
- Generic numerical linear algebra is delegated to a mature library (Gonum)
  rather than maintained in-project; project-specific research/domain logic
  remains project-owned, and the dependency boundary is narrow (§3.1, §3.2).
- LinUCB is an **opt-in fifth mode**: `AllModes()` and `Run`/`RunMatrix`
  defaults remain the four baseline modes, and the saved baseline dataset is
  unchanged; LinUCB is selected explicitly (`RunSelected`, `MatrixConfig.Modes`,
  `-modes linucb`). The learner reuses the existing Adapter/runner/reward path —
  no new downloader, runner, worker pool, or reward.
- Network-unavailable runs excluded from valid aggregates.

---

## 26. Known caveats

- The origin server is a controlled synthetic HTTP Range server, not a real CDN
  or origin.
- Network conditions use `tc`/`netem` on a specific interface; in the current
  container this needs privileges (`sudo -n`), and shaping affects that
  interface's traffic (loopback is preferred for local httptest experiments).
- Wall-clock duration/throughput vary run-to-run; the `loss` scenario in
  particular shows high variance.
- `JointPolicy` is deterministic; its alternation can be masked by bound
  fallbacks.
- The reward is the **current baseline (Candidate C: absolute + improvement
  throughput; state-change `ActionCost`)**: weights/normalizers are explicit and
  configurable but not empirically tuned or scientifically validated. With
  `RewardConfig == nil`, the downloader still passes `0`, so LinUCB run through
  the default config would learn from a zero signal.
- Fairness is deliberately absent from the reward because no fairness signal is
  measured; `ActionCost` is a churn cost, not fairness.
- LinUCB is available as an opt-in fifth mode, has a calibration/diagnostics
  workflow, and was included in the first formal 5-mode benchmark (§22.6).
  Because it made no controller changes there, **no meaningful learned-policy
  performance evaluation exists** yet.
- **Observability:** the pre-audit per-request throughput could not expose the
  effect of concurrency; this was corrected by adding the wall-clock
  `AggregateThroughput` observable (§22.5), which the benchmark confirms responds
  to overlap.
- **Exploration:** the §22.7 diagnostic proved the `ActionNone=100 %` lock-in was
  **B/A/C/F** (exploration/init-tie/reward-scale/credit), not a
  concurrency-observability defect, and it is now **corrected** (§22.8) by an
  optional deterministic warm-up (`InitialExplorationRounds`, set to 1 in the
  experiment). The 4 MiB Benchmark v3 (§22.11) then showed **no meaningful
  post-warm-up evolution**: after the warm-up, LinUCB locked onto a single arm
  and never changed concurrency. So the warm-up fixes "never tried", but the
  current reward-scale/disjoint-UCB interaction still yields a stable
  single-arm lock; this is **not** an observability defect and was **not** tuned.
- **Remaining, documented, not fixed:** the harness models no chunk-size
  performance tradeoff, so chunk size remains unidentifiable from throughput
  (§22.7 cause **E**); and credit assignment stays approximate under
  concurrency.
- Credit assignment is **approximate under concurrency > 1** and for chunk size
  while old-size requests are in flight, because each decision's reward is
  computed from the next completed attempt (§22.5).
- **Warm-up vs decision budget:** with `InitialExplorationRounds=1`, the first
  five selections are forced. The locked 256 KiB benchmark yields only ~2 learned
  decisions (§22.9); the selected 4 MiB horizon yields 32–122 learned decisions
  (§22.11). The horizon still varies by scenario because chunk growth shrinks the
  number of ranges, so decision counts must be reported, not assumed.
- An **aria2c external baseline** has been measured (§22.16); **IDM is not
  evaluated** (Windows-only, no Linux measurement path). Configuration
  differences (aria2's 1 MiB minimum split vs the internal 32 KiB chunk) make the
  comparison descriptive, not a ranking.
- **Documentation/code comment drift:** the doc comment on
  `downloader.Config.Policy` still says "only concurrency actions are applied",
  but the actual `policyRunner.applyAction` applies both concurrency and
  chunk-size actions (and ignores reserved Set actions). The behavior in §11 is
  authoritative; the comment is stale. A future agent may correct the comment
  when touching that file, but no code behavior is wrong.

---

## 27. Current next step

LinUCB (the first learner) and the first `RewardCalculator` are implemented and
unit-tested behind `rl.Policy` and `policyRunner` respectively (§20.6, §20.7,
§21, §22). The architecture is unchanged:

```
Downloader
  -> existing Policy interface
  -> RL Adapter
  -> LinUCB
  -> ActionIndex
  -> existing policy.Action
  -> existing policyRunner (computes reward, correct credit assignment)
  -> existing controllers
```

LinUCB is integrated as the **opt-in fifth experiment mode** (§16.1), the full
causal loop is validated by deterministic integration tests, a diagnostic
calibration workflow exists (§22.1–§22.5), **Formal Benchmark v1** (pre-correction)
is frozen (§22.6), the **LinUCB Exploration Diagnostic** (§22.7) explained the
`ActionNone=100 %` result, the **LinUCB Exploration Correction** (§22.8) is
implemented, **Formal Benchmark v2** and the **learning-horizon study**
(§22.9–§22.10) are complete, **Formal Benchmark v3** at 4 MiB has been run
(§22.11), the **post-warm-up lock-in has been diagnosed** (§22.12), the
**periodic exploration correction** is implemented and evaluated (§22.13), the
**live standard-vs-periodic benchmark** is complete (§22.14), the **final
six-mode 4 MiB benchmark** has been run (§22.15), an **aria2c external baseline**
has been measured (§22.16), the **request-granularity sensitivity study**
(§22.17) is complete, the **wall-clock throttle benchmark** (§22.18) has
been run, and the **aria2 wall-clock + granularity-matched controls** (§22.19–
§22.20) are complete. The architecture is unchanged.

Final benchmark outcome (§22.15): 300/300 valid. Placed beside the four
deterministic controllers, periodic LinUCB increases action coverage and changes
controller state but does not change the dominant arm preference in most
scenarios; performance differences are descriptive only. Across v3–v5, standard
LinUCB locks onto one arm, and deterministic periodic exploration moves the
posterior without generally changing the preference. **No winner or superiority
has been established.** The aria2 baseline (§22.16) measured 4 concurrent
connections with fixed 1 MiB splits and no within-run adaptation; the
granularity study (§22.17) showed a matched internal 1 MiB/4C config reproduces
aria2 closely in 4/5 scenarios (0.85–0.97×), substantially reducing — but not
eliminating — the request-granularity confound (latency retains ~2.3× unexplained
aria2 overhead). The wall-clock throttle benchmark (§22.18) removed request count
as the rate trigger (transition at ≈1.04 s elapsed in every mode) and showed
that **periodic LinUCB now prefers `increase-concurrency` and reaches
concurrency 3.90**, matching concurrency-adaptive, while standard LinUCB still
locks onto a no-op; the preference change is environment/schedule-dependent.

The current RL evaluation is **complete for the specified controller**, the
external aria2 baseline is complete, the granularity confound is largely
characterized, and the wall-clock throttle study is complete. The genuinely
remaining items are separate research questions, not tuning:

- **Preference lock-in design study:** the live/six-mode/request-count benchmarks
  show coverage and posterior movement are insufficient to change which arm
  dominates in most scenarios, but the wall-clock study (§22.18) shows the
  preference is environment/schedule-dependent (periodic LinUCB chose
  `increase-concurrency` there). Any mechanism change must be a falsifiable,
  versioned experiment (own dataset; all frozen datasets preserved) and must not
  tune alpha/reward/features to a desired outcome.
- **Wall-clock throttle follow-ups:** applying the wall-clock scenario to the
  other four impairments, or a longer/multi-transition schedule, are separate,
  explicitly-scoped experiments (not run here). The aria2 wall-clock baseline is
  now done (§22.19).
- **Chunk-size tradeoff modelling:** the harness models no meaningful chunk-size
  performance tradeoff, so chunk adaptation cannot be evaluated until one is
  added (a deliberate environment change with its own justification).
- **Credit assignment under concurrency:** one-step reward attribution remains
  approximate; redesigning it is a separate task.
- **Fairness:** no measurable fairness signal exists yet.
- **IDM baseline:** requires a separate Windows measurement; no Linux path.
- **aria2 latency overhead:** the residual ~2.3× latency gap is unexplained by
  granularity/concurrency and would need a separate, more invasive trace.
- **Wall-clock throttle benchmark:** the experiment server now supports a
  wall-clock rate schedule (§14.1); §22.18 ran it, and §22.19–§22.20 added the
  aria2 and granularity-matched controls. Still open (not run): applying the
  wall-clock schedule to the other four impairments.

**Status: the internal experimental campaign is complete and the evidence chain
has been synthesized and audited (§22.21).** The **sequential RL phase has
begun** (§22.22): a Double DQN controller/training architecture is implemented and
sanity-validated. LinUCB remains the retained contextual-bandit baseline. **No
performance claim** is made for the DQN; the stored model is a development/sanity
model, the training simulator is not the final test, and no aria2 comparison has
been run.

The **final head-to-head has been completed (§22.26) and the outperformance claim
was NOT supported**: under the locked 8-condition unstable suite, the frozen DQN
Candidate v2 was ~75.7 % slower than the frozen aria2 baseline (geometric ratio
1.757, 95 % CI [1.669, 1.850]), faster in only 1/8 conditions and 31/240 pairs.
All four pre-specified criteria failed. The experiment and conditions were not
altered to rescue the claim.

**Status:** the empirical campaign is complete for its original framing. The
supported write-up scope is now: the controlled synthetic-server evaluation of
§22.21, the DQN architecture/validation of §22.22–§22.24, the fairly-chosen
aria2 baseline of §22.25, and the **negative** final result of §22.26. The
outperformance claim must **not** appear in any write-up.

The **v3 request-efficiency correction (§22.27)** addressed the fragmentation
cause but produced a **mixed** result: it fixed request efficiency yet regressed
the concurrency adaptation that v2 had, because the v3 simulator over-favoured
large chunks. Candidate v3 is therefore an **experimental** formulation, not a
validated candidate.

The **v4 adaptive-concurrency formulation (§22.28)** is the clean final RL
formulation: chunk pinned at 1 MiB, concurrency-only learned control, and a live
transfer that matches aria2's request granularity (4 × 1 MiB) while holding
concurrency 3. Candidate v4 is the candidate intended for a future, explicitly
scoped head-to-head.

The **next genuinely remaining work** (not started; explicitly scoped) is the
**final head-to-head of frozen Candidate v4 against the frozen aria2 baseline**,
run with the same rigor as §22.26 (locked matrix, paired design, pre-specified
criteria, no post-hoc changes), with the v4 model evaluated at **1 MiB chunks**
so the remaining algorithmic difference is **learned adaptive concurrency vs
aria2's fixed concurrency**. It must (a) use fresh, clearly-versioned datasets,
(b) keep all frozen artifacts and prior candidates/results, (c) not tune v4 or
aria2 after seeing results, and (d) report honestly whether the pre-specified
outperformance criteria are met (the §22.26 negative result stands as history).
Real-network, chunk-size-tradeoff, learned-exploration, fairness, and IDM work
remain separate projects. **No outperformance claim against aria2 is supported by
any current evidence.**

**Update (current experimental state):** the frozen external baseline is now
**FINAL ARIA2 BASELINE CONFIG v2** (candidate F: 16 connections / split 16 /
1 MiB minimum split; `testdata/diagnostics/aria2_calibration_v2/`, SHA-256
`b7249817…`), selected on a 16 MiB evaluation scale; config v1 is retained as
history. The active experimental formulation is **DQN Candidate v5**
(concurrency-only, state spec version 2, 16 MiB default horizon, fixed 1 MiB
chunk, decision interval candidates {1,2,4}); it is **not frozen**, no v5 model
artifact exists, and no v5 training/validation/live-transfer/final comparison
has been run. The v5 simulator/state/live contract was audited and corrected
(§22.29), and the v5 training/validation pipeline has now been run (§22.30): one
experimental Candidate v5 model exists (SHA-256 `3a927a58…`, reproducible) and
passes the pre-specified constant/ramp NOT-READY gate, but it is generally
slower than the best fixed concurrency on the held-out simulator conditions. The
v4 head-to-head described above has **not** been run. The **live transfer** of
Candidate v5 on the real controlled downloader (16 MiB, 1 MiB fixed chunk) has
now been completed (§22.31): 125/125 valid, pre-specified gates pass, v5 tracks
fixed-C3 but is slower than fixed-C4 and the observed adaptation is shallow.
The **final head-to-head of Candidate v5 against the frozen aria2 v2 baseline
has now been completed (§22.32)**: under the reused methodology and the
mandatory 4-vs-16 concurrency-asymmetry caveat, v5 was slower in all 8 tested
conditions (pooled geometric ratio 2.3217, 95 % CI [2.2269, 2.4206]; v5 faster
in 1/240 pairs and 0/8 conditions). No outperformance claim is supported. The
empirical campaign for this framing is complete; remaining items (real-network
deployment, chunk-size tradeoff, learned exploration, fairness, IDM) are
separate projects.

**Candidate v6 update (§22.33–§22.34):** a new experimental study with
concurrency 1–16 under a shared time-varying bottleneck has been pre-registered,
trained, held-out validated, and live-validated. All four pre-registered gates
(A environment, B training, C condition-dependence, D live transfer) pass; v6's
behavior is genuinely phase-responsive (it lowers/moderates concurrency under
shared-capacity degradation and holds high concurrency under server throttling).
However, on the live controlled server v6 was slower than the best fixed
concurrency in every tested trajectory, because the live server's independent
per-response pacing still rewards high concurrency. The pre-registered frozen
aria2-F comparison is the next stage; it has NOT been run and no superiority
claim is supported.

**Final v6-vs-aria2-F update (§22.35):** the pre-registered comparison has now
been run — 150 matched pairs, 300/300 valid. v6 was slower pooled (geometric
ratio 1.456, 95 % CI [1.300, 1.630]) and on A2/B2/C2/D2; it was ~5.2 % faster on
E2 (Holm p = 0.128, not significant). The stated RL-under-instability hypothesis
is **not supported** by this controlled experiment. No further training or
design iteration is performed in this session; any next experiment must be
separately designed and documented.

**Diagnostic update (§22.36–§22.37):** the long-duration diagnostic showed
startup amortization but a growing disadvantage on A2/E2; the follow-up
replication reproduced the D2/512 MiB reversal (20/20 pairs, ratio 0.5816) and
explained it as a request-granularity × phase-locked-pacing interaction, not
generic adaptive superiority. A new 7-phase long multi-transition trajectory
(L1) has different per-phase preferred concurrency ({1,2,3,5,16}) but only a
~4 % full-run completion-time spread at 512 MiB, so a v7 design is not yet
justified by this environment; the next step would be to strengthen the
long-duration environment (larger per-phase concurrency sensitivity) before any
v7 training.

**L2 environment-design update (§22.38–§22.39):** the stronger L2 design was
pre-registered, swept, and adjudicated: per-phase optima differ substantially
(best C [16,2,4,16,2,6,16]; C16 up to 62 % worse in degraded phases), but the
**achievable** full-run headroom under the frozen ±1/one-decision-per-request
action semantics is ≈0 (−0.3 % ramp-aware oracle vs best static C16). In
addition, the installed aria2 1.37.0 cannot provide ~1 MiB ranges with ≤16
connections at 512 MiB, so a granularity-matched long-duration aria2 baseline
cannot currently be constructed. Gates: **NOT_READY_ENVIRONMENT_TOO_WEAK** and
**NOT_READY_ARIA2_GRANULARITY**. No v7 training is justified; any next step must
first strengthen the byte-weighted concurrency tradeoff (or change control
authority as a deliberate v7 formulation decision) and resolve the
granularity-matched baseline question.

**v7 design update (§22.40–§22.41):** changing the action space to ±4 steps
(selected from the A/B/C candidates) roughly doubled achievable headroom versus
L2 (≈0 → 8.1 %) but still missed the pre-registered ≥10 % threshold on the new
byte-weighted L3 trajectory, so the pre-registered gate outcome is **STOP**:
v7 training is not justified until a future, separately pre-registered
environment creates a larger byte-weighted concurrency penalty. The runner's
additive step semantics (Value as step, Value ≤ 0 unchanged) and its contract
test are retained; no v7 model exists and no v7-vs-aria2 comparison was run.

**L4 final update (§22.42–§22.43):** the byte-weighted L4 environment reached
17.6 % action-constrained simulated headroom (≥10 % gate), but a small
representative live fixed-C sweep showed that the decisive degraded regime does
**not** penalize excessive concurrency live (C16 ≈ C8 > C4), while the
high-capacity and recovery regimes did match. The pre-registered outcome is
**`NOT_READY_LIVE_MISMATCH`**; the simulated opportunity rests on a
simulator-only mechanism and the byte-weighting rule (≥50 %) was narrowly
missed (47.1 %). **Another RL training iteration is not justified** until a
live-reproducible, physically honest mechanism makes excessive concurrency
genuinely expensive. All frozen candidates, datasets, and findings are
preserved.

**T1 update (§22.44–§22.45):** the live per-request server-throttle mechanism
was pre-registered and measured. Pacing persistence is real live (GATE A), but
with equal 1 MiB requests and independent per-response pacing there is no
concurrency reversal: C4–C16 finish before the throttle at normal 8 MiB/s and
aggregate throughput scales monotonically with C, so the D2 benefit is confirmed
to be a request-granularity effect rather than an adaptive-concurrency
opportunity. Outcome `NOT_READY`; no RL training is justified by the current
live environment. Any future stage must find a live-reproducible cost of
concurrency or explicitly reframe the research question.

**T2 update (§22.46–§22.47):** adaptive **range size** was tested live as the
alternative control dimension at fixed C=16. The 16 MiB chunk was fastest
(184.2 s vs 245.7–267.0 s), because requests started in fast phases retain the
fast rate across the throttles and bypass them; smaller chunks keep sampling
the slow rate. There is no small-vs-large preference reversal, so adaptive
range size has no exploitable headroom here (outcome
`NOT_READY_NO_CHUNK_TRADEOFF`). No RL training is justified by either the
concurrency or range-size direction in the current live environment; the D2
512 MiB result remains a request-granularity artifact.

**M1 update (§22.48):** the real-mobile-trace stage stopped at its
data-acquisition prerequisite: no real mobile/cellular trace exists in the
repository, and synthesizing one is forbidden, so the outcome is
`NOT_READY_NO_REAL_MOBILE_TRACE`. Real public sources are reachable, so the
next viable step is a dedicated stage to acquire ≥5 documented real mobility
traces, canonicalize them, build and validate a trace replayer, and only then
repeat the pre-registered fixed-C/temporal-C* analysis. No RL training and no
aria2 comparison are justified before that chain is established.

**Early-replay corrective update (§22.63, §22.64):** the §22.62 root cause has
been fixed in code and tests. The child now exits non-zero on fatal downloader
errors and persists `fatal`/`err`; `SUCCESS_TRACE_REACHED_EOF` requires a
normal mm-link exit, no fatal child error, **and** a downlink-log endpoint
within the frozen 100 ms `AlignmentTolerance` of the `.down` trace duration
(new outcomes `DOWNLOADER_FATAL_ERROR`, `TRACE_ENDPOINT_NOT_REACHED`); the
pilot resolves `<family>.up`/`<family>.down` pairs and rejects
missing/mismatched pairs; the pilot request timeout is explicit `-timeout`
(default 120 s) with the 45 m watchdog unchanged.

**The two native diagnostics ran and passed (§22.64, 2026-09-26):** EVDO C=1
(17 m 45 s) and LTE C=16 (22 m 47 s) both reached `SUCCESS_TRACE_REACHED_EOF`
with `endpoint_valid=true`, `mm_link_exit=0`, child status `canceled`, no fatal
error, endpoint residual −35 ms, and complete downlink delivery. Outcome:
`NATIVE_DIAGNOSTICS_PASS — READY FOR CORRECTED PILOT`.

**The corrected formal pilot then ran and is INVALID (§22.65, 2026-09-26):**
the 10-run matrix was launched in `/home/vscode/epoch_pilot_corrected`. EVDO
C=1 and C=2 completed valid trace EOF; EVDO C=4, C=8 and C=16 died early with
fatal `retry limit exceeded` after 2 attempts (trace times 512.3 s / 240.1 s /
150.0 s of 1 062.0 s). The gate correctly rejected them
(`DOWNLOADER_FATAL_ERROR`, `endpoint_valid=false`), the pilot was stopped per
the pre-registered stop rule, and the remaining LTE runs were not completed.
No epoch extraction, shared validity, coverage, duration selection, oracle,
tau, or headroom was computed. Outcome: `BLOCKED — FORMAL EPOCH PILOT INVALID`.
`EPOCH_DURATION_BLOCKED` still stands; no duration is frozen.

**Mechanism diagnosed (§22.66, 2026-09-26):** the EVDO early deaths are an
unintended **C-dependent censoring** effect of the formal 120 s per-request
timeout under the shared low-rate link. Frozen artifacts show the C8 death at
exactly 2×120 s and the C16 death at 120 s + the fixed 30 s transport dial
timeout; a controlled 120 s vs 600 s sweep on EVDO C=4/8/16 showed C4 and C8
reaching genuine trace EOF at 600 s (4 and 24 completed requests had needed
>120 s), while C16 at 600 s still died (600 s request timeout at ≈940 s
followed by a 30 s retry dial timeout at ≈971 s). Status:
`TIMEOUT_CENSORING_SUPPORTED`. The formal 120 s timeout is therefore **not
safe for the fixed-C EVDO measurement**; the pilot revision rule only permits
candidate-list and byte-floor changes, so this is outside its scope.

**Timeout policy implemented (§22.67–§22.68):** the formal replacement is the
**watchdog-only** policy — no HTTP request deadline and no connection dial
deadline, with the 45 m parent and child watchdogs as the only time bounds
(trace EOF remains the measurement endpoint) and retry semantics unchanged.
It is implemented as protocol version 2 (`epoch.DefaultProtocol()`), explicit
`NoRequestTimeout`/`NoDialTimeout` configuration in `internal/fetcher`/
`internal/downloader`, and pilot `-timeout`/`-dial-timeout` flags defaulting to
`none`; the historical version-1 120 s/30 s policy remains representable via
`epoch.LegacyProtocolV1()` and the diagnostic CLI overrides. Synthetic tests
and the full validation suite pass, and a bounded EVDO C=1 smoke with no
deadlines ended in `WATCHDOG_TIMEOUT` with clean teardown. No full pilot has
been run yet.

**Formal v2 pilot completed (§22.69, 2026-09-27):** the fresh 10-run
watchdog-only matrix in `/home/vscode/epoch_pilot_v2` finished with all 10
runs valid trace EOF (residuals −35…−58 ms, no fatal error, no workload
exhaustion, `-timeout none -dial-timeout none` confirmed per run). Epoch
extraction and shared validity completed; only the **120 s** candidate passed
the frozen coverage rule (aggregate survival 85.7 %, min-trace 7 shared-valid
epochs) and was selected (`2m0s`); 30/60/90 s are reported unsuitable under
the existing thresholds and were not tuned.

**Oracle stage blocked (§22.70, 2026-09-27):** the frozen v2 input was
verified (protocol v2, selected 120 s, 21 candidate / 18 shared-valid / 3
excluded epochs; EVDO 7, LTE 11) and preserved under
`/home/vscode/oracle_stage_v2/`, but formal headroom could **not** be
computed: the repository has no pre-registered tau regime classifier/
threshold, no tau (transition-cost) measurement design or frozen measurement,
and no completion-time/workload mapping — exactly the prerequisites §22.52
records as not frozen. Outcome: `BLOCKED — ORACLE PREREQUISITE UNRESOLVED`
(specific gate: `BLOCKED — TAU REGIME RULE NOT PRE-REGISTERED`).

**Tau pre-registered and measured (§22.71, 2026-09-27):** the oracle
objective, native-regime classifier, 2.0 regime threshold, injection selection,
and tau aggregation are frozen in `testdata/oracle_preregistration_v2/`. The
72-run switch-injection campaign completed 72/72 valid; all eight directions
have aggregated tau (only 1→2 regime-specific; pooled |tau| ≤ 0.57 s
elsewhere). No headroom was computed.

**Formal oracle analysis executed (§22.72, 2026-09-27):** the frozen DP
computed fixed-C completion equivalents, the instantaneous diagnostic, and the
constrained oracle with persisted sequences. Descriptive two-trace headroom:
EVDO 1.61 %, LTE 0.44 % (nonnegative-tau sensitivity: 1.58 % / 0.047 %;
zero-tau: 1.64 % / 0.17 %). The task's pre-registered sanity bound
`T_instantaneous <= T_constrained` fails for LTE because the frozen tau rule
preserves negative measured taus; the DP was verified against exhaustive
enumeration and no value was clamped. Status:
`BLOCKED — ORACLE COMPUTATION INCONSISTENT`.

**Nonnegative formal tau ready (§22.73, 2026-09-27):** oracle protocol
version 2 (`testdata/oracle_preregistration_v3/`) defines
`tau_formal = max(0, estimator)` after the unchanged median/regime rule, with
raw signed observations preserved. The fresh v3 campaign
(`/home/vscode/tau_campaign_v3/`, same design/timestamps, plan hashes matching
v2) completed 72/72 valid; formal tau is ≥ 0 for all eight directions
(2→1 and 8→4 floored; no regime-specific directions in the fresh data).
Headroom was not recomputed.

**Formal v3 oracle complete (§22.74, 2026-09-27):** with the nonnegative
formal tau table, all sanity inequalities hold and the descriptive two-trace
headroom is EVDO 1.6075 %, LTE 0.0320 % (historical signed-tau reproduction:
1.6072 % / 0.4436 %; zero-tau diagnostic: 1.6408 % / 0.1740 %). Artifacts are
byte-identical across two runs.

**Trace-expansion gate audited (§22.75, 2026-09-27):** the repository does
**not** freeze the final ≥5-trace protocol — the count/threshold exist only as
an aspiration in analysis output/docs, and diversity, admissibility, trace
identities, tau scope, reuse rules, and the statistical formula were UNFROZEN at
audit time; they are now frozen in §22.76 (`testdata/oracle_preregistration_v4/`).
Status at audit time: `BLOCKED — FINAL TRACE PROTOCOL INCOMPLETE` (superseded by
the frozen protocol below); audit artifacts under
`/home/vscode/final_trace_expansion_decision/`.

**Final trace protocol frozen (§22.76, 2026-09-27):** the prospective
pre-registration `testdata/oracle_preregistration_v4/final_trace_protocol.json`
freezes the five-trace population, eligibility, existing replay/oracle rules,
**per-trace tau**, reuse rules, the statistical design (mean H, sample SD,
one-sided 95 % t LCB, df = n−1, pass iff `LCB_95 >= 0.10`), and invalid/
missing handling. Planning artifacts are consistent with the freeze. Status:
`FINAL_TRACE_PROTOCOL_FROZEN — READY FOR FINAL TRACE EXECUTION`.

**Immediate next step:** the frozen five-trace campaign is complete. Stage A
(15/15 fixed-C replays, §22.77), Stage B (88/88 per-trace tau runs, §22.79),
and Stage C (frozen oracle + final gate, §22.80) have all run under the
amended preregistration. The pre-registered gate **FAILED**
(`LCB_95 = −0.0026077 < 0.10`); the five trace-level H values and all
artifacts are in `/home/vscode/final_oracle_analysis/`. No further
experimental or analysis work is planned for this campaign. Any write-up must
use the supported scope (controlled synthetic-server evaluation, DQN
architecture/validation, fairly-chosen aria2 baseline, negative final result)
and must not claim RL/adaptive/aria2 superiority or causal real-world
improvement; remaining items (real-network deployment, chunk-size tradeoff,
learned exploration, fairness, IDM) are separate projects.

**Salvage study (separate, frozen; Stage 0S complete):** the chunk-size-tradeoff
salvage study is specified in `testdata/salvage_s_protocol_v7/`
(§22.81–§22.83) and is now `FINAL_S_PROTOCOL_FROZEN — READY_FOR_STAGE_0S`.
The human froze Candidate A (`R=4 Mbps`, `D=40 ms`, `FileSize=512 MiB`,
`N=4`), the log-scale `delta_log = log(0.95) = −0.05129329438755058`, the
tail definition `T_tail_primary = D + C_max/R = 33.594432 s` (with a noted
0.128 ms transcription difference from the decision text), the trigger
`B_switch = N·S0` = 64/16/4 MiB, the Stage 0S GO/NO-GO rule, and the SSR grid;
the exploratory duplicate was removed and the fixed sequence frozen. **Stage
0S is now complete (§22.84): 32/32 clean and all four criteria PASS → GO.**
The next step is the confirmatory Stage 1 (fresh data; frozen arms,
strategies, trigger, priors, effect threshold, and analysis) as a **separate
task** under the frozen protocol; it has not been started. A GO does not mean
chunk-size adaptation is beneficial — it only justifies the next pre-specified
stage. However, the pre-Stage-1 scaling/headroom analysis (§22.85) now shows
that the frozen design is **analytically insufficient under the frozen
mechanism** at 512 MiB: the frozen strategies' relative gains are negative on
the whole 64 MiB–32 GiB grid (the trigger fires at a full final burst) and even
an exploratory optimally-timed single switch stays ≤ +0.3545 % (≪ 5 %), with
relative headroom not increasing with file size. **No amendment is proposed**
(larger files do not help); proceeding to Stage 1 as frozen would require an
explicit human decision, and any design/mechanism change requires a separately
frozen protocol decision. The follow-up **mechanism pivot screening (§22.86)**
then tested whether a different realistic mechanism could create a moving
chunk-size preference: the preferred S does not change between stationary
states for any screened candidate, causal within-download switches are zero or
harmful, and the only positive opportunity is a fragile, clairvoyant,
phase-dependent lock-in artifact. **All candidates are
`MECHANISTICALLY INSUFFICIENT` and no new preregistered protocol is warranted
under the screened mechanisms**; the frozen v7 protocol remains unchanged and
no Stage 1 run was performed. The follow-up **adaptive-N mechanism screen
(§22.87)** then asked the same question for concurrency: `direct`/`fair`/
`fair_bg` are monotone (preferred N = 16), `cap` is a flat plateau, and only
`degrade` (concurrency-dependent server capacity) shows a state-dependent
interior optimum with causal gains up to +6.66 % — but that mechanism class
was already rejected live (L4 GATE E), so it is **PLAUSIBLE — FRAGILE**. No
mechanism reaches ROBUST and **no adaptive-N protocol is justified** in the
current infrastructure; no adaptive-N experiment was run. The final
**evidence synthesis (§22.88)** consolidated all of the above into the
paper-ready mechanistic/negative-result story: the contribution is a
pre-registered, headroom-first methodology, RL is deferred (not abandoned),
the aria2 evidence is bounded to its tested configurations, and no further
empirical work is required for the negative-result paper. Status:
`PAPER-READY — MECHANISTIC / NEGATIVE-RESULT STORY`. The follow-up
**aria2-competitive benchmark (§22.89)** then asked the bounded positive
question the synthesis left open — can a fixed configuration reproducibly beat
aria2 under the frozen D2 workload? — and answered it: the full aria2 split
frontier (strongest split=16) and a 40-config our-frontier grid (best C=16,
4 MiB) were explored, both configurations frozen before a predeclared fresh
30-pair block, and the final comparison is 30/30 wins at geometric ratio 0.3756
CI [0.3728, 0.3785] (ours 20.12 s vs aria2 53.56 s mean), content-verified. The
mechanism is a request-granularity x phase-locked-pacing interaction (3840 x
4 MiB ranges vs 450 x 32 MiB pieces all held at the 640 KiB/s phase); controls
(granularity-matched ratio 0.862; constant-rate collapse of the gap) bound it.
This is now the project's **primary competitive result**, scoped strictly to
the frozen D2/512 MiB family. Status: `ARIA2_COMPETITIVE_WIN_REPRODUCED`. The
follow-up **joint N+S phase-locked pacing screen (§22.90)** asked whether that
mechanism can be turned into a genuine within-download adaptation result: a
calibrated deterministic simulator (D2 fit within 3 %) evaluated a predeclared
two-phase regime family, the frozen 8-action space, and a frozen (4,4M) start.
The fixed optimum varies only in S (N*=16 always, max wrong-config penalty
7.9 %); every primary causal controller loses (−16 % to −28 %), and the
strongest diagnostic causal variants (probe, oracle-tuned blind ramp,
optimistic causal bound) peak at +1.1 / +3.7 / +3.4 % — below the 5 %
materiality threshold in every regime. The predeclared gate FAILED (no
positive causal headroom) → STOP: no preregistration and **no empirical
execution** (the frozen stop condition). Conclusion: no robust joint adaptation
opportunity under this mechanism; the fixed 62.4 % aria2 result is unchanged
and remains the primary competitive result. Status:
`ANALYTICAL_SCREEN_FAILED — NO CAUSAL JOINT HEADROOM`. The final mechanism
branch, the **live shared-bottleneck/queueing gate (§22.91)**, was then tested
live (real `tc tbf` on `lo` + iperf3 background): the contention cost is
measurable and grows with N (RTT 0.04 ms -> 33-86 ms median, drops 0-860,
retrans 0-576), but the completion time is work-conserving: no finite interior
N optimum (flat at B=0; boundary N=16 under background, where more flows win a
larger TCP share) and no material state-dependent N*. The predeclared gate
FAILED -> STOP: no S sweep, no adaptive controller, no aria2 comparison, no
confirmatory protocol. Status: `NO_LIVE-GROUNDED_JOINT_N+S_OPPORTUNITY`. The
final mechanism branch — **correlated loss + range-retry amplification
(§22.92)** — was then screened analytically (two-state Gilbert–Elliott loss,
frozen retry semantics, grounded parameters + predeclared robustness sweep):
the S-retry amplification is real and material under burst loss (S=16 MiB
fragile: 32.7 % per-attempt failure, 48 % download aborts, +36.3 % penalty),
but the N dimension is monotone in both loss states and all 9 variants
(N*=16 everywhere), so the joint optimum does not move. The analytical gate
failed condition 1 -> STOP: no live gate, no adaptive execution, no protocol.
Status: `NO_JOINT_N+S_OPPORTUNITY_UNDER_CORRELATED_LOSS`. Per the task's final
stop condition the mechanism search is **closed**; the fixed 62.4 % aria2
benchmark remains the project's primary competitive result and is unchanged.
The subsequent **Track 1 fresh joint N+S D2 benchmark (§22.93)** then tested
whether the *existing* adaptive policies add value on the already-demonstrated
D2 mechanism: 20 blocks × {fixed, N-only, S-only, joint}, 80/80 valid. The
joint policy adapted both N and S in 20/20 runs but was 2.32× slower than the
best fixed (16, 4 MiB) and 12.4 % faster than the frozen aria2 reference; the
mechanism analysis shows the per-completed-attempt decision cadence and the
saturating per-request throughput signal freeze the controllers at (6, 6 MiB).
Classification C = `BEATS_ARIA2_ONLY`; the adaptation-value test failed and the
aria2 margin is not adaptation evidence. The subsequent **Track 2 pre-screen
(§22.94)** then reproduced the proposal-promised N-dependent server throttling
exactly from `python/rl/sim_env_v5.py` and evaluated the predeclared
16-candidate degraded family over the full grid at three workload scales:
**no candidate qualifies** — the strongest moves S* to the timeout-cliff
boundary and the N argmin only degenerately (N=8/N=16 tie), because the
failure-based concurrency penalty plus the 3-attempt retry limit is an abort
cliff rather than a smooth interior N optimum. Per the frozen stop rule no live
server extension, live gate, adaptive run, or aria2 comparison was performed.
Status: `PRE_SCREEN_FAILED_NO_QUALIFYING_SCENARIO`. The fixed 62.4 % aria2 D2
benchmark remains the project's primary competitive result.
The **Track 2-B soft-cap platform (§22.95-§22.98)** then produced the project's
first clean joint-adaptation result under a live N-dependent server mechanism:
the frozen live gate passed on N*/S* movement and cross-state materiality
(+31.8 % / +254.4 %) with the documented capped S-margin caveat (3.8 % < 5 %),
and the causal joint controller reduced completion time 23.4 % vs the best
stationary fixed configuration (geo 0.766, 10/10), while aria2 wins in the
capped state via its burst-start cap escape (the honest reversal). Per the
remaining-month plan (2026-10-08), P1 is now reported with both baselines
explicitly (§22.99), the S-only ablation confirms the joint target (N carries
the gain; S adds a consistent 5.3 %; S-only alone hurts), and a six-workload
family was designed analytically and frozen as
`manifest/w1_w6_manifest.json` (§22.100) **before any live W1-W6 run**. The
six-workload study is now complete: the live grid validated 5/6 model optima
(the W5 mismatch is disclosed, not retuned) and the frozen band selector
matches the per-workload clairvoyant best on average (geo 0.999 [0.969, 1.029])
while the pooled ratio vs the family single fixed configuration is 0.770
(six-cluster CI [0.455, 1.304]; not robust across workloads, see §22.101) — a **(B)** workload-selection result over
**six predeclared controlled workloads** (§22.100-§22.101,
`reports/w1_w6_report.md`, figures `figW1..figW4`). The timeout sensitivity
((4,4M) invariant at 20/30/60 s) and the capped-S precision (margin 3.74 %,
CI [3.09, 4.45], criterion unchanged) are folded into
`reports/track2b_soft_cap_report.md`. The remaining work is the manuscript
(IEEE Access primary; mechanism-dependent-granularity thesis): update
`reports/paper_draft.md` with the six-workload section (done), harmonize
figures, and refresh `docs/team_status_2026-09-29.md`. mm-link trace anchoring
remains low priority unless time permits.
Historical artifacts (`/home/vscode/epoch_pilot`,
`/home/vscode/epoch_pilot_corrected`, `/home/vscode/evdo_timeout_diag`,
`/home/vscode/tau_campaign_v2`) remain untouched and must not be reused for
selection; `/home/vscode/epoch_pilot` is untouched forensic evidence.

First files to inspect: `testdata/final_evaluation/` (frozen negative result),
`testdata/diagnostics/dqn_validation_v4/` (Candidate v4),
`testdata/diagnostics/dqn_validation_v3/` (v3 experiment),
`internal/rl/dqn/` (§22.22–§22.24, §22.28), `python/rl/`,
`python/train_dqn.py`, `python/train_validate.py`, `python/train_validate_v2.py`,
`python/train_validate_v3.py`, `python/train_validate_v4.py`,
`python/rl/sim_env_v4.py`, `python/train_validate_v5.py`,
`python/rl/sim_env_v5.py`, `python/rl/state_v5.py`,
`testdata/diagnostics/dqn_validation_v5/`,
`cmd/dqn-live-v5/`, `testdata/diagnostics/dqn_validation_v5_live/`,
`cmd/final-evaluation-v5/`, `testdata/final_evaluation_v5/`,
`python/report_final_evaluation_v5.py`,
`testdata/diagnostics/dqn_validation_v2/` (frozen Candidate v2),
`testdata/diagnostics/dqn_validation/` (historical Candidate v1),
`testdata/diagnostics/dqn/`, `cmd/dqn-transfer/`,
`testdata/diagnostics/aria2_calibration/` (frozen aria2 baseline),
`cmd/aria2-calibration/`,
`testdata/final_research_audit/` (audit artifacts),
`docs/PROJECT_CONTEXT.md` §22.21, `internal/rl/linucb.go`,
`internal/experiment/matrix.go` and `experiment.go`, `cmd/experiment/`,
`cmd/aria2-benchmark/`, `cmd/aria2-wallclock/`, `cmd/wallclock-throttle/`,
`cmd/granularity-study/`, `testdata/benchmark_v6/`, `testdata/benchmark_aria2_v2/`,
`testdata/diagnostics/wallclock_throttle/`, `testdata/diagnostics/aria2_wallclock/`,
`testdata/diagnostics/request_granularity/`,
`testdata/diagnostics/request_granularity_wallclock/`,
`cmd/epoch-pilot/`, `internal/experiment/pilot/`,
`internal/experiment/replay/`, `internal/experiment/epoch/` (§22.59–§22.63),
`testdata/diagnostics/linucb_lockin/`,
`testdata/diagnostics/linucb_periodic_exploration/`.

---

## 28. How to continue from another client

1. Read `AGENTS.md`.
2. Read this file (`docs/PROJECT_CONTEXT.md`).
3. Inspect relevant source and tests; treat them as authoritative.
4. Run the appropriate test suite before major changes.
5. Update this document after substantive work
   (`PROJECT_CONTEXT.md updated: yes`).

Useful commands (repository root):

```bash
go test ./...
go test -race ./...
go test -count=5 ./...
go vet ./...
gofmt -l .
go build ./...
```

Single-scenario experiment (four modes; flags verified via
`go run ./cmd/experiment -h`):

```bash
go run ./cmd/experiment -reps 2 -file-size 262144 \
  -chunk 32768 -min-chunk 32768 -max-chunk 131072 \
  -initial-chunk 32768 -chunk-step 32768 \
  -concurrency 1 -min-concurrency 1 -max-concurrency 4 \
  -initial-rate 524288 -changed-rate 4194304 -switch-after 4 \
  -improve 0.2 -degrade 0.2 -window 4 -retries 3
```

Matrix (add `-sudo` for tc/netem in this container; `-json` for machine-readable
output; `-file-size` overrides every scenario's file size, default matches
`DefaultMatrixConfig`):

```bash
go run ./cmd/experiment -matrix -sudo -reps 10
go run ./cmd/experiment -matrix -sudo -reps 10 -json > matrix.json
go run ./cmd/experiment -matrix -sudo -reps 10 -json -file-size 4194304 > matrix_4MiB.json
```

Mahimahi replay harness smoke (§22.59, §22.63; synthetic traces only, sparse
output, mandatory watchdog). Since §22.63, `--downlink-log` evidence is
required for `SUCCESS_TRACE_REACHED_EOF`: without it the outcome is
`TRACE_ENDPOINT_NOT_REACHED` (by design, never EOF):

```bash
go run ./cmd/mm-replay -up /tmp/up.link -down /tmp/down.link -c 4 \
  -timeout 120s -workload-size 67108864 -chunk 262144 -max-runtime 30s \
  -output /tmp/mm-out.bin -summary /tmp/mm-summary.json \
  -downlink-log /tmp/mm-down.log
```

Opt-in LinUCB mode (deterministic smoke; does not run the research matrix):

```bash
go run ./cmd/experiment -modes linucb -reps 1 -linucb-alpha 0.2
go run ./cmd/experiment -modes fixed,linucb -reps 1
```

LinUCB calibration and reward diagnostics (writes
`testdata/calibration/calibration.json`; flags verified via
`go run ./cmd/calibration -h`):

```bash
go run ./cmd/calibration -reps 2 -alpha 0.2
go run ./cmd/calibration -reps 1 -json
```

LinUCB exploration diagnostic (separate forced-action study; writes
`testdata/diagnostics/linucb_exploration/report.json` + `.txt`; `-sudo` only for
the loss scenario's tc/netem):

```bash
go run ./cmd/linucb-diagnostic -reps 5 -sudo
```

LinUCB learning-horizon study (separate; varies only file size; writes
`testdata/diagnostics/linucb_horizon/report.json` + `.txt`):

```bash
go run ./cmd/linucb-horizon -sizes 262144,524288,1048576,2097152,4194304 \
  -scenarios baseline,server-throttle,latency,combined -modes linucb,fixed -reps 3 -sudo
```

Available experiment flags: `-changed-rate -chunk -chunk-step -concurrency
-degrade -file-size -improve -initial-chunk -initial-rate -json -linucb-alpha
-matrix -max-chunk -max-concurrency -min-chunk -min-concurrency -modes -reps
-retries -sudo -switch-after -timeout -window`. Calibration flags: `-alpha -json
-out -reps`. Diagnostic flags: `-reps -out -sudo -loss`.
