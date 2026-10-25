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
- [ ] Add retrain results (§9b: Stage C and D, parity, simulator gates)
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

- [ ] Joint N + range-size agent on the capped server. Needs simulator
  support for variable range size and straggler splitting (≥ 2 MiB
  unreceived), a new parity check against live runs, and a new frozen
  protocol. About 1 day.
- [ ] IEEE Access template conversion. Review 3 says the paper is "being
  formatted"; the template conversion is still open in
  `reports/ieee_access/README.md`.

## Housekeeping (on every completed step)

- Update `docs/PROJECT_CONTEXT.md` §22.109 and this file.
- Before declaring a block done, run the Go checks (build, vet, gofmt,
  test -p 1) and the Python tests.
