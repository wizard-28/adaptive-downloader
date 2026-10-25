#!/usr/bin/env python3
"""Run one stage of baselines_ext exactly as manifest.json declares.

Usage: run.py BINARY STAGE   (STAGE = E)
Each run writes runs_<STAGE>/<run_id>.json (rl_bench output) and appends one line
to index_<STAGE>.jsonl. Existing valid results are skipped, so the runner resumes.
Invalid runs are kept and never re-run automatically.
"""
import json, os, random, subprocess, sys, time

BASE = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
M = json.load(open(os.path.join(BASE, "manifest.json")))


def schedule(st):
    rng = random.Random(st["seed"])
    out = []
    for sc in st["scenario_order"]:
        for b in range(st["blocks"]):
            arms = list(st["scenario_arms"][sc])
            rng.shuffle(arms)
            for a in arms:
                out.append((sc, b, a))
    return out


def args_for(sc, arm):
    s = M["scenarios"][sc]
    a = list(M["common_args"]) + list(M["arms"][arm])
    if s["uses_cap"]:
        a += M["cap_args"]
    return a + list(s["extra"])


def run(binary, run_dir, index, run_id, args):
    out = os.path.join(run_dir, run_id + ".json")
    if os.path.exists(out):
        return None
    cmd = [binary, "-run-id", run_id, "-out", out] + args
    t0 = time.time()
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=1500)
    meta = {"run_id": run_id, "cmd": cmd[1:], "exit": p.returncode, "wall_s": round(time.time() - t0, 3),
            "stderr_tail": p.stderr[-400:]}
    try:
        r = json.load(open(out))
        meta.update(completion_ms=r.get("completion_ms"), content_verified=r.get("content_verified"),
                    err=r.get("err"), splits=r.get("config", {}).get("splits"))
    except Exception as e:  # missing/unparsable result is recorded, not hidden
        meta["parse_error"] = str(e)
    meta["valid"] = p.returncode == 0 and meta.get("content_verified") is True and not meta.get("err")
    with open(index, "a") as f:
        f.write(json.dumps(meta) + "\n")
    return meta


def main():
    binary = os.path.abspath(sys.argv[1])
    stage = sys.argv[2]
    st = M["stages"][stage]
    run_dir = os.path.join(BASE, "runs_" + stage)
    os.makedirs(run_dir, exist_ok=True)
    index = os.path.join(BASE, f"index_{stage}.jsonl")
    plan = schedule(st)
    for i, (sc, b, arm) in enumerate(plan, 1):
        rid = f"{sc}_b{b:02d}_{arm}"
        m = run(binary, run_dir, index, rid, args_for(sc, arm))
        if m is not None:
            c = m.get("completion_ms")
            print(f"[{i}/{len(plan)}] {rid} {'%.1fs' % (c / 1000) if c else '?'} valid={m['valid']}", flush=True)
    subprocess.run(["sudo", "-n", "tc", "qdisc", "show", "dev", "lo"])


if __name__ == "__main__":
    main()
