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

