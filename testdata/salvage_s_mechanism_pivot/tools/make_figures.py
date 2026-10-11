#!/usr/bin/env python3
"""Analytical figures for the adaptive-S mechanism pivot (simulation output only)."""
import json
import os
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

BASE = "/workspaces/mvp/testdata/salvage_s_mechanism_pivot"
sys.path.insert(0, os.path.join(BASE, "tools"))
import mechanism_sim as ms

J = json.load(open(os.path.join(BASE, "analysis", "screening.json")))
FIG = os.path.join(BASE, "figures")
os.makedirs(FIG, exist_ok=True)
MIB = 1024 ** 2
ARMS = ["256KiB", "1MiB", "4MiB", "16MiB"]
ARM_BYTES = {l: s for l, s in ms.ARMS}
MODELS = ["shared", "lockin_full", "serialized"]
COLORS = {"256KiB": "#1f77b4", "1MiB": "#ff7f0e", "4MiB": "#2ca02c", "16MiB": "#d62728"}


def e1_rows(model, size="512MiB"):
    return [r for r in J["e1_stationary"] if r["model"] == model and r["file_size"] == size]


# Fig 1/2: completion and normalized completion vs S by state
for fignum, key, ylabel, fname in [
    (1, "completion", "completion time (s)", "fig1_completion_vs_S_by_state.png"),
    (2, "normalized", "T_S / T_ref", "fig2_normalized_vs_S_by_state.png"),
]:
    fig, axes = plt.subplots(1, 2, figsize=(11, 4.6), sharey=False)
    for ax, model in zip(axes, ["shared", "lockin_full"]):
        rows = e1_rows(model)
        rates = sorted({r["aggregate_rate_bytes_per_s"] for r in rows})
        for r in rates:
            rr = [x for x in rows if x["aggregate_rate_bytes_per_s"] == r][0]
            if key == "completion":
                ys = [rr["T_" + a] for a in ARMS]
                t_ref = rr["file_bytes"] / r
            else:
                t_ref = rr["file_bytes"] / r
                ys = [rr["T_" + a] / t_ref for a in ARMS]
            ax.plot(ARMS, ys, marker="o", ms=4, lw=1.4,
                    label="%.2f MB/s" % (r / MIB))
        ax.set_xlabel("fixed chunk size (analytical states)")
        ax.set_ylabel(ylabel)
        ax.set_title("model: %s" % model)
        ax.grid(True, alpha=0.25)
        ax.legend(fontsize=7, title="aggregate rate")
    fig.suptitle("Fig. %d — %s vs fixed S in each constant-rate state (analytical simulation)" % (
        fignum, "Completion time" if key == "completion" else "Normalized completion"))
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, fname), dpi=160)
    plt.close(fig)

# Fig 3: preferred S across states
fig, axes = plt.subplots(1, 2, figsize=(11, 4.6))
ax = axes[0]
arm_y = {a: i for i, a in enumerate(ARMS)}
for model in MODELS:
    rows = sorted(e1_rows(model), key=lambda r: r["aggregate_rate_bytes_per_s"])
    xs = [r["aggregate_rate_bytes_per_s"] / MIB for r in rows]
    ys = [arm_y[r["argmin"]] for r in rows]
    ax.plot(xs, ys, marker="o", ms=4, lw=1.4, label=model)
ax.set_yticks(range(len(ARMS)))
ax.set_yticklabels(ARMS)
ax.set_xscale("log", base=2)
ax.set_xlabel("aggregate rate (MB/s)")
ax.set_ylabel("model-implied preferred S")
ax.set_title("stationary states (F=512 MiB): argmin_S T(S | rate)")
ax.grid(True, alpha=0.25)
ax.legend(fontsize=8)
ax = axes[1]
labels, los, his = [], [], []
for r in J["e3_traces"]:
    if r["model"] == "lockin_full":
        labels.append(r["trace"].replace("-driving", ""))
        los.append(arm_y[r["regime_argmin_low"]])
        his.append(arm_y[r["regime_argmin_high"]])
x = range(len(labels))
ax.plot(x, los, marker="o", ms=5, lw=0, label="low-rate regime")
ax.plot(x, his, marker="x", ms=7, lw=0, label="high-rate regime")
ax.set_xticks(list(x))
ax.set_xticklabels(labels, rotation=30, ha="right", fontsize=7)
ax.set_yticks(range(len(ARMS)))
ax.set_yticklabels(ARMS)
ax.set_title("trace regimes (lock-in model): argmin per regime")
ax.grid(True, alpha=0.25)
ax.legend(fontsize=8)
fig.suptitle("Fig. 3 — Preferred S across environmental states (analytical; flat = no moving optimum)")
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig3_preferred_S_across_states.png"), dpi=160)
plt.close(fig)

# Fig 4: requests spanning a capacity transition (shared model)
prof = ms.two_phase_profile(0.5 * MIB, 8 * MIB, 40.0)
res = ms.simulate(64 * MIB, prof, "shared", 4 * MIB, N=4, D=0.04, keep_schedule=True)
fig, ax = plt.subplots(figsize=(10, 4.8))
for e in res["schedule"]:
    if e["completion_time"] is None:
        continue
    ax.barh(0, e["completion_time"] - e["transfer_start"], left=e["transfer_start"],
            height=0.35, color=COLORS.get("4MiB"), alpha=0.55)
    ax.barh(0, 0.04, left=e["alloc_time"], height=0.5, color="gray", alpha=0.8)
ax.axvline(40.0, color="k", ls="--", lw=1.2, label="capacity transition (0.5 -> 8 MB/s)")
ax.set_xlabel("time (s)")
ax.set_yticks([])
ax.set_title("Fig. 4 — Shared-capacity model: requests span the transition; in-flight rates change mid-request")
ax.legend(fontsize=8, loc="upper left")
ax.grid(True, alpha=0.25, axis="x")
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig4_requests_spanning_transition.png"), dpi=160)
plt.close(fig)

# Fig 5: fixed vs switch vs oracle (gains)
fig, ax = plt.subplots(figsize=(11, 4.8))
cases = [r for r in J["e2_transitions"] if r["case"] != "stable_to_volatile"]
labels = ["%s|%s" % (r["model"][:6], r["case"].replace("_", " ")[:18]) for r in cases]
xs = range(len(cases))
ax.bar([x - 0.22 for x in xs], [100 * r["causal_gain"] for r in cases], width=0.22,
       label="causal (observed-rate switch)", color="#1f77b4")
ax.bar(list(xs), [100 * r["oracle_gain"] for r in cases], width=0.22,
       label="oracle single switch (diagnostic)", color="#d62728")
ax.bar([x + 0.22 for x in xs], [100 * r["stationary_causal_gain"] for r in cases], width=0.22,
       label="causal in stationary control", color="#2ca02c")
ax.axhline(0, color="k", lw=1)
ax.axhline(5, color="gray", ls=":", label="5% practical threshold")
ax.set_xticks(list(xs))
ax.set_xticklabels(labels, rotation=40, ha="right", fontsize=6.5)
ax.set_ylabel("gain vs model-implied best fixed (%)")
ax.set_title("Fig. 5 — Fixed vs one-switch vs oracle: within-download transition cases")
ax.legend(fontsize=7.5)
ax.grid(True, alpha=0.25, axis="y")
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig5_fixed_vs_switch_vs_oracle.png"), dpi=160)
plt.close(fig)

# Fig 6: headroom vs volatility period
fig, ax = plt.subplots(figsize=(9, 4.8))
for frac in [0.25, 0.5]:
    for mode, mk in [("alternating", "o"), ("random", "s")]:
        rows = [r for r in J["e2c_volatility_robustness"] if r["stable_frac"] == frac and r["mode"] == mode]
        rows.sort(key=lambda r: r["period_s"])
        ax.plot([r["period_s"] for r in rows], [100 * r["oracle_gain"] for r in rows],
                marker=mk, ls="-", label="oracle, stable_frac=%.2f (%s)" % (frac, mode))
        ax.plot([r["period_s"] for r in rows], [100 * r["causal_from_16MiB_gain"] for r in rows],
                marker=mk, ls="--", alpha=0.6, label="causal from 16MiB, stable_frac=%.2f (%s)" % (frac, mode))
ax.axhline(0, color="k", lw=1)
ax.axhline(5, color="gray", ls=":", label="5% practical threshold")
ax.set_xscale("log")
ax.set_xticks([5, 17, 60])
ax.set_xticklabels(["5", "17", "60"])
ax.set_xlabel("volatility alternation period (s, log scale)")
ax.set_ylabel("gain vs model-implied best fixed (%)")
ax.set_title("Fig. 6 — Lock-in headroom vs volatility period: fragile oracle, harmful causal")
ax.legend(fontsize=6.5, ncol=2)
ax.grid(True, alpha=0.25)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig6_headroom_vs_volatility.png"), dpi=160)
plt.close(fig)

# Fig 7: stationary vs changing
fig, ax = plt.subplots(figsize=(9, 4.8))
changing = [100 * r["causal_gain"] for r in J["e2_transitions"] if r["case"] != "stable_to_volatile"]
stationary = [100 * r["stationary_causal_gain"] for r in J["e2_transitions"] if r["case"] != "stable_to_volatile"]
changing += [100 * r["causal_from_16MiB_gain"] for r in J["e2c_volatility_robustness"]]
stationary += [100 * r["stationary_causal_gain"] for r in J["e2c_volatility_robustness"]]
ax.scatter(stationary, changing, color="#d62728", zorder=3)
ax.axhline(0, color="k", lw=1)
ax.axvline(0, color="k", lw=1)
ax.axhline(5, color="gray", ls=":", label="5% practical threshold")
ax.set_xlabel("causal gain in stationary control (%)")
ax.set_ylabel("causal gain in changing environment (%)")
ax.set_title("Fig. 7 — Stationary vs changing: causal gains (all cases, both models)")
ax.legend(fontsize=8)
ax.grid(True, alpha=0.25)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "fig7_stationary_vs_changing.png"), dpi=160)
plt.close(fig)

print("wrote 7 figures to", FIG)
for f in sorted(os.listdir(FIG)):
    print("  ", f, os.path.getsize(os.path.join(FIG, f)))
