# Stage 0S Pilot Report — Salvage Protocol v7

Frozen protocol: `salvage_s_protocol_v7` — `FINAL_S_PROTOCOL_FROZEN — READY_FOR_STAGE_0S`

**This is NOT a confirmatory adaptive-effect result.** Stage 0S is a
feasibility/mechanism gate only. No switching strategy was run, no winner is
reported, and no adaptive chunk-size improvement is claimed.

## 1. Experimental design

- Mechanism: R = 500000 bytes/s (4 Mbps), D = 40 ms, FileSize = 536870912 bytes (512 MiB), N = 4
- Arms: 256KiB (262144 B), 1MiB (1048576 B), 4MiB (4194304 B), 16MiB (16777216 B)
- Design: 8 replicate blocks x 4 arms = 32 runs; randomized order within each block
  (seed 20260928, recorded in the manifest).
- Server: internal/experiment/server.NewLogical; client: internal/downloader (fixed mode; no policy).
- No adaptive switching; each run is fixed at its assigned chunk size.

## 2. Executed run ledger

- planned: 32, attempted: 32, clean: 32, technical failures: 0, reruns: 0
- technical-rerun policy: the frozen protocol contains no rerun authorization;
  failed runs are recorded and not rerun.

## 3. Per-arm results (server-side per-request service time)

| Arm | expected D+s/R (s) | pooled median (s) | relative deviation | within +/-25% | requests |
|---|---|---|---|---|---|
| 256KiB | 0.564288 | 0.577717 | +2.3798% | PASS | 16384 |
| 1MiB | 2.137152 | 2.190526 | +2.4975% | PASS | 4096 |
| 4MiB | 8.428608 | 8.640930 | +2.5191% | PASS | 1024 |
| 16MiB | 33.594432 | 34.427871 | +2.4809% | PASS | 256 |

## 4. Criterion 1 — mechanism presence: **PASS**

## 5. Criterion 2 — strict service-time direction: **PASS**

Medians: 256KiB=0.577717 < 1MiB=2.190526 < 4MiB=8.640930 < 16MiB=34.427871

## 6. Criterion 3 — tail sufficiency (16 MiB arm): **PASS**

- realized tail durations (s): 34.867, 35.263, 35.003, 34.942, 35.618, 34.599, 35.111, 34.952
- pooled median: 34.977589 s; bounds [26.8755456, 41.9930400] s; >= 20 s: True

## 7. Criterion 4 — feasibility/reliability: **PASS**

- clean runs: 32/32 (>= 28 required)
- maximum wall-clock: 296.629 s (<= 1800 s required)
- within-block residual SD of log(T/T_ref): 0.000795 (<= 0.25 required), df = 21

## 8. Formal four-criterion decision

- criterion_1_mechanism_presence: PASS
- criterion_2_direction: PASS
- criterion_3_tail_sufficiency: PASS
- criterion_4_feasibility_reliability: PASS

## 9. Final GO/NO-GO: **GO**

A GO means only that the frozen mechanism and finite-file tail were sufficiently
realized under the pre-specified Stage 0S feasibility criteria to justify proceeding
to the next pre-specified stage. It does not mean chunk-size adaptation is beneficial.

## 10. Limitations / anomalies

- No technical failures or anomalies were observed; see the run ledger for details.
- Service times are server-side; client-side medians are recorded as diagnostics in
  analysis/per_arm.json.
- The content source is the deterministic logical server (bounded memory) with the same
  range semantics and pacing as the materialized server.

## 11. Artifact manifest

- `manifest/manifest.json`, `manifest/run_ledger.jsonl`, `manifest/run_ledger.json`
- `raw/<run_id>.json` (32 raw artifacts incl. client records and server request log)
- `logs/<run_id>.log`, `logs/driver.log`
- `analysis/stage0s_analysis.json`, `analysis/per_run.csv`, `analysis/per_arm.json`,
  `analysis/direction.json`, `analysis/tail.json`, `analysis/reliability.json`,
  `analysis/criteria.json`
- `tools/stage0s_run.go`, `tools/run_pilot.py`, `tools/analyze_stage0s.py`,
  `tools/preflight/preflight_smoke.go`

## 12. Confirmatory separation

No Bayesian confirmatory analysis, no switching strategy, and no SSR simulation was
run in this task.
