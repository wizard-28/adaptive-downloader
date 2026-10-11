#!/usr/bin/env python3
"""Adaptive-N mechanism screening experiments (analytical only).

E1  stationary preferred-N tables across states for every candidate model
E2  within-download state transitions: fixed vs oracle vs causal vs stationary
E3  parameter robustness sweep (predeclared grid)
Outputs: analysis/screening.json + CSVs
"""
import csv
import json
import math
import os
import sys

BASE = "/workspaces/mvp/testdata/adaptive_n_mechanism_screen"
sys.path.insert(0, os.path.join(BASE, "tools"))
import n_sim as ns

MIB = 1024 ** 2
F = 512 * MIB
S = 1 * MIB
D0 = 0.040
R_DEFAULT = 0.5 * MIB
N_GRID = ns.N_GRID
MODELS = ["direct", "fair", "fair_bg", "cap", "degrade"]


def params(C=8 * MIB, K=4, gamma=0.05, R=R_DEFAULT):
    return {"D0": D0, "C": C, "K": K, "gamma": gamma, "R": R}


def e1(rows, summary):
    states = []
    for r in [0.25 * MIB, 0.5 * MIB, 1 * MIB, 2 * MIB]:
        states.append(("direct", {"R": r}, params(R=r), "R=%.2fMB/s" % (r / MIB)))
    for c in [0.5 * MIB, 2 * MIB, 8 * MIB, 32 * MIB]:
        states.append(("fair", {"C": c}, params(C=c), "C=%.2fMB/s" % (c / MIB)))
    for b in [0, 4, 16]:
        states.append(("fair_bg", {"C": 8 * MIB, "B": b}, params(C=8 * MIB), "B=%d" % b))
    for k in [1, 2, 4, 8]:
        states.append(("cap", {"C": 8 * MIB, "K": k}, params(C=8 * MIB, K=k), "K=%d" % k))
    for c in [0.5 * MIB, 2 * MIB, 8 * MIB, 32 * MIB]:
        for g in [0.01, 0.05, 0.1]:
            states.append(("degrade", {"C": c, "gamma": g}, params(C=c, gamma=g),
                           "C=%.2fMB/s,g=%.2f" % (c / MIB, g)))
    for model, state, p, label in states:
        prof = ns.constant_state(state)
        times = ns.fixed_sweep(F, S, prof, model, p)
        best = min(times, key=lambda n: times[n])
        t_ref = {n: F / max(1e-9, _agg(model, n, state, p)) for n in N_GRID}
        row = {"model": model, "state": label, "argmin_N": best,
               "times": {str(n): times[n] for n in N_GRID},
               "normalized": {str(n): times[n] / t_ref[n] for n in N_GRID},
               "state_params": {k: v for k, v in state.items()}}
        rows.append(row)
        summary.setdefault(model, {})[label] = best
    return rows


def _agg(model, n, state, p):
    """Steady-state aggregate rate at concurrency n for normalization."""
    if model == "direct":
        return n * p["R"]
    if model == "fair":
        return state.get("C", p["C"])
    if model == "fair_bg":
        return state.get("C", p["C"]) * n / (n + state.get("B", 0))
    if model == "cap":
        served = min(n, state.get("K", p["K"]))
        return served * min(p["R"], state.get("C", p["C"]) / served)
    if model == "degrade":
        return state.get("C", p["C"]) / (1 + state.get("gamma", p["gamma"]) * (n - 1))
    raise ValueError(model)


def e2(rows, summary):
    cases = [
        ("degrade", {"C": 32 * MIB, "gamma": 0.05}, {"C": 0.5 * MIB, "gamma": 0.05}, 0.25, "degrade C high->low"),
        ("degrade", {"C": 0.5 * MIB, "gamma": 0.05}, {"C": 32 * MIB, "gamma": 0.05}, 0.25, "degrade C low->high"),
        ("degrade", {"C": 8 * MIB, "gamma": 0.01}, {"C": 8 * MIB, "gamma": 0.1}, 0.25, "degrade gamma low->high"),
        ("cap", {"C": 8 * MIB, "K": 8}, {"C": 8 * MIB, "K": 2}, 0.25, "cap K 8->2"),
        ("cap", {"C": 8 * MIB, "K": 2}, {"C": 8 * MIB, "K": 8}, 0.25, "cap K 2->8"),
    ]
    for model, s1, s2, frac, label in cases:
        p = params(C=max(s1.get("C", 8 * MIB), s2.get("C", 8 * MIB)),
                   K=max(s1.get("K", 4), s2.get("K", 4)), gamma=max(s1.get("gamma", 0.05), s2.get("gamma", 0.05)))
        # transition at frac of the ideal first-phase time
        t1 = frac * F / max(1e-9, _agg(model, 4, s1, p))
        prof = ns.two_phase(s1, s2, t1)
        bestN, times = ns.best_fixed(F, S, prof, model, p)
        ref = times[bestN]
        # per-state optima (stationary)
        n1 = min(N_GRID, key=lambda n: ns.simulate(F, S, n, ns.constant_state(s1), model, p)["completion_seconds"])
        n2 = min(N_GRID, key=lambda n: ns.simulate(F, S, n, ns.constant_state(s2), model, p)["completion_seconds"])
        orc = ns.oracle_one_switch(F, S, prof, model, p, grid=12, horizon=ref)
        # causal: start at the state-1 optimum; when the observed median per-request
        # rate crosses the midpoint between the two regimes' expected rates, switch to n2
        # observable signal: per-request rate at the SAME N (n1) in both states
        r1 = _agg(model, n1, s1, p) / n1
        r2 = _agg(model, n1, s2, p) / n1
        caus = ns.simulate(F, S, n1, prof, model, p,
                           switch=_causal_bidir(r1, r2, n2))
        # stationary control: constant state with the same ideal completion
        t_ideal = _ideal_time(prof, F, model, p)
        s_avg = dict(s1)
        s_avg["C"] = F / t_ideal
        sprof = ns.constant_state(s_avg)
        sb, st = ns.best_fixed(F, S, sprof, model, p)
        scaus = ns.simulate(F, S, n1, sprof, model, p, switch=_causal_bidir(r1, r2, n2))
        row = {"model": model, "case": label, "transition_frac": frac,
               "state1": s1, "state2": s2, "state1_opt_N": n1, "state2_opt_N": n2,
               "reference_fixed_N": bestN, "reference_fixed_seconds": ref,
               "fixed_times": {str(n): times[n] for n in N_GRID},
               "oracle_gain": orc["gain"], "oracle_seconds": orc["completion"],
               "oracle_start_N": orc["start_N"], "oracle_target_N": orc["target_N"],
               "oracle_switch_time": orc["switch_time"],
               "causal_start_N": n1, "causal_target_N": n2,
               "causal_gain": (ref - caus["completion_seconds"]) / ref,
               "causal_fired": caus["switch_info"] is not None,
               "causal_seconds": caus["completion_seconds"],
               "stationary_best_N": sb,
               "stationary_causal_gain": (st[sb] - scaus["completion_seconds"]) / st[sb]}
        rows.append(row)
        summary.setdefault(model, {})[label] = {"state1_opt": n1, "state2_opt": n2,
                                                "causal_gain": row["causal_gain"],
                                                "oracle_gain": row["oracle_gain"]}
        print("  E2 %-28s N*=%s->%s ref=%s causal=%+.3f%% oracle=%+.3f%%" % (
            label, n1, n2, bestN, 100 * row["causal_gain"], 100 * row["oracle_gain"]))


def _causal_bidir(r1, r2, target_N, k=3):
    """One future-only N switch driven only by observed completed-request
    throughput: when the median of the last k samples crosses the midpoint
    between the two regimes' expected rates, switch once to target_N."""
    thr = math.sqrt(max(1e-9, r1) * max(1e-9, r2))
    def policy(ctx):
        if len(ctx["samples"]) < k:
            return None
        vals = sorted(ctx["samples"][-k:])
        med = vals[k // 2]
        if r1 > r2 and med < thr:
            return target_N
        if r1 < r2 and med > thr:
            return target_N
        return None
    return policy


def _ideal_time(profile, F, model, p):
    remaining = F
    t = 0.0
    while remaining > 0:
        state, end = profile.state_and_end(t)
        agg = _agg(model, 4, state, p)
        dt = end - t
        delivered = agg * dt
        if delivered >= remaining:
            return t + remaining / agg
        remaining -= delivered
        t = end
    return t


def e3(rows, summary):
    """Parameter robustness of the degrade model's interior optimum."""
    for c in [0.5 * MIB, 2 * MIB, 8 * MIB, 32 * MIB]:
        for g in [0.005, 0.01, 0.02, 0.05, 0.1, 0.2]:
            p = params(C=c, gamma=g)
            prof = ns.constant_state({"C": c, "gamma": g})
            times = ns.fixed_sweep(F, S, prof, "degrade", p)
            best = min(times, key=lambda n: times[n])
            rows.append({"model": "degrade", "C": c, "gamma": g, "argmin_N": best,
                         "times": {str(n): times[n] for n in N_GRID}})
    # state-dependent optimum existence across C for fixed gamma
    for g in [0.005, 0.01, 0.02, 0.05, 0.1, 0.2]:
        argmins = [r["argmin_N"] for r in rows if r["model"] == "degrade" and r["gamma"] == g]
        summary.setdefault("degrade_robustness", {})["gamma=%.3f" % g] = {
            "argmins_over_C": argmins, "state_dependent": len(set(argmins)) > 1}


def e4(rows, summary):
    """Transition-severity sweep for the degrade model (C high -> low)."""
    for ratio in [2, 4, 8, 16, 32]:
        c1, c2 = 32 * MIB, 32 * MIB / ratio
        g = 0.05
        p = params(C=c1, gamma=g)
        s1, s2 = {"C": c1, "gamma": g}, {"C": c2, "gamma": g}
        t1 = 0.25 * F / _agg("degrade", 4, s1, p)
        prof = ns.two_phase(s1, s2, t1)
        bestN, times = ns.best_fixed(F, S, prof, "degrade", p)
        ref = times[bestN]
        n1 = min(N_GRID, key=lambda n: ns.simulate(F, S, n, ns.constant_state(s1), "degrade", p)["completion_seconds"])
        n2 = min(N_GRID, key=lambda n: ns.simulate(F, S, n, ns.constant_state(s2), "degrade", p)["completion_seconds"])
        r1 = _agg("degrade", n1, s1, p) / n1
        r2 = _agg("degrade", n1, s2, p) / n1
        caus = ns.simulate(F, S, n1, prof, "degrade", p, switch=_causal_bidir(r1, r2, n2))
        orc = ns.oracle_one_switch(F, S, prof, "degrade", p, grid=12, horizon=ref)
        row = {"ratio": ratio, "state1_C": c1, "state2_C": c2, "N1": n1, "N2": n2,
               "reference_fixed_N": bestN, "reference_fixed_seconds": ref,
               "causal_gain": (ref - caus["completion_seconds"]) / ref,
               "causal_fired": caus["switch_info"] is not None,
               "oracle_gain": orc["gain"], "oracle_seconds": orc["completion"]}
        rows.append(row)
        summary[str(ratio)] = {"causal_gain": row["causal_gain"], "oracle_gain": row["oracle_gain"],
                               "N1": n1, "N2": n2}
        print("  E4 ratio=%-3d N*=%s->%s causal=%+.3f%% oracle=%+.3f%%" % (
            ratio, n1, n2, 100 * row["causal_gain"], 100 * row["oracle_gain"]))


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
    e1(e1_rows, e1_sum)
    print("E1 done:", len(e1_rows), "rows")
    e2_rows, e2_sum = [], {}
    e2(e2_rows, e2_sum)
    print("E2 done:", len(e2_rows), "rows")
    e3_rows, e3_sum = [], {}
    e3(e3_rows, e3_sum)
    print("E3 done:", len(e3_rows), "rows")
    e4_rows, e4_sum = [], {}
    e4(e4_rows, e4_sum)
    print("E4 done:", len(e4_rows), "rows")
    result = {"e1_stationary": e1_rows, "e1_summary": e1_sum,
              "e2_transitions": e2_rows, "e2_summary": e2_sum,
              "e3_robustness": e3_rows, "e3_summary": e3_sum,
              "e4_severity": e4_rows, "e4_summary": e4_sum,
              "mechanism_inventory": mechanism_inventory()}
    json.dump(result, open(os.path.join(BASE, "analysis", "screening.json"), "w"), indent=2)
    write_csv(os.path.join(BASE, "analysis", "e1_stationary.csv"), e1_rows)
    write_csv(os.path.join(BASE, "analysis", "e2_transitions.csv"), e2_rows)
    write_csv(os.path.join(BASE, "analysis", "e3_robustness.csv"), e3_rows)
    write_csv(os.path.join(BASE, "analysis", "e4_severity.csv"), e4_rows)
    print("wrote analysis/screening.json + CSVs")
    return 0


def mechanism_inventory():
    return [
        {"id": "A", "name": "per-flow / per-connection bandwidth sharing", "screened_model": "fair",
         "physical_motivation": "fair sharing among concurrent TCP flows at a shared bottleneck (documented)",
         "infrastructure": "shared bottleneck replay exists (mm-link); no per-flow fairness instrumentation live",
         "live_evidence": "M3 aggregate throughput envelope: no C sensitivity"},
        {"id": "B", "name": "server-side concurrency cap or queueing", "screened_model": "cap",
         "physical_motivation": "servers limit concurrent connections/workers (documented server behavior)",
         "infrastructure": "the controlled server has no concurrency cap; would need a server change",
         "live_evidence": "T1: per-response pacing, no live reversal at equal granularity"},
        {"id": "C", "name": "queueing/overhead that grows with concurrent requests", "screened_model": "degrade",
         "physical_motivation": "server per-connection processing cost (context switching, memory) degrades aggregate capacity",
         "infrastructure": "not present in the controlled server",
         "live_evidence": "L4 GATE E: the simulated over-concurrency penalty did NOT survive live (NOT_READY_LIVE_MISMATCH)"},
        {"id": "D", "name": "background traffic competing for a shared bottleneck", "screened_model": "fair_bg",
         "physical_motivation": "background flows share the bottleneck; fair sharing reduces the downloader's share",
         "infrastructure": "tc/netem background traffic possible in principle; not part of the current controlled stack",
         "live_evidence": "no live background-traffic study"},
        {"id": "E", "name": "contention where additional flows increase queueing delay", "screened_model": "cap/degrade",
         "physical_motivation": "queueing delay grows with utilization",
         "infrastructure": "not present live",
         "live_evidence": "T1/L4: no live concurrency penalty observed"},
        {"id": "F", "name": "server request-processing capacity that saturates as N increases", "screened_model": "cap",
         "physical_motivation": "server parallelism is finite",
         "infrastructure": "not present live",
         "live_evidence": "T1: aggregate scales monotonically with C"},
        {"id": "G", "name": "changing available capacity + per-flow sharing", "screened_model": "fair (changing C)",
         "physical_motivation": "capacity varies over time (native traces)",
         "infrastructure": "yes (mm-link + native traces)",
         "live_evidence": "S pivot shared model: no state-dependent preference"},
        {"id": "H", "name": "changing server availability + concurrency-dependent queueing", "screened_model": "cap/degrade",
         "physical_motivation": "server capacity varies with load",
         "infrastructure": "not present live",
         "live_evidence": "L4 GATE E: live mismatch"},
    ]


if __name__ == "__main__":
    sys.exit(main())
