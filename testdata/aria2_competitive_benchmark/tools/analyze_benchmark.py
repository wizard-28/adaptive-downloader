#!/usr/bin/env python3
"""Analysis + figures for the aria2-competitive benchmark (existing artifacts only)."""
import json
import math
import os
import random
import statistics
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

BASE = "/workspaces/mvp/testdata/aria2_competitive_benchmark"
MIB = 1024 ** 2
OUT = os.path.join(BASE, "analysis")
FIG = os.path.join(BASE, "figures")
os.makedirs(OUT, exist_ok=True)
os.makedirs(FIG, exist_ok=True)


def load(path):
    return json.load(open(os.path.join(BASE, path)))


def paired_stats(pairs, a="a", b="b"):
    ta = [p[a]["completion_ms"] for p in pairs]
    tb = [p[b]["completion_ms"] for p in pairs]
    ratios = [x / y for x, y in zip(ta, tb)]
    logs = [math.log(r) for r in ratios]
    mean = statistics.mean(logs)
    sd = statistics.pstdev(logs)
    se = sd / math.sqrt(len(logs))
    ci = (math.exp(mean - 1.96 * se), math.exp(mean + 1.96 * se))
    wins = sum(1 for r in ratios if r < 1)
    losses = sum(1 for r in ratios if r > 1)
    # paired sign-flip permutation test (one-sided: A faster)
    rng = random.Random(20260928)
    obs = statistics.mean(logs)
    n = len(logs)
    ge = 0
    trials = 20000
    for _ in range(trials):
        s = 0.0
        for v in logs:
            s += v if rng.random() < 0.5 else -v
        if s / n <= obs:
            ge += 1
    p = (ge + 1) / (trials + 1)
    return {"n": n, "a_mean_ms": statistics.mean(ta), "b_mean_ms": statistics.mean(tb),
            "a_median_ms": statistics.median(ta), "b_median_ms": statistics.median(tb),
            "geo_ratio": math.exp(mean), "mean_ratio": statistics.mean(ratios),
            "median_ratio": statistics.median(ratios), "ratio_ci95": ci,
            "relative_improvement": 1 - math.exp(mean), "wins": wins, "losses": losses,
            "p_one_sided": p}


def main():
    fc = load("manifest/frozen_configs.json")
    ours = fc["our_final"]
    ar = fc["aria2_final"]
    d2 = load("reproduction_d2/pairs.json")["pairs"]
    d2stats = paired_stats(d2)
    final = load("final_reproduction/pairs.json")["pairs"]
    fstats = paired_stats(final)
    hist = load("analysis/d2_reproduction_stats.json")

    # frontiers: read every per-run artifact (runs.json was overwritten by the
    # top-3/extra blocks; the per-run JSONs are the authoritative records)
    import glob, re
    af_by_split = {}
    for pth in glob.glob(os.path.join(BASE, "discovery_aria2", "af-*.json")):
        j = json.load(open(pth))
        m = re.search(r"--split=(\d+)", j["config"].get("aria2_args", ""))
        if not m:
            continue
        af_by_split.setdefault(int(m.group(1)), []).append(j["completion_ms"])
    af_med = {s: statistics.median(v) for s, v in af_by_split.items()}
    of_by = {}
    for pth in glob.glob(os.path.join(BASE, "discovery_ours", "of-*.json")):
        j = json.load(open(pth))
        k = (j["config"]["concurrency"], j["config"]["chunk_bytes"])
        of_by.setdefault(k, []).append(j["completion_ms"])
    of_med = {k: statistics.median(v) for k, v in of_by.items()}

    # corrected discovery selection (3-run medians for the top-3; driver bookkeeping fix)
    top3 = {}
    for pat, key in [("of-c16-s4194304", (16, 4194304)), ("of-c16-s8388608", (16, 8388608)),
                     ("of-c16-s524288", (16, 524288))]:
        vals = []
        for pth in [os.path.join(BASE, "discovery_ours", pat + ".json"),
                    os.path.join(BASE, "discovery_ours", pat + "-r2.json"),
                    os.path.join(BASE, "discovery_ours", pat + "-r3.json")]:
            if os.path.exists(pth):
                vals.append(json.load(open(pth))["completion_ms"])
        if vals:
            top3["%d/%d" % key] = {"median_ms": statistics.median(vals), "runs": vals}
    const = {}
    for rid in ["const-ours-4M", "const-ours-32M", "const-aria2"]:
        pth = os.path.join(BASE, "mechanism", rid + ".json")
        if os.path.exists(pth):
            const[rid] = json.load(open(pth))["completion_ms"]

    # mechanism diagnostics from the final block traces
    def trace_stats(pairs, side):
        starts, sizes, inflight, phases = [], [], [], []
        for p in pairs:
            reqs = [r for r in p[side]["server_requests"] if r["has_range"]]
            for r in reqs:
                starts.append(r["start_ms"])
                sizes.append(r["range_end"] - r["range_start"] + 1)
                ph = "<2s" if r["start_ms"] < 2000 else ("2-4s" if r["start_ms"] < 4000 else ">=4s")
                phases.append(ph)
            # max in-flight
            events = []
            for r in reqs:
                events.append((r["start_ms"], 1))
                events.append((r["end_ms"], -1))
            events.sort()
            k = mx = 0
            for _, d in events:
                k += d
                mx = max(mx, k)
            inflight.append(mx)
        from collections import Counter
        return {"range_count": len(sizes), "mean_range_bytes": statistics.mean(sizes),
                "median_range_bytes": statistics.median(sizes), "max_inflight": max(inflight),
                "mean_inflight": statistics.mean(inflight),
                "phase_counts": dict(Counter(phases))}
    mech = {"ours": trace_stats(final, "a"), "aria2": trace_stats(final, "b")}

    result = {"frozen": fc, "d2_reproduction": {"stats": d2stats, "historical": hist},
              "aria2_frontier": {"medians": {str(k): v for k, v in sorted(af_med.items())},
                                 "runs_per_split": {str(k): len(v) for k, v in sorted(af_by_split.items())}},
              "ours_frontier": {"medians": {"%d/%d" % k: v for k, v in sorted(of_med.items(), key=lambda kv: kv[1])}},
              "final": fstats, "mechanism": mech,
              "selection_corrected_top3": top3, "constant_rate_controls_ms": const,
              "note_selection": "the driver recorded the top-3 medians over 2 re-runs; the corrected 3-run medians (grid run + 2 re-runs) are reported here; the frozen config (C=16, 4MiB) is the top-1 under both"}
    json.dump(result, open(os.path.join(OUT, "benchmark_analysis.json"), "w"), indent=2)

    # ---------------- figures ----------------
    # Fig 1: D2 historical vs fresh
    fig, ax = plt.subplots(figsize=(7, 4.2))
    labels = ["historical\n(v6 vs aria2-F)", "fresh reproduction\n(v6 vs aria2-F)"]
    vals = [hist["geo_ratio"], d2stats["geo_ratio"]]
    errs = [[vals[0] - hist["ratio_ci95"][0], vals[1] - d2stats["ratio_ci95"][0]],
            [hist["ratio_ci95"][1] - vals[0], d2stats["ratio_ci95"][1] - vals[1]]]
    ax.bar(labels, vals, yerr=errs, capsize=5, color=["#9ecae1", "#3182bd"], width=0.5)
    ax.axhline(1, color="k", ls="--", lw=1)
    for i, v in enumerate(vals):
        ax.text(i, v + 0.01, "%.3f" % v, ha="center")
    ax.set_ylabel("geometric completion ratio v6/aria2")
    ax.set_title("Fig. 1 — D2 historical vs fresh reproduction (LIVE EXPERIMENTAL)")
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "fig1_d2_reproduction.png"), dpi=170)
    plt.close(fig)

    # Fig 2: aria2 frontier
    fig, ax = plt.subplots(figsize=(7.5, 4.2))
    ss = sorted(af_med)
    ax.plot(ss, [af_med[s] / 1000 for s in ss], marker="o")
    ax.set_yscale("log")
    ax.set_xticks(ss)
    ax.set_xlabel("aria2 --split / --max-connection-per-server (min-split 1M)")
    ax.set_ylabel("median completion time (s, log)")
    ax.set_title("Fig. 2 — aria2 configuration frontier on D2/512MiB (LIVE EXPERIMENTAL)")
    ax.grid(True, alpha=0.3)
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "fig2_aria2_frontier.png"), dpi=170)
    plt.close(fig)

    # Fig 3: our frontier heatmap
    cs = [1, 2, 4, 8, 16]
    chunks = [262144, 524288, 1048576, 2097152, 4194304, 8388608, 16777216, 33554432]
    grid = np.full((len(cs), len(chunks)), np.nan)
    for (c, ch), v in of_med.items():
        grid[cs.index(c), chunks.index(ch)] = v / 1000
    fig, ax = plt.subplots(figsize=(8.5, 4.2))
    im = ax.imshow(grid, cmap="viridis_r", aspect="auto")
    ax.set_xticks(range(len(chunks)))
    ax.set_xticklabels(["256K", "512K", "1M", "2M", "4M", "8M", "16M", "32M"])
    ax.set_yticks(range(len(cs)))
    ax.set_yticklabels(cs)
    for i in range(len(cs)):
        for j in range(len(chunks)):
            ax.text(j, i, "%.0f" % grid[i, j], ha="center", va="center", color="white", fontsize=7)
    ax.set_xlabel("chunk size")
    ax.set_ylabel("concurrency C")
    ax.set_title("Fig. 3 — our fixed-policy frontier on D2/512MiB: median completion (s) (LIVE)")
    fig.colorbar(im, ax=ax, label="median s")
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "fig3_ours_frontier.png"), dpi=170)
    plt.close(fig)

    # Fig 4: final comparison
    fig, ax = plt.subplots(figsize=(7.5, 4.2))
    ta = [p["a"]["completion_ms"] / 1000 for p in final]
    tb = [p["b"]["completion_ms"] / 1000 for p in final]
    ax.boxplot([ta, tb], tick_labels=["ours (frozen)", "aria2 (frozen)"])
    ax.set_ylabel("completion time (s)")
    ax.set_title("Fig. 4 — Final 30 paired runs (LIVE EXPERIMENTAL; predeclared configs)")
    ax.grid(True, alpha=0.3, axis="y")
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "fig4_final_comparison.png"), dpi=170)
    plt.close(fig)

    # Fig 5: request-size distributions
    fig, ax = plt.subplots(figsize=(7.5, 4.2))
    sizes_ours = [r["range_end"] - r["range_start"] + 1 for p in final for r in p["a"]["server_requests"] if r["has_range"]]
    sizes_ar = [r["range_end"] - r["range_start"] + 1 for p in final for r in p["b"]["server_requests"] if r["has_range"]]
    bins = np.logspace(4, 8, 40)
    ax.hist(sizes_ours, bins=bins, alpha=0.6, label="ours (frozen)", color="#4c72b0")
    ax.hist(sizes_ar, bins=bins, alpha=0.6, label="aria2 (frozen)", color="#dd8452")
    ax.set_xscale("log")
    ax.set_xlabel("range size (bytes, log)")
    ax.set_ylabel("requests")
    ax.set_title("Fig. 5 — Request-size distributions (LIVE EXPERIMENTAL)")
    ax.legend()
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "fig5_request_sizes.png"), dpi=170)
    plt.close(fig)

    # Fig 6: request timeline for one pair (phase behavior)
    p = final[0]
    fig, ax = plt.subplots(figsize=(9.5, 4.4))
    for i, r in enumerate([x for x in p["a"]["server_requests"] if x["has_range"]][:40]):
        ax.barh(1, (r["end_ms"] - r["start_ms"]) / 1000, left=r["start_ms"] / 1000, height=0.3,
                color="#4c72b0", alpha=0.6)
    for i, r in enumerate([x for x in p["b"]["server_requests"] if x["has_range"]][:40]):
        ax.barh(0, (r["end_ms"] - r["start_ms"]) / 1000, left=r["start_ms"] / 1000, height=0.3,
                color="#dd8452", alpha=0.6)
    ax.axvline(2, color="k", ls="--", lw=1)
    ax.axvline(4, color="k", ls="--", lw=1)
    ax.text(2.05, 1.4, "throttle", fontsize=8)
    ax.text(4.05, 1.4, "recovery", fontsize=8)
    ax.set_yticks([0, 1])
    ax.set_yticklabels(["aria2", "ours"])
    ax.set_xlabel("time since download start (s)")
    ax.set_title("Fig. 6 — Request timeline vs phase-locked pacing (pair %s; LIVE)" % p["pair_id"])
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "fig6_request_timeline.png"), dpi=170)
    plt.close(fig)

    # Fig 7: cumulative bytes
    fig, ax = plt.subplots(figsize=(8, 4.4))
    for side, color, label in [("a", "#4c72b0", "ours"), ("b", "#dd8452", "aria2")]:
        ev = []
        for r in p[side]["server_requests"]:
            if r["has_range"]:
                ev.append((r["end_ms"] / 1000.0, r["range_end"] - r["range_start"] + 1))
        ev.sort()
        xs, ys, acc = [], [], 0
        for t, b in ev:
            acc += b
            xs.append(t)
            ys.append(acc / MIB)
        ax.plot(xs, ys, color=color, label=label)
    ax.axvline(2, color="k", ls="--", lw=1)
    ax.axvline(4, color="k", ls="--", lw=1)
    ax.set_xlabel("time (s)")
    ax.set_ylabel("cumulative bytes completed (MiB)")
    ax.set_title("Fig. 7 — Cumulative bytes over time (pair %s; LIVE)" % p["pair_id"])
    ax.legend()
    ax.grid(True, alpha=0.3)
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "fig7_cumulative_bytes.png"), dpi=170)
    plt.close(fig)

    # Fig 8: paired ratios (final)
    fig, ax = plt.subplots(figsize=(7.5, 4.2))
    ratios = [p["a"]["completion_ms"] / p["b"]["completion_ms"] for p in final]
    ax.hist(ratios, bins=12, color="#3182bd")
    ax.axvline(1, color="k", ls="--")
    ax.axvline(statistics.mean(ratios), color="r", ls="-", label="mean %.3f" % statistics.mean(ratios))
    ax.set_xlabel("paired completion ratio ours/aria2")
    ax.set_ylabel("pairs")
    ax.set_title("Fig. 8 — Final paired ratios (LIVE; %d/%d ours faster)" % (fstats["wins"], fstats["n"]))
    ax.legend()
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "fig8_paired_ratios.png"), dpi=170)
    plt.close(fig)

    # Fig 9: mechanism controls (granularity x phase-locking)
    ctrl = load("mechanism/controls.json")
    ca = paired_stats(ctrl["controlA"], "a", "b")
    fig, axes = plt.subplots(1, 2, figsize=(11, 4.2))
    ax = axes[0]
    labels = ["ours\n4MiB", "ours\n32MiB", "aria2\n32MiB"]
    vals = [fstats["a_mean_ms"] / 1000, ca["a_mean_ms"] / 1000, fstats["b_mean_ms"] / 1000]
    ax.bar(labels, vals, color=["#4c72b0", "#55a868", "#dd8452"], width=0.5)
    for i, v in enumerate(vals):
        ax.text(i, v + 0.5, "%.1f" % v, ha="center")
    ax.set_ylabel("mean completion time (s)")
    ax.set_title("D2 phase-locked schedule")
    ax.grid(True, alpha=0.3, axis="y")
    ax = axes[1]
    vals = [const.get("const-ours-4M", 0) / 1000, const.get("const-ours-32M", 0) / 1000,
            const.get("const-aria2", 0) / 1000]
    ax.bar(labels, vals, color=["#4c72b0", "#55a868", "#dd8452"], width=0.5)
    for i, v in enumerate(vals):
        ax.text(i, v + 0.3, "%.1f" % v, ha="center")
    ax.set_ylabel("completion time (s)")
    ax.set_title("constant 2.5 MB/s (no transition)")
    ax.grid(True, alpha=0.3, axis="y")
    fig.suptitle("Fig. 9 — Mechanism control: granularity effect is phase-locked (LIVE)")
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "fig9_mechanism_control.png"), dpi=170)
    plt.close(fig)

    print("analysis + 9 figures written")
    print("final stats:", {k: round(v, 4) if isinstance(v, float) else v for k, v in fstats.items()})
    print("mechanism:", json.dumps(mech, indent=1)[:800])
    return 0


if __name__ == "__main__":
    sys.exit(main())
