# adaptivedl

An HTTP range download manager (Go) that adapts two control dimensions while a
download is running:

1. **concurrency** — how many range requests are in flight;
2. **future chunk size** — the size of *new* range allocations (in-flight ranges
   are never resized).

The repository contains the downloader, a controlled range-serving test server,
a `tc`/`netem` network-emulation layer, deterministic and learned policies, and
the experiment harnesses used to evaluate them against aria2, axel and lftp. The
research state, results, and caveats are documented in
[`docs/PROJECT_CONTEXT.md`](docs/PROJECT_CONTEXT.md), an index to the full text in
`docs/context/` (find a section with `python3 tools/context_find.py 22.109`).
Read [`AGENTS.md`](AGENTS.md) before contributing.

Project: BCSE308P (Computer Networks Lab), VIT Vellore. Team: Sourajyoti Basak,
Shwetank Shrijan, Ayush Konar.

## Status

- The downloader, controllers, policies, and test server are implemented and
  unit/integration tested.
- Headline result (controlled D2 workload): a fixed configuration of 16
  connections and 4 MiB ranges finishes about 62 % faster than aria2c (frozen
  30-pair benchmark). Adaptive results are reported with their caveats; the
  Track 2-B gate and the W1–W6 selector claims are documented in
  `docs/PROJECT_CONTEXT.md` §22.97–§22.101.
- Question and answer: does adapting concurrency and range size during a download
  help when conditions are not optimal, against aria2 and the best fixed setting?
  Mostly no: adaptive controllers beat aria2 only when the server rate rose, and
  a fixed setting did better still; engine details mattered more than the
  controller. Full evidence: `reports/paper_draft.md`.
- Scope: single server, HTTP/1.1, controlled loopback and emulated networks, plus
  a small live check on public mirrors. No production or multi-source claims.

## Paper, presentation and viva material

- Paper: `reports/paper_draft.md` (source of truth). Build the PDFs with
  `python3 tools/paper_to_pdf.py` (plain), `python3 tools/paper_to_ieee.py` (IEEE
  Access template) and `python3 tools/paper_to_ieee.py --conf` (IEEE conference
  template); output in `reports/paper/`. Every number is recomputed by
  `python3 tools/claims_check.py`. LaTeX needs `pdflatex` and `bibtex`.
- Presentation: `reports/deck12/` (the 15-slide talk) and `reports/deck/` (long
  deck), source files for Claude Slides decks; each folder's README says how to
  republish. Charts come from `reports/handbook/make_deck_figs.py`.
- Viva preparation: `reports/thursday/` (`qa.md` spoken answers, `qa_detailed.md`
  exact numbers, `viva_cheatsheet.md`, `who_explains_what.md`, `demo.md`).
- Reviews 1 to 3: `reports/Review2_Project_Review.pdf`,
  `reports/Review3_Final_Report.pdf`; team handbook: `reports/handbook/`.
- Third-party files (IEEE templates, traces): see `THIRD_PARTY_NOTICES.md`.

## Requirements

- Go 1.27.1 (see `go.mod`). The only Go dependency is `gonum.org/v1/gonum`.
- Python 3 with `torch==2.9.0+cpu` and `numpy==2.5.3` for the DQN training code
  under `python/` (training only; inference is in Go). See `python/requirements.txt`.
- Optional, for network-shaped experiments: Linux `tc`/`netem` with `sudo`,
  and `aria2c` for the external baseline.

## Layout

```
cmd/             CLI entry points (downloader, experiment, calibration, ...)
internal/        library packages
  rangetracker/  half-open byte-range partition and lifecycle
  fetcher/       one HTTP Range request -> result
  assembler/     random-access writes of completed ranges
  metrics/       per-attempt records and aggregates
  concurrency/   concurrency target controller
  chunksize/     chunk-size target controller
  downloader/    config, worker pool, policy runner
  policy/        Policy interface and deterministic policies
  experiment/    controlled server, tc/netem, matrix, replay, tau/oracle tools
  rl/            RL state/action/reward contract, LinUCB, DQN inference
  diagnostics/   separately labelled diagnostic studies
python/          DQN training code (not needed at runtime)
docs/            PROJECT_CONTEXT.md (handoff), team status, notes
reports/         study reports and the paper draft
testdata/        frozen manifests, raw run outputs, analysis artifacts
scripts/         auxiliary scripts
```

Range conventions: `RangeTracker` uses half-open `[start, end)`; `Fetcher`,
`Assembler`, and metrics use inclusive `[start, end]`. The downloader converts
between them. See `docs/PROJECT_CONTEXT.md` §6.

## Build and test

```bash
go build ./...
go vet ./...
gofmt -l .            # should print nothing for the module packages
go test ./...
go test -race ./...
```

Some tests in `internal/experiment/...` drive real subprocesses and take
seconds to minutes. `TestAnchorCalibration` (`internal/experiment/replay`) is
timing-sensitive and can fail under heavy parallel load; see
`docs/PROJECT_CONTEXT.md` §26.

Python tests:

```bash
cd python && python3 -m pytest -q tests
```

## Running

Single download:

```bash
go run ./cmd/downloader -h
```

Experiment harness (four baseline modes; `-sudo` enables `tc`/`netem`):

```bash
go run ./cmd/experiment -h
go run ./cmd/experiment -matrix -sudo -reps 10 -json > matrix.json
```

`cmd/experiment` prints results to stdout (text, or JSON with `-json`). The
study tools under `testdata/*/tools/` write their raw runs and analysis into
their own study directory. Use the `-h` flag of each command to see its
options; the documented invocations are in `docs/PROJECT_CONTEXT.md` §28.

## Reproducibility and frozen artifacts

Studies are pre-registered: each has a frozen manifest, raw per-run results
(including invalid runs), and an analysis script. Frozen evidence must not be
edited after the fact; amendments are recorded in `docs/PROJECT_CONTEXT.md`.
Compiled tool binaries are not tracked; rebuild them from the source next to
each one (for example `go build -o ns_bench` in the study's `tools/` directory
for the Track 2-B tools).

## Contributing

Follow `AGENTS.md`: keep the range invariants and controller independence,
update `docs/PROJECT_CONTEXT.md` for substantive changes, and add tests for
behaviour changes.

## License

Code: MIT (`LICENSE`; the copyright holder line still needs the authors'
confirmation). Data: `DATA_LICENSE.md`. Citation: `CITATION.cff` (authors to be
confirmed).
