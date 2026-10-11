"""Train, select, and freeze DQN Candidate v3 (request-efficiency correction).

v3 keeps the Double DQN architecture but changes the control problem:
  * 6 discrete chunk levels up to 1 MiB,
  * a decision interval (completed requests per RL step),
  * a request-cost model so fragmentation is genuinely costly.

Pipeline: decision-interval comparison -> multi-seed training -> validation-only
selection -> held-out test vs baselines -> request-efficiency metrics ->
ablations -> freeze Candidate v3. No aria2 comparison; no performance claim.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import random as _r
import statistics as st
import sys
import time

import numpy as np
import torch

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.dqn import TrainConfig, train  # noqa: E402
from rl.sim_env_v3 import SimEnv as SimEnvV3  # noqa: E402
from rl.model import export_model  # noqa: E402
from rl.sim_env_v3 import (
    CHUNK_LEVELS,
    NUM_ACTIONS,
    SimConfig,
    SimEnv,
    chunk_index,
)  # noqa: E402

SEEDS = [0, 1, 2, 3, 4]
EPISODES = 1500
MAX_STEPS = 200
DECISION_INTERVAL = 4

TRAIN_RANGES = {
    "rand_link_bandwidth": (256 * 1024, 8 * 1024 * 1024),
    "rand_per_connection": (256 * 1024, 8 * 1024 * 1024),
    "rand_rate_after": (256 * 1024, 4 * 1024 * 1024),
    "rand_latency_s": (0.0, 0.08),
    "rand_loss_percent": (0.0, 4.0),
    "rand_file_size": (256 * 1024, 16 * 1024 * 1024),
    "rand_switch_after_requests": (0, 16),
    "rand_switch_after_seconds": (0.25, 2.5),
}

VALIDATION_CONDITIONS = [
    {
        "link_bandwidth": 700 * 1024,
        "per_connection_rate": 700 * 1024,
        "rate_after": 3 * 1024 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 0.8,
        "latency_s": 0.09,
        "loss_percent": 0.0,
        "file_size": 4 * 1024 * 1024,
    },
    {
        "link_bandwidth": 2 * 1024 * 1024,
        "per_connection_rate": 1 * 1024 * 1024,
        "rate_after": 512 * 1024,
        "switch_kind": "request-count",
        "switch_after_requests": 8,
        "latency_s": 0.0,
        "loss_percent": 4.5,
        "file_size": 4 * 1024 * 1024,
    },
    {
        "link_bandwidth": 5 * 1024 * 1024,
        "per_connection_rate": 2 * 1024 * 1024,
        "rate_after": 1024 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 0.4,
        "latency_s": 0.05,
        "loss_percent": 2.5,
        "file_size": 8 * 1024 * 1024,
    },
    {
        "link_bandwidth": 3 * 1024 * 1024,
        "per_connection_rate": 1 * 1024 * 1024,
        "rate_after": 3 * 1024 * 1024,
        "switch_kind": "none",
        "latency_s": 0.02,
        "loss_percent": 0.0,
        "file_size": 4 * 1024 * 1024,
    },
]

TEST_CONDITIONS = [
    {
        "name": "unseen-low-bandwidth",
        "link_bandwidth": 384 * 1024,
        "per_connection_rate": 384 * 1024,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 4 * 1024 * 1024,
    },
    {
        "name": "unseen-high-bandwidth",
        "link_bandwidth": 12 * 1024 * 1024,
        "per_connection_rate": 3 * 1024 * 1024,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 8 * 1024 * 1024,
    },
    {
        "name": "unseen-latency",
        "link_bandwidth": 4 * 1024 * 1024,
        "per_connection_rate": 2 * 1024 * 1024,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.14,
        "loss_percent": 0.0,
        "file_size": 4 * 1024 * 1024,
    },
    {
        "name": "unseen-loss",
        "link_bandwidth": 4 * 1024 * 1024,
        "per_connection_rate": 2 * 1024 * 1024,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.0,
        "loss_percent": 7.0,
        "file_size": 4 * 1024 * 1024,
    },
    {
        "name": "unseen-latency+loss",
        "link_bandwidth": 3 * 1024 * 1024,
        "per_connection_rate": 1 * 1024 * 1024,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.1,
        "loss_percent": 5.0,
        "file_size": 8 * 1024 * 1024,
    },
    {
        "name": "unseen-throttle-up-wallclock",
        "link_bandwidth": 256 * 1024,
        "per_connection_rate": 256 * 1024,
        "rate_after": 10 * 1024 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 1.5,
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 8 * 1024 * 1024,
    },
    {
        "name": "unseen-throttle-down-wallclock",
        "link_bandwidth": 10 * 1024 * 1024,
        "per_connection_rate": 4 * 1024 * 1024,
        "rate_after": 512 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 0.3,
        "latency_s": 0.0,
        "loss_percent": 1.0,
        "file_size": 8 * 1024 * 1024,
    },
    {
        "name": "unseen-combined",
        "link_bandwidth": 2 * 1024 * 1024,
        "per_connection_rate": 1 * 1024 * 1024,
        "rate_after": 8 * 1024 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 0.6,
        "latency_s": 0.12,
        "loss_percent": 6.0,
        "file_size": 8 * 1024 * 1024,
    },
]


def make_env(cond, seed, interval=DECISION_INTERVAL):
    cfg = SimConfig(
        file_size=cond["file_size"],
        link_bandwidth=cond["link_bandwidth"],
        per_connection_rate=cond["per_connection_rate"],
        rate_after=cond.get("rate_after", 0),
        switch_kind=cond.get("switch_kind", "none"),
        switch_after_requests=cond.get("switch_after_requests", 0),
        switch_after_seconds=cond.get("switch_after_seconds", 0.0),
        latency_s=cond["latency_s"],
        loss_percent=cond["loss_percent"],
        decision_interval=interval,
        max_episode_steps=MAX_STEPS,
    )
    return SimEnv(cfg, seed=seed)


def train_cfg(interval=DECISION_INTERVAL):
    return SimConfig(
        decision_interval=interval, max_episode_steps=MAX_STEPS, **TRAIN_RANGES
    )


# ---------- rollouts ----------


def dqn_rollout(agent, cond, seed, interval=DECISION_INTERVAL):
    return _rollout(make_env(cond, seed, interval), "dqn", agent=agent)


def scripted_rollout(cond, seed, kind, interval=DECISION_INTERVAL):
    return _rollout(make_env(cond, seed, interval), kind)


def _rollout(env, kind, agent=None):
    env.reset()
    total = 0.0
    steps = 0
    conc_changes = chunk_changes = 0
    prev_thr = None
    prefer_conc = True
    while not env.done() and steps < MAX_STEPS + 5:
        c0, k0 = env.concurrency, env.chunk_idx
        if kind == "dqn":
            a = agent.greedy(env.state())
        elif kind == "fixed" or prev_thr is None:
            a = 0
        else:
            thr = env._aggregate()
            change = (thr - prev_thr) / prev_thr if prev_thr else 0.0
            if change >= 0.10:
                a = _action(kind, True, prefer_conc)
                if kind == "joint":
                    prefer_conc = not prefer_conc
            elif change <= -0.10:
                a = _action(kind, False, prefer_conc)
                if kind == "joint":
                    prefer_conc = not prefer_conc
            else:
                a = 0
        prev_thr = env._aggregate()
        res = env.step(a)
        total += res["elapsed"]
        if env.concurrency != c0:
            conc_changes += 1
        if env.chunk_idx != k0:
            chunk_changes += 1
        steps += 1
    mib = env.total / (1024 * 1024)
    return {
        "completion_s": total,
        "steps": steps,
        "requests": env.requests,
        "requests_per_mib": env.requests / mib if mib else 0,
        "conc_changes": conc_changes,
        "chunk_changes": chunk_changes,
        "effective_actions_per_mib": env.effective_actions / mib if mib else 0,
        "max_concurrency": env.max_conc,
        "avg_concurrency": env.conc_sum / env.conc_samples if env.conc_samples else 0,
        "avg_chunk_kib": (
            st.mean(
                [
                    c / 1024
                    for c in CHUNK_LEVELS
                    if env.chunk_time[CHUNK_LEVELS.index(c)] > 0
                ]
            )
            if any(env.chunk_time)
            else 0
        ),
        "chunk_time_fractions": [
            t / env.elapsed if env.elapsed else 0 for t in env.chunk_time
        ],
    }


def _action(kind, improve, prefer_conc):
    if kind == "concurrency":
        return 1 if improve else 2
    if kind == "chunk":
        return 3 if improve else 4
    if improve:
        return 1 if prefer_conc else 3
    return 2 if prefer_conc else 4


POLICIES = {
    "fixed": lambda c, s: scripted_rollout(c, s, "fixed"),
    "concurrency-adaptive": lambda c, s: scripted_rollout(c, s, "concurrency"),
    "chunk-size-adaptive": lambda c, s: scripted_rollout(c, s, "chunk"),
    "joint-adaptive": lambda c, s: scripted_rollout(c, s, "joint"),
}


def eval_conditions(conditions, fn, seeds):
    rows = []
    for cond in conditions:
        for s in seeds:
            r = fn(cond, s)
            r["condition"] = cond.get("name", str(cond["link_bandwidth"]))
            rows.append(r)
    return rows


def summarize(rows):
    comp = [r["completion_s"] for r in rows]
    return {
        "n": len(rows),
        "mean_completion_s": float(st.mean(comp)),
        "median_completion_s": float(st.median(comp)),
        "sd_completion_s": float(st.pstdev(comp)) if len(comp) > 1 else 0.0,
        "mean_requests_per_mib": float(st.mean([r["requests_per_mib"] for r in rows])),
        "mean_effective_actions_per_mib": float(
            st.mean([r["effective_actions_per_mib"] for r in rows])
        ),
        "mean_conc_changes": float(st.mean([r["conc_changes"] for r in rows])),
        "mean_chunk_changes": float(st.mean([r["chunk_changes"] for r in rows])),
        "mean_max_concurrency": float(st.mean([r["max_concurrency"] for r in rows])),
        "mean_avg_concurrency": float(st.mean([r["avg_concurrency"] for r in rows])),
    }


def validation_mean(agent, interval=DECISION_INTERVAL):
    rows = eval_conditions(
        VALIDATION_CONDITIONS,
        lambda c, s: dqn_rollout(agent, c, s, interval),
        [100, 101, 102],
    )
    return summarize(rows)["mean_completion_s"], summarize(rows)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="testdata/diagnostics/dqn_validation_v3")
    ap.add_argument("--episodes", type=int, default=EPISODES)
    ap.add_argument("--intervals", default="1,4,8")
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)

    intervals = [int(x) for x in args.intervals.split(",")]

    # ---- decision-interval experiment (simulation only, 1 seed each) ----
    interval_results = {}
    for iv in intervals:
        cfg = TrainConfig(episodes=max(400, args.episodes // 3), seed=0)
        sim = SimConfig(
            decision_interval=iv, max_episode_steps=MAX_STEPS, **TRAIN_RANGES
        )
        agent, report = train(cfg, sim, env_class=SimEnvV3)
        rows = eval_conditions(
            VALIDATION_CONDITIONS, lambda c, s: dqn_rollout(agent, c, s, iv), [100, 101]
        )
        interval_results[str(iv)] = {
            **summarize(rows),
            "final_loss": report["final_loss"],
            "mean_return": report["mean_return"],
        }
        print(
            f"interval {iv}: val_mean={interval_results[str(iv)]['mean_completion_s']:.4f} "
            f"req/MiB={interval_results[str(iv)]['mean_requests_per_mib']:.1f}"
        )

    # choose the interval with the lowest validation mean completion (tie-break: fewer requests/MiB)
    best_iv = min(
        intervals,
        key=lambda iv: (
            interval_results[str(iv)]["mean_completion_s"],
            interval_results[str(iv)]["mean_requests_per_mib"],
        ),
    )
    print(f"selected decision interval {best_iv}")

    # ---- multi-seed training at the selected interval ----
    sim_cfg = train_cfg(best_iv)
    cfg_base = TrainConfig(episodes=args.episodes, seed=0)
    seed_results = []
    agents = {}
    for seed in SEEDS:
        cfg = TrainConfig(episodes=args.episodes, seed=seed)
        t0 = time.time()
        agent, report = train(cfg, sim_cfg, env_class=SimEnvV3)
        val_mean, val_summary = validation_mean(agent, best_iv)
        seed_results.append(
            {
                "seed": seed,
                "final_loss": report["final_loss"],
                "mean_return": report["mean_return"],
                "q_min": report["q_min"],
                "q_max": report["q_max"],
                "final_epsilon": report["final_epsilon"],
                "validation_mean_completion_s": val_mean,
                "wall_seconds": time.time() - t0,
            }
        )
        agents[seed] = (agent, cfg, val_mean)
        print(f"seed {seed}: val_mean={val_mean:.4f} loss={report['final_loss']}")

    best_seed = min(
        seed_results, key=lambda r: (r["validation_mean_completion_s"], r["seed"])
    )["seed"]
    agent, cfg, val = agents[best_seed]
    val_mean, val_summary = validation_mean(agent, best_iv)

    # ---- held-out test ----
    test_seeds = [200, 201, 202, 203]
    heldout = {
        "dqn": {
            "overall": summarize(
                eval_conditions(
                    TEST_CONDITIONS,
                    lambda c, s: dqn_rollout(agent, c, s, best_iv),
                    test_seeds,
                )
            )
        }
    }
    for name, fn in POLICIES.items():
        heldout[name] = {
            "overall": summarize(eval_conditions(TEST_CONDITIONS, fn, test_seeds))
        }
    for name in heldout:
        fn = (
            (lambda c, s: dqn_rollout(agent, c, s, best_iv))
            if name == "dqn"
            else POLICIES[name]
        )
        rows = eval_conditions(TEST_CONDITIONS, fn, test_seeds)
        heldout[name]["by_condition"] = {
            c["name"]: summarize([r for r in rows if r["condition"] == c["name"]])
            for c in TEST_CONDITIONS
        }

    # ---- ablations: decision interval small vs large ----
    ablations = {}
    for iv in (1, 8):
        abl_agent = agents[best_seed][
            0
        ]  # same trained model, evaluated with different step intervals is not valid;
        # instead train a dedicated ablation model per interval (1 seed) to isolate the formulation effect.
        acfg = TrainConfig(episodes=max(400, args.episodes // 3), seed=0)
        asim = SimConfig(
            decision_interval=iv, max_episode_steps=MAX_STEPS, **TRAIN_RANGES
        )
        aagent, _ = train(acfg, asim, env_class=SimEnvV3)
        rows = eval_conditions(
            TEST_CONDITIONS, lambda c, s: dqn_rollout(aagent, c, s, iv), test_seeds
        )
        ablations[f"decision_interval_{iv}"] = summarize(rows)
    ablations["dqn_final"] = heldout["dqn"]["overall"]

    # ---- freeze ----
    candidate = export_model(agent, cfg, sim_cfg, label="FINAL DQN CANDIDATE v3")
    cand_bytes = json.dumps(candidate, indent=2, sort_keys=True).encode()
    with open(os.path.join(args.out, "final_dqn_candidate_v3.json"), "wb") as f:
        f.write(cand_bytes)
    cand_hash = hashlib.sha256(cand_bytes).hexdigest()
    with open(os.path.join(args.out, "final_dqn_candidate_v3.sha256"), "w") as f:
        f.write(cand_hash + "\n")

    # ---- artifacts ----
    write_json(
        os.path.join(args.out, "simulator_spec_v3.json"),
        {
            "label": "corrected training simulator v3 (training-only)",
            "chunk_levels_kib": [c // 1024 for c in CHUNK_LEVELS],
            "decision_interval": best_iv,
            "dynamics": {
                "request_cost": "latency_s + overhead_s + chunk/per_request_rate",
                "shared_link": "per-request rate = min(current_rate/concurrency, per_connection_rate)",
                "throttles": ["none", "request-count", "wall-clock"],
                "step": "one RL step = decision_interval completed requests; reward=-interval wall-clock seconds",
            },
            "train_ranges": TRAIN_RANGES,
        },
    )
    write_json(
        os.path.join(args.out, "train_config_v3.json"),
        {
            "train": cfg_base.to_json(),
            "seeds": SEEDS,
            "decision_interval": best_iv,
            "train_ranges": TRAIN_RANGES,
            "validation_conditions": VALIDATION_CONDITIONS,
            "test_conditions": TEST_CONDITIONS,
            "selection_criterion": "min mean validation completion time; tie-break lowest seed (test never used)",
            "python": platform.python_version(),
            "torch": torch.__version__,
            "numpy": np.__version__,
        },
    )
    write_json(
        os.path.join(args.out, "decision_interval_results.json"),
        {"candidates": intervals, "selected": best_iv, "results": interval_results},
    )
    write_json(
        os.path.join(args.out, "seed_results_v3.json"),
        {"seeds": seed_results, "selected_seed": best_seed},
    )
    write_json(
        os.path.join(args.out, "validation_results_v3.json"),
        {
            "conditions": VALIDATION_CONDITIONS,
            "selected_seed": best_seed,
            "validation_mean_completion_s": val_mean,
            "validation_summary": val_summary,
        },
    )
    write_json(os.path.join(args.out, "heldout_test_results_v3.json"), heldout)
    write_json(os.path.join(args.out, "ablations_v3.json"), ablations)

    report = {
        "label": "FINAL DQN CANDIDATE v3 (request-efficiency correction; no performance claim)",
        "candidate_sha256": cand_hash,
        "selected_seed": best_seed,
        "decision_interval": best_iv,
        "validation_mean_completion_s": val_mean,
        "heldout_overall": {k: heldout[k]["overall"] for k in heldout},
        "ablations": ablations,
        "no_performance_claim": True,
    }
    write_json(os.path.join(args.out, "report.json"), report)
    with open(os.path.join(args.out, "report.txt"), "w") as f:
        f.write(
            "DQN Candidate v3 (request-efficiency correction). NO performance claim; NO aria2.\n\n"
        )
        f.write(f"decision interval candidates={intervals} selected={best_iv}\n")
        f.write(f"seeds={SEEDS} episodes={args.episodes} selected_seed={best_seed}\n")
        f.write(
            f"validation mean completion={val_mean:.4f}s candidate sha256={cand_hash}\n\n"
        )
        f.write("decision interval results:\n")
        for iv in intervals:
            r = interval_results[str(iv)]
            f.write(
                f"  interval {iv}: mean={r['mean_completion_s']:.4f}s req/MiB={r['mean_requests_per_mib']:.1f} churn/MiB={r['mean_effective_actions_per_mib']:.2f}\n"
            )
        f.write("\nheld-out TEST (mean completion s / requests per MiB):\n")
        for k in heldout:
            o = heldout[k]["overall"]
            f.write(
                f"  {k:<22} mean={o['mean_completion_s']:.4f} req/MiB={o['mean_requests_per_mib']:.1f} concChg={o['mean_conc_changes']:.2f}\n"
            )
        f.write("\nablations (mean completion s / req/MiB):\n")
        for k, o in ablations.items():
            f.write(
                f"  {k:<22} mean={o['mean_completion_s']:.4f} req/MiB={o['mean_requests_per_mib']:.1f}\n"
            )
    print(f"selected seed {best_seed} interval {best_iv} candidate {cand_hash}")
    return 0


def write_json(path, obj):
    with open(path, "w") as f:
        json.dump(obj, f, indent=2, sort_keys=True)
        f.write("\n")


if __name__ == "__main__":
    raise SystemExit(main())
