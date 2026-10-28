#!/usr/bin/env python3
"""Column-width figures 4-8 of the IEEE Access paper, drawn from committed analysis data.

Same data and descriptive scenario names as reports/handbook/make_deck_figs.py (the
presentation deck), but sized for print: 3.45 in (one column) or 7.1 in (both columns),
7-8 pt text, 300 dpi. Output: reports/ieee_access/figures/fig4..fig8_*.png.

Usage: python3 reports/ieee_access/make_paper_figs.py   (from the repository root)
"""
import json
import os

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import matplotlib.ticker
from matplotlib.patches import Patch

OUT = "reports/ieee_access/figures"
T = "testdata"
BLUE, ORANGE, GREEN, GREY, DARK, RED = "#2f6fd6", "#e07b28", "#2e9e6a", "#b9b8b0", "#55544e", "#c8423b"
plt.rcParams.update({"font.family": "DejaVu Sans", "font.size": 7.5, "axes.labelsize": 7.5,
                     "xtick.labelsize": 7, "ytick.labelsize": 7, "legend.fontsize": 7,
                     "axes.spines.top": False, "axes.spines.right": False, "axes.edgecolor": "#8a8a85",
                     "axes.linewidth": 0.6})
COL, FULL = 3.45, 7.1


def load(p):
    with open(p) as f:
        return json.load(f)


def save(f, name):
    f.tight_layout(pad=0.4)
    f.savefig(os.path.join(OUT, name), dpi=300)
    plt.close(f)


os.makedirs(OUT, exist_ok=True)

# Fig. 4: adaptive arms against aria2 (Section 9e)
rows = [("server slow, then fast", [0.876], 0.376),
        ("server speeds up at 1 s", [0.359, 0.432], 0.344),
        ("server cap switches on at 8 s", [1.096, 1.310, 1.367, 1.422, 1.781], 1.016),
        ("collapse, delay, loss, throttle", [1.456], None),
        ("six capped server variants", [1.660], None)]
f, ax = plt.subplots(figsize=(COL, 2.3))
ax.axvspan(0.28, 1.0, color="#e8f0fb", lw=0)
ax.axvspan(1.0, 2.1, color="#fbeee4", lw=0)
for i, (lab, arms, fixed) in enumerate(reversed(rows)):
    ax.scatter(arms, [i] * len(arms), color=ORANGE, s=22, zorder=3)
    if fixed:
        ax.scatter([fixed], [i], color=BLUE, marker="D", s=22, zorder=3)
ax.axvline(1, color=DARK, ls="--", lw=0.8)
ax.set_xscale("log")
ax.set_xticks([0.3, 0.5, 0.7, 1, 1.5, 2], ["0.3", "0.5", "0.7", "1", "1.5", "2"])
ax.xaxis.set_minor_formatter(matplotlib.ticker.NullFormatter())
ax.set_xlim(0.28, 2.1)
ax.set_yticks(range(len(rows)), [r[0] for r in reversed(rows)])
ax.set_ylim(-0.6, len(rows) - 0.1)
ax.set_xlabel("time ÷ aria2 time (below 1: faster)")
ax.scatter([], [], color=ORANGE, s=22, label="our adaptive arms")
ax.scatter([], [], color=BLUE, marker="D", s=22, label="our best fixed setting")
ax.legend(frameon=False, loc="lower left", ncol=1, fontsize=6.5)
save(f, "fig7_vs_aria2.png")

# Fig. 5: all adaptive controllers against fixed settings (Section 10), full width
rows = [("drop rule vs small-piece fixed, original engine", 0.766, 0.762, 0.769),
        ("drop rule vs 16 × 32 MiB, fixed engine", 1.071, 1.054, 1.085),
        ("workload selector vs 16 × 32 MiB, cap from start", 1.229, 1.223, 1.234),
        ("online tuner vs 16 × 1 MiB, cap from 8 s", 1.212, 1.180, 1.248),
        ("first DQN agent, not retrained, vs 16 × 32 MiB", 1.356, 1.252, 1.472),
        ("first retrained agent vs 16 × 1 MiB", 0.907, 0.893, 0.921),
        ("first retrained agent vs 16 × 32 MiB", 1.298, 1.274, 1.321),
        ("agent picking N and piece size vs 16 × 32 MiB", 1.471, 1.405, 1.546),
        ("broadly trained agent vs 16 × 1 MiB", 0.845, 0.840, 0.850)]
f, ax = plt.subplots(figsize=(FULL, 2.5))
for i, (lab, r, lo, hi) in enumerate(reversed(rows)):
    col = GREEN if hi < 1 else RED
    ax.plot([lo, hi], [i, i], color=col, lw=1.4)
    ax.plot(r, i, "o", color=col, ms=4.5)
    ax.text(hi + 0.012, i, f"{r:.3f}", va="center", fontsize=7, zorder=5, bbox=dict(fc="white", ec="none", pad=0.6))
ax.axvline(1, color=DARK, ls="--", lw=0.8)
ax.set_yticks(range(len(rows)), [r[0] for r in reversed(rows)])
ax.set_xlabel("adaptive ÷ fixed time, 95 % interval (green: adaptive faster; red: adaptive slower)")
ax.set_xlim(0.72, 1.62)
save(f, "fig8_all_controllers.png")

# Fig. 6: oracle headroom on the five replayed traces (Section 8)
H = [("Verizon\nEVDO", 1.641), ("Verizon\nLTE", 0.019), ("AT&T\nLTE", 0.016), ("T-Mobile\nLTE", 0.061),
     ("T-Mobile\nUMTS", 0.294)]
f, ax = plt.subplots(figsize=(COL, 1.9))
ax.bar([h[0] for h in H], [h[1] for h in H], color=BLUE, width=0.6)
for i, (_, h) in enumerate(H):
    ax.text(i, h + 0.25, f"{h:.2f} %", ha="center", fontsize=7)
ax.axhline(10, color=RED, ls="--", lw=1)
ax.text(4.4, 9.5, "bar set in advance: 10 %", ha="right", va="top", fontsize=7, color=RED)
ax.set_ylim(0, 11)
ax.set_ylabel("oracle headroom (%)")
save(f, "fig5_oracle_headroom.png")

# Fig. 7: engine ablation (Section 7a)
abl = load(f"{T}/split_ablation/analysis/summary.json")["scenarios"]
rows = [("cap from\nstart", "CAP0", "fixed_16_1M_T30", "fixed_16_32M_NT_split"),
        ("cap from\n8 s", "CAP8", "fixed_16_1M_T30", "fixed_16_32M_NT_split"),
        ("no cap", "HEALTHY", "fixed_16_1M_T30", "fixed_16_1M_NT_split"),
        ("slow,\nthen fast", "D2", "fixed_16_4M_T30", "fixed_16_4M_NT_split")]
f, ax = plt.subplots(figsize=(COL, 2.1))
w = 0.27
for i, (lab, sc, old, new) in enumerate(rows):
    for j, (arm, col) in enumerate([(old, GREY), (new, BLUE), ("aria2", DARK)]):
        v = abl[sc]["geo_mean_s"][arm]
        ax.bar(i + (j - 1) * w, v, w * 0.92, color=col)
        ax.text(i + (j - 1) * w, v + 2, f"{v:.0f}", ha="center", fontsize=6.5)
ax.set_xticks(range(len(rows)), [r[0] for r in rows])
ax.set_ylabel("completion time (s)")
ax.legend(handles=[Patch(color=GREY, label="original engine"), Patch(color=BLUE, label="fixed engine"),
                   Patch(color=DARK, label="aria2")], frameon=False, loc="upper right")
ax.set_ylim(0, 135)
save(f, "fig4_engine_ablation.png")

# Fig. 8: broadly trained agent on held-out scenarios (Section 9d)
c2 = load(f"{T}/rl_cap_train_v2/analysis/C.json")["scenarios"]
d2 = load(f"{T}/rl_cap_train_v2/analysis/D.json")["scenarios"]
sc = {**c2, **d2}
order = [("CAP8", "8 s cap\n(trained)"), ("HEALTHY", "no cap"), ("H_C12", "cap\n12 MiB/s"),
         ("H_RTT110", "delay\n110 ms"), ("H_DIP", "cap +\ndip"), ("H_C20", "cap\n20 MiB/s")]
f, ax = plt.subplots(figsize=(COL, 2.4))
w = 0.27
for j, (arm, col) in enumerate([("v1", GREY), ("v2", BLUE), ("drop", ORANGE)]):
    for i, (s, _) in enumerate(order):
        b = sc[s][arm]["vs_best_fixed"]
        x = i + (j - 1) * w
        ax.bar(x, b["geo_ratio"] - 1, w * 0.9, bottom=1, color=col)
        ax.plot([x, x], b["ci95"], color="#222", lw=0.6)
ax.axhline(1, color=DARK, lw=0.8)
ax.axvspan(1.5, 5.5, color="#eef1f6", lw=0, zorder=0)
ax.text(3.5, 1.40, "held out (never trained on)", ha="center", fontsize=7, color=DARK)
ax.set_xticks(range(len(order)), [o[1] for o in order])
ax.set_ylabel("time ÷ best fixed 1 MiB")
ax.set_ylim(0.72, 1.46)
ax.legend(handles=[Patch(color=GREY, label="first retrained"), Patch(color=BLUE, label="broadly trained"),
                   Patch(color=ORANGE, label="drop rule")],
          frameon=False, loc="lower left", bbox_to_anchor=(0.0, 1.0), ncol=3, fontsize=6.5)
save(f, "fig6_broad_training.png")
print("paper figures in", OUT)
