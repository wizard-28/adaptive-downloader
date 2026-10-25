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
- [~] Stage D held-out live runs: 65 runs (protocol said 80; arithmetic slip, amendment A1) (G_C16, G_RTT, G_D2), started
  ~07:20 UTC, about 1.5 h
- [ ] Analyse D, apply the frozen wording rules, commit

## 2. Extra baselines and network sweep

- [x] axel 2.17.13 and lftp 4.9.2 installed and smoke-tested (16 parallel
  connections)
- [x] wget2 removed by amendment A2: cannot download one file in parallel
  (upstream issue #195, confirmed in the v2.1.0 source)
- [x] `testdata/baselines_ext` protocol frozen (87d546b, A1, A2): 4 systems ×
  4 scenarios × 5 blocks = 80 runs
- [ ] Run baselines_ext stage E (after Stage D), about 1.5 h
- [ ] Analyse E and commit
- [x] Delay and loss sweep protocol frozen (`testdata/netem_sweep`): D2 and
  CAP8, delay {10, 40, 100} ms × loss {0, 0.4, 2} %, ours vs aria2, 3 blocks,
  108 runs
- [ ] Run netem_sweep stage S (after stage E), about 80 min; analyse; commit

## 3. Fold results into the paper and the number check

- [x] §7a engine ablation, §7b online tuner, §9a frozen DQN on the capped
  server; checker at 168/168 with text anchors; self-test passes
  (36b9217, 8133250)
- [x] §9b retrain (blind parity, C, D), abstract and claim map; checker
  183/183; self-test 9/9 (40c2a2f and next)
- Note: the token scan weakens as recomputed values grow (0.763 = 1/1.310
  slipped through). Give every headline number a `text` anchor in
  `paper_numbers.json`.
- [ ] Add baselines_ext and sweep results
- [ ] Extend `tools/claims_check.py` and `paper_numbers.json` for the new
  results; rerun the self-test
- [ ] Update the abstract, limitations, conclusion and claim map

## 4. Viva preparation

- [~] `reports/thursday/qa.md` aligned with Review 3 (Q9, Q11 fixed;
  Q15–Q20 added). Fill in the `[RESULTS]` and `[STATUS]` placeholders
  (Q17, Q20, and wget2 in Q20)
- [~] `reports/thursday/project_summary.md` updated (findings 5–6, takeaway); fill in Stage D, baselines and sweep
- [~] Demo script written (`reports/thursday/demo.md`: live CLI download
  with probe and `-sha256`, capped-server run with the retrained agent, tests
  and checker). Still to do: run it end to end and fill in the timings, after
  the live studies finish (it uses the loopback)
- [ ] Rehearse the hardest questions: generalization, "tuned to the test",
  why no range-size action, aria2 gap, wget2 exclusion, IEEE status

## 5. After the above, if time allows before Thursday

- [~] Joint N + range-size agent on the capped server.
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
  - [ ] A2, then training: chained after the sweep (no live overlap)
  - [ ] Write the simulator-prediction amendment, then run C and L, analyse,
    and apply the wording rules
- [ ] Retrain v2 for generalization (after the joint study). Same learner,
  action space and Go adapter as rl_cap_train.
  - The current Stage D result stands as final: faster in 0 of 3. The three
    G_* scenarios are reported as "used during v2 design" and are no longer
    held out.
  - [ ] Broaden the training distribution: C up to about 24 MiB/s, one-way
    delay up to about 120 ms, multi-phase schedules with dips.
  - [ ] Calibrate across delay: collect request-timing logs at 2–3 delays
    (for example 20, 80 and 120 ms); delay-dependent jitter in sim_env_cap
    (as a new module or an opt-in option, keeping the frozen study
    reproducible).
  - [ ] Blind live parity at 80 and 120 ms.
  - [ ] Choose and freeze NEW held-out scenarios before training (for
    example C = 12 or 20 MiB/s, 120 ms, a different dip schedule).
  - [ ] Frozen protocol, 5 seeds, validation-only selection, live
    in-distribution and held-out stages. About 5–6 h including live runs.
- [ ] IEEE Access template conversion. Review 3 says the paper is "being
  formatted"; the template conversion is still open in
  `reports/ieee_access/README.md`.

## Housekeeping (on every completed step)

- Update `docs/PROJECT_CONTEXT.md` §22.109 and this file.
- Before declaring a block done, run the Go checks (build, vet, gofmt,
  test -p 1) and the Python tests.
