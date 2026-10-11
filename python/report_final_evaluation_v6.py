"""Human-readable report for the final v6-vs-aria2-F head-to-head.

Reads only the frozen artifacts written by cmd/final-evaluation-v6 and
python/analyze_final_evaluation.py; writes report.txt, per_condition_table.txt
and a refreshed artifact_hashes.json. No new statistics.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os


def load(base, name):
    with open(os.path.join(base, name)) as f:
        return json.load(f)


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", default="testdata/final_evaluation_v6")
    ap.add_argument("--conditions", default="A2,B2,C2,D2,E2")
    args = ap.parse_args()
    base = os.path.abspath(args.dir)
    conditions = [c for c in args.conditions.split(",") if c]

    manifest = load(base, "manifest_v6.json")
    validity = load(base, "validity_report.json")
    stats = load(base, "statistical_analysis.json")
    runs = load(base, "raw_results_all.json")["results"]
    agg = stats["aggregate"]
    per = stats["per_condition"]
    eff = stats["effect_distribution"]

    v6 = [r for r in runs if r["system"] == "dqn"]
    ar = [r for r in runs if r["system"] == "aria2"]

    lines = []
    w = lines.append
    w("FINAL PRE-REGISTERED HEAD-TO-HEAD: DQN Candidate v6 vs frozen aria2-F")
    w("Controlled server; pre-registered v6 trajectories A-E (wall-clock phases); matched pairs.")
    w("NO real-network/mobile generalization; NO claim of general superiority over aria2.")
    w("")
    w("1. FROZEN ARTIFACTS")
    w(f"   v6 model SHA-256        : {manifest['v6_sha256']}")
    w(f"   aria2 baseline SHA-256  : {manifest['aria2_baseline_sha256']}")
    w(f"   aria2 version/config    : {manifest['aria2_version']} | {manifest['aria2_command']}")
    w(f"   matrix SHA-256          : {manifest['matrix_sha256']}")
    w("")
    w("2. MATRIX")
    w(f"   trajectories : {', '.join(c['id'] for c in manifest['conditions'])}")
    w(f"   pairs/traj   : {manifest['pairs_per_condition']}; matched pairs {manifest['pairs']}; downloads {manifest['total_downloads']}")
    w(f"   file/chunk   : {manifest['file_size']} / {manifest['chunk_size']}; v6 bounds {manifest['v6_concurrency_bounds']}; aria2 max connections 16")
    w(f"   order seed   : {manifest['schedule_seed']}")
    w("")
    w("3. VALIDITY")
    w(f"   valid runs {validity['valid_runs']}/{validity['recorded_runs']}, invalid {validity['invalid_runs']}; "
      f"valid pairs {validity['valid_pairs']}/{validity['expected_pairs']}")
    for e in (validity.get("exclusions") or []):
        w(f"   EXCLUDED {e['run_id']} {e['system']}: {e['err']}")
    w("")
    w("4. PER-TRAJECTORY MATCHED-PAIR RESULTS (ms; analyzer key 'dqn' = v6)")
    w(f"   {'traj':<4} {'n':>3} {'v6 mean':>9} {'aria2 mean':>11} {'v6 med':>9} {'aria2 med':>10} {'ratio':>7} {'CI95':>16} {'p_holm':>8}")
    for c in conditions:
        p = per[c]
        ci = p["ci95_log_ratio"]
        w(f"   {c:<4} {p['n']:>3} {p['dqn_mean_ms']:>9.1f} {p['aria2_mean_ms']:>11.1f} "
          f"{p['dqn_median_ms']:>9.1f} {p['aria2_median_ms']:>10.1f} {p['geometric_ratio']:>7.3f} "
          f"[{math.exp(ci[0]):.3f},{math.exp(ci[1]):.3f}] {p['p_holm']:>8.4g}")
    w("")
    w("5. POOLED MATCHED-PAIR ANALYSIS")
    w(f"   pairs {agg['pairs']}; v6 mean {agg['dqn_mean_ms']:.1f} ms; aria2 mean {agg['aria2_mean_ms']:.1f} ms")
    w(f"   geometric ratio {agg['geometric_ratio']:.4f} (95% CI [{agg['ratio_ci95'][0]:.4f},{agg['ratio_ci95'][1]:.4f}])")
    w(f"   relative reduction {100*agg['relative_reduction']:.2f}% (negative = v6 slower)")
    w(f"   paired permutation p {agg['p_one_sided']:.6g}; v6 faster {eff['dqn_faster_pairs']}/{agg['pairs']} pairs "
      f"({eff['dqn_faster_pct']:.1f}%); conditions v6 faster {eff['conditions_dqn_faster']}/{len(conditions)}")
    w(f"   pre-specified claim criteria: {stats['claim_criteria']} -> claim_supported={stats['claim_supported']}")
    w("")
    w("6. V6 BEHAVIOR (authoritative controller state)")
    changes = [r["effective_concurrency_changes"] for r in v6]
    twc = [r["time_weighted_concurrency"] for r in v6]
    constant = sum(1 for r in v6 if r["constant_run"]) / len(v6)
    ramp = sum(1 for r in v6 if r["ramp_to_max"]) / len(v6)
    w(f"   mean changes/run {sum(changes)/len(changes):.2f}; mean time-weighted concurrency {sum(twc)/len(twc):.2f}; "
      f"constant {constant:.3f}; ramp-to-max {ramp:.3f}")
    for c in conditions:
        vc = [r for r in v6 if r["condition"] == c and r["valid"]]
        pm = [r["phase_mean_concurrency"] for r in vc]
        means = [sum(x[i] for x in pm) / len(pm) for i in range(3)]
        lat = [v for r in vc for v in r["transition_reaction_ms"] if v >= 0]
        w(f"   {c}: phase mean C {means[0]:.2f}->{means[1]:.2f}->{means[2]:.2f}; "
          f"reaction latencies {sorted(round(x) for x in lat)[:6]} ms")
    w("")
    w("7. ARIA2 BEHAVIOR")
    inflight = [r["max_in_flight"] for r in ar]
    rng = [r["range_count"] for r in ar]
    w(f"   max in-flight mean {sum(inflight)/len(inflight):.2f} (max {max(inflight)}); "
      f"range requests/run mean {sum(rng)/len(rng):.2f} (1 MiB); non-range probes mean "
      f"{sum(r['non_range_requests'] for r in ar)/len(ar):.2f}; retries {sum(r['retries'] for r in ar)}; "
      f"failed server requests {sum(r['failed_requests'] for r in ar)}")
    w("")
    w("8. EVIDENCE-SUPPORTED CONCLUSION")
    w("   The pooled matched-pair result (ratio %.3f, CI [%.3f,%.3f]) shows no measurable v6 advantage "
      "across the pre-registered unstable trajectories; per-trajectory results are reported above. "
      "v6 did change concurrency with the environment, but the adaptation did not translate into lower "
      "completion time than aria2-F under these controlled conditions. No general-superiority, "
      "mobile-network, or real-world claim is made." % (agg["geometric_ratio"], agg["ratio_ci95"][0], agg["ratio_ci95"][1]))
    w("")
    w("Limitations: controlled synthetic server; five pre-registered trajectories; 30 pairs each; "
      "aria2's 1 MiB minimum split and initial non-range probe; v6 was previously found slower than the "
      "best fixed concurrency on every live trajectory (PROJECT_CONTEXT 22.34).")
    with open(os.path.join(base, "report.txt"), "w") as f:
        f.write("\n".join(lines) + "\n")

    table = ["trajectory\tpairs\tv6_mean_ms\taria2_mean_ms\tv6_median_ms\taria2_median_ms\tgeometric_ratio\tratio_ci95\tp_holm"]
    for c in conditions:
        p = per[c]
        ci = p["ci95_log_ratio"]
        table.append(f"{c}\t{p['n']}\t{p['dqn_mean_ms']:.1f}\t{p['aria2_mean_ms']:.1f}\t{p['dqn_median_ms']:.1f}\t"
                     f"{p['aria2_median_ms']:.1f}\t{p['geometric_ratio']:.4f}\t[{math.exp(ci[0]):.4f},{math.exp(ci[1]):.4f}]\t{p['p_holm']:.6g}")
    with open(os.path.join(base, "per_condition_table.txt"), "w") as f:
        f.write("\n".join(table) + "\n")

    hashes = {}
    for name in sorted(os.listdir(base)):
        if name == "artifact_hashes.json":
            continue
        p = os.path.join(base, name)
        if os.path.isfile(p):
            hashes[name] = sha256_file(p)
    with open(os.path.join(base, "artifact_hashes.json"), "w") as f:
        json.dump(hashes, f, indent=2, sort_keys=True)
        f.write("\n")
    print(f"wrote report.txt, per_condition_table.txt, artifact_hashes.json ({len(hashes)} files)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
