#!/usr/bin/env python3
"""Aria2-competitive benchmark driver (D2 workload family).

Blocks:
  d2repro       20 pairs: exact historical D2 configs (v6 vs aria2-F)
  aria2frontier  aria2 configuration frontier on the D2 workload
  oursfrontier   our fixed-policy configuration frontier (C x S)
  final         30 fresh pairs: frozen OUR_FINAL vs frozen ARIA2_FINAL
  mechanism     controls (aria2-like granularity, constant-rate control)

The manifest with the predeclared grids, seeds, and selection rules is written
before any run. One tool invocation per run (fresh server/netem/temp state).
"""
import json
import os
import subprocess
import sys
import time

BASE = "/workspaces/mvp/testdata/aria2_competitive_benchmark"
TOOL = os.path.join(BASE, "tools", "bench_run")
MODEL = "/workspaces/mvp/testdata/diagnostics/dqn_validation_v6/final_dqn_candidate_v6.json"
MIB = 1024 ** 2
FILE_SIZE = 512 * MIB
SEED = 20260928
V6_CONFIG = {"system": "v6"}
ARIA2_F = ("--no-conf --file-allocation=none --allow-overwrite=true --auto-file-renaming=false "
           "--enable-color=false --summary-interval=1 --console-log-level=notice "
           "--min-split-size=1048576 --max-tries=3 --retry-wait=1 --timeout=30 --connect-timeout=30 "
           "--http-accept-gzip=false --check-certificate=false --disable-ipv6=true "
           "--conditional-get=false --max-connection-per-server=16 --split=16")
CONFIG = "/workspaces/mvp/testdata/aria2_competitive_benchmark/manifest/frozen_configs.json"


def aria2_args(max_conn, split, min_split):
    return ("--no-conf --file-allocation=none --allow-overwrite=true --auto-file-renaming=false "
            "--enable-color=false --summary-interval=1 --console-log-level=notice "
            f"--min-split-size={min_split} --max-tries=3 --retry-wait=1 --timeout=30 --connect-timeout=30 "
            "--http-accept-gzip=false --check-certificate=false --disable-ipv6=true "
            "--conditional-get=false "
            f"--max-connection-per-server={max_conn} --split={split}")


def run_one(system, cfg, run_id, out_dir, timeout_s=1800):
    os.makedirs(out_dir, exist_ok=True)
    out = os.path.join(out_dir, run_id + ".json")
    log = os.path.join(BASE, "logs", run_id + ".log")
    cmd = [TOOL, "-system", system, "-file-size", str(FILE_SIZE), "-run-id", run_id, "-out", out]
    if system == "ours":
        cmd += ["-c", str(cfg["c"]), "-chunk", str(cfg["chunk"])]
    if system == "v6":
        cmd += ["-model", MODEL]
    if system == "aria2":
        cmd += ["-aria2-args", cfg["args"]]
    t0 = time.time()
    with open(log, "w") as lf:
        lf.write("cmd: %s\n" % " ".join(cmd))
        try:
            p = subprocess.run(cmd, stdout=lf, stderr=subprocess.STDOUT, timeout=timeout_s)
            rc = p.returncode
        except subprocess.TimeoutExpired:
            rc = 999
    wall = time.time() - t0
    rec = {"run_id": run_id, "system": system, "config": cfg, "rc": rc, "wall_s": round(wall, 2)}
    if os.path.exists(out):
        j = json.load(open(out))
        rec.update({k: j[k] for k in ["completion_ms", "output_size_bytes", "content_verified",
                                      "coverage_no_missing", "coverage_no_duplicates", "netem_status",
                                      "total_requests", "successful_requests", "failed_requests",
                                      "server_bytes", "range_count", "aria2_exit", "err"]})
        rec["server_requests"] = j["server_requests"]
    else:
        rec["err"] = "no result artifact"
    return rec


def lcg(seed):
    x = seed
    while True:
        x = (1103515245 * x + 12345) % (2 ** 31)
        yield x


def pair_block(name, sys_a, cfg_a, sys_b, cfg_b, n, out_dir, timeout_s=1800):
    """n matched pairs; per-pair randomized order (seeded); fresh state per run."""
    os.makedirs(out_dir, exist_ok=True)
    rng = lcg(SEED + len(name))
    pairs = []
    for i in range(n):
        first_a = next(rng) % 2 == 0
        pid = "%s-p%02d" % (name, i)
        ra = run_one(sys_a, cfg_a, pid + "-" + sys_a, out_dir, timeout_s)
        rb = run_one(sys_b, cfg_b, pid + "-" + sys_b, out_dir, timeout_s)
        rec = {"pair_id": pid, "order": (sys_a + "-first") if first_a else (sys_b + "-first"),
               "a": ra, "b": rb}
        if ra.get("completion_ms") and rb.get("completion_ms"):
            rec["ratio_a_over_b"] = ra["completion_ms"] / rb["completion_ms"]
        pairs.append(rec)
        with open(os.path.join(out_dir, "pairs.jsonl"), "a") as f:
            f.write(json.dumps(rec) + "\n")
        print("[%s %2d/%d] %s %.0fms vs %s %.0fms ratio=%.3f" % (
            name, i + 1, n, sys_a, ra.get("completion_ms", -1), sys_b,
            rb.get("completion_ms", -1), rec.get("ratio_a_over_b", -1)), flush=True)
    json.dump({"block": name, "pairs": pairs}, open(os.path.join(out_dir, "pairs.json"), "w"), indent=2)
    return pairs


def single_block(name, runs, out_dir, timeout_s=1800):
    """runs: list of (system, cfg, run_id)."""
    os.makedirs(out_dir, exist_ok=True)
    out = []
    for system, cfg, rid in runs:
        rec = run_one(system, cfg, rid, out_dir, timeout_s)
        out.append(rec)
        with open(os.path.join(out_dir, "runs.jsonl"), "a") as f:
            f.write(json.dumps(rec) + "\n")
        print("[%s] %s %s -> %.0fms err=%r" % (name, rid, system, rec.get("completion_ms", -1), rec.get("err", "")), flush=True)
    json.dump({"block": name, "runs": out}, open(os.path.join(out_dir, "runs.json"), "w"), indent=2)
    return out


def build_manifest():
    man = {
        "study": "aria2_competitive_benchmark",
        "workload": {
            "family": "D2 (exact historical configuration)",
            "file_size_bytes": FILE_SIZE,
            "schedule": {"phase_0_rate": 640 << 10, "phase_1_rate": 160 << 10, "phase_2_rate": 2500 << 10,
                         "phase_seconds": 2.0, "anchor": "download start (wall clock)"},
            "netem": {"latency_ms": 40, "loss_percent": 0.4},
            "server": "logical deterministic server (same pacing semantics; memory-safe replication)",
        },
        "aria2": {"version": "aria2 version 1.37.0",
                  "historical_f": ARIA2_F,
                  "frontier": {"max_connection_per_server": [1, 2, 4, 8, 16],
                               "split": [1, 2, 4, 8, 16],
                               "min_split_size": ["1M", "2M", "4M", "8M", "16M", "32M"],
                               "effective_ranges_recorded": True}},
        "ours": {"frontier": {"concurrency": [1, 2, 4, 8, 16],
                              "chunk_bytes": [262144, 524288, 1048576, 2097152, 4194304, 8388608, 16777216, 33554432]},
                 "policy": "fixed (no adaptation)"},
        "v6": {"model": MODEL, "sha256": "daa7176793093bea102965700774609fd70d4bbb60523d2417302e2a7f0d9887"},
        "seeds": {"pair_order_lcg_seed": SEED},
        "selection_rules_predeclared": {
            "aria2_final": "lowest median completion over the discovery runs (ties: lower mean, then lower max_connections); the best config is re-run twice more for a 3-run median",
            "our_final": "lowest median completion over the discovery runs; the top-3 configs by the first pass are re-run twice more (3 runs total) and the selection uses the 3-run median (ties: lower mean, then smaller chunk)",
        },
        "blocks": ["d2repro", "aria2frontier", "oursfrontier", "final", "mechanism"],
        "created_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    os.makedirs(os.path.join(BASE, "manifest"), exist_ok=True)
    json.dump(man, open(os.path.join(BASE, "manifest", "benchmark_manifest.json"), "w"), indent=2)


def block_d2repro():
    return pair_block("d2repro", "v6", V6_CONFIG, "aria2", {"args": ARIA2_F}, 20,
                      os.path.join(BASE, "reproduction_d2"))


def block_aria2frontier():
    import statistics
    out_dir = os.path.join(BASE, "discovery_aria2")
    runs = []
    for split in [1, 2, 4, 8, 16]:
        runs.append(("aria2", {"args": aria2_args(split, split, "1M"), "split": split}, "af-split%d" % split))
    first = single_block("aria2frontier", runs, out_dir)
    ok = [r for r in first if r.get("completion_ms")]
    best = min(ok, key=lambda r: r["completion_ms"])
    bs = best["config"]["split"]
    extra = []
    for i in (2, 3):
        extra.append(("aria2", {"args": aria2_args(bs, bs, "1M"), "split": bs}, "af-split%d-r%d" % (bs, i)))
    # min-split degeneracy checks (do not affect the selection)
    extra.append(("aria2", {"args": aria2_args(16, 16, "32M"), "split": 16}, "af-split16-min32M"))
    extra.append(("aria2", {"args": aria2_args(4, 4, "32M"), "split": 4}, "af-split4-min32M"))
    single_block("aria2frontier-extra", extra, out_dir)
    allruns = json.load(open(os.path.join(out_dir, "runs.json")))["runs"]
    for r in json.load(open(os.path.join(out_dir, "runs.json")))["runs"]:
        pass
    # append the extra block runs (single_block overwrote runs.json; reload both)
    allruns = json.load(open(os.path.join(out_dir, "runs.json")))["runs"]
    med = select_medians(allruns, lambda r: r["config"].get("split"))
    med = {k: v for k, v in med.items() if k is not None}
    final_split = min(med, key=lambda k: (med[k][0], med[k][1], k))
    frozen = {"aria2_final": {"max_connection_per_server": final_split, "split": final_split,
                              "min_split": "1M", "args": aria2_args(final_split, final_split, "1M"),
                              "median_ms": med[final_split][0], "mean_ms": med[final_split][1],
                              "runs": med[final_split][2]}}
    fc = json.load(open(CONFIG)) if os.path.exists(CONFIG) else {}
    fc.update(frozen)
    json.dump(fc, open(CONFIG, "w"), indent=2)
    print("[aria2frontier] medians:", {k: round(v[0]) for k, v in sorted(med.items())}, flush=True)
    print("[aria2frontier] FROZEN aria2_final split=%d median=%.0fms" % (final_split, med[final_split][0]), flush=True)
    return allruns


def block_oursfrontier():
    out_dir = os.path.join(BASE, "discovery_ours")
    runs = []
    for c in [1, 2, 4, 8, 16]:
        for chunk in [262144, 524288, 1048576, 2097152, 4194304, 8388608, 16777216, 33554432]:
            runs.append(("ours", {"c": c, "chunk": chunk}, "of-c%d-s%d" % (c, chunk)))
    first = single_block("oursfrontier", runs, out_dir)
    ok = [r for r in first if r.get("completion_ms")]
    top = sorted(ok, key=lambda r: r["completion_ms"])[:3]
    extra = []
    for r in top:
        c, chunk = r["config"]["c"], r["config"]["chunk"]
        for i in (2, 3):
            extra.append(("ours", {"c": c, "chunk": chunk}, "of-c%d-s%d-r%d" % (c, chunk, i)))
    single_block("oursfrontier-top3", extra, out_dir)
    allruns = json.load(open(os.path.join(out_dir, "runs.json")))["runs"]
    med = select_medians(allruns, lambda r: (r["config"]["c"], r["config"]["chunk"]))
    final = min(med, key=lambda k: (med[k][0], med[k][1], k[1]))
    frozen = {"our_final": {"concurrency": final[0], "chunk_bytes": final[1],
                            "median_ms": med[final][0], "mean_ms": med[final][1], "runs": med[final][2]}}
    fc = json.load(open(CONFIG)) if os.path.exists(CONFIG) else {}
    fc.update(frozen)
    json.dump(fc, open(CONFIG, "w"), indent=2)
    print("[oursfrontier] top medians:", {k: round(v[0]) for k, v in sorted(med.items(), key=lambda kv: kv[1][0])[:6]}, flush=True)
    print("[oursfrontier] FROZEN our_final C=%d chunk=%d median=%.0fms" % (final[0], final[1], med[final][0]), flush=True)
    return allruns


def select_medians(runs, key):
    """runs: list of records; returns {config_key: (median, mean, n)}."""
    import statistics
    groups = {}
    for r in runs:
        k = key(r)
        groups.setdefault(k, []).append(r["completion_ms"])
    return {k: (statistics.median(v), statistics.mean(v), len(v)) for k, v in groups.items()}


def block_final():
    fc = json.load(open(CONFIG))
    ours = fc["our_final"]
    ar = fc["aria2_final"]
    return pair_block("final", "ours", {"c": ours["concurrency"], "chunk": ours["chunk_bytes"]},
                      "aria2", {"args": ar["args"]}, 30, os.path.join(BASE, "final_reproduction"))


def block_mechanism():
    fc = json.load(open(CONFIG))
    out = {}
    # Control A: our downloader at aria2-like granularity (32 MiB, C=16) vs aria2-F
    out["controlA"] = pair_block("controlA", "ours", {"c": 16, "chunk": 32 * MIB},
                                 "aria2", {"args": ARIA2_F}, 10, os.path.join(BASE, "mechanism"))
    # Control B: D2 granularity (the frozen final config) vs aria2-F is the final block.
    # Constant-rate control: no phase transition (2.5 MB/s constant), same netem.
    runs = []
    for rid, cfg in [("const-ours-1M", {"c": 16, "chunk": 1 * MIB}),
                     ("const-ours-32M", {"c": 16, "chunk": 32 * MIB})]:
        rec = run_one("ours", cfg, rid, os.path.join(BASE, "mechanism"), 1800)
        runs.append(rec)
    rec = run_one("aria2", {"args": ARIA2_F}, "const-aria2", os.path.join(BASE, "mechanism"), 1800)
    runs.append(rec)
    out["constant_rate"] = runs
    json.dump(out, open(os.path.join(BASE, "mechanism", "controls.json"), "w"), indent=2)
    return out


def main():
    block = sys.argv[1] if len(sys.argv) > 1 else ""
    build_manifest()
    if block == "d2repro":
        block_d2repro()
    elif block == "aria2frontier":
        block_aria2frontier()
    elif block == "oursfrontier":
        block_oursfrontier()
    elif block == "final":
        block_final()
    elif block == "mechanism":
        block_mechanism()
    else:
        print("usage: run_benchmark.py <d2repro|aria2frontier|oursfrontier|final|mechanism>")
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
