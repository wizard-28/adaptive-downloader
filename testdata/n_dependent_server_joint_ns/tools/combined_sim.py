#!/usr/bin/env python3
"""EXPLORATORY combined-mechanism simulator (derived from the frozen
joint_ns_phase_pacing_screen/tools/jns_sim.py; default behaviour identical).

Adds one optional server state on top of the phase-locked per-response pacing:

    rate_eff = min(schedule.rate(start), C / (k * (1 + gamma * (k - 1))))

where k is the number of in-flight requests at the request's server start
(including itself). C = 0 disables the degradation (the original behaviour).
This is the "soft" N-dependent server throttling from the proposal: no hard
request failures, so no 3-attempt abort cliff.

Exploratory only; not a frozen pre-screen.
"""

import heapq
import math

MIB = 1024 * 1024
KIB = 1024

N_GRID = [1, 2, 4, 8, 16]
S_GRID = [256 * KIB, 1 * MIB, 4 * MIB, 16 * MIB, 32 * MIB]

# Frozen joint action space (task section 9).
ACTION_SPACE = [
    (1, 256 * KIB),
    (2, 1 * MIB),
    (4, 1 * MIB),
    (4, 4 * MIB),
    (8, 4 * MIB),
    (8, 16 * MIB),
    (16, 4 * MIB),
    (16, 16 * MIB),
]

START_N = 4
START_S = 4 * MIB

D2_INITIAL = 640 * KIB
D2_THROTTLE = 160 * KIB
D2_RECOVERY = 2500 * KIB

# Frozen two-phase family anchored to the measured D2 mechanism.
FAMILY_SLOW = 640 * KIB
FAMILY_FAST = 2500 * KIB
FAMILY_TRANSITIONS = [1.0, 2.0, 4.0, 8.0]

FILE_SIZE = 512 * MIB
CHUNK = 16 * KIB


class Schedule:
    """Piecewise-constant wall-clock rate schedule (seconds, bytes/s)."""

    def __init__(self, segs):
        self.segs = [(float(d), int(r)) for d, r in segs]
        self.cum = []
        acc = 0.0
        for d, _ in self.segs:
            acc += d
            self.cum.append(acc)

    def rate(self, t):
        if t < 0:
            return self.segs[0][1]
        for i, c in enumerate(self.cum):
            if t < c:
                return self.segs[i][1]
        return self.segs[-1][1]


def two_phase(t_transition, slow=FAMILY_SLOW, fast=FAMILY_FAST):
    return Schedule([(t_transition, slow), (1e9, fast)])


def d2_anchor():
    return Schedule([(2.0, D2_INITIAL), (2.0, D2_THROTTLE), (1e9, D2_RECOVERY)])


def constant(rate):
    return Schedule([(1e9, rate)])


class ServerState:
    """Optional N-dependent aggregate rate cap (C=0 disables)."""

    def __init__(self, C=0, gamma=0.0):
        self.C = float(C)
        self.gamma = float(gamma)


class StateSchedule:
    """Piecewise-constant server state over wall-clock time."""

    def __init__(self, segs):
        # segs: list of (duration_s, ServerState)
        self.segs = [(float(d), st) for d, st in segs]
        self.cum = []
        acc = 0.0
        for d, _ in self.segs:
            acc += d
            self.cum.append(acc)

    def state_at(self, t):
        for i, c in enumerate(self.cum):
            if t < c:
                return self.segs[i][1]
        return self.segs[-1][1]


class Params:
    """Calibrated physical parameters (seconds unless noted)."""

    def __init__(self, h=0.088, c=0.0006, init=0.121, down=0.045,
                 timeout=30.0, retry_gap=0.135, max_attempts=3):
        self.h = h            # worker inter-request server-start gap
        self.c = c            # per-16KiB server write overhead
        self.init = init      # first request server-start delay
        self.down = down      # final-byte client latency
        self.timeout = timeout    # per-attempt client timeout (30 s, frozen tool config)
        self.retry_gap = retry_gap  # abort -> retried attempt server start
        self.max_attempts = max_attempts  # frozen downloader retry limit


def _duration(L, rate, params):
    return L / float(rate) + params.c * math.ceil(L / CHUNK)


class Worker:
    __slots__ = ("avail", "req", "retiring", "wid", "retry_range", "attempts")

    def __init__(self, wid, avail):
        self.wid = wid
        self.avail = avail
        self.req = None
        self.retiring = False
        self.retry_range = None
        self.attempts = 0


def simulate(F, N_start, S_start, schedule, params, controller=None,
             planned_switches=None, keep_requests=False, state=None,
             state_schedule=None):
    """Run one deterministic download.

    controller: callable(ctx) -> None | (new_N, new_S); called after every
    completed request (completion event), before the freed worker reallocates.
    planned_switches: [(time, N, S)] applied at exact event times (diagnostic
    oracle only; not causal).
    """
    workers = []
    next_wid = 0
    for _ in range(N_start):
        workers.append(Worker(next_wid, params.init))
        next_wid += 1
    cursor = 0
    completed = 0
    samples = []
    requests = []
    switches = []
    n_requests = 0
    events = []           # (time, kind, seq, payload)
    seq = 0
    t = 0.0
    target_N = N_start
    target_S = S_start

    def live_count():
        return sum(1 for x in workers if not x.retiring)

    def allocate(w, now):
        nonlocal cursor, n_requests, seq
        if w.retry_range is not None:
            rng = w.retry_range
            w.retry_range = None
            w.attempts += 1
        else:
            if cursor >= F:
                w.req = None
                return
            size = min(target_S, F - cursor)
            rng = (cursor, cursor + size)
            cursor += size
            n_requests += 1
            w.attempts = 1
        size = rng[1] - rng[0]
        start = w.avail
        rate = schedule.rate(start)
        st = state_schedule.state_at(start) if state_schedule is not None else state
        if st is not None and st.C > 0:
            k = 1 + sum(1 for e in events if e[1] == 1)
            cap = st.C / (k * (1.0 + st.gamma * (k - 1)))
            rate = min(rate, cap)
        dur = _duration(size, rate, params)
        if dur + params.h > params.timeout:
            # client aborts the attempt; the server write fails at the abort;
            # the range is retried with its original boundaries (never resized)
            end = start + (params.timeout - params.h / 2.0)
            w.req = {"size": size, "start": start, "end": end, "rate": rate,
                     "range": rng, "wid": w.wid, "aborted": True}
            w.retry_range = rng
        else:
            end = start + dur
            w.req = {"size": size, "start": start, "end": end, "rate": rate,
                     "range": rng, "wid": w.wid, "aborted": False}
        seq += 1
        heapq.heappush(events, (end, 1, seq, w))

    def apply_switch(now, new_N, new_S, why):
        nonlocal target_N, target_S, next_wid
        new_N = max(1, int(new_N))
        new_S = max(CHUNK, int(new_S))
        if (new_N, new_S) == (target_N, target_S):
            return
        switches.append({"time": now, "from_N": target_N, "from_S": target_S,
                         "to_N": new_N, "to_S": new_S, "why": why})
        target_N, target_S = new_N, new_S
        live = live_count()
        if target_N > live:
            for _ in range(target_N - live):
                nw = Worker(next_wid, now + params.h / 2.0)
                next_wid += 1
                workers.append(nw)
                allocate(nw, now)
        elif target_N < live:
            livew = [x for x in workers if not x.retiring]
            for x in livew[target_N:]:
                x.retiring = True

    for w in workers:
        allocate(w, 0.0)

    if planned_switches:
        for (ts, nN, nS) in planned_switches:
            seq += 1
            heapq.heappush(events, (ts, 0, seq, (nN, nS)))

    while events:
        ev_t, kind, _, payload = heapq.heappop(events)
        t = ev_t
        if kind == 0:
            nN, nS = payload
            apply_switch(t, nN, nS, "planned")
            continue
        w = payload
        req = w.req
        if req is None or abs(req["end"] - ev_t) > 1e-9:
            continue
        aborted = req.get("aborted", False)
        w.req = None
        if aborted:
            # failed attempt: partial bytes are discarded; no completion sample
            if keep_requests:
                requests.append({"wid": w.wid, "start": req["start"], "end": req["end"],
                                 "rate": req["rate"], "size": req["size"],
                                 "range": req["range"], "aborted": True})
            if w.attempts >= params.max_attempts:
                # frozen downloader semantics: the range fails after the retry
                # limit and the whole run aborts
                return {"completion": req["end"] + params.down, "n_requests": n_requests,
                        "samples": samples, "switches": switches, "requests": requests,
                        "final_N": target_N, "final_S": target_S, "failed": True,
                        "failed_range": req["range"]}
            w.avail = req["end"] + params.retry_gap
            allocate(w, t)
            continue
        completed += req["size"]
        samples.append(req["size"] / max(1e-9, (req["end"] - req["start"]) + params.h))
        if keep_requests:
            requests.append({"wid": w.wid, "start": req["start"], "end": req["end"],
                             "rate": req["rate"], "size": req["size"],
                             "range": req["range"], "aborted": False})
        w.avail = req["end"] + params.h

        if controller is not None:
            ctx = {"t": t, "samples": samples, "completed": completed,
                   "current_N": target_N, "current_S": target_S,
                   "n_requests": n_requests}
            res = controller(ctx)
            if res is not None:
                apply_switch(t, res[0], res[1], "controller")

        if w.retiring:
            workers.remove(w)
            continue
        if live_count() > target_N:
            w.retiring = True
            workers.remove(w)
            continue
        allocate(w, t)

    return {"completion": t + params.down, "n_requests": n_requests,
            "samples": samples, "switches": switches, "requests": requests,
            "final_N": target_N, "final_S": target_S, "failed": False}


def fixed(F, N, S, schedule, params, keep_requests=False, state=None,
          state_schedule=None):
    return simulate(F, N, S, schedule, params, keep_requests=keep_requests,
                    state=state, state_schedule=state_schedule)


def fixed_frontier(F, schedule, params, n_grid=N_GRID, s_grid=S_GRID, state=None):
    out = {}
    for N in n_grid:
        for S in s_grid:
            out[(N, S)] = fixed(F, N, S, schedule, params, state=state)["completion"]
    return out


def best_fixed(frontier):
    (N, S), t = min(frontier.items(), key=lambda kv: kv[1])
    return {"N": N, "S": S, "completion": t}


# ------------------------- predeclared controllers -------------------------

def make_one_switch_controller(signal_k=3, threshold=1.5, target=(16, 16 * MIB)):
    """One-switch-first causal controller (frozen family).

    Baseline: median of the first `signal_k` observed samples (all regimes
    start in the slow phase, so the baseline is established before any
    transition information exists). Trigger: median of the last `signal_k`
    samples >= threshold * baseline. Action: one joint switch to `target`.
    Only completed-request observations are used.
    """
    state = {"done": False, "baseline": None}

    def controller(ctx):
        if state["done"]:
            return None
        s = ctx["samples"]
        if len(s) < signal_k:
            return None
        if state["baseline"] is None:
            state["baseline"] = sorted(s[:signal_k])[signal_k // 2]
            return None
        recent = sorted(s[-signal_k:])[signal_k // 2]
        if recent >= threshold * state["baseline"]:
            state["done"] = True
            return target
        return None

    return controller


def oracle_one_switch(F, schedule, params, t_grid=64, start_actions=None,
                      target_actions=None, horizon=None):
    """Clairvoyant diagnostic upper bound: best (start action, exact switch
    time, target action) single joint switch. Not a policy."""
    if start_actions is None:
        start_actions = [(START_N, START_S)]
    if target_actions is None:
        target_actions = ACTION_SPACE
    if horizon is None:
        horizon = 60.0
    best = None
    for (N0, S0) in start_actions:
        for gi in range(1, t_grid + 1):
            t_switch = horizon * gi / (t_grid + 1.0)
            for (N1, S1) in target_actions:
                r = simulate(F, N0, S0, schedule, params,
                             planned_switches=[(t_switch, N1, S1)])
                if not r["switches"]:
                    continue
                if best is None or r["completion"] < best["completion"]:
                    best = {"completion": r["completion"], "start_N": N0, "start_S": S0,
                            "switch_time": t_switch, "to_N": N1, "to_S": S1}
    return best


def _optima(frontier):
    (N, S), t = min(frontier.items(), key=lambda kv: kv[1])
    other_n = [v for (n, ss), v in frontier.items() if n != N and ss == S]
    other_s = [v for (n, ss), v in frontier.items() if ss != S and n == N]
    return {"N": N, "S": S, "t": t,
            "N_margin": (min(other_n) - t) / min(other_n) if other_n else None,
            "S_margin": (min(other_s) - t) / min(other_s) if other_s else None}


def main():
    params = Params()
    sched = d2_anchor()
    states = [
        ("GOOD (no cap)", ServerState(0, 0)),
        ("C=4M g=0.05", ServerState(4 * MIB, 0.05)),
        ("C=8M g=0.05", ServerState(8 * MIB, 0.05)),
        ("C=8M g=0.10", ServerState(8 * MIB, 0.10)),
        ("C=16M g=0.05", ServerState(16 * MIB, 0.05)),
        ("C=16M g=0.10", ServerState(16 * MIB, 0.10)),
    ]
    results = {}
    for name, st in states:
        fr = fixed_frontier(FILE_SIZE, sched, params, state=st)
        results[name] = {"frontier": fr, "opt": _optima(fr)}
        o = results[name]["opt"]
        print("%-14s opt=(N=%-2d S=%-3dM) %7.2fs  Nmar=%s Smar=%s" % (
            name, o["N"], o["S"] // MIB, o["t"],
            "n/a" if o["N_margin"] is None else "%+.1f%%" % (100 * o["N_margin"]),
            "n/a" if o["S_margin"] is None else "%+.1f%%" % (100 * o["S_margin"])))
    # matrix for the most interesting state
    for name in ["GOOD (no cap)", "C=8M g=0.05"]:
        fr = results[name]["frontier"]
        print("\n%s completion matrix (s), rows N, cols S" % name)
        print("        " + "".join("%9s" % ("%dM" % (s // MIB)) for s in S_GRID))
        for n in N_GRID:
            print("  N=%-3d " % n + "".join("%9.2f" % fr[(n, s)] for s in S_GRID))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
