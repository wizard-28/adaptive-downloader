#!/usr/bin/env python3
"""Figures for the Review 3 report, drawn from the committed analysis files.

Usage: python3 reports/review3/make_figs.py   (run from the repository root)
"""
import json, os
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

OUT = "reports/review3/figs"
os.makedirs(OUT, exist_ok=True)
BLUE, GREY, DARK, LIGHT = "#2f6fd6", "#b9b8b0", "#55544e", "#e9e8e4"
plt.rcParams.update({"font.family": "DejaVu Sans", "font.size": 10, "axes.spines.top": False,
                     "axes.spines.right": False, "axes.edgecolor": "#8a8a85", "axes.titleweight": "bold",
                     "axes.titlesize": 11, "axes.titlelocation": "left"})

abl = json.load(open("testdata/split_ablation/analysis/summary.json"))["scenarios"]


def g(sc, arm):
    return abl[sc]["geo_mean_s"][arm]


# 1. Engine ablation: before fix, after fix, aria2 -----------------------------
rows = [("Cap from start\n(CAP0)", "CAP0", "fixed_16_1M_T30", "fixed_16_32M_NT_split"),
        ("Cap from 8 s\n(CAP8)", "CAP8", "fixed_16_1M_T30", "fixed_16_32M_NT_split"),
        ("No cap\n(HEALTHY)", "HEALTHY", "fixed_16_1M_T30", "fixed_16_1M_NT_split"),
        ("Paced\n(D2)", "D2", "fixed_16_4M_T30", "fixed_16_4M_NT_split")]
fig, ax = plt.subplots(figsize=(7.2, 3.4))
w = 0.26
for i, (lab, sc, old, new) in enumerate(rows):
    vals = [g(sc, old), g(sc, new), g(sc, "aria2")]
    for j, (v, c) in enumerate(zip(vals, [GREY, BLUE, DARK])):
        ax.bar(i + (j - 1) * w, v, w * 0.92, color=c)
        ax.text(i + (j - 1) * w, v + 1.5, f"{v:.0f}", ha="center", fontsize=8.5, color="#333")
ax.set_xticks(range(len(rows)), [r[0] for r in rows])
ax.set_ylabel("completion time (s), geometric mean of 10")
ax.set_title("With the engine fixed, ours matches aria2 under the cap")
ax.legend(["ours, earlier best setting on the original engine (30 s deadline, no splitting)",
           "ours, best setting on the fixed engine (no deadline, straggler splitting)", "aria2 1.37"],
          frameon=False, fontsize=8, loc="upper right")
ax.set_ylim(0, 135)
fig.tight_layout()
fig.savefig(f"{OUT}/ablation.png", dpi=220)
plt.close(fig)

# 2. Adaptive controllers vs best stationary ------------------------------------
items = [("Causal controller vs best of 3\npredeclared fixed settings (CAP8)", g("CAP8", "causal_joint_T30"), g("CAP8", "fixed_16_1M_T30")),
         ("Causal controller vs best fixed,\nfixed engine (CAP8)", g("CAP8", "causal_joint_NT_split"), g("CAP8", "fixed_16_32M_NT_split")),
         ("Workload selector vs best fixed,\nfixed engine (CAP0)", g("CAP0", "selector_NT_split"), g("CAP0", "fixed_16_32M_NT_split"))]
fig, ax = plt.subplots(figsize=(7.2, 2.9))
for i, (lab, ad, st) in enumerate(items):
    y = len(items) - 1 - i
    ax.plot([st, ad], [y, y], color=GREY, lw=3, zorder=1)
    ax.scatter([st], [y], color=DARK, s=60, zorder=2)
    ax.scatter([ad], [y], color=BLUE, s=60, zorder=3)
    ax.text(ad, y + 0.22, f"{ad:.1f}", ha="center", fontsize=8.5, color=BLUE)
    ax.text(st, y - 0.38, f"{st:.1f}", ha="center", fontsize=8.5, color=DARK)
ax.set_yticks(range(len(items)), [it[0] for it in reversed(items)], fontsize=8.5)
ax.set_xlabel("completion time (s)")
ax.set_xlim(45, 95)
ax.set_ylim(-0.7, len(items) - 0.4)
ax.scatter([], [], color=BLUE, label="adaptive"); ax.scatter([], [], color=DARK, label="fixed")
ax.legend(frameon=False, fontsize=8.5, loc="lower right")
ax.set_title("The adaptive gain disappears with the fixed engine")
fig.tight_layout()
fig.savefig(f"{OUT}/adaptive.png", dpi=220)
plt.close(fig)

# 3. Wasted bytes under the old 30 s whole-request timeout ----------------------
old = json.load(open("testdata/split_ablation/analysis/old_grid_reliability.json"))
cells = [("N1", "capped/N1/S32768KiB"), ("N2", "capped/N2/S32768KiB"), ("N4", "capped/N4/S32768KiB"),
         ("N8", "capped/N8/S32768KiB"), ("N16", "capped/N16/S32768KiB")]
fig, ax = plt.subplots(figsize=(7.2, 2.8))
vals = [100 * old[k]["wasted_frac_mean"] for _, k in cells]
bars = ax.bar([c[0] for c in cells], vals, color=[GREY if v < 50 else "#c95b4b" for v in vals], width=0.6)
for b, (_, k), v in zip(bars, cells, vals):
    tag = "" if old[k]["valid"] else "  aborted"
    ax.text(b.get_x() + b.get_width() / 2, v + 2, f"{v:.0f} %{tag}", ha="center", fontsize=8.5)
ax.set_ylabel("extra bytes re-downloaded\n(% of file size)")
ax.set_xlabel("connections, 32 MiB ranges, capped server, 30 s whole-request timeout")
ax.set_ylim(0, 130)
ax.set_title("The old 30 s timeout re-downloaded slow ranges")
fig.tight_layout()
fig.savefig(f"{OUT}/waste.png", dpi=220)
plt.close(fig)

# 3b. Live pilot: one global fixed cell vs the per-condition best -------------
lp = [c for c in json.load(open("testdata/live_pilot_l1/analysis/summary.json"))["conditions"] if c["status"] == "OK"]
fig, ax = plt.subplots(figsize=(7.2, 3.0))
xs = range(len(lp))
glob_r = [100 * (c["time_s"]["ours_N8_S1M"] / c["best_s"] - 1) for c in lp]
aria_r = [100 * (c["aria2_over_best_fixed"] - 1) for c in lp]
ax.axhline(5, color=GREY, ls="--", lw=1, label="5 % line")
ax.scatter(xs, aria_r, color=GREY, s=28, label="aria2 1.37", zorder=2)
ax.scatter(xs, glob_r, color=BLUE, s=34, label="ours, one fixed setting (8 x 1 MiB) everywhere", zorder=3)
sess = {"S1": 1, "S1T": 1, "S2": 2, "S4": 3, "S5R": 4}
ax.set_xticks(list(xs), [f"{c['server']}\n#{sess[c['slot']]}" for c in lp], fontsize=7.5)
ax.set_ylabel("slower than the best fixed\nsetting for the condition (%)")
ax.set_xlabel("mirror and measurement session")
ax.legend(frameon=False, fontsize=8, loc="upper left")
ax.set_title("On public mirrors one fixed setting was close to best in most conditions")
fig.tight_layout()
fig.savefig(f"{OUT}/live.png", dpi=220)
plt.close(fig)

# 4. Online tuner (drawn only once its analysis exists) --------------------------
p = "testdata/online_tuner/analysis/summary.json"
if os.path.exists(p):
    tun = json.load(open(p))["scenarios"]
    arms = [("online_tuner", "online tuner (FastBioDL-style)", BLUE), ("fixed_16_1M_NT_split", "fixed 16 x 1 MiB", GREY),
            ("fixed_4_1M_NT_split", "fixed 4 x 1 MiB", LIGHT), ("aria2", "aria2 1.37", DARK)]
    scs = [s for s in ("CAP8", "HEALTHY") if s in tun]
    fig, ax = plt.subplots(figsize=(7.2, 3.2))
    w = 0.2
    for i, sc in enumerate(scs):
        for j, (a, lab, c) in enumerate(arms):
            v = tun[sc]["geo_mean_s"].get(a)
            if v is None:
                continue
            x = i + (j - 1.5) * w
            ax.bar(x, v, w * 0.92, color=c, edgecolor="#999" if c == LIGHT else c, label=lab if i == 0 else None)
            ax.text(x, v + 1.5, f"{v:.0f}", ha="center", fontsize=8.5)
    ax.set_xticks(range(len(scs)), [{"CAP8": "Cap from 8 s (CAP8)", "HEALTHY": "No cap (HEALTHY)"}[s] for s in scs])
    ax.set_ylabel("completion time (s), geometric mean")
    ax.legend(frameon=False, fontsize=8.5, ncol=2, loc="upper right")
    ax.set_ylim(0, 125)
    ax.set_title("Online concurrency tuner against fixed settings and aria2")
    fig.tight_layout()
    fig.savefig(f"{OUT}/tuner.png", dpi=220)
    plt.close(fig)
print("figures in", OUT)
