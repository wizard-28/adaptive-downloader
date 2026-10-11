#!/usr/bin/env python3
"""Generate the mechanism pivot report and manifest from the screening outputs."""
import json
import os
import time

BASE = "/workspaces/mvp/testdata/salvage_s_mechanism_pivot"
J = json.load(open(os.path.join(BASE, "analysis", "screening.json")))
INV = json.load(open(os.path.join(BASE, "inventory", "trace_inventory.json")))
MIB = 1024 ** 2


def pct(x):
    return "%+.3f%%" % (100 * x)


def main():
    e1 = J["e1_stationary"]
    e2 = J["e2_transitions"]
    e2c = J["e2c_volatility_robustness"]
    e3 = J["e3_traces"]

    def e1_flat(model, size="512MiB"):
        rows = [r for r in e1 if r["model"] == model and r["file_size"] == size]
        return sorted({r["argmin"] for r in rows})

    causal_gains = [r["causal_gain"] for r in e2 if r["case"] != "stable_to_volatile"]
    oracle_gains = [r["oracle_gain"] for r in e2 if r["case"] != "stable_to_volatile"]
    e2c_causal = [r["causal_from_16MiB_gain"] for r in e2c] + [r["causal_from_4MiB_gain"] for r in e2c]
    e2c_oracle = [r["oracle_gain"] for r in e2c]
    e3_causal = [r["causal_gain"] for r in e3]
    stationary_all = [r["stationary_causal_gain"] for r in e2] + [r["stationary_causal_gain"] for r in e2c] + [r["stationary_causal_gain"] for r in e3]

    L = []
    a = L.append
    a("# Adaptive-S Mechanism Pivot — Analytical Screening Report")
    a("")
    a("**ANALYTICAL SIMULATION ONLY.** No empirical network experiment was run: no downloads, no")
    a("mm-link, no server/client processes, no tc/netem, no packet replay. All results are")
    a("deterministic simulator output under the stated models.")
    a("")
    a("Generated: %s" % time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
    a("")
    a("## 1. Why the static mechanism was closed")
    a("")
    a("The frozen v7 static mechanism (per-request rate R + fixed RTT D) was closed by the")
    a("pre-Stage-1 scaling/headroom analysis (`salvage_s_scaling_analysis`, §22.85): its frozen")
    a("switching strategies have non-positive relative gain at every file size, and even an")
    a("optimally timed single switch stays <= +0.35% — far below the frozen 5% practical")
    a("threshold. No Stage 1 confirmatory run was performed and none is justified under that")
    a("mechanism.")
    a("")
    a("## 2. What Stage 0S established")
    a("")
    a("Stage 0S (32/32 clean, GO) established that the static mechanism was realized as")
    a("designed: per-request service times matched D+s/R within ~2.5%, the tail window was")
    a("present, and the run was reliable. It was a feasibility gate only; it did not test")
    a("adaptation.")
    a("")
    a("## 3. What the file-size scaling analysis established")
    a("")
    a("Relative headroom does not increase with file size (the bounded one-time tail effect is")
    a("diluted by the growing bulk); fixed-S normalized costs are scale-invariant")
    a("(`T_S/T_ref = 1 + R*D/s`); increasing the file size does not rescue the relative gain.")
    a("")
    a("## 4. Candidate mechanism inventory")
    a("")
    a("| id | mechanism | screened model | infrastructure |")
    a("|---|---|---|---|")
    for m in J["mechanism_inventory"]:
        a("| %s | %s | %s | %s |" % (m["id"], m["name"], m["screened_model"], m["supported_by_infrastructure"]))
    a("")
    a("## 5. Real-trace suitability assessment")
    a("")
    a("Native Mahimahi delivery-opportunity traces (capacity schedules; mm-link loops at EOF):")
    a("")
    a("| family | duration s | mean KiB/s | p90/p10 | frac<25% median | eligible |")
    a("|---|---|---|---|---|---|")
    for t in INV["native_delivery_opportunity"]:
        a("| %s | %.1f | %.1f | %.1f | %.2f | %s |" % (
            t["family"], t["duration_s"], t["mean_bytes_per_s"] / 1024, t["p90_over_p10"],
            t["frac_bins_below_25pct_median"], t["full_length_ge_450s"]))
    a("")
    a("All eight traces show strong genuine time-varying capacity (p90/p10 = 5.5-27.4; 4-18% of")
    a("seconds below 25% of the median). The 23 throughput-trajectory traces (NYU-METS, UGent)")
    a("are measured end-to-end throughput, NOT path capacity; the M3 study showed an aggregate")
    a("throughput envelope imposes the same ceiling on every concurrency level, so they cannot")
    a("represent a request-granularity problem and were used only for inventory context.")
    a("")
    a("## 6. Analytical model")
    a("")
    a("Deterministic event-driven finite-file scheduler simulator (`tools/mechanism_sim.py`):")
    a("N=4 work-conserving workers; contiguous ranges allocated in order; future-only chunk size;")
    a("in-flight requests never resized; request completion depends on the capacity experienced")
    a("throughout the request (piecewise-constant profiles; 1 s bins for traces, zero bins at the")
    a("documented 8000 bps M3 outage floor). Service models: `shared` (aggregate capacity shared")
    a("equally among active transfers; in-flight tracking), `lockin_full` (per-request rate fixed")
    a("at request start and held; the existing server's scheduled-rate semantics), `serialized`")
    a("(one request at a time), `constant` (the closed v7 control).")
    a("")
    a("## 7. Moving-optimum test (stationary states)")
    a("")
    a("argmin_S T(S | constant rate) over rates 0.25-8 MB/s aggregate and file sizes 512/520/544 MiB:")
    a("")
    a("| model | preferred S at F=512 MiB across all rates | 520 MiB | 544 MiB |")
    a("|---|---|---|---|")
    for model in ["shared", "lockin_full", "serialized"]:
        a("| %s | %s | %s | %s |" % (model, ", ".join(e1_flat(model, "512MiB")),
                                     ", ".join(e1_flat(model, "520MiB")), ", ".join(e1_flat(model, "544MiB"))))
    a("")
    a("**The preferred S does not change between stationary rate states for any model.** The")
    a("shared model prefers 16 MiB at every rate; the lock-in model also prefers 16 MiB at 512 MiB")
    a("(the 520/544 MiB variation is a file-size-residue effect, which criterion 4 disqualifies);")
    a("the serialized model has a fixed 256 KiB preference. The real-trace regime test (low-rate")
    a("vs high-rate terciles) likewise gives 16 MiB in both regimes for every trace.")
    a("")
    a("## 8. Causal stationary-vs-changing comparison")
    a("")
    a("Within-download transition cases (low->high, high->low, mild) and stable->volatile cases:")
    a("")
    a("| experiment | causal gain range | oracle gain range | stationary-control gain |")
    a("|---|---|---|---|")
    a("| E2 transitions (all models) | %s .. %s | %s .. %s | %s .. %s |" % (
        pct(min(causal_gains)), pct(max(causal_gains)), pct(min(oracle_gains)), pct(max(oracle_gains)),
        pct(min(r["stationary_causal_gain"] for r in e2)), pct(max(r["stationary_causal_gain"] for r in e2))))
    a("| E2c stable->volatile (lock-in) | %s .. %s | %s .. %s | 0.000%% .. 0.000%% |" % (
        pct(min(e2c_causal)), pct(max(e2c_causal)), pct(min(e2c_oracle)), pct(max(e2c_oracle))))
    a("| E3 real traces (all models) | %s .. %s | n/a | %s .. %s |" % (
        pct(min(e3_causal)), pct(max(e3_causal)),
        pct(min(r["stationary_causal_gain"] for r in e3)), pct(max(r["stationary_causal_gain"] for r in e3))))
    a("")
    a("Causal, observation-driven switches are **zero or harmful** in every case: under the")
    a("shared and serialized models they never fire (no state-dependent preference); under the")
    a("lock-in model they fire after the volatility is already visible and then underperform the")
    a("best fixed arm by 2-61%. In stationary controls every causal gain is exactly 0.000%.")
    a("")
    a("## 9. Headroom analysis")
    a("")
    a("The only positive within-download opportunity found is the **clairvoyant** single switch in")
    a("the lock-in stable->volatile case: oracle gains of 0-32%, varying with the volatility")
    a("period (0% at 60 s, 7-23% at 17 s, 11-32% at 5 s) and mode. Because it requires perfect")
    a("knowledge of the alternation phase and vanishes at longer periods, it is a fragile")
    a("diagnostic artifact, not a causal opportunity. The strongest robust statement is:")
    a("**no screened mechanism provides a causal within-download S-switch headroom above 0%.**")
    a("")
    a("## 10. Robustness to parameter variation")
    a("")
    a("The lock-in oracle headroom is highly parameter-sensitive (period and phase of the")
    a("alternation; stable-phase fraction), and the preferred S at non-aligned file sizes is a")
    a("residue effect. No candidate remained present across the parameter range; the only")
    a("parameter-robust preference is the fixed 16 MiB (shared/lock-in at aligned files).")
    a("")
    a("## 11. Candidate classification (frozen five-condition rule)")
    a("")
    a("| candidate | 1 preferred S changes between states | 2 transition within a download | 3 causal adaptation can react | 4 not residue/phase dependent | 5 weakens when stationary | classification |")
    a("|---|---|---|---|---|---|---|")
    a("| A/B/C/D shared time-varying capacity | FAIL (16 MiB at every rate/regime) | PASS | FAIL (0.000%) | PASS | PASS | **MECHANISTICALLY INSUFFICIENT** |")
    a("| E server serialization / concurrency-dependent rate | FAIL (fixed 256 KiB) | PASS | FAIL (0.000%) | PASS | PASS | **MECHANISTICALLY INSUFFICIENT** |")
    a("| F per-request lock-in + volatility | FAIL (16 MiB at every stationary rate) | PASS | FAIL (-2% to -61%) | FAIL (period/phase dependent) | PASS | **MECHANISTICALLY INSUFFICIENT** |")
    a("")
    a("No candidate satisfies the five conditions. No mechanism was accepted on the basis of a")
    a("favorable gain.")
    a("")
    a("## 12. Recommended next mechanism")
    a("")
    a("**None from the screened set.** The natural trace-driven mechanisms do not create a")
    a("state-dependent preferred S, and the mechanisms that can create a large S-effect (lock-in")
    a("under volatility) are not causally exploitable within one download. No new preregistered")
    a("protocol is warranted under the tested mechanisms. A future study would require a")
    a("mechanism where the optimal S robustly changes between observable states and a causal")
    a("policy captures a meaningful fraction of the clairvoyant bound.")
    a("")
    a("## 13. Exact additional evidence required before experimental execution")
    a("")
    a("1. A documented or measured mechanism with a robust, reproducible state-dependent argmin_S")
    a("   (not residue- or phase-dependent).")
    a("2. A causal (non-clairvoyant) policy that captures a material fraction of the oracle bound")
    a("   in simulation, with the policy fixed before execution.")
    a("3. A trace/parameter population screened by a pre-declared technical eligibility rule,")
    a("   summarized across all eligible items (no selection by adaptive outcome).")
    a("4. A new preregistered protocol at a new path; the frozen v7 protocol must not be modified.")
    a("")
    a("## 14. Limitations")
    a("")
    a("- Deterministic simulator; no timing noise, TCP dynamics, or per-connection scheduling.")
    a("- The lock-in model's per-request fixed rate is the existing server's scheduled-rate")
    a("  semantics; it is a coarse model of real congestion-control adaptation.")
    a("- Trace profiles are 1 s bins with the documented outage floor; sub-second burstiness is")
    a("  not modeled.")
    a("- The clairvoyant oracle is a diagnostic upper bound only and is not a policy.")
    a("")
    a("## Paper-scope test")
    a("")
    a("No appropriately narrow claim can be made: the screened mechanisms do not support a")
    a("statement that chunk size becomes state-dependent within a download in a causally")
    a("exploitable way. No RL, universal, joint N+S, aria2, or real-world claim is made.")
    a("")
    a("## Statement")
    a("")
    a("Zero empirical network experiments were performed for this analysis.")
    os.makedirs(os.path.join(BASE, "reports"), exist_ok=True)
    open(os.path.join(BASE, "reports", "mechanism_pivot_report.md"), "w").write("\n".join(L) + "\n")

    manifest = {
        "study": "salvage_s_mechanism_pivot",
        "kind": "analytical screening (no empirical experiments)",
        "created_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "context": {"closed_static_protocol": "salvage_s_protocol_v7 (unmodified)",
                    "stage0s": "testdata/salvage_s_stage0s", "scaling": "testdata/salvage_s_scaling_analysis"},
        "mechanism_parameters_reference": {"R_bytes_per_s": 500000, "D_seconds": 0.040, "N": 4},
        "chunk_arms_bytes": {l: s for l, s in [("256KiB", 256 * 1024), ("1MiB", 1 * MIB),
                                               ("4MiB", 4 * MIB), ("16MiB", 16 * MIB)]},
        "models": ["shared", "lockin_full", "serialized", "constant"],
        "trace_inventory": {"native_delivery_opportunity": len(INV["native_delivery_opportunity"]),
                            "throughput_trajectory": len(INV["throughput_trajectory"])},
        "experiments": ["E1 stationary argmin", "E2 transitions", "E2b stable->volatile",
                        "E2c volatility robustness", "E3 real traces"],
        "rng": {"stochastic": False, "deterministic": True,
                "pseudo_random_alternation_seed": 12345},
        "tools": {"trace_inventory": "tools/trace_inventory.py", "simulator": "tools/mechanism_sim.py",
                  "screening": "tools/run_screening.py", "figures": "tools/make_figures.py",
                  "report": "tools/make_report.py"},
        "classification": {"A/B/C/D": "MECHANISTICALLY INSUFFICIENT", "E": "MECHANISTICALLY INSUFFICIENT",
                           "F": "MECHANISTICALLY INSUFFICIENT"},
        "new_preregistered_protocol_warranted": False,
    }
    os.makedirs(os.path.join(BASE, "manifest"), exist_ok=True)
    json.dump(manifest, open(os.path.join(BASE, "manifest", "manifest.json"), "w"), indent=2)
    print("wrote reports/mechanism_pivot_report.md and manifest/manifest.json")
    print("causal gains range:", pct(min(causal_gains + e2c_causal + e3_causal)), "..",
          pct(max(causal_gains + e2c_causal + e3_causal)))
    print("oracle gains range:", pct(min(oracle_gains + e2c_oracle)), "..", pct(max(oracle_gains + e2c_oracle)))
    print("stationary gains range:", pct(min(stationary_all)), "..", pct(max(stationary_all)))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
