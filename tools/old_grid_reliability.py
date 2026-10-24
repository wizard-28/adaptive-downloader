#!/usr/bin/env python3
"""Wasted bytes and failed attempts in the published fixed (N,S) grid, which ran
with a 30 s whole-request timeout (testdata/n_dependent_server_joint_ns/fixed_grid).
Read-only over the frozen directory. Usage: old_grid_reliability.py OUT_JSON"""
import collections, glob, json, re, sys

GRID = "testdata/n_dependent_server_joint_ns/fixed_grid"
cells = collections.defaultdict(list)
for f in glob.glob(f"{GRID}/gate_*_r*.json"):
    if f.endswith(".meta.json"):
        continue
    m = re.search(r"gate_(\w+?)_n(\d+)_s(\d+)_r", f)
    cells[f"{m[1]}/N{m[2]}/S{int(m[3]) >> 10}KiB"].append(json.load(open(f)))
out = {}
for k, rs in sorted(cells.items()):
    size = rs[0]["output_size_bytes"] or 536870912
    out[k] = {"runs": len(rs),
              "valid": sum(1 for r in rs if r["content_verified"] and not r["err"]),
              "failed_attempts_mean": sum(r["client_failed"] for r in rs) / len(rs),
              "wasted_frac_mean": sum(max(0, r["client_bytes"] - 536870912) for r in rs) / len(rs) / 536870912}
json.dump(out, open(sys.argv[1], "w"), indent=1)
bad = {k: v for k, v in out.items() if v["failed_attempts_mean"] > 0}
for k, v in bad.items():
    print(f"{k:22s} valid {v['valid']}/{v['runs']}  failed {v['failed_attempts_mean']:.0f}  wasted {100 * v['wasted_frac_mean']:.1f}%")
print(f"cells with failures: {len(bad)} of {len(out)}")
