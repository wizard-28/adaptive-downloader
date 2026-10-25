#!/usr/bin/env python3
"""Predeclared analysis of live_mirror_check (protocol.md). Standard library only.

Usage: python3 testdata/live_mirror_check/tools/analyze.py [SLOT]   (from the repository root)
Reads raw/<SLOT>.jsonl (every slot if omitted; amendment A2), writes analysis/<SLOT or M>.json and prints the predeclared comparisons
and the frozen no-harm sentences.
"""
import glob, json, math, os, random, statistics, sys

BASE = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
BOOT, SEED = 10000, 1
ARMS = {"ours_N16_S1M_dqn6_nrt_split": "v2", "ours_N16_S1M_drop_nrt_split": "drop",
        "ours_N16_S1M_nrt_split": "fixed_16_1M", "ours_N8_S1M_nrt_split": "fixed_8_1M"}
FIXED = ["fixed_16_1M", "fixed_8_1M"]
T975 = {1: 12.706, 2: 4.303, 3: 3.182, 4: 2.776, 5: 2.571, 6: 2.447, 7: 2.365, 8: 2.306, 9: 2.262}
ORDER = {"slower": 0, "not distinguishable": 1, "faster": 2}


def geo(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


def verdict(lo, hi):
    return "faster" if hi < 1 else "slower" if lo > 1 else "not distinguishable"


def ratio(t, num, den):
    a, b = t.get(num, {}), t.get(den, {})
    blocks = sorted(set(a) & set(b))
    if len(blocks) < 2:
        return None
    rs = [a[k] / b[k] for k in blocks]
    rng = random.Random(SEED)
    boots = sorted(geo([rs[rng.randrange(len(rs))] for _ in rs]) for _ in range(BOOT))
    lo, hi = boots[int(0.025 * BOOT)], boots[int(0.975 * BOOT) - 1]
    logs = [math.log(r) for r in rs]
    h = T975[min(len(rs) - 1, 9)] * statistics.stdev(logs) / math.sqrt(len(rs))
    tlo, thi = math.exp(statistics.mean(logs) - h), math.exp(statistics.mean(logs) + h)
    used = min(verdict(lo, hi), verdict(tlo, thi), key=ORDER.get)
    return {"num": num, "den": den, "n_blocks": len(blocks), "geo_ratio": geo(rs), "ci95": [lo, hi],
            "t_ci95": [tlo, thi], "verdict_used": used}


def main():
    tab, invalid, desc = {}, [], {}
    slot = sys.argv[1] if len(sys.argv) > 1 else None
    for p in sorted(glob.glob(os.path.join(BASE, "raw", (slot or "*") + ".jsonl"))):
        for line in open(p):
            r = json.loads(line)
            arm = ARMS.get(r["arm"])
            if arm is None:
                continue
            if r.get("valid") and r.get("checksum_ok"):
                tab.setdefault(r["server"], {}).setdefault(arm, {})[r["rep"]] = r["duration_s"]
                d = desc.setdefault((r["server"], arm), {"final_n": [], "moves": [], "switched": 0})
                dec = r.get("decisions") or []
                d["final_n"].append(r.get("final_n"))
                d["moves"].append(sum(1 for e in dec if e.get("action") not in (None, "none", "no_change")))
                if arm == "drop" and any(str(e.get("action", "")).startswith("dec-n") for e in dec):
                    d["switched"] += 1
            else:
                invalid.append({"server": r["server"], "arm": arm, "rep": r["rep"], "err": (r.get("err") or "")[:160],
                                "rate_limited": r.get("rate_limited", False)})
    out, sentences = {}, {}
    no_harm = {"v2": 0, "drop": 0}
    for srv, t in sorted(tab.items()):
        fixed = [a for a in FIXED if t.get(a)]
        best = min(fixed, key=lambda a: geo(list(t[a].values())))
        o = {"geo_mean_s": {a: geo(list(v.values())) for a, v in t.items()},
             "n_valid": {a: len(v) for a, v in t.items()}, "best_fixed": best}
        for a in ("v2", "drop"):
            r = ratio(t, a, best)
            o[f"{a}_vs_best_fixed"] = r
            o[f"{a}_vs_each"] = {f: ratio(t, a, f) for f in fixed}
            ok = r is not None and r["verdict_used"] != "slower" and r["geo_ratio"] <= 1.05
            no_harm[a] += ok
            sentences[f"{srv}.{a}"] = (f"no harm detected on {srv}" if ok else
                                       f"slower than the best fixed setting on {srv} by "
                                       f"{(r['geo_ratio'] - 1) * 100:.1f} %" if r else f"no comparison on {srv}")
        o["v2_vs_drop"] = ratio(t, "v2", "drop")
        o["descriptive"] = {a: {"median_final_n": statistics.median([x for x in desc[(srv, a)]["final_n"] if x])
                                if any(desc[(srv, a)]["final_n"]) else None,
                                "median_moves": statistics.median(desc[(srv, a)]["moves"]),
                                "drop_switched_runs": desc[(srv, a)]["switched"] if a == "drop" else None}
                            for a in ("v2", "drop") if (srv, a) in desc}
        out[srv] = o
    summary = (f"On {no_harm['v2']} of {len(out)} public mirrors, the v2 agent showed no harm against the best "
               f"fixed 1 MiB setting (and the drop rule on {no_harm['drop']} of {len(out)}).")
    res = {"label": "descriptive; one client, one access link", "invalid": invalid, "servers": out,
           "sentences": sentences, "summary": summary}
    os.makedirs(os.path.join(BASE, "analysis"), exist_ok=True)
    json.dump(res, open(os.path.join(BASE, "analysis", (slot or "M") + ".json"), "w"), indent=1)
    for srv, o in out.items():
        print(srv, {k: round(v, 2) for k, v in o["geo_mean_s"].items()}, "best", o["best_fixed"])
        for k in ("v2_vs_best_fixed", "drop_vs_best_fixed", "v2_vs_drop"):
            r = o[k]
            if r:
                print(f"  {k}: {r['geo_ratio']:.3f} [{r['ci95'][0]:.3f}, {r['ci95'][1]:.3f}]"
                      f" t[{r['t_ci95'][0]:.3f}, {r['t_ci95'][1]:.3f}] {r['verdict_used']}")
        print("  ", o["descriptive"])
    print("invalid:", len(invalid))
    for k, v in sentences.items():
        print(k, ":", v)
    print(summary)


if __name__ == "__main__":
    main()
