### 22.27 DQN v3 Request-Efficiency Correction

**Status: implemented and evaluated; mixed result, reported honestly. Candidate
v3 is an EXPERIMENTAL corrected formulation, not a validated candidate for the
final claim. No aria2 comparison; no performance claim.**

- **Why v2 failed (§22.26):** v2 made one RL decision per completed request
  (**57–87 decisions**) with a 32–128 KiB chunk ladder, producing far more
  requests/round-trips than aria2's **4 × 1 MiB** requests; the per-request
  overhead dominated and DQN was ~75.7 % slower.
- **v3 design changes:** chunk **levels** extended to
  {32, 64, 128, 256, 512, 1024} KiB (increase/decrease on the ladder, so 1 MiB is
  reachable without hard-coding it); a **decision interval** `{1,4,8}` completed
  requests per RL step (transition reward = negative wall-clock seconds of the
  whole interval, preserving `(s,a,r,s',done)`); chunk **cost arises mechanically**
  from `latency + per-request overhead + transfer time` (no reward bonus); the
  training distribution expanded to file 256 KiB–16 MiB, bandwidth 256 KiB–8 MiB/s,
  latency 0–80 ms, loss 0–4 %, both throttle kinds. State spec default
  `MaxChunkSize` raised to 1 MiB (13 features unchanged; Go integration test
  covers the exported model).
- **Decision-interval experiment (simulation only):** interval 1 → val mean
  11.73 s / 22.5 req/MiB; **interval 4 → 10.12 s / 13.4 req/MiB (selected)**;
  interval 8 → 11.79 s / 17.0 req/MiB.
- **Training / selection:** seeds 0–4 (1500 episodes, interval 4); selected
  **seed 0** by validation (test never used). Candidate SHA-256
  `96361535edd3216eabebf2d1fb72791b3551517bbb3a7bbaf44b7a8ee8eb492b`.
- **Held-out simulator (mean completion s / DQN req per MiB; not ranked):**
  DQN **12.70 s / 13.8 req·MiB⁻¹** vs fixed 25.65/32.0, concurrency-adaptive
  24.29/32.0, chunk-size-adaptive 24.60/29.5, joint-adaptive 23.76/30.6. Per
  condition DQN uses few requests where it pays (combined 3.6, latency 4.5,
  latency+loss 4.5 req/MiB) and is ~2–3× faster than the scripted baselines in
  most conditions. **Note:** the scripted baselines still show 29–32 req/MiB
  because their relative-throughput trigger rarely fires in the analytic
  simulator — a baseline-fidelity caveat at the chunk dimension.
- **Ablation (decision interval):** interval 1 → 13.68 s / 21.0 req/MiB;
  interval 8 → 15.71 s / 18.0 req/MiB; selected interval 4 → 12.70 s / 13.8.
- **Live transfer v3 vs v2 (25 runs each; real controlled server; v3 run with
  `-max-chunk 1048576`):**
  - **Improved:** the wall-clock throttle attempts fell **75 → 37** via chunk
    growth to **~449 KiB** (v2 ~102 KiB); chunk now reaches ~160–449 KiB across
    scenarios; fragmentation is substantially reduced.
  - **Regressed:** v3 stays at **concurrency 1** in every scenario (v2 used
    1.4–4.0); completion time is worse under latency (3723 vs 1993 ms) and
    combined (5997 vs 2573 ms). The simulator's per-request-rate model made large
    chunks strongly dominant, so the learned policy under-used concurrency.
  - **Net:** **mixed** — request efficiency improved, concurrency adaptation
    regressed; not a clean success on both axes. Per the task's failure
    condition this is documented rather than presented as a win.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v3/{simulator_spec_v3.json,
  train_config_v3.json, decision_interval_results.json, seed_results_v3.json,
  validation_results_v3.json, heldout_test_results_v3.json, ablations_v3.json,
  live_transfer_results_v3.json, final_dqn_candidate_v3.json,
  final_dqn_candidate_v3.sha256, sim_to_real_v3.json, report.json, report.txt}`.
  Code: `python/train_validate_v3.py`, `python/rl/sim_env_v3.py`; the DQN Go
  package (state/model/live) and `cmd/dqn-transfer` (now with `-max-chunk`) are
  reused. Candidate v2 is untouched.
- **Limitations:** test set not touched by tuning; the real server still models
  no chunk-size tradeoff (so live chunk behavior is not "optimization"); the v3
  simulator over-favoured large chunks, costing concurrency adaptation; live n=5;
  no aria2 comparison; **no performance claim.**

### 22.28 DQN v4 — Adaptive Concurrency Formulation

**Status: implemented and evaluated with a clean, positive transfer result.
Candidate v4 frozen. No aria2 comparison; no performance claim.** This is the
final RL formulation and is deliberately **concurrency-only** — it is **no longer
a joint chunk/concurrency RL claim**.

- **Why v2/v3 failed:** v2 fragmented (57–87 decisions, 32–128 KiB chunks; ~75.7 %
  slower than aria2, §22.26). v3 fixed fragmentation (chunk up to ~449 KiB) but the
  simulator rewarded larger chunks while the real server models **no chunk-size
  tradeoff**, so the learned chunk behavior was invalid and concurrency adaptation
  regressed (§22.27).
- **v4 design:** **chunk size is FIXED at 1 MiB** (fixed for every policy; matched
  to aria2's granularity), and the **only learned variable is concurrency (1–4)**.
  Action space is 3 actions `{none, increase-concurrency, decrease-concurrency}`
  (`ConcurrencyActionNames`/`ConcurrencyIndexToAction`); reward is unchanged
  (`r_t = −elapsed wall-clock seconds`, gamma 1); decision interval ∈ {2,4,8}
  requests per RL step. The 13-feature state is unchanged (chunk-size becomes a
  constant input); state spec `MaxChunkSize` 1 MiB.
- **Simulator (`python/rl/sim_env_v4.py`):** per-request time `latency + chunk/rate`
  (independent of concurrency); a wave of `concurrency` requests runs **in
  parallel** (wave time = slowest request), so aggregate throughput scales with
  concurrency; an optional `link_bandwidth` shared cap makes extra concurrency stop
  helping. Verified: without a cap C=1→2.16 s, C=2→1.08 s, C=4→0.54 s; with a
  2 MiB/s cap C=1..4 ≈ 4.0–4.2 s. Both throttles supported. Calibrated to the
  frozen server (`1 MiB@512 KiB/s ≈ 2.05 s`).
- **Decision-interval experiment:** interval 2 → 3.62 s / 0.42 churn·MiB⁻¹;
  **interval 4 → 2.89 s / 0.09**; interval 8 → **2.27 s / 0.06 (selected)**.
- **Training / selection:** seeds 0–4 (1500 episodes); selected **seed 1** by
  validation only. Candidate SHA-256
  `227d9845096b5d1382c58cc9080e5907fc0b81cba7dc04546c5eaa5b3ecf4ad5`;
  same-seed retrain is byte-identical (deterministic).
- **Held-out simulator (mean completion s, all policies at 1 req/MiB):** DQN
  **2.96 s (avg concurrency 2.97)** vs fixed 4.10 (static C≈2.5) and
  concurrency-adaptive 4.10. Caveat: the scripted baselines are inert in the
  analytic simulator (their throughput trigger rarely fires), so the comparison is
  mostly against a static concurrency. Ablations: DQN 2.96 < random 4.87 <
  shuffled 4.10; corrupted-state 2.92 ≈ DQN, indicating the learned advantage is
  largely "hold concurrency ≈3" rather than strongly state-conditional.
- **Request efficiency:** **1.0 request/MiB** for every policy (1 MiB chunks) — the
  v2/v3 fragmentation is eliminated by construction.
- **Live transfer (25 runs each, real controlled server, chunk pinned 1 MiB for
  v4):**

  | scenario | v2 dur/att/C | v3 dur/att/C/chunk | **v4 dur/att/C/chunk** |
  |---|---|---|---|
  | baseline | 1171/109/1.0 | 1160/87/1.0/32768 | **841/4/3.0/1048576** |
  | throttle-wallclock | 1499/75/1.4 | 1918/37/1.0/458752 | **2634/4/3.0/1048576** |
  | latency | 1993/66/1.6 | 3723/76/1.0/163840 | **1009/4/3.0/1048576** |
  | loss | 1227/71/2.0 | 1485/81/1.0/131072 | **845/4/3.0/1048576** |
  | combined | 2573/66/4.0 | 5997/75/1.0/91750 | **3355/4/3.2/1048576** |

  **25/25 valid.** v4 makes **exactly 4 requests of 1 MiB** (aria2's granularity)
  and holds **concurrency 3** (occasionally 4) — no fragmentation, no chunk drift,
  and faster than v3 everywhere and than v2 in baseline/latency/loss.
- **Success criteria (§18):** met — request granularity comparable to aria2;
  stable concurrency control; no artificial chunk drift; sensible latency/loss
  behavior; improved sim-to-real agreement over v3. The throttle-wallclock run is
  slower than v2 (2634 vs 1499 ms) because v4 pins 1 MiB chunks and cannot adapt
  granularity — expected and documented.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v4/{simulator_spec_v4.json,
  train_config_v4.json, decision_interval_results_v4.json, seed_results_v4.json,
  validation_results_v4.json, heldout_test_results_v4.json, ablations_v4.json,
  live_transfer_results_v4.json, final_dqn_candidate_v4.json,
  final_dqn_candidate_v4.sha256, sim_to_real_v4.json, report.json, report.txt}`.
  Code: `python/train_validate_v4.py`, `python/rl/sim_env_v4.py`; Go
  `internal/rl/dqn/{policy,live}.go` (concurrency-only action space) and
  `cmd/dqn-transfer` (`-fixed-chunk`). Candidates v1/v2/v3 untouched.
- **Limitations:** synthetic simulator; the real server models no chunk-size
  tradeoff (now irrelevant because chunk is pinned); live n=5; approximate live
  observability; the simulator baselines are weak; concurrency is held near 3
  rather than strongly condition-dependent in short runs; **no aria2 comparison
  and no performance claim yet.**

### 22.29 DQN Candidate v5 (concurrency-only, state spec v2) — simulator/state audit and fixes

**Status: experimental, NOT frozen. No v5 training/validation/live-transfer run
exists; no v5 model artifact has been produced.** This subsection records the
first full audit of the v5 simulator/state/live contract and the correctness
fixes it produced. The v5 formulation itself (16 MiB default horizon, fixed
1 MiB chunk, concurrency 1–4, actions `none / increase-concurrency /
decrease-concurrency`, Double DQN, state spec version 2 with 13 temporal
features, dynamic within-episode condition transitions, decision interval
candidates {1,2,4}) is unchanged.

- **Files:** `python/rl/sim_env_v5.py`, `python/rl/state_v5.py`,
  `internal/rl/dqn/state_v5.go`, `internal/rl/dqn/live.go`; tests
  `python/tests/test_sim_env_v5.py`, `python/tests/test_state_v5.py`, and the v5
  tests in `internal/rl/dqn/state_v5_test.go`.
- **No v5 artifacts yet:** there is no `testdata/diagnostics/dqn_validation_v5/`,
  no `simulator_spec_v5.json`, and no `static_concurrency_sweep_v5.json`; the
  simulator docstring references to the latter were corrected to state that the
  sweep/spec are still to be generated (no measured claim).
- **Bugs found and fixed (training/live contract only; frozen v1–v4 artifacts
  untouched):**
  1. **Decision interval suppressed concurrency.** The old step ran
     `min(concurrency, decision_interval)` requests as a fresh serial wave and
     charged `max(service_time)` per wave, so with `decision_interval <
     concurrency` extra workers were idle and strictly harmful, while the state
     feature `aggregate_throughput` still advertised `C*n/svc`. The step now
     accounts elapsed time as continuous-worker pipeline time
     `sum(service_times)/C` (using at most the remaining requests when the file
     finishes inside the step), so concurrency changes the aggregate rate for
     every candidate interval. Requests per decision remain
     `min(decision_interval, remaining_reqs)`.
  2. **Wall-clock transitions were not applied mid-step.** Phase lookup used
     `self.elapsed`, advanced only after the whole step, despite a per-wave
     re-evaluation comment. Phase selection now uses the elapsed time at each
     request's start (`self.elapsed + accrued`), and `transitions` counts
     boundaries crossed.
  3. **`request_duration_slope` was identically zero.** `_obs` returned
     `self.prev_dur` for both `request_duration_s` and `prev_duration_s`. The
     simulator now tracks `last_dur` (most recent request service time) and
     `prev_dur` (previous step) separately, matching Go `ObservationV5`.
  4. **Window counters were per decision interval, not per attempt.**
     `window_attempts` counted intervals and the failure/retry/completion
     windows stored 0/1 per interval. They now push one sample per request,
     matching the feature definitions and the Go live contract.
  5. **Live progress was fabricated.** `LivePolicy.doneBytes` used
     `SuccessfulRequests * BytesTransferred` (successes × last request bytes),
     which is wrong on failures, partial final ranges, and varying sizes, and it
     feeds `remaining_bytes_frac`. Live progress is now the accumulated
     `BytesTransferred` of successful attempts (one decision per completed
     attempt; a failed attempt never advances it).
  6. **Python export could not produce a Go-valid v5 model.**
     `python/rl/state_v5.py` omitted `min_chunk_size`/`max_chunk_size`, which Go
     `StateSpec.Validate` requires, and `python/rl/model.py` always exported the
     v1 spec/feature names. The v5 spec now carries 1 MiB chunk bounds and
     `export_model` selects spec/feature names from the environment config
     (`SimEnvV5` records its resolved spec on the config).
- **Verified consistent (unchanged):** feature order, normalization, and spec
  version match Go `FeatureNamesV5`/`FeaturesV5`/`DefaultStateSpecV5`; the
  concurrency-only action space is 3 actions mapped by
  `ConcurrencyIndexToAction`; reward remains `-elapsed` with gamma 1; the
  simulator is deterministic for a fixed seed. A shared exact feature-vector
  contract test pins Python and Go together.
- **Tests:** `TestV5FeatureParityContractExact`,
  `TestV5LivePolicyProgressFromSuccessBytes` (Go) and `test_state_v5.py` /
  `test_sim_env_v5.py` (Python: interval/no-suppression, pipeline accounting,
  requests-per-step, mid-step transition, per-attempt windows, duration slope,
  determinism/truncation, export contract).
- **Validation:** `go test ./...`, `go test -race ./...`,
  `go test -count=5 ./internal/rl/... ./internal/downloader/...
  ./internal/experiment/...`, `go vet ./...`, `gofmt -l .`, `go build ./...`,
  and `python3 -m pytest python/tests -q` (29 passed) all pass. Frozen candidates
  v1–v4 and the whole `testdata/` manifest (155 files) are byte-identical.
- **Next step (now done):** the v5 training/validation pipeline is implemented
  and run in §22.30 (multi-seed training, validation-only selection, held-out
  test vs fixed C, behavior diagnostics, pre-specified constant/ramp gate, one
  exported candidate). Live transfer and any comparison remain separate tasks.
  No v5 performance claim exists.

### 22.30 DQN Candidate v5 training + held-out validation (simulator only)

**Status: pipeline complete; ONE experimental Candidate v5 model exported and
audited. NOT a performance claim; no live transfer, no aria2 comparison.** This
stage implements the v5 training/validation pipeline on top of the audited
simulator/state (§22.29). The model is behaviorally adaptive (passes the
pre-specified constant-policy gate) but is **generally slower than the best
fixed concurrency** on the held-out conditions, so it is **not** ready for a
head-to-head claim.

- **Code:** `python/train_validate_v5.py` (new). Reuses the existing Double DQN
  (`python/rl/dqn.py`), export (`python/rl/model.py`), and the v5
  simulator/state; adds additive per-episode `returns`/`episode_steps` to the
  training report. `internal/rl/dqn/v5_candidate_smoke_test.go` (new) loads and
  validates the exported artifact and runs deterministic v5 greedy inference in
  Go (skips when absent; override with `DQN_V5_CANDIDATE`).
  `cmd/dqn-transfer` now dispatches on `StateSpec.Version` so a v5 model uses
  `NewLiveConcurrencyPolicyV5`; previously a 3-action v5 model would silently
  use the v1 observation layout (both accept 13 features), which is now covered
  by `TestNewLivePolicyForModelDispatchesV5`.
- **Reproduce:** `python3 python/train_validate_v5.py` (defaults: 1500
  episodes/seed, seeds 0–4, intervals 1,2,4, out
  `testdata/diagnostics/dqn_validation_v5`). Smoke:
  `python3 python/train_validate_v5.py --out testdata/diagnostics/dqn_validation_v5_smoke
  --episodes 30 --seeds 0 --intervals 2 --val-seeds 100 --test-seeds 200
  --sweep-seeds 300`.
- **Training configuration:** state spec version 2, fixed 1 MiB chunk,
  concurrency 1–4, 3 actions (`none`/`increase-concurrency`/
  `decrease-concurrency`), v5 randomized ranges (rate 256 KiB–8 MiB/s, link
  multiple 0–4× rate, latency 0–80 ms, loss 0–4 %, congestion 0–0.06, file
  4–32 MiB, 1–3 phases, phase durations 0.3–2.0 s). Training is not tuned on
  the live scenarios.
- **Seed separation (deterministic):** training 0–4; validation 100–102; test
  200–203; static sweep 300–302; randomized eval 400–404. The test seeds and
  conditions are never used for selection.
- **Selection:** decision interval chosen on VALIDATION only — interval 4
  (val mean 2.316 s) vs 1 (2.760 s) and 2 (2.850 s); seed 0 selected on
  validation mean (2.293 s), tie-break lowest seed.
- **Static concurrency sweep (prerequisite; simulator only):**
  `static_concurrency_sweep_v5.json`/`.txt` — best fixed C differs across the 8
  regimes: C=1 (`shared-cap-hard`), C=2 (`combined-mild`), C=3
  (`shared-cap-moderate`), C=4 (stable/low-bandwidth/latency/throttle), so
  distinct best C = {1,2,3,4}. Adaptive concurrency is therefore meaningful in
  the simulator; no simulator change was made to obtain this.
- **Held-out TEST (8 conditions × 4 seeds; mean completion ms; descriptive):**

  | condition | DQN | C=1 | C=2 | C=3 | C=4 | best | DQN−best |
  |---|---|---|---|---|---|---|---|
  | unseen-low-bandwidth | 7607.2 | 21333.3 | 10706.7 | 7164.4 | **5393.3** | C4 | +2213.9 |
  | unseen-high-bandwidth | 631.4 | 1333.3 | 826.7 | 657.8 | **573.3** | C4 | +58.1 |
  | unseen-latency | 1093.3 | 2960.0 | 1560.0 | 1093.3 | **860.0** | C4 | +233.3 |
  | unseen-loss | 936.7 | 2312.5 | 1236.2 | 877.5 | **698.1** | C4 | +238.5 |
  | unseen-latency+loss | 1547.8 | 3900.0 | 2084.2 | 1442.8 | **1122.1** | C4 | +425.7 |
  | unseen-throttle-up | 2351.7 | 5500.0 | 2910.0 | **2046.7** | 2590.0 | C3 | +305.0 |
  | unseen-throttle-down | 2837.5 | 22500.0 | 7610.0 | 3913.3 | **2065.0** | C4 | +772.5 |
  | unseen-three-phase | 4089.2 | 20396.7 | 10191.7 | 5456.7 | **4089.2** | C4 | +0.0 |

  DQN matches/beats the best fixed C on **1/8** conditions (a tie). Descriptive
  only (means; n=4 per condition).
- **Behavior diagnostics:** action fraction none/increase/decrease =
  0.152/0.707/0.141; concurrency time fractions C2 0.213 / C3 0.334 / C4 0.454
  (C1 0.000); mean effective changes/episode 2.00; constant-episode fraction
  0.062; ramp-to-max fraction 0.219; changes before/after first transition =
  51/13 (6 transition reactions within 3 decisions); dominant C by condition is
  C=3 in 6/8 and C=4 in 2/8 (three-phase, throttle-down) → **weakly
  condition-dependent**; the policy never settles on C=1/C=2 as a dominant
  value. Randomized-distribution eval (seeds 400–404): mean 3.254 s, constant
  fraction 0.20, 3.6 changes/episode, dominant C=4.
- **NOT-READY gate (pre-specified):** NOT READY if constant-episode fraction
  ≥ 0.9, or one C's time fraction ≥ 0.9, or ramp-to-max fraction ≥ 0.9, or zero
  effective changes. Result 0.062 / 0.454 / 0.219 / 64 changes →
  **READY_FOR_NEXT_STAGE** (behavioral gate only; not a performance claim).
- **Candidate v5 (experimental, simulator-selected):** SHA-256
  `3a927a58baa7f9b5f150827ba0bd81defc95adb25e5328eb037f5edf9925da75`; state
  spec v2, 13 features, 3 actions. An independent run
  (`--seeds 0 --intervals 4`) reproduces the byte-identical hash. The Go smoke
  test loads it, validates the contract and SHA, and infers deterministically.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v5/` —
  `final_dqn_candidate_v5.json`/`.sha256`, `train_config_v5.json`,
  `simulator_spec_v5.json`, `seed_results_v5.json`,
  `validation_results_v5.json`, `decision_interval_results_v5.json`,
  `training_episodes_v5.json`, `heldout_test_results_v5.json`,
  `diagnostics_v5.json`, `static_concurrency_sweep_v5.json`/`.txt`,
  `report.json`, `report.txt`; smoke copy in `dqn_validation_v5_smoke/`.
- **Caveats:** simulator only; no real-downloader transfer and no aria2
  comparison; the model underperforms the best fixed C on 7/8 held-out
  conditions; adaptation is weak (dominant C usually 3/4, never 1/2); the v5
  reward/credit and live-observability caveats from §22.22–§22.29 remain; means
  are descriptive (n=4).
- **Next step (now done):** live transfer of Candidate v5 on the real
  controlled downloader is completed in §22.31. A pre-specified head-to-head
  against the frozen aria2 v2 baseline remains separately scoped and is NOT run.

### 22.31 Candidate v5 live transfer validation (controlled server)

**Status: completed. 125/125 runs valid; the three pre-specified gates pass;
NO aria2 comparison, NO performance/superiority claim, NO real-network
generalization.** This validates whether the simulator-trained Candidate v5
transfers its behavior to the real controlled downloader/server (16 MiB file,
1 MiB fixed chunk, wall-clock conditions).

- **Harness:** `cmd/dqn-live-v5/` (new, experiment-only; it does not modify the
  downloader, policy runner, controllers, model, or simulator). It reuses the
  existing `downloader`, `experiment/server` (request log + wall-clock
  `RateSchedule`), `experiment/network` (tc/netem on `lo`, per-run apply/clear),
  `metrics`, and `dqn.LivePolicy` via `NewLiveConcurrencyPolicyV5`.
  Authoritative concurrency is derived from the controller target reported at
  each decision: applied-after(action k) = `ConcurrencyBefore` of decision k+1
  (or the final controller target); the model's own pre-application
  `Decision.ConcurrencyAfter` is deliberately NOT used.
- **Matrix:** 5 scenarios (`baseline`, `server-throttle-wallclock`,
  `latency` 20 ms, `loss` 1 %, `combined` 1 MiB/s + 20 ms + 1 %) × 5 policies
  (`fixed-1..4`, `v5`) × 5 reps = **125 runs**, deterministic run order
  (`-order-seed 20260925`), fresh server/output/downloader/netem per run. The
  rate transition is wall-clock (512 KiB/s → 4 MiB/s after 1 s), never
  request-count based. All policies share server, rates, netem, retries (3),
  timeout (30 s), and 1 MiB granularity.
- **Reproduce:** `go run ./cmd/dqn-live-v5 -out
  testdata/diagnostics/dqn_validation_v5_live -sudo`.
- **Validity (GATE A):** 125/125 valid, 0 invalid, 0 network-unavailable (all
  netem runs `applied`; qdisc clean afterwards). Per-run checks: exact 16 MiB
  size and SHA-256 content match; successful ranges tile `[0,16 MiB)` (no
  gaps/overlaps); `sum(success bytes) == 16 MiB`; model-reported remaining bytes
  match recorded successful bytes per attempt; concurrency stays in [1,4];
  requested actions stay in {none, increase, decrease}; no failed attempt
  contributes completed bytes; retries 0.
- **Observed request behavior:** every run issued exactly 16 × 1 MiB range
  requests (no remainder, 0 retries). In the transition scenario the server log
  shows exactly 1 request paced at 512 KiB/s and 15 at 4 MiB/s per run,
  confirming the wall-clock transition was observed, not inferred.
- **Fixed-C results (mean completion ms; descriptive, not a ranking):**

  | scenario | C=1 | C=2 | C=3 | C=4 | v5 |
  |---|---|---|---|---|---|
  | baseline | 4445.6 | 2262.2 | 1731.3 | **1170.0** | 1719.9 |
  | server-throttle-wallclock | 6248.0 | 4041.3 | 3499.1 | **2947.2** | 3494.9 |
  | latency | 5202.8 | 2695.6 | 1985.8 | **1391.6** | 2039.9 |
  | loss | 4614.4 | 2268.9 | 1724.6 | **1244.7** | 1735.2 |
  | combined | 17451.0 | 8994.3 | 6558.1 | **4486.0** | 6557.7 |

  v5 tracks fixed-C3 closely (within ~3 % in four scenarios) and is slower than
  fixed-C4 everywhere; SDs are small (v5 12–40 ms). The real server paces each
  response independently (no shared bottleneck), so C=4 is the per-scenario
  optimum here; this harness therefore cannot distinguish shared-cap regimes
  that the simulator models.
- **v5 actual concurrency behavior:** non-constant everywhere (0 constant
  runs), no ramp-to-max-only run, no monotone-decrease run; mean actual
  changes/run 5.2–11.2 (increases ≈ 60 %, decreases ≈ 40 %), i.e. a noisy ramp
  to C3–C4 followed by ±1 oscillation from the one-decision-per-completion
  credit lag. Final C is 3 in baseline/latency/throttle (5/5 each), 3 in loss
  (4/5; one run C2), and 4 in combined (4/5). Dominant actual concurrency by
  time is C4 in four scenarios (fraction 0.59–0.67) and C1 in the throttle
  scenario (0.59, because the single slow first request holds C1 for the first
  ~2.1 s).
- **Transition reaction (GATE C):** all 5 valid v5 transition runs establish the
  concurrency in effect at the transition (C1), the first decision after it
  (~2067 ms; requested `increase-concurrency`), the applied change (C1→C2), and
  the reaction latency (median 1067 ms). The latency floor is the in-flight
  1 MiB @ 512 KiB/s request duration minus the 1 s transition, i.e. the policy
  reacts at the first completion after the transition. Post-transition mean
  concurrency 2.49.
- **Gates (pre-specified in the harness before the campaign):**
  - **GATE A (technical validity): PASS** — 125/125 valid, 0 exclusions.
  - **GATE B (genuine adaptation): PASS by the pre-specified rule** —
    constant fraction 0.00, ramp-to-max 0.00, monotone-decrease 0.00, worst
    dominant time fraction 0.674, 2 distinct dominant actual concurrency
    values. **Qualitative Task-5 reading: the transfer is adaptive but shallow.**
    The policy ramps to C3–C4 and oscillates in every scenario; it does not
    hold low concurrency anywhere, and the between-scenario difference is
    driven by the transition scenario's slow first request (time-weighted C1),
    not by deliberate condition discrimination. The two distinct dominant
    values do not by themselves demonstrate condition-dependent adaptation.
  - **GATE C (reaction observability): PASS** — complete reaction logs for 5/5
    transition runs; all 5 reacted at the first post-transition completion.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v5_live/` —
  `live_manifest_v5.json` (locked matrix, model hash, gate definitions, run
  order), `live_runs_v5.json` (+ `.sha256`; raw per-run decisions with
  authoritative before/after concurrency and timestamps, metrics, server
  request log with per-request rate and start/end timestamps, validation),
  `live_aggregate_v5.json` (per-scenario/policy stats + paired per-rep
  observations), `live_adaptation_v5.json` (trajectory classes, dominant
  concurrency, reactions), `live_gates_v5.json`, `live_validation_v5.json`,
  `live_report_v5.txt`. Smoke copy in `dqn_validation_v5_live_smoke/`.
- **Frozen artifacts:** v1–v4 candidates and the pre-existing `testdata/`
  manifest (155 files) are byte-identical; aria2 v2 calibration untouched.
- **Next step (now done):** the pre-specified head-to-head of Candidate v5
  against the frozen aria2 v2 baseline is completed in §22.32. Passing these
  gates did not establish superiority over aria2.

### 22.32 Final head-to-head: DQN Candidate v5 vs frozen aria2 baseline v2

**Status: completed. The pre-specified outperformance criteria were NOT met;
v5 was slower than aria2-F in every tested condition. Reported as measured; no
tuning and no post-hoc scenario changes.** This is the final evaluation of the
two frozen configurations.

- **Frozen configurations:** DQN Candidate v5, SHA-256 `3a927a58…` (state spec
  v2, 13 features, 3 actions, fixed 1 MiB chunk, concurrency 1–4); FINAL ARIA2
  BASELINE CONFIG v2 / candidate F
  (`testdata/diagnostics/aria2_calibration_v2/selected_baseline_v2.json`,
  SHA-256 `b7249817…`; aria2 1.37.0, `--max-connection-per-server=16
  --split=16 --min-split-size=1048576`). Both hashes were verified before the
  campaign; neither was modified.
- **Matrix (reused methodology):** the established U1–U8 wall-clock unstable
  profiles from §22.26 at the frozen **16 MiB** v5/aria2-v2 scale (the only
  deliberate change vs §22.26's 4 MiB, applied identically to both systems);
  30 matched pairs per condition = **240 pairs / 480 downloads**; deterministic
  per-pair order (seed `20260925`); fresh server/output/netem per run; per-run
  netem apply/clear; wall-clock `RateSchedule` transitions only.
- **Harness:** new `cmd/final-evaluation-v5/` (experiment-only) reuses the
  §22.26 pair structure/order/conditions and the validated v5 live path
  (`NewLiveConcurrencyPolicyV5`, 1 MiB fixed chunk, C 1–4), and runs aria2-F from
  the hash-verified frozen command (parsed to argv; candidate-F flags asserted,
  version asserted). Statistics reuse
  `python/analyze_final_evaluation.py` unchanged (matched pairs; effect =
  log(v5/aria2); one-sided paired permutation test; 95 % t CI; Holm across 8
  conditions); the analyzer's established `dqn` key denotes Candidate v5.
  `python/report_final_evaluation_v5.py` writes the human-readable report from
  the frozen artifacts.
- **Reproduce:** `go run ./cmd/final-evaluation-v5 -out
  testdata/final_evaluation_v5 -sudo`; then
  `python3 python/analyze_final_evaluation.py --dir testdata/final_evaluation_v5`
  and `python3 python/report_final_evaluation_v5.py --dir testdata/final_evaluation_v5`.
- **Validity:** **480/480 runs valid, 0 invalid, 240/240 valid pairs, 0
  exclusions.** Every run: no error, exactly 16 MiB output with SHA-256 equal to
  the served content, v5 range-tiling/success-bytes/progress/action/concurrency
  checks pass, and every requested netem condition applied; qdisc clean.
- **Per-condition matched-pair results (mean ms; ratio = geometric v5/aria2):**

  | cond | v5 mean | aria2 mean | ratio | ratio 95 % CI | p_holm |
  |---|---|---|---|---|---|
  | U1 | 3262.9 | 1485.5 | 2.199 | [2.164, 2.234] | 1 |
  | U2 | 2336.6 | 785.2 | 2.977 | [2.943, 3.012] | 1 |
  | U3 | 1982.8 | 754.2 | 2.799 | [2.463, 3.181] | 1 |
  | U4 | 3922.6 | 1907.2 | 2.086 | [1.943, 2.240] | 1 |
  | U5 | 4221.9 | 2865.2 | 1.493 | [1.397, 1.594] | 1 |
  | U6 | 3004.9 | 1118.7 | 2.686 | [2.667, 2.705] | 1 |
  | U7 | 2288.9 | 891.2 | 2.808 | [2.411, 3.270] | 1 |
  | U8 | 3432.5 | 1806.7 | 1.962 | [1.787, 2.155] | 1 |

- **Pooled:** v5 mean 3056.6 ms vs aria2 1451.7 ms; geometric ratio **2.3217**
  (95 % CI [2.2269, 2.4206]); relative reduction −132.2 % (negative = v5
  slower); one-sided paired permutation p = 1.0; v5 faster in **1/240 pairs
  (0.4 %)** and **0/8 conditions**. All four pre-specified claim criteria false
  → `claim_supported = false`.
- **v5 live behavior (authoritative controller state):** max concurrency 4 in
  every run; time-weighted mean 2.702; mean 7.4 effective changes/run; final
  concurrency C3 180/240, C4 43, C2 17; constant-run fraction 0.000; ramp-to-max
  fraction 0.021. Transition reaction latencies (transition → first effective
  change): U1 566 ms, U2 152 ms, U6 733 ms, U7 665 ms, U8 316 ms.
- **aria2-F observed behavior:** max in-flight mean 15.82 / max 16 (204/240 runs
  reached 16); 15–16 × 1 MiB range requests per run plus 1 initial non-range
  probe; 0 aria2-reported retries; 245 server-recorded failed requests across
  480 runs (the aborted initial probe); outputs SHA-correct in all runs.
- **STRUCTURAL ASYMMETRY (mandatory):** v5 is capped at **4** concurrent
  requests while aria2-F uses up to **16** configured connections. Candidate F
  was selected by the v2 calibration precisely because
  `--min-split-size=1048576` requires ≥16 connections to obtain the intended
  ~16 × 1 MiB split at 16 MiB. The controlled server paces each response
  independently, so aria2-F can exploit up to 16 independently paced responses
  while v5 can exploit at most 4. This is a comparison of two complete frozen
  configurations, **not** a capability-matched comparison; the 2.32 ratio must
  not be read as an algorithmic effect isolated from the asymmetry.
- **Interpretation (evidence-supported):** under the tested controlled
  conditions and methodology, **v5 did not outperform aria2-F and was slower in
  all 8 conditions**; no universal, consumer-network, optimality, convergence,
  or real-network generalization claim is made. The negative §22.26 result
  (Candidate v2 vs aria2 v1) and the shallow v5 adaptation finding (§22.31)
  remain unchanged and are not reinterpreted.
- **Artifacts:** `testdata/final_evaluation_v5/` — `final_test_matrix_v5.json`
  (+ `.sha256`), `execution_schedule.json`, `raw_results.json` (analyzer input,
  valid pairs), `raw_results_all.json` (all 480 runs + telemetry),
  `request_traces.json`, `validity_report.json`, `asymmetry_report.json`,
  `manifest_v5.json`, `statistical_analysis.json`, `claim_assessment.json`,
  `summary.txt`, `per_condition_table.txt`, `report.txt`, `run.log`,
  `artifact_hashes.json`. The §22.26 `testdata/final_evaluation/` directory and
  all other frozen artifacts are untouched.
- **Limitations:** controlled synthetic server; single 16 MiB file; the 8
  U-profiles; 30 pairs per condition; the 4-vs-16 concurrency asymmetry;
  aria2's initial non-range probe; no real-network or IDM comparison; no
  fairness signal.

### 22.33 Candidate v6 pre-registration (written BEFORE training)

**Status: pre-registered; the pipeline has since run (see §22.34).** This section fixes the
conditions, baselines, metrics and gates before any v6 training or run, so they
cannot be tuned after seeing outcomes.

**v5 failure audit (evidence from §22.30–§22.32; v5 is NOT modified):**
- v5 concurrency was capped at **4**; frozen aria2-F used up to **16**
  connections.
- the live controlled server paces **each response independently** with no
  shared bottleneck, so 16 independently paced responses were strongly rewarded
  and fixed-C4 was fastest in every tested condition; there was no interior
  concurrency tradeoff for the policy to learn.
- v5 mostly **ramped to C3–C4 and oscillated** (time-weighted mean 2.70; mean
  7.4 changes/run; constant 0.000; ramp-to-max 0.021) rather than reacting
  discriminatively to regime changes; the only between-condition difference was
  timing-driven (the wall-clock transition scenario's slow first request).
- v5 trained with simulator `decision_interval=4` while the live runner decided
  once per completed attempt — a **cadence mismatch** (v5's timing mechanics
  were corrected, but the cadence was not aligned).
- v5's feature layout/normalization/parity contract is otherwise validated and
  is reused as the basis for v6 (with a new spec version for the wider range).

**v6 formulation (fixed before implementation):**
- 16 MiB evaluation horizon; 1 MiB fixed chunk; **concurrency 1–16**.
- actions `0 = none`, `1 = increase-concurrency`, `2 = decrease-concurrency`
  (±1, no new action kinds, no target-setting actions).
- Double DQN; temporal 13-feature state, **state spec version 3**, same
  documented feature layout as v5 but concurrency normalized over [1,16].
- reward = **negative wall-clock elapsed seconds** of the decision interval
  (`r_t = -elapsed_t`, gamma 1); no throughput/latency/loss bonuses, no
  condition labels, no hard-coded "latency ⇒ decrease" mapping; the policy sees
  only transfer outcomes.
- **decision cadence = one decision per completed request** in both simulator
  and live runner (`decision_interval = 1`). The live runner already decides
  once per completed attempt; the simulator is fixed to the same cadence so the
  two are identical by construction. Interval candidates {1,2,4} are therefore
  not evaluated for v6: the live runner's cadence pins the contract to 1.

**Environment (genuine shared bottleneck, not independent pacing):**
- per-phase shared capacity `B(t)` (the simulator's `link_bandwidth`): each
  active transfer receives `min(per-response rate, B/C)`, so increasing C has
  diminishing/negative value when B binds; per-connection overhead and
  concurrency-dependent loss/congestion add further cost.
- server-side rate schedules model throttling independent of request count.
- live experiments reuse the existing controlled server and tc/netem
  (bandwidth/latency/loss; mid-run re-apply at phase boundaries) rather than a
  separate framework. These are controlled unstable-network profiles, not a
  claim about all mobile networks.

**Pre-registered trajectories (3 wall-clock phases; training durations
randomized in a fixed 1.5–3.0 s range per phase; held-out durations fixed and
documented per condition):**
- **A** bandwidth collapse/recovery: B high → B low → B high.
- **B** latency spike/recovery: latency low → high → low.
- **C** burst-loss episode: loss low → elevated → low.
- **D** server throttle/release: server rate high → severe throttle → recovered.
- **E** combined deterioration/partial recovery: good → lower B + higher
  latency + loss → partial recovery.

**Gates (fixed now; not changed after results):**
- **GATE A — environment validity:** the static C=1..16 sweep over
  pre-registered regimes shows multiple meaningful concurrency optima (not all
  C16). If it fails, v6 training does not start.
- **GATE B — training validity:** v6 beats the best fixed-C baseline on
  held-out dynamic simulator conditions; selection uses validation data only.
- **GATE C — condition-dependent behavior:** v6 reacts differently to
  materially different trajectories and is neither C16/ramp-locked nor random
  adjacent oscillation.
- **GATE D — live transfer:** the condition-dependent behavior remains
  observable on the controlled live server/network.
Failure of any gate is reported as **NOT READY** with a diagnosis. The final
aria2-F comparison is out of scope until GATE A–D pass.

### 22.34 Candidate v6: training, held-out validation, and live transfer

**Status: the pre-registered pipeline (§22.33) ran to completion; GATES A–D
PASS. v6 is READY for the (not yet run) frozen aria2-F comparison per the
pre-registered gates. No performance/superiority claim; live performance was
worse than fixed concurrency.** All v1–v5 and aria2 artifacts remain untouched.

- **Formulation:** 16 MiB horizon, 1 MiB fixed chunk, concurrency 1–16,
  actions none/increase/decrease, Double DQN, state spec **version 3** (same
  13-feature layout as v5, bounds [1,16]), reward = −interval wall-clock
  seconds, one decision per completed request in both simulator and live
  runner.
- **Candidate:** `testdata/diagnostics/dqn_validation_v6/final_dqn_candidate_v6.json`,
  SHA-256 `daa7176793093bea102965700774609fd70d4bbb60523d2417302e2a7f0d9887`;
  selected seed 2, 1200 episodes/seed, seeds 0–4; Go loads/infers it via
  `NewLiveConcurrencyPolicyV6` (dispatch and Go smoke test added).
- **GATE A (environment):** static C=1..16 sweep over pre-registered regimes →
  best fixed C = {1, 2, 5, 9, 16} (distinct); PASS.
- **GATE B (training):** v6 held-out mean 4.476 s vs best globally-fixed C
  (C12, chosen on validation only) 4.867 s; PASS. Per-condition oracle best
  fixed C (C6/C6/C2/C16/C2) was 0.03–0.33 s faster than v6.
- **GATE C (condition dependence):** constant 0.000, ramp-to-max 0.000, 3/5
  conditions with phase-to-phase time-weighted concurrency spread ≥ 2.0; PASS.
  Simulator phase means: A2 9.55→8.67→6.44, C2 9.03→5.04→7.00, E2 8.63→6.50→
  5.39, D2 ~15 (server throttle holds high concurrency), B2 8.96→8.18 (many
  runs finish before phase 3).
- **GATE D (live transfer):** `cmd/dqn-live-v6` ran the 5 trajectories ×
  {fixed C=1..16, v6} × 3 reps = **255/255 valid** runs on the controlled
  server with mid-run tc/netem qdisc replacement and server rate phases. v6
  live phase means: A2 1.3→2.3→3.6, B2 1.8→~6.2→~6.9, C2 1.2→~2.9→~5.1,
  D2 1.1→2.4→4.0, E2 1.8→5.8→5.6; constant 0.000, ramp 0.000, 4/5 responsive;
  PASS. Transition reaction latencies were observed in every condition.
- **Live performance (descriptive, not a ranking):** v6 mean 7578 ms vs best
  globally-fixed live C (C11) 5012 ms; per-condition best fixed: A2 C4 6588,
  B2 C8 3992, C2 C4 4151, D2 C16 2158, E2 C4 5508 ms. v6 was slower than the
  best fixed C in every live condition; the largest gap was D2 (server
  per-response throttle), where v6 held low/moderate concurrency while C16 was
  far faster. The live server's per-response pacing again rewards high
  concurrency; the simulator's shared-bottleneck tradeoff transfers only
  partially.
- **Implementation notes/fixes:** v6 fixed the inherited tail-accounting for
  the one-decision-per-request cadence (the final request was charged a full
  service time when interval < C, penalizing high C); `model.py` exports v6 by
  spec version; `state_v6.py/go` and `sim_env_v6.py` are additive; the
  `cmd/dqn-transfer` dispatch selects v6 by spec version.
- **Artifacts:** `testdata/diagnostics/dqn_validation_v6/` (static sweep,
  train config/results, held-out, adaptation, gates, report, candidate + sha)
  and `testdata/diagnostics/dqn_validation_v6_live/` (manifest, runs,
  aggregates, adaptation, gates, report); smoke copies under `*_smoke/`.
- **Next step (separate, not run):** the pre-registered frozen aria2-F
  comparison, with the structural asymmetry (v6 ≤16 vs aria2-F 16 connections)
  and the live fixed-C result documented; it must not be represented as a
  capability-matched comparison.

### 22.35 Final pre-registered head-to-head: v6 vs frozen aria2-F

**Status: completed. The pre-specified v6 advantage under unstable
trajectories was NOT supported; pooled and per-trajectory results are reported
as measured.** Frozen candidates only; no tuning; no post-hoc changes.

- **Frozen systems:** v6 SHA-256
  `daa7176793093bea102965700774609fd70d4bbb60523d2417302e2a7f0d9887`; aria2-F
  metadata SHA-256 `b72498176a48f889cd2d1dbc9d9ef6b9afc668f2cf0d902e44d51dee0dedca02`
  (aria2 1.37.0, 16 connections / split 16 / min-split 1 MiB). Both verified
  before the campaign; neither modified.
- **Matrix:** the five pre-registered v6 trajectories A2–E2 (three wall-clock
  phases each, §22.33) × 30 matched pairs = **150 pairs / 300 downloads**, at
  16 MiB / 1 MiB fixed chunk; deterministic per-pair order (seed 20260925);
  fresh server/output/netem per run with mid-run tc/netem qdisc replacement and
  server rate phases.
- **Harness/statistics:** `cmd/final-evaluation-v6` (v5 matched-pair structure
  + v6 phase scheduler); `python/analyze_final_evaluation.py` reused with an
  additive `--conditions` flag (default unchanged; majority criterion
  generalized to >half of the conditions); `python/report_final_evaluation_v6.py`
  writes the human-readable report.
- **Validity: 300/300 runs valid, 150/150 pairs, 0 exclusions** (exact 16 MiB +
  SHA-256 match, no errors, netem applied, qdisc clean).
- **Per-trajectory matched pairs (mean ms; ratio = geometric v6/aria2):**

  | traj | v6 | aria2 | ratio | ratio 95 % CI | p_holm |
  |---|---|---|---|---|---|
  | A2 (bandwidth collapse/recovery) | 8313.5 | 7855.4 | 1.065 | [1.017,1.116] | 1 |
  | B2 (latency spike/recovery) | 4932.6 | 4802.7 | 1.027 | [0.995,1.061] | 1 |
  | C2 (burst loss) | 5569.0 | 4828.4 | 1.153 | [1.118,1.190] | 1 |
  | D2 (server throttle/release) | 12043.9 | 2221.5 | 5.471 | [5.205,5.751] | 1 |
  | E2 (combined deterioration) | 6648.2 | 7055.1 | 0.948 | [0.897,1.002] | 0.128 |

- **Pooled:** v6 7501.5 ms vs aria2 5352.6 ms; geometric ratio **1.4560**
  (95 % CI [1.3004, 1.6303]); v6 faster in 37/150 pairs (24.7 %) and 1/5
  trajectories; all four pre-specified criteria false →
  `claim_supported = false`.
- **v6 adaptation (conclusion unchanged):** non-constant/non-ramp; phase mean C
  rises with recovery (A2 1.20→1.94→3.17; B2 1.84→6.15→6.37; C2 1.25→3.16→
  4.78; D2 1.10→2.40→3.93; E2 1.79→4.93→4.80); transition reaction medians
  213/196/299/1579/508 ms. Adaptation was observable but did not translate into
  lower completion time than aria2-F except (non-significantly) on E2.
- **aria2 behavior:** max in-flight mean **15.85** (max 16); 15.01 × 1 MiB
  range requests/run plus 1.00 non-range probe; 0 retries; 146 server-recorded
  failed requests (aborted probes); all outputs SHA-correct.
- **Remaining asymmetry (documented, not hidden):** both systems nominally
  allow 16 concurrent requests and both use ~1 MiB ranged requests, but aria2
  opens up to 16 connections immediately while v6 starts at C=1 and ramps;
  aria2 also issues one initial non-range probe.
- **Evidence-supported conclusion:** across the five pre-registered controlled
  unstable trajectories, **v6 did not show a measurable completion-time
  advantage over aria2-F** (pooled ratio 1.456; slower on A2/B2/C2/D2; ~5.2 %
  faster on E2, Holm p = 0.128, not significant). No general-superiority,
  mobile-network, or real-world generalization claim is made.
- **Artifacts:** `testdata/final_evaluation_v6/` — matrix + sha, schedule,
  `raw_results.json` (analyzer input), `raw_results_all.json` (full telemetry),
  `validity_report.json`, `manifest_v6.json`, `statistical_analysis.json`,
  `claim_assessment.json`, `summary.txt`, `per_condition_table.txt`,
  `report.txt`, `artifact_hashes.json`.

### 22.36 Long-duration diagnostic: frozen v6 vs aria2-F

**Status: completed (diagnostic only). It does NOT modify the v6 final result
(§22.35) and involved no training, no model/simulator changes, and no new
scenarios.** Question: does v6's disadvantage shrink as the download becomes
long enough to amortize its startup/adaptation cost?

- **Frozen systems:** v6 SHA-256
  `daa7176793093bea102965700774609fd70d4bbb60523d2417302e2a7f0d9887`; aria2-F
  metadata SHA-256 `b72498176a48f889cd2d1dbc9d9ef6b9afc668f2cf0d902e44d51dee0dedca02`
  (unchanged).
- **Design (fixed before results):** pre-registered trajectories **A2
  (bandwidth collapse/recovery), D2 (server throttle/release), E2
  (combined deterioration)**, with the **original 2 s phases unchanged**; sizes
  **16, 128, 512 MiB**; 5 matched pairs per size × trajectory = 45 pairs /
  90 downloads; seed 20260925; fresh environment per run. **1 GiB was not run:
  the container has ~1 GB RAM available and a 1 GiB object (plus the server's
  copy) would risk OOM; this resource exclusion is independent of results.**
- **Harness:** `cmd/final-evaluation-v6` gained an additive `-file-size` flag
  (default 16 MiB; the frozen matrix is unchanged); `python/analyze_long_duration_v6.py`
  aggregates the per-size established analyses.
- **Validity: 90/90 runs valid, 45/45 pairs, 0 exclusions** (exact size +
  SHA-256, no errors, netem applied, qdisc clean).
- **Primary metric (geometric v6/aria2 ratio; startup fraction = time to first
  effective v6 change / v6 total):**

  | size | traj | v6 ms | aria2 ms | ratio | 95 % CI | startup frac | post-startup ratio |
  |---|---|---|---|---|---|---|---|
  | 16 MiB | A2 | 8041.6 | 8635.1 | 0.943 | [0.773,1.150] | 0.148 | 0.820 |
  | 16 MiB | D2 | 12260.8 | 2041.0 | 6.004 | [5.835,6.178] | 0.147 | 5.123 |
  | 16 MiB | E2 | 6748.6 | 6480.4 | 1.039 | [0.949,1.139] | 0.080 | 0.960 |
  | 128 MiB | A2 | 38827.8 | 39947.7 | 0.973 | [0.892,1.061] | 0.026 | 0.951 |
  | 128 MiB | D2 | 15834.7 | 13650.2 | 1.160 | [1.139,1.182] | 0.114 | 1.028 |
  | 128 MiB | E2 | 46296.0 | 43549.1 | 1.062 | [0.989,1.141] | 0.013 | 1.051 |
  | 512 MiB | A2 | 149609.4 | 137043.1 | 1.092 | [1.065,1.119] | 0.009 | 1.083 |
  | 512 MiB | D2 | 30316.0 | 53313.7 | 0.568 | [0.554,0.583] | 0.060 | 0.535 |
  | 512 MiB | E2 | 194418.2 | 160454.1 | 1.211 | [1.188,1.235] | 0.004 | 1.207 |
  | 16 MiB | ALL | 9017.0 | 5718.8 | 1.805 | [1.128,2.888] | 0.125 | 2.301 |
  | 128 MiB | ALL | 33652.8 | 32382.3 | 1.062 | [1.008,1.120] | 0.051 | 1.010 |
  | 512 MiB | ALL | 124781.2 | 116937.0 | 0.909 | [0.757,1.092] | 0.024 | 0.941 |

- **Pre-defined verdict:** the pooled ratio moves 1.805 → 1.062 → 0.909 with
  startup falling to 2.4 % of runtime at 512 MiB → the encoded rule returns
  **STRONGLY_PROMISING** (long ratio ≤ 1.0). **Per-trajectory, however, the
  result is MIXED and not robust:** A2 goes 0.943 → 0.973 → **1.092** and E2
  goes 1.039 → 1.062 → **1.211** — v6's disadvantage *grows* with size and
  post-startup v6 remains slower, i.e. CASE B (starts badly and stays
  systematically worse), contradicting the amortization hypothesis for those
  trajectories. The pooled ≤1.0 result is driven entirely by **D2**, which
  reverses 6.004 → 1.160 → **0.568** (at 512 MiB aria2 is unexpectedly slow:
  53.3 s vs v6 30.3 s).
- **Startup-cost analysis:** startup time is indeed amortized (12.5 % → 5.1 %
  → 2.4 % of v6 runtime), so the diagnostic cleanly separates startup from
  steady state. The separation shows the pooled improvement is **not** a
  broadly amortized-startup effect: with startup removed, A2/E2 still worsen
  with size.
- **Caveat (unchanged trajectories):** with absolute 2 s phases the disturbed
  window is fixed, so the disturbed fraction shrinks with size and the
  recovered phase dominates long transfers. A duration diagnostic with
  size-proportional or repeated transitions remains a separately pre-registered
  design, not a change to these frozen trajectories.
- **Evidence-supported conclusion:** making the download longer **does** amortize
  v6's startup, and the pooled ratio crosses 1.0 at 512 MiB, but the advantage
  appears only in D2 while A2/E2 show a growing disadvantage. Longer duration is
  therefore **promising only if the D2 large-size behavior is reproduced and
  explained**; the current evidence does not support a general "longer is
  better" conclusion for v6. This does not modify §22.35.
- **Artifacts:** `testdata/diagnostics/dqn_long_duration_v6/` — per-size
  `size_{16,128,512}MiB/` (matrix, manifest, raw runs, established
  `statistical_analysis.json`/`summary.txt`), plus `long_duration_summary.json`/
  `.txt`, `per_pair_v6_vs_aria2.json`, `report.txt`, `artifact_hashes.json`;
  smoke dirs `dqn_long_duration_v6_smoke_{16,128}`.

### 22.37 D2 mechanism + long multi-transition trajectory diagnostic

**Status: completed. Diagnostic only; no training; no v7; no change to the
§22.35 v6 final result.**

- **D2/512 MiB replicated:** dedicated campaign of **20 matched pairs** (fresh
  server/output/netem per run, deterministic order seed 20260925): v6
  31.14 s vs aria2 53.45 s mean; v6/aria2 geometric ratio **0.5816**
  (95 % CI [0.566, 0.598]); v6 faster in **20/20** pairs; the established
  analysis gives `claim_supported = true` for this single condition.
  **D2 REPRODUCED.**
- **Mechanism (identified from telemetry, not assumed):** on 512 MiB aria2 with
  `--split=16` issues **15 × 32 MiB** range pieces (plus one probe); all start
  before the 2 s transition at the 640 KiB/s initial rate. The server selects a
  request's pacing rate at request start and holds it for the whole request, so
  each 32 MiB piece takes ~51 s. v6 allocates **1 MiB** ranges dynamically:
  only ~7 requests start before recovery and **505/512 start after 4 s** at
  2.5 MiB/s. The reversal is a **request-granularity × phase-locked-pacing
  interaction**, not generic adaptive superiority; at 16 MiB aria2's pieces are
  1 MiB and complete before the first transition, so the effect disappears.
- **New long trajectory family L1** (separate from the frozen A2–E2): seven
  predetermined phases — good (B=6 MiB), severe degradation (B=1 MiB, 3 % loss,
  high latency), recovery (B=4 MiB), server throttle (256 KiB/s per response, no
  shared cap), recovery (B=5 MiB), degradation (B=1.5 MiB), final good — with
  phase durations predetermined in **[12,18] s**, last phase to completion.
- **Static C=1..16 sweeps (frozen v6 simulator only):** per-phase steady optima
  are **C = 1, 2, 5, 16, 5, 3, 1** (distinct best C {1,2,3,5,16}): high C is
  optimal in the server-throttle phase; low/moderate C is optimal under shared
  caps with latency/loss. Full 512 MiB L1 (15 s phases): every policy traverses
  all seven phases; best fixed C=15 (137.8 s) vs C1 (143.7 s) — a **~4 % total
  completion-time spread**, much smaller than the per-phase differences because
  the shared caps bound each phase and all policies see the same sequence.
- **File size for any future live long-duration experiment: 512 MiB** (largest
  RAM-safe size in this container; the trajectory stays dynamic for ~90 s; the
  measured startup fraction at 512 MiB was 2.4 %); 1 GiB remains unsafe.
- **Interpretation:** D2 is reproducible and mechanistically explained. The L1
  environment has genuinely different per-phase preferred concurrency, but the
  full-run completion-time headroom is only ~4 %, so the duration hypothesis is
  **not yet shown to be strongly promising**. Any v7 design must demonstrate it
  can capture that headroom, and must not rely on the D2 granularity effect
  (which v6's fixed 1 MiB chunk already obtains for free).
- **Artifacts:** `testdata/diagnostics/long_duration_d2_diagnostic/` —
  `replication/` (raw runs + established statistics), `smoke/`,
  `d2_mechanism.json`, `long_trajectory_spec.json`,
  `static_sweep_long_trajectory.json`, `report.txt`, `artifact_hashes.json`.

### 22.38 Candidate v7 environment pre-registration: L2 (written BEFORE the sweep)

**Status: pre-registered; measurements follow below in §22.39. No v7 training,
no v6 changes, no new final comparison.**

**Why L1 was weak (quantified in §22.37):** L1's per-phase best C differed
({1,2,3,5,16}), but its full 512 MiB completion times spanned only
137.8 s (C16) – 143.7 s (C1), ≈4 %; the shared caps bound each phase, so all
policies paid the same phase-capacity cost and only overhead/loss terms were
C-dependent. The cumulative payoff from adapting C was therefore small.

**L2 design (fixed before measurement), separate from frozen A2–E2 and L1:**
512 MiB, 1 MiB chunks, C 1–16, seven predetermined phases with durations in
**[18, 25] s** (phases 1–6; phase 7 runs to completion):

| # | regime | rate | shared cap B | latency | loss | congestion | intent |
|---|---|---|---|---|---|---|---|
| 1 | server capacity | 512 KiB/s | 0 | 20 ms | 0.3 % | 0.002 | high C useful |
| 2 | severe shared bottleneck | 8 MiB/s | 1 MiB/s | 120 ms | 3.0 % | 0.03 | low C materially better |
| 3 | recovery | 8 MiB/s | 4 MiB/s | 40 ms | 0.5 % | 0.006 | moderate C |
| 4 | severe server throttle | 128 KiB/s | 0 | 30 ms | 0.5 % | 0.004 | high C useful |
| 5 | heavy congestion | 8 MiB/s | 2.5 MiB/s | 100 ms | 2.0 % | 0.06 | low C materially better |
| 6 | recovery | 8 MiB/s | 5 MiB/s | 30 ms | 0.4 % | 0.004 | moderate/high C |
| 7 | final server capacity | 1 MiB/s | 0 | 20 ms | 0.3 % | 0.002 | high C useful, to completion |

Each regime is a controlled abstraction of a plausible unstable/mobile-like
condition (capacity collapse, severe shared-bottleneck congestion, recovery,
server-side throttling, and a capacity-limited final phase); this is not claimed
to be a literal cellular/train link. No condition labels or reward shaping are
exposed to the policy.

**Sweep and metrics (fixed before measurement):** deterministic simulator
sweeps (seeds 300–302) of the frozen v6 simulator: per-phase steady sweeps
(128 MiB, C=1..16) and the full 512 MiB L2 trajectory (C=1..16) plus a
ramp-aware dynamic oracle (knows each phase's best C and moves C by ±1 per
decision, i.e. within the frozen action semantics). Definitions:
`best_static = min_C full-trajectory completion`;
`dynamic_oracle = completion under the ramp-aware oracle`;
`adaptive_headroom = (best_static − dynamic_oracle) / best_static`.
Threshold rationale: live/run-to-run dispersion in the v6 campaigns was
~1–5 %; headroom < 5 % is treated as too weak, 5–10 % as marginal, and
≥ 10 % as meaningful.

**New long-duration aria2 baseline (experimental, separate from frozen F):**
candidate `--max-connection-per-server=16 --split=512 --min-split-size=1048576`
intended to give ~1 MiB ranges with ≤16 simultaneous connections at 512 MiB.
Part 7 requires empirical verification: if the installed aria2 1.37.0 cannot
produce ~1 MiB ranges under this option set, the outcome is
`NOT_READY_ARIA2_GRANULARITY` and the comparison is not claimed to be
granularity-matched. Frozen aria2-F is unchanged.

### 22.39 L2 environment-design results and V7 decision gate

**Status: completed (simulator design sweeps + live aria2 granularity probe;
no training; no final comparison). Gates: `NOT_READY_ENVIRONMENT_TOO_WEAK`
(primary) and `NOT_READY_ARIA2_GRANULARITY` (blocking for a granularity-matched
comparison). Neither category was redefined after results.**

- **Transitions smoke:** all seven L2 phases were reached in a trajectory run
  (transitions [1..6]).
- **Per-phase steady C=1..16 sweep (128 MiB, seeds 300–302):** best C by phase =
  **[16, 2, 4, 16, 2, 6, 16]**; C16-vs-best completion gap = 0.0 %, 35.9 %,
  5.7 %, 0.0 %, 62.2 %, 3.5 %, 0.0 %. So the degraded phases materially dislike
  high C, while the capacity/throttle phases strongly require it — the
  pre-registered tradeoff exists per phase.
- **Full 512 MiB L2 trajectory (20 s phases, 3 seeds):** best static C = **16**
  (132.9 s); C1 = 422.8 s; **ramp-aware dynamic oracle = 133.2 s** →
  achievable adaptive headroom = **−0.3 %**. The idealized phase-wise oracle
  (instant C changes, perfect future knowledge) would be 117.7 s → 11.4 % upper
  bound, but that is **not reachable** under the frozen one-decision-per-request
  and ±1-concurrency action semantics. Phase bytes at best static
  ([137,13,67,37,25,85,148] MiB) and times (20.1/20.4/19.7/20.3/19.6/20.4/
  12.5 s) show all transitions occur while substantial work remains; the reason
  the headroom is ~0 is that the high-C phases carry most of the bytes while the
  low-C degraded phases carry few, and the ramp is too slow to exploit the
  transitions.
- **aria2 long-duration granularity probe (experiment-only
  `cmd/l2-aria2-probe`, 512 MiB, unlimited rate):** frozen-F-like
  `--split=16 --max-connection-per-server=16 --min-split-size=1MiB` → 15 ×
  32 MiB pieces (+ probe), max in-flight 16. Candidate
  `--split=512 --max-connection-per-server=16 --min-split-size=1MiB` → **still
  15 × 32 MiB pieces** (aria2 1.37.0 bounds the piece count by
  `--max-connection-per-server`). **~1 MiB ranges with ≤16 simultaneous
  connections at 512 MiB is not achievable**, so the granularity-matched
  long-duration aria2 baseline required by §22.38 does not exist with the
  installed aria2; the new baseline was not adopted. Frozen aria2-F is
  unchanged and no comparison is claimed.
- **Consequence:** a fair v7 test of adaptive concurrency would first need an
  environment with much larger *byte-weighted* wrong-C penalties (or faster
  control authority, which is a v7 formulation decision), and separately a
  granularity-matched aggressive baseline that aria2 1.37.0 cannot currently
  provide at this scale. v7 training is therefore **not** justified yet.
- **Artifacts:** `testdata/diagnostics/l2_environment_design/` —
  `l2_spec.json`, `l2_smoke.json`, `l2_static_phase_sweep.json`,
  `l2_full_trajectory_sweep.json`, `l2_adaptive_headroom.json`, `report.txt`,
  `aria2_probe/probe_split{16,512}_maxconn16.json`, `artifact_hashes.json`.

