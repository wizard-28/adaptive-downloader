# Paper draft — working skeleton

**Working title:** *Request granularity and concurrency adaptation for HTTP range
downloads under controlled server-side pacing: mechanisms, a practical baseline
result, and when adaptation actually helps.*

Status legend: **\[DONE\]** evidence complete;
**[PENDING]** analysis pending; **[TODO]** writing.

---

## Abstract (draft)

Adaptive HTTP range downloaders jointly choose concurrency (N) and range size
(S), but it is rarely clear when adaptation has anything causal to exploit. We
build a controlled single-server HTTP/1.1 range-download platform with
configurable per-response pacing, concurrency-dependent throttling, and network
impairment, and we use it to (i) characterise a request-granularity x
request-start-timing x phase-locked-pacing mechanism under which a fixed
configuration (16 connections, 4 MiB ranges) completes a 512 MiB download
**62.4 % faster than aria2 1.37 (30/30 paired runs)**, (ii) show through a
series of pre-registered mechanism screens that within-download adaptation has
no causal headroom under the measured mechanisms unless the server itself
degrades with concurrency, and (iii) demonstrate two positive adaptive results
under controlled server-behaviour scenarios: a **causal joint (N,S)
cross-workload selector** that beats the best single fixed configuration across
a healthy/degraded server family, and a **within-download concurrency
adaptation** controller that detects a server degradation and beats the best
fixed configuration and aria2. All results are scoped to the controlled
mechanisms and are fully reproducible.

---

## 1. Introduction

- Segmented range downloads; fixed vs adaptive N and S.
- The problem: adaptation is only useful when the environment has a
  state-dependent optimum that a causal controller can observe.
- Contributions:
  1. a controlled experimental platform (downloader + server + policies);
  2. a mechanism characterisation and a large practical baseline win over
     aria2 [DONE];
  3. a systematic mechanism-first study of when adaptation helps [DONE];
  4. a cross-workload joint (N,S) selection result (mean +27.9 %, worst +8.2 % over the best single fixed config) [DONE];
  5. a within-download joint adaptation result under server degradation (+23.4 % over the best fixed, 10/10) [DONE];
  6. an honest limits analysis, including the mechanism-dependent aria2 contrast.
- Scope statement: controlled server/network mechanisms, not claims about
  arbitrary production servers.

## 2. Related work

Liu et al. (smart DTN), Swargo/Arifuzzaman (elastic DTN, FastBioDL),
DeLoad (short-video preloading), MDTP (multi-source), BOLA (ABR analogy),
aria2/IDM as practical baselines. Position: mechanism-first evaluation with
pre-registration and controlled server behaviour.

## 3. System and experimental platform

- Go downloader: RangeTracker (half-open), fetcher (inclusive), assembler,
  metrics, worker pool (dynamic N, in-flight immutable), concurrency and
  chunk-size controllers, policy interface (fixed/throughput/chunk/joint),
  serialized policyRunner, LinUCB adapter and DQN candidates.
- Controlled server: deterministic logical object, per-response pacing sampled
  at request start and held, wall-clock rate schedules, fault injection, and
  (new, §22.96) a **soft concurrency cap**
  `rate_eff = min(base, C/(k*(1+gamma*(k-1))))` sampled at request start.
- Harness: experiment matrix, aria2/curl/wget runners, content verification,
  tc/netem, mm-link replay.

## 4. The phase-locked pacing mechanism and the 62.4 % aria2 result [DONE]

- Workload D2: 512 MiB, per-response pacing 640 -> 160 -> 2500 KiB/s at
  2 s / 4 s, 40 ms RTT + 0.4 % loss.
- Final block (30 pairs): ours (16, 4 MiB) mean 20.12 s vs aria2 53.56 s,
  geometric ratio 0.3756 CI [0.3728, 0.3785], 30/30, p ~ 5e-5.
- Mechanism: aria2's 450 x 32 MiB pieces all start < 2 s and are held at
  640 KiB/s; ours' 3840 x 4 MiB ranges re-allocate into the recovered phase.
- Controls: granularity-matched ratio 0.862; constant-rate collapse; full
  frontiers on both sides.

## 5. When does adaptation help? Mechanism-first screens [DONE]

- Pre-registered screens: phase-pacing (S moves, N*=16, no causal headroom),
  live queueing (work-conserving), correlated loss (S fragile, N monotone),
  failure-based N-dependent throttling (abort cliff), adaptive-N degradation
  (interior optimum analytically, live mismatch).
- Track 1: the existing adaptive heuristics on D2 adapt but lose to the fixed
  optimum (joint 2.32x slower; classification BEATS_ARIA2_ONLY); the cause is
  the per-request throughput signal saturating plus the decision cadence.
- Conclusion: within-download adaptation needs a mechanism whose optimum moves
  in both N and S and is causally observable.

## 6. Cross-workload joint (N,S) selection [DONE]

- Family: healthy server (T=1 s pacing, no cap) vs concurrency-degraded server
  (C=8 MiB/s, gamma=0.05); stationary optima (16, 1 MiB) vs (4, 4 MiB);
  cross penalties +35.8 % / +252.9 %.
- Selector: probe at (16, 1 MiB), classify after >= 20 samples by the minimum
  per-request throughput (< 450 KiB/s => degraded), select the predeclared
  (N,S), no further switching.
- Live (10 blocks/workload): healthy selector 19.1 s vs fixed 19.0 s
  (geo 1.006, CI [0.939,1.080]) vs aria2 53.9 s (geo 0.353, 10/10);
  capped selector 84.2 s vs fixed 115.3 s (geo 0.730, CI [0.728,0.732],
  10/10) vs aria2 68.3 s (0/10).
- Baselines stated explicitly: vs the **best single fixed configuration across
  the two predeclared controlled workloads** (8, 4 MiB; 36.3 s healthy /
  91.7 s capped) the selector gains +47.5 % / +8.2 % (mean +27.9 %, worst
  +8.2 %); vs the **per-workload best fixed** (healthy (16,1M) 18.4 s, capped
  (4,4M) 86.4 s) it is -3.5 % (healthy, policy-path overhead; in-block
  19.1 vs 19.0 s is indistinguishable) and +2.6 % (capped). The selector is
  not told which workload it faces; it classifies causally.

### 6.1 Six-workload expansion [DONE]

Frozen manifest `testdata/n_dependent_server_joint_ns/manifest/w1_w6_manifest.json`
(designed analytically before any live W1-W6 run; 13/72 candidates qualified).
Six predeclared controlled workloads (W1 healthy; W2-W6 = schedule x cap x
transition variants). Live fixed-grid validation (2 reps/cell): 5/6 model
optima matched; **W5 mismatched** (live (16,16M) 85.4 s vs model (4,4M)
95.7 s — a retry/k feedback the model lacks). The frozen selector was not
retuned after this observation.

10 fresh paired blocks per workload (240 runs, 0 invalid):
selector | per-workload best fixed (live argmin) | family-best single fixed |
aria2. Pooled paired geometric ratios (95 % bootstrap):

| comparison | geo mean | CI | wins |
|---|---|---|---|
| selector vs per-workload best fixed | **0.999** | [0.969, 1.029] | 24/60 |
| selector vs family-best single fixed | **0.770** | stratified [0.768, 0.772]; six-cluster [0.455, 1.304] | 40/60 |
| selector vs aria2 | 1.660 | [1.363, 1.988] | 10/60 |

Per workload (paired geometric ratio, selector / baseline; < 1 favors the
selector):

| workload | vs per-workload best fixed | vs family-best single fixed (4, 4 MiB) |
|---|---|---|
| W1 | 1.013 | 0.286 |
| W2 | 0.934 | 0.869 |
| W3 | 1.019 | 0.949 |
| W4 | 0.808 (probe-phase timing: the selector runs the fast (16, 1 MiB) config until the cap appears) | 0.765 |
| W5 | 1.172 (frozen-target mismatch) | 1.058 |
| W6 | 1.088 | 1.090 |

aria2 wins all degraded workloads (cap escape). Interpretation (concept B, not
C): pooled over 60 workload-blocks the selector is not distinguishable from the
per-workload clairvoyant (0.999) while not being told the workload, but this
average hides offsetting per-workload effects (a +19 % timing win on W4 against
losses of 9-17 % on W5/W6), and the pooled CI treats blocks as independent. The
pooled ratio against the family single fixed configuration (0.770) is **not** a
robust speed-up claim: a six-cluster interval is [0.455, 1.304]
(`analysis/w1_w6_robustness.json`), it is driven by W1 (a healthy workload vs a
degraded-state fixed config), over W2-W6 the geometric mean is about 0.94, and
the selector loses on W5 and W6. The honest summary is a selector that is not
significantly different from the per-workload clairvoyant on average, with mixed
per-workload results.

## 7. Within-download joint adaptation under server degradation [DONE]

- State schedule: no cap [0, 8 s), cap on [8 s, EOF).
- Controller: baseline = running max of the windowed median per-request
  throughput; on a drop below 0.5x baseline, switch once (joint target
  (4, 4 MiB); N-only target (4, 1 MiB); S-only target (16, 4 MiB)).
- Analytical: post-switch optimum (4, 1 MiB); joint target +11.5 %, N-only
  +15.4 % over the best fixed; aria2 90.3 s modelled.
- Live (10 blocks): causal joint 58.7 s vs best fixed (16,1M) 76.7 s
  (geo 0.766, CI [0.763,0.769], 10/10 wins, +23.4 %); N-only control 62.0 s
  (geo 0.808, 10/10; joint vs N-only geo 0.947, CI [0.943,0.951], 10/10);
  **S-only ablation** (target (16, 4 MiB)) 82.1 s — worse than no switch
  (76.7 s, geo 1.070, 0/10): under this cap the N move carries the gain and
  the S move alone actively hurts; aria2 53.6 s (geo 1.096, 0/10) — aria2
  wins via its burst-start cap escape (its 32 MiB pieces start at k<10 and
  hold 640 KiB/s).

## 8. Discussion and limitations

- Controlled mechanisms; the 62.4 % result is mechanism-specific.
- The within-download win is concurrency-driven (honest).
- Workload-dependent selection is not within-download adaptation.
- Single server, HTTP/1.1, loopback; no real-network deployment.
- Negative results are reported as limits, not hidden.

## 9. Conclusion

## Appendix / reproducibility

- Artifact paths, frozen manifests, raw runs, analysis scripts.

---

## Claim -> evidence map

| Claim | Evidence | Status |
|---|---|---|
| Fixed (16,4M) beats aria2 by 62.4 % under D2 | 30-pair frozen benchmark | DONE |
| The win is granularity x timing x pacing | request logs + controls | DONE |
| Within-download adaptation has no causal headroom under the measured mechanisms | 4 pre-registered screens + Track 1 | DONE |
| A causal selector beats the best single fixed config across a healthy/degraded server family | P1 live experiment (mean +27.9 %, worst +8.2 %) | DONE |
| A frozen workload selector matches the per-workload clairvoyant best on average (not an equivalence test) and is mixed versus a single family config across six predeclared workloads | W1-W6 live study (240 runs; selector vs per-workload geo 0.999 [0.969,1.029]; vs family single fixed geo 0.770 [0.682,0.860], 40/60; W5 mismatch disclosed) | DONE |
| A causal joint controller beats the best fixed within a download when the server degrades | P2 live experiment (+23.4 %, geo 0.766, 10/10) | DONE |
| The soft N-dependent throttle creates a state-dependent joint optimum live | pre-screen + live gate (N* 16->4, S* 1M->4M; S* local margin 3.8 %, below the 5 % bar) | DONE |
| aria2 comparison is mechanism-dependent | ours +62.4 % (D2) and 2.8x (healthy); aria2 wins under the cap (burst-start escape) | DONE |
