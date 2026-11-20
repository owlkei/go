# 0014: Rebrand to Owlkei Go

Status: Approved

## Goal
The framework is Owlkei Go: users write `import "go.owlkei.com/owlkei"` and `owlkei.New()`, every
README, example, log and panic text says Owlkei, the repository lives at `github.com/owlkei/go`, and the
README shows the new Owlkei Go lockup. Behavior does not change.

## Scope
- In: `go.mod` (module `go.owlkei.com/owlkei`) and every import (also `bench/` and the five
  `examples/*` modules), the root package `owlky` → `owlkei`, `api/owlky.txt` → `api/owlkei.txt` and
  `internal/apicheck`, the `owlky:` prefix of errors, logs and panics → `owlkei:`, the startup banner,
  `.owlky/` → `.owlkei/`, CLAUDE.md, every README and document (history included, as in 0010, with the
  note in `docs/roadmap/README.md` updated), links `github.com/dangduoc08/go` → `github.com/owlkei/go`,
  the README logo replaced by the `owlkei-go-*` lockups, the GitHub About text, the backlog.
- Out: the env prefix `OK_` (unchanged: the UI keeps `ok`, which now reads OwlKei); agents and skills
  `go-*` (unchanged); the vanity server for `go.owlkei.com` (website, DNS on the owner's checklist); the
  local folder `~/Projects/owlky/go` (Claude Code memory is keyed by it).

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Module path and package: `go.owlkei.com/owlkei`, package `owlkei`, every import in the module, `bench/` and `examples/*` (their `replace` lines) | light | - | - | `go build ./...`, the bench module and `make examples` build | todo |
| 2 | API check: `api/owlkei.txt`, `internal/apicheck`, `make api-check` | light | - | 1 | the file shows only the rename | todo |
| 3 | Texts: `owlky:` → `owlkei:` in errors, logs and panics and the tests that pin them; banner; test-only env names | light | - | 1 | `git grep -i owlky -- '*.go'` is empty; `go test -race ./...` green | todo |
| 4 | Tooling and docs: `.owlky/` → `.owlkei/`, CLAUDE.md, every README and document, links to `github.com/owlkei/go`, history documents with the note updated | light | - | 1-3 | `git grep -i owlky` matches only the rename note and this roadmap | todo |
| 5 | Brand assets: the `owlkei-go-{horizontal,stacked}-{light,dark}.svg` lockups from the UI repository (branch `rebrand/owlkei`, `apps/storybook/public/brand/`) replace `docs/assets/logo-*.svg`; the README header uses the lockup; badges and the "Part of Owlkei" link point at owlkei.com; GitHub About and topics (`owlky` → `owlkei`) | light | - | 4 | the README renders the new lockup in both themes | todo |
| 6 | Verify and ship: `make check`, push to `owlkei/go`, CI green | light | - | 1-5 | CI green on `main` | todo |

Path: `tdd` (tester writes failing tests, builder makes them pass) or `light` (one agent or the main
session writes tests and code together, for small steps). State: `todo`, `doing`, `done`, `blocked`.

## Decisions
Agreed with the owner on 2026-11-20 (prompted by the UI session's brand notice):
- Owlky becomes Owlkei (the owner bought owlkei.com); the product is Owlkei Go.
- Import path `go.owlkei.com/owlkei`, package `owlkei`.
- The env prefix stays `OK_`.
- The repository moved to the `owlkei` organization on 2026-11-20 (`github.com/owlkei/go`; GitHub
  redirects the old URLs; the local remote was updated).
- The README uses the Owlkei Go lockups from the UI repository's brand kit; the owl mark is unchanged.
- Runs right after roadmap 0013, before the security roadmap; a pure rename, so no design.
- Commits follow `.claude/commands/commit.md`: no Claude attribution or trailers.

## Log
- 2026-11-20: repository transferred `dangduoc08/go` → `owlkei/go` by the orchestrator at the owner's
  request, before the run.
