#!/usr/bin/env python3
"""Print-sized versions of the three wide manuscript figures for the 7-page cut.

`reports/ieee_access/make_figures.py` draws figs 1-3 at 7.2 in for a full-width
placement. The short conference paper places them in one column, so they were
scaled to half size and their 7 pt labels became unreadable. This script redraws
the same data at 3.45 in with 7-7.5 pt text.

Output: reports/paper/figures/fig1_ours_vs_aria2.png,
        reports/paper/figures/fig3_within_download_controller.png

Usage: python3 reports/paper/make_conf_figs.py   (from the repository root)
"""
import glob
import json
import math
import os

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
T = os.path.join(ROOT, "testdata")
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "figures")
os.makedirs(OUT, exist_ok=True)
geo = lambda xs: math.exp(sum(map(math.log, xs)) / len(xs))
COL = 3.45

plt.rcParams.update({
    "font.family": "DejaVu Sans", "font.size": 7.5, "axes.labelsize": 7.5,
    "xtick.labelsize": 7, "ytick.labelsize": 7, "legend.fontsize": 7,
    "axes.spines.top": False, "axes.spines.right": False,
    "axes.edgecolor": "#8a8a85", "axes.linewidth": 0.6,
})

# ---- Fig 1: ours/aria2 ratio across D2, sweep schedules, NGINX ------------
rows = []
fr = os.path.join(T, "aria2_competitive_benchmark", "final_reproduction")
r = []
for i in range(60):
    po, pa = os.path.join(fr, f"final-p{i:02d}-ours.json"), os.path.join(fr, f"final-p{i:02d}-aria2.json")
    if os.path.exists(po) and os.path.exists(pa):
        r.append(json.load(open(po))["completion_ms"] / json.load(open(pa))["completion_ms"])
rows.append(("D2 (30 pairs)", r, "D2"))

m = json.load(open(os.path.join(T, "d2_pacing_sweep", "manifest", "sweep_manifest.json")))
short = {"S0_D2_reference": "D2 reference", "S1_no_dip": "S1 no dip", "S2_long_dip": "S2 long dip",
         "S3_partial_recovery": "S3 partial", "S4_constant_fast": "S4 constant fast",
         "S5_constant_slow": "S5 constant slow"}
for s, spec in m["schedules_kib_per_s"].items():
    r = []
    for p in range(m["pairs_per_schedule"]):
        a = json.load(open(os.path.join(T, "d2_pacing_sweep", "runs", f"sw-{s[:2]}-p{p:02d}-aria2.json")))
        o = json.load(open(os.path.join(T, "d2_pacing_sweep", "runs", f"sw-{s[:2]}-p{p:02d}-ours.json")))
        r.append(o["completion_ms"] / a["completion_ms"])
    rows.append((short.get(s, "sweep " + s), r, "sweep"))

m = json.load(open(os.path.join(T, "nginx_limit_rate", "manifest", "nginx_manifest.json")))
for c in m["configs"]:
    r = []
    for p in range(m["pairs_per_config"]):
        a = json.load(open(os.path.join(T, "nginx_limit_rate", "runs", f"ng-{c[:2]}-p{p:02d}-aria2.json")))
        o = json.load(open(os.path.join(T, "nginx_limit_rate", "runs", f"ng-{c[:2]}-p{p:02d}-ours.json")))
        r.append(o["completion_ms"] / a["completion_ms"])
    rows.append(("NGINX " + c.replace("_limit", "").replace("_constant", "").replace("_burst_then_pace", " burst"), r, "nginx"))

col = {"D2": "#1f77b4", "sweep": "#2ca02c", "nginx": "#d62728"}
fig, ax = plt.subplots(figsize=(COL, 3.2))
for i, (lab, r, g) in enumerate(rows):
    y = len(rows) - i
    ax.scatter(r, [y] * len(r), s=9, color=col[g], alpha=0.7, lw=0)
    ax.plot([geo(r)] * 2, [y - 0.3, y + 0.3], color="k", lw=1.2)
ax.axvline(1.0, ls="--", color="gray", lw=0.8)
ax.set_yticks([len(rows) - i for i in range(len(rows))])
ax.set_yticklabels([r[0] for r in rows])
ax.set_xlabel("ratio ours / aria2  (< 1: ours faster)")
ax.set_xlim(0.3, 1.15)
ax.set_ylim(0.4, len(rows) + 0.6)
fig.tight_layout(pad=0.4)
fig.savefig(os.path.join(OUT, "fig1_ours_vs_aria2.png"), dpi=300)
plt.close(fig)

# ---- Fig 3: within-download controller vs fixed vs aria2 (P2) ------------
R = {}
for f in glob.glob(os.path.join(T, "n_dependent_server_joint_ns", "adaptive", "p2_*.json")):
    d = json.load(open(f))
    n, rr = os.path.basename(f)[:-5].rsplit("_r", 1)
    R.setdefault(n, []).append(d["completion_ms"] / 1000)
order = ["p2_causal_joint", "p2_causal_n_only", "p2_causal_s_only", "p2_fixed_16_1M",
         "p2_fixed_4_1M", "p2_fixed_4_4M", "p2_aria2"]
labels = ["joint", "N-only", "S-only", "16×1", "4×1", "4×4", "aria2"]
fig, ax = plt.subplots(figsize=(COL, 2.8))
for i, n in enumerate(order):
    ax.scatter([i] * len(R[n]), R[n], s=9, color="#d62728" if n.endswith("aria2") else "#1f77b4",
               alpha=0.7, lw=0)
    ax.plot([i - .3, i + .3], [sum(R[n]) / len(R[n])] * 2, color="k", lw=1.2)
ax.set_xticks(range(len(order)))
ax.set_xticklabels(labels)
ax.set_ylabel("completion time (s)")
ax.set_xlabel("cap switches on at 8 s; 10 paired blocks")
ax.set_ylim(0, max(max(v) for v in R.values()) * 1.12)
fig.tight_layout(pad=0.4)
fig.savefig(os.path.join(OUT, "fig3_within_download_controller.png"), dpi=300)
plt.close(fig)
print("wrote", os.path.join(OUT, "fig1_ours_vs_aria2.png"))
print("wrote", os.path.join(OUT, "fig3_within_download_controller.png"))
