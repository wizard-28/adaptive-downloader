# Paper builds

- `paper_conf.pdf` — **6-page conference cut**, built from
  `paper_conf_8.md`:
  ```bash
  python3 tools/paper_to_ieee.py --conf --src reports/paper/paper_conf_8.md
  ```
  It contains the system architecture, four result figures (aria2 ratios,
  oracle headroom, cap controller, engine ablation) and two summary tables
  (screens, learned agents). Every float is cited in the text and carries a
  `\label`; the converter resolves `{{fig:...}}` / `{{tab:...}}` call-outs.
- `paper_conf_full.pdf` / `.tex` — the previous 14-page conference version,
  kept as a backup. Do not edit the generated `.tex`; edit the Markdown source.
- `paper_access.pdf` — the full IEEE Access version (16 pages), still built from
  `reports/paper_draft.md` with `python3 tools/paper_to_ieee.py`.
- `make_architecture.py` — draws `figures/architecture.png` as a compact
  horizontal pipeline at 7 pt, thin blue strokes and pale fills, sized for full
  text width so it matches the result figures.
- `make_conf_figs.py` — redraws the aria2-ratio and cap-controller figures at
  one-column print size (`figures/fig1_ours_vs_aria2.png`,
  `figures/fig3_within_download_controller.png`). The full-width versions in
  `reports/ieee_access/figures/` were twice too large for a column.

The short source keeps the central measurements and the engine ablation; the
long paper retains every screen, the full learned-policy inventory and all
caveats. `paper_conf_8.md` is the source to edit for the short version.

## Recent layout fixes

- Tables use column widths proportional to their content, not equal thirds.
- The architecture image is cropped with `bbox_inches="tight"` and included at
  `0.84\textwidth`; the earlier build had large white side margins.
- References use one consistent size and the two columns are balanced; the
  editorial provenance notes were removed from the entries.
- `\raggedbottom` removes the mid-column justification hole on page 3.
- `\clearpage` starts the reference list on its own page rather than leaving a
  single reference at the foot of page 5.

## Formulas

The 6-page cut has three numbered display equations: the concurrency cap
(`eq:cap`), the paired geometric-mean ratio (`eq:ratio`) and the oracle
objective and headroom definition (`eq:oracle`), cited as Eq. (1)–(3). The
online-tuner utility is described inline. The Markdown source
uses `$$ ... $$ {#label}`; `tools/paper_to_ieee.py` turns that into a numbered
`equation` environment and resolves `{{eq:label}}` references.

## Text pass

The 6-page cut was rewritten for natural scholarly prose. An adversarial
stylometry review scored the first version at 34/100 AI-likeness; after the
pass, two independent reviewers scored the final text at **12 %** and **15 %**
and neither would flag it to an IEEE editor. Neither number comes from a
commercial detector; they are subagent judgements against a fixed rubric.

What changed: the abstract and introduction were rebuilt around the engine
result instead of a result inventory; the five contribution bullets became
three sentences; the extended-abstract boilerplate was removed; the conclusion
became an argument rather than a recap; a duplicated pilot result was fixed;
bold emphasis was cut back; and paragraph endings were varied so that fewer
paragraphs close on a verdict. All numbers and caveats are unchanged, and
`tools/claims_check.py` still reports 233/233.

## What is left

1. The repository link in the paper (https://github.com/wizard-28/adaptive-downloader)
   must be public before the paper is sent; it returns 404 when signed out.
2. Page limit of the venue not checked (the cut is 6 pages with references).
3. Related work rests on full-text reads of MDTP, Balaprakash et al., the
   journal version of Falcon and FastBioDL. BOLA, DeLoad and the SC21 Falcon
   paper (`arifuzzaman2021online`) are known from abstracts only; the text says so.
4. Re-run `python3 tools/claims_check.py` after any number change; it reports
   233/233.
5. Style: a subagent review against a fixed rubric scores the text about 22 %
   AI-like (single-model judgement, not a commercial detector).
