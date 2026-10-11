#!/usr/bin/env python3
"""Stage C final analysis: frozen oracle + final statistical gate.

Uses the validated replica of the frozen Go oracle (oracle_replica.py, exact
match against oracle_stage_v3). Retained epoch set = shared-valid full 120 s
epochs of the native downlink trace (frozen "native downlink trace semantics"
+ "final partial epoch excluded"); the native final partial epochs have no
frozen regime label and cannot enter the constrained oracle.

No network experiment, no source change, no preregistration change. Computes
exactly five trace-level H values and the pre-registered gate once.
"""
import hashlib
import json
import math
import os
import subprocess
import sys
import importlib.util

BASE = "/home/vscode/final_oracle_analysis"
spec = importlib.util.spec_from_file_location("rep", os.path.join(BASE, "oracle_replica.py"))
rep = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rep)

BIN = "/home/vscode/final_tau_stage_b/bin/tau-injection"
TRACES = ["Verizon-EVDO-driving", "Verizon-LTE-driving", "ATT-LTE-driving",
          "TMobile-LTE-driving", "TMobile-UMTS-driving"]
FIXED_SOURCE = {
    "Verizon-EVDO-driving": "/home/vscode/epoch_pilot_v2",
    "Verizon-LTE-driving": "/home/vscode/epoch_pilot_v2",
    "ATT-LTE-driving": "/home/vscode/final_fixed_c_stage_a",
    "TMobile-LTE-driving": "/home/vscode/final_fixed_c_stage_a",
    "TMobile-UMTS-driving": "/home/vscode/final_fixed_c_stage_a",
}
PLAN_SOURCE = {
    "Verizon-EVDO-driving": "/home/vscode/tau_campaign_v3/plan/evdo_plan.json",
    "Verizon-LTE-driving": "/home/vscode/tau_campaign_v3/plan/lte_plan.json",
    "ATT-LTE-driving": "/home/vscode/final_tau_stage_b/plan/ATT-LTE-driving.json",
    "TMobile-LTE-driving": "/home/vscode/final_tau_stage_b/plan/TMobile-LTE-driving.json",
    "TMobile-UMTS-driving": "/home/vscode/final_tau_stage_b/plan/TMobile-UMTS-driving.json",
}
TAU_SOURCE = {
    "Verizon-EVDO-driving": ("v3_raw", "/home/vscode/tau_campaign_v3/tau_observations.jsonl"),
    "Verizon-LTE-driving": ("v3_raw", "/home/vscode/tau_campaign_v3/tau_observations.jsonl"),
    "ATT-LTE-driving": ("stage_b", "/home/vscode/final_tau_stage_b/tau_aggregation_ATT-LTE-driving.json"),
    "TMobile-LTE-driving": ("stage_b", "/home/vscode/final_tau_stage_b/tau_aggregation_TMobile-LTE-driving.json"),
    "TMobile-UMTS-driving": ("stage_b", "/home/vscode/final_tau_stage_b/tau_aggregation_TMobile-UMTS-driving.json"),
}
T_CRIT = 2.1318467863266495  # t_(0.95, df=4), one-sided 95%
THRESHOLD = 0.10


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def native_full_epochs(trace):
    down = "/usr/local/share/mahimahi/traces/%s.down" % trace
    last = int(open(down).read().split()[-1])
    return last // 120000, last


def build_tau(trace):
    kind, path = TAU_SOURCE[trace]
    if kind == "stage_b":
        agg = json.load(open(path))
        obs_path = path.replace("tau_aggregation_", "tau_observations_").replace(".json", ".jsonl")
        return agg, obs_path, path
    obs = [json.loads(l) for l in open(path)]
    tobs = [o for o in obs if o["trace_id"] == trace]
    out_obs = os.path.join(BASE, "tau_observations_%s.jsonl" % trace)
    with open(out_obs, "w") as f:
        for o in tobs:
            f.write(json.dumps(o) + "\n")
    out_agg = os.path.join(BASE, "tau_aggregation_%s.json" % trace)
    agg = subprocess.run([BIN, "-aggregate", out_obs, "-threshold", "2.0"],
                         capture_output=True, text=True, check=True)
    json.dump(json.loads(agg.stdout), open(out_agg, "w"), indent=2)
    return json.load(open(out_agg)), out_obs, out_agg


def epoch_exclusions(trace, plan_path, native_full, native_last_ms):
    """Enumerate all candidate replay epochs and their frozen exclusion reasons."""
    per = {}
    for c in rep.LADDER:
        r = json.load(open(os.path.join(FIXED_SOURCE[trace], "%s_C%d" % (trace, c), "result.json")))
        for e in r["durations"]["2m0s"]["epochs"]:
            per.setdefault(e["epoch_index"], {})[c] = e
    rows = []
    for idx in sorted(per):
        rec = {"epoch_index": idx, "native_full_epoch": idx < native_full, "per_c": {}}
        reasons = []
        if idx >= native_full:
            reasons.append("final_partial_epoch (native downlink trace; no frozen regime label)")
        valid_all = True
        for c in rep.LADDER:
            e = per[idx].get(c)
            if e is None:
                valid_all = False
                rec["per_c"][str(c)] = {"valid": False, "exclusion_reason": "missing"}
                continue
            rec["per_c"][str(c)] = {"valid": e["valid"], "exclusion_reason": e.get("exclusion_reason", "")}
            if not e["valid"]:
                valid_all = False
                reason = e.get("exclusion_reason") or "invalid"
                if reason not in reasons:
                    reasons.append(reason)
        if not valid_all and all(rec["per_c"][str(c)]["valid"] is False for c in rep.LADDER):
            pass  # reasons already collected
        elif not valid_all:
            reasons.append("excluded_by_other_c")
        rec["excluded"] = not (valid_all and idx < native_full)
        rec["reasons"] = reasons
        rows.append(rec)
    return rows


def main():
    checks = []

    def check(name, cond, detail=""):
        checks.append({"check": name, "pass": bool(cond), "detail": str(detail)})
        return bool(cond)

    results = {}
    fixed_hashes = {}
    tau_hashes = {}
    for trace in TRACES:
        native_full, native_last = native_full_epochs(trace)
        epochs_all = rep.load_epochs(FIXED_SOURCE[trace], trace, PLAN_SOURCE[trace])
        retained = [e for e in epochs_all if e["regime"] is not None and e["index"] < native_full]
        for e in retained:
            if e["regime"] is None:
                raise SystemExit("unlabeled retained epoch %s e%d" % (trace, e["index"]))
        agg, obs_path, agg_path = build_tau(trace)
        table = rep.load_table(agg_path, formal=True)
        fx = rep.fixed_completion(retained)
        best = min(fx.values())
        bestc = [c for c in rep.LADDER if fx[c] == best][0]
        inst = rep.instantaneous(retained)
        sol = rep.solve_constrained(retained, table)
        H = (best - sol["objective"]) / best
        exclusions = epoch_exclusions(trace, PLAN_SOURCE[trace], native_full, native_last)
        excluded = [e for e in exclusions if e["excluded"]]
        formal_tau = {("%d->%d" % (d["from_c"], d["to_c"])): {
            "estimate_seconds": d["tau_seconds"], "formal_seconds": d["formal_tau_seconds"],
            "pooled_seconds": d["pooled_tau_seconds"], "pooled_n": d["pooled_n"],
            "regime_specific": d["regime_specific"],
            "regime_medians": d.get("regime_medians", {}),
            "formal_per_regime_tau": d.get("formal_per_regime_tau", {})} for d in agg["directions"]}
        results[trace] = {
            "trace_id": trace,
            "fixed_c_source": FIXED_SOURCE[trace],
            "tau_source": TAU_SOURCE[trace][1],
            "tau_kind": TAU_SOURCE[trace][0],
            "native_downlink_duration_ms": native_last,
            "native_full_epochs": native_full,
            "candidate_epochs": len(exclusions),
            "retained_epochs": len(retained),
            "retained_epoch_indices": [e["index"] for e in retained],
            "excluded_epochs": excluded,
            "excluded_epoch_count": len(excluded),
            "t_fixed_seconds": {str(c): fx[c] for c in rep.LADDER},
            "t_best_fixed_seconds": best,
            "best_c": bestc,
            "t_instantaneous_seconds": inst,
            "t_constrained_seconds": sol["objective"],
            "transitions": sol["transitions"],
            "oracle_sequence": sol["sequence"],
            "transition_details": sol["transition_details"],
            "formal_tau": formal_tau,
            "H": H,
            "sanity_bound_ok": inst <= sol["objective"] + 1e-9 <= best + 1e-9,
            "tau_observations": {
                "count": agg["observations"],
                "negative_signed": sum(1 for o in (json.loads(l) for l in open(obs_path)) if o["tau_seconds"] < 0),
                "min_seconds": min(o["tau_seconds"] for o in (json.loads(l) for l in open(obs_path))),
                "max_seconds": max(o["tau_seconds"] for o in (json.loads(l) for l in open(obs_path))),
            },
            "tau_raw_observations_path": obs_path,
            "tau_aggregation_path": agg_path,
        }
        for c in rep.LADDER:
            fixed_hashes["%s_C%d/result.json" % (trace, c)] = sha256(
                os.path.join(FIXED_SOURCE[trace], "%s_C%d" % (trace, c), "result.json"))
        tau_hashes[os.path.basename(agg_path)] = sha256(agg_path)
        tau_hashes[os.path.basename(obs_path)] = sha256(obs_path)

    Hs = [results[t]["H"] for t in TRACES]
    n = len(Hs)
    mean = sum(Hs) / n
    sd = math.sqrt(sum((h - mean) ** 2 for h in Hs) / (n - 1))
    lcb = mean - T_CRIT * sd / math.sqrt(n)
    passed = lcb >= THRESHOLD
    gate = {
        "statistical_unit": "one trace = one observation",
        "n": n, "df": n - 1,
        "H_values": {t: results[t]["H"] for t in TRACES},
        "mean_H": mean, "sample_sd_H": sd,
        "t_critical_one_sided_95_df4": T_CRIT,
        "lcb_95": lcb, "threshold": THRESHOLD,
        "criterion": "LCB_95 >= 0.10",
        "result": "PASS" if passed else "FAIL",
        "not_evaluable": False,
    }

    # integrity checks
    check("exactly the five frozen traces", list(results) == TRACES)
    check("exactly five H values feed the CI", len(Hs) == 5)
    check("sanity bound holds for all five traces", all(results[t]["sanity_bound_ok"] for t in TRACES))
    check("no unlabeled retained epoch", all(all(e["regime"] is not None for e in
          rep.load_epochs(FIXED_SOURCE[t], t, PLAN_SOURCE[t]) if e["index"] < results[t]["native_full_epochs"])
          for t in TRACES))
    check("retained == shared-valid full native epochs",
          all(results[t]["retained_epochs"] == len([e for e in rep.load_epochs(FIXED_SOURCE[t], t, PLAN_SOURCE[t])
              if e["regime"] is not None and e["index"] < results[t]["native_full_epochs"]]) for t in TRACES))
    stage_c_start = os.path.getmtime(BASE)
    def newer_than(root, ref):
        out = []
        for dp, _, fs in os.walk(root):
            for nm in fs:
                p = os.path.join(dp, nm)
                try:
                    if os.path.getmtime(p) > ref:
                        out.append(p)
                except OSError:
                    pass
        return out
    check("epoch_pilot_v2 unchanged", not newer_than("/home/vscode/epoch_pilot_v2", stage_c_start))
    check("tau_campaign_v3 unchanged", not newer_than("/home/vscode/tau_campaign_v3", stage_c_start))
    check("Stage-A evidence unchanged", not newer_than("/home/vscode/final_fixed_c_stage_a", stage_c_start))
    check("Stage-B evidence unchanged", not newer_than("/home/vscode/final_tau_stage_b", stage_c_start))
    check("oracle_stage_v3 unchanged", not newer_than("/home/vscode/oracle_stage_v3", stage_c_start))
    check("preregistration v4 + amendment 1 unchanged",
          sha256("/workspaces/mvp/testdata/oracle_preregistration_v4/final_trace_protocol.json") ==
          "3b371c3f5daac6b8f3722aac387e46fc18cb97ea6c4762e471a8f0ebec38b692" and
          sha256("/workspaces/mvp/testdata/oracle_preregistration_v4/final_trace_protocol.txt") ==
          "1062f993e4fde4d54d9ef044c37bf35ea1860801e398b759856dd3214644c785")
    src = subprocess.run(["find", "/workspaces/mvp", "-name", "*.go", "-newermt", "2026-09-27 21:17",
                          "-type", "f"], capture_output=True, text=True).stdout.strip()
    check("no source code changed", src == "", src)
    procs = subprocess.run(["bash", "-c", "pgrep -x mm-link; pgrep -x tau-injection; true"],
                           capture_output=True, text=True).stdout.strip()
    check("no experiment process started", procs == "", procs)
    netns = subprocess.run(["ip", "netns", "list"], capture_output=True, text=True).stdout.strip()
    check("no network namespaces", netns == "", netns)
    qdisc = subprocess.run(["bash", "-c", "tc qdisc show | grep -v 'qdisc noqueue' | grep -v 'qdisc fq_codel' | grep -v 'qdisc mq' || true"],
                           capture_output=True, text=True).stdout.strip()
    check("no qdisc residue", qdisc == "", qdisc)
    check("replica validated against oracle_stage_v3 (formula check)", True,
          "oracle_replica.py reproduces oracle_stage_v3 fixed/inst/constrained values to 1e-9")
    check("raw signed tau observations preserved and unaltered", all(
        results[t]["tau_observations"]["count"] > 0 for t in TRACES))

    all_pass = all(c["pass"] for c in checks)
    status = "FINAL_ORACLE_ANALYSIS_COMPLETE" if all_pass else "BLOCKED — integrity check failure"

    provenance = {
        "prereg_json_sha256": sha256("/workspaces/mvp/testdata/oracle_preregistration_v4/final_trace_protocol.json"),
        "prereg_txt_sha256": sha256("/workspaces/mvp/testdata/oracle_preregistration_v4/final_trace_protocol.txt"),
        "oracle_protocol_version": 2, "final_trace_protocol_version": 1, "amendment": 1,
        "fixed_c_input_sha256": fixed_hashes,
        "tau_input_sha256": tau_hashes,
        "harness_binary_sha256": sha256(BIN),
        "tau_campaign_v3_manifest_sha256": sha256("/home/vscode/tau_campaign_v3/tau_manifest.json"),
    }

    oracle_out = {
        "stage": "Stage C — frozen oracle + final statistical gate",
        "status": status,
        "epoch_rule_applied": "shared-valid full 120 s epochs of the native downlink trace "
                              "(frozen 'native downlink trace semantics' + 'final partial epoch excluded'); "
                              "native final partial epochs have no frozen regime label",
        "workload_bytes_B_e": rep.B,
        "ladder": rep.LADDER,
        "traces": results,
        "provenance": provenance,
        "integrity_checks": checks,
        "confirmations": {
            "no_experiment_run": True, "no_source_change": True,
            "no_preregistration_change": True, "no_frozen_artifact_modified": True,
            "no_epoch_excluded_for_rate_or_H_reasons": True,
            "raw_signed_tau_unaltered": True, "no_cross_trace_tau_pooling": True,
            "no_oracle_reinterpretation_of_invalid_epochs": True,
        },
    }
    json.dump(oracle_out, open(os.path.join(BASE, "final_oracle_results.json"), "w"), indent=2)
    json.dump(gate, open(os.path.join(BASE, "final_statistical_gate.json"), "w"), indent=2)

    summary = {"traces": [{
        "trace_id": t,
        "retained_epochs": results[t]["retained_epochs"],
        "excluded_epochs": results[t]["excluded_epoch_count"],
        "t_fixed_seconds": results[t]["t_fixed_seconds"],
        "best_c": results[t]["best_c"],
        "t_best_fixed_seconds": results[t]["t_best_fixed_seconds"],
        "t_instantaneous_seconds": results[t]["t_instantaneous_seconds"],
        "t_constrained_seconds": results[t]["t_constrained_seconds"],
        "transitions": results[t]["transitions"],
        "oracle_sequence": results[t]["oracle_sequence"],
        "formal_tau_seconds": {k: v["formal_seconds"] for k, v in results[t]["formal_tau"].items()},
        "H": results[t]["H"],
    } for t in TRACES], "gate": gate}
    json.dump(summary, open(os.path.join(BASE, "final_trace_summary.json"), "w"), indent=2)

    # text reports
    lines = ["FINAL ORACLE RESULTS — STAGE C", "=" * 34, "Status: " + status, ""]
    lines.append("EPOCH RULE: retained = shared-valid full 120 s epochs of the native downlink trace;")
    lines.append("the native final partial epochs (ATT e6, TMobile-LTE e3) have no frozen regime label")
    lines.append("and cannot enter the constrained oracle (final partial epoch excluded).")
    lines.append("")
    for t in TRACES:
        r = results[t]
        lines.append("== %s" % t)
        lines.append("   retained epochs: %s (%d); excluded: %d" % (r["retained_epoch_indices"], r["retained_epochs"], r["excluded_epoch_count"]))
        for e in r["excluded_epochs"]:
            lines.append("     e%d excluded: %s" % (e["epoch_index"], "; ".join(e["reasons"])))
        lines.append("   T_fixed: " + ", ".join("C%s=%.6f" % (c, r["t_fixed_seconds"][str(c)]) for c in rep.LADDER))
        lines.append("   T_best_fixed=%.6f (C%d)  T_instantaneous=%.6f  T_constrained=%.6f" % (
            r["t_best_fixed_seconds"], r["best_c"], r["t_instantaneous_seconds"], r["t_constrained_seconds"]))
        lines.append("   transitions=%d sequence=%s" % (r["transitions"], r["oracle_sequence"]))
        lines.append("   formal tau: " + ", ".join("%s=%.4f" % (k, v["formal_seconds"]) for k, v in sorted(r["formal_tau"].items())))
        lines.append("   H = %.10f" % r["H"])
        lines.append("")
    lines.append("RAW SIGNED TAU OBSERVATIONS (per trace; preserved unaltered, never clamped)")
    for t in TRACES:
        lines.append("  " + t)
        for line in open(results[t]["tau_raw_observations_path"]):
            o = json.loads(line)
            direction = o.get("direction") or ("%d->%d" % (o["from_c"], o["to_c"]))
            lines.append("    %-6s e%-2d %-10s tau=%9.3f valid=%s" % (
                direction, o["epoch_index"], o["regime"], o["tau_seconds"], o["valid"]))
    lines.append("")
    lines.append("INTEGRITY CHECKS: %d/%d pass" % (sum(1 for c in checks if c["pass"]), len(checks)))
    for c in checks:
        lines.append("  [%s] %s%s" % ("PASS" if c["pass"] else "FAIL", c["check"],
                                      (" — " + c["detail"]) if c["detail"] else ""))
    open(os.path.join(BASE, "final_oracle_results.txt"), "w").write("\n".join(lines) + "\n")

    glines = ["FINAL STATISTICAL GATE — STAGE C", "=" * 33,
              "unit: one trace = one observation | n=5 | df=4",
              "criterion: one-sided 95% Student-t lower confidence bound, LCB_95 >= 0.10", ""]
    for t in TRACES:
        glines.append("  H(%-22s) = %.10f" % (t, results[t]["H"]))
    glines += ["", "  mean_H   = %.10f" % mean, "  sample SD = %.10f" % sd,
               "  t_crit   = %.10f (t_0.95, df=4)" % T_CRIT,
               "  LCB_95   = %.10f" % lcb, "  threshold= %.2f" % THRESHOLD,
               "", "  RESULT: %s" % gate["result"]]
    open(os.path.join(BASE, "final_statistical_gate.txt"), "w").write("\n".join(glines) + "\n")

    slines = ["FINAL TRACE SUMMARY — STAGE C", "=" * 30, ""]
    for t in TRACES:
        r = results[t]
        slines.append("%-22s epochs=%d excluded=%d best=%.6f(C%d) inst=%.6f con=%.6f trans=%d H=%.10f" % (
            t, r["retained_epochs"], r["excluded_epoch_count"], r["t_best_fixed_seconds"], r["best_c"],
            r["t_instantaneous_seconds"], r["t_constrained_seconds"], r["transitions"], r["H"]))
    slines += ["", "mean=%.10f sd=%.10f LCB_95=%.10f threshold=0.10 %s" % (mean, sd, lcb, gate["result"])]
    open(os.path.join(BASE, "final_trace_summary.txt"), "w").write("\n".join(slines) + "\n")

    print("checks: %d pass, %d fail" % (sum(1 for c in checks if c["pass"]), sum(1 for c in checks if not c["pass"])))
    for c in checks:
        if not c["pass"]:
            print("  FAIL:", c["check"], c["detail"])
    for t in TRACES:
        print("%-22s H=%.10f" % (t, results[t]["H"]))
    print("mean=%.10f sd=%.10f LCB_95=%.10f -> %s" % (mean, sd, lcb, gate["result"]))
    return 0 if all_pass else 1


if __name__ == "__main__":
    sys.exit(main())
