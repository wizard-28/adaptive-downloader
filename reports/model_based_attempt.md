# Bounded model-based controller attempt: negative result (stopped at the feasibility gate)

**Outcome: the predeclared identifiability gate failed (0 of 6 criteria). No live
evaluation was run. Phase stopped by rule; nothing was tuned afterwards.**

Controller considered: *online system identification followed by a single
within-download joint (N,S) selection* (one probe window, one decision, no
re-estimation). It is not continuous adaptation, online tracking, or RL.

| item | value |
|---|---|
| branch | `exp/model-based-ns` (from tag `paper-rc1`), not merged |
| gate protocol (committed before code) | commit `b22ad8b`, `testdata/model_based_ns/manifest/gate_protocol.json` |
| single amendment A1 (committed before the evidence run) | commit `35f2af5`, `gate_protocol_A1.json` |
| evidence run | 560 simulated rows, commit `4b625c4`, `analysis/gate_results.json` |
| result | `testdata/model_based_ns/FEASIBILITY_RESULT.md` (on the branch) |

Why it stopped, in short: median regret against the grid oracle was 0.54 (bar
0.05), median predicted-time error 0.46 (bar 0.10), total cost including probing
1.77x oracle (bar 1.25x), the estimator fell back to the healthy configuration in
64 % of rows (bar 30 %), and an ablation using the *true* server concurrency also
failed, so client-side uncertainty in the concurrency was not the sole cause.
The cause was not isolated.

Scope: simulation of the soft-cap mechanism with the frozen platform constants
and a noise model calibrated on live per-request logs. It does not show that no
estimator could succeed and says nothing about live behaviour. The submission
candidate (`paper-rc1`) does not depend on this experiment.

Preservation: the A0 smoke run (a failure that led to amendment A1) and the A1
evidence run are both kept on the branch.
