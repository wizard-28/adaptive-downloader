# Frozen DQN agent on the capped server: does RL win where adaptation has room? (protocol)

Frozen before the first data run. Amendments go at the end, dated, and never
edit the sections above them.

## Why

Earlier DQN head-to-heads were run on short files (4–16 MiB). The agent started
at one connection while aria2 opened 16. It was never tested on the capped
server, where the best setting changes during a download. This experiment
removes those three handicaps. It does not retrain the agent: it tests the
frozen v6 candidate as built.

## Policy under test

The frozen DQN v6 candidate is
`testdata/diagnostics/dqn_validation_v6/final_dqn_candidate_v6.json`, run through
`dqn.NewLiveConcurrencyPolicyV6`.

- It adapts concurrency only, within N 1–16, with S fixed at 1 MiB, as trained.
- Its policy window is 4 attempts, as in its own evaluation.
- It runs on the fixed engine: no whole-request deadline, straggler splitting
  on. Every arm uses the same engine.
- There are two start points:
  - `dqn6_start1` starts at N=1, as it was trained and evaluated;
  - `dqn6_start16` starts at N=16, matching aria2's opening. N=16 is outside
    the start states it saw in training.

## Design

- **Harness:** `tools/rl_bench.go`, a copy of the online-tuner harness with an
  additive `-policy dqn6`. The server model, cap (C = 8 MiB/s, γ = 0.05), t1
  schedule, 512 MiB file, netem (40 ms each way, 0.4 % loss) and aria2 flags
  are unchanged.
- **Scenarios:** CAP8 (the cap switches on at 8 s) and HEALTHY (no cap).
- **Arms:**
  - `dqn6_start16`
  - `dqn6_start1`
  - `fixed_16_1M_NT_split`, the same range size as the agent;
  - `fixed_16_32M_NT_split`, the best fixed setting under the cap in the engine
    ablation;
  - `aria2`.
- **Blocks:** 5 blocks per scenario, with arms shuffled within each block
  (seed 20261026). This is screening scale, with 50 runs in total.
- **Validity rules:** as in the split ablation. Invalid runs are kept and
  reported.

## Predeclared comparisons (`tools/analyze_rl_capped.py`)

All comparisons are paired by block. Each gives the geometric-mean ratio with a
95 % percentile bootstrap (10,000 resamples).

- Each DQN arm against each fixed arm, and against the better of the two
  (chosen after the fact).
- Each DQN arm against aria2.
- Descriptive only: final N and number of moves.

## Interpretation rules (fixed in advance)

- A ratio is "faster" if the CI upper bound is below 1, "slower" if the lower
  bound is above 1, and "not distinguishable" otherwise.
- With 5 blocks, this is a screen. Any result is reported as screening
  evidence.
- If neither DQN arm is faster than the best fixed arm in CAP8, the reports
  must say: the frozen agent did not beat a well-chosen fixed setting even
  with a fair start, a long file and the scenario built so that the best
  setting changes.
- If a DQN arm is faster in CAP8, the reports must say so, and must say it is
  a 5-block screen of a frozen agent.
