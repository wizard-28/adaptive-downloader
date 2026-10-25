#!/usr/bin/env python3
"""Amendment A3: simulator predictions for every Stage C and D arm (calibrated
v2 simulator, seeds 100-109), after training and before any C or D run.

Usage: python3 testdata/rl_cap_train_v2/tools/predict_CD.py   (from the repository root)
"""
import json, os, sys

sys.path.insert(0, os.path.dirname(__file__))
from sim_rules import drop_s, fixed_s, gm  # noqa: E402
from rl.sim_env_cap import live_condition, load_calibration_multi, run_policy  # noqa: E402

sys.path.insert(0, "python")
from train_cap import NumpyPolicy  # noqa: E402

BASE = "testdata/rl_cap_train_v2"
CALIB = [f"{BASE}/calibration_{d}.json" for d in (20, 40, 80, 120)]
SEEDS = list(range(100, 110))
MODELS = {"v2": f"{BASE}/model/dqn_cap_v2_candidate.json", "v1": "testdata/rl_cap_train/model/dqn_cap_candidate.json"}


def main():
    cal = load_calibration_multi(CALIB)
    man = json.load(open(f"{BASE}/manifest.json"))
    pols = {k: NumpyPolicy(json.load(open(p))) for k, p in MODELS.items()}
    out = {}
    for st in ("C", "D"):
        for sc, arms in man["stages"][st]["scenario_arms"].items():
            c = live_condition(sc, cal)
            row = {}
            for a in arms:
                if a in pols:
                    row[a] = gm([run_policy(c, 16, pols[a], window=16, spec=pols[a].spec, seed=k).completion_s()
                                 for k in SEEDS])
                elif a == "drop":
                    row[a] = drop_s(c, SEEDS)
                else:
                    row[a] = fixed_s(c, int(a.split("_")[1]), SEEDS)
            out[sc] = row
            print(st, sc, {k: round(v, 2) for k, v in row.items()}, flush=True)
    json.dump({"calibration": CALIB, "seeds": SEEDS, "predictions_s": out}, open(f"{BASE}/predictions_CD.json", "w"),
              indent=1)


if __name__ == "__main__":
    main()
