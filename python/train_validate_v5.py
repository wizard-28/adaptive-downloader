"""Train, select, and audit DQN Candidate v5: concurrency-only adaptive control.

Candidate v5 uses the state-spec-version-2 temporal state and the corrected
pipeline simulator (python/rl/sim_env_v5.py; see PROJECT_CONTEXT §22.29):

  * chunk size is FIXED at 1 MiB for every policy (aria2-like granularity);
  * the only learned variable is concurrency (1-4);
  * one decision interval completes `decision_interval` requests per RL step;
  * reward = -elapsed wall-clock seconds, gamma = 1.

Pipeline:
  static concurrency sweep -> (optional) decision-interval selection ->
  multi-seed training -> validation-only model selection -> held-out test vs
  fixed C=1..4 -> behavior diagnostics + NOT-READY constant-policy gate ->
  export exactly one candidate.

This script does NOT run the real downloader (no live transfer), does NOT run
an aria2 comparison, and makes NO performance claim.

Seeds are strictly separated (deterministic given the same CLI arguments):

  training (network init + env stream) : --seeds        (default 0..4)
  validation rollouts                  : --val-seeds    (default 100..102)
  test rollouts                        : --test-seeds   (default 200..203)
  static-sweep rollouts                : --sweep-seeds  (default 300..302)
  randomized-distribution evaluation   : --random-seeds (default 400..404)

The held-out TEST conditions and seeds are never used for any selection.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import statistics as st
import sys
import time

import numpy as np
import torch

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.dqn import TrainConfig, train  # noqa: E402
from rl.model import export_model  # noqa: E402
from rl.state_v5 import default_state_spec as default_state_spec_v5  # noqa: E402
from rl.sim_env_v5 import (  # noqa: E402
    ACTION_NAMES,
    FIXED_CHUNK,
    NUM_ACTIONS,
    Phase,
    SimConfig,
    SimEnv as SimEnvV5,
)

MIB = 1024 * 1024
EPISODES = 1500
MAX_STEPS = 400
MAX_CONC = 4
DECISION_INTERVALS = [1, 2, 4]
DEFAULT_SEEDS = [0, 1, 2, 3, 4]
DEFAULT_VAL_SEEDS = [100, 101, 102]
DEFAULT_TEST_SEEDS = [200, 201, 202, 203]
DEFAULT_SWEEP_SEEDS = [300, 301, 302]
DEFAULT_RANDOM_SEEDS = [400, 401, 402, 403, 404]

# Documented v5 training distribution (mirrors sim_env_v5.SimConfig defaults).
# Training must not be tuned on the eventual live scenarios.
TRAIN_RANGES = {
    "rand_rate": (256 * 1024, 8 * MIB),
    "rand_link_mult": (0.0, 4.0),
    "rand_latency_s": (0.0, 0.08),
    "rand_loss_percent": (0.0, 4.0),
    "rand_congestion": (0.0, 0.06),
    "rand_file_size": (4 * MIB, 32 * MIB),
    "rand_phase_count": (1, 3),
    "rand_phase_seconds": (0.3, 2.0),
}


def ph(rate, link=0, lat=0.0, loss=0.0, cong=0.0):
    return {
        "rate": rate,
        "link_bandwidth": link,
        "latency_s": lat,
        "loss_percent": loss,
        "congestion": cong,
    }


# ---------------------------------------------------------------------------
# Static concurrency sweep regimes (TASK 4 prerequisite; simulator only).
# Different regimes are expected to have different best fixed C; this is a
# diagnostic, not a manufactured result.
# ---------------------------------------------------------------------------
SWEEP_REGIMES = [
    {
        "name": "stable-no-cap",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(4 * MIB, lat=0.02)],
    },
    {
        "name": "low-bandwidth",
        "file_size": 4 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(512 * 1024, lat=0.02)],
    },
    {
        "name": "latency-dominant",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(4 * MIB, lat=0.075)],
    },
    {
        "name": "shared-cap-hard",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(8 * MIB, link=2 * MIB)],
    },
    {
        "name": "shared-cap-moderate",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(1 * MIB, link=int(2.5 * MIB), lat=0.02, loss=1.0, cong=0.005)],
    },
    {
        "name": "combined-mild",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(4 * MIB, link=6 * MIB, lat=0.06, loss=1.5, cong=0.025)],
    },
    {
        "name": "throttle-up-wallclock",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(512 * 1024, lat=0.02), ph(6 * MIB, lat=0.02)],
    },
    {
        "name": "throttle-down-wallclock",
        "file_size": 8 * MIB,
        "phase_seconds": 0.5,
        "phases": [ph(6 * MIB, lat=0.02), ph(512 * 1024, lat=0.02)],
    },
]

# ---------------------------------------------------------------------------
# Held-out conditions. Explicit phases with pinned wall-clock transition times;
# these parameter tuples are not sampled by the training distribution.
# ---------------------------------------------------------------------------
VALIDATION_CONDITIONS = [
    {
        "name": "val-steady-shared-cap",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(3 * MIB, link=int(1.5 * MIB), lat=0.04)],
    },
    {
        "name": "val-latency-high",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(5 * MIB, lat=0.075)],
    },
    {
        "name": "val-loss-congestion",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(4 * MIB, lat=0.03, loss=2.5, cong=0.04)],
    },
    {
        "name": "val-multiphase-up",
        "file_size": 16 * MIB,
        "phase_seconds": 0.9,
        "phases": [ph(768 * 1024, lat=0.02, loss=0.5), ph(5 * MIB, lat=0.02, loss=0.5)],
    },
]

TEST_CONDITIONS = [
    {
        "name": "unseen-low-bandwidth",
        "file_size": 4 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(192 * 1024)],
    },
    {
        "name": "unseen-high-bandwidth",
        "file_size": 16 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(12 * MIB)],
    },
    {
        "name": "unseen-latency",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(4 * MIB, lat=0.12)],
    },
    {
        "name": "unseen-loss",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(4 * MIB, loss=6.0, cong=0.02)],
    },
    {
        "name": "unseen-latency+loss",
        "file_size": 8 * MIB,
        "phase_seconds": 1.0,
        "phases": [ph(3 * MIB, lat=0.1, loss=5.0, cong=0.03)],
    },
    {
        "name": "unseen-throttle-up",
        "file_size": 16 * MIB,
        "phase_seconds": 1.2,
        "phases": [ph(256 * 1024), ph(10 * MIB)],
    },
    {
        "name": "unseen-throttle-down",
        "file_size": 16 * MIB,
        "phase_seconds": 0.5,
        "phases": [ph(10 * MIB), ph(512 * 1024)],
    },
    {
        "name": "unseen-three-phase",
        "file_size": 16 * MIB,
        "phase_seconds": 0.6,
        "phases": [
            ph(1 * MIB, lat=0.03, loss=1.0),
            ph(4 * MIB, lat=0.03, loss=1.0),
            ph(768 * 1024, lat=0.03, loss=1.0),
        ],
    },
]


# ---------------------------------------------------------------------------
# Environment construction
# ---------------------------------------------------------------------------
def condition_phases(cond):
    return [
        Phase(
            rate=p["rate"],
            link_bandwidth=p.get("link_bandwidth", 0),
            latency_s=p.get("latency_s", 0.0),
            loss_percent=p.get("loss_percent", 0.0),
            congestion=p.get("congestion", 0.0),
            overhead_s=p.get("overhead_s", 0.02),
        )
        for p in cond["phases"]
    ]


def make_env(cond, seed, interval, fixed_c=None):
    secs = cond.get("phase_seconds", 1.0)
    if isinstance(secs, (tuple, list)):
        lo, hi = secs
    else:
        lo = hi = secs
    lo_c, hi_c = (fixed_c, fixed_c) if fixed_c else (1, MAX_CONC)
    cfg = SimConfig(
        file_size=cond["file_size"],
        min_concurrency=lo_c,
        max_concurrency=hi_c,
        decision_interval=interval,
        max_episode_steps=MAX_STEPS,
        phases=condition_phases(cond),
        rand_phase_seconds=(lo, hi),
    )
    return SimEnvV5(cfg, seed=seed)


def train_cfg(interval):
    return SimConfig(
        decision_interval=interval,
        max_episode_steps=MAX_STEPS,
        **TRAIN_RANGES,
    )


# ---------------------------------------------------------------------------
# Rollout + diagnostics
# ---------------------------------------------------------------------------
def rollout(env, agent=None):
    """Run one episode; agent=None means the fixed-concurrency (action none) policy."""
    env.reset()
    total_elapsed = 0.0
    steps = 0
    requests = 0
    action_counts = [0] * NUM_ACTIONS
    effective_counts = [0] * NUM_ACTIONS
    conc_time = [0.0] * (MAX_CONC + 1)
    changes = []
    transition_decisions = []
    prev_phase = env.phase_index

    while not env.done() and steps < MAX_STEPS + 5:
        action = agent.greedy(env.state()) if agent is not None else 0
        c_before = env.concurrency
        ph_before = env.phase_index
        res = env.step(action)
        elapsed = res["elapsed"]
        total_elapsed += elapsed
        requests += res["requests"]
        action_counts[action] += 1
        if res["effective_action"] != -1:
            effective_counts[res["effective_action"]] += 1
        conc_time[env.concurrency] += elapsed
        if env.concurrency != c_before:
            changes.append(
                {
                    "decision": steps,
                    "phase_index": ph_before,
                    "from": c_before,
                    "to": env.concurrency,
                    "elapsed_before": total_elapsed - elapsed,
                }
            )
        if env.phase_index != prev_phase:
            transition_decisions.append(steps)
            prev_phase = env.phase_index
        steps += 1

    first_transition = transition_decisions[0] if transition_decisions else None
    reactions = 0
    for d in transition_decisions:
        if any(d < ch["decision"] <= d + 3 for ch in changes):
            reactions += 1
    changes_before = sum(1 for ch in changes if ch["phase_index"] == 0)
    increases = sum(1 for ch in changes if ch["to"] > ch["from"])
    decreases = sum(1 for ch in changes if ch["to"] < ch["from"])
    return {
        "completion_s": total_elapsed,
        "steps": steps,
        "requests": requests,
        "file_mib": env.total / MIB,
        "action_counts": action_counts,
        "effective_counts": effective_counts,
        "conc_time": conc_time,
        "final_concurrency": env.concurrency,
        "max_concurrency": env.max_conc,
        "time_weighted_concurrency": (
            sum(c * t for c, t in enumerate(conc_time)) / total_elapsed
            if total_elapsed > 0
            else 0.0
        ),
        "effective_changes": len(changes),
        "changes": changes,
        "transitions": transition_decisions,
        "first_transition_decision": first_transition,
        "changes_before_first_transition": changes_before,
        "changes_after_first_transition": len(changes) - changes_before,
        "transition_reactions_within_3": reactions,
        "increases": increases,
        "decreases": decreases,
        "constant_episode": len(changes) == 0,
        # A ramp-to-max episode only ever increases and ends at the maximum: a
        # constant-max policy in disguise, not condition-dependent adaptation.
        "ramp_to_max": decreases == 0
        and increases > 0
        and env.concurrency == MAX_CONC,
    }


def distribution(values):
    vals = list(values)
    if not vals:
        return {"n": 0}
    if len(vals) == 1:
        return {
            "n": 1,
            "mean": float(vals[0]),
            "median": float(vals[0]),
            "sd": 0.0,
            "min": float(vals[0]),
            "p25": float(vals[0]),
            "p75": float(vals[0]),
            "max": float(vals[0]),
        }
    q = st.quantiles(vals, n=4)
    return {
        "n": len(vals),
        "mean": float(st.mean(vals)),
        "median": float(st.median(vals)),
        "sd": float(st.pstdev(vals)),
        "min": float(min(vals)),
        "p25": float(q[0]),
        "p75": float(q[2]),
        "max": float(max(vals)),
    }


def summarize(rows):
    total_time = sum(r["completion_s"] for r in rows)
    conc_time = [0.0] * (MAX_CONC + 1)
    for r in rows:
        for c in range(1, MAX_CONC + 1):
            conc_time[c] += r["conc_time"][c]
    conc_frac = {
        str(c): (conc_time[c] / total_time if total_time > 0 else 0.0)
        for c in range(1, MAX_CONC + 1)
    }
    action_counts = [0] * NUM_ACTIONS
    for r in rows:
        for i, v in enumerate(r["action_counts"]):
            action_counts[i] += v
    total_decisions = sum(action_counts)
    final_dist = {}
    for r in rows:
        k = str(r["final_concurrency"])
        final_dist[k] = final_dist.get(k, 0) + 1
    return {
        "n": len(rows),
        "completion": distribution([r["completion_s"] for r in rows]),
        "mean_requests": float(st.mean([r["requests"] for r in rows])),
        "mean_steps": float(st.mean([r["steps"] for r in rows])),
        "mean_effective_changes": float(st.mean([r["effective_changes"] for r in rows])),
        "constant_episode_fraction": float(
            st.mean([1.0 if r["constant_episode"] else 0.0 for r in rows])
        ),
        "ramp_to_max_fraction": float(
            st.mean([1.0 if r["ramp_to_max"] else 0.0 for r in rows])
        ),
        "changes_before_first_transition": int(
            sum(r["changes_before_first_transition"] for r in rows)
        ),
        "changes_after_first_transition": int(
            sum(r["changes_after_first_transition"] for r in rows)
        ),
        "transition_reactions_within_3": int(
            sum(r["transition_reactions_within_3"] for r in rows)
        ),
        "transitions": int(sum(len(r["transitions"]) for r in rows)),
        "max_concurrency": int(max(r["max_concurrency"] for r in rows)),
        "time_weighted_mean_concurrency": float(
            sum(r["time_weighted_concurrency"] for r in rows) / len(rows)
        ),
        "action_fraction": (
            {ACTION_NAMES[i]: action_counts[i] / total_decisions for i in range(NUM_ACTIONS)}
            if total_decisions
            else {}
        ),
        "concurrency_time_fraction": conc_frac,
        "dominant_concurrency": max(conc_frac, key=conc_frac.get),
        "final_concurrency_distribution": final_dist,
    }


# ---------------------------------------------------------------------------
# Static concurrency sweep (TASK 4 prerequisite)
# ---------------------------------------------------------------------------
def run_static_sweep(seeds, interval):
    regimes = {}
    for regime in SWEEP_REGIMES:
        by_c = {}
        for c in (1, 2, 3, 4):
            rows = [
                rollout(make_env(regime, s, interval, fixed_c=c)) for s in seeds
            ]
            by_c[str(c)] = distribution([r["completion_s"] for r in rows])["mean"]
        best_c = min(range(1, 5), key=lambda c: by_c[str(c)])
        regimes[regime["name"]] = {
            "mean_completion_s_by_c": by_c,
            "best_c": best_c,
        }
    best_values = sorted({v["best_c"] for v in regimes.values()})
    return {
        "label": "static concurrency sweep (simulator only; prerequisite for interpreting adaptive behavior)",
        "decision_interval": interval,
        "seeds": seeds,
        "regimes": regimes,
        "distinct_best_c": best_values,
        "different_regimes_have_different_best_c": len(best_values) > 1,
        "note": (
            "Fixed-concurrency policies C=1..4 start at C and never change it. "
            "If all regimes had the same best C, adaptive concurrency would be "
            "uninformative in this simulator."
        ),
    }


# ---------------------------------------------------------------------------
# Evaluation helpers
# ---------------------------------------------------------------------------
def eval_conditions(conditions, seeds, interval, agent=None, fixed_c=None):
    rows = []
    for cond in conditions:
        for s in seeds:
            env = make_env(cond, s, interval, fixed_c=fixed_c)
            r = rollout(env, agent if fixed_c is None else None)
            r["condition"] = cond["name"]
            rows.append(r)
    return rows


def randomized_eval(agent, seeds, interval):
    rows = []
    for s in seeds:
        env = SimEnvV5(train_cfg(interval), seed=s)
        env.randomize()
        rows.append(rollout(env, agent))
    return rows


# ---------------------------------------------------------------------------
# Output helpers
# ---------------------------------------------------------------------------
def write_json(path, obj):
    with open(path, "w") as f:
        json.dump(obj, f, indent=2, sort_keys=True)
        f.write("\n")


def write_text(path, text):
    with open(path, "w") as f:
        f.write(text)


# ---------------------------------------------------------------------------
# Main pipeline
# ---------------------------------------------------------------------------
def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--out", default="testdata/diagnostics/dqn_validation_v5", help="output directory"
    )
    ap.add_argument("--episodes", type=int, default=EPISODES)
    ap.add_argument("--seeds", default=",".join(str(s) for s in DEFAULT_SEEDS))
    ap.add_argument(
        "--intervals",
        default=",".join(str(i) for i in DECISION_INTERVALS),
        help="decision-interval candidates; a single value skips selection",
    )
    ap.add_argument(
        "--interval-episodes",
        type=int,
        default=0,
        help="episodes per interval for interval selection (0 = max(300, episodes//4))",
    )
    ap.add_argument("--val-seeds", default=",".join(str(s) for s in DEFAULT_VAL_SEEDS))
    ap.add_argument("--test-seeds", default=",".join(str(s) for s in DEFAULT_TEST_SEEDS))
    ap.add_argument("--sweep-seeds", default=",".join(str(s) for s in DEFAULT_SWEEP_SEEDS))
    ap.add_argument(
        "--random-seeds", default=",".join(str(s) for s in DEFAULT_RANDOM_SEEDS)
    )
    ap.add_argument("--skip-sweep", action="store_true")
    args = ap.parse_args()

    os.makedirs(args.out, exist_ok=True)
    seeds = [int(x) for x in args.seeds.split(",") if x]
    intervals = [int(x) for x in args.intervals.split(",") if x]
    val_seeds = [int(x) for x in args.val_seeds.split(",") if x]
    test_seeds = [int(x) for x in args.test_seeds.split(",") if x]
    sweep_seeds = [int(x) for x in args.sweep_seeds.split(",") if x]
    random_seeds = [int(x) for x in args.random_seeds.split(",") if x]
    interval_episodes = args.interval_episodes or max(300, args.episodes // 4)

    # ---- TASK 4: static sweep prerequisite ----
    sweep = None
    if not args.skip_sweep:
        sweep = run_static_sweep(sweep_seeds, intervals[0])
        write_json(os.path.join(args.out, "static_concurrency_sweep_v5.json"), sweep)
        lines = [
            "static concurrency sweep v5 (simulator only; NO performance claim)",
            f"decision_interval={intervals[0]} seeds={sweep_seeds}",
            "",
            f"{'regime':<26} {'C=1':>8} {'C=2':>8} {'C=3':>8} {'C=4':>8}  best",
        ]
        for name, r in sweep["regimes"].items():
            b = r["mean_completion_s_by_c"]
            lines.append(
                f"{name:<26} {b['1']:>8.3f} {b['2']:>8.3f} {b['3']:>8.3f} {b['4']:>8.3f}  C={r['best_c']}"
            )
        lines.append("")
        lines.append(
            f"distinct best fixed C across regimes: {sweep['distinct_best_c']} "
            f"(varies={sweep['different_regimes_have_different_best_c']})"
        )
        write_text(os.path.join(args.out, "static_concurrency_sweep_v5.txt"), "\n".join(lines) + "\n")
        print(
            "sweep: distinct best fixed C =",
            sweep["distinct_best_c"],
            "(varies)" if sweep["different_regimes_have_different_best_c"] else "(constant)",
        )

    # ---- decision-interval selection on VALIDATION only ----
    interval_results = {}
    if len(intervals) > 1:
        selection_seeds = val_seeds[:2] if len(val_seeds) >= 2 else val_seeds
        for iv in intervals:
            cfg = TrainConfig(episodes=interval_episodes, seed=seeds[0], num_actions=NUM_ACTIONS)
            agent, report = train(cfg, train_cfg(iv), env_class=SimEnvV5)
            rows = eval_conditions(VALIDATION_CONDITIONS, selection_seeds, iv, agent=agent)
            summary = summarize(rows)
            interval_results[str(iv)] = {
                "mean_completion_s": summary["completion"]["mean"],
                "mean_effective_changes": summary["mean_effective_changes"],
                "constant_episode_fraction": summary["constant_episode_fraction"],
                "final_loss": report["final_loss"],
                "mean_return": report["mean_return"],
            }
            print(
                f"interval {iv}: val_mean={interval_results[str(iv)]['mean_completion_s']:.4f}s "
                f"changes/ep={summary['mean_effective_changes']:.2f}"
            )
        best_interval = min(
            intervals,
            key=lambda iv: (interval_results[str(iv)]["mean_completion_s"], iv),
        )
        write_json(
            os.path.join(args.out, "decision_interval_results_v5.json"),
            {
                "candidates": intervals,
                "interval_episodes": interval_episodes,
                "selection_seeds": selection_seeds,
                "selected": best_interval,
                "criterion": "min mean validation completion time; tie-break lower interval; test never used",
                "results": interval_results,
            },
        )
    else:
        best_interval = intervals[0]
    print(f"selected decision interval {best_interval}")

    # ---- multi-seed training + validation selection ----
    sim_cfg = train_cfg(best_interval)
    seed_results = []
    agents = {}
    for seed in seeds:
        cfg = TrainConfig(episodes=args.episodes, seed=seed, num_actions=NUM_ACTIONS)
        t0 = time.time()
        agent, report = train(cfg, sim_cfg, env_class=SimEnvV5)
        rows = eval_conditions(VALIDATION_CONDITIONS, val_seeds, best_interval, agent=agent)
        val = summarize(rows)
        seed_results.append(
            {
                "seed": seed,
                "final_loss": report["final_loss"],
                "mean_return": report["mean_return"],
                "q_min": report["q_min"],
                "q_max": report["q_max"],
                "final_epsilon": report["final_epsilon"],
                "terminal_episodes": report["terminal_episodes"],
                "truncated_episodes": report["truncated_episodes"],
                "validation_mean_completion_s": val["completion"]["mean"],
                "validation_mean_effective_changes": val["mean_effective_changes"],
                "validation_constant_episode_fraction": val["constant_episode_fraction"],
                "wall_seconds": time.time() - t0,
            }
        )
        agents[seed] = (agent, cfg, report, val)
        loss = report["final_loss"]
        loss_txt = f"{loss:.5f}" if loss is not None else "n/a (replay < train_start)"
        print(
            f"seed {seed}: val_mean={val['completion']['mean']:.4f}s "
            f"loss={loss_txt} truncated={report['truncated_episodes']}"
        )

    best_seed = min(
        seed_results, key=lambda r: (r["validation_mean_completion_s"], r["seed"])
    )["seed"]
    agent, cfg, report, val_summary = agents[best_seed]
    val_mean = val_summary["completion"]["mean"]

    # ---- episode reward/time distribution (training) ----
    training_episodes = {
        "selected_seed": best_seed,
        "episodes": args.episodes,
        "interval": best_interval,
        "episode_completion_s": distribution([-r for r in report["returns"]]),
        "episode_steps": distribution(report["episode_steps"]),
        "episode_returns": report["returns"],
        "episode_steps_list": report["episode_steps"],
    }
    write_json(os.path.join(args.out, "training_episodes_v5.json"), training_episodes)

    # ---- held-out test: DQN vs fixed C=1..4 ----
    dqn_rows = eval_conditions(TEST_CONDITIONS, test_seeds, best_interval, agent=agent)
    fixed_rows = {
        c: eval_conditions(TEST_CONDITIONS, test_seeds, best_interval, fixed_c=c)
        for c in (1, 2, 3, 4)
    }
    heldout = {
        "dqn": {"overall": summarize(dqn_rows)},
        "fixed": {f"C{c}": {"overall": summarize(fixed_rows[c])} for c in (1, 2, 3, 4)},
    }
    by_condition = {}
    dqn_beats_best_fixed = 0
    for cond in TEST_CONDITIONS:
        name = cond["name"]
        dqn_sub = [r for r in dqn_rows if r["condition"] == name]
        fixed_by_c = {
            f"C{c}": distribution(
                [r["completion_s"] for r in fixed_rows[c] if r["condition"] == name]
            )["mean"]
            for c in (1, 2, 3, 4)
        }
        best_c = min(range(1, 5), key=lambda c: fixed_by_c[f"C{c}"])
        dqn_mean = distribution([r["completion_s"] for r in dqn_sub])["mean"]
        if dqn_mean <= fixed_by_c[f"C{best_c}"]:
            dqn_beats_best_fixed += 1
        by_condition[name] = {
            "dqn": summarize(dqn_sub),
            "fixed_mean_completion_s": fixed_by_c,
            "best_fixed_c": best_c,
            "dqn_minus_best_fixed_s": dqn_mean - fixed_by_c[f"C{best_c}"],
        }
    heldout["by_condition"] = by_condition
    heldout["randomized_distribution"] = {
        "dqn": summarize(randomized_eval(agent, random_seeds, best_interval)),
        "seeds": random_seeds,
    }
    write_json(os.path.join(args.out, "heldout_test_results_v5.json"), heldout)

    # ---- behavior diagnostics + NOT-READY gate ----
    diagnostic_rows = dqn_rows
    diag = {
        "heldout_conditions": [c["name"] for c in TEST_CONDITIONS],
        "seeds": test_seeds,
        "overall": summarize(diagnostic_rows),
        "by_condition": {name: by_condition[name]["dqn"] for name in by_condition},
        "fixed_c_overall": {k: heldout["fixed"][k]["overall"] for k in heldout["fixed"]},
    }
    const_frac = diag["overall"]["constant_episode_fraction"]
    dominant_time_frac = max(diag["overall"]["concurrency_time_fraction"].values())
    total_changes = int(
        sum(r["effective_changes"] for r in diagnostic_rows)
    )
    distinct_final = len(diag["overall"]["final_concurrency_distribution"])
    dominant_by_condition = {name: by_condition[name]["dqn"]["dominant_concurrency"] for name in by_condition}
    distinct_dominant = len(set(dominant_by_condition.values()))
    ramp_frac = diag["overall"]["ramp_to_max_fraction"]
    not_ready = (
        const_frac >= 0.9
        or dominant_time_frac >= 0.9
        or ramp_frac >= 0.9
        or total_changes == 0
    )
    gate = {
        "pre_specified_rule": (
            "NOT READY if constant_episode_fraction >= 0.9 OR max single-concurrency "
            "time fraction >= 0.9 OR ramp_to_max_fraction >= 0.9 OR total effective "
            "concurrency changes == 0; a constant-concurrency policy (including a "
            "ramp-to-max-then-hold policy) is not adaptation, regardless of completion time."
        ),
        "constant_episode_fraction": const_frac,
        "ramp_to_max_fraction": ramp_frac,
        "dominant_concurrency_time_fraction": dominant_time_frac,
        "total_effective_concurrency_changes": total_changes,
        "distinct_final_concurrency_values": distinct_final,
        "dominant_concurrency_by_condition": dominant_by_condition,
        "distinct_dominant_concurrency_by_condition": distinct_dominant,
        "condition_dependent_behavior": distinct_dominant > 1,
        "dqn_matches_or_beats_best_fixed_on_conditions": dqn_beats_best_fixed,
        "verdict": "NOT_READY_CONSTANT_POLICY" if not_ready else "READY_FOR_NEXT_STAGE",
    }
    diagnostics = {"gate": gate, "behavior": diag, "static_sweep_ref": "static_concurrency_sweep_v5.json"}
    write_json(os.path.join(args.out, "diagnostics_v5.json"), diagnostics)

    # ---- export exactly one candidate ----
    # A fresh config carries no resolved spec until an environment is built, so
    # set the v5 spec explicitly; otherwise export_model would fall back to v1.
    export_sim_cfg = train_cfg(best_interval)
    export_sim_cfg.spec = default_state_spec_v5()
    candidate = export_model(
        agent,
        cfg,
        export_sim_cfg,
        label="FINAL DQN CANDIDATE v5 (concurrency-only, state spec v2; experimental)",
        action_names=list(ACTION_NAMES),
        num_actions=NUM_ACTIONS,
    )
    cand_bytes = json.dumps(candidate, indent=2, sort_keys=True).encode()
    candidate_path = os.path.join(args.out, "final_dqn_candidate_v5.json")
    with open(candidate_path, "wb") as f:
        f.write(cand_bytes)
    cand_hash = hashlib.sha256(cand_bytes).hexdigest()
    with open(os.path.join(args.out, "final_dqn_candidate_v5.sha256"), "w") as f:
        f.write(cand_hash + "\n")

    # ---- metadata ----
    write_json(
        os.path.join(args.out, "simulator_spec_v5.json"),
        {
            "label": "concurrency-focused training simulator v5 (training-only; state spec v2)",
            "fixed_chunk_bytes": FIXED_CHUNK,
            "action_space": ACTION_NAMES,
            "num_actions": NUM_ACTIONS,
            "state_spec_version": export_sim_cfg.spec.version,
            "feature_names": list(candidate["feature_names"]),
            "decision_interval": best_interval,
            "dynamics": {
                "per_request": "latency + per-extra-connection overhead + chunk/(effective per-response rate)",
                "workers": "concurrency workers run continuously; interval elapsed = sum(service_times)/active_workers",
                "shared_cap": "optional link_bandwidth caps the aggregate; extra concurrency then stops helping",
                "loss": "per-request failure probability rising with concurrency (congestion); one retransmission modeled",
                "phases": "1-3 within-episode phases with randomized wall-clock transition times",
                "step": "one RL step completes decision_interval requests; reward=-interval wall-clock seconds",
            },
            "calibration_reference": (
                "real server paces each response at `rate`; 1MiB@512KiB/s ~2.05s (frozen)"
            ),
            "train_ranges": TRAIN_RANGES,
            "static_sweep": "static_concurrency_sweep_v5.json",
        },
    )
    write_json(
        os.path.join(args.out, "train_config_v5.json"),
        {
            "train": cfg.to_json(),
            "seeds": seeds,
            "selected_seed": best_seed,
            "decision_intervals": intervals,
            "selected_decision_interval": best_interval,
            "action_space": ACTION_NAMES,
            "num_actions": NUM_ACTIONS,
            "fixed_chunk_bytes": FIXED_CHUNK,
            "state_spec_version": export_sim_cfg.spec.version,
            "train_ranges": TRAIN_RANGES,
            "validation_conditions": VALIDATION_CONDITIONS,
            "test_conditions": TEST_CONDITIONS,
            "seed_separation": {
                "train": seeds,
                "validation": val_seeds,
                "test": test_seeds,
                "sweep": sweep_seeds,
                "randomized_eval": random_seeds,
            },
            "selection_criterion": (
                "min mean validation completion time; tie-break lowest seed "
                "(validation only; test never used)"
            ),
            "python": platform.python_version(),
            "torch": torch.__version__,
            "numpy": np.__version__,
        },
    )
    write_json(os.path.join(args.out, "seed_results_v5.json"), {"seeds": seed_results, "selected_seed": best_seed})
    write_json(
        os.path.join(args.out, "validation_results_v5.json"),
        {
            "conditions": VALIDATION_CONDITIONS,
            "seeds": val_seeds,
            "selected_seed": best_seed,
            "validation_mean_completion_s": val_mean,
            "validation_summary": val_summary,
        },
    )

    report = {
        "label": "FINAL DQN CANDIDATE v5 (experimental; concurrency-only; no performance claim)",
        "candidate_sha256": cand_hash,
        "candidate_path": candidate_path,
        "selected_seed": best_seed,
        "selected_decision_interval": best_interval,
        "episodes_per_seed": args.episodes,
        "validation_mean_completion_s": val_mean,
        "heldout_overall": {
            "dqn": heldout["dqn"]["overall"],
            "fixed": {c: heldout["fixed"][c]["overall"] for c in heldout["fixed"]},
        },
        "randomized_distribution": heldout["randomized_distribution"],
        "static_sweep": sweep,
        "gate": gate,
        "live_transfer_run": False,
        "aria2_comparison_run": False,
        "no_performance_claim": True,
    }
    write_json(os.path.join(args.out, "report.json"), report)

    # ---- human-readable report ----
    lines = []
    lines.append("DQN Candidate v5 (concurrency-only, 1 MiB fixed chunk, state spec v2)")
    lines.append("EXPERIMENTAL. NO live transfer; NO aria2 comparison; NO performance claim.")
    lines.append("")
    lines.append(f"candidate sha256 : {cand_hash}")
    lines.append(f"selected seed    : {best_seed}")
    lines.append(f"decision interval: {best_interval}")
    lines.append(f"episodes/seed    : {args.episodes}")
    lines.append(f"gate verdict     : {gate['verdict']}")
    lines.append("")
    lines.append("training episode completion-time distribution (s):")
    c = training_episodes["episode_completion_s"]
    lines.append(
        f"  n={c['n']} mean={c['mean']:.3f} median={c['median']:.3f} sd={c['sd']:.3f} "
        f"min={c['min']:.3f} p25={c['p25']:.3f} p75={c['p75']:.3f} max={c['max']:.3f}"
    )
    lines.append("")
    lines.append("validation (selection only):")
    lines.append(f"  mean completion = {val_mean:.4f}s over {len(VALIDATION_CONDITIONS)} conditions x {len(val_seeds)} seeds")
    lines.append("")
    if sweep is not None:
        lines.append("static concurrency sweep (best fixed C per regime):")
        for name, r in sweep["regimes"].items():
            lines.append(f"  {name:<26} best=C{r['best_c']}")
        lines.append(
            f"  distinct best C = {sweep['distinct_best_c']} "
            f"(varies={sweep['different_regimes_have_different_best_c']})"
        )
        lines.append("")
    lines.append("held-out TEST (never used for selection): mean completion ms (DQN vs fixed C=1..4)")
    lines.append(
        f"  {'condition':<24} {'DQN':>9} {'C=1':>9} {'C=2':>9} {'C=3':>9} {'C=4':>9} {'best':>5} {'DQN-best':>9}"
    )
    for name in by_condition:
        b = by_condition[name]
        fb = b["fixed_mean_completion_s"]
        lines.append(
            f"  {name:<24} {b['dqn']['completion']['mean']*1000:>9.1f} "
            f"{fb['C1']*1000:>9.1f} {fb['C2']*1000:>9.1f} {fb['C3']*1000:>9.1f} {fb['C4']*1000:>9.1f} "
            f"C{b['best_fixed_c']:>4} {b['dqn_minus_best_fixed_s']*1000:>+9.1f}"
        )
    lines.append("")
    dqn_overall = heldout["dqn"]["overall"]
    lines.append("DQN held-out behavior (all test conditions):")
    lines.append(
        f"  mean effective concurrency changes/episode = {dqn_overall['mean_effective_changes']:.2f}"
    )
    lines.append(
        f"  constant-episode fraction = {dqn_overall['constant_episode_fraction']:.3f}"
    )
    lines.append("  action fraction      = " + ", ".join(
        f"{k}={v:.3f}" for k, v in dqn_overall["action_fraction"].items()
    ))
    lines.append("  concurrency time fraction = " + ", ".join(
        f"C{k}={v:.3f}" for k, v in dqn_overall["concurrency_time_fraction"].items()
    ))
    lines.append(
        "  final concurrency distribution = " + ", ".join(
            f"C{k}:{v}" for k, v in dqn_overall["final_concurrency_distribution"].items()
        )
    )
    lines.append(
        f"  changes before/after first transition = "
        f"{dqn_overall['changes_before_first_transition']}/"
        f"{dqn_overall['changes_after_first_transition']} "
        f"(transition reactions within 3 decisions: {dqn_overall['transition_reactions_within_3']})"
    )
    lines.append(
        f"  dominant concurrency by condition = {dominant_by_condition} "
        f"(distinct={distinct_dominant})"
    )
    lines.append("")
    lines.append("NOT-READY gate:")
    lines.append(f"  rule: {gate['pre_specified_rule']}")
    lines.append(
        f"  constant_episode_fraction={gate['constant_episode_fraction']:.3f} "
        f"ramp_to_max_fraction={gate['ramp_to_max_fraction']:.3f} "
        f"dominant_time_fraction={gate['dominant_concurrency_time_fraction']:.3f} "
        f"total_changes={gate['total_effective_concurrency_changes']}"
    )
    lines.append(f"  verdict: {gate['verdict']}")
    lines.append("")
    lines.append("Caveats: simulator-only; no live transfer; no aria2 comparison; constant")
    lines.append("behavior is NOT counted as adaptation; completion times are not a claim.")
    write_text(os.path.join(args.out, "report.txt"), "\n".join(lines) + "\n")

    print(f"selected seed {best_seed} interval {best_interval} candidate {cand_hash}")
    print(f"gate verdict: {gate['verdict']}")
    print(f"wrote {args.out}/report.json and report.txt")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
