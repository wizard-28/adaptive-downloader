"""Concurrency-focused training environment v5 (16 MiB horizon, 1 MiB fixed chunk).

Candidate v5 addresses the v4 limitation: a 4 MiB file gave only 4 x 1 MiB ranges,
an extremely short sequential control horizon, and the agent largely learned a
constant "hold C~=3" policy with corrupted-state performance indistinguishable
from the learned policy.

Design:
  * FILE SIZE default 16 MiB (training also samples 4-32 MiB), CHUNK FIXED at
    1 MiB, CONCURRENCY 1-4. No chunk-size control.
  * The environment supports MULTIPLE within-episode condition transitions at
    randomized times, so the optimal concurrency can change during one episode.
  * The dynamics do NOT encode "latency means increase concurrency" or "loss means
    decrease concurrency" as rules. The optimum arises from the modelled transfer
    mechanics:
      - a shared bottleneck `link_bandwidth` B (bytes/s) that all concurrent
        connections share, versus the per-response throttle `rate`;
      - latency, which more concurrency can overlap (hiding latency);
      - loss/congestion, where extra connections worsen effective loss and a small
        per-extra-connection overhead adds cost.
    Depending on B relative to rate, latency, and loss, the best fixed
    concurrency can vary (a static concurrency sweep over C=1..4 is the intended
    diagnostic; it is not an established result here).

Calibration: the per-response throttle and 1 MiB granularity match the frozen real
server (`1 MiB @ 512 KiB/s ~ 2.05 s`). The shared-bottleneck and per-extra-
connection overhead parameters are MODELLED; dedicated real-server C=1..4
completion measurements are unavailable, so the concurrency relationship is
grounded qualitatively, not fitted (a simulator_spec_v5.json record is still to
be generated).

Training-only. NOT the real server.
"""

from __future__ import annotations

import math
import random
from dataclasses import dataclass, asdict, field

from .state_v5 import StateSpec, default_state_spec, features

# Concurrency-only action space (Candidate v5).
ACTION_NAMES = ["none", "increase-concurrency", "decrease-concurrency"]
NUM_ACTIONS = len(ACTION_NAMES)

FIXED_CHUNK = 1024 * 1024


@dataclass
class Phase:
    """One stationary network phase within an episode."""

    rate: int  # per-response server throttle (bytes/s)
    link_bandwidth: int  # shared bottleneck (bytes/s); 0 = none
    latency_s: float
    loss_percent: float
    congestion: float = 0.0  # per-extra-connection effective-loss increment
    overhead_s: float = 0.02  # per-extra-connection wall-clock overhead


@dataclass
class SimConfig:
    file_size: int = 16 * 1024 * 1024
    min_concurrency: int = 1
    max_concurrency: int = 4

    # Optional fixed single-phase condition (used by evaluation).
    rate: int = 4 * 1024 * 1024
    link_bandwidth: int = 0
    latency_s: float = 0.02
    loss_percent: float = 0.0
    congestion: float = 0.0
    overhead_s: float = 0.02

    # Optional explicitly-scripted phases (evaluation). When empty, the
    # randomized training distribution is used.
    phases: list[Phase] = field(default_factory=list)

    decision_interval: int = 2
    max_episode_steps: int = 400

    # Randomization ranges (training only).
    rand_rate: tuple[int, int] = (256 * 1024, 8 * 1024 * 1024)
    rand_link_mult: tuple[float, float] = (0.0, 4.0)  # B as a multiple of rate
    rand_latency_s: tuple[float, float] = (0.0, 0.08)
    rand_loss_percent: tuple[float, float] = (0.0, 4.0)
    rand_congestion: tuple[float, float] = (0.0, 0.06)
    rand_file_size: tuple[int, int] = (4 * 1024 * 1024, 32 * 1024 * 1024)
    rand_phase_count: tuple[int, int] = (1, 3)  # including stationary (1 phase)
    rand_phase_seconds: tuple[float, float] = (0.3, 2.0)

    spec: StateSpec = None  # type: ignore[assignment]

    def to_json(self) -> dict:
        d = asdict(self)
        d["fixed_chunk"] = FIXED_CHUNK
        d["action_space"] = ACTION_NAMES
        d["spec"] = (self.spec or default_state_spec()).to_json()
        return d


class SimEnv:
    def __init__(self, cfg: SimConfig, seed: int):
        self.cfg = cfg
        self.spec = cfg.spec or default_state_spec()
        self.spec.validate()
        # Record the resolved spec on the config so model export uses the same
        # state contract the environment trained against.
        cfg.spec = self.spec
        self.rng = random.Random(seed)
        self.seed = seed
        self.chunk = FIXED_CHUNK
        self.reset()

    # ---- condition sampling ----
    def _sample_phase(self, allow_link=True) -> Phase:
        c = self.cfg
        rate = self.rng.randint(*c.rand_rate)
        link = 0
        if allow_link:
            mult = self.rng.uniform(*c.rand_link_mult)
            if mult > 0:
                link = max(1, int(rate * mult))
        return Phase(
            rate=rate,
            link_bandwidth=link,
            latency_s=self.rng.uniform(*c.rand_latency_s),
            loss_percent=self.rng.uniform(*c.rand_loss_percent),
            congestion=self.rng.uniform(*c.rand_congestion),
            overhead_s=0.02,
        )

    def randomize(self) -> None:
        c = self.cfg
        c.file_size = self.rng.randint(*c.rand_file_size)
        n = self.rng.randint(*c.rand_phase_count)
        c.phases = [self._sample_phase() for _ in range(n)]

    # ---- episode setup ----
    def reset(self) -> list[float]:
        c = self.cfg
        if not c.phases:
            if c.rate > 0:
                c.phases = [
                    Phase(
                        rate=c.rate,
                        link_bandwidth=c.link_bandwidth,
                        latency_s=c.latency_s,
                        loss_percent=c.loss_percent,
                        congestion=c.congestion,
                        overhead_s=c.overhead_s,
                    )
                ]
            else:
                c.phases = [self._sample_phase()]
        # Assign randomized durations to phase boundaries (wall-clock seconds).
        self.phase_ends: list[float] = []
        t = 0.0
        for i, ph in enumerate(c.phases):
            if i == len(c.phases) - 1 and len(c.phases) > 1:
                # last phase runs to completion
                self.phase_ends.append(float("inf"))
            else:
                dur = self.rng.uniform(*c.rand_phase_seconds)
                t += dur
                self.phase_ends.append(t)

        self.concurrency = self.rng.randint(c.min_concurrency, c.max_concurrency)
        self.total = c.file_size
        self.done_bytes = 0
        self.requests = 0
        self.steps = 0
        self.elapsed = 0.0
        self.phase_index = 0
        self.transitions = 0
        self._done = False
        self._truncated = False

        self.prev_thr = 0.0
        self.ewma = 0.0
        self.vol = 0.0
        self.last_dur = 0.0
        self.prev_dur = 0.0
        self.since_conc = 0.0
        self.fail_window: list[int] = []
        self.retry_window: list[int] = []
        self.done_window: list[int] = []

        self.effective_actions = 0
        self.conc_sum = 0.0
        self.conc_samples = 0
        self.max_conc = self.concurrency

        self.conc_trajectory: list[tuple[float, int]] = [(0.0, self.concurrency)]
        self.action_history: list[int] = []
        return self.state()

    # ---- phase / dynamics ----
    def _phase_index_at(self, t: float) -> int:
        idx = 0
        while idx < len(self.cfg.phases) - 1 and t >= self.phase_ends[idx]:
            idx += 1
        return idx

    def _current_phase(self) -> Phase:
        idx = self._phase_index_at(self.elapsed)
        if idx != self.phase_index:
            self.transitions += idx - self.phase_index
            self.phase_index = idx
        return self.cfg.phases[idx]

    def _per_response_rate(self, C: int, ph: Phase) -> float:
        per = float(ph.rate)
        if ph.link_bandwidth > 0:
            per = min(per, float(ph.link_bandwidth) / max(1, C))
        return per

    def _loss_effective(self, C: int, ph: Phase) -> float:
        return min(0.9, ph.loss_percent / 100.0 + ph.congestion * (C - 1))

    # ---- observation / state ----
    def _obs(self) -> dict:
        ph = self._current_phase()
        C = self.concurrency
        le = self._loss_effective(C, ph)
        per = self._per_response_rate(C, ph)
        n = min(self.chunk, self.total - self.done_bytes)
        svc = ph.latency_s + ph.overhead_s * (C - 1) + (n / (1 - le)) / per
        agg = C * n / svc if svc > 0 else 0.0
        return {
            "concurrency": C,
            "total_bytes": self.total,
            "done_bytes": self.done_bytes,
            "aggregate_throughput": agg,
            "throughput_ewma": self.ewma,
            "prev_throughput": self.prev_thr,
            "throughput_vol": self.vol,
            "request_duration_s": self.last_dur,
            "prev_duration_s": self.prev_dur,
            "since_conc_change_s": self.since_conc,
            "window_failures": sum(self.fail_window),
            "window_retries": sum(self.retry_window),
            "window_attempts": len(self.fail_window),
            "window_completed": sum(self.done_window),
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
            self.conc_trajectory.append((self.elapsed, self.concurrency))
        self.action_history.append(action)

        # One decision interval completes `decision_interval` requests. The
        # `concurrency` workers run continuously, including across decision
        # boundaries: a non-final interval's wall-clock is the pipeline time
        # sum(service_times)/concurrency, so concurrency always changes the
        # aggregate transfer rate. Only an interval that finishes the file can
        # use at most the remaining requests as active workers. The phase is
        # selected from the elapsed time at each request's start, so a
        # wall-clock transition applies mid-step.
        remaining_reqs = math.ceil((self.total - self.done_bytes) / self.chunk)
        reqs = min(self.cfg.decision_interval, remaining_reqs)
        active = (
            self.concurrency
            if remaining_reqs > reqs
            else max(1, min(self.concurrency, reqs))
        )

        bytes_interval = 0
        accrued = 0.0
        last_svc = 0.0
        for _ in range(reqs):
            ph = self.cfg.phases[self._phase_index_at(self.elapsed + accrued)]
            le = self._loss_effective(self.concurrency, ph)
            per = self._per_response_rate(self.concurrency, ph)
            n = min(self.chunk, self.total - self.done_bytes)
            svc = ph.latency_s + ph.overhead_s * (self.concurrency - 1) + n / per
            self.requests += 1
            failed = self.rng.random() < le
            if failed:
                svc += ph.latency_s + n / per  # one retransmission
            self.done_bytes += n
            bytes_interval += n
            accrued += svc / active
            last_svc = svc
            # Window samples are per ATTEMPT, matching the live runner
            # (policy_runner.observe): a failed range is one failed attempt
            # (failure=1, retry=0, completed=0) followed by one successful
            # retry attempt (failure=0, retry=1, completed=1). The bytes are
            # credited exactly once, by the successful retry attempt.
            if failed:
                self._push(self.fail_window, 1)
                self._push(self.retry_window, 0)
                self._push(self.done_window, 0)
                self._push(self.fail_window, 0)
                self._push(self.retry_window, 1)
                self._push(self.done_window, 1)
            else:
                self._push(self.fail_window, 0)
                self._push(self.retry_window, 0)
                self._push(self.done_window, 1)
        interval_elapsed = accrued

        # Advance the phase index to the end-of-step elapsed time; this counts
        # any boundary crossed during the step.
        self.elapsed += interval_elapsed
        self._current_phase()

        thr = bytes_interval / interval_elapsed if interval_elapsed > 0 else 0.0
        self.since_conc += interval_elapsed
        self.prev_dur = self.last_dur
        self.last_dur = last_svc
        self.vol = self.spec.ewma_alpha * abs(thr - self.prev_thr) + (
            1 - self.spec.ewma_alpha
        ) * self.vol
        self.ewma = (
            self.spec.ewma_alpha * thr + (1 - self.spec.ewma_alpha) * self.ewma
        )
        self.prev_thr = thr
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
            "remaining_bytes": self.total - self.done_bytes,
            "phase_index": self.phase_index,
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
