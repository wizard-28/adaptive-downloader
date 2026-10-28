# Likely questions and prepared answers: DETAILED reference (exact numbers)

This is the reference version with exact numbers and internal names, for checking
facts. The version to practise and say out loud is `qa.md` (plain spoken words).

Short answers first, then supporting detail. Every number here appears in
`reports/paper_draft.md`, `reports/Review3_Final_Report.pdf` or the study
folders under `testdata/` (split_ablation, online_tuner, rl_capped,
live_pilot_l1, rl_cap_train). Updated 2026-10-25 to match Review 3.

---

**1. So reinforcement learning failed. Why should we care?**

The question was never "can RL win?" It was "when is there anything for any
adaptive controller to exploit?" The strongest negative results are
learner-independent. On the cellular traces, even an oracle that knows the
future gained 0.41 % on average, with a lower bound of −0.26 %. That oracle
switches only N (1 to 16), between 120 s stretches of each trace, with S fixed at
1 MiB, under our replay model. That says the
environment offered almost nothing, which is useful for anyone designing
adaptive downloaders.

**2. Why did RL lose?**

There are three reasons:
- The ceiling was low. Oracle bounds apply to every policy.
- Simulator gains did not transfer. L4 showed 17.6 % headroom in simulation,
  but the live server did not penalise excess concurrency.
- Where real headroom existed, a one-threshold rule captured it (+23.4 % over
  the best of three small-range fixed settings on the original engine,
  replicated at R = 0.767). On the fixed engine, though, the best fixed
  setting (16 × 32 MiB) beat it by 7 %; see Q15.

A better learner cannot beat a near-zero ceiling.

**3. Why only N and S? Maybe RL would win on another dial.**

N and S are *the* dials of segmented downloaders: aria2's `-x`, `-s` and `-k`,
and IDM's connection count. The scope was fixed before the study, and narrow
scope is what lets a controlled study attribute effects. Other dials, such as
mirror choice, restarting slow requests, hedging or congestion control, are a
different question, and they are listed as future work. We do not claim RL
can never help downloading.

**4. Did you explore RL fully?**

No, and we do not claim to. Two learner families were tested: a LinUCB bandit,
and Double DQN through several versions. The conclusion rests on oracle bounds
computed before training, not on exhausting learners.

**5. Isn't this obvious?**

The framework is intuitive: adaptation needs a moving optimum, and a single
flow gains little from extra connections. These findings were not obvious:
- capacity varied 6.5–25.3× (p90/p10 per trace) but the oracle headroom stayed under 1 %;
- request-start rate locking created a 62.4 % benchmark gap;
- aria2's lead under the cap came from two engine details (a whole-request
  timeout and no straggler splitting), not from smarter control;
- simulator headroom disappeared live;
- a one-rule controller captured the real headroom.

**6. Networks change all the time. Why don't fixed settings lose?**

On one bottleneck path, the best N is roughly "enough to fill the pipe". When
capacity drops, everything slows down, but the same setting is still about
right. Adaptation pays only when *too much concurrency has a cost that changes
over time*, such as a server that throttles per client. We screened
shared-bottleneck queueing and correlated loss and found no opportunity. The
constructed concurrency cap was the one case where it paid, and only against
small-range fixed settings on the original engine (Q15).

**7. What about real networks? Everything is loopback.**

- The cellular results use recorded real traces (Winstein et al., NSDI 2013),
  replayed through Mahimahi so they are reproducible.
- The live-internet pilot covers four public mirrors (kernel.org CDN and
  origin, Japan, China) across four time-of-day slots. One global setting,
  8 × 1 MiB, was on average 3.7 % from each condition's best (bootstrap
  [1.0, 4.8]; 16 conditions). That is under the predeclared 5 % line.
- One slot accidentally ran on a second network. There the headroom was
  7.2 % [1.2, 12.2] over only 4 conditions, which is inconclusive and
  reported separately.
- It is a descriptive pilot with one client and one primary access network, not a
  validation.

**8. Is the 62.4 % win over aria2 real?**

Yes, on that workload: 30 of 30 pairs, geometric ratio 0.3756. It comes mostly
from request granularity and the start-of-request rate mechanism, not from
smarter control. With granularity matched, the ratio is 0.862. Screens across
six pacing schedules and against stock NGINX point the same way. It is a
mechanism result, and we present it as one.

**9. aria2 beat you under the cap. Doesn't that undermine the adaptive result?**

We investigated it, and it changed the conclusion. A 230-run engine ablation
traced aria2's lead to two engine differences in our downloader, not to
aria2's control:
- **No straggler splitting.** With the cap on from the start, 16 × 32 MiB
  took 40 % less time with splitting (R = 0.599) and then matched aria2
  (68.2 s against 68.5 s, R = 0.995).
- **The 30 s whole-request timeout** cut off large, slow ranges. Without it,
  our best setting with the cap from 8 s came within 1.6 % of aria2 (54.4 s
  against 53.5 s, R = 1.016, a small but real gap).
On the fixed engine, the best fixed setting also beat our adaptive
controllers, so we say plainly that adaptation did not win there.

**10. You built the server model yourselves. Isn't the positive result rigged?**

It is a constructed environment, and we say so. It shows the conditions under
which adaptation can help, not that real servers behave this way. The
six-workload family was selected from 72 analytic candidates, with two
amendments made before any live run, all disclosed. The cross-family claim
over one fixed setting is *not* established: the workload-level interval
includes 1.

**11. How do we know the numbers are right?**

`tools/claims_check.py` recomputes all 231 numbers in the paper
sections from the raw runs and
fails on any number it cannot reproduce. It caught real errors in our own
frozen scripts, such as a fixed t value used for every n. Those were corrected
in the paper, and the frozen artifacts were left untouched.

**12. Is a negative result publishable?**

Yes, especially with this level of rigour: predeclared hashed protocols (not an
independent preregistration; one family of workloads was amended twice, and the paper says so), failed gates
reported as failed, and reproducible numbers. It fits venues that judge
soundness over novelty, such as measurement or reproducibility tracks,
workshops, or journals like IEEE Access. The framing is "when does adaptive
downloading help?", not "RL for downloading".

**13. What would you do with more time?**

Test action spaces with delayed, changing costs, starting with multi-mirror
scheduling and restarting slow requests, and run live measurements across
many clients and networks. Each would first pass a headroom gate and have to
beat both the best static setting and a strong myopic heuristic. Prior work
already covers RL for parallel transfers, congestion control and video
bitrate, so any new claim would be novel in scope, not technique.

**14. What surprised you most?**

Two things:
- **The cellular traces.** Capacity swung massively, yet an oracle could
  barely improve on one fixed setting.
- **The live pilot's side finding.** A major public mirror (Tsinghua) rejects
  Go's default User-Agent with HTTP 403, so a stock Go downloader is locked
  out.

**15. You fixed the engine. Did adaptation still help?**

Not against the best fixed setting on the fixed engine:
- The causal controller was 7 % slower than 16 × 32 MiB with the cap from
  8 s (R = 1.071).
- The workload selector was 23 % slower with the cap from the start.
The earlier +23.4 % holds against small-range fixed settings on the original
engine, and we report both. The honest summary is "engine before policy":
splitting and the timeout semantics mattered more than any controller.

**16. You cite FastBioDL. Did you compare against it?**

We re-implemented its online-tuning idea (aggregate throughput over 2 s
windows, U = T/K^N, reverse and halve the step on a drop) as a project policy
and ran it under a frozen protocol, 64 runs. It was 1.212 (cap from 8 s) and
1.220 (no cap) times the time of the best fixed 1 MiB setting (16 × 1 MiB, the
same piece size; with 32 MiB pieces the fixed setting is faster still). It reacted to the cap in
2.1 s but spent 8.1 s climbing from N = 4 to 16 without a cap. We did not use
FastBioDL's code.

**17. You now trained the DQN for the capped server. Isn't that tuning to
the test?**

That is why the study (testdata/rl_cap_train, protocol frozen before any
training or live run) has two parts:
- **In-distribution (Stage C):** the live CAP8 and HEALTHY scenarios, which
  lie inside the training ranges. A win there shows only that the agent
  learned this server model.
- **Held out (Stage D):** three live scenarios, each outside the training
  ranges on one axis: cap C = 16 MiB/s (trained 4–12), 80 ms each way
  (trained 20–60), and the three-phase d2 schedule (never trained). Each
  needs a different N after the cap (7, 5, 4, against 3 for CAP8), so a rule
  memorised for CAP8 does not transfer by accident.
We say "generalizes within this server family" only if it wins all three.
Nothing here supports claims about real servers.
**Results.** In-distribution (CAP8, 10 blocks), it took 0.907 [0.893, 0.921]
of fixed 16 × 1 MiB's time, close to the simulator's 0.912. Without a cap it
was slower (1.108). Held out, it was faster than the best fixed 1 MiB setting
in 0 of 3 scenarios: C = 16 MiB/s slower (1.177), and 80 ms delay (1.016) and
the d2 schedule (1.015) not distinguishable. So it learned the server it was
trained on and did not generalize. We say that plainly.

**18. Why did the first DQN do badly on the capped server?**

We found two simulator–live mismatches before retraining:
- The v6 simulator had no arrival-time cap.
- Its throughput feature did not match what the live runner measures. Live
  throughput is the bytes of the last 4 attempts over their time span, which
  barely changes above N = 4. The agent could hardly see the effect of extra
  connections.
The new simulator copies the live runner's measurements and was checked
against fresh live runs, with predictions written down before the runs.

**19. Can the retrained agent beat 16 × 32 MiB?**

We said in advance that it cannot. 16 × 32 MiB wins under the cap because all
16 ranges start before the cap and keep the pre-cap rate. An agent that only
changes N with 1 MiB ranges cannot do that. Even the perfect switch in our
simulator (64 s) is slower than 16 × 32 MiB live (52.6 s). The fair test is
against the best fixed setting with the same 1 MiB range size.

**20. Why not IDM, axel or wget2?**

IDM is closed source, runs only on Windows, and cannot be scripted for
hundreds of randomised runs in our Linux testbed. axel, wget2 and lftp are
open source. We ran axel and lftp in the same testbed (80 runs, 5 blocks per
scenario, all valid). wget2 was excluded: it downloads one file over a single
connection however it is configured (upstream issue #195, which we confirmed
in its source).
- **No cap and the paced D2 workload:** ours took 0.345–0.375 of each tool's
  time. Like aria2, axel and lftp split the file into 16 equal 32 MiB parts at
  the start, so all of them are stuck at the slow starting rate.
- **Cap from 8 s:** all four were level, at about 53 s.
- **Cap from the start:** lftp took 114 s, because it never re-splits a slow
  part, which is what our engine did without splitting. axel was 2.5 % faster
  than ours there.
So the 62.4 % mechanism is not aria2-specific; it applies to every
segmented tool that splits the file once at the start.


**21. Why not let the agent choose the range size as well as the number of connections?**

We tried that, under a protocol frozen in advance (`testdata/rl_joint`). The
simulator showed real headroom: a hand-written rule (1 MiB ranges, then
33 MiB once the rate rises) took 0.352 of the best fixed time under the cap.
The Double DQN agent did not find it. Live, over 10 blocks, it took 1.471 of
the time of fixed 16 × 32 MiB under the cap (slower), and it was slower than
the best fixed setting in all four scenarios. The simulator predicted every
live arm to within about 5 %, so the problem is the learner, not the model of
the network. The agent was still on 1 MiB ranges when the cap arrived at 8 s.

A control run on a server that re-checks rates during each response showed
something else. There, fixed 16 × 32 MiB lost most of its advantage (50.9 s
became 67.4 s), and the agent was level with it (1.029, not distinguishable).
So the large-range win depends on that server's arrival-time rate rule. Our
honest conclusion: a joint agent trained on a checked simulator did not beat
the best fixed setting live.

**22. Does the learned agent generalize?**

Partly, and only within the range it was trained on. The first retrain (v1)
beat the best fixed 1 MiB setting where it was trained (0.907). It was
faster in none of three held-out conditions, all outside its training
ranges. For v2 we froze a new protocol, reviewed adversarially before
freezing:
- broader training: cap 4–24 MiB/s, delay 10–120 ms, rate dips;
- calibration at four delays, checked blind at 80 and 120 ms;
- four new held-out scenarios inside those ranges.

Live, over 8 blocks:
- **Held out:** v2 was faster than the best fixed setting in 3 of them
  (0.882, 0.819, 0.952) and slower in 1, by 0.7 %.
- **Against v1:** faster in all 6 scenarios.
- **Without a cap:** it still costs about 5 %.

Three caveats, all stated in advance:
- **One seed:** it is one seed chosen on validation, and the other seeds
  were not as good.
- **Drop rule:** the simple drop rule beat v2 in the scenarios it was tuned
  for. Two of v2's three wins over it came where the rule's tuning was
  wrong.
- **Scope:** "generalizes" here means new combinations inside the training
  envelope, not new kinds of conditions or real servers.

**23. Did you run the adaptive agents on real servers?**

Yes, as a frozen no-harm check on three public kernel mirrors. The first
attempt was lost to an outage on our own link, and we report it rather than
hide it. On the clean rerun (60 of 60 valid) neither the v2 agent nor the
drop rule passed the no-harm rule on any of the three mirrors:
- v2 took 1.2–1.65× the best fixed time (only kcdn clearly);
- the drop rule took 1.06–1.33×.

The mirrors have no cap, but their throughput is noisy. Both arms cut their
connections on these uncapped servers (the drop rule switched in 4 of 5
runs on two mirrors); our reading is that they took noise for a cap, though
we did not analyse the traces to show it. This is the cost side of
adaptation: with little headroom to pay for it (3.7 % selection headroom in
our pilot), adapting carried a cost. It is the same lesson as the cellular
traces: variability is not opportunity.

**24. Your first proposal (Review 1) said the action is a joint pair (N, S) chosen at a fixed interval T, and that you would use PPO. What did you build instead, and why?**

Review 1 described the idea. In the build, we changed how decisions are
triggered and what one action does, for reasons we recorded: cleaner
measurement, clean ablations, and early learning failures. Our reports should
have said so.

Three differences, each with its reason:
- **Decisions after each finished request, not every T seconds.** The
  downloader is event-driven, and one request record is the natural
  measurement. A request boundary is also the clean moment to change N or S,
  because requests already in flight are never resized. An early DQN (v5) was
  trained with a decision interval of 4 requests while the live runner decided
  once per request; the mismatch hurt it, so v6 was trained on the live cadence.
  Cost: with very large pieces, decisions are rare.
- **One dial per action, not a pair.** The range manager and the concurrency
  manager are separate; S only affects future ranges. Single-dial actions let us
  run the N-only, S-only and joint ablation that Review 1 itself asked for
  (under the cap: N-only 0.808, joint 0.766, S-only 1.070 of the fixed time).
  When S was learned, the early DQN versions fragmented the file into
  32–128 KiB pieces and were 75.7 % slower than aria2 (v2), or learned from a
  simulator that rewarded larger chunks the server did not reward (v3), so v4
  onward learned N only. Both dials are still adapted: the causal drop controller
  and workload selector choose a target pair (N, S) together (N jumps to the
  target in one decision, then S moves one step at a time; never both in the
  same decision), the rule-based joint policy alternates, and LinUCB and the
  joint DQN agent choose among five single-dial actions. A learner that picks
  the pair as one action was not built; it is listed as future work.
- **PPO.** Review 1 mentions PPO only in its related-work table (for the
  Swargo and Arifuzzaman work). Its own plan says "a later reinforcement-learning
  policy" and that the evaluation does not depend on RL convergence. We moved
  from LinUCB to Double DQN after a lock-in diagnostic showed a one-step bandit
  cannot handle credit across time (§22.12 of PROJECT_CONTEXT). We did not try
  PPO and have no comparison against it; that is a limitation.

Two more points on this question. Review 3 marks "joint adaptation of N and S"
as Achieved; the accurate reading is "both dials adapted, never as one learned
pair". Review 1 also asked for background-traffic conditions: that was
screened with iperf3 traffic sharing a bottleneck (no state-dependent best N
found), and fairness to other users was not measured.

**25. What is the main answer, in numbers, against aria2?**

Against aria2 under changing conditions, our adaptive arms were faster only
when the server rate rose during the download (time ratios 0.36 to 0.88), and
there a fixed configuration of the same engine was faster still (0.344 to
0.376). When the server degraded, aria2 was faster than every adaptive arm
(1.096 to 1.781). This is the paper's section 9e and its contribution 1.

**26. Why did you choose those thresholds and sizes?** (reasons recorded in the repo)
- **10 % headroom bar:** run-to-run dispersion in our campaigns was about
  1 to 5 %, so under 5 % counted as too weak, 5 to 10 % as marginal, and 10 % or
  more as meaningful. The bar was set before the oracle ran.
- **120 s epochs:** the oracle protocol takes the shortest of 30, 60, 90 and
  120 s that keeps at least 80 % of epochs valid with at least 3 per trace;
  that was 120 s.
- **512 MiB file:** the largest size that is safe on memory in our container,
  and it keeps the trajectory dynamic for about 90 s (startup is 2.4 % of it).
- **Cap C = 8 MiB/s, gamma = 0.05:** of 27 screened (schedule, cap) pairs,
  exactly one passed the predeclared rule that both N and S optima move and
  that cross-state penalties exceed 10 % both ways.
- No recorded reason (ask the authors before the viva): why 16 connections is
  the maximum, why 3, 5, 8 or 10 blocks per study, why a 64 MB file for the live
  pilot, why loopback was preferred to a real path.

**Synonyms used across our documents:** "drop rule" = "causal drop controller" =
"causal controller" (the paper); "best fixed setting" = "best fixed
configuration" (the paper also says "best stationary configuration" when the
comparison set is the three fixed settings tried under one schedule).

**Updates after Review 3 (which the PDFs do not contain):** the checker now
recomputes 231 numbers (Review 3: 144); axel and lftp were added (section 4.3);
a delay and loss grid was added (section 4.4); DQNs were retrained for the
capped server and tested on held-out conditions (sections 9b, 9d), where one
beat the fixed 1 MiB setting; a joint five-action agent was built (section 9c);
and a mirror no-harm check was run (section 8a).

