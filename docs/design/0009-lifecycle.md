# 0009: Lifecycle and DI wiring

Status: Implemented
Packages: `core` (`app.go`, `module.go`, `module_builder.go`, `fn.go`, new `container.go`), `common`
(route and event registries, `Construct`, the `SERVE` route DSL), `devtool` (the snapshot bug), `log`
(`NewLog`, `WrapLogger`)

Roadmap: `docs/roadmap/0003-server-and-production.md`, step 3. Depends on design 0007. The logger
API of the roadmap's Decisions (`SetLogger`, the `Logger()` getter, `UseLogOptions` and
`App.LogOptions` removed) is taken as given. All of this is cold path.

## Goal
An `App` owns everything it wires. Two apps in one process (a test suite, an admin app next to the
public one) are independent: their own routes, providers, layers, logger and publisher. `Create`
fails loudly on misuse instead of with a nil pointer, hooks run in dependency order and shut down in
reverse, `EnableDevtool` works without versioning, and `SERVE_static_ANY` serves `/static/...`.

## Bugs found
Bugs 1, 2 and 10-15 were reproduced with throwaway probes against `master` (4d4576c); nothing was
left in the tree. Bugs 3-9 are read from the code: bug 1 hides most of them today (a second app has
no controllers), and they appear as soon as it is fixed.

| # | Where | Input | Today | Rule |
|---|---|---|---|---|
| 1 | `core/module.go:18-19`, `:155-157`, `:144-146` | A second `core.New()` + `Create(otherRoot)` in the process | `bootstrapMainModule` returns at once because `mainModulePtr` is already set, so the second root never enters `modulesInjectedFromMain` and `injectControllers` is skipped: the app has no route and answers 404 to everything | I1 |
| 2 | `core/module.go:25`, `:342-346` | The same provider type in two apps | `providerSingletonByKey` is per process: `NewProvider` runs once and both apps share the instance | I2 |
| 3 | `core/module.go:24`, `core/app.go:103-105`, `:245`, `:597` | Two apps | `globalInterfaceByKey` is per process: the last `New()` decides which broker every app's `common.Publisher` uses, the last `Create` / `UseLogger` which logger is injected | I4 |
| 4 | `core/module.go:23`, `:77` | Two apps | Global providers of one app resolve dependencies in the other | I5 |
| 5 | `core/module.go:20`, `:281-286` | A module func (`var Module = func() *core.Module {…}`) imported by two apps | Built once per process: the second app gets the first one's wired module | I3 |
| 6 | `core/module.go:21`, `:301` | The same controller type in two apps whose modules have a prefix | `globalPrefixesByController` keeps growing: the second app's routes get the prefix twice | I1 |
| 7 | `common/http.go:23`, `:146-155`; `common/ws.go:13`, `:43-53` | Two apps with the same route or event | `InsertedRoutes` / `InsertedEvents` are per process: the second `Create` panics with `route conflict` (once bug 1 is fixed) | I6 |
| 8 | `common/fn.go:34-37`, `:210-231` | The same guard, interceptor, filter or middleware configuration in two apps | `Construct` keeps one instance per process (by type and `reflect.DeepEqual`): both apps share the layer and the state its constructor made | I7 |
| 9 | `log/log.go:58-59`, `:82` | `log.NewLog(optsB)` after any earlier `log.NewLog` | `once.Do`: the first call's options win for the whole process, so `SetLogger(log.NewLog(&log.LogOptions{…}))` would silently configure nothing | G5 |
| 10 | `core/module.go:77`, `core/fn.go:65-66`, `core/module.go:467-468` | A provider of module X depends on a provider of an `IsGlobal` module that X does not import | The value injected is the one passed to `Providers(…)`, before `NewProvider` ran (probe: the field set by `NewProvider` is empty in the dependent, set in the controller) | P3 |
| 11 | `core/module.go:164-183` | An `IsGlobal` module imported by a module other than the root | Never registered as global: `Create` panics `can't resolve dependency` | P4 |
| 12 | `devtool/devtool_builder.go:217-221`, `core/app.go:729` | `EnableDevtool()` without `EnableVersioning()`, at least one route | `Create` panics: nil pointer dereference (`devtoolBuilder.versioning.Key`) | D1 |
| 13 | `common/fn.go:83-92`, `core/http.go:53-57` | `func (c C) SERVE_static_ANY() string { return "public" }` | The route is the one-segment glob `/static*/`: `/static/a.txt` and `/static/sub/x` answer 404; only `/static<x>` matches, and it looks for `public/static<x>` | V1 |
| 14 | `core/app.go:744-752` | Modules root → A → B, shutdown | `OnShutdown` runs root, A, B, the same order as `OnInit` and `OnReady`. Nothing guarantees that a module stops before the modules it imports; the reverse of initialization does | K4 |
| 15 | `core/app.go:179-181`, `:249` | `Create(nil)`; `Create` twice; `BindGlobalGuards` after `Create` | nil pointer panic; a silent no-op that leaves the first wiring; a silent no-op | M1-M3 |

Observed, not changed: a module's `Name` comes from parsing the caller's Go source at `Build()`
(`core/module_builder.go:111-158`), so it is empty in a deployed binary (no source files, or
`-trimpath`); it is only used in startup log lines. Module funcs are identified by their code
pointer (`reflect.Value.Pointer`), so two closures made from one function literal count as one
module. Providers are injected by value: each dependent holds a copy of the constructed provider, so
state that must be shared sits behind a pointer.

## Public API
```go
func (app *App) SetLogger(logger common.Logger) *App // new; replaces UseLogger
func (app *App) Logger() common.Logger               // new; replaces the field App.Logger
```

Exported additions and why users need each:

| Addition | Why it is exported |
|---|---|
| `(*App).SetLogger` | the one way to give the app a logger (roadmap Decision) |
| `(*App).Logger` | `main` logs with the app's logger (`app.Logger().Error(…)`) |

Removed (**breaking**):

| Identifier | Replacement / reason |
|---|---|
| `(*App).UseLogger` | `SetLogger` (`Use` means middleware) |
| `(*App).UseLogOptions`, field `App.LogOptions` | `app.SetLogger(log.NewLog(&log.LogOptions{…}))`; masking for a custom logger: `app.SetLogger(log.WrapLogger(myLogger, fields))` |
| field `App.Logger` | `app.Logger()` |
| `Module` fields `HTTPExceptionFilters`, `HTTPMiddlewares`, `HTTPGuards`, `HTTPInterceptors`, `HTTPMainHandlers`, `WSGuards`, `WSInterceptors`, `WSExceptionFilters`, `WSMainHandlers` | wiring results; they move into the app (a module is no longer written by `Create`) |
| `Module`'s embedded `*sync.Mutex` (`Lock`, `Unlock`), `(*Module).NewModule` | wiring internals |
| `common.InsertedRoutes`, `common.InsertedEvents`, `(*common.HTTP).GetConfigurations`, `common.HTTPConfig` | process-wide registries; conflicts are checked per app by `core` |
| `common.Construct` | the layer instances are kept per app by `core` |

Changed signatures (**breaking**, framework plumbing between `core` and `common`): the six
`InjectProvidersInto{HTTP,WS}{Guards,Interceptors,ExceptionFilters}` methods and
`InjectProvidersIntoHTTPMiddlewares` take one more argument, `construct func(obj any, constructor
string) any`, and call it where they called `Construct`. `(*common.HTTP).AddHandlerToRouterMap` and
`(*common.WS).AddHandlerToEventMap` keep their signatures and no longer panic on a conflict.

Kept as they are: `core.New`, `Create`, `Get`, `ModuleBuilder` and its methods, `Module.Name`,
`IsGlobal`, `OnInit`, `OnReady`, `OnShutdown`, `(*Module).Prefix`, `(*Module).ID`, every
`BindGlobal…` and `Enable…` method, `log.LogOptions`, `log.NewLog` and `log.WrapLogger` (signatures).

Behavior a user can see (**breaking** where marked):
- **Breaking:** `OnInit` and `OnReady` run for a module's imports before the module itself (K1); today
  the root runs first.
- **Breaking:** `log.NewLog` returns a new logger on every call (G5).
- **Breaking:** `SERVE_<name>_ANY` is the route `/<name>/*/` (V1).
- **Breaking:** configuration after `Create`, a second `Create` and `Create(nil)` panic (M1-M3).
- New bootstrap panic category: `"invalid app: …"` (added to the list in `CLAUDE.md`).

Unexported contract (tests are internal to `core`):

```go
// core/container.go: everything one App wires. Created by New, filled by Create on one goroutine,
// read-only afterwards.
type container struct{ /* module funcs already called, provider instances, global providers,
    injectable interfaces (Logger, Publisher), controller prefixes, layer instances,
    route and event names for conflict checks, the modules in initialization order */ }
func (app *App) Create(m *Module)
```

Removed (unexported): the package variables `mainModulePtr`, `modulesInjectedFromMain`,
`staticModuleByDynamicPtr`, `globalPrefixesByController`, `globalProviderByKey`,
`globalInterfaceByKey`, `providerSingletonByKey`, `moduleGlobalMu` (`core/module.go:17-25`),
`common`'s `singletons` and `singletonsMu`, `log`'s `singleInstance` and `once`, and the test helper
`resetModuleGlobals`. Kept: `moduleNameFileCache` (a cache of immutable parse results) and
`fieldNameByRole` (a constant table).

## Behavior

### One app owns its wiring
- **I1** Two apps created in one process from two root modules each answer their own routes (bug 1).
  A module `Prefix` appears once in each app's routes, however many apps were created before.
- **I2** Each app constructs its own providers: for a provider type present in two apps,
  `NewProvider` runs once per app, and state behind a pointer made in `NewProvider` is not shared.
- **I3** `Create` does not write to a `*Module`: the same module value (a root, or a package-level
  `var X = some.Register(…)`) can be given to any number of apps, one after another or at the same
  time. A module func is called once per app; `NewController` runs once per controller type per app.
- **I4** A `common.Logger` field is injected with the logger of the app that wires it; a
  `common.Publisher` (field or handler argument) publishes to that app's broker.
- **I5** A global provider of one app is not visible in another: a dependency that only another app
  could satisfy panics `dependency injection: can't resolve dependency …` as it does alone.
- **I6** Two apps may declare the same route and the same WebSocket event. Inside one app the
  conflict panics stay, with today's texts: `route conflict: <fn> method is conflicted with <fn>
  method` (same method, version and route shape; parameter names do not count) and `event conflict:
  <fn> event is conflicted with <fn> event`.
- **I7** A guard, interceptor, exception filter or middleware is constructed per app
  (`NewGuard`, `NewInterceptor`, `NewExceptionFilter`, `NewMiddleware` run once per app for one type
  and configuration). Inside one app today's rule stays: equal configurations of one type share one
  instance; different configurations get their own.
- **I8** Two apps can be created on two goroutines at once, from the same or different modules,
  without a data race.
- **I9** `app.Get(p)` returns the instance this app constructed for `p`'s type, `nil` for a type it
  does not have, and `nil` before `Create`.

### Providers
- **P1** Inside one app each provider type is constructed once, whatever the number of modules that
  list it.
- **P2** A provider is constructed after the providers its exported fields name, in the same module
  or in imported ones; what is injected is always the result of `NewProvider`, never the value given
  to `Providers(…)`.
- **P3** P2 holds for providers of `IsGlobal` modules injected into modules that do not import them
  (bug 10).
- **P4** The providers of an `IsGlobal` module are visible to every module of the app, at any import
  depth (bug 11). The root module's providers are global, as today.
- **P5** Two providers whose fields name each other, directly or through others, panic at `Create`:
  `dependency injection: circular dependency: <A> -> <B> -> <A>` (type names as `reflect.Type.String`
  prints them). Today one of them silently receives an unconstructed value.
- **P6** Unchanged: an unresolved dependency and an unexported field panic with today's
  `dependency injection: …` texts; controllers, global layers and module funcs' arguments resolve as
  today (module providers, then global providers, then `common.Logger` / `common.Publisher`).

### Hooks
- **K1** Modules are initialized depth first: a module after every module it imports, imports in the
  order of `Imports(…)`, each module once per app even when imported several times. Initializing a
  module means: call its `OnInit` if set, then construct its providers. Example: root imports A and
  C, both import B → `OnInit` order B, A, C, root.
- **K2** `OnReady` runs once per app at the end of `Create`, after routes, layers and the devtool
  snapshot exist, in the order of K1.
- **K3** A panic in `OnInit` or `OnReady` stops `Create` and reaches the caller unchanged.
- **K4** `OnShutdown` runs once per app, in the exact reverse of K1 (root, C, A, B in the example),
  when design 0008's shutdown reaches it (G3, G6, G7 there). An app whose `Create` did not finish
  runs no `OnShutdown`.
- **K5** A module value shared by two apps has its hooks called once per app.

### Misuse
- **M1** `Create(nil)` panics `invalid module: Create needs a root module`.
- **M2** A second `Create` on one app panics `invalid app: Create called twice`.
- **M3** `BindGlobalMiddlewares`, `BindGlobalGuards`, `BindGlobalInterceptors`,
  `BindGlobalExceptionFilters`, `EnableVersioning`, `EnableWS`, `EnableDevtool`, `EnableAccessLog`
  and `SetLogger` called after `Create` panic `invalid app: <Method> must be called before Create`.
- **M4** All three are bootstrap panics in `color.FmtRed`, like the others. Changing a module
  (`Prefix`, `IsGlobal`, a hook) after it was given to `Create` is not supported; the README says so.

### Logger
- **G1** `app.Logger()` never returns `nil`. Without `SetLogger` it is the built-in logger with
  default options, the same value on every call.
- **G2** After `app.SetLogger(l)`, everything logged through `app.Logger()` and through injected
  `common.Logger` fields reaches `l`. `SetLogger` returns the app.
- **G3** A custom logger still gets the `log` struct tags and masking machinery: the app uses
  `log.WrapLogger(l, nil)`. A logger that already came from `log.NewLog` or `log.WrapLogger` is used
  as it is, not wrapped a second time; `log.WrapLogger(l, nil)` returns such an `l` unchanged.
- **G4** `SetLogger(nil)` panics `invalid app: SetLogger needs a logger`.
- **G5** `log.NewLog(opts)` returns a new logger built from `opts` on every call (bug 9); two calls
  with different options give two loggers that behave differently. `nil` means the defaults, as
  today.
- **G6** Migration: `UseLogger(l)` → `SetLogger(l)`; `UseLogOptions(o)` → `SetLogger(log.NewLog(o))`;
  `UseLogger(l)` + `UseLogOptions(&log.LogOptions{MaskFields: f})` → `SetLogger(log.WrapLogger(l,
  f))`; `app.Logger.Info(…)` → `app.Logger().Info(…)`.

### Devtool
- **D1** `EnableDevtool()` without `EnableVersioning()`: `Create` does not panic. Each HTTP entry of
  the snapshot has `Versioning.Value` = the route's version, `Key` = `""` and `Type` = 0. With
  versioning the snapshot is what it is today.

### `SERVE` routes
- **V1** In a method whose operation is `SERVE`, an `ANY` that ends a resource group and follows at
  least one name token is a segment of its own: `SERVE_static_ANY` → `SERVE /static/*/`;
  `SERVE_a_b_ANY` → `/a_b/*/`; `SERVE_assets_ANY_OF_public` → `/public/assets/*/`.
- **V2** Unchanged: `SERVE_ANY` → `/*/`; `SERVE_ANY_OF_static` → `/static/*/`; an `ANY` between two
  name tokens is a glob (`SERVE_me_ANY_bers` → `/me*bers/`); every other operation (`READ_assets_ANY`
  → `/assets*/`).
- **V3** With `SERVE_static_ANY` returning `"public"`: `GET /static/a.txt` serves `public/a.txt`,
  `GET /static/sub/x.txt` serves `public/sub/x.txt`, `GET /static` is the directory itself (design
  0007 S1), `GET /staticzzz` answers 404.
- **V4** `SERVE_static_ANY` and `SERVE_ANY_OF_static` in one app are the same route: I6's panic.

### Kept (characterization tests first)
- **Z1** For one app, the routes (method, path, version), their module and controller prefixes, the
  layers bound to each route and their order, and the order in which providers are constructed are
  what they are today, except where K1 changes the hook order. The tester pins these with tests that
  pass on today's code before the builder moves the wiring.

## Performance
Cold path: `Create` runs once. Written for readability and ownership; type A only (maps sized from
the module's counts, no regexp per call: `pkgFromControllerKeyReg` in `core/fn.go:20` goes away with
its only use if the move makes it dead). No heavy technique, nothing to benchmark per request.

The request path is not touched: handlers, plans and routes are built as in designs 0006 and 0007.
`app.Logger()` is a field read. The existing `Create` benchmarks (`core/module_bench_test.go`,
`module_builder_bench_test.go`) are kept and rerun: `Create` of the benchmark tree must not get
slower by more than its noise, and they no longer need to reset package state between iterations.

## Concurrency
- `Create` wires on the calling goroutine. Everything it builds lives in the app's `container`, so
  no lock is needed for one app, and two apps never touch the same state (I8). The process-wide
  mutex `moduleGlobalMu` goes away with what it guarded.
- A `*Module` is read-only for `Create` (I3); the user must not change it while an app is being
  created from it.
- A module func may be called by two apps at once: it must not write shared state without its own
  protection (README).
- After `Create` the container is never written; `Get` and the request path read it freely.
- `OnReady` and `OnShutdown` each run once per app (`sync.Once`), hooks one at a time, in order.
- Tests: `TestCreate_TwoAppsConcurrently_NoRace` (the same root and shared imports, 8 goroutines,
  `-race`), `TestGet_ConcurrentAfterCreate_NoRace`.

## Security
- Wiring uses reflection on the developer's types at startup only; no client input reaches it.
- Isolation: before this change one app could publish into another app's broker, log through
  another app's logger (and miss its own masking rules), and resolve another app's providers (bugs
  2-4). Each of those is closed by I2, I4, I5.
- Masking and `log` tags keep applying to custom loggers (G3); removing `UseLogOptions` does not
  remove masking (G6).
- Static files: V1 widens what `SERVE_x_ANY` serves, from nothing useful to the whole returned
  directory tree. Design 0007 S1-S2 bound it (no listing, no traversal). The README says to return a
  directory that holds only public files.
- The devtool snapshot is built in memory only; D1 changes no exposure.

## Impact
- `core/module.go` keeps the `Module` type, `Prefix`, `ID`; the wiring moves to methods of
  `container` in `core/container.go` (a file for one unexported topic, per the file rule), with its
  tests in `container_test.go`. `core/app.go`: `New`, `Create` and the `init…` functions read the
  container instead of `app.module`'s fields; the init-order comment at `app.go:163-178` is updated
  with the new order. `core/fn.go`: `injectDependencies` and `buildFieldInjectionCallback` resolve
  from the container. `core/ws.go`: `resolveGlobalMiddlewares` calls the container's construct
  (forced by I7; no other WS change).
- `common`: `http.go` (registry and `GetConfigurations` removed), `ws.go` (registry removed),
  `fn.go` (`Construct` removed; `ParseFuncNameToURL` for V1), the seven `InjectProvidersInto…` methods.
- `devtool/devtool_builder.go`: D1. `log/log.go`, `log/log_mask.go`: G3, G5 (design 0010 reviews the
  rest of `log`).
- Callers of the logger API: `sample/main.go`, `bench/owlky.go`, the root `README.md`, package
  READMEs that show `UseLogger`. Built-in modules need no change (they set `IsGlobal` and `OnInit`
  on the module they build).
- Tests: the 62 calls of `resetModuleGlobals()` disappear; tests that read `Module`'s removed fields
  or `common.InsertedRoutes` are rewritten against the app (`ServeHTTP`, `Get`) or the container.
- Docs: `core/README.md` (Create, hooks and their order, two apps in one process, `SetLogger` /
  `Logger`, the misuse panics), `common/README.md` (`SERVE_x_ANY`, removed registries and
  `Construct`), `log` README (written in step 4), `CLAUDE.md` (the `invalid app:` category, `SERVE`),
  `.owlky/modules-and-providers.md`, `.owlky/startup-lifecycle-analysis.md`,
  `.owlky/routing-system.md` in step 5.
- README content for "Two apps in one process": each `core.New()` is independent; share a module
  value freely; a resource created outside the framework (a cache backend built in a package-level
  `Register` call) is shared because the user created it once.

## Test plan
Internal tests; one function per behavior. No test resets package state: each builds its own apps.

| Rule | Test | Case |
|---|---|---|
| Z1 | `TestCreate_Characterization_Routes`, `…_NestedPrefixes`, `…_LayerOrderPerRoute`, `…_ProviderConstructionOrder` | written first, green on today's code (with today's reset helper), still green after |
| I1 | `TestTwoApps_EachAnswersItsRoutes`, `…_PrefixNotDoubled` | different roots; the second app's route answers 200, the first app's route answers 404 on the second |
| I2 | `TestTwoApps_ProvidersConstructedPerApp`, `…_ProviderStateNotShared` | a counter in `NewProvider`; a pointer field incremented through a route |
| I3 | `TestTwoApps_SameRootModule`, `…_SharedImportedModuleValue`, `…_ModuleFuncCalledOncePerApp`, `TestCreate_DoesNotWriteModule` | deep copy of the module before `Create` equals it after |
| I4 | `TestTwoApps_LoggerInjectedPerApp`, `…_PublisherPerApp` | two recording loggers; a publish on one app is not seen by the other's subscriber |
| I5-I7 | `TestTwoApps_GlobalProviderNotVisibleAcross`, `…_SameRouteNoConflict`, `…_SameEventNoConflict`, `TestCreate_RouteConflictInsideOneApp`, `…_EventConflictInsideOneApp`, `TestTwoApps_LayersConstructedPerApp`, `TestCreate_EqualLayerConfigurationsShareInstance` | the panic texts; a counter in `NewGuard` |
| I8-I9 | `TestCreate_TwoAppsConcurrently_NoRace`, `TestGet_BeforeCreateIsNil`, `…_UnknownTypeIsNil`, `…_ReturnsThisAppsInstance` | |
| P1-P5 | `TestProviders_ConstructedOncePerApp`, `…_DependenciesFirst_SameModule`, `…_DependenciesFirst_AcrossModules`, `…_GlobalInjectedConstructed`, `…_GlobalAtDepthTwo`, `…_CycleOfTwoPanics`, `…_CycleOfThreePanics`, `…_SelfDependencyPanics` | declaration order reversed on purpose; the panic lists the cycle |
| P6 | `TestProviders_UnresolvedPanics`, `…_UnexportedFieldPanics` | today's texts |
| K1-K5 | `TestHooks_OnInitDependenciesFirst`, `…_DiamondImportOnce`, `…_OnInitBeforeOwnProviders`, `…_OnReadySameOrderAfterWiring`, `…_OnInitPanicStopsCreate`, `…_OnShutdownReverseOrder`, `…_NoShutdownWithoutCreate`, `…_SharedModuleHooksPerApp` | root → A, C → B; `OnReady` sees `app.Get` non-nil |
| M1-M4 | `TestCreate_NilModulePanics`, `…_TwicePanics`, `TestApp_ConfigurationAfterCreatePanics` | one sub-case per method of M3 is one test function each (`…_BindGlobalGuardsAfterCreatePanics`, …) |
| G1-G6 | `TestLogger_DefaultIsNotNil`, `…_SameValueEachCall`, `TestSetLogger_LogsReachCustomLogger`, `…_InjectedFieldsUseIt`, `…_CustomLoggerGetsTagsAndMasking`, `…_WrappedLoggerNotWrappedAgain`, `…_NilPanics`, `…_AfterCreatePanics`; in `log`: `TestNewLog_NewLoggerPerCall`, `…_OptionsOfSecondCallApply`, `TestWrapLogger_NilFieldsOnWrappedReturnsSame` | a struct with a `log:"-"` field; JSON and text formats from two `NewLog` calls |
| D1 | `TestDevtool_WithoutVersioning`, `…_WithVersioningUnchanged` | in `devtool` and through `app.EnableDevtool()` |
| V1-V4 | in `common`: `TestParseFuncNameToURL_ServeTrailingAnyIsSegment`, `…_ServeAnyBetweenNamesIsGlob`, `…_ReadTrailingAnyStaysGlob`; in `core`: `TestServe_NameAnyServesSubtree`, `…_NameAnyDoesNotMatchPrefixSibling`, `…_NameAnyConflictsWithAnyOfName` | a temp directory with `a.txt` and `sub/x.txt` |
| benchmarks | the existing `Create` benchmarks, without resets | `b.Loop`, `b.ReportAllocs`, before and after |

## Open questions
1. **Hook order (K1, K4).** Today `OnInit` and `OnReady` run root first and `OnShutdown` runs in the
   same order. Proposed: imports first for `OnInit` / `OnReady`, the reverse for `OnShutdown`.
   Changing only `OnShutdown` (reverse of today's order: B, A… last-initialized first) is the smaller
   alternative. **Recommendation: change all three**, so "a module starts after what it imports and
   stops before it" is one rule.
2. **`SERVE_<name>_ANY` only, or `ANY` everywhere (V1-V2)?** WebSocket already reads
   `SUBSCRIBE_chat_ANY` as `chat.*` (its own segment). Making a trailing `ANY` its own segment for
   every HTTP operation would be consistent, but `READ_assets_ANY` → `/assets*/` is documented
   (`common/README.md:184`) and tested. **Recommendation: `SERVE` only now**; revisit in the API
   surface roadmap.
3. **`log.NewLog` stops being a process singleton (G5).** Needed for the Decision's
   `SetLogger(log.NewLog(…))` to work when a logger already exists (every test suite). Code that
   calls `log.NewLog(nil)` in several places expecting one shared instance gets several equal ones.
   **Recommendation: accept**, in this step, ahead of design 0010.
4. **Circular providers panic (P5).** New check; today the cycle "works" with one side unconstructed.
   **Recommendation: accept.**
5. **Strict misuse panics (M2-M3).** A program that today calls `BindGlobal…` after `Create` (it has
   no effect) will panic at startup. **Recommendation: accept**; it follows "no option whose effect
   depends on another call".

## Decisions (2026-11-16)

Approved by the user: every open question answered as recommended.

- Q1 imports first for `OnInit` / `OnReady`, the reverse for `OnShutdown`.
- Q2 `ANY` fixed for `SERVE` only; revisited in the API surface roadmap.
- Q3 `log.NewLog` stops being a singleton, in this step.
- Q4 circular providers panic.
- Q5 strict misuse panics with the `invalid app:` category.
