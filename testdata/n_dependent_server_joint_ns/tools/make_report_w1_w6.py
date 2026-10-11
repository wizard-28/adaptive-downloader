#!/usr/bin/env python3
"""W1-W6 report generator (reads the frozen manifest + live grid + adaptive)."""
import json
import os

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
MAN = os.path.join(BASE, "manifest", "w1_w6_manifest.json")
GRID = os.path.join(BASE, "analysis", "w1_w6_grid.json")
ADAPT = os.path.join(BASE, "analysis", "w1_w6_adaptive.json")
OUT_ROOTS = [os.path.join(BASE, "reports"), "/workspaces/mvp/reports"]

M = json.load(open(MAN))
G = json.load(open(GRID))
A = json.load(open(ADAPT))
WL = sorted(M["workloads"], key=lambda x: int(x[1:]))
MIB = 1024 ** 2
lines = []


def a(x=""):
    lines.append(x)


a("# W1-W6 six-workload family: live fixed-grid validation and adaptive comparisons")
a("")
a("Status: **six predeclared controlled workloads** (frozen manifest")
a("`manifest/w1_w6_manifest.json`, designed analytically before any live W1-W6")
a("run). Claim separation: **(A)** fixed-config competitive result vs aria2 under")
a("D2 (the 62.4 % result, separate report); **(B)** workload-level configuration")
a("selection (this report); **(C)** within-download joint adaptation (Track 2-B")
a("P2, separate report). This report is a **(B)** result; it is not")
a("within-download adaptation and makes no Internet-wide generalization claim.")
a("")
a("## 1. Frozen workload family")
a("")
a("| workload | T (s) | cap C (MiB/s) | gamma | cap at t (s) | model optimum B | model B completion | base-side penalty |")
a("|---|---|---|---|---|---|---|---|")
for wid in WL:
    w = M["workloads"][wid]
    b = w["optimum_B"]
    pen = w.get("base_pen")
    a("| %s (%s) | %s | %s | %s | %s | (%d, %d MiB) | %.1f s | %s |" % (
        wid, w["label"].replace("|", "/"), w["schedule_T_s"],
        (w["cap_c"] // MIB) if w["cap_c"] else "-",
        w["cap_gamma"] if w["cap_c"] else "-",
        w["cap_at_s"] if w["cap_at_s"] else "-",
        b["N"], b["S"] // MIB, b["t"],
        ("%.0f %%" % (100 * pen)) if pen else "-"))
a("")
a(("Selection rule (predeclared, model only): maximize sum(min(cap_gain, base_pen)) "
   "over 5-subsets of qualified candidates with transition/cap coverage and "
   "lexicographic tie-breaks. %s; %d/72 candidates qualified. No t=12 candidate "
   "qualified, so coverage relaxed as documented.")
  % (M["selection"]["constraint_note"], M["selection"]["n_qualified"]))
a("")
a("## 2. Live fixed-grid validation")
a("")
a("Per workload: cells {4,8,16} x {1,4,16 MiB} + predicted optimum, 2 reps,")
a("netem 40 ms + 0.4 %, timeout 30 s, retries 3. Invalid runs preserved.")
a("")
a("| workload | live argmin | live argmin | predicted | match | invalid runs |")
a("|---|---|---|---|---|---|")
for wid in WL:
    g = G["workloads"][wid]
    inval = sum(c["invalid"] for c in g["cells"].values())
    a("| %s | %s | %.1f s | %s | %s | %d |" % (
        wid, g["live_argmin"], g["live_argmin_s"] or -1, g["predicted_optimum"],
        "yes" if g["prediction_match"] else "**no**", inval))
a("")
a("Family-best single fixed configuration (live argmin of the mean over the six")
a("workloads, common cells only): **%s** (mean %.1f s)." % (
    G["family_best_single_fixed"], G["family_best_single_fixed_mean_s"] or -1))
a("")
a("## 3. Adaptive comparisons (10 fresh paired blocks per workload)")
a("")
a("Modes: frozen band selector (probe (16,1 MiB); 25 s post-drop window; nearest")
a("of the frozen levels) | per-workload best fixed (live argmin) | family best")
a("single fixed (live) | aria2. Invalid runs preserved; selector aborts count as")
a("losses.")
a("")
a("| workload | selector | per-workload fixed | family fixed | aria2 | selector vs per-workload | selector vs family | selector vs aria2 |")
a("|---|---|---|---|---|---|---|---|")
for wid in WL:
    r = A["workloads"][wid]
    def med(m):
        v = r[m]["median_s"]
        return "%.1f s" % v if v else "n/a"
    def pair(m):
        d = r["paired"][m]
        if not d["n"]:
            return "n/a"
        ci = d.get("ci95") or [d["geo_mean"], d["geo_mean"]]
        return "geo %.3f [%.3f, %.3f], %d/%d" % (
            d["geo_mean"], ci[0], ci[1], d["wins"], d["n"])
    a("| %s | %s | %s | %s | %s | %s | %s | %s |" % (
        wid, med("band_selector"), med("per_workload_best_fixed"),
        med("family_best_fixed"), med("aria2"),
        pair("per_workload_best_fixed"), pair("family_best_fixed"), pair("aria2")))
a("")
p = A["pooled"]
a("Pooled over all workload-blocks (geometric mean of paired ratios, 95 % bootstrap):")
a("")
a("| comparison | n | geo mean | 95 % CI | wins |")
a("|---|---|---|---|---|")
for key, label in (("sel_vs_per_workload", "selector vs per-workload best fixed"),
                   ("sel_vs_family", "selector vs family best single fixed"),
                   ("sel_vs_aria2", "selector vs aria2")):
    s = p[key]
    if s.get("n"):
        a("| %s | %d | %.3f | [%.3f, %.3f] | %d/%d |" % (
            label, s["n"], s["geo_mean"], s["ci95"][0], s["ci95"][1],
            s["wins"], s["n"]))
    else:
        a("| %s | 0 | - | - | - |" % label)
a("")
a("## 4. Selector behavior")
a("")
a("Frozen levels (B/s): %s -> targets %s." % (
    ", ".join(str(v) for v in M["selector"]["levels_bytes_s"]),
    ", ".join(M["selector"]["targets"])))
a("Three workloads (W2-W4) share the same degraded signature and optimum, so the")
a("selector's effective decision is between two configurations while still")
a("covering all six workloads; this is a property of the frozen family, not a")
a("post-hoc choice.")
a("")
a("**W5 frozen-target mismatch (honest):** the live validation found W5's optimum")
a("at (16, 16 MiB) 85.4 s, not the model's (4, 4 MiB) 95.7 s (retry/k feedback")
a("under the cap raises rates for retried ranges). The selector's target stayed")
a("frozen at the model value, so W5 is the selector's largest loss vs the")
a("per-workload clairvoyant baseline (geo 1.172). Retuning the frozen mapping")
a("after observing live outcomes is prohibited by the protocol; the mismatch is")
a("reported instead.")
a("")
a("## 5. Caveats")
a("")
a("- Pooled CIs treat 60 workload-blocks as independent. Robustness (analysis/w1_w6_robustness.json, tools/robustness_w1_w6.py): selector vs per-workload pooled 0.999, stratified-bootstrap [0.996, 1.002], six-cluster t-CI [0.873, 1.143]. Selector vs family single fixed pooled 0.770, stratified [0.768, 0.772], six-cluster t-CI [0.455, 1.304]; the pooled figure is driven by W1 and is not robust across the six workloads.")
a("- The selector is a within-download reconfiguration (one probe-to-target switch). Its W4 win (131 s vs 162 s best fixed) is partly an early-window effect, so B-vs-C separation is a semantic distinction, not a clean one.")
a("- The design was amended twice after seeing design output (classification window; coverage constraint); see PROJECT_CONTEXT section 22.100 amendment log.")
a("")
a("- Controlled single-server loopback scenario; no real-network claim.")
a("- The selector is one-shot per download and does not re-probe.")
a("- W1 (healthy) completes before any decision point in most runs; that is the")
a("  correct behavior for a healthy workload.")
a("- One model-vs-live mismatch (W5, see contrast 4) is disclosed; the adaptive")
a("  baselines use live argmins, so the comparison stays fair (and is")
a("  conservative for the selector).")
a("- Timeout sensitivity and the capped S-margin precision (Track 2-B) are")
a("  reported separately in `reports/track2b_soft_cap_report.md`.")
a("")
a("## 6. Artifacts")
a("")
a("- `manifest/w1_w6_manifest.json` (frozen design), `analysis/w1_w6_design.json`,")
a("  `analysis/w1_w6_grid.json`, `analysis/w1_w6_adaptive.json`.")
a("- `w1_w6_grid/` (%d runs), `w1_w6_adaptive/` (%d runs), `smoke/` (band smoke)." % (
    len([f for f in os.listdir(os.path.join(BASE, "w1_w6_grid")) if f.endswith(".json")])
    if os.path.isdir(os.path.join(BASE, "w1_w6_grid")) else 0,
    len([f for f in os.listdir(os.path.join(BASE, "w1_w6_adaptive")) if f.endswith(".json")])
    if os.path.isdir(os.path.join(BASE, "w1_w6_adaptive")) else 0))
a("- `figures/figW1_modes.png`, `figW2_ratios.png`, `figW3_grids.png`,")
a("  `figW4_selector.png`; tools `design_w1_w6.py`, `run_w1_w6_grid.py`,")
a("  `run_w1_w6_adaptive.py`, `make_figures_w1_w6.py`, `make_report_w1_w6.py`.")

text = "\n".join(lines) + "\n"
for root in OUT_ROOTS:
    os.makedirs(root, exist_ok=True)
    with open(os.path.join(root, "w1_w6_report.md"), "w") as f:
        f.write(text)
print("w1_w6_report.md written to", OUT_ROOTS)
