# Plan: a controller that chooses the pair (N, S) as one action

Written 2026-10-28 as a handoff note before compacting the session. Resume by reading
this file, `queue.md`, `AGENTS.md` and `docs/PROJECT_CONTEXT.md` (an index; find sections
with `python3 tools/context_find.py 22.109`). Work on a branch, `exp/joint-pair`, off the
current `main`. Do not touch the published decks or the paper until Phase 4.

## Why

Review 1 promised a joint action a_t = (N_t, S_t), chosen together at every control
interval. We built one dial per action. Review 3 marked "joint adaptation" Achieved. The
honest answer is available (both dials were adapted, never as one learned pair; reasons
recorded in qa.md Q21 and the paper limitations), but it reads as an unmet promise. One
small, bounded study converts "we never tried it" into "we tried it, and here is what it
showed". Either outcome is acceptable; report whatever happens.

## Design (smallest faithful version)

1. **Pair action.** New opt-in kind `ActionSetPair` in `internal/policy/policy.go`. The
   `Action` struct has `Kind` and `Value int64` only: add one field for the second value
   (for example `Chunk int64`, zero for every existing kind). `Validate` requires both
   positive. `policyRunner.applyAction` (`internal/downloader/policy_runner.go`, around
   line 280) applies the pair in one decision: set the concurrency target (the N
   controller `internal/concurrency` has Increase/Decrease; check for a `Set`, add one
   with bounds if missing; `internal/chunksize` already has `Set(target)`) and set the
   chunk-size target; notify the worker pool once. In-flight ranges stay immutable;
   chunk size still affects future allocations only. Existing kinds and all existing
   studies are unchanged. This is a deliberate, documented exception to the one-dial rule
   in `AGENTS.md` section 6: add a paragraph there and in `docs/context/` (section 25).
2. **Pair-choosing controller.** A LinUCB-style bandit whose arms are preset pairs. Reuse
   the LinUCB core (`internal/rl/linucb.go`: `LinUCBConfig.NumActions` already takes any
   arm count; it uses the 10-feature state of `internal/rl/state.go` and the composite
   reward of `reward_calculator.go`). Add a thin wrapper that maps arm index to
   `SetPair(N, S)`; do not copy the algorithm. Predeclared arms (6):
   (16, 1 MiB), (16, 4 MiB), (16, 32 MiB), (4, 1 MiB), (4, 4 MiB), (4, 32 MiB). Start arm:
   (16, 1 MiB), as the other adaptive arms start. Decision: after each completed request,
   as everywhere. Alpha and warm-up predeclared in the protocol, not tuned afterwards.
3. **Harness.** Add `-policy pairbandit` to `testdata/rl_joint/tools/joint_bench.go` (it
   already has the capped server, `-cap-at 8`, `-cap-gamma 0.05`, `-split`,
   `-no-request-timeout`, netem via `sudo -n`, aria2 and fixed arms, decision logs). New
   study folder `testdata/joint_pair/` modelled on `testdata/rl_joint/` (protocol.md,
   manifest.json, tools, analysis, runs, index, log).
4. **Experiment (frozen before any run).** Scenario: cap from 8 s (primary), corrected
   engine (splitting on, no request deadline), 512 MiB. Arms: pair bandit; fixed
   16 x 32 MiB (the best fixed); fixed 16 x 1 MiB; the causal drop controller
   (N 16 -> 4, S 1 -> 4 MiB); aria2 with the frozen flags. 10 blocks, shuffled, seeded.
   Optional second scenario, no cap, for the no-harm side (5 blocks). Primary
   comparison: pair bandit time divided by best fixed time (geometric mean, 95 %
   interval); "not distinguishable" if the interval includes 1. Frozen rules, hash
   recorded before the first run, amendments dated at the end, failed runs kept.
5. **Analysis.** Follow `testdata/rl_joint/analysis` conventions; output JSON feeds
   `tools/paper_numbers.json` and `tools/number_sources.json` so `claims_check.py` covers
   every new number (text anchors for headline numbers).

## Phases (commit at the end of each; the repo is green at every commit)

| Phase | Work | Done when |
|---|---|---|
| 1 | `ActionSetPair`, runner support, `Set` on the N controller if missing, unit tests (pair applied atomically, bounds clamp, in-flight unchanged, existing kinds unchanged), docs/AGENTS note | `go build ./...`, `go vet ./...`, `gofmt -l internal cmd`, `go test -p 1 -count=1 ./...` all pass |
| 2 | Pair-bandit wrapper with tests; `-policy pairbandit` in the harness; study folder; protocol.md, manifest, analysis script; smoke run of 2 short runs; hash and freeze the protocol | smoke run valid; protocol committed before any evaluation run |
| 3 | Evaluation runs (background, machine idle, one timing job at a time, never during a demo rehearsal); analysis; decision logs kept | all runs valid or invalid ones documented; analysis JSON committed |
| 4 | Paper (a new section after 9e or inside 9c: what it is, result, caveats; abstract and contributions only if the result warrants), limitations and future-work wording, `qa.md` Q21 update, deck12 notes (slides 6, 13, 15, wording only; no new slide, deck limit is 15), `claims_check.py` anchors, rebuild PDFs, publish deck12 | `python3 tools/claims_check.py` all ok, 0 unverified; `python3 tools/test_claims_check.py` all PASS; paper PDFs build; decks render |

Stop and keep the previous state if Phase 4 cannot finish by Thursday evening
(the review is Friday 5 PM; no changes after Friday 12:00). Merge to `main` only when all
checks above pass.

## Safeguards

- Never edit frozen studies or their analysis; add new files only.
- Do not run timing-sensitive jobs while the user rehearses the demo; check for other
  live runs first (`pgrep -fa "joint_bench|ns_bench|go test"`).
- Use the loopback with `sudo -n tc` as the other capped studies do; clean up netem after
  each run (AGENTS section 10).
- No unsupported claims: report the result as measured, scoped to this server model.
- Keep slides within 15; no internal code names on slides; say "learning controllers".
- Subagents, if used, must be Sonnet and read-only unless the task is an isolated edit.

## Budget gating

The user wants about 5 % of weekly usage left. Stop at a phase boundary when the user
says so; every phase ends committed and green, so stopping is safe. Phase 1 is the first
checkpoint: report what it cost before starting Phase 2.

## Where things are

- Action and runner: `internal/policy/policy.go`, `internal/downloader/policy_runner.go`.
- Controllers: `internal/concurrency`, `internal/chunksize`.
- LinUCB, state, reward: `internal/rl/linucb.go`, `state.go`, `reward_calculator.go`.
- Capped-server harness and the model protocol: `testdata/rl_joint/` (`tools/joint_bench.go`,
  `protocol.md`).
- Paper: `reports/paper_draft.md` (sections 9a to 9e, 10); Q&A: `reports/thursday/qa.md`
  and `qa_detailed.md`; decks: `reports/deck12/` (published, artifact
  RnC4UyEgdPMmsZBPYjcF7J; README has the publish recipe).
- Standing preferences are in the memory notes under
  `/home/vscode/.claude/projects/-workspaces-mvp/memory/`.
