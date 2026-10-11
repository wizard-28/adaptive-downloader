"""Export a trained Double DQN to the portable JSON model consumed by Go."""

from __future__ import annotations

import json

from .state import FEATURE_NAMES, STATE_SPEC_VERSION, default_state_spec
from .state_v5 import FEATURE_NAMES as FEATURE_NAMES_V5
from .state_v5 import STATE_SPEC_VERSION as STATE_SPEC_VERSION_V5
from .state_v6 import FEATURE_NAMES as FEATURE_NAMES_V6
from .state_v6 import STATE_SPEC_VERSION as STATE_SPEC_VERSION_V6
from .dqn import DoubleDQN, TrainConfig
from .sim_env import ACTION_NAMES, NUM_ACTIONS

MODEL_VERSION = 1


def export_model(
    agent: DoubleDQN,
    cfg: TrainConfig,
    sim_cfg,
    label: str,
    action_names=None,
    num_actions=None,
) -> dict:
    """Build the portable model artifact dict.

    The artifact is self-describing: model version, state spec, feature names,
    action mapping, layer weights/biases, and training metadata. Go validates all
    of it before inference. action_names/num_actions default to the five-action
    v1/v2/v3 space; v4 passes the three-action concurrency-only space.
    """
    if action_names is None:
        action_names = list(ACTION_NAMES)
    if num_actions is None:
        num_actions = NUM_ACTIONS
    # Use the state spec the environment trained against (v5 sets it on its
    # config); fall back to the v1 spec for historical v1-v4 simulators.
    spec = getattr(sim_cfg, "spec", None) or default_state_spec()
    if spec.version == STATE_SPEC_VERSION:
        feature_names = list(FEATURE_NAMES)
    elif spec.version == STATE_SPEC_VERSION_V5:
        feature_names = list(FEATURE_NAMES_V5)
    elif spec.version == STATE_SPEC_VERSION_V6:
        feature_names = list(FEATURE_NAMES_V6)
    else:
        raise ValueError(f"unsupported state spec version {spec.version}")
    layers = []
    net = agent.online.net
    linear_layers = [m for m in net if hasattr(m, "weight")]
    hidden = cfg.hidden_sizes
    for i, lin in enumerate(linear_layers):
        is_last = i == len(linear_layers) - 1
        layers.append(
            {
                "in": int(lin.in_features),
                "out": int(lin.out_features),
                "weights": lin.weight.detach().cpu().numpy().astype(float).tolist(),
                "bias": lin.bias.detach().cpu().numpy().astype(float).tolist(),
                "activation": "linear" if is_last else "relu",
            }
        )
    return {
        "model_version": MODEL_VERSION,
        "name": "double-dqn-mlp",
        "state_spec": spec.to_json(),
        "feature_names": feature_names,
        "num_actions": num_actions,
        "action_names": action_names,
        "layers": layers,
        "training": {
            "seed": cfg.seed,
            "episodes": cfg.episodes,
            "gamma": cfg.gamma,
            "learning_rate": cfg.learning_rate,
            "hidden_sizes": list(hidden),
            "epsilon_final": agent.epsilon(),
            "simulator_spec": json.dumps(sim_cfg.to_json(), sort_keys=True),
            "label": label,
        },
    }
