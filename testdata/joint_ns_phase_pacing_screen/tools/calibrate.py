#!/usr/bin/env python3
"""Calibrate the joint N+S simulator against the measured D2 benchmark frontier.

Observed data (immutable evidence):
  testdata/aria2_competitive_benchmark/discovery_ours/of-c{C}-s{S}.json
  (40 fixed-policy configurations, one run each, exact D2 schedule, netem 40ms
  + 0.4%), plus the measured aria2 32 MiB-piece anchor.

The calibration is a deterministic grid search over the four physical
parameters (h, c, init, down); no RNG. Output: analysis/calibration.json.
"""
import glob
import json
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import jns_sim as sim  # noqa: E402

BASE = "/workspaces/mvp/testdata/aria2_competitive_benchmark"
OUT = "/workspaces/mvp/testdata/joint_ns_phase_pacing_screen/analysis"


def load_observed():
    obs = {}
    for p in glob.glob(os.path.join(BASE, "discovery_ours", "of-*.json")):
        name = os.path.basename(p)
        if re.search(r"-r\d+\.json$", name):
            continue
        j = json.load(open(p))
        cfg = j["config"]
        key = (cfg["concurrency"], cfg["chunk_bytes"])
        obs[key] = j["completion_ms"] / 1000.0
    return obs


def load_aria2_anchor():
    j = json.load(open(os.path.join(BASE, "final_reproduction", "pairs.json")))
    vals = [p["b"]["completion_ms"] / 1000.0 for p in j["pairs"]]
    reqs = [r for r in j["pairs"][0]["b"]["server_requests"] if r["has_range"]]
    return {"median_s": sorted(vals)[len(vals) // 2], "n_pieces": len(reqs),
            "piece_bytes": 33554432, "start_s": reqs[0]["start_ms"] / 1000.0}


def fit(obs):
    best = None
    fit_keys = [k for k in obs if k[1] <= 16 * sim.MIB]
    for h in [0.06, 0.07, 0.08, 0.088, 0.095, 0.11]:
        for c in [0.0002, 0.0004, 0.0006, 0.0008, 0.0010]:
            for init in [0.09, 0.121, 0.15]:
                for down in [0.03, 0.045, 0.06]:
                    for rg in [0.10, 0.135, 0.18]:
                        params = sim.Params(h=h, c=c, init=init, down=down, retry_gap=rg)
                        sched = sim.d2_anchor()
                        err = 0.0
                        preds = {}
                        for (N, S), o in obs.items():
                            p = sim.fixed(sim.FILE_SIZE, N, S, sched, params)["completion"]
                            preds[(N, S)] = p
                        for k in fit_keys:
                            err += ((preds[k] - obs[k]) / obs[k]) ** 2
                        rmse = (err / len(fit_keys)) ** 0.5
                        if best is None or rmse < best["rmse"]:
                            best = {"h": h, "c": c, "init": init, "down": down,
                                    "retry_gap": rg, "rmse": rmse, "preds": preds}
    return best


def main():
    os.makedirs(OUT, exist_ok=True)
    obs = load_observed()
    anchor = load_aria2_anchor()
    best = fit(obs)
    params = sim.Params(h=best["h"], c=best["c"], init=best["init"], down=best["down"],
                        retry_gap=best["retry_gap"])

    rows = []
    for (N, S), o in sorted(obs.items()):
        p = best["preds"][(N, S)]
        rows.append({"N": N, "S": S, "observed_s": o, "predicted_s": p,
                     "rel_err": (p - o) / o})
    rmse = best["rmse"]
    maxrel = max(abs(r["rel_err"]) for r in rows)

    # aria2 anchor prediction: n_pieces parallel requests, slow rate held
    n = anchor["n_pieces"]
    L = anchor["piece_bytes"]
    rate = sim.D2_INITIAL
    dur = sim._duration(L, rate, params)
    aria2_pred = anchor["start_s"] + dur + params.down

    out = {
        "params": {"h": best["h"], "c": best["c"], "init": best["init"], "down": best["down"],
                   "retry_gap": best["retry_gap"]},
        "fit": {"rmse_rel": rmse, "max_abs_rel": maxrel, "n_points": len(rows),
                "fit_points": "S <= 16 MiB (the 32 MiB configs hit the 30 s timeout + retry)"},
        "rows": rows,
        "aria2_anchor": {"observed_median_s": anchor["median_s"],
                         "predicted_s": aria2_pred, "rel_err": (aria2_pred - anchor["median_s"]) / anchor["median_s"]},
        "notes": [
            "Model: per-response rate sampled at server start and held; duration = L/rate + c*ceil(L/16KiB);",
            "worker inter-request gap h; first request server-start init; client completion = last server end + down.",
            "Calibrated on the 40 measured D2 fixed-policy configurations; aria2 anchor is a held-out check.",
        ],
    }
    json.dump(out, open(os.path.join(OUT, "calibration.json"), "w"), indent=2)
    print("best params: h=%.3f c=%.4f init=%.3f down=%.3f retry_gap=%.3f" % (
        best["h"], best["c"], best["init"], best["down"], best["retry_gap"]))
    print("fit: rmse_rel=%.4f max_abs_rel=%.4f over %d points" % (rmse, maxrel, len(rows)))
    print("aria2 anchor: observed median %.2fs, predicted %.2fs (rel %.3f)" % (
        anchor["median_s"], aria2_pred, (aria2_pred - anchor["median_s"]) / anchor["median_s"]))
    worst = sorted(rows, key=lambda r: -abs(r["rel_err"]))[:6]
    for r in worst:
        print("  worst: N=%-2d S=%-8d obs=%7.2f pred=%7.2f rel=%+.3f" % (
            r["N"], r["S"], r["observed_s"], r["predicted_s"], r["rel_err"]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
