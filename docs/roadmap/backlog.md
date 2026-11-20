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
5. Storage: adapters over existing databases (SQLite, Postgres), not an engine of our own; Owlkei Go
   adds connections, transactions, migrations, pool bounds, the circuit breaker, trace and devtools.
6. HTTP client (timeouts, bounded retries, circuit breaker).
7. 0.x release once the court booking app runs for real.
8. After the release: WebSocket (0006), devtools and MCP.

## Rebrand to Owlkei Go (decided 2026-11-19, before the security roadmap)

Ginject (this framework's old name) and Owlkei become one brand, Owlkei, with independent products: **Owlkei UI** (the component
library) and **Owlkei Go** (this framework; "Server" was dropped: it reads like VPS hosting); later Owlkei CLI, Devtools, SDK and Studio. "Ginject"
read as a plugin of Gin, and one brand shares owlkei.com, accounts, MCP and the devtools platform.
Map: https://claude.ai/artifact/58h7inTZEi2qboPG41i1f1 (private to the owner).

- **Import path:** `go.owlkei.com/owlkei`, package `owlkei` (`owlkei.New()`, `owlkei.Module`); a vanity path:
  `go.owlkei.com/*?go-get=1` answers `<meta name="go-import" content="go.owlkei.com/owlkei git
  https://github.com/<owner>/go">` (a DNS record `go` to the website's host, a small middleware in
  the Next.js site, browsers redirected to the Owlkei Go docs). Needed before publishing; until then a
  `replace` in `go.mod` works.
- **Repositories:** the GitHub org `owlkei` was deleted and its name is blocked for 90 days (from about
  2026-11-19). Until then the repositories stay on the owner's account (`dangduoc08/go`,
  `dangduoc08/ui`) and the `go-import` tag points there; once the org name is free, move them to
  `owlkei/go` and `owlkei/ui` and change that one tag (no import changes for users). Nothing is published
  yet, so nobody depends on the interim location.
- **Rename in this repository:** done by roadmap 0010 (module, package, API file, knowledge base,
  agents `go-*`, every document). The planned mode switch is named `OK_ENV` for now; its final
  name is decided with "Less configuration in `main`".
- **Independence:** UI works with any backend, Owlkei Go with any frontend or JSON only; they meet at the
  `data-ok-*` markup, never import each other. owlkei.com is the one shared platform.
- **Devtools:** the collector stays here (`internal/devtool`, `EnableDevtool`); the interface lives at
  `owlkei.com/devtools` (built with Owlkei UI, source next to the website). The browser fetches the
  snapshot from the local app directly (CORS + `Access-Control-Allow-Private-Network` for the owlkei.com
  origin only, a pairing token printed at start, off in production); only the paid tier sends it to
  owlkei.com.

## Own WebSocket implementation (after HTTP is production ready)

Decided 2026-11-16: Owlkei Go replaces `golang.org/x/net/websocket` (its last dependency; its own
documentation points to other packages) with an implementation of its own, once the HTTP roadmaps
(0003 to 0005) are done. It becomes part of the WebSocket roadmap (0006).

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

## WebSocket framework lines without the connection ID (WebSocket roadmap 0006)

Found 2026-11-16 in the review of roadmap 0004: the framework's own WebSocket error lines
(`WSReadFailed` in `core/read_loop.go`, `WSWriteFailed` in `core/write_loop.go`, `WSPingSendFailed`
in `core/ws.go`) are written through `ws.logger`, not through `log.From` on the connection's
context, so they carry no `trace_id` or `connection`. The WebSocket roadmap logs them through the
connection scope.

## Public API surface (after roadmap 0005)

Decided 2026-11-16 (Product principles in `CLAUDE.md`): Owlkei Go exports about 607 functions, types and
methods outside `sample/` and `bench/`, most of them framework internals (`common.Construct`,
`BuildHTTPInterceptMiddleware`, the `common.InsertedRoutes` global, `routing` internals,
`ctx.BinderOf`, the devtool builder). A dedicated roadmap, after 0005 and before WebSocket: review
every exported name, move internals to `internal/` or unexport them, collapse duplicate ways of doing
one job, and rewrite the docs around the resulting surface.

- Decided 2026-11-17: `pattern` moves to `internal/pattern` (only `wsevent`, `memorybroker` and
  `log` use it; users write patterns as strings, never the type).

## MCP (with the devtools platform)

Decided 2026-11-16: Owlkei Go gets MCP support when development of the devtools platform starts, in
three forms:

1. **A developer MCP:** tools AI coding agents query to write correct Owlkei Go code (concepts, the
   route DSL, injectable types, examples, scaffolding of modules / controllers / guards, a handler
   signature check, explanations of bootstrap panics). Lives outside the framework's code.
2. **A runtime MCP module** (for example `modules/mcp`, `app.EnableMCP()`): an app's routes exposed as
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

Decided 2026-11-17: a real app built with Owlkei Go + Owlkei (no devtools needed yet), to validate the
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

Priority: one of the two focuses of Owlkei Go (CLAUDE.md › Product principles). Planned right after
the public API review (which must design the render route kind), before the 0.x alpha; the court
booking app (real app phase) is built with it.

Decided 2026-11-17: Owlkei Go renders HTML pages on the server, so an app built with Owlkei's
framework-free HTML version needs no separate JSON API plus a React client (cheaper on small
machines: no client bundle, no second app). Details are discussed when the roadmap starts. Known
points to cover:

- A controller route kind that renders a template with the handler's data (the route DSL, so the
  public API review must leave room for it).
- `html/template` (contextual autoescaping) as the engine; layouts and partials; templates from an
  `embed.FS` in production, reloaded from disk in development; parsed once and cached.
- Owlkei's HTML snippets and CDN assets (`owlkei.css`, the loader) used directly in templates; the CSP
  from `helmet` must allow them (nonces or hashes for inline scripts).
- Errors and 404 pages as HTML for page routes, JSON for API routes.
- Forms: `csrf` (0005) already protects them; validation errors rendered back into the form.
- Middleware refusals (`csrf` 403, `hostcheck` 421, `loadshed` 503) run before routing, so they
  pick HTML or JSON from the `Accept` header, not from the route.
- CSP: per-request nonces for inline scripts; Owlkei's assets embedded in the binary and served from
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
- **One mode switch.** `OK_ENV=development` turns on, together, the pretty log, template reload
  from disk and the devtools; unset means production. Replaces per-feature switches
  (`IsReload`, the log format, the pages example's `PAGES_DEV`).
- **Enable from the controllers.** A `owlkei.WS` controller opens `/ws` without `EnableWS` (the
  render side cannot do the same: `//go:embed` must stay in the app). Weigh against "one obvious way".
- **`Listen()` without arguments** reads `PORT` (default 3000), as PaaS platforms expect.
- **`AppOptions` from the environment (decided 2026-11-19).** Opt-in `owlkei.OptionsFromEnv()` returns
  an `AppOptions` filled from the environment and `.env` (every group non-nil, "off" when no variable
  set it); every field that is plain data binds from a variable.
  - **Names (decided 2026-11-19):** `OK_` (the same `ok` as Owlkei UI's `--ok-*` and `data-ok-*`; the
    docs say `OK_` is Owlkei's), then the field path, each `.` written `__`, each field in upper snake
    case: `HTTP.MaxBodyBytes` → `OK_HTTP__MAX_BODY_BYTES`, `Server.TLS.CertFile` →
    `OK_SERVER__TLS__CERT_FILE`, `Origins` → `OK_ORIGINS`. Readers derive the name from `AppOptions`
    without the docs, and it splits back one way only. Only letters, digits and `_`: a `.` or `-` can't
    be exported in a shell, and `${OK_HTTP-MAX}` silently means "OK_HTTP or the text MAX". Never a
    bare name (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` are Go's outbound proxy variables). `PORT` is the
    one platform-convention exception, filling `Server.Port` when `OK_SERVER__PORT` is unset. Names
    are generated from the struct; a test checks that no two fields share one.
  - **Types** from the field, not the file (values are strings): `string`; integers; `bool` only
    `true` / `false`; `time.Duration` with a unit (`15s`); `[]string` comma-separated; enums by their
    documented lower-case name (`header`). Slices are `a,b,c`, each element parsed as the declared
    Go element type (`[]int`, `[]time.Duration`, `[]netip.Prefix`…). **No silent errors (owner,
    2026-11-19): anything wrong stops the app at `New`**: a value that doesn't convert, one bad element
    of a list (`1,a,2`: the whole variable is refused, never half a list), a value of the right type but
    wrong meaning (an origin without a scheme, a bad CIDR, a missing TLS file), and an unknown `OK_*`
    variable (a typo would leave a setting unapplied), with the closest name ("did you mean
    `OK_HOSTS`?"). The panic names the variable, the element, the expected type and the `.env` line,
    never a secret's value. Version skew (a variable a newer release added) fails the same way, on
    purpose. An empty value (`OK_ORIGINS=`) is an error too ("remove the variable to unset it").
  - **Sizes (decided):** a type `owlkei.ByteSize` (`int64` underneath, like `time.Duration`) for every size
    field (`HTTP.MaxBodyBytes`, later WebSocket message sizes and upload limits), chosen by type, not by
    a struct tag. It accepts a plain number of bytes and units with their standard meaning, as
    Kubernetes does: `KB` / `MB` / `GB` = 1000ⁿ, `KiB` / `MiB` / `GiB` = 1024ⁿ; a lower-case `b` (bits)
    is refused. Constants for code: `owlkei.KB`, `owlkei.MB`, `owlkei.KiB`, `owlkei.MiB`… (`10 * owlkei.MiB`).
    An exported addition, justified by the size fields across groups.
  - **Sources and precedence (decided: code wins):** environment > `.env` fill the options first, then
    code assigns after them, so code wins by statement order, explicit `false` / `0` / `""` included,
    with no pointer fields:
    ```go
    opts := owlkei.OptionsFromEnv()
    opts.HTTP.Compress = false
    opts.Render = &owlkei.RenderOptions{FS: web}
    app := owlkei.New(opts)
    ```
    Without `OptionsFromEnv()` nothing is read. The real environment beats `.env`. `.env` only: standard syntax (Docker Compose, Kubernetes,
    systemd and IDEs read the same file), no YAML / TOML / JSON (one more source and a parser; JSON
    values for a structured field only if one ever needs it). `.env` is read by the same opt-in, from
    the working directory, skipped when missing. The startup log names each value's source (`(code)`,
    `(OK_HOSTS)`, `(.env:3)`), never a secret's value. Docs: `.env` in `.gitignore`, a committed
    `.env.example`.
  - **Code-only fields** (Go values, no variable): `Render.FS`, `Logger`, `TLS.Config`.
  - **Injection:** only `OK_*` and `PORT` are read (never `HTTP_*`: httpoxy, where a CGI server turns a
    request's `Proxy:` header into `HTTP_PROXY`); code wins; unknown `OK_*` stops the app; no value is
    logged. `modules/config` reads every OS variable but applies none by itself (the app picks keys);
    an allowlist option for it (read only declared keys) is a security-roadmap item.
  - **Implementation:** reuse `modules/config`'s `.env` parser and typed binding through an internal
    package used by both (the module still reads at `Create`; `AppOptions` at `New`, before any module),
    so both read one `.env` with one syntax: `OK_*` for the framework, the rest for the app.
- **Special environment variables (2026-11-19).** Today the framework reads no environment variable
  and no `.env` file: `Listen(port)` takes the port, and `.env` is read only when the app imports
  `config.Register(...)`, which still never feeds `Listen`. Proposal to weigh against "explicit
  protection" (App options): read only variables that are platform conventions and change no behavior
  silently, such as `PORT` (Heroku, Cloud Run, Fly.io, Railway set it, so a deploy needs no code
  change); an explicit value in code wins over the variable; the startup log says where each value came
  from (`port=8080 (PORT)`). A mode switch (`OK_ENV`) that turns features on or off is in tension
  with "nothing runs unless configured": decide it with this item, after App options. Whether such
  variables come from `.env` too (without `modules/config`) is part of the decision.

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

## Route DSL changes

Follow-ups found by the roadmap 0008 review (2026-11-18):
- Decided 2026-11-19 (owner): parameter names and versions stay verbatim, upper-case words included
  (`BY_userID` → `{userID}`, `VERSION_V1` → `V1`); only an all-upper-case path segment that is not a
  route word panics. No change.
- Done 2026-11-19 (owner): the odd names now stop `Create()`: `FILE` twice in one resource
  (`READ_logo_png_FILE_FILE`), `ANY` as an extension (`READ_logo_ANY_FILE`), `ANY` twice in a row
  (`READ_ANY_ANY_FILE`, `SERVE_x_ANY_ANY`). A name may still be `ANY` (`READ_ANY_html_FILE` → `/*.html`).

Taken by roadmap `docs/roadmap/0008-server-rendered-html.md`, steps 10-17 (2026-11-18).

## Multi-parameter page routes: tests (done 2026-11-20)

Done: nine tests in `internal/core/http_page_test.go` (`TestPage_MultiParam_*`) cover the seven cases below; all passed
with no code change. The README's template table and layout rule show a multi-parameter example.

Route parsing with several parameters is tested (`internal/common`: `BY_a_AND_b`, nested `OF`, order),
and page routes with one parameter are (`bookings/{id}.html`, `bookings/{id}/index.html`, a wrong name,
both forms). Page routes with several parameters have no test. With `/implement`, add tests (and fix
what fails) for:
1. nested template lookup: `orgs/{org_id}/projects/{project_id}.html` and `…/{project_id}/index.html`;
2. `AND`: `reports/{year}/{month}.html`;
3. a `layout.html` inside a parameter directory (`orgs/{org_id}/layout.html`) wrapping every page below,
   nested in the root layout;
4. page CSS and JS inside parameter directories: served at `/assets/_pages/orgs/{org_id}/…`, linked by
   `{{pageAssets}}`;
5. a multi-parameter `SUBMIT_` sharing its `RENDER_`'s template;
6. errors: a wrong name for one of the parameters; both forms (`{org_id}.html` and
   `{org_id}/index.html`) for a middle parameter;
7. an error page (`404.html`) for a multi-parameter page whose handler panics `NotFoundException`.

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

## Release hygiene (owner, 2026-11-20: before 0.1)

The repository is public, so everything committed is already published: `CLAUDE.md`, `.claude/`
(agents and commands), `.owlkei/` (the AI knowledge base), `docs/roadmap/`, `docs/design/`,
`docs/review/`. Decided: keep them (transparency, and they help contributors who work with AI agents);
Claude Code memory lives in the owner's home directory and is never part of the repository;
`CLAUDE.local.md` and `.claude/settings.local.json` stay ignored. Before 0.1:
1. **No personal data or local paths** in committed files, checked in CI (like Owlkei UI's
   `check:names`). Fixed 2026-11-20: a local path in `.owlkei/README.md`. Open: `package.json`'s author
   email (the owner decides: keep, or a GitHub `noreply` address).
2. **`AGENTS.md`** pointing at `CLAUDE.md` and `.owlkei/`, so every AI tool (Codex, Cursor, Claude Code…)
   finds the conventions.
3. **Roadmap wording:** keep the decisions and their reasons; remove internal notes a reader can't
   follow.
4. **README › Contributing:** the knowledge base, `make check`, and the workflow commands (`/roadmap`,
   `/feature`, `/implement`).
5. **Know that `go get` downloads the whole module** (`docs/`, `.claude/`, `.owlkei/` included) into
   the user's module cache; small, so acceptable.

## Security audit 0012: low findings (2026-11-21)

`docs/review/security-0012.md` lists nine low findings left open after roadmap 0012 (its high H1 and
medium M1 were fixed in the roadmap): gaps in the forbidden-file and Windows device-name lists, the
`RENDER_`/`SUBMIT_` guard check matching by type only, caching of unguarded `SERVE_` files, Host and
CSRF details, and three benchmarks the design lists but nobody wrote. Take them in the next security
pass or one by one with `/implement`.

## Accurate trace timings (found 2026-11-19)

The access log and the trace show stage times that users read as the cost of their code, so they
must be right. Measured on `examples/03-pages` and a two-middleware app:
- **Cold first request.** The first request after start showed `helmet` at 23 to 34 µs; later ones 3 to
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

Its own small roadmap or the first steps of App options (the trace display changes there anyway).

## App options (discussed 2026-11-19, before the security roadmap)

All configuration in one place, passed to `New`; code (layers) stays in `Bind…` methods; running takes no
configuration. Comparable frameworks: Spring Security and Rails own their security chain's order, users
configure it; ASP.NET Core and Django leave the order to users and pay for it in docs and bugs.

```go
app := owlkei.New(&owlkei.AppOptions{
	Proxy:   &owlkei.ProxyOptions{TrustedProxies: []string{"10.0.0.0/8"}},
	Hosts:   []string{"shop.example.com"},
	Origins: []string{"https://shop.example.com"}, // CORS, CSRF and the WebSocket Origin check
	HTTP:    &owlkei.HTTPOptions{MaxBodyBytes: 1 << 20, Compress: true, HTTPSRedirect: true},
	Server: &owlkei.ServerOptions{
		ShutdownTimeout: 15 * time.Second,
		TLS:             &owlkei.TLSOptions{CertFile: "cert.pem", KeyFile: "key.pem"},
	},
	Render:         &owlkei.RenderOptions{FS: web},
	WS:             &owlkei.WSOptions{Path: "ws", MaxConnectionsPerIP: 20},
	Versioning:     &owlkei.VersioningOptions{Type: owlkei.VersioningHeader},
	Logger:         log.NewLog(&log.LogOptions{Format: log.FormatJSON}),
	AccessLog:      true,
	StartupTimeout: 30 * time.Second,
})
app.BindGlobalGuards(throttler.Throttler{Limit: 100, TTL: time.Minute})
app.BindGlobalMiddlewares(TenantMiddleware{}) // after the built-in stages, before routing
app.Create(AppModule)
app.Listen(443) // TLS comes from AppOptions.Server.TLS; ListenTLS is gone
```

Agreed with the owner (2026-11-19):
- `owlkei.New(opts *AppOptions)`, `nil` = safe defaults (D-032 naming: `XxxOptions`, never `Config`;
  `AppOptions` because it configures `owlkei.App`). Replaced: `SetLogger`, `SetStartupTimeout`,
  `EnableAccessLog`, `EnableDevtool`, `EnableRender`, `EnableVersioning`, `EnableWS`, the `ServerOptions`
  parameter of `Listen` / `Serve`, `ListenTLS`. `MaxRequestBodyBytes` moves to `HTTP.MaxBodyBytes` so it
  also applies when `App` runs inside the user's own `http.Server`. `Server` (shutdown, TLS, H2C) only
  applies when Owlkei serves; ignored with a log line otherwise.
- **No public built-in middlewares.** The eight (`loadshed`, `proxy`, `hostcheck`, `httpsredirect`,
  `helmet`, `cors`, `csrf`, `compress`) move to `internal/` and become fixed-order stages built at `New`
  from `AppOptions`: loadshed → proxy → hostcheck → httpsredirect → security headers → CORS → CSRF →
  compress → user middlewares → routing. A disabled stage is not in the chain. Order dependence is the
  framework's job, never the user's.
- **User middlewares stay** (`BindGlobalMiddlewares`, controller `BindMiddleware`), same API, always
  after the built-in stages; the docs rule: user middlewares must not depend on each other's order.
  Behavior changes to list: a user middleware bound before `helmet` / `proxy` / `loadshed` used to skip
  security headers, read the balancer's IP, or run under overload; now it never does.
- **Stage shape (internal):** keep the existing middleware shape (`Use(r, w, next)`, reusing the tests);
  `loadshed` and `compress` must wrap the handler anyway. Switching the check-only stages to a flat loop
  of check functions is allowed only if a benchmark shows a gain (internal, so free to change).
- **Port in the options (decided 2026-11-19):** `ServerOptions.Port`; `Listen()` takes no argument
  (empty port: `PORT` when the environment is read, else 3000); `Serve(listener)` stays for a listener
  of your own. `Create` (wire the app) and `Listen` (serve) stay separate: tests and embedding use
  `Create` + `ServeHTTP` with no port opened, `Listen` returns serve errors as an `error`, and a
  blocking `Create` would stop the code after it (NestJS splits `create()` and `listen()` too). No
  one-line `owlkei.Run(opts, module)`: a second way.
- **Defaults (2026-11-19): every stage is off** unless configured, in both roadmaps. Safety comes from
  the docs (a Configuration page that opens with a production checklist: one `AppOptions` block to
  copy), `examples/05-production`, `llms.txt` / MCP returning that block, and later the CLI templates
  (`owlkei new`). One startup warning, no behavior change: `Warn` `SecurityCheck` when the app has
  `SUBMIT_` routes without CSRF, or HTML pages without security headers, with the docs link; silenced
  by turning the protection off explicitly. This changes the product principle "Safe defaults" in
  CLAUDE.md to "Explicit protection: nothing runs unless configured; one `AppOptions` block; the CLI's
  templates ship the production configuration" (edited when the roadmap starts). No dev/prod mode is
  needed for these defaults.
- **Tracing the built-in stages (decided 2026-11-19):** kept, as a new stage `trace.StagePreRoute`
  (`"pre-route"`), named by role like the other stages (not `builtin`: origin, not role; not `ingress`,
  `edge`, `gate` (the `AsyncImports` gate) or `security` (`loadshed`, `compress`)). It serves every
  transport: what runs before the handler is found (HTTP method and path, the WebSocket handshake, a
  WebSocket message before topic matching, a gRPC method later). `Name` = the stage (`loadshed`,
  `proxy`, `hostcheck`, `httpsredirect`, `headers`, `cors`, `csrf`, `compress`; for WebSocket `origin`,
  `connections`, `message-size`, `message-rate`), `Duration` (for `loadshed`, queue wait included),
  `Code` = the refusal status or `0`. `StageMiddleware` stays for user middlewares only. Nothing is
  emitted without a listener; a disabled stage emits nothing. The pretty trace shows:
  ```text
  ├── operation           GET /bookings
  ├── pre-route
  │   ├── headers         23.05µs  (7%)
  │   └── csrf            422ns    (0%)
  ├── handler
  │   └── RENDER_bookings 22.5µs   (7%)
  ├── render
  │   └── bookings/index  142.51µs (41%)
  ├── other               156.07µs (45%)
  └── total               344.55µs
  ```
  Fixes found in today's output (`examples/03-pages`): percentages are of the measured stages, not of
  `total` (Helmet showed 12 % for 23 µs of 344 µs); show them of `total` with an `other` line for the
  untraced rest (routing, injection, writing); `operation` shows the path too. `helmet`: checked 2026-11-19. The
  23 µs was a cold first request (33 µs again on a first request, then 3 to 12 µs whatever its position
  in the chain), while `BenchmarkHelmet_*` says 0.23 µs and 0 allocations. The nonce is not the cause
  (it is made at render time, `placeNonce`); the likely cost is about ten writes into a fresh
  `http.Header` map on each real request, which the benchmark hides by reusing one warm map. For the
  security roadmap: a benchmark on a fresh header map per iteration, then decide (for example sizing
  the map once, or fewer headers on non-HTML answers).
- **Shared vs transport-specific:** `Proxy`, `Hosts`, `Origins` and load apply to every transport
  (HTTP, the WebSocket handshake, gRPC later); `HTTP`, `WS`, later `GRPC` hold the rest. SSE is an HTTP
  feature, not a transport (with "`loadshed` and long-lived requests").
- **Keeping `AppOptions` small:** the top level holds groups and a few shared fields; a feature adds a
  group, not top-level fields; empty means safe; one docs section per group (Configuration page:
  start here, who is the client, HTTP, Server, Pages, WebSocket, Versioning, Operations); godoc on
  every field with its default; no field depends on another (conflicts panic at `New`).
- **Two roadmaps:** first "App options" (move and merge, behavior unchanged: mostly mechanical, about
  370 `New()` and 600 `Enable…` / `Set…` call sites, mostly tests); then the security roadmap shares
  `Origins`, adds the WebSocket Origin check and the `SecurityCheck` warning, and fixes the sinks.

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
  Proposal (2026-11-19, the owner is thinking about it): no guard runs for `public/` and the shared
  `/assets/*` (public by definition; global middlewares still run); `/assets/_pages/<page>/…` runs the
  guards of that page's `RENDER_` route (global, controller and route-bound), so whoever can't see a
  page can't fetch its CSS and JS either. Private files go through a guarded `READ_…` route, not
  `public/`. Cost: `throttler` (a guard) no longer covers static files (`loadshed` still does); an
  opt-in option if that is needed. NestJS (checked 2026-11-19) serves static files with
  `express.static` / `@fastify/static` mounted as platform middleware before its pipeline, so no
  guard, global or not, runs on them (middlewares bound with `app.use()` do), and a protected file goes
  through a guarded controller route: the proposal's first row matches it; page assets guarded by their
  page have no NestJS equivalent. Draft roadmap: `docs/roadmap/0012-security.md` (paused 2026-11-19,
  its three open decisions recommended: `EnableRender` binds `csrf` and `helmet` itself; `Create()`
  panics on a wrong middleware order; this static-file rule).
- **Error format, decided 2026-11-19:** once a route matched, the route decides (a page route answers
  HTML, an API route JSON, whatever `Accept` says: guards such as `throttler`, filters, handlers);
  before routing (`csrf`, `hostcheck`, `loadshed`) and without a route (404, 405), `Accept` decides.
  Already the behavior (`TestPageError_ThrottlerFollowsRoute`); document it in the REST review.
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
  the asset and public routes): `go-security` audit as in roadmap 0005, report in `docs/review/`.
- Found 2026-11-18 (pages by route, step 17): a file in `public/` whose name holds `{` or `*` becomes a
  route parameter or a wildcard, so a file name can widen what the public route matches. Refuse such
  names at `Create()` (or escape them) in the security roadmap.
- **Injection review (raised by the maintainer, 2026-11-18).** Owlkei Go has no database layer yet, so
  SQL injection is a rule for the storage roadmap: its API takes parameters only (placeholders through
  `database/sql`), never a query built by string concatenation, and offers no raw-string escape helper;
  `gosec` (G201/G202, already used by `go-security` audits) runs in CI. No input-sanitizing
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

The rule is in `.owlkei/conventions.md` › Tests › Files. Move every test into the `_test.go` of the
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

## Startup and dependency lifecycle (discussed 2026-11-18, first part of the storage roadmap)

Verified on `402b10f`..HEAD (`internal/core/container.go` `callModuleFunc`, `scanGlobals`): a module
factory (`func(deps...) owlkei.Module`, passed to `Imports`) gets its parameters only from global
providers (plus `log.Logger` and `owlkei.Publisher`). A global from a static module is found whatever its
position or depth; a provider of a non-global sibling module panics `can't resolve argument`; a global
provided by a module that another factory returns is found only if that factory ran first (order of
`Imports`). Factories and `OnInit` run sequentially with no deadline: a dependency that never answers
(a database down at start) hangs `Create()` forever, with nothing in the log.

Agreed with the owner (design with the storage roadmap):

1. **Names:** **Module** (`ModuleBuilder()…Build()`) and **Module factory** (`func(deps...) owlkei.Module`).
   **Global** is a property of either (`IsGlobal`) and **async** is how it is imported (`AsyncImports`,
   item 5): neither is a kind. `Register…` is only a function name a module author picks. Rename the
   internal `isDynamicModule` / `dynamicModules` / `callModuleFunc`; one wording in README, CLAUDE.md and
   `.owlkei/`.
2. **Factory scope:** a factory resolves providers from its siblings in the same `Imports`, not only
   globals.
3. **No order dependency:** factories run in dependency order, whatever their position in `Imports`; a
   cycle panics with the chain.
4. **Module and Module factory fail fast:** an error or panic stops `Create()` with a clear message; the
   process exits non-zero.
5. **`AsyncImports(...)`** (a builder method next to `Imports`; the importing app decides, not the module
   author): every entry, a Module or a factory, is built in the background and the app starts at once.
   For a Module, the background part is building its providers and running their `OnInit` (a provider
   that connects there); for a factory, calling it first. Unlike NestJS `forRootAsync`, which still
   blocks the bootstrap (say so in the docs).
   - factory signatures, in both lists: `func(deps...) Module`, `func(deps...) (Module, error)`,
     `func(ctx context.Context, deps...) (Module, error)`; any other shape panics at `Create()` listing the
     valid ones. In `Imports`, `ctx` carries one startup deadline (default 30 s, an app option) and an
     error, a panic or the deadline fails fast; in `AsyncImports`, `ctx` is canceled at shutdown and an
     error or a panic turns the module `failed`;
   - success, even 10 s later: the module is wired then (providers built, dependents constructed lazily,
     their routes switched on by one atomic swap);
   - failure: the app keeps running; the module may retry (a retry / backoff helper offered by the
     framework; retries belong to the module, like the storage modules, not to each app);
   - the same entry in both `Imports` and `AsyncImports` panics, naming the module;
   - a factory resolves from its siblings in both lists (an async sibling once it is ready);
   - states `pending`, `ready`, `failed`, one-way (`ready` never goes back), held atomically per module
     and shown in the log and the devtool snapshot.
   - **lifecycle** (agreed 2026-11-18; module hooks are `OnInit` before the providers, `OnReady`,
     `OnShutdown` in reverse order). In `Create()`: the graph is scanned (a static async Module's
     providers are known, a factory's only once it returns); the `Imports` modules are built as today
     and fail fast; modules that depend on an async one get their routes registered behind a closed
     atomic gate (`503`) but no providers, controllers or `OnInit` yet; one goroutine per
     `AsyncImports` entry starts inside `Create()`; `OnReady` runs for the ready modules and the app
     listens. In the background: a factory waits for the siblings it needs and is called with `ctx`;
     then the module's `OnInit` and `NewProvider()` run (a panic counts as an error). On success the
     module is `ready`, its dependents are built in dependency order (`OnInit`, providers, controllers,
     then the gate opens), `OnReady` runs for them, and the log says `ModuleReady module=<name>
     after=<d>`. On failure the module and its dependents are `failed`, an `Error` log carries the
     cause, routes keep answering `503`; the framework calls a factory once (retries live inside it).
     At shutdown `ctx` is canceled, the framework waits for the goroutines within the shutdown timeout,
     and `OnShutdown` runs in reverse order only for `ready` modules (a factory that fails cleans up what
     it opened before returning);
   - **unknown providers of a factory:** a module whose dependency nobody provides yet stays `pending`
     while any async factory is still running; once all of them are done and it is still missing, the
     module is `failed` with an `Error` log `can't resolve <T>` (no panic: the app is running). A module
     whose dependencies come from static async Modules waits only for those;
   - **controllers in an async factory's module are refused:** the module is `failed` with a log telling
     to put the controllers in a static module that imports it (the router stays immutable). Controllers
     in a static async Module are fine (known at `Create()`, behind the gate);
   - WebSocket events of a module that is not ready answer a "not ready" error to `subscribe` and
     `publish`;
   - **docs:** this lifecycle gets a detailed section in the README (a timeline of `Create()`, the
     background and shutdown; the states; what runs when; the two refusals above; the difference from
     NestJS), with runnable examples, plus `.owlkei/` and CLAUDE.md.
6. **Readiness propagates through the module graph** (imports and injected providers): a module is ready
   only when every async module it depends on is ready, and a failure propagates (`sql` failed → `user`
   → `booking` not ready; a `booking` that only needs `redis` works). Per module first; per controller
   later, if needed.
7. **Routes of a module that is not ready stay registered and answer `503`** (an HTML route renders
   `503.html`); one atomic load, only on the affected routes; no cost on the others.
8. **After a module is ready, run-time failures are the business code's job:** a database that dies 5
   minutes later doesn't unwire anything (`*sql.DB` is a pool that reconnects on its own); the handler
   handles the error like any negative case.

Nice to have, later (with the health check and the circuit breaker):

- **Health check:** `app.EnableHealth(nil | &owlkei.HealthOptions{…})`; `/health/live` always `200`
  (never checks a dependency); `/health/ready` `200` when the static modules are up, `503` while draining
  after SIGTERM; a minimal body (`status`, `degraded`), per-module detail opt-in, never error text (R24);
  answered before the middlewares and guards except `proxy` (goes into the security roadmap as a
  bypass). Open: default paths (`/health/live` vs `/healthz`), the bypass.
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

## Timing-sensitive tests (found in roadmap 0009)

Fixed 2026-11-19: `TestLoadShed_ArrivalAtFreedSlotMayTakeIt` raced the 100 ms queue timeout on a busy
machine; the test lengthens it (`waitTimeout` is a package variable for that).
Fixed 2026-11-18: `TestTokenBucket_RetryAfterNeverZeroOnRefusal` uses a 500 ms refill and accepts 1 or 2
(a second boundary); the `ListenTLS` tests retry the bind on a new port when another process took it;
the `loadshed` L12 acceptance runs (`make check` only, without `-race`) are measured up to 3 times and
fail only when every attempt does. A machine that stays busy for minutes (a browser using a full core)
can still fail L12: rerun `make check` on a quiet machine.
- Low, unrelated: the `InstanceLoader` log line printed the root module twice (seen once in roadmap 0009); not reproducible on 2026-11-19 with static, imported and async modules: closed.
