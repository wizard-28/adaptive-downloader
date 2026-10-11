#!/usr/bin/env python3
"""Track 2 analysis: extract the pre-screen evidence and the structural reasons
for the frozen STOP decision."""
import json
import math
import os
import statistics
import sys

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
sys.path.insert(0, os.path.join(BASE, "tools"))
import ns_sim as sim  # noqa: E402

J = json.load(open(os.path.join(BASE, "analysis", "prescreen.json")))
MIB = sim.MIB
N_GRID = [1, 2, 4, 8, 16]
S_GRID = [int(k) for k in J["grid"]["S"]]
FILE_SIZES = J["grid"]["file_sizes"]


def cell(fs_entry, key):
    return fs_entry["good"]["cells"][key]


DEG_CELLS = {}


def cand_cell(fs_entry, name, key):
    return DEG_CELLS[key]


def main():
    out = {"file_sizes": {}, "structural": {}}
    for fb in FILE_SIZES:
        e = J["file_sizes"][str(fb)]
        go = e["good"]["optimum"]
        out["file_sizes"][str(fb)] = {
            "good_optimum": go,
            "candidates": {name: {"optimum": c["optimum"],
                                  "penalty_good": c.get("penalty_good"),
                                  "penalty_degraded": c.get("penalty_degraded"),
                                  "checks": c.get("checks")}
                           for name, c in e["candidates"].items()},
        }

    # structural analysis on the strongest candidate (F=64 MiB, c=0.04)
    fs = 64 * MIB
    e = J["file_sizes"][str(fs)]
    name = "D_r0.4_l0.02_c0.04"
    st = e["candidates"][name]["state"]
    good = e["good"]["cells"]
    # recompute the degraded cells directly (the prescreen stores only summaries)
    global DEG_CELLS
    deg_cells = DEG_CELLS
    for n in N_GRID:
        for s in S_GRID:
            run = sim.Run(n, s, file_bytes=fs)
            rs = [sim.simulate(sim.State(**st), run, seed) for seed in range(1, 21)]
            ok = [r["completion"] for r in rs if not r["aborted"]]
            deg_cells["%d/%d" % (n, s)] = {
                "N": n, "S": s, "success_fraction": len(ok) / len(rs),
                "median_completion": statistics.median(ok) if ok else None}

    # timeout cliff: for the best N per state, the largest S that does not time out
    def cliff(cells, n):
        rows = []
        for s in S_GRID:
            c = cells["%d/%d" % (n, s)]
            rows.append((s, c["median_completion"], 1 - c["success_fraction"]))
        return rows

    # per-request service time at the optimum (analytical)
    per = st["rate"]
    out["structural"] = {
        "strongest_candidate": {"file_bytes": fs, "state": st},
        "degraded_timeout_cliff": {
            "per_response_rate_MiBps": per / MIB,
            "max_S_before_timeout_MiB": (per * 30) / MIB,
            "note": "svc(S) = latency + overhead*(k-1) + S/per; the 30 s client timeout aborts larger S",
        },
        "degraded_optimum_N_plateau": {
            "optimum_key": e["candidates"][name]["optimum"]["key"],
            "completions_at_optimum_S": {
                str(n): cand_cell(e, name, "%d/%d" % (n, e["candidates"][name]["optimum"]["S"]))["median_completion"]
                for n in N_GRID},
            "note": "N beyond the request count ties; the N* movement is degenerate",
        },
        "abort_cliff": {
            "p_eff_at_k16": min(0.9, st["loss"] + st["congestion"] * 15),
            "p_3_failures_at_k16": min(0.9, st["loss"] + st["congestion"] * 15) ** 3,
            "note": "P(range aborts) = p_eff^3; with many ranges the high-k cells abort",
        },
        "single_best_fixed_across_states": {},
    }
    # minimax: the fixed config minimizing the worst-case penalty across (GOOD, DEGRADED)
    best = None
    for n in N_GRID:
        for s in S_GRID:
            gc = good["%d/%d" % (n, s)]
            dc = cand_cell(e, name, "%d/%d" % (n, s))
            g = gc["median_completion"]
            d = dc["median_completion"]
            worst = max((g or math.inf) / e["good"]["optimum"]["completion"],
                        (d or math.inf) / e["candidates"][name]["optimum"]["completion"])
            if best is None or worst < best[0]:
                best = (worst, n, s, g, d)
    out["structural"]["single_best_fixed_across_states"] = {
        "N": best[1], "S": best[2], "good_completion": best[3],
        "degraded_completion": best[4], "worst_relative_penalty": best[0],
        "note": "even the best single fixed config pays a large worst-case penalty",
    }
    json.dump(out, open(os.path.join(BASE, "analysis", "track2_analysis.json"), "w"), indent=2)

    print("=== structural summary (strongest candidate, F=64 MiB) ===")
    o = e["candidates"][name]["optimum"]
    print("GOOD optimum: (N=%d,S=%.1fM) %.2fs | DEGRADED optimum: (N=%d,S=%.1fM) %.2fs" % (
        e["good"]["optimum"]["N"], e["good"]["optimum"]["S"] / MIB, e["good"]["optimum"]["completion"],
        o["N"], o["S"] / MIB, o["completion"]))
    print("penalties: GOOD using D's optimum %+.1f%%; D using GOOD's optimum %+.1f%%" % (
        100 * e["candidates"][name]["penalty_good"], 100 * e["candidates"][name]["penalty_degraded"]))
    print("N margins: GOOD %.1f%% DEGRADED %.1f%%; S margins: GOOD %.1f%% DEGRADED %.1f%%" % (
        100 * (e["good"]["optimum"]["N_margin"] or 0), 100 * (o["N_margin"] or 0),
        100 * (e["good"]["optimum"]["S_margin"] or 0), 100 * (o["S_margin"] or 0)))
    print("degraded completions at the optimum S (N=1,2,4,8,16):",
          {k: (None if v is None else round(v, 2)) for k, v in
           out["structural"]["degraded_optimum_N_plateau"]["completions_at_optimum_S"].items()})
    print("timeout cliff: S > %.1f MiB times out at per=%.2f MiB/s" % (
        out["structural"]["degraded_timeout_cliff"]["max_S_before_timeout_MiB"],
        out["structural"]["degraded_timeout_cliff"]["per_response_rate_MiBps"]))
    print("abort cliff: p_eff(k=16)=%.2f -> P(3 failures)=%.3f" % (
        out["structural"]["abort_cliff"]["p_eff_at_k16"],
        out["structural"]["abort_cliff"]["p_3_failures_at_k16"]))
    print("best single fixed across states: (N=%d,S=%.1fM) worst penalty %.1fx" % (
        best[1], best[2] / MIB, best[0]))
    print("selected scenario:", J["selected"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
