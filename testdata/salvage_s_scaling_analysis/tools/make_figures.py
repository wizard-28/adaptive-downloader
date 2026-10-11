#!/usr/bin/env python3
"""Generate the seven Stage-1-planning figures from the deterministic analysis.

Honesty rules: analytical (Model A), Stage 0S-calibrated (Model B), and Stage 0S
empirical observations are visually distinct; the single-switch envelope is
labelled EXPLORATORY; no uncertainty is fabricated for the deterministic model.
"""
import json
import os

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

BASE = "/workspaces/mvp/testdata/salvage_s_scaling_analysis"
J = json.load(open(os.path.join(BASE, "analysis", "scaling_analysis.json")))
FIG = os.path.join(BASE, "figures")
os.makedirs(FIG, exist_ok=True)

MIB, GIB = 1024 ** 2, 1024 ** 3
ARMS = ["256KiB", "1MiB", "4MiB", "16MiB"]
COLORS = {"256KiB": "#1f77b4", "1MiB": "#ff7f0e", "4MiB": "#2ca02c", "16MiB": "#d62728"}
MODEL_STYLE = {"MODEL_A_analytical": {"ls": "-", "marker": "o", "label": "analytical (Model A)"},
               "MODEL_B_stage0s_calibrated": {"ls": "--", "marker": "s", "label": "Stage 0S-calibrated (Model B)"}}
STRATS = ["16MiB->4MiB", "4MiB->1MiB", "1MiB->256KiB"]
STRAT_COLORS = {"16MiB->4MiB": "#d62728", "4MiB->1MiB": "#2ca02c", "1MiB->256KiB": "#1f77b4"}


def fmt_x(F):
    if F % GIB == 0:
        return "%dGiB" % (F // GIB)
    return "%dMiB" % (F // MIB)


def fixed_series(model, arm):
    rows = [r for r in J["fixed_rows"] if r["model"] == model and r["arm"] == arm]
    rows.sort(key=lambda r: r["file_size_bytes"])
    return [r["file_size_bytes"] for r in rows], [r["completion_seconds"] for r in rows], \
        [r["normalized_T_over_Tref"] for r in rows]


def switch_series(model, strat, key):
    rows = [r for r in J["switch_rows"] if r["model"] == model and r["strategy"] == strat]
    rows.sort(key=lambda r: r["file_size_bytes"])
    return [r["file_size_bytes"] for r in rows], [r[key] for r in rows]


def xaxis(ax, sizes):
    ax.set_xscale("log", base=2)
    ax.set_xticks(sizes)
    ax.set_xticklabels([fmt_x(f) for f in sizes], rotation=45, ha="right")
    ax.grid(True, which="both", alpha=0.25)


# Figure 1: completion time vs file size
fig, ax = plt.subplots(figsize=(9, 5.5))
sizes = J["file_size_grid"]
for model, st in MODEL_STYLE.items():
    for arm in ARMS:
        x, y, _ = fixed_series(model, arm)
        ax.plot(x, y, color=COLORS[arm], ls=st["ls"], marker=st["marker"], ms=4, lw=1.6,
                label="%s — %s" % (arm, st["label"]))
ax.plot(sizes, [f / (4 * 500000.0) for f in sizes], color="k", ls=":", lw=1.4, label="T_ref(F) = F/(N*R)")
xaxis(ax, sizes)
ax.set_ylabel("modelled completion time (s)")
ax.set_xlabel("file size (log scale)")
ax.set_title("Fig. 1 — Completion time vs file size (analytical simulation; no downloads)")
ax.legend(fontsize=7.5, ncol=2)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig1_completion_vs_filesize.png"), dpi=160)
plt.close(fig)

# Figure 2: normalized completion time
fig, ax = plt.subplots(figsize=(9, 5.5))
for model, st in MODEL_STYLE.items():
    for arm in ARMS:
        x, _, y = fixed_series(model, arm)
        ax.plot(x, y, color=COLORS[arm], ls=st["ls"], marker=st["marker"], ms=4, lw=1.6,
                label="%s — %s" % (arm, st["label"]))
ax.axhline(1.0, color="k", ls=":", lw=1.2, label="T_ref")
xaxis(ax, sizes)
ax.set_ylabel("T_S(F) / T_ref(F)")
ax.set_xlabel("file size (log scale)")
ax.set_title("Fig. 2 — Normalized completion time vs file size (fixed arms)")
ax.legend(fontsize=7.5, ncol=2)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig2_normalized_vs_filesize.png"), dpi=160)
plt.close(fig)

# Figure 3: relative gain/loss of the frozen strategies
fig, ax = plt.subplots(figsize=(9, 5.5))
for model, st in MODEL_STYLE.items():
    for strat in STRATS:
        x, y = switch_series(model, strat, "relative_gain")
        ax.plot(x, [100 * v for v in y], color=STRAT_COLORS[strat], ls=st["ls"],
                marker=st["marker"], ms=4, lw=1.6, label="%s — %s" % (strat, st["label"]))
ax.axhline(0, color="k", lw=1.0)
ax.axhline(5, color="gray", ls=":", lw=1.4, label="5% practical threshold (delta_T)")
xaxis(ax, sizes)
ax.set_ylabel("relative gain vs model-implied best fixed (%)")
ax.set_xlabel("file size (log scale)")
ax.set_title("Fig. 3 — Frozen switching strategies: relative gain/loss (negative values shown)")
ax.legend(fontsize=7.5, ncol=2)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig3_relative_gain_vs_filesize.png"), dpi=160)
plt.close(fig)

# Figure 4: absolute time saved
fig, ax = plt.subplots(figsize=(9, 5.5))
for model, st in MODEL_STYLE.items():
    for strat in STRATS:
        x, y = switch_series(model, strat, "absolute_time_saved_seconds")
        ax.plot(x, y, color=STRAT_COLORS[strat], ls=st["ls"], marker=st["marker"], ms=4, lw=1.6,
                label="%s — %s" % (strat, st["label"]))
ax.axhline(0, color="k", lw=1.0)
xaxis(ax, sizes)
ax.set_ylabel("absolute time saved vs model-implied best fixed (s)")
ax.set_xlabel("file size (log scale)")
ax.set_title("Fig. 4 — Frozen switching strategies: absolute time saved (negative values shown)")
ax.legend(fontsize=7.5, ncol=2)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig4_absolute_saved_vs_filesize.png"), dpi=160)
plt.close(fig)

# Figure 5: exploratory single-switch upper envelope
fig, ax = plt.subplots(figsize=(9, 5.5))
for model, st in MODEL_STYLE.items():
    for strat in STRATS:
        rows = [r for r in J["envelope_rows"] if r["model"] == model and r["strategy"] == strat and r["best_switch"]]
        rows.sort(key=lambda r: r["file_size_bytes"])
        if rows:
            ax.plot([r["file_size_bytes"] for r in rows],
                    [100 * r["best_switch"]["relative_gain"] for r in rows],
                    color=STRAT_COLORS[strat], ls=st["ls"], marker=st["marker"], ms=4, lw=1.5,
                    label="grid: %s — %s" % (strat, st["label"]))
dense = [r for r in J["dense_modular"] if r["model"] == "MODEL_A_analytical" and r["best_16to4_envelope_gain"] is not None]
dense.sort(key=lambda r: r["file_size_bytes"])
ax.plot([r["file_size_bytes"] for r in dense], [100 * r["best_16to4_envelope_gain"] for r in dense],
        color="black", ls="-", marker=".", ms=4, lw=1.0,
        label="dense residue scan: 16MiB->4MiB (Model A)")
ax.axhline(0, color="k", lw=1.0)
ax.axhline(5, color="gray", ls=":", lw=1.4, label="5% practical threshold")
xaxis(ax, sizes + [512 * MIB + 128 * MIB])
ax.set_ylabel("best single-switch relative gain (%)")
ax.set_xlabel("file size (log scale)")
ax.set_title("Fig. 5 — EXPLORATORY analytical single-switch upper envelope (not the frozen policy)")
ax.legend(fontsize=7, ncol=2)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig5_envelope_vs_filesize.png"), dpi=160)
plt.close(fig)

# Figure 6: analytical vs calibrated models vs Stage 0S observed
fig, ax = plt.subplots(figsize=(9.5, 5.5))
for model, st in MODEL_STYLE.items():
    for arm in ARMS:
        x, y, _ = fixed_series(model, arm)
        ax.plot(x, y, color=COLORS[arm], ls=st["ls"], marker=st["marker"], ms=4, lw=1.6,
                label="%s — %s" % (arm, st["label"]))
obs = J["stage0s_validation"]["MODEL_B_stage0s_calibrated"]
for arm in ARMS:
    o = obs[arm]
    ax.errorbar([512 * MIB], [o["observed_median_seconds"]],
                yerr=[[o["observed_median_seconds"] - o["observed_min_seconds"]],
                      [o["observed_max_seconds"] - o["observed_median_seconds"]]],
                fmt="*", color="black", ms=11, capsize=4, lw=1.2,
                label="Stage 0S observed median + 8-run min–max (empirical)" if arm == "256KiB" else None)
ax.axvline(512 * MIB, color="gray", ls=":", lw=1.0)
xaxis(ax, sizes)
ax.set_ylabel("completion time (s)")
ax.set_xlabel("file size (log scale)")
ax.set_title("Fig. 6 — Analytical vs Stage 0S-calibrated fixed-S curves (+ Stage 0S observations)")
ax.legend(fontsize=7, ncol=2)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig6_models_comparison.png"), dpi=160)
plt.close(fig)

# Figure 7: tail contribution vs total completion time
fig, ax = plt.subplots(figsize=(9, 5.5))
q = J["protocol"]["frozen_quantities"]
T_tail_A = q["T_tail_primary_seconds"]
params_B = J["models"]["MODEL_B_stage0s_calibrated"]["params"]
T_tail_B = params_B["a_seconds"] + params_B["b_seconds_per_byte"] * 16 * MIB
xs = J["file_size_grid"]
t_ref = [f / (4 * 500000.0) for f in xs]
ax.plot(xs, [100 * T_tail_A / t for t in t_ref], color="#d62728", marker="o", ms=4,
        label="tail window share — Model A (T_tail_primary / T_ref)")
ax.plot(xs, [100 * T_tail_B / t for t in t_ref], color="#d62728", ls="--", marker="s", ms=4,
        label="tail window share — Model B")
ax.plot(xs, [100 * (0.040 * 500000.0) / (16 * MIB)] * len(xs), color="#1f77b4", ls=":", lw=1.6,
        label="per-request overhead share, 16MiB arm (R*D/s; scale-invariant)")
ax.plot(xs, [100 * (0.040 * 500000.0) / (256 * 1024)] * len(xs), color="#1f77b4", ls="-.", lw=1.4,
        label="per-request overhead share, 256KiB arm (R*D/s)")
xaxis(ax, xs)
ax.set_ylabel("share of idealized completion T_ref (%)")
ax.set_xlabel("file size (log scale)")
ax.set_title("Fig. 7 — Tail contribution vs total completion time as file size grows")
ax.legend(fontsize=7.5)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig7_tail_contribution_vs_filesize.png"), dpi=160)
plt.close(fig)

print("wrote 7 figures to", FIG)
for f in sorted(os.listdir(FIG)):
    print("  ", f, os.path.getsize(os.path.join(FIG, f)), "bytes")
