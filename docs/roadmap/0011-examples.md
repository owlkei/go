# 0011: Examples

Status: Running

## Goal
A developer new to Owlky Go learns it from `examples/`: five small projects in learning order, each one
a standalone Go module that runs, has a README and a test, and can be copied out of the repository as
the start of a real app. The old kitchen-sink `sample/` is gone.

## Scope
- In: a new `examples/` folder (one module per example, `replace go.owlky.com/owlky => ../..`), a `go.work`
  at the root, CI and `make check` running every example's tests, the README's "Examples" section and
  its quick links, CLAUDE.md and `.owlky/` where they name `sample/`.
- Out: a WebSocket example (after the WebSocket roadmap); framework changes (an example that needs one notes it in the backlog instead); an auth and
  session example (after the security roadmap); the `bench/` module.

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Layout: `examples/` with one module per example, `go.work`, CI and `make check` run `go vet` and `go test -race` in every example; a shared README template (what you learn, run, try with `curl`, files to read first) | light | - | - | an empty example builds and its test runs in CI | todo |
| 2 | `01-hello`: one controller, two routes, a parameter (the Quick start) | light | - | 1 | README and test; `go run .` answers the README's `curl` calls | todo |
| 3 | `02-rest-api`: CRUD of one resource with a module, a provider, a pipe that validates, exceptions (404, 422), JSON | light | - | 1 | test covers every route and error | todo |
| 4 | `03-pages`: move `sample/pages` (`RENDER_`, `SUBMIT_`, layout, a form with field errors, Owlky UI markup) | light | - | 1 | its tests pass in the new module | todo |
| 5 | `04-async-modules`: move `sample/async` (`AsyncImports`, `Retry`, 503 until ready) | light | - | 1 | its behavior is tested in the new module | todo |
| 6 | `05-production`: `helmet`, `csrf`, `cors`, `loadshed`, `throttler`, access log, graceful shutdown, one logging layer | light | - | 1 | test checks the security headers, a refused request and a clean shutdown | todo |
| 7 | Remove `sample/` (`main.go`, `shop`, `shared`, `benchmarks`, `confs`, `data`, `pages`, `async`); update every link to it | light | - | 2-6 | no reference to `sample/` left | todo |
| 8 | Docs: README "Examples" section in learning order, quick links, CLAUDE.md, `.owlky/`; one review of every example against the "No AI slop" list (Decisions), findings fixed; `make check`, push, CI green | light | - | 7 | the review finds nothing left; CI green on `main` | todo |

Path: `tdd` (tester writes failing tests, builder makes them pass) or `light` (one agent or the main
session writes tests and code together, for small steps). State: `todo`, `doing`, `done`, `blocked`.

## Decisions
Approved by the owner on 2026-11-19, to run before the security roadmap. Agreed with the owner on 2026-11-19:
- `examples/` (the Go community's name) replaces `sample/`; every example is its own module, so it is
  exactly what a user's project looks like (drop the `replace` once `go.owlky.com` serves).
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
  - READMEs in plain, direct English (the style rules of `.owlky/conventions.md`), with the exact
    commands and outputs that the test proves.
  Step 8 adds one review pass over all examples against this list (the reviewer reads them as a
  newcomer would), and its findings are fixed before the push.
- Commits follow `.claude/commands/commit.md`: no Claude attribution or trailers.

## Log
