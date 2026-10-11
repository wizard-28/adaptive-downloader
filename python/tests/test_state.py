import math
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.state import FEATURE_NAMES, StateSpec, default_state_spec, features


def test_feature_dim():
    assert len(FEATURE_NAMES) == 13


def test_normalization_and_determinism():
    obs = {
        "concurrency": 3,
        "chunk_size": 65536,
        "total_bytes": 1000,
        "done_bytes": 250,
        "aggregate_throughput": 4 << 20,
        "request_duration_s": 0.5,
        "throughput_ewma": 4 << 20,
        "prev_throughput": 2 << 20,
        "throughput_vol": 1 << 20,
        "prev_duration_s": 0.25,
        "since_conc_change_s": 2.0,
        "since_chunk_change_s": 1.0,
        "window_failures": 2,
        "window_retries": 1,
        "window_attempts": 8,
    }
    x = features(obs, default_state_spec())
    assert len(x) == 13
    assert all(-1.0 <= v <= 1.0 and math.isfinite(v) for v in x)
    assert abs(x[0] - 2.0 / 3.0) < 1e-9
    assert abs(x[2] - 0.75) < 1e-9
    assert features(obs) == x


def test_remaining_bytes_informative():
    seen = set()
    for done in range(0, 1001, 100):
        x = features(
            {
                "concurrency": 1,
                "chunk_size": 32768,
                "total_bytes": 1000,
                "done_bytes": done,
            }
        )
        seen.add(round(x[2], 3))
    assert len(seen) >= 10


def test_bad_spec():
    try:
        StateSpec(throughput_scale=0).validate()
        assert False
    except ValueError:
        pass
