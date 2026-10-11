import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.sim_env_v5 import FIXED_CHUNK, NUM_ACTIONS, Phase, SimConfig, SimEnv

MIB = 1024 * 1024


def fixed_cfg(**kw):
    base = dict(
        file_size=16 * MIB,
        rate=4 * MIB,
        link_bandwidth=0,
        latency_s=0.02,
        loss_percent=0.0,
        congestion=0.0,
        overhead_s=0.02,
        decision_interval=1,
        max_episode_steps=400,
    )
    base.update(kw)
    return SimConfig(**base)


def test_action_space_and_fixed_chunk():
    assert NUM_ACTIONS == 3
    assert FIXED_CHUNK == MIB


def test_interval_does_not_suppress_concurrency():
    # Regression: with decision_interval == 1 the old batch model ran exactly
    # one request per step and charged its full service time, so raising
    # concurrency could not help (it only added overhead). Concurrency must
    # instead shorten the pipeline time for the same completed work.
    elapsed = {}
    for c in (1, 4):
        e = SimEnv(fixed_cfg(decision_interval=1), seed=5)
        e.reset()
        e.concurrency = c
        r = e.step(0)
        assert r["requests"] == 1
        elapsed[c] = r["elapsed"]
    assert elapsed[4] < elapsed[1]


def test_pipeline_accounting_matches_service_over_concurrency():
    e = SimEnv(fixed_cfg(decision_interval=1), seed=5)
    e.reset()
    e.concurrency = 4
    r = e.step(0)
    svc = 0.02 + 0.02 * (4 - 1) + MIB / (4 * MIB)
    assert abs(r["elapsed"] - svc / 4) < 1e-9


def test_decision_interval_sets_requests_per_step():
    for interval in (1, 2, 4):
        e = SimEnv(fixed_cfg(decision_interval=interval), seed=5)
        e.reset()
        e.concurrency = 2
        r = e.step(0)
        assert r["requests"] == interval
        assert r["elapsed"] > 0


def test_wall_clock_transition_applies_mid_step():
    cfg = SimConfig(
        file_size=4 * MIB,
        min_concurrency=1,
        max_concurrency=1,
        decision_interval=4,
        max_episode_steps=100,
        phases=[
            Phase(rate=1 * MIB, link_bandwidth=0, latency_s=0.0, loss_percent=0.0),
            Phase(rate=8 * MIB, link_bandwidth=0, latency_s=0.0, loss_percent=0.0),
        ],
    )
    e = SimEnv(cfg, seed=1)
    e.reset()
    e.phase_ends = [0.1, float("inf")]
    r = e.step(0)
    assert r["requests"] == 4
    # Request 1 runs in phase 0 (1 MiB / 1 MiB/s = 1.0 s); requests 2-4 start
    # after the 0.1 s transition and run in phase 1 (0.125 s each).
    assert abs(r["elapsed"] - 1.375) < 1e-9
    assert r["phase_index"] == 1
    assert e.transitions == 1


def test_phase_transitions_are_reached_during_episode():
    phases = [
        Phase(rate=4 * MIB, link_bandwidth=0, latency_s=0.0, loss_percent=0.0)
        for _ in range(3)
    ]
    cfg = SimConfig(
        file_size=2 * MIB,
        decision_interval=1,
        max_episode_steps=100,
        phases=phases,
    )
    e = SimEnv(cfg, seed=4)
    e.reset()
    e.phase_ends = [0.01, 0.02, float("inf")]
    e.concurrency = 1
    e.step(0)
    assert e.phase_index == 2
    assert e.transitions == 2


class _AlwaysFail:
    def random(self):
        return 0.0


def test_windows_are_per_attempt():
    # Regression: the old simulator pushed one window sample per decision
    # interval, so window_attempts counted intervals and failures were
    # collapsed to 0/1; the Go live contract counts attempts.
    e = SimEnv(fixed_cfg(decision_interval=2, loss_percent=50.0), seed=3)
    e.reset()
    e.concurrency = 1
    e.rng = _AlwaysFail()
    r = e.step(0)
    assert r["requests"] == 2
    o = e._obs()
    assert o["window_attempts"] == 2
    assert o["window_failures"] == 2
    assert o["window_retries"] == 2
    assert o["window_completed"] == 0


def test_completed_window_counts_successes():
    e = SimEnv(fixed_cfg(decision_interval=2), seed=3)
    e.reset()
    e.concurrency = 1
    e.step(0)
    o = e._obs()
    assert o["window_attempts"] == 2
    assert o["window_completed"] == 2
    assert o["window_failures"] == 0


def test_duration_slope_uses_distinct_observations():
    # Regression: request_duration_s and prev_duration_s used to be the same
    # value, so request_duration_slope was always exactly zero.
    e = SimEnv(fixed_cfg(decision_interval=1), seed=3)
    e.reset()
    e.concurrency = 1
    e.step(0)
    e.step(1)  # increase concurrency -> a different request service time
    o = e._obs()
    assert o["request_duration_s"] != o["prev_duration_s"]
    assert e.state()[7] > 0.0


def test_determinism_termination_truncation():
    def run():
        e = SimEnv(fixed_cfg(file_size=2 * MIB, decision_interval=2), seed=11)
        e.reset()
        out = []
        while not e.done() and len(out) < 1000:
            out.append(e.step(0))
        return out

    a, b = run(), run()
    assert [r["next_state"] for r in a] == [r["next_state"] for r in b]
    assert a[-1]["done"]

    big = SimEnv(fixed_cfg(file_size=100 * MIB, max_episode_steps=3), seed=2)
    big.reset()
    for _ in range(3):
        big.step(0)
    assert big.done() and big._truncated
