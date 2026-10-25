#!/usr/bin/env python3
"""Extract simulator calibration from the rl_capped request logs.

Usage: python3 testdata/rl_cap_train/tools/calibrate.py   (from the repository root)

Uses only per-request timing components, never completion times:
  excess       = server (end - start) - bytes / rate        (server log)
  client_extra = client attempt duration - server duration  (attempt log)
split by rate class (>= 2 MiB/s vs below), over every valid 1 MiB-range run of
our downloader in testdata/rl_capped/runs. Writes calibration.json with the
0..100 percentiles of each, which sim_env_cap samples by inverse CDF.
"""
import glob, json, os
import numpy as np

OUT = "testdata/rl_cap_train/calibration.json"
MIB = 1 << 20
comp = {"hi": {"excess": [], "client_extra": []}, "lo": {"excess": [], "client_extra": []}}
used = []
for f in sorted(glob.glob("testdata/rl_capped/runs/*.json")):
    r = json.load(open(f))
    if r["system"] != "ours" or r["config"].get("initial_chunk_bytes") != MIB:
        continue
    if r.get("content_verified") is not True or r.get("err"):
        continue
    used.append(os.path.basename(f))
    srv = {s["range_start"]: s for s in r["server_requests"]}
    for a in r["attempts"]:
        s = srv[a["range_start"]]
        n = s["range_end"] - s["range_start"] + 1
        sd = (s["end_ms"] - s["start_ms"]) / 1000
        cls = "hi" if s["rate_bytes_per_second"] >= 2 * MIB else "lo"
        comp[cls]["excess"].append(sd - n / s["rate_bytes_per_second"])
        comp[cls]["client_extra"].append(a["duration_ms"] / 1000 - sd)
q = list(range(101))
out = {"source_runs": used, "rate_class_threshold_bytes_per_s": 2 * MIB, "measured_owd_s": 0.040,
       "percentiles": q}
for cls, d in comp.items():
    out[cls] = {k: {"n": len(v), "mean": float(np.mean(v)), "q": [float(x) for x in np.percentile(v, q)]}
                for k, v in d.items()}
json.dump(out, open(OUT, "w"), indent=1)
for cls in ("hi", "lo"):
    print(cls, {k: (v["n"], round(v["mean"], 4)) for k, v in out[cls].items()})
