## 23. Test coverage (by subsystem)

Test files exist for every package. Highlights (behavioral, not exhaustive):

- **RangeTracker:** sequential/variable-chunk allocation, final partial chunk,
  completion, requeue/merging, duplicate-allocation prevention, concurrency.
- **Fetcher:** success + metadata, `Range` header, 206/`Content-Range`
  validation, 200 rejection, incomplete/overlong bodies, server error,
  connection failure, timeout/cancellation, invalid range; dedicated
  HTTP/1.1-only transport (real plaintext and h2-enabled-TLS requests),
  Cmax=16 idle-pool configuration, `http.DefaultTransport` untouched,
  caller-supplied client passed through unchanged; single-connection reuse
  across sequential ranges verified via client `httptrace` plus server-side
  `ConnContext` identity (no dials beyond the first, all responses HTTP/1.1);
  incremental timestamped received-byte accounting (optional
  `ByteObserver`/`ByteMeter`: read-time attribution, per-connection identity,
  partial-read/error preservation, multi-interval, failed-response and
  concurrent-request coverage).
- **Assembler:** single/in-order/out-of-order writes, first/middle/final,
  invalid range, length mismatch, exact reconstruction, size, close semantics.
- **Downloader:** fixed/adaptive concurrency, out-of-order completion, retry
  then success, retry-limit error, context cancellation, metrics per mode,
  serialized decisions, chunk-size integration (fixed/dynamic/bounds/final
  partial), in-flight immutability, retry-boundary preservation; empirical
  C→flow mapping for C∈{1,2,4,8,16} against a deterministic barrier server
  (server-side connection identity: max in-flight requests and max active TCP
  connections equal C, exactly C total connections created, one request per
  connection at a time, reuse without idle-pool churn); optional
  `ByteObserver` wiring verified under gated C=4 concurrency (aggregate and
  per-request totals cross-checked against attempt records, cumulative
  monotonicity, distinct connection identities, output byte-match).
- **Metrics/Concurrency/Chunksize:** per-attempt records and aggregates;
  controller bounds/clamping/set/min==max/concurrency safety.
- **Policies:** each policy's decision rules, thresholds, zero-throughput,
  bounds, alternation/fallback (JointPolicy), interface implementation.
- **Experiment:** four-mode expansion/equivalence/fresh-state, aggregates and
  variability, JSON/text, network apply/clear and unavailable exclusion,
  matrix scenarios; LinUCB opt-in mode construction/validation/completion, the
  default `AllModes()` excluding LinUCB, and `MatrixConfig.Modes` selection;
  LinUCB calibration diagnostics (action distribution, reward stats, component
  aggregation, churn, reward reconstruction cross-check, determinism) and the
  `cmd/calibration` CLI (`distributionString`, JSON round-trip, report);
  `cmd/experiment` matrix file-size override (`applyFileSizeOverride` preserves
  the default and does not mutate `DefaultMatrixConfig`).
- **Experiment server / network:** range responses, invalid ranges, delay/rate/
  runtime rate change, deterministic failures, stats; logical bounded-memory
  workload (`NewLogical`, `DefaultLogicalSize`): deterministic offset-addressed
  content, exact ranges/headers, invalid-range 416s, bounded-memory construction
  and 64 MiB range streaming, offset stability, finite-server compatibility;
  tc command construction, error classification, interface detection, gated
  privileged integration test.
- **Mahimahi replay harness (§22.59):** trace parsing/validation; invalid
  configuration; mm-link start failure; downloader start failure; watchdog
  timeout with no leftover processes; local child download against the logical
  server (byte-exact); synthetic C={1,4,16} mm-link smoke with host-network
  before/after comparison; workload exhaustion mapping; distinct outcome exit
  codes. **Evidence-based EOF (§22.63):** endpoint residual arithmetic
  (exact/inside/boundary/outside/observed-beyond/missing evidence); synthetic
  CASES A–F — A real EOF + clean child → EOF; B early clean child exit +
  mm-link rc=0 → NOT EOF; C fatal child + mm-link rc=0 → NOT EOF; D missing
  endpoint evidence → NOT EOF; E timeout-then-retry-success → child success;
  F timeout + retry exhaustion → non-zero child and `DOWNLOADER_FATAL_ERROR`
  (NOT EOF); missing summary → NOT EOF (never EOF); child statuses
  (clean/fatal/canceled) and child exit contract (clean → no error, fatal
  retry-limit → non-nil error with persisted `fatal`+`err`, cancellation → not
  fatal). The real mm-link smoke and anchor-calibration tests also assert
  endpoint evidence.
- **Trace-pair resolution (§22.63):** `ResolveTracePair` accepts a bare stem,
  `.up`, or `.down` path and returns both members; missing `.up`/`.down`,
  cross-directory pairs, mismatched families, and missing explicit members are
  rejected; `Plan` carries the resolved `UpPath`/`DownPath` per run.
- **Watchdog-only timeout policy (§22.68):** explicit no-request/no-dial
  deadline configuration and ambiguity validation (`fetcher`), mirrored
  `downloader` policy fields, `replay.ParseDeadline`/`DeadlineString`,
  `cmd/epoch-pilot` and `cmd/mm-replay` `-timeout`/`-dial-timeout` parsing
  (`none` vs positive duration), versioned protocol revision
  (`epoch.DefaultProtocol` v2, `LegacyProtocolV1`,
  `ValidateTimeoutPolicyRevision`), no-client-local-deadline behavior, blocked
  request canceled by the parent watchdog (`WATCHDOG_TIMEOUT`, non-EOF, clean
  shutdown), canceled child before EOF (not EOF), external cancellation
  (`CANCELED`, not EOF), genuine-failure retry retained, and
  cancellation-not-retried.
- **Trace anchor (§22.60):** trace-time arithmetic; launch-based anchor
  independent of child start; first-byte-as-measurement; EOF represented
  separately from child completion; monotonic arithmetic; calibration
  statistics; tolerance boundary; raw/derived reproducibility; mm-link
  trace-log parsing; repeated C={1,4,16} synthetic calibration with two known
  gap edges and downlink-log cross-checks.
- **Epoch construction / pilot accounting (§22.61):** exact trace-time
  boundaries incl. partial tail; read-time cross-epoch attribution; 5 MiB
  byte-floor boundary; 0.80 observed-fraction boundary; observed-duration R
  divisor; shared cross-C exclusion; trace-time anchoring; determinism;
  exclusion accounting and shortest-passing candidate selection; revision
  rules; frozen native-trace corpus scan and good/degraded classification.
- **Epoch pilot driver (§22.61):** 10-run plan generation; unique run
  directories and resume semantics; failure-record preservation; streaming
  JSONL event accumulation into all four candidate durations in one pass;
  post-hoc multi-duration evaluation; shared validity and exclusion
  accounting; shortest-passing selection; missing-anchor non-fabrication;
  dry-run configuration/plan output (verified manually with `-dry-run`).
- **RL:** state conversion/validation/features, action mapping/round-trip,
  reward validation, adapter conversion, `AlwaysActionPolicy`, real-downloader
  integration for increase-chunk / increase-concurrency / none; LinUCB
  (config validation, initial model, deterministic/lowest-index selection,
  disjoint action isolation, reward-sign effects, hand-computable d=1 update,
  exploration math, malformed-input fallback, adapter compatibility, and a
  package-local check that `Update` changes the selected arm's model while
  leaving others untouched); reward computation (config validation,
  first-observation handling, throughput/latency/retry/failure/action
   components, aggregate-throughput source, hand-computable formula,
   determinism, edge cases); the new **aggregate-throughput observable**
   (runner per-request-vs-overlapping discrimination with a fake clock, feature
   normalization/order, and reward-uses-aggregate); runner-level
   **credit-assignment timing** (Update receives the previous context/action;
   Update count = SelectAction count − 1); and end-to-end **causal ordering and
   reward association** through the real Adapter + downloader
   (`internal/rl/causal_integration_test.go`) plus a first-observation no-update
   test.
- **Diagnostics:** `internal/diagnostics` (forced-action correctness for
  none / increase-concurrency / increase-chunk-size, report shape, and the
  before/after exploration-correction coverage test) and a white-box
  `internal/rl` diagnostic test that instruments the real LinUCB score/update to
  document the initialization tie and greedy lock-in.
- **Exploration correction (§22.8):** `internal/rl/linucb_exploration_correction_test.go`
  (per-action coverage, multiple rounds, determinism, transition to normal
  selection, no-op effectiveness bookkeeping, invalid/mismatched state,
  unchanged `A`/`b` update, default-disabled).
- **Learning-horizon study (§22.10):** `internal/diagnostics/horizon_test.go`
  (warm-up is 5, larger file yields more total and post-warm-up decisions,
  correctness across modes).
- **Post-warm-up lock-in diagnostic (§22.12):**
  `internal/rl/linucb_lockin_diagnostic_test.go` replays the frozen v3 data
  through the production LinUCB, asserts a 100 % action-replay match, and writes
  the deterministic artifacts under `testdata/diagnostics/linucb_lockin/`.
- **Periodic exploration (§22.13):** `internal/rl/linucb_periodic_exploration_test.go`
  (interval 0 preserves behavior; interval 1/5 timing; deterministic cursor
  cycle; warm-up once; standard `A`/`b` update; greedy between events;
  invalid state does not advance the cursor; effectiveness bookkeeping) and
  `linucb_periodic_exploration_diagnostic_test.go` (deterministic frozen-v3
  replay, writes `testdata/diagnostics/linucb_periodic_exploration/`).
- **Live periodic benchmark (§22.14):**
  `internal/rl/linucb_periodic_exploration_live_test.go` replays the frozen v4
  standard/periodic runs through the production LinUCB (action-match validated)
  and writes `testdata/diagnostics/linucb_periodic_exploration_live/`;
  `cmd/experiment` interval override covered by `TestApplyLinUCBIntervalOverride`.
- **Six-mode mode plumbing (§22.15):** `ModeLinUCBPeriodic` /
  `AllModesWithBothLinUCB` exercised by the experiment tests and the
  `cmd/experiment` mode parser.
- **aria2 baseline harness (§22.16):** `cmd/aria2-benchmark/main_test.go`
  (`parseAria2Log`, `summarizeRanges`, deterministic `contentPattern`).
- **Granularity study (§22.17):** `cmd/granularity-study/main_test.go`
  (`summarizeRanges`, `contentPattern`) plus an integration run asserting 4×
  1 MiB requests, byte-match output, and in-flight concurrency ≤ 4.
- **Wall-clock schedule (§14.1, §22.18):** `internal/experiment/server/server_test.go`
  (validation, before/at/after transition, request-count independence, same
  elapsed → same rate, sequence diagnostic, unchanged `SetRateAfter`,
  clear/restore, concurrent consistency, request log) and
  `internal/experiment/wallclock_schedule_test.go` (wired `ScenarioConfig`
  schedule runs correctly and transitions by elapsed time).
- **aria2 wall-clock baseline (§22.19):** `cmd/aria2-wallclock/main_test.go`
  (`summarizeRanges`, `countRetries`, `contentPattern`, locked-config flags).
- **Wall-clock granularity control (§22.20):** `cmd/granularity-study/main_test.go`
  (`-scenario wallclock` produces 4×1 MiB requests, max in-flight 4, all before
  the 1 s transition).
- **Sequential RL / DQN (§22.22):** `internal/rl/dqn/` tests cover state
  normalization/temporal features/remaining-bytes/malformed rejection/
  determinism; model forward pass/action dim/malformed/round-trip; simulator
  reset/step/determinism/termination/truncation/randomization; action mapping and
  policy adapter; and loading the Python-exported model with deterministic Go
  inference. Python tests (`python/tests/`) cover the mirrored state contract,
  simulator, replay buffer, finite-loss/target-update/seed-hash determinism, and
  model export.
- **DQN validation/transfer (§22.23):** `python/train_validate.py` (multi-seed
  train, validation selection, held-out test, ablations, candidate freeze);
  `internal/rl/dqn/live.go` (incremental live-policy state + greedy inference);
  `cmd/dqn-transfer/` (real-downloader transfer run).
- **DQN env correction / v2 (§22.24):** `python/train_validate_v2.py` (corrected
  simulator training/validation/freeze) and the corrected `python/rl/sim_env.py`
  dynamics; the DQN Go package and transfer command are reused unchanged.
- **DQN v3/v4 (§22.27–§22.28):** `python/train_validate_v3.py` /
  `python/rl/sim_env_v3.py` (chunk-ladder + decision interval) and
  `python/train_validate_v4.py` / `python/rl/sim_env_v4.py` (concurrency-only,
  1 MiB fixed). Go: `internal/rl/dqn/policy.go`   (`ConcurrencyActionNames`/
  `ConcurrencyIndexToAction`), `internal/rl/dqn/live.go`
  (`NewLiveConcurrencyPolicy`); `cmd/dqn-transfer` (`-max-chunk`, `-fixed-chunk`).
- **DQN v5 pipeline (§22.30):** `python/train_validate_v5.py` was exercised by a
  tiny deterministic smoke run and then the full campaign (static sweep,
  interval selection, 5-seed training, validation selection, held-out test vs
  fixed C=1..4, diagnostics, gate, export). `internal/rl/dqn/v5_candidate_smoke_test.go`
  loads the exported v5 artifact, validates its state/action contract and
  SHA-256, and runs deterministic v5 greedy inference (skips if absent).
  `cmd/dqn-transfer` has `TestNewLivePolicyForModelDispatchesV5`; Python tests
  assert the new per-episode `returns`/`episode_steps` report fields.
- **DQN v5 live transfer (§22.31):** `cmd/dqn-live-v5` unit tests cover
  authoritative after-action derivation, range tiling, in-flight overlap,
  progress accounting, trajectory classification, and reaction analysis; the
  125-run campaign plus a 4-run smoke record all required per-run fields and
  pass the pre-specified gates.
- **Final v5-vs-aria2-v2 head-to-head (§22.32):** `cmd/final-evaluation-v5`
  unit tests cover the reused U1–U8 conditions, deterministic pair schedule,
  frozen-command argv parsing, authoritative v5 after-action derivation,
  transition reaction timing, in-flight overlap and range/progress accounting;
  the reused analyzer and `python/report_final_evaluation_v5.py` operate only on
  the frozen artifacts.
- **Candidate v6 (§22.33–§22.34):** `python/tests/test_state_v6.py`,
  `python/tests/test_sim_env_v6.py` (including the one-decision-per-request
  timing contract and shared-bottleneck diminishing returns),
  `internal/rl/dqn/state_v6_test.go` (feature parity, spec separation, live
  cadence), `internal/rl/dqn/v6_candidate_smoke_test.go` (artifact load/infer),
  and the static sweep/training/live commands.
- **Final v6-vs-aria2-F head-to-head (§22.35):** `cmd/final-evaluation-v6`
  reuses the v5 matched-pair structure with the v6 wall-clock phase scheduler;
  `python/analyze_final_evaluation.py` gained an additive `--conditions` flag
  (default behavior unchanged); `python/report_final_evaluation_v6.py`
  generates the report from the frozen artifacts.

**Latest verified test status:** after the RL State Observability Audit,
`go test ./...`, `go test -race ./...`,
`go test -count=5 ./internal/rl/ ./internal/downloader/ ./internal/experiment/`,
`go vet ./...`, `gofmt -l .`, and `go build ./...` all pass. Statement coverage
from a prior run: `internal/rl` 88.7 %, `internal/downloader` 92.3 %; earlier
figures: downloader ~93 %, policy/chunksize/concurrency/metrics ~100 %,
rangetracker ~96 %, fetcher ~86 %, assembler ~79 %, experiment ~93 %. Re-run to
confirm; do not treat these as frozen. After the Candidate v5 simulator/state
audit (§22.29), the same Go commands plus `python3 -m pytest python/tests -q`
(29 passed) all pass again, and the frozen `testdata/` manifest (155 files) is
byte-identical.

---

