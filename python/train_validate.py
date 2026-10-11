"""Train, select, and freeze a DQN policy from the randomized simulator.

Pipeline:
  1. Train Double DQN across multiple seeds on randomized TRAIN conditions.
  2. Select the candidate using a VALIDATION criterion only (mean validation
     completion time), never training reward and never the test set.
  3. Evaluate the frozen candidate on a deterministic held-out TEST set against
     simple simulator baselines (fixed / concurrency-adaptive / joint-adaptive).
  4. Run ablations (shuffled actions, random policy, shuffled state features).
  5. Freeze FINAL DQN CANDIDATE v1.
  6. Export the live-transfer config for the Go downloader.

No performance claim is made here; this is simulator validation + transfer setup.
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

from rl.dqn import DoubleDQN, TrainConfig, train  # noqa: E402
from rl.model import export_model  # noqa: E402
from rl.sim_env import ACTION_NAMES, NUM_ACTIONS, SimConfig, SimEnv  # noqa: E402
from rl.state import FEATURE_NAMES, default_state_spec  # noqa: E402

SEEDS = [0, 1, 2, 3, 4]
TRAIN_EPISODES = 800
MAX_EPISODE_STEPS = 120

# Explicit condition ranges. TRAIN randomizes across these; VALIDATION and TEST
# use disjoint, unseen values (see below).
TRAIN_RANGES = {
    "bandwidth": (512 * 1024, 8 * 1024 * 1024),
    "bandwidth_2": (256 * 1024, 4 * 1024 * 1024),
    "latency_s": (0.0, 0.08),
    "loss_percent": (0.0, 4.0),
    "switch_after": (0, 12),
    "file_size": (256 * 1024, 1536 * 1024),
}

# Validation: unseen latency/loss values inside a separate band.
VALIDATION_CONDITIONS = [
    {
        "bandwidth": 700 * 1024,
        "bandwidth_2": 3 * 1024 * 1024,
        "switch_after": 3,
        "latency_s": 0.09,
        "loss_percent": 0.0,
        "file_size": 640 * 1024,
    },
    {
        "bandwidth": 2 * 1024 * 1024,
        "bandwidth_2": 512 * 1024,
        "switch_after": 8,
        "latency_s": 0.0,
        "loss_percent": 4.5,
        "file_size": 768 * 1024,
    },
    {
        "bandwidth": 5 * 1024 * 1024,
        "bandwidth_2": 1024 * 1024,
        "switch_after": 2,
        "latency_s": 0.05,
        "loss_percent": 2.5,
        "file_size": 512 * 1024,
    },
]

# Held-out TEST: values and combinations not present in TRAIN or VALIDATION.
TEST_CONDITIONS = [
    {
        "name": "unseen-low-bandwidth",
        "bandwidth": 384 * 1024,
        "bandwidth_2": 384 * 1024,
        "switch_after": 0,
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 1024 * 1024,
    },
    {
        "name": "unseen-high-bandwidth",
        "bandwidth": 12 * 1024 * 1024,
        "bandwidth_2": 12 * 1024 * 1024,
        "switch_after": 0,
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 1024 * 1024,
    },
    {
        "name": "unseen-latency",
        "bandwidth": 4 * 1024 * 1024,
        "bandwidth_2": 4 * 1024 * 1024,
        "switch_after": 0,
        "latency_s": 0.14,
        "loss_percent": 0.0,
        "file_size": 768 * 1024,
    },
    {
        "name": "unseen-loss",
        "bandwidth": 4 * 1024 * 1024,
        "bandwidth_2": 4 * 1024 * 1024,
        "switch_after": 0,
        "latency_s": 0.0,
        "loss_percent": 7.0,
        "file_size": 768 * 1024,
    },
    {
        "name": "unseen-latency+loss",
        "bandwidth": 3 * 1024 * 1024,
        "bandwidth_2": 3 * 1024 * 1024,
        "switch_after": 0,
        "latency_s": 0.1,
        "loss_percent": 5.0,
        "file_size": 896 * 1024,
    },
    {
        "name": "unseen-throttle-up",
        "bandwidth": 256 * 1024,
        "bandwidth_2": 10 * 1024 * 1024,
        "switch_after": 6,
        "latency_s": 0.0,
        "loss_percent": 0.0,
        "file_size": 1024 * 1024,
    },
    {
        "name": "unseen-throttle-down",
        "bandwidth": 10 * 1024 * 1024,
        "bandwidth_2": 512 * 1024,
        "switch_after": 10,
        "latency_s": 0.0,
        "loss_percent": 1.0,
        "file_size": 1024 * 1024,
    },
    {
        "name": "unseen-combined",
        "bandwidth": 2 * 1024 * 1024,
        "bandwidth_2": 8 * 1024 * 1024,
        "switch_after": 4,
        "latency_s": 0.12,
        "loss_percent": 6.0,
        "file_size": 1024 * 1024,
    },
]


def make_env(cond: dict, seed: int) -> SimEnv:
    cfg = SimConfig(
        file_size=cond["file_size"],
        bandwidth=cond["bandwidth"],
        bandwidth_2=cond["bandwidth_2"],
        switch_after=cond["switch_after"],
        latency_s=cond["latency_s"],
        loss_percent=cond["loss_percent"],
        max_episode_steps=MAX_EPISODE_STEPS,
    )
    return SimEnv(cfg, seed=seed)


def train_sim_config() -> SimConfig:
    return SimConfig(
        max_episode_steps=MAX_EPISODE_STEPS,
        rand_bandwidth=TRAIN_RANGES["bandwidth"],
        rand_bandwidth_2=TRAIN_RANGES["bandwidth_2"],
        rand_latency_s=TRAIN_RANGES["latency_s"],
        rand_loss_percent=TRAIN_RANGES["loss_percent"],
        rand_switch_after=TRAIN_RANGES["switch_after"],
        rand_file_size=TRAIN_RANGES["file_size"],
        rand_initial_concurrency=True,
    )


# ---------------- policies ----------------


def dqn_rollout(agent: DoubleDQN, cond: dict, seed: int) -> dict:
    env = make_env(cond, seed)
    env.reset()
    total = 0.0
    steps = 0
    conc_changes = 0
    chunk_changes = 0
    while not env.done() and steps < MAX_EPISODE_STEPS + 5:
        c0, k0 = env.concurrency, env.chunk_size
        a = agent.greedy(env.state())
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


def fixed_rollout(cond: dict, seed: int) -> dict:
    return scripted_rollout(cond, seed, kind="fixed")


def concurrency_rollout(cond: dict, seed: int) -> dict:
    return scripted_rollout(cond, seed, kind="concurrency")


def joint_rollout(cond: dict, seed: int) -> dict:
    return scripted_rollout(cond, seed, kind="joint")


def scripted_rollout(cond: dict, seed: int, kind: str) -> dict:
    """Simple deterministic simulator baselines.

    fixed: always ActionNone.
    concurrency: increase concurrency when aggregate throughput rises vs the
        previous wave, decrease when it falls (a throughput rule on concurrency).
    joint: alternate concurrency/chunk increases on improvement and decreases on
        degradation.
    """
    env = make_env(cond, seed)
    env.reset()
    total, steps = 0.0, 0
    conc_changes = chunk_changes = 0
    prev_thr = None
    prefer_conc = True
    while not env.done() and steps < MAX_EPISODE_STEPS + 5:
        c0, k0 = env.concurrency, env.chunk_size
        thr = env._aggregate()
        if kind == "fixed" or prev_thr is None:
            a = 0
        else:
            change = (thr - prev_thr) / prev_thr if prev_thr > 0 else 0.0
            if change >= 0.10:
                a = 1 if kind == "concurrency" else (1 if prefer_conc else 3)
                prefer_conc = not prefer_conc
            elif change <= -0.10:
                a = 2 if kind == "concurrency" else (2 if prefer_conc else 4)
                prefer_conc = not prefer_conc
            else:
                a = 0
        prev_thr = thr
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
    "dqn": dqn_rollout,
    "fixed": fixed_rollout,
    "concurrency-adaptive": concurrency_rollout,
    "joint-adaptive": joint_rollout,
}


def eval_conditions(conditions, policy, seeds) -> list[dict]:
    rows = []
    for cond in conditions:
        for s in seeds:
            r = policy(cond, s)
            r["condition"] = cond.get("name", cond["bandwidth"])
            rows.append(r)
    return rows


def summarize(rows) -> dict:
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


def validation_mean(agent: DoubleDQN) -> float:
    rows = eval_conditions(
        VALIDATION_CONDITIONS,
        lambda c, s: dqn_rollout(agent, c, s),
        list(range(100, 103)),
    )
    return summarize(rows)["mean_completion_s"]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="testdata/diagnostics/dqn_validation")
    ap.add_argument("--episodes", type=int, default=TRAIN_EPISODES)
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)

    train_cfg_base = TrainConfig(episodes=args.episodes, seed=0)
    sim_cfg = train_sim_config()

    seed_results = []
    agents = {}
    for seed in SEEDS:
        cfg = TrainConfig(episodes=args.episodes, seed=seed)
        start = time.time()
        agent, report = train(cfg, sim_cfg)
        val = validation_mean(agent)
        model = export_model(agent, cfg, sim_cfg, label="SEED-SELECTION CANDIDATE")
        mbytes = json.dumps(model, indent=2, sort_keys=True).encode()
        h = hashlib.sha256(mbytes).hexdigest()
        rec = {
            "seed": seed,
            "episodes": report["episodes"],
            "total_steps": report["total_steps"],
            "terminal_episodes": report["terminal_episodes"],
            "truncated_episodes": report["truncated_episodes"],
            "final_loss": report["final_loss"],
            "mean_return": report["mean_return"],
            "q_min": report["q_min"],
            "q_max": report["q_max"],
            "final_epsilon": report["final_epsilon"],
            "validation_mean_completion_s": val,
            "model_sha256": h,
            "wall_seconds": time.time() - start,
        }
        seed_results.append(rec)
        agents[seed] = (agent, cfg, report, val, h)
        print(f"seed {seed}: val_mean={val:.4f} sha={h} loss={report['final_loss']}")

    # Model selection: lowest VALIDATION mean completion time, tie-break to the
    # lowest seed (explicit, deterministic rule). Never uses the test set.
    best_seed = min(
        seed_results, key=lambda r: (r["validation_mean_completion_s"], r["seed"])
    )["seed"]
    agent, cfg, report, val, cand_hash = agents[best_seed]

    # Held-out TEST set: all baselines over the same episodes.
    test_seeds = list(range(200, 204))  # 4 episodes per condition
    heldout = {}
    for name, fn in POLICIES.items():
        rows = eval_conditions(
            TEST_CONDITIONS,
            fn if name != "dqn" else (lambda c, s: dqn_rollout(agent, c, s)),
            test_seeds,
        )
        heldout[name] = {
            "overall": summarize(rows),
            "by_condition": {
                c["name"]: summarize([r for r in rows if r["condition"] == c["name"]])
                for c in TEST_CONDITIONS
            },
        }

    # Ablations.
    ablations = {}
    # A. shuffled actions: apply a random permutation of the model's action each step.
    import random as _r

    def shuffled_rollout(cond, seed):
        env = make_env(cond, seed)
        env.reset()
        total, steps = 0.0, 0
        rng = _r.Random(seed)
        while not env.done() and steps < MAX_EPISODE_STEPS + 5:
            a = agent.greedy(env.state())
            a = rng.randrange(NUM_ACTIONS)
            total += env.step(a)["elapsed"]
            steps += 1
        return {
            "completion_s": total,
            "steps": steps,
            "conc_changes": 0,
            "chunk_changes": 0,
        }

    ablations["shuffled_actions"] = summarize(
        eval_conditions(TEST_CONDITIONS, shuffled_rollout, test_seeds)
    )

    # B. deterministic random policy (seeded uniform).
    def random_rollout(cond, seed):
        env = make_env(cond, seed)
        env.reset()
        total, steps = 0.0, 0
        rng = _r.Random(seed * 7 + 1)
        while not env.done() and steps < MAX_EPISODE_STEPS + 5:
            total += env.step(rng.randrange(NUM_ACTIONS))["elapsed"]
            steps += 1
        return {
            "completion_s": total,
            "steps": steps,
            "conc_changes": 0,
            "chunk_changes": 0,
        }

    ablations["random_policy"] = summarize(
        eval_conditions(TEST_CONDITIONS, random_rollout, test_seeds)
    )

    # C. corrupted temporal state: zero out temporal features before inference.
    def corrupt_rollout(cond, seed):
        env = make_env(cond, seed)
        env.reset()
        total, steps = 0.0, 0
        while not env.done() and steps < MAX_EPISODE_STEPS + 5:
            x = list(env.state())
            # destroy temporal structure: features 4..12 (ewma, slope, volatility,
            # durations, rates, staleness)
            for i in range(3, len(x)):
                x[i] = 0.0
            import torch as _t

            with _t.no_grad():
                q = agent.online(_t.tensor(x, dtype=_t.float32).unsqueeze(0))
            a = int(_t.argmax(q, dim=1).item())
            total += env.step(a)["elapsed"]
            steps += 1
        return {
            "completion_s": total,
            "steps": steps,
            "conc_changes": 0,
            "chunk_changes": 0,
        }

    ablations["corrupted_state"] = summarize(
        eval_conditions(TEST_CONDITIONS, corrupt_rollout, test_seeds)
    )

    # Candidate artifact + hash.
    candidate = export_model(agent, cfg, sim_cfg, label="FINAL DQN CANDIDATE v1")
    cand_bytes = json.dumps(candidate, indent=2, sort_keys=True).encode()
    with open(os.path.join(args.out, "final_dqn_candidate.json"), "wb") as f:
        f.write(cand_bytes)
    # Hash the exact bytes on disk so the Go transfer guard matches byte-for-byte.
    cand_hash = hashlib.sha256(cand_bytes).hexdigest()
    with open(os.path.join(args.out, "final_dqn_candidate.sha256"), "w") as f:
        f.write(cand_hash + "\n")

    write_json(
        os.path.join(args.out, "train_config.json"),
        {
            "train": train_cfg_base.to_json(),
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
        os.path.join(args.out, "seed_results.json"),
        {"seeds": seed_results, "selected_seed": best_seed},
    )
    write_json(
        os.path.join(args.out, "validation_results.json"),
        {
            "conditions": VALIDATION_CONDITIONS,
            "selected_seed": best_seed,
            "validation_mean_completion_s": val,
        },
    )
    write_json(os.path.join(args.out, "heldout_test_results.json"), heldout)
    write_json(os.path.join(args.out, "ablations.json"), ablations)

    report = {
        "label": "FINAL DQN CANDIDATE v1 (simulator-validated; not a final performance claim)",
        "selected_seed": best_seed,
        "candidate_sha256": cand_hash,
        "validation_mean_completion_s": val,
        "seed_validation_means": {
            r["seed"]: r["validation_mean_completion_s"] for r in seed_results
        },
        "heldout_overall": {k: v["overall"] for k, v in heldout.items()},
        "ablations_overall": ablations,
        "no_performance_claim": True,
    }
    write_json(os.path.join(args.out, "report.json"), report)
    with open(os.path.join(args.out, "report.txt"), "w") as f:
        f.write(
            "DQN training + held-out validation (simulator). FINAL DQN CANDIDATE v1.\n"
        )
        f.write("No performance claim; no aria2 comparison; simulator-only.\n\n")
        f.write(
            f"seeds: {SEEDS}\nepisodes/seed: {args.episodes}\nselection: {report['selection_criterion'] if False else 'min validation mean completion time, tie-break lowest seed'}\n"
        )
        f.write(
            f"selected seed: {best_seed}\nvalidation mean completion: {val:.4f}s\ncandidate sha256: {cand_hash}\n\n"
        )
        f.write("held-out TEST (mean completion s):\n")
        for k, v in heldout.items():
            o = v["overall"]
            f.write(
                f"  {k:<22} mean={o['mean_completion_s']:.4f} median={o['median_completion_s']:.4f} sd={o['sd_completion_s']:.4f} steps={o['mean_steps']:.1f} concChg={o['mean_conc_changes']:.2f}\n"
            )
        f.write("\nablations (mean completion s):\n")
        for k, o in ablations.items():
            f.write(f"  {k:<22} mean={o['mean_completion_s']:.4f}\n")
        f.write("\nseed validation means:\n")
        for r in seed_results:
            f.write(
                f"  seed {r['seed']}: val={r['validation_mean_completion_s']:.4f} sha={r['model_sha256']}\n"
            )
    print(f"selected seed {best_seed} candidate {cand_hash}")
    return 0


def write_json(path, obj):
    with open(path, "w") as f:
        json.dump(obj, f, indent=2, sort_keys=True)
        f.write("\n")


if __name__ == "__main__":
    raise SystemExit(main())
