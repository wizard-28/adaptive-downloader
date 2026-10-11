#!/usr/bin/env python3
"""Track 2 Phase 1: analytical pre-screen of the N-dependent server family.

Predeclared candidate family (frozen before evaluation):
  GOOD state: rate=2.5 MiB/s, link=0, loss=0.004, congestion=0, overhead=0.02
  DEGRADED candidates: rate in {0.4, 0.5} MiB/s x loss in {0.02, 0.03}
    x congestion in {0.02, 0.04, 0.06} with link=0, plus 4 link variants
    (rate=1.0 MiB/s, link in {2.0, 3.0} MiB/s, congestion in {0.04, 0.06}).

Predeclared selection rule (frozen before evaluation):
  A candidate pair (GOOD, D) is selected iff ALL hold:
    1. N*_GOOD != N*_D and S*_GOOD != S*_D (joint argmins over viable cells);
    2. material: max(penalty_GOOD, penalty_D) >= 0.10, where
       penalty_X = T_X(best_D)/T_X(best_X) - 1 (using the other state's optimum);
    3. separated (coordinate-wise): in each state,
       T(N*,S*) <= 0.95 * min_{N'!=N*} T(N',S*)   (N margin)
       T(N*,S*) <= 0.95 * min_{S'!=S*} T(N*,S')   (S margin);
    4. reachable: the abort rate at each optimum <= 0.20 and the second-best
       distinct cell completes in >= 50% of seeds.
  Among qualifying candidates pick the one maximizing min(penalty_GOOD, penalty_D);
  tie-break larger congestion, then larger rate.
  If none qualifies: STOP TRACK 2.
"""
import json
import os
import statistics
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import ns_sim as sim  # noqa: E402

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
OUT = os.path.join(BASE, "analysis")
os.makedirs(OUT, exist_ok=True)
MIB = sim.MIB
KIB = sim.KIB

N_GRID = [1, 2, 4, 8, 16]
S_GRID = [262144, 1048576, 2097152, 3145728, 4194304, 5242880, 6291456,
          7340032, 8388608, 9437184, 10485760, 11534336, 12582912,
          13631488, 14680064, 15728640, 16777216]
FILE_SIZES = [64 * MIB, 128 * MIB, 256 * MIB]  # predeclared workload scales
FILE = FILE_SIZES[0]  # default (kept for imports)
SEEDS = list(range(1, 21))

GOOD = sim.State("GOOD", rate=int(2.5 * MIB), link=0, loss=0.004, congestion=0.0)

DEGRADED_CANDIDATES = []
for rate_mib in [0.4, 0.5]:
    for loss in [0.02, 0.03]:
        for cong in [0.02, 0.04, 0.06]:
            DEGRADED_CANDIDATES.append(sim.State(
                "D_r%.1f_l%.2f_c%.2f" % (rate_mib, loss, cong),
                rate=int(rate_mib * MIB), link=0, loss=loss, congestion=cong))
for link_mib in [2.0, 3.0]:
    for cong in [0.04, 0.06]:
        DEGRADED_CANDIDATES.append(sim.State(
            "D_link%.1f_c%.2f" % (link_mib, cong),
            rate=int(1.0 * MIB), link=int(link_mib * MIB), loss=0.03, congestion=cong))


def eval_state(st, file_bytes=FILE, seeds=SEEDS):
    cells = {}
    for n in N_GRID:
        for s in S_GRID:
            run = sim.Run(n, s, file_bytes=file_bytes)
            rs = [sim.simulate(st, run, seed) for seed in seeds]
            ok = [r for r in rs if not r["aborted"]]
            comps = sorted(r["completion"] for r in ok)
            cells["%d/%d" % (n, s)] = {
                "N": n, "S": s,
                "success_fraction": len(ok) / len(rs),
                "median_completion": statistics.median(comps) if comps else None,
                "min_completion": min(comps) if comps else None,
                "max_completion": max(comps) if comps else None,
                "median_retries": statistics.median(r["retries"] for r in rs),
                "median_failures": statistics.median(r["failures"] for r in rs),
                "median_timeouts": statistics.median(r["timeouts"] for r in rs),
            }
    return cells


def optima(cells):
    viable = {k: v for k, v in cells.items()
              if v["success_fraction"] >= 0.5 and v["median_completion"] is not None}
    if not viable:
        return None
    ordered = sorted(viable.items(), key=lambda kv: kv[1]["median_completion"])
    best = ordered[0]
    second = None
    for k, v in ordered[1:]:
        if (v["N"], v["S"]) != (best[1]["N"], best[1]["S"]):
            second = (k, v)
            break
    N_, S_ = best[1]["N"], best[1]["S"]
    # coordinate-wise margins over viable cells
    other_n = [v["median_completion"] for k, v in viable.items()
               if v["N"] != N_ and v["S"] == S_]
    other_s = [v["median_completion"] for k, v in viable.items()
               if v["S"] != S_ and v["N"] == N_]
    n_margin = ((min(other_n) - best[1]["median_completion"]) / min(other_n)) if other_n else None
    s_margin = ((min(other_s) - best[1]["median_completion"]) / min(other_s)) if other_s else None
    return {
        "key": best[0], "N": N_, "S": S_,
        "completion": best[1]["median_completion"],
        "abort_rate": 1 - best[1]["success_fraction"],
        "second_key": second[0] if second else None,
        "second_completion": second[1]["median_completion"] if second else None,
        "second_N": second[1]["N"] if second else None,
        "second_S": second[1]["S"] if second else None,
        "second_success_fraction": second[1]["success_fraction"] if second else None,
        "gap": ((second[1]["median_completion"] - best[1]["median_completion"])
                / second[1]["median_completion"]) if second else None,
        "N_margin": n_margin, "S_margin": s_margin,
        "viable_cells": len(viable),
    }


def cell_by_ns(cells, n, s):
    return cells["%d/%d" % (n, s)]


def main():
    results = {"family": {"good": GOOD.dict(),
                          "degraded": [c.dict() for c in DEGRADED_CANDIDATES]},
               "grid": {"N": N_GRID, "S": S_GRID, "seeds": len(SEEDS),
                        "file_sizes": FILE_SIZES},
               "file_sizes": {}, "selected": None}

    qualified = []
    for file_bytes in FILE_SIZES:
        print("=== file size %d MiB ===" % (file_bytes // MIB), flush=True)
        good_cells = eval_state(GOOD, file_bytes)
        good_opt = optima(good_cells)
        print("  GOOD optimum: (N=%d,S=%d) %.1fs Nmar %.1f%% Smar %.1f%% aborts %.0f%%" % (
            good_opt["N"], good_opt["S"], good_opt["completion"],
            100 * (good_opt["N_margin"] or 0), 100 * (good_opt["S_margin"] or 0),
            100 * good_opt["abort_rate"]), flush=True)
        fs_entry = {"good": {"cells": good_cells, "optimum": good_opt}, "candidates": {}}
        for cand in DEGRADED_CANDIDATES:
            cells = eval_state(cand, file_bytes)
            opt = optima(cells)
            entry = {"state": cand.dict(), "optimum": opt}
            if opt:
                g_at_d = cell_by_ns(good_cells, opt["N"], opt["S"])
                d_at_g = cell_by_ns(cells, good_opt["N"], good_opt["S"])
                penalty_good = ((g_at_d["median_completion"] or float("inf"))
                                - good_opt["completion"]) / good_opt["completion"]
                penalty_deg = ((d_at_g["median_completion"] or float("inf"))
                               - opt["completion"]) / opt["completion"]
                entry["penalty_good"] = penalty_good
                entry["penalty_degraded"] = penalty_deg
                entry["checks"] = {
                    "N_moves": opt["N"] != good_opt["N"],
                    "S_moves": opt["S"] != good_opt["S"],
                    "material": max(penalty_good, penalty_deg) >= 0.10,
                    "separated": (good_opt["N_margin"] or 0) >= 0.05 and (good_opt["S_margin"] or 0) >= 0.05
                    and (opt["N_margin"] or 0) >= 0.05 and (opt["S_margin"] or 0) >= 0.05,
                    "reachable": opt["abort_rate"] <= 0.20
                    and (opt["second_success_fraction"] or 0) >= 0.5,
                }
                entry["qualifies"] = all(entry["checks"].values())
                if entry["qualifies"]:
                    qualified.append({"file_bytes": file_bytes, **entry})
            fs_entry["candidates"][cand.name] = entry
            print("  %-18s opt=(N=%d,S=%d) %.1fs Nmar=%.0f%% Smar=%.0f%% abort=%.0f%% pen_good=%.1f%% pen_deg=%.1f%% qual=%s" % (
                cand.name, opt["N"] if opt else -1, opt["S"] if opt else -1,
                opt["completion"] if opt else -1,
                100 * (opt["N_margin"] or 0) if opt else 0, 100 * (opt["S_margin"] or 0) if opt else 0,
                100 * opt["abort_rate"] if opt else 0,
                100 * entry.get("penalty_good", 0), 100 * entry.get("penalty_degraded", 0),
                entry.get("qualifies")), flush=True)
        results["file_sizes"][str(file_bytes)] = fs_entry

    if qualified:
        # predeclared tie-break: smallest file size, then max min(penalty), then larger congestion, then larger rate
        qualified.sort(key=lambda e: (e["file_bytes"],
                                      -min(e["penalty_good"], e["penalty_degraded"]),
                                      -e["state"]["congestion"], -e["state"]["rate"]))
        sel = qualified[0]
        results["selected"] = {
            "file_bytes": sel["file_bytes"], "state": sel["state"],
            "optimum": sel["optimum"], "penalty_good": sel["penalty_good"],
            "penalty_degraded": sel["penalty_degraded"], "checks": sel["checks"],
            "rule": "smallest qualifying file size, then max min(penalty_good, penalty_degraded), then larger congestion, then larger rate",
            "n_qualified": len(qualified),
            "qualified": [{"file_bytes": e["file_bytes"], "name": e["state"]["name"]} for e in qualified],
        }
        print("\nSELECTED: F=%d MiB state=%s" % (sel["file_bytes"] // MIB, sel["state"]["name"]))
    else:
        results["selected"] = None
        print("\nNO CANDIDATE QUALIFIES AT ANY PREDECLARED FILE SIZE -> STOP TRACK 2")

    json.dump(results, open(os.path.join(OUT, "prescreen.json"), "w"), indent=2)
    print("prescreen written")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
