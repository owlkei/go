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

`ServeHTTP(w http.ResponseWriter, r *http.Request)` serves one request: it caps the body, takes a pooled context, sets the `X-Request-ID` header, runs the pipeline and, with a trace listener, emits the `complete` event. Requests to the WebSocket path are upgraded when `EnableWS` was called.

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

## Benchmarks

Benchmark results on darwin/amd64 (Intel Core i7-9750H @ 2.60GHz, Go 1.27.4), `go test -run '^$' -bench . -benchmem ./core/`, median of 5 runs. A plan call is measured on a prepared context without a trace listener unless the name says `Traced`.

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| BenchmarkHTTPHandlerPlan_Invoke_NoArgs (`func() string`) | 276 | 40 | 2 |
| BenchmarkHTTPHandlerPlan_Invoke_Param (`func(ctx.Param) ctx.Map`) | 269 | 24 | 1 |
| BenchmarkHTTPHandlerPlan_Invoke_QueryPipe (two-field DTO) | 350 | 48 | 2 |
| BenchmarkHTTPHandlerPlan_Invoke_BodyPipeWithProvider | 371 | 56 | 2 |
| BenchmarkHTTPHandlerPlan_Invoke_FiveBuiltins | 738 | 16 | 1 |
| BenchmarkHTTPHandlerPlan_Invoke_QueryPipe_Traced | 1069 | 304 | 6 |
| BenchmarkWSHandlerPlan_Invoke_Payload | 222 | 0 | 0 |
| BenchmarkWSHandlerPlan_Invoke_PayloadPipe | 248 | 0 | 0 |
| BenchmarkNewHTTPHandlerPlan (cold, `Create()`) | 1351 | 352 | 10 |
| BenchmarkReturnHTTP_String | 104 | 56 | 1 |
| BenchmarkReturnHTTP_Int | 102 | 36 | 2 |
| BenchmarkReturnHTTP_Float | 161 | 42 | 2 |
| BenchmarkReturnHTTP_Map | 1246 | 219 | 10 |
| BenchmarkReturnHTTP_PointerToStruct | 346 | 70 | 2 |
| BenchmarkReturnHTTP_InterfaceString | 77 | 50 | 1 |
| BenchmarkTraceHTTPHandler_NoListener | 27 | 0 | 0 |
| BenchmarkTraceHTTPHandler_WithListener | 321 | 128 | 2 |
| BenchmarkBuildUseMiddleware_NoListener | 35 | 0 | 0 |
| BenchmarkHandleRequest_MatchedRoute | 3817 | 6365 | 23 |

*Note: These benchmarks are machine-dependent and were captured at documentation generation time (November 16, 2026).*
