# Code conventions

How Ginject code is written today, measured from the repository (2026-11-16). Each rule cites a
real example. "Majority" rules say how strong the habit is; follow the majority in new code and
don't mass-rewrite the exceptions.

## Source files

- **No comments in Go code**, tests and benchmarks included (commit `5babd36` stripped them with
  an AST pass). One block survives at `core/app.go:213` (the init order of `App.Create`); leave it,
  don't add others. Explain "why" in the commit message, the README or `.ginject/`.
- **File names** are snake case, one concern per file, the transport in front when a concept has an
  HTTP and a WS side: `common/http_guard.go`, `common/ws_guard.go`, `core/ws_connmgr.go`,
  `core/read_loop.go`. The demo app uses dots (`sample/shared/log.http_guard.go`): that style stays
  in `sample/`.
- **Where functions live in a package** (the maintainer's rule D-034, adopted on master 2026-11-16):
  - Exported API goes in files named after their topic: `trie.go`, `router.go`, `binding.go`.
  - An unexported helper (function, constant, variable or type) used by **two or more files** goes in
    `fn.go`, its tests in `fn_test.go`.
  - An unexported helper used by **one file** stays in that file. A helper called by one function goes
    right after that function; constants, variables and types used by several functions of the file
    stay at the top of the file, after the imports. Exported functions are never reordered to fit
    this rule.
  - A package with a single code file has no `fn.go`: everything stays in that file.
  - A file whose whole content is one unexported topic (a subsystem such as `core/read_loop.go`,
    `core/write_loop.go`, `core/publisher.go`, `core/default.go`, `ctx/binding_typed.go`, the
    handler plans) keeps its own topic-named file even when other files call into it. `fn.go` is for
    small helpers that belong to no such topic.
  - When a second file starts using a helper, move the helper and its tests to `fn.go` / `fn_test.go`.
  - Methods stay with their type, whatever calls them (`handleRequest` is a method of `*HTTP`, so it
    stays in `http.go`); the rule is about free functions, constants, variables and types.
  - A `fn.go` over about 400 lines splits by topic into `fn_<topic>.go` (and `fn_<topic>_test.go`),
    for example `core/fn_plan.go`.
  - Moves are pure: no logic change in the same commit as a move.
- **Tests and benchmarks sit next to the file:** `<file>_test.go`, `<file>_bench_test.go`
  (`common/http_guard_test.go`, `common/http_guard_bench_test.go`). A test that spans files gets its
  own name (`core/ws_fanout_test.go`).
- **Test packages are internal** (`package core`, not `core_test`): 2 files in the repository are
  external (`wsevent`). Write internal tests unless the test is about the public API only.

## Naming

- **Receivers are one to three letters** from the type (`c`, `p`, `m`, `e`, `app`, `rb`): 683 of 737
  methods outside `sample/`. `func (s *shard) evictOneLocked` (`memorycache/memory_cache.go`).
- **`instance` is the receiver of user-facing configuration types** whose value the user writes:
  `func (instance CORS) NewMiddleware()` (`middlewares/cors/cors.go:100`), the `csrf` and `helmet`
  middlewares, `log/log.go`, and every controller, provider, guard and filter in `sample/`. Use it
  for types a user declares in their own app, and in examples.
- **Exported option booleans start with `Is`:** `IsGlobal`, `IsAllowCredentials`,
  `IsAllowPrivateNetwork`, `IsExpandVariables`, `IsOverride`. Exceptions (`AddSource`,
  `DisableGitignore`) predate the habit; new options use `Is`.
- **Maps are named `<values>By<Key>`:** `entriesByKey`, `funcNameByEvent`, `routeByPattern`,
  `docsByTable`.
- **`Locked` suffix** marks a method that expects its caller to hold the lock
  (`evictOneLocked`).
- **Constructors** are `New<Type>` (`NewMemoryCache`, `NewRouter`, `NewWSConnmgr`). Constants that
  name a looked-up method end in `MethodName` (`GuardMethodName = "CanActivate"`,
  `common/guard.go:10`).
- Acronyms keep one case: `HTTP`, `WS`, `ID`, `JSON`, `URL` (`NewWSContext`, `ErrInvalidID`).

## Configuration and modules

- **Options struct + loader:** a public `<X>Options` (or the middleware type itself) holds what the
  user sets; a private `load<X>Options` fills defaults once and returns the normalized form:
  `loadCORSOptions` (`middlewares/cors/cors.go:108`),
  `loadConfigOptions` (`modules/config/module.go:28`). Never normalize on the request path.
- **Middlewares** are a value type with the public options as fields. When they have options to
  compile, they implement `NewMiddleware() common.MiddlewareFn`, which the framework calls once
  through the app's container (`(*container).construct`, `core/container.go`) and which returns a private
  compiled type that implements `Use` (`compiledCORS`). Every package under `middlewares/` follows
  it; the option type's own `Use` calls `instance.NewMiddleware().Use(...)` and is for tests only.
  Invalid options panic in `NewMiddleware` as `invalid <package>: <detail>` (`color.FmtRed`), and a
  refusal on the request path is `panic(&prebuiltException)`, answered by the HTTP layer.
- **Dynamic modules** are `Register(opts *<X>ModuleOptions) *core.Module` that accept `nil` and
  apply defaults, then set `IsGlobal` / `OnInit` on the built module
  (`modules/cache/module.go:16`). `OnInit` types are named `<X>OnInitFn`.

## Errors and panics

- **Bootstrap failures panic** with `"<category>: <detail>"` in `color.FmtRed` (see `CLAUDE.md`,
  Bootstrap-time Error Convention). The request path never panics on purpose.
- **Runtime errors are values.** Sentinels are `Err<Reason>` in a `var` block, with messages
  prefixed by the package and a colon, lower case, no final period:
  `ErrEmptyKey = errors.New("memorycache: key must not be empty")`
  (`memorycache/memory_cache.go`), the `memorybroker` errors. Callers compare them with `==` or
  `errors.Is`.
- **Wrap with `%w`** and the package prefix when adding context:
  `fmt.Errorf("core: listen on %s: %w", address, err)` (`core/server.go`).
- Known inconsistency: a few `core` errors have no prefix (`"root not found: %s"`). Follow the rule in new code.

## Concurrency

- Shared state sits behind `sync.Mutex` / `sync.RWMutex` fields named `mu` (10 of 12), next to the fields
  they guard (`shard` in `memorycache/memory_cache.go`). Hot shared maps are sharded.
- Prefer no background goroutine at all: work that must happen later runs on a timer armed only
  while there is work, holding its owner through a `weak.Pointer` so an unreferenced owner is
  collected and needs no stop method (`memorycache`'s sweep). Types that do own a goroutine expose a
  stop method (`AccessLog.Stop`) and tests `defer` it. Packages that touch shared state carry a `..._NoRace` / `Concurrent...` test run under `-race`
  (`core/ws_connmgr_test.go`, `core/fn_test.go`).

## Performance: hot path and cold path

Correctness and race-freedom come first, always. Then the rule depends on where the code runs.

- **Hot path:** code that runs on every request or message: route lookup and the trie (`routing/`,
  `internal/ds`), `ctx` creation and binding, the middleware → guard → interceptor → handler → filter
  pipeline, handler injection, response writing, per-request logging and tracing, WS read / write
  loops and fanout.
  - Optimize for speed: minimize allocations; keep `fmt`, reflection, regexp and `defer` out. Do
    that work once at `Create()` / construction time and keep the result (the `load<X>Options`
    pattern, precomputed handler argument lists).
  - **Take every measurable gain, however small.** Costs add up across the pipeline: ten functions at
    14 ns each is 140 ns per request. A gain counts when it is stable across repeated runs or removes
    an allocation.
  - Every fast path has a test proving it returns what the slow path returns (for example over random
    inputs).
  - The maintainer must still understand every optimization: explain each one in the design or the
    package README's performance notes.
  - Code a client can trigger at will (warm code: an error path, a 404, a rejected guard) counts as
    hot.
- **Cold path:** everything else: bootstrap, `Create()`, module and route registration, startup
  logging, error formatting, rarely called helpers. Write for readability and ownership; avoid only
  obvious waste.
- **Type A vs type B optimizations.**
  - **Type A** is free (no readability cost, no extra branch) and applies everywhere: no regexp
    compiled per call, no needless allocation or copy, no `Sprintf` for plain concatenation,
    pre-sized slices and maps when the size is known.
  - **Type B** costs something: a fast path, `unsafe`, `sync.Pool`, hand-managed buffers, restructuring
    to make a function inline. Only on proven hot or warm code.
  - Not sure which? Do type A now and record the type B idea in the roadmap or the review report.
- **Heavy techniques need the maintainer's approval for each use, before the code is written:**
  `unsafe`, `sync.Pool`, hand-managed buffers, restructuring to inline. Put them in a design (with a
  benchmark showing the gain and the constraint they bring); an agent never adds one on its own.
- **Bound client-controlled work with limits first** (body size, header count, path depth, message
  size), then make it fast.
- **Propose before optimizing:** state the expected gain, then measure. Never optimize first and
  explain afterwards.
- **Numbers or it didn't happen:** `go test -run='^$' -bench . -benchmem -count 5` before and after, on
  the same machine; compare with `benchstat` (installed in `~/go/bin`).
- **Benchmarks** use `for b.Loop() { ... }` and `b.ReportAllocs()` (Go 1.24+: `b.Loop` keeps results
  alive, so no sink variable). `b.RunParallel` still needs a sink: count per goroutine, then add to an
  `atomic` value at the end. Most existing benchmarks still use `for i := 0; i < b.N; i++`: convert
  one when you touch it.

## Tests

- **Name:** `Test<Subject>_<Behavior>` (818 of 998 tests have one or two underscores):
  `TestMemoryCache_GetMiss`, `TestWSConnection_TrySend_ConcurrentSendsNoRace`.
- **One function per behavior**, not table tests (`t.Run` appears in one test file).
- **Assertions:** `t.Error(test.DiffMessage(actual, expected, "what"))` from `internal/test`
  (about 1,400 uses); `t.Fatal(err)` / `t.Fatalf` for setup that makes the rest meaningless.
  `t.Errorf` is the exception (about 60 uses), kept for messages that `DiffMessage` can't express.
- **Benchmarks** use realistic input sizes; their loop and allocation reporting follow the
  Performance section above (20 of 60 benchmark files call `b.ReportAllocs()` today).
