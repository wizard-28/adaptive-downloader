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

