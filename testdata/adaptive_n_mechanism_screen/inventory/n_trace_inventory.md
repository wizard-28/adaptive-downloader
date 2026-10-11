# Trace inventory for the adaptive-N screen (offline inspection only)

| trace kind | source | represents | can it establish a concurrency mechanism? |
|---|---|---|---|
| Native Mahimahi delivery-opportunity traces (8) | `/usr/local/share/mahimahi/traces` | time-varying **aggregate downstream capacity** (packet opportunity schedules; mm-link loops at EOF) | **No.** Aggregate capacity alone does not contain per-flow fairness, queueing, or per-connection cost information. It can only drive a shared-capacity model, whose preferred N is monotone (N=16). |
| Throughput-trajectory traces (23) | NYU-METS / UGent (`testdata/external/mobile_traces/raw`) | measured end-to-end **throughput**, not path capacity | **No.** M3 showed an aggregate throughput envelope imposes the same ceiling on every concurrency level (no C sensitivity). |
| Synthetic/simulator states | this screen | server capacity C, parallelism cap K, per-connection overhead gamma, background flows B | Analytical only; not traces. |

**Conclusion:** no existing trace in the repository contains sufficient information to infer
concurrency sensitivity (per-flow fairness, queueing, server parallelism, or per-connection
cost). The mechanism must come from the server/system model, and the current controlled
server has no concurrency-dependent cost (T1, L4 GATE E). The full S-pivot inventory with
per-trace statistics is at `testdata/salvage_s_mechanism_pivot/inventory/trace_inventory.md`.
