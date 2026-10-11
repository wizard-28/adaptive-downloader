#!/usr/bin/env python3
"""Mechanism-gate figures (live measurements only)."""
import json
import os

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

BASE = "/workspaces/mvp/testdata/live_joint_ns_queueing"
J = json.load(open(os.path.join(BASE, "analysis", "gate_analysis.json")))
CAL = json.load(open(os.path.join(BASE, "calibration", "model_calibration.json")))
FIG = os.path.join(BASE, "figures")
os.makedirs(FIG, exist_ok=True)
STATES = ["S0_no_background", "S1_moderate", "S2_heavy"]
N_GRID = [1, 2, 4, 8, 16]
C = 5_000_000
plt.rcParams.update({"font.size": 9, "figure.dpi": 130})

colors = {"S0_no_background": "tab:blue", "S1_moderate": "tab:orange", "S2_heavy": "tab:red"}


def cell(st, n):
    return J["states"][st]["cells"][str(n)]


# Fig 1: completion vs N + work-conserving prediction
fig, ax = plt.subplots(figsize=(7.5, 4.2))
for st in STATES:
    meds = [cell(st, n)["median_s"] for n in N_GRID]
    lo = [cell(st, n)["min_s"] for n in N_GRID]
    hi = [cell(st, n)["max_s"] for n in N_GRID]
    ax.errorbar(N_GRID, meds, yerr=[np.array(meds) - np.array(lo), np.array(hi) - np.array(meds)],
                marker="o", capsize=3, color=colors[st], label=st)
    b_ach = cell(st, 16)["achieved_background_bps"]
    if b_ach:
        share = C - b_ach / 8.0
        ax.axhline((32 << 20) / share, color=colors[st], ls=":", lw=1,
                   label="%s work-conserving F/(C-B)" % st)
ax.set_xscale("log", base=2)
ax.set_xticks(N_GRID)
ax.set_xticklabels(N_GRID)
ax.set_xlabel("downloader concurrency N")
ax.set_ylabel("completion time (s)")
ax.set_title("Fig 1 - live completion vs N by contention state (median, min-max of 3 reps)")
ax.legend(fontsize=6.5)
ax.grid(alpha=0.3)
fig.savefig(os.path.join(FIG, "fig1_completion_vs_N.png"), bbox_inches="tight")
plt.close(fig)

# Fig 2: RTT vs N
fig, axes = plt.subplots(1, 2, figsize=(11, 3.8))
for ax, key, lab in [(axes[0], "ping_median_ms", "median RTT (ms)"), (axes[1], "ping_p95_ms", "p95 RTT (ms)")]:
    for st in STATES:
        ax.plot(N_GRID, [cell(st, n)[key] for n in N_GRID], marker="o", color=colors[st], label=st)
    ax.axhline(0.036, color="grey", ls="--", lw=1, label="no-qdisc baseline (0.04 ms)")
    ax.set_xscale("log", base=2)
    ax.set_xticks(N_GRID)
    ax.set_xticklabels(N_GRID)
    ax.set_xlabel("N")
    ax.set_ylabel(lab)
    ax.set_title("Fig 2 - queueing delay vs N (%s)" % lab)
    ax.grid(alpha=0.3)
axes[0].legend(fontsize=6.5)
fig.savefig(os.path.join(FIG, "fig2_rtt_vs_N.png"), bbox_inches="tight")
plt.close(fig)

# Fig 3: drops / retrans vs N
fig, axes = plt.subplots(1, 2, figsize=(11, 3.8))
for ax, key, lab in [(axes[0], "drops", "qdisc drops"), (axes[1], "retrans", "TCP retransmissions")]:
    for st in STATES:
        ax.plot(N_GRID, [cell(st, n)[key] for n in N_GRID], marker="o", color=colors[st], label=st)
    ax.set_xscale("log", base=2)
    ax.set_xticks(N_GRID)
    ax.set_xticklabels(N_GRID)
    ax.set_xlabel("N")
    ax.set_ylabel(lab)
    ax.set_title("Fig 3 - %s vs N" % lab)
    ax.grid(alpha=0.3)
axes[0].legend(fontsize=6.5)
fig.savefig(os.path.join(FIG, "fig3_drops_retrans_vs_N.png"), bbox_inches="tight")
plt.close(fig)

# Fig 4: offered vs achieved background
fig, ax = plt.subplots(figsize=(7, 3.8))
xs = np.arange(len(STATES))
offered, achieved = [], []
for st in STATES:
    bo = json.load(open(os.path.join(BASE, "mechanism_gate", "background_only_%s.json" % st)))
    off = bo.get("offered_bits_per_second", bo.get("offered_bps", 0) * 8)
    ach = bo.get("achieved_bits_per_second", bo.get("achieved_bps", 0.0))
    offered.append(off / 1e6)
    achieved.append(ach / 1e6)
w = 0.35
ax.bar(xs - w / 2, offered, width=w, label="offered (background-only)")
ax.bar(xs + w / 2, achieved, width=w, label="achieved (background-only)")
for i, st in enumerate(STATES):
    per_n = [cell(st, n)["achieved_background_bps"] for n in N_GRID]
    per_n = [v / 1e6 for v in per_n if v]
    if per_n:
        ax.plot([i + w / 2] * len(per_n), per_n, "k.", ms=5,
                label="achieved during downloads (per N)" if i == 0 else None)
ax.set_xticks(xs)
ax.set_xticklabels(STATES)
ax.set_ylabel("Mbit/s")
ax.set_title("Fig 4 - background offered vs achieved under the 40 Mbit/s bottleneck")
ax.legend(fontsize=7)
ax.grid(alpha=0.3, axis="y")
fig.savefig(os.path.join(FIG, "fig4_background.png"), bbox_inches="tight")
plt.close(fig)

# Fig 5: model vs live
fig, ax = plt.subplots(figsize=(6.4, 4.6))
for st in STATES:
    obs = [r["observed_s"] for r in CAL["rows"] if r["state"] == st]
    pred = [r["predicted_s"] for r in CAL["rows"] if r["state"] == st]
    ax.scatter(obs, pred, color=colors[st], label=st)
lim = [5, 80]
ax.plot(lim, lim, "k--", lw=1)
ax.set_xscale("log")
ax.set_yscale("log")
ax.set_xlim(lim)
ax.set_ylim(lim)
ax.set_xlabel("live median completion (s)")
ax.set_ylabel("model prediction (s)")
ax.set_title("Fig 5 - calibrated queueing model vs live (RMSE %.1f%%)" % (100 * CAL["rmse_rel"]))
ax.legend(fontsize=7)
ax.grid(alpha=0.3)
fig.savefig(os.path.join(FIG, "fig5_model_vs_live.png"), bbox_inches="tight")
plt.close(fig)

print("figures written")
