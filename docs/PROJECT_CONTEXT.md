# PROJECT_CONTEXT — index

This is the living handoff document for the repository. **It is now an index.**
The full text (about 650 KB, kept verbatim and numbered as before) is split into
the files below under `docs/context/`, so that a search or a read loads only
the part you need. Section numbers such as "§22.109" are unchanged everywhere
(code comments, reports, the paper, the handbook): use the finder to locate them.

## How to find things

```
python3 tools/context_find.py 22.109      # file:line of "### 22.109 ..."
python3 tools/context_find.py 20          # file:line of "## 20. ..."
python3 tools/context_find.py -s lock-in  # case-insensitive search of all context files
```

Then read only that range (Read with offset and limit, or `sed -n 'A,Bp'`).
Rules of thumb: architecture, invariants and semantics are in files 00–04;
anything that happened (experiments, amendments, results, the paper, the deck)
is a numbered entry of §22 in files 05a–05h, newest entries last (22.109–22.111
come before 22.96–22.108 inside 05g/05h); the current next step is in 09.

## Files

| File | Contents | Size |
|---|---|---|
| `docs/context/00-overview-and-structure.md` | Header, §1 project overview, §2 research positioning, §3 repository structure | 10 KB |
| `docs/context/01-architecture-and-invariants.md` | §4 architecture, §5 download data flow, §6 range semantics, §7 retry/in-flight invariants, §8 controllers, §9 worker pool | 8 KB |
| `docs/context/02-policies-runner-metrics.md` | §10 policy interface, §11 policy runner, §12 existing policies, §13 metrics | 6 KB |
| `docs/context/03-experiment-infrastructure.md` | §14 experiment server, §15 tc/netem, §16 modes, §17 matrix, §18 baseline dataset, §19 trajectory semantics | 12 KB |
| `docs/context/04-rl-infrastructure.md` | §20 RL infrastructure, §21 decision timing and credit assignment | 13 KB |
| `docs/context/05a-status-22.1-to-22.13.md` | §22 running status log (RL status, LinUCB, benchmarks): 22.1 … 22.13 | 61 KB |
| `docs/context/05b-status-22.14-to-22.26.md` | §22 running status log (oldest first, except 22.109–22.111 which precede 22.96–22.108): 22.14 … 22.26, 13 entries | 57 KB |
| `docs/context/05c-status-22.27-to-22.39.md` | §22 running status log (oldest first, except 22.109–22.111 which precede 22.96–22.108): 22.27 … 22.39, 13 entries | 57 KB |
| `docs/context/05d-status-22.40-to-22.58.md` | §22 running status log (oldest first, except 22.109–22.111 which precede 22.96–22.108): 22.40 … 22.58, 19 entries | 57 KB |
| `docs/context/05e-status-22.59-to-22.69.md` | §22 running status log (oldest first, except 22.109–22.111 which precede 22.96–22.108): 22.59 … 22.69, 11 entries | 59 KB |
| `docs/context/05f-status-22.70-to-22.81.md` | §22 running status log (oldest first, except 22.109–22.111 which precede 22.96–22.108): 22.70 … 22.81, 12 entries | 60 KB |
| `docs/context/05g-status-22.82-to-22.95-and-22.109-to-22.111.md` | §22 running status log (oldest first, except 22.109–22.111 which precede 22.96–22.108): 22.82 … 22.109, 17 entries | 58 KB |
| `docs/context/05h-status-22.96-to-22.108.md` | §22 running status log (oldest first, except 22.109–22.111 which precede 22.96–22.108): 22.108 … 22.96, 13 entries | 49 KB |
| `docs/context/06-test-coverage.md` | §23 test coverage by subsystem | 15 KB |
| `docs/context/07-completed-milestones.md` | §24 completed milestones | 58 KB |
| `docs/context/08-design-decisions-and-caveats.md` | §25 important design decisions, §26 known caveats | 6 KB |
| `docs/context/09-next-step-and-handoff.md` | §27 current next step, §28 how to continue from another client | 38 KB |

Reassembling `docs/context/*.md` in name order gives back the former single
`docs/PROJECT_CONTEXT.md` byte for byte (checked when it was split, 2026-10-28;
the old file is in git history).

## Where things stand (2026-10-28)

All experiments are finished. The viva is Thursday 2026-10-29; what remains is
presentation material and answers. Open items: handbook swing wording ("about
5–27×", change only when the user asks), IEEE author details (ORCID, funding,
conflicts). Details and the latest entries: §22.109 (DQN retrained for the
capped server and follow-ups), §22.110 (team handbook), §22.111 (paper, decks,
IEEE templates). The work queue is `queue.md`; the presentation is two
Slides decks (`reports/deck12/` the official 12 slides, `reports/deck/` the long
deck); the paper is `reports/paper_draft.md` with PDFs from
`tools/paper_to_pdf.py` and `tools/paper_to_ieee.py` (`--conf` for the
conference template).

## Maintaining it

- Edit the file that holds the section you change. Add new status entries as
  new `### 22.N ...` headings at the end of the last `05*` file (start a new
  `05i-...` file when it passes about 60 KB), and add the file to the table above.
- Keep section numbers stable; do not renumber.
- Keep this index short: it points, it does not repeat.
