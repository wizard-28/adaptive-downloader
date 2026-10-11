# Future Work Boundary

Synthesis of completed evidence only. No new experiments were run.

---

## 1. Limitations inventory

**Transport and topology**
- Single-server, single-source HTTP range downloads only.
- HTTP/1.1 only; no HTTP/2, HTTP/3, or QUIC; no multipart/disjoint ranges; no
  CDN-specific or cross-server scheduling.
- All evidence is from a controlled synthetic server and loopback/mm-link
  replays; no real-network deployment.

**Mechanism coverage**
- The static per-request rate + fixed RTT mechanism was fully characterized, but
  it is one mechanism.
- The S screening covered a shared-capacity, a per-request lock-in, and a
  serialized model; the N screening covered direct, fair, background-sharing,
  server-cap, and concurrency-dependent-degradation models.
- The concurrency-dependent degradation mechanism (the only analytically
  state-dependent N mechanism) is not live-grounded and was rejected by the
  project's own live validation (L4 GATE E).
- Trace grounding is limited: the native traces represent aggregate capacity
  only; the throughput traces are not capacity; no trace contains per-flow
  fairness, queueing, or per-connection cost information.

**Workload and grids**
- Finite-file workloads; the S scaling grid is 64 MiB–32 GiB at 512 MiB
  baseline; the N screen uses 512 MiB, S = 1 MiB, N ∈ {1,2,4,8,16}.
- Single-request-size convention (1 MiB) in the N screen; the S study fixed
  N = 4.
- The tail/scale analysis assumes the frozen epoch/measurement rules.

**Empirical adaptive-policy validation**
- No adaptive-S or adaptive-N confirmatory experiment was executed (none was
  justified).
- The deterministic policies were evaluated descriptively; no superiority claim.
- RL: LinUCB locked onto one arm; the DQN candidates were slower than the
  frozen aria2 baseline in the final head-to-heads; Candidate v6's pre-registered
  aria2 comparison was not run because no mechanism supported it.

**Practical baseline**
- aria2 1.37.0 only; its 1 MiB minimum split constrains granularity matching;
  configs v1 (4 connections) and v2 (16 connections) are frozen for their tested
  scales; the comparisons are limited to the tested scenarios and file sizes
  (4 MiB, 16 MiB calibration scale).
- IDM was not evaluated (Windows-only; no path in this repository).

**Evidence boundaries**
- Analytical/simulation results are not empirical evidence; oracles are
  diagnostic only; the descriptive two-trace headroom is not a population
  estimate; the synthetic conditions are not representative of all networks.

---

## 2. What NOT to do next (result-shopping boundary)

These studies must **not** be launched merely to obtain a favorable result:

1. Larger and larger files under the same static S mechanism (relative headroom
   does not increase; the tail is diluted).
2. Arbitrary new S triggers or re-tuned thresholds after seeing results.
3. Arbitrary new N cost functions chosen to create an interior optimum.
4. Cherry-picked traces or residues; selection by adaptive outcome.
5. Continuous parameter tuning for maximum adaptive gain.
6. Repeated mechanism invention until one produces a positive number.
7. RL without a demonstrated, live-grounded adaptive opportunity.
8. Post-hoc modification of the frozen protocols (v7, Stage 0S, the five-trace
   gate, the N classification rule).
9. Presenting analytical or oracle results as empirical measurements.

This boundary exists to close the result-shopping loop: the completed negative
results are the scientific output, not an obstacle to be tuned away.

---

## 3. Remaining evidence gaps and the condition for new work

**Gap:** no live-grounded mechanism currently exists that creates a robust,
causal, state-dependent optimum for N or S in the controlled environment.

**Condition for any new empirical work** (all must hold):

1. A controlled experiment demonstrates a reproducible concurrency- or
   chunk-size-dependent cost that makes excessive control genuinely expensive
   (the current stack does not; L4 GATE E, T1, M3).
2. The mechanism's parameters are measured and grounded, never tuned to produce
   a gain.
3. A causal policy is pre-registered (one switch, adjacent steps,
   observed-throughput trigger, frozen N/S mapping).
4. The model-implied best fixed baseline and the 5 % practical reference are
   pre-registered before execution.
5. Fresh evidence is used, at a new path; all frozen artifacts remain untouched.

**RL:** deferred, not abandoned. Revisit only after (1)–(5) hold and a
pre-registered RL comparison against the model-implied best fixed baseline is
possible.

**Paper status:** the completed evidence is sufficient for the
mechanistic/negative-result paper; no additional empirical evidence is required
for that paper.
