# Ginject AI Knowledge Base — Complete Index

**Purpose**: Central navigation point for comprehensive AI-friendly documentation of Ginject framework.

**Optimization**: Machine-readable, minimal prose, organized by access pattern.

---

## Quick Navigation by Use Case

### "I need to understand how Ginject works"
1. Start: [architecture-core-concepts.md](architecture-core-concepts.md)
2. Then: [request-pipeline.md](request-pipeline.md)
3. Details: [handler-execution.md](handler-execution.md)

### "I'm debugging a framework issue"
1. Start: [anti-patterns-gotchas.md](anti-patterns-gotchas.md)
2. Look up: [metadata-lifecycles.json](metadata-lifecycles.json)
3. Trace: [metadata-concurrency-model.json](metadata-concurrency-model.json)

### "I need to implement a feature"
1. Reference: [ai-guidance-quick-reference.md](ai-guidance-quick-reference.md)
2. Details: [routing-system.md](routing-system.md) or [websocket-system.md](websocket-system.md)
3. APIs: [package-reference.md](package-reference.md)

### "I need to understand performance"
1. Start: [metadata-performance.json](metadata-performance.json)
2. Details: [architecture-core-concepts.md#6-concurrency-model](architecture-core-concepts.md)

### "I need to understand code organization"
1. Start: [modules-and-providers.md](modules-and-providers.md)
2. Reference: [package-reference.md](package-reference.md)

---

## Document Catalog

### Architecture & Design

| Document | Purpose | Audience | Format |
|----------|---------|----------|--------|
| [architecture-core-concepts.md](architecture-core-concepts.md) | Core framework concepts, design patterns, lifecycles | All | Markdown |
| [startup-lifecycle-analysis.md](startup-lifecycle-analysis.md) | Startup sequence, module initialization, race condition analysis | Developers | Markdown |
| [routing-system.md](routing-system.md) | URL routing, naming conventions, pattern matching | Developers | Markdown |
| [handler-execution.md](handler-execution.md) | Handler invocation, dependency resolution, parameter injection | Developers | Markdown |
| [request-pipeline.md](request-pipeline.md) | Request lifecycle, middleware, guards, interceptors, filters | Developers | Markdown |
| [websocket-system.md](websocket-system.md) | WebSocket connections, pub/sub, fanout semantics | Developers | Markdown |
| [modules-and-providers.md](modules-and-providers.md) | Module system, dependency injection, composition | Developers | Markdown |

### Reference & API Documentation

| Document | Purpose | Audience | Format |
|----------|---------|----------|--------|
| [package-reference.md](package-reference.md) | Complete public API reference by package | Developers | Markdown |
| [metadata-api-index.json](metadata-api-index.json) | Machine-readable API index | AI Agents | JSON |
| [metadata-concurrency-model.json](metadata-concurrency-model.json) | Thread safety guarantees and race condition scenarios | Developers | JSON |
| [metadata-performance.json](metadata-performance.json) | Performance characteristics and bottlenecks | Performance Engineers | JSON |
| [metadata-lifecycles.json](metadata-lifecycles.json) | Detailed state machines for app/request/connection | Developers | JSON |

### Guidance & Best Practices

| Document | Purpose | Audience | Format |
|----------|---------|----------|--------|
| [conventions.md](conventions.md) | Code conventions measured from the code: files, naming, options, errors, tests | All | Markdown |
| [anti-patterns-gotchas.md](anti-patterns-gotchas.md) | Common mistakes, anti-patterns, what NOT to do | All | Markdown |
| [ai-guidance-quick-reference.md](ai-guidance-quick-reference.md) | Decision trees for common scenarios | AI Agents | Markdown |

---

## Document Dependency Graph

```
architecture-core-concepts.md (foundation)
├── request-pipeline.md (depends on core concepts)
├── handler-execution.md (depends on core concepts)
├── routing-system.md (depends on core concepts)
├── websocket-system.md (depends on core concepts)
├── modules-and-providers.md (depends on core concepts)
└── anti-patterns-gotchas.md (references all)

metadata-*.json (independently useful, cross-reference each other)

ai-guidance-quick-reference.md (depends on all, provides summaries)

package-reference.md (independent API reference)
```

---

## Core Framework Concepts (Quick Summary)

### 1. Module → Controller → Provider Pattern
- **Module**: Composition root (controllers, providers, imports), built with `core.ModuleBuilder()…Build()`
- **Controller**: HTTP/WS handler container (embeds `common.HTTP` and/or `common.WS`)
- **Provider**: Injected business logic, constructed once per app

### 2. Request Lifecycle (the spec: [request-pipeline.md](request-pipeline.md) § 1)
```
global middleware → module middleware → global guard → module guard →
global pre-interceptor → module pre-interceptor → main handler →
module post-interceptor → global post-interceptor → response
error in a middleware      → the HTTP layer's fixed error response (no exception filter)
error from a guard onwards → module exception filter → global exception filter → built-in filter
```

### 3. Dependency Injection (Reflection at `Create()` only)
- Handler parameters are planned once at `Create()` (`core/fn_plan.go`); a request runs the plan
- No tags needed: provider fields match by type identity (package path + type), handler parameters by type identity
- An unresolvable dependency panics at `Create()` (`dependency injection: …`)

### 4. WebSocket Model
- NOT streaming: RPC-over-WS with pub/sub
- Each message = complete request/response cycle; middlewares run only at the handshake
- Fanout via the injected `common.Publisher`

### 5. Exception Handling
- A panic from a guard onwards reaches the route's exception filters; a middleware panic gets the fixed error response
- Only exceptions reach the client with their code and message; any other value answers a fixed 500 and is logged (`UnhandledPanic`, with at most 4 KiB of stack)

### 6. Server and Lifecycle
- `app.Listen(port, nil)`, `app.ListenTLS(port, cert, key, nil)`, `app.Serve(listener, nil)`; options in `*core.ServerOptions` (`ShutdownTimeout`, `MaxRequestBodyBytes`, `TLSConfig`, `H2C`)
- Graceful shutdown on SIGINT / SIGTERM / `app.Stop()`, then `OnShutdown` hooks in reverse init order
- One logger API: `app.SetLogger(l)` before `Create`, read with `app.Logger()`
- Request tracing: one internal `trace.Tracer` per app; `EnableAccessLog()` is its only listener
- Details: `core/README.md`, [startup-lifecycle-analysis.md](startup-lifecycle-analysis.md)

---

## Critical Rules (Memorize)

| Rule | Why | Consequence |
|------|-----|------------|
| Never hold context after request | Context is pooled, reused | Stale data, wrong request |
| Never keep a `next` across requests | `next()` belongs to one request | It does nothing on a later request |
| Code after `next()` is not "after the handler" | The chain is linear | Use an interceptor's `Pipe` operators for post-handler work |
| Never call NewProvider() manually | The app constructs each provider once and injects its fields first | Uninjected fields, a second instance |
| Never access raw conn in WS | Concurrent with fanout | Message corruption |
| Configuration methods before `Create` | They panic after it | `invalid app: <Method> must be called before Create` |
| Provider type must match field type exactly | Keys are package path + type | Dependency resolution fails at `Create()` |

---

## Common Scenarios & Quick Solutions

### "I need to create an HTTP endpoint"
**Files to read**:
1. [routing-system.md](routing-system.md#1-route-naming-convention)
2. [ai-guidance-quick-reference.md](ai-guidance-quick-reference.md#decision-tree-how-do-i-implement-this-feature)

**Steps**:
1. Embed `common.HTTP` in controller
2. Create method: `READ()` / `CREATE()` / etc.
3. Add parameters (injected automatically)
4. Register in `ModuleBuilder().Controllers(...)`

### "I need to validate input"
**Files to read**:
1. [anti-patterns-gotchas.md#guard-returning-false](anti-patterns-gotchas.md#32-gotcha-panic-in-guard)
2. [ai-guidance-quick-reference.md](ai-guidance-quick-reference.md#decision-tree-how-do-i-implement-this-feature)

**Steps**:
1. Preferred: a pipe (a value type with `Transform`, see `common/pipe.go`) that binds and validates one parameter, panicking an exception on bad input
2. Or validate in the handler and panic `exception.BadRequestException(...)`
3. Guards decide access (`true` / `false`), not input shape

### "I need authentication"
**Files to read**:
1. [request-pipeline.md#3-guard-layer](request-pipeline.md)
2. [ai-guidance-quick-reference.md](ai-guidance-quick-reference.md)

**Steps**:
1. Create Guard struct with CanActivate()
2. Check JWT/token in httpCtx.Request.Header
3. Return true (authorized) or false (forbidden)
4. Bind to handlers: `c.BindGuard(guard, handler1, handler2)`

### "I need WebSocket"
**Files to read**:
1. [websocket-system.md](websocket-system.md)
2. [ai-guidance-quick-reference.md](ai-guidance-quick-reference.md)

**Steps**:
1. Call `app.EnableWS(&core.WSConfig{Path: "/ws"})` before `app.Create()`
2. Embed `common.WS` in controller
3. Create method: `SUBSCRIBE_chat_PERSON_ANY()` (see [websocket-system.md](websocket-system.md))
4. Inject `common.Publisher` for fanout
5. Call `publisher.Publish(topic, data)`

### "I need to handle errors"
**Files to read**:
1. [request-pipeline.md#5-exception-filter-layer](request-pipeline.md)
2. [exception handling section](ai-guidance-quick-reference.md#decision-tree-how-do-i-handle-this-error)

**Steps**:
1. Panic with `exception.BadRequestException(...)`
2. From a guard onwards the route's exception filters catch it (module, then global, then the built-in one)
3. The built-in filter writes `{"code","error","message"}`; bind a custom filter with `BindExceptionFilter` / `BindGlobalExceptionFilters`
4. A panic in a middleware skips the filters: the HTTP layer writes the same fixed format

### "I need a database connection"
**Files to read**:
1. [modules-and-providers.md](modules-and-providers.md)
2. [handler-execution.md#3-provider-instantiation](handler-execution.md)

**Steps**:
1. Create Provider struct
2. Implement `NewProvider() Provider`
3. Register in `ModuleBuilder().Providers(...)`
4. Inject into handlers/controllers
5. Framework resolves automatically

---

## Machine-Readable Metadata

All `metadata-*.json` files contain structured data for AI parsing:

**metadata-api-index.json**:
- Public types and methods per package
- Interface definitions
- Dependency types
- Critical rules

**metadata-concurrency-model.json**:
- Thread safety guarantees
- Race condition scenarios
- Goroutine models
- Synchronization mechanisms

**metadata-performance.json**:
- Latency breakdowns
- Throughput characteristics
- Bottlenecks
- Optimization opportunities

**metadata-lifecycles.json**:
- State machines for app/request/connection
- Initialization order (SACRED)
- Provider lifecycle
- Context mutations

---

## For AI Code Generation Agents

**Before generating code**:
1. Read [ai-guidance-quick-reference.md](ai-guidance-quick-reference.md)
2. Consult decision trees for use case
3. Check [anti-patterns-gotchas.md](anti-patterns-gotchas.md) for what NOT to do
4. Reference [package-reference.md](package-reference.md) for correct APIs

**Common mistakes to avoid**:
- Holding context after request
- Type mismatch (provider vs field or parameter)
- Expecting code after `next()` to run after the handler
- Accessing raw websocket.Conn
- Manual NewProvider() calls
- APIs remembered from older versions: check `core/README.md` and `go doc ./core` first

**Performance-conscious generation**:
- Reflection runs at `Create()`, not per request; per-request costs are in `core/README.md` › Benchmarks
- A non-exception panic costs about 25 µs (stack capture for the log): panic exceptions
- Profile before optimizing

---

## Version & Updates

**Knowledge Base Version**: 1.2  
**Last Updated**: 2026-11-16 (roadmap 0003: dispatch, server, per-app container, `trace.Tracer`)

### 2026-11-16 Update (roadmap 0003)

Designs 0007-0010: the request lifecycle is a written spec; `Listen(port, opts)`, `ListenTLS`, `Serve`, `Stop` and `ServerOptions`; graceful shutdown; modules, providers, layers and routes are per app (two apps in one process are independent); `SetLogger` / `Logger()` are the only logger API; `trace.Tracer` is the tracing primitive. Pages rewritten against the code: startup-lifecycle-analysis, modules-and-providers, the lifecycle and API metadata, and the provider / panic sections of every other page.

### 2026-09-25 Correction Pass

A source-verified audit found and fixed fabricated APIs (`common.REST`, `App.SetMaxBodySize`, per-request deadlines, `memorycache` persistence, `Guarder` / `Interceptable` / `ExceptionFilterable` as compile-time interfaces: they are `any`, checked by method shape). Spot-check anything load-bearing against source before trusting it.

---

## Related Documentation

**In Repository**:
- `CLAUDE.md` — Framework overview, commands and workflow
- Package READMEs (`core/README.md`, `log/README.md`, `trace/README.md`, `accesslog/README.md`, `modules/*/README.md`, …) — per-package API docs not duplicated here

**`ARCHITECTURE_REVIEW_AND_ROADMAP.md`, `IMPLEMENTATION_PLAN.md`, and `BUILDING_WITH_GINJECT.md` do not exist anywhere in this repository** — these were referenced by earlier versions of this doc but were never real files (verified 2026-09-25). Do not tell a user to consult them.

**External**:
- Go stdlib `net/http` — HTTP fundamentals
- Go stdlib `reflect` — Reflection details
- `golang.org/x/net/websocket` — WebSocket implementation

---

## How to Use This Knowledge Base

### For Developers
1. Start with [architecture-core-concepts.md](architecture-core-concepts.md)
2. Jump to specific topics as needed
3. Reference [package-reference.md](package-reference.md) for API details
4. Check [anti-patterns-gotchas.md](anti-patterns-gotchas.md) before implementing

### For AI Agents (Claude Code, etc.)
1. Load this INDEX.md as context
2. Use [ai-guidance-quick-reference.md](ai-guidance-quick-reference.md) for decision trees
3. Consult [metadata-*.json](metadata-*.json) for structured data
4. Verify against [anti-patterns-gotchas.md](anti-patterns-gotchas.md) before generating code
5. Reference [package-reference.md](package-reference.md) for exact APIs

### For Code Reviewers
1. Use [anti-patterns-gotchas.md](anti-patterns-gotchas.md) to identify issues
2. Check against [request-pipeline.md](request-pipeline.md) for pipeline violations
3. Verify concurrency safety with [metadata-concurrency-model.json](metadata-concurrency-model.json)
4. Confirm initialization order matches [metadata-lifecycles.json](metadata-lifecycles.json)

---

The code is the source of truth: when this knowledge base disagrees with it, fix the doc in the same change.
