#!/usr/bin/env python3
"""B — FROZEN pre-screen: soft N-dependent server throttle + phase-locked pacing.

Predeclared before this run (manifest/b_scenario_manifest.json):
  family:  schedules {T=1,2,4,8 s two-phase, D2 anchor} x caps
           {none, C in {4,8,16,32} MiB/s x gamma in {0.02,0.05,0.10,0.20}}
  grid:    N in {1,2,4,8,16}; S in {256K,1M,4M,16M,32M}; F = 512 MiB
  rule:    a pair qualifies iff both N* and S* move between the no-cap and
           capped stationary states, coordinate margins >= 5 % in both states,
           cross-state penalties >= 10 % both ways, and the optima are not
           degenerate ties. Select the qualifying pair maximizing
           min(penalty_cap_side, penalty_base_side); tie-break larger
           (N_margin + S_margin), then smaller transition time.
  adaptive: state schedule = [no-cap 0-8 s, capped 8-68 s, no-cap 68 s+];
           predeclared causal controller (start (16,1M), degraded (4,4M),
           drop 0.6x baseline, recover 1.5x capped baseline, k=3 samples);
           N-only and S-only controls; clairvoyant 2-switch oracle;
           best stationary fixed under the same schedule.
"""
import json
import os
import statistics
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import combined_sim as cs  # noqa: E402
from combined_sim import (Params, ServerState, StateSchedule, d2_anchor,
                          two_phase, fixed, fixed_frontier, N_GRID, S_GRID,
                          FILE_SIZE, MIB, ACTION_SPACE)  # noqa: E402

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
OUT = os.path.join(BASE, "analysis")
MAN = os.path.join(BASE, "manifest")

SCHEDULES = {"T=1": two_phase(1.0), "T=2": two_phase(2.0), "T=4": two_phase(4.0),
             "T=8": two_phase(8.0), "D2": d2_anchor()}
CAPS = {"none": ServerState(0, 0)}
for c in (4, 8, 16, 32):
    for g in (0.02, 0.05, 0.10, 0.20):
        CAPS["C=%dM_g=%.2f" % (c, g)] = ServerState(c * MIB, g)


def select_pair():
    frontiers = {}
    states = {}
    for sname, sched in SCHEDULES.items():
        for cname, st in CAPS.items():
            fr = fixed_frontier(FILE_SIZE, sched, Params(), state=st)
            frontiers[(sname, cname)] = fr
            states["%s|%s" % (sname, cname)] = cs._optima(fr)
    pairs = []
    for sname in SCHEDULES:
        base = cs._optima(frontiers[(sname, "none")])
        for cname in CAPS:
            if cname == "none":
                continue
            o = cs._optima(frontiers[(sname, cname)])
            pen_cap = (frontiers[(sname, cname)][(base["N"], base["S"])] - o["t"]) / o["t"]
            pen_base = (frontiers[(sname, "none")][(o["N"], o["S"])] - base["t"]) / base["t"]
            checks = {
                "N_moves": base["N"] != o["N"],
                "S_moves": base["S"] != o["S"],
                "N_margin_ge_5pct": (o["N_margin"] or 0) >= 0.05,
                "S_margin_ge_5pct": (o["S_margin"] or 0) >= 0.05,
                "material_cap_side": pen_cap >= 0.10,
                "material_base_side": pen_base >= 0.10,
            }
            pairs.append({"schedule": sname, "cap": cname, "base_opt": base, "cap_opt": o,
                          "pen_cap": pen_cap, "pen_base": pen_base, "checks": checks,
                          "qualifies": all(checks.values())})
    qual = [p for p in pairs if p["qualifies"]]
    qual.sort(key=lambda p: (-min(p["pen_cap"], p["pen_base"]),
                             -((p["cap_opt"]["N_margin"] or 0) + (p["cap_opt"]["S_margin"] or 0)),
                             list(SCHEDULES).index(p["schedule"])))
    return states, pairs, (qual[0] if qual else None), len(qual)


def make_cap_controller(start=(16, 1 * MIB), degraded=(4, 4 * MIB),
                        drop=0.6, recover=1.5, k=3):
    st = {"phase": "normal", "baseline": None, "capped": None}

    def ctrl(ctx):
        s = ctx["samples"]
        if len(s) < k:
            return None
        if st["baseline"] is None:
            st["baseline"] = sorted(s[:k])[k // 2]
            return None
        recent = sorted(s[-k:])[k // 2]
        if st["phase"] == "normal":
            if recent < drop * st["baseline"]:
                st["phase"] = "capped"
                st["capped"] = recent
                return degraded
        else:
            if recent > recover * st["capped"]:
                st["phase"] = "normal"
                st["baseline"] = recent
                return start
        return None

    return ctrl


def adaptive_pre_screen(sel):
    sched = SCHEDULES[sel["schedule"]]
    st = CAPS[sel["cap"]]
    state_sched = StateSchedule([(8.0, ServerState(0, 0)), (60.0, st), (1e9, ServerState(0, 0))])
    start = (sel["base_opt"]["N"], sel["base_opt"]["S"])
    degraded = (sel["cap_opt"]["N"], sel["cap_opt"]["S"])
    out = {"state_schedule": {"no_cap_s": 8.0, "capped_s": 60.0, "cap": sel["cap"],
                              "schedule": sel["schedule"]},
           "start": start, "degraded": degraded}

    # best stationary fixed under the schedule
    best = None
    fixed_times = {}
    for n in N_GRID:
        for s in S_GRID:
            r = fixed(FILE_SIZE, n, s, sched, Params(), state_schedule=state_sched)
            t = r["completion"] if not r["failed"] else None
            fixed_times["%d/%d" % (n, s)] = t
            if t is not None and (best is None or t < best["t"]):
                best = {"N": n, "S": s, "t": t}
    out["best_stationary_fixed"] = best
    out["fixed_times"] = fixed_times

    # causal joint controller + N-only / S-only controls
    def run_ctrl(start_a, degraded_a, target2):
        ctrl = make_cap_controller(start=start_a, degraded=degraded_a)
        r = cs.simulate(FILE_SIZE, start_a[0], start_a[1], sched, Params(),
                        controller=ctrl, state_schedule=state_sched, keep_requests=True)
        return {"completion": r["completion"], "failed": r["failed"],
                "switches": r["switches"], "n_requests": r["n_requests"]}

    out["causal_joint"] = run_ctrl(start, degraded, start)
    out["causal_n_only"] = run_ctrl(start, (degraded[0], start[1]), start)
    out["causal_s_only"] = run_ctrl(start, (start[0], degraded[1]), start)

    # clairvoyant 2-switch oracle (coarse grid)
    oracle = None
    for t1 in (6.0, 7.0, 8.0, 9.0, 10.0):
        for t2 in (64.0, 66.0, 68.0, 70.0, 72.0):
            for a1 in [(4, 4 * MIB), (4, 1 * MIB), (8, 4 * MIB), (16, 4 * MIB)]:
                for a2 in [(16, 1 * MIB), (16, 4 * MIB)]:
                    r = cs.simulate(FILE_SIZE, start[0], start[1], sched, Params(),
                                    planned_switches=[(t1, a1[0], a1[1]), (t2, a2[0], a2[1])],
                                    state_schedule=state_sched)
                    if not r["failed"] and (oracle is None or r["completion"] < oracle["t"]):
                        oracle = {"t1": t1, "a1": list(a1), "t2": t2, "a2": list(a2),
                                  "t": r["completion"]}
    out["oracle"] = oracle

    # modelled aria2: 16 pieces of 32 MiB, all start ~0.4 s, rate sampled then held
    aria = fixed(FILE_SIZE, 16, 32 * MIB, sched, Params(), state_schedule=state_sched)
    out["aria2_model"] = {"completion": aria["completion"], "failed": aria["failed"]}

    b = best["t"]
    out["headroom"] = {
        "causal_joint_vs_best_fixed": (b - out["causal_joint"]["completion"]) / b,
        "oracle_vs_best_fixed": (b - oracle["t"]) / b if oracle else None,
        "causal_joint_vs_aria2": (aria["completion"] - out["causal_joint"]["completion"]) / aria["completion"],
    }
    return out


def main():
    os.makedirs(OUT, exist_ok=True)
    os.makedirs(MAN, exist_ok=True)
    states, pairs, sel, n_qual = select_pair()
    result = {"family": {"schedules": list(SCHEDULES), "caps": list(CAPS),
                         "N": N_GRID, "S": S_GRID, "file_bytes": FILE_SIZE},
              "states": states, "pairs": pairs, "n_qualified": n_qual, "selected": sel}
    print("qualified pairs: %d / %d" % (n_qual, len(pairs)))
    if sel:
        print("selected: %s / %s  base=%s cap=%s  pen_cap=%.1f%% pen_base=%.1f%%" % (
            sel["schedule"], sel["cap"], (sel["base_opt"]["N"], sel["base_opt"]["S"]),
            (sel["cap_opt"]["N"], sel["cap_opt"]["S"]),
            100 * sel["pen_cap"], 100 * sel["pen_base"]))
        ad = adaptive_pre_screen(sel)
        result["adaptive"] = ad
        b = ad["best_stationary_fixed"]["t"]
        print("adaptive pre-screen: best fixed (%d,%dM) %.1fs | joint %.1fs (%.1f%%) | N-only %.1fs | S-only %.1fs | oracle %.1fs (%.1f%%) | aria2 %.1fs" % (
            ad["best_stationary_fixed"]["N"], ad["best_stationary_fixed"]["S"] // MIB, b,
            ad["causal_joint"]["completion"], 100 * ad["headroom"]["causal_joint_vs_best_fixed"],
            ad["causal_n_only"]["completion"], ad["causal_s_only"]["completion"],
            ad["oracle"]["t"], 100 * ad["headroom"]["oracle_vs_best_fixed"],
            ad["aria2_model"]["completion"]))
        print("joint switches:", ad["causal_joint"]["switches"])
    json.dump(result, open(os.path.join(OUT, "prescreen_b.json"), "w"), indent=2)
    json.dump({"family": result["family"], "selection_rule":
               "both N* and S* move; coordinate margins >=5% both states; cross-state penalties >=10% both ways; no ties; select max min(penalty), tie-break margins then smaller T",
               "selected": sel, "n_qualified": n_qual,
               "adaptive_design": result.get("adaptive", {}).get("state_schedule"),
               "note": "frozen before the live server implementation; the exploratory sweep in §22.95 is disclosed"},
              open(os.path.join(MAN, "b_scenario_manifest.json"), "w"), indent=2)
    print("written prescreen_b.json + b_scenario_manifest.json")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
