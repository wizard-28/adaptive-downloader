#!/usr/bin/env python3
"""Salvage Protocol v7 — pre-Stage-1 file-size scaling / headroom analysis.

ANALYTICAL SIMULATION ONLY. No network I/O, no downloads, no server/client
processes, no mm-link. Deterministic (no RNG).

Implements the frozen finite-file scheduler semantics:
  - N concurrent workers, one contiguous range per request
  - chunk-size target affects future allocations only
  - range allocation in order, completion events, work-conserving workers
  - B_pending = TotalSize - CompletedBytes - InFlightBytes, sampled from
    tracker-level state immediately before each allocation decision
  - at most one switch; after the switch the target is fixed; in-flight
    ranges are never resized; final partial range; EOF

Two request-level service models (never blended):
  MODEL A (analytical):          t(s) = D + s/R
  MODEL B (Stage 0S calibrated): t(s) = a + b*s, OLS fit to the four frozen
                                 Stage 0S pooled median service times
"""
import csv
import heapq
import json
import math
import os
import sys
import time

BASE = "/workspaces/mvp/testdata/salvage_s_scaling_analysis"
PROTOCOL = "/workspaces/mvp/testdata/salvage_s_protocol_v7/final_s_protocol.json"
STAGE0S_PER_ARM = "/workspaces/mvp/testdata/salvage_s_stage0s/analysis/per_arm.json"

MIB = 1024 ** 2
GIB = 1024 ** 3
R = 500000.0
D = 0.040
N = 4
ARMS = [("256KiB", 256 * 1024), ("1MiB", 1 * MIB), ("4MiB", 4 * MIB), ("16MiB", 16 * MIB)]
CAL = {256 * 1024: 0.5777166945, 1 * MIB: 2.1905264195, 4 * MIB: 8.6409296585, 16 * MIB: 34.4278713685}
GRID = [64 * MIB, 128 * MIB, 256 * MIB, 512 * MIB, 1 * GIB, 2 * GIB, 4 * GIB, 8 * GIB, 16 * GIB, 32 * GIB]
STRATEGIES = [(16 * MIB, 4 * MIB, 64 * MIB), (4 * MIB, 1 * MIB, 16 * MIB), (1 * MIB, 256 * 1024, 4 * MIB)]


def ols_fit_B():
    xs = sorted(CAL)
    ys = [CAL[x] for x in xs]
    n = len(xs)
    mx = sum(xs) / n
    my = sum(ys) / n
    b = sum((x - mx) * (y - my) for x, y in zip(xs, ys)) / sum((x - mx) ** 2 for x in xs)
    a = my - b * mx
    ss_res = sum((y - (a + b * x)) ** 2 for x, y in zip(xs, ys))
    ss_tot = sum((y - my) ** 2 for y in ys)
    return a, b, 1 - ss_res / ss_tot


A_B, B_B, R2_B = ols_fit_B()


def svc_A(s):
    return D + s / R


def svc_B(s):
    return A_B + B_B * s


def simulate(F, S0, S1=None, B_switch=None, svc=svc_A, keep_schedule=False):
    """Deterministic work-conserving finite-file scheduler simulation."""
    heap = []
    seq = 0
    next_alloc = 0
    completed = 0
    in_flight = 0
    current_S = S0
    switched = False
    switch_info = None
    schedule = []

    def try_alloc(worker, t):
        nonlocal next_alloc, in_flight, current_S, switched, switch_info, seq
        if next_alloc >= F:
            # EOF: there is no allocation decision, so the trigger is not
            # evaluated and no artificial switch event is created.
            return False
        b_pending = F - completed - in_flight
        if S1 is not None and not switched and B_switch is not None and b_pending <= B_switch:
            switched = True
            switch_info = {"time": t, "B_pending_bytes": b_pending, "completed_bytes": completed,
                           "in_flight_bytes": in_flight, "remaining_bytes": F - completed,
                           "next_alloc_bytes": next_alloc}
            current_S = S1
        size = min(current_S, F - next_alloc)
        start = next_alloc
        next_alloc += size
        in_flight += size
        seq += 1
        end = t + svc(size)
        heapq.heappush(heap, (end, seq, worker, size, start))
        if keep_schedule:
            schedule.append({"start": start, "size": size, "alloc_time": t,
                             "end_time": end, "chunk_target": current_S})
        return True

    for w in range(N):
        try_alloc(w, 0.0)
    last_t = 0.0
    n_requests = seq
    while heap:
        t, s, worker, size, start = heapq.heappop(heap)
        completed += size
        in_flight -= size
        last_t = t
        try_alloc(worker, t)
    out = {"completion_seconds": last_t, "n_requests": n_requests,
           "switched": switched, "switch": switch_info}
    if keep_schedule:
        out["schedule"] = schedule
    return out


def gain(ref, t):
    return (ref - t) / ref


def main():
    os.makedirs(BASE, exist_ok=True)
    proto = json.load(open(PROTOCOL))
    s0 = json.load(open(STAGE0S_PER_ARM))
    t_ref_coef = 1.0 / (N * R)  # T_ref(F) = F * t_ref_coef

    models = {
        "MODEL_A_analytical": {"svc": svc_A, "params": {"a_seconds": D, "b_seconds_per_byte": 1.0 / R},
                               "label": "analytical D + s/R"},
        "MODEL_B_stage0s_calibrated": {"svc": svc_B, "params": {"a_seconds": A_B, "b_seconds_per_byte": B_B,
                                                                "r_squared": R2_B},
                                       "label": "Stage 0S calibrated a + b*s"},
    }

    fixed_rows = []
    switch_rows = []
    envelope_rows = []
    per_f_summary = {}
    for model_name, m in models.items():
        svc = m["svc"]
        for F in GRID:
            t_ref = F * t_ref_coef
            fixed = {}
            for label, s in ARMS:
                r = simulate(F, s, svc=svc)
                fixed[label] = r["completion_seconds"]
                fixed_rows.append({"model": model_name, "file_size_bytes": F,
                                   "file_size_label": fmt(F), "arm": label, "arm_bytes": s,
                                   "completion_seconds": r["completion_seconds"],
                                   "normalized_T_over_Tref": r["completion_seconds"] / t_ref,
                                   "n_requests": r["n_requests"]})
            ref_label = min(fixed, key=lambda k: fixed[k])
            ref_time = fixed[ref_label]
            summary = {"model": model_name, "file_size_bytes": F, "file_size_label": fmt(F),
                       "t_ref_seconds": t_ref, "model_implied_lowest_fixed": ref_label,
                       "reference_fixed_completion_seconds": ref_time,
                       "fixed_completion_seconds": fixed}
            for (s0v, s1v, bsw) in STRATEGIES:
                r = simulate(F, s0v, S1=s1v, B_switch=bsw, svc=svc)
                g = gain(ref_time, r["completion_seconds"])
                switch_rows.append({
                    "model": model_name, "file_size_bytes": F, "file_size_label": fmt(F),
                    "strategy": "%s->%s" % (label_of(s0v), label_of(s1v)),
                    "S0_bytes": s0v, "S1_bytes": s1v, "B_switch_bytes": bsw,
                    "switch_fired": r["switched"],
                    "switch_time_seconds": r["switch"]["time"] if r["switched"] else None,
                    "B_pending_at_switch_bytes": r["switch"]["B_pending_bytes"] if r["switched"] else None,
                    "completed_at_switch_bytes": r["switch"]["completed_bytes"] if r["switched"] else None,
                    "in_flight_at_switch_bytes": r["switch"]["in_flight_bytes"] if r["switched"] else None,
                    "remaining_at_switch_bytes": r["switch"]["remaining_bytes"] if r["switched"] else None,
                    "completion_seconds": r["completion_seconds"],
                    "reference_fixed_completion_seconds": ref_time,
                    "absolute_time_saved_seconds": ref_time - r["completion_seconds"],
                    "relative_gain": g,
                    "n_requests": r["n_requests"]})
                summary.setdefault("frozen_switches", {})["%s->%s" % (label_of(s0v), label_of(s1v))] = {
                    "gain": g, "fired": r["switched"], "completion_seconds": r["completion_seconds"]}
            per_f_summary.setdefault(model_name, {})[fmt(F)] = summary

            # EXPLORATORY analytical single-switch upper envelope
            for (s0v, s1v) in [(16 * MIB, 4 * MIB), (16 * MIB, 1 * MIB), (16 * MIB, 256 * 1024),
                               (4 * MIB, 1 * MIB), (4 * MIB, 256 * 1024), (1 * MIB, 256 * 1024)]:
                cap = N * s0v
                thresholds = sorted(set([0] + [cap * k / 32.0 for k in range(1, 33)]))
                best = None
                for bsw in thresholds:
                    if bsw == 0:
                        continue
                    r = simulate(F, s0v, S1=s1v, B_switch=bsw, svc=svc)
                    if not r["switched"]:
                        continue
                    g = gain(ref_time, r["completion_seconds"])
                    if best is None or g > best["relative_gain"]:
                        best = {"relative_gain": g, "B_switch_bytes": bsw,
                                "absolute_time_saved_seconds": ref_time - r["completion_seconds"],
                                "switch_time_seconds": r["switch"]["time"],
                                "completion_seconds": r["completion_seconds"]}
                envelope_rows.append({"model": model_name, "file_size_bytes": F,
                                      "file_size_label": fmt(F),
                                      "strategy": "%s->%s" % (label_of(s0v), label_of(s1v)),
                                      "S0_bytes": s0v, "S1_bytes": s1v,
                                      "best_switch": best,
                                      "reference_fixed_completion_seconds": ref_time,
                                      "reference_fixed": ref_label})

    # dense modular sensitivity around the baseline (aligned) and at larger F
    dense = []
    dense_sizes = [512 * MIB + k * 4 * MIB for k in range(0, 33)] + [2 * GIB + 32 * MIB, 8 * GIB + 32 * MIB, 16 * GIB + 32 * MIB]
    for model_name, m in models.items():
        svc = m["svc"]
        for F in dense_sizes:
            t_ref = F * t_ref_coef
            fixed = {label: simulate(F, s, svc=svc)["completion_seconds"] for label, s in ARMS}
            ref_label = min(fixed, key=lambda k: fixed[k])
            ref_time = fixed[ref_label]
            frozen = simulate(F, 16 * MIB, S1=4 * MIB, B_switch=64 * MIB, svc=svc)
            # best threshold for 16->4
            cap = 64 * MIB
            best = None
            for k in range(1, 129):
                bsw = cap * k / 128.0
                r = simulate(F, 16 * MIB, S1=4 * MIB, B_switch=bsw, svc=svc)
                if not r["switched"]:
                    continue
                g = gain(ref_time, r["completion_seconds"])
                if best is None or g > best["relative_gain"]:
                    best = {"relative_gain": g, "B_switch_bytes": bsw}
            dense.append({"model": model_name, "file_size_bytes": F, "file_size_label": fmt(F),
                          "T_ref_seconds": t_ref, "reference_fixed": ref_label,
                          "frozen_16to4_gain": gain(ref_time, frozen["completion_seconds"]),
                          "frozen_16to4_fired": frozen["switched"],
                          "best_16to4_envelope_gain": best["relative_gain"] if best else None,
                          "best_16to4_threshold_bytes": best["B_switch_bytes"] if best else None,
                          "fixed_16MiB_gain": gain(ref_time, fixed["16MiB"])})

    # Stage 0S validation: model-predicted vs observed fixed-S medians at 512 MiB
    observed = {}
    with open("/workspaces/mvp/testdata/salvage_s_stage0s/analysis/per_run.csv") as f:
        for row in csv.DictReader(f):
            observed.setdefault(row["arm_label"], []).append(float(row["duration_seconds"]))
    validation = {}
    for model_name, m in models.items():
        svc = m["svc"]
        pred = {label: simulate(512 * MIB, s, svc=svc)["completion_seconds"] for label, s in ARMS}
        for label, _ in ARMS:
            obs = sorted(observed[label])
            med = obs[len(obs) // 2] if len(obs) % 2 else 0.5 * (obs[len(obs) // 2 - 1] + obs[len(obs) // 2])
            validation.setdefault(model_name, {})[label] = {
                "predicted_seconds": pred[label], "observed_median_seconds": med,
                "observed_min_seconds": obs[0], "observed_max_seconds": obs[-1],
                "relative_error": (pred[label] - med) / med}

    # scaling diagnostics: frozen-strategy gains and numerical derivatives
    scaling = {}
    for model_name in models:
        rows = [r for r in switch_rows if r["model"] == model_name]
        for strat in ["16MiB->4MiB", "4MiB->1MiB", "1MiB->256KiB"]:
            rs = [r for r in rows if r["strategy"] == strat]
            rs.sort(key=lambda r: r["file_size_bytes"])
            gains = [r["relative_gain"] for r in rs]
            dF = [((gains[i + 1] - gains[i]) / (rs[i + 1]["file_size_bytes"] - rs[i]["file_size_bytes"]))
                  for i in range(len(gains) - 1)]
            scaling.setdefault(model_name, {})[strat] = {
                "gains": gains, "file_sizes": [r["file_size_bytes"] for r in rs],
                "d_gain_dF": dF, "max_gain": max(gains), "min_gain": min(gains)}

    envelope_best = {}
    for model_name in models:
        rows = [r for r in envelope_rows if r["model"] == model_name and r["best_switch"]]
        best_by_strat = {}
        for r in rows:
            s = r["strategy"]
            if s not in best_by_strat or r["best_switch"]["relative_gain"] > best_by_strat[s]["relative_gain"]:
                best_by_strat[s] = {"relative_gain": r["best_switch"]["relative_gain"],
                                    "file_size_label": r["file_size_label"],
                                    "B_switch_bytes": r["best_switch"]["B_switch_bytes"]}
        envelope_best[model_name] = best_by_strat

    result = {
        "protocol": {"id": proto["protocol_id"], "status": proto["status"],
                     "frozen_quantities": proto["human_decision_record"]["frozen_quantities"]},
        "mechanism": {"R_bytes_per_second": R, "D_seconds": D, "N": N,
                      "arms_bytes": {label: s for label, s in ARMS},
                      "strategies": [{"from": label_of(a), "to": label_of(b), "B_switch_bytes": c}
                                     for a, b, c in STRATEGIES]},
        "models": {k: {"label": v["label"], "params": v["params"]} for k, v in models.items()},
        "stage0s_calibration": {str(k): v for k, v in CAL.items()},
        "file_size_grid": GRID,
        "fixed_rows": fixed_rows,
        "switch_rows": switch_rows,
        "envelope_rows": envelope_rows,
        "dense_modular": dense,
        "scaling": scaling,
        "stage0s_validation": validation,
        "envelope_best": envelope_best,
        "notes": [
            "Analytical simulation only; no empirical network measurements.",
            "The reference fixed condition is the model-implied lowest fixed completion time at each file size (not a sample minimum).",
            "The upper envelope is EXPLORATORY ANALYTICAL ONLY and is not the frozen Stage 1 policy.",
        ],
    }
    with open(os.path.join(BASE, "analysis", "scaling_analysis.json"), "w") as f:
        json.dump(result, f, indent=2)
    write_csv(os.path.join(BASE, "analysis", "fixed_s.csv"), fixed_rows)
    write_csv(os.path.join(BASE, "analysis", "frozen_switches.csv"), switch_rows)
    write_csv(os.path.join(BASE, "analysis", "envelope.csv"),
              [{**{k: v for k, v in r.items() if k != "best_switch"},
                "best_relative_gain": (r["best_switch"] or {}).get("relative_gain"),
                "best_B_switch_bytes": (r["best_switch"] or {}).get("B_switch_bytes")} for r in envelope_rows])
    write_csv(os.path.join(BASE, "analysis", "dense_modular.csv"), dense)

    manifest = {
        "protocol_version": proto["protocol_version"],
        "protocol_id": proto["protocol_id"],
        "protocol_status": proto["status"],
        "protocol_json_sha256": sha256(PROTOCOL),
        "frozen_R_bytes_per_second": R, "frozen_D_seconds": D, "frozen_N": N,
        "stage0s_calibrated_service_times_seconds": {str(k): v for k, v in CAL.items()},
        "file_size_grid_bytes": GRID,
        "chunk_size_grid_bytes": [s for _, s in ARMS],
        "switch_strategies": [{"from": label_of(a), "to": label_of(b), "B_switch_bytes": c} for a, b, c in STRATEGIES],
        "model_equations": {
            "MODEL_A_analytical": "t(s) = D + s/R",
            "MODEL_B_stage0s_calibrated": "t(s) = a + b*s with a=%.12f, b=%.12e (OLS fit to the four pooled medians; R^2=%.12f)" % (A_B, B_B, R2_B),
        },
        "simulation_assumptions": [
            "N=4 work-conserving workers; one contiguous range per request; allocation in order",
            "B_pending = TotalSize - CompletedBytes - InFlightBytes sampled immediately before each allocation decision",
            "at most one switch; target fixed afterwards; in-flight ranges never resized",
            "final partial range supported; EOF when all bytes are complete",
            "deterministic (no RNG); ties broken by allocation sequence",
            "no failures/retries are modeled (Stage 0S had 0 failures and 0 retries)",
        ],
        "rng_seed": None,
        "tools": {"analysis": "tools/scaling_analysis.py", "figures": "tools/make_figures.py",
                  "simulator": "tools/scaling_analysis.py:simulate()"},
        "created_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    with open(os.path.join(BASE, "manifest", "manifest.json"), "w") as f:
        json.dump(manifest, f, indent=2)

    print("Model B fit: a=%.9f b=%.12e R2=%.12f" % (A_B, B_B, R2_B))
    for model_name in models:
        s = scaling[model_name]
        print("== %s" % model_name)
        for strat, v in s.items():
            print("   %-16s max_gain=%+.6f%% min_gain=%+.6f%%" % (strat, 100 * v["max_gain"], 100 * v["min_gain"]))
        eb = envelope_best[model_name]
        for strat, v in eb.items():
            print("   envelope %-16s max=%+.6f%% at %s (B_switch=%s)" % (strat, 100 * v["relative_gain"], v["file_size_label"], v["B_switch_bytes"]))
    return 0


def label_of(s):
    for label, v in ARMS:
        if v == s:
            return label
    return str(s)


def fmt(F):
    if F % GIB == 0:
        return "%dGiB" % (F // GIB)
    return "%dMiB" % (F // MIB)


def write_csv(path, rows):
    if not rows:
        return
    keys = []
    for r in rows:
        for k in r:
            if k not in keys:
                keys.append(k)
    with open(path, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=keys)
        w.writeheader()
        for r in rows:
            w.writerow(r)


def sha256(path):
    import hashlib
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


if __name__ == "__main__":
    sys.exit(main())
