#!/usr/bin/env python3
"""Analyze the live mechanism gate: fixed-N sweep under predeclared contention states.

Gate criteria (frozen in manifest/gate_design.json):
  G-N1 interior optimum: in >=1 state, a measurable contention cost grows with N
        AND the winning N is not the largest N, with a material margin
        (winner >= 5% faster than the second best) and a consistent winner
        (best in >= 2 of 3 reps).
  G-N2 state dependence: N*(a) != N*(b) for >=2 states with the same
        materiality and consistency requirements.
  overall PASS iff G-N1 and G-N2; otherwise STOP (no S sweep, no adaptation).
"""
import glob
import json
import os
import statistics
from collections import defaultdict

BASE = "/workspaces/mvp/testdata/live_joint_ns_queueing"
C_BPS = 5_000_000
STATES = ["S0_no_background", "S1_moderate", "S2_heavy"]
N_GRID = [1, 2, 4, 8, 16]


def load():
    rows = defaultdict(list)
    for p in sorted(glob.glob(os.path.join(BASE, "mechanism_gate", "runs", "gate_*.json"))):
        rid = os.path.basename(p)[:-5]
        j = json.load(open(p))
        mp = os.path.join(BASE, "mechanism_gate", "meta", rid + ".json")
        if not os.path.exists(mp):
            continue
        m = json.load(open(mp))
        ok = (j["err"] == "" and j["content_verified"] and m.get("background_ok"))
        rows[(m["state"], m["n"])].append({
            "run_id": rid, "valid": ok, "rep": m["rep"],
            "completion_s": j["completion_ms"] / 1000.0,
            "ping_median_ms": m["ping_median_ms"], "ping_p95_ms": m["ping_p95_ms"],
            "drops": m["qdisc_drops"], "retrans": m["nstat_delta"].get("TcpRetransSegs"),
            "achieved_background_bps": m["iperf_mean_bps"],
            "client_retries": j["client_retries"],
        })
    return rows


def cell_stats(rows, state, n):
    rs = [r for r in rows[(state, n)] if r["valid"]]
    comps = sorted(r["completion_s"] for r in rs)
    return {
        "n_valid": len(rs), "completions_s": comps,
        "median_s": statistics.median(comps), "min_s": min(comps), "max_s": max(comps),
        "ping_median_ms": statistics.median(r["ping_median_ms"] for r in rs),
        "ping_p95_ms": statistics.median(r["ping_p95_ms"] for r in rs),
        "drops": statistics.median(r["drops"] for r in rs),
        "retrans": statistics.median(r["retrans"] for r in rs),
        "achieved_background_bps": statistics.median(r["achieved_background_bps"] for r in rs
                                                     if r["achieved_background_bps"] is not None)
        if any(r["achieved_background_bps"] is not None for r in rs) else None,
        "reps": rs,
    }


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
    rows = load()
    out = {"params": {"C_bps": C_BPS, "file_bytes": 32 << 20, "chunk_bytes": 1 << 20},
           "states": {}, "gate": {}}
    for st in STATES:
        st_out = {"cells": {}}
        for n in N_GRID:
            st_out["cells"][str(n)] = cell_stats(rows, st, n)
        meds = {n: st_out["cells"][str(n)]["median_s"] for n in N_GRID}
        n_star = min(meds, key=lambda k: meds[k])
        second = sorted(meds.items(), key=lambda kv: kv[1])[1]
        margin = (second[1] - meds[n_star]) / second[1]
        # consistent winner: n_star is the fastest in >=2 of 3 reps
        wins = 0
        for rep in range(1, 4):
            rs = [r for r in rows[(st, n_star)] if r["valid"] and r["rep"] == rep]
            if not rs:
                continue
            v = rs[0]["completion_s"]
            others = [r["completion_s"] for m in N_GRID if m != n_star
                      for r in rows[(st, m)] if r["valid"] and r["rep"] == rep]
            if others and v <= min(others):
                wins += 1
        p95s = [st_out["cells"][str(n)]["ping_p95_ms"] for n in N_GRID]
        drops = [st_out["cells"][str(n)]["drops"] for n in N_GRID]
        retr = [st_out["cells"][str(n)]["retrans"] for n in N_GRID]
        st_out["summary"] = {
            "median_s": {str(n): meds[n] for n in N_GRID},
            "N_star": n_star, "second_best": second[0], "second_s": second[1],
            "margin": margin, "winner_rep_wins": wins,
            "ping_p95_spearman_vs_N": spearman(N_GRID, p95s),
            "drops_spearman_vs_N": spearman(N_GRID, drops),
            "retrans_spearman_vs_N": spearman(N_GRID, retr),
            "ping_p95_ratio_16_over_1": p95s[-1] / p95s[0],
            "achieved_background_bps": st_out["cells"]["16"]["achieved_background_bps"],
            "interior_optimum": 1 < n_star < 16,
            "material_margin": margin >= 0.05,
            "consistent_winner": wins >= 2,
        }
        out["states"][st] = st_out

    # predeclared gate
    interior = {st: out["states"][st]["summary"] for st in STATES}
    g1_states = [st for st in STATES if interior[st]["interior_optimum"]
                 and interior[st]["material_margin"] and interior[st]["consistent_winner"]]
    stars = {st: interior[st]["N_star"] for st in STATES}
    material_state_diff = []
    for i, a in enumerate(STATES):
        for b in STATES[i + 1:]:
            if stars[a] != stars[b] and interior[a]["material_margin"] and interior[b]["material_margin"] \
               and interior[a]["consistent_winner"] and interior[b]["consistent_winner"]:
                material_state_diff.append((a, b))
    contention_cost = {st: (interior[st]["ping_p95_spearman_vs_N"] > 0.5
                            and interior[st]["ping_p95_ratio_16_over_1"] > 1.1)
                       for st in STATES}
    out["gate"] = {
        "G-N1_interior_optimum": {
            "states_with_interior_material_consistent_optimum": g1_states,
            "contention_cost_measurable_per_state": contention_cost,
            "N_star_per_state": stars,
            "pass": len(g1_states) >= 1,
        },
        "G-N2_state_dependence": {
            "material_state_pairs": [list(p) for p in material_state_diff],
            "pass": len(material_state_diff) >= 1,
        },
    }
    out["gate"]["overall"] = "PASS" if (out["gate"]["G-N1_interior_optimum"]["pass"]
                                        and out["gate"]["G-N2_state_dependence"]["pass"]) else "FAIL"
    out["verdict"] = ("JOINT_OPPORTUNITY_CANDIDATE" if out["gate"]["overall"] == "PASS"
                      else "NO_LIVE-GROUNDED_JOINT_N+S_OPPORTUNITY")
    json.dump(out, open(os.path.join(BASE, "analysis", "gate_analysis.json"), "w"), indent=2)

    print("=== live mechanism gate: completion (median s) by state x N ===")
    print("state                 " + "".join("%9d" % n for n in N_GRID) + "   N*  margin")
    for st in STATES:
        s = out["states"][st]["summary"]
        print("%-20s " % st + "".join("%9.2f" % s["median_s"][str(n)] for n in N_GRID)
              + "   %2d  %+.1f%%" % (s["N_star"], 100 * s["margin"]))
    print("=== contention cost (median ping p95 ms) ===")
    for st in STATES:
        c = out["states"][st]["cells"]
        print("%-20s " % st + "".join("%9.1f" % c[str(n)]["ping_p95_ms"] for n in N_GRID)
              + "  (N=16/N=1: %.2fx)" % out["states"][st]["summary"]["ping_p95_ratio_16_over_1"])
    print("=== drops / retrans (median) ===")
    for st in STATES:
        c = out["states"][st]["cells"]
        print("%-20s drops " % st + "".join("%9.0f" % c[str(n)]["drops"] for n in N_GRID))
        print("%-20s retr  " % "" + "".join("%9.0f" % c[str(n)]["retrans"] for n in N_GRID))
    print("=== gate ===")
    print(json.dumps(out["gate"], indent=2))
    print("VERDICT:", out["verdict"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
