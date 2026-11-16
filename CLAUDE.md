# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make test          # run all tests with coverage
make lint          # run golangci-lint
make deps          # install go + yarn dependencies
make tidy          # go mod tidy

# run a single test
go test ./routing/... -run TestRouter -v

# run the sample app
cd sample && go run main.go
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
- `make check` runs what CI runs (gofmt, `go vet`, `golangci-lint` with `.golangci.yml`, `go test -race`);
  CI (`.github/workflows/ci.yml`) also runs `govulncheck` on every push, pull request and weekly.
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

### DI Container (`core/`)

- **Provider**: any struct implementing `NewProvider() Provider`. Providers are injected into other providers and controllers via struct field types (reflection-based, no tags needed).
- **Controller**: any struct implementing `NewController() Controller`. Must embed `common.HTTP` (HTTP) and/or `common.WS` (WebSocket) to register routes. There is no `common.REST`.
- **Module**: built with `core.ModuleBuilder().Imports(...).Controllers(...).Providers(...).Build()`. Modules compose the app tree. Set the built module's `IsGlobal` field to `true` to make its providers available everywhere; hooks are the `OnInit`, `OnReady` and `OnShutdown` fields (`core/README.md` › Modules and lifecycle).
- **App**: created via `core.New()`, wired by `app.Create(rootModule)`, started with `app.Listen(port, nil)` (or `ListenTLS` / `Serve`; options in `core.ServerOptions`, `core/README.md` › Server).

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
their code and message; anything else answers a fixed 500 and is logged.

Each layer is bound with `BindMiddleware(fn, handlers...)`, `BindGuard(fn, handlers...)`, etc. on the `common.Middleware`, `common.Guard`, `common.Interceptor`, `common.ExceptionFilter` embedded fields of a controller.

App-wide bindings: `app.BindGlobalMiddlewares`, `BindGlobalGuards`, `BindGlobalInterceptors`, `BindGlobalExceptionFilters`.

Contracts:
- Middleware is a real interface, `common.MiddlewareFn`: `Use(*http.Request, http.ResponseWriter, ctx.Next)`.
- Guards, interceptors and exception filters are **not** interfaces: `common.Guarder`, `Interceptable`
  and `ExceptionFilterable` are `any`. The framework matches the method by name and signature
  (`GuardMethodName = "CanActivate"`, `"Intercept"`, `"Catch"`) through an unexported interface, which
  differs for HTTP and WS (`common/http_*.go`, `common/ws_*.go`); a wrong shape panics at startup
  (`GuardShapeError` and friends), and so does a layer bound to a method that is not a handler of the
  controller (`invalid guard: … is bound to '<name>', which is not a handler of the controller`):
  - HTTP: `CanActivate(*ctx.HTTPContext) bool` · `Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any` · `Catch(*ctx.HTTPContext, *exception.Exception)`
  - WS: the same with `*ctx.WSContext`.
  - Note the `Catch` argument order: context first, exception second.

### Handler Injection

Handler method parameters are injected by type (type identity, not the printed name) — declare them in the handler signature and the framework resolves them from the request context. Each handler gets an argument plan once at `Create()` (`newHTTPHandlerPlan` / `newWSHandlerPlan` in `core/fn_plan.go`, design 0006); a request only runs it. The allowed sets (table in `core/README.md` › Handler injection):

- HTTP: `*ctx.HTTPContext`, `*http.Request`, `http.ResponseWriter`, `ctx.Body`, `ctx.Form`, `ctx.Query`, `ctx.Header`, `ctx.Param`, `ctx.File`, `ctx.Next`, `ctx.Redirect`, `common.Publisher`, and value types implementing the HTTP `*Pipeable` interfaces of `common/pipe.go` (a type with `Transform`; a pointer pipe parameter is refused).
- WS: `*ctx.WSContext`, `*websocket.Conn`, `ctx.WSPayload`, `ctx.Next`, `common.Publisher`, value types implementing `common.WSPayloadPipeable`.

Anything else panics at `Create()` with `"dependency injection: ..."`.

All types are re-exported from the root `ginject` package (`aliases.go`).

### Modules: Static vs Dynamic

- **Static modules**: `var Module = func() *core.Module { return core.ModuleBuilder()....Build() }` — pass the func itself to `Imports(...)`; the framework builds it once.
- **Dynamic modules**: a package-level `Register(opts *<X>ModuleOptions) *core.Module` that returns a configured module (every built-in module, e.g. `config.Register(&config.ConfigModuleOptions{...})`).

### Built-in Modules (`modules/`)

Each has a README in its folder.

- `modules/config`: `.env` + OS env loader with typed struct binding (`bind:"NAME"` tags); inject `ConfigService`.
- `modules/cache`: thread-safe in-memory cache with TTL (backed by `memorycache/`, sharded); inject `CacheService`.
- `modules/httpclient`: outbound HTTP client with retries, downloads, SSE and middleware.
- `modules/storage`: embedded document store (models, queries, indexes, transactions).

### WebSocket

Controllers embed `common.WS` (alongside or instead of `common.HTTP`). Handler names start with `SUBSCRIBE` and map to event names (e.g. `SUBSCRIBE_chat_PERSON_ANY`); enable the endpoint with `app.EnableWS(&core.WSConfig{Path: "ws"}, middlewares...)`. The framework handles the `websocket.Conn` lifecycle; handlers receive `ctx.WSPayload` for incoming events and fan out through `common.Publisher`.

### Bootstrap-time Error Convention

Build/config failures (route conflicts, unresolved dependencies, invalid handlers) in `core/`, `common/`, `routing/` are reported via `panic()`, not returned as `error` — there is no error-return API (`Build()` returns the module, `Create()` returns nothing; neither returns an `error`). This only fires during `app.Create()`'s module/controller wiring (or a configuration call made after it), never on the request path, so an uncaught panic there is fail-fast by design.

Message format is `"<category>: <detail>"`:
- `"route conflict: ..."` (`core/container.go`, per app: the same method, version and route shape; routes that differ only in parameter names conflict; `SERVE_static_ANY` and `SERVE_ANY_OF_static` are the same route, `/static/*`)
- `"event conflict: ..."` (`core/container.go`, per app)
- `"invalid handler: ..."` (`routing/router.go`)
- `"invalid route: ..."` (`routing/router.go`: a partial parameter segment such as `/files/{name}.json`, a literal `$` segment, or more than 64 segments)
- `"invalid module: ..."` (`core/module_builder.go`; `Create(nil)` in `core/app.go`)
- `"invalid app: ..."` (`core/app.go`: `Create` called twice, a configuration method or `SetLogger` after `Create`, `SetLogger(nil)`)
- `"dependency injection: ..."` (`core/fn.go`, `core/fn_plan.go`, `core/container.go`)

These are wrapped in `color.FmtRed(...)` since they only ever surface on a developer's terminal at startup. If an `err` you're panicking already came from a function that applies `color.FmtRed`, just `panic(err)` — don't re-wrap, or the ANSI codes nest and garble the output. An unresolvable handler argument is a `Create()` panic too; the only per-request panic of injection is a pipe whose `Transform` returns a wrong value, an uncolored `exception.InternalServerErrorException` that `core/http.go`'s `recover()` turns into a 500.
