"""Predeclared analysis for the D2 pacing-schedule sweep (descriptive only)."""
import glob, json, math, os, statistics as st

BASE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
m = json.load(open(os.path.join(BASE, "manifest", "sweep_manifest.json")))
runs = {}
for o in m["order"]:
    p = os.path.join(BASE, "runs", o["run_id"] + ".json")
    runs[(o["schedule"], o["pair"], o["system"])] = json.load(open(p)) if os.path.exists(p) else None

def valid(d):
    return bool(d) and d.get("content_verified") is True and not d.get("err") \
        and d.get("netem_status") == "applied" and d.get("aria2_exit", 0) == 0

out = {"label": "D2 pacing-schedule sweep results (screening, n=5 pairs/schedule; descriptive)",
       "manifest_sha256": open(os.path.join(BASE, "manifest", "sweep_manifest.sha256")).read().split()[0],
       "schedules": {}, "invalid_runs": []}
for s, spec in m["schedules_kib_per_s"].items():
    rows = []
    for p in range(m["pairs_per_schedule"]):
        a, b = runs[(s, p, "ours")], runs[(s, p, "aria2")]
        for sy, d in (("ours", a), ("aria2", b)):
            if not valid(d):
                out["invalid_runs"].append({"schedule": s, "pair": p, "system": sy,
                                            "reason": "missing" if d is None else (d.get("err") or "unverified/aria2 exit/netem")})
        if valid(a) and valid(b):
            rows.append((p, a["completion_ms"] / 1000, b["completion_ms"] / 1000))
    ratios = [o / r for _, o, r in rows]
    ent = {"phases": spec, "n_valid_pairs": len(rows)}
    if rows:
        ent.update(ours_mean_s=st.mean(o for _, o, _ in rows), aria2_mean_s=st.mean(r for _, _, r in rows),
                   per_pair_ratio=[round(x, 4) for x in ratios],
                   geo_mean_ratio=math.exp(st.mean(math.log(x) for x in ratios)),
                   ratio_min=min(ratios), ratio_max=max(ratios), pairs_ours_faster=sum(x < 1 for x in ratios))
    out["schedules"][s] = ent
json.dump(out, open(os.path.join(BASE, "analysis", "sweep_results.json"), "w"), indent=1)
L = [out["label"], "", f"{'schedule':22s} {'phases':20s} n  ours_s  aria2_s  geo_ratio  [min,max]  ours<aria2"]
for s, e in out["schedules"].items():
    if e["n_valid_pairs"]:
        L.append(f"{s:22s} {e['phases']:20s} {e['n_valid_pairs']}  {e['ours_mean_s']:6.1f}  {e['aria2_mean_s']:6.1f}  {e['geo_mean_ratio']:8.3f}  [{e['ratio_min']:.3f},{e['ratio_max']:.3f}]  {e['pairs_ours_faster']}/{e['n_valid_pairs']}")
    else:
        L.append(f"{s:22s} {e['phases']:20s} 0  (no valid pairs)")
L.append("")
L.append(f"invalid/missing runs: {len(out['invalid_runs'])}")
for r in out["invalid_runs"]:
    L.append("  " + json.dumps(r))
open(os.path.join(BASE, "analysis", "sweep_results.txt"), "w").write("\n".join(L) + "\n")
print("\n".join(L))
