#!/usr/bin/env python3
"""Analytical figures for the adaptive-N mechanism screen (simulation output only)."""
import json
import os
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

BASE = "/workspaces/mvp/testdata/adaptive_n_mechanism_screen"
sys.path.insert(0, os.path.join(BASE, "tools"))
import n_sim as ns

J = json.load(open(os.path.join(BASE, "analysis", "screening.json")))
FIG = os.path.join(BASE, "figures")
os.makedirs(FIG, exist_ok=True)
MIB = 1024 ** 2
N_GRID = [1, 2, 4, 8, 16]


def rows(model, pred=None):
    rs = [r for r in J["e1_stationary"] if r["model"] == model]
    if pred:
        rs = [r for r in rs if pred(r)]
    return rs


# Fig 1/2: completion and normalized vs N by state
for fignum, key, ylab, fname in [
    (1, "times", "completion time (s)", "fig1_completion_vs_N_by_state.png"),
    (2, "normalized", "T(N) / T_ref(N)", "fig2_normalized_vs_N_by_state.png"),
]:
    fig, axes = plt.subplots(1, 2, figsize=(11, 4.6))
    ax = axes[0]
    for r in rows("degrade", lambda r: r["state_params"].get("gamma") == 0.05):
        ax.plot(N_GRID, [r[key][str(n)] for n in N_GRID], marker="o", ms=4,
                label=r["state"])
    ax.set_title("degrade (gamma=0.05): aggregate capacity states")
    ax = axes[1]
    for r in rows("cap"):
        ax.plot(N_GRID, [r[key][str(n)] for n in N_GRID], marker="s", ms=4, label=r["state"])
    for r in rows("fair"):
        ax.plot(N_GRID, [r[key][str(n)] for n in N_GRID], marker="^", ms=4, alpha=0.6,
                label="fair " + r["state"])
    ax.set_title("cap (C=8MB/s) and fair states")
    for ax in axes:
        ax.set_xlabel("concurrency N")
        ax.set_ylabel(ylab)
        ax.set_xticks(N_GRID)
        ax.grid(True, alpha=0.25)
        ax.legend(fontsize=6.5)
    fig.suptitle("Fig. %d — %s vs N by environmental state (analytical simulation)" % (
        fignum, "Completion time" if key == "times" else "Normalized completion"))
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, fname), dpi=160)
    plt.close(fig)

# Fig 3: preferred N vs state
fig, axes = plt.subplots(1, 2, figsize=(11, 4.6))
ax = axes[0]
for g, mk in [(0.01, "o"), (0.05, "s"), (0.1, "^")]:
    rs = sorted(rows("degrade", lambda r: r["state_params"].get("gamma") == g),
                key=lambda r: r["state_params"]["C"])
    ax.plot([r["state_params"]["C"] / MIB for r in rs], [r["argmin_N"] for r in rs],
            marker=mk, label="gamma=%.2f" % g)
for r in rows("fair"):
    pass
ax.set_xscale("log", base=2)
ax.set_xlabel("server aggregate capacity C (MB/s, log)")
ax.set_ylabel("preferred N (degrade model)")
ax.set_title("degrade: preferred N vs capacity")
ax.grid(True, alpha=0.25)
ax.legend(fontsize=8)
ax = axes[1]
rs = sorted(rows("cap"), key=lambda r: r["state_params"]["K"])
ax.plot([r["state_params"]["K"] for r in rs], [r["argmin_N"] for r in rs], marker="s", label="cap: preferred N vs K")
ax.plot(N_GRID, [16] * len(N_GRID), ls=":", color="gray", label="direct/fair: flat N=16")
ax.set_xlabel("server concurrency cap K (cap model)")
ax.set_ylabel("preferred N")
ax.set_xticks(N_GRID)
ax.set_title("cap: preferred N tracks the plateau edge")
ax.grid(True, alpha=0.25)
ax.legend(fontsize=8)
fig.suptitle("Fig. 3 — Preferred N as a function of environmental state")
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig3_preferred_N_by_state.png"), dpi=160)
plt.close(fig)

# Fig 4: one-download transition with N adaptation (degrade C 32 -> 1 MB/s)
p = {"D0": 0.04, "C": 32 * MIB, "K": 4, "gamma": 0.05, "R": 0.5 * MIB}
s1, s2 = {"C": 32 * MIB, "gamma": 0.05}, {"C": 1 * MIB, "gamma": 0.05}
t1 = 0.25 * 512 * MIB / (32 * MIB)
prof = ns.two_phase(s1, s2, t1)
F = 512 * MIB
n1, n2 = 4, 1
r1 = (32 * MIB / (1 + 0.05 * (n1 - 1))) / n1
r2 = (1 * MIB / (1 + 0.05 * (n1 - 1))) / n1
thr = (r1 * r2) ** 0.5


def policy(ctx):
    if len(ctx["samples"]) < 3:
        return None
    vals = sorted(ctx["samples"][-3:])
    med = vals[1]
    return n2 if (r1 > r2 and med < thr) else None


res = ns.simulate(F, 1 * MIB, n1, prof, "degrade", p, switch=policy)
fig, ax = plt.subplots(figsize=(10, 4.8))
sw = res["switch_info"]
t_sw = sw["time"] if sw else res["completion_seconds"]
ax.step([0, t_sw, t_sw, res["completion_seconds"]], [n1, n1, n2, n2], where="post",
        color="#1f77b4", lw=2, label="causal adaptive N")
ax.axvline(t1, color="k", ls="--", lw=1.2, label="state transition C 32->1 MB/s")
ax.axhline(n1, color="gray", ls=":", lw=1, label="fixed N=%d" % n1)
ax.axhline(n2, color="gray", ls="-.", lw=1, label="fixed N=%d" % n2)
ax.set_xlabel("time (s)")
ax.set_ylabel("concurrency N")
ax.set_yticks(N_GRID)
ax.set_title("Fig. 4 — One download with a server-capacity transition and a causal N switch")
ax.legend(fontsize=8)
ax.grid(True, alpha=0.25)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig4_transition_with_N_adaptation.png"), dpi=160)
plt.close(fig)

# Fig 5: fixed vs causal vs oracle (E2 cases)
fig, ax = plt.subplots(figsize=(11, 4.8))
cases = J["e2_transitions"]
xs = np.arange(len(cases))
ax.bar(xs - 0.2, [100 * r["causal_gain"] for r in cases], width=0.2, label="causal adaptive N", color="#1f77b4")
ax.bar(xs, [100 * r["oracle_gain"] for r in cases], width=0.2, label="oracle single switch (diagnostic)", color="#d62728")
ax.bar(xs + 0.2, [100 * r["stationary_causal_gain"] for r in cases], width=0.2, label="causal in stationary control", color="#2ca02c")
ax.axhline(0, color="k", lw=1)
ax.axhline(5, color="gray", ls=":", label="5% practical reference")
ax.set_xticks(xs)
ax.set_xticklabels([r["case"] for r in cases], rotation=25, ha="right", fontsize=7)
ax.set_ylabel("gain vs model-implied best fixed N (%)")
ax.set_title("Fig. 5 — Fixed vs causal adaptive N vs oracle N (within-download transitions)")
ax.legend(fontsize=7.5)
ax.grid(True, alpha=0.25, axis="y")
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig5_fixed_vs_causal_vs_oracle.png"), dpi=160)
plt.close(fig)

# Fig 6: causal headroom vs transition severity
fig, ax = plt.subplots(figsize=(9, 4.8))
sev = sorted(J["e4_severity"], key=lambda r: r["ratio"])
ax.plot([r["ratio"] for r in sev], [100 * r["causal_gain"] for r in sev], marker="o", label="causal adaptive N")
ax.plot([r["ratio"] for r in sev], [100 * r["oracle_gain"] for r in sev], marker="s", ls="--", label="oracle single switch")
ax.axhline(0, color="k", lw=1)
ax.axhline(5, color="gray", ls=":", label="5% practical reference")
ax.set_xscale("log", base=2)
ax.set_xticks([2, 4, 8, 16, 32])
ax.set_xticklabels(["2", "4", "8", "16", "32"])
ax.set_xlabel("capacity-drop severity (C_high / C_low, log)")
ax.set_ylabel("gain vs model-implied best fixed N (%)")
ax.set_title("Fig. 6 — Causal headroom vs transition severity (degrade model, analytical)")
ax.legend(fontsize=8)
ax.grid(True, alpha=0.25)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig6_causal_headroom_vs_severity.png"), dpi=160)
plt.close(fig)

# Fig 7: stationary vs changing headroom
fig, ax = plt.subplots(figsize=(9, 4.8))
ch = [100 * r["causal_gain"] for r in J["e2_transitions"]] + [100 * r["causal_gain"] for r in J["e4_severity"]]
st = [100 * r["stationary_causal_gain"] for r in J["e2_transitions"]] + [0.0] * len(J["e4_severity"])
ax.scatter(st, ch, color="#d62728", zorder=3)
ax.axhline(0, color="k", lw=1)
ax.axvline(0, color="k", lw=1)
ax.axhline(5, color="gray", ls=":", label="5% practical reference")
ax.set_xlabel("causal gain in stationary control (%)")
ax.set_ylabel("causal gain in changing environment (%)")
ax.set_title("Fig. 7 — Stationary vs changing: causal N gains (all cases)")
ax.legend(fontsize=8)
ax.grid(True, alpha=0.25)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig7_stationary_vs_changing.png"), dpi=160)
plt.close(fig)

# Fig 8: parameter-robustness heatmap of preferred N (degrade)
cs = sorted({r["C"] for r in J["e3_robustness"]})
gs = sorted({r["gamma"] for r in J["e3_robustness"]})
grid = np.full((len(gs), len(cs)), np.nan)
for r in J["e3_robustness"]:
    i = gs.index(r["gamma"])
    j = cs.index(r["C"])
    grid[i, j] = r["argmin_N"]
fig, ax = plt.subplots(figsize=(8, 4.8))
im = ax.imshow(grid, cmap="viridis", aspect="auto", origin="lower")
ax.set_xticks(range(len(cs)))
ax.set_xticklabels(["%.2f" % (c / MIB) for c in cs])
ax.set_yticks(range(len(gs)))
ax.set_yticklabels(["%.3f" % g for g in gs])
for i in range(len(gs)):
    for j in range(len(cs)):
        ax.text(j, i, "%d" % grid[i, j], ha="center", va="center", color="white", fontsize=8)
ax.set_xlabel("server aggregate capacity C (MB/s)")
ax.set_ylabel("per-connection overhead gamma")
ax.set_title("Fig. 8 — Preferred N robustness (degrade model): preferred N over C x gamma")
fig.colorbar(im, ax=ax, label="preferred N")
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig8_parameter_robustness_heatmap.png"), dpi=160)
plt.close(fig)

print("wrote 8 figures to", FIG)
for f in sorted(os.listdir(FIG)):
    print("  ", f, os.path.getsize(os.path.join(FIG, f)))
