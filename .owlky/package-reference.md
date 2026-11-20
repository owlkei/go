# Package Reference & Public APIs

**Optimization**: Structured reference, minimal prose, organized by package.

**Verification policy**: the `core`, `log`, `trace` and `accesslog` signatures below were checked against source on 2026-11-16 (roadmap 0003; logger and access log signatures again for design 0012), the rest on 2026-09-25. If you (an AI agent) are about to generate code against an API in this file, and this file is more than a few weeks old relative to the repo, spot-check the signature with a grep before trusting it — this file has previously contained fabricated APIs that looked exactly as confident as everything else in it.

---

## Public surface (design 0017)

`api/owlky.txt` is the exact public API (`make api` writes it, `make api-check` diffs it in `make check`
and CI). An application imports only these paths; every other package is under `internal/`:

| Import path | Holds |
|---|---|
| `go.owlky.com/owlky` | `New`, `ModuleBuilder`, `TraceTransport`; aliases `App`, `Module`, `Controller`, `Provider`, `ServerOptions`, `WSOptions`, `RenderOptions`, `HTTP`, `WS`, `Guard`, `Interceptor`, `ExceptionFilter`, `Middleware`, `MiddlewareFn`, `ArgumentMetadata`, `Publisher`, `HTTPContext`, `WSContext`, `Body`, `Form`, `File`, `Query`, `Header`, `Param`, `WSPayload`, `Map`, `Next`, `Redirect`, `FieldLevel`, `DataFile`, `Aggregation`, `VersioningOptions`, `VersioningType`, `PathStyle`; constants `VersioningQuery`, `VersioningHeader`, `VersioningCustom`, `VersioningMediaType`, `PathStyleKebab`, `PathStyleAsWritten` |
| `…/exception` | `Exception` (`Code`, `Message`, `StatusText`, `Error`, `Unwrap`, `AppendJSON`, `MarshalJSON`), `ExceptionOptions`, `NewException`, the 31 `<Status>Exception` constructors |
| `…/log` | `NewLog`, `WrapLogger`, `LogOptions{Level, Format, IsAddSource, MaskFields}`, `Logger`, `LogAdapter`, `Format`, `LevelDebug`…`LevelFatal`, `FormatText`/`FormatJSON`/`FormatPretty` |
| `…/modules/cache` | `Register`, `CacheModuleOptions`, `CacheService`, `Cache`, `CacheOnInitFn`, `NewMemoryCache`, `MemoryCacheOptions`, `ErrEmptyKey`, `ErrNegativeTTL`, `ErrEntryTooLarge`, `ErrNotCounter` |
| `…/modules/config` | `Register`, `ConfigModuleOptions` (`EnvFilePaths`…), `ConfigService`, `ConfigLoadFn`, `ConfigHookFn`, `ConfigOnInitFn` |
| `…/guards/throttler` | `Throttler`, `Strategy`, `StrategyFixedWindow`, `StrategySlidingWindow`, `StrategyTokenBucket`; `NewGuard() any` |

- Root aliases point at internal types: `HTTPContext = *ctx.HTTPContext`, `Aggregation = *aggregation.Aggregation`,
  `App = *core.App`, `Module = *core.Module`, `DataFile = *ctx.DataFile` are pointer aliases; the rest are plain.
  `reflect` still prints the internal names (`*ctx.HTTPContext`), so injection panics show them; the six
  layer-shape panics spell the types as users write them (`func(owlky.HTTPContext) bool`).
- Hook rule (D3): what the framework needs from a user-visible type is a package function of its internal
  package (`ctx.HTTPReset(c)`, `aggregation.SetMainData(a, d)`), never an exported method or field, because
  members stay reachable through an alias.
- Public packages (`exception`, `log`, `middlewares/*`, `modules/*`, `guards/*`) import the engine by its
  internal path, never the root package (D6).
- Runtime errors read `owlky: …` (was `core:` / `ctx:`) and `cache: …` (was `memorycache:`).
- The sections below are per package; the engine packages are documented by their internal name.

---

## Package: `core` (`internal/core`)

**Responsibility**: Application lifecycle, handler invocation, DI container

### App

```go
type App struct {
    // unexported fields only: router, container, tracer, broker, logger, server state
}
```

Every `core.New(opts)` is independent (own routes, providers, layers, logger, publisher). The whole
configuration is the `*AppOptions` given to `New` (design 0022); the logger is `AppOptions.Logger`, read with `Logger()`; `App` has no exported fields.

**Public Methods** (all chainable, return `*App`, unless noted):

| Method | Purpose |
|--------|---------|
| `New(opts *AppOptions) *App` | Create an app from its options (`nil` = defaults); reads `opts` once, copies what it keeps, and makes every check (panics `invalid app: <Group>.<Field> …`); compiles the pre-route stages in their fixed order |
| `OptionsFromEnv() *AppOptions` (package function) | The same struct, every pointer group non-nil, filled from `OK_<PATH>` variables (`__` between levels, upper snake case), `PORT` (when `OK_SERVER__PORT` is unset) and `./.env` (the environment wins; code written after the call wins); never panics: `New` panics once with every recorded error (`invalid app: <n> environment errors:`) and logs one `Info AppOption` line per non-zero field with its source |
| `Create(m *Module)` | Initialize app with module tree — call exactly once |
| `Listen() error` | Start the server on `:<Server.Port>` (3000 when 0), HTTPS with HTTP/2 when `Server.TLS` holds a certificate, `h2c` with `Server.IsH2C`; blocks until shutdown |
| `Serve(l net.Listener) error` | Serve on the caller's listener with the same TLS and protocol rules; ignores `Server.Port` (logs `Info ServerPortIgnored` when it is set) |
| `Stop()` | Graceful shutdown of a serving app (drain, then `OnShutdown` hooks once); on an app that is not serving, runs the hooks and marks it stopped; blocks until the hooks finished |
| `Get(p Provider) any` | Look up an already-injected provider by type |
| `ServeHTTP(w, r)` | HTTP handler (auto-called by Go stdlib) |
| `BindGlobalMiddlewares(fn ...common.MiddlewareFn) *App` | Register global middleware |
| `BindGlobalGuards(g ...any) *App` | Register global guards |
| `BindGlobalInterceptors(i ...any) *App` | Register global interceptors |
| `BindGlobalExceptionFilters(f ...any) *App` | Register global exception filters |
| `AppOptions.WS` (`*WSOptions{Path}`) | WebSocket support, on when `Path` is set; the handshake runs the `loadshed`, `proxy` and `hostcheck` stages that are on, the `origin` check (not same-origin and not in `Origins` → `403 Origin not allowed`), then the `BindGlobalMiddlewares` |
| `AppOptions.Render` (`*RenderOptions`) | Server-rendered pages: `RENDER_` / `SUBMIT_` routes of `common.HTTP` controllers (design 0018 amendment 2). `RenderOptions{FS fs.FS; Dir string; IsReload bool}`: on when `FS` is set (`Dir` or `IsReload` without it → `invalid app: Render.<Field> is set without Render.FS`), `Dir` the directory inside it (default `web`, `.` the root; missing → `invalid render: RenderOptions.FS has no "<dir>" directory…`), `IsReload` on an `embed.FS` reads `Dir` from disk and logs `Warn RenderReloadOn` at `Create()`. `Create()` parses every template once (`invalid render: <file>: <error>`), binds each page route to `<path>.html` / `<path>/index.html` (panics: no template, two templates, reserved directory, error page name, an `.html` without a route), adds `/assets/*` when `Dir/assets` or a page CSS / JS file exists and one route per `Dir/public` file. File conventions, error pages by status and the four template functions (`nonce`, `asset`, `dict`, `pageAssets`): `internal/render/README.md` |
| `AppOptions.IsDevtool` | Build the devtool snapshot at `Create()` (routes, layers, request schemas, the `pre_route` stages with their `Snapshot()`; no transport yet) |
| `AppOptions.IsAccessLog` | Enable access logging |
| `AppOptions.Versioning` (`*versioning.Options`) | API versioning, on when `Type` is set; the options are copied |
| `AppOptions.Logger` (`log.LogAdapter`) | The app's logger (wrapped with `log.WrapLogger(logger, nil)`); configure the built-in one with `Logger: log.NewLog(&log.LogOptions{…})` |
| `Logger() log.Logger` | The app's logger; never `nil` (the built-in logger by default) |
| `AppOptions.StartupTimeout` | The budget of `Create()` for the module factories of `Imports` (default 30 s, counted from the start of `Create()`, the deadline of a factory's `ctx`); negative panics `invalid app: StartupTimeout is <v>, it must be positive` |

**There is no `SetMaxBodySize`/`SetMaxRequestBodySize` method anywhere.** The request body cap is `AppOptions.HTTP.MaxBodyBytes` (a `ByteSize`: 0 = 10 MiB, negative = no cap), stored at `New` in the unexported plain `int64` `maxBodyBytes` and applied in `ServeHTTP` to every request with a body, also when `App` runs in the user's own `http.Server` — see "HTTP Server Defaults" below.

### HTTP Server Defaults

`internal/core/server.go`; the only options are the four fields of `ServerOptions` (`Port` 0 = 3000, `ShutdownTimeout` 0 = 30 s, `TLS{CertFile, KeyFile, Config}`, `IsH2C`) and `HTTPOptions.MaxBodyBytes`. The rest is internal (`serverDefaults`, set by `New()`), with no public setter and no exported constant:

| Setting | Value |
|----------|-------|
| `ReadHeaderTimeout` | 5 s |
| `ReadTimeout` | 30 s |
| `WriteTimeout` | 30 s (a handler extends it with `http.NewResponseController(w).SetWriteDeadline`) |
| `IdleTimeout` | 75 s |
| `MaxHeaderBytes` | 64 KiB (431 beyond) |

Exceeding the body cap surfaces as a 413 `exception.RequestEntityTooLargeException` from `ctx.Body()`, not a raw error. These timeouts bound the connection, not handler execution time — there is no per-handler deadline (see `ctx` section). Full rules: `internal/core/README.md` › Server.

### Module

```go
type Module struct {
    id       string
    Name     string
    prefixes []string

    imports        []any
    staticModules  []*Module
    moduleFactories []any
    providers      []Provider
    controllers    []Controller

    IsGlobal   bool
    OnInit     func()
    OnReady    func()
    OnShutdown func()
}
```

**Methods**: `Prefix(prefix string) *Module`, `ID() string`.

**Lifecycle hooks** (once per app, `internal/core/container.go`): modules are initialized depth first, imports before the module, in `Imports(…)` order, each once (root → A, C; A, C → B gives B, A, C, root). Initializing a module calls `OnInit`, then constructs its providers. `OnReady` runs at the end of `Create` in the same order; `OnShutdown` runs in the exact reverse (root, C, A, B) on SIGINT/SIGTERM or `app.Stop()`, guarded so it runs exactly once, and never for an app whose `Create` did not finish. Configuration methods after `Create`, a second `Create` and `Create(nil)` panic (`invalid app: …` / `invalid module: …`).

### ModuleBuilder

The root package exports the constructor `owlky.ModuleBuilder()` (`core.NewModuleBuilder()`); it returns `*core.ModuleBuilder`, which has no root alias:

```go
func ModuleBuilder() *core.ModuleBuilder
```

| Method | Signature | Notes |
|--------|-----------|-------|
| `Imports` | `Imports(modules ...any) *ModuleBuilder` | Takes `...any`, not `...*Module`: a `*Module` or a module factory (`func(deps...) *Module`, `func(deps...) (*Module, error)`, `func(ctx, deps...) (*Module, error)`); fails fast at `Create()` |
| `AsyncImports` | `AsyncImports(modules ...any) *ModuleBuilder` | Same entries, built in the background; the modules that inject their providers are `pending` and their routes answer `503` until ready (README › Module lifecycle). One entry in both lists panics |
| `Providers` | `Providers(providers ...Provider) *ModuleBuilder` | |
| `Controllers` | `Controllers(controllers ...Controller) *ModuleBuilder` | |
| `PathStyle` | `PathStyle(style common.PathStyle) *ModuleBuilder` | `owlky.PathStyleKebab` (default, `READ_user_profile` → `/user-profile`) or `owlky.PathStyleAsWritten` (`/user_profile`); inherited by imports like `Prefix`, an explicit setting wins; another value panics `invalid module: PathStyle needs owlky.PathStyleKebab or owlky.PathStyleAsWritten` |
| `Build` | `Build() *Module` | |

**There is no `Export(...string)` method and no `IsGlobal(bool)` builder method — both are fabrications from an earlier bad doc pass.** `IsGlobal` is a public **field** set directly on the built `*Module` (`module.IsGlobal = true`), not chained on the builder.

### Retry

```go
type RetryOptions struct { InitialDelay, MaxDelay time.Duration; MaxAttempts int }  // root alias owlky.RetryOptions
func Retry(ctx context.Context, opts *RetryOptions, fn func(ctx context.Context) error) error  // owlky.Retry
```

Backoff with jitter (`InitialDelay` 500 ms doubling up to `MaxDelay` 30 s, each wait random in `[d/2, d]`)
until `fn` returns `nil`, `ctx` is done (`owlky: retry stopped after <n> attempts: …`) or `MaxAttempts`
is reached (`owlky: retry gave up after <n> attempts: …`); a nil `fn` panics `invalid retry: Retry needs a function`.
With an `AsyncImports` factory's `ctx` it reports attempts (`ModuleRetry`, devtool, `Retry-After`).

### Provider / Controller

```go
type Provider interface {
    NewProvider() Provider
}

type Controller interface {
    NewController() Controller
}
```

Any struct implementing the respective method qualifies — no tags, no registration call needed beyond passing the value to `ModuleBuilder().Providers(...)` / `.Controllers(...)`.

---

## Package: `ctx` (`internal/ctx`)

**Responsibility**: Request context objects and dependency types

`ctx.Context` is `interface { ID() string; Context() context.Context }` (no `SetID`: the ID comes
from the trace scope built in `ctx.HTTPInit`, design 0011).

### HTTPContext

```go
type HTTPContext struct {
    *http.Request
    http.ResponseWriter

    // only exported fields; next, code (defaults to http.StatusOK), timestamp, body, form, file,
    // query, header, param, paramKeys, paramValues are unexported and reached by ctx.HTTP… hooks
}
```

Framework hooks (package functions, design 0017 D3): `HTTPInit(c, w, r)`, `HTTPReset(c)`,
`HTTPSetParams(c, keys, values)`, `HTTPParamValues(c)`, `HTTPNext(c)` / `HTTPSetNext(c, next)`,
`HTTPCode(c)` / `HTTPSetCode(c, code)`, `HTTPTimestamp(c) *time.Time`, `HTTPAddAggregation(c, a)`,
`HTTPAggregations(c)`; WS: `WSInit(c, conn)`, `WSReset(c)`, `WSNext` / `WSSetNext`, `WSTimestamp`,
`WSSetSend`, `WSSetPayload`, `WSAddAggregation`, `WSAggregations`.

**No `Deadline` field, no `SetDeadline()`, no `IsDeadlineExceeded()` method exist.** There is no per-request timeout API anywhere in this framework — only the server-level timeouts in `core.App`'s defaults table above.

| Method | Signature | Notes |
|--------|-----------|-------|
| `ctx.HTTPInit` | `HTTPInit(c, w http.ResponseWriter, r *http.Request)` | A hook, not a method. Builds the trace scope and sets `c.Request = r.WithContext(<scope ctx>)`: `c.Request != r` afterwards |
| `ctx.HTTPReset` | `HTTPReset(c)` | A hook. Clears all fields for pool reuse |
| `Status` | `Status(code int) *HTTPContext` | Sets the status code; chainable — call BEFORE `JSON`/`Text`, since they write using whatever code is current |
| `Text` | `Text(data string, args ...any)` | Formats only when `args` are given |
| `JSON` | `JSON(data any, args ...any)` | A `string` is raw JSON (formatted only with `args`); a `JSONAppender` writes its own bytes; anything else goes through `encoding/json`. An encoding error panics with a 500 `exception.Exception` and writes nothing |
| `JSONP` | `JSONP(data any, args ...any)` | Callback from the cached `Query()`, reduced to `[A-Za-z0-9_$.[]]` and 128 bytes; falls back to `JSON` when nothing is left; sends `X-Content-Type-Options: nosniff` |
| `Redirect` | `Redirect(url string)` | Uses the status code when it is 3xx, else 301 |
| `ID` | `ID() string` | The scope's `Request`: 16 lowercase hex characters (`crypto/rand`), `""` before `HTTPInit` / after `HTTPReset`; never from a header |
| `Context` | `Context() context.Context` (promoted from `*http.Request`) | Carries the trace scope (`tracecontext.ScopeOf`); outlives the request |
| `Body` | `Body() Body` | Lazily parses a JSON body (`application/json` or `application/*+json`, case-insensitive), never `nil`; 400 `Invalid JSON body` / `Cannot read request body`, 413 on `*http.MaxBytesError` |
| `Form` / `File` | `Form() Form` / `File() File` | One multipart parse shared by both; never `nil`; 400 `Invalid form body`, 413 over the limit; temp files removed after the request (`httpExchange.reset`) |
| `ctx.HTTPAddAggregation` / `HTTPAggregations` | `HTTPAddAggregation(c, a *aggregation.Aggregation)` / `HTTPAggregations(c) []*aggregation.Aggregation` | Hooks. Per-request list, cleared by `HTTPReset` (`WSAddAggregation` / `WSAggregations` on `WSContext`) |

Removed: the writer types `ctx.DataWriter`, `ctx.JSON`, `ctx.JSONP`, `ctx.Text` (`internal/ctx/writer.go`).

### Binding

`ctx.BinderOf(reflect.Type) (*Binder, error)` builds the binding plan of a struct type once and caches
it per type (`sync.Map`); it returns `ctx.ErrBindTarget` for `nil` or a non-struct type. `Binder.StrArr`,
`Binder.Header`, `Binder.Struct` and `Binder.File` bind with that plan. `Query`, `Form`, `Header`,
`Param`, `Body` and `WSPayload` have `Bind(s) (any, []FieldLevel)` and `BindValue(s) any`, and
`BindStruct` / `BindStrArr` / `BindFile` are thin wrappers over the binder: they panic with an error
wrapping `ErrBindTarget` when `s` is not a struct value.

### WSContext

```go
type WSContext struct {
    *websocket.Conn
    // only exported field; next, timestamp, payload, send… are reached by ctx.WS… hooks
}
```

| Method | Signature |
|--------|-----------|
| `ctx.WSInit` | `WSInit(c, conn *websocket.Conn)` — a hook; builds the message scope from `conn.Request().Context()` |
| `ID` | `ID() string` — the message ID (`Scope.Message`), one per dispatch |
| `ctx.WSReset` | `WSReset(c)` — a hook |
| `WSPayload` | `WSPayload() WSPayload` — returns the map by value, NOT `*WSPayload` |
| `ctx.WSSetPayload` | `WSSetPayload(c, p WSPayload)` — a hook |
| `Send` | `Send(data any)` |
| `ctx.WSSetSend` | `WSSetSend(c, fn func(data any))` — a hook |
| `Context` / `SetContext` | `Context() context.Context` / `SetContext(ctx context.Context)` — the message context after `WSInit` (done when the connection's handler returns); `SetContext` keeps the message scope nearest, ignores `nil` |

`type WSPayload map[string]any` — a map type, not a struct.

### Built-in Dependency Types

Applications write them with the root aliases.

**HTTP-specific**: `owlky.HTTPContext`, `*http.Request`, `http.ResponseWriter`, `owlky.Body`, `owlky.Query`, `owlky.Header`, `owlky.Param`, `owlky.Form`, `owlky.File`, `owlky.Next`, `owlky.Redirect`, `owlky.Publisher`

**WebSocket-specific**: `owlky.WSContext`, `*websocket.Conn`, `owlky.WSPayload`, `owlky.Next`, `owlky.Publisher`

---

## Package: `common` (`internal/common`)

**Responsibility**: Pipeline shape-checking, embeddable controller building blocks

### Cross-Cutting Concern Types

**Only `MiddlewareFn` is a real, compile-time Go interface. Guards, interceptors and exception filters are bound as `any` (no named type) — checked at bind time by method name and signature** (a type assertion to an unexported interface, then the method value), not the compiler:

| Declared type | Real definition | Checked shape (func alias) | Bind-time check fails with |
|---|---|---|---|
| `MiddlewareFn` | `interface { Use(*http.Request, http.ResponseWriter, ctx.Next) }` | `Use = func(*http.Request, http.ResponseWriter, ctx.Next)` | compile error if no `Use` method |
| guard | `any` | `HTTPCanActivate = func(*ctx.HTTPContext) bool`, method name `"CanActivate"` | `GuardShapeError` |
| interceptor | `any` | `HTTPIntercept = func(*ctx.HTTPContext, *aggregation.Aggregation) any`, method name `"Intercept"` | `InterceptorShapeError` |
| exception filter | `any` | `HTTPCatch = func(*ctx.HTTPContext, *exception.Exception)`, method name `"Catch"` | `ExceptionFilterShapeError` |

**`Catch`'s parameter order is `(*ctx.HTTPContext, *exception.Exception)` — HTTPContext FIRST.** A common mistake (present in older docs and even in the project's own `CLAUDE.md`) is writing `Catch(*exception.Exception, *ctx.HTTPContext)`, which fails the shape check.

A bare func literal does **not** satisfy any of these — you must bind a struct value with the matching method. Example:
```go
type AuthGuard struct{}
func (g AuthGuard) CanActivate(httpCtx owlky.HTTPContext) bool { return isAuthorized(httpCtx) }
// c.BindGuard(AuthGuard{}, c.READ_BY_ID)
```

### `Publisher`

`Logger` and `LogAdapter` moved to `log` (design 0017); see the `log` section.

```go
type Publisher interface {
    Publish(topic string, payload ...any) error   // variadic payload, not a single `data any`
}
```

There is no `Log()`/`LogStructured()` method on `log.Logger` — it mirrors slog-style leveled logging.

### Embeddable Controller Types

**There is no `common.REST` type — this is a pervasive fabrication from earlier docs. The real embed is `common.HTTP`.** Four cross-cutting-concern types are embedded *separately* alongside it (or alongside `common.WS`), each with its own `Bind*` method:

```go
type UserController struct {
    common.HTTP              // required for HTTP routing
    common.Middleware        // optional: BindMiddleware(MiddlewareFn, handlers ...any) *Middleware
    common.Guard             // optional: BindGuard(any, handlers ...any) *Guard
    common.Interceptor       // optional: BindInterceptor(any, handlers ...any) *Interceptor
    common.ExceptionFilter   // optional: BindExceptionFilter(any, handlers ...any) *ExceptionFilter
}
```

`common.HTTP` has two methods: `Prefix(v string, handlers ...any) *HTTP` (a controller prefix, for every route or the given handlers; may hold `{param}` segments) and `Path(v string, handlers ...any) *HTTP` (an exact path replacing what the name spells; prefixes, method and version still apply; `invalid route:` panics for no handler, a non-route handler, two paths, `?` / `#`). There is no page embed (`common.Page` and `common.View` were removed, design 0018 amendment 2): `RENDER_<route>` (GET page) and `SUBMIT_<route>` (POST on the `RENDER_`'s path, same controller) are page routes of an HTTP controller; the handler's only or last result is the template's data. Default status 200 (`RENDER_`) / 422 (`SUBMIT_`); `c.Redirect` without a 3xx status → 303. A `SUBMIT_` without its `RENDER_` → `invalid handler: …`; `Path` / targeted `Prefix` on a `SUBMIT_` → `invalid route: …`.

`common.HTTP` itself only carries unexported routing bookkeeping, read by `core` through `common.HTTPPatternToFuncNameMap(h)`, `HTTPFuncNameToPatternMap(h)`, `HTTPRouterMap(h)` — it has no `Middleware`/`Guard`/`Interceptor`/`ExceptionFilter` fields; those are separate embeds. `common.WS` follows the same pattern for WebSocket controllers (`funcNameByEvent`, `eventMap` read by `common.WSEventMap(ws)`).

### One Instance Per Configuration (per app)

`core`'s container (`(*container).construct`, `internal/core/container.go`, passed to the `common.InjectProvidersInto…` methods as their `construct` argument) backs **every** bind of a Middleware/Guard/Interceptor/ExceptionFilter (global or per-controller, HTTP and WS). Instances are kept per app: two apps never share one. Its cache key is the bound value's `reflect.Type` plus the constructor name (`NewMiddleware`, `NewGuard`, `NewInterceptor`, `NewExceptionFilter`); under one key, values are compared with `reflect.DeepEqual` (a pointer is compared by a shallow snapshot of its pointee taken before the constructor runs).

- Equal configurations share: the constructor runs once per app and every equal bind gets the identical result. A rate limiter bound with the same options on two controllers stays one limiter.
- Different configurations get their own instance:
```go
// Controller A and B each get their own TenantHeader instance.
c.BindMiddleware(TenantHeader{Tenant: "a"})
c.BindMiddleware(TenantHeader{Tenant: "b"})
```
- Two types whose `Type().String()` is equal (`a/auth.Guard`, `b/auth.Guard`) never share; the same value built with two constructor names gets each constructor's own result.
- Without the constructor method, the result is the value given in the first call of that configuration.
- A value holding a non-nil func, or a different channel, is never deeply equal to another, so it is never shared.

### Binding targets

`Bind…(layer, handlers...)` with no handlers binds every route (HTTP) or event (WS) of the controller. With handlers, only those that are routes (`FuncNameToPatternMap`) or events (`ParseWSFuncNameToEvent`) are bound; a handler of the other transport binds nothing on this side; a handler given twice yields one item. A handler that is neither (a helper method, `nil`, a non-func) panics at `Create()`: `invalid guard: <LayerType> is bound to '<name>', which is not a handler of the controller` (`invalid middleware`, `invalid interceptor`, `invalid exception filter`).

---

## Package: `routing` (`internal/routing`)

**Responsibility**: URL routing and path matching

### Router

```go
const (
    MaxPathLength   = 4096 // bytes of the request path; longer paths never match
    MaxPathSegments = 64   // segments of a path or a route
)

type Router struct {
    // unexported: one trie (and a static-path map) per (method, version), the items in creation order
    GlobalMiddlewares  []ctx.HTTPHandler
    InjectableHandlers map[string]any // keyed by RouterItem.Pattern
}

func NewRouter() *Router
```

| Method | Signature |
|--------|-----------|
| `Match` | `Match(method, path, version string, params []string) (*RouterItem, []string)` — nil on a miss; the item is shared and read-only; the slice is `params` plus one value per route parameter, freshly allocated when there are values |
| `Add` | `Add(method, route, version string, handler ctx.HTTPHandler) *Router` |
| `AddInjectableHandler` | `AddInjectableHandler(method, route, version string, handler any) *Router` — also sets `RouterItem.Injectable` |
| `Use` | `Use(handlers ...ctx.HTTPHandler) *Router` — global middleware |
| `For` | `For(methods []string, route, version string) func(handlers ...ctx.HTTPHandler) *Router` |

There is no `Group` (removed in design 0002). `Add`, `For` and `AddInjectableHandler` panic with
`"invalid route: <route>: <reason>"` for a partial parameter segment (`/files/{name}.json`, `/a/{}`)
or more than 64 segments. Matching rules: `.owlky/routing-system.md` §2.

### RouterItem

```go
type RouterItem struct {
    Method       string
    Version      string
    Pattern      string            // MethodRouteVersionToPattern(method, route, version)
    Index        int               // dense, unique, in creation order
    HandlerIndex int
    Handlers     []ctx.HTTPHandler
    ParamKeys    map[string][]int  // name → positions in the parameter values
    ParamNames   []string          // ParamNames[i] names the i-th value; nil without parameters
    Injectable   any               // the handler given to AddInjectableHandler, else nil
}
```

### Naming Tokens

| Token | Meaning | Example |
|-------|---------|---------|
| `READ` | GET | `READ_BY_ID` → `GET /:id` |
| `CREATE` | POST | `CREATE` → `POST /` |
| `UPDATE` | PUT | `UPDATE_BY_ID` → `PUT /:id` |
| `MODIFY` | PATCH | `MODIFY_BY_ID` → `PATCH /:id` |
| `DELETE` | DELETE | `DELETE_BY_ID` → `DELETE /:id` |
| `PREFLIGHT` | OPTIONS | `PREFLIGHT` → `OPTIONS /` |
| `BY` | path param | `_BY_ID` → `/:id` |
| `AND` | additional segment | `_AND_NAME` → `/:name` |
| `OF` | sub-resource | `_OF_COMMENTS` → `/comments` |
| `ANY` | wildcard segment | `READ_ANY` → `GET /*` |
| `FILE` | file serving | |
| `VERSION_X` | API version | `_VERSION_2` → `/v2/...` |

---

## Package: `exception`

**Responsibility**: Exception types and error handling

```go
type Exception struct {
    // unexported: message, description, cause, code, isJoined; no error value is built at construction
}
```

No stack-trace field or method exists — `GetStackTrace()`/`SetStackTrace()` are fabrications; do not generate calls to them.

| Method | Signature |
|--------|-----------|
| `Error` | `Error() string` |
| `Unwrap` | `Unwrap() error` |
| `Code` | `Code() int` |
| `Message` | `Message() string` |
| `StatusText` | `StatusText() string` |
| `AppendJSON` | `AppendJSON(dst []byte) []byte`: appends `{"code":…,"error":…,"message":…}` (`encoding/json` of a fixed struct, same bytes as `json.Marshal` of the map); makes `Exception` and `*Exception` a `ctx.JSONAppender` |
| `MarshalJSON` | `MarshalJSON() ([]byte, error)`: the same body, so `json.Marshal(ex)` is no longer `{}` |

All constructors share the shape `func XException(message string, opts ...any) Exception`.

**HTTP constructors** (22 total): `BadRequestException`(400), `UnauthorizedException`(401), `ForbiddenException`(403), `NotFoundException`(404), `MethodNotAllowedException`(405), `NotAcceptableException`(406), `RequestTimeoutException`(408), `ConflictException`(409), `GoneException`(410), `PreconditionFailedException`(412), `RequestEntityTooLargeException`(413), `UnsupportedMediaTypeException`(415), `MisdirectedRequestException`(421), `UnprocessableEntityException`(422), `TeapotException`(418), `TooManyRequestsException`(429), `InternalServerErrorException`(500), `NotImplementedException`(501), `BadGatewayException`(502), `ServiceUnavailableException`(503), `GatewayTimeoutException`(504), `HTTPVersionNotSupportedException`(505).

**WS constructors**: `GoingAwayException`, `ProtocolErrorException`, `UnsupportedDataException`, `InvalidPayloadException`, `PolicyViolationException`, `MessageTooBigException`, `WSInternalErrorException`, `NotSubscribedException`, `TopicNotFoundException`.

`ExceptionOptions{Description, Cause}` is the `opts ...any` pattern for attaching a description or a wrapped cause. Only `opts[0]` is read: a non-empty `string` (the error text), an `error` (the cause, `Error()` is its text), `ExceptionOptions` or a non-nil `*ExceptionOptions` (`Error()` is `Description` or the status text, then `": " + Cause.Error()` with a cause). Any other value, `nil`, `""` and a nil `*ExceptionOptions` count as no option: `Error()` is the status text. `Unwrap()` is the cause or `nil`. Exceptions without a cause compare equal with `==`.

---

## Package: `log`

**Responsibility**: Structured logging (wraps `log/slog`)

```go
func NewLog(opts *LogOptions) log.Logger        // returns log.Logger, NOT a package-local `log.Logger` — no such type exists
func WrapLogger(next log.LogAdapter, maskFields []string) log.Logger
```

`log.LogAdapter` is the five methods (`Debug`, `Info`, `Warn`, `Error`, `Fatal`); `log.Logger` is
`LogAdapter` plus `From(ctx context.Context) Logger` (the scope's `trace_id`, `request`, `connection`,
`message`, `upstream` added to every line; built-in: after the arguments, adapter: before them). A type
with the five methods only is a `LogAdapter`: pass it as `AppOptions.Logger` / to `WrapLogger`, never store it in a
`log.Logger` variable.

```go
type LogOptions struct {
    Level       slog.Level // LevelDebug/LevelInfo/LevelWarn/LevelError/LevelFatal; used as given
    Format      log.Format // FormatText/FormatJSON/FormatPretty; zero: Pretty on a terminal, JSON otherwise
    IsAddSource bool       // the caller's file:line in all three formats
    MaskFields  []string   // case-insensitive rules; see log/README.md; args pair exactly like slog (a non-string key takes one slot), LogValuer groups masked by path
}
```

- `LogOptions` has exactly the four fields above; output is stdout; anything else is a custom logger behind `WrapLogger`.
- The Pretty handler is unexported; `internal/color.Colored{Text, Color}` is printed in its color by Pretty and as its `Text` in JSON and Text. Access lines carry an internal marker: Pretty colors their durations (`color.ForDuration`); user lines are never colored that way.
- Every `NewLog` logger writes to one stdout pipeline (`internal/logpipe`): synchronous without an app, during `Create` and after shutdown; asynchronous while an app serves (1 MiB queue, 64 KiB records, `WARN LogDropped` counts). A line reaches stdout up to about 1 ms late while an app serves; an unclean exit can lose up to about 1 ms of lines more. `Fatal` writes at once, flushes for at most 2 s, then exits.
- A logger call never panics because of a value: depth 16, 1,000 values per argument, `[MAX_DEPTH]` / `[TRUNCATED]`, a panicking `String` / `Error` / `LogValue` gives a `log_error` pair.
- `WrapLogger(nil, …)` panics; `WrapLogger` of a built-in or wrapped logger merges the rules (one layer).

---

## Package: `memorybroker` (`internal/memorybroker`)

**Responsibility**: Lightweight in-process topic-based pub/sub broker (no queue groups, no persistence, no ack/retry — see `internal/memorybroker/README.md` for the full spec, kept in sync with this section)

### Broker Interface

```go
type Broker interface {
    Subscribe(topic string, handler MessageHandler) (Subscription, error)
    Unsubscribe(sub Subscription) error
    Publish(topic string, payload any) error
    PublishAsync(topic string, payload any) error
    Close() error
}

type Message struct {
    Topic     string    // Topic name
    Payload   any       // Message data
    Timestamp time.Time // Captured before dispatch (time.Now())
}

type MessageHandler func(*Message)

type Subscription interface {
    ID() string
    Topic() string
    Unsubscribe() error
}

var (
    ErrClosed              = errors.New("memorybroker: broker is closed")
    ErrNilHandler          = errors.New("memorybroker: handler must not be nil")
    ErrEmptyTopic          = errors.New("memorybroker: topic must not be empty")
    ErrForeignSubscription = errors.New("memorybroker: subscription does not belong to this broker")
)
```

**No `ID`/`Metadata` on `Message`, no `Once`/`SubscribeQueue`/`Off`/`Topics`/`ListenerCount`/`Clear`/`Stats` on `Broker`** — these existed in an earlier, richer design and were deliberately removed for a leaner surface. Do not regenerate them from memory; verify against `internal/memorybroker/broker.go` before citing the API.

### Pattern Matching

Segment-based glob matcher (`pattern` package), **not regex**: `*` is exactly one segment (any position), a trailing `**` is one or more; `**` elsewhere, `*` inside a segment and `>` are literal:
- **Exact**: `user.login` — O(1) map lookup, matches only that literal topic
- **Suffix Wildcard**: `user.**` — O(1) map lookup by prefix; matches any depth below `user.` (`user.a`, `user.a.b`), not `user`
- **Global**: `**` — O(1), matches every topic. `*` alone is Complex and matches single-segment topics only
- **Complex**: `user.*`, `user.*.profile`, `*.created`, `tenant.*.user.**` — O(n) scan over registered complex patterns
- `Pattern.Match` never allocates (the complex kind walks the topic in place). Every rule with examples: `internal/pattern/MATCHING.md` (generated by `TestMatchingTableIsUpToDate`; regenerate with `go test ./pattern -run TestMatchingTableIsUpToDate -update-matching`)
- `MaskFields` uses the same syntax; a rule matches any end of the path and a matching node is masked whole (`log/README.md` › Masking)

### Delivery Model

- **Fan-Out only**: every subscription matching a topic fires on every `Publish`/`PublishAsync` — no `Once`, no queue-group load balancing
- **`Publish`**: synchronous, dispatches on the caller's goroutine; not tracked by `Close`'s `WaitGroup`
- **`PublishAsync`**: one goroutine per call (no worker pool, no bounded queue), tracked by an internal `sync.WaitGroup` so `Close` can drain in-flight calls
- **Snapshot dispatch**: handlers are copied into a private slice under `RLock`, the lock is released, *then* handlers run — a handler may safely call back into `Subscribe`/`Unsubscribe`/`Publish`/`PublishAsync` without deadlocking (verified; see Anti-Patterns for the one exception)
- **Panic recovery**: unconditional `recover()` around every handler call — one panicking handler never stops the rest or crashes the broker (not configurable, no `OnPanic` hook)

**Usage**:
```go
// Subscribe to pattern (via memorybroker package)
sub, _ := broker.Subscribe("users.*", func(msg *memorybroker.Message) {
    log.Printf("Topic: %s, Data: %v", msg.Topic, msg.Payload)
})

// Publish message
broker.Publish("users.created", userData)

// Cleanup
broker.Unsubscribe(sub)

// Alternative (preferred): inject owlky.Publisher into a handler
func (instance UserController) SUBSCRIBE_users_created(pub owlky.Publisher) string {
    pub.Publish("users.profile.updated", profileData)
    return "ok"
}
```

### Architecture

- **Locking**: one `sync.RWMutex` guards four maps (`exactByTopic`, `prefixByPrefix`, `globalByID`, `complexByTopic`) — no sharding
- **`Close()`**: `CompareAndSwap`-guarded (idempotent, safe from any goroutine) → `wg.Wait()` for in-flight `PublishAsync` goroutines → clears all four maps. Does **not** wait for a concurrent synchronous `Publish` call (see Concurrency Model)
- **Cross-broker protection**: `Unsubscribe` checks `subscription.broker == b`, rejecting a `Subscription` from a different `MemoryBroker` with `ErrForeignSubscription`
- **`Unsubscribe(nil)`**: always `nil`, unconditionally, even on a closed broker — the one exception to "all calls return `ErrClosed` after Close"

### Key Features

- ✓ Thread-safe concurrent Subscribe/Unsubscribe/Publish/PublishAsync/Close (single mutex, race-tested — see `memory_broker_test.go`'s concurrent test suite)
- ✓ `publishInternal` sizes the handler slice exactly and skips allocation entirely when nothing matches a topic
- ✓ No sharding, no background sweep, no TTL/expiration — this package holds no state beyond active subscriptions (compare `memorycache`, which does shard and sweep)

---

## Package: `trace` (`internal/trace`)

**Responsibility**: Request tracing and observability

```go
const (
    StageMiddleware      = "middleware"
    StageGuard           = "guard"
    StagePreInterceptor  = "pre_interceptor"    // split pre/post, not one combined "interceptor" stage
    StagePostInterceptor = "post_interceptor"
    StageExceptionFilter = "exception_filter"   // NOT "filter"
    StagePipe            = "pipe"
    StageHandler         = "handler"
    StageRender          = "render"             // one per HTML render (page or error page), after handler
    StageComplete        = "complete"
)
// No StageHandshake exists.

const (
    TransportHTTP = "HTTP"   // uppercase, NOT "http"
    TransportWS   = "WS"
)

type Event struct {
    ID        string
    TraceID   string   // the scope's trace ID ("" without a scope)
    SpanID    string   // complete: == ID; other stages: a fresh NewSpanID() (only while traced)
    ParentID  string   // complete: Scope.Parent; other stages: == ID
    Stage     string
    Name      string
    Transport string
    Duration  time.Duration
    Operation string
    Target    string
    Code      int
}

type Tracer struct{ /* unexported; the zero value is ready */ }
func (t *Tracer) Enabled() bool                        // one atomic load
func (t *Tracer) Emit(e Event)                         // synchronous, in registration order, no recover, 0 allocs
func (t *Tracer) Listen(fn func(Event)) (stop func())  // stop removes exactly that registration

func Transport(base http.RoundTripper) http.RoundTripper // design 0013; nil base = http.DefaultTransport at each call
```

- `Transport` (`internal/trace/http_transport.go`) adds `Traceparent: <Scope.Traceparent()>` (the request's or
  message's own span, the scope's flags) to a shallow copy of an outbound request whose context carries a
  trace scope; without a scope, or when the caller's header map already has the canonical
  `Traceparent` key (even with no value: the opt-out), the same request passes through untouched.
  Nothing else is added (no `tracestate`, no `X-Request-Id`). It forwards `CloseIdleConnections`.

- There is no event bus (design 0010). `core` holds one `trace.Tracer`
  per app (`app.tracer`); the access log is its only listener today. Every event carries `Transport`.
- Span tree per unit (request or WS message): `complete` is the root, emitted last; every other stage is
  a child of `ID`. HTTP exception filter events follow `ex.traced` like every other stage.

---

## Package: `aggregation` (`internal/aggregation`)

**Responsibility**: Interceptor pipeline state, carried through the request context

```go
type Operator struct{ /* unexported: kind + function; the zero value is skipped */ }

type Aggregation struct {
    // no exported field: interceptorData, name, isMainHandlerCalled, mainData, operators
}

func NewAggregation() *Aggregation

// framework hooks (package functions, design 0017 D3)
func SetMainData(a *Aggregation, d any)
func Aggregate(a *Aggregation) any
func Name(a *Aggregation) string
func SetName(a *Aggregation, name string)
func InterceptorData(a *Aggregation) any
func SetInterceptorData(a *Aggregation, data any)
func IsMainHandlerCalled(a *Aggregation) bool
```

**This bears no resemblance to older docs claiming `Data`/`Exception`/`Request`/`Response` fields — none of those exist.**

| Method | Signature | Purpose |
|--------|-----------|---------|
| `Transform` | `Transform(fn func(any) any) Operator` | Creates a transform step (registers nothing) |
| `Tap` | `Tap(fn func(any) any) Operator` | Creates a side-effect step (return value discarded) |
| `Filter` | `Filter(predicate func(any) bool) Operator` | Creates a filter step (predicate false → `Aggregate()` returns nil; `nil` data with a true predicate continues) |
| `Pipe` | `Pipe(operators ...Operator) any` | Makes `IsMainHandlerCalled(a)` true, replaces the operators with exactly these (nil-function and zero operators skipped), returns nil; up to 4 operators stored inline without allocating |

Used as the second parameter of an interceptor's `Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any` (see `common` section). The framework calls `aggregation.SetMainData(a, handlerResult)` then `aggregation.Aggregate(a)` (the piped operators over the main data in `Pipe` argument order) after the handler returns (`internal/core/http.go`).

---

## Package: `wsevent` (`internal/wsevent`)

**Responsibility**: WebSocket event-name routing (mirrors `routing` for WS)

**There is no `Matcher` type or package-level `Match(topic, pattern string) bool` function.** The real exported type is `WSEvent`:

```go
type WSEventItem struct {
    Handler     any
    Middlewares []ctx.WSHandler
}

func NewWSEvent() *WSEvent
```

| Method | Signature |
|--------|-----------|
| `Add` | `Add(pattern string, value WSEventItem)` |
| `AddMiddlewares` | `AddMiddlewares(pattern string, middlewares ...ctx.WSHandler)` |
| `AddInjectableHandler` | `AddInjectableHandler(pattern string, handler any)` |
| `Match` | `Match(topic string) (WSEventItem, string, bool)` — a **method**, one string param, three return values |

Pattern matching delegates to the same `pattern` package `memorybroker` uses: segment-based glob (`*` one segment, trailing `**` one or more) — **not regex**. (`"users\.[a-z]+"` style patterns do not work; there is no `matcher` package either — that name doesn't exist anywhere in the repo.)

---

## Package: `modules/config`

**Responsibility**: `.env` loading with typed struct binding

```go
func Register(opts *ConfigModuleOptions) *core.Module   // package name: config

type ConfigModuleOptions struct {
    IsGlobal          bool
    IsIgnoreEnvFile   bool
    IsOverride        bool
    IsExpandVariables bool
    EnvFilePaths      []string
    Loads             []func() map[string]any
    Hooks             []func(ConfigService)
    OnInit            func()
}
```

Provides `ConfigService{State *configState}` (`Get(k string) any`, `Set(k string, v any)`, `Transform(s any) (any, []ctx.FieldLevel)`). `State` is opaque (unexported type, no exported field or method): there is **no exported config map or `.env` type**; read values only through `Get`. `Get` is an atomic load + map lookup; `Set` copies the map under a mutex (copy on write), so both are race-free at any time; `Set` on a zero `ConfigService{}` panics. `Transform` binds strings, and bools / integers / floats through their `strconv` text, skipping maps, slices and nil. **There is no package-level `NewConfigModule(envPath string)` function** — use `config.Register(opts)`.

- Precedence: process env, then `EnvFilePaths` in order, then `Loads` in order; first source wins, last with `IsOverride`. Files never reach `os.Setenv`.
- Default file: `.env` in the nearest directory upward with a `go.mod`, else the working directory; missing default is fine, listed paths must exist. `IsIgnoreEnvFile` reads no file; with `EnvFilePaths` it panics.
- Strict parser (`dotenv.go`, byte loops, no regexp): `[export ]KEY=value`, `#` starts a comment only after a blank, no escapes unquoted, double quotes with `\n \r \t \\ \" \$`, literal single quotes, multiline quotes, BOM, CRLF. Expansion (`IsExpandVariables`) of `${NAME}` only (same file, then process env) with cycle detection.
- Limits: 1 MiB regular file (FIFOs / devices refused before reading), 64 KiB per value and 16 MiB for all values together after expansion, no NUL, `Loads` nesting ≤ 32.
- Every failure is a `Register` panic `invalid config: <path>:<line>: <detail>` without any value.
- `inspect.ConfigStateOf(service)` (internal, for devtools) returns the files read and the sorted key names, never values.

## Package: `modules/cache`

**Responsibility**: Pluggable cache abstraction + default in-memory backend

```go
func Register(opts *CacheModuleOptions) *core.Module   // package name: cache

type CacheModuleOptions struct {
    IsGlobal bool
    OnInit   CacheOnInitFn  // func()
    Backend  Cache          // pluggable; defaults to NewMemoryCache(nil) if nil
}

type MemoryCacheOptions = memorycache.MemoryCacheOptions // MaxEntries, MaxBytes
func NewMemoryCache(opts *MemoryCacheOptions) Cache      // the built-in store; nil = defaults

type Cache interface {
    Get(ctx context.Context, key string) ([]byte, bool)
    Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
    SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)
    Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
    Delete(ctx context.Context, key string) error
    Keys(ctx context.Context) []string
    TTL(ctx context.Context, key string) (time.Duration, bool)
}
```

`CacheService{Backend Cache}` forwards all seven methods unchanged. `cache.ErrEmptyKey`, `ErrNegativeTTL`, `ErrEntryTooLarge`, `ErrNotCounter` are the `memorycache` sentinels (same values, messages `cache: …`). The backend is created once per `Register` call and shared by every injected copy and every app built from that module value; `Register` starts no goroutine. Contract for any backend: exact expiry, copies in and out, atomic `SetNX` / `Incr`, counters 8 bytes big-endian, negative ttl is `ErrNegativeTTL`. There is no size option on the module: pass `Backend: cache.NewMemoryCache(&cache.MemoryCacheOptions{…})`.

**There is no package-level `NewCacheModule()` function** — use `cache.Register(opts)`. No LFU eviction anywhere (see `memorycache` package below for the real CLOCK eviction).

Owlky Go has no HTTP client or persistence module: outbound calls use `net/http` with `owlky.TraceTransport` (see `internal/trace/README.md`).

---

## Package: `memorycache` (`internal/memorycache`)

**Responsibility**: Bounded in-memory sharded byte cache with exact TTL and an atomic counter — **no persistence of any kind**

```go
type MemoryCacheOptions struct {
    MaxEntries int   // default 65536; rounded down to a multiple of 64, minimum 64
    MaxBytes   int64 // default 16 << 20; len(key)+len(val) summed; minimum 64 KiB
}
func NewMemoryCache(opts *MemoryCacheOptions) *MemoryCache // nil = defaults
```

| Method | Signature |
|--------|-----------|
| `Get` | `Get(ctx, key string) ([]byte, bool)` |
| `Set` | `Set(ctx, key string, val []byte, ttl time.Duration) error` |
| `SetNX` | `SetNX(ctx, key string, val []byte, ttl time.Duration) (bool, error)` |
| `Incr` | `Incr(ctx, key string, delta int64, ttl time.Duration) (int64, error)` — 8-byte big-endian counter; creates with `ttl`, keeps the expiry of a live counter |
| `Delete` | `Delete(ctx, key string) error` |
| `Keys` | `Keys(ctx) []string` |
| `TTL` | `TTL(ctx, key string) (time.Duration, bool)` |

Errors: `ErrEmptyKey`, `ErrNegativeTTL` (a negative ttl writes nothing), `ErrEntryTooLarge` (`len(key)+len(val) > MaxBytes/64`), `ErrNotCounter`. Nothing to close: no method shuts a cache down; an unreferenced cache is collected.

**`PersistenceConfig` and `NewMemoryCacheWithConfig` do not exist and never have — do not generate them.** The package has zero file I/O.

**Key characteristics**: 64 shards (`hashKey(key) & 63`, `hash/maphash` with a per-process random seed), each a `sync.RWMutex` + index map + entry array + free list, padded to its own cache lines. Each shard holds `MaxEntries/64` entries and `MaxBytes/64` bytes; a write past a bound evicts in that shard by CLOCK (expired first, then entries whose reference mark — set by a `Get` hit or a rewrite — is clear). Time is the monotonic clock (`time.Since` of the cache's creation); an expired entry is absent at once for every method. No goroutine and no timer at creation: one `time.AfterFunc` sweep (every 5 s) is armed only while an entry with a ttl exists and holds the cache through a `weak.Pointer`, so an unreferenced cache is collected. Keys are `strings.Clone`d. `inspect.CacheStateOf(mc)` (internal) reports entries, bytes, bounds, hits, misses, evictions, expirations and whether the sweep is armed. Conforms to `modules/cache.Cache`. See `internal/memorycache/README.md`.

---

## Guards & Middlewares (selected)

### `guards/throttler`

```go
type Throttler struct {
    Limit    int64                        // default 100
    TTL      time.Duration                // default time.Minute
    Strategy Strategy                     // StrategyFixedWindow (default 0) | StrategySlidingWindow | StrategyTokenBucket; unknown panics
    KeyFunc  func(owlky.HTTPContext) string // default: the client IP (owlky.ClientIP), IPv6 by /64, "unknown" for the zero Addr
    Backend  cache.Cache                  // default: one memorycache per guard, MaxEntries 32,768 (CLOCK eviction)
}
func (instance Throttler) NewGuard() any // the compiled *guard (CanActivate, inspect.Snapshotter)
```

- Bind the `Throttler` value (`BindGuard(throttler.Throttler{...})`); `construct` calls `NewGuard` once.
  Equal options share one compiled guard and one budget (layer cache).
- No request header is read for the default key; trust goes through `AppOptions.Proxy` (design 0014 T1).
- Keys over 128 bytes become `h:` + 16 hex digits of a per-guard `maphash` (T2).
- Fixed window: one `Incr` per request (counts refusals too). Sliding window (one 24-byte entry per
  key: window id, previous and current count; refusals not stored) and token bucket (16 bytes) run
  Get + Set under one of 64 mutexes picked by `maphash` of the stored key, unlocked with `defer`
  so a panicking custom `Backend` never leaves a stripe locked (coordinator decision).
- Backend errors allow the request (T7). No goroutine; the store's expiry timer only while it holds TTL entries.
- Snapshot (`name: throttler`): limit, ttl, strategy, key, backend, and with the default store
  maxKeys / keys / evictions (from `inspect.CacheStateOf`), then rejected (an atomic, refusals only).
- Memory: idle about 14 KiB (the store's shards + 512 B of mutexes); about 150 B per entry (IPv4 key), 280 B (128-byte key); full store about 4.7 MiB (IPv4),
  5.2 MiB (IPv6), 8.7 MiB with 128-byte custom keys. Per request 6 allocs (fixed window), 8 (the others).

### Pre-route stages (`internal/middlewares/*`, designs 0014 and 0022)

Not importable: each package below is compiled by `core.New` from its `AppOptions` group (`Compile(&pkg.Options…)`, no reflection) and runs as a pre-route stage in the fixed order `loadshed` → `proxy` → `hostcheck` → `httpsredirect` → `headers` (`helmet`) → `cors` → `csrf` → `compress`. A stage is off until configured: `LoadShed`, `HTTPSRedirect`, `Headers`, `CORS`, `CSRF`, `Compress` by `IsEnabled`; `Proxy` by `TrustedProxies` or `TrustedHops`; `hostcheck` by a non-empty `Hosts`. `cors` and `csrf` read one origin set compiled from `AppOptions.Origins` (`internal/middlewares/origin`), which also backs the WebSocket handshake check. Option checks panic at `New` as `invalid app: <Group>.<Field> …` (the `invalid <pkg>:` texts below, rewritten). Users read the client with `owlky.ClientIP` / `ClientScheme` / `ClientHost`. The structs below are the internal option types; their public forms are `owlky.ProxyOptions`, `LoadShedOptions`, `HTTPSRedirectOptions`, `HeadersOptions`, `CORSOptions`, `CSRFOptions`, `CompressOptions` (`MinSize` a `ByteSize`).

### `internal/middlewares/proxy` (design 0014)

```go
type Header int
const ( HeaderXForwarded Header = iota; HeaderForwarded )
type Proxy struct {
    TrustedProxies  []string // IPs or CIDRs of the proxies in front
    TrustedHops     int      // or: how many proxies are in front (1-16)
    Header          Header   // HeaderXForwarded (default) or HeaderForwarded (RFC 7239); the other family is never read
    IsHostForwarded bool     // read X-Forwarded-Host / host=; off: Host(r) is always r.Host
}
func (instance Proxy) NewMiddleware() common.MiddlewareFn
func ClientIP(r *http.Request) netip.Addr // zero Addr when unknown
func Scheme(r *http.Request) string       // "https" or "http"
func Host(r *http.Request) string
```

Exactly one of `TrustedProxies` / `TrustedHops` (else panic `invalid proxy: set TrustedProxies or TrustedHops[, not both]`). Without `proxy` bound, nothing in Owlky Go reads `X-Forwarded-*`, `Forwarded` or `X-Real-IP`: the client is `r.RemoteAddr`, the scheme `r.TLS != nil`, the host `r.Host`. Trusted-proxies mode: entries right to left, client = first untrusted, at most 16 entries of 512 bytes, invalid entry ends the walk. Scheme and host are read only when the client came from a header entry. `X-Forwarded-Proto`: the last value only (last line, after its last comma), whatever the counts; `Forwarded`: `proto` of the client's element or the nearest one to its right. The host (`X-Forwarded-Host` last value, or `host=`) is read only with `IsHostForwarded` (security review M2, design 0014 Revision 2026-11-17); no count rule. Snapshot field `hostForwarded`. The result is written into the request's `tracecontext.Scope` (`ClientIP`, `ClientHost`, `IsClientHTTPS`, `IsClientSet`); the request is never changed. Bind it first.

### `internal/middlewares/hostcheck`, `internal/middlewares/httpsredirect`

```go
type HostCheck struct { Hosts []string }             // "example.com", "api.example.com:8443", "*.example.com", "127.0.0.1", "[::1]"
type HTTPSRedirect struct { Host string; Port int }  // fixed target host (no port); HTTPS port when not 443
```

`hostcheck` checks `proxy.Host(r)`: no match → `421 Unknown host`, empty or > 255 bytes → `400 Invalid host`; no implicit `localhost`. `httpsredirect`: `proxy.Scheme(r) == "https"` → next; else `308` to `https://<Host or request host without port><:Port><path>?<query>`, no body; an invalid request host (with no `Host` set) → `400 Invalid host`.

### `internal/middlewares/csrf` (rebuilt, design 0014)

```go
type CSRF struct { TrustedOrigins []string } // scheme://host[:port], at most 64
```

Wraps `http.CrossOriginProtection` (Go 1.25+): safe methods pass; `Sec-Fetch-Site` `same-origin`/`none` pass; any other value → `TrustedOrigins` or `403 Cross-origin request refused`; no `Sec-Fetch-Site` and no `Origin` → pass (not a browser); `Origin` host equal to the host → pass. `Origin` > 256 bytes refused. No token, no cookie, no context value, no body read. **`TrustedOrigins` is the only field and the package exports nothing else: do not generate token fields or helpers.** Server-rendered forms need nothing more (no token, no hidden field); a refusal is a 403 HTML page when `Accept` holds `text/html` and `EnableRender` is on, else JSON.

### `internal/middlewares/cors` (changed, design 0014)

```go
type CORS struct {
    AllowOrigins          []string      // required: "https://app.example.com", "https://*.example.com", or "*" alone
    AllowMethods          []string      // default GET, HEAD, PUT, PATCH, POST, DELETE
    AllowHeaders          []string      // default Content-Type, Authorization (never the request's list)
    ExposeHeaders         []string
    MaxAge                time.Duration // default 10 minutes
    IsAllowCredentials    bool
    IsAllowPrivateNetwork bool
}
func (instance CORS) NewMiddleware() common.MiddlewareFn
```

**Panics at `Create()`**: empty `AllowOrigins` (`invalid cors: AllowOrigins is empty` — the zero value allows nothing), `"*"` with other entries or with `IsAllowCredentials`, an origin that is not lower-case `scheme://host[:port]` or `scheme://*.host[:port]`, `null`, more than 256 origins or 64 list entries, a name that is not an RFC 9110 token. `Vary: Origin` on every response with a list. Preflight is exactly `OPTIONS` + `Origin` + `Access-Control-Request-Method` (`204`); any other `OPTIONS` reaches its `PREFLIGHT_…` route. A non-matching origin gets no CORS header but is still served. A WebSocket upgrade from a non-matching `Origin` → `403 Origin not allowed`. **The seven fields listed above are the only options; origins are exact strings or `*.` patterns.**

### `internal/middlewares/helmet` (changed, design 0014)

```go
const Disabled = "-"
type Helmet struct {
    ContentSecurityPolicy, CrossOriginEmbedderPolicy, CrossOriginOpenerPolicy,
    CrossOriginResourcePolicy, FrameOptions, PermissionsPolicy,
    ReferrerPolicy, StrictTransportSecurity string
}
```

Each field: `""` = default, `helmet.Disabled` = not sent, else sent as written (control characters panic). HSTS (`max-age=63072000; includeSubDomains`) only when `proxy.Scheme(r) == "https"`. COEP not sent by default. Always sent: `X-Content-Type-Options: nosniff`, `X-XSS-Protection: 0`, `X-DNS-Prefetch-Control: off`, `X-Permitted-Cross-Domain-Policies: none`, `Origin-Agent-Cluster: ?1`. **The eight string fields above are the only options; the always-sent headers can't be configured.** On pages rendered by Owlky Go that call `{{nonce}}`, the CSP carries `'nonce-<N>'` in `script-src` / `style-src` (inserted after `default-src` when absent) and in `script-src-elem` / `style-src-elem` when present; directives with `'unsafe-inline'` or `'none'` are untouched; no new field (design 0018 R30-R35). Snapshot adds `nonce_directives`.

### `internal/middlewares/compress`

```go
type Compress struct { MinSize int } // default 1024
```

gzip at `BestSpeed` for candidates (not `HEAD`, no `Range`, no `Upgrade`, `Accept-Encoding` names gzip with q ≠ 0 in its first 256 bytes) whose response is compressible text, at least `MinSize`, not encoded, not `204`/`206`/`304`, no `no-transform`. `Vary: Accept-Encoding` on every response. At most 8 writers per instance (pooled, design-approved `sync.Pool`); none free → sent uncompressed. Uses `internal/httphook` to wrap the writer and finish at the end of the exchange.

### `internal/middlewares/loadshed` (new, design 0014)

```go
type LoadShed struct {
    MaxInFlight int // 0: adaptive (default); 1-1,000,000: fixed limit
    MaxQueue    int // 0: 64; negative: no queue; at most 65,536
}
```

Bounds requests between admission and the end of their exchange (released through an `internal/httphook` `OnDone` hook, also on panic, 404, abort and at the WS handshake). Over the limit, FIFO queue of `MaxQueue` for 100 ms (`enqueue` first hands free slots to the listed waiters in order, then checks full, then retries for itself only when the list is empty: a request never joins ahead of a waiter; one that arrives at the instant a slot frees, before the slot reaches a waiter, may take it: design 0014 revision 3, amended L2), then `503` + `Retry-After: 1`, message `Server is busy` (`panic(&busy)`); a request whose context ends while waiting gets nothing. Adaptive mode: bounds read once in `NewMiddleware` (`min` = `GOMAXPROCS`, `max` = smallest of `GOMEMLIMIT` / cgroup `memory.max` / v1 `memory.limit_in_bytes` ÷ 4 MiB, else 256 × `GOMAXPROCS`, clamped 64-4,096; start 8 × `GOMAXPROCS`); windows of 4 × the current limit samples (sum in units of 64 ns and count packed in one atomic word, design 0014 M4); a sample is `elapsed` minus the queue wait and minus the time inside `r.Body.Read`, capped at 32 × the baseline once one is known; baseline: `learned` from quiet samples (≥ 8 per window) and probes (blended `(learned + input) / 2`, refreshes the age), a faster window lowers the working baseline to `max(avg, learned / 1.5)` without touching `learned` or the age; the limit moves only at the close of a window under pressure (a request queued or refused: one `atomic.Bool`) with a baseline and no probe during it: `useful = limit × min(1.5 × baseline / avg, 1)`, `next = 0.8 × limit + 0.2 × (useful + √useful)`; no growth when `avg < baseline / 2`; floor `max(min, ceil(earned / 2))` where `earned` is the highest integer limit reached by a raise (the start limit is not earned); a close below the floor holds the limit there and marks "held by the floor"; then clamp `min`-`max`; probe when under pressure, the baseline is unknown, older than 30 s or the floor held, and no probe started in the last 30 s: an unexported record per probe (`adaptive.current`, an `atomic.Pointer`, stored before `admit` goes to `-min`; `held`, `sum`, `count`, `finished`), at most `min` of its own requests in flight (`held`) and the total within `limit`, so it admits beside long-held requests from before it; a probe request remembers `finished` at admission and is a probe sample only when no request the probe did not admit finished meanwhile (overlap filter); ends after 8 probe samples or at the first admission after 1 s; a probe with samples is a learning, sets `earned = min(earned, limit)` and clears the hold; an empty probe changes nothing. A probe request without a body carries a 16 B ticket (record, mark); waiters and the body wrapper carry them in their own fields. Long-held acceptance L14 (`TestAdaptive_LongHeld_*`, skipped with `-short` and `-race`); known limit: each long-held request (SSE, download, long poll) holds a slot for its life, in both modes. Body time: in adaptive mode an admitted request with a body (not `nil`, not `http.NoBody`) gets `r.Body` replaced by an unexported wrapper (forwards `Read` / `Close` unchanged, `atomic.Int64` read time, also its `OnDone` hook; 64 B, created at the three admission points); refused requests and fixed mode keep `r.Body`. Clock of the rule: unexported `adaptive.now` (default `time.Now`; tests inject a fake), read only at a window close and during a probe. Known limit: a CPU-bound handler that never reaches the scheduler hides overload from any in-flight limit (`BenchmarkLoadShed_L12_CPUBoundNoYield_5x`); L12 tests use a handler that yields. A queued request's sample excludes its wait in loadshed's own queue (measured when a slot is handed to it). No goroutine, no timer while idle; admitted request: 0 allocations (1 with a body in adaptive mode, 1 of 16 B when a probe admits it). `max` bounds the count in flight, not memory. Steering acceptance L13 (`TestAdaptive_Steering_*`, skipped with `-short`); `BenchmarkLoadShed_L13_SlowRouteTuned` reports the residual (known limit). Implements `inspect.Snapshotter` (`mode`, `limit`, `queue`, `wait`, `retry_after`, `in_flight`, `waiting`, `refused_*`, and in adaptive mode `min`, `max`, `probes`, `probing`, `baseline`, `average`).

Order (fixed by `New`, design 0022 P1): `loadshed`, `proxy`, `hostcheck`, `httpsredirect`, `headers`, `cors`, `csrf`, `compress`.

---

## Supporting Packages (brief — not exhaustively cataloged here)

- `internal/render` — the template engine behind `AppOptions.Render` (design 0018): `New(fsys, isReload) *Engine`, `Load`, `Bind(route) (string, error)`, `CheckBindings`, `Render(route, data) (*Buffer, error)`, `RenderError(code, data)`, `ErrorTemplate(code)`, `Release`, `Assets`, `AssetHashes`, `Public`, `PublicFiles`, `PageAssets`, `PageAsset`, `Files`, `IsReload`, `Snapshot`, `Counters`, `NonceDraws`; `Buffer{Bytes, Len, Nonce}`, `FileError{File, Err}`, `ErrNoTemplate`, `ErrTwoTemplates`, `ErrReservedDir`, `ErrErrorPageName`, `ErrPageTooLarge`, `ErrNoErrorPage`. Pooled buffers (256 KiB retention, 8 MiB limit), the nonce placeholder overwritten in place. Not for users.
- `devtool` — `Devtool{}` (embeds `GetConfigurationResponse`: `Controller.Http []*HTTPComponent` with route, method, layers by scope, versioning, request schemas and `kind: "page"` for page routes; `Render` (`render`: `is_reload`, `pages` (with `route` and `assets`), `partials`, `error_templates`, `assets`, `public`, `renders`, `failures`) with `EnableRender`; plain Go structs with JSON tags) and `GetConfiguration(...)`. There is no transport yet. `EnableDevtool()` on `App` builds it at `Create()`.
- `accesslog` — `NewAccessLog(cfg *AccessLogOptions) *AccessLog` (`AccessLogOptions{Tracer *trace.Tracer; Logger log.LogAdapter}`), `(*AccessLog).Stop()`; wired in via `AppOptions.IsAccessLog`, stopped once at shutdown after the `OnShutdown` hooks. No goroutine: the line is logged on the request goroutine; each stage's value is one string (`"CORS 40µs (4%), CSRF 60µs (6%)"`). Bounded: 64 stages per request, 16,384 pending requests; with the built-in logger the line is an Info record of the log pipeline (dropped and counted in `LogDropped` under overload). Line keys start `trace_id`, then `request` (HTTP) or `connection` + `message` (WS), then `ip` for HTTP when the client address is known (`trace.Event.ClientIP`, filled on `complete` from `tracecontext.ClientIP`); there is no `id` key.
- `versioning` (`internal/versioning`) — `Options{Type, Key, DefaultVersion, Extractor}`, `type Type int` (`TypeQuery`, `TypeHeader`, `TypeCustom`, `TypeMediaType` = 1–4), hooks `TypeString(o) string` / `Version(o, c *ctx.HTTPContext) string`; users write `app.EnableVersioning(&owlky.VersioningOptions{Type: owlky.VersioningHeader, …})`.
- `internal/tracecontext` — W3C trace context (design 0011): `NewSpanID`, `NewTraceID`, `ParseTraceparent`, `NewRequest`, `NewMessage`, `With`, `ScopeOf`, `Scope` (`TraceID`, `Request`, `Connection`, `Message`, `Upstream`, `Parent`, `LogSlot`, and the client fields `ClientIP`, `ClientHost`, `IsClientHTTPS`, `IsClientSet` written by `proxy`; `AppendArgs`, `Traceparent`, `SpanID`, `Headers`); `ClientIP` / `IsHTTPS` / `Host(r)` (what `proxy.ClientIP` / `Scheme` / `Host` return) and the shared host syntax helpers `RemoteIP`, `IsValidHost`, `HostName`, `SplitHostPort`, `IsPort`.
- `internal/httphook` — `Hook{Done(elapsed)}`, `Writer{Inner, SetInner, OnDone}`, `Of(w)`, `CSP{Value, Parts}` + `CSPWriter{SetCSP}` (helmet's nonce form, design 0018): the framework's response writer lets a built-in middleware put a writer in front and run code when the exchange ends (after the lifecycle and before the `complete` event, at the WS handshake's end, before `http.ErrAbortHandler` is re-panicked). One length check per request when unused. Not for users.
- `internal/inspect` — besides `CacheStateOf` / `ConfigStateOf`: `Field`, `Snapshot`, `Snapshotter`; every compiled middleware of design 0014 implements `Snapshotter` (effective options + counters, snake_case keys, built only when called). All random bytes from `crypto/rand`, through a `sync.Pool` of 512-byte sources (504-byte refills, no mutex, design 0013); one 18-byte read per new trace (tail + request ID), 8 bytes for a continued trace or a message under a connection.
- `internal/logpipe` — the output pipeline of the built-in logger (design 0012): `Level` (`Debug`…`Fatal`), `Sink` (`Write`, `StartAsync`, `StopAsync`, `Flush`, `Snapshot`: an `inspect.Snapshotter` named `logpipe`; async writes are woken 1 ms after a burst's first record or at 16 KiB, design 0016), `New`, `Stdout`, `SetStdout` (tests), `FlushTimeout` (2 s), `AccessLine` (the access log's line handed to the built-in logger).
- `internal/crypto` — `UUID()` only (`crypto/rand`); `RandomHex` / `AppendRandomHex` are gone.
- `internal/apicheck` — the API guard (design 0017 step 3), a `main` package: `make api` writes `api/owlky.txt` (sorted `pkg` lines of every public package plus `reach` lines for members of module types those lines mention), `make api-check` diffs it and fails on a public package outside its allow list. Runs in `make check` and the CI `lint` job.
- `internal/*` — everything else: the engine (`core`, `common`, `ctx`, `routing`, `pattern`, `wsevent`, `memorybroker`, `memorycache`, `aggregation`, `versioning`, `trace`, `accesslog`, `devtool`, moved there by design 0017) and private utilities (color, ds, str, crypto, num, slice, test, tracecontext, logpipe, httphook, inspect, apicheck). Not importable from outside the module.

**There is no `data/` package or directory anywhere in this repository** — an earlier doc listed one; it never existed.
