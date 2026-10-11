"""Train, select, and freeze DQN Candidate v4: concurrency-only adaptive control.

Chunk size is FIXED at 1 MiB for every policy; the only learned variable is
concurrency (1-4). This removes the v3 sim-to-real failure (chunk rewarded in the
simulator, absent on the real server) and matches request granularity to aria2.

Pipeline: decision-interval comparison -> multi-seed training -> validation-only
selection -> held-out test vs baselines -> request-efficiency metrics ->
ablations -> freeze Candidate v4. No aria2 comparison; no performance claim.
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
from rl.model import export_model  # noqa: E402
from rl.sim_env_v4 import (
    ACTION_NAMES,
    FIXED_CHUNK,
    NUM_ACTIONS,
    SimConfig,
    SimEnv as SimEnvV4,
)  # noqa: E402

SEEDS = [0, 1, 2, 3, 4]
EPISODES = 1500
MAX_STEPS = 200

TRAIN_RANGES = {
    "rand_rate": (256 * 1024, 8 * 1024 * 1024),
    "rand_link_bandwidth": (0, 8 * 1024 * 1024),
    "rand_rate_after": (256 * 1024, 4 * 1024 * 1024),
    "rand_latency_s": (0.0, 0.08),
    "rand_loss_percent": (0.0, 4.0),
    "rand_file_size": (256 * 1024, 16 * 1024 * 1024),
    "rand_switch_after_requests": (0, 16),
    "rand_switch_after_seconds": (0.25, 2.5),
}

VALIDATION_CONDITIONS = [
    {
        "rate": 700 * 1024,
        "link_bandwidth": 700 * 1024,
        "rate_after": 3 * 1024 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 0.8,
        "latency_s": 0.09,
        "loss_percent": 0.0,
        "file_size": 4 * 1024 * 1024,
    },
    {
        "rate": 2 * 1024 * 1024,
        "link_bandwidth": 2 * 1024 * 1024,
        "rate_after": 512 * 1024,
        "switch_kind": "request-count",
        "switch_after_requests": 8,
        "latency_s": 0.0,
        "loss_percent": 4.5,
        "file_size": 4 * 1024 * 1024,
    },
    {
        "rate": 5 * 1024 * 1024,
        "link_bandwidth": 0,
        "rate_after": 1024 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 0.4,
        "latency_s": 0.05,
        "loss_percent": 2.5,
        "file_size": 8 * 1024 * 1024,
    },
    {
        "rate": 3 * 1024 * 1024,
        "link_bandwidth": 0,
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
        "rate": 384 * 1024,
        "link_bandwidth": 0,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 4 * 1024 * 1024,
    },
    {
        "name": "unseen-high-bandwidth",
        "rate": 12 * 1024 * 1024,
        "link_bandwidth": 0,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 8 * 1024 * 1024,
    },
    {
        "name": "unseen-latency",
        "rate": 4 * 1024 * 1024,
        "link_bandwidth": 0,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.14,
        "loss_percent": 0.0,
        "file_size": 4 * 1024 * 1024,
    },
    {
        "name": "unseen-loss",
        "rate": 4 * 1024 * 1024,
        "link_bandwidth": 0,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.0,
        "loss_percent": 7.0,
        "file_size": 4 * 1024 * 1024,
    },
    {
        "name": "unseen-latency+loss",
        "rate": 3 * 1024 * 1024,
        "link_bandwidth": 0,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.1,
        "loss_percent": 5.0,
        "file_size": 8 * 1024 * 1024,
    },
    {
        "name": "unseen-throttle-up-wallclock",
        "rate": 256 * 1024,
        "link_bandwidth": 0,
        "rate_after": 10 * 1024 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 1.5,
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 8 * 1024 * 1024,
    },
    {
        "name": "unseen-throttle-down-wallclock",
        "rate": 10 * 1024 * 1024,
        "link_bandwidth": 0,
        "rate_after": 512 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 0.3,
        "latency_s": 0.0,
        "loss_percent": 1.0,
        "file_size": 8 * 1024 * 1024,
    },
    {
        "name": "unseen-combined",
        "rate": 2 * 1024 * 1024,
        "link_bandwidth": 2 * 1024 * 1024,
        "rate_after": 8 * 1024 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 0.6,
        "latency_s": 0.12,
        "loss_percent": 6.0,
        "file_size": 8 * 1024 * 1024,
    },
]


def make_env(cond, seed, interval):
    cfg = SimConfig(
        file_size=cond["file_size"],
        rate=cond["rate"],
        link_bandwidth=cond.get("link_bandwidth", 0),
        rate_after=cond.get("rate_after", 0),
        switch_kind=cond.get("switch_kind", "none"),
        switch_after_requests=cond.get("switch_after_requests", 0),
        switch_after_seconds=cond.get("switch_after_seconds", 0.0),
        latency_s=cond["latency_s"],
        loss_percent=cond["loss_percent"],
        decision_interval=interval,
        max_episode_steps=MAX_STEPS,
    )
    return SimEnvV4(cfg, seed=seed)


def train_cfg(interval):
    return SimConfig(
        decision_interval=interval, max_episode_steps=MAX_STEPS, **TRAIN_RANGES
    )


def dqn_rollout(agent, cond, seed, interval):
    return _rollout(make_env(cond, seed, interval), "dqn", agent)


def scripted_rollout(cond, seed, interval, kind):
    return _rollout(make_env(cond, seed, interval), kind)


def _rollout(env, kind, agent=None):
    env.reset()
    total = 0.0
    steps = 0
    conc_changes = 0
    prev_thr = None
    while not env.done() and steps < MAX_STEPS + 5:
        c0 = env.concurrency
        if kind == "dqn":
            a = agent.greedy(env.state())
        elif kind == "fixed" or prev_thr is None:
            a = 0
        else:
            thr = env._aggregate()
            change = (thr - prev_thr) / prev_thr if prev_thr else 0.0
            if change >= 0.10:
                a = 1
            elif change <= -0.10:
                a = 2
            else:
                a = 0
        prev_thr = env._aggregate()
        res = env.step(a)
        total += res["elapsed"]
        if env.concurrency != c0:
            conc_changes += 1
        steps += 1
    mib = env.total / (1024 * 1024)
    return {
        "completion_s": total,
        "steps": steps,
        "requests": env.requests,
        "requests_per_mib": env.requests / mib if mib else 0,
        "conc_changes": conc_changes,
        "effective_actions_per_mib": env.effective_actions / mib if mib else 0,
        "max_concurrency": env.max_conc,
        "avg_concurrency": env.conc_sum / env.conc_samples if env.conc_samples else 0,
    }


POLICIES = {
    "fixed": lambda c, s, iv: scripted_rollout(c, s, iv, "fixed"),
    "concurrency-adaptive": lambda c, s, iv: scripted_rollout(c, s, iv, "concurrency"),
}


def eval_conditions(conditions, fn, seeds, interval):
    rows = []
    for cond in conditions:
        for s in seeds:
            r = fn(cond, s, interval)
            r["condition"] = cond.get("name", str(cond["rate"]))
            rows.append(r)
    return rows


def summarize(rows):
    comp = [r["completion_s"] for r in rows]
    return {
        "n": len(rows),
        "mean_completion_s": float(st.mean(comp)),
        "median_completion_s": float(st.median(comp)),
        "sd_completion_s": float(st.pstdev(comp)) if len(comp) > 1 else 0.0,
        "mean_requests": float(st.mean([r["requests"] for r in rows])),
        "mean_requests_per_mib": float(st.mean([r["requests_per_mib"] for r in rows])),
        "mean_effective_actions_per_mib": float(
            st.mean([r["effective_actions_per_mib"] for r in rows])
        ),
        "mean_conc_changes": float(st.mean([r["conc_changes"] for r in rows])),
        "mean_max_concurrency": float(st.mean([r["max_concurrency"] for r in rows])),
        "mean_avg_concurrency": float(st.mean([r["avg_concurrency"] for r in rows])),
    }


def validation_mean(agent, interval):
    rows = eval_conditions(
        VALIDATION_CONDITIONS,
        lambda c, s, iv: dqn_rollout(agent, c, s, iv),
        [100, 101, 102],
        interval,
    )
    return summarize(rows)["mean_completion_s"], summarize(rows)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="testdata/diagnostics/dqn_validation_v4")
    ap.add_argument("--episodes", type=int, default=EPISODES)
    ap.add_argument("--intervals", default="2,4,8")
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)

    intervals = [int(x) for x in args.intervals.split(",")]

    # ---- decision-interval experiment ----
    interval_results = {}
    for iv in intervals:
        cfg = TrainConfig(
            episodes=max(400, args.episodes // 3), seed=0, num_actions=NUM_ACTIONS
        )
        sim = train_cfg(iv)
        agent, report = train(cfg, sim, env_class=SimEnvV4)
        rows = eval_conditions(
            VALIDATION_CONDITIONS,
            lambda c, s, i: dqn_rollout(agent, c, s, i),
            [100, 101],
            iv,
        )
        interval_results[str(iv)] = {
            **summarize(rows),
            "final_loss": report["final_loss"],
            "mean_return": report["mean_return"],
        }
        print(
            f"interval {iv}: val_mean={interval_results[str(iv)]['mean_completion_s']:.4f} "
            f"req/MiB={interval_results[str(iv)]['mean_requests_per_mib']:.1f} "
            f"churn/MiB={interval_results[str(iv)]['mean_effective_actions_per_mib']:.2f}"
        )

    best_iv = min(
        intervals,
        key=lambda iv: (
            interval_results[str(iv)]["mean_completion_s"],
            interval_results[str(iv)]["mean_effective_actions_per_mib"],
        ),
    )
    print(f"selected decision interval {best_iv}")

    # ---- multi-seed training ----
    sim_cfg = train_cfg(best_iv)
    cfg_base = TrainConfig(episodes=args.episodes, seed=0, num_actions=NUM_ACTIONS)
    seed_results = []
    agents = {}
    for seed in SEEDS:
        cfg = TrainConfig(episodes=args.episodes, seed=seed, num_actions=NUM_ACTIONS)
        t0 = time.time()
        agent, report = train(cfg, sim_cfg, env_class=SimEnvV4)
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
                    lambda c, s, i: dqn_rollout(agent, c, s, i),
                    test_seeds,
                    best_iv,
                )
            )
        }
    }
    for name, fn in POLICIES.items():
        heldout[name] = {
            "overall": summarize(
                eval_conditions(TEST_CONDITIONS, fn, test_seeds, best_iv)
            )
        }
    for name in heldout:
        fn = (
            (lambda c, s, i: dqn_rollout(agent, c, s, i))
            if name == "dqn"
            else POLICIES[name]
        )
        rows = eval_conditions(TEST_CONDITIONS, fn, test_seeds, best_iv)
        heldout[name]["by_condition"] = {
            c["name"]: summarize([r for r in rows if r["condition"] == c["name"]])
            for c in TEST_CONDITIONS
        }

    # ---- ablations ----
    ablations = {"dqn_final": heldout["dqn"]["overall"]}

    def rand_pol(c, s, iv):
        env = make_env(c, s, iv)
        env.reset()
        rng = _r.Random(s * 7 + 1)
        total, steps = 0.0, 0
        while not env.done() and steps < MAX_STEPS + 5:
            total += env.step(rng.randrange(NUM_ACTIONS))["elapsed"]
            steps += 1
        return {
            "completion_s": total,
            "steps": steps,
            "requests": env.requests,
            "requests_per_mib": env.requests / (env.total / (1024 * 1024)),
            "conc_changes": 0,
            "effective_actions_per_mib": 0,
            "max_concurrency": env.max_conc,
            "avg_concurrency": 0,
        }

    def shuffled(c, s, iv):
        env = make_env(c, s, iv)
        env.reset()
        rng = _r.Random(s)
        total, steps = 0.0, 0
        while not env.done() and steps < MAX_STEPS + 5:
            _ = agent.greedy(env.state())
            total += env.step(rng.randrange(NUM_ACTIONS))["elapsed"]
            steps += 1
        return {
            "completion_s": total,
            "steps": steps,
            "requests": env.requests,
            "requests_per_mib": env.requests / (env.total / (1024 * 1024)),
            "conc_changes": 0,
            "effective_actions_per_mib": 0,
            "max_concurrency": env.max_conc,
            "avg_concurrency": 0,
        }

    def corrupt(c, s, iv):
        env = make_env(c, s, iv)
        env.reset()
        total, steps = 0.0, 0
        while not env.done() and steps < MAX_STEPS + 5:
            x = list(env.state())
            for i in range(3, len(x)):
                x[i] = 0.0
            with torch.no_grad():
                q = agent.online(torch.tensor(x, dtype=torch.float32).unsqueeze(0))
            total += env.step(int(torch.argmax(q, dim=1).item()))["elapsed"]
            steps += 1
        return {
            "completion_s": total,
            "steps": steps,
            "requests": env.requests,
            "requests_per_mib": env.requests / (env.total / (1024 * 1024)),
            "conc_changes": 0,
            "effective_actions_per_mib": 0,
            "max_concurrency": env.max_conc,
            "avg_concurrency": 0,
        }

    ablations["random_policy"] = summarize(
        eval_conditions(TEST_CONDITIONS, rand_pol, test_seeds, best_iv)
    )
    ablations["shuffled_actions"] = summarize(
        eval_conditions(TEST_CONDITIONS, shuffled, test_seeds, best_iv)
    )
    ablations["corrupted_state"] = summarize(
        eval_conditions(TEST_CONDITIONS, corrupt, test_seeds, best_iv)
    )

    # ---- freeze ----
    candidate = export_model(
        agent,
        cfg,
        sim_cfg,
        label="FINAL DQN CANDIDATE v4",
        action_names=list(ACTION_NAMES),
        num_actions=NUM_ACTIONS,
    )
    cand_bytes = json.dumps(candidate, indent=2, sort_keys=True).encode()
    with open(os.path.join(args.out, "final_dqn_candidate_v4.json"), "wb") as f:
        f.write(cand_bytes)
    cand_hash = hashlib.sha256(cand_bytes).hexdigest()
    with open(os.path.join(args.out, "final_dqn_candidate_v4.sha256"), "w") as f:
        f.write(cand_hash + "\n")

    write_json(
        os.path.join(args.out, "simulator_spec_v4.json"),
        {
            "label": "concurrency-focused training simulator v4 (training-only)",
            "fixed_chunk_bytes": FIXED_CHUNK,
            "action_space": ACTION_NAMES,
            "decision_interval": best_iv,
            "dynamics": {
                "per_request": "latency + chunk/current_rate (independent of concurrency)",
                "parallel_wave": "wave wall-clock = slowest of `concurrency` parallel requests; aggregate throughput = bytes/elapsed",
                "shared_cap": "optional link_bandwidth caps current_rate/concurrency, so extra concurrency can stop helping",
                "throttles": ["none", "request-count", "wall-clock"],
                "step": "one RL step = decision_interval requests; reward=-interval wall-clock seconds",
            },
            "calibration_reference": "real server paces each response at `rate`; 1MiB@512KiB/s ~2.05s, 32KiB@512KiB/s ~0.064s (frozen)",
            "train_ranges": TRAIN_RANGES,
        },
    )
    write_json(
        os.path.join(args.out, "train_config_v4.json"),
        {
            "train": cfg_base.to_json(),
            "seeds": SEEDS,
            "decision_interval": best_iv,
            "action_space": ACTION_NAMES,
            "fixed_chunk_bytes": FIXED_CHUNK,
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
        os.path.join(args.out, "decision_interval_results_v4.json"),
        {"candidates": intervals, "selected": best_iv, "results": interval_results},
    )
    write_json(
        os.path.join(args.out, "seed_results_v4.json"),
        {"seeds": seed_results, "selected_seed": best_seed},
    )
    write_json(
        os.path.join(args.out, "validation_results_v4.json"),
        {
            "conditions": VALIDATION_CONDITIONS,
            "selected_seed": best_seed,
            "validation_mean_completion_s": val_mean,
            "validation_summary": val_summary,
        },
    )
    write_json(os.path.join(args.out, "heldout_test_results_v4.json"), heldout)
    write_json(os.path.join(args.out, "ablations_v4.json"), ablations)

    report = {
        "label": "FINAL DQN CANDIDATE v4 (adaptive-concurrency formulation; no performance claim)",
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
            "DQN Candidate v4 (concurrency-only, 1 MiB fixed chunk). NO performance claim; NO aria2.\n\n"
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
        f.write(
            "\nheld-out TEST (mean completion s / req per MiB / avg concurrency):\n"
        )
        for k in heldout:
            o = heldout[k]["overall"]
            f.write(
                f"  {k:<22} mean={o['mean_completion_s']:.4f} req/MiB={o['mean_requests_per_mib']:.1f} "
                f"avgC={o['mean_avg_concurrency']:.2f} maxC={o['mean_max_concurrency']:.2f} churn/MiB={o['mean_effective_actions_per_mib']:.2f}\n"
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
