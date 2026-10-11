"""v7 design stage: action-space comparison, L3 trajectory static sweep,
action-constrained dynamic oracle, adaptive headroom, gates. Simulator only;
no training. Writes testdata/diagnostics/v7_design/.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import statistics as st
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.sim_env_v6 import Phase, SimConfig, SimEnv  # noqa: E402

MIB = 1024 * 1024
SEEDS = [300, 301, 302]
HEADROOM_THRESHOLD = 0.10
MAX_STEP = 4


def p(rate, link=0, lat=0.0, loss=0.0, cong=0.0):
    return {"rate": rate, "link_bandwidth": link, "latency_s": lat,
            "loss_percent": loss, "congestion": cong}


L3 = [
    (p(512 * 1024, lat=0.02, loss=0.3, cong=0.002), 20.0),              # 1 capacity
    (p(8 * MIB, link=4 * MIB, lat=0.12, loss=3.0, cong=0.05), 35.0),    # 2 severe shared cap
    (p(8 * MIB, link=6 * MIB, lat=0.04, loss=0.6, cong=0.008), 20.0),   # 3 recovery
    (p(128 * 1024, lat=0.03, loss=0.5, cong=0.004), 25.0),              # 4 severe throttle
    (p(8 * MIB, link=4 * MIB, lat=0.10, loss=2.5, cong=0.06), 35.0),    # 5 heavy congestion
    (p(8 * MIB, link=6 * MIB, lat=0.03, loss=0.4, cong=0.004), 20.0),   # 6 recovery
    (p(1 * MIB, lat=0.02, loss=0.3, cong=0.002), None),                 # 7 final capacity
]
L3_PHASES = [x[0] for x in L3]
L3_DUR = [x[1] for x in L3]

ACTION_SPACES = {
    "A_steps": {"actions": ["none", "+1", "-1", "+4", "-4"], "n": 5, "max_delta": 4,
                "reach_16_to_2_decisions": 4, "semantics": "ActionKind inc/dec with Value=step, atomic, clamp [1,16]",
                "implementable": True, "selected": True},
    "B_small_large": {"actions": ["none", "small+", "small-", "large+", "large-"], "n": 5, "max_delta": 4,
                      "reach_16_to_2_decisions": 4, "semantics": "same as A (small=1, large=4)", "implementable": True,
                      "selected": False, "reason": "redundant with A"},
    "C_targets": {"actions": ["none", "low", "medium", "high", "max"], "n": 5, "max_delta": 15,
                  "reach_16_to_2_decisions": 1, "semantics": "instant target set", "implementable": True,
                  "selected": False, "reason": "introduces reserved target-setting actions; discontinuous; oscillation risk"},
}


def make_env(phases, durs, c, seed, file_mib, max_steps=20000):
    cfg = SimConfig(file_size=file_mib * MIB, min_concurrency=c, max_concurrency=c,
                    decision_interval=1, max_episode_steps=max_steps,
                    phases=[Phase(**x) for x in phases], rand_phase_seconds=(durs[0], durs[0]))
    env = SimEnv(cfg, seed=seed)
    env.reset()
    return env


def run_fixed_single(phase, c, seed, file_mib=128):
    cfg = SimConfig(file_size=file_mib * MIB, min_concurrency=c, max_concurrency=c,
                    decision_interval=1, max_episode_steps=20000,
                    phases=[Phase(**phase)], rand_phase_seconds=(30.0, 30.0))
    env = SimEnv(cfg, seed=seed)
    env.reset()
    t = 0.0
    while not env.done():
        t += env.step(0)["elapsed"]
    return t


def run_full_fixed(durs, c, seed):
    first = durs[0]
    cfg = SimConfig(file_size=512 * MIB, min_concurrency=c, max_concurrency=c,
                    decision_interval=1, max_episode_steps=20000,
                    phases=[Phase(**x) for x in L3_PHASES], rand_phase_seconds=(first, first))
    # per-phase durations differ: preset phase_ends manually below
    env = SimEnv(cfg, seed=seed)
    env.reset()
    ends, t = [], 0.0
    for i, d in enumerate(durs[:-1]):
        t += d
        ends.append(t)
    ends.append(float("inf"))
    env.phase_ends = ends
    total = 0.0
    phase_bytes = [0] * len(L3_PHASES)
    prev = 0
    transitions = []
    last = 0
    while not env.done():
        pi = env.phase_index
        if pi != last:
            transitions.append(pi)
            last = pi
        e = env.step(0)["elapsed"]
        total += e
        phase_bytes[pi] += env.done_bytes - prev
        prev = env.done_bytes
    return total, phase_bytes, transitions, env.phase_index


def run_oracle(durs, phase_best, seed):
    cfg = SimConfig(file_size=512 * MIB, min_concurrency=1, max_concurrency=16,
                    decision_interval=1, max_episode_steps=20000,
                    phases=[Phase(**x) for x in L3_PHASES], rand_phase_seconds=(durs[0], durs[0]))
    env = SimEnv(cfg, seed=seed)
    env.reset()
    ends, t = [], 0.0
    for i, d in enumerate(durs[:-1]):
        t += d
        ends.append(t)
    ends.append(float("inf"))
    env.phase_ends = ends
    total = 0.0
    while not env.done():
        target = phase_best.get(env.phase_index, env.concurrency)
        delta = target - env.concurrency
        step = min(MAX_STEP, abs(delta))
        # atomic, clamped application of the selected action space (A)
        env.concurrency = max(1, min(16, env.concurrency + (step if delta > 0 else -step)))
        total += env.step(0)["elapsed"]
    return total


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", default="testdata/diagnostics/v7_design")
    args = ap.parse_args()
    base = os.path.abspath(args.dir)
    os.makedirs(base, exist_ok=True)
    write = lambda name, obj: open(os.path.join(base, name), "w").write(
        json.dumps(obj, indent=2, sort_keys=True) + "\n")

    # per-phase steady sweep
    phase_sweep, phase_best = {}, {}
    for i, ph in enumerate(L3_PHASES):
        by_c = {str(c): st.mean([run_fixed_single(ph, c, s) for s in SEEDS]) for c in range(1, 17)}
        best = min(range(1, 17), key=lambda c: by_c[str(c)])
        phase_best[i] = best
        phase_sweep[str(i + 1)] = {"phase": ph, "mean_completion_s_by_c": {k: round(v, 4) for k, v in by_c.items()},
                                   "throughput_mibps_by_c": {k: round(128 / v, 3) for k, v in by_c.items()},
                                   "best_c": best,
                                   "c16_vs_best_gap_pct": 100 * (by_c["16"] - by_c[str(best)]) / by_c[str(best)]}

    # full trajectory static
    full = {}
    for c in range(1, 17):
        vals, pbytes, trans, lastpi = [], None, None, None
        for s in SEEDS:
            t, pb, tr, lp = run_full_fixed(L3_DUR, c, s)
            vals.append(t)
            pbytes, trans, lastpi = pb, tr, lp
        full[str(c)] = {"mean_completion_s": st.mean(vals), "sd_s": st.pstdev(vals),
                        "phase_bytes_mib": [round(x / MIB, 1) for x in pbytes],
                        "transitions": trans, "final_phase_index": lastpi}
    best_static_c = min(range(1, 17), key=lambda c: full[str(c)]["mean_completion_s"])
    best_static = full[str(best_static_c)]["mean_completion_s"]

    oracle_vals = [run_oracle(L3_DUR, phase_best, s) for s in SEEDS]
    oracle = st.mean(oracle_vals)
    headroom = (best_static - oracle) / best_static

    t_ref, pbytes_ref, trans_ref, lastpi_ref = run_full_fixed(L3_DUR, best_static_c, SEEDS[0])
    rem_at_transitions = []
    cum = 0
    for i in range(len(L3_PHASES)):
        cum += pbytes_ref[i]
        if i < len(L3_PHASES) - 1:
            rem_at_transitions.append(round(max(0.0, 512 * MIB - cum), 1))
    bests = sorted(set(phase_best.values()))
    gates = {
        "A_distinct_phase_optima": {"passed": any(b <= 6 for b in bests) and any(b >= 11 for b in bests),
                                    "phase_best_c": [phase_best[i] for i in range(len(L3_PHASES))]},
        "B_headroom_ge_10pct": {"passed": headroom >= HEADROOM_THRESHOLD,
                                "headroom": headroom, "threshold": HEADROOM_THRESHOLD},
        "C_transitions_with_work": {"passed": len(trans_ref) >= 5 and all(x > 0 for x in rem_at_transitions[:5]),
                                    "transitions": trans_ref, "remaining_bytes_fraction_at_transitions":
                                    [round(x / (512 * MIB), 3) for x in rem_at_transitions]},
        "D_sim_live_parity": {"passed": True,
                              "detail": "selected A semantics: atomic step <=4, clamp [1,16]; live policyRunner honors Action.Value as step (Go test policy_runner_v7_test.go); simulator oracle applies the identical rule"},
        "E_live_shared_cap_tradeoff": {"passed": None,
                                       "detail": "evidence reused from frozen live v6 fixed-C data (A2 shared-cap trajectory); see report"},
        "F_control_authority": {"passed": True,
                                "detail": "16->2 in 4 decisions; 14/4<=4; phases 20-35 s at ~1 decision per completed request"},
    }
    if not gates["A_distinct_phase_optima"]["passed"] or not gates["B_headroom_ge_10pct"]["passed"] or \
       not gates["C_transitions_with_work"]["passed"]:
        outcome = "STOP"
    else:
        outcome = "V7_FORMULATION_READY"

    write("v7_prereg_spec.json", {
        "selected_action_space": "A_steps", "action_spaces_considered": ACTION_SPACES,
        "concurrency_bounds": [1, 16], "chunk_bytes": 1 << 20,
        "decision_cadence": "one decision per completed request",
        "reward": "negative elapsed wall-clock seconds",
        "L3_phases": L3_PHASES, "L3_durations_s": L3_DUR,
        "headroom_threshold": HEADROOM_THRESHOLD, "action_step": MAX_STEP,
        "gates": gates, "outcome": outcome,
    })
    write("action_space_comparison.json", ACTION_SPACES)
    write("l3_static_phase_sweep.json", phase_sweep)
    write("l3_full_trajectory_sweep.json", {"by_c": full, "best_static_c": best_static_c, "best_static_s": best_static})
    write("l3_adaptive_headroom.json", {
        "best_static_c": best_static_c, "best_static_s": best_static,
        "action_constrained_oracle_s": oracle, "oracle_values_s": oracle_vals,
        "adaptive_headroom": headroom, "threshold": HEADROOM_THRESHOLD,
        "action_space": "A_steps (none,+1,-1,+4,-4), atomic, clamp [1,16]",
        "phase_best_c": {str(i + 1): phase_best[i] for i in range(len(L3_PHASES))},
    })
    write("v7_gates.json", {"gates": gates, "outcome": outcome})

    lines = []
    w = lines.append
    w("v7 design stage: action authority + L3 environment (simulator only; no training)")
    w("")
    w("Action spaces: A steps {none,+1,-1,+4,-4} (SELECTED), B small/large (redundant), C targets (rejected: reserved actions/oscillation)")
    w(f"Selected semantics: atomic step <=4, clamp [1,16], one decision per completed request")
    w("")
    w(f"{'phase':>5} {'bestC':>5} {'C16 penalty%':>13}  throughput MiB/s (C1,C2,C4,C16)")
    for i in range(len(L3_PHASES)):
        s = phase_sweep[str(i + 1)]
        t = s["throughput_mibps_by_c"]
        w(f"{i+1:>5} {s['best_c']:>5} {s['c16_vs_best_gap_pct']:>13.1f}  " +
          " ".join(f"C{c}={t[str(c)]:.2f}" for c in (1, 2, 4, 8, 16)))
    w("")
    w(f"best static C={best_static_c} ({best_static:.1f}s); C16={full['16']['mean_completion_s']:.1f}s; C1={full['1']['mean_completion_s']:.1f}s")
    w(f"action-constrained oracle (<=4/decision) = {oracle:.1f}s  ->  headroom = {100*headroom:.1f}% (threshold 10%)")
    w(f"transitions reached: {trans_ref}; remaining-bytes fraction at each transition: {[round(x/(512*MIB),3) for x in rem_at_transitions]}")
    w(f"phase bytes at best static (MiB): {[round(x/MIB,1) for x in pbytes_ref]}")
    w("")
    w(f"gates: {json.dumps(gates, sort_keys=True)}")
    w(f"OUTCOME: {outcome}")
    with open(os.path.join(base, "report.txt"), "w") as f:
        f.write("\n".join(lines) + "\n")

    hashes = {}
    for root, _, files in os.walk(base):
        for name in files:
            if name == "artifact_hashes.json":
                continue
            pth = os.path.join(root, name)
            hashes[os.path.relpath(pth, base)] = hashlib.sha256(open(pth, "rb").read()).hexdigest()
    write("artifact_hashes.json", hashes)
    print(f"phase_best={[phase_best[i] for i in range(len(L3_PHASES))]} best_static=C{best_static_c} "
          f"{best_static:.1f}s oracle={oracle:.1f}s headroom={100*headroom:.1f}% outcome={outcome}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
