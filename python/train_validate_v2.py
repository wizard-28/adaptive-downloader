"""Train, select, and freeze DQN Candidate v2 on the CORRECTED simulator.

v2 corrects the v1 environment:
  * shared-bandwidth concurrency dynamics (concurrency changes aggregate rate),
  * per-request overhead giving chunk size a real tradeoff,
  * both request-count and wall-clock throttles on a continuous clock.

Pipeline: multi-seed training on random conditions -> validation-only selection
-> deterministic held-out test with scripted baselines -> ablations -> freeze.
No aria2 comparison; no performance claim; the test set is never used for tuning.
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

from rl.dqn import DoubleDQN, TrainConfig, train  # noqa: E402
from rl.model import export_model  # noqa: E402
from rl.sim_env import NUM_ACTIONS, SimConfig, SimEnv  # noqa: E402

SEEDS = [0, 1, 2, 3, 4]
TRAIN_EPISODES = 1200
MAX_EPISODE_STEPS = 150

TRAIN_RANGES = {
    "rand_link_bandwidth": (512 * 1024, 8 * 1024 * 1024),
    "rand_per_connection": (512 * 1024, 8 * 1024 * 1024),
    "rand_rate_after": (256 * 1024, 4 * 1024 * 1024),
    "rand_latency_s": (0.0, 0.08),
    "rand_loss_percent": (0.0, 4.0),
    "rand_file_size": (256 * 1024, 4 * 1024 * 1024),
    "rand_switch_after_requests": (0, 12),
    "rand_switch_after_seconds": (0.2, 2.0),
}

# Validation: distinct combinations including wall-clock transitions.
VALIDATION_CONDITIONS = [
    {
        "link_bandwidth": 700 * 1024,
        "per_connection_rate": 700 * 1024,
        "rate_after": 3 * 1024 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 0.8,
        "latency_s": 0.09,
        "loss_percent": 0.0,
        "file_size": 1024 * 1024,
    },
    {
        "link_bandwidth": 2 * 1024 * 1024,
        "per_connection_rate": 1 * 1024 * 1024,
        "rate_after": 512 * 1024,
        "switch_kind": "request-count",
        "switch_after_requests": 8,
        "latency_s": 0.0,
        "loss_percent": 4.5,
        "file_size": 1024 * 1024,
    },
    {
        "link_bandwidth": 5 * 1024 * 1024,
        "per_connection_rate": 2 * 1024 * 1024,
        "rate_after": 1024 * 1024,
        "switch_kind": "wall-clock",
        "switch_after_seconds": 0.4,
        "latency_s": 0.05,
        "loss_percent": 2.5,
        "file_size": 768 * 1024,
    },
    {
        "link_bandwidth": 3 * 1024 * 1024,
        "per_connection_rate": 1 * 1024 * 1024,
        "rate_after": 3 * 1024 * 1024,
        "switch_kind": "none",
        "latency_s": 0.02,
        "loss_percent": 0.0,
        "file_size": 1024 * 1024,
    },
]

# Held-out TEST: unseen values/combinations, including wall-clock schedules.
TEST_CONDITIONS = [
    {
        "name": "unseen-low-bandwidth",
        "link_bandwidth": 384 * 1024,
        "per_connection_rate": 384 * 1024,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 1024 * 1024,
    },
    {
        "name": "unseen-high-bandwidth",
        "link_bandwidth": 12 * 1024 * 1024,
        "per_connection_rate": 3 * 1024 * 1024,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 1024 * 1024,
    },
    {
        "name": "unseen-latency",
        "link_bandwidth": 4 * 1024 * 1024,
        "per_connection_rate": 2 * 1024 * 1024,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.14,
        "loss_percent": 0.0,
        "file_size": 768 * 1024,
    },
    {
        "name": "unseen-loss",
        "link_bandwidth": 4 * 1024 * 1024,
        "per_connection_rate": 2 * 1024 * 1024,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.0,
        "loss_percent": 7.0,
        "file_size": 768 * 1024,
    },
    {
        "name": "unseen-latency+loss",
        "link_bandwidth": 3 * 1024 * 1024,
        "per_connection_rate": 1 * 1024 * 1024,
        "rate_after": 0,
        "switch_kind": "none",
        "latency_s": 0.1,
        "loss_percent": 5.0,
        "file_size": 896 * 1024,
    },
    {
        "name": "unseen-throttle-up-request",
        "link_bandwidth": 256 * 1024,
        "per_connection_rate": 256 * 1024,
        "rate_after": 10 * 1024 * 1024,
        "switch_kind": "request-count",
        "switch_after_requests": 6,
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 1024 * 1024,
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
        "file_size": 1024 * 1024,
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
        "file_size": 1024 * 1024,
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
        "file_size": 1024 * 1024,
    },
]


def make_env(cond: dict, seed: int) -> SimEnv:
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
        max_episode_steps=MAX_EPISODE_STEPS,
    )
    return SimEnv(cfg, seed=seed)


def train_sim_config() -> SimConfig:
    return SimConfig(max_episode_steps=MAX_EPISODE_STEPS, **TRAIN_RANGES)


# ---------- rollout helpers ----------


def dqn_rollout(agent, cond, seed):
    return _rollout(make_env(cond, seed), seed, kind="dqn", agent=agent)


def scripted_rollout(cond, seed, kind):
    return _rollout(make_env(cond, seed), seed, kind=kind)


def _baseline_action(kind: str, improve: bool, prefer_conc: bool) -> int:
    """Action for a scripted baseline.

    concurrency: 1 (increase) / 2 (decrease) concurrency.
    chunk:       3 (increase) / 4 (decrease) chunk size.
    joint:       alternate dimension, 1|3 (improve) / 2|4 (degrade).
    """
    if kind == "concurrency":
        return 1 if improve else 2
    if kind == "chunk":
        return 3 if improve else 4
    # joint
    if improve:
        return 1 if prefer_conc else 3
    return 2 if prefer_conc else 4


def _rollout(env, seed, kind, agent=None):
    env.reset()
    total = 0.0
    steps = 0
    conc_changes = chunk_changes = 0
    prev_thr = None
    prefer_conc = True
    while not env.done() and steps < MAX_EPISODE_STEPS + 5:
        c0, k0 = env.concurrency, env.chunk_size
        if kind == "dqn":
            a = agent.greedy(env.state())
        elif kind == "fixed" or prev_thr is None:
            a = 0
        else:
            thr = env._aggregate()
            change = (thr - prev_thr) / prev_thr if prev_thr else 0.0
            if change >= 0.10:
                a = _baseline_action(kind, improve=True, prefer_conc=prefer_conc)
                if kind == "joint":
                    prefer_conc = not prefer_conc
            elif change <= -0.10:
                a = _baseline_action(kind, improve=False, prefer_conc=prefer_conc)
                if kind == "joint":
                    prefer_conc = not prefer_conc
            else:
                a = 0
        prev_thr = env._aggregate()
        res = env.step(a)
        total += res["elapsed"]
        if env.concurrency != c0:
            conc_changes += 1
        if env.chunk_size != k0:
            chunk_changes += 1
        steps += 1
    return {
        "completion_s": total,
        "steps": steps,
        "conc_changes": conc_changes,
        "chunk_changes": chunk_changes,
    }


POLICIES = {
    "fixed": lambda c, s: scripted_rollout(c, s, "fixed"),
    "concurrency-adaptive": lambda c, s: scripted_rollout(c, s, "concurrency"),
    "chunk-size-adaptive": lambda c, s: scripted_rollout(c, s, "chunk"),
    "joint-adaptive": lambda c, s: scripted_rollout(c, s, "joint"),
}


def _chunk_rollout(cond, seed):
    return scripted_rollout(cond, seed, "chunk")


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
        "mean_steps": float(st.mean([r["steps"] for r in rows])),
        "mean_conc_changes": float(st.mean([r["conc_changes"] for r in rows])),
        "mean_chunk_changes": float(st.mean([r["chunk_changes"] for r in rows])),
    }


def validation_mean(agent):
    rows = eval_conditions(
        VALIDATION_CONDITIONS, lambda c, s: dqn_rollout(agent, c, s), [100, 101, 102]
    )
    return summarize(rows)["mean_completion_s"], summarize(rows)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="testdata/diagnostics/dqn_validation_v2")
    ap.add_argument("--episodes", type=int, default=TRAIN_EPISODES)
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)

    sim_cfg = train_sim_config()
    cfg_base = TrainConfig(episodes=args.episodes, seed=0)

    seed_results = []
    agents = {}
    for seed in SEEDS:
        cfg = TrainConfig(episodes=args.episodes, seed=seed)
        t0 = time.time()
        agent, report = train(cfg, sim_cfg)
        val_mean, val_summary = validation_mean(agent)
        rec = {
            "seed": seed,
            **{
                k: report[k]
                for k in (
                    "episodes",
                    "total_steps",
                    "terminal_episodes",
                    "truncated_episodes",
                    "final_loss",
                    "mean_return",
                    "q_min",
                    "q_max",
                    "final_epsilon",
                )
            },
            "validation_mean_completion_s": val_mean,
            "wall_seconds": time.time() - t0,
        }
        seed_results.append(rec)
        agents[seed] = (agent, cfg, val_mean)
        print(f"seed {seed}: val_mean={val_mean:.4f} loss={report['final_loss']}")

    best_seed = min(
        seed_results, key=lambda r: (r["validation_mean_completion_s"], r["seed"])
    )["seed"]
    agent, cfg, val = agents[best_seed]
    val_mean, val_summary = validation_mean(agent)

    # Held-out test: DQN + baselines (same episodes).
    test_seeds = [200, 201, 202, 203]
    heldout = {}
    heldout["dqn"] = {
        "overall": summarize(
            eval_conditions(
                TEST_CONDITIONS, lambda c, s: dqn_rollout(agent, c, s), test_seeds
            )
        )
    }
    for name, fn in POLICIES.items():
        heldout[name] = {
            "overall": summarize(eval_conditions(TEST_CONDITIONS, fn, test_seeds))
        }
    for name in heldout:
        key = {"dqn": lambda c, s: dqn_rollout(agent, c, s)}.get(name)
        if key:
            rows = eval_conditions(TEST_CONDITIONS, key, test_seeds)
        else:
            rows = eval_conditions(TEST_CONDITIONS, POLICIES[name], test_seeds)
        heldout[name]["by_condition"] = {
            c["name"]: summarize([r for r in rows if r["condition"] == c["name"]])
            for c in TEST_CONDITIONS
        }

    # Baseline fidelity check (must actually change controllers).
    fidelity = {}
    for name, kind in [
        ("concurrency-adaptive", "concurrency"),
        ("chunk-size-adaptive", "chunk"),
        ("joint-adaptive", "joint"),
    ]:
        rows = [
            scripted_rollout(c, s, kind) for c in TEST_CONDITIONS for s in test_seeds
        ]
        fidelity[name] = {
            "mean_conc_changes": float(st.mean([r["conc_changes"] for r in rows])),
            "mean_chunk_changes": float(st.mean([r["chunk_changes"] for r in rows])),
        }

    # Ablations.
    ablations = {}
    ablations["dqn"] = heldout["dqn"]["overall"]

    def shuffled(c, s):
        env = make_env(c, s)
        env.reset()
        rng = _r.Random(s)
        total, steps = 0.0, 0
        while not env.done() and steps < MAX_EPISODE_STEPS + 5:
            _ = agent.greedy(env.state())
            total += env.step(rng.randrange(NUM_ACTIONS))["elapsed"]
            steps += 1
        return {
            "completion_s": total,
            "steps": steps,
            "conc_changes": 0,
            "chunk_changes": 0,
        }

    def rand_pol(c, s):
        env = make_env(c, s)
        env.reset()
        rng = _r.Random(s * 7 + 1)
        total, steps = 0.0, 0
        while not env.done() and steps < MAX_EPISODE_STEPS + 5:
            total += env.step(rng.randrange(NUM_ACTIONS))["elapsed"]
            steps += 1
        return {
            "completion_s": total,
            "steps": steps,
            "conc_changes": 0,
            "chunk_changes": 0,
        }

    def corrupt(c, s):
        env = make_env(c, s)
        env.reset()
        total, steps = 0.0, 0
        while not env.done() and steps < MAX_EPISODE_STEPS + 5:
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
            "conc_changes": 0,
            "chunk_changes": 0,
        }

    ablations["random_policy"] = summarize(
        eval_conditions(TEST_CONDITIONS, rand_pol, test_seeds)
    )
    ablations["shuffled_actions"] = summarize(
        eval_conditions(TEST_CONDITIONS, shuffled, test_seeds)
    )
    ablations["corrupted_state"] = summarize(
        eval_conditions(TEST_CONDITIONS, corrupt, test_seeds)
    )

    # Freeze candidate.
    candidate = export_model(agent, cfg, sim_cfg, label="FINAL DQN CANDIDATE v2")
    cand_bytes = json.dumps(candidate, indent=2, sort_keys=True).encode()
    with open(os.path.join(args.out, "final_dqn_candidate_v2.json"), "wb") as f:
        f.write(cand_bytes)
    cand_hash = hashlib.sha256(cand_bytes).hexdigest()
    with open(os.path.join(args.out, "final_dqn_candidate_v2.sha256"), "w") as f:
        f.write(cand_hash + "\n")

    write_json(
        os.path.join(args.out, "simulator_spec_v2.json"),
        {
            "label": "corrected training simulator v2 (training-only; not the real server)",
            "dynamics": {
                "shared_bandwidth": "per-request rate = min(link_bandwidth/concurrency, per_connection_rate); wave wall-clock = slowest concurrent request",
                "request_overhead": "latency_s + overhead_s per request; larger chunks amortize overhead",
                "throttle_models": ["none", "request-count", "wall-clock"],
                "clock": "continuous simulated elapsed time; wall-clock rate selected from elapsed",
            },
            "calibration": {
                "32KiB@512KiB/s": {"sim": 0.064, "real_measured": 0.064},
                "32KiB@4MiB/s": {"sim": 0.0078, "real_measured": 0.008},
                "1MiB@512KiB/s": {"sim": 2.0, "real_measured": 2.05},
                "20ms_latency": {"note": "added per request; real netem 20ms"},
                "1pct_loss": {"note": "single retry on failure; real fetcher retries"},
            },
            "train_ranges": TRAIN_RANGES,
        },
    )
    write_json(
        os.path.join(args.out, "train_config_v2.json"),
        {
            "train": cfg_base.to_json(),
            "seeds": SEEDS,
            "train_ranges": TRAIN_RANGES,
            "validation_conditions": VALIDATION_CONDITIONS,
            "test_conditions": TEST_CONDITIONS,
            "max_episode_steps": MAX_EPISODE_STEPS,
            "selection_criterion": "min mean validation completion time; tie-break lowest seed",
            "python": platform.python_version(),
            "torch": torch.__version__,
            "numpy": np.__version__,
        },
    )
    write_json(
        os.path.join(args.out, "seed_results_v2.json"),
        {"seeds": seed_results, "selected_seed": best_seed},
    )
    write_json(
        os.path.join(args.out, "validation_results_v2.json"),
        {
            "conditions": VALIDATION_CONDITIONS,
            "selected_seed": best_seed,
            "validation_mean_completion_s": val_mean,
            "validation_summary": val_summary,
        },
    )
    write_json(os.path.join(args.out, "heldout_test_results_v2.json"), heldout)
    write_json(os.path.join(args.out, "baseline_validation_v2.json"), fidelity)
    write_json(os.path.join(args.out, "ablations_v2.json"), ablations)

    report = {
        "label": "FINAL DQN CANDIDATE v2 (corrected simulator; simulator-validated; no performance claim)",
        "candidate_sha256": cand_hash,
        "selected_seed": best_seed,
        "selection_criterion": "min mean validation completion time; tie-break lowest seed (test never used)",
        "validation_mean_completion_s": val_mean,
        "heldout_overall": {k: heldout[k]["overall"] for k in heldout},
        "baseline_fidelity": fidelity,
        "ablations": ablations,
        "no_performance_claim": True,
    }
    write_json(os.path.join(args.out, "report.json"), report)
    with open(os.path.join(args.out, "report.txt"), "w") as f:
        f.write(
            "DQN v2 on corrected simulator: training + held-out validation. NO performance claim; NO aria2.\n\n"
        )
        f.write(f"seeds={SEEDS} episodes={args.episodes} selected_seed={best_seed}\n")
        f.write(
            f"validation mean completion={val_mean:.4f}s\ncandidate sha256={cand_hash}\n\n"
        )
        f.write("baseline fidelity (must be > 0 for adaptive baselines):\n")
        for k, v in fidelity.items():
            f.write(
                f"  {k:<22} concChg={v['mean_conc_changes']:.2f} chunkChg={v['mean_chunk_changes']:.2f}\n"
            )
        f.write("\nheld-out TEST mean completion (s):\n")
        for k in heldout:
            o = heldout[k]["overall"]
            f.write(
                f"  {k:<22} mean={o['mean_completion_s']:.4f} median={o['median_completion_s']:.4f} sd={o['sd_completion_s']:.4f}\n"
            )
        f.write("\nablations (mean completion s):\n")
        for k, o in ablations.items():
            f.write(f"  {k:<22} mean={o['mean_completion_s']:.4f}\n")
        f.write("\nper-seed validation:\n")
        for r in seed_results:
            f.write(
                f"  seed {r['seed']}: val={r['validation_mean_completion_s']:.4f} loss={r['final_loss']}\n"
            )
    print(f"selected seed {best_seed} candidate {cand_hash}")
    return 0


def write_json(path, obj):
    with open(path, "w") as f:
        json.dump(obj, f, indent=2, sort_keys=True)
        f.write("\n")


if __name__ == "__main__":
    raise SystemExit(main())
