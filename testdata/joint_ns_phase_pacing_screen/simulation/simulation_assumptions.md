# Simulation assumptions (joint N+S phase-locked pacing screen)

**Analytical simulation only.** No network experiment was run: no downloads, no
server/client processes, no tc/netem, no replay. All numbers are deterministic
simulator output, not empirical evidence. The simulator is calibrated against
the measured D2 benchmark evidence and validated on held-out anchors.

## Simulator

`tools/jns_sim.py` — deterministic event-driven finite-file downloader model.
No RNG. State: N workers, each with at most one contiguous range request.

- **Request granularity:** one contiguous HTTP range request per worker; the
  size is the future-only chunk size `S` at allocation time. An in-flight
  request is never resized or terminated (changing `S` affects future
  allocations only).
- **Server pacing:** the rate is sampled at request start (server clock) and
  held for the whole request. Server duration
  `L/rate + c * ceil(L/16 KiB)` where `c` is the per-16 KiB write/sleep
  overhead measured in the benchmark.
- **Client cycle:** a worker's next request reaches the server `h` seconds
  after the previous request's server end (up + down latency + processing).
  The first request of a run reaches the server after `init` seconds.
  Completion is measured client-side: last server end + `down`.
- **Timeout semantics (frozen downloader config):** if the client-observed
  attempt duration would exceed 30 s, the attempt is aborted; the range is
  retried with its original boundaries (`retry_gap` after the abort). After
  `max_attempts = 3` failed attempts the run fails (the range fails and the
  download aborts). This matters for 32 MiB requests at the slow rate.
- **N changes:** increases add workers that allocate immediately; decreases
  retire workers between requests. This matches `workerPool.SetTarget`
  (in-flight requests are never killed).
- **Controllers** observe only completed-request information available before
  the next allocation: observed throughput samples, completed bytes, current
  N/S, time. No schedule, regime, transition time, or future information.

## Calibration (measured D2 evidence)

Fitted on the 40 fixed-policy configurations of
`testdata/aria2_competitive_benchmark/discovery_ours/` (exact D2 schedule,
netem 40 ms + 0.4%):

| parameter | value |
|---|---|
| `h` (inter-request gap) | 0.088 s |
| `c` (per-16 KiB server overhead) | 0.0006 s |
| `init` (first request delay) | 0.150 s |
| `down` (final-byte latency) | 0.060 s |
| `retry_gap` | 0.100 s |
| timeout / max attempts | 30 s / 3 (frozen tool config) |

Fit quality: **relative RMSE 1.40 %**, max absolute relative error **2.99 %**
over all 40 configurations. Held-out anchors: the aria2 32 MiB-piece run
(observed median 53.32 s, predicted 52.89 s, −0.8 %); the measured D2 best
fixed (16, 4 MiB) = 19.99 s, predicted 19.92 s; measured (16, 1 MiB) = 25.60 s,
predicted 25.65 s; measured (16, 16 MiB) = 34.55 s, predicted 33.68 s;
measured (16, 32 MiB) = 45.97 s, predicted 44.60 s.

The 32 MiB configurations are shaped by the 30 s timeout + retry; they are
reproduced within 3 % but are excluded from the fit target (the fit uses
`S <= 16 MiB`).

## Limitations

- The model is deterministic and symmetric: it does not model netem loss
  retransmits, client scheduling jitter, or server CPU contention. These are
  small in the measured evidence (the D2 frontier fit is within 3 %).
- The regime family is a two-phase slow->fast abstraction of the measured D2
  mechanism; the exact D2 3-phase schedule is validated separately.
- Controller results are conditional on the frozen action space, the frozen
  starting configuration (4, 4 MiB), and the predeclared controller families.
