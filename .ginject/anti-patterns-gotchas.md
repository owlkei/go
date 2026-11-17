# Anti-Patterns, Gotchas, & Common Mistakes

**Purpose**: Document what NOT to do and why. This prevents AI agents from generating broken code.

## 1. Dependency Resolution Gotchas

### 1.1 GOTCHA: Type Mismatch (Provider vs Field) and Providers as Handler Parameters

**Problem**:
```go
type UserService struct{}

func (instance UserService) NewProvider() ginject.Provider { return instance }

type UserController struct {
	ginject.HTTP
	UserService *UserService
}

func (instance UserController) READ_users(us UserService) string { return "" }
```

**Why It Fails**:
- Providers are injected into **fields**, matched by type identity (package path + type): the provider is `UserService`, the field is `*UserService`, so `Create()` panics `dependency injection: can't resolve dependency '*main.UserService' …` (a pointer to a provider type is still a provider type)
- A provider is never a handler parameter: `READ_users(us UserService)` makes `Create()` panic `dependency injection: can't resolve dependencies of the 'func(main.UserService) string' …`

**RIGHT**:
```go
type UserController struct {
	ginject.HTTP
	UserService UserService
}

func (instance UserController) READ_users() string {
	return instance.UserService.Name()
}
```

**Rule**: Providers go in exported fields of exactly the provider's type; handler parameters are the built-in types and pipes (`core/README.md` › Handler injection)

---

### 1.2 GOTCHA: Calling NewProvider() Manually

**WRONG**:
```go
// In handler
func (c UserController) READ() {
    us := UserService{}.NewProvider()  // Manual call!
    us.GetUser(123)  // Wrong type!
}
```

**Why It's Wrong**:
- The app constructs each provider once, after injecting its exported fields: a manual call gets an instance with empty fields
- It is a second instance: state kept by the app's instance is not shared

**RIGHT**:
```go
type UserController struct {
	ginject.HTTP
	UserService UserService
}

func (instance UserController) READ_users_BY_id(param ginject.Param) string {
	return instance.UserService.GetUser(param.Get("id"))
}
```

**Rule**: Never manually call NewProvider() — use injection instead

---

### 1.3 GOTCHA: Circular Dependencies

Providers are value types injected by value, so a cycle (`ServiceA` with a `ServiceB` field, `ServiceB` with a `ServiceA` field) is an invalid recursive type: Go refuses to compile it. A cycle through a pointer field (`B *ServiceB`) needs `*ServiceB` to be a provider type of its own.

**Rule**: Restructure shared logic into a third provider that both depend on

---

## 2. Context Lifecycle Gotchas

### 2.1 GOTCHA: Retaining Pooled Context After Request

**WRONG**:
```go
var lastCtx ginject.HTTPContext

func (c UserController) READ(httpCtx ginject.HTTPContext) {
    lastCtx = httpCtx  // WRONG! Retain pooled object
    return "ok"
}

// Later request uses same pooled context
// lastCtx still holds reference → stale data!
```

**Why It's Wrong**:
- Contexts are pooled for reuse (performance)
- Retaining context = stale reference in next request
- Race conditions: multiple requests share same object
- Memory leaks: context never returns to pool

**RIGHT**:
```go
func (c UserController) READ(httpCtx ginject.HTTPContext) {
    // Use httpCtx only within request
    return httpCtx.ID()  // Use locally
    // After function returns, httpCtx released to pool
}
```

**Rule**: Never hold onto context reference after handler returns. To keep the request's IDs for
later work (a goroutine, a service call), keep `httpCtx.Context()` instead: it is not pooled, and its
trace scope (`internal/tracecontext`) stays readable after the request ended.

---

### 2.2 GOTCHA: Mutating Context After Guard

**WRONG**:
```go
func (c UserController) READ(httpCtx ginject.HTTPContext) {
    // Handler runs AFTER guard
    // If guard saw certain context state, don't change it
    httpCtx.Request.Header.Set("X-User-ID", "123")  // WRONG!
    // Guard already made decision based on original headers
}
```

**Why It's Wrong**:
- Guard made authorization decision based on original request
- Mutating headers after = bypassing guard logic
- Can cause security issues

**RIGHT**:
```go
// Use middleware to set headers (before guard)
app.BindGlobalMiddlewares(func(r *http.Request, w http.ResponseWriter, next ginject.Next) {
    // Runs BEFORE guard
    r.Header.Set("X-User-ID", "123")
    next()
})

// Guard sees modified request
func (c UserController) READ(httpCtx ginject.HTTPContext) {
    // Use context
}
```

**Rule**: Mutations must happen in middleware (before guard), not in handler

---

## 3. Handler Pipeline Gotchas

### 3.1 GOTCHA: Expecting a Guard's `false` to Answer 401

A guard that returns `false` hands `exception.ForbiddenException("Access denied")` (403) to the route's exception filters. A guard may also panic an exception: the panic goes to the same filters, so `panic(exception.UnauthorizedException("not logged in"))` answers 401.

```go
func (instance AuthGuard) CanActivate(c ginject.HTTPContext) bool {
	if c.Request.Header.Get("Authorization") == "" {
		panic(exception.UnauthorizedException("not logged in"))
	}
	return true
}
```

**Rule**: Return `false` for 403; panic an exception for any other status. A non-exception panic answers a fixed 500 and is logged.

---

### 3.2 GOTCHA: Infinite Loop in Middleware

**WRONG**:
```go
app.BindGlobalMiddlewares(func(r *http.Request, w http.ResponseWriter, next ginject.Next) {
    // Process request
    next()
    
    // WRONG: Calling next() again!
    next()  // Infinite recursion
})
```

**Why It's Wrong**:
- Calling next() multiple times = traversing chain multiple times
- Can cause infinite loops or stack overflow
- Double-processes handler

**RIGHT**:
```go
app.BindGlobalMiddlewares(func(r *http.Request, w http.ResponseWriter, next ginject.Next) {
    // Pre-processing
    startTime := time.Now()
    
    next()  // Single call
    
    // Post-processing
    duration := time.Since(startTime)
    log.Printf("Request took %v", duration)
})
```

**Rule**: Call next() exactly once per middleware

---

## 4. WebSocket Gotchas

### 4.1 GOTCHA: Message Drop on Slow Client

**PROBLEM**:
```go
publisher.Publish("users.created", userData)  // Sends to all subscribers via memorybroker
// If subscriber's send channel buffer full (32 messages)
// Message is DROPPED silently
// No error, no exception, no notification
```

**Why It's Wrong**:
- Send channel: 32-message buffer per connection
- If client reads slowly, buffer fills
- New messages drop silently
- Client has no way to know

**Mitigation**:
```go
// Monitor connection lag
// Implement backpressure
// Implement message queue with persistence
// Implement client heartbeat + timeout

// In readLoop:
if channelFull {
    closeConnection(1008, "send buffer overflow")
}
```

**Rule**: Be aware that slow subscribers miss messages

---

### 4.2 GOTCHA: Accessing Raw Connection in Handler

**WRONG**:
```go
func (c ChatController) ON_MESSAGE(conn *websocket.Conn) {
    conn.Send(someMessage)  // Direct access to conn
    // But framework's fanout also uses conn
    // Race condition possible!
}
```

**Why It's Wrong**:
- Both handler and fanout write to connection
- No synchronization
- Can corrupt message stream

**RIGHT**:
```go
func (c ChatController) ON_MESSAGE(publisher ginject.Publisher) {
    publisher.Publish("topic", data)  // Safe, thread-safe
}
```

**Rule**: Use Publisher, never access raw conn directly

---

### 4.3 GOTCHA: No Message Ordering Guarantee

**PROBLEM**:
```go
// Memorybroker publishes to 1000 subscribers
publisher.Publish("event", data)
// Different clients might receive in different orders
// Not guaranteed FIFO across connections
```

**Why**:
- Broker iterates subscribers
- Each fanout callback is non-blocking TrySend()
- Order depends on scheduling

**Implication**:
- Don't assume Client A sees message before Client B
- If order matters, add sequence numbers to messages

**Rule**: Messages not ordered across connections

---

## 5. Module & Provider Gotchas

### 5.1 GOTCHA: Mutable Static Module Variables

**WRONG**:
```go
var MyModule = func() ginject.Module {
    var counter = 0  // MUTABLE STATE!
    
    return ginject.ModuleBuilder().
        Providers(
            CounterService{counter: &counter},  // Shared mutable
        ).
        Build()
}()

// Every app.Create() reuses same counter!
```

**Why It's Wrong**:
- Module created once (static)
- Mutable state persists across app instances
- Multiple tests interfere with each other

**RIGHT**:
```go
// Option 1: Create new module each time
func NewMyModule() ginject.Module {
    return ginject.ModuleBuilder().
        Providers(
            CounterService{counter: 0},
        ).
        Build()
}

// Option 2: Make counter immutable
var MyModule = func() ginject.Module {
    return ginject.ModuleBuilder().
        Providers(
            CounterService{counter: 0},  // Immutable
        ).
        Build()
}()
```

**Rule**: Avoid mutable state in static modules

---

### 5.2 GOTCHA: Expecting a Value Provider to Share State

`NewProvider()` is called once per app, and the result is injected **by value** into every field that names it. A provider that holds a plain counter gives each user its own copy; mutations are not shared.

```go
type CounterService struct {
	count *atomic.Int64
}

func (instance CounterService) NewProvider() ginject.Provider {
	instance.count = new(atomic.Int64)
	return instance
}
```

**Rule**: Open a resource or allocate shared state in `NewProvider()` (once per app) and keep it behind a pointer; close it in the module's `OnShutdown`

---

## 6. Exception Handling Gotchas

### 6.1 GOTCHA: Expecting an Exception Filter to Catch a Middleware Panic

A panic in a middleware never reaches your exception filters: the HTTP layer answers it with the fixed `{"code","error","message"}` format (an exception's code and message, else a fixed 500). Panics from a guard onwards (guards, interceptors, pipes, the handler, operators) go to the filters.

**Rule**: A middleware that wants its own response writes it and returns without `next()` (`core/README.md` › Errors in middleware)

---

### 6.2 GOTCHA: Multiple Exception Filters Fighting

**PROBLEM**:
```go
c.BindExceptionFilter(validationFilter, c.READ)
c.BindExceptionFilter(authFilter, c.READ)

// inside one scope the filter bound last runs first: authFilter
// a filter that returns has handled the error: validationFilter never runs
// a filter that panics passes its panic to the next filter (panic(ex) passes ex unchanged)
```

**RIGHT**: one filter that handles every code it cares about, and `panic(ex)` for the rest:
```go
func (instance APIFilter) Catch(c ginject.HTTPContext, ex *exception.Exception) {
	switch ex.Code() {
	case http.StatusUnauthorized, http.StatusBadRequest:
		c.Status(ex.Code()).JSON(ginject.Map{"error": ex.Message()})
	default:
		panic(ex)
	}
}
```

**Rule**: One filter per route, or make filter handle all cases

---

## 7. Performance Gotchas

### 7.1 GOTCHA: Reflection Overhead in Hot Path

**Problem**: the handler is still called through `reflect.Value.Call` (about 200 ns and one allocation on the reference machine, plus one per result that is not pointer-shaped). The argument plan is built once at `Create()` (`newHTTPHandlerPlan`, design 0006): a request walks no types, builds no keys and makes no map lookups for its arguments, and pipe prototypes are not rebuilt.

**Mitigation**:
- Don't try to cache handler signatures yourself: the framework plans them at `Create()`
- Pipes cost what their `Transform` costs (a `Bind` and the boxed result): profile hot DTOs
- Handlers with more than 8 parameters allocate their argument list

**Rule**: Treat the call itself as a fixed ~200 ns floor; optimize accessors and pipes, not parameter counts

---

### 7.2 GOTCHA: Panicking Non-Exceptions

An exception costs about 1.6 µs per request; any other panic value (a string, an `error`) is logged as `UnhandledPanic` and, for the first panic of its stack key in a second, captures up to 4 KiB of stack: about 25 µs and 7.6 KB. Later panics of the key in that second are logged without the stack (about 6.5 µs per request under a flood; `core/README.md` › Panic stacks).

**Rule**: Panic exceptions (`exception.BadRequestException(…)`) for expected errors

---

### 7.3 GOTCHA: Handler Results by Kind

- A handler returning a pointer (`*User`, `(int, *User)`) writes the pointee as JSON (a nil pointer writes `null`); it used to write the address as text.
- A result declared as `any` holding a string is written as text (`text/plain`); numbers, bools, maps and pointers in an `any` stay JSON (`7` is `application/json`).
- An array (`[3]int`) is JSON. A `chan`, `uintptr` or `unsafe.Pointer` writes nothing.
- A string result is text even if it contains JSON: return a map or struct for a JSON body.

**Rule**: Return a struct, map or pointer for JSON; a string (or an `any` holding one) for text. Table in `core/README.md` › Results.

### 7.4 GOTCHA: Pipe Parameters

- A pipe parameter must be a value type: `func (c X) READ(dto *SearchDTO)` panics at `Create()` (`pipe '*…SearchDTO' … must not be a pointer`).
- `Transform` must return the parameter's type (or one assignable to it): `return &dto` or `return nil` answers a 500 exception (`pipe …: Transform returned …, want …`) and the handler is not called.
- A type from another package that prints like a framework type (your own `ginject.HTTPContext`) is not injected: `Create()` panics.

### 7.5 GOTCHA: Forgetting `From` Inside a Request

```go
func (instance UserController) READ_me(c ginject.HTTPContext) any {
	instance.Logger.Info("ReadMe")                  // no trace_id, no request: can't be tied to the request
	instance.Logger.From(c.Context()).Info("ReadMe") // carries trace_id and request
	return "ok"
}
```

The compiler can't catch it. Pass `c.Context()` (or a context derived from it) to services and call
`From` there. `From` costs about 300 ns and 2 allocations the first time per request and logger, then
about 6 ns; a line through it costs what a plain line does (`log/README.md` › From).

## 8. Testing Gotchas

### 8.1 GOTCHA: State Shared Between Tests Outside the Framework

Every `ginject.New()` is independent: routes, providers, layers, logger and publisher belong to the app, so two tests may give the same module value to two apps. What is shared is what you created once yourself: a package-level `var X = some.Register(…)` that built a backend (a cache), or state captured in a module func.

**Rule**: A new `ginject.New()` per test; build modules that hold resources per test when the tests must not share them

---

### 8.2 GOTCHA: Reading Stdout While an App Serves

Between the end of `Create` and shutdown, built-in log lines reach stdout a moment after the call
(asynchronous pipeline). A test that reads stdout stops the app first (`app.Stop()` flushes), or gives
the app a logger of its own with `SetLogger`.

### 8.3 GOTCHA: Testing WebSocket Without Actual Connection

**WRONG**:
```go
// Trying to test WS handler without real connection
func TestWSHandler(t *testing.T) {
    c := ctx.NewWSContext()
    // c.Conn is nil!
    
    app.ws.handleRequest(c)  // Panics: nil pointer dereference
}
```

**Why It's Wrong**:
- WSContext needs real websocket.Conn
- Can't test without real connection or mock

**RIGHT**:
```go
// Use httptest.Server with websocket upgrade
// Or mock websocket.Conn interface
// Or test handler logic separately (use dependency injection)
```

**Rule**: WS tests need real connections or mocks

---

## 9. Message Broker (`memorybroker`) Gotchas

### 9.1 GOTCHA: Calling Close() Synchronously From a PublishAsync Handler

**VERIFIED** (reproduced deterministically, not speculative — see `memorybroker/broker_test.go`'s `TestClose_FromWithinPublishAsyncHandler_Deadlocks`):

```go
b.Subscribe("foo", func(*memorybroker.Message) {
    b.Close()  // DEADLOCKS — never returns
})
b.PublishAsync("foo", nil)
```

**Why It's Wrong**:
- `PublishAsync`'s goroutine runs `defer wg.Done()` around the call that (transitively) invokes the handler
- The handler calls `Close()`, which blocks in `wg.Wait()`
- But `wg.Done()` for *this exact goroutine* only fires after the handler returns — which can't happen until `Close()` returns
- Same structural class as calling `sync.WaitGroup.Wait()` from inside a goroutine that same `WaitGroup` is tracking — universally caller error in Go, not a bug in the mutex/lock logic (already independently audited and confirmed race-free)
- Not fixable without either weakening "Close waits for every accepted PublishAsync" or goroutine-local-storage hacks (Go has none by design) — treated as an unsupported call pattern, not special-cased

**RIGHT**:
```go
b.Subscribe("foo", func(*memorybroker.Message) {
    go b.Close()  // fire the close from a different goroutine
})
```

**Rule**: Never call `Broker.Close()` synchronously from within a handler dispatched by `PublishAsync`. Calling `Close()` from within a handler dispatched by the *synchronous* `Publish` is safe — `Publish` isn't `WaitGroup`-tracked, and no lock is held during handler execution (see `metadata-concurrency-model.json`'s `deadlock_risks.risk_2`).

### 9.2 GOTCHA: Assuming Close() Waits For Synchronous Publish

**PROBLEM**:
```go
go b.Publish("slow", payload)  // handler still running
b.Close()                      // returns almost immediately — does NOT wait for the handler above
```

**Why**: `Close()` only synchronizes with in-flight `PublishAsync` goroutines (via `sync.WaitGroup`) and with its own map-clearing (via the broker's `RWMutex`). A synchronous `Publish` call on another goroutine is invisible to `Close()` — this is intentional, not a bug (verified via `TestClose_DoesNotWaitForConcurrentSyncPublish`, race-clean).

**Rule**: If code needs to guarantee no handler is still running after shutdown, don't rely on `Close()` for synchronous `Publish` calls — track them externally (e.g. your own `WaitGroup`) or route that traffic through `PublishAsync` instead.

### 9.3 GOTCHA: Assuming a Foreign Subscription Silently Corrupts State

```go
subA, _ := brokerA.Subscribe("topic", handler)
brokerB.Unsubscribe(subA)  // returns ErrForeignSubscription, no-op on both brokers
```

**Not a bug to guard against defensively** — `Unsubscribe` checks `subscription.broker == receiver` and rejects mismatches with `ErrForeignSubscription` before touching any map. `subA` remains fully active on `brokerA`.

**Rule**: A cross-broker `Unsubscribe` call is safe (rejected with a typed error), but almost always indicates a wiring bug — the caller has the wrong `Broker` reference.

## 10. Cross-Cutting Concern Instance Gotchas

### 10.1 GOTCHA: Expecting Two Equal Bindings to Get Two Instances

Binding one Middleware/Guard/Interceptor/ExceptionFilter type with **different** configuration gives one instance per configuration:
```go
// Controller A and controller B each get their own cors.CORS instance.
c.BindMiddleware(cors.CORS{AllowOrigins: []string{"https://a.example.com"}})
c.BindMiddleware(cors.CORS{AllowOrigins: []string{"https://b.example.com"}})
```

**The gotcha**: the app's layer cache (`(*container).construct` in `core/container.go`, behind every `Bind…`, global or per-controller, HTTP and WS) shares one instance, inside one app, between bindings whose values are **deeply equal** (same `reflect.Type`, same constructor name, `reflect.DeepEqual`). Two `throttler` guards bound with the same options on two controllers are one limiter with one shared budget, not two. A value holding a non-nil func or a different channel is never equal to another, so it is never shared.

**Rule**: Equal configuration → one shared instance (its fields are yours to protect across requests); give the bindings different configuration when they must keep separate state.

### 10.2 GOTCHA: Binding a Layer to a Method That Is Not a Handler

`c.BindGuard(AuthGuard{}, c.Helper)` where `Helper` is neither a route nor an event of the controller panics at `Create()`: `invalid guard: <type> is bound to 'Helper', which is not a handler of the controller`. Before, the layer silently applied to every route. A handler of the other transport (a `SUBSCRIBE_…` method given to an HTTP-only guard) binds nothing on that side.

## 11. HTTP Body Size Gotchas

### 11.1 GOTCHA: Assuming Request Bodies Are Unbounded

**WRONG** (generating an upload handler without checking the cap):
```go
func (c UploadController) CREATE(body ginject.Body) any {
    // Assumes any size body reaches here — it doesn't.
}
```

**Why It's Wrong**: Every request body is wrapped in `http.MaxBytesReader` at the app's cap: 10 MiB unless the serving call sets `ServerOptions.MaxRequestBodyBytes` (negative = no cap). Do not generate `app.SetMaxBodySize(...)`, it does not exist. A body over the cap causes `ginject.Body()` to panic `exception.RequestEntityTooLargeException` (413) instead of parsing.

**RIGHT**: Raise the cap for the whole app with `app.Listen(port, &ginject.ServerOptions{MaxRequestBodyBytes: 50 << 20})`; there is no per-route cap.

**Rule**: Any handler that accepts uploads must account for the app-wide cap and the 413 it produces; don't assume an unlimited body size.

## 12. CORS, CSRF and Client IP Gotchas

### 12.1 GOTCHA: `cors.CORS{}` With No Origin List

**WRONG**:
```go
cors.CORS{}                           // panics at Create(): invalid cors: AllowOrigins is empty
cors.CORS{AllowOrigins: []string{"*"}, IsAllowCredentials: true} // panics: * never with credentials
```

**Why It's Wrong**: the zero value allows nothing (design 0014) and `*` with credentials would give every site credentialed access. The fields are exactly `AllowOrigins`, `AllowMethods`, `AllowHeaders`, `ExposeHeaders`, `MaxAge`, `IsAllowCredentials`, `IsAllowPrivateNetwork`; origins are exact strings or `*.` patterns, never patterns of another syntax.

**RIGHT**:
```go
cors.CORS{AllowOrigins: []string{"https://app.example.com", "https://*.example.com"}, IsAllowCredentials: true}
```

**Rule**: list lower-case `scheme://host[:port]` origins or `scheme://*.host[:port]` patterns; `*` only alone and only without credentials.

### 12.2 GOTCHA: CSRF Tokens

**WRONG**: generating CSRF token fields or helpers on `csrf.CSRF`, or front-end code that reads a CSRF cookie and sends a token header.

**Why It's Wrong**: `csrf` now wraps `http.CrossOriginProtection` (Fetch Metadata + `Origin`): there is no token, no cookie and no context value. The only option is `TrustedOrigins`.

**RIGHT**: `csrf.CSRF{}` or `csrf.CSRF{TrustedOrigins: []string{"https://app.example.com"}}`; keep `GET` free of state changes.

### 12.3 GOTCHA: Reading the Client IP From Headers

**WRONG**:
```go
ip := c.Request.Header.Get("X-Forwarded-For") // any client can send it
ip := c.Request.Header.Get("X-Real-IP")
```

**Why It's Wrong**: a client writes these headers itself: one new value per request bypasses a rate limit and fakes the log. Behind a proxy, `r.RemoteAddr` is the proxy.

**RIGHT**: bind `proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}}` first, then read `proxy.ClientIP(c.Request)`, `proxy.Scheme(c.Request)` and `proxy.Host(c.Request)` everywhere (handlers, guards, a throttler `KeyFunc`).

**Rule**: never parse a forwarding header yourself; without `proxy` bound the client is the connection's peer, by design.


### 12.4 GOTCHA: Security Limits Left to the App (security review 0005)

Each is documented once in the package README named; generated code must respect them.

- **Header value slices are shared** between responses (`helmet`, `cors`, `compress`, static
  `Cache-Control`, the scope's `X-Request-Id` / `Traceparent`): change headers with `Set` / `Add` /
  `Del` or a new slice, never `h[k][0] = …` (`core/README.md` › Response writer).
- **`c.Redirect` takes any URL**: validate a URL from the request (local paths only) before
  redirecting (`ctx/README.md` › Redirect).
- **`c.JSON` / `c.Text` format arguments are not escaped**: pass request data in a `ginject.Map` or a
  struct, never as a format argument or the format string (`ctx/README.md` › JSON).
- **`ginject.TraceTransport` sends `traceparent` to every host**, and an incoming valid `traceparent`
  sets the trace ID and flags: never authorize by a trace ID (`trace/README.md` › Security).
- **`proxy` hops mode (`TrustedHops`) doesn't check the peer**: only when the app is unreachable
  except through the proxies (`middlewares/proxy/README.md` › Security).
- **Throttler**: a client with an IPv6 /48 can evict its own /64 counters from the 32,768-entry
  default store; `SlidingWindow` / `TokenBucket` are exact within one process only, even with a
  shared `Backend` (`guards/throttler/README.md` › Known limits).
