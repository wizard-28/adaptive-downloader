#!/usr/bin/env python3
"""Track 1 driver: fresh D2 joint N+S benchmark.

Frozen design (manifest/track1_manifest.json):
  20 blocks x {fixed, n-only, s-only, joint}, randomized mode order per block,
  fresh server/output per run, no early stopping, no tuning.
  Invalid runs are preserved with their exact reason and never replaced.
  After the matrix: 3 aria2 consistency runs (frozen flags).
"""
import json
import os
import random
import subprocess
import sys
import time

BASE = "/workspaces/mvp/testdata/joint_ns_d2"
TOOL = os.path.join(BASE, "tools", "bench_ns")
PER_RUN = os.path.join(BASE, "per_run")
RAW = os.path.join(BASE, "raw")
LOGS = os.path.join(BASE, "logs")
for d in (PER_RUN, RAW, LOGS):
    os.makedirs(d, exist_ok=True)

BLOCKS = 20
MODES = ["fixed", "n-only", "s-only", "joint"]
ORDER_SEED = 20260929

FROZEN = {
    "file-size": str(512 << 20),
    "initial-n": "4", "min-n": "1", "max-n": "16",
    "initial-chunk": str(4 << 20), "min-chunk": str(256 << 10),
    "max-chunk": str(16 << 20), "chunk-step": str(1 << 20),
    "improve": "0.10", "degrade": "0.10", "window": "4",
    "timeout": "30s", "retries": "3",
}


def run_one(run_id, mode, extra=None):
    out_path = os.path.join(PER_RUN, run_id + ".json")
    cmd = [TOOL, "-mode", mode, "-run-id", run_id, "-out", out_path]
    for k, v in FROZEN.items():
        cmd += ["-" + k, v]
    if extra:
        cmd += extra
    log_base = os.path.join(RAW, run_id)
    t0 = time.time()
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=900)
    wall = time.time() - t0
    open(log_base + ".stdout.txt", "w").write(p.stdout)
    open(log_base + ".stderr.txt", "w").write(p.stderr)
    meta = {"run_id": run_id, "mode": mode, "cmd": cmd, "exit": p.returncode,
            "wall_s": wall, "stdout": p.stdout.strip(), "stderr": p.stderr.strip()}
    result = None
    if os.path.exists(out_path):
        try:
            result = json.load(open(out_path))
            meta["completion_ms"] = result.get("completion_ms")
            meta["content_verified"] = result.get("content_verified")
            meta["netem_status"] = result.get("netem_status")
            meta["output_size_bytes"] = result.get("output_size_bytes")
            meta["client_retries"] = result.get("client_retries")
            meta["decisions"] = len(result.get("decisions") or [])
            meta["err"] = result.get("err")
        except Exception as e:
            meta["parse_error"] = str(e)
    # validity (frozen invalid-run policy)
    reasons = []
    if p.returncode != 0:
        reasons.append("runner exit code %d" % p.returncode)
    if result is None:
        reasons.append("missing or unparsable result JSON")
    else:
        if result.get("netem_status") != "applied":
            reasons.append("netem not applied")
        if result.get("err"):
            reasons.append("runner error: %s" % result["err"])
        if not result.get("content_verified"):
            reasons.append("content verification failed")
        if result.get("output_size_bytes") != 512 << 20:
            reasons.append("output size mismatch")
    meta["valid"] = not reasons
    meta["invalid_reasons"] = reasons
    json.dump(meta, open(os.path.join(RAW, run_id + ".meta.json"), "w"), indent=2)
    with open(os.path.join(BASE, "index.jsonl"), "a") as f:
        f.write(json.dumps({k: meta.get(k) for k in
                            ("run_id", "mode", "exit", "wall_s", "completion_ms",
                             "valid", "invalid_reasons", "decisions", "content_verified")}) + "\n")
    return meta


def main():
    rng = random.Random(ORDER_SEED)
    schedule = []
    for b in range(1, BLOCKS + 1):
        order = MODES[:]
        rng.shuffle(order)
        schedule.append({"block": b, "order": order})
    json.dump({"order_seed": ORDER_SEED, "blocks": BLOCKS, "schedule": schedule},
              open(os.path.join(BASE, "manifest", "run_schedule.json"), "w"), indent=2)

    for entry in schedule:
        b = entry["block"]
        for mode in entry["order"]:
            run_id = "b%02d_%s" % (b, mode)
            m = run_one(run_id, mode)
            print("block %2d %-7s comp=%-8s valid=%s decisions=%-3s wall=%.1fs %s" % (
                b, mode,
                "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
                m["valid"], m.get("decisions"), m["wall_s"],
                ",".join(m["invalid_reasons"]) if m["invalid_reasons"] else ""), flush=True)

    # aria2 environment consistency checks (frozen flags; outside the blocked matrix)
    for i in range(1, 4):
        run_id = "aria2_consistency_%d" % i
        m = run_one(run_id, "aria2")
        print("aria2 consistency %d comp=%-8s valid=%s" % (
            i, "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
            m["valid"]), flush=True)
    print("driver done", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
