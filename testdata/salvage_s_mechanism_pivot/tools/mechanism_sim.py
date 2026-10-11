#!/usr/bin/env python3
"""Adaptive-S mechanism screening simulator (analytical only; no network I/O).

Deterministic event-driven simulator of a finite HTTP range download with N
concurrent workers under a time-varying downstream service rate. It supports
several request-level service models so that candidate mechanisms can be
screened for a *state-dependent* preferred chunk size:

  shared        aggregate capacity r(t) is shared equally among the requests
                currently transferring; in-flight requests experience every
                capacity change (the natural mm-link / shared-link model)
  lockin_full   each request is paced at the profile's per-request rate at the
                moment it starts and keeps that rate for its whole duration
                (the existing project server semantics for scheduled rates:
                SetRateScheduleAt + per-request pacing fixed at request start)
  serialized    one request at a time is served at r(t); others queue
  constant      fixed per-request rate (the closed v7 static control)

Capacity profiles are piecewise-constant and loop at the end (the mm-link
default for trace files). Request completion depends on the capacity
experienced throughout the request; a request is never resized.
"""
import bisect
import json
import math
import os
import statistics

BASE = "/workspaces/mvp/testdata/salvage_s_mechanism_pivot"
MIB = 1024 ** 2
GIB = 1024 ** 3
N_DEFAULT = 4
D_DEFAULT = 0.040
ARMS = [("256KiB", 256 * 1024), ("1MiB", 1 * MIB), ("4MiB", 4 * MIB), ("16MiB", 16 * MIB)]


class Profile:
    """Piecewise-constant per-request rate profile, looped at the end."""

    def __init__(self, segments):
        self.segs = [(float(d), float(r)) for d, r in segments if d > 0]
        self.cum = []
        acc = 0.0
        for d, _ in self.segs:
            acc += d
            self.cum.append(acc)
        self.T = acc

    def rate_and_end(self, t):
        tt = t % self.T
        i = bisect.bisect_right(self.cum, tt)
        if i >= len(self.segs):
            i = len(self.segs) - 1
        seg_start = self.cum[i - 1] if i > 0 else 0.0
        seg_end_abs = t + (self.cum[i] - tt)
        return self.segs[i][1], seg_end_abs


def constant_profile(r, duration=1e9):
    return Profile([(duration, r)])


def two_phase_profile(r1, r2, t1):
    return Profile([(t1, r1), (1e9, r2)])


def simulate(F, profile, model, S_start, N=N_DEFAULT, D=D_DEFAULT, R_const=None, switch=None, keep_schedule=False):
    """Run one deterministic download. Returns a result dict."""
    workers = [None] * N
    next_alloc = 0
    completed = 0
    current_S = S_start
    switched = False
    switch_info = None
    samples = []  # completed-request throughput samples
    n_requests = 0
    schedule = []

    def active_transfers(t):
        return [w for w in workers if w is not None and w["transfer_start"] <= t]

    def alloc(worker_idx, t):
        nonlocal next_alloc, current_S, switched, switch_info, n_requests
        if next_alloc >= F:
            workers[worker_idx] = None
            return
        if switch is not None and not switched:
            newS = switch({"t": t, "B_pending": F - completed - sum(w["remaining"] for w in workers if w),
                           "completed": completed, "samples": samples, "current_S": current_S})
            if newS and newS != current_S:
                switch_info = {"time": t, "from_S": current_S, "to_S": newS,
                               "B_pending": F - completed - sum(w["remaining"] for w in workers if w)}
                current_S = newS
                switched = True
        size = min(current_S, F - next_alloc)
        req = {"size": size, "remaining": size, "alloc_time": t, "transfer_start": t + D, "rate": None}
        if keep_schedule:
            req["sched_idx"] = len(schedule)
            schedule.append({"start": next_alloc, "size": size, "alloc_time": t,
                             "transfer_start": t + D, "completion_time": None, "chunk_target": current_S})
        next_alloc += size
        n_requests += 1
        workers[worker_idx] = req

    for i in range(N):
        alloc(i, 0.0)

    t = 0.0
    while True:
        # complete any requests whose remaining bytes are within tolerance
        for idx, w in enumerate(workers):
            if w is not None and w["remaining"] <= 1e-6 * w["size"]:
                samples.append(w["size"] / max(1e-9, t - w["transfer_start"]))
                completed += w["size"]
                if keep_schedule and "sched_idx" in w:
                    schedule[w["sched_idx"]]["completion_time"] = t
                workers[idx] = None
                alloc(idx, t)
        if completed >= F - 1e-6:
            break
        transferring = active_transfers(t)
        if not transferring:
            if all(w is None for w in workers):
                if completed >= F - 1e-6:
                    break
                raise RuntimeError("idle with work remaining")
            # advance to the earliest pending transfer start
            starts = [w["transfer_start"] for w in workers if w is not None]
            t = min(starts)
            continue

        # next transfer start
        pending = [w["transfer_start"] for w in workers if w is not None and w["transfer_start"] > t]
        next_start = min(pending) if pending else math.inf
        r, seg_end = profile.rate_and_end(t)

        # per-request rates for this epoch
        if model == "shared":
            k = len(transferring)
            rates = {id(w): r / k for w in transferring}
        elif model == "lockin_full":
            for w in transferring:
                if w["rate"] is None:
                    r0, _ = profile.rate_and_end(w["transfer_start"])
                    w["rate"] = r0
            rates = {id(w): w["rate"] for w in transferring}
        elif model == "serialized":
            head = min(transferring, key=lambda w: w["alloc_time"])
            rates = {id(w): (r if w is head else 0.0) for w in transferring}
        elif model == "constant":
            rates = {id(w): R_const for w in transferring}
        else:
            raise ValueError(model)

        # earliest completion in this epoch
        best_w, best_dt = None, math.inf
        for w in transferring:
            rate = rates[id(w)]
            if rate <= 0:
                continue
            dt = w["remaining"] / rate
            if dt < best_dt:
                best_dt, best_w = dt, w

        next_seg = seg_end
        t_next = min(next_start, next_seg, t + best_dt)
        dt = t_next - t
        for w in transferring:
            rate = rates[id(w)]
            w["remaining"] -= rate * dt
        t = t_next

        # completions
        for idx, w in enumerate(workers):
            if w is not None and w["remaining"] <= 1e-6 * w["size"]:
                samples.append(w["size"] / max(1e-9, t - w["transfer_start"]))
                completed += w["size"]
                if keep_schedule and "sched_idx" in w:
                    schedule[w["sched_idx"]]["completion_time"] = t
                workers[idx] = None
                alloc(idx, t)

        if completed >= F - 1e-6:
            break
        if t > 1e7:
            raise RuntimeError("simulation did not finish")

    out = {"completion_seconds": t, "n_requests": n_requests, "switched": switched,
           "switch_info": switch_info}
    if keep_schedule:
        out["schedule"] = schedule
    return out


def best_fixed(F, profile, model, N=N_DEFAULT, D=D_DEFAULT, R_const=None):
    out = {}
    for label, s in ARMS:
        out[label] = simulate(F, profile, model, s, N=N, D=D, R_const=R_const)["completion_seconds"]
    return out


def oracle_one_switch(F, profile, model, N=N_DEFAULT, D=D_DEFAULT, R_const=None, grid=12, horizon=None):
    """Diagnostic upper bound: best single future-only switch (any start arm,
    any direction, any time) found by a coarse grid search."""
    fixed = best_fixed(F, profile, model, N=N, D=D, R_const=R_const)
    ref = min(fixed.values())
    if horizon is None:
        horizon = ref
    best = {"gain": 0.0, "completion": ref, "switch_time": None, "target": None, "start": None}
    for start_label, start_s in ARMS:
        for gi in range(1, grid + 1):
            t_switch = horizon * gi / (grid + 1)
            for label, s in ARMS:
                def policy(ctx, _t=t_switch, _s=s):
                    return _s if ctx["t"] >= _t else None
                r = simulate(F, profile, model, start_s, N=N, D=D, R_const=R_const, switch=policy)
                if r["switched"]:
                    g = (ref - r["completion_seconds"]) / ref
                    if g > best["gain"]:
                        best = {"gain": g, "completion": r["completion_seconds"],
                                "switch_time": t_switch, "target": label, "start": start_label}
    return {"reference_fixed_seconds": ref, **best}


def causal_volatility_switch(k, ratio, s_target):
    """One future-only switch driven only by observed samples: switch once to
    s_target when the last k completed-request throughput samples show a
    max/min ratio above `ratio` (volatility detection)."""
    def policy(ctx):
        if len(ctx["samples"]) >= k:
            vals = ctx["samples"][-k:]
            mn, mx = min(vals), max(vals)
            if mn > 0 and mx / mn > ratio:
                return s_target
        return None
    return policy


def causal_rate_switch(threshold, s_below, s_above, k=3):
    """One future-only switch driven only by observed completed-request
    throughput: once k samples are available, switch once to s_above when the
    median sample exceeds the threshold, or to s_below otherwise."""
    def policy(ctx):
        if len(ctx["samples"]) >= k:
            med = statistics.median(ctx["samples"][-k:])
            return s_above if med > threshold else s_below
        return None
    return policy
