#!/usr/bin/env python3
"""Track 2 figures: pre-screen surfaces and the structural STOP evidence."""
import json
import os
import statistics
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
sys.path.insert(0, os.path.join(BASE, "tools"))
import ns_sim as sim  # noqa: E402

FIG = os.path.join(BASE, "figures")
os.makedirs(FIG, exist_ok=True)
MIB = sim.MIB
J = json.load(open(os.path.join(BASE, "analysis", "prescreen.json")))
A = json.load(open(os.path.join(BASE, "analysis", "track2_analysis.json")))
N_GRID = [1, 2, 4, 8, 16]
S_GRID = [int(k) for k in J["grid"]["S"]]
FS = 64 * MIB
plt.rcParams.update({"font.size": 9, "figure.dpi": 130})

e = J["file_sizes"][str(FS)]
good = e["good"]["cells"]
name = "D_r0.4_l0.02_c0.04"
st = e["candidates"][name]["state"]

# recompute the degraded cells (same seeds as the pre-screen)
deg = {}
for n in N_GRID:
    for s in S_GRID:
        run = sim.Run(n, s, file_bytes=FS)
        rs = [sim.simulate(sim.State(**st), run, seed) for seed in range(1, 21)]
        ok = [r["completion"] for r in rs if not r["aborted"]]
        deg["%d/%d" % (n, s)] = {"median": statistics.median(ok) if ok else None,
                                 "success": len(ok) / len(rs)}


def surf(cells, key):
    M = np.full((len(N_GRID), len(S_GRID)), np.nan)
    for i, n in enumerate(N_GRID):
        for j, s in enumerate(S_GRID):
            v = cells["%d/%d" % (n, s)][key]
            M[i, j] = v if v is not None else np.nan
    return M


# Fig 1/2: surfaces
for M, title, fname in [
    (surf(good, "median_completion"), "Fig 1 - GOOD state completion surface T(N,S) (F=64 MiB)", "fig1_surface_good.png"),
    (surf(deg, "median"), "Fig 2 - DEGRADED state (rate 0.4 MiB/s, congestion 0.04) completion surface", "fig2_surface_degraded.png")]:
    fig, ax = plt.subplots(figsize=(9, 3.8))
    im = ax.imshow(M, aspect="auto", cmap="viridis")
    for i in range(len(N_GRID)):
        for j in range(len(S_GRID)):
            v = M[i, j]
            ax.text(j, i, "%.0f" % v if np.isfinite(v) else "x", ha="center", va="center",
                    fontsize=6, color="white" if not np.isfinite(v) or v < np.nanmax(M) * 0.6 else "black")
    ax.set_xticks(range(len(S_GRID)))
    ax.set_xticklabels(["%.0f" % (s / MIB) for s in S_GRID], fontsize=7)
    ax.set_yticks(range(len(N_GRID)))
    ax.set_yticklabels(N_GRID)
    ax.set_xlabel("S (MiB)")
    ax.set_ylabel("N")
    ax.set_title(title)
    fig.colorbar(im, ax=ax, shrink=0.85, label="median completion (s); x = no valid completion")
    fig.savefig(os.path.join(FIG, fname), bbox_inches="tight")
    plt.close(fig)

# Fig 3: completion vs N (min across S)
fig, ax = plt.subplots(figsize=(7.5, 4))
for lab, cells, col in [("GOOD", good, "tab:blue"), ("DEGRADED", deg, "tab:red")]:
    ys = []
    for n in N_GRID:
        vals = [cells["%d/%d" % (n, s)]["median_completion" if lab == "GOOD" else "median"]
                for s in S_GRID]
        vals = [v for v in vals if v is not None]
        ys.append(min(vals) if vals else np.nan)
    ax.plot(N_GRID, ys, marker="o", color=col, label=lab)
ax.set_xscale("log", base=2)
ax.set_yscale("log")
ax.set_xticks(N_GRID)
ax.set_xticklabels(N_GRID)
ax.set_xlabel("N")
ax.set_ylabel("best completion across S (s)")
ax.set_title("Fig 3 - state-dependent N optima (degraded: N=8 and N=16 tie)")
ax.grid(alpha=0.3, which="both")
ax.legend()
fig.savefig(os.path.join(FIG, "fig3_completion_vs_N.png"), bbox_inches="tight")
plt.close(fig)

# Fig 4: completion vs S at the best N per state (showing the timeout cliff)
fig, ax = plt.subplots(figsize=(8, 4))
for lab, cells, n, col in [("GOOD (N=16)", good, 16, "tab:blue"), ("DEGRADED (N=8)", deg, 8, "tab:red")]:
    ys = [cells["%d/%d" % (n, s)]["median_completion" if lab.startswith("GOOD") else "median"] for s in S_GRID]
    ax.plot([s / MIB for s in S_GRID], ys, marker="o", color=col, label=lab)
ax.axvline((st["rate"] * 30) / MIB, color="black", ls="--", lw=1,
           label="timeout cliff: S = per x 30 s = %.1f MiB" % ((st["rate"] * 30) / MIB))
ax.set_xlabel("S (MiB)")
ax.set_ylabel("median completion (s)")
ax.set_title("Fig 4 - completion vs S: the degraded optimum sits just below the timeout cliff")
ax.grid(alpha=0.3)
ax.legend(fontsize=8)
fig.savefig(os.path.join(FIG, "fig4_completion_vs_S.png"), bbox_inches="tight")
plt.close(fig)

# Fig 5: abort-rate surface (degraded)
fig, ax = plt.subplots(figsize=(9, 3.8))
M = surf(deg, "success")
im = ax.imshow(1 - M, aspect="auto", cmap="Reds", vmin=0, vmax=1)
for i in range(len(N_GRID)):
    for j in range(len(S_GRID)):
        v = 1 - M[i, j]
        ax.text(j, i, "%.0f" % (100 * v), ha="center", va="center", fontsize=6,
                color="white" if v > 0.5 else "black")
ax.set_xticks(range(len(S_GRID)))
ax.set_xticklabels(["%.0f" % (s / MIB) for s in S_GRID], fontsize=7)
ax.set_yticks(range(len(N_GRID)))
ax.set_yticklabels(N_GRID)
ax.set_xlabel("S (MiB)")
ax.set_ylabel("N")
ax.set_title("Fig 5 - DEGRADED abort rate (%%): the retry-limit cliff, not a smooth concurrency penalty")
fig.colorbar(im, ax=ax, shrink=0.85, label="abort rate")
fig.savefig(os.path.join(FIG, "fig5_abort_surface.png"), bbox_inches="tight")
plt.close(fig)

# Fig 6: N*/S* across the predeclared candidates and file sizes
fig, axes = plt.subplots(1, 2, figsize=(12, 4))
for ax, key, lab in [(axes[0], "N", "N*"), (axes[1], "S", "S* (MiB)")]:
    names = sorted(J["file_sizes"]["67108864"]["candidates"].keys())
    xs = np.arange(len(names))
    for fb, col in zip([str(64 * MIB), str(128 * MIB), str(256 * MIB)], ["tab:blue", "tab:orange", "tab:green"]):
        vals = []
        for nm in names:
            o = J["file_sizes"][fb]["candidates"][nm]["optimum"]
            vals.append(o[key] if o else np.nan)
        if key == "S":
            vals = [v / MIB if v == v else np.nan for v in vals]
        ax.scatter(xs, vals, s=25, color=col, label="F=%d MiB" % (int(fb) // MIB))
    go = J["file_sizes"]["67108864"]["good"]["optimum"]
    ax.axhline(go[key] / MIB if key == "S" else go[key], color="black", ls="--", lw=1, label="GOOD optimum")
    ax.set_xticks(xs)
    ax.set_xticklabels(names, rotation=40, ha="right", fontsize=7)
    ax.set_ylabel(lab)
    ax.set_title("Fig 6 - %s across the predeclared degraded candidates" % lab)
    ax.grid(alpha=0.3)
axes[0].legend(fontsize=7)
fig.savefig(os.path.join(FIG, "fig6_candidates.png"), bbox_inches="tight")
plt.close(fig)

print("figures written")
