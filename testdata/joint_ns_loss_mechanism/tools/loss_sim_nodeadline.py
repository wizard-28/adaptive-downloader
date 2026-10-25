#!/usr/bin/env python3
"""
# NOTE (copy of loss_sim.py, which stays frozen): the only change is that the
# whole-request deadline is configurable. Params(timeout=None) uses the module
# DEFAULT_TIMEOUT; DEFAULT_TIMEOUT=30.0 reproduces the original exactly, and
# timeout=math.inf (or DEFAULT_TIMEOUT=math.inf) means NO deadline (attempts
# never fail for being slow; mirrors downloader.Config.NoRequestTimeout).
# The model has no other abort channel (loss only acts via burst stalls), so
# with no deadline no attempt/range ever fails. No straggler splitting added.
Deterministic event-driven simulator for the correlated-loss / range-retry
mechanism screen (ANALYTICAL ONLY; no network I/O).

Model (frozen in parameter_grounding/parameter_grounding.md):
  - Time-indexed two-state Markov loss process: G (clean, loss p_G, mean sojourn
    T_G) and B (burst, loss p_B, mean sojourn T_B). Seeded -> deterministic.
  - One contiguous HTTP range request per worker; future-only chunk size S;
    finite file F.
  - Per-response pacing at constant rate R (rate sampled at request start and
    held; the D2 initial-rate regime is the primary R).
  - During G the transfer progresses at R*(1-p_G); during B the connection is
    stalled (progress fraction g, primary 0) and recovers after the TCP RTO
    backoff for the burst duration (attempt sequence 0.2,0.6,1.4,3.0,6.2,12.6,
    25.4 s).
  - Frozen retry semantics: an attempt that exceeds the 30 s client timeout is
    aborted; the SAME range is retried (original boundaries) after a 0.1 s
    delay, up to 3 attempts; exhausting them fails the download. In-flight
    ranges are never resized.
  - Workers: N, each allocates the next contiguous range when free; a worker's
    next request starts h seconds after the previous completion.
"""
import bisect
import math
import random

DEFAULT_TIMEOUT = 30.0
MIB = 1024 * 1024
KIB = 1024
MSS = 1448
RTO_ATTEMPTS = [0.2, 0.6, 1.4, 3.0, 6.2, 12.6, 25.4, 51.0, 102.2]


class Params:
    def __init__(self, rate=640 * KIB, p_good=0.004, t_good=20.0,
                 p_bad=0.5, t_bad=2.0, g_bad=0.0,
                 timeout=None, max_attempts=3, retry_delay=0.1, h=0.088):
        self.rate = float(rate)
        self.p_good = float(p_good)
        self.t_good = float(t_good)
        self.p_bad = float(p_bad)
        self.t_bad = float(t_bad)
        self.g_bad = float(g_bad)
        self.timeout = float(DEFAULT_TIMEOUT if timeout is None else timeout)
        self.max_attempts = int(max_attempts)
        self.retry_delay = float(retry_delay)
        self.h = float(h)

    def dict(self):
        return dict(rate=self.rate, p_good=self.p_good, t_good=self.t_good,
                    p_bad=self.p_bad, t_bad=self.t_bad, g_bad=self.g_bad,
                    timeout=self.timeout, max_attempts=self.max_attempts,
                    retry_delay=self.retry_delay, h=self.h)


def recovery_after(duration):
    """RTO backoff delay after a burst of the given duration: the time from the
    burst end until the first retransmission attempt at or after it."""
    for a in RTO_ATTEMPTS:
        if a >= duration:
            return a - duration
    return 120.0 - duration


class LossProcess:
    """Deterministic (seeded) two-state Markov loss process, time-indexed."""

    def __init__(self, p, seed, horizon=30000.0):
        rng = random.Random(seed)
        self.p = p
        self.seed = seed
        self.starts = []
        self.ends = []
        self.states = []
        t = 0.0
        state = "G"
        while t < horizon:
            mean = p.t_good if state == "G" else p.t_bad
            dur = rng.expovariate(1.0 / mean)
            self.starts.append(t)
            self.ends.append(t + dur)
            self.states.append(state)
            t += dur
            state = "B" if state == "G" else "G"

    def _idx(self, t):
        i = bisect.bisect_right(self.starts, t) - 1
        if i < 0:
            i = 0
        if i >= len(self.starts):
            i = len(self.starts) - 1
        return i

    def state_at(self, t):
        return self.states[self._idx(t)]

    def loss_prob_at(self, t):
        return self.p.p_good if self.states[self._idx(t)] == "G" else self.p.p_bad

    def state_fraction(self, horizon=30000.0):
        g = sum(self.ends[i] - self.starts[i] for i in range(len(self.states))
                if self.states[i] == "G")
        total = sum(self.ends[i] - self.starts[i] for i in range(len(self.states)))
        return g / total if total else 0.0


def transfer_until(t0, size, proc, p, deadline, stats=None):
    """Transfer `size` bytes starting at t0. Returns the completion time, or
    None if the deadline is reached first (the attempt times out). `stats`
    optionally accumulates loss-recovery overhead and burst-event counts."""
    t = t0
    remaining = float(size)
    while remaining > 0:
        i = proc._idx(t)
        state = proc.states[i]
        end = proc.ends[i]
        if state == "G":
            eff = p.rate * (1.0 - p.p_good)
            dt = remaining / eff
            if t + dt <= end:
                return t + dt if t + dt <= deadline else None
            remaining -= eff * (end - t)
            t = end
        else:
            eff = p.rate * p.g_bad
            if eff > 0:
                dt = remaining / eff
                if t + dt <= end:
                    return t + dt if t + dt <= deadline else None
                remaining -= eff * (end - t)
            stall = (end - t) + recovery_after(end - t)
            if stats is not None:
                stats["stall_time"] = stats.get("stall_time", 0.0) + stall
                stats["burst_events"] = stats.get("burst_events", 0) + 1
            t = end + recovery_after(end - t)
        if t >= deadline:
            return None
    return t if t <= deadline else None


def simulate(F, N, S, proc, p, collect_requests=False):
    """One finite-file download under the given loss process."""
    workers = [0.0] * N
    cursor = 0
    requests = []
    ranges_done = 0
    failures = 0
    retries = 0
    retry_bytes = 0
    stats = {"stall_time": 0.0, "burst_events": 0}
    while cursor < F:
        i = min(range(N), key=lambda k: workers[k])
        t0 = workers[i]
        size = min(S, F - cursor)
        cursor += size
        attempts = 0
        t = t0
        while True:
            attempts += 1
            deadline = t + p.timeout
            tend = transfer_until(t, size, proc, p, deadline, stats)
            if tend is not None:
                break
            failures += 1
            if attempts >= p.max_attempts:
                return {"failed": True, "completion": math.inf, "requests": ranges_done,
                        "failures": failures, "retries": retries, "retry_bytes": retry_bytes,
                        "goodput": 0.0, "requests_log": None,
                        "stall_time": stats["stall_time"], "burst_events": stats["burst_events"]}
            retries += 1
            retry_bytes += size
            t = deadline + p.retry_delay
        ranges_done += 1
        if collect_requests:
            requests.append({"start": t0, "end": tend, "size": size,
                             "attempts": attempts, "failed_attempts": attempts - 1})
        workers[i] = tend + p.h
    completion = max(workers)
    return {"failed": False, "completion": completion, "requests": ranges_done,
            "failures": failures, "retries": retries, "retry_bytes": retry_bytes,
            "goodput": F / completion if completion > 0 else 0.0,
            "requests_log": requests if collect_requests else None,
            "stall_time": stats["stall_time"], "burst_events": stats["burst_events"]}


N_GRID = [1, 2, 4, 8, 16]
S_GRID = [256 * KIB, 1 * MIB, 4 * MIB, 16 * MIB]
FILE = 512 * MIB


def run_cell(proc, p, N, S, collect_requests=False):
    return simulate(FILE, N, S, proc, p, collect_requests=collect_requests)
