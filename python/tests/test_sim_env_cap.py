import math
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.sim_env_cap import (  # noqa: E402
    MIB, NUM_ACTIONS, Condition, SimConfig, SimEnv, _inv_cdf, cap_spec, live_condition, run_policy,
)
from rl.state_v6 import FEATURE_NAMES  # noqa: E402


def hold(state, env):
    return 0


def env_for(cond, n, window=16):
    return SimEnv(SimConfig(window=window, start_n=n, condition=cond), seed=0)


def test_single_worker_cycle_is_deterministic_without_calibration():
    # N=1, constant rate, no cap: each request = owd + n/rate + excess + owd.
    c = Condition(file_size=4 * MIB, rate0=2 * MIB, rate1=2 * MIB, cap_at=None)
    e = run_policy(c, 1, hold)
    cycle = 0.040 + 0.5 + 0.023 + 0.040
    assert math.isclose(e.completion_s(), c.client_start_s + 4 * cycle, rel_tol=1e-9)
    assert e.done_bytes_true == 4 * MIB


def test_all_bytes_complete_and_one_decision_per_attempt():
    e = run_policy(live_condition("CAP8"), 16, hold)
    assert e.done_bytes_true == 512 * MIB
    assert e.attempts == 512
    assert e.steps == 512  # live also decides at the final completion


def test_cap_rate_counts_only_capped_requests_in_service():
    c = Condition(file_size=64 * MIB, rate0=8 * MIB, rate1=8 * MIB, cap_at=0.0, cap_c=8 * MIB, cap_gamma=0.05)
    e = env_for(c, 4)
    # 4 requests arrive at once: k = 1..4 in arrival order.
    want = [int(8 * MIB / (k * (1 + 0.05 * (k - 1)))) for k in (1, 2, 3, 4)]
    first = sorted(ev[4] for ev in e.events) + [e.rates[0]]
    assert sorted(int(r) for r in first) == sorted(want)


def test_requests_before_cap_are_not_counted():
    # Cap switches on after the first requests arrive: they keep the base rate
    # and do not count towards k for later capped requests.
    c = Condition(file_size=64 * MIB, rate0=640 * 1024, rate1=640 * 1024, cap_at=0.5)
    e = env_for(c, 16)
    assert all(int(ev[4]) == 640 * 1024 for ev in e.events)
    assert e.capped_ends == []


def test_decrease_retires_one_worker_increase_starts_one():
    c = Condition(file_size=256 * MIB, cap_at=None)
    e = env_for(c, 8)
    # between decisions one completed worker awaits its decision, so the
    # workers alive are in_flight + 1
    assert e.in_flight + 1 == 8
    e.step(2)  # completer retires; the next completion is then popped
    assert e.target == 7 and e.in_flight + 1 == 7
    e.step(1)  # completer continues and one new worker starts
    assert e.target == 8 and e.in_flight + 1 == 8


def test_increase_while_over_target_does_not_spawn():
    c = Condition(file_size=256 * MIB, cap_at=None)
    e = env_for(c, 8)
    e.step(2)
    e.step(2)
    assert e.in_flight + 1 == 6
    e.step(1)  # back to 7: exactly one new worker
    assert e.in_flight + 1 == 7
    e.step(2)  # 6: over target by one, the completer retires
    e.step(1)  # 7 again
    assert e.in_flight + 1 == 7


def test_aggregate_observable_matches_policy_runner_formula():
    e = env_for(live_condition("HEALTHY"), 16, window=4)
    for _ in range(40):
        e.step(0)
    rec = e.recent
    span = max(t for _, _, t in rec) - min(t - d for _, d, t in rec)
    assert math.isclose(e.last_obs["aggregate_throughput"], sum(b for b, _, _ in rec) / span)
    assert len(rec) == 4


def test_state_layout_is_v6():
    e = env_for(live_condition("CAP8"), 16)
    assert len(e.state()) == len(FEATURE_NAMES) == 13
    assert cap_spec().version == 3
    assert NUM_ACTIONS == 3


def test_cap_makes_fewer_connections_faster_when_capped_from_start():
    t3 = run_policy(live_condition("CAP0"), 3, hold).completion_s()
    t16 = run_policy(live_condition("CAP0"), 16, hold).completion_s()
    h3 = run_policy(live_condition("HEALTHY"), 3, hold).completion_s()
    h16 = run_policy(live_condition("HEALTHY"), 16, hold).completion_s()
    assert t3 < t16
    assert h16 < h3


def test_inverse_cdf_interpolates():
    q = list(range(101))
    assert _inv_cdf(q, 0.0) == 0
    assert math.isclose(_inv_cdf(q, 0.505), 50.5)
    assert _inv_cdf(q, 1.0) == 100


def test_calibrated_runs_are_seeded():
    calib = {"rate_class_threshold_bytes_per_s": 2 * MIB, "measured_owd_s": 0.04,
             "hi": {"excess": {"q": [0.02] * 50 + [0.03] * 51}, "client_extra": {"q": [0.08] * 90 + [0.3] * 11}},
             "lo": {"excess": {"q": [0.04] * 101}, "client_extra": {"q": [0.08] * 101}}}
    a = run_policy(live_condition("HEALTHY", calib), 16, hold, seed=3).completion_s()
    b = run_policy(live_condition("HEALTHY", calib), 16, hold, seed=3).completion_s()
    c = run_policy(live_condition("HEALTHY", calib), 16, hold, seed=4).completion_s()
    assert a == b and a != c


def test_randomize_samples_valid_conditions():
    e = SimEnv(SimConfig(), seed=5)
    for _ in range(50):
        e.randomize()
        e.reset()
        c = e._cond
        assert 128 * MIB <= c.file_size <= 512 * MIB
        assert 1 <= e.target <= 16
        assert c.cap_at is None or c.cap_at >= 0


def test_multi_phase_schedule():
    c = live_condition("G_D2")
    assert c.base_rate(0.5) == 640 * 1024
    assert c.base_rate(2.5) == 160 * 1024
    assert c.base_rate(5.0) == 2500 * 1024
