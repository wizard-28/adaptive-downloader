import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.sim_env_cap import Condition, live_condition, run_policy  # noqa: E402
from rl.sim_env_joint import (  # noqa: E402
    MIB, NUM_ACTIONS, JointConfig, SimEnv, joint_spec, run_joint,
)
from rl.state import FEATURE_NAMES  # noqa: E402


def hold(state, env):
    return 0


def test_equals_cap_sim_at_fixed_one_mib():
    for sc in ("CAP8", "HEALTHY", "CAP0"):
        for n in (4, 16):
            a = run_policy(live_condition(sc), n, hold).completion_s()
            b = run_joint(live_condition(sc), n, MIB, hold, max_chunk=MIB).completion_s()
            assert a == b, (sc, n, a, b)


def test_all_bytes_once_and_ranges_tile_the_file():
    e = run_joint(live_condition("CAP0"), 16, 32 * MIB, hold)
    assert e.done_bytes_true == 512 * MIB
    assert e.splits > 0  # 32 MiB ranges leave stragglers to split


def test_no_split_at_one_mib_and_when_disabled():
    assert run_joint(live_condition("CAP0"), 16, MIB, hold).splits == 0
    assert run_joint(live_condition("CAP0"), 16, 32 * MIB, hold, split=False).splits == 0


def test_splitting_shortens_capped_large_ranges():
    with_split = run_joint(live_condition("CAP0"), 16, 32 * MIB, hold).completion_s()
    without = run_joint(live_condition("CAP0"), 16, 32 * MIB, hold, split=False).completion_s()
    assert with_split < 0.75 * without


def test_chunk_action_changes_future_allocations_only():
    c = Condition(file_size=256 * MIB, cap_at=None)
    e = SimEnv(JointConfig(window=16, start_n=4, start_chunk=MIB, condition=c), 0)
    in_flight_sizes = sorted(r.length for r in e.inflight.values())
    e.step(3)  # +4 MiB
    assert e.chunk == 5 * MIB
    # ranges already in flight keep their size
    assert all(r.length == MIB for r in e.inflight.values() if r.rid <= 4)
    assert in_flight_sizes == [MIB] * len(in_flight_sizes)
    assert max(r.length for r in e.inflight.values()) == 5 * MIB


def test_chunk_bounds_clamp():
    c = Condition(file_size=256 * MIB, cap_at=None)
    e = SimEnv(JointConfig(window=16, start_n=4, start_chunk=MIB, condition=c), 0)
    e.step(4)
    assert e.chunk == MIB
    for _ in range(12):
        e.step(3)
    assert e.chunk == 33 * MIB


def test_state_is_v1_layout():
    e = SimEnv(JointConfig(window=16, start_n=16, start_chunk=MIB, condition=live_condition("CAP8")), 0)
    assert len(e.state()) == len(FEATURE_NAMES) == 13
    assert joint_spec().version == 1
    assert NUM_ACTIONS == 5
    assert "chunk_size" in e.last_obs and "since_chunk_change_s" in e.last_obs


def test_randomize_samples_chunk_from_grid():
    e = SimEnv(JointConfig(), seed=3)
    grid = set(e.chunk_grid())
    for _ in range(30):
        e.randomize()
        e.reset()
        assert e.chunk in grid
        assert 1 <= e.target <= 16
