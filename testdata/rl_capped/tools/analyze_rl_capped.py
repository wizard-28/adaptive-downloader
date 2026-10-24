#!/usr/bin/env python3
"""Predeclared analysis of the RL-on-capped-server experiment (protocol.md). Standard library only.

Usage: analyze_online_tuner.py [RUNS_DIR] [OUT_JSON]
"""
import glob, json, math, os, random, statistics, sys

BASE = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
M = json.load(open(os.path.join(BASE, "manifest.json")))
BOOT, SEED = 10000, 20261026
STATIONARY = ["fixed_16_1M_NT_split", "fixed_16_32M_NT_split"]
LEARNED = ["dqn6_start16", "dqn6_start1"]


def geo(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


def load(run_dir):
    t, invalid, traj = {}, [], {}
    for p in sorted(glob.glob(os.path.join(run_dir, "*.json"))):
        r = json.load(open(p))
        rid = r["run_id"]
        sc, b, arm = rid.split("_", 2)
        if r.get("content_verified") is True and not r.get("err"):
            t.setdefault(sc, {}).setdefault(arm, {})[int(b[1:])] = r["completion_ms"] / 1000
            if arm in LEARNED:
                d = r.get("decisions") or []
                moves = [e for e in d if e["action_kind"] != "none"]
                traj.setdefault((sc, arm), []).append({"block": int(b[1:]), "final_n": r.get("final_n"),
                                                "moves": len(moves),
                                                "n_seen": sorted({e["concurrency"] for e in d})})
        else:
            invalid.append({"run_id": rid, "err": (r.get("err") or "content not verified")[:160]})
    return t, invalid, traj


def ratio(tab, num, den):
    a, b = tab.get(num, {}), tab.get(den, {})
    blocks = sorted(set(a) & set(b))
    if not blocks:
        return None
    rs = [a[k] / b[k] for k in blocks]
    rng = random.Random(SEED)
    boots = sorted(geo([rs[rng.randrange(len(rs))] for _ in rs]) for _ in range(BOOT))
    lo, hi = boots[int(0.025 * BOOT)], boots[int(0.975 * BOOT) - 1]
    verdict = "faster" if hi < 1 else "slower" if lo > 1 else "not distinguishable"
    return {"num": num, "den": den, "n_blocks": len(blocks), "geo_ratio": geo(rs), "ci95": [lo, hi],
            "verdict": verdict, "wins": sum(r < 1 for r in rs),
            "dropped_blocks": sorted((set(a) | set(b)) - set(blocks))}


def analyze(run_dir):
    tab, invalid, traj = load(run_dir)
    res = {"label": "controlled experiment; one constructed server model", "invalid": invalid, "scenarios": {}}
    for sc in M["scenario_order"]:
        t = tab.get(sc, {})
        out = {"geo_mean_s": {a: geo(list(v.values())) for a, v in t.items()},
               "n_valid": {a: len(v) for a, v in t.items()}}
        present = [c for c in STATIONARY if t.get(c)]
        best = min(present, key=lambda c: geo(list(t[c].values()))) if present else None
        for a in LEARNED:
            out[a] = {"vs_each_fixed": [ratio(t, a, c) for c in STATIONARY],
                      "vs_best_fixed": dict(ratio(t, a, best) or {}, best_fixed=best),
                      "vs_aria2": ratio(t, a, "aria2")}
            tr = traj.get((sc, a), [])
            out[a]["trajectory"] = {"runs": tr,
                                    "median_final_n": statistics.median([x["final_n"] for x in tr]) if tr else None,
                                    "median_moves": statistics.median([x["moves"] for x in tr]) if tr else None}
        res["scenarios"][sc] = out
    return res


def main():
    run_dir = sys.argv[1] if len(sys.argv) > 1 else os.path.join(BASE, "runs")
    out = sys.argv[2] if len(sys.argv) > 2 else os.path.join(BASE, "analysis", "summary.json")
    res = analyze(run_dir)
    os.makedirs(os.path.dirname(out), exist_ok=True)
    json.dump(res, open(out, "w"), indent=1)
    for sc, o in res["scenarios"].items():
        print(sc, {k: round(v, 2) for k, v in o["geo_mean_s"].items()})
        for a in LEARNED:
            for key in ("vs_best_fixed", "vs_aria2"):
                r = o[a][key]
                if r and "geo_ratio" in r:
                    print(f"  {a} {key}: {r['geo_ratio']:.3f} [{r['ci95'][0]:.3f}, {r['ci95'][1]:.3f}] {r['verdict']} ({r['den']})")
    print("invalid:", len(res["invalid"]))


if __name__ == "__main__":
    main()
