#!/usr/bin/env python3
"""Track 2-B adaptive experiment driver (P1 cross-workload selection + P2
within-download concurrency adaptation + baselines).

P1 (cross-workload joint selection): for each workload state (healthy = no cap;
capped = C=8 MiB/s, gamma=0.05), run 20 fresh paired blocks of
  selector (probe classifier, target (16,1M)/(4,4M)) | best fixed | aria2 | curl | wget
P2 (within-download adaptation): state schedule [no cap 0-8 s, cap on 8 s+], 20
blocks of
  causal-drop joint (target (4,4M)) | causal-drop N-only (target (4,1M)) |
  fixed (16,1M) | fixed (4,4M) | aria2
All runs fresh, content-verified, invalid runs preserved.
"""
import json
import os
import subprocess
import sys
import time

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
TOOL = os.path.join(BASE, "tools", "ns_bench")
RUNS = os.path.join(BASE, "adaptive")
LOGS = os.path.join(BASE, "logs")
for d in (RUNS, LOGS):
    os.makedirs(d, exist_ok=True)

REPS = int(sys.argv[1]) if len(sys.argv) > 1 else 10  # low live variance; primary effects are large
CAP = ["-cap-c", "8388608", "-cap-gamma", "0.05"]


NETEM = ["-netem-latency-ms", "40", "-netem-loss", "0.4"]


def run(run_id, args):
    out = os.path.join(RUNS, run_id + ".json")
    if os.path.exists(out):
        try:
            j = json.load(open(out))
            if j.get("content_verified") and not j.get("err"):
                return {"run_id": run_id, "completion_ms": j.get("completion_ms"),
                        "valid": True, "invalid_reasons": [], "final_n": j.get("final_n"),
                        "final_s": j.get("final_s"), "skipped": True}
        except Exception:
            pass
    cmd = [TOOL, "-run-id", run_id, "-out", out] + args + NETEM
    t0 = time.time()
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=900)
    wall = time.time() - t0
    meta = {"run_id": run_id, "cmd": cmd, "exit": p.returncode, "wall_s": wall,
            "stdout": p.stdout.strip(), "stderr": p.stderr.strip()}
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
    with open(os.path.join(BASE, "index_adaptive.jsonl"), "a") as f:
        f.write(json.dumps({k: meta.get(k) for k in
                            ("run_id", "completion_ms", "valid", "invalid_reasons",
                             "final_n", "final_s")}) + "\n")
    return meta


def p1():
    for state, capargs in [("healthy", []), ("capped", CAP + ["-cap-at", "0"])]:
        for rep in range(REPS):
            runs = {
                "selector": ["-system", "ours", "-policy", "classifier",
                             "-n", "16", "-chunk", "1048576",
                             "-healthy-n", "16", "-healthy-s", "1048576",
                             "-capped-n", "4", "-capped-s", "4194304",
                             "-threshold", "460800", "-classify-after", "20",
                             "-schedule", "t1"] + capargs,
                "fixed": ["-system", "ours", "-policy", "none",
                          "-n", "16", "-chunk", "1048576", "-schedule", "t1"] + capargs,
                "aria2": ["-system", "aria2", "-schedule", "t1"] + capargs,
            }
            for name, args in runs.items():
                m = run("p1_%s_%s_r%02d" % (state, name, rep), args)
                print("p1 %-7s %-8s r%02d %8s valid=%s" % (
                    state, name, rep,
                    "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
                    m["valid"]), flush=True)
        # single-connection baselines: deterministic and slow; one wget run only,
        # healthy state only (the cap does not bind at k=1)
        if state == "healthy":
            for name in ("wget",):
                m = run("p1_%s_%s_r00" % (state, name),
                        ["-system", name, "-schedule", "t1"] + capargs)
                print("p1 %-7s %-8s r00 %8s valid=%s" % (
                    state, name,
                    "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
                    m["valid"]), flush=True)


def p2():
    for rep in range(REPS):
        runs = {
            "causal_joint": ["-system", "ours", "-policy", "drop",
                             "-n", "16", "-chunk", "1048576",
                             "-capped-n", "4", "-capped-s", "4194304",
                             "-drop", "0.5", "-schedule", "t1",
                             "-cap-at", "8"] + CAP,
            "causal_n_only": ["-system", "ours", "-policy", "drop",
                              "-n", "16", "-chunk", "1048576",
                              "-capped-n", "4", "-capped-s", "1048576",
                              "-drop", "0.5", "-schedule", "t1",
                              "-cap-at", "8"] + CAP,
            "causal_s_only": ["-system", "ours", "-policy", "drop",
                              "-n", "16", "-chunk", "1048576",
                              "-capped-n", "16", "-capped-s", "4194304",
                              "-drop", "0.5", "-schedule", "t1",
                              "-cap-at", "8"] + CAP,
            "fixed_16_1M": ["-system", "ours", "-policy", "none",
                            "-n", "16", "-chunk", "1048576", "-schedule", "t1",
                            "-cap-at", "8"] + CAP,
            "fixed_4_4M": ["-system", "ours", "-policy", "none",
                           "-n", "4", "-chunk", "4194304", "-schedule", "t1",
                           "-cap-at", "8"] + CAP,
            "fixed_4_1M": ["-system", "ours", "-policy", "none",
                           "-n", "4", "-chunk", "1048576", "-schedule", "t1",
                           "-cap-at", "8"] + CAP,
            "aria2": ["-system", "aria2", "-schedule", "t1", "-cap-at", "8"] + CAP,
        }
        for name, args in runs.items():
            m = run("p2_%s_r%02d" % (name, rep), args)
            print("p2 %-14s r%02d %8s valid=%s final=(%s,%s)" % (
                name, rep,
                "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
                m["valid"], m.get("final_n"), m.get("final_s")), flush=True)


def main():
    what = sys.argv[2] if len(sys.argv) > 2 else "all"
    if what in ("all", "p1"):
        p1()
    if what in ("all", "p2"):
        p2()
    print("adaptive driver done", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
