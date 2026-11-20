# 0011: Examples

Status: Done

## Goal
A developer new to Owlkei Go learns it from `examples/`: five small projects in learning order, each one
a standalone Go module that runs, has a README and a test, and can be copied out of the repository as
the start of a real app. The old kitchen-sink `sample/` is gone.

## Scope
- In: a new `examples/` folder (one module per example, `replace go.owlkei.com/owlkei => ../..`), a `go.work`
  at the root, CI and `make check` running every example's tests, the README's "Examples" section and
  its quick links, CLAUDE.md and `.owlkei/` where they name `sample/`.
- Out: a WebSocket example (after the WebSocket roadmap); framework changes (an example that needs one notes it in the backlog instead); an auth and
  session example (after the security roadmap); the `bench/` module.

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Layout: `examples/` with one module per example, `go.work`, CI and `make check` run `go vet` and `go test -race` in every example; a shared README template (what you learn, run, try with `curl`, files to read first) | light | - | - | an empty example builds and its test runs in CI | done |
| 2 | `01-hello`: one controller, two routes, a parameter (the Quick start) | light | - | 1 | README and test; `go run .` answers the README's `curl` calls | done |
| 3 | `02-rest-api`: CRUD of one resource with a module, a provider, a pipe that validates, exceptions (404, 422), JSON | light | - | 1 | test covers every route and error | done |
| 4 | `03-pages`: move `sample/pages` (`RENDER_`, `SUBMIT_`, layout, a form with field errors, Owlkei UI markup) | light | - | 1 | its tests pass in the new module | done |
| 5 | `04-async-modules`: move `sample/async` (`AsyncImports`, `Retry`, 503 until ready) | light | - | 1 | its behavior is tested in the new module | done |
| 6 | `05-production`: `helmet`, `csrf`, `cors`, `loadshed`, `throttler`, access log, graceful shutdown, one logging layer | light | - | 1 | test checks the security headers, a refused request and a clean shutdown | done |
| 7 | Remove `sample/` (`main.go`, `shop`, `shared`, `benchmarks`, `confs`, `data`, `pages`, `async`); update every link to it | light | - | 2-6 | no reference to `sample/` left | done |
| 8 | Docs: README "Examples" section in learning order, quick links, CLAUDE.md, `.owlkei/`; one review of every example against the "No AI slop" list (Decisions), findings fixed; `make check`, push, CI green | light | - | 7 | the review finds nothing left; CI green on `main` | done |

Path: `tdd` (tester writes failing tests, builder makes them pass) or `light` (one agent or the main
session writes tests and code together, for small steps). State: `todo`, `doing`, `done`, `blocked`.

## Decisions
Approved by the owner on 2026-11-19, to run before the security roadmap. Agreed with the owner on 2026-11-19:
- `examples/` (the Go community's name) replaces `sample/`; every example is its own module, so it is
  exactly what a user's project looks like (drop the `replace` once `go.owlkei.com` serves).
- Five examples in learning order; the `shop` app is dropped, not rewritten. No WebSocket example
  until the WebSocket rework lands (owner, 2026-11-19).
- Examples use only the public API and real-looking code; no framework change in this roadmap.
- **No AI slop** (owner, 2026-11-19: developers read examples more than any other code). Every example
  reads like code a careful Go developer wrote by hand:
  - one domain per example with real names and data (a library's books, a team's chat rooms), never
    `foo`, `item1`, `data`, `result`, `handler`, `manager`, `utils`, `helper`;
  - comments only where the reason isn't in the code (why a guard is global, why a module is async);
    never a comment that repeats the line (`// create the app`), section banners, or "This function…";
  - idiomatic Go: small functions, early returns, errors wrapped with context, no interfaces or layers
    the example doesn't need, no dead code, no `TODO`, no commented-out code, no emoji;
  - each example teaches its one concept and stays short (`01-hello` under 40 lines of Go; the others
    under about 300); repeated boilerplate across examples is cut, not copied;
  - READMEs in plain, direct English (the style rules of `.owlkei/conventions.md`), with the exact
    commands and outputs that the test proves.
  Step 8 adds one review pass over all examples against this list (the reviewer reads them as a
  newcomer would), and its findings are fixed before the push.
- Commits follow `.claude/commands/commit.md`: no Claude attribution or trailers.

## Log
- 2026-11-19: run started at `31c7eb3` (after `578de69`, a fix of the bench target the rename broke). The owner asked for steps 1-4 first.
- 2026-11-19: step 1: no `go.work`. A workspace would pull `bench/` and every example into the root's `go test ./...`, `go vet` and the API check; `make examples` loops over `examples/*/go.mod` instead (vet + `test -race` in each), run by `make check` and by a CI step. Each example's `go.mod` requires `go.owlkei.com/owlkei v0.0.0` with `replace => ../..`, the same lines a user writes until the vanity path serves.
- 2026-11-19: step 3: providers can't have unexported fields, so the catalog's shared state is an exported pointer set by `NewCatalog()`, and `NewBooksModule()` builds a fresh catalog per app (each test gets its own). `golangci-lint` is clean in every example (run by hand; the root lint skips nested modules).
- 2026-11-19: step 4 landed as two commits: the move (`2b418cb`) and the module files and README (`ff565e5`).
- 2026-11-19: `TestLoadShed_ArrivalAtFreedSlotMayTakeIt` failed once in a pre-commit hook under load (passes 30 of 30 alone): another timing-sensitive test for the backlog list.
- 2026-11-19: step 5: the old `sample/async` had no test; `04-async-modules` has two (a late warehouse: 503 with `Retry-After`, then 200; a bad URL: 503 without it) and a stock list instead of `len(sku) * 10`. Step 6: `05-production` runs its tests on a real listener (the body limit and the shutdown live in the serving call). Step 7: `surface_test.go` (B4) and `internal/apicheck` now look at `examples/`.
- 2026-11-19: step 8: the review found 4 must-fix, 7 should-fix and 4 nits, all fixed (the pages README's JavaScript claim, a file named `helpers_test.go`, a 130-line test split in two with the history check and the hash recomputation dropped, a comment on why the async tests poll, the warehouse's dead field and bare errors, `NewBookingsModule`, clearer names, `log.Fatal` and port 3000 in every example, comment and README cleanups, a capped inbox). Receivers stay short in examples (Go style; `instance` stays in README snippets), `.owlkei/conventions.md` says so. `make check` green (L12 570 s).

