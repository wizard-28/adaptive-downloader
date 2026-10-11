#!/usr/bin/env python3
"""Predeclared robustness sweep for the correlated-loss screen.

Variants (frozen in parameter_grounding/parameter_grounding.md):
  T_B=1 s with p_B=1.0; T_B=5 s; T_G=10 s; T_G=60 s; g=0.25; g=0.5;
  R=1250 KiB/s; R=2500 KiB/s.
Each variant is evaluated for the GOOD (T_B=0.15 s) and BAD (T_B=2 s)
conditions with 4 seeds per cell; the primary parameters are unchanged.
"""
import json
import os
import statistics
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import loss_sim as L  # noqa: E402

BASE = "/workspaces/mvp/testdata/joint_ns_loss_mechanism"
OUT = os.path.join(BASE, "analytical_gate")
KIB = L.KIB
SEEDS = [1, 2, 3, 4]

VARIANTS = [
    ("primary", {}),
    ("T_B=1_p_B=1.0", {"t_bad": 1.0, "p_bad": 1.0}),
    ("T_B=5", {"t_bad": 5.0}),
    ("T_G=10", {"t_good": 10.0}),
    ("T_G=60", {"t_good": 60.0}),
    ("g=0.25", {"g_bad": 0.25}),
    ("g=0.5", {"g_bad": 0.5}),
    ("R=1250K", {"rate": 1250 * KIB}),
    ("R=2500K", {"rate": 2500 * KIB}),
]


def cell_median(params, n, s, seeds):
    comps = []
    fails = 0
    for seed in seeds:
        proc = L.LossProcess(params, seed=seed)
        r = L.run_cell(proc, params, n, s)
        if r["failed"]:
            fails += 1
        else:
            comps.append(r["completion"])
    return (statistics.median(comps) if comps else None, fails, len(seeds))


def main():
    out = {"variants": {}, "seeds": SEEDS}
    for name, over in VARIANTS:
        out["variants"][name] = {}
        for cond, tbad in [("GOOD", 0.15), ("BAD", 2.0)]:
            kw = dict(t_bad=tbad)
            kw.update(over)
            params = L.Params(**kw)
            cells = {}
            for n in L.N_GRID:
                for s in L.S_GRID:
                    med, fails, nseeds = cell_median(params, n, s, SEEDS)
                    cells["%d/%d" % (n, s)] = {"median_completion": med, "failures": fails,
                                               "seeds": nseeds,
                                               "viable": fails <= nseeds // 2}
            viable = {k: v for k, v in cells.items() if v["viable"] and v["median_completion"]}
            best = min(viable.items(), key=lambda kv: kv[1]["median_completion"]) if viable else None
            nstar = best[1]["median_completion"] is not None and int(best[0].split("/")[0]) if best else None
            # N monotonicity on min completion across S
            mins = []
            for n in L.N_GRID:
                vals = [cells["%d/%d" % (n, s)]["median_completion"] for s in L.S_GRID
                        if cells["%d/%d" % (n, s)]["viable"] and cells["%d/%d" % (n, s)]["median_completion"]]
                mins.append(min(vals) if vals else None)
            finite = [m for m in mins if m is not None]
            out["variants"][name][cond] = {
                "params": params.dict(), "cells": cells,
                "optimum": {"key": best[0], "N": int(best[0].split("/")[0]),
                            "S": int(best[0].split("/")[1]),
                            "completion": best[1]["median_completion"]} if best else None,
                "min_completion_by_N": {str(n): m for n, m in zip(L.N_GRID, mins)},
                "monotone_decreasing_in_N": all(finite[i] >= finite[i + 1] - 1e-9
                                                for i in range(len(finite) - 1)),
            }
            print("%-16s %-4s optimum %s monotone_N=%s" % (
                name, cond, out["variants"][name][cond]["optimum"],
                out["variants"][name][cond]["monotone_decreasing_in_N"]), flush=True)
    json.dump(out, open(os.path.join(OUT, "robustness_sweep.json"), "w"), indent=2)
    print("sweep written")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
