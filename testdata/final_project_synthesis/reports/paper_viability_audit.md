# Paper Viability Audit

Synthesis of completed evidence only. No new experiments were run.

---

## 1. Reconstructed research question

**Original proposal (documented in `docs/PROJECT_CONTEXT.md` §2 and the milestone
history; no separate proposal file exists in the repository):** in a
client-observed, Range-based, single-source download under server throttling
and network impairments, what are clean, measurable control policies for
concurrency (N) and *future* chunk size (S), and what would a learned
(contextual-bandit / RL) policy need to observe and optimize to be comparable
to deterministic baselines? The proposal included adaptive N, adaptive S,
N-only and S-only ablations, joint N+S, fixed/heuristic policies, LinUCB, RL,
a controlled server, network emulation, and an aria2 baseline.

**Revised question actually investigated (final):**

> Under realistic, live-grounded download mechanisms, when does adaptation of
> HTTP range concurrency (N) or future chunk size (S) have sufficient *causal
> headroom* to justify an adaptive policy at all — and what mechanism must
> first exist for that headroom to be real?

This is a **mechanism-first / headroom-first** reformulation. It was forced by
the evidence: the project could control N and S, but no screened mechanism
created a state-dependent optimum that survived live grounding, so no
confirmatory adaptive experiment was justified.

**Why the question had to narrow:** the original framing assumed the control
variables (N, S) were the contribution. The completed evidence shows the
binding constraint is the *environment*: a policy can only exploit a causal
opportunity that the mechanism physically creates. Without such a mechanism,
RL/policy complexity has nothing to optimize (see §4 below).

---

## 2. What was implemented vs screened vs not executed

| Stage | Status | Evidence |
|---|---|---|
| Range downloader (tracker/fetcher/assembler/metrics), worker pool, controllers, policy interface, deterministic policies | COMPLETED | Source + tests; baseline matrices |
| Controlled HTTP/1.1 range server + tc/netem abstraction | COMPLETED | `internal/experiment/server`, `network` |
| Concurrency/oracle study (five-trace, frozen protocol + amendment) | COMPLETED | `final_fixed_c_stage_a`, `final_tau_stage_b`, `final_oracle_analysis` (gate FAIL) |
| aria2 baselines (configs v1/v2), granularity/wall-clock controls, DQN head-to-heads | COMPLETED (negative) | §22.16, §22.20, §22.25, §22.26, §22.32 |
| Adaptive-S static mechanism (v7 + Stage 0S) | COMPLETED (GO feasibility), confirmatory NOT JUSTIFIED | `salvage_s_protocol_v7`, `salvage_s_stage0s` |
| Adaptive-S scaling / mechanism pivot | ANALYTICALLY SCREENED | `salvage_s_scaling_analysis`, `salvage_s_mechanism_pivot` |
| Adaptive-N mechanism screen | ANALYTICALLY SCREENED | `adaptive_n_mechanism_screen` |
| RL (LinUCB, DQN) adaptive-policy validation | NOT JUSTIFIED / NOT EXECUTED as a positive claim | LinUCB/DQN studies exist and were negative or descriptive |
| Joint N+S confirmatory adaptation | NOT JUSTIFIED / NOT EXECUTED | No mechanism established |
| Real-network deployment / consumer-network generalization | NOT EXECUTED | — |

---

## 3. Strongest paper narrative

**Working title:** *When does adaptive HTTP range downloading actually have
useful headroom? A mechanism-first negative result.*

The completed evidence supports a mechanistic/negative-result paper built on
the following chain:

1. Adaptation cannot be justified merely because N or S is controllable.
2. A mechanism must first create a **state-dependent optimum** (`argmin_N T(N|x)`
   or `argmin_S T(S|x)` changing with an environmental state that changes
   during one download).
3. Under the static rate+RTT mechanism, S-dependent request costs are real and
   reproducible (Stage 0S), but the frozen switching rules have non-positive
   headroom and the one-time tail is too small.
4. Increasing the workload size does not rescue relative adaptive benefit: the
   per-request cost differences are scale-invariant and the tail is diluted.
5. Broader S screening and N screening find no robust causal opportunity;
   the analytically plausible N mechanism (concurrency-dependent server cost)
   fails live physical grounding.
6. Therefore policy complexity (including RL) should **follow** demonstrated
   causal opportunity rather than precede it.

The paper must state this is a scoped result about the tested mechanisms and
controlled environment, **not** a universal theorem about adaptive downloaders.

---

## 4. RL decision

**RL should not be executed as a confirmatory study under the current
evidence; it is deferred, not abandoned for all time.**

- The project's own RL attempts were executed and were negative/descriptive:
  LinUCB locked onto one arm; the frozen DQN Candidate v2 was 75.7% slower than
  the frozen aria2 baseline (ratio 1.757, 95% CI [1.669, 1.850]); Candidate v5
  was 2.32× slower (95% CI [2.2269, 2.4206]); Candidate v6's pre-registered
  aria2 comparison was not run because no mechanism supported it.
- RL can optimize a demonstrated controllable opportunity, but it cannot create
  a physical mechanism the downloader does not possess. No robust causal N/S
  opportunity has been established, so a confirmatory RL run has nothing
  meaningful to exploit.
- This is **not an implementation failure**: the downloader, reward contract,
  adapters, and learners were implemented and unit-tested; the environment
  lacked the mechanism.

**Condition for revisiting RL:** first establish a live-grounded, causal,
state-dependent N or S opportunity (see `future_work_boundary.md`), then run a
pre-registered RL comparison against the model-implied best fixed baseline.

---

## 5. Final research decision

**PAPER-READY — MECHANISTIC / NEGATIVE-RESULT STORY.**

Supporting evidence:

- The S chain is closed with pre-registered evidence at every step (protocol,
  feasibility, scaling, mechanism pivot).
- The N chain is closed with a pre-registered classification rule and explicit
  live-grounding failure (L4 GATE E, T1, M3).
- The concurrency oracle study provides a frozen, five-trace statistical gate
  result (FAIL, LCB_95 = −0.0026077 < 0.10) with per-trace descriptive values.
- The aria2 evidence provides a fairly-configured, statistically analyzed
  external baseline with honest negative head-to-head results.
- The contribution (headroom-first methodology + negative mechanism results)
  is supported by the completed evidence without new experiments.

**No additional empirical evidence is required for this paper.** Additional
experiments would only be needed to support a *different* (positive) claim,
which the evidence does not currently permit.

---

## 6. Proposed paper structure

| Section | Evidence used | Figures | Claims allowed | Claims prohibited |
|---|---|---|---|---|
| 1 Introduction | Project positioning; the S/N closure | Fig. 1 | Adaptation needs a mechanism; headroom-first framing | "First adaptive downloader"; universal claims |
| 2 Problem formulation | Definitions of N, S, future-only resizing, causal opportunity | Fig. 1 | Formal `argmin` state-dependence condition | Any claim that control implies benefit |
| 3 Adaptive downloader architecture | Source, tests, policy interface | Fig. 1 | Architecture and abstractions are implemented | Architecture novelty claims |
| 4 Research methodology | Pre-registration chain (v7, Stage 0S, five-trace gate, N rule) | — | Pre-registered, reproducible methodology | "Proven" methodology |
| 5 Concurrency evidence | T1, M3, L3/L4, five-trace oracle (FAIL) | Fig. 6 (live boundary) | Monotone concurrency; gate FAIL; L4 live mismatch | Adaptive-N benefit |
| 6 Chunk-size mechanism study | Stage 0S, scaling | Figs. 2–4 | Service cost reproduced; non-positive switching headroom | Adaptive-S benefit |
| 7 Mechanism screening | S pivot, N screen | Figs. 5–6 | No robust state-dependent optimum in the screened set; degrade is analytical-only | Any positive adaptive claim from simulation |
| 8 Practical baseline comparison | aria2 §22.16/§22.20/§22.25/§22.26/§22.32 | Fig. 7 (narrow, matched) | Granularity-matched ~4% difference (n=10); negative DQN head-to-heads | General aria2 superiority/inferiority |
| 9 Discussion | All of the above | — | Headroom-first implication; result-shopping boundary | Universal theorem |
| 10 Limitations | Limitations inventory | — | Scope limits | Hiding the synthetic/controlled nature |
| 11 Conclusion | The negative chains | — | Mechanism-first conclusion | Any unsupported positive claim |
