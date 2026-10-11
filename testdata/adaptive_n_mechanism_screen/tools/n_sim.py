#!/usr/bin/env python3
"""Deterministic adaptive-N mechanism simulator (analytical only; no network I/O).

Finite HTTP range download with a configurable number of concurrent workers
(one contiguous request per worker, fixed request size S) over a time-varying
environmental state. Request transfer rates depend on the *current* number of
active requests and the *current* state, so in-flight requests experience state
evolution throughout their lifetime.

Models (per-request transfer rate given k active requests and state):
  direct    rate = R                                  (fixed per-request throttle; T1 control)
  fair      rate = C / k                              (aggregate C shared fairly among flows)
  fair_bg   rate = C / (k + B)                        (fair sharing with B background flows)
  cap       served = min(k, K); rate = min(R, C/served) for the K oldest requests, 0 otherwise
                                                      (server-side concurrency cap + FIFO wait)
  degrade   agg = C / (1 + gamma*(k-1)); rate = agg/k  (server aggregate capacity degrades
                                                      with the number of concurrent connections)

N changes are applied at completion events: increasing N adds workers (they allocate
immediately); decreasing N retires workers, which finish their in-flight request and then
stop (in-flight requests are never resized or terminated).
"""
import bisect
import math

N_GRID = [1, 2, 4, 8, 16]


class Profile:
    """Looped piecewise-constant environmental state."""

    def __init__(self, segs):
        # segs: list of (duration_s, state_dict)
        self.segs = [(float(d), s) for d, s in segs if d > 0]
        self.cum = []
        acc = 0.0
        for d, _ in self.segs:
            acc += d
            self.cum.append(acc)
        self.T = acc

    def state_and_end(self, t):
        tt = t % self.T
        i = bisect.bisect_right(self.cum, tt)
        if i >= len(self.segs):
            i = len(self.segs) - 1
        seg_end = t + (self.cum[i] - tt)
        return self.segs[i][1], seg_end


def constant_state(state, duration=1e9):
    return Profile([(duration, dict(state))])


def two_phase(state1, state2, t1):
    return Profile([(t1, dict(state1)), (1e9, dict(state2))])


def rates_for(model, k, state, params):
    """Return a list of per-request rates for k active requests (oldest first order
    is handled by the caller for `cap`)."""
    C = state.get("C", params["C"])
    if model == "direct":
        return [params["R"]] * k
    if model == "fair":
        return [C / k] * k
    if model == "fair_bg":
        return [C / (k + state.get("B", 0))] * k
    if model == "degrade":
        agg = C / (1.0 + state.get("gamma", params["gamma"]) * (k - 1))
        return [agg / k] * k
    if model == "cap":
        K = state.get("K", params["K"])
        served = min(k, K)
        r = min(params["R"], C / served)
        return [r] * served + [0.0] * (k - served)
    raise ValueError(model)


def simulate(F, S, N_start, profile, model, params, switch=None, keep_schedule=False):
    D0 = params["D0"]
    workers = []
    next_alloc = 0
    completed = 0
    t = 0.0
    samples = []
    schedule = []
    n_requests = 0
    switch_info = None
    target_N = N_start

    def alloc(w, t):
        nonlocal next_alloc, n_requests
        if next_alloc >= F:
            w["req"] = None
            return
        size = min(S, F - next_alloc)
        w["req"] = {"size": size, "remaining": size, "alloc_time": t, "transfer_start": t + D0}
        if keep_schedule:
            w["req"]["sched_idx"] = len(schedule)
            schedule.append({"size": size, "alloc_time": t, "transfer_start": t + D0,
                             "completion_time": None})
        next_alloc += size
        n_requests += 1

    for _ in range(N_start):
        workers.append({"req": None, "retiring": False})
    for w in workers:
        alloc(w, 0.0)

    while True:
        # completions
        for w in workers:
            if w["req"] is not None and w["req"]["remaining"] <= 1e-6 * w["req"]["size"]:
                samples.append(w["req"]["size"] / max(1e-9, t - w["req"]["transfer_start"]))
                completed += w["req"]["size"]
                if keep_schedule:
                    schedule[w["req"]["sched_idx"]]["completion_time"] = t
                w["req"] = None
        if completed >= F - 1e-6:
            break
        # apply the N policy at this completion boundary (observed samples only)
        if switch is not None:
            newN = switch({"t": t, "samples": samples, "target_N": target_N,
                           "B_pending": F - completed})
            if newN is not None and newN != target_N:
                switch_info = {"time": t, "from_N": target_N, "to_N": newN}
                target_N = newN
                if target_N > len(workers):
                    for _ in range(target_N - len(workers)):
                        w = {"req": None, "retiring": False}
                        workers.append(w)
                        alloc(w, t)
                else:
                    live = [w for w in workers if not w["retiring"]]
                    for w in live[target_N:]:
                        w["retiring"] = True
        # drop retired workers with no request
        workers[:] = [w for w in workers if not (w["retiring"] and w["req"] is None)]
        # work-conserving allocation to free workers
        for w in workers:
            if w["req"] is None and not w["retiring"]:
                alloc(w, t)
        active = [w for w in workers if w["req"] is not None and w["req"]["transfer_start"] <= t]
        if not active:
            starts = [w["req"]["transfer_start"] for w in workers if w["req"] is not None]
            if not starts:
                if completed >= F - 1e-6:
                    break
                raise RuntimeError("no progress possible")
            t = min(starts)
            continue
        state, seg_end = profile.state_and_end(t)
        k = len(active)
        rs = rates_for(model, k, state, params)
        if model == "cap":
            # oldest requests are served first
            order = sorted(range(k), key=lambda i: active[i]["req"]["alloc_time"])
            rate_by_w = {}
            for rank, i in enumerate(order):
                rate_by_w[id(active[i])] = rs[rank]
        else:
            rate_by_w = {id(active[i]): rs[i] for i in range(k)}
        best_dt = math.inf
        for w in active:
            r = rate_by_w[id(w)]
            if r > 0:
                best_dt = min(best_dt, w["req"]["remaining"] / r)
        if math.isinf(best_dt):
            t = seg_end
            continue
        t_next = min(seg_end, t + best_dt)
        dt = t_next - t
        for w in active:
            w["req"]["remaining"] -= rate_by_w[id(w)] * dt
        t = t_next
        if t > 1e7:
            raise RuntimeError("simulation did not finish")

    out = {"completion_seconds": t, "n_requests": n_requests,
           "switch_info": switch_info, "final_N": target_N}
    if keep_schedule:
        out["schedule"] = schedule
    return out


def fixed_sweep(F, S, profile, model, params):
    return {N: simulate(F, S, N, profile, model, params)["completion_seconds"] for N in N_GRID}


def best_fixed(F, S, profile, model, params):
    times = fixed_sweep(F, S, profile, model, params)
    best = min(times, key=lambda n: times[n])
    return best, times


def oracle_one_switch(F, S, profile, model, params, grid=12, horizon=None):
    """Diagnostic upper bound: best single adjacent-N switch at any time
    (start N x switch time x target N), found by a coarse grid search."""
    _, times = best_fixed(F, S, profile, model, params)
    ref = min(times.values())
    if horizon is None:
        horizon = ref
    best = {"gain": 0.0, "completion": ref, "start_N": None, "switch_time": None, "target_N": None}
    for startN in N_GRID:
        for gi in range(1, grid + 1):
            t_switch = horizon * gi / (grid + 1)
            for targetN in N_GRID:
                def policy(ctx, _t=t_switch, _n=targetN):
                    return _n if ctx["t"] >= _t else None
                r = simulate(F, S, startN, profile, model, params, switch=policy)
                if r["switch_info"]:
                    g = (ref - r["completion_seconds"]) / ref
                    if g > best["gain"]:
                        best = {"gain": g, "completion": r["completion_seconds"], "start_N": startN,
                                "switch_time": t_switch, "target_N": targetN}
    return {"reference_fixed_seconds": ref, "reference_fixed_N": min(times, key=lambda n: times[n]), **best}


def causal_rate_switch(threshold, k=3):
    """One future-only N switch driven only by observed completed-request
    throughput: once k samples exist, set N to the regime optimum implied by
    the observed median rate (mapping provided by `mapping`)."""
    def policy(ctx, mapping=None):
        if mapping is None or len(ctx["samples"]) < k:
            return None
        med = sorted(ctx["samples"][-k:])[k // 2]
        for lo, hi, n in mapping:
            if lo <= med < hi:
                return n
        return None
    return policy
