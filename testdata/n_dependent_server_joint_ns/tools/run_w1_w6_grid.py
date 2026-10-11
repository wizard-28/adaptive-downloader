#!/usr/bin/env python3
"""Phase 4a: live fixed-grid validation across the frozen W1-W6 family.

PRECONDITION: manifest/w1_w6_manifest.json must exist (frozen by
tools/design_w1_w6.py) and the Phase-1 chain must have finished (no
concurrent netem).  This driver follows the frozen manifest exactly:
per-workload cell set, 2 reps, 512 MiB, timeout 30 s, retries 3,
netem 40 ms + 0.4 % on lo.  Invalid runs are preserved and reported.
"""
import json
import os
import statistics
import subprocess
import time

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
TOOL = os.path.join(BASE, "tools", "ns_bench")
MAN = os.path.join(BASE, "manifest", "w1_w6_manifest.json")
RUNS = os.path.join(BASE, "w1_w6_grid")
LOGS = os.path.join(BASE, "logs")
ANALYSIS = os.path.join(BASE, "analysis")
for d in (RUNS, LOGS, ANALYSIS):
    os.makedirs(d, exist_ok=True)

NETEM = ["-netem-latency-ms", "40", "-netem-loss", "0.4"]


def run(run_id, args):
    out = os.path.join(RUNS, run_id + ".json")
    if os.path.exists(out):
        try:
            j = json.load(open(out))
            if j.get("content_verified") and not j.get("err"):
                return {"run_id": run_id, "completion_ms": j.get("completion_ms"),
                        "valid": True, "invalid_reasons": [], "skipped": True}
        except Exception:
            pass
    cmd = [TOOL, "-run-id", run_id, "-out", out] + args + NETEM
    t0 = time.time()
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=900)
    meta = {"run_id": run_id, "cmd": cmd, "exit": p.returncode,
            "wall_s": time.time() - t0}
    result = None
    if os.path.exists(out):
        try:
            result = json.load(open(out))
            meta["completion_ms"] = result.get("completion_ms")
            meta["content_verified"] = result.get("content_verified")
            meta["final_n"] = result.get("final_n")
            meta["final_s"] = result.get("final_s")
            meta["err"] = result.get("err")
        except Exception as e:
            meta["parse_error"] = str(e)
    reasons = []
    if p.returncode != 0:
        reasons.append("exit %d" % p.returncode)
    if result is None:
        reasons.append("missing/unparsable result")
    else:
        if result.get("err"):
            reasons.append(result["err"])
        if not result.get("content_verified"):
            reasons.append("content verification failed")
    meta["valid"] = not reasons
    meta["invalid_reasons"] = reasons
    json.dump(meta, open(os.path.join(LOGS, run_id + ".meta.json"), "w"), indent=2)
    with open(os.path.join(BASE, "index_w1_w6_grid.jsonl"), "a") as f:
        f.write(json.dumps({k: meta.get(k) for k in
                            ("run_id", "completion_ms", "valid", "invalid_reasons")}) + "\n")
    return meta


def workload_args(w):
    args = ["-system", "ours", "-policy", "none", "-schedule", w["schedule"],
            "-n", "4", "-chunk", "4194304"]
    if w["cap_c"]:
        args += ["-cap-c", str(w["cap_c"]), "-cap-gamma", str(w["cap_gamma"]),
                 "-cap-at", str(w["cap_at_s"])]
    return args


def main():
    man = json.load(open(MAN))
    workloads = man["workloads"]
    reps = 2
    for wid in sorted(workloads):
        w = workloads[wid]
        for (n, s) in w["live_cells"]:
            for rep in range(w.get("live_reps", reps)):
                rid = "%s_n%d_s%d_r%d" % (wid, n, s, rep)
                m = run(rid, workload_args(w) + ["-n", str(n), "-chunk", str(s)])
                print("%s n=%-2d s=%-9d r%d %8s valid=%s" % (
                    wid, n, s, rep,
                    "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
                    m["valid"]), flush=True)
    grid = {}
    for wid in sorted(workloads):
        w = workloads[wid]
        cells = {}
        for (n, s) in w["live_cells"]:
            key = "%d/%d" % (n, s)
            vals, inval = [], []
            for rep in range(w.get("live_reps", reps)):
                p = os.path.join(RUNS, "%s_n%d_s%d_r%d.json" % (wid, n, s, rep))
                if os.path.exists(p):
                    j = json.load(open(p))
                    if j.get("content_verified") and not j.get("err") and j.get("completion_ms"):
                        vals.append(j["completion_ms"] / 1000.0)
                    else:
                        inval.append(j.get("err") or "invalid")
            cells[key] = {"valid": len(vals), "invalid": len(inval),
                          "median_s": statistics.median(vals) if vals else None,
                          "runs_s": vals, "invalid_reasons": inval}
        ranked = sorted((c["median_s"], k) for k, c in cells.items() if c["median_s"])
        pred = "%d/%d" % (w["optimum_B"]["N"], w["optimum_B"]["S"])
        grid[wid] = {"label": w["label"], "cells": cells,
                     "live_argmin": ranked[0][1] if ranked else None,
                     "live_argmin_s": ranked[0][0] if ranked else None,
                     "predicted_optimum": pred,
                     "prediction_match": bool(ranked and ranked[0][1] == pred)}
    common = None
    for wid, g in grid.items():
        ks = {k for k, c in g["cells"].items() if c["median_s"]}
        common = ks if common is None else (common & ks)
    family = {}
    for k in sorted(common or []):
        vals = [grid[wid]["cells"][k]["median_s"] for wid in sorted(workloads)
                if grid[wid]["cells"][k]["median_s"]]
        family[k] = {"mean_s": statistics.mean(vals), "n_workloads": len(vals)}
    fam_ranked = sorted((v["mean_s"], k) for k, v in family.items())
    out = {"workloads": grid, "common_cells": sorted(common or []),
           "family_mean_by_cell": family,
           "family_best_single_fixed": fam_ranked[0][1] if fam_ranked else None,
           "family_best_single_fixed_mean_s": fam_ranked[0][0] if fam_ranked else None}
    json.dump(out, open(os.path.join(ANALYSIS, "w1_w6_grid.json"), "w"), indent=2)
    for wid in sorted(grid):
        g = grid[wid]
        print("%s (%s): live argmin %s %.1fs | predicted %s match=%s" % (
            wid, g["label"], g["live_argmin"], g["live_argmin_s"] or -1,
            g["predicted_optimum"], g["prediction_match"]))
    print("family best single fixed:", out["family_best_single_fixed"],
          out["family_best_single_fixed_mean_s"])
    print("w1_w6 grid done", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
