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

- Decided 2026-11-17: `pattern` moves to `internal/pattern` (only `wsevent`, `memorybroker` and
  `log` use it; users write patterns as strings, never the type).

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
- `EnableWS` writes into the caller's `*WSOptions`: two apps sharing one config overwrite each other.
- The goroutine that drops dead WebSocket connections never stops, even after `Stop()`.
- `Publisher.Publish` silently drops every payload after the first.
- Found 2026-11-18: acks and errors go through `TrySend` on the 32-slot send buffer, so a client that
  receives many events loses its acks silently (`TestWS_ConcurrentConnectionsIsolated` times out under
  load: 8 clients × 20 publishes fan out 1 280 events). Control frames need their own path or a
  blocking send with a deadline.

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

## `loadshed` and long-lived requests (found in roadmap 0006)

- Found 2026-11-18: `TestAdaptive_NoRefusalBelowCapacity_1CPU` (L12) refuses requests when the host is
  busy (load average 3-4 from other processes), at `402b10f` too. The test measures capacity and then
  offers 70 % of it, so another process taking CPU mid-run makes 70 % of the measured capacity more
  than the CPU left. Make the acceptance step measure capacity again right before the check, or run it
  only on a quiet runner.


Every request in flight holds one slot of the concurrency limit for its whole life, so a few SSE
clients, long polls or large downloads can fill the limit of a small machine and leave nothing for
ordinary requests. Options to design: exclude routes or requests marked long-lived from the limit
(a per-route opt-out or a separate fixed budget), or let a handler release its slot once it starts
streaming. Needs its own design; today the README documents it as a known limit.


## Less configuration in `main` (discussed 2026-11-18, outside the SSR roadmap)

Proposals the maintainer asked to keep for later (not decided):
- **Built-in middleware order:** moved to "Security roadmap" below.
- **One mode switch.** `GINJECT_ENV=development` turns on, together, the pretty log, template reload
  from disk and the devtools; unset means production. Replaces per-feature switches
  (`IsReload`, the log format, the sample's `PAGES_DEV`).
- **Enable from the controllers.** A `ginject.WS` controller opens `/ws` without `EnableWS` (the
  render side cannot do the same: `//go:embed` must stay in the app). Weigh against "one obvious way".
- **`Listen()` without arguments** reads `PORT` (default 3000), as PaaS platforms expect.

## Shared layout data for pages (SSR follow-up, discussed 2026-11-18)

Layouts don't repeat HTML, but every page handler must still put the data its header and sidebar need
(current user, active menu item) into its `View.Data`. Proposal: `RenderOptions.Layout func(c
ginject.HTTPContext) any`, called once per render; the layout receives `.Layout` and `.Page` and places
the page with `{{template "content" .Page}}`; `error.html` gets the same `.Layout`. Changes design
0018's rule that page and layout share `.`. Belongs with the authentication roadmap (the current user).

## Ginject CLI and editor tools (decided 2026-11-18)

Ginject gets a command-line tool. Commands discussed so far:
- **`ginject routes [packages]`**: the route table of an app (method, path, page or API, handler,
  file and line, WebSocket events), computed with the runtime's own parser (`ParseFuncNameToURL`,
  `ParseWSFuncNameToEvent`), never a re-implementation. `--json` for tools; usable in CI to catch
  route conflicts or diff routes between branches.
- **`ginject routes -w`** (optional, `go:generate`): writes a `// GET /path` comment above each
  handler, with `-check` for CI. Only if routes in the source are wanted; the editor tool below is
  preferred because it never goes stale.
- **VS Code extension**: a CodeLens or inlay hint above each handler with its method and path,
  updated as the name is typed, fed by `ginject routes --json`. `gopls` has no custom CodeLens, so
  an extension is the only way. JetBrains later, if needed.
- **`ginject new`**: scaffolds a project (module, controller, provider; for SSR the `web/`
  directory with `layout.html`, `error.html` and one page with its controller). The runtime never
  creates files (design 0018 amendment 1).
- Later: the local devtools binary (`ginject devtools`) discussed for the devtools platform.

## Route DSL changes

Follow-ups found by the roadmap 0008 review (2026-11-18), not decided:
- Words after `BY`/`AND` and after `VERSION` absorb any all-upper-case word: `READ_users_BY_id_CREATE_x`
  → `{id_CREATE_x}`, `READ_users_BY_id_ALL` → `{id_ALL}`, `READ_x_VERSION_OF_y` → version `OF_y`. A
  future token would then change such a route silently, which the upper-case rule exists to prevent.
  Proposal: stop a parameter name or a version at any all-upper-case word that is not a token, and
  panic as the upper-case rule does.
- Odd names accepted without a panic: `READ_logo_png_FILE_FILE` → `/logo.png`, `READ_ANY_ANY_FILE` →
  `/*.*`, `SERVE_x_ANY_ANY` → `/x*/*`.

Taken by roadmap `docs/roadmap/0008-server-rendered-html.md`, steps 10-17 (2026-11-18).

## Security roadmap (decided 2026-11-18: right after the SSR phase)

Server-rendered pages with sessions are stateful: browsers send cookies on every request, which opens
CSRF, cache leaks of personal pages, open redirects and session theft that a stateless token API
mostly avoids. The roadmap reviews the SSR side first, then the REST side again, and settles the
middleware order.

**Rule while the SSR phase runs:** any security-relevant point found while building is written here at
once (what, where, why), not fixed on the spot unless it blocks the work.

Findings so far (2026-11-18 review of the SSR work):
- **Cache leak:** pages and error pages default to `Cache-Control: no-cache`, which lets a shared cache
  (CDN, proxy) store a personal page. Proposal: `private, no-cache` by default; a public page sets its
  own `Cache-Control`.
- **Open redirect:** `c.Redirect(url)` accepts any URL; `c.Redirect(form.Next)` sends users to any site.
  Proposal: on page routes, only same-origin paths; an absolute URL to another host needs an explicit call.
- **SSR without protection:** `EnableRender` works without `csrf` and `helmet`, so an SSR app can run
  with no CSRF check and no CSP. Options: (a) panic at `Create()` unless turned off explicitly; (b)
  `EnableRender` binds `csrf` and `helmet` with defaults, in the right order, unless the app bound them,
  and logs it. Leaning (b), "safe defaults".
- **Docs:** `template.HTML` in `Data` disables escaping (warn); `Bind` fills only `bind`-tagged fields
  (mass assignment); the rules of `public/` (dotfiles, `..`, directories) as for `assets/`.
- **Sessions** (with the authentication roadmap): `HttpOnly`, `Secure`, `SameSite=Lax`, a new session
  ID at login, lifetimes; brute force on login with `throttler`.

Findings of the design pass for the route DSL and pages by route (2026-11-18, design 0019 and design 0018
amendment 2; written here, not fixed):
- **A guard on the page does not cover its form post:** layers are bound per handler, so
  `BindGuard(auth, instance.RENDER_account)` leaves `SUBMIT_account` (the POST on the same URL) open
  (amendment 2, P16). Proposal: a layer bound to a `RENDER_` also covers its `SUBMIT_`, or `Create()`
  refuses a guard that covers one and not the other.
- **`SUBMIT_` without `csrf`:** a `SUBMIT_` is by definition a state-changing browser POST. Extends "SSR
  without protection": `Create()` could require `csrf` as soon as one `SUBMIT_` exists.
- **`//go:embed all:web`:** `public/.well-known/…` is only embedded with the `all:` prefix (verified:
  `//go:embed web` skips names starting with `.` or `_`), and `all:` also embeds every dotfile under
  `web/` (`.env`, `.DS_Store`, editor swap files) into the binary. They are never served (hidden segment
  rules of `assets/`, `public/` and the templates), but they ship. Docs must say it; consider a `Warn` at
  `Create()` listing hidden files found in `Dir`, or serving `.well-known` from a non-hidden name.
- **`public/` is served as is:** every file in it is public at the root URL, without escaping, nonce or
  allowlist (a forgotten `backup.sql`, a `.map`, a raw `.html` with inline script that the CSP then
  blocks or, without `helmet`, runs). Rules to document with the `public/` docs item above; consider a
  `Create()` log of the public URLs. A file in `public/` also wins over a parameter or wildcard route at
  the same URL (static first).
- **Static routes and auth guards:** global guards run for `/assets/*`, `/assets/_pages/…` and the
  `public/` routes (R40, P24), so a global login guard also blocks `robots.txt`, `favicon.ico` and the
  CSS of the login page, while without one the CSS and JS of guarded pages (`/assets/_pages/admin/…`)
  are fetched by anyone and their URLs reveal the template tree and parameter names. The authentication
  roadmap decides the exemptions (design 0018 open question 7 covered `/assets/*` only).
- **Kebab-case moves URLs:** every multi-word resource changes from `_` to `-` (design 0019). Anything
  outside the app keyed on the old paths must follow: reverse proxy and WAF allowlists, `throttler`
  `KeyFunc`s built from the path, monitoring. An old URL answers 404, not a redirect.
- **`Path` and prefixes are developer text, not validated beyond the router's checks:** `Path` refuses
  `?` and `#` only. Encoded characters, mixed case and trailing dots are registered as written; review
  with the REST pass (path normalization before matching).
- **Error pages render under load:** an HTML 5xx (also `loadshed`'s 503 for a browser) executes
  `503.html` / `5xx.html` inside the root layout, so a refusal costs a template execution (design 0018 ›
  Performance already notes it). An error page's layout must also work with the four error keys only: a
  layout that reads the current user fails and falls back to the built-in page (P19), which is safe but
  silent apart from `RenderFailed`. Consider a pre-rendered page for middleware refusals.
- **Upper-case and malformed route names now stop `Create()`** (design 0019 B9, B13 to B16): before,
  `READ_files_BY_name_ANY` registered `/*/files/{name}` (a wildcard in front of the resource) and
  `READ_users_BY` registered `/users/{}`. Check deployed apps for routes that were wider than their
  names read when the REST review runs.

Also in scope:
- **Middleware order and automatic ordering** (moved from "Less configuration in `main`"): the framework
  orders the built-in middlewares by the production checklist (`loadshed` → `proxy` → `hostcheck` →
  `httpsredirect` → `helmet` → `cors` → `csrf` → `compress`) whatever order they are bound in, user
  middlewares after them. Today a wrong order (`proxy` after `hostcheck`) runs silently wrong. To decide
  together with the SSR auto-binding above.
- **REST review again** after the API review (0007) and the SSR changes (R24 client text, `Vary: Accept`,
  the asset and public routes): `ginject-security` audit as in roadmap 0005, report in `docs/review/`.
- Found 2026-11-18 (pages by route, step 17): a file in `public/` whose name holds `{` or `*` becomes a
  route parameter or a wildcard, so a file name can widen what the public route matches. Refuse such
  names at `Create()` (or escape them) in the security roadmap.
- **Injection review (raised by the maintainer, 2026-11-18).** Ginject has no database layer yet, so
  SQL injection is a rule for the storage roadmap: its API takes parameters only (placeholders through
  `database/sql`), never a query built by string concatenation, and offers no raw-string escape helper;
  `gosec` (G201/G202, already used by `ginject-security` audits) runs in CI. No input-sanitizing
  "anti-injection" middleware: escaping belongs at the sink (SQL parameters, `html/template`, headers).
  Review every other sink in this roadmap: HTML (`html/template`, `template.HTML` in data), headers and
  redirects (CR/LF, open redirect), logs (control characters: escaped in all formats, verify Text),
  paths (static, `assets/`, `public/`, file uploads), template names (table built at `Create()`), JSON
  (encoder), and later commands and queries in the storage and HTTP client roadmaps.

## Regroup test files

Done in roadmap 0008 step 20. Follow-up: by the 1,500-line signal, split the code behind the largest core test
files (`helpers_test.go` 7,615 lines: split it by the code its helpers serve; `app_test.go` 2,357,
`http_page_test.go` 1,908, `container_test.go` 1,605, `server_test.go` 1,511).

Taken by roadmap 0008 (step 20, 2026-11-18: after its review, before its `make check`).

### Plan

The rule is in `.ginject/conventions.md` › Tests › Files. Move every test into the `_test.go` of the
code file that implements it (`internal/core`: 14 `create_*_test.go` → `app_test.go` /
`container_test.go`; `http_page_*_test.go` → `http_page_test.go`; `http_page_error_test.go` and
`http_error_page_test.go` → `http_error_test.go`; root `surface_*_test.go` → `surface_test.go`; the same
in every package), helpers into one `helpers_test.go` per package. Moves only, no test changed: the
count of tests and benchmarks per package is the same before and after (`go test -list .`), and
`make check` passes. One commit per package.
- Found 2026-11-18 (review of roadmap 0008): `public/` file names holding `*` or `{…}` register a wildcard
  or a parameter route (`public/*` answers `GET /anything`, `public/{id}/x` answers `GET /zz/x`), and
  `public/{id}.txt` panics with a route message that doesn't name the file. Refuse such names with an
  `invalid render: public/<name> …` panic, or register them as literal segments. A file with a space
  in its name can't be reached (cause not found yet).
