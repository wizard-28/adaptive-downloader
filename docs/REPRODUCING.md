# Reproducing the results

This guide is self-contained. It lists what is required, what can be verified
offline in minutes from the archived raw runs, and what must be re-executed live.

## 1. Requirements

| Item | Version / note |
|---|---|
| OS | Linux (developed on a Linux container; `tc`/`netem` needed for live runs) |
| Go | 1.27.1 (`go.mod`); only external Go dependency is `gonum.org/v1/gonum` |
| Python | 3.x; standard library only for analysis and `tools/claims_check.py` |
| Python (RL training only) | `torch==2.9.0+cpu`, `numpy==2.5.3` (`python/requirements.txt`); inference is in Go |
| aria2 | **1.37.0** (`aria2c`), the version used for every baseline |
| Live runs | root-equivalent `sudo -n tc ...` on the loopback interface; ~1 GB free RAM |

Live experiments shape the loopback interface with `tc qdisc ... netem`. Every
driver applies and clears shaping per run and records `netem_status`; runs
whose shaping could not be applied are marked invalid and are excluded from
ratios, never silently averaged.

## 2. Offline verification (minutes, no network shaping)

```
go build ./... && go vet ./...
go test -count=1 ./...                  # unit + integration tests
go test -race -count=1 ./internal/experiment/replay/ ./internal/rl/...
python3 -m pytest python/tests -q       # simulator/state tests (needs torch/numpy for some)
python3 python/gen_state_parity_v5.py --check
python3 tools/claims_check.py           # recomputes every number in the paper
```

`tools/claims_check.py` reads only raw run files under `testdata/` and compares
each recomputed value with `tools/paper_numbers.json` (the numbers quoted in
`reports/paper_draft.md`) within a stated tolerance. It exits non-zero on any
mismatch. `--print` lists all recomputed values.

Offline analysis scripts that regenerate committed analysis JSON from raw runs:
`testdata/n_dependent_server_joint_ns/tools/{analyze_adaptive_b,analyze_gate_b,
analyze_track2,robustness_w1_w6}.py`. Re-running them should leave
`git diff --stat` empty.

## 3. Where each headline result comes from

| Result | Raw runs | Analysis |
|---|---|---|
| D2: ours (16 conn, 4 MiB) vs aria2, 30 pairs | `testdata/aria2_competitive_benchmark/final_reproduction/` | `analysis/benchmark_analysis.json`; `tools/claims_check.py` |
| Live gate for the constructed cap | `testdata/n_dependent_server_joint_ns/fixed_grid/`, `precision/` | `analysis/gate_b.json`, `analysis/precision_s_row.json` |
| Cross-workload selector (P1), within-download controller (P2) | `.../adaptive/` | `analysis/adaptive_b.json` |
| Six-workload study (W1-W6) | `.../w1_w6_grid/`, `.../w1_w6_adaptive/` | `analysis/w1_w6_*.json` |
| Recorded-trace replay oracle gate | `testdata/final_oracle_analysis/` | `final_statistical_gate.json` |
| Learned-policy head-to-heads | `testdata/final_evaluation{,_v5,_v6}/raw_results*.json` | `statistical_analysis.json` |
| D2 pacing-schedule sensitivity (screening) | `testdata/d2_pacing_sweep/runs/` | `analysis/sweep_results.{json,txt}` |

## 4. Re-running live experiments

Live drivers write many files and take minutes to hours. Most drivers under
`testdata/*/tools/` have the checkout path `/workspaces/mvp` written into them;
check the repository out at that path (or create a symlink there) before running
them. The sweep driver added with the paper (`testdata/d2_pacing_sweep/tools/`)
uses relative paths.

D2 pacing-schedule sweep (about 30-40 minutes):

```
go build -o testdata/d2_pacing_sweep/tools/sweep_run ./testdata/d2_pacing_sweep/tools/
python3 testdata/d2_pacing_sweep/tools/run_sweep.py      # refuses if manifest hash changed
python3 testdata/d2_pacing_sweep/tools/analyze_sweep.py
```

The run order, schedules, exclusion rules and analysis are fixed in
`testdata/d2_pacing_sweep/manifest/sweep_manifest.json` (SHA-256 in
`sweep_manifest.sha256`). The driver is resumable and never re-runs a completed
cell.

D2 benchmark and the Track 2-B drivers: `testdata/aria2_competitive_benchmark/tools/run_benchmark.py`
and `testdata/n_dependent_server_joint_ns/tools/{run_gate_b,run_adaptive_b,
run_w1_w6_grid,run_w1_w6_adaptive}.py`; build the Go tool next to each driver
first (`bench_run`, `ns_bench`).

Wall-clock results vary with machine load. Run nothing else CPU-heavy while a
timing experiment runs, and expect small absolute differences; ratios between
systems measured in the same block are the comparable quantity.

## 5. What the evidence does and does not support

See `reports/paper_draft.md` (claim-to-evidence map) and
`docs/PREREGISTRATION_NOTE.md`. In short: results are from a controlled
single-server emulation with a request-start-rate server model; recorded
cellular traces are replayed, not measured live; protocols are predeclared with
recorded hashes but not independently timestamped.

## 6. Notes for readers of the source

Some source comments and evidence reports refer to an internal project-context
document that is not part of this release. Everything needed to reproduce and
interpret the results is in this guide, the manifests under `testdata/*/manifest`
and the claim map in the paper draft.

## 7. Data

The raw archive is large (hundreds of MB). It is distributed separately with a
DOI and `SHA256SUMS`; verify with `sha256sum -c SHA256SUMS`. Data terms are in
`DATA_LICENSE.md`.
