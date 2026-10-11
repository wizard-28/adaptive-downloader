# Note on "pre-registered" wording in this repository

Many documents and source comments describe protocols as "pre-registered" or
"frozen before execution". What actually exists, and what does not:

**Exists.** Protocol and manifest files were written to disk before the
corresponding runs, and their SHA-256 hashes were recorded in later documents
and checked by the drivers before execution (for example
`testdata/d2_pacing_sweep/manifest/sweep_manifest.sha256`, which the sweep
driver verifies). Frozen candidate models and baselines carry recorded SHA-256
hashes.

**Does not exist.**

- No independent, third-party timestamp (no registry deposit, no signed commit
  history). The repository history begins with a single import commit, so the
  order of events before that import cannot be verified from version control.
- The W1-W6 manifest was amended twice after design output had been seen and
  before any live W1-W6 run (post-drop classification window changed from 20
  samples to a fixed 25 s; transition-coverage constraint relaxed). The first
  version was overwritten and is not preserved.
- The six workloads and the 72 analytic candidates were produced by a search
  over server behaviours chosen by the authors.

**How to read the wording.** Wherever this repository says "pre-registered" for
work completed before the d2_pacing_sweep, read it as "predeclared in a logged
protocol with a recorded hash, not independently timestamped". The sweep
protocol and later manifests are committed, with their hashes, before their
runs; every version is preserved.

Documents in `testdata/` are evidence records and were not edited to change this
wording.
