#!/usr/bin/env python3
"""EXPLORATORY combined-mechanism screen (soft N-dependent server throttle +
phase-locked per-response pacing).

This is a design exploration, not the frozen pre-screen: it was run to check
whether the mechanism class the proposal promised ("configurable N-dependent
server throttling") can create a clean state-dependent JOINT (N*,S*) movement
when combined with the time-varying per-response pacing schedule. The frozen
pre-screen (with a predeclared family and selection rule) will be run
separately and will report all candidates.

Model: rate_eff = min(schedule.rate(start), C/(k*(1+gamma*(k-1)))) with k the
in-flight request count at the request's server start (see combined_sim.py).
"""
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import combined_sim as cs  # noqa: E402
from combined_sim import (Params, ServerState, d2_anchor, two_phase,
                          fixed_frontier, N_GRID, S_GRID, FILE_SIZE, MIB)  # noqa: E402

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
OUT = os.path.join(BASE, "analysis")

SCHEDULES = {"D2": d2_anchor(), "T=1": two_phase(1.0), "T=4": two_phase(4.0)}
CAPS = {"none": ServerState(0, 0)}
for c in (8, 16, 32):
    for g in (0.02, 0.05, 0.10):
        CAPS["C=%dM_g=%.2f" % (c, g)] = ServerState(c * MIB, g)


def main():
    params = Params()
    out = {"note": "exploratory; frozen pre-screen to follow", "grid": {"N": N_GRID, "S": S_GRID,
           "file_bytes": FILE_SIZE}, "states": {}, "pairs": []}
    frontiers = {}
    for sname, sched in SCHEDULES.items():
        for cname, st in CAPS.items():
            fr = fixed_frontier(FILE_SIZE, sched, params, state=st)
            frontiers[(sname, cname)] = fr
            o = cs._optima(fr)
            out["states"]["%s|%s" % (sname, cname)] = o
        base = cs._optima(frontiers[(sname, "none")])
        for cname in CAPS:
            if cname == "none":
                continue
            o = cs._optima(frontiers[(sname, cname)])
            pen_cap = (frontiers[(sname, cname)][(base["N"], base["S"])] - o["t"]) / o["t"]
            pen_base = (frontiers[(sname, "none")][(o["N"], o["S"])] - base["t"]) / base["t"]
            checks = {
                "N_moves": base["N"] != o["N"],
                "S_moves": base["S"] != o["S"],
                "N_margin_ge_5pct": (o["N_margin"] or 0) >= 0.05,
                "S_margin_ge_5pct": (o["S_margin"] or 0) >= 0.05,
                "material_cap_side": pen_cap >= 0.10,
                "material_base_side": pen_base >= 0.10,
            }
            pair = {"schedule": sname, "cap": cname, "base_opt": base, "cap_opt": o,
                    "penalty_cap_uses_base_opt": pen_cap, "penalty_base_uses_cap_opt": pen_base,
                    "checks": checks, "qualifies": all(checks.values())}
            out["pairs"].append(pair)
    qual = [p for p in out["pairs"] if p["qualifies"]]
    out["qualified"] = [{"schedule": p["schedule"], "cap": p["cap"],
                         "base": [p["base_opt"]["N"], p["base_opt"]["S"]],
                         "cap_opt": [p["cap_opt"]["N"], p["cap_opt"]["S"]],
                         "pen_cap": p["penalty_cap_uses_base_opt"],
                         "pen_base": p["penalty_base_uses_cap_opt"]} for p in qual]
    json.dump(out, open(os.path.join(OUT, "exploratory_combined_soft_cap.json"), "w"), indent=2)
    print("qualifying (schedule, cap) pairs: %d / %d" % (len(qual), len(out["pairs"])))
    for p in out["qualified"]:
        print("  %-4s %-14s (N,S) %s -> %s  cap-side +%.1f%% base-side +%.1f%%" % (
            p["schedule"], p["cap"], p["base"], p["cap_opt"],
            100 * p["pen_cap"], 100 * p["pen_base"]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
