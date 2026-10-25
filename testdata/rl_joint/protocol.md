# Joint concurrency and range-size DQN on the capped server, with a live-rate control (protocol)

Frozen 2026-10-25, before the blind parity runs, before any training run
whose result is evaluated, and before any live evaluation run. Amendments go
at the end, dated, and never edit the sections above them.

## Why

The concurrency-only retrain (`testdata/rl_cap_train`) cannot reach
16×32 MiB under the cap, because its range size is fixed at 1 MiB. The joint
simulator (`python/rl/sim_env_joint.py`) shows large headroom for a policy
that also sets the range size. A scripted rule that uses 1 MiB ranges until
the rate rises, then grows them to 33 MiB, scores 0.352 of the best fixed
time under CAP8 in the simulator.

**That headroom comes mainly from the server's arrival-time rate rule:** a
request keeps the rate in force when it arrives. Large ranges allocated
after the rate rises and before the cap keep the fast rate. This study tests
whether a learned agent finds that behaviour live. A control stage on a
server variant that re-checks rates during each response (live-rate mode)
measures how much of any gain survives without the rule.

## What is fixed

- **Learner:** the Double DQN of `python/rl/dqn.py` (MLP 64×64) with the
  5-action joint space: none, ±1 concurrency, ±range size.
  - N runs from 1 to 16.
  - The range size runs from 1 to 33 MiB in additive 4 MiB steps
    (1, 5, …, 33). The 1 MiB floor avoids the v2 fragmentation failure.
  - γ = 1.0, because the return is minus the total completion time. Each
    decision covers a different number of bytes, so per-decision discounting
    would favour large ranges.
  - The reward is −Δt/10 per decision. The scaling keeps |Q| near 10 and
    does not change the greedy action.
  - Other hyperparameters are as in `rl_cap_train`: 1,000 episodes per seed,
    learning rate 5e-4, replay 100,000, batch 64, ε from 1.0 to 0.05 over
    150,000 steps, target sync every 1,000 steps, training starts at 2,000.
  - The seeds are 0–4.
- **State:** layout v1, 13 features including the range size and the time
  since the last range-size change.
  - The scales are: N 1–16, range size 1–33 MiB, throughput 32 MiB/s,
    duration 8 s, staleness 10 s.
  - The Go `dqn.NewLivePolicy` loads it. Policy window: 16 attempts.
- **Simulator:** `sim_env_joint`, which uses the `sim_env_cap` server model
  and calibration and adds variable range size and straggler splitting
  (≥ 2 MiB unreceived).
  - With ranges fixed at 1 MiB it equals `sim_env_cap` exactly; a test
    checks this.
  - A retrospective check against the live split ablation (8
    configurations) came within 5.3 %. Long ranges come out up to 5 % fast.
- **Harness:** `tools/joint_bench.go`. It is `rl_bench` plus `-policy dqnj`
  and `-live-rate`, which turns on the server's opt-in live-rate mode
  (`Server.SetLiveRate`). The server model, cap (C = 8 MiB/s, γ = 0.05), t1
  and d2 schedules, 512 MiB file, netem (40 ms each way, 0.4 % loss), engine
  (no deadline, splitting on) and frozen aria2 flags are unchanged.

## Stage A2: blind live parity (new fixed configurations)

Two repetitions of each. The predictions are 20-seed simulator geometric
means, with the 5th–95th percentiles in brackets:

| Configuration | Predicted (s) |
|---|---|
| CAP8, 16×9 MiB | 81.69 [81.65, 81.77] |
| CAP0, 8×17 MiB | 80.04 [79.87, 81.20] |
| D2, 16×5 MiB | 20.30 [20.13, 20.71] |
| HEALTHY, 16×17 MiB | 34.13 [33.86, 34.69] |

- **Pass:** every live geometric mean is within ±8 % of its prediction.
- **If A2 fails:** stop, record the failure, amend the simulator and rerun
  A2 with fresh runs. Live evaluation waits until A2 passes.

## Stage B: training and selection (simulator only)

- **Script:** `python/train_joint.py`.
- **Training conditions:** as in `rl_cap_train`, with the start N uniform in
  1–16 and the start range size uniform on the grid.
- **Selection:** on the 8 `rl_cap_train` validation conditions, starting at
  16 connections and 1 MiB. The score is the geometric mean of agent time ÷
  best fixed (N, S) time. The grid is N ∈ {4, 8, 16} × range size ∈ {1, 5,
  9, 13, 17, 25, 33} MiB.
- **Simulator test (reported only):** CAP8, HEALTHY, CAP0, D2, G_C16, G_RTT
  and G_D2, plus 20 out-of-distribution conditions.
- **Gates (reported; live evaluation runs whatever the outcome):**
  - J1: CAP8 agent ≤ 0.97 of the best fixed;
  - J2: HEALTHY agent ≤ 1.05 of the best fixed;
  - J3: the agent changes the range size in CAP8.
- **Before Stage C,** the simulator predictions for every Stage C and
  Stage L arm on the arrival-rule scenarios are written into an amendment.
  The live-rate scenarios have no simulator prediction.
- **Go parity:** `internal/rl/dqn/joint_candidate_parity_test.go` must pass:
  the Go features and actions must equal Python's on the fixture.

## Stage C: live evaluation on the arrival-rule server (10 blocks)

The agent `dqnj` starts at N = 16 and 1 MiB, with window 16. The other arms
are fixed 16×1 MiB and 16×32 MiB in every scenario, plus aria2 in CAP8 and
HEALTHY, and 16×5 MiB in D2 (the simulator's best fixed setting there).

| Scenario | Arms |
|---|---|
| CAP8 | dqnj, 16×1 MiB, 16×32 MiB, aria2 |
| HEALTHY | dqnj, 16×1 MiB, 16×32 MiB, aria2 |
| CAP0 | dqnj, 16×1 MiB, 16×32 MiB |
| D2 | dqnj, 16×1 MiB, 16×32 MiB, 16×5 MiB |

That is 150 runs, with arms shuffled within each block (seed 20261026).

## Stage L: control on the live-rate server (5 blocks)

| Scenario | Arms |
|---|---|
| CAP8L (cap from 8 s, live rate) | dqnj, 16×1 MiB, 16×32 MiB, 4×4 MiB |
| HEALTHYL (no cap, live rate) | dqnj, 16×1 MiB, 16×32 MiB, 4×4 MiB |

That is 40 runs (seed 20261027). The agent is the same frozen model, not
retrained for this server.

## Predeclared comparisons (`tools/analyze.py`)

All comparisons are paired by block, using the geometric-mean ratio with a
95 % percentile bootstrap (10,000 resamples, seed 1).

- **Primary:** CAP8 `dqnj` against the better of the fixed arms (chosen
  after the fact).
- **Secondary:**
  - every scenario, `dqnj` against each fixed arm and against the best fixed
    arm;
  - `dqnj` against aria2 in CAP8 and HEALTHY.
- **Control:** CAP8L and HEALTHYL, `dqnj` against the best fixed arm on the
  live-rate server.
- **Descriptive:**
  - the agent's final N and range size, its number of moves, and its range
    size at 8 s;
  - the share of bytes served at a pre-cap rate in CAP8 and CAP8L (from the
    server request logs).

## Interpretation rules (fixed in advance)

- The CI rule from the other studies applies: "faster" if the CI upper bound
  is below 1, "slower" if the lower bound is above 1, "not distinguishable"
  otherwise.
- **If the primary comparison is faster:** "on the arrival-rule server model
  it was trained for, the joint agent beat the best fixed setting". The same
  sentence must give the CAP8L control result:
  - if the control is faster too: "and still did so on a server that
    re-checks rates during a response";
  - otherwise: "but not on a server that re-checks rates during a response,
    so the gain depends on the arrival-time rule".
- **If the primary comparison is not faster:** "a joint agent trained on a
  checked simulator did not beat the best fixed setting live".
- Any win over aria2 is reported in the same frame: a mechanism of the
  server model, not general superiority.
- This is one trained candidate and one server family. Nothing here supports
  claims about real servers.

## Amendment A1 (2026-10-25, before any Stage C or L run): training budget

**Problem.** The Stage B budget was stated in episodes (1,000 per seed). That
figure was carried over from `rl_cap_train`, whose 1 MiB episodes have about
320 decisions. Joint episodes with large ranges have about 40. The first
training run therefore gave each seed only 39,368–43,130 steps, while ε
decays over 150,000 steps. Training ended with ε near 0.75, and |Q| ≤ 0.82
against returns near −5. By a criterion that does not depend on
performance, an incomplete ε schedule, that candidate is undertrained.

**Disclosure.** Its simulator test results were seen before this amendment.
In CAP8 it scored 1.022 of the best fixed time, and gates J1 and J2 failed.
The decision here rests only on the ε-schedule criterion. Its outputs are
kept unchanged in `train_A0/` and `model_A0/`, and it is not evaluated live.

**Change.** Retrain with `--episodes 11000`, about 450,000 steps per seed, so
the ε schedule completes three times over. The actual step counts are
reported. Everything else in Stage B is unchanged, including the seeds,
selection, test sets and gates. The A2 parity result stands.

## Amendment A2 (2026-10-25, after Stage B, before any Stage C or L run): predictions and Stage B outcome

**Stage B, retrained under A1.** Each seed ran 747,503–823,821 steps, so the
ε schedule completed. Seed 0 was selected on validation. Its sha256 prefix is
c421d5358c8c, and the Go parity test passes.

Simulator test, agent ÷ best fixed (N, S):

| Scenario | Ratio |
|---|---|
| CAP8 | 1.513 |
| HEALTHY | 1.070 |
| CAP0 | 1.333 |
| D2 | 1.375 |
| G_C16 | 1.002 |
| G_RTT | 1.458 |
| G_D2 | 1.845 |
| Out-of-distribution | 2.19, faster in 0 of 20 |

Gates J1 and J2 fail and J3 passes. As frozen, the live stages run
regardless of the gates.

**Predictions for Stage C:** simulator geometric means over seeds 100–109,
in seconds (`predictions_C.json`).

| Scenario | dqnj | 16×1 MiB | 16×32 MiB | 16×5 MiB |
|---|---|---|---|---|
| CAP8 | 73.30 | 77.43 | 51.54 | – |
| HEALTHY | 20.37 | 19.03 | 51.54 | – |
| CAP0 | 88.16 | 114.77 | 66.65 | – |
| D2 | 28.10 | 26.20 | 51.54 | 20.44 |

**Caveat from A2.** The simulator was optimistic by +1.5 % to +7.0 % in the
blind check, and more so for long ranges, so the 16×32 MiB predictions are
likely a few percent fast. There are no predictions for the live-rate
scenarios (CAP8L, HEALTHYL), because the simulator does not model live-rate
mode.
