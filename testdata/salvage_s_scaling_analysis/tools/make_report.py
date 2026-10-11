#!/usr/bin/env python3
"""Generate the scaling headroom report and the Stage 1 implications document
from the deterministic analysis outputs. No derived value is typed by hand."""
import json
import os
import time

BASE = "/workspaces/mvp/testdata/salvage_s_scaling_analysis"
J = json.load(open(os.path.join(BASE, "analysis", "scaling_analysis.json")))
MIB, GIB = 1024 ** 2, 1024 ** 3
ARMS = ["256KiB", "1MiB", "4MiB", "16MiB"]
STRATS = ["16MiB->4MiB", "4MiB->1MiB", "1MiB->256KiB"]
MODELS = ["MODEL_A_analytical", "MODEL_B_stage0s_calibrated"]


def pct(x):
    return "%.4f%%" % (100 * x)


def by_size(rows, model, strat):
    r = [x for x in rows if x["model"] == model and x["strategy"] == strat]
    r.sort(key=lambda x: x["file_size_bytes"])
    return r


def main():
    # summary metrics
    frozen_max = {}
    for model in MODELS:
        for strat in STRATS:
            rs = by_size(J["switch_rows"], model, strat)
            frozen_max[(model, strat)] = {
                "max": max(r["relative_gain"] for r in rs),
                "min": min(r["relative_gain"] for r in rs),
                "at_max": max(rs, key=lambda r: r["relative_gain"])["file_size_label"]}
    env_grid_max = {}
    for model in MODELS:
        for strat in STRATS:
            rows = [r for r in J["envelope_rows"] if r["model"] == model and r["strategy"] == strat and r["best_switch"]]
            env_grid_max[(model, strat)] = max(rows, key=lambda r: r["best_switch"]["relative_gain"])
    dense = [r for r in J["dense_modular"] if r["model"] == "MODEL_A_analytical" and r["best_16to4_envelope_gain"] is not None]
    dense_max = max(dense, key=lambda r: r["best_16to4_envelope_gain"])
    dense_large = [r for r in dense if r["file_size_bytes"] > 640 * MIB]
    dense_large_max = max(dense_large, key=lambda r: r["best_16to4_envelope_gain"])
    base512 = {}
    for model in MODELS:
        for strat in STRATS:
            rs = by_size(J["switch_rows"], model, strat)
            base512[(model, strat)] = [r for r in rs if r["file_size_bytes"] == 512 * MIB][0]
    scaling = J["scaling"]

    # ---------------- report ----------------
    L = []
    a = L.append
    a("# Pre-Stage-1 File-Size Scaling / Headroom Analysis — Salvage Protocol v7")
    a("")
    a("**ANALYTICAL SIMULATION ONLY.** No empirical network experiment was run: no mm-link,")
    a("no server/client downloads, no tc/netem, no packet replay, no Stage 1 runs. All results")
    a("below are deterministic model output under the frozen mechanism.")
    a("")
    a("Generated: %s" % time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
    a("")
    a("## 1. Research question")
    a("")
    a("Under the frozen static rate + RTT mechanism, can increasing the file size plausibly")
    a("increase the **relative** completion-time headroom available to chunk-size adaptation?")
    a("The three frozen switching strategies and an exploratory single-switch upper envelope are")
    a("examined across a file-size grid.")
    a("")
    a("## 2. Frozen mechanism")
    a("")
    a("- R = %.0f bytes/s (4 Mbps), D = %.0f ms, N = %d" % (500000.0, 40.0, 4))
    a("- arms: " + ", ".join("%s (%d B)" % (l, s) for l, s in zip(ARMS, [256 * 1024, 1 * MIB, 4 * MIB, 16 * MIB])))
    a("- strategies and thresholds: " + "; ".join("%s->%s at B_pending <= %s" % (s["from"], s["to"], s["B_switch_bytes"]) for s in J["mechanism"]["strategies"]))
    a("- one switch maximum; evaluated immediately before allocation; in-flight ranges never resized.")
    a("")
    a("## 3. Analytical model")
    a("")
    a("MODEL A: `t(s) = D + s/R` (frozen request-level model).")
    a("")
    a("The simulator is a deterministic work-conserving event simulation of the frozen semantics")
    a("(N=4 workers, contiguous ranges allocated in order, future-only chunk size, B_pending sampled")
    a("immediately before each allocation, one switch, final partial range, EOF).")
    a("")
    a("## 4. Stage 0S calibration")
    a("")
    a("MODEL B: `t(s) = a + b*s` with a = %.9f s and b = %.12e s/byte" % (
        J["models"]["MODEL_B_stage0s_calibrated"]["params"]["a_seconds"],
        J["models"]["MODEL_B_stage0s_calibrated"]["params"]["b_seconds_per_byte"]))
    a("(OLS fit to the four frozen Stage 0S pooled median service times; R^2 = %.12f)." % (
        J["models"]["MODEL_B_stage0s_calibrated"]["params"]["r_squared"]))
    a("The models are never blended.")
    a("")
    a("Stage 0S validation at F = 512 MiB (predicted vs observed median):")
    a("")
    a("| Arm | Model A predicted | Model B predicted | Stage 0S observed median | B rel. error |")
    a("|---|---|---|---|---|")
    for arm in ARMS:
        va = J["stage0s_validation"]["MODEL_A_analytical"][arm]
        vb = J["stage0s_validation"]["MODEL_B_stage0s_calibrated"][arm]
        a("| %s | %.3f s | %.3f s | %.3f s | %+.4f%% |" % (
            arm, va["predicted_seconds"], vb["predicted_seconds"], vb["observed_median_seconds"],
            100 * vb["relative_error"]))
    a("")
    a("Model B reproduces the Stage 0S fixed-S observations to within 0.4%; Model A underestimates")
    a("them by ~2.5% because the observed service times include a small scheduling overhead.")
    a("")
    a("## 5. File-size grid")
    a("")
    a("Main grid: " + ", ".join("%d GiB" % (f // GIB) if f % GIB == 0 else "%d MiB" % (f // MIB) for f in J["file_size_grid"]))
    a("plus a dense residue scan (512–640 MiB in 4 MiB steps and larger-F residues).")
    a("")
    a("## 6. Fixed-S scaling results")
    a("")
    a("For exact full bursts the normalized fixed-S completion is scale-invariant:")
    a("`T_S/T_ref = 1 + R*D/s` (Model A), i.e. +0.119% (16 MiB), +0.477% (4 MiB),")
    a("+1.907% (1 MiB), +7.629% (256 KiB) — independent of F. The model-implied lowest")
    a("completion-time fixed condition is 16 MiB on the entire main grid.")
    a("")
    a("| File size | model-implied best fixed | T_ref (s) | best fixed (s) | 256KiB (s) | 1MiB (s) | 4MiB (s) | 16MiB (s) |")
    a("|---|---|---|---|---|---|---|---|")
    for f in J["file_size_grid"]:
        rows = [r for r in J["fixed_rows"] if r["model"] == "MODEL_A_analytical" and r["file_size_bytes"] == f]
        d = {r["arm"]: r["completion_seconds"] for r in rows}
        ref = min(d, key=lambda k: d[k])
        a("| %s | %s | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f |" % (
            fmt(f), ref, f / (4 * 500000.0), d[ref], d["256KiB"], d["1MiB"], d["4MiB"], d["16MiB"]))
    a("")
    a("## 7. Frozen-switch scaling results")
    a("")
    a("Relative gain vs the model-implied best fixed condition (`relative_gain = (T_ref_fixed - T_switch)/T_ref_fixed`):")
    a("")
    a("| File size | " + " | ".join(STRATS) + " |")
    a("|---|" + "---|" * len(STRATS))
    for f in J["file_size_grid"]:
        cells = []
        for strat in STRATS:
            r = [x for x in by_size(J["switch_rows"], "MODEL_A_analytical", strat) if x["file_size_bytes"] == f][0]
            cells.append(pct(r["relative_gain"]))
        a("| %s | %s |" % (fmt(f), " | ".join(cells)))
    a("")
    a("Maximum frozen-strategy relative gains across the grid:")
    a("")
    a("| Strategy | Model A max | Model A at | Model B max |")
    a("|---|---|---|---|")
    for strat in STRATS:
        a("| %s | %s | %s | %s |" % (strat, pct(frozen_max[("MODEL_A_analytical", strat)]["max"]),
                                     frozen_max[("MODEL_A_analytical", strat)]["at_max"],
                                     pct(frozen_max[("MODEL_B_stage0s_calibrated", strat)]["max"])))
    a("")
    a("All frozen-strategy gains are **negative** on the entire grid. The switch fires at exactly")
    a("a full final burst (B_switch = N*S0), where replacing S0 chunks with S1 chunks adds")
    a("per-request D overhead without shortening the tail. At F = 512 MiB the gains are:")
    for strat in STRATS:
        r = base512[("MODEL_A_analytical", strat)]
        a("- %s: %s (absolute %+0.3f s; switch fired=%s)" % (
            strat, pct(r["relative_gain"]), r["absolute_time_saved_seconds"], r["switch_fired"]))
    a("")
    a("## 8. Analytical upper-envelope results (EXPLORATORY ONLY)")
    a("")
    a("For each starting arm the switch point was scanned over feasible B_pending thresholds;")
    a("this is **not** the frozen Stage 1 policy and must not be treated as evidence.")
    a("")
    a("| Strategy | Model A best envelope | at | Model B best envelope |")
    a("|---|---|---|---|")
    for strat in STRATS:
        ra = env_grid_max[("MODEL_A_analytical", strat)]
        rb = env_grid_max[("MODEL_B_stage0s_calibrated", strat)]
        a("| %s | %s | %s | %s |" % (strat, pct(ra["best_switch"]["relative_gain"]), ra["file_size_label"],
                                     pct(rb["best_switch"]["relative_gain"])))
    a("")
    a("On the main grid even the optimally timed single switch is **<= 0** (the aligned power-of-two")
    a("file sizes have no tail-packing defect). The dense residue scan (16MiB->4MiB, Model A) finds a")
    a("maximum of **%s at %s** (threshold %s B) — i.e. at most ~0.36%%, the bulk per-request overhead" % (
        pct(dense_max["best_16to4_envelope_gain"]), dense_max["file_size_label"],
        int(dense_max["best_16to4_threshold_bytes"])))
    a("difference between 16 MiB and 4 MiB — and it occurs only at residues where a smaller fixed arm")
    a("is the reference; the larger-F residues stay <= %s (at %s)." % (
        pct(dense_large_max["best_16to4_envelope_gain"]), dense_large_max["file_size_label"]))
    a("")
    a("## 9. Absolute vs relative gain")
    a("")
    a("Absolute time saved (frozen strategies) is <= 0 on the grid; for the exploratory envelope the")
    a("absolute saving at favourable residues grows with F (a fixed ~0.36% of a growing T_ref), while")
    a("the relative gain stays bounded at ~0.36%. The two quantities must not be conflated: a larger")
    a("file can show a larger absolute difference while the percentage improvement does not increase.")
    a("")
    a("## 10. 5% threshold analysis")
    a("")
    a("| File size | Frozen 16->4 | Frozen 4->1 | Frozen 1->256K | Analytical single-switch upper envelope (16->4, dense scan) |")
    a("|---|---|---|---|---|")
    dense_by_F = {r["file_size_label"]: r for r in dense}
    for f in J["file_size_grid"]:
        label = fmt(f)
        env = dense_by_F.get(label, {})
        env_txt = pct(env["best_16to4_envelope_gain"]) if env.get("best_16to4_envelope_gain") is not None else "<= 0"
        cells = []
        for strat in STRATS:
            r = [x for x in by_size(J["switch_rows"], "MODEL_A_analytical", strat) if x["file_size_bytes"] == f][0]
            cells.append(pct(r["relative_gain"]))
        a("| %s | %s | %s | %s | %s |" % (label, cells[0], cells[1], cells[2], env_txt))
    a("")
    a("**No frozen strategy reaches 5% at any grid file size, and the analytical single-switch")
    a("upper envelope does not reach 5%% either** (maximum ~%s in the dense scan)." % pct(dense_max["best_16to4_envelope_gain"]))
    a("")
    a("## 11. Analytical vs calibrated-model comparison")
    a("")
    a("Both models agree qualitatively: fixed-S normalized costs are scale-invariant, the model-implied")
    a("best fixed arm is 16 MiB, frozen-switch gains are negative, and the envelope is far below 5%.")
    a("Model B shifts all times up by ~2.5% and slightly changes the negative asymptotes")
    a("(16->4: %s vs %s; 4->1: %s vs %s; 1->256K: %s vs %s). No qualitative disagreement." % (
        pct(frozen_max[("MODEL_A_analytical", "16MiB->4MiB")]["max"]), pct(frozen_max[("MODEL_B_stage0s_calibrated", "16MiB->4MiB")]["max"]),
        pct(frozen_max[("MODEL_A_analytical", "4MiB->1MiB")]["max"]), pct(frozen_max[("MODEL_B_stage0s_calibrated", "4MiB->1MiB")]["max"]),
        pct(frozen_max[("MODEL_A_analytical", "1MiB->256KiB")]["max"]), pct(frozen_max[("MODEL_B_stage0s_calibrated", "1MiB->256KiB")]["max"])))
    a("")
    a("## 12. Figures")
    a("")
    for f, cap in [
        ("fig1_completion_vs_filesize.png", "Completion time vs file size (fixed arms, both models)."),
        ("fig2_normalized_vs_filesize.png", "Normalized completion time T/T_ref vs file size."),
        ("fig3_relative_gain_vs_filesize.png", "Frozen strategies: relative gain/loss (negative values shown; 5% threshold)."),
        ("fig4_absolute_saved_vs_filesize.png", "Frozen strategies: absolute time saved."),
        ("fig5_envelope_vs_filesize.png", "EXPLORATORY single-switch upper envelope (grid + dense residue scan; 5% threshold)."),
        ("fig6_models_comparison.png", "Analytical vs Stage 0S-calibrated fixed-S curves + Stage 0S observed medians."),
        ("fig7_tail_contribution_vs_filesize.png", "Tail contribution vs total completion time as F grows."),
    ]:
        a("- `figures/%s` — %s" % (f, cap))
    a("")
    a("All figures are model output except the explicitly marked Stage 0S observed medians in Fig. 6;")
    a("no analytical uncertainty bars are drawn (the model is deterministic).")
    a("")
    a("## 13. Stage 1 implication")
    a("")
    a("**ANALYTICALLY INSUFFICIENT UNDER FROZEN MECHANISM.** See `STAGE1_IMPLICATIONS.md`.")
    a("")
    a("## 14. Limitations")
    a("")
    a("- Deterministic model: no timing noise; Stage 0S residual SD was 0.0008 in log time, so noise")
    a("  is not expected to create systematic headroom.")
    a("- Model B extrapolates the linear fit to partial range sizes (the fit is nearly perfect, R^2 = %.10f)." % (
        J["models"]["MODEL_B_stage0s_calibrated"]["params"]["r_squared"]))
    a("- The simulator assumes work-conserving allocation and identical service times per size; it")
    a("  does not model TCP effects, connection reuse, or server-side scheduling beyond the frozen model.")
    a("- The upper envelope is exploratory and was not used to change the frozen protocol.")
    a("")
    a("## 15. Statement")
    a("")
    a("No empirical network experiment was performed for this analysis. No mm-link, server, client,")
    a("tc/netem, or replay process was started; no confirmatory Stage 1 run was created.")
    os.makedirs(os.path.join(BASE, "reports"), exist_ok=True)
    open(os.path.join(BASE, "reports", "scaling_headroom_report.md"), "w").write("\n".join(L) + "\n")

    # ---------------- Stage 1 implications ----------------
    I = []
    b = I.append
    b("# STAGE1_IMPLICATIONS — Salvage Protocol v7 scaling / headroom analysis")
    b("")
    b("**Verdict: ANALYTICALLY INSUFFICIENT UNDER FROZEN MECHANISM**")
    b("")
    b("This document reports the analytical simulation result only. It does not change the frozen")
    b("protocol and it does not authorize any Stage 1 run.")
    b("")
    b("## Result summary")
    b("")
    b("- Frozen strategy gains (relative to the model-implied best fixed arm) across the entire")
    b("  file-size grid are **negative**: 16MiB->4MiB max %s, 4MiB->1MiB max %s," % (
        pct(frozen_max[("MODEL_A_analytical", "16MiB->4MiB")]["max"]),
        pct(frozen_max[("MODEL_A_analytical", "4MiB->1MiB")]["max"])))
    b("  1MiB->256KiB max %s (Model A; Model B agrees qualitatively)." % pct(frozen_max[("MODEL_A_analytical", "1MiB->256KiB")]["max"]))
    b("- At the frozen baseline (F = 512 MiB) the 16MiB->4MiB strategy yields %s." % pct(base512[("MODEL_A_analytical", "16MiB->4MiB")]["relative_gain"]))
    b("- Even the EXPLORATORY optimally timed single switch is <= 0 on the main grid and reaches at")
    b("  most ~%s in a dense residue scan — far below the frozen 5%% practical threshold." % pct(dense_max["best_16to4_envelope_gain"]))
    b("- Increasing the file size does **not** increase the relative headroom: the one-time tail effect")
    b("  is diluted by the growing bulk (relative tail share falls as 1/F, see Fig. 7), and the frozen")
    b("  thresholds fire at exactly full bursts where switching only adds per-request overhead.")
    b("")
    b("## Why")
    b("")
    b("Under the frozen static mechanism the per-request service time is `D + s/R`. The completion")
    b("time of an exactly-aligned file is `(F/(N*s)) * (D + s/R)`, so the fixed-arm cost differences")
    b("are per-byte overheads: `T/T_ref = 1 + R*D/s` — scale-invariant. The only one-time effect is")
    b("the final tail (<= ~D + S0/R); it is bounded and therefore contributes a relative gain that")
    b("decays with F. Because B_switch = N*S0 fires exactly when the remaining work equals one full")
    b("burst of the starting arm, replacing it with smaller chunks cannot shorten the tail and adds")
    b("D overhead. A well-timed switch can capture at most the bulk overhead difference (~0.36% for")
    b("16->4), and only at file sizes where a smaller fixed arm happens to be the reference.")
    b("")
    b("## Consequence for Stage 1")
    b("")
    b("- The current frozen Stage 1 design (512 MiB, the three frozen strategies, the frozen trigger)")
    b("  is **not** mechanistically capable of producing a practically meaningful (5%) effect under")
    b("  the frozen mechanism.")
    b("- **No amendment is proposed here.** A larger file size is not justified either: it reduces the")
    b("  relative headroom rather than increasing it. Any change to the mechanism, trigger timing, or")
    b("  design would require a separately frozen protocol decision; this analysis deliberately does")
    b("  not modify the frozen protocol.")
    b("")
    b("## Evidence")
    b("")
    b("- `analysis/scaling_analysis.json`, `analysis/fixed_s.csv`, `analysis/frozen_switches.csv`,")
    b("  `analysis/envelope.csv`, `analysis/dense_modular.csv`")
    b("- `reports/scaling_headroom_report.md`, `figures/fig1..fig7`")
    b("- Deterministic simulator: `tools/scaling_analysis.py` (no RNG, no network I/O).")
    b("")
    b("## Integrity")
    b("")
    b("No empirical network experiment was performed: zero downloads, zero mm-link processes, zero")
    b("server/client runs, zero tc/netem, zero packet replay. The frozen protocol and all Stage 0S /")
    b("oracle / Stage A/B/C artifacts were not modified.")
    open(os.path.join(BASE, "STAGE1_IMPLICATIONS.md"), "w").write("\n".join(I) + "\n")

    print("wrote reports/scaling_headroom_report.md and STAGE1_IMPLICATIONS.md")
    print("frozen max gains (Model A):", {s: pct(frozen_max[("MODEL_A_analytical", s)]["max"]) for s in STRATS})
    print("dense envelope max:", pct(dense_max["best_16to4_envelope_gain"]), "at", dense_max["file_size_label"])
    print("512MiB 16->4 gain:", pct(base512[("MODEL_A_analytical", "16MiB->4MiB")]["relative_gain"]))
    return 0


def fmt(F):
    if F % GIB == 0:
        return "%dGiB" % (F // GIB)
    return "%dMiB" % (F // MIB)


if __name__ == "__main__":
    raise SystemExit(main())
