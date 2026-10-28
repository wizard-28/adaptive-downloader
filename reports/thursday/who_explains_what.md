# Who explains what tomorrow

Source of the roles: the "Team Member Responsibility" table in Review 1 and the
"Individual Contribution" table in Review 2 (they agree), checked against what is
actually in the code and the paper today. Not based on commit history.
Slide numbers are the 15-slide presentation.

## The roles as submitted

| Student | Assigned role |
|---|---|
| **Sourajyoti Basak** (24BCE0774) | Downloader architecture and core implementation; adaptive concurrency and range management; test server and Linux network experiments; system integration and validation |
| **Shwetank Shrijan** (24BDS0141) | Downloader development and testing of assigned components; monitoring and experiment automation; baseline and heuristic evaluation |
| **Ayush Konar** (24BDS0107) | LinUCB; RL state, action and reward design; Python/PyTorch models; integration of the learned policy with the Go downloader; ML/RL evaluation |

## Who presents which slide

| Slide | Topic | Presenter |
|---|---|---|
| 1 | Title and team | Sourajyoti (introduces all three) |
| 2 | Problem, objectives, prior-art matrix | Sourajyoti |
| 3 | Proposed solution | Sourajyoti |
| 4 | Final architecture (and "how one decision is made") | Sourajyoti |
| 5 | Methodology (fair races, plans written first, checks) | **Shwetank** |
| 6 | Implementation: policy interface and invariants, straggler splitting | Sourajyoti |
| 6 | Implementation: "how the controllers decide": the rule | Shwetank |
| 6 | Implementation: LinUCB and Double DQN lines | **Ayush** |
| 7 | Live demo, step 1: download from the public mirror | Sourajyoti |
| 7 | Live demo, step 2: throttling server with the retrained agent | Ayush |
| 7 | Live demo, step 3: tests and the number checker | Shwetank |
| 8 | Testing and results: 62.4 %, 1.071, the timeline | Shwetank |
| 8 | Testing and results: the 0.907 learning-agent row | Ayush |
| 9 | Mobile traces and the perfect-controller ceiling | Sourajyoti |
| 10 | Engine ablation: why aria2 led | Sourajyoti |
| 11 | Performance and comparison with aria2, axel, lftp | Shwetank |
| 12 | Innovation and application | Ayush |
| 13 | Limitations | Shwetank |
| 14 | Conclusion | Ayush |
| 15 | Future enhancement, "Thank you. Questions?" | Ayush |

Slide count: Sourajyoti 7 (1 to 4, 6, 9, 10), Shwetank 4 (5, 8, 11, 13), Ayush 3
(12, 14, 15), plus shared parts of 6, 7 and 8. If you want it more even, move slide
13 or slide 11 to Ayush.

## What each student must be able to explain

### Sourajyoti: the system and the experiments
- **Code you own:** `internal/downloader` (worker pool, policy runner), `fetcher`,
  `assembler`, `rangetracker`, the N and S controllers (`concurrency`, `chunksize`),
  the experiment server and the netem network shaping (`internal/experiment/server`,
  `network`), the trace replay, `cmd/downloader`.
- **Explain:** how a download works (ranges, workers, assembling the file); the rules
  that keep it correct (running requests are never resized, a failed range retries
  with the same bounds, one decision at a time); straggler splitting; the test server
  (pacing, cap, faults) and what netem adds; why aria2 led under the cap (the 30 s limit
  and no splitting); the mobile-trace replay and the 0.41 % ceiling.
- **Questions that are yours:** Q8 (big win over aria2), Q9 (aria2 beat you), Q10
  (is the server rigged), Q6, Q7, Q15 (engine fix), Q21 (the joint-action deviation:
  why we decide after each request and one dial per action).

### Shwetank: measuring, baselines and rules
- **Code you own:** `internal/metrics` (the monitoring), `internal/policy` (fixed
  setting, throughput rule, piece-size rule, the alternating joint rule, the online
  tuner), the experiment runner and automation (`internal/experiment`, `cmd/experiment`,
  `matrix`), the baselines (`cmd/aria2-benchmark`, aria2, axel and lftp runs), and the
  number checker `tools/claims_check.py`.
- **Explain:** how we measure (one record per request; fair back-to-back random-order
  races; plans written first; time ratio, below 1 is faster); the baselines and why
  (aria2, axel, lftp, the best fixed setting; why not wget2 and IDM); the simple rules
  (the drop rule, the workload selector, the online tuner) and how they decide; the
  comparison results; how every paper number is recomputed by a script; the limits of
  the study.
- **Questions that are yours:** Q11 (are the numbers right), Q12 (publishable), Q16
  (FastBioDL), Q18 (axel, lftp, wget2, IDM), Q22 (main answer against aria2), Q25
  (limits).

### Ayush: the learning controllers
- **Code you own:** `internal/rl` (state, action, reward, LinUCB, the DQN inference in
  Go) and `python/rl`, `python/train_*.py` (the simulator, the state, the Double DQN
  model and training), plus the learning-agent experiments and live runs.
- **Explain:** what LinUCB and Double DQN are in simple words; what they see (about ten
  and about thirteen numbers), what they can do (five and three actions), the reward
  (minus the time to the next request); why we trained in a simulator and checked it
  against live runs; why LinUCB was replaced by Double DQN; the results: the retrained
  agent won on its own server, did not win outside its range, and failed the no-harm test
  on real mirrors; why a learner cannot beat a near-zero ceiling.
- **Questions that are yours:** Q1 to Q4 (why learning failed, why only N and S, did you
  try everything), Q17 (is it tuned to the test), Q19 (piece size agent), Q20 (real
  servers), Q23 and Q24 (how the controllers work and what they see), and the PPO part
  of Q21.

## Shared: everyone must know

- The 30-second story: tools fix N and S at the start; we let them change mid-download;
  adapting rarely helped; the engine matters more than the controller.
- The main answer against aria2: adaptive controllers won only when the server sped up,
  and a fixed setting won by more; they lost when the server got worse.
- The Review 1 deviation line: "Review 1 described the idea. In the build, we changed how
  decisions are triggered and what one action does, for reasons we recorded: cleaner
  measurement, clean comparisons, and early learning failures. Our reports should have
  said so."
- Honest answers: we did not try PPO; we never built a learner that picks (N, S) as one
  action; we have no recorded reason for 16 connections, the number of rounds, the 64 MB
  live file or why loopback.

## Hand-offs
Sourajyoti (1 to 4) hands to Shwetank for the method (5); Shwetank hands the controllers
line on slide 6 to Ayush, then back to Sourajyoti for the engine; Sourajyoti runs the demo
and passes steps 2 and 3 to Ayush and Shwetank; Shwetank gives the results (8); Sourajyoti
shows the mobile traces and the engine ablation (9 and 10); Shwetank gives the comparison
(11); Ayush closes with innovation, the conclusion and future work (12, 14, 15), with
Shwetank on limitations (13).
