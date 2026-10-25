#!/usr/bin/env python3
"""Predeclared analysis of baselines_ext (protocol.md). Standard library only.

Usage: analyze.py   (reads runs_E/, writes analysis/E.json)
"""
import glob, json, math, os, random, re, sys

BASE = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
M = json.load(open(os.path.join(BASE, "manifest.json")))
BOOT, SEED = 10000, 1
RID = re.compile(r"^(.*)_b(\d\d)_(.*)$")
TOOLS = ["axel", "wget2", "lftp"]


def geo(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


def peak(sv):
    ev = sorted([(s["start_ms"], 1) for s in sv] + [(s["end_ms"], -1) for s in sv])
    c = m = 0
    for _, d in ev:
        c += d
        m = max(m, c)
    return m


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
            "verdict": verdict, "wins": sum(r < 1 for r in rs)}


def main():
    tab, invalid, desc = {}, [], {}
    for p in sorted(glob.glob(os.path.join(BASE, "runs_E", "*.json"))):
        r = json.load(open(p))
        sc, b, arm = RID.match(r["run_id"]).groups()
        if r.get("content_verified") is True and not r.get("err"):
            tab.setdefault(sc, {}).setdefault(arm, {})[int(b)] = r["completion_ms"] / 1000
            sv = r.get("server_requests") or []
            desc.setdefault(sc, {}).setdefault(arm, []).append({"peak_concurrency": peak(sv), "requests": len(sv)})
        else:
            invalid.append({"run_id": r["run_id"], "err": (r.get("err") or "content not verified")[:200]})
    res = {"label": "screen; constructed server models", "invalid": invalid, "scenarios": {}}
    for sc in M["stages"]["E"]["scenario_order"]:
        t = tab.get(sc, {})
        ours = M["ours_arm"][sc]
        o = {"geo_mean_s": {a: geo(list(v.values())) for a, v in t.items()},
             "n_valid": {a: len(v) for a, v in t.items()}, "ours_arm": ours,
             "describe": {a: {"peak_concurrency_max": max(x["peak_concurrency"] for x in v),
                              "requests_median": sorted(x["requests"] for x in v)[len(v) // 2]}
                          for a, v in desc.get(sc, {}).items()},
             "ours_vs": {x: ratio(t, ours, x) for x in TOOLS + ["aria2"]},
             "tool_vs_aria2": {x: ratio(t, x, "aria2") for x in TOOLS}}
        res["scenarios"][sc] = o
        print(sc, {a: round(v, 1) for a, v in o["geo_mean_s"].items()})
        for x, r in o["ours_vs"].items():
            if r:
                print(f"  ours/{x}: {r['geo_ratio']:.3f} [{r['ci95'][0]:.3f}, {r['ci95'][1]:.3f}] {r['verdict']}")
        print("  peak concurrency", {a: d["peak_concurrency_max"] for a, d in o["describe"].items()})
    print("invalid:", len(invalid))
    os.makedirs(os.path.join(BASE, "analysis"), exist_ok=True)
    json.dump(res, open(os.path.join(BASE, "analysis", "E.json"), "w"), indent=1)


if __name__ == "__main__":
    main()
