#!/usr/bin/env python3
"""Phase 4b: frozen adaptive/selector comparisons across W1-W6.

PRECONDITION: manifest/w1_w6_manifest.json (frozen) and
analysis/w1_w6_grid.json (live fixed-grid validation) must exist.

Per workload, 10 fresh paired blocks:
  band_selector          (frozen levels/targets from the manifest)
  per_workload_best_fixed (live fixed-grid argmin for that workload)
  family_best_fixed       (live single fixed config minimizing the mean over
                           the six workloads)
  aria2                   (frozen aria2 config)
All runs fresh, content-verified; invalid runs preserved and reported
(selector aborts count as losses).  Analysis: paired per-block ratios,
geometric mean, bootstrap 95 % CI, win counts.
"""
import json
import os
import random
import statistics
import subprocess
import time

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
TOOL = os.path.join(BASE, "tools", "ns_bench")
MAN = os.path.join(BASE, "manifest", "w1_w6_manifest.json")
GRID = os.path.join(BASE, "analysis", "w1_w6_grid.json")
RUNS = os.path.join(BASE, "w1_w6_adaptive")
LOGS = os.path.join(BASE, "logs")
ANALYSIS = os.path.join(BASE, "analysis")
for d in (RUNS, LOGS, ANALYSIS):
    os.makedirs(d, exist_ok=True)

NETEM = ["-netem-latency-ms", "40", "-netem-loss", "0.4"]
BLOCKS = 10


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
    with open(os.path.join(BASE, "index_w1_w6_adaptive.jsonl"), "a") as f:
        f.write(json.dumps({k: meta.get(k) for k in
                            ("run_id", "completion_ms", "valid", "invalid_reasons")}) + "\n")
    return meta


def common_args(w):
    args = ["-schedule", w["schedule"]]
    if w["cap_c"]:
        args += ["-cap-c", str(w["cap_c"]), "-cap-gamma", str(w["cap_gamma"]),
                 "-cap-at", str(w["cap_at_s"])]
    return args


def parse_cell(k):
    n, s = k.split("/")
    return int(n), int(s)


def main():
    man = json.load(open(MAN))
    grid = json.load(open(GRID))
    workloads = man["workloads"]
    sel = man["selector"]
    levels = ",".join(str(v) for v in sel["levels_bytes_s"])
    targets = ",".join(sel["targets"])
    family_cfg = grid["family_best_single_fixed"]
    fn, fs = parse_cell(family_cfg)

    mode_args = {}
    for wid, w in sorted(workloads.items()):
        live = grid["workloads"][wid]["live_argmin"]
        pw_n, pw_s = parse_cell(live)
        mode_args[wid] = {
            "band_selector": ["-system", "ours", "-policy", "band",
                              "-n", "16", "-chunk", "1048576",
                              "-band-levels", levels, "-band-targets", targets,
                              "-band-window", str(sel["classify_window_s"]),
                              "-deadline", str(sel["deadline_s"])] + common_args(w),
            "per_workload_best_fixed": ["-system", "ours", "-policy", "none",
                                        "-n", str(pw_n), "-chunk", str(pw_s)] + common_args(w),
            "family_best_fixed": ["-system", "ours", "-policy", "none",
                                  "-n", str(fn), "-chunk", str(fs)] + common_args(w),
            "aria2": ["-system", "aria2"] + common_args(w),
        }
        print("%s (%s): per-workload best fixed %s; family best fixed %s" % (
            wid, w["label"], live, family_cfg), flush=True)

    for rep in range(BLOCKS):
        for wid in sorted(workloads):
            for mode in ("band_selector", "per_workload_best_fixed",
                         "family_best_fixed", "aria2"):
                rid = "%s_%s_r%02d" % (wid, mode, rep)
                m = run(rid, mode_args[wid][mode])
                print("%s %-23s r%02d %8s valid=%s final=(%s,%s)" % (
                    wid, mode, rep,
                    "%.1fs" % (m["completion_ms"] / 1000) if m.get("completion_ms") else "?",
                    m["valid"], m.get("final_n"), m.get("final_s")), flush=True)

    def load(wid, mode, rep):
        p = os.path.join(RUNS, "%s_%s_r%02d.json" % (wid, mode, rep))
        if not os.path.exists(p):
            return None
        j = json.load(open(p))
        if j.get("content_verified") and not j.get("err") and j.get("completion_ms"):
            return j["completion_ms"] / 1000.0
        return None

    results = {}
    pooled = {"sel_vs_per_workload": [], "sel_vs_family": [], "sel_vs_aria2": []}
    for wid in sorted(workloads):
        per = {}
        for mode in ("band_selector", "per_workload_best_fixed",
                     "family_best_fixed", "aria2"):
            vals = [load(wid, mode, r) for r in range(BLOCKS)]
            per[mode] = {"valid_s": [v for v in vals if v is not None],
                         "invalid": sum(1 for v in vals if v is None),
                         "median_s": statistics.median([v for v in vals if v is not None])
                                     if any(v is not None for v in vals) else None}
        ratios = {}
        for base in ("per_workload_best_fixed", "family_best_fixed", "aria2"):
            rs, wins, losses = [], 0, 0
            for r in range(BLOCKS):
                a = load(wid, "band_selector", r)
                b = load(wid, base, r)
                if a is None or b is None:
                    losses += 1 if a is None else 0
                    continue
                rs.append(a / b)
                wins += a < b
                losses += a >= b
            rs_sorted = sorted(rs)
            ci = None
            if rs:
                random.seed(20261008)
                boot = sorted(statistics.geometric_mean(random.choices(rs, k=len(rs)))
                              for _ in range(20000))
                ci = [boot[int(0.025 * len(boot))], boot[int(0.975 * len(boot))]]
            ratios[base] = {"n": len(rs), "wins": wins, "losses": losses,
                            "geo_mean": (statistics.geometric_mean(rs) if rs else None),
                            "ci95": ci,
                            "ratio_range": [rs_sorted[0], rs_sorted[-1]] if rs else None,
                            "ratios": rs}
            pooled["sel_vs_" + ("per_workload" if base == "per_workload_best_fixed"
                                else "family" if base == "family_best_fixed" else "aria2")
                   ].extend(rs)
        per["paired"] = ratios
        results[wid] = per
        print("%s selector median %s | per-workload %s | family %s | aria2 %s" % (
            wid,
            "%.1fs" % per["band_selector"]["median_s"] if per["band_selector"]["median_s"] else "?",
            "%.1fs" % per["per_workload_best_fixed"]["median_s"] if per["per_workload_best_fixed"]["median_s"] else "?",
            "%.1fs" % per["family_best_fixed"]["median_s"] if per["family_best_fixed"]["median_s"] else "?",
            "%.1fs" % per["aria2"]["median_s"] if per["aria2"]["median_s"] else "?"), flush=True)

    random.seed(20261008)
    summary = {}
    for key, rs in pooled.items():
        if not rs:
            summary[key] = {"n": 0}
            continue
        gm = statistics.geometric_mean(rs)
        boot = []
        for _ in range(20000):
            boot.append(statistics.geometric_mean(random.choices(rs, k=len(rs))))
        boot.sort()
        summary[key] = {"n": len(rs), "geo_mean": gm,
                        "ci95": [boot[int(0.025 * len(boot))], boot[int(0.975 * len(boot))]],
                        "wins": sum(1 for v in rs if v < 1), "losses": sum(1 for v in rs if v >= 1)}
    out = {"blocks": BLOCKS, "family_best_single_fixed": family_cfg,
           "selector_levels": levels, "selector_targets": targets,
           "workloads": results, "pooled": summary}
    json.dump(out, open(os.path.join(ANALYSIS, "w1_w6_adaptive.json"), "w"), indent=2)
    print(json.dumps(summary, indent=2))
    print("w1_w6 adaptive done", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
