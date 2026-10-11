#!/usr/bin/env python3
"""Generate the Track 2 report (root reports/ and study reports/)."""
import json
import os
import time

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
ROOT_REPORTS = "/workspaces/mvp/reports"
J = json.load(open(os.path.join(BASE, "analysis", "prescreen.json")))
A = json.load(open(os.path.join(BASE, "analysis", "track2_analysis.json")))
M = json.load(open(os.path.join(BASE, "manifest", "track2_manifest.json")))
MIB = 1024 ** 2

L = []
a = L.append
a("# Track 2 — N-Dependent Server Mechanism: Analytical Pre-Screen Report")
a("")
a("**Final status: `PRE_SCREEN_FAILED_NO_QUALIFYING_SCENARIO`** (maps to")
a("`LIVE_MECHANISM_GATE_FAILED_JOINT`; the live gate was **not reached**). Per the")
a("frozen process, no live server extension, no live fixed-grid gate, no adaptive")
a("experiment and no aria2 comparison were performed. The mechanism does not produce a")
a("genuine state-dependent joint optimum under its own faithful semantics; per the")
a("task's stop rule it was not tuned until it did.")
a("")
a("Generated: %s" % time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
a("")
a("## 1. The mechanism and its relationship to `sim_env_v5.py`")
a("")
a("The pre-screen reproduces the exact semantics of the project's existing simulator")
a("(`python/rl/sim_env_v5.py`), which is the mechanism the submitted proposal promised")
a("to implement as controlled N-dependent server throttling:")
a("")
a("| simulator semantics | live correspondence (pre-screen model) |")
a("|---|---|")
a("| `p_eff(k) = min(0.9, loss + congestion*(k-1))` per request | per-attempt failure probability at the active-request count `k` |")
a("| `svc = latency + overhead*(k-1) + n/per` | service time; `overhead*(k-1)` is the per-extra-connection delay |")
a("| `per = min(rate, link/k)` | fair-share per-response rate sampled at request start |")
a("| `failed -> svc += latency + n/per` (one full retransmission) | the failed attempt consumes the full service time, then fails; the downloader retries the **same range** with its existing semantics (max 3 attempts, then the download aborts) |")
a("| (simulator has no timeout/abort) | 30 s per-attempt client timeout, 0.1 s retry delay, client cycle overhead 0.088 s |")
a("")
a("The explicitly rejected substitute `R_eff = R*(1-loss_eff)` was **not** used: the")
a("mechanism is probabilistic request failure, not rate reduction.")
a("")
a("## 2. Predeclared family and selection rule (frozen before evaluation)")
a("")
a("- **GOOD:** rate 2.5 MiB/s, link 0, loss 0.4 %, congestion 0, overhead 20 ms, latency 20 ms.")
a("- **DEGRADED candidates (16):** rate {0.4, 0.5} MiB/s x loss {2 %, 3 %} x congestion")
a("  {0.02, 0.04, 0.06} with link 0 (12), plus 4 link variants (rate 1.0 MiB/s, link")
a("  {2.0, 3.0} MiB/s, congestion {0.04, 0.06}, loss 3 %).")
a("- **Grid:** N in {1,2,4,8,16}; S = the exact frozen Track-1 action set (256 KiB plus")
a("  1..16 MiB); 20 seeds per cell; timeout/retry semantics as above.")
a("- **Selection rule:** a pair qualifies iff (1) N* and (2) S* differ between states,")
a("  (3) material cross-state penalty >= 10 %, (4) coordinate-wise margins >= 5 % in")
a("  both states, (5) the optima are reachable (abort rate <= 20 %, second-best")
a("  completes in >= 50 % of seeds). Tie-break: smallest file size, then max")
a("  min(penalty), then larger congestion, then larger rate.")
a("- **Stop rule:** no qualifying candidate -> STOP TRACK 2, no tuning.")
a("")
a("**Pre-screen revisions (evaluation-design only; the candidate family was never")
a("changed):** revision 0 used F = 32 MiB and a second-best-cell separation metric and")
a("was degenerate (F/S < N at large S; exact ties). Revision 1 used F = 64 MiB with")
a("coordinate-wise margins. Revision 2 evaluated the same family at the predeclared")
a("workload scales F in {64, 128, 256} MiB (the N-degeneracy is a function of F/S vs N).")
a("All revisions are recorded in `manifest/track2_manifest.json`.")
a("")
a("## 3. Pre-screen results (all 16 candidates x 3 file sizes)")
a("")
a("| file size | GOOD optimum | best degraded candidate | N* | S* | N margin | S margin | penalties | qualifies |")
a("|---|---|---|---|---|---|---|---|---|")
for fb in ["67108864", "134217728", "268435456"]:
    e = J["file_sizes"][fb]
    go = e["good"]["optimum"]
    best = None
    for nm, c in e["candidates"].items():
        if not c["optimum"]:
            continue
        score = min(c.get("penalty_good", 0), c.get("penalty_degraded", 0))
        if best is None or score > best[1]:
            best = (nm, score, c)
    nm, score, c = best
    o = c["optimum"]
    a("| %d MiB | (N=%d,S=%.0fM) %.1fs | %s | %d | %.0f MiB | %.0f%% | %.0f%% | +%.0f%%/+%.0f%% | %s |" % (
        int(fb) // MIB, go["N"], go["S"] / MIB, go["completion"], nm,
        o["N"], o["S"] / MIB, 100 * (o["N_margin"] or 0), 100 * (o["S_margin"] or 0),
        100 * c.get("penalty_good", 0), 100 * c.get("penalty_degraded", 0),
        "yes" if c.get("qualifies") else "no"))
a("")
a("**No candidate qualifies at any predeclared file size.** The strongest candidate is")
a("`D_r0.4_l0.02_c0.04` at F = 64 MiB.")
a("")
a("## 4. The strongest candidate and why it fails the joint gate")
a("")
a("| | GOOD | DEGRADED |")
a("|---|---|---|")
a("| optimum | (N=16, S=4 MiB), %.2f s | (N=8, S=11 MiB), %.2f s |" % (
    J["file_sizes"]["67108864"]["good"]["optimum"]["completion"],
    J["file_sizes"]["67108864"]["candidates"]["D_r0.4_l0.02_c0.04"]["optimum"]["completion"]))
a("| N margin (best vs other N at the same S) | 46.8 %% | **0.0 %%** |")
a("| S margin (best vs other S at the same N) | 14.3 %% | **1.0 %%** |")
a("| cross-state penalty | using DEGRADED's optimum costs +134.4 %% | using GOOD's optimum costs +10.7 %% |")
a("| abort rate at the optimum | 0 %% | 0 %% (the optimum sits just below the cliff) |")
a("")
a("Degraded completions at the optimum S = 11 MiB: N=1 160.6 s, N=2 82.7 s,")
a("N=4 55.1 s, **N=8 27.6 s, N=16 27.6 s**. The N* \"movement\" (16 -> 8) is")
a("**degenerate**: N=8 and N=16 are exactly equivalent because only six requests")
a("exist at that S; the argmin picks N=8 by iteration order, not by performance.")
a("The S* movement is a **cliff boundary**: the optimum is the largest S below the")
a("30 s timeout cliff (S > per x 30 s = 12 MiB aborts), and its margin over the next")
a("smaller S is 1.0 %.")
a("")
a("Additionally, the best **single** fixed configuration across the two states")
a("(N=16, S=4 MiB) is within **10.7 %** of the degraded optimum, so even an S-only")
a("controller gains at most ~10 % over the best fixed config in the degraded state —")
a("and no N change is required to obtain it.")
a("")
a("## 5. Structural reasons (why the mechanism does not produce a joint opportunity)")
a("")
a("1. **Abort cliff.** The failure-based concurrency penalty (`p_eff(16)=0.62` at")
a("   congestion 0.04) plus the downloader's 3-attempt retry limit makes high-")
a("   concurrency cells **abort** (P(3 failures) = 23.8 % per range) rather than")
a("   become smoothly slower. The simulator's smooth interior N optimum (its")
a("   one-retransmission cost model, L4 bestC=2-6) does not survive the live")
a("   retry/abort semantics.")
a("2. **Per-request, size-independent failure cost.** The retry cost per byte is")
a("   constant, so the degraded state favours fewer, larger requests; S* is pushed")
a("   to the timeout boundary rather than balancing against a size-dependent cost.")
a("3. **N degeneracy at that boundary.** At the timeout-boundary S* the request")
a("   count is small (F/S = 6), so N beyond it is irrelevant: the N optimum is a")
a("   plateau, not a separated interior point.")
a("4. **Marginal adaptive value.** The best single fixed configuration is within")
a("   10.7 % of the degraded optimum, and an S-only switch suffices; the joint")
a("   movement is neither necessary nor cleanly separated.")
a("")
a("## 6. Fixed-grid live gate, adaptive experiment, aria2")
a("")
a("**NOT REACHED / NOT RUN.** The frozen rule stops Track 2 when no candidate")
a("qualifies; the live server extension was therefore not implemented (implementing")
a("it would only reproduce an already-characterised negative), and no live fixed")
a("grid, adaptive controller, or aria2 comparison was executed. No Track-2")
a("environment changes were made; no frozen artifact was touched.")
a("")
a("## 7. Limitations")
a("")
a("- Analytical only: the pre-screen models the proposed live semantics exactly, but")
a("  no live confirmation was performed (per the frozen stop rule).")
a("- The candidate family is finite (16 degraded candidates x 3 workload scales); a")
a("  different family (e.g., a size-dependent failure probability or a server that")
a("  degrades without hard request failures) could behave differently, but inventing")
a("  one is out of scope for this task.")
a("- The 3-attempt retry limit is the frozen downloader semantics; relaxing it is not")
a("  permitted and would change the mechanism's character.")
a("")
a("## 8. What this means")
a("")
a("The mechanism the proposal promised (configurable N-dependent server throttling)")
a("was reproduced exactly in analysis and does **not** create a genuine joint N+S")
a("opportunity: its concurrency penalty is a retry-limit cliff, its S preference is")
a("pushed to the timeout boundary, and the N optimum there is degenerate. The")
a("defensible competitive result remains the fixed **N=16, S=4 MiB D2 aria2 win")
a("(62.4 %)** and the Track-1 finding that the existing adaptive heuristics do not")
a("add value on D2 (`BEATS_ARIA2_ONLY`).")
a("")
a("## 9. Figures")
a("")
for fn, cap in [
    ("fig1_surface_good.png", "GOOD state completion surface T(N,S)"),
    ("fig2_surface_degraded.png", "DEGRADED state completion surface (timeout cliff visible)"),
    ("fig3_completion_vs_N.png", "state-dependent N optima (degraded N=8/N=16 tie)"),
    ("fig4_completion_vs_S.png", "completion vs S with the timeout cliff"),
    ("fig5_abort_surface.png", "degraded abort-rate surface (retry-limit cliff)"),
    ("fig6_candidates.png", "N*/S* across all predeclared candidates and file sizes"),
]:
    a("- `figures/%s` — %s" % (fn, cap))
a("")
a("## 10. Artifacts")
a("")
a("- `manifest/track2_manifest.json` (frozen family/rule, revisions, stop decision);")
a("  `analysis/prescreen.json` (+ revision snapshots), `analysis/track2_analysis.json`;")
a("  `figures/fig1..fig6`; `tools/ns_sim.py`, `tools/prescreen.py`,")
a("  `tools/analyze_track2.py`, `tools/make_figures.py`, `tools/make_report.py`.")
a("")
report = "\n".join(L) + "\n"
os.makedirs(ROOT_REPORTS, exist_ok=True)
open(os.path.join(ROOT_REPORTS, "n_dependent_server_joint_ns_report.md"), "w").write(report)
open(os.path.join(BASE, "reports", "n_dependent_server_joint_ns_report.md"), "w").write(report)
print("report written to %s and %s" % (ROOT_REPORTS, os.path.join(BASE, "reports")))
