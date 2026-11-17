# 0007: Public API review

Status: Running

## Goal
Every exported name outside `internal/`, `sample/` and `bench/` is one a user needs, named by one
rule, with one obvious way per job. Framework internals live in `internal/` or are unexported, a check
keeps the surface from growing unnoticed, and the docs describe the surface that is left. The surface
leaves room for the next roadmaps (a render route kind for SSR, authentication as guards).

## Scope
- In: every package outside `internal/`, `sample/` and `bench/` (about 500 exported names today:
  `ctx` 100, `common` 82, `core` 68, `exception` 41, `devtool` 24, the rest under 20 each); the root
  package's aliases; `sample/` and every README and `.ginject/` file follow the new names.
- In: the WebSocket packages (`wsevent`, `memorybroker`, the WS parts of `core` and `ctx`) for names
  only: hide or rename, no behavior change.
- Out: behavior changes, except where two APIs do one job and one is removed; the WebSocket rework
  (its own roadmap); new features (render route kind, auth): only room is left for them.

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Inventory: every exported name with its package, kind, where it is used (other packages, `sample/`, READMEs) and a first class: user API, extension point (an interface users implement), framework internal | light | - | - | `docs/review/api-inventory.md` lists every exported name with a class | done |
| 2 | Target surface: per package, what stays, what moves to `internal/`, what is unexported, what is renamed or merged; the naming rules (settings structs `XxxOptions` passed as `opts *XxxOptions`, `nil` = defaults; constructors; errors; events); the import paths users need; room for the render route kind and auth | light | docs/design/0017-public-api.md | 1 | the design lists every exported name that stays, with its reason, and every move or rename | done |
| 3 | API guard (before the moves, so each move is a diff of the file): a check that writes the exported API of every public package to a committed file and fails `make check` when the code differs (like Go's `api/` check), so every addition is a reviewed diff | light | docs/design/0017-public-api.md | 2 | adding an exported name without updating the file fails `make check` | done |
| 4a | Move to `internal/` (path only): `pattern`, `wsevent`, `memorybroker`, `routing`, `devtool`, `accesslog` | light | docs/design/0017-public-api.md | 3 | the packages live under `internal/`; `api/ginject.txt` shows only removals; `make check` green | done |
| 4b | Move `trace`, `memorycache`, `aggregation`, `versioning`; add `ginject.TraceTransport`, `cache.NewMemoryCache`, `cache.MemoryCacheOptions` | light | docs/design/0017-public-api.md | 4a | as the design's table; samples build | done |
| 4c | Move `common`, `ctx`, `core` whole (package names unchanged); `Logger` / `LogAdapter` to `log`; complete the root package; `sample/` and `bench/` on the public paths | light | docs/design/0017-public-api.md | 4b | the design's import list is the only public surface; risk rows 1-4 of the design tested | done |
| 5 | Hooks: framework-only members of user-visible types become package-level functions of the internal packages; duplicate ways merged; `any` for the three layer types | tdd | docs/design/0017-public-api.md | 4c | only the design's members remain reachable; hot-path benchmarks within the design's bound | done |
| 6 | Naming pass over the remaining packages: settings structs, constructors, error and event names by the design's rules | light | docs/design/0017-public-api.md | 2 | every public name follows the rules; the API file matches the design | done |
| 7 | Docs: README of every package, the main README, `.ginject/` and `CLAUDE.md` rewritten around the new surface | light | - | 3-6 | every exported name is documented; no doc names a removed one | todo |
| 8 | Measure: `make bench` and the `bench/` load test against `docs/review/http-0006.md` | light | - | 4-6 | no scenario slower than the noise band; `docs/review/http-0007.md` | todo |

Path: `tdd` (tester writes failing tests, builder makes them pass) or `light` (one agent or the main
session writes tests and code together, for small steps). State: `todo`, `doing`, `done`, `blocked`.

## Decisions
- Ginject is not published yet: breaking changes are free. `sample/`, docs and the knowledge base move
  with the code in the same run; there are no deprecation aliases.
- `pattern` moves to `internal/pattern` (backlog, 2026-11-17).
- Branch `main` (v3) is a reference, not a source to merge: its decisions on names (D-032 settings
  structs, D-028 messages, D-003 layer interfaces) are inputs to the design where they fit `master`.
- Approved 2026-11-18 with the proposals below:
  - **Import surface:** users import the root package `ginject` plus `exception`, `log`,
    `middlewares/*`, `modules/*` and `guards/*`. What users need from `common`, `ctx` and `core` is
    reached through the root package; the rest of those packages moves to `internal/` or is
    unexported. The design decides package by package how far this goes (an interface users
    implement may stay where it is).
  - **Request-path performance** (Ginject at 74-85 % of net/http with the log off) is a separate
    roadmap, not part of this one.
  - **`devtool`** moves to `internal/` until the devtools platform starts; only `EnableDevtool`
    stays public.

## Log
- 2026-11-18: design 0017 approved with every recommendation of its open questions; run started at `2647b71`.
- 2026-11-18: step 3: a reached named type whose underlying type is neither a struct nor an interface also gets a `reach <T>, type <underlying>` line (the design listed only fields and methods, so `type T int` becoming `string` went unseen). `make api-check` takes about 0.7 s with a warm build cache, 18 s cold.
- 2026-11-18: step 1 (inventory) is done by the architect in the design pass, as the input of design 0017.
- 2026-11-18: after the design, step 4 split in 4a-4c (the `core` / `common` / `ctx` move is the risky part) and step 5 turned from "unexport" into hooks: names used across packages can't be unexported, the packages move to `internal/` instead. The old step 5 examples (`Construct`, `BuildHTTPInterceptMiddleware`, `InsertedRoutes`) no longer exist on `master`.
- 2026-11-18: step 4a: besides the removals, `api/ginject.txt` gains `reach` lines for `memorybroker` and `routing` types that `core` and `common` still expose (`WSOptions.Broker`, the router); they go with 4c and 5.
- 2026-11-18: step 4c: `ginject.ModuleBuilder` is a variable holding `core.ModuleBuilder` until step 5 exports the builder type (the design's `func ModuleBuilder() *core.ModuleBuilder`). B4 checks every file of `sample/` (tests too: users copy them) and `bench/` except `bench/*_test.go` (a measuring module whose tests use `internal/test`). `api/ginject.txt`: 782 lines before step 4, 437 after.
- 2026-11-18: step 5: every hook stays inlinable except `HTTPInit`, `WSInit` and `Aggregate` (not inlinable as methods either); no `internal/core` caller lost inlining. Interleaved benchmarks against the previous commit: Static 0.98, Chain 0.96, Invoke_Param 1.01, allocations equal; no fallback used. The versioning renames (`Options`, `Type`) are done in step 6 with the naming pass.
