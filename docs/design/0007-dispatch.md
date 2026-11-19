# 0007: Request dispatch and the request lifecycle

Status: Implemented
Packages: `core` (`app.go`, `http.go`, `fn.go`, `default.go`, new `http_writer.go`; one function of
`ws.go`), `ctx` (`Reset`), `exception` (`AppendJSON`), `common` (dead code removed)

Roadmap: `docs/roadmap/0003-server-and-production.md`, step 1. Measured against
`docs/review/http-0002.md`. The roadmap's Decisions (lifecycle, fixed HTTP-layer error response, 405,
WS middleware removal, heavy techniques 0003 H3 and 0004 H1) are taken as given.

## Goal
The request lifecycle becomes a written spec that the code follows arrow by arrow and a test proves
arrow by arrow. A request that fails never shows the client anything but an exception's own code and
message; a missing route answers 404 or 405 from precomputed bytes; an interceptor that does not
call `Pipe` really stops the request. `ServeHTTP` loses its per-request closures and map lookups.

## Bugs found
Each was reproduced with a throwaway probe against `master` (4d4576c) through `app.ServeHTTP`;
nothing was left in the tree.

| # | Where | Input | Today | Rule |
|---|---|---|---|---|
| 1 | `common/http_interceptor.go:97-98`, `core/http.go:149-167` | An interceptor returns a value without calling `Pipe` | The response is its value, but the handler still ran (side effects happen), and so did every later interceptor | L12 |
| 2 | `common/exception_filter.go:56-60`, `core/default.go:12-33` | A handler panics a string, an `error`, or hits a runtime error | The text reaches the client: `"message":"handler secret: /etc/passwd"`, `"assignment to entry in nil map"`, `"wrap: sql: no rows; dsn=postgres://u:p@h"` | E3 |
| 3 | `core/fn.go:152-156` | A middleware panics a string or an `error` | Same leak, straight through the built-in filter: `"message":"pq: connection refused 10.0.0.5"` | E3 |
| 4 | `common/http_exception_filter.go:101-108` | A user filter panics a string | The next filter receives it as the message; a filter that prints the message leaks it (`F[G]filter blew up: internal detail`) | E7 |
| 5 | `core/http.go:65-71`, `core/fn.go:152-156` | A handler or middleware writes the response, then panics | A second response is appended to the first: `first{"code":500,…}` with the first status | E4 |
| 6 | `core/fn.go:162-169` | A middleware panics while a trace listener is attached | No `middleware` trace event for it (the emit comes after the call) | E8 |
| 7 | `core/http.go:249-257` | `GET /` + 100,000 characters | The 404 body echoes the path: 100,057 bytes back (a reflection amplifier, bounded only by the header limit) | N1 |
| 8 | `core/http.go:83-87`, `:209-221` | `DELETE /ok` when only `GET /ok` exists | 404 `Cannot DELETE /ok`; no `Allow` | N2 |
| 9 | `common/http.go:13-21` (no `HEAD` token), `core/http.go:83` | `HEAD /ok` on a `READ_ok` route | 404: `curl -I`, health checkers and link checkers fail on every GET route | N3 |
| 10 | `core/http.go:239-243` | `GET /files/` on `SERVE_ANY_OF_files` | A directory listing of the served directory (`<pre><a href=".env">…`) | S1 |
| 11 | `core/http.go:89-105` | Versioning on, no `DefaultVersion`, request without a version to a `…_VERSION_NEUTRAL` route | 410 `Deprecated URL usage`, although `versioning/README.md:41` says neutral routes answer any version | V2 |
| 12 | `core/app.go:113-115` | A request without a body | `r.Body` is `http.NoBody` (not nil), so `MaxBytesReader` is allocated for every GET | P1 |
| 13 | `core/ws.go:89-91` | Any HTTP request while WS is enabled | `isWSPath` builds a new string with `str.Enclose` per request | P10 |
| 14 | `net/http` `checkWriteHeaderCode`, `core/default.go:17` | `panic(exception.NewException("x", 7))` (or a WS close code) on an HTTP route | `WriteHeader(7)` panics inside the built-in filter; the chain ends and the client gets an empty 200 | E6 |

By design, not a bug: a middleware's panic never reaches the user's exception filters (roadmap
Decision). Observed, not changed here: the WS read loop still uses `common.NormalizeRecovered`, so a
non-exception panic in a WS handler still sends its text to the client (roadmap 0005).

## Request lifecycle (the spec)
Step 5 copies this section to `.owlky/request-pipeline.md` as written.

```
request
  │  (the WebSocket path leaves here: handshake middlewares, then the upgrade)
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
- **Middleware is an HTTP concept.** A WebSocket connection runs the middlewares given to `EnableWS`
  once, at the handshake; no middleware runs per message.

## Public API
No exported identifier is added.

Removed (**breaking**; all are framework internals, none has a caller outside the framework):

| Identifier | Why |
|---|---|
| `common.WSMiddlewareItem`, the `WS` field of `common.MiddlewareItem`, `(*common.Middleware).InjectProvidersIntoWSMiddlewares` | per-event WS middleware (roadmap Decision); no caller (`grep`: only its tests and benchmark) |
| `core.WSMiddlewareLayer`, `core.WSCommonLayer` (`core/module_builder.go:38-46`) | no use anywhere |
| `common.BuildHTTPGuardMiddleware`, `common.BuildHTTPInterceptMiddleware`, `common.RunHTTPCatchChain` | `core` calls the guard, the interceptor and the filters directly (L3-L22); kept, they would contradict the lifecycle (`BuildHTTPInterceptMiddleware` always continues) |

Their tests and benchmarks go with them. The WS counterparts (`BuildWSGuardMiddleware`,
`BuildWSInterceptMiddleware`, `RunWSCatchChain`, `NormalizeRecovered`) stay untouched.

Unchanged signatures, changed behavior:
- `exception.Exception.AppendJSON(dst []byte) []byte`: same bytes, written by hand (H1).
- `(*ctx.HTTPContext).Reset()`: keeps the capacity of `ParamValues` and the aggregation list (H3).
- `(*core.App).ServeHTTP`: everything below.

Behavior a user can see (**breaking** where marked):
- **Breaking:** the 404 message is `Not Found` instead of `Cannot <METHOD> <path>` (N1).
- **Breaking:** a panic that is not an exception answers `{"code":500,"error":"Internal Server
  Error","message":"Internal Server Error"}`; exception filters receive that exception instead of
  one carrying the panic text (E3, E7).
- **Breaking:** an interceptor that does not call `Pipe` stops the handler (L12).
- **Breaking:** `SERVE` routes never list a directory (S1).
- New: 405 with `Allow` (N2); `HEAD` on `GET` routes (N3); an exception whose code is not 400-599
  answers 500 (E6).

Unexported contract (names for the tester and the builder; tests are internal to `core`):

```go
// core/http_writer.go: the response writer every layer receives.
type responseWriter struct{ /* http.ResponseWriter, the status written, or 0 */ }
func (w *responseWriter) Header() http.Header
func (w *responseWriter) Write(p []byte) (int, error)
func (w *responseWriter) WriteHeader(code int)
func (w *responseWriter) Unwrap() http.ResponseWriter       // for http.ResponseController
func (w *responseWriter) Flush()                            // forwards when the server supports it
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error)
func (w *responseWriter) ReadFrom(r io.Reader) (int64, error) // keeps sendfile for http.ServeFile
func (w *responseWriter) status() int                       // 0 = nothing written yet

// core/http.go
type httpExchange struct{ ctx.HTTPContext; writer responseWriter } // what App.ctxPool holds
type httpRoute struct{ /* one per route, immutable after Create: middlewares, guards,
    interceptors, the handler plan, filters, the SERVE index; indexed by routing.RouterItem.Index */ }
func (http *HTTP) handleRequest(ex *httpExchange)
func writeHTTPError(ex *httpExchange, e *exception.Exception) // the HTTP layer's error response
func asException(rec any) (*exception.Exception, bool)        // false = not an exception
var notFoundBody, methodNotAllowedBody, goneBody, internalErrorBody []byte // built once, read-only
```

Removed (unexported): `buildUseMiddleware`, `traceHTTPHandler`, `HTTP.catchFnsByRoute`,
`HTTP.lastWildcardSlashIndexByRoute`, `HTTP.resolveAndCallHandler`, `HTTP.emitPostInterceptor`,
`returnNotFound`, `returnInvalidURL`, `returnDeprecatedURL`. `core` registers routes with
`Router.AddInjectableHandler` and finds them with `Router.Match`; it no longer uses `Router.Use`,
`Router.For` or `RouterItem.Handlers` (the `routing` package is not changed).

## Behavior

### Lifecycle (one conformance test per rule)
Setup for L1-L9: one global and one module layer of each kind, each appending its name to a list.

- **L1** Global middlewares run before module middlewares; each scope in bind order.
- **L2** Every middleware of the route has returned before the first guard starts.
- **L3** Global guards run before module guards.
- **L4** Every guard has returned `true` before the first interceptor's `Intercept` starts.
- **L5** Global interceptors' `Intercept` runs before module interceptors'.
- **L6** The handler runs after every `Intercept` has returned through `Pipe`.
- **L7** After the handler, the module interceptors' operators run first, in reverse bind order.
- **L8** Then the global interceptors' operators, in reverse bind order. Each interceptor's operators
  receive the data the previous one produced; the first receives the handler's result.
- **L9** The data left after the last operator is written by the result rules of design 0006
  (`returnHTTP`, `setStatusCode`). The status is 200, or 201 when the request method is `POST`,
  unless a layer or the handler set another.
- **L10** A middleware that returns without calling `next()`: no later middleware, no guard, no
  interceptor, no handler and no filter runs, and the framework writes nothing (what the middleware
  wrote is the response; if it wrote nothing, `net/http` sends an empty 200). Calling `next()` more
  than once equals calling it once. Code after `next()` runs before the next layer starts.
- **L11** A guard that returns `false`: no later guard, no interceptor and no handler runs; the
  route's filters receive `exception.ForbiddenException("Access denied")` (403).
- **L12** An interceptor whose `Intercept` returns without calling `Pipe`: no later interceptor and
  no handler runs. Its return value is the data: the operators of the interceptors that ran before
  it (they called `Pipe`) run on it in reverse order (L7-L8), and the result is written by L9. The
  handler's status rules do not apply (200, or 201 for `POST`).
- **L13** A global middleware panics: no user exception filter runs; the response is the HTTP
  layer's error response (E1-E4).
- **L14** A module middleware panics: same as L13.
- **L15** A guard panics: the error goes to the route's filters (L19-L22).
- **L16** An interceptor's `Intercept` panics: same as L15.
- **L17** The handler, one of its pipes or its argument binding panics: same as L15.
- **L18** An interceptor operator (`Transform`, `Tap`, `Filter`) panics after the handler: same.
- **L19** Filters of a route are ordered: module filters (bound last first), global filters (bound
  last first), the built-in filter. The first one receives the error. With a module filter that
  returns, no global filter runs.
- **L20** A filter that panics: the next filter in the order receives what it panicked with
  (normalized by E7). `panic(ex)` with the exception it received is the way to pass it on unchanged.
- **L21** A route without module filters: the first global filter receives the error; without any
  user filter the built-in one does.
- **L22** A filter that returns has handled the error: the framework writes nothing more, even if
  the filter wrote nothing. If every filter panics, the HTTP layer's error response is written for
  the last panic.
- **L23** A request that matches no route still runs the global middlewares (bind order, L10 and L13
  apply), then answers by N1-N2. No module layer and no filter runs.
- **L24** A request to the WebSocket path (`EnableWS`) runs only the middlewares given to
  `EnableWS`, by L10 and L13; a middleware that does not call `next()`, or panics, refuses the
  handshake. None of `BindGlobalMiddlewares` runs. No middleware runs for a WebSocket message.

### The HTTP layer's error response
- **E1** Format: status = the code, `Content-Type: application/json`, body exactly
  `{"code":<int>,"error":<string>,"message":<string>}` (the bytes of `Exception.AppendJSON`). It
  removes the headers `Content-Length`, `Content-Encoding`, `ETag` and `Last-Modified` set earlier
  in the request and keeps the others (`X-Request-Id`, CORS headers). Users cannot change it.
- **E2** An exception answers with its own code, `Error()` and message. Counts as an exception: an
  `exception.Exception`, a non-nil `*exception.Exception`, and an `error` that wraps one at any depth
  (`errors.As`). An empty `Error()` becomes the status text of the code; an empty message becomes
  `Unhandled exception has occurred` (today's `core/default.go:8`).
- **E3** Anything else (a string, another `error`, a runtime error, `nil`, any value) answers 500
  with `error` and `message` both `Internal Server Error`. The framework logs once:
  `Logger.Error("UnhandledPanic", "id", <request ID>, "method", …, "path", …, "panic", <the value>,
  "stack", <the goroutine's stack, at most 4 KiB>)`. Nothing of the value is in the response.
- **E4** When a response has already been written (a status or body bytes went through the writer:
  `c.Text` / `c.JSON` / `c.Redirect`, `w.Write`, `w.WriteHeader` with a code of 200 or more,
  `http.ServeFile`), no filter runs and nothing more is written; the status and body stay what was
  sent. The framework logs `Logger.Error("ErrorAfterResponse", "id", …, "method", …, "path", …,
  "code", <status sent>, "panic", <value or the exception's message>, "stack", …)`.
- **E5** `panic(http.ErrAbortHandler)` is not caught: it is re-panicked so `net/http` aborts the
  connection. Nothing is written or logged, and no filter runs.
- **E6** An exception whose code is outside 400-599 answers status 500 with `"code":500`; `error`
  and `message` are kept. `WriteHeader` is never called with an invalid code.
- **E7** Exception filters always receive an `*exception.Exception`: the exception itself (E2), or
  for anything else `exception.InternalServerErrorException("Internal Server Error")`, after the log
  line of E3. This holds for what a filter panics with, too (L20).
- **E8** With a trace listener, a middleware that panics still emits its `middleware` event (same
  `ID`, `Name`, `Transport`, `Duration`), with `Code` = the status of the error response. A
  middleware that does not panic emits it with `Code` 0, as today. The `complete` event's `Code` is
  the status actually written (a middleware's `w.WriteHeader(204)` gives 204, not `c.Code`).
- **E9** The built-in exception filter writes exactly the response of E1-E6 for the exception it
  receives. There is no mode that returns more detail.

### No route
- **N1** No route for the path under any method: status 404, body exactly
  `{"code":404,"error":"Not Found","message":"Not Found"}`, `Content-Type: application/json`,
  `X-Request-Id` set. The body does not depend on the request (a 100,000-character path gets the
  same 54 bytes).
- **N2** The path has a route for another method (same version resolution as V1): status 405, body
  `{"code":405,"error":"Method Not Allowed","message":"Method Not Allowed"}`, and an `Allow` header
  listing the methods that have a route for the path, in the order `GET, HEAD, POST, PUT, PATCH,
  DELETE, OPTIONS` (`HEAD` is listed exactly when `GET` is). Any request method gets it, `OPTIONS`
  and unknown methods included. Example: only `READ_ok` and `CREATE_ok` exist; `DELETE /ok` → 405,
  `Allow: GET, HEAD, POST`.
- **N3** `HEAD` is dispatched to the `GET` route of the path, through the whole lifecycle;
  `c.Method` stays `HEAD`. Status and headers equal those of `GET`. Over a real server `net/http`
  sends no body; through `ServeHTTP` on a recorder the body is what the handler wrote.
- **N4** A path longer than `routing.MaxPathLength` (4,096) or with more than 64 segments matches
  nothing: N1.

### Versioning (`EnableVersioning`)
- **V1** The version is `Versioning.GetVersion`. The route is the one registered for (method, path,
  version); if there is none, the one registered as `NeutralVersion`.
- **V2** A neutral route answers every request, with or without a version (bug 11).
- **V3** Kept from today: with versioning on, a request whose version is empty and that matches a
  route registered without a version runs the global middlewares and then answers 410, body
  `{"code":410,"error":"Gone","message":"Deprecated URL usage"}` (HTTP layer; no module layer, no
  filter). See Open question 3.

### Body limit and static files
- **B1** A request body is capped at the app's limit (10 MiB; design 0008 makes it an option):
  reading past it fails and `ctx` answers 413 (design 0003, B9). A request without a body
  (`r.Body == nil` or `http.NoBody`) is not wrapped. Bodies with a declared length and chunked bodies
  are both capped.
- **S1** A `SERVE` route whose resolved path is a directory answers that directory's `index.html`
  when it exists (as `http.ServeFile` does, including its redirect to the slash-terminated URL),
  otherwise N1's 404. No response ever contains a directory listing.
- **S2** Kept: a resolved path outside the served directory answers 400
  `{"code":400,"error":"Bad Request","message":"Invalid URL path"}`; a missing file or a handler
  result that is not a string answers N1's 404.

### Heavy techniques (fast path equals slow path)
- **H1-eq** For any code, error text and message, `Exception.AppendJSON(nil)` equals
  `json.Marshal(struct{Code int "json:\"code\""; Error string "json:\"error\""; Message string
  "json:\"message\""}{…})` byte for byte, and `AppendJSON(dst)` equals `append(dst, thatBody...)`.
- **H3-eq** After `Reset`, an `HTTPContext` equals `NewHTTPContext()` in every field except the
  capacity of `ParamValues` and of the aggregation list; both have length 0 and every element of
  their backing arrays is zero. A slice whose capacity is over 64 is dropped (`nil`).

## Performance
Everything here is hot or warm (a client triggers the 404, 405 and error paths at will). Numbers
"today" are `docs/review/http-0002.md` (medians) and an allocation profile taken for this design
(`-memprofilerate 1`, 20,000 requests per scenario); "expected" is an estimate to be proved with
`benchstat` (`-count 5`, before and after, in the commit).

Where today's allocations come from (objects per request, `core`'s share):

| Scenario | Total | `isNext` + `c.Next` closure | `MaxBytesReader` | `next` wrapper per middleware | aggregation list | 404 body (message, map, boxing, `json.Marshal`, clone) |
|---|---|---|---|---|---|---|
| Static | 9 | 2 | 1 | - | - | - |
| Chain | 25 | 2 | 1 | 1 | 1 | - |
| NotFound | 23 | 2 | 1 | - | - | 16 |

### Planned changes
| # | Change | Type | Today | Expected |
|---|---|---|---|---|
| P1 | No `MaxBytesReader` for a request without a body (bug 12) | A | 1 alloc, about 30 ns on every GET | 0 |
| P2 | One immutable `httpRoute` per route, found by `RouterItem.Index` in a slice | A | 3 map lookups keyed by the pattern string per request (`InjectableHandlers`, the plan map of `app.go:262-264`, `lastWildcardSlashIndexByRoute`), a 4th on errors | 1 slice index; about −60 to −90 ns |
| P3 | No closure for a route without middleware (`c.Next` is a shared no-op there); with middleware, one flag and one closure per request, `next` passed to every `Use` | A | 2 allocs per request + 1 per middleware, 1 `defer` per middleware | 0 allocs without middleware; 2 with, however many; 1 `defer` per request |
| P4 | `HasListeners` is asked once per request | A | once per layer, once per handler call, once at the end (an `RWMutex` read lock each, `event/event.go:132-137`) | 1 per request |
| P5 | Guards and interceptors are called directly; a refused guard goes to the filters without `panic` / `recover` | A | a wrapper closure per layer; a refusal pays a panic and a recover | direct calls; the refusal saves the unwind (to be measured; a recovered panic costs several hundred ns) |
| P6 | H3: the aggregation list and `ParamValues` keep their capacity | B | 1 alloc per request with an interceptor | 0 on a warm context (see H3 for `ParamValues`) |
| P7 | 404, 405, 410 and the fixed 500 are byte slices built once | A | NotFound: 16 allocs and a `json.Marshal` of a map per miss | 1 `Write` of shared bytes |
| P8 | H1: `AppendJSON` by hand | B | about 500 ns, 2 allocs (`json.Marshal` of a struct, design 0004 P2) | about 60-80 ns, 1 alloc (the body, sized once) |
| P9 | `X-Request-Id` and `Content-Type` are set by map assignment with the canonical key | A | `Header.Set` canonicalizes the key | about −20 ns each; still 1 alloc each (0003 H2 is not approved) |
| P10 | `isWSPath` compares without building a string (bug 13; the one WS-file change, forced by `ServeHTTP`) | A | 1 alloc, about 40 ns per HTTP request when WS is on | 0 |
| P11 | `ServeHTTP` releases the context in straight-line code | A | `defer app.releaseCtx(c)` | no `defer`; a request that re-panics (E5) does not return its context to the pool |

Added cost: the tracking writer (two more interface hops per response, about 10 ns, 0 allocs); on a
miss, one `Match` per other method in use (at most six, usually two or three).

### Expected per scenario
| Scenario | Today | Expected | Allocs |
|---|---|---|---|
| Static | 793 ns | about 620 ns | 9 → 6 |
| Param | 1,993 ns | about 1,850 ns | 19 → 16 |
| Query | 3,054 ns | about 2,900 ns | 28 → 25 |
| JSONBody | 4,831 ns | about 4,750 ns | 34 → 32 |
| Chain | 3,123 ns | about 2,850 ns | 25 → 22 |
| Exception | 2,252 ns | about 1,650 ns | 10 → 6 |
| NotFound | 2,783 ns | about 1,100 ns (0.8× net/http's 1,381 ns; the roadmap's bar is 1.5×) | 23 → about 5 |

### Heavy techniques (approved by the roadmap for exactly these uses)
- **H1 (0004 H1): hand-written `Exception.AppendJSON`.** One use: the body
  `{"code":…,"error":…,"message":…}`. It grows `dst` once to the exact size estimate, appends the keys
  as literals and the code with `strconv.AppendInt`. A string made only of printable ASCII
  (0x20-0x7E) without `"`, `\`, `<`, `>`, `&` is copied between quotes; any other string is encoded
  with `json.Marshal(s)` and appended, so escaping is `encoding/json`'s by construction. Callers: the
  built-in filter and `writeHTTPError` (per error), and the package-level bodies of P7 (once).
  Constraint: a new field in the body means changing this function; H1-eq is tested on random and
  hostile strings (quotes, backslashes, `<script>`, control bytes, invalid UTF-8, U+2028, empty,
  64 KiB).
- **H3 (0003 H3): two slices keep their capacity across requests**, on the pooled context:
  `HTTPContext.ParamValues` and the aggregation list. `Reset` clears their elements, cuts the length
  to 0 and drops a slice whose capacity is over 64. Uses: `AddAggregation` appends into the kept
  list (effective at once: −1 alloc per request with an interceptor); `handleRequest` passes
  `c.ParamValues[:0]` to `Router.Match`. **The `ParamValues` half saves nothing yet:** the trie
  allocates the values itself (`internal/ds/trie.go:193-200`, `collect`) until 0002 H1 lets it append
  to the caller's slice, and 0002 H1 is still in the backlog (Open question 2). Constraint:
  `ParamValues` and `Aggregations()` are valid only during the request (`Param()` copies).

Not a listed heavy technique, but it needs a decision (Open question 1): the tracking writer lives
inside the pooled `httpExchange`, so it costs no allocation.

Rejected: a `next` closure kept on the pooled context (0 allocs, but a `next` called late by a
leaked goroutine would let another request pass a middleware that refused it); a 256-byte scratch
array for the error body (that is 0003 H1, not approved); pooling `Aggregation` values (rejected in
design 0004).

## Concurrency
- `httpRoute` values, the route slice, the `Allow` table and the fixed bodies are written during
  `Create` (one goroutine) or package initialization and only read afterwards. Nobody writes to a
  fixed body: `Write` does not keep or change its argument.
- An `httpExchange` belongs to one request on one goroutine from `ctxPool.Get` to `Put`. The `next`
  closure and its flag are allocated per request, so a `next()` called after the request ended
  changes nothing that another request reads.
- The writer handed to middlewares and handlers is part of the pooled exchange: like
  `net/http`'s own `ResponseWriter`, it must not be used after the request returns. `Reset` points it
  at nothing, so a late write on an idle exchange is dropped; a late write after the exchange was
  reused would reach another request (Open question 1).
- `handleRequest` holds the request's only `recover`; each filter call has its own, so a panicking
  filter cannot skip the next one.
- A test serves 64 concurrent requests over routes that take every path above (success, refusal,
  panic, 404, 405) under `-race`.

## Security
- **No internal text leaves the process** (E3, E7, E9): the panic value, the stack and the request
  ID go to the app's logger only. The stack is capped at 4 KiB per log line.
- **No reflected input** in 404, 405, 410 or 500 bodies (N1): fixed bytes, so no amplification and
  nothing to escape. An exception's own `error` and `message` are JSON-escaped by H1 (`<`, `>`, `&`
  included, as `encoding/json` does).
- **Exception text is the developer's choice.** `exception.X("message", err)` puts `err.Error()` in
  the `error` field (design 0004); the README says to pass a cause only when its text is safe to
  show.
- **Limits first:** body cap (B1); path length and depth (N4); at most six extra lookups on a miss
  (N2); the log line of E3 is bounded by the header limit plus 4 KiB.
- **Never two responses** (E4), **never an invalid status** (E6), **`ErrAbortHandler` honored** (E5).
- **Static files:** no directory listing (S1); path traversal answers 400 (S2). Dot files inside the
  served directory are served (`.well-known` must work): the README says to serve a directory that
  holds only public files.
- **405 reveals** which methods exist for a path. That is what `Allow` is for; guards still protect
  the routes.

## Impact
- `core/app.go`: `ServeHTTP`, the `init…` functions that build routes (they fill `httpRoute`s instead
  of calling `Router.Use` / `For`), `releaseCtx`. `core/http.go`: rewritten around `httpRoute`.
  `core/fn.go`: `buildUseMiddleware` removed. `core/ws.go`: `isWSPath` (P10) and
  `resolveGlobalMiddlewares` / `handshake` use the same middleware runner (forced: they called
  `buildUseMiddleware`). No other WS file changes.
- `ctx/http_context.go`: `Reset`. `exception/exception.go`: `AppendJSON`. `common`: removals.
- Tests that change with the behavior: `TestReturnNotFound`, `TestReturnInvalidURL`,
  `TestReturnDeprecatedURL`, `TestServeHTTPNotFound` (message), the `TestTrace_Interceptor…ShortCircuited`
  test (events of interceptors that no longer run), `common`'s tests of the removed functions.
- Docs: `core/README.md` (ServeHTTP, a new "Errors in middleware" section, performance notes and
  benchmarks), `common/README.md` (removed helpers), `exception/README.md` (H1), `ctx/README.md`
  (H3), `.owlky/request-pipeline.md` (the spec above, step 5), `CLAUDE.md` › Request Pipeline (the
  order line), root `README.md` where it shows `Cannot GET`.
- README "Errors in middleware" (content): a middleware's error never reaches exception filters; an
  exception answers with its code and message, anything else with a fixed 500 and a log line; to
  send your own response, write it and return without `next()`; `next()` does not wrap the handler.

## Test plan
`core` tests are internal and go through `app.ServeHTTP` with `httptest`; one function per rule.

| Rule | Test | Case |
|---|---|---|
| L1-L9 | `TestLifecycle_GlobalMiddlewareBeforeModuleMiddleware`, `…_MiddlewareBeforeGuard`, `…_GlobalGuardBeforeModuleGuard`, `…_GuardBeforeInterceptor`, `…_GlobalInterceptBeforeModuleIntercept`, `…_InterceptBeforeHandler`, `…_ModulePostAfterHandler`, `…_GlobalPostAfterModulePost`, `…_PostDataIsTheResponse` | the recorded order of names; L8 also chains two transforms; L9 also `POST` → 201 |
| L10 | `TestLifecycle_MiddlewareWithoutNextStops`, `…_NextTwiceEqualsOnce`, `…_CodeAfterNextRunsBeforeNextLayer` | global and module middleware; empty 200 when nothing is written |
| L11 | `TestLifecycle_GuardFalseGoesToFilters` | 403 through a module filter; the handler counter stays 0 |
| L12 | `TestLifecycle_InterceptWithoutPipeStopsHandler`, `…_ShortCircuitRunsEarlierPosts` | handler counter 0; a later interceptor not called; the global post wraps the value |
| L13-L14 | `TestLifecycle_GlobalMiddlewarePanicSkipsFilters`, `…_ModuleMiddlewarePanicSkipsFilters` | exception and string panics; filter counters stay 0 |
| L15-L18 | `TestLifecycle_GuardPanicGoesToModuleFilter`, `…_InterceptPanic…`, `…_HandlerPanic…`, `…_PipePanic…`, `…_PostOperatorPanic…` | the module filter receives the exception |
| L19-L22 | `TestLifecycle_FilterOrder`, `…_ModuleFilterRethrowReachesGlobalFilter`, `…_NoModuleFilterUsesGlobalFilter`, `…_NoUserFilterUsesBuiltIn`, `…_FilterReturnHandles`, `…_EveryFilterPanics` | two filters per scope; a silent filter gives an empty 200 |
| L23-L24 | `TestLifecycle_NoRouteRunsGlobalMiddlewares`, `…_WSHandshakeRunsOnlyWSMiddlewares` | middleware stops a 404; a WS handshake refused by a middleware |
| E1-E2 | `TestHTTPError_Format`, `…_ExceptionValuePointerWrapped`, `…_EmptyFieldsFallBack`, `…_RemovesEntityHeaders` | exact bytes; `fmt.Errorf("%w")` two levels deep |
| E3 | `TestHTTPError_StringPanicIsFixed500`, `…_ErrorPanic…`, `…_RuntimeErrorPanic…`, `…_NilPanic…`, `…_LogsValueStackAndID` | body bytes equal `internalErrorBody`; a recording logger holds the value, the ID of the `X-Request-Id` header and a stack of at most 4 KiB |
| E4 | `TestHTTPError_AfterHandlerWrote`, `…_AfterMiddlewareWrote`, `…_AfterServeFile`, `…_InformationalStatusDoesNotCount` | body and status unchanged; one log line; filter counter 0 |
| E5-E6 | `TestHTTPError_AbortHandlerRepanics`, `…_InvalidCodeAnswers500` | codes 0, 7, 200, 399, 600, 4001; boundaries 400 and 599 kept |
| E7 | `TestHTTPError_FilterNeverSeesPanicText`, `…_FilterPanicTextNotPassedOn` | a filter that prints `GetMessage()` |
| E8-E9 | `TestTrace_MiddlewarePanicEmitsEventWithCode`, `TestTrace_CompleteCodeIsWrittenStatus`, `TestGlobalHTTPExceptionFilter_EqualsHTTPError` | |
| N1-N4 | `TestNoRoute_404FixedBody`, `…_LongPathSameBody`, `…_405WithAllow`, `…_405ForOptionsAndUnknownMethod`, `…_AllowOrder`, `…_HeadUsesGetRoute`, `…_HeadIn405Allow`, `…_PathOverLimit` | 100,000-character path; routes for every DSL method |
| V1-V3 | `TestVersioning_ExactThenNeutral`, `…_NeutralAnswersWithoutVersion`, `…_UnversionedWithoutVersionIs410`, `…_405UsesTheRequestVersion` | header versioning with and without `DefaultVersion` |
| B1, S1-S2 | `TestBodyLimit_NoBodyNotWrapped`, `…_DeclaredOverLimit413`, `…_ChunkedOverLimit413`, `TestServe_DirectoryWithoutIndexIs404`, `…_DirectoryWithIndex`, `…_NeverListsDirectory`, `…_TraversalIs400` | |
| writer | `TestResponseWriter_TracksWriteAndWriteHeader`, `…_UnwrapWorksWithResponseController`, `…_FlushHijackReadFromForward`, `…_ResetDropsLateWrites` | in `core/http_writer_test.go` |
| H1-eq | `TestAppendJSON_EqualsEncodingJSON_Random`, `…_Hostile`, `…_AppendsToDst`, `FuzzAppendJSON` | in `exception`; 10,000 random strings |
| H3-eq | `TestReset_EqualsNewContextExceptCapacity`, `…_ClearsElements`, `…_DropsOver64` | in `ctx`; reflection over every field |
| removals | `TestCommon_NoWSMiddlewareAPI` is not written: the compiler proves it | |
| concurrency | `TestServeHTTP_ConcurrentAllPaths_NoRace` | 64 goroutines, `-race` |
| benchmarks | `BenchmarkServeHTTP_Static`, `_Chain`, `_NotFound`, `_MethodNotAllowed`, `_Exception`, `_PanicString`, `BenchmarkAppendJSON_Plain`, `_Escaped`, `BenchmarkReset_WithInterceptor` | `b.Loop`, `b.ReportAllocs`; plus `make bench-compare` |

## Open questions
1. **Where the tracking writer lives.** E4 needs a writer that knows whether something was sent.
   Proposed: inside the pooled `httpExchange` (0 allocations; a write made after the request ended,
   which `net/http` already forbids, could reach another request once the exchange is reused; Gin
   and Echo do the same). Alternative: allocate it per request (+1 allocation, about 30 ns on every
   request, late writes harmless). **Recommendation: pooled.**
2. **Approve 0002 H1 with this step?** Without it the `ParamValues` half of H3 saves nothing (see
   H3). It is about ten lines in `internal/ds/trie.go` (`collect` appends to the caller's slice), was
   prototyped at 163 ns + 1 alloc → 120 ns + 0 allocs, and has its benchmark. **Recommendation:
   yes**; otherwise the builder wires `c.ParamValues[:0]` and Param stays at 16 allocations.
3. **Keep the 410 of V3?** With versioning on, a route declared without a version can never be
   reached (410 without a version, 404 with one). Alternative: drop the 410, so a request without a
   version reaches unversioned routes. **Recommendation: keep it in this step** (it is existing
   behavior nobody reported) and decide with the versioning review of roadmap 0004.
4. **L12: do earlier interceptors' operators run on a short-circuit value?** Proposed: yes (a global
   "envelope" interceptor still wraps a value served by a module cache interceptor; this is what the
   arrows "module post → global post → response" say). Today they are skipped. **Recommendation:
   yes.**
5. **`HEAD` → `GET` (N3)** is not in the roadmap. Without it `HEAD` would answer 405 on every GET
   route. **Recommendation: accept N3.**

## Decisions (2026-11-16)

Approved by the user: every open question answered as recommended.

- Q1 the tracking writer lives in the pooled `httpExchange` (0 allocations).
- Q2 heavy technique 0002 H1 (the router appends parameter values to the caller's slice) is **approved** with this step, together with 0003 H3 and 0004 H1.
- Q3 the 410 of V3 stays in this step; versioning is reviewed in roadmap 0004.
- Q4 earlier interceptors' operators run on a short-circuit value.
- Q5 N3 accepted: `HEAD` is served by the `GET` route.
