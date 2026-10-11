"""Simulator correctness audit tests for the v5 environment.

These pin the semantics listed in the v5 simulator audit
(testdata/diagnostics/dqn_validation_v5/simulator_correctness_report.*).
"""

import os
import sys

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.sim_env_v5 import FIXED_CHUNK, NUM_ACTIONS, Phase, SimConfig, SimEnv

MIB = 1024 * 1024


def ph(rate=4 * MIB, link=0, lat=0.0, loss=0.0, cong=0.0, ovh=0.0):
    return Phase(
        rate=rate,
        link_bandwidth=link,
        latency_s=lat,
        loss_percent=loss,
        congestion=cong,
        overhead_s=ovh,
    )


def cfg(phases, size=16 * MIB, interval=1, steps=1000, cmin=1, cmax=4):
    return SimConfig(
        file_size=size,
        phases=phases,
        decision_interval=interval,
        max_episode_steps=steps,
        min_concurrency=cmin,
        max_concurrency=cmax,
    )


def run_fixed(c, config, seed=1):
    e = SimEnv(config, seed=seed)
    e.reset()
    e.concurrency = c
    steps = []
    while not e.done():
        steps.append(e.step(0))
    return e, steps


@pytest.mark.parametrize("c", [1, 4])
@pytest.mark.parametrize("interval", [1, 2, 4])
def test_exact_byte_accounting_and_time_sum(c, interval):
    size = 16 * MIB
    e, steps = run_fixed(c, cfg([ph(lat=0.01)], size=size, interval=interval))
    assert e.done_bytes == size
    assert e._done and not e._truncated
    assert sum(s["requests"] for s in steps) == size // FIXED_CHUNK
    assert sum(s["requests"] for s in steps[:-1]) == (len(steps) - 1) * interval
    # reward is exactly -elapsed per step; episode time is the sum of steps
    for s in steps:
        assert s["reward"] == -s["elapsed"]
    assert abs(sum(s["elapsed"] for s in steps) - e.elapsed) < 1e-9
    assert steps[-1]["remaining_bytes"] == 0
    assert all(s["remaining_bytes"] > 0 for s in steps[:-1])


def test_partial_final_chunk_counted_once():
    size = 5 * MIB + 123
    e, steps = run_fixed(2, cfg([ph()], size=size, interval=4))
    assert e.done_bytes == size
    assert sum(s["requests"] for s in steps) == 6


def test_c4_transfers_four_requests_in_parallel():
    # interval=4: one step is one 4-wide wave; with no latency/overhead the
    # step takes one request's service time, not four.
    e = SimEnv(cfg([ph(rate=1 * MIB)], interval=4), seed=1)
    e.reset()
    e.concurrency = 4
    r = e.step(0)
    assert r["requests"] == 4
    assert abs(r["elapsed"] - 1.0) < 1e-9
    e.reset()
    e.concurrency = 1
    r = e.step(0)
    assert abs(r["elapsed"] - 4.0) < 1e-9


class _Fail:
    def random(self):
        return 0.0


class _Pass:
    def random(self):
        return 0.999999


def test_failed_attempt_costs_time_but_bytes_complete_once():
    c = cfg([ph(rate=1 * MIB, loss=50.0)], size=4 * MIB, interval=1)
    ok = SimEnv(c, seed=1)
    ok.reset()
    ok.concurrency = 1
    ok.rng = _Pass()
    bad = SimEnv(cfg([ph(rate=1 * MIB, loss=50.0)], size=4 * MIB, interval=1), seed=1)
    bad.reset()
    bad.concurrency = 1
    bad.rng = _Fail()
    r_ok, r_bad = ok.step(0), bad.step(0)
    # the failed range still credits its bytes exactly once ...
    assert ok.done_bytes == bad.done_bytes == FIXED_CHUNK
    assert r_bad["remaining_bytes"] == r_ok["remaining_bytes"]
    # ... but pays the retransmission time
    assert abs(r_ok["elapsed"] - 1.0) < 1e-9
    assert abs(r_bad["elapsed"] - 2.0) < 1e-9


def test_failure_retry_completed_accounting_per_attempt():
    e = SimEnv(cfg([ph(loss=50.0)], interval=1), seed=1)
    e.reset()
    e.concurrency = 1
    e.rng = _Fail()
    e.step(0)
    o = e._obs()
    # attempt 1 failed, attempt 2 is the successful retry
    assert (o["window_attempts"], o["window_failures"]) == (2, 1)
    assert (o["window_retries"], o["window_completed"]) == (1, 1)
    assert e.fail_window == [1, 0] and e.retry_window == [0, 1]
    assert e.done_window == [0, 1]
    e.rng = _Pass()
    e.step(0)
    assert e.fail_window == [1, 0, 0]
    assert e.done_window == [0, 1, 1]


def test_window_is_bounded():
    e, _ = run_fixed(1, cfg([ph(loss=90.0)], interval=2))
    n = e.spec.counter_window
    assert len(e.fail_window) <= n and len(e.done_window) <= n


def test_wall_clock_transition_at_configured_elapsed_time():
    phases = [ph(rate=1 * MIB), ph(rate=8 * MIB)]
    e = SimEnv(cfg(phases, size=8 * MIB, interval=1), seed=1)
    e.reset()
    e.concurrency = 1
    e.phase_ends = [2.5, float("inf")]
    served = []
    while not e.done():
        t0 = e.elapsed
        r = e.step(0)
        served.append((t0, r["elapsed"]))
    # requests start at t=0,1,2 (phase 0, 1 s each); the third starts before
    # 2.5 s so it is still slow; the fourth starts at t=3 > 2.5 -> fast.
    assert [round(d, 6) for _, d in served[:4]] == [1.0, 1.0, 1.0, 0.125]
    assert e.transitions == 1


def test_transition_request_index_depends_on_request_timing():
    # The same wall-clock boundary is crossed at a different request index when
    # per-request service time differs (time-based, not request-count-based).
    def first_fast_index(rate):
        phases = [ph(rate=rate), ph(rate=64 * MIB)]
        e = SimEnv(cfg(phases, size=16 * MIB, interval=1), seed=1)
        e.reset()
        e.concurrency = 1
        e.phase_ends = [3.0, float("inf")]
        i = 0
        while not e.done():
            e.step(0)
            i += 1
            if e.phase_index == 1:
                return i
        return None

    slow = first_fast_index(1 * MIB)  # 1 s/request  -> boundary at request 3
    fast = first_fast_index(4 * MIB)  # 0.25 s/request -> boundary at request 12
    assert slow == 3
    assert fast == 12
    assert slow != fast


def test_seed_determinism_including_randomize():
    def trace(seed):
        c = SimConfig(decision_interval=2, max_episode_steps=500)
        e = SimEnv(c, seed=seed)
        out = []
        for _ in range(5):
            e.randomize()
            e.reset()
            while not e.done():
                r = e.step(len(out) % NUM_ACTIONS)
                out.append((r["elapsed"], r["concurrency"], r["phase_index"]))
        return out

    assert trace(7) == trace(7)
    assert trace(7) != trace(8)


def test_actions_respect_bounds_and_noop_does_not_mutate():
    e = SimEnv(cfg([ph()], interval=1), seed=1)
    e.reset()
    e.concurrency = 4
    r = e.step(1)
    assert r["no_op"] and r["concurrency"] == 4
    e.concurrency = 1
    r = e.step(2)
    assert r["no_op"] and r["concurrency"] == 1
    for _ in range(10):
        e.step(1)
        assert 1 <= e.concurrency <= 4
    r = e.step(2)
    assert not r["no_op"] and r["concurrency"] == 3


@pytest.mark.parametrize("bad", [-1, 3, 99])
def test_invalid_action_raises_without_mutation(bad):
    e = SimEnv(cfg([ph()], interval=2), seed=1)
    e.reset()
    snap = (e.concurrency, e.done_bytes, e.elapsed, e.steps, len(e.action_history))
    with pytest.raises(ValueError):
        e.step(bad)
    assert snap == (
        e.concurrency,
        e.done_bytes,
        e.elapsed,
        e.steps,
        len(e.action_history),
    )


def test_terminal_vs_truncation():
    e = SimEnv(cfg([ph()], size=4 * MIB, interval=2, steps=100), seed=1)
    e.reset()
    while not e.done():
        e.step(0)
    assert e._done and not e._truncated and e.done_bytes == e.total
    with pytest.raises(RuntimeError):
        e.step(0)

    t = SimEnv(cfg([ph()], size=64 * MIB, interval=1, steps=3), seed=1)
    t.reset()
    for _ in range(3):
        t.step(0)
    assert t._truncated and not t._done and t.done_bytes < t.total

    # finishing on exactly the max step is terminal, not truncated
    f = SimEnv(cfg([ph()], size=2 * MIB, interval=1, steps=2), seed=1)
    f.reset()
    f.step(0)
    f.step(0)
    assert f._done and not f._truncated


def test_stationary_episode_never_transitions():
    c = SimConfig(
        decision_interval=1,
        max_episode_steps=1000,
        rand_phase_count=(1, 1),
        rand_file_size=(8 * MIB, 16 * MIB),
    )
    e = SimEnv(c, seed=3)
    for _ in range(20):
        e.randomize()
        e.reset()
        assert len(e.cfg.phases) == 1
        while not e.done():
            e.step(0)
        assert e.transitions == 0 and e.phase_index == 0


def test_multi_phase_episodes_transition_meaningfully():
    c = SimConfig(
        decision_interval=1,
        max_episode_steps=1000,
        rand_phase_count=(2, 3),
        rand_file_size=(8 * MIB, 32 * MIB),
    )
    e = SimEnv(c, seed=11)
    hit = 0
    n = 100
    for _ in range(n):
        e.randomize()
        e.reset()
        while not e.done():
            e.step(0)
        hit += e.transitions >= 1
    assert hit / n >= 0.5


def test_concurrency_never_leaves_bounds_in_randomized_rollout():
    e = SimEnv(SimConfig(decision_interval=1, max_episode_steps=300), seed=5)
    for ep in range(10):
        e.randomize()
        e.reset()
        k = 0
        while not e.done():
            e.step((k * 7 + ep) % NUM_ACTIONS)
            k += 1
            assert 1 <= e.concurrency <= 4
