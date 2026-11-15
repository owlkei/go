# 0005: common: pipeline layers and the route DSL

Status: Approved
Packages: `common`; call-site edits in `core/app.go`, `core/http.go`, `core/read_loop.go`; one
addition expected from `ctx` (design 0003, see "Expected from ctx and exception")

Roadmap: `docs/roadmap/0002-request-hot-path.md`, step 5. Depends on designs 0002, 0003, 0004.

## Goal
Guards, interceptors and exception filters stop going through reflection on every request, an
interceptor costs one allocation instead of six, and five bugs are fixed: two layers of the same type
no longer share one instance, a layer bound to a non-route no longer lands on every route, an
exception re-thrown from a filter keeps its status code, routes with three or more parameters keep
their order. The route DSL, the layer contracts and the binding API stay as they are.

## Bugs found
Each was reproduced with a throwaway probe against `master` (b2d1070); nothing was left in the tree.

| # | Where | Input | Today | Rule |
|---|---|---|---|---|
| 1 | `common/fn.go:189-210` | `BindGuard(RoleGuard{Role: "admin"})` on controller A, `BindGuard(RoleGuard{Role: "user"})` on controller B | `Construct` caches by `Type().String()`: B's routes are guarded by A's instance (`X-Role: user` → 403, `X-Role: admin` → 200) | B1 |
| 2 | `common/fn.go:191` | Two layer types with the same package name and type name (`a/auth.Guard`, `b/auth.Guard`) | Same cache key `*auth.Guard`: the second `Construct` returns the first type's instance | B3 |
| 3 | `common/fn.go:191` | One type built with two constructor names (`NewMiddleware`, then `NewGuard`) | The key has no constructor: the second call returns the first constructor's result | B4 |
| 4 | `common/http_guard.go:62-69` and the same block in the seven other `InjectProvidersInto…` functions | `BindGuard(g, instance.Helper)` where `Helper` is not a route (or an HTTP layer bound to a `SUBSCRIBE_…` method) | No handler resolves, so `applyAll` becomes true: the layer is bound to every route of the controller | B8, B9 |
| 5 | `common/exception_filter.go:49` | A filter delegates with `panic(ex)` (`ex` is the `*exception.Exception` it received), or user code panics a pointer | Only the value type is recognized: the next filter gets a 500 whose message is the old status text (`404 "nope"` → `500 "Not Found"`) | B21 |
| 6 | `common/exception_filter.go:61-63` | Any panic that is not an exception | The `map[string]any{"description": …}` option matches no case of `exception.errorBuilder`: it is allocated and ignored, and `Error()` is `""` | B23 |
| 7 | `common/fn.go:111-136` | `READ_x_BY_a_AND_b_AND_c` | Every parameter after the first is inserted right after the first: `/x/{a}/{c}/{b}/`, so `GET /x/1/2/3` binds `b=3, c=2` | B25 |
| 8 | `common/http_guard.go:25`, `ws_guard.go:20` and the four other `As…` functions | `AsHTTPGuard(nil)` | `reflect.Value.MethodByName` panics on the zero `Value` | B11 |

Observed, not changed here (owner in brackets): an interceptor that never calls `Pipe` still runs the
main handler, only its result is dropped (`core/http.go:123`, 0003); a second `App` created in the
same process answers 404 on its routes (process-global state in `core`, 0003);
`ArgumentMetadata.ContextType` is never set (Q3); `HTTP.GetConfigurations` ignores its receiver and
`InjectProvidersIntoWSMiddlewares` has no caller (WebSocket roadmap); `READ_BY` yields `/{}/`.

## Public API
Unchanged signatures, new behavior (not breaking unless noted in Behavior):

```go
func Construct(obj any, constructor string) any
func AsHTTPGuard(guarder any) (HTTPCanActivate, bool)
func AsWSGuard(guarder any) (WSCanActivate, bool)
func AsHTTPInterceptor(interceptable any) (HTTPIntercept, bool)
func AsWSInterceptor(interceptable any) (WSIntercept, bool)
func AsHTTPExceptionFilter(exceptionFilterable any) (HTTPCatch, bool)
func AsWSExceptionFilter(exceptionFilterable any) (WSCatch, bool)
func BuildHTTPGuardMiddleware(canActivateFn HTTPCanActivate) ctx.HTTPHandler
func BuildWSGuardMiddleware(canActivateFn WSCanActivate) ctx.WSHandler
func RunHTTPCatchChain(c *ctx.HTTPContext, catchFns []HTTPCatch, rec any)
func RunWSCatchChain(c *ctx.WSContext, catchFns []WSCatch, rec any)
func NormalizeRecovered(rec any) *exception.Exception
func ParseFuncNameToURL(fnName string) (string, string, string)
```

Changed (**breaking**, exported but only `core` calls them):

```go
// the first parameter was the context key (the route pattern); it is now the interceptor's name
func BuildHTTPInterceptMiddleware(name string, interceptFn HTTPIntercept) ctx.HTTPHandler
func BuildWSInterceptMiddleware(name string, interceptFn WSIntercept) ctx.WSHandler
```

Removed (**breaking**): `type WithValueKey string` (`common/interceptor.go:12`) and its alias
`core.WithValueKey` (`core/app.go:126`): the aggregations leave the request's `context.Context`.

Added, unexported (the public contract stays "`Guarder`, `Interceptable`, `ExceptionFilterable` are
`any`, matched by method name and signature"):

```go
type httpGuard interface{ CanActivate(*ctx.HTTPContext) bool }
type wsGuard interface{ CanActivate(*ctx.WSContext) bool }
type httpInterceptor interface{ Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any }
type wsInterceptor interface{ Intercept(*ctx.WSContext, *aggregation.Aggregation) any }
type httpExceptionFilter interface{ Catch(*ctx.HTTPContext, *exception.Exception) }
type wsExceptionFilter interface{ Catch(*ctx.WSContext, *exception.Exception) }
```

Everything else exported by `common` keeps its signature and behavior: the `Bind…` methods, the
`InjectProvidersInto…` functions (targets: B7-B10), `HTTP`, `WS`, `Prefix`, the layer and item types,
the `*Pipeable` interfaces and keys, `ArgumentMetadata`, `MiddlewareFn`, `Logger`, `Publisher`.

### Expected from ctx and exception
Both are in the parallel designs already; `common` uses nothing else that is new.

```go
// ctx, design 0003 (its B36): a per-request list on both contexts
func (c *HTTPContext) AddAggregation(a *aggregation.Aggregation)
func (c *HTTPContext) Aggregations() []*aggregation.Aggregation
func (c *WSContext) AddAggregation(a *aggregation.Aggregation)
func (c *WSContext) Aggregations() []*aggregation.Aggregation
```

- `Aggregations` returns them in `AddAggregation` order, and nil or empty when there are none.
- `Reset` empties the list and keeps its capacity, so a warm pooled context adds without allocating.
  The list is valid only during the request.
- **If 0003 is approved without the slot,** O2 is dropped: `Build…InterceptMiddleware` keeps
  `context.WithValue` and its signature, and B13-B16 hold except B15.
- `exception`, design 0004: `Exception` stays a value type that user code panics by value. Its E1
  (an unsupported option counts as no option) gives B23's `Error()`; its E9 (constructors don't
  allocate) makes B12 and B23 cheaper with no change here. `aggregation` (0004): `NewAggregation`
  and the fields `Name`, `InterceptorData`, `IsMainHandlerCalled` are unchanged.

## Behavior

### Construct
- **B1 One instance per configuration.** `Construct(&RoleGuard{Role: "admin"}, "NewGuard")` then
  `Construct(&RoleGuard{Role: "user"}, "NewGuard")`: the constructor (or, without one, the identity)
  runs for each; the second result has `Role == "user"`. Two values are the same configuration when
  they have the same `reflect.Type` and `reflect.DeepEqual` is true.
- **B2 Equal configurations share.** Two calls with deeply equal values of one type and the same
  constructor name run the constructor once and return the identical result (`first == second`).
  `TestConstruct_CachesSingletonPerType` keeps passing.
- **B3 Type identity.** Values of two different types whose `Type().String()` is equal never share.
- **B4 Constructor name is part of the key.** The same value with `"NewGuard"` and with
  `"NewMiddleware"` runs each constructor once and returns each one's own result.
- **B5 No constructor.** When the value has no method of that name, the result is the value given
  in the first call of its configuration (unchanged).
- **B6 Concurrent calls** with equal values run the constructor exactly once (`-race`).
- A value that holds a non-nil func field is never deeply equal to another, so it is never shared.

### Binding targets (the eight `InjectProvidersInto…` functions)
"Layer" is the bound middleware, guard, interceptor or exception filter; "handlers" are the variadic
arguments of `Bind…`.
- **B7** No handlers: one item per route (HTTP) or event (WS) of the controller. Unchanged.
- **B8** Handlers given: one item per given handler that is a route (HTTP: its name is in
  `FuncNameToPatternMap`) or an event (WS: `ParseWSFuncNameToEvent` accepts its name), and none for
  the others. Never "all" (bug 4). A handler given twice yields one item.
- **B9** Handlers given and none belongs to this transport, each being a handler name of the other
  transport (HTTP side: `ParseWSFuncNameToEvent` accepts it; WS side: `ParseFuncNameToURL` returns a
  method): zero items, no panic.
- **B10** A handler that is neither (a helper method, a non-func value, nil) panics at `Create()`:
  `"invalid guard: <LayerType> is bound to '<name>', which is not a handler of the controller"`,
  with `invalid middleware`, `invalid interceptor`, `invalid exception filter` for the other layers
  (`<name>` is `GetFuncName`'s result, or the value's kind for a non-func), in `color.FmtRed`.
- Item order, item fields, provider injection through `cb`, `Construct`, and the "wrong shape" panics
  (`http_guard.go:55-59` and siblings) are unchanged.

### Shape checks
- **B11** `AsHTTPGuard(x)` returns `(fn, true)` exactly when `x` is non-nil and its method set has an
  exported `CanActivate` of type `func(*ctx.HTTPContext) bool`; `fn(c)` calls it on `x`. The five
  other `As…` functions follow with their signatures (`common/ws_guard.go:12`,
  `http_interceptor.go:15`, `ws_interceptor.go:13`, `http_exception_filter.go:14`,
  `ws_exception_filter.go:12`). nil → `(nil, false)`, no panic. For every non-nil `x` the result
  equals the reflection lookup used today (`reflect.ValueOf(x).MethodByName(name).Interface().(T)`):
  same `ok`, same effect and return value when called.

### Guard dispatch
- **B12** `BuildHTTPGuardMiddleware(fn)(c)`: `fn(c) == true` → `c.Next()` called once, no panic.
  `false` → `c.Next` not called, panics an `exception.Exception` with code 403, message
  `"Access denied"`, `Error() == "Forbidden"`. WS the same. Unchanged; the exception is built once.

### Interceptor dispatch
- **B13** `BuildHTTPInterceptMiddleware(name, fn)(c)`, in this order: creates one new
  `*aggregation.Aggregation` with `Name == name` and `IsMainHandlerCalled == false`; adds it to the
  context (`c.AddAggregation`); calls `fn(c, a)` once and stores the result in `a.InterceptorData`;
  calls `c.Next()` once, also when `fn` did not call `Pipe` (unchanged, see "Observed").
- **B14** Two interceptor middlewares on one request: `c.Aggregations()` holds both, in execution
  order. A panic in `fn` propagates; the aggregation is already in the list.
- **B15** The middleware does not replace `c.Request` (today `WithContext` swaps it,
  `http_interceptor.go:103-105`): the pointer is the same before and after.
- **B16** A context reused after `Reset` starts with no aggregation. WS: the same four rules with
  `BuildWSInterceptMiddleware` and `*ctx.WSContext`.
- `Aggregation.Name` is now always set (today only while a trace listener is attached,
  `core/app.go:456-478`).

### Catch chain
- **B17** `RunHTTPCatchChain(c, nil, rec)` and an empty slice: returns, calls nothing.
- **B18** The first filter is called with `c` and `NormalizeRecovered(rec)`. When it returns, no other
  filter is called.
- **B19** When filter *i* panics with value `v`, filter *i+1* is called with `NormalizeRecovered(v)`.
- **B20** When every filter panics, the function returns normally: it never lets a panic out. It does
  not modify `catchFns`. `RunWSCatchChain`: the same four rules.

### NormalizeRecovered
- **B21** `*exception.Exception`, non-nil: the same pointer is returned (bug 5). A nil pointer is
  treated like B24.
- **B22** `exception.Exception` value: a pointer to a copy; code, message and `Error()` are equal.
- **B23** Any other `error`: code 500, message `err.Error()`, `Error() == "Internal Server Error"`
  (today `""`; the default filter prints `"Internal Server Error"` in both cases, so its response
  body is byte-identical). A `string`: code 500, message is the string.
- **B24** Anything else (nil, an int, a struct): code 500, message `"Internal Server Error"`.

### Route DSL
- **B25 Parameter order.** Parameters of one resource keep their declaration order for any count:
  `READ_x_BY_a_AND_b_AND_c` → `("GET", "/x/{a}/{b}/{c}/", "")`;
  `READ_BY_a_AND_b_AND_c_AND_d` → `("GET", "/{a}/{b}/{c}/{d}/", "")`;
  `READ_m_BY_a_AND_b_AND_c_OF_u_BY_d_AND_e_AND_f` → `("GET", "/u/{d}/{e}/{f}/m/{a}/{b}/{c}/", "")`.
- **B26 Everything else is frozen.** The eight cases of `TestParseFnNameToURL`
  (`common/http_test.go:153-218`) and these keep their result:

  | Name | Result |
  |---|---|
  | `READ` | `GET`, `/`, `` |
  | `READ_BY_ID_AND_NAME` | `GET`, `/{ID}/{NAME}/`, `` |
  | `READ_users_BY_id_OF_posts_BY_post_id` | `GET`, `/posts/{post_id}/users/{id}/`, `` |
  | `READ_ANY` | `GET`, `/*/`, `` |
  | `SERVE_assets_ANY` | `SERVE`, `/assets*/`, `` |
  | `READ_logo_png_FILE` | `GET`, `/logo.png/`, `` |
  | `READ_VERSION_1_2` | `GET`, `/`, `1_2` |
  | `READ_users_CREATE_x` | `GET`, `/users_CREATE_x/`, `` |
  | `read_users` | ``, ``, `` |

  `ParseWSFuncNameToEvent`, `GetFuncName`, `ToWSEventName`, `Prefix`, `GetPrefixes`,
  `AddHandlerToRouterMap` (the `"route conflict: …"` panic) and `AddHandlerToEventMap`
  (`"event conflict: …"`) are unchanged.

### Pipes
- **B27** The eight `*Pipeable` interfaces, the eight keys and `ArgumentMetadata` are unchanged.
  `ParamType` is the pipe's key (`"body"`, `"query"`, …; set by `core`, design 0006).
  `ContextType`: Q3.

## Performance

| Function | Path | Note |
|---|---|---|
| The closures returned by `BuildHTTPGuardMiddleware`, `BuildWSGuardMiddleware`, `BuildHTTPInterceptMiddleware`, `BuildWSInterceptMiddleware` | hot | every request of a guarded or intercepted route |
| The funcs returned by the six `As…` functions | hot | they *are* the guard, interceptor and filter calls |
| `RunHTTPCatchChain`, `RunWSCatchChain`, `NormalizeRecovered` | hot (warm) | a client triggers them at will |
| `Construct`, `As…` themselves, the eight `InjectProvidersInto…`, `Bind…`, `GetFuncName`, `ParseFuncNameToURL`, `ParseWSFuncNameToEvent`, `Prefix`, `GetPrefixes`, `AddHandlerTo…`, `GetConfigurations`, the `…ShapeError` functions | cold | `Create()` only |

Baseline on the roadmap's machine (i7-9750H, Go 1.27.4), existing benchmarks plus probes:

| What | Today |
|---|---|
| One guard call through the func returned by `AsHTTPGuard` | 355 ns, 1 alloc (8 B) |
| The same method through an interface method value | 3.7 ns, 0 allocs |
| `BuildHTTPInterceptMiddleware` on a fresh request, no-op `Intercept` | about 320 ns, 5 allocs (480 B) |
| `RunHTTPCatchChain`, one filter, exception value | 50 ns, 1 alloc (48 B) |
| `RunHTTPCatchChain`, one filter, non-exception (`BenchmarkRunHTTPCatchChain`) | 212 ns, 4 allocs (432 B) |
| Guard rejection: panic, recover, one filter | 560 ns, 3 allocs |
| `bench/` Chain / Exception | 5,840 ns, 40 allocs / 5,950 ns, 33 allocs |

Hot changes, none of them a heavy technique:

- **O1 Real method values instead of reflection stubs.** `reflect.Value.MethodByName(…).Interface()`
  returns a func made by reflection: each call goes through `reflect.callMethod` (frame from a pool,
  argument copy, one allocation). Asserting the layer to the unexported interface and taking the
  method value (`g.CanActivate`) gives a compiled closure. Same accepted set (B11). Expected: about
  −350 ns and −1 allocation per guard, per interceptor and per exception filter call.
- **O2 Aggregations on the context.** The interceptor middleware stops calling
  `context.WithValue` (which also boxes its string key), `Request.WithContext` (a copy of the
  `http.Request`) and a new slice per request. What remains is the `Aggregation` itself. `core`'s `tagInterceptorName`
  wrappers disappear because `name` is set here. Expected: about 320 ns → under 40 ns and 5 → 1
  allocations per interceptor, and one closure call less.
- **O3 Catch chain as a loop.** One small function with the `recover` wraps each filter call; the
  chain iterates instead of recursing. `NormalizeRecovered` returns a re-thrown pointer as is
  (0 allocations) and builds the 500 without the ignored map (4 → at most 2 allocations).
- **O4 (type A)** The 403 of a rejected guard is built once at package level and panicked by value;
  the two redundant resets of a new aggregation (`http_interceptor.go:108-109`) go.

Expected end to end (`make bench`): Chain about −1.0 µs and −7 allocations (one guard, one
interceptor); Exception about −0.4 µs and −1 allocation. The rest of an exception's cost is the
panic and recover themselves, the error body (0003, 0004) and `core`'s glue (roadmap 0003).

Cold changes: `Construct` compares with `reflect.DeepEqual` over the few entries of one type; the
target lookup of B8-B10 is one helper shared by the eight functions instead of eight copies. No
measured target.

Recorded for roadmap 0003, not done here (type B, needs `handleRequest`): rejecting a guard without
`panic` / `recover`; one lock-free `HasListeners` check per request instead of one `RWMutex` read lock
per layer (`core/fn.go:593-621`, about 22 ns each and a shared cache line under load).

## Concurrency
- Request path: no package state is written. Each built closure captures the layer's method value
  and, for interceptors, a name. The shared 403 is an immutable value copied by `panic`.
- A layer instance is shared by every request of the routes it is bound to (and, by B2, by every
  equal binding): its own fields are the user's to protect, as today.
- An `Aggregation` belongs to one request and is dropped by `Reset`; a handler must not keep it.
- `Construct`'s cache: one mutex around the lookup, one `sync.Once` per entry (B6). `InsertedRoutes`,
  `InsertedEvents`, `HTTPOperations`, `HTTPTokenMap`, `WSOperations` are written only during
  `Create()`, which `core` serializes (`moduleGlobalMu`, `core/module.go:304`).
- No goroutine is started.

## Security
- The request path of `common` panics on purpose in one place: a rejected guard, with an
  `exception.Exception` (B12). The catch chain cannot let a panic out (B20) and is bounded by the
  number of filters; it no longer recurses.
- B8-B10 turn a silent mis-binding into a start-up failure. For guards the old behavior failed
  closed; for interceptors and exception filters it changed responses of unrelated routes.
- B21 keeps the status a filter chose when it re-throws; before, a 401 or 404 became a 500.
- B23 still puts the text of an arbitrary panic (`runtime error: …`, a driver's message) in the
  exception's message, which the default filter sends to the client: Q2.
- Nothing here reads client input directly; limits (body size, path depth) belong to 0002 and 0003.

## Impact
- `core/app.go:520-560`: pass the layer name to `Build…InterceptMiddleware`; delete
  `tagInterceptorName`, `tagWSInterceptorName` (`app.go:456-478`) and the `WithValueKey` alias (`:126`).
- `core/http.go:125`, `core/read_loop.go:202`: read `c.Aggregations()` and branch on its length
  instead of `c.Context().Value(WithValueKey(…))`. No other change to `handleRequest`.
- Tests that read the aggregations from the request's `context.Context`
  (`common/http_interceptor_test.go`, `ws_interceptor_test.go`, core tests) are rewritten.
- Docs: `CLAUDE.md` › Contracts (the lookup is by method name and signature, no longer "with
  reflection"); `.ginject/request-pipeline.md` (interceptor state, catch chain, B8-B10);
  `.ginject/routing-system.md` (B25); `.ginject/anti-patterns-gotchas.md` if it mentions the shared
  instance.
- User-visible changes: bugs 1-7; a layer bound to a non-handler now stops `Create()` (B10).

### README (`common/README.md`, new, structure of `modules/config/README.md`)
1. What the package is: the contracts a controller embeds (`HTTP`, `WS`, `Middleware`, `Guard`,
   `Interceptor`, `ExceptionFilter`) and the types a handler can ask for.
2. Route DSL: the token table (operations, `BY`, `AND`, `OF`, `ANY`, `FILE`, `VERSION`), the
   examples of B25 and B26, `SUBSCRIBE_…` events, `Prefix`.
3. Layers: one section each with the method to write (HTTP and WS signatures), `Bind…(layer,
   handlers...)`, the optional constructor (`NewMiddleware`, `NewGuard`, `NewInterceptor`,
   `NewExceptionFilter`), and the instance rule (B1, B2: equal configuration → one instance).
4. Pipes: the eight interfaces, `ArgumentMetadata`, one `Transform` example.
5. Exceptions in the pipeline: a rejected guard is a 403, how filters chain (return = handled,
   `panic(ex)` = next filter).
6. Performance notes: O1-O4 in two or three sentences each, with the numbers measured by the run.
7. Benchmarks: how to run them, the table after the run.

## Test plan
Internal tests (`package common`), one function per behavior, `test.DiffMessage`.

| Rule | Test | Case |
|---|---|---|
| B1 | `TestConstruct_DifferentValuesGetOwnInstance` | `Role: "admin"` then `"user"`; a type with and one without a constructor |
| B2 | `TestConstruct_CachesSingletonPerType` (kept), `TestConstruct_EqualValuesShareInstance` | call count 1; pointers to equal structs |
| B3 | `TestConstruct_SameNameDifferentTypesDoNotShare` | two local types declared in two functions (same `String()`) |
| B4 | `TestConstruct_ConstructorNameIsPartOfKey` | a type with `NewGuard` and `NewMiddleware` |
| B5 | `TestConstruct_MissingConstructorReturnsInputUnchanged` (kept) | |
| B6 | `TestConstruct_ConcurrentEqualValuesConstructOnce` | 64 goroutines, `-race` |
| B7 | `TestInjectProvidersIntoHTTPGuards_NoHandlersBindsAllRoutes` + the seven siblings | existing tests, kept |
| B8 | `TestInjectProvidersIntoHTTPGuards_HandlersBindOnlyThoseRoutes`, `…_DuplicateHandlerBindsOnce` + siblings | 3 routes, 1 or 2 targeted |
| B9 | `TestInjectProvidersIntoHTTPGuards_OtherTransportHandlerBindsNothing`, `TestInjectProvidersIntoWSGuards_HTTPHandlerBindsNothing` + siblings | |
| B10 | `TestInjectProvidersIntoHTTPGuards_NonHandlerPanics`, `…_NilHandlerPanics`, `…_NonFuncHandlerPanics` + one per layer for the category text | message contains `invalid guard:` and the name |
| B11 | `TestAsHTTPGuard_MatchesReflectionLookup` + the five siblings; `TestAsHTTPGuard_Nil` | fixtures: value receiver, pointer receiver, promoted method from an embedded struct, wrong parameter, wrong result, no method, unexported method, typed nil pointer; compared with the reflection lookup written in the test |
| B12 | `TestBuildHTTPGuardMiddleware_AllowCallsNext`, `…_RejectPanicsForbidden`, `…_RejectDoesNotCallNext`, `…_RejectionsAreIndependentValues`; WS the same | recover and compare code, message, `Error()` |
| B13 | `TestBuildHTTPInterceptMiddleware_AddsNamedAggregation`, `…_StoresInterceptorData`, `…_CallsNextOnce`, `…_CallsNextWithoutPipe` | |
| B14 | `TestBuildHTTPInterceptMiddleware_TwoInterceptorsKeepOrder`, `…_PanicPropagates` | |
| B15 | `TestBuildHTTPInterceptMiddleware_KeepsRequestPointer` | |
| B16 | `TestBuildHTTPInterceptMiddleware_ResetStartsEmpty`; WS: the same names with `WS` | |
| B17 | `TestRunHTTPCatchChain_EmptyDoesNothing` | nil and empty |
| B18 | `TestRunHTTPCatchChain_FirstFilterHandles` | second filter not called |
| B19 | `TestRunHTTPCatchChain_PanicGoesToNextFilter` | value, pointer and `error` re-thrown |
| B20 | `TestRunHTTPCatchChain_AllPanicReturns` (3 filters, each called once, in order), `…_DoesNotModifySlice`; WS the same | |
| B21 | `TestNormalizeRecovered_PointerIsReturnedAsIs`, `…_NilPointerIsUnknown` | pointer identity |
| B22 | `TestNormalizeRecovered_ValueKeepsFields` | |
| B23 | `TestNormalizeRecovered_ErrorBecomes500`, `…_StringBecomes500` | `Error()` text |
| B24 | `TestNormalizeRecovered_NilIs500`, `…_OtherValueIs500` | |
| B25 | `TestParseFnNameToURL_ThreeParamsKeepOrder`, `…_FourParamsKeepOrder`, `…_ThreeParamsPerResource` | |
| B26 | `TestParseFnNameToURL` (kept) + `TestParseFnNameToURL_FrozenCases` | the table above |
| B27 | `TestPipeableKeys_Unchanged` | the eight constants |
| bugs 1, 4, 5 end to end | in `core`: `TestApp_SameGuardTypeDifferentConfig`, `TestApp_GuardBoundToNonRoutePanics`, `TestApp_FilterRethrowKeepsCode` | through `ServeHTTP`; each test resets the globals (`resetModuleGlobals`, `core/module_test.go:17`) |

Benchmarks, all with `for b.Loop()` and `b.ReportAllocs()`; before and after with `-count 5` and
`benchstat` in the commit:

| Benchmark | Target |
|---|---|
| `BenchmarkHTTPGuard_Call` (new: `AsHTTPGuard` result called on a context), `BenchmarkWSGuard_Call`, `BenchmarkHTTPInterceptor_Call`, `BenchmarkHTTPExceptionFilter_Call` | under 10 ns, 0 allocs (355 ns, 1 alloc today) |
| `BenchmarkBuildHTTPGuardMiddleware_Allow` / `_Reject` (new; reject recovers and runs one filter) | under 15 ns, 0 allocs / under 520 ns, at most 2 allocs |
| `BenchmarkBuildHTTPInterceptMiddleware`, `BenchmarkBuildWSInterceptMiddleware` (rewritten: the context is `Reset` every iteration; today's version grows one context forever) | under 40 ns, 1 alloc |
| `BenchmarkRunHTTPCatchChain_Exception` / `_Pointer` / `_Error`, `BenchmarkRunWSCatchChain` | 1 / 0 / at most 2 allocs |
| `BenchmarkNormalizeRecovered_Exception` / `_Pointer` / `_Error` | same allocations as above |
| Cold, converted without a target: `BenchmarkConstruct`, `BenchmarkGetFnName`, `BenchmarkParseFnNameToURL_Simple` / `_Complex`, `BenchmarkParseWSFuncNameToEvent`, the `InjectProvidersInto…_ApplyAll` set, `BenchmarkAs…`, `BenchmarkGetPrefixes_…`, `BenchmarkAddPrefixesToRoute`, `BenchmarkAddHandlerToEventMap` | |
| Deleted: `BenchmarkArgumentMetadata_Init`, `_ZeroValue`, `BenchmarkPipeableInterfaceAssertion`, `BenchmarkToWSEventName`, `BenchmarkGetConfigurations` | they measure a struct literal, a type assertion, two `Trim` calls and a dead function |

## Open questions
- **Q1 `Construct` sharing.** B1-B2 share an instance between bindings with deeply equal values
  (a rate limiter bound with the same options on two controllers stays one limiter, as today).
  The alternative is no cache: one instance per `Bind…` call, simpler, but it splits state that is
  shared today. Recommendation: B1-B2 as written.
- **Q2 Text of unknown panics.** B23 keeps sending `err.Error()` of a non-exception panic to the
  client through the default filter. Recommendation: keep it in this step and decide in roadmap
  0003, where the default filter and the logger live: answer `"Internal Server Error"` and log the
  text. Doing only the first half here would lose the text.
- **Q3 `ArgumentMetadata.ContextType`** is always `""`. Recommendation: fill it with new constants
  `common.ContextTypeHTTP = "http"` and `common.ContextTypeWS = "ws"` (set in design 0006, free at
  request time). Alternative: remove the field (breaking).
- **Q4 Exceptions wrapped in an error.** `panic(fmt.Errorf("load: %w", exception.NotFoundException("x")))`
  is a 500 today. `Exception` has `Unwrap`, so `errors.As` could recover the 404. Recommendation:
  yes, as an extra case between B22 and B23 (cost only on the non-exception path); left out of the
  rules until you agree.

## Decisions (2026-11-16)

Approved by Claude under the user's one-time delegation for roadmap 0002 (see its Log). Heavy
techniques are **not** approved: they are built without and listed as proposals for the user.
The open questions above are answered as follows.

- Q1 `Construct`: one instance per equal configuration (same type, `reflect.DeepEqual` value), the
  cache keyed by the type itself plus the constructor name; values that can't be compared (funcs,
  channels) are never shared.
- Q2 non-exception panic text reaching the client: decided in roadmap 0003 (filter and logger), noted
  there as a security item.
- Q3 `ArgumentMetadata.ContextType`: filled with new `"http"` / `"ws"` constants.
- Q4 exceptions wrapped in an error: recovered with `errors.As`; add it to the rules and tests.
- The interceptor state uses `AddAggregation` / `Aggregations` from design 0003, with the list
  cleared to nil in `Reset` (0003 H3 is not approved), so expect one allocation per interceptor
  request instead of zero.
