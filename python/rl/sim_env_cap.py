"""Capped-server training environment for the DQN retrain (study rl_cap_train).

An event-driven model of the live rl_bench setup: the controlled server with a
per-response pacing schedule and the soft N-dependent concurrency cap, driven by
the downloader's worker pool and serialized policy runner. Unlike sim_env_v6,
it reproduces the live runner's observables exactly:

  * one decision per completed attempt (policyRunner.observe);
  * AggregateThroughput = bytes of the last `window` attempts / (latest end -
    earliest start), as policyRunner.aggregateThroughput;
  * RequestDuration = the completing attempt's client-side duration;
  * the LivePolicy v6 trackers (EWMA, volatility, staleness, counter windows,
    progress) updated in the same order as live.go observeV5.

Server model (internal/experiment/server):
  * a request reaches the server one-way delay after it is sent;
  * its rate is fixed at arrival: the base schedule rate, lowered to
    C / (k (1 + gamma (k - 1))) when the cap is active, where k counts the
    capped requests in service including this one (requests that arrived
    before the cap switched on are not counted);
  * service time = bytes / rate + excess(rate); the client sees it one-way
    delay later.

Worker pool (internal/downloader/worker_pool.go):
  * an increase starts one worker at once while ranges remain;
  * a decrease retires the next worker that completes (checked after its
    decision, before it allocates);
  * 1 MiB ranges never split (a split needs 2 x SplitMinBytes unreceived), so
    splitting is not modelled; no failures are modelled (0 failures in the
    calibration runs).

Calibration constants come from the server request logs of
testdata/rl_capped/runs (see testdata/rl_cap_train/protocol.md), not from
completion times. Training-only: NOT the real server.
"""

from __future__ import annotations

import heapq
import math
import random
from dataclasses import asdict, dataclass, field

from .state_v6 import StateSpec, features

ACTION_NAMES = ["none", "increase-concurrency", "decrease-concurrency"]
NUM_ACTIONS = len(ACTION_NAMES)

MIB = 1024 * 1024
KIB = 1024
FIXED_CHUNK = MIB


def cap_spec() -> StateSpec:
    """State spec v3 (same 13-feature layout as v6) with scales sized for the
    capped server: aggregate up to ~20 MiB/s, request durations up to ~4 s."""
    return StateSpec(
        throughput_scale=32.0 * MIB,
        duration_scale=4.0,
        staleness_scale=10.0,
        ewma_alpha=0.3,
        counter_window=8,
    )


@dataclass
class Condition:
    """One server condition. Times are seconds from harness start."""

    file_size: int = 512 * MIB
    rate0: int = 640 * KIB  # initial per-response rate
    rate1: int = 2500 * KIB  # rate after switch_at
    switch_at: float = 1.0
    cap_at: float | None = 8.0  # None: no cap
    cap_c: float = 8.0 * MIB
    cap_gamma: float = 0.05
    owd_s: float = 0.040  # one-way delay (netem 40 ms each way)
    client_start_s: float = 0.08  # first request leaves the client
    excess_low_s: float = 0.040  # service excess below 2 MiB/s
    excess_high_s: float = 0.023  # service excess at >= 2 MiB/s
    # Empirical timing jitter (calibration.json). When set, each request's
    # server excess and client overhead are drawn from the measured
    # distributions of its rate class instead of the fixed medians above.
    calib: dict | None = None
    # Optional multi-phase base schedule [(at_s, rate), ...] sorted by time;
    # when set it replaces rate0/rate1/switch_at (e.g. the d2 schedule).
    schedule: list | None = None

    def excess(self, rate: float) -> float:
        return self.excess_high_s if rate >= 2 * MIB else self.excess_low_s

    def sample(self, rng: random.Random, rate: float) -> tuple[float, float]:
        """(server excess, response-leg delay) for one request."""
        if self.calib is None:
            return self.excess(rate), self.owd_s
        if "tables" in self.calib:
            return self._sample_multi(rng, rate)
        cls = self.calib["hi" if rate >= self.calib["rate_class_threshold_bytes_per_s"] else "lo"]
        ex = _inv_cdf(cls["excess"]["q"], rng.random())
        ce = _inv_cdf(cls["client_extra"]["q"], rng.random())
        # client_extra was measured at owd 0.040 each way: keep its tail, move its base
        return ex, max(0.0, ce - 2 * self.calib["measured_owd_s"]) + self.owd_s

    def _sample_multi(self, rng: random.Random, rate: float) -> tuple[float, float]:
        """Delay-dependent calibration (v2): interpolate the inverse CDFs of the
        two tables whose measured delays bracket owd_s, clamping outside them.
        Draws the same two uniforms as the single-table path."""
        tables = self.calib["tables"]  # sorted by measured_owd_s
        lo, hi = tables[0], tables[-1]
        for a, b in zip(tables, tables[1:]):
            if a["measured_owd_s"] <= self.owd_s <= b["measured_owd_s"]:
                lo, hi = a, b
                break
        else:
            if self.owd_s > tables[-1]["measured_owd_s"]:
                lo = hi
            else:
                hi = lo
        span = hi["measured_owd_s"] - lo["measured_owd_s"]
        w = 0.0 if span <= 0 else (self.owd_s - lo["measured_owd_s"]) / span
        if "classes" in self.calib:  # v2 amendment A1: several rate classes
            key = self.calib["classes"][sum(rate >= t for t in self.calib["rate_class_thresholds_bytes_per_s"])]
        else:
            key = "hi" if rate >= self.calib["rate_class_threshold_bytes_per_s"] else "lo"
        u_ex, u_ce = rng.random(), rng.random()

        def comp(t, name, u):
            return _inv_cdf(t[key][name]["q"], u)

        ex = (1 - w) * comp(lo, "excess", u_ex) + w * comp(hi, "excess", u_ex)
        tail_lo = comp(lo, "client_extra", u_ce) - 2 * lo["measured_owd_s"]
        tail_hi = comp(hi, "client_extra", u_ce) - 2 * hi["measured_owd_s"]
        return ex, max(0.0, (1 - w) * tail_lo + w * tail_hi) + self.owd_s

    def base_rate(self, t: float) -> float:
        if self.schedule:
            rate = self.schedule[0][1]
            for at, r in self.schedule:
                if t >= at:
                    rate = r
            return rate
        return self.rate1 if t >= self.switch_at else self.rate0


def _inv_cdf(q: list[float], u: float) -> float:
    """Piecewise-linear inverse CDF over 0..100 percentiles."""
    x = u * (len(q) - 1)
    i = min(int(x), len(q) - 2)
    return q[i] + (q[i + 1] - q[i]) * (x - i)


def load_calibration(path: str) -> dict:
    import json
    with open(path) as f:
        return json.load(f)


def load_calibration_multi(paths: list[str]) -> dict:
    """Combine single-delay calibration files into one delay-dependent table set."""
    tables = sorted((load_calibration(p) for p in paths), key=lambda t: t["measured_owd_s"])
    if "classes" in tables[0]:
        keys = {(tuple(t["rate_class_thresholds_bytes_per_s"]), tuple(t["classes"])) for t in tables}
        if len(keys) != 1:
            raise ValueError("calibration tables use different rate classes")
        thr, classes = keys.pop()
        return {"rate_class_thresholds_bytes_per_s": list(thr), "classes": list(classes), "tables": tables}
    thr = {t["rate_class_threshold_bytes_per_s"] for t in tables}
    if len(thr) != 1:
        raise ValueError("calibration tables use different rate-class thresholds")
    return {"rate_class_threshold_bytes_per_s": thr.pop(), "tables": tables}


def live_condition(name: str, calib: dict | None = None) -> Condition:
    """The two live scenarios of testdata/rl_capped (t1 schedule, 512 MiB)."""
    if name == "CAP8":
        return Condition(cap_at=8.0, calib=calib)
    if name == "HEALTHY":
        return Condition(cap_at=None, calib=calib)
    if name == "CAP0":
        return Condition(cap_at=0.0, calib=calib)
    # Held-out generalization scenarios (outside the training ranges).
    if name == "G_C16":
        return Condition(cap_at=8.0, cap_c=16.0 * MIB, calib=calib)
    if name == "G_RTT":
        return Condition(cap_at=8.0, owd_s=0.080, client_start_s=0.16, calib=calib)
    if name == "G_D2":
        return Condition(cap_at=8.0, schedule=[(0.0, 640 * KIB), (2.0, 160 * KIB), (4.0, 2500 * KIB)], calib=calib)
    # v2 held-out scenarios (testdata/rl_cap_train_v2/protocol.md).
    if name == "H_C12":
        return Condition(cap_at=6.0, cap_c=12.0 * MIB, calib=calib)
    if name == "H_RTT110":
        return Condition(cap_at=12.0, owd_s=0.110, client_start_s=0.22, calib=calib)
    if name == "H_DIP":
        return Condition(cap_at=6.0, schedule=[(0.0, 640 * KIB), (1.0, 2500 * KIB), (30.0, 400 * KIB),
                                               (40.0, 2500 * KIB)], calib=calib)
    if name == "H_C20":
        return Condition(cap_at=4.0, cap_c=20.0 * MIB, owd_s=0.020, client_start_s=0.04, calib=calib)
    raise ValueError(name)


@dataclass
class SimConfig:
    min_concurrency: int = 1
    max_concurrency: int = 16
    window: int = 16  # policyRunner window (rl_bench -window)
    start_n: int | None = None  # None: sampled uniformly in training
    condition: Condition | None = None  # None: sampled in training
    max_episode_steps: int = 5000
    spec: StateSpec = field(default_factory=cap_spec)
    calib: dict | None = None  # timing jitter for sampled training conditions

    # Training distribution (only used by randomize()).
    rand_file_mib: tuple[int, int] = (128, 512)
    rand_rate0_kib: tuple[int, int] = (320, 1280)
    rand_rate1_kib: tuple[int, int] = (1500, 4000)
    rand_switch_s: tuple[float, float] = (0.5, 3.0)
    rand_cap_mode: tuple[float, float, float] = (0.25, 0.15, 0.60)  # none, from 0, later
    rand_cap_at_s: tuple[float, float] = (2.0, 30.0)
    rand_cap_c_mib: tuple[float, float] = (4.0, 12.0)
    rand_cap_gamma: tuple[float, float] = (0.02, 0.10)
    rand_owd_s: tuple[float, float] = (0.02, 0.06)
    # v2 distribution (testdata/rl_cap_train_v2/protocol.md). Off by default,
    # so v1 training draws the same random sequence as before.
    v2_dist: bool = False
    v2_cap_c_mib: tuple[float, float] = (4.0, 24.0)
    v2_owd_s: tuple[float, float] = (0.010, 0.120)
    v2_sched_mix: tuple[float, float, float] = (0.50, 0.25, 0.25)  # two-phase, early dip, late dip

    def to_json(self) -> dict:
        d = asdict(self)
        d.pop("calib", None)
        if d.get("condition"):
            d["condition"].pop("calib", None)
        d["calibrated"] = self.calib is not None
        d["fixed_chunk"] = FIXED_CHUNK
        d["action_space"] = ACTION_NAMES
        d["spec"] = self.spec.to_json()
        return d


class SimEnv:
    """Gym-like environment used by rl.dqn.train: reset/step/done/randomize."""

    def __init__(self, cfg: SimConfig, seed: int):
        self.cfg = cfg
        self.spec = cfg.spec
        self.spec.validate()
        self.rng = random.Random(seed)
        self.seed = seed
        self._cond = cfg.condition or Condition()
        self._start_n = cfg.start_n
        self.reset()

    # ---- training distribution ----
    def sample_condition(self) -> Condition:
        if self.cfg.v2_dist:
            return self._sample_condition_v2()
        c, r = self.cfg, self.rng
        u = r.random()
        if u < c.rand_cap_mode[0]:
            cap_at = None
        elif u < c.rand_cap_mode[0] + c.rand_cap_mode[1]:
            cap_at = 0.0
        else:
            cap_at = r.uniform(*c.rand_cap_at_s)
        return Condition(
            file_size=r.randint(*c.rand_file_mib) * MIB,
            rate0=r.randint(*c.rand_rate0_kib) * KIB,
            rate1=r.randint(*c.rand_rate1_kib) * KIB,
            switch_at=r.uniform(*c.rand_switch_s),
            cap_at=cap_at,
            cap_c=r.uniform(*c.rand_cap_c_mib) * MIB,
            cap_gamma=r.uniform(*c.rand_cap_gamma),
            owd_s=r.uniform(*c.rand_owd_s),
            calib=c.calib,
        )

    def _sample_condition_v2(self) -> Condition:
        c, r = self.cfg, self.rng
        u = r.random()
        if u < c.rand_cap_mode[0]:
            cap_at = None
        elif u < c.rand_cap_mode[0] + c.rand_cap_mode[1]:
            cap_at = 0.0
        else:
            cap_at = r.uniform(*c.rand_cap_at_s)
        rate0 = r.randint(*c.rand_rate0_kib) * KIB
        rate1 = r.randint(*c.rand_rate1_kib) * KIB
        switch_at = r.uniform(*c.rand_switch_s)
        owd = r.uniform(*c.v2_owd_s)
        v = r.random()
        if v < c.v2_sched_mix[0]:
            schedule = None
        elif v < c.v2_sched_mix[0] + c.v2_sched_mix[1]:
            dip_at = r.uniform(1.0, 4.0)
            schedule = [(0.0, rate0), (dip_at, r.randint(100, 400) * KIB), (dip_at + r.uniform(1.0, 4.0), rate1)]
        else:
            dip_at = max(switch_at + 0.5, r.uniform(5.0, 25.0))
            schedule = [(0.0, rate0), (switch_at, rate1), (dip_at, r.randint(200, 800) * KIB),
                        (dip_at + r.uniform(2.0, 10.0), rate1)]
        return Condition(
            file_size=r.randint(*c.rand_file_mib) * MIB,
            rate0=rate0, rate1=rate1, switch_at=switch_at, schedule=schedule,
            cap_at=cap_at,
            cap_c=r.uniform(*c.v2_cap_c_mib) * MIB,
            cap_gamma=r.uniform(*c.rand_cap_gamma),
            owd_s=owd, client_start_s=2 * owd,
            calib=c.calib,
        )

    def randomize(self) -> None:
        self._cond = self.sample_condition()
        self._start_n = self.rng.randint(self.cfg.min_concurrency, self.cfg.max_concurrency)

    # ---- server ----
    def _cap_rate(self, k: int) -> float:
        c = self._cond
        r = c.cap_c / (k * (1.0 + c.cap_gamma * (k - 1)))
        return max(1.0, float(int(r)))

    def _issue(self, t_send: float) -> None:
        """A worker sends a request for the next range at client time t_send."""
        c = self._cond
        n = min(FIXED_CHUNK, self.total - self.allocated)
        self.allocated += n
        arrive = t_send + c.owd_s
        rate = float(c.base_rate(arrive))
        capped = c.cap_at is not None and arrive >= c.cap_at
        if capped:
            self.capped_ends = [e for e in self.capped_ends if e > arrive]
            k = len(self.capped_ends) + 1
            rate = min(rate, self._cap_rate(k))
        ex, back = c.sample(self.rng, rate)
        srv_end = arrive + n / rate + ex
        if capped:
            self.capped_ends.append(srv_end)
        t_done = srv_end + back
        self.seq += 1
        heapq.heappush(self.events, (t_done, self.seq, t_send, n, rate))
        self.in_flight += 1

    # ---- episode ----
    def reset(self) -> list[float]:
        c = self._cond
        n0 = self._start_n if self._start_n is not None else self.rng.randint(
            self.cfg.min_concurrency, self.cfg.max_concurrency)
        self.total = c.file_size
        self.allocated = 0
        self.done_bytes_true = 0
        self.events: list = []
        self.seq = 0
        self.in_flight = 0
        self.capped_ends: list[float] = []
        self.target = n0
        self.recent: list[tuple[int, float, float]] = []  # (bytes, duration, end)
        self.steps = 0
        self.attempts = 0
        self._done = False
        self._truncated = False
        self.now = c.client_start_s
        self.decision_time = c.client_start_s
        # LivePolicy trackers
        self.ewma = 0.0
        self.vol = 0.0
        self.prev_agg = 0.0
        self.prev_dur = 0.0
        self.since_conc = 0.0
        self.prev_conc = 0
        self.fail_w: list[int] = []
        self.retry_w: list[int] = []
        self.done_w: list[int] = []
        self.done_bytes_obs = 0
        self.trajectory: list[tuple[float, int]] = [(self.now, n0)]
        self.rates: list[float] = []
        for _ in range(n0):
            if self.allocated < self.total:
                self._issue(self.now)
        self._state = self._advance()
        return self._state

    def _observe(self, nbytes: int, dur: float, end: float) -> list[float]:
        """policyRunner.observe + LivePolicy.observeV5 for one completed attempt."""
        self.attempts += 1
        self.recent.append((nbytes, dur, end))
        if len(self.recent) > self.cfg.window:
            self.recent = self.recent[-self.cfg.window:]
        earliest = min(e - d for _, d, e in self.recent)
        latest = max(e for _, _, e in self.recent)
        span = latest - earliest
        agg = sum(b for b, _, _ in self.recent) / span if span > 0 else 0.0
        conc = self.target  # controller.Current() at observation time

        # observeV5: success => progress += bytes; windows get one entry each
        self.done_bytes_obs += nbytes
        self._push(self.fail_w, 0)
        self._push(self.retry_w, 0)
        self._push(self.done_w, 1)
        step = dur if dur > 0 else 0.001
        if conc == self.prev_conc:
            self.since_conc += step
        else:
            self.since_conc = 0.0
        obs = {
            "concurrency": conc,
            "total_bytes": self.total,
            "done_bytes": min(self.done_bytes_obs, self.total),
            "aggregate_throughput": agg,
            "throughput_ewma": self.ewma,
            "prev_throughput": self.prev_agg,
            "throughput_vol": self.vol,
            "request_duration_s": dur,
            "prev_duration_s": self.prev_dur,
            "since_conc_change_s": self.since_conc,
            "window_failures": sum(self.fail_w),
            "window_retries": sum(self.retry_w),
            "window_attempts": len(self.fail_w),
            "window_completed": sum(self.done_w),
        }
        a = self.spec.ewma_alpha
        self.vol = a * abs(agg - self.prev_agg) + (1 - a) * self.vol
        self.ewma = a * agg + (1 - a) * self.ewma
        self.prev_agg = agg
        self.prev_dur = dur
        self.prev_conc = conc  # selectActionV6 sets prevConc = ctx.Concurrency
        self.last_obs = obs
        return features(obs, self.spec)

    def _advance(self) -> list[float] | None:
        """Pop the next completion and build its observation (a decision point)."""
        if not self.events:
            self._done = True
            return None
        t_done, _, t_send, n, rate = heapq.heappop(self.events)
        self.in_flight -= 1
        self.now = t_done
        self.done_bytes_true += n
        self.rates.append(rate)
        self._pending_worker = True  # the completing worker awaits the decision
        return self._observe(n, t_done - t_send, t_done)

    def state(self) -> list[float]:
        return self._state if self._state is not None else [0.0] * 13

    def done(self) -> bool:
        return self._done or self._truncated

    def step(self, action: int) -> dict:
        if self.done():
            raise RuntimeError("episode already finished")
        if action < 0 or action >= NUM_ACTIONS:
            raise ValueError(f"invalid action {action}")
        before = self.target
        if action == 1 and self.target < self.cfg.max_concurrency:
            self.target += 1
        elif action == 2 and self.target > self.cfg.min_concurrency:
            self.target -= 1
        effective = self.target != before
        if effective:
            self.trajectory.append((self.now, self.target))
        # worker.overTarget: the completing worker retires if over target, else
        # takes the next range; reconcile then starts workers up to target.
        active = self.in_flight + 1
        if active > self.target:
            active -= 1
        elif self.allocated < self.total:
            self._issue(self.now)
        else:
            active -= 1
        while active < self.target and self.allocated < self.total:
            self._issue(self.now)
            active += 1
        t0 = self.now
        self.steps += 1
        nxt = self._advance()
        self._state = nxt if nxt is not None else [0.0] * 13
        elapsed = self.now - t0
        if self.cfg.max_episode_steps > 0 and self.steps >= self.cfg.max_episode_steps and not self._done:
            self._truncated = True
        return {
            "next_state": self.state(),
            "reward": -elapsed,
            "done": self.done(),
            "elapsed": elapsed,
            "concurrency": self.target,
            "effective_action": action if effective else -1,
            "no_op": not effective,
        }

    def completion_s(self) -> float:
        """Harness start to last byte, as rl_bench completion_ms."""
        return self.now

    def _push(self, w: list[int], v: int) -> None:
        w.append(v)
        if len(w) > self.spec.counter_window:
            del w[: len(w) - self.spec.counter_window]


def run_policy(cond: Condition, start_n: int, act, window: int = 16, spec: StateSpec | None = None,
               seed: int = 0) -> SimEnv:
    """Run one episode with act(state, env) -> action index; return the env."""
    cfg = SimConfig(window=window, start_n=start_n, condition=cond, spec=spec or cap_spec())
    env = SimEnv(cfg, seed)
    while not env.done():
        env.step(act(env.state(), env))
    return env
