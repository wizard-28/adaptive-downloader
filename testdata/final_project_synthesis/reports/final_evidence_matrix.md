# Final Evidence Matrix

Synthesis of completed evidence only. No new experiments were run.

Evidence-type categories (never blended in the final paper):

- **LIVE EXPERIMENTAL** — real processes against the controlled server (downloads, aria2, tc/netem)
- **TRACE REPLAY** — mm-link replay of native delivery-opportunity traces
- **DETERMINISTIC ANALYTICAL MODEL** — closed-form/event simulation with no RNG
- **SIMULATION** — training/design simulators (e.g., DQN environment)
- **DIAGNOSTIC / EXPLORATORY** — clairvoyant oracles, sensitivity analyses, feasibility checks

---

## 1. Claim inventory

| # | Claim | Evidence type | Evidence artifact | Strength | Scope | Allowed in final paper? |
|---|---|---|---|---|---|---|
| 1 | Chunk size changes per-request service cost in the controlled mechanism (larger chunk = larger service time; `D+s/R` model) | LIVE EXPERIMENTAL | `salvage_s_stage0s` (32/32 clean; pooled medians within +2.4–2.5% of `D+s/R`) | DIRECTLY ESTABLISHED | Frozen controlled mechanism, 4 arms, 512 MiB, N=4 | Yes |
| 2 | A finite-file tail exists and is predictable from the mechanism parameters | LIVE EXPERIMENTAL + DETERMINISTIC ANALYTICAL MODEL | Stage 0S tail median 34.98 s vs analytic 33.59 s; scaling Fig. 7 | DIRECTLY ESTABLISHED (realized) / ANALYTICALLY ESTABLISHED (decomposition) | Frozen mechanism; single-server | Yes |
| 3 | The static rate+RTT mechanism creates adaptive-S headroom | DETERMINISTIC ANALYTICAL MODEL | `salvage_s_scaling_analysis` (frozen strategies non-positive at every F; envelope ≤ +0.3545%) | NOT ESTABLISHED | Frozen mechanism, 64 MiB–32 GiB | Only as a negative result |
| 4 | Larger files increase relative adaptive-S gains | DETERMINISTIC ANALYTICAL MODEL | Scaling: relative gain does not increase; tail share ∝ 1/F | NOT ESTABLISHED | Frozen mechanism | Only as a negative result |
| 5 | Adaptive chunk sizing is beneficial | — | No positive causal result in any completed study | NOT ESTABLISHED | — | Only as "not supported" |
| 6 | Adaptive concurrency is beneficial | — | T1 monotone; L4 live mismatch; N screen no robust mechanism | NOT ESTABLISHED | Screened mechanisms | Only as "not supported" |
| 7 | N can have a state-dependent optimum | DETERMINISTIC ANALYTICAL MODEL | N screen `degrade`: preferred N rises with C; causal gain up to +6.66% (severe drops) | ANALYTICALLY ESTABLISHED, NOT LIVE-GROUNDED | Analytical model only; contradicted by L4 GATE E | Only with explicit "analytical, not live" qualifier |
| 8 | S has a state-dependent optimum under the screened mechanisms | DETERMINISTIC ANALYTICAL MODEL | S pivot: preferred S = 16 MiB in every stationary state/regime | NOT ESTABLISHED | Screened mechanisms | Only as a negative result |
| 9 | Changing networks create a causal adaptive opportunity | DETERMINISTIC ANALYTICAL MODEL | S pivot and N screen: causal switches zero or harmful; only fragile clairvoyant artifacts | NOT ESTABLISHED | Screened mechanisms | Only as a negative result |
| 10 | RL improves downloading | LIVE EXPERIMENTAL (negative) | `final_evaluation` DQN v2: 75.7% slower than aria2, ratio 1.757 [1.669, 1.850]; v5: 2.32× slower | NOT ESTABLISHED | Tested controlled conditions | Only as "not supported" |
| 11 | Joint N+S adaptation improves downloading | — | Joint mode never beat the deterministic baselines with a mechanism-grounded effect; no adaptive-S headroom established | NOT ESTABLISHED | Tested conditions | Only as "not supported" |
| 12 | The proposed downloader beats aria2 | LIVE EXPERIMENTAL (negative) | §22.16, §22.20, §22.26, §22.32 | NOT ESTABLISHED | Tested scenarios/configs | Only as "not supported"; no ranking |
| 13 | The proposed downloader generalizes to consumer networks | — | All evidence is synthetic/controlled single-server; no real-network deployment | NOT ESTABLISHED | — | No |
| 14 | The five-trace concurrency headroom passes the pre-registered 10% gate | TRACE REPLAY | `final_oracle_analysis`: LCB_95 = −0.0026077 < 0.10 → FAIL | DIRECTLY ESTABLISHED (gate FAIL) | Frozen five-trace population and model | Yes (as the frozen negative gate result) |
| 15 | The two-trace descriptive headroom is ~1.6% (EVDO) / ~0.03% (LTE) | TRACE REPLAY | `oracle_stage_v3` | DESCRIPTIVE ONLY | Two traces; counterfactual model | Yes, labelled descriptive |
| 16 | The aria2 baseline is fairly configured and its behavior characterized | LIVE EXPERIMENTAL | §22.25 calibration (75/75 valid; config C selected, later v2 at 16 MiB); §22.20 matched control | DIRECTLY ESTABLISHED (for the tested configs) | aria2 1.37.0, controlled server | Yes, with configuration limits |
| 17 | Granularity+parallelism matching reproduces aria2's wall-clock behavior within ~4% | LIVE EXPERIMENTAL | §22.20: aria2 2129.7 ms vs matched internal 2055.7 ms (+3.6%, n=10) | DIRECTLY ESTABLISHED (narrow) | One 4 MiB wall-clock profile | Yes, with qualifiers |
| 18 | A headroom-first methodology can close a study before policy complexity is added | METHODOLOGICAL (synthesis of all above) | The S and N closure chains in §2 | ANALYTICALLY/DESCRIPTIVELY SUPPORTED | This project's evidence | Yes, as the main methodological contribution |

---

## 2. Negative / closure chains

### S chain

1. **Static rate+RTT mechanism** (`salvage_s_protocol_v7`): per-request pacing rate fixed at request start, fixed RTT, N=4.
2. **Stage 0S mechanism realized (GO)**: 32/32 clean; service times within +2.4–2.5% of `D+s/R`; tail present — the mechanism was real, not broken.
3. **Frozen switching rules non-positive** (scaling): 16→4 max −0.0007%, 4→1 max −0.3579%, 1→256K max −1.7867% on the 64 MiB–32 GiB grid; the trigger fires at a full final burst where smaller chunks only add per-request overhead.
4. **File-size scaling does not rescue relative gain**: normalized fixed-S costs are scale-invariant (`1 + R·D/s`); the one-time tail effect is diluted (tail share ∝ 1/F); even the optimally timed single switch is ≤ 0 on the aligned grid and ≤ +0.3545% in a dense residue scan.
5. **Broader mechanism screening finds no robust causal S opportunity** (S pivot): preferred S = 16 MiB in every stationary state; causal switches zero or harmful (−61.2%…0%); the only positive opportunity is a fragile, clairvoyant, phase-dependent lock-in artifact (0–32%, 0% at a 60 s volatility period).

### N chain

1. **Direct throttle** (T1): aggregate throughput scales monotonically with C in every phase; no live concurrency reversal at equal 1 MiB granularity.
2. **Monotone concurrency behavior** (M3): the aggregate throughput envelope imposes the same ceiling on every C; no C sensitivity.
3. **Alternative analytical mechanisms** (N screen): `fair`, `fair_bg`, `direct` are monotone (N=16); `cap` is a flat plateau (oracle headroom 0%).
4. **Some analytical mechanisms can create an interior N optimum**: `degrade` (server aggregate capacity degrading with connection count) gives a state-dependent preferred N (1→4 across 0.5–32 MB/s) with causal gains up to +6.66% at severe drops.
5. **Those mechanisms fail live physical grounding**: L4's simulated over-concurrency penalty was already rejected live (GATE E, `NOT_READY_LIVE_MISMATCH`); the current controlled server has no concurrency-dependent cost; the `degrade` causal capture is severity-dependent.
6. **No robust adaptive-N mechanism**: classification PLAUSIBLE — FRAGILE, never ROBUST.

### Why these are scientifically meaningful

- They are **falsification results with mechanism-level resolution**: each step identifies *which* component (trigger timing, scaling, mechanism physics, live reproducibility) blocks adaptation, not merely that a policy underperformed.
- They are **pre-registered**: the S protocol, Stage 0S gate, five-trace gate, and N classification rule were frozen before execution.
- They **bound the design space**: any future adaptive-downloading study must first exhibit a state-dependent optimum that survives live grounding; this screen provides the negative controls and the methodology.
- They **prevent result-shopping**: the closure chain defines the conditions under which further tuning would be p-hacking rather than science.

---

## 3. Evidence-type map (strict)

| Result | Category |
|---|---|
| Stage 0S 32/32 runs, service-time medians, realized tail | LIVE EXPERIMENTAL |
| Five-trace fixed-C replays (Stage A), tau campaign (Stage B), final oracle (Stage C) | TRACE REPLAY |
| EVDO/LTE descriptive two-trace headroom (§22.74) | TRACE REPLAY (descriptive) |
| S scaling fixed-S curves, frozen-switch gains, upper envelope | DETERMINISTIC ANALYTICAL MODEL |
| S pivot stationary tables, transitions, volatility robustness | DETERMINISTIC ANALYTICAL MODEL |
| N screen stationary tables, transitions, robustness, severity | DETERMINISTIC ANALYTICAL MODEL |
| T1 live fixed-C sweep and oracle attempt | LIVE EXPERIMENTAL (oracle target = DIAGNOSTIC) |
| L3/L4 simulator sweeps | SIMULATION; L4 live probe = LIVE EXPERIMENTAL (negative) |
| aria2 baselines (§22.16, §22.19–22.20, §22.25) | LIVE EXPERIMENTAL |
| DQN head-to-heads (§22.26, §22.32) | LIVE EXPERIMENTAL (negative) |
| Clairvoyant oracles (S/N upper envelopes) | DIAGNOSTIC / EXPLORATORY — never a deployable policy |
| DQN training/validation pipelines | SIMULATION |

**Rules enforced:** the analytical +6.66% adaptive-N headroom is not empirical evidence; oracle results are not causal policy results; simulated S envelopes are not measured speedups; the aria2 runs are empirical only for their tested scenarios and configurations.
