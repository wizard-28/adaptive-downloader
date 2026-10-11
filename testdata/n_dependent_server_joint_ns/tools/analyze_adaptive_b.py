#!/usr/bin/env python3
"""Track 2-B adaptive analysis (P1 cross-workload selection, P2 within-download
adaptation) with paired ratios, bootstrap CIs, and win/loss counts."""
import glob
import json
import math
import os
import random
import statistics

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
MIB = 1024 ** 2
REPS = 10
BOOT = 10000
SEED = 20260929


def load(prefix):
    runs = {}
    invalid = []
    for p in sorted(glob.glob(os.path.join(BASE, "adaptive", prefix + "*.json"))):
        rid = os.path.basename(p)[:-5]
        j = json.load(open(p))
        mp = os.path.join(BASE, "logs", rid + ".meta.json")
        m = json.load(open(mp)) if os.path.exists(mp) else {}
        rec = {"run_id": rid, "completion_ms": j.get("completion_ms"),
               "final_n": j.get("final_n"), "final_s": j.get("final_s"),
               "switches": [d for d in (j.get("decisions") or []) if d["action_kind"] != "none"],
               "valid": m.get("valid", False) and not j.get("err") and j.get("content_verified")}
        if rec["valid"]:
            runs[rid] = rec
        else:
            invalid.append(rid)
    return runs, invalid


def geo(v):
    return math.exp(sum(math.log(x) for x in v) / len(v))


def boot_ci(v, n=BOOT, seed=SEED):
    rng = random.Random(seed)
    s = sorted(geo([v[rng.randrange(len(v))] for _ in range(len(v))]) for _ in range(n))
    return s[int(0.025 * n)], s[int(0.975 * n)]


def by_mode(runs, mode):
    return {rid: r for rid, r in runs.items() if rid.startswith(mode)}


def main():
    out = {"p1": {}, "p2": {}, "invalid": []}
    # ---------------- P1 ----------------
    for state in ("healthy", "capped"):
        sels, fixed, aria = {}, {}, {}
        for rid, r in load("p1_%s_" % state)[0].items():
            for mode, dst in (("selector", sels), ("fixed", fixed), ("aria2", aria)):
                if rid.startswith("p1_%s_%s_" % (state, mode)):
                    dst[rid] = r
        pairs = []
        for rid, r in sels.items():
            rep = rid.rsplit("_", 1)[1]
            f = fixed.get("p1_%s_fixed_%s" % (state, rep))
            a = aria.get("p1_%s_aria2_%s" % (state, rep))
            if f and a:
                pairs.append({"rep": rep, "sel": r["completion_ms"],
                              "fixed": f["completion_ms"], "aria": a["completion_ms"]})
        if not pairs:
            continue
        ratios_f = [p["sel"] / p["fixed"] for p in pairs]
        ratios_a = [p["sel"] / p["aria"] for p in pairs]
        out["p1"][state] = {
            "n": len(pairs),
            "sel_mean_s": statistics.mean(p["sel"] for p in pairs) / 1000,
            "fixed_mean_s": statistics.mean(p["fixed"] for p in pairs) / 1000,
            "aria_mean_s": statistics.mean(p["aria"] for p in pairs) / 1000,
            "sel_vs_fixed_geo": geo(ratios_f), "sel_vs_fixed_ci": boot_ci(ratios_f),
            "sel_vs_aria_geo": geo(ratios_a), "sel_vs_aria_ci": boot_ci(ratios_a),
            "wins_vs_fixed": sum(1 for r in ratios_f if r < 1),
            "wins_vs_aria": sum(1 for r in ratios_a if r < 1),
            "final_states": sorted({(r["final_n"], r["final_s"]) for r in sels.values()}),
        }
    # explicit baselines for P1 (computed from the raw runs + live gate)
    try:
        gate = json.load(open(os.path.join(BASE, "analysis", "gate_b.json")))
        common = set(gate["cells"]["healthy"]) & set(gate["cells"]["capped"])
        family_best = min(common, key=lambda k: statistics.mean(
            [gate["cells"]["healthy"][k], gate["cells"]["capped"][k]]))
        fb_gains, pw_gains = [], []
        pw_best = {}
        for state in ("healthy", "capped"):
            sel = out["p1"][state]["sel_mean_s"] * 1000
            fb_fixed = gate["cells"][state][family_best]
            fb_gains.append((fb_fixed - sel) / fb_fixed)
            o = gate["optima"][state]
            pw_key = "%d/%d" % (o["N"], o["S"])
            pw_best[state] = {"config": pw_key, "completion_s": o["completion_s"]}
            pw_gains.append((o["completion_s"] * 1000 - sel) / (o["completion_s"] * 1000))
        out["p1_baselines"] = {
            "family_best_single_fixed": {
                "config": family_best,
                "healthy_s": gate["cells"]["healthy"][family_best] / 1000,
                "capped_s": gate["cells"]["capped"][family_best] / 1000,
                "gains": {"healthy": fb_gains[0], "capped": fb_gains[1],
                          "mean": statistics.mean(fb_gains), "worst": min(fb_gains)},
            },
            "per_workload_best_fixed": {
                "healthy": pw_best["healthy"], "capped": pw_best["capped"],
                "gains": {"healthy": pw_gains[0], "capped": pw_gains[1]},
            },
            "note": ("family_best_single_fixed = the single configuration minimizing the mean over the "
                     "two predeclared controlled workloads; per_workload_best_fixed = the best fixed "
                     "configuration for each workload separately"),
        }
    except Exception as e:
        out["p1_baselines"] = {"error": str(e)}

    # ---------------- P2 ----------------
    runs, invalid = load("p2_")
    out["invalid"] = invalid
    modes = {}
    for mode in ("causal_joint", "causal_n_only", "causal_s_only", "fixed_16_1M",
                 "fixed_4_4M", "fixed_4_1M", "aria2"):
        modes[mode] = {rid: r for rid, r in runs.items() if rid.startswith("p2_%s_" % mode)}
    reps = sorted({rid.rsplit("_", 1)[1] for rid in runs})
    rows = []
    for rep in reps:
        row = {"rep": rep}
        for mode in modes:
            r = modes[mode].get("p2_%s_%s" % (mode, rep))
            if r:
                row[mode] = r["completion_ms"]
        rows.append(row)
    # best fixed per rep = min over the three fixed modes present
    for row in rows:
        f = [row[m] for m in ("fixed_16_1M", "fixed_4_4M", "fixed_4_1M") if m in row]
        if f:
            row["best_fixed"] = min(f)
    out["p2"]["modes"] = {}
    for mode in modes:
        vals = [r["completion_ms"] for r in modes[mode].values()]
        if vals:
            out["p2"]["modes"][mode] = {
                "n": len(vals), "mean_s": statistics.mean(vals) / 1000,
                "median_s": statistics.median(vals) / 1000,
                "final_states": sorted({(r["final_n"], r["final_s"]) for r in modes[mode].values()}),
            }
    for mode in ("causal_joint", "causal_n_only", "causal_s_only"):
        rs, ra = [], []
        for row in rows:
            if mode in row and "best_fixed" in row:
                rs.append(row[mode] / row["best_fixed"])
            if mode in row and "aria2" in row:
                ra.append(row[mode] / row["aria2"])
        if rs:
            out["p2"][mode] = {
                "n": len(rs), "vs_best_fixed_geo": geo(rs), "vs_best_fixed_ci": boot_ci(rs),
                "wins_vs_best_fixed": sum(1 for r in rs if r < 1),
                "vs_aria2_geo": geo(ra) if ra else None,
                "vs_aria2_ci": boot_ci(ra) if ra else None,
                "wins_vs_aria2": sum(1 for r in ra if r < 1),
            }
    for a, b, key in (("causal_joint", "causal_n_only", "joint_vs_n_only"),
                      ("causal_joint", "causal_s_only", "joint_vs_s_only"),
                      ("causal_n_only", "causal_s_only", "n_only_vs_s_only")):
        rs = []
        for row in rows:
            if a in row and b in row:
                rs.append(row[a] / row[b])
        if rs:
            out["p2"][key] = {
                "n": len(rs), "geo": geo(rs), "ci": boot_ci(rs),
                "wins": sum(1 for r in rs if r < 1),
                "mean_a_s": statistics.mean([row[a] for row in rows if a in row]) / 1000,
                "mean_b_s": statistics.mean([row[b] for row in rows if b in row]) / 1000,
            }
    out["p2"]["best_fixed_mean_s"] = statistics.mean(
        row["best_fixed"] for row in rows if "best_fixed" in row) / 1000 if rows else None
    json.dump(out, open(os.path.join(BASE, "analysis", "adaptive_b.json"), "w"), indent=2)

    print("=== P1 ===")
    for state in ("healthy", "capped"):
        if state in out["p1"]:
            s = out["p1"][state]
            print("%-7s selector %.1fs vs fixed %.1fs (geo %.3f, CI %.3f-%.3f, %d/%d wins) vs aria2 %.1fs (geo %.3f, %d/%d)" % (
                state, s["sel_mean_s"], s["fixed_mean_s"], s["sel_vs_fixed_geo"],
                s["sel_vs_fixed_ci"][0], s["sel_vs_fixed_ci"][1], s["wins_vs_fixed"], s["n"],
                s["aria_mean_s"], s["sel_vs_aria_geo"], s["wins_vs_aria"], s["n"]))
    print("P1 baselines:", json.dumps(out.get("p1_baselines", {}), default=str))
    print("=== P2 ===")
    for mode, s in out["p2"]["modes"].items():
        print("  %-14s mean %.1fs median %.1fs final=%s" % (
            mode, s["mean_s"], s["median_s"], s["final_states"]))
    for mode in ("causal_joint", "causal_n_only"):
        if mode in out["p2"]:
            s = out["p2"][mode]
            print("  %-14s vs best fixed geo %.3f CI %.3f-%.3f (%d/%d wins); vs aria2 geo %s (%d/%d)" % (
                mode, s["vs_best_fixed_geo"], s["vs_best_fixed_ci"][0], s["vs_best_fixed_ci"][1],
                s["wins_vs_best_fixed"], s["n"],
                "%.3f" % s["vs_aria2_geo"] if s["vs_aria2_geo"] else "n/a",
                s["wins_vs_aria2"], s["n"]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
