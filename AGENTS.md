# AGENTS.md

Persistent working instructions for any coding agent (human or AI) contributing to
this repository.

This file describes **how to work** on the repository. It is **not** the project
state document. The current architecture, completed milestones, experiment
results, RL design, known caveats, and the immediate next step live in:

- `docs/PROJECT_CONTEXT.md` — living handoff document (current project state)
- Source code and tests — the final authority on actual behavior

> **Document hierarchy / source of truth**
>
> 1. Actual source code and tests
> 2. `docs/PROJECT_CONTEXT.md`
> 3. `AGENTS.md` (this file)
>
> If documentation conflicts with code or tests, inspect the implementation,
> determine the real behavior, and update `PROJECT_CONTEXT.md`. Never invent
> implementation behavior from documentation alone.

---

## 1. Read this first

Before beginning any substantive work:

1. Read `AGENTS.md` (this file).
2. Read `docs/PROJECT_CONTEXT.md`.
3. Inspect the relevant source files and tests.
4. Treat source code and tests as authoritative if documentation conflicts with
   implementation.

If `docs/PROJECT_CONTEXT.md` does not exist yet, do **not** invent project state.
Inspect the repository (packages under `internal/`, `cmd/`, tests, and
`testdata/`) and work from what is actually present.

---

## 2. Living project context (mandatory)

`docs/PROJECT_CONTEXT.md` is a **living handoff document**. After **every
substantive implementation task**, update it in the same task.

Update it whenever any of the following changes:

- architecture
- APIs
- package responsibilities
- controller behavior
- downloader behavior
- retry semantics
- range semantics
- policy behavior
- experiment behavior
- metrics
- RL state / action / reward design
- research positioning
- completed milestones
- known limitations
- important design decisions
- current status
- immediate next step

Do not let `PROJECT_CONTEXT.md` become stale.

Exception: trivial changes (formatting-only, or a temporary debugging change
that is fully removed) do not require an update.

At the end of every substantive task, explicitly report one of:

```
PROJECT_CONTEXT.md updated: yes
```

```
PROJECT_CONTEXT.md updated: no — no substantive project-state change.
```

---

## 3. Scope discipline

Follow the exact scope of the current task.

- If a task says "do not implement RL", do not implement RL.
- If a task says "only modify experiment infrastructure", do not redesign the
  downloader.
- Do not prematurely implement future roadmap items.
- Do not make unrelated refactors merely because they look desirable.

Prefer the smallest change that satisfies the requested task.

---

## 4. Architectural discipline

Respect the existing architecture and package responsibilities. In particular:

- Do not create duplicate policy runners.
- Keep the existing serialized policy decision path (one decision at a time).
- Policies return generic `Action` values; they do not mutate controllers.
- Downloader/controller code must not contain policy-specific special cases.
- Controllers own controller state and bounds.
- The worker pool owns worker lifecycle.
- `RangeTracker` owns range allocation and range state.
- `Fetcher` owns HTTP range requests.
- `Assembler` owns writing ranges to the output file.
- `Metrics` owns attempt/run measurements.

Do not bypass existing abstractions without a clear, documented reason.

---

## 5. Critical range invariants

Preserve these unless a task explicitly changes them:

- `RangeTracker` uses **half-open** ranges: `[start, end)`.
- `Fetcher` / `Assembler` use **inclusive** ranges: `[start, end]`.
- The **downloader** performs the conversion between the two explicitly.
- One request represents one contiguous byte range.
- An **in-flight range is immutable**.
- Changing the future chunk-size target must **not** resize an existing
  in-flight range.
- A **failed range must retry using its original boundaries**.
- Do not silently re-split a failed range using the current chunk-size target.

---

## 6. Controller independence

Concurrency and chunk size are separate control dimensions.

- A concurrency action must not modify chunk size.
- A chunk-size action must not modify concurrency.
- Chunk-size changes affect **future allocations only**.
- The worker pool must not forcibly resize or terminate an in-flight HTTP
  request merely because concurrency changed.

---

## 7. Policy discipline

- Use the existing `Policy` interface.
- Do not introduce policy-specific branches (for example
  `if policy is JointPolicy { ... }`); the downloader reacts to generic
  `ActionKind` values.
- Keep these concepts separate:
  - deterministic policies
  - the RL policy adapter
  - future learning algorithms
- Do not modify deterministic policies when implementing unrelated RL
  infrastructure.

---

## 8. RL discipline

Keep the RL architecture layered; do not collapse layers:

```
Downloader
    -> existing Policy interface
    -> RL Adapter
    -> RL State
    -> learning algorithm
    -> ActionIndex
    -> existing policy.Action
    -> existing policyRunner
    -> controllers
```

- Do not create a separate RL downloader.
- Do not create a separate RL worker pool.
- Do not duplicate policy execution infrastructure.
- The RL action space currently consists of the supported generic actions only.
- Do not silently introduce arbitrary controller target-setting actions.
- Before treating a learned-policy setup as benchmark-ready, verify that each
  action's intended effect is observable in the RL state and reflected in the
  reward. An observable that an action does not change (for example a
  per-request throughput that is independent of concurrency) cannot be learned
  from, regardless of the algorithm or weights. Fix observability before
  benchmarking; do not "fix" it by tuning reward weights.

---

## 9. Experiment discipline

Experiments must be reproducible and controlled.

For comparisons:

- use identical scenario configuration across modes
- use fresh server/output/state per run
- preserve raw per-run results
- preserve control trajectories
- preserve correctness information
- preserve network-condition status

Also:

- Do not silently discard failed runs.
- Do not aggregate network-condition-unavailable runs as valid measurements.
- Do not claim superiority from a small smoke test.
- Do not rank experimental modes unless explicitly requested and scientifically
  justified.
- Preserve the distinction between control-flow validation, raw measurements,
  and research conclusions.
- When a learner fails to act or explore, separate the causes diagnostically
  before changing the algorithm or tuning: run a separate, labeled experiment
  (for example forcing each action and measuring the reward/state feedback it
  produces) to distinguish exploration failure from environment/identifiability
  failure. Do not assume the learner or reward is at fault.

---

## 10. Network experiment safety

- Never silently assume tc/netem shaping succeeded.
- If tc/netem configuration fails: record the failure explicitly, do not pretend
  the requested condition existed, and exclude unavailable conditions from valid
  aggregates.
- Always clean up network shaping after experiments.
- Do not leave persistent qdisc configuration behind.

---

## 11. Testing

For substantive changes, run the relevant tests. For major implementation
changes, run:

```
go test ./...
go test -race ./...
go test -count=5 ./...
go vet ./...
gofmt -l .
go build ./...
```

Fix failures before declaring a task complete.

- Do not remove or weaken existing tests merely to make a change pass.
- When fixing a discovered bug, add a regression test when practical.

---

## 12. Determinism

Prefer deterministic tests.

- Do not use arbitrary sleeps to synchronize tests.
- Use channels, barriers, existing gated servers, and deterministic
  request-count transitions when synchronization is required.
- Do not introduce randomness unless the task explicitly requires it.
- If randomness is required for a future learning algorithm, make it explicit
  and controllable.

---

## 13. Data and metrics integrity

- Metrics must represent actual downloader behavior.
- Every attempt should continue to produce exactly one attempt record.
- Do not fabricate measurements.
- Do not alter metric semantics merely to make an experiment look better.
- When changing metrics, update `PROJECT_CONTEXT.md` and the relevant tests.

---

## 14. Research claim discipline

- Do not make unsupported novelty claims.
- In particular, do not claim that joint adaptation of concurrency and chunk
  size is universally novel.
- Distinguish clearly between: established prior work, this project's specific
  research scope, hypotheses, measured results, and conclusions.
- Do not turn raw benchmark numbers into claims of superiority without
  appropriate experimental evidence.

---

## 15. Code quality

Prefer:

- small, focused changes
- existing abstractions
- explicit validation
- deterministic behavior
- clear names
- tests alongside behavior changes

Avoid:

- speculative abstractions
- premature optimization
- unrelated refactoring
- hidden global state

---

## 15a. Dependency policy

Third-party dependencies are **permitted and encouraged** when they materially
improve correctness, maintainability, reliability, or implementation quality.
Do **not** reimplement substantial generic infrastructure merely to avoid a
dependency.

Before introducing a dependency, inspect and weigh:

- maturity and maintenance status (recent releases, active maintenance)
- API quality and documentation
- license (must be compatible; document it)
- Go version compatibility
- dependency footprint (direct **and** transitive)
- whether it solves the exact problem
- whether it simplifies the repository versus adding unnecessary complexity

Prefer **focused, established libraries** over large frameworks. Use sound
engineering judgment; per-dependency approval is not required, but a dependency
must be justified in the task's report and (when meaningful) in
`docs/PROJECT_CONTEXT.md`. Record newly added direct dependencies and why they
exist. Run `go mod tidy` and inspect the `go.mod`/`go.sum` diff.

**Numerical linear algebra and other substantial mathematical infrastructure
should use established libraries** (for example Gonum) rather than being
reimplemented from scratch.

Keep the dependency boundary narrow: a library should remain an implementation
detail where possible, and its types should not leak across the project. Do not
replace the standard library with third-party packages without a compelling
reason (no logging/config/HTTP/CLI framework is needed for simple stdlib use).

**Keep project-specific research and domain logic project-owned** — this
includes range semantics, the downloader, worker pool, controllers, the policy
system, the reward design, and the RL state/action/reward contracts and the
learning algorithm itself. A dependency must never obscure project-owned
research behavior.

---

## 16. Handoff requirement

Assume another coding agent may take over at any time. Therefore:

- keep `PROJECT_CONTEXT.md` current
- document important architectural decisions
- document non-obvious invariants
- document unfinished work
- document the immediate next step
- preserve reproducible commands
- preserve important experiment outputs

Never rely on information that exists only in the conversation.

---

## 17. Task completion report

At the end of every substantive task, report:

1. What changed
2. Files created/modified
3. Tests run
4. Test results
5. Important design decisions
6. Known caveats
7. Whether `PROJECT_CONTEXT.md` was updated (use the exact line from §2)

---

## 18. Where to find project state

Do not duplicate the full architecture or implementation state here. For:

- current architecture
- completed milestones
- current implementation state
- experiment results
- RL state / action / reward design
- known caveats
- immediate next step

read `docs/PROJECT_CONTEXT.md`.

This file (`AGENTS.md`) stays focused on persistent agent behavior and workflow.
