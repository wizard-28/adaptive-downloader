#!/usr/bin/env python3
"""Generate the Track 1 report (root reports/ and study reports/)."""
import json
import os
import time

BASE = "/workspaces/mvp/testdata/joint_ns_d2"
ROOT_REPORTS = "/workspaces/mvp/reports"
J = json.load(open(os.path.join(BASE, "analysis", "track1_analysis.json")))
M = json.load(open(os.path.join(BASE, "analysis", "mechanism_track1.json")))
MIB = 1024 ** 2
MODES = ["fixed", "n-only", "s-only", "joint"]
LABEL = {"fixed": "fixed (N=16, S=4 MiB)", "n-only": "N-only adaptive",
         "s-only": "S-only adaptive", "joint": "joint adaptive N+S"}


def ms(x):
    return "%.2f" % (x / 1000)


L = []
a = L.append
a("# Track 1 — Fresh Joint N+S D2 Benchmark Report")
a("")
a("**Classification: C = `BEATS_ARIA2_ONLY`.** Under the frozen D2 workload, the joint")
a("adaptive N+S policy (existing heuristic `JointPolicy`, frozen configuration) adapted")
a("both N and S online in 20/20 runs, beat the frozen aria2 configuration by 12.4 %,")
a("but was **2.32x slower than the best fixed configuration (N=16, S=4 MiB)**. Beating")
a("aria2 alone is not adaptation evidence; the adaptation-value test (joint vs best")
a("fixed) failed.")
a("")
a("Generated: %s" % time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
a("")
a("## 1. Scope and frozen design")
a("")
a("This is Track 1 only (no Track 2 work, no server changes, no RL). The design was")
a("frozen in `manifest/track1_manifest.json` before execution:")
a("")
a("- **Workload:** exact D2 mechanism — 512 MiB logical object, HTTP/1.1, per-response")
a("  pacing 640 KiB/s (0-2 s) -> 160 KiB/s (2-4 s) -> 2500 KiB/s (>=4 s), rate sampled")
a("  at request start and held, tc/netem 40 ms + 0.4 % on `lo`, full content/hash")
a("  verification, fresh server/output per run.")
a("- **Modes:** fixed (16, 4 MiB); N-only (`ThroughputPolicy`); S-only")
a("  (`ChunkSizePolicy`); joint (`JointPolicy`). LinUCB intentionally not run.")
a("- **Frozen controller config:** initial (N=4, S=4 MiB); N in [1,16], step +/-1;")
a("  S transition `S' = clamp(S +/- 1 MiB, 256 KiB, 16 MiB)` (reachable set")
a("  {256 KiB} union {1..16 MiB}); one serialized decision per completed attempt;")
a("  thresholds +/-10 %; policy window 4 attempts; 30 s timeout; 3 same-range retries.")
a("- **Design:** 20 blocks, all four modes per block in independently randomized order,")
a("  80 planned runs, no early stopping, no tuning after results; invalid runs")
a("  preserved and never replaced.")
a("")
a("## 2. Execution summary")
a("")
a("| | |")
a("|---|---|")
a("| Valid runs | **80 / 80** (0 invalid) |")
a("| Blocks | 20 (4 modes each) |")
a("| Content verification | 80/80 byte-exact against the logical generator |")
a("| Retries / timeouts | 0 across all 80 runs |")
a("| aria2 consistency runs (frozen flags, outside matrix) | 3; mean 53.27 s vs frozen 30-pair mean 53.56 s (-0.5 %) |")
a("")
a("## 3. Completion-time results")
a("")
a("| mode | n | mean | median | min | max |")
a("|---|---|---|---|---|---|")
for m in MODES:
    s = J["modes"][m]
    a("| %s | %d | %s s | %s s | %s s | %s s |" % (
        LABEL[m], s["n"], ms(s["mean_ms"]), ms(s["median_ms"]), ms(s["min_ms"]), ms(s["max_ms"])))
a("")
a("## 4. Primary comparisons (frozen rules)")
a("")
a("| comparison | n pairs | geometric ratio | 95 % CI | wins/losses | beats? |")
a("|---|---|---|---|---|---|")
c = J["comparisons"]["joint_vs_fixed"]
a("| **joint vs best fixed** (primary) | %d | **%.4f** | [%.4f, %.4f] | %d/%d | %s |" % (
    c["n_pairs"], c["geo_ratio"], c["ci95"][0], c["ci95"][1], c["wins"], c["losses"], c["beats"]))
for key, lab in [("joint_vs_n-only", "joint vs N-only"), ("joint_vs_s-only", "joint vs S-only")]:
    c = J["comparisons"][key]
    a("| %s | %d | %.4f | [%.4f, %.4f] | %d/%d | %s |" % (
        lab, c["n_pairs"], c["geo_ratio"], c["ci95"][0], c["ci95"][1], c["wins"], c["losses"], c["beats"]))
c = J["comparisons"]["joint_vs_frozen_aria2"]
a("| **joint vs frozen aria2** | %d/%d | **%.4f** | [%.4f, %.4f] | n/a (unpaired) | %s |" % (
    c["n_joint"], c["n_aria2"], c["geo_ratio"], c["ci95"][0], c["ci95"][1], c["beats"]))
a("")
a("The `beats` rule (predeclared): geometric ratio <= 0.95 **and** 95 % CI upper bound")
a("< 1.0. The joint policy is **2.32x slower** than the best fixed configuration and")
a("**1.27x slower** than the N-only adaptive policy, but **12.4 % faster** than the")
a("frozen aria2 configuration.")
a("")
a("**The aria2 win is not adaptation evidence.** The fixed (16, 4 MiB) configuration")
a("beats the same frozen aria2 reference by ~62 %; the joint policy's 12.4 % margin is")
a("a weaker exploitation of the same phase-locked-pacing mechanism, not a gain from")
a("adaptation.")
a("")
a("## 5. Adaptation evidence (joint mode)")
a("")
a("- **N changed** in 20/20 runs; **S changed** in 20/20 runs; **both changed** in")
a("  **20/20 runs** (100 %) — genuine joint movement by the task's definition.")
a("- Median final state **(N=6, S=6 MiB)**; all 20 runs reached exactly (6, 6) (the")
a("  policy is deterministic and the signal is identical across runs).")
a("- N trajectory: (4,4) -> (5,4) -> (6,4) -> (6,5) -> (6,6), i.e. +2 concurrency and")
a("  +2 MiB chunk steps; 4 applied actions total, then 85 no-ops.")
a("- All actions occur in a **~30 ms window at ~8.62-8.65 s** (first action median")
a("  8.624 s, last action median 8.636 s); no action occurs before 8.6 s or after")
a("  8.65 s.")
a("- **Movement is away from the optimum**: the best fixed state is (16, 4 MiB); the")
a("  joint policy stops at (6, 6 MiB) — N too low and S too large.")
a("")
a("N-only final state: median **(8, 4 MiB)**; S-only final state: median **(4, 8 MiB)**.")
a("Both adapt but stop far from the optimum as well.")
a("")
a("## 6. Mechanism analysis (from recorded trajectories and request logs)")
a("")
a("1. **Decision cadence + initial granularity delay the first decision.** Decisions")
a("   require a completed attempt; with the frozen initial S=4 MiB at 640 KiB/s the")
a("   first four attempts complete at ~6.6 s, so the first policy decision happens at")
a("   ~6.7 s and the first *actionable* decisions at ~8.6 s — after the pacing")
a("   recovery. The controller cannot act during the phase that determines the D2")
a("   mechanism.")
a("2. **The policy signal saturates.** The policies react to the windowed *per-request*")
a("   throughput. It rises from ~607 KiB/s (slow phase) to ~2184 KiB/s at the recovery")
a("   and then stays flat (~2210 KiB/s) for the remaining ~85 decisions. Once the")
a("   +/-10 % rule stops firing, no further actions occur: the controllers freeze at")
a("   (6,6) / (8,4) / (4,8). The recorded signal profile is in")
a("   `analysis/mechanism_track1.json` (`joint_signal`).")
a("3. **Adaptation lag vs file progress.** By the first actionable decision (~8.6 s)")
a("   most of the 512 MiB remains, but the policy performs only +/-1 steps per")
a("   actionable decision and has no further signal to continue; it cannot ramp N from")
a("   4 to 16.")
a("4. **Granularity/phase interaction.** Final request counts: fixed 128 (all 4 MiB),")
a("   N-only 128, S-only 69 (8 MiB), joint 89 (6 MiB). Request start phases: fixed 16")
a("   requests <2 s and 112 >=4 s; the adaptive modes have only 4 requests <2 s and")
a("   the rest >=4 s (their first 4 MiB requests straddle the 2-4 s dip). Larger final")
a("   S increases each request's service time (6 MiB at 2.5 MiB/s = 2.4 s) and reduces")
a("   scheduling flexibility; combined with N=6 this yields the 46.9 s completion.")
a("5. **Retries/timeouts played no role:** 0 retries and 0 failures in all 80 runs.")
a("")
a("## 7. Classification (predeclared A-E)")
a("")
a("| check | result |")
a("|---|---|")
a("| joint adaptation demonstrated (>=50 % of runs changed both N and S) | yes (100 %) |")
a("| joint beats best fixed (ratio <= 0.95, CI upper < 1) | **no** (ratio 2.3161) |")
a("| joint beats frozen aria2 | **yes** (ratio 0.8759, CI upper 0.8801) |")
a("| **classification** | **C = `BEATS_ARIA2_ONLY`** |")
a("")
a("## 8. Empirical results vs interpretation")
a("")
a("**Empirical (measured, 80/80 valid):** the completion times, ratios, CIs, win/loss")
a("counts, the recorded trajectories, final states, action times, request logs, and the")
a("zero retry count above.")
a("")
a("**Interpretation (not directly measured):** the explanation that the per-request")
a("throughput signal saturation plus the decision cadence prevents the existing")
a("heuristic policies from reaching the optimal concurrency. It is supported by the")
a("recorded signal profile and action timestamps but is a mechanistic reading, not a")
a("controlled experiment on the policy's signal.")
a("")
a("## 9. Limitations")
a("")
a("- The heuristic policies are the *existing* implementations with frozen bounds,")
a("  step sizes, thresholds, and initial state; the study measures their behavior, not")
a("  the best achievable adaptive policy.")
a("- The aria2 comparison uses the immutable frozen 30-pair aria2 distribution")
a("  (unpaired); three fresh aria2 runs under the same session conditions agreed")
a("  within 0.5 %, supporting cross-session comparability, but the comparison remains")
a("  unpaired.")
a("- 20 blocks give narrow CIs for the observed deterministic behavior; the result is")
a("  specific to the frozen D2 workload and controller configuration.")
a("- No LinUCB, no Track 2 mechanism, no server changes were part of this pass.")
a("")
a("## 10. Figures")
a("")
for fn, cap in [
    ("fig1_n_trajectory.png", "N trajectories (joint, N-only) vs the fixed optimum"),
    ("fig2_s_trajectory.png", "S trajectories (joint, S-only) vs the fixed optimum"),
    ("fig3_completion_comparison.png", "completion by mode and per-block pairing"),
    ("fig4_cumulative_bytes.png", "cumulative bytes vs time (median run per mode)"),
    ("fig5_request_sizes.png", "request-size distributions by mode"),
    ("fig6_phase_mechanism.png", "D2 phase mechanism: request start vs sampled rate"),
    ("fig7_signal_saturation.png", "the policy signal saturates after the recovery"),
    ("fig8_paired_ratios.png", "paired per-block ratios"),
]:
    a("- `figures/%s` — %s" % (fn, cap))
a("")
a("## 11. Artifacts")
a("")
a("- `manifest/track1_manifest.json` (frozen before execution),")
a("  `manifest/run_schedule.json`; `per_run/` (80 results), `raw/` (runner logs +")
a("  validity metadata), `logs/`; `analysis/track1_analysis.json`,")
a("  `analysis/mechanism_track1.json`; `figures/fig1..fig8`; `tools/bench_ns.go`,")
a("  `tools/run_track1.py`, `tools/analyze_track1.py`, `tools/mechanism_track1.py`,")
a("  `tools/make_figures.py`, `tools/make_report.py`.")
a("- Frozen prior evidence untouched: the 30-pair aria2 competitive benchmark, all")
a("  mechanism screens, and the historical results are unchanged.")
a("")
report = "\n".join(L) + "\n"
os.makedirs(ROOT_REPORTS, exist_ok=True)
open(os.path.join(ROOT_REPORTS, "joint_ns_d2_report.md"), "w").write(report)
open(os.path.join(BASE, "reports", "joint_ns_d2_report.md"), "w").write(report)
print("report written to %s and %s" % (ROOT_REPORTS, os.path.join(BASE, "reports")))
