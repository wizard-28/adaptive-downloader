"""Double DQN trainer (PyTorch).

Deliberately small: an MLP Q-network + target network, uniform experience replay,
epsilon-greedy exploration, Huber loss, gradient clipping, periodic target
synchronization, terminal episodes, and deterministic greedy inference. No PPO /
SAC / actor-critic / recurrent / prioritized / distributional / multi-agent.

The reward is the negative elapsed wall-clock time per transition, gamma = 1.0,
so the undiscounted episode return approximates -completion_time.
"""

from __future__ import annotations

import random
from collections import deque
from dataclasses import dataclass, asdict

import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F

from .sim_env import NUM_ACTIONS, SimEnv


@dataclass
class TrainConfig:
    hidden_sizes: tuple[int, int] = (64, 64)
    learning_rate: float = 1e-3
    replay_capacity: int = 20000
    batch_size: int = 64
    gamma: float = 1.0
    epsilon_start: float = 1.0
    epsilon_end: float = 0.05
    epsilon_decay_steps: int = 5000
    target_update_interval: int = 200
    max_episode_steps: int = 200
    episodes: int = 300
    seed: int = 0
    gradient_clip: float = 10.0
    train_start: int = 500
    randomize_conditions: bool = True
    num_actions: int = NUM_ACTIONS

    def to_json(self) -> dict:
        return asdict(self)


class QNet(nn.Module):
    def __init__(self, in_dim: int, hidden_sizes: tuple[int, ...], n_actions: int):
        super().__init__()
        layers: list[nn.Module] = []
        prev = in_dim
        for h in hidden_sizes:
            layers.append(nn.Linear(prev, h))
            layers.append(nn.ReLU())
            prev = h
        layers.append(nn.Linear(prev, n_actions))
        self.net = nn.Sequential(*layers)

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        return self.net(x)


class ReplayBuffer:
    """Uniform experience replay with deterministic seeded sampling."""

    def __init__(self, capacity: int, seed: int):
        self.capacity = capacity
        self.buf: deque = deque(maxlen=capacity)
        self.rng = random.Random(seed)

    def __len__(self) -> int:
        return len(self.buf)

    def push(self, s, a, r, s2, done) -> None:
        self.buf.append(
            (
                np.asarray(s, dtype=np.float32),
                int(a),
                float(r),
                np.asarray(s2, dtype=np.float32),
                bool(done),
            )
        )

    def sample(self, batch_size: int):
        batch = self.rng.sample(self.buf, batch_size)
        s, a, r, s2, d = zip(*batch)
        return (
            np.stack(s),
            np.array(a),
            np.array(r, dtype=np.float32),
            np.stack(s2),
            np.array(d, dtype=np.float32),
        )


class DoubleDQN:
    """Double DQN agent with a target network."""

    def __init__(self, in_dim: int, cfg: TrainConfig, seed: int):
        self.cfg = cfg
        self.seed = seed
        torch.manual_seed(seed)
        np.random.seed(seed)
        self.online = QNet(in_dim, cfg.hidden_sizes, cfg.num_actions)
        self.target = QNet(in_dim, cfg.hidden_sizes, cfg.num_actions)
        self.target.load_state_dict(self.online.state_dict())
        self.optimizer = torch.optim.Adam(
            self.online.parameters(), lr=cfg.learning_rate
        )
        self.replay = ReplayBuffer(cfg.replay_capacity, seed)
        self.steps = 0
        self.losses: list[float] = []
        self.max_abs_q = 0.0

    def epsilon(self) -> float:
        c = self.cfg
        if self.steps >= c.epsilon_decay_steps:
            return c.epsilon_end
        frac = self.steps / max(1, c.epsilon_decay_steps)
        return c.epsilon_start + (c.epsilon_end - c.epsilon_start) * frac

    def act(self, state: list[float], explore: bool) -> int:
        if explore and random.Random(self.seed + self.steps).random() < self.epsilon():
            return self.replay.rng.randrange(self.cfg.num_actions)
        with torch.no_grad():
            x = torch.tensor(state, dtype=torch.float32).unsqueeze(0)
            q = self.online(x)
        return int(torch.argmax(q, dim=1).item())

    def greedy(self, state: list[float]) -> int:
        return self.act(state, explore=False)

    def train_step(self) -> float | None:
        c = self.cfg
        if len(self.replay) < max(c.batch_size, c.train_start):
            return None
        s, a, r, s2, d = self.replay.sample(c.batch_size)
        s = torch.tensor(s, dtype=torch.float32)
        s2 = torch.tensor(s2, dtype=torch.float32)
        a = torch.tensor(a, dtype=torch.long)
        r = torch.tensor(r, dtype=torch.float32)
        d = torch.tensor(d, dtype=torch.float32)

        q = self.online(s).gather(1, a.unsqueeze(1)).squeeze(1)
        with torch.no_grad():
            # Double DQN: select with online net, evaluate with target net.
            next_a = torch.argmax(self.online(s2), dim=1)
            next_q = self.target(s2).gather(1, next_a.unsqueeze(1)).squeeze(1)
            target = r + c.gamma * (1.0 - d) * next_q
        loss = F.smooth_l1_loss(q, target)
        self.optimizer.zero_grad()
        loss.backward()
        torch.nn.utils.clip_grad_norm_(self.online.parameters(), c.gradient_clip)
        self.optimizer.step()
        self.losses.append(float(loss.item()))
        self.max_abs_q = max(self.max_abs_q, float(q.detach().abs().max().item()))
        return float(loss.item())

    def maybe_sync_target(self) -> None:
        if self.steps % self.cfg.target_update_interval == 0:
            self.target.load_state_dict(self.online.state_dict())


def train(cfg: TrainConfig, sim_cfg, env_class=None) -> tuple[DoubleDQN, dict]:
    """Run a training loop and return the agent plus a deterministic report.

    env_class defaults to the v2 SimEnv; v3 passes its own env class so both
    formulations share the same learner.
    """
    if env_class is None:
        env_class = SimEnv
    env = env_class(sim_cfg, seed=cfg.seed)
    in_dim = len(env.state())
    agent = DoubleDQN(in_dim, cfg, seed=cfg.seed)

    episodes = 0
    total_steps = 0
    terminal_episodes = 0
    truncated_episodes = 0
    returns: list[float] = []
    episode_steps: list[int] = []
    q_min, q_max = float("inf"), float("-inf")

    for _ in range(cfg.episodes):
        if cfg.randomize_conditions:
            env.randomize()
        s = env.reset()
        ep_return = 0.0
        steps = 0
        while not env.done():
            a = agent.act(s, explore=True)
            res = env.step(a)
            agent.replay.push(s, a, res["reward"], res["next_state"], res["done"])
            agent.steps += 1
            loss = agent.train_step()
            agent.maybe_sync_target()
            ep_return += res["reward"]
            s = res["next_state"]
            steps += 1
            if steps > 10000:  # absolute safety bound
                break
        episodes += 1
        total_steps += steps
        returns.append(ep_return)
        episode_steps.append(steps)
        if env._done:
            terminal_episodes += 1
        if env._truncated:
            truncated_episodes += 1
        with torch.no_grad():
            x = torch.tensor(s, dtype=torch.float32).unsqueeze(0)
            q = agent.online(x)
            q_min = min(q_min, float(q.min().item()))
            q_max = max(q_max, float(q.max().item()))

    report = {
        "episodes": episodes,
        "total_steps": total_steps,
        "terminal_episodes": terminal_episodes,
        "truncated_episodes": truncated_episodes,
        "final_epsilon": agent.epsilon(),
        "final_loss": agent.losses[-1] if agent.losses else None,
        "mean_loss_last_100": (
            float(np.mean(agent.losses[-100:])) if agent.losses else None
        ),
        "loss_finite": bool(all(np.isfinite(agent.losses))) if agent.losses else None,
        "q_min": q_min,
        "q_max": q_max,
        "q_finite": bool(np.isfinite(q_min) and np.isfinite(q_max)),
        "mean_return": float(np.mean(returns)),
        "replay_size": len(agent.replay),
        # Per-episode traces (additive; used by the v5 pipeline diagnostics).
        "returns": returns,
        "episode_steps": episode_steps,
    }
    return agent, report
