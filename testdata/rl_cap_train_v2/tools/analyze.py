#!/usr/bin/env python3
"""Predeclared analysis of rl_cap_train_v2 (protocol.md). Standard library only.

Usage: analyze.py STAGE        (STAGE = C or D; reads runs_<STAGE>/)
       analyze.py verdict      (reads analysis/C.json and analysis/D.json)
Writes analysis/<STAGE>.json and prints the predeclared comparisons; "verdict"
applies the frozen wording rules to both stages.
"""
import glob, json, math, os, random, re, statistics, sys

BASE = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
BOOT, SEED = 10000, 1
RID = re.compile(r"^(.*)_b(\d\d)_(.*)$")
AGENTS = ["v2", "v1", "drop"]
CAP_AT = {"CAP8": 8.0, "HEALTHY": None, "H_C12": 6.0, "H_RTT110": 12.0, "H_DIP": 6.0, "H_C20": 4.0}
# two-sided 97.5 % t quantiles by degrees of freedom (paired t-interval sensitivity)
T975 = {1: 12.706, 2: 4.303, 3: 3.182, 4: 2.776, 5: 2.571, 6: 2.447, 7: 2.365, 8: 2.306, 9: 2.262, 10: 2.228}


def geo(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


def load(stage):
    t, invalid, traj = {}, [], {}
    for p in sorted(glob.glob(os.path.join(BASE, "runs_" + stage, "*.json"))):
        r = json.load(open(p))
        sc, b, arm = RID.match(r["run_id"]).groups()
        if r.get("content_verified") is True and not r.get("err"):
            t.setdefault(sc, {}).setdefault(arm, {})[int(b)] = r["completion_ms"] / 1000
            if arm in AGENTS:
                d = r.get("decisions") or []
                cap = CAP_AT.get(sc)
                after = [e["concurrency"] for e in d if cap is not None and e["time_ms"] <= (cap + 10) * 1000]
                traj.setdefault((sc, arm), []).append({
                    "block": int(b), "final_n": r.get("final_n"),
                    "moves": sum(e.get("action_kind", "none") != "none" for e in d),
                    "n_10s_after_cap": after[-1] if after else None})
        else:
            invalid.append({"run_id": r["run_id"], "err": (r.get("err") or "content not verified")[:160]})
    return t, invalid, traj


def verdict_of(lo, hi):
    return "faster" if hi < 1 else "slower" if lo > 1 else "not distinguishable"


def ratio(tab, num, den):
    a, b = tab.get(num, {}), tab.get(den, {})
    blocks = sorted(set(a) & set(b))
    if not blocks or num == den:
        return None
    rs = [a[k] / b[k] for k in blocks]
    rng = random.Random(SEED)
    boots = sorted(geo([rs[rng.randrange(len(rs))] for _ in rs]) for _ in range(BOOT))
    lo, hi = boots[int(0.025 * BOOT)], boots[int(0.975 * BOOT) - 1]
    out = {"num": num, "den": den, "n_blocks": len(blocks), "geo_ratio": geo(rs), "ci95": [lo, hi],
           "verdict": verdict_of(lo, hi), "wins": sum(r < 1 for r in rs)}
    if len(rs) >= 2:
        logs = [math.log(r) for r in rs]
        m, sd = statistics.mean(logs), statistics.stdev(logs)
        h = T975[min(len(rs) - 1, 10)] * sd / math.sqrt(len(rs))
        tlo, thi = math.exp(m - h), math.exp(m + h)
        out["t_ci95"] = [tlo, thi]
        out["t_verdict"] = verdict_of(tlo, thi)
        # protocol: where the two disagree, the less favourable verdict is used
        order = {"slower": 0, "not distinguishable": 1, "faster": 2}
        out["verdict_used"] = min(out["verdict"], out["t_verdict"], key=order.get)
    else:
        out["verdict_used"] = out["verdict"]
    return out


def stage_cd(tab, traj):
    out = {}
    for sc, t in sorted(tab.items()):
        fixed = sorted(a for a in t if a.startswith("fixed_"))
        best = min(fixed, key=lambda a: geo(list(t[a].values())))
        o = {"geo_mean_s": {a: geo(list(v.values())) for a, v in t.items()},
             "n_valid": {a: len(v) for a, v in t.items()}, "fixed_arms": fixed, "best_fixed": best}
        for a in AGENTS:
            if a not in t:
                continue
            o[a] = {"vs_best_fixed": ratio(t, a, best),
                    **{f"vs_{f}": ratio(t, a, f) for f in fixed if f != best}}
            if a == "v2":
                o[a]["vs_v1"] = ratio(t, "v2", "v1")
                o[a]["vs_drop"] = ratio(t, "v2", "drop")
            tr = traj.get((sc, a), [])
            med = lambda k: statistics.median([x[k] for x in tr if x[k] is not None]) if any(
                x[k] is not None for x in tr) else None
            o[a]["trajectory"] = {"runs": tr, "median_final_n": med("final_n"), "median_moves": med("moves"),
                                  "median_n_10s_after_cap": med("n_10s_after_cap")}
        out[sc] = o
    return out


def verdict():
    c = json.load(open(os.path.join(BASE, "analysis", "C.json")))["scenarios"]
    d = json.load(open(os.path.join(BASE, "analysis", "D.json")))["scenarios"]
    v = lambda o, a, k: o[a][k]["verdict_used"]
    H = sorted(d)
    M = len(H)
    k = sum(v(d[s], "v2", "vs_best_fixed") == "faster" for s in H)
    m = sum(v(d[s], "v2", "vs_best_fixed") == "slower" for s in H)
    k1 = sum(v(d[s], "v1", "vs_best_fixed") == "faster" for s in H)
    m1 = sum(v(d[s], "v1", "vs_best_fixed") == "slower" for s in H)
    if k >= M - 1 and k >= 3 and m == 0:
        gen = "v2 beat the best fixed 1 MiB setting on held-out scenarios inside its training envelope"
    elif k >= 1:
        gen = "v2 generalized partially"
    else:
        gen = "v2 did not generalize to held-out scenarios"
    gen += (f": faster in {k}, slower in {m} of {M} held-out scenarios, all inside the v2 training ranges"
            f" (v1 on the same scenarios: faster in {k1}, slower in {m1})")
    cap8 = v(c["CAP8"], "v2", "vs_best_fixed")
    ind = ("v2 beat the best fixed 1 MiB setting on CAP8" if cap8 == "faster"
           else "v2 did not beat the best fixed 1 MiB setting on CAP8")
    ind += f" (HEALTHY: {v(c['HEALTHY'], 'v2', 'vs_best_fixed')})"
    allsc = {**c, **d}
    a1 = sum(v(o, "v2", "vs_v1") == "faster" for o in allsc.values())
    b1 = sum(v(o, "v2", "vs_v1") == "slower" for o in allsc.values())
    a = sum(v(o, "v2", "vs_drop") == "faster" for o in allsc.values())
    b = sum(v(o, "v2", "vs_drop") == "slower" for o in allsc.values())
    rule = ("learned control adds something over a C=8-tuned drop rule" if a > b
            else "learning reached at most what a C=8-tuned drop rule does" if a == 0
            else "mixed against a C=8-tuned drop rule")
    rule += (f": v2 faster in {a}, slower in {b} of {len(allsc)} scenarios; the rule's predicted time was"
             " worse than fixed 16 x 1 MiB in H_RTT110 and H_C20 (amendment A2)")
    out = {"generalization": gen, "in_distribution": ind,
           "v2_vs_v1": f"v2 faster than v1 in {a1}, slower in {b1} of {len(allsc)} scenarios",
           "against_rule": rule, "counts": {"k": k, "m": m, "M": M, "k_v1": k1, "m_v1": m1,
                                            "rule_faster": a, "rule_slower": b, "v1_faster": a1, "v1_slower": b1}}
    json.dump(out, open(os.path.join(BASE, "analysis", "verdict.json"), "w"), indent=1)
    for key, val in out.items():
        print(key, ":", val)


def main():
    stage = sys.argv[1]
    if stage == "verdict":
        return verdict()
    tab, invalid, traj = load(stage)
    res = {"stage": stage, "label": "controlled experiment; one constructed server model", "invalid": invalid,
           "scenarios": stage_cd(tab, traj)}
    for sc, o in res["scenarios"].items():
        print(sc, {k: round(v, 2) for k, v in o["geo_mean_s"].items()}, "best fixed", o["best_fixed"])
        for a in AGENTS:
            for key, r in (o.get(a) or {}).items():
                if isinstance(r, dict) and "geo_ratio" in r:
                    print(f"  {a} {key}: {r['geo_ratio']:.3f} [{r['ci95'][0]:.3f}, {r['ci95'][1]:.3f}]"
                          f" t[{r.get('t_ci95', [0, 0])[0]:.3f}, {r.get('t_ci95', [0, 0])[1]:.3f}] {r['verdict_used']}")
            if a in o:
                tj = o[a]["trajectory"]
                print(f"  {a} final N {tj['median_final_n']} moves {tj['median_moves']}"
                      f" N 10 s after cap {tj['median_n_10s_after_cap']}")
    print("invalid:", len(invalid))
    os.makedirs(os.path.join(BASE, "analysis"), exist_ok=True)
    json.dump(res, open(os.path.join(BASE, "analysis", stage + ".json"), "w"), indent=1)


if __name__ == "__main__":
    main()
