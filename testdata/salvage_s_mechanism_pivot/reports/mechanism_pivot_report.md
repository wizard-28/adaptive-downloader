# Adaptive-S Mechanism Pivot — Analytical Screening Report

**ANALYTICAL SIMULATION ONLY.** No empirical network experiment was run: no downloads, no
mm-link, no server/client processes, no tc/netem, no packet replay. All results are
deterministic simulator output under the stated models.

Generated: 2026-09-28T16:08:17Z

## 1. Why the static mechanism was closed

The frozen v7 static mechanism (per-request rate R + fixed RTT D) was closed by the
pre-Stage-1 scaling/headroom analysis (`salvage_s_scaling_analysis`, §22.85): its frozen
switching strategies have non-positive relative gain at every file size, and even an
optimally timed single switch stays <= +0.35% — far below the frozen 5% practical
threshold. No Stage 1 confirmatory run was performed and none is justified under that
mechanism.

## 2. What Stage 0S established

Stage 0S (32/32 clean, GO) established that the static mechanism was realized as
designed: per-request service times matched D+s/R within ~2.5%, the tail window was
present, and the run was reliable. It was a feasibility gate only; it did not test
adaptation.

## 3. What the file-size scaling analysis established

Relative headroom does not increase with file size (the bounded one-time tail effect is
diluted by the growing bulk); fixed-S normalized costs are scale-invariant
(`T_S/T_ref = 1 + R*D/s`); increasing the file size does not rescue the relative gain.

## 4. Candidate mechanism inventory

| id | mechanism | screened model | infrastructure |
|---|---|---|---|
| A | time-varying downstream capacity (shared link, in-flight tracking) | shared | yes (mm-link + native traces) |
| B | time-varying capacity + fixed RTT | shared (D=40ms) | yes |
| C | time-varying capacity + per-request setup overhead | shared with D representing the setup cost | yes |
| D | capacity transitions during in-flight range requests | shared (in-flight integration) | yes |
| E | server-side request serialization / concurrency-dependent service rate | serialized | partially (controlled server can throttle per request; serialization would need a change) |
| F | documented HTTP request-level behavior (per-request pacing fixed at request start) | lockin_full | yes (server.SetRateScheduleAt + per-request pacing fixed at request start) |

## 5. Real-trace suitability assessment

Native Mahimahi delivery-opportunity traces (capacity schedules; mm-link loops at EOF):

| family | duration s | mean KiB/s | p90/p10 | frac<25% median | eligible |
|---|---|---|---|---|---|
| ATT-LTE-driving-2016 | 120.0 | 552.1 | 13.7 | 0.12 | False |
| ATT-LTE-driving | 786.1 | 630.8 | 6.9 | 0.10 | True |
| TMobile-LTE-driving | 474.6 | 1557.6 | 6.5 | 0.04 | True |
| TMobile-LTE-short | 140.0 | 2038.7 | 5.5 | 0.01 | False |
| TMobile-UMTS-driving | 928.4 | 246.4 | 27.4 | 0.18 | True |
| Verizon-EVDO-driving | 1062.0 | 63.5 | 24.2 | 0.16 | True |
| Verizon-LTE-driving | 1363.0 | 1185.3 | 14.3 | 0.10 | True |
| Verizon-LTE-short | 140.0 | 609.4 | 7.4 | 0.06 | False |

All eight traces show strong genuine time-varying capacity (p90/p10 = 5.5-27.4; 4-18% of
seconds below 25% of the median). The 23 throughput-trajectory traces (NYU-METS, UGent)
are measured end-to-end throughput, NOT path capacity; the M3 study showed an aggregate
throughput envelope imposes the same ceiling on every concurrency level, so they cannot
represent a request-granularity problem and were used only for inventory context.

## 6. Analytical model

Deterministic event-driven finite-file scheduler simulator (`tools/mechanism_sim.py`):
N=4 work-conserving workers; contiguous ranges allocated in order; future-only chunk size;
in-flight requests never resized; request completion depends on the capacity experienced
throughout the request (piecewise-constant profiles; 1 s bins for traces, zero bins at the
documented 8000 bps M3 outage floor). Service models: `shared` (aggregate capacity shared
equally among active transfers; in-flight tracking), `lockin_full` (per-request rate fixed
at request start and held; the existing server's scheduled-rate semantics), `serialized`
(one request at a time), `constant` (the closed v7 control).

## 7. Moving-optimum test (stationary states)

argmin_S T(S | constant rate) over rates 0.25-8 MB/s aggregate and file sizes 512/520/544 MiB:

| model | preferred S at F=512 MiB across all rates | 520 MiB | 544 MiB |
|---|---|---|---|
| shared | 16MiB | 16MiB | 16MiB |
| lockin_full | 16MiB | 1MiB, 4MiB | 4MiB |
| serialized | 256KiB | 256KiB | 256KiB |

**The preferred S does not change between stationary rate states for any model.** The
shared model prefers 16 MiB at every rate; the lock-in model also prefers 16 MiB at 512 MiB
(the 520/544 MiB variation is a file-size-residue effect, which criterion 4 disqualifies);
the serialized model has a fixed 256 KiB preference. The real-trace regime test (low-rate
vs high-rate terciles) likewise gives 16 MiB in both regimes for every trace.

## 8. Causal stationary-vs-changing comparison

Within-download transition cases (low->high, high->low, mild) and stable->volatile cases:

| experiment | causal gain range | oracle gain range | stationary-control gain |
|---|---|---|---|
| E2 transitions (all models) | -4.671% .. +0.000% | +0.000% .. +0.470% | -0.000% .. +0.000% |
| E2c stable->volatile (lock-in) | -61.222% .. +0.000% | +0.000% .. +31.675% | 0.000% .. 0.000% |
| E3 real traces (all models) | -42.620% .. +0.000% | n/a | -0.000% .. +0.000% |

Causal, observation-driven switches are **zero or harmful** in every case: under the
shared and serialized models they never fire (no state-dependent preference); under the
lock-in model they fire after the volatility is already visible and then underperform the
best fixed arm by 2-61%. In stationary controls every causal gain is exactly 0.000%.

## 9. Headroom analysis

The only positive within-download opportunity found is the **clairvoyant** single switch in
the lock-in stable->volatile case: oracle gains of 0-32%, varying with the volatility
period (0% at 60 s, 7-23% at 17 s, 11-32% at 5 s) and mode. Because it requires perfect
knowledge of the alternation phase and vanishes at longer periods, it is a fragile
diagnostic artifact, not a causal opportunity. The strongest robust statement is:
**no screened mechanism provides a causal within-download S-switch headroom above 0%.**

## 10. Robustness to parameter variation

The lock-in oracle headroom is highly parameter-sensitive (period and phase of the
alternation; stable-phase fraction), and the preferred S at non-aligned file sizes is a
residue effect. No candidate remained present across the parameter range; the only
parameter-robust preference is the fixed 16 MiB (shared/lock-in at aligned files).

## 11. Candidate classification (frozen five-condition rule)

| candidate | 1 preferred S changes between states | 2 transition within a download | 3 causal adaptation can react | 4 not residue/phase dependent | 5 weakens when stationary | classification |
|---|---|---|---|---|---|---|
| A/B/C/D shared time-varying capacity | FAIL (16 MiB at every rate/regime) | PASS | FAIL (0.000%) | PASS | PASS | **MECHANISTICALLY INSUFFICIENT** |
| E server serialization / concurrency-dependent rate | FAIL (fixed 256 KiB) | PASS | FAIL (0.000%) | PASS | PASS | **MECHANISTICALLY INSUFFICIENT** |
| F per-request lock-in + volatility | FAIL (16 MiB at every stationary rate) | PASS | FAIL (-2% to -61%) | FAIL (period/phase dependent) | PASS | **MECHANISTICALLY INSUFFICIENT** |

No candidate satisfies the five conditions. No mechanism was accepted on the basis of a
favorable gain.

## 12. Recommended next mechanism

**None from the screened set.** The natural trace-driven mechanisms do not create a
state-dependent preferred S, and the mechanisms that can create a large S-effect (lock-in
under volatility) are not causally exploitable within one download. No new preregistered
protocol is warranted under the tested mechanisms. A future study would require a
mechanism where the optimal S robustly changes between observable states and a causal
policy captures a meaningful fraction of the clairvoyant bound.

## 13. Exact additional evidence required before experimental execution

1. A documented or measured mechanism with a robust, reproducible state-dependent argmin_S
   (not residue- or phase-dependent).
2. A causal (non-clairvoyant) policy that captures a material fraction of the oracle bound
   in simulation, with the policy fixed before execution.
3. A trace/parameter population screened by a pre-declared technical eligibility rule,
   summarized across all eligible items (no selection by adaptive outcome).
4. A new preregistered protocol at a new path; the frozen v7 protocol must not be modified.

## 14. Limitations

- Deterministic simulator; no timing noise, TCP dynamics, or per-connection scheduling.
- The lock-in model's per-request fixed rate is the existing server's scheduled-rate
  semantics; it is a coarse model of real congestion-control adaptation.
- Trace profiles are 1 s bins with the documented outage floor; sub-second burstiness is
  not modeled.
- The clairvoyant oracle is a diagnostic upper bound only and is not a policy.

## Paper-scope test

No appropriately narrow claim can be made: the screened mechanisms do not support a
statement that chunk size becomes state-dependent within a download in a causally
exploitable way. No RL, universal, joint N+S, aria2, or real-world claim is made.

## Statement

Zero empirical network experiments were performed for this analysis.
