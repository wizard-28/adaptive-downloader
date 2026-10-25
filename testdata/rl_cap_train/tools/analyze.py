#!/usr/bin/env python3
"""Predeclared analysis of rl_cap_train (protocol.md). Standard library only.

Usage: analyze.py STAGE        (STAGE = A2, C or D; reads runs_<STAGE>/)
Writes analysis/<STAGE>.json and prints the predeclared comparisons.
"""
import glob, json, math, os, random, re, statistics, sys

BASE = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
BOOT, SEED = 10000, 1
RID = re.compile(r"^(.*)_b(\d\d)_(.*)$")
PRED = {  # protocol Stage A2 predictions: (gm, p5, p95)
    ("CAP8", "fixed_3_1M"): 93.76, ("CAP8", "fixed_8_1M"): 82.01,
    ("CAP0", "fixed_3_1M"): 93.78, ("CAP0", "fixed_16_1M_NT_split"): 114.69,
}
FIXED_1M = ["fixed_16_1M_NT_split", "fixed_4_1M"]
DQN = ["dqnc_start16", "dqnc_start1", "dqn6_start16"]


def geo(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


def load(stage):
    t, invalid, traj = {}, [], {}
    for p in sorted(glob.glob(os.path.join(BASE, "runs_" + stage, "*.json"))):
        r = json.load(open(p))
        sc, b, arm = RID.match(r["run_id"]).groups()
        if r.get("content_verified") is True and not r.get("err"):
            t.setdefault(sc, {}).setdefault(arm, {})[int(b)] = r["completion_ms"] / 1000
            if arm in DQN:
                d = r.get("decisions") or []
                at15 = [e["concurrency"] for e in d if e["time_ms"] <= 15000]
                traj.setdefault((sc, arm), []).append({
                    "block": int(b), "final_n": r.get("final_n"),
                    "moves": sum(e["action_kind"] != "none" for e in d),
                    "n_at_15s": at15[-1] if at15 else None})
        else:
            invalid.append({"run_id": r["run_id"], "err": (r.get("err") or "content not verified")[:160]})
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
            "verdict": verdict, "wins": sum(r < 1 for r in rs)}


def best_of(t, arms):
    present = [a for a in arms if t.get(a)]
    return min(present, key=lambda a: geo(list(t[a].values()))) if present else None


def stage_a2(tab):
    rows, ok = [], True
    for (sc, arm), p in PRED.items():
        v = list(tab.get(sc, {}).get(arm, {}).values())
        g = geo(v) if v else None
        within = g is not None and abs(g / p - 1) <= 0.08
        ok &= within
        rows.append({"scenario": sc, "arm": arm, "predicted_s": p, "live_gm_s": g, "n": len(v),
                     "rel_err": (g / p - 1) if g else None, "within_8pct": within})
    g = lambda sc, a: geo(list(tab[sc][a].values()))
    order = {"CAP0_N3_faster_than_N16": g("CAP0", "fixed_3_1M") < g("CAP0", "fixed_16_1M_NT_split"),
             "CAP8_N8_faster_than_N3": g("CAP8", "fixed_8_1M") < g("CAP8", "fixed_3_1M")}
    return {"rows": rows, "ordering": order, "pass": ok and all(order.values())}


def stage_cd(tab, traj):
    out = {}
    for sc, t in tab.items():
        bf1 = best_of(t, FIXED_1M)
        bfa = best_of(t, FIXED_1M + ["fixed_16_32M_NT_split"])
        o = {"geo_mean_s": {a: geo(list(v.values())) for a, v in t.items()},
             "n_valid": {a: len(v) for a, v in t.items()}, "best_fixed_1M": bf1, "best_fixed_any": bfa}
        for a in DQN:
            if a not in t:
                continue
            o[a] = {"vs_best_fixed_1M": ratio(t, a, bf1),
                    "vs_fixed_16_32M": ratio(t, a, "fixed_16_32M_NT_split"),
                    "vs_best_fixed_any": ratio(t, a, bfa),
                    "vs_aria2": ratio(t, a, "aria2")}
            if a.startswith("dqnc") and "dqn6_start16" in t:
                o[a]["vs_dqn6_start16"] = ratio(t, a, "dqn6_start16")
            tr = traj.get((sc, a), [])
            o[a]["trajectory"] = {"runs": tr,
                                  "median_final_n": statistics.median([x["final_n"] for x in tr]) if tr else None,
                                  "median_moves": statistics.median([x["moves"] for x in tr]) if tr else None,
                                  "median_n_at_15s": statistics.median([x["n_at_15s"] for x in tr if x["n_at_15s"]]) if tr else None}
        out[sc] = o
    return out


def main():
    stage = sys.argv[1]
    tab, invalid, traj = load(stage)
    res = {"stage": stage, "label": "controlled experiment; one constructed server model", "invalid": invalid}
    if stage == "A2":
        res.update(stage_a2(tab))
        for r in res["rows"]:
            print(r)
        print("ordering", res["ordering"], "PASS" if res["pass"] else "FAIL")
    else:
        res["scenarios"] = stage_cd(tab, traj)
        for sc, o in res["scenarios"].items():
            print(sc, {k: round(v, 2) for k, v in o["geo_mean_s"].items()})
            for a in DQN:
                for k, r in (o.get(a) or {}).items():
                    if isinstance(r, dict) and "geo_ratio" in r:
                        print(f"  {a} {k}: {r['geo_ratio']:.3f} [{r['ci95'][0]:.3f}, {r['ci95'][1]:.3f}] {r['verdict']} ({r['den']})")
                if a in o:
                    tj = o[a]["trajectory"]
                    print(f"  {a} final N {tj['median_final_n']} moves {tj['median_moves']} N@15s {tj['median_n_at_15s']}")
    print("invalid:", len(invalid))
    os.makedirs(os.path.join(BASE, "analysis"), exist_ok=True)
    json.dump(res, open(os.path.join(BASE, "analysis", stage + ".json"), "w"), indent=1)


if __name__ == "__main__":
    main()
