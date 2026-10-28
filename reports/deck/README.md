# Presentation deck (viva, 2026-10-29)

The deck is a Claude Slides artifact:
**https://claude.ai/artifact/Pny8DjgRgQhfdW1bXEbZjh** (private to the owner;
share it from the page's Share menu; Share › Export gives .pptx or PDF).

This folder is the deck's source. Every slide is one file:

- `project/deck.json`: title, slide `order`, sections, fonts.
- `project/slides/<id>.html`: one `<section>` per slide, inline styles on a
  1920 × 1080 canvas, speaker notes in the final `<aside>`.

## Editing from a new Claude Code session

1. Edit the slide files here (or ask Claude to).
2. Publish with the Artifact tool: `url` = the link above,
   `root` = `reports/deck` (absolute: `/workspaces/mvp/reports/deck`),
   `file_path` = one changed file, `files` = the other changed files as
   `{"project/slides/<id>.html": "project/slides/<id>.html"}`.
   Send `project/deck.json` only when the order, sections or title change.
3. If the publish is refused because the deck was edited in the browser,
   read the named files back from the artifact, merge, and publish again.

Charts come from `reports/handbook/make_deck_figs.py`, which writes
`reports/handbook/figs_deck/`. They are uploaded to the artifact as assets
(`asset: true`), and slides reference them as `/_blob/<id>`. A re-drawn chart
must be uploaded again and its new `/_blob/` URL put into the slide.

## Rules the team asked for

- The examiner is seeing the project in person for the first time: slides must
  explain themselves; give the setup, say what a chart shows, and keep a
  takeaway on every results slide.
- No code names on slides (no D2, S0–S5, CAP8, H_C12, kcdn, v1/v2, W1–W6…).
  Use descriptive names: "server slow, then fast", "cap from 8 s",
  "first retrained agent", "broadly trained agent", "kernel.org CDN", etc.
- Text at least 24 px; charts drawn at slide size (no tiny insets).
- Speaker notes in simple English: what to say, the idea in simple words, and
  plain definitions of every term on the slide.
- Numbers must match `reports/paper_draft.md` (checked by
  `tools/claims_check.py`) and the study folders under `testdata/`.

## Slide order

cover · question · prior (what earlier work did and what we did not find) ·
novelty (what is new) · answer · roadmap · background · architecture · engine ·
platform · method · r1 (62.4 % vs aria2) · r1b (other timetables, NGINX, delay
and loss) · r1c (axel and lftp) · r2a (mobile traces swing) · r2 (perfect
controller gains 0.41 %) · r2b (other conditions) · r3 (throttling server, drop
rule) · r3b (engine fixes) · r3c (fixed engine beats adaptive) · r4a (what the
AI controllers see, do and are rewarded for) · r4 (early AI) · r4b (joint
agent) · r4c (broadly trained agent) · r4d (all vs big pieces) · r5 (live
pilot) · r5b (real-server no-harm check) · vsaria2 (core answer) · summary ·
whyrl · limits · team · demo · close
