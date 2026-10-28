### 22.112 Documentation pass for the viva code review (2026-10-28, comments and docs only; NO BEHAVIOUR CHANGE)

**Why.** The examiner may point at any line and ask what it does and why. The
code was already commented in places, but the *reasons* behind the unusual parts
(the half-open/inclusive conversion, in-place retries, front allocation, the two
different throughput numbers, the acquisition order of straggler splits, the cap
formula) were spread across the paper, this context file and the tests. This pass
puts the explanation where the code is, and adds one navigable document for the
review itself.

**What changed.**

- `internal/downloader/downloader.go`: package doc now states the control loop,
  how one run works, the range conventions with a worked example (100 bytes,
  32-byte chunks → `[0,32) [32,64) [64,100)`) and the retry/split exceptions. The
  stale "chunk size remains fixed" and "only concurrency actions are applied"
  claims are corrected. `New` documents the controller setup, `Download` its
  outcome order and the `MaxRetries+1` bound, and `runState` why attempts are
  keyed by range start.
- `internal/downloader/worker_pool.go`: the wake/reconcile protocol, why workers
  only retire between ranges, the ordered steps of `splitStraggler` with a
  numeric example, and the per-attempt sequence in `processTask`.
- `internal/downloader/policy_runner.go`: the observe → Update(previous) →
  SelectAction → apply timeline, why the reward belongs to the previous action,
  a worked example of the two throughput measures (four overlapping 1 MiB
  attempts: 0.5 MiB/s per-request vs 2 MiB/s aggregate), and an action table.
- `internal/rangetracker`: allocation-from-the-front rationale and example, the
  `Fail`/merge semantics, and why the worker pool retries in place instead.
- `internal/fetcher`: the four success checks as a list, the `bytes=32-63`
  example, and a `Limit` shrink example.
- `internal/assembler`, `internal/metrics`, `internal/concurrency`,
  `internal/chunksize`: out-of-order write example; why every attempt is
  recorded and how the two throughput statistics differ; `Set` semantics.
- `internal/policy`: `Context` gains a worked reading (including that `ChunkSize`
  is the target for the *next* allocation) and the `Value` semantics of the
  increase/decrease kinds (step magnitude).
- `internal/experiment/server`: `ServeHTTP` now lists the five steps of the
  model, `SetConcurrencyCap` shows the cap at k = 1, 4, 16 (8 MiB/s, 1.74 MiB/s
  per request, 293 KiB/s per request), `SetLiveRate` states what the control
  isolates, and `parseRangeHeader` states the accepted forms with examples.
- `internal/experiment/network`: the exact `tc` command, idempotence, and the
  `ErrNoQdisc` cleanup convention ("nothing to undo" is success).
- `internal/rl`: feature normalization example, exact-inverse action mapping,
  an end-to-end reward example (value 4.75 with the default config), LinUCB
  score/update examples with one arm before and after one update, and the DQN
  greedy tie-break plus the thirteen v6 features with their sources.
- `cmd/downloader`, `cmd/experiment`: what each entry point is and is not.
- New `reports/thursday/code_walkthrough.md`: the map from file/function to
  explanation, the invariants with where each is enforced, one run end to end,
  the controller and learner rules with examples, the server model and its cap,
  how a comparison number is produced, twenty "if they point at this line"
  answers, things that look like bugs and are not, and where each study lives.
- `README.md` points at the walkthrough.

**Verification.** Comments and Markdown only; no code path, threshold, metric or
protocol changed. `go build ./...`, `go vet ./...`, `gofmt -l internal cmd`
(clean) and `go test -p 1 -count=1 ./...` all pass; `python3
tools/claims_check.py` stays at 231/231 with 0 unverified tokens;
`python3 tools/test_claims_check.py` passes. Arithmetic in the examples was
checked by hand against the constants in the code (cap at k = 1/4/16, reward
4.75, LinUCB score 0.112, 1 MiB within [1, 33 MiB] = 0.030); the walkthrough
contains no experimental results, so no new claim anchors were needed.

**Caveat.** Comment accuracy is only as good as this pass: if a later change
alters behaviour, the comment beside it must change too. The invariants listed in
§3 of the walkthrough duplicate AGENTS.md §5 on purpose, so a reader of the code
does not need the agent instructions.
