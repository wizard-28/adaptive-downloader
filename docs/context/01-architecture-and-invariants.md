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

