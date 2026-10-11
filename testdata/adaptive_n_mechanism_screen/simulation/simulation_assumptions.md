# Simulation assumptions (adaptive-N mechanism screen)

The deterministic event-driven finite-file scheduler simulator is `tools/n_sim.py:simulate()`.
No network I/O; no RNG.

- N workers; one contiguous 1 MiB request per worker; F = 512 MiB; per-request RTT 40 ms.
- Request transfer rates depend on the CURRENT number of active requests and the CURRENT
  environmental state, so in-flight requests experience state evolution throughout their
  lifetime (no start-of-request approximation for capacity).
- N changes are applied at completion events: increases add workers that allocate immediately;
  decreases retire workers, which finish their in-flight request and then stop (in-flight
  requests are never resized or terminated).
- Models: direct, fair, fair_bg, cap, degrade (see model/model_definition.json).
- One switch per download; causal policies use only observed completed-request throughput
  (median of the last 3 samples). The oracle is a coarse-grid clairvoyant diagnostic, not a
  policy.
- The simulator output is analytical, not empirical evidence.
