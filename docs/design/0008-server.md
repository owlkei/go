# 0008: Production server

Status: Implemented
Packages: `core` (`app.go`, new `server.go`)

Roadmap: `docs/roadmap/0003-server-and-production.md`, step 2. Depends on design 0007 (the writer
`Unwrap`s, the logger receives errors) and uses the hook order of design 0009 (K1-K4). The server API
of the roadmap's Decisions (`Listen(port, opts)`, `ListenTLS`, `Serve`, the four fields of
`ServerOptions`, automatic graceful shutdown, no ACME, no `Use…` method) is taken as given.

## Goal
`app.Listen(8080, nil)` is a server one can put on the open internet: bounded timeouts and sizes,
graceful shutdown that lets running requests finish before resources close, TLS with HTTP/2, h2c
behind a proxy, or a listener of the user's own. Four options; everything else is a safe default.

## Bugs found
Read from the code and checked with throwaway probes against `master` (4d4576c).

| # | Where | Input | Today | Rule |
|---|---|---|---|---|
| 1 | `core/app.go:706-713` | SIGTERM while a request is running | `OnShutdown` hooks run first, then the drain: the running request works on resources its module already closed | G3 |
| 2 | `core/app.go:755-757`, `:751` | `app.Stop()` while `Listen` serves | Only the hooks run; the server keeps accepting requests on closed resources (`shutdownChan` is closed but nothing reads it) | G6 |
| 3 | `core/app.go:695-696` | A second Ctrl+C while a request hangs in the drain | Swallowed: the signal channel is still registered and nobody reads it, so the process cannot be interrupted for 30 s | G5 |
| 4 | `core/app.go:664-666`, `:699-705` | The port is already in use | The "Here Are Your Access URLs" banner is printed, then `Listen` returns the bind error | S3 |
| 5 | `core/app.go:612-616` | `Listen` before `Create` | nil pointer panic (`app.module` is nil) | S2 |
| 6 | `core/app.go:669-677` | Output to a file or a log collector | The ANSI banner goes to stdout in production logs | S4 |
| 7 | `core/app.go:652-662` | Any `net/http` server error (TLS handshake failures, …) | Written by the standard `log` package to stderr, outside the app's logger and its masking | S6 |
| 8 | `core/app.go:41-42` | A 10 MiB upload (the allowed size) on a 10 Mbit/s line | `ReadTimeout` is 5 s for the whole request, so any body slower than 16 Mbit/s fails | T1 |

There is no TLS, HTTP/2 or custom listener today; `maxRequestBodyBytes` has no public setter.

## Public API
```go
type ServerOptions struct {
	ShutdownTimeout     time.Duration // how long running requests may finish; 0 = 30 s
	MaxRequestBodyBytes int64         // request body cap; 0 = 10 MiB, negative = no cap
	TLSConfig           *tls.Config   // ListenTLS and Serve: the TLS configuration
	H2C                 bool          // Listen and Serve without TLS: accept unencrypted HTTP/2
}

func (app *App) Listen(port int, opts *ServerOptions) error                               // changed, breaking
func (app *App) ListenTLS(port int, certFile, keyFile string, opts *ServerOptions) error  // new
func (app *App) Serve(l net.Listener, opts *ServerOptions) error                          // new
func (app *App) Stop()                                                                    // changed behavior
```

Exported additions and why users need each:

| Addition | Why it is exported |
|---|---|
| `ServerOptions` (4 fields) | the roadmap's Decision; the only server configuration |
| `(*App).ListenTLS` | HTTPS without a proxy |
| `(*App).Serve` | a listener the user made: a loopback or Unix socket address, systemd socket activation, a test's `127.0.0.1:0` |

`H2C` keeps the name of the Decision although new option booleans start with `Is`
(`.owlky/conventions.md`); it is an acronym, not an adjective.

Changed (**breaking**): `Listen(port int) error` → `Listen(port int, opts *ServerOptions) error`.
`Stop()` now also stops a running server (G6).

Removed (**breaking**): the constants `DefaultMaxRequestBodyBytes`, `DefaultMaxHeaderBytes`,
`DefaultReadHeaderTimeout`, `DefaultReadTimeout`, `DefaultWriteTimeout`, `DefaultIdleTimeout`
(`core/app.go:35-44`). They become unexported: the Decision keeps these values internal, and an
exported constant nobody can pass anywhere is noise.

No sentinel error is exported: callers log what `Listen` returns.

Unexported contract (tests are internal to `core`):

```go
// core/server.go
type serverDefaults struct {
	readHeaderTimeout, readTimeout, writeTimeout, idleTimeout time.Duration
	maxHeaderBytes                                            int
}
func loadServerOptions(opts *ServerOptions) ServerOptions // nil and zero values → defaults (O1-O3)
func (app *App) newServer(opts ServerOptions, isTLS bool) *http.Server
// Test seams, fields of App set by New: tests replace them to shorten timeouts and inject signals.
//   app.serverDefaults serverDefaults
//   app.notifySignals  func() (signals <-chan os.Signal, stop func())
```

## Behavior

### Options
- **O1** `opts == nil` equals `&ServerOptions{}`.
- **O2** `ShutdownTimeout`: 0 or negative means 30 s.
- **O3** `MaxRequestBodyBytes`: 0 means 10 MiB (10 << 20); negative means no cap; a positive value is
  the cap of design 0007 B1 for every request served by this app from the moment the call starts
  serving. `ServeHTTP` used without a serving call keeps 10 MiB.
- **O4** `Listen` with a non-nil `TLSConfig` returns
  `owlky: TLSConfig is set: use ListenTLS, or Serve with your listener` and does not bind (it would
  otherwise serve plaintext by accident).
- **O5** `ListenTLS` with `H2C: true` returns `owlky: IsH2C applies to connections without TLS` and does
  not bind. `Serve` with both `TLSConfig` and `H2C` returns the same error.
- **O6** The options struct is read once, when the call starts; changing it later has no effect.
  A `TLSConfig` is cloned (`Clone()`), never modified.

### Starting
- **S1** `Listen(port, opts)` listens on TCP `:<port>` (every interface) and serves HTTP/1.1.
  `ListenTLS` does the same with TLS. `Serve(l, opts)` serves on `l`: with TLS when
  `opts.TLSConfig` is set, plain otherwise. All three block until the server has stopped.
- **S2** A serving call before `Create` returns `owlky: Create must be called before serving` (no
  panic). A second serving call while the app is serving returns `owlky: app is already serving`.
  A serving call after the app has stopped returns `owlky: app is stopped`. In all three, a listener
  passed to `Serve` is left open and nothing is logged.
- **S3** The address is bound before anything is logged about it: a bind failure (port in use, port
  out of range, no permission) is returned as `net.Listen`'s error, wrapped as
  `owlky: listen on :<port>: <err>`; no startup line, no banner, no hook runs, and the app can be
  served again.
- **S4** After a successful bind the app logs, through its logger: one `InstanceLoader` line per
  named module, one `RouteExplorer` line per route (sorted), one `WebSocketEvent` line per event (as
  today), then `Logger.Info("ServerListening", "address", <the listener's address>, "tls",
  <bool>, "protocols", <"HTTP/1.1" | "HTTP/1.1, HTTP/2" | "HTTP/1.1, h2c">)`. The address is the
  bound one (`Listen(0, nil)` logs the port the system chose). The colored access-URL banner is
  printed only when stdout is a terminal.
- **S5** `ListenTLS(port, certFile, keyFile, opts)`: with both file names, the pair is loaded before
  binding; a load error is returned as `owlky: load TLS certificate: <err>`. With both empty,
  `opts.TLSConfig` must supply certificates (`Certificates`, `GetCertificate` or
  `GetConfigForClient`); otherwise the error is `owlky: ListenTLS needs certFile and keyFile, or a
  TLSConfig with certificates`. Exactly one empty name is the same error. `Serve` with a `TLSConfig`
  that supplies no certificate returns the same error.
- **S6** The server's own error output (`http.Server.ErrorLog`) goes to the app's logger:
  `Logger.Warn("HTTPServer", "message", <line without the trailing newline>)`.

### Protocols
- **H1** Plain (`Listen`, `Serve` without `TLSConfig`): HTTP/1.1 only. An HTTP/2 prior-knowledge
  client is not served as HTTP/2.
- **H2** TLS (`ListenTLS`, `Serve` with `TLSConfig`): HTTP/1.1 and HTTP/2, chosen by ALPN; nothing to
  configure. A handler sees `r.Proto == "HTTP/2.0"` for an HTTP/2 client. The user's `NextProtos` is
  not required; when a user sets `NextProtos` without `h2`, HTTP/2 is off (their choice).
- **H3** `H2C: true` on a plain server: HTTP/1.1 and unencrypted HTTP/2 with prior knowledge on the
  same port (`r.Proto == "HTTP/2.0"` for an h2c client, `HTTP/1.1` for the others). The `Upgrade:
  h2c` request form is answered as HTTP/1.1 (RFC 9113 removed it).
- **H4** WebSocket needs an HTTP/1.1 connection (it hijacks). On a TLS server clients negotiate
  HTTP/1.1 for `wss://` by ALPN, so `EnableWS` works with H2; a WebSocket upgrade sent on an HTTP/2
  connection is refused by the handshake (not served).

How: Go 1.24 and later carry this in `net/http` (`http.Server.Protocols`: `SetHTTP1`, `SetHTTP2`,
`SetUnencryptedHTTP2`). Probed on Go 1.27.4: an h2c prior-knowledge client is served as HTTP/2.0, an
HTTP/1.1 client on the same port as HTTP/1.1, and `Shutdown` drains a running h2c request. **No
dependency is added**: `golang.org/x/net/http2/h2c` is not needed.

### Timeouts and limits (internal defaults)
- **T1** Every server gets these values; they are not options (Decision). Changes from today are
  explained and are Open question 1.

| Setting | Today | Proposed | Why |
|---|---|---|---|
| `ReadHeaderTimeout` | 2 s | 5 s | bounds a slow-header (slowloris) connection; 2 s cuts real clients on slow mobile links |
| `ReadTimeout` (headers + body) | 5 s | 30 s | bug 8: 10 MiB in 30 s needs 2.7 Mbit/s instead of 16 |
| `WriteTimeout` (from the end of the headers to the end of the response) | 15 s | 30 s | bounds a client that reads slowly; 15 s cuts ordinary slow handlers and medium downloads |
| `IdleTimeout` (keep-alive) | 60 s | 75 s | must be longer than the idle timeout of the proxy in front (AWS ALB and nginx upstreams default to 60 s), or the proxy reuses a connection the server just closed and answers 502 |
| `MaxHeaderBytes` | 1 MiB | 64 KiB | each connection can make the server buffer this much before any handler runs; also the HTTP/2 header list limit |
| HTTP/2 concurrent streams per connection | - | `net/http` default (at least 100) | |
| Request body | 10 MiB | 10 MiB (O3) | |

- **T2** A handler that needs longer (a large upload or download, server-sent events) extends its
  own deadline with the standard library:
  `http.NewResponseController(w).SetWriteDeadline(time.Time{})` or `SetReadDeadline`. This works
  with the writer handlers receive (design 0007: it has `Unwrap`).
- **T3** Keep-alive is on. Request bodies are closed by `net/http`.

### Graceful shutdown
- **G1** A serving call shuts down when the process receives `SIGINT` or `SIGTERM`, or when
  `app.Stop()` is called. It logs `Logger.Info("ShutdownSignal", "signal", <name>)` for a signal,
  `Logger.Info("Shutdown")` for `Stop`.
- **G2** Shutdown stops accepting: the listener is closed (new connections are refused) and idle
  keep-alive connections are closed at once.
- **G3** Requests that are running finish normally, for at most `ShutdownTimeout`. `OnShutdown`
  hooks run only after the last running request has returned, or after G4.
- **G4** When `ShutdownTimeout` passes with requests still running, their connections are closed
  (`http.Server.Close`; each request's `Context()` is cancelled), the hooks run, and the serving
  call returns `owlky: shutdown timed out after <timeout>` (no count of requests: counting them would
  cost two atomic operations on every request).
- **G5** After the first signal the app stops intercepting `SIGINT` and `SIGTERM`: a second signal
  terminates the process the default way, even during the drain or inside a hook.
- **G6** `Stop()` on a serving app runs G2-G4 and returns when the hooks have finished; the serving
  call returns after that. `Stop()` on an app that is not serving (used as an `http.Handler`) runs
  the hooks and returns. `Stop()` is safe from any goroutine and any number of times: the hooks run
  once per app, later calls wait for the first to finish and return. Called from inside a request
  handler it waits for that same request, so the README says to call it as `go app.Stop()` there.
- **G7** The hooks run in the order of design 0009 K4 (the reverse of initialization). A hook that
  panics is logged (`Logger.Error("OnShutdownPanic", "module", <name>, "panic", <value>)`) and the
  remaining hooks still run. Hooks have no timeout of their own (G5 is the way out).
- **G8** Return value: `nil` after a shutdown that drained in time (signal or `Stop`); G4's error
  after a timeout; when the server stops by itself (the listener fails), the hooks run and the
  error is returned as `owlky: serve: <err>`.
- **G9** After a serving call has returned from a shutdown the app is stopped: it cannot serve again
  (S2). `ServeHTTP` still answers (the user's own server may still be draining).
- **G10** WebSocket connections are hijacked, so `net/http` neither waits for them nor closes them;
  they end when the process exits. Closing them with a close frame during shutdown belongs to
  roadmap 0005.

### `App` as an `http.Handler` (advanced)
- **A1** `ServeHTTP` works without a serving call: no listener, no timeouts, no signal handling. The
  body cap is 10 MiB. `OnShutdown` hooks run only when the program calls `app.Stop()`. The README
  documents it under "Advanced: your own server" with these caveats, and as the way to tune what
  `ServerOptions` does not expose (other timeouts, several listeners, HTTP/3, mounting under a
  mux).

## Production checklist
Every item is a test (Test plan, "checklist"). Step 6 reports the results.

| # | Item | Rule |
|---|---|---|
| C1 | A client that never finishes its headers is disconnected after `ReadHeaderTimeout` | T1 |
| C2 | A client that stalls in the body is disconnected after `ReadTimeout` | T1 |
| C3 | A client that does not read the response is disconnected after `WriteTimeout` | T1 |
| C4 | An idle keep-alive connection is closed after `IdleTimeout` | T1 |
| C5 | Headers over the limit answer 431 | T1 |
| C6 | A body over the cap answers 413, declared and chunked | O3, 0007 B1 |
| C7 | A handler can stream past `WriteTimeout` by extending its deadline | T2 |
| C8 | A running request finishes with its normal response during shutdown | G3 |
| C9 | No new connection is accepted after shutdown starts | G2 |
| C10 | Hooks run after the last request, in reverse order, each once | G3, G7 |
| C11 | A request that outlives `ShutdownTimeout` is cut and reported | G4 |
| C12 | The second signal is not intercepted | G5 |
| C13 | A bind failure is returned before any startup output | S3 |
| C14 | TLS serves HTTP/2 and HTTP/1.1; plain serves HTTP/1.1; `H2C` adds HTTP/2 | H1-H3 |
| C15 | TLS misconfiguration never falls back to plaintext | O4, S5 |
| C16 | A panic in a handler, over a real connection, answers the fixed 500 and the server keeps serving | 0007 E3 |
| C17 | Server errors reach the app's logger; no ANSI banner without a terminal | S6, S4 |
| C18 | No goroutine of the server outlives the serving call | Concurrency |

## Performance
Cold path, except one read per request: the body cap. It is an `atomic.Int64` loaded in `ServeHTTP`
(about 1 ns, no allocation), written when a serving call starts. Everything else runs once per
process start or stop: write it for readability. Type A only: the route list is sorted once; no
`fmt.Sprintf` for the address (`net.JoinHostPort`, `strconv.Itoa`).

No heavy technique. HTTP/2 and h2c add no cost to HTTP/1.1 requests (`net/http` picks the protocol
per connection).

## Concurrency
- State machine on `App`, one mutex (`app.serveMu`): `created` → `serving` → `stopping` → `stopped`.
  S2 and G6 are decided under it; the lock is never held while serving, draining or running hooks.
- Goroutines of a serving call: `net/http`'s accept loop and one per connection (owned by
  `http.Server`), and one goroutine that waits for a signal or `Stop` and then runs the shutdown.
  The serving call returns only after the accept loop has returned and the shutdown goroutine has
  finished the hooks (C18). The signal registration is released on every return path.
- The shutdown procedure runs once per app (`sync.Once`); `Stop` callers wait on a channel closed
  when it finishes. Lock order: `serveMu` only; hooks run with no lock held, so a hook may call
  `Stop()` (it returns at once, the shutdown is already running).
- Two apps in one process each register for signals and each shut down on one signal.
- `MaxRequestBodyBytes` is written before the listener accepts and read atomically.
- Tests run under `-race` and check goroutine counts before and after a serving call.

## Security
- **Limits before anything else** (T1): header time and size, body time and size, idle time, HTTP/2
  streams. All are on by default and cannot be switched off through `ServerOptions`.
- **Slow clients** are bounded in every direction (C1-C4).
- **No accidental plaintext** (O4, S5): a TLS intent that cannot be honored is an error before the
  port opens.
- **TLS:** the user's `tls.Config` is used as given (cloned); Go's defaults apply otherwise (TLS 1.2
  minimum, its cipher suite choice). Private keys are read once at start; rotation without restart
  is `TLSConfig.GetCertificate`. No ACME (Decision); `autocert.Manager.TLSConfig()` fits `TLSConfig`.
- **h2c is opt-in** and meant for a trusted hop (a TLS-terminating proxy): the README says not to
  expose an h2c port to the internet. HTTP/2 flood protections (rapid reset, stream limits) are the
  standard library's.
- **Shutdown cannot be held hostage:** G4 bounds the drain; G5 lets the operator kill the process.
- **Logs:** S6 routes server errors through the app's logger and its masking; the address line of S4
  holds no secret.
- `Listen` binds every interface, as today; a loopback-only server uses `Serve` with a listener on
  `127.0.0.1` (README).

## Impact
- Callers: `sample/main.go` (`app.Listen(port, nil)`, and it must not call `Fatal` on a `nil`
  error), `bench/`, the README examples, `CLAUDE.md` › Architecture ("started with
  `app.Listen(port)`").
- `core/app.go`: `Listen`, `serveWithGracefulShutdown`, `Stop`, `callOnShutdown`, the constants;
  the server code moves to `core/server.go` (one topic: the file rule); `logBoostrap` and
  `getLocalIP` move with it (their only caller).
- Tests that change: `TestDefaultHTTPServerLimits_Values`, `TestNew_AppliesDefaultBodyLimit`,
  `TestServeHTTPBodyLimit_ZeroDisablesCap` (negative disables), `TestListen_Logs…`.
- Docs: `core/README.md` gets "Server" (the three calls, the options table, shutdown, TLS, HTTP/2,
  h2c), "Timeouts and limits" (the table of T1 and T2's recipe), "Deploying" (run behind a proxy
  or with `ListenTLS`; Kubernetes: add a `preStop` sleep so the endpoint is removed before SIGTERM;
  set the proxy's idle timeout below 75 s) and "Advanced: your own server" (A1).
  `.owlky/startup-lifecycle-analysis.md` and `.owlky/package-reference.md` in step 5.

## Test plan
Internal tests in `core/server_test.go`. Servers run on `127.0.0.1:0` through `Serve` (or `Listen`
with port 0 and the logged address), with `serverDefaults` shortened to tens of milliseconds and
`notifySignals` replaced by a channel the test owns. TLS tests generate a self-signed certificate in
memory (`crypto/x509`).

| Rule | Test | Case |
|---|---|---|
| O1-O3 | `TestLoadServerOptions_NilEqualsZero`, `…_ShutdownTimeoutDefault`, `…_BodyCapDefaultNegativePositive` | 0, negative, positive |
| O4-O6 | `TestListen_TLSConfigIsAnError`, `TestListenTLS_H2CIsAnError`, `TestServe_TLSConfigWithH2CIsAnError`, `TestServe_OptionsReadOnce`, `TestServe_TLSConfigNotModified` | the port stays free after the error |
| S1-S2 | `TestServe_AnswersOverTheListener`, `TestListen_BeforeCreateReturnsError`, `TestServe_SecondCallReturnsError`, `TestServe_AfterStopReturnsError`, `TestServe_ErrorLeavesListenerOpen` | |
| S3-S4 | `TestListen_PortInUseReturnsBeforeLogging`, `TestListen_LogsBoundAddress`, `TestListen_NoBannerWithoutTerminal` | a recording logger; stdout replaced by a pipe |
| S5-S6 | `TestListenTLS_BadCertificateFile`, `…_NoCertificateSource`, `…_OneEmptyFileName`, `…_TLSConfigCertificates`, `TestServe_ServerErrorsGoToLogger` | garbage bytes to a TLS port produce a `HTTPServer` warning |
| H1-H4 | `TestProtocols_PlainIsHTTP1`, `…_TLSNegotiatesHTTP2`, `…_TLSServesHTTP1Client`, `…_H2CServesBothOnOnePort`, `…_PlainRefusesPriorKnowledgeHTTP2`, `…_WebSocketOverTLS` | the handler reports `r.Proto` |
| T1-T2 (C1-C7) | `TestTimeouts_SlowHeaders`, `…_SlowBody`, `…_SlowReader`, `…_IdleConnection`, `…_HeadersOverLimit431`, `…_BodyOverCap413`, `…_HandlerExtendsWriteDeadline` | raw TCP clients |
| G1-G3 (C8-C10) | `TestShutdown_SignalDrainsRunningRequest`, `…_StopDrainsRunningRequest`, `…_RefusesNewConnections`, `…_ClosesIdleConnections`, `…_HooksAfterLastRequest` | a handler blocked on a channel; hook and handler record their order |
| G4 (C11) | `TestShutdown_TimeoutCutsRequestAndReports` | the request's `Context()` is done; the error names the timeout; the hooks still ran |
| G5 (C12) | `TestShutdown_StopsInterceptingSignals` | the seam's `stop` was called before the drain started |
| G6-G9 | `TestStop_WithoutServerRunsHooksOnce`, `…_ConcurrentCallsRunHooksOnce`, `…_ReturnsAfterHooks`, `TestShutdown_HookPanicDoesNotStopOthers`, `…_ReturnsNilWhenDrained`, `…_ListenerFailureRunsHooks`, `…_AppCannotServeAgain` | 16 goroutines call `Stop` |
| C13-C18 | `TestChecklist_…` one per item not covered above: `…_PanicOverRealConnection`, `…_NoGoroutineLeak` | goroutine count returns to its start |
| A1 | `TestServeHTTP_WithoutServerUsesDefaultBodyCap` | |

No benchmark: nothing here runs per request except one atomic load, covered by design 0007's
`BenchmarkServeHTTP_Static`.

## Open questions
1. **The changed defaults of T1** (`ReadHeaderTimeout` 2 → 5 s, `ReadTimeout` 5 → 30 s,
   `WriteTimeout` 15 → 30 s, `IdleTimeout` 60 → 75 s, `MaxHeaderBytes` 1 MiB → 64 KiB). The Decision
   allows designs to "tighten or explain"; three of these loosen a timeout, for the reasons in the
   table. **Recommendation: accept all five.** The cautious alternative keeps 2 s / 15 s / 60 s and
   changes only `ReadTimeout` (bug 8) and `MaxHeaderBytes`.
2. **One serving call per app** (S2). Serving HTTP and HTTPS at once from one app then needs the
   advanced path (A1). Allowing several calls would make `MaxRequestBodyBytes` depend on which call
   came last. **Recommendation: one call.**
3. **`Stop()` blocks until the hooks finish** (G6), which waits forever-until-timeout when called
   from a handler without `go`. The alternative returns at once and leaves "when is it done" to the
   serving call's return. **Recommendation: block** (tests and `main` want "stopped means
   stopped").

## Decisions (2026-11-16)

Approved by the user: every open question answered as recommended.

- Q1 all five changed defaults of T1 accepted.
- Q2 one serving call per app.
- Q3 `Stop()` blocks until the hooks finish.
