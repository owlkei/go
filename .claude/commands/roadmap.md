---
description: Plan and run multi-step work - discuss and write a roadmap (user approves), design the steps (user approves), then run every step to the end without further approval, committing the work on the current branch. Never pushes.
argument-hint: "new <topic> | design <roadmap> | run <roadmap> | status [roadmap]"
---

# Roadmap: $ARGUMENTS

The first word is the mode. Roadmaps live in `docs/roadmap/` (template and statuses in its
`README.md`); `<roadmap>` is a file name or number there, and the most recent one when omitted.

The user approves exactly two things: the **roadmap** and the **designs**. Tests, code, refactors and
docs need no approval. Once both are approved, `run` stages and commits its own work (see
Commits); you never push, merge or tag.

## `new <topic>`: discuss, then write

Discuss the goal, scope, constraints and risks with the user until the steps are clear; ask only what
changes the plan. Write `docs/roadmap/NNNN-<slug>.md` (Status: Draft) with each step's path:

- `tdd` when the step adds or changes public behavior, concurrency or security;
- `light` for small steps: a bug fix, a private helper, docs, a rename, a follow-up of an existing pattern.

Steps that need a design are the `tdd` ones plus any `light` step that changes an exported API. Show
the table and **wait**. On approval set `Status: Approved` and continue with `design`.

## `design <roadmap>`: one architect pass, one approval

Run `ginject-architect` **once** for all the steps that need a design (split into two or three calls
only when the steps share no code). Give it the roadmap file and its Decisions section, so it does not
reopen them. Model: the rules of `/feature` step 1, applied to the whole batch (`fable` if any step is
complex or the weekly reset is near, else `opus`; fall back to `opus`).

Show the user the design files, their public APIs and open questions, and **wait**. Apply their
changes, set each design to `Status: Approved` and the roadmap to `Designed`.

## `run <roadmap>`: to the end, no stops

Before the first step:

- Record `git status --porcelain` in your notes: files that were already modified or untracked are
  the user's, and you never stage them.
- Work on the current branch (normally `main`): no separate roadmap branch (the user's choice,
  2026-11-16). Record the starting commit so the report can list `git log --oneline <start>..HEAD`.

Set `Running`. Take steps in dependency order; for each, set `doing`, then:

- **`tdd`:** `ginject-tester` (red), then `ginject-builder` (green), with the checks of `/feature`
  steps 3 and 4 (only test files changed; tests untouched by the builder).
- **`light`:** do it yourself in the main session, tests included, or with one `ginject-builder` call
  when it needs a lot of reading.

A step is `done` when its "Done when" holds and the touched packages pass `go vet`, `golangci-lint`
and `go test -race`. Update its state in the file right away.

**You coordinate failures; you never stop to ask.**

- A failing builder gets one more attempt with the failure output. After that, call
  `ginject-architect` with both reports: it decides whether the design, the tests or the code is wrong.
  Apply its decision (a revised design, tests redone through the tester, or code through the builder)
  and write one Log line: what changed against the approved design and why.
- A step that still cannot be finished is `blocked` with its reason in the Log. Continue with every
  step that does not depend on it.

When every step is `done` or `blocked`:

1. `ginject-reviewer` **once**, over the whole working tree against the roadmap and its designs. Fix
   critical and high findings (code through the builder or yourself, tests through the tester).
2. `make check`.
3. Set `Done` (or leave `Running` if steps are blocked) and report: steps and states, deviations from
   the designs, blocked steps, review findings left open, the `make check` result, and the commits
   made (`git log --oneline <start>..HEAD`). The user reviews the commits and pushes them.

### Commits

- Commit whenever a coherent change is complete and its packages are green: usually after a step,
  sometimes after a few small steps that belong together, and once more after the review fixes. Never
  commit red tests (a TDD red state stays uncommitted until the step is green).
- Stage only files the run changed (`git add <paths>`, never `git add -A`), never the user's files
  recorded at the start.
- Group and word the commits by the rules of `.claude/commands/commit.md`: one concern per commit,
  Conventional Commits in English, an implementation with its tests, no `--no-verify`, no amend, no
  Claude attribution or trailers.
- A failing pre-commit hook is a failure like any other: fix the cause, then commit again.

## `status [roadmap]`

Print the Steps table and the last Log lines. Read only.

## Token budget

The flow is a default, not a ritual: take the cheapest path that keeps the quality.

- Each agent starts cold. Send it the file paths it needs and the relevant `CLAUDE.md` sections, not
  the conversation; ask for a short report.
- Prefer the main session for small steps (it already has the context) over a new agent.
- One architect pass and one review pass per roadmap, not per step.
- Run package tests per step and `make check` once at the end.
- Don't re-read files you already have, and don't re-run a check whose inputs have not changed.
