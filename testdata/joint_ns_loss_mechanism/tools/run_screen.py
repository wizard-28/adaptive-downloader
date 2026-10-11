#!/usr/bin/env python3
"""Correlated-loss / range-retry mechanism screen (ANALYTICAL ONLY).

Frozen design (parameter_grounding/parameter_grounding.md):
  conditions: GOOD (T_B=0.15 s, overall loss ~0.4 %) and BAD (T_B=2.0 s,
  overall loss ~4.6 %); primary R=640 KiB/s; N in {1,2,4,8,16};
  S in {256K,1M,4M,16M}; F=512 MiB; 10 seeds/cell; frozen retry semantics.
  Viability rule (predeclared): a cell is viable if >= 50 % of its seeds
  complete; optima are taken over viable cells by median completion.
  Robustness sweep (predeclared): T_B {1,5}, p_B {1.0}, T_G {10,60},
  g {0.25,0.5}, R {1250,2500} KiB/s.
"""
import json
import os
import statistics
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import loss_sim as L  # noqa: E402

BASE = "/workspaces/mvp/testdata/joint_ns_loss_mechanism"
OUT = os.path.join(BASE, "analytical_gate")
os.makedirs(OUT, exist_ok=True)
SEEDS = list(range(1, 11))
MIB = L.MIB


def log(msg):
    print(msg, flush=True)


def run_condition(name, params, seeds=SEEDS, grid_n=L.N_GRID, grid_s=L.S_GRID):
    cells = {}
    for n in grid_n:
        for s in grid_s:
            runs = []
            for seed in seeds:
                proc = L.LossProcess(params, seed=seed)
                r = L.run_cell(proc, params, n, s)
                runs.append({"seed": seed, "completion": r["completion"],
                             "failed": r["failed"], "failures": r["failures"],
                             "retries": r["retries"], "retry_bytes": r["retry_bytes"],
                             "goodput": r["goodput"], "stall_time": r["stall_time"],
                             "burst_events": r["burst_events"], "requests": r["requests"]})
            ok = [r for r in runs if not r["failed"]]
            frac = len(ok) / len(runs)
            cells["%d/%d" % (n, s)] = {
                "N": n, "S": s, "seed_runs": runs, "viable": frac >= 0.5,
                "success_fraction": frac,
                "median_completion": statistics.median([r["completion"] for r in ok]) if ok else None,
                "median_failures": statistics.median(r["failures"] for r in runs),
                "median_retries": statistics.median(r["retries"] for r in runs),
                "median_retry_bytes": statistics.median(r["retry_bytes"] for r in runs),
                "median_goodput": statistics.median([r["goodput"] for r in ok]) if ok else 0.0,
                "median_stall_time": statistics.median(r["stall_time"] for r in runs),
                "median_burst_events": statistics.median(r["burst_events"] for r in runs),
                "failure_rate_per_request": (statistics.median(r["failures"] for r in runs)
                                             / max(1, statistics.median(r["requests"] for r in runs))),
            }
    return cells


def optima(cells):
    viable = {k: v for k, v in cells.items() if v["viable"] and v["median_completion"] is not None}
    if not viable:
        return None
    best = min(viable.items(), key=lambda kv: kv[1]["median_completion"])
    second = sorted(viable.items(), key=lambda kv: kv[1]["median_completion"])[1] if len(viable) > 1 else None
    margin = ((second[1]["median_completion"] - best[1]["median_completion"])
              / second[1]["median_completion"]) if second else None
    return {"key": best[0], "N": best[1]["N"], "S": best[1]["S"],
            "completion": best[1]["median_completion"],
            "second_key": second[0] if second else None,
            "second_completion": second[1]["median_completion"] if second else None,
            "margin": margin,
            "viable_cells": len(viable)}


def spearman(xs, ys):
    def rank(v):
        order = sorted(range(len(v)), key=lambda i: v[i])
        r = [0.0] * len(v)
        for pos, i in enumerate(order):
            r[i] = pos + 1.0
        return r
    rx, ry = rank(xs), rank(ys)
    n = len(xs)
    mx, my = sum(rx) / n, sum(ry) / n
    num = sum((rx[i] - mx) * (ry[i] - my) for i in range(n))
    den = (sum((rx[i] - mx) ** 2 for i in range(n)) * sum((ry[i] - my) ** 2 for i in range(n))) ** 0.5
    return num / den if den else 0.0


def main():
    primary = {
        "GOOD": L.Params(t_bad=0.15),
        "BAD": L.Params(t_bad=2.0),
    }
    out = {"params_primary": {k: v.dict() for k, v in primary.items()},
           "seeds": SEEDS, "conditions": {}, "gate": {}}
    for name, params in primary.items():
        log("condition %s: running 20 cells x %d seeds..." % (name, len(SEEDS)))
        cells = run_condition(name, params)
        opt = optima(cells)
        out["conditions"][name] = {"cells": cells, "optimum": opt}
        # loss process summary
        proc = L.LossProcess(params, seed=1)
        gf = proc.state_fraction()
        overall = 1 - (gf * (1 - params.p_good) + (1 - gf) * (1 - params.p_bad))
        out["conditions"][name]["loss_summary"] = {
            "good_state_fraction": gf, "overall_loss": overall,
            "p_good": params.p_good, "p_bad": params.p_bad,
            "t_good": params.t_good, "t_bad": params.t_bad}
        log("  optimum: %s  (margin %s)" % (opt, None if opt is None else "%.1f%%" % (100 * opt["margin"])))

    # mechanism checks
    checks = {"S_retry_exposure": {}, "N_contention": {}}
    for name in primary:
        cells = out["conditions"][name]["cells"]
        # S-retry exposure: aggregate over N and seeds per S
        for s in L.S_GRID:
            cs = [cells["%d/%d" % (n, s)] for n in L.N_GRID]
            fr = statistics.mean(c["failure_rate_per_request"] for c in cs)
            ret = statistics.mean(c["median_retries"] for c in cs)
            rb = statistics.mean(c["median_retry_bytes"] for c in cs)
            checks["S_retry_exposure"].setdefault(name, {})["%d" % s] = {
                "requests_per_download": L.FILE // s,
                "packets_per_request": s // L.MSS,
                "mean_failure_rate_per_request": fr,
                "mean_retries_per_download": ret,
                "mean_retry_bytes_per_download": rb,
            }
        ss = [int(k) for k in checks["S_retry_exposure"][name]]
        frs = [checks["S_retry_exposure"][name][str(s)]["mean_failure_rate_per_request"] for s in ss]
        checks["S_retry_exposure"][name]["spearman_failure_vs_S"] = spearman(ss, frs)
        # N-contention: completion/goodput/stall vs N (aggregate over S and seeds)
        for n in L.N_GRID:
            cs = [cells["%d/%d" % (n, s)] for s in L.S_GRID]
            comps = [c["median_completion"] for c in cs if c["median_completion"] is not None]
            checks["N_contention"].setdefault(name, {})["%d" % n] = {
                "median_completion_across_S": statistics.median(comps) if comps else None,
                "min_completion_across_S": min(comps) if comps else None,
                "median_goodput_across_S": statistics.median(c["median_goodput"] for c in cs),
                "median_stall_time": statistics.median(c["median_stall_time"] for c in cs),
                "median_failures": statistics.median(c["median_failures"] for c in cs),
            }
        comps = [checks["N_contention"][name][str(n)]["min_completion_across_S"] for n in L.N_GRID]
        finite = [c for c in comps if c is not None]
        checks["N_contention"][name]["monotone_decreasing_in_N"] = all(
            finite[i] >= finite[i + 1] - 1e-9 for i in range(len(finite) - 1))
        checks["N_contention"][name]["N_star"] = L.N_GRID[comps.index(min(finite))]

    # predeclared analytical gate (task section 17, conditions 1-3)
    opt_good = out["conditions"]["GOOD"]["optimum"]
    opt_bad = out["conditions"]["BAD"]["optimum"]
    n_differs = opt_good and opt_bad and opt_good["N"] != opt_bad["N"]
    s_differs = opt_good and opt_bad and opt_good["S"] != opt_bad["S"]
    joint_differs = n_differs and s_differs
    out["gate"] = {
        "condition_1_N_star_differs": {"GOOD": opt_good["N"] if opt_good else None,
                                       "BAD": opt_bad["N"] if opt_bad else None,
                                       "pass": bool(n_differs)},
        "condition_2_S_star_differs": {"GOOD": opt_good["S"] if opt_good else None,
                                       "BAD": opt_bad["S"] if opt_bad else None,
                                       "pass": bool(s_differs)},
        "condition_3_joint_differs": {"pass": bool(joint_differs)},
        "mechanism_S_retry": checks["S_retry_exposure"],
        "mechanism_N": checks["N_contention"],
        "note": ("conditions 4-8 (transition, causal detection, causal headroom, "
                 "file-size residue, stationary control) are only evaluated if "
                 "conditions 1-3 pass"),
    }
    out["gate"]["overall"] = "PASS" if joint_differs else "FAIL"
    out["verdict"] = ("JOINT_OPPORTUNITY_CANDIDATE" if joint_differs
                      else "NO_JOINT_N+S_OPPORTUNITY_UNDER_CORRELATED_LOSS")
    json.dump(out, open(os.path.join(OUT, "stationary_screen.json"), "w"), indent=2)

    print("\n=== stationary optima ===")
    for name in primary:
        o = out["conditions"][name]["optimum"]
        print("  %-4s (N*,S*) = (%s,%s)  completion %.1fs  margin %s" % (
            name, o["N"], o["S"], o["completion"],
            "n/a" if o["margin"] is None else "%+.1f%%" % (100 * o["margin"])))
    print("=== S-retry exposure (failure rate per request) ===")
    for name in primary:
        e = checks["S_retry_exposure"][name]
        print("  %-4s " % name + "  ".join("S=%dK: %.4f" % (s // 1024, e[str(s)]["mean_failure_rate_per_request"])
                                           for s in L.S_GRID) + "  (spearman %+.2f)" % e["spearman_failure_vs_S"])
    print("=== N (min completion across S) ===")
    for name in primary:
        e = checks["N_contention"][name]
        print("  %-4s " % name + "  ".join("N=%d: %s" % (n, "fail" if e[str(n)]["min_completion_across_S"] is None
                                                        else "%.0fs" % e[str(n)]["min_completion_across_S"])
                                           for n in L.N_GRID) + "  monotone=%s N*=%d" % (
            e["monotone_decreasing_in_N"], e["N_star"]))
    print("=== gate ===")
    print("  condition 1 (N* differs): %s" % out["gate"]["condition_1_N_star_differs"]["pass"])
    print("  condition 2 (S* differs): %s" % out["gate"]["condition_2_S_star_differs"]["pass"])
    print("  condition 3 (joint differs): %s" % out["gate"]["condition_3_joint_differs"]["pass"])
    print("VERDICT:", out["verdict"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
