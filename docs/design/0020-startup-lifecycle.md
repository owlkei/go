# 0020: Startup and dependency lifecycle

Status: Approved
Packages: `internal/core`, `internal/wsevent`, `internal/devtool`, root package (`aliases.go`), `api/owlky.txt`

Covers steps 2-7 of `docs/roadmap/0009-startup-lifecycle.md`. The decisions of that roadmap and of
`docs/roadmap/backlog.md` › "Startup and dependency lifecycle" are taken as given. Step 1 renames
`isDynamicModule`, `dynamicModules` and `callModuleFunc`; this design says "module factory" and names
today's functions only to locate code.

## Goal
A module factory can return an error, take a `context.Context`, and resolve providers from its siblings in
any order; `Create()` stops with the module's name on an error, a panic or the startup timeout.
`AsyncImports` builds modules in the background: the app serves at once, routes and WebSocket events of
modules that are not ready answer `503` / "not ready", and switch on by themselves. A retry helper, log
lines and the devtool snapshot show what is happening.

## Public API

```go
// internal/core, reached through the root aliases
func (m *ModuleBuilder) AsyncImports(modules ...any) *ModuleBuilder   // step 4
func (app *App) SetStartupTimeout(d time.Duration) *App               // step 2, default 30 s

type RetryOptions struct {       // step 6
	InitialDelay time.Duration   // first wait; <= 0 means 500 ms
	MaxDelay     time.Duration   // cap of one wait; <= 0 means 30 s
	MaxAttempts  int             // <= 0 means until ctx is done
}

// root package
type RetryOptions = core.RetryOptions
func Retry(ctx context.Context, opts *RetryOptions, fn func(ctx context.Context) error) error
```

Accepted entries of `Imports` and `AsyncImports` (no type is added: they stay `any`):

```go
owlky.Module
func(deps...) owlky.Module
func(deps...) (owlky.Module, error)
func(ctx context.Context, deps...) (owlky.Module, error)
```

Why each name is exported: `AsyncImports` is the feature; `SetStartupTimeout` is the app option the roadmap
asks for (a setter like `SetLogger`, because `Create` takes no options and its signature must not change);
`Retry` / `RetryOptions` are for module authors. Module states are **not** exported (log and devtool only).

Exact additions to `api/owlky.txt`:

```
pkg owlky, func Retry(context.Context, *core.RetryOptions, func(context.Context) error) error
pkg owlky, type RetryOptions = core.RetryOptions
reach core.App, method (*core.App) SetStartupTimeout(time.Duration) *core.App
reach core.ModuleBuilder, method (*core.ModuleBuilder) AsyncImports(...any) *core.ModuleBuilder
reach core.RetryOptions, field InitialDelay time.Duration
reach core.RetryOptions, field MaxAttempts int
reach core.RetryOptions, field MaxDelay time.Duration
```

## Behavior

Words used below.
- **Label** of a module: `Module.Name`; for a factory that has not returned (or returned a module without a
  name), its function name with the import path cut to the last element (`sql.Register`).
- **Sync set:** the root and every module reachable from it through `Imports` entries only.
- **Entry:** one item of an `AsyncImports` list found in a module of the sync set. Its **unit** is the entry's
  module plus every module reachable from it (both lists) that is not in the sync set and not in the unit of
  an entry listed earlier. A unit has one state; `AsyncImports` inside a unit behaves as `Imports`.
- **Owner** of a provider type: the module that declares it (first declaration wins, as today).
- All panics are `color.FmtRed`, as today.

### Step 2: factory signatures and the startup timeout

| # | Rule |
|---|---|
| F1 | `Build()` checks every entry of both lists by `reflect.Type` (not by the printed type as `isDynamicModule` does): a `*core.Module`, or a non-variadic func whose results are `(*core.Module)` or `(*core.Module, error)`, with `context.Context` allowed only as the first parameter and only with the two-result form |
| F2 | Any other value (a nil entry, `func() string`, `func(context.Context) owlky.Module`, a variadic func, `context.Context` as second parameter, three results) panics at `Build()`, once for all bad entries, one per line joined by `"\n       "` as today: `invalid module: can't import '<type, or <nil>>' with <Imports\|AsyncImports>: an import is a owlky.Module or a module factory: func(deps...) owlky.Module, func(deps...) (owlky.Module, error) or func(ctx context.Context, deps...) (owlky.Module, error)`. It replaces `can't pass '%v' type as module` |
| F3 | `SetStartupTimeout(d)`: `d <= 0` panics `invalid app: SetStartupTimeout needs a positive duration`; after `Create` it panics `invalid app: SetStartupTimeout must be called before Create` (`mustBeBeforeCreate`). Default 30 s |
| F4 | The timeout is one budget for the whole `Create()`, counted from its start. A `ctx` factory of `Imports` receives a context whose deadline is that instant; it is canceled when `Create()` returns or panics |
| F5 | Every factory of the sync set is called on a helper goroutine; `Create()` waits for its return, its panic or the deadline, whatever the signature. Its parameters are resolved on `Create()`'s goroutine before the call |
| F6 | A factory returns a non-nil error → `Create()` panics `invalid module: module factory '<label>' failed: <err>` |
| F7 | A factory panics → the helper recovers, logs `Error` `ModuleFactoryPanic` (`module`, `panic`, `stack`) and `Create()` panics `invalid module: module factory '<label>' panicked: <value>` |
| F8 | The deadline passes first → `Create()` panics `invalid module: module factory '<label>' did not return within the startup timeout (<d>)` with `d` as set (`30s`). The helper goroutine is abandoned (the process is expected to exit) |
| F9 | A nil module with a nil error → `invalid module: module factory '<label>' returned a nil module` |
| F10 | A factory is still called once per app (same function value → same module, as `moduleByFunc` today). `OnInit` and `NewProvider` of sync modules are not covered by the timeout (Open question 3) |

### Step 3: sibling scope and order

| # | Rule |
|---|---|
| S1 | A factory listed by module `P` resolves each non-`ctx` parameter `T`, in this order: a provider visible in another entry of `P`'s `Imports` / `AsyncImports` (declared by that module or visible through its imports, the existing `visibleByModule`); a global provider; `log.Logger` / `owlky.Publisher`. `P`'s own providers are not in scope |
| S2 | Global resolution is unchanged: a global of a static module is found whatever its position or depth, and its owner's `OnInit` runs before the provider is built |
| S3 | Order does not matter. `Create()` scans the static tree first, then repeats: call, in discovery order (depth first, list order), the first factory not yet called whose parameters all resolve; scan the module it returns (its globals, its imports, its factories). It stops when no factory is callable |
| S4 | One factory left with an unresolved parameter panics with today's message `dependency injection: can't resolve argument of '<func type>'. Please make sure that the argument '<key>' at index [<i>] is available in the injected providers` |
| S5 | Two or more factories left panic `dependency injection: module factories can't be ordered: '<label>' needs '<T>' → '<label>' needs '<T>' → …. Nobody provides these types, or the factories need each other`, in discovery order. What a factory provides is unknown until it returns, so a cycle of factories and a missing provider cannot be told apart: this list is the chain |
| S6 | A sync-set factory whose missing type is owned by a module that is not ready at `Create()` (an entry's unit, or a module that needs one, G2) panics `invalid module: module factory '<label>' is in Imports and needs '<T>' of '<module>', which is not ready at Create: import it with AsyncImports` |

### Step 4: states and the gate

| # | Rule |
|---|---|
| G1 | Every module has a state: `pending`, `ready` or `failed`. It changes once (`pending` → `ready` or `failed`). Sync-set modules that need no entry are `ready` when `Create()` returns. A unit shares its entry's state |
| G2 | **Needs.** Module `M` needs entry `E` when a provider owned by a module of `E`'s unit, or by a module that needs `E`, is the type of a field of: one of `M`'s providers, one of `M`'s controllers, a layer (`BindMiddleware`, `BindGuard`, `BindInterceptor`, `BindExceptionFilter`) bound in `M`'s controllers, or a pipe in their handler signatures. Importing alone is not a need (Open question 1). A controller belongs to the first module that declares it |
| G3 | `M` is `ready` when every entry it needs is `ready`, `failed` as soon as one is `failed` (so `sql` failed → `user` → `booking` failed; a `booking` that injects only `redis` providers is untouched) |
| G4 | An app-wide layer (`BindGlobal…`, the `EnableWS` middlewares) that injects a provider of a module not ready at `Create()` panics `dependency injection: app-wide <kind> '<Type>' injects '<T>' of '<module>', which is not ready at Create: app-wide layers can't depend on AsyncImports` |
| G5 | Two entries whose statically known providers need each other panic at `Create()`: `invalid module: async modules need each other: '<a>' → '<b>' → '<a>'` |
| G6 | **Route probe.** For a controller of a module that is not ready at `Create()`, `Create()` builds a throwaway instance (providers that are ready injected, the others left zero), calls `NewController()` and reads its routes, events, `Prefix` / `Path` and bound layers. All route checks run then (`invalid route`, `route conflict`, `event conflict`, `invalid render`, `SUBMIT_` without `RENDER_`, template binding). A panic inside that `NewController()` stops `Create()` as `invalid module: NewController of '<Type>' failed before its module '<module>' is ready: <value>. NewController must not use injected providers` |
| G7 | Probed routes are registered in the router and probed events in the event matcher at `Create()`, behind the module's closed gate. The router, the event matcher and every map of `HTTP` / `WS` are never written after `Create()` |
| G8 | HTTP request to a gated route whose module is not `ready`: global middlewares run first (a middleware that refuses wins); then `503` with the body of `exception.ServiceUnavailableException("Service Unavailable")`. No module middleware, guard, interceptor, handler or user filter runs. R24 holds: status text only, never the cause |
| G9 | Format of G8: JSON; on a page route (`RENDER_`, `SUBMIT_`) HTML through the existing error page chain (`503.html`, `5xx.html`, built-in page), whatever `Accept` says, with `.Message` = `Service Unavailable` |
| G10 | `Retry-After` while the module is `pending`: the whole seconds until the entry's next `Retry` attempt (R6 stores it atomically), at least 1 and at most 60, from prebuilt strings; `5` when no `Retry` attempt is scheduled (a factory that retries by itself or not at all). No `Retry-After` when it is `failed` (amended 2026-11-18, owner: the value follows the retry instead of a fixed 5) |
| G11 | 404, 405 and `Allow` treat gated routes like any registered route |
| G12 | WebSocket: `subscribe` or `publish` on a topic whose matched event belongs to a module that is not `ready` replies one `error` with `exception.ServiceUnavailableException("not ready: " + topic)` (`code` 503) and stops handling that message like `TopicNotFound` does; no subscription is made, nothing is published, no guard or handler runs. The handshake is not gated |
| G13 | `ModuleBuilder.AsyncImports(...)` exists from this step: it records entries (F1, F2 apply). An entry in both lists of one builder panics at `Build()`: `invalid module: '<label>' is in both Imports and AsyncImports` (same `*Module`, or same function). An entry of one module's `AsyncImports` that another module lists in `Imports` panics at `Create()`: `invalid module: '<label>' is imported with Imports by '<x>' and with AsyncImports by '<y>'`. Until step 5, entries stay `pending` (only the unexported `settle`, below, moves them) |
| G14 | `OnInit`, providers, controllers and `OnReady` of a module that is not ready do not run in `Create()`. `App.Get(p)` returns `nil` for a provider that is not built yet |

### Step 5: the background

| # | Rule |
|---|---|
| A1 | `Create()` starts one goroutine per entry after all static wiring (routes, layers, devtool) and before `OnReady` of the ready modules. `Create()` never waits for them and never panics because of them |
| A2 | An entry's goroutine: wait for the entries it needs (a factory: for the owners of its parameters, S1; an async sibling counts once `ready`); call the factory, if any, with a `ctx` that is canceled at shutdown and has no deadline; discover the returned module's imports and call the factories of the unit (S3, same `ctx`); then for each module of the unit in discovery order: `OnInit`, `NewProvider()` of its providers; then its controllers (A5); the gate opens; state `ready`; `OnReady` of the unit's modules |
| A3 | When an entry becomes `ready`, every pending module whose needs are now all `ready` is built on the same goroutine, in discovery order: `OnInit`, providers, controllers, gate open, state `ready`; then `OnReady` for those modules in the same order. A route may be served before its module's `OnReady` returns |
| A4 | A route that opened behaves exactly as if its module had been imported with `Imports`: same middlewares, guards, interceptors, filters, in the same order (app-wide ones included). A module's HTTP routes and WS events open in one atomic store |
| A5 | Failure of an entry: a factory error, a panic anywhere in A2 (recovered; `stack` logged), a nil module, or a parameter that never resolves (A7). The entry and every module that needs it become `failed`; their gates never open; the app keeps running. The framework never calls a factory twice |
| A6 | A panic or an injection error while building a dependent in A3 fails that module and those that need it; modules already `ready` stay `ready` |
| A7 | **Unknown providers.** With at least one async factory in the app, a provider-typed field or factory parameter that no known module provides does not panic at `Create()`: its module (or entry) is `pending`. When a factory returns a module that provides it, that entry becomes a need (G2). When no async factory is running or still able to run and the type is still missing, the module is `failed` with cause `can't resolve <T>` (`T` as `reflect.Type.String()`). Without any async factory the app panics at `Create()` exactly as today |
| A8 | **Controllers discovered in the background are refused.** A module first seen after `Create()` (returned by a factory of a unit) that declares controllers fails its entry with cause `module '<label>' declares controllers (<Type>, …) but comes from an async factory: put them in a static module that imports it`. Controllers of a static async Module are known at `Create()` and gated (G6) |
| A9 | If the real `NewController()` yields other routes or events than the probe, the module fails with cause `routes of '<Type>' changed between Create and construction` |
| A10 | After a failure that follows a factory's return (A8, a provider panic), `OnShutdown` of that module is not called: only `ready` modules get it. A factory that fails cleans up before returning |
| A11 | **Shutdown** (`Stop`, a signal, a serve error; also `Stop` before serving). The async `ctx` is canceled first, before the drain. After the drain the app waits for the entry goroutines until `ShutdownTimeout` (30 s when never served) has passed since shutdown began; each one still running then is logged `Warn` `ModuleShutdownTimeout` and abandoned. From the cancel on, no module changes state and no gate opens; a factory that returns a module after the cancel has its result dropped |
| A12 | `OnShutdown` then runs only for `ready` modules, in the reverse of the order in which they became ready (`Create()` order first, then background order). `OnReady` in `Create()` runs only for modules ready then |

### Step 6: `Retry`

| # | Rule |
|---|---|
| R1 | `Retry` calls `fn(ctx)`; a nil error returns nil at once. Otherwise it waits and calls again. Attempt numbers start at 1 |
| R2 | Wait before attempt `n+1`: `d = min(MaxDelay, InitialDelay × 2^(n-1))`, then a uniform random time in `[d/2, d]` (`math/rand/v2`) |
| R3 | `ctx` done before or during a wait, or after a failed attempt: returns `fmt.Errorf("owlky: retry stopped after %d attempts: %w", n, errors.Join(ctx.Err(), lastErr))` (`errors.Is` finds both). `fn` is not called when `ctx` is already done (`n` = 0, no `lastErr`) |
| R4 | `MaxAttempts > 0` and attempt `MaxAttempts` fails: returns `fmt.Errorf("owlky: retry gave up after %d attempts: %w", n, lastErr)` without waiting |
| R5 | `opts == nil` means the defaults. `fn == nil` panics `invalid retry: Retry needs a function`. A panic in `fn` is not recovered |
| R6 | When `ctx` is the one the framework gave a factory (it carries the entry in an unexported context key), every failed attempt adds 1 to the entry's `attempts`, stores the error as its `last_error` and logs `Warn` `ModuleRetry` (`module`, `attempt`, `error`, `next`). With any other `ctx`, `Retry` only retries |

### Step 7: log lines and the devtool snapshot

| Event | Level | Fields | When |
|---|---|---|---|
| `ModulePending` | Info | `module`, `waiting_for` (labels, or `can't resolve <T>` items) | in `Create()`, once per module that is not ready |
| `ModuleReady` | Info | `module`, `after` (since `Create()` began) | a module becomes ready in the background |
| `ModuleFailed` | Error | `module`, `after`, `cause`; `stack` for a panic | A5, A6; for a dependent, `cause` is `needs '<label>'` |
| `ModuleRetry` | Warn | R6 | R6 |
| `ModuleFactoryPanic` | Error | F7 | F7 |
| `ModuleShutdownTimeout` | Warn | `module` | A11 |

`InstanceLoader … initialized` (at listen) is printed only for modules that are `ready` then.

`devtool.GetConfigurationResponse` gains `Modules []*ModuleStatus` (`json:"modules,omitempty"`), read live at
each `GetConfiguration` call through a function core gives the builder (`AddModules(func() []*ModuleStatus)`,
like `Render.Counters`), in discovery order:

```go
type ModuleStatus struct {
	Name       string   `json:"name,omitempty"`
	State      string   `json:"state,omitempty"`        // "pending", "ready", "failed"
	IsAsync    bool     `json:"is_async,omitempty"`     // an AsyncImports entry or a module of its unit
	WaitingFor []string `json:"waiting_for,omitempty"`  // while pending: as in ModulePending
	Attempts   int      `json:"attempts,omitempty"`     // R6
	LastError  string   `json:"last_error,omitempty"`   // R6, or the cause once failed
	ReadyAfterMs int64  `json:"ready_after_ms,omitempty"`
}
```

### Unexported seams the tests use (package `core`)

```go
type moduleStatus uint32 // modulePending, moduleReady, moduleFailed; String() "pending" | "ready" | "failed"
func (c *container) status(m *Module) moduleStatus
func (c *container) settled(m *Module) <-chan struct{}   // closed when m leaves pending
func (c *container) settle(entry *Module, err error)     // first call wins; the entry goroutine's last step
```

For a factory entry not yet returned, tests reach its state through `statusOfFactory(fn any)` /
`settledFactory(fn any)` (same semantics, keyed by the function).

## Performance

| Path | Cost |
|---|---|
| Ungated HTTP route | One load and compare of `httpRoute.gate` (nil) in `handleRequest`, right after `match`; the field sits next to `middlewares`, which the next statement reads. No atomic, no call, no allocation. Every `bench/` `BenchmarkOwlky/*` scenario must stay inside the noise band of `docs/review/http-0008.md` (Static 1.26 µs, Param 2.91 µs, NotFound 1.09 µs…), bytes and allocations equal |
| Gated route, open | That compare, one `atomic.Pointer` load of the module's live table, one index. Expected under 2 ns, 0 allocations (`BenchmarkHTTP_GatedRouteOpen` against the same route ungated) |
| Gated route, closed (client-triggered, so warm) | Global middlewares + a prebuilt JSON body (as `notFoundBody`), one more atomic load for G10; 0 allocations beyond the `Retry-After` header slice, which is a package-level value. Pages cost one error page render, as `loadshed`'s 503 |
| Ungated WS event | One nil compare on the matched `wsevent.WSEventItem` |
| `Create()`, background wiring, `Retry` | Cold. Needs are computed once at `Create()` (and when a factory returns) |
| Idle | No goroutine once every entry has settled; one goroutine per entry until then. Per gated module one gate (an atomic pointer and its table), per gated route one placeholder `httpRoute` |

No heavy technique (`unsafe`, `sync.Pool`, manual buffers, inlining restructure) is used.

Shape of the gate (the builder may name things differently, the load count is the contract):

```go
type moduleGate struct{ live atomic.Pointer[moduleLive] }      // nil until the module is ready
type moduleLive struct{ http []*httpRoute; ws []wsLive }        // built whole, then stored once
// placeholder: httpRoute{gate, gateIndex, isPage, template, pageRoute}; wsevent.WSEventItem gains a gate
// handle that core fills, so the plan and the catch chain of a gated event come from moduleLive, not from
// the maps wsPlanByEvent / catchFnsByEvent
```

## Concurrency

Today the container is filled in `Create()` and read-only afterwards. That stays true for everything a
request reads without the gate.

| # | Invariant |
|---|---|
| C1 | The request path (HTTP and WS) reads only data that is immutable after `Create()` plus `moduleGate.live`. A `moduleLive` and everything it points to (routes, plans, layers, controllers, providers) is fully built before the single `Store` and never changed afterwards; the atomic store is the happens-before edge |
| C2 | `container.wireMu sync.Mutex`: at most one goroutine wires at a time (A2 after the factory returns, A3). `OnInit`, `NewProvider`, `NewController`, layer constructors and `OnReady` run under it, as serially as in `Create()`. **Factories run outside it**, so slow factories overlap. A slow `NewProvider` or `OnInit` of one unit delays the wiring of the others (not requests): docs tell authors to connect in a factory |
| C3 | `container.mu sync.RWMutex` guards the container's maps and the module table after the goroutines start. It is never held while user code runs. Lock order `wireMu` → `mu`. `App.Get`, the devtool function and shutdown take `mu` (read) |
| C4 | Module status, `attempts` and the ready order are atomics or under `mu`; `settle` is first-call-wins |
| C5 | Goroutines: one per entry, started in A1, ended by return; each selects on the async `ctx` wherever it waits (for needs, for `wireMu` hand-over between modules). `Create()`'s helper goroutines (F5) end with their factory. `Retry` starts none (a `time.Timer`, stopped on return) |
| C6 | Shutdown sets a `stopping` flag under `mu` before waiting (A11); wiring checks it before each module and before each `Store` |
| C7 | A user's `OnReady` or factory calling `app.Stop()` from an entry goroutine must not deadlock: `Stop` does not wait for the goroutine it is called from (same rule as `hooksGoroutine` today) |

## Security

- The 503 of a gated route and the WS reply carry no cause, module name or dependency name (R24). Causes go
  to the log and the devtool snapshot only; `last_error` may hold a driver's error text (a DSN host), so the
  devtool keeps its existing "development only" status and the docs repeat it.
- Global middlewares run before the 503: `helmet`, `cors`, `hostcheck`, `loadshed` and the access log apply
  to it. Guards do not run, so a 503 tells an unauthenticated client only that the route exists, as 405 does.
- A closed gate is cheap to hit (no handler work); `loadshed` and the throttler's app-wide use still apply.
- `Retry` has bounded waits (`MaxDelay`), jitter against synchronized reconnects, and at most one log line
  per attempt.
- No user input reaches a panic message or a state.

## Impact

- Callers: none must change. `Create()` now runs sync factories on a helper goroutine (F5): a factory that
  relies on being on `main`'s goroutine (it should not) changes. The F2 text replaces
  `can't pass '%v' type as module`; tests pinning it are updated in step 2.
- `modules/config`, `modules/cache`: their `Register` returns a module, not a factory: no code change. Their
  READMEs say how they behave under `AsyncImports` (step 8).
- New files (suggested): `module_factory.go` (F, S), `module_state.go` (G, the gate), `module_async.go` (A),
  `retry.go` (R), each with its `_test.go`; the 503 writer in `http_error.go`; the WS reply in `read_loop.go`.
- `internal/wsevent`: `WSEventItem` gains the gate handle; `Match` is unchanged.
- Docs (step 8): README lifecycle section, `.owlky/modules-and-providers.md`,
  `.owlky/startup-lifecycle-analysis.md`, `.owlky/metadata-lifecycles.json`,
  `.owlky/metadata-concurrency-model.json`, CLAUDE.md (panic list: the new `invalid module:`,
  `dependency injection:` and `invalid retry:` messages; `SetStartupTimeout`; `AsyncImports`),
  `internal/core/README.md`, `internal/devtool/README.md`.

## Test plan

Internal tests (`package core`), one function per behavior, `-race` on every async test. Slow dependencies
are factories blocked on a channel the test closes; no test sleeps to wait for a state (`settled`).

| Step | Rules | Tests (file) |
|---|---|---|
| 2 | F1, F2 | `TestModuleBuilder_Imports_AcceptsThreeFactoryShapes`; `…_RefusesNil`, `…_RefusesWrongResult`, `…_RefusesCtxWithoutError`, `…_RefusesCtxNotFirst`, `…_RefusesVariadic`, `…_ListsEveryBadEntry` (exact text) (`module_builder_test.go`) |
| 2 | F3, F4 | `TestApp_SetStartupTimeout_RefusesZeroAndNegative`, `…_RefusesAfterCreate`; `TestModuleFactory_CtxCarriesStartupDeadline`, `…_CtxCanceledAfterCreate` |
| 2 | F6-F10 | `TestModuleFactory_ErrorStopsCreate` (label and error in the panic), `…_PanicStopsCreate` (+ `ModuleFactoryPanic` logged), `…_NeverReturnsStopsAtDeadline` (10 ms timeout, each of the three shapes), `…_NilModuleStopsCreate`, `…_CalledOnce` |
| 3 | S1-S3 | `TestModuleFactory_ResolvesSiblingProvider`, `…_ResolvesSiblingReExport`, `…_SiblingListedAfter`, `…_FactoryAfterFactory` (both orders), `…_GlobalFromReturnedModuleAnyOrder`, `…_GlobalAtAnyDepth` (unchanged), `…_OwnProvidersNotInScope`, `…_OwnerOnInitRunsFirst` |
| 3 | S4-S6 | `…_OneUnresolvedKeepsMessage`, `…_TwoUnresolvedListsChain` (exact text, discovery order), `…_SyncNeedsAsyncProviderPanics` |
| 4 | G1-G5 | `TestModuleState_ReadyWithoutAsync`, `…_DependentPendingThroughProvider`, `…_ThroughControllerField`, `…_ThroughBoundGuard`, `…_ThroughPipe`, `…_ImportAloneIsNotANeed`, `…_FailurePropagates` (`settle(sql, err)` → `user`, `booking` failed, `booking` on `redis` only not), `…_OneWay` (second `settle` ignored), `…_AppWideGuardOnAsyncPanics`, `…_AsyncCyclePanics` |
| 4 | G6, G7, G13, G14 | `TestRouteProbe_ChecksRoutesAtCreate` (conflict between a gated and an open route panics), `…_NewControllerPanicNamesController`, `…_NoHookRunsWhilePending`, `TestApp_Get_NilWhilePending`; `TestModuleBuilder_AsyncImports_BothListsPanics`, `…_BothListsAcrossModulesPanics` |
| 4 | G8-G11 | `TestGate_Pending503JSON` (exact body, `Retry-After: 5`), `…_Failed503NoRetryAfter`, `…_Page503HTML` (`503.html`, then `5xx.html`, then built-in; `Accept: application/json` still HTML), `…_NoCauseInBody`, `…_GlobalMiddlewareRunsAndCanRefuse`, `…_NoGuardOrFilterRuns`, `…_OtherModulesServe`, `…_405ListsGatedMethod` (`http_error_test.go`) |
| 4 | G12 | `TestWSGate_SubscribeNotReady`, `…_PublishNotReady` (reply `code` 503, no subscription, handler not called), `…_OtherEventsWork` (`read_loop_test.go`) |
| 4 | Perf | `BenchmarkHTTP_GatedRouteOpen`, `BenchmarkHTTP_GatedRouteClosed` (0 allocs on JSON); `bench/` rerun against `http-0008.md` |
| 5 | A1-A4 | `TestAsync_CreateReturnsWhileFactoryBlocks`, `…_ReadySwitchesRoutesOn` (the "10 s later" case: 503, release, `settled`, 200), `…_OrderOnInitProvidersControllersOnReady` (recorded), `…_DependentsInDiscoveryOrder`, `…_OpenRouteHasSameLayersAsSync` (layer order recorded in both apps), `…_StaticAsyncModuleWithControllers`, `…_FactoryWaitsForAsyncSibling`, `…_WSEventSwitchesOn` |
| 5 | A5-A10 | `…_FactoryErrorFails` (`ModuleFailed` with the cause, routes stay 503), `…_FactoryPanicFails`, `…_ProviderPanicFails`, `…_SQLFailsUserAndBooking503RedisBookingWorks` (the backlog scenario, end to end over `ServeHTTP`), `…_DependentPanicFailsOnlyIt`, `…_UnknownProviderPendingThenFailed` (`can't resolve <T>` after the last factory returns), `…_UnknownProviderArrivesFromFactory`, `…_UnknownProviderWithoutAsyncFactoryStillPanics`, `…_FactoryModuleWithControllersRefused`, `…_RoutesChangedFails`, `…_FactoryCalledOnce` |
| 5 | A11, A12, C | `…_ShutdownWhilePendingCancelsCtx` (factory sees `ctx.Done()`, `Stop` returns, no `OnShutdown` for it), `…_ShutdownTimeoutAbandons` (factory ignores `ctx`; short `ShutdownTimeout`; `ModuleShutdownTimeout`), `…_LateResultDropped`, `…_OnShutdownOnlyReadyReverseOrder`, `…_StopFromOnReadyDoesNotDeadlock`, `TestAsync_ConcurrentRequestsDuringSwitch_NoRace` (requests on gated and open routes from many goroutines while three entries settle; every answer is 200 or 503, never a mix of layers), `TestAsync_ConcurrentGetAndDevtool_NoRace` |
| 6 | R1-R6 | `TestRetry_FirstSuccess`, `…_SucceedsAfterTransientErrors`, `…_DelayBounds` (each wait within `[d/2, d]`, capped by `MaxDelay`; measured with small delays), `…_StopsOnCancel` (`errors.Is` for `context.Canceled` and the last error), `…_CtxAlreadyDone`, `…_MaxAttempts`, `…_NilOptions`, `…_NilFnPanics`, `…_ReportsAttemptsToEntry` (factory in `AsyncImports`: `attempts`, `ModuleRetry`, then `ready`), `…_ForeignCtxReportsNothing`, `…_StopsAtShutdown` (`retry_test.go`); root `surface_test.go`: `owlky.Retry`, `owlky.RetryOptions` |
| 7 | V | `TestModuleLog_PendingReadyFailed` (fields), `TestServer_InstanceLoaderOnlyReady`; `TestDevtool_ModulesLive` (pending → ready changes between two calls; `waiting_for`, `attempts`, `last_error`) (`devtool_builder_test.go`, core side next to `initDevtool`) |

## Risks

- **`NewController()` runs twice for gated controllers** (G6). One with side effects or that reads an
  injected provider breaks; G6 turns the second case into a clear `Create()` panic, the first is documented.
- **Later errors.** Injection and handler-plan mistakes of a gated module show as `ModuleFailed` in the
  log, not as a `Create()` panic. The probe catches every route-shape mistake; the rest needs the log.
- **A5/A10 leak:** a failure after a factory returned leaves what the factory opened.
- **Serialized wiring (C2)** trades latency of unrelated units for a container that stays simple and
  race-free. If measured as a problem, per-unit wiring is a later design.
- **Factory identity is the code pointer** (`moduleByFunc`, today): two closures of one function literal
  count as one factory, in G13 too. Unchanged, now documented.
- **Abandoned goroutines** (F8, A11) hold their resources until the process exits.
- S5 cannot prove a cycle (see the rule); the message says so.

## Open questions

1. **Is importing alone a need (G2)?** The backlog says readiness propagates "through imports and injected
   providers". Counted literally, the root (which imports everything) and every grouping module would be
   gated by every async module: a `/` or `/health` controller in the root would answer 503 until the
   database is up, and forever if it fails. **Recommendation:** as written in G2, injection only; imports
   matter through what they make injectable. The backlog's own example (`booking` on `redis` only works)
   needs this whenever `booking` and `user` share an ancestor.
2. **`Retry-After` (G10).** **Recommendation:** `5` while pending, none when failed, not configurable.
   Alternative: never send it.
3. **Should the startup timeout also cover `OnInit` / `NewProvider` of sync modules (F10)?** They take no
   `ctx`; covering them means running every hook on a helper goroutine and losing the panic's own stack.
   **Recommendation:** no; factories only, and the docs say "do slow work in a factory". Revisit with the
   storage roadmap.
4. **WS "not ready" code (G12).** **Recommendation:** reuse `ServiceUnavailableException` (503), no new
   exported exception. Alternative: a new close-range code next to `4001` / `4004`.
5. **Units (definition of "unit").** `AsyncImports(storage.Module)` puts the modules that only
   `storage.Module` imports, and their factories, in the background too. **Recommendation:** yes; otherwise
   a nested factory would still block `Create()` although the app asked for async.
