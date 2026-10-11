#!/usr/bin/env python3
"""Track 2-B live fixed-grid gate analysis: live optima per state and the
gate verdict (both N* and S* must move with material separation)."""
import glob
import json
import os
import statistics

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
MIB = 1024 ** 2
N_GRID = [1, 2, 4, 8, 16]
S_GRID = [262144, 1048576, 4194304, 16777216, 33554432]


def load():
    cells = {}
    invalid = []
    for p in sorted(glob.glob(os.path.join(BASE, "fixed_grid", "gate_*.json"))):
        rid = os.path.basename(p)[:-5]
        j = json.load(open(p))
        mp = os.path.join(BASE, "logs", rid + ".meta.json")
        m = json.load(open(mp)) if os.path.exists(mp) else {}
        key = (m.get("state"), m.get("N"), m.get("S"))
        rec = {"run_id": rid, "completion_ms": j.get("completion_ms"),
               "valid": m.get("valid", False) and not j.get("err")
               and j.get("content_verified")}
        if rec["valid"]:
            cells.setdefault(key, []).append(rec["completion_ms"])
        else:
            invalid.append(rid)
    return cells, invalid


def main():
    cells, invalid = load()
    out = {"cells": {}, "optima": {}, "gate": {}, "invalid": invalid}
    for state in ("healthy", "capped"):
        med = {}
        for n in N_GRID:
            for s in S_GRID:
                v = cells.get((state, n, s), [])
                if v:
                    med["%d/%d" % (n, s)] = statistics.median(v)
        out["cells"][state] = med
        if not med:
            continue
        (bk, bt) = min(med.items(), key=lambda kv: kv[1])
        n_star, s_star = [int(x) for x in bk.split("/")]
        other_n = [v for k, v in med.items()
                   if int(k.split("/")[1]) == s_star and int(k.split("/")[0]) != n_star]
        other_s = [v for k, v in med.items()
                   if int(k.split("/")[0]) == n_star and int(k.split("/")[1]) != s_star]
        out["optima"][state] = {
            "N": n_star, "S": s_star, "completion_s": bt / 1000.0,
            "N_margin": (min(other_n) - bt) / min(other_n) if other_n else None,
            "S_margin": (min(other_s) - bt) / min(other_s) if other_s else None,
            "n_reps": {"%d/%d" % (k[1], k[2]): len(v) for k, v in cells.items()
                       if k[0] == state and k[1] == n_star and k[2] == s_star},
        }
    if "healthy" in out["optima"] and "capped" in out["optima"]:
        h, c = out["optima"]["healthy"], out["optima"]["capped"]
        pen_cap = (out["cells"]["capped"].get("%d/%d" % (h["N"], h["S"]), float("inf"))
                   - c["completion_s"] * 1000) / (c["completion_s"] * 1000)
        pen_healthy = (out["cells"]["healthy"].get("%d/%d" % (c["N"], c["S"]), float("inf"))
                       - h["completion_s"] * 1000) / (h["completion_s"] * 1000)
        checks = {
            "N_moves": h["N"] != c["N"],
            "S_moves": h["S"] != c["S"],
            "N_margin_ge_5pct": (c["N_margin"] or 0) >= 0.05,
            "S_margin_ge_5pct": (c["S_margin"] or 0) >= 0.05,
            "material_cap_side": pen_cap >= 0.10,
            "material_healthy_side": pen_healthy >= 0.10,
        }
        out["gate"] = {"checks": checks, "pass": all(checks.values()),
                       "penalty_capped_uses_healthy_opt": pen_cap,
                       "penalty_healthy_uses_capped_opt": pen_healthy}
    json.dump(out, open(os.path.join(BASE, "analysis", "gate_b.json"), "w"), indent=2)
    for state in ("healthy", "capped"):
        if state in out["optima"]:
            o = out["optima"][state]
            print("%-8s optimum (N=%d,S=%dM) %.1fs  Nmar=%s Smar=%s" % (
                state, o["N"], o["S"] // MIB, o["completion_s"],
                "n/a" if o["N_margin"] is None else "%+.1f%%" % (100 * o["N_margin"]),
                "n/a" if o["S_margin"] is None else "%+.1f%%" % (100 * o["S_margin"])))
    print("gate:", json.dumps(out["gate"], indent=1))
    print("invalid runs:", len(invalid))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
