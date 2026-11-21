# Roadmaps

A roadmap is a set of steps the maintainer and Claude agree on before any design or code. Flow
(`/roadmap`): discuss → roadmap (maintainer approves) → designs for the steps that need one
(maintainer approves) → Claude runs every step to the end without further approval and commits its
work on the current branch → the maintainer reviews the commits and pushes them.

- **Name:** `NNNN-<slug>.md`, numbered in order.
- **Status:** `Draft` → `Approved` → `Designed` (designs approved) → `Running` → `Done`.
- Claude keeps the Steps table and the Log up to date while it runs, so a run that loses its context
  resumes from the file.
- **Who is who in the Logs:** the *owner* or *maintainer* decides; the *orchestrator* (or *main
  session*) is the Claude Code session that runs the roadmap and calls the agents of
  `.claude/agents/`: the *architect* (designs), the *tester* (failing tests), the *builder* (code that
  makes them pass), the *reviewer* and *security* (reviews and audits). Roadmaps and designs are
  numbered separately: in a Log, `0014` next to a step or a revision is the design
  `docs/design/0014-…`.

## Template

```markdown
# NNNN: <title>

Status: Draft

## Goal
What is true when the roadmap is done, in two or three sentences.

## Scope
- In: the packages and behaviors this covers.
- Out: what it deliberately leaves alone.

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | <what> | tdd | docs/design/NNNN-x.md | - | <observable result> | todo |
| 2 | <what> | light | - | 1 | <observable result> | todo |

Path: `tdd` (tester writes failing tests, builder makes them pass) or `light` (one agent or the main
session writes tests and code together, for small steps). State: `todo`, `doing`, `done`, `blocked`.

## Decisions
Choices made while discussing, so designs don't reopen them.

## Log
Dated lines written during the run: deviations from a design, escalations, blocked steps and why.
```

The framework was called Ginject until roadmap 0010 (2026-11-19) renamed it Owlky Go
(`go.owlky.com/owlky`), and Owlky Go until roadmap 0014 (2026-11-21) renamed it Owlkei Go
(`go.owlkei.com/owlkei`). The old names were replaced in every document, history included, except the
two roadmaps that describe the renames; the original texts are in the git history. The files under
`examples/03-pages/web/assets/` keep the names of the Owlky UI build they were copied from.
