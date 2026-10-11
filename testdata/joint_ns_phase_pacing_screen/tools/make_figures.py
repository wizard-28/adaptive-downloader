#!/usr/bin/env python3
"""Analytical figures for the joint N+S phase-locked pacing screen (simulation only)."""
import json
import os
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

BASE = "/workspaces/mvp/testdata/joint_ns_phase_pacing_screen"
sys.path.insert(0, os.path.join(BASE, "tools"))
import jns_sim as sim  # noqa: E402

J = json.load(open(os.path.join(BASE, "analysis", "screen.json")))
CAL = json.load(open(os.path.join(BASE, "analysis", "calibration.json")))
FIG = os.path.join(BASE, "figures")
os.makedirs(FIG, exist_ok=True)
MIB = 1024 ** 2
KIB = 1024
FAMILY = ["T=1s", "T=2s", "T=4s", "T=8s"]
PARAMS = sim.Params(**CAL["params"])

plt.rcParams.update({"font.size": 9, "axes.titlesize": 10, "figure.dpi": 130})


def sched_of(k):
    return sim.two_phase(float(k.split("=")[1].rstrip("s")))


# ---------------- Fig 1: fixed-policy surfaces ----------------
fig, axes = plt.subplots(1, 4, figsize=(15, 3.6))
for ax, k in zip(axes, FAMILY):
    fr = J["family"][k]["frontier"]
    M = np.array([[fr["%d/%d" % (n, s)] for s in sim.S_GRID] for n in sim.N_GRID], dtype=float)
    im = ax.imshow(np.log10(M), aspect="auto", cmap="viridis")
    b = J["family"][k]["best_fixed"]
    j = sim.S_GRID.index(b["S"])
    i = sim.N_GRID.index(b["N"])
    ax.scatter([j], [i], marker="*", s=180, c="red", edgecolors="white", zorder=5)
    ax.set_xticks(range(len(sim.S_GRID)))
    ax.set_xticklabels(["256K", "1M", "4M", "16M", "32M"], rotation=45)
    ax.set_yticks(range(len(sim.N_GRID)))
    ax.set_yticklabels(sim.N_GRID)
    ax.set_title("%s  (best %.2fs)" % (k, b["completion"]))
    ax.set_xlabel("S")
    if ax is axes[0]:
        ax.set_ylabel("N")
    for ii in range(len(sim.N_GRID)):
        for jj in range(len(sim.S_GRID)):
            ax.text(jj, ii, "%.0f" % M[ii, jj], ha="center", va="center", fontsize=6,
                    color="white" if np.log10(M[ii, jj]) < 1.35 else "black")
fig.colorbar(im, ax=axes, shrink=0.8, label="log10 completion (s)")
fig.suptitle("Fig 1 - fixed-policy completion surface over (N,S) per pacing regime (analytical)")
fig.savefig(os.path.join(FIG, "fig1_fixed_surface.png"), bbox_inches="tight")
plt.close(fig)

# ---------------- Fig 2: fixed optima + wrong-config penalty ----------------
fig, axes = plt.subplots(1, 2, figsize=(11, 3.8))
ax = axes[0]
xs = np.arange(len(FAMILY))
sstar = [J["family"][k]["best_fixed"]["S"] / MIB for k in FAMILY]
ax.plot(xs, sstar, marker="o", color="tab:blue")
for i, k in enumerate(FAMILY):
    b = J["family"][k]["best_fixed"]
    ax.annotate("N=%d\n%.2fs" % (b["N"], b["completion"]), (i, sstar[i]),
                textcoords="offset points", xytext=(0, 10), ha="center", fontsize=8)
ax.set_xticks(xs)
ax.set_xticklabels(FAMILY)
ax.set_ylabel("optimal S (MiB)")
ax.set_title("Fig 2a - per-regime fixed optimum (N*=16 always)")
ax.grid(alpha=0.3)

ax = axes[1]
pen = J["wrong_config_penalty"]
M = np.array([[100 * (pen[a][b] if pen[a][b] is not None else np.nan) for b in FAMILY] for a in FAMILY])
im = ax.imshow(M, cmap="Reds", vmin=0)
for i in range(4):
    for j in range(4):
        ax.text(j, i, "%.1f%%" % M[i, j], ha="center", va="center", fontsize=8)
ax.set_xticks(range(4))
ax.set_xticklabels(FAMILY, rotation=30)
ax.set_yticks(range(4))
ax.set_yticklabels(FAMILY)
ax.set_xlabel("evaluated regime")
ax.set_ylabel("config from regime")
ax.set_title("Fig 2b - wrong-config penalty (best fixed of A used in B)")
fig.colorbar(im, ax=ax, shrink=0.85, label="% worse")
fig.savefig(os.path.join(FIG, "fig2_fixed_optimum.png"), bbox_inches="tight")
plt.close(fig)


# ---------------- helper: trajectories ----------------
def one_switch_traj(k, target=(16, 16 * MIB)):
    ctrl = sim.make_one_switch_controller(3, 1.5, target)
    return sim.simulate(sim.FILE_SIZE, sim.START_N, sim.START_S, sched_of(k), PARAMS,
                        controller=ctrl, keep_requests=True)


def probe_traj(k):
    state = {"step": 0, "baseline": None}

    def ctrl(ctx):
        s = ctx["samples"]
        if state["step"] == 0:
            state["step"] = 1
            return (16, 256 * KIB)
        if state["baseline"] is None and len(s) >= 3:
            state["baseline"] = np.median(s[:3])
            return None
        if state["step"] == 1 and state["baseline"] is not None and len(s) >= 4:
            if np.median(s[-3:]) >= 1.5 * state["baseline"]:
                state["step"] = 2
                return (16, 16 * MIB)
        return None

    return sim.simulate(sim.FILE_SIZE, sim.START_N, sim.START_S, sched_of(k), PARAMS,
                        controller=ctrl, keep_requests=True)


# ---------------- Fig 3: adaptive trajectories ----------------
fig, axes = plt.subplots(1, 2, figsize=(11, 3.8))
for ax, k in zip(axes, ["T=1s", "T=8s"]):
    pr = probe_traj(k)
    sw = one_switch_traj(k)
    for r, lab, c in [(pr, "probe (diagnostic)", "tab:orange"), (sw, "one-switch (primary)", "tab:blue")]:
        ts = [0.0] + [s["time"] for s in r["switches"]] + [r["completion"]]
        Ns = [sim.START_N] + [s["to_N"] for s in r["switches"]] + [r["final_N"]]
        Ss = [sim.START_S / MIB] + [s["to_S"] / MIB for s in r["switches"]] + [r["final_S"] / MIB]
        ax.step(ts, Ns, where="post", color=c, label=lab + " N")
        ax.step(ts, Ss, where="post", color=c, ls="--", label=lab + " S (MiB)")
    T = float(k.split("=")[1].rstrip("s"))
    ax.axvline(T, color="grey", ls=":", label="transition T")
    ax.set_xlabel("time (s)")
    ax.set_title("Fig 3 - adaptive trajectories, %s" % k)
    ax.grid(alpha=0.3)
axes[0].legend(fontsize=6.5)
fig.savefig(os.path.join(FIG, "fig3_adaptive_trajectory.png"), bbox_inches="tight")
plt.close(fig)

# ---------------- Fig 4: throughput vs pacing ----------------
fig, axes = plt.subplots(1, 2, figsize=(11, 3.8))
for ax, k in zip(axes, ["T=1s", "T=8s"]):
    pr = probe_traj(k)
    sched = sched_of(k)
    T = float(k.split("=")[1].rstrip("s"))
    reqs = [r for r in pr["requests"] if not r.get("aborted")]
    ax.scatter([r["end"] for r in reqs], [r["size"] / (r["end"] - r["start"] + PARAMS.h) / MIB
                                          for r in reqs], s=8, label="observed request throughput")
    ax.axhline(sim.FAMILY_SLOW / MIB, color="tab:red", ls="--", lw=1, label="slow 640 KiB/s")
    ax.axhline(sim.FAMILY_FAST / MIB, color="tab:green", ls="--", lw=1, label="fast 2500 KiB/s")
    ax.axvline(T, color="grey", ls=":", label="transition T")
    ax.set_xlabel("server time (s)")
    ax.set_ylabel("MiB/s")
    ax.set_title("Fig 4 - observed throughput vs pacing, %s" % k)
    ax.legend(fontsize=6.5)
    ax.grid(alpha=0.3)
fig.savefig(os.path.join(FIG, "fig4_throughput_pacing.png"), bbox_inches="tight")
plt.close(fig)

# ---------------- Fig 5: fixed vs adaptive vs aria2 ----------------
fig, ax = plt.subplots(figsize=(9, 4))
w = 0.17
xs = np.arange(len(FAMILY))
series = {
    "best fixed": [J["family"][k]["best_fixed"]["completion"] for k in FAMILY],
    "action-space fixed": [J["family"][k]["action_space_best"]["completion"] for k in FAMILY],
    "one-switch (best)": [min(c["completion"] for c in J["family"][k]["causal"].values()) for k in FAMILY],
    "probe (diagnostic)": [J["family"][k]["probe_variants"]["joint_16_16M"]["completion"] for k in FAMILY],
    "aria2 (fixed)": [J["family"][k]["aria2"] for k in FAMILY],
}
for i, (lab, vals) in enumerate(series.items()):
    ax.bar(xs + (i - 2) * w, vals, width=w, label=lab)
ax.set_xticks(xs)
ax.set_xticklabels(FAMILY)
ax.set_ylabel("completion (s)")
ax.set_title("Fig 5 - fixed vs adaptive vs aria2 per pacing regime (analytical)")
ax.legend(fontsize=7.5)
ax.grid(alpha=0.3, axis="y")
fig.savefig(os.path.join(FIG, "fig5_fixed_vs_adaptive_vs_aria2.png"), bbox_inches="tight")
plt.close(fig)

# ---------------- Fig 6: headroom ----------------
fig, ax = plt.subplots(figsize=(9, 4))
series = {
    "one-switch (best)": [max(c["gain_vs_best_fixed"] for c in J["family"][k]["causal"].values()) for k in FAMILY],
    "probe (diagnostic)": [J["family"][k]["probe_variants"]["joint_16_16M"]["gain_vs_best_fixed"] for k in FAMILY],
    "blind ramp (oracle-tuned)": [J["family"][k]["blind_ramp_best"][1] for k in FAMILY],
    "optimistic causal bound": [(J["family"][k]["best_fixed"]["completion"] - J["family"][k]["optimistic_causal_bound"]["completion"]) / J["family"][k]["best_fixed"]["completion"] for k in FAMILY],
    "clairvoyant oracle": [J["family"][k]["oracle_unconstrained"]["gain_vs_best_fixed"] for k in FAMILY],
}
xs = np.arange(len(FAMILY))
w = 0.15
for i, (lab, vals) in enumerate(series.items()):
    ax.bar(xs + (i - 2) * w, [100 * v for v in vals], width=w, label=lab)
ax.axhline(5, color="red", ls="--", lw=1, label="5% materiality gate")
ax.axhline(0, color="black", lw=0.8)
ax.set_xticks(xs)
ax.set_xticklabels(FAMILY)
ax.set_ylabel("gain vs per-regime best fixed (%)")
ax.set_title("Fig 6 - adaptive vs best-fixed headroom (negative: no causal capture)")
ax.legend(fontsize=7.5)
ax.grid(alpha=0.3, axis="y")
fig.savefig(os.path.join(FIG, "fig6_headroom.png"), bbox_inches="tight")
plt.close(fig)

# ---------------- Fig 7: causal vs oracle ----------------
fig, ax = plt.subplots(figsize=(9, 4))
series = {
    "probe (causal)": [J["family"][k]["probe_variants"]["joint_16_16M"]["gain_vs_best_fixed"] for k in FAMILY],
    "optimistic causal bound": [(J["family"][k]["best_fixed"]["completion"] - J["family"][k]["optimistic_causal_bound"]["completion"]) / J["family"][k]["best_fixed"]["completion"] for k in FAMILY],
    "same-start oracle": [J["family"][k]["oracle_same_start"]["gain_vs_best_fixed"] for k in FAMILY],
    "unconstrained oracle": [J["family"][k]["oracle_unconstrained"]["gain_vs_best_fixed"] for k in FAMILY],
}
for i, (lab, vals) in enumerate(series.items()):
    ax.plot(xs, [100 * v for v in vals], marker="o", label=lab)
ax.axhline(5, color="red", ls="--", lw=1, label="5% materiality gate")
ax.axhline(0, color="black", lw=0.8)
ax.set_xticks(xs)
ax.set_xticklabels(FAMILY)
ax.set_ylabel("gain vs best fixed (%)")
ax.set_title("Fig 7 - causal capture vs oracle headroom (oracle never deployable)")
ax.legend(fontsize=7.5)
ax.grid(alpha=0.3)
fig.savefig(os.path.join(FIG, "fig7_causal_vs_oracle.png"), bbox_inches="tight")
plt.close(fig)

# ---------------- Fig 8: stationary controls ----------------
fig, axes = plt.subplots(1, 2, figsize=(11, 3.8))
for ax, name in zip(axes, ["constant_640KiB", "constant_2500KiB"]):
    st = J["stationary"][name]
    vals = [st["start_config_completion"], st["best_fixed"]["completion"]]
    # causal controllers make no switch -> their completion equals the start config
    causal_vals = [c["completion"] for c in st["causal"].values()]
    labels = ["start (4,4M)", "best fixed (%d,%.0fM)" % (st["best_fixed"]["N"], st["best_fixed"]["S"] / MIB),
              "causal controllers (no switch)"]
    ax.bar([0, 1, 2], [vals[0], vals[1], causal_vals[0]], color=["tab:grey", "tab:green", "tab:orange"])
    ax.set_xticks([0, 1, 2])
    ax.set_xticklabels(labels, rotation=12, fontsize=7.5)
    ax.set_ylabel("completion (s)")
    ax.set_title("Fig 8 - stationary control: %s (no spurious adaptation gain)" % name)
    ax.grid(alpha=0.3, axis="y")
fig.savefig(os.path.join(FIG, "fig8_stationary_control.png"), bbox_inches="tight")
plt.close(fig)

print("figures written to", FIG)
