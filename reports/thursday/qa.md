# Likely questions and prepared answers (Thursday review)

Short answers first, then supporting detail. Every number here appears in
`reports/paper_draft.md` or `testdata/live_pilot_l1/`.

---

**1. So reinforcement learning failed. Why should we care?**

The question was never "can RL win?" It was "when is there anything for any
adaptive controller to exploit?" The strongest negative results are
learner-independent. On the cellular traces, even an oracle that knows the
future gained 0.41 % on average, with a lower bound of −0.26 %. That says the
environment offered almost nothing, which is useful for anyone designing
adaptive downloaders.

**2. Why did RL lose?**

There are three reasons:
- The ceiling was low. Oracle bounds apply to every policy.
- Simulator gains did not transfer. L4 showed 17.6 % headroom in simulation,
  but the live server did not penalise excess concurrency.
- Where real headroom existed, a one-threshold rule captured it (+23.4 % over
  the best of three fixed settings).

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
- capacity varied 5–27× but the oracle headroom stayed under 1 %;
- request-start rate locking created a 62.4 % benchmark gap, and reversed
  direction under the cap;
- simulator headroom disappeared live;
- a one-rule controller captured the real headroom.

**6. Networks change all the time. Why don't fixed settings lose?**

On one bottleneck path, the best N is roughly "enough to fill the pipe". When
capacity drops, everything slows down, but the same setting is still about
right. Adaptation pays only when *too much concurrency has a cost that changes
over time*, such as a server that throttles per client. We screened
shared-bottleneck queueing and correlated loss and found no opportunity. The
constructed concurrency cap was the one case where it paid.

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

No. The claim is improvement over the best *stationary* configuration (+23.4 %),
not superiority over aria2. aria2 wins there because its early pieces start
while concurrency is low and keep that higher rate. It is the same
request-start mechanism, in reverse.

**10. You built the server model yourselves. Isn't the positive result rigged?**

It is a constructed environment, and we say so. It shows the conditions under
which adaptation can help, not that real servers behave this way. The
six-workload family was selected from 72 analytic candidates, with two
amendments made before any live run, all disclosed. The cross-family claim
over one fixed setting is *not* established: the workload-level interval
includes 1.

**11. How do we know the numbers are right?**

`tools/claims_check.py` recomputes all 113 paper numbers from the raw runs and
fails on any number it cannot reproduce. It caught real errors in our own
frozen scripts, such as a fixed t value used for every n. Those were corrected
in the paper, and the frozen artifacts were left untouched.

**12. Is a negative result publishable?**

Yes, especially with this level of rigour: preregistration, failed gates
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
