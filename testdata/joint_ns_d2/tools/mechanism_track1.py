#!/usr/bin/env python3
"""Track 1 mechanism analysis: reconstruct why each mode performed as it did
from the recorded per-decision trajectories and the server request logs."""
import glob
import json
import os
import statistics
from collections import Counter, defaultdict

BASE = "/workspaces/mvp/testdata/joint_ns_d2"
MIB = 1024 ** 2
MODES = ["fixed", "n-only", "s-only", "joint"]
N_MIN, N_MAX, S_MIN, S_MAX, S_STEP = 1, 16, 256 << 10, 16 << 20, 1 << 20


def clamp(v, lo, hi):
    return max(lo, min(hi, v))


def derive(decisions):
    states = []
    n, s = None, None
    for d in decisions:
        if n is None:
            n, s = d["concurrency"], d["chunk_size"]
        states.append((d["time_ms"], n, s, d["throughput"], d["aggregate_throughput"], d["action_kind"]))
        k = d["action_kind"]
        if k == "increase-concurrency":
            n = clamp(n + 1, N_MIN, N_MAX)
        elif k == "decrease-concurrency":
            n = clamp(n - 1, N_MIN, N_MAX)
        elif k == "increase-chunk-size":
            s = clamp(s + S_STEP, S_MIN, S_MAX)
        elif k == "decrease-chunk-size":
            s = clamp(s - S_STEP, S_MIN, S_MAX)
    return states, (n, s)


def main():
    out = {"modes": {}}
    for m in MODES:
        runs = []
        for p in sorted(glob.glob(os.path.join(BASE, "per_run", "b*_%s.json" % m))):
            j = json.load(open(p))
            reqs = [r for r in j["server_requests"] if r["has_range"]]
            sizes = Counter(r["range_end"] - r["range_start"] + 1 for r in reqs)
            rates = Counter(r["rate_bytes_per_second"] for r in reqs)
            phases = Counter("<2s" if r["start_ms"] < 2000 else ("2-4s" if r["start_ms"] < 4000 else ">=4s")
                             for r in reqs)
            decs = j.get("decisions") or []
            states, final = derive(decs) if decs else ([], None)
            actions = Counter(d["action_kind"] for d in decs)
            actionable = [d for d in decs if d["action_kind"] != "none"]
            last_action = max((d["time_ms"] for d in actionable), default=None)
            runs.append({
                "run_id": os.path.basename(p)[:-5],
                "completion_ms": j["completion_ms"],
                "n_requests": len(reqs), "retries": j["client_retries"],
                "sizes_MiB": sorted(round(s / MIB, 2) for s in sizes),
                "rate_counts": {str(k): v for k, v in rates.items()},
                "phase_counts": dict(phases),
                "n_decisions": len(decs), "actions": dict(actions),
                "first_action_ms": min((d["time_ms"] for d in actionable), default=None),
                "last_action_ms": last_action,
                "actions_before_2s": sum(1 for d in actionable if d["time_ms"] < 2000),
                "actions_2_4s": sum(1 for d in actionable if 2000 <= d["time_ms"] < 4000),
                "actions_after_4s": sum(1 for d in actionable if d["time_ms"] >= 4000),
                "final_state": list(final) if final else None,
                "unique_states": len({(s[1], s[2]) for s in states}),
                "throughput_saturation_ms": next((s[0] for s in states if s[0] > 8000), None),
            })
        finals = [r["final_state"] for r in runs if r["final_state"]]
        out["modes"][m] = {
            "n_runs": len(runs),
            "median_n_requests": statistics.median(r["n_requests"] for r in runs),
            "total_retries": sum(r["retries"] for r in runs),
            "median_decisions": statistics.median(r["n_decisions"] for r in runs),
            "median_first_action_ms": statistics.median(r["first_action_ms"] for r in runs) if any(r["first_action_ms"] for r in runs) else None,
            "median_last_action_ms": statistics.median(r["last_action_ms"] for r in runs) if any(r["last_action_ms"] for r in runs) else None,
            "median_final_N": statistics.median(f[0] for f in finals) if finals else None,
            "median_final_S_MiB": statistics.median(f[1] for f in finals) / MIB if finals else None,
            "final_states": sorted({(f[0], round(f[1] / MIB, 2)) for f in finals}),
            "phase_counts_median": {k: statistics.median(r["phase_counts"].get(k, 0) for r in runs)
                                    for k in ["<2s", "2-4s", ">=4s"]},
            "runs": runs,
        }

    # joint signal saturation: the throughput at each decision, median across runs by decision index
    joint = out["modes"]["joint"]["runs"]
    prof = defaultdict(list)
    for r in joint:
        j = json.load(open(os.path.join(BASE, "per_run", r["run_id"] + ".json")))
        for i, d in enumerate(j.get("decisions") or []):
            prof[i].append(d["throughput"])
    out["joint_signal"] = {
        "median_throughput_by_decision": {str(i): statistics.median(v) / 1024
                                          for i, v in sorted(prof.items())},
        "note": "windowed per-request throughput (KiB/s) at each decision index; the +/-10% rule stops firing once this signal stabilizes after the pacing recovery",
    }
    json.dump(out, open(os.path.join(BASE, "analysis", "mechanism_track1.json"), "w"), indent=2)

    print("=== mechanism summary ===")
    for m in MODES:
        s = out["modes"][m]
        print("  %-7s reqs=%4d retries=%2d decisions=%3d final(N,S)=%s first_action=%sms last_action=%sms phases=%s" % (
            m, s["median_n_requests"], s["total_retries"], s["median_decisions"],
            (s["median_final_N"], s["median_final_S_MiB"]), s["median_first_action_ms"],
            s["median_last_action_ms"], s["phase_counts_median"]))
    sig = out["joint_signal"]["median_throughput_by_decision"]
    keys = sorted(sig, key=int)
    print("  joint per-request throughput (KiB/s) by decision index:",
          {k: round(sig[k]) for k in keys[:14]}, "... last:", {k: round(sig[k]) for k in keys[-3:]})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
