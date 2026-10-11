"""L2 environment-design stage: deterministic simulator sweeps of the
pre-registered L2 trajectory, per-phase C=1..16 curves, and the
adaptive-headroom calculation. No training; frozen v6 simulator only.

Writes to testdata/diagnostics/l2_environment_design/.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import statistics as st
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.sim_env_v6 import Phase, SimConfig, SimEnv  # noqa: E402

MIB = 1024 * 1024
SEEDS = [300, 301, 302]
PHASE_SECONDS = 20.0  # representative duration within the pre-registered [18,25] s bounds


def p(rate, link=0, lat=0.0, loss=0.0, cong=0.0):
    return {"rate": rate, "link_bandwidth": link, "latency_s": lat,
            "loss_percent": loss, "congestion": cong}


L2_PHASES = [
    p(512 * 1024, lat=0.02, loss=0.3, cong=0.002),                    # 1 server capacity
    p(8 * MIB, link=1 * MIB, lat=0.12, loss=3.0, cong=0.03),          # 2 severe shared bottleneck
    p(8 * MIB, link=4 * MIB, lat=0.04, loss=0.5, cong=0.006),         # 3 recovery
    p(128 * 1024, lat=0.03, loss=0.5, cong=0.004),                    # 4 severe server throttle
    p(8 * MIB, link=2.5 * MIB, lat=0.10, loss=2.0, cong=0.06),        # 5 heavy congestion
    p(8 * MIB, link=5 * MIB, lat=0.03, loss=0.4, cong=0.004),         # 6 recovery
    p(1 * MIB, lat=0.02, loss=0.3, cong=0.002),                       # 7 final capacity (to completion)
]

L2_SPEC = {
    "name": "L2",
    "label": "long multi-transition trajectory with materially different phase-optimal concurrency",
    "phases": L2_PHASES,
    "phase_seconds_bounds": [18, 25],
    "phase_seconds_used_for_sweep": PHASE_SECONDS,
    "file_size_bytes": 512 * MIB,
    "note": "pre-registered in PROJECT_CONTEXT 22.38 before measurement; separate from A2-E2 and L1",
}

GATE_HEADROOM = 0.10
MARGINAL_HEADROOM = 0.05


def make_env(phases, c, seed, file_mib, dur, max_steps=5000):
    cfg = SimConfig(file_size=file_mib * MIB, min_concurrency=c, max_concurrency=c,
                    decision_interval=1, max_episode_steps=max_steps,
                    phases=[Phase(**x) for x in phases],
                    rand_phase_seconds=(dur, dur))
    env = SimEnv(cfg, seed=seed)
    env.reset()
    return env


def run_fixed(phases, c, seed, file_mib, dur):
    env = make_env(phases, c, seed, file_mib, dur)
    total = 0.0
    while not env.done():
        total += env.step(0)["elapsed"]
    return total


def run_oracle(phases, phase_best, seed, file_mib, dur):
    """Ramp-aware oracle: knows each phase's best C, moves C by +/-1 per decision."""
    cfg = SimConfig(file_size=file_mib * MIB, min_concurrency=1, max_concurrency=16,
                    decision_interval=1, max_episode_steps=5000,
                    phases=[Phase(**x) for x in phases], rand_phase_seconds=(dur, dur))
    env = SimEnv(cfg, seed=seed)
    env.reset()
    total = 0.0
    while not env.done():
        target = phase_best.get(env.phase_index, env.concurrency)
        if env.concurrency < target:
            a = 1
        elif env.concurrency > target:
            a = 2
        else:
            a = 0
        total += env.step(a)["elapsed"]
    return total


def phase_bytes_and_time(phases, c, seed, file_mib, dur):
    env = make_env(phases, c, seed, file_mib, dur)
    prev_done = 0
    bytes_phase = [0] * len(phases)
    time_phase = [0.0] * len(phases)
    while not env.done():
        pi = env.phase_index
        env.step(0)
        bytes_phase[pi] += env.done_bytes - prev_done
        prev_done = env.done_bytes
        time_phase[pi] += 0.0  # time captured below via elapsed accumulator
    # rerun to capture per-step elapsed (cheap, deterministic)
    env = make_env(phases, c, seed, file_mib, dur)
    prev_done = 0
    bytes_phase = [0] * len(phases)
    time_phase = [0.0] * len(phases)
    while not env.done():
        pi = env.phase_index
        e = env.step(0)["elapsed"]
        bytes_phase[pi] += env.done_bytes - prev_done
        prev_done = env.done_bytes
        time_phase[pi] += e
    return bytes_phase, time_phase


def smoke_transitions():
    env = make_env(L2_PHASES, 8, 1, 32, 2.0, max_steps=5000)
    transitions = []
    last = 0
    while not env.done():
        pi = env.phase_index
        if pi != last:
            transitions.append(pi)
            last = pi
        env.step(0)
    return transitions


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", default="testdata/diagnostics/l2_environment_design")
    args = ap.parse_args()
    base = os.path.abspath(args.dir)
    os.makedirs(base, exist_ok=True)

    # ---- smoke A: transitions reached during a trajectory ----
    transitions = smoke_transitions()
    smoke = {"transitions_reached": transitions, "all_phases": transitions == list(range(1, len(L2_PHASES)))}

    # ---- per-phase steady sweep (128 MiB) ----
    phase_sweep = {}
    phase_best = {}
    for i, ph in enumerate(L2_PHASES):
        by_c = {str(c): st.mean([run_fixed([ph], c, s, 128, 15.0) for s in SEEDS]) for c in range(1, 17)}
        best = min(range(1, 17), key=lambda c: by_c[str(c)])
        phase_best[i] = best
        thr = {c: 128 / by_c[str(c)] for c in map(str, range(1, 17))}
        phase_sweep[str(i + 1)] = {
            "phase": ph, "mean_completion_s_by_c": {k: round(v, 4) for k, v in by_c.items()},
            "throughput_mibps_by_c": {k: round(v, 3) for k, v in thr.items()},
            "best_c": best,
            "c16_vs_best_gap_pct": 100 * (by_c["16"] - by_c[str(best)]) / by_c[str(best)],
            "c1_vs_c16_gap_pct": 100 * (by_c["1"] - by_c["16"]) / by_c["16"],
        }

    # ---- full 512 MiB L2 trajectory sweep ----
    full = {}
    for c in range(1, 17):
        vals = [run_fixed(L2_PHASES, c, s, 512, PHASE_SECONDS) for s in SEEDS]
        full[str(c)] = {"mean_completion_s": st.mean(vals), "sd_s": st.pstdev(vals)}
    best_static_c = min(range(1, 17), key=lambda c: full[str(c)]["mean_completion_s"])
    best_static = full[str(best_static_c)]["mean_completion_s"]

    # ---- oracle (ramp-aware) and instant upper bound ----
    oracle_vals = [run_oracle(L2_PHASES, phase_best, s, 512, PHASE_SECONDS) for s in SEEDS]
    oracle = st.mean(oracle_vals)
    bphase, tphase = phase_bytes_and_time(L2_PHASES, best_static_c, SEEDS[0], 512, PHASE_SECONDS)
    instant = sum((bphase[i] / MIB) / (128 / phase_sweep[str(i + 1)]["mean_completion_s_by_c"][str(phase_best[i])])
                  for i in range(len(L2_PHASES)))
    headroom = (best_static - oracle) / best_static if best_static else 0.0
    headroom_instant = (best_static - instant) / best_static if best_static else 0.0

    bests = sorted(set(phase_best.values()))
    if not smoke["all_phases"]:
        gate = "NOT_READY_TRANSITIONS"
    elif not (any(b <= 6 for b in bests) and any(b >= 11 for b in bests)):
        gate = "NOT_READY_ENVIRONMENT_TOO_WEAK"
    elif headroom >= GATE_HEADROOM:
        gate = "READY_FOR_V7_ENVIRONMENT"
    elif headroom >= MARGINAL_HEADROOM:
        gate = "MARGINAL_ENVIRONMENT"
    else:
        gate = "NOT_READY_ENVIRONMENT_TOO_WEAK"

    write = lambda name, obj: open(os.path.join(base, name), "w").write(json.dumps(obj, indent=2, sort_keys=True) + "\n")
    write("l2_spec.json", L2_SPEC)
    write("l2_smoke.json", smoke)
    write("l2_static_phase_sweep.json", phase_sweep)
    write("l2_full_trajectory_sweep.json", {"phase_seconds": PHASE_SECONDS, "file_mib": 512,
                                            "seeds": SEEDS, "by_c": full, "best_static_c": best_static_c,
                                            "best_static_s": best_static})
    write("l2_adaptive_headroom.json", {
        "best_static_c": best_static_c, "best_static_s": best_static,
        "dynamic_oracle_ramp_aware_s": oracle, "oracle_ramp_values_s": oracle_vals,
        "adaptive_headroom_ramp": headroom,
        "oracle_instant_upper_bound_s": instant, "adaptive_headroom_instant_upper_bound": headroom_instant,
        "phase_best_c": {str(i + 1): phase_best[i] for i in range(len(L2_PHASES))},
        "phase_bytes_mib_best_static": [round(x / MIB, 2) for x in bphase],
        "phase_time_s_best_static": [round(x, 2) for x in tphase],
        "gate_threshold": GATE_HEADROOM, "marginal_threshold": MARGINAL_HEADROOM,
        "gate": gate,
    })

    lines = []
    w = lines.append
    w("L2 environment-design report (simulator only; no training; frozen v6 simulator)")
    w("")
    w(f"file size 512 MiB; phases {len(L2_PHASES)}; phase duration used {PHASE_SECONDS}s within pre-registered [18,25]s; seeds {SEEDS}")
    w(f"smoke A transitions reached: {transitions} (all={smoke['all_phases']})")
    w("")
    w(f"{'phase':>5} {'bestC':>5} {'C16/best gap%':>13} {'C1 vs C16 gap%':>14}  throughput MiB/s by C (1,2,4,8,12,16)")
    for i in range(len(L2_PHASES)):
        s = phase_sweep[str(i + 1)]
        t = s["throughput_mibps_by_c"]
        w(f"{i+1:>5} {s['best_c']:>5} {s['c16_vs_best_gap_pct']:>13.1f} {s['c1_vs_c16_gap_pct']:>14.1f}  " +
          " ".join(f"C{c}={t[str(c)]:.2f}" for c in (1, 2, 4, 8, 12, 16)))
    w("")
    w(f"full 512 MiB: best static C={best_static_c} ({best_static:.1f}s); C1={full['1']['mean_completion_s']:.1f}s; "
      f"C16={full['16']['mean_completion_s']:.1f}s")
    w(f"ramp-aware dynamic oracle={oracle:.1f}s; adaptive headroom={100*headroom:.1f}% "
      f"(instant upper bound {instant:.1f}s / {100*headroom_instant:.1f}%)")
    w(f"phase bytes (MiB) at best static: {[round(x/MIB,1) for x in bphase]}")
    w(f"phase times (s) at best static: {[round(x,1) for x in tphase]}")
    w("")
    w(f"per-phase best C: {[phase_best[i] for i in range(len(L2_PHASES))]}")
    w(f"GATE: {gate}")
    with open(os.path.join(base, "report.txt"), "w") as f:
        f.write("\n".join(lines) + "\n")

    hashes = {}
    for root, _, files in os.walk(base):
        for name in files:
            if name == "artifact_hashes.json":
                continue
            pth = os.path.join(root, name)
            h = hashlib.sha256(open(pth, "rb").read()).hexdigest()
            hashes[os.path.relpath(pth, base)] = h
    write("artifact_hashes.json", hashes)
    print(f"phase best C={[phase_best[i] for i in range(len(L2_PHASES))]}; "
          f"best static C={best_static_c} {best_static:.1f}s; oracle {oracle:.1f}s; "
          f"headroom {100*headroom:.1f}%; gate {gate}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
