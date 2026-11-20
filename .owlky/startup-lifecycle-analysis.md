# Startup Lifecycle & Race Condition Analysis

**Purpose**: Application startup sequence, module initialization guarantees, shutdown, and the race
conditions an app can still create for itself. The user-facing contract (hooks, server, shutdown) is
`internal/core/README.md` › Modules and lifecycle and › Server; this page explains the implementation.

**Last Updated**: 2026-11-18 (roadmap 0009, design 0020: module factories, `AsyncImports`, states, `Retry`).
User guide with a timeline: `README.md` › Module lifecycle.

---

## Critical Finding

⚠️ **Async work started in a hook is not tracked**: a request or WebSocket message can reach a
provider before work it started in a goroutine has finished, when:
1. The handler depends on a provider (for example a cache client)
2. A hook starts that provider's setup in a goroutine (`go connectToRedis()`)
3. A client connects during that async phase

The framework guarantees only that every synchronous hook has returned before the server starts.
The tracked alternative is a module factory in `AsyncImports` that connects (with `owlky.Retry`):
the module stays `pending`, the routes that need it answer `503`, and they open only once it returned.

---

## Startup Sequence

### Phase 1: App Creation (`core.New(opts)`)

```
core.New(opts)
  ├─ loadAppOptions(opts)            every check (panics `invalid app: …`), copies, TLS pair load,
  │                                  the pre-route stages compiled once in their fixed order
  ├─ newHTTP()                       router and route tables of this app
  ├─ memorybroker.NewMemoryBroker()  the app's own broker (ready for pub/sub immediately)
  ├─ newContainer()                  the app's own modules, providers, layers, routes and events
  ├─ log.NewLog(nil)                 the built-in logger (replaced by AppOptions.Logger)
  ├─ context pools (HTTP exchange, WS context), server defaults, body cap 10 MiB
  ├─ container.addInterface(common.Publisher)
  └─ BindGlobalExceptionFilters(built-in HTTP and WS filters)
```

Every `core.New(opts)` is independent: there is no package-level registry of routes, events, providers or
layers (design 0009). The app's `trace.Tracer` is a zero value (no listener).

### Phase 2: App Initialization (`app.Create(m *Module)`, `internal/core/app.go`)

Sequential, on the calling goroutine:

```
Create(m)
  ├─ m == nil          → panic "invalid module: Create needs a root module"
  ├─ second call       → panic "invalid app: Create called twice"
  ├─ container.addInterface(log.Logger)
  ├─ container.wire(m)                      internal/core/container.go
  │    ├─ addGlobals(root)                  the root's providers are global
  │    ├─ scanGlobals(root)                 walks the static *Module imports first: every IsGlobal
  │    │                                    module's providers become global, at any depth
  │    ├─ discover(root)                    depth first, each module once:
  │    │     module factories of the sync set are called here, in dependency order (arguments from
  │    │     siblings, then globals, then Logger, Publisher), each on a helper goroutine within the
  │    │     startup timeout (AppOptions.StartupTimeout, default 30 s, one budget; its ctx carries the deadline);
  │    │     the provider of an argument is built after its owner module's OnInit (which then
  │    │     does not run again); each result is scanned before the next call;
  │    │     visible providers and controllers per module, prefixes per controller
  │    ├─ states and needs                  modules that need an AsyncImports entry are pending
  │    ├─ for each ready module in init order: OnInit() (unless already run), then provide() each provider
  │    │     (a provider is constructed after the providers its fields name, once per app)
  │    └─ injectController() for every controller of a ready module (fields injected, then NewController);
  │          controllers of pending modules are probed (throwaway instance, NewController) and gated
  ├─ initWS()                               only when AppOptions.WS.Path is set
  ├─ initRoutes(), initMiddlewares(), initExceptionFilters(), initGuards(),
  │  initInterceptors(), initMainHandlers()  layers constructed once per type and configuration,
  │                                          handler plans built (internal/core/fn_plan.go)
  ├─ app.http.finishRoutes()                one immutable route struct per route, fixed 404/405 bodies
  ├─ initDevtool()                          after routes and layers exist
  ├─ initAccessLog()                        the tracer's only listener, when AppOptions.IsAccessLog is true
  ├─ start one goroutine per AsyncImports entry (module_async.go)
  └─ callOnReady()                          OnReady of every ready module, in init order; then isCreated = true
```

**Why this order** (do not reorder without re-checking each step):
1. `wire` runs first: it initializes the modules, injects the controllers and fills `app.container`,
   which every later step reads.
2. `initWS` runs right after it and before `initExceptionFilters`, `initGuards`, `initInterceptors`
   and `initMainHandlers`: those write into `app.ws` (`catchFnsByEvent`, `eventMatcher`) whenever the
   app has WS handlers. `app.ws` stays nil without `AppOptions.WS.Path`, so each of those call sites checks
   `app.ws != nil`.
3. `initDevtool` runs after the routes and layers exist: it reads them for its snapshot.
4. `callOnReady` runs last. Only then is the app marked created, so a `Create` that did not finish
   runs no `OnShutdown`.

**Init order**: a module after every module it imports, imports in `Imports(…)` order, each module once
per app. Root importing A and C, both importing B: B, A, C, root.

**Key Points**:
- A panic in `OnInit` or `OnReady` stops `Create` and reaches the caller unchanged. An app whose
  `Create` did not finish runs no `OnShutdown`.
- `Create` never writes to the modules it is given: the same module value may be given to several apps.
- The `BindGlobal…` methods panic after `Create` (configuration is read at `New`):
  `invalid app: <Method> must be called before Create`.

### Phase 2b: Background (`AsyncImports`, `internal/core/module_async.go`)

```
per entry goroutine
  ├─ wait for the entries its factory needs
  ├─ factory(ctx)            ctx canceled at shutdown, no deadline; owlky.Retry reports attempts
  ├─ under wireMu: OnInit, providers, controllers of each module of the unit
  ├─ gate.live.Store(...)    one atomic store opens every route and event of the module
  ├─ state ready, log ModuleReady, then OnReady (a panic is logged OnReadyPanic)
  └─ the same for every pending dependent whose needs are now all ready
failure (error, panic, nil module, type never resolved) → failed, spreads to what needs it
```

### Phase 3: Server Start (`app.Listen()`, `app.Serve(listener)`; `internal/core/server.go`)

```
app.Listen()
  ├─ checks (before Create, already serving, stopped, option conflicts) → return an error
  ├─ bind :<port>          a bind error returns here: nothing logged, no hook runs
  ├─ log InstanceLoader (one line per named module that is ready), RouteExplorer (one per route),
  │  WebSocketEvent (one per event), then the access-URL banner on a terminal, else ServerListening
  │  (address, tls, protocols)
  └─ serve until SIGINT / SIGTERM / app.Stop() or a server error
```

The server accepts connections as soon as the banner or `ServerListening` is out.

### Phase 4: Shutdown (`internal/core/server.go`)

```
SIGINT / SIGTERM / app.Stop()
  ├─ stop intercepting signals (a second signal ends the process the default way)
  ├─ cancel the AsyncImports ctx (no state changes, no gate opens from now on)
  ├─ close the listener and idle keep-alive connections
  ├─ drain running requests for ShutdownTimeout (default 30 s), then close their connections
  ├─ wait for the entry goroutines until ShutdownTimeout since shutdown began; log
  │     ModuleShutdownTimeout and abandon the ones still running
  ├─ runShutdownHooks()   once per app (app.shutdownOnce):
  │     OnShutdown of every ready module in the reverse of the order they became ready (root first);
  │     a panicking hook is logged as OnShutdownPanic (module, panic) and the others still run;
  │     then the access log is stopped, a pending PanicStacksSuppressed count is logged,
  │     and the stdout log pipeline is flushed (at most 2 s) and made synchronous again
  └─ the serving call returns nil, "owlky: shutdown timed out after <timeout>" or "owlky: serve: <err>"
```

`app.Stop()` on an app that is not serving runs the hooks once and marks the app stopped. Every
`Stop()` waits until the hooks have finished, except one called from inside an `OnShutdown` hook
(the goroutine running the hooks): it returns at once instead of deadlocking. WebSocket
connections are not drained: they end when the process exits.

---

## Race Conditions

### Condition 1: Async Module Initialization ⚠️ REAL

```go
var CacheModule = func() owlky.Module {
	m := owlky.ModuleBuilder().Providers(CacheService{}).Build()
	m.OnInit = func() {
		go client.Connect("redis://...")
	}
	return m
}()

func (instance ChatController) SUBSCRIBE_chat(payload owlky.WSPayload) string {
	instance.CacheService.Set("key", "value")
	return "ok"
}
```

Timeline: `OnInit` returns while `Connect` is pending → `OnReady` → `Listen` → a client connects →
the handler calls the cache before the connection exists.

**Framework Guarantee**: none for goroutines a hook starts. Connect in a module factory instead
(`Imports` to wait at `Create()`, `AsyncImports` to serve at once with the routes that need it gated).

### Condition 2: Import Order ✓ DEFINED

Imports are initialized in `Imports(…)` order, after the modules they import themselves. A provider
is constructed after the providers its exported fields name, whatever the module order, so the order
of `Imports(…)` matters only for the order hooks run in.

### Condition 3: Broker Subscriptions ✓ NOT POSSIBLE AT STARTUP

The broker is created in `core.New(opts)`; subscriptions only happen when a WebSocket client sends a
`subscribe` message, after the server is listening.

---

## Module Lifecycle Guarantees

### What IS Guaranteed

| Phase | Guarantee |
|-------|-----------|
| `OnInit()` | Returned before the module's providers are constructed |
| `OnReady()` | Every ready module's has returned before `Create` returns; a background module's runs right after its gate opens |
| Providers | Each constructed once per app, before `Create` returns, or before its module's gate opens |
| Controllers and routes | Registered before `Create` returns (gated ones behind a closed gate) |
| Gated routes | Answer `503` until every entry their module needs is `ready`; never after it fails |
| `OnShutdown()` | Runs once per app, after the drain, only for ready modules, in reverse ready order |

### What IS NOT Guaranteed

| Item | Why |
|------|-----|
| Async operations complete | The framework doesn't track goroutines you start (it tracks `AsyncImports` factories) |
| A ready module stays healthy | States are one way: run-time failures are the business code's job |
| External services ready | They are outside the framework |
| WebSocket connections drained at shutdown | Not implemented (roadmap 0005) |

---

## Message Safety (WebSocket)

| Scenario | Behavior | Loss Risk |
|----------|----------|-----------|
| Handler succeeds | Reply sent | None |
| Handler panics | Exception sent to the client | Message not retried |
| Slow client buffer full | Message dropped via `TrySend` | Lost |
| Process exits | In-memory broker state lost | High |

No ACK / NACK, no retry, no persistence: the broker is in memory (`memorybroker`).

---

## Recommendations

1. **Connect in a module factory**, not in `OnInit` or `NewProvider`: it can take `ctx` and return an
   error. In `Imports`, `Create` waits for it within the startup timeout.
2. **Use `AsyncImports` for a dependency that may be down at start**, with `owlky.Retry(ctx, …)` for
   transient errors (no attempt limit); return permanent errors (bad config) at once.
3. **Start background work in `OnReady`** (`go startWorkerPool()`): the module is built then.
4. **Release resources in `OnShutdown`**: it runs after the last request returned, for ready modules.
