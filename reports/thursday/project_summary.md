# When does adaptive downloading help? A one-page summary

*Prepared 2026-10-18 for the Thursday 2026-10-22 project review. Numbers are
from `reports/paper_draft.md` (verified by `tools/claims_check.py`) unless
marked as live-pilot results.*

## Question

Segmented HTTP downloaders such as aria2 and IDM split a file into byte ranges
and fetch them in parallel. Two settings decide how: **N**, the number of
concurrent connections, and **S**, the range size. The usual story is that
networks change, fixed settings cannot adapt, and so an adaptive or learned
controller should win. The project asked a narrower question: **when is there
actually something for adaptation to exploit?**

## What we built

- **Go downloader.** Range tracker, fetcher, assembler, worker pool, and N and
  S controllers. Fixed rules, a LinUCB bandit and Double DQN agents all plug in
  through one policy interface, so every controller runs through the same
  machinery.
- **Controlled server.** Per-response pacing, wall-clock rate schedules, fault
  injection, and a soft cap under which each request's rate falls with
  concurrency.
- **Harness.** tc/netem delay and loss, Mahimahi replay of recorded cellular
  traces, aria2, curl and NGINX baselines, and a SHA-256 check of every
  output.
- **Method.** Preregistered gates and stop rules, frozen hashed artifacts, an
  oracle headroom bound computed before training any learner, and a claims
  checker that recomputes all 113 paper numbers from raw runs.

## Findings

1. **A fixed setting beat aria2 by 62.4 %, and adaptation was not the reason.**
   On one characterised workload, a fixed 16 × 4 MiB configuration had a
   geometric time ratio of 0.3756 against aria2 (30 of 30 pairs). The gap
   comes mainly from range granularity interacting with request-start timing
   and phase-locked pacing: it shrinks to 0.862 when granularity is matched.
   In screens, ours was faster in 28 of 30 pairs across six pacing schedules
   and in 10 of 10 against stock NGINX rate limiting.
2. **Variability is not opportunity.** The Mahimahi cellular traces swing
   5–27× in capacity. Even so, an oracle that knows the future and switches
   concurrency optimally gains only 0.41 % on average over five traces. The
   one-sided 95 % lower bound was −0.26 % against a predeclared 10 % bar, so
   the gate failed. This bound holds for *any* concurrency-switching policy.
3. **Adaptation helps when concurrency has a cost that changes over time.**
   Under a server cap where per-request rate falls with concurrency and the
   cap switches on mid-download:
   - a one-threshold causal controller beat the best of three fixed settings
     by 23.4 % (58.7 s vs 76.7 s, 10 of 10 blocks);
   - the N move carries the gain;
   - aria2 was still faster, because its early pieces start before the cap.

   Across six predeclared workloads, a frozen selector matched the
   per-workload best fixed setting on average (0.999).
4. **Every learned policy lost.**
   - Double DQN v2, v5 and v6 were 1.757×, 2.322× and 1.456× slower than
     aria2 in their head-to-heads.
   - LinUCB locked onto a single arm.
   - Several later environment designs failed their headroom gates before
     any training. One showed 17.6 % headroom in simulation that did not
     reproduce live.
   - A model-based attempt met 0 of 6 feasibility criteria.

## Why reinforcement learning did not win

The learner was not the bottleneck; the opportunity was. On one bottleneck
path, the best N is "enough connections to fill the pipe". When capacity
changes, you go faster or slower, but the same setting stays best. Where real
headroom existed, a simple rule that detects one change captured it.

## Live-internet pilot (complete, descriptive)

- **Design:** four public kernel.org mirrors (CDN, origin, Japan, China),
  reached over the author's own broadband. 8 fixed (N, S) cells plus aria2
  and curl, 2 reps each, in four time-of-day slots on the primary (Tata)
  network (`testdata/live_pilot_l1/`).
- **Result:** 328 of 350 runs were valid, giving 16 complete conditions. One
  global setting, 8 connections × 1 MiB, was on average 3.7 % slower than
  each condition's own best (bootstrap 95 % [1.0, 4.8]). The upper bound is
  below the predeclared 5 % line. The setting was within 5 % of the best in
  11 of 16 conditions, and at most 15.3 % slower in any one.
- **What mattered:** "enough connections for this server". The origin mirror
  needed N ≥ 8, and N = 4 was 1.37–1.71× slower there. The access link, at
  about 2.4 MB/s, capped every server.
- **Other network:** one morning slot ran on a second network by accident. It
  is reported separately: 7.2 % headroom [1.2, 12.2] over 4 conditions. That
  is inconclusive, but it is the one hint that the best setting can move
  more.
- **Side finding:** the Tsinghua mirror rejects Go's default User-Agent with
  HTTP 403.

## Scope and limits

- The server models are constructed.
- Experiments ran on one machine with loopback plus netem.
- The cellular traces were replayed, not measured on live mobile networks.
- Two learner families were tested.
- One aria2 version was tested.
- Live internet is covered only by the small pilot (one client, one primary
  network).

## Takeaway

> Fixed configurations cannot adapt. On the conditions we tested, they did not
> need to. Adaptation pays when concurrency has a cost that changes over
> time, and there a simple rule was enough.

**Future work:** action spaces with delayed, changing costs, such as
multi-mirror scheduling, restarting slow requests, straggler hedging, and live
networks at scale. Each must pass a headroom gate first and beat both the best
static setting and a strong myopic heuristic.
