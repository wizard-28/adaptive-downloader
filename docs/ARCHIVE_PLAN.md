# Large-artifact archive plan (no DOI has been issued)

`testdata/` is about 320 MB (about 3,400 tracked files in the repository, plus
untracked local binaries that are never released). Proposed split:

| Archive | Contents | Needed for |
|---|---|---|
| `adaptive-evidence` | `testdata/n_dependent_server_joint_ns/{adaptive,w1_w6_adaptive,w1_w6_grid,fixed_grid,precision,timeout_sensitivity,analysis,manifest}` | Track 2-B / W1-W6 claims |
| `d2-aria2-evidence` | `testdata/aria2_competitive_benchmark/**`, `testdata/d2_pacing_sweep/**`, `testdata/nginx_limit_rate/**` | D2, sweep, NGINX |
| `learned-policy-evidence` | `testdata/final_evaluation*/`, `testdata/diagnostics/dqn_*` | Section 9 inventory |
| `trace-gate-evidence` | `testdata/final_oracle_analysis/**`, `testdata/oracle_preregistration_v4/**` | recorded-trace gate (third-party trace terms apply) |

Procedure (to be executed by an authorized person):
1. Build the release tree with `scripts/make_public_release.sh OUTDIR REV`.
2. Create the archives from the release tree; compute SHA-256 for each archive
   and record them in a `SHA256SUMS` file included in each archive.
3. Deposit in a DOI-issuing repository (e.g. Zenodo or Figshare); record the
   **issued** DOI in `CITATION.cff` and the manuscript. DOI field is currently
   `TO-BE-ISSUED`; do not invent one.
4. Verify by extracting into a clean tree and running `python3
   tools/claims_check.py`.

Third-party trace files keep their own terms (see `DATA_LICENSE.md`); confirm
they may be redistributed before depositing `trace-gate-evidence`.
