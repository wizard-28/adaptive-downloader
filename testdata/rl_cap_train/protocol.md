# DQN retrained for the capped server (protocol)

Frozen 2026-10-25, before the prospective parity runs, before any training run
whose result is evaluated, and before any live evaluation run. Amendments go at
the end, dated, and never edit the sections above them.

## Why

`testdata/rl_capped` ran the frozen DQN v6 agent on the capped server: it was
slower than the best fixed setting (CAP8 1.356, HEALTHY 1.031). That agent was
trained on a different simulator (`python/rl/sim_env_v6.py`), which differs
from the live setup in ways that matter:

- It has no soft concurrency cap with a rate fixed at request arrival.
- Its aggregate-throughput observable is C·n/svc. The live
  `policyRunner.aggregateThroughput` is the bytes of the last `-window`
  attempts over their wall-clock span. With `-window 4` that observable
  saturates near N = 4 (A0 below).

This study retrains the same learner, with the same action space, state layout
and Go adapter, on a simulator of the capped server that reproduces the live
runner. It is checked against live runs before the trained agent is evaluated
live.

## What stays fixed

- **Learner:** Double DQN (`python/rl/dqn.py`), MLP 64×64, 3 actions
  (none / ±1 concurrency), N in 1–16, range size fixed at 1 MiB.
- **State:** layout v3, the same 13 features as v6. The Go
  `dqn.NewLiveConcurrencyPolicyV6` loads the new model unchanged. Only the
  normalization scales in the model's state spec differ: throughput 32 MiB/s,
  duration 4 s, staleness 10 s. They are carried in the model file.
- **Live harness:** `testdata/rl_capped/tools/rl_bench.go`, unchanged, with
  `-policy dqn6 -dqn-model <new model> -window 16`. Engine: no whole-request
  deadline, straggler splitting on (inert at 1 MiB: a split needs 2 MiB
  unreceived).
- **Server model:** t1 schedule, cap C = 8 MiB/s, γ = 0.05, 512 MiB, netem
  40 ms each way, 0.4 % loss. Frozen aria2 flags.

## Stage A: simulator and its check against live

**Simulator.** `python/rl/sim_env_cap.py`, an event-driven model of the
server, worker pool and policy runner:

- The rate is fixed at arrival, with k counting only the capped requests in
  service.
- An increase starts a worker at once; a decrease retires the next worker
  that completes.
- There is one decision per completed attempt.
- The observables are built exactly as in `policyRunner.observe` and
  `LivePolicy.observeV5`.

**Calibration.** `tools/calibrate.py` reads `calibration.json`, which is built
only from per-request timing components in the rl_capped request logs:

- server excess = server time − bytes/rate;
- client extra = client duration − server time;

each split by rate class. Completion times are not used. The simulator draws
both per request from these distributions.

**A0. Observability, done before freezing.** Mean observables at fixed N in
the simulator, steady state:

| Window | HEALTHY aggregate (MiB/s), N = 1 / 4 / 16 | CAP0 request duration (s), N = 1 / 4 / 16 |
|---|---|---|
| 4 | 1.9 / 4.4 / 6.3 | 0.53 / 0.69 / 3.56 |
| 16 | 1.7 / 6.2 / 14.8 | 0.53 / 0.69 / 3.56 |

With window 16 the aggregate resolves N above 4, and the request duration
exposes the cap. The new agent uses window 16 in both training and live runs.

**A1. Retrospective check against rl_capped (already seen, so not blind).**
The simulator mean is the geometric mean (gm) over 20 seeds; the live value is
the 5-run geometric mean.

| Arm | Simulator (s) | Live (s) |
|---|---|---|
| CAP8, fixed 16×1 MiB | 77.7 | 76.6 |
| HEALTHY, fixed 16×1 MiB | 18.8 | 18.4 |
| CAP8, v6 closed loop, start 1 (window 4) | 88.1 | 89.7 |
| CAP8, v6 closed loop, start 16 (window 4) | 79.7 (range 66.4–92.2) | 71.3 (range 66.6–84.1) |
| HEALTHY, v6 closed loop, start 1 | 21.4 | 22.0 |
| HEALTHY, v6 closed loop, start 16 | 19.6 | 19.0 |

**A2. Prospective check (blind).** These are new live runs of fixed N at
1 MiB, 2 repetitions each, with predictions written here first: 20-seed
geometric mean, with the 5th–95th percentile in brackets.

| Run | Predicted (s) |
|---|---|
| CAP8, N = 3 | 93.76 [92.02, 95.22] |
| CAP8, N = 8 | 82.01 [80.80, 83.12] |
| CAP0, N = 3 | 93.78 [92.03, 95.23] |
| CAP0, N = 16 | 114.69 [114.18, 115.19] |

**Pass rule.**
- The live geometric mean of each run must be within ±8 % of its prediction.
- The live ordering must match the simulator's: CAP0 N = 3 faster than
  N = 16, and CAP8 N = 8 faster than N = 3.
- **If A2 fails:** stop before live evaluation. Record the failure, diagnose
  it, amend the simulator with a dated amendment, and run a fresh A2 set.
  Trained models are not evaluated live on a failed simulator.

## Stage B: training and selection (simulator only)

- **Script:** `python/train_cap.py`.
- **Seeds:** 0, 1, 2, 3, 4.
- **Per seed:**
  - 1,000 episodes;
  - γ = 0.99 (one step per completed MiB, so discounting is per byte);
  - learning rate 5e-4, replay 100,000, batch 64;
  - ε from 1.0 to 0.05 over 150,000 steps;
  - target sync every 1,000 steps, training starts at 2,000 steps.
- **Training conditions are sampled per episode:**
  - file 128–512 MiB;
  - initial rate 320–1280 KiB/s, switching at 0.5–3 s to 1500–4000 KiB/s;
  - cap: none (25 %), from 0 s (15 %), or at 2–30 s (60 %);
  - C 4–12 MiB/s, γ 0.02–0.10, one-way delay 20–60 ms;
  - start N uniform in 1–16;
  - calibrated jitter on.
- **Validation (selection only):** 8 simulator conditions that differ from
  the live ones.
  - File 384 MiB; rate 512 KiB/s, then 3 MiB/s from 1.5 s.
  - Cap at none, 0 s, 5 s or 15 s, with C = 6 or 10 MiB/s and γ = 0.05.
  - Each condition is run with start N = 1 and N = 16, 3 seeds each.
  - **Score:** geometric mean of (agent time ÷ best fixed N at 1 MiB for that
    condition).
  - The seed with the lowest score is selected; ties go to the lowest seed.
    Test conditions are never used for selection.
- **Simulator test (reported, not used for selection):**
  - live CAP8, HEALTHY and CAP0 conditions, start N = 1 and N = 16, 10 seeds;
  - compared against the best fixed N at 1 MiB, the 16→3 switch oracle
    (CAP8: 64.0 s) and v6.
- **Simulator gates (reported; the live evaluation runs whatever the
  outcome, so a gate never hides a live result):**
  - G1: CAP8, start 16, at least 3 % faster than the best fixed N at 1 MiB.
  - G2: HEALTHY, start 16, no more than 5 % slower than the best fixed N at
    1 MiB.
  - G3: the policy changes N in CAP8, so it is not a constant policy.
- **Export:**
  - `model/dqn_cap_candidate.json` plus its sha256;
  - `model/parity_fixture.json`, raw observations with the Python action.
    A Go test checks that `Model.SelectActionV6` returns the same action for
    every one.

## Stage C: live evaluation

- **Scenarios:** CAP8 and HEALTHY.
- **Blocks:** 10 per scenario, with arms shuffled within each block (seed
  20261025).
- **Arms:**
  - `dqnc_start16`: new model, `-n 16 -window 16`;
  - `dqnc_start1`: new model, `-n 1 -window 16`;
  - `dqn6_start16`: frozen v6, `-n 16 -window 4`, exactly as in rl_capped;
  - `fixed_16_1M_NT_split`: the best fixed N at 1 MiB in the simulator for
    both scenarios;
  - `fixed_16_32M_NT_split`;
  - `aria2`.
- **Total:** 120 runs.
- **Validity rules:** as in the split ablation. Invalid runs are kept and
  reported, never re-run silently.

**Predeclared comparisons (`tools/analyze.py`).** All are paired by block,
with the geometric-mean ratio and a 95 % percentile bootstrap (10,000
resamples, seed 1).

- **Primary:** `dqnc_start16` against `fixed_16_1M_NT_split` in CAP8.
- **Secondary:**
  - every dqnc arm against `fixed_16_1M_NT_split`, `fixed_16_32M_NT_split`,
    the better fixed arm (chosen after the fact) and aria2, in both
    scenarios;
  - `dqnc_start16` against `dqn6_start16`, to test whether retraining helped.
- **Descriptive:** final N, number of moves, and N at 15 s.

**Interpretation rules, fixed in advance.**

- A ratio counts as:
  - "faster" if its CI upper bound is below 1;
  - "slower" if its lower bound is above 1;
  - "not distinguishable" otherwise.
- **If the primary comparison is faster,** the reports say the retrained agent
  beat the best fixed setting with the same range size on the capped server.
  The same sentence also gives its result against 16×32 MiB and aria2.
- **If it is not faster,** the reports say retraining on a checked
  capped-server simulator did not give a live win over the best fixed setting
  with the same range size.
- **16×32 MiB under CAP8:** it wins because its 16 ranges all start before the
  cap and keep the pre-cap rate. A concurrency-only agent at 1 MiB cannot do
  that. Its simulated best case, the switch oracle at 64.0 s, is slower than
  16×32 MiB live (52.6 s). Losing to 16×32 MiB is therefore expected, and the
  reports must state this reason.
- 10 blocks of one trained candidate is still a small study. Results are
  stated for this server model only.

## Stage D: generalization to held-out conditions

**Why this stage.** CAP8 and HEALTHY lie inside the training distribution, so
a Stage C win would show only that the agent learned this server model, not
that it generalizes. Stage D tests conditions outside the training ranges,
one axis at a time. They use the same harness flags with no code change.
Simulator predictions were computed before freezing, with 10 seeds per
setting.

| Scenario | Change from CAP8 | Outside training because | Best fixed N at 1 MiB (sim) | 16 → n oracle at 8 s (sim) |
|---|---|---|---|---|
| G_C16 | cap C = 16 MiB/s | C trained 4–12 | N = 16, 42.9 s | 16 → 7, 36.4 s |
| G_RTT | netem 80 ms each way | one-way delay trained 20–60 ms | N = 16, 83.1 s | 16 → 5, 67.2 s |
| G_D2 | d2 schedule: 640 → 160 KiB/s at 2 s → 2500 KiB/s at 4 s | three-phase schedule never trained | N = 4, 95.1 s (N = 16: 113.9 s) | 16 → 4, 89.1 s |

Each scenario needs a different post-cap N from CAP8 (3). A fixed rule
learned for CAP8 therefore does not transfer automatically.

- **Arms:**
  - `dqnc_start16`, `dqn6_start16`, `fixed_16_1M_NT_split` and
    `fixed_16_32M_NT_split`, in every G scenario;
  - `fixed_4_1M` in addition in G_D2.
- **Blocks:** 5 per scenario, shuffled (seed 20261027). That is 80 runs, a
  screen.
- **Simulator generalization test (Stage B, reported):** 20 conditions from a
  distribution outside training:
  - C 14–24 MiB/s, γ 0.12–0.20, one-way delay 70–100 ms;
  - a three-phase schedule with a dip.
  It reports the geometric-mean ratio of agent time to the best fixed N at
  1 MiB for each condition.
- **Comparisons:** `dqnc_start16` against the best fixed 1 MiB arm in that
  scenario (chosen after the fact), against 16×32 MiB and against
  `dqn6_start16`, using the same bootstrap and CI rule.

## What a result can and cannot show (fixed in advance)

- **A win in Stage C alone:** "the retrained agent beat the best fixed setting
  with the same range size on the server model it was trained for." Never
  "RL is better" or a general claim.
- **Faster than the best fixed 1 MiB arm in k of the 3 G scenarios:** the
  reports say it generalized to k of 3 held-out conditions of this server
  family, and name which ones. Only k = 3 may be described as generalizing
  within the family.
- **Nothing here supports superiority** on real servers, other cap
  mechanisms, or other downloaders. The reports say so in the Limitations
  section.

## Amendment A1 (2026-10-25, during Stage D, no design change): run count

Stage D's run total was misstated as "80 runs". The arms and blocks as
specified (G_C16 4 arms, G_RTT 4, G_D2 5, × 5 blocks) give 65 runs. That is
what the manifest schedules and what was run. No arm, block or scenario was
changed.
