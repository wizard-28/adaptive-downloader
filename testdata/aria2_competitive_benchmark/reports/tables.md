# Benchmark Tables

## Table 1 — D2 historical reproduction parameters

| Parameter | Value |
|---|---|
| File size | 536,870,912 B (512 MiB) |
| Server pacing phases | 640 KiB/s (0-2 s) -> 160 KiB/s (2-4 s) -> 2500 KiB/s (>=4 s) |
| Netem | 40 ms latency + 0.4% loss on lo |
| Our (historical) | frozen DQN Candidate v6, 1 MiB chunks, concurrency 1-16 |
| aria2 | 1.37.0, --max-connection-per-server=16 --split=16 --min-split-size=1M (F) |
| Pairs | 20 (fresh) |
| Order seed | 20260928 (LCG) |
| Historical result | ratio 0.5230 CI [0.5200, 0.5261], 20/20 |
| Fresh result | ratio 0.5230 CI [0.5200, 0.5261], 20/20 |

## Table 2 — aria2 configuration frontier (D2/512MiB; actual ranges recorded)

| split / max-conn | min-split | actual piece size | median completion (s) |
|---|---|---|---|
| 1 | 1M | 512 MiB | 833.2 |
| 2 | 1M | 256 MiB | 419.2 |
| 4 | 1M | 128 MiB | 210.4 |
| 8 | 1M | 64 MiB | 105.6 |
| 16 | 1M | 32 MiB | 53.3 |

## Table 3 — our configuration frontier (D2/512MiB; medians, s)

| C \ chunk | 256K | 512K | 1M | 2M | 4M | 8M | 16M | 32M |
|---|---|---|---|---|---|---|---|---|
| C=1 | 419 | 332 | 289 | 274 | 251 | 251 | 258 | 267 |
| C=2 | 212 | 167 | 147 | 142 | 127 | 129 | 137 | 148 |
| C=4 | 107 | 84 | 77 | 77 | 65 | 69 | 78 | 88 |
| C=8 | 55 | 44 | 42 | 46 | 35 | 39 | 49 | 59 |
| C=16 | 29 | 25 | 26 | 30 | 20 | 25 | 35 | 46 |

## Table 4 — frozen final configurations

| System | Configuration | Selection median (s) |
|---|---|---|
| ours | C=16, chunk 4 MiB, fixed policy | 20.30 (3-run corrected: 19.99) |
| aria2 | split=16, max-conn=16, min-split 1M | 53.33 |

## Table 5 — final paired comparison (30 fresh pairs)

| Metric | Value |
|---|---|
| ours mean (ms) | 20119.907666666666 |
| aria2 mean (ms) | 53559.5613 |
| geometric ratio | 0.3756370278940139 |
| 95% CI | [0.3727790802326555, 0.37851688629357666] |
| relative improvement | 0.6243629721059861 |
| wins | 30 |
| losses | 0 |
| permutation p (one-sided) | 4.999750012499375e-05 |

## Table 6 — mechanism diagnostics (final block)

| Metric | ours | aria2 |
|---|---|---|
| requests (30 runs) | 3840 | 450 |
| mean range | 4.0 MiB | 32.0 MiB |
| max in-flight | 16 | 15 |
| starts <2 s | 480 | 450 |
| starts >=4 s | 3360 | None |
| D2 mean (s) | 20.12 | 53.56 |
| constant-rate (2.5 MB/s) (s) | 15.33 | 24.81 |
