#!/usr/bin/env python3
"""Generate the adaptive-N mechanism report and manifest from the screening outputs."""
import json
import os
import time

BASE = "/workspaces/mvp/testdata/adaptive_n_mechanism_screen"
J = json.load(open(os.path.join(BASE, "analysis", "screening.json")))
MIB = 1024 ** 2


def pct(x):
    return "%+.3f%%" % (100 * x)


def main():
    e1, e2, e3, e4 = J["e1_stationary"], J["e2_transitions"], J["e3_robustness"], J["e4_severity"]
    causal_all = [r["causal_gain"] for r in e2] + [r["causal_gain"] for r in e4]
    oracle_all = [r["oracle_gain"] for r in e2] + [r["oracle_gain"] for r in e4]
    stat_all = [r["stationary_causal_gain"] for r in e2]
    exceed5 = [g for g in causal_all if g >= 0.05]
    causal_sorted = sorted(causal_all)
    median_causal = causal_sorted[len(causal_sorted) // 2]

    def argmins(model):
        return {r["state"]: r["argmin_N"] for r in e1 if r["model"] == model}

    L = []
    a = L.append
    a("# Adaptive-N Mechanism Screen — Analytical Report")
    a("")
    a("**ANALYTICAL SIMULATION ONLY.** No empirical network experiment was run: no downloads, no")
    a("mm-link, no server/client processes, no tc/netem, no replay. All numbers are deterministic")
    a("simulator output and are not empirical evidence.")
    a("")
    a("Generated: %s" % time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
    a("")
    a("## 1. Current S-study closure")
    a("")
    a("The static chunk-size (S) mechanism is closed for confirmation: Stage 0S passed its")
    a("feasibility gate, but the scaling analysis showed the frozen S switches have non-positive")
    a("relative gain at every file size, and the mechanism-pivot screen found no robust")
    a("state-dependent S optimum (all candidates MECHANISTICALLY INSUFFICIENT). No S confirmatory")
    a("run was performed. The S evidence is untouched by this study.")
    a("")
    a("## 2. Previous concurrency evidence (unchanged)")
    a("")
    a("- **T1 (live per-response pacing):** aggregate throughput scales monotonically with C in")
    a("  every phase; no live concurrency reversal at equal 1 MiB granularity (GATE B FAIL).")
    a("- **L3 (static per-phase sweep):** phase optima vary, but the action-constrained oracle")
    a("  reached only 8.1% simulated headroom (< 10% gate) -> STOP.")
    a("- **L4 (simulated over-concurrency penalty):** 17.6% simulated headroom, but the live")
    a("  representative sweep did NOT reproduce the penalty (GATE E FAIL;")
    a("  `NOT_READY_LIVE_MISMATCH`). This is decisive for the current screen.")
    a("- **M3 (aggregate throughput envelope replay):** every C is capped at the same instantaneous")
    a("  rate; no C sensitivity.")
    a("")
    a("## 3. Candidate mechanisms")
    a("")
    a("| id | mechanism | screened model | live grounding |")
    a("|---|---|---|---|")
    for m in J["mechanism_inventory"]:
        a("| %s | %s | %s | %s |" % (m["id"], m["name"], m["screened_model"], m["live_evidence"]))
    a("")
    a("## 4. Trace inventory")
    a("")
    a("The 8 native Mahimahi delivery-opportunity traces represent time-varying **aggregate")
    a("capacity** (strong variability, p90/p10 5.5-27.4). They do **not** contain per-flow")
    a("fairness, queueing, or per-connection cost information, so they cannot establish a")
    a("concurrency-sensitive mechanism. The 23 throughput-trajectory traces (NYU-METS/UGent) are")
    a("measured end-to-end throughput, not capacity, and the M3 envelope limitation applies.")
    a("**No existing trace establishes the necessary concurrency mechanism**; the mechanism must")
    a("come from the server/system model.")
    a("")
    a("## 5. Analytical models")
    a("")
    a("Deterministic event-driven finite-file simulator (`tools/n_sim.py`): N workers, one")
    a("contiguous 1 MiB request per worker, F = 512 MiB, per-request RTT 40 ms; request transfer")
    a("rates depend on the current number of active requests and the current state (in-flight")
    a("requests experience state evolution). Models: `direct` (fixed per-request rate; T1"),
    a("`fair` (aggregate C shared among k flows), `fair_bg` (fair sharing with B background"),
    a("`cap` (server parallelism K with FIFO wait), `degrade` (server aggregate capacity")
    a("C/(1+gamma*(k-1))). N changes add workers or retire them (in-flight requests finish).")
    a("")
    a("## 6. Stationary N optima")
    a("")
    a("| model | preferred N across states | state-dependent? |")
    a("|---|---|---|")
    a("| direct (R = 0.25-2 MB/s) | 16 at every R | no (monotone) |")
    a("| fair (C = 0.5-32 MB/s) | 16 at every C | no (monotone) |")
    a("| fair_bg (B = 0, 4, 16) | 16 at every B | no (monotone) |")
    a("| cap (K = 1, 2, 4, 8) | %s | yes (tracks the plateau edge; completion flat above it) |" % argmins("cap"))
    a("| degrade (C x gamma grid) | %s | yes (interior optimum rises with C, shifts with gamma) |" % argmins("degrade"))
    a("")
    a("Only `cap` and `degrade` show a state-dependent preferred N. `cap` is a plateau (the")
    a("model-implied best fixed N is already robust across states), while `degrade` has a true")
    a("interior optimum whose location depends on the state.")
    a("")
    a("## 7. Within-download state transitions")
    a("")
    a("| case | state-1 opt N | state-2 opt N | best fixed N | causal gain | oracle gain |")
    a("|---|---|---|---|---|---|")
    for r in e2:
        a("| %s | %s | %s | %s | %s | %s |" % (r["case"], r["state1_opt_N"], r["state2_opt_N"],
                                                 r["reference_fixed_N"], pct(r["causal_gain"]), pct(r["oracle_gain"])))
    a("")
    a("## 8. Causal adaptive-N results")
    a("")
    a("The causal policy uses only observed completed-request throughput (median of the last 3")
    a("samples) and one switch per download. Results: causal gains range %s .. %s across all"
      % (pct(min(causal_all)), pct(max(causal_all))))
    a("transition cases; median %s. The `cap` model gives no causal gain (plateau; the state is"
      % pct(median_causal))
    a("not observable in the per-request rate), and one `cap` case is strongly negative because")
    a("the causal policy mis-times an unobservable change.")
    a("")
    a("## 9. Oracle comparison")
    a("")
    a("The oracle is a coarse-grid diagnostic (one future-only switch with perfect knowledge) and")
    a("is not a policy. Oracle gains range %s .. %s. The causal/oracle capture ratio is"
      % (pct(min(oracle_all)), pct(max(oracle_all))))
    a("inconsistent: near 100% in severe `degrade` transitions (ratios >= 8) but far lower in")
    a("mild transitions and in the gamma transition (0.5% vs 17.4%).")
    a("")
    a("## 10. Parameter robustness")
    a("")
    a("Predeclared sweep: C in {0.5, 2, 8, 32} MB/s x gamma in {0.005, 0.01, 0.02, 0.05, 0.1,")
    a("0.2}. The `degrade` preferred N is state-dependent for **every** gamma (argmins over C:")
    a("")
    for g, v in sorted(J["e3_summary"]["degrade_robustness"].items()):
        a("- %s: %s" % (g, v["argmins_over_C"]))
    a("")
    a("So the analytical state-dependence is broad, not narrow. However the mechanism itself")
    a("requires a server-side per-connection overhead (gamma) that is **not present in the")
    a("current controlled infrastructure** and was already rejected live (L4 GATE E).")
    a("")
    a("## 11. Stationary-vs-changing control")
    a("")
    a("In every stationary control the causal gain is %s .. %s (i.e., no intrinsic state-change"
      % (pct(min(stat_all)), pct(max(stat_all))))
    a("advantage); the changing-state gains are zero or positive. The effect therefore weakens")
    a("materially when the state is stationary.")
    a("")
    a("## 12. 5% headroom analysis")
    a("")
    a("- maximum causal headroom: **%s** (degrade, severe capacity drop)" % pct(max(causal_all)))
    a("- median causal headroom: %s" % pct(median_causal))
    a("- maximum oracle headroom: %s" % pct(max(oracle_all)))
    a("- fraction of transition cases with causal gain >= 5%%: %d/%d" % (len(exceed5), len(causal_all)))
    a("- stationary-control headroom: ~0")
    a("")
    a("The 5%% level is reachable **only analytically** in the `degrade` model at severe")
    a("capacity drops; it is not demonstrated live and must not be treated as an empirical result.")
    a("")
    a("## 13. Mechanism classifications")
    a("")
    a("| candidate | state-dependent preferred N | causal adaptation | residue-free | stationary control | parameter sweep | live grounding | classification |")
    a("|---|---|---|---|---|---|---|---|")
    a("| A per-flow sharing (fair) | no | n/a | yes | n/a | n/a | mm-link yes | **MECHANISTICALLY INSUFFICIENT** |")
    a("| B server cap (cap) | yes (plateau edge) | no (0%; plateau) | yes | n/a | n/a | not in the live stack | **MECHANISTICALLY INSUFFICIENT** |")
    a("| C concurrency-dependent server cost (degrade) | yes | yes (up to +6.7%) | yes | yes | yes (all gamma) | **NO (L4 GATE E live mismatch)** | **PLAUSIBLE - FRAGILE** |")
    a("| D background traffic (fair_bg) | no | n/a | yes | n/a | n/a | no live study | **MECHANISTICALLY INSUFFICIENT** |")
    a("| E queueing/contention (cap/degrade) | yes | mixed | yes | yes | yes | NO (L4/T1) | **PLAUSIBLE - FRAGILE** |")
    a("| F server saturation (cap) | yes (plateau edge) | no | yes | n/a | n/a | not live | **MECHANISTICALLY INSUFFICIENT** |")
    a("| G changing capacity + sharing (fair) | no | n/a | yes | n/a | n/a | mm-link yes | **MECHANISTICALLY INSUFFICIENT** |")
    a("| H changing server availability + queueing (cap/degrade) | yes | mixed | yes | yes | yes | NO (L4) | **PLAUSIBLE - FRAGILE** |")
    a("")
    a("The `degrade`-class mechanisms pass the six analytical ROBUST conditions but fail the")
    a("physical-grounding requirement: the only live validation of this mechanism class (L4 GATE")
    a("E) rejected it, and the current controlled server has no concurrency-dependent capacity")
    a("cost. They are therefore classified **FRAGILE**, not ROBUST.")
    a("")
    a("## 14. Does a robust adaptive-N opportunity exist?")
    a("")
    a("**No.** Under the mechanisms available in the current project infrastructure, the")
    a("preferred N is monotone or plateau-flat (N=16), and the only analytically state-dependent")
    a("mechanism (concurrency-dependent server capacity) is not live-grounded and was already")
    a("rejected by live validation. No robust adaptive-N opportunity is established, so no")
    a("empirical protocol is justified at this time.")
    a("")
    a("## 15. Exact requirements for any future experimental protocol")
    a("")
    a("1. **Establish the mechanism live first.** A controlled experiment must show a reproducible")
    a("   concurrency-dependent server/system cost (the current stack does not; L4 GATE E).")
    a("2. **Ground the parameters.** The mechanism's parameters must be measured, not tuned; no")
    a("   parameter may be chosen to produce a desired gain.")
    a("3. **Freeze a causal policy before execution.** One switch per download, adjacent N steps,")
    a("   triggered by an observed-throughput threshold and a pre-registered N mapping.")
    a("4. **Pre-register the control and target.** The model-implied best fixed N as the baseline")
    a("   and the 5% practical reference (not tuned to the result).")
    a("5. **Fresh evidence at a new path.** A new preregistered protocol; the frozen S/oracle/")
    a("   Stage A/B/C artifacts and this screen remain untouched.")
    a("")
    a("## Statement")
    a("")
    a("Zero empirical network experiments were performed for this analysis.")
    os.makedirs(os.path.join(BASE, "reports"), exist_ok=True)
    open(os.path.join(BASE, "reports", "adaptive_n_mechanism_report.md"), "w").write("\n".join(L) + "\n")

    manifest = {
        "study": "adaptive_n_mechanism_screen",
        "kind": "analytical screening (no empirical experiments)",
        "created_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "context": {"s_study": "closed (salvage_s_protocol_v7, salvage_s_stage0s, salvage_s_scaling_analysis, salvage_s_mechanism_pivot)",
                    "concurrency_evidence": ["T1 (testdata/diagnostics/t1_server_throttle)",
                                             "L3/L4 (testdata/diagnostics/l4_environment_design, v7_design)",
                                             "M3 (testdata/diagnostics/m3_mobile_replay)"]},
        "baseline": {"file_bytes": 512 * MIB, "request_bytes": 1 * MIB, "rtt_seconds": 0.040,
                     "N_grid": [1, 2, 4, 8, 16]},
        "models": ["direct", "fair", "fair_bg", "cap", "degrade"],
        "state_parameters": {"C_bytes_per_s": [0.5 * MIB, 2 * MIB, 8 * MIB, 32 * MIB],
                             "K": [1, 2, 4, 8], "B": [0, 4, 16],
                             "gamma": [0.005, 0.01, 0.02, 0.05, 0.1, 0.2]},
        "experiments": ["E1 stationary", "E2 transitions", "E3 robustness", "E4 severity"],
        "rng": {"stochastic": False, "deterministic": True},
        "tools": {"simulator": "tools/n_sim.py", "screening": "tools/run_n_screening.py",
                  "figures": "tools/make_figures.py", "report": "tools/make_report.py"},
        "classification": {"A": "MECHANISTICALLY INSUFFICIENT", "B": "MECHANISTICALLY INSUFFICIENT",
                           "C": "PLAUSIBLE - FRAGILE", "D": "MECHANISTICALLY INSUFFICIENT",
                           "E": "PLAUSIBLE - FRAGILE", "F": "MECHANISTICALLY INSUFFICIENT",
                           "G": "MECHANISTICALLY INSUFFICIENT", "H": "PLAUSIBLE - FRAGILE"},
        "robust_mechanism_found": False,
        "new_preregistered_protocol_justified": False,
    }
    os.makedirs(os.path.join(BASE, "manifest"), exist_ok=True)
    json.dump(manifest, open(os.path.join(BASE, "manifest", "manifest.json"), "w"), indent=2)
    json.dump({"mechanism_inventory": J["mechanism_inventory"],
               "classification": manifest["classification"]},
              open(os.path.join(BASE, "mechanisms", "mechanism_inventory.json"), "w"), indent=2)
    print("wrote reports/adaptive_n_mechanism_report.md, manifest/, mechanisms/")
    print("causal gains:", pct(min(causal_all)), "..", pct(max(causal_all)), "| median", pct(median_causal))
    print("oracle gains:", pct(min(oracle_all)), "..", pct(max(oracle_all)))
    print(">=5% causal cases:", len(exceed5), "/", len(causal_all))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
