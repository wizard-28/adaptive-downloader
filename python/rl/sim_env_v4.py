"""Concurrency-focused training environment v4 (chunk fixed at 1 MiB).

The final RL formulation removes chunk-size control entirely:
  * CHUNK IS FIXED at 1 MiB (matched to the aria2 baseline) for every policy;
  * the only learned variable is CONCURRENCY (1-4).

This removes the v3 sim-to-real failure, where the simulator rewarded larger
chunks but the real server models no chunk-size tradeoff. Here request
granularity is identical across DQN and every baseline.

Dynamics (calibrated to the frozen real server, see simulator_spec_v4.json):
  * the server paces EACH response independently at `rate` bytes/s (application
    throttling), so one request of `chunk` bytes takes
    `latency + chunk/rate` (independent of concurrency);
  * `concurrency` requests overlap, so wall-clock for a wave of C requests is the
    slowest request, and aggregate throughput scales with C up to the point where
    loss/retry contention and latency dominate.
  * under a shared/server bottleneck modeled as a `per_connection_rate` cap below
    the link rate, higher concurrency still helps by overlap; a `link_bandwidth`
    cap below `C*rate` models a shared bottleneck where extra concurrency stops
    helping.
  * rate may switch by request count or wall-clock elapsed time.

Training-only. NOT the real server.
"""

from __future__ import annotations

import random
from dataclasses import dataclass, asdict

from .state import StateSpec, default_state_spec, features

# Concurrency-only action space (Candidate v4).
ACTION_NAMES = ["none", "increase-concurrency", "decrease-concurrency"]
NUM_ACTIONS = len(ACTION_NAMES)

FIXED_CHUNK = 1024 * 1024


@dataclass
class SimConfig:
    file_size: int = 4 * 1024 * 1024
    min_concurrency: int = 1
    max_concurrency: int = 4

    # Server-side per-response pacing rate (bytes/s) and an optional shared-link
    # cap (bytes/s) below C*rate.
    rate: int = 4 * 1024 * 1024
    link_bandwidth: int = 0  # 0 = no shared cap (per-request rate binds)

    rate_after: int = 0
    switch_kind: str = "none"  # none | request-count | wall-clock
    switch_after_requests: int = 0
    switch_after_seconds: float = 0.0

    latency_s: float = 0.02
    loss_percent: float = 0.0
    retry_delay_s: float = 0.0

    decision_interval: int = 4
    max_episode_steps: int = 200

    # Randomization ranges (training only).
    rand_rate: tuple[int, int] = (256 * 1024, 8 * 1024 * 1024)
    rand_link_bandwidth: tuple[int, int] = (0, 8 * 1024 * 1024)
    rand_rate_after: tuple[int, int] = (256 * 1024, 4 * 1024 * 1024)
    rand_latency_s: tuple[float, float] = (0.0, 0.08)
    rand_loss_percent: tuple[float, float] = (0.0, 4.0)
    rand_file_size: tuple[int, int] = (256 * 1024, 16 * 1024 * 1024)
    rand_switch_after_requests: tuple[int, int] = (0, 16)
    rand_switch_after_seconds: tuple[float, float] = (0.25, 2.5)

    spec: StateSpec = None  # type: ignore[assignment]

    def to_json(self) -> dict:
        d = asdict(self)
        d["fixed_chunk"] = FIXED_CHUNK
        d["spec"] = (self.spec or default_state_spec()).to_json()
        return d


class SimEnv:
    def __init__(self, cfg: SimConfig, seed: int):
        self.cfg = cfg
        self.spec = cfg.spec or default_state_spec()
        self.spec.validate()
        self.rng = random.Random(seed)
        self.seed = seed
        self.chunk = FIXED_CHUNK
        self.reset()

    def randomize(self) -> None:
        c = self.cfg
        c.rate = self.rng.randint(*c.rand_rate)
        lo, hi = c.rand_link_bandwidth
        c.link_bandwidth = 0 if hi <= 0 else self.rng.randint(lo, hi)
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
            return c.rate
        if c.switch_kind == "request-count":
            if c.switch_after_requests > 0 and self.requests >= c.switch_after_requests:
                return c.rate_after
            return c.rate
        if c.switch_kind == "wall-clock":
            if self.elapsed >= c.switch_after_seconds:
                return c.rate_after
            return c.rate
        return c.rate

    def reset(self) -> list[float]:
        self.concurrency = self.rng.randint(
            self.cfg.min_concurrency, self.cfg.max_concurrency
        )
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
        self.fail_window: list[int] = []
        self.retry_window: list[int] = []
        self.effective_actions = 0
        self.conc_sum = 0.0
        self.conc_samples = 0
        self.max_conc = self.concurrency
        return self.state()

    # ---- dynamics ----
    def _per_request_rate(self) -> float:
        rate = float(self.current_rate())
        if self.cfg.link_bandwidth > 0:
            rate = min(rate, self.cfg.link_bandwidth / max(1, self.concurrency))
        return rate

    def _aggregate(self) -> float:
        return self.concurrency * self._per_request_rate()

    def _obs(self) -> dict:
        return {
            "concurrency": self.concurrency,
            "chunk_size": self.chunk,
            "total_bytes": self.total,
            "done_bytes": self.done_bytes,
            "aggregate_throughput": self._aggregate(),
            "throughput_ewma": self.ewma,
            "prev_throughput": self.prev_thr,
            "throughput_vol": self.vol,
            "request_duration_s": self.prev_dur,
            "prev_duration_s": self.prev_dur,
            "since_conc_change_s": self.since_conc,
            "since_chunk_change_s": 0.0,
            "window_failures": sum(self.fail_window),
            "window_retries": sum(self.retry_window),
            "window_attempts": len(self.fail_window),
        }

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

        before = self.concurrency
        effective = self._apply(action)
        if effective:
            self.effective_actions += 1
        if self.concurrency != before:
            self.since_conc = 0.0

        # One RL interval = a wave of up to `decision_interval` requests executed
        # with `concurrency` workers in PARALLEL. Requests overlap in wall-clock:
        # the wave's elapsed time is the slowest request, and aggregate throughput
        # = bytes_transferred / wave_elapsed. This is why more concurrency can
        # help (more overlap) up to the shared-link cap.
        bytes_interval = 0
        failures = 0
        wave_elapsed = 0.0
        reqs = 0
        remaining_in_interval = self.cfg.decision_interval
        while remaining_in_interval > 0 and self.done_bytes < self.total:
            wave = min(self.concurrency, remaining_in_interval)
            if self.done_bytes + wave * self.chunk > self.total:
                wave = max(
                    1, (self.total - self.done_bytes + self.chunk - 1) // self.chunk
                )
            this_wave_elapsed = 0.0
            for _ in range(int(wave)):
                if self.done_bytes >= self.total:
                    break
                self.requests += 1
                reqs += 1
                per_req = self._per_request_rate()
                remaining = self.total - self.done_bytes
                n = min(self.chunk, remaining)
                svc = self.cfg.latency_s + n / per_req
                if self.rng.random() * 100 < self.cfg.loss_percent:
                    failures += 1
                    svc += self.cfg.retry_delay_s + self.cfg.latency_s + n / per_req
                # parallel: wave time is the slowest request
                this_wave_elapsed = max(this_wave_elapsed, svc)
                self.done_bytes += n
                bytes_interval += n
            wave_elapsed += this_wave_elapsed
            remaining_in_interval -= int(wave)
        interval_elapsed = wave_elapsed

        thr = bytes_interval / interval_elapsed if interval_elapsed > 0 else 0.0
        self.elapsed += interval_elapsed
        self.since_conc += interval_elapsed
        self.prev_thr = thr
        self.ewma = self.spec.ewma_alpha * thr + (1 - self.spec.ewma_alpha) * self.ewma
        self.vol = (
            self.spec.ewma_alpha * abs(thr - self.prev_thr)
            + (1 - self.spec.ewma_alpha) * self.vol
        )
        self.prev_dur = interval_elapsed / max(1, reqs)
        self._push(self.fail_window, 1 if failures > 0 else 0)
        self._push(self.retry_window, 1 if failures > 0 else 0)
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
            "requests": reqs,
            "requested_action": action,
            "effective_action": action if effective else -1,
            "no_op": not effective,
            "concurrency": self.concurrency,
            "chunk_size": self.chunk,
            "remaining_bytes": self.total - self.done_bytes,
        }

    def _apply(self, action: int) -> bool:
        if action == 1 and self.concurrency < self.cfg.max_concurrency:
            self.concurrency += 1
            return True
        if action == 2 and self.concurrency > self.cfg.min_concurrency:
            self.concurrency -= 1
            return True
        return False

    def _push(self, window: list[int], v: int) -> None:
        window.append(v)
        if len(window) > self.spec.counter_window:
            del window[: len(window) - self.spec.counter_window]
