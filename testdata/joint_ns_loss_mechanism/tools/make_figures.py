#!/usr/bin/env python3
"""Figures for the correlated-loss / range-retry analytical gate."""
import json
import os
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

BASE = "/workspaces/mvp/testdata/joint_ns_loss_mechanism"
sys.path.insert(0, os.path.join(BASE, "tools"))
import loss_sim as L  # noqa: E402

J = json.load(open(os.path.join(BASE, "analytical_gate", "stationary_screen.json")))
A = json.load(open(os.path.join(BASE, "analysis", "analytical_gate_analysis.json")))
SW = json.load(open(os.path.join(BASE, "analytical_gate", "robustness_sweep.json")))
FIG = os.path.join(BASE, "figures")
os.makedirs(FIG, exist_ok=True)
MIB = L.MIB
CONDS = ["GOOD", "BAD"]
COL = {"GOOD": "tab:blue", "BAD": "tab:red"}
plt.rcParams.update({"font.size": 9, "figure.dpi": 130})


def cell(cond, n, s):
    return J["conditions"][cond]["cells"]["%d/%d" % (n, s)]


# Fig 1: completion vs N (min across S)
fig, ax = plt.subplots(figsize=(7.5, 4.2))
for cond in CONDS:
    ys = [A["conditions"][cond]["N"][str(n)]["min_completion_s"] for n in L.N_GRID]
    ax.plot(L.N_GRID, ys, marker="o", color=COL[cond], label="%s (min across S)" % cond)
ax.set_xscale("log", base=2)
ax.set_yscale("log")
ax.set_xticks(L.N_GRID)
ax.set_xticklabels(L.N_GRID)
ax.set_xlabel("concurrency N")
ax.set_ylabel("completion (s)")
ax.set_title("Fig 1 - completion vs N by loss condition (median of 10 seeds; min across S)")
ax.grid(alpha=0.3, which="both")
ax.legend(fontsize=8)
fig.savefig(os.path.join(FIG, "fig1_completion_vs_N.png"), bbox_inches="tight")
plt.close(fig)

# Fig 2: completion vs S at N=16
fig, ax = plt.subplots(figsize=(7.5, 4.2))
xs = np.arange(len(L.S_GRID))
for cond in CONDS:
    ys, err = [], []
    for s in L.S_GRID:
        c = cell(cond, 16, s)
        ys.append(c["median_completion"] if c["median_completion"] else np.nan)
        err.append(0)
    ax.plot(xs, ys, marker="o", color=COL[cond], label=cond)
    for i, s in enumerate(L.S_GRID):
        c = cell(cond, 16, s)
        if not c["viable"] or c["median_completion"] is None:
            ax.annotate("non-viable\n(%.0f%% success)" % (100 * c["success_fraction"]),
                        (i, ys[i] if not np.isnan(ys[i]) else ax.get_ylim()[0]),
                        textcoords="offset points", xytext=(0, -28), ha="center", fontsize=7,
                        color=COL[cond])
ax.set_xticks(xs)
ax.set_xticklabels(["256K", "1M", "4M", "16M"])
ax.set_xlabel("chunk size S")
ax.set_ylabel("completion (s), N=16")
ax.set_title("Fig 2 - completion vs S by loss condition (N=16, median of 10 seeds)")
ax.grid(alpha=0.3)
ax.legend(fontsize=8)
fig.savefig(os.path.join(FIG, "fig2_completion_vs_S.png"), bbox_inches="tight")
plt.close(fig)

# Figs 3/4: T(N,S) surfaces
for cond, fname in [("GOOD", "fig3_surface_GOOD.png"), ("BAD", "fig4_surface_BAD.png")]:
    fig, ax = plt.subplots(figsize=(6.4, 4.2))
    M = np.full((len(L.N_GRID), len(L.S_GRID)), np.nan)
    for i, n in enumerate(L.N_GRID):
        for j, s in enumerate(L.S_GRID):
            c = cell(cond, n, s)
            M[i, j] = c["median_completion"] if (c["viable"] and c["median_completion"]) else np.nan
    im = ax.imshow(np.log10(M), aspect="auto", cmap="viridis")
    for i, n in enumerate(L.N_GRID):
        for j, s in enumerate(L.S_GRID):
            c = cell(cond, n, s)
            txt = ("%.0f" % c["median_completion"]) if (c["viable"] and c["median_completion"]) else "fail"
            ax.text(j, i, txt, ha="center", va="center", fontsize=7,
                    color="white" if not np.isfinite(M[i, j]) or np.log10(M[i, j]) < 2.2 else "black")
    ax.set_xticks(range(len(L.S_GRID)))
    ax.set_xticklabels(["256K", "1M", "4M", "16M"])
    ax.set_yticks(range(len(L.N_GRID)))
    ax.set_yticklabels(L.N_GRID)
    ax.set_xlabel("S")
    ax.set_ylabel("N")
    opt = J["conditions"][cond]["optimum"]
    ax.scatter([L.S_GRID.index(opt["S"])], [L.N_GRID.index(opt["N"])], marker="*", s=200,
               c="red", edgecolors="white", zorder=5)
    ax.set_title("Fig %s - completion surface T(N,S), %s (log10 colour, star = optimum)" %
                 ("3" if cond == "GOOD" else "4", cond))
    fig.colorbar(im, ax=ax, shrink=0.85, label="log10 completion (s)")
    fig.savefig(os.path.join(FIG, fname), bbox_inches="tight")
    plt.close(fig)

# Fig 5: optimum movement (the key figure)
fig, axes = plt.subplots(1, 2, figsize=(11, 4))
ax = axes[0]
for cond in CONDS:
    opt = J["conditions"][cond]["optimum"]
    ax.scatter([opt["N"]], [opt["S"] / MIB], s=180, color=COL[cond], zorder=5,
               label="%s optimum" % cond)
    ax.annotate("%s: (N=%d, S=%dM)\n%.1fs" % (cond, opt["N"], opt["S"] // MIB, opt["completion"]),
                (opt["N"], opt["S"] / MIB), textcoords="offset points", xytext=(-10, 12),
                ha="center", fontsize=8)
ax.set_xscale("log", base=2)
ax.set_xticks(L.N_GRID)
ax.set_xticklabels(L.N_GRID)
ax.set_yticks([0.25, 1, 4, 16])
ax.set_yticklabels(["256K", "1M", "4M", "16M"])
ax.set_xlabel("N*")
ax.set_ylabel("S*")
ax.set_title("Fig 5a - fixed optimum by loss condition (N* identical: 16)")
ax.grid(alpha=0.3)
ax.legend(fontsize=7.5)
ax = axes[1]
m = A["materiality"]
labels = ["GOOD: use BAD's S*\n(16M -> 4M)", "BAD: use GOOD's S*\n(4M -> 16M)"]
vals = [100 * m["GOOD_at_N16"]["penalty"], 100 * m["BAD_at_N16"]["penalty"]]
ax.bar(labels, vals, color=["tab:blue", "tab:red"])
ax.axhline(5, color="black", ls="--", lw=1, label="5% materiality")
ax.set_ylabel("completion penalty at N=16 (%)")
ax.set_title("Fig 5b - materiality of the S* movement")
ax.legend(fontsize=7.5)
fig.savefig(os.path.join(FIG, "fig5_optimum_movement.png"), bbox_inches="tight")
plt.close(fig)

# Fig 6: failure exposure vs S
fig, axes = plt.subplots(1, 2, figsize=(11, 3.8))
for ax, key, lab in [(axes[0], "per_attempt_failure_rate", "per-attempt failure rate"),
                     (axes[1], "download_abort_rate", "download abort rate")]:
    xs = np.arange(len(L.S_GRID))
    for cond in CONDS:
        vals = [A["conditions"][cond]["S_retry"][str(s)][key] for s in L.S_GRID]
        ax.plot(xs, vals, marker="o", color=COL[cond], label=cond)
    ax.set_xticks(xs)
    ax.set_xticklabels(["256K", "1M", "4M", "16M"])
    ax.set_xlabel("S")
    ax.set_ylabel(lab)
    ax.set_title("Fig 6 - %s vs S" % lab)
    ax.grid(alpha=0.3)
axes[0].legend(fontsize=8)
fig.savefig(os.path.join(FIG, "fig6_failure_vs_S.png"), bbox_inches="tight")
plt.close(fig)

# Fig 7: retry cost vs S
fig, axes = plt.subplots(1, 2, figsize=(11, 3.8))
for ax, key, lab in [(axes[0], "retries_per_download", "retries per download"),
                     (axes[1], "retry_bytes_per_download_MiB", "retry bytes per download (MiB)")]:
    xs = np.arange(len(L.S_GRID))
    for cond in CONDS:
        vals = [A["conditions"][cond]["S_retry"][str(s)][key] for s in L.S_GRID]
        ax.plot(xs, vals, marker="o", color=COL[cond], label=cond)
    ax.set_xticks(xs)
    ax.set_xticklabels(["256K", "1M", "4M", "16M"])
    ax.set_xlabel("S")
    ax.set_ylabel(lab)
    ax.set_title("Fig 7 - %s vs S" % lab)
    ax.grid(alpha=0.3)
axes[0].legend(fontsize=8)
fig.savefig(os.path.join(FIG, "fig7_retry_cost_vs_S.png"), bbox_inches="tight")
plt.close(fig)

# Fig 8: robustness
fig, ax = plt.subplots(figsize=(9.5, 4))
names = list(SW["variants"].keys())
xs = np.arange(len(names))
for i, cond in enumerate(CONDS):
    s_star = [SW["variants"][nm][cond]["optimum"]["S"] / MIB for nm in names]
    n_star = [SW["variants"][nm][cond]["optimum"]["N"] for nm in names]
    ax.scatter(xs + (i - 0.5) * 0.25, s_star, s=70, color=COL[cond], label="%s S*" % cond)
    for x, s_, n_ in zip(xs + (i - 0.5) * 0.25, s_star, n_star):
        ax.annotate("N=%d" % n_, (x, s_), textcoords="offset points", xytext=(0, 7),
                    ha="center", fontsize=6.5)
ax.set_xticks(xs)
ax.set_xticklabels(names, rotation=25, ha="right", fontsize=8)
ax.set_yticks([0.25, 1, 4, 16])
ax.set_yticklabels(["256K", "1M", "4M", "16M"])
ax.set_ylabel("S*")
ax.set_title("Fig 8 - robustness: optimum across predeclared parameter variants (N*=16 everywhere)")
ax.grid(alpha=0.3)
ax.legend(fontsize=8)
fig.savefig(os.path.join(FIG, "fig8_robustness.png"), bbox_inches="tight")
plt.close(fig)

print("figures written")
