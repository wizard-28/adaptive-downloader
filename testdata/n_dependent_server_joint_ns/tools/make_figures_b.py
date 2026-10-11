#!/usr/bin/env python3
"""Track 2-B figures (live gate + adaptive results)."""
import glob
import json
import os
import statistics

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
FIG = os.path.join(BASE, "figures")
os.makedirs(FIG, exist_ok=True)
MIB = 1024 ** 2
N_GRID = [1, 2, 4, 8, 16]
S_GRID = [262144, 1048576, 4194304, 16777216, 33554432]
plt.rcParams.update({"font.size": 9, "figure.dpi": 130})

G = json.load(open(os.path.join(BASE, "analysis", "gate_b.json")))
try:
    A = json.load(open(os.path.join(BASE, "analysis", "adaptive_b.json")))
except Exception:
    A = None


def surf(state):
    M = np.full((len(N_GRID), len(S_GRID)), np.nan)
    for i, n in enumerate(N_GRID):
        for j, s in enumerate(S_GRID):
            v = G["cells"][state].get("%d/%d" % (n, s))
            M[i, j] = v if v is not None else np.nan
    return M


# Fig B1: live surfaces
for state, fname in [("healthy", "figB1_surface_healthy.png"), ("capped", "figB1_surface_capped.png")]:
    fig, ax = plt.subplots(figsize=(7.5, 3.6))
    M = surf(state)
    im = ax.imshow(M, aspect="auto", cmap="viridis")
    for i in range(len(N_GRID)):
        for j in range(len(S_GRID)):
            v = M[i, j]
            ax.text(j, i, "%.0f" % (v / 1000) if np.isfinite(v) else "x", ha="center",
                    va="center", fontsize=7,
                    color="white" if not np.isfinite(v) or v < np.nanmax(M) * 0.55 else "black")
    o = G["optima"][state]
    ax.scatter([S_GRID.index(o["S"])], [N_GRID.index(o["N"])], marker="*", s=200,
               c="red", edgecolors="white", zorder=5)
    ax.set_xticks(range(len(S_GRID)))
    ax.set_xticklabels(["%dM" % (s // MIB) for s in S_GRID])
    ax.set_yticks(range(len(N_GRID)))
    ax.set_yticklabels(N_GRID)
    ax.set_xlabel("S")
    ax.set_ylabel("N")
    ax.set_title("Fig B1 - live fixed completion (s), %s state; star = optimum" % state)
    fig.colorbar(im, ax=ax, shrink=0.85, label="completion (s)")
    fig.savefig(os.path.join(FIG, fname), bbox_inches="tight")
    plt.close(fig)

# Fig B2: optima movement + margins
fig, axes = plt.subplots(1, 2, figsize=(11, 4))
ax = axes[0]
for state, col in [("healthy", "tab:blue"), ("capped", "tab:red")]:
    o = G["optima"][state]
    ax.scatter([o["N"]], [o["S"] / MIB], s=180, color=col, zorder=5, label=state)
    ax.annotate("%s (%d,%dM)\n%.1fs" % (state, o["N"], o["S"] // MIB, o["completion_s"]),
                (o["N"], o["S"] / MIB), textcoords="offset points", xytext=(0, 12),
                ha="center", fontsize=8)
ax.set_xscale("log", base=2)
ax.set_xticks(N_GRID)
ax.set_xticklabels(N_GRID)
ax.set_yticks([0.25, 1, 4, 16, 32])
ax.set_yticklabels(["256K", "1M", "4M", "16M", "32M"])
ax.set_xlabel("N*")
ax.set_ylabel("S*")
ax.set_title("Fig B2a - live state-dependent joint optimum")
ax.grid(alpha=0.3)
ax.legend(fontsize=8)
ax = axes[1]
checks = G["gate"]["checks"]
names = list(checks.keys())
vals = [1 if checks[k] else 0 for k in names]
ax.barh(names, vals, color=["tab:green" if v else "tab:red" for v in vals])
ax.set_xlim(0, 1.2)
ax.set_title("Fig B2b - live gate checks (pass=%s)" % G["gate"]["pass"])
fig.savefig(os.path.join(FIG, "figB2_gate.png"), bbox_inches="tight")
plt.close(fig)

# Fig B3: cap mechanism from a capped run
capped_runs = sorted(glob.glob(os.path.join(BASE, "fixed_grid", "gate_capped_n16_s4194304_r*.json")))
if capped_runs:
    j = json.load(open(capped_runs[0]))
    reqs = sorted([r for r in j["server_requests"] if r["has_range"]], key=lambda r: r["start_ms"])
    fig, ax = plt.subplots(figsize=(8, 4))
    xs = [r["active_k"] for r in reqs]
    ys = [r["rate_bytes_per_second"] / 1024.0 for r in reqs]
    ax.scatter(xs, ys, s=8, alpha=0.5, label="observed per-request rate")
    ks = list(range(1, 17))
    ax.plot(ks, [min(2560000, 8388608 / (k * (1 + 0.05 * (k - 1)))) / 1024.0 for k in ks],
            color="black", ls="--", lw=1, label="min(base, C/(k(1+gamma(k-1))))")
    ax.set_xlabel("active requests k at request start")
    ax.set_ylabel("effective rate (KiB/s)")
    ax.set_title("Fig B3 - live soft concurrency cap (capped state, (16, 4 MiB) run)")
    ax.legend(fontsize=8)
    ax.grid(alpha=0.3)
    fig.savefig(os.path.join(FIG, "figB3_cap_mechanism.png"), bbox_inches="tight")
    plt.close(fig)

# Fig B4/B5: adaptive results (when available)
if A:
    fig, axes = plt.subplots(1, 2, figsize=(12, 4))
    ax = axes[0]
    states = [s for s in ("healthy", "capped") if s in A.get("p1", {})]
    w = 0.35
    xs = np.arange(len(states))
    for i, mode in enumerate(["sel_mean_s", "fixed_mean_s", "aria_mean_s"]):
        vals = [A["p1"][s][mode] for s in states]
        ax.bar(xs + (i - 1) * w, vals, width=w, label=mode.replace("_mean_s", ""))
    ax.set_xticks(xs)
    ax.set_xticklabels(states)
    ax.set_ylabel("mean completion (s)")
    ax.set_title("Fig B4 - P1 selector vs fixed vs aria2")
    ax.legend(fontsize=8)
    ax.grid(alpha=0.3, axis="y")
    ax = axes[1]
    modes = [m for m in ("causal_joint", "causal_n_only", "fixed_16_1M", "fixed_4_4M", "fixed_4_1M", "aria2")
             if m in A.get("p2", {}).get("modes", {})]
    vals = [A["p2"]["modes"][m]["mean_s"] for m in modes]
    ax.bar(modes, vals)
    ax.set_ylabel("mean completion (s)")
    ax.set_title("Fig B5 - P2 within-download modes")
    ax.tick_params(axis="x", rotation=25)
    ax.grid(alpha=0.3, axis="y")
    fig.savefig(os.path.join(FIG, "figB4_adaptive.png"), bbox_inches="tight")
    plt.close(fig)

print("figures written")
