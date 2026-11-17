# Ginject

Ginject is a dependency injection web framework for Go, inspired by NestJS. You build HTTP and
WebSocket servers from modules, controllers and providers, and Ginject wires them together.

- **No routing code.** A method named `READ_users_BY_id` answers `GET /users/{id}`. You declare
  routes and WebSocket events by naming methods.
- **No wiring code.** Providers are injected into controllers by field type, and handler
  parameters by parameter type. There are no struct tags and no code generation.
- **Mistakes stop the boot.** A missing provider, a parameter that can't be injected or two
  handlers for one route make `Create()` panic, before the server takes a request.
- **Fast at run time.** Ginject does its reflection once, at `Create()`. A request runs a plan
  that was built at startup.
- **Safe defaults.** The server has timeouts, a request body limit and graceful shutdown. Only
  exceptions reach the client: any other panic answers a fixed `500` and is logged.

- [Ginject](#ginject)
  - [Features](#features)
  - [Installation](#installation)
  - [Quick start](#quick-start)
  - [Core concepts](#core-concepts)
    - [Application](#application)
    - [Module](#module)
    - [Provider](#provider)
    - [Controller](#controller)
    - [Routes from method names](#routes-from-method-names)
    - [Handler parameters](#handler-parameters)
    - [Handler results](#handler-results)
    - [Request lifecycle](#request-lifecycle)
    - [Middleware](#middleware)
    - [Guard](#guard)
    - [Interceptor](#interceptor)
    - [Exception filter](#exception-filter)
    - [Pipes](#pipes)
  - [WebSocket](#websocket)
  - [Versioning](#versioning)
  - [Static files](#static-files)
  - [Built-in modules](#built-in-modules)
    - [Config](#config)
    - [Cache](#cache)
    - [Outbound HTTP calls](#outbound-http-calls)
  - [Built-in middlewares](#built-in-middlewares)
  - [Production checklist](#production-checklist)
  - [Exceptions](#exceptions)
  - [License](#license)

---

## Features

- Reflection-based dependency injection, done once at startup: no tags, no code generation
- Routes and WebSocket events from method names
- A fixed request pipeline: middleware, guard, interceptor, handler, exception filter
- WebSocket with topics, publish and subscribe, and the same guards, interceptors and filters as HTTP
- API versioning from the query, a header, the `Accept` media type or your own function
- Typed handler parameters and pipes that bind and validate input
- Static file serving that never lists a directory or serves a dotfile
- Config and Cache modules
- Opt-in production middlewares: load shedding, trusted proxies, host check, HTTPS redirect,
  security headers, CORS, CSRF and compression, plus a rate limit guard
- JSON logs with request and trace IDs, and an access log

---

## Installation

Ginject needs Go 1.27.4 or later.

```bash
go get github.com/dangduoc08/ginject
```

---

## Quick start

Create `main.go`:

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type GreetingService struct{}

func (instance GreetingService) NewProvider() core.Provider {
	return instance
}

func (instance GreetingService) Greet(name string) string {
	return "hello " + name
}

type HelloController struct {
	common.HTTP
	GreetingService GreetingService
}

func (instance HelloController) NewController() core.Controller {
	return instance
}

func (instance HelloController) READ_hello() string {
	return "hello"
}

func (instance HelloController) READ_hello_BY_name(param ctx.Param) ctx.Map {
	return ctx.Map{"greeting": instance.GreetingService.Greet(param.Get("name"))}
}

func main() {
	app := core.New()
	app.Create(
		core.ModuleBuilder().
			Controllers(HelloController{}).
			Providers(GreetingService{}).
			Build(),
	)
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

Run it with `go run .`, then call it:

- `GET http://localhost:3000/hello` answers `hello` as `text/plain`.
- `GET http://localhost:3000/hello/joe` answers `{"greeting":"hello joe"}` as `application/json`.
- `GET http://localhost:3000/missing` answers `404` with `{"code":404,"error":"Not Found","message":"Not Found"}`.

---

## Core concepts

### Application

`core.New()` creates an application. Configure it, wire it once with `Create`, then serve it:

1. **Configure.** Call the configuration methods below. Each one returns the app, so calls chain.
   They must run before `Create`: after it they panic with
   `invalid app: <Method> must be called before Create`.
2. **Create.** `app.Create(rootModule)` builds the module tree, constructs the providers and
   controllers, and plans every handler. Call it once per app.
3. **Serve.** `app.Listen(port, nil)` blocks until the server stops. `ListenTLS` serves HTTPS and
   HTTP/2, and `Serve` serves on a `net.Listener` you made. Pass `nil` options for the safe
   defaults, or a `*core.ServerOptions` (`ShutdownTimeout`, `MaxRequestBodyBytes`, `TLSConfig`,
   `H2C`).

| Method | Effect |
|---|---|
| `SetLogger(common.LogAdapter)` | Sets the app's logger. Read it back with `app.Logger()`. |
| `BindGlobalMiddlewares(...)` | Middlewares for every HTTP request. |
| `BindGlobalGuards(...)` | Guards for every HTTP route and WebSocket event. |
| `BindGlobalInterceptors(...)` | Interceptors for every HTTP route and WebSocket event. |
| `BindGlobalExceptionFilters(...)` | Exception filters for every HTTP route and WebSocket event. |
| `EnableVersioning(versioning.Versioning)` | Routes per API version. See [Versioning](#versioning). |
| `EnableWS(*core.WSConfig, ...)` | The WebSocket server. See [WebSocket](#websocket). |
| `EnableAccessLog()` | One access log line per request. |
| `EnableDevtool()` | A snapshot of the routes and layers for development tools. |

The server shuts down gracefully on `SIGINT`, `SIGTERM` or `app.Stop()`: running requests get
30 seconds to finish. An `App` is also an `http.Handler`, so you can serve it with your own
`http.Server` or call it from tests with `httptest`. See [core › Server](core/README.md#server).

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
)

type HealthController struct {
	common.HTTP
}

func (instance HealthController) NewController() core.Controller {
	return instance
}

func (instance HealthController) READ_health() string {
	return "ok"
}

func TestHealth(t *testing.T) {
	app := core.New()
	app.Create(core.ModuleBuilder().Controllers(HealthController{}).Build())

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}
```

Each `core.New()` is independent: two apps in one process have their own providers, routes,
layers and logger.

### Module

A module groups controllers and providers, and imports other modules. Build one with
`core.ModuleBuilder()`:

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
)

type UserService struct{}

func (instance UserService) NewProvider() core.Provider {
	return instance
}

func (instance UserService) FindAll() []string {
	return []string{"alice", "bob"}
}

type UserController struct {
	common.HTTP
	UserService UserService
}

func (instance UserController) NewController() core.Controller {
	return instance
}

func (instance UserController) READ_users() []string {
	return instance.UserService.FindAll()
}

var UserModule = core.ModuleBuilder().
	Controllers(UserController{}).
	Providers(UserService{}).
	Build()

var AppModule = core.ModuleBuilder().
	Imports(UserModule).
	Build().
	Prefix("api")

func main() {
	app := core.New()
	app.Create(AppModule)
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /api/users` answers `["alice","bob"]`.

- **Prefix:** `Prefix("api")` adds a path prefix to every route of the module and of the modules
  it imports. A prefix on an importing module goes in front: a child with `Prefix("v1")` imported
  by a root with `Prefix("api")` serves `/api/v1/…`.
- **Global modules:** set `IsGlobal = true` on a module to make its providers visible to every
  module, without an import. The root module's providers are always global.
- **Module functions:** `Imports` also takes a function that returns a `*core.Module`. Ginject
  calls it once per app and injects its arguments from the global providers, so a module can
  depend on configuration. This is how `config.Register(…)` and `cache.Register(…)` work.
- **Hooks:** a module's `OnInit` runs before its providers are constructed, `OnReady` at the end
  of `Create`, and `OnShutdown` when the server shuts down, in reverse order.

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/modules/config"
)

type Database struct {
	URL string
}

func (instance Database) NewProvider() core.Provider {
	return instance
}

func NewDatabaseModule(configService config.ConfigService) *core.Module {
	url, _ := configService.Get("DATABASE_URL").(string)
	module := core.ModuleBuilder().
		Providers(Database{URL: url}).
		Build()
	module.IsGlobal = true
	module.OnShutdown = func() {
		// Close the connection pool here.
	}
	return module
}

type StatusController struct {
	common.HTTP
	Database Database
}

func (instance StatusController) NewController() core.Controller {
	return instance
}

func (instance StatusController) READ_status() bool {
	return instance.Database.URL != ""
}

func main() {
	app := core.New()
	app.Create(
		core.ModuleBuilder().
			Imports(
				config.Register(&config.ConfigModuleOptions{IsGlobal: true, IsIgnoreEnvFile: true}),
				NewDatabaseModule,
			).
			Controllers(StatusController{}).
			Build(),
	)
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`NewDatabaseModule` receives the `ConfigService` of the global config module. An argument that no
global provider supplies stops `Create()` with
`dependency injection: can't resolve argument of '<func>' …`.

### Provider

A provider is a struct with a `NewProvider() core.Provider` method. It holds your business logic:
services, repositories, clients.

- Ginject constructs each provider type once per app. It first injects the provider's exported
  fields, then calls `NewProvider`, and keeps what `NewProvider` returns.
- Fields are matched by type: declare a field of a provider's type and Ginject fills it. A field
  whose type no visible provider has stops `Create()` with
  `dependency injection: can't resolve dependency '<type>' of the provider…`.
- A provider is visible to its own module, to the modules that import it, and to every module
  when its module is global.
- Providers are injected by value. Put state that must be shared, such as a connection pool,
  behind a pointer.
- A field of type `common.Logger` gets the app's logger, and a field of type `common.Publisher`
  the app's WebSocket publisher.

Never call `NewProvider` yourself: the instance it returns has no injected fields. To read a
provider from outside the module tree, use `app.Get(UserService{}).(UserService)` after `Create`.

### Controller

A controller is a struct with a `NewController() core.Controller` method. Embed `common.HTTP` for
HTTP routes or `common.WS` for WebSocket events, plus one embed per kind of layer you bind:
`common.Middleware`, `common.Guard`, `common.Interceptor` and `common.ExceptionFilter`.

Bind layers in `NewController`. Pass method values to bind a layer to some handlers only, or none
to bind it to every handler of the controller:

```go
package main

import (
	"net/http"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type AdminGuard struct{}

func (instance AdminGuard) CanActivate(c *ctx.HTTPContext) bool {
	return c.Header().Get("X-Role") == "admin"
}

type NoStoreMiddleware struct{}

func (instance NoStoreMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set("Cache-Control", "no-store")
	next()
}

type ProductController struct {
	common.HTTP
	common.Guard
	common.Middleware
}

func (instance ProductController) NewController() core.Controller {
	instance.Prefix("catalog")
	instance.BindMiddleware(NoStoreMiddleware{})
	instance.BindGuard(AdminGuard{}, instance.CREATE_products, instance.DELETE_products_BY_id)
	return instance
}

func (instance ProductController) READ_products() []string {
	return []string{"pen", "ink"}
}

func (instance ProductController) CREATE_products() string {
	return "created"
}

func (instance ProductController) DELETE_products_BY_id(param ctx.Param) string {
	return "deleted " + param.Get("id")
}

func main() {
	app := core.New()
	app.Create(core.ModuleBuilder().Controllers(ProductController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /catalog/products` is open. `POST /catalog/products` and `DELETE /catalog/products/7`
answer `403` without `X-Role: admin`. Every response carries `Cache-Control: no-store`.

`instance.Prefix("catalog")` adds a prefix to every route of the controller; pass method values
after the prefix to add it to some routes only. A layer bound to a method that is not a handler
stops `Create()`: `invalid guard: main.AdminGuard is bound to 'Helper', which is not a handler of
the controller`.

### Routes from method names

An exported controller method is an HTTP handler when its name starts with an operation. The rest
of the name builds the path:

```
<OPERATION>[_<resource>][_BY_<param>[_AND_<param>…]][_OF_<parent>[_BY_<param>…]…][_VERSION_<version>]
```

Upper-case tokens have a meaning. Every other word is path text, written as is.

| Token | Meaning | Example |
|---|---|---|
| `READ` | `GET` (and `HEAD`) | `READ_users` → `GET /users` |
| `CREATE` | `POST` | `CREATE_users` → `POST /users` |
| `UPDATE` | `PUT` | `UPDATE_users_BY_id` → `PUT /users/{id}` |
| `MODIFY` | `PATCH` | `MODIFY_users_BY_id` → `PATCH /users/{id}` |
| `DELETE` | `DELETE` | `DELETE_users_BY_id` → `DELETE /users/{id}` |
| `PREFLIGHT` | `OPTIONS` | `PREFLIGHT_users` → `OPTIONS /users` |
| `SERVE` | Static files (`GET`) | `SERVE_ANY_OF_assets` → `GET /assets/*`. See [Static files](#static-files). |
| `BY` | The first parameter of the current resource | `READ_users_BY_id` → `GET /users/{id}` |
| `AND` | One more parameter of the current resource | `READ_files_BY_dir_AND_name` → `GET /files/{dir}/{name}` |
| `OF` | A parent resource, placed before the current one | `READ_posts_BY_post_id_OF_users_BY_user_id` → `GET /users/{user_id}/posts/{post_id}` |
| `ANY` | A wildcard `*` | `READ_ANY` → `GET /*` |
| `FILE` | Turns the last `_` of a resource into a `.` | `READ_robots_txt_FILE` → `GET /robots.txt` |
| `VERSION` | The rest of the name is the API version | `READ_users_VERSION_2` → `GET /users`, version `2` |

The rules that follow from the name:

- A bare operation is the root: `READ` answers `GET /`.
- Words joined with `_` stay one segment: `READ_user_profile` is `GET /user_profile`. For two
  segments, use `OF`: `READ_profile_OF_users` is `GET /users/profile`.
- A parameter keeps its name as written: `BY_id` is read with `param.Get("id")`, and `BY_ID` with
  `param.Get("ID")`.
- A trailing slash in the request doesn't matter: `GET /users/7` and `GET /users/7/` both reach
  `READ_users_BY_id`.
- A static segment wins over a parameter: with `READ_me_OF_users` (`GET /users/me`) and
  `READ_users_BY_id`, `GET /users/me` reaches `READ_me_OF_users`.
- A method whose name doesn't start with an operation (`findUser`, `Helper`) is not a route.
- Two handlers with the same method, path shape and version stop `Create()` with
  `route conflict: <name> method is conflicted with <name> method`.

A request for a path with no route answers `404`. A request for a path that has a route under
another method answers `405` with an `Allow` header. `HEAD` is served by the `GET` route.
The complete rules are in [common › Route DSL](common/README.md#route-dsl).

### Handler parameters

Declare the parameters a handler needs. Ginject passes them by type, in any order:

```go
package main

import (
	"net/http"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type OrderController struct {
	common.HTTP
}

func (instance OrderController) NewController() core.Controller {
	return instance
}

func (instance OrderController) READ_orders_BY_id(
	c *ctx.HTTPContext,
	param ctx.Param,
	query ctx.Query,
	header ctx.Header,
) ctx.Map {
	return ctx.Map{
		"id":       param.Get("id"),
		"expand":   query.Get("expand"),
		"language": header.Get("Accept-Language"),
		"request":  c.GetID(),
	}
}

func (instance OrderController) CREATE_orders(body ctx.Body, w http.ResponseWriter) ctx.Map {
	w.Header().Set("Location", "/orders/1")
	return ctx.Map{"item": body.Get("item")}
}

func main() {
	app := core.New()
	app.Create(core.ModuleBuilder().Controllers(OrderController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

| Parameter type | HTTP | WebSocket | Value |
|---|---|---|---|
| `*ctx.HTTPContext` | yes | no | The request's context: request data, status and response helpers. |
| `*http.Request`, `http.ResponseWriter` | yes | no | The request and the response writer. |
| `ctx.Param` | yes | no | Path parameters. |
| `ctx.Query` | yes | no | Query string values. |
| `ctx.Header` | yes | no | Request headers. |
| `ctx.Body` | yes | no | The JSON body, as a `map[string]any`. Malformed JSON answers `400`. |
| `ctx.Form`, `ctx.File` | yes | no | Form values and uploaded files of a urlencoded or multipart request. |
| `ctx.Redirect` | yes | no | `c.Redirect`: sends a redirect. |
| `*ctx.WSContext` | no | yes | The message's context. |
| `ctx.WSPayload` | no | yes | The message's `message` object. |
| `ctx.Next` | yes | yes | Continues the pipeline. |
| `common.Publisher` | yes | yes | Publishes to WebSocket subscribers. |
| A pipe | yes | yes | What its `Transform` returns. See [Pipes](#pipes). |

The root `ginject` package re-exports these types as aliases (`ginject.Param`, `ginject.Body`,
`ginject.HTTPContext`, `ginject.Map` …), so you can import one package instead of several.

Any other parameter type stops `Create()`:
`dependency injection: can't resolve dependencies of the 'func(*ctx.HTTPContext, int) string'…`.

The context belongs to one request: never keep `c` or a value read from it after the handler
returns. Pass `c.Context()` to services and goroutines instead. It carries the request's trace and
request IDs. See [ctx](ctx/README.md).

### Handler results

A handler returns nothing, one value, or a status code and a value:

| Result | Response |
|---|---|
| `string`, a number or a `bool` | Text, `text/plain; charset=utf-8` |
| A map, slice, struct, array or pointer | JSON, `application/json` |
| `any` | A string as text, anything else as JSON |
| `(int, value)` | The first result sets the status code |

The status is `200`, or `201` for `POST`, unless a layer or the handler sets another one. To
write the response yourself, take `*ctx.HTTPContext` and call `c.Status(code).JSON(data)`,
`c.Text(…)` or `c.Redirect(url)`, and return nothing.

```go
package main

import (
	"net/http"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type JobController struct {
	common.HTTP
}

func (instance JobController) NewController() core.Controller {
	return instance
}

func (instance JobController) CREATE_jobs() (int, ctx.Map) {
	return http.StatusAccepted, ctx.Map{"status": "queued"}
}

func (instance JobController) READ_jobs_BY_id(c *ctx.HTTPContext) {
	c.Status(http.StatusOK).JSON(ctx.Map{"id": c.Param().Get("id")})
}

func main() {
	app := core.New()
	app.Create(core.ModuleBuilder().Controllers(JobController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`POST /jobs` answers `202` with `{"status":"queued"}`.

### Request lifecycle

Every HTTP request runs the same steps, in this order:

```
global middlewares → module middlewares
  → global guards → module guards
    → global interceptors → module interceptors
      → pipes and handler
    ← module interceptor operators ← global interceptor operators
  → response

a panic in a middleware        → the error response (no exception filter runs)
a panic from a guard onwards   → module exception filters → global exception filters → built-in filter
```

- **Global** layers are bound on the app with `BindGlobal…`. **Module** layers are bound in a
  controller with `BindMiddleware`, `BindGuard`, `BindInterceptor` and `BindExceptionFilter`.
- Inside one step, layers run in the order you bound them. Exception filters run the other way:
  the filter bound last runs first.
- A layer stops the request by not continuing: a middleware that returns without calling
  `next()`, a guard that returns `false`, or an interceptor that returns without calling `Pipe`.
- The chain is linear. Code after `next()` runs before the next layer starts, not after the
  handler. Work that must happen after the handler belongs in an interceptor.
- A request that matches no route runs the global middlewares only, then answers `404` or `405`.

The complete rules are in [core › Request lifecycle](core/README.md#request-lifecycle).

### Middleware

A middleware is a struct with a `Use(*http.Request, http.ResponseWriter, ctx.Next)` method. Call
`next()` to continue, or write a response and return to stop the request:

```go
package main

import (
	"net/http"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type RequestLogMiddleware struct {
	Logger common.Logger
}

func (instance RequestLogMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.Logger.From(r.Context()).Info("Request", "method", r.Method, "path", r.URL.Path)
	next()
}

type APIKeyMiddleware struct{}

func (instance APIKeyMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	if r.Header.Get("X-Api-Key") == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"missing API key"}`))
		return
	}
	next()
}

type ReportController struct {
	common.HTTP
	common.Middleware
}

func (instance ReportController) NewController() core.Controller {
	instance.BindMiddleware(APIKeyMiddleware{}, instance.READ_reports)
	return instance
}

func (instance ReportController) READ_reports() []string {
	return []string{"2026-q3"}
}

func main() {
	app := core.New()
	app.BindGlobalMiddlewares(RequestLogMiddleware{})
	app.Create(core.ModuleBuilder().Controllers(ReportController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

- Exported fields of a middleware get providers injected, like a controller's. A
  `common.Logger` field gets the app's logger.
- `Logger.From(r.Context())` adds the request's `trace_id` and `request` ID to the log line.
- A middleware's panic never reaches your exception filters. An exception keeps its code and
  message, and any other value answers a fixed `500`. See
  [core › Errors in middleware](core/README.md#errors-in-middleware).
- Middlewares don't run for WebSocket messages. See [WebSocket](#websocket).

### Guard

A guard decides whether a request may reach its handler. Write
`CanActivate(*ctx.HTTPContext) bool` for HTTP routes, or `CanActivate(*ctx.WSContext) bool` for
WebSocket events. A guard that returns `false` answers `403` with
`{"code":403,"error":"Forbidden","message":"Access denied"}`.

Ginject reads the signature of `CanActivate` to know the guard's transport: a global HTTP guard
runs only for HTTP routes, and a WebSocket guard only for WebSocket events.

A guard can have an optional `NewGuard` constructor. Ginject injects providers into the guard's
exported fields, calls `NewGuard` once at `Create()`, and runs the instance it returns. A guard
can't have unexported fields: they stop `Create()`.

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/modules/config"
)

type TokenGuard struct {
	ConfigService config.ConfigService
	Token         string
}

func (instance TokenGuard) NewGuard() TokenGuard {
	instance.Token, _ = instance.ConfigService.Get("API_TOKEN").(string)
	return instance
}

func (instance TokenGuard) CanActivate(c *ctx.HTTPContext) bool {
	return instance.Token != "" && c.Header().Get("Authorization") == "Bearer "+instance.Token
}

type AccountController struct {
	common.HTTP
	common.Guard
}

func (instance AccountController) NewController() core.Controller {
	instance.BindGuard(TokenGuard{})
	return instance
}

func (instance AccountController) READ_account() string {
	return "account"
}

func main() {
	app := core.New()
	app.Create(
		core.ModuleBuilder().
			Imports(config.Register(&config.ConfigModuleOptions{IsGlobal: true})).
			Controllers(AccountController{}).
			Build(),
	)
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

Bind a guard to every route with `app.BindGlobalGuards(TokenGuard{})`. A guard that needs to
answer with another status, such as `401`, panics with an exception:
`panic(exception.UnauthorizedException("Missing token"))`. Guards decide access. To check the
shape of the input, use a [pipe](#pipes).

### Interceptor

An interceptor runs code before and after the handler. Write
`Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any`, or the same method with
`*ctx.WSContext` for WebSocket events.

- Return `a.Pipe(operators…)` to run the handler and pass its result through the operators.
- Return any other value to skip the handler: that value is the response.

| Operator | Effect |
|---|---|
| `a.Transform(func(data any) any)` | Replaces the data with the function's result. |
| `a.Tap(func(data any) any)` | Calls the function and keeps the data, for side effects such as logging. |
| `a.Filter(func(data any) bool)` | Stops the remaining operators when the function returns `false`. |

```go
package main

import (
	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type EnvelopeInterceptor struct{}

func (instance EnvelopeInterceptor) Intercept(c *ctx.HTTPContext, a *aggregation.Aggregation) any {
	return a.Pipe(
		a.Transform(func(data any) any {
			return ctx.Map{"data": data, "request": c.GetID()}
		}),
	)
}

type TagController struct {
	common.HTTP
}

func (instance TagController) NewController() core.Controller {
	return instance
}

func (instance TagController) READ_tags() []string {
	return []string{"go", "web"}
}

func main() {
	app := core.New()
	app.BindGlobalInterceptors(EnvelopeInterceptor{})
	app.Create(core.ModuleBuilder().Controllers(TagController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /tags` answers `{"data":["go","web"],"request":"<request ID>"}`. Bind an interceptor to one
controller with `instance.BindInterceptor(EnvelopeInterceptor{})` (embed `common.Interceptor`).
The optional constructor is `NewInterceptor`.

### Exception filter

Panic with an exception anywhere from a guard onwards, and an exception filter turns it into the
response. Without a filter of your own, the built-in filter answers with the exception's code
and `{"code":<int>,"error":<string>,"message":<string>}`.

To change the response, write `Catch(*ctx.HTTPContext, *exception.Exception)`, or the same method
with `*ctx.WSContext` for WebSocket events:

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
)

type ProblemFilter struct{}

func (instance ProblemFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	c.Status(ex.GetCode()).JSON(ctx.Map{
		"status": ex.GetCode(),
		"title":  ex.GetStatusText(),
		"detail": ex.GetMessage(),
	})
}

type BookController struct {
	common.HTTP
	common.ExceptionFilter
}

func (instance BookController) NewController() core.Controller {
	instance.BindExceptionFilter(ProblemFilter{})
	return instance
}

func (instance BookController) READ_books_BY_id(param ctx.Param) string {
	if param.Get("id") != "1" {
		panic(exception.NotFoundException("Book not found"))
	}
	return "Go in Action"
}

func main() {
	app := core.New()
	app.Create(core.ModuleBuilder().Controllers(BookController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /books/2` answers `404` with `{"detail":"Book not found","status":404,"title":"Not Found"}`.

- A filter that returns has handled the exception. A filter that panics passes what it panicked
  with to the next filter: `panic(ex)` passes the exception on unchanged.
- Only exceptions reach the client. Any other panic (a string, an `error`, a runtime error)
  reaches the filters as a `500` with the message `Internal Server Error`, and the value is
  logged with its stack. Your internal errors never leak into a response.
- An `error` that wraps an exception (`fmt.Errorf("load book: %w", exception.NotFoundException("Book not found"))`)
  keeps the exception's code.

Bind a filter to every route with `app.BindGlobalExceptionFilters(ProblemFilter{})`. See
[Exceptions](#exceptions) for the list of exceptions.

### Pipes

A pipe binds and validates one handler parameter. It is a value type with a `Transform` method
that takes a source and a `common.ArgumentMetadata`, and returns a value of the pipe's own type.
Declare the pipe as a handler parameter: Ginject calls `Transform` before the handler and passes
its result.

| Source parameter of `Transform` | Interface |
|---|---|
| `ctx.Body` | `common.BodyPipeable` |
| `ctx.Query` | `common.QueryPipeable` |
| `ctx.Param` | `common.ParamPipeable` |
| `ctx.Header` | `common.HeaderPipeable` |
| `ctx.Form` | `common.FormPipeable` |
| `ctx.File` | `common.FilePipeable` |
| `*ctx.HTTPContext` | `common.ContextPipeable` |
| `ctx.WSPayload` | `common.WSPayloadPipeable` |

`Bind` and `BindValue` copy the source into a struct, field by field, by the `bind` tag:

```go
package main

import (
	"strings"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
)

type CreateUserBody struct {
	Name  string `bind:"name"`
	Email string `bind:"email"`
}

func (instance CreateUserBody) Transform(body ctx.Body, _ common.ArgumentMetadata) any {
	user := body.BindValue(instance).(CreateUserBody)
	if user.Name == "" || !strings.Contains(user.Email, "@") {
		panic(exception.BadRequestException("A name and a valid email are required"))
	}
	return user
}

type SignupController struct {
	common.HTTP
}

func (instance SignupController) NewController() core.Controller {
	return instance
}

func (instance SignupController) CREATE_users(user CreateUserBody) ctx.Map {
	return ctx.Map{"name": user.Name, "email": user.Email}
}

func main() {
	app := core.New()
	app.Create(core.ModuleBuilder().Controllers(SignupController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`POST /users` with `{"name":"ann","email":"ann@example.com"}` answers `201` with the user.
With `{}` it answers `400`.

- A pipe's exported fields get providers injected at `Create()`, like a controller's.
- `Transform` runs on a fresh copy of the pipe for every request: changing its fields affects no
  other request.
- A pointer parameter (`*CreateUserBody`) stops `Create()`. A `Transform` that returns another
  type answers `500`.

See [core › Pipes](core/README.md#pipes) and [ctx › Binding](ctx/README.md#binding).

---

## WebSocket

Turn on the WebSocket server with `app.EnableWS(&core.WSConfig{Path: "ws"})` before `Create`. An
empty `Path` serves `/ws`. Then write a controller that embeds `common.WS`, with one
`SUBSCRIBE_…` method per event:

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type ChatMessage struct {
	Text string `bind:"text"`
}

type ChatController struct {
	common.WS
}

func (instance ChatController) NewController() core.Controller {
	return instance
}

func (instance ChatController) SUBSCRIBE_chat_ANY(payload ctx.WSPayload) ctx.Map {
	message := payload.BindValue(ChatMessage{}).(ChatMessage)
	return ctx.Map{"received": message.Text}
}

func main() {
	app := core.New()
	app.EnableWS(&core.WSConfig{Path: "ws"})
	app.Create(core.ModuleBuilder().Controllers(ChatController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

Event names come from the method name: the words after `SUBSCRIBE`, lower-cased and joined with
`.`. `ANY` is a wildcard `*`. `SUBSCRIBE_chat_message` handles the topic `chat.message`, and
`SUBSCRIBE_chat_ANY` handles `chat.room1`, `chat.room2` and every other topic with one segment after `chat` (not `chat.room1.typing`).

### Messages

Clients and the server exchange JSON objects with the fields `type`, `id`, `topic` (a list) and
`message`. A client connects to `ws://localhost:3000/ws`, then sends:

| Client sends | Server does |
|---|---|
| `{"type":"subscribe","id":"1","topic":["chat.room1"]}` | Runs the event's guards and interceptors (not the handler), subscribes the connection, and answers `{"type":"ack","id":"1"}`. |
| `{"type":"publish","id":"2","topic":["chat.room1"],"message":{"text":"hi"}}` | Runs the event's guards, interceptors and handler, answers the sender with `{"type":"event","id":"2","message":<handler result>}`, sends `{"type":"event","topic":["chat.room1"],"message":{"text":"hi"}}` to every subscriber of the topic (the sender included), then answers `{"type":"ack","id":"2"}`. |
| `{"type":"unsubscribe","topic":["chat.room1"]}` | Ends the subscription. No answer. |
| `{"type":"ping"}` | Answers `{"type":"pong"}`. |

- On connect, the server sends `{"type":"connected","id":"<connection ID>"}`.
- The server's messages always carry `type`, `id` and `message`. A field without a value is `""`
  or `null`.
- A client must subscribe to a topic before it publishes to it. Otherwise it gets an `error`
  message with code `4001`. A topic without a handler gets code `4004`.
- Errors are `{"type":"error","id":"<id>","message":{"code":…,"error":…,"message":…}}`. A guard
  that returns `false` gives code `403`.
- The server sends `{"type":"ping"}` every 30 seconds. A connection that sends nothing for 60
  seconds is closed, so answer pings with `{"type":"pong"}`.
- Each connection has a send buffer of 32 messages. A message for a slow client whose buffer is
  full is dropped.

### Publish from the server

Ask for a `common.Publisher` in any handler, HTTP or WebSocket, to send a message to every
subscriber of a topic:

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type NewsController struct {
	common.HTTP
}

func (instance NewsController) NewController() core.Controller {
	return instance
}

func (instance NewsController) CREATE_news(body ctx.Body, publisher common.Publisher) string {
	if err := publisher.Publish("chat.news", ctx.Map{"headline": body.Get("headline")}); err != nil {
		panic(err)
	}
	return "sent"
}

type ChatController struct {
	common.WS
}

func (instance ChatController) NewController() core.Controller {
	return instance
}

func (instance ChatController) SUBSCRIBE_chat_ANY(payload ctx.WSPayload) ctx.WSPayload {
	return payload
}

func main() {
	app := core.New()
	app.EnableWS(&core.WSConfig{Path: "ws"})
	app.Create(core.ModuleBuilder().Controllers(NewsController{}, ChatController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`POST /news` with `{"headline":"Go 1.27"}` sends
`{"type":"event","topic":["chat.news"],"message":{"headline":"Go 1.27"}}` to every connection
subscribed to `chat.news`.

### Layers on WebSocket

- Guards, interceptors and exception filters work on WebSocket events: write their methods with
  `*ctx.WSContext` and bind them in the controller or globally.
- Middlewares bound with `BindGlobalMiddlewares` don't run on WebSocket connections. Pass the
  middlewares the handshake needs to `EnableWS`, after the config:
  `app.EnableWS(&core.WSConfig{Path: "ws"}, cors.CORS{AllowOrigins: []string{"https://app.example.com"}})`.
  They run once per connection, at the handshake, and a middleware that doesn't call `next()`
  refuses the connection.

---

## Versioning

Versioning lets one path answer differently per API version. Enable it before `Create`, then add a
`VERSION` token to each handler name:

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/versioning"
)

type UserController struct {
	common.HTTP
}

func (instance UserController) NewController() core.Controller {
	return instance
}

func (instance UserController) READ_users_VERSION_1() []string {
	return []string{"alice"}
}

func (instance UserController) READ_users_VERSION_2() ctx.Map {
	return ctx.Map{"users": []string{"alice"}, "total": 1}
}

func (instance UserController) READ_health_VERSION_NEUTRAL() string {
	return "ok"
}

func main() {
	app := core.New()
	app.EnableVersioning(versioning.Versioning{
		Type:           versioning.HeaderVersion,
		Key:            "X-Api-Version",
		DefaultVersion: "1",
	})
	app.Create(core.ModuleBuilder().Controllers(UserController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /users` answers `["alice"]`, and `GET /users` with `X-Api-Version: 2` answers
`{"total":1,"users":["alice"]}`. `GET /health` answers for any version.

| `Type` | Where the version is read | Example with the default `Key` (`v`) |
|---|---|---|
| `versioning.QueryVersion` | A query parameter | `GET /users?v=2` |
| `versioning.HeaderVersion` | A request header | `v: 2` |
| `versioning.MediaType` | A parameter of the `Accept` header | `Accept: application/json;v=2` |
| `versioning.CustomVersion` | Your `Extractor` function, `func(*ctx.HTTPContext) string` | |

- `DefaultVersion` is the version of a request that carries none.
- `VERSION_NEUTRAL` marks a route that answers for every version, and for requests without one.
- Once versioning is on, give every handler a `VERSION` token. A route without one no longer
  answers: requests get `404`, or `410 Gone` for a request without a version when
  `DefaultVersion` is empty.

See [versioning](versioning/README.md).

---

## Static files

A `SERVE_…` handler returns the directory to serve. `SERVE_ANY_OF_assets` serves the route
`/assets/*`:

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type AssetController struct {
	common.HTTP
}

func (instance AssetController) NewController() core.Controller {
	return instance
}

func (instance AssetController) SERVE_ANY_OF_assets(c *ctx.HTTPContext) string {
	c.ResponseWriter.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	return "public/assets"
}

func main() {
	app := core.New()
	app.Create(core.ModuleBuilder().Controllers(AssetController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /assets/app.js` serves `public/assets/app.js`.

- A directory answers its `index.html`, or `404`. No response is ever a directory listing.
- A path that leaves the directory answers `400`. A segment that starts with `.` (`.env`,
  `.git/config`) answers `404`, except `.well-known`. A symbolic link is followed only when its
  target stays inside the directory.
- Files are sent with `Cache-Control: no-cache` unless a layer or the handler sets
  `Cache-Control`, as above. Set a long lifetime only for files whose names change with their
  content, such as `app.3f9a1c.js`.

See [core › Static files](core/README.md#static-files).

---

## Built-in modules

### Config

The config module reads the environment and `.env` files into one `ConfigService`, with type
binding, variable expansion and validation hooks. It never prints a value. See the
[Config module documentation](modules/config/README.md).

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/modules/config"
)

type DatabaseService struct {
	ConfigService config.ConfigService
	Host          string
}

func (instance DatabaseService) NewProvider() core.Provider {
	instance.Host, _ = instance.ConfigService.Get("DB_HOST").(string)
	return instance
}

type InfoController struct {
	common.HTTP
	DatabaseService DatabaseService
}

func (instance InfoController) NewController() core.Controller {
	return instance
}

func (instance InfoController) READ_info() string {
	return instance.DatabaseService.Host
}

func main() {
	app := core.New()
	app.Create(
		core.ModuleBuilder().
			Imports(
				config.Register(&config.ConfigModuleOptions{
					IsGlobal:          true,
					IsExpandVariables: true,
				}),
			).
			Controllers(InfoController{}).
			Providers(DatabaseService{}).
			Build(),
	)
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

**`ConfigModuleOptions`:**

| Field | Type | Description |
|---|---|---|
| `IsGlobal` | `bool` | Makes `ConfigService` available to every module. |
| `IsIgnoreEnvFile` | `bool` | Reads the process environment only, without `.env` files. |
| `IsOverride` | `bool` | Lets `.env` values override the process environment. |
| `IsExpandVariables` | `bool` | Expands `${VAR}` in values. |
| `ENVFilePaths` | `[]string` | The `.env` files to read. The default is `.env` in the directory of the nearest `go.mod`. |
| `Loads` | `[]config.ConfigLoadFn` | Functions that return more configuration as a `map[string]any`. |
| `Hooks` | `[]config.ConfigHookFn` | Functions that receive the `ConfigService` at startup, to validate or transform it. |
| `OnInit` | `config.ConfigOnInitFn` | Runs when the module is initialized. |

### Cache

The cache module stores bytes by key, with an expiry and an atomic counter. The default store is
in memory and bounded (65,536 entries, 16 MiB), with no goroutine while it is empty. You can pass
any backend that implements `cache.Cache`. See the
[Cache module documentation](modules/cache/README.md).

```go
package main

import (
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/modules/cache"
)

type ProfileController struct {
	common.HTTP
	Cache cache.CacheService
}

func (instance ProfileController) NewController() core.Controller {
	return instance
}

func (instance ProfileController) READ_profiles_BY_id(c *ctx.HTTPContext) string {
	key := "profile:" + c.Param().Get("id")
	if data, ok := instance.Cache.Get(c.Context(), key); ok {
		return string(data)
	}

	profile := "profile of " + c.Param().Get("id")
	_ = instance.Cache.Set(c.Context(), key, []byte(profile), 5*time.Minute)
	return profile
}

func main() {
	app := core.New()
	app.Create(
		core.ModuleBuilder().
			Imports(cache.Register(&cache.CacheModuleOptions{IsGlobal: true})).
			Controllers(ProfileController{}).
			Build(),
	)
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

**`CacheModuleOptions`:**

| Field | Type | Description |
|---|---|---|
| `IsGlobal` | `bool` | Makes `CacheService` available to every module. |
| `OnInit` | `cache.CacheOnInitFn` | Runs once when the app that imports the module is created. |
| `Backend` | `cache.Cache` | The store. The default is `memorycache.NewMemoryCache(nil)`. |

### Outbound HTTP calls

Ginject ships no HTTP client module. Call other services with `net/http`, and wrap the transport
with `trace.Transport` so the outgoing request continues the incoming request's trace
(`Traceparent`). See the [trace documentation](trace/README.md#outbound-calls).

```go
package main

import (
	"net/http"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/trace"
)

var client = &http.Client{Transport: trace.Transport(nil), Timeout: 10 * time.Second}

type AccountController struct {
	common.HTTP
	Logger common.Logger
}

func (instance AccountController) NewController() core.Controller {
	return instance
}

func (instance AccountController) READ_profile(c *ctx.HTTPContext) int {
	req, err := http.NewRequestWithContext(c.Context(), http.MethodGet, "https://accounts.example.com/me", nil)
	if err != nil {
		panic(err)
	}
	res, err := client.Do(req)
	if err != nil {
		instance.Logger.From(c.Context()).Error("AccountsCallFailed", "error", err)
		panic(exception.BadGatewayException("Accounts service unavailable"))
	}
	defer res.Body.Close()
	return res.StatusCode
}

func main() {
	app := core.New()
	app.Create(core.ModuleBuilder().Controllers(AccountController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

The error is logged, not passed to the exception: an exception's cause becomes the `error` field
of the response, and this one names an internal host.

---

## Built-in middlewares

Every middleware is opt-in: an app that doesn't bind one pays nothing for it. Each package has its
own README with every option, its defaults and their sources.

| Package | What it does |
|---|---|
| [`middlewares/loadshed`](middlewares/loadshed/README.md) | Keeps the server fast under overload: a limit on requests in flight that adapts to measured latency, then `503` with `Retry-After` |
| [`middlewares/proxy`](middlewares/proxy/README.md) | The real client IP, scheme and host behind proxies you trust (`X-Forwarded-*` or `Forwarded`) |
| [`middlewares/hostcheck`](middlewares/hostcheck/README.md) | Answers only for the host names you list (DNS rebinding, host header injection) |
| [`middlewares/httpsredirect`](middlewares/httpsredirect/README.md) | Redirects plain HTTP to HTTPS with `308`, never to another host |
| [`middlewares/helmet`](middlewares/helmet/README.md) | The OWASP security response headers (CSP, HSTS over HTTPS only, COOP, CORP and more) |
| [`middlewares/cors`](middlewares/cors/README.md) | Lets the browser pages you list call your API from another origin |
| [`middlewares/csrf`](middlewares/csrf/README.md) | Refuses cross-origin state-changing requests from browsers (Fetch Metadata and `Origin`, no token) |
| [`middlewares/compress`](middlewares/compress/README.md) | gzip for text responses, with bounded memory |

Rate limiting per client is a guard, not a middleware: [`guards/throttler`](guards/throttler/README.md).
The access log is a switch on the app: `app.EnableAccessLog()` ([`accesslog`](accesslog/README.md)).

---

## Production checklist

A new app is safe to run as it is: the server has timeouts, a 10 MiB body limit and graceful
shutdown ([core › Server](core/README.md#server)). Before you put it on the internet, turn on the
pieces below. Each one costs nothing until you bind it.

### Logs
1. **JSON logs.** The built-in logger writes JSON when stdout is not a terminal, which is the case
   in a container. Set it explicitly and add the fields to mask:
   `app.SetLogger(log.NewLog(&log.LogOptions{LogFormat: log.JSONFormat, MaskFields: []string{"password"}}))`
   ([log](log/README.md)).
2. **Access log.** `app.EnableAccessLog()` writes one line per request with its status, its total
   time and the time of each middleware, guard and handler ([accesslog](accesslog/README.md)).
3. **Log with the request's context.** Log through `Logger.From(c.Context())` in handlers and
   services, so every line carries the request's `trace_id` and `request` ID. See
   [Find the logs of a request](log/README.md#find-the-logs-of-a-request).

### Middlewares, in this order
Bind them with one `BindGlobalMiddlewares` call. Middlewares run in the order you bind them, and
the order matters:

1. **`loadshed`** first, so a request refused under overload costs as little as possible.
2. **`proxy`**, when the app runs behind a load balancer or reverse proxy. It must come before
   every middleware that reads the client: `hostcheck`, `httpsredirect`, `helmet`, `csrf`, the
   throttler and the access log. Without it they all see the proxy's address and the connection's
   scheme. List your proxies' addresses in `TrustedProxies`: no forwarding header is trusted
   otherwise.
3. **`hostcheck`**, with the host names your app serves.
4. **`httpsredirect`**, unless your proxy already redirects HTTP to HTTPS. Set `Host` so the target
   never comes from the client's `Host` header.
5. **`helmet`** for the security headers. Its default Content Security Policy allows only your own
   origin: set your own policy if your pages load scripts or styles from a CDN.
6. **`cors`**, only when browser pages on another origin call your API.
7. **`csrf`**, when browsers send cookies or HTTP authentication to your app (sessions, HTML
   forms). List the origins that `cors` allows with credentials in `TrustedOrigins` too.
8. **`compress`**, unless your proxy already compresses responses. Don't bind it to routes that
   put a secret next to text from the request in one response (BREACH).

### Guards
- **`throttler`** limits each client: bind it globally with a budget your real users never reach,
  and with a tighter one on routes such as logins and password resets. It reads the client IP from
  `proxy`.

### Example

```go
package main

import (
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/guards/throttler"
	"github.com/dangduoc08/ginject/log"
	"github.com/dangduoc08/ginject/middlewares/compress"
	"github.com/dangduoc08/ginject/middlewares/cors"
	"github.com/dangduoc08/ginject/middlewares/csrf"
	"github.com/dangduoc08/ginject/middlewares/helmet"
	"github.com/dangduoc08/ginject/middlewares/hostcheck"
	"github.com/dangduoc08/ginject/middlewares/httpsredirect"
	"github.com/dangduoc08/ginject/middlewares/loadshed"
	"github.com/dangduoc08/ginject/middlewares/proxy"
)

type HelloController struct {
	common.HTTP
}

func (instance HelloController) NewController() core.Controller {
	return instance
}

func (instance HelloController) READ_hello() string {
	return "hello"
}

func main() {
	app := core.New()

	app.SetLogger(log.NewLog(&log.LogOptions{
		LogFormat:  log.JSONFormat,
		MaskFields: []string{"password", "token"},
	}))
	app.EnableAccessLog()

	app.BindGlobalMiddlewares(
		loadshed.LoadShed{},
		proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}},
		hostcheck.HostCheck{Hosts: []string{"example.com"}},
		httpsredirect.HTTPSRedirect{Host: "example.com"},
		helmet.Helmet{},
		cors.CORS{
			AllowOrigins:       []string{"https://app.example.com"},
			IsAllowCredentials: true,
		},
		csrf.CSRF{TrustedOrigins: []string{"https://app.example.com"}},
		compress.Compress{},
	)

	app.BindGlobalGuards(throttler.Throttler{Limit: 300, TTL: time.Minute})

	app.Create(
		core.ModuleBuilder().
			Controllers(HelloController{}).
			Build(),
	)

	if err := app.Listen(8080, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

Behind a load balancer at `10.0.0.7`, a request for `https://example.com/hello` from
`198.51.100.20` answers `hello` with the security and rate limit headers, and logs one access line
with `"ip":"198.51.100.20"`. The same request over plain HTTP answers
`308 Location: https://example.com/hello`.

### WebSocket
Middlewares bound with `BindGlobalMiddlewares` don't run on the WebSocket handshake. Pass the ones
the handshake needs to `EnableWS`, in the same order, for example
`app.EnableWS(&core.WSConfig{Path: "ws"}, loadshed.LoadShed{}, proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}}, hostcheck.HostCheck{Hosts: []string{"example.com"}}, cors.CORS{AllowOrigins: []string{"https://app.example.com"}})`.
`cors` then refuses a handshake from an origin you didn't list.

### Security notes
- `c.Redirect` sends the client to any URL: never pass a URL from the request unchecked
  ([ctx › Redirect](ctx/README.md#redirect)).
- `c.JSON` and `c.Text` insert format arguments unescaped: pass request data in a map or struct
  instead ([ctx › JSON](ctx/README.md#json)).
- `trace.Transport` sends your trace ID and request ID to every host you call
  ([trace › Outbound calls](trace/README.md#outbound-calls)).
- Change response headers with `Header().Set`, `Add` or `Del`, never by writing into a value slice
  in place ([core › Response writer](core/README.md#response-writer)).

---

## Exceptions

An exception carries a status code, a message for the client and an error text. Panic with one
from a guard, a pipe, a handler or an interceptor:

```go
package main

import (
	"database/sql"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/exception"
)

type InvoiceController struct {
	common.HTTP
}

func (instance InvoiceController) NewController() core.Controller {
	return instance
}

func (instance InvoiceController) READ_invoices() string {
	panic(exception.NotFoundException("Invoice not found", exception.ExceptionOptions{
		Description: "no invoice with this id",
		Cause:       sql.ErrNoRows,
	}))
}

func main() {
	app := core.New()
	app.Create(core.ModuleBuilder().Controllers(InvoiceController{}).Build())
	if err := app.Listen(3000, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /invoices` answers `404` with
`{"code":404,"error":"no invoice with this id: sql: no rows in result set","message":"Invoice not found"}`.

- **Message:** the first argument, the `message` field. Write it for the client.
- **Error text:** the `error` field. It is the code's status text by default, a `string` option,
  or `ExceptionOptions.Description`. A cause (an `error` option or `ExceptionOptions.Cause`) adds
  its text, and `errors.Is` and `errors.As` find it.
- **Custom codes:** `exception.NewException("Too many attempts", 429)` builds an exception with
  any code. A code outside 400 to 599 answers `500`.

| Constructor | Code |
|---|---|
| `BadRequestException` | 400 |
| `UnauthorizedException` | 401 |
| `ForbiddenException` | 403 |
| `NotFoundException` | 404 |
| `MethodNotAllowedException` | 405 |
| `NotAcceptableException` | 406 |
| `RequestTimeoutException` | 408 |
| `ConflictException` | 409 |
| `GoneException` | 410 |
| `PreconditionFailedException` | 412 |
| `RequestEntityTooLargeException` | 413 |
| `UnsupportedMediaTypeException` | 415 |
| `TeapotException` | 418 |
| `MisdirectedRequestException` | 421 |
| `UnprocessableEntityException` | 422 |
| `TooManyRequestsException` | 429 |
| `InternalServerErrorException` | 500 |
| `NotImplementedException` | 501 |
| `BadGatewayException` | 502 |
| `ServiceUnavailableException` | 503 |
| `GatewayTimeoutException` | 504 |
| `HTTPVersionNotSupportedException` | 505 |

WebSocket events also have constructors with WebSocket close codes, such as
`PolicyViolationException` (1008) and `TopicNotFoundException` (4004). See
[exception](exception/README.md).

---

## License

[MIT](LICENSE.md)
