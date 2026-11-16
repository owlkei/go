# 0005: Middlewares, built-in modules and security

Status: Designed

## Goal
Every middleware, guard and built-in module Ginject ships follows current standards and security
best practice, is safe by default, bounded in memory and nearly free when idle, on a small server.
The HTTP layer gains the middlewares a production app needs. The request scope added in 0004 costs
less on every request. The HTTP side passes a security audit, and a final HTTP report closes the
HTTP work before WebSocket.

## Scope
- In: the request scope cost from 0004 (`internal/tracecontext`, `ctx`, `core/http.go`) and an
  outbound `traceparent` transport in `trace`; `middlewares/cors`, `csrf`, `helmet`; new HTTP
  middlewares `proxy`, `hostcheck`, `httpsredirect`, `compress`, `loadshed`;
  static file serving in `core` (`SERVE_…`); `guards/throttler`;
  `modules/config`, `modules/cache`, `memorycache`; removing `modules/storage` and
  `modules/httpclient`; a security audit of the HTTP side; READMEs and the knowledge base; the HTTP
  report.
- Out: WebSocket, `memorybroker`, `wsevent` and `pattern` (0006); `devtool` (devtools roadmap); the
  public API surface review (its own roadmap after this one); the log batching delay (backlog); new
  storage and HTTP client modules (one roadmap, designed from scratch; backlog); authentication
  (Basic, API key, JWT, sessions: guards, a later roadmap); a timeout per route (an interceptor,
  backlog).

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Request scope cost: the +1 alloc and +528 B per request of 0004 (the request copy and the scope), the ID generator's global mutex (one locked read for the trace tail and request ID, or a per-P / batched random source) | tdd | docs/design/0013-request-scope-cost.md | - | Static and `StaticParallel` measured against `http-0004.md`; no lock contention in the parallel profile; IDs keep 0011's guarantees (format, uniqueness, `crypto/rand`) | todo |
| 2 | Outbound trace context: `trace.Transport(base http.RoundTripper)` adds `traceparent` from the request's context, so any client built on `net/http` continues the trace (replaces what `httpclient` did) | tdd | docs/design/0013-request-scope-cost.md | 1 | a call made with `c.Context()` carries the request's trace ID and its own span; an existing `traceparent` is kept; zero cost without a scope | todo |
| 3 | `proxy` (new): client IP, scheme and host from `Forwarded` (RFC 7239) and `X-Forwarded-*` only when the peer is a configured trusted proxy (CIDRs, or a hop count); without configuration `RemoteAddr` and the connection's scheme; the result is what the throttler, the access log, `httpsredirect` and `hostcheck` read | tdd | docs/design/0014-middlewares.md | 1 | spoofed headers from an untrusted peer change nothing; chains of proxies resolved right to left; hostile header sizes bounded | todo |
| 4 | `csrf` rebuilt on the current standard: Fetch Metadata (`Sec-Fetch-Site`) and `Origin` checks as Go's `http.CrossOriginProtection` does (no token, no cookie, no body read), with a trusted-origins list and an opt-in token fallback only if the design finds it needed | tdd | docs/design/0014-middlewares.md | 3 | cross-site unsafe requests rejected in every browser case of the design's table; same-origin and non-browser clients pass; no body read; no request copy | todo |
| 5 | `cors` and `helmet` to current standards: CORS (credentials never with `*`, `Vary: Origin`, preflight cache, private network access), helmet's header set (CSP, COOP, CORP, `Referrer-Policy`, `Permissions-Policy`, `X-Content-Type-Options`, HSTS over HTTPS only, `X-XSS-Protection: 0`) | tdd | docs/design/0014-middlewares.md | 3 | defaults match current OWASP and MDN guidance in the design's table; per-request cost and allocs measured | todo |
| 6 | `hostcheck` (new): allowed `Host` values, rejecting the rest (DNS rebinding, host header injection); `httpsredirect` (new): HTTP to HTTPS with the scheme from `proxy` | tdd | docs/design/0014-middlewares.md | 3 | unknown hosts get 421 or 400 as the design says; redirects keep path and query and never open-redirect | todo |
| 7 | `compress` (new): gzip from the standard library, minimum size, skips compressed types, `Range` and responses already encoded, `Vary: Accept-Encoding` | tdd | docs/design/0014-middlewares.md | 1 | compressed responses round-trip; CPU and memory per response measured on GOMAXPROCS 1 | todo |
| 8 | `loadshed` (new): an adaptive concurrency limit from measured latency (AIMD or gradient) within a min and max derived from the machine (GOMAXPROCS, GOMEMLIMIT / cgroup memory), a short bounded queue, then an immediate 503 with `Retry-After`; a fixed N as an explicit override; protects the whole server (the throttler limits each client) | tdd | docs/design/0014-middlewares.md | 1 | under a burst far above capacity, latency of admitted requests and heap stay bounded on GOMAXPROCS 1 and 2; the limit settles without oscillating; the extra requests get 503 at once; a few atomics per request, no goroutine or timer when idle | todo |
| 9 | Static file serving (`SERVE_…`): `Cache-Control` defaults (the dotfile and symlink escape bug was fixed before the roadmap, 2026-11-17) | tdd | docs/design/0014-middlewares.md | - | the design's defaults on every served file; hashed asset names can be marked immutable | todo |
| 10 | Throttler: client IP from `proxy` (proxy headers trusted only behind configured trusted proxies), bounded key store, no idle goroutine, the three strategies checked under concurrency | tdd | docs/design/0014-middlewares.md | 3 | a spoofed header can't bypass the limit or grow memory; memory stays under the design's bound with many distinct clients; strategies exact under `-race` with 12 goroutines | todo |
| 11 | `config`, `cache`, `memorycache`: deep dive; `memorycache` kept and fixed: no sweeper waking forever (today a ticker every ~20 ms, about 50 wakeups a second while idle: expiry on access plus a sweep that runs only while entries with a TTL exist), a bound on entries and bytes with eviction (today unbounded, and the throttler's default store); tests for `modules/cache` (none today), a README for `memorycache` | tdd | docs/design/0015-config-cache.md | 1 | eviction and TTL exact; memory stays under the bound with millions of distinct keys; zero goroutines and wakeups while empty, the cost with entries stated | todo |
| 12 | Remove `modules/storage` and `modules/httpclient`: the packages, their mentions in CLAUDE.md, `.ginject/` and the agents; `sample/shop` on in-memory repositories in the sample, `sample/benchmarks` on `net/http` with `trace.Transport` | light | - | 2 | no import or mention of either package left; the samples build and serve the same routes; `make check` green | todo |
| 13 | Security audit of the HTTP side (`ginject-security`: govulncheck, gosec, request handling, every middleware, guard and module above); fixes: dependency updates and small patches directly, behavior changes through the tester | light | - | 1-12 | no critical or high finding left open; the audit report in `docs/review/security-0005.md` | todo |
| 14 | READMEs of every touched or new package, a "Production checklist" in the main README (which middlewares to turn on, in which order), and the knowledge base (`/kb update`) | light | - | 1-13 | every new or changed name documented; KB matches the code | todo |
| 15 | Final HTTP report: the 0004 measurements again in production settings (JSON log, real output, GOMAXPROCS 1, 2 and all, process CPU per request), plus each middleware and module; `bench/` load test on the release build with logging on, against the other frameworks with their production loggers (Decisions); `docs/review/http-0005.md` | light | - | 1-14 | the review file, compared with `http-0004.md`; headline numbers with logging on | todo |

## Decisions
- **Merged from the 0004 review:** the request scope cost (step 1) is part of this roadmap.
- **Breaking the public API is allowed**, as in 0003 and 0004; the architecture and the request
  lifecycle stay.
- **Middleware is the HTTP layer only** (decided in 0003): every middleware here works on
  `*http.Request` and `http.ResponseWriter` before guards; WebSocket runs them at the handshake only.
  Authentication and authorization are guards, not middlewares.
- **Current standards and security best practice first** (decided 2026-11-17): each middleware's
  design cites the standard or guidance it follows (RFCs, OWASP cheat sheets, MDN, Fetch Metadata)
  and lists its defaults against it. Safe by default; loosening is explicit.
- **Timeouts per route are an interceptor, not a middleware** (decided 2026-11-17): the timeout
  goes through the exception filters and covers WebSocket messages too; the server's timeouts of
  0003 already bound the whole request. Backlog.
- **Static file serving** (found 2026-11-17): dotfiles were served and symlinks could leave the
  served directory; fixed at once as a bug (`os.Root`, dot segments 404 except `.well-known`).
  Only the cache defaults remain in step 9.
- **`loadshed` adapts by default** (decided 2026-11-17): the limit follows measured latency within
  bounds taken from the machine, so users never have to pick N; a fixed N is an override. If the
  design can't show a few atomics per request, no idle goroutine or timer and no oscillation on
  1-2 vCPU, the default falls back to a limit derived from the machine.
- **Runtime state is inspectable by the devtools** (decided 2026-11-17): every component keeps its
  parameters and state (loadshed's limit and latency, the throttler's keys and rejections, cache
  sizes and evictions, each middleware's effective config) readable through an internal snapshot,
  built only when read and free when the devtools is not attached.
- **No built-in health check endpoints** (decided 2026-11-17): `/healthz` and `/readyz` are not
  part of Ginject; apps write their own routes.
- **Every middleware is opt-in** and costs nothing when not used; the README's production checklist
  says which ones to turn on.
- **Trusted proxies** (decided 2026-11-17): no proxy header is trusted by default; the client IP is
  `RemoteAddr` until trusted proxies are configured (`proxy`). Breaking for apps behind a proxy.
- **Small machines first** (CLAUDE.md › Product principles): every store, buffer and goroutine is
  fixed-size or bounded; idle cost is stated and near zero; nothing runs when unused.
- **Measurements in production settings** (decided 2026-11-17): JSON log to a real output, process
  CPU per request, GOMAXPROCS 1 and 2 as well as all cores, and lines or work actually done counted
  (a component that drops work must not look cheap).
- **Speed is measured on the most optimized build, with production logging** (decided 2026-11-17):
  a release binary (no `-race`, no coverage, `-trimpath`, PGO when a profile exists) served over a
  real socket under load, besides the micro benchmarks. Ginject logs as in production (built-in
  logger, JSON, access log on, to a real output). Against another framework: each one runs with its
  own production request logger when it has one (Gin's Logger, Echo's RequestLogger, Chi's Logger,
  Fiber's logger); against one without a logger (`net/http`), Ginject's log is off for that
  comparison. Results with logging on are the headline: nobody runs production without logs.
- **`memorycache` is kept and fixed, not rewritten** (decided 2026-11-17): the sharded map stays;
  the always-on sweeper and the missing size bound go.
- **Rate limiting stays an opt-in guard** (`guards/throttler`), decided in 0004.
- **`modules/storage` and `modules/httpclient` are removed, not fixed** (decided 2026-11-17): new
  storage and HTTP client modules will be designed from scratch in one later roadmap (requirements
  first). Until then Ginject ships neither; outbound calls use `net/http` with `trace.Transport`.
- Designs: one architect pass, three design files (0013 request scope and outbound trace context;
  0014 middlewares and throttler; 0015 config and cache).

## Log
- 2026-11-17: roadmap approved by the user.
- 2026-11-17: designs 0013, 0014 and 0015 approved by the user, every open question as recommended; 0014 and 0015 reconciled (CLOCK eviction, `Incr` for the fixed window). Condition: small public API, every name documented clearly (the docs feed a website later).
