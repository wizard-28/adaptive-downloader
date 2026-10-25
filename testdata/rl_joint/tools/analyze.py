#!/usr/bin/env python3
"""Predeclared analysis of rl_joint (protocol.md). Standard library only.

Usage: analyze.py STAGE     (STAGE = A2, C or L; reads runs_<STAGE>/)
"""
import glob, json, math, os, random, re, statistics, sys

BASE = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
BOOT, SEED = 10000, 1
RID = re.compile(r"^(.*)_b(\d\d)_(.*)$")
PRED = {("CAP8", "fixed_16_9M"): 81.69, ("CAP0", "fixed_8_17M"): 80.04,
        ("D2", "fixed_16_5M"): 20.30, ("HEALTHY", "fixed_16_17M"): 34.13}
FIXED = ["fixed_16_1M", "fixed_16_32M", "fixed_16_5M", "fixed_4_4M"]
CAP_AT_MS = 8000


def geo(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


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


def precap_share(r):
    """Share of bytes in requests that arrived before the cap switched on."""
    sv = r.get("server_requests") or []
    tot = sum(s["range_end"] - s["range_start"] + 1 for s in sv)
    pre = sum(s["range_end"] - s["range_start"] + 1 for s in sv if s["start_ms"] < CAP_AT_MS)
    return pre / tot if tot else None


def load(stage):
    t, invalid, desc = {}, [], {}
    for p in sorted(glob.glob(os.path.join(BASE, "runs_" + stage, "*.json"))):
        r = json.load(open(p))
        sc, b, arm = RID.match(r["run_id"]).groups()
        if r.get("content_verified") is True and not r.get("err"):
            t.setdefault(sc, {}).setdefault(arm, {})[int(b)] = r["completion_ms"] / 1000
            d = r.get("decisions") or []
            at8 = [e["chunk_size"] for e in d if e["time_ms"] <= CAP_AT_MS]
            desc.setdefault(sc, {}).setdefault(arm, []).append({
                "block": int(b), "final_n": r.get("final_n"), "final_s": r.get("final_s"),
                "moves": sum(e["action_kind"] != "none" for e in d),
                "chunk_at_8s": at8[-1] if at8 else None, "precap_byte_share": precap_share(r)})
        else:
            invalid.append({"run_id": r["run_id"], "err": (r.get("err") or "content not verified")[:160]})
    return t, invalid, desc


def main():
    stage = sys.argv[1]
    tab, invalid, desc = load(stage)
    res = {"stage": stage, "label": "controlled experiment; constructed server models", "invalid": invalid}
    if stage == "A2":
        rows, ok = [], True
        for (sc, arm), p in PRED.items():
            v = list(tab.get(sc, {}).get(arm, {}).values())
            g = geo(v) if v else None
            within = g is not None and abs(g / p - 1) <= 0.08
            ok &= within
            rows.append({"scenario": sc, "arm": arm, "predicted_s": p, "live_gm_s": g, "n": len(v),
                         "rel_err": (g / p - 1) if g else None, "within_8pct": within})
            print(rows[-1])
        res.update(rows=rows, **{"pass": ok})
        print("PASS" if ok else "FAIL")
    else:
        res["scenarios"] = {}
        for sc, t in tab.items():
            fixed = [a for a in FIXED if t.get(a)]
            best = min(fixed, key=lambda a: geo(list(t[a].values()))) if fixed else None
            o = {"geo_mean_s": {a: geo(list(v.values())) for a, v in t.items()},
                 "n_valid": {a: len(v) for a, v in t.items()}, "best_fixed": best,
                 "dqnj_vs_best_fixed": ratio(t, "dqnj", best),
                 "dqnj_vs_each": {a: ratio(t, "dqnj", a) for a in fixed},
                 "dqnj_vs_aria2": ratio(t, "dqnj", "aria2"),
                 "describe": desc.get(sc, {})}
            res["scenarios"][sc] = o
            print(sc, {a: round(v, 1) for a, v in o["geo_mean_s"].items()})
            r = o["dqnj_vs_best_fixed"]
            if r:
                print(f"  dqnj vs best fixed ({best}): {r['geo_ratio']:.3f} [{r['ci95'][0]:.3f}, {r['ci95'][1]:.3f}] {r['verdict']}")
            r = o["dqnj_vs_aria2"]
            if r:
                print(f"  dqnj vs aria2: {r['geo_ratio']:.3f} [{r['ci95'][0]:.3f}, {r['ci95'][1]:.3f}] {r['verdict']}")
            dj = desc.get(sc, {}).get("dqnj", [])
            if dj:
                med = lambda k: statistics.median([x[k] for x in dj if x[k] is not None]) if any(x[k] is not None for x in dj) else None
                print(f"  dqnj final N {med('final_n')} final S {med('final_s')} moves {med('moves')} S@8s {med('chunk_at_8s')} precap share {med('precap_byte_share')}")
    print("invalid:", len(invalid))
    os.makedirs(os.path.join(BASE, "analysis"), exist_ok=True)
    json.dump(res, open(os.path.join(BASE, "analysis", stage + ".json"), "w"), indent=1)


if __name__ == "__main__":
    main()
