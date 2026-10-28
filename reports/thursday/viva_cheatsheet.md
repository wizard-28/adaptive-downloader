# Viva cheat sheet, in simple words

Everything here is checked against the paper (reports/paper_draft.md). Spoken-word
answers to 30 questions: reports/thursday/qa.md; exact numbers: qa_detailed.md. Slides: https://claude.ai/artifact/RnC4UyEgdPMmsZBPYjcF7J

## The whole project in 30 seconds

Download tools like aria2 pick two settings at the start and never change them:
**N** (how many connections) and **S** (how big each piece is). We built a
downloader that can change N and S while the download runs, and asked one
question: **does that help when conditions are not ideal?** We tested it against
aria2, axel, lftp and our own best fixed setting. Short answer: **rarely.**
Adapting helped only when the server speeds up, and even then a fixed setting of
ours did better. How the downloader is built mattered more than how clever the
controller is.

## Numbers to remember

| Number | What it is |
|---|---|
| **62.4 %** | less time than aria2 with one fixed setting, on a server that starts slow then speeds up. Mostly request timing, not adapting. |
| **0.41 %** | the most a perfect controller (one that knows the future) could gain on 5 recorded mobile traces, even though speed swings 6.5 to 25 times |
| **23.4 %** | faster than the best fixed setting we tried, using the drop rule under a throttling server. But aria2 was still faster, and after we fixed our engine a fixed setting beat the drop rule. |
| **1.071** | the drop rule's time divided by the best fixed setting's time, after the engine fix: 7 % slower |
| **0.907** | the retrained learning agent's time divided by the fixed 1 MiB setting, on the server it trained on: 9 % faster |
| **3.7 %** | on 4 real public servers, one simple fixed setting (8 connections of 1 MiB) was on average this much slower than the best fixed setting for each case |
| **231** | numbers in our paper, all recomputed from the raw data by a script |

## The questions you will probably get

**What is N and S?** N is how many connections download at once. S is the size of
each piece of the file. Think of N as the number of lanes on a road and S as the
size of each truck.

**What did you build?** A downloader in Go where a "policy" can change N and S
mid-download, a test server where we control exactly how fast it answers, and a
fair way to compare: every option runs back to back in random order, plans are
written down before running, and a script recomputes every number.

**How does your adaptation actually work?** After every finished request the
policy looks at recent speeds, picks one action (nothing, N up or down, S up or
down) and later gets a score. Example, the drop rule: it remembers the best recent
speed; when the speed falls below half of that, it switches once, cutting N from 16
to 4 in one decision and then raising S from 1 to 4 MiB, one step at a time. That
is the timeline on slide 8.

**Did you beat aria2?** Only in one case. When the server sped up during the
download, our adaptive controllers were faster than aria2 (time ratios 0.36 to
0.88), but our fixed setting was faster still (0.344 to 0.376). When the server got
worse, aria2 beat every adaptive controller (1.096 to 1.781).

**Is the 62.4 % real? Why were you so much faster?** It is real, on that server.
But it is not because we adapt. aria2 starts its big pieces while the server is
slow, and each piece keeps the slow speed it started with. Our smaller pieces
start later, after the speed-up. We also found about 14 % that we cannot fully
explain, and we say so.

**aria2 beat you under the throttling server. Doesn't that kill your result?**
It showed us something. We found two weaknesses in our own engine: a 30-second
time limit that cancelled slow but working requests, and no splitting of slow
pieces at the end (aria2 does that). After fixing both, we matched aria2 or came
within 1.6 %. So the lesson is: fix the engine before adding intelligence.

**Why a "perfect controller"?** To measure the ceiling before building anything.
If even a controller that knows the future gains almost nothing, no real
controller can. We decided in advance that 10 % would be worth pursuing, because
normal run-to-run noise is about 1 to 5 %. The result was 0.41 %.

**Why didn't you choose both N and S together, as in Review 1?** We did adapt
both, but one dial per decision. Reasons: one dial at a time lets us see which dial
helped (N-only 0.808, both 0.766, S-only 1.070 of the fixed time; N did the
work). S only changes future pieces, N changes workers now, so keeping them
separate is safer. And when we first let the learner choose piece size, it cut the
file into tiny pieces and was 75.7 % slower than aria2. Nothing forced it. It was a
design choice. A learner choosing the pair as one action was not built; it is
future work.

**Review 1 also said fixed time intervals and PPO.** Review 1 described the idea.
In the build, we changed how decisions are triggered and what one action does, for
reasons we recorded: cleaner measurement, clean ablations, and early learning
failures. Our reports should have said so. We decide after each finished request
(the clean moment to change settings). PPO was never promised in Review 1; we used
Double DQN after a simpler learner (LinUCB) got stuck on one action. We did not
try PPO.

**What do the learning controllers see and do?** LinUCB sees 10 numbers (N, S,
speeds, request time, bytes done, counts of failures and retries) and picks one of
5 actions. The Double DQN sees 13 numbers and picks one of 3 actions (nothing, N
up, N down; S fixed at 1 MiB). Its reward is minus the seconds until the next
request finishes, so a faster download scores higher.

**Why did the learning agents lose?** The problem was not the learner, it was the
opportunity. On the mobile traces there was almost nothing to gain. Where there
was something to gain (the throttling server), a simple rule already captured it.
One retrained agent did beat the fixed 1 MiB setting where it trained (0.907) and
on 3 of 4 new conditions inside its training range, but not outside it, and not
against fixed 32 MiB pieces.

**You built the server yourselves. Isn't it rigged?** We built it so a state-
dependent best setting exists, otherwise adapting could never help. We say so.
That is why we also tested on recorded mobile networks and on real public servers.

**Everything is on one laptop. What about real networks?** True, it is a
limitation. We added a small real-server check: 4 public servers, 8 times of day,
from one home connection. Adaptive controllers failed our "no harm" test on all
three mirrors we ran them on. This is small and descriptive, not a proof.

**How do we know the numbers are right?** A script (`tools/claims_check.py`)
recomputes all 231 numbers in the paper from the raw run files. Plans were written
and fingerprinted before the runs (but not registered with an outside service, and
one set of workloads was amended twice; the paper says so). Every downloaded file
is checked byte for byte.

**What is new?** Not a new algorithm. Others tune the number of connections. In
the papers we read, we did not find: a race against aria2, axel and lftp together
with the best fixed setting; a ceiling measured before building a controller;
engine effects separated from the controller; and negative results kept. We read
abstracts, and full text only for FastBioDL, so we say "in what we read".

**Why not axel, wget2, IDM?** We ran axel and lftp. wget2 cannot download one file
in parallel (we checked its source). IDM is closed and Windows-only, so we could
not script it.

**What does the demo show?** (Slide 7) A real download from cdn.kernel.org with
8 connections of 1 MiB, then the retrained agent on our throttling server (it
lowers N over the next seconds, a little differently each run), then our tests and
the number checker. If it fails, slide 8's timeline shows the same thing.

**What are the limits?** Constructed servers; one machine over loopback; only
capacity was replayed from the mobile traces; we never changed delay or loss mid-
download; one client for the real-server check; one chosen training run for the
agent; speed measured, not fairness to other users; no correction for many
comparisons.

**What would you do next?** A learner that picks (N, S) as one action; live tests
where delay and loss change during a download; multi-mirror scheduling and
restarting slow requests; a stall timeout; fairness; more real data.

## Things to say honestly if asked

- "We did not try PPO."
- "We did not run a learner that chooses N and S as one action."
- "We do not know why 14 % of our advantage over aria2 remains unexplained."
- "These results are from a controlled setup, not production servers."
- If you do not know: "We did not test that, and we would not claim it."

## Do not say

- "Nobody has done this before." Say "in the papers we read, we did not find".
- "AI". Say "learning controllers".
- "The learner failed because it was bad". Say "there was little for any
  controller to gain".
