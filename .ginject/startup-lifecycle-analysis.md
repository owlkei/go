# Startup Lifecycle & Race Condition Analysis

**Purpose**: Application startup sequence, module initialization guarantees, shutdown, and the race
conditions an app can still create for itself. The user-facing contract (hooks, server, shutdown) is
`core/README.md` › Modules and lifecycle and › Server; this page explains the implementation.

**Last Updated**: 2026-11-16 (roadmap 0003, designs 0008 and 0009)

---

## Critical Finding

⚠️ **Async work started in a hook is not tracked**: a request or WebSocket message can reach a
provider before work it started in a goroutine has finished, when:
1. The handler depends on a provider (for example a cache client)
2. A hook starts that provider's setup in a goroutine (`go connectToRedis()`)
3. A client connects during that async phase

The framework guarantees only that every synchronous hook has returned before the server starts.

---

## Startup Sequence

### Phase 1: App Creation (`core.New()`)

```
core.New()
  ├─ newHTTP()                       router and route tables of this app
  ├─ memorybroker.NewMemoryBroker()  the app's own broker (ready for pub/sub immediately)
  ├─ newContainer()                  the app's own modules, providers, layers, routes and events
  ├─ log.NewLog(nil)                 the built-in logger (replace it with SetLogger before Create)
  ├─ context pools (HTTP exchange, WS context), server defaults, body cap 10 MiB
  ├─ container.addInterface(common.Publisher)
  └─ BindGlobalExceptionFilters(built-in HTTP and WS filters)
```

Every `core.New()` is independent: there is no package-level registry of routes, events, providers or
layers (design 0009). The app's `trace.Tracer` is a zero value (no listener).

### Phase 2: App Initialization (`app.Create(m *Module)`, `core/app.go`)

Sequential, on the calling goroutine:

```
Create(m)
  ├─ m == nil          → panic "invalid module: Create needs a root module"
  ├─ second call       → panic "invalid app: Create called twice"
  ├─ container.addInterface(common.Logger)
  ├─ container.wire(m)                      core/container.go
  │    ├─ addGlobals(root)                  the root's providers are global
  │    ├─ discover(root)                    depth first, each module once:
  │    │     IsGlobal → its providers become global;
  │    │     module funcs are called here (arguments from global providers, then Logger, Publisher);
  │    │     visible providers and controllers per module, prefixes per controller
  │    ├─ for each module in init order: OnInit(), then provide() each of its providers
  │    │     (a provider is constructed after the providers its fields name, once per app)
  │    └─ injectController() for every controller of the tree (fields injected, then NewController)
  ├─ initWS()                               only when EnableWS was called
  ├─ initRoutes(), initMiddlewares(), initExceptionFilters(), initGuards(),
  │  initInterceptors(), initMainHandlers()  layers constructed once per type and configuration,
  │                                          handler plans built (core/fn_plan.go)
  ├─ app.http.finishRoutes()                one immutable route struct per route, fixed 404/405 bodies
  ├─ initDevtool()                          after routes and layers exist
  ├─ initAccessLog()                        the tracer's only listener, when EnableAccessLog was called
  └─ callOnReady()                          OnReady of every module, in init order; then isCreated = true
```

**Init order**: a module after every module it imports, imports in `Imports(…)` order, each module once
per app. Root importing A and C, both importing B: B, A, C, root.

**Key Points**:
- A panic in `OnInit` or `OnReady` stops `Create` and reaches the caller unchanged. An app whose
  `Create` did not finish runs no `OnShutdown`.
- `Create` never writes to the modules it is given: the same module value may be given to several apps.
- Configuration methods (`BindGlobal…`, `Enable…`, `SetLogger`) panic after `Create`:
  `invalid app: <Method> must be called before Create`.

### Phase 3: Server Start (`app.Listen(port, nil)`, `ListenTLS`, `Serve`; `core/server.go`)

```
app.Listen(port, opts)
  ├─ checks (before Create, already serving, stopped, option conflicts) → return an error
  ├─ bind :<port>          a bind error returns here: nothing logged, no hook runs
  ├─ log InstanceLoader (one line per named module), RouteExplorer (one per route),
  │  WebSocketEvent (one per event), ServerListening (address, tls, protocols)
  └─ serve until SIGINT / SIGTERM / app.Stop() or a server error
```

The server accepts connections as soon as `ServerListening` is logged.

### Phase 4: Shutdown (`core/server.go`)

```
SIGINT / SIGTERM / app.Stop()
  ├─ stop intercepting signals (a second signal ends the process the default way)
  ├─ close the listener and idle keep-alive connections
  ├─ drain running requests for ShutdownTimeout (default 30 s), then close their connections
  ├─ runShutdownHooks()   once per app (app.shutdownOnce):
  │     OnShutdown of every module in the exact reverse of the init order (root first);
  │     a panicking hook is logged as OnShutdownPanic (module, panic) and the others still run;
  │     then the access log is stopped, so its last lines are written
  └─ the serving call returns nil, "core: shutdown timed out after <timeout>" or "core: serve: <err>"
```

`app.Stop()` on an app that is not serving runs the hooks once and marks the app stopped. WebSocket
connections are not drained: they end when the process exits.

---

## Race Conditions

### Condition 1: Async Module Initialization ⚠️ REAL

```go
var CacheModule = func() *core.Module {
	m := core.ModuleBuilder().Providers(CacheService{}).Build()
	m.OnInit = func() {
		go client.Connect("redis://...")
	}
	return m
}()

func (instance ChatController) SUBSCRIBE_chat(payload ctx.WSPayload) string {
	instance.CacheService.Set("key", "value")
	return "ok"
}
```

Timeline: `OnInit` returns while `Connect` is pending → `OnReady` → `Listen` → a client connects →
the handler calls the cache before the connection exists.

**Framework Guarantee**: none for goroutines a hook starts.

### Condition 2: Import Order ✓ DEFINED

Imports are initialized in `Imports(…)` order, after the modules they import themselves. A provider
is constructed after the providers its exported fields name, whatever the module order, so the order
of `Imports(…)` matters only for the order hooks run in.

### Condition 3: Broker Subscriptions ✓ NOT POSSIBLE AT STARTUP

The broker is created in `core.New()`; subscriptions only happen when a WebSocket client sends a
`subscribe` message, after the server is listening.

---

## Module Lifecycle Guarantees

### What IS Guaranteed

| Phase | Guarantee |
|-------|-----------|
| `OnInit()` | Returned before the module's providers are constructed |
| `OnReady()` | Every module's has returned before `Create` returns, so before any serving call |
| Providers | Each constructed once per app, before `Create` returns |
| Controllers and routes | Registered before `Create` returns |
| `OnShutdown()` | Runs once per app, after the drain, in reverse init order |

### What IS NOT Guaranteed

| Item | Why |
|------|-----|
| Async operations complete | The framework doesn't track goroutines |
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

1. **Connect synchronously in `OnInit`** when a provider needs a ready connection: `Create` waits for it.
2. **Start background work in `OnReady`** (`go startWorkerPool()`): every module is initialized then.
3. **Fail fast**: panic in `OnReady` when a dependency isn't ready; `Create` stops with that panic.
4. **Release resources in `OnShutdown`**: it runs after the last request returned.
