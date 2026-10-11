# Figure Selection

Synthesis of completed evidence only. No new experiments were run.

Evidence-type labels are mandatory on every final figure: **LIVE EXPERIMENTAL**,
**TRACE REPLAY**, **DETERMINISTIC ANALYTICAL MODEL**, **SIMULATION**,
**DIAGNOSTIC / EXPLORATORY**, **SCHEMATIC**.

---

## 1. Inventory of existing figures

| Figure | Source artifact | Type | Scientific message | Redundancy | Decision |
|---|---|---|---|---|---|
| `final_research_audit/figures/fig1_lockin_scores.svg` | §22 audit | LIVE + SIMULATION | LinUCB arm lock-in | Superseded by the S/N closure story | EXCLUDE |
| `final_research_audit/figures/fig2_standard_vs_periodic_coverage.svg` | §22 audit | LIVE | Coverage vs preference | Descriptive, not needed for the closure | EXCLUDE |
| `final_research_audit/figures/fig3_wallclock_timeline.svg` | §22.18 | LIVE | Wall-clock throttle timeline | Context only | EXCLUDE |
| `final_research_audit/figures/fig4_wallclock_concurrency.svg` | §22.18 | LIVE | Concurrency under wall-clock | Context only | EXCLUDE |
| `final_research_audit/figures/fig5_aria2_vs_matched.svg` | §22.20 | LIVE | aria2 vs matched internal | Reused as part of final Fig. 7 | KEEP (merge) |
| `final_evaluation/figures/fig1_per_condition_means.svg` | §22.26 | LIVE | DQN vs aria2 per condition | Reused as part of final Fig. 7 | KEEP (merge) |
| `final_evaluation/figures/fig2_paired_delta_hist.svg` | §22.26 | LIVE | Paired delta distribution | Detailed; optional | EXCLUDE |
| `final_evaluation/figures/fig3_relative_reduction.svg` | §22.26 | LIVE | Relative reduction per condition | Detailed; optional | EXCLUDE |
| `final_evaluation/figures/fig4_dqn_concurrency_u1.svg` | §22.26 | LIVE | DQN concurrency (U1) | Behavioral detail | EXCLUDE |
| `final_evaluation/figures/fig5_dqn_latency_u5.svg` | §22.26 | LIVE | DQN latency (U5) | Behavioral detail | EXCLUDE |
| `salvage_s_scaling_analysis/fig1..fig7` (7 PNGs) | §22.85 | DETERMINISTIC ANALYTICAL MODEL | Fixed-S scaling; gains; envelope; tail share | Source for final Figs. 3–4 | KEEP (re-render) |
| `salvage_s_mechanism_pivot/fig1..fig7` (7 PNGs) | §22.86 | DETERMINISTIC ANALYTICAL MODEL | S state tables; transitions; volatility | Source for final Fig. 5 | KEEP (re-render) |
| `adaptive_n_mechanism_screen/fig1..fig8` (8 PNGs) | §22.87 | DETERMINISTIC ANALYTICAL MODEL | N state tables; transitions; robustness | Source for final Fig. 6 | KEEP (re-render) |
| (none) | Stage 0S | LIVE | Service time vs chunk size | Required mechanism validation | CREATE (final Fig. 2) |
| (none) | Architecture | SCHEMATIC | System/measurement loop | Required for Fig. 1 | CREATE (final Fig. 1) |

No figures exist for the five-trace oracle study (it is report/table-based).

---

## 2. Final compact figure set (7 figures)

All files in `testdata/final_project_synthesis/figures/`.

| Final figure | Type label in title | Message | Source evidence |
|---|---|---|---|
| `fig1_system_methodology.png` | SCHEMATIC | Range allocation, N, S, future-only adaptation, measurement loop | Architecture (source + §3) |
| `fig2_stage0s_mechanism_validation.png` | LIVE EXPERIMENTAL | Service time vs S for 256 KiB–16 MiB with the `D+s/R` reference (+2.4–2.5%) | Stage 0S `per_arm.json` |
| `fig3_s_headroom_closure.png` | DETERMINISTIC ANALYTICAL MODEL | Frozen S strategies' relative gain vs file size; exploratory single-switch envelope; 5% threshold | Scaling `scaling_analysis.json` |
| `fig4_absolute_vs_relative.png` | DETERMINISTIC ANALYTICAL MODEL | Absolute time saved (negative) and the bulk/tail decomposition showing tail dilution | Scaling `scaling_analysis.json` |
| `fig5_s_mechanism_screening.png` | DETERMINISTIC ANALYTICAL MODEL | Preferred S flat across stationary states; changing-state causal gains zero/harmful | Pivot `screening.json` |
| `fig6_n_mechanism_screening.png` | DETERMINISTIC ANALYTICAL MODEL (NOT LIVE-GROUNDED) | Preferred N rises with C (analytical); causal gains up to +6.7% at severe drops; live-grounding boundary in the title | N screen `screening.json` |
| `fig7_practical_baseline_evidence.png` | LIVE EXPERIMENTAL | Granularity-matched aria2 control (n=10) and the final DQN-vs-aria2 head-to-head (240 pairs) | §22.20; `final_evaluation/statistical_analysis.json` |

**Excluded:** all per-study figures not listed above (superseded by the closure
story or redundant detail). The final paper should use at most these 7.

---

## 3. Figure honesty rules (enforced)

1. Every final figure title contains its evidence-type label; simulated and
   analytical results are never styled as experimental measurements.
2. The analytical +6.7% N headroom appears only in Fig. 6 with the explicit
   "NOT LIVE-GROUNDED" label; it is never placed next to the live Stage 0S or
   aria2 results without separation.
3. The exploratory single-switch envelope in Fig. 3 is labelled EXPLORATORY and
   is never called a measured speedup.
4. Oracle results (Figs. 5–6) are labelled diagnostic and are never presented as
   deployable policies.
5. The aria2 figures (Fig. 7) are labelled with their tested conditions and do
   not rank the downloader against aria2.
6. Fig. 2's uncertainty is the measured Stage 0S data only; the analytical line
   is deterministic and carries no fabricated error bars.
