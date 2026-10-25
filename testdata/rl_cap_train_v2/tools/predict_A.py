#!/usr/bin/env python3
"""Stage A blind parity predictions (20-seed simulator geometric means, with the
5th-95th percentiles), from the delay-dependent calibration. Run before any A run.

Usage: python3 testdata/rl_cap_train_v2/tools/predict_A.py   (from the repository root)
"""
import json, math, os, sys
import numpy as np

sys.path.insert(0, "python")
from rl.sim_env_cap import KIB, MIB, Condition, load_calibration_multi, run_policy  # noqa: E402

BASE = "testdata/rl_cap_train_v2"
CALIB = [f"{BASE}/calibration_{d}.json" for d in (20, 40, 80, 120)]
D2 = [(0.0, 640 * KIB), (2.0, 160 * KIB), (4.0, 2500 * KIB)]


def configs(cal):
    def c(owd, **kw):
        return Condition(owd_s=owd, client_start_s=2 * owd, calib=cal, **kw)
    return {
        "A_CAP0_8x1M_80": (c(0.080, cap_at=0.0), 8),
        "A_HEALTHY_8x1M_80": (c(0.080, cap_at=None), 8),
        "A_CAP8_12x1M_120": (c(0.120, cap_at=8.0), 12),
        "A_D2custom_16x1M_120": (c(0.120, cap_at=8.0, schedule=D2), 16),
    }


def main():
    cal = load_calibration_multi(CALIB)
    out = {}
    for name, (cond, n) in configs(cal).items():
        t = [run_policy(cond, n, lambda s, e: 0, seed=k).completion_s() for k in range(20)]
        out[name] = {"n": n, "gm_s": math.exp(np.mean(np.log(t))),
                     "p5_s": float(np.percentile(t, 5)), "p95_s": float(np.percentile(t, 95))}
        print(name, {k: round(v, 2) if isinstance(v, float) else v for k, v in out[name].items()})
    json.dump({"calibration": CALIB, "predictions": out}, open(f"{BASE}/predictions_A.json", "w"), indent=1)


if __name__ == "__main__":
    main()
