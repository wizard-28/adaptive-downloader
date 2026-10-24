#!/usr/bin/env python3
"""Adaptation speed of the adaptive controllers, from recorded decision logs.

Usage: adaptation_speed.py OUT_JSON

- reaction_s: for runs where the cap switches on at 8 s (CAP8), seconds from 8 s
  to the controller's first concurrency move that reduces N after 8 s.
- first_move_s: for runs with the cap on from the start (CAP0), seconds to the
  first non-"none" decision.
- ramp_s: for the online tuner without a cap (HEALTHY), seconds until a
  decision observes N = 16.
Only valid runs are used. Standard library only.
"""
import glob, json, statistics, sys

CAP_AT_MS = 8000
SOURCES = [
    ("split_ablation", "CAP8", "causal_joint_NT_split"),
    ("split_ablation", "CAP8", "causal_joint_T30"),
    ("split_ablation", "CAP0", "selector_NT_split"),
    ("online_tuner", "CAP8", "online_tuner"),
    ("online_tuner", "HEALTHY", "online_tuner"),
]


def runs(study, sc, arm):
    for p in sorted(glob.glob(f"testdata/{study}/runs/{sc}_b*_{arm}.json")):
        r = json.load(open(p))
        if r.get("content_verified") is True and not r.get("err"):
            yield r


def metric(sc, arm, d):
    moves = [e for e in d if e["action_kind"] != "none"]
    if sc == "CAP8":
        after = [e for e in moves if e["time_ms"] >= CAP_AT_MS and e["action_kind"] == "decrease-concurrency"]
        return ("reaction_s", (after[0]["time_ms"] - CAP_AT_MS) / 1000 if after else None)
    if sc == "CAP0":
        return ("first_move_s", moves[0]["time_ms"] / 1000 if moves else None)
    hit = [e for e in d if e["concurrency"] >= 16]
    return ("ramp_s", hit[0]["time_ms"] / 1000 if hit else None)


out = {}
for study, sc, arm in SOURCES:
    vals, name = [], None
    for r in runs(study, sc, arm):
        name, v = metric(sc, arm, r.get("decisions") or [])
        if v is not None:
            vals.append(v)
    key = f"{study}/{sc}/{arm}"
    out[key] = {"metric": name, "n": len(vals),
                "median": round(statistics.median(vals), 2) if vals else None,
                "min": round(min(vals), 2) if vals else None, "max": round(max(vals), 2) if vals else None}
    print(key, out[key])
json.dump(out, open(sys.argv[1], "w"), indent=1)
