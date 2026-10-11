"""Sanity-training CLI for the DQN controller.

Runs a SMALL exploratory training (not the final policy), writes the training
config/report and the exported portable model to testdata/diagnostics/dqn/, and
prints the model SHA-256. Deterministic for a fixed seed.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
import time

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.dqn import TrainConfig, train  # noqa: E402
from rl.model import export_model  # noqa: E402
from rl.sim_env import SimConfig  # noqa: E402


def build_sim_config() -> SimConfig:
    return SimConfig()


def main() -> int:
    ap = argparse.ArgumentParser(description="DQN sanity training (DEVELOPMENT model)")
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--episodes", type=int, default=300)
    ap.add_argument("--out", default="testdata/diagnostics/dqn")
    args = ap.parse_args()

    cfg = TrainConfig(seed=args.seed, episodes=args.episodes)
    sim_cfg = build_sim_config()

    start = time.time()
    agent, report = train(cfg, sim_cfg)
    wall = time.time() - start

    os.makedirs(args.out, exist_ok=True)
    with open(os.path.join(args.out, "training_config.json"), "w") as f:
        json.dump(
            {"train": cfg.to_json(), "simulator": sim_cfg.to_json()},
            f,
            indent=2,
            sort_keys=True,
        )
        f.write("\n")

    model = export_model(agent, cfg, sim_cfg, label="DEVELOPMENT / SANITY MODEL")
    model_bytes = json.dumps(model, indent=2, sort_keys=True).encode()
    with open(os.path.join(args.out, "exported_model.json"), "wb") as f:
        f.write(model_bytes + b"\n")
    sha = hashlib.sha256(model_bytes).hexdigest()
    with open(os.path.join(args.out, "model_sha256.txt"), "w") as f:
        f.write(sha + "\n")

    report["wall_seconds"] = wall
    report["model_sha256"] = sha
    report["seed"] = cfg.seed
    report["episodes_requested"] = cfg.episodes
    with open(os.path.join(args.out, "training_report.json"), "w") as f:
        json.dump(report, f, indent=2, sort_keys=True)
        f.write("\n")
    with open(os.path.join(args.out, "training_report.txt"), "w") as f:
        f.write(
            "DQN sanity training report (DEVELOPMENT / SANITY MODEL - not the final policy)\n"
        )
        f.write("No performance claim is made.\n\n")
        for k, v in sorted(report.items()):
            f.write(f"{k}: {v}\n")
    print(
        f"seed={cfg.seed} sha256={sha} final_loss={report['final_loss']} "
        f"epsilon={report['final_epsilon']} q=[{report['q_min']:.3f},{report['q_max']:.3f}]"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
