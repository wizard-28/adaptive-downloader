"""Corrected training environment for the DQN controller (v2).

Differences from the v1 analytical simulator:
  * SHARED BANDWIDTH: a total link bandwidth is divided across the active
    requests, each capped by a per-connection/server ceiling. Increasing
    concurrency therefore raises aggregate throughput only up to the shared
    cap (diminishing/negative returns), so concurrency actions matter.
  * REQUEST OVERHEAD: every request pays a fixed per-request setup overhead
    (latency + scheduling overhead). Larger chunks amortize this over more
    bytes, so chunk size has a genuine tradeoff. No reward bonus is added.
  * TWO THROTTLE MODELS: request-count switching and WALL-CLOCK switching, the
    latter driven by a continuous simulated clock (elapsed time), not requests.
  * CONTINUOUS CLOCK: every step advances simulated time; the wall-clock rate is
    selected from elapsed time.

TRAINING environment only. The REAL evaluation server's chunk-size tradeoff
remains limited (see docs/PROJECT_CONTEXT.md); this simulator deliberately adds
mechanics the real server does not model, so a chunk policy has something to
learn. Training conditions are NOT the final evaluation conditions.
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


@dataclass
class SimConfig:
    file_size: int = 512 * 1024
    min_concurrency: int = 1
    max_concurrency: int = 4
    min_chunk_size: int = 32 * 1024
    max_chunk_size: int = 128 * 1024
    chunk_step: int = 32 * 1024

    # Shared bandwidth: total link rate (bytes/s) and per-connection ceiling.
    link_bandwidth: int = 4 * 1024 * 1024
    per_connection_rate: int = 4 * 1024 * 1024

    # Optional rate transition.
    rate_after: int = 0  # 0 = no transition
    switch_kind: str = "none"  # "none" | "request-count" | "wall-clock"
    switch_after_requests: int = 0
    switch_after_seconds: float = 0.0

    # Per-request overhead (seconds): latency + scheduling.
    latency_s: float = 0.02
    overhead_s: float = 0.002

    loss_percent: float = 0.0
    max_episode_steps: int = 120

    # Randomization ranges (used by randomize()). Training-only.
    rand_link_bandwidth: tuple[int, int] = (512 * 1024, 8 * 1024 * 1024)
    rand_per_connection: tuple[int, int] = (512 * 1024, 8 * 1024 * 1024)
    rand_rate_after: tuple[int, int] = (256 * 1024, 4 * 1024 * 1024)
    rand_latency_s: tuple[float, float] = (0.0, 0.08)
    rand_loss_percent: tuple[float, float] = (0.0, 4.0)
    rand_file_size: tuple[int, int] = (256 * 1024, 4 * 1024 * 1024)
    rand_switch_after_requests: tuple[int, int] = (0, 12)
    rand_switch_after_seconds: tuple[float, float] = (0.2, 2.0)
    rand_initial_chunk: bool = True

    spec: StateSpec = None  # type: ignore[assignment]

    def to_json(self) -> dict:
        d = asdict(self)
        d["spec"] = (self.spec or default_state_spec()).to_json()
        return d


class SimEnv:
    """Deterministic (seeded) episodic environment implementing the RL contract."""

    def __init__(self, cfg: SimConfig, seed: int):
        self.cfg = cfg
        self.spec = cfg.spec or default_state_spec()
        self.spec.validate()
        self.seed = seed
        self.rng = random.Random(seed)
        self.reset()

    # --- rate selection on the continuous clock ---
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

    def randomize(self) -> None:
        """Randomize episode conditions within the configured ranges (training)."""
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

    def reset(self) -> list[float]:
        self.concurrency = self.rng.randint(
            self.cfg.min_concurrency, self.cfg.max_concurrency
        )
        self.chunk_size = self.cfg.min_chunk_size
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
        return self.state()

    # --- dynamics ---
    def per_request_rate(self) -> float:
        """Per-request rate under shared bandwidth with a per-connection cap.

        Total available rate is the *current* link rate (which may have switched
        by the throttle model), divided equally across active requests, but no
        request may exceed the per-connection/server ceiling. At low concurrency
        a single request is capped by per_connection_rate; at higher concurrency
        the shared link becomes the binding constraint.
        """
        rate = min(
            self.current_rate() / max(1, self.concurrency),
            self.cfg.per_connection_rate,
        )
        return float(rate)

    def request_service_time(self, chunk: int) -> float:
        """latency + overhead + transfer time for one request of `chunk` bytes."""
        return (
            self.cfg.latency_s + self.cfg.overhead_s + chunk / self.per_request_rate()
        )

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
        c = self.concurrency
        return c * self.per_request_rate()

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

        before_c, before_k = self.concurrency, self.chunk_size
        self._apply(action)
        noop = self._is_noop(action, before_c, before_k)
        if self.concurrency != before_c:
            self.since_conc = 0.0
        if self.chunk_size != before_k:
            self.since_chunk = 0.0

        # One request wave: each worker runs one request concurrently. The wave's
        # wall-clock time is the slowest request (requests overlap), not the sum.
        # Each worker still transfers `chunk_size` bytes, so the wave's aggregate
        # throughput = concurrency * per-request rate (bounded by the shared
        # link / per-connection cap modeled in per_request_rate()).
        wave_elapsed = 0.0
        bytes_wave = 0
        failures = 0
        for _ in range(self.concurrency):
            if self.done_bytes >= self.total:
                break
            self.requests += 1
            per_req = self.per_request_rate()
            remaining = self.total - self.done_bytes
            n = min(self.chunk_size, remaining)
            svc = self.cfg.latency_s + self.cfg.overhead_s + n / per_req
            failed = self.rng.random() * 100 < self.cfg.loss_percent
            if failed:
                failures += 1
                svc += self.cfg.latency_s + self.cfg.overhead_s + n / per_req
            if svc > wave_elapsed:
                wave_elapsed = svc
            self.done_bytes += n
            bytes_wave += n
            if self.done_bytes >= self.total:
                break
        elapsed = wave_elapsed

        thr = bytes_wave / elapsed if elapsed > 0 else 0.0
        self.elapsed += elapsed
        self.since_conc += elapsed
        self.since_chunk += elapsed
        self.prev_thr = thr
        self.ewma = self.spec.ewma_alpha * thr + (1 - self.spec.ewma_alpha) * self.ewma
        self.vol = (
            self.spec.ewma_alpha * abs(thr - self.prev_thr)
            + (1 - self.spec.ewma_alpha) * self.vol
        )
        self.prev_dur = elapsed
        self._push(self.fail_window, 1 if failures > 0 else 0)
        self._push(self.retry_window, 1 if failures > 0 else 0)

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
            "reward": -elapsed,
            "done": self._done or self._truncated,
            "elapsed": elapsed,
            "requested_action": action,
            "effective_action": action,
            "no_op": noop,
            "concurrency": self.concurrency,
            "chunk_size": self.chunk_size,
            "remaining_bytes": self.total - self.done_bytes,
        }

    def _apply(self, action: int) -> None:
        if action == 1 and self.concurrency < self.cfg.max_concurrency:
            self.concurrency += 1
        elif action == 2 and self.concurrency > self.cfg.min_concurrency:
            self.concurrency -= 1
        elif (
            action == 3
            and self.chunk_size + self.cfg.chunk_step <= self.cfg.max_chunk_size
        ):
            self.chunk_size += self.cfg.chunk_step
        elif (
            action == 4
            and self.chunk_size - self.cfg.chunk_step >= self.cfg.min_chunk_size
        ):
            self.chunk_size -= self.cfg.chunk_step

    def _is_noop(self, action: int, before_c: int, before_k: int) -> bool:
        if action == 0:
            return True
        if action in (1, 2):
            return self.concurrency == before_c
        return self.chunk_size == before_k

    def _push(self, window: list[int], v: int) -> None:
        window.append(v)
        if len(window) > self.spec.counter_window:
            del window[: len(window) - self.spec.counter_window]
