#!/usr/bin/env python3
"""Mechanism screening experiments (analytical only).

E1  stationary preferred-S table across rates and file-size residues
E2  within-download transition: fixed vs oracle vs causal vs stationary control
E3  real Mahimahi capacity traces: fixed vs causal, and regime argmin

Outputs: analysis/screening.json, analysis/e1_stationary.csv,
analysis/e2_transitions.csv, analysis/e3_traces.csv
"""
import csv
import json
import math
import os
import statistics
import sys

BASE = "/workspaces/mvp/testdata/salvage_s_mechanism_pivot"
sys.path.insert(0, os.path.join(BASE, "tools"))
import mechanism_sim as ms

MIB, GIB = 1024 ** 2, 1024 ** 3
N, D = 4, 0.040
ARMS = ms.ARMS
ARM_BYTES = {label: s for label, s in ARMS}
NATIVE = "/usr/local/share/mahimahi/traces"
FULL_TRACES = ["Verizon-EVDO-driving", "Verizon-LTE-driving", "ATT-LTE-driving",
               "TMobile-LTE-driving", "TMobile-UMTS-driving"]
MODELS = ["shared", "lockin_full", "serialized"]


def ideal_time(profile, F, N=4):
    """Time for the aggregate rate N*r(t) to deliver F bytes (looped profile)."""
    remaining = F
    t = 0.0
    while remaining > 0:
        r, end = profile.rate_and_end(t)
        dt = end - t
        delivered = N * r * dt
        if delivered >= remaining:
            return t + remaining / (N * r)
        remaining -= delivered
        t = end
    return t


def make_profile(segs_agg, model):
    """Aggregate-capacity segments -> model-specific profile (lockin_full is
    paced per request, so it uses the aggregate rate divided by N)."""
    if model == "lockin_full":
        return ms.Profile([(d, r / N) for d, r in segs_agg])
    return ms.Profile(segs_agg)


def cprof(r_agg, model):
    return ms.constant_profile(r_agg / N) if model == "lockin_full" else ms.constant_profile(r_agg)


OUTAGE_FLOOR_BYTES_PER_S = 8000 / 8  # 8000 bps outage-like crawl (matches the M3 replay floor)


def bin_profile_from_trace(path, bin_s=1.0):
    """1 s-binned aggregate capacity; zero-opportunity bins are replayed at the
    documented 8000 bps outage floor (the M3 precedent) so a locked per-request
    rate is never exactly zero."""
    lines = [int(x) for x in open(path).read().split()]
    dur = lines[-1] / 1000.0
    bins = [0] * (int(dur / bin_s) + 1)
    for ms_ in lines:
        bins[int(ms_ / 1000.0 / bin_s)] += 1
    return ms.Profile([(bin_s, max(b * 1500 / bin_s, OUTAGE_FLOOR_BYTES_PER_S)) for b in bins])


def e1(out_rows, summary):
    rates = [0.25 * MIB, 0.5 * MIB, 1.0 * MIB, 2.0 * MIB, 4.0 * MIB, 8.0 * MIB]  # aggregate bytes/s
    sizes = [("512MiB", 512 * MIB), ("520MiB", 520 * MIB), ("544MiB", 544 * MIB)]
    for model in MODELS:
        for label, F in sizes:
            for r in rates:
                prof = cprof(r, model)
                times = {a: ms.simulate(F, prof, model, s, N=N, D=D)["completion_seconds"]
                         for a, s in ARMS}
                best = min(times, key=lambda k: times[k])
                row = {"model": model, "file_size": label, "file_bytes": F,
                       "aggregate_rate_bytes_per_s": r,
                       "T_256KiB": times["256KiB"], "T_1MiB": times["1MiB"],
                       "T_4MiB": times["4MiB"], "T_16MiB": times["16MiB"], "argmin": best}
                out_rows.append(row)
                summary.setdefault(model, {}).setdefault(label, {})[str(r)] = best


def stationary_control_rate(profile, F, N=4):
    t_ideal = ideal_time(profile, F, N)
    return F / (N * t_ideal)


def e2(out_rows, summary):
    cases = [
        ("low_to_high", 0.5 * MIB, 8 * MIB, 0.25),
        ("low_to_high", 0.5 * MIB, 8 * MIB, 0.50),
        ("high_to_low", 8 * MIB, 0.5 * MIB, 0.25),
        ("high_to_low", 8 * MIB, 0.5 * MIB, 0.50),
        ("mild_low_to_high", 1 * MIB, 4 * MIB, 0.25),
    ]
    F = 512 * MIB
    for model in MODELS:
        for name, r1, r2, frac in cases:
            # transition at frac of the ideal first-phase time
            t1 = frac * F / (N * r1)
            prof = make_profile([(t1, r1), (1e9, r2)], model)
            fixed = ms.best_fixed(F, prof, model, N=N, D=D)
            best_fixed_label = min(fixed, key=lambda k: fixed[k])
            ref = fixed[best_fixed_label]
            # regime optima from E1
            lo = min(ARMS, key=lambda a: ms.simulate(F, cprof(r1, model), model, a[1], N=N, D=D)["completion_seconds"])[0]
            hi = min(ARMS, key=lambda a: ms.simulate(F, cprof(r2, model), model, a[1], N=N, D=D)["completion_seconds"])[0]
            threshold = math.sqrt(r1 * r2) / N  # observed samples are per-request rates
            causal = ms.simulate(F, prof, model, ARM_BYTES[best_fixed_label], N=N, D=D,
                                 switch=ms.causal_rate_switch(threshold, ARM_BYTES[best_fixed_label], ARM_BYTES[hi]))
            oracle = ms.oracle_one_switch(F, prof, model, N=N, D=D, grid=16, horizon=ideal_time(prof, F, N))
            # stationary control: constant rate with the same ideal completion
            r_avg = stationary_control_rate(prof, F, N)
            sprof = cprof(r_avg, model)
            sfixed = ms.best_fixed(F, sprof, model, N=N, D=D)
            sbest = min(sfixed, key=lambda k: sfixed[k])
            causal_stationary = ms.simulate(F, sprof, model, ARM_BYTES[lo], N=N, D=D,
                                            switch=ms.causal_rate_switch(threshold, ARM_BYTES[lo], ARM_BYTES[hi]))
            row = {"model": model, "case": name, "r1": r1, "r2": r2, "transition_frac": frac,
                   "reference_fixed_arm": best_fixed_label, "reference_fixed_seconds": ref,
                   "fixed_seconds": fixed,
                   "causal_start_arm": best_fixed_label, "causal_target_arm": hi,
                   "causal_seconds": causal["completion_seconds"], "causal_fired": causal["switched"],
                   "causal_gain": (ref - causal["completion_seconds"]) / ref,
                   "oracle_seconds": oracle["completion"], "oracle_gain": oracle["gain"],
                   "oracle_switch_time": oracle["switch_time"], "oracle_target": oracle["target"],
                   "stationary_rate_bytes_per_s": r_avg,
                   "stationary_best_fixed_arm": sbest,
                   "stationary_best_fixed_seconds": sfixed[sbest],
                   "stationary_causal_seconds": causal_stationary["completion_seconds"],
                   "stationary_causal_gain": (sfixed[sbest] - causal_stationary["completion_seconds"]) / sfixed[sbest]}
            out_rows.append(row)
            summary.setdefault(model, {})[name + "_" + str(frac)] = {
                "causal_gain": row["causal_gain"], "oracle_gain": row["oracle_gain"],
                "stationary_causal_gain": row["stationary_causal_gain"],
                "reference_fixed_arm": best_fixed_label}


def stable_to_volatile_profile(r_stable, r_lo, r_hi, t1, period=5.0, n=400):
    segs = [(t1, r_stable)]
    for i in range(n):
        segs.append((period, r_hi if i % 2 == 0 else r_lo))
    return ms.Profile(segs)


def e2b(out_rows, summary):
    """Stable -> volatile transition: the cleanest causal form of a
    state-dependent preferred S under the lock-in mechanism."""
    F = 512 * MIB
    r_stable, r_lo, r_hi = 8 * MIB, 0.5 * MIB, 8 * MIB
    t1 = 0.25 * F / (N * r_stable)
    for model in ["shared", "lockin_full"]:
        prof = make_profile([(d, r) for d, r in stable_to_volatile_profile(r_stable, r_lo, r_hi, t1).segs], model)
        fixed = ms.best_fixed(F, prof, model, N=N, D=D)
        best = min(fixed, key=lambda k: fixed[k])
        ref = fixed[best]
        causal = ms.simulate(F, prof, model, ARM_BYTES[best], N=N, D=D,
                             switch=ms.causal_volatility_switch(4, 3.0, ARM_BYTES["256KiB"]))
        oracle = ms.oracle_one_switch(F, prof, model, N=N, D=D, grid=16, horizon=ideal_time(prof, F, N))
        r_avg = stationary_control_rate(prof, F, N)
        sprof = cprof(r_avg, model)
        sfixed = ms.best_fixed(F, sprof, model, N=N, D=D)
        sbest = min(sfixed, key=lambda k: sfixed[k])
        scausal = ms.simulate(F, sprof, model, ARM_BYTES[sbest], N=N, D=D,
                              switch=ms.causal_volatility_switch(4, 3.0, ARM_BYTES["256KiB"]))
        row = {"model": model, "case": "stable_to_volatile", "r_stable": r_stable, "r_lo": r_lo, "r_hi": r_hi,
               "reference_fixed_arm": best, "reference_fixed_seconds": ref, "fixed_seconds": fixed,
               "causal_start_arm": best, "causal_target_arm": "256KiB",
               "causal_seconds": causal["completion_seconds"], "causal_fired": causal["switched"],
               "causal_gain": (ref - causal["completion_seconds"]) / ref,
               "oracle_seconds": oracle["completion"], "oracle_gain": oracle["gain"],
               "oracle_start": oracle.get("start"), "oracle_target": oracle.get("target"),
               "oracle_switch_time": oracle.get("switch_time"),
               "stationary_causal_gain": (sfixed[sbest] - scausal["completion_seconds"]) / sfixed[sbest]}
        out_rows.append(row)
        summary.setdefault(model, {})["stable_to_volatile"] = {"causal_gain": row["causal_gain"],
                                                               "oracle_gain": row["oracle_gain"],
                                                               "reference_fixed_arm": best}
        print("  E2b %-12s ref=%s causal=%+.3f%% oracle=%+.3f%% (start=%s target=%s)" % (
            model, best, 100 * row["causal_gain"], 100 * row["oracle_gain"], oracle.get("start"), oracle.get("target")))


def volatile_profile(r_stable, r_lo, r_hi, t_stable, period, n=2000, mode="alternating", seed=12345):
    segs = [(t_stable, r_stable)]
    if mode == "alternating":
        for i in range(n):
            segs.append((period, r_hi if i % 2 == 0 else r_lo))
    else:  # deterministic pseudo-random alternation (LCG), no phase alignment
        x = seed
        for i in range(n):
            x = (1103515245 * x + 12345) % (2 ** 31)
            segs.append((period, r_hi if (x >> 16) % 2 == 0 else r_lo))
    return ms.Profile(segs)


def e2c(out_rows, summary):
    """Robustness of the stable->volatile opportunity: periods, stable-phase
    length, pseudo-random volatility, and causal policies that start large."""
    F = 512 * MIB
    r_stable, r_lo, r_hi = 8 * MIB, 0.5 * MIB, 8 * MIB
    for stable_frac in [0.25, 0.5]:
        for period in [5.0, 17.0, 60.0]:
            for mode in ["alternating", "random"]:
                t_stable = stable_frac * F / (N * r_stable)
                prof = make_profile([(d, r) for d, r in volatile_profile(r_stable, r_lo, r_hi, t_stable, period, mode=mode).segs], "lockin_full")
                fixed = ms.best_fixed(F, prof, "lockin_full", N=N, D=D)
                best = min(fixed, key=lambda k: fixed[k])
                ref = fixed[best]
                orc = ms.oracle_one_switch(F, prof, "lockin_full", N=N, D=D, grid=20, horizon=ideal_time(prof, F, N))
                causals = {}
                for start_arm in ["16MiB", "4MiB"]:
                    c = ms.simulate(F, prof, "lockin_full", ARM_BYTES[start_arm], N=N, D=D,
                                    switch=ms.causal_volatility_switch(4, 3.0, ARM_BYTES["256KiB"]))
                    causals[start_arm] = {"gain": (ref - c["completion_seconds"]) / ref,
                                          "fired": c["switched"], "seconds": c["completion_seconds"]}
                r_avg = stationary_control_rate(prof, F, N)
                sprof = cprof(r_avg, "lockin_full")
                sfixed = ms.best_fixed(F, sprof, "lockin_full", N=N, D=D)
                sbest = min(sfixed, key=lambda k: sfixed[k])
                sc = ms.simulate(F, sprof, "lockin_full", ARM_BYTES["16MiB"], N=N, D=D,
                                 switch=ms.causal_volatility_switch(4, 3.0, ARM_BYTES["256KiB"]))
                row = {"stable_frac": stable_frac, "period_s": period, "mode": mode,
                       "reference_fixed_arm": best, "reference_fixed_seconds": ref,
                       "fixed_seconds": fixed,
                       "oracle_gain": orc["gain"], "oracle_seconds": orc["completion"],
                       "oracle_start": orc.get("start"), "oracle_target": orc.get("target"),
                       "oracle_switch_time": orc.get("switch_time"),
                       "causal_from_16MiB_gain": causals["16MiB"]["gain"], "causal_16_fired": causals["16MiB"]["fired"],
                       "causal_from_4MiB_gain": causals["4MiB"]["gain"], "causal_4_fired": causals["4MiB"]["fired"],
                       "stationary_causal_gain": (sfixed[sbest] - sc["completion_seconds"]) / sfixed[sbest],
                       "stationary_best_fixed_arm": sbest}
                out_rows.append(row)
                summary.setdefault("lockin_full", {})["sf%s_p%s_%s" % (stable_frac, period, mode)] = {
                    "oracle_gain": row["oracle_gain"], "causal16": row["causal_from_16MiB_gain"],
                    "causal4": row["causal_from_4MiB_gain"], "reference_fixed_arm": best}


def e3(out_rows, summary):
    F = 64 * MIB
    for model in MODELS:
        for fam in FULL_TRACES:
            prof_agg = bin_profile_from_trace(os.path.join(NATIVE, fam + ".down"))
            prof = make_profile([(d, r) for d, r in prof_agg.segs], model)
            fixed = ms.best_fixed(F, prof, model, N=N, D=D)
            best = min(fixed, key=lambda k: fixed[k])
            ref = fixed[best]
            # regime optima at the trace's low/high tercile rates (aggregate)
            rates = sorted(r for _, r in prof_agg.segs)
            lo_r = rates[max(0, len(rates) // 6)]
            hi_r = rates[min(len(rates) - 1, 5 * len(rates) // 6)]
            prof_lo = cprof(lo_r, model)
            prof_hi = cprof(hi_r, model)
            lo = min(ARMS, key=lambda a: ms.simulate(F, prof_lo, model, a[1], N=N, D=D)["completion_seconds"])[0]
            hi = min(ARMS, key=lambda a: ms.simulate(F, prof_hi, model, a[1], N=N, D=D)["completion_seconds"])[0]
            threshold = math.sqrt(lo_r * hi_r) / N  # observed samples are per-request rates
            causal = ms.simulate(F, prof, model, ARM_BYTES[best], N=N, D=D,
                                 switch=ms.causal_rate_switch(threshold, ARM_BYTES[best], ARM_BYTES[hi]))
            r_avg = stationary_control_rate(prof, F, N)
            sprof = cprof(r_avg, model)
            sfixed = ms.best_fixed(F, sprof, model, N=N, D=D)
            sbest = min(sfixed, key=lambda k: sfixed[k])
            causal_stationary = ms.simulate(F, sprof, model, ARM_BYTES[lo], N=N, D=D,
                                            switch=ms.causal_rate_switch(threshold, ARM_BYTES[lo], ARM_BYTES[hi]))
            row = {"model": model, "trace": fam, "file_bytes": F,
                   "trace_mean_aggregate_bytes_per_s": statistics.mean(r for _, r in prof_agg.segs),
                   "low_regime_rate": lo_r, "high_regime_rate": hi_r,
                   "regime_argmin_low": lo, "regime_argmin_high": hi,
                   "reference_fixed_arm": best, "reference_fixed_seconds": ref,
                   "fixed_seconds": fixed,
                   "causal_start_arm": best, "causal_target_arm": hi,
                   "causal_seconds": causal["completion_seconds"], "causal_fired": causal["switched"],
                   "causal_gain": (ref - causal["completion_seconds"]) / ref,
                   "stationary_rate_bytes_per_s": r_avg,
                   "stationary_best_fixed_arm": sbest,
                   "stationary_causal_gain": (sfixed[sbest] - causal_stationary["completion_seconds"]) / sfixed[sbest]}
            out_rows.append(row)
            summary.setdefault(model, {})[fam] = {"regime_low": lo, "regime_high": hi,
                                                  "causal_gain": row["causal_gain"],
                                                  "stationary_causal_gain": row["stationary_causal_gain"],
                                                  "reference_fixed_arm": best}


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


def main():
    e1_rows, e1_sum = [], {}
    e2_rows, e2_sum = [], {}
    e3_rows, e3_sum = [], {}
    e1(e1_rows, e1_sum)
    print("E1 done:", len(e1_rows), "rows")
    e2(e2_rows, e2_sum)
    print("E2 done:", len(e2_rows), "rows")
    e2b(e2_rows, e2_sum)
    print("E2b done (stable->volatile)")
    e2c_rows, e2c_sum = [], {}
    e2c(e2c_rows, e2c_sum)
    print("E2c done:", len(e2c_rows), "rows")
    e3(e3_rows, e3_sum)
    print("E3 done:", len(e3_rows), "rows")

    result = {"e1_stationary": e1_rows, "e1_argmin_summary": e1_sum,
              "e2c_volatility_robustness": e2c_rows, "e2c_summary": e2c_sum,
              "e2_transitions": e2_rows, "e2_summary": e2_sum,
              "e3_traces": e3_rows, "e3_summary": e3_sum,
              "mechanism_inventory": mechanism_inventory()}
    os.makedirs(os.path.join(BASE, "analysis"), exist_ok=True)
    json.dump(result, open(os.path.join(BASE, "analysis", "screening.json"), "w"), indent=2)
    write_csv(os.path.join(BASE, "analysis", "e1_stationary.csv"), e1_rows)
    write_csv(os.path.join(BASE, "analysis", "e2_transitions.csv"), e2_rows)
    write_csv(os.path.join(BASE, "analysis", "e2c_volatility.csv"), e2c_rows)
    write_csv(os.path.join(BASE, "analysis", "e3_traces.csv"), e3_rows)
    print("wrote analysis/screening.json and CSVs")
    return 0


def mechanism_inventory():
    return [
        {"id": "A", "name": "time-varying downstream capacity (shared link, in-flight tracking)",
         "physical_motivation": "mobile/cellular capacity varies on second scales; mm-link delivery-opportunity traces are the natural replay vehicle",
         "required_parameters": "trace/profile r(t), N, RTT D, file size",
         "screened_model": "shared",
         "supported_by_infrastructure": "yes (mm-link + native traces)",
         "risk": "none (natural mechanism)"},
        {"id": "B", "name": "time-varying capacity + fixed RTT",
         "physical_motivation": "same as A with a non-zero latency per request",
         "required_parameters": "A + D",
         "screened_model": "shared (D=40ms)",
         "supported_by_infrastructure": "yes",
         "risk": "none"},
        {"id": "C", "name": "time-varying capacity + per-request setup overhead",
         "physical_motivation": "per-request setup cost (handshake/queueing) on top of capacity variation",
         "required_parameters": "A + setup cost",
         "screened_model": "shared with D representing the setup cost",
         "supported_by_infrastructure": "yes",
         "risk": "low"},
        {"id": "D", "name": "capacity transitions during in-flight range requests",
         "physical_motivation": "requests are not resized; a request experiences every capacity change while in flight",
         "required_parameters": "A",
         "screened_model": "shared (in-flight integration)",
         "supported_by_infrastructure": "yes",
         "risk": "none"},
        {"id": "E", "name": "server-side request serialization / concurrency-dependent service rate",
         "physical_motivation": "a server that serializes range requests or degrades per-request rate with load",
         "required_parameters": "serialization policy, rate",
         "screened_model": "serialized",
         "supported_by_infrastructure": "partially (controlled server can throttle per request; serialization would need a change)",
         "risk": "medium (can be engineered to favor a chunk size)"},
        {"id": "F", "name": "documented HTTP request-level behavior (per-request pacing fixed at request start)",
         "physical_motivation": "server-side per-request rate limiting whose rate is sampled when the response starts (the existing project server semantics for scheduled rates); a response keeps its start rate",
         "required_parameters": "rate schedule/profile, per-request pacing semantics",
         "screened_model": "lockin_full",
         "supported_by_infrastructure": "yes (server.SetRateScheduleAt + per-request pacing fixed at request start)",
         "risk": "medium (the schedule must be externally grounded, not authored to win)"},
    ]


if __name__ == "__main__":
    sys.exit(main())
