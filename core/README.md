# Core

*Package `core` is the dependency injection container and the server of `Ginject`: it builds the module tree, injects providers into controllers, plans every handler's arguments once at `Create()` and serves HTTP and WebSocket requests through the middleware, guard, interceptor, handler and exception filter pipeline.*

- [Core](#core)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`App`](#app)
    - [New](#new)
    - [Create](#create)
    - [ServeHTTP](#servehttp)
    - [Get](#get)
    - [SetLogger and Logger](#setlogger-and-logger)
    - [Configuration methods](#configuration-methods)
  - [Modules and lifecycle](#modules-and-lifecycle)
    - [ModuleBuilder](#modulebuilder)
    - [`Module` fields](#module-fields)
    - [Prefix and ID](#prefix-and-id)
    - [Provider and Controller](#provider-and-controller)
    - [Providers](#providers)
    - [Hooks](#hooks)
    - [Two apps in one process](#two-apps-in-one-process)
    - [Misuse](#misuse)
  - [Server](#server)
    - [Listen, ListenTLS and Serve](#listen-listentls-and-serve)
    - [ServerOptions](#serveroptions)
      - [ShutdownTimeout](#shutdowntimeout)
      - [MaxRequestBodyBytes](#maxrequestbodybytes)
      - [TLSConfig](#tlsconfig)
      - [H2C](#h2c)
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
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
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
	app := core.New()
	app.Create(
		core.ModuleBuilder().
			Controllers(UserController{}).
			Providers(GreetingService{}).
			Build(),
	)
	if err := app.Listen(3000, nil); err != nil {
		panic(err)
	}
}
```

`GET /users/7?page=2` answers `{"greeting":"hello 7","page":2}`.

## `App`

`App` holds everything it wires: its modules, providers, layers, routes, logger and publisher, the HTTP router, the optional WebSocket server and the request tracer. Configure it with the methods below, wire it once with `Create`, then serve with `Listen`, `ListenTLS` or `Serve` (see [Server](#server)).

### New

Returns an `App` with the default request body limit (10 MiB), the built-in exception filters, the built-in logger and a `common.Publisher` backed by the app's own in-memory broker.

#### Returns
- 1st value: `*App`

### Create

Builds the module tree from the root module, initializes the modules (see [Modules and lifecycle](#modules-and-lifecycle)), creates the controllers, plans every handler, binds middlewares, guards, interceptors and exception filters, then runs the `OnReady` hooks. It panics (`"dependency injection: ..."`, `"route conflict: ..."`, `"event conflict: ..."`, `"invalid app: ..."`, …) when the application can't be built. Call it once per app. `Create` never writes to the modules it is given.

#### Parameters
- 1st parameter: `*Module`

- Description: The root module, built with `ModuleBuilder()`.

#### Usage

```go
app.Create(core.ModuleBuilder().Controllers(UserController{}).Build())
```

### ServeHTTP

`ServeHTTP(w http.ResponseWriter, r *http.Request)` serves one request: it caps a request body at the app's limit (10 MiB, or the `MaxRequestBodyBytes` of the serving call; a request without a body, `nil` or `http.NoBody`, is not wrapped), takes a pooled context, builds the request's trace scope, sets the `X-Request-Id` and `Traceparent` response headers before any middleware runs, runs the [request lifecycle](#request-lifecycle) and, with a trace listener, emits the `complete` event, whose `Code` is the status actually written (`200` when nothing was written). Requests to the WebSocket path are upgraded when `EnableWS` was called.

#### Request and trace IDs
- Every response (a route, a 404, a 405, an error) carries `X-Request-Id: <request ID>` (16 hex characters, the same as `c.GetID()`) and `Traceparent: 00-<trace ID>-<request ID>-<flags>`.
- The request ID is always the app's own: neither an incoming `X-Request-Id` nor a `traceparent` sets it.
- One valid incoming `traceparent` continues its trace ID and keeps its flags (`& 03`); an absent, repeated or invalid one (upper case, zero IDs, version `ff`, CR/LF, a megabyte of junk) is ignored silently and a new, time-ordered trace starts with flags `03`.
- The scope is in `c.Context()`, `c.Request.Context()` and the `*http.Request` every middleware receives, so guards, interceptors, filters, the handler and a service called with `c.Context()` all read the same IDs. A goroutine that keeps `c.Context()` reads them after the request ended and the pooled context was reused; concurrent requests never read each other's scope.
- An outbound `net/http` request built with `c.Context()` and sent through `trace.Transport` forwards the trace (`Traceparent`); see [trace](../trace/README.md#outbound-calls).
- A WebSocket handshake follows the same rules: the 101 response carries both headers, and the connection ID (the `connected` payload's `id`, the `WSConnmgr` key) is the handshake's request ID. Each message dispatched to a topic is its own message with its own ID (a frame naming two topics is two messages); its `c.Context()` carries the connection's trace, the connection ID and the message ID, and is done when the connection closes.
- A refused upgrade (bad version, missing key) still releases the pooled context.
- The scope adds one allocation to a request with no trace listener (7 for a static route, 4 for a 404, 5 for a 405), and continuing a trace costs no more.

### Get

`Get(p Provider) any` returns the instance this app created for `p`'s type, or `nil` for a type the app does not have and before `Create`. It is safe to call from several goroutines after `Create`.

```go
service := app.Get(GreetingService{}).(GreetingService)
```

### SetLogger and Logger

```go
func (app *App) SetLogger(logger common.LogAdapter) *App
func (app *App) Logger() common.Logger
```

`SetLogger` is the one way to give the app a logger; it returns the app. It takes any `common.LogAdapter` (the five methods `Debug`, `Info`, `Warn`, `Error`, `Fatal`: a zap `SugaredLogger` fits). Everything logged through `app.Logger()`, the server and access logs, and every injected `common.Logger` field reaches it. A custom logger gets the `log` struct tags and masking machinery and `From` (`log.WrapLogger(logger, nil)`); a logger that already came from `log.NewLog` or `log.WrapLogger` is used as it is. A custom logger is called directly on the caller: the bounds of the log's output pipeline do not apply to it.

`Logger()` never returns `nil`: without `SetLogger` it is the built-in logger with default options, the same value on every call. `app.Logger()` and every injected `common.Logger` have `From(ctx)`, whatever was given to `SetLogger`: inside a request, `logger.From(c.Context()).Info(…)` adds the trace ID and the request ID to the line. Only fields of type `common.Logger` are injected; a `common.LogAdapter` field is not.

```go
app.SetLogger(log.NewLog(&log.LogOptions{LogFormat: log.JSONFormat}))
app.SetLogger(log.WrapLogger(myLogger, []string{"password"}))
app.Logger().Info("Started")
```

#### Rules
- `SetLogger(nil)` panics `invalid app: SetLogger needs a logger`.
- A logger wrapped with `log.WrapLogger(l, fields)` keeps its masking rules.
- A type with the five methods and no `From` is accepted, and `From` on `app.Logger()` passes the scope's fields to it first, then the call's arguments.
- The built-in logger's lines are synchronous during `Create` (its `OnReady` hooks included): a bootstrap panic loses no line. As the last step of `Create` the stdout pipeline becomes asynchronous; it becomes synchronous again when the last created app has shut down (see `log/README.md`, Output pipeline).

### Configuration methods

Each returns the `App` so calls can be chained. Call them before `Create`: after it they panic `invalid app: <Method> must be called before Create`.

| Method | Effect |
|---|---|
| `BindGlobalMiddlewares(...common.MiddlewareFn)` | Middlewares that run for every HTTP request |
| `BindGlobalGuards(...common.Guarder)` | Guards for every HTTP route and WS event |
| `BindGlobalInterceptors(...common.Interceptable)` | Interceptors for every HTTP route and WS event |
| `BindGlobalExceptionFilters(...common.ExceptionFilterable)` | Exception filters for every HTTP route and WS event |
| `EnableVersioning(versioning.Versioning)` | Route versioning (`Key` defaults to `v`) |
| `EnableWS(*WSConfig, ...common.MiddlewareFn)` | The WebSocket server, with handshake middlewares |
| `EnableDevtool()` | The devtool snapshot of routes and layers |
| `EnableAccessLog()` | One access log line per request |
| `SetLogger(common.LogAdapter)` | The app's logger (see [SetLogger and Logger](#setlogger-and-logger)) |

## Modules and lifecycle

A module groups controllers and providers and imports other modules: `*Module` values or module funcs (`func(...) *core.Module`). Module funcs are called once per app, whatever the number of modules that import them, while the tree is discovered. Their arguments resolve from the global providers (the root module's, and those of every `IsGlobal` module of the tree, at any import depth and in any import order), then `common.Logger` and `common.Publisher`; an argument with no provider panics `dependency injection: can't resolve argument of '<func>' …`. An `IsGlobal` module returned by a module func is visible to the module funcs called after it. The provider of an argument is built after the `OnInit` of the module that declares it, so that module's `OnInit` runs before the module func is called (and not again later).

### ModuleBuilder

```go
func ModuleBuilder() *moduleBuilder
func (m *moduleBuilder) Imports(modules ...any) *moduleBuilder
func (m *moduleBuilder) Providers(providers ...Provider) *moduleBuilder
func (m *moduleBuilder) Controllers(controllers ...Controller) *moduleBuilder
func (m *moduleBuilder) Build() *Module
```

`ModuleBuilder()` starts a module. `Imports`, `Providers` and `Controllers` append to the module's lists and return the builder, so calls can be chained and repeated. `Build()` returns the `*Module`.

#### Parameters
- `Imports`: `...any`, each a `*Module` or a module func whose type starts with `func(` and returns `*core.Module`.
- `Providers`: `...Provider`, provider values (see [Provider and Controller](#provider-and-controller)).
- `Controllers`: `...Controller`, controller values.

#### Returns
- `ModuleBuilder`, `Imports`, `Providers`, `Controllers`: `*moduleBuilder`, the same builder.
- `Build`: `*Module`, a new module with its own `ID()` and a resolved `Name`.

#### Usage

```go
var UserModule = core.ModuleBuilder().
	Controllers(UserController{}).
	Providers(GreetingService{}).
	Build()

var AppModule = core.ModuleBuilder().
	Imports(UserModule).
	Build()
```

#### Rules
- An import that is neither a `*Module` nor a module func panics `invalid module: can't pass '<type>' type as module` when `Build` runs.
- `Name` is `<package>.<variable>` of the variable the module is assigned to (a `var X = func() *core.Module {…}` or a `var X = factory()` included), else `<package>.<function>` of the function that calls `Build`. A module built as an argument of `Imports` gets its own factory's name, never the outer variable's.
- Chained calls accumulate: `Providers(p1).Providers(p2)` lists both.

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

Runs when the module is initialized, before its providers are constructed (see [Hooks](#hooks)).

#### OnReady
Type: `func()`

Default: `nil`

Required: `false`

Runs once at the end of `Create` (see [Hooks](#hooks)).

#### OnShutdown
Type: `func()`

Default: `nil`

Required: `false`

Runs once when the app shuts down or `Stop()` is called (see [Hooks](#hooks) and [Graceful shutdown and Stop](#graceful-shutdown-and-stop)).

```go
var CacheModule = func() *core.Module {
	m := core.ModuleBuilder().Providers(GreetingService{}).Build()
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
var V1Module = core.ModuleBuilder().Controllers(UserController{}).Build().Prefix("v1")
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

### Two apps in one process

Each `core.New()` is independent: its routes, providers, layers (guards, interceptors, exception filters, middlewares), logger and publisher are its own. Two apps may declare the same route or WebSocket event, construct their own instance of every provider and layer, and can be created on two goroutines at once.

You can give the same module value (a root, or a package-level `var X = some.Register(…)`) to any number of apps. A module func may be called by two apps at once: it must not write shared state without its own protection. A resource created outside the framework, such as a cache backend built in a package-level `Register` call, is shared because you created it once. Don't change a module (`Prefix`, `IsGlobal`, a hook) after giving it to `Create`.

Inside one app, a guard, interceptor, exception filter or middleware is constructed once per type and configuration: equal configurations share one instance, different ones get their own.

### Misuse

- `Create(nil)` panics `invalid module: Create needs a root module`.
- A second `Create` on one app panics `invalid app: Create called twice`.
- A configuration method after `Create` panics `invalid app: <Method> must be called before Create`.
- Two routes of one app with the same method, version and shape (parameter names don't count) panic `route conflict: <fn> method is conflicted with <fn> method`; two handlers of one event panic `event conflict: <fn> event is conflicted with <fn> event`.

## Server

### Listen, ListenTLS and Serve

```go
func (app *App) Listen(port int, opts *ServerOptions) error
func (app *App) ListenTLS(port int, certFile, keyFile string, opts *ServerOptions) error
func (app *App) Serve(l net.Listener, opts *ServerOptions) error
```

Call one of them once, after `Create`. Each one blocks until the server has stopped. `nil` options give the safe defaults.

- `Listen` listens on TCP `:<port>` (every interface) and serves HTTP/1.1.
- `ListenTLS` does the same with TLS, and serves HTTP/2 as well. Pass a certificate and key file, or two empty names and a `TLSConfig` that supplies certificates (`Certificates`, `GetCertificate` or `GetConfigForClient`).
- `Serve` serves on a listener you made: a loopback or Unix socket address, systemd socket activation, or `127.0.0.1:0` in a test. It serves TLS when `TLSConfig` is set, plain HTTP otherwise.

```go
if err := app.Listen(8080, nil); err != nil {
	app.Logger().Fatal("AppError", "error", err)
}

err := app.ListenTLS(443, "cert.pem", "key.pem", nil)

err := app.ListenTLS(443, "", "", &core.ServerOptions{TLSConfig: manager.TLSConfig()})

l, _ := net.Listen("tcp", "127.0.0.1:8080")
err := app.Serve(l, nil)
```

A server that only listens on loopback uses `Serve` with a listener on `127.0.0.1`: `Listen` always binds every interface.

### ServerOptions

Pass `nil` for the defaults. A zero field takes its default too.

#### ShutdownTimeout
Type: `time.Duration`

Default: `0`, which means 30 s (a negative value too)

Required: `false`

How long running requests may finish during a shutdown.

#### MaxRequestBodyBytes
Type: `int64`

Default: `0`, which means 10 MiB

Required: `false`

The request body cap. A negative value turns the cap off. A body over the cap answers 413, whether its length is declared or chunked.

#### TLSConfig
Type: `*tls.Config`

Default: `nil`

Required: `false` (`ListenTLS` needs it when no certificate files are given)

The TLS configuration of `ListenTLS` and `Serve`. It is cloned, never modified.

#### H2C
Type: `bool`

Default: `false`

Required: `false`

For `Listen` and `Serve` without TLS: also accept unencrypted HTTP/2.

```go
err := app.Listen(8080, &core.ServerOptions{
	ShutdownTimeout:     10 * time.Second,
	MaxRequestBodyBytes: 1 << 20,
	H2C:                 true,
})
```

The options are read once, when the call starts: changing the struct later has no effect.

### Startup errors and logs

The serving calls return an error, and never fall back to plain HTTP, when:

| Case | Error |
|---|---|
| Called before `Create` | `core: Create must be called before serving` |
| The app is already serving | `core: app is already serving` |
| The app has stopped | `core: app is stopped` |
| `Listen` with a `TLSConfig` | `core: TLSConfig is set: use ListenTLS, or Serve with your listener` |
| `ListenTLS`, or `Serve` with a `TLSConfig`, with `H2C` | `core: H2C applies to connections without TLS` |
| No certificate source, or only one of the file names | `core: ListenTLS needs certFile and keyFile, or a TLSConfig with certificates` |
| The certificate pair can't be loaded | `core: load TLS certificate: <err>` |
| The port can't be bound (in use, out of range) | `core: listen on :<port>: <err>`, wrapping `net.Listen`'s error |

In these cases nothing is logged, no hook runs, a listener passed to `Serve` is left open, and the port stays free. After a bind error the app can be served again.

Once the address is bound, the app logs through its logger: one `InstanceLoader` line per named module, one `RouteExplorer` line per route (sorted), one `WebSocketEvent` line per event, then `ServerListening` with `address` (the bound address: `Listen(0, nil)` logs the port the system chose), `tls` and `protocols`. The colored access-URL banner is printed only when stdout is a terminal. Errors of the server itself (a failed TLS handshake, for example) are logged as `Logger.Warn("HTTPServer", "message", <line>)`.

### Protocols

| Server | Protocols |
|---|---|
| `Listen`, `Serve` without TLS | HTTP/1.1 only; an HTTP/2 prior-knowledge client is not served as HTTP/2 |
| `ListenTLS`, `Serve` with TLS | HTTP/1.1 and HTTP/2, chosen by ALPN. A `TLSConfig.NextProtos` without `h2` turns HTTP/2 off |
| `H2C: true` | HTTP/1.1 and unencrypted HTTP/2 with prior knowledge on the same port. An `Upgrade: h2c` request is answered as HTTP/1.1 |

A handler sees `r.Proto == "HTTP/2.0"` for an HTTP/2 client. WebSocket needs HTTP/1.1: `wss://` clients negotiate it by ALPN, so `EnableWS` works on a TLS server, and an upgrade sent over HTTP/2 is refused. Use `H2C` only on a hop you trust, such as behind a TLS-terminating proxy: never expose an h2c port to the internet.

### Graceful shutdown and Stop

A serving call shuts down on `SIGINT`, `SIGTERM` or `app.Stop()`, and logs `ShutdownSignal` (with `signal`) or `Shutdown`:

1. The app stops intercepting `SIGINT` and `SIGTERM`: a second signal ends the process the default way, even during the drain or a hook.
2. The listener closes (new connections are refused) and idle keep-alive connections close at once.
3. Running requests finish normally, for at most `ShutdownTimeout`. When it passes, their connections are closed (each request's `Context()` is cancelled).
4. The modules' `OnShutdown` hooks run, after the last request returned, once each, in the reverse of the initialization order (root module first, see [Hooks](#hooks)). A hook that panics is logged (`OnShutdownPanic` with `module` and `panic`) and the other hooks still run.
5. The access log stops, a pending count of suppressed panic stacks is logged, and the log's stdout pipeline is flushed for at most 2 seconds: the lines of the last requests and of the hooks are written when the serving call returns. Lines logged after that are synchronous.
6. The serving call returns `nil`, `core: shutdown timed out after <timeout>` when step 3 timed out, or `core: serve: <err>` when the server stopped by itself (the listener failed; the hooks run in that case too).

After that the app is stopped: a new serving call returns `core: app is stopped`, while `ServeHTTP` still answers.

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
| Request body | 10 MiB | `MaxRequestBodyBytes` |

A handler that needs longer (a large upload or download, server-sent events) extends its own deadline:

```go
func (instance FileController) READ_export(c *ctx.HTTPContext) {
	_ = http.NewResponseController(c.ResponseWriter).SetWriteDeadline(time.Time{})
}
```

A panic in a handler answers the fixed 500 and the server keeps serving.

### Deploying

- Run behind a TLS-terminating proxy with `Listen` (add `H2C: true` when the proxy speaks h2c), or serve HTTPS yourself with `ListenTLS`. For certificate rotation without a restart, use `TLSConfig.GetCertificate`; `autocert.Manager.TLSConfig()` fits `TLSConfig`.
- Set the proxy's idle timeout below 75 s.
- On Kubernetes, add a `preStop` sleep so the endpoint is removed before `SIGTERM` arrives.

### Advanced: your own server

`App` is an `http.Handler`, so you can run it in a server you configure yourself: other timeouts, several listeners, HTTP/3, or mounting under another mux.

```go
server := &http.Server{Addr: ":8080", Handler: app, ReadHeaderTimeout: 10 * time.Second}
```

Caveats:

- No listener, timeouts or signal handling come from Ginject: set them on your server.
- The body cap is 10 MiB.
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
- Middleware is an HTTP concept: a WebSocket connection runs the middlewares given to `EnableWS` once, at the handshake, and no middleware per message.

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
- A request to the WebSocket path runs only the middlewares given to `EnableWS`; a middleware that does not call `next()`, or panics, refuses the handshake. None of `BindGlobalMiddlewares` runs.
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

The HTTP layer and the built-in exception filter write the same response, with no mode that returns more detail:

- Status: the exception's code. A code outside 400-599 answers `500` (the body's `code` too); `error` and `message` are kept.
- `Content-Type: application/json`; the headers `Content-Length`, `Content-Encoding`, `ETag` and `Last-Modified` set earlier are removed, the others (`X-Request-Id`, CORS headers) stay.
- Body: exactly `{"code":<int>,"error":<string>,"message":<string>}` (`Exception.AppendJSON`).
- An exception is an `exception.Exception`, a non-nil `*exception.Exception`, or an `error` that wraps one at any depth. An empty `Error()` becomes the status text of the code (`Internal Server Error` when it has none); an empty message becomes `Unhandled exception has occurred`.
- Anything else answers `{"code":500,"error":"Internal Server Error","message":"Internal Server Error"}`. The framework logs `Logger.From(c.Context()).Error("UnhandledPanic", "method", …, "path", …, "panic", <the value>, "stack", <at most 4 KiB>)`: the line starts with the scope's fields (`trace_id`, `request`, and `upstream` when the request carried one) and `stack` may be left out (see [Panic stacks](#panic-stacks)), and exception filters receive `exception.InternalServerErrorException("Internal Server Error")` instead of the value.
- Once a status or body bytes were sent, no filter runs and nothing more is written: the framework logs `Logger.From(c.Context()).Error("ErrorAfterResponse", "method", …, "path", …, "code", <status sent>, "panic", <value or the exception's message>, "stack", …)`, sampled like `UnhandledPanic`.
- `panic(http.ErrAbortHandler)` is re-panicked so `net/http` aborts the connection; nothing is written or logged.

`exception.X("message", err)` puts `err.Error()` in the `error` field: pass a cause only when its text is safe to show.

### Error response rules

From the tests (`http_error_test.go`):

- A middleware's panic and the built-in filter give the same status, body and `Content-Type` for every value.
- A string, an `error`, a runtime error, `panic(nil)`, a nil `*exception.Exception` or any other value answers the fixed 500 body; nothing of the value is in the response. It is logged once as `UnhandledPanic`; an exception is not.
- A filter that prints `GetMessage()` of a non-exception panic prints `Internal Server Error`; a filter's own non-exception panic reaches the next filter as the same fixed exception, and is logged.
- After `c.Text`, `c.JSON`, `c.Redirect`, `w.Write`, `w.WriteHeader` with a code of 200 or more, or `http.ServeFile`, a later panic changes neither status nor body, runs no filter and logs one `ErrorAfterResponse` line. An informational status (`103`) does not count as a response.
- Codes `0`, `7`, `200`, `399`, `600`, `999`, `1008`, `4001` and `-1` answer `500`; `400` and `599` are kept. `WriteHeader` is never called with an invalid code.
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
- With `EnableVersioning`, the route registered for (method, path, version) answers; without one, the route registered with `VERSION_NEUTRAL`. A neutral route answers requests with or without a version. A request without a version to a route registered without one runs the global middlewares, then answers `410` `{"code":410,"error":"Gone","message":"Deprecated URL usage"}`. The `Allow` of a 405 lists the methods of the request's version and the neutral routes.

## Static files

A `SERVE_…` handler returns the path to serve. `SERVE_static_ANY` and `SERVE_ANY_OF_static` are the same route, `/static/*`: with a handler that returns `"public"`, `GET /static/a.txt` serves `public/a.txt`, `GET /static/sub/x.txt` serves `public/sub/x.txt`, `GET /static` is the directory itself and `GET /staticzzz` answers 404.

- A file is served with `http.ServeFile` (ranges, `If-Modified-Since`, content type by extension). Under a wildcard route (`SERVE_<x>_ANY`, `SERVE_ANY_OF_<x>`) the file is opened through `os.Root` on the served directory, with `http.ServeFileFS`.
- A directory answers its `index.html` when it exists (with the redirect to the slash-terminated URL), otherwise the 404 body. No response ever contains a directory listing.
- A resolved path outside the served directory answers `400` `{"code":400,"error":"Bad Request","message":"Invalid URL path"}`; a missing file, or a result that is not a string, answers the 404 body.
- Under a wildcard route, a path with a segment that starts with `.` (`.env`, `.git/config`, `css/.secret`) answers the 404 body, except `.well-known` (RFC 8615). A symbolic link is followed only when its target stays inside the served directory; otherwise the 404 body. Still serve a directory that holds only public files.
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

## Response writer

Every layer receives the same writer as `c.ResponseWriter` (middlewares as their `http.ResponseWriter`). It records whether a response was sent, and forwards `Flush`, `Hijack`, `ReadFrom` (so `http.ServeFile` keeps `sendfile`) and `Unwrap` (so `http.ResponseController` reaches `SetWriteDeadline` and friends). Like `net/http`'s own writer, don't use it after the request returns: a late write on an idle context is dropped.

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
| `ctx.Next` | yes | yes | `c.Next` |
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

- Exported fields of a struct pipe get providers injected at `Create()`, like a controller's: a provider of the module, else a global provider, else a global interface (such as `common.Logger`), else the zero value. An unexported field or an unresolvable provider field stops `Create()`.
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

Each app has its own `trace.Tracer`. It is internal: `EnableAccessLog()` adds the only listener, and there is no public way to add another. With that listener, each pipe argument emits a `pipe` event (name: the parameter type, transport `http` or `ws`, the time its `Transform` took), also when `Transform` panics or returns a wrong value. Each HTTP handler call emits a `handler` event (name: the method name, `SERVE_…` routes included) whose duration excludes the pipes; it is not emitted when an argument failed. WS handlers emit no `handler` event. Without a listener nothing is emitted and no clock is read.

Every event carries `ID`, `TraceID`, `SpanID`, `ParentID`, `Stage`, `Name`, `Transport` (`HTTP` or `WS`, on every stage) and `Duration`; the `complete` event also carries `Operation` (the method or the WS message type), `Target` (the URL path without the query string, or the WS topic), `Code` and, for an HTTP request or handshake, `ClientIP` (as `proxy.ClientIP` resolves it). `Enabled()` is asked once per request: a request that started without a listener emits nothing, exception filter events included.

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
- The WebSocket side (`WS`, `NewWS`, `WSConfig`, `WSConnmgr`, `NewWSConnmgr`, `WSConnection`, `WSPayload`, `WSPayloadType` and its `Type…` constants) is documented with the WebSocket review of this package. `EnableWS(&core.WSConfig{Path: "/ws"})` turns it on.

## Performance notes

- **The plan.** At `Create()` each handler gets a list of argument steps: the parameter types are compared once, pipe prototypes are built once (with their providers injected), the publisher is looked up once, and `ctx.BinderOf` is called once per struct pipe type so the first `Bind` of that type walks no types. A request runs the steps and calls the method once.
- **One reflection hop.** The plan calls the method through `reflect.Type.Method(i).Func` with the receiver as the first argument, not through a method value (which reflection would enter a second time).
- **Arguments on the stack.** Handlers with up to 8 parameters build their argument list in a local array; longer handlers allocate it.
- **Fresh pipe receivers without copying by hand.** A pipe prototype is held as an interface value; calling `Transform` on a value receiver copies it, so no per-request `reflect.New` or injection is needed.
- **Tracing is separate.** One listener check per call; the traced path has its own functions, so the untraced path has no `defer` and reads no clock.
- What remains is the cost of `reflect.Value.Call` (one allocation for the result slice, plus one per result that is not pointer-shaped) and whatever the accessors and pipes do.
- **One route struct per route.** At `Create()` each route gets an immutable `httpRoute` (module middlewares, guards and interceptors with the global ones in front, the handler plan, the filter list, the `SERVE` index), stored in a slice at the router item's index: a request does one slice index instead of three map lookups keyed by the pattern string.
- **No closure without middleware.** A route without middleware gets a shared no-op `c.Next`; with middlewares, one closure and one flag per request, passed to every `Use` (a closure kept on the pooled context would let a leaked `next` continue another request). Guards and interceptors are called directly, and a refused guard reaches the filters without a panic.
- **One listener check, one recover.** `Tracer.Enabled` (one atomic load) is asked once per request; `handleRequest` holds the request's only `defer` / `recover`, and `ServeHTTP` releases the context in straight-line code.
- **Fixed bodies.** The 404, 405, 410 and fixed 500 bodies are byte slices built once; a miss tries only the methods that have routes for the `Allow` header, which comes from a table built once.
- **One atomic load for the body cap.** The cap is an `atomic.Int64` written when a serving call starts and loaded only for a request with a body (about 1 ns, no allocation). The rest of the server code runs once per start or stop.
- **No `MaxBytesReader` for a bodiless request**, and the router appends parameter values to the pooled context's `ParamValues`, whose capacity `Reset` keeps.
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
| BenchmarkCreate_DynamicSubmodules (cold) | 62788 | 6720 | 66 |

The trace scope (design 0011, same machine, `benchstat` against the code before it, 6 runs): Static 615 → 884 ns (96 → 624 B, 6 → 7 allocations), NotFound 485 → 745 ns (48 → 576 B, 3 → 4), MethodNotAllowed 433 → 699 ns (64 → 592 B, 4 → 5).

Against the previous dispatch (same machine, `benchstat`, 5 runs): Static 837 → 636 ns (9 → 6 allocations), Chain 1,277 → 806 ns (13 → 10), NotFound 2,387 → 523 ns (23 → 3), MethodNotAllowed 2,285 → 464 ns (23 → 4), Exception 1,913 → 1,570 ns (10 → 6), MultiInterceptor 1,827 → 972 ns (15 → 9).

*Note: These benchmarks are machine-dependent and were captured at documentation generation time (November 16, 2026).*
