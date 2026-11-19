# 0017: Public API surface

Status: Approved
Packages: every package outside `internal/`, `sample/` and `bench/`; the root package; `sample/`, `bench/`;
a new `internal/apicheck`. Roadmap: `docs/roadmap/0007-public-api-review.md`, steps 2 to 6. Input:
`docs/review/api-inventory.md` (step 1: 776 exported names, each with its uses and class).

## Goal
An application imports `go.owlky.com/owlky` plus `exception`, `log`, `middlewares/*`,
`modules/*` and `guards/*`, and nothing else can be imported. Every name it can reach is one it needs,
named by one rule: 776 exported names become 368. A committed API file makes every later addition a
reviewed diff. The next roadmaps (a render route kind, authentication guards) add names to this surface
without moving or renaming what is in it.

## Decisions in one table

| # | Decision | Why |
|---|---|---|
| D1 | `core`, `common` and `ctx` move to `internal/` as whole packages; the root package is their only door | 223 framework internals are used across packages (`ctx.NewHTTPContext`, `common.AsHTTPGuard`, `routing.ParseToParamKey`…). Go can only share them by exporting them, so a public package that keeps them cannot hide them. A path under `internal/` can. Two import paths for one type (`ctx.Body` and `owlky.Body`) would also be two ways |
| D2 | Ten more packages move to `internal/`: `pattern`, `devtool`, `wsevent`, `routing`, `memorybroker`, `memorycache`, `accesslog`, `trace`, `aggregation`, `versioning` | None is in the roadmap's import list. What users need from them is reached through the root package (`aggregation`, `versioning`, `trace.Transport`) or `modules/cache` (`memorycache`) |
| D3 | **Hook rule.** What the framework needs from a type users see, and users must not touch, is a package-level function of the internal package that owns the type (`ctx.HTTPReset(c)`), never an exported method or field (`c.Reset()`) | A method or field of an aliased type stays reachable through the alias (`owlky.HTTPContext`), whatever the path of its package. A package-level function of an `internal/` package is not. 42 members become 42 functions |
| D4 | Package names do not change (`core`, `ctx`, `common`…), only their paths | `reflect.Type.String()` prints the package name: panic messages, the devtool snapshot, layer names in traces and the dynamic-module test (`*core.Module`) stay byte for byte |
| D5 | A root alias exists only for a type whose package users cannot import | `owlky.Request`, `owlky.Response` and `owlky.Exception` are second names of `*http.Request`, `http.ResponseWriter` and `*exception.Exception`. `log.Logger` gets no root alias for the same reason |
| D6 | `exception`, `log`, `middlewares/*`, `modules/*` and `guards/*` stay public and keep importing the engine by its internal path, not through the root package | `core`'s own tests import `middlewares/proxy`: a `proxy` that imported the root package (which imports `core`) would be an import cycle in those tests |
| D7 | D-003 (`main`): guards, interceptors and filters stay `any`, checked by shape at `Create()`. The three named `any` types (`Guarder`, `Interceptable`, `ExceptionFilterable`) are removed | One type serves HTTP and WS on `master`. A named type that is `any` adds a name and no check |

## Public API

### Import paths after the change

| Import path | Holds |
|---|---|
| `go.owlky.com/owlky` | The application, modules, controllers, layers, contexts, request data, versioning and server options |
| `…/owlky/exception` | Exceptions |
| `…/owlky/log` | The built-in logger and the logger interfaces |
| `…/owlky/middlewares/compress`, `cors`, `csrf`, `helmet`, `hostcheck`, `httpsredirect`, `loadshed`, `proxy` | Built-in middlewares |
| `…/owlky/modules/cache`, `config` | Built-in modules |
| `…/owlky/guards/throttler` | Built-in guard |

### Root package `owlky` (39 names: 17 kept, 22 added, 4 removed)

```go
package owlky

type (
	App           = *core.App
	Module        = *core.Module
	Controller    = core.Controller
	Provider      = core.Provider
	ServerOptions = core.ServerOptions
	WSOptions     = core.WSOptions

	HTTP             = common.HTTP
	WS               = common.WS
	Guard            = common.Guard
	Interceptor      = common.Interceptor
	ExceptionFilter  = common.ExceptionFilter
	Middleware       = common.Middleware
	MiddlewareFn     = common.MiddlewareFn
	ArgumentMetadata = common.ArgumentMetadata
	Publisher        = common.Publisher

	HTTPContext = *ctx.HTTPContext
	WSContext   = *ctx.WSContext
	Body        = ctx.Body
	Form        = ctx.Form
	File        = ctx.File
	Query       = ctx.Query
	Header      = ctx.Header
	Param       = ctx.Param
	WSPayload   = ctx.WSPayload
	Map         = ctx.Map
	Next        = ctx.Next
	Redirect    = ctx.Redirect
	FieldLevel  = ctx.FieldLevel
	DataFile    = *ctx.DataFile

	Aggregation = *aggregation.Aggregation

	VersioningOptions = versioning.Options
	VersioningType    = versioning.Type
)

const (
	VersioningQuery     = versioning.TypeQuery
	VersioningHeader    = versioning.TypeHeader
	VersioningCustom    = versioning.TypeCustom
	VersioningMediaType = versioning.TypeMediaType
)

func New() App
func ModuleBuilder() *core.ModuleBuilder
func TraceTransport(base http.RoundTripper) http.RoundTripper
```

Alias form: a pointer alias for a type the framework hands out by pointer (`App`, `Module`,
`HTTPContext`, `WSContext`, `Aggregation`, `DataFile`), a plain alias for a type users construct, embed or
implement.

| Name | State | Why users need it |
|---|---|---|
| `New` | added | Creates the application. Replaces `core.New` |
| `ModuleBuilder` | added | Builds a module. Replaces `core.ModuleBuilder` |
| `TraceTransport` | added | Continues the request's trace on outbound calls. Replaces `trace.Transport` |
| `App` | kept | The type of `New()`'s result, for functions that take the app |
| `Module` | added | The result type of a static module (`var Module = func() owlky.Module`) and of `Register` |
| `Controller`, `Provider` | added | The result types of `NewController()` and `NewProvider()` |
| `ServerOptions` | added | Second argument of `Listen`, `ListenTLS`, `Serve` |
| `WSOptions` | added | Argument of `EnableWS`. Was `core.WSOptions` |
| `HTTP`, `WS` | added | Embedded in a controller to declare its transport |
| `Guard`, `Interceptor`, `ExceptionFilter`, `Middleware` | added | Embedded in a controller to bind layers to handlers |
| `MiddlewareFn` | added | The type of a middleware list and the result type of `NewMiddleware()` |
| `ArgumentMetadata` | added | Second parameter of every pipe's `Transform` |
| `Publisher` | kept | Handler parameter and injected field that publishes to WebSocket topics |
| `HTTPContext`, `WSContext` | kept | Parameter of handlers, guards, interceptors and filters |
| `Body`, `Form`, `File`, `Query`, `Header`, `Param`, `WSPayload` | kept | Handler parameters and the first parameter of a pipe's `Transform` |
| `Map` | kept | The JSON object handlers return |
| `Next`, `Redirect` | kept | Handler parameters; `Next` is also the third parameter of `Use` |
| `FieldLevel` | kept | Element of the second result of `Bind`, for validation |
| `DataFile` | added | Parameter of a file DTO's `IsValid` and `Store` |
| `Aggregation` | kept | Second parameter of `Intercept` |
| `VersioningOptions` | renamed from `Versioning` | Argument of `EnableVersioning` |
| `VersioningType` and its 4 constants | added | The value of `VersioningOptions.Type`. Replace `versioning.QueryVersion`… |
| `Router` | removed | No API takes or returns a router |
| `Request`, `Response`, `Exception` | removed | D5: write `*http.Request`, `http.ResponseWriter`, `*exception.Exception` |

### Members reachable through the root package

Members not listed here are hooks (next section) or are removed. The inventory's last column gives the fate
of every single name.

| Type | Exported members that stay | Why |
|---|---|---|
| `core.App` | `ServeHTTP`, `Create`, `BindGlobalMiddlewares`, `BindGlobalGuards`, `BindGlobalInterceptors`, `BindGlobalExceptionFilters`, `EnableVersioning`, `EnableWS`, `EnableAccessLog`, `EnableDevtool`, `SetLogger`, `Logger`, `Get`, `Listen`, `ListenTLS`, `Serve`, `Stop` | The application's whole life. `ServeHTTP` is the documented escape hatch (`App` is an `http.Handler`) |
| `core.Module` | `Name`, `IsGlobal`, `OnInit`, `OnReady`, `OnShutdown`, `Prefix` | What a module author sets after `Build()` |
| `core.ModuleBuilder` (was the unexported `moduleBuilder`) | `Imports`, `Providers`, `Controllers`, `Build` | The builder chain |
| `core.Controller`, `core.Provider` | `NewController`, `NewProvider` | Implemented by every controller and provider |
| `core.ServerOptions` | `ShutdownTimeout`, `MaxRequestBodyBytes`, `TLSConfig`, `IsH2C` | Server settings |
| `core.WSOptions` | `Path` | WebSocket endpoint path |
| `common.HTTP` | `Prefix` | Controller route prefix |
| `common.WS` | none | Marker |
| `common.Guard`, `Interceptor`, `ExceptionFilter`, `Middleware` | `BindGuard`, `BindInterceptor`, `BindExceptionFilter`, `BindMiddleware` | Bind a layer to handlers |
| `common.MiddlewareFn` | `Use` | Implemented by every middleware |
| `common.ArgumentMetadata` | `ContextType`, `ParamType` | Tell a pipe its transport and source |
| `common.Publisher` | `Publish` | Publish to a topic |
| `ctx.HTTPContext` | embedded `Request`, `ResponseWriter`; `ID`, `Status`, `Text`, `JSON`, `JSONP`, `Redirect`, `Body`, `Form`, `File`, `Query`, `Header`, `Param` | Read the request, write the response |
| `ctx.WSContext` | embedded `Conn`; `ID`, `Context`, `SetContext`, `WSPayload`, `Send` | Read the message, answer the connection, carry values |
| `ctx.Body` | `Get`, `Set`, `Del`, `Has`, `Bind`, `BindValue` | Read and bind a JSON body |
| `ctx.Form`, `ctx.Query`, `ctx.Header`, `ctx.Param` | `Get`, `Set`, `Add`, `Del`, `Has`, `Bind`, `BindValue` | Read and bind string-array sources |
| `ctx.File` | `Bind` | Bind uploaded files |
| `ctx.WSPayload` | `Bind`, `BindValue` | Bind a message |
| `ctx.FieldLevel` | `Tag`, `NestedTag`, `Namespace`, `Field`, `Index`, `Value`, `IsValue`, `Kind`, `Type` | Validation of one bound field |
| `ctx.DataFile` | embedded `FileHeader`; `Index`, `Size`, `Total`, `Key`, `Filename`, `Type`, `Dest` | One uploaded file |
| `aggregation.Aggregation` | `Pipe`, `Transform`, `Tap`, `Filter` | Post-process a handler result in an interceptor |
| `aggregation.Operator` | none | The value `Transform`, `Tap` and `Filter` return and `Pipe` takes; never named |
| `versioning.Options` | `Type`, `Key`, `DefaultVersion`, `Extractor` | Versioning settings |

Signatures that change on these types:

```go
func (app *App) EnableVersioning(opts *versioning.Options) *App
func (app *App) EnableWS(opts *WSOptions, middlewares ...common.MiddlewareFn) *App
func (app *App) SetLogger(logger log.LogAdapter) *App
func (app *App) Logger() log.Logger
func (app *App) BindGlobalGuards(guards ...any) *App
func (app *App) BindGlobalInterceptors(interceptors ...any) *App
func (app *App) BindGlobalExceptionFilters(exceptionFilters ...any) *App
func (g *Guard) BindGuard(guard any, handlers ...any) *Guard
func (i *Interceptor) BindInterceptor(interceptor any, handlers ...any) *Interceptor
func (e *ExceptionFilter) BindExceptionFilter(exceptionFilter any, handlers ...any) *ExceptionFilter
func (c *HTTPContext) ID() string
func (c *WSContext) ID() string
```

### Public packages that stay

Every name of these packages stays exported; the table lists the reason per group and every change.
"Forced" marks a name users don't call that Go or the container requires to be exported.

| Package | Names (before → after) | Stay because | Changes |
|---|---|---|---|
| `exception` | 43 → 43 | `Exception` is what handlers panic with and filters catch; `NewException` and the 31 `<Status>Exception` constructors create it; `ExceptionOptions` (`Description`, `Cause`) sets its details; `Error`, `Unwrap`, `MarshalJSON`, `AppendJSON` make it an error that encodes itself | `GetCode` → `Code`, `GetMessage` → `Message`, `GetStatusText` → `StatusText` |
| `log` | 15 → 24 | `NewLog` and `LogOptions` configure the built-in logger; `WrapLogger` adds masking to a user's logger; the level and format constants are the values of `LogOptions` | `Logger` (`From`) and `LogAdapter` (`Debug`, `Info`, `Warn`, `Error`, `Fatal`) move in from `common`; new type `Format` (was the unexported `logFormat`); `LogOptions.LogFormat` → `Format`, `AddSource` → `IsAddSource`; `DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel`, `FatalLevel` → `LevelDebug`, `LevelInfo`, `LevelWarn`, `LevelError`, `LevelFatal`; `TextFormat`, `JSONFormat`, `PrettyFormat` → `FormatText`, `FormatJSON`, `FormatPretty` |
| `middlewares/compress`, `cors`, `csrf`, `helmet`, `hostcheck`, `httpsredirect`, `loadshed` | 4, 10, 4, 12, 4, 5, 5 → same | The option type and its fields are the middleware's settings; `NewMiddleware` is called by the container (forced); `Use` makes the option type a `MiddlewareFn` so it can be bound (forced); `helmet.Disabled` turns one header off | none |
| `middlewares/proxy` | 13 → 13 | As above, plus `ClientIP`, `Host`, `Scheme` read the trusted values in application code | `Forwarded` → `HeaderForwarded`, `XForwarded` → `HeaderXForwarded` |
| `modules/cache` | 28 → 32 | `Register` and `CacheModuleOptions` configure the module; `CacheService` and its 7 methods are what applications inject; `Cache` is the backend extension point; the 4 `Err…` sentinels are what callers compare; `CacheOnInitFn` types `OnInit`; `CacheService.Backend` and `NewProvider` are forced | added `NewMemoryCache`, `MemoryCacheOptions` (`MaxEntries`, `MaxBytes`) |
| `modules/config` | 19 → 19 | `Register` and `ConfigModuleOptions` configure the module; `ConfigService` (`Get`, `Set`, `Transform`) is what applications inject; `ConfigLoadFn`, `ConfigHookFn`, `ConfigOnInitFn` type the options; `ConfigService.State` and `NewProvider` are forced | `ENVFilePaths` → `EnvFilePaths` |
| `guards/throttler` | 11 → 11 | `Throttler` and its 5 fields are the guard's settings; `Strategy` and its constants are the values of one; `NewGuard` is forced | `FixedWindow`, `SlidingWindow`, `TokenBucket` → `StrategyFixedWindow`, `StrategySlidingWindow`, `StrategyTokenBucket`; `NewGuard()` returns `any` |

Names added outside the root package, with their reason:

```go
package log

type Format int
type LogAdapter interface { Debug, Info, Warn, Error, Fatal (msg string, args ...any) }
type Logger interface { LogAdapter; From(ctx context.Context) Logger }
func NewLog(opts *LogOptions) Logger
func WrapLogger(next LogAdapter, maskFields []string) Logger

package cache

type MemoryCacheOptions = memorycache.MemoryCacheOptions
func NewMemoryCache(opts *MemoryCacheOptions) Cache
```

| Added name | Why |
|---|---|
| `log.Logger`, `log.LogAdapter` | Moved, not new. `log` is where users already create and wrap loggers, and `common` is no longer importable |
| `log.Format` | An enum-like constant needs a type users can name (a config struct field) |
| `cache.NewMemoryCache`, `cache.MemoryCacheOptions` | Today a small machine bounds the default cache with `memorycache.NewMemoryCache(&memorycache.MemoryCacheOptions{MaxEntries: …})` as `Backend`. `memorycache` moves to `internal/`, so the same call needs a public spelling |
| `owlky.VersioningType` | Same reason as `log.Format` |

### Package moves (step 4)

A move changes the directory and the import path, nothing else. Tests, benchmarks and the README move with
the package.

| Package | To | Reached by users through | Names (before → reachable after) |
|---|---|---|---|
| `pattern` | `internal/pattern` | nothing | 12 → 0 |
| `wsevent` | `internal/wsevent` | nothing | 9 → 0 |
| `memorybroker` | `internal/memorybroker` | nothing (`owlky.Publisher`) | 26 → 0 |
| `routing` | `internal/routing` | nothing | 27 → 0 |
| `devtool` | `internal/devtool` | `App.EnableDevtool` | 55 → 0 |
| `accesslog` | `internal/accesslog` | `App.EnableAccessLog` | 6 → 0 |
| `trace` | `internal/trace` | `owlky.TraceTransport` | 28 → 1 |
| `memorycache` | `internal/memorycache` | `cache.NewMemoryCache`, `cache.MemoryCacheOptions` | 16 → 4 |
| `aggregation` | `internal/aggregation` | `owlky.Aggregation` | 12 → 6 |
| `versioning` | `internal/versioning` | `owlky.VersioningOptions`, `VersioningType`, 4 constants | 13 → 9 |
| `common` | `internal/common` | 9 root aliases; `Logger`, `LogAdapter` go to `log` | 173 → 18 (+ 8 in `log`) |
| `ctx` | `internal/ctx` | 14 root aliases | 130 → 88 |
| `core` | `internal/core` | `owlky.New`, `ModuleBuilder`, 6 root aliases | 75 → 42 |

### Hooks (step 5)

Each row replaces exported members by an unexported field and package-level functions of the same internal
package. Callers change from `x.M(args)` to `pkg.F(x, args)` and from `x.Field` to `pkg.F(x)`. `core`'s
`httpExchange` embeds the HTTP context: its promoted uses (`ex.Next`, `ex.Code`, `ex.Timestamp`,
`ex.ParamValues`, `ex.GetID()`, `ex.HTTPContext.Reset()`) change the same way.

`internal/ctx`:

| Today | After |
|---|---|
| `(*HTTPContext).Init(w, r)` | `func HTTPInit(c *HTTPContext, w http.ResponseWriter, r *http.Request)` |
| `(*HTTPContext).Reset()` | `func HTTPReset(c *HTTPContext)` |
| fields `ParamKeys`, `ParamValues` | fields `paramKeys`, `paramValues`; `func HTTPSetParams(c *HTTPContext, keys map[string][]int, values []string)`; `func HTTPParamValues(c *HTTPContext) []string` |
| field `HTTPContext.Next` | field `next`; `func HTTPNext(c *HTTPContext) Next`; `func HTTPSetNext(c *HTTPContext, next Next)` |
| field `HTTPContext.Code` | field `code`; `func HTTPCode(c *HTTPContext) int`; `func HTTPSetCode(c *HTTPContext, code int)` |
| field `HTTPContext.Timestamp` | field `timestamp`; `func HTTPTimestamp(c *HTTPContext) *time.Time` |
| `(*HTTPContext).AddAggregation(a)`, `Aggregations()` | `func HTTPAddAggregation(c *HTTPContext, a *aggregation.Aggregation)`; `func HTTPAggregations(c *HTTPContext) []*aggregation.Aggregation` |
| `(*WSContext).Init(conn)`, `Reset()` | `func WSInit(c *WSContext, conn *websocket.Conn)`; `func WSReset(c *WSContext)` |
| field `WSContext.Next` | field `next`; `func WSNext(c *WSContext) Next`; `func WSSetNext(c *WSContext, next Next)` |
| field `WSContext.Timestamp` | field `timestamp`; `func WSTimestamp(c *WSContext) time.Time` |
| `(*WSContext).SetSend(fn)`, `SetWSPayload(p)` | `func WSSetSend(c *WSContext, fn func(data any))`; `func WSSetPayload(c *WSContext, p WSPayload)` |
| `(*WSContext).AddAggregation(a)`, `Aggregations()` | `func WSAddAggregation(c *WSContext, a *aggregation.Aggregation)`; `func WSAggregations(c *WSContext) []*aggregation.Aggregation` |
| `Context` interface `{ Reset(); GetID() string; Context() context.Context }` | `{ ID() string; Context() context.Context }` |
| `ErrFunc` | removed (unused) |

`internal/aggregation`:

| Today | After |
|---|---|
| fields `Name`, `InterceptorData`, `IsMainHandlerCalled` | fields `name`, `interceptorData`, `isMainHandlerCalled`; `func SetName(a *Aggregation, name string)`; `func Name(a *Aggregation) string`; `func SetInterceptorData(a *Aggregation, data any)`; `func InterceptorData(a *Aggregation) any`; `func IsMainHandlerCalled(a *Aggregation) bool` |
| `(*Aggregation).SetMainData(d)`, `Aggregate()` | `func SetMainData(a *Aggregation, d any)`; `func Aggregate(a *Aggregation) any` |

`internal/common`:

| Today | After |
|---|---|
| fields `HTTP.PatternToFuncNameMap`, `FuncNameToPatternMap`, `RouterMap` | fields `patternToFuncNameMap`, `funcNameToPatternMap`, `routerMap`; `func HTTPPatternToFuncNameMap(h *HTTP) map[string]string`; `func HTTPFuncNameToPatternMap(h *HTTP) map[string]string`; `func HTTPRouterMap(h *HTTP) map[string]any` |
| `(*HTTP).AddHandlerToRouterMap(modulePrefixes, fnName, handler)` | `func AddHandlerToRouterMap(h *HTTP, modulePrefixes []string, fnName string, handler any)` |
| `(*HTTP).GetPrefixes()` | unexported method `getPrefixes` |
| field `WS.EventMap` | field `eventMap`; `func WSEventMap(ws *WS) map[string]any` |
| `(*WS).AddHandlerToEventMap(fnName, handler)` | `func AddHandlerToEventMap(ws *WS, fnName string, handler any)` |
| fields `Guard.GuardHandlers`, `Interceptor.InterceptorHandlers`, `ExceptionFilter.ExceptionFilterHandlers`, `Middleware.MiddlewareHandlers` | fields `guardHandlers`, `interceptorHandlers`, `exceptionFilterHandlers`, `middlewareHandlers` |
| `(*Guard).InjectProvidersIntoHTTPGuards(r, cb, construct)`, `InjectProvidersIntoWSGuards(ws, cb, construct)` | `func InjectProvidersIntoHTTPGuards(g *Guard, r *HTTP, cb …, construct …) []GuardItem`; `func InjectProvidersIntoWSGuards(g *Guard, ws *WS, cb …, construct …) []GuardItem` |
| `(*Interceptor).InjectProvidersIntoHTTPInterceptors`, `InjectProvidersIntoWSInterceptors` | the same two functions with `i *Interceptor` as first parameter |
| `(*ExceptionFilter).InjectProvidersIntoHTTPExceptionFilters`, `InjectProvidersIntoWSExceptionFilters` | the same two functions with `e *ExceptionFilter` as first parameter |
| `(*Middleware).InjectProvidersIntoHTTPMiddlewares` | the same function with `m *Middleware` as first parameter |
| `Guarder`, `Interceptable`, `ExceptionFilterable` | removed; every use is `any` |
| `Logger`, `LogAdapter` | move to `log` |

`internal/versioning` and `internal/core`:

| Today | After |
|---|---|
| `versioning.Versioning` | `versioning.Options`, field `Type` of the new `type Type int` |
| `QueryVersion`, `HeaderVersion`, `CustomVersion`, `MediaType` | `TypeQuery`, `TypeHeader`, `TypeCustom`, `TypeMediaType` (same values 1 to 4) |
| `(*Versioning).GetTypeString()`, `GetVersion(c)` | `func TypeString(o *Options) string`; `func Version(o *Options, c *ctx.HTTPContext) string` |
| `core.ModuleBuilder()` returning `*moduleBuilder` | `type ModuleBuilder struct`; `func NewModuleBuilder() *ModuleBuilder` (the root `ModuleBuilder()` calls it) |
| `(*core.Module).ID()` | removed (unused; the field `id` stays) |
| `core.WSOptions`, parameter `cfg` | `core.WSOptions`, parameter `opts` |
| `core.loggerKey`, `core.publisherKey` string literals | `genFieldKey(reflect.TypeFor[log.Logger]())`, `genFieldKey(reflect.TypeFor[common.Publisher]())` |
| `accesslog.AccessLogConfig`, `NewAccessLog(cfg)` | `AccessLogOptions`, `NewAccessLog(opts *AccessLogOptions)` |

Every other exported name of the moved packages (`core.HTTP`, `core.WS`, `core.NewWS`, `core.WSConnmgr`,
the `core.Type…` message types, `ctx.BinderOf`, `ctx.NewHTTPContext`, the `common` items, layers and DSL
tables, `routing`, `wsevent`, `devtool`…) keeps its name: the path hides it and the API check proves it is
not reachable. The WebSocket names are not renamed here; roadmap 0006 owns them.

### Duplicate ways

| # | Two ways today | Survives | Removed | Test that the kept way covers the removed one |
|---|---|---|---|---|
| 1 | `owlky.Request` and `*http.Request` as a handler parameter | `*http.Request` | the alias | `TestHTTPHandlerPlan_InjectsRequest` (`core/fn_plan_test.go`) |
| 2 | `owlky.Response` and `http.ResponseWriter` | `http.ResponseWriter` | the alias | `TestHTTPHandlerPlan_InjectsResponseWriter` |
| 3 | `owlky.Exception` and `*exception.Exception` in `Catch` | `*exception.Exception` | the alias | new `TestSurface_FilterCatchesExceptionPointer` |
| 4 | `c.Code = 201` and `c.Status(201)` | `Status` | the exported field | `TestHTTPContext_Status` (`ctx/http_context_test.go`), new `TestHTTPContext_StatusSetsWrittenCode` |
| 5 | `c.Next()` on a context and a `Next` handler parameter | the parameter | the exported fields | `TestHTTPHandlerPlan_InjectsNext` |
| 6 | `common.Guarder`, `Interceptable`, `ExceptionFilterable` and `any` | `any` | the three types | new `TestBindGuard_AcceptsAnyShape`, existing shape tests of `common` |
| 7 | `versioning.Versioning{…}` by value and an options pointer | `*VersioningOptions` | the value parameter | `TestVersioning_ExactThenNeutral` (`core/http_versioning_test.go`), new `TestEnableVersioning_NilIsZeroOptions` |
| 8 | `core.Module.ID()` and nothing (unused) | nothing | the method | none needed |

Looked at and kept on purpose:

| Pair | Why both stay |
|---|---|
| `Bind` and `BindValue` (6 types) | `Bind` returns the field levels for validation and allocates their slice; `BindValue` allocates nothing extra. Removing `BindValue` adds one allocation per request to every pipe that does not validate by field level. `Test…_BindValueEqualsBind` (6 tests in `ctx/binding_hostile_test.go`) pin them to the same value |
| `<Middleware>.Use` and `NewMiddleware().Use` | Go needs `Use` on the option type for it to be a `MiddlewareFn` |
| `Map` and `map[string]any` | `Map` is the documented result type; both encode the same way |
| The four option forms of `exception.NewException` (`string`, `error`, `ExceptionOptions`, `*ExceptionOptions`) | They are not the same job: the `error` form makes `Error()` the cause's text, the options form joins it to the description. Open question 3 |

## Naming rules

| Kind | Rule | Applied by this design |
|---|---|---|
| Settings struct | `XxxOptions`, `XxxModuleOptions` for a module; passed as `opts *XxxOptions`; `nil` means defaults (D-032). `Config` is reserved for `modules/config` | `WSOptions` → `WSOptions`, `AccessLogConfig` → `AccessLogOptions`, `Versioning` → `VersioningOptions` (pointer parameter). Known exception until roadmap 0006: `EnableWS(nil)` still fails (backlog) |
| Layer (middleware, guard, interceptor, filter) | A value type whose exported fields are its options; no options struct, no constructor; the container calls `New<Layer>()` | none (already so) |
| Option boolean | Starts with `Is` | `H2C` → `IsH2C`, `AddSource` → `IsAddSource` |
| Constructor | `New<Type>`; `New` for the package's main type; a builder entry is `<Type>Builder()`; a dynamic module is `Register(opts)` | none on the surface; `core.NewModuleBuilder` inside |
| Accessor | A bare noun, no `Get`. `Get(key)` is a keyed lookup only | `GetID` → `ID` (2), `GetCode` → `Code`, `GetMessage` → `Message`, `GetStatusText` → `StatusText` |
| Enum-like constant | A named, exported type; constants `<Type><Value>`, like `slog.LevelDebug` and `http.MethodGet`. In the root package the prefix is the feature (`Versioning…`) | `log` levels and formats, `throttler` strategies, `proxy` headers, the versioning types; type `log.Format` |
| Field | No package or type stutter; `Env`, not `ENV` (it is not an initialism) | `LogOptions.LogFormat` → `Format`, `ENVFilePaths` → `EnvFilePaths` |
| Function type | An alias `<Owner><What>Fn` (`CacheOnInitFn`, `ConfigLoadFn`) | none. `MiddlewareFn` is an interface and keeps its name: open question 4 |
| Lifecycle event | A field `On<Event>` of type `func()` (`OnInit`, `OnReady`, `OnShutdown`) | none |
| Log event (D-028) | `msg` is a fixed PascalCase name, details in key-value pairs (`WSReadFailed`) | none |
| Sentinel error | `Err<Reason>`, message `"<package the user imports>: <reason>"`, lower case, no final period | B9: `core:` → `owlky:`, `ctx:` → `owlky:`, `memorycache:` → `cache:` |
| Exception | Constructor `<Status>Exception(message, opts...)` | none |
| Bootstrap panic | `"<category>: <detail>"`; a type users write is spelled as they write it | B8: six messages say `owlky.HTTPContext` / `owlky.WSContext` |

D-028 rule 4 (client messages never carry internal error text) is not applied here: it changes responses,
and the SSR roadmap decides error output for pages and APIs together.

## API guard (step 3)

| Item | Decision |
|---|---|
| Tool | `internal/apicheck` (package `main`), standard library only: `os/exec` (`go list -export -deps -json ./...`), `go/importer` (`ForCompiler(fset, "gc", lookup)` reading each package's export file), `go/types`. No new module dependency |
| File | `api/owlky.txt`, committed, sorted, one name per line |
| Commands | `make api` writes the file (`go run ./internal/apicheck -write`). `make api-check` compares (`go run ./internal/apicheck`), prints the differing lines with `+` / `-` and exits 1. `make check` runs `api-check` after `go vet`; the CI `lint` job runs it too |
| Public packages | Packages of the module without an `internal`, `sample` or `bench` path element |
| Lines of a public package | `pkg <path>, const <Name> <type> = <value>` · `var <Name> <type>` · `func <Name><signature>` · `type <Name> <kind>` (`struct`, `interface`, `= <target>` for an alias, else the underlying type) · `type <Name> struct, <Field> <type>` (`embedded` when it is) · `type <Name> interface, <Method><signature>` · `method (<receiver>) <Name><signature>` |
| Reachable lines | For every named type of this module, in any package, that a listed line mentions (alias target, parameter, result, field), and transitively: `reach <pkg>.<Type>, field <Name> <type>` and `reach <pkg>.<Type>, method (<receiver>) <Name><signature>`. Types of other modules and of the standard library are not expanded |
| Signatures | Types are printed with their package name; parameter names are dropped |
| A change is | An added, removed or different line: a new exported name, a new member of a reachable type, a changed type, signature, alias target or constant value |
| A change is not | Parameter names, comments, struct tags, unexported names, anything in `internal/` that no public line reaches |
| Allowed packages | The tool also fails on a public package outside the list of "Import paths after the change" (`middlewares/*`, `modules/*`, `guards/*` by prefix). Until step 4 ends the list also holds the packages that have not moved yet; each move removes one |
| Cost | Runs at check time only: nothing in the binary, no cost at run time. The inventory prototype (source importer, three passes) took 17 s; export data needs one `go list` and no type-checking of dependencies. Step 3 measures it. It runs in `make check` and CI, not in the pre-commit hook |

Example lines:

```
pkg owlky, const VersioningHeader versioning.Type = 2
pkg owlky, func New() *core.App
pkg owlky, type HTTPContext = *ctx.HTTPContext
pkg owlky/log, type LogOptions struct, Format log.Format
pkg owlky/exception, method (exception.Exception) Code() int
reach core.App, method (*core.App) Listen(int, *core.ServerOptions) error
reach ctx.HTTPContext, field Request *http.Request embedded
```

## Example application on the new surface

```go
package main

import (
	"net/http"
	"strings"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
	"go.owlky.com/owlky/middlewares/helmet"
)

type NoteService struct{}

func (instance NoteService) NewProvider() owlky.Provider {
	return instance
}

func (instance NoteService) Find(id string) (string, bool) {
	return "note " + id, id != "0"
}

type NoteBody struct {
	Text string `bind:"text"`
}

func (instance NoteBody) Transform(body owlky.Body, _ owlky.ArgumentMetadata) any {
	note := body.BindValue(instance).(NoteBody)
	if strings.TrimSpace(note.Text) == "" {
		panic(exception.BadRequestException("text is required"))
	}
	return note
}

type TokenGuard struct{}

func (instance TokenGuard) CanActivate(c owlky.HTTPContext) bool {
	return c.Header().Get("Authorization") != ""
}

type RequestLog struct {
	Logger log.Logger
}

func (instance RequestLog) Use(r *http.Request, w http.ResponseWriter, next owlky.Next) {
	instance.Logger.From(r.Context()).Info("Request", "path", r.URL.Path)
	next()
}

type NoteController struct {
	owlky.HTTP
	owlky.Guard
	NoteService NoteService
}

func (instance NoteController) NewController() owlky.Controller {
	instance.BindGuard(TokenGuard{}, instance.CREATE_notes)
	return instance
}

func (instance NoteController) READ_notes_BY_id(param owlky.Param) owlky.Map {
	text, ok := instance.NoteService.Find(param.Get("id"))
	if !ok {
		panic(exception.NotFoundException("note not found"))
	}
	return owlky.Map{"text": text}
}

func (instance NoteController) CREATE_notes(note NoteBody, publisher owlky.Publisher) owlky.Map {
	if err := publisher.Publish("notes.created", owlky.Map{"text": note.Text}); err != nil {
		panic(err)
	}
	return owlky.Map{"text": note.Text}
}

type NoteGateway struct {
	owlky.WS
}

func (instance NoteGateway) NewController() owlky.Controller {
	return instance
}

func (instance NoteGateway) SUBSCRIBE_notes_ANY(payload owlky.WSPayload) owlky.WSPayload {
	return payload
}

var NoteModule = func() owlky.Module {
	return owlky.ModuleBuilder().
		Providers(NoteService{}).
		Controllers(NoteController{}, NoteGateway{}).
		Build()
}

func main() {
	app := owlky.New()
	app.SetLogger(log.NewLog(&log.LogOptions{Level: log.LevelInfo, Format: log.FormatJSON}))
	app.BindGlobalMiddlewares(helmet.Helmet{}, RequestLog{})
	app.EnableWS(&owlky.WSOptions{Path: "ws"})
	app.Create(owlky.ModuleBuilder().Imports(NoteModule).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

| Piece | In the example |
|---|---|
| Provider | `NoteService` |
| DTO binding and exception | `NoteBody.Transform`, `exception.BadRequestException` |
| Guard | `TokenGuard`, bound with `BindGuard` |
| Middleware | `RequestLog` (own), `helmet.Helmet` (built-in) |
| Controller | `NoteController` |
| WebSocket gateway | `NoteGateway`, fed by `publisher.Publish` |
| Module | `NoteModule` |

## Behavior

Surface:

- **B1.** The importable packages of the module are exactly those of "Import paths after the change".
  `go run ./internal/apicheck` exits 1 with `api: package <path> is importable: move it under internal/ or add
  it to the allow list` for any other package outside `internal/`, `sample/` and `bench/`.
- **B2.** With `api/owlky.txt` up to date, `go run ./internal/apicheck` exits 0 and prints nothing. After an
  exported function `Foo()` is added to `log`, it exits 1 and prints `+ pkg owlky/log, func Foo()`. After a
  parameter is renamed or a comment edited, it exits 0.
- **B3.** After an exported method `Foo()` is added to `internal/ctx.HTTPContext`, the check exits 1 and
  prints `+ reach ctx.HTTPContext, method (*ctx.HTTPContext) Foo()`. After an exported function is added to
  `internal/ctx`, it exits 0.
- **B4.** No file under `sample/` or `bench/` imports a path with an `internal` element or a moved package.
- **B5.** The method set of `owlky.HTTPContext` has no `Init`, `Reset`, `AddAggregation`, `Aggregations` or
  `GetID`, and its struct has no exported field other than `Request` and `ResponseWriter`. The method set of
  `owlky.WSContext` has no `Init`, `Reset`, `AddAggregation`, `Aggregations`, `SetSend`, `SetWSPayload` or
  `GetID`, and its only exported field is `Conn`. `owlky.Aggregation` has exactly the methods `Pipe`,
  `Transform`, `Tap`, `Filter` and no exported field.
- **B6.** The exported methods of `*owlky.HTTP`, `*owlky.WS`, `*owlky.Guard`, `*owlky.Interceptor`,
  `*owlky.ExceptionFilter`, `*owlky.Middleware` are exactly `Prefix`, none, `BindGuard`,
  `BindInterceptor`, `BindExceptionFilter`, `BindMiddleware`; none of the six has an exported field.

Wiring through the root names (unchanged behavior, new spelling):

- **B7.** A controller that embeds `owlky.HTTP` and has `READ_hello() string` answers `GET /hello` with
  `hello`. One that embeds `owlky.WS` with `SUBSCRIBE_chat_ANY` handles a `publish` to `chat.room1`.
- **B8.** A controller that embeds `owlky.Guard` and calls `BindGuard(g, instance.READ_x)` runs `g` on that
  route only; the same for `Interceptor`, `ExceptionFilter`, `Middleware`.
- **B9.** `Imports` accepts what it accepts today, spelled with the root names: a `owlky.Module` value (a
  `Register(…)` result) and a function whose result is `owlky.Module` (called once, its parameters
  resolved from global providers). Any other value panics with `invalid module: can't pass '<type>' type as
  module`. For `var M = func() owlky.Module { return owlky.ModuleBuilder().Build() }` the module's
  resolved `Name` is `<package>.M`.
- **B10.** An exported field of type `log.Logger` on a controller, provider or layer receives the app's
  logger (the one given to `SetLogger`); a field of type `owlky.Publisher` receives the app's publisher.
- **B11.** A handler parameter of each root alias type (`owlky.HTTPContext`, `Body`, `Form`, `Query`,
  `Header`, `Param`, `File`, `Next`, `Redirect`, `Publisher`, `WSContext`, `WSPayload`) is injected as
  today.

Changed text and values:

- **B12.** A layer with a wrong method shape panics at `Create()` with (the six messages of
  `common/*_guard.go`, `*_interceptor.go`, `*_exception_filter.go`):
  - `invalid guard: <type>.CanActivate must be func(owlky.HTTPContext) bool to be bound as a HTTP guard`
  - `invalid guard: <type>.CanActivate must be func(owlky.WSContext) bool to be bound as a WS guard`
  - `invalid interceptor: <type>.Intercept must be func(owlky.HTTPContext, owlky.Aggregation) any to be bound as a HTTP interceptor`
  - `invalid interceptor: <type>.Intercept must be func(owlky.WSContext, owlky.Aggregation) any to be bound as a WS interceptor`
  - `invalid exception filter: <type>.Catch must be func(owlky.HTTPContext, *exception.Exception) to be bound as a HTTP exception filter`
  - `invalid exception filter: <type>.Catch must be func(owlky.WSContext, *exception.Exception) to be bound as a WS exception filter`

  Messages that print a type through reflection (`dependency injection: …`) still print `*ctx.HTTPContext`,
  `ctx.Body`, `*core.Module`: the package names are unchanged (D4).
- **B13.** Errors that reach application code carry the prefix of the package the application imports:
  - `Listen`, `ListenTLS`, `Serve`: `owlky: Create must be called before serving`, `owlky: app is already
    serving`, `owlky: app is stopped`, `owlky: TLSConfig is set: use ListenTLS, or Serve with your
    listener`, `owlky: IsH2C applies to connections without TLS`, `owlky: ListenTLS needs certFile and
    keyFile, or a TLSConfig with certificates`, `owlky: load TLS certificate: <err>`, `owlky: listen on
    <address>: <err>`, `owlky: serve: <err>`, `owlky: shutdown timed out after <timeout>`.
  - A response writer used after its request ended: `owlky: response writer used after the request ended`.
  - `Bind` / `BindValue` on a non-struct target panic with an error whose text starts with `owlky: bind
    target must be a struct`.
  - `cache.ErrEmptyKey`, `ErrNegativeTTL`, `ErrEntryTooLarge`, `ErrNotCounter`: `cache: key must not be
    empty`, `cache: ttl must not be negative`, `cache: entry is larger than the cache allows`, `cache: value
    is not a counter`. `errors.Is` against the four sentinels works as before.
- **B14.** `EnableVersioning(nil)` behaves as `EnableVersioning(&owlky.VersioningOptions{})`, which behaves
  as today's `EnableVersioning(versioning.Versioning{})` (`Key` `"v"`, no version source). With
  `Type: owlky.VersioningHeader`, `Key: "X-Api-Version"`, `DefaultVersion: "1"`, routing is today's. The
  options are copied: changing the caller's struct after the call changes nothing.
- **B15.** Constant values do not change: `VersioningQuery` 1, `VersioningHeader` 2, `VersioningCustom` 3,
  `VersioningMediaType` 4; `throttler.StrategyFixedWindow` 0, `StrategySlidingWindow` 1,
  `StrategyTokenBucket` 2; `proxy.HeaderXForwarded` 0, `HeaderForwarded` 1; `log.LevelDebug` to `LevelError`
  equal `slog.LevelDebug` to `slog.LevelError`, `log.LevelFatal` is `slog.Level(12)`; the zero `log.Format`
  is still the default format. The devtool snapshot's versioning `Type` strings (`query`, `header`, `custom`,
  `media_type`) do not change.
- **B16.** `Exception.Code()`, `Message()` and `StatusText()` return what `GetCode()`, `GetMessage()` and
  `GetStatusText()` returned. `HTTPContext.ID()` and `WSContext.ID()` return what `GetID()` returned.
- **B17.** `c.Status(201)` followed by `c.JSON(v)` writes status 201; a handler result still writes 200, and
  201 for `CREATE`. Nothing outside `internal/` can set the code another way.

Moved code keeps its behavior:

- **B18.** Each hook function returns or does what the member it replaces did: after `ctx.HTTPReset(c)` the
  context equals a context after today's `Reset()`; `ctx.HTTPSetParams` followed by `c.Param()` gives today's
  parameters; `aggregation.Aggregate(a)` after `aggregation.SetMainData(a, d)` gives today's
  `a.SetMainData(d).Aggregate()`.
- **B19.** `cache.NewMemoryCache(nil)` returns a `Cache` with the defaults `Register(nil)` uses.
  `cache.NewMemoryCache(&cache.MemoryCacheOptions{MaxEntries: 1})` holds one entry: a second `Set` evicts the
  first.
- **B20.** `owlky.TraceTransport(base)` returns what `trace.Transport(base)` returned: `nil` uses
  `http.DefaultTransport`, a request in a traced context gets `Traceparent`, an existing header is kept.
- **B21.** The container's keys for `log.Logger` and `common.Publisher` equal `genFieldKey` of their
  reflected types: no string literal of an import path is left in non-test code.
- **B22.** The devtool snapshot of an app is byte for byte the one before the change (same controller, same
  layers, versioning on and off).

## Performance

| Path | Change | Expected cost |
|---|---|---|
| Hot: `core/http.go`, `core/app.go`, `core/read_loop.go`, `core/fn_plan.go` | 20 `ctx` hooks and 7 `aggregation` hooks are called as functions instead of methods and field accesses | None: a method and a function with the receiver as first parameter compile to the same code, and each hook is a leaf small enough to inline. Step 5 checks `go build -gcflags=-m` for `can inline` on each hook, and that no caller that was inlined stops being inlined |
| Hot: everything else | Import paths and names only | None |
| Cold: `Create()` | Two injection keys computed by reflection once per app instead of two constants; `common` hooks called as functions | Two `reflect.TypeFor` calls per `New()` |
| Build time | `internal/apicheck` in `make check` | Measured in step 3 |

No `unsafe`, `sync.Pool`, manual buffer or inlining restructure is added. Step 8 of the roadmap compares
`make bench` with `docs/review/http-0006.md`. Fallback, so the builder is never blocked: if a hook cannot be
inlined or a scenario leaves the noise band, that one member stays an exported method or field, is listed in
`api/owlky.txt` as reachable and is reported in the roadmap's Log. Idle and per-request memory do not
change.

## Concurrency

Nothing new: no goroutine, lock or shared state is added or removed. The hooks touch the same fields from
the same goroutines as the members they replace. `internal/apicheck` is a single-goroutine command.

## Security

- Hiding `Init`, `Reset`, `ParamKeys`, `ParamValues`, `SetSend` and `SetWSPayload` removes a way for
  application code to corrupt a pooled context (a `Reset()` in a handler made the next request reuse a
  context the handler still held).
- `internal/` packages cannot be imported by other modules: third-party code cannot reach the router, the
  container or the broker.
- No limit, validation or default changes. B13 changes error prefixes only; no message gains request data.
- `api/owlky.txt` holds signatures only.

## Impact

| What | Change |
|---|---|
| Applications | Every import of `core`, `common`, `ctx`, `aggregation`, `versioning`, `trace`, `memorycache` becomes the root package (or `log`, `modules/cache`). Not published yet: no aliases for old names |
| `sample/` | Imports only the public paths; `internal/crypto` is replaced by a helper in `sample/shared` on `crypto/rand` |
| `bench/owlky.go` | Written against the root package |
| READMEs | Move with their package; step 7 rewrites the main README, the public packages' READMEs, `.owlky/` and `CLAUDE.md` (Architecture, Handler Injection, Bootstrap-time Error Convention paths) |
| `.owlky/conventions.md` | Gains the hook rule (D3), the enum rule, the accessor rule and the alias rule (D5); the `Is…` exceptions (`AddSource`) are gone |
| `Makefile`, `.github/workflows/ci.yml` | `api`, `api-check`; `check` and the `lint` job run the check |
| Tests | Internal tests move with their package; tests of `core` that read hook members call the hook functions; tests that assert B12 and B13 texts change |

## Order of steps 3 to 6 and risk

Each sub-step ends with `go build ./... && go test ./...` green on the touched packages and a regenerated
`api/owlky.txt` in the same commit, so the API diff of every sub-step is reviewable on its own.

| Step | Work | Kind | Risk |
|---|---|---|---|
| 3 | `internal/apicheck`, `make api`, first `api/owlky.txt` of today's surface, allow list with today's packages | new tool | Low. Done first so steps 4 to 6 are diffs of the file |
| 4a | Move `pattern`, `wsevent`, `memorybroker`, `routing`, `devtool`, `accesslog` | path only | Low. No name changes |
| 4b | Move `trace`, `memorycache`, `aggregation`, `versioning`; add `owlky.TraceTransport`, `cache.NewMemoryCache`, `cache.MemoryCacheOptions` | path, 3 names | Low |
| 4c | Move `common`, `ctx`, `core`; move `Logger` / `LogAdapter` to `log`; complete the root package; `sample/` and `bench/` on the public paths | path, root | **Medium**: rows 1 to 4 of the table below |
| 5 | Hooks (tables above), duplicate ways 1 to 8, `any` for the three layer types. Path `tdd`: the tester writes B5, B6, B17, B18 first | members | **Medium**: hot path (Performance), rows 5 and 6 |
| 6 | Naming pass: every rename of "Naming rules", B12, B13, typed versioning | names, text | Low to medium: rows 7 and 8 |

Changes that are behavior, not only names:

| # | Where | What depends on the name or path | Rule for the builder |
|---|---|---|---|
| 1 | `core/container.go` `fieldNameByRole` | The container finds a controller's `HTTP`, `WS`, `Guard`, `Interceptor`, `ExceptionFilter`, `Middleware` by **embedded field name**, then asserts the `common` type. An embedded alias takes the alias's name | The six root aliases keep exactly these names. B7 and B8 test them through `owlky.` |
| 2 | `core/module_builder.go` `isDynamicModule`, `core/container.go:255` | The printed type must end in `*core.Module` | The package stays named `core` (D4). B9 |
| 3 | `core/app.go` `loggerKey`, `core/fn.go` `publisherKey` | String literals of `…/owlky/common/common.Logger` and `…Publisher`; both paths change in 4c | Compute them (Hooks table). B10, B21 |
| 4 | `core/module_builder.go` `resolveModuleName` | `runtime.Callers(3, …)` counted from `Build()` | `owlky.ModuleBuilder()` wraps only the constructor; `Build` stays a method users call directly. B9 |
| 5 | `core/fn.go` `injectDependencies` | Panics on an unexported field of a controller, provider or layer, and copies each field by reflection | The embedded structs stay exported fields of the user's type; only their own fields become unexported. `CacheService.Backend` and `ConfigService.State` stay exported |
| 6 | Methods found by name or shape | `NewController`, `NewProvider`, `NewGuard`, `NewInterceptor`, `NewExceptionFilter`, `NewMiddleware`, `CanActivate`, `Intercept`, `Catch`, `Transform`, `Use`, `IsValid`, `Store`, `AppendJSON`, `Snapshot` | None is renamed |
| 7 | Route DSL | Operation and separator words in `common/http.go`, `common/ws.go`, `common/fn.go`, `routing` | Moved, not edited. No rename of this design touches a DSL word |
| 8 | Devtool snapshot, struct tags | Layer names (`reflect.Type.String()`), versioning type strings, pipe keys (`body`, `query`…), the `bind` and `log` tags, the exception JSON keys | Unchanged (D4, B15, B22). No tag is renamed |

## Room for the next roadmaps

| Need | Where it plugs in | What this design reserves |
|---|---|---|
| Render route kind: a controller role | A seventh embeddable found by field name, next to `HTTP` and `WS` (`fieldNameByRole`), exported as one root alias | The names `Page`, `View`, `Render`, `Template` in the root package |
| Render route kind: the DSL | A new operation word in the `common` operation table | `RENDER` is not used by any token today; a method `RENDER_x` is ignored, so adding it breaks no app |
| Render route kind: the result | A new case of the handler-result switch (`core.returnHTTP`) for a root type, or a method on `HTTPContext` | Adding a method to `ctx.HTTPContext` or a root type is one reviewed `+` line |
| Templates, layouts, assets | `app.EnableRender(opts *owlky.RenderOptions)`, by the `Enable…(opts *XxxOptions)` rule | The `Enable` prefix and `XxxOptions` naming |
| HTML or JSON errors per route kind | Inside the built-in exception filter and the HTTP layer's error writer | Nothing on the surface; D-028 rule 4 is decided there |
| Authentication guards | New packages `guards/<name>`: a value type with its options as fields, `NewGuard()`, `CanActivate(c owlky.HTTPContext) bool`, providers injected in exported fields | The import list already allows `guards/*` |
| The authenticated user in handlers | Today `c.Request = c.WithContext(…)` (HTTP) and `c.SetContext(…)` (WS), both kept. A typed handler parameter is one more row of the handler plan and one root alias | The names `Principal`, `Session`, `User` in the root package |
| Roles per route | `BindGuard(roles.Guard{Roles: …}, instance.READ_x)`: the layer value is its configuration | Nothing |
| 401, 403 and redirects from a guard | `panic(exception.UnauthorizedException(…))` works today; a redirect answer is a behavior decision of the auth roadmap | Nothing |
| Sessions, tokens | `modules/<name>` with `Register(opts *XxxModuleOptions)` | The import list already allows `modules/*` |

After this design a new feature is a root alias, a method, an `Enable…` or a satellite package. None needs
a name here to move again.

## Test plan

| Rule | Test | Case |
|---|---|---|
| B1 | `TestAPICheck_UnknownPublicPackageFails` (`internal/apicheck`) | A fixture module with an extra public package; allowed prefixes pass |
| B2 | `TestAPICheck_UpToDatePasses`, `TestAPICheck_AddedFuncFails`, `TestAPICheck_RemovedFuncFails`, `TestAPICheck_ParamRenamePasses` | Fixture module; exact `+` / `-` lines |
| B3 | `TestAPICheck_ReachableMethodFails`, `TestAPICheck_InternalFuncPasses`, `TestAPICheck_ReachIsTransitive` | Alias to an internal type; a type reached through a method result |
| B2 | `TestAPIFile_MatchesCode` (`internal/apicheck`) | The real module against `api/owlky.txt` |
| B4 | `TestSurface_SampleUsesPublicImportsOnly` (root package) | Parses the imports of `sample/**` and `bench/*.go` |
| B5 | `TestSurface_HTTPContextHasNoHooks`, `TestSurface_WSContextHasNoHooks`, `TestSurface_AggregationMembers` | Reflection on the alias types |
| B6 | `TestSurface_EmbeddableMembers` | Exact method names and zero exported fields, six types |
| B7 | `TestSurface_HTTPControllerRoutes`, `TestSurface_WSControllerEvents` | An app built with root names only, over `httptest` |
| B8 | `TestSurface_BindLayersThroughAliases` | Guard, interceptor, filter, middleware on one of two routes |
| B9 | `TestSurface_ModuleFunc`, `TestSurface_ModuleValue`, `TestSurface_InvalidModulePanics`, `TestSurface_ModuleNameResolved` | `func() owlky.Module`; a `Register(…)` result; a string; `Name` |
| B10, B21 | `TestSurface_LoggerFieldInjected`, `TestSurface_PublisherFieldInjected`, `TestContainer_InterfaceKeysAreReflected` | Controller, provider and layer fields; key equality |
| B11 | `TestSurface_HandlerParameters` | One handler per root alias type |
| B12 | `TestHTTPGuard_WrongShapeMessage` and the five siblings (`internal/common`) | Exact text |
| B13 | `TestListen_ErrorPrefix…` (one per message), `TestBindValue_PanicsWithErrBindTarget` (text), `TestCacheErrors_Prefix` | Exact text; `errors.Is` still true |
| B14 | `TestEnableVersioning_NilIsZeroOptions`, `TestEnableVersioning_CopiesOptions`, existing `TestVersioning_*` | nil, zero, header |
| B15 | `TestSurface_ConstantValues`, `TestDevtool_WithVersioningUnchanged` (existing) | Every listed value |
| B16 | `TestException_Accessors`, `TestHTTPContext_ID`, `TestWSContext_ID` | Each code family; an HTTP and a WS scope |
| B17 | `TestHTTPContext_StatusSetsWrittenCode`, existing `TestHTTPContext_Status` | 201 then `JSON`; default 200 |
| B18 | Existing tests of the replaced members, re-pointed to the functions: `TestReset_ClearsAllFields`, `TestReset_EveryFieldIsZeroAfterUse`, `TestHTTPContext_Init_GetIDEmptyBeforeInitAndAfterReset` (`ctx`), the param, aggregation and `InjectProviders…` tests of `ctx`, `aggregation`, `common` | Zero context, used context, slices over the kept capacity |
| B19 | `TestNewMemoryCache_NilUsesDefaults`, `TestNewMemoryCache_MaxEntries` | nil; one entry |
| B20 | Existing `TestTransport_*` moved to `internal/trace`; `TestTraceTransport_DelegatesToTrace` (root) | nil base, scope, existing header |
| B22 | `TestDevtool_SnapshotUnchangedByMove` | Golden snapshot taken before step 4 |
| Duplicates 1 to 7 | The tests named in "Duplicate ways" | |

## Open questions

Decided 2026-11-18 by the maintainer: every recommendation below is taken (1 convert all 42 hooks, with the per-member fallback; 2 `<Type><Value>`; 3 `exception` options deferred to the SSR error work; 4 keep `MiddlewareFn`; 5 `log.Logger` only; 6 `owlky:` / `cache:` prefixes; 7 keep `Bind` and `BindValue`).


1. **How far `ctx`, `common` and `aggregation` hooks go (D3).** Recommended: convert all 42 members now, with
   the Performance fallback per member. Alternative: leave them exported and listed as `reach` lines (no
   hot-path edit, but `c.Reset()` stays callable by applications). A single package for the whole engine
   would hide them without functions; it is a restructure of about 8,800 lines and its own roadmap.
2. **Enum constants as `<Type><Value>`.** It renames 17 constants users write (`log.DebugLevel` →
   `log.LevelDebug`, `versioning.HeaderVersion` → `owlky.VersioningHeader`…). Recommended: yes, it is the
   standard library's order, and today's enums use three orders (`DebugLevel`, `KindExact`, `FixedWindow`). Alternative: keep
   `<Value><Type>` (`DebugLevel`) everywhere and rename only `throttler`, `proxy` and `MediaType`.
3. **`exception` options.** `NewException(message, code, opts ...any)` takes a `string`, an `error`, an
   `ExceptionOptions` or a pointer, and ignores any other type silently. Recommended: leave it in this
   roadmap and decide it with the SSR error work. A typed form changes the `error` field clients receive
   today for invalid bodies (the `error` shortcut prints the cause alone), which D-028 rule 4 has to settle
   anyway. Alternative: `opts ...*ExceptionOptions` now, accepting that change.
4. **`MiddlewareFn` is an interface with a function-type name.** Recommended: keep it. `Middleware` is taken
   by the embeddable the container finds by that name, and the name is in every middleware README. Alternative:
   `MiddlewareHandler`.
5. **`log.Logger` instead of a root `owlky.Logger`.** Recommended: `log.Logger` (D5: one name; `log` is
   already imported to configure logging). Cost: a controller that only logs imports `log` next to the root
   package.
6. **Error prefixes (B13).** Recommended: `owlky:` and `cache:`, the packages users import, per D-028
   rule 2. Alternative: keep `core:`, `ctx:` and `memorycache:`; then messages name packages users cannot
   find.
7. **`Bind` and `BindValue`.** Recommended: keep both (one allocation per request). Alternative: remove
   `BindValue` for one obvious way; `Test…_BindValueEqualsBind` already proves `Bind` covers it.
