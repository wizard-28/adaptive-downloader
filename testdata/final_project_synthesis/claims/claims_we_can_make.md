# Claims We Can Make

Synthesis of completed evidence only. No new experiments were run.

Paper-ready wording; strength never exceeds the evidence.

---

## A. Claims we can make (directly established)

1. **Mechanism validation (live).** "Under the tested controlled server with
   per-request pacing and a fixed 40 ms response delay, the per-request service
   time increased with chunk size from 256 KiB to 16 MiB, matching the
   analytical `D + s/R` prediction within approximately 2.4–2.5 % across all
   four arms (32/32 runs clean)."
2. **Tail realization (live + analytical).** "The finite-file tail predicted by
   the mechanism parameters (33.59 s) was realized in the pilot (pooled median
   34.98 s, within the pre-registered bounds)."
3. **Frozen five-trace gate (trace replay).** "The pre-registered five-trace
   statistical gate failed: the one-sided 95 % lower confidence bound on the
   mean trace-level headroom was `LCB_95 = −0.0026077`, below the frozen
   threshold of 0.10."
4. **Descriptive two-trace headroom (trace replay).** "The two-trace descriptive
   headroom was 1.6075 % (Verizon-EVDO-driving) and 0.0320 % (Verizon-LTE-
   driving); these are descriptive values under the frozen counterfactual model,
   not a population estimate."
5. **S closure (analytical).** "Under the frozen static rate+RTT mechanism, the
   frozen chunk-size switching strategies had non-positive relative gains at
   every tested file size (64 MiB–32 GiB), and the exploratory optimally-timed
   single switch did not exceed +0.36 %."
6. **S scaling (analytical).** "Normalized fixed-chunk completion costs were
   scale-invariant (`T_S/T_ref = 1 + R·D/s`), and the one-time tail contribution
   to completion fell with file size; increasing the file size did not increase
   relative adaptive-S headroom."
7. **S screening (analytical).** "Across the screened mechanisms, the preferred
   chunk size did not change between stationary environmental states, and causal
   within-download switches were zero or harmful."
8. **N closure (live + analytical).** "Under independent per-response pacing,
   aggregate throughput scaled monotonically with concurrency, and the only
   live-validated result was monotone (no concurrency reversal); a simulated
   over-concurrency penalty did not survive live validation."
9. **N screening (analytical, explicitly labelled).** "In a deterministic
   analytical model, a concurrency-dependent server-capacity mechanism produced
   a state-dependent preferred concurrency and causal gains up to 6.7 % at
   severe capacity drops; this is an analytical result that is not
   live-grounded."
10. **Practical baseline (live, narrow).** "With request granularity and
    parallelism matched (four 1 MiB requests, four concurrent), aria2's mean
    completion time was 2129.7 ms versus 2055.7 ms for the internal fixed
    configuration (+3.6 %, n=10) on one 4 MiB wall-clock profile."
11. **RL outcome (live, negative).** "The frozen DQN Candidate v2 was 75.7 %
    slower than the frozen aria2 baseline (geometric ratio 1.757, 95 % CI
    [1.669, 1.850], 240 matched pairs, faster in 31/240 pairs)."
12. **Methodological claim.** "A pre-registered, mechanism-first methodology can
    close an adaptive-downloading study before adding policy complexity: the
    causal headroom must be demonstrated before the policy is."

---

## B. Claims we can make only with qualifiers

1. "The results **suggest** that adaptive chunk sizing has limited headroom
   under the tested static rate+RTT mechanism." *(Qualifier: analytical model;
   one mechanism; controlled single-server.)*
2. "The concurrency results are **consistent with** the absence of a
   concurrency-dependent cost in the current controlled stack." *(Qualifier:
   the negative evidence covers the tested configurations; it does not prove no
   mechanism exists in real networks.)*
3. "Request-granularity matching **largely explains** the aria2-vs-internal
   wall-clock difference in the tested profile (~4 %)." *(Qualifier: n=10, one
   profile, synthetic server, residual confounds documented.)*
4. "The analytical N headroom **could** be exploitable if a live-reproducible
   concurrency-dependent server cost existed." *(Qualifier: no such live cost
   was found; the claim is conditional and must not be presented as a result.)*
5. "The methodology **may** generalize to other adaptive-downloading studies."
   *(Qualifier: demonstrated only for this project's mechanisms and evidence.)*

---

## C. Claims we must not make

1. "Adaptive chunk sizing is beneficial."
2. "Adaptive concurrency is beneficial."
3. "Joint N+S adaptation improves downloading."
4. "RL improves download performance."
5. "The proposed downloader outperforms aria2." (No ranking; the head-to-heads
   were negative.)
6. "The proposed downloader generalizes to consumer networks."
7. "Larger files increase adaptive-S gains."
8. "Changing networks create an adaptive opportunity." (Not under the screened
   mechanisms.)
9. "The analytical +6.66 % N headroom is an empirical result."
10. "The clairvoyant oracles are deployable policies."
11. "The study proves no adaptive downloader can work." (It is a scoped result
    about the tested mechanisms and environment, not a universal theorem.)
12. Any novelty claim about adaptive concurrency or chunk sizing per se.
