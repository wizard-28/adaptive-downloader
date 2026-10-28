# Viva questions, short spoken answers

Say these in your own words. Exact numbers are in the paper and `qa_detailed.md`.
Say "learning controllers", not "AI". If you do not know, say "we did not test that".

**N** is how many connections download at once. **S** is how big each piece is.
A **fixed setting** keeps the same N and S for the whole download. The **perfect
controller** is an imaginary one that knows the future, so it shows the most any
real controller could gain.

---

**1. So the learning controllers failed. Why should we care?**

Our question was whether there is anything to gain by adapting, not whether
learning can win. We measured that first: on recorded mobile networks, even a
perfect controller gained less than half a percent. Knowing there is almost
nothing to gain is useful before anyone builds a smarter downloader.

**2. Why did the learning controllers lose?**

There was very little to gain, and a better learner cannot beat a ceiling near
zero. Where there was something to gain, a very simple rule already got it.

**3. Why only N and S?**

They are the two settings real segmented downloaders expose, like aria2's
connections and piece size. A narrow scope lets us say what caused what. Other
ideas, like choosing between mirrors, are future work.

**4. Did you try every learning method?**

No. We tested two: a simple one called LinUCB and a neural network called Double
DQN. Our conclusion rests mainly on the ceiling we measured, not on trying every
learner.

**5. Isn't this obvious?**

The idea is. Some results were not: mobile speed swings by 6 to 25 times yet the
best setting barely moves, and our big win over aria2 came from when requests start,
not from adapting.

**6. Networks change all the time. Why don't fixed settings lose?**

When a network slows down, every setting slows down together, so the same setting
stays about right. Adapting only pays when extra connections carry a cost that
changes over time, like a server that throttles each client.

**7. Everything is on one laptop. What about real networks?**

That is a limit, and we say so. We also replayed recorded real mobile networks and
ran a small check on four real public servers from one home connection. One simple
fixed setting was within about 4 percent of the best setting on average. That check
is small and descriptive, not a proof.

**8. Is the big win over aria2 real?**

Yes: about 60 percent less time, every time in 30 paired runs. But it comes from
when requests start, not from adapting. aria2 starts its big pieces while the server
is slow, and they keep the slow speed. We also cannot fully explain a small part of
the advantage, and we say so.

**9. aria2 beat you under the throttling server. Doesn't that hurt your result?**

It taught us something. Our own downloader had two weaknesses: a 30-second time limit
and no splitting of slow pieces near the end. After fixing them we matched aria2.
Then a good fixed setting beat our adaptive controllers, and we say adapting did not
win there. The lesson is: fix the engine before adding intelligence.

**10. You built the server yourselves. Isn't the positive result rigged?**

It is a server we designed, so that a case exists where the best setting changes.
It shows when adapting can help, not that real servers behave that way. That is why
we also used recorded mobile networks and real public servers.

**11. How do we know your numbers are right?**

A script recomputes every number in the paper, more than two hundred, from the raw
run files. Every downloaded file is also checked byte for byte.

**12. Is a negative result worth publishing?**

Yes, if it is careful. Our plans were written down and fingerprinted before running,
failed checks are reported as failed, and the numbers can be reproduced. It is not an
independent registration, and we say so.

**13. What would you do with more time?**

Harder cases: sharing a download across mirrors, restarting slow requests, and live
tests on many more networks.

**14. What surprised you most?**

That mobile networks swing wildly, yet a perfect controller could barely improve on
one fixed setting.

**15. After the engine fix, did adapting help?**

No. Against the best fixed setting, our simple drop rule was about 7 percent slower.
The earlier gain was only against small-piece fixed settings on the older engine.
Engine before policy.

**16. Did you compare against FastBioDL?**

We rebuilt its idea ourselves, nudging the number of connections by measured speed.
It was about 20 percent slower than a fixed setting with the same piece size. We did
not use their code.

**17. You trained the agent on the throttling server. Isn't that tuning to the test?**

We tested it on the server it trained on and on conditions outside its training
range. On its own server it was about 9 percent faster than the fixed setting with
the same piece size. On conditions outside its range it did not win. A broader-trained
agent won on three of four new conditions inside its range. So it generalises only
inside what it was trained on.

**18. Why not IDM, axel or wget2?**

IDM is closed and Windows-only, so we could not script it. We ran axel and lftp: like
aria2 they split the file once at the start, so they also took about three times as
long as our fixed setting when the server sped up. wget2 cannot download one file in
parallel.

**19. Why not let the agent choose piece size too?**

We tried it, and it did not beat the best fixed setting on the real server. The
big-piece advantage depends on a rule in our server: a request keeps the speed that
was in force when it started.

**20. Do the learning controllers help on real servers?**

No. On three real public mirrors, neither the learning agent nor the drop rule passed
our no-harm test. A network that varies is not a network where adapting helps.

**21. You proposed a joint (N, S) action at fixed intervals, and PPO. What did you build?**

Review 1 described the idea. In the build, we changed how decisions are triggered and
what one action does, for reasons we recorded: cleaner measurement, clean comparisons,
and early learning failures. Our reports should have said so.
- **Timing:** we decide after each finished request, because that is the clean moment
  to change settings.
- **One dial per action:** it shows which dial helped. N did most of the work; changing
  S alone made things worse than doing nothing. Early learners that chose piece size
  broke the file into tiny pieces. We still adapt both dials, but never as one learned
  pair. Nothing forced that; it was a design choice, and it is future work.
- **PPO:** it was never promised in Review 1. We used Double DQN after LinUCB kept
  picking the same action. We did not try PPO.

**22. What is your main answer against aria2?**

Our adaptive controllers were faster only when the server sped up, and even then a
fixed setting of ours was faster still. When the server got worse, aria2 was faster
than every adaptive controller.

**23. How does your adaptation work?**

After each finished request, the controller looks at recent speeds, picks one action
(nothing, more or fewer connections, bigger or smaller pieces) and later gets a score.
The simple drop rule remembers the best recent speed; when speed falls below half of
that, it switches once, cutting connections from 16 to 4 and then raising the piece
size step by step. That is the timeline on the results slide.

**24. What do the learning controllers see and do?**

LinUCB sees about ten numbers (settings, speeds, request time, failures) and picks
one of five actions. The neural network sees about thirteen numbers and picks one of
three: nothing, more connections or fewer. Its reward is minus the time until the next
request finishes, so faster scores higher.

**25. What are the limits?**

The servers where adapting helped are ones we built. Everything controlled ran on one
machine. The mobile traces replay only speed, and we never changed delay or loss
mid-download. The real-server check was small. The best agent is one training run. We
measured speed, not fairness to other users.

**26. What is new?**

Not a new algorithm. In the papers we read, we did not find these together: a race
against aria2, axel and lftp and the best fixed setting; a ceiling measured first;
engine effects separated from the controller; and negative results kept.

**If she asks why 16 connections, why that number of rounds, why a 64 MB file in the
live check, or why loopback:** say honestly that we do not have a recorded reason.
