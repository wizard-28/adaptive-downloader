"""Static concurrency sweep for Candidate v6 (GATE A prerequisite).

Holds concurrency fixed at C=1..16 over pre-registered network regimes and
measures mean completion time. GATE A requires multiple distinct preferred
concurrency values (not all C16). The sweep is deterministic for a fixed seed
list and does not modify the simulator.

Run BEFORE any v6 training:
    python3 python/v6_static_sweep.py
"""

from __future__ import annotations

import argparse
import json
import os
import statistics as st
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.sim_env_v6 import Phase, SimConfig, SimEnv  # noqa: E402

MIB = 1024 * 1024
SWEEP_SEEDS = [300, 301, 302]
CONCURRENCIES = list(range(1, 17))


def ph(rate, link=0, lat=0.0, loss=0.0, cong=0.0):
    return {
        "rate": rate,
        "link_bandwidth": link,
        "latency_s": lat,
        "loss_percent": loss,
        "congestion": cong,
    }


# Pre-registered regimes: no-cap, hard shared cap, mild/moderate shared cap with
# congestion, heavy congestion, server-throttled. Chosen before training.
REGIMES = [
    {"name": "no-cap-high-rate", "file_size": 16 * MIB, "phases": [ph(8 * MIB, lat=0.02)]},
    {"name": "shared-cap-2mib", "file_size": 16 * MIB, "phases": [ph(8 * MIB, link=2 * MIB, lat=0.01)]},
    {
        "name": "shared-cap-6mib-mild-congestion",
        "file_size": 16 * MIB,
        "phases": [ph(8 * MIB, link=6 * MIB, lat=0.05, loss=0.5, cong=0.01)],
    },
    {
        "name": "shared-cap-10mib-mild-congestion",
        "file_size": 16 * MIB,
        "phases": [ph(8 * MIB, link=10 * MIB, lat=0.05, loss=0.5, cong=0.006)],
    },
    {
        "name": "shared-cap-3mib-heavy-congestion",
        "file_size": 16 * MIB,
        "phases": [ph(8 * MIB, link=3 * MIB, lat=0.10, loss=0.5, cong=0.06)],
    },
    {"name": "server-throttled-no-cap", "file_size": 16 * MIB, "phases": [ph(1 * MIB, lat=0.02)]},
]


def run_fixed(regime, c, seed):
    cfg = SimConfig(
        file_size=regime["file_size"],
        min_concurrency=c,
        max_concurrency=c,
        decision_interval=1,
        max_episode_steps=1000,
        phases=[
            Phase(
                rate=p["rate"],
                link_bandwidth=p["link_bandwidth"],
                latency_s=p["latency_s"],
                loss_percent=p["loss_percent"],
                congestion=p["congestion"],
            )
            for p in regime["phases"]
        ],
        rand_phase_seconds=(1.0, 1.0),
    )
    env = SimEnv(cfg, seed=seed)
    env.reset()
    total = 0.0
    while not env.done():
        total += env.step(0)["elapsed"]
    return total


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="testdata/diagnostics/dqn_validation_v6")
    ap.add_argument("--seeds", default=",".join(str(s) for s in SWEEP_SEEDS))
    args = ap.parse_args()
    seeds = [int(s) for s in args.seeds.split(",") if s]

    os.makedirs(args.out, exist_ok=True)
    regimes = {}
    for regime in REGIMES:
        by_c = {}
        for c in CONCURRENCIES:
            comps = [run_fixed(regime, c, s) for s in seeds]
            by_c[str(c)] = {
                "mean_s": st.mean(comps),
                "median_s": st.median(comps),
                "sd_s": st.pstdev(comps) if len(comps) > 1 else 0.0,
                "min_s": min(comps),
                "max_s": max(comps),
            }
        best_c = min(CONCURRENCIES, key=lambda c: by_c[str(c)]["mean_s"])
        regimes[regime["name"]] = {
            "phases": regime["phases"],
            "file_size": regime["file_size"],
            "mean_completion_s_by_c": {k: round(v["mean_s"], 4) for k, v in by_c.items()},
            "best_c": best_c,
            "by_c": by_c,
        }

    best_values = sorted({r["best_c"] for r in regimes.values()})
    gate = {
        "distinct_best_c": best_values,
        "multiple_meaningful_optima": len(best_values) >= 2 and best_values != [16],
        "rule": "GATE A passes when at least two distinct preferred concurrency values exist and the optima are not all C16",
    }
    out = {
        "label": "Candidate v6 static concurrency sweep (simulator only; GATE A prerequisite)",
        "decision_interval": 1,
        "seeds": seeds,
        "concurrencies": CONCURRENCIES,
        "regimes": regimes,
        "gate_a": gate,
        "note": "Fixed-C policies; one request per decision; shared per-phase capacity B(t) is the simulator link_bandwidth",
    }
    with open(os.path.join(args.out, "static_concurrency_sweep_v6.json"), "w") as f:
        json.dump(out, f, indent=2, sort_keys=True)
        f.write("\n")

    lines = [
        "Candidate v6 static concurrency sweep (simulator only; GATE A)",
        f"decision_interval=1 seeds={seeds}",
        "",
        f"{'regime':<34} {'best':>4}  mean completion s by C (subset)",
    ]
    for name, r in regimes.items():
        b = r["mean_completion_s_by_c"]
        sample = " ".join(f"C{c}={b[str(c)]:.2f}" for c in (1, 2, 4, 8, 12, 16))
        lines.append(f"{name:<34} C{r['best_c']:<3} {sample}")
    lines.append("")
    lines.append(
        f"distinct best C = {best_values}; gate A passed = {gate['multiple_meaningful_optima']}"
    )
    with open(os.path.join(args.out, "static_concurrency_sweep_v6.txt"), "w") as f:
        f.write("\n".join(lines) + "\n")

    print("\n".join(lines))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
