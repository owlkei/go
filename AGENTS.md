# AGENTS.md

Instructions for AI coding agents (Codex, Cursor, Copilot, Claude Code and others) working in this
repository. They are kept in two places; read both before changing code:

- **[`CLAUDE.md`](CLAUDE.md)**: commands, product principles, the workflow, the architecture and the
  rules every change follows. It is written for Claude Code but applies to any agent.
- **[`.owlkei/`](.owlkei/INDEX.md)**: the knowledge base (routing, request pipeline, handler
  injection, modules, WebSocket, gotchas, package reference). Code conventions are in
  [`.owlkei/conventions.md`](.owlkei/conventions.md). When a document disagrees with the code, the
  code wins: fix the document in the same change.

## Before you finish

- `make check` passes: gofmt, `go vet`, the public API listing (`make api-check`; after an intended
  API change, `make api`), personal data (`make check-names`), `golangci-lint`, `go test -race`, and
  the examples.
- A package whose code changed has an up-to-date `README.md`.
- No comments in Go code; tests are named `Test<Subject>_<Behavior>`.

## Commits

Conventional Commits in English, one concern per commit, an implementation with its tests
(`.claude/commands/commit.md`). No AI attribution or trailers (`Co-Authored-By` and similar). Never
commit personal data, local paths or secrets.

## Planned work

Larger work goes through a roadmap (`docs/roadmap/`) and a design (`docs/design/`) that the
maintainer approves before code is written. Decided work without a roadmap is in
[`docs/roadmap/backlog.md`](docs/roadmap/backlog.md).
