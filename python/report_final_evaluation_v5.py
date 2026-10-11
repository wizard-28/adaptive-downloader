"""Human-readable report for the final v5-vs-aria2-v2 head-to-head.

Reads only the frozen artifacts written by cmd/final-evaluation-v5 and the
reused python/analyze_final_evaluation.py, and writes report.txt,
per_condition_table.txt and a refreshed artifact_hashes.json. No new
statistics are computed here; every number is copied from those artifacts.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os

CONDITIONS = ["U1", "U2", "U3", "U4", "U5", "U6", "U7", "U8"]


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
    ap.add_argument("--dir", default="testdata/final_evaluation_v5")
    args = ap.parse_args()
    base = os.path.abspath(args.dir)

    manifest = load(base, "manifest_v5.json")
    validity = load(base, "validity_report.json")
    stats = load(base, "statistical_analysis.json")
    asym = load(base, "asymmetry_report.json")
    runs = load(base, "raw_results_all.json")["results"]

    v5 = [r for r in runs if r["system"] == "dqn"]
    aria = [r for r in runs if r["system"] == "aria2"]
    agg = stats["aggregate"]
    per = stats["per_condition"]
    eff = stats["effect_distribution"]

    lines = []
    w = lines.append
    w("FINAL HEAD-TO-HEAD: DQN Candidate v5 vs FINAL ARIA2 BASELINE CONFIG v2 (candidate F)")
    w("Controlled server, 16 MiB file, 1 MiB fixed chunk, wall-clock unstable profiles.")
    w("NO real-network generalization; NO capability-matched claim; structural asymmetry preserved below.")
    w("")
    w("1. FROZEN ARTIFACTS")
    w(f"   v5 model SHA-256          : {manifest['v5_sha256']}")
    w(f"   aria2 baseline metadata   : {manifest['aria2_baseline_sha256']}")
    w(f"   aria2 version             : {manifest['aria2_version']}")
    w(f"   aria2 configuration       : {manifest['aria2_command']}")
    w(f"   matrix SHA-256            : {manifest['matrix_sha256']}")
    w("")
    w("2. TEST MATRIX")
    w(f"   source methodology        : {manifest['source_methodology']}")
    w(f"   conditions                : {', '.join(c['id'] for c in manifest['conditions'])}")
    w(f"   pairs per condition       : {manifest['pairs_per_condition']}")
    w(f"   matched pairs             : {manifest['pairs']}")
    w(f"   total downloads           : {manifest['total_downloads']}")
    w(f"   file / chunk              : {manifest['file_size']} / {manifest['chunk_size']}")
    w(f"   v5 concurrency bounds     : {manifest['v5_concurrency_bounds']}")
    w(f"   aria2 max connections     : {manifest['aria2_max_connections']}")
    w(f"   order seed                : {manifest['schedule_seed']}")
    w("")
    w("3. VALIDITY")
    w(
        f"   valid runs {validity['valid_runs']}/{validity['recorded_runs']}, "
        f"invalid {validity['invalid_runs']}; valid pairs {validity['valid_pairs']}/{validity['expected_pairs']}, "
        f"excluded {validity['excluded_pairs']}"
    )
    w(f"   rule: {validity['rule']}")
    if validity["exclusions"]:
        for e in validity["exclusions"]:
            w(f"   EXCLUDED {e['run_id']}: {e['notes']} err={e['err']}")
    w("")
    w("4. PER-CONDITION MATCHED-PAIR RESULTS (ms; analyzer keys 'dqn' = Candidate v5)")
    w(f"   {'cond':<4} {'n':>3} {'v5 mean':>9} {'aria2 mean':>11} {'v5 med':>9} {'aria2 med':>10} {'ratio':>7} {'CI95':>16} {'p_holm':>8}")
    for c in CONDITIONS:
        p = per[c]
        ci = p["ci95_log_ratio"]
        import math

        w(
            f"   {c:<4} {p['n']:>3} {p['dqn_mean_ms']:>9.1f} {p['aria2_mean_ms']:>11.1f} "
            f"{p['dqn_median_ms']:>9.1f} {p['aria2_median_ms']:>10.1f} {p['geometric_ratio']:>7.3f} "
            f"[{math.exp(ci[0]):.3f},{math.exp(ci[1]):.3f}]".rjust(16)
            + f" {p['p_holm']:>8.4g}"
        )
    w("")
    w("5. POOLED MATCHED-PAIR ANALYSIS (primary unit = pair; effect = log(v5/aria2))")
    w(f"   pairs                     : {agg['pairs']}")
    w(f"   v5 mean / aria2 mean      : {agg['dqn_mean_ms']:.1f} ms / {agg['aria2_mean_ms']:.1f} ms")
    w(f"   geometric ratio           : {agg['geometric_ratio']:.4f} (ratio 95% CI [{agg['ratio_ci95'][0]:.4f}, {agg['ratio_ci95'][1]:.4f}])")
    w(f"   relative reduction        : {100 * agg['relative_reduction']:.2f}% (negative = v5 slower)")
    w(f"   paired permutation p      : {agg['p_one_sided']:.6g} (one-sided, v5 faster)")
    w(f"   effect distribution       : v5 faster {eff['dqn_faster_pairs']}/{agg['pairs']} ({eff['dqn_faster_pct']:.1f}%), "
      f"aria2 faster {eff['aria2_faster_pairs']}, ties {eff['ties']}; conditions v5 faster {eff['conditions_dqn_faster']}/8")
    w("")
    w("6. PRE-SPECIFIED CLAIM CRITERIA (reused methodology, locked before the run)")
    for k, v in stats["claim_criteria"].items():
        w(f"   {k}: {v}")
    w(f"   claim_supported           : {stats['claim_supported']}")
    w("")
    w("7. V5 OBSERVED LIVE BEHAVIOR (authoritative controller state)")
    maxcs = [r["max_concurrency"] for r in v5]
    twcs = [r["time_weighted_concurrency"] for r in v5]
    changes = [r["effective_concurrency_changes"] for r in v5]
    finalc = {c: sum(1 for r in v5 if r["final_concurrency"] == c) for c in (1, 2, 3, 4)}
    constant = sum(1 for r in v5 if r["constant_run"]) / len(v5)
    ramp = sum(1 for r in v5 if r["ramp_to_max"]) / len(v5)
    w(f"   max concurrency           : always {max(maxcs)}; time-weighted mean {sum(twcs)/len(twcs):.3f}")
    w(f"   effective changes/run     : mean {sum(changes)/len(changes):.2f} (min {min(changes)}, max {max(changes)})")
    w(f"   final concurrency counts  : {finalc}")
    w(f"   constant-run fraction     : {constant:.3f}; ramp-to-max fraction {ramp:.3f}")
    w("   transition reactions (mean latency from transition to first effective change):")
    for c in CONDITIONS:
        lat = [r["reaction_latency_ms"] for r in v5 if r["condition"] == c and r.get("reaction_latency_ms", -1) >= 0]
        if lat:
            w(f"     {c}: {sum(lat)/len(lat):.0f} ms over {len(lat)} runs")
    w("")
    w("8. ARIA2 OBSERVED REQUEST BEHAVIOR")
    inflight = [r["max_in_flight"] for r in aria]
    ranges = [r["range_count"] for r in aria]
    nonrange = [r["non_range_requests"] for r in aria]
    w(f"   max in-flight             : mean {sum(inflight)/len(inflight):.2f}, max {max(inflight)} "
      f"({sum(1 for x in inflight if x == 16)}/{len(aria)} runs reached 16)")
    w(f"   range requests/run        : mean {sum(ranges)/len(ranges):.2f}, range size 1 MiB")
    w(f"   non-range requests/run    : {sum(nonrange)/len(nonrange):.2f} (initial probe; aborted)")
    w(f"   retries                   : {sum(r['retries'] for r in aria)}; failed server-side requests {sum(r['failed_requests'] for r in aria)}")
    w("")
    w("9. STRUCTURAL ASYMMETRY (MANDATORY CONTEXT)")
    w(f"   A. v5 maximum concurrency                    : {asym['v5_max_concurrency']}")
    w(f"   B. aria2-F maximum configured connections    : {asym['aria2_max_configured_connections']}")
    w(f"   C. selection reason                          : {asym['aria2_baseline_selection_reason']}")
    w(f"   D. server pacing                             : {asym['server_pacing']}")
    w(f"   consequence                                  : {asym['consequence']}")
    w(f"   observed aria2 max in-flight (mean/max)      : {asym['observed_aria2_max_in_flight_mean']:.2f} / {asym['observed_aria2_max_in_flight_max']:.0f}")
    w(f"   observed v5 time-weighted concurrency (mean) : {asym['observed_v5_time_weighted_mean']:.3f}")
    w("")
    w("10. INTERPRETATION")
    w("   Descriptive: v5 was slower than aria2-F in all 8 tested conditions; the pooled")
    w("   geometric ratio was %.2f with a 95%% CI that excludes 1.0, and v5 was faster in" % agg["geometric_ratio"])
    w("   %d/%d pairs and %d/8 conditions." % (eff["dqn_faster_pairs"], agg["pairs"], eff["conditions_dqn_faster"]))
    w("   Statistical: the pre-specified claim criteria were all false; no v5-faster claim is supported.")
    w("   Structural: v5 can use at most 4 concurrent requests while aria2-F uses up to 16;")
    w("   the controlled server paces responses independently, so aria2-F can exploit up to")
    w("   16 independently paced responses. This is a comparison of two complete frozen")
    w("   configurations, NOT a capability-matched comparison. The ratio must not be read as")
    w("   an algorithmic effect isolated from this asymmetry.")
    w("   Supported conclusion (exactly): under the tested controlled conditions at 16 MiB with")
    w("   the frozen configurations and methodology, v5 did not outperform aria2-F and was")
    w("   slower; no universal, consumer-network, optimality, convergence, or generalization")
    w("   claim is made.")
    w("")
    w("11. V5 ADAPTATION CONTEXT (established before this comparison; unchanged)")
    w("   v5 passed the live technical/reaction gates and was non-constant, but adaptation")
    w("   was shallow: it generally ramps to C3-C4, never holds low concurrency, and does not")
    w("   reliably track the simulator's static optimum. The head-to-head evaluated the")
    w("   frozen candidate; nothing was retrained or tuned based on these results.")
    w("")
    w("12. ARTIFACTS")
    for name in sorted(os.listdir(base)):
        w(f"   {name}")
    w("")

    with open(os.path.join(base, "report.txt"), "w") as f:
        f.write("\n".join(lines) + "\n")

    table = []
    table.append("condition\tpairs\tv5_mean_ms\taria2_mean_ms\tv5_median_ms\taria2_median_ms\tgeometric_ratio\tratio_ci95\tp_holm")
    import math

    for c in CONDITIONS:
        p = per[c]
        ci = p["ci95_log_ratio"]
        table.append(
            f"{c}\t{p['n']}\t{p['dqn_mean_ms']:.1f}\t{p['aria2_mean_ms']:.1f}\t"
            f"{p['dqn_median_ms']:.1f}\t{p['aria2_median_ms']:.1f}\t{p['geometric_ratio']:.4f}\t"
            f"[{math.exp(ci[0]):.4f},{math.exp(ci[1]):.4f}]\t{p['p_holm']:.6g}"
        )
    with open(os.path.join(base, "per_condition_table.txt"), "w") as f:
        f.write("\n".join(table) + "\n")

    hashes = {}
    for name in sorted(os.listdir(base)):
        if name == "artifact_hashes.json":
            continue
        path = os.path.join(base, name)
        if os.path.isfile(path):
            hashes[name] = sha256_file(path)
    with open(os.path.join(base, "artifact_hashes.json"), "w") as f:
        json.dump(hashes, f, indent=2, sort_keys=True)
        f.write("\n")

    print(f"wrote report.txt, per_condition_table.txt, artifact_hashes.json ({len(hashes)} files) to {base}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
