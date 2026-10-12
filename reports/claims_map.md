# Claim → evidence map (public)

Source of truth for each number: `tools/claims_check.py` (raw run files; paths in `tools/number_sources.json`). Labels: **Confirmed** = reproduced from raw runs at the stated n; **Descriptive** = point estimate, no inferential claim; **Failed gate** = a predeclared criterion that was not met; **Not supported** = the evidence points the other way or is absent.

| Claim | Label | Evidence |
|---|---|---|
| Fixed (16, 4 MiB) beats aria2 by 62.4 % on D2 (30/30, ratio 0.3756) | Confirmed (single workload) | 30-pair frozen benchmark; per-request logs |
| The D2 gap is partly attributable to granularity × request-start timing × pacing | Confirmed for the matched-granularity effect (0.862); **residual advantage unexplained** (constant fast pacing 0.568; NGINX constant 0.863) | granularity-matched control; constant-rate control; sweep S4; NGINX N1 |
| Ours is faster than aria2 across six pacing schedules except constant slow pacing (parity) | Descriptive (screen, n=5/schedule, 28/30 pairs) | `d2_pacing_sweep/analysis/sweep_results.json` |
| Ours is faster than aria2 against stock NGINX per-response rate limiting | Descriptive (screen, n=5/config, 10/10 pairs); predeclared parity expectation for N1 **not met** | `nginx_limit_rate/analysis/nginx_results.json` |
| No exploitable concurrency headroom on recorded cellular traces replayed | Confirmed under frozen model; Failed gate for any headroom claim | five-trace gate, LCB95 = −0.26 % vs 10 % |
| Soft cap creates a state-dependent joint optimum (live) | Descriptive; **Failed gate** (S-margin 3.74 % < 5 %) | gate_b.json pass=false; N*/S* moved, cross-state penalties material |
| Causal joint controller beats best stationary config within a download under the cap | Confirmed (10 blocks, best of 3 configs) | geo 0.766, 10/10 |
| …and beats aria2 | **Not supported** (aria2 faster, 0/10) | geo 1.096 |
| Selector matches per-workload best fixed on average | Descriptive (interval includes 1; not an equivalence test) | 0.999 [0.969, 1.029]; workload-level [0.873, 1.143] |
| Selector improves on a single family-wide fixed config | **Not established** | 0.770 point; workload-level [0.455, 1.304]; loses W5/W6 |
| Learned policies beat aria2 | **Not supported** | §9 |
