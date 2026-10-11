# Simulation assumptions (adaptive-S mechanism pivot)

The deterministic event-driven finite-file scheduler simulator is
`tools/mechanism_sim.py:simulate()`. No network I/O; no RNG (the pseudo-random
volatility profile uses a fixed LCG seed 12345).

- N=4 work-conserving workers; one contiguous range per request; allocation in order.
- Future-only chunk size; in-flight requests are never resized; request completion depends on
  the capacity experienced throughout the request (piecewise-constant profiles).
- B_pending is not needed here: the screening is fixed-S and single-switch; the switch
  policies observe completed-request throughput samples only.
- Traces: 1 s aggregate capacity bins; zero-opportunity bins use the documented 8000 bps M3
  outage floor so a locked per-request rate is never exactly zero.
- Models: shared (fair sharing), lockin_full (per-request rate fixed at request start and
  held), serialized, constant (v7 control).
- The oracle is a clairvoyant diagnostic upper bound (grid search over start arm, switch
  time, target arm); it is not a policy.
