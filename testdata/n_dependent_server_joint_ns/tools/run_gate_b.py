#!/usr/bin/env python3
"""Track 2-B live fixed-grid gate driver.

Both states (healthy = no cap; capped = C=8 MiB/s, gamma=0.05 from t=0), the
full 5x5 (N,S) grid, one run per cell plus two extra runs on the analytically
predicted top-3 cells per state. Invalid runs are preserved with their reason.
"""
import json
import os
import subprocess
import time

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
TOOL = os.path.join(BASE, "tools", "ns_bench")
RUNS = os.path.join(BASE, "fixed_grid")
LOGS = os.path.join(BASE, "logs")
for d in (RUNS, LOGS):
    os.makedirs(d, exist_ok=True)

N_GRID = [1, 2, 4, 8, 16]
S_GRID = [262144, 1048576, 4194304, 16777216, 33554432]
STATES = {
    "healthy": ["-cap-c", "0"],
    "capped": ["-cap-c", "8388608", "-cap-gamma", "0.05", "-cap-at", "0"],
}
TOP = {"healthy": [(16, 1048576), (16, 4194304), (8, 1048576)],
       "capped": [(4, 4194304), (4, 1048576), (8, 4194304)]}


def run_cell(state, n, s, rep):
    run_id = "gate_%s_n%d_s%d_r%d" % (state, n, s, rep)
    out = os.path.join(RUNS, run_id + ".json")
    cmd = [TOOL, "-system", "ours", "-policy", "none", "-n", str(n), "-chunk", str(s),
           "-min-chunk", "262144", "-max-chunk", "33554432", "-schedule", "t1",
           "-netem-latency-ms", "40", "-netem-loss", "0.4",
           "-run-id", run_id, "-out", out] + STATES[state]
    t0 = time.time()
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=900)
    wall = time.time() - t0
    meta = {"run_id": run_id, "state": state, "N": n, "S": s, "rep": rep,
            "cmd": cmd, "exit": p.returncode, "wall_s": wall,
            "stdout": p.stdout.strip(), "stderr": p.stderr.strip()}
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
    with open(os.path.join(BASE, "index_gate.jsonl"), "a") as f:
        f.write(json.dumps({k: meta.get(k) for k in
                            ("run_id", "state", "N", "S", "rep", "completion_ms",
                             "valid", "invalid_reasons")}) + "\n")
    return meta


def main():
    for state in STATES:
        for n in N_GRID:
            for s in S_GRID:
                m = run_cell(state, n, s, 0)
                print("%-8s N=%-2d S=%-8d %8s valid=%s %s" % (
                    state, n, s,
                    "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
                    m["valid"], ",".join(m["invalid_reasons"])), flush=True)
        for (n, s) in TOP[state]:
            for rep in (1, 2):
                m = run_cell(state, n, s, rep)
                print("%-8s N=%-2d S=%-8d r%d %8s valid=%s" % (
                    state, n, s, rep,
                    "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
                    m["valid"]), flush=True)
    print("gate driver done", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
