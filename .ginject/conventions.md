# Code conventions

How Ginject code is written today, measured from the repository (2026-11-16). Each rule cites a
real example. "Majority" rules say how strong the habit is; follow the majority in new code and
don't mass-rewrite the exceptions.

## Source files

- **No comments in Go code**, tests and benchmarks included (commit `5babd36` stripped them with
  an AST pass). One block survives at `core/app.go:208` (the init order of `App.Create`); leave it,
  don't add others. Explain "why" in the commit message, the README or `.ginject/`.
- **File names** are snake case, one concern per file, the transport in front when a concept has an
  HTTP and a WS side: `common/http_guard.go`, `common/ws_guard.go`, `core/ws_connmgr.go`,
  `core/read_loop.go`. The demo app uses dots (`sample/shared/log.http_guard.go`): that style stays
  in `sample/`.
- **Tests and benchmarks sit next to the file:** `<file>_test.go`, `<file>_bench_test.go`
  (`common/http_guard_test.go`, `common/http_guard_bench_test.go`). A test that spans files gets its
  own name (`core/ws_fanout_test.go`).
- **Test packages are internal** (`package core`, not `core_test`): 2 files in the repository are
  external (`wsevent`). Write internal tests unless the test is about the public API only.

## Naming

- **Receivers are one to three letters** from the type (`c`, `p`, `m`, `e`, `app`, `rb`): 683 of 737
  methods outside `sample/`. `func (s *shard) evictLocked` (`memorycache/memory_cache.go:42`).
- **`instance` is the receiver of user-facing configuration types** whose value the user writes:
  `func (instance CORS) NewMiddleware()` (`middlewares/cors/cors.go:58`), the `csrf` and `helmet`
  middlewares, `log/log.go`, and every controller, provider, guard and filter in `sample/`. Use it
  for types a user declares in their own app, and in examples.
- **Exported option booleans start with `Is`:** `IsGlobal`, `IsAllowCredentials`,
  `IsPreflightContinue`, `IsExpandVariables`, `IsOverride`. Exceptions (`DisableHSTS`,
  `HSTSPreload`, `AddSource`, `DisableGitignore`) predate the habit; new options use `Is`.
- **Maps are named `<values>By<Key>`:** `entriesByKey`, `funcNameByEvent`, `catchFnsByRoute`,
  `docsByTable`.
- **`Locked` suffix** marks a method that expects its caller to hold the lock
  (`evictLocked`).
- **Constructors** are `New<Type>` (`NewMemoryCache`, `NewRouter`, `NewWSConnmgr`). Constants that
  name a looked-up method end in `MethodName` (`GuardMethodName = "CanActivate"`,
  `common/guard.go:10`).
- Acronyms keep one case: `HTTP`, `WS`, `ID`, `JSON`, `URL` (`NewWSContext`, `ErrInvalidID`).

## Configuration and modules

- **Options struct + loader:** a public `<X>Options` (or the middleware type itself) holds what the
  user sets; a private `load<X>Options` fills defaults once and returns the normalized form:
  `loadCORSOptions` (`middlewares/cors/cors.go:175`), `loadLogOptions` (`log/log.go:61`),
  `loadConfigOptions` (`modules/config/module.go:28`). Never normalize on the request path.
- **Middlewares** are a value type with the public options as fields. When they have options to
  compile, they implement `NewMiddleware() common.MiddlewareFn`, which the framework calls once
  through `common.Construct` (`common/fn.go:189`, `core/app.go:396`) and which returns a private
  compiled type that implements `Use` (`compiledCORS`). `cors`, `csrf` and `helmet` follow it.
- **Dynamic modules** are `Register(opts *<X>ModuleOptions) *core.Module` that accept `nil` and
  apply defaults, then set `IsGlobal` / `OnInit` on the built module
  (`modules/cache/module.go:16`). `OnInit` types are named `<X>OnInitFn`.

## Errors and panics

- **Bootstrap failures panic** with `"<category>: <detail>"` in `color.FmtRed` (see `CLAUDE.md`,
  Bootstrap-time Error Convention). The request path never panics on purpose.
- **Runtime errors are values.** Sentinels are `Err<Reason>` in a `var` block, with messages
  prefixed by the package and a colon, lower case, no final period:
  `ErrEmptyKey = errors.New("memorycache: key must not be empty")`
  (`memorycache/memory_cache.go:18`), the `memorybroker` errors. Callers compare them with `==` or
  `errors.Is`.
- **Wrap with `%w`** and the package prefix when adding context:
  `fmt.Errorf("httpclient: JSON marshal: %w", err)`.
- Known inconsistency: `modules/storage/errors.go` prefixes `store:` instead of `storage:`; a few
  `core` errors have no prefix (`"root not found: %s"`). Follow the rule in new code.

## Concurrency

- Shared state sits behind `sync.Mutex` / `sync.RWMutex` fields named `mu` (10 of 12), next to the fields
  they guard (`shard` in `memorycache/memory_cache.go`). Hot shared maps are sharded.
- Types with background goroutines expose a stop method (`MemoryCache.Stop`) and tests `defer`
  it. Packages that touch shared state carry a `..._NoRace` / `Concurrent...` test run under `-race`
  (`core/ws_connmgr_test.go`, `core/fn_test.go`).

## Tests

- **Name:** `Test<Subject>_<Behavior>` (818 of 998 tests have one or two underscores):
  `TestMemoryCache_GetMiss`, `TestWSConnection_TrySend_ConcurrentSendsNoRace`.
- **One function per behavior**, not table tests (`t.Run` appears in one test file).
- **Assertions:** `t.Error(test.DiffMessage(actual, expected, "what"))` from `internal/test`
  (about 1,400 uses); `t.Fatal(err)` / `t.Fatalf` for setup that makes the rest meaningless.
  `t.Errorf` is the exception (about 60 uses), kept for messages that `DiffMessage` can't express.
- **Benchmarks** use realistic input sizes. 20 of 60 benchmark files call `b.ReportAllocs()`; new
  benchmarks call it, since allocations are what `/refactor optimize` measures.
