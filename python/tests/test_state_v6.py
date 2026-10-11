import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.state_v6 import (
    FEATURE_NAMES,
    STATE_SPEC_VERSION,
    StateSpec,
    default_state_spec,
    features,
)


def test_spec_version_and_bounds():
    assert STATE_SPEC_VERSION == 3
    assert len(FEATURE_NAMES) == 13
    spec = default_state_spec()
    assert spec.version == 3
    assert spec.min_concurrency == 1
    assert spec.max_concurrency == 16
    spec.validate()


def test_spec_json_carries_go_required_chunk_bounds():
    spec = default_state_spec().to_json()
    assert spec["min_chunk_size"] == 1 << 20
    assert spec["max_chunk_size"] == 1 << 20
    for key in (
        "version",
        "min_concurrency",
        "max_concurrency",
        "throughput_scale",
        "duration_scale",
        "staleness_scale",
        "ewma_alpha",
        "counter_window",
    ):
        assert key in spec, key


def test_invalid_spec_rejected():
    for kwargs in ({"min_chunk_size": 0}, {"max_chunk_size": 1}, {"version": 2}):
        try:
            StateSpec(**kwargs).validate()
        except ValueError:
            continue
        raise AssertionError(f"invalid spec accepted: {kwargs}")


def test_features_exact_values_match_go_contract():
    # Duplicated verbatim in internal/rl/dqn/state_v6_test.go
    # (TestV6FeatureParityContractExact). Same observation as the v5 parity
    # test; only the concurrency normalization bound differs (16).
    obs = {
        "concurrency": 3,
        "total_bytes": 2000,
        "done_bytes": 500,
        "aggregate_throughput": 5 * 1024 * 1024,
        "throughput_ewma": 4 * 1024 * 1024,
        "prev_throughput": 2 * 1024 * 1024,
        "throughput_vol": 1 * 1024 * 1024,
        "request_duration_s": 0.5,
        "prev_duration_s": 0.25,
        "since_conc_change_s": 3.0,
        "window_failures": 3,
        "window_retries": 2,
        "window_attempts": 7,
        "window_completed": 5,
    }
    want = [
        2.0 / 15.0,
        0.75,
        0.625,
        0.5,
        0.375,
        0.125,
        0.5,
        0.25,
        3.0 / 7.0,
        2.0 / 7.0,
        1.0,
        0.625,
        1.0,
    ]
    got = features(obs, default_state_spec())
    assert len(got) == len(FEATURE_NAMES)
    for i, (g, w) in enumerate(zip(got, want)):
        assert abs(g - w) < 1e-12, (i, FEATURE_NAMES[i], g, w)


def test_v6_model_export_contract():
    from rl.dqn import DoubleDQN, TrainConfig
    from rl.model import export_model
    from rl.sim_env_v6 import ACTION_NAMES as V6_ACTIONS
    from rl.sim_env_v6 import SimConfig as SimConfigV6
    from rl.sim_env_v6 import SimEnv as SimEnvV6

    sim_cfg = SimConfigV6()
    SimEnvV6(sim_cfg, seed=0)  # resolves and records the v6 spec on the config
    cfg = TrainConfig(hidden_sizes=(4,), episodes=0, num_actions=3)
    agent = DoubleDQN(len(FEATURE_NAMES), cfg, seed=0)
    art = export_model(
        agent,
        cfg,
        sim_cfg,
        "test-v6",
        action_names=list(V6_ACTIONS),
        num_actions=3,
    )
    assert art["num_actions"] == 3
    assert art["state_spec"]["version"] == 3
    assert art["state_spec"]["max_concurrency"] == 16
    assert art["state_spec"]["min_chunk_size"] == 1 << 20
    assert art["feature_names"] == list(FEATURE_NAMES)
    assert art["layers"][0]["in"] == len(FEATURE_NAMES)
    assert art["layers"][-1]["out"] == 3
