#!/usr/bin/env python3
"""Final paper figure set (synthesis of existing evidence only; no new experiments).

Every figure carries its evidence type: schematic, LIVE EXPERIMENTAL,
TRACE REPLAY, DETERMINISTIC ANALYTICAL MODEL, or DIAGNOSTIC.
"""
import json
import os

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.patches import FancyBboxPatch, FancyArrowPatch
import numpy as np

S0S = "/workspaces/mvp/testdata/salvage_s_stage0s"
SCALE = "/workspaces/mvp/testdata/salvage_s_scaling_analysis"
PIVOT = "/workspaces/mvp/testdata/salvage_s_mechanism_pivot"
NSCREEN = "/workspaces/mvp/testdata/adaptive_n_mechanism_screen"
FINAL = "/workspaces/mvp/testdata/final_evaluation"
OUT = "/workspaces/mvp/testdata/final_project_synthesis/figures"
MIB = 1024 ** 2
os.makedirs(OUT, exist_ok=True)

# ---------------- Fig 1: system / methodology (schematic) ----------------
fig, ax = plt.subplots(figsize=(11, 5.2))
ax.axis("off")
boxes = [
    (0.02, 0.62, "RangeTracker\nrange allocation\n(future-only S)"),
    (0.22, 0.62, "Worker pool\nN concurrent\nrange requests"),
    (0.42, 0.62, "Fetcher\none contiguous\nrange / request"),
    (0.62, 0.62, "Assembler\nrandom-access\nwrites"),
    (0.82, 0.62, "Metrics\nattempt records\n+ aggregates"),
    (0.22, 0.18, "Controller loop\nconcurrency N\nchunk size S"),
    (0.52, 0.18, "Policy interface\n(deterministic /\nRL adapter)"),
    (0.82, 0.18, "Controlled server\nper-request pacing,\nfixed RTT, traces"),
]
for x, y, t in boxes:
    ax.add_patch(FancyBboxPatch((x, y), 0.16, 0.24, boxstyle="round,pad=0.02",
                                fc="#eef3fb", ec="#33517a", lw=1.4))
    ax.text(x + 0.08, y + 0.12, t, ha="center", va="center", fontsize=7.5)
arrows = [(0.18, 0.74, 0.22, 0.74), (0.38, 0.74, 0.42, 0.74), (0.58, 0.74, 0.62, 0.74),
          (0.78, 0.74, 0.82, 0.74), (0.30, 0.62, 0.30, 0.42), (0.38, 0.30, 0.52, 0.30),
          (0.68, 0.30, 0.82, 0.30), (0.90, 0.42, 0.90, 0.62)]
for x1, y1, x2, y2 in arrows:
    ax.add_patch(FancyArrowPatch((x1, y1), (x2, y2), arrowstyle="-|>", mutation_scale=12,
                                 color="#33517a", lw=1.2))
ax.text(0.5, 0.95, "Fig. 1 — Adaptive downloader architecture and measurement loop (schematic)",
        ha="center", fontsize=11)
ax.text(0.5, 0.04, "future-only chunk resizing; in-flight requests are never resized; "
                   "one decision at a time (serialized policy path)",
        ha="center", fontsize=8, style="italic")
fig.tight_layout()
fig.savefig(os.path.join(OUT, "fig1_system_methodology.png"), dpi=170)
plt.close(fig)

# ---------------- Fig 2: Stage 0S mechanism validation (LIVE) ----------------
pa = json.load(open(os.path.join(S0S, "analysis", "per_arm.json")))
arms = ["256KiB", "1MiB", "4MiB", "16MiB"]
obs = [pa[a]["pooled_median_server_service_seconds"] for a in arms]
exp = [pa[a]["expected_service_seconds"] for a in arms]
x = np.arange(len(arms))
fig, ax = plt.subplots(figsize=(8, 4.6))
ax.bar(x, obs, width=0.5, color="#4c72b0", label="Stage 0S pooled median (LIVE)")
ax.plot(x, exp, "k--o", ms=5, lw=1.6, label="analytical D + s/R")
for i, (o, e) in enumerate(zip(obs, exp)):
    ax.text(i, o + 0.6, "%+.2f%%" % (100 * (o - e) / e), ha="center", fontsize=8)
ax.set_xticks(x)
ax.set_xticklabels(arms)
ax.set_ylabel("per-request service time (s)")
ax.set_title("Fig. 2 — Mechanism validation (LIVE EXPERIMENTAL): service time vs chunk size")
ax.legend(fontsize=8)
ax.grid(True, alpha=0.25, axis="y")
fig.tight_layout()
fig.savefig(os.path.join(OUT, "fig2_stage0s_mechanism_validation.png"), dpi=170)
plt.close(fig)

# ---------------- Fig 3: S headroom closure (ANALYTICAL) ----------------
sc = json.load(open(os.path.join(SCALE, "analysis", "scaling_analysis.json")))
sizes = sc["file_size_grid"]
strats = ["16MiB->4MiB", "4MiB->1MiB", "1MiB->256KiB"]
fig, ax = plt.subplots(figsize=(9, 4.8))
for s in strats:
    rs = [r for r in sc["switch_rows"] if r["model"] == "MODEL_A_analytical" and r["strategy"] == s]
    rs.sort(key=lambda r: r["file_size_bytes"])
    ax.plot([r["file_size_bytes"] for r in rs], [100 * r["relative_gain"] for r in rs],
            marker="o", ms=4, label="frozen %s" % s)
dense = sorted([r for r in sc["dense_modular"] if r["model"] == "MODEL_A_analytical"
                and r["best_16to4_envelope_gain"] is not None], key=lambda r: r["file_size_bytes"])
ax.plot([r["file_size_bytes"] for r in dense], [100 * r["best_16to4_envelope_gain"] for r in dense],
        color="black", lw=1.2, marker=".", ms=4, label="EXPLORATORY single-switch upper envelope (16->4)")
ax.axhline(0, color="k", lw=1)
ax.axhline(5, color="gray", ls=":", lw=1.5, label="5% practical threshold")
ax.set_xscale("log", base=2)
ax.set_xticks(sizes)
ax.set_xticklabels([("%dGiB" % (f // (1024 ** 3))) if f % (1024 ** 3) == 0 else ("%dMiB" % (f // MIB)) for f in sizes],
                   rotation=45, ha="right")
ax.set_ylabel("relative gain vs model-implied best fixed (%)")
ax.set_title("Fig. 3 — S headroom closure (DETERMINISTIC ANALYTICAL MODEL)")
ax.legend(fontsize=7.5)
ax.grid(True, alpha=0.25)
fig.tight_layout()
fig.savefig(os.path.join(OUT, "fig3_s_headroom_closure.png"), dpi=170)
plt.close(fig)

# ---------------- Fig 4: absolute vs relative + tail share (ANALYTICAL) ----------------
fig, axes = plt.subplots(1, 2, figsize=(11, 4.6))
ax = axes[0]
for s in strats:
    rs = [r for r in sc["switch_rows"] if r["model"] == "MODEL_A_analytical" and r["strategy"] == s]
    rs.sort(key=lambda r: r["file_size_bytes"])
    ax.plot([r["file_size_bytes"] for r in rs], [r["absolute_time_saved_seconds"] for r in rs],
            marker="o", ms=4, label=s)
ax.axhline(0, color="k", lw=1)
ax.set_xscale("log", base=2)
ax.set_xticks(sizes)
ax.set_xticklabels(["%d" % (f // MIB) for f in sizes], rotation=45, ha="right")
ax.set_xlabel("file size (MiB, log)")
ax.set_ylabel("absolute time saved vs best fixed (s)")
ax.set_title("absolute difference")
ax.grid(True, alpha=0.25)
ax.legend(fontsize=7)
ax = axes[1]
q = sc["protocol"]["frozen_quantities"]
params_B = sc["models"]["MODEL_B_stage0s_calibrated"]["params"]
t_ref = [f / (4 * 500000.0) for f in sizes]
ax.plot(sizes, [100 * q["T_tail_primary_seconds"] / t for t in t_ref], marker="o", ms=4,
        label="tail-window share of T_ref (Model A)")
ax.plot(sizes, [100 * (params_B["a_seconds"] + params_B["b_seconds_per_byte"] * 16 * MIB) / t for t in t_ref],
        marker="s", ms=4, ls="--", label="tail-window share (Model B)")
ax.plot(sizes, [100 * (0.040 * 500000.0) / (16 * MIB)] * len(sizes), ls=":", lw=1.5,
        label="per-request overhead share, 16MiB arm (scale-invariant)")
ax.set_xscale("log", base=2)
ax.set_xticks(sizes)
ax.set_xticklabels(["%d" % (f // MIB) for f in sizes], rotation=45, ha="right")
ax.set_xlabel("file size (MiB, log)")
ax.set_ylabel("share of idealized completion (%)")
ax.set_title("bulk/tail decomposition")
ax.grid(True, alpha=0.25)
ax.legend(fontsize=7)
fig.suptitle("Fig. 4 — Why longer downloads do not rescue S (DETERMINISTIC ANALYTICAL MODEL)")
fig.tight_layout()
fig.savefig(os.path.join(OUT, "fig4_absolute_vs_relative.png"), dpi=170)
plt.close(fig)

# ---------------- Fig 5: S mechanism screening (ANALYTICAL) ----------------
pv = json.load(open(os.path.join(PIVOT, "analysis", "screening.json")))
fig, axes = plt.subplots(1, 2, figsize=(11, 4.6))
ax = axes[0]
arm_y = {"256KiB": 0, "1MiB": 1, "4MiB": 2, "16MiB": 3}
for model in ["shared", "lockin_full", "serialized"]:
    rows = sorted([r for r in pv["e1_stationary"] if r["model"] == model and r["file_size"] == "512MiB"],
                  key=lambda r: r["aggregate_rate_bytes_per_s"])
    ax.plot([r["aggregate_rate_bytes_per_s"] / MIB for r in rows], [arm_y[r["argmin"]] for r in rows],
            marker="o", ms=4, label=model)
ax.set_yticks(range(4))
ax.set_yticklabels(list(arm_y))
ax.set_xscale("log", base=2)
ax.set_xlabel("aggregate rate (MB/s, log)")
ax.set_ylabel("preferred S")
ax.set_title("preferred S is flat across stationary states")
ax.grid(True, alpha=0.25)
ax.legend(fontsize=7)
ax = axes[1]
cases = [r for r in pv["e2_transitions"] if r["case"] != "stable_to_volatile"]
xs = np.arange(len(cases))
ax.bar(xs - 0.2, [100 * r["causal_gain"] for r in cases], width=0.2, label="causal", color="#1f77b4")
ax.bar(xs, [100 * r["oracle_gain"] for r in cases], width=0.2, label="oracle (diagnostic)", color="#d62728")
ax.bar(xs + 0.2, [100 * r["stationary_causal_gain"] for r in cases], width=0.2, label="stationary control", color="#2ca02c")
ax.axhline(0, color="k", lw=1)
ax.axhline(5, color="gray", ls=":", label="5% reference")
ax.set_xticks(xs)
ax.set_xticklabels([r["case"] for r in cases], rotation=30, ha="right", fontsize=6.5)
ax.set_ylabel("gain (%)")
ax.set_title("changing-state gains: causal zero/harmful")
ax.grid(True, alpha=0.25, axis="y")
ax.legend(fontsize=6.5)
fig.suptitle("Fig. 5 — S mechanism screening (DETERMINISTIC ANALYTICAL MODEL)")
fig.tight_layout()
fig.savefig(os.path.join(OUT, "fig5_s_mechanism_screening.png"), dpi=170)
plt.close(fig)

# ---------------- Fig 6: N mechanism screening (ANALYTICAL + live boundary) ----------------
nv = json.load(open(os.path.join(NSCREEN, "analysis", "screening.json")))
fig, axes = plt.subplots(1, 2, figsize=(11, 4.6))
ax = axes[0]
for g, mk in [(0.01, "o"), (0.05, "s"), (0.1, "^")]:
    rows = sorted([r for r in nv["e1_stationary"] if r["model"] == "degrade"
                   and r["state_params"].get("gamma") == g], key=lambda r: r["state_params"]["C"])
    ax.plot([r["state_params"]["C"] / MIB for r in rows], [r["argmin_N"] for r in rows],
            marker=mk, label="degrade gamma=%.2f" % g)
ax.plot([0.5, 32], [16, 16], ls=":", color="gray", label="direct/fair: N=16 (monotone)")
ax.set_xscale("log", base=2)
ax.set_xlabel("server aggregate capacity C (MB/s, log)")
ax.set_ylabel("preferred N")
ax.set_title("preferred N rises with C (analytical only)")
ax.grid(True, alpha=0.25)
ax.legend(fontsize=7)
ax = axes[1]
cases = nv["e2_transitions"] + nv["e4_severity"]
xs = np.arange(len(cases))
ax.bar(xs - 0.2, [100 * r["causal_gain"] for r in cases], width=0.2, label="causal", color="#1f77b4")
ax.bar(xs, [100 * r["oracle_gain"] for r in cases], width=0.2, label="oracle (diagnostic)", color="#d62728")
ax.bar(xs + 0.2, [100 * r["stationary_causal_gain"] for r in nv["e2_transitions"]] + [0] * len(nv["e4_severity"]),
       width=0.2, label="stationary control", color="#2ca02c")
ax.axhline(0, color="k", lw=1)
ax.axhline(5, color="gray", ls=":", label="5% reference")
ax.set_xticks(xs)
ax.set_xticklabels([r.get("case", "drop ratio %s" % r.get("ratio")) for r in cases], rotation=30, ha="right", fontsize=6)
ax.set_ylabel("gain (%)")
ax.set_title("causal gains up to +6.7% at severe drops (analytical)")
ax.grid(True, alpha=0.25, axis="y")
ax.legend(fontsize=6.5)
fig.suptitle("Fig. 6 — N mechanism screening (DETERMINISTIC ANALYTICAL MODEL; "
             "NOT LIVE-GROUNDED: L4 GATE E live mismatch, T1 monotone)", fontsize=9.5)
fig.tight_layout()
fig.savefig(os.path.join(OUT, "fig6_n_mechanism_screening.png"), dpi=170)
plt.close(fig)

# ---------------- Fig 7: practical baseline evidence (LIVE) ----------------
agg = json.load(open(os.path.join(FINAL, "statistical_analysis.json")))["aggregate"]
fig, axes = plt.subplots(1, 2, figsize=(11, 4.6))
ax = axes[0]
labels = ["aria2\n(config v1)", "matched internal\n1 MiB / 4C"]
vals = [2129.7, 2055.7]
ax.bar(labels, vals, width=0.5, color=["#dd8452", "#4c72b0"])
for i, v in enumerate(vals):
    ax.text(i, v + 15, "%.1f ms" % v, ha="center", fontsize=9)
ax.set_ylabel("mean completion time (ms)")
ax.set_title("wall-clock granularity-matched control (n=10)")
ax.grid(True, alpha=0.25, axis="y")
ax = axes[1]
ax.bar([0, 1], [agg["dqn_mean_ms"], agg["aria2_mean_ms"]], width=0.5, color=["#c44e52", "#55a868"])
ax.text(0, agg["dqn_mean_ms"] + 30, "%.0f ms" % agg["dqn_mean_ms"], ha="center", fontsize=9)
ax.text(1, agg["aria2_mean_ms"] + 30, "%.0f ms" % agg["aria2_mean_ms"], ha="center", fontsize=9)
ax.set_xticks([0, 1])
ax.set_xticklabels(["DQN v2", "aria2 v1"])
ax.set_ylabel("mean completion time (ms)")
ax.set_title("final head-to-head (240 pairs): ratio %.3f [%.2f, %.2f]"
             % (agg["geometric_ratio"], agg["ratio_ci95"][0], agg["ratio_ci95"][1]))
ax.grid(True, alpha=0.25, axis="y")
fig.suptitle("Fig. 7 — Practical baseline evidence (LIVE EXPERIMENTAL; tested scenarios only; no ranking)")
fig.tight_layout()
fig.savefig(os.path.join(OUT, "fig7_practical_baseline_evidence.png"), dpi=170)
plt.close(fig)

print("wrote final figures to", OUT)
for f in sorted(os.listdir(OUT)):
    print("  ", f, os.path.getsize(os.path.join(OUT, f)))
