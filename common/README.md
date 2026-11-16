# Common

*Common is a part of `Ginject` framework: it holds the contracts a controller embeds (`HTTP`, `WS`, `Middleware`, `Guard`, `Interceptor`, `ExceptionFilter`), the route and event naming DSL, the pipes and the types a handler can ask for, and the request-time glue that runs guards, interceptors and exception filters.*

- [Common](#common)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [Route DSL](#route-dsl)
    - [Tokens](#tokens)
    - [ParseFuncNameToURL](#parsefuncnametourl)
    - [ParseWSFuncNameToEvent](#parsewsfuncnametoevent)
    - [GetFuncName](#getfuncname)
    - [ToWSEventName](#towseventname)
  - [`HTTP` Controller Embed](#http-controller-embed)
    - [Prefix](#prefix)
    - [GetPrefixes](#getprefixes)
    - [AddHandlerToRouterMap](#addhandlertoroutermap)
    - [GetConfigurations](#getconfigurations)
  - [`WS` Controller Embed](#ws-controller-embed)
    - [AddHandlerToEventMap](#addhandlertoeventmap)
  - [Layers](#layers)
    - [Middleware](#middleware)
    - [Guard](#guard)
    - [Interceptor](#interceptor)
    - [ExceptionFilter](#exceptionfilter)
    - [Binding targets](#binding-targets)
    - [Construct](#construct)
    - [Shape checks](#shape-checks)
    - [Item types](#item-types)
  - [Request-time functions](#request-time-functions)
    - [BuildWSGuardMiddleware](#buildwsguardmiddleware)
    - [BuildWSInterceptMiddleware](#buildwsinterceptmiddleware)
    - [RunWSCatchChain](#runwscatchchain)
    - [NormalizeRecovered](#normalizerecovered)
  - [Pipes](#pipes)
    - [`ArgumentMetadata` Fields](#argumentmetadata-fields)
  - [`Logger` and `Publisher`](#logger-and-publisher)
  - [Exceptions in the pipeline](#exceptions-in-the-pipeline)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

## Key Features
- Routes and events come from method names: `READ_users_BY_id` is `GET /users/{id}/`, `SUBSCRIBE_chat_ANY` is the event `chat.*`
- Guards, interceptors and exception filters are plain structs matched by method name and signature, called through compiled method values (no reflection per request)
- One layer instance per configuration: equal bindings share it, different ones get their own
- A layer bound to a method that is not a handler stops `Create()` instead of applying to every route
- The exception filter chain never lets a panic out, and a re-thrown exception keeps its status code

## Usage

A controller embeds `common.HTTP` for its routes and one embed per layer it binds:

```go
package main

import (
	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
)

type RoleGuard struct {
	Role string
}

func (instance RoleGuard) CanActivate(c *ctx.HTTPContext) bool {
	return c.Header().Get("X-Role") == instance.Role
}

type EnvelopeInterceptor struct{}

func (instance EnvelopeInterceptor) Intercept(c *ctx.HTTPContext, a *aggregation.Aggregation) any {
	return a.Pipe(
		a.Transform(func(data any) any {
			return map[string]any{"data": data}
		}),
	)
}

type NotFoundFilter struct{}

func (instance NotFoundFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	if ex.GetCode() != 404 {
		panic(ex)
	}
	c.Status(404).JSON(map[string]any{"message": ex.GetMessage()})
}

type UserController struct {
	common.HTTP
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (instance UserController) NewController() core.Controller {
	instance.Prefix("v1")
	instance.BindGuard(RoleGuard{Role: "admin"}, instance.DELETE_users_BY_id)
	instance.BindInterceptor(EnvelopeInterceptor{})
	instance.BindExceptionFilter(NotFoundFilter{})
	return instance
}

func (instance UserController) READ_users_BY_id(param ctx.Param) string {
	if param.Get("id") == "0" {
		panic(exception.NotFoundException("User not found"))
	}
	return param.Get("id")
}

func (instance UserController) DELETE_users_BY_id(param ctx.Param) string {
	return param.Get("id")
}

func main() {
	app := core.New()
	app.Create(
		core.ModuleBuilder().
			Controllers(UserController{}).
			Build(),
	)
	app.Listen(8080, nil)
}
```

`GET /v1/users/7/` answers `{"data":"7"}`; `GET /v1/users/0/` reaches `NotFoundFilter`; `DELETE /v1/users/7/` without `X-Role: admin` is a 403.

## Route DSL

### Tokens

A route name is `<OPERATION>[_<resource>][_BY_<param>[_AND_<param>…]][_OF_<resource>…][_VERSION_<version>]`, in upper-case tokens and free resource words.

| Token | Exported as | Meaning |
|---|---|---|
| `READ`, `CREATE`, `UPDATE`, `MODIFY`, `DELETE`, `PREFLIGHT`, `SERVE` | `HTTPOperations` (`map[string]string`, token → method: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`, `SERVE`) | the operation, first token |
| `BY` | `TokenBy` | the first parameter of the current resource |
| `AND` | `TokenAnd` | one more parameter of the current resource |
| `OF` | `TokenOf` | a parent resource, placed before the current one |
| `ANY` | `TokenAny` | a wildcard `*` |
| `FILE` | `TokenFile` | turns the last `_` of the segment into a `.` extension (`logo_png_FILE` → `logo.png`) |
| `VERSION` | `TokenVersion` | the rest of the name is the version |

`HTTPTokenMap` (`map[string]string`) holds the six separator tokens. `SUBSCRIBE` (`WSOperations`, `map[string]string`) starts a WebSocket event name.

### ParseFuncNameToURL

Turns a method name into its HTTP method, route and version.

#### Parameters
- 1st parameter: `string`

- Description: The method name

#### Returns
- 1st value: `string`

- Description: The HTTP method, or `""` when the name is not a route

- 2nd value: `string`

- Description: The route, always enclosed in `/`, or `""` when the name is not a route

- 3rd value: `string`

- Description: The version, or `""`

#### Usage

```go
method, route, version := common.ParseFuncNameToURL("READ_users_BY_id_OF_posts_BY_post_id_VERSION_2")
// "GET", "/posts/{post_id}/users/{id}/", "2"
```

#### Rules
- Each operation token maps to its method: `READ_health` → `GET /health/`, `CREATE_users` → `POST`, `UPDATE_users` → `PUT`, `MODIFY_users` → `PATCH`, `DELETE_users` → `DELETE`, `PREFLIGHT_health` → `OPTIONS`.
- A name that does not start with an operation (`INVALID_users`, `lowercase_users`, `read_users`, `""`) gives `""`, `""`, `""`.
- A bare operation is the root: `READ` → `/`; `READ_VERSION_v1` → `/`, version `v1`.
- `READ_BY_id` → `/{id}/`; `READ_BY_ID_AND_NAME` → `/{ID}/{NAME}/`.
- Parameters of one resource keep their declaration order for any count: `READ_x_BY_a_AND_b_AND_c` → `/x/{a}/{b}/{c}/`; `READ_BY_a_AND_b_AND_c_AND_d` → `/{a}/{b}/{c}/{d}/`; `READ_m_BY_a_AND_b_AND_c_OF_u_BY_d_AND_e_AND_f` → `/u/{d}/{e}/{f}/m/{a}/{b}/{c}/`.
- `OF` puts the parent first: `READ_users_BY_id_OF_posts_BY_post_id` → `/posts/{post_id}/users/{id}/`; `MODIFY_dm_events_OF_with_BY_participant_id_OF_dm_conversations_VERSION_V2` → `PATCH /dm_conversations/with/{participant_id}/dm_events/`, version `V2`.
- `ANY` is a wildcard: `READ_ANY` → `/*/`; `SERVE_assets_ANY` → `SERVE /assets*/`; `READ_me_ANY_bers_OF_us_ANY_ers_BY_id_VERSION_v1_v1` → `/us*ers/{id}/me*bers/`, version `v1_v1`.
- `FILE` makes an extension: `READ_logo_png_FILE` → `/logo.png/`; `READ_ANY_HTML_FILE_OF_members_OF_ANY_JPEG_FILE_VERSION_112____3` → `/*.jpeg/members/*.html/`, version `112_3`.
- The version is the rest of the name joined with `_`, empty parts dropped: `READ_users_VERSION_V_12` → `V_12`; `READ_users_VERSION_` and `READ_users_VERSION` → `""`.
- An operation token inside a resource stays a word: `READ_users_CREATE_x` → `/users_CREATE_x/`.

### ParseWSFuncNameToEvent

Turns a `SUBSCRIBE_…` method name into its event name.

#### Parameters
- 1st parameter: `string`

- Description: The method name

#### Returns
- 1st value: `string`

- Description: The event name, segments joined with `.`

- 2nd value: `bool`

- Description: Whether the name is a WebSocket handler

#### Usage

```go
event, ok := common.ParseWSFuncNameToEvent("SUBSCRIBE_chat_ANY_message")
// "chat.*.message", true
```

#### Rules
- `SUBSCRIBE_message` → `message`; `SUBSCRIBE_chat_message` → `chat.message`; `SUBSCRIBE_a_b_c` → `a.b.c`.
- `ANY` is `*`: `SUBSCRIBE_chat_ANY` → `chat.*`; `SUBSCRIBE_ANY` → `*`.
- Segments are lower-cased: `SUBSCRIBE_Chat_Message` → `chat.message`.
- `READ_users`, `CREATE_orders`, `""`, `SUBSCRIBE`, `SUBSCRIBE_` and `lowercase_message` are not events (`false`).

### GetFuncName

Returns the bare name of a function or method value (`instance.READ_users` → `"READ_users"`), without package, receiver or the `-fm` suffix of a method value.

#### Parameters
- 1st parameter: `any`

- Description: A function or method value

#### Returns
- 1st value: `string`

- Description: Its name

#### Usage

```go
name := common.GetFuncName(instance.READ_users)
```

### ToWSEventName

Trims one leading and one trailing `/`: `"/room/events/"` → `"room/events"`, `"status"` → `"status"`.

#### Parameters
- 1st parameter: `string`

- Description: A route-like string

#### Returns
- 1st value: `string`

- Description: The trimmed string

#### Usage

```go
name := common.ToWSEventName("/message/")
```

## `HTTP` Controller Embed

`HTTP` makes a controller an HTTP controller. `core` fills its maps at `Create()`.

### PatternToFuncNameMap
Type: `map[string]string`

Required: `false`

Route pattern → method name, one entry per route of the controller.

### FuncNameToPatternMap
Type: `map[string]string`

Required: `false`

Method name → route pattern; the binding targets of HTTP layers are looked up here.

### RouterMap
Type: `map[string]any`

Required: `false`

Route pattern → the handler value.

Related types:
- `Prefix` (`Value string`, `Handlers []any`): one call of `HTTP.Prefix`.
- `HTTPConfig` (`Method`, `Route`, `Version`, `Func`, all `string`): one entry of `GetConfigurations`.
- `HTTPLayer` (`Handler any`, `ControllerPath`, `Name`, `Route`, `Version`, `Method`, `Pattern`, `MainHandlerName`, all `string`): a layer bound to one route, as `core` stores it.
- `InsertedRoutes` (`map[string]string`): every route pattern inserted in the process → its method name.

### Prefix

Adds a path prefix to every route of the controller, or to the given handlers only. The prefix added last is the outermost.

#### Parameters
- 1st parameter: `string`

- Description: The prefix, with or without slashes

- Variadic parameter: `...any`

- Description: Handlers (method values); none means every route

#### Returns
- 1st value: `*HTTP`

- Description: The receiver, for chaining

#### Usage

```go
instance.Prefix("v1", instance.READ_users)
```

#### Rules
- `Prefix("/api")` gives `/api/users/` for `/users/`, without a double slash.
- A prefix bound to `READ_users` changes `READ_users` only.

### GetPrefixes

Returns the prefixes as `prefix → handler name` maps; `"*"` stands for every route.

#### Returns
- 1st value: `[]map[string]string`

- Description: One map per prefix and handler, empty when there is none

#### Usage

```go
prefixes := instance.GetPrefixes()
```

#### Rules
- `Prefix("/api")` gives `[{"/api/": "*"}]`; a prefix with two handlers gives two entries with their names; no prefix gives an empty slice.

### AddHandlerToRouterMap

Registers a method as a route when its name parses as one (`core` calls it at `Create()`).

#### Parameters
- 1st parameter: `[]string`

- Description: Module prefixes, outermost last

- 2nd parameter: `string`

- Description: The method name

- 3rd parameter: `any`

- Description: The handler value

#### Usage

```go
instance.AddHandlerToRouterMap(nil, "READ_items", handler)
```

#### Rules
- `READ_items` adds one route and records `InsertedRoutes["/items/||/[GET]/"]`.
- A second method with the same pattern panics: `route conflict: <name> method is conflicted with <name> method`.

### GetConfigurations

Returns every route in `InsertedRoutes` as an `HTTPConfig`.

#### Returns
- 1st value: `[]HTTPConfig`

- Description: One entry per inserted route

#### Usage

```go
for _, route := range instance.GetConfigurations() {
	_ = route.Method
}
```

## `WS` Controller Embed

`WS` makes a controller a WebSocket controller. `EventMap` (`map[string]any`) holds event name → handler; `InsertedEvents` (`map[string]string`) every event of the process → its method name. `WSLayer` (`Handler any`, `EventName`, `Name` `string`) is a layer bound to one event.

### AddHandlerToEventMap

Registers a `SUBSCRIBE_…` method as an event (`core` calls it at `Create()`).

#### Parameters
- 1st parameter: `string`

- Description: The method name

- 2nd parameter: `any`

- Description: The handler value

#### Usage

```go
instance.AddHandlerToEventMap("SUBSCRIBE_chat_ANY", handler)
```

#### Rules
- A name that is not an event (`READ_users`) is ignored.
- `SUBSCRIBE_chat_ANY` is stored under `chat.*`.
- A second method with the same event panics: `event conflict: <name> event is conflicted with <name> event`.

## Layers

Each layer is an embed with one `Bind…` method that returns its receiver, so calls chain. Bind a struct value that has the layer's method. The optional constructor is called once per configuration (see [Construct](#construct)); its result is the instance that runs.

### Middleware

```go
type MiddlewareFn interface {
	Use(*http.Request, http.ResponseWriter, ctx.Next)
}
type Use = func(*http.Request, http.ResponseWriter, ctx.Next)
```

`BindMiddleware(middlewareFn MiddlewareFn, handlers ...any) *Middleware` on the `Middleware` embed (field `MiddlewareHandlers`). Optional constructor: `NewMiddleware`. `InjectProvidersIntoHTTPMiddlewares(r *HTTP, cb)` builds the `[]MiddlewareItem` that `core` registers. Middleware is an HTTP concept: a WebSocket connection runs only the middlewares given to `core`'s `EnableWS`, once, at the handshake, and no middleware runs per message.

### Guard

```go
type Guarder any
type HTTPCanActivate = func(*ctx.HTTPContext) bool
type WSCanActivate = func(*ctx.WSContext) bool
const GuardMethodName = "CanActivate"
```

Write `CanActivate(*ctx.HTTPContext) bool` (HTTP) or `CanActivate(*ctx.WSContext) bool` (WS). `BindGuard(guarder Guarder, handlers ...any) *Guard` on the `Guard` embed (field `GuardHandlers`). Optional constructor: `NewGuard`. `InjectProvidersIntoHTTPGuards` / `InjectProvidersIntoWSGuards` build the `[]GuardItem`. A guard that returns `false` answers 403 (see [Exceptions in the pipeline](#exceptions-in-the-pipeline)).

### Interceptor

```go
type Interceptable any
type HTTPIntercept = func(*ctx.HTTPContext, *aggregation.Aggregation) any
type WSIntercept = func(*ctx.WSContext, *aggregation.Aggregation) any
const InterceptorMethodName = "Intercept"
```

Write `Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any` or its WS form. Return `a.Pipe(operators...)` to run the handler and transform its result; return anything else to answer with that value instead. `BindInterceptor(interceptable Interceptable, handlers ...any) *Interceptor` on the `Interceptor` embed (field `InterceptorHandlers`). Optional constructor: `NewInterceptor`. `InjectProvidersIntoHTTPInterceptors` / `InjectProvidersIntoWSInterceptors` build the `[]InterceptorItem`.

### ExceptionFilter

```go
type ExceptionFilterable any
type HTTPCatch = func(*ctx.HTTPContext, *exception.Exception)
type WSCatch = func(*ctx.WSContext, *exception.Exception)
const ExceptionFilterMethodName = "Catch"
```

Write `Catch(*ctx.HTTPContext, *exception.Exception)` (context first) or its WS form. `BindExceptionFilter(exceptionFilterable ExceptionFilterable, handlers ...any) *ExceptionFilter` on the `ExceptionFilter` embed (field `ExceptionFilterHandlers`). Optional constructor: `NewExceptionFilter`. `InjectProvidersIntoHTTPExceptionFilters` / `InjectProvidersIntoWSExceptionFilters` build the `[]ExceptionFilterItem`.

### Binding targets

The eight `InjectProvidersInto…(r *HTTP | ws *WS, cb func(int, reflect.Type, reflect.Value, reflect.Value))` functions run at `Create()`: they make a new instance of each bound layer, call `cb` once per field so `core` injects providers, pass it through `Construct`, check its shape and return one item per bound route or event.

#### Rules
- No handlers: one item per route (HTTP) or event (WS) of the controller.
- Handlers given: one item per handler that is a route (its name is in `FuncNameToPatternMap`) or an event (`ParseWSFuncNameToEvent` accepts it). A handler given twice yields one item.
- A handler of the other transport binds nothing on this side, without a panic: a `SUBSCRIBE_…` method on the HTTP side, a `READ_…` method on the WS side.
- A handler that is neither (a helper method, `nil`, a non-func value) panics: `invalid guard: <LayerType> is bound to '<name>', which is not a handler of the controller`, with `invalid middleware`, `invalid interceptor` and `invalid exception filter` for the other layers. `<name>` is the method name, or the kind of a non-func value (`'int'`, `'string'`).
- A guard, interceptor or exception filter without its method, or with a wrong parameter or result type, panics.
- Each item's `Common.Handler` is the callable method of the instance and, on HTTP, `Common.MainHandlerName` the route's method name.
- `GuardShapeError`, `InterceptorShapeError` and `ExceptionFilterShapeError` return the error used for a global layer with no usable method; its text contains the layer's type.

### Construct

`Construct(obj any, constructor string) any` returns the instance of a layer configuration: the result of `obj.<constructor>()`, or `obj` itself when it has no such method.

#### Parameters
- 1st parameter: `any`

- Description: The layer value, after provider injection

- 2nd parameter: `string`

- Description: The constructor's method name (`NewGuard`, `NewMiddleware`, `NewInterceptor`, `NewExceptionFilter`)

#### Returns
- 1st value: `any`

- Description: The instance for this configuration

#### Usage

```go
guard := common.Construct(&RoleGuard{Role: "admin"}, "NewGuard")
```

#### Rules
- Two values are the same configuration when they have the same `reflect.Type` and are `reflect.DeepEqual`; the constructor name is part of the key.
- Different configurations get their own instance: `Role: "admin"` and `Role: "user"` never share, with or without a constructor.
- Equal configurations run the constructor once and return the identical result, also from 64 concurrent calls.
- Two types with the same printed name (two local `guard` types) never share.
- The same value with `NewGuard` and `NewMiddleware` runs each constructor once and returns each one's own result.
- Without the constructor method, the result is the value given in the first call of its configuration.
- A value holding a non-nil func, or a different channel, is never equal to another, so it is never shared.

### Shape checks

`AsHTTPGuard`, `AsWSGuard`, `AsHTTPInterceptor`, `AsWSInterceptor`, `AsHTTPExceptionFilter` and `AsWSExceptionFilter` take `any` and return the layer's method as its func type and `true`, or `nil, false`.

#### Usage

```go
canActivate, ok := common.AsHTTPGuard(RoleGuard{Role: "admin"})
```

#### Rules
- The result is `true` exactly when the value's method set has the exported method with that exact signature: a value receiver, a pointer receiver (on a pointer), or a method promoted from an embedded struct.
- A wrong parameter, a wrong result, no method or an unexported method gives `false`.
- `nil` gives `nil, false`, without a panic.
- The returned func calls the method on the given instance (a typed nil pointer included) and gives the same result as a reflection lookup by name.

### Item types

`GuardItem`, `InterceptorItem` and `ExceptionFilterItem` each hold an `HTTP` and a `WS` part; `MiddlewareItem` holds only the `HTTP` part. The HTTP part (`HTTPMiddlewareItem`, `HTTPGuardItem`, `HTTPInterceptorItem`, `HTTPExceptionFilterItem`) has `Method`, `Route`, `Version`, `Pattern` (`string`) and `Common`; the WS part (`WSGuardItem`, `WSInterceptorItem`, `WSExceptionFilterItem`) has `EventName` (`string`) and `Common`. `CommonItem` is `Name string` (the layer's type), `Handler any` (its callable method) and `MainHandlerName string` (the route's method name, HTTP only).

## Request-time functions

`core` calls these on every WebSocket message of an event with layers. On HTTP, `core` calls the guards, the interceptors and the exception filters itself, in the order of the request lifecycle.

### BuildWSGuardMiddleware

Wraps a guard's method into a middleware.

#### Parameters
- 1st parameter: `WSCanActivate`

- Description: The guard's method

#### Returns
- 1st value: `ctx.WSHandler`

- Description: The middleware

#### Usage

```go
mw := common.BuildWSGuardMiddleware(canActivate)
```

#### Rules
- The guard gets the request's context.
- `true` calls `c.Next()` once.
- `false` does not call `c.Next()` and panics an `exception.Exception` value with code 403, message `"Access denied"` and `Error() == "Forbidden"`. Every rejection is an equal value, and each recovered copy is its own.

### BuildWSInterceptMiddleware

Wraps an interceptor's method into a middleware.

#### Parameters
- 1st parameter: `string`

- Description: The interceptor's name, stored in `Aggregation.Name`

- 2nd parameter: `WSIntercept`

- Description: The interceptor's method

#### Returns
- 1st value: `ctx.WSHandler`

- Description: The middleware

#### Usage

```go
mw := common.BuildWSInterceptMiddleware("EnvelopeInterceptor", intercept)
```

#### Rules
- It creates one new aggregation with `Name` set and `IsMainHandlerCalled == false`, adds it to the context (`c.Aggregations()` lists it before the interceptor runs), calls the interceptor once with it, stores the result in `InterceptorData`, then calls `c.Next()` once, also when the interceptor never called `Pipe`.
- Two interceptors on one request leave two distinct aggregations in execution order; the same middleware run twice appends a second one.
- A panic in the interceptor propagates, `c.Next()` is not called, and the aggregation is already in the list.
- `c.Request` is never replaced.
- A context reused after `Reset` starts with no aggregation.

### RunWSCatchChain

Runs exception filters on a recovered panic value.

#### Parameters
- 1st parameter: `*ctx.WSContext`

- Description: The message's context

- 2nd parameter: `[]WSCatch`

- Description: The filters, first to try first

- 3rd parameter: `any`

- Description: The recovered value

#### Usage

```go
defer func() {
	if rec := recover(); rec != nil {
		common.RunWSCatchChain(c, catchFns, rec)
	}
}()
```

#### Rules
- `nil` or an empty slice calls nothing.
- The first filter gets the context and `NormalizeRecovered(rec)`; when it returns, no other filter runs.
- When a filter panics with `v`, the next one gets `NormalizeRecovered(v)`: a re-thrown value, pointer or `error` reaches it; a re-thrown pointer keeps its code and message; `panic(nil)` reaches it too.
- When every filter panics, each runs once, in order, and the function returns normally, also for a long chain. The slice is not modified.

### NormalizeRecovered

Turns any recovered value into an exception.

#### Parameters
- 1st parameter: `any`

- Description: The recovered value

#### Returns
- 1st value: `*exception.Exception`

- Description: The exception, never nil

#### Usage

```go
ex := common.NormalizeRecovered(recover())
```

#### Rules
- A non-nil `*exception.Exception` is returned as is (the same pointer, code and message).
- An `exception.Exception` value gives a pointer to a copy with the same code, message and `Error()`.
- An `error` that wraps an exception, at any depth (`fmt.Errorf("load user: %w", exception.NotFoundException("x"))`), gives that exception's code.
- Any other `error` gives code 500, message `err.Error()` and `Error() == "Internal Server Error"`; a `string` gives code 500 with the string as message.
- Anything else (`nil`, a nil `*exception.Exception`, an `int`, a float, a slice, a struct or a pointer to one) gives code 500 and message `"Internal Server Error"`.

## Pipes

A pipe is a struct with a `Transform` method; declare it as a handler parameter and `core` passes its result:

| Interface | `Transform` input | Key constant | Key |
|---|---|---|---|
| `ContextPipeable` | `*ctx.HTTPContext` | `ContextPipeableKey` | `"context"` |
| `BodyPipeable` | `ctx.Body` | `BodyPipeableKey` | `"body"` |
| `FormPipeable` | `ctx.Form` | `FormPipeableKey` | `"form"` |
| `QueryPipeable` | `ctx.Query` | `QueryPipeableKey` | `"query"` |
| `HeaderPipeable` | `ctx.Header` | `HeaderPipeableKey` | `"header"` |
| `ParamPipeable` | `ctx.Param` | `ParamPipeableKey` | `"param"` |
| `FilePipeable` | `ctx.File` | `FilePipeableKey` | `"file"` |
| `WSPayloadPipeable` | `ctx.WSPayload` | `WSPayloadPipeableKey` | `"wsPayload"` |

Every `Transform` takes a second `ArgumentMetadata` parameter and returns `any`.

```go
type PageQuery struct {
	Page int
}

func (instance PageQuery) Transform(query ctx.Query, metadata common.ArgumentMetadata) any {
	page, _ := strconv.Atoi(query.Get("page"))
	return PageQuery{Page: max(page, 1)}
}
```

### `ArgumentMetadata` Fields

#### ContextType
Type: `string`

Default: `""`

Required: `false`

The transport of the request: `ContextTypeHTTP` (`"http"`) or `ContextTypeWS` (`"ws"`).

#### ParamType
Type: `string`

Default: `""`

Required: `false`

The pipe's key, one of the key constants above (`"body"`, `"query"`, …).

## `Logger` and `Publisher`

`Logger` is the leveled logger a provider can ask for: `Debug`, `Info`, `Warn`, `Error`, `Fatal`, each `(msg string, args ...any)`. `Publisher` has `Publish(topic string, payload ...any) error` and sends a message to WebSocket subscribers of a topic.

## Exceptions in the pipeline

- A guard that returns `false` is a 403 `Forbidden` with message `"Access denied"`.
- A filter that returns has handled the exception. A filter that panics passes to the next filter: `panic(ex)` with the pointer it received keeps the status code and the message.
- An error that wraps an exception keeps the exception's code.
- On HTTP, any other panic reaches the filters as a 500 with the fixed message `"Internal Server Error"`; the value is logged, never shown (see `core`'s README, "Errors in middleware" and the request lifecycle).
- On WebSocket, `NormalizeRecovered` still turns any other panic into a 500 whose message is the panic's text (an `error` or a `string`).

## Performance notes

- **Compiled method values.** The `As…` functions assert the layer to a small unexported interface and return its method value, so a guard, interceptor or filter call is a plain closure call: about 4 ns and 0 allocations, where the reflection stub it replaces took about 400 ns and 1 allocation.
- **Interceptor state on the context.** The interceptor middleware adds its aggregation to the request's context list (`c.AddAggregation`) instead of copying the `http.Request` with a new `context.WithValue`: about 95 ns and 2 allocations (the aggregation and the list, which `Reset` drops), where the old path took about 340 to 640 ns. `core` also no longer looks the list up in the request's `context.Context`, which saved one boxed key per request on every route.
- **Catch chain as a loop.** One small function with the `recover` wraps each filter call, and the chain iterates instead of recursing. A re-thrown pointer is returned as is (0 allocations, about 11 ns for the chain), and a plain error builds its 500 with 1 allocation: an error that has no `Unwrap` or `As` method can't hold an exception, so `errors.As` is skipped for it.
- **Shared 403.** A rejected WS guard panics one package-level exception value; an allowed guard costs about 2 ns.
- **Cold path.** `Construct` compares configurations with `reflect.DeepEqual` under one mutex (about 220 ns for a cache hit), at `Create()` only.

End to end (`make bench`, 10 alternating runs), the `Chain` scenario (a middleware, a guard and an interceptor) went from about 5.3 µs and 33 allocations to about 4.0 µs and 25 allocations, and the `Exception` scenario from about 3.9 µs to 3.5 µs with one allocation less.

## Benchmarks

Benchmark results on darwin/amd64 (Intel Core i7-9750H @ 2.60GHz), Go 1.27.4, median of 5 runs:

```sh
go test -run '^$' -bench . -benchmem -count 5 ./common/
```

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| BenchmarkNormalizeRecovered_Exception | 50.92 | 128 | 2 |
| BenchmarkNormalizeRecovered_Pointer | 2.594 | 0 | 0 |
| BenchmarkNormalizeRecovered_Error | 47.01 | 64 | 1 |
| BenchmarkGetFnName | 59.72 | 0 | 0 |
| BenchmarkParseFnNameToURL_Simple | 215.7 | 48 | 3 |
| BenchmarkParseFnNameToURL_Complex | 2217 | 1056 | 12 |
| BenchmarkParseWSFuncNameToEvent | 136.1 | 16 | 1 |
| BenchmarkConstruct | 222.6 | 0 | 0 |
| BenchmarkGetPrefixes_NoHandlers | 492.4 | 720 | 7 |
| BenchmarkGetPrefixes_WithHandlers | 638.8 | 704 | 7 |
| BenchmarkAddPrefixesToRoute | 219.5 | 40 | 2 |
| BenchmarkInjectProvidersIntoHTTPExceptionFilters_ApplyAll | 14930 | 12200 | 67 |
| BenchmarkAsHTTPExceptionFilter | 2.609 | 0 | 0 |
| BenchmarkHTTPExceptionFilter_Call | 3.82 | 0 | 0 |
| BenchmarkInjectProvidersIntoHTTPGuards_ApplyAll | 14880 | 12200 | 67 |
| BenchmarkAsHTTPGuard | 2.605 | 0 | 0 |
| BenchmarkHTTPGuard_Call | 4.433 | 0 | 0 |
| BenchmarkInjectProvidersIntoHTTPInterceptors_ApplyAll | 14560 | 12200 | 67 |
| BenchmarkAsHTTPInterceptor | 2.519 | 0 | 0 |
| BenchmarkHTTPInterceptor_Call | 3.943 | 0 | 0 |
| BenchmarkInjectProvidersIntoHTTPMiddlewares_ApplyAll | 16020 | 13568 | 124 |
| BenchmarkAddHandlerToEventMap | 3905 | 3008 | 27 |
| BenchmarkWSFnToSubject | 485.7 | 56 | 6 |
| BenchmarkInjectProvidersIntoWSExceptionFilters_ApplyAll | 4971 | 11240 | 7 |
| BenchmarkAsWSExceptionFilter | 2.605 | 0 | 0 |
| BenchmarkWSExceptionFilter_Call | 3.822 | 0 | 0 |
| BenchmarkRunWSCatchChain | 71.1 | 128 | 2 |
| BenchmarkInjectProvidersIntoWSGuards_ApplyAll | 5141 | 11240 | 7 |
| BenchmarkAsWSGuard | 2.604 | 0 | 0 |
| BenchmarkWSGuard_Call | 3.82 | 0 | 0 |
| BenchmarkBuildWSGuardMiddleware_Allow | 1.736 | 0 | 0 |
| BenchmarkInjectProvidersIntoWSInterceptors_ApplyAll | 5027 | 11240 | 7 |
| BenchmarkAsWSInterceptor | 2.701 | 0 | 0 |
| BenchmarkWSInterceptor_Call | 3.82 | 0 | 0 |
| BenchmarkBuildWSInterceptMiddleware | 94.35 | 200 | 2 |

*Note: These benchmarks are machine-dependent and were captured at documentation generation time (November 16, 2026).*
