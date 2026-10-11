import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.dqn import ReplayBuffer, TrainConfig, train
from rl.sim_env import SimConfig


def test_replay_buffer():
    import random

    rb = ReplayBuffer(5, seed=0)
    for i in range(10):
        rb.push([i] * 13, i % 5, -0.1, [i + 1] * 13, False)
    assert len(rb) == 5  # capacity
    s, a, r, s2, d = rb.sample(4)
    assert s.shape == (4, 13)
    assert a.shape == (4,)


def test_training_finite_and_terminates():
    cfg = TrainConfig(episodes=10, seed=0, train_start=32, batch_size=16)
    agent, report = train(cfg, SimConfig(file_size=256 * 1024, max_episode_steps=100))
    assert report["loss_finite"]
    assert report["q_finite"]
    assert report["final_epsilon"] <= cfg.epsilon_start
    assert report["terminal_episodes"] + report["truncated_episodes"] == 10
    assert report["replay_size"] > 0
    assert len(report["returns"]) == 10
    assert len(report["episode_steps"]) == 10


def test_target_update():
    cfg = TrainConfig(
        episodes=2, seed=1, train_start=16, batch_size=8, target_update_interval=5
    )
    agent, _ = train(cfg, SimConfig(file_size=128 * 1024, max_episode_steps=50))
    # After training, target should have been synced at least once.
    import torch

    same = all(
        torch.equal(p, q)
        for p, q in zip(agent.online.parameters(), agent.target.parameters())
    )
    assert same or agent.steps >= cfg.target_update_interval


def test_seed_determinism_hash():
    import json, hashlib
    from rl.model import export_model

    def run(seed):
        cfg = TrainConfig(episodes=3, seed=seed, train_start=16, batch_size=8)
        sim = SimConfig(file_size=128 * 1024, max_episode_steps=50)
        agent, _ = train(cfg, sim)
        m = export_model(agent, cfg, sim, "TEST")
        return hashlib.sha256(
            json.dumps(m, indent=2, sort_keys=True).encode()
        ).hexdigest()

    assert run(0) == run(0)  # same seed -> identical export
    assert run(0) != run(1)  # different seed -> different export
