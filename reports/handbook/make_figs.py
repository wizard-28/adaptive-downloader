#!/usr/bin/env python3
"""New figures for the team handbook, drawn from committed analysis files.

Usage: python3 reports/handbook/make_figs.py   (run from the repository root)
Figures copied from reports/review2, reports/review3 and reports/ieee_access
are not regenerated here.
"""
import json
import math
import os

import matplotlib
import matplotlib.ticker

matplotlib.use("Agg")
import matplotlib.pyplot as plt

OUT = "reports/handbook/figs"
os.makedirs(OUT, exist_ok=True)
BLUE, ORANGE, GREEN, GREY, DARK, RED = "#2f6fd6", "#e07b28", "#2e9e6a", "#b9b8b0", "#55544e", "#c8423b"
plt.rcParams.update({"font.family": "DejaVu Sans", "font.size": 10, "axes.spines.top": False,
                     "axes.spines.right": False, "axes.edgecolor": "#8a8a85", "axes.titleweight": "bold",
                     "axes.titlesize": 11, "axes.titlelocation": "left"})
MIB = 1024 * 1024


def load(p):
    with open(p) as f:
        return json.load(f)


def save(fig, name):
    fig.tight_layout()
    fig.savefig(os.path.join(OUT, name), dpi=200)
    plt.close(fig)


# 1. Soft cap: per-request and aggregate rate against k ----------------------
C, gamma, base = 8 * MIB, 0.05, 2500 * 1024
ks = list(range(1, 17))
per = [min(base, C / (k * (1 + gamma * (k - 1)))) for k in ks]
agg = [k * p for k, p in zip(ks, per)]
fig, ax = plt.subplots(figsize=(7.0, 3.3))
ax.plot(ks, [p / MIB for p in per], "o-", color=BLUE, label="rate of one new request")
ax.plot(ks, [a / MIB for a in agg], "s-", color=ORANGE, label="total if all k run at that rate")
ax.axhline(base / MIB, color=GREY, ls=":", lw=1)
ax.text(16.2, base / MIB, "base\n2500 KiB/s", va="center", fontsize=8, color=DARK)
ax.set_xlabel("k = active requests when a new one arrives (including it)")
ax.set_ylabel("MiB/s")
ax.set_xticks(ks)
ax.set_title("Soft cap, C = 8 MiB/s, γ = 0.05: beyond a few connections the total falls")
ax.legend(frameon=False, fontsize=9)
save(fig, "cap_curve.png")

# 2. Arrival-time rule: why long ranges started before the cap win -----------
fig, axs = plt.subplots(1, 2, figsize=(7.4, 2.9), sharey=True)
for ax, (title, size_s, n) in zip(axs, [("16 × 32 MiB: all start before the cap", 52, 16),
                                         ("16 × 1 MiB: most start after the cap", None, 16)]):
    ax.axvspan(8, 60, color="#fbe3d6", lw=0)
    ax.text(59, -1.3, "cap on from 8 s →", fontsize=8, color=RED, ha="right")
    if size_s:
        for i in range(n):
            ax.barh(i, size_s - 4 + (i % 4), left=0.2 * i / n, color=BLUE, height=0.7)
    else:
        t = 0.0
        for i in range(n):
            x = 0.0
            while x < 58:
                d = 0.45 if x < 8 else 1.9
                ax.barh(i, d * 0.92, left=x, color=BLUE if x < 8 else GREY, height=0.7)
                x += d
    ax.set_title(title, fontsize=9.5)
    ax.set_xlabel("time (s)")
    ax.set_xlim(0, 60)
    ax.set_yticks([])
axs[0].set_ylabel("connection")
axs[1].text(30, n + 0.2, "blue: started before the cap (fast rate)   grey: started under the cap",
            fontsize=7.5, ha="center", color=DARK)
fig.suptitle("A request keeps the rate it had when it arrived (schematic)", x=0.02, ha="left",
             fontweight="bold", fontsize=11)
save(fig, "arrival_rule.png")

# 3. Cellular-trace oracle headroom ------------------------------------------
traces = [("Verizon EVDO", 0.0164075465), ("Verizon LTE", 0.0001943981), ("AT&T LTE", 0.0001563742),
          ("T-Mobile LTE", 0.0006142775), ("T-Mobile UMTS", 0.0029383433)]
gate = load("testdata/final_oracle_analysis/final_statistical_gate.json") if os.path.exists(
    "testdata/final_oracle_analysis/final_statistical_gate.json") else None
fig, ax = plt.subplots(figsize=(7.0, 3.0))
ax.bar([t for t, _ in traces], [h * 100 for _, h in traces], color=BLUE)
for i, (_, h) in enumerate(traces):
    ax.text(i, h * 100 + 0.25, f"{h * 100:.3f} %", ha="center", fontsize=8.5)
ax.axhline(10, color=RED, ls="--", lw=1.2)
ax.text(4.45, 10.3, "predeclared bar: 10 %", ha="right", fontsize=8.5, color=RED)
ax.axhline(-0.26, color=DARK, ls=":", lw=1)
ax.text(4.45, -1.2, "one-sided 95 % lower bound: −0.26 %", ha="right", fontsize=8.5, color=DARK)
ax.set_ylim(-2, 11.5)
ax.set_ylabel("oracle headroom over best fixed N (%)")
ax.set_title("Capacity swings 5–27×, yet a clairvoyant oracle gains < 2 %")
save(fig, "oracle.png")

# 4. Every adaptive or learned arm against its best fixed setting ------------
rows = [
    ("Causal joint controller, CAP8, original engine\n(vs best of 3 small-range fixed)", 0.766, 0.762, 0.769, "orig"),
    ("Causal joint controller, CAP8, fixed engine\n(vs 16 × 32 MiB)", 1.071, 1.054, 1.085, "fix"),
    ("Workload selector, six workloads\n(vs per-workload best fixed)", 0.999, 0.969, 1.029, "orig"),
    ("Workload selector, CAP0, fixed engine\n(vs 16 × 32 MiB)", 1.229, 1.223, 1.234, "fix"),
    ("Online tuner (FastBioDL-style), CAP8\n(vs 16 × 1 MiB)", 1.212, 1.180, 1.248, "fix"),
    ("Online tuner, no cap", 1.220, 1.203, 1.232, "fix"),
    ("Frozen DQN v6, CAP8, start N = 16\n(vs 16 × 32 MiB)", 1.356, 1.252, 1.472, "fix"),
    ("Cap-trained DQN v1, CAP8\n(vs 16 × 1 MiB)", 0.907, 0.893, 0.921, "rl"),
    ("Cap-trained DQN v1, CAP8\n(vs 16 × 32 MiB)", 1.298, 1.274, 1.321, "rl"),
    ("Joint N+S DQN, CAP8\n(vs 16 × 32 MiB)", 1.471, 1.405, 1.546, "rl"),
    ("DQN v2, CAP8\n(vs 16 × 1 MiB)", 0.845, 0.840, 0.850, "rl"),
]
fig, ax = plt.subplots(figsize=(7.6, 6.6))
for i, (lab, r, lo, hi, kind) in enumerate(reversed(rows)):
    col = GREEN if hi < 1 else (RED if lo > 1 else DARK)
    ax.plot([lo, hi], [i, i], color=col, lw=2.2)
    ax.plot(r, i, "o", color=col, ms=6)
    ax.text(hi + 0.012, i, f"{r:.3f}", va="center", fontsize=8.5)
ax.axvline(1, color=DARK, ls="--", lw=1)
ax.set_yticks(range(len(rows)), [r[0] for r in reversed(rows)], fontsize=8.2)
ax.set_xlabel("time ratio R = arm ÷ comparison (95 % interval);  R < 1: arm faster")
ax.set_title("Selected adaptive and learned arms against fixed settings")
ax.set_xlim(0.7, 1.62)
save(fig, "forest.png")

# 5. v2 study: v1, v2 and drop rule against the best fixed 1 MiB arm ---------
c = load("testdata/rl_cap_train_v2/analysis/C.json")["scenarios"]
d = load("testdata/rl_cap_train_v2/analysis/D.json")["scenarios"]
sc = {**c, **d}
order = ["CAP8", "HEALTHY", "H_C12", "H_RTT110", "H_DIP", "H_C20"]
fig, ax = plt.subplots(figsize=(7.6, 3.6))
w = 0.26
for j, (arm, col) in enumerate([("v1", GREY), ("v2", BLUE), ("drop", ORANGE)]):
    for i, s in enumerate(order):
        b = sc[s][arm]["vs_best_fixed"]
        r, lo, hi = b["geo_ratio"], b["ci95"][0], b["ci95"][1]
        x = i + (j - 1) * w
        ax.bar(x, r - 1, w * 0.9, bottom=1, color=col, label=arm if i == 0 else None)
        ax.plot([x, x], [lo, hi], color="#222", lw=0.9)
ax.axhline(1, color=DARK, lw=1)
ax.set_xticks(range(len(order)), ["CAP8\n(train)", "HEALTHY\n(no cap)", "H_C12", "H_RTT110", "H_DIP", "H_C20"])
ax.set_ylabel("time ÷ best fixed 1 MiB arm")
ax.set_title("DQN v2 study (8 blocks): bars below 1 are faster than the best fixed setting")
from matplotlib.patches import Patch
ax.legend(handles=[Patch(color=GREY, label="v1 (first cap-trained agent)"),
                   Patch(color=BLUE, label="v2 (broader training)"),
                   Patch(color=ORANGE, label="drop rule (C = 8-tuned)")],
          frameon=False, fontsize=8.5, ncol=3, loc="upper left")
ax.set_ylim(0.7, 1.42)
save(fig, "v2.png")

# 6. CAP8 completion times across studies -------------------------------------
j = load("testdata/rl_joint/analysis/C.json")["scenarios"]["CAP8"]["geo_mean_s"]
v = c["CAP8"]["geo_mean_s"]
bars = [("fixed 16 × 32 MiB (joint study)", j["fixed_16_32M"], DARK),
        ("aria2 (joint study)", j["aria2"], DARK),
        ("drop rule (v2 study)", v["drop"], ORANGE),
        ("DQN v2 (v2 study)", v["v2"], BLUE),
        ("DQN v1 (v2 study)", v["v1"], GREY),
        ("joint N+S DQN (joint study)", j["dqnj"], GREY),
        ("fixed 16 × 1 MiB (v2 study)", v["fixed_16_1M"], DARK)]
fig, ax = plt.subplots(figsize=(7.4, 3.4))
for i, (lab, t, col) in enumerate(reversed(bars)):
    ax.barh(i, t, color=col, height=0.65)
    ax.text(t + 0.6, i, f"{t:.1f} s", va="center", fontsize=8.5)
ax.set_yticks(range(len(bars)), [b[0] for b in reversed(bars)], fontsize=8.8)
ax.set_xlabel("completion time under CAP8 (s, geometric mean)")
ax.set_title("Cap from 8 s: large ranges still win (two studies, not paired)")
ax.set_xlim(0, 88)
save(fig, "cap8_times.png")

# 7. Joint study: agent against best fixed per scenario ----------------------
jc = load("testdata/rl_joint/analysis/C.json")["scenarios"]
fig, ax = plt.subplots(figsize=(7.0, 3.0))
names = ["CAP8", "HEALTHY", "CAP0", "D2"]
best = {"CAP8": "fixed_16_32M", "HEALTHY": "fixed_16_1M", "CAP0": "fixed_16_32M", "D2": "fixed_16_5M"}
for i, s in enumerate(names):
    g = jc[s]["geo_mean_s"]
    ax.bar(i - 0.18, g[best[s]], 0.34, color=DARK, label="best fixed" if i == 0 else None)
    ax.bar(i + 0.18, g["dqnj"], 0.34, color=BLUE, label="joint DQN" if i == 0 else None)
    ax.text(i + 0.18, g["dqnj"] + 1.5, f"×{g['dqnj'] / g[best[s]]:.2f}", ha="center", fontsize=8.5)
ax.set_xticks(range(4), [f"{s}\n(best: {best[s].replace('fixed_', '').replace('_', ' × ')})" for s in names],
              fontsize=8.5)
ax.set_ylabel("completion time (s)")
ax.set_title("Joint N + range-size agent: slower than the best fixed setting in 4 of 4")
ax.legend(frameon=False, fontsize=8.5)
save(fig, "joint.png")

# 8. Mirror check -------------------------------------------------------------
m = load("testdata/live_mirror_check/analysis/M2.json")["servers"]
fig, ax = plt.subplots(figsize=(7.0, 3.1))
for i, s in enumerate(["kcdn", "kedge", "riken"]):
    for k, (key, col, lab) in enumerate([("v2_vs_best_fixed", BLUE, "DQN v2"),
                                         ("drop_vs_best_fixed", ORANGE, "drop rule")]):
        b = m[s][key]
        x = i + (k - 0.5) * 0.3
        ax.plot([b["ci95"][0], b["ci95"][1]], [x, x], color=col, lw=2)
        ax.plot(b["geo_ratio"], x, "o", color=col, label=lab if i == 0 else None)
        ax.text(b["ci95"][1] + 0.03, x, f"{b['geo_ratio']:.3f}", va="center", fontsize=8.5)
ax.axvline(1, color=DARK, ls="--", lw=1)
ax.axvline(1.05, color=RED, ls=":", lw=1)
ax.text(1.06, 2.45, "no-harm line 1.05", fontsize=8, color=RED)
ax.set_yticks(range(3), ["kcdn\n(cdn.kernel.org)", "kedge\n(mirrors.edge)", "riken\n(ftp.riken.jp)"])
ax.set_xlabel("time ÷ best fixed 1 MiB setting (95 % bootstrap, 5 blocks)")
ax.set_title("Real mirrors: both adaptive arms above the no-harm line (slot M2)")
ax.legend(frameon=False, fontsize=8.5, loc="lower right")
save(fig, "mirror.png")

# 9. Geometric vs arithmetic mean illustration --------------------------------
fig, ax = plt.subplots(figsize=(6.6, 2.6))
ratios = [0.5, 2.0]
ax.bar(["pair 1: half the time", "pair 2: twice the time"], ratios, color=[GREEN, RED])
ax.axhline(sum(ratios) / 2, color=ORANGE, ls="--")
ax.text(1.45, 1.27, "arithmetic mean 1.25 (biased)", fontsize=8.5, color=ORANGE, ha="right")
ax.axhline(math.sqrt(ratios[0] * ratios[1]), color=BLUE, ls="-")
ax.text(1.45, 0.92, "geometric mean 1.00 (symmetric)", fontsize=8.5, color=BLUE, ha="right")
ax.set_ylabel("time ratio")
ax.set_title("Why ratios are averaged geometrically")
save(fig, "geomean.png")

print("figures written to", OUT)


# 10. Mahimahi trace capacity over time (downlink, 1 s bins) -------------------
TR = "mahimahi-clone-dir/mahimahi/traces"


def trace_mbps(name):
    with open(os.path.join(TR, name + ".down")) as f:
        ts = [int(x) for x in f if x.strip()]
    secs = ts[-1] // 1000 + 1
    bins = [0] * secs
    for t in ts:
        bins[t // 1000] += 1
    return [b * 1500 * 8 / 1e6 for b in bins]  # each opportunity = one 1500-byte packet


def pct(v, q):
    s = sorted(v)
    return s[min(len(s) - 1, int(q * len(s)))]


if os.path.isdir(TR):
    names = ["Verizon-EVDO-driving", "Verizon-LTE-driving", "ATT-LTE-driving", "TMobile-LTE-driving",
             "TMobile-UMTS-driving"]
    swing = {}
    for n in names:
        v = trace_mbps(n)
        lo = pct(v, 0.10)
        swing[n] = (pct(v, 0.90) / lo) if lo > 0 else float("inf")
    print("p90/p10 per trace:", {k: round(x, 1) for k, x in swing.items()})
    show = [("Verizon-LTE-driving", "Verizon LTE", BLUE), ("TMobile-UMTS-driving", "T-Mobile UMTS", ORANGE),
            ("ATT-LTE-driving", "AT&T LTE", GREEN)]
    fig, axs = plt.subplots(len(show), 1, figsize=(7.6, 4.6), sharex=True)
    for ax, (n, lab, col) in zip(axs, show):
        v = trace_mbps(n)[:300]
        ax.fill_between(range(len(v)), v, color=col, alpha=0.25, lw=0)
        ax.plot(v, color=col, lw=1.1)
        ax.set_ylabel("Mbit/s", fontsize=8.5)
        ax.text(0.99, 0.82, f"{lab}: p90/p10 = {swing[n]:.1f}×", transform=ax.transAxes, ha="right",
                fontsize=9, fontweight="bold", color=DARK)
        ax.set_ylim(0, max(v) * 1.15)
    axs[-1].set_xlabel("time (s), first 5 minutes of each recorded drive")
    axs[0].set_title("Recorded cellular link capacity, 1 s bins (Mahimahi traces)")
    save(fig, "trace_capacity.png")

# 11. Adaptive arms and our fixed setting against aria2 ------------------------
rows = [("Rate dips, then rises (D2)", [("rule-based joint", 0.876)], 0.376),
        ("Rate rises at 1 s, no cap", [("DQN v6", 0.359), ("online tuner", 0.432)], 0.344),
        ("Cap switches on at 8 s", [("causal", 1.096), ("cap-trained v1", 1.310), ("DQN v6", 1.367),
                                    ("joint agent", 1.422), ("online tuner", 1.781)], 1.016),
        ("Collapse, spike, loss, throttle", [("DQN v6", 1.456)], None),
        ("Six capped workloads", [("selector", 1.660)], None)]
LABELS = {"Rate dips, then rises (D2)": "rule-based joint 0.876",
          "Rate rises at 1 s, no cap": "DQN v6 0.359, online tuner 0.432",
          "Cap switches on at 8 s": "causal 1.096 · learned agents 1.31–1.42 · tuner 1.78",
          "Collapse, spike, loss, throttle": "DQN v6 1.456",
          "Six capped workloads": "selector 1.660"}
fig, ax = plt.subplots(figsize=(8.2, 4.4))
ax.axvspan(0.25, 1.0, color="#e8f0fb", lw=0)
ax.axvspan(1.0, 2.2, color="#fbeee4", lw=0)
for i, (lab, arms, fixed) in enumerate(reversed(rows)):
    y = i
    for j, (name, r) in enumerate(arms):
        ax.plot(r, y, "o", color=ORANGE, ms=8, zorder=3)
    hi = max(r for _, r in arms)
    ax.text(hi if hi > 1 else min(r for _, r in arms), y - 0.2, LABELS[lab], ha="right" if hi > 1 else "left",
            va="top", fontsize=8, color=DARK)
    if fixed:
        ax.text(fixed, y + 0.16, f"fixed {fixed:.3f}", ha="center", va="bottom", fontsize=8, color=BLUE)
    if fixed:
        ax.plot(fixed, y, "D", color=BLUE, ms=8, zorder=3)
ax.axvline(1, color=DARK, ls="--", lw=1)
ax.set_xscale("log")
ax.set_xticks([0.3, 0.5, 0.7, 1, 1.5, 2])
ax.set_xticklabels(["0.3", "0.5", "0.7", "1", "1.5", "2"])
ax.xaxis.set_minor_formatter(matplotlib.ticker.NullFormatter())
ax.set_xlim(0.28, 2.1)
ax.set_yticks(range(len(rows)), [r[0] for r in reversed(rows)])
ax.set_ylim(-0.6, len(rows) - 0.3)
ax.text(0.30, len(rows) - 0.45, "faster than aria2", fontsize=8.5, color=BLUE)
ax.text(2.05, len(rows) - 0.45, "slower than aria2", fontsize=8.5, color="#a0521d", ha="right")
ax.set_xlabel("time ÷ aria2’s time (log scale)")
ax.plot([], [], "o", color=ORANGE, label="our adaptive arms")
ax.plot([], [], "D", color=BLUE, label="our best fixed setting")
ax.legend(frameon=False, fontsize=8.5, loc="lower left")
ax.set_title("When conditions change mid-download: ours against aria2")
save(fig, "vsaria2.png")

# 12. D2 request timeline from per-request server logs (pair p00) -------------
FR = "testdata/aria2_competitive_benchmark/final_reproduction"


def lanes(reqs):
    ends, out = [], []
    for r in sorted(reqs, key=lambda r: r["start_ms"]):
        for k, e in enumerate(ends):
            if e <= r["start_ms"]:
                ends[k] = r["end_ms"]
                out.append((k, r))
                break
        else:
            ends.append(r["end_ms"])
            out.append((len(ends) - 1, r))
    return out


if os.path.isdir(FR):
    fig, axs = plt.subplots(2, 1, figsize=(7.8, 4.4), sharex=True)
    for ax, sysname, title in [(axs[0], "aria2", "aria2: 16 big pieces, all started in the slow phase"),
                               (axs[1], "ours", "ours (16 × 4 MiB): most pieces start after the speed-up")]:
        reqs = [r for r in load(f"{FR}/final-p00-{sysname}.json")["server_requests"] if r["end_ms"] > r["start_ms"]]
        for k, r in lanes(reqs):
            fast = r["rate_bytes_per_second"] >= 2000 * 1024
            ax.barh(k, (r["end_ms"] - r["start_ms"]) / 1000, left=r["start_ms"] / 1000, height=0.75,
                    color=BLUE if fast else ORANGE, edgecolor="white", lw=0.3)
        ax.axvspan(0, 4, color="#fbeee4", lw=0, zorder=0)
        ax.set_title(title, fontsize=9.5)
        ax.set_yticks([])
        ax.set_ylabel("connection", fontsize=8.5)
    axs[1].set_xlabel("time (s) · shaded: slow phase (640 then 160 KiB/s) · after 4 s: 2,500 KiB/s")
    from matplotlib.patches import Patch
    axs[1].legend(handles=[Patch(color=ORANGE, label="request started in the slow phase"),
                           Patch(color=BLUE, label="request started after the speed-up")],
                  frameon=False, fontsize=8.5, loc="center right")
    fig.suptitle("D2, one paired run: each bar is one request, from the server's log", x=0.02, ha="left",
                 fontweight="bold", fontsize=11)
    save(fig, "d2_requests.png")
