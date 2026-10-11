"""Corrected training environment v3: decision-interval + chunk-level control.

Key v2 failures this addresses:
  * v2 made one RL decision per completed (small) request, so the learned policy
    could not behave like a practical downloader and paid a per-request overhead
    proportional to its request count (57-87 decisions in the final evaluation,
    vs aria2's 4).
  * v2's chunk action space (32/64/128 KiB) could not reach aria2-like granularity.

v3 changes:
  * CHUNK LEVELS: a discrete ladder {32,64,128,256,512,1024} KiB; the action is
    increase/decrease on the ladder, so the controller can reach 1 MiB.
  * DECISION INTERVAL: one RL step covers `decision_interval` completed requests
    (a meaningful control interval), not necessarily one request. The transition
    reward is the negative wall-clock time of the whole interval.
  * REQUEST COST: latency + fixed per-request overhead + transfer time, so many
    small requests genuinely cost more than few large ones. No reward bonus.
  * SHARED LINK + per-connection cap, both throttles, expanded ranges.

Simulator is training-only and is NOT the real evaluation server (which models
no chunk-size tradeoff).
"""

from __future__ import annotations

import random
from dataclasses import dataclass, asdict

from .state import StateSpec, default_state_spec, features

ACTION_NAMES = [
    "none",
    "increase-concurrency",
    "decrease-concurrency",
    "increase-chunk-size",
    "decrease-chunk-size",
]
NUM_ACTIONS = len(ACTION_NAMES)

CHUNK_LEVELS = [32 * 1024, 64 * 1024, 128 * 1024, 256 * 1024, 512 * 1024, 1024 * 1024]


def chunk_index(size: int) -> int:
    best = 0
    for i, c in enumerate(CHUNK_LEVELS):
        if size >= c:
            best = i
    return best


@dataclass
class SimConfig:
    file_size: int = 1024 * 1024
    min_concurrency: int = 1
    max_concurrency: int = 4

    # Link model.
    link_bandwidth: int = 4 * 1024 * 1024
    per_connection_rate: int = 4 * 1024 * 1024

    # Throttle model.
    rate_after: int = 0
    switch_kind: str = "none"  # none | request-count | wall-clock
    switch_after_requests: int = 0
    switch_after_seconds: float = 0.0

    # Per-request cost.
    latency_s: float = 0.02
    overhead_s: float = 0.003
    loss_percent: float = 0.0

    # Decision interval: completed requests per RL step.
    decision_interval: int = 4

    max_episode_steps: int = 200

    # Randomization ranges (training only).
    rand_link_bandwidth: tuple[int, int] = (256 * 1024, 8 * 1024 * 1024)
    rand_per_connection: tuple[int, int] = (256 * 1024, 8 * 1024 * 1024)
    rand_rate_after: tuple[int, int] = (256 * 1024, 4 * 1024 * 1024)
    rand_latency_s: tuple[float, float] = (0.0, 0.08)
    rand_loss_percent: tuple[float, float] = (0.0, 4.0)
    rand_file_size: tuple[int, int] = (256 * 1024, 16 * 1024 * 1024)
    rand_switch_after_requests: tuple[int, int] = (0, 16)
    rand_switch_after_seconds: tuple[float, float] = (0.25, 2.5)

    spec: StateSpec = None  # type: ignore[assignment]

    def to_json(self) -> dict:
        d = asdict(self)
        d["chunk_levels"] = CHUNK_LEVELS
        d["spec"] = (self.spec or default_state_spec()).to_json()
        return d


class SimEnv:
    """Deterministic (seeded) episodic environment with a decision interval."""

    def __init__(self, cfg: SimConfig, seed: int):
        self.cfg = cfg
        self.spec = cfg.spec or default_state_spec()
        self.spec.validate()
        self.rng = random.Random(seed)
        self.seed = seed
        self.reset()

    def randomize(self) -> None:
        c = self.cfg
        c.link_bandwidth = self.rng.randint(*c.rand_link_bandwidth)
        c.per_connection_rate = self.rng.randint(*c.rand_per_connection)
        c.rate_after = self.rng.randint(*c.rand_rate_after)
        c.latency_s = self.rng.uniform(*c.rand_latency_s)
        c.loss_percent = self.rng.uniform(*c.rand_loss_percent)
        c.file_size = self.rng.randint(*c.rand_file_size)
        c.switch_kind = self.rng.choice(["none", "request-count", "wall-clock"])
        c.switch_after_requests = self.rng.randint(*c.rand_switch_after_requests)
        c.switch_after_seconds = self.rng.uniform(*c.rand_switch_after_seconds)

    def current_rate(self) -> int:
        c = self.cfg
        if c.rate_after <= 0 or c.switch_kind == "none":
            return c.link_bandwidth
        if c.switch_kind == "request-count":
            if c.switch_after_requests > 0 and self.requests >= c.switch_after_requests:
                return c.rate_after
            return c.link_bandwidth
        if c.switch_kind == "wall-clock":
            if self.elapsed >= c.switch_after_seconds:
                return c.rate_after
            return c.link_bandwidth
        return c.link_bandwidth

    def per_request_rate(self) -> float:
        return float(
            min(
                self.current_rate() / max(1, self.concurrency),
                self.cfg.per_connection_rate,
            )
        )

    def reset(self) -> list[float]:
        self.concurrency = self.rng.randint(
            self.cfg.min_concurrency, self.cfg.max_concurrency
        )
        self.chunk_idx = 0
        self.total = self.cfg.file_size
        self.done_bytes = 0
        self.requests = 0
        self.steps = 0
        self.elapsed = 0.0
        self._done = False
        self._truncated = False
        self.prev_thr = 0.0
        self.ewma = 0.0
        self.vol = 0.0
        self.prev_dur = 0.0
        self.since_conc = 0.0
        self.since_chunk = 0.0
        self.fail_window: list[int] = []
        self.retry_window: list[int] = []
        # metrics
        self.effective_actions = 0
        self.chunk_time = [0.0] * len(CHUNK_LEVELS)
        self.conc_sum = 0.0
        self.conc_samples = 0
        self.max_conc = self.concurrency
        return self.state()

    @property
    def chunk_size(self) -> int:
        return CHUNK_LEVELS[self.chunk_idx]

    def _obs(self) -> dict:
        return {
            "concurrency": self.concurrency,
            "chunk_size": self.chunk_size,
            "total_bytes": self.total,
            "done_bytes": self.done_bytes,
            "aggregate_throughput": self._aggregate(),
            "throughput_ewma": self.ewma,
            "prev_throughput": self.prev_thr,
            "throughput_vol": self.vol,
            "request_duration_s": self.prev_dur,
            "prev_duration_s": self.prev_dur,
            "since_conc_change_s": self.since_conc,
            "since_chunk_change_s": self.since_chunk,
            "window_failures": sum(self.fail_window),
            "window_retries": sum(self.retry_window),
            "window_attempts": len(self.fail_window),
        }

    def _aggregate(self) -> float:
        return self.concurrency * self.per_request_rate()

    def state(self) -> list[float]:
        return features(self._obs(), self.spec)

    def done(self) -> bool:
        return self._done or self._truncated

    def max_episode_steps(self) -> int:
        return self.cfg.max_episode_steps

    def step(self, action: int):
        if self.done():
            raise RuntimeError("episode already finished")
        if action < 0 or action >= NUM_ACTIONS:
            raise ValueError(f"invalid action {action}")

        before_c, before_k = self.concurrency, self.chunk_idx
        effective = self._apply(action)
        if effective:
            self.effective_actions += 1
        if self.concurrency != before_c:
            self.since_conc = 0.0
        if self.chunk_idx != before_k:
            self.since_chunk = 0.0

        # One RL interval = `decision_interval` completed requests (or until done).
        interval_elapsed = 0.0
        requests_this_interval = 0
        bytes_interval = 0
        failures = 0
        target_requests = self.cfg.decision_interval
        while requests_this_interval < target_requests and self.done_bytes < self.total:
            self.requests += 1
            requests_this_interval += 1
            per_req = self.per_request_rate()
            remaining = self.total - self.done_bytes
            n = min(self.chunk_size, remaining)
            svc = self.cfg.latency_s + self.cfg.overhead_s + n / per_req
            if self.rng.random() * 100 < self.cfg.loss_percent:
                failures += 1
                svc += self.cfg.latency_s + self.cfg.overhead_s + n / per_req
            interval_elapsed += svc
            self.done_bytes += n
            bytes_interval += n
            if self.done_bytes >= self.total:
                break

        thr = bytes_interval / interval_elapsed if interval_elapsed > 0 else 0.0
        self.elapsed += interval_elapsed
        self.since_conc += interval_elapsed
        self.since_chunk += interval_elapsed
        self.prev_thr = thr
        self.ewma = self.spec.ewma_alpha * thr + (1 - self.spec.ewma_alpha) * self.ewma
        self.vol = (
            self.spec.ewma_alpha * abs(thr - self.prev_thr)
            + (1 - self.spec.ewma_alpha) * self.vol
        )
        self.prev_dur = interval_elapsed / max(1, requests_this_interval)
        self._push(self.fail_window, 1 if failures > 0 else 0)
        self._push(self.retry_window, 1 if failures > 0 else 0)
        self.chunk_time[self.chunk_idx] += interval_elapsed
        self.conc_sum += self.concurrency
        self.conc_samples += 1
        self.max_conc = max(self.max_conc, self.concurrency)

        self.steps += 1
        if self.done_bytes >= self.total:
            self._done = True
        if (
            self.cfg.max_episode_steps > 0
            and self.steps >= self.cfg.max_episode_steps
            and not self._done
        ):
            self._truncated = True

        return {
            "next_state": self.state(),
            "reward": -interval_elapsed,
            "done": self.done(),
            "elapsed": interval_elapsed,
            "requests": requests_this_interval,
            "requested_action": action,
            "effective_action": action if effective else -1,
            "no_op": not effective,
            "concurrency": self.concurrency,
            "chunk_size": self.chunk_size,
            "remaining_bytes": self.total - self.done_bytes,
        }

    def _apply(self, action: int) -> bool:
        if action == 1 and self.concurrency < self.cfg.max_concurrency:
            self.concurrency += 1
            return True
        if action == 2 and self.concurrency > self.cfg.min_concurrency:
            self.concurrency -= 1
            return True
        if action == 3 and self.chunk_idx + 1 < len(CHUNK_LEVELS):
            self.chunk_idx += 1
            return True
        if action == 4 and self.chunk_idx - 1 >= 0:
            self.chunk_idx -= 1
            return True
        return False

    def _push(self, window: list[int], v: int) -> None:
        window.append(v)
        if len(window) > self.spec.counter_window:
            del window[: len(window) - self.spec.counter_window]
