# 0004: Trace context and logging

Status: Approved

## Goal
Every log line written while Ginject serves something can be tied to the request (or WebSocket
connection and message) it belongs to, with IDs in the W3C Trace Context format that any log
library, gateway or other service can read. Logging never makes a request wait for I/O, holds a
bounded amount of memory, and a panic flood can't flood the log.

## Scope
- In: request and trace IDs (`internal/crypto` and `ctx`), `traceparent` in and out, span IDs on
  `trace.Tracer` events, the scope in `context.Context` for every trigger point (HTTP request, WS
  handshake, WS message), the logger API (`From`), the asynchronous log pipeline (`log`,
  `accesslog`), panic stack sampling (`core`), `modules/httpclient` forwarding `traceparent`, the
  READMEs and the knowledge base.
- Out: an OpenTelemetry SDK or exporter module (not planned: Ginject only follows the W3C format);
  the devtools sink (the devtools roadmap adds it to the pipeline); rewriting the WebSocket transport
  (own implementation, later roadmap); `span_id` per stage on log lines (later, only if measured
  worth it).

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Trace context IDs: one generator (`crypto/rand`, honest package name) for a 128-bit trace ID (time-ordered prefix) and 64-bit span IDs, W3C format; the request ID is ours, always (`X-Request-Id`); a valid incoming `traceparent` continues its trace ID, an invalid one is ignored; `traceparent` on the response; WS connection ID = the handshake's request ID (no more UUID), each message its own ID with the connection as parent | tdd | docs/design/0011-trace-context.md | - | IDs match the W3C grammar; no duplicate in 10M; parallel generation unique; hostile `traceparent` values ignored; benchmarks under 100 ns | todo |
| 2 | Scope in `context.Context`: every trigger point puts its scope (trace, span, request, connection, message, upstream) in the context it hands out; `Context()` joins the `ctx.Context` interface; `modules/httpclient` forwards `traceparent` from a request's context | tdd | docs/design/0011-trace-context.md | 1 | middleware, guard, interceptor, filter, handler and a service called with `c.Context()` all see the same scope; a goroutine holding it after the request still reads its own IDs | todo |
| 3 | Spans on trace events: span ID and parent on every `trace.Tracer` event (one span per stage, the request as root), generated only while a listener exists | tdd | docs/design/0011-trace-context.md | 1 | the event tree of a request rebuilds the lifecycle order; 0 extra cost with no listener | todo |
| 4 | Logger API: the injected `common.Logger` gains `From(ctx)` (same logger plus the scope's fields as key-value arguments, readable by any log library); what `SetLogger` accepts stays the 5-method interface adapters implement | tdd | docs/design/0012-log-pipeline.md | 2 | `s.Logger.From(ctx).Info(…)` carries the IDs with the built-in logger and with an adapter (zap-shaped test double); existing `s.Logger.Info(…)` calls unchanged | todo |
| 5 | Asynchronous log pipeline: encode on the caller, one bounded writer per output, batched writes, drop by level under overload (Warn and above kept, a bounded wait when full), drops counted and reported, flush after the shutdown hooks and before `Fatal` exits; the access log writes through it | tdd | docs/design/0012-log-pipeline.md | 4 | a stuck stdout never blocks a request; memory stays under the design's bound under load; parallel logging scales across cores | todo |
| 6 | Panic stack sampling: at most 10 stacks per second per app, the rest logged without a stack, a `stacks_suppressed` line per second | tdd | docs/design/0012-log-pipeline.md | 5 | a panic flood costs a bounded amount of log and CPU | todo |
| 7 | READMEs (`log`, `accesslog`, `trace`, `core`, `ctx`, `modules/httpclient`) and the knowledge base (`/kb update`) | light | - | 1-6 | every new name documented; KB matches the code | todo |
| 8 | Measure: request cost with and without a listener, log call cost (1 and 12 goroutines, fast and slow reader), pipeline memory bound, panic flood; `docs/review/http-0004.md` | light | - | 1-7 | the review file, compared with `http-0003.md` | todo |

## Decisions
- **W3C Trace Context, no OpenTelemetry dependency:** Ginject follows the format (trace ID, span ID,
  `traceparent`) so gateways, other services and any log library can read the IDs; no SDK and no
  exporter module.
- **The request ID is always Ginject's own;** an incoming `traceparent` only continues the trace ID.
  A client can never choose the request ID.
- **Logger API:** `s.Logger.From(ctx)` on the injected logger (no free function, no `ctx.Log()`,
  no `Info(ctx, …)` on every call); outside a request `s.Logger.Info(…)` as today. The logger only
  knows `context.Context`; every Ginject context exposes one through `Context()`; the pooled
  `HTTPContext` itself is never offered as a context.
- **IDs reach other log libraries as key-value arguments** (`trace_id`, `request`, `connection`,
  `message`…), so an adapter needs nothing new. Field names are fixed in design 0011.
- **Log pipeline:** as described in Background below (moved from the backlog):
  bounded in bytes, one writer per output, no user option.
- **Panic stacks:** 10 per second per app; rate limiting stays an opt-in middleware
  (`guards/throttler`).
- **Small machines first** (CLAUDE.md › Product principles): every queue, buffer and goroutine this
  roadmap adds is fixed-size and sized for a small server (one or two vCPUs, a few hundred MiB);
  idle cost (memory, goroutines, wakeups) is stated and kept near zero; nothing runs or allocates
  when unused (no listener, no log call).
- **Numbering:** the roadmap planned as 0004 (middlewares, guards, built-in modules, security audit)
  becomes 0005; WebSocket becomes 0006.
- Designs: one architect pass, two design files (0011 trace context: steps 1-3; 0012 log pipeline:
  steps 4-6).

## Background (decided 2026-11-16, moved from the backlog)

### Panic stacks and rate limiting (roadmap 0004)

Decided 2026-11-16 (review finding L1 of roadmap 0003): a non-exception panic costs about 26 µs and
a 4 KiB stack written synchronously, so a client that can trigger one can flood the log.

- **Stack sampling:** the first 10 stacks per second per app are logged; after that the panic's
  value and request ID are logged without the stack, and one `stacks_suppressed` line per second
  reports the count. No user option.
- **Rate limiting stays a middleware** (`guards/throttler`), opt-in, for flexibility: not on by
  default. The README's production section says to turn it on (or limit at the proxy).
- The synchronous log write itself is reviewed with the log pipeline below.

### Asynchronous log pipeline (roadmap 0004; devtools reuses it)

Decided 2026-11-16. Today every log line is formatted and written to stdout synchronously under one
mutex: about 2.1 µs a line (9.4 µs with a slow reader), no scaling across cores (about 470k lines/s
for the whole process), and a stuck stdout hangs every request that logs while blocked goroutines
pile up without bound.

- **Request side:** level check, resolve, **mask**, encode once (values can change later), request
  ID attached, then push the bytes to a bounded queue. Never waits on I/O.
- **One writer per output** (not per logger), so loggers writing to stdout share one queue and keep
  their order. Lines of one goroutine stay in call order; across goroutines the timestamp and request
  ID are the order (as in every logger).
- **Batching:** the writer drains everything queued into one write (about 64 KiB batches).
- **Bounded memory:** a line is capped (about 64 KiB, truncated with a marker), the queue is bounded
  in bytes (about 4 MiB), oversized buffers are not pooled: a fixed few MiB whatever the load.
- **Overload:** above about 80 % full, Debug and Info are dropped; Warn, Error and panics are kept;
  when full, Error and panics wait at most about 100 ms, then are dropped. Drops are counted and
  reported once per second, per level. The panic stack sampling above lives here.
- **Flush:** after the shutdown hooks, synchronously before `Fatal` exits.
- **The access log** writes through the same writer (no worker of its own).
- **Sinks:** stdout only in 0004; each sink has its own queue, so a slow sink never slows another.
  The devtools roadmap adds a devtools sink (batched by time or size, compressed, bounded retry with
  backoff, offline drops counted and reported on reconnect). Masking always happens before any sink.
- **No user option;** custom loggers from `SetLogger` are called directly as today. Exact sizes and
  thresholds are measured in the design. Heavy techniques to approve there: `sync.Pool` for line
  buffers and the hand-managed batch buffer (no lock-free ring unless measured necessary).

### Request-scoped logging (roadmap 0004)

Decided 2026-11-16. User logs carry no request ID today, so they can't be tied to the request that
wrote them (debugging from an `X-Request-Id`, and the devtools timeline of a request).

- **API:** the injected logger gains one method: `s.Logger.From(ctx context.Context)` returns the
  same logger with the scope's fields (request, connection, message…). Outside a request,
  `s.Logger.Info(…)` as today; nothing breaks. The name `From` is final unless the design finds a
  clash.
- **Two interfaces:** what `SetLogger` accepts stays the 5-method interface that adapters (zap,
  zerolog…) implement; the injected `common.Logger` adds `From` and is implemented by the
  framework's wrapper only (the `WrapLogger` layer `SetLogger` already applies).
- **Transport-free:** the logger only knows `context.Context`. Every trigger point (HTTP request,
  WebSocket handshake and message, a future `SCHEDULE_…`) puts its scope into a `context.Context`,
  and every Ginject context exposes it through `Context()` (added to the `ctx.Context` interface).
  Passing the pooled `HTTPContext` itself as a context is not offered: it is reused after the request.
- **Scope model:** `request` (HTTP); `connection` (= the handshake's request ID) and `message` (WS);
  `upstream` (a validated incoming `X-Request-Id` or `traceparent`, never replacing our own ID);
  `cause` (what published a broker message). `httpclient` forwards the ID.
- **Known gap:** forgetting `From` inside a request logs without the ID (not caught by the compiler);
  docs, the developer MCP and devtools ("log outside a scope") cover it.
- Rejected: `ctx.Log()` (no middleware or service layer), a goroutine-ID scope (assembly, lost in
  child goroutines), copy-on-call injection (silent misses, cost per layer), `Info(ctx, …)` on every
  call (breaking, noisy).

## Log
- 2026-11-16: roadmap approved by the user.
