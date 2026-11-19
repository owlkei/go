# 0010: Rebrand to Owlky Go

Status: Running

## Goal
The framework is Owlky Go: users write `import "go.owlky.com/owlky"` and `owlky.New()`, every
README, sample, log and panic text says Owlky, and the repository's own tooling (knowledge base,
agents, skills, API check) follows the new names. Behavior does not change.

## Scope
- In: past roadmaps, designs and reviews too (the owner chose a clean rename, 2026-11-19; one note in
  `docs/roadmap/README.md`). `go.mod` (module `go.owlky.com/owlky`) and every import in the repository (342 files), the root
  package `ginject` → `owlky`, `bench/go.mod`, `api/ginject.txt` and `internal/apicheck`, the `ginject:`
  prefix of errors, logs and panics (47 places) and the tests that pin them, `.ginject/` → `.owlky/`,
  the agents `ginject-*` → `go-*` and the skills and CLAUDE.md that name them, README and every package
  README, `sample/`, links to `github.com/dangduoc08/ginject` → `github.com/dangduoc08/go`, the test-only
  `GINJECT_FRESH_PROCESS`, the backlog.
- Out: the vanity server for
  `go.owlky.com` (owlky.com website, DNS); the local folder `~/Projects/ginject` (Claude Code memory is
  keyed by it); the env names of the planned mode switch (decided with "Less configuration in `main`").

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Module path and package: `go.mod` → `go.owlky.com/owlky`, every import, root package `owlky`, `bench/go.mod` (its `replace` keeps working without `go.owlky.com`) | light | - | - | `go build ./...`, `go vet ./...` and the bench module build; no `dangduoc08/ginject` import left | todo |
| 2 | API check: `api/ginject.txt` → `api/owlky.txt`, `internal/apicheck` and `make api-check` follow | light | - | 1 | `make api-check` passes; the file shows only the package rename | todo |
| 3 | Texts: the `ginject:` prefix of errors, logs and panics → `owlky:`; startup banner and log names that say Ginject; tests that pin them; `GINJECT_FRESH_PROCESS` → `OWLKY_FRESH_PROCESS` | light | - | 1 | `git grep -i ginject -- '*.go'` is empty; `go test -race ./...` green | todo |
| 4 | Tooling: `.ginject/` → `.owlky/`, `.claude/agents/ginject-*` → `go-*`, the skills and CLAUDE.md that name them, lint and hook configs | light | - | 1 | no reference to `.ginject/` or `ginject-*` agents | todo |
| 5 | Docs: README (Owlky Go, pairs with Owlky UI, import path, a `replace` note until `go.owlky.com` serves), every package README, `sample/`, badges and links to `github.com/dangduoc08/go`, the backlog | light | - | 1-4 | `git grep -i ginject` matches only this roadmap, the note in `docs/roadmap/README.md` and the backlog entry that names the old brand; README examples compile | todo |
| 6 | Verify and ship: `make check`, CI green on `main`, push | light | - | 1-5 | CI green on the pushed `main` | todo |

Path: `tdd` (tester writes failing tests, builder makes them pass) or `light` (one agent or the main
session writes tests and code together, for small steps). State: `todo`, `doing`, `done`, `blocked`.

## Decisions
Agreed with the owner on 2026-11-19 (backlog › "Rebrand to Owlky Go"):
- Product name Owlky Go ("Server" reads like VPS hosting); import path `go.owlky.com/owlky`, package
  `owlky`; the repository is renamed `dangduoc08/ginject` → `dangduoc08/go` by the owner now, and moves
  to the `owlky` org when its name is free again (about 2027-02-17).
- `.ginject/` → `.owlky/`; agents and skills `ginject-*` → `go-*` (no clash with the UI repository's
  `owlky-*` agents).
- A pure rename: no behavior change, so no design; the owner's approval of this roadmap covers the
  public rename. No deprecation aliases (nothing is published).
- Commits follow `.claude/commands/commit.md`: no Claude attribution or trailers.

## Log
