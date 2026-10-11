#!/usr/bin/env python3
"""Generate reports/joint_ns_phase_pacing_screen.md from the analysis JSONs."""
import json
import os
import time

BASE = "/workspaces/mvp/testdata/joint_ns_phase_pacing_screen"
J = json.load(open(os.path.join(BASE, "analysis", "screen.json")))
CAL = json.load(open(os.path.join(BASE, "analysis", "calibration.json")))
MIB = 1024 ** 2
FAMILY = ["T=1s", "T=2s", "T=4s", "T=8s"]
P = J["params"]


def best_one_switch(k):
    return max(J["family"][k]["causal"].items(), key=lambda kv: kv[1]["gain_vs_best_fixed"])


def f(x):
    return "%.2f" % x


L = []
a = L.append
a("# Joint N+S Phase-Locked Pacing Screen — Analytical Report")
a("")
a("**Status: `ANALYTICAL_SCREEN_FAILED — NO CAUSAL JOINT HEADROOM`.** "
  "ANALYTICAL SIMULATION ONLY: no downloads, no server/client processes, no "
  "tc/netem, no replay were run. All numbers are deterministic simulator output, "
  "not empirical evidence. The completed fixed aria2 benchmark is untouched.")
a("")
a("Generated: %s" % time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
a("")
a("## 1. Motivation")
a("")
a("The frozen aria2-competitive benchmark established a reproducible 62.4 % "
  "completion-time advantage (geometric ratio 0.3756, 30/30 pairs) for a fixed "
  "configuration (N=16, S=4 MiB) over fixed aria2 (split=16) under the D2 "
  "workload. The mechanism is request granularity x request-start timing x "
  "phase-locked per-response pacing: aria2's 32 MiB pieces all start before the "
  "2 s transition and are held at 640 KiB/s, while 4 MiB ranges re-allocate into "
  "the recovered 2.5 MiB/s phase. That result is **not** adaptive (the "
  "configuration never changes during a download). This screen asks the next, "
  "bounded question: can a causal controller that changes **both** N and S "
  "during a download exploit a *predeclared* family of pacing regimes and beat "
  "the best fixed configuration?")
a("")
a("## 2. Frozen design (predeclared before the results)")
a("")
a("- **Regime family (2-phase, anchored to the measured D2 rates):** slow "
  "640 KiB/s -> fast 2500 KiB/s, transition T in {1, 2, 4, 8} s; 512 MiB file. "
  "The controller never receives T, the schedule, or a regime identifier. The "
  "exact D2 3-phase schedule is a validation anchor only.")
a("- **Fixed frontier:** N in {1,2,4,8,16} x S in {256 KiB, 1 MiB, 4 MiB, "
  "16 MiB, 32 MiB}.")
a("- **Frozen action space:** (1,256K), (2,1M), (4,1M), (4,4M), (8,4M), "
  "(8,16M), (16,4M), (16,16M).")
a("- **Starting configuration:** (4, 4 MiB) — deliberately not the already "
  "optimal 16/4M.")
a("- **Primary causal family (one-switch):** baseline = median of the first 3 "
  "observed throughput samples; trigger = median of the last 3 samples >= "
  "threshold x baseline; thresholds {1.25, 1.5, 2.0}; targets {(16,16M), "
  "(16,4M), (4,16M)}; at most one switch per download.")
a("- **Diagnostic extensions (clearly labelled):** two-switch causal probe; "
  "predeclared blind ramps (time-based, no performance signal); an optimistic "
  "causal bound (clairvoyant from the first completion onward); a clairvoyant "
  "oracle. None is deployable except the causal families.")
a("- **Predeclared gate:** G1 fixed optimum varies (argmin differs; max "
  "wrong-config penalty >= 5 %); G2 positive causal headroom (a predeclared "
  "causal controller beats the per-regime best fixed by >= 5 % in >= 2 "
  "regimes); G3 the same controller beats aria2; G4 no stationary gain "
  "(>= 2 %). PASS iff all four; otherwise STOP with no empirical execution.")
a("")
a("## 3. Simulator and calibration")
a("")
a("Deterministic event-driven model (`tools/jns_sim.py`): one contiguous range "
  "request per worker; S applies to future allocations only; the server samples "
  "a request's rate at request start and holds it; server duration "
  "`L/rate + c*ceil(L/16KiB)`; a worker's next request reaches the server `h` "
  "seconds after the previous server end; N changes add workers immediately or "
  "retire them between requests; the frozen 30 s per-attempt timeout and "
  "same-range retry (max 3 attempts) are modelled.")
a("")
a("Fitted on the 40 measured D2 fixed-policy configurations "
  "(`discovery_ours/`): `h=%.3f s`, `c=%.4f s`, `init=%.3f s`, `down=%.3f s`, "
  "`retry_gap=%.3f s`. Relative RMSE **%.2f %%**, max absolute relative error "
  "**%.2f %%**. Held-out checks: aria2 32 MiB-piece median observed 53.32 s vs "
  "predicted 52.89 s (-0.8 percent); measured D2 (16,4M) 19.99 s vs predicted "
  "19.92 s; (16,1M) 25.60 s vs 25.65 s." % (
      P["h"], P["c"], P["init"], P["down"], P["retry_gap"],
      100 * CAL["fit"]["rmse_rel"], 100 * CAL["fit"]["max_abs_rel"]))
a("")
a("## 4. Fixed frontier: the optimum varies, but only in S")
a("")
a("| regime | best fixed | completion (s) | action-space best | completion (s) | aria2 (s) |")
a("|---|---|---|---|---|---|")
for k in FAMILY:
    b = J["family"][k]["best_fixed"]
    ab = J["family"][k]["action_space_best"]
    a("| %s | (N=%d, S=%.0f MiB) | %s | %s | %s | %s |" % (
        k, b["N"], b["S"] / MIB, f(b["completion"]), ab["action"], f(ab["completion"]),
        f(J["family"][k]["aria2"])))
a("")
a("**N\\* = 16 in every regime** (per-response pacing is independent, so more "
  "parallelism is always better; consistent with the live T1 result that "
  "aggregate throughput scales monotonically with concurrency). The optimal S "
  "varies: 1 MiB at T=1,2,8 s and 4 MiB at T=4 s. The maximum wrong-config "
  "penalty is **%.1f %%** (using the T=4 s optimum in the T=1,2,8 s regimes). "
  "The D2 anchor reproduces the measured optimum: (16,4M) predicted 19.92 s vs "
  "measured 19.99 s." % (100 * J["gate"]["G1_fixed_optimum_varies"]["max_wrong_config_penalty"]))
a("")
a("The variation is real but **single-dimensional**: only S changes, and only "
  "between two neighbouring values; N never needs to change. A genuine *joint* "
  "(N and S) adaptation opportunity does not exist under this mechanism.")
a("")
a("## 5. Causal controllers: no positive headroom")
a("")
a("| regime | one-switch best | gain | probe (diagnostic) | gain | blind ramp (oracle-tuned) | gain | optimistic causal bound | gain |")
a("|---|---|---|---|---|---|---|---|---|")
for k in FAMILY:
    name, c = best_one_switch(k)
    pr = J["family"][k]["probe_variants"]["joint_16_16M"]
    rk, rg, rc = J["family"][k]["blind_ramp_best"]
    ocb = J["family"][k]["optimistic_causal_bound"]
    b = J["family"][k]["best_fixed"]["completion"]
    a("| %s | %s | %+.1f %% | %s s | %+.1f %% | %s | %+.1f %% | %s s | %+.1f %% |" % (
        k, name, 100 * c["gain_vs_best_fixed"], f(pr["completion"]),
        100 * pr["gain_vs_best_fixed"], rk, 100 * rg, f(ocb["completion"]),
        100 * (b - ocb["completion"]) / b))
a("")
a("Every predeclared performance-driven causal controller loses in every regime "
  "except one marginal case (+1.1 % at T=8 s, far below the 5 % materiality "
  "threshold). The two dominant causes are structural:")
a("")
a("1. **Starting-configuration handicap.** The frozen start (4, 4 MiB) fetches "
  "only 16 MiB in the first ~6.6 s, while the best fixed config (16, 1 MiB) "
  "fetches ~66 MiB. That ~50 MiB deficit costs ~1.25 s (~7 %) at the fast rate "
  "and cannot be recovered: the **optimistic causal bound** (perfect knowledge "
  "from the first completion onward, clairvoyant exact-time switches) still "
  "loses by 14.7/7.8/6.3 % at T=1/2/4 s and gains only +3.4 % at T=8 s.")
a("2. **Observation latency.** With 4 MiB requests the first completed sample "
  "arrives at ~6.6 s, and a post-transition sample requires a request that "
  "*started* after T; detection is therefore late for T=1,2,4 s and only the "
  "diagnostic 256 KiB probe shortens it for T=8 s (detection 8.35 s). Even the "
  "probe, which is a stronger two-switch design, reaches only +1.1 %.")
a("")
a("The blind-ramp scan (an oracle over ramp time and target — an upper bound "
  "for any time-based controller) reaches at most +3.7 % (T=2 s). No causal "
  "class reaches the 5 % threshold in even one regime, let alone two.")
a("")
a("## 6. Oracle: the regime information has value, but it is not causally capturable")
a("")
a("| regime | same-start oracle gain | unconstrained oracle gain |")
a("|---|---|---|")
for k in FAMILY:
    a("| %s | %+.1f %% | %+.1f %% |" % (
        k, 100 * J["family"][k]["oracle_same_start"]["gain_vs_best_fixed"],
        100 * J["family"][k]["oracle_unconstrained"]["gain_vs_best_fixed"]))
a("")
a("A clairvoyant oracle that may also choose the starting configuration gains "
  "6.3–14.5 %, confirming that knowing the transition time is worth something. "
  "But constrained to the frozen start, the same-start oracle gains at most "
  "+2.3 %, and the optimistic causal bound at most +3.4 %: the value is in the "
  "*start*, not in a within-download adaptation. Causal/oracle capture is "
  "effectively zero.")
a("")
a("## 7. Stationary controls")
a("")
a("| control | best fixed | start (4,4M) | causal completion | switches | spurious gain |")
a("|---|---|---|---|---|---|")
for name, st in J["stationary"].items():
    cv = [c["completion"] for c in st["causal"].values()]
    ns = sum(1 for c in st["causal"].values() if c["switches"])
    spurious = (st["start_config_completion"] - cv[0]) / st["start_config_completion"]
    a("| %s | (N=%d, S=%.0f MiB) %s s | %s s | %s s | %d | %+.1f %% |" % (
        name, st["best_fixed"]["N"], st["best_fixed"]["S"] / MIB,
        f(st["best_fixed"]["completion"]), f(st["start_config_completion"]),
        f(cv[0]), ns, 100 * spurious))
a("")
a("In stationary conditions the causal controllers make **no switches** and "
  "finish exactly at their starting configuration's time: adaptation produces "
  "no intrinsic benefit (and no spurious exploration gain).")
a("")
a("## 8. Gate verdict and failure interpretation")
a("")
g = J["gate"]
a("- **G1 (fixed optimum varies):** PASS — argmins differ (S* = 1 MiB vs "
  "4 MiB), max wrong-config penalty %.1f %%." % (100 * g["G1_fixed_optimum_varies"]["max_wrong_config_penalty"]))
a("- **G2 (positive causal headroom):** FAIL — best predeclared causal gain "
  "%.1f %% (T=8 s); %d regimes with >= 5 %% causal gain (required: >= 2)."
  % (100 * max(g["G2_positive_causal_headroom"]["best_causal_gain_per_regime"].values()),
     g["G2_positive_causal_headroom"]["n_regimes_gain_ge_5pct"]))
a("- **G3 (beats aria2):** PASS — the causal controllers beat aria2 "
  "(~53 s) in every regime, but this is not the adaptation claim.")
a("- **G4 (no stationary gain):** PASS.")
a("- **Overall: FAIL → STOP.** No new preregistration was created and **no "
  "empirical experiment was run**.")
a("")
a("**Interpretation (per the frozen failure rule):** there is **no robust joint "
  "adaptation opportunity under this mechanism**. The phase-locked pacing "
  "mechanism creates a *fixed-configuration* advantage (already captured by the "
  "62.4 % benchmark), not a within-download adaptation advantage. The regime "
  "information is real (oracle headroom) but its causal value is dominated by "
  "the frozen starting handicap and the request-completion observation "
  "latency. This is a property of the mechanism and the frozen design, not of "
  "a particular learning algorithm; running LinUCB/RL on this environment "
  "would be searching for an advantage that the analytical screen shows is not "
  "there.")
a("")
a("## 9. What would be needed for a positive joint-adaptation study")
a("")
a("A future study must first establish a mechanism where a *causal* signal can "
  "change the joint (N,S) optimum **before** the loss of the starting phase is "
  "irrecoverable, e.g.: (a) an initial configuration that is not dominated "
  "before the first observation (this screen's frozen start alone costs ~7 %); "
  "(b) per-request streaming progress or a fast sub-request observation channel "
  "so the transition can be detected inside a request rather than after it; or "
  "(c) a regime family whose fixed optimum varies in N (not only S), which the "
  "measured per-response pacing mechanism does not produce. Any such study must "
  "freeze its regime family, action space, starting state, signal, thresholds, "
  "and gate before execution.")
a("")
a("## 10. Scope and immutability")
a("")
a("- This screen is analytical and calibrated to the measured D2 evidence; it "
  "does not modify or re-interpret the frozen aria2-competitive benchmark, the "
  "salvage-S studies, the adaptive-N screen, or any historical evidence.")
a("- No empirical network experiment was run because the predeclared gate "
  "failed (the task's explicit stop condition).")
a("- Claim scope: within this model and the frozen design, no causal joint N+S "
  "controller starting at (4,4M) can beat the best fixed configuration by a "
  "material margin across the regime family.")
a("")
a("## 11. Figures")
a("")
for fn, cap in [
    ("fig1_fixed_surface.png", "fixed-policy completion surface over (N,S) per regime (star = optimum)"),
    ("fig2_fixed_optimum.png", "per-regime optimum (S*) and the wrong-config penalty matrix"),
    ("fig3_adaptive_trajectory.png", "N and S trajectories of the one-switch and probe controllers"),
    ("fig4_throughput_pacing.png", "observed request throughput vs the hidden pacing schedule"),
    ("fig5_fixed_vs_adaptive_vs_aria2.png", "best fixed vs adaptive vs aria2 per regime"),
    ("fig6_headroom.png", "adaptive vs best-fixed headroom with the 5 % gate"),
    ("fig7_causal_vs_oracle.png", "causal capture vs oracle headroom"),
    ("fig8_stationary_control.png", "stationary controls: no spurious adaptation gain"),
]:
    a("- `figures/%s` — %s" % (fn, cap))
a("")
a("## 12. Artifacts")
a("")
a("- `model/model_definition.json` — frozen design; "
  "`simulation/simulation_assumptions.md` — model and calibration; "
  "`tools/jns_sim.py`, `tools/calibrate.py`, `tools/run_screen.py`, "
  "`tools/make_figures.py`; `analysis/calibration.json`, "
  "`analysis/screen.json`; `manifest/manifest.json`.")
a("")
open(os.path.join(BASE, "reports", "joint_ns_phase_pacing_screen.md"), "w").write("\n".join(L) + "\n")
print("report written")
