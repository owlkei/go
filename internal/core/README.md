# Core

Internal package: applications use `owlky.New(opts)`, `owlky.OptionsFromEnv()`, `owlky.ModuleBuilder()` and the root package's `App`, `AppOptions` (with its groups), `Module`, `Controller` and `Provider` instead.

*Package `core` is the dependency injection container and the server of `Owlky Go`: it builds the module tree, injects providers into controllers, plans every handler's arguments once at `Create()` and serves HTTP and WebSocket requests through the middleware, guard, interceptor, handler and exception filter pipeline.*

- [Core](#core)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`App`](#app)
    - [New](#new)
    - [Create](#create)
    - [ServeHTTP](#servehttp)
    - [Get](#get)
    - [Logger](#logger)
    - [Configuration methods](#configuration-methods)
    - [Pre-route stages](#pre-route-stages)
    - [SecurityCheck](#securitycheck)
  - [Modules and lifecycle](#modules-and-lifecycle)
    - [ModuleBuilder](#modulebuilder)
    - [PathStyle](#pathstyle)
    - [Module factories](#module-factories)
    - [AsyncImports and module states](#asyncimports-and-module-states)
    - [Retry](#retry)
    - [`Module` fields](#module-fields)
    - [Prefix and ID](#prefix-and-id)
    - [Provider and Controller](#provider-and-controller)
    - [Providers](#providers)
    - [Hooks](#hooks)
    - [Two apps in one process](#two-apps-in-one-process)
    - [Misuse](#misuse)
  - [Server](#server)
    - [Listen and Serve](#listen-and-serve)
    - [ServerOptions](#serveroptions)
      - [ShutdownTimeout](#shutdowntimeout)
      - [Port](#port)
      - [TLS](#tls)
      - [IsH2C](#ish2c)
    - [Startup errors and logs](#startup-errors-and-logs)
    - [Protocols](#protocols)
    - [Graceful shutdown and Stop](#graceful-shutdown-and-stop)
    - [Timeouts and limits](#timeouts-and-limits)
    - [Deploying](#deploying)
    - [Advanced: your own server](#advanced-your-own-server)
  - [Request lifecycle](#request-lifecycle)
    - [Lifecycle rules](#lifecycle-rules)
  - [Errors in middleware](#errors-in-middleware)
  - [Error response](#error-response)
    - [Error response rules](#error-response-rules)
    - [Panic stacks](#panic-stacks)
  - [No route, HEAD and versioning](#no-route-head-and-versioning)
  - [Static files](#static-files)
  - [Pages](#pages)
  - [Response writer](#response-writer)
  - [Handler injection](#handler-injection)
    - [Parameters](#parameters)
    - [Pipes](#pipes)
    - [Results](#results)
    - [Trace events](#trace-events)
    - [Rules](#rules)
  - [Other exported types](#other-exported-types)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

## Key Features
- Handler arguments are planned once at `Create()`: a request runs a precomputed list of steps and one `reflect.Value.Call`.
- A handler whose arguments can't be resolved stops `Create()` instead of failing on every request.
- Pipes (`Transform` types) get providers injected into their exported fields and a fresh receiver on every request.
- Results are written by their kind: text for strings and numbers, JSON for maps, slices, structs, arrays and pointers.
- One written request lifecycle, followed arrow by arrow: middlewares, guards, interceptors, handler, post-interceptors, with errors routed by where they happened.
- Only exceptions reach the client with their code and message: any other panic answers a fixed 500 and goes to the logger.
- A missing route answers 404 or 405 (with `Allow`) from precomputed bytes that never echo the request; `HEAD` is served by the `GET` route.

## Usage

```go
package main

import (
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/core"
	"go.owlky.com/owlky/internal/ctx"
)

type GreetingService struct{}

func (instance GreetingService) NewProvider() core.Provider {
	return instance
}

func (instance GreetingService) Greet(name string) string {
	return "hello " + name
}

type PageDTO struct {
	Page int `bind:"page"`
}

func (instance PageDTO) Transform(query ctx.Query, _ common.ArgumentMetadata) any {
	bound, _ := query.Bind(instance)
	return bound
}

type UserController struct {
	common.HTTP
	GreetingService GreetingService
}

func (instance UserController) NewController() core.Controller {
	return instance
}

func (instance UserController) READ_users_BY_id(param ctx.Param, page PageDTO) ctx.Map {
	return ctx.Map{
		"greeting": instance.GreetingService.Greet(param.Get("id")),
		"page":     page.Page,
	}
}

func main() {
	app := core.New(nil)
	app.Create(
		core.NewModuleBuilder().
			Controllers(UserController{}).
			Providers(GreetingService{}).
			Build(),
	)
	if err := app.Listen(); err != nil {
		panic(err)
	}
}
```

`GET /users/7?page=2` answers `{"greeting":"hello 7","page":2}`.

## `App`

`App` holds everything it wires: its modules, providers, layers, routes, logger and publisher, the HTTP router, the optional WebSocket server and the request tracer. Configure it with the `AppOptions` given to `New`, bind app-wide layers with the `BindGlobal…` methods, wire it once with `Create`, then serve with `Listen` or `Serve` (see [Server](#server)).

### New

```go
func New(opts *AppOptions) *App
func OptionsFromEnv() *AppOptions
```

`New` returns an `App` configured by `opts`; `nil` and `&AppOptions{}` give the same app: the built-in logger, a 30 s startup timeout, a 10 MiB request body limit, port 3000, a 30 s shutdown timeout, no TLS, no access log, no devtool, no pages, no WebSocket, no versioning and no pre-route stage. It also holds the built-in exception filters and a `common.Publisher` backed by the app's own in-memory broker.

`AppOptions` is the whole configuration (design 0022; field by field in the root `README.md` › Configuration): `Logger`, `StartupTimeout`, `IsAccessLog`, `IsDevtool`, `Server`, `HTTP`, `Render`, `WS`, `Versioning`, and the pre-route stages `Proxy`, `Hosts`, `Origins`, `LoadShed`, `HTTPSRedirect`, `Headers`, `CORS`, `CSRF`, `Compress`. A pointer group left `nil` is the same as an empty one. A group with a required field is on when that field is set (`Render.FS`, `WS.Path`, `Versioning.Type`, `Proxy.TrustedProxies` / `TrustedHops`, `Hosts`); a group whose fields are all optional has `IsEnabled`.

`OptionsFromEnv` returns the same struct, every pointer group non-nil, filled from `OK_<PATH>` variables (levels joined by `__`, names in upper snake case: `OK_HTTP__MAX_BODY_BYTES`), `PORT` (when `OK_SERVER__PORT` is not set) and `./.env` (the environment wins). Code written after the call wins. It never panics: `New` panics once with every recorded error.

#### Rules
- `New` reads `opts` once and copies what it keeps (slices, a cloned `*tls.Config`): changing `opts` after `New` has no effect.
- Every check happens in `New` and panics `invalid app: <detail>`: `StartupTimeout is -1s, it must be positive`, `Server.Port is 65536, it must be between 1 and 65535`, `Server.TLS needs both CertFile and KeyFile`, `Server.TLS.Config has no certificate`, `Server.IsH2C applies to connections without TLS`, `HTTP.ServeDirs has an empty entry`, `Render.Dir is set without Render.FS`, `Versioning.Key is set without Versioning.Type`, `<Group>.<Field> is set while <Group>.IsEnabled is false`, `<Group>.IsEnabled and <Group>.IsDisabled are both set`, `Origins is set, but CORS, CSRF and WS are off`, `CORS needs Origins`, and each stage's own checks (`Proxy.TrustedHops is 20, it must be between 1 and 16`, `Hosts has 300 entries, at most 256 are allowed`, `Compress.MinSize is -1, it must be 0 or more`).
- `PORT` is read only by `OptionsFromEnv`, never by `New` or `Listen`.
- With options from `OptionsFromEnv`, an unknown `OK_*` name (with the closest known name), an empty value, a value or list element that does not convert, a code-only field (`OK_LOGGER`, `OK_RENDER__FS`, `OK_SERVER__TLS__CONFIG`, `OK_VERSIONING__EXTRACTOR`) and a `.env` syntax error make `New` panic `invalid app: <n> environment errors:` with one error per line; a value never appears in a `.env` syntax error.
- With options from `OptionsFromEnv`, `New` logs one `Info "AppOption"` line per non-zero field: `option`, `value` and `source` (the variable, `.env:<line>`, `PORT` or `code`).
- `ByteSize` prints the largest of `GiB`, `GB`, `MiB`, `MB`, `KiB`, `KB` that divides the value exactly, else plain digits (`10MiB`, `1500`); a variable takes plain bytes or a whole number with one of those units (`10b` and `1.5MB` are refused).

#### Returns
- 1st value: `*App`

### Create

Builds the module tree from the root module, initializes the modules (see [Modules and lifecycle](#modules-and-lifecycle)), creates the controllers, plans every handler, binds middlewares, guards, interceptors and exception filters, then runs the `OnReady` hooks. It panics (`"dependency injection: ..."`, `"route conflict: ..."`, `"event conflict: ..."`, `"invalid app: ..."`, …) when the application can't be built. Call it once per app. `Create` never writes to the modules it is given.

#### Parameters
- 1st parameter: `*Module`

- Description: The root module, built with `ModuleBuilder()`.

#### Usage

```go
app.Create(core.NewModuleBuilder().Controllers(UserController{}).Build())
```

### ServeHTTP

`ServeHTTP(w http.ResponseWriter, r *http.Request)` serves one request: it caps a request body at the app's limit (`HTTP.MaxBodyBytes`: 10 MiB by default, negative for no limit, whoever serves the app; a request without a body, `nil` or `http.NoBody`, is not wrapped), takes a pooled context, builds the request's trace scope, sets the `X-Request-Id` and `Traceparent` response headers before any middleware runs, runs the [request lifecycle](#request-lifecycle) and, with a trace listener, emits the `complete` event, whose `Code` is the status actually written (`200` when nothing was written). Requests to the WebSocket path are upgraded when `WS.Path` is set.

#### Request and trace IDs
- Every response (a route, a 404, a 405, an error) carries `X-Request-Id: <request ID>` (16 hex characters, the same as `c.ID()`) and `Traceparent: 00-<trace ID>-<request ID>-<flags>`.
- The request ID is always the app's own: neither an incoming `X-Request-Id` nor a `traceparent` sets it.
- One valid incoming `traceparent` continues its trace ID and keeps its flags (`& 03`), so a client chooses the trace ID of its request (see [trace › Security](../trace/README.md#security)); an absent, repeated or invalid one (upper case, zero IDs, version `ff`, CR/LF, a megabyte of junk) is ignored silently and a new, time-ordered trace starts with flags `03`.
- The scope is in `c.Context()`, `c.Request.Context()` and the `*http.Request` every middleware receives, so guards, interceptors, filters, the handler and a service called with `c.Context()` all read the same IDs. A goroutine that keeps `c.Context()` reads them after the request ended and the pooled context was reused; concurrent requests never read each other's scope.
- An outbound `net/http` request built with `c.Context()` and sent through `trace.Transport` forwards the trace (`Traceparent`); see [trace](../trace/README.md#outbound-calls).
- A WebSocket handshake follows the same rules: the 101 response and a refusal by a pre-route stage or a middleware (403, 421, 503) carry both headers, and the connection ID (the `connected` payload's `id`, the `WSConnmgr` key) is the handshake's request ID. Each message dispatched to a topic is its own message with its own ID (a frame naming two topics is two messages); its `c.Context()` carries the connection's trace, the connection ID and the message ID, and is done when the connection closes.
- A refused upgrade (bad version, missing key) still releases the pooled context.
- The scope adds one allocation to a request with no trace listener (7 for a static route, 4 for a 404, 5 for a 405), and continuing a trace costs no more.

### Get

`Get(p Provider) any` returns the instance this app created for `p`'s type, or `nil` for a type the app does not have, before `Create`, and while the provider's module is not ready yet (see [AsyncImports and module states](#asyncimports-and-module-states)). It is safe to call from several goroutines after `Create`.

```go
service := app.Get(GreetingService{}).(GreetingService)
```

### Logger

```go
func (app *App) Logger() log.Logger
```

`AppOptions.Logger` is the one way to give the app a logger. It takes any `log.LogAdapter` (the five methods `Debug`, `Info`, `Warn`, `Error`, `Fatal`: a zap `SugaredLogger` fits). Everything logged through `app.Logger()`, the server and access logs, and every injected `log.Logger` field reaches it. A custom logger gets the `log` struct tags and masking machinery and `From` (`log.WrapLogger(logger, nil)`); a logger that already came from `log.NewLog` or `log.WrapLogger` is used as it is. A custom logger is called directly on the caller: the bounds of the log's output pipeline do not apply to it.

`Logger()` never returns `nil`: without `AppOptions.Logger` it is the built-in logger with default options, the same value on every call. `app.Logger()` and every injected `log.Logger` have `From(ctx)`, whatever logger was given: inside a request, `logger.From(c.Context()).Info(…)` adds the trace ID and the request ID to the line. Only fields of type `log.Logger` are injected; a `log.LogAdapter` field is not.

```go
app := core.New(&core.AppOptions{Logger: log.NewLog(&log.LogOptions{Format: log.FormatJSON})})
app = core.New(&core.AppOptions{Logger: log.WrapLogger(myLogger, []string{"password"})})
app.Logger().Info("Started")
```

#### Rules
- A logger wrapped with `log.WrapLogger(l, fields)` keeps its masking rules.
- A type with the five methods and no `From` is accepted, and `From` on `app.Logger()` passes the scope's fields to it first, then the call's arguments.
- The built-in logger's lines are synchronous during `Create` (its `OnReady` hooks included): a bootstrap panic loses no line. As the last step of `Create` the stdout pipeline becomes asynchronous; it becomes synchronous again when the last created app has shut down (see `log/README.md`, Output pipeline).

### Configuration methods

Configuration is in `AppOptions` (see [New](#new)); these methods bind code. Each returns the `App` so calls can be chained. Call them before `Create`: after it they panic `invalid app: <Method> must be called before Create`.

| Method | Effect |
|---|---|
| `BindGlobalMiddlewares(...common.MiddlewareFn)` | Middlewares that run for every HTTP request, after the pre-route stages, and once per WebSocket connection at the handshake |
| `BindGlobalGuards(...any)` | Guards for every HTTP route and WS event |
| `BindGlobalInterceptors(...any)` | Interceptors for every HTTP route and WS event |
| `BindGlobalExceptionFilters(...any)` | Exception filters for every HTTP route and WS event |

### Pre-route stages

`New` compiles the stages that `AppOptions` turns on, once, in this fixed order: `loadshed` → `proxy` → `hostcheck` → `httpsredirect` → `headers` → `cors` → `csrf` → `compress`. They run in `ServeHTTP` before route matching, the async gate and every user middleware.

- A stage that is off is not in the list: an app that configures nothing runs no stage.
- The order of `AppOptions` fields or of `BindGlobalMiddlewares` calls never changes the order.
- A request refused by a stage gets the stage's answer, never `404`, `405`, `410` or the gate's `503` (`Hosts: {"a.com"}`, `Host: b.com`, unknown path → `421`).
- `cors` and `csrf` read one origin set compiled from `Origins`; a wildcard entry (`https://*.a.com`) also passes `csrf`.
- On the WebSocket handshake the stages that apply run in the order `loadshed` → `proxy` → `hostcheck` → `origin`, then the `BindGlobalMiddlewares` in bind order; `httpsredirect`, `headers`, `cors`, `csrf` and `compress` don't run there.
- `origin` runs on every handshake when `WS.Path` is set: no `Origin` passes, the same origin passes (scheme and host as `ClientScheme` and `ClientHost` give them, default ports dropped), an `Origin` in `Origins` passes, anything else (`null`, longer than 256 bytes) gets `403 Origin not allowed`.
- With a trace listener each stage that runs emits one `pre-route` event (`Name` the stage, `Code` the status it answered with when it ended the request, else `0`); a request ended by a stage emits no later stage, middleware, guard or handler event.
- With `IsDevtool`, the snapshot lists the stages in a `pre_route` array, in order, each with its `Snapshot()`.

### SecurityCheck

At the end of `Create` the app logs at most one line, `Warn "SecurityCheck" "missing" <list> "docs" <url>`, with `<list>` the comma-joined names of:

- `csrf`, when the app has a `SUBMIT_` route and neither `CSRF.IsEnabled` nor `CSRF.IsDisabled` is true;
- `headers`, when it has a `RENDER_` route and neither `Headers.IsEnabled` nor `Headers.IsDisabled` is true.

Routes of `AsyncImports` modules count. An API-only app, or one with every protection on or `IsDisabled`, logs nothing. The check changes no response.

## Modules and lifecycle

A module groups controllers and providers and imports other modules. An entry of `Imports` or `AsyncImports` is a `*Module` or a module factory (see [Module factories](#module-factories)). `IsGlobal` is a property of a module, not a kind; `config.Register` and `cache.Register` return a `*Module` and are not factories. The user-facing guide, with the timeline of `Create`, the background and shutdown, is the main README › Module lifecycle.

### ModuleBuilder

```go
type ModuleBuilder struct

func NewModuleBuilder() *ModuleBuilder
func (m *ModuleBuilder) Imports(modules ...any) *ModuleBuilder
func (m *ModuleBuilder) AsyncImports(modules ...any) *ModuleBuilder
func (m *ModuleBuilder) Providers(providers ...Provider) *ModuleBuilder
func (m *ModuleBuilder) Controllers(controllers ...Controller) *ModuleBuilder
func (m *ModuleBuilder) PathStyle(style common.PathStyle) *ModuleBuilder
func (m *ModuleBuilder) Build() *Module
```

`NewModuleBuilder()` starts a module (the root package's `owlky.ModuleBuilder()` calls it). Each call returns a new builder. `Imports`, `AsyncImports`, `Providers` and `Controllers` append to the module's lists and return the builder, so calls can be chained and repeated. `PathStyle` sets how the resource words of the module's route names are joined (see [PathStyle](#pathstyle)). `Build()` returns the `*Module`.

#### Parameters
- `Imports`, `AsyncImports`: `...any`, each a `*Module` or a module factory of one of the three shapes in [Module factories](#module-factories).
- `Providers`: `...Provider`, provider values (see [Provider and Controller](#provider-and-controller)).
- `Controllers`: `...Controller`, controller values.
- `PathStyle`: `common.PathStyleKebab` or `common.PathStyleAsWritten` (`owlky.PathStyleKebab`, `owlky.PathStyleAsWritten`).

#### Returns
- `NewModuleBuilder`: a new `*ModuleBuilder`. `Imports`, `AsyncImports`, `Providers`, `Controllers`, `PathStyle`: the same builder.
- `Build`: `*Module`, a new module with its own `ID()` and a resolved `Name`.

#### Usage

```go
var UserModule = core.NewModuleBuilder().
	Controllers(UserController{}).
	Providers(GreetingService{}).
	Build()

var AppModule = core.NewModuleBuilder().
	Imports(UserModule).
	Build()
```

#### Rules
- An import that is neither a `*Module` nor a module factory panics `invalid module: can't import '<type>' with Imports: an import is a owlky.Module or a module factory: func(deps...) owlky.Module, func(deps...) (owlky.Module, error) or func(ctx context.Context, deps...) (owlky.Module, error)` at `Build()` (one line per bad entry; `AsyncImports` entries name `AsyncImports`). The check is by `reflect.Type`: a nil entry, a variadic func, `context.Context` anywhere but first, a `ctx` func without the error result and three results are refused.
- The same entry (the same `*Module`, or the same function) in both lists of one builder panics `invalid module: '<label>' is in both Imports and AsyncImports` at `Build()`; listed with `Imports` by one module and with `AsyncImports` by another, it panics `invalid module: '<label>' is imported with Imports by '<x>' and with AsyncImports by '<y>'` at `Create()`.
- `Name` is `<package>.<variable>` of the variable the module is assigned to (a `var X = func() *core.Module {…}` or a `var X = factory()` included), else `<package>.<function>` of the function that calls `Build`. A module built as an argument of `Imports` gets its own factory's name, never the outer variable's.
- Chained calls accumulate: `Providers(p1).Providers(p2)` lists both.
- `PathStyle` with a value other than the two constants (`PathStyle(0)`, `PathStyle(9)`) panics `invalid module: PathStyle needs owlky.PathStyleKebab or owlky.PathStyleAsWritten` at the call. Called twice, the last call wins.

### PathStyle

The style of a controller is the explicit style of the first module that set one, in this order: the module that lists the controller, then each module that imports it (directly or through other modules) in the order `Create()` discovers them (depth first, `Imports` order: the order in which module prefixes are stacked). No module set one: `PathStyleKebab`.

- A root with `PathStyle(PathStyleAsWritten)` and modules without a style: `READ_payment_intents` is `/payment_intents/` in the root's own controllers and in every imported module, static or a module factory.
- An imported module with `PathStyle(PathStyleKebab)` under an as-written root keeps `/payment-intents/`, and so do the modules it imports that set none (the nearest explicit setting wins).
- Two parents `A` (as written) and `B` (kebab) both import `M` (no style): `Imports(A, B)` gives `M` as written, `Imports(B, A)` kebab. `M`'s controller is still bound once.
- The style applies to resource words only: never to prefixes, `Path`, parameters, versions or WebSocket events.
- Two apps that build their modules separately do not share a style.

```go
var BillingModule = core.NewModuleBuilder().
	PathStyle(common.PathStyleAsWritten).
	Controllers(BillingController{}).
	Build()
```

### Module factories

```go
func(deps...) *core.Module
func(deps...) (*core.Module, error)
func(ctx context.Context, deps...) (*core.Module, error)
```

A factory is called once per app (same function value, same module; two closures of one function literal are one factory). The label in messages is the returned module's `Name`, else the function name cut to its last import path element (`sql.Register`).

**Scope and order.** A factory listed by module `P` resolves each non-`ctx` parameter from a provider visible in another entry of `P`'s `Imports` / `AsyncImports` (declared by it or visible through its imports), then a global provider (the root module's and every `IsGlobal` module's, at any depth), then `log.Logger` / `common.Publisher`. `P`'s own providers are not in scope. `Create` scans the static tree, then repeats: call, in discovery order (depth first, list order), the first factory not yet called whose parameters all resolve, and scan the module it returns. The provider of an argument is built after the `OnInit` of the module that declares it, so that module's `OnInit` runs before the factory (and not again later).

**Startup timeout.** `AppOptions.StartupTimeout` (default 30 s) is one budget for the whole `Create`, counted from its start. Every factory of the sync set runs on a helper goroutine; its parameters are resolved on `Create`'s goroutine first. A `ctx` factory gets a context whose deadline is that instant, canceled when `Create` returns or panics. `OnInit` and `NewProvider` are not covered.

#### Rules
- A negative `StartupTimeout` panics at `New`: `invalid app: StartupTimeout is -1s, it must be positive`; `0` means the default.
- A factory error panics `invalid module: module factory '<label>' failed: <err>`.
- A factory panic is recovered, logged `Error` `ModuleFactoryPanic` (`module`, `panic`, `stack`), and panics `invalid module: module factory '<label>' panicked: <value>`.
- The deadline first panics `invalid module: module factory '<label>' did not return within the startup timeout (<d>)`; the helper goroutine is abandoned.
- A nil module with a nil error panics `invalid module: module factory '<label>' returned a nil module`.
- One factory left with an unresolved parameter panics `dependency injection: can't resolve argument of '<func type>'. Please make sure that the argument '<key>' at index [<i>] is available in the injected providers`.
- Two or more left panic `dependency injection: module factories can't be ordered: '<label>' needs '<T>' → '<label>' needs '<T>' → …. Nobody provides these types, or the factories need each other`, in discovery order. A cycle and a missing provider can't be told apart before the factories return.
- A factory in `Imports` whose missing type is owned by a module that is not ready at `Create` panics `invalid module: module factory '<label>' is in Imports and needs '<T>' of '<module>', which is not ready at Create: import it with AsyncImports`.

### AsyncImports and module states

The **sync set** is the root plus every module reachable through `Imports` only. Each `AsyncImports` entry found in it is built in the background with its **unit**: its module plus every module reachable from it (both lists) that is not in the sync set nor in an earlier entry's unit. A unit has one state; inside a unit, `AsyncImports` behaves as `Imports`.

**States.** Every module is `pending`, `ready` or `failed`, and changes once. Module `M` **needs** entry `E` when a provider owned by `E`'s unit, or by a module that needs `E`, is the type of a field of one of `M`'s providers or controllers, of a layer bound in its controllers, or of a pipe in their handlers. Importing alone is not a need. `M` is `ready` when every entry it needs is, `failed` as soon as one is. With an async factory in the app, a provider type nobody provides yet leaves its module `pending`; it becomes `failed` with cause `can't resolve <T>` once no async factory can still run. Without one, `Create` panics as before.

**Create.** Modules that are not ready get no `OnInit`, providers, controllers or `OnReady` there. For each of their controllers, `Create` builds a throwaway instance (ready providers injected, the others zero), calls `NewController` and reads its routes, events, prefixes and layers, so every route check runs at `Create`. The routes and events are registered behind the module's closed gate; the router, the event matcher and the `HTTP` / `WS` maps are never written after `Create`. Then one goroutine per entry starts, before `OnReady` of the ready modules.

**Background.** An entry's goroutine waits for the entries its factory needs, calls the factory with a `ctx` canceled at shutdown and without a deadline, scans what it returns and calls the unit's factories, then, per module of the unit in discovery order: `OnInit`, `NewProvider`, controllers; the gate opens in one atomic store; state `ready`; `OnReady`. Each pending module whose needs are now all `ready` is built the same way on the same goroutine. Wiring is serialized by a mutex (factories run outside it): a slow `OnInit` or `NewProvider` delays other units. An open route is the route a sync module would have: same layers, same order.

**Gate.** A request to a closed route runs the global middlewares, then answers `503` with the body of `exception.ServiceUnavailableException("Service Unavailable")`: JSON, or HTML through `503.html`, `5xx.html` and the built-in page on a page route, whatever `Accept` says. No module middleware, guard, interceptor, filter or handler runs. `Retry-After` while `pending` is the seconds to the entry's next `Retry` attempt (1 to 60, prebuilt strings), `5` when none is scheduled; none when `failed`. 404, 405 and `Allow` count gated routes. A WS `subscribe` or `publish` on a gated event replies one `error` with `ServiceUnavailableException("not ready: " + topic)` (`code` 503); the handshake is not gated.

**Shutdown.** The async `ctx` is canceled before the drain. After it the app waits for the entry goroutines until `ShutdownTimeout` (30 s when never served) has passed since shutdown began, logs `Warn` `ModuleShutdownTimeout` (`module`) for each one still running and abandons it. From the cancel on no state changes and no gate opens; a factory result that arrives later is dropped. `OnShutdown` runs only for `ready` modules, in the reverse of the order they became ready (`Create` order first).

**Visibility.** `ModulePending` (Info, `module`, `waiting_for`) at `Create`, `ModuleReady` (Info, `module`, `after`), `ModuleFailed` (Error, `module`, `after`, `cause`, `stack` for a panic; a dependent's cause is `needs '<label>'`). `InstanceLoader` lines at listen list only modules ready then. The devtool snapshot's `modules` lists a `ModuleStatus` per module, read live (see [devtool](../devtool/README.md#modules)).

#### Rules
- `Create` never waits for an entry and never panics because of one.
- A failure of an entry (a factory error, a panic in its factory, `OnInit`, `NewProvider` or `NewController`, a nil module, a type that never resolves) fails it and every module that needs it; their gates never open; the app keeps running. A failure while building a dependent fails only it and its own dependents.
- A module that an async factory returns with controllers fails its entry: `module '<label>' declares controllers (<Type>, …) but comes from an async factory: put them in a static module that imports it`. A static `*Module` in `AsyncImports` may have controllers (probed and gated).
- In an app with `AsyncImports`, controllers of ready modules that bind a layer are probed too, so their `NewController` also runs twice: `NewController` must not use injected providers or have side effects.
- A panic in a probed `NewController` panics `invalid module: NewController of '<Type>' failed before its module '<module>' is ready: <value>. NewController must not use injected providers`. Real routes that differ from the probe fail the module: `routes of '<Type>' changed between Create and construction`.
- An app-wide layer (`BindGlobal…`) that injects a provider of a module not ready at `Create` panics `dependency injection: app-wide <kind> '<Type>' injects '<T>' of '<module>', which is not ready at Create: app-wide layers can't depend on AsyncImports`.
- Two entries whose known providers need each other panic `invalid module: async modules need each other: '<a>' → '<b>' → '<a>'`.
- A panic in a background `OnReady` is logged `OnReadyPanic` (`module`, `panic`, `stack`); the module stays `ready`. A route may be served before its module's `OnReady` returns.
- A module that fails after its factory returned gets no `OnShutdown`: a failing factory cleans up before returning.
- `Stop()` called from a background `OnReady` or factory does not deadlock.

### Retry

```go
type RetryOptions struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
}

func Retry(ctx context.Context, opts *RetryOptions, fn func(ctx context.Context) error) error
```

`Retry` (the root package's `owlky.Retry`) calls `fn(ctx)` until it returns `nil`. The wait before attempt `n+1` is `min(MaxDelay, InitialDelay × 2^(n-1))`, then a uniform random time in `[d/2, d]`. Defaults: `InitialDelay` 500 ms, `MaxDelay` 30 s (zero or less means the default), `MaxAttempts` 0 (until `ctx` is done). It starts no goroutine (one `time.Timer`, stopped on return).

#### Rules
- A nil error returns `nil` at once; attempts count from 1.
- `ctx` done before or during a wait, or after a failed attempt, returns `owlky: retry stopped after <n> attempts: …` wrapping `errors.Join(ctx.Err(), lastErr)`; `fn` is not called when `ctx` is already done (`n` = 0).
- `MaxAttempts > 0` reached returns `owlky: retry gave up after <n> attempts: <lastErr>` without waiting.
- `nil` options are the defaults; a nil `fn` panics `invalid retry: Retry needs a function`; a panic in `fn` is not recovered.
- With the `ctx` of an `AsyncImports` factory, each failed attempt adds to the entry's `attempts`, stores `last_error`, sets the next attempt time for `Retry-After` and logs `Warn` `ModuleRetry` (`module`, `attempt`, `error`, `next`). With any other `ctx` it only retries.

### `Module` fields

#### Name
Type: `string`

Default: resolved by `Build` (see the rules above)

Required: `false`

The name used in the `InstanceLoader` startup line. A module with an empty name logs no line.

#### IsGlobal
Type: `bool`

Default: `false`

Required: `false`

Makes the module's providers visible to every module of the app (see [Providers](#providers)).

#### OnInit
Type: `func()`

Default: `nil`

Required: `false`

Runs when the module is initialized, before its providers are constructed: in `Create`, or in the background for a module that is not ready then (see [Hooks](#hooks)).

#### OnReady
Type: `func()`

Default: `nil`

Required: `false`

Runs once at the end of `Create`, or right after the module becomes ready in the background (see [Hooks](#hooks)).

#### OnShutdown
Type: `func()`

Default: `nil`

Required: `false`

Runs once when the app shuts down or `Stop()` is called (see [Hooks](#hooks) and [Graceful shutdown and Stop](#graceful-shutdown-and-stop)).

```go
var CacheModule = func() *core.Module {
	m := core.NewModuleBuilder().Providers(GreetingService{}).Build()
	m.IsGlobal = true
	m.OnShutdown = func() { /* close connections */ }
	return m
}()
```

### Prefix and ID

```go
func (m *Module) Prefix(prefix string) *Module
func (m *Module) ID() string
```

`Prefix` adds a path prefix to every route of the module and of the modules it imports, and returns the module. A prefix added to an importing module goes in front: a child with `Prefix("v1")` imported by a root with `Prefix("api")` serves `/api/v1/ping`, and `/ping` and `/v1/ping` answer 404. `ID` returns the identifier `Build` gave the module, unique per built module.

```go
var V1Module = core.NewModuleBuilder().Controllers(UserController{}).Build().Prefix("v1")
```

### Provider and Controller

```go
type Provider interface {
	NewProvider() Provider
}

type Controller interface {
	NewController() Controller
}
```

A provider's `NewProvider` (once per app) and a controller's `NewController` are called after their exported fields were injected; what they return is what the app keeps. A controller embeds `common.HTTP` for HTTP routes or `common.WS` for WebSocket events, and binds its module layers in `NewController` (`BindMiddleware`, `BindGuard`, `BindInterceptor`, `BindExceptionFilter`).

### Providers

- Each provider type is constructed once per app, however many modules list it.
- A provider is constructed after the providers its exported fields name (in its module, in the modules it imports, or global), and what is injected is always the result of `NewProvider`.
- The providers of the root module and of every `IsGlobal` module (at any import depth) are visible to every module of the app.
- A dependency that can't be resolved panics `dependency injection: can't resolve dependency '<type>' of the provider…`; an unexported field panics `dependency injection: can't set value to unexported '<field>' field of the <type> provider`.
- Providers are injected by value: state that must be shared sits behind a pointer.

### Hooks

Modules are initialized depth first: a module after every module it imports, imports in the order of `Imports(…)`, each module once per app. Initializing a module calls its `OnInit`, then constructs its providers. With root importing A and C, both importing B, `OnInit` runs B, A, C, root.

- `OnReady` runs once per app at the end of `Create`, after routes, layers and the devtool snapshot exist, in the same order. `app.Get` works inside it.
- A panic in `OnInit` or `OnReady` stops `Create` and reaches the caller unchanged.
- `OnShutdown` runs once per app, in the exact reverse order (root, C, A, B), when the app shuts down or `Stop()` is called. An app whose `Create` did not finish runs no `OnShutdown`.
- A module that is not ready at `Create` runs `OnInit` and `OnReady` in the background, and `OnShutdown` only if it became ready, in the reverse of the ready order (see [AsyncImports and module states](#asyncimports-and-module-states)).

### Two apps in one process

Each `core.New(opts)` is independent: its routes, providers, layers (guards, interceptors, exception filters, middlewares), logger and publisher are its own. Two apps may declare the same route or WebSocket event, construct their own instance of every provider and layer, and can be created on two goroutines at once.

You can give the same module value (a root, or a package-level `var X = some.Register(…)`) to any number of apps. A module factory may be called by two apps at once: it must not write shared state without its own protection. A resource created outside the framework, such as a cache backend built in a package-level `Register` call, is shared because you created it once. Don't change a module (`Prefix`, `IsGlobal`, a hook) after giving it to `Create`.

Inside one app, a guard, interceptor, exception filter or middleware is constructed once per type and configuration: equal configurations share one instance, different ones get their own.

### Misuse

- `Create(nil)` panics `invalid module: Create needs a root module`.
- A second `Create` on one app panics `invalid app: Create called twice`.
- A configuration method after `Create` panics `invalid app: <Method> must be called before Create`.
- A route name the parser would misread panics `invalid route: …` (an upper-case word that is not a route word, `BY` without a name, a word after the parameters, `OF` with nothing on one side, `FILE` without a name and an extension, `ALL` not last in a WebSocket name): see [common › Route DSL](../common/README.md#route-dsl). So do the misuses of `Path` ([common › Path](../common/README.md#path)).
- Two routes of one app with the same method, version and shape (parameter names don't count) panic `route conflict: <fn> method is conflicted with <fn> method`; two handlers of one event panic `event conflict: <fn> event is conflicted with <fn> event`.

## Server

### Listen and Serve

```go
func (app *App) Listen() error
func (app *App) Serve(l net.Listener) error
```

Call one of them once, after `Create`. Each one blocks until the server has stopped. Their settings are `AppOptions.Server`.

- `Listen` listens on TCP `:<Server.Port>` (every interface; 3000 when `Port` is 0). It serves HTTPS, with HTTP/1.1 and HTTP/2, when `Server.TLS` holds a certificate, and HTTP/1.1 otherwise.
- `Serve` serves on a listener you made, with the same TLS and protocol rules: a loopback or Unix socket address, systemd socket activation, or `127.0.0.1:0` in a test. It ignores `Port`, and logs `Info "ServerPortIgnored" "port" <n>` once when `Port` is set.

```go
app := core.New(&core.AppOptions{Server: &core.ServerOptions{Port: 8080}})
app.Create(root)
if err := app.Listen(); err != nil {
	app.Logger().Fatal("AppError", "error", err)
}

app = core.New(&core.AppOptions{Server: &core.ServerOptions{
	Port: 443,
	TLS:  &core.TLSOptions{CertFile: "cert.pem", KeyFile: "key.pem"},
}})

app = core.New(&core.AppOptions{Server: &core.ServerOptions{
	Port: 443,
	TLS:  &core.TLSOptions{Config: manager.TLSConfig()},
}})

l, _ := net.Listen("tcp", "127.0.0.1:8080")
err := app.Serve(l)
```

A server that only listens on loopback uses `Serve` with a listener on `127.0.0.1`: `Listen` always binds every interface. `Server` has no effect when the app is only used as an `http.Handler`.

### ServerOptions

`AppOptions.Server`; `nil` gives the defaults. A zero field takes its default too. The values are read once, at `New`.

#### Port
Type: `int`

Default: `0`, which means 3000 (`OptionsFromEnv` fills it from `OK_SERVER__PORT`, else `PORT`)

Required: `false`

The TCP port `Listen` opens. A value outside 0-65535 panics at `New`.

#### ShutdownTimeout
Type: `time.Duration`

Default: `0`, which means 30 s

Required: `false`

How long running requests and `OnShutdown` hooks may take during a shutdown, after a signal or `Stop()`. A negative value panics at `New`.

#### TLS
Type: `*TLSOptions`

Default: `nil`, no TLS

Required: `false`

`CertFile` and `KeyFile` name a PEM certificate chain and its key, loaded at `New` (a missing or bad file panics there: `invalid app: Server.TLS: load certificate: <err>`). `Config` is a base `*tls.Config` (code only); it is cloned, never modified, and the pair of files is added to it. Without files, `Config` must supply certificates (`Certificates`, `GetCertificate` or `GetConfigForClient`). Without `Config`, the minimum version is TLS 1.2.

#### IsH2C
Type: `bool`

Default: `false`

Required: `false`

Without TLS: also accept unencrypted HTTP/2. With TLS it panics at `New`.

```go
app := core.New(&core.AppOptions{
	Server: &core.ServerOptions{Port: 8080, ShutdownTimeout: 10 * time.Second, IsH2C: true},
	HTTP:   &core.HTTPOptions{MaxBodyBytes: 1 * core.MiB},
})
```

The request body cap is `HTTP.MaxBodyBytes` (a `ByteSize`; `0` means 10 MiB, negative turns the cap off). It applies in `ServeHTTP`, so also in your own `http.Server`. A body over the cap answers 413, whether its length is declared or chunked.

### Startup errors and logs

The serving calls return an error, and never fall back to plain HTTP, when:

| Case | Error |
|---|---|
| Called before `Create` | `owlky: Create must be called before serving` |
| The app is already serving | `owlky: app is already serving` |
| The app has stopped | `owlky: app is stopped` |
| The port can't be bound (in use, out of range) | `owlky: listen on :<port>: <err>`, wrapping `net.Listen`'s error |

In these cases nothing is logged, no hook runs, a listener passed to `Serve` is left open, and the port stays free. After a bind error the app can be served again.

Once the address is bound, the app logs through its logger: one `InstanceLoader` line per named module that is ready then, one `RouteExplorer` line per route (sorted), one `WebSocketEvent` line per event, then the address. When stdout is a terminal, that is the colored access-URL banner (`http://` or `https://`, localhost and the LAN address); otherwise it is a `ServerListening` line with `address` (the bound address), `tls` and `protocols`, for log collectors. Errors of the server itself (a failed TLS handshake, for example) are logged as `Logger.Warn("HTTPServer", "message", <line>)`.

### Protocols

| Server | Protocols |
|---|---|
| `Listen`, `Serve` without TLS | HTTP/1.1 only; an HTTP/2 prior-knowledge client is not served as HTTP/2 |
| `Listen`, `Serve` with `Server.TLS` | HTTP/1.1 and HTTP/2, chosen by ALPN. A `TLS.Config.NextProtos` without `h2` turns HTTP/2 off |
| `IsH2C: true` | HTTP/1.1 and unencrypted HTTP/2 with prior knowledge on the same port. An `Upgrade: h2c` request is answered as HTTP/1.1 |

A handler sees `r.Proto == "HTTP/2.0"` for an HTTP/2 client. WebSocket needs HTTP/1.1: `wss://` clients negotiate it by ALPN, so WebSocket works on a TLS server, and an upgrade sent over HTTP/2 is refused. Use `IsH2C` only on a hop you trust, such as behind a TLS-terminating proxy: never expose an h2c port to the internet.

### Graceful shutdown and Stop

A serving call shuts down on `SIGINT`, `SIGTERM` or `app.Stop()`, and logs `ShutdownSignal` (with `signal`) or `Shutdown`:

1. The app stops intercepting `SIGINT` and `SIGTERM`: a second signal ends the process the default way, even during the drain or a hook.
2. The listener closes (new connections are refused) and idle keep-alive connections close at once. The context of the `AsyncImports` factories is canceled.
3. Running requests finish normally, for at most `ShutdownTimeout`. When it passes, their connections are closed (each request's `Context()` is cancelled).
4. The app waits for the `AsyncImports` goroutines until `ShutdownTimeout` has passed since shutdown began; one still running is logged `ModuleShutdownTimeout` and abandoned. Then the modules' `OnShutdown` hooks run until the same deadline (at least 100 ms when the drain used it all; hooks still running are logged `ShutdownHooksTimeout` and abandoned), only for `ready` modules, after the last request returned, once each, in the reverse of the initialization order (root module first, see [Hooks](#hooks)). A hook that panics is logged (`OnShutdownPanic` with `module` and `panic`) and the other hooks still run.
5. The access log stops, a pending count of suppressed panic stacks is logged, and the log's stdout pipeline is flushed for at most 2 seconds: the lines of the last requests and of the hooks are written when the serving call returns. Lines logged after that are synchronous.
6. The serving call returns `nil`, `owlky: shutdown timed out after <timeout>` when step 3 timed out, or `owlky: serve: <err>` when the server stopped by itself (the listener failed; the hooks run in that case too).

After that the app is stopped: a new serving call returns `owlky: app is stopped`, while `ServeHTTP` still answers.

`Stop()` is safe from any goroutine and any number of times. On a serving app it runs the shutdown above and returns when the hooks have finished. On an app that is not serving, it runs the hooks once and marks the app stopped. The hooks run once per app: every later call waits until the hooks have finished, from any goroutine, and only a call made from inside an `OnShutdown` hook returns at once. Called from a request handler, `Stop()` waits for that same request, so call it as `go app.Stop()` there.

WebSocket connections are not drained: they end when the process exits.

### Timeouts and limits

Every server gets these limits. They are not options.

| Setting | Value | Why |
|---|---|---|
| `ReadHeaderTimeout` | 5 s | Disconnects a client that never finishes its headers (slowloris) |
| `ReadTimeout` (headers and body) | 30 s | Disconnects a client that stalls in the body; 10 MiB in 30 s needs about 2.7 Mbit/s |
| `WriteTimeout` | 30 s | Disconnects a client that doesn't read the response |
| `IdleTimeout` (keep-alive) | 75 s | Longer than the 60 s idle timeout of common proxies, so a proxy never reuses a connection the server just closed |
| `MaxHeaderBytes` | 64 KiB | Headers over the limit answer 431 |
| Request body | 10 MiB | `HTTP.MaxBodyBytes` |

A handler that needs longer (a large upload or download, server-sent events) extends its own deadline:

```go
func (instance FileController) READ_export(c *ctx.HTTPContext) {
	_ = http.NewResponseController(c.ResponseWriter).SetWriteDeadline(time.Time{})
}
```

A panic in a handler answers the fixed 500 and the server keeps serving.

### Deploying

- Run behind a TLS-terminating proxy with `Listen` (add `IsH2C: true` when the proxy speaks h2c), or serve HTTPS yourself with `Server.TLS`. For certificate rotation without a restart, use `TLS.Config.GetCertificate`; `autocert.Manager.TLSConfig()` fits `TLSConfig`.
- Set the proxy's idle timeout below 75 s.
- On Kubernetes, add a `preStop` sleep so the endpoint is removed before `SIGTERM` arrives.

### Advanced: your own server

`App` is an `http.Handler`, so you can run it in a server you configure yourself: other timeouts, several listeners, HTTP/3, or mounting under another mux.

```go
server := &http.Server{Addr: ":8080", Handler: app, ReadHeaderTimeout: 10 * time.Second}
```

Caveats:

- No listener, timeouts or signal handling come from Owlky Go: set them on your server.
- The body cap is `HTTP.MaxBodyBytes` (10 MiB by default); `AppOptions.Server` has no effect.
- No graceful shutdown: call `server.Shutdown`, then `app.Stop()` to run the `OnShutdown` hooks and flush the log. Log lines are asynchronous from `Create` on, so call `app.Stop()` before the process exits.

## Request lifecycle

Every HTTP request follows one fixed order:

```
request
  │  (the WebSocket path leaves here: handshake middlewares, then the upgrade)
  ▼
global middleware ──── no route ────────────────► 405 or 404
  ▼
module middleware
  ▼
global guard ► module guard
  ▼
global pre-interceptor ► module pre-interceptor
  ▼
main handler
  ▼
module post-interceptor ► global post-interceptor
  ▼
response

error in a middleware            ► the error response (no exception filter)
error from a guard onwards       ► module exception filter ► global exception filter ► built-in filter
```

- **Global** layers are bound on the `App` (`BindGlobal…`); **module** layers are bound in a controller with `BindMiddleware`, `BindGuard`, `BindInterceptor` and `BindExceptionFilter`.
- Inside one box, layers run in the order you bound them. Exception filters are the exception: inside one scope the filter bound last runs first.
- The chain is linear. `next()` only says "continue": the next layer starts after `Use` returns. Work that must happen after the handler belongs in an interceptor's `Pipe` operators. (Built-in middlewares such as `compress` use an internal hook to wrap the response and to run code when the exchange ends; it is not part of the public API.)
- A layer stops the request by not continuing: a middleware that returns without `next()`, a guard that returns `false`, an interceptor that returns without calling `Pipe`.
- A filter that returns has handled the error. A filter that panics passes what it panicked with to the next filter; `panic(ex)` with the exception it received passes it on unchanged.
- Middleware is an HTTP concept: a WebSocket connection runs the pre-route stages that apply and the `BindGlobalMiddlewares` once, at the handshake, and no middleware per message.

### Lifecycle rules

From the tests (`http_lifecycle_test.go`):

- Global middlewares run before module middlewares, each scope in bind order; every middleware has returned before the first guard starts.
- Global guards run before module guards; every guard has returned `true` before the first `Intercept` starts.
- Global interceptors' `Intercept` runs before module ones; the handler runs once, after every `Intercept` returned through `Pipe`.
- After the handler, the module interceptors' operators run first, then the global ones, each in reverse bind order. Each operator receives what the previous one produced; the last result is written. The status is `200`, or `201` for `POST`, unless a layer or the handler set another.
- A middleware that returns without `next()`: no later middleware, guard, interceptor, handler or filter runs, and what it wrote is the response (an empty `200` if it wrote nothing). Calling `next()` twice equals once; code after `next()` runs before the next layer starts. A `next` kept from an earlier request does not continue a later one.
- A guard that returns `false`: no later guard, no interceptor and no handler runs; the route's filters receive `exception.ForbiddenException("Access denied")` (403).
- An interceptor that returns without `Pipe`: no later interceptor and no handler runs. Its return value is the data: the operators of the interceptors that ran before it run on it in reverse order, and the result is written (`200`, or `201` for `POST`).
- A panic in a global or module middleware: no exception filter runs, the response is the [error response](#error-response).
- A panic in a guard, an `Intercept`, a pipe, the argument binding, the handler or an operator goes to the route's filters: module filters (bound last first), global filters (bound last first), the built-in filter. With a module filter that returns, no global filter runs.
- A filter that returns has handled the error: nothing more is written, even if the filter wrote nothing. If every filter panics, the error response is written for the last panic.
- A request that matches no route runs only the global middlewares, then answers 404 or 405.
- A request to the WebSocket path runs the pre-route stages of the handshake, then the `BindGlobalMiddlewares` in bind order; a middleware that does not call `next()`, or panics, refuses the handshake. A controller's `BindMiddleware` doesn't run there.
- 64 concurrent requests over every path (success, refusal, panic, 404, 405) answer the same as alone, under `-race`.

## Errors in middleware

A middleware's error never reaches your exception filters: the HTTP layer answers it. If the middleware panics with an exception (`panic(exception.UnauthorizedException("token expired"))`), the client gets that exception's code and message. Anything else (a string, an `error`, a runtime error) answers a fixed `500` and the value goes to the logger, with the request's scope fields and the stack (see [Panic stacks](#panic-stacks)).

You can't change the format of that response. To send your own response, write it and return without calling `next()`:

```go
func (instance AuthMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	if r.Header.Get("Authorization") == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"missing token"}`))
		return
	}
	next()
}
```

`next()` does not wrap the handler: code after `next()` runs before the next layer starts, not after the response. Use an interceptor for work after the handler.

## Error response

The HTTP layer and the built-in exception filter write the same response, with no mode that returns more detail.

**Format.** Without `Render.FS`, every error is JSON. With it:

| Where the error happened | Format |
|---|---|
| A page route (`RENDER_`, `SUBMIT_`), from its first guard on: a guard returning `false`, a panic in a guard, pipe, interceptor, handler or filter | HTML |
| An API route (`common.HTTP`) or a `SERVE_…` route, from its first guard on | JSON, whatever `Accept` says |
| Before any guard: a global or module middleware (`csrf` 403, `hostcheck` 421, `loadshed` 503), no route (404, 405, 410) | HTML when a value of the `Accept` header contains `text/html` (ASCII case-insensitive, q-values are not read), else JSON. `*/*` alone, `application/xhtml+xml` alone or no `Accept` answer JSON. The response carries `Vary: Accept` |

`guards/throttler` is a guard: its 429 follows the route kind and keeps its `Retry-After` and rate headers.

**Client text.** One rule computes `code`, `error` and `message` (JSON) or `.Code`, `.Status` and `.Message` (HTML). No cause text and no 500 detail ever reach the client:

| Source | Code sent | `error` / `.Status` | `message` / `.Message` | Logged |
|---|---|---|---|---|
| Exception without a cause, code 400-599 except 500 | its code | its description, else the status text (`Internal Server Error` when the code has none) | its message (empty: `Unhandled exception has occurred`) | no |
| Exception with a cause (`exception.X("m", err)` or `ExceptionOptions.Cause`), code except 500 | its code | the status text, even when a description is set | its message | `Debug` `ExceptionCause` with `method`, `path`, `code`, `cause` |
| Exception with code 500, or a code outside 400-599 | 500 | `Internal Server Error` | `Internal Server Error` | `Error` `InternalServerError` with `method`, `path`, `message`, `cause`, when its message or cause is not the fixed text |
| Any other panic value | 500 | `Internal Server Error` | `Internal Server Error` | `Error` `UnhandledPanic` (below) |
| A failed page render | 500 | `Internal Server Error` | `Internal Server Error` | `Error` `RenderFailed` |

**JSON.**

- `Content-Type: application/json`; the headers `Content-Length`, `Content-Encoding`, `ETag` and `Last-Modified` set earlier are removed, the others (`X-Request-Id`, `Allow`, `Retry-After`, CORS headers) stay.
- Body: exactly `{"code":<int>,"error":<string>,"message":<string>}` (`Exception.AppendJSON`).

**HTML.**

- The first file that exists at the root of `Dir`: `<code>.html`, then `<N>xx.html` (`4xx.html`, `5xx.html`), inside the root `layout.html` when there is one, with the data `map[string]any{"Code": int, "Status": string, "Message": string, "RequestID": string}` (`RequestID` is `c.ID()`, the `X-Request-Id` of the response). `error.html` has no role (it stops `Create()`, see [Page rules](#page-rules)).
- Without such a file, a built-in page: a complete HTML document with no layout, no style, no script and no external resource (it passes any CSP), holding the code, the status text and the message, escaped.
- When the chosen file or its layout fails, the built-in page is sent with the same code and `RenderFailed` is logged with `template` (`404`, `5xx`); the next file is not tried. An error page is never empty and never a half page.
- `Content-Type: text/html; charset=utf-8`; the same headers as JSON are removed or kept.
- A filter of yours runs first on a page route and may answer by itself; an exception it panics with continues to the next filter and ends as an HTML error.

**Common rules.**

- An exception is an `exception.Exception`, a non-nil `*exception.Exception`, or an `error` that wraps one at any depth.
- Anything else answers the fixed 500. The framework logs `Logger.From(c.Context()).Error("UnhandledPanic", "method", …, "path", …, "panic", <the value>, "stack", <at most 4 KiB>)`: the line starts with the scope's fields (`trace_id`, `request`, and `upstream` when the request carried one) and `stack` may be left out (see [Panic stacks](#panic-stacks)), and exception filters receive `exception.InternalServerErrorException("Internal Server Error")` instead of the value.
- A body or form over `HTTP.MaxBodyBytes` answers `413` `Request body too large`, a file that cannot be opened `400` `Invalid file upload`; the error stays as the cause.
- Once a status or body bytes were sent, no filter runs and nothing more is written: the framework logs `Logger.From(c.Context()).Error("ErrorAfterResponse", "method", …, "path", …, "code", <status sent>, "panic", <value or the exception's message>, "stack", …)`, sampled like `UnhandledPanic`.
- `panic(http.ErrAbortHandler)` is re-panicked so `net/http` aborts the connection; nothing is written or logged.

### Error response rules

From the tests (`http_error_test.go`):

- A middleware's panic and the built-in filter give the same status, body and `Content-Type` for every value.
- A string, an `error`, a runtime error, `panic(nil)`, a nil `*exception.Exception` or any other value answers the fixed 500 body; nothing of the value is in the response. It is logged once as `UnhandledPanic`; an exception is not.
- A filter that prints `Message()` of a non-exception panic prints `Internal Server Error`; a filter's own non-exception panic reaches the next filter as the same fixed exception, and is logged.
- After `c.Text`, `c.JSON`, `c.Redirect`, `w.Write`, `w.WriteHeader` with a code of 200 or more, or `http.ServeFile`, a later panic changes neither status nor body, runs no filter and logs one `ErrorAfterResponse` line. An informational status (`103`) does not count as a response.
- Codes `0`, `7`, `200`, `399`, `600`, `999`, `1008`, `4001` and `-1` answer the fixed 500; `400` and `599` are kept. `WriteHeader` is never called with an invalid code.

From the tests (`http_page_error_test.go`, `guards/throttler/throttler_page_test.go`):

- On a page route a refused guard, a panic in a guard, pipe, interceptor, handler or a filter's re-panic answers HTML even with `Accept: application/json`; the same on an API route answers JSON even with `Accept: text/html`.
- `text/html`, `TEXT/HTML`, a browser's list, `text/html;q=0` and a second `Accept` line holding `text/html` pick HTML before the guards; `*/*`, `application/xhtml+xml`, `text/plain`, an empty or missing `Accept` pick JSON. Without `Render.FS` nothing changes, and no `Vary` is added.
- No case of the client-text table sends a cause's text, an IP or a 500 exception's own message, in either format.
- With a trace listener, a middleware that panics still emits its `middleware` event, with `Code` = the status of the error response; one that returns emits `Code` 0.

### Panic stacks

A client that can make a route panic could flood the log with 4 KiB stacks. Each app samples them, with no option to set:

- A stack key is a hash of the first 16 return addresses where the panic is logged: the same panic reached the same way has the same key.
- The first panic of a key in a second (Unix seconds) is logged with its `stack`. Further panics of that key in the same second, and any panic once 10 stacks were logged in that second, are logged with every other pair and no `stack`; the stack is not even captured. A flood of one panic therefore uses one stack per second, and a rarer panic still gets its own.
- At most 10 lines with `stack` per app per second; two apps do not share the budget. Keys live in a fixed table of 32 slots, so memory does not grow with the number of distinct panics.
- For each second in which a stack was left out, one `Logger.Warn("PanicStacksSuppressed", "stacks_suppressed", n)` line is logged when the second ends (a timer armed by the first suppression; no timer otherwise). Shutdown logs a pending count at once and stops the timer.
- Sampling applies to `UnhandledPanic` (HTTP and the WebSocket handshake) and `ErrorAfterResponse`. Every panic is still logged; only the stack is sampled.

Rate limiting stays opt-in: turn on `guards/throttler` (or limit at the proxy) in production.

## No route, HEAD and versioning

- No route for the path under any method: `404`, body `{"code":404,"error":"Not Found","message":"Not Found"}`, `Content-Type: application/json`, `X-Request-Id` set. The body never depends on the request: a 100,000-character path gets the same bytes.
- A route for the path under another method: `405`, body `{"code":405,"error":"Method Not Allowed","message":"Method Not Allowed"}` and an `Allow` header listing the methods that have a route, in the order `GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS` (`HEAD` exactly when `GET` is). Any request method gets it, `OPTIONS` and unknown methods included.
- `HEAD` is served by the `GET` route of the path, through the whole lifecycle; `c.Method` stays `HEAD`. Over a real server `net/http` sends no body.
- A path longer than 4,096 bytes or with more than 64 segments matches nothing (404).
- With `Versioning`, the route registered for (method, path, version) answers; without one, the route registered with `VERSION_NEUTRAL`. A neutral route answers requests with or without a version. A request without a version to a route registered without one runs the global middlewares, then answers `410` `{"code":410,"error":"Gone","message":"Deprecated URL usage"}`. The `Allow` of a 405 lists the methods of the request's version and the neutral routes.

## Static files

A `SERVE_…` handler returns the path to serve. `SERVE_static_ANY` and `SERVE_ANY_OF_static` are the same route, `/static/*`: with a handler that returns `"public"`, `GET /static/a.txt` serves `public/a.txt`, `GET /static/sub/x.txt` serves `public/sub/x.txt`, `GET /static` is the directory itself and `GET /staticzzz` answers 404.

- A file is served with `http.ServeFile` (ranges, `If-Modified-Since`, content type by extension). Under a wildcard route (`SERVE_<x>_ANY`, `SERVE_ANY_OF_<x>`) the file is opened through `os.Root` on the served directory, with `http.ServeFileFS`.
- A directory answers its `index.html` when it exists (with the redirect to the slash-terminated URL), otherwise the 404 body. No response ever contains a directory listing.
- A resolved path outside the served directory answers `400` `{"code":400,"error":"Bad Request","message":"Invalid URL path"}`; a missing file, or a result that is not a string, answers the 404 body.
- Under a wildcard route, a path with a segment that starts with `.` (`.env`, `.git/config`, `css/.secret`) answers the 404 body, except `.well-known` (RFC 8615). A symbolic link is followed only when its target stays inside the served directory; otherwise the 404 body. Still serve a directory that holds only public files.
- A handler may return `"."`: files are served from the working directory, with the same rules (dot segments answer 404, a path that leaves the directory answers 400).
- The path a handler returns is trusted as is: dot directories in it and symbolic links above the served directory are the application's choice.
- **Caching.** A served file (`200`, `206`, `304`, and the `index.html` of a directory) is sent with `Cache-Control: no-cache` when no layer set `Cache-Control` before: the browser keeps the file but revalidates it with `If-Modified-Since` on every use, and never guesses a lifetime (RFC 9111 § 4.2.2). A `Cache-Control` set by a middleware, guard, interceptor or the handler is kept. Mark hashed assets (`app.3f9a1c.js`) immutable in their handler:

  ```go
  func (c AssetsController) SERVE_ANY_OF_assets(hc *ctx.HTTPContext) string {
  	hc.ResponseWriter.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
  	return "public/assets"
  }
  ```

  No file name is treated as hashed automatically: a wrong guess would cache a file for a year with no way back.
- An error a `SERVE_…` route answers itself (`404`, `400`) removes `Cache-Control` first, so an `immutable` set for assets never lands on an error. Responses of other routes are not touched.

## Pages

`AppOptions.Render` (`&RenderOptions{FS: web}`) turns on server-rendered HTML. In a `common.HTTP` controller, `RENDER_<route>` (or `RENDER`) is a page route: `GET` and `HEAD` on the path the same words give after `READ_`, with the same prefixes, `PathStyle`, `Path`, modules, guards, interceptors, middlewares, pipes and filters. `SUBMIT_<route>` is the `POST` on the path of the `RENDER_` of the same words in the same controller. The route picks the template: the final path without its outer slashes, as `<path>.html` or `<path>/index.html` in `Dir`, with parameters written `{name}` (`RENDER_bookings_BY_id` → `bookings/{id}.html`). The handler's result is the template's data. API routes and page routes may share a controller. The template files follow the conventions of [render](../render/README.md#file-conventions).

### `RenderOptions` fields

| Field | Type | Default | Required | Description |
|---|---|---|---|---|
| `FS` | `fs.FS` | none | yes | The templates and assets, usually the app's `embed.FS` |
| `Dir` | `string` | `"web"` | no | The directory of `FS` that holds them; `"."` is the root of `FS`. A missing directory panics `invalid render: RenderOptions.FS has no "<dir>" directory: embed it with //go:embed <dir>, or set Dir` |
| `IsReload` | `bool` | `false` | no | Development only: re-parse the templates when a file changed. With an `embed.FS`, the directory `Dir` is read from disk (relative to the working directory) instead of the binary; a missing one panics at `New`. `Create()` logs a `Warn` `RenderReloadOn` |

```go
package main

import (
	"embed"

	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/core"
	"go.owlky.com/owlky/internal/ctx"
)

//go:embed web
var web embed.FS

type BookingForm struct {
	Nights string `bind:"nights"`
}

func (instance BookingForm) Transform(f ctx.Form, _ common.ArgumentMetadata) any {
	bound, _ := f.Bind(instance)
	return bound
}

type BookingPages struct {
	common.HTTP
}

func (instance BookingPages) NewController() core.Controller { return instance }

func (instance BookingPages) RENDER_bookings() map[string]any {
	return map[string]any{"Count": 3}
}

func (instance BookingPages) RENDER_new_OF_bookings() BookingForm {
	return BookingForm{Nights: "1"}
}

func (instance BookingPages) SUBMIT_new_OF_bookings(c *ctx.HTTPContext, form BookingForm) BookingForm {
	if form.Nights != "" {
		c.Redirect("/bookings")
	}
	return form
}

func main() {
	app := core.New(&core.AppOptions{Render: &core.RenderOptions{FS: web}})
	app.Create(core.NewModuleBuilder().Controllers(BookingPages{}).Build())
}
```

With `web/bookings/index.html` and `web/bookings/new.html`, `GET /bookings` renders the first, `GET /bookings/new` the second, and `POST /bookings/new` renders the second again with 422 or redirects with 303.

### Page rules
- `Create()` panics `invalid render: <T>.<M> is a page route, set Render.FS in AppOptions` for a `RENDER_` or `SUBMIT_` without `Render.FS`.
- `Render.Dir` or `Render.IsReload` without `Render.FS` panics at `New`: `invalid app: Render.<Field> is set without Render.FS`.
- Template binding at `Create()`: no candidate panics `invalid render: <T>.<M> needs a template: <a> or <b>` (a parameter named differently in the file counts as missing); both panic `invalid render: <T>.<M> has two templates: <a> and <b>`; a path below `partials/`, `assets/` or `public/` panics `invalid render: <T>.<M> can have no template: <dir>/ is reserved`; a final path of one segment named like an error page (`RENDER_404`, `RENDER_5xx`, `Path("/404")`) panics `invalid render: <T>.<M> resolves to 404.html, which is an error page, not a route`. Under a prefix (`admin/404.html`) the name is an ordinary page. A last segment `index` or `layout` uses the directory form only (`courts/layout/index.html`).
- `ANY`, `VERSION` or `FILE` in a page route name panics `invalid route: ANY in RENDER_files_ANY: a page route has no wildcard` (`has no version`, `has no file extension`); a final path holding `*` (from `Path` or a prefix) panics `invalid route: <T>.<M> is a page route and its path <route> holds a wildcard`.
- A `SUBMIT_` without the `RENDER_` of the same words in its controller panics `invalid handler: <T>.SUBMIT_x needs <T>.RENDER_x: a SUBMIT_ posts to the page that shows its form`. `Path` or a `Prefix` bound to a `SUBMIT_` panics `invalid route: Path("/y") is bound to 'SUBMIT_x': a SUBMIT_ uses the path of its RENDER_`; `Path` and prefixes on the `RENDER_` move both.
- Every `.html` file that is not a layout, a partial, a root error page or the template of a route (outside `assets/` and `public/`) panics `invalid render: <file> has no RENDER_ route: add one, move it to partials/, or delete it`, one line per file. `error.html` is such a file.
- A root `200.html`, `302.html`, `3xx.html` or `600.html` panics `invalid render: <file> is not an error page: use 400.html to 599.html, 4xx.html or 5xx.html`.
- `Create()` parses every template once; a parse error, a call of a template that does not exist or an `html/template` escaping error panics `invalid render: <file path>: <error>`.
- A page answers inside its nearest layout with `Content-Type: text/html; charset=utf-8`, a `Content-Length` and `Cache-Control: no-cache` unless a layer or the handler set `Cache-Control`. A `RENDER_` answers 200 and a `SUBMIT_` that renders 422, unless `c.Status(code)` or the `(int, data)` result sets it. `c.Redirect(url)` (or the `ctx.Redirect` parameter) without a 3xx status answers `303 See Other` on a page route; an explicit 3xx is kept, and API routes still answer `301`. `CREATE_`, `UPDATE_` and `DELETE_` stay API routes.
- The data is the handler's only or last result after the interceptors, of any type (a map, a struct, a slice, a string, `nil`); a handler with no result renders with `nil`. A post-interceptor's replacement is the data, and an interceptor that answers without calling the handler supplies it.
- With `Versioning`, page routes answer any version and a request without one.
- `PUT` on a page path answers 405 with an `Allow` header holding `GET` and, with a `SUBMIT_`, `POST`. A guard bound to `RENDER_x` only does not cover `SUBMIT_x`; `BindGuard(g)` covers both.
- Forms bind on page routes as on API routes (urlencoded and multipart, through a `Form` pipe). A malformed form body answers `400` `Invalid form body` and one over `HTTP.MaxBodyBytes` `413` `Request body too large`, as HTML errors. The `csrf` stage refuses a cross-origin form POST with a 403 in the format `Accept` picks. `HEAD` answers the status and headers with no body.
- Values in the data are escaped for their context by `html/template`: text, attributes, URLs (a `javascript:` URL becomes `#ZgotmplZ`) and scripts.
- When the handler already answered (`c.Redirect`, `c.Text`), the result is ignored.
- A template that fails while executing (a field the data's type does not have, a failed `dict` or `asset`) or a page larger than 8 MiB answers the fixed 500 as an [HTML error](#error-response) and logs one `Error` `RenderFailed` with `template` (the template path without `.html`) and `cause`. No byte of the failed page is sent and no file is opened. With `nil` data, `{{.X}}` renders nothing.
- Without `IsReload`, a file changed after `Create()` is never read. With `IsReload`, the next render sees a page, layout, partial or page CSS or JS file that was added, removed or changed (name, size or modification time) and binds every route again (`x.html` moved to `x/index.html` keeps answering). A file that fails to parse, a bound template that is gone or doubled, or a set that lost its `{{pageAssets}}` answers the fixed 500 and logs `Error` `RenderReloadFailed` with `file` and `cause` until it is fixed, never the error text in the response. A new `.html` without a route and a new orphan CSS file are ignored until the restart.

### CSP nonce

`{{nonce}}` in a page, its layout, a partial it reaches or an error page writes the response's nonce: 32 lowercase hexadecimal characters from `crypto/rand`, the same at every use in the page and in the `Content-Security-Policy` header that the `headers` stage sent. Put it on inline scripts and styles:

```html
<script nonce="{{nonce}}">document.documentElement.dataset.theme = "dark"</script>
```

#### Nonce rules
- A nonce is drawn only for a response whose page set (or the error page's set) calls `{{nonce}}`. API routes, assets and pages without it draw none, and their `Content-Security-Policy` is the `headers` stage's static value, byte for byte.
- With `Headers` on and its default policy, a page that calls `{{nonce}}` answers `Content-Security-Policy: default-src 'self'; script-src 'self' 'nonce-<N>'; style-src 'self' 'nonce-<N>'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests`. Custom policies follow [helmet › Nonce](../middlewares/helmet/README.md#nonce).
- Every response has its own nonce (1,000 responses, 1,000 values; 64 concurrent clients never share one). A value from the request (a header, a query, a submitted `Content-Security-Policy`) never becomes the nonce.
- Without `Headers`, or with `ContentSecurityPolicy: "-"`, `{{nonce}}` still renders a nonce and no CSP header is added.
- A `Content-Security-Policy` set by the handler, or by a middleware (they run after the `headers` stage), is left as it is.

### Assets

When `Dir` has an `assets` directory or a page CSS or JS file, `Create()` adds a built-in route of the `SERVE` kind at `/assets/*` that serves `FS/assets`. Link files with `{{asset "css/app.css"}}`, which writes `/assets/css/app.css?v=<16 hex>` (the first 8 bytes of the file's SHA-256, hashed once at `Create()`).

#### Asset rules
- `GET` and `HEAD /assets/<path>` serve `FS/assets/<path>` with the content type of its extension (`text/css`, `text/javascript`, `image/svg+xml`).
- A directory, a missing file, a path with `..` or with a segment starting with `.` (except `.well-known`, which is served and hashed) answers `404` as JSON, whatever `Accept` says. Templates are not reachable through it.
- Without `IsReload`, every file answers `ETag: "<16 hex>"`. With `?v=` equal to that hash it answers `Cache-Control: public, max-age=31536000, immutable`; without `v`, or with another value, `Cache-Control: no-cache`. `If-None-Match` with the ETag answers `304`.
- With `IsReload`, assets answer `Cache-Control: no-cache` and no `ETag`, each request reads the file from `FS` (a file changed or added after `Create()` is served), and `{{asset}}` adds no `?v=`.
- A user route of the same shape (`SERVE_ANY_OF_assets`) panics `route conflict: …` at `Create()`. Without an `assets` directory and page CSS or JS files no route is added and `/assets/*` is free.
- Global middlewares and global guards run for the asset route like for any route; a refusal is JSON.

### Public files

When `Dir` has a `public` directory, `Create()` adds one `GET` route per file, after the controllers' routes, at the file's path below `public`: `public/robots.txt` → `/robots.txt`, `public/.well-known/security.txt` → `/.well-known/security.txt`. The routes are named `public/<path>` in the startup route log and the devtool snapshot.

#### Public file rules
- The response has the content type of the extension, `Cache-Control: no-cache` and `ETag: "<16 hex>"` (the first 8 bytes of the SHA-256, hashed at `Create()`); `If-None-Match` with it answers `304`. `HEAD` answers the headers.
- `.html` files are sent as they are, never parsed. A directory has no index (`public/docs/index.html` is `/docs/index.html` only). Files with a segment starting with `.` other than `.well-known` are not served.
- A route of the same method and path shape panics `route conflict: public/robots.txt method is conflicted with READ_robots_txt_FILE method` (also against a `RENDER_`). With `Versioning` the routes are version-neutral.
- With `IsReload`, `no-cache` and no `ETag`, and the file is read on each request; a file added after `Create()` answers 404 until the restart.
- Errors on these routes are JSON; global middlewares and global guards apply. A path that matches no route follows the [error response](#error-response) rules for no route. Without a `public` directory no route is added.

### Page CSS and JS

`X.css`, `X.<any>.css`, `X.js` and `X.<any>.js` next to `X.html` belong to that page, layout or root error page (the longest `X` wins). `{{pageAssets}}`, usually in the layout's `<head>`, writes their tags: the layout's files, then the page's, each group sorted by path:

```html
<link rel="stylesheet" href="/assets/_pages/layout.css?v=…">
<link rel="stylesheet" href="/assets/_pages/bookings/new.css?v=…">
<script src="/assets/_pages/bookings/new.form.js?v=…" defer></script>
```

#### Page CSS and JS rules
- A CSS or JS file that belongs to no template, or any under `partials/`, panics `invalid render: <file> belongs to no template: name it after a page or layout of its directory (X.css, X.<name>.css), or move it to assets/`.
- A page set that owns a file and in which no template (layout, page, reached partial) calls `{{pageAssets}}` panics `invalid render: <page> has <file>, but no template of the page calls {{pageAssets}}: add it to <layout>`.
- `GET` and `HEAD /assets/_pages/<path>` serve exactly these files with the asset rules below (`immutable` for a matching `?v=`, else `no-cache`, `ETag`, `304`); a template, a partial or an unowned file there answers 404 JSON. Path segments are percent-encoded in the tags (`%7Bid%7D.css`). A file or directory `assets/_pages` panics `invalid render: assets/_pages is reserved for page CSS and JS`.
- The tags are same-origin files: `{{pageAssets}}` draws no nonce. With `IsReload` the tags carry no `?v=` and follow the files.

### Render events and devtool
- With a trace listener, each render emits a `render` event (see [Trace events](#trace-events)).
- With `IsDevtool`, each page route has `kind: "page"` in the snapshot, and the snapshot has a `render` section (see [devtool](../devtool/README.md#render)).

## Response writer

Every layer receives the same writer as `c.ResponseWriter` (middlewares as their `http.ResponseWriter`). It records whether a response was sent, and forwards `Flush`, `Hijack`, `ReadFrom` (so `http.ServeFile` keeps `sendfile`) and `Unwrap` (so `http.ResponseController` reaches `SetWriteDeadline` and friends). Like `net/http`'s own writer, never write to it after the handler returns: until the context is reused, such a write gets an error and reaches nothing; after that it can reach another request's response, and it is a data race.

Some header values are slices built once and shared, so setting them costs no allocation: the `Cache-Control: no-cache` of static files, the headers of `helmet`, `cors` and `compress`, and the `X-Request-Id` and `Traceparent` values, which share their memory with the request's trace scope. A page with a nonce never edits `helmet`'s shared policy slice: `helmet` hands its precompiled nonce form to the writer (`internal/httphook`'s `CSPWriter`), and the page assigns a new slice only when the header still holds `helmet`'s own. Change a header only through `Header().Set`, `Add` or `Del`, or by assigning a new slice. Never write into the slice you read (`w.Header()["Cache-Control"][0] = "no-store"`): that changes the value for every other response that shares it, and is a data race.

## Handler injection

A handler is a controller method whose name maps to a route (`READ_users_BY_id`) or a WebSocket event. Its parameters are injected by type and its results are written to the response.

### Parameters

Parameters are matched by type identity: a type from another package that only prints the same (a `*ctx.HTTPContext` of your own package named `ctx`) is not accepted.

| Parameter type | HTTP handler | WS handler | Argument |
|---|---|---|---|
| `*ctx.HTTPContext` | yes | no | the request's context |
| `*http.Request` | yes | no | `c.Request` |
| `http.ResponseWriter` | yes | no | `c.ResponseWriter` (a nil writer is passed as nil) |
| `ctx.Body`, `ctx.Form`, `ctx.Query`, `ctx.Header`, `ctx.Param`, `ctx.File` | yes | no | `c.Body()`, `c.Form()`, `c.Query()`, `c.Header()`, `c.Param()`, `c.File()` |
| `ctx.Redirect` | yes | no | `c.Redirect` |
| `*ctx.WSContext` | no | yes | the message's context |
| `*websocket.Conn` (`golang.org/x/net/websocket`) | no | yes | `c.Conn` |
| `ctx.WSPayload` | no | yes | `c.WSPayload()` |
| `ctx.Next` | yes | yes | the middlewares' `next` (`ctx.HTTPNext(c)` / `ctx.WSNext(c)`) |
| `common.Publisher` | yes | yes | the app's publisher, looked up at `Create()` |
| a pipe of `common.ContextPipeable`, `BodyPipeable`, `FormPipeable`, `QueryPipeable`, `HeaderPipeable`, `ParamPipeable` or `FilePipeable` | yes | no | what its `Transform` returns |
| a pipe of `common.WSPayloadPipeable` | no | yes | what its `Transform` returns |

Anything else stops `Create()` with:

```
dependency injection: can't resolve dependencies of the 'func(*ctx.HTTPContext, int) string'. Please make sure that the argument dependency at index [1] is available in the handler
```

This covers an unknown type, a variadic parameter, an interface type other than `common.Publisher`, a type of the other transport (`*ctx.WSContext` in an HTTP handler, `ctx.Body` in a WS handler), and `common.Publisher` when no publisher is stored.

### Pipes

A pipe is a value type with a `Transform(source, common.ArgumentMetadata) any` method, where the source is the context or one of the request data types. Struct and non-struct types (`type UserID int`) are both accepted.

- Exported fields of a struct pipe get providers injected at `Create()`, like a controller's: a provider of the module, else a global provider, else a global interface (such as `log.Logger`), else the zero value. An unexported field or an unresolvable provider field stops `Create()`.
- `Transform` runs once per request, in parameter order, before the handler. Its receiver is a fresh copy of the injected prototype on every request: changing its fields in `Transform` affects neither the next request nor a concurrent one.
- `ArgumentMetadata.ParamType` is the source's key (`common.QueryPipeableKey`, …) and `ContextType` is `common.ContextTypeHTTP` or `common.ContextTypeWS`.
- A pointer parameter (`*PageDTO`) stops `Create()`: `dependency injection: pipe '*main.PageDTO' of the 'func(*main.PageDTO) ctx.Map' must not be a pointer`.
- `Transform` must return a value of the parameter's type (or one assignable to it). `nil` or another type is a `500` exception, `pipe main.PageDTO: Transform returned *main.PageDTO, want main.PageDTO`, and the handler is not called.

### Results

A handler returns nothing, one value, or `(status int, value)`. With two results the first one sets the status code when it is a known HTTP status; a third result is ignored. The value is written by its kind:

| Kind | Response |
|---|---|
| map, slice, struct, array | JSON |
| pointer | JSON of the pointee; a nil pointer writes `null` |
| string | text |
| bool, integers, floats, complex numbers | text (`strconv` formatting; floats use `'g'`: `1e+08`) |
| func | its type as text |
| interface (`any`) | a string is written as text; anything else as JSON (`7`, `true` and `100000000` stay `application/json`); a nil interface writes `null` |
| chan, `uintptr`, `unsafe.Pointer` | nothing |

WS handlers reply with their last result as the message.

### Trace events

Each app has its own `trace.Tracer`. It is internal: `AppOptions.IsAccessLog` adds the only listener, and there is no public way to add another. With that listener, each pipe argument emits a `pipe` event (name: the parameter type, transport `http` or `ws`, the time its `Transform` took), also when `Transform` panics or returns a wrong value. Each HTTP handler call emits a `handler` event (name: the method name, `SERVE_…` routes included) whose duration excludes the pipes; it is not emitted when an argument failed. WS handlers emit no `handler` event. Without a listener nothing is emitted and no clock is read. Each render of a page or an HTML error page emits a `render` event after the `handler` event and before `complete`: `Name` is the template path without `.html` (`bookings/{id}`, `404`, `4xx`) or `error (built-in)` for the built-in page, `Duration` the time to execute the template and place the nonce, and `Code` 500 when the render failed. A JSON error renders nothing.

Every event carries `ID`, `TraceID`, `SpanID`, `ParentID`, `Stage`, `Name`, `Transport` (`HTTP` or `WS`, on every stage) and `Duration`; the `complete` event also carries `Operation` (the method or the WS message type), `Target` (the URL path without the query string, or the WS topic), `Code` and, for an HTTP request or handshake, `ClientIP` (as `owlky.ClientIP` resolves it). `Enabled()` is asked once per request: a request that started without a listener emits nothing, exception filter events included.

The events of one request or WebSocket message form a span tree: `complete` is emitted last and is the root (`SpanID == ID`, `ParentID` the incoming `traceparent` parent, `""`, or the connection ID for a WS message); every other stage is a child with a fresh `SpanID` and `ParentID == ID`, in lifecycle order. Span IDs are generated only while a listener exists: without one, a request generates its own ID and nothing else. With `EnableAccessLog()`, the access log is stopped once at shutdown, after the `OnShutdown` hooks, so the lines of the last requests are written before the process exits.

### Rules

From the tests (`fn_plan_test.go`, `fn_plan_ws_test.go`, `fn_app_test.go`, `fn_test.go`):

- Each parameter of the table receives the same value a direct call with the context's accessors would pass: the same context, request and connection pointers, the same body, form, query, header, param, file and payload maps; `Next` and `Redirect` act on the context.
- The publisher is the one stored when the plan is built.
- Building a plan calls neither the handler nor any `Transform`.
- A call runs the handler once, on the controller value given at build (pointer or value receivers), and returns its results in declaration order.
- `Transform` calls run left to right, then the handler. When an argument panics (an accessor's exception, such as `413` for an oversized body, or a `Transform` panic), the panic propagates unchanged, later arguments are not resolved and the handler is not called. A handler's panic propagates unchanged.
- One plan is safe for concurrent calls.
- The arguments and results are the same with and without a trace listener; a listener added or removed between two calls takes effect on the next call.
- A handler parameter whose type only prints like a framework type, a wrong `Transform` result, a pointer result and an `any` result go through `Create` and `ServeHTTP` as described above: `Create()` panics, `500` without reflection text, JSON, text.

## Other exported types

- `HTTP` is the app's HTTP router and layer store. It has no exported fields or methods: you never build or use one.
- The WebSocket side (`WS`, `NewWS`, `WSOptions`, `WSConnmgr`, `NewWSConnmgr`, `WSConnection`, `WSPayload`, `WSPayloadType` and its `Type…` constants) is documented with the WebSocket review of this package. `AppOptions.WS` (`&core.WSOptions{Path: "/ws"}`) turns it on.

## Performance notes

- **The plan.** At `Create()` each handler gets a list of argument steps: the parameter types are compared once, pipe prototypes are built once (with their providers injected), the publisher is looked up once, and `ctx.BinderOf` is called once per struct pipe type so the first `Bind` of that type walks no types. A request runs the steps and calls the method once.
- **One reflection hop.** The plan calls the method through `reflect.Type.Method(i).Func` with the receiver as the first argument, not through a method value (which reflection would enter a second time).
- **Arguments on the stack.** Handlers with up to 8 parameters build their argument list in a local array; longer handlers allocate it.
- **Fresh pipe receivers without copying by hand.** A pipe prototype is held as an interface value; calling `Transform` on a value receiver copies it, so no per-request `reflect.New` or injection is needed.
- **Tracing is separate.** One listener check per call; the traced path has its own functions, so the untraced path has no `defer` and reads no clock.
- What remains is the cost of `reflect.Value.Call` (one allocation for the result slice, plus one per result that is not pointer-shaped) and whatever the accessors and pipes do.
- **One route struct per route.** At `Create()` each route gets an immutable `httpRoute` (module middlewares, guards and interceptors with the global ones in front, the handler plan, the filter list, the `SERVE` index), stored in a slice at the router item's index: a request does one slice index instead of three map lookups keyed by the pattern string.
- **No closure without middleware.** A route without middleware gets a shared no-op `next`; with middlewares, one closure and one flag per request, passed to every `Use` (a closure kept on the pooled context would let a leaked `next` continue another request). Guards and interceptors are called directly, and a refused guard reaches the filters without a panic.
- **One listener check, one recover.** `Tracer.Enabled` (one atomic load) is asked once per request; `handleRequest` holds the request's only `defer` / `recover`, and `ServeHTTP` releases the context in straight-line code.
- **Fixed bodies.** The 404, 405, 410 and fixed 500 bodies are byte slices built once; a miss tries only the methods that have routes for the `Allow` header, which comes from a table built once.
- **One atomic load for the body cap.** The cap is an `atomic.Int64` written when a serving call starts and loaded only for a request with a body (about 1 ns, no allocation). The rest of the server code runs once per start or stop.
- **No `MaxBytesReader` for a bodiless request**, and the router appends parameter values to the pooled context's `ctx.HTTPParamValues(c)`, whose capacity `ctx.HTTPReset` keeps.
- **Hooks are inlined.** The `ctx` and `aggregation` hooks (`ctx.HTTPSetCode`, `ctx.HTTPSetParams`, `aggregation.SetMainData`…) are package functions with the context as first parameter; `go build -gcflags=-m` reports each as inlinable, so they cost what the field accesses and methods they replace cost.
- **Tracking writer in the pooled exchange.** The writer lives next to the context in the pool, so it costs no allocation. Its internal exchange hooks (used by `compress`) cost one length check per request when none is registered; the hook list keeps its capacity across pooled exchanges, and the clock is read once, only when a hook exists.
- **Static caching.** The `no-cache` value of a served file is a one-element slice built once and assigned to the header map: no allocation.
- **The trace scope.** Each request builds one scope (the `traceparent` string and a context value of 240 B together) and copies the request with `r.WithContext` (320 B) so middleware sees the IDs: about +270 ns, +530 B and +1 allocation against the request ID it replaces. The response headers reuse one-element slices prebuilt in the scope, so they allocate nothing. The random bytes come from a 512-byte `crypto/rand` buffer (`internal/tracecontext`), one system call per about 28 requests.
- **The cost of an unexpected panic.** A non-exception panic hashes 16 return addresses for its stack key (about 1.6 µs) and captures up to 4 KiB of stack only when the line keeps it (about 25 µs and 7.6 KB). Under a flood almost every panic is logged without a stack: `BenchmarkServeHTTP_PanicFlood` is about 6.5 µs per request against about 37 µs before sampling.

## Benchmarks

Benchmark results on darwin/amd64 (Intel Core i7-9750H @ 2.60GHz, Go 1.27.4), `go test -run '^$' -bench . -benchmem ./core/`, median of 5 runs. A plan call is measured on a prepared context without a trace listener unless the name says `Traced`.

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| BenchmarkServeHTTP_Static (`GET`, a string result) | 940 | 624 | 7 |
| BenchmarkServeHTTP_StaticWithTraceparent (a valid incoming `traceparent`) | 943 | 624 | 7 |
| BenchmarkServeHTTP_StaticWithListener (a trace listener: two stage spans) | 1210 | 640 | 8 |
| BenchmarkServeHTTP_StaticParallel | 687 | 624 | 7 |
| BenchmarkServeHTTP_Chain (a middleware, a guard, an interceptor) | 1117 | 968 | 11 |
| BenchmarkServeHTTP_MultiInterceptor (three interceptors) | 1320 | 1200 | 10 |
| BenchmarkServeHTTP_NotFound | 760 | 576 | 4 |
| BenchmarkServeHTTP_MethodNotAllowed | 716 | 592 | 5 |
| BenchmarkServeHTTP_Exception (a handler panics an exception) | 1645 | 784 | 7 |
| BenchmarkServeHTTP_PanicString (a handler panics a string, one route: after the first, the stack is sampled out) | 6070 | 1168 | 14 |
| BenchmarkServeHTTP_PanicFlood_Parallel (the same, 12 goroutines) | 2042 | 1169 | 14 |
| BenchmarkHTTPHandlerPlan_Invoke_NoArgs (`func() string`) | 210 | 40 | 2 |
| BenchmarkHTTPHandlerPlan_Invoke_Param (`func(ctx.Param) ctx.Map`) | 206 | 24 | 1 |
| BenchmarkHTTPHandlerPlan_Invoke_QueryPipe (two-field DTO) | 265 | 48 | 2 |
| BenchmarkHTTPHandlerPlan_Invoke_BodyPipeWithProvider | 278 | 56 | 2 |
| BenchmarkHTTPHandlerPlan_Invoke_FiveBuiltins | 575 | 16 | 1 |
| BenchmarkHTTPHandlerPlan_Invoke_QueryPipe_Traced | 654 | 80 | 4 |
| BenchmarkNewHTTPHandlerPlan (cold, `Create()`) | 1086 | 352 | 10 |
| BenchmarkReturnHTTP_String | 71 | 46 | 1 |
| BenchmarkReturnHTTP_Int | 89 | 43 | 2 |
| BenchmarkReturnHTTP_Float | 128 | 38 | 2 |
| BenchmarkReturnHTTP_Map | 1011 | 219 | 10 |
| BenchmarkReturnHTTP_PointerToStruct | 273 | 62 | 2 |
| BenchmarkReturnHTTP_InterfaceString | 60 | 43 | 1 |
| BenchmarkModuleBuilderBuild (cold) | 8073 | 824 | 10 |
| BenchmarkCreate_FiveControllers (cold) | 41706 | 27392 | 378 |
| BenchmarkCreate_ChainedProviders (cold) | 118651 | 78177 | 697 |
| BenchmarkCreate_StaticSubmodules (cold) | 73707 | 69824 | 496 |
| BenchmarkCreate_FactorySubmodules (cold) | 62788 | 6720 | 66 |

The trace scope (design 0011, same machine, `benchstat` against the code before it, 6 runs): Static 615 → 884 ns (96 → 624 B, 6 → 7 allocations), NotFound 485 → 745 ns (48 → 576 B, 3 → 4), MethodNotAllowed 433 → 699 ns (64 → 592 B, 4 → 5).

Against the previous dispatch (same machine, `benchstat`, 5 runs): Static 837 → 636 ns (9 → 6 allocations), Chain 1,277 → 806 ns (13 → 10), NotFound 2,387 → 523 ns (23 → 3), MethodNotAllowed 2,285 → 464 ns (23 → 4), Exception 1,913 → 1,570 ns (10 → 6), MultiInterceptor 1,827 → 972 ns (15 → 9).

*Note: These benchmarks are machine-dependent and were captured at documentation generation time (November 16, 2026).*
