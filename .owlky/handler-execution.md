# Handler Execution & Dependency Resolution

**Optimization**: Pseudo-code, decision trees, state machines. Focus on exact execution order and invariants.

## 1. Handler Plans (design 0006)

Every handler gets an argument plan once, at `Create()`; a request only runs it. Code: `internal/core/fn_plan.go`
(`newHTTPHandlerPlan`, `newWSHandlerPlan`, `invoke`), call sites in `internal/core/container.go`
(`bindHTTPController` / `bindWSController` build the plans into the app's
`container.httpPlanByPattern` / `wsPlanByEvent`); `internal/core/app.go` `initRoutes` stores each HTTP plan in its route (`httpRoute.plan`,
called with `call(c, ev, traced)`), and the WS read loop looks the plan up through `resolveAndCallHandler`.

### 1.1 Building a plan (cold, `Create()`)

```
newHTTPHandlerPlan(recv, method, injectedProviders):
    funcType = recv.Method(method.Index).Type().String()   // without receiver, for messages
    for i, t in method.Type.In(1..):
        variadic last parameter            → panic unresolved(funcType, i)
        t in httpArgKindByType             → step {kind}          // type identity, not names
        t == common.Publisher              → step {fixed: publisher stored now}  // none stored → panic unresolved
        t is an interface                  → panic unresolved
        t implements an HTTP pipe interface (Context, Body, Form, Query, Header, Param, File, in that order):
            t is a pointer                 → panic "pipe '<t>' of the '<funcType>' must not be a pointer"
            ctx.BinderOf(t) if struct (cache warm-up, result ignored)
            prototype = injectDependencies(zero t, "pipe", injectedProviders)   // struct; zero value otherwise
            step {pipe: Transform bound to the prototype, meta {ContextType: "http", ParamType: key}}
        otherwise                          → panic unresolved
```

`unresolved` is `"dependency injection: can't resolve dependencies of the '<funcType>'. Please make
sure that the argument dependency at index [<i>] is available in the handler"` in `color.FmtRed`.
Building calls neither the handler nor any `Transform`; the plan is immutable afterwards.

| Parameter type | HTTP | WS | Argument |
|---|---|---|---|
| `*ctx.HTTPContext` | yes | no | `c` |
| `*http.Request` | yes | no | `c.Request` |
| `http.ResponseWriter` | yes | no | `c.ResponseWriter` (nil → typed nil) |
| `ctx.Body`, `Form`, `Query`, `Header`, `Param`, `File` | yes | no | the accessor |
| `ctx.Redirect` | yes | no | `c.Redirect` |
| `*ctx.WSContext` | no | yes | `c` |
| `*websocket.Conn` (`golang.org/x/net/websocket`) | no | yes | `c.Conn` |
| `ctx.WSPayload` | no | yes | `c.WSPayload()` |
| `ctx.Next` | yes | yes | the middlewares' `next` (`ctx.HTTPNext(c)` / `ctx.WSNext(c)`) |
| `common.Publisher` | yes | yes | stored value, fixed at build |
| value type implementing an HTTP `*Pipeable` | yes | no | `Transform` result |
| value type implementing `common.WSPayloadPipeable` | no | yes | `Transform` result |

### 1.2 Invoking (hot, every request or message)

```
invoke(c, ev):
    in = local [9]reflect.Value (heap slice above 8 parameters); in[0] = receiver
    if ev has trace listeners → invokeTraced
    for each step: in[i+1] = step(c)                 // switch on kind, or the bound Transform
    return method.Func.Call(in)                       // one reflection hop
```

- **Order:** arguments left to right, all before the handler. A panic while resolving an argument
  (an accessor's exception such as 413, a `Transform` panic) propagates unchanged; later arguments
  are not resolved and the handler is not called.
- **Pipe receivers:** the prototype is held as an interface value; a value-receiver `Transform`
  gets its own copy, so changes to the receiver never leak into the next or a concurrent request.
- **Wrong pipe result:** `nil` or a value not assignable to the parameter type panics
  `exception.InternalServerErrorException("pipe <T>: Transform returned <dynamic type|nil>, want <T>")`.
- **Tracing (HTTP):** one `pipe` event per pipe argument (also when it panics or is refused) and a
  `handler` event (method name, duration minus the pipes) after the handler returns or panics, not
  when an argument failed. **WS:** `pipe` events only.

### 1.3 Concurrency

Plans are built during `Create()` on the calling goroutine, in the app's own container, and are
read-only afterwards; requests share them without a lock. The argument array lives on the request's stack.

---

## 2. WebSocket Handlers

Same plan with the WS rows of the table; `ArgumentMetadata.ContextType` is `"ws"`. The read loop
calls `ws.resolveAndCallHandler(pattern, item.Handler, c)`; the pattern is the matched event and
selects the plan. The handler's last result is the reply message.

---

## 3. Provider Instantiation

Providers are constructed at `Create()`, never per request or per injection (`provide` in
`internal/core/container.go`, explained in [architecture-core-concepts.md](architecture-core-concepts.md) § 4.1):

- each provider type once per app, after the providers its exported fields name
- the value `NewProvider()` returns is injected, by value, into every field of its type (controllers,
  other providers, layers, pipe prototypes)
- shared state belongs behind a pointer; open resources in `NewProvider()` and close them in the
  module's `OnShutdown`

```go
type EmailService struct {
	conn *smtp.Client
}

func (instance EmailService) NewProvider() core.Provider {
	instance.conn, _ = smtp.Dial("localhost:25")
	return instance
}
```

---

## 4. Error Handling in Handler Invocation

### 4.1 Panic Recovery

**Point of Recovery**: `HTTP.handleRequest()` in `internal/core/http.go` defers `recoverRequest`, the
request's only `recover()`: a middleware's panic gets the HTTP layer's error response, a later one
runs the route's exception filters (`HTTP.catch`). `invoke` never recovers; on WS,
`dispatchWSEvent` in `internal/core/read_loop.go` recovers into `runCatchChain`.

### 4.2 No Per-Request Timeout Check

There is no deadline check before handler invocation — `ctx.HTTPContext.IsDeadlineExceeded()` does not exist (verified absent from `internal/ctx/http_context.go`), and no code in `internal/core/fn_plan.go`'s handler-invocation path references it or `exception.RequestTimeoutException` at all. A handler runs to completion regardless of how long it takes; only the underlying `http.Server`'s connection-level timeouts (`ReadTimeout`/`WriteTimeout`, set once by the serving call, `internal/core/server.go`) provide any time bound, and those act on the connection, not the handler goroutine.

### 4.3 Common Handler Errors

| Error | Cause | Recovery |
|-------|-------|----------|
| `dependency injection: can't resolve` | Unknown parameter type | Panic at `Create()` (never per request) |
| `pipe <T>: Transform returned …, want <T>` | `Transform` returned nil or another type | 500 exception → exception filter |
| `panic(exception.BadRequestException(...))` | Validation failure | Caught by exception filter |
| `return nil` from handler | No return value | OK, handler can return any type |
| `dependency injection: can't set value to unexported '<field>' field of the <pkg>.<Pipe> pipe` | A pipe (form DTO) with an unexported field | Panic at `Create()`: pipe fields are injected like a controller's, so keep them exported |
| `invalid handler: <Type>.SUBMIT_x needs <Type>.RENDER_x: a SUBMIT_ posts to the page that shows its form` | A `SUBMIT_` without the `RENDER_` of the same words in its controller | Panic at `Create()` |
| `invalid render: <Type>.<Method> needs a template: <a> or <b>` | A `RENDER_` / `SUBMIT_` whose path has no template file | Panic at `Create()` |

---

## 5. Return Value Handling

### 5.1 HTTP results

A handler returns nothing, one value, or `(status, value)`. With two or more results,
`setStatusCode` applies the first one when it is an `int` (or an `any` holding one) with a known
status text; the second is written; a third is ignored. `returnHTTP(c, v)` by kind:

| Kind | Response |
|---|---|
| `Map`, `Slice`, `Struct`, `Array`, `Pointer` | `c.JSON(v)` (pointee JSON; nil pointer → `null`) |
| `Interface` | holding a string → `c.Text`; otherwise `c.JSON` (numbers and bools stay `application/json`; nil → `null`) |
| `String` | `c.Text` |
| bool, integers, floats (`'g'`), complex | `c.Text` with `strconv` formatting |
| `Func` | `c.Text(type string)` |
| `Chan`, `Uintptr`, `UnsafePointer`, invalid `Value` | nothing |

Interceptors call `returnHTTP(c, reflect.ValueOf(data))`, so their data is written by its dynamic
kind. `SERVE_…` routes serve the returned path instead.

Page routes (`RENDER_`, `SUBMIT_`; design 0018 amendment 2) never call `returnHTTP`: `writeResult`
sends the result to `writePage` (`internal/core/http_page.go`), which renders the template bound to
the route at `Create()` with the only or last result as its data (any type; `nil` when the handler
has none; an interceptor's data or a post-interceptor's replacement is the data), and ignores it
when a response was already written. `(int, data)` sets the status like any two-result handler; the
default is 200 for `RENDER_` and 422 for `SUBMIT_`, and a redirect without a 3xx status answers 303.
The result type is never checked at `Create()`: a template that fails on it (a missing field) is a
render failure at run time, fixed 500 HTML and `RenderFailed`.

### 5.2 WebSocket Handlers

The last result is sent as the `event` reply's `Message` (JSON-encoded by the connection).

---

## 6. Dependency Resolution Performance

Numbers: `internal/core/README.md` › Benchmarks and design 0006 › Performance. A call costs one
`reflect.Value.Call` (about 200 ns and one allocation for the result slice, plus one per result that
is not pointer-shaped) plus whatever the accessors and pipes do. The plan removed per-request type
walking, key building, map lookups, argument-list construction and pipe prototype rebuilding.
A typed call without reflection needs `unsafe` or an opt-in method-value API (design 0006 Q1): not
approved.

---

## 7. Panic Recovery Points

### 7.1 Where Panics Are Recovered

All in `HTTP.handleRequest()` (`recoverRequest`), the request's only `recover()`; the spec is
[request-pipeline.md](request-pipeline.md) § 1.

| Layer | Goes to |
|-------|---------|
| Global or module middleware | The HTTP layer's fixed error response (no exception filter) |
| Guard, interceptor (`Intercept`, operators), pipe, argument binding, handler | Module filters (bound last first), global filters, the built-in filter |
| Exception filter | The next filter; if every filter panics, the error response for the last panic |
| Any, after a response was sent | Nothing written; `ErrorAfterResponse` logged |

### 7.2 Panic Propagation Rules

- Only exceptions reach the client with their code and message; any other value answers a fixed 500 and is logged as `UnhandledPanic`
- `panic(http.ErrAbortHandler)` is re-panicked so `net/http` aborts the connection

---

## 8. Known Limitations & Gotchas

### 8.1 Parameter Type Matching

**GOTCHA**: Providers are injected into controller (and pipe) **fields**, not handler parameters.
A provider type as a handler parameter is an unknown type and panics at `Create()`. Field types
must match the provider type exactly:

```go
// Provider registered as
type UserService struct { ... }
func (u UserService) NewProvider() Provider { return u }

// WRONG: a provider as a handler parameter
func (c UserController) READ(us UserService) {}
// Result at Create(): "dependency injection: can't resolve dependencies of the 'func(main.UserService)' ..."

// WRONG: field type differs from the provider type
type UserController struct {
    owlky.HTTP
    UserService *UserService // provider is UserService: unresolved
}

// RIGHT: same type, as a field
type UserController struct {
    owlky.HTTP
    UserService UserService
}
```

### 8.2 Circular Dependencies

Value providers can't form a cycle: a struct that contains itself through another struct is an
invalid recursive type and doesn't compile.

### 8.3 Nil From NewProvider

A provider whose `NewProvider()` returns `nil` is stored as `nil`; `injectDependencies`
(`internal/core/fn.go`) skips nil entries, so a field of that type is unresolved and `Create()` panics
`dependency injection: can't resolve dependency …` (read from the code, no test pins it).
