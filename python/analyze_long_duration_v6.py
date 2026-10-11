"""Long-duration diagnostic for the frozen v6 candidate (no training, no new
statistics). Aggregates the per-size matched-pair outputs produced by
cmd/final-evaluation-v6 (statistics from the established
python/analyze_final_evaluation.py) and adds the duration/startup analysis.

Primary metric: geometric completion-time ratio v6/aria2 as a function of size.
Pre-defined interpretation (PROJECT_CONTEXT, before results):
  PROMISING         : ratio moves toward 1.0 as size increases and startup cost
                      becomes a small fraction of runtime.
  STRONGLY PROMISING: approaches/crosses 1.0 at the long duration.
  NOT SUPPORTED     : ratio stays roughly constant or moves farther above 1.0.
  MIXED             : long-duration benefit only in particular trajectories.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import statistics as st


def load(path):
    with open(path) as f:
        return json.load(f)


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", default="testdata/diagnostics/dqn_long_duration_v6")
    ap.add_argument("--sizes", default="16,128,512")
    ap.add_argument("--conditions", default="A2,D2,E2")
    args = ap.parse_args()
    base = os.path.abspath(args.dir)
    sizes = [int(x) for x in args.sizes.split(",") if x]
    conditions = [c for c in args.conditions.split(",") if c]

    per_size = {}
    pair_rows = []
    for mib in sizes:
        d = os.path.join(base, f"size_{mib}MiB")
        manifest = load(os.path.join(d, "manifest_v6.json"))
        stats = load(os.path.join(d, "statistical_analysis.json"))
        raw = load(os.path.join(d, "raw_results_all.json"))["results"]
        valid = [r for r in raw if r["valid"]]
        by_pair = {}
        for r in valid:
            by_pair.setdefault(r["pair_id"], {})[r["system"]] = r
        startup_list, steady_ratio_list = [], []
        for pid, m in sorted(by_pair.items()):
            if "dqn" not in m or "aria2" not in m:
                continue
            v, a = m["dqn"], m["aria2"]
            actions = v.get("actions") or []
            first = next((x["time_since_start_ms"] for x in actions if x.get("changed")), -1)
            startup_s = first / 1000.0 if first >= 0 else None
            v_s, a_s = v["duration_ms"] / 1000.0, a["duration_ms"] / 1000.0
            sf = (startup_s / v_s) if startup_s is not None and v_s > 0 else None
            if sf is not None:
                startup_list.append(sf)
            steady_ratio_list.append(((v["duration_ms"] - (startup_s * 1000.0 if startup_s else 0.0)) / a["duration_ms"])
                                     if a["duration_ms"] > 0 else float("nan"))
            pair_rows.append({
                "size_mib": mib, "trajectory": v["condition"], "pair_id": pid,
                "v6_ms": v["duration_ms"], "aria2_ms": a["duration_ms"],
                "ratio": v["duration_ms"] / a["duration_ms"],
                "diff_ms": v["duration_ms"] - a["duration_ms"],
                "v6_startup_to_first_change_ms": first if first >= 0 else None,
                "v6_startup_fraction": sf,
                "v6_effective_changes": v.get("effective_concurrency_changes"),
                "v6_final_concurrency": v.get("final_concurrency"),
                "v6_time_weighted_concurrency": v.get("time_weighted_concurrency"),
                "v6_phase_mean_concurrency": v.get("phase_mean_concurrency"),
                "aria2_max_in_flight": a.get("max_in_flight"),
                "aria2_range_count": a.get("range_count"),
                "aria2_non_range_requests": a.get("non_range_requests"),
            })
        per_cond = {}
        for c in conditions:
            agg = stats["per_condition"][c]
            ci = agg["ci95_log_ratio"]
            pr = [x for x in pair_rows if x["size_mib"] == mib and x["trajectory"] == c]
            per_cond[c] = {
                "n": agg["n"],
                "v6_mean_ms": agg["dqn_mean_ms"],
                "aria2_mean_ms": agg["aria2_mean_ms"],
                "ratio": agg["geometric_ratio"],
                "ratio_ci95": [math.exp(ci[0]), math.exp(ci[1])],
                "p_one_sided": agg["p_one_sided"],
                "p_holm": agg["p_holm"],
                "mean_startup_fraction": st.mean([x["v6_startup_fraction"] for x in pr if x["v6_startup_fraction"] is not None]) if pr else None,
                "mean_v6_steady_over_aria2": st.mean([x for x in steady_ratio_list[:0]]) if False else None,
                "v6_steady_over_aria2": st.mean([
                    (x["v6_ms"] - (x["v6_startup_to_first_change_ms"] or 0.0)) / x["aria2_ms"] for x in pr
                ]) if pr else None,
            }
        agg = stats["aggregate"]
        per_size[str(mib)] = {
            "file_size_bytes": manifest["file_size"],
            "pairs": agg["pairs"],
            "v6_mean_ms": agg["dqn_mean_ms"],
            "aria2_mean_ms": agg["aria2_mean_ms"],
            "ratio": agg["geometric_ratio"],
            "ratio_ci95": agg["ratio_ci95"],
            "p_one_sided": agg["p_one_sided"],
            "claim_supported": stats["claim_supported"],
            "mean_startup_fraction": st.mean(startup_list) if startup_list else None,
            "overall_v6_steady_over_aria2": st.mean(steady_ratio_list) if steady_ratio_list else None,
            "per_trajectory": per_cond,
        }

    # trend (observed directly; no regression / trend claim)
    trend = {"overall_ratio_by_size": {s: per_size[s]["ratio"] for s in map(str, sizes)}}
    for c in conditions:
        trend[f"{c}_ratio_by_size"] = {s: per_size[s]["per_trajectory"][c]["ratio"] for s in map(str, sizes)}
    overall = [per_size[str(s)]["ratio"] for s in sizes]
    decreasing = all(overall[i + 1] <= overall[i] + 0.02 for i in range(len(overall) - 1))
    long_ratio = overall[-1]
    short_ratio = overall[0]
    if long_ratio <= 1.0:
        verdict = "STRONGLY_PROMISING"
    elif decreasing and long_ratio < short_ratio:
        verdict = "PROMISING"
    else:
        verdict = "NOT_SUPPORTED"
    # MIXED when only some trajectories cross/approach 1.0 and the overall
    # ratios are not monotonically improving.
    per_traj_beats = any(
        per_size[str(sz)]["per_trajectory"][c]["ratio"] <= 1.0 for sz in sizes for c in conditions
    )
    if verdict != "STRONGLY_PROMISING" and per_traj_beats and not decreasing:
        verdict = "MIXED"
    trend["observed"] = {
        "overall_ratios": {str(s): overall[i] for i, s in enumerate(sizes)},
        "short_ratio": short_ratio, "long_ratio": long_ratio,
        "monotone_non_increasing_within_0.02": decreasing,
        "verdict": verdict,
    }

    summary = {
        "label": "Frozen v6 long-duration diagnostic (diagnostic only; does NOT modify the v6 final result)",
        "v6_sha256": load(os.path.join(base, "size_16MiB", "manifest_v6.json"))["v6_sha256"],
        "aria2_baseline_sha256": load(os.path.join(base, "size_16MiB", "manifest_v6.json"))["aria2_baseline_sha256"],
        "sizes_mib": sizes, "trajectories": conditions, "pairs_per_cell": 5,
        "phase_seconds": 2.0,
        "per_size": per_size, "trend": trend,
    }
    with open(os.path.join(base, "long_duration_summary.json"), "w") as f:
        json.dump(summary, f, indent=2, sort_keys=True)
        f.write("\n")
    with open(os.path.join(base, "per_pair_v6_vs_aria2.json"), "w") as f:
        json.dump({"pairs": pair_rows}, f, indent=2, sort_keys=True)
        f.write("\n")

    lines = []
    w = lines.append
    w("Long-duration diagnostic: frozen v6 vs aria2-F (matched pairs; diagnostic only)")
    w("This does NOT modify the v6 final result (PROJECT_CONTEXT 22.35).")
    w("")
    w(f"v6 SHA-256    : {summary['v6_sha256']}")
    w(f"aria2 SHA-256 : {summary['aria2_baseline_sha256']}")
    w(f"sizes         : {sizes} MiB; trajectories {conditions}; 5 pairs per size x trajectory; 2 s phases (unchanged)")
    w("")
    w(f"{'Size(MiB)':>9} {'Traj':>5} {'v6 ms':>9} {'aria2 ms':>9} {'Ratio':>7} {'CI95':>15} {'StartupFrac':>11} {'SteadyRatio':>11}")
    for s in sizes:
        for c in conditions:
            p = per_size[str(s)]["per_trajectory"][c]
            w(f"{s:>9} {c:>5} {p['v6_mean_ms']:>9.1f} {p['aria2_mean_ms']:>9.1f} {p['ratio']:>7.3f} "
              f"[{p['ratio_ci95'][0]:.3f},{p['ratio_ci95'][1]:.3f}] "
              f"{(p['mean_startup_fraction'] if p['mean_startup_fraction'] is not None else float('nan')):>11.3f} "
              f"{(p['v6_steady_over_aria2'] if p['v6_steady_over_aria2'] is not None else float('nan')):>11.3f}")
        p = per_size[str(s)]
        w(f"{s:>9} {'ALL':>5} {p['v6_mean_ms']:>9.1f} {p['aria2_mean_ms']:>9.1f} {p['ratio']:>7.3f} "
          f"[{p['ratio_ci95'][0]:.3f},{p['ratio_ci95'][1]:.3f}] {p['mean_startup_fraction']:>11.3f} {p['overall_v6_steady_over_aria2']:>11.3f}")
        w("")
    w("Interpretation (pre-defined): PROMISING = ratio moves toward 1.0 with size and startup")
    w("becomes a small fraction; STRONGLY PROMISING = approaches/crosses 1.0 at long duration;")
    w("NOT SUPPORTED = ratio roughly constant or farther above 1.0; MIXED = only some trajectories.")
    w(f"Observed overall ratios by size: {trend['observed']['overall_ratios']}")
    w(f"Verdict: {trend['observed']['verdict']}")
    w("Caveat: the pre-registered trajectories use absolute 2 s phases, so the disturbed fraction")
    w("shrinks as the file grows (measured: phases 1-2 are 2 s each at every size; the recovered")
    w("phase dominates long transfers). The diagnostic therefore measures startup/adaptation")
    w("amortization under a fixed disturbance window, not a longer disturbance.")
    with open(os.path.join(base, "long_duration_summary.txt"), "w") as f:
        f.write("\n".join(lines) + "\n")
    with open(os.path.join(base, "report.txt"), "w") as f:
        f.write("\n".join(lines) + "\n")

    hashes = {}
    for root, _, files in os.walk(base):
        for name in files:
            if name == "artifact_hashes.json":
                continue
            p = os.path.join(root, name)
            hashes[os.path.relpath(p, base)] = sha256_file(p)
    with open(os.path.join(base, "artifact_hashes.json"), "w") as f:
        json.dump(hashes, f, indent=2, sort_keys=True)
        f.write("\n")
    print(f"overall ratios by size: {trend['observed']['overall_ratios']}; verdict {trend['observed']['verdict']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
