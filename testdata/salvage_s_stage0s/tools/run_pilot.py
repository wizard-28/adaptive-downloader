#!/usr/bin/env python3
"""Frozen Stage 0S pilot driver (Salvage Protocol v7).

Runs exactly 32 planned runs = 8 replicate blocks x 4 fixed chunk-size arms,
sequentially, in a pre-registered randomized order within each block. Writes
the audit manifest before Run 1 and an incremental run ledger. It never
inspects intermediate results to change the design, never reruns a failed run
(the frozen protocol contains no technical-rerun authorization), and never
starts a switching strategy.

Execution artifacts only; no repository source is modified.
"""
import hashlib
import json
import os
import platform
import random
import subprocess
import sys
import time

BASE = "/workspaces/mvp/testdata/salvage_s_stage0s"
RUNNER = os.path.join(BASE, "tools", "stage0s_run")
PROTOCOL = "/workspaces/mvp/testdata/salvage_s_protocol_v7/final_s_protocol.json"
ARMS = [("256KiB", 262144), ("1MiB", 1048576), ("4MiB", 4194304), ("16MiB", 16777216)]
BLOCKS = 8
SEED = 20260928  # fixed before execution; recorded in the manifest
RUN_TIMEOUT_S = 900


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def build_manifest():
    rng = random.Random(SEED)
    arm_by_label = dict(ARMS)
    blocks = []
    planned = []
    for b in range(BLOCKS):
        labels = [a[0] for a in ARMS]
        rng.shuffle(labels)
        runs = []
        for o, label in enumerate(labels):
            rid = "b%d_o%d_%s" % (b, o, label)
            runs.append({"order": o, "arm_label": label, "arm_bytes": arm_by_label[label], "run_id": rid})
            planned.append({"run_id": rid, "block": b, "order": o, "arm_label": label,
                            "arm_bytes": arm_by_label[label]})
        blocks.append({"block": b, "order": runs})
    return blocks, planned


def cleanup_check():
    def running(name):
        p = subprocess.run(["pgrep", "-x", name], capture_output=True, text=True)
        return p.stdout.strip() != ""
    qdisc = subprocess.run(["bash", "-c", "tc qdisc show | grep -v 'qdisc noqueue' || true"],
                           capture_output=True, text=True).stdout.strip()
    netns = subprocess.run(["ip", "netns", "list"], capture_output=True, text=True).stdout.strip()
    return {"stage0s_run_running": running("stage0s_run"), "mm_link_running": running("mm-link"),
            "qdisc_clean": qdisc == "", "netns_clean": netns == ""}


def main():
    protocol = json.load(open(PROTOCOL))
    blocks, planned = build_manifest()
    manifest = {
        "protocol_id": protocol["protocol_id"],
        "protocol_version": protocol["protocol_version"],
        "protocol_status": protocol["status"],
        "protocol_json_sha256": sha256(PROTOCOL),
        "frozen_mechanism": protocol["human_decision_record"]["frozen_mechanism_package"],
        "frozen_quantities": protocol["human_decision_record"]["frozen_quantities"],
        "chunk_size_arms": [{"label": l, "bytes": b} for l, b in ARMS],
        "replicate_blocks": BLOCKS,
        "planned_runs": 32,
        "randomization": {"method": "python random.Random(seed).shuffle of the four arms within each block",
                          "seed": SEED, "blocks": blocks},
        "planned_run_ids": [p["run_id"] for p in planned],
        "runner": {"path": RUNNER, "sha256": sha256(RUNNER), "source": "tools/stage0s_run.go",
                   "constants": {"file_size_bytes": 536870912, "rate_bytes_per_second": 500000,
                                 "delay_ms": 40, "concurrency": 4, "timeout_ms": 600000,
                                 "dial_timeout_ms": 30000, "max_retries": 3}},
        "server_config": {"implementation": "internal/experiment/server.NewLogical",
                          "content_source": "deterministic generated logical object (never materialized; bounded memory)",
                          "rate_bytes_per_second": 500000, "delay_ms": 40, "request_log": True,
                          "http": "HTTP/1.1 only (project fetcher transport)"},
        "client_config": {"implementation": "internal/downloader (fixed mode; no policy)",
                          "concurrency": 4, "chunk_size": "fixed per run (one of the four arms)",
                          "timeout_ms": 600000, "dial_timeout_ms": 30000, "max_retries": 3},
        "environment": {
            "hostname": platform.node(), "platform": platform.platform(), "python": platform.python_version(),
            "cpus": os.cpu_count(), "date_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "go_version": subprocess.run(["go", "version"], capture_output=True, text=True).stdout.strip(),
        },
        "preflight": {"smoke_tool": "tools/preflight/preflight_smoke (8 MiB, non-frozen; excluded from the ledger)",
                      "smoke_result": "OK (mechanism present)"},
        "technical_rerun_policy": "the frozen protocol contains no technical-rerun authorization; failed runs are recorded and not rerun",
        "experiment_start_timestamp_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    os.makedirs(os.path.join(BASE, "manifest"), exist_ok=True)
    with open(os.path.join(BASE, "manifest", "manifest.json"), "w") as f:
        json.dump(manifest, f, indent=2)
    print("manifest written: 32 planned runs, seed", SEED, flush=True)

    ledger_path = os.path.join(BASE, "manifest", "run_ledger.jsonl")
    open(ledger_path, "w").close()
    ledger = []
    for i, run in enumerate(planned):
        rid = run["run_id"]
        out = os.path.join(BASE, "raw", rid + ".json")
        log = os.path.join(BASE, "logs", rid + ".log")
        cmd = [RUNNER, "-block", str(run["block"]), "-arm", str(run["arm_bytes"]),
               "-order", str(run["order"]), "-run-id", rid, "-out", out]
        start = time.time()
        rc = None
        with open(log, "w") as lf:
            lf.write("cmd: %s\n" % " ".join(cmd))
            try:
                p = subprocess.run(cmd, stdout=lf, stderr=subprocess.STDOUT, timeout=RUN_TIMEOUT_S)
                rc = p.returncode
            except subprocess.TimeoutExpired:
                rc = 999
        end = time.time()
        entry = {"run_id": rid, "block": run["block"], "order": run["order"],
                 "arm_label": run["arm_label"], "arm_bytes": run["arm_bytes"],
                 "planned": True, "attempted": True, "exit_code": rc,
                 "start_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(start)),
                 "end_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(end)),
                 "wall_seconds": round(end - start, 3)}
        if os.path.exists(out):
            raw = json.load(open(out))
            entry["clean"] = bool(raw.get("clean"))
            entry["failure_reason"] = raw.get("failure_reason", "")
            entry["duration_ns"] = raw.get("duration_ns")
        else:
            entry["clean"] = False
            entry["failure_reason"] = "no raw artifact written (exit %s)" % rc
        entry["cleanup"] = cleanup_check()
        entry["status"] = "clean" if entry["clean"] and rc == 0 else "technical_failure"
        ledger.append(entry)
        with open(ledger_path, "a") as f:
            f.write(json.dumps(entry) + "\n")
        print("[%2d/32] %s clean=%s rc=%s wall=%.1fs %s" % (
            i + 1, rid, entry["clean"], rc, entry["wall_seconds"], entry["failure_reason"]), flush=True)

    with open(os.path.join(BASE, "manifest", "run_ledger.json"), "w") as f:
        json.dump({"planned": len(planned), "attempted": len(ledger),
                   "clean": sum(1 for e in ledger if e["clean"]),
                   "technical_failures": sum(1 for e in ledger if not e["clean"]),
                   "reruns": 0, "ledger": ledger}, f, indent=2)
    clean = sum(1 for e in ledger if e["clean"])
    print("DONE planned=%d attempted=%d clean=%d failures=%d" % (
        len(planned), len(ledger), clean, len(ledger) - clean), flush=True)
    return 0 if clean == len(planned) else 1


if __name__ == "__main__":
    sys.exit(main())
