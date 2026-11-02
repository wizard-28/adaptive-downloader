#!/usr/bin/env python3
"""Paper-sized system architecture figure (one IEEE column).

Same structure and component names as the handbook figure
(reports/handbook/figs/architecture.png): one Downloader coordinates five
parts, only the Fetcher talks to the network, and policies act only through
the controllers. Drawn in the style of the result figures: DejaVu Sans, thin
muted strokes, pale fills, white background. The handbook's command-line box
is left out; it names tools, not parts of the system.

Output: reports/paper/figures/architecture.png
"""
import os

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.patches import FancyArrowPatch, FancyBboxPatch

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "figures", "architecture.png")
os.makedirs(os.path.dirname(OUT), exist_ok=True)

INK = "#3f3f3a"
BLUE = "#2f6fd6"
PALE = "#eef4fb"
GREY = "#8a8a85"
LINE = "#b9b9b2"

plt.rcParams.update({"font.family": "DejaVu Sans", "axes.linewidth": 0.5})

fig, ax = plt.subplots(figsize=(3.45, 3.05))
ax.set_xlim(-0.08, 10.08)
ax.set_ylim(-0.05, 10.05)
ax.axis("off")
fig.patch.set_facecolor("white")

W, GAP = 1.8, 0.25
col = lambda i: i * (W + GAP)


def box(x, y, w, h, title, sub="", hl=False, fc="white"):
    ax.add_patch(FancyBboxPatch(
        (x, y), w, h, boxstyle="round,pad=0.01,rounding_size=0.12",
        fc=PALE if hl else fc, ec=BLUE if hl else LINE, lw=0.8 if hl else 0.6))
    if sub:
        ax.text(x + w / 2, y + h * 0.68, title, ha="center", va="center",
                fontsize=5.4, color=INK, weight="bold")
        ax.text(x + w / 2, y + h * 0.28, sub, ha="center", va="center",
                fontsize=5.1, color=INK, linespacing=1.1)
    else:
        ax.text(x + w / 2, y + h / 2, title, ha="center", va="center",
                fontsize=5.4, color=INK, weight="bold")


def arrow(p, q, color=GREY):
    ax.add_patch(FancyArrowPatch(p, q, arrowstyle="-|>", mutation_scale=4,
                                 lw=0.5, color=color, shrinkA=0, shrinkB=0))


# Downloader
box(0, 8.75, 10, 1.2, "Downloader",
    "owns one download; converts half-open tracker ranges to inclusive HTTP ranges",
    hl=True)

# Row of five parts
y2, h2 = 6.1, 1.75
parts = [
    ("RangeTracker", "half-open ranges\nallocate, split"),
    ("Worker pool", "N workers\nretry in place"),
    ("Assembler", "inclusive ranges\nWriteAt offset"),
    ("Metrics", "one record\nper attempt"),
    ("Policy runner", "one decision\nat a time"),
]
for i, (t, s) in enumerate(parts):
    box(col(i), y2, W, h2, t, s)
    arrow((col(i) + W / 2, 8.75), (col(i) + W / 2, y2 + h2))

# Third row
y3, h3 = 3.75, 1.75
box(col(1), y3, W, h3, "Fetcher", "one Range request\nchecks 206 reply")
box(col(3), y3, W, h3, "Controllers", "target N and S\nread by the pool")
box(col(4), y3, W, h3, "Policies", "fixed, rules,\nLinUCB, DQN")
arrow((col(1) + W / 2, y2), (col(1) + W / 2, y3 + h3))
arrow((col(4) + W / 2, y2), (col(4) + W / 2, y3 + h3))
# runner -> controllers, elbow
ax.plot([col(4) + 0.45, col(4) + 0.45, col(3) + W / 2], [y2, 5.75 + 0.0, 5.75],
        color=GREY, lw=0.5, solid_capstyle="butt")
arrow((col(3) + W / 2, 5.75), (col(3) + W / 2, y3 + h3))

# Network bar
yn = 2.2
ax.add_patch(FancyBboxPatch((0, yn), 10, 0.75, boxstyle="round,pad=0.01,rounding_size=0.12",
                            fc="#ececea", ec=LINE, lw=0.6))
ax.text(5, yn + 0.375, "Network: netem on loopback, 80 ms round trip, 0.4 % loss; or live internet",
        ha="center", va="center", fontsize=5.1, color=INK)
arrow((col(1) + W / 2, y3), (col(1) + W / 2, yn + 0.75))

# Servers
ys, hs = 0.1, 1.45
box(0.4, ys, 4.6, hs, "HTTP Range server", "test server: pacing, soft cap,\nor a public mirror (live pilot)")
box(5.4, ys, 4.6, hs, "aria2 1.37 (baseline)", "same server, same network,\nfrozen flags -x16 -s16 -k1M")
arrow((col(1) + W / 2, yn), (col(1) + W / 2, ys + hs))
arrow((5.4, ys + hs / 2), (5.0, ys + hs / 2))

fig.subplots_adjust(0, 0, 1, 1)
fig.savefig(OUT, dpi=400, facecolor="white", bbox_inches="tight", pad_inches=0.02)
print("wrote", OUT)
