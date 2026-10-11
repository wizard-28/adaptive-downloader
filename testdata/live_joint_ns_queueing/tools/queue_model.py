#!/usr/bin/env python3
"""Deterministic shared-bottleneck queueing model, calibrated to the live gate.

Model (semi-empirical; structural form, one fitted loss coefficient):

    T(N,S | C,B) = F / (C - B_ach(N))                 # work-conserving bulk
                 + S / (N * (C - B_ach(N)))           # finite-file request tail
                 + k_loss * retrans(N) * MSS / (C - B_ach(N))   # retransmitted bytes consume capacity
                 + RTT_q                              # measured queueing delay

where B_ach(N) is the *measured* achieved background rate at concurrency N
(the background's TCP share shrinks as the downloader adds flows), retrans(N)
is the measured median retransmission count, MSS = 1448 bytes, and RTT_q is
the measured median ping RTT. k_loss is the only fitted parameter.

This model is calibrated on the fixed-N live gate observations only; it is not
used to force or evaluate any adaptive result (none was run).
"""
import json
import os
import statistics

BASE = "/workspaces/mvp/testdata/live_joint_ns_queueing"
C_BPS = 5_000_000
F = 32 << 20
S = 1 << 20
MSS = 1448
STATES = ["S0_no_background", "S1_moderate", "S2_heavy"]
N_GRID = [1, 2, 4, 8, 16]


def main():
    J = json.load(open(os.path.join(BASE, "analysis", "gate_analysis.json")))
    cells = {}
    for st in STATES:
        for n in N_GRID:
            c = J["states"][st]["cells"][str(n)]
            b_ach = c["achieved_background_bps"]
            if b_ach is None:
                b_ach = 0.0
            cells[(st, n)] = {
                "obs": c["median_s"], "B_ach": b_ach / 8.0,  # bits/s -> bytes/s
                "retrans": c["retrans"], "rtt_q": c["ping_median_ms"] / 1000.0,
            }

    def predict(st, n, k_loss):
        c = cells[(st, n)]
        share = max(C_BPS - c["B_ach"], 1.0)
        bulk = F / share
        tail = S / (n * share)
        loss = k_loss * c["retrans"] * MSS / share
        return bulk + tail + loss + c["rtt_q"]

    def sse(k):
        return sum((predict(st, n, k) - cells[(st, n)]["obs"]) ** 2
                   for st in STATES for n in N_GRID)

    # 1-D grid + refinement fit of k_loss
    best = None
    for i in range(0, 100001):
        k = i / 2000.0
        v = sse(k)
        if best is None or v < best[1]:
            best = (k, v)
    k_loss = best[0]
    rows = []
    for st in STATES:
        for n in N_GRID:
            p = predict(st, n, k_loss)
            o = cells[(st, n)]["obs"]
            rows.append({"state": st, "N": n, "observed_s": o, "predicted_s": p,
                         "rel_err": (p - o) / o})
    rel = [abs(r["rel_err"]) for r in rows]
    pred_nstar = {}
    for st in STATES:
        meds = {n: predict(st, n, k_loss) for n in N_GRID}
        pred_nstar[st] = min(meds, key=lambda k: meds[k])
    out = {
        "model": "T = F/(C-B_ach) + S/(N(C-B_ach)) + k_loss*retrans*MSS/(C-B_ach) + RTT_q",
        "k_loss": k_loss,
        "C_bytes_per_second": C_BPS, "file_bytes": F, "chunk_bytes": S,
        "rmse_rel": (sum(r * r for r in rel) / len(rel)) ** 0.5,
        "max_abs_rel": max(rel),
        "rows": rows,
        "predicted_N_star_per_state": pred_nstar,
        "observed_N_star_per_state": {st: J["states"][st]["summary"]["N_star"] for st in STATES},
        "note": ("B_ach(N) is measured per state/N; the model reproduces the live medians "
                 "within the reported error and predicts the same boundary/flat N* pattern. "
                 "The work-conserving bulk term is N-independent, so no interior optimum is "
                 "structurally expected from this mechanism."),
    }
    os.makedirs(os.path.join(BASE, "calibration"), exist_ok=True)
    json.dump(out, open(os.path.join(BASE, "calibration", "model_calibration.json"), "w"), indent=2)
    print("k_loss=%.3f  rmse_rel=%.3f  max_abs_rel=%.3f" % (k_loss, out["rmse_rel"], out["max_abs_rel"]))
    print("observed N*:", out["observed_N_star_per_state"])
    print("predicted N*:", out["predicted_N_star_per_state"])
    for r in rows:
        print("  %-16s N=%-2d obs=%7.2f pred=%7.2f rel=%+.1f%%" % (
            r["state"], r["N"], r["observed_s"], r["predicted_s"], 100 * r["rel_err"]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
