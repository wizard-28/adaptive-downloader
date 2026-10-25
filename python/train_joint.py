"""Train, select, test and export the joint (concurrency + range size) DQN
candidate on the joint simulator `rl.sim_env_joint`.

    python3 python/train_joint.py [--episodes 1000] [--workers 2]   (from the repository root)

Each seed is trained in its own process. Every evaluation runs the EXPORTED
model through a numpy forward pass (the same arithmetic as Go inference), so
what is selected and tested is exactly what ships. Selection uses validation
conditions only; the test and out-of-distribution sets are reported, never
used for selection. Do not run while live runs are in progress (CPU load
would distort their timing).

Differences from train_cap.py:
  * gamma = 1.0: the objective is the undiscounted episodic return, i.e. minus
    the total completion time. Decisions now cover variable byte counts (the
    range size is controlled), so per-decision discounting would bias the
    agent towards large ranges (fewer decisions).
  * training rewards are scaled by 1/10 (ScaledSimEnv) to keep |Q| near 10;
    argmax is unchanged. Evaluation never uses the reward.
  * a "fixed setting" is a pair (N, S) with no actions.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import random
import sys
import time
from concurrent.futures import ProcessPoolExecutor

import numpy as np

sys.path.insert(0, os.path.dirname(__file__))

from rl.sim_env_cap import KIB, MIB, Condition, load_calibration, live_condition  # noqa: E402
from rl.sim_env_joint import (  # noqa: E402
    ACTION_NAMES, NUM_ACTIONS, JointConfig, SimEnv, joint_spec, run_joint,
)
from rl.state import StateSpec  # noqa: E402

STUDY = "testdata/rl_joint"
CALIB = "testdata/rl_cap_train/calibration.json"
SEEDS = [0, 1, 2, 3, 4]
REWARD_SCALE = 10.0
FIXED_N = (4, 8, 16)
FIXED_S_MIB = (1, 5, 9, 13, 17, 25, 33)


class ScaledSimEnv(SimEnv):
    """Training wrapper: reward / 10 (keeps |Q| ~ 10; argmax unchanged)."""

    def step(self, action: int) -> dict:
        res = super().step(action)
        res["reward"] = res["reward"] / REWARD_SCALE
        return res


def train_config(seed: int, episodes: int):
    from rl.dqn import TrainConfig
    return TrainConfig(
        episodes=episodes, seed=seed, num_actions=NUM_ACTIONS, gamma=1.0, learning_rate=5e-4,
        replay_capacity=100_000, batch_size=64, epsilon_decay_steps=150_000,
        target_update_interval=1000, train_start=2000, max_episode_steps=5000,
    )


def train_seed(args: tuple[int, int, str]) -> dict:
    seed, episodes, out_dir = args
    import torch
    torch.set_num_threads(2)
    from rl.dqn import train
    from rl.model import export_model
    calib = load_calibration(CALIB)
    cfg = train_config(seed, episodes)
    sim_cfg = JointConfig(calib=calib)
    t0 = time.time()
    agent, rep = train(cfg, sim_cfg, env_class=ScaledSimEnv)
    model = export_model(agent, cfg, sim_cfg, label=f"rl_joint seed {seed}",
                         action_names=list(ACTION_NAMES), num_actions=NUM_ACTIONS)
    path = os.path.join(out_dir, f"seed_{seed}.json")
    with open(path, "w") as f:
        json.dump(model, f)
    rets = rep["returns"]
    return {"seed": seed, "model": path, "wall_s": round(time.time() - t0, 1),
            "total_steps": rep["total_steps"], "final_loss": rep["final_loss"],
            "loss_finite": rep["loss_finite"], "q_min": rep["q_min"], "q_max": rep["q_max"],
            "reward_scale": REWARD_SCALE,
            "mean_return_first_100": float(np.mean(rets[:100])), "mean_return_last_100": float(np.mean(rets[-100:]))}


# ---- inference identical to Go (ReLU MLP, argmax, lowest index on ties) ----
class NumpyPolicy:
    def __init__(self, model: dict):
        self.spec = StateSpec(**model["state_spec"])
        self.layers = [(np.array(l["weights"]), np.array(l["bias"]), l["activation"]) for l in model["layers"]]

    def __call__(self, state, env=None) -> int:
        h = np.asarray(state, dtype=float)
        for w, b, act in self.layers:
            h = w @ h + b
            if act == "relu":
                h = np.maximum(h, 0.0)
        return int(np.argmax(h))


def gm(v):
    return math.exp(sum(math.log(x) for x in v) / len(v))


def fixed_time(cond, n, s_bytes, seeds):
    return gm([run_joint(cond, n, s_bytes, lambda st, e: 0, seed=k).completion_s() for k in seeds])


def best_fixed(cond, seeds):
    """(time, N, S_MiB) of the best fixed setting over the joint grid."""
    return min((fixed_time(cond, n, s * MIB, seeds), n, s) for n in FIXED_N for s in FIXED_S_MIB)


def episode(cond, start_n, start_chunk, pol, window, seed):
    """(completion_s, final N, final S, effective moves, chunk moves)."""
    env = run_joint(cond, start_n, start_chunk, pol, window=window, seed=seed)
    tr = env.trajectory
    moves = len(tr) - 1
    chunk_moves = sum(1 for i in range(1, len(tr)) if tr[i][2] != tr[i - 1][2])
    return env.completion_s(), env.target, env.chunk, moves, chunk_moves


def median(v):
    return sorted(v)[len(v) // 2]


# ---- condition sets ----
def validation_conditions(calib):
    out = []
    for c in (6.0, 10.0):
        for cap_at in (None, 0.0, 5.0, 15.0):
            out.append((f"V_C{int(c)}_{'none' if cap_at is None else int(cap_at)}",
                        Condition(file_size=384 * MIB, rate0=512 * KIB, rate1=3 * MIB, switch_at=1.5,
                                  cap_at=cap_at, cap_c=c * MIB, cap_gamma=0.05, calib=calib)))
    return out


def ood_conditions(calib, n=20, seed=777):
    r = random.Random(seed)
    out = []
    for i in range(n):
        dip_at = r.uniform(1.0, 3.0)
        rec_at = dip_at + r.uniform(1.0, 3.0)
        sched = [(0.0, r.randint(320, 1280) * KIB), (dip_at, r.randint(100, 300) * KIB),
                 (rec_at, r.randint(1500, 4000) * KIB)]
        out.append((f"OOD_{i:02d}", Condition(
            file_size=r.randint(256, 512) * MIB, schedule=sched, cap_at=r.uniform(4.0, 20.0),
            cap_c=r.uniform(14.0, 24.0) * MIB, cap_gamma=r.uniform(0.12, 0.20),
            owd_s=r.uniform(0.07, 0.10), calib=calib)))
    return out


def test_conditions(calib):
    out = [(sc, live_condition(sc, calib)) for sc in ("CAP8", "HEALTHY", "CAP0", "G_C16", "G_RTT", "G_D2")]
    out.append(("D2", Condition(cap_at=None, schedule=[(0.0, 640 * KIB), (2.0, 160 * KIB), (4.0, 2500 * KIB)],
                                calib=calib)))
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--episodes", type=int, default=1000)
    ap.add_argument("--workers", type=int, default=2)
    ap.add_argument("--seeds", type=str, default=",".join(map(str, SEEDS)))
    a = ap.parse_args()
    seeds = [int(s) for s in a.seeds.split(",")]
    out_dir = f"{STUDY}/train"
    os.makedirs(out_dir, exist_ok=True)
    os.makedirs(f"{STUDY}/model", exist_ok=True)
    calib = load_calibration(CALIB)
    start_n, start_s = 16, MIB

    t0 = time.time()
    with ProcessPoolExecutor(max_workers=a.workers) as ex:
        trained = list(ex.map(train_seed, [(s, a.episodes, out_dir) for s in seeds]))
    print("trained", [(t["seed"], t["wall_s"]) for t in trained], flush=True)

    # ---- validation and selection ----
    val = validation_conditions(calib)
    val_best = {name: best_fixed(c, [10, 11, 12]) for name, c in val}
    seed_rows = []
    for t in trained:
        pol = NumpyPolicy(json.load(open(t["model"])))
        ratios = []
        for name, c in val:
            tt = gm([episode(c, start_n, start_s, pol, 16, k)[0] for k in (20, 21, 22)])
            ratios.append(tt / val_best[name][0])
        t["validation_score"] = gm(ratios)
        seed_rows.append(t)
        print("seed", t["seed"], "validation score %.4f" % t["validation_score"], flush=True)
    sel = min(seed_rows, key=lambda r: (round(r["validation_score"], 12), r["seed"]))
    model = json.load(open(sel["model"]))
    pol = NumpyPolicy(model)

    # ---- simulator test (reported only) ----
    test = {}
    tseeds = list(range(100, 110))
    for sc, c in test_conditions(calib):
        bt, bn, bs = best_fixed(c, tseeds)
        eps = [episode(c, start_n, start_s, pol, 16, k) for k in tseeds]
        at = gm([e[0] for e in eps])
        row = {"best_fixed_n": bn, "best_fixed_s_mib": bs, "best_fixed_s": bt,
               "fixed_16x1MiB_s": fixed_time(c, 16, MIB, tseeds),
               "fixed_16x33MiB_s": fixed_time(c, 16, 33 * MIB, tseeds),
               "agent_s": at, "agent_vs_best_fixed": at / bt,
               "agent_moves_median": median([e[3] for e in eps]),
               "agent_chunk_moves_median": median([e[4] for e in eps]),
               "agent_final_ns": [(e[1], e[2]) for e in eps]}
        test[sc] = row
        print(sc, {k: (round(v, 3) if isinstance(v, float) else v) for k, v in row.items() if k != "agent_final_ns"}, flush=True)

    ood = []
    for name, c in ood_conditions(calib):
        bt, bn, bs = best_fixed(c, [30, 31])
        tt = gm([episode(c, start_n, start_s, pol, 16, k)[0] for k in (30, 31)])
        ood.append({"name": name, "best_fixed_n": bn, "best_fixed_s_mib": bs, "best_fixed_s": bt,
                    "agent_s": tt, "ratio": tt / bt})
    ood_summary = {"n": len(ood), "gm_ratio_agent": gm([o["ratio"] for o in ood]),
                   "agent_faster_than_best_fixed": sum(o["ratio"] < 1 for o in ood)}
    print("OOD", ood_summary, flush=True)

    gates = {
        "J1_cap8_agent_at_least_3pct_faster_than_best_fixed": test["CAP8"]["agent_vs_best_fixed"] <= 0.97,
        "J2_healthy_within_5pct_of_best_fixed": test["HEALTHY"]["agent_vs_best_fixed"] <= 1.05,
        "J3_changes_s_in_cap8": test["CAP8"]["agent_chunk_moves_median"] > 0,
    }
    print("gates (reported, never block)", gates, flush=True)

    # ---- export ----
    mpath = f"{STUDY}/model/dqn_joint_candidate.json"
    with open(mpath, "w") as f:
        json.dump(model, f)
    sha = hashlib.sha256(open(mpath, "rb").read()).hexdigest()
    open(f"{STUDY}/model/dqn_joint_candidate.sha256", "w").write(f"{sha}  dqn_joint_candidate.json\n")
    fixture = []
    for sc, cond in test_conditions(calib):
        if sc not in ("CAP8", "HEALTHY", "D2"):
            continue
        cfg = JointConfig(window=16, start_n=start_n, start_chunk=start_s, condition=cond)
        env = SimEnv(cfg, 7)
        i = 0
        while not env.done():
            act = pol(env.state(), env)
            if i % 5 == 0:
                fixture.append({"obs": dict(env.last_obs), "features": env.state(), "action": act})
            env.step(act)
            i += 1
    json.dump({"model_sha256": sha, "cases": fixture}, open(f"{STUDY}/model/parity_fixture.json", "w"))

    report = {"selected_seed": sel["seed"], "selection": "min validation score; tie lowest seed",
              "model_sha256": sha, "seeds": seed_rows, "validation_best_fixed": val_best,
              "test": test, "ood": ood, "ood_summary": ood_summary, "gates": gates,
              "train_config": train_config(sel["seed"], a.episodes).to_json(),
              "reward_scale": REWARD_SCALE,
              "sim_config": JointConfig().to_json(), "wall_s": round(time.time() - t0, 1)}
    json.dump(report, open(f"{STUDY}/train/report.json", "w"), indent=1, default=str)
    print("selected seed", sel["seed"], "sha", sha[:12], "wall %.0fs" % (time.time() - t0))


if __name__ == "__main__":
    main()
