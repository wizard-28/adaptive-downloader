# Proposal Alignment

Synthesis of completed evidence only. No new experiments were run.

The original submitted proposal is documented in `docs/PROJECT_CONTEXT.md` §2
and the milestone history; no separate proposal file exists in the repository.
Statuses: **COMPLETED**, **PARTIALLY COMPLETED**, **ANALYTICALLY SCREENED**,
**NOT JUSTIFIED / NOT EXECUTED**.

---

## 1. Research-question reconstruction

| Item | Content |
|---|---|
| Original question | In a client-observed, Range-based, single-source download under server throttling and network impairments, what are clean, measurable control policies for concurrency (N) and future chunk size (S), and what would a learned (contextual-bandit) policy need to observe and optimize to be comparable to deterministic baselines? |
| Revised question actually investigated | Under realistic, live-grounded mechanisms, when does adaptive N or future-only S have sufficient **causal headroom** to justify an adaptive policy at all — and what mechanism must exist first? |
| Implemented | Range downloader (tracker/fetcher/assembler/metrics), worker pool, controllers, policy interface, deterministic policies; controlled HTTP/1.1 server; tc/netem abstraction; experiment harness; RL contract + LinUCB + DQN candidates; external aria2 baseline harness; pre-registered concurrency oracle (five traces); pre-registered S protocol (v7) and Stage 0S; analytical scaling/pivot/N screens. |
| Not scientifically justified to execute | Adaptive-S confirmatory stage (no headroom); adaptive-N confirmatory stage (no live-grounded mechanism); joint N+S confirmatory; RL confirmatory head-to-head against a demonstrated opportunity. |
| Why the question narrowed | The evidence showed the binding constraint is the **environment**, not the controller: no screened mechanism produced a state-dependent optimum that survived live grounding, so policy complexity had nothing causal to optimize. |
| Coherence with the original project | The revised question remains within the original project's scope (adaptive range downloading under controlled mechanisms) and its methods (pre-registered, reproducible, mechanism-first). It adds a falsifiable gate — causal headroom — that the original framing assumed rather than tested. |

---

## 2. Proposal item mapping

| Proposal item | Status | Evidence | Final-paper treatment |
|---|---|---|---|
| Adaptive concurrency N | ANALYTICALLY SCREENED; confirmatory NOT JUSTIFIED | T1, M3, L3/L4, N screen | Negative chain: monotone/plateau preferred N; analytical `degrade` not live-grounded |
| Adaptive chunk size S | COMPLETED feasibility (Stage 0S GO); confirmatory NOT JUSTIFIED | v7 protocol, Stage 0S, scaling, pivot | Negative chain: real service-cost effect, non-positive switching headroom, no state-dependent optimum |
| Joint N+S adaptation | NOT JUSTIFIED / NOT EXECUTED as a positive claim | Joint mode exists in the harness; no mechanism established | State that no joint benefit is claimed; the joint mode is an implementation artifact, not a validated result |
| Fixed policy | COMPLETED | Baseline matrices; Stage 0S fixed arms | Used as the reference fixed condition throughout |
| Heuristic policy | COMPLETED (deterministic policies) | Throughput/ChunkSize/Joint policies | Descriptive baselines; no superiority claims |
| LinUCB | COMPLETED (negative/descriptive) | §22.7–§22.18 (lock-in, coverage, wall-clock) | State the lock-in/coverage findings; no benefit claim |
| RL (DQN) | COMPLETED (negative) | §22.22–§22.34 (candidates v2–v6; final head-to-heads) | Negative results: v2 75.7% slower than aria2; v5 2.32× slower; v6 comparison not run |
| Controlled server | COMPLETED | `internal/experiment/server` | Methodology foundation; per-request pacing semantics are the key mechanism ingredient |
| Network emulation | COMPLETED | tc/netem abstraction; mm-link trace replays | Used in the concurrency and aria2 studies; M3 envelope limitation documented |
| Ablations (N-only, S-only) | PARTIALLY COMPLETED | Modes exist; S-only mechanism closed analytically | Describe the ablations as implemented but not confirmatorily validated |
| aria2 baseline | COMPLETED | §22.16, §22.20, §22.25, §22.26, §22.32 | Fairly-configured baseline; negative head-to-head; no ranking |
| Five-trace statistical gate | COMPLETED (FAIL) | `final_oracle_analysis` | The frozen negative gate result (LCB_95 = −0.0026077 < 0.10) |
| Real-network / consumer-network generalization | NOT EXECUTED | — | Explicit limitation; no generalization claim |

---

## 3. What the final paper must not pretend

- RL was **not** scientifically validated; the executed RL results were
  negative/descriptive, and no mechanism supported a confirmatory RL claim.
- Adaptive N and S were **not** shown to be beneficial; they were shown to lack
  a robust causal opportunity under the screened mechanisms.
- The downloader was **not** shown to beat aria2; the head-to-heads were
  negative for the tested configurations.
- The analytical N headroom (+6.66%) is **not** empirical evidence and is
  contradicted by the project's own live validation (L4 GATE E).
