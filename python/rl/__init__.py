"""Sequential RL (Double DQN) training package for the adaptive downloader.

This package trains a small discrete-action Double DQN against a fast analytical
training environment and exports a portable JSON model artifact that the Go
downloader loads for deterministic inference.

It does NOT modify, replace, or retrain the retained LinUCB baseline, and it
makes NO performance claim yet. The training environment conditions are NOT the
final evaluation conditions.
"""

from .state import (
    FEATURE_NAMES,
    STATE_SPEC_VERSION,
    StateSpec,
    default_state_spec,
    features,
)
from .sim_env import SimConfig, SimEnv
from .dqn import DoubleDQN, TrainConfig, train
from .model import export_model

__all__ = [
    "FEATURE_NAMES",
    "STATE_SPEC_VERSION",
    "StateSpec",
    "default_state_spec",
    "features",
    "SimConfig",
    "SimEnv",
    "DoubleDQN",
    "TrainConfig",
    "train",
    "export_model",
]
