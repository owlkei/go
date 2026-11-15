# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make test          # run all tests with coverage
make lint          # run golangci-lint
make deps          # install go + yarn dependencies
make tidy          # go mod tidy
make protoc        # regenerate protobuf from devtool/*.proto

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
no comments in Go code; snake-case files with the transport first (`http_guard.go`); short
receivers (`instance` for user-facing option types); `Is…` option booleans; `<values>By<Key>` maps;
`load<X>Options` normalizes once; `Err<Reason>` sentinels with a `"<pkg>: "` prefix; internal test
packages, `Test<Subject>_<Behavior>`, one function per behavior, `test.DiffMessage`.

## Workflow

The user approves **plans and designs**; Claude does the rest. Outside a roadmap run Claude commits only
when asked (`/commit`); during `/roadmap run` (roadmap and designs approved) it stages and commits its
own work on a `roadmap/<slug>` branch. Claude never pushes, merges or tags.

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
- Slash commands (`.claude/commands/`): `/roadmap`, `/feature`, `/implement`, `/refactor [optimize]`,
  `/doc <package>` (README), `/kb [update|audit|readiness]` (this file + `.ginject/`), `/check-staged`,
  `/commit`.
- Every package that work adds or changes code in gets a `README.md` if it has none (structure of
  `modules/config/README.md`, written by `/doc`'s steps); `sample/**` is exempt.
- `make check` runs what CI runs (gofmt, `go vet`, `golangci-lint` with `.golangci.yml`, `go test -race`).
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

Agents run one after another in the same working tree; parallel work needs one git worktree each.
Formats: `docs/roadmap/README.md`, `docs/design/README.md`.

## Architecture

Ginject is a NestJS-inspired dependency injection web framework for Go. The core pattern is **Module → Controller → Provider**.

### DI Container (`core/`)

- **Provider**: any struct implementing `NewProvider() Provider`. Providers are injected into other providers and controllers via struct field types (reflection-based, no tags needed).
- **Controller**: any struct implementing `NewController() Controller`. Must embed `common.HTTP` (HTTP) and/or `common.WS` (WebSocket) to register routes. There is no `common.REST`.
- **Module**: built with `core.ModuleBuilder().Imports(...).Controllers(...).Providers(...).Build()`. Modules compose the app tree. Set `IsGlobal: true` to make providers available everywhere.
- **App**: created via `core.New()`, wired by `app.Create(rootModule)`, started with `app.Listen(port)`.

### REST Route Convention

Controller method names are parsed into HTTP routes — no annotations needed. Naming tokens: `READ`→GET, `CREATE`→POST, `UPDATE`→PUT, `MODIFY`→PATCH, `DELETE`→DELETE, `PREFLIGHT`→OPTIONS. Separator tokens: `BY` (path param), `AND` (additional segment), `OF` (sub-resource), `ANY` (wildcard), `VERSION` (version tag), `FILE` (file serving).

Example: `READ_BY_ID_AND_NAME` → `GET /:id/:name`

Versioning adds a `_VERSION_X` suffix: `READ_VERSION_1` → version 1 of that route.

### Request Pipeline (per route)

```
GlobalMiddlewares → ModuleMiddlewares → Guards → Interceptors → MainHandler → ExceptionFilters
```

Each layer is bound with `BindMiddleware(fn, handlers...)`, `BindGuard(fn, handlers...)`, etc. on the `common.Middleware`, `common.Guard`, `common.Interceptor`, `common.ExceptionFilter` embedded fields of a controller.

App-wide bindings: `app.BindGlobalMiddlewares`, `BindGlobalGuards`, `BindGlobalInterceptors`, `BindGlobalExceptionFilters`.

Contracts:
- Middleware is a real interface, `common.MiddlewareFn`: `Use(*http.Request, http.ResponseWriter, ctx.Next)`.
- Guards, interceptors and exception filters are **not** interfaces: `common.Guarder`, `Interceptable`
  and `ExceptionFilterable` are `any`. The framework looks the method up by name with reflection
  (`GuardMethodName = "CanActivate"`, `"Intercept"`, `"Catch"`) and checks its signature, which differs
  for HTTP and WS (`common/http_*.go`, `common/ws_*.go`); a wrong shape panics at startup
  (`GuardShapeError` and friends):
  - HTTP: `CanActivate(*ctx.HTTPContext) bool` · `Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any` · `Catch(*ctx.HTTPContext, *exception.Exception)`
  - WS: the same with `*ctx.WSContext`.
  - Note the `Catch` argument order: context first, exception second.

### Handler Injection

Handler method parameters are injected by type — declare them in the handler signature and the framework resolves them from the request context. The allowed sets are `knownHTTPDependencyKeys` / `knownWSDependencyKeys` in `core/app.go`:

- HTTP: `*ctx.HTTPContext`, `*http.Request`, `http.ResponseWriter`, `ctx.Body`, `ctx.Form`, `ctx.Query`, `ctx.Header`, `ctx.Param`, `ctx.File`, `ctx.Next`, `ctx.Redirect`, `common.Publisher`, and the `*Pipeable` types of `common/pipe.go` (a struct with `Transform`).
- WS: `*ctx.WSContext`, `*websocket.Conn`, `ctx.WSPayload`, `ctx.Next`, `common.Publisher`, `common.WSPayloadPipeable`.

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

Build/config failures (route conflicts, unresolved dependencies, invalid handlers) in `core/`, `common/`, `routing/` are reported via `panic()`, not returned as `error` — there is no error-return API (`Build()` returns the module, `Create()` returns nothing; neither returns an `error`). This only fires during `app.Create()`'s module/controller wiring, never on the request path, so an uncaught panic there is fail-fast by design.

Message format is `"<category>: <detail>"`:
- `"route conflict: ..."` (`common/rest.go`)
- `"event conflict: ..."` (`common/ws.go`)
- `"invalid handler: ..."` (`routing/router.go`)
- `"invalid module: ..."` (`core/module_builder.go`)
- `"dependency injection: ..."` (`core/fn.go`)

These are wrapped in `color.FmtRed(...)` since they only ever surface on a developer's terminal at startup. If an `err` you're panicking already came from a function that applies `color.FmtRed`, just `panic(err)` — don't re-wrap, or the ANSI codes nest and garble the output. The one exception is `invokeHandlerByProviders` in `core/fn.go`, which panics during per-request handling and is caught by `core/http.go`'s `recover()` — it stays a plain, uncolored `fmt.Errorf`.
