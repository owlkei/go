# Core

*Package `core` is the dependency injection container and the server of `Ginject`: it builds the module tree, injects providers into controllers, plans every handler's arguments once at `Create()` and serves HTTP and WebSocket requests through the middleware, guard, interceptor, handler and exception filter pipeline.*

- [Core](#core)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`App`](#app)
    - [New](#new)
    - [Create](#create)
    - [Listen and Stop](#listen-and-stop)
    - [ServeHTTP](#servehttp)
    - [Get](#get)
    - [Configuration methods](#configuration-methods)
    - [Constants](#constants)
  - [Request lifecycle](#request-lifecycle)
    - [Lifecycle rules](#lifecycle-rules)
  - [Errors in middleware](#errors-in-middleware)
  - [Error response](#error-response)
    - [Error response rules](#error-response-rules)
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
	if err := app.Listen(3000); err != nil {
		panic(err)
	}
}
```

`GET /users/7?page=2` answers `{"greeting":"hello 7","page":2}`.

## `App`

`App` holds the module tree, the HTTP router, the optional WebSocket server and the trace event bus. Configure it with the methods below, wire it once with `Create`, then serve with `Listen` (or mount it as an `http.Handler`).

### New

Returns an `App` with the default request body limit (`DefaultMaxRequestBodyBytes`), the built-in exception filters and a `common.Publisher` backed by an in-memory broker.

#### Returns
- 1st value: `*App`

### Create

Builds the module tree from the root module, creates the providers and controllers, plans every handler, and binds middlewares, guards, interceptors and exception filters. It panics (`"dependency injection: ..."`, `"route conflict: ..."`, …) when the application can't be built. Call it once.

#### Parameters
- 1st parameter: `*Module`

- Description: The root module, built with `ModuleBuilder()`.

#### Usage

```go
app.Create(core.ModuleBuilder().Controllers(UserController{}).Build())
```

### Listen and Stop

`Listen(port int) error` logs the modules, routes and WebSocket events, then serves on `:<port>` with the default server timeouts (see [Constants](#constants)). On `SIGINT` or `SIGTERM` it runs the modules' `OnShutdown` hooks and shuts the server down gracefully within 30 seconds. `Stop()` runs the `OnShutdown` hooks once.

### ServeHTTP

`ServeHTTP(w http.ResponseWriter, r *http.Request)` serves one request: it caps a request body at `DefaultMaxRequestBodyBytes` (a request without a body, `nil` or `http.NoBody`, is not wrapped), takes a pooled context, sets the `X-Request-Id` header, runs the [request lifecycle](#request-lifecycle) and, with a trace listener, emits the `complete` event, whose `Code` is the status actually written (`200` when nothing was written). Requests to the WebSocket path are upgraded when `EnableWS` was called.

### Get

`Get(p Provider) any` returns the created instance of a provider type registered in the module tree, or `nil`.

```go
service := app.Get(GreetingService{}).(GreetingService)
```

### Configuration methods

Each returns the `App` so calls can be chained. Call them before `Create`.

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
| `UseLogger(common.Logger)` | A custom logger |
| `UseLogOptions(*log.LogOptions)` | Options of the built-in logger |

### Constants

| Constant | Value |
|---|---|
| `DefaultMaxRequestBodyBytes` | `10 << 20` (10 MB) |
| `DefaultMaxHeaderBytes` | `1 << 20` (1 MB) |
| `DefaultReadHeaderTimeout` | `2 * time.Second` |
| `DefaultReadTimeout` | `5 * time.Second` |
| `DefaultWriteTimeout` | `15 * time.Second` |
| `DefaultIdleTimeout` | `60 * time.Second` |

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
- The chain is linear. `next()` only says "continue": the next layer starts after `Use` returns. Work that must happen after the handler belongs in an interceptor's `Pipe` operators.
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

A middleware's error never reaches your exception filters: the HTTP layer answers it. If the middleware panics with an exception (`panic(exception.UnauthorizedException("token expired"))`), the client gets that exception's code and message. Anything else (a string, an `error`, a runtime error) answers a fixed `500` and the value goes to the logger, with the request ID and the stack.

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
- Anything else answers `{"code":500,"error":"Internal Server Error","message":"Internal Server Error"}`. The framework logs `Logger.Error("UnhandledPanic", "id", <request ID>, "method", …, "path", …, "panic", <the value>, "stack", <at most 4 KiB>)`, and exception filters receive `exception.InternalServerErrorException("Internal Server Error")` instead of the value.
- Once a status or body bytes were sent, no filter runs and nothing more is written: the framework logs `Logger.Error("ErrorAfterResponse", "id", …, "method", …, "path", …, "code", <status sent>, "panic", <value or the exception's message>, "stack", …)`.
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

## No route, HEAD and versioning

- No route for the path under any method: `404`, body `{"code":404,"error":"Not Found","message":"Not Found"}`, `Content-Type: application/json`, `X-Request-Id` set. The body never depends on the request: a 100,000-character path gets the same bytes.
- A route for the path under another method: `405`, body `{"code":405,"error":"Method Not Allowed","message":"Method Not Allowed"}` and an `Allow` header listing the methods that have a route, in the order `GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS` (`HEAD` exactly when `GET` is). Any request method gets it, `OPTIONS` and unknown methods included.
- `HEAD` is served by the `GET` route of the path, through the whole lifecycle; `c.Method` stays `HEAD`. Over a real server `net/http` sends no body.
- A path longer than 4,096 bytes or with more than 64 segments matches nothing (404).
- With `EnableVersioning`, the route registered for (method, path, version) answers; without one, the route registered with `VERSION_NEUTRAL`. A neutral route answers requests with or without a version. A request without a version to a route registered without one runs the global middlewares, then answers `410` `{"code":410,"error":"Gone","message":"Deprecated URL usage"}`. The `Allow` of a 405 lists the methods of the request's version and the neutral routes.

## Static files

A `SERVE_…` handler returns the path to serve:

- A file is served with `http.ServeFile`.
- A directory answers its `index.html` when it exists (with `http.ServeFile`'s redirect to the slash-terminated URL), otherwise the 404 body. No response ever contains a directory listing.
- A resolved path outside the served directory answers `400` `{"code":400,"error":"Bad Request","message":"Invalid URL path"}`; a missing file, or a result that is not a string, answers the 404 body.
- Dot files inside the served directory are served (`.well-known` must work): serve a directory that holds only public files.

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

With a listener on `trace.EventName`, each pipe argument emits a `pipe` event (name: the parameter type, transport `http` or `ws`, the time its `Transform` took), also when `Transform` panics or returns a wrong value. Each HTTP handler call emits a `handler` event (name: the method name, `SERVE_…` routes included) whose duration excludes the pipes; it is not emitted when an argument failed. WS handlers emit no `handler` event. Without a listener nothing is emitted and no clock is read.

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

`Module` (built by `ModuleBuilder()`; `Prefix`, `ID`, `NewModule`, the `IsGlobal`, `OnInit`, `OnReady` and `OnShutdown` fields and the layer lists), `Provider`, `Controller`, `HTTP`, `WS`, `WSConfig`, `NewWS`, `WSConnmgr`, `WSConnection`, `WSPayload`, `WSPayloadType` and the WebSocket layer types are documented with the rest of the server in a later revision of this README.

## Performance notes

- **The plan.** At `Create()` each handler gets a list of argument steps: the parameter types are compared once, pipe prototypes are built once (with their providers injected), the publisher is looked up once, and `ctx.BinderOf` is called once per struct pipe type so the first `Bind` of that type walks no types. A request runs the steps and calls the method once.
- **One reflection hop.** The plan calls the method through `reflect.Type.Method(i).Func` with the receiver as the first argument, not through a method value (which reflection would enter a second time).
- **Arguments on the stack.** Handlers with up to 8 parameters build their argument list in a local array; longer handlers allocate it.
- **Fresh pipe receivers without copying by hand.** A pipe prototype is held as an interface value; calling `Transform` on a value receiver copies it, so no per-request `reflect.New` or injection is needed.
- **Tracing is separate.** One listener check per call; the traced path has its own functions, so the untraced path has no `defer` and reads no clock.
- What remains is the cost of `reflect.Value.Call` (one allocation for the result slice, plus one per result that is not pointer-shaped) and whatever the accessors and pipes do.
- **One route struct per route.** At `Create()` each route gets an immutable `httpRoute` (module middlewares, guards and interceptors with the global ones in front, the handler plan, the filter list, the `SERVE` index), stored in a slice at the router item's index: a request does one slice index instead of three map lookups keyed by the pattern string.
- **No closure without middleware.** A route without middleware gets a shared no-op `c.Next`; with middlewares, one closure and one flag per request, passed to every `Use` (a closure kept on the pooled context would let a leaked `next` continue another request). Guards and interceptors are called directly, and a refused guard reaches the filters without a panic.
- **One listener check, one recover.** `HasListeners` is asked once per request; `handleRequest` holds the request's only `defer` / `recover`, and `ServeHTTP` releases the context in straight-line code.
- **Fixed bodies.** The 404, 405, 410 and fixed 500 bodies are byte slices built once; a miss tries only the methods that have routes for the `Allow` header, which comes from a table built once.
- **No `MaxBytesReader` for a bodiless request**, and the router appends parameter values to the pooled context's `ParamValues`, whose capacity `Reset` keeps.
- **Tracking writer in the pooled exchange.** The writer lives next to the context in the pool, so it costs no allocation.
- **The cost of an unexpected panic.** A non-exception panic captures up to 4 KiB of stack for its log line: about 25 µs and 7.6 KB per request, against about 1.6 µs for an exception.

## Benchmarks

Benchmark results on darwin/amd64 (Intel Core i7-9750H @ 2.60GHz, Go 1.27.4), `go test -run '^$' -bench . -benchmem ./core/`, median of 5 runs. A plan call is measured on a prepared context without a trace listener unless the name says `Traced`.

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| BenchmarkServeHTTP_Static (`GET`, a string result) | 636 | 96 | 6 |
| BenchmarkServeHTTP_Chain (a middleware, a guard, an interceptor) | 806 | 440 | 10 |
| BenchmarkServeHTTP_MultiInterceptor (three interceptors) | 972 | 672 | 9 |
| BenchmarkServeHTTP_NotFound | 523 | 48 | 3 |
| BenchmarkServeHTTP_MethodNotAllowed | 464 | 64 | 4 |
| BenchmarkServeHTTP_Exception (a handler panics an exception) | 1570 | 256 | 6 |
| BenchmarkServeHTTP_PanicString (a handler panics a string: stack capture for the log) | 26977 | 7668 | 12 |
| BenchmarkHTTPHandlerPlan_Invoke_NoArgs (`func() string`) | 210 | 40 | 2 |
| BenchmarkHTTPHandlerPlan_Invoke_Param (`func(ctx.Param) ctx.Map`) | 206 | 24 | 1 |
| BenchmarkHTTPHandlerPlan_Invoke_QueryPipe (two-field DTO) | 265 | 48 | 2 |
| BenchmarkHTTPHandlerPlan_Invoke_BodyPipeWithProvider | 278 | 56 | 2 |
| BenchmarkHTTPHandlerPlan_Invoke_FiveBuiltins | 575 | 16 | 1 |
| BenchmarkHTTPHandlerPlan_Invoke_QueryPipe_Traced | 915 | 304 | 6 |
| BenchmarkNewHTTPHandlerPlan (cold, `Create()`) | 1086 | 352 | 10 |
| BenchmarkReturnHTTP_String | 71 | 46 | 1 |
| BenchmarkReturnHTTP_Int | 89 | 43 | 2 |
| BenchmarkReturnHTTP_Float | 128 | 38 | 2 |
| BenchmarkReturnHTTP_Map | 1011 | 219 | 10 |
| BenchmarkReturnHTTP_PointerToStruct | 273 | 62 | 2 |
| BenchmarkReturnHTTP_InterfaceString | 60 | 43 | 1 |
| BenchmarkTraceHTTPCatch_NoListener | 22 | 0 | 0 |

Against the previous dispatch (same machine, `benchstat`, 5 runs): Static 837 → 636 ns (9 → 6 allocations), Chain 1,277 → 806 ns (13 → 10), NotFound 2,387 → 523 ns (23 → 3), MethodNotAllowed 2,285 → 464 ns (23 → 4), Exception 1,913 → 1,570 ns (10 → 6), MultiInterceptor 1,827 → 972 ns (15 → 9).

*Note: These benchmarks are machine-dependent and were captured at documentation generation time (November 16, 2026).*
