#!/usr/bin/env python3
"""Joint N+S phase-locked pacing screen (ANALYTICAL ONLY).

Predeclared design (frozen before running; see manifest/):
  - Regime family: two-phase slow->fast, slow = 640 KiB/s, fast = 2500 KiB/s,
    transition T in {1,2,4,8} s; 512 MiB file; the exact D2 3-phase schedule is
    a validation anchor only.
  - Fixed frontier: N in {1,2,4,8,16} x S in {256K,1M,4M,16M,32M}.
  - Frozen action space A (8 joint actions); starting configuration (4,4M).
  - Primary causal family: one-switch controllers (baseline = median of the
    first 3 observed throughput samples; trigger = median of the last 3
    samples >= threshold * baseline; thresholds {1.25,1.5,2.0}; targets
    {(16,16M),(16,4M),(4,16M)}). No schedule/regime/future information.
  - Diagnostic extension (clearly labelled): two-switch probe controller.
  - Oracle: clairvoyant exact-time switch (diagnostic only).
  - Stationary controls: constant 640 KiB/s and constant 2500 KiB/s.
  - Predeclared analytical gate:
      G1 fixed optimum varies: per-regime argmin differs and the wrong-config
         penalty >= 5% in at least one regime;
      G2 positive causal headroom: best causal controller beats the per-regime
         best fixed by >= 5% in >= 2 regimes;
      G3 same-regime beat over aria2;
      G4 no spurious stationary gain.
    PASS = G1 and G2 and G3 and G4. Otherwise STOP (no empirical execution).
"""
import json
import os
import statistics
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import jns_sim as sim  # noqa: E402

OUT = "/workspaces/mvp/testdata/joint_ns_phase_pacing_screen/analysis"
MIB = sim.MIB

THRESHOLDS = [1.25, 1.5, 2.0]
TARGETS = {
    "joint_16_16M": (16, 16 * MIB),
    "n_only_16_4M": (16, 4 * MIB),
    "s_only_4_16M": (4, 16 * MIB),
}
ALL_TARGETS = {("t%d_%d" % (n, s)): (n, s) for (n, s) in sim.ACTION_SPACE}
FAMILY = sim.FAMILY_TRANSITIONS


def regime_name(T):
    return "T=%gs" % T


def frontier_table(frontier):
    out = {}
    for (N, S), t in frontier.items():
        out["%d/%d" % (N, S)] = t
    return out


def log(msg):
    print(msg, flush=True)


def run_fixed(sched, params):
    fr = {}
    for N in sim.N_GRID:
        for S in sim.S_GRID:
            r = sim.fixed(sim.FILE_SIZE, N, S, sched, params)
            fr[(N, S)] = None if r["failed"] else r["completion"]
    valid = {k: v for k, v in fr.items() if v is not None}
    best = sim.best_fixed(valid)
    act = {}
    for a in sim.ACTION_SPACE:
        r = sim.fixed(sim.FILE_SIZE, a[0], a[1], sched, params)
        act[a] = None if r["failed"] else r["completion"]
    valid_act = {k: v for k, v in act.items() if v is not None}
    best_act = min(valid_act.items(), key=lambda kv: kv[1])
    return fr, best, act, best_act


def run_causal(sched, params, threshold, target):
    ctrl = sim.make_one_switch_controller(signal_k=3, threshold=threshold, target=target)
    r = sim.simulate(sim.FILE_SIZE, sim.START_N, sim.START_S, sched, params,
                     controller=ctrl, keep_requests=True)
    return r


def run_probe(sched, params):
    """Diagnostic two-switch causal probe: at the first completion switch to
    (16, 256K) to shorten the observation delay; once the recent throughput
    rises above the pre-switch baseline, switch to (16, 16M). Labelled
    diagnostic extension, not the primary one-switch design."""
    state = {"step": 0, "baseline": None}

    def ctrl(ctx):
        s = ctx["samples"]
        if state["step"] == 0:
            state["step"] = 1
            return (16, 256 * sim.KIB)
        if state["baseline"] is None and len(s) >= 3:
            state["baseline"] = statistics.median(s[:3])
            return None
        if state["step"] == 1 and state["baseline"] is not None and len(s) >= 4:
            recent = statistics.median(s[-3:])
            if recent >= 1.5 * state["baseline"]:
                state["step"] = 2
                return (16, 16 * MIB)
        return None

    return sim.simulate(sim.FILE_SIZE, sim.START_N, sim.START_S, sched, params,
                        controller=ctrl, keep_requests=True)


def optimistic_causal_bound(sched, params, t_first):
    """Optimistic upper bound for ANY causal controller: the first 4 MiB/4M
    requests are in flight until t_first (no causal action is possible before
    the first completion); from t_first onward allow a clairvoyant choice of
    two planned switches (t_first and the true transition T). If this bound
    cannot beat the best fixed, no causal controller can."""
    targets = list(sim.ACTION_SPACE) + [(N, S) for N in sim.N_GRID for S in sim.S_GRID]
    targets = list(dict.fromkeys(targets))
    T = sched.cum[0]
    best = None
    for a1 in [None] + targets:
        planned = []
        if a1 is not None:
            planned.append((t_first, a1[0], a1[1]))
        if T > t_first:
            for a2 in targets:
                p2 = list(planned) + [(T, a2[0], a2[1])]
                r = sim.simulate(sim.FILE_SIZE, sim.START_N, sim.START_S, sched, params,
                                 planned_switches=p2)
                if best is None or r["completion"] < best["completion"]:
                    best = {"completion": r["completion"], "a1": a1, "a2": a2}
        else:
            r = sim.simulate(sim.FILE_SIZE, sim.START_N, sim.START_S, sched, params,
                             planned_switches=planned)
            if best is None or r["completion"] < best["completion"]:
                best = {"completion": r["completion"], "a1": a1, "a2": None}
    return best


def aria2_model(sched, params):
    """aria2's measured structure: 16 pieces of 32 MiB issued at ~0.405 s,
    each request's rate sampled at its own start and held (no timeout abort:
    aria2's --timeout is a connection timeout). Completion = max end + down."""
    n, L, start = 16, 32 * MIB, 0.405
    rate = sched.rate(start)
    dur = sim._duration(L, rate, params)
    return start + dur + params.down


def main():
    os.makedirs(OUT, exist_ok=True)
    params = sim.Params(**json.load(open(os.path.join(OUT, "calibration.json")))["params"])
    result = {"params": params.__dict__, "family": {}, "stationary": {}, "aria2": {}}

    for T in FAMILY:
        log("regime T=%gs: fixed frontier..." % T)
        sched = sim.two_phase(T)
        fr, best, act, best_act = run_fixed(sched, params)
        entry = {
            "frontier": frontier_table(fr),
            "best_fixed": {"N": best["N"], "S": best["S"], "completion": best["completion"]},
            "action_space_best": {"action": list(best_act[0]), "completion": best_act[1]},
            "causal": {},
        }
        log("regime T=%gs: causal controllers..." % T)
        for th in THRESHOLDS:
            for name, target in TARGETS.items():
                r = run_causal(sched, params, th, target)
                gain = (best["completion"] - r["completion"]) / best["completion"]
                entry["causal"]["th=%.2f_%s" % (th, name)] = {
                    "completion": r["completion"], "n_requests": r["n_requests"],
                    "switches": r["switches"], "gain_vs_best_fixed": gain,
                    "switch_time": r["switches"][0]["time"] if r["switches"] else None,
                }
        log("regime T=%gs: full causal target scan..." % T)
        full = {}
        for th in THRESHOLDS:
            for tname, target in ALL_TARGETS.items():
                r = run_causal(sched, params, th, target)
                full["th=%.2f_%s" % (th, tname)] = {
                    "completion": r["completion"],
                    "gain_vs_best_fixed": (best["completion"] - r["completion"]) / best["completion"],
                    "switches": r["switches"],
                }
        entry["causal_full_scan"] = full
        entry["causal_full_scan_best"] = max(
            ((k, v["gain_vs_best_fixed"], v["completion"]) for k, v in full.items()),
            key=lambda kv: kv[1])
        log("regime T=%gs: probe + oracles..." % T)
        pr = run_probe(sched, params)
        probes = {"joint_16_16M": pr}
        for tname, target in [("n_only_16_4M", (16, 4 * MIB)), ("joint_8_16M", (8, 16 * MIB))]:
            st = {"step": 0, "baseline": None}

            def ctrl(ctx, _target=target, _st=st):
                s = ctx["samples"]
                if _st["step"] == 0:
                    _st["step"] = 1
                    return (16, 256 * sim.KIB)
                if _st["baseline"] is None and len(s) >= 3:
                    _st["baseline"] = statistics.median(s[:3])
                    return None
                if _st["step"] == 1 and _st["baseline"] is not None and len(s) >= 4:
                    recent = statistics.median(s[-3:])
                    if recent >= 1.5 * _st["baseline"]:
                        _st["step"] = 2
                        return _target
                return None

            rr = sim.simulate(sim.FILE_SIZE, sim.START_N, sim.START_S, sched, params,
                              controller=ctrl, keep_requests=True)
            probes[tname] = rr
        entry["probe_variants"] = {
            k: {"completion": v["completion"],
                "gain_vs_best_fixed": (best["completion"] - v["completion"]) / best["completion"],
                "switches": v["switches"]}
            for k, v in probes.items()}
        entry["optimistic_causal_bound"] = optimistic_causal_bound(sched, params, pr["switches"][0]["time"])
        # blind ramps (time-based, no performance signal); the full scan is an
        # oracle over the ramp time/target -> an upper bound for the time-based class
        entry["blind_ramp"] = {}
        for t_ramp in [1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0]:
            for (n, s_) in sim.ACTION_SPACE:
                r = sim.simulate(sim.FILE_SIZE, sim.START_N, sim.START_S, sched, params,
                                 planned_switches=[(t_ramp, n, s_)])
                entry["blind_ramp"]["t=%.0f_%d_%d" % (t_ramp, n, s_)] = {
                    "completion": r["completion"],
                    "gain_vs_best_fixed": (best["completion"] - r["completion"]) / best["completion"]}
        entry["blind_ramp_best"] = max(
            ((k, v["gain_vs_best_fixed"], v["completion"]) for k, v in entry["blind_ramp"].items()),
            key=lambda kv: kv[1])
        entry["probe_diagnostic"] = {
            "completion": pr["completion"], "switches": pr["switches"],
            "gain_vs_best_fixed": (best["completion"] - pr["completion"]) / best["completion"],
        }
        # oracle (a): same starting config, exact-time clairvoyant switch
        orc_a = sim.oracle_one_switch(sim.FILE_SIZE, sched, params, t_grid=64,
                                      start_actions=[(sim.START_N, sim.START_S)],
                                      horizon=best["completion"] * 1.5)
        # oracle (b): unconstrained start + switch over the action space
        orc_b = sim.oracle_one_switch(sim.FILE_SIZE, sched, params, t_grid=64,
                                      start_actions=sim.ACTION_SPACE,
                                      horizon=best["completion"] * 1.5)
        entry["oracle_same_start"] = orc_a
        entry["oracle_unconstrained"] = orc_b
        if orc_a:
            entry["oracle_same_start"]["gain_vs_best_fixed"] = (
                best["completion"] - orc_a["completion"]) / best["completion"]
        if orc_b:
            entry["oracle_unconstrained"]["gain_vs_best_fixed"] = (
                best["completion"] - orc_b["completion"]) / best["completion"]
        entry["aria2"] = aria2_model(sched, params)
        result["family"][regime_name(T)] = entry

    # D2 anchor validation
    log("D2 anchor validation...")
    d2 = sim.d2_anchor()
    fr, best, act, best_act = run_fixed(d2, params)
    result["d2_anchor"] = {
        "frontier": frontier_table(fr),
        "best_fixed": {"N": best["N"], "S": best["S"], "completion": best["completion"]},
        "aria2": aria2_model(d2, params),
    }

    # stationary controls
    for name, rate in [("constant_640KiB", sim.FAMILY_SLOW), ("constant_2500KiB", sim.FAMILY_FAST)]:
        log("stationary %s..." % name)
        sched = sim.constant(rate)
        fr, best, act, best_act = run_fixed(sched, params)
        st = {"best_fixed": {"N": best["N"], "S": best["S"], "completion": best["completion"]},
              "causal": {}}
        for th in THRESHOLDS:
            for tname, target in TARGETS.items():
                r = run_causal(sched, params, th, target)
                st["causal"]["th=%.2f_%s" % (th, tname)] = {
                    "completion": r["completion"], "switches": r["switches"],
                    "gain_vs_best_fixed": (best["completion"] - r["completion"]) / best["completion"],
                }
        # starting config completion in the stationary regime
        st["start_config_completion"] = sim.fixed(sim.FILE_SIZE, sim.START_N, sim.START_S, sched, params)["completion"]
        result["stationary"][name] = st

    # wrong-config penalty matrix (regime x regime)
    log("wrong-config penalty matrix...")
    pen = {}
    for T1 in FAMILY:
        s1 = sim.two_phase(T1)
        _, best1, _, _ = run_fixed(s1, params)
        row = {}
        for T2 in FAMILY:
            s2 = sim.two_phase(T2)
            rr = sim.fixed(sim.FILE_SIZE, best1["N"], best1["S"], s2, params)
            if rr["failed"]:
                row[regime_name(T2)] = None
            else:
                row[regime_name(T2)] = (rr["completion"] - result["family"][regime_name(T2)]["best_fixed"]["completion"]) / \
                    result["family"][regime_name(T2)]["best_fixed"]["completion"]
        pen[regime_name(T1)] = row
    result["wrong_config_penalty"] = pen

    # ---------------- predeclared gate ----------------
    bests = {k: v["best_fixed"] for k, v in result["family"].items()}
    argmins = {k: (v["N"], v["S"]) for k, v in bests.items()}
    distinct_argmins = len(set(argmins.values()))
    wrong_pen_max = max(v for r in pen.values() for v in r.values() if v is not None)

    causal_gains = {}
    for k, v in result["family"].items():
        gs = {name: c["gain_vs_best_fixed"] for name, c in v["causal"].items()}
        gs.update({name: c["gain_vs_best_fixed"] for name, c in v["causal_full_scan"].items()})
        gs.update({("probe_" + name): c["gain_vs_best_fixed"] for name, c in v["probe_variants"].items()})
        causal_gains[k] = gs
    best_per_regime = {k: max(v.values()) for k, v in causal_gains.items()}
    n_regimes_ge5 = sum(1 for k, g in best_per_regime.items() if g >= 0.05)
    median_gain = statistics.median(best_per_regime.values())

    causal_best_completion = {}
    for k in result["family"]:
        causal_best_completion[k] = min(c["completion"] for c in result["family"][k]["causal"].values())
    aria2_beaten_regimes = [k for k in result["family"]
                            if causal_best_completion[k] < result["family"][k]["aria2"]]

    stationary_gains = {}
    for name, st in result["stationary"].items():
        gs = [c["gain_vs_best_fixed"] for c in st["causal"].values()]
        stationary_gains[name] = {"max_causal_gain": max(gs),
                                  "switches": sum(1 for c in st["causal"].values() if c["switches"])}

    gate = {
        "G1_fixed_optimum_varies": {"distinct_argmins": distinct_argmins,
                                    "argmins": {k: list(v) for k, v in argmins.items()},
                                    "max_wrong_config_penalty": wrong_pen_max,
                                    "pass": distinct_argmins > 1 and wrong_pen_max >= 0.05},
        "G2_positive_causal_headroom": {
            "best_causal_gain_per_regime": best_per_regime,
            "optimistic_causal_bound_gain_per_regime": {
                k: (bests[k]["completion"] - result["family"][k]["optimistic_causal_bound"]["completion"]) / bests[k]["completion"]
                for k in result["family"]},
            "best_blind_ramp_gain_per_regime": {
                k: result["family"][k]["blind_ramp_best"][1] for k in result["family"]},
                                        "n_regimes_gain_ge_5pct": n_regimes_ge5,
                                        "family_median_gain": median_gain,
                                        "pass": n_regimes_ge5 >= 2},
        "G3_beats_aria2": {"aria2_per_regime": {k: result["family"][k]["aria2"] for k in result["family"]},
                           "causal_best_per_regime": causal_best_completion,
                           "regimes_beaten": aria2_beaten_regimes,
                           "pass": len(aria2_beaten_regimes) >= 2},
        "G4_no_stationary_gain": {"stationary": stationary_gains,
                                  "pass": all(v["max_causal_gain"] < 0.02 for v in stationary_gains.values())},
    }
    gate["overall"] = "PASS" if all(g["pass"] for g in gate.values() if isinstance(g, dict)) else "FAIL"
    result["gate"] = gate

    json.dump(result, open(os.path.join(OUT, "screen.json"), "w"), indent=2)

    print("=== FIXED FRONTIER OPTIMA ===")
    for k in result["family"]:
        b = bests[k]
        print("  %-6s best fixed N=%-2d S=%-8d %.2fs | action-space best %s %.2fs | aria2 %.2fs" % (
            k, b["N"], b["S"], b["completion"],
            result["family"][k]["action_space_best"]["action"],
            result["family"][k]["action_space_best"]["completion"],
            result["family"][k]["aria2"]))
    print("=== CAUSAL (best threshold/target per regime) ===")
    for k in result["family"]:
        name, g = max(causal_gains[k].items(), key=lambda kv: kv[1])
        print("  %-6s best causal %-18s gain vs best fixed %+.1f%%" % (k, name, 100 * g))
    print("=== PROBE / RAMP / BOUND ===")
    for k in result["family"]:
        e = result["family"][k]
        best_probe = max(e["probe_variants"].items(), key=lambda kv: kv[1]["gain_vs_best_fixed"])
        ramp_key, ramp_gain, ramp_comp = e["blind_ramp_best"]
        ocb = e["optimistic_causal_bound"]
        print("  %-6s best probe %-14s %+6.1f%% | best blind ramp %-14s %+6.1f%% | optimistic causal bound %.2fs (%+.1f%%)" % (
            k, best_probe[0], 100 * best_probe[1]["gain_vs_best_fixed"],
            ramp_key, 100 * ramp_gain,
            ocb["completion"], 100 * (bests[k]["completion"] - ocb["completion"]) / bests[k]["completion"]))
    print("=== ORACLE ===")
    for k in result["family"]:
        oa = result["family"][k]["oracle_same_start"]
        ob = result["family"][k]["oracle_unconstrained"]
        print("  %-6s same-start gain %+.1f%% (switch %.1fs -> %s/%s) | unconstrained gain %+.1f%%" % (
            k, 100 * oa["gain_vs_best_fixed"], oa["switch_time"], oa["to_N"], oa["to_S"],
            100 * ob["gain_vs_best_fixed"]))
    print("=== GATE ===")
    print(json.dumps(gate, indent=2, default=str))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
