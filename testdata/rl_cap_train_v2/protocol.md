# Capped-server DQN v2: broader training for generalization, with a drop-rule arm (protocol)

Frozen 2026-10-25, before any calibration run, simulator change, training
run or live run of this study. Amendments go at the end, dated, and never
edit the sections above them. The draft was revised once before freezing,
after an independent adversarial review (held-out headroom, the drop rule's
tuning, wording coverage, blocks, stop rule).

## Why

The v1 retrain (`testdata/rl_cap_train`) beat the best fixed 1 MiB setting
on CAP8 live (0.907), but was faster in 0 of 3 held-out scenarios (Stage D).
That result stands as final. All three held-out scenarios lay outside the
training ranges: a cap of C = 16 MiB/s (trained 4–12), a one-way delay of
80 ms (trained 20–60), and a three-phase schedule (never trained). The
simulator was also calibrated at one delay (40 ms) only.

v2 asks a narrower question: **with a training distribution that covers
those axes, and timing calibrated across delay, does the agent beat the
best fixed setting on new scenarios it was not trained or selected on?**
The new held-out scenarios lie inside the broadened ranges, as new
combinations. A v2 win therefore shows generalization within a trained
envelope, not extrapolation, and the reports must say so.

A causal drop rule runs as an extra arm in every live stage. In earlier
studies, a simple rule captured the available headroom where a learner did
not (§22.91, rl_joint), so the rule is the bar a learner has to clear.

## What stays fixed (from rl_cap_train)

- **Learner:** Double DQN of `python/rl/dqn.py` (MLP 64×64). 3 actions:
  none, ±1 concurrency; N 1–16; range size fixed at 1 MiB. State layout v3
  (`cap_spec()`), 13 features. γ = 0.99, learning rate 5e-4, replay
  100,000, batch 64, ε 1.0 → 0.05 over 150,000 steps, target sync 1,000,
  training starts at 2,000. Seeds 0–4. 1,000 episodes per seed (about
  320 decisions each, as in v1, so the ε schedule completes); the actual
  step counts are reported.
- **Go adapter:** the same `-policy dqn6 -dqn-model` path as v1, with
  `-window 16`. A Go parity test on a fixture from the selected model must
  pass before any live run.
- **Server and harness:** capped server model (C, γ, arrival-time rate
  rule), 512 MiB file, netem loss 0.4 %, engine with no request deadline and
  splitting on. The live-rate server mode stays off.
- **Statistics:** paired-block geometric-mean ratio, 95 % percentile
  bootstrap (10,000 resamples, seed 1). "Faster" if the CI upper bound is
  below 1, "slower" if the lower bound is above 1, "not distinguishable"
  otherwise.

## What changes

1. **Training distribution (opt-in fields; v1 stays reproducible):**
   - cap C uniform 4–24 MiB/s (v1: 4–12), γ 0.02–0.10 (unchanged);
   - one-way delay uniform 10–120 ms (v1: 20–60), with the client start at
     2 × delay (v1 kept it at 80 ms);
   - schedules: 50 % two-phase as in v1; 25 % with an early dip (rate0 →
     dip 100–400 KiB/s at 1–4 s → rate1 1–4 s later, as in d2); 25 % with
     a late dip (two-phase, then dip 200–800 KiB/s at 5–25 s for 2–10 s,
     then back to rate1);
   - cap mode, cap time, file size, rate0, rate1 and switch time as in v1.
2. **Delay-dependent calibration.** Stage K collects request timing at
   one-way delays of 20, 80 and 120 ms. With the existing 40 ms table, the
   simulator holds 4 tables and interpolates the inverse CDFs linearly in
   delay, clamping outside 20–120 ms. This is a new opt-in option of
   `sim_env_cap`. With one table, the v1 behaviour is unchanged; a test
   checks this.
3. **Validation set (selection only):** the 8 v1 validation conditions, plus
   the same 8 at a one-way delay of 80 ms, plus 4 late-dip conditions
   (C = 6 or 10, cap at 15 s or none, dip to 400 KiB/s from 6 s to 10 s),
   20 in all. Selection score: geometric mean of agent time ÷ best fixed N
   at 1 MiB, as in v1.
4. **Harness:** `tools/v2_bench.go` is `rl_joint/tools/joint_bench.go` plus
   `-schedule custom -schedule-spec "at_s:rate,at_s:rate,..."` for
   arbitrary base-rate phases. Nothing else in the harness changes.

## Stage K: calibration runs (live)

- Fixed 16 × 1 MiB and 4 × 1 MiB, in CAP8 and HEALTHY, at one-way delays of
  20, 80 and 120 ms. 2 repetitions each: 24 runs, shuffled (seed 20261028).
- `tools/calibrate.py` extracts the same per-request components as v1
  (server excess, client overhead, split by rate class) for each delay,
  never completion times.

## Stage A: blind live parity (after K, before training)

Simulator predictions (20-seed geometric means, with the calibrated
simulator) are written into an amendment before any A run.

| Configuration | Delay |
|---|---|
| CAP0, 8 × 1 MiB | 80 ms |
| HEALTHY, 8 × 1 MiB | 80 ms |
| CAP8, 12 × 1 MiB | 120 ms |
| D2 via `-schedule custom -schedule-spec 2:163840,4:2560000`, 16 × 1 MiB | 120 ms |

2 repetitions each. The last row exercises the custom-schedule path before
Stage D; a unit check confirms its phases equal the built-in d2. **Pass:** every live geometric mean within ±8 % of its
prediction. **If A fails,** stop, record it, amend the simulator once and
rerun A with fresh runs; training waits until A passes. **Stop rule:** if
the rerun also fails, v2 stops there and is reported as "simulator could not
be calibrated across delay"; no training or live evaluation follows.

## Held-out scenarios (frozen here, before training)

None of them is in the training, validation or v1 test sets.

| Scenario | Definition |
|---|---|
| H_C12 | t1; cap C = 12 MiB/s from 6 s; 40 ms |
| H_RTT110 | t1; cap C = 8 MiB/s from 12 s; 110 ms |
| H_DIP | custom: 640 KiB/s → 2500 at 1 s → 400 at 30 s → 2500 at 40 s; cap C = 8 MiB/s from 6 s; 40 ms |
| H_C20 | t1; cap C = 20 MiB/s from 4 s; 20 ms |

γ = 0.05 and 512 MiB in all four. They lie inside the v2 training ranges
(new combinations), and H_RTT110 lies between the 80 ms validation delay and
the 120 ms training maximum. Screened before freezing with the v1-calibrated
simulator (5 seeds), the 16 → n oracle at the cap time beats the best fixed
N at 1 MiB by about 17 % (H_C12), 19 % (H_RTT110), 13 % (H_DIP) and 8 %
(H_C20), so each has headroom for an N-only agent.

**Headroom gate.** Before training, an amendment records, from the
calibrated v2 simulator, the best fixed N at 1 MiB, the 16 → n oracle and
the `drop` arm's prediction for each H scenario. A scenario whose oracle
headroom is below 5 % is dropped (not replaced) and the denominator below
shrinks accordingly. No retraining or reselection follows any simulator
result on the H scenarios.

## Stage B: training and selection (simulator only)

Script `python/train_cap_v2.py`. Train 5 seeds on the broadened
distribution; select on the 20 validation conditions only. Reported, never
used for selection: CAP8, HEALTHY, CAP0, D2, G_C16, G_RTT and G_D2 (the v1
held-out scenarios, now inside the training ranges and labelled "used
during v2 design"), the 4 H scenarios, and the 20 v1 out-of-distribution
conditions, relabelled "v1 OOD set": they are now mostly inside the v2
ranges (only their γ 0.12–0.20 is outside). Results for all 5 seeds and the
training curves are reported. The training budget is kept at 1,000
episodes because the ε schedule completes; the training curve is the
check that it is enough. Then the Go parity test, then an amendment with the simulator
predictions for every Stage C and D arm, before any C or D run.

## Stage C: live, in distribution (8 blocks)

Scenarios CAP8 and HEALTHY at 40 ms. Arms:

- `v2`: the v2 model, `-n 16 -window 16`;
- `v1`: the v1 model (`rl_cap_train/model/dqn_cap_candidate.json`), same
  flags;
- `drop`: `-policy drop -n 16 -chunk 1048576 -capped-n 4 -capped-s 1048576
  -drop 0.5` (the frozen P2 N-only rule, default window 3). Its capped
  target N = 4 was tuned for C = 8 MiB/s, and it is always labelled
  "C=8-tuned drop rule";
- `fixed_16_1M`: fixed 16 × 1 MiB.

All with `-no-request-timeout -split`. 64 runs, shuffled within each block
(seed 20261029).

## Stage D: live, held out (8 blocks)

The H scenarios that pass the headroom gate. Arms: `v2`, `v1`, `drop`,
`fixed_16_1M`, and the simulator's best fixed N at 1 MiB for that scenario
if it is not 16 (fixed in the pre-training amendment). At most 160 runs,
shuffled within each block (seed 20261030).

## Predeclared comparisons (`tools/analyze.py`)

- **Primary (generalization):** in each H scenario, `v2` against the better
  fixed arm, defined as the fixed arm with the lower live geometric mean;
  every fixed arm is reported too.
- **Primary (in distribution):** CAP8, `v2` against `fixed_16_1M`.
- **Secondary:** every scenario, `v2` against `v1`, against `drop` and
  against each fixed arm; `v1` and `drop` against the best fixed arm.
- **Descriptive:** final N, number of moves, N 10 s after the cap.
- **Sensitivity:** with 8 blocks the percentile bootstrap may undercover, so
  every primary ratio is also given with a paired t-interval on the log
  ratios. Where the two disagree, the less favourable verdict is used.

## Interpretation rules (fixed in advance)

- **Generalization.** Let k be the H scenarios where `v2` is faster than the
  best fixed arm, m those where it is slower, of M scenarios kept. The
  sentence always gives "faster in k, slower in m of M".
  - k ≥ M − 1, k ≥ 3 and m = 0: "v2 beat the best fixed 1 MiB setting on
    held-out scenarios inside its training envelope";
  - k ≥ 1 otherwise: "v2 generalized partially";
  - k = 0: "v2 did not generalize to held-out scenarios".
  The same sentence states that the scenarios lie within the training
  ranges, and gives v1's k and m on the same scenarios.
- **In distribution (CAP8 primary):** faster: "v2 beat the best fixed 1 MiB
  setting on CAP8"; otherwise "v2 did not beat it on CAP8". HEALTHY is
  always reported with it.
- **v2 against v1,** per scenario, by the CI rule; summarised as "v2 faster
  than v1 in a, slower in b of 2 + M scenarios".
- **Against the rule,** over 2 + M scenarios, with a faster and b slower:
  - a > b: "learned control adds something over a C=8-tuned drop rule";
  - a = 0: "learning reached at most what a C=8-tuned drop rule does";
  - otherwise: "mixed against a C=8-tuned drop rule".
  Both counts are always given, and scenarios where the rule's predicted
  time (from the amendment) is worse than fixed 16 × 1 MiB are named, since
  the rule's tuning, not learning, may explain them.
- **Claim scope if v2 wins:** the best fixed setting with the same range
  size (1 MiB), under server-side changes, on this server model. Not 16 ×
  32 MiB, aria2 or real servers. v1 and v2 are both reported.
- If HEALTHY is slower for `v2`, the reports state the cost alongside any
  win.

## Amendment A1 (2026-10-25, after Stage K, before any Stage A run): rate classes and Stage A predictions

**Stage K:** 24 of 24 runs valid. Client overhead beyond the round trip
grows with delay. In the ≥ 2 MiB/s class the mean client_extra was 0.044,
0.102, 0.195 and 0.321 s at 20, 40, 80 and 120 ms one-way delay. Server
excess grew from 0.024 to 0.055 s. So a single 40 ms table understates
timing at high delay.

**In-sample check (diagnostic, on the K runs themselves).** With the two
v1 rate classes, 10 of 12 K cells were within 2 %. CAP8 at 120 ms was off:
−13.1 % at N = 16 and +3.7 % at N = 4. The cause was the pooled class below
2 MiB/s, which mixed three regimes:
- capped requests under 0.5 MiB/s (N = 16; mean client_extra 0.25 s);
- start-up requests at 640 KiB/s (2.26 s at 120 ms);
- capped requests at about 1.74 MiB/s (k = 4).

**Change.** Calibration uses four rate classes: below 0.5 MiB/s, 0.5–1,
1–2 and ≥ 2 MiB/s, in `tools/calibrate.py` and `sim_env_cap`'s multi-table
path. The 40 ms table is rebuilt from the v1 source runs with the same
classes. The per-request components and their extraction are unchanged.
With four classes, 13 of 14 in-sample cells are within 3 %. The residual,
CAP8 at 120 ms with N = 16, is −7.7 %. It is disclosed here and not tuned
further, because Stage A is the out-of-sample test. A three-class version
was tried first (N = 4 at 120 ms became +11.1 %) and rejected on the same
in-sample check.

**Custom-schedule check.** `tools/v2_bench_test.go` confirms that
`-schedule-spec 2:163840,4:2560000` equals the built-in d2 phases.

**Stage A predictions** (`predictions_A.json`): 20-seed geometric means,
with the 5th–95th percentiles. Pass if each live geometric mean is within
±8 %.

| Configuration | Predicted (s) |
|---|---|
| CAP0, 8 × 1 MiB, 80 ms | 93.32 [92.88, 93.72] |
| HEALTHY, 8 × 1 MiB, 80 ms | 43.99 [43.59, 44.27] |
| CAP8, 12 × 1 MiB, 120 ms | 94.82 [92.31, 96.63] |
| D2 (custom spec), 16 × 1 MiB, 120 ms | 103.00 [96.80, 106.23] |

## Amendment A2 (2026-10-25, after Stage A, before training): Stage A result and headroom gate

**Stage A passed:** 8 of 8 runs valid, each geometric mean within ±8 %
(`analysis_A.json`).

| Configuration | Live vs prediction |
|---|---|
| CAP0, 8 × 1 MiB, 80 ms | +1.4 % |
| HEALTHY, 8 × 1 MiB, 80 ms | −3.3 % |
| CAP8, 12 × 1 MiB, 120 ms | +5.6 % |
| D2 (custom), 16 × 1 MiB, 120 ms | +5.6 % |

The host had a load average of 12–19 on 12 cores during these runs, from
outside this container. The user removed that load before training.

**Headroom gate** (`predictions_H.json`, calibrated v2 simulator, 10
seeds; times in seconds):

| Scenario | Best fixed N (s) | Fixed 16 | 16 → n oracle | Headroom | C=8-tuned drop rule | Kept |
|---|---|---|---|---|---|---|
| H_C12 | 9 (59.8) | 60.7 | n = 6, 49.4 | 17.4 % | 55.7 | yes |
| H_RTT110 | 14 (80.3) | 82.6 | n = 6, 68.1 | 15.2 % | 97.4 | yes |
| H_DIP | 14 (87.7) | 88.1 | n = 5, 76.1 | 13.2 % | 77.4 | yes |
| H_C20 | 8 (39.0) | 42.2 | n = 7, 35.1 | 9.9 % | 53.2 | yes |
| CAP8 (reference) | 16 (77.6) | 77.6 | n = 5, 60.8 | 21.7 % | 61.7 | – |
| HEALTHY (reference) | 16 (19.0) | 19.0 | – | – | 19.0 | – |

All four H scenarios pass the gate, so M = 4. The Stage D extra fixed arms
are, from the simulator's best fixed N: `fixed_9_1M` (H_C12), `fixed_14_1M`
(H_RTT110, H_DIP) and `fixed_8_1M` (H_C20). The drop rule's predicted time
is worse than fixed 16 × 1 MiB in H_RTT110 and H_C20. As the protocol
requires, those two are named in any rule comparison.
