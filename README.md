# Adaptive HTTP range downloader: controlled-mechanism evaluation

A Go HTTP range-request downloader (adaptive concurrency and range size), a
controlled range-serving test server, tc/netem and trace-replay harnesses, and
the experiment data and analysis used in the accompanying paper.

**What this repository is.** A research artifact: a platform and a mechanism-first
evaluation under *controlled* server and network behaviour. It is not a drop-in
replacement for aria2 and makes no claim about production servers or live
networks. The paper's claim-to-evidence map is `reports/paper_draft.md`.

## Quick start

```
go build ./... && go test -count=1 ./...
python3 tools/claims_check.py        # recomputes every paper number from raw runs
```

Full reproduction instructions, requirements and expected runtimes:
[`docs/REPRODUCING.md`](docs/REPRODUCING.md). On the meaning of
"pre-registered": [`docs/PREREGISTRATION_NOTE.md`](docs/PREREGISTRATION_NOTE.md).

## Layout

```
cmd/        command-line entry points (downloader, experiment, evaluation drivers)
internal/   downloader, policies, controlled server, network shaping, RL adapter
python/     simulators, training and analysis scripts (RL inference is in Go)
testdata/   experiment manifests, raw runs, analysis outputs, tools
reports/    paper draft and per-study reports
tools/      claims_check.py and paper_numbers.json
```

## Licenses

Code: MIT (`LICENSE`). Data: not licensed for reuse yet; the authors retain all rights.
See `DATA_LICENSE.md`. Citation:
`CITATION.cff` (authors to be confirmed).
