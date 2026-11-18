# 0009: Startup and dependency lifecycle

Status: Running

## Goal
A module that waits on a dependency (a database, a cache) can no longer hang or crash the app at start
unless the app asks for that. Factories resolve from their siblings in any order and can fail cleanly
with a startup deadline. `AsyncImports` builds modules in the background: the app serves at once, the
routes that need a module that is not ready answer `503`, and they switch on by themselves when it is.
The lifecycle is documented in detail.

## Scope
- In: `internal/core` (container, module builder, app, server shutdown), the root package
  (`ModuleBuilder().AsyncImports`, the startup deadline option, the retry helper), the HTTP and
  WebSocket dispatch (the readiness gate), `internal/devtool` (module states), `modules/config` and
  `modules/cache` where they are factories, `sample/`, README, `.ginject/`, CLAUDE.md.
- Out: the health check, `Health(ctx)` on providers, the run-time `degraded` state and the circuit
  breaker (backlog › "Startup and dependency lifecycle", nice to have); the storage modules themselves
  (the storage roadmap uses this one); per-controller readiness.

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Names: Module and Module factory everywhere (internal `isDynamicModule`, `dynamicModules`, `callModuleFunc` renamed; README, `.ginject/`, CLAUDE.md wording) | light | - | - | no "dynamic module" left in code or docs; `make check` green | done |
| 2 | Factory signatures: `func(deps...) Module`, `func(deps...) (Module, error)`, `func(ctx, deps...) (Module, error)`; any other shape panics listing the valid ones; one startup deadline for `Create()` (app option, default 30 s) in `ctx`; an error, a panic or the deadline stops `Create()` with a clear message | tdd | docs/design/0020-startup-lifecycle.md | 1 | a factory that never returns fails `Create()` at the deadline with the module's name | todo |
| 3 | Factory scope and order: a factory resolves providers from its siblings in the same `Imports`, factories run in dependency order whatever their position, a cycle panics with the chain | tdd | docs/design/0020-startup-lifecycle.md | 2 | the order cases verified on `402b10f` (sibling, factory after factory) resolve; a cycle names the chain | todo |
| 4 | Module states and the readiness gate: `pending` / `ready` / `failed` per module, held atomically; readiness propagates through imports and injected providers; routes of a module that is not ready stay registered and answer `503` (`503.html` for pages), WebSocket events answer "not ready"; one atomic load on the affected routes only | tdd | docs/design/0020-startup-lifecycle.md | 3 | a gated route answers `503` and the others keep their benchmarks | todo |
| 5 | `AsyncImports(...)`: Modules and factories built in the background from `Create()`; dependents built lazily in dependency order when ready (`OnInit`, providers, controllers, gate open, `OnReady`); failure propagates; a dependency nobody provides fails the module once every async factory is done; controllers in an async factory's module are refused; at shutdown `ctx` is canceled, goroutines are awaited within the shutdown timeout, `OnShutdown` runs only for `ready` modules; one entry in both lists panics | tdd | docs/design/0020-startup-lifecycle.md | 4 | the backlog's lifecycle cases (sql fails → user and booking `503`, booking on redis only works; ready 10 s later switches routes on; shutdown while pending) pass under `-race` | todo |
| 6 | Retry helper for module authors: retry with backoff and jitter until `ctx` is done, attempts reported to the module state | tdd | docs/design/0020-startup-lifecycle.md | 5 | a factory using it becomes `ready` after transient errors and stops at shutdown | todo |
| 7 | Visibility: log lines (`ModuleReady module=<name> after=<d>`, `ModuleFailed` with the cause), module states, attempts and `waiting_for` in the devtool snapshot | light | docs/design/0020-startup-lifecycle.md | 5, 6 | the states show in the log and the snapshot | todo |
| 8 | Docs: a detailed lifecycle section in the README (timeline of `Create()`, background and shutdown; states; when each hook runs; the two refusals; the difference from NestJS `forRootAsync`), runnable examples, `.ginject/`, CLAUDE.md, the `config` / `cache` READMEs; a sample app with an async module | light | - | 2-7 | every new name is documented; the README examples compile | todo |
| 9 | Measure: `make bench` and the `bench/` load test against `docs/review/http-0008.md` | light | - | 4, 5 | no scenario slower than the noise band; `docs/review/http-0009.md` | todo |

Path: `tdd` (tester writes failing tests, builder makes them pass) or `light` (one agent or the main
session writes tests and code together, for small steps). State: `todo`, `doing`, `done`, `blocked`.

## Decisions
Agreed with the owner on 2026-11-18 (backlog › "Startup and dependency lifecycle", items 1-8):
- Two kinds: Module and Module factory. Global is a property, async is how a module is imported
  (`AsyncImports`), `Register…` is only a function name.
- `Imports` fail fast; `AsyncImports` (Modules and factories) never stop the app. The importing app
  decides, not the module author.
- States are one-way (`ready` never goes back). After `ready`, run-time failures are the business code's
  job; nothing is unwired.
- Unknown providers of a factory: `pending` while an async factory runs, `failed` with `can't resolve <T>`
  afterwards, no panic. Controllers in an async factory's module are refused.
- Retries live inside the factory, with a helper from the framework; the framework calls a factory once.
- This roadmap runs before the security roadmap (the owner's choice, 2026-11-18).

## Log
- 2026-11-18: design 0020 approved with every recommendation of its open questions; run started at `688eeb1`.
