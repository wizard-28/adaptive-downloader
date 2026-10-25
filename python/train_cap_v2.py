"""Stage B of testdata/rl_cap_train_v2/protocol.md: train on the broadened v2
distribution with delay-dependent calibration, select on the 20 v2
validation conditions, test and export the v2 candidate.

    python3 python/train_cap_v2.py [--episodes 1000] [--workers 2]   (from the repository root)

Same learner, hyperparameters, inference check and export as train_cap.py
(v1). Additionally reports every seed on the H scenarios and the v1 model
next to v2 on every test scenario.

Each seed is trained in its own process. Every evaluation runs the EXPORTED
model through a numpy forward pass (the same arithmetic as Go inference), so
what is selected and tested is exactly what ships. Selection uses validation
conditions only; the test and out-of-distribution sets are reported, never
used for selection. Do not run while live runs are in progress (CPU load
would distort their timing).
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

from rl.sim_env_cap import (  # noqa: E402
    ACTION_NAMES, KIB, MIB, NUM_ACTIONS, Condition, SimConfig, SimEnv, load_calibration_multi,
    live_condition, run_policy,
)
from rl.state_v6 import StateSpec  # noqa: E402

STUDY = "testdata/rl_cap_train_v2"
CALIB = [f"{STUDY}/calibration_{d}.json" for d in (20, 40, 80, 120)]
SEEDS = [0, 1, 2, 3, 4]
V1_MODEL = "testdata/rl_cap_train/model/dqn_cap_candidate.json"
TEST_SCENARIOS = ("CAP8", "HEALTHY", "CAP0", "G_C16", "G_RTT", "G_D2", "H_C12", "H_RTT110", "H_DIP", "H_C20")


def train_config(seed: int, episodes: int):
    from rl.dqn import TrainConfig
    return TrainConfig(
        episodes=episodes, seed=seed, num_actions=NUM_ACTIONS, gamma=0.99, learning_rate=5e-4,
        replay_capacity=100_000, batch_size=64, epsilon_decay_steps=150_000,
        target_update_interval=1000, train_start=2000, max_episode_steps=5000,
    )


def train_seed(args: tuple[int, int, str]) -> dict:
    seed, episodes, out_dir = args
    import torch
    torch.set_num_threads(2)
    from rl.dqn import train
    from rl.model import export_model
    calib = load_calibration_multi(CALIB)
    cfg = train_config(seed, episodes)
    sim_cfg = SimConfig(calib=calib, v2_dist=True)
    t0 = time.time()
    agent, rep = train(cfg, sim_cfg, env_class=SimEnv)
    model = export_model(agent, cfg, sim_cfg, label=f"rl_cap_train_v2 seed {seed}",
                         action_names=list(ACTION_NAMES), num_actions=NUM_ACTIONS)
    path = os.path.join(out_dir, f"seed_{seed}.json")
    with open(path, "w") as f:
        json.dump(model, f)
    rets = rep["returns"]
    return {"seed": seed, "model": path, "wall_s": round(time.time() - t0, 1),
            "total_steps": rep["total_steps"], "final_loss": rep["final_loss"],
            "loss_finite": rep["loss_finite"], "q_min": rep["q_min"], "q_max": rep["q_max"],
            "mean_return_first_100": float(np.mean(rets[:100])), "mean_return_last_100": float(np.mean(rets[-100:])),
            "return_curve_per_100": [float(np.mean(rets[i:i + 100])) for i in range(0, len(rets), 100)]}


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


def fixed_time(cond, n, seeds):
    return gm([run_policy(cond, n, lambda s, e: 0, seed=k).completion_s() for k in seeds])


def best_fixed(cond, seeds):
    return min((fixed_time(cond, n, seeds), n) for n in range(1, 17))


def episode(cond, start_n, pol, window, seed):
    env = run_policy(cond, start_n, pol, window=window, spec=pol.spec, seed=seed)
    moves = sum(1 for i in range(1, len(env.trajectory)))
    return env.completion_s(), env.target, moves


# ---- condition sets ----
def validation_conditions(calib):
    """The 8 v1 validation conditions, the same 8 at 80 ms, and 4 late-dip ones."""
    out = []
    for owd in (0.040, 0.080):
        for c in (6.0, 10.0):
            for cap_at in (None, 0.0, 5.0, 15.0):
                out.append((f"V_C{int(c)}_{'none' if cap_at is None else int(cap_at)}_{int(owd * 1000)}ms",
                            Condition(file_size=384 * MIB, rate0=512 * KIB, rate1=3 * MIB, switch_at=1.5,
                                      cap_at=cap_at, cap_c=c * MIB, cap_gamma=0.05,
                                      owd_s=owd, client_start_s=2 * owd, calib=calib)))
    for c in (6.0, 10.0):
        for cap_at in (None, 15.0):
            out.append((f"V_DIP_C{int(c)}_{'none' if cap_at is None else int(cap_at)}",
                        Condition(file_size=384 * MIB, schedule=[(0.0, 512 * KIB), (1.5, 3 * MIB), (6.0, 400 * KIB),
                                                                 (10.0, 3 * MIB)],
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
    calib = load_calibration_multi(CALIB)

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
            for n0 in (1, 16):
                tt = gm([episode(c, n0, pol, 16, k)[0] for k in (20, 21, 22)])
                ratios.append(tt / val_best[name][0])
        t["validation_score"] = gm(ratios)
        seed_rows.append(t)
        print("seed", t["seed"], "validation score %.4f" % t["validation_score"], flush=True)
    sel = min(seed_rows, key=lambda r: (round(r["validation_score"], 12), r["seed"]))
    model = json.load(open(sel["model"]))
    pol = NumpyPolicy(model)

    # ---- simulator test (reported only) ----
    v1pol = NumpyPolicy(json.load(open(V1_MODEL)))
    seed_pols = {t["seed"]: NumpyPolicy(json.load(open(t["model"]))) for t in trained}
    test = {}
    tseeds = list(range(100, 110))
    for sc in TEST_SCENARIOS:
        c = live_condition(sc, calib)
        bt, bn = best_fixed(c, tseeds)
        row = {"best_fixed_n": bn, "best_fixed_s": bt}
        for n0 in (1, 16):
            eps = [episode(c, n0, pol, 16, k) for k in tseeds]
            row[f"dqnc_start{n0}_s"] = gm([e[0] for e in eps])
            row[f"dqnc_start{n0}_final_n"] = [e[1] for e in eps]
            row[f"dqnc_start{n0}_moves_median"] = sorted(e[2] for e in eps)[len(eps) // 2]
            row[f"dqnc_start{n0}_vs_best_fixed"] = row[f"dqnc_start{n0}_s"] / bt
        row["v1_start16_s"] = gm([episode(c, 16, v1pol, 16, k)[0] for k in tseeds])
        row["v1_start16_vs_best_fixed"] = row["v1_start16_s"] / bt
        if sc.startswith("H_"):
            row["all_seeds_start16_vs_best_fixed"] = {
                s: gm([episode(c, 16, sp, 16, k)[0] for k in tseeds]) / bt for s, sp in seed_pols.items()}
        if c.cap_at is not None and c.cap_at > 0:
            row["oracle"] = min((gm([run_policy(c, 16, lambda s, e, m=m: 2 if (e.now >= c.cap_at and e.target > m) else 0,
                                                seed=k).completion_s() for k in tseeds]), m) for m in range(2, 16))
        test[sc] = row
        print(sc, {k: (round(v, 3) if isinstance(v, float) else v) for k, v in row.items() if not k.endswith("final_n")}, flush=True)

    ood = []
    for name, c in ood_conditions(calib):
        bt, bn = best_fixed(c, [30, 31])
        tt = gm([episode(c, 16, pol, 16, k)[0] for k in (30, 31)])
        t1 = gm([episode(c, 16, v1pol, 16, k)[0] for k in (30, 31)])
        ood.append({"name": name, "best_fixed_n": bn, "best_fixed_s": bt, "dqnc_s": tt, "v1_s": t1,
                    "ratio": tt / bt, "v1_ratio": t1 / bt})
    # The "v1 OOD set": mostly inside the v2 training ranges now (protocol).
    ood_summary = {"n": len(ood), "gm_ratio_dqnc": gm([o["ratio"] for o in ood]),
                   "gm_ratio_v1": gm([o["v1_ratio"] for o in ood]),
                   "dqnc_faster_than_best_fixed": sum(o["ratio"] < 1 for o in ood)}
    print("v1 OOD set", ood_summary, flush=True)

    # Reported only; the v2 protocol has no training gates.
    gates = {
        "G1_cap8_start16_at_least_3pct_faster": test["CAP8"]["dqnc_start16_vs_best_fixed"] <= 0.97,
        "G2_healthy_start16_within_5pct": test["HEALTHY"]["dqnc_start16_vs_best_fixed"] <= 1.05,
        "G3_changes_n_in_cap8": test["CAP8"]["dqnc_start16_moves_median"] > 0,
        "H_faster_than_best_fixed": [sc for sc in TEST_SCENARIOS if sc.startswith("H_")
                                     and test[sc]["dqnc_start16_vs_best_fixed"] < 1],
    }
    print("indicators", gates, flush=True)

    # ---- export ----
    mpath = f"{STUDY}/model/dqn_cap_v2_candidate.json"
    with open(mpath, "w") as f:
        json.dump(model, f)
    sha = hashlib.sha256(open(mpath, "rb").read()).hexdigest()
    open(f"{STUDY}/model/dqn_cap_v2_candidate.sha256", "w").write(f"{sha}  dqn_cap_v2_candidate.json\n")
    fixture = []
    for sc, n0 in (("CAP8", 16), ("H_RTT110", 16), ("H_DIP", 1)):
        cfg = SimConfig(window=16, start_n=n0, condition=live_condition(sc, calib), spec=pol.spec)
        env = SimEnv(cfg, 7)
        i = 0
        while not env.done():
            act = pol(env.state())
            if i % 5 == 0:
                fixture.append({"obs": dict(env.last_obs), "features": env.state(), "action": act})
            env.step(act)
            i += 1
    json.dump({"model_sha256": sha, "cases": fixture}, open(f"{STUDY}/model/parity_fixture.json", "w"))

    report = {"selected_seed": sel["seed"], "selection": "min validation score; tie lowest seed",
              "model_sha256": sha, "seeds": seed_rows, "validation_best_fixed": val_best,
              "test": test, "ood": ood, "ood_summary": ood_summary, "gates": gates,
              "train_config": train_config(sel["seed"], a.episodes).to_json(),
              "sim_config": SimConfig(v2_dist=True).to_json(), "calibration": CALIB,
              "wall_s": round(time.time() - t0, 1)}
    json.dump(report, open(f"{STUDY}/train/report.json", "w"), indent=1, default=str)
    print("selected seed", sel["seed"], "sha", sha[:12], "wall %.0fs" % (time.time() - t0))


if __name__ == "__main__":
    main()
