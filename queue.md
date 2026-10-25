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
- [ ] Add baselines_ext and sweep results
- [ ] Extend `tools/claims_check.py` and `paper_numbers.json` for the new
  results; rerun the self-test
- [ ] Update the abstract, limitations, conclusion and claim map

## 4. Viva preparation

- [~] `reports/thursday/qa.md` aligned with Review 3 (Q9, Q11 fixed;
  Q15–Q20 added). Fill in the `[RESULTS]` and `[STATUS]` placeholders
  (Q17, Q20, and wget2 in Q20)
- [~] `reports/thursday/project_summary.md` updated (findings 5–6, takeaway); fill in the sweep (Stage D and baselines are in qa.md; summary still needs the baselines)
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
  - [ ] **Rule arm** in every v2 live stage: the causal drop rule
    (`-policy drop`, N-only, 1 MiB, corrected engine), in-distribution and
    on the fresh held-out scenarios. Wording rule, fixed in advance: if v2
    beats the rule, "learned control adds something"; if it only matches,
    "learning reached what a simple rule does".
  - Claim scope if v2 wins: the best fixed setting *with the same range
    size*, under server-side changes (a cap switching on). Report v1 and v2
    both.
- [ ] Real-server check (after v2): public mirrors from the live pilot
  (kernel.org CDN, origin, riken), 64 MB file, arms v2 agent / drop rule /
  fixed 8×1 MiB / 16×1 MiB, about 3 mirrors × 5 reps (about 4 GB), with the
  live-pilot etiquette (User-Agent, ≤16 connections, abort on 429/503).
  - [ ] Opt-in `-policy` support in `cmd/live-pilot` (additive) and tests
  - [ ] Frozen protocol. Stated in advance: these mirrors showed little
    headroom (3.7 %), so the check mainly tests "no harm on unlike
    servers"; descriptive, one client and one access link
  - Uses the internet link, not the loopback.
- [ ] IEEE Access template conversion. Review 3 says the paper is "being
  formatted"; the template conversion is still open in
  `reports/ieee_access/README.md`.

## Housekeeping (on every completed step)

- Update `docs/PROJECT_CONTEXT.md` §22.109 and this file.
- Before declaring a block done, run the Go checks (build, vet, gofmt,
  test -p 1) and the Python tests.
