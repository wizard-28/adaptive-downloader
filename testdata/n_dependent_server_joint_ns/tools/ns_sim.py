#!/usr/bin/env python3
"""Exact discrete-event model of the Track-2 live server semantics.

It reproduces the mechanism of python/rl/sim_env_v5.py as it will be
implemented live:

  p_eff(k)  = min(0.9, loss + congestion * (k - 1))     per-attempt failure prob
  overhead  = overhead_s * (k - 1)                      per-extra-connection delay
  per       = min(rate, link / k)                       fair-share per-response rate
  svc       = latency + overhead + size / per           service time

plus the frozen downloader semantics: 30 s per-attempt timeout (the attempt is
aborted at the timeout; the server slot is released), same-range retry after
retry_delay, at most max_attempts attempts, then the download aborts. A seeded
failure consumes the full service time (the live server sleeps, then fails).
"""
import heapq
import math
import random

MIB = 1024 * 1024
KIB = 1024


class State:
    def __init__(self, name="", rate=2500 * KIB, link=0, loss=0.004,
                 congestion=0.0, overhead=0.02, latency=0.02):
        self.name = name
        self.rate = float(rate)
        self.link = float(link)
        self.loss = float(loss)
        self.congestion = float(congestion)
        self.overhead = float(overhead)
        self.latency = float(latency)

    def dict(self):
        return dict(name=self.name, rate=self.rate, link=self.link, loss=self.loss,
                    congestion=self.congestion, overhead=self.overhead,
                    latency=self.latency)


class Run:
    def __init__(self, n, s, file_bytes=32 * MIB, timeout=30.0,
                 max_attempts=3, retry_delay=0.1, h=0.088):
        self.n = n
        self.s = s
        self.file = file_bytes
        self.timeout = timeout
        self.max_attempts = max_attempts
        self.retry_delay = retry_delay
        self.h = h


def simulate(st, run, seed, collect_log=False):
    rng = random.Random(seed)
    cur = [None] * run.n          # (start, size) per worker
    attempts = [0] * run.n
    last = [None] * run.n         # last attempt outcome per worker
    starts = [(0.0, w) for w in range(run.n)]
    heapq.heapify(starts)
    in_flight = []
    cursor = 0
    done = 0
    completion = None
    retries = failures = timeouts = 0
    requests = 0
    log = []
    aborted = False
    abort_time = None

    while starts or in_flight:
        t_start = starts[0][0] if starts else math.inf
        t_end = in_flight[0][0] if in_flight else math.inf
        if t_end <= t_start:
            t, w = heapq.heappop(in_flight)
            o = last[w]
            if o["failed"]:
                failures += 1
                if o["timed_out"]:
                    timeouts += 1
                if attempts[w] >= run.max_attempts:
                    aborted = True
                    abort_time = t
                    break
                retries += 1
                heapq.heappush(starts, (t + run.retry_delay, w))
            else:
                done += cur[w][1]
                cur[w] = None
                if done >= run.file:
                    completion = t
                    break
                heapq.heappush(starts, (t + run.h, w))
        else:
            t, w = heapq.heappop(starts)
            if cur[w] is None:
                if cursor >= run.file:
                    continue
                size = min(run.s, run.file - cursor)
                cur[w] = (cursor, size)
                cursor += size
            attempts[w] += 1
            requests += 1
            k = 1 + len(in_flight)
            per = st.rate if st.link <= 0 else min(st.rate, st.link / k)
            delay = st.latency + st.overhead * (k - 1)
            svc = delay + cur[w][1] / per
            timed_out = svc > run.timeout
            seeded = rng.random() < min(0.9, st.loss + st.congestion * (k - 1))
            failed = timed_out or seeded
            last[w] = {"failed": failed, "timed_out": timed_out}
            if collect_log:
                log.append({"t": t, "w": w, "k": k, "size": cur[w][1],
                            "per": per, "svc": svc, "failed": failed,
                            "timed_out": timed_out})
            heapq.heappush(in_flight, (t + min(svc, run.timeout), w))

    return {"completion": completion, "aborted": aborted, "abort_time": abort_time,
            "retries": retries, "failures": failures, "timeouts": timeouts,
            "requests": requests, "log": log if collect_log else None}
