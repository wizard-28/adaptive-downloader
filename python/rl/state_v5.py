"""DQN state contract v5 (state spec version 2), mirroring internal/rl/dqn/state_v5.go.

Candidate v5 uses a concurrency-focused TEMPORAL state with a 16 MiB horizon and a
concurrency-only action space. Chunk size is fixed at 1 MiB and is not a control
variable, so the state carries no chunk-size feature.

The feature order and normalization MUST match the Go implementation; the model
artifact records the state spec version and feature names so Go inference rejects
a mismatch. Version 2 is distinct from the historical version 1 layout used by
Candidates v1-v4.
"""

from __future__ import annotations

import math
from dataclasses import dataclass, asdict

STATE_SPEC_VERSION = 2

FEATURE_NAMES = [
    "concurrency_norm",  # normalized current concurrency
    "remaining_bytes_frac",  # remaining/total, informative all episode
    "aggregate_throughput_norm",  # current aggregate throughput / scale
    "aggregate_throughput_ewma_norm",  # EWMA of aggregate throughput / scale
    "aggregate_throughput_slope",  # change vs previous observation / scale
    "aggregate_throughput_volatility",  # EWMA of |delta| / scale
    "request_duration_norm",  # request duration / scale
    "request_duration_slope",  # change vs previous duration / scale
    "recent_failure_rate",  # failures in window / window attempts, [0,1]
    "recent_retry_rate",  # retries in window / window attempts, [0,1]
    "since_concurrency_change_norm",  # time since last effective concurrency change / scale
    "recent_completed_norm",  # completed requests in window / window size, [0,1]
    "throughput_improvement",  # signed (thr - prev_thr)/prev_thr in [-1,1]
]


@dataclass(frozen=True)
class StateSpec:
    version: int = STATE_SPEC_VERSION
    min_concurrency: int = 1
    max_concurrency: int = 4
    # Chunk-size bounds are informational in v5 (chunk is fixed at 1 MiB), but
    # the Go StateSpec.Validate contract requires finite positive chunk bounds,
    # so the exported spec must carry them.
    min_chunk_size: int = 1 << 20
    max_chunk_size: int = 1 << 20
    throughput_scale: float = 8.0 * 1024 * 1024
    duration_scale: float = 1.0
    staleness_scale: float = 1.0
    ewma_alpha: float = 0.3
    counter_window: int = 8

    def validate(self) -> None:
        if self.version != STATE_SPEC_VERSION:
            raise ValueError(f"unsupported state spec version {self.version}")
        if self.min_concurrency < 1 or self.max_concurrency < self.min_concurrency:
            raise ValueError("invalid concurrency bounds")
        if self.min_chunk_size < 1 or self.max_chunk_size < self.min_chunk_size:
            raise ValueError("invalid chunk bounds")
        for name in ("throughput_scale", "duration_scale", "staleness_scale"):
            v = getattr(self, name)
            if not math.isfinite(v) or v <= 0:
                raise ValueError(f"{name} must be finite and > 0")
        if not (0.0 < self.ewma_alpha <= 1.0):
            raise ValueError("ewma_alpha must be in (0,1]")
        if self.counter_window < 1:
            raise ValueError("counter_window must be >= 1")

    def to_json(self) -> dict:
        return asdict(self)


def default_state_spec() -> StateSpec:
    return StateSpec()


def _normalize(value: float, lo: float, hi: float) -> float:
    if hi <= lo:
        return 0.0
    return _clamp01((value - lo) / (hi - lo))


def _clamp01(v: float) -> float:
    return 0.0 if v < 0 else (1.0 if v > 1 else v)


def _clamp_pm1(v: float) -> float:
    return -1.0 if v < -1 else (1.0 if v > 1 else v)


def features(obs: dict, spec: StateSpec | None = None) -> list[float]:
    """Build the normalized v5 feature vector for a raw observation dict.

    Mirrors Go ObservationV5.Features. Raises ValueError on non-finite output.
    """
    spec = spec or default_state_spec()
    total = float(obs.get("total_bytes", 0))
    done = float(obs.get("done_bytes", 0))
    remaining = (total - done) / total if total > 0 else 0.0

    throughput = float(obs.get("aggregate_throughput", 0.0))
    prev_thr = float(obs.get("prev_throughput", 0.0))
    ewma = float(obs.get("throughput_ewma", 0.0))
    vol = float(obs.get("throughput_vol", 0.0))
    dur = float(obs.get("request_duration_s", 0.0))
    prev_dur = float(obs.get("prev_duration_s", 0.0))

    attempts = float(obs.get("window_attempts", 0))
    fail_rate = (
        _clamp01(float(obs.get("window_failures", 0)) / attempts)
        if attempts > 0
        else 0.0
    )
    retry_rate = (
        _clamp01(float(obs.get("window_retries", 0)) / attempts)
        if attempts > 0
        else 0.0
    )
    completed = float(obs.get("window_completed", 0))
    completed_norm = (
        _clamp01(completed / spec.counter_window) if spec.counter_window > 0 else 0.0
    )

    improvement = 0.0
    if prev_thr > 0:
        improvement = _clamp_pm1((throughput - prev_thr) / prev_thr)

    x = [
        _normalize(
            float(obs.get("concurrency", 0)), spec.min_concurrency, spec.max_concurrency
        ),
        _clamp01(remaining),
        _clamp01(throughput / spec.throughput_scale),
        _clamp01(ewma / spec.throughput_scale),
        _clamp_pm1((throughput - prev_thr) / spec.throughput_scale),
        _clamp01(vol / spec.throughput_scale),
        _clamp01(dur / spec.duration_scale),
        _clamp_pm1((dur - prev_dur) / spec.duration_scale),
        fail_rate,
        retry_rate,
        _clamp01(float(obs.get("since_conc_change_s", 0.0)) / spec.staleness_scale),
        completed_norm,
        improvement,
    ]
    if len(x) != len(FEATURE_NAMES):
        raise ValueError(f"built {len(x)} features, want {len(FEATURE_NAMES)}")
    for i, v in enumerate(x):
        if not math.isfinite(v):
            raise ValueError(f"feature {i} not finite")
    return x
