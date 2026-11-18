# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make test          # run all tests with coverage
make lint          # run golangci-lint
make deps          # install go + yarn dependencies
make tidy          # go mod tidy

# run a single test
go test ./internal/routing/... -run TestRouter -v

# run the sample app
cd sample && go run main.go
# run the server-rendered pages sample (http://localhost:8080/bookings; PAGES_DEV=1 from sample/pages reloads templates)
go run ./sample/pages
```

## Knowledge base

Deeper, AI-oriented docs live in `.ginject/` — start at `.ginject/INDEX.md` (routing, pipeline,
handler injection, WebSocket, modules, gotchas, package reference). When it disagrees with the
code, the code wins: fix the doc in the same change.

**Code conventions: `.ginject/conventions.md`.** Read it before writing or reviewing Go code. In short:
no comments in Go code; snake-case files with the transport first (`http_guard.go`); exported API in
topic files, unexported helpers used by two or more files in `fn.go` (`fn_<topic>.go` past ~400 lines),
helpers used by one file after their caller in that file, methods with their type; short
receivers (`instance` for user-facing option types); `Is…` option booleans; `<values>By<Key>` maps;
`load<X>Options` normalizes once; `Err<Reason>` sentinels with a `"<pkg>: "` prefix; internal test
packages, `Test<Subject>_<Behavior>`, one function per behavior, `test.DiffMessage`.

**Performance depends on the path** (details: `.ginject/conventions.md` › Performance). Correctness
and race-freedom first.
- **Hot path** (every request or message: trie, `ctx`, binding, pipeline, injection, response,
  per-request log / trace, WS loops; plus anything a client can trigger at will): optimize for speed,
  no `fmt` / reflection / regexp / `defer` (do that work once at `Create()`), take every measurable
  gain, prove each fast path equals the slow path with a test.
- **Cold path** (bootstrap, `Create()`, registration, startup log, error formatting): readability and
  ownership first, only avoid obvious waste.
- **Type A** (free: no per-call regexp, no needless alloc or copy, no `Sprintf` to concatenate)
  everywhere; **type B** (fast paths, `unsafe`, `sync.Pool`, manual buffers, inlining tricks) only on
  proven hot code. `unsafe`, `sync.Pool`, manual buffers and inlining restructures need the user's
  approval per use, in a design, with a benchmark.
- Limits before speed for client-controlled input. Propose an optimization with its expected gain
  before writing it; prove it with `-bench . -benchmem -count 5` before and after (`benchstat`).
  Benchmarks: `for b.Loop()` + `b.ReportAllocs()`.

## Product principles (the maintainer, 2026-11-16)

Ginject is for vibecoders and small teams building with AI, not for enterprises tuning every knob.

**Two first-class outputs** (2026-11-17): REST APIs and server-side rendered HTML pages. SSR uses Go's
`html/template` with Owlky's framework-free HTML version (the reason Owlky ships HTML alongside
React): one Go binary serves finished pages, with no JSON API plus a client app to build and run.
Designs consider both outputs (errors, security headers such as CSP, forms, the route DSL).

- **Powerful inside, small outside.** Build everything a production backend needs, but in the
  framework's lower layers. The surface users see is as small as possible: they learn only what they
  must, and use only what they should.
- **One obvious way.** No two APIs for the same job; no option whose effect depends on another call.
- **Safe defaults, little configuration.** Defaults are production-safe; options exist only for what
  real apps need to change. Deep tuning goes through standard Go escape hatches (`App` is an
  `http.Handler`), documented as advanced.
- **Hide by default.** A new identifier is unexported (or under `internal/`) unless users need it.
  Designs list every exported addition and justify it; the reviewer flags exported names users don't
  need.
- **Small machines first** (2026-11-16). Users run on modest servers and containers (small VPS,
  shared CI/CD runners and deploy targets Ginject will support later): CPU and memory are scarce.
  Every feature has a bounded, measured memory footprint and costs nothing when unused; background
  goroutines, buffers and caches are few, small and fixed-size; defaults are sized for a small box,
  not a large one. Designs state the idle and per-request memory and CPU cost of what they add.

## Workflow

The user approves **plans and designs**; Claude does the rest. Outside a roadmap run Claude commits only
when asked (`/commit`); during `/roadmap run` (roadmap and designs approved) it stages and commits its
own work on the current branch (no separate roadmap branch). Claude never pushes, merges or tags.

- **Bigger work → `/roadmap`.** `new`: discuss, write `docs/roadmap/NNNN-<slug>.md` → **user approves
  the roadmap** → `design`: one architect pass for the steps that need a design → **user approves the
  designs** → `run`: every step to the end with no further approval. The main session coordinates
  failures (retry, architect, `blocked` + Log) instead of stopping, then runs one review and `make check`.
- **One feature → `/feature`** (the same flow for a single design); **small change → `/implement`**.
- **No command typed → propose one, then wait.** When the user asks for work in plain words, name the
  command you would run and why in one sentence (for example "This touches the request pipeline, so I
  suggest `/feature`. OK?") and start only after they agree. Questions, explanations and read-only
  lookups need no command and no confirmation.
- **Tokens:** the flows are defaults, not rituals. Take the cheapest path that keeps the quality: the
  main session for small steps, cold agents only when they pay off, short prompts with file paths, one
  architect and one review pass per roadmap, package tests per step and `make check` once at the end.
- Slash commands (`.claude/commands/`): `/roadmap`, `/feature`, `/implement`, `/refactor [optimize]`, `/security [audit|fix]`,
  `/doc <package>` (README), `/kb [update|audit|readiness]` (this file + `.ginject/`), `/check-staged`,
  `/commit`.
- Every package that work adds or changes code in gets a `README.md` if it has none (structure of
  `modules/config/README.md`, written by `/doc`'s steps); `sample/**` is exempt.
- `make check` runs what CI runs (gofmt, `go vet`, `make api-check`, `golangci-lint` with `.golangci.yml`, `go test -race`);
  CI (`.github/workflows/ci.yml`) also runs `govulncheck` on every push, pull request and weekly.
  `make api-check` (`internal/apicheck`) compares the public API with `api/ginject.txt`; after an
  intended API change, `make api` rewrites it.
- Claude Code hooks (`.claude/settings.json`, scripts in `.claude/hooks/`): every edited `.go` file is
  gofmt-ed; before stopping, `go vet`, `golangci-lint` and `go test -race` run on the changed packages and
  a failure is sent back to Claude to fix. Pre-commit (husky + lint-staged) checks gofmt and
  golangci-lint on what is staged, then runs `make test`.

### Agents (`.claude/agents/`)

| Agent | Model | Writes | Role |
|---|---|---|---|
| `ginject-architect` | Opus by default; overridden to Fable for complex work and in the 48 hours before Fable's weekly reset (`CLAUDE.local.md`), back to Opus when Fable is unavailable | `docs/design/` only | designs, and the call when the builder fails twice |
| `ginject-tester` | Sonnet | `*_test.go` only | failing tests from an approved design (TDD red) |
| `ginject-builder` | Opus | code, README, docs; never `*_test.go` | make the tests pass, then refactor (green); the orchestrator checks that no test file changed |
| `ginject-reviewer` | Opus | nothing | review against the designs and `CLAUDE.md` |
| `ginject-security` | Opus | `go.mod` / `go.sum` and small fixes, only in `fix` mode | known vulnerabilities (govulncheck, latest advisories, Go security releases) and backend code flaws; `/security [audit\|fix]` |

Agents run one after another in the same working tree; parallel work needs one git worktree each.
Formats: `docs/roadmap/README.md`, `docs/design/README.md`.

## Architecture

Ginject is a NestJS-inspired dependency injection web framework for Go. The core pattern is **Module → Controller → Provider**.

### Package map and import rules (design 0017)

| Path | Holds | Importable by applications |
|---|---|---|
| `/` (package `ginject`: `aliases.go`, `trace.go`) | `New`, `ModuleBuilder`, `TraceTransport` and the aliases of every engine type users name (`App`, `Module`, `HTTP`, `Page`, `View`, `RenderOptions`, `Guard`, `HTTPContext`, `Body`, `Aggregation`, `VersioningOptions`…) | yes |
| `exception`, `log`, `middlewares/*`, `modules/*`, `guards/*` | exceptions, the logger and `log.Logger` / `log.LogAdapter`, built-in middlewares, modules and the throttler guard | yes |
| `internal/core`, `internal/common`, `internal/ctx` | the container and server, the controller embeds and layers, the request contexts | no (root aliases) |
| `internal/render` | the `html/template` engine behind `EnableRender`: template sets, pooled buffers, the nonce, the asset table (design 0018) | no |
| `internal/routing`, `pattern`, `wsevent`, `memorybroker`, `memorycache`, `aggregation`, `versioning`, `trace`, `accesslog`, `devtool` | the engine's other parts | no |
| `internal/apicheck`, `color`, `crypto`, `ds`, `httphook`, `inspect`, `logpipe`, `num`, `slice`, `str`, `test`, `tracecontext` | tools and utilities | no |
| `sample/`, `bench/` | the example apps (`sample/pages`: server-rendered HTML) and the framework benchmarks | no |

- Applications (and `sample/`, every README example) import only the root package plus `exception`,
  `log`, `middlewares/*`, `modules/*`, `guards/*`. `api/ginject.txt` is the exact public API; `make api-check` fails on any
  other importable package or an unreviewed change.
- The public packages import the engine by its internal path (`internal/core`, `internal/ctx`…), never the
  root package: `internal/core`'s tests import `middlewares/proxy`, so that would be a cycle.
- Hook rule: what the framework needs from a type users see is a package function of the internal package
  (`ctx.HTTPReset(c)`, `aggregation.SetMainData(a, d)`), never an exported method or field, which an alias
  would expose.
- Package names stay `core`, `ctx`, `common`…: `reflect` prints them (`*ctx.HTTPContext` in injection
  panics, devtool, traces). User-written types in the six layer-shape panics read `ginject.HTTPContext`,
  `ginject.WSContext`, `ginject.Aggregation`. Runtime errors start with `ginject:` (`cache:` for the memory store).
- Naming: settings structs `XxxOptions` passed as `opts *XxxOptions` (`nil` = defaults); option booleans
  `Is…`; accessors without `Get` (`Code()`, `ID()`); enum-like constants `<Type><Value>` (`log.LevelDebug`,
  `log.FormatJSON`, `throttler.StrategyFixedWindow`, `proxy.HeaderXForwarded`, `ginject.VersioningHeader`).

### DI Container (`internal/core/`)

Written as an application writes it (root aliases):

- **Provider**: any struct implementing `NewProvider() ginject.Provider`. Providers are injected into other providers and controllers via struct field types (reflection-based, no tags needed).
- **Controller**: any struct implementing `NewController() ginject.Controller`. Must embed `ginject.HTTP` (API routes), `ginject.Page` (HTML page routes) and/or `ginject.WS` (WebSocket) to register routes; `HTTP` and `Page` exclude each other. There is no `REST` embed.
- **Page** (design 0018, `README.md` › Pages, `internal/core/README.md` › Pages): `app.EnableRender(&ginject.RenderOptions{FS: web, IsReload: dev})` (`Dir` defaults to `web`; with an `embed.FS`, `IsReload` reads it from disk) before `Create`; a `ginject.Page` controller uses the same route words and layers, and each handler returns `ginject.View{Name, Data}` (only or last result). Files in `FS`: `layout.html` per directory tree, top-level `partials/`, root `error.html`, `assets/` (served at `/assets/*`), every other `.html` a page named by its path. Fixed template functions `nonce`, `asset`, `dict` (no user `FuncMap`). Status 200 for every method, `c.Redirect` → 303. Forms: a `Form` pipe (exported fields only) + `Bind` + `FieldLevel`, 422 re-render. `html/template` strips JavaScript comments in inline scripts.
- **Module**: built with `ginject.ModuleBuilder().Imports(...).Controllers(...).Providers(...).Build()` (inside the engine: `core.NewModuleBuilder()`). Modules compose the app tree. Set the built module's `IsGlobal` field to `true` to make its providers available everywhere; hooks are the `OnInit`, `OnReady` and `OnShutdown` fields (`internal/core/README.md` › Modules and lifecycle).
- **App**: created via `ginject.New()`, wired by `app.Create(rootModule)`, started with `app.Listen(port, nil)` (or `ListenTLS` / `Serve`; options in `ginject.ServerOptions`: `ShutdownTimeout`, `MaxRequestBodyBytes`, `TLSConfig`, `IsH2C`; `internal/core/README.md` › Server).

### REST Route Convention

Controller method names are parsed into HTTP routes — no annotations needed. Naming tokens: `READ`→GET, `CREATE`→POST, `UPDATE`→PUT, `MODIFY`→PATCH, `DELETE`→DELETE, `PREFLIGHT`→OPTIONS. Separator tokens: `BY` (path param), `AND` (additional segment), `OF` (sub-resource), `ANY` (wildcard), `VERSION` (version tag), `FILE` (file serving).

Example: `READ_BY_ID_AND_NAME` → `GET /{ID}/{NAME}` (parameter names keep the method name's case; `READ_users_BY_id` → `GET /users/{id}`)

Versioning adds a `_VERSION_X` suffix: `READ_VERSION_1` → version 1 of that route.

### Request Pipeline (per route)

```
GlobalMiddlewares → ModuleMiddlewares → GlobalGuards → ModuleGuards → GlobalInterceptors → ModuleInterceptors
  → MainHandler → module post-interceptors → global post-interceptors → response
error in a middleware → the HTTP layer's fixed error response (no user filter)
error from a guard on → module filters → global filters → built-in filter
```

The spec, arrow by arrow: `.ginject/request-pipeline.md` § 1. Only exceptions reach the client with
their code and message; anything else answers a fixed 500 and is logged. A cause's text never reaches
the client (the status text is sent, the cause logged at Debug), and a 500 exception sends the fixed
`Internal Server Error` text (design 0018 R24).

Error output: JSON without `EnableRender`. With it, a matched page route (`ginject.Page`) from its first
guard on answers HTML (`error.html` in the root layout, else a built-in page); an API, `SERVE` or asset
route answers JSON; before any guard (middleware refusals, 404, 405, 410) HTML only when an `Accept`
value contains `text/html`, with `Vary: Accept`.

Each layer is bound with `BindMiddleware(fn, handlers...)`, `BindGuard(fn, handlers...)`, etc. on the `ginject.Middleware`, `ginject.Guard`, `ginject.Interceptor`, `ginject.ExceptionFilter` embedded fields of a controller.

App-wide bindings: `app.BindGlobalMiddlewares`, `BindGlobalGuards`, `BindGlobalInterceptors`, `BindGlobalExceptionFilters`.

Contracts:
- Middleware is a real interface, `ginject.MiddlewareFn`: `Use(*http.Request, http.ResponseWriter, ginject.Next)`.
- Guards, interceptors and exception filters are **not** interfaces: they are bound as `any` (no named
  type stands for them). The framework matches the method by name and signature
  (`GuardMethodName = "CanActivate"`, `"Intercept"`, `"Catch"`) through an unexported interface, which
  differs for HTTP and WS (`internal/common/http_*.go`, `internal/common/ws_*.go`); a wrong shape panics at startup
  (`GuardShapeError` and friends), and so does a layer bound to a method that is not a handler of the
  controller (`invalid guard: … is bound to '<name>', which is not a handler of the controller`):
  - HTTP: `CanActivate(ginject.HTTPContext) bool` · `Intercept(ginject.HTTPContext, ginject.Aggregation) any` · `Catch(ginject.HTTPContext, *exception.Exception)`
  - WS: the same with `ginject.WSContext`.
  - Note the `Catch` argument order: context first, exception second.

### Handler Injection

Handler method parameters are injected by type (type identity, not the printed name) — declare them in the handler signature and the framework resolves them from the request context. Each handler gets an argument plan once at `Create()` (`newHTTPHandlerPlan` / `newWSHandlerPlan` in `internal/core/fn_plan.go`, design 0006); a request only runs it. The allowed sets (table in `internal/core/README.md` › Handler injection):

- HTTP: `ginject.HTTPContext`, `*http.Request`, `http.ResponseWriter`, `ginject.Body`, `ginject.Form`, `ginject.Query`, `ginject.Header`, `ginject.Param`, `ginject.File`, `ginject.Next`, `ginject.Redirect`, `ginject.Publisher`, and value types implementing the HTTP `*Pipeable` interfaces of `internal/common/pipe.go` (a type with `Transform(<source>, ginject.ArgumentMetadata) any`; a pointer pipe parameter is refused).
- WS: `ginject.WSContext`, `*websocket.Conn`, `ginject.WSPayload`, `ginject.Next`, `ginject.Publisher`, value types with `Transform(ginject.WSPayload, ginject.ArgumentMetadata) any`.

Anything else panics at `Create()` with `"dependency injection: ..."`.

These are the root aliases of `internal/ctx` and `internal/common` types (`aliases.go`); the root package is the only way applications name them.

### Modules: Static vs Dynamic

- **Static modules**: `var Module = func() ginject.Module { return ginject.ModuleBuilder()....Build() }` — pass the func itself to `Imports(...)`; the framework builds it once.
- **Dynamic modules**: a package-level `Register(opts *<X>ModuleOptions) *core.Module` (`ginject.Module`) that returns a configured module (every built-in module, e.g. `config.Register(&config.ConfigModuleOptions{...})`).

### Built-in Modules (`modules/`)

Each has a README in its folder.

- `modules/config`: strict `.env` + OS env loader with typed struct binding (`bind:"NAME"` tags); inject `ConfigService` (safe for concurrent `Get` / `Set`, never prints a value).
- `modules/cache`: key-value cache with TTL and an atomic `Incr` (default backend `cache.NewMemoryCache(nil)`, implemented by `internal/memorycache`: bounded, 64 shards, CLOCK eviction, no goroutine while idle; other bounds with `cache.NewMemoryCache(&cache.MemoryCacheOptions{…})`); inject `CacheService`.

There is no HTTP client or persistence module: outbound calls use `net/http` with `ginject.TraceTransport`
(`internal/trace/README.md` › Outbound calls).

### WebSocket

Controllers embed `ginject.WS` (alongside or instead of `ginject.HTTP`). Handler names start with `SUBSCRIBE` and map to event names (e.g. `SUBSCRIBE_chat_PERSON_ANY`); enable the endpoint with `app.EnableWS(&ginject.WSOptions{Path: "ws"}, middlewares...)`. The framework handles the `websocket.Conn` lifecycle; handlers receive `ginject.WSPayload` for incoming events and fan out through `ginject.Publisher`.

### Bootstrap-time Error Convention

Build/config failures (route conflicts, unresolved dependencies, invalid handlers) in `internal/core/`, `internal/common/`, `internal/routing/` are reported via `panic()`, not returned as `error` — there is no error-return API (`Build()` returns the module, `Create()` returns nothing; neither returns an `error`). This only fires during `app.Create()`'s module/controller wiring (or a configuration call made after it), never on the request path, so an uncaught panic there is fail-fast by design.

Message format is `"<category>: <detail>"`:
- `"route conflict: ..."` (`internal/core/container.go`, per app: the same method, version and route shape; routes that differ only in parameter names conflict; `SERVE_static_ANY` and `SERVE_ANY_OF_static` are the same route, `/static/*`)
- `"event conflict: ..."` (`internal/core/container.go`, per app)
- `"invalid handler: ..."` (`internal/routing/router.go`; `internal/core/container.go` `checkViewResults`: a page handler not returning `ginject.View`, an API handler returning one)
- `"invalid route: ..."` (`internal/routing/router.go`: a partial parameter segment such as `/files/{name}.json`, a literal `$` segment, or more than 64 segments)
- `"invalid controller: ..."` (`internal/core/container.go`: a controller embedding both `ginject.HTTP` and `ginject.Page`)
- `"invalid render: ..."` (`internal/core/app.go`, `container.go`, `http_page.go`: `EnableRender` without `FS` or called twice, a `Page` controller without `EnableRender`, a template that fails to parse, calls a missing template or fails to escape, with its file path)
- `"invalid module: ..."` (`internal/core/module_builder.go`; `Create(nil)` in `internal/core/app.go`)
- `"invalid app: ..."` (`internal/core/app.go`: `Create` called twice, a configuration method (`EnableRender` too) or `SetLogger` after `Create`, `SetLogger(nil)`)
- `"dependency injection: ..."` (`internal/core/fn.go`, `internal/core/fn_plan.go`, `internal/core/container.go`)
- `"invalid config: ..."` (`modules/config`: a bad `.env` file or line with its path and line number, never a value; contradicting options; too deeply nested `Loads`; `Set` on a zero `ConfigService`)

These are wrapped in `color.FmtRed(...)` since they only ever surface on a developer's terminal at startup. If an `err` you're panicking already came from a function that applies `color.FmtRed`, just `panic(err)` — don't re-wrap, or the ANSI codes nest and garble the output. An unresolvable handler argument is a `Create()` panic too; the only per-request panic of injection is a pipe whose `Transform` returns a wrong value, an uncolored `exception.InternalServerErrorException` that `internal/core/http.go`'s `recover()` turns into a 500.
