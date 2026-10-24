#!/usr/bin/env python3
"""Predeclared analysis of the split ablation (protocol.md). Standard library only.

Usage: analyze_split_ablation.py [RUNS_DIR] [OUT_JSON]
"""
import glob, json, math, os, random, sys

BASE = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
M = json.load(open(os.path.join(BASE, "manifest.json")))
BOOT, SEED = 10000, 20261024


def geo(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


def load(run_dir):
    t, invalid = {}, []
    for p in sorted(glob.glob(os.path.join(run_dir, "*.json"))):
        r = json.load(open(p))
        rid = r["run_id"]
        sc, b, arm = rid.split("_", 2)
        if r.get("content_verified") is True and not r.get("err"):
            t.setdefault(sc, {}).setdefault(arm, {})[int(b[1:])] = r["completion_ms"] / 1000
        else:
            invalid.append({"run_id": rid, "err": (r.get("err") or "content not verified")[:160]})
    return t, invalid


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


def best_vs(tab, num, cands):
    """num / per-block value of the stationary arm with the lowest geometric mean."""
    present = [c for c in cands if tab.get(c)]
    if not present:
        return None
    best = min(present, key=lambda c: geo(list(tab[c].values())))
    r = ratio(tab, num, best)
    if r:
        r["best_stationary"] = best
    return r


def analyze(run_dir):
    tab, invalid = load(run_dir)
    res = {"label": "controlled ablation; one constructed server model", "invalid": invalid, "scenarios": {}}
    for sc, arms in M["scenario_arms"].items():
        t = tab.get(sc, {})
        e = {"geo_mean_s": {a: geo(list(t[a].values())) for a in arms if t.get(a)},
             "valid_runs": {a: len(t.get(a, {})) for a in arms}, "comparisons": {}}
        c = e["comparisons"]
        ours = [a for a in arms if a != "aria2"]
        c["C3"] = {a: ratio(t, a, "aria2") for a in ours}
        if e["geo_mean_s"]:
            o = {a: v for a, v in e["geo_mean_s"].items() if a != "aria2"}
            if o:
                e["best_ours_post_hoc"] = min(o, key=o.get)
        stat = [a for a in arms if a.startswith("fixed_") and a.endswith("_NT_split")]
        if sc in ("CAP0", "CAP8"):
            c["C1"] = ratio(t, "fixed_16_32M_NT", "fixed_16_1M_T30")
            c["C2"] = ratio(t, "fixed_16_32M_NT_split", "fixed_16_32M_NT")
            adapt = "causal_joint_NT_split" if sc == "CAP8" else "selector_NT_split"
            c["C4"] = {"vs_each": {s: ratio(t, adapt, s) for s in stat},
                       "vs_best_stationary": best_vs(t, adapt, stat)}
            if sc == "CAP8":
                c["C4"]["published_replication"] = ratio(t, "causal_joint_T30", "fixed_16_1M_T30")
        if sc == "HEALTHY":
            c["C5"] = ratio(t, "fixed_16_1M_NT_split", "fixed_16_1M_T30")
        if sc == "D2":
            c["C5"] = ratio(t, "fixed_16_4M_NT_split", "fixed_16_4M_T30")
        res["scenarios"][sc] = e
    return res


def main():
    run_dir = sys.argv[1] if len(sys.argv) > 1 else os.path.join(BASE, "runs")
    out = sys.argv[2] if len(sys.argv) > 2 else os.path.join(BASE, "analysis", "summary.json")
    res = analyze(run_dir)
    os.makedirs(os.path.dirname(out), exist_ok=True)
    json.dump(res, open(out, "w"), indent=1, sort_keys=True)
    for sc, e in res["scenarios"].items():
        print(f"== {sc}  best ours (post hoc): {e.get('best_ours_post_hoc')}")
        for a, g in sorted(e["geo_mean_s"].items(), key=lambda x: x[1]):
            print(f"   {a:24s} {g:7.1f} s  (n={e['valid_runs'][a]})")
        for k, v in e["comparisons"].items():
            items = v.items() if isinstance(v, dict) and "geo_ratio" not in v else [(k, v)]
            for name, r in items:
                if isinstance(r, dict) and "vs_each" in str(name):
                    continue
                if isinstance(r, dict) and "geo_ratio" in r:
                    print(f"   {k} {r['num']} / {r['den']}: {r['geo_ratio']:.3f} [{r['ci95'][0]:.3f}, {r['ci95'][1]:.3f}] {r['verdict']} (n={r['n_blocks']})")
    print(f"invalid runs: {len(res['invalid'])}")


if __name__ == "__main__":
    main()
