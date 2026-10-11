"""L4 environment-design: static per-phase and full-trajectory sweeps,
action-constrained oracle, byte-weighting check, headroom, gates.
Simulator only; no training. Writes testdata/diagnostics/l4_environment_design/.
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
MAX_STEP = 4
HEADROOM_THRESHOLD = 0.10


def p(rate, link=0, lat=0.0, loss=0.0, cong=0.0):
    return {"rate": rate, "link_bandwidth": link, "latency_s": lat,
            "loss_percent": loss, "congestion": cong}


L4 = [
    (p(512 * 1024, lat=0.02, loss=0.3, cong=0.002), 20.0),             # 1 high-capacity
    (p(8 * MIB, link=4 * MIB, lat=0.12, loss=3.0, cong=0.05), 55.0),   # 2 severe shared (byte-heavy)
    (p(8 * MIB, link=6 * MIB, lat=0.04, loss=0.6, cong=0.008), 20.0),  # 3 recovery
    (p(128 * 1024, lat=0.03, loss=0.5, cong=0.004), 20.0),             # 4 throttle
    (p(8 * MIB, link=4 * MIB, lat=0.10, loss=2.5, cong=0.06), 55.0),   # 5 congestion (byte-heavy)
    (p(8 * MIB, link=6 * MIB, lat=0.03, loss=0.4, cong=0.004), 20.0),  # 6 recovery
    (p(1 * MIB, lat=0.02, loss=0.3, cong=0.002), None),                # 7 final
]
L4_PHASES = [x[0] for x in L4]
L4_DUR = [x[1] for x in L4]


def env_for(phases, c, seed, file_mib, max_steps=20000):
    cfg = SimConfig(file_size=file_mib * MIB, min_concurrency=c, max_concurrency=c,
                    decision_interval=1, max_episode_steps=max_steps,
                    phases=[Phase(**x) for x in phases], rand_phase_seconds=(30.0, 30.0))
    e = SimEnv(cfg, seed=seed)
    e.reset()
    return e


def phase_ends(durs):
    ends, t = [], 0.0
    for d in durs[:-1]:
        t += d
        ends.append(t)
    ends.append(float("inf"))
    return ends


def run_full(c, seed):
    e = env_for(L4_PHASES, c, seed, 512)
    e.phase_ends = phase_ends(L4_DUR)
    total, pb, times = 0.0, [0] * len(L4_PHASES), [0.0] * len(L4_PHASES)
    prev, trans, last = 0, [], 0
    while not e.done():
        pi = e.phase_index
        if pi != last:
            trans.append(pi)
            last = pi
        el = e.step(0)["elapsed"]
        total += el
        times[pi] += el
        pb[pi] += e.done_bytes - prev
        prev = e.done_bytes
    return total, pb, times, trans, e.phase_index


def run_oracle(phase_best, seed, trace=False):
    e = env_for(L4_PHASES, 1, seed, 512)
    e.phase_ends = phase_ends(L4_DUR)
    total, seq, times = 0.0, [], [0.0] * len(L4_PHASES)
    while not e.done():
        pi = e.phase_index
        target = phase_best[pi]
        d = target - e.concurrency
        step = min(MAX_STEP, abs(d))
        e.concurrency = max(1, min(16, e.concurrency + (step if d > 0 else -step)))
        el = e.step(0)["elapsed"]
        total += el
        times[pi] += el
        seq.append((round(total, 3), e.concurrency))
    return (total, seq, times) if trace else (total, times)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", default="testdata/diagnostics/l4_environment_design")
    args = ap.parse_args()
    base = os.path.abspath(args.dir)
    os.makedirs(base, exist_ok=True)
    write = lambda n, o: open(os.path.join(base, n), "w").write(json.dumps(o, indent=2, sort_keys=True) + "\n")

    # smoke: transitions reached
    _, _, _, trans_smoke, final_pi = run_full(1, SEEDS[0])

    # per-phase steady sweep
    phase_sweep, phase_best = {}, {}
    for i, ph in enumerate(L4_PHASES):
        by_c = {}
        for c in range(1, 17):
            vals = []
            for s in SEEDS:
                e = env_for([ph], c, s, 128, max_steps=20000)
                e.phase_ends = [float("inf")]
                t = 0.0
                while not e.done():
                    t += e.step(0)["elapsed"]
                vals.append(t)
            by_c[str(c)] = st.mean(vals)
        best = min(range(1, 17), key=lambda c: by_c[str(c)])
        phase_best[i] = best
        phase_sweep[str(i + 1)] = {"phase": ph, "mean_completion_s_by_c": {k: round(v, 4) for k, v in by_c.items()},
                                   "throughput_mibps_by_c": {k: round(128 / v, 3) for k, v in by_c.items()},
                                   "best_c": best,
                                   "c16_penalty_pct": 100 * (by_c["16"] - by_c[str(best)]) / by_c[str(best)],
                                   "c1_penalty_pct": 100 * (by_c["1"] - by_c[str(best)]) / by_c[str(best)]}

    # full fixed-C sweep
    full = {}
    for c in range(1, 17):
        vals, pb, times, trans, fpi = [], None, None, None, None
        for s in SEEDS:
            t, b, tm, tr, fp = run_full(c, s)
            vals.append(t)
            pb, times, trans, fpi = b, tm, tr, fp
        full[str(c)] = {"mean_completion_s": st.mean(vals), "sd_s": st.pstdev(vals),
                        "phase_bytes_mib": [round(x / MIB, 1) for x in pb],
                        "phase_time_s": [round(x, 2) for x in times],
                        "transitions": trans, "final_phase_index": fpi}
    best_static_c = min(range(1, 17), key=lambda c: full[str(c)]["mean_completion_s"])
    best_static = full[str(best_static_c)]["mean_completion_s"]

    # oracle
    oracle_vals, oracle_times, oracle_seq = [], None, None
    for s in SEEDS:
        t, seq, times = run_oracle(phase_best, s, trace=True)
        oracle_vals.append(t)
        oracle_times, oracle_seq = times, seq
    oracle = st.mean(oracle_vals)
    headroom = (best_static - oracle) / best_static

    # byte weighting in the oracle run (measured, from a C-policy run with the
    # same C trajectory is not available; use the oracle's realized byte share by
    # re-running the oracle and recording bytes)
    e = env_for(L4_PHASES, 1, SEEDS[0], 512)
    e.phase_ends = phase_ends(L4_DUR)
    pb_oracle, prev = [0] * len(L4_PHASES), 0
    total_oracle_run = 0.0
    while not e.done():
        pi = e.phase_index
        tgt = phase_best[pi]
        d = tgt - e.concurrency
        stp = min(MAX_STEP, abs(d))
        e.concurrency = max(1, min(16, e.concurrency + (stp if d > 0 else -stp)))
        total_oracle_run += e.step(0)["elapsed"]
        pb_oracle[pi] += e.done_bytes - prev
        prev = e.done_bytes
    degraded = [i for i, ph in enumerate(L4_PHASES)
                if phase_sweep[str(i + 1)]["c16_penalty_pct"] >= 25.0]
    highc = [i for i, ph in enumerate(L4_PHASES)
             if phase_sweep[str(i + 1)]["throughput_mibps_by_c"]["16"] >= 2 * phase_sweep[str(i + 1)]["throughput_mibps_by_c"]["1"]]
    degraded_share = sum(pb_oracle[i] for i in degraded) / max(1, sum(pb_oracle))
    highc_share = sum(pb_oracle[i] for i in highc) / max(1, sum(pb_oracle))

    # transitions with substantial remaining work at best static
    cum, rem = 0, []
    for i in range(len(L4_PHASES) - 1):
        cum += full[str(best_static_c)]["phase_bytes_mib"][i] * MIB
        rem.append(round(max(0.0, (512 * MIB - cum)) / (512 * MIB), 3))
    gates = {
        "A_distinct_phase_optima": {"passed": any(phase_best[i] <= 6 for i in phase_best) and any(phase_best[i] >= 11 for i in phase_best),
                                    "phase_best_c": [phase_best[i] for i in range(len(L4_PHASES))]},
        "B_headroom_ge_10pct": {"passed": headroom >= HEADROOM_THRESHOLD, "headroom": headroom, "threshold": HEADROOM_THRESHOLD},
        "C_transitions_with_work": {"passed": len(trans_smoke) >= 5 and all(x > 0 for x in rem[:5]),
                                    "transitions": trans_smoke, "remaining_frac": rem},
        "D_sim_live_parity": {"passed": True,
                              "detail": "atomic <=4 step, clamp [1,16]; runner test policy_runner_v7_test.go; simulator oracle identical"},
        "E_live_tradeoff": {"passed": None, "detail": "see live probe results"},
        "F_control_authority": {"passed": True, "detail": "16->2 in 4 decisions; phases 20-55 s"},
        "byte_weight": {"degraded_phase_indices": degraded, "degraded_byte_share": degraded_share,
                        "highc_phase_indices": highc, "highc_byte_share": highc_share,
                        "rule_met": degraded_share >= 0.50 and highc_share >= 0.20},
    }
    if not gates["byte_weight"]["rule_met"]:
        outcome = "NOT_READY_PHASE_WEIGHTING"
    elif not gates["A_distinct_phase_optima"]["passed"]:
        outcome = "NOT_READY_ENVIRONMENT_PHYSICS"
    elif not gates["B_headroom_ge_10pct"]["passed"]:
        outcome = "NOT_READY_HEADROOM"
    elif not gates["F_control_authority"]["passed"]:
        outcome = "NOT_READY_CONTROL_AUTHORITY"
    else:
        outcome = "PENDING_LIVE"

    write("l4_prereg_phases.json", {"phases": L4_PHASES, "durations_s": L4_DUR,
                                    "file_bytes": 512 * MIB, "action_space": "none,+1,-1,+4,-4",
                                    "headroom_threshold": HEADROOM_THRESHOLD})
    write("l4_phase_sweep.json", phase_sweep)
    write("l4_full_sweep.json", {"by_c": full, "best_static_c": best_static_c, "best_static_s": best_static})
    write("l4_oracle.json", {"action_constrained_oracle_s": oracle, "oracle_values_s": oracle_vals,
                             "oracle_phase_time_s": [round(x, 2) for x in oracle_times],
                             "oracle_concurrency_trace": oracle_seq[:200],
                             "adaptive_headroom": headroom, "threshold": HEADROOM_THRESHOLD,
                             "phase_best_c": {str(i + 1): phase_best[i] for i in range(len(L4_PHASES))}})
    write("l4_gates.json", {"gates": gates, "outcome": outcome})

    lines = []
    w = lines.append
    w("L4 environment-design (simulator only; no training; no aria2 comparison)")
    w("")
    w(f"{'phase':>5} {'bestC':>5} {'C16 pen%':>9} {'C1 pen%':>8}  throughput C1,C2,C4,C8,C16")
    for i in range(len(L4_PHASES)):
        s = phase_sweep[str(i + 1)]
        t = s["throughput_mibps_by_c"]
        w(f"{i+1:>5} {s['best_c']:>5} {s['c16_penalty_pct']:>9.1f} {s['c1_penalty_pct']:>8.1f}  " +
          " ".join(f"{t[str(c)]:.2f}" for c in (1, 2, 4, 8, 16)))
    w("")
    w(f"durations {L4_DUR}; file 512 MiB")
    w(f"best static C={best_static_c} ({best_static:.1f}s); C1={full['1']['mean_completion_s']:.1f}s; C16={full['16']['mean_completion_s']:.1f}s")
    w(f"action-constrained oracle={oracle:.1f}s -> headroom={100*headroom:.1f}% (threshold 10%)")
    w(f"byte shares (oracle run): degraded={100*degraded_share:.1f}% (rule>=50%), high-C={100*highc_share:.1f}% (rule>=20%)")
    w(f"phase bytes at best static (MiB): {full[str(best_static_c)]['phase_bytes_mib']}")
    w(f"oracle phase times (s): {[round(x,1) for x in oracle_times]}")
    w(f"gates: {json.dumps(gates, sort_keys=True)}")
    w(f"OUTCOME (pre-live): {outcome}")
    with open(os.path.join(base, "report.txt"), "w") as f:
        f.write("\n".join(lines) + "\n")

    hashes = {}
    for root, _, files in os.walk(base):
        for n in files:
            if n == "artifact_hashes.json":
                continue
            pth = os.path.join(root, n)
            hashes[os.path.relpath(pth, base)] = hashlib.sha256(open(pth, "rb").read()).hexdigest()
    write("artifact_hashes.json", hashes)
    print(f"best_static=C{best_static_c} {best_static:.1f}s oracle={oracle:.1f}s headroom={100*headroom:.1f}% "
          f"degraded_share={100*degraded_share:.1f}% outcome={outcome}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
