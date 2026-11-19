# 0013: Request scope cost and outbound trace context

Status: Approved
Packages: `internal/tracecontext`, `trace`

Roadmap: `docs/roadmap/0005-middlewares-modules-security.md`, steps 1 and 2. Its Decisions are taken
as given. Supersedes these parts of design 0011: rule G3 and heavy technique H1 (the random source),
and rules C1-C3 (`modules/httpclient`, replaced by `trace.Transport`; the module is removed in step
12). Every other rule of 0011 stays as written: G1, G2, G4, G5, P1-P4, S1-S11, W1-W8, L1-L2, E1-E7.
Designs 0014 (middlewares) and 0015 (config and cache) meet this one in "Coordination".

## Goal
The ID generator stops being a lock that every request, message and stage span waits on: with all
cores busy, requests no longer queue on one mutex. A client built on `net/http` continues the
request's trace by wrapping its transport with `trace.Transport`: one function, no module, no
option. The measurements below also say what this step does not buy: on one or two CPUs no safe
candidate lowers the cost of a request, and the one candidate that does is unsafe (Q1).

## Public API

Added (step 2):

```go
package trace

func Transport(base http.RoundTripper) http.RoundTripper
```

| Exported name | Why users need it |
|---|---|
| `trace.Transport` | roadmap decision: outbound calls use `net/http` with `trace.Transport`. It is the only way left to send `traceparent` once `modules/httpclient` is gone. One function; the returned type is unexported |

Nothing else is exported. No option, no header constant, no new `Event` field. Step 1 changes no
exported name and no signature in `ctx` or `core`.

Usage (for the README):

```go
client := &http.Client{Transport: trace.Transport(nil), Timeout: 10 * time.Second}

req, _ := http.NewRequestWithContext(c.Context(), http.MethodGet, url, nil)
res, err := client.Do(req)
```

Internal (not public; listed because the tester uses it), package `internal/tracecontext`:

```go
const sourceSize = 504 // was 512: 28 requests of 18 bytes, and the source is one 512-byte object

type source struct {
    buf  [sourceSize]byte
    left int
}

func (s *source) read(dst []byte)

var sources sync.Pool // of *source; replaces `var source struct{ mu sync.Mutex; … }`

func (s *Scope) ChildTraceparent() string // step 2; not added if Q4 is answered as recommended
```

Removed: the package-level `source` variable and its mutex; the unexported field
`Scope.traceparent` (it duplicated `headers[1]`; `Traceparent()` returns `headers[1]`, which
`NewRequest` and `NewMessage` both fill). `NewRequest`, `NewMessage`, `NewSpanID`, `NewTraceID`,
`ScopeOf`, `With`, `Headers` and `AppendArgs` keep their signatures.

File for the transport: `trace/http_transport.go`.

## Behavior

### Random source (step 1; replaces 0011 G3)

- **R1** `(*source).read(dst)` copies `len(dst)` bytes out of the source's buffer. When fewer than
  `len(dst)` bytes are left it first refills the whole buffer with one `crypto/rand.Read` of exactly
  504 bytes and drops the leftover. A zero `source` makes no read until its first `read`. The bytes
  of consecutive `read` calls on one `source` are the reader's stream in order, across refills.
- **R2** All random bytes of every ID still come from `crypto/rand` (0011 G3's first sentence
  stays). `NewSpanID`, `NewTraceID`, `NewRequest` and `NewMessage` take a `*source` from `sources`,
  read, and put it back before they return. No `sync.Mutex` is taken. A source is used by one
  goroutine at a time, so no random byte is handed out twice (0011 G4 keeps proving it: no
  duplicate in 10,000,000 IDs, none across 16 goroutines × 100,000).
- **R3** One read per scope. `NewRequest` without an accepted `traceparent` (0011 S3) makes one
  `read` of 18 bytes: the first 10 are the random tail of the trace ID, the last 8 the request ID.
  With an accepted `traceparent` it makes one `read` of 8 bytes (the request ID). `NewMessage` under
  a connection scope reads 8 bytes; without one (0011 W4 fallback) 18. So with a reader that hands
  out a counting stream, the 8 bytes of the request ID directly follow the 10 bytes of the trace
  tail.
- **R4** Zero redraw (keeps 0011 G1): when the 8 ID bytes are all zero, or the 10 tail bytes are all
  zero, the whole read is drawn again. `NewSpanID` never returns `0000000000000000`.
- **R5** Every call the generator makes to the reader asks for exactly 504 bytes.
- **R6** The scope is unchanged for its readers: for the same reader stream and headers, the
  `Scope` fields, `Traceparent()`, `SpanID()`, `Headers()` and `AppendArgs()` return what they
  return today. `Headers()` still returns two one-element slices backed by the scope.
- **R7** The request is still copied (0011 S1, decision Q2 of 0011): after `Init`, `c.Request != r`,
  and `c.Context()`, `c.Request.Context()` and the `*http.Request` a middleware receives carry the
  same `*Scope`. A scope is allocated per request and never pooled or reused (0011 S8).
- **R8** Allocations: `NewRequest` makes at most 2 allocations (the scope context and the 55-byte
  `traceparent` string); a static route keeps its 7 allocations
  (`TestServeHTTP_StaticRouteAllocations`).

### `trace.Transport` (step 2; replaces 0011 C1-C3)

"Scope" below is `tracecontext.ScopeOf(req.Context())`.

- **T1** `Transport(base)` returns a non-nil `http.RoundTripper` that is safe for concurrent use.
  With a nil `base` it calls `http.DefaultTransport`, read at each `RoundTrip` (as `http.Client`
  does).
- **T2** No scope: `base.RoundTrip` receives the same `*http.Request` pointer, and the wrapper
  allocates nothing. This is the "zero cost without a scope" of the roadmap: one context lookup.
- **T3** A scope and no `Traceparent` key in `req.Header`: `base.RoundTrip` receives a shallow copy
  of the request (another pointer; the same method, URL, body, `GetBody`, `ContentLength` and
  context) whose `Header` is a deep copy of `req.Header` plus exactly one value under
  `Traceparent`: `00-<Scope.TraceID>-<span>-<flags>`. `<flags>` are the scope's (the last two
  digits of `Scope.Traceparent()`). `<span>` is the call's own span ID: a fresh `NewSpanID()` for
  each `RoundTrip` call, 16 lower-case hex digits, never the scope's `SpanID()`; two calls with the
  same context carry different spans. `ParseTraceparent` accepts the value. (Q4 proposes the
  scope's span instead.)
- **T4** The caller's request is never changed (the `http.RoundTripper` contract): after
  `RoundTrip`, `req.Header` has no `Traceparent` key and the same entries as before; a nil
  `req.Header` stays nil.
- **T5** A `Traceparent` key that exists in `req.Header` wins, whatever it holds (a valid value, an
  invalid one, or no value at all): `base` receives the same pointer and nothing is added. A caller
  opts out for one call with `req.Header["Traceparent"] = nil`; `net/http` then writes no header.
  Only the canonical key is looked up.
- **T6** Any scope works: an HTTP request's, a WebSocket handshake's, a WebSocket message's
  (`c.Context()` of a `*ctx.WSContext`: the connection's trace ID), through any number of
  `context.WithValue` / `WithTimeout` / `WithCancel` wrappers, and after the request ended (a
  goroutine that kept the context, 0011 S8).
- **T7** Nothing else is added or forwarded: no `tracestate`, no `X-Request-Id` (0011 C3, Q4 of
  0011).
- **T8** Each `RoundTrip` call is handled on its own. A redirect followed by `http.Client` and a
  retry by the caller pass through the transport again: same trace ID, another span.
- **T9** The response and the error of `base` are returned unchanged.
- **T10** The returned value has a `CloseIdleConnections()` method that calls the same method of
  the base (of `http.DefaultTransport` for a nil base) when it has one, so
  `http.Client.CloseIdleConnections` keeps working through the wrapper. It does nothing when the
  base has no such method.

## Performance

Hot path: step 1 runs on every request, message and traced stage. Step 2 runs once per outbound
call, next to a network round trip.

**How it was measured (2026-11-17).** The review laptop (i7-9750H, 6 cores / 12 threads, Go 1.27.4,
darwin). It was not quiet (load average 3.5-6.5; spreads of ±10-20 % per row), so only same-run,
alternating comparisons count. Three levels: (a) the candidate alone in a tight loop; (b)
`BenchmarkServeHTTP_Static` and `_StaticParallel` of `core` with `-cpu 1,2,12`, 6 to 8 alternating
rounds, `benchstat`; (c) production settings as the roadmap defines them: a scratch program serves
Static requests from GOMAXPROCS goroutines after a warm-up and reports the process's CPU time
(user + system, `getrusage`) per request: 1,000,000 requests with the access log off (median of 9
alternating runs), 400,000 with it on (JSON to a file, lines counted, median of 5). Candidates
were prototyped in a copy of the repository; step 1 measures the real code the same way.

### Candidates

| # | Candidate | Result | Verdict |
|---|---|---|---|
| A | One locked read of 18 bytes instead of two (10 + 8), same mutex | alone: 59 → 37 ns on one goroutine; 50 / 86 / 122 → 36 / 54 / 69 ns with 1 / 2 / 12. In `ServeHTTP`: StaticParallel-12 689 → 632 ns (−8 %, p = 0.026). The mutex profile is unchanged: 21.1 s → 23.4 s of delay in `sync.(*Mutex).Unlock` under `tracecontext` in a 2 s run | taken (R3), but not enough alone: the contention stays |
| B | Sources in a `sync.Pool` (per P), one read | alone: 37 ns; 42 / 27 / 79 ns with 1 / 2 / 12. StaticParallel-12 689 → 538 ns (−22 %, p = 0.002); clean prototype 826 → 645 ns (−22 %, p = 0.004). No `sync.Mutex` entry left in the mutex profile. `NewSpanID` at 12 goroutines 132 → 52 ns, `NewTraceID` 156 → 59 ns (closes 0004 finding L2) | **recommended (H1)** |
| C | A source owned by each pooled `HTTPContext`, no lock, no pool | StaticParallel-12 689 → 538 ns (−22 %), the same as B | not taken: 512 B more per pooled context (it grows with concurrent requests, B with GOMAXPROCS), a new `NewRequest` parameter, and stage spans and WebSocket messages would still use the lock |
| D | 16 mutex shards picked by an atomic counter and `TryLock` | alone: 45 ns; 42 / 66 / 84 ns | not taken: slower than A |
| E | `crypto/rand.Read` directly, no buffer | alone: 318 ns; 386 / 729 / 858 ns | not taken (as in 0011) |
| F | Scope context and request copy in one allocation (`*x = *r.WithContext(ctx)` into a struct that holds both) | 2 → 1 allocation, 496 → 480 B alone; in `ServeHTTP` 7 → 6 allocations, 624 → 608 B; no time change that passes p < 0.05 on any CPU count | **not recommended (H3)** |
| G | The request copy kept in the pooled exchange (same trick, into the exchange) | 624 → 304 B, 7 → 6 allocations, GC cycles per million requests 214 → 102. Static −10.5 % (p = 0.002), StaticParallel −7 % / −10.5 % / −28 % at 1 / 2 / 12 | **not recommended (H2, Q1)**: unsafe |
| H | Copy the request only when a middleware or a handler parameter needs it | not built | rejected: `c.Request.Context()` in a guard or handler would lose the scope on routes without middleware (0011 Q2 chose "one rule, no silent miss"), and every production app binds global middlewares (the roadmap's checklist), so only benchmarks would gain |
| I | Pool the scope | not built | rejected: not safe. A goroutine may keep the context after the request (0011 S8), nothing tells when the last reader is gone, and 0014 writes fields into the scope |
| J | The scope in a per-connection context slot (`http.Server.ConnContext`) | not built | rejected: wrong for HTTP/2 (many requests per connection), breaks S8 on keep-alive, and `App` is an `http.Handler` any server may run |

### Production settings: process CPU per request (Static)

| Access log | GOMAXPROCS | Today | A (one locked read) | B (pool) | B + F | B + G |
|---|---|---|---|---|---|---|
| off | 1 | 1,396 ns | 1,292 | 1,300 | 1,301 | 1,296 |
| off | 2 | 1,557 ns | 1,451 | 1,477 | 1,424 | 1,223 (−21 %) |
| off | 12 | 3,478 ns (wall 670 ns) | 3,125 (579) | 4,003 (506) | 3,844 (476) | 2,949 (334) |
| on | 1 | 6,471 ns, 82 % of lines | 6,411, 83 % | 6,493, 83 % | 6,051, 79 % | 5,904, 76 % |
| on | 2 | 7,993 ns, all lines | 7,952 | 7,255 (−9 %) | 7,073 | 7,235 |
| on | 12 | 19,958 ns, all lines | 19,310 | 19,024 | 18,939 | 18,324 |

What the table says:

- **On 1 and 2 CPUs the generator's lock is not a cost** (0.0 and 1.4 ns of mutex wait per request)
  and no candidate but G moves the CPU per request beyond the noise. With the access log on, the
  log line is three quarters of the request and even G's gain disappears on 2 CPUs (7,255 against
  7,235 ns); on 1 CPU its lower figure comes with fewer lines written.
- **On 12 CPUs** today's mutex wait is 3.4 µs per request, summed over the waiting goroutines.
  With B it is 0.9 µs, all of it in the runtime's own locks (allocator and scheduler): the wall time
  per request drops 24 %, but the process CPU per request rises 15 %, because goroutines that used
  to sleep on the mutex now run and contend in the allocator for the 608 B each request leaves.
  Throughput is what all-core users see; the CPU figure is stated so step 15 does not read it as a
  regression.
- The remaining cost of the scope is its garbage (the 320 B request copy, the 160 B scope, the 64 B
  string), and only G removes a large part of it.

### Recommendation and budget

Take A and B, and drop the duplicate `traceparent` field (16 B). Keep the request copy and the
per-request scope.

| | Today | After step 1 |
|---|---|---|
| Static (core benchmark), 1 goroutine | 7 allocs, 624 B | 7 allocs, 608 B; time unchanged (p ≥ 0.19 on 1, 2 and 12 CPUs) |
| StaticParallel, GOMAXPROCS 12 | 826 ns | 645 ns (−22 %) |
| StaticParallel, GOMAXPROCS 1 and 2 | 1,741 / 1,082 ns | no significant change (p ≥ 0.33) |
| `NewRequest`, own trace | 236 ns, 2 allocs, 240 B | 245 ns (p = 0.5), 2 allocs, 224 B |
| `NewSpanID`, 1 goroutine | 56 ns | 61 ns (+5 ns: the pool's get and put cost more than a free mutex) |
| `NewSpanID` / `NewTraceID`, 12 goroutines | 132 / 156 ns | 52 / 59 ns |
| `sync.Mutex` delay under `tracecontext`, StaticParallel-12 | 21 s in a 2 s run | none |

The +1 allocation of roadmap 0004 stays, and its +528 B become +512 B.

**Targets for step 1** (same-run comparison against the commit before it):
1. `BenchmarkServeHTTP_StaticParallel` at GOMAXPROCS 12: at least 15 % less time per request, and
   no `sync.Mutex` frame under `internal/tracecontext` in its mutex profile.
2. `BenchmarkServeHTTP_Static` and `_StaticParallel` at GOMAXPROCS 1 and 2: no significant
   regression (`benchstat`, p < 0.05), 7 allocations, at most 608 B.
3. Process CPU per request at GOMAXPROCS 1 and 2, access log off and on with the lines counted: not
   higher than before beyond the run's spread. The figure for all cores is reported with the wall
   time next to it.
4. `NewSpanID` and `NewTraceID`: under 100 ns and 1 allocation, on 1 and on 12 goroutines.

**Idle and memory cost.** No goroutine, no timer, no wakeup. The static 512-byte buffer goes away.
The pool holds one 512-byte source per P that generated an ID recently (plus at most as many in
the pool's victim cache): 0.5-1 KiB on GOMAXPROCS 1, at most about 12 KiB on 12, and nothing after
two GC cycles without traffic. The first ID after that costs one 512 B allocation and one
`crypto/rand.Read` of 504 bytes (0.65 µs here). Per request: 16 B less than today.

**Step 2** (throwaway prototype, a stub base, a request with three headers):

| Case | Cost added by the wrapper |
|---|---|
| No scope (T2) | 5 ns, 0 allocs |
| Header already set (T5) | 20 ns, 0 allocs |
| Scope, own span (T3) | 0.70 µs, 6 allocs, 848 B: the request copy (320 B), the header copy, the value slice, the span ID and the 55-byte string |
| Scope, the request's span (Q4) | 0.59 µs, 5 allocs, 784 B |

Idle cost: none (the wrapper is one pointer). The copy is what the `RoundTripper` contract asks
for and is small next to a network call; nothing is pooled.

### Heavy techniques

- **H1, to approve: a `sync.Pool` of random sources** (`var sources sync.Pool` of `*source`,
  512 B each), replacing 0011 H1 (one buffer behind a mutex). Use: `readRandom` gets a source,
  reads, puts it back; no `defer`. Gain: candidate B above. Constraints: up to 504 unused random
  bytes per source stay in memory (identifiers, not secrets, as in 0011); the pool drops idle
  sources at GC, and under `-race` it drops them at random, so the order and refill-count tests
  move from the package functions to the `source` type (R1), where they are exact. Equality proof:
  `TestSource_StreamEqualsReader` on a `source`, plus R3's contiguity through the public
  functions.
- **H2, not recommended (Q1): the request copy inside the pooled exchange.** It relies on
  `(*http.Request).WithContext` being inlined so its copy stays on the stack. It is the only
  candidate that lowers the bytes (−320 B), but the `*http.Request` a middleware or handler holds
  would then be reused by the next request. Code that keeps it after the handler returns (a
  goroutine reading `r.Header`, the pattern of `http.TimeoutHandler`) would read another client's
  headers, `Authorization` included, and race. `net/http` never reuses a request, so nothing warns
  the user. The WebSocket handshake also keeps `c.Request` for the life of the connection
  (`core/app.go` `serveWS`, `ctx/ws_context.go` `connectionContext`), so that path would need its
  own copy.
- **H3, not recommended: scope context and request copy in one allocation** (the same inlining
  dependence). One allocation less, no measurable time, and once 0014 adds its 48 B to the scope
  the merged object is 512 B only if nothing else grows (`http.Request` is 304 B in Go 1.27;
  a larger one moves it to the 576 B class, above today's 320 + 208). A goroutine that keeps only
  the context would also keep the request's header map, URL and body alive.

**Not proposed:** `unsafe.String` over a byte array in the scope (saves the 64 B string, needs
`unsafe`); a `TryLock` fast path in front of the pool (about 5 ns on one goroutine); a user-space
generator seeded from `crypto/rand` (`math/rand/v2` ChaCha8), which would break 0011 G3.

## Concurrency
- No goroutine is added by either step.
- A `source` is owned by the goroutine between `sources.Get` and `sources.Put`; it is never shared,
  so it needs no lock. The generator takes no lock of its own and never holds a source while
  calling user code.
- A `Scope` stays one allocation per request, written before it is published and never reused.
  (0014 adds fields that `proxy` writes on the request's goroutine; see Coordination.)
- The transport holds only `base`, set once in `Transport`. `RoundTrip` reads the scope's immutable
  strings and never writes to the scope or to the caller's request (T4), so concurrent calls with
  one request context are safe.

## Security
- **0011's guarantees hold:** every ID bit that was random still comes from `crypto/rand` (R2); the
  format, the time prefix and the uniqueness rules are untouched; the request ID is always
  Owlky Go's (0011 S2); the trace ID a client controls is validated before it is stored (P1-P3).
- **Unused random bytes** now sit in up to one source per P instead of one buffer. They are
  identifiers, not secrets (0011 Security); a source is never read by two goroutines, so no ID
  repeats.
- **The transport sends the trace ID and a span ID to every host called with a request's
  context**, third parties and redirect targets included, as `httpclient` did (0011 Security). A
  caller that minds sets the key itself or opts out (T5). The value is built only from the scope's
  validated hex strings: no header injection. `tracestate` and the incoming `X-Request-Id` never
  leave (T7).
- **A client-chosen trace ID travels on.** A client that sends a valid `traceparent` picks the
  trace ID that outbound calls carry. That is what the format is for; it is 32 hex digits and
  authorizes nothing.
- **Bounded work:** the wrapper copies the caller's own headers once per call; nothing an inbound
  client controls sizes it.
- **H2 is refused on security grounds** (cross-request disclosure through a kept
  `*http.Request`).

## Impact
- **`internal/tracecontext`:** `id.go` (the `source` type, the pool, one 18-byte read), `scope.go`
  (`NewRequest` and `NewMessage` use it; `Scope.traceparent` removed; `ChildTraceparent`).
  `id_test.go`: `TestSource_StreamEqualsReader`, `TestSource_ReadsInBlocksOf512OnDemand` and
  `TestNewTraceID_TailIsReaderStream` assert an exact global order and refill count; with a pool
  under `-race` they fail (measured: 44 refills where 3 are expected), so they are rewritten as the
  test plan says. README: the generator and performance sections.
- **`trace`:** the new file; the package now imports `net/http` and `internal/tracecontext` (no
  cycle: `tracecontext` imports neither `trace` nor `core`). README: a section "Outbound calls".
- **`ctx`, `core`:** no code change. `Init` and `ServeHTTP` stay as they are.
- **`modules/httpclient`:** untouched here; step 12 removes it and moves `sample/benchmarks` to
  `net/http` with `trace.Transport`.
- **Compatibility:** none broken. The receiving service's `upstream` changes meaning if T3 stays
  (Q4).
- **Docs (step 14):** `internal/tracecontext/README.md`, `trace/README.md`, the main README
  (outbound calls), `.owlky/package-reference.md` (the `tracecontext` line says "one 512-byte
  `crypto/rand` buffer behind a mutex"), `.owlky/request-pipeline.md` (outbound propagation).
  `CLAUDE.md` needs no change for these two steps.

## Test plan

Internal test packages, one function per behavior.

`internal/tracecontext/id_test.go`
- R1 `TestSource_StreamEqualsReader` (a `source` value, stubbed reader: 151 reads of 8 bytes equal
  the reader's stream in order across refills), `TestSource_RefillDropsLeftover` (63 reads of 8
  bytes use one refill, the 64th triggers the second), `TestSource_LazyFill` (a zero `source`: no
  reader call before the first `read`), `TestSource_ReadLargerThanLeft` (an 18-byte read with 10
  bytes left refills and returns the first 18 bytes of the new block).
- R2 the existing `TestNewSpanID_NoDuplicateIn10M`, `TestNewTraceID_NoDuplicateIn10M`,
  `TestNewSpanID_ParallelUnique`, `TestNewTraceID_ParallelUnique` (unchanged, `-race`);
  `TestNewSpanID_BytesAreContiguousInStream` and `TestNewTraceID_TailIsContiguousInStream`
  (counting reader: the bytes of one ID are consecutive stream values; no assertion on which block).
- R4 `TestNewSpanID_NeverZero` (existing), `TestNewRequest_RedrawsZeroRequestID`,
  `TestNewRequest_RedrawsZeroTraceTail` (a reader that returns zeros first).
- R5 `TestSource_EveryReaderCallIs504Bytes` (through `NewSpanID`, `NewTraceID`, `NewRequest`,
  16 goroutines).
- Benchmarks: the existing `BenchmarkNewSpanID`, `BenchmarkNewTraceID` and their `_Parallel` twins.

`internal/tracecontext/scope_test.go`
- R3 `TestNewRequest_OneReadOf18Bytes` (counting reader: the request ID's bytes follow the trace
  tail's), `TestNewRequest_ContinuedTraceReads8Bytes` (the trace ID is the header's, the request ID
  is contiguous), `TestNewMessage_UnderConnectionReads8Bytes`, `TestNewMessage_WithoutConnectionReads18Bytes`.
- R6 the existing S2-S6, W4, L1 tests pass unchanged; `TestScope_TraceparentIsHeaderValue`
  (`Traceparent()` equals the second slice of `Headers()`).
- R8 `TestNewRequest_TwoAllocations`.

`ctx`, `core`
- R7 the existing `TestHTTPContext_Init_RequestCarriesScope`, `TestScope_SameInEveryLayer`,
  `TestScope_GoroutineAfterRequest`, `TestScope_ConcurrentRequestsIsolated` pass unchanged.
- R8 the existing `TestServeHTTP_StaticRouteAllocations`.
- Benchmarks: `BenchmarkServeHTTP_Static`, `_StaticParallel`, `_StaticWithListener` with
  `-cpu 1,2,12`, and the mutex profile of target 1.

`trace/http_transport_test.go`
- T1 `TestTransport_NilBaseUsesDefaultTransport` (swap `http.DefaultTransport` after `Transport(nil)`
  was called: the new one receives the request), `TestTransport_ConcurrentUse` (`-race`, 16
  goroutines, one request context).
- T2 `TestTransport_NoScopeSameRequest` (pointer identity), `TestTransport_NoScopeNoAllocation`
  (`testing.AllocsPerRun` = 0), `TestTransport_BackgroundContext`.
- T3 `TestTransport_AddsTraceparent` (trace ID and flags of the scope; the value parses),
  `TestTransport_OwnSpanPerCall` (two calls differ, neither is the request ID),
  `TestTransport_FlagsOfContinuedTrace` (incoming flags `00` and `01`), `TestTransport_KeepsOtherHeaders`.
- T4 `TestTransport_CallerRequestUnchanged`, `TestTransport_NilHeader`.
- T5 `TestTransport_ExistingHeaderWins`, `TestTransport_InvalidExistingHeaderKept`,
  `TestTransport_EmptyKeyOptsOut`.
- T6 `TestTransport_MessageScope`, `TestTransport_DerivedContext` (`WithTimeout` over `WithValue`),
  `TestTransport_ContextKeptAfterRequest`; in `core`: `TestTransport_FromHandlerContext` (a handler
  calls an `httptest.Server` with `c.Context()`: the server sees the response's trace ID).
- T7 `TestTransport_AddsNothingElse`.
- T8 `TestTransport_RedirectKeepsTraceNewSpan` (`httptest.Server` that redirects once).
- T9 `TestTransport_ReturnsBaseResponseAndError`.
- T10 `TestTransport_CloseIdleConnectionsForwarded`, `TestTransport_CloseIdleConnectionsBaseWithoutMethod`.
- Benchmarks: `BenchmarkTransport_NoScope`, `_ExistingHeader`, `_Scope`.

## Open questions

1. **Q1 The request copy stays (R7), so one or two CPUs gain nothing.** The only candidate that
   lowers the per-request cost on a small machine is H2 (−320 B, −21 % CPU on 2 CPUs with the
   access log off, nothing measurable with it on). It makes a kept `*http.Request` unsafe.
   Recommendation: do not take H2; accept that step 1's result is the all-cores scaling and 16 B,
   and that the scope's remaining cost is the price of 0011 Q2.
2. **Q2 H1: a `sync.Pool` of sources, or the mutex with one read (candidate A alone).** A alone
   needs no new heavy technique and is about 5 ns cheaper per lone ID on one goroutine, but the
   roadmap's "no lock contention in the parallel profile" is not met with it. Recommendation: H1.
3. **Q3 H3 (one allocation for the scope and the request copy).** Recommendation: no; one
   allocation less, no measured time, and it depends on the size of `http.Request`.
4. **Q4 Which span the outbound header names.** T3 follows the roadmap: each call gets its own
   span ID, whose parent is the request. But no event or log line records that span, so "parent =
   the scope" is observable nowhere, and the receiving Owlky Go service logs an `upstream` that
   matches nothing on our side (only `trace_id` joins the two logs). 0011 C1 sent the request's own
   span (`Scope.Traceparent()`): the receiver's `upstream` is then our `request`, the call costs one
   allocation and 64 B less, and `ChildTraceparent` is not needed. That is valid W3C for a caller
   that records no client spans. Recommendation: send the request's span now (T3 becomes "the value
   is `Scope.Traceparent()`", T8 "the same value", `TestTransport_OwnSpanPerCall` becomes
   `TestTransport_SpanIsRequestID`), and move to a span per call when outbound calls are emitted
   as trace events (the HTTP client roadmap). This changes step 2's "its own span" wording.
5. **Q5 The name `trace.Transport`.** The package already uses "transport" for HTTP or WS
   (`TransportHTTP`, `TransportWS`, `Event.Transport`). The function does not collide and reads
   well where it is used (`http.Client{Transport: trace.Transport(nil)}`); `trace.RoundTripper` or
   `trace.Propagate` would avoid the double meaning. Recommendation: keep `Transport` (the roadmap's
   name) and say in the README which is which.
6. **Q6 Numbers from a loaded darwin laptop.** `crypto/rand` on Linux (getrandom, through the vDSO
   on recent kernels) was not measured; the buffer keeps its cost off the request either way.
   Recommendation: accept; step 15 measures on the target settings.
7. **Q7 Status line.** `docs/design/README.md` defines `Proposed`; this file says `Draft` as the
   roadmap run asked while 0014 and 0015 are written in parallel. Switch to `Proposed` once the
   three are reconciled.

## Coordination

**With 0014 (middlewares).** 0014 keeps the resolved client (`ClientIP`, `ClientHost`,
`IsClientHTTPS`, `IsClientSet`, about 48 B) in the request's `tracecontext.Scope`, and the access
log reads it from `trace.Event.ClientIP` on `complete`. What 0013 guarantees for it:
1. The scope stays reachable from the `*http.Request` a middleware receives through
   `tracecontext.ScopeOf(r.Context())`, without allocation, for requests and WebSocket handshakes:
   the request copy stays (R7).
2. One scope per request, allocated zeroed in `NewRequest`, never pooled, shared or copied (R7,
   candidate I rejected), so `proxy` can write its fields on the request's goroutine. With the
   `traceparent` field removed the scope allocation is 160 B; 0014's fields make it 208 B, not
   224 B.
3. 0013 does not change `completeEvent`, `ServeHTTP` or `core.responseWriter`, so 0014's
   `ClientIP` line and its hook calls meet no conflict there. If Q1 were answered with H2,
   `c.Request` would point into the exchange; `proxy.ClientIP(r)` would not change.
4. Both designs add to package `trace` (0013 a function in `http_transport.go`, 0014 the field
   `Event.ClientIP`) and to `Scope` (0013 removes one unexported field, 0014 adds four exported
   ones). Step 1 lands first, as the roadmap orders.
5. `trace.Transport` reads only the scope's trace ID, span ID and flags, which never change after
   `Init`; it does not read 0014's fields and forwards no `Forwarded` / `X-Forwarded-*` header.
6. 0011's contract called the scope immutable. With 0014 it has fields written after `Init`:
   the README of `internal/tracecontext` says which fields those are (0014 owns that text).

**With 0015 (config and cache).** No shared type or file. Two shared rules: 0013 adds no goroutine,
timer or fixed buffer (the pool is empty when idle), in line with the sweeper 0015 removes; and the
process-CPU harness described under "How it was measured" is the method step 15 reuses for every
module, so 0015's figures should be stated the same way (GOMAXPROCS 1, 2 and all, work counted).

## Decisions (2026-11-17)

Approved by the user. All open questions answered as recommended: no pooled request copy; the random source pool (H1) approved; no merged allocation; outbound `traceparent` carries the request's span; the name `trace.Transport` kept. The user's condition: the public API stays as small and simple as the Product principles require, and every name is documented clearly (the docs will feed a website later).
