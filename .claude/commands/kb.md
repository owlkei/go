---
description: Keep the AI knowledge base (CLAUDE.md + .owlky/) true to the code. Modes - update (default) fixes docs, audit reports without editing, readiness simulates a new agent.
argument-hint: "[update | audit | readiness] [package or topic]"
---

# Knowledge base: $ARGUMENTS

The knowledge base is `CLAUDE.md` plus `.owlky/` (entry point `.owlky/INDEX.md`). Package READMEs
belong to `/doc`, not to this command.

Parse `$ARGUMENTS`: the first word is the mode (`update` when it is missing or is not a mode); the rest
narrows the scope to a package (`routing`, `modules/cache`) or a topic (`websocket`, `pipeline`). No
scope: everything changed since the last commit that touched `.owlky/`
(`git log -1 --format=%H -- .owlky CLAUDE.md`, then `git diff --stat <that>..HEAD -- '*.go'`).

## Ground rules (every mode)

- **The code is the source of truth.** Every type, function, method, field, constant and file a doc names
  must exist: `grep` it before you write it or accept it. This repository already had a whole release of
  docs with invented APIs (`common.REST`, interface types that are really `any`): never repeat that.
- Never infer an API from a NestJS habit, a name or another framework. When behavior is unclear, read the
  implementation and its tests; when it is still unclear, write "unverified" or ask, never guess.
- Don't create documents or JSON metadata files that nothing maintains. Edit the existing pages; add a page
  only when a subsystem has none, and list it in `INDEX.md`.
- One explanation per concept. Other pages link to it instead of repeating it.
- Never edit Go code in this command.

## Mode `update` (default)

1. Find what changed in scope: exported API (`go doc ./<pkg>` or the exported declarations), lifecycle,
   pipeline order, panics and their messages, concurrency guarantees.
2. For each change, find every doc that mentions it (`grep -rn "<Name>" CLAUDE.md .owlky`), and fix it.
3. Check the pages you touched for names that no longer exist: for each identifier in backticks,
   `grep -rn "<Ident>" --include='*.go' .` must find a declaration.
4. Keep `CLAUDE.md` short: commands, the core concepts, conventions that apply to every change. Details
   go in `.owlky/`.
5. Report: files changed, one line each on what was wrong, and anything left unverified.

## Mode `audit` (read only)

Do steps 1 and 3 of `update` over the whole scope without editing. Also look for: contradictions between
pages, duplicated explanations, broken links between pages, subsystems with no page, and JSON metadata in
`.owlky/` that no longer matches the code.

Report one list, most severe first: `file:line`, what is wrong, the evidence (the grep or the code
location), and the fix. Severity: **critical** (the doc would make an agent write code that does not
compile or misbehaves), **high** (missing or misleading for a common task), **low** (duplication, style).

## Mode `readiness` (read only)

Act as an agent that has never seen this repository and may only read `CLAUDE.md` and `.owlky/`.
Pick 6 to 8 realistic tasks for the scope, such as: an HTTP controller with a path param and a body DTO,
a guard for HTTP and WS, an interceptor, an exception filter, a WS handler that publishes, a dynamic
module with options, a test for a handler. For each task, write the code you would produce from the docs
alone, then check it against the code (names, signatures, method shapes, registration).

Report per task: pass or fail, which doc misled or lacked the answer, and the source file you had to read.
End with the top fixes in priority order. Don't give numeric scores.
