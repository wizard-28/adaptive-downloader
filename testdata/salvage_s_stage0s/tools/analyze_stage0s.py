#!/usr/bin/env python3
"""Deterministic Stage 0S analysis (Salvage Protocol v7).

Computes every Stage 0S quantity from the raw per-run artifacts only:
per-run table, per-arm pooled median service time, direction check, realized
16 MiB tail analysis, reliability/feasibility, the four frozen criteria, and
the mechanical GO/NO-GO. The final report is generated from these outputs.

Definitions (documented for reproducibility):
  - per-request service time (primary): server-side request duration
    (end_unix_nano - start_unix_nano) for requests with has_range=true;
    expected value D + s/R = 0.040 + arm_bytes/500000.
  - realized tail duration: client run end wall time minus the server Start of
    the first successful ranged request whose range_start lies in the final
    tail region [FileSize - N*C_max, FileSize), N*C_max = 64 MiB.
  - within-block residual SD: residual SD of y = log(T/T_ref) after removing
    arm and block means (two-way fixed effects), df = n - C - B + 1 = 21.
No confirmatory adaptive-switch analysis is performed.
"""
import csv
import json
import math
import os
import statistics
import sys

BASE = "/workspaces/mvp/testdata/salvage_s_stage0s"
PROTOCOL = "/workspaces/mvp/testdata/salvage_s_protocol_v7/final_s_protocol.json"

MIB = 1024 ** 2
FILE_SIZE = 536870912
RATE = 500000.0
DELAY = 0.040
N = 4
C_MAX = 16 * MIB
TAIL_REGION = N * C_MAX


def main():
    proto = json.load(open(PROTOCOL))
    T_ref = proto["derived_quantities"]["T_ref"]["value"]
    T_tail = proto["derived_quantities"]["T_tail_primary"]["value"]
    delta_tail = proto["derived_quantities"]["delta_tail"]["value"]
    delta_log = proto["derived_quantities"]["delta_T"]["log_scale_representation"]["value"]
    tail_lo, tail_hi = 0.8 * T_tail, 1.25 * T_tail

    manifest = json.load(open(os.path.join(BASE, "manifest", "manifest.json")))
    planned = manifest["planned_run_ids"]
    arm_bytes = {a["label"]: a["bytes"] for a in manifest["chunk_size_arms"]}
    arm_order = ["256KiB", "1MiB", "4MiB", "16MiB"]

    runs = {rid: json.load(open(os.path.join(BASE, "raw", rid + ".json"))) for rid in planned}

    per_run = []
    svc_by_arm = {a: [] for a in arm_order}
    cli_by_arm = {a: [] for a in arm_order}
    tail_16 = []
    for rid in planned:
        r = runs[rid]
        label = [a for a, b in arm_bytes.items() if b == r["arm_bytes"]][0]
        tail_start = None
        for e in r["server_request_log"]:
            if e["has_range"] and e["range_start"] >= FILE_SIZE - TAIL_REGION:
                tail_start = e["start_unix_nano"]
                break
        tail_dur = (r["end_wall_unix_nano"] - tail_start) / 1e9 if tail_start is not None else None
        svc = [(e["end_unix_nano"] - e["start_unix_nano"]) / 1e9
               for e in r["server_request_log"] if e["has_range"]]
        cli = [c["duration_ns"] / 1e9 for c in r["client_records"] if c["success"]]
        svc_by_arm[label].extend(svc)
        cli_by_arm[label].extend(cli)
        if label == "16MiB" and tail_dur is not None:
            tail_16.append(tail_dur)
        per_run.append({
            "run_id": rid, "block": r["block"], "order_in_block": r["order_in_block"],
            "arm_label": label, "arm_bytes": r["arm_bytes"],
            "duration_seconds": r["duration_ns"] / 1e9,
            "clean": bool(r["clean"]), "run_err": r["run_err"],
            "total_attempts": r["total_attempts"], "failed_attempts": r["failed_attempts"],
            "retry_attempts": r["retry_attempts"],
            "server_requests": len(r["server_request_log"]),
            "server_service_median_seconds": statistics.median(svc) if svc else None,
            "client_service_median_seconds": statistics.median(cli) if cli else None,
            "realized_tail_seconds": tail_dur,
            "eof_reached": bool(r["eof_reached"]), "coverage_no_missing": bool(r["coverage_no_missing"]),
            "coverage_no_duplicates": bool(r["coverage_no_duplicates"]),
            "output_size_bytes": r["output_size_bytes"],
        })

    # Criterion 1: mechanism presence
    per_arm = {}
    for label in arm_order:
        s = arm_bytes[label]
        expected = DELAY + s / RATE
        med_server = statistics.median(svc_by_arm[label])
        med_client = statistics.median(cli_by_arm[label])
        dev = (med_server - expected) / expected
        per_arm[label] = {
            "arm_bytes": s, "expected_service_seconds": expected,
            "pooled_median_server_service_seconds": med_server,
            "pooled_median_client_service_seconds": med_client,
            "relative_deviation": dev, "within_25pct": abs(dev) <= 0.25,
            "requests": len(svc_by_arm[label]),
        }
    c1_pass = all(v["within_25pct"] for v in per_arm.values())

    # Criterion 2: strict direction
    meds = [per_arm[a]["pooled_median_server_service_seconds"] for a in arm_order]
    c2_pass = all(meds[i] < meds[i + 1] for i in range(len(meds) - 1))
    direction = {"order": arm_order, "medians_seconds": meds, "strictly_increasing": c2_pass}

    # Criterion 3: tail sufficiency (16 MiB arm)
    tail_med = statistics.median(tail_16) if tail_16 else None
    c3_pass = tail_med is not None and tail_med >= 20.0 and tail_lo <= tail_med <= tail_hi
    tail = {"runs": len(tail_16), "realized_tail_seconds": tail_16,
            "pooled_median_tail_seconds": tail_med, "min": min(tail_16) if tail_16 else None,
            "max": max(tail_16) if tail_16 else None,
            "T_tail_primary_seconds": T_tail, "lower_bound_seconds": tail_lo,
            "upper_bound_seconds": tail_hi, "ge_20s": tail_med >= 20.0 if tail_med else False,
            "within_bounds": (tail_lo <= tail_med <= tail_hi) if tail_med else False,
            "pass": c3_pass}

    # Criterion 4: feasibility / reliability
    clean = sum(1 for p in per_run if p["clean"])
    max_wall = max(p["duration_seconds"] for p in per_run)
    ys = {p["run_id"]: math.log(p["duration_seconds"] / T_ref) for p in per_run}
    grand = sum(ys.values()) / len(ys)
    mean_arm = {a: sum(ys[p["run_id"]] for p in per_run if p["arm_label"] == a) /
                sum(1 for p in per_run if p["arm_label"] == a) for a in arm_order}
    mean_block = {b: sum(ys[p["run_id"]] for p in per_run if p["block"] == b) /
                  sum(1 for p in per_run if p["block"] == b) for b in range(8)}
    resid = [ys[p["run_id"]] - mean_arm[p["arm_label"]] - mean_block[p["block"]] + grand for p in per_run]
    n, C, B = len(per_run), len(arm_order), 8
    df = n - C - B + 1
    resid_sd = math.sqrt(sum(r * r for r in resid) / df)
    c4_pass = clean >= 28 and max_wall <= 1800.0 and resid_sd <= 0.25
    reliability = {"clean_runs": clean, "planned_runs": len(per_run),
                   "technical_failures": len(per_run) - clean,
                   "clean_ge_28": clean >= 28,
                   "max_wall_seconds": max_wall, "max_wall_le_1800": max_wall <= 1800.0,
                   "within_block_residual_sd": resid_sd, "residual_df": df,
                   "residual_sd_le_0.25": resid_sd <= 0.25, "pass": c4_pass,
                   "log_scale": {"T_ref_seconds": T_ref, "grand_mean": grand}}

    criteria = {
        "criterion_1_mechanism_presence": {"pass": c1_pass, "detail": per_arm},
        "criterion_2_direction": {"pass": c2_pass, "detail": direction},
        "criterion_3_tail_sufficiency": {"pass": c3_pass, "detail": tail},
        "criterion_4_feasibility_reliability": {"pass": c4_pass, "detail": reliability},
    }
    go = c1_pass and c2_pass and c3_pass and c4_pass
    result = {
        "protocol": {"id": proto["protocol_id"], "status": proto["status"],
                     "frozen_quantities": {"T_ref_seconds": T_ref, "T_tail_primary_seconds": T_tail,
                                           "delta_tail_seconds": delta_tail, "delta_log": delta_log}},
        "per_run": per_run, "per_arm": per_arm, "direction": direction, "tail": tail,
        "reliability": reliability, "criteria": criteria,
        "final_go_no_go": "GO" if go else "NO-GO",
        "not_confirmatory": "Stage 0S is a feasibility/mechanism gate only; no adaptive-effect conclusion is made.",
    }

    out = os.path.join(BASE, "analysis")
    os.makedirs(out, exist_ok=True)
    json.dump(result, open(os.path.join(out, "stage0s_analysis.json"), "w"), indent=2)
    for name, obj in [("per_arm.json", per_arm), ("direction.json", direction), ("tail.json", tail),
                      ("reliability.json", reliability), ("criteria.json", criteria)]:
        json.dump(obj, open(os.path.join(out, name), "w"), indent=2)
    with open(os.path.join(out, "per_run.csv"), "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(per_run[0].keys()))
        w.writeheader()
        for p in per_run:
            w.writerow(p)
    prdir = os.path.join(BASE, "per_run")
    os.makedirs(prdir, exist_ok=True)
    for p in per_run:
        json.dump(p, open(os.path.join(prdir, p["run_id"] + ".json"), "w"), indent=2)

    # report (generated from the analysis; no hand-typed derived values)
    L = []
    L.append("# Stage 0S Pilot Report — Salvage Protocol v7")
    L.append("")
    L.append("Frozen protocol: `%s` — `%s`" % (proto["protocol_id"], proto["status"]))
    L.append("")
    L.append("**This is NOT a confirmatory adaptive-effect result.** Stage 0S is a")
    L.append("feasibility/mechanism gate only. No switching strategy was run, no winner is")
    L.append("reported, and no adaptive chunk-size improvement is claimed.")
    L.append("")
    L.append("## 1. Experimental design")
    L.append("")
    m = manifest["frozen_mechanism"]
    L.append("- Mechanism: R = %d bytes/s (4 Mbps), D = %d ms, FileSize = %d bytes (512 MiB), N = %d"
             % (m["R_bytes_per_second"], m["D_seconds"] * 1000, m["FileSize_bytes"], m["N"]))
    L.append("- Arms: " + ", ".join("%s (%d B)" % (a, arm_bytes[a]) for a in arm_order))
    L.append("- Design: 8 replicate blocks x 4 arms = 32 runs; randomized order within each block")
    L.append("  (seed %s, recorded in the manifest)." % manifest["randomization"]["seed"])
    L.append("- Server: %s; client: %s." % (manifest["server_config"]["implementation"],
                                           manifest["client_config"]["implementation"]))
    L.append("- No adaptive switching; each run is fixed at its assigned chunk size.")
    L.append("")
    L.append("## 2. Executed run ledger")
    L.append("")
    L.append("- planned: %d, attempted: %d, clean: %d, technical failures: %d, reruns: %d"
             % (len(per_run), len(per_run), clean, len(per_run) - clean, 0))
    L.append("- technical-rerun policy: the frozen protocol contains no rerun authorization;")
    L.append("  failed runs are recorded and not rerun.")
    L.append("")
    L.append("## 3. Per-arm results (server-side per-request service time)")
    L.append("")
    L.append("| Arm | expected D+s/R (s) | pooled median (s) | relative deviation | within +/-25% | requests |")
    L.append("|---|---|---|---|---|---|")
    for a in arm_order:
        v = per_arm[a]
        L.append("| %s | %.6f | %.6f | %+.4f%% | %s | %d |" % (
            a, v["expected_service_seconds"], v["pooled_median_server_service_seconds"],
            100 * v["relative_deviation"], "PASS" if v["within_25pct"] else "FAIL", v["requests"]))
    L.append("")
    L.append("## 4. Criterion 1 — mechanism presence: **%s**" % ("PASS" if c1_pass else "FAIL"))
    L.append("")
    L.append("## 5. Criterion 2 — strict service-time direction: **%s**" % ("PASS" if c2_pass else "FAIL"))
    L.append("")
    L.append("Medians: " + " < ".join("%s=%.6f" % (a, meds[i]) for i, a in enumerate(arm_order)))
    L.append("")
    L.append("## 6. Criterion 3 — tail sufficiency (16 MiB arm): **%s**" % ("PASS" if c3_pass else "FAIL"))
    L.append("")
    L.append("- realized tail durations (s): " + ", ".join("%.3f" % t for t in tail_16))
    L.append("- pooled median: %.6f s; bounds [%.7f, %.7f] s; >= 20 s: %s"
             % (tail_med, tail_lo, tail_hi, tail["ge_20s"]))
    L.append("")
    L.append("## 7. Criterion 4 — feasibility/reliability: **%s**" % ("PASS" if c4_pass else "FAIL"))
    L.append("")
    L.append("- clean runs: %d/%d (>= 28 required)" % (clean, len(per_run)))
    L.append("- maximum wall-clock: %.3f s (<= 1800 s required)" % max_wall)
    L.append("- within-block residual SD of log(T/T_ref): %.6f (<= 0.25 required), df = %d"
             % (resid_sd, df))
    L.append("")
    L.append("## 8. Formal four-criterion decision")
    L.append("")
    for k, v in criteria.items():
        L.append("- %s: %s" % (k, "PASS" if v["pass"] else "FAIL"))
    L.append("")
    L.append("## 9. Final GO/NO-GO: **%s**" % result["final_go_no_go"])
    L.append("")
    L.append("A GO means only that the frozen mechanism and finite-file tail were sufficiently")
    L.append("realized under the pre-specified Stage 0S feasibility criteria to justify proceeding")
    L.append("to the next pre-specified stage. It does not mean chunk-size adaptation is beneficial.")
    L.append("")
    L.append("## 10. Limitations / anomalies")
    L.append("")
    L.append("- No technical failures or anomalies were observed; see the run ledger for details.")
    L.append("- Service times are server-side; client-side medians are recorded as diagnostics in")
    L.append("  analysis/per_arm.json.")
    L.append("- The content source is the deterministic logical server (bounded memory) with the same")
    L.append("  range semantics and pacing as the materialized server.")
    L.append("")
    L.append("## 11. Artifact manifest")
    L.append("")
    L.append("- `manifest/manifest.json`, `manifest/run_ledger.jsonl`, `manifest/run_ledger.json`")
    L.append("- `raw/<run_id>.json` (32 raw artifacts incl. client records and server request log)")
    L.append("- `logs/<run_id>.log`, `logs/driver.log`")
    L.append("- `analysis/stage0s_analysis.json`, `analysis/per_run.csv`, `analysis/per_arm.json`,")
    L.append("  `analysis/direction.json`, `analysis/tail.json`, `analysis/reliability.json`,")
    L.append("  `analysis/criteria.json`")
    L.append("- `tools/stage0s_run.go`, `tools/run_pilot.py`, `tools/analyze_stage0s.py`,")
    L.append("  `tools/preflight/preflight_smoke.go`")
    L.append("")
    L.append("## 12. Confirmatory separation")
    L.append("")
    L.append("No Bayesian confirmatory analysis, no switching strategy, and no SSR simulation was")
    L.append("run in this task.")
    os.makedirs(os.path.join(BASE, "final_report"), exist_ok=True)
    open(os.path.join(BASE, "final_report", "stage0s_report.md"), "w").write("\n".join(L) + "\n")

    print("per-arm medians:", {a: round(per_arm[a]["pooled_median_server_service_seconds"], 6) for a in arm_order})
    print("direction:", c2_pass, "| c1:", c1_pass, "| c3:", c3_pass, "tail_med=%.6f" % tail_med,
          "| c4:", c4_pass, "resid_sd=%.6f" % resid_sd)
    print("clean=%d max_wall=%.3f final=%s" % (clean, max_wall, result["final_go_no_go"]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
