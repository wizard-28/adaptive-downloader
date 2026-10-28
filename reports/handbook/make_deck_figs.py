#!/usr/bin/env python3
"""Slide-size figures for the presentation deck, drawn from committed data.

Each figure is sized to the box it fills on a 1920 x 1080 slide (DPI 150, so
1 pt = 2.1 px on the slide) with 14-17 pt text, and has no chart title: the
slide's heading does that job. Output: reports/handbook/figs_deck/.

Usage: python3 reports/handbook/make_deck_figs.py   (from the repository root)
"""
import glob
import json
import math
import os
import statistics as stats

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import matplotlib.ticker
from matplotlib.patches import Patch

OUT = "reports/handbook/figs_deck"
os.makedirs(OUT, exist_ok=True)
DPI = 150
BLUE, ORANGE, GREEN, GREY, DARK, RED = "#2f6fd6", "#e07b28", "#2e9e6a", "#b9b8b0", "#55544e", "#c8423b"
plt.rcParams.update({"font.family": "DejaVu Sans", "font.size": 15, "axes.labelsize": 15,
                     "xtick.labelsize": 14, "ytick.labelsize": 14, "legend.fontsize": 14,
                     "axes.spines.top": False, "axes.spines.right": False, "axes.edgecolor": "#8a8a85"})
MIB, KIB = 1024 * 1024, 1024
T = "testdata"


def load(p):
    with open(p) as f:
        return json.load(f)


def fig(w_px, h_px):
    return plt.subplots(figsize=(w_px / DPI, h_px / DPI))


def save(f, name):
    f.tight_layout()
    f.savefig(os.path.join(OUT, name), dpi=DPI)
    plt.close(f)


# --- platform: D2 schedule and soft cap (800 x 400 each) ---------------------
f, ax = fig(800, 400)
ts = [0, 2, 2, 4, 4, 10]
rs = [640, 640, 160, 160, 2500, 2500]
ax.plot(ts, rs, color=BLUE, lw=3)
ax.fill_between(ts, rs, color=BLUE, alpha=0.12)
ax.axvspan(0, 4, color="#fbeee4", lw=0, zorder=0)
ax.text(7, 1150, "requests started in the\nshaded slow phase\nkeep the slow rate", ha="center", fontsize=14, color="#a0521d")
ax.set_xlabel("time since download start (s)")
ax.set_ylabel("KiB/s")
ax.set_xlim(0, 10)
ax.set_ylim(0, 2800)
save(f, "schedule.png")

C, gamma, base = 8 * MIB, 0.05, 2500 * KIB
ks = list(range(1, 17))
per = [min(base, C / (k * (1 + gamma * (k - 1)))) / MIB for k in ks]
f, ax = fig(800, 400)
ax.plot(ks, [k * p for k, p in zip(ks, per)], "s-", color=ORANGE, lw=2.5, ms=6, label="total, k requests")
ax.plot(ks, per, "o-", color=BLUE, lw=2.5, ms=6, label="one new request")
ax.set_xlabel("k = active requests (incl. the new one)")
ax.set_ylabel("MiB/s")
ax.set_xticks([1, 4, 8, 12, 16])
ax.legend(frameon=False, loc="lower left", bbox_to_anchor=(0.0, 1.0), ncol=2, fontsize=13)
save(f, "cap_curve.png")

# --- r1: D2 progress and per-request timeline (816 x 430 each) ---------------
FR = f"{T}/aria2_competitive_benchmark/final_reproduction"


def reqs(sysname):
    return [r for r in load(f"{FR}/final-p00-{sysname}.json")["server_requests"] if r["end_ms"] > r["start_ms"]]


f, ax = fig(816, 430)
grid = [x / 4 for x in range(0, 57 * 4)]
for sysname, col, lab in [("ours", BLUE, "ours, 16 × 4 MiB"), ("aria2", ORANGE, "aria2")]:
    rq = reqs(sysname)
    tot = sum((r["range_end"] - r["range_start"] + 1) if r["has_range"] else 0 for r in rq)
    ys = []
    for t in grid:
        ms = t * 1000
        b = 0.0
        for r in rq:
            if not r["has_range"]:
                continue
            size = r["range_end"] - r["range_start"] + 1
            frac = min(1.0, max(0.0, (ms - r["start_ms"]) / (r["end_ms"] - r["start_ms"])))
            b += frac * size
        ys.append(100 * b / tot)
    ax.plot(grid, ys, color=col, lw=3, label=lab)
ax.set_xlabel("time (s)")
ax.set_ylabel("file received (%)")
ax.set_ylim(0, 105)
ax.legend(frameon=False, loc="lower right")
save(f, "progress.png")


def lanes(rq):
    ends, out = [], []
    for r in sorted(rq, key=lambda r: r["start_ms"]):
        for k, e in enumerate(ends):
            if e <= r["start_ms"]:
                ends[k] = r["end_ms"]
                out.append((k, r))
                break
        else:
            ends.append(r["end_ms"])
            out.append((len(ends) - 1, r))
    return out


f, axs = plt.subplots(2, 1, figsize=(872 / DPI, 430 / DPI), sharex=True)
for ax, sysname, lab in [(axs[0], "aria2", "aria2"), (axs[1], "ours", "ours")]:
    for k, r in lanes(reqs(sysname)):
        fast = r["rate_bytes_per_second"] >= 2000 * KIB
        ax.barh(k, (r["end_ms"] - r["start_ms"]) / 1000, left=r["start_ms"] / 1000, height=0.8,
                color=BLUE if fast else ORANGE, edgecolor="white", lw=0.3)
    ax.axvspan(0, 4, color="#fbeee4", lw=0, zorder=0)
    ax.set_yticks([])
    ax.set_ylabel(lab, rotation=0, ha="right", va="center")
axs[1].set_xlabel("time (s); each bar is one request")
axs[1].legend(handles=[Patch(color=ORANGE, label="started slow"), Patch(color=BLUE, label="started fast")],
              frameon=False, loc="center right", fontsize=13)
save(f, "d2_requests.png")

# --- r1b: ours / aria2 across D2, sweep, NGINX (1664 x 400) -------------------
rows = []
r = []
for i in range(60):
    po, pa = f"{FR}/final-p{i:02d}-ours.json", f"{FR}/final-p{i:02d}-aria2.json"
    if os.path.exists(po) and os.path.exists(pa):
        r.append(load(po)["completion_ms"] / load(pa)["completion_ms"])
rows.append(("benchmark\n(30 pairs)", r, BLUE))
m = load(f"{T}/d2_pacing_sweep/manifest/sweep_manifest.json")
for s in m["schedules_kib_per_s"]:
    r = [load(f"{T}/d2_pacing_sweep/runs/sw-{s[:2]}-p{p:02d}-ours.json")["completion_ms"] /
         load(f"{T}/d2_pacing_sweep/runs/sw-{s[:2]}-p{p:02d}-aria2.json")["completion_ms"]
         for p in range(m["pairs_per_schedule"])]
    SCHED = {"S0": "slow, dip,\nthen fast", "S1": "slow,\nthen fast", "S2": "long\nslow dip",
             "S3": "partial\nrecovery", "S4": "always\nfast", "S5": "always\nslow"}
    rows.append((SCHED[s.split("_", 1)[0]], r, GREEN))
m = load(f"{T}/nginx_limit_rate/manifest/nginx_manifest.json")
for c in m["configs"]:
    r = [load(f"{T}/nginx_limit_rate/runs/ng-{c[:2]}-p{p:02d}-ours.json")["completion_ms"] /
         load(f"{T}/nginx_limit_rate/runs/ng-{c[:2]}-p{p:02d}-aria2.json")["completion_ms"]
         for p in range(m["pairs_per_config"])]
    rows.append(({"N1": "NGINX\nrate limit", "N2": "NGINX, first\n1 MiB free"}[c[:2]], r, RED))
f, ax = fig(1664, 430)
for i, (lab, r, col) in enumerate(rows):
    ax.scatter([i] * len(r), r, color=col, s=40, alpha=0.6, zorder=2)
    g = math.exp(stats.mean(math.log(x) for x in r))
    ax.plot([i - 0.3, i + 0.3], [g, g], color="#14213d", lw=3, zorder=3)
    ax.text(i, max(r + [g]) + 0.025, f"{g:.2f}", ha="center", va="bottom", fontsize=14, zorder=5,
            bbox=dict(fc="white", ec="none", pad=1.5))
ax.axhline(1, color=DARK, ls="--", lw=1.2)
ax.text(len(rows) - 0.5, 1.02, "parity", ha="right", va="bottom", fontsize=13, color=DARK)
ax.set_xticks(range(len(rows)), [r[0] for r in rows])
ax.set_ylabel("ours ÷ aria2 time")
ax.set_ylim(0.3, 1.1)
save(f, "fig1_ours_vs_aria2.png")

# --- r2a: trace capacity (1100 x 590) ----------------------------------------
TR = "mahimahi-clone-dir/mahimahi/traces"


def trace_mbps(name):
    with open(os.path.join(TR, name + ".down")) as fh:
        tt = [int(x) for x in fh if x.strip()]
    bins = [0] * (tt[-1] // 1000 + 1)
    for t in tt:
        bins[t // 1000] += 1
    return [b * 1500 * 8 / 1e6 for b in bins]


def p9010(v):
    s = sorted(v)
    lo = s[int(0.10 * len(s))]
    return s[min(len(s) - 1, int(0.90 * len(s)))] / lo


if os.path.isdir(TR):
    show = [("Verizon-LTE-driving", "Verizon LTE", BLUE), ("TMobile-UMTS-driving", "T-Mobile UMTS", ORANGE),
            ("ATT-LTE-driving", "AT&T LTE", GREEN)]
    f, axs = plt.subplots(3, 1, figsize=(1100 / DPI, 590 / DPI), sharex=True)
    for ax, (n, lab, col) in zip(axs, show):
        full = trace_mbps(n)
        v = full[:300]
        ax.fill_between(range(len(v)), v, color=col, alpha=0.25, lw=0)
        ax.plot(v, color=col, lw=1.6)
        ax.set_ylim(0, max(v) * 1.1)
        ax.set_title(f"{lab} · swing {p9010(full):.0f}×", loc="right", fontsize=14, fontweight="bold", color=DARK, pad=3)
        ax.yaxis.set_major_locator(matplotlib.ticker.MaxNLocator(3))
    axs[1].set_ylabel("Mbit/s")
    axs[-1].set_xlabel("time (s), first 5 minutes of each drive")
    save(f, "trace_capacity.png")

# --- r2: oracle headroom (1100 x 480) ----------------------------------------
H = [("Verizon\nEVDO", 1.641), ("Verizon\nLTE", 0.019), ("AT&T\nLTE", 0.016), ("T-Mobile\nLTE", 0.061),
     ("T-Mobile\nUMTS", 0.294)]
f, ax = fig(1100, 480)
ax.bar([h[0] for h in H], [h[1] for h in H], color=BLUE, width=0.6)
for i, (_, h) in enumerate(H):
    ax.text(i, h + 0.25, f"{h:.2f} %", ha="center", fontsize=15)
ax.axhline(10, color=RED, ls="--", lw=2)
ax.text(4.4, 9.4, "bar set in advance: 10 %", ha="right", va="top", fontsize=15, color=RED)
ax.set_ylim(0, 11)
ax.set_ylabel("oracle headroom (%)")
save(f, "oracle.png")

# --- r3: P2 within-download controller (1100 x 480) ---------------------------
R = {}
for fp in glob.glob(f"{T}/n_dependent_server_joint_ns/adaptive/p2_*.json"):
    n = os.path.basename(fp)[:-5].rsplit("_r", 1)[0]
    R.setdefault(n, []).append(load(fp)["completion_ms"] / 1000)
order = [("p2_aria2", "aria2", DARK), ("p2_causal_joint", "drop rule\nN and S", BLUE), ("p2_causal_n_only", "drop rule\nN only", BLUE),
         ("p2_fixed_16_1M", "fixed\n16×1 MiB", GREY), ("p2_fixed_4_1M", "fixed\n4×1 MiB", GREY),
         ("p2_fixed_4_4M", "fixed\n4×4 MiB", GREY), ("p2_causal_s_only", "drop rule\nS only", ORANGE)]
order = [o for o in order if o[0] in R]
f, ax = fig(1100, 480)
for i, (n, lab, col) in enumerate(order):
    g = math.exp(stats.mean(math.log(x) for x in R[n]))
    ax.bar(i, g, color=col, width=0.62)
    ax.text(i, g + 1.5, f"{g:.1f}", ha="center", fontsize=15)
ax.set_xticks(range(len(order)), [o[1] for o in order], fontsize=12)
ax.set_ylabel("completion time (s)")
ax.set_ylim(0, max(math.exp(stats.mean(math.log(x) for x in R[o[0]])) for o in order) * 1.18)
save(f, "fig3_within_download_controller.png")

# --- r3b: engine ablation (1100 x 500) ----------------------------------------
abl = load(f"{T}/split_ablation/analysis/summary.json")["scenarios"]
rows = [("cap from start", "CAP0", "fixed_16_1M_T30", "fixed_16_32M_NT_split"),
        ("cap from 8 s", "CAP8", "fixed_16_1M_T30", "fixed_16_32M_NT_split"),
        ("no cap", "HEALTHY", "fixed_16_1M_T30", "fixed_16_1M_NT_split"),
        ("slow, then fast", "D2", "fixed_16_4M_T30", "fixed_16_4M_NT_split")]
f, ax = fig(1100, 500)
w = 0.27
for i, (lab, sc, old, new) in enumerate(rows):
    for j, (arm, col) in enumerate([(old, GREY), (new, BLUE), ("aria2", DARK)]):
        v = abl[sc]["geo_mean_s"][arm]
        ax.bar(i + (j - 1) * w, v, w * 0.92, color=col)
        ax.text(i + (j - 1) * w, v + 2, f"{v:.0f}", ha="center", fontsize=13)
ax.set_xticks(range(len(rows)), [r[0] for r in rows])
ax.set_ylabel("completion time (s)")
ax.legend(handles=[Patch(color=GREY, label="original engine"), Patch(color=BLUE, label="fixed engine"),
                   Patch(color=DARK, label="aria2")], frameon=False, loc="upper right")
ax.set_ylim(0, 135)
save(f, "ablation.png")

# --- r3c: adaptive vs best fixed on the fixed engine (1300 x 520) ------------
items = [("online tuner, no cap\n(vs 16 × 1 MiB)", 1.220, 1.203, 1.232),
         ("online tuner, cap from 8 s\n(vs 16 × 1 MiB)", 1.212, 1.180, 1.248),
         ("workload selector, cap from start\n(vs 16 × 32 MiB)", 1.229, 1.223, 1.234),
         ("drop-rule controller, cap from 8 s\n(vs 16 × 32 MiB)", 1.071, 1.054, 1.085)]
f, ax = fig(1300, 520)
for i, (lab, r, lo, hi) in enumerate(items):
    ax.barh(i, r - 1, left=1, color=RED, height=0.55)
    ax.plot([lo, hi], [i, i], color="#14213d", lw=2)
    ax.text(hi + 0.006, i, f"{r:.3f}", va="center", fontsize=15)
ax.axvline(1, color=DARK, lw=1.5)
ax.set_yticks(range(len(items)), [it[0] for it in items])
ax.set_xlim(0.98, 1.3)
ax.set_xlabel("adaptive ÷ fixed time (right of 1: adaptive slower)")
save(f, "adaptive.png")

# --- r4b: joint agent (900 x 420) ---------------------------------------------
jc = load(f"{T}/rl_joint/analysis/C.json")["scenarios"]
best = {"CAP8": "fixed_16_32M", "HEALTHY": "fixed_16_1M", "CAP0": "fixed_16_32M", "D2": "fixed_16_5M"}
f, ax = fig(900, 420)
for i, s in enumerate(best):
    g = jc[s]["geo_mean_s"]
    ax.bar(i - 0.2, g[best[s]], 0.38, color=DARK)
    ax.bar(i + 0.2, g["dqnj"], 0.38, color=BLUE)
    ax.text(i + 0.2, g["dqnj"] + 2, f"×{g['dqnj'] / g[best[s]]:.2f}", ha="center", fontsize=14)
ax.set_xticks(range(4), ["cap from\n8 s", "no cap", "cap from\nstart", "slow,\nthen fast"])
ax.set_ylabel("time (s)")
ax.legend(handles=[Patch(color=DARK, label="best fixed"), Patch(color=BLUE, label="agent picking N + piece size")], frameon=False,
          loc="lower left", bbox_to_anchor=(0.0, 1.0), ncol=2, fontsize=13)
ax.set_ylim(0, 112)
save(f, "joint.png")

# --- r4c: v2 study (1100 x 520) ----------------------------------------------
c2 = load(f"{T}/rl_cap_train_v2/analysis/C.json")["scenarios"]
d2 = load(f"{T}/rl_cap_train_v2/analysis/D.json")["scenarios"]
sc = {**c2, **d2}
order = [("CAP8", "cap at 8 s\n(trained)"), ("HEALTHY", "no cap"), ("H_C12", "cap\n12 MiB/s"),
         ("H_RTT110", "delay\n110 ms"), ("H_DIP", "cap +\ndip"), ("H_C20", "cap\n20 MiB/s")]
f, ax = fig(1100, 520)
w = 0.27
for j, (arm, col) in enumerate([("v1", GREY), ("v2", BLUE), ("drop", ORANGE)]):
    for i, (s, _) in enumerate(order):
        b = sc[s][arm]["vs_best_fixed"]
        x = i + (j - 1) * w
        ax.bar(x, b["geo_ratio"] - 1, w * 0.9, bottom=1, color=col)
        ax.plot([x, x], b["ci95"], color="#222", lw=1.2)
ax.axhline(1, color=DARK, lw=1.2)
ax.axvspan(1.5, 5.5, color="#eef1f6", lw=0, zorder=0)
ax.text(3.5, 1.40, "held out (never trained on)", ha="center", fontsize=14, color=DARK)
ax.set_xticks(range(len(order)), [o[1] for o in order])
ax.set_ylabel("time ÷ best fixed 1 MiB")
ax.set_ylim(0.72, 1.44)
ax.legend(handles=[Patch(color=GREY, label="first retrained"), Patch(color=BLUE, label="broadly trained"),
                   Patch(color=ORANGE, label="drop rule")],
          frameon=False, loc="lower left", bbox_to_anchor=(0.0, 1.0), ncol=3, fontsize=13)
save(f, "v2.png")

# --- r4d: CAP8 times (1100 x 540) --------------------------------------------
j8 = jc["CAP8"]["geo_mean_s"]
v8 = c2["CAP8"]["geo_mean_s"]
bars = [("fixed 16 × 32 MiB", j8["fixed_16_32M"], DARK), ("aria2", j8["aria2"], DARK), ("drop rule", v8["drop"], ORANGE),
        ("broadly trained agent", v8["v2"], BLUE), ("first retrained agent", v8["v1"], GREY),
        ("agent picking N and piece size", j8["dqnj"], GREY),
        ("fixed 16 × 1 MiB", v8["fixed_16_1M"], DARK)]
f, ax = fig(1100, 540)
for i, (lab, t, col) in enumerate(reversed(bars)):
    ax.barh(i, t, color=col, height=0.65)
    ax.text(t + 0.8, i, f"{t:.1f} s", va="center", fontsize=15)
ax.set_yticks(range(len(bars)), [b[0] for b in reversed(bars)])
ax.set_xlabel("completion time, cap from 8 s (s)")
ax.set_xlim(0, 90)
save(f, "cap8_times.png")

# --- r5: live pilot (1100 x 480) ----------------------------------------------
lp = [c for c in load(f"{T}/live_pilot_l1/analysis/summary.json")["conditions"] if c["status"] == "OK"]
f, ax = fig(1100, 520)
xs = range(len(lp))
ax.axhline(5, color=RED, ls="--", lw=1.5)
ax.text(len(lp) - 0.3, 5.7, "5 % line", ha="left", fontsize=14, color=RED)
ax.set_xlim(-0.7, len(lp) + 1.6)
ax.scatter(xs, [100 * (c["aria2_over_best_fixed"] - 1) for c in lp], color=GREY, s=70, label="aria2", zorder=2)
ax.scatter(xs, [100 * (c["time_s"]["ours_N8_S1M"] / c["best_s"] - 1) for c in lp], color=BLUE, s=80,
           label="ours, 8 × 1 MiB everywhere", zorder=3)
NAMES = {"kcdn": "kernel.org CDN", "kedge": "kernel.org origin", "riken": "RIKEN, Japan", "tuna": "Tsinghua, China"}
groups = {}
for i, c in enumerate(lp):
    groups.setdefault(c["server"], []).append(i)
ax.set_xticks([sum(g) / len(g) for g in groups.values()], [NAMES[k].replace(" ", "\n", 1) for k in groups], fontsize=14)
ax.set_xlabel("one dot per time of day, grouped by server")
ax.set_ylabel("% slower than the\nbest fixed cell")
ax.legend(frameon=False, loc="upper left")
save(f, "live.png")

# --- r5b: mirror check (1100 x 480) -------------------------------------------
mm = load(f"{T}/live_mirror_check/analysis/M2.json")["servers"]
f, ax = fig(1100, 480)
for i, s in enumerate(["kcdn", "kedge", "riken"]):
    for k, (key, col, lab) in enumerate([("v2_vs_best_fixed", BLUE, "broadly trained agent"), ("drop_vs_best_fixed", ORANGE, "drop rule")]):
        b = mm[s][key]
        y = i + (k - 0.5) * 0.34
        ax.plot(b["ci95"], [y, y], color=col, lw=3)
        ax.plot(b["geo_ratio"], y, "o", color=col, ms=10, label=lab if i == 0 else None)
        ax.text(b["ci95"][1] + 0.04, y, f"{b['geo_ratio']:.2f}", va="center", fontsize=14)
ax.axvline(1, color=DARK, ls="--", lw=1.2)
ax.axvline(1.05, color=RED, ls=":", lw=1.5)
ax.set_yticks(range(3), ["kernel.org\nCDN", "kernel.org\norigin", "RIKEN,\nJapan"])
ax.set_xlabel("time ÷ best fixed 1 MiB (95 % interval)")
ax.set_xlim(0.6, 3.1)
ax.legend(frameon=False, loc="lower right")
save(f, "mirror.png")

# --- vsaria2 (1100 x 560) ------------------------------------------------------
rows = [("server slow, then fast", [0.876], 0.376),
        ("server speeds up at 1 s", [0.359, 0.432], 0.344),
        ("server cap switches on at 8 s", [1.096, 1.310, 1.367, 1.422, 1.781], 1.016),
        ("collapse, delay, loss, throttle", [1.456], None),
        ("six capped server variants", [1.660], None)]
f, ax = fig(1100, 560)
ax.axvspan(0.28, 1.0, color="#e8f0fb", lw=0)
ax.axvspan(1.0, 2.1, color="#fbeee4", lw=0)
for i, (lab, arms, fixed) in enumerate(reversed(rows)):
    ax.scatter(arms, [i] * len(arms), color=ORANGE, s=110, zorder=3)
    if fixed:
        ax.scatter([fixed], [i], color=BLUE, marker="D", s=110, zorder=3)
ax.axvline(1, color=DARK, ls="--", lw=1.5)
ax.set_xscale("log")
ax.set_xticks([0.3, 0.5, 0.7, 1, 1.5, 2], ["0.3", "0.5", "0.7", "1", "1.5", "2"])
ax.xaxis.set_minor_formatter(matplotlib.ticker.NullFormatter())
ax.set_xlim(0.28, 2.1)
ax.set_yticks(range(len(rows)), [r[0] for r in reversed(rows)])
ax.set_ylim(-0.6, len(rows) - 0.4)
ax.set_xlabel("time ÷ aria2’s time (left of 1: faster)")
ax.scatter([], [], color=ORANGE, s=110, label="our adaptive arms")
ax.scatter([], [], color=BLUE, marker="D", s=110, label="our best fixed")
ax.legend(frameon=False, loc="lower left")
save(f, "vsaria2.png")

# --- summary forest (1664 x 600) ----------------------------------------------
rows = [("drop-rule controller vs small-piece fixed, original engine", 0.766, 0.762, 0.769),
        ("drop-rule controller vs 16 × 32 MiB, fixed engine", 1.071, 1.054, 1.085),
        ("workload selector vs 16 × 32 MiB, cap from start", 1.229, 1.223, 1.234),
        ("online tuner vs 16 × 1 MiB, cap from 8 s", 1.212, 1.180, 1.248),
        ("first DQN agent, not retrained, vs 16 × 32 MiB", 1.356, 1.252, 1.472),
        ("first retrained agent vs 16 × 1 MiB", 0.907, 0.893, 0.921),
        ("first retrained agent vs 16 × 32 MiB", 1.298, 1.274, 1.321),
        ("agent picking N and piece size vs 16 × 32 MiB", 1.471, 1.405, 1.546),
        ("broadly trained agent vs 16 × 1 MiB", 0.845, 0.840, 0.850)]
f, ax = fig(1664, 600)
for i, (lab, r, lo, hi) in enumerate(reversed(rows)):
    col = GREEN if hi < 1 else RED
    ax.plot([lo, hi], [i, i], color=col, lw=3)
    ax.plot(r, i, "o", color=col, ms=10)
    ax.text(hi + 0.012, i, f"{r:.3f}", va="center", fontsize=14, zorder=5, bbox=dict(fc="white", ec="none", pad=1))
ax.axvline(1, color=DARK, ls="--", lw=1.5)
ax.set_yticks(range(len(rows)), [r[0] for r in reversed(rows)], fontsize=14)
ax.set_xlabel("adaptive ÷ fixed time (95 % interval)")
ax.set_xlim(0.72, 1.62)
save(f, "forest.png")

print("deck figures in", OUT)
