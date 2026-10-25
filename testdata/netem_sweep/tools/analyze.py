#!/usr/bin/env python3
"""Predeclared analysis of netem_sweep (protocol.md). Standard library only.

Usage: analyze.py   (reads runs_S/, writes analysis/S.json)
"""
import glob, json, math, os, re

BASE = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
M = json.load(open(os.path.join(BASE, "manifest.json")))
RID = re.compile(r"^(.*)_b(\d\d)_(.*)$")


def geo(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


def main():
    tab, invalid = {}, []
    for p in sorted(glob.glob(os.path.join(BASE, "runs_S", "*.json"))):
        r = json.load(open(p))
        sc, b, arm = RID.match(r["run_id"]).groups()
        if r.get("content_verified") is True and not r.get("err"):
            tab.setdefault(sc, {}).setdefault(arm, {})[int(b)] = r["completion_ms"] / 1000
        else:
            invalid.append({"run_id": r["run_id"], "err": (r.get("err") or "content not verified")[:200]})
    cells, summary = {}, {}
    for sc in M["stages"]["S"]["scenario_order"]:
        ours = M["stages"]["S"]["scenario_arms"][sc][0]
        a, b = tab.get(sc, {}).get(ours, {}), tab.get(sc, {}).get("aria2", {})
        blocks = sorted(set(a) & set(b))
        rs = [a[k] / b[k] for k in blocks]
        side = None if not rs else "ours_faster" if max(rs) < 1 else "aria2_faster" if min(rs) > 1 else "mixed"
        cells[sc] = {"n_blocks": len(rs), "ours_s": geo(list(a.values())) if a else None,
                     "aria2_s": geo(list(b.values())) if b else None,
                     "geo_ratio": geo(rs) if rs else None, "min": min(rs) if rs else None,
                     "max": max(rs) if rs else None, "side": side}
        wl = sc.split("_")[0]
        summary.setdefault(wl, {}).setdefault(side, []).append(sc)
        c = cells[sc]
        if rs:
            print(f"{sc:16s} ours {c['ours_s']:6.1f} aria2 {c['aria2_s']:6.1f} ratio {c['geo_ratio']:.3f} [{c['min']:.3f}, {c['max']:.3f}] {side}")
    ref = {wl: cells.get(f"{wl}_d40_l0p4", {}).get("side") for wl in summary}
    holds = {wl: all(c["side"] == ref[wl] for s, c in cells.items() if s.startswith(wl + "_")) for wl in summary}
    print("direction holds across grid:", holds, "invalid:", len(invalid))
    os.makedirs(os.path.join(BASE, "analysis"), exist_ok=True)
    json.dump({"label": "descriptive screen", "invalid": invalid, "cells": cells, "by_side": summary,
               "reference_side": ref, "direction_holds": holds},
              open(os.path.join(BASE, "analysis", "S.json"), "w"), indent=1)


if __name__ == "__main__":
    main()
