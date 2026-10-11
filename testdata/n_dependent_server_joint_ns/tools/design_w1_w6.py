#!/usr/bin/env python3
"""Phase 3 design: analytical W1-W6 family + frozen live-protocol manifest.

PREDECLARED BEFORE any new live workload runs.  This script sees only the
calibrated model (combined_sim, Params h=0.088, timeout 30 s, 3 attempts);
it never reads live W3-W6 outcomes.

Candidate family (72 degraded candidates + W1):
  T in {1,2,4} s (two-phase schedule transition),
  C in {4,8,12,16} MiB/s, gamma in {0.05,0.10} (N-dependent cap),
  t in {4,8,12} s (cap activation; state schedule [no cap 0..t, cap t..)).
W1 = healthy workload: schedule T=1, no cap.

Per candidate:
  A = optimum of the T-matched pure no-cap frontier.
  B = optimum of the candidate's state schedule.
  cap_gain = (t_sched(A) - t_sched(B)) / t_sched(B)   (A-cell aborted => inf)
  base_pen = (t_nocap(B) - t_nocap(A)) / t_nocap(A)
  opt_10pct = (t_sched(A) - t_sched(B)) / t_sched(A) >= 0.10

Qualification (all required; fixed before evaluation):
  N_moves, S_moves (B vs A); B reachable (no abort);
  cap_gain >= 0.20; opt_10pct;
  detectable: predicted post-transition probe level < 0.40 x predicted peak
  probe level (predeclared selector identifiability precondition; the live
  band detector fires at 0.50 x peak, so the check leaves noise margin).

Selection (fixed before evaluation): exhaustively choose 5 qualified
candidates maximizing sum(min(cap_gain, base_pen)) subject to transition
coverage {4,8,12} s and >= 2 distinct caps; tie-break lexicographic
(T, C, gamma, t).  If full coverage is infeasible, relax to >= 2 distinct
transition times, then to unconstrained, documenting the relaxation.
"""
import hashlib
import itertools
import json
import os
import statistics
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from combined_sim import (Params, ServerState, StateSchedule, two_phase,  # noqa: E402
                          fixed, N_GRID, S_GRID, FILE_SIZE, MIB, KIB,
                          FAMILY_SLOW, FAMILY_FAST)

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
OUT = os.path.join(BASE, "analysis")
MAN = os.path.join(BASE, "manifest")
PARAMS = Params()

TS = (1, 2, 4)
CAPS = (4, 8, 12, 16)
GAMMAS = (0.05, 0.10)
TRANS = (4, 8, 12)
LEVEL_FRACTION = 0.40
CAP_GAIN_MIN = 0.20
OPT_10PCT_MIN = 0.10

NOCAP = {}


def frontier(schedule, state_schedule=None):
    fr, failed = {}, {}
    for N in N_GRID:
        for S in S_GRID:
            r = fixed(FILE_SIZE, N, S, schedule, PARAMS,
                      state_schedule=state_schedule)
            fr[(N, S)] = r["completion"] if not r["failed"] else None
            failed[(N, S)] = bool(r["failed"])
    return fr, failed


def optima(fr):
    cells = {k: v for k, v in fr.items() if v is not None}
    (N, S), t = min(cells.items(), key=lambda kv: kv[1])
    other_n = [v for (n, ss), v in cells.items() if n != N and ss == S]
    other_s = [v for (n, ss), v in cells.items() if ss != S and n == N]
    return {"N": N, "S": S, "t": t,
            "N_margin": (min(other_n) - t) / min(other_n) if other_n else None,
            "S_margin": (min(other_s) - t) / min(other_s) if other_s else None}


def no_cap_frontier(T):
    if T not in NOCAP:
        NOCAP[T] = frontier(two_phase(float(T)))
    return NOCAP[T]


def predicted_level(C, gamma, k=16.0):
    return C * MIB / (k * (1.0 + gamma * (k - 1.0)))


def predicted_peak(T, t):
    return FAMILY_FAST if t > T else FAMILY_SLOW


def candidate(T, C, gamma, t):
    sched = two_phase(float(T))
    st = ServerState(C * MIB, gamma)
    ss = StateSchedule([(float(t), ServerState(0, 0)), (1e9, st)])
    fr_st, failed_st = frontier(sched, state_schedule=ss)
    B = optima(fr_st)
    fr_nc, _ = no_cap_frontier(T)
    A = optima(fr_nc)
    a_cell = fr_st[(A["N"], A["S"])]
    cap_gain = ((a_cell - B["t"]) / B["t"]) if a_cell is not None else 9.99
    opt_10 = ((a_cell - B["t"]) / a_cell) if a_cell is not None else 9.99
    base_cell = fr_nc[(B["N"], B["S"])]
    base_pen = ((base_cell - A["t"]) / A["t"]) if base_cell is not None else 9.99
    lvl = predicted_level(C, gamma)
    peak = predicted_peak(T, t)
    checks = {
        "N_moves": A["N"] != B["N"],
        "S_moves": A["S"] != B["S"],
        "reachable": not failed_st[(B["N"], B["S"])],
        "cap_gain_ge_20pct": cap_gain >= CAP_GAIN_MIN,
        "opt_10pct": opt_10 >= OPT_10PCT_MIN,
        "detectable": lvl < LEVEL_FRACTION * peak,
    }
    return {"T": T, "C": C, "gamma": gamma, "t": t,
            "A": A, "B": B, "cap_gain": cap_gain, "base_pen": base_pen,
            "opt_10pct": opt_10, "probe_level": lvl, "probe_peak": peak,
            "checks": checks, "qualifies": all(checks.values())}


def select(qualified):
    if len(qualified) < 5:
        return sorted(qualified, key=lambda c: (c["T"], c["C"], c["gamma"], c["t"])), \
            "fewer than 5 candidates qualified; all qualified workloads selected"
    def try_select(coverage, min_caps, min_trans):
        best = None
        for combo in itertools.combinations(qualified, 5):
            if coverage is not None and {c["t"] for c in combo} != set(coverage):
                continue
            if len({c["t"] for c in combo}) < min_trans:
                continue
            if len({c["C"] for c in combo}) < min_caps:
                continue
            score = sum(min(c["cap_gain"], c["base_pen"]) for c in combo)
            key = (round(-score, 9),
                   sorted((c["T"], c["C"], c["gamma"], c["t"]) for c in combo))
            if best is None or key < best[0]:
                best = (key, combo)
        return [c for c in best[1]] if best else None
    for coverage, min_caps, min_trans, note in (
            (TRANS, 2, 3, "full transition coverage"),
            (None, 2, 2, "relaxed: >= 2 distinct transitions, >= 2 distinct caps"),
            (None, 1, 2, "relaxed: >= 2 distinct transitions only")):
        sel = try_select(coverage, min_caps, min_trans)
        if sel:
            return sorted(sel, key=lambda c: (c["T"], c["C"], c["gamma"], c["t"])), note
    return [], "no 5-subset satisfied any coverage rule; none selected"


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def main():
    os.makedirs(OUT, exist_ok=True)
    os.makedirs(MAN, exist_ok=True)
    print("evaluating candidates (T x C x gamma x t)...", flush=True)
    cands = []
    for T in TS:
        for C in CAPS:
            for g in GAMMAS:
                for t in TRANS:
                    c = candidate(T, C, g, t)
                    cands.append(c)
                    print("  T=%d C=%-2d g=%.2f t=%-2d  A=(%d,%d) B=(%d,%d) cap_gain=%5.1f%% base_pen=%6.1f%% level=%6.0fK peak=%dK q=%s" % (
                        T, C, g, t, c["A"]["N"], c["A"]["S"] // MIB,
                        c["B"]["N"], c["B"]["S"] // MIB,
                        100 * min(c["cap_gain"], 9.99), 100 * min(c["base_pen"], 9.99),
                        c["probe_level"] / 1024, c["probe_peak"] / 1024,
                        c["qualifies"]), flush=True)
    qual = [c for c in cands if c["qualifies"]]
    print("qualified: %d / %d" % (len(qual), len(cands)), flush=True)
    selected, note = select(qual)

    workloads = {}
    base_cells = sorted({(4, 1 * MIB), (4, 4 * MIB), (4, 16 * MIB),
                         (8, 1 * MIB), (8, 4 * MIB), (8, 16 * MIB),
                         (16, 1 * MIB), (16, 4 * MIB), (16, 16 * MIB)})
    w1_opt = optima(no_cap_frontier(1)[0])
    workloads["W1"] = {
        "label": "healthy",
        "schedule": "t1", "schedule_T_s": 1, "cap_c": 0, "cap_gamma": 0.0,
        "cap_at_s": None,
        "optimum_B": {"N": w1_opt["N"], "S": w1_opt["S"], "t": w1_opt["t"],
                      "N_margin": w1_opt["N_margin"], "S_margin": w1_opt["S_margin"]},
        "live_cells": [[n, s] for (n, s) in base_cells],
        "live_reps": 2,
    }
    for i, c in enumerate(selected, start=2):
        cells = list(base_cells)
        if (c["B"]["N"], c["B"]["S"]) not in cells:
            cells.append((c["B"]["N"], c["B"]["S"]))
        cells = sorted(set(cells))
        workloads["W%d" % i] = {
            "label": "T=%d,C=%dM,g=%.2f,t=%d" % (c["T"], c["C"], c["gamma"], c["t"]),
            "schedule": "t%d" % c["T"], "schedule_T_s": c["T"],
            "cap_c": c["C"] * MIB, "cap_gamma": c["gamma"], "cap_at_s": c["t"],
            "A_nocap_optimum": {"N": c["A"]["N"], "S": c["A"]["S"], "t": c["A"]["t"]},
            "optimum_B": {"N": c["B"]["N"], "S": c["B"]["S"], "t": c["B"]["t"],
                          "N_margin": c["B"]["N_margin"], "S_margin": c["B"]["S_margin"]},
            "cap_gain": c["cap_gain"], "base_pen": c["base_pen"],
            "opt_10pct": c["opt_10pct"],
            "probe_level": c["probe_level"], "probe_peak": c["probe_peak"],
            "live_cells": [[n, s] for (n, s) in cells],
            "live_reps": 2,
        }
    levels = [int(c["probe_level"]) for c in selected]
    targets = ["%d:%d" % (c["B"]["N"], c["B"]["S"]) for c in selected]
    selector = {
        "mode": "band",
        "probe_config": "(16, 1 MiB)",
        "drop_ratio": 0.5, "window": 3,
        "classify_window_s": 25, "min_post_drop_samples": 1,
        "deadline_s": 60,
        "levels_bytes_s": levels,
        "targets": targets,
        "preregistered_note": ("level list is the model-predicted post-transition probe "
                               "throughput C/(k*(1+gamma*(k-1))) at k=16; nearest level wins; "
                               "no drop by deadline => healthy (16, 1 MiB)"),
        "pre_live_review": ("classification window fixed at 25 s with >= 1 post-drop sample "
                            "before any W1-W6 live run: a 20-sample window would take up to "
                            "~200 s under the strongest frozen cap (request durations ~10 s), "
                            "exceeding the download duration; time-based window avoids that"),
    }
    manifest = {
        "frozen_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "design_script": "tools/design_w1_w6.py",
        "ns_bench_sha256": sha256_file(os.path.join(BASE, "tools", "ns_bench")),
        "model": {"sim": "tools/combined_sim.py", "params": vars(PARAMS),
                  "file_bytes": FILE_SIZE, "n_grid": N_GRID, "s_grid": S_GRID},
        "candidate_family": {"T": list(TS), "C_mib": list(CAPS), "gamma": list(GAMMAS),
                             "t_s": list(TRANS), "n_candidates": len(cands)},
        "qualification": {
            "checks": ["N_moves", "S_moves", "reachable", "cap_gain>=0.20",
                       "opt_10pct>=0.10",
                       "detectable: probe_level < 0.40 * probe_peak"],
            "cap_gain_min": CAP_GAIN_MIN, "opt_10pct_min": OPT_10PCT_MIN,
            "level_fraction": LEVEL_FRACTION,
            "note": ("detectability is a predeclared selector-identifiability "
                     "precondition; the live band detector fires at 0.50 x peak"),
        },
        "selection": {"rule": ("maximize sum(min(cap_gain, base_pen)) over 5-subsets "
                               "with transition coverage and >=2 distinct caps; "
                               "lexicographic tie-break"),
                      "constraint_note": note,
                      "n_qualified": len(qual),
                      "selected_candidates": [{"T": c["T"], "C": c["C"],
                                               "gamma": c["gamma"], "t": c["t"]}
                                              for c in selected]},
        "candidates": [{"T": c["T"], "C": c["C"], "gamma": c["gamma"], "t": c["t"],
                        "A": c["A"], "B": c["B"], "cap_gain": c["cap_gain"],
                        "base_pen": c["base_pen"], "opt_10pct": c["opt_10pct"],
                        "probe_level": c["probe_level"], "probe_peak": c["probe_peak"],
                        "checks": c["checks"], "qualifies": c["qualifies"]}
                       for c in cands],
        "workloads": workloads,
        "selector": selector,
        "live_protocol": {
            "netem": "40ms + 0.4% loss on lo",
            "timeout_s": 30, "retries": 3, "file_bytes": FILE_SIZE,
            "fixed_grid": {"base_cells": [[n, s] for (n, s) in base_cells],
                           "extra_cell": "analytical optimum B per workload",
                           "reps": 2},
            "adaptive": {
                "modes": ["band_selector", "per_workload_best_fixed",
                          "family_best_fixed", "aria2"],
                "blocks": 10,
                "baseline_definition": ("per_workload_best_fixed = live fixed-grid argmin per "
                                        "workload; family_best_fixed = live fixed-grid argmin "
                                        "minimizing the mean over the six workloads"),
                "analysis": ("paired per-block ratios vs each baseline; geometric mean; "
                             "bootstrap 95% CI; win counts; invalid runs preserved and "
                             "reported (selector aborts count as losses)"),
            },
            "exclusion": "invalid/aborted runs preserved, reported, never dropped silently",
        },
    }
    json.dump(manifest, open(os.path.join(MAN, "w1_w6_manifest.json"), "w"), indent=2)
    json.dump({"manifest": "manifest/w1_w6_manifest.json",
               "n_qualified": len(qual), "selection_note": note,
               "selected": manifest["selection"]["selected_candidates"]},
              open(os.path.join(OUT, "w1_w6_design.json"), "w"), indent=2)
    print("selected workloads: %s" % json.dumps(
        manifest["selection"]["selected_candidates"]), flush=True)
    print("selector levels: %s targets: %s" % (levels, targets), flush=True)
    print("written manifest/w1_w6_manifest.json + analysis/w1_w6_design.json", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
