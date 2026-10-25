# CLAUDE.md

Read `AGENTS.md` first; it holds the repository's working rules. This file
adds instructions specific to Claude Code sessions.

## Maintain the work queue (`queue.md`)

`queue.md` at the repository root is the running work queue: open tasks in
priority order, each marked `[x]` done, `[~]` in progress or `[ ]` not
started, with commit hashes for finished steps.

- Read it at the start of a session and after a context compaction, before
  choosing what to do next.
- Update it whenever a task starts, finishes, is blocked, or is added or
  dropped (with the reason), including tasks the user adds mid-conversation.
- Keep it short. Details belong in `docs/PROJECT_CONTEXT.md`; the queue
  points there.

## Keep the main context clean: delegate to subagents

Hand self-contained side investigations to a subagent instead of doing them
inline. Examples: checking a third-party tool's documentation, source or issue
tracker; long web research; sweeping many files; digging through large logs.
The main session keeps only the conclusion, not the raw pages, logs or file
dumps.

Pick the model to fit the task:

- **Haiku:** narrow, well-specified lookups and checks, such as "confirm this
  function's behaviour in tag X and report the quoted lines", a small
  empirical test, or a file search.
- **Sonnet:** multi-step investigations or edits that need judgement but not
  the full project context.
- **Opus:** hard reasoning, adversarial review of results or claims, or work
  that needs the full research context.

How to delegate:

- Give the subagent everything already established, the exact open
  questions, the limits on what it may touch, and the report format.
- Tell it never to edit project files unless that is the task, and never to
  load the machine while live, timing-sensitive runs are in progress.
- Do not delegate work that is cheaper to do inline, such as a single file
  read or one grep.
- Treat its report as evidence to check, not as ground truth. Verify any
  surprising claim before acting on it.
