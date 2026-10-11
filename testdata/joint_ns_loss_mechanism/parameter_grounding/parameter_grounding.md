# Parameter Grounding — Correlated Loss / Range-Retry Mechanism

Frozen before any simulation result was inspected. Every parameter below is either
(a) taken from immutable project evidence, (b) taken from explicitly cited external
measurements/literature, or (c) declared as a modelling abstraction with a
predeclared sensitivity axis. No parameter was chosen to produce a favourable
outcome.

## 1. What the project contains (loss evidence)

| Evidence | Value | Artifact |
|---|---|---|
| D2 workload netem loss | **0.4 %** (constant, uncorrelated) | `testdata/diagnostics/long_duration_d2_diagnostic/`, `docs/PROJECT_CONTEXT.md` §22.89 |
| Controlled-scenario netem loss | **1 %** (constant) | request-granularity / wall-clock / v4–v6 validation matrices |
| L1/L4 severe-phase netem loss | **2.5 % and 3.0 %** (constant, hand-authored phases) | `docs/PROJECT_CONTEXT.md` §22.59–§22.70, `testdata/diagnostics/l4_environment_design/` |
| DQN validation/test loss range | **0–4 % train, 4.5 % val, 7 % test** (constant) | `docs/PROJECT_CONTEXT.md` §22.22–§22.24 |
| Native Mahimahi traces | aggregate capacity only; **no loss information** | `testdata/adaptive_n_mechanism_screen/inventory/n_trace_inventory.md` |
| External mobile throughput traces (UGent 4G) | `packet_loss: unavailable` | `testdata/diagnostics/m2_trace_acquisition/trace_inventory.json` |
| Request-granularity "loss" scenario (1 %) | 4.0 attempts / 4 ranges = **no request failures under 1 % constant loss** | `testdata/diagnostics/request_granularity/report.txt` |

**Conclusion:** the project grounds the *loss-rate range* (clean ≈ 0.4–1 %; severe
≈ 2.5–7 %) but contains **no correlated/burst-loss measurements and no burst-duration
measurements**. The transition/burst parameters therefore require explicit external
grounding (task §19 permits this if recorded).

## 2. External grounding (explicit citations)

- **Two-state Gilbert–Elliott model** as the standard correlated-loss model:
  J. Pieper, S. Voran, *Relationships Between Gilbert–Elliot Burst Error Model
  Parameters and Error Statistics*, NTIA Technical Memorandum TM-23-565 (2023)
  (`its.ntia.gov/publications/download/TM-23-565.pdf`). It documents the model
  controls (error rate `x̄`, expected burst length `L1`, proportion of time in the
  bad state `πB`, conditional bad-state loss `1-h`) and notes Gilbert's suggested
  `h = 0.5` (50 % conditional loss in the bad state).
- **Measured GE parameter ranges for real packet-loss traces**:
  G. Haßlinger, O. Hohlfeld, *The Gilbert–Elliott Model for Packet Loss in Real Time
  Services on the Internet*, MMB 2008 (`kom.tu-darmstadt.de/papers/HH08_1034.pdf`).
  Fitted backbone traces with mean loss 0.1 % and 0.7 %; bad-state sojourns are
  geometric with mean `(1-r)/r`, and bursts persist over `1/r` packet events; the
  paper documents that burst lengths dominate the second-order statistics and that
  Gilbert's `h = 0.5` is empirically supported.
- **Loss-run / episode statistics** (mean burst lengths of a few to tens/hundreds of
  packets, with long clean sojourns) are consistent with V. Paxson, *End-to-End
  Internet Packet Dynamics*, SIGCOMM 1997, and M. Yajnik et al., *Measurement and
  Modelling of the Temporal Dependence in Packet Loss*, INFOCOM 1999 (the standard
  references for loss-run lengths; cited as the range source, not for a single
  point).

## 3. Frozen model parameters

**Loss process:** time-indexed two-state Markov chain; state G (clean) and state B
(burst); exponential sojourns; per-packet loss probabilities `p_G`, `p_B`.

**Project-grounded anchors**

| Parameter | Value | Source |
|---|---|---|
| clean-state loss `p_G` | **0.4 %** (primary); 1 % (robustness) | project D2 / scenario netem |
| burst-state loss `p_B` | **50 %** (primary; Gilbert `h=0.5`); 100 % (robustness) | NTIA TM-23-565 / Haßlinger–Hohlfeld |
| clean sojourn `T_G` | **20 s** (primary); 10 s / 60 s (robustness) | burst literature: long clean runs |
| burst sojourn `T_B` | **GOOD condition 0.15 s** (time-averaged loss 0.42 %); **BAD condition 2.0 s** (time-averaged loss 4.6 %) | burst durations: short bursts vs long degradations; the two conditions land inside the project's documented clean (0.4–1 %) and severe (2.5–7 %) ranges |
| transfer rate `R` | **640 KiB/s** (primary); 1250 / 2500 KiB/s (robustness) | D2 initial per-response pacing rate — the documented regime in which the 30 s request timeout actually triggered range retries (D2 32 MiB evidence) |
| request timeout | **30 s** | frozen downloader configuration (`fetcher.DefaultTimeout`) |
| retry limit | **3 attempts** | frozen downloader configuration (`MaxRetries`) |
| retry delay | **0.1 s** | measured D2 retry gap (`testdata/aria2_competitive_benchmark`) |
| per-request client overhead `h` | **0.088 s** | calibrated D2 simulator (`joint_ns_phase_pacing_screen/analysis/calibration.json`) |
| MSS | **1448 B** | live study link MTU 1500 (`live_joint_ns_queueing`) |

**Declared modelling abstractions (with predeclared sensitivity axes)**

- During a B interval the connection is modelled as **stalled** (zero progress) and
  recovers after the TCP RTO backoff for the burst duration (attempt sequence
  0.2, 0.6, 1.4, 3.0, 6.2, 12.6, 25.4 s). Sensitivity: B-state progress fraction
  `g ∈ {0, 0.25, 0.5}` (primary 0).
- Loss inside the G interval is modelled as a small throughput factor
  `(1 - p_G)`; the rare G losses are recovered by fast retransmit and are not
  modelled as stalls.

**Grid (frozen):** F = 512 MiB; N ∈ {1,2,4,8,16}; S ∈ {256 KiB, 1 MiB, 4 MiB, 16 MiB};
10 deterministic seeds per cell (seeded Markov traces, identical seed set across
cells); primary analysis plus the predeclared robustness sweep above.

## 4. Grounding verdict

The loss-rate anchors come from immutable project measurements; the burst/transition
dynamics come from the explicitly cited two-state Markov literature (NTIA TM-23-565,
Haßlinger–Hohlfeld, Paxson, Yajnik et al.). The mechanism is therefore **grounded
enough to screen analytically**. Any empirical/live execution additionally requires
the analytical gate to pass first (task §17–§18).
