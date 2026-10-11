#!/usr/bin/env python3
"""Faithful Python replica of the frozen Go oracle (cmd/oracle + tau/oracle.go).

Validated against /home/vscode/oracle_stage_v3 (EVDO/LTE, v3 pooled formal tau)
before being used for the five-trace final analysis. Implements exactly the
frozen DP (stay or one adjacent ladder step; dwell = no change at e-1 when
changing at e; transition cost uses the entered epoch's regime label).
"""
import json, math, os, sys

LADDER = [1, 2, 4, 8, 16]
B = 5242880  # B_e = 5 MiB
INF = math.inf


def load_epochs(pilot, trace_id, plan_path, epoch_key="2m0s"):
    plan = json.load(open(plan_path))
    label = {e["epoch_index"]: e["regime"] for e in plan["epochs"]}
    by = {}
    for c in LADDER:
        r = json.load(open(os.path.join(pilot, "%s_C%d" % (trace_id, c), "result.json")))
        dur = r["durations"][epoch_key]
        for e in dur["epochs"]:
            by.setdefault(e["epoch_index"], {})[c] = (e["r_bytes_per_second"], e["valid"])
    idxs = []
    for i, per in by.items():
        if len(per) != len(LADDER):
            continue
        if all(per[c][1] for c in LADDER):
            idxs.append(i)
    idxs.sort()
    epochs = []
    for i in idxs:
        epochs.append({"index": i, "regime": label.get(i), "rates": {c: by[i][c][0] for c in LADDER}})
    return epochs


def load_table(agg_path, formal=True):
    agg = json.load(open(agg_path))
    pooled, regime_specific, regime_medians = {}, {}, {}
    for d in agg["directions"]:
        key = (d["from_c"], d["to_c"])
        regime_specific[key] = d["regime_specific"]
        if formal:
            pooled[key] = d["formal_tau_seconds"]
            if d["regime_specific"] and d.get("formal_per_regime_tau"):
                regime_medians[key] = dict(d["formal_per_regime_tau"])
        else:
            pooled[key] = d["pooled_tau_seconds"]
            if d["regime_specific"] and d.get("per_regime_tau"):
                regime_medians[key] = dict(d["per_regime_tau"])
    return {"pooled": pooled, "regime_specific": regime_specific, "regime_medians": regime_medians}


def tau_value(table, frm, to, regime):
    key = (frm, to)
    if key not in table["pooled"]:
        raise KeyError(key)
    if table["regime_specific"].get(key):
        m = table["regime_medians"].get(key)
        if m and regime in m:
            return m[regime]
    return table["pooled"][key]


def fixed_completion(epochs):
    return {c: sum(B / e["rates"][c] for e in epochs) for c in LADDER}


def instantaneous(epochs):
    return sum(B / max(e["rates"][c] for c in LADDER) for e in epochs)


def solve_constrained(epochs, table):
    for e in epochs:
        if e["regime"] is None:
            raise ValueError("epoch %d has no frozen regime label" % e["index"])
    n = len(epochs)
    idx = {c: i for i, c in enumerate(LADDER)}
    noChange, changed = 0, 1
    cost = [[[INF, INF] for _ in LADDER] for _ in range(n)]
    parent = [[[None, None] for _ in LADDER] for _ in range(n)]
    for ci, c in enumerate(LADDER):
        cost[0][ci][noChange] = B / epochs[0]["rates"][c]
    for e in range(1, n):
        regime = epochs[e]["regime"]
        for ci, c in enumerate(LADDER):
            best = INF
            bestPrev = noChange
            for prev in (noChange, changed):
                if cost[e - 1][ci][prev] < best:
                    best = cost[e - 1][ci][prev]
                    bestPrev = prev
            if not math.isinf(best):
                cost[e][ci][noChange] = best + B / epochs[e]["rates"][c]
                parent[e][ci][noChange] = (c, bestPrev)
            for cpi, cp in enumerate(LADDER):
                if cp == c or abs(idx[cp] - idx[c]) != 1:
                    continue
                base = cost[e - 1][cpi][noChange]
                if math.isinf(base):
                    continue
                tau = tau_value(table, cp, c, regime)
                val = base + tau + B / epochs[e]["rates"][c]
                if val < cost[e][ci][changed]:
                    cost[e][ci][changed] = val
                    parent[e][ci][changed] = (cp, noChange)
    best = INF
    bestCi, bestPrev = -1, noChange
    for ci, c in enumerate(LADDER):
        for prev in (noChange, changed):
            if cost[n - 1][ci][prev] < best:
                best = cost[n - 1][ci][prev]
                bestCi, bestPrev = ci, prev
    seq = [0] * n
    ci, prev = bestCi, bestPrev
    for e in range(n - 1, -1, -1):
        seq[e] = LADDER[ci]
        if e == 0:
            break
        p = parent[e][ci][prev]
        ci = LADDER.index(p[0])
        prev = p[1]
    audit = []
    for e in range(1, n):
        if seq[e] != seq[e - 1]:
            audit.append({"epoch_index": e, "from_c": seq[e - 1], "to_c": seq[e],
                          "regime": epochs[e]["regime"],
                          "tau_seconds": tau_value(table, seq[e - 1], seq[e], epochs[e]["regime"])})
    return {"objective": best, "sequence": seq, "transitions": len(audit), "transition_details": audit}


def validate():
    pilot = "/home/vscode/epoch_pilot_v2"
    v3 = "/home/vscode/tau_campaign_v3"
    table = load_table(os.path.join(v3, "tau_aggregation.json"), formal=True)
    out = {}
    for trace, plan in [("Verizon-EVDO-driving", os.path.join(v3, "plan", "evdo_plan.json")),
                        ("Verizon-LTE-driving", os.path.join(v3, "plan", "lte_plan.json"))]:
        epochs = load_epochs(pilot, trace, plan)
        fx = fixed_completion(epochs)
        inst = instantaneous(epochs)
        sol = solve_constrained(epochs, table)
        best = min(fx.values())
        bestc = [c for c in LADDER if fx[c] == best][0]
        out[trace] = {"retained": len(epochs), "fixed": fx, "best": best, "best_c": bestc,
                      "instantaneous": inst, "constrained": sol,
                      "h": (best - sol["objective"]) / best}
    ref_fixed = json.load(open("/home/vscode/oracle_stage_v3/fixed_c_completion.json"))
    ref_inst = json.load(open("/home/vscode/oracle_stage_v3/instantaneous_oracle.json"))
    ref_con = json.load(open("/home/vscode/oracle_stage_v3/constrained_oracle.json"))
    ok = True
    for rt in ref_fixed["traces"]:
        t = rt["trace_id"]
        mine = out[t]
        if mine["retained"] != rt["retained_epochs"]:
            ok = False; print("MISMATCH retained", t)
        for ct in rt["t_fixed_seconds"]:
            if abs(mine["fixed"][ct["c"]] - ct["value"]) > 1e-9:
                ok = False; print("MISMATCH t_fixed", t, ct["c"], mine["fixed"][ct["c"]], ct["value"])
        if abs(mine["best"] - rt["t_best_fixed_seconds"]) > 1e-9:
            ok = False; print("MISMATCH best", t)
    for rt in ref_inst["traces"]:
        if abs(out[rt["trace_id"]]["instantaneous"] - rt["t_instantaneous_seconds"]) > 1e-9:
            ok = False; print("MISMATCH inst", rt["trace_id"])
    for rt in ref_con["traces"]:
        mine = out[rt["trace_id"]]["constrained"]
        if abs(mine["objective"] - rt["t_constrained_seconds"]) > 1e-9:
            ok = False; print("MISMATCH con", rt["trace_id"])
        if mine["sequence"] != rt["sequence"]:
            ok = False; print("MISMATCH seq", rt["trace_id"], mine["sequence"], rt["sequence"])
    for t, m in out.items():
        print("%s: retained=%d best=%.10f (C%d) inst=%.10f con=%.10f H=%.10f" % (
            t, m["retained"], m["best"], m["best_c"], m["instantaneous"], m["constrained"]["objective"], m["h"]))
    print("REPLICA VALIDATION:", "PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(validate())
