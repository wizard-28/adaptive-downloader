import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.sim_env_v6 import FIXED_CHUNK, NUM_ACTIONS, Phase, SimConfig, SimEnv

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
        min_concurrency=1,
        max_concurrency=16,
    )
    base.update(kw)
    return SimConfig(**base)


def test_action_space_concurrency_range_and_fixed_chunk():
    assert NUM_ACTIONS == 3
    assert FIXED_CHUNK == MIB
    e = SimEnv(fixed_cfg(), seed=1)
    s = e.reset()
    assert len(s) == 13
    assert 1 <= e.concurrency <= 16


def test_decision_cadence_is_one_request_per_decision():
    # Live runner contract: one SelectAction per completed attempt. The
    # simulator must therefore complete exactly one request per decision.
    e = SimEnv(fixed_cfg(), seed=5)
    e.reset()
    e.concurrency = 4
    r = e.step(0)
    assert r["requests"] == 1
    # Pipeline accounting with continuously busy workers: elapsed = svc/C.
    svc = 0.02 + 0.02 * (4 - 1) + MIB / (4 * MIB)
    assert abs(r["elapsed"] - svc / 4) < 1e-9
    decisions = 1
    while not e.done():
        e.step(0)
        decisions += 1
    assert decisions == 16  # 16 MiB / 1 MiB ranges, one decision each


def test_action_applies_to_the_interval_after_the_decision():
    e = SimEnv(fixed_cfg(), seed=7)
    e.reset()
    e.concurrency = 1
    r1 = e.step(0)  # decision at C=1, interval runs at C=1
    r2 = e.step(1)  # action increases to C=2 before the interval
    assert e.concurrency == 2
    assert r2["requests"] == 1
    assert r2["elapsed"] < r1["elapsed"]  # pipeline halves the per-decision time
    svc2 = 0.02 + 0.02 * (2 - 1) + MIB / (4 * MIB)
    assert abs(r2["elapsed"] - svc2 / 2) < 1e-9


def test_shared_bottleneck_creates_diminishing_returns():
    def total(link, c):
        e = SimEnv(fixed_cfg(link_bandwidth=link), seed=11)
        e.reset()
        e.min_concurrency = c
        e.max_concurrency = c
        e.concurrency = c
        t = 0.0
        while not e.done():
            t += e.step(0)["elapsed"]
        return t

    capped_1, capped_16 = total(2 * MIB, 1), total(2 * MIB, 16)
    uncapped_1, uncapped_16 = total(0, 1), total(0, 16)
    # Under a shared 2 MiB/s bottleneck, C=16 gives almost no benefit.
    assert capped_16 > capped_1 * 0.9
    assert capped_16 < capped_1 * 1.2
    # Without a shared bottleneck, higher concurrency is strongly rewarded.
    assert uncapped_16 < uncapped_1 / 4.0


def test_phase_transitions_are_reached_and_applied():
    phases = [
        Phase(rate=4 * MIB, link_bandwidth=0, latency_s=0.0, loss_percent=0.0),
        Phase(rate=4 * MIB, link_bandwidth=0, latency_s=0.0, loss_percent=0.0),
        Phase(rate=4 * MIB, link_bandwidth=0, latency_s=0.0, loss_percent=0.0),
    ]
    cfg = SimConfig(
        file_size=4 * MIB,
        min_concurrency=1,
        max_concurrency=16,
        decision_interval=1,
        max_episode_steps=100,
        phases=phases,
        rand_phase_seconds=(0.2, 0.2),
    )
    e = SimEnv(cfg, seed=3)
    e.reset()
    e.concurrency = 1
    while not e.done():
        e.step(0)
    assert e.transitions == 2
    assert e.phase_index == 2


def test_determinism_and_truncation():
    def run():
        e = SimEnv(fixed_cfg(), seed=13)
        e.reset()
        out = []
        while not e.done() and len(out) < 100:
            out.append(e.step(0)["next_state"])
        return out

    assert run() == run()

    big = SimEnv(fixed_cfg(file_size=100 * MIB, max_episode_steps=3), seed=2)
    big.reset()
    for _ in range(3):
        big.step(0)
    assert big.done() and big._truncated


def test_windows_are_per_attempt():
    e = SimEnv(fixed_cfg(loss_percent=50.0), seed=3)
    e.reset()
    e.concurrency = 1

    class _Fail:
        def random(self):
            return 0.0

    e.rng = _Fail()
    e.step(0)
    o = e._obs()
    assert o["window_attempts"] == 1
    assert o["window_failures"] == 1
    assert o["window_completed"] == 0
