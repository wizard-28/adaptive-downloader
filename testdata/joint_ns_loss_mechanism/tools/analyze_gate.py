#!/usr/bin/env python3
"""Final analysis of the correlated-loss / range-retry analytical gate."""
import json
import os
import statistics
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import loss_sim as L  # noqa: E402

BASE = "/workspaces/mvp/testdata/joint_ns_loss_mechanism"
J = json.load(open(os.path.join(BASE, "analytical_gate", "stationary_screen.json")))
SW = json.load(open(os.path.join(BASE, "analytical_gate", "robustness_sweep.json")))
MIB = L.MIB


def s_metrics(cond, s):
    cells = J["conditions"][cond]["cells"]
    attempts = 0
    failures = 0
    retries = 0
    retry_bytes = 0
    requests = 0
    aborted = 0
    nseeds = 0
    for n in L.N_GRID:
        c = cells["%d/%d" % (n, s)]
        for r in c["seed_runs"]:
            nseeds += 1
            if r["failed"]:
                aborted += 1
            attempts += r["requests"] + r["failures"]
            failures += r["failures"]
            retries += r["retries"]
            retry_bytes += r["retry_bytes"]
            requests += r["requests"]
    return {
        "requests_per_download": L.FILE // s,
        "packets_per_request": s // L.MSS,
        "per_attempt_failure_rate": failures / attempts if attempts else 0.0,
        "retries_per_download": retries / nseeds if nseeds else 0.0,
        "retry_bytes_per_download_MiB": retry_bytes / nseeds / MIB if nseeds else 0.0,
        "download_abort_rate": aborted / nseeds if nseeds else 0.0,
    }


def n_metrics(cond):
    cells = J["conditions"][cond]["cells"]
    out = {}
    for n in L.N_GRID:
        comps = []
        goodputs = []
        stalls = []
        bursts = []
        fails = []
        for s in L.S_GRID:
            c = cells["%d/%d" % (n, s)]
            if c["median_completion"] is not None:
                comps.append(c["median_completion"])
                goodputs.append(c["median_goodput"])
            stalls.append(c["median_stall_time"])
            bursts.append(c["median_burst_events"])
            fails.append(c["median_failures"])
        out[str(n)] = {
            "min_completion_s": min(comps) if comps else None,
            "median_completion_s": statistics.median(comps) if comps else None,
            "median_goodput_MiBps": statistics.median(goodputs) if goodputs else None,
            "median_stall_time_s": statistics.median(stalls),
            "median_burst_events": statistics.median(bursts),
            "median_failures": statistics.median(fails),
        }
    return out


def main():
    out = {"conditions": {}, "materiality": {}, "gate": {}}
    for cond in ["GOOD", "BAD"]:
        out["conditions"][cond] = {
            "loss_summary": J["conditions"][cond]["loss_summary"],
            "optimum": J["conditions"][cond]["optimum"],
            "S_retry": {str(s): s_metrics(cond, s) for s in L.S_GRID},
            "N": n_metrics(cond),
        }

    # materiality: cost of using the other condition's optimum S at the best N
    def cell(cond, n, s):
        return J["conditions"][cond]["cells"]["%d/%d" % (n, s)]

    opt_good = J["conditions"]["GOOD"]["optimum"]
    opt_bad = J["conditions"]["BAD"]["optimum"]
    good_s, bad_s = opt_good["S"], opt_bad["S"]
    # at N=16 (the N* of both conditions)
    t_good_with_goodS = cell("GOOD", 16, good_s)["median_completion"]
    t_good_with_badS = cell("GOOD", 16, bad_s)["median_completion"]
    t_bad_with_badS = cell("BAD", 16, bad_s)["median_completion"]
    t_bad_with_goodS = cell("BAD", 16, good_s)["median_completion"]
    out["materiality"] = {
        "S_star_GOOD": good_s, "S_star_BAD": bad_s,
        "N_star_GOOD": opt_good["N"], "N_star_BAD": opt_bad["N"],
        "GOOD_at_N16": {"S_star": t_good_with_goodS, "other_S": t_good_with_badS,
                        "penalty": (t_good_with_badS - t_good_with_goodS) / t_good_with_goodS},
        "BAD_at_N16": {"S_star": t_bad_with_badS, "other_S": t_bad_with_goodS,
                       "penalty": (t_bad_with_goodS - t_bad_with_badS) / t_bad_with_badS},
        "threshold": 0.05,
    }

    # robustness summary
    rob = {}
    for name, v in SW["variants"].items():
        row = {}
        for cond in ["GOOD", "BAD"]:
            e = v[cond]
            row[cond] = {"optimum_N": e["optimum"]["N"] if e["optimum"] else None,
                         "optimum_S": e["optimum"]["S"] if e["optimum"] else None,
                         "monotone_decreasing_in_N": e["monotone_decreasing_in_N"]}
        rob[name] = row
    out["robustness"] = rob
    n_variants = len(rob)
    n_nstar16 = sum(1 for r in rob.values() if r["GOOD"]["optimum_N"] == 16 and r["BAD"]["optimum_N"] == 16)
    n_smove = sum(1 for r in rob.values() if r["GOOD"]["optimum_S"] != r["BAD"]["optimum_S"])
    n_monotone = sum(1 for r in rob.values() if r["GOOD"]["monotone_decreasing_in_N"] and r["BAD"]["monotone_decreasing_in_N"])
    out["robustness_summary"] = {
        "variants": n_variants,
        "N_star_16_in_both_conditions": n_nstar16,
        "S_star_moves_between_conditions": n_smove,
        "N_monotone_in_both_conditions": n_monotone,
    }

    out["gate"] = J["gate"]
    out["verdict"] = J["verdict"]
    json.dump(out, open(os.path.join(BASE, "analysis", "analytical_gate_analysis.json"), "w"), indent=2)

    print("=== S-retry exposure (BAD) ===")
    for s in L.S_GRID:
        m = out["conditions"]["BAD"]["S_retry"][str(s)]
        print("  S=%-7s reqs=%-5d pkt/req=%-5d fail/attempt=%.4f retries/dl=%.1f retry_MiB/dl=%.1f abort=%.2f" % (
            "%dK" % (s // 1024), m["requests_per_download"], m["packets_per_request"],
            m["per_attempt_failure_rate"], m["retries_per_download"],
            m["retry_bytes_per_download_MiB"], m["download_abort_rate"]))
    print("=== materiality ===")
    m = out["materiality"]
    print("  GOOD at N=16: S*=16M %.1fs vs 4M %.1fs (penalty %+.1f%%)" % (
        m["GOOD_at_N16"]["S_star"], m["GOOD_at_N16"]["other_S"], 100 * m["GOOD_at_N16"]["penalty"]))
    print("  BAD  at N=16: S*=4M %.1fs vs 16M %.1fs (penalty %+.1f%%)" % (
        m["BAD_at_N16"]["S_star"], m["BAD_at_N16"]["other_S"], 100 * m["BAD_at_N16"]["penalty"]))
    print("=== robustness ===")
    print(" ", out["robustness_summary"])
    print("VERDICT:", out["verdict"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
