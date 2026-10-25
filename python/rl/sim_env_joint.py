"""Joint concurrency + range-size environment for the capped server.

Extends the event-driven capped-server model of sim_env_cap (same server
model and calibration) to the joint action space of the v1 live adapter
(`dqn.NewLivePolicy`, state spec version 1, 5 actions):

  * the range size S is a controller target read at each allocation
    (internal/chunksize: additive +-step within [min, max]); a change affects
    future allocations only, never an in-flight range;
  * allocation takes min(S, remaining unallocated bytes) from the front;
  * straggler splitting (downloader.Config.SplitStragglers): when nothing is
    pending, a worker whose range just completed takes the second half of the
    unreceived part of the in-flight range with the most unreceived bytes, if
    at least 2 x SplitMinBytes (1 MiB) remain; the victim's end moves to the
    split point and the new piece is a new request (new rate at arrival);
  * observables follow `LivePolicy.observe` (v1): concurrency, chunk size,
    progress, windowed aggregate, request duration, EWMA/slope/volatility,
    failure/retry windows, and time since the last concurrency and chunk-size
    change.

Training-only: NOT the real server. sim_env_cap stays unchanged (it backs
the frozen rl_cap_train study).
"""

from __future__ import annotations

import heapq
import random
from dataclasses import asdict, dataclass, field

from .sim_env_cap import KIB, MIB, Condition, SimConfig as CapSimConfig, SimEnv as CapSimEnv
from .state import StateSpec, features

ACTION_NAMES = ["none", "increase-concurrency", "decrease-concurrency",
                "increase-chunk-size", "decrease-chunk-size"]
NUM_ACTIONS = len(ACTION_NAMES)
SPLIT_MIN = MIB


def joint_spec() -> StateSpec:
    """State spec v1 sized for the capped server with ranges up to 33 MiB."""
    return StateSpec(min_concurrency=1, max_concurrency=16, min_chunk_size=MIB,
                     max_chunk_size=33 * MIB, throughput_scale=32.0 * MIB,
                     duration_scale=8.0, staleness_scale=10.0, ewma_alpha=0.3, counter_window=8)


@dataclass
class JointConfig(CapSimConfig):
    min_chunk: int = MIB
    max_chunk: int = 33 * MIB
    chunk_step: int = 4 * MIB
    start_chunk: int | None = None  # None: sampled in training from the grid
    split: bool = True
    spec: StateSpec = field(default_factory=joint_spec)

    def to_json(self) -> dict:
        d = asdict(self)
        d.pop("calib", None)
        if d.get("condition"):
            d["condition"].pop("calib", None)
        d["calibrated"] = self.calib is not None
        d["action_space"] = ACTION_NAMES
        d["spec"] = self.spec.to_json()
        return d


class _Req:
    __slots__ = ("rid", "start", "length", "t_send", "arrive", "rate", "ex", "back", "capped", "ver")

    def srv_end(self) -> float:
        return self.arrive + self.length / self.rate + self.ex

    def t_done(self) -> float:
        return self.srv_end() + self.back

    def received(self, t: float, owd: float) -> int:
        """Bytes the client holds at time t (body streams from arrival)."""
        got = int((t - self.arrive - owd) * self.rate)
        return max(0, min(self.length, got))


class SimEnv(CapSimEnv):
    """Gym-like joint environment used by rl.dqn.train."""

    def __init__(self, cfg: JointConfig, seed: int):
        self._start_chunk = cfg.start_chunk
        super().__init__(cfg, seed)

    def chunk_grid(self) -> list[int]:
        c = self.cfg
        return list(range(c.min_chunk, c.max_chunk + 1, c.chunk_step))

    def randomize(self) -> None:
        super().randomize()
        self._start_chunk = self.rng.choice(self.chunk_grid())

    # ---- server ----
    def _send(self, t_send: float, start: int, length: int) -> None:
        c = self._cond
        r = _Req()
        self.seq += 1
        r.rid, r.start, r.length, r.t_send, r.ver = self.seq, start, length, t_send, 0
        r.arrive = t_send + c.owd_s
        rate = float(c.base_rate(r.arrive))
        r.capped = c.cap_at is not None and r.arrive >= c.cap_at
        if r.capped:
            k = 1 + sum(1 for q in self.inflight.values() if q.capped and q.srv_end() > r.arrive)
            rate = min(rate, self._cap_rate(k))
        r.rate = rate
        r.ex, r.back = c.sample(self.rng, rate)
        self.inflight[r.rid] = r
        heapq.heappush(self.events, (r.t_done(), r.rid, r.ver))

    def _allocate(self, t: float) -> bool:
        """Allocate the next range at the current chunk target; False if none."""
        if self.allocated >= self.total:
            return False
        n = min(self.chunk, self.total - self.allocated)
        self._send(t, self.allocated, n)
        self.allocated += n
        return True

    def _split(self, t: float) -> bool:
        if not self.cfg.split:
            return False
        best, best_rem, best_recv = None, 0, 0
        for q in self.inflight.values():
            recv = q.received(t, self._cond.owd_s)
            rem = q.length - recv
            if rem > best_rem:
                best, best_rem, best_recv = q, rem, recv
        if best is None or best_rem < 2 * SPLIT_MIN:
            return False
        at = best.start + best_recv + best_rem // 2
        tail = best.start + best.length - at
        best.length = at - best.start
        best.ver += 1
        heapq.heappush(self.events, (best.t_done(), best.rid, best.ver))
        self.splits += 1
        self._send(t, at, tail)
        return True

    def _work(self, t: float) -> bool:
        """A free worker takes a range (or a split); False if it must exit."""
        return self._allocate(t) or self._split(t)

    # ---- episode ----
    def reset(self) -> list[float]:
        c = self._cond
        cfg = self.cfg
        n0 = self._start_n if self._start_n is not None else self.rng.randint(
            cfg.min_concurrency, cfg.max_concurrency)
        self.chunk = self._start_chunk if self._start_chunk is not None else cfg.min_chunk
        self.total = c.file_size
        self.allocated = 0
        self.done_bytes_true = 0
        self.events: list = []
        self.inflight: dict[int, _Req] = {}
        self.seq = 0
        self.splits = 0
        self.target = n0
        self.active = 0
        self.recent: list[tuple[int, float, float]] = []
        self.steps = 0
        self.attempts = 0
        self._done = False
        self._truncated = False
        self.now = c.client_start_s
        self.ewma = self.vol = self.prev_agg = self.prev_dur = 0.0
        self.since_conc = self.since_chunk = 0.0
        self.prev_conc = 0
        self.prev_chunk = 0
        self.fail_w: list[int] = []
        self.retry_w: list[int] = []
        self.done_bytes_obs = 0
        self.trajectory: list[tuple[float, int, int]] = [(self.now, n0, self.chunk)]
        self.rates: list[float] = []
        for _ in range(n0):
            if self._work(self.now):
                self.active += 1
        self._state = self._advance()
        return self._state

    def _observe(self, nbytes: int, dur: float, end: float) -> list[float]:
        """policyRunner.observe + LivePolicy.observe (v1) for one attempt."""
        self.attempts += 1
        self.recent.append((nbytes, dur, end))
        if len(self.recent) > self.cfg.window:
            self.recent = self.recent[-self.cfg.window:]
        earliest = min(e - d for _, d, e in self.recent)
        latest = max(e for _, _, e in self.recent)
        span = latest - earliest
        agg = sum(b for b, _, _ in self.recent) / span if span > 0 else 0.0
        conc, chunk = self.target, self.chunk
        self.done_bytes_obs += nbytes
        self._push(self.fail_w, 0)
        self._push(self.retry_w, 0)
        step = dur if dur > 0 else 0.001
        self.since_conc = self.since_conc + step if conc == self.prev_conc else 0.0
        self.since_chunk = self.since_chunk + step if chunk == self.prev_chunk else 0.0
        obs = {
            "concurrency": conc, "chunk_size": chunk, "total_bytes": self.total,
            "done_bytes": min(self.done_bytes_obs, self.total),
            "aggregate_throughput": agg, "throughput_ewma": self.ewma,
            "prev_throughput": self.prev_agg, "throughput_vol": self.vol,
            "request_duration_s": dur, "prev_duration_s": self.prev_dur,
            "since_conc_change_s": self.since_conc, "since_chunk_change_s": self.since_chunk,
            "failed_requests": 0, "retries": 0, "total_attempts": self.attempts,
            "window_failures": sum(self.fail_w), "window_retries": sum(self.retry_w),
            "window_attempts": len(self.fail_w),
        }
        a = self.spec.ewma_alpha
        self.vol = a * abs(agg - self.prev_agg) + (1 - a) * self.vol
        self.ewma = a * agg + (1 - a) * self.ewma
        self.prev_agg, self.prev_dur = agg, dur
        self.prev_conc, self.prev_chunk = conc, chunk
        self.last_obs = obs
        return features(obs, self.spec)

    def _advance(self) -> list[float] | None:
        while self.events:
            t_done, rid, ver = heapq.heappop(self.events)
            r = self.inflight.get(rid)
            if r is None or r.ver != ver:
                continue  # stale entry from before a split
            del self.inflight[rid]
            self.now = t_done
            self.done_bytes_true += r.length
            self.rates.append(r.rate)
            return self._observe(r.length, t_done - r.t_send, t_done)
        self._done = True
        return None

    def step(self, action: int) -> dict:
        if self.done():
            raise RuntimeError("episode already finished")
        if action < 0 or action >= NUM_ACTIONS:
            raise ValueError(f"invalid action {action}")
        cfg = self.cfg
        before = (self.target, self.chunk)
        if action == 1 and self.target < cfg.max_concurrency:
            self.target += 1
        elif action == 2 and self.target > cfg.min_concurrency:
            self.target -= 1
        elif action == 3:
            self.chunk = min(cfg.max_chunk, self.chunk + cfg.chunk_step)
        elif action == 4:
            self.chunk = max(cfg.min_chunk, self.chunk - cfg.chunk_step)
        effective = (self.target, self.chunk) != before
        if effective:
            self.trajectory.append((self.now, self.target, self.chunk))
        # the completing worker: retire if over target, else take work
        if self.active > self.target or not self._work(self.now):
            self.active -= 1
        # reconcile: start workers while below target and bytes are pending
        while self.active < self.target and self.allocated < self.total:
            self._allocate(self.now)
            self.active += 1
        t0 = self.now
        self.steps += 1
        nxt = self._advance()
        self._state = nxt if nxt is not None else [0.0] * 13
        if cfg.max_episode_steps > 0 and self.steps >= cfg.max_episode_steps and not self._done:
            self._truncated = True
        return {"next_state": self.state(), "reward": -(self.now - t0), "done": self.done(),
                "elapsed": self.now - t0, "concurrency": self.target, "chunk": self.chunk,
                "effective_action": action if effective else -1, "no_op": not effective}


def run_joint(cond: Condition, start_n: int, start_chunk: int, act, window: int = 16,
              seed: int = 0, **kw) -> SimEnv:
    cfg = JointConfig(window=window, start_n=start_n, start_chunk=start_chunk, condition=cond, **kw)
    env = SimEnv(cfg, seed)
    while not env.done():
        env.step(act(env.state(), env))
    return env
