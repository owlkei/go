# Ginject Core Concepts & Architecture

**Optimization**: This document is structured for AI parsing — explicit rules, decision trees, state machines. Minimal prose.

## 1. Fundamental Design Patterns

### 1.1 Module → Controller → Provider (DI Triangle)

**Structure**:
```
Module (composition root)
├── Controllers (handler containers, embedded REST/WS)
│   └── Requests routed to handler methods
├── Providers (domain logic, injected into controllers)
└── Imports (nested modules, recursive composition)
```

**Invariants**:
- Every Module is wired once per app at `app.Create()` (`(*container).wire`); a module value can be given to any number of apps
- Controllers are constructed once per app at `Create()` (fields injected, then `NewController`); handler methods run on that value
- Providers are constructed once per app (fields injected, then `NewProvider`), whatever the number of modules that list them
- Module tree is immutable after `app.Create()`

**Dependency Resolution** (all at `Create()`):
1. Provider and controller fields: matched by type identity (package path + type) against the providers visible to the module
2. Handler parameters: matched against the built-in types and pipes (section 1.4); providers are not handler parameters
3. Anything unresolvable panics `dependency injection: …` at `Create()`

**Cost**: reflection runs once per handler at `Create()`; a request runs the prepared plan (`core/README.md` › Benchmarks)

---

### 1.2 Request Pipeline (HTTP)

The spec, arrow by arrow, is [request-pipeline.md](request-pipeline.md) § 1. In short:

```
global middleware → module middleware → global guard → module guard →
global pre-interceptor → module pre-interceptor → main handler →
module post-interceptor → global post-interceptor → response

error in a middleware      → the HTTP layer's fixed error response (no exception filter)
error from a guard onwards → module exception filter → global exception filter → built-in filter
```

**Key Rules**:
- A layer stops the request by not continuing: a middleware without `next()`, a guard returning `false` (the filters get a 403), an interceptor that returns without `Pipe`
- The chain is linear: code after `next()` runs before the next layer starts, not after the handler
- `HTTP.handleRequest()` (`core/http.go`) holds the request's only `recover()`
- Only exceptions reach the client with their code and message; any other panic answers a fixed 500 and is logged

---

### 1.3 WebSocket Event Dispatch

**Connection Lifecycle**:
```
HTTP upgrade request
        ↓
[Handshake middlewares run]
        ↓
websocket.Conn accepted + registered
        ↓
readLoop() spawned (blocks on conn.Receive())
writeLoop() spawned (drains send channel)
        ↓
Client sends JSON: { type: "publish", topic: "users.created", message: {...} }
        ↓
[Topic pattern matching: exact → suffix-wildcard → complex regex]
        ↓
[Handler lookup]
        ↓
[guard → interceptor → handler; middlewares ran once, at the handshake]
        ↓
[Handler execution → memorybroker.Publish(topic)]
        ↓
[Fanout: all subscribers get conn.TrySend() — non-blocking]
        ↓
[writeLoop() sends to wire when ready]
        ↓
Connection closes → readLoop exits → writeLoop exits
```

**Key Rules**:
- WebSocket is NOT streaming; it's RPC-over-WS with pub/sub
- Each message is a complete request/response cycle
- Fanout is fire-and-forget (callbacks are non-blocking)
- Send channel buffer is fixed (32 messages) — overflow silently drops
- No message ordering guarantees across connections

---

### 1.4 Dependency Injection via Reflection

**Resolution Algorithm** (`newHTTPHandlerPlan` / `newWSHandlerPlan` in `core/fn_plan.go`, once per handler at `Create()`):
```
For each parameter of handler(Param1, …, ParamN):
    if its type is a built-in type of the transport: a step that reads it from the context
    else if it is a value type with the transport's Transform method: a pipe step
        (the prototype gets providers injected into its exported fields once)
    else: panic "dependency injection: can't resolve dependencies of the '<func type>' …"
A request runs the steps left to right, then calls the method once.
```

**Built-in HTTP Dependencies**:
- `*ctx.HTTPContext` — full request context
- `*http.Request` — raw request object
- `http.ResponseWriter` — raw response writer
- `ctx.Body` — parsed request body
- `ctx.Query` — URL query parameters
- `ctx.Header` — HTTP headers
- `ctx.Param` — path parameters (from `BY_<name>` segments)
- `ctx.Form` — form-encoded data
- `ctx.File` — file uploads
- `ctx.Next` — function to call next middleware
- `ctx.Redirect` — function to redirect response
- `common.Publisher` — message publisher (global)
- Pipeable types (Body, Query, Header, Param, Form, File with custom transforms)

**Built-in WS Dependencies**:
- `*ctx.WSContext` — WebSocket connection context
- `*websocket.Conn` — raw connection
- `ctx.WSPayload` — parsed incoming message
- `ctx.Next` — function to call next middleware
- `common.Publisher` — message publisher
- Pipeable types (WSPayload with custom transforms)

**Critical Rules**:
- Parameters match by type identity; a type that only prints like a framework type is refused
- A pointer pipe parameter is refused at `Create()`
- An unexported field of a provider, controller or pipe panics at `Create()`
- Unknown parameter types → `Create()` panics (fail-fast); full table in `core/README.md` › Handler injection

---

## 2. Lifecycle & Initialization Order

### 2.1 App.Create() Initialization Sequence

The order is fixed (comment on `App.Create` in `core/app.go`); the walkthrough is in
[startup-lifecycle-analysis.md](startup-lifecycle-analysis.md) § Phase 2:

```
container.addInterface(common.Logger)
container.wire(root)       modules discovered, OnInit + providers per module, controllers injected
initWS()                   only when EnableWS was called
initRoutes(), initMiddlewares(), initExceptionFilters(), initGuards(), initInterceptors(), initMainHandlers()
app.http.finishRoutes()    one immutable route struct per route, fixed 404 / 405 bodies
initDevtool(), initAccessLog()
callOnReady()              then the app is marked created
```

**Why Order Matters**:
- `wire` MUST run first (it fills the container everything else reads)
- `initWS` MUST run before the layer and handler init functions (they write WS state when WS is enabled)
- `initDevtool` after routes and layers exist; `callOnReady` last

### 2.2 Per-Request Lifecycle (HTTP)

```
1. App.ServeHTTP(w, r)
   └─ a request with a body: r.Body = http.MaxBytesReader(w, r.Body, cap) (cap > 0)
   └─ app.ctxPool.Get() → *httpExchange (context + tracking writer), c.Init(&ex.writer, r)
   └─ X-Request-Id set; Tracer.Enabled() asked once
2. HTTP.handleRequest (the only defer / recover)
   └─ routing.Router.Match(method, path, version, params), retried with the neutral version
   └─ no route: global middlewares, then 404 / 405 (+ Allow) from precomputed bytes
   └─ the route's httpRoute: middlewares → guards → interceptors → handler plan → operators
   └─ panics routed per request-pipeline.md § 1
3. Result written by its kind (core/README.md › Results)
4. With a listener: the complete event (Code = the status written)
5. app.releaseCtx(ex): c.Reset() (keeps slice capacity), back to the pool
```

**Performance**: measured numbers are in `core/README.md` › Benchmarks (a static route is about 0.6 µs and 6 allocations).

### 2.3 WebSocket Connection Lifecycle

```
1. HTTP upgrade request arrives
2. Handshake middleware chain runs (same pipeline as HTTP)
3. websocket.Conn accepted
4. connID (UUID) generated
5. WSConnMgr.Register(connID, conn)
   └─ Creates send channel (buffer 32)
   └─ Spawns writeLoop() goroutine
   └─ Spawns readLoop() goroutine
6. Client can now send messages
7. readLoop() reads and dispatches to handlers
8. Each message creates temporary *ctx.WSContext (pooled)
9. Handler can memorybroker.Publish() — fanout to all subscribers
10. Each subscriber: conn.TrySend() → non-blocking send to channel
11. writeLoop() drains channel and writes to wire
12. Connection closes → readLoop exits
13. Unregister() closes done channel → writeLoop exits
14. All subscriptions cleaned up
```

---

## 3. Context & Request Scoping

### 3.1 HTTPContext Structure

**Pooled Object** (sync.Pool):
```go
type HTTPContext struct {
    *http.Request              // Raw request
    http.ResponseWriter        // Raw response writer
    
    contextID                  // Request ID (16 lowercase hex characters, made in Init)
    
    body Body                  // Parsed request body
    form Form                  // Form data
    file File                  // File uploads
    query Query                // Query parameters
    header Header              // Headers
    param Param                // Path parameters
    aggregations []*aggregation.Aggregation // AddAggregation / Aggregations, per request
    ParamKeys map[string][]int // Indices of dynamic path params
    ParamValues []string       // Values of dynamic path params
    
    Next Next                  // Middleware chain continuation
    Code int                   // HTTP status code (defaults to http.StatusOK)
    Timestamp time.Time        // Request start time
}
```

**No `Deadline` field, `SetDeadline()`, or `IsDeadlineExceeded()` method exist on `HTTPContext`** — verified absent from `ctx/http_context.go`. Do not generate code that calls them. There is no per-request deadline API in this framework; the only timeouts are server-level (`http.Server.ReadTimeout`/`WriteTimeout`/etc., fixed by the serving call, `core/server.go` — see invariant #8 below).

**Lifecycle**:
1. Allocated from pool via `app.ctxPool.Get()`
2. Initialized via `c.Init(w, r)`
3. Used throughout request processing
4. Reset via `c.Reset()` which clears all fields (no reference to the request, writer or parsed input survives)
5. Returned to pool via `app.ctxPool.Put(c)` for reuse

**Operations**:
- `Status(code) *HTTPContext` — sets `c.Code`; chainable, call before `JSON`/`Text` since they write using whatever `Code` was last set
- `Text(data string, args ...any)` / `JSON(data any, args ...any)` / `JSONP(data any, args ...any)` — write response using the current `Code`; a `JSON` encoding error panics with a 500 exception before anything is written
- `Redirect(url)` — HTTP redirect with `Code` when it is 3xx, else 301

### 3.2 WSContext Structure

**Similar to HTTPContext but for WebSocket**:
```go
type WSContext struct {
    *websocket.Conn            // Raw WebSocket connection
    contextID                  // Request ID
    
    wsPayload *WSPayload       // Parsed incoming message
    Next Next                  // Middleware chain continuation
    Timestamp time.Time        // Message arrival time
}
```

---

## 4. Dependency Injection Details

### 4.1 Provider Registration

```go
type Provider interface {
    NewProvider() Provider
}
```

**How It Works** (`core/container.go`):
1. Any struct implementing `NewProvider() Provider` is a provider
2. Register via `ModuleBuilder().Providers(MyProvider{})`
3. At `app.Create()`, `discover` walks the module tree once: each module's visible providers (its own, its imports', the global ones of the root and `IsGlobal` modules)
4. Key = package path + type (`genFieldKey`: `t.PkgPath() + "/" + t.String()`)
5. `provide(key)` constructs a provider once per app: the providers its exported fields name first, then `injectDependencies`, then `NewProvider()`; the result is what every field receives

**Critical Rules**:
- `NewProvider()` is called once per provider type per app; never call it yourself
- Providers are injected by value: state shared between users sits behind a pointer
- `app.Get(MyProvider{})` returns the app's instance after `Create`

### 4.2 Matching

- A field matches a provider of exactly its type: `UserService` and `*UserService` are different keys
- Lookup order per field (`injectDependencies`, `core/fn.go`): a provider of the module, a global provider, an interface the container holds (`common.Logger`, `common.Publisher`); otherwise a field whose type implements `Provider` panics `dependency injection: can't resolve dependency …`, and any other field keeps the value it was given (configuration)
- An unexported field panics `dependency injection: can't set value to unexported '<field>' field of the <type> provider`

---

## 5. Error Handling & Recovery

### 5.1 Exception Type System

**Exception Interface** (exception/exception.go):
```go
type Exception struct {
    message string       // User-facing message
    error error          // Underlying error
    code int             // HTTP status code or WS close code
    stackTrace string    // Full stack trace if captured
}

Methods:
- GetCode() int
- GetMessage() string
- GetStatusText() string (HTTP status text)
- GetStackTrace() string
- SetStackTrace(trace string)
```

**Built-in Exception Types**:
- `BadRequestException` (400)
- `UnauthorizedException` (401)
- `ForbiddenException` (403)
- `NotFoundException` (404)
- `ConflictException` (409)
- `InternalServerErrorException` (500)
- `NotImplementedException` (501)
- `ServiceUnavailableException` (503)
- WebSocket close codes (various)

### 5.2 Panic Recovery

**Recovery Points**:
1. **HTTP request**: `HTTP.handleRequest()` (`core/http.go`) holds the request's only `recover()`. A middleware's panic gets the HTTP layer's fixed error response (no user filter); a panic of a guard, interceptor, pipe, the handler or an operator goes to the route's filters (`httpHandlerPlan.invoke` itself never recovers). Full rules: [request-pipeline.md](request-pipeline.md) section 1.
2. **WS message**: `dispatchWSEvent` (`core/read_loop.go`) recovers into the WS catch chain with `common.NormalizeRecovered`.

**Recovery Flow**:
```
Handler panics with exception.BadRequestException("invalid")
        ↓
recover() catches panic value
        ↓
HTTP: asException(value), else log UnhandledPanic + fixed 500 exception; WS: common.NormalizeRecovered(value)
        ↓
Exception filter chain called with exception (a loop; a filter that panics passes to the next)
        ↓
Filter's Catch(context, exception) handles response
        ↓
HTTP response written (or WS message sent)
```

**NormalizeRecovered() Behavior** (WS only; HTTP never shows a non-exception's text):
- Input: `any` (panic value)
- A non-nil `*exception.Exception`: returned as is (a filter that re-throws `panic(ex)` keeps the code)
- An `exception.Exception` value: a pointer to a copy
- An `error` that wraps an exception (`errors.As`): that exception
- Any other `error`: 500, message `err.Error()`; a `string`: 500, message the string
- Anything else (nil, a nil pointer, an int, a struct): 500, message `"Internal Server Error"`
- No stack trace is captured

### 5.3 Exception Filter Architecture

**Filter Interface**:
```go
type ExceptionFilterable any // matched by its method:
// Catch(*ctx.HTTPContext, *exception.Exception)
```

**Execution Order**:
- Inside one scope the filter bound last runs first
- Module (controller) filters run before global filters, then the built-in filter
- A filter that returns has handled the error; a filter that panics passes what it panicked with to the next filter

**Default Filters**:
- `globalHTTPExceptionFilter` — catches all HTTP exceptions, writes JSON response
- `globalWSExceptionFilter` — catches all WS exceptions

---

## 6. Concurrency Model

### 6.1 Request Isolation

**HTTP Requests**:
- Each request gets its own pooled `*ctx.HTTPContext`
- No shared mutable state within single request
- Thread-safe because each request is independent

**WebSocket Connections**:
- Each connection gets its own `*ctx.WSContext` per message
- readLoop: single goroutine per connection (not concurrent)
- writeLoop: single goroutine per connection (not concurrent)
- Send channel: buffered (32), non-blocking `TrySend()`

### 6.2 Global Shared State

**Thread-Safe**:
- the app's container (providers, layers, routes) — read-only after app.Create()
- `routing.Router` — read-only after app.Create()
- `sync.Pool` for context pooling — handles synchronization
- `trace.Tracer` — listeners in an immutable slice behind an `atomic.Pointer`; `Enabled` / `Emit` take no lock, `Listen` / `stop` serialize on one mutex
- `accesslog.AccessLog` — 64 shards (mutex + map each) of pending requests, a bounded queue to one worker goroutine, atomic drop counter
- `memorybroker.Broker` — single sync.RWMutex over 4 maps (exact/prefix/global/complex); no sharding; PublishAsync tracked via sync.WaitGroup so Close() can drain in-flight goroutines

**Not Thread-Safe (by design)**:
- Module state (captured at app.Create(), immutable after)
- Handler closures capturing request state

### 6.3 Goroutine Model

**Per HTTP Request**: 1 goroutine (within ServeHTTP)

**Per WebSocket Connection**:
- readLoop: 1 goroutine (blocking on conn.Receive)
- writeLoop: 1 goroutine (blocking on send channel)
- Total: 2 goroutines per active connection

**Total Max Goroutines** = 1 (main) + N (concurrent HTTP) + 2M (M active WS connections)

---

## 7. Key Architectural Invariants

**NEVER VIOLATE THESE**:

1. **Context Lifecycle**:
   - HTTPContext must be released via `app.releaseCtx()` after use
   - Never hold onto context reference after request completes
   - Never mutate context fields after guard stage

2. **Dependency Resolution**:
   - Provider and controller fields match provider types exactly (package path + type)
   - Handler parameters are built-in types or pipes, resolved into a plan at `Create()`
   - All injectable fields MUST be exported

3. **Pipeline Execution**:
   - Middlewares continue with `next()`, guards return `bool`, interceptors continue with `Pipe`
   - A panic from a guard onwards reaches the exception filters; a middleware panic gets the fixed error response

4. **Panic Recovery**:
   - `HTTP.handleRequest()` recovers every request panic (except `http.ErrAbortHandler`, re-panicked)
   - Non-exception values never reach the client: fixed 500 + `UnhandledPanic` log line

5. **Module Initialization**:
   - Module tree is immutable after `app.Create()`
   - Providers and controllers are constructed once per app
   - Every `core.New()` is independent: no package-level registry

6. **WebSocket**:
   - Each message is a complete request/response (not streaming)
   - Send channel buffer (32) can overflow — messages drop
   - Fanout is fire-and-forget (no error handling)

7. **Concurrency**:
   - Each HTTP request is independent (no shared mutable state)
   - Each WS connection is single-threaded (readLoop + writeLoop)
   - App state is read-only after app.Create() (each app has its own)

8. **Request Body Size**:
   - Every request body is capped at 10 MiB by default, enforced via `http.MaxBytesReader` in `App.ServeHTTP` (core/app.go)
   - The only way to change it is `ServerOptions.MaxRequestBodyBytes` of the serving call (negative = no cap); the `atomic.Int64` field `app.maxRequestBodyBytes` is written when that call starts
   - Exceeding the cap surfaces as a 413 `exception.RequestEntityTooLargeException` from `ctx.Body()`, not a raw panic (ctx/http_body.go)

9. **Cross-Cutting Concern Instances** (Guard/Middleware/Interceptor/ExceptionFilter):
   - See [package-reference.md](package-reference.md), package `common`, section "One Instance Per Configuration (per app)" for full detail
   - Bound instances are shared per configuration: same `reflect.Type`, same constructor name, `reflect.DeepEqual` value
   - Binding the same struct type twice with different configuration gives two instances; equal configurations share one

