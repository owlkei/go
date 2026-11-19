# Common

Internal package: applications use `owlky.HTTP`, `WS`, `Middleware`, `Guard`, `Interceptor`, `ExceptionFilter`, `MiddlewareFn`, `ArgumentMetadata`, `Publisher` and `log.Logger` / `log.LogAdapter` instead.

*Common is a part of `Owlky Go` framework: it holds the contracts a controller embeds (`HTTP`, `WS`, `Middleware`, `Guard`, `Interceptor`, `ExceptionFilter`), the route and event naming DSL, the pipes and the types a handler can ask for, and the request-time glue that runs guards, interceptors and exception filters.*

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
    - [Path](#path)
    - [PathStyle](#pathstyle)
    - [AddHandlerToRouterMap](#addhandlertoroutermap)
    - [Page routes](#page-routes)
  - [`WS` Controller Embed](#ws-controller-embed)
    - [AddHandlerToEventMap](#addhandlertoeventmap)
  - [Layers](#layers)
    - [Middleware](#middleware)
    - [Guard](#guard)
    - [Interceptor](#interceptor)
    - [ExceptionFilter](#exceptionfilter)
    - [Binding targets](#binding-targets)
    - [Shape checks](#shape-checks)
    - [Item types](#item-types)
  - [Request-time functions](#request-time-functions)
    - [BuildWSGuardMiddleware](#buildwsguardmiddleware)
    - [BuildWSInterceptMiddleware](#buildwsinterceptmiddleware)
    - [RunWSCatchChain](#runwscatchchain)
    - [NormalizeRecovered](#normalizerecovered)
  - [Pipes](#pipes)
    - [`ArgumentMetadata` Fields](#argumentmetadata-fields)
  - [`LogAdapter`, `Logger` and `Publisher`](#logadapter-logger-and-publisher)
  - [Exceptions in the pipeline](#exceptions-in-the-pipeline)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

## Key Features
- Routes and events come from method names: `READ_users_BY_id` is `GET /users/{id}/`, `READ_user_profile` is `GET /user-profile/`, `SUBSCRIBE_chat_ANY` is the event `chat.*`
- A name the parser would misread (an upper-case word that is not a route word, a `BY` without a name) stops `Create()` instead of serving another URL
- Guards, interceptors and exception filters are plain structs matched by method name and signature, called through compiled method values (no reflection per request)
- One layer instance per configuration: equal bindings share it, different ones get their own
- A layer bound to a method that is not a handler stops `Create()` instead of applying to every route
- The exception filter chain never lets a panic out, and a re-thrown exception keeps its status code

## Usage

A controller embeds `common.HTTP` for its routes and one embed per layer it binds:

```go
package main

import (
	"go.owlky.com/owlky/internal/aggregation"
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/core"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/exception"
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
	if ex.Code() != 404 {
		panic(ex)
	}
	c.Status(404).JSON(map[string]any{"message": ex.Message()})
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
		core.NewModuleBuilder().
			Controllers(UserController{}).
			Build(),
	)
	app.Listen(8080, nil)
}
```

`GET /v1/users/7/` answers `{"data":"7"}`; `GET /v1/users/0/` reaches `NotFoundFilter`; `DELETE /v1/users/7/` without `X-Role: admin` is a 403.

## Route DSL

### Tokens

A route name is `<OPERATION>[_<resource>][_BY_<param>[_AND_<param>…]][_ANY][_OF_<resource>…][_VERSION_<version>]`, in upper-case tokens and free resource words. After the operation, the route words are `BY`, `AND`, `OF`, `ANY`, `FILE`, `VERSION` (HTTP) and `ANY`, `ALL` (WebSocket).

| Token | Exported as | Meaning |
|---|---|---|
| `READ`, `CREATE`, `UPDATE`, `MODIFY`, `DELETE`, `PREFLIGHT`, `RENDER`, `SUBMIT`, `SERVE` | `HTTPOperations` (`map[string]string`, token → method: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`, `GET`, `POST`, `SERVE`) | the operation, first token |
| `BY` | `TokenBy` | the first parameter of the current resource |
| `AND` | `TokenAnd` | one more parameter of the current resource |
| `OF` | `TokenOf` | a parent resource, placed before the current one |
| `ANY` | `TokenAny` | a wildcard `*` |
| `FILE` | `TokenFile` | turns the joint before the word that precedes it into a `.` and lower-cases that word (`logo_png_FILE` → `logo.png`) |
| `VERSION` | `TokenVersion` | the rest of the name is the version |
| `ALL` | `TokenAll` | WebSocket only, last word: `**` (one or more segments) |

`HTTPTokenMap` (`map[string]string`) holds the six HTTP route words. `SUBSCRIBE` (`WSOperations`, `map[string]string`) starts a WebSocket event name.

### ParseFuncNameToURL

Turns a method name into its HTTP method, route and version. A name that starts with an operation but that the parser would misread panics with `invalid route: …` (red), during `Create()`.

#### Parameters
- 1st parameter: `string`

- Description: The method name

- 2nd parameter: `PathStyle`

- Description: How the words of one resource are joined: `PathStyleKebab` with `-`, `PathStyleAsWritten` with `_`

#### Returns
- 1st value: `string`

- Description: The HTTP method, or `""` when the name is not a route

- 2nd value: `string`

- Description: The route, always enclosed in `/`, or `""` when the name is not a route

- 3rd value: `string`

- Description: The version, or `""`

#### Usage

```go
method, route, version := common.ParseFuncNameToURL("READ_users_BY_id_OF_posts_BY_post_id_VERSION_2", common.PathStyleKebab)
// "GET", "/posts/{post_id}/users/{id}/", "2"

common.ParseFuncNameToURL("READ_user_profile", common.PathStyleKebab)
// "GET", "/user-profile/", ""
```

#### Rules
- Each operation token maps to its method: `READ_health` → `GET /health/`, `CREATE_users` → `POST`, `UPDATE_users` → `PUT`, `MODIFY_users` → `PATCH`, `DELETE_users` → `DELETE`, `PREFLIGHT_health` → `OPTIONS`.
- A name that does not start with an operation (`INVALID_users`, `lowercase_users`, `read_users`, `""`) gives `""`, `""`, `""`.
- A bare operation is the root: `READ` → `/`; `READ_VERSION_v1` → `/`, version `v1`.
- `READ_BY_id` → `/{id}/`; `READ_BY_ID_AND_NAME` → `/{ID}/{NAME}/`.
- Parameters of one resource keep their declaration order for any count: `READ_x_BY_a_AND_b_AND_c` → `/x/{a}/{b}/{c}/`; `READ_BY_a_AND_b_AND_c_AND_d` → `/{a}/{b}/{c}/{d}/`; `READ_m_BY_a_AND_b_AND_c_OF_u_BY_d_AND_e_AND_f` → `/u/{d}/{e}/{f}/m/{a}/{b}/{c}/`.
- `OF` puts the parent first: `READ_users_BY_id_OF_posts_BY_post_id` → `/posts/{post_id}/users/{id}/`; `MODIFY_dm_events_OF_with_BY_participant_id_OF_dm_conversations_VERSION_V2` → `PATCH /dm-conversations/with/{participant_id}/dm-events/`, version `V2` (`/dm_conversations/with/{participant_id}/dm_events/` as written).
- The resource words of one resource are joined with `-` under `PathStyleKebab` (`READ_user_profile` → `/user-profile/`, `READ_user_profile_OF_big_orgs` → `/big-orgs/user-profile/`) and with `_` under `PathStyleAsWritten` (`/user_profile/`, `/big_orgs/user_profile/`). `OF` makes a new segment in both styles.
- A word is copied as written, never lower-cased or split at a capital: `READ_userProfile` → `/userProfile/`; `READ_orders_2024` → `/orders-2024/`; `READ_404` → `/404/`; `READ_2fa_codes` → `/2fa-codes/`; `READ_Users` → `/Users/`; `READ_é_cafés` → `/é-cafés/`.
- Empty words in a resource are dropped: `READ_user__profile` → `/user-profile/`; `READ__users` and `READ_users_` → `/users/`; `READ_a___b` → `/a-b/`.
- A parameter name is the exact text between the `_` after `BY` / `AND` and the `_` before the next route word, in both styles: `BY_user_id` → `{user_id}`, `BY_user__id` → `{user__id}`, `BY__id` → `{_id}`, `READ_users_BY_id__OF_big_orgs` → `/big-orgs/users/{id_}/`, `BY_ID` → `{ID}`. `BY` and `AND` are interchangeable: `READ_users_AND_id` → `/users/{id}/`.
- A resource without words leaves no empty segment: `READ_BY_id_OF_users` → `/users/{id}/`.
- `ANY` is a wildcard glued to its neighbours with no separator: `READ_ANY` → `/*/`; `READ_assets_ANY` → `/assets*/`; `READ_a_b_ANY` → `/a-b*/`; `READ_me_ANY_bers` → `/me*bers/`; `READ_big_me_ANY_bers_x` → `/big-me*bers-x/`.
- In a `SERVE` method, an `ANY` that ends a resource after at least one word is a segment of its own: `SERVE_static_ANY` → `SERVE /static/*/`; `SERVE_static_files_ANY` and `SERVE_ANY_OF_static_files` → `/static-files/*/` (the same route); `SERVE_assets_ANY_OF_public` → `/public/assets/*/`; `SERVE_ANY` → `/*/`.
- One `ANY` after the parameters of a resource is a wildcard segment after them, for every operation: `READ_files_BY_name_ANY` → `/files/{name}/*/` (`GET /files/a/b/c` gives `name` = `a`); `READ_files_BY_dir_AND_name_ANY` → `/files/{dir}/{name}/*/`; `READ_files_BY_name_ANY_OF_x` → `/x/files/{name}/*/`; `READ_BY_name_ANY` → `/{name}/*/`.
- `FILE` turns the joint before the preceding word into `.` and lower-cases that word: `READ_logo_png_FILE` → `/logo.png/`; `READ_site_map_xml_FILE` → `/site-map.xml/`; `READ_a_b_FILE_c` → `/a.b-c/`; `READ_img_ANY_png_FILE` → `/img*.png/`; `READ_ANY_html_FILE_OF_members_OF_ANY_jpeg_FILE_VERSION_112____3` → `/*.jpeg/members/*.html/`, version `112_3`.
- The version is the rest of the name joined with `_`, empty parts dropped, never kebab-cased or checked: `READ_users_VERSION_V_12` → `V_12`; `READ_user_profile_VERSION_v1_v1` → `/user-profile/`, `v1_v1`; `READ_users_VERSION_` and `READ_users_VERSION` → `""`.
- A resource word with an upper-case letter and no lower-case letter that is not a route word panics `invalid route: <W> in <Name> is upper case but not a route word; write it in lower case or camelCase`: `READ_orgs_FB`, `READ_orgs_V2`, `READ_plan_B`, `READ_URL_shortener`, `READ_ÉCOLE`, `READ_users_CREATE_x`, `READ_ALL`, `READ_logo_PNG_FILE`, `READ_ANY_HTML_FILE`. Allowed: `READ_orgsFB`, `READ_apiV2`, `READ_Users`, `READ_404`, `READ_BY_ID`, `READ_users_VERSION_V2`, `READ_x_VERSION_NEUTRAL`. Names that do not start with an operation (`Helper_FB`, `read_FB`) are never checked.
- Malformed names panic, all `invalid route: `:
  - `BY in READ_users_BY has no parameter name` (also `READ_users_BY_OF_x`, `READ_users_BY_id_AND`, `DELETE_BY_ID_AND_VERSION` for `AND`);
  - `x in READ_files_BY_name_ANY_x comes after a parameter; start the next resource with OF` (also `FILE` in `READ_users_BY_id_FILE`);
  - `OF in READ_OF_users needs a resource or a parameter on both sides` (also `READ_users_OF`, `READ_a_OF_OF_b`, `SERVE_ANY_OF_`);
  - `FILE in READ_txt_FILE needs a name and an extension before it` (also `READ_FILE`, `READ_FILE_ANY`).

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
- `ANY` is `*`: `SUBSCRIBE_chat_ANY` → `chat.*` (one segment after `chat`); `SUBSCRIBE_ANY` → `*` (single-segment topics). See `internal/pattern/MATCHING.md`.
- `ALL` is `**`, one or more segments: `SUBSCRIBE_chat_ALL` → `chat.**` (`chat.room1.typing` reaches it, `chat` alone does not); `SUBSCRIBE_ALL` → `**`; `SUBSCRIBE_chat_ANY_ALL` → `chat.*.**`. `ALL` anywhere but last panics `invalid route: ALL in <Name> must be the last word` (`SUBSCRIBE_chat_ALL_x`, `SUBSCRIBE_ALL_chat`).
- Segments are lower-cased: `SUBSCRIBE_Chat_Message` → `chat.message`, `SUBSCRIBE_chatRoom` → `chatroom`. A word with an upper-case letter and no lower-case letter that is not `ANY` or `ALL` panics with the upper-case message of `ParseFuncNameToURL`: `SUBSCRIBE_chat_PERSON_ANY`, `SUBSCRIBE_chat_V2`. `PathStyle` does not apply to events.
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

`HTTP` makes a controller an HTTP controller. Its methods are `Prefix` and `Path` and it has no exported field: `core` fills its unexported maps at `Create()` and reads them through package functions, which a controller can't reach through the root alias `owlky.HTTP`.

| Function | Returns |
|----------|---------|
| `HTTPPatternToFuncNameMap(h *HTTP) map[string]string` | Route pattern → method name, one entry per route of the controller |
| `HTTPFuncNameToPatternMap(h *HTTP) map[string]string` | Method name → route pattern; the binding targets of HTTP layers are looked up here |
| `HTTPRouterMap(h *HTTP) map[string]any` | Route pattern → the handler value |

Each map is `nil` until `AddHandlerToRouterMap` adds a route.

Related types:
- `Prefix` (`Value string`, `Handlers []any`): one call of `HTTP.Prefix`.
- `HTTPLayer` (`Handler any`, `ControllerPath`, `Name`, `Route`, `Version`, `Method`, `Pattern`, `MainHandlerName`, `Template`, all `string`, and `IsPage bool`): a layer bound to one route, as `core` stores it. `Template string` is the template path a page route is bound to (`bookings/{id}`); `IsPage` is set on the main handler of a page route (`RENDER_`, `SUBMIT_`).

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

### Path

Gives one or more handlers an exact path that replaces the path their names spell. The operation word still gives the HTTP method, `VERSION` still gives the version, and the controller and module prefixes still apply.

#### Parameters
- 1st parameter: `string`

- Description: The path, with or without slashes; it may hold `{name}` parameters and `*`, and is never kebab-cased

- Variadic parameter: `...any`

- Description: Handlers (method values), at least one

#### Returns
- 1st value: `*HTTP`

- Description: The receiver, for chaining

#### Usage

```go
func (instance TokenController) NewController() owlky.Controller {
	instance.Path("/oauth2/token", instance.CREATE_token)
	return instance
}
```

#### Rules
- `Path("/oauth2/token", instance.CREATE_token)` gives `POST /oauth2/token/`; with `Prefix("v1")` and a module prefix `api`, `POST /api/v1/oauth2/token/`. `Path("/people", instance.READ_users_VERSION_2)` keeps version `2`.
- `Path("/a_b/{id}", h)` stays `/a_b/{id}/` under `PathStyleKebab`; `Path("/", h)` is the controller's root.
- The handler's name is still parsed: `Path("/x", instance.READ_orgs_FB)` panics with the upper-case message.
- `Path("/token", instance.READ_token, instance.CREATE_token)` is two routes; the same operation twice on one path is a `route conflict:` in `core`, and so is an exact path equal to another handler's path.
- Guards and other layers bound to the handler, and prefixes targeted at it, work unchanged.
- Panics, all `invalid route: `: `Path("/x") needs at least one handler`; `Path("/x") is bound to 'Helper', which is not a handler of the controller` (a helper, a `SUBSCRIBE_` method or a non-function such as `'string'`; `core` checks method names after the controller's routes are registered, through `CheckExactPaths(h *HTTP)`); `READ_ok has two exact paths: "/one" and "/two"`; `Path("/x?y=1") holds '?': a path has no query or fragment` (also `#`).

### PathStyle

`type PathStyle int` decides how `ParseFuncNameToURL` joins the words of one resource. The root package re-exports it as `owlky.PathStyle`; `core`'s `ModuleBuilder.PathStyle` sets it per module.

| Constant | Value | Joint | `READ_payment_intents` |
|---|---|---|---|
| `PathStyleKebab` | `1` | `-` (the default) | `/payment-intents/` |
| `PathStyleAsWritten` | `2` | `_` | `/payment_intents/` |

The zero value means "not set". The style applies to resource words only: never to prefixes, exact paths, parameter names, versions or WebSocket events. It changes nothing at request time: `GET /user_profile` is a 404 when the route is `/user-profile/`.

### AddHandlerToRouterMap

`AddHandlerToRouterMap(h *HTTP, modulePrefixes []string, style PathStyle, fnName string, handler any)` registers a method as a route when its name parses as one (`core` calls it at `Create()` with the controller's style). An exact path set with `Path` replaces the parsed route first. The controller's prefixes apply next: the unexported `getPrefixes` lists them as `prefix → handler name` maps, `"*"` standing for every route (`Prefix("/api")` gives `[{"/api/": "*"}]`; a prefix with two handlers gives two entries; no prefix gives an empty slice).

#### Parameters
- 1st parameter: `*HTTP`

- Description: The controller's embed

- 2nd parameter: `[]string`

- Description: Module prefixes, outermost last

- 3rd parameter: `PathStyle`

- Description: The style of the controller's resource words

- 4th parameter: `string`

- Description: The method name

- 5th parameter: `any`

- Description: The handler value

#### Usage

```go
common.AddHandlerToRouterMap(&instance.HTTP, nil, common.PathStyleKebab, "READ_items", handler)
```

#### Rules
- `READ_items` adds one route: `HTTPPatternToFuncNameMap(h)["/items/||/[GET]/"]` is `READ_items`.
- `READ_payment_intents` is `/payment-intents/` under `PathStyleKebab` and `/payment_intents/` under `PathStyleAsWritten`; prefixes keep their text (`Prefix("my_api")` and the module prefix `/pay_ments/` give `/pay_ments/my_api/payment-intents/`).
- It never panics on conflicts: route conflicts are checked per app by `core` (`route conflict: <name> method is conflicted with <name> method`), so two apps may declare the same route.

`AddHandlerToRouterMapWithPageVersion(h, modulePrefixes, style, fnName, handler, pageVersion string)` is the same with the version given to page routes; `core` passes the neutral version when versioning is on, so a page route answers any version.

### Page routes

`RENDER` (`GET`) and `SUBMIT` (`POST`) are page operations. `PageOperation(fnName string) string` returns `"RENDER"`, `"SUBMIT"` or `""` from the first word; `RenderNameOf("SUBMIT_new_OF_bookings")` is `RENDER_new_OF_bookings`.

#### Rules
- `ParseFuncNameToURL("RENDER_bookings", style)` returns `GET`, `/bookings/`, `""`; `SUBMIT_bookings` returns `POST` on the same path.
- `ANY`, `VERSION` or `FILE` in a page name panics `invalid route: ANY in RENDER_files_ANY: a page route has no wildcard` (`has no version`, `has no file extension`).
- A `SUBMIT_` takes the exact path and the prefixes of its `RENDER_`: `Path("/moved", instance.RENDER_x)` and `Prefix("p", instance.RENDER_x)` move `SUBMIT_x` to `POST /p/moved/`.
- `Path` or `Prefix` bound to a `SUBMIT_` panics `invalid route: Path("/y") is bound to 'SUBMIT_x': a SUBMIT_ uses the path of its RENDER_` (`Prefix("p")` alike).

## `WS` Controller Embed

`WS` makes a controller a WebSocket controller. It has no exported method or field: `WSEventMap(ws *WS) map[string]any` returns its event name → handler map (`nil` until an event is added). `WSLayer` (`Handler any`, `EventName`, `Name` `string`) is a layer bound to one event.

### AddHandlerToEventMap

`AddHandlerToEventMap(ws *WS, fnName string, handler any)` registers a `SUBSCRIBE_…` method as an event (`core` calls it at `Create()`).

#### Parameters
- 1st parameter: `*WS`

- Description: The controller's embed

- 2nd parameter: `string`

- Description: The method name

- 3rd parameter: `any`

- Description: The handler value

#### Usage

```go
common.AddHandlerToEventMap(&instance.WS, "SUBSCRIBE_chat_ANY", handler)
```

#### Rules
- A name that is not an event (`READ_users`) is ignored.
- `SUBSCRIBE_chat_ANY` is stored under `chat.*`.
- It never panics: event conflicts are checked per app by `core` (`event conflict: <name> event is conflicted with <name> event`), so two apps may declare the same event.

## Layers

Each layer is an embed with one `Bind…` method that returns its receiver, so calls chain, and no exported field (the bound list is unexported). Guards, interceptors and exception filters are `any`, checked by method shape at `Create()`: no named interface type stands for them. Bind a struct value that has the layer's method. The optional constructor is called once per configuration and app, by `core` (see [Binding targets](#binding-targets)); its result is the instance that runs.

### Middleware

```go
type MiddlewareFn interface {
	Use(*http.Request, http.ResponseWriter, ctx.Next)
}
type Use = func(*http.Request, http.ResponseWriter, ctx.Next)
```

`BindMiddleware(middlewareFn MiddlewareFn, handlers ...any) *Middleware` on the `Middleware` embed. Optional constructor: `NewMiddleware`. `InjectProvidersIntoHTTPMiddlewares(m *Middleware, r *HTTP, cb, construct)` builds the `[]MiddlewareItem` that `core` registers. Middleware is an HTTP concept: a WebSocket connection runs only the middlewares given to `core`'s `EnableWS`, once, at the handshake, and no middleware runs per message.

### Guard

```go
type HTTPCanActivate = func(*ctx.HTTPContext) bool
type WSCanActivate = func(*ctx.WSContext) bool
const GuardMethodName = "CanActivate"
```

Write `CanActivate(*ctx.HTTPContext) bool` (HTTP) or `CanActivate(*ctx.WSContext) bool` (WS). `BindGuard(guard any, handlers ...any) *Guard` on the `Guard` embed. Optional constructor: `NewGuard`. `InjectProvidersIntoHTTPGuards(g, r, cb, construct)` / `InjectProvidersIntoWSGuards(g, ws, cb, construct)` build the `[]GuardItem`. A guard that returns `false` answers 403 (see [Exceptions in the pipeline](#exceptions-in-the-pipeline)).

### Interceptor

```go
type HTTPIntercept = func(*ctx.HTTPContext, *aggregation.Aggregation) any
type WSIntercept = func(*ctx.WSContext, *aggregation.Aggregation) any
const InterceptorMethodName = "Intercept"
```

Write `Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any` or its WS form. Return `a.Pipe(operators...)` to run the handler and transform its result; return anything else to answer with that value instead. `BindInterceptor(interceptor any, handlers ...any) *Interceptor` on the `Interceptor` embed. Optional constructor: `NewInterceptor`. `InjectProvidersIntoHTTPInterceptors(i, r, cb, construct)` / `InjectProvidersIntoWSInterceptors(i, ws, cb, construct)` build the `[]InterceptorItem`.

### ExceptionFilter

```go
type HTTPCatch = func(*ctx.HTTPContext, *exception.Exception)
type WSCatch = func(*ctx.WSContext, *exception.Exception)
const ExceptionFilterMethodName = "Catch"
```

Write `Catch(*ctx.HTTPContext, *exception.Exception)` (context first) or its WS form. `BindExceptionFilter(exceptionFilter any, handlers ...any) *ExceptionFilter` on the `ExceptionFilter` embed. Optional constructor: `NewExceptionFilter`. `InjectProvidersIntoHTTPExceptionFilters(e, r, cb, construct)` / `InjectProvidersIntoWSExceptionFilters(e, ws, cb, construct)` build the `[]ExceptionFilterItem`.

### Binding targets

The seven `InjectProvidersInto…(layer, r *HTTP | ws *WS, cb func(int, reflect.Type, reflect.Value, reflect.Value), construct func(obj any, constructor string) any)` functions run at `Create()`: they make a new instance of each bound layer, call `cb` once per field so `core` injects providers, pass it to `construct` with the constructor's name (`NewGuard`, `NewMiddleware`, `NewInterceptor`, `NewExceptionFilter`), check the shape of what it returns and return one item per bound route or event. `core` keeps the instances per app: equal configurations (same type, `reflect.DeepEqual`) share one instance, different ones get their own, and a layer without the constructor is used as given.

#### Rules
- No handlers: one item per route (HTTP) or event (WS) of the controller.
- Handlers given: one item per handler that is a route (its name is in `FuncNameToPatternMap`) or an event (`ParseWSFuncNameToEvent` accepts it). A handler given twice yields one item.
- A handler of the other transport binds nothing on this side, without a panic: a `SUBSCRIBE_…` method on the HTTP side, a `READ_…` method on the WS side.
- A handler that is neither (a helper method, `nil`, a non-func value) panics: `invalid guard: <LayerType> is bound to '<name>', which is not a handler of the controller`, with `invalid middleware`, `invalid interceptor` and `invalid exception filter` for the other layers. `<name>` is the method name, or the kind of a non-func value (`'int'`, `'string'`).
- A guard, interceptor or exception filter without its method, or with a wrong parameter or result type, panics.
- Each item's `Common.Handler` is the callable method of the instance and, on HTTP, `Common.MainHandlerName` the route's method name.
- `GuardShapeError`, `InterceptorShapeError` and `ExceptionFilterShapeError` return the error used for a global layer with no usable method; its text contains the layer's type.

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
- `true` calls `ctx.WSNext(c)()` once.
- `false` does not call it and panics an `exception.Exception` value with code 403, message `"Access denied"` and `Error() == "Forbidden"`. Every rejection is an equal value, and each recovered copy is its own.

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
- It creates one new aggregation with `aggregation.Name` set and `aggregation.IsMainHandlerCalled` `false`, adds it to the context (`ctx.WSAggregations(c)` lists it before the interceptor runs), calls the interceptor once with it, stores the result with `aggregation.SetInterceptorData`, then calls `ctx.WSNext(c)()` once, also when the interceptor never called `Pipe`.
- Two interceptors on one request leave two distinct aggregations in execution order; the same middleware run twice appends a second one.
- A panic in the interceptor propagates, `next` is not called, and the aggregation is already in the list.
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

## `LogAdapter`, `Logger` and `Publisher`

`LogAdapter` and `Logger` are defined here and reached as `log.LogAdapter` and `log.Logger` (see `log/README.md`).

`Publisher` has `Publish(topic string, payload ...any) error` and sends a message to WebSocket subscribers of a topic.

## Exceptions in the pipeline

- A guard that returns `false` is a 403 `Forbidden` with message `"Access denied"`.
- A filter that returns has handled the exception. A filter that panics passes to the next filter: `panic(ex)` with the pointer it received keeps the status code and the message.
- An error that wraps an exception keeps the exception's code.
- On HTTP, any other panic reaches the filters as a 500 with the fixed message `"Internal Server Error"`; the value is logged, never shown (see `core`'s README, "Errors in middleware" and the request lifecycle).
- On WebSocket, `NormalizeRecovered` still turns any other panic into a 500 whose message is the panic's text (an `error` or a `string`).

## Performance notes

- **Compiled method values.** The `As…` functions assert the layer to a small unexported interface and return its method value, so a guard, interceptor or filter call is a plain closure call: about 4 ns and 0 allocations, where the reflection stub it replaces took about 400 ns and 1 allocation.
- **Interceptor state on the context.** The interceptor middleware adds its aggregation to the request's context list (`ctx.WSAddAggregation`) instead of copying the `http.Request` with a new `context.WithValue`: about 95 ns and 2 allocations (the aggregation and the list, which `ctx.WSReset` drops), where the old path took about 340 to 640 ns. `core` also no longer looks the list up in the request's `context.Context`, which saved one boxed key per request on every route.
- **Catch chain as a loop.** One small function with the `recover` wraps each filter call, and the chain iterates instead of recursing. A re-thrown pointer is returned as is (0 allocations, about 11 ns for the chain), and a plain error builds its 500 with 1 allocation: an error that has no `Unwrap` or `As` method can't hold an exception, so `errors.As` is skipped for it.
- **Shared 403.** A rejected WS guard panics one package-level exception value; an allowed guard costs about 2 ns.

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
