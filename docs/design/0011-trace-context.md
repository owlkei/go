# 0011: Trace context: IDs, scope in `context.Context`, spans on trace events

Status: Approved
Packages: `internal/tracecontext` (new), `internal/crypto`, `ctx`, `core`, `trace`, `accesslog`,
`modules/httpclient`

Roadmap: `docs/roadmap/0004-trace-context-and-logging.md`, steps 1-3. Its Decisions and Background
are taken as given. Design 0012 (log pipeline, steps 4-6) reads the scope through the seam in
"Contract with design 0012".

## Goal
Every HTTP request, WebSocket connection and WebSocket message gets IDs in the W3C Trace Context
format, and the `context.Context` Owlky Go hands out carries them: a middleware, a guard, a handler
or a service called with `c.Context()` reads the same scope, and a goroutine that keeps the context
after the request still reads its own IDs. An incoming `traceparent` continues the caller's trace,
the response and `modules/httpclient` carry ours, and every `trace.Tracer` event says which span it
is and what its parent is.

## Public API

Added:

```go
package ctx

type Context interface {
    Reset()
    GetID() string
    Context() context.Context   // added
}

package trace

type Event struct {
    ID        string
    TraceID   string   // added
    SpanID    string   // added
    ParentID  string   // added
    Stage     string
    Name      string
    Transport string
    Duration  time.Duration
    Operation string
    Target    string
    Code      int
}
```

| Exported name | Why users (or listeners) need it |
|---|---|
| `ctx.Context.Context()` | roadmap decision: every Owlky Go context exposes a `context.Context`. `*HTTPContext` already has it (promoted from `*http.Request`), `*WSContext` already declares it: no new method, only the interface line |
| `trace.Event.TraceID` | groups the events of one trace; the access log prints it |
| `trace.Event.SpanID`, `ParentID` | roadmap step 3; three plain strings instead of new types (Q3) |

Removed:

| Name | Why |
|---|---|
| `ctx.Context.SetID()`, `(*HTTPContext).SetID()`, `(*WSContext).SetID()` | IDs now come from the scope built in `Init`. Today `SetID` is a no-op after `Init` (`ctx/context.go:17-22`), so no user code can depend on its effect. The embedded `contextID` type goes away; `GetID()` becomes a method of each context |

Changed behavior, same signature: `(*HTTPContext).Init`, `(*HTTPContext).GetID`,
`(*WSContext).Init`, `(*WSContext).GetID`, `(*WSContext).Context`, `(*WSContext).SetContext`
(rules S1-S9). No new option, no new header constant (`ctx.RequestID` stays; the `traceparent`
name is unexported).

Internal (not public, listed because the tester and design 0012 use it): package
`internal/tracecontext`.

```go
package tracecontext

const (
    KeyTraceID    = "trace_id"
    KeyRequest    = "request"
    KeyConnection = "connection"
    KeyMessage    = "message"
    KeyUpstream   = "upstream"
)

type Scope struct {
    TraceID    string // 32 lower-case hex
    Request    string // 16 hex; HTTP request scope only
    Connection string // 16 hex; WS message scope only
    Message    string // 16 hex; WS message scope only
    Upstream   string // HTTP request scope only, when a valid one came in
    Parent     string // parent span: traceparent's parent-id (HTTP) or the connection ID (WS message); "" when none
    LogSlot    atomic.Value // reserved for log (design 0012): the scoped logger cached once per scope
}

func NewTraceID(now time.Time) string
func NewSpanID() string
func ParseTraceparent(value string) (traceID, parentID string, flags byte, ok bool)

func NewRequest(parent context.Context, h http.Header, now time.Time) (context.Context, *Scope)
func NewMessage(parent context.Context, now time.Time) (context.Context, *Scope)
func With(parent context.Context, s *Scope) context.Context

func ScopeOf(c context.Context) *Scope
func (s *Scope) AppendArgs(dst []any) []any
func (s *Scope) Traceparent() string
func (s *Scope) SpanID() string   // Request, or Message when Request is empty
```

Files: `id.go` (generator), `traceparent.go`, `scope.go`, `README.md`.

`internal/crypto` keeps `UUID` only (it reads `crypto/rand`, so the name is honest; users:
`memorybroker/fn.go`, `sample/shop/accounts/service.go`). `RandomHex` and `AppendRandomHex` are
deleted: their one caller was `ctx/context.go`, and a `math/rand` function in a package named
`crypto` is the dishonest name the roadmap points at. `core/ws.go` stops importing `crypto`.

## Behavior

### Generator (`internal/tracecontext/id.go`)

- **G1** `NewSpanID()` returns 16 characters in `0-9a-f`, never `0000000000000000`.
- **G2** `NewTraceID(now)` returns 32 characters in `0-9a-f`. The first 12 are `now.UnixMilli()` as
  a 48-bit big-endian number; the last 20 are random (80 bits). This is the ULID layout written in
  hex: IDs made at least 1 ms apart sort as strings in creation order, and the right-most 7 bytes
  are random as W3C Trace Context level 2 requires for the random flag.
- **G3** All random bytes come from `crypto/rand`, for every ID kind (one source). They are read
  through one package-level 512-byte buffer behind a `sync.Mutex`: an ID copies its bytes out, and
  the buffer is refilled with one `crypto/rand.Read` when it runs out (H1). The buffer is filled
  on first use, not at init.
- **G4** Uniqueness: 10,000,000 span IDs hold no duplicate; 10,000,000 trace IDs hold no
  duplicate; 16 goroutines generating 100,000 IDs each produce no duplicate.
- **G5** `NewSpanID` and `NewTraceID` are safe for concurrent use and never return an error
  (`crypto/rand.Read` does not fail since Go 1.24; it aborts the process if the OS has no entropy).

### `traceparent` (`internal/tracecontext/traceparent.go`)

- **P1** `ParseTraceparent(v)` accepts exactly `VV-TTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTT-PPPPPPPPPPPPPPPP-FF`:
  `VV` two lower-case hex digits and not `ff`; trace ID 32 lower-case hex digits, not all zero;
  parent ID 16 lower-case hex digits, not all zero; `FF` two lower-case hex digits; `-` at indexes
  2, 35 and 52. It returns the trace ID, the parent ID, the flags as a byte and `true`.
- **P2** Version `00`: the value must be exactly 55 bytes. Any other version: at least 55 bytes,
  and when longer, byte 55 must be `-`; only the first 55 bytes are read.
- **P3** Everything else returns `"", "", 0, false`: upper-case hex, a shorter value, spaces or
  tabs inside, an all-zero ID, version `ff`, a non-hex byte, an empty string. The work is bounded:
  at most 56 bytes are looked at whatever the length.
- **P4** When the value is longer than 55 bytes, the returned strings are copies (they must not
  keep a long header value alive); otherwise they are substrings of `v`.

### Scope of an HTTP request

- **S1** `(*HTTPContext).Init(w, r)` builds the scope with `NewRequest(r.Context(), r.Header,
  c.Timestamp)`, replaces the request with `r.WithContext(scopeContext)` and keeps it in
  `c.Request`. After `Init`: `c.Context()`, `c.Request.Context()` and the `*http.Request` every
  middleware receives all carry the scope; `c.GetID()` is `Scope.Request`.
- **S2** `Scope.Request` is always a fresh `NewSpanID()`. No header can set or influence it.
- **S3** Trace ID: when `r.Header["Traceparent"]` has exactly one value and `ParseTraceparent`
  accepts it, `Scope.TraceID` is its trace ID and `Scope.Parent` its parent ID. Otherwise (absent,
  two or more values, invalid) `Scope.TraceID` is `NewTraceID(c.Timestamp)` and `Parent` is `""`.
  An invalid header is ignored silently: no log line, no error response.
- **S4** `Scope.Upstream`: the `traceparent` parent ID when S3 accepted one; else the incoming
  `X-Request-Id` when the header has exactly one value of 1 to 64 bytes, each in
  `A-Z a-z 0-9 - _ .`; else `""`.
- **S5** Flags of the outgoing `traceparent`: continuing a trace, the incoming flags `& 0x03`
  (sampled and random bits kept, unknown bits cleared); own trace, `03` (Q1).
- **S6** `Scope.Traceparent()` returns `00-<TraceID>-<SpanID()>-<flags>` (55 bytes).
- **S7** `App.ServeHTTP` sets two response headers before any middleware runs, on every response
  (a route, a 404, a 405, an error): `X-Request-Id: <Scope.Request>` and
  `Traceparent: <Scope.Traceparent()>`. The response `traceparent` therefore names our request as
  the span, with the continued or the new trace ID.
- **S8** The scope outlives the request: a goroutine that kept `c.Context()` (or a context derived
  from it) reads the same `Scope` values after the handler returned, after the pooled context was
  reused by other requests, and after the context was cancelled. The pooled `HTTPContext` holds
  only a pointer to the scope; `Reset` clears the pointer, never the scope.
- **S9** `ScopeOf` finds the scope through any number of `context.WithValue`, `WithCancel`,
  `WithTimeout` wrappers. `ScopeOf(nil)`, `ScopeOf(context.Background())` and a context from
  outside a request return `nil`. `Deadline`, `Done` and `Err` of the scope context are its
  parent's (the server's request context), so cancellation is unchanged.
- **S10** `GetID()` returns `""` before `Init` and after `Reset`, as today.
- **S11** A context that already carries a scope (an app mounted inside another handler) does not
  change S1-S5: `Init` builds a new scope from the headers; `ScopeOf` returns the nearest one.

### Scope of a WebSocket connection and message

- **W1** The handshake is an HTTP request: S1-S6 apply. `App.ServeHTTP` passes `c.Request` (the
  request that carries the scope) to the WebSocket server.
- **W2** The connection ID is the handshake's `Scope.Request`: the `connected` payload's `id`, the
  key in `WSConnmgr`, and `WSConnection.ID` are that 16-hex value. `crypto.UUID()` and the
  `WSConnIDGenerationFailed` log line leave `core/ws.go`. If the connection's request carries no
  scope (a test that builds a `websocket.Conn` by hand), the ID is `NewSpanID()`.
- **W3** The 101 response carries `X-Request-Id` and `Traceparent` with the handshake's values
  (set through `websocket.Config.Header` in the `Handshake` callback).
- **W4** `(*WSContext).Init(conn)` builds a message scope with `NewMessage(parent, c.Timestamp)`,
  where `parent` is `conn.Request().Context()`: `TraceID` is the connection scope's trace ID,
  `Connection` its `Request`, `Message` a fresh `NewSpanID()`, `Parent` the connection ID, flags
  the connection's; `Request` and `Upstream` are empty. With a nil `conn`, a nil request or a
  parent without a scope: parent `context.Background()`, a new trace ID, `Connection` and `Parent`
  empty.
- **W5** `c.GetID()` is `Scope.Message`. `c.Context()` returns the message context: it carries the
  message scope, and it is done when the connection's handler returns (the handshake request's
  context ends then). It no longer returns `context.Background()` after `Init`; before `Init` and
  after `Reset` it still does.
- **W6** A message is one dispatch: `runWSMiddlewares` creates one `WSContext` per topic of a
  `subscribe` or `publish` frame, so a frame naming three topics is three messages with three IDs
  (as today, Q5). Frames that reach no context (`ping`, `pong`, `unsubscribe`, an unknown type, an
  unknown topic) have no message ID.
- **W7** `(*WSContext).SetContext(ctx)` keeps the scope: when `ScopeOf(ctx)` is not this message's
  scope, the stored context is `With(ctx, scope)`. `c.GetID()` never changes after `Init`.
- **W8** S8 holds for a message context kept after the dispatch and after the connection closed.

### Field names (log arguments)

- **L1** `(*Scope).AppendArgs(dst)` appends key-value pairs, in this order, skipping empty values:

  | Key | Value | Present in |
  |---|---|---|
  | `trace_id` | `Scope.TraceID`, 32 hex | every scope |
  | `request` | `Scope.Request`, 16 hex (it is also the request's span ID) | HTTP request, WS handshake |
  | `connection` | `Scope.Connection`, 16 hex | WS message |
  | `message` | `Scope.Message`, 16 hex (also the message's span ID) | WS message |
  | `upstream` | `Scope.Upstream` | HTTP request with a valid incoming ID |

  An HTTP scope yields 4 or 6 elements, a message scope 6. On a nil `*Scope` it returns `dst`.
- **L2** Reserved, produced by nothing in this design: `span_id` (the per-stage span, roadmap
  "Out"; `request` and `message` already are the W3C span IDs of their unit, so writing the same
  value twice per line is not worth the bytes) and `cause` (broker messages, a later roadmap).

### Spans on trace events

- **E1** Every event `core` emits has `TraceID` = the scope's trace ID and `ID` unchanged (the
  request ID, or the message ID on `TransportWS`).
- **E2** `complete` is the root span of its unit: `SpanID == ID`. `ParentID` is `Scope.Parent`:
  the incoming parent ID or `""` (HTTP and the handshake), the connection ID (WS message).
- **E3** Every other stage event is one child span: `SpanID` is a fresh `NewSpanID()`,
  `ParentID == ID`. The tree is flat (stages run one after another; a pipe is not nested in its
  handler, whose duration already excludes pipes).
- **E4** The events of one unit are emitted on its goroutine in lifecycle order, the root last;
  all `SpanID`s of one unit differ.
- **E5** A stage span ID is generated only on the traced branch (`ex.traced` for HTTP, the
  `Enabled()` branch of the WS wrappers). Without a listener a request calls `NewSpanID` exactly
  once (its own ID), as it generated one ID before.
- **E6** HTTP reads the scope once per traced request, right after `Init`, into the exchange
  (unexported field, cleared by `reset`); WS wrappers read `ScopeOf(c.Context())`, which W7 makes
  reliable.
- **E7** The access log line starts with `"trace_id", TraceID`, then `"request", ID` on
  `TransportHTTP`, or `"connection", ParentID, "message", ID` on `TransportWS` (`connection`
  skipped when empty), then `"transport"`, `"code"`, `"operation"` and the rest unchanged. The key
  `id` is gone (it supersedes design 0010's "one key, `id`" for the access log, Q6). Correlation
  inside `accesslog` still uses `Event.ID`.

### `modules/httpclient`

- **C1** `Send()` on a builder whose `Context(ctx)` carries a scope sets the request header
  `Traceparent: <Scope.Traceparent()>`: the trace ID, and our request (or message) ID as the
  parent. The receiving Owlky Go service therefore logs our request ID as its `upstream`.
- **C2** A `traceparent` header set by the caller (`SetHeader`, `Header`, `Headers`) wins: nothing
  is overwritten. No scope in the context, or no `Context(ctx)`: no header. Every retry carries
  the same value. `OnBeforeRequest` hooks run after it is set and may delete it.
- **C3** `X-Request-Id` and `tracestate` are not forwarded (Q4).

### Fixes

- **F1** HTTP exception filter events follow `ex.traced` like every other HTTP stage, not
  `Tracer.Enabled()` at call time. Today (`core/app.go:283-296`) a listener registered while a
  request runs gets filter events for a request that never emits `complete`, which leaves an
  orphan record in the access log until it is evicted. `httpRoute.filters` keeps the name next to
  the catch function and `(*HTTP).catch` emits.
- **F2** When the WebSocket library refuses an upgrade before calling `Handshake` (bad version,
  missing key), the pooled exchange is never released (`core/app.go:112-136`). `ServeHTTP`
  releases it after `upgrade` returns when the callback did not.

## Contract with design 0012

The logger reads the scope with these and nothing else:

```go
import "go.owlky.com/owlky/internal/tracecontext"

func tracecontext.ScopeOf(c context.Context) *tracecontext.Scope
func (s *tracecontext.Scope) AppendArgs(dst []any) []any
```

- `ScopeOf` returns `nil` for a nil context and for a context without a scope; `From(ctx)` then
  returns the logger unchanged. It walks `Value` once (about 8 ns through three wrappers, 0
  allocations) and never blocks.
- The returned `*Scope` is immutable and shared: read it from any goroutine, at any time after
  the request; never write to it. Its strings stay valid for as long as they are referenced.
- `AppendArgs` gives the key-value arguments for adapters (rule L1: fixed keys, fixed order,
  empty values skipped). It boxes each string (one allocation per value); the built-in encoder
  can read the exported fields and the `Key…` constants directly instead.
- The keys are final: `trace_id`, `request`, `connection`, `message`, `upstream`. `span_id` and
  `cause` are reserved (L2).
- 0012 owns the two panic lines of `core` (`logPanic`, `logAfterResponse`, key `"id"` today):
  they should move to `logger.From(c.Context())` and drop `"id"`. This design does not touch them.
- This design changes the access log's ID keys (E7) but not how it writes; 0012 moves its writes
  to the pipeline.

## Performance

Hot path: every request and every WS message. Measured on the review machine (i7-9750H, Go
1.27.4, darwin) with a throwaway benchmark; step 8 measures the real code.

**Idle cost (nothing served, no listener):** 512 bytes of static buffer plus a mutex (filled at
the first ID), no goroutine, no timer, no wakeup, no table. Nothing is added per connection
beyond the handshake's scope (about 210 B, kept while the connection lives).

**Per unit:**

| Part | Before | After | Notes |
|---|---|---|---|
| Random bytes for one request (18) | 3 × `math/rand` ≈ 17 ns | ≈ 32 ns, 0 allocs (60 ns with 12 goroutines) | direct `crypto/rand.Read`: 304 ns, 820 ns with 12 goroutines |
| IDs as text | 1 alloc, 16 B (`RandomHex`) | 1 alloc, 64 B: the 55-byte `traceparent`; trace ID and request ID are substrings of it | |
| Scope context | - | 1 alloc, about 144 B | holds the parent, the `Scope`, and the two one-element header arrays, so the response headers cost no allocation (the `[]string{id}` of today, 16 B, goes away) |
| `r.WithContext` | - | 1 alloc, 320 B, 73 ns | required so middleware (`*http.Request` only) sees the scope (Q2) |
| Header lookups and parse | - | about 40 ns; +20 ns with a valid `traceparent` | two map reads |
| Second response header | - | about 50 ns in `net/http`, 70 bytes on the wire | roadmap decision |
| **HTTP request, no listener** | ≈ 60 ns, 2 allocs, 32 B | **≈ +250 ns, 3 allocs (+1), ≈ 530 B (+0.5 KiB)** | Static is 668 ns / 7 allocs / 160 B today: expect about 0.9 µs / 8 allocs / 0.66 KiB |
| **WS message** | 40 ns, 1 alloc, 16 B | ≈ +60 ns, 2 allocs, ≈ 160 B | scope context + message ID |
| Stage span, no listener | 0 | 0 | E5 |
| Stage span, with a listener | - | ≈ 50 ns, 1 alloc, 16 B per stage | `Event` grows from 112 B to 160 B (passed by value) |
| `ScopeOf` | - | ≈ 8 ns, 0 allocs | |
| `NewSpanID`, `NewTraceID` alone | - | ≈ 50 ns, 1 alloc each | step 1's budget is 100 ns |

Targets the benchmarks must hold: `NewSpanID` and `NewTraceID` under 100 ns and 1 allocation,
single and parallel; `BenchmarkServeHTTP` Static at most +1 allocation; a request with no
listener makes no `NewSpanID` call beyond its own.

**Heavy technique to approve (one):**

- **H1 A hand-managed random buffer.** `var source struct { mu sync.Mutex; buf [512]byte; next
  int }`; `read(dst)` locks, refills with `crypto/rand.Read(buf[:])` when fewer than `len(dst)`
  bytes are left, copies, unlocks (no `defer`). Gain: 304 → 32 ns per request single-threaded,
  820 → 60 ns with 12 goroutines (one system call per 28 requests instead of one per request; on
  Linux without the vDSO the direct call is slower still). Constraint: up to 512 unused random
  bytes sit in memory; fine for identifiers, which are not secrets (see Security). Chosen over a
  `sync.Pool` of buffers (35 ns / 70 ns: no faster, one 512 B buffer per P, dropped at each GC)
  because it is smaller and fixed. Equality proof: a test replaces the reader and checks that the
  bytes of consecutive IDs are the reader's stream in order, across a refill.

**Not proposed:** `unsafe.String` over a byte array inside the scope (would save the 64 B string
allocation, about 20 ns); cutting stage span IDs from one block per traced request (would save
allocations only while a listener exists). Recorded for step 8 if the numbers ask for them.

## Concurrency
- No goroutine is added.
- The generator's buffer and index are guarded by its mutex; it is the only lock, taken for a
  copy of at most 18 bytes (and a 512-byte `crypto/rand.Read` every 28th request), never while
  calling user code.
- A `Scope` and its context are written once, before they are published (`Init`), and read-only
  afterwards: any goroutine may read them without a lock, during and after the request.
- The pooled `HTTPContext` / `WSContext` hold a pointer to the scope. The pool reuses the
  context, never the scope: each request allocates its own, and the garbage collector frees it
  when the last holder drops it. This is why S8 holds without generation counters.
- `context.WithCancel` and `WithTimeout` on the scope context start no goroutine: `Done` and
  `Value` forward to the parent, so the standard library still finds the parent's cancel context.

## Security
- **IDs are identifiers, not secrets.** They are written to logs, response headers and other
  services. Nothing may authorize by a request, connection or trace ID. The WS connection ID stops
  being a UUID that nobody else saw: it appears in the handshake's `X-Request-Id`, the access log
  and user logs. `WSConnmgr` only uses it as a key for the connection's own frames; no client
  message names another connection's ID.
- **Unpredictable anyway:** all bits come from `crypto/rand` (G3), so a client can't guess the
  next ID or force a collision. 64 random bits per request and connection ID: for 100,000 live
  connections the collision probability is about 3 × 10⁻¹⁰.
- **The client controls only the trace ID** (by sending a valid `traceparent`) and `upstream`.
  Both are validated to a fixed alphabet and length before they reach a log line or a header
  (P1-P3, S4): no CR/LF, no quotes, no control characters, at most 32 and 64 bytes. A client can
  make many requests share one trace ID; that is what the format is for, and the request ID stays
  ours (S2).
- **Bounded work:** parsing reads at most 56 bytes; multiple header values are refused without
  being parsed; nothing client-controlled is stored beyond 32 + 64 bytes per request.
- **The trace ID shows its creation time** (millisecond). Responses already carry `Date`.
- **`httpclient` sends our trace ID and request ID to every host called with a request's
  context** (C1), third parties included. They are identifiers only; a caller that minds sets its
  own header or deletes it in `OnBeforeRequest` (C2). `tracestate` and the incoming `X-Request-Id`
  never leave (C3).

## Impact
- **`ctx`:** `context.go` loses `contextID` and the `crypto` import; `http_context.go` and
  `ws_context.go` gain an unexported `scope *tracecontext.Scope`, their own `GetID`, and the new
  `Init`. After `Init`, `c.Request` is a copy of the request passed in: tests that compare
  `c.Request == r` by pointer change. Tests calling `SetID()` (`ctx/context_test.go`,
  `ctx/http_context_test.go`, `ctx/ws_context_test.go`, a few in `core`) are rewritten.
- **`core`:** `app.go` (`ServeHTTP`: both headers from the scope, `c.Request` to the WS server,
  `wsCfg.Header`, F2, event fields), `http.go` and `fn.go` and `fn_plan.go` (event fields, F1),
  `ws.go` (W2), the WS `emit…` closures. `readLoop` needs no change for the scope (W4 happens in
  `Init`).
- **`trace`:** three fields; README table.
- **`accesslog`:** E7 (one branch on `Transport`, `size` of the argument slice). Log parsers that
  read `id` change to `request` / `message`.
- **`modules/httpclient`:** `buildRequest` (C1-C2).
- **`internal/crypto`:** two functions, their tests and benchmarks removed; README rewritten.
- **Compatibility:** the `connected` payload's `id` is 16 hex characters instead of a 36-character
  UUID; `X-Request-Id` keeps its 16-hex shape; responses gain `Traceparent`; code that called
  `SetID()` or implemented `ctx.Context` no longer compiles.
- **Docs (step 7):** READMEs of `internal/tracecontext` (new), `internal/crypto`, `ctx`, `trace`,
  `accesslog`, `core`, `modules/httpclient`; `.owlky/request-pipeline.md`,
  `websocket-system.md`, `package-reference.md`; `CLAUDE.md` needs no change.

## Test plan

Internal test packages, one function per behavior.

`internal/tracecontext/id_test.go`
- G1 `TestNewSpanID_Format` (length, alphabet over 10,000 IDs), `TestNewSpanID_NeverZero` (reader
  stubbed to return zeros, then non-zero: the result is not all zeros).
- G2 `TestNewTraceID_Format`, `TestNewTraceID_TimePrefix` (a fixed `now`: first 12 characters
  equal its milliseconds), `TestNewTraceID_SortsByTime` (two times 1 ms apart),
  `TestNewTraceID_RandomTail` (every hex digit appears at each of the last 20 positions over
  10,000 IDs).
- G3 `TestSource_StreamEqualsReader` (H1: stubbed reader, bytes of consecutive IDs across a
  refill equal the reader's stream), `TestSource_LazyFill` (no read before the first ID).
- G4 `TestNewSpanID_NoDuplicateIn10M`, `TestNewTraceID_NoDuplicateIn10M` (raw values collected in
  a slice, sorted, neighbours compared: 80 MB and 160 MB; skipped with `-short`),
  `TestNewSpanID_ParallelUnique`, `TestNewTraceID_ParallelUnique` (16 goroutines × 100,000, under
  `-race`).
- Benchmarks `BenchmarkNewSpanID`, `BenchmarkNewTraceID`, each with a `_Parallel` twin, and
  `BenchmarkScopeOf`.

`internal/tracecontext/traceparent_test.go`
- P1 `TestParseTraceparent_Valid`, `…_FlagsByte`.
- P2 `…_Version00WrongLength`, `…_FutureVersionWithSuffix`, `…_FutureVersionWithoutDash`.
- P3 one test each: `…_Empty`, `…_Short`, `…_UpperCase`, `…_ZeroTraceID`, `…_ZeroParentID`,
  `…_VersionFF`, `…_NonHex`, `…_WrongDashes`, `…_Whitespace`, `…_ControlCharacters`,
  `…_OneMegabyte` (returns false; a benchmark shows constant time), and a fuzz target
  `FuzzParseTraceparent` (never panics; `ok` implies the grammar of P1).
- P4 `…_LongValueReturnsCopies`.

`internal/tracecontext/scope_test.go`
- S2-S5 `TestNewRequest_OwnTrace`, `…_ContinuesTrace`, `…_InvalidTraceparentIgnored`,
  `…_TwoTraceparentValuesIgnored`, `…_RequestIDNeverFromHeader`, `…_UpstreamFromTraceparent`,
  `…_UpstreamFromRequestID`, `…_UpstreamRejectsBadCharacters`, `…_UpstreamRejects65Bytes`,
  `…_FlagsContinued`, `…_FlagsUnknownBitsCleared`, `…_FlagsOwn`.
- S6 `TestScope_Traceparent` (parses back with `ParseTraceparent`).
- S9 `TestScopeOf_ThroughWrappers`, `…_Nil`, `…_Background`, `TestScopeContext_ForwardsDone`.
- W4 `TestNewMessage_FromConnection`, `…_WithoutConnection`.
- L1 `TestScope_AppendArgs_HTTP`, `…_HTTPWithUpstream`, `…_Message`, `…_NilScope`,
  `…_KeepsExisting`.

`ctx`
- S1 `TestHTTPContext_Init_RequestCarriesScope`, `…_GetIDIsRequest`; S10
  `…_GetIDEmptyAfterReset`; S8 `TestHTTPContext_ScopeSurvivesReuse` (keep `c.Context()`, `Reset`,
  `Init` with another request, the kept context still reads the first IDs).
- W4-W7 `TestWSContext_Init_NilConn`, `…_ContextCarriesMessageScope`,
  `…_SetContextKeepsScope`, `…_SetContextDerivedUnchanged`, `…_ContextBackgroundAfterReset`.

`core`
- S7 `TestServeHTTP_RequestIDAndTraceparentHeaders`, `…_HeadersOn404`, `…_HeadersOnError`,
  `…_ContinuesIncomingTrace`, `…_HostileTraceparentIgnored`, `…_ClientCannotSetRequestID`.
- Step 2 `TestScope_SameInEveryLayer` (global and module middleware through `r.Context()`, guard,
  interceptor, filter, handler, and a provider called with `c.Context()` record the same
  `*Scope`), `TestScope_GoroutineAfterRequest` (a goroutine started by the handler reads its IDs
  after 1,000 further requests reused the pool), `TestScope_ConcurrentRequestsIsolated` (`-race`).
- W1-W3, W5, W8 `TestWS_ConnectionIDIsHandshakeRequestID`, `…_HandshakeResponseHeaders`,
  `…_MessageScope` (connection = connected `id`, own message ID, trace ID of the handshake),
  `…_TwoTopicsTwoMessages` (W6), `…_ContextDoneAfterClose`, `…_ScopeAfterClose`.
- E1-E5 `TestTrace_EventTree_HTTP` (root `complete` last with `SpanID == ID`; each stage
  `ParentID == ID`; distinct span IDs; arrival order = middleware, guard, pre interceptor, pipe,
  handler, post interceptor), `…_RootParentFromTraceparent`, `…_EventTree_WSMessage` (root parent
  = connection ID), `…_HandshakeEvents`, `TestTrace_NoListenerGeneratesOneID` (generator call
  counter), and `BenchmarkServeHTTP` allocations with no listener.
- F1 `TestTrace_ListenerAddedMidRequestGetsNoFilterEvent`; F2
  `TestServeHTTP_RefusedUpgradeReleasesExchange`.

`accesslog`
- E7 `TestLineArgs_HTTPKeys`, `…_WSKeys`, `…_WSWithoutConnection`.

`modules/httpclient`
- C1 `TestSend_ForwardsTraceparent`; C2 `…_CallerHeaderWins`, `…_NoScopeNoHeader`,
  `…_RetriesKeepTraceparent`, `…_BeforeRequestHookCanDelete`; C3 `…_DoesNotForwardRequestID`.

## Open questions

1. **Q1 Flags of a trace we start.** `03` (sampled + random) or `02` (random only). With `02`, a
   downstream service using OpenTelemetry's default parent-based sampler records nothing for
   traces Owlky Go starts; with `03` it records all of them. Recommendation: `03`. Owlky Go has no
   sampler, its logs do carry every request, and "my trace is missing downstream" is the worse
   surprise.
2. **Q2 The request copy.** Putting the scope where middleware can read it (`r.Context()`) needs
   `r.WithContext`: 320 B and 73 ns per request, most of this design's cost. The alternative,
   scope only behind `c.Context()`, saves it but leaves `r.Context()` in middleware and in
   handlers that inject `*http.Request` without IDs, and step 2's "middleware sees the same
   scope" unmet. Recommendation: always copy; one rule, no silent miss.
3. **Q3 Span IDs on events as strings.** Strings cost one 16 B allocation per stage while a
   listener exists; a `trace.SpanID` value type would cost none but adds an exported type and a
   method that the only listener today (the access log) doesn't read. Recommendation: strings.
4. **Q4 `tracestate`.** W3C asks participants to pass it on. Doing so means keeping up to 512
   client-controlled bytes per request and forwarding them to other hosts. Recommendation: not
   now; Owlky Go adds no vendor state, and the trace ID alone keeps the trace joined.
5. **Q5 One message ID per topic of a frame** (W6), because the read loop builds one context and
   one `complete` event per topic. One ID per frame would need a second level (frame → dispatch).
   Recommendation: per dispatch now; revisit with the WebSocket roadmap.
6. **Q6 Access log keys change** from `id` to `request` / `connection` + `message`, plus
   `trace_id` (E7), so access lines join with user logs on the same keys. It breaks parsers
   written against `id`. Recommendation: rename now, before a release fixes the names.
7. **Q7 No start time on events.** Sibling order is the emission order (E4); a `Start time.Time`
   field would let a listener rebuild the timeline from the data alone at 24 B per event.
   Recommendation: leave it to the devtools roadmap, which is the first consumer.

## Coordination

- 2026-11-16: `Scope` gains `LogSlot atomic.Value`, asked by design 0012 (`From` caches the scoped
  logger once per scope instead of per call). 0011 never reads or writes it; a `Scope` is never copied
  after it is created.
- 2026-11-16 (review of roadmap 0004): the internal API also has `func (s *Scope) Headers()
  (requestID, traceparent []string)`, the prebuilt one-element header values of a request scope
  (no allocation per response). The slices are backed by the scope: callers never write them in
  place. `NewMessage` applies the W4 fallback itself: a parent without a scope becomes
  `context.Background()`, so `(*WSContext).Init` looks the connection scope up once.

## Decisions (2026-11-16)

Approved by the user: every open question answered as recommended.

- Q1 traces Owlky Go starts carry flags `03`.
- Q2 the request is always copied with the scope (`r.WithContext`), so middleware and `*http.Request`
  code see the IDs; the cost (about +250 ns, +1 alloc per request) is accepted.
- Q3 span IDs on events are strings.
- Q4 `tracestate` is not propagated.
- Q5 one message ID per topic of a frame; revisited in the WebSocket roadmap.
- Q6 the access log keys move from `id` to `trace_id` and `request` (or `connection` + `message`) now.
- Q7 no `Start` time on events (devtools roadmap).
- Heavy technique H1 (a 512-byte `crypto/rand` buffer behind a mutex) approved.
