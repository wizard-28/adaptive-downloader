#!/usr/bin/env python3
"""Phase 1 timeout sensitivity (Track 2-B follow-up).

Question: does the selected capped optimum (N=4, S=4 MiB) change with the
per-request timeout?  Predeclared grid: capped state from start (C=8 MiB/s,
gamma=0.05), N in {4,8,16} x S in {1,4,16} MiB, timeout in {20,30,60} s,
2 reps per cell.  Criteria unchanged; invalid runs preserved.
"""
import json
import os
import statistics
import subprocess
import time

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
TOOL = os.path.join(BASE, "tools", "ns_bench")
RUNS = os.path.join(BASE, "timeout_sensitivity")
LOGS = os.path.join(BASE, "logs")
ANALYSIS = os.path.join(BASE, "analysis")
for d in (RUNS, LOGS, ANALYSIS):
    os.makedirs(d, exist_ok=True)

NETEM = ["-netem-latency-ms", "40", "-netem-loss", "0.4"]
CAP = ["-cap-c", "8388608", "-cap-gamma", "0.05", "-cap-at", "0"]
REPS = 2
TIMEOUTS = (20, 30, 60)
NS = (4, 8, 16)
SS = (1 << 20, 4 << 20, 16 << 20)


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
    wall = time.time() - t0
    meta = {"run_id": run_id, "cmd": cmd, "exit": p.returncode, "wall_s": wall}
    result = None
    if os.path.exists(out):
        try:
            result = json.load(open(out))
            meta["completion_ms"] = result.get("completion_ms")
            meta["content_verified"] = result.get("content_verified")
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
    with open(os.path.join(BASE, "index_timeout_sensitivity.jsonl"), "a") as f:
        f.write(json.dumps({k: meta.get(k) for k in
                            ("run_id", "completion_ms", "valid", "invalid_reasons")}) + "\n")
    return meta


def main():
    runs = {}
    for t in TIMEOUTS:
        base = ["-system", "ours", "-policy", "none", "-schedule", "t1",
                "-timeout", "%ds" % t] + CAP
        for n in NS:
            for s in SS:
                key = "%d/%d" % (n, s)
                runs[(t, key)] = []
                for rep in range(REPS):
                    rid = "to%d_capped_n%d_s%d_r%d" % (t, n, s, rep)
                    m = run(rid, base + ["-n", str(n), "-chunk", str(s)])
                    runs[(t, key)].append(m)
                    print("timeout %2ds n=%-2d s=%-2dMiB r%d %8s valid=%s" % (
                        t, n, s >> 20, rep,
                        "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
                        m["valid"]), flush=True)
    by_timeout = {}
    for t in TIMEOUTS:
        cells = {}
        for key in sorted({k[1] for k in runs if k[0] == t}):
            ms = [m for m in runs[(t, key)]]
            ok = [m["completion_ms"] / 1000 for m in ms if m["valid"] and m.get("completion_ms")]
            cells[key] = {
                "valid_reps": len(ok),
                "invalid_reps": len(ms) - len(ok),
                "median_s": statistics.median(ok) if ok else None,
                "runs_ms": [m.get("completion_ms") for m in ms],
                "invalid_reasons": [r for m in ms if not m["valid"] for r in m["invalid_reasons"]],
            }
        ranked = sorted((c["median_s"], k) for k, c in cells.items() if c["median_s"])
        by_timeout["%ds" % t] = {
            "cells": cells,
            "argmin": ranked[0][1] if ranked else None,
            "argmin_completion_s": ranked[0][0] if ranked else None,
            "selected_4_4M_is_argmin": bool(ranked and ranked[0][1] == "4/4194304"),
        }
    out = {"grid": {"N": list(NS), "S": list(SS), "timeouts": list(TIMEOUTS),
                    "reps": REPS, "state": "capped from start C=8MiB/s gamma=0.05",
                    "netem": "40ms + 0.4%"},
           "by_timeout": by_timeout}
    json.dump(out, open(os.path.join(ANALYSIS, "timeout_sensitivity.json"), "w"), indent=2)
    for t, v in by_timeout.items():
        print("timeout %s argmin=%s (%.1fs) 4/4M-optimum=%s" % (
            t, v["argmin"], v["argmin_completion_s"] or -1, v["selected_4_4M_is_argmin"]))
    print("timeout sensitivity done", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
