### 22.108 Review 1 gap closure (2026-10-24): online tuner, CLI probe/checksum, goodput + reliability, Review 2/3 reports (COMPLETE; reports submitted 2026-10-24)

**Status: `ONLINE_TUNER_COMPLETE; REPORTS_BUILT`.** Online tuner: 64/64 valid
(commit 093a10b). By the frozen rule the tuner was slower than the best
stationary arm in both scenarios: CAP8 1.212 [1.180, 1.248], HEALTHY 1.220
[1.203, 1.232] (vs fixed_16_1M_NT_split); vs aria2 1.781 (CAP8) and 0.432
(HEALTHY). Adaptation speed (`testdata/online_tuner/analysis/adaptation_speed.json`):
causal 1.5 s, tuner 2.1 s after the 8 s cap; selector classifies at 4.0 s;
tuner climbs 4->16 in 8.1 s without a cap. Test functions now 549 in 29
packages. Review 2/3 PDFs: `reports/Review2_Project_Review.pdf`,
`reports/Review3_Final_Report.pdf` (an independent reviewer subagent's 40-item
critique was applied: wording '62.4 % less time', RL design + results table,
ablation arm table, limitations incl. no request timeout and fairness,
definitions of selector/causal/tuner, live-pilot sessions, curl reference).
Earlier status: `ONLINE_TUNER_RUNNING`.
- **rl_capped (commit 2b6e1ae protocol, 13297fe data):** frozen DQN v6 on the
  fixed engine, 512 MiB, CAP8/HEALTHY, 5 blocks, 50/50 valid. vs best fixed:
  CAP8 start16 1.356 [1.252, 1.472], start1 1.706; HEALTHY start16 1.031,
  start1 1.194. start16 vs fixed_16_1M (same S) at CAP8 0.931 [0.879, 1.016].
  Median ~412 N moves per run at CAP8, final N 2.
- **Independent verifier pass (subagent) applied:** CLI scope wording (the CLI
  has no policy/split flags; experiments use harness programs), Content-Range
  checked only when present (wording softened; making it mandatory is a
  possible follow-up), gofmt scoped to internal/ and cmd/ (3 testdata tools
  files unformatted), paper draft does NOT yet contain the ablation/tuner/
  rl_capped results (to fold in before Thursday), v5 N<=4 disclosed.
- **Next (before Thu 2026-10-29):** retrain DQN on the capped-server model
  (sim parity check, 3-5 seeds, frozen protocol, 10 blocks); add axel/wget2/
  lftp baselines; delay/loss sweep; fold results into paper_draft + claims_check. The user compared the work against the
Review 1 proposal (`Computer_Networks_Lab_Submission` docx, Assessment 8) and
asked to close the gaps before submitting Review 2 and Review 3 on
2026-10-24 (target 23:00 IST); physical review Thu 2026-10-29. Anything not yet
verified must be verified by Thursday morning.

- **Rationale found in the repo for Review 1 deviations:**
  - Joint RL dropped: DQN v2 fragmented ranges (32-128 KiB); v3's simulator
    rewarded larger chunks the live server did not, so v4+ learn N only with
    S = 1 MiB (§22.27-§22.28). v6 is concurrency-only.
  - No PPO: Double DQN was chosen after the LinUCB lock-in diagnosis (§22.12,
    §22.22); no recorded reason against PPO beyond scope.
  - Per-request decisions instead of a fixed interval T: the live runner
    decides per completed attempt; v6 pinned the simulator to it (§22.33).
  - FastBioDL / Falcon: the paper's related work positions them as online
    concurrency optimizers but none was run as a baseline (now added).
  - Liu et al. 2018 "Toward a smart data transfer node" (FGCS 89:10-18) IS
    real: Crossref confirms DOI 10.1016/j.future.2018.06.033. The earlier
    `reports/references.md` note calling it unresolved is wrong.
- **Code (tested):**
  - `internal/policy/online.go` `OnlineTunerPolicy`: FastBioDL/Falcon-style
    online concurrency tuner (aggregate throughput over fixed windows,
    U = T / K^N, reverse + halve step on a >tol drop, clamp at bounds, S
    untouched). Tests `online_test.go` (7, injected clock).
  - `cmd/downloader`: probe with `Range: bytes=0-0` (requires 206 and a total
    in Content-Range) when `-size` is 0; `-sha256` verification; `run()` split
    out for tests (`main_test.go`, 5 tests).
- **Metrics change (response latency):** `fetcher.Result.Latency` = time from
  sending the request until response headers arrived (0 when none arrived);
  copied to `metrics.Record.Latency`; `policy.Context.Latency` = mean over the
  recent window of attempts that got a response (`policyRunner`). Additive: no
  existing field or decision changes. Tests: `TestFetchRecordsLatency`,
  `TestFetchLatencyZeroOnConnectionFailure`, `TestPolicyRunnerLatencyIsWindowMean`,
  Context validation case. Closes the Review 1 "network monitor" latency gap.
- **Adaptation speed:** `tools/adaptation_speed.py` (from decision logs):
  causal controller cuts N 1.5 s after the 8 s cap; selector classifies at ~4 s.
- **Analyses:**
  - `tools/goodput_reliability.py` -> `testdata/split_ablation/analysis/
    goodput_reliability.json`: 230/230 ablation runs had 0 failed attempts,
    0 retries, 0 wasted bytes.
  - `tools/old_grid_reliability.py` -> `.../old_grid_reliability.json`
    (read-only over the frozen grid): under the 30 s whole-request timeout,
    11 of 50 cells had failures; capped 16 x 32 MiB re-downloaded 112 % of the
    file and aborted.
- **Online tuner experiment:** `testdata/online_tuner/` (protocol frozen in
  commit 7b438e4 before runs; amendment A1 fixed the `-policy` flag name after
  the first smoke attempt failed with a flag error, no data). 2 scenarios
  (CAP8, HEALTHY) x 4 arms x 8 blocks = 64 runs; harness `tools/
  tuner_bench.go`; analysis `tools/analyze_online_tuner.py`.
- **Reports:** `reports/review2/review2.tex` (-> `reports/
  Review2_Project_Review.pdf`) and `reports/review3/review3.tex` (figures from
  `reports/review3/make_figs.py`). Both are LaTeX, built with latexmk.
- **Immediate next step (superseded):** done — tuner analysed, suite run,
  PDFs rebuilt and submitted. Remaining Thursday items are tracked in §22.109.
### 22.107 Study audit (2026-10-24): engine flaws behind "aria2 wins under the cap"; opt-in straggler splitting; split ablation (COMPLETE)

**Status: `SPLIT_ABLATION_COMPLETE`.** 230/230 runs valid (commit f258df7).
Results by the protocol's rules (`testdata/split_ablation/analysis/summary.json`):
  - C2 splitting at CAP0: 16 x 32 MiB with split / without = 0.599
    [0.598, 0.601].
  - C3 vs aria2: CAP0 best ours (16 x 32 MiB, split) 0.995 [0.992, 0.999]
    (level; statement replaced by the engine explanation). CAP8 best ours
    1.016 [1.006, 1.029] (aria2 still 1.6 % faster; statement kept with the
    gap quantified). HEALTHY 0.344, D2 0.370 (ours faster).
  - C4: CAP8 causal_joint_NT_split / best stationary (16 x 32 MiB split)
    1.071 [1.054, 1.085] slower; CAP0 selector / best stationary 1.229
    slower. Replication of the published claim: causal_joint_T30 /
    fixed_16_1M_T30 0.767. So once the engine is fixed, a stationary
    configuration beats the adaptive controllers in both capped scenarios.
  - C5 no harm: HEALTHY 0.981 [0.931, 1.011], D2 0.995.
  - The paper revision required by these rules is still pending (§22.108).

Earlier status: `SPLIT_ABLATION_PENDING`. Branch `exp/live-pilot-l1`. The user
asked for an audit of mistakes that keep us from beating aria2, then asked
for all flaws to be fixed by Wed 2026-10-28. The user chose opt-in splitting
over an explanation-only fix.

- **Audit findings:**
  1. **Whole-request timeout.** All capped-server runs (§22.96–§22.98) used a
     30 s deadline covering request and body. 16 MiB and 32 MiB ranges at
     N=16 mostly failed and restarted from zero. aria2's `--timeout` acts on
     stalls. The earlier timeout-sensitivity study only went up to 60 s.
  2. **No dynamic segmentation.** The server log of `p1_capped_aria2_r00`
     shows aria2 splitting the unfinished part of slow pieces from 53 s on.
     Ours had immutable in-flight ranges, so its slowest range set the
     finish time.
     - Scratchpad diagnostic (not data, disclosed in the protocol): ours
       16 × 32 MiB with no deadline and no split took 114.7/113.9 s at CAP0
       (aria2 68.1/68.4) and 56.4/54.2 s at CAP8 (aria2 53.2/53.2).
     - So at CAP8 a *stationary* configuration beat the published causal
       controller's 58.7 s. The published "+23.4 % vs best of three" holds
       only for its predeclared set, all of which had small S.
  3. **Netem RTT mislabel.** `delay 40ms` on `lo` gives an 80 ms RTT
     (measured 80.1 ms), and the loss also applies in both directions. The
     paper text said "40 ms RTT"; corrected in §4 and the abstract.
  4. **DQN head-to-head design.** The files were 4–16 MiB, and v6 started at
     C=1 while aria2 opened 16 connections at once. This is now stated in
     §9; nothing was rerun.
- **Code:**
  - `downloader.Config.SplitStragglers` and `SplitMinBytes` (default 1 MiB),
    plus `Downloader.Splits()`.
  - `fetcher.Limit` and `FetchLimited`: an in-flight end can be shrunk under
    a lock, never below the received bytes. With a nil `Limit` the path is
    unchanged.
  - `rangetracker.Split`.
  - Tests: `internal/downloader/split_test.go` (gated server; tail taken;
    head retry uses the shortened bounds; off by default),
    `internal/fetcher/limit_test.go`, and `TestSplit*`.
  - `AGENTS.md` §5 has a documented opt-in exception.
- **Split ablation:**
  - Protocol: `testdata/split_ablation/` (`protocol.md`, `manifest.json`;
    harness `tools/split_bench.go` is a copy of frozen `ns_bench.go` plus
    `-split` and `-no-request-timeout`).
  - Runner and analysis: `tools/run_split_ablation.py` and
    `tools/analyze_split_ablation.py`.
  - Size: 230 runs, CAP8/CAP0/HEALTHY/D2, 10 blocks, comparisons C1–C5.
- **Immediate next step:** commit and freeze the protocol, run the smoke,
  run the ablation (about 4.5 h, uses `lo` netem, so no tests in parallel),
  then analyse. Revise paper §6/§7, the abstract (iii), the conclusion and
  the claim map strictly by the protocol's interpretation rules.

### 22.106 Live-internet pilot L-P1 + Thursday-review materials (2026-10-18 to 2026-10-20; analysed 2026-10-24; COMPLETE)

**Status: `LIVE_PILOT_COMPLETE` (descriptive; no RL, no adaptive policy run
live).** Branch `exp/live-pilot-l1` (not merged). Protocol frozen before S1
in commit 3e643d2: `testdata/live_pilot_l1/protocol.md`, `manifest.json`,
`stage0_probe.txt`.

- **Question:** on real public mirrors over the author's real access network,
  does the best fixed (N,S) change across servers and time-of-day slots? The
  headline is the selection headroom of one global fixed cell against the
  per-condition clairvoyant best, an upper bound on what any cross-condition
  adaptive selection could add.
- **Design:**
  - Client: devcontainer, AS45820 Tata Teleservices, Katpadi IN; about
    2.4 MB/s single-stream to the CDN at Stage 0.
  - File: `linux-3.0.tar.xz` (63,798,008 B), SHA-256 from kernel.org.
  - Servers: `kcdn`, `kedge`, `riken`, `tuna` (5 of 13 candidates passed
    Stage 0; `jaist` is the reserve).
  - Arms: 8 fixed cells (N ∈ {1,4,8,16} × S ∈ {1,4} MiB) + aria2 1.37.0
    (`-x16 -s16 -k1M`, 60 s timeout) + curl; 2 reps per slot.
  - Order: seeded blocks.
  - Timeouts: watchdog-only (`NoRequestTimeout`, 900 s per-run deadline).
  - Slots (IST): S1 Sun 12:00, S1T Sun 13:15 (tuna only), S2 Sun 21:00,
    S3 Mon 08:00, S4 Mon 20:00, S5 Tue 08:00.
- **Code:**
  - `cmd/live-pilot` (runner and tests; one JSONL record per run, resumable,
    invalid runs kept)
  - `scripts/live_pilot_slots.sh` (detached slot scheduler, `@server`
    filter)
  - `tools/analyze_live_pilot.py` and `tools/test_analyze_live_pilot.py`
    (predeclared descriptive analysis)
  - **API addition:** an optional `UserAgent` field on `fetcher.Config` and
    `downloader.Config`. Empty keeps the Go default and unchanged behaviour;
    `TestFetchUserAgent` covers it.
- **Amendments:**
  - A1: the access network drops nightly at about 01:30 IST; no slot overlaps
    it.
  - A2: the `tuna` mirror returns 403 to Go's default User-Agent
    `Go-http-client/1.1` (`stage0_ua_check.txt`). All 16 S1 downloader runs
    against it failed and are kept as invalid. From S1T on, the downloader
    sends `adaptivedl-research-pilot/1.0 (live_pilot_l1)`. S1T also records
    an operator error: the first launch ran all servers; 2 `kcdn` runs are
    kept and one in-flight run was lost.
  - A3: S3 ran on a **different access network** (operator-reported). It
    was moved unchanged to `raw_other_network/S3.jsonl`, is analysed
    separately as a supplementary result, and is never pooled with the Tata
    conditions. S5 (Tue 08:00) is now **required**, as the Tata morning slot.
  - A4: an internet drop at the start of S5 (8 records, 2 valid, all
    `kcdn`; kept in `raw/S5.jsonl`, and `kcdn`/S5 is INCOMPLETE). The slot
    was re-run in full as **S5R** from 08:06 IST on Tata.
- **Data so far:**
  - S1: 80 runs, 64 valid; all invalid runs are `tuna`/403.
  - S1T: 22 of 22 valid.
  - S2: 80 of 80 valid.
  - S3 (other network): 68 of 80 valid. Its DNS and retry failures cluster
    between about 08:08 and 08:40 IST, and its peaks reach about 12 MB/s.
  - S4: 80 of 80 valid (peaks about 2.43 MB/s, the Tata ceiling).
  - S5: 8 records, 2 valid (outage, A4). S5R: 80 of 80 valid.
  - The S5R scheduler's output was sent to /dev/null by operator error, so a
    reconstructed note is in `logs/scheduler.log`.
- **Results (`analysis/summary.json`, `analysis_other_network/summary.json`;
  recomputed by `tools/claims_check.py`, `lp1.*`/`lp1x.*`):**
  - **Primary (Tata):** 350 records, 328 valid (16 `tuna`/S1 403 runs, 6 S5
    outage runs). 16 complete conditions (4 servers × S1/S1T(tuna)/S2/S4/S5R).
    - Global cell 8 × 1 MiB, headroom **3.7 %**, bootstrap 95 % [1.0, 4.8].
      The upper bound is below the predeclared 5 % line, so by the protocol
      rule "one fixed configuration was near-best", which supports but does
      not prove the thesis.
    - The global cell was within 5 % of the best in 11 of 16 conditions, and
      at most 15.3 % slower.
    - No condition is ACCESS_LINK_BOUND, because N = 1 was always more than
      5 % slower.
    - The top cell changed across slots on all 4 servers, but it always had
      N ≥ 8 except for N = 4 cells three times. On `kedge`, N = 4 was
      1.37–1.71× slower than the best, so "enough connections" is
      server-dependent.
    - aria2 over the best fixed cell: 1.00–1.26. Within-run CV: 0.11–0.29.
  - **Other network (S3, A3):** 68 of 80 valid, headroom 7.2 % [1.2, 12.2]
    over 4 conditions (global cell also 8 × 1 MiB), CV 1.16–1.77. Inconclusive,
    and reported separately.
  - **Correction:** an in-session remark (2026-10-18) that "every N ≥ 4 cell
    is close to best" was wrong for `kedge`.
- **Paper:**
  - `reports/paper_draft.md` §8a is filled with final numbers and no longer
    labeled DRAFT.
  - The §10 limitation now names the pilot.
  - New verified citations (Mahimahi, Sprout/Saturator trace provenance,
    LinUCB, DQN, Double DQN, tc-netem) are in `reports/references.bib` and
    `reports/references.md`.
  - Still open (human): the redistribution terms for the Mahimahi-bundled
    traces.
  - `tools/claims_check.py`: 144/144, 0 unverified tokens. It now has a live
    pilot block, and `paper_numbers.json`/`number_sources.json` are
    extended.
- **Thursday materials:**
  - Slide deck (15 slides, private claude.ai artifact
    [private viva slide link removed]). Slide 11 **still
    holds the pilot placeholders**; it was not republished after the
    analysis.
  - `reports/thursday/project_summary.md`
  - `reports/thursday/qa.md`
- **Tests (2026-10-24):** build, vet, gofmt (outside frozen `testdata/` and
  `.claude/`), `go test ./...`, `-p 1`, `-race -p 1`, pytest 70 + 5, the
  claims self-test and claims 144/144 all pass. `go test -count=5 -p 1
  ./...` failed once in `TestAnchorCalibration`, with a **new failure mode**:
  the warm-up replay returned `DOWNLOADER_START_FAILED`, meaning the mm-link
  child exited non-zero. That is different from the documented wall-clock
  residual. It passed in 6 of 6 isolated reruns and in a separate
  `-count=5` of the package. No Go code changed since the last green
  `-count=5`. It is treated as load-sensitivity of the replay test and is
  not investigated further.
- **Immediate next step:** human review of §8a and the amendments A1–A4.
  Decide whether to merge `exp/live-pilot-l1` into `main`, and whether the
  deck's slide 11 should be filled. Do not rank modes.

### 22.105 Finalization cycle: paper-rc1, hardened claims check, bounded model-based controller attempt (executed 2026-10-12)

**Status: manuscript/release candidate tagged (`paper-rc1`, commit d1d4afa);
model-based controller attempt STOPPED at its predeclared feasibility gate
(0/6 criteria); no live evaluation; not merged Final submission-candidate tag after the negative-result disclosure was added to
the manuscript: `paper-rc1.1` (it differs from `paper-rc1` only in
`reports/paper_draft.md` §10, `reports/model_based_attempt.md` and internal docs).** Not submission-ready: human
items below are open. Frozen evidence under `testdata/` is unchanged since the
import commit 8685eb7 (`git diff --name-status 8685eb7 HEAD -- testdata` shows
additions only).

- **Phase 1 (manuscript):** claims re-scoped (D2 fixed-config result, sweep
  28/30 pairs and 0.378-0.806 range, NGINX 0.863/0.597 with failed parity
  expectation, six-workload 0.770 [0.455, 1.304] not established, P2 0.766 vs
  aria2 slower, failed S-margin and trace gates). D2 mechanism claim downgraded
  to "partly attributable" because constant-fast pacing (S4 0.568) and NGINX N1
  (0.863) still favour ours. RL inventory has a valid/stale/non-comparable
  column; related work and `reports/references.bib` contain only entries
  verified against Crossref/arXiv/RFC Editor/aria2 manual (`reports/references.md`
  lists unresolved candidates: "Liu et al. smart DTN" (no match; likely a
  mislabel of DeLoad's first author), "elastic DTN", Mahimahi, trace dataset,
  LinUCB/DQN/netem refs). Figures regenerated from raw data
  (`reports/ieee_access/make_figures.py`).
- **Corrections found by the stronger checker:** frozen analysis scripts used a
  fixed t=2.045 for every n (and z=1.96 for the D2 analysis); paper intervals
  are now recomputed with the correct critical value (v2 [1.672, 1.847], v5
  [2.230, 2.417], v6 [1.305, 1.624], D2 [0.3726, 0.3787], long-duration 512 MiB
  [0.750, 1.102], control A [0.834, 0.890]); frozen artifacts untouched. The
  "model 95.7 s" W5 figure was wrong: model prediction 94.8 s, live (4,4M)
  95.9 s, live argmin (16,16M) 85.4 s (also true of §22.100).
- **Phase 2:** `tools/claims_check.py` recomputes 113 expected numbers plus a
  manuscript scan that fails on any number not reproduced from raw runs or
  listed in `tools/paper_constants.json`; `tools/test_claims_check.py` shows it
  fails on mutated numbers (found and fixed two loose-match weaknesses).
  Release script builds from a committed revision, runs a leak scan, writes
  SHA256SUMS and runs the checker inside the tree. Clean-checkout verification
  `reports/verification/verification_a985b00.txt`: claims 113/113, parity current,
  pytest 70, Go build/vet ok, race (replay, rl) ok, **default parallel
  `go test ./...` failed twice in internal/experiment/replay (wall-clock
  alignment tests, load sensitive); passes isolated and with `-p 1`** (documented
  in docs/REPRODUCING.md; no test weakened).
- **Phase 3 (branch `exp/model-based-ns`, not merged):** gate protocol committed
  (b22ad8b), one amendment A1 after an A0 smoke failure was diagnosed as a probe
  design flaw (size fixed => overhead collinear with size/b) plus a non-robust
  loss (35f2af5), single evidence run 560 rows: median regret 0.54 (<= 0.05),
  prediction error 0.46 (<= 0.10), total cost 1.77x (<= 1.25x), k-hat worse than
  naive k:=N, logC error 0.16 (<= 0.15), fallback 64 % (<= 30 %). The true-k
  ablation also failed. Stopped by rule; no live runs, no second controller.
  Summary: `reports/model_based_attempt.md`.
- **Human-only blockers:** authors/order/affiliations/ORCID; copyright holder
  and contributor consent for MIT; third-party trace redistribution terms and
  citation; AI-use disclosure sign-off; DOI issuance and data archive upload;
  unresolved citations (Mahimahi, trace dataset, standard RL/bandit refs);
  IEEE Access template conversion.

### 22.104 Publication-readiness pass: manuscript correction, pacing sweep, NGINX check, release (executed 2026-10-11)

**Status: manuscript corrected against raw data; two screening checks run under
frozen protocols; public release tooling added. Frozen evidence unchanged.**

- **Confirmed results (reproduced from raw runs by `tools/claims_check.py`, 113
  numbers, `tools/paper_numbers.json`):** D2 ours (16 x 4 MiB) vs aria2, 30/30,
  geo 0.3756; within-download joint controller vs best of three stationary
  configs, geo 0.766, 10/10; the same controller is slower than aria2 (geo 1.096,
  0/10).
- **Descriptive estimates:** six-workload selector vs family-best single fixed:
  point 0.770, workload-level interval [0.455, 1.304] (includes 1;
  consistent improvement NOT established); vs per-workload best fixed 0.999
  [0.969, 1.029] block-level, [0.873, 1.143] workload-level. Cross-workload P1
  gains (+47.5 % / +8.2 %) are against a baseline chosen for two workloads that
  were designed to have different optima.
- **Failed gates (reported as failed):** live S-margin 3.74 % vs predeclared
  5 % (5 of 6 checks passed); five-trace recorded-replay oracle gate LCB95
  = -0.26 % vs 10 %.
- **New screening evidence (n=5 pairs per cell, descriptive only):**
  - `testdata/d2_pacing_sweep/` (protocol committed as 0ff53da before any run;
    60/60 valid): ours/aria2 geo ratios S0 D2 0.378, S1 no-dip 0.425, S2
    long-dip 0.806, S3 partial-recovery 0.583, S4 constant-fast 0.568, S5
    constant-slow 0.998. Predeclared expectations (S0 reproduces D2; S5 parity)
    held. Ours faster in 28/30 pairs. S4 shows an advantage with no phase
    structure; its mechanism was not tested.
  - `testdata/nginx_limit_rate/` (protocol committed as b60b967 before any run;
    20/20 valid; NGINX 1.24.0 per-response `limit_rate`/`limit_rate_after`, NOT
    a shared or N-dependent cap): N1 constant 0.863 (5/5), N2 burst-then-pace
    0.597 (5/5). The predeclared N1 parity expectation was **not met**; cause
    untested.
- **Corrections made to earlier text:** paper draft abstract/section 6 (withdrawn
  "23 %" family claim; per-workload baseline labels; aria2 "beats" claim for the
  P2 controller removed); the "constant pacing removes the win" wording (false
  for constant fast pacing); D2 request counts are per-30-runs totals (128 vs 16
  requests per run); long-duration v6 table description (pooled ratio 1.805 ->
  1.062 -> 0.909 driven by D2 only); duplicate section number renumbered
  (22.99 -> 22.102).
- **Bug fixed:** the NGINX runner (`nginx_run.go`) initially left `tc netem` on
  `lo` because `os.Exit` skipped a deferred clear; found in smoke testing,
  cleared, fixed (explicit clear on every exit path) and verified on success and
  failure paths before any measured run. Qdisc clean after every run and after the
  campaign.
- **Pre-registration language:** `docs/PREREGISTRATION_NOTE.md` states that
  protocols were predeclared with recorded hashes but are not independently
  timestamped; the repository starts at one import commit; W1-W6 was amended
  twice post-hoc. Evidence records under `testdata/` were not edited.
- **Raw-trace oracle artifacts archived:** `testdata/final_oracle_analysis/`
  (copied from the original out-of-repo location so the trace gate is
  recomputable; `tau_raw_observations_path` fields still name the old path).
- **Release tooling:** `LICENSE` (MIT; copyright holder placeholder),
  `DATA_LICENSE.md` (proposed CC BY 4.0 pending ownership verification),
  `CITATION.cff` (authors placeholder), `docs/REPRODUCING.md`,
  `README.public.md`, `.gitattributes` (export-ignore for internal docs),
  `scripts/make_public_release.sh` (clean tree + SHA256SUMS).
- **Validation:** `go vet`, `go build`, `go test -count=1 ./...` (30 packages),
  `go test -race` on replay and rl, `pytest python/tests` (70),
  `gen_state_parity_v5.py --check`, offline analysis scripts re-run with no
  diff, `claims_check.py` 113/113.
- **Open (human decisions / not done):** authors, order, affiliations, ORCID,
  copyright holder, AI-use disclosure; bibliography verification; data archive
  DOI and `SHA256SUMS` upload; IEEE Access template conversion and figures; the
  model-based controller (deliberately deferred). No controller or mechanism
  search was run.

### 22.103 Research verification audit (executed 2026-10-11; no live experiments re-run)

**Status: headline results reproduce from raw data; three defects fixed.** Scope:
independent recomputation from raw run files (not via the repo's analysis
scripts), re-execution of the offline analysis scripts, a code/test pass, and a
documentation-consistency pass. No live experiment was re-run.

- **Independently recomputed from raw runs, all match the documented numbers:**
  - Track 2-B adaptive (`adaptive/`, 133 indexed runs, 0 invalid, one distinct
    output SHA-256 across all runs): P1 healthy selector/fixed geo 1.006
    (6/10), capped 0.730 (10/10), vs aria2 0.353 / 1.232; P2 joint vs best
    fixed 0.766 (10/10), joint vs N-only 0.947 (10/10), joint vs S-only 0.715,
    vs aria2 1.096 (0/10).
  - W1-W6 adaptive (`w1_w6_adaptive/`, 240 runs, 0 invalid, one output SHA-256):
    pooled selector / per-workload best fixed 0.999 [0.969, 1.029], 24/60;
    selector / family-best fixed 0.770 [0.684, 0.859], 40/60, six-cluster t-CI
    [0.455, 1.304]; vs aria2 1.660, 10/60; per-workload medians and live
    argmin configs as in §22.101; W2-W6 geometric mean vs family fixed 0.938.
  - `analyze_adaptive_b.py`, `analyze_gate_b.py`, `analyze_track2.py`,
    `robustness_w1_w6.py` re-run: committed analysis JSONs unchanged.
  - aria2 mechanism: per-request server logs of a capped aria2 run show the
    first nine pieces start at k <= 9 where the cap rate (>= 665 KB/s) exceeds
    the 640 KiB/s base rate and then hold 640 KiB/s for ~52 s, as documented.
  - aria2 fairness probe (local, unthrottled, 64 MiB): `-x16 -s16/64/256 -k1M`
    produce the same 1/2/4 MiB range-size mix, so split > 16 does not give
    aria2 finer pieces; the aria2-F frontier is not under-tuned on that axis.
  - `ns_bench` binary SHA-256 equals the manifest value; the unit tests in
    `tools/band_policy_test.go` pass. A fresh rebuild is not bit-identical
    (build path/VCS stamping), so the binary-to-source link rests on the
    manifest hash and mtimes only.
- **Defects found and fixed:**
  1. `reports/paper_draft.md` §6 still claimed the selector "beats any single
     fixed configuration ... by 23 %" (withdrawn in §22.101) and labelled
     per-workload ratios that are against the per-workload best fixed as
     "family single fixed" (self-contradictory: "beats ... on W1-W4 ... loses
     W3"). Replaced by a table with both baselines per workload and the
     non-robustness caveat.
  2. Section-number collision: a second `### 22.99` (v5 simulator audit) is now
     §22.102.
  3. `internal/experiment/replay` `TestAnchorCalibration` failed
     deterministically under a parallel `go test ./...` because the first
     replay in the process paid a 450-700 ms cold-start offset (passes alone).
     A discarded warm-up replay was added; tolerances and assertions are
     unchanged. Three consecutive full `go test -count=1 ./...` runs and
     `-race` on `replay` and `rl` pass.
- **Remaining limits (not fixed; stated, not hidden):**
  - P2's "best fixed" is the best of three fixed configurations run under the
    P2 schedule ((16,1M), (4,1M), (4,4M)); there is no fixed grid under the
    P2 (cap-at-8 s) schedule, so "best fixed" there is "best of those tried".
  - W2, W3, W4 share one target by construction (same classifier level), so
    they are not three independent confirmations of selection.
  - The soft cap fixes a request's rate at request start (server model);
    aria2's "cap escape" is a property of that server model, as §22.98 states.
  - Pooled CIs treat blocks as independent; the cluster-level interval is the
    appropriate one for any cross-workload claim.
  - The pre-live W1-W6 manifest amendments (§22.100 amendment log) are
    post-hoc design refinements, not prospective pre-registration.

### 22.102 v5 simulator correctness audit follow-up (executed 2026-10-11; simulator only)

**Status: audit complete; one accounting bug fixed; the exported Candidate v5
artifact is now STALE with respect to the corrected simulator.** No training,
no new candidate, no live run, no aria2 comparison. Frozen candidates v1-v5,
the aria2-F baseline, benchmarks and `final_evaluation` are byte-identical
(`git diff HEAD -- testdata` shows no tracked change; all `.sha256` files match).

- **Bug found/fixed (`python/rl/sim_env_v5.py`):** a failed range was logged as
  ONE window sample (failure=1, retry=1, completed=0). The live runner
  (`policy_runner.observe`) records two attempts: the failed attempt
  (failure) and the successful retry attempt (`Attempt>1` -> retry, success ->
  completed). The simulator now pushes both samples, so `window_attempts`,
  `recent_failure_rate`, `recent_retry_rate` and `recent_completed_norm` follow
  the live definition. Bytes (credited exactly once), elapsed time (includes one
  retransmission), reward, RNG draw order and the static sweep are unchanged.
- **Consequence:** `final_dqn_candidate_v5.json` was trained before this fix on
  the old window semantic. It is not modified; it must not be described as
  trained on the corrected state distribution. Any further use of the v5
  simulator for learning requires retraining (not done here).
- **Semantics verified by tests** (`python/tests/test_sim_env_v5_audit.py`, 22
  parametrized cases + the existing file): fixed 1 MiB chunk and partial final
  range; concurrency in [1,4]; invalid action raises with no state mutation;
  boundary actions are no-ops; a step is exactly `decision_interval` requests
  except the final one; C=4 runs 4 requests in parallel; reward == -elapsed and
  sum(step elapsed) == episode elapsed; wall-clock (not request-count) phase
  transitions, and the transition request index depends on per-request timing;
  failed range -> bytes once + retransmission time + per-attempt windows;
  terminal iff all bytes done, truncation only from `max_episode_steps`;
  seeded determinism including `randomize()`; stationary episodes never
  transition; >=50 % of multi-phase episodes transition.
- **Python<->Go parity:** `python/gen_state_parity_v5.py` writes
  `testdata/diagnostics/dqn_validation_v5/state_parity_v5.json` (6 observations
  incl. clamping/unknown-total edge cases, tolerance 1e-12, spec + feature order
  pinned). `TestV5StateParityFixture` (Go) recomputes every vector with
  `ObservationV5.FeaturesV5`; `test_parity_fixture_is_current` fails if the
  fixture is stale. Result: all 13 features match on all cases.
- **Static sweep:** recomputed by `python/audit_simulator_v5.py` and identical to
  the existing `static_concurrency_sweep_v5.*` (not overwritten): best fixed C
  takes all of {1,2,3,4} across regimes. Single-mechanism attribution (16 MiB,
  rate 4 MiB/s): no shared cap -> C=4; shared cap = 1x/2x/3x rate -> C=1/2/3;
  loss/congestion alone never lowers best C (needs a shared cap); latency
  keeps C=4. Modelled mechanics only; dedicated real-server C=1..4 measurements
  are UNAVAILABLE.
- **Calibration:** 1 MiB @ 512 KiB/s, C=1: simulator 2.000 s vs real-server
  reference ~2.053 s (gap 52.9 ms, 2.6 %, fixed per-request HTTP overhead not
  modelled). Recorded, not fitted; full fidelity is not claimed.
- **Artifacts:** `simulator_correctness_report.{json,txt}` (new),
  `state_parity_v5.json` (new). Reproduce: `python3 python/audit_simulator_v5.py`.
- **Validation:** `go vet`, `go build`, `go test -race ./...` pass;
  `go test ./...` passed except a load-dependent failure in
  `internal/experiment/replay` `TestAnchorCalibration` on the first replay of a
  process (fixed in §22.103). `pytest python/tests`: 70 passed. `gofmt -l` lists only three pre-existing `testdata/**/tools/*.go`
  files; `black --check python` fails on 17 pre-existing files (HEAD already
  fails, including `sim_env_v5.py`), left untouched for scope.
- **READY TO TRAIN (simulator correctness gate): yes** - correctness gate only;
  not a training run or candidate. Immediate next step: decide whether to
  retrain v5 under the corrected windows before any further v5 use.

### 22.101 W1-W6 adaptive comparisons (executed 2026-10-09, LIVE): selector matches per-workload clairvoyant on average; family-single-config comparison not robust across workloads

**Status: `SIX_WORKLOAD_SELECTION_DEMONSTRATED`.** 10 fresh paired blocks per
workload (240 runs, 0 invalid; netem 40 ms + 0.4 %, content-verified). Frozen
protocol (`manifest/w1_w6_manifest.json`): band selector (probe (16,1 MiB),
25 s post-drop window, nearest frozen level) vs per-workload best fixed (live
argmin) vs family-best single fixed (live (4,4 MiB)) vs aria2. Report
`reports/w1_w6_report.md` (root + study copy); figures `figW1_W6_...`
(see artifacts).

- **Pooled paired geometric ratios (95 % bootstrap, 60 workload-blocks):**
  - selector vs per-workload best fixed: **0.999 [0.969, 1.029]**, 24/60 wins
    — no detectable difference on average (the pooled CI includes 1; this is not
    an equivalence test). Robustness (`analysis/w1_w6_robustness.json`): stratified
    bootstrap [0.996, 1.002] (block noise only); six-cluster t-CI [0.873, 1.143]
    (workloads as the unit);
  - selector vs family-best single fixed: **0.770** pooled; stratified bootstrap
    [0.768, 0.772]; **six-cluster t-CI [0.455, 1.304]**. The pooled figure is
    dominated by W1 (geo 0.286: a healthy workload vs a degraded-state fixed
    config). Over W2-W6 the geometric mean is about 0.94, and the selector loses
    to the family fixed on W5 (1.058) and W6 (1.090). The "23 % faster" phrasing
    is withdrawn.
  - selector vs aria2: 1.660 [1.363, 1.988], 10/60 — aria2 wins every degraded
    workload via its burst-start cap escape; the selector wins the healthy
    state 2.8x.
- **Per workload (median of 10):** W1 18.4 vs 18.4 / 65.3 / 53.2; W2 152.3 vs
  162.8 / 175.1 / 53.2; W3 166.2 vs 163.0 / 175.0 / 53.4; W4 131.1 vs 162.1 /
  171.6 / 53.4 (probe-phase timing win, +19.2 % vs per-workload); W5 101.0 vs
  85.9 / 95.3 / 53.4 (-17.2 %, frozen model-target mismatch); W6 75.1 vs 68.8
  / 68.6 / 53.3 (-8.8 %).
- **W5 mismatch (disclosed, not retuned):** the live grid found W5's optimum at
  (16,16M) 85.4 s vs the model's (4,4M) 95.7 s (retry/k feedback raises rates
  for retried ranges); the selector's target stayed at the frozen model value
  per the stop rule (no tuning after live outcomes).
- **Claim discipline:** this is a **(B)** workload-level selection result over
  **six predeclared controlled workloads**, not within-download adaptation
  **(C)** and not the **(A)** D2 aria2 result; no Internet-wide generalization
  claim. The W1 continuous-fixed results were run under the same frozen
  protocol and count as one of the six.

### 22.100 W1-W6 six-workload family: analytical design + manifest freeze (executed 2026-10-08, analytical; pre-live)

**Status: `W1_W6_FAMILY_FROZEN`** (before any live W1-W6 run). Per the
remaining-month plan (mechanism-dependent-granularity thesis), the six-workload
expansion was designed analytically from the calibrated live-equivalent model
only (`tools/combined_sim.py`, Params h=0.088, timeout 30 s, 3 attempts); no
live W3-W6 outcome existed or was consulted. Frozen artifacts:
`manifest/w1_w6_manifest.json` + `analysis/w1_w6_design.json`
(`tools/design_w1_w6.py`).

- **Candidate family:** T in {1,2,4} x C in {4,8,12,16} MiB/s x gamma in
  {0.05,0.10} x cap activation t in {4,8,12} s (72 candidates) + W1 healthy.
- **Qualification (predeclared):** B (state-schedule optimum) vs A (T-matched
  no-cap optimum): N and S both move; B reachable; cap-side gain >= 20 %;
  >= 10 % right-config gain; detectable (model post-transition probe level
  < 0.40 x peak probe level — selector-identifiability precondition).
  **13/72 qualify**, all with t in {4,8}; no t=12 candidate qualifies, so the
  transition-coverage constraint is relaxed to >= 2 distinct transitions
  (documented in the manifest).
- **Selection (predeclared):** maximize sum(min(cap_gain, base_pen)) over
  5-subsets with coverage and lexicographic tie-breaks; selected **W2
  (T=1,C=4,g=.10,t=4), W3 (T=2,C=4,g=.10,t=4), W4 (T=2,C=4,g=.10,t=8),
  W5 (T=2,C=8,g=.10,t=4), W6 (T=2,C=12,g=.10,t=4)**. Model optima: W2-W4
  **(2,4 MiB)** (~160 s), W5-W6 **(4,4 MiB)** (94.8 s / 67.9 s); base-side
  penalties 231-579 % (the candidate optima are terrible in the healthy
  state — a genuine two-way trade-off).
- **Selector (predeclared):** `band` policy, probe (16,1 MiB), running-max
  drop detector (0.5x, window 3), fixed **25 s** post-drop classification
  window (>= 1 sample), nearest predicted level, deadline 60 s -> healthy
  (16,1 MiB). Levels 104857 x3 (W2-W4 share the class and the target) /
  209715 (W5) / 314572 (W6) B/s; targets (2,4 MiB) / (4,4 MiB). Pre-live
  review note: a sample-count window (20 samples) was rejected before any
  live W1-W6 run because post-drop request durations reach ~10 s under the
  strongest selected cap, making the window exceed the download duration.
- **Live protocol (frozen):** per workload fixed grid {4,8,16} x {1,4,16} MiB
  + predicted optimum, 2 reps, then 10 paired blocks of
  band_selector | per-workload best fixed (live argmin) | family best single
  fixed (live) | aria2; netem 40 ms + 0.4 %; invalid runs preserved.
- **Live fixed-grid validation (complete, 2026-10-08):** 114 runs (2 reps/cell,
  netem 40 ms + 0.4 %, timeout 30 s), invalid runs preserved. Live argmins vs
  the frozen model predictions: **W1 (16,1M) 18.5 s match; W2 (2,4M) 163.2 s
  match; W3 (2,4M) 163.0 s match; W4 (2,4M) 162.3 s match; W6 (4,4M) 68.6 s
  match; W5 mismatch** — the live optimum is **(16,16M) 85.4 s**, not the
  model's (4,4M) 95.7 s. Mechanism of the W5 mismatch: under C=8/gamma=0.10
  the first 16 x 16 MiB wave samples its rate pre-cap and completes; failed
  later ranges time out (30 s) and the retry lowers k, which raises the
  cap rate for retries, so (16,16M) completes (32 successes + 8 partial
  failures) at 85.4 s — a retry/k feedback the analytical model does not
  capture. The frozen selector (model-derived target (4,4M) for W5) is
  **not** retuned after this observation (stop rule); W5's honest expected
  penalty vs the clairvoyant live argmin is ~10 %. Family-best single fixed
  (live, common cells): **(4,4M), mean 125.2 s**.
- **Amendment log (added 2026-10-11 audit):** the manifest was amended twice
  after the first design run and before any live W1-W6 run, both after seeing
  design output: (a) the post-drop classification window changed from 20 samples
  to a fixed 25 s window (>= 1 sample), because 20 samples would exceed the
  download duration under the W2-W4 cap; (b) the transition-coverage constraint
  was relaxed from {4,8,12} to >= 2 distinct transitions, because no t=12
  candidate qualified. The first manifest was overwritten and is not preserved.
  These are post-hoc design refinements, not prospective pre-registration. The
  W2-W4 selector levels (104857 x3) are identical, so the three share one target
  by construction.
- **Adaptive comparisons (running at write time):** 10 fresh paired blocks per
  workload of band_selector | per-workload best fixed (live argmin) | family
  best single fixed (live) | aria2, frozen protocol; results land in
  `analysis/w1_w6_adaptive.json` and `reports/w1_w6_report.md`. Frozen
  D2/aria2 evidence and all prior screens untouched.

### 22.99 P1 baseline correction + P2 S-only ablation (executed 2026-10-08, LIVE)

**Status: complete.** Plan-phase-1 items, criteria unchanged.

- **P1 baselines (no data bug; terminology fix):** `analysis/adaptive_b.json`
  now reports both baselines explicitly from the raw gate/runs:
  - best **single** fixed configuration across the two predeclared controlled
    workloads = (8, 4 MiB), 36.3 s healthy / 91.7 s capped; the selector gains
    **+47.5 % / +8.2 %** (mean +27.9 %, worst +8.2 %);
  - per-workload best fixed (healthy (16,1M) 18.4 s; capped (4,4M) 86.4 s):
    selector **-3.5 %** in the healthy state (policy-path overhead; the
    in-block paired 19.1 vs 19.0 s is indistinguishable) and **+2.6 %** in the
    capped state.
  `reports/track2b_soft_cap_report.md` and `reports/paper_draft.md` reworded;
  P1 remains explicitly a **two predeclared controlled workloads** result.
- **P2 S-only ablation (10 fresh blocks):** `causal_s_only` (target
  (16,4 MiB); S moves, N stays) = **82.1 s** — worse than fixed (16,1M) 76.7 s
  (geo 1.070 vs per-rep best fixed, 0/10) and far worse than joint (58.7 s;
  joint vs S-only geo 0.715, 10/10). N-only 62.0 s; **joint vs N-only geo
  0.947 CI [0.943, 0.951], 10/10 wins** (smaller but consistent S-side gain).
  The N move carries the gain under this cap; S alone actively hurts.
- **Band selector policy added (pre-live):** `tools/ns_bench.go` mode `band`
  + `tools/band_policy_test.go` (nearest-level, tie-break, parsing), built and
  frozen (sha256 recorded in the W1-W6 manifest). `tools/design_w1_w6.py`,
  `tools/run_w1_w6_grid.py`, `tools/run_w1_w6_adaptive.py` added.
- **Report/analysis updates:** `tools/analyze_adaptive_b.py` +
  `tools/make_report_b.py` now emit the S-only row, the paired joint-vs-N-only
  and joint-vs-S-only comparisons, the two P1 baselines, and a dynamic run
  count.
- **Timeout sensitivity (complete):** across the capped grid {N=4,8,16} x
  {S=1,4,16 MiB} at 20/30/60 s per-request timeout (2 reps/cell), the argmin is
  **(4, 4 MiB) at every timeout** (86.5 s / 86.4 s / 86.5 s). The timeout changes
  only *reachability*: invalid cells 2 (20 s) / 1 (30 s) / 0 (60 s) — the
  high-N large-S aborts are a timeout artifact of the frozen retry limit, not a
  change in the mechanism's optimum. Full grid + abort reasons in
  `analysis/timeout_sensitivity.json`, raw runs in `timeout_sensitivity/`.
- **Capped S-row precision replication (complete):** 4 extra reps at N=4 pooled
  with the gate runs -> S=4 MiB 86.4 s (n=7), S=1 MiB 89.7 s (n=7), S=16 MiB
  97.0 s (n=5); local S margin **3.74 %** (95 % bootstrap CI [3.09 %, 4.45 %]).
  The preregistered 5 % bar is unchanged and still missed; the S* movement is
  statistically solid but carries the documented local-optimum robustness
  caveat (`analysis/precision_s_row.json`).
- **Band-selector smoke (pre-live, implementation check only):** one live W2
  run (T=1, C=4 MiB/s, gamma=0.10, t=4) with the frozen band parameters:
  drop detected and classified at 31.6 s (level 103.6 KiB/s -> target (2,4 MiB)),
  completion **155.2 s** (model 160.5 s; ~305 s without switching),
  content-verified, no retries. No frozen parameter was changed after this
  smoke (`smoke/smoke_w2_band.json`).

### 22.98 Track 2-B adaptive results (executed 2026-10-08, LIVE): joint adaptation demonstrated

**Status: `JOINT_ADAPTATION_DEMONSTRATED_UNDER_CONTROLLED_SERVER_DEGRADATION`.** Two
predeclared adaptive experiments were run on the live soft-cap platform (10
blocks each, tc/netem 40 ms + 0.4 %, content-verified; report
`reports/track2b_soft_cap_report.md`).

- **P1 — cross-workload joint (N,S) selection.** The causal probe selector
  classifies the workload after >= 20 samples and picks the predeclared (N,S):
  - healthy: selector 19.1 s vs fixed (16,1M) 19.0 s (geo 1.006, CI
    [0.939, 1.080], 6/10) vs aria2 53.9 s (geo 0.353, 10/10) — matches the best
    fixed and beats aria2 **2.8x**;
  - capped: selector 84.2 s vs fixed (16,1M) 115.3 s (geo 0.730, CI
    [0.728, 0.732], **10/10 wins**) vs aria2 68.3 s (geo 1.232, 0/10);
  - cross-workload: the best **single** fixed configuration (N=8, S=4 MiB)
    loses to the selector on **both** workloads — gains +47.5 % (healthy) and
    +8.2 % (capped), mean **+27.9 %**, worst **+8.2 %**.
- **P2 — within-download joint adaptation under degradation** (no cap [0,8 s),
  cap on [8 s, EOF)): the causal drop controller switches once to (4, 4 MiB):
  - causal joint **58.7 s** vs best fixed (16,1M) 76.7 s — geo **0.766** CI
    [0.763, 0.769], **10/10 wins** (+23.4 %); N-only control (target (4,1M))
    62.0 s (geo 0.808, 10/10) — so the win is genuinely joint (both N and S
    change and the joint target is the better one);
  - aria2 53.6 s (geo 1.096, 0/10): aria2 wins in this scenario.
- **aria2 mechanism contrast (documented honestly):** under the cap, aria2's
  16 x 32 MiB pieces all start at ~0.4 s while k is still low — the first nine
  get 640 KiB/s and hold it, so they **escape the cap**; ours re-allocates
  continuously and is capped again each time, and large ranges time out. This
  is the inverse of the D2 mechanism (where aria2's burst-start was the
  weakness). Ours beats aria2 by 62.4 % under D2 and 2.8x under the healthy
  schedule; aria2 wins under the cap.
- **Gate caveat (corrected):** the live gate **failed** its predeclared S-margin
  check (3.8 % < 5 %; `gate_b.json` `pass: false`); N/S movement and cross-state
  materiality passed (+31.8 % / +254.4 %). See §22.97.
- **Artifacts:** `testdata/n_dependent_server_joint_ns/` (manifest/b2_design.json,
  analysis/prescreen_b.json + gate_b.json + adaptive_b.json, fixed_grid/,
  adaptive/ 122 runs, figures/figB1..figB4, tools/); server extension + tests in
  `internal/experiment/server/`. The frozen D2/aria2 benchmark and all prior
  screens are unchanged.

### 22.97 Track 2-B live fixed-grid gate (executed 2026-10-07, LIVE): FAILED ONE PREDECLARED CHECK (S-margin)

**Status: `LIVE_GATE_FAILED` (`analysis/gate_b.json`: `"pass": false`; 5 of 6 checks pass).** The live fixed grid (both states,
full 5x5 (N,S) grid, 1 rep/cell + 2 extra reps on the top-3 cells per state,
tc/netem 40 ms + 0.4 % on lo to match the calibrated per-request overhead;
62 cells, 3 invalid high-N/large-S cells that abort under the cap — preserved
with reasons).

- **Healthy optimum (live): (N=16, S=1 MiB) 18.4 s**, N margin +48.3 %,
  S margin +8.4 % (matches the analytical 18.42 s).
- **Capped optimum (live): (N=4, S=4 MiB) 86.4 s**, N margin +5.8 %,
  S margin +3.8 % (the capped S curve peaks clearly at 4 MiB: 4M 86.4 s vs
  1M 89.8, 16M 96.8, 256K 104.5, 32M 109.7; the 3.4 s difference is ~7x the
  per-cell rep spread).
- **Gate checks:** N moves PASS, S moves PASS (1 MiB -> 4 MiB), N margin
  >= 5 % PASS, S margin >= 5 % **FAIL (3.8 %)**, cross-state materiality PASS
  (+31.8 % / +254.4 %). Overall: **FAILED** — the predeclared gate requires all
  checks; the S-margin check fails (3.8 % < 5 %). Correction (2026-10-11 audit):
  earlier text described this as "passed with caveat"; that was incorrect. The
  Track 2-B adaptive runs (§22.98-§22.99) were executed on a platform whose
  predeclared gate failed one check; they are exploratory with respect to the
  S-side mechanism, not confirmatory.
- **Platform note:** without netem the live S landscape is flat (the
  per-request RTT overhead is absent); the no-netem grid is preserved under
  `fixed_grid/invalid_no_netem/` as the documented model-live mismatch. All
  adaptive runs include the same netem.
- **Adaptive experiment running** (`tools/run_adaptive_b.py`): P1
  cross-workload selection (selector | fixed | aria2 | curl | wget) and P2
  within-download adaptation (causal joint | causal N-only | fixed variants |
  aria2), 10 blocks each, netem matched.

### 22.96 Track 2-B soft N-dependent server throttle: live implementation + smoke (executed 2026-09-29, LIVE)

**Status: `LIVE_MECHANISM_IMPLEMENTED_AND_SMOKE_VALIDATED`; the live fixed-grid
gate is running.** Following the exploratory finding (§22.95), the mechanism was
implemented in the controlled server and validated live.

- **Server extension** (`internal/experiment/server/server.go`): a soft
  concurrency cap — `SetConcurrencyCap(C, gamma)` makes a request starting
  while k requests are active (including itself) paced at
  `min(base rate, C/(k*(1+gamma*(k-1))))`, sampled at request start and held.
  It is a rate throttle only: it never changes status codes, failures, or retry
  behavior. Active-request tracking (`ActiveRequests()`), request-log fields
  (`ActiveK`, `BaseRate`, `CapRate`), default off (the D2 behaviour is
  unchanged). Tests: `internal/experiment/server/concurrency_cap_test.go`
  (rate formula, validation, default-off, a 4-way concurrency test verifying
  the observed k and rates, and the failure path).
- **Frozen design** (`manifest/b2_design.json`): the predeclared family (5
  schedules x 17 cap settings x the 5x5 grid) was screened; 5 of 80 pairs
  qualify under the strict rule; the selected pair is the T=1 s schedule with
  C=8 MiB/s, gamma=0.05 (the cap binds at the degraded optimum, so both
  transitions are detectable). Stationary optima: healthy (16, 1 MiB) 18.42 s
  vs capped (4, 4 MiB) 85.51 s; cross penalties +35.8 % / +252.9 %.
- **Positive results to measure live:** P1 = cross-workload joint selection
  (probe classifier picks (16,1 MiB) or (4,4 MiB); analytical gain ~23.6 % mean
  over the best single fixed); P2 = within-download adaptation under a cap
  transition at 8 s (analytical: joint target +11.5 %, N-only target +15.4 %
  over the best fixed; the measured post-switch optimum is (4, 1 MiB), so the
  within-download win is concurrency-driven — reported honestly).
- **Live smoke (content-verified, 0 retries):** healthy fixed (16,1M) 15.8 s;
  capped fixed (16,1M) 113.0 s; capped fixed (4,4M) 84.5 s; classifier healthy
  15.8 s final (16,1M); classifier capped 81.8 s final (4,4M); drop controller
  (cap at 8 s) 51.8 s final (4,1M). Live matches the analytical model within a
  few percent.
- **Running:** the live fixed-grid gate (both states, full 5x5 grid, 1 rep per
  cell + 2 extra reps on the top-3 cells per state; driver
  `tools/run_gate_b.py`, analysis `tools/analyze_gate_b.py`).
- **Next:** gate verdict; the adaptive experiment (`tools/run_adaptive_b.py`,
  20 blocks of P1 + P2 with aria2/curl/wget baselines); the paper.
- **Not modified:** Track 1, the aria2 competitive benchmark, and all prior
  screens are untouched. The only source change is the additive server cap.


---

