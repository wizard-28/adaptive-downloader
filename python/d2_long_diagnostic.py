"""Diagnostic stage: D2/512MiB mechanism + long-duration trajectory design and
static C=1..16 sweeps. Reads the 20-pair replication and the previous
long-duration D2 runs; uses the frozen v6 simulator only for environment
design (no training, no v6 changes).

Writes into testdata/diagnostics/long_duration_d2_diagnostic/.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import statistics as st
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.sim_env_v6 import Phase, SimConfig, SimEnv  # noqa: E402

MIB = 1024 * 1024
SWEEP_SEEDS = [300, 301]


def load(path):
    with open(path) as f:
        return json.load(f)


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


def write_json(path, obj):
    with open(path, "w") as f:
        json.dump(obj, f, indent=2, sort_keys=True)
        f.write("\n")


# ---------------------------------------------------------------------------
# Part A/B: D2 mechanism
# ---------------------------------------------------------------------------

def request_profile(run):
    events = [e for e in run.get("server_requests", []) if e.get("has_range")]
    if not events:
        return {}
    t0 = events[0]["start"]
    import datetime

    def off(s):
        return (datetime.datetime.fromisoformat(s.replace("Z", "+00:00"))
                - datetime.datetime.fromisoformat(t0.replace("Z", "+00:00"))).total_seconds()

    lens, rates, phases = [], {}, [0, 0, 0]
    for e in events:
        lens.append(e["range"]["End"] - e["range"]["Start"] + 1)
        rates[str(e["rate"])] = rates.get(str(e["rate"]), 0) + 1
        o = off(e["start"])
        phases[0 if o < 2 else (1 if o < 4 else 2)] += 1
    return {
        "range_count": len(events),
        "range_size_bytes": sorted(set(lens)),
        "rate_histogram": rates,
        "requests_started_phase": {"<2s": phases[0], "2-4s": phases[1], ">=4s": phases[2]},
        "max_in_flight": run.get("max_in_flight"),
        "duration_ms": run["duration_ms"],
    }


def d2_mechanism(rep_dir, orig512_dir, orig16_dir):
    out = {"replication": {}, "previous_512": {}, "previous_16": {}}
    for label, d in (("replication", rep_dir), ("previous_512", orig512_dir), ("previous_16", orig16_dir)):
        raw = load(os.path.join(d, "raw_results_all.json"))["results"]
        d2 = [r for r in raw if r["condition"] == "D2"]
        by_pair = {}
        for r in d2:
            by_pair.setdefault(r["pair_id"], {})[r["system"]] = r
        pairs = []
        for pid, m in sorted(by_pair.items()):
            if "dqn" in m and "aria2" in m and m["dqn"]["valid"] and m["aria2"]["valid"]:
                v, a = m["dqn"], m["aria2"]
                pairs.append({
                    "pair_id": pid,
                    "v6_ms": v["duration_ms"], "aria2_ms": a["duration_ms"],
                    "ratio": v["duration_ms"] / a["duration_ms"],
                    "v6_final_concurrency": v.get("final_concurrency"),
                    "v6_phase_mean_concurrency": v.get("phase_mean_concurrency"),
                    "v6_profile": request_profile(v),
                    "aria2_profile": request_profile(a),
                })
        if pairs:
            ratios = [p["ratio"] for p in pairs]
            out[label] = {
                "pairs": len(pairs),
                "v6_mean_ms": st.mean([p["v6_ms"] for p in pairs]),
                "aria2_mean_ms": st.mean([p["aria2_ms"] for p in pairs]),
                "geometric_ratio": math.exp(st.mean([math.log(r) for r in ratios])),
                "ratio_min": min(ratios), "ratio_max": max(ratios),
                "v6_faster_pairs": sum(1 for r in ratios if r < 1.0),
                "per_pair": pairs,
                "aria2_range_sizes": sorted({s for p in pairs for s in p["aria2_profile"].get("range_size_bytes", [])}),
                "v6_range_sizes": sorted({s for p in pairs for s in p["v6_profile"].get("range_size_bytes", [])}),
                "aria2_requests_started_phase": {
                    k: st.mean([p["aria2_profile"]["requests_started_phase"][k] for p in pairs]) for k in ("<2s", "2-4s", ">=4s")
                },
                "v6_requests_started_phase": {
                    k: st.mean([p["v6_profile"]["requests_started_phase"][k] for p in pairs]) for k in ("<2s", "2-4s", ">=4s")
                },
            }
    out["mechanism"] = (
        "At 512 MiB aria2 splits into 16 pieces of 32 MiB (--split=16); each piece is one "
        "HTTP range request whose pacing rate is selected at request start and held for the "
        "whole request. All aria2 requests start before the 2 s transition at the 640 KiB/s "
        "initial rate, so ~32 MiB/request at 640 KiB/s takes ~51 s. v6 allocates 1 MiB ranges "
        "dynamically, so its requests complete in ~1.6 s and 505/512 are issued after recovery "
        "at 2.5 MiB/s. The reversal is therefore a request-granularity x phase-locked-pacing "
        "interaction, not evidence of generic adaptive superiority."
    )
    return out


# ---------------------------------------------------------------------------
# Part C: long multi-transition trajectory (new family L1, separate from A2-E2)
# ---------------------------------------------------------------------------

def p(rate, link=0, lat=0.0, loss=0.0, cong=0.0):
    return {"rate": rate, "link_bandwidth": link, "latency_s": lat,
            "loss_percent": loss, "congestion": cong}


L1_PHASES = [
    p(8 * MIB, link=6 * MIB, lat=0.02, loss=0.3, cong=0.004),     # 1 good
    p(8 * MIB, link=1 * MIB, lat=0.12, loss=3.0, cong=0.02),      # 2 severe degradation
    p(8 * MIB, link=4 * MIB, lat=0.04, loss=0.6, cong=0.006),     # 3 recovery
    p(256 * 1024, lat=0.03, loss=0.5, cong=0.004),                # 4 server throttle
    p(8 * MIB, link=5 * MIB, lat=0.03, loss=0.4, cong=0.005),     # 5 recovery
    p(8 * MIB, link=1.5 * MIB, lat=0.09, loss=2.5, cong=0.015),   # 6 another degradation
    p(8 * MIB, link=6 * MIB, lat=0.02, loss=0.3, cong=0.004),     # 7 good (to completion)
]

L1_SPEC = {
    "name": "L1",
    "label": "long multi-transition trajectory (new family, separate from the frozen A2-E2)",
    "phases": L1_PHASES,
    "phase_seconds_bounds": [12, 18],
    "phase_count": len(L1_PHASES),
    "note": "phase durations are predetermined bounds [12,18] s; the last phase runs to completion",
}


def single_phase_env(phase, c, seed, file_mib):
    cfg = SimConfig(file_size=file_mib * MIB, min_concurrency=c, max_concurrency=c,
                    decision_interval=1, max_episode_steps=5000,
                    phases=[Phase(**phase)], rand_phase_seconds=(15.0, 15.0))
    env = SimEnv(cfg, seed=seed)
    env.reset()
    t = 0.0
    while not env.done():
        t += env.step(0)["elapsed"]
    return t


def per_phase_sweep(file_mib=128):
    res = {}
    for i, phase in enumerate(L1_PHASES):
        by_c = {str(c): st.mean([single_phase_env(phase, c, s, file_mib) for s in SWEEP_SEEDS])
                for c in range(1, 17)}
        best = min(range(1, 17), key=lambda c: by_c[str(c)])
        res[str(i + 1)] = {"phase": phase, "mean_completion_s_by_c": {k: round(v, 4) for k, v in by_c.items()},
                           "best_c": best, "throughput_mibps": file_mib / by_c[str(best)]}
    bests = sorted({v["best_c"] for v in res.values()})
    return {"file_mib": file_mib, "seeds": SWEEP_SEEDS, "phases": res,
            "distinct_best_c": bests,
            "has_low_and_high": any(b <= 6 for b in bests) and any(b >= 11 for b in bests)}


def full_trajectory_env(c, seed, file_mib, dur):
    cfg = SimConfig(file_size=file_mib * MIB, min_concurrency=c, max_concurrency=c,
                    decision_interval=1, max_episode_steps=20000,
                    phases=[Phase(**ph) for ph in L1_PHASES],
                    rand_phase_seconds=(dur, dur))
    env = SimEnv(cfg, seed=seed)
    env.reset()
    total = 0.0
    phase_time = [0.0] * len(L1_PHASES)
    while not env.done():
        pi = env.phase_index
        e = env.step(0)["elapsed"]
        total += e
        phase_time[pi] += e
    return total, phase_time, env.phase_index


def full_trajectory_sweep(file_mib=512, dur=15.0):
    res = {}
    for c in range(1, 17):
        vals, ptimes, final_idx = [], [], []
        for s in SWEEP_SEEDS:
            t, pt, fi = full_trajectory_env(c, s, file_mib, dur)
            vals.append(t)
            ptimes.append(pt)
            final_idx.append(fi)
        res[str(c)] = {
            "mean_completion_s": st.mean(vals),
            "mean_phase_time_s": [round(st.mean([pt[i] for pt in ptimes]), 3) for i in range(len(L1_PHASES))],
            "mean_final_phase_index": st.mean(final_idx),
        }
    best = min(range(1, 17), key=lambda c: res[str(c)]["mean_completion_s"])
    return {"file_mib": file_mib, "phase_seconds": dur, "seeds": SWEEP_SEEDS,
            "by_c": res, "best_c": best,
            "c16_completion_s": res["16"]["mean_completion_s"],
            "c1_completion_s": res["1"]["mean_completion_s"]}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", default="testdata/diagnostics/long_duration_d2_diagnostic")
    ap.add_argument("--skip-sweep", action="store_true")
    args = ap.parse_args()
    base = os.path.abspath(args.dir)
    os.makedirs(base, exist_ok=True)

    # ---- D2 mechanism ----
    mech = d2_mechanism(
        os.path.join(base, "replication"),
        os.path.abspath("testdata/diagnostics/dqn_long_duration_v6/size_512MiB"),
        os.path.abspath("testdata/diagnostics/dqn_long_duration_v6/size_16MiB"),
    )
    stats = load(os.path.join(base, "replication", "statistical_analysis.json"))
    mech["replication_established_stats"] = stats["aggregate"]
    mech["replication_per_condition"] = stats["per_condition"]
    write_json(os.path.join(base, "d2_mechanism.json"), mech)

    # ---- long trajectory design + sweeps ----
    write_json(os.path.join(base, "long_trajectory_spec.json"), L1_SPEC)
    result = {"L1_SPEC": L1_SPEC}
    if not args.skip_sweep:
        phase_sweep = per_phase_sweep(128)
        full_sweep = full_trajectory_sweep(512, 15.0)
        result["per_phase_sweep"] = phase_sweep
        result["full_trajectory_sweep"] = full_sweep
        result["file_size_rationale"] = {
            "chosen_live_size_mib": 512,
            "reasons": [
                "512 MiB is the largest size already validated end-to-end in this container (RAM ~1 GB available; 1 GiB is unsafe).",
                "6 predetermined phases of 12-18 s => the trajectory stays dynamic for ~72-108 s; at the shared caps a 512 MiB transfer lasts longer than that at C=1..16 (see full sweep).",
                "startup cost measured at 16/128/512 MiB was 12.5%/5.1%/2.4% of runtime, so 512 MiB keeps startup small while spanning many transitions.",
            ],
        }
        write_json(os.path.join(base, "static_sweep_long_trajectory.json"), result)

    # ---- report ----
    lines = []
    w = lines.append
    w("D2/512MiB mechanism + long-duration trajectory diagnostic (no training; no new head-to-head)")
    w("")
    w("PART A/B - D2 replication and mechanism")
    rep = mech["replication"]
    w(f"  replication pairs={rep['pairs']} v6 mean={rep['v6_mean_ms']:.1f}ms aria2 mean={rep['aria2_mean_ms']:.1f}ms "
      f"geo ratio={rep['geometric_ratio']:.4f} range=[{rep['ratio_min']:.3f},{rep['ratio_max']:.3f}] v6 faster={rep['v6_faster_pairs']}/{rep['pairs']}")
    a = stats["aggregate"]
    w(f"  established pooled: ratio={a['geometric_ratio']:.4f} CI={a['ratio_ci95']} p={a['p_one_sided']:.4g} claim_supported={stats['claim_supported']}")
    w(f"  aria2 range sizes={rep['aria2_range_sizes']}; v6 range sizes={rep['v6_range_sizes']}")
    w(f"  aria2 requests started <2s/2-4s/>=4s = {rep['aria2_requests_started_phase']}")
    w(f"  v6    requests started <2s/2-4s/>=4s = {rep['v6_requests_started_phase']}")
    w(f"  mechanism: {mech['mechanism']}")
    w("")
    w("PART C/D/E - long multi-transition trajectory L1")
    for i, ph in enumerate(L1_PHASES):
        w(f"  phase {i+1}: rate={ph['rate']} link={ph['link_bandwidth']} lat={ph['latency_s']} loss={ph['loss_percent']} cong={ph['congestion']}")
    w(f"  phase durations: {L1_SPEC['phase_seconds_bounds']} s; last phase to completion")
    if not args.skip_sweep:
        ps = result["per_phase_sweep"]
        w(f"  per-phase steady sweep (128 MiB, C=1..16): distinct best C={ps['distinct_best_c']} has_low_and_high={ps['has_low_and_high']}")
        for k, v in ps["phases"].items():
            w(f"    phase {k}: best C={v['best_c']} best throughput={v['throughput_mibps']:.2f} MiB/s")
        fs = result["full_trajectory_sweep"]
        w(f"  full L1 sweep (512 MiB, 15 s phases): best C={fs['best_c']} "
          f"C1={fs['c1_completion_s']:.1f}s C16={fs['c16_completion_s']:.1f}s")
        for c in ("1", "4", "8", "12", "16"):
            v = fs["by_c"][c]
            w(f"    C{c}: completion={v['mean_completion_s']:.1f}s final_phase_index={v['mean_final_phase_index']:.1f} phase_time={v['mean_phase_time_s']}")
    with open(os.path.join(base, "report.txt"), "w") as f:
        f.write("\n".join(lines) + "\n")

    hashes = {}
    for root, _, files in os.walk(base):
        for name in files:
            if name == "artifact_hashes.json":
                continue
            pth = os.path.join(root, name)
            hashes[os.path.relpath(pth, base)] = sha256_file(pth)
    write_json(os.path.join(base, "artifact_hashes.json"), hashes)
    print(f"D2 replication ratio={rep['geometric_ratio']:.4f} v6_faster={rep['v6_faster_pairs']}/{rep['pairs']}")
    if not args.skip_sweep:
        print(f"per-phase best C={result['per_phase_sweep']['distinct_best_c']}; "
              f"full L1 best C={result['full_trajectory_sweep']['best_c']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
