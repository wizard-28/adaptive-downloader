"""Predeclared analysis for the NGINX check (descriptive only)."""
import json, math, os, statistics as st
BASE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
m = json.load(open(os.path.join(BASE, "manifest", "nginx_manifest.json")))
def load(rid):
    p = os.path.join(BASE, "runs", rid + ".json")
    return json.load(open(p)) if os.path.exists(p) else None
valid = lambda d: bool(d) and d.get("content_verified") is True and not d.get("err") and d.get("netem_status") == "applied"
by = {(o["config"], o["pair"], o["system"]): load(o["run_id"]) for o in m["order"]}
out = {"label": "NGINX limit_rate check (screening, n=5 pairs/config; descriptive)", "configs": {}, "invalid_runs": []}
for c in m["configs"]:
    rows = []
    for p in range(m["pairs_per_config"]):
        a, b = by[(c, p, "ours")], by[(c, p, "aria2")]
        for sy, d in (("ours", a), ("aria2", b)):
            if not valid(d):
                out["invalid_runs"].append({"config": c, "pair": p, "system": sy, "reason": "missing" if d is None else (d.get("err") or "unverified")})
        if valid(a) and valid(b):
            rows.append((a["completion_ms"] / 1000, b["completion_ms"] / 1000))
    e = {"n_valid_pairs": len(rows)}
    if rows:
        r = [o / a for o, a in rows]
        e.update(ours_mean_s=st.mean(o for o, _ in rows), aria2_mean_s=st.mean(a for _, a in rows),
                 per_pair_ratio=[round(x, 4) for x in r], geo_mean_ratio=math.exp(st.mean(map(math.log, r))),
                 ratio_min=min(r), ratio_max=max(r), pairs_ours_faster=sum(x < 1 for x in r))
    out["configs"][c] = e
json.dump(out, open(os.path.join(BASE, "analysis", "nginx_results.json"), "w"), indent=1)
L = [out["label"], ""]
for c, e in out["configs"].items():
    L.append(f"{c:22s} n={e['n_valid_pairs']}" + (f" ours={e['ours_mean_s']:.1f}s aria2={e['aria2_mean_s']:.1f}s geo={e['geo_mean_ratio']:.3f} [{e['ratio_min']:.3f},{e['ratio_max']:.3f}] ours<aria2 {e['pairs_ours_faster']}/{e['n_valid_pairs']}" if e['n_valid_pairs'] else ""))
L.append(f"invalid/missing: {len(out['invalid_runs'])}")
for r in out["invalid_runs"]: L.append("  " + json.dumps(r))
open(os.path.join(BASE, "analysis", "nginx_results.txt"), "w").write("\n".join(L) + "\n")
print("\n".join(L))
