#!/usr/bin/env python3
"""Stage K: per-delay simulator calibration from the v2 calibration runs.

Usage: python3 testdata/rl_cap_train_v2/tools/calibrate.py   (from the repository root)

The same per-request components as testdata/rl_cap_train/tools/calibrate.py,
never completion times:
  excess       = server (end - start) - bytes / rate        (server log)
  client_extra = client attempt duration - server duration  (attempt log)
split by rate class (>= 2 MiB/s vs below). One file per one-way delay,
calibration_<ms>.json, from the valid runs_K runs at that delay. The 40 ms
table is the v1 file (testdata/rl_cap_train/calibration.json), unchanged.
"""
import glob, json, os
import numpy as np

BASE = "testdata/rl_cap_train_v2"
MIB = 1 << 20
by_delay = {}
for f in sorted(glob.glob(f"{BASE}/runs_K/*.json")):
    r = json.load(open(f))
    if r.get("content_verified") is not True or r.get("err"):
        continue
    d = int(r["run_id"].split("_")[1])
    g = by_delay.setdefault(d, {"runs": [], "hi": {"excess": [], "client_extra": []},
                                "lo": {"excess": [], "client_extra": []}})
    g["runs"].append(os.path.basename(f))
    srv = {s["range_start"]: s for s in r["server_requests"]}
    for a in r["attempts"]:
        s = srv.get(a["range_start"])
        if s is None:
            continue
        n = s["range_end"] - s["range_start"] + 1
        sd = (s["end_ms"] - s["start_ms"]) / 1000
        cls = "hi" if s["rate_bytes_per_second"] >= 2 * MIB else "lo"
        g[cls]["excess"].append(sd - n / s["rate_bytes_per_second"])
        g[cls]["client_extra"].append(a["duration_ms"] / 1000 - sd)
q = list(range(101))
for d, g in sorted(by_delay.items()):
    out = {"source_runs": g["runs"], "rate_class_threshold_bytes_per_s": 2 * MIB,
           "measured_owd_s": d / 1000, "percentiles": q}
    for cls in ("hi", "lo"):
        out[cls] = {k: {"n": len(v), "mean": float(np.mean(v)), "q": [float(x) for x in np.percentile(v, q)]}
                    for k, v in g[cls].items()}
    json.dump(out, open(f"{BASE}/calibration_{d}.json", "w"), indent=1)
    print(d, "ms", len(g["runs"]), "runs", {c: {k: (v["n"], round(v["mean"], 4)) for k, v in out[c].items()}
                                          for c in ("hi", "lo")})
