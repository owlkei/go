# 0003: Server and production

Status: Running

## Goal
Finish the HTTP core: the rest of `core` (request dispatch, the 404 path, the server, lifecycle and
DI wiring) and the per-request logging and tracing get the deep dive, and Ginject gets the
production defaults and options a real deployment needs: timeouts, limits, graceful shutdown, TLS,
HTTP/2, panics that never leak internals. Measured against `docs/review/http-0002.md`.

## Scope
- In: `core` except WebSocket (`app.go`, `http.go`, `module.go`, `module_builder.go`, `default.go`,
  `fn.go`, `fn_plan.go` as far as dispatch needs); `log`, `accesslog`, `trace`, `event`; the devtool
  snapshot bug; the findings carried over from roadmap 0002 (`docs/review/http-0002.md` › What is
  left); `core`'s README.
- Out: middlewares, guards and built-in modules (roadmap 0004); WebSocket, including `core/ws*.go`,
  `read_loop.go`, `write_loop.go` (roadmap 0005).

## Steps
Every package step is the deep dive of roadmap 0002: bugs (each proved by a failing test), hot /
cold performance per `.ginject/conventions.md` › Performance, concurrency and security, tests,
benchmarks (`b.Loop`, `b.ReportAllocs`, `-count 5` + `benchstat` before and after in the commit),
README, the file rule.

| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Request dispatch: `ServeHTTP` and `handleRequest` (closures per request, `MaxBytesReader` on bodiless requests, the 404 / 405 paths with precomputed bodies), interceptors that never call `Pipe`, and the request lifecycle as the spec: every arrow of the lifecycle (see Decisions) tested; the HTTP layer's error response made explicit (see Decisions) | tdd | docs/design/0007-dispatch.md | - | deep dive done; one conformance test per lifecycle arrow; NotFound within 1.5× of net/http | done |
| 2 | Production server: `Listen` and its options (timeouts, header and body limits, keep-alive, `http.Server` access for advanced use), graceful shutdown (signals, drain timeout, `OnShutdown` order), TLS, HTTP/2 (automatic over TLS, h2c opt-in), a listener or `net.Listener` of the user's | tdd | docs/design/0008-server.md | 1 | deep dive done; the production checklist in the design holds, each item tested | done |
| 3 | Lifecycle and DI wiring (cold): `Create`, modules, providers, `OnInit` / `OnReady` / `OnShutdown`, the package-level registries (a second `App` in one process answers 404), `EnableDevtool` without versioning, `SERVE_<x>_ANY` never matching `/<x>/...` | tdd | docs/design/0009-lifecycle.md | 1 | deep dive done; two `App`s in one process work independently | done |
| 4 | Logging and tracing: `log`, `accesslog` (per-request work is hot), `trace`, `event` (the event bus every request checks) | tdd | docs/design/0010-observability.md | 1 | deep dive done; access log on costs a bounded, measured amount per request | todo |
| 5 | `core` README completed (server, lifecycle, production options), `.ginject/` updated (`/kb update core`) | light | - | 1-4 | README covers every exported API of `core` (HTTP side); KB matches the code | todo |
| 6 | Measure: rerun `make bench-compare` and the load test, compare with 0002 and the baseline; production checklist results | light | - | 1-5 | `docs/review/http-0003.md` | todo |

## Decisions
- Breaking the public API is allowed; the architecture stays (Module → Controller → Provider, the
  layers, the request cycle). Designs mark any change to them.
- Production is the bar. Defaults are safe for the open internet (today's timeouts and limits are
  the starting point; designs may tighten or explain them).
- **Request lifecycle (the maintainer's design, kept as is):** global middleware → module middleware
  → global guard → module guard → global pre-interceptor → module pre-interceptor → main handler →
  module post-interceptor → global post-interceptor → response. An error in a middleware goes
  straight to the HTTP layer's error response; an error from a guard onwards goes to the module
  exception filter, and an error in the module filter to the global exception filter. Middleware is
  an HTTP-layer concept only: WebSocket runs middlewares at the handshake, never per message. Step 1
  writes this lifecycle into `.ginject/request-pipeline.md` as the spec and tests each arrow.
- **The HTTP layer's error response** (decided 2026-11-16): one fixed format, the default filter's
  JSON (`code`, `error`, `message`); **users cannot customize it** (no hook; a middleware that wants
  its own response writes it and returns without `next()`). Only exceptions reach the client with
  their code and message; anything else (a string, an error, any value) answers a 500 with a fixed
  message, and the value, the stack and the request ID go to `app.Logger.Error`. This holds for the
  default exception filter too (no development mode that returns details). When the response is
  already written, nothing more is written: the error is logged only. The middleware trace event is
  emitted on a panic too, with the status code. The README documents it ("Errors in middleware").
- Designs: one architect pass with Fable (dispatch, server and lifecycle share `core`; logging is
  separate code), split into two calls (steps 1-3, step 4). The user approves the designs.
- The run commits on `master`; WebSocket code is touched only where a shared `core` change forces it.

- **Server API** (decided 2026-11-16): options go to the listen call, `opts *ServerOptions` with
  `nil` for the safe defaults, like `http.ListenAndServe(addr, nil)`:
  ```go
  app.Listen(8080, nil)
  app.Listen(8080, &core.ServerOptions{ReadTimeout: 10 * time.Second, ShutdownTimeout: 30 * time.Second})
  app.ListenTLS(443, "cert.pem", "key.pem", nil)                        // HTTP/2 automatic over TLS
  app.ListenTLS(443, "", "", &core.ServerOptions{TLSConfig: myTLSConfig})
  app.Listen(8080, &core.ServerOptions{H2C: true})                       // behind a TLS-terminating proxy
  app.Serve(listener, nil)                                               // the user's own net.Listener
  ```
  Graceful shutdown is automatic in all of them (SIGINT / SIGTERM, drain for `ShutdownTimeout`, then
  `OnShutdown`). No automatic certificates (ACME); `TLSConfig` covers `autocert`. Field names and exact
  behavior are fixed in design 0008. No `Use…` method for server options (`Use` means middleware).
- **`App` as an `http.Handler`** (`ServeHTTP`) stays (tests, mounting under another mux, wrappers such
  as OpenTelemetry or an HTTP/3 server) but is documented only under an advanced section, with its
  caveats (no graceful shutdown, no server defaults, `OnShutdown` through `app.Stop()`), not as a way
  to run an app.
- **Logger API:** one way to set the logger, `app.SetLogger(l)` (renamed from `UseLogger`: `Use` means
  middleware). `App.UseLogOptions` and the public field `App.LogOptions` are removed (v2 leftovers,
  never meant as public API); configure the built-in logger with `app.SetLogger(log.NewLog(&log.LogOptions{...}))`
  and mask fields of a custom logger with `app.SetLogger(log.WrapLogger(myLogger, fields))`. The
  public field `App.Logger` becomes the getter `app.Logger()`. The `log.LogOptions` struct stays.
  Step 3.
- **`ServerOptions` has four fields:** `ShutdownTimeout`, `MaxRequestBodyBytes`, `TLSConfig`, `H2C`.
  Read / write / idle timeouts and the header limit stay as safe internal defaults (Product
  principles in `CLAUDE.md`: small outside, safe defaults); deeper tuning uses `App` as an
  `http.Handler` (advanced section).
- **Heavy techniques included:** 0003 H3 (capacity kept across requests) and 0004 H1 (hand-written
  error body), designed in step 1; the other three stay in the backlog.
- **405 Method Not Allowed** with an `Allow` header when the path exists for another method.
- **Per-event WebSocket middleware** (`common.InjectProvidersIntoWSMiddlewares`, `WSMiddlewareItem`)
  is dead code that contradicts "middleware is HTTP only": removed in step 1.

## Log
- 2026-11-16: roadmap approved by the user, with every decision above.
- 2026-11-16: designs 0007-0010 approved by the user, every open question as recommended; heavy technique 0002 H1 approved too (with 0003 H3, 0004 H1).
- 2026-11-16: step 1 done. Deviations: `AppendJSON` marshals the whole struct when a string needs escaping (same bytes, fewer allocations); the WS handshake shares middleware construction, not the HTTP loop; the test expecting a 53-byte 404 body corrected to 54 (the design miscounted). A non-exception panic costs about 25 µs (the 4 KiB stack of E3).
- 2026-11-16: step 2 done. Deviations: the defaults constructor is `newServerDefaults()` (the tests own the `serverDefaults` type name); any `Stop()` made while the shutdown hooks run returns at once (Go cannot tell the calling goroutine), other concurrent calls wait.
- 2026-11-16: step 3 done. Deviations: P5 (circular providers) dropped, unreachable with value providers; imports follow the `Imports(…)` order (static imports no longer always first); two pre-existing tests that contradicted V1 and G5 corrected by the orchestrator.
