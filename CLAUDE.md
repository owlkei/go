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

# vet and test every example (each examples/<NN-name> is its own module)
make examples
# run an example (01-hello, 02-rest-api, 03-pages, 04-async-modules, 05-production)
cd examples/03-pages && go run .
```

## Knowledge base

Deeper, AI-oriented docs live in `.owlky/` — start at `.owlky/INDEX.md` (routing, pipeline,
handler injection, WebSocket, modules, gotchas, package reference). When it disagrees with the
code, the code wins: fix the doc in the same change.

**Code conventions: `.owlky/conventions.md`.** Read it before writing or reviewing Go code. In short:
no comments in Go code; snake-case files with the transport first (`http_guard.go`); exported API in
topic files, unexported helpers used by two or more files in `fn.go` (`fn_<topic>.go` past ~400 lines),
helpers used by one file after their caller in that file, methods with their type; short
receivers (`instance` for user-facing option types); `Is…` option booleans; `<values>By<Key>` maps;
`load<X>Options` normalizes once; `Err<Reason>` sentinels with a `"<pkg>: "` prefix; internal test
packages, `Test<Subject>_<Behavior>`, one function per behavior, `test.DiffMessage`.

**Performance depends on the path** (details: `.owlky/conventions.md` › Performance). Correctness
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

Owlky Go is for vibecoders and small teams building with AI, not for enterprises tuning every knob.

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
  shared CI/CD runners and deploy targets Owlky Go will support later): CPU and memory are scarce.
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
  `/doc <package>` (README), `/kb [update|audit|readiness]` (this file + `.owlky/`), `/check-staged`,
  `/commit`.
- Every package that work adds or changes code in gets a `README.md` if it has none (structure of
  `modules/config/README.md`, written by `/doc`'s steps); `examples/**` is exempt.
- `make check` runs what CI runs (gofmt, `go vet`, `make api-check`, `golangci-lint` with `.golangci.yml`, `go test -race`);
  CI (`.github/workflows/ci.yml`) also runs `govulncheck` on every push, pull request and weekly.
  `make api-check` (`internal/apicheck`) compares the public API with `api/owlky.txt`; after an
  intended API change, `make api` rewrites it.
- Claude Code hooks (`.claude/settings.json`, scripts in `.claude/hooks/`): every edited `.go` file is
  gofmt-ed; before stopping, `go vet`, `golangci-lint` and `go test -race` run on the changed packages and
  a failure is sent back to Claude to fix. Pre-commit (husky + lint-staged) checks gofmt and
  golangci-lint on what is staged, then runs `make test`.

### Agents (`.claude/agents/`)

| Agent | Model | Writes | Role |
|---|---|---|---|
| `go-architect` | Opus by default; overridden to Fable for complex work and in the 24 hours before Fable's weekly reset (`CLAUDE.local.md`), back to Opus when Fable is unavailable | `docs/design/` only | designs, and the call when the builder fails twice |
| `go-tester` | Sonnet | `*_test.go` only | failing tests from an approved design (TDD red) |
| `go-builder` | Opus | code, README, docs; never `*_test.go` | make the tests pass, then refactor (green); the orchestrator checks that no test file changed |
| `go-reviewer` | Opus | nothing | review against the designs and `CLAUDE.md` |
| `go-security` | Opus | `go.mod` / `go.sum` and small fixes, only in `fix` mode | known vulnerabilities (govulncheck, latest advisories, Go security releases) and backend code flaws; `/security [audit\|fix]` |

Agents run one after another in the same working tree; parallel work needs one git worktree each.
Formats: `docs/roadmap/README.md`, `docs/design/README.md`.

## Architecture

Owlky Go is a NestJS-inspired dependency injection web framework for Go. The core pattern is **Module → Controller → Provider**.

### Package map and import rules (design 0017)

| Path | Holds | Importable by applications |
|---|---|---|
| `/` (package `owlky`: `aliases.go`, `options.go`, `trace.go`) | `New`, `OptionsFromEnv`, `ModuleBuilder`, `TraceTransport`, `ClientIP` / `ClientScheme` / `ClientHost`, `AppOptions` with its groups and `ByteSize`, and the aliases of every engine type users name (`App`, `Module`, `HTTP`, `RenderOptions`, `PathStyle`, `Guard`, `HTTPContext`, `Body`, `Aggregation`, `VersioningOptions`…) | yes |
| `exception`, `log`, `modules/*`, `guards/*` | exceptions, the logger and `log.Logger` / `log.LogAdapter`, modules and the throttler guard | yes |
| `internal/core`, `internal/common`, `internal/ctx` | the container and server, the controller embeds and layers, the request contexts | no (root aliases) |
| `internal/middlewares/*` (`loadshed`, `proxy`, `hostcheck`, `httpsredirect`, `helmet`, `cors`, `csrf`, `compress`, `origin`), `internal/dotenv` | the pre-route stages `New` builds from `AppOptions`, in that fixed order (`origin`: the shared `Origins` set and the WebSocket handshake check), and the `.env` parser shared by `modules/config` and `OptionsFromEnv` (design 0022) | no |
| `internal/render` | the `html/template` engine behind `AppOptions.Render`: template sets, pooled buffers, the nonce, the asset table (design 0018) | no |
| `internal/routing`, `pattern`, `wsevent`, `memorybroker`, `memorycache`, `aggregation`, `versioning`, `trace`, `accesslog`, `devtool` | the engine's other parts | no |
| `internal/apicheck`, `color`, `crypto`, `ds`, `httphook`, `inspect`, `logpipe`, `num`, `slice`, `str`, `test`, `tracecontext` | tools and utilities | no |
| `examples/`, `bench/` | five example apps in learning order, each its own module with `replace go.owlky.com/owlky => ../..` (`make examples` vets and tests them; roadmap 0011), and the framework benchmarks | no |

- Applications (and `examples/`, every README example) import only the root package plus `exception`,
  `log`, `modules/*`, `guards/*`. `api/owlky.txt` is the exact public API; `make api-check` fails on any
  other importable package or an unreviewed change.
- The public packages import the engine by its internal path (`internal/core`, `internal/ctx`…), never the
  root package (`guards/throttler` reads the client with `tracecontext.ClientIP`, not `owlky.ClientIP`).
- Hook rule: what the framework needs from a type users see is a package function of the internal package
  (`ctx.HTTPReset(c)`, `aggregation.SetMainData(a, d)`), never an exported method or field, which an alias
  would expose.
- Package names stay `core`, `ctx`, `common`…: `reflect` prints them (`*ctx.HTTPContext` in injection
  panics, devtool, traces). User-written types in the six layer-shape panics read `owlky.HTTPContext`,
  `owlky.WSContext`, `owlky.Aggregation`. Runtime errors start with `owlky:` (`cache:` for the memory store).
- Naming: settings structs `XxxOptions` passed as `opts *XxxOptions` (`nil` = defaults); option booleans
  `Is…`; accessors without `Get` (`Code()`, `ID()`); enum-like constants `<Type><Value>` (`log.LevelDebug`,
  `log.FormatJSON`, `throttler.StrategyFixedWindow`, `owlky.ProxyHeaderXForwarded`, `owlky.VersioningHeader`, `owlky.PathStyleKebab`).

### DI Container (`internal/core/`)

Written as an application writes it (root aliases):

- **Provider**: any struct implementing `NewProvider() owlky.Provider`. Providers are injected into other providers and controllers via struct field types (reflection-based, no tags needed).
- **Controller**: any struct implementing `NewController() owlky.Controller`. Must embed `owlky.HTTP` (API and page routes) and/or `owlky.WS` (WebSocket) to register routes. There is no `REST` embed.
- **Page** (design 0018 amendment 2, `README.md` › Pages, `internal/core/README.md` › Pages): `Render: &owlky.RenderOptions{FS: web, IsReload: dev}` in `AppOptions` (`Dir` defaults to `web`; with an `embed.FS`, `IsReload` reads it from disk). In a `owlky.HTTP` controller, `RENDER_<route>` is a GET page and `SUBMIT_<route>` the POST on the same path; the route picks the template (`<path>.html` or `<path>/index.html` after prefixes and `PathStyle`, `{id}` for parameters), the handler's result is the data. Files in `Dir`: `layout.html` per directory tree, top-level `partials/`, root `<400–599>.html` / `4xx.html` / `5xx.html` error pages, `assets/` (served at `/assets/*`), `public/` (served at the root URL), `X.css` / `X.js` next to `X.html` (linked by `{{pageAssets}}`, served at `/assets/_pages/…`); an `.html` without a route stops `Create`. Fixed template functions `nonce`, `asset`, `dict`, `pageAssets` (no user `FuncMap`). `RENDER_` answers 200, a `SUBMIT_` that renders 422, `c.Redirect` → 303. Forms: a `Form` pipe (exported fields only) + `Bind` + `FieldLevel`. `html/template` strips JavaScript comments in inline scripts.
- **Module**: built with `owlky.ModuleBuilder().Imports(...).Controllers(...).Providers(...).Build()` (inside the engine: `core.NewModuleBuilder()`); `.PathStyle(owlky.PathStyleAsWritten)` keeps `_` in its URLs and those of its imports (default `owlky.PathStyleKebab`; inherited like `Prefix`, an explicit setting wins). Modules compose the app tree. Set the built module's `IsGlobal` field to `true` to make its providers available everywhere; hooks are the `OnInit`, `OnReady` and `OnShutdown` fields (`internal/core/README.md` › Modules and lifecycle). `.AsyncImports(...)` builds entries in the background (see Modules and module factories below).
- **App**: created via `owlky.New(opts *owlky.AppOptions)` (`nil` = defaults; `owlky.OptionsFromEnv()` fills the same struct from `OK_*` variables, `PORT` and `./.env`), wired by `app.Create(rootModule)`, started with `app.Listen()` (or `app.Serve(listener)`). The whole configuration is in `AppOptions`, read and checked once at `New` (design 0022): `Logger`, `StartupTimeout`, `IsAccessLog`, `IsDevtool`, `Server` (`Port`, `ShutdownTimeout`, `TLS`, `IsH2C`), `HTTP` (`MaxBodyBytes` as a `ByteSize`, `ServeDirs`), `Render`, `WS`, `Versioning`, and the pre-route stages `Proxy`, `Hosts`, `Origins`, `LoadShed`, `HTTPSRedirect`, `Headers`, `CORS`, `CSRF`, `Compress` (off until configured; fixed order loadshed → proxy → hostcheck → httpsredirect → headers → cors → csrf → compress, before routing and user middlewares). There are no `Enable…` / `Set…` methods and no public built-in middlewares.

### REST Route Convention

Controller method names are parsed into HTTP routes — no annotations needed (design 0019; `README.md` › Routes from method names, `internal/common/README.md` › Route DSL). Operation tokens: `READ`→GET, `CREATE`→POST, `UPDATE`→PUT, `MODIFY`→PATCH, `DELETE`→DELETE, `PREFLIGHT`→OPTIONS, `RENDER`→GET page, `SUBMIT`→POST on its `RENDER_`'s path, `SERVE`→static files. Route words: `BY` (path param), `AND` (another param), `OF` (parent resource, placed first), `ANY` (wildcard `*`), `FILE` (`READ_robots_txt_FILE` → `/robots.txt`), `VERSION` (version tag); WebSocket: `ANY` (`*`), `ALL` (`**`, last word only).

- Words of one resource are kebab-case: `READ_user_profile` → `GET /user-profile` (`/user_profile` with `PathStyleAsWritten`). camelCase, parameter names, versions, `Prefix` and `Path` strings are never rewritten. Non-resource segments go through `OF`: `READ_new_OF_bookings` → `GET /bookings/new`.
- Example: `READ_BY_ID_AND_NAME` → `GET /{ID}/{NAME}` (parameter names keep the method name's case; `READ_users_BY_id` → `GET /users/{id}`). Versioning adds a `_VERSION_X` suffix: `READ_VERSION_1` → version 1 of that route.
- `instance.Path("/oauth2/token", instance.CREATE_token)` in `NewController()` gives a handler an exact path; method, version and prefixes still apply.
- An upper-case word that is not a route word (`READ_orgs_FB`, `SUBSCRIBE_chat_V2`) and a malformed name (`READ_users_BY`, `READ_OF_users`, `READ_txt_FILE`, `READ_logo_png_FILE_FILE`, `SERVE_x_ANY_ANY`) stop `Create()` with `invalid route:`.

### Request Pipeline (per route)

```
GlobalMiddlewares → ModuleMiddlewares → GlobalGuards → ModuleGuards → GlobalInterceptors → ModuleInterceptors
  → MainHandler → module post-interceptors → global post-interceptors → response
error in a middleware → the HTTP layer's fixed error response (no user filter)
error from a guard on → module filters → global filters → built-in filter
```

The spec, arrow by arrow: `.owlky/request-pipeline.md` § 1. Only exceptions reach the client with
their code and message; anything else answers a fixed 500 and is logged. A cause's text never reaches
the client (the status text is sent, the cause logged at Debug), and a 500 exception sends the fixed
`Internal Server Error` text (design 0018 R24).

Error output: JSON without `Render.FS`. With it, a matched page route (`RENDER_`, `SUBMIT_`) from its first
guard on answers HTML (`<code>.html`, then `<N>xx.html` in the root layout, else a built-in page); an API,
`SERVE`, public-file or asset route answers JSON; before any guard (middleware refusals, 404, 405, 410) HTML only when an `Accept`
value contains `text/html`, with `Vary: Accept`.

Each layer is bound with `BindMiddleware(fn, handlers...)`, `BindGuard(fn, handlers...)`, etc. on the `owlky.Middleware`, `owlky.Guard`, `owlky.Interceptor`, `owlky.ExceptionFilter` embedded fields of a controller.

App-wide bindings: `app.BindGlobalMiddlewares`, `BindGlobalGuards`, `BindGlobalInterceptors`, `BindGlobalExceptionFilters`.

Contracts:
- Middleware is a real interface, `owlky.MiddlewareFn`: `Use(*http.Request, http.ResponseWriter, owlky.Next)`.
- Guards, interceptors and exception filters are **not** interfaces: they are bound as `any` (no named
  type stands for them). The framework matches the method by name and signature
  (`GuardMethodName = "CanActivate"`, `"Intercept"`, `"Catch"`) through an unexported interface, which
  differs for HTTP and WS (`internal/common/http_*.go`, `internal/common/ws_*.go`); a wrong shape panics at startup
  (`GuardShapeError` and friends), and so does a layer bound to a method that is not a handler of the
  controller (`invalid guard: … is bound to '<name>', which is not a handler of the controller`):
  - HTTP: `CanActivate(owlky.HTTPContext) bool` · `Intercept(owlky.HTTPContext, owlky.Aggregation) any` · `Catch(owlky.HTTPContext, *exception.Exception)`
  - WS: the same with `owlky.WSContext`.
  - Note the `Catch` argument order: context first, exception second.

### Handler Injection

Handler method parameters are injected by type (type identity, not the printed name) — declare them in the handler signature and the framework resolves them from the request context. Each handler gets an argument plan once at `Create()` (`newHTTPHandlerPlan` / `newWSHandlerPlan` in `internal/core/fn_plan.go`, design 0006); a request only runs it. The allowed sets (table in `internal/core/README.md` › Handler injection):

- HTTP: `owlky.HTTPContext`, `*http.Request`, `http.ResponseWriter`, `owlky.Body`, `owlky.Form`, `owlky.Query`, `owlky.Header`, `owlky.Param`, `owlky.File`, `owlky.Next`, `owlky.Redirect`, `owlky.Publisher`, and value types implementing the HTTP `*Pipeable` interfaces of `internal/common/pipe.go` (a type with `Transform(<source>, owlky.ArgumentMetadata) any`; a pointer pipe parameter is refused).
- WS: `owlky.WSContext`, `*websocket.Conn`, `owlky.WSPayload`, `owlky.Next`, `owlky.Publisher`, value types with `Transform(owlky.WSPayload, owlky.ArgumentMetadata) any`.

Anything else panics at `Create()` with `"dependency injection: ..."`.

These are the root aliases of `internal/ctx` and `internal/common` types (`aliases.go`); the root package is the only way applications name them.

### Modules and module factories

Two kinds, passed to `Imports(...)` or `AsyncImports(...)`: a **Module** (a `owlky.Module` value: `owlky.ModuleBuilder()....Build()`, or what a function such as `config.Register(&config.ConfigModuleOptions{...})` returns; `Register` is only a name) and a **Module factory** (a function value returning a `owlky.Module`, whose parameters are injected; the framework calls it once per app). **Global** (`IsGlobal`) is a property of either, not a kind; async is how a module is imported. Design 0020; user guide `README.md` › Module lifecycle, rules `internal/core/README.md` › Module factories / AsyncImports and module states / Retry.

- Factory shapes: `func(deps...) Module`, `func(deps...) (Module, error)`, `func(ctx, deps...) (Module, error)`; any other panics at `Build()`. Parameters resolve from siblings (the other entries of the same `Imports` / `AsyncImports`), then globals, then `log.Logger` / `Publisher`; factories run in dependency order whatever their position.
- `Imports` fails fast: a factory error, panic or `AppOptions.StartupTimeout` deadline (default 30 s, one budget for `Create`, in `ctx`) stops `Create()`. `AsyncImports` never stops the app: entries build in the background (one goroutine each, `ctx` canceled at shutdown), modules that inject their providers are `pending` → `ready` | `failed` (one way); their routes stay registered behind a gate and answer `503` (`Retry-After` = seconds to the next `Retry` attempt, else 5; none when failed; `503.html` on pages), WS events reply `not ready: <topic>`. `OnShutdown` only for `ready` modules.
- Refused: controllers in a module an async factory returns (entry fails); one entry in both lists; a `NewController` that uses injected providers (gated controllers run it twice: a probe at `Create()`, then for real); app-wide layers injecting async providers; an `Imports` factory needing an async module.
- `owlky.Retry(ctx, *owlky.RetryOptions, fn)`: backoff with jitter until `ctx` is done or `MaxAttempts`; with an async factory's `ctx` it reports `ModuleRetry`, devtool `attempts` / `last_error`, `Retry-After`. Transient errors: retry without a limit; permanent ones (bad config): return the error.
- Example app: `examples/04-async-modules`.

### Built-in Modules (`modules/`)

Each has a README in its folder.

- `modules/config`: strict `.env` + OS env loader with typed struct binding (`bind:"NAME"` tags); inject `ConfigService` (safe for concurrent `Get` / `Set`, never prints a value).
- `modules/cache`: key-value cache with TTL and an atomic `Incr` (default backend `cache.NewMemoryCache(nil)`, implemented by `internal/memorycache`: bounded, 64 shards, CLOCK eviction, no goroutine while idle; other bounds with `cache.NewMemoryCache(&cache.MemoryCacheOptions{…})`); inject `CacheService`.

There is no HTTP client or persistence module: outbound calls use `net/http` with `owlky.TraceTransport`
(`internal/trace/README.md` › Outbound calls).

### WebSocket

Controllers embed `owlky.WS` (alongside or instead of `owlky.HTTP`). Handler names start with `SUBSCRIBE` and map to event names (e.g. `SUBSCRIBE_chat_ANY` → `chat.*`, `SUBSCRIBE_chat_ALL` → `chat.**`); enable the endpoint with `WS: &owlky.WSOptions{Path: "ws"}` in `AppOptions`; the handshake runs the loadshed, proxy and hostcheck stages that are on, the Origin check (an `Origin` that is neither same-origin nor in `Origins` gets 403), then the `BindGlobalMiddlewares`. The framework handles the `websocket.Conn` lifecycle; handlers receive `owlky.WSPayload` for incoming events and fan out through `owlky.Publisher`.

### Bootstrap-time Error Convention

Build/config failures (route conflicts, unresolved dependencies, invalid handlers) in `internal/core/`, `internal/common/`, `internal/routing/` are reported via `panic()`, not returned as `error` — there is no error-return API (`Build()` returns the module, `Create()` returns nothing; neither returns an `error`). This only fires during `app.Create()`'s module/controller wiring (or a configuration call made after it), never on the request path, so an uncaught panic there is fail-fast by design.

Message format is `"<category>: <detail>"`:
- `"route conflict: ..."` (`internal/core/container.go`, per app: the same method, version and route shape; routes that differ only in parameter names conflict; `SERVE_static_ANY` and `SERVE_ANY_OF_static` are the same route, `/static/*`)
- `"event conflict: ..."` (`internal/core/container.go`, per app)
- `"invalid handler: ..."` (`internal/routing/router.go`; `internal/core/container.go`: a `SUBMIT_` without the `RENDER_` of the same words in its controller)
- `"invalid route: ..."` (`internal/routing/router.go`: a partial parameter segment such as `/files/{name}.json`, a literal `$` segment, or more than 64 segments; `internal/common`: an upper-case word that is not a route word, `BY` / `AND` without a name, a word after the parameters, `OF` with nothing on one side, `FILE` without a name and an extension, `ALL` not last, `ANY`, `VERSION` or `FILE` in a page route name, a `Path` without a handler, bound to a non-route, set twice or holding `?` / `#`, `Path` or `Prefix` bound to a `SUBMIT_`; `internal/core/container.go`: a page route whose path holds a wildcard)
- `"invalid render: ..."` (`internal/core/app.go`, `container.go`, `http_page.go`: a `Render.FS` without its `Dir` directory (at `New`), a `RENDER_` / `SUBMIT_` without `Render.FS`, a page route with no template, two templates, a reserved directory or an error page name, an `.html` file without a route, an unusable error page name, a CSS or JS file that belongs to no template, a page set with such files and no `{{pageAssets}}`, `assets/_pages`, a template that fails to parse, calls a missing template or fails to escape, with its file path)
- `"invalid module: ..."` (`internal/core/module_builder.go`: an import that is not a module or a module factory, one entry in both `Imports` and `AsyncImports`, `PathStyle` with another value than the two constants; `Create(nil)` in `internal/core/app.go`; `internal/core/module_factory.go`: a module factory that failed, panicked, returned a nil module, did not return within the startup timeout, or is in `Imports` and needs an async module; `internal/core/module_state.go`: an entry imported with both lists by two modules, async modules that need each other, a probed `NewController` that panicked)
- `"invalid app: ..."` (`internal/core/options.go`, at `New`: every bad `AppOptions` value, as `<Group>.<Field> …` (design 0022 A3), and the errors `OptionsFromEnv` recorded, `invalid app: <n> environment errors:` with one per line; `internal/core/app.go`: `Create` called twice, a `BindGlobal…` call after `Create`)
- `"invalid retry: ..."` (`internal/core/retry.go`: `Retry` without a function; called by module code, not only at `Create()`)
- `"dependency injection: ..."` (`internal/core/fn.go`, `internal/core/fn_plan.go`, `internal/core/container.go`; `internal/core/module_factory.go`: factories that can't be ordered; `internal/core/module_state.go`: an app-wide layer that injects an async provider)
- `"invalid config: ..."` (`modules/config`: a bad `.env` file or line with its path and line number, never a value; contradicting options; too deeply nested `Loads`; `Set` on a zero `ConfigService`)

These are wrapped in `color.FmtRed(...)` since they only ever surface on a developer's terminal at startup. If an `err` you're panicking already came from a function that applies `color.FmtRed`, just `panic(err)` — don't re-wrap, or the ANSI codes nest and garble the output. An unresolvable handler argument is a `Create()` panic too; the only per-request panic of injection is a pipe whose `Transform` returns a wrong value, an uncolored `exception.InternalServerErrorException` that `internal/core/http.go`'s `recover()` turns into a 500.
