#!/usr/bin/env python3
"""Stage K: per-delay simulator calibration from the v2 calibration runs.

Usage: python3 testdata/rl_cap_train_v2/tools/calibrate.py   (from the repository root)

The same per-request components as testdata/rl_cap_train/tools/calibrate.py,
never completion times:
  excess       = server (end - start) - bytes / rate        (server log)
  client_extra = client attempt duration - server duration  (attempt log)
split into four rate classes (amendment A1): below 0.5 MiB/s, 0.5-1, 1-2
and >= 2 MiB/s. One file per one-way delay, calibration_<ms>.json, from the
valid runs_K runs at that delay; the 40 ms table is rebuilt the same way from
the v1 source runs (testdata/rl_cap_train/calibration.json "source_runs").
"""
import glob, json, os
import numpy as np

BASE = "testdata/rl_cap_train_v2"
MIB = 1 << 20
THRESHOLDS = [MIB // 2, MIB, 2 * MIB]
CLASSES = ["c0", "c1", "c2", "c3"]


def rate_class(rate):
    return CLASSES[sum(rate >= t for t in THRESHOLDS)]


V1 = json.load(open("testdata/rl_cap_train/calibration.json"))["source_runs"]
files = [(int(f.split("/")[-1].split("_")[1]), f) for f in sorted(glob.glob(f"{BASE}/runs_K/*.json"))]
files += [(40, f"testdata/rl_capped/runs/{name}") for name in V1]
by_delay = {}
for d, f in files:
    r = json.load(open(f))
    if r.get("content_verified") is not True or r.get("err"):
        continue
    g = by_delay.setdefault(d, {"runs": [], **{c: {"excess": [], "client_extra": []} for c in CLASSES}})
    g["runs"].append(os.path.basename(f))
    srv = {s["range_start"]: s for s in r["server_requests"]}
    for a in r["attempts"]:
        s = srv.get(a["range_start"])
        if s is None:
            continue
        n = s["range_end"] - s["range_start"] + 1
        sd = (s["end_ms"] - s["start_ms"]) / 1000
        cls = rate_class(s["rate_bytes_per_second"])
        g[cls]["excess"].append(sd - n / s["rate_bytes_per_second"])
        g[cls]["client_extra"].append(a["duration_ms"] / 1000 - sd)
q = list(range(101))
for d, g in sorted(by_delay.items()):
    out = {"source_runs": g["runs"], "rate_class_thresholds_bytes_per_s": THRESHOLDS,
           "classes": CLASSES, "measured_owd_s": d / 1000, "percentiles": q}
    for cls in CLASSES:
        out[cls] = {k: {"n": len(v), "mean": float(np.mean(v)), "q": [float(x) for x in np.percentile(v, q)]}
                    for k, v in g[cls].items()}
    json.dump(out, open(f"{BASE}/calibration_{d}.json", "w"), indent=1)
    print(d, "ms", len(g["runs"]), "runs", {c: {k: (v["n"], round(v["mean"], 4)) for k, v in out[c].items()}
                                          for c in CLASSES})
