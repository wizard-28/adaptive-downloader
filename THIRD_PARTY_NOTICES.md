# Third-party material in or used by this repository

Kept for a private repository. Review each item before making the repository public.

| Item | Where | Status |
|---|---|---|
| IEEE Access LaTeX template (`ieeeaccess.cls`, a modified `IEEEtran.cls`, logos, sample) | `reports/ieee_access/template/` | © IEEE, distributed by IEEE for authors preparing papers. Remove before a public release unless redistribution is confirmed. |
| IEEE conference LaTeX template | `reports/ieee_conf/template/` | © IEEE, same as above. |
| Mahimahi cellular traces (Winstein et al., NSDI 2013, recorded with Saturator) | not in git; replayed from a local Mahimahi checkout | Mahimahi is GPL-3.0. Redistribution terms of its bundled traces are unconfirmed (`DATA_LICENSE.md`); the trace files are deliberately not committed. |
| Mahimahi source clone | `mahimahi-clone-dir/` (ignored by git) | Third-party GPL-3.0 code, not vendored. |
| aria2, axel, lftp | used as installed programs, not included | Their own licenses apply. |
| Go module `gonum.org/v1/gonum` | `go.mod` | BSD-3-Clause. |
| PyTorch, NumPy | `python/requirements.txt` | Training only; their own licenses apply. |
| Linux kernel source tarball (`linux-3.0.tar.xz`) | downloaded from public mirrors at run time | Not included. |

AI assistance: parts of the code, analysis scripts and drafts were written with an AI
coding assistant (Anthropic Claude via Claude Code); see the Declarations section of
`reports/paper_draft.md`. Commit messages may carry Claude Code attribution lines.
