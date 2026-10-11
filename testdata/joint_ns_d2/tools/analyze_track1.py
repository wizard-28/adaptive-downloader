#!/usr/bin/env python3
"""Track 1 analysis: fresh D2 joint N+S benchmark.

Reads per_run/*.json + raw/*.meta.json (validity), computes the frozen
comparisons, adaptation evidence, and the predeclared A-E classification.
"""
import glob
import json
import math
import os
import random
import statistics

BASE = "/workspaces/mvp/testdata/joint_ns_d2"
FROZEN_BENCH = "/workspaces/mvp/testdata/aria2_competitive_benchmark"
MIB = 1024 ** 2
MODES = ["fixed", "n-only", "s-only", "joint"]
MODE_LABEL = {"fixed": "fixed (16,4M)", "n-only": "N-only adaptive",
              "s-only": "S-only adaptive", "joint": "joint adaptive"}
BOOT = 10000
RNG_SEED = 20260929

# frozen transition rules (manifest/track1_manifest.json)
N_MIN, N_MAX, S_MIN, S_MAX, S_STEP = 1, 16, 256 << 10, 16 << 20, 1 << 20


def clamp(v, lo, hi):
    return max(lo, min(hi, v))


def derive_states(decisions, final_from_result=None):
    """Derive the controller state sequence from recorded decisions.

    The decisions record the before-state (ctx.Concurrency/ChunkSize) and the
    action; actions returned by the bounded policies always change state, so
    the next state follows the frozen transition rule."""
    states = []
    n, s = None, None
    n_changes = s_changes = 0
    for d in decisions:
        if n is None:
            n, s = d["concurrency"], d["chunk_size"]
        else:
            # observed before-state is authoritative; count changes
            if d["concurrency"] != n:
                n_changes += 1
                n = d["concurrency"]
            if d["chunk_size"] != s:
                s_changes += 1
                s = d["chunk_size"]
        states.append((d["time_ms"], n, s))
        k = d["action_kind"]
        if k == "increase-concurrency":
            n = clamp(n + 1, N_MIN, N_MAX)
            n_changes += 1
        elif k == "decrease-concurrency":
            n = clamp(n - 1, N_MIN, N_MAX)
            n_changes += 1
        elif k == "increase-chunk-size":
            s = clamp(s + S_STEP, S_MIN, S_MAX)
            s_changes += 1
        elif k == "decrease-chunk-size":
            s = clamp(s - S_STEP, S_MIN, S_MAX)
            s_changes += 1
    return {"states": states, "n_changes": n_changes, "s_changes": s_changes,
            "final": (n, s)}


def geo(vals):
    return math.exp(sum(math.log(v) for v in vals) / len(vals)) if vals else None


def bootstrap_ci_geo(ratios, n=BOOT, seed=RNG_SEED):
    rng = random.Random(seed)
    vals = []
    for _ in range(n):
        sample = [ratios[rng.randrange(len(ratios))] for _ in range(len(ratios))]
        vals.append(geo(sample))
    vals.sort()
    return vals[int(0.025 * n)], vals[int(0.975 * n)]


def bootstrap_ratio_ci(a, b, n=BOOT, seed=RNG_SEED):
    """Unpaired bootstrap CI of geo(a)/geo(b)."""
    rng = random.Random(seed)
    vals = []
    for _ in range(n):
        sa = [a[rng.randrange(len(a))] for _ in range(len(a))]
        sb = [b[rng.randrange(len(b))] for _ in range(len(b))]
        vals.append(geo(sa) / geo(sb))
    vals.sort()
    return vals[int(0.025 * n)], vals[int(0.975 * n)]


def load():
    runs = {}
    invalid = []
    for p in sorted(glob.glob(os.path.join(BASE, "per_run", "b*_*.json"))):
        rid = os.path.basename(p)[:-5]
        j = json.load(open(p))
        mp = os.path.join(BASE, "raw", rid + ".meta.json")
        m = json.load(open(mp)) if os.path.exists(mp) else {}
        mode = j.get("mode")
        rec = {"run_id": rid, "mode": mode, "completion_ms": j.get("completion_ms"),
               "valid": m.get("valid", False), "reasons": m.get("invalid_reasons", []),
               "decisions": j.get("decisions") or [], "result": j}
        if rec["valid"]:
            runs[rid] = rec
        else:
            invalid.append(rec)
    return runs, invalid


def load_frozen_aria2():
    pairs = json.load(open(os.path.join(FROZEN_BENCH, "final_reproduction", "pairs.json")))["pairs"]
    return [p["b"]["completion_ms"] for p in pairs]


def main():
    runs, invalid = load()
    by_mode = {m: [r for r in runs.values() if r["mode"] == m] for m in MODES}
    out = {"n_valid": len(runs), "n_invalid": len(invalid),
           "invalid_runs": [{"run_id": r["run_id"], "mode": r["mode"], "reasons": r["reasons"]}
                            for r in invalid],
           "modes": {}, "comparisons": {}, "adaptation": {}, "classification": {}}

    for m in MODES:
        comps = sorted(r["completion_ms"] for r in by_mode[m])
        out["modes"][m] = {
            "label": MODE_LABEL[m], "n": len(comps),
            "mean_ms": statistics.mean(comps) if comps else None,
            "median_ms": statistics.median(comps) if comps else None,
            "sd_ms": statistics.pstdev(comps) if len(comps) > 1 else 0.0,
            "min_ms": min(comps) if comps else None, "max_ms": max(comps) if comps else None,
            "completions_ms": comps,
        }

    # paired per-block ratios
    blocks = sorted({int(r["run_id"][1:3]) for r in runs.values()})
    for other in ["n-only", "s-only"]:
        pairs = []
        for b in blocks:
            a = next((r for r in by_mode["joint"] if r["run_id"].startswith("b%02d_" % b)), None)
            o = next((r for r in by_mode[other] if r["run_id"].startswith("b%02d_" % b)), None)
            if a and o:
                pairs.append((a["completion_ms"], o["completion_ms"]))
        ratios = [a / o for a, o in pairs]
        if ratios:
            ci = bootstrap_ci_geo(ratios)
            out["comparisons"]["joint_vs_%s" % other] = {
                "n_pairs": len(ratios), "geo_ratio": geo(ratios), "ci95": ci,
                "wins": sum(1 for r in ratios if r < 1), "losses": sum(1 for r in ratios if r > 1),
                "beats": geo(ratios) <= 0.95 and ci[1] < 1.0,
                "mean_ms": {"joint": statistics.mean(a for a, _ in pairs),
                            "other": statistics.mean(o for _, o in pairs)},
            }
    # primary: joint vs fixed
    pairs = []
    for b in blocks:
        a = next((r for r in by_mode["joint"] if r["run_id"].startswith("b%02d_" % b)), None)
        f = next((r for r in by_mode["fixed"] if r["run_id"].startswith("b%02d_" % b)), None)
        if a and f:
            pairs.append((a["completion_ms"], f["completion_ms"]))
    ratios = [a / f for a, f in pairs]
    ci = bootstrap_ci_geo(ratios)
    out["comparisons"]["joint_vs_fixed"] = {
        "n_pairs": len(ratios), "geo_ratio": geo(ratios), "ci95": ci,
        "wins": sum(1 for r in ratios if r < 1), "losses": sum(1 for r in ratios if r > 1),
        "beats": geo(ratios) <= 0.95 and ci[1] < 1.0,
        "mean_ms": {"joint": statistics.mean(a for a, _ in pairs),
                    "fixed": statistics.mean(f for _, f in pairs)},
    }
    # aria2: frozen 30-pair distribution (immutable) + fresh consistency runs
    frozen = load_frozen_aria2()
    joint_vals = [r["completion_ms"] for r in by_mode["joint"]]
    ci2 = bootstrap_ratio_ci(joint_vals, frozen)
    out["comparisons"]["joint_vs_frozen_aria2"] = {
        "n_joint": len(joint_vals), "n_aria2": len(frozen),
        "geo_ratio": geo(joint_vals) / geo(frozen), "ci95": ci2,
        "aria2_geo_ms": geo(frozen), "aria2_mean_ms": statistics.mean(frozen),
        "aria2_median_ms": statistics.median(frozen),
        "beats": geo(joint_vals) / geo(frozen) <= 0.95 and ci2[1] < 1.0,
    }
    cons = []
    for p in sorted(glob.glob(os.path.join(BASE, "per_run", "aria2_consistency_*.json"))):
        j = json.load(open(p))
        if j.get("content_verified") and not j.get("err"):
            cons.append(j["completion_ms"])
    out["comparisons"]["aria2_consistency_fresh"] = {
        "n": len(cons), "completions_ms": cons,
        "mean_ms": statistics.mean(cons) if cons else None,
        "frozen_mean_ms": statistics.mean(frozen),
        "rel_diff": (statistics.mean(cons) - statistics.mean(frozen)) / statistics.mean(frozen) if cons else None,
    }

    # adaptation evidence (joint mode)
    joint_adapt = []
    for r in by_mode["joint"]:
        d = derive_states(r["decisions"])
        total = r["completion_ms"]
        occ = {}
        st = d["states"]
        for i, (t, n, s) in enumerate(st):
            t1 = st[i + 1][0] if i + 1 < len(st) else total
            occ[(n, s)] = occ.get((n, s), 0.0) + (t1 - t)
        joint_adapt.append({
            "run_id": r["run_id"], "n_changes": d["n_changes"], "s_changes": d["s_changes"],
            "unique_states": len(occ), "final": list(d["final"]),
            "occupancy": {"%d/%d" % k: v for k, v in occ.items()},
            "first_action_ms": next((dd["time_ms"] for dd in r["decisions"]
                                     if dd["action_kind"] != "none"), None),
            "actions_after_4s": sum(1 for dd in r["decisions"] if dd["time_ms"] >= 4000 and dd["action_kind"] != "none"),
            "n_decisions": len(r["decisions"]),
        })
    both = sum(1 for a in joint_adapt if a["n_changes"] > 0 and a["s_changes"] > 0)
    out["adaptation"]["joint"] = {
        "runs": joint_adapt,
        "n_runs": len(joint_adapt),
        "n_changed_both": both,
        "fraction_changed_both": both / len(joint_adapt) if joint_adapt else 0.0,
        "n_changed_n": sum(1 for a in joint_adapt if a["n_changes"] > 0),
        "n_changed_s": sum(1 for a in joint_adapt if a["s_changes"] > 0),
        "median_final_N": statistics.median(a["final"][0] for a in joint_adapt) if joint_adapt else None,
        "median_final_S_MiB": statistics.median(a["final"][1] for a in joint_adapt) / MIB if joint_adapt else None,
    }
    for m in ["n-only", "s-only"]:
        ad = []
        for r in by_mode[m]:
            d = derive_states(r["decisions"])
            ad.append({"run_id": r["run_id"], "n_changes": d["n_changes"],
                       "s_changes": d["s_changes"], "final": list(d["final"])})
        out["adaptation"][m] = {"n_runs": len(ad),
                                "median_final": [statistics.median(a["final"][0] for a in ad),
                                                 statistics.median(a["final"][1] for a in ad)] if ad else None}

    # classification (frozen rules)
    cmp_fixed = out["comparisons"]["joint_vs_fixed"]
    cmp_aria2 = out["comparisons"]["joint_vs_frozen_aria2"]
    adapted = out["adaptation"]["joint"]["fraction_changed_both"] >= 0.5
    if not adapted:
        cls = "E"
    elif cmp_fixed["beats"] and cmp_aria2["beats"]:
        cls = "A"
    elif cmp_fixed["beats"]:
        cls = "B"
    elif cmp_aria2["beats"]:
        cls = "C"
    else:
        cls = "D"
    out["classification"] = {
        "code": cls,
        "names": {"A": "BEATS_FIXED_AND_ARIA2", "B": "BEATS_FIXED_ONLY",
                  "C": "BEATS_ARIA2_ONLY", "D": "NO_CLEAR_ADAPTIVE_GAIN",
                  "E": "NOT_DEMONSTRATED"},
        "joint_adaptation_demonstrated": adapted,
        "beats_fixed": cmp_fixed["beats"], "beats_aria2": cmp_aria2["beats"],
        "rules": "E if <50% of valid joint runs changed both N and S; else A/B/C/D by the frozen beats rule (ratio <= 0.95 and CI upper < 1.0)",
    }
    json.dump(out, open(os.path.join(BASE, "analysis", "track1_analysis.json"), "w"), indent=2)

    print("=== Track 1 summary ===")
    print("valid runs: %d, invalid: %d" % (len(runs), len(invalid)))
    for m in MODES:
        s = out["modes"][m]
        print("  %-14s n=%2d mean=%7.2fs median=%7.2fs min=%7.2fs max=%7.2fs" % (
            m, s["n"], s["mean_ms"] / 1000, s["median_ms"] / 1000,
            s["min_ms"] / 1000, s["max_ms"] / 1000))
    for k, v in out["comparisons"].items():
        if "geo_ratio" in v:
            print("  %-26s geo_ratio=%.4f CI=[%.4f,%.4f] wins=%s losses=%s beats=%s" % (
                k, v["geo_ratio"], v["ci95"][0], v["ci95"][1],
                v.get("wins"), v.get("losses"), v.get("beats")))
        else:
            print("  %-26s %s" % (k, {kk: vv for kk, vv in v.items() if kk != "completions_ms"}))
    a = out["adaptation"]["joint"]
    print("  joint adaptation: %d/%d runs changed both N and S; median final (N,S)=(%s,%s)" % (
        a["n_changed_both"], a["n_runs"], a["median_final_N"], a["median_final_S_MiB"]))
    print("CLASSIFICATION: %s = %s" % (cls, out["classification"]["names"][cls]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
