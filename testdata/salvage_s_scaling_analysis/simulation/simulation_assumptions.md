# Simulation assumptions (Salvage Protocol v7 scaling analysis)

The deterministic finite-file scheduler simulator is `tools/scaling_analysis.py:simulate()`.
It performs no network I/O and uses no RNG.

- N = 4 work-conserving workers; one contiguous range per request; ranges allocated in order.
- Chunk-size target affects future allocations only; in-flight ranges are never resized.
- B_pending = TotalSize - CompletedBytes - InFlightBytes is sampled from tracker-level state
  immediately before each allocation decision; at most one switch; after the switch the target
  is fixed for the remainder of the run.
- The trigger is not evaluated when there is no allocation decision (no artificial switch at EOF).
- The final partial range is supported (service time derived from the same request-level model).
- Deterministic tie-breaking by allocation sequence; no failures/retries are modeled
  (Stage 0S had 0 failures and 0 retries).
- Service models (never blended): MODEL A `t(s) = D + s/R`; MODEL B `t(s) = a + b*s`
  (OLS fit to the four frozen Stage 0S pooled median service times; see
  `model/model_definition.json`).
