#!/usr/bin/env python3
"""Generate the three manuscript figures from RAW run files (no summary JSON).

Usage: python3 reports/ieee_access/make_figures.py
Writes reports/ieee_access/figures/fig{1,2,3}_*.png. Every plotted value is
recomputed from testdata/**/<run>.json; see tools/claims_check.py for the same
quantities with source paths.
"""
import glob, json, math, os, random, statistics as st
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
T = os.path.join(ROOT, "testdata")
OUT = os.path.join(os.path.dirname(__file__), "figures")
geo = lambda xs: math.exp(sum(map(math.log, xs)) / len(xs))

# ---- Fig 1: ours/aria2 ratio across D2, sweep schedules, NGINX -------------
def pairs(dirpath, pat_o, pat_a):
    o, a = {}, {}
    for f in glob.glob(os.path.join(dirpath, pat_o)):
        d = json.load(open(f)); o[os.path.basename(f)] = d
    for f in glob.glob(os.path.join(dirpath, pat_a)):
        d = json.load(open(f)); a[os.path.basename(f)] = d
    return o, a

rows = []  # (label, [per-pair ratios], group)
fr = os.path.join(T, "aria2_competitive_benchmark", "final_reproduction")
r = []
for i in range(60):
    po, pa = os.path.join(fr, f"final-p{i:02d}-ours.json"), os.path.join(fr, f"final-p{i:02d}-aria2.json")
    if os.path.exists(po) and os.path.exists(pa):
        r.append(json.load(open(po))["completion_ms"] / json.load(open(pa))["completion_ms"])
rows.append(("D2 frozen benchmark (n=30)", r, "D2"))
m = json.load(open(os.path.join(T, "d2_pacing_sweep", "manifest", "sweep_manifest.json")))
for s, spec in m["schedules_kib_per_s"].items():
    r = []
    for p in range(m["pairs_per_schedule"]):
        a = json.load(open(os.path.join(T, "d2_pacing_sweep", "runs", f"sw-{s[:2]}-p{p:02d}-aria2.json")))
        o = json.load(open(os.path.join(T, "d2_pacing_sweep", "runs", f"sw-{s[:2]}-p{p:02d}-ours.json")))
        r.append(o["completion_ms"] / a["completion_ms"])
    rows.append((f"sweep {s.split('_',1)[0]} {spec}", r, "sweep"))
m = json.load(open(os.path.join(T, "nginx_limit_rate", "manifest", "nginx_manifest.json")))
for c in m["configs"]:
    r = []
    for p in range(m["pairs_per_config"]):
        a = json.load(open(os.path.join(T, "nginx_limit_rate", "runs", f"ng-{c[:2]}-p{p:02d}-aria2.json")))
        o = json.load(open(os.path.join(T, "nginx_limit_rate", "runs", f"ng-{c[:2]}-p{p:02d}-ours.json")))
        r.append(o["completion_ms"] / a["completion_ms"])
    rows.append((f"NGINX {c}", r, "nginx"))
col = {"D2": "#1f77b4", "sweep": "#2ca02c", "nginx": "#d62728"}
fig, ax = plt.subplots(figsize=(7.2, 4.4))
for i, (lab, r, g) in enumerate(rows):
    y = len(rows) - i
    ax.scatter(r, [y] * len(r), s=14, color=col[g], alpha=0.7)
    ax.plot([geo(r)] * 2, [y - 0.3, y + 0.3], color="k", lw=1.5)
ax.axvline(1.0, ls="--", color="gray")
ax.set_yticks([len(rows) - i for i in range(len(rows))])
ax.set_yticklabels([r[0] for r in rows], fontsize=7)
ax.set_xlabel("paired completion-time ratio, ours / aria2  (< 1: ours faster)")
ax.set_title("Fixed 16 x 4 MiB vs aria2 1.37: D2, pacing sweep (n=5/schedule), NGINX (n=5/config)", fontsize=8)
ax.set_xlim(0.3, 1.15)
fig.tight_layout(); fig.savefig(os.path.join(OUT, "fig1_ours_vs_aria2.png"), dpi=200); plt.close(fig)

# ---- Fig 2: six-workload selector ratios + workload-level interval ------------
W = {}
for f in glob.glob(os.path.join(T, "n_dependent_server_joint_ns", "w1_w6_adaptive", "*.json")):
    d = json.load(open(f)); w, rest = d["run_id"].split("_", 1); s, rr = rest.rsplit("_r", 1)
    W.setdefault((w, s), {})[int(rr)] = d["completion_ms"]
ws = sorted({k[0] for k in W})
fig, axs = plt.subplots(1, 2, figsize=(7.2, 3.6), sharey=True)
for ax, base, title in ((axs[0], "per_workload_best_fixed", "vs per-workload best fixed"),
                        (axs[1], "family_best_fixed", "vs family-best single fixed (4,4 MiB)")):
    per = []
    for i, w in enumerate(ws):
        rr = [W[(w, "band_selector")][k] / W[(w, base)][k] for k in range(10)]
        ax.scatter([i] * 10, rr, s=10, alpha=0.6, color="#1f77b4")
        per.append(geo(rr)); ax.plot([i - .25, i + .25], [per[-1]] * 2, color="k", lw=1.5)
    l = [math.log(x) for x in per]; mu, se = st.mean(l), st.stdev(l) / math.sqrt(6)
    lo, hi = math.exp(mu - 2.5706 * se), math.exp(mu + 2.5706 * se)
    ax.axhspan(lo, hi, color="orange", alpha=0.25, label=f"workload-level 95% t-interval [{lo:.2f}, {hi:.2f}]")
    ax.axhline(1, ls="--", color="gray")
    ax.set_xticks(range(6)); ax.set_xticklabels(ws); ax.set_title(title, fontsize=9)
    ax.legend(fontsize=7, loc="upper right"); ax.set_yscale("log")
axs[0].set_ylabel("paired ratio selector / baseline (< 1: selector faster)")
fig.suptitle("Six-workload selector: dots = blocks (n=10), bar = workload geo-mean", fontsize=9)
fig.tight_layout(); fig.savefig(os.path.join(OUT, "fig2_six_workload_selector.png"), dpi=200, bbox_inches="tight"); plt.close(fig)

# ---- Fig 3: within-download controller vs fixed vs aria2 (P2) ---------------
R = {}
for f in glob.glob(os.path.join(T, "n_dependent_server_joint_ns", "adaptive", "p2_*.json")):
    d = json.load(open(f)); n, rr = os.path.basename(f)[:-5].rsplit("_r", 1)
    R.setdefault(n, []).append(d["completion_ms"] / 1000)
order = ["p2_causal_joint", "p2_causal_n_only", "p2_causal_s_only", "p2_fixed_16_1M", "p2_fixed_4_1M", "p2_fixed_4_4M", "p2_aria2"]
fig, ax = plt.subplots(figsize=(7.2, 3.6))
for i, n in enumerate(order):
    ax.scatter([i] * len(R[n]), R[n], s=14, color="#d62728" if n.endswith("aria2") else "#1f77b4", alpha=0.7)
    ax.plot([i - .3, i + .3], [st.mean(R[n])] * 2, color="k", lw=1.5)
ax.set_xticks(range(len(order))); ax.set_xticklabels([o[3:] for o in order], rotation=20, fontsize=8)
ax.set_ylabel("completion time (s)")
ax.set_title("P2 (cap from 8 s): controller beats the best of three fixed configs but not aria2 (n=10 blocks)", fontsize=8)
fig.tight_layout(); fig.savefig(os.path.join(OUT, "fig3_within_download_controller.png"), dpi=200); plt.close(fig)
print("wrote", sorted(os.listdir(OUT)))
