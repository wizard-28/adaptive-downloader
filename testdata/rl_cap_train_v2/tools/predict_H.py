#!/usr/bin/env python3
"""Headroom gate (protocol: Held-out scenarios): for each H scenario, from the
calibrated v2 simulator (10 seeds), the best fixed N at 1 MiB, fixed 16, the
16 -> n oracle at the cap time, and the drop rule. Run before training.

Usage: python3 testdata/rl_cap_train_v2/tools/predict_H.py   (from the repository root)
"""
import json, os, sys

sys.path.insert(0, os.path.dirname(__file__))
from sim_rules import drop_s, fixed_s, oracle_best  # noqa: E402
from rl.sim_env_cap import live_condition, load_calibration_multi  # noqa: E402

BASE = "testdata/rl_cap_train_v2"
CALIB = [f"{BASE}/calibration_{d}.json" for d in (20, 40, 80, 120)]
SEEDS = list(range(100, 110))


def main():
    cal = load_calibration_multi(CALIB)
    out = {}
    for sc in ("H_C12", "H_RTT110", "H_DIP", "H_C20", "CAP8", "HEALTHY"):
        c = live_condition(sc, cal)
        fx = {n: fixed_s(c, n, SEEDS) for n in range(1, 17)}
        bn = min(fx, key=fx.get)
        row = {"best_fixed_n": bn, "best_fixed_s": fx[bn], "fixed_16_s": fx[16], "drop_s": drop_s(c, SEEDS)}
        if c.cap_at is not None and c.cap_at > 0:
            ot, om = oracle_best(c, SEEDS)
            row.update(oracle_s=ot, oracle_n=om, headroom=1 - ot / fx[bn])
        if sc.startswith("H_"):
            row["kept"] = row.get("headroom", 0) >= 0.05
        out[sc] = row
        print(sc, {k: round(v, 3) if isinstance(v, float) else v for k, v in row.items()}, flush=True)
    json.dump({"calibration": CALIB, "seeds": SEEDS, "scenarios": out}, open(f"{BASE}/predictions_H.json", "w"), indent=1)


if __name__ == "__main__":
    main()
