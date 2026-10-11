#!/usr/bin/env python3
"""W1-W6 figures: live grid validation, adaptive comparisons, selector behavior."""
import json
import os
import statistics

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
FIG = os.path.join(BASE, "figures")
RUNS = os.path.join(BASE, "w1_w6_adaptive")
MIB = 1024 ** 2
plt.rcParams.update({"font.size": 9, "figure.dpi": 130})

M = json.load(open(os.path.join(BASE, "manifest", "w1_w6_manifest.json")))
G = json.load(open(os.path.join(BASE, "analysis", "w1_w6_grid.json")))
A = json.load(open(os.path.join(BASE, "analysis", "w1_w6_adaptive.json")))
WL = sorted(M["workloads"], key=lambda x: int(x[1:]))
MODES = ["band_selector", "per_workload_best_fixed", "family_best_fixed", "aria2"]
COLORS = ["#2b7bba", "#e1812c", "#59a14f", "#b07aa1"]


def wl_label(w):
    lab = M["workloads"][w]["label"]
    return w if lab == "healthy" else "%s\n%s" % (w, lab)


# Fig W1: per-workload completion by mode
fig, ax = plt.subplots(figsize=(11, 4.2))
x = np.arange(len(WL))
w = 0.2
for i, mode in enumerate(MODES):
    vals = [A["workloads"][wid][mode]["median_s"] for wid in WL]
    ax.bar(x + (i - 1.5) * w, vals, w, label=mode.replace("_", " "), color=COLORS[i])
    for xi, v in zip(x + (i - 1.5) * w, vals):
        if v:
            ax.text(xi, v + 2, "%.0f" % v, ha="center", va="bottom", fontsize=6.5)
ax.set_xticks(x)
ax.set_xticklabels([wl_label(w_) for w_ in WL])
ax.set_ylabel("completion (s, median of 10)")
ax.set_title("W1-W6: frozen band selector vs baselines (live, netem 40ms + 0.4%)")
ax.legend(fontsize=8)
ax.grid(axis="y", alpha=0.3)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "figW1_modes.png"))
plt.close(fig)

# Fig W2: paired ratios of selector vs baselines
fig, axes = plt.subplots(1, 3, figsize=(11, 3.6), sharey=True)
bases = ["per_workload_best_fixed", "family_best_fixed", "aria2"]
for ax, base in zip(axes, bases):
    data = []
    for wid in WL:
        d = A["workloads"][wid]["paired"][base]
        data.append(d["ratios"] if d["ratios"] else [np.nan])
    ax.boxplot(data, vert=True, widths=0.55)
    ax.axhline(1.0, color="k", lw=0.8, ls="--")
    ax.set_title("selector / %s" % base.replace("_", " "), fontsize=8)
    ax.set_xticklabels([w for w in WL])
    ax.grid(axis="y", alpha=0.3)
axes[0].set_ylabel("paired ratio (<1 = selector faster)")
fig.tight_layout()
fig.savefig(os.path.join(FIG, "figW2_ratios.png"))
plt.close(fig)

# Fig W3: live fixed-grid heatmaps
fig, axes = plt.subplots(2, 3, figsize=(11, 6))
cells_n = [4, 8, 16]
cells_s = [1 * MIB, 4 * MIB, 16 * MIB]
for ax, wid in zip(axes.flat, WL):
    Mx = np.full((3, 3), np.nan)
    for i, n in enumerate(cells_n):
        for j, s in enumerate(cells_s):
            c = G["workloads"][wid]["cells"].get("%d/%d" % (n, s))
            if c and c["median_s"]:
                Mx[i, j] = c["median_s"]
    im = ax.imshow(Mx, cmap="viridis_r", aspect="auto")
    ax.set_xticks(range(3))
    ax.set_xticklabels(["%dM" % (s // MIB) for s in cells_s])
    ax.set_yticks(range(3))
    ax.set_yticklabels(cells_n)
    ax.set_title("%s  (%s)" % (wid, M["workloads"][wid]["label"]), fontsize=8)
    for i in range(3):
        for j in range(3):
            if not np.isnan(Mx[i, j]):
                ax.text(j, i, "%.0f" % Mx[i, j], ha="center", va="center",
                        color="white" if Mx[i, j] > np.nanmax(Mx) * 0.55 else "black",
                        fontsize=7)
    fig.colorbar(im, ax=ax, fraction=0.046)
fig.suptitle("Live fixed grid (s, median of 2): N x S per workload", fontsize=10)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "figW3_grids.png"))
plt.close(fig)

# Fig W4: selector classification behavior (representative run per workload)
fig, axes = plt.subplots(2, 3, figsize=(11, 5.4), sharex=False)
levels = M["selector"]["levels_bytes_s"]
targets = M["selector"]["targets"]
for ax, wid in zip(axes.flat, WL):
    p = os.path.join(RUNS, "%s_band_selector_r00.json" % wid)
    if not os.path.exists(p):
        ax.set_title(wid)
        continue
    j = json.load(open(p))
    ds = j.get("decisions", [])
    ts = [d["time_ms"] / 1000.0 for d in ds]
    th = [d["throughput"] / 1024.0 for d in ds]
    ax.plot(ts, th, ".", ms=2.5, color="#2b7bba")
    for lv in sorted(set(levels)):
        ax.axhline(lv / 1024.0, color="gray", ls=":", lw=0.8)
    dec = next((d for d in ds if d["action_kind"] != "none"), None)
    if dec:
        ax.axvline(dec["time_ms"] / 1000.0, color="crimson", ls="--", lw=1)
        ax.text(dec["time_ms"] / 1000.0, ax.get_ylim()[1] * 0.9, " switch",
                color="crimson", fontsize=7)
    ax.set_title("%s final=(%s,%s)" % (wid, j.get("final_n"), j.get("final_s")), fontsize=8)
    ax.set_ylabel("KiB/s", fontsize=7)
    ax.grid(alpha=0.3)
for ax in axes[1]:
    ax.set_xlabel("time (s)", fontsize=7)
fig.suptitle("Band selector: observed per-request throughput, dotted = frozen levels", fontsize=10)
fig.tight_layout()
fig.savefig(os.path.join(FIG, "figW4_selector.png"))
plt.close(fig)

print("figures figW1..figW4 written")
