#!/usr/bin/env python3
"""Track 1 figures."""
import glob
import json
import os
import statistics

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

BASE = "/workspaces/mvp/testdata/joint_ns_d2"
FIG = os.path.join(BASE, "figures")
os.makedirs(FIG, exist_ok=True)
MIB = 1024 ** 2
MODES = ["fixed", "n-only", "s-only", "joint"]
COL = {"fixed": "black", "n-only": "tab:blue", "s-only": "tab:green", "joint": "tab:red"}
J = json.load(open(os.path.join(BASE, "analysis", "track1_analysis.json")))
M = json.load(open(os.path.join(BASE, "analysis", "mechanism_track1.json")))
plt.rcParams.update({"font.size": 9, "figure.dpi": 130})


def load_runs(mode):
    return [json.load(open(p)) for p in sorted(glob.glob(os.path.join(BASE, "per_run", "b*_%s.json" % mode)))]


def derive_states(decisions):
    N_MIN, N_MAX, S_MIN, S_MAX, S_STEP = 1, 16, 256 << 10, 16 << 20, 1 << 20
    states = []
    n = s = None
    for d in decisions:
        if n is None:
            n, s = d["concurrency"], d["chunk_size"]
        states.append((d["time_ms"], n, s))
        k = d["action_kind"]
        if k == "increase-concurrency":
            n = min(N_MAX, n + 1)
        elif k == "decrease-concurrency":
            n = max(N_MIN, n - 1)
        elif k == "increase-chunk-size":
            s = min(S_MAX, s + S_STEP)
        elif k == "decrease-chunk-size":
            s = max(S_MIN, s - S_STEP)
    return states, (n, s)


# Fig 1: N trajectories (joint) + fixed reference; Fig 2: S trajectories
for fig, idx, ref, ylab, fname in [
        (1, 1, 16, "N", "fig1_n_trajectory.png"),
        (2, 2, 4, "S (MiB)", "fig2_s_trajectory.png")]:
    f, axes = plt.subplots(1, 2, figsize=(11.5, 4))
    for ax, mode in [(axes[0], "joint"), (axes[1], "n-only" if idx == 1 else "s-only")]:
        runs = load_runs(mode)
        for j in runs:
            st, fin = derive_states(j["decisions"] or [])
            ts = [0.0] + [s[0] for s in st] + [j["completion_ms"]]
            vals = [(4 if idx == 1 else 4)] + [s[idx] if idx == 1 else s[idx] / MIB for s in st] + \
                   [(fin[0] if idx == 1 else fin[1] / MIB)]
            ax.step(ts, vals, where="post", color=COL[mode], alpha=0.35, lw=1)
        ax.axhline(ref, color="black", ls="--", lw=1.2, label="fixed best (%s=%s)" % (ylab, ref))
        ax.set_xlabel("time (s)")
        ax.set_ylabel(ylab)
        ax.set_title("Fig %d - %s trajectories (%s, all 20 runs)" % (fig, ylab, mode))
        ax.grid(alpha=0.3)
        ax.legend(fontsize=7)
    f.savefig(os.path.join(FIG, fname), bbox_inches="tight")
    plt.close(f)

# Fig 3: completion comparison + paired lines
f, axes = plt.subplots(1, 2, figsize=(11.5, 4))
ax = axes[0]
data = [J["modes"][m]["completions_ms"] for m in MODES]
bp = ax.boxplot([[v / 1000 for v in d] for d in data], tick_labels=MODES, showfliers=False)
for i, d in enumerate(data):
    ax.scatter([i + 1] * len(d), [v / 1000 for v in d], s=8, color=COL[MODES[i]], zorder=3)
ax.set_ylabel("completion time (s)")
ax.set_title("Fig 3a - completion by mode (20 runs each)")
ax.grid(alpha=0.3, axis="y")
ax = axes[1]
for b in range(1, 21):
    j = next((r for r in J["modes"]["joint"]["completions_ms"]), None)
    vals = {}
    for m in MODES:
        rr = json.load(open(os.path.join(BASE, "per_run", "b%02d_%s.json" % (b, m))))
        vals[m] = rr["completion_ms"] / 1000
    ax.plot([0, 3], [vals["fixed"], vals["fixed"]], color="black", lw=0.6, alpha=0.5)
    ax.plot([0], [vals["fixed"]], "o", color="black", ms=4)
    ax.plot([1], [vals["n-only"]], "o", color=COL["n-only"], ms=4)
    ax.plot([2], [vals["s-only"]], "o", color=COL["s-only"], ms=4)
    ax.plot([3], [vals["joint"]], "o", color=COL["joint"], ms=4)
    ax.plot([0, 1], [vals["fixed"], vals["n-only"]], color="grey", lw=0.5, alpha=0.4)
    ax.plot([0, 2], [vals["fixed"], vals["s-only"]], color="grey", lw=0.5, alpha=0.4)
    ax.plot([0, 3], [vals["fixed"], vals["joint"]], color="grey", lw=0.5, alpha=0.4)
ax.set_xticks(range(4))
ax.set_xticklabels(MODES)
ax.set_ylabel("completion time (s)")
ax.set_title("Fig 3b - per-block pairing (each grey line = one block)")
ax.grid(alpha=0.3, axis="y")
f.savefig(os.path.join(FIG, "fig3_completion_comparison.png"), bbox_inches="tight")
plt.close(f)

# Fig 4: cumulative bytes vs time (median run per mode)
f, ax = plt.subplots(figsize=(7.5, 4.2))
for m in MODES:
    runs = sorted(load_runs(m), key=lambda j: j["completion_ms"])
    j = runs[len(runs) // 2]
    reqs = sorted([r for r in j["server_requests"] if r["has_range"]], key=lambda r: r["end_ms"])
    ts, cum = [0.0], [0.0]
    total = 0
    for r in reqs:
        total += r["range_end"] - r["range_start"] + 1
        ts.append(r["end_ms"] / 1000.0)
        cum.append(total / MIB)
    ax.step(ts, cum, where="post", color=COL[m], label="%s (%.1fs)" % (m, j["completion_ms"] / 1000))
ax.set_xlabel("time (s)")
ax.set_ylabel("cumulative bytes (MiB)")
ax.set_title("Fig 4 - cumulative bytes vs time (median run per mode)")
ax.legend(fontsize=8)
ax.grid(alpha=0.3)
f.savefig(os.path.join(FIG, "fig4_cumulative_bytes.png"), bbox_inches="tight")
plt.close(f)

# Fig 5: request-size distributions (grouped by mode)
f, ax = plt.subplots(figsize=(8.5, 4.2))
allvals = sorted({round((r["range_end"] - r["range_start"] + 1) / MIB, 2)
                  for m in MODES for j in load_runs(m) for r in j["server_requests"] if r["has_range"]})
w = 0.2
for i, m in enumerate(MODES):
    counts = []
    for v in allvals:
        c = sum(1 for j in load_runs(m) for r in j["server_requests"]
                if r["has_range"] and abs((r["range_end"] - r["range_start"] + 1) / MIB - v) < 1e-6)
        counts.append(c / 20.0)
    ax.bar(np.arange(len(allvals)) + (i - 1.5) * w, counts, width=w, color=COL[m], label=m)
ax.set_xticks(range(len(allvals)))
ax.set_xticklabels(["%.2f" % v for v in allvals], fontsize=7)
ax.set_xlabel("request size (MiB)")
ax.set_ylabel("requests per download (mean)")
ax.set_title("Fig 5 - request-size distribution by mode")
ax.legend(fontsize=8)
ax.grid(alpha=0.3, axis="y")
f.savefig(os.path.join(FIG, "fig5_request_sizes.png"), bbox_inches="tight")
plt.close(f)

# Fig 6: request start vs sampled rate (phase mechanism)
f, axes = plt.subplots(1, 4, figsize=(15, 3.6), sharey=True)
for ax, m in zip(axes, MODES):
    runs = sorted(load_runs(m), key=lambda j: j["completion_ms"])
    j = runs[len(runs) // 2]
    for r in [r for r in j["server_requests"] if r["has_range"]]:
        ax.scatter(r["start_ms"] / 1000.0, r["rate_bytes_per_second"] / 1024.0, s=10,
                   color=COL[m], alpha=0.7)
    ax.axvline(2, color="grey", ls=":")
    ax.axvline(4, color="grey", ls=":")
    ax.set_xlabel("request start (s)")
    ax.set_title("%s (%.1fs)" % (m, j["completion_ms"] / 1000))
    ax.grid(alpha=0.3)
axes[0].set_ylabel("sampled rate (KiB/s)")
f.suptitle("Fig 6 - D2 phase mechanism: request start time vs rate sampled at start (median run)")
f.savefig(os.path.join(FIG, "fig6_phase_mechanism.png"), bbox_inches="tight")
plt.close(f)

# Fig 7: signal saturation + action times (joint)
f, ax = plt.subplots(figsize=(8.5, 4.2))
sig = M["joint_signal"]["median_throughput_by_decision"]
idx = sorted(int(k) for k in sig)
ax.plot(idx, [sig[str(i)] for i in idx], marker=".", color="tab:red", label="median windowed per-request throughput")
acts = [(d["time_ms"], d["action_kind"]) for j in load_runs("joint") for d in (j["decisions"] or []) if d["action_kind"] != "none"]
ax.axvspan(0, 10, color="orange", alpha=0.08)
ax.set_xlabel("decision index (joint mode, median signal)")
ax.set_ylabel("windowed per-request throughput (KiB/s)")
ax.set_title("Fig 7 - the signal saturates after the pacing recovery; all %d actions occur in a 30 ms window at ~8.6 s" % len(acts))
ax.grid(alpha=0.3)
ax.legend(fontsize=8)
f.savefig(os.path.join(FIG, "fig7_signal_saturation.png"), bbox_inches="tight")
plt.close(f)

# Fig 8: paired ratios
f, ax = plt.subplots(figsize=(8.5, 4.2))
labels, ratios = [], []
for m in ["n-only", "s-only", "joint"]:
    rs = []
    for b in range(1, 21):
        j = json.load(open(os.path.join(BASE, "per_run", "b%02d_joint.json" % b)))["completion_ms"]
        o = json.load(open(os.path.join(BASE, "per_run", "b%02d_%s.json" % (b, m))))["completion_ms"]
        rs.append(j / o)
    ratios.append(rs)
    labels.append("joint / %s" % m)
ax.boxplot(ratios, tick_labels=labels, showfliers=False)
ax.axhline(1.0, color="black", lw=1)
ax.axhline(0.95, color="green", ls="--", lw=1, label="5% materiality")
ax.set_ylabel("per-block completion ratio")
ax.set_title("Fig 8 - paired ratios (joint vs other modes; <1 means joint faster)")
ax.legend(fontsize=8)
ax.grid(alpha=0.3, axis="y")
f.savefig(os.path.join(FIG, "fig8_paired_ratios.png"), bbox_inches="tight")
plt.close(f)

print("figures written")
