# Backlog

Decided work that has no roadmap yet. A roadmap that takes an item links back to it and removes it
from here.

## Order to the first release (decided 2026-11-17, updated 2026-11-21)

Done: middlewares, modules and security (0005), the public API review (0007), server-side rendering
(0008), the startup lifecycle (0009), app options (0013), the security roadmap (0012), the rebrands
(0010, 0014), the examples (0011).

Next:
1. Authentication and authorization (sessions for SSR, tokens for REST, roles and permissions as
   guards). The court booking app starts once it lands, on `database/sql`, and drives the next
   roadmaps.
2. Storage: adapters over existing databases (SQLite, Postgres), not an engine of our own; Owlkei Go
   adds connections, transactions, migrations, pool bounds, the circuit breaker, trace and devtools.
3. HTTP client (timeouts, bounded retries, circuit breaker).
4. 0.x release once the court booking app runs for real and `go.owlkei.com` serves the module.
5. After the release: WebSocket, devtools and MCP.

Not ordered yet, before the release: Validation of DTOs, the low findings of audit 0012, accurate trace
timings.

## Owlkei platform (from the rebrand, 2026-11-19)

Owlkei is one brand with independent products: **Owlkei UI** (the component library) and **Owlkei Go**
(this framework); later Owlkei CLI, Devtools, SDK and Studio. The renames are done (roadmaps 0010 and
0014; the repositories live at `github.com/owlkei/go` and `github.com/owlkei/ui`).

- **Vanity import path (before publishing, owner's checklist):** `go.owlkei.com/*?go-get=1` answers
  `<meta name="go-import" content="go.owlkei.com/owlkei git https://github.com/owlkei/go">` (a DNS
  record `go` to the website's host, a small middleware in the Next.js site, browsers redirected to the
  Owlkei Go docs). Until then a `replace` in `go.mod` works (README › Installation).
- **Independence:** UI works with any backend, Owlkei Go with any frontend or JSON only; they meet at the
  `data-ok-*` markup, never import each other. owlkei.com is the one shared platform.
- **Devtools:** the collector stays here (`internal/devtool`, `AppOptions.Devtool`); the interface lives
  at `owlkei.com/devtools` (built with Owlkei UI, source next to the website). The browser fetches the
  snapshot from the local app directly (CORS + `Access-Control-Allow-Private-Network` for the owlkei.com
  origin only, a pairing token printed at start, off in production); only the paid tier sends it to
  owlkei.com.

## Own WebSocket implementation (after HTTP is production ready)

Decided 2026-11-16: Owlkei Go replaces `golang.org/x/net/websocket` (its last dependency; its own
documentation points to other packages) with an implementation of its own, once HTTP is production
ready. It becomes part of the WebSocket roadmap.

- **Why:** no dependency left in `go.mod`; hot-path control by the rules of `.owlkei/conventions.md`
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

## WebSocket framework lines without the connection ID (for the WebSocket roadmap)

Found 2026-11-16 in the review of roadmap 0004: the framework's own WebSocket error lines
(`WSReadFailed` in `internal/core/read_loop.go`, `WSWriteFailed` in `internal/core/write_loop.go`, `WSPingSendFailed`
in `internal/core/ws.go`) are written through `ws.logger`, not through `log.From` on the connection's
context, so they carry no `trace_id` or `connection`. The WebSocket roadmap logs them through the
connection scope.

## WebSocket bugs and limits (for the WebSocket roadmap)

Found 2026-11-17 while rewriting the main README, and later:
- The goroutine that drops dead WebSocket connections never stops, even after `Stop()`.
- `Publisher.Publish` silently drops every payload after the first.
- Found 2026-11-18: acks and errors go through `TrySend` on the 32-slot send buffer, so a client that
  receives many events loses its acks silently (`TestWS_ConcurrentConnectionsIsolated` times out under
  load: 8 clients × 20 publishes fan out 1 280 events). Control frames need their own path or a
  blocking send with a deadline.
- Limits left out of App options (0013): `WS.MaxConnectionsPerIP`, message sizes (`ByteSize`) and
  message rates, as `pre-route` stages of the handshake and of each message (`connections`,
  `message-size`, `message-rate`). Today a frame may be 32 MiB, the `x/net/websocket` default (audit
  0012, L8).
- `WSOptions.globalMiddlewares` is never set outside tests since 0013 (WebSocket tests still set it):
  remove it or wire it.

## Glob topic syntax in the WebSocket layer (for the WebSocket roadmap)

Decided 2026-11-18: `pattern` now uses the glob syntax of `main`'s D-030 (`*` one segment, a trailing
`**` one or more). The WebSocket layer follows only partly, and the owner accepted the break until
the WebSocket roadmap:
- `SUBSCRIBE_chat_ANY` still registers `chat.*`, now one segment after `chat` (was one or more), and
  `SUBSCRIBE_ANY` registers `*`, now single-segment topics only (was every topic). The method-name
  DSL needs a token for `**`: decided 2026-11-18, the token is `ALL` (`SUBSCRIBE_chat_ALL` → `chat.**`,
  `SUBSCRIBE_ALL` → `**`).
- Topics from clients and `Publish` are not validated: reject empty segments (`a..b`, `b.`), limit
  segment characters to `A-Z a-z 0-9 _ -` and bound the length, so the edge cases of
  `pattern/README.md` › Edge cases never reach the matcher.

## MCP (with the devtools platform)

Decided 2026-11-16: Owlkei Go gets MCP support when development of the devtools platform starts, in
three forms:

1. **A developer MCP:** tools AI coding agents query to write correct Owlkei Go code (concepts, the
   route DSL, injectable types, examples, scaffolding of modules / controllers / guards, a handler
   signature check, explanations of bootstrap panics). Lives outside the framework's code.
2. **A runtime MCP module** (for example `modules/mcp` with an `AppOptions.MCP` group): an app's routes exposed as
   MCP tools, with input schemas from the DTOs (the devtool snapshot already computes them), the
   Streamable HTTP transport, auth through guards, nothing exposed unless chosen, rate limits.
   Implemented by Owlkei Go itself (JSON-RPC over HTTP), to keep `go.mod` free of dependencies.
3. **A devtools MCP:** AI agents read a running app through the devtools platform (routes, DI graph,
   traces of failed requests).

## Heavy techniques waiting for approval

Designed and measured in the roadmap 0002 designs, not built (each needs the maintainer's approval
per use). Details and expected gains: `docs/review/http-0002.md` › What is left.

| Design | Technique |
|---|---|
| 0003 H1 | one scratch buffer and a kept JSON encoder per pooled context |
| 0003 H2 | shared read-only header value slices |

## New storage and HTTP client modules

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

SQL injection (decided in the security roadmap): the storage API takes parameters only (placeholders
through `database/sql`), never a query built by string concatenation, and offers no raw-string escape
helper; `gosec` (G201/G202) already runs in CI. No input-sanitizing middleware: escaping belongs at the
sink.

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

Decided 2026-11-17: a real app built with Owlkei Go + Owlkei (no devtools needed yet), to validate the
framework from the user's side before 1.0. Idea: booking for pickleball / badminton / mini football
courts (SaaS for court owners). Keep it small: one court type, one payment gateway, one 1 vCPU VPS.

- Owner side: court schedule grid, revenue and peak hours charts, regular customers, court photos.
- Player side: public booking page, free slots, slots held by others shown live (WebSocket),
  payment through a local gateway (webhook).
- What it exercises: auth and multi-tenancy, double-booking races, payment webhooks and outbound
  calls (timeouts, circuit breaker), public pages (CSRF, security headers, `throttler`, load shedding),
  WebSocket.
- Storage and auth don't exist yet: use `database/sql` (SQLite or Postgres) and a simple auth; the
  pain points become the requirements of the storage and auth roadmaps.
- Later, a light web analytics app as a real-world performance test.

## Authentication notes (from the security roadmap)

- **Sessions:** `HttpOnly`, `Secure`, `SameSite=Lax`, a new session ID at login, lifetimes; brute force
  on login with `throttler`.
- **The current user in layouts:** see "Shared layout data for pages".
- **Error pages under load:** an HTML 5xx (also load shedding's 503 for a browser) executes `503.html` /
  `5xx.html` inside the root layout, so a refusal costs a template execution, and a layout that reads
  the current user fails and falls back to the built-in page (P19), safe but silent apart from
  `RenderFailed`. Consider a pre-rendered page for pre-route refusals.

## `loadshed` and long-lived requests (found in roadmap 0006)

Every request in flight holds one slot of the concurrency limit for its whole life, so a few SSE
clients, long polls or large downloads can fill the limit of a small machine and leave nothing for
ordinary requests. Options to design: exclude routes or requests marked long-lived from the limit
(a per-route opt-out or a separate fixed budget), or let a handler release its slot once it starts
streaming. Needs its own design; today the README documents it as a known limit.

The L12 acceptance test (`TestAdaptive_NoRefusalBelowCapacity_1CPU`) measures capacity and then offers
70 % of it, so another process taking CPU mid-run can make it refuse. `make check` runs it up to 3 times
(fails only when every attempt does); a machine that stays busy for minutes can still fail it. A better
fix: measure capacity again right before the check, or run it only on a quiet runner.

## Less configuration in `main` (discussed 2026-11-18)

Done in roadmap 0013: `AppOptions`, `Listen()` without arguments, `PORT`, `OptionsFromEnv()` with `OK_*`
variables and `.env`. Still open (not decided):
- **One mode switch.** `OK_ENV=development` turns on, together, the pretty log, template reload
  from disk and the devtools; unset means production. Replaces per-feature switches (`IsReload`, the
  log format). In tension with "Explicit protection" (nothing runs unless configured): a mode that turns
  features on or off must stay explicit and visible in the startup log.
- **Enable from the controllers.** A `owlkei.WS` controller opens `/ws` without `AppOptions.WS` (the
  render side cannot do the same: `//go:embed` must stay in the app). Weigh against "one obvious way".
- **`modules/config` allowlist:** it reads every OS variable but applies none by itself (the app picks
  keys); an option to read only declared keys.

## Shared layout data for pages (SSR follow-up, discussed 2026-11-18)

Layouts don't repeat HTML, but every page handler must still put the data its header and sidebar need
(current user, active menu item) into its `View.Data`. Proposal: `RenderOptions.Layout func(c
owlkei.HTTPContext) any`, called once per render; the layout receives `.Layout` and `.Page` and places
the page with `{{template "content" .Page}}`; `error.html` gets the same `.Layout`. Changes design
0018's rule that page and layout share `.`. Belongs with the authentication roadmap (the current user).

## Owlkei Go CLI and editor tools (decided 2026-11-18)

Owlkei Go gets a command-line tool. Commands discussed so far:
- **`owlkei routes [packages]`**: the route table of an app (method, path, page or API, handler,
  file and line, WebSocket events), computed with the runtime's own parser (`ParseFuncNameToURL`,
  `ParseWSFuncNameToEvent`), never a re-implementation. `--json` for tools; usable in CI to catch
  route conflicts or diff routes between branches.
- **`owlkei routes -w`** (optional, `go:generate`): writes a `// GET /path` comment above each
  handler, with `-check` for CI. Only if routes in the source are wanted; the editor tool below is
  preferred because it never goes stale.
- **VS Code extension**: a CodeLens or inlay hint above each handler with its method and path,
  updated as the name is typed, fed by `owlkei routes --json`. `gopls` has no custom CodeLens, so
  an extension is the only way. JetBrains later, if needed.
- **`owlkei new`**: scaffolds a project (module, controller, provider; for SSR the `web/`
  directory with `layout.html`, `error.html` and one page with its controller). The runtime never
  creates files (design 0018 amendment 1).
- Later: the local devtools binary (`owlkei devtools`) discussed for the devtools platform.

## TechEmpower benchmarks (owner, 2026-11-19: after the storage roadmap)

The owner wants to know where Owlkei Go stands, and to use a good result in its marketing. Estimate
today (not measured): with the `net/http` group (Gin, Echo, Chi), mid-table on plaintext and JSON,
upper half once a database is involved; our own load tests (0006) put it level with or ahead of
Gin / Echo / Chi, 61-67 % of Fiber on 1-2 cores and ahead of Fiber at 12 cores.

- **When:** after the storage roadmap, so the database tests use Owlkei Go's own SQL module.
- **What:** a TechEmpower implementation of Owlkei Go for the six tests (plaintext, JSON, single
  query, multiple queries, fortunes, updates) with PostgreSQL, written as a user would write it
  (`AppOptions`, controllers, providers, `RENDER_` for fortunes): classified **Realistic**, no
  hand-written HTTP parser, no benchmark-only shortcuts; log off as the other entries do.
- **How:** run the official toolset (Docker) locally, with the Gin, Echo, Chi and Fiber entries of
  the TechEmpower repository on the same machine, report in `docs/review/techempower-<n>.md`; then
  submit the entry upstream (a pull request to the TechEmpower repository) so the official rounds
  list it.
- **Marketing:** quote only official or reproducible numbers, name the test and the classification
  ("Realistic", which test), compare with frameworks of the same class, and keep the positioning
  (the ecosystem first, performance as a guarantee). Watch the fortunes test: `html/template` took
  92 % of a page's time in 0008, so it is where a template optimization would show.

## Security audit 0012: low findings (2026-11-21)

`docs/review/security-0012.md` lists nine low findings left open after roadmap 0012 (its high H1 and
medium M1 were fixed in the roadmap): gaps in the forbidden-file and Windows device-name lists, the
`RENDER_`/`SUBMIT_` guard check matching by type only, caching of unguarded `SERVE_` files, Host and
CSRF details, and three benchmarks the design lists but nobody wrote. Take them in the next security
pass or one by one with `/implement`.

## Accurate trace timings (found 2026-11-19)

The access log and the trace show stage times that users read as the cost of their code, so they
must be right. Measured on `examples/03-pages` and a two-middleware app:
- **Cold first request.** The first request after start showed `helmet` (now the `headers` stage) at 23 to 34 µs; later ones 3 to
  12 µs; its benchmark says 0.23 µs. Nothing tells the reader that the first request is cold: mark it
  (for example `cold` on the first request of each route), or leave it out of any aggregate.
- **Percentages of the wrong base.** They are shares of the measured stages, not of `total`
  (12 % shown for 23 µs of 344 µs). Show them of `total`, with an `other` line for the untraced rest
  (routing, injection, writing the response).
- **What a stage's duration includes.** Check that a stage's time is its own work only: no trace
  overhead (clock reads, span ID generation) and no time of the stages it wraps; a middleware's time
  ends at `next()` and starts again after it returns, or the docs say what it covers.
- **Benchmarks that match production.** `BenchmarkHelmet_*` reuses one warm `http.Header` map and
  reports far less than a real request pays. Benchmarks of per-request code use fresh request state per
  iteration (header maps, contexts), or say why not.
- **A calibration test.** A stage that sleeps a known time (for example 2 ms) shows that time within a
  tolerance in the trace, and the sum of stages plus `other` equals `total`.
- **The clock.** Durations use the monotonic clock (`time.Since`), never wall-clock differences.

Its own small roadmap. The display it should reach is in design 0022 › Step 7: trace (the `pre-route` group,
percentages of `total`, an `other` line).

## Large test files and a static file bug

- By the 1,500-line signal, split the code behind the largest core test files (`helpers_test.go`
  7,615 lines: split it by the code its helpers serve; `app_test.go` 2,357, `http_page_test.go` 1,908,
  `container_test.go` 1,605, `server_test.go` 1,511). The rule is in `.owlkei/conventions.md` › Tests ›
  Files.
- Found 2026-11-18: a file in `public/` with a space in its name can't be reached (cause not found yet).

## Health check and run-time detection (after the startup lifecycle, 0009)

Left out of roadmap 0009; with the circuit breaker:

- **Health check:** an `AppOptions.Health` group; `/health/live` always `200` (never checks a
  dependency); `/health/ready` `200` when the static modules are up, `503` while draining after SIGTERM;
  a minimal body (`status`, `degraded`), per-module detail opt-in, never error text (R24); answered
  before the user middlewares and guards, after the `proxy` stage. Open: default paths (`/health/live`
  vs `/healthz`).
- **Run-time detection:** providers may implement `Health(ctx context.Context) error` (cached, singleflight,
  timeout) or a circuit breaker counts failures; a run-time state `degraded` propagates like readiness and
  the affected routes answer `503` at once instead of waiting for timeouts, back to `ready` when the check
  passes. The built-in storage modules may map connection errors to `503`.

## Validation of DTOs (decided 2026-11-18)

Today a pipe binds its fields by the `bind` tag (`Bind`, `BindValue`) and validates by hand in `Transform`.
Wanted: declarative validation next to the binding, on the same struct, so a DTO says both where a field
comes from and what it must be.

- Rules in a tag beside `bind` (for example `validate:"required,email,max=120"`), checked after binding,
  before the handler; a fixed set of rules first (required, length, range, pattern, one of, email, URL),
  custom rules registered by name.
- One error shape for every field that fails (field name as bound, rule, no input echoed), answered `400`
  or `422` for REST and handed back to a `SUBMIT_` page so the form re-renders with its field errors.
- Rules parsed once per type at `Create()` (a malformed tag panics there), no reflection per request
  beyond what binding already does; no new dependency without a decision.
- Same rules for WebSocket message bodies.
