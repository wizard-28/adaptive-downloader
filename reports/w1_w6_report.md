# W1-W6 six-workload family: live fixed-grid validation and adaptive comparisons

Status: **six predeclared controlled workloads** (frozen manifest
`manifest/w1_w6_manifest.json`, designed analytically before any live W1-W6
run). Claim separation: **(A)** fixed-config competitive result vs aria2 under
D2 (the 62.4 % result, separate report); **(B)** workload-level configuration
selection (this report); **(C)** within-download joint adaptation (Track 2-B
P2, separate report). This report is a **(B)** result; it is not
within-download adaptation and makes no Internet-wide generalization claim.

## 1. Frozen workload family

| workload | T (s) | cap C (MiB/s) | gamma | cap at t (s) | model optimum B | model B completion | base-side penalty |
|---|---|---|---|---|---|---|---|
| W1 (healthy) | 1 | - | - | - | (16, 1 MiB) | 18.4 s | - |
| W2 (T=1,C=4M,g=0.10,t=4) | 1 | 4 | 0.1 | 4 | (2, 4 MiB) | 160.5 s | 579 % |
| W3 (T=2,C=4M,g=0.10,t=4) | 2 | 4 | 0.1 | 4 | (2, 4 MiB) | 160.5 s | 538 % |
| W4 (T=2,C=4M,g=0.10,t=8) | 2 | 4 | 0.1 | 8 | (2, 4 MiB) | 160.0 s | 538 % |
| W5 (T=2,C=8M,g=0.10,t=4) | 2 | 8 | 0.1 | 4 | (4, 4 MiB) | 94.8 s | 231 % |
| W6 (T=2,C=12M,g=0.10,t=4) | 2 | 12 | 0.1 | 4 | (4, 4 MiB) | 67.9 s | 231 % |

Selection rule (predeclared, model only): maximize sum(min(cap_gain, base_pen)) over 5-subsets of qualified candidates with transition/cap coverage and lexicographic tie-breaks. relaxed: >= 2 distinct transitions, >= 2 distinct caps; 13/72 candidates qualified. No t=12 candidate qualified, so coverage relaxed as documented.

## 2. Live fixed-grid validation

Per workload: cells {4,8,16} x {1,4,16 MiB} + predicted optimum, 2 reps,
netem 40 ms + 0.4 %, timeout 30 s, retries 3. Invalid runs preserved.

| workload | live argmin | live argmin | predicted | match | invalid runs |
|---|---|---|---|---|---|
| W1 | 16/1048576 | 18.5 s | 16/1048576 | yes | 0 |
| W2 | 2/4194304 | 163.2 s | 2/4194304 | yes | 6 |
| W3 | 2/4194304 | 163.0 s | 2/4194304 | yes | 6 |
| W4 | 2/4194304 | 162.3 s | 2/4194304 | yes | 6 |
| W5 | 16/16777216 | 85.4 s | 4/4194304 | **no** | 0 |
| W6 | 4/4194304 | 68.6 s | 4/4194304 | yes | 0 |

Family-best single fixed configuration (live argmin of the mean over the six
workloads, common cells only): **4/4194304** (mean 125.2 s).

## 3. Adaptive comparisons (10 fresh paired blocks per workload)

Modes: frozen band selector (probe (16,1 MiB); 25 s post-drop window; nearest
of the frozen levels) | per-workload best fixed (live argmin) | family best
single fixed (live) | aria2. Invalid runs preserved; selector aborts count as
losses.

| workload | selector | per-workload fixed | family fixed | aria2 | selector vs per-workload | selector vs family | selector vs aria2 |
|---|---|---|---|---|---|---|---|
| W1 | 18.4 s | 18.4 s | 65.3 s | 53.2 s | geo 1.013 [1.000, 1.030], 4/10 | geo 0.286 [0.283, 0.291], 10/10 | geo 0.354 [0.346, 0.365], 10/10 |
| W2 | 152.3 s | 162.8 s | 175.1 s | 53.2 s | geo 0.934 [0.932, 0.936], 10/10 | geo 0.869 [0.868, 0.871], 10/10 | geo 2.981 [2.848, 3.247], 0/10 |
| W3 | 166.2 s | 163.0 s | 175.0 s | 53.4 s | geo 1.019 [1.017, 1.022], 0/10 | geo 0.949 [0.947, 0.951], 10/10 | geo 3.107 [3.094, 3.119], 0/10 |
| W4 | 131.1 s | 162.1 s | 171.6 s | 53.4 s | geo 0.808 [0.806, 0.811], 10/10 | geo 0.765 [0.763, 0.767], 10/10 | geo 2.433 [2.399, 2.461], 0/10 |
| W5 | 101.0 s | 85.9 s | 95.3 s | 53.4 s | geo 1.172 [1.163, 1.180], 0/10 | geo 1.058 [1.055, 1.060], 0/10 | geo 1.883 [1.870, 1.894], 0/10 |
| W6 | 75.1 s | 68.8 s | 68.6 s | 53.3 s | geo 1.088 [1.084, 1.092], 0/10 | geo 1.090 [1.084, 1.095], 0/10 | geo 1.394 [1.377, 1.407], 0/10 |

Pooled over all workload-blocks (geometric mean of paired ratios, 95 % bootstrap):

| comparison | n | geo mean | 95 % CI | wins |
|---|---|---|---|---|
| selector vs per-workload best fixed | 60 | 0.999 | [0.969, 1.029] | 24/60 |
| selector vs family best single fixed | 60 | 0.770 | [0.682, 0.860] | 40/60 |
| selector vs aria2 | 60 | 1.660 | [1.363, 1.988] | 10/60 |

## 4. Selector behavior

Frozen levels (B/s): 104857, 104857, 104857, 209715, 314572 -> targets 2:4194304, 2:4194304, 2:4194304, 4:4194304, 4:4194304.
Three workloads (W2-W4) share the same degraded signature and optimum, so the
selector's effective decision is between two configurations while still
covering all six workloads; this is a property of the frozen family, not a
post-hoc choice.

**W5 frozen-target mismatch (honest):** the live validation found W5's optimum
at (16, 16 MiB) 85.4 s, not the model's (4, 4 MiB) 95.7 s (retry/k feedback
under the cap raises rates for retried ranges). The selector's target stayed
frozen at the model value, so W5 is the selector's largest loss vs the
per-workload clairvoyant baseline (geo 1.172). Retuning the frozen mapping
after observing live outcomes is prohibited by the protocol; the mismatch is
reported instead.

## 5. Caveats

- Pooled CIs treat 60 workload-blocks as independent. Robustness (analysis/w1_w6_robustness.json, tools/robustness_w1_w6.py): selector vs per-workload pooled 0.999, stratified-bootstrap [0.996, 1.002], six-cluster t-CI [0.873, 1.143]. Selector vs family single fixed pooled 0.770, stratified [0.768, 0.772], six-cluster t-CI [0.455, 1.304]; the pooled figure is driven by W1 and is not robust across the six workloads.
- The selector is a within-download reconfiguration (one probe-to-target switch). Its W4 win (131 s vs 162 s best fixed) is partly an early-window effect, so B-vs-C separation is a semantic distinction, not a clean one.
- The design was amended twice after seeing design output (classification window; coverage constraint); see PROJECT_CONTEXT section 22.100 amendment log.

- Controlled single-server loopback scenario; no real-network claim.
- The selector is one-shot per download and does not re-probe.
- W1 (healthy) completes before any decision point in most runs; that is the
  correct behavior for a healthy workload.
- One model-vs-live mismatch (W5, see contrast 4) is disclosed; the adaptive
  baselines use live argmins, so the comparison stays fair (and is
  conservative for the selector).
- Timeout sensitivity and the capped S-margin precision (Track 2-B) are
  reported separately in `reports/track2b_soft_cap_report.md`.

## 6. Artifacts

- `manifest/w1_w6_manifest.json` (frozen design), `analysis/w1_w6_design.json`,
  `analysis/w1_w6_grid.json`, `analysis/w1_w6_adaptive.json`.
- `w1_w6_grid/` (114 runs), `w1_w6_adaptive/` (240 runs), `smoke/` (band smoke).
- `figures/figW1_modes.png`, `figW2_ratios.png`, `figW3_grids.png`,
  `figW4_selector.png`; tools `design_w1_w6.py`, `run_w1_w6_grid.py`,
  `run_w1_w6_adaptive.py`, `make_figures_w1_w6.py`, `make_report_w1_w6.py`.
