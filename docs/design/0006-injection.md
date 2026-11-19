# 0006: Handler injection (`core/fn.go`)

Status: Approved
Packages: `core` (`fn.go`, and its call sites in `app.go`, `module.go`, `ws.go`, `read_loop.go`)

Roadmap: `docs/roadmap/0002-request-hot-path.md`, step 6 (hot spot 1). Depends on designs 0003 and
0005. The rest of `core` (server, `handleRequest`, 404 path, lifecycle) is roadmap 0003.

## Goal
Each handler gets an argument plan once, at `Create()`; a request only runs it. Calling a handler
costs one reflection hop instead of two and no longer rebuilds its pipe DTOs: about 550 → 240 ns for
a handler without arguments, about 1,400 → 330 ns and 11 → 3 allocations for a handler with one
pipe. Seven bugs are fixed, among them a handler that returns a pointer answering with a memory
address. Handler signatures, injectable types and `Transform` order stay.

## Bugs found
Each was reproduced with a throwaway probe against `master` (b2d1070); nothing was left in the tree.

| # | Where | Input | Today | Rule |
|---|---|---|---|---|
| 1 | `core/app.go:78-81`, `core/fn.go:71` | A handler parameter whose type prints like a built-in but is another type: `*ctx.HTTPContext` of a user package named `ctx`, `*websocket.Conn` of gorilla/websocket, `*http.Request` of another `http` | The key is `"/" + Type.String()` (pointers have no package path), so `Create()` accepts it and every request answers 500 `reflect: Call using *ctx.HTTPContext as type *ctx.HTTPContext` | B1, B2 |
| 2 | `core/fn.go:392-395` | `func (…) READ_user() *User`, or `(int, *User)` | The response is the pointer's address as text: `200 "0x16d14ddb06d0"` | B19 |
| 3 | `core/fn.go:362-367` | `func (…) READ_x() any { return "ok" }` | The static kind is `Interface`, so the string goes to `c.JSON`, which reads a string as raw JSON: 500 `json: error calling MarshalJSON …`. The same handler behind an interceptor answers `ok` (`core/http.go:183` uses the dynamic value) | B18 |
| 4 | `core/fn.go:361-402` | A handler that returns an array (`[3]int`) | No case: empty 200 | B19 |
| 5 | `core/fn.go:497`, `:511` | A pipe whose `Transform` returns nil or another type (`return &dto`) | `reflect.Value.Call` panics; the client gets 500 with `reflect: Call using *bench.searchQuery as type bench.searchQuery` | B11 |
| 6 | `core/fn.go:88`, `:178` | A pipe that is not a struct (`type UserID int` with `Transform(ctx.Param, …)`), or a pointer parameter `*DTO` | `Create()` dies with the raw `reflect: NumField of non-struct type core.UserID` | B4, B5 |
| 7 | `core/app.go:303-305`, `:323` | A `SERVE_…` route with a trace listener | `nameByPattern` is keyed by the `[SERVE]` pattern, the request by the `[GET]` one: the handler event has an empty `Name` | B16 |

Dead code: `toWSMessage` (`fn.go:405-451`) has no caller; it is deleted with its tests and benchmarks.

Observed, not changed here: a middleware's panic goes straight to the built-in filter and skips the
user's filters (`fn.go:572-576`); WS handlers emit no handler trace event (`app.go:247-251`);
`Body()` panics a raw error on malformed JSON, so the client gets a 500 (`ctx/http_body.go:37-39`;
fixed by design 0003, B9).

## Public API
No exported symbol of `core` changes. User-facing contract, unchanged: handler method signatures,
the injectable types (B1), `Transform` order (B9), results (`()`, `(T)`, `(int, T)`).

Behavior changes a user can see (**breaking** where marked):
- Bugs 1, 5, 6: clearer failures, at `Create()` where possible.
- Bugs 2, 4: pointers and arrays are written as JSON.
- **Breaking, small (Q2):** a result declared as an interface type is written by its dynamic type
  (B18). A string in an `any` becomes text instead of a 500; a number or bool in an `any` becomes
  `text/plain` instead of `application/json` (same digits, except that floats follow `strconv`'s
  `'g'` format: `1e+08` instead of `100000000`).

Unexported API (the contract for tests and the builder), all in `core/fn.go`:

```go
type httpHandlerPlan struct { /* immutable after newHTTPHandlerPlan */ }
type wsHandlerPlan struct { /* immutable after newWSHandlerPlan */ }

// recv is the controller value returned by NewController; method is one of recv.Type()'s methods.
// Both panic at bootstrap ("dependency injection: …", color.FmtRed) when the handler can't be planned.
func newHTTPHandlerPlan(recv reflect.Value, method reflect.Method, injectedProviders map[string]Provider) *httpHandlerPlan
func newWSHandlerPlan(recv reflect.Value, method reflect.Method, injectedProviders map[string]Provider) *wsHandlerPlan

// Resolve the arguments, call the handler once, return its results in order.
func (p *httpHandlerPlan) invoke(c *ctx.HTTPContext, ev *event.Event) []reflect.Value
func (p *wsHandlerPlan) invoke(c *ctx.WSContext, ev *event.Event) []reflect.Value

func returnHTTP(c *ctx.HTTPContext, data reflect.Value)          // unchanged signature, B18-B20
func setStatusCode(c *ctx.HTTPContext, statusCode reflect.Value) // unchanged
```

Removed (unexported): `invokeHTTPHandlerByProviders`, `invokeWSHandlerByProviders`,
`getHTTPDependency`, `getWSDependency`, `getFnArgs`, `getFnArgsByType`, `classifyArgType`,
`argClassification`, `argClassificationCache`, `isInjectableHandler`, `toWSMessage`,
`knownHTTPDependencyKeys`, `knownWSDependencyKeys` and the key constants of `app.go:78-91` (only
`publisherKey` stays, `globalInterfaceByKey` uses it). `createStaticModuleFromDynamicModule` loops
over its function's parameters with `genFieldKey` itself; its behavior and messages are unchanged.

Unchanged in `fn.go`: `injectDependencies`, `buildFieldInjectionCallback`, `isInjectedProvider`, the
key helpers, `toUniqueControllers`, `getLocalIP`, `logBoostrap`, `buildUseMiddleware`,
`traceHTTPHandler`, `traceHTTPCatch`, `traceWSHandler`, `traceWSCatch`.

### Expected from ctx (design 0003)
Injection needs only this; stated so the two designs can be reconciled.

- `(*HTTPContext).Body()`, `Form()`, `Query()`, `Header()`, `Param()`, `File()`, and
  `(*WSContext).WSPayload()`: computed at most once per request, the same value on every call, reset
  by `Reset`. A failure is a panicked `exception.Exception` (413 for an oversized body today).
- `c.Next` (field, `ctx.Next`), `c.Redirect` (method), `c.GetID()`.
- **Binding prepared at `Create()`:** `ctx.BinderOf(t reflect.Type) (*Binder, error)` (0003) builds
  and caches the field plan of a struct type. `newHTTPHandlerPlan` and `newWSHandlerPlan` call it
  once for each struct pipe type and ignore the result: it warms the cache, so `Body.Bind`,
  `Query.Bind`, `Form.Bind`, `Header.Bind`, `Param.Bind`, `File.Bind` and `WSPayload.Bind` of that
  type walk no types on a request. Injection never runs a binding itself: `Transform` is user code
  and calls `Bind`; that order stays, so the plan holds no `*Binder`. Non-struct pipes (B4) are not
  passed to it (`ErrBindTarget`). If 0003 changes or drops `BinderOf`, only this one call changes.
- Malformed JSON is an exception of code 400 from `Body()` (0003, B9), no longer a raw error.

## Behavior

### Building the plan (`Create()`)
- **B1 Parameters are classified by type identity** (`reflect.Type` equality), in this order:

  | Parameter type | HTTP handler | WS handler | Argument |
  |---|---|---|---|
  | `*ctx.HTTPContext` | yes | no | `c` |
  | `*http.Request` | yes | no | `c.Request` |
  | `http.ResponseWriter` | yes | no | `c.ResponseWriter` |
  | `ctx.Body`, `ctx.Form`, `ctx.Query`, `ctx.Header`, `ctx.Param`, `ctx.File` | yes | no | `c.Body()`, `c.Form()`, `c.Query()`, `c.Header()`, `c.Param()`, `c.File()` |
  | `ctx.Redirect` (`func(string)`) | yes | no | `c.Redirect` |
  | `*ctx.WSContext` | no | yes | `c` |
  | `*websocket.Conn` (`golang.org/x/net/websocket`) | no | yes | `c.Conn` |
  | `ctx.WSPayload` | no | yes | `c.WSPayload()` |
  | `ctx.Next` (`func()`) | yes | yes | `c.Next` |
  | `common.Publisher` | yes | yes | the value stored under `publisherKey` when the plan is built |
  | a non-interface, non-pointer type that implements `common.ContextPipeable`, `BodyPipeable`, `FormPipeable`, `QueryPipeable`, `HeaderPipeable`, `ParamPipeable` or `FilePipeable` | yes | no | B10 |
  | a non-interface, non-pointer type that implements `common.WSPayloadPipeable` | no | yes | B10 |

  A nil `c.ResponseWriter` or publisher is passed as a nil value of the parameter's type, without a
  panic.
- **B2 Anything else panics at `Create()`** with today's text: `"dependency injection: can't resolve
  dependencies of the '<func type>'. Please make sure that the argument dependency at index [<i>] is
  available in the handler"`. `<func type>` is the handler's type without its receiver
  (`func(int) string`), `<i>` the parameter's index. Cases: an unknown type; a variadic parameter; a
  "no" cell of B1 (a `ctx.Body` in a WS handler, a `WSPayloadPipeable` in an HTTP handler); an
  interface type; a type that only prints like a built-in (bug 1); `common.Publisher` when nothing
  is stored under `publisherKey`.
- **B3 Struct pipes** get one prototype per parameter: a value of the parameter's type whose fields
  are filled by the rules of `injectDependencies` (`fn.go:172-219`: a provider of `injectedProviders`,
  else a global provider, else a global interface, else the zero value) with its panics unchanged
  (unexported field, unresolved provider).
- **B4 Non-struct pipes** (`type UserID int`) are accepted; the prototype is the zero value (bug 6).
- **B5 Pointer pipes** (`*DTO` whose method set has `Transform`) panic at `Create()`:
  `"dependency injection: pipe '<type>' of the '<func type>' must not be a pointer"`.
- **B6** Building a plan calls neither the handler nor any `Transform`. A plan never changes after
  it is built.

### Invoking (per request)
- **B7** `invoke` calls the handler exactly once, on the controller value given at build, with one
  argument per parameter as B1 defines, and returns its results in declaration order
  (`len == NumOut`).
- **B8 Same arguments as today.** For every row of B1 and every pipe kind, the handler receives what
  `invokeHTTPHandlerByProviders` / `invokeWSHandlerByProviders` pass today (`fn.go:286-358`,
  `:514-536`): identical pointers for `c`, `c.Request`, `c.Conn`; the same map for body, form, query,
  header, param, file and payload; `Next` and `Redirect` act on `c`.
- **B9 Order.** Arguments are resolved left to right, all before the handler runs; `Transform` calls
  happen in parameter order. When resolving argument *i* panics (an accessor's exception, a
  `Transform` panic), the panic propagates unchanged, later arguments are not resolved and the
  handler is not called. A handler's panic propagates unchanged.
- **B10 Pipes.** `Transform` is called once per request with the source of its kind (context → `c`,
  body → `c.Body()`, form → `c.Form()`, query → `c.Query()`, header → `c.Header()`, param →
  `c.Param()`, file → `c.File()`, wsPayload → `c.WSPayload()`) and
  `common.ArgumentMetadata{ParamType: <the kind's key>}` (`ContextType`: design 0005, Q3). Its
  receiver equals the prototype of B3 or B4 on every request: a `Transform` that changes its
  receiver's fields does not affect the next request or a concurrent one.
- **B11 Wrong result.** When `Transform` returns nil or a value that is not assignable to the
  parameter's type, the request panics `exception.InternalServerErrorException("pipe <type>:
  Transform returned <dynamic type or nil>, want <type>")` and the handler is not called (bug 5).
  A value of an assignable type is passed as is.

### Tracing
- **B12** Without a listener on `trace.EventName`, `invoke` emits nothing and reads no clock. The
  arguments and results are the same with and without a listener.
- **B13** With a listener, each pipe argument emits one `trace.Event{ID: c.GetID(), Stage:
  trace.StagePipe, Name: <parameter type's String()>, Transport: trace.TransportHTTP or
  trace.TransportWS, Duration}`, in parameter order, also when its `Transform` panics or returns a
  wrong value. Other arguments emit nothing.
- **B14** HTTP, with a listener: after the handler returns or panics, one `trace.Event{ID, Stage:
  trace.StageHandler, Name: <method name>, Duration}` (no `Transport`, as today), where `Duration`
  is the time since `invoke` started minus the pipes' durations. Not emitted when an argument failed.
- **B15** WS: no handler event (unchanged).
- **B16** The handler event's `Name` is the method name for every route, `SERVE_…` included (bug 7).
- **B17** A listener added or removed between two requests takes effect on the next `invoke`.

### Writing the result
- **B18** `returnHTTP(c, v)`: when `v` is of an interface type, a nil interface writes
  `c.JSON(nil)` (`null`, unchanged); otherwise its dynamic value is written by the table of B19
  (bug 3). An invalid `Value` (`reflect.ValueOf(nil)`, the interceptor path) writes nothing
  (unchanged).
- **B19** By kind of the (dynamic) value:

  | Kind | Response |
  |---|---|
  | `Map`, `Slice`, `Struct`, `Array` (new) | `c.JSON(value)` |
  | `Pointer`: nil → `c.JSON(nil)`; otherwise `c.JSON(pointer)` (new: was the address as text) | JSON |
  | `String` | `c.Text(s)` |
  | `Bool`, signed and unsigned integers, `Float32`, `Float64`, `Complex64/128` | `c.Text` with today's formatting (`fn.go:368-391`) |
  | `Func` | `c.Text(type string)` (unchanged) |
  | `UnsafePointer`, `Chan`, `Uintptr` | nothing is written (`UnsafePointer` wrote its address) |

- **B20** `setStatusCode` is unchanged: with two or more results, a first result that is an `int`
  (or an `any` holding one) with a known status text sets the code; anything else is ignored.

## Performance

| Function in `fn.go` | Path |
|---|---|
| `(*httpHandlerPlan).invoke`, `(*wsHandlerPlan).invoke`, `returnHTTP`, `setStatusCode` | hot: every request or message |
| The closures returned by `buildUseMiddleware`, `traceHTTPHandler`, `traceHTTPCatch`, `traceWSHandler`, `traceWSCatch` | hot; not changed here (see the end of this section) |
| `newHTTPHandlerPlan`, `newWSHandlerPlan`, `injectDependencies`, `buildFieldInjectionCallback`, `isInjectedProvider`, `createStaticModuleFromDynamicModule`, `isDynamicModule`, the key helpers, `snapshotGlobalProviders`, `toUniqueControllers`, the five builders above themselves, `getLocalIP`, `logBoostrap` | cold: `Create()` or `Listen()` |

Baseline (i7-9750H, Go 1.27.4; `invokeHTTPHandlerByProviders` on a prepared context, no listener):

| Handler | Today |
|---|---|
| `func() string` | 553 ns, 3 allocs (48 B) |
| `func(ctx.Param) ctx.Map` | 680 ns, 2 allocs |
| `func(dto) ctx.Map`, one query pipe with two fields, `Transform` returns its receiver | 1,395 ns, 11 allocs (184 B) |
| five built-in parameters | 1,560 ns, 4 allocs |
| `bench/`: Static / Param / Query / JSONBody | 1,717 ns, 13 allocs / 3,700 ns, 27 / 6,200 ns, 46 / 8,150 ns, 52 |

What a request pays today and the plan removes:

- **O1 The plan.** No per-request `reflect.TypeOf`, `NumIn`, `In`, `sync.Map` lookup per parameter
  (`fn.go:46`), string-keyed map lookup (`fn.go:488`), `switch` on strings (`fn.go:287`), or callback
  closure. Each argument is a precomputed step.
- **O2 Pipe prototypes.** Today every request and every pipe parameter runs `reflect.New` twice,
  `Interface()`, and `injectDependencies` (`path.Base`, a name concatenation, and per field a key
  concatenation, two `sync.Map` lookups and a `reflect.New` in `isInjectedProvider`):
  `BenchmarkGetFnArgsByType_MixedPipeable` measures 590 ns and 6 allocations for two pipes. The
  prototype is built once (B3); it is held as an interface value of a value-receiver type, so each
  `Transform` call gets its own copy (B10). Expected: about −300 ns and −3 to −8 allocations per pipe
  parameter, growing with the DTO's field count.
- **O3 One reflection hop.** `module.go:338` stores `controllerValue.Method(j).Interface()`: a func
  that reflection fabricates, whose every call re-enters `reflect.callMethod`. Calling it with
  `reflect.Value.Call` is reflection twice. The plan keeps the method itself
  (`reflect.Type.Method(i).Func`, receiver as first argument) and calls that. Measured on
  `func() string`: 443 → 210 ns, 3 → 2 allocations.
- **O4 Arguments on the stack.** The `[]reflect.Value` for `Call` is a local array for handlers with
  up to 8 parameters (it does not escape: measured); longer handlers use a slice.
- **O5 One listener check per call** (two today), and the traced path lives in its own functions, so
  the untraced path has no `defer` and no `time.Now`.
- **O6** The publisher is looked up once, at build.

Expected after (same benchmark): `func() string` about 240 ns, 2 allocs; `func(ctx.Param) ctx.Map`
about 280 ns, 1 alloc; one query pipe about 330 ns, 3 allocs; five parameters about 450 ns.
End to end (`make bench`): Static and Param about −300 to −400 ns and −1 allocation; Query and
JSONBody about −1.0 µs and −8 allocations each. What remains is the floor of `reflect.Value.Call`:
about 210 ns, one allocation for the result slice and one per result that is not pointer-shaped
(a string, a struct).

### Why there is no typed fast path
The brief asked to avoid `reflect.Value.Call` where a typed call is possible. Measured on
`func() string`:

| Way to call the method | Cost |
|---|---|
| Today: `reflect.ValueOf(stub).Call` | 443 ns, 3 allocs |
| `recv.Method(i).Call` | 240 ns, 3 allocs |
| Type-assert the stub to `func() string` and call it | 249 ns, 1 alloc |
| `Type.Method(i).Func.Call` with the receiver (O3) | 210 ns, 2 allocs |
| `unsafe`: the method's func value reinterpreted as `func(unsafe.Pointer) string` | 1.9 ns, 0 allocs |
| A method value written in user code (`instance.READ_static`) | 1.6 ns, 0 allocs |

Handlers are found by reflection over the controller's methods, and the only func reflection can
return for a method is the stub of row 3, which costs as much as `Call`. A typed call therefore
needs either `unsafe` or method values supplied by the user. Neither is in this design: Q1.

**Heavy techniques in this design: none.** No `unsafe`, no `sync.Pool`, no hand-managed buffer, no
inlining restructure.

Recorded for roadmap 0003, not done here: `buildUseMiddleware` keeps a `defer`/`recover` per
middleware per request (about 30 ns each; one `recover` in `handleRequest` could replace them once
unmatched routes have a filter chain); each trace wrapper takes `Event`'s read lock
(`event/event.go:132-137`, about 22 ns and a shared cache line; an atomic flag would do);
`handleRequest` finds the plan with a map lookup by pattern, which the `RouterItem.Injectable` of
design 0002 can carry instead.

## Concurrency
- A plan is built during `Create()` (serialized by `moduleGlobalMu`) and is read-only afterwards:
  every request of a route shares it without a lock. `argClassificationCache` (a `sync.Map` read per
  parameter per request) goes.
- Shared across requests and read-only: the method and receiver `reflect.Value`s, the pipe
  prototypes (B10: value receivers copy them), the publisher value. The controller value is shared
  as today; its provider fields are the user's to protect.
- Per request and on the goroutine's stack: the argument array. `invoke` writes no shared state.
- `returnHTTP` and `setStatusCode` touch only the request's context.
- No goroutine is started. `TestHTTPHandlerPlan_ConcurrentInvokeNoRace` runs one plan from 64
  goroutines under `-race`, with a pipe that modifies its receiver.

## Security
- `invoke` panics on purpose in one case, with an exception: B11. `reflect.Value.Call` cannot panic
  on its arguments: arity and the built-in types are fixed at build (B1, B2), pipe results are
  checked first (B11), nil interfaces are passed typed (B1).
- Panics reachable from client input pass through unchanged (B9): an accessor's exception (413 on an
  oversized body, 400 on malformed JSON once 0003 lands), and whatever a `Transform` or handler
  panics. `core/http.go:66-72` recovers them into the filter chain.
- Bug 2 was an information leak (heap addresses); B19 closes it. B11 no longer sends reflection's
  text; its message names the DTO type, as the old one did (0005, Q2 covers hiding such texts).
- Bug 1 let a handler compile, start and fail on every request; B2 stops the start instead.
- Nothing here sizes work by client input: the number of arguments is fixed at build. Body size,
  header count and path depth are bounded elsewhere (0002, 0003, `core/app.go`).

## Impact
- `core/module.go:324-363`, `:453-484`: where `isInjectableHandler` runs today, build the plan with
  the controller value and `controllerType.MethodByName(<handler name>)`, and keep it on the main
  module in unexported maps (`httpPlanByPattern`, key `HTTPLayer.Pattern`; `wsPlanByEvent`, key
  `WSLayer.EventName`). `HTTPLayer.Handler` stays the func: the router and the devtool read it.
- `core/app.go:303-331`: `resolveAndCallHandler` becomes a lookup and `plan.invoke(c, app.event)`.
  Plans are stored in `HTTP` under the key `handleRequest` passes (`routing.MethodRouteVersionToPattern(
  routing.OperationsMapHTTPMethods[h.Method], h.Route, h.Version)`, the expression of `app.go:340`).
  `nameByPattern` and the handler-event code move into `invoke`. `core/http.go` is not edited:
  `resolveAndCallHandler` keeps its signature and `[]reflect.Value` result.
- `core/app.go:247-251`, `core/ws.go:30`, `:37`, `core/read_loop.go:200`: the WS
  `resolveAndCallHandler` takes the event pattern (`func(pattern string, f any, c *ctx.WSContext)
  []reflect.Value`) to find its plan.
- `core/app.go:77-124`: the key constants and both `known…DependencyKeys` maps go (`publisherKey`
  stays).
- Tests of the removed functions (`core/fn_test.go:253-723`) are replaced by the plan's tests;
  `core/fn_bench_test.go` loses its `…_Old` copies.
- Docs: `CLAUDE.md` › Handler Injection (the sets are the table of B1, no longer two maps in
  `app.go`; the exception note about `invokeHandlerByProviders` panicking per request goes: an
  unresolvable argument is a `Create()` panic); `.owlky/handler-execution.md` (rewritten around the
  plan), `.owlky/request-pipeline.md`, `.owlky/anti-patterns-gotchas.md` (results: B18, B19).

### What `core/README.md` (roadmap 0003) should say about injection
1. The parameter table of B1 for HTTP and WS, and that anything else stops `Create()`.
2. Pipes: a value type with `Transform(source, common.ArgumentMetadata) any`; struct pipes get
   providers injected into exported fields; the receiver is fresh on every request; `Transform` runs
   in parameter order before the handler; a pointer parameter is refused; the result must have the
   parameter's type.
3. Results: nothing, one value, or `(status int, value)`; the table of B19.
4. Cost: arguments are planned once at `Create()`; a call is one `reflect.Value.Call` (about 210 ns
   and one allocation on the reference machine) plus whatever the accessors and pipes do.
5. Trace events: `pipe` per pipe argument, `handler` per HTTP call.

## Test plan
Internal tests (`package core`) in `core/fn_test.go`, one function per behavior, `test.DiffMessage`.
Fixtures: a controller struct with one method per case; contexts from `ctx.NewHTTPContext` /
`ctx.NewWSContext` with a prepared request. Bug 1 needs a second package named `ctx`, which a
`_test.go` file cannot provide: the orchestrator adds `internal/test/lookalike/ctx/ctx.go`
(`package ctx` with `type HTTPContext struct{}`, no logic) together with the tests.

| Rule | Test | Case |
|---|---|---|
| B1, B8 | `TestHTTPHandlerPlan_InjectsHTTPContext`, `_InjectsRequest`, `_InjectsResponseWriter`, `_InjectsBody`, `_InjectsForm`, `_InjectsQuery`, `_InjectsHeader`, `_InjectsParam`, `_InjectsFile`, `_InjectsNext`, `_InjectsRedirect`, `_InjectsPublisher`; `TestWSHandlerPlan_InjectsWSContext`, `_InjectsConn`, `_InjectsWSPayload`, `_InjectsNext`, `_InjectsPublisher` | the handler records its argument; pointer identity or map equality with the accessor; `Next` and `Redirect` are called and their effect checked |
| B1 | `TestHTTPHandlerPlan_NilResponseWriterIsTypedNil` | no panic, handler sees nil |
| B8 (fast path equals slow path) | `TestHTTPHandlerPlan_MatchesReferenceInvocation`, `TestWSHandlerPlan_MatchesReferenceInvocation` | one handler taking every row of B1 and every pipe kind (19 parameters HTTP, 6 WS), run on the same context by `invoke` and by a reference written in the test (today's algorithm: a fresh `injectDependencies` instance per pipe, `Transform`, `reflect.ValueOf(handler).Call`); recorded arguments and results are deeply equal. Covers the more-than-8-parameters path |
| B2 | `TestNewHTTPHandlerPlan_UnknownTypePanics`, `_VariadicPanics`, `_InterfaceParameterPanics`, `_WSTypeInHTTPHandlerPanics`, `_LookalikeTypePanics` (parameter `*ctx.HTTPContext` of the fixture package below), `_MissingPublisherPanics`; `TestNewWSHandlerPlan_HTTPTypePanics`, `_UnknownTypePanics` | message text, func type without receiver, index |
| B3 | `TestNewHTTPHandlerPlan_PipeGetsLocalProvider`, `_PipeGetsGlobalProvider`, `_PipeUnexportedFieldPanics`, `_PipeUnresolvedProviderPanics` | |
| B4 | `TestNewHTTPHandlerPlan_ScalarPipeAccepted` | `type id int`, param pipe, handler receives the transformed value |
| B5 | `TestNewHTTPHandlerPlan_PointerPipePanics` | message |
| B6 | `TestNewHTTPHandlerPlan_DoesNotCallHandlerOrTransform` | counters stay 0 |
| B7 | `TestHTTPHandlerPlan_ReturnsResultsInOrder`, `_NoResult`, `_UsesControllerValue` (a controller field is visible in the handler), `_CallsHandlerOnce` | 0, 1, 2 and 3 results |
| B9 | `TestHTTPHandlerPlan_TransformsRunInParameterOrder`, `_ArgumentPanicSkipsLaterArgumentsAndHandler`, `_HandlerPanicPropagates`, `_ExceptionFromAccessorPropagates` (oversized body → the 413 exception value) | |
| B10 | `TestHTTPHandlerPlan_ContextPipe`, `_BodyPipe`, `_FormPipe`, `_QueryPipe`, `_HeaderPipe`, `_ParamPipe`, `_FilePipe`, `TestWSHandlerPlan_PayloadPipe` (source and `ParamType`); `TestHTTPHandlerPlan_PipeReceiverIsFreshEachRequest`; `TestHTTPHandlerPlan_ConcurrentInvokeNoRace` | the receiver-mutating pipe; 64 goroutines |
| B11 | `TestHTTPHandlerPlan_TransformReturnsNil`, `_TransformReturnsPointer`, `_TransformReturnsOtherType`, `_TransformReturnsAssignableType`; WS: `TestWSHandlerPlan_TransformReturnsNil` | recovered value is an `exception.Exception`, code 500, message; handler not called |
| B12 | `TestHTTPHandlerPlan_NoListenerEmitsNothing`, `_TracedEqualsUntraced` | same recorded arguments and results on both paths |
| B13 | `TestHTTPHandlerPlan_PipeEmitsStagePipe`, `_TwoPipesEmitInOrder`, `_NonPipeEmitsNoStagePipe`, `_PipePanicStillEmitsStagePipe`, `_WrongPipeResultStillEmitsStagePipe`; `TestWSHandlerPlan_PipeEmitsStagePipe`, `_PipePanicStillEmitsStagePipe` | fields of the event; `Transport` |
| B14 | `TestHTTPHandlerPlan_EmitsStageHandler`, `_HandlerPanicStillEmitsStageHandler`, `_ArgumentFailureEmitsNoStageHandler`, `_HandlerDurationExcludesPipes` (a pipe that sleeps 20 ms: handler duration under 20 ms) | |
| B15 | `TestWSHandlerPlan_EmitsNoStageHandler` | |
| B16 | `TestApp_ServeRouteHandlerEventHasName` | through `ServeHTTP` with a listener |
| B17 | `TestHTTPHandlerPlan_ListenerAddedBetweenCalls` | |
| B18 | `TestReturnHTTP_InterfaceString`, `_InterfaceInt`, `_InterfaceMap`, `_NilInterface`, `_InvalidValueWritesNothing` | body and `Content-Type` |
| B19 | `TestReturnHTTPString`, `Map`, `Int`, `Bool`, `Slice`, `Float`, `Uint` (kept); `TestReturnHTTP_Struct`, `_Array`, `_PointerToStruct`, `_NilPointer`, `_Func`, `_UnsafePointerWritesNothing` | the pointer case asserts the body is the struct's JSON |
| B20 | `TestSetStatusCode…` (kept, four tests) | |
| bugs 1, 2, 3, 5 end to end | `TestApp_LookalikeParameterPanicsAtCreate`, `TestApp_HandlerReturningPointerWritesJSON`, `TestApp_HandlerReturningAnyStringWritesText`, `TestApp_PipeWrongResultIs500` | through `Create` and `ServeHTTP`; each resets the globals (`resetModuleGlobals`) |
| unchanged helpers | the existing tests of `injectDependencies`, `buildFieldInjectionCallback`, the key helpers, the trace wrappers, `buildUseMiddleware`, dynamic modules | kept as they are |

Benchmarks (`core/fn_bench_test.go`), all `for b.Loop()` + `b.ReportAllocs()`; before and after with
`-count 5` and `benchstat` in the commit. "Before" for the `invoke` rows is
`invokeHTTPHandlerByProviders` on the same fixtures, measured on the commit before the change.

| Benchmark | Before | Target |
|---|---|---|
| `BenchmarkHTTPHandlerPlan_Invoke_NoArgs` (`func() string`) | 553 ns, 3 allocs | under 280 ns, 2 allocs |
| `BenchmarkHTTPHandlerPlan_Invoke_Param` (`func(ctx.Param) ctx.Map`, returns nil) | 680 ns, 2 allocs | under 330 ns, 1 alloc |
| `BenchmarkHTTPHandlerPlan_Invoke_QueryPipe` (two-field DTO, `Transform` returns the receiver) | 1,395 ns, 11 allocs | under 400 ns, 3 allocs |
| `BenchmarkHTTPHandlerPlan_Invoke_BodyPipeWithProvider` (DTO with one provider field) | measure | at most the QueryPipe allocations |
| `BenchmarkHTTPHandlerPlan_Invoke_FiveBuiltins` | 1,560 ns, 4 allocs | under 550 ns |
| `BenchmarkHTTPHandlerPlan_Invoke_QueryPipe_Traced` (one listener) | measure | no target; reported |
| `BenchmarkWSHandlerPlan_Invoke_Payload`, `_PayloadPipe` | measure | same shape as HTTP |
| `BenchmarkNewHTTPHandlerPlan` (cold) | - | reported |
| `BenchmarkReturnHTTP_String`, `_Int`, `_Float`, `_Map`, `_PointerToStruct`, `_InterfaceString` | today's `…_New` numbers | no regression |
| `BenchmarkTraceHTTPHandler_NoListener` / `_WithListener`, `BenchmarkTraceHTTPCatch_NoListener`, `BenchmarkBuildUseMiddleware_NoListener` / `_WithListener` | converted, `…_Old` copies deleted | unchanged code |
| Cold helpers (`GenFieldKey`, `GenControllerKey`, `GetPkgFromControllerKey`, `IsDynamicModule`, `ToUniqueControllers`) | converted | - |
| Deleted: `BenchmarkGetFnArgsByType_…`, `BenchmarkToWSMessage…`, every `…_Old` | | |
| `make bench` (`bench/`) | the table in Performance | Static, Param: at least −250 ns; Query, JSONBody: at least −800 ns and −6 allocs |

## Open questions
- **Q1 A typed fast path needs a decision the roadmap has not taken.** After this design the handler
  call costs about 210 ns and 1-2 allocations in every scenario, so "Static within 1.5× of net/http"
  (180 ns, the end goal of roadmap 0003) cannot be reached while the call goes through reflection.
  Two ways out, neither in this design:
  1. `unsafe`: take the method's func value from `reflect.PointerTo(T).Method(i).Func` and call it
     as `func(unsafe.Pointer, …) …` (measured: 1.9 ns, 0 allocations). Constraints: it relies on
     the layout of interface and func values and on `*T` and `unsafe.Pointer` being passed alike; it
     only works for a fixed list of signatures made of framework types (a DTO parameter or a
     user-struct result has a layout the framework can't name), so it would speed up `func() string`
     or `func(ctx.Param) ctx.Map`, not the typical handler with a DTO; each listed signature needs
     its own equality test.
  2. An opt-in method on the controller that returns real method values
     (`Handlers() map[string]any{"READ_static": instance.READ_static}`): no `unsafe`, same limit on
     signatures, more to write for the user.
  Recommendation: neither now. Take the 55-75 % from this design, measure in step 7, and decide in
  roadmap 0003 with the Static number in hand; if a fast path is still wanted, option 2 first.
- **Q2 Interface results by dynamic type (B18).** It fixes the 500 of bug 3 and makes the plain path
  agree with the interceptor path, at the price of the content-type change listed under Public API.
  Alternative: only strings change, numbers and bools in an `any` stay JSON (two rules instead of
  one, and the interceptor path keeps disagreeing). Recommendation: B18 as written.
- **Q3 More than two results.** `func() (int, string, string)` writes the second and ignores the
  third (`core/http.go:194-201`). Recommendation: leave it; refusing it at `Create()` is a breaking
  change that belongs to roadmap 0003 with the rest of `handleRequest`.

## Decisions (2026-11-16)

Approved by Claude under the user's one-time delegation for roadmap 0002 (see its Log). Heavy
techniques are **not** approved: they are built without and listed as proposals for the user.
The open questions above are answered as follows.

- Q1 the reflection floor is accepted for now; no `unsafe` and no opt-in registration API. Revisit
  after step 7's measurement.
- Q2 interface-typed results: change **strings only** (an `any` holding a string is written as text,
  fixing the 500); numbers and bools in an `any` keep today's content type. Fewer visible changes.
- Q3 a third return value: left for roadmap 0003.
- The lookalike-type fixture (`internal/test/lookalike/ctx/ctx.go`) is added by the orchestrator
  before the tester runs.
