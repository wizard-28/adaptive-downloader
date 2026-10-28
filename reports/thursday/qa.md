# Viva questions, answered the way you would say them

Read these out loud. Each answer is short and uses round numbers, because
nobody says "0.3756 with a 95 percent interval" in a conversation. If she wants
exact numbers, they are in the paper and in `qa_detailed.md`. Use "learning
controllers", not "AI". If you do not know something, say "we did not test that"
and do not guess.

Words used here: **N** is how many connections download at once. **S** is how
big each piece of the file is. A **fixed setting** keeps the same N and S for the
whole download. The **perfect controller** is an imaginary one that knows the
future, so it shows the most any real controller could gain.

---

**1. So the learning controllers failed. Why should we care?**

Our question was never "can machine learning win?" It was "is there anything for
any adaptive controller to gain?" We measured that first. On recorded mobile
networks, even a perfect controller that knows the future gained less than half
a percent. So the answer is that there is almost nothing to gain, and that is
useful to know before anyone builds a smarter downloader.

**2. Why did the learning controllers lose?**

Three reasons. First, the ceiling was very low, and no controller can beat the
ceiling. Second, gains we saw in the simulator did not carry over to the real
server. Third, where there was real room to gain, a very simple rule already got
it, so the learner had nothing extra to find. A better learner cannot beat a
ceiling that is close to zero.

**3. Why only N and S? Maybe another setting would work.**

N and S are the two settings that segmented downloaders like aria2 actually
expose. We fixed that scope before we started, because a narrow study lets you
say what caused what. Other things, like choosing between mirrors or restarting
slow requests, are a different question, and we list them as future work. We do
not claim learning can never help downloading.

**4. Did you explore reinforcement learning fully?**

No, and we do not claim that. We tested two kinds of learner: a simple one
called LinUCB, and a neural network called Double DQN, in several versions. Our
conclusion rests mainly on the ceiling we measured before training anything, not
on having tried every learner.

**5. Isn't this obvious?**

The idea is intuitive: adapting only helps if the best setting moves around. But
some of what we found was not obvious. Mobile network speed swings by 6 to 25
times, yet the best setting hardly changes. Our big win over aria2 came from when
requests start, not from adapting. And aria2's lead under the throttling server
came from two weaknesses in our own downloader, not from smarter control.

**6. Networks change all the time. Why don't fixed settings lose?**

On one path, the best number of connections is roughly "enough to fill the pipe".
When the network slows down, everything slows down together, and the same setting
is still about right. Adapting only pays when too many connections carry a cost
that changes over time, like a server that throttles each client. We checked
shared links and bursty loss and found nothing to gain. Only the throttling server
we built showed a real benefit.

**7. What about real networks? Everything is on one laptop.**

That is true, and we list it as a limit. We did three things about it. We used
recorded real mobile traces, replayed so they are repeatable. We ran a small
check on four real public servers, at different times of day, from one home
connection. On average one simple fixed setting was within about 4 percent of the
best setting for each case. That pilot is small and descriptive. It is not a
proof.

**8. Is the big win over aria2 real?**

Yes, on that test: about 60 percent less time, so roughly a third of aria2's
time, in every one of 30 paired runs. But it is not because we adapt. aria2
starts its big pieces while the server is slow, and each piece keeps that slow
speed. Our smaller pieces start later, after the server speeds up. Even when we
match the piece sizes we are still faster by about 14 percent, and we cannot fully
explain that part, and we say so.

**9. aria2 beat you under the throttling server. Doesn't that undermine your result?**

It taught us something. We found two weaknesses in our own downloader. One: a
30-second time limit that cancelled slow but working requests. Two: no splitting
of slow pieces near the end, which aria2 does. After fixing both, we matched aria2
or came within about 2 percent of it. And on the fixed engine a good fixed setting
also beat our adaptive controllers, so we say plainly that adapting did not win
there.

**10. You built the server yourselves. Isn't the positive result rigged?**

It is a server we designed, and we say so. We built it so that a case exists where
the best setting changes, because otherwise adapting could never help. It shows
when adapting can help, not that real servers behave this way. That is why we also
tested on recorded mobile networks and real public servers.

**11. How do we know your numbers are right?**

A script recomputes every number in our paper, more than two hundred of them, from
the raw run files, and it fails if a number does not match. It even caught
mistakes in our own earlier scripts, which we corrected. Every downloaded file is
also checked byte for byte.

**12. Is a negative result worth publishing?**

Yes, when it is done carefully. Our plans were written down and fingerprinted
before running, failed checks are reported as failed, and the numbers are
reproducible. To be honest, that is not an independent registration, and one group
of test workloads was changed twice before its runs. The paper says so.

**13. What would you do with more time?**

Try harder cases where the best action has a changing cost: sharing a download
across several mirrors, restarting slow requests, and testing live on many more
networks. Each would first have to show there is something to gain, and then beat
the best fixed setting.

**14. What surprised you most?**

Two things. The mobile traces swing wildly, yet a perfect controller could barely
improve on one fixed setting. And one big public mirror, in China, refuses
downloads that come from Go's default client name, so a plain Go downloader is
blocked there.

**15. You fixed the engine. Did adapting still help?**

Not against the best fixed setting on the fixed engine. Our simple drop rule was
about 7 percent slower than the best fixed setting, and our workload selector was
about a quarter slower. The earlier gain of about a quarter was against small-piece
fixed settings on the older engine. We report both. The lesson is "engine before
policy": the time limit and splitting slow pieces mattered more than any
controller.

**16. You cite FastBioDL. Did you compare against it?**

We rebuilt its idea ourselves, nudging the number of connections up or down based
on measured speed, and tested it properly. It was about 20 percent slower than a
fixed setting with the same piece size. It reacted to the throttle within about
two seconds, but it spent about eight seconds climbing back up when there was no
throttle. We did not use their code.

**17. You retrained the agent on the throttling server. Isn't that tuning to the test?**

That is exactly why we tested it two ways. First on the server it trained on, where
a win only shows it learned that server. Then on conditions it had never trained
on. On the server it trained on it was about 9 percent faster than the fixed setting
with the same piece size. On three conditions outside its training range it did not
win any. So it learned its own server and did not generalise there, and we say that
plainly.

**18. Why did the first agent do badly on the capped server?**

We found two mismatches between our simulator and the real server. The simulator
had no cap that applies when a request arrives, and the speed number the agent saw
did not match what the real runner measures. So the agent could hardly see the
effect of extra connections. We rebuilt the simulator to copy the real
measurements, and checked it against fresh real runs, with the predictions written
down first.

**19. Can the retrained agent beat 32 MiB pieces?**

We said in advance that it cannot. With 16 big pieces, all of them start before
the throttle and keep the fast speed. An agent that only changes the number of
connections with small pieces cannot copy that. So the fair test is against the best
fixed setting with the same small piece size.

**20. Why not IDM, axel or wget2?**

IDM is closed and Windows-only, so we could not script it for hundreds of random
runs. We did run axel and lftp. wget2 cannot download one file in parallel (we
checked its source), so it was left out. axel and lftp behaved like aria2: they
split the file once at the start, so they also took about three times as long as
our fixed setting on the speed-up server, and level with everyone under the
throttle. lftp was much slower when the throttle was on from the start, because it
never re-splits a slow piece.

**21. Why not let the agent choose the piece size too?**

We tried it, with the plan frozen in advance. In the simulator there was real room
to gain. The agent did not find it. On the real server it was slower than the best
fixed setting in every case. A control experiment showed the big-piece advantage
depends on a rule in our server: a request keeps the speed that was in force when
it started. So the honest result is that a joint agent trained on a checked
simulator did not beat the best fixed setting.

**22. Does the learning agent generalise?**

Only partly. The first retrained agent did not win on any condition outside its
training range. The second one, trained more broadly, won on three of four new
combinations inside its training range, and lost on one by less than one percent.
But it is one agent chosen on validation, and the simple drop rule still beat it on
the cases the rule was tuned for. And it costs about 5 percent when there is no
throttle. "Generalises" here means inside the range it saw, not new kinds of
conditions or real servers.

**23. Did you run the adaptive agents on real servers?**

Yes, as a no-harm check on three public mirrors. The first attempt was ruined by an
outage on our own internet link, and we report that. On the clean rerun neither the
agent nor the drop rule passed our no-harm test on any of the three mirrors. We think
they mistook noise for a throttle and cut connections for no reason, though we did
not analyse that. It is the same lesson as the mobile traces: a network that varies
is not a network where adapting helps.

**24. Your first proposal said a joint (N, S) action at a fixed interval, and PPO. What did you build?**

Review 1 described the idea. In the build, we changed how decisions are triggered
and what one action does, for reasons we recorded: cleaner measurement, clean
comparisons, and early learning failures. Our reports should have said so.
- **When it decides:** after each finished request, not on a timer. A finished
  request is the clean moment to change settings, because a running request is never
  resized. The cost is that with very large pieces, decisions are rare.
- **One dial at a time:** this lets us see which dial helped. N did the work: using
  N alone gave most of the gain, S alone made things worse than doing nothing. When
  we first let a learner choose piece size, it chopped the file into tiny pieces and
  took almost twice as long as aria2. We still adapt both dials. The drop rule picks
  the pair together and applies it in steps, and the joint agent we trained has both
  dials. We never built one learner that picks the pair as one action. Nothing forced
  that; it was a design choice, and it is future work.
- **PPO:** it appears in Review 1 only in the related-work table, not in our plan. We
  moved from LinUCB to Double DQN after LinUCB kept picking the same action. We did not
  try PPO, and we say that.

Review 3 marks the joint adaptation as achieved. The accurate reading is "both dials
adapted, never as one learned pair". Background traffic was screened and showed
nothing to gain. We did not measure fairness to other users.

**25. What is your main answer against aria2, in a sentence?**

Against aria2, our adaptive controllers were faster only when the server sped up
during the download, and even then a fixed setting of ours was faster still. When the
server got worse, aria2 was faster than every adaptive controller.

**26. Why did you pick those thresholds and sizes?**

Only say what we recorded:
- **The 10 percent bar for the perfect controller:** our run-to-run noise was about
  1 to 5 percent, so under 5 percent is too weak to matter and 10 percent is
  meaningful. We set it before we looked.
- **A 512 MiB file:** the biggest size that is safe on memory in our container, and
  it still runs long enough for conditions to change.
- **The throttling server's settings:** of 27 combinations we screened, exactly one
  met our rule that the best N and the best S both move.
- **If she asks why 16 connections, why those numbers of rounds, why a 64 MB file in
  the live check, or why loopback:** say honestly that we do not have a recorded
  reason, and do not make one up.

**27. How does your adaptation actually work?**

After every finished request, the controller looks at the recent speeds, chooses one
action (do nothing, more or fewer connections, bigger or smaller pieces), and
later gets a score. Our simple drop rule remembers the best recent speed. When the
speed falls below half of that, it switches once. In our timeline, it cuts the
connections from 16 to 4 about a second and a half after the throttle starts, then
raises the piece size step by step. That is the picture on the results slide.

**28. What do the learning controllers see and do?**

The simple one, LinUCB, sees about ten numbers, such as the current settings, the
speeds, how long the last request took, and counts of failures and retries, and it
picks one of five actions. The neural network, Double DQN, sees about thirteen
numbers and picks one of three actions: nothing, more connections or fewer. Its
reward is minus the time until the next request finishes, so a faster download scores
higher. We did not tune any reward weights.

**29. What are the limits of your study?**

The servers where adapting helped are ones we built. Everything controlled ran on one
machine. The mobile traces replay only capacity, not delay or loss, and we never
changed delay or loss mid-download. The real-server check was small, from one home
connection. The best agent is one training run. We measured speed, not fairness to
other users. And we did not correct for running many comparisons.

**30. What is new in your work?**

Not a new algorithm. Others tune the number of connections. In the papers we read, we
did not find these together: a race against aria2, axel and lftp and the best fixed
setting; a ceiling measured before building a controller; the engine's effects
separated from the controller's; and negative results kept. We read abstracts, and
the full text only for one of them, so we say "in what we read", not "nobody has done
it".

---

**Words to avoid:** "AI" (say learning controllers), "nobody has done this" (say "in
the papers we read, we did not find"), "the learner was bad" (say "there was little to
gain").

**Exact numbers** for any answer above are in `qa_detailed.md` and the paper.
