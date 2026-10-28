## 25. Important design decisions

- Half-open `RangeTracker` ranges; inclusive `Fetcher`/`Assembler`/metrics;
  explicit conversion in the downloader.
- One request = one contiguous range.
- In-flight ranges are immutable; chunk-size changes are future-only.
- Failed ranges retry in place with original boundaries (rather than
  `Tracker.Fail` re-allocation) to avoid re-splitting under a changed target.
- Chunk size is read lazily at each allocation.
- Exactly one serialized policy runner; no duplicate runners.
- Policies return generic `ActionKind` values; no policy-type branches in the
  downloader; one dimension per action.
- Deterministic rule-based `JointPolicy` before any learning.
- Raw RL `State` + explicit, documented `Features` normalization.
- Finite discrete RL action space; explicit-target `Set` actions reserved and
  unsupported for learning.
- Reward structure/validation defined **and computed** by `RewardCalculator`;
  one explicit additive formula (benefit minus costs); the learner consumes only
  `Reward.Value`.
- `policy.Context` exposes two throughputs: per-request `Throughput`
  (`sum bytes/sum durations`) and wall-clock `AggregateThroughput`
  (`sum bytes/wall-clock span`), both windowed. The reward's throughput
  components use the aggregate one, because the per-request signal is
  action-independent w.r.t. concurrency under server pacing (see §22.5); the
  per-request signal is retained in the State/features. The deterministic
  policies keep reading the per-request `Throughput` and are unchanged.
- Correct credit assignment: `policyRunner.Update` is called with the
  **previous** context/action (the outcome observed now belongs to the action
  chosen before); first decision has no `Update`.
- LinUCB behind the existing `rl.Policy`; it consumes only `State.Features`,
  uses the finite `ActionIndex` space, updates with `reward.Value` only, and
  breaks ties by the lowest `ActionIndex`.
- Generic numerical linear algebra is delegated to a mature library (Gonum)
  rather than maintained in-project; project-specific research/domain logic
  remains project-owned, and the dependency boundary is narrow (§3.1, §3.2).
- LinUCB is an **opt-in fifth mode**: `AllModes()` and `Run`/`RunMatrix`
  defaults remain the four baseline modes, and the saved baseline dataset is
  unchanged; LinUCB is selected explicitly (`RunSelected`, `MatrixConfig.Modes`,
  `-modes linucb`). The learner reuses the existing Adapter/runner/reward path —
  no new downloader, runner, worker pool, or reward.
- Network-unavailable runs excluded from valid aggregates.
- **Documentation convention:** package docs carry the flow, the invariants and a
  worked example where the arithmetic is non-obvious (range conversion, the two
  throughput measures, the cap, the reward, LinUCB's score). Reasons live beside
  the code, not only in reports; `reports/thursday/code_walkthrough.md` is the
  navigable index for a code review, and it must stay true to the source.

---

## 26. Known caveats

- The origin server is a controlled synthetic HTTP Range server, not a real CDN
  or origin.
- Network conditions use `tc`/`netem` on a specific interface; in the current
  container this needs privileges (`sudo -n`), and shaping affects that
  interface's traffic (loopback is preferred for local httptest experiments).
- Wall-clock duration/throughput vary run-to-run; the `loss` scenario in
  particular shows high variance.
- `JointPolicy` is deterministic; its alternation can be masked by bound
  fallbacks.
- The reward is the **current baseline (Candidate C: absolute + improvement
  throughput; state-change `ActionCost`)**: weights/normalizers are explicit and
  configurable but not empirically tuned or scientifically validated. With
  `RewardConfig == nil`, the downloader still passes `0`, so LinUCB run through
  the default config would learn from a zero signal.
- Fairness is deliberately absent from the reward because no fairness signal is
  measured; `ActionCost` is a churn cost, not fairness.
- LinUCB is available as an opt-in fifth mode, has a calibration/diagnostics
  workflow, and was included in the first formal 5-mode benchmark (§22.6).
  Because it made no controller changes there, **no meaningful learned-policy
  performance evaluation exists** yet.
- **Observability:** the pre-audit per-request throughput could not expose the
  effect of concurrency; this was corrected by adding the wall-clock
  `AggregateThroughput` observable (§22.5), which the benchmark confirms responds
  to overlap.
- **Exploration:** the §22.7 diagnostic proved the `ActionNone=100 %` lock-in was
  **B/A/C/F** (exploration/init-tie/reward-scale/credit), not a
  concurrency-observability defect, and it is now **corrected** (§22.8) by an
  optional deterministic warm-up (`InitialExplorationRounds`, set to 1 in the
  experiment). The 4 MiB Benchmark v3 (§22.11) then showed **no meaningful
  post-warm-up evolution**: after the warm-up, LinUCB locked onto a single arm
  and never changed concurrency. So the warm-up fixes "never tried", but the
  current reward-scale/disjoint-UCB interaction still yields a stable
  single-arm lock; this is **not** an observability defect and was **not** tuned.
- **Remaining, documented, not fixed:** the harness models no chunk-size
  performance tradeoff, so chunk size remains unidentifiable from throughput
  (§22.7 cause **E**); and credit assignment stays approximate under
  concurrency.
- Credit assignment is **approximate under concurrency > 1** and for chunk size
  while old-size requests are in flight, because each decision's reward is
  computed from the next completed attempt (§22.5).
- **Warm-up vs decision budget:** with `InitialExplorationRounds=1`, the first
  five selections are forced. The locked 256 KiB benchmark yields only ~2 learned
  decisions (§22.9); the selected 4 MiB horizon yields 32–122 learned decisions
  (§22.11). The horizon still varies by scenario because chunk growth shrinks the
  number of ranges, so decision counts must be reported, not assumed.
- An **aria2c external baseline** has been measured (§22.16); **IDM is not
  evaluated** (Windows-only, no Linux measurement path). Configuration
  differences (aria2's 1 MiB minimum split vs the internal 32 KiB chunk) make the
  comparison descriptive, not a ranking.
- **Documentation/code comment drift:** the doc comment on
  `downloader.Config.Policy` still says "only concurrency actions are applied",
  but the actual `policyRunner.applyAction` applies both concurrency and
  chunk-size actions (and ignores reserved Set actions). The behavior in §11 is
  authoritative; the comment is stale. A future agent may correct the comment
  when touching that file, but no code behavior is wrong.

---

