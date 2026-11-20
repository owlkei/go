# Request Pipeline: Middleware, Guards, Interceptors, Filters

**Optimization**: Execution order diagrams, truth tables for stage responsibilities, decision trees.

## 1. Request lifecycle (the spec)

This section is the contract (design `docs/design/0007-dispatch.md`, roadmap 0003 step 1); the code
in `internal/core/http.go` follows it arrow by arrow and `internal/core/http_lifecycle_test.go` tests each rule (L1-L24).

```
request
  │  (the WebSocket path leaves here: loadshed ► proxy ► hostcheck ► origin, the global
  │   middlewares, then the upgrade)
  ▼
pre-route stages ───── a stage refuses ─────────► its answer (HTTP layer; never 404, 405, 410 or
  │  loadshed ► proxy ► hostcheck ► httpsredirect ►      the gate's 503)
  │  headers ► cors ► csrf ► compress (only the stages AppOptions turns on; design 0022 P1-P2)
  ▼
global middleware ──── no route ────────────────► 405 or 404   (HTTP layer)
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

error in a middleware            ► HTTP layer's error response
error from a guard onwards       ► module exception filter ► global exception filter ► built-in filter
```

- **Global** layers are bound on the `App` (`BindGlobal…`). **Module** layers are bound in a
  controller with `BindMiddleware`, `BindGuard`, `BindInterceptor`, `BindExceptionFilter`.
- Inside one box, layers run in the order they were bound. Exception filters are the exception:
  inside one scope the filter bound last runs first.
- The chain is **linear**. `next()` only says "continue": the next layer starts after `Use`
  returns, so code written after `next()` runs before the next layer, not after the handler. Work
  that must happen after the handler belongs in an interceptor's `Pipe` operators.
- A layer stops the request by not continuing: a middleware that returns without `next()`, a guard
  that returns `false`, an interceptor that returns without `Pipe`.
- An **error** is a panic, or a guard's refusal (a 403 exception). Where it goes depends only on
  where it happened: a middleware's error is answered by the HTTP layer; an error in a guard, an
  interceptor, a pipe, the handler or a post-interceptor goes to the route's filters.
- A filter that returns has handled the error. A filter that panics passes what it panicked with to
  the next filter. The built-in filter is always last and writes the HTTP layer's error format.
- Once a response has been written, the framework writes nothing more: a later error is only logged.
- **Middleware is an HTTP concept.** A WebSocket handshake runs the pre-route stages that apply (`loadshed`, `proxy`, `hostcheck`, `origin`) and then the `BindGlobalMiddlewares`
  once, at the handshake; no middleware runs per message.

### 1.1 Stopping and errors per layer

| Layer | Stops the request by | A panic goes to |
|-------|----------------------|-----------------|
| Global / module middleware | returning without `next()` (its own response stays) | the HTTP layer's error response, no user filter |
| Guard | returning `false` (403 `Access denied` to the route's filters) | the route's filters |
| Interceptor (`Intercept`) | returning without `Pipe` (its value is the data) | the route's filters |
| Pipe, argument binding, handler | - | the route's filters |
| Post-interceptor operators | - | the route's filters |
| Exception filter | returning (it handled the error) | the next filter, then the HTTP layer |

### 1.2 The HTTP layer's error response

One writer for the HTTP layer, `recoverRequest`, `answerNoRoute` and the built-in filter
(`internal/core/http_error.go`); users cannot change it (no hook). A middleware that wants its own
response writes it and returns without `next()`.

- Format: JSON without `Render.FS`. With it: a page route from its first guard on is HTML
  (`ctx.HTTPIsPage`), an API / `SERVE` / asset / public file route JSON; before any guard (middlewares, 404, 405, 410)
  HTML when an `Accept` value contains `text/html`, else JSON, plus `Vary: Accept`.
- JSON body `{"code":<int>,"error":<string>,"message":<string>}`, `Content-Type: application/json`;
  HTML is the first root file of `<code>.html`, `<N>xx.html` (`4xx.html`, `5xx.html`) in the root
  layout (data `Code`, `Status`, `Message`, `RequestID`; `render.RenderError`), or a built-in page
  when none exists or the chosen one fails (`RenderFailed`; the next file is not tried). `Content-Length`, `Content-Encoding`,
  `ETag` and `Last-Modified` set earlier are removed, other headers (`X-Request-Id`, `Traceparent`,
  CORS, `Allow`, `Retry-After`) stay.
- Client text (`clientText` in `internal/core/fn.go`, design 0018 R24): code 500 or outside 400-599
  answers the fixed 500 text and logs `InternalServerError` (Error) when the message or a cause is
  not the fixed text; a cause never reaches the client (`error` = status text, cause logged as
  `ExceptionCause` at Debug); otherwise the code, `Error()` (status text when empty, `Internal Server
  Error` when the code has none) and the message (`Unhandled exception has occurred` when empty).
- Anything else (string, error, runtime error, `panic(nil)`, any value) answers
  `{"code":500,"error":"Internal Server Error","message":"Internal Server Error"}` and logs
  `Logger.From(c.Context()).Error("UnhandledPanic", "method", "path", "panic", "stack" ≤ 4 KiB)`: the
  scope's fields (`trace_id`, `request`, `upstream`) come first, and `stack` is sampled (one per stack
  key per second, 10 per app per second, `PanicStacksSuppressed` per second; `internal/core/README.md` ›
  Panic stacks). Filters receive
  `exception.InternalServerErrorException("Internal Server Error")` for such values, also when a
  filter panics with one.
- Once a status ≥ 200 or body bytes went through the writer, nothing more is written and no filter
  runs: `Logger.From(c.Context()).Error("ErrorAfterResponse", "method", "path", "code", <status sent>,
  "panic", "stack")`, sampled like `UnhandledPanic`.
- `panic(http.ErrAbortHandler)` is re-panicked for `net/http`; nothing is written or logged.

### 1.3 No route, HEAD, versioning

- No route for the path under any method: 404, fixed body `{"code":404,"error":"Not Found","message":"Not Found"}`
  (never echoes the path). A route for another method: 405 with `Allow` in the order
  `GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS` (`HEAD` exactly when `GET`).
- `HEAD` is served by the `GET` route through the whole lifecycle; `c.Method` stays `HEAD`.
- Versioning: the route for (method, path, version), else the `NeutralVersion` one. A neutral route
  answers requests with or without a version. With versioning on, a request without a version to a
  route registered without one runs the global middlewares, then answers 410
  `Deprecated URL usage`.
- Before everything (`App.ServeHTTP`, before the WebSocket path test and the stages), a path with
  a control character answers 400 `Invalid URL path`; a non-canonical path is `path.Clean`-ed on a
  copy of the request (design 0021 N).
- `SERVE` routes (`internal/core/http_static.go`, design 0021 D, S, F, M, I) never list a directory:
  a directory answers its `index.html` (301 to the slash-terminated URL) or 404. The handler's root
  must lie below an `HTTP.ServeDirs` entry, else a fixed 500 + `ServeRootRefused`; files open
  through `os.Root` (declared directory → handler root → file). The name policy
  (`isServableName`, `isForbiddenFile`) refuses odd and forbidden names with the fixed 404. Every
  static response gets `nosniff` and a framework-set `Content-Type`; multi-range is ignored; active
  files are sent as attachments with a sandbox CSP unless the handler set `Content-Disposition`; a
  guarded `SERVE_` route defaults to `private, no-cache`.

### 1.3a Page routes (design 0018 amendment 2)

A `RENDER_` (GET, HEAD) or `SUBMIT_` (POST on the path of the `RENDER_` with the same words, same
controller) route of a `common.HTTP` controller (`HTTPLayer.IsPage` → `httpRoute.isPage`, the bound
template in `HTTPLayer.Template`) runs the same boxes. The differences:

- `serveRoute` marks the context (`ctx.HTTPSetPage`) before the first guard: the default status is
  200 for `RENDER_` and 422 for a `SUBMIT_` that renders (no 201), `c.Redirect` without a 3xx status
  answers 303 (API: 301), and errors from the first guard on are HTML (§ 1.2).
- `writeResult` → `writePage` (`internal/core/http_page.go`) after the post-interceptors: if the
  writer already has a status (redirect, direct write) nothing is written; else the only or last
  result (any type, `nil` without one) is the data of the route's template, rendered into a pooled
  `render.Buffer`: `Content-Type: text/html; charset=utf-8`, `Content-Length`, `Cache-Control:
  private, no-cache` unless set (HTML error pages too), and the nonce placed into `helmet`'s precompiled CSP form. A failed render →
  fixed 500 HTML + `RenderFailed` (`RenderReloadFailed` with `IsReload`). `HEAD` writes no body.
- Page routes are version-neutral; `PUT` on a page path answers 405 with `Allow: GET, HEAD, POST`.
  A layer bound to `RENDER_x` only does not run on `SUBMIT_x`; a guard bound so panics at
  `Create()` (`checkSubmitCovered` in `internal/common/fn.go`) unless the same guard type is bound
  to `SUBMIT_x` or controller-wide.
- With a tracer listener: one `render` stage event after `handler`, before `complete` (name = the
  template path, `404` / `4xx` for error pages, `error (built-in)`).
- The asset route (`/assets/*`, `addAssetRoute` in `internal/core/http_asset.go`) exists when
  `Dir/assets` or a page CSS / JS file exists; it is a `SERVE` route (JSON errors), runs the global
  middlewares but no guard (`addGlobalGuard` skips it), answers `ETag` + `immutable` for a matching
  `?v=` hash, and serves `/assets/_pages/<path>` (only the CSS / JS files that templates own). A
  page's file runs the guards of the page's `RENDER_` route (`routeByTemplate`,
  `render.Engine.PageAssetOwner`), refusals → fixed 404, through the async gate like the route.
- Public files: one `GET` route per file of `Dir/public` at its path below `public`
  (`public/<path>` in the route log), `no-cache` + `ETag`, JSON errors, global middlewares, no guard.

### 1.4 Where it lives in the code

- `App.ServeHTTP` (`internal/core/app.go`): caps a request body (never `nil` / `http.NoBody`), takes a pooled
  `httpExchange` (the `ctx.HTTPContext` plus the tracking `responseWriter` of `internal/core/http_writer.go`),
  reads the trace scope `Init` built (`ex.scope`), sets `X-Request-Id` and `Traceparent` from the
  scope's prebuilt header slices (every response: route, 404, 405, error), asks `tracer.Enabled()`
  once, calls `HTTP.handleRequest`, emits `complete` with the status actually written, releases the
  exchange (no `defer`). The WS path passes `c.Request` (the scoped request) to the upgrade, sets both
  headers on the 101 through `websocket.Config.Header`, and releases the exchange after the upgrade
  when the library refused it before `Handshake`.
- Trace scope (design 0011, `internal/tracecontext`): the request ID is always ours; one valid
  `traceparent` continues the trace ID, anything else is ignored silently. Middleware, guards,
  interceptors, filters and the handler read the same `*Scope` (`r.Context()` / `c.Context()`).
  Outbound calls continue the trace through `owlkei.TraceTransport(base)` on an `http.Client` and a request
  built with `c.Context()`: it sends the scope's own `traceparent` (design 0013).
- `HTTP.handleRequest` (`internal/core/http.go`) holds the request's only `recover`. `ex.filters` is `nil`
  while middlewares run (a panic there is the HTTP layer's) and the route's filter list from the
  first guard on.
- One immutable `httpRoute` per route, built at `Create()` and indexed by `RouterItem.Index`: module
  middlewares, guards (global then module), interceptors (global then module), the handler plan,
  filters (module bound last first, global bound last first, the built-in one last), the SERVE index.
- `next` is one closure and one flag per request (only when a middleware exists), so a `next` kept
  from an earlier request cannot continue a later one. Routes without middleware get a shared no-op
  `next` (`ctx.HTTPSetNext`).
- Every layer receives the same `*responseWriter` as `c.ResponseWriter`; it forwards `Flush`,
  `Hijack`, `ReadFrom` and `Unwrap` (for `http.ResponseController`) and must not be used after the
  request returns: `runHooks` sets its inner writer to nil, so a late write gets `errResponseEnded`
  instead of reaching a writer a hook pooled (compress); a late write on an idle exchange is dropped.

---

## 2. Middleware Layer

### 2.1 Middleware Interface

`MiddlewareFn` is an **interface**, not a func type — a bare func literal does NOT satisfy it (internal/common/middleware.go):
```go
type Use = func(*http.Request, http.ResponseWriter, ctx.Next)

type MiddlewareFn interface {
    Use(*http.Request, http.ResponseWriter, ctx.Next)
}
```

Bind calls must pass a **struct value implementing `Use`** — the framework resolves it via `reflect` at bind time, not a plain func:
```go
type LogMiddleware struct{}

func (m LogMiddleware) Use(r *http.Request, w http.ResponseWriter, next owlkei.Next) {
    next()
}
```

**Responsibilities**:
- Inspect the request, set headers
- Call `next()` to continue; return without it to stop (and write your own response)
- `next()` only says "continue": the next layer starts after `Use` returns, so code after `next()`
  runs before the next layer, not after the handler. Work after the handler belongs in an
  interceptor's `Pipe` operators. Calling `next()` twice equals once.
- Middleware is HTTP only: a WebSocket handshake runs the pre-route stages that apply and the `BindGlobalMiddlewares` (not a controller's `BindMiddleware`),
  once, at the handshake (a missing `next()` or a panic refuses it); none runs per message. The
  per-event WS middleware API was removed.

### 2.2 Middleware Registration

**Global** (applies to all routes):
```go
app.BindGlobalMiddlewares(LogMiddleware{})
```

**Module-Scoped** (applies to this module's routes):
```go
type UserController struct {
    owlkei.HTTP
    owlkei.Middleware
}

func (c UserController) NewController() Controller {
    c.BindMiddleware(LogMiddleware{}, c.READ_BY_ID)  // Can specify specific handlers
    return c
}
```

**Optional `NewMiddleware()` constructor** — if the bound type defines `NewMiddleware() MiddlewareFn`, the app calls it once (its container's `construct`, `internal/core/container.go`) instead of using the literal directly — the standard place to precompute/validate config once instead of per-request (the built-in stages are not bound this way: `New` compiles them from `AppOptions`, design 0022). The container keeps one instance per configuration and app (same type, same constructor name, `reflect.DeepEqual` value): equal bindings share an instance, different ones get their own. Same applies to Guard/Interceptor/ExceptionFilter below. A layer bound to a method that is neither a route nor an event of the controller panics at `Create()` (`invalid middleware: … is bound to '<name>', which is not a handler of the controller`); a handler of the other transport binds nothing on this side. Full detail: [package-reference.md](package-reference.md), package `common`.

### 2.3 Middleware Execution Order

0. The pre-route stages of `AppOptions` (fixed order `loadshed`, `proxy`, `hostcheck`, `httpsredirect`,
   `headers`, `cors`, `csrf`, `compress`; before route matching, the async gate and every user
   middleware; trace stage `pre-route`)
1. Global middlewares (bind order), also for a request that matches no route
2. Module middlewares (bind order)
3. Then guards, interceptors, handler (section 1)

A middleware that does not call `next()`: nothing after it runs, no filter runs, and what it wrote is
the response (an empty 200 if it wrote nothing). A middleware's panic is answered by the HTTP layer
(1.2) and still emits its `middleware` trace event, with `Code` = the error response's status.

### 2.4 Exchange hooks (internal, design 0014)

The framework's response writer (`core.responseWriter`, the `w` every middleware receives)
implements `internal/httphook.Writer`: `SetInner(x)` routes every later `Header` / `WriteHeader` /
`Write` / `Flush` / `ReadFrom` / `Hijack` / `Unwrap` to `x` (the recorded status stays the one the
layer passed), and `OnDone(h)` runs `h.Done(elapsed)` once when the exchange ends, last registered
first: after `handleRequest` returned (response or error response written, 404 included) and
before the `complete` trace event; at the end of a WS handshake (accepted or refused); before
`http.ErrAbortHandler` is re-panicked. `reset` clears hooks and the inner writer for the next pooled
request. Unused, it costs one length check. The lifecycle order does not change; `compress` and
`loadshed` use it. Not public API.

### 2.5 Pre-route stages and the client

`proxy` writes the resolved client (IP, scheme, host) into the request's `tracecontext.Scope`;
`owlkei.ClientIP` / `ClientScheme` / `ClientHost` (backed by `tracecontext.ClientIP` / `IsHTTPS` / `Host`) are the
only readers, used by `hostcheck`, `httpsredirect`, `headers` (HSTS), `csrf` (forwarded host, only with `Proxy.IsHostForwarded`), the WebSocket `origin` check, the
throttler and the `complete` event (`trace.Event.ClientIP`, printed as `ip` by the access log).
No stage copies the request, replaces its context or reads its body; refusals
are `panic(&prebuiltException)` (1.2). The order is fixed by `New` (design 0022 P1): `loadshed`, `proxy`, `hostcheck`,
`httpsredirect`, `headers`, `cors`, `csrf`, `compress`; every stage is off until its `AppOptions` field is set,
and user middlewares always run after them. `SERVE_…` routes send
`Cache-Control: no-cache` unless a layer set one, and drop `Cache-Control` on their own 404 / 400.

---

## 3. Guard Layer

### 3.1 Guard Interface

A guard is bound as `any` (internal/common/guard.go) — there is NO compile-time interface. The framework matches the method by name and signature (a type assertion to an unexported interface, then the method value, so a guard call costs no reflection) and requires it to match this func type exactly:
```go
type HTTPCanActivate = func(*ctx.HTTPContext) bool
```
A bound value that has no `CanActivate` method matching this signature fails at bind time with `common.GuardShapeError`. On HTTP, a guard that returns `false` hands `exception.ForbiddenException("Access denied")` (403, `Error()` is `"Forbidden"`) to the route's filters without a panic; on WS the guard middleware still panics it.

**Responsibilities**:
- Inspect request (auth, permissions, etc.)
- Return bool: true = continue, false = forbid
- A panic goes to the route's exception filters

### 3.2 Guard Execution Rules

**Decision Tree**:
```
Guard.CanActivate(httpCtx) → bool

if true:
    Continue with the next guard, then the interceptors

if false:
    No later guard, no interceptor, no handler
    The route's filters receive ForbiddenException("Access denied") (403)

if panic:
    The route's filters receive the error (section 1.2 for non-exceptions)
```

### 3.3 Guard Registration

A guard is a value — bind a struct implementing `CanActivate`, not a bare func:

```go
type AuthGuard struct{}

func (g AuthGuard) CanActivate(httpCtx owlkei.HTTPContext) bool {
    return isAuthorized(httpCtx)
}

type UserController struct {
    owlkei.HTTP
    owlkei.Guard
}

func (c UserController) NewController() Controller {
    c.BindGuard(AuthGuard{}, c.READ_BY_ID)  // Protect specific handler
    return c
}
```

**Common Guards**:
- `AuthGuard`: Check JWT token, user identity
- `RoleGuard`: Check user roles/permissions
- `RateLimitGuard`: Check rate limit
- `IpWhitelistGuard`: Check source IP

---

## 4. Interceptor Layer

### 4.1 Interceptor Interface

An interceptor is bound as `any` (internal/common/interceptor.go) — matched by method name and signature against this func shape, not a compile-time interface:
```go
type HTTPIntercept = func(*ctx.HTTPContext, *aggregation.Aggregation) any
```

**Responsibilities**:
- Pre-handler: Modify request context
- Post-handler: Access response + aggregation
- Return transformed data or pass-through

### 4.2 Intercept() Execution

`Intercept` runs once, before the handler, with a fresh `*aggregation.Aggregation` whose `aggregation.Name` is
the interceptor's name; `core` adds it to the context first (`ctx.HTTPAddAggregation`) and stores the return
value with `aggregation.SetInterceptorData`. `ctx.HTTPAggregations(c)` lists them in execution order. `ctx.HTTPReset`
empties the list and keeps its capacity (one allocation per interceptor per request: the
aggregation).

```
Intercept(httpCtx, aggregation) called before the handler
    ├─ returns aggregation.Pipe(ops...)  → IsMainHandlerCalled(a) = true: the next interceptor, then the
    │                                      handler; afterwards SetMainData(a, data) and Aggregate(a)
    │                                      apply ops in Pipe argument order
    └─ returns without Pipe              → no later interceptor and no handler run; the returned
                                           value is the data, the operators of the interceptors
                                           that ran before it run on it (reverse order), and the
                                           result is written (200, or 201 for POST)
```

Operators that are created (`Transform`, `Tap`, `Filter`) but not passed to `Pipe` never run.

### 4.3 Aggregation Object

```go
type Aggregation struct {
    // no exported field; read by package functions:
    // aggregation.InterceptorData(a) (what Intercept returned), aggregation.Name(a),
    // aggregation.IsMainHandlerCalled(a) (set by Pipe); main data and piped operators
}
```

### 4.4 Interceptor Registration

An interceptor is a value — bind a struct implementing `Intercept`, not a bare func:

```go
type TimestampInterceptor struct{}

func (i TimestampInterceptor) Intercept(httpCtx owlkei.HTTPContext, agg owlkei.Aggregation) any {
    return agg.Pipe(
        agg.Transform(func(data any) any {
            return map[string]any{"data": data, "timestamp": time.Now()}
        }),
    )
}

type UserController struct {
    owlkei.HTTP
    owlkei.Interceptor
}

func (c UserController) NewController() Controller {
    c.BindInterceptor(TimestampInterceptor{}, c.READ_BY_ID)
    return c
}
```

### 4.5 Interceptor Execution Order

**Pre-Handler**:
- Executed in registration order (first registered → last registered)

**Post-Handler**:
- Module interceptors' operators first, then global ones, each in REVERSE bind order
- Each receives the data the previous one produced; the first receives the handler's result
- Inside one interceptor, its piped operators run in `Pipe` argument order

---

## 5. Exception Filter Layer

### 5.1 Exception Filter Interface

An exception filter is bound as `any` (internal/common/exception_filter.go) — matched by method name and signature against this func shape. Note the parameter order: `*ctx.HTTPContext` comes FIRST, `*exception.Exception` SECOND (internal/common/http_exception_filter.go):
```go
type HTTPCatch = func(*ctx.HTTPContext, *exception.Exception)
```

**Responsibilities**:
- Handle an error from a guard onwards and write the response
- Never sees a middleware's error (the HTTP layer answers it)

### 5.2 Exception Filter Execution

**When Triggered**:
```
Guard refusal, or a panic in a guard, interceptor, pipe, binding, handler or operator
    ↓
recovered once by HTTP.recoverRequest() (internal/core/http.go)
    ↓
asException(value) → the exception, or log UnhandledPanic + InternalServerErrorException("Internal Server Error")
    ↓
filters in order: module (bound last first), global (bound last first), built-in
    ↓
Filter.Catch(httpCtx, exception)
```

`HTTP.catch` loops over the route's filters: a filter that returns handled the error (the framework
writes nothing more, even if the filter wrote nothing); a filter that panics passes what it panicked
with to the next one (`panic(ex)` with the exception it got passes it on unchanged). If every filter
panics, the HTTP layer's error response is written for the last panic. Before each filter, a response
already written stops the chain (`ErrorAfterResponse` log). WS keeps `common.RunWSCatchChain` with
`NormalizeRecovered`, which still puts a non-exception panic's text in the message (roadmap 0005).

### 5.3 Filter Execution Order

**LIFO (Last-In-First-Out) per scope**:
- Module filters first, bound last first; then global filters, bound last first; the built-in last
- First filter that returns wins
- Without module filters the first global filter receives the error; without user filters the built-in one

**Example**:
```go
c.BindExceptionFilter(validationFilter, c.READ)    // Registered 1st → called 2nd
c.BindExceptionFilter(authFilter, c.READ)          // Registered 2nd → called 1st

// On exception: authFilter.Catch() → if not handled → validationFilter.Catch()
```

### 5.4 Filter Registration

**Global Filter** (all routes):
```go
app.BindGlobalExceptionFilters(globalHTTPExceptionFilter{})
```

**Route-Specific Filter**:

An exception filter is `any` — bind a struct implementing `Catch(*ctx.HTTPContext, *exception.Exception)`, not a bare func, and note `httpCtx` comes first:

```go
type UserErrorFilter struct{}

func (f UserErrorFilter) Catch(httpCtx owlkei.HTTPContext, ex *exception.Exception) {
    httpCtx.Status(ex.Code())
    httpCtx.JSON(map[string]any{
        "error": ex.Message(),
    })
}

type UserController struct {
    owlkei.HTTP
    owlkei.ExceptionFilter
}

func (c UserController) NewController() Controller {
    c.BindExceptionFilter(UserErrorFilter{}, c.READ_BY_ID)
    return c
}
```

### 5.5 Default Exception Filter

`globalHTTPExceptionFilter` (`internal/core/default.go`) is bound by `New()` and is always the last filter.
It writes exactly the HTTP layer's error response (section 1.2) for the exception it receives, via
`writeErrorResponse` (`internal/core/fn.go`); there is no mode that returns more detail.

Note: `c.JSON`/`c.Text`/`c.JSONP` write using whatever status was last set via `c.Status(code)` — call `Status` before `JSON`; both are chainable since `Status` returns `*HTTPContext`.

---

## 6. Pipeline Interactions

### 6.1 Cross-Layer Communication

**How layers talk to each other**:
- Middleware → Guard: Via httpCtx (modified request)
- Guard → Interceptor: Via httpCtx (if guard returns true)
- Interceptor → Handler: Via httpCtx (modified context)
- Handler → Interceptor: Via return value (in aggregation)
- Interceptor → Filter: Via exception in aggregation
- Filter → Response: Directly writes response

### 6.2 Context Mutations

**Safe to mutate in**:
- Middleware: httpCtx fields
- Guard: httpCtx (read-only recommended)
- Interceptor (pre): httpCtx fields
- Interceptor (post): the handler result, through the piped operators

**NOT safe to mutate in**:
- Exception filter: httpCtx already set

**Rule**: Never mutate httpCtx after guard stage

---

## 7. Error & Recovery Rules

### 7.1 What Happens on Panic?

See section 1.1: a middleware's panic is answered by the HTTP layer; from a guard on, the route's
filters get it; a filter's panic goes to the next filter. Nothing propagates out of `ServeHTTP`
except `http.ErrAbortHandler`.

### 7.2 Guard Returning False

```
Guard.CanActivate() returns false
    ↓
no later guard, no interceptor, no handler
    ↓
the route's filters receive ForbiddenException("Access denied")
    ↓
the built-in filter answers 403 {"code":403,"error":"Forbidden","message":"Access denied"} unless a user filter handles it
```

On a page route (`RENDER_`, `SUBMIT_`, with `Render.FS`) the same 403 is the HTML error page (`403.html`, `4xx.html` or the built-in page).

### 7.3 Timeout Handling

**There is no per-request deadline API.** `ctx.HTTPContext` has no `SetDeadline`/`IsDeadlineExceeded`/`Deadline` field (verified absent from `internal/ctx/http_context.go`) — do not generate handler code that calls them. The only timeouts are set once, server-wide, by the serving call (`Listen` / `ListenTLS` / `Serve`, `internal/core/server.go`), not configurable:

```
ReadHeaderTimeout = 5s
ReadTimeout       = 30s
WriteTimeout      = 30s
IdleTimeout       = 75s
```

These apply to the underlying `http.Server` connection, not to individual handler execution — there's no built-in way to cap how long a single handler is allowed to run. If a handler needs a deadline (e.g. for a slow downstream call), it must create its own `context.WithTimeout` internally.

---

## 8. Performance Considerations

### 8.1 Pipeline Overhead

Measured numbers live in `internal/core/README.md` (Benchmarks): a static route through `ServeHTTP` is about
0.6 µs and 6 allocations, a route with a middleware, a guard and an interceptor about 0.8 µs, a 404
about 0.5 µs and 3 allocations. Per layer the framework adds a direct function call (no wrapper
closure, no reflection); a middleware chain adds one closure and one flag per request; an
interceptor adds one aggregation. A non-exception panic costs a 4 KiB stack capture for its log
line (about 25 µs).

### 8.2 Optimization Tips

1. Keep per-request work in layers small; precompute in `NewMiddleware` / `NewGuard` constructors
2. Prefer a guard returning `false` over panicking a 403 (no unwind)
3. Defer heavy work: use goroutines for non-critical tasks

---

## 9. Common Pipeline Patterns

### 9.1 Auth Flow

```
Middleware (CORS, logging)
    ↓
Guard (check JWT token)
    ↓ (if guard returns true)
Interceptor (enrich context with user)
    ↓
Handler (use user from context)
    ↓
Interceptor (transform response)
    ↓
Response sent
```

### 9.2 Error Handling Flow

```
Handler panics with BadRequestException
    ↓
Panic recovered
    ↓
Route's exception filters (module, global, built-in)
    ↓
Filter writes 400 Bad Request + error details
    ↓
Response sent
```

### 9.3 Rate Limiting Flow

```
Middleware (no-op)
    ↓
Guard (check rate limit)
    ↓
If exceeded: return false → filters → 403 Forbidden
    ↓
If allowed: continue
    ↓
Handler executes
```

