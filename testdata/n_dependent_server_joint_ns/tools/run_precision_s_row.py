#!/usr/bin/env python3
"""Phase 1 precision replication of the selected capped S row.

Selected capped optimum: N=4.  Predeclared S row S in {1,4,16} MiB under the
capped state from start (C=8 MiB/s, gamma=0.05), 4 extra reps per S, pooled
with the frozen gate runs for the same cells.  Criterion unchanged (5% local
S margin); only the precision of the estimate changes.  Invalid runs preserved.
"""
import glob
import json
import os
import random
import statistics
import subprocess
import time

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
TOOL = os.path.join(BASE, "tools", "ns_bench")
RUNS = os.path.join(BASE, "precision")
LOGS = os.path.join(BASE, "logs")
ANALYSIS = os.path.join(BASE, "analysis")
GRID = os.path.join(BASE, "fixed_grid")
for d in (RUNS, LOGS, ANALYSIS):
    os.makedirs(d, exist_ok=True)

NETEM = ["-netem-latency-ms", "40", "-netem-loss", "0.4"]
CAP = ["-cap-c", "8388608", "-cap-gamma", "0.05", "-cap-at", "0"]
REPS = 4
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
    with open(os.path.join(BASE, "index_precision.jsonl"), "a") as f:
        f.write(json.dumps({k: meta.get(k) for k in
                            ("run_id", "completion_ms", "valid", "invalid_reasons")}) + "\n")
    return meta


def load_gate_cell(s):
    vals = []
    for f in sorted(glob.glob(os.path.join(GRID, "gate_capped_n4_s%d_r*.json" % s))):
        j = json.load(open(f))
        if j.get("content_verified") and not j.get("err") and j.get("completion_ms"):
            vals.append(j["completion_ms"] / 1000.0)
    return vals


def main():
    base = ["-system", "ours", "-policy", "none", "-schedule", "t1",
            "-n", "4", "-timeout", "30s"] + CAP
    for s in SS:
        for rep in range(REPS):
            rid = "prec_capped_n4_s%d_r%d" % (s, rep)
            m = run(rid, base + ["-chunk", str(s)])
            print("precision n=4 s=%-2dMiB r%d %8s valid=%s" % (
                s >> 20, rep,
                "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
                m["valid"]), flush=True)
    pooled = {}
    for s in SS:
        gate_vals = load_gate_cell(s)
        prec_vals = []
        for rep in range(REPS):
            p = os.path.join(RUNS, "prec_capped_n4_s%d_r%d.json" % (s, rep))
            if os.path.exists(p):
                j = json.load(open(p))
                if j.get("content_verified") and not j.get("err") and j.get("completion_ms"):
                    prec_vals.append(j["completion_ms"] / 1000.0)
        pooled["%d" % s] = {"gate_s": gate_vals, "precision_s": prec_vals,
                            "all_s": gate_vals + prec_vals}
    med = {k: statistics.median(v["all_s"]) for k, v in pooled.items()}
    keys = sorted(pooled, key=lambda k: med[k])
    best, second = keys[0], keys[1]
    margin = (med[second] - med[best]) / med[second]
    random.seed(20261008)
    boot = []
    for _ in range(20000):
        b = statistics.median(random.choices(pooled[best]["all_s"], k=len(pooled[best]["all_s"])))
        s2 = statistics.median(random.choices(pooled[second]["all_s"], k=len(pooled[second]["all_s"])))
        boot.append((s2 - b) / s2)
    boot.sort()
    out = {
        "design": {"N": 4, "S": list(SS), "state": "capped from start C=8MiB/s gamma=0.05",
                   "timeout_s": 30, "extra_reps": REPS, "netem": "40ms + 0.4%"},
        "pooled": {k: {"n": len(v["all_s"]), "gate_n": len(v["gate_s"]),
                       "precision_n": len(v["precision_s"]),
                       "median_s": med[k], "min_s": min(v["all_s"]), "max_s": max(v["all_s"])}
                   for k, v in pooled.items()},
        "local_S_margin": {
            "best": "%s" % best, "second": "%s" % second,
            "margin": margin, "ci95_low": boot[int(0.025 * len(boot))],
            "ci95_high": boot[int(0.975 * len(boot))],
            "criterion": 0.05,
            "criterion_state": "reported, unchanged; not redefined after the fact",
        },
    }
    json.dump(out, open(os.path.join(ANALYSIS, "precision_s_row.json"), "w"), indent=2)
    print(json.dumps(out["local_S_margin"], indent=1))
    print("precision replication done", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
