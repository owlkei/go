# Backlog

Decided work that has no roadmap yet. A roadmap that takes an item links back to it and removes it
from here.


## Order to the first release (decided 2026-11-17)

1. 0005: middlewares, modules and security.
2. Public API review (leaves room for the render route kind and auth).
3. Server-side rendering; the court booking app starts right after, on `database/sql` and a minimal
   auth, and drives the next roadmaps.
4. Authentication and authorization (sessions for SSR, tokens for REST, roles and permissions as
   guards).
5. Storage: adapters over existing databases (SQLite, Postgres), not an engine of our own; Ginject
   adds connections, transactions, migrations, pool bounds, the circuit breaker, trace and devtools.
6. HTTP client (timeouts, bounded retries, circuit breaker).
7. 0.x release once the court booking app runs for real.
8. After the release: WebSocket (0006), devtools and MCP.

## Own WebSocket implementation (after HTTP is production ready)

Decided 2026-11-16: Ginject replaces `golang.org/x/net/websocket` (its last dependency; its own
documentation points to other packages) with an implementation of its own, once the HTTP roadmaps
(0003 to 0005) are done. It becomes part of the WebSocket roadmap (0006).

- **Why:** no dependency left in `go.mod`; hot-path control by the rules of `.ginject/conventions.md`
  (buffer reuse, low allocations); backpressure, limits, trace and devtools hooks at the frame
  level; ownership of every line.
- **Scope, phase 1 (RFC 6455):** the handshake through `http.Hijacker`; framing (masking, 7 / 16 /
  64-bit lengths), fragmented messages, control frames (ping, pong, close) and the close handshake;
  UTF-8 validation of text messages; frame and message size limits; read and write deadlines.
- **Scope, phase 2 (RFC 7692):** `permessage-deflate` with `compress/flate`, with a decompressed-size
  limit (decompression bombs). Phase 2 may come later than phase 1.
- **Verification:** the Autobahn TestSuite on CI (Docker; every core case passes before the
  implementation counts as done), fuzz tests of the frame parser (no panic, bounded memory), and
  benchmarks against `coder/websocket` and `gorilla/websocket` in `bench/`.
- **Joins the remaining WebSocket work:** acks, backpressure, limits, hooks, shutdown, broker.

## WebSocket framework lines without the connection ID (WebSocket roadmap 0006)

Found 2026-11-16 in the review of roadmap 0004: the framework's own WebSocket error lines
(`WSReadFailed` in `core/read_loop.go`, `WSWriteFailed` in `core/write_loop.go`, `WSPingSendFailed`
in `core/ws.go`) are written through `ws.logger`, not through `log.From` on the connection's
context, so they carry no `trace_id` or `connection`. The WebSocket roadmap logs them through the
connection scope.

## Public API surface (after roadmap 0005)

Decided 2026-11-16 (Product principles in `CLAUDE.md`): Ginject exports about 607 functions, types and
methods outside `sample/` and `bench/`, most of them framework internals (`common.Construct`,
`BuildHTTPInterceptMiddleware`, the `common.InsertedRoutes` global, `routing` internals,
`ctx.BinderOf`, the devtool builder). A dedicated roadmap, after 0005 and before WebSocket: review
every exported name, move internals to `internal/` or unexport them, collapse duplicate ways of doing
one job, and rewrite the docs around the resulting surface.

## MCP (with the devtools platform)

Decided 2026-11-16: Ginject gets MCP support when development of the devtools platform starts, in
three forms:

1. **A developer MCP:** tools AI coding agents query to write correct Ginject code (concepts, the
   route DSL, injectable types, examples, scaffolding of modules / controllers / guards, a handler
   signature check, explanations of bootstrap panics). Lives outside the framework's code.
2. **A runtime MCP module** (for example `modules/mcp`, `app.EnableMCP()`): an app's routes exposed as
   MCP tools, with input schemas from the DTOs (the devtool snapshot already computes them), the
   Streamable HTTP transport, auth through guards, nothing exposed unless chosen, rate limits.
   Implemented by Ginject itself (JSON-RPC over HTTP), to keep `go.mod` free of dependencies.
3. **A devtools MCP:** AI agents read a running app through the devtools platform (routes, DI graph,
   traces of failed requests).

## Heavy techniques waiting for approval

Designed and measured in the roadmap 0002 designs, not built (each needs the maintainer's approval
per use). Details and expected gains: `docs/review/http-0002.md` › What is left.

| Design | Technique |
|---|---|
| 0003 H1 | one scratch buffer and a kept JSON encoder per pooled context |
| 0003 H2 | shared read-only header value slices |

## Log writer batching delay (measured in roadmap 0004)

On two or more CPUs the log writer is woken for almost every line: an access line costs about 3 µs
more CPU on 2 CPUs than on 1 (`docs/review/http-0004.md`, follow-up). A short batching delay (the
writer waits up to about 1 ms or 16 KiB before writing, a timer armed only while the queue is not
empty, Fatal and shutdown flush at once) would remove most of it. Costs: up to about 1 ms of log
latency and one more state in the sink. Needs a design addendum to 0012.

## New storage and HTTP client modules (after roadmap 0005)

Decided 2026-11-17: the old `modules/storage` (embedded append-only document store, known data-loss
risks in transactions and compaction) is removed in roadmap 0005. A new storage module gets its own
roadmap, starting with requirements: an embedded engine of our own or adapters over existing
databases (SQLite, Postgres…), memory and disk bounds for small machines, crash safety proven by
injected crashes, and the API surface (one obvious way).

`modules/httpclient` is removed in 0005 too and rewritten in the same roadmap (requirements first:
timeouts, response size limits, retries only for idempotent requests, redirect bounds, SSRF guards
for user-supplied URLs). Until then apps use `net/http` with `trace.Transport` (0005 step 2).

Circuit breaker (decided 2026-11-17): part of the new HTTP client, after its default timeouts and
bounded retries (idempotent requests only). Opt-in, per host, as an `http.RoundTripper` so any
client built on `net/http` can use it; closed / open / half-open with a failure-rate threshold over
a window; fails at once while open. A few atomics per call, nothing while idle, state readable by
the devtools (state, failure rate, next probe). Not urgent: timeouts prevent most cascading
failures, and `loadshed`'s adaptive limit (0005) already protects the server when a dependency
slows down; the breaker adds failing fast and giving the dependency time to recover. A storage
module over an external database may reuse it for its connections.

## Timeout interceptor (with the interceptor review)

Decided 2026-11-17 (roadmap 0005 discussion): a timeout per route or controller is a built-in
interceptor, not a middleware.

- The timeout is a `RequestTimeoutException` that goes through the exception filters (same JSON as
  every other error, customizable), unlike a middleware error (the fixed HTTP-layer response).
- It wraps WebSocket messages and future trigger points (scheduler) the same way: one obvious way.
- The server's timeouts (0003) already bound the whole request, middlewares and guards included.
- Hard part for the design: Go can't stop a handler goroutine, so the timeout is cooperative (a
  deadline on `c.Context()`). Answering early while the handler still runs must never let it write
  into a pooled `HTTPContext` reused by another request: either the context returns to the pool only
  after the handler returns, or the response writer is detached from the pooled context.

## Real app phase: court booking SaaS (after the 0.x alpha)

Decided 2026-11-17: a real app built with Ginject + Owlky (no devtools needed yet), to validate the
framework from the user's side before 1.0. Idea: booking for pickleball / badminton / mini football
courts (SaaS for court owners). Keep it small: one court type, one payment gateway, one 1 vCPU VPS.

- Owner side: court schedule grid, revenue and peak hours charts, regular customers, court photos.
- Player side: public booking page, free slots, slots held by others shown live (WebSocket),
  payment through a local gateway (webhook).
- What it exercises: auth and multi-tenancy, double-booking races, payment webhooks and outbound
  calls (timeouts, circuit breaker), public pages (`csrf`, `helmet`, throttler, `loadshed`),
  WebSocket.
- Storage and auth don't exist yet: use `database/sql` (SQLite or Postgres) and a simple auth; the
  pain points become the requirements of the storage and auth roadmaps.
- Later, a light web analytics app as a real-world performance test.

## Server-rendered HTML controllers (before the first release)

Priority: one of the two focuses of Ginject (CLAUDE.md › Product principles). Planned right after
the public API review (which must design the render route kind), before the 0.x alpha; the court
booking app (real app phase) is built with it.

Decided 2026-11-17: Ginject renders HTML pages on the server, so an app built with Owlky's
framework-free HTML version needs no separate JSON API plus a React client (cheaper on small
machines: no client bundle, no second app). Details are discussed when the roadmap starts. Known
points to cover:

- A controller route kind that renders a template with the handler's data (the route DSL, so the
  public API review must leave room for it).
- `html/template` (contextual autoescaping) as the engine; layouts and partials; templates from an
  `embed.FS` in production, reloaded from disk in development; parsed once and cached.
- Owlky's HTML snippets and CDN assets (`owlky.css`, the loader) used directly in templates; the CSP
  from `helmet` must allow them (nonces or hashes for inline scripts).
- Errors and 404 pages as HTML for page routes, JSON for API routes.
- Forms: `csrf` (0005) already protects them; validation errors rendered back into the form.
- Middleware refusals (`csrf` 403, `hostcheck` 421, `loadshed` 503) run before routing, so they
  pick HTML or JSON from the `Accept` header, not from the route.
- CSP: per-request nonces for inline scripts; Owlky's assets embedded in the binary and served from
  the app's own origin (`'self'`).

## WebSocket bugs found while documenting (roadmap 0005, for 0006)

Found 2026-11-17 while rewriting the main README (not fixed; the WebSocket roadmap owns them):
- `EnableWS(nil)` crashes on a nil pointer instead of a bootstrap panic.
- `EnableWS` writes into the caller's `*WSConfig`: two apps sharing one config overwrite each other.
- The goroutine that drops dead WebSocket connections never stops, even after `Stop()`.
- `Publisher.Publish` silently drops every payload after the first.
