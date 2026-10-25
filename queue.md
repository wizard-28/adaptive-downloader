# Work queue

Deadline: viva on Thursday 2026-10-29, with everything verified by Thursday
morning. Live runs must not overlap: only one timing-sensitive job uses the
loopback at a time. Status markers: `[x]` done, `[~]` in progress, `[ ]` not
started. Details live in `docs/PROJECT_CONTEXT.md` §22.109.

## 1. Retrain DQN on the capped server (`testdata/rl_cap_train`)

- [x] Simulator `python/rl/sim_env_cap.py`, calibration taken from request
  logs (not completion times), 13 tests
- [x] Protocol frozen before any data (9b6b5f3), with held-out
  generalization stage D added at the user's request
- [x] A2 blind live parity: PASS, all four checks within 1.4 % (1ef2c77)
- [x] Stage B training: seed 0 selected; Go parity test passes (9b4e45f).
  Simulator gates: G1 pass, **G2 fail** (HEALTHY 1.080), G3 pass
- [x] Stage C live: 120/120 valid. Primary result: CAP8 dqnc_start16 vs
  16×1 MiB = 0.907 [0.893, 0.921], FASTER (simulator 0.912). Against
  16×32 MiB 1.298 and aria2 1.310, slower; against v6 0.949, not
  distinguishable. HEALTHY 1.108, slower (simulator 1.080). Start 1 slower
  in both scenarios.
- [x] Stage D held-out: 65/65 valid (protocol said 80; arithmetic slip,
  amendment A1). Faster than the best fixed 1 MiB in **0 of 3**: G_C16 1.177
  slower; G_RTT 1.016 and G_D2 1.015 (vs 4×1 MiB) not distinguishable
  (a68d511). The simulator was too optimistic for G_RTT (0.930).

## 2. Extra baselines and network sweep

- [x] axel 2.17.13 and lftp 4.9.2 installed and smoke-tested (16 parallel
  connections)
- [x] wget2 removed by amendment A2: cannot download one file in parallel
  (upstream issue #195, confirmed in the v2.1.0 source)
- [x] `testdata/baselines_ext` protocol frozen (87d546b, A1, A2): 4 systems ×
  4 scenarios × 5 blocks = 80 runs
- [x] baselines_ext E: 80/80 valid. HEALTHY and D2: ours 0.345–0.375 of
  every tool's time (all three open 16 × 32 MiB at the start, pinned at
  640 KiB/s). CAP8: all level (~53 s). CAP0: lftp 114 s (16 requests, no
  re-splitting, like our no-split engine); axel 2.5 % faster than ours (41
  requests); aria2 0.995.
- [x] Delay and loss sweep protocol frozen (`testdata/netem_sweep`): D2 and
  CAP8, delay {10, 40, 100} ms × loss {0, 0.4, 2} %, ours vs aria2, 3 blocks,
  108 runs
- [x] netem_sweep S: 108/108 valid. D2: ours faster in every block of all
  9 cells (0.36–0.46), so the direction holds across the grid. CAP8: does
  NOT hold; near parity. Reference cell (40 ms, 0.4 %) aria2 faster in 3/3
  (1.035); 5 mixed; aria2 faster at 10 ms/2 % (1.073); ours faster at 100 ms
  with 0 % (0.769) and 0.4 % (0.955).

## 3. Fold results into the paper and the number check

- [x] §7a engine ablation, §7b online tuner, §9a frozen DQN on the capped
  server; checker at 168/168 with text anchors; self-test passes
  (36b9217, 8133250)
- [x] §9b retrain (blind parity, C, D), abstract and claim map; checker
  183/183; self-test 9/9 (40c2a2f and next)
- Note: the token scan weakens as recomputed values grow (0.763 = 1/1.310
  slipped through). Give every headline number a `text` anchor in
  `paper_numbers.json`.
- [x] §4.3 axel/lftp, §4.4 sweep, §9c joint, wasted-bytes caveat, loss
  re-check correction; checker 199/199, 0 unverified; self-test 9/9 after
  anchoring P2 (ee912a7)
- [ ] §9d v2 (after Stage D); then abstract, limitations, conclusion and
  claim map

## 4. Viva preparation

- [~] `reports/thursday/qa.md` aligned with Review 3 (Q9, Q11 fixed;
  Q15–Q20 added). Fill in the `[RESULTS]` and `[STATUS]` placeholders
  (Q17, Q20, and wget2 in Q20)
- [~] `reports/thursday/project_summary.md`: baselines, sweep and joint
  (finding 7) added; v2 pending
- [x] Demo run end to end and timed (`reports/thursday/demo.md`): about
  2.5 min in all; every step OK
- [ ] Rehearse the hardest questions: generalization, "tuned to the test",
  why no range-size action, aria2 gap, wget2 exclusion, IEEE status

## 5. After the above, if time allows before Thursday

- [x] Joint N + range-size agent on the capped server.
  - [x] Simulator `python/rl/sim_env_joint.py` (variable S, splitting, v1
    observables). It equals sim_env_cap exactly at 1 MiB. Retrospective
    live check on 8 configurations (split ablation) within 5.3 %; long
    ranges come out up to 5 % fast.
  - Finding: the sim headroom is large (scripted "1 MiB, then 33 MiB once the
    rate rises": CAP8 0.352 of the best fixed) but comes from the
    arrival-time rate rule; a server that re-checks rates during a response
    would remove most of it.
  - [x] User chose: run with a live-rate control.
  - [x] Server opt-in live-rate mode with 4 tests (e71d4b0); harness
    `testdata/rl_joint/tools/joint_bench.go` (`-policy dqnj`, `-live-rate`);
    simulator tests (8); `python/train_joint.py`; Go parity test
  - [x] Protocol frozen (`testdata/rl_joint/protocol.md`): A2 blind parity (8
    runs), B training, C 150 runs (10 blocks), L 40 runs on the live-rate
    server (5 blocks)
  - [x] A2 blind parity PASS, 8/8 valid. Errors +1.5 %, +4.5 %, +3.4 %,
    **+7.0 %** (HEALTHY 16×17 MiB; the limit is 8 %). The simulator is
    consistently optimistic, more so for long ranges, so read the
    predictions for a large-range agent as optimistic.
  - [x] First training run was undertrained: about 40k steps per seed
    against 150k ε decay, so final ε was near 0.75. Amendment A1: kept as
    train_A0/ and not evaluated live. (Protocol error: the budget was set in
    episodes, carried over from the 1 MiB study.)
  - [~] Retraining with --episodes 11000 (about 450k steps per seed, about
    26 min per seed). The first attempt was stopped by Claude Code for low
    system memory after seeds 0–1 (kept in train_A1_partial_killed/).
    Restarted with all 5 seeds and 2 workers, after the user freed memory.
    About 65–80 min.
  - [x] Retrained (747k–824k steps per seed). Simulator: CAP8 1.513 of the
    best fixed, so the agent did NOT learn the large-range behaviour; J1 and
    J2 fail, J3 passes. Go parity passes. Predictions frozen in amendment A2
    (cc7c87f).
  - [x] Stage C (150/150 valid, fd7130e): CAP8 agent 1.471 [1.405, 1.546]
    of 16x32 MiB, slower (primary not faster). HEALTHY 1.081, CAP0 1.359,
    D2 1.372, all slower; vs aria2 CAP8 1.422 slower, HEALTHY 0.388 faster.
    Live within -1..+5 % of the A2 predictions. Agent still at 1 MiB at 8 s.
  - [x] Stage L (40/40 valid, 5e2acee): CAP8L 1.029 [0.989, 1.081], not
    distinguishable; HEALTHYL 1.128 slower.
  - [x] Frozen wording: "a joint agent trained on a checked simulator did
    not beat the best fixed setting live". See PROJECT_CONTEXT §22.109.
- [x] Engine-fix audit of the network-side studies (Haiku subagent): the
  Mahimahi five-trace gate ran with no request deadline (all arms the same),
  so it is unaffected. M3, the live pilot and the §22.91 queueing gate are
  unaffected (no deadline, or zero retries).
  - [x] §22.92 correlated-loss screen re-simulated without the deadline
    (`testdata/joint_ns_loss_mechanism/nodeadline/`; the copy reproduces the
    frozen outputs byte for byte at 30 s). Verdict unchanged and stronger:
    NO_JOINT_N+S_OPPORTUNITY; S* = 16M in 9/9 variants (the 16M→4M shift in
    4/9 was a deadline artefact); 0 failures, 0 retries.
  - [x] netem sweep and split ablation "failed_requests": server-side writes
    aborted by straggler splits (0 without splitting, never above the
    splits; 0 client failures). Paper caveat: "zero wasted bytes" is
    client-side.
- [~] Retrain v2 for generalization (after the joint study). Protocol
  `testdata/rl_cap_train_v2/protocol.md`, frozen after an Opus review (new
  held-out H_C12, H_RTT110, H_DIP, H_C20 with 8-19 % sim headroom; headroom
  gate; stop rule; 8 blocks).
  - [x] Stage K 24/24 valid; amendment A1: four rate classes (b634a51).
  - [x] Stage A blind parity PASS, +1.4/-3.3/+5.6/+5.6 % (8/8 valid).
  - [x] Headroom gate: all 4 H kept, 9.9-17.4 % (A2, bc9917b).
  - [x] Stage B: seed 1 selected (27657be2), Go parity passes; sim H
    0.891/0.951/0.945/1.025, but only seed 1 is good on H (A3, 1492f23).
  - [~] Live D (160 runs), about 21:50 UTC.
  - [x] Stage C 64/64 valid: CAP8 v2 0.845 faster than fixed 16x1 MiB, vs
    v1 0.929 faster, vs drop 1.048 slower; HEALTHY 1.047 slower (2368d56).
  - [ ] `analyze.py D`, `verdict`; then paper §9d, PROJECT_CONTEXT, viva.
- [ ] Real-server check (after v2): public mirrors from the live pilot
  (kernel.org CDN, origin, riken), 64 MB file, arms v2 agent / drop rule /
  fixed 8×1 MiB / 16×1 MiB, about 3 mirrors × 5 reps (about 4 GB), with the
  live-pilot etiquette (User-Agent, ≤16 connections, abort on 429/503).
  - [x] Opt-in policy arms in `cmd/live-pilot` (Sonnet subagent, reviewed;
    plus `reference_arms` switch), tests pass (5b360b2)
  - [x] Protocol, manifest (60 runs) and analysis frozen
    (`testdata/live_mirror_check`, 5b360b2). No-harm check, descriptive.
  - [ ] Run after v2 Stage D: range probe, then `live-pilot -manifest
    testdata/live_mirror_check/manifest.json -slot M1 -out
    testdata/live_mirror_check/raw/M1.jsonl` (about 45 min)
  - Uses the internet link, not the loopback.
- [ ] IEEE Access template conversion. Review 3 says the paper is "being
  formatted"; the template conversion is still open in
  `reports/ieee_access/README.md`.

## Housekeeping (on every completed step)

- Update `docs/PROJECT_CONTEXT.md` §22.109 and this file.
- Before declaring a block done, run the Go checks (build, vet, gofmt,
  test -p 1) and the Python tests.
