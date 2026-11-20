<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img src="docs/assets/logo-light.svg" alt="Owlkei Go logo" width="120">
  </picture>
</p>

<h1 align="center">Owlkei Go</h1>

<p align="center">
  <strong>Build Go backends the way you think.</strong><br>
  Name a method, get a route. APIs, web pages and real-time apps in one framework,<br>
  with dependency injection that wires your app for you.
</p>

<p align="center">
  <a href="https://github.com/owlkei/go/actions/workflows/ci.yml"><img src="https://github.com/owlkei/go/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI status"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/dangduoc08/go?logo=go&amp;label=Go" alt="Go version"></a>
  <a href="LICENSE.md"><img src="https://img.shields.io/github/license/dangduoc08/go?color=4D7A5C" alt="MIT license"></a>
  <img src="https://img.shields.io/badge/status-pre--release-E8A33D" alt="Status: pre-release">
  <a href="https://owlkei.com"><img src="https://img.shields.io/badge/part%20of-Owlkei-3E6A8A" alt="Part of Owlkei"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#core-concepts">Concepts</a> ·
  <a href="examples/">Examples</a> ·
  <a href="#module-lifecycle">Lifecycle</a> ·
  <a href="https://owlkei.com">Owlkei UI</a>
</p>

---

Most Go web code is plumbing: routes registered by hand, constructors passed down by hand, and the
same checks written in every handler. Owlkei Go takes that work off your plate. You write modules,
controllers and services; Owlkei Go reads their names and types once at startup and wires the whole
app, so a mistake stops the boot instead of reaching production. If you have used NestJS, you will
feel at home.

```go
type UserController struct {
	owlkei.HTTP
	Users UserService // injected by type
}

func (c UserController) READ_users_BY_id(param owlkei.Param) User { // GET /users/{id}
	return c.Users.Find(param.Get("id"))
}
```

Owlkei Go is part of [Owlkei](https://owlkei.com). It works on its own, and it pairs with **Owlkei UI**,
the component library: page routes render Owlkei UI's HTML markup, and its stylesheet and scripts are
served from your app's own origin.

> **Pre-release.** Owlkei Go is not published yet and its API may change before 1.0. See
> [Installation](#installation) to try it today.

## Why Owlkei Go

- **No routing code.** A method named `READ_users_BY_id` answers `GET /users/{id}`. You declare
  routes and WebSocket events by naming methods.
- **No wiring code.** Providers are injected into controllers by field type, and handler
  parameters by parameter type. There are no struct tags and no code generation.
- **Mistakes stop the boot.** A missing provider, a parameter that can't be injected or two
  handlers for one route make `Create()` panic, before the server takes a request.
- **Fast at run time.** Owlkei Go does its reflection once, at `Create()`. A request runs a plan
  that was built at startup.
- **Explicit protection.** Nothing runs unless you configure it. One `AppOptions` block holds the
  whole configuration, documented in [one place](#configuration), with a
  [production checklist](#production-checklist) to copy.
- **A safe server.** The server has timeouts, a request body limit and graceful shutdown. Only
  exceptions reach the client: any other panic answers a fixed `500` and is logged.

## Contents

- [Why Owlkei Go](#why-owlkei-go)
  - [Features](#features)
  - [Installation](#installation)
  - [Quick start](#quick-start)
  - [Examples](#examples)
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
  - [Module lifecycle](#module-lifecycle)
    - [Modules and module factories](#modules-and-module-factories)
    - [Factory signatures](#factory-signatures)
    - [Where a factory finds its arguments](#where-a-factory-finds-its-arguments)
    - [Startup timeout](#startup-timeout)
    - [Imports and AsyncImports](#imports-and-asyncimports)
    - [Timeline](#timeline)
    - [Module states](#module-states)
    - [What clients get](#what-clients-get)
    - [When hooks run](#when-hooks-run)
    - [Refusals](#refusals)
    - [Retry](#retry)
    - [Transient and permanent errors](#transient-and-permanent-errors)
    - [Don't retry in a goroutine of your own](#dont-retry-in-a-goroutine-of-your-own)
    - [After a module is ready](#after-a-module-is-ready)
    - [Compared with NestJS forRootAsync](#compared-with-nestjs-forrootasync)
  - [Pages](#pages)
  - [WebSocket](#websocket)
  - [Versioning](#versioning)
  - [Static files](#static-files)
  - [Built-in modules](#built-in-modules)
    - [Config](#config)
    - [Cache](#cache)
    - [Outbound HTTP calls](#outbound-http-calls)
  - [Configuration](#configuration)
    - [Start here](#start-here)
    - [Production checklist](#production-checklist)
    - [The built-in stages and their order](#the-built-in-stages-and-their-order)
    - [Who is the client: Proxy, Hosts, Origins](#who-is-the-client-proxy-hosts-origins)
    - [HTTP](#http)
    - [Server](#server)
    - [Pages (Render)](#pages-render)
    - [WebSocket options](#websocket-options)
    - [Versioning options](#versioning-options)
    - [Operations](#operations)
    - [From the environment](#from-the-environment)
    - [Behavior changes](#behavior-changes)
    - [Security notes](#security-notes)
  - [Exceptions](#exceptions)
  - [Package reference](#package-reference)
  - [License](#license)

---

## Features

- Reflection-based dependency injection, done once at startup: no tags, no code generation
- Routes and WebSocket events from method names, with kebab-case URLs by default
- Server-rendered HTML pages with `html/template`, the template picked by the route: layouts,
  partials, error pages by status, forms, a CSP nonce per response, hashed assets, page CSS and
  JS, and public files, from one `embed.FS`
- A fixed request pipeline: middleware, guard, interceptor, handler, exception filter
- Module factories that may fail with a startup deadline, and `AsyncImports`: modules that connect in
  the background while the app serves, their routes answering `503` until they are ready
- WebSocket with topics, publish and subscribe, and the same guards, interceptors and filters as HTTP
- API versioning from the query, a header, the `Accept` media type or your own function
- Typed handler parameters and pipes that bind and validate input
- Static file serving that never lists a directory or serves a dotfile
- Config and Cache modules
- Opt-in production protections, set in `AppOptions` and run in a fixed order before routing: load
  shedding, trusted proxies, host check, HTTPS redirect, security headers, CORS, CSRF and
  compression, plus a rate limit guard
- One `AppOptions` struct for the whole configuration, also filled from `OK_*` variables and `.env`
- JSON logs with request and trace IDs, and an access log

---

## Installation

Owlkei Go needs Go 1.27.4 or later.

```bash
go get go.owlkei.com/owlkei
```

Until `go.owlkei.com` serves the module, `go get` can't find it. Clone the repository next to your
project and point the import path at it:

```bash
git clone https://github.com/owlkei/go ../owlkei-go
go mod edit -require=go.owlkei.com/owlkei@v0.0.0 -replace=go.owlkei.com/owlkei=../owlkei-go
go mod tidy
```

---

## Quick start

Create `main.go`:

```go
package main

import "go.owlkei.com/owlkei"

type GreetingService struct{}

func (instance GreetingService) NewProvider() owlkei.Provider {
	return instance
}

func (instance GreetingService) Greet(name string) string {
	return "hello " + name
}

type HelloController struct {
	owlkei.HTTP
	GreetingService GreetingService
}

func (instance HelloController) NewController() owlkei.Controller {
	return instance
}

func (instance HelloController) READ_hello() string {
	return "hello"
}

func (instance HelloController) READ_hello_BY_name(param owlkei.Param) owlkei.Map {
	return owlkei.Map{"greeting": instance.GreetingService.Greet(param.Get("name"))}
}

func main() {
	app := owlkei.New(nil)
	app.Create(
		owlkei.ModuleBuilder().
			Controllers(HelloController{}).
			Providers(GreetingService{}).
			Build(),
	)
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

Run it with `go run .`, then call it:

- `GET http://localhost:3000/hello` answers `hello` as `text/plain`.
- `GET http://localhost:3000/hello/joe` answers `{"greeting":"hello joe"}` as `application/json`.
- `GET http://localhost:3000/missing` answers `404` with `{"code":404,"error":"Not Found","message":"Not Found"}`.

---

## Examples

Five small apps in [`examples/`](examples/), in the order to read them. Each one is its own Go module
with a README and tests: copy a folder out of the repository and it is the start of your project.

| Example | You learn |
|---|---|
| [01 · Hello](examples/01-hello/README.md) | A controller and routes from method names, in under 40 lines |
| [02 · REST API](examples/02-rest-api/README.md) | Modules, a shared service, a validating pipe, `404` and `422` |
| [03 · Pages](examples/03-pages/README.md) | Server-rendered pages and forms with Owlkei UI's HTML components |
| [04 · Async modules](examples/04-async-modules/README.md) | Start before a dependency is ready; `503` until it is, `Retry` with backoff |
| [05 · Production](examples/05-production/README.md) | Load shedding, security headers, CORS, rate limits, body limits, graceful shutdown |

---

## Core concepts

### Application

`owlkei.New(opts)` creates an application from an `*owlkei.AppOptions` (`nil` gives the defaults),
then you wire it once with `Create` and serve it:

1. **Configure.** Every setting is a field of `owlkei.AppOptions`, read once by `New`. A bad value
   panics in `New` with `invalid app: <Field> …`. `owlkei.OptionsFromEnv()` fills the same struct
   from `OK_*` variables and `.env` (see [Configuration](#configuration)).
2. **Bind app-wide layers.** `BindGlobalMiddlewares`, `BindGlobalGuards`, `BindGlobalInterceptors`
   and `BindGlobalExceptionFilters` run before `Create`; after it they panic with
   `invalid app: <Method> must be called before Create`.
3. **Create.** `app.Create(rootModule)` builds the module tree, constructs the providers and
   controllers, and plans every handler. Call it once per app.
4. **Serve.** `app.Listen()` opens `Server.Port` (default 3000) and blocks until the server stops.
   With `Server.TLS` it serves HTTPS and HTTP/2. `app.Serve(listener)` serves on a
   `net.Listener` you made.

| Field | Effect |
|---|---|
| `Logger` | The app's logger. Read it back with `app.Logger()`. |
| `StartupTimeout` | How long `Create()` may take for the module factories of `Imports` (default 30 s). See [Startup timeout](#startup-timeout). |
| `IsAccessLog` | One access log line per request. |
| `IsDevtool` | A snapshot of the routes and layers for development tools. |
| `Server` | `Port`, `ShutdownTimeout`, `TLS` (`CertFile`, `KeyFile`, `Config`) and `IsH2C`, used by `Listen` and `Serve`. |
| `HTTP` | `MaxBodyBytes` (default 10 MiB, negative turns the cap off) for every request, also in your own `http.Server`. |
| `Versioning` | Routes per API version. See [Versioning](#versioning). |
| `WS` | The WebSocket server. See [WebSocket](#websocket). |
| `Render` | Server-rendered HTML pages. See [Pages](#pages). |
| `Proxy`, `Hosts`, `Origins`, `LoadShed`, `HTTPSRedirect`, `Headers`, `CORS`, `CSRF`, `Compress` | The built-in protections, each off until set. See [Configuration](#configuration). |

The server shuts down gracefully on `SIGINT`, `SIGTERM` or `app.Stop()`: running requests get
30 seconds to finish. An `App` is also an `http.Handler`, so you can serve it with your own
`http.Server` or call it from tests with `httptest`. See [core › Server](internal/core/README.md#server).

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.owlkei.com/owlkei"
)

type HealthController struct {
	owlkei.HTTP
}

func (instance HealthController) NewController() owlkei.Controller {
	return instance
}

func (instance HealthController) READ_health() string {
	return "ok"
}

func TestHealth(t *testing.T) {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(HealthController{}).Build())

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}
```

Each `owlkei.New(…)` is independent: two apps in one process have their own providers, routes,
layers and logger.

### Module

A module groups controllers and providers, and imports other modules. Build one with
`owlkei.ModuleBuilder()`:

```go
package main

import "go.owlkei.com/owlkei"

type UserService struct{}

func (instance UserService) NewProvider() owlkei.Provider {
	return instance
}

func (instance UserService) FindAll() []string {
	return []string{"alice", "bob"}
}

type UserController struct {
	owlkei.HTTP
	UserService UserService
}

func (instance UserController) NewController() owlkei.Controller {
	return instance
}

func (instance UserController) READ_users() []string {
	return instance.UserService.FindAll()
}

var UserModule = owlkei.ModuleBuilder().
	Controllers(UserController{}).
	Providers(UserService{}).
	Build()

var AppModule = owlkei.ModuleBuilder().
	Imports(UserModule).
	Build().
	Prefix("api")

func main() {
	app := owlkei.New(nil)
	app.Create(AppModule)
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /api/users` answers `["alice","bob"]`.

- **Prefix:** `Prefix("api")` adds a path prefix to every route of the module and of the modules
  it imports. A prefix on an importing module goes in front: a child with `Prefix("v1")` imported
  by a root with `Prefix("api")` serves `/api/v1/…`.
- **Path style:** `ModuleBuilder().PathStyle(owlkei.PathStyleAsWritten)` keeps `_` in the URLs of
  the module and of the modules it imports, instead of `-`. See
  [Routes from method names](#routes-from-method-names).
- **Global modules:** set `IsGlobal = true` on a module to make its providers visible to every
  module, without an import. The root module's providers are always global.
- **Module factories:** `Imports` also takes a function that returns a `owlkei.Module`, and
  optionally an error. Owlkei Go calls it once per app and injects its arguments, so a module can
  depend on configuration or connect to a database first. `config.Register(…)` and
  `cache.Register(…)` are not factories: you call them, and they return a module.
- **Async imports:** `AsyncImports` takes the same entries as `Imports`, but builds them in the
  background: the app serves at once, and the routes that need them answer `503` until they are
  ready.
- **Hooks:** a module's `OnInit` runs before its providers are constructed, `OnReady` at the end
  of `Create`, and `OnShutdown` when the server shuts down, in reverse order.

The details, with the timeline of `Create()` and of the background, are in
[Module lifecycle](#module-lifecycle).

```go
package main

import (
	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/modules/config"
)

type Database struct {
	URL string
}

func (instance Database) NewProvider() owlkei.Provider {
	return instance
}

func NewDatabaseModule(configService config.ConfigService) owlkei.Module {
	url, _ := configService.Get("DATABASE_URL").(string)
	module := owlkei.ModuleBuilder().
		Providers(Database{URL: url}).
		Build()
	module.IsGlobal = true
	module.OnShutdown = func() {
		// Close the connection pool here.
	}
	return module
}

type StatusController struct {
	owlkei.HTTP
	Database Database
}

func (instance StatusController) NewController() owlkei.Controller {
	return instance
}

func (instance StatusController) READ_status() bool {
	return instance.Database.URL != ""
}

func main() {
	app := owlkei.New(nil)
	app.Create(
		owlkei.ModuleBuilder().
			Imports(
				config.Register(&config.ConfigModuleOptions{IsGlobal: true, IsIgnoreEnvFile: true}),
				NewDatabaseModule,
			).
			Controllers(StatusController{}).
			Build(),
	)
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`NewDatabaseModule` receives the `ConfigService` of the global config module. A factory also sees
the providers of the other modules in the same `Imports` list. An argument that no provider
supplies stops `Create()` with `dependency injection: can't resolve argument of '<func>' …`.

### Provider

A provider is a struct with a `NewProvider() owlkei.Provider` method. It holds your business logic:
services, repositories, clients.

- Owlkei Go constructs each provider type once per app. It first injects the provider's exported
  fields, then calls `NewProvider`, and keeps what `NewProvider` returns.
- Fields are matched by type: declare a field of a provider's type and Owlkei Go fills it. A field
  whose type no visible provider has stops `Create()` with
  `dependency injection: can't resolve dependency '<type>' of the provider…`.
- A provider is visible to its own module, to the modules that import it, and to every module
  when its module is global.
- Providers are injected by value. Put state that must be shared, such as a connection pool,
  behind a pointer.
- A field of type `log.Logger` gets the app's logger, and a field of type `owlkei.Publisher`
  the app's WebSocket publisher.

Never call `NewProvider` yourself: the instance it returns has no injected fields. To read a
provider from outside the module tree, use `app.Get(UserService{}).(UserService)` after `Create`.

### Controller

A controller is a struct with a `NewController() owlkei.Controller` method. Embed `owlkei.HTTP` for
HTTP routes and HTML pages (see [Pages](#pages)) or `owlkei.WS` for WebSocket events, plus one
embed per kind of layer you bind:
`owlkei.Middleware`, `owlkei.Guard`, `owlkei.Interceptor` and `owlkei.ExceptionFilter`.

Bind layers in `NewController`. Pass method values to bind a layer to some handlers only, or none
to bind it to every handler of the controller:

```go
package main

import (
	"net/http"

	"go.owlkei.com/owlkei"
)

type AdminGuard struct{}

func (instance AdminGuard) CanActivate(c owlkei.HTTPContext) bool {
	return c.Header().Get("X-Role") == "admin"
}

type NoStoreMiddleware struct{}

func (instance NoStoreMiddleware) Use(r *http.Request, w http.ResponseWriter, next owlkei.Next) {
	w.Header().Set("Cache-Control", "no-store")
	next()
}

type ProductController struct {
	owlkei.HTTP
	owlkei.Guard
	owlkei.Middleware
}

func (instance ProductController) NewController() owlkei.Controller {
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

func (instance ProductController) DELETE_products_BY_id(param owlkei.Param) string {
	return "deleted " + param.Get("id")
}

func main() {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(ProductController{}).Build())
	if err := app.Listen(); err != nil {
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

Upper-case words are route words. Every other word is path text, written in lower case or
camelCase.

| Token | Meaning | Example |
|---|---|---|
| `READ` | `GET` (and `HEAD`) | `READ_users` → `GET /users` |
| `CREATE` | `POST` | `CREATE_users` → `POST /users` |
| `UPDATE` | `PUT` | `UPDATE_users_BY_id` → `PUT /users/{id}` |
| `MODIFY` | `PATCH` | `MODIFY_users_BY_id` → `PATCH /users/{id}` |
| `DELETE` | `DELETE` | `DELETE_users_BY_id` → `DELETE /users/{id}` |
| `PREFLIGHT` | `OPTIONS` | `PREFLIGHT_users` → `OPTIONS /users` |
| `RENDER` | An HTML page (`GET` and `HEAD`). See [Pages](#pages). | `RENDER_bookings` → `GET /bookings`, template `bookings.html` or `bookings/index.html` |
| `SUBMIT` | The form post of the `RENDER_` with the same words (`POST`) | `SUBMIT_new_OF_bookings` → `POST /bookings/new` |
| `SERVE` | Static files (`GET`) | `SERVE_ANY_OF_assets` → `GET /assets/*`. See [Static files](#static-files). |
| `BY` | The first parameter of the current resource | `READ_users_BY_id` → `GET /users/{id}` |
| `AND` | One more parameter of the current resource | `READ_files_BY_dir_AND_name` → `GET /files/{dir}/{name}` |
| `OF` | A parent resource, placed before the current one | `READ_posts_BY_post_id_OF_users_BY_user_id` → `GET /users/{user_id}/posts/{post_id}` |
| `ANY` | A wildcard `*` | `READ_ANY` → `GET /*` |
| `FILE` | Turns the `_` before the last word of a resource into a `.` | `READ_robots_txt_FILE` → `GET /robots.txt` |
| `VERSION` | The rest of the name is the API version | `READ_users_VERSION_2` → `GET /users`, version `2` |
| `ALL` | WebSocket only, as the last word: every deeper topic. See [WebSocket](#websocket). | `SUBSCRIBE_chat_ALL` → `chat.**` |

The rules that follow from the name:

- A bare operation is the root: `READ` answers `GET /`.
- **Kebab-case.** The words of one resource are joined with `-`: `READ_user_profile` is
  `GET /user-profile`, and `READ_site_map_xml_FILE` is `GET /site-map.xml`. For two segments,
  use `OF`: `READ_profile_OF_users` is `GET /users/profile`.
- A word is copied as written, never lower-cased or split: `READ_userProfile` is
  `GET /userProfile`, `READ_orders_2024` is `GET /orders-2024`.
- A parameter keeps its name as written: `BY_user_id` is read with `param.Get("user_id")`, and
  `BY_ID` with `param.Get("ID")`.
- A trailing slash in the request doesn't matter: `GET /users/7` and `GET /users/7/` both reach
  `READ_users_BY_id`.
- A static segment wins over a parameter: with `READ_me_OF_users` (`GET /users/me`) and
  `READ_users_BY_id`, `GET /users/me` reaches `READ_me_OF_users`.
- A method whose name doesn't start with an operation (`findUser`, `Helper`) is not a route.
- Two handlers with the same method, path shape and version stop `Create()` with
  `route conflict: <name> method is conflicted with <name> method`.

A request for a path with no route answers `404`. A request for a path that has a route under
another method answers `405` with an `Allow` header. `HEAD` is served by the `GET` route.

#### Segments that are not resources

`OF` places any word before the current resource, so a segment such as `new`, `edit` or `cancel`
is a resource of its own under its parent:

| Method | Route |
|---|---|
| `READ_new_OF_bookings` | `GET /bookings/new` |
| `READ_edit_OF_bookings_BY_id` | `GET /bookings/{id}/edit` |
| `CREATE_cancel_OF_bookings_BY_id` | `POST /bookings/{id}/cancel` |
| `READ_me_OF_users` | `GET /users/me` |

#### Deep nesting with a prefix

A long chain of `OF` makes long names. Put the shared part of the path in the controller's
`Prefix` instead. A prefix may hold parameters, which handlers read like any other:

```go
package main

import "go.owlkei.com/owlkei"

type CommentController struct {
	owlkei.HTTP
}

func (instance CommentController) NewController() owlkei.Controller {
	instance.Prefix("users/{user_id}/posts/{post_id}")
	return instance
}

func (instance CommentController) READ_comments() []string {
	return []string{"first"}
}

func (instance CommentController) READ_comments_BY_comment_id(param owlkei.Param) owlkei.Map {
	return owlkei.Map{
		"user":    param.Get("user_id"),
		"post":    param.Get("post_id"),
		"comment": param.Get("comment_id"),
	}
}

func main() {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(CommentController{}).Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /users/1/posts/2/comments/3` answers `{"comment":"3","post":"2","user":"1"}`. Without the
prefix, the same route is `READ_comments_BY_comment_id_OF_posts_BY_post_id_OF_users_BY_user_id`.
A prefix is never rewritten: `Prefix("payment_intents")` keeps its `_`.

#### Keep `_` in URLs with `PathStyle`

Some APIs keep `_` in their URLs (`/payment_intents`). Set `PathStyleAsWritten` once on the root
module, and every module it imports follows. A module that sets its own style keeps it, so a
reusable module's URLs never change with the app that imports it:

```go
package main

import "go.owlkei.com/owlkei"

type BillingController struct {
	owlkei.HTTP
}

func (instance BillingController) NewController() owlkei.Controller {
	return instance
}

func (instance BillingController) READ_payment_intents() []string {
	return []string{"pi_1"}
}

type StatusController struct {
	owlkei.HTTP
}

func (instance StatusController) NewController() owlkei.Controller {
	return instance
}

func (instance StatusController) READ_health_checks() string {
	return "ok"
}

var StatusModule = owlkei.ModuleBuilder().
	PathStyle(owlkei.PathStyleKebab).
	Controllers(StatusController{}).
	Build()

var AppModule = owlkei.ModuleBuilder().
	PathStyle(owlkei.PathStyleAsWritten).
	Imports(StatusModule).
	Controllers(BillingController{}).
	Build()

func main() {
	app := owlkei.New(nil)
	app.Create(AppModule)
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /payment_intents` answers `["pi_1"]`, and `GET /health-checks` answers `ok`.

| Style | `READ_payment_intents` | Default |
|---|---|---|
| `owlkei.PathStyleKebab` | `GET /payment-intents` | yes |
| `owlkei.PathStyleAsWritten` | `GET /payment_intents` | no |

- The style applies to the words of a resource only. Prefixes, exact paths, parameter names,
  versions and WebSocket events never change.
- It changes nothing at request time: with the default style, `GET /payment_intents` answers `404`.
- A module imported by two modules with different styles gets the first one found, in the order
  that module prefixes are stacked.
- Any other value stops the call:
  `invalid module: PathStyle needs owlkei.PathStyleKebab or owlkei.PathStyleAsWritten`.

#### Write a path by hand with `Path`

When no name can spell a path (`/oauth2/token`), give the handler an exact path in `NewController`:

```go
package main

import "go.owlkei.com/owlkei"

type TokenController struct {
	owlkei.HTTP
}

func (instance TokenController) NewController() owlkei.Controller {
	instance.Path("/oauth2/token", instance.CREATE_token)
	return instance
}

func (instance TokenController) CREATE_token() owlkei.Map {
	return owlkei.Map{"access_token": "t0k3n"}
}

func main() {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(TokenController{}).Build().Prefix("api"))
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`POST /api/oauth2/token` answers `201` with `{"access_token":"t0k3n"}`.

- `Path` replaces only what the name spells. The operation still gives the HTTP method, `VERSION`
  the version, and the controller and module prefixes still apply.
- The path is never kebab-cased. It may hold `{name}` parameters and `*`.
- `Path("/token", instance.READ_token, instance.CREATE_token)` gives one path to two routes.
- The name is still checked by the rules below.
- These calls stop `Create()`, all with `invalid route:`: `Path` with no handler, a handler that is
  not a route of the controller, a second `Path` for one handler, and a path with `?` or `#`.

#### Names that stop the boot

A name that the parser would misread stops `Create()` instead of serving another URL. Each message
starts with `invalid route:`:

| Name | Why | Message |
|---|---|---|
| `READ_orgs_FB`, `READ_logo_PNG_FILE`, `SUBSCRIBE_chat_V2` | An upper-case word that is not a route word, so a future route word can't change the route | `FB in READ_orgs_FB is upper case but not a route word; write it in lower case or camelCase` |
| `READ_users_BY`, `READ_users_BY_id_AND` | `BY` or `AND` without a name | `BY in READ_users_BY has no parameter name` |
| `READ_files_BY_name_ANY_x` | A word after the parameters | `x in READ_files_BY_name_ANY_x comes after a parameter; start the next resource with OF` |
| `READ_OF_users`, `READ_users_OF` | `OF` with nothing on one side | `OF in READ_OF_users needs a resource or a parameter on both sides` |
| `READ_txt_FILE` | `FILE` without a name and an extension | `FILE in READ_txt_FILE needs a name and an extension before it` |
| `READ_logo_png_FILE_FILE` | `FILE` twice in one resource | `FILE twice in one resource of READ_logo_png_FILE_FILE` |
| `READ_logo_ANY_FILE` | `ANY` as the extension | `FILE in READ_logo_ANY_FILE needs a word as the extension, not ANY` |
| `SERVE_x_ANY_ANY`, `READ_ANY_ANY_FILE` | `ANY` twice in a row | `ANY twice in a row in SERVE_x_ANY_ANY; one ANY already matches any text` |
| `RENDER_files_ANY`, `RENDER_x_VERSION_2` | A page route with `ANY`, `VERSION` or `FILE` | `ANY in RENDER_files_ANY: a page route has no wildcard` |

Write `READ_orgsFB` or `READ_orgs_fb` instead of `READ_orgs_FB`. Digits are not upper case:
`READ_404` and `READ_users_VERSION_V2` are fine. The complete rules are in
[common › Route DSL](internal/common/README.md#route-dsl).

### Handler parameters

Declare the parameters a handler needs. Owlkei Go passes them by type, in any order:

```go
package main

import (
	"net/http"

	"go.owlkei.com/owlkei"
)

type OrderController struct {
	owlkei.HTTP
}

func (instance OrderController) NewController() owlkei.Controller {
	return instance
}

func (instance OrderController) READ_orders_BY_id(
	c owlkei.HTTPContext,
	param owlkei.Param,
	query owlkei.Query,
	header owlkei.Header,
) owlkei.Map {
	return owlkei.Map{
		"id":       param.Get("id"),
		"expand":   query.Get("expand"),
		"language": header.Get("Accept-Language"),
		"request":  c.ID(),
	}
}

func (instance OrderController) CREATE_orders(body owlkei.Body, w http.ResponseWriter) owlkei.Map {
	w.Header().Set("Location", "/orders/1")
	return owlkei.Map{"item": body.Get("item")}
}

func main() {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(OrderController{}).Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

| Parameter type | HTTP | WebSocket | Value |
|---|---|---|---|
| `owlkei.HTTPContext` | yes | no | The request's context: request data, status and response helpers. |
| `*http.Request`, `http.ResponseWriter` | yes | no | The request and the response writer. |
| `owlkei.Param` | yes | no | Path parameters. |
| `owlkei.Query` | yes | no | Query string values. |
| `owlkei.Header` | yes | no | Request headers. |
| `owlkei.Body` | yes | no | The JSON body, as a `map[string]any`. Malformed JSON answers `400`. |
| `owlkei.Form`, `owlkei.File` | yes | no | Form values and uploaded files of a urlencoded or multipart request. |
| `owlkei.Redirect` | yes | no | `c.Redirect`: sends a redirect. |
| `owlkei.WSContext` | no | yes | The message's context. |
| `owlkei.WSPayload` | no | yes | The message's `message` object. |
| `owlkei.Next` | yes | yes | Continues the pipeline. |
| `owlkei.Publisher` | yes | yes | Publishes to WebSocket subscribers. |
| A pipe | yes | yes | What its `Transform` returns. See [Pipes](#pipes). |

These types live in the root `owlkei` package. An application imports it, plus `exception`,
`log`, `modules/*` and `guards/*`; the engine's other packages are internal.

Any other parameter type stops `Create()`:
`dependency injection: can't resolve dependencies of the 'func(*ctx.HTTPContext, int) string'…`.

The context belongs to one request: never keep `c` or a value read from it after the handler
returns. Pass `c.Context()` to services and goroutines instead. It carries the request's trace and
request IDs. See [ctx](internal/ctx/README.md).

### Handler results

A handler returns nothing, one value, or a status code and a value:

| Result | Response |
|---|---|
| `string`, a number or a `bool` | Text, `text/plain; charset=utf-8` |
| A map, slice, struct, array or pointer | JSON, `application/json` |
| `any` | A string as text, anything else as JSON |
| `(int, value)` | The first result sets the status code |

The status is `200`, or `201` for `POST`, unless a layer or the handler sets another one. A page
route (`RENDER_`, `SUBMIT_`) renders its result into its template instead: see [Pages](#pages). To
write the response yourself, take `owlkei.HTTPContext` and call `c.Status(code).JSON(data)`,
`c.Text(…)` or `c.Redirect(url)`, and return nothing.

```go
package main

import (
	"net/http"

	"go.owlkei.com/owlkei"
)

type JobController struct {
	owlkei.HTTP
}

func (instance JobController) NewController() owlkei.Controller {
	return instance
}

func (instance JobController) CREATE_jobs() (int, owlkei.Map) {
	return http.StatusAccepted, owlkei.Map{"status": "queued"}
}

func (instance JobController) READ_jobs_BY_id(c owlkei.HTTPContext) {
	c.Status(http.StatusOK).JSON(owlkei.Map{"id": c.Param().Get("id")})
}

func main() {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(JobController{}).Build())
	if err := app.Listen(); err != nil {
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

The complete rules are in [core › Request lifecycle](internal/core/README.md#request-lifecycle).

### Middleware

A middleware is a struct with a `Use(*http.Request, http.ResponseWriter, owlkei.Next)` method. Call
`next()` to continue, or write a response and return to stop the request:

```go
package main

import (
	"net/http"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/log"
)

type RequestLogMiddleware struct {
	Logger log.Logger
}

func (instance RequestLogMiddleware) Use(r *http.Request, w http.ResponseWriter, next owlkei.Next) {
	instance.Logger.From(r.Context()).Info("Request", "method", r.Method, "path", r.URL.Path)
	next()
}

type APIKeyMiddleware struct{}

func (instance APIKeyMiddleware) Use(r *http.Request, w http.ResponseWriter, next owlkei.Next) {
	if r.Header.Get("X-Api-Key") == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"missing API key"}`))
		return
	}
	next()
}

type ReportController struct {
	owlkei.HTTP
	owlkei.Middleware
}

func (instance ReportController) NewController() owlkei.Controller {
	instance.BindMiddleware(APIKeyMiddleware{}, instance.READ_reports)
	return instance
}

func (instance ReportController) READ_reports() []string {
	return []string{"2026-q3"}
}

func main() {
	app := owlkei.New(nil)
	app.BindGlobalMiddlewares(RequestLogMiddleware{})
	app.Create(owlkei.ModuleBuilder().Controllers(ReportController{}).Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

- Exported fields of a middleware get providers injected, like a controller's. A
  `log.Logger` field gets the app's logger.
- `Logger.From(r.Context())` adds the request's `trace_id` and `request` ID to the log line.
- A middleware's panic never reaches your exception filters. An exception keeps its code and
  message, and any other value answers a fixed `500`. See
  [core › Errors in middleware](internal/core/README.md#errors-in-middleware).
- Middlewares don't run for WebSocket messages. See [WebSocket](#websocket).

### Guard

A guard decides whether a request may reach its handler. Write
`CanActivate(owlkei.HTTPContext) bool` for HTTP routes, or `CanActivate(owlkei.WSContext) bool` for
WebSocket events. A guard that returns `false` answers `403` with
`{"code":403,"error":"Forbidden","message":"Access denied"}`.

Owlkei Go reads the signature of `CanActivate` to know the guard's transport: a global HTTP guard
runs only for HTTP routes, and a WebSocket guard only for WebSocket events.

A guard can have an optional `NewGuard` constructor. Owlkei Go injects providers into the guard's
exported fields, calls `NewGuard` once at `Create()`, and runs the instance it returns. A guard
can't have unexported fields: they stop `Create()`.

```go
package main

import (
	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/modules/config"
)

type TokenGuard struct {
	ConfigService config.ConfigService
	Token         string
}

func (instance TokenGuard) NewGuard() TokenGuard {
	instance.Token, _ = instance.ConfigService.Get("API_TOKEN").(string)
	return instance
}

func (instance TokenGuard) CanActivate(c owlkei.HTTPContext) bool {
	return instance.Token != "" && c.Header().Get("Authorization") == "Bearer "+instance.Token
}

type AccountController struct {
	owlkei.HTTP
	owlkei.Guard
}

func (instance AccountController) NewController() owlkei.Controller {
	instance.BindGuard(TokenGuard{})
	return instance
}

func (instance AccountController) READ_account() string {
	return "account"
}

func main() {
	app := owlkei.New(nil)
	app.Create(
		owlkei.ModuleBuilder().
			Imports(config.Register(&config.ConfigModuleOptions{IsGlobal: true})).
			Controllers(AccountController{}).
			Build(),
	)
	if err := app.Listen(); err != nil {
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
`Intercept(owlkei.HTTPContext, owlkei.Aggregation) any`, or the same method with
`owlkei.WSContext` for WebSocket events.

- Return `a.Pipe(operators…)` to run the handler and pass its result through the operators.
- Return any other value to skip the handler: that value is the response.

| Operator | Effect |
|---|---|
| `a.Transform(func(data any) any)` | Replaces the data with the function's result. |
| `a.Tap(func(data any) any)` | Calls the function and keeps the data, for side effects such as logging. |
| `a.Filter(func(data any) bool)` | Stops the remaining operators when the function returns `false`. |

```go
package main

import "go.owlkei.com/owlkei"

type EnvelopeInterceptor struct{}

func (instance EnvelopeInterceptor) Intercept(c owlkei.HTTPContext, a owlkei.Aggregation) any {
	return a.Pipe(
		a.Transform(func(data any) any {
			return owlkei.Map{"data": data, "request": c.ID()}
		}),
	)
}

type TagController struct {
	owlkei.HTTP
}

func (instance TagController) NewController() owlkei.Controller {
	return instance
}

func (instance TagController) READ_tags() []string {
	return []string{"go", "web"}
}

func main() {
	app := owlkei.New(nil)
	app.BindGlobalInterceptors(EnvelopeInterceptor{})
	app.Create(owlkei.ModuleBuilder().Controllers(TagController{}).Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /tags` answers `{"data":["go","web"],"request":"<request ID>"}`. Bind an interceptor to one
controller with `instance.BindInterceptor(EnvelopeInterceptor{})` (embed `owlkei.Interceptor`).
The optional constructor is `NewInterceptor`.

### Exception filter

Panic with an exception anywhere from a guard onwards, and an exception filter turns it into the
response. Without a filter of your own, the built-in filter answers with the exception's code
and `{"code":<int>,"error":<string>,"message":<string>}`.

To change the response, write `Catch(owlkei.HTTPContext, *exception.Exception)`, or the same method
with `owlkei.WSContext` for WebSocket events:

```go
package main

import (
	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
)

type ProblemFilter struct{}

func (instance ProblemFilter) Catch(c owlkei.HTTPContext, ex *exception.Exception) {
	c.Status(ex.Code()).JSON(owlkei.Map{
		"status": ex.Code(),
		"title":  ex.StatusText(),
		"detail": ex.Message(),
	})
}

type BookController struct {
	owlkei.HTTP
	owlkei.ExceptionFilter
}

func (instance BookController) NewController() owlkei.Controller {
	instance.BindExceptionFilter(ProblemFilter{})
	return instance
}

func (instance BookController) READ_books_BY_id(param owlkei.Param) string {
	if param.Get("id") != "1" {
		panic(exception.NotFoundException("Book not found"))
	}
	return "Go in Action"
}

func main() {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(BookController{}).Build())
	if err := app.Listen(); err != nil {
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
that takes a source and a `owlkei.ArgumentMetadata`, and returns a value of the pipe's own type.
Declare the pipe as a handler parameter: Owlkei Go calls `Transform` before the handler and passes
its result.

| Source | `Transform` method |
|---|---|
| The JSON body | `Transform(owlkei.Body, owlkei.ArgumentMetadata) any` |
| The query string | `Transform(owlkei.Query, owlkei.ArgumentMetadata) any` |
| The path parameters | `Transform(owlkei.Param, owlkei.ArgumentMetadata) any` |
| The request headers | `Transform(owlkei.Header, owlkei.ArgumentMetadata) any` |
| The form values | `Transform(owlkei.Form, owlkei.ArgumentMetadata) any` |
| The uploaded files | `Transform(owlkei.File, owlkei.ArgumentMetadata) any` |
| The request's context | `Transform(owlkei.HTTPContext, owlkei.ArgumentMetadata) any` |
| A WebSocket message | `Transform(owlkei.WSPayload, owlkei.ArgumentMetadata) any` |

`ArgumentMetadata` tells the pipe where it runs: `ContextType` is `http` or `ws`, and `ParamType`
is the source (`body`, `query`, `param`, `header`, `form`, `file`, `context` or `wsPayload`).

`Bind` and `BindValue` copy the source into a struct, field by field, by the `bind` tag:

```go
package main

import (
	"strings"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
)

type CreateUserBody struct {
	Name  string `bind:"name"`
	Email string `bind:"email"`
}

func (instance CreateUserBody) Transform(body owlkei.Body, _ owlkei.ArgumentMetadata) any {
	user := body.BindValue(instance).(CreateUserBody)
	if user.Name == "" || !strings.Contains(user.Email, "@") {
		panic(exception.BadRequestException("A name and a valid email are required"))
	}
	return user
}

type SignupController struct {
	owlkei.HTTP
}

func (instance SignupController) NewController() owlkei.Controller {
	return instance
}

func (instance SignupController) CREATE_users(user CreateUserBody) owlkei.Map {
	return owlkei.Map{"name": user.Name, "email": user.Email}
}

func main() {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(SignupController{}).Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`POST /users` with `{"name":"ann","email":"ann@example.com"}` answers `201` with the user.
With `{}` it answers `400`.

- A pipe's exported fields get providers injected at `Create()`, like a controller's.
- `Transform` runs on a fresh copy of the pipe for every request: changing its fields affects no
  other request.
- A pipe can't have unexported fields: they stop `Create()` with
  `dependency injection: can't set value to unexported '<field>' field of the main.CreateUserBody pipe`.
- A pointer parameter (`*CreateUserBody`) stops `Create()`. A `Transform` that returns another
  type answers `500`.

See [core › Pipes](internal/core/README.md#pipes) and [ctx › Binding](internal/ctx/README.md#binding).

---

## Module lifecycle

This section explains how Owlkei Go builds modules, in which order, and what happens when a module
needs a slow or unreliable dependency such as a database. Read it before you write a module that
connects to something.

### Modules and module factories

`Imports` and `AsyncImports` take two kinds of entries:

- **Module:** a value built with `owlkei.ModuleBuilder()…Build()`. Its providers and controllers
  are known before `Create()`.
- **Module factory:** a function that returns a module. Owlkei Go calls it during `Create()` (or in
  the background, with `AsyncImports`) and injects its parameters. Use a factory when the module
  needs configuration, or must connect to something before it can provide anything.

There is no third kind. **Global** is a property of a module: set `IsGlobal = true` on the module,
whether you built it yourself or a factory returned it. **`Register`** is only a function name:
`config.Register(opts)` and `cache.Register(opts)` are functions you call, and they return a
module. Owlkei Go never calls them.

### Factory signatures

A factory has one of these three shapes:

```go
func(deps...) owlkei.Module
func(deps...) (owlkei.Module, error)
func(ctx context.Context, deps...) (owlkei.Module, error)
```

- **`deps`:** any number of parameters. Each one is a provider type, `log.Logger` or
  `owlkei.Publisher`.
- **`ctx`:** only as the first parameter, and only with the `(owlkei.Module, error)` result. Its
  deadline is the startup timeout in `Imports`. In `AsyncImports` it has no deadline and is
  canceled at shutdown.
- **Anything else stops `Build()`.** A nil entry, a variadic function, `ctx` in second place or a
  `ctx` factory without an error result panic with every bad entry listed:
  `invalid module: can't import '<type>' with Imports: an import is a owlkei.Module or a module factory: …`.
- **Called once per app.** The same function listed by two modules is one factory, called once.
  Two closures of one function literal count as one factory too.
- **A failed factory stops `Create()`** when it is in `Imports`. The message names the factory:
  `module factory '<name>' failed: <err>`, `… panicked: <value>` (the stack is logged as
  `ModuleFactoryPanic`), `… returned a nil module`, or
  `… did not return within the startup timeout (30s)`.

### Where a factory finds its arguments

A factory listed by module `P` resolves each argument from, in this order:

1. The providers of the other entries of `P`'s `Imports` and `AsyncImports`: its siblings, and what
   they import.
2. The global providers: the root module's, and those of every `IsGlobal` module.
3. `log.Logger` and `owlkei.Publisher`.

`P`'s own providers are not in scope: `P` is built after its imports.

**Order doesn't matter.** Owlkei Go calls the factories in dependency order, whatever their position
in the list. A factory can come before the sibling it needs, and a factory can need what another
factory returns. When nothing can be called anymore, `Create()` stops:

- One factory left: `dependency injection: can't resolve argument of '<func>'. Please make sure that the argument '<type>' at index [<i>] is available in the injected providers`.
- Two or more: `dependency injection: module factories can't be ordered: '<a>' needs '<T>' → '<b>' needs '<U>'. Nobody provides these types, or the factories need each other`.

In this example, `NewDatabaseModule` needs the `ConfigService` of its sibling, which is listed after
it and is not global:

```go
package main

import (
	"context"
	"database/sql"
	"time"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/modules/config"
)

type Database struct {
	DB *sql.DB
}

func (instance Database) NewProvider() owlkei.Provider {
	return instance
}

func NewDatabaseModule(ctx context.Context, configService config.ConfigService) (owlkei.Module, error) {
	url, _ := configService.Get("DATABASE_URL").(string)
	// Import a driver, such as github.com/jackc/pgx/v5/stdlib, to register "pgx".
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	module := owlkei.ModuleBuilder().
		Providers(Database{DB: db}).
		Build()
	module.OnShutdown = func() {
		_ = db.Close()
	}
	return module, nil
}

type StatusController struct {
	owlkei.HTTP
	Database Database
}

func (instance StatusController) NewController() owlkei.Controller {
	return instance
}

func (instance StatusController) READ_status(c owlkei.HTTPContext) string {
	if err := instance.Database.DB.PingContext(c.Context()); err != nil {
		return "down"
	}
	return "up"
}

func main() {
	app := owlkei.New(&owlkei.AppOptions{StartupTimeout: 10 * time.Second})
	app.Create(
		owlkei.ModuleBuilder().
			Imports(
				NewDatabaseModule,
				config.Register(&config.ConfigModuleOptions{IsIgnoreEnvFile: true}),
			).
			Controllers(StatusController{}).
			Build(),
	)
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

The factory is in `Imports`, so the app does not start without the database. Without a driver,
`Create()` stops with
`invalid module: module factory 'main.NewDatabaseModule' failed: sql: unknown driver "pgx" (forgotten import?)`.

### Startup timeout

`AppOptions.StartupTimeout` sets how long `Create()` may take for the factories of `Imports`. The
default is 30 seconds.

- **One budget.** The timeout counts from the start of `Create()`, for all factories together.
- **The deadline is in `ctx`.** A `ctx` factory gets a context with that deadline. The context is
  canceled when `Create()` returns: don't keep it for later work.
- **A factory that ignores `ctx` is abandoned.** Owlkei Go calls every factory of `Imports` on a
  helper goroutine and stops waiting at the deadline, whatever the factory's shape. `Create()` then
  panics with `module factory '<name>' did not return within the startup timeout (10s)`.
- **Factories only.** `OnInit` and `NewProvider` are not covered: they take no `ctx`. Do slow work,
  such as connecting, in a factory.
- **Checked by `New`.** Zero means the default; a negative duration panics
  `invalid app: StartupTimeout is <d>, it must be positive`.

The timeout never applies to `AsyncImports`.

### Imports and AsyncImports

The app that imports a module decides how it starts, not the module's author. The same Module or
factory works in both lists:

- **`Imports` fails fast.** `Create()` waits for the entry. If it fails, the app does not start.
- **`AsyncImports` never stops the app.** `Create()` returns at once and the entry is built in the
  background. Until it is ready, only the routes that need it answer `503`.

An entry of `AsyncImports` brings its own imports with it: the modules that only it imports, and
their factories, are built in the background too.

This app serves `/health` at once. `/users-count` answers `503` until the database answers, then
switches on by itself:

```go
package main

import (
	"context"
	"database/sql"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/modules/config"
)

type Database struct {
	DB *sql.DB
}

func (instance Database) NewProvider() owlkei.Provider {
	return instance
}

func NewDatabaseModule(ctx context.Context, configService config.ConfigService) (owlkei.Module, error) {
	url, _ := configService.Get("DATABASE_URL").(string)
	// Import a driver, such as github.com/jackc/pgx/v5/stdlib, to register "pgx".
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}

	err = owlkei.Retry(ctx, nil, func(ctx context.Context) error {
		return db.PingContext(ctx)
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	module := owlkei.ModuleBuilder().
		Providers(Database{DB: db}).
		Build()
	module.OnShutdown = func() {
		_ = db.Close()
	}
	return module, nil
}

type UserController struct {
	owlkei.HTTP
	Database Database
}

func (instance UserController) NewController() owlkei.Controller {
	return instance
}

func (instance UserController) READ_users_count(c owlkei.HTTPContext) owlkei.Map {
	var count int
	err := instance.Database.DB.QueryRowContext(c.Context(), "SELECT count(*) FROM users").Scan(&count)
	if err != nil {
		panic(exception.ServiceUnavailableException("Service Unavailable", exception.ExceptionOptions{Cause: err}))
	}
	return owlkei.Map{"count": count}
}

type HealthController struct {
	owlkei.HTTP
}

func (instance HealthController) NewController() owlkei.Controller {
	return instance
}

func (instance HealthController) READ_health() string {
	return "ok"
}

var UserModule = owlkei.ModuleBuilder().
	AsyncImports(NewDatabaseModule).
	Controllers(UserController{}).
	Build()

var AppModule = owlkei.ModuleBuilder().
	Imports(
		config.Register(&config.ConfigModuleOptions{IsGlobal: true, IsIgnoreEnvFile: true}),
		UserModule,
	).
	Controllers(HealthController{}).
	Build()

func main() {
	app := owlkei.New(nil)
	app.Create(AppModule)
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

- `config.Register` stays in `Imports`: it reads the environment when you call it, so there is
  nothing to wait for, and a bad `.env` file should stop the boot.
- The factory finds `ConfigService` because the config module is global.
- `UserController` sits in `UserModule`, a static module that imports the factory. The factory's
  module has providers only (see [Refusals](#refusals)).
- `HealthController` injects nothing from the database, so it is never gated.

A complete app that you can run is in [`examples/04-async-modules`](examples/04-async-modules/README.md).

### Timeline

```
Create()
  ├─ check every module, factory and route name, also those of modules that are not ready
  ├─ call the factories of Imports, in dependency order, within the startup timeout
  ├─ build the modules that are ready: OnInit, providers, controllers
  ├─ register every route and WebSocket event; those of modules that are not ready sit behind a closed gate
  ├─ start one goroutine per AsyncImports entry
  └─ OnReady of the ready modules
Create() returns; Listen serves at once

background, one goroutine per entry
  ├─ wait for the entries its factory needs
  ├─ call the factory, if any (ctx canceled at shutdown, no deadline)
  ├─ OnInit, then providers, then controllers of each module of the entry
  ├─ open the gate: the module is ready (log ModuleReady)
  ├─ OnReady of those modules
  └─ build every module that was waiting only for this entry, with the same steps
the goroutine ends

shutdown (SIGINT, SIGTERM, app.Stop())
  ├─ cancel the background ctx: Retry stops, no module changes state, no gate opens
  ├─ drain the running requests (ShutdownTimeout)
  ├─ wait for the background goroutines until ShutdownTimeout has passed since shutdown began;
  │    a goroutine still running is logged as ModuleShutdownTimeout and abandoned
  └─ OnShutdown of the ready modules, in the reverse of the order they became ready
```

- Owlkei Go wires one module at a time. Factories run in parallel, but a slow `OnInit` or
  `NewProvider` delays the other background modules: connect in the factory.
- A factory that returns after the cancel has its module dropped. Close what it opened when `ctx`
  is done.
- Once every entry has settled, no goroutine is left.

### Module states

Every module has one of three states:

| State | Meaning |
|---|---|
| `pending` | The module waits for an `AsyncImports` entry, or is one. |
| `ready` | The module is built and its routes are open. |
| `failed` | The module, or an entry it needs, failed. It stays closed until you fix the cause and restart. |

- **One way.** A state changes once: `pending` → `ready` or `pending` → `failed`. A `ready` module
  never goes back.
- **What depends on an entry.** A module needs an entry when one of its providers, controllers,
  bound layers or pipes injects a provider from that entry, or from a module that needs it.
  Importing alone is not a need: a root module that imports everything keeps serving its own routes.
- **Failure spreads to what needs it, and only to that.** When the database entry fails, the user
  module that injects it fails, and so does a booking module that injects the user service. A
  booking module that injects only the cache keeps working.
- **Unknown providers wait.** With an async factory in the app, a provider type that no known
  module provides doesn't stop `Create()`: its module is `pending`. It becomes `ready` when a
  factory returns a module that provides the type, and `failed` with `can't resolve <type>` when the
  last factory has returned without it. Without an async factory, `Create()` stops as usual.
- **Before ready.** `app.Get(provider)` returns `nil` for a provider that is not built yet.

The log shows the states:

| Line | Level | Fields | When |
|---|---|---|---|
| `ModulePending` | Info | `module`, `waiting_for` | At `Create()`, once per module that is not ready |
| `ModuleRetry` | Warn | `module`, `attempt`, `error`, `next` | Each failed `Retry` attempt |
| `ModuleReady` | Info | `module`, `after` | A module becomes ready in the background |
| `ModuleFailed` | Error | `module`, `after`, `cause`, `stack` for a panic | A module fails |
| `ModuleShutdownTimeout` | Warn | `module` | A background goroutine outlives the shutdown |

The `InstanceLoader` line at listen is printed only for modules that are ready then. With
`IsDevtool: true`, the snapshot's `modules` list shows each module's `state`, `waiting_for`,
`attempts`, `last_error` and `ready_after_ms`, read live. `last_error` may hold a driver's error
text, such as a host name: the devtool is for development only.

### What clients get

| Request | `pending` | `failed` |
|---|---|---|
| HTTP route | `503` with `{"code":503,"error":"Service Unavailable","message":"Service Unavailable"}` and `Retry-After` | The same `503`, without `Retry-After` |
| Page route (`RENDER_`, `SUBMIT_`) | `503` as HTML: `503.html`, then `5xx.html`, then the built-in page, whatever `Accept` says | The same, without `Retry-After` |
| WebSocket `subscribe` or `publish` | `{"type":"error","id":"<id>","message":{"code":503,"error":"Service Unavailable","message":"not ready: <topic>"}}`, nothing subscribed or published | The same |

- **`Retry-After`** is the whole seconds until the next `Retry` attempt of the entry, from 1 to 60.
  It is `5` when no attempt is scheduled, such as before the first attempt or for a factory that
  doesn't use `Retry`.
- **The pre-route stages and global middlewares run first.** The security headers, CORS, load shedding and the access log apply to the
  `503`, and a middleware that refuses wins. Module middlewares, guards, interceptors, exception
  filters and the handler don't run.
- **No cause reaches the client.** The answer never names the module, the dependency or the error.
- **Routes exist.** `404`, `405` and `Allow` treat a closed route like an open one. The WebSocket
  handshake is never gated.
- **Ready means served.** An open route runs exactly as if its module had been imported with
  `Imports`: same layers, same order.

### When hooks run

| Hook | Module ready at `Create()` | Module built in the background |
|---|---|---|
| `OnInit` | During `Create()`, before the module's providers. A panic stops `Create()`. | In the background, once the entries it needs are ready, before its providers. A panic fails the module. |
| `OnReady` | At the end of `Create()`, in init order. A panic stops `Create()`. | Right after its gate opens: a request may arrive before `OnReady` returns. A panic is logged as `OnReadyPanic`, and the module stays ready. |
| `OnShutdown` | At shutdown, in the reverse of the init order. | At shutdown, only if the module became ready, in the reverse of the order modules became ready. A `pending` or `failed` module gets none. |

A factory that fails must clean up before it returns: its module never gets `OnShutdown`.

### Refusals

These mistakes stop the boot, or fail the module, with a message that names it:

- **Controllers in an async factory's module.** A module that an async factory returns may hold
  providers only. Its entry fails with
  `module '<name>' declares controllers (<Type>, …) but comes from an async factory: put them in a static module that imports it`.
  Put the controllers in a static module that lists the factory in `AsyncImports`, as `UserModule`
  does above. A static Module in `AsyncImports` may have controllers: they are known at `Create()`.
- **One entry in both lists.** `Build()` panics with `invalid module: '<name>' is in both Imports and AsyncImports`,
  and `Create()` with `invalid module: '<name>' is imported with Imports by '<a>' and with AsyncImports by '<b>'`
  when two modules disagree.
- **`NewController` must not use injected providers.** In an app with `AsyncImports`, Owlkei Go may
  call `NewController` twice: once at `Create()` on a throwaway instance, to register and check the
  routes, and once to build the controller. This is always the case for the controllers of a module
  that is not ready at `Create()`, and can happen for other controllers that bind layers. In the
  first call, the providers that are not ready are zero values. A panic there stops `Create()` with
  `invalid module: NewController of '<Type>' failed before its module '<name>' is ready: <value>. NewController must not use injected providers`.
  Keep `NewController` to `Prefix`, `Path` and the `Bind…` calls, with no side effects. If the
  second call gives other routes, the module fails.
- **A factory in `Imports` that needs an async module.** `Create()` panics with
  `module factory '<name>' is in Imports and needs '<T>' of '<module>', which is not ready at Create: import it with AsyncImports`.
- **App-wide layers that inject an async provider.** A `BindGlobal…` layer
  can't wait: `Create()` panics with `app-wide layers can't depend on AsyncImports`.
- **Async modules that need each other.** `Create()` panics with
  `invalid module: async modules need each other: '<a>' → '<b>' → '<a>'`.

### Retry

`owlkei.Retry(ctx, opts, fn)` calls `fn` until it returns `nil`, waiting longer after each failure.
Use it inside a factory:

```go
err := owlkei.Retry(ctx, &owlkei.RetryOptions{InitialDelay: time.Second}, func(ctx context.Context) error {
	return db.PingContext(ctx)
})
```

| Field | Type | Default | Description |
|---|---|---|---|
| `InitialDelay` | `time.Duration` | 500 ms | The first wait. Zero or less means the default. |
| `MaxDelay` | `time.Duration` | 30 s | The longest wait. Zero or less means the default. |
| `MaxAttempts` | `int` | until `ctx` is done | The number of calls before it gives up. Zero or less means no limit. |

- **Backoff with jitter.** The wait before attempt `n+1` is `InitialDelay × 2^(n-1)`, capped at
  `MaxDelay`, then a random time between half of it and all of it. The jitter keeps many instances
  from reconnecting at the same moment.
- **It stops with `ctx`.** When `ctx` is done, `Retry` returns
  `owlkei: retry stopped after <n> attempts: …`. `errors.Is` finds both `ctx.Err()` and the last
  error. `fn` is not called when `ctx` is already done.
- **It gives up after `MaxAttempts`.** It returns `owlkei: retry gave up after <n> attempts: <last error>`.
- **`nil` options** mean the defaults. A nil `fn` panics `invalid retry: Retry needs a function`.
  A panic in `fn` is not recovered.
- **It reports to the module.** With the `ctx` that Owlkei Go gave an `AsyncImports` factory, each
  failed attempt logs `ModuleRetry`, counts in the devtool's `attempts` and `last_error`, and sets
  the `Retry-After` that clients get. With any other `ctx`, `Retry` only retries.

In an `Imports` factory, `Retry` stops at the startup timeout, because that is the deadline of its
`ctx`.

### Transient and permanent errors

Sort the errors of your factory in two groups:

- **Transient errors** (the database is starting, the network is down for a moment): retry
  without a limit. Pass `nil` options, or leave `MaxAttempts` at zero. The module recovers by
  itself when the dependency comes back, and the app needs no restart.
- **Permanent errors** (a bad URL, a missing setting, a refused password): return the error at
  once, without `Retry`. The module becomes `failed`, which needs a fix and a restart. Retrying
  would only fill the log.

The examples above follow this rule: `sql.Open` checks the configuration and returns its error at
once, and only `PingContext` is retried.

### Don't retry in a goroutine of your own

Owlkei Go calls a factory once. Two shortcuts look tempting, and both lose what the framework gives
you:

- **Returning an error and retrying later yourself.** The error makes the module `failed`, and
  `failed` is final: a later success can't reopen its routes.
- **Returning a module at once and connecting in a goroutine.** The module is `ready` before the
  connection exists, so its routes open and requests reach a client that isn't connected. There is
  no `503` with `Retry-After`, no state in the log or the devtool, no cancel at shutdown, and
  `OnShutdown` can run while the goroutine still connects.

Retry inside the factory with `owlkei.Retry` and the factory's `ctx`.

### After a module is ready

A `ready` module stays ready. When its database goes down later, Owlkei Go doesn't unwire the module
or close its routes: run-time failures are the business code's job. Let the driver's pool
reconnect, and answer the failed request with an exception, as `READ_users_count` does with
`exception.ServiceUnavailableException`.

### Compared with NestJS forRootAsync

NestJS's `forRootAsync` awaits its `useFactory` during the bootstrap: the app doesn't listen until
every async module has resolved, and a factory that rejects stops the bootstrap. An Owlkei Go factory
in `Imports` behaves the same way, with a deadline.

`AsyncImports` is different. The app listens at once, only the routes that need the module answer
`503` until it is ready, and a failure leaves the rest of the app running. In NestJS, "async"
describes how the module's author configures the module. In Owlkei Go, it describes how the app
imports it.

---

## Pages

Owlkei Go also serves finished HTML pages, rendered on the server with Go's `html/template`. A page
is a route like any other: it uses the same route words, modules, guards, interceptors, pipes and
filters as an API route, and the route picks the template. One binary carries its templates and
assets, and nothing extra runs while the server is idle.

- **The URL is the file.** `RENDER_bookings` answers `GET /bookings` with
  `web/bookings/index.html`. There is no template name to keep in sync.
- **Escaped by default.** Every value is escaped for where it appears: text, attribute, URL or
  script.
- **Errors in the right format.** A page route answers errors as an HTML page, chosen by status.
  An API route answers JSON.
- **Strict CSP without work.** With `Headers` on, an inline `<script nonce="{{nonce}}">` runs and every
  other inline script is blocked.
- **Forms without tokens.** `CSRF: &owlkei.CSRFOptions{IsEnabled: true}` refuses cross-origin form posts, and a `Form` pipe binds
  and checks the fields.
- **Mistakes stop the boot.** A page without a template, a template without a route, or a
  stylesheet that belongs to no page stops `Create()`.

### Render a page

Turn on rendering with `Render: &owlkei.RenderOptions{FS: web}` in `AppOptions`. Then,
in a controller that embeds `owlkei.HTTP`, name a handler with one of the two page operations:

- **`RENDER_<route>`** answers `GET` (and `HEAD`) on the path that `READ_<route>` would have. It
  renders the template of that path, and the handler's result is the template's data.
- **`SUBMIT_<route>`** answers `POST` on the same path, for the form of that page. It renders the
  same template, with status `422`, unless the handler redirects.

API routes and page routes can share one controller.

This booking site lists bookings, shows one, and takes new ones through a form. It is a shorter
version of [`examples/03-pages`](examples/03-pages/README.md). The files:

```
main.go
web/layout.html            the layout of every page
web/layout.css             the layout's stylesheet, linked by {{pageAssets}}
web/4xx.html               client errors, such as 404
web/5xx.html               server errors
web/bookings/index.html    GET /bookings
web/bookings/{id}.html     GET /bookings/{id}
web/bookings/new.html      GET and POST /bookings/new
web/bookings/new.css       the stylesheet of bookings/new.html
web/partials/field.html    a partial
web/assets/app.css         /assets/app.css?v=<hash>
web/public/robots.txt      /robots.txt
```

`main.go`:

```go
package main

import (
	"embed"
	"os"
	"strconv"
	"strings"
	"sync"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
)

//go:embed web
var web embed.FS

type Booking struct {
	ID     string
	Guest  string
	Nights string
}

type BookingTable struct {
	mu   sync.Mutex
	rows []Booking
}

type BookingService struct {
	Table *BookingTable
}

func (instance BookingService) NewProvider() owlkei.Provider {
	instance.Table = &BookingTable{}
	return instance
}

func (instance BookingService) All() []Booking {
	instance.Table.mu.Lock()
	defer instance.Table.mu.Unlock()
	return append([]Booking(nil), instance.Table.rows...)
}

func (instance BookingService) Find(id string) (Booking, bool) {
	for _, booking := range instance.All() {
		if booking.ID == id {
			return booking, true
		}
	}
	return Booking{}, false
}

func (instance BookingService) Add(guest, nights string) {
	instance.Table.mu.Lock()
	defer instance.Table.mu.Unlock()
	id := strconv.Itoa(len(instance.Table.rows) + 1)
	instance.Table.rows = append(instance.Table.rows, Booking{ID: id, Guest: guest, Nights: nights})
}

type BookingForm struct {
	Guest  string `bind:"guest"`
	Nights string `bind:"nights"`
	Errors map[string]string
}

func (instance BookingForm) Transform(form owlkei.Form, _ owlkei.ArgumentMetadata) any {
	bound, fields := form.Bind(instance)
	dto := bound.(BookingForm)
	dto.Errors = map[string]string{}
	for _, field := range fields {
		if value, _ := field.Value().(string); !field.IsValue() || strings.TrimSpace(value) == "" {
			dto.Errors[field.Tag()] = "Fill in this field"
		}
	}
	return dto
}

type BookingPages struct {
	owlkei.HTTP
	Bookings BookingService
}

func (instance BookingPages) NewController() owlkei.Controller {
	return instance
}

func (instance BookingPages) RENDER_bookings() owlkei.Map {
	return owlkei.Map{"Bookings": instance.Bookings.All()}
}

func (instance BookingPages) RENDER_bookings_BY_id(param owlkei.Param) Booking {
	booking, ok := instance.Bookings.Find(param.Get("id"))
	if !ok {
		panic(exception.NotFoundException("No booking has this number"))
	}
	return booking
}

func (instance BookingPages) RENDER_new_OF_bookings() BookingForm {
	return BookingForm{Nights: "1"}
}

func (instance BookingPages) SUBMIT_new_OF_bookings(c owlkei.HTTPContext, form BookingForm) BookingForm {
	if len(form.Errors) == 0 {
		instance.Bookings.Add(form.Guest, form.Nights)
		c.Redirect("/bookings")
	}
	return form
}

func main() {
	app := owlkei.New(&owlkei.AppOptions{
		Headers: &owlkei.HeadersOptions{IsEnabled: true},
		CSRF:    &owlkei.CSRFOptions{IsEnabled: true},
		// The web directory of the embedded files; PAGES_DEV=1 reads it from disk and reloads it.
		Render: &owlkei.RenderOptions{FS: web, IsReload: os.Getenv("PAGES_DEV") == "1"},
	})
	app.Create(
		owlkei.ModuleBuilder().
			Controllers(BookingPages{}).
			Providers(BookingService{}).
			Build(),
	)
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`web/layout.html` wraps every page. `{{template "content" .}}` places the page, the `title`
block is the default that a page can replace, and `{{pageAssets}}` links the CSS and JS files
named after the layout and the page:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>{{block "title" .}}Bookings{{end}} · Hotel</title>
  <link rel="stylesheet" href="{{asset "app.css"}}">
  {{pageAssets}}
  <script nonce="{{nonce}}">
    document.documentElement.dataset.theme = "light";
  </script>
</head>
<body>
  <main>
{{template "content" .}}
  </main>
</body>
</html>
```

`web/bookings/index.html` is the page of `RENDER_bookings`. Its data is the handler's result:

```html
<h1>Bookings</h1>
<ul>
  {{range .Bookings}}<li><a href="/bookings/{{.ID}}">{{.Guest}}</a>, {{.Nights}} nights</li>
  {{else}}<li>No bookings yet.</li>{{end}}
</ul>
<p><a href="/bookings/new">New booking</a></p>
```

`web/bookings/{id}.html` is the page of `RENDER_bookings_BY_id`. It replaces the layout's title:

```html
{{define "title"}}Booking #{{.ID}}{{end}}
<h1>Booking #{{.ID}}</h1>
<p>{{.Guest}} stays {{.Nights}} nights.</p>
```

`web/bookings/new.html` is the page of `RENDER_new_OF_bookings` and `SUBMIT_new_OF_bookings`. The
form posts to its own URL, and calls the partial `partials/field` once per field, with `dict`:

```html
{{define "title"}}New booking{{end}}
<h1>New booking</h1>
<form method="post" action="/bookings/new">
  {{template "partials/field" dict "Name" "guest" "Label" "Guest name" "Value" .Guest "Error" .Errors.guest}}
  {{template "partials/field" dict "Name" "nights" "Label" "Nights" "Value" .Nights "Error" .Errors.nights}}
  <button type="submit">Book</button>
</form>
```

`web/partials/field.html`:

```html
<p>
  <label for="{{.Name}}">{{.Label}}</label>
  <input id="{{.Name}}" name="{{.Name}}" value="{{.Value}}"{{if .Error}} aria-invalid="true"{{end}}>
  {{with .Error}}<span>{{.}}</span>{{end}}
</p>
```

`web/4xx.html` renders the client errors of a page route inside the layout, and `web/5xx.html`
the server errors, with the same content:

```html
{{define "title"}}{{.Status}}{{end}}
<h1>{{.Code}} {{.Status}}</h1>
<p>{{.Message}}</p>
{{with .RequestID}}<p>Request ID: {{.}}</p>{{end}}
```

`web/assets/app.css`, `web/layout.css` and `web/bookings/new.css`:

```css
body { font-family: system-ui, sans-serif; }
```

```css
main { max-width: 40rem; margin: 2rem auto; }
```

```css
form p { display: grid; gap: 0.25rem; }
```

`web/public/robots.txt`:

```
User-agent: *
Allow: /
```

Run it with `go run .`, then open the pages in a browser:

- `GET /bookings/new` answers the form with `200`, as `text/html; charset=utf-8`. Its `<head>`
  links `/assets/_pages/layout.css?v=<hash>`, then `/assets/_pages/bookings/new.css?v=<hash>`.
- `POST /bookings/new` with an empty field answers `422` with the form again: the message next to
  the field and the submitted values kept, escaped (`"><script>` arrives as
  `value="&#34;&gt;&lt;script&gt;"`).
- `POST /bookings/new` with both fields answers `303 See Other` with `Location: /bookings`.
- `GET /bookings/9` answers `404` with `web/4xx.html` in the layout: "No booking has this
  number".
- `GET /assets/app.css?v=<hash>` answers the stylesheet with
  `Cache-Control: public, max-age=31536000, immutable`.
- `GET /robots.txt` answers `web/public/robots.txt` with `Cache-Control: no-cache` and an `ETag`.

### File conventions

`RenderOptions.FS` holds the templates and the assets, usually the app's `embed.FS`
(`//go:embed web`). Owlkei Go reads the directory `RenderOptions.Dir` inside it: `web` when `Dir` is empty,
the root of `FS` with `Dir: "."`. A `FS` without that directory stops `New` with
`invalid render: RenderOptions.FS has no "web" directory: embed it with //go:embed web, or set Dir`.
With `IsReload` on an `embed.FS`, Owlkei Go reads the same directory from disk instead (relative to the
working directory), so edits show without a rebuild; `Create()` logs a `Warn` `RenderReloadOn`, and a
missing directory on disk stops it. Where a file is decides what it is:

| Path in `Dir` | Role |
|---|---|
| `layout.html` in any directory | The layout of the pages in that directory and below it. The nearest one wins |
| `partials/**/*.html` (the top-level `partials` directory only) | A partial, included with `{{template "partials/field" .}}` (its path without `.html`) |
| `400.html` to `599.html`, `4xx.html`, `5xx.html` (at the root) | Error pages. See [Error pages](#error-pages) |
| `assets/**` | Static files at `/assets/…` with hashed URLs, never parsed. See [Assets](#assets) |
| `public/**` | Static files at the root URL under their own names, never parsed. See [Public files](#public-files) |
| `X.css`, `X.<name>.css`, `X.js`, `X.<name>.js` next to `X.html` | The CSS and JS of that page, layout or error page. See [Page CSS and JS](#page-css-and-js) |
| Every other `**/*.html` | The page of the route with that path |
| Any other file, and any path with a segment that starts with `.` | Ignored, never served |

The template of a page route is its final path, after the module and controller prefixes, the
path style and `Path`: `<path>.html` or `<path>/index.html`. A parameter is written `{name}` in
the file or directory name, with the route's parameter name:

| Route | Template |
|---|---|
| `RENDER` | `index.html` |
| `RENDER_bookings` | `bookings.html` or `bookings/index.html` |
| `RENDER_bookings_BY_id` | `bookings/{id}.html` or `bookings/{id}/index.html` |
| `RENDER_new_OF_bookings` | `bookings/new.html` or `bookings/new/index.html` |
| `RENDER_projects_BY_project_id_OF_orgs_BY_org_id` | `orgs/{org_id}/projects/{project_id}.html` or `orgs/{org_id}/projects/{project_id}/index.html` |
| `RENDER_reports_BY_year_AND_month` | `reports/{year}/{month}.html` or `reports/{year}/{month}/index.html` |
| `RENDER_users` in a module with `Prefix("admin")` | `admin/users.html` or `admin/users/index.html` |
| `RENDER_court_bookings` | `court-bookings.html` or `court-bookings/index.html` (`court_bookings…` with `PathStyleAsWritten`) |
| A last segment `index` or `layout` (`RENDER_layout_OF_courts`) | `courts/layout/index.html` only |

- **Page:** the whole file is the page's content. It needs no `{{define}}`.
- **Layout:** places the page with `{{template "content" .}}`. A page replaces a layout's
  `{{block "title" .}}…{{end}}` with its own `{{define "title"}}…{{end}}`. Layouts don't nest, and
  a page without a layout is rendered alone. A parameter directory can hold one too:
  `orgs/{org_id}/layout.html` wraps every page below it, such as `orgs/{org_id}/projects/{project_id}.html`,
  while `orgs/{org_id}.html` (in `orgs/`) keeps the root layout.
- **Data:** the handler's only or last result is `.` in the page and in its layout, of any type: a
  `owlkei.Map`, a struct, a slice or `nil`. A handler with no result renders with `nil`. A partial
  gets what the caller passes.

`Create()` reads and parses every layout, page, partial and error page once, and binds each page
route to its template. A parse error, a call of a template that doesn't exist, or an escaping
error stops the boot with the file's path, so a request never finds one:

```
invalid render: bookings/new.html: template: bookings/new.html:4: function "dcit" not defined
```

### Template functions

Templates have four functions, and you can't add your own. Format values with methods on your
data instead, such as `{{.Date.Format "2 Jan 2006"}}`.

| Function | Writes | Notes |
|---|---|---|
| `{{nonce}}` | The response's CSP nonce: 32 lowercase hexadecimal characters from `crypto/rand` | The same value at every use in one response and in its `Content-Security-Policy` header. See [Content Security Policy](#content-security-policy) |
| `{{asset "app.css"}}` | `/assets/app.css?v=<16 hex>`, where the hash is the first 8 bytes of the file's SHA-256 | With `IsReload`, `/assets/app.css` with no `?v=`. A file that is not in `assets/` fails the render |
| `{{dict "Name" "email" "Value" .Email}}` | A `map[string]any` | Passes several values to a partial. An odd number of arguments or a key that is not a string fails the render |
| `{{pageAssets}}` | The `<link>` and `<script>` tags of the page's CSS and JS files | See [Page CSS and JS](#page-css-and-js) |

### Status codes and redirects

| Situation | Status |
|---|---|
| A `RENDER_` handler returns | `200` |
| A `SUBMIT_` handler returns without a redirect | `422`: the form is shown again with its errors |
| The handler calls `c.Status(code)`, or returns `(int, data)` | That code |
| The handler calls `c.Redirect(url)`, or the `owlkei.Redirect` parameter, without a 3xx status | `303 See Other`, so the browser follows it with `GET` |
| The handler set a 3xx status with `c.Status` before the redirect | That status |

- A page answers with `Content-Type: text/html; charset=utf-8`, a `Content-Length` and
  `Cache-Control: no-cache`, unless a layer or the handler set `Cache-Control`.
- After a redirect, or any response the handler wrote itself, the result is ignored.
- `HEAD` on a page answers its status and headers with no body. `PUT` on a page's path answers
  `405` with `Allow: GET, HEAD, POST`.
- With `Versioning` on, a page route answers every version and requests without one.
- On an API route, `c.Redirect` still answers `301`, and `CREATE_` still answers `201`.

Each mistake stops `Create()`:

| Mistake | Panic |
|---|---|
| A page route without `Render.FS` | `invalid render: BookingPages.RENDER_bookings is a page route, set Render.FS in AppOptions` |
| A page route without its template | `invalid render: BookingPages.RENDER_bookings needs a template: bookings.html or bookings/index.html` |
| A page route with both templates | `invalid render: BookingPages.RENDER_bookings has two templates: bookings.html and bookings/index.html` |
| A page route below `partials/`, `assets/` or `public/` | `invalid render: BookingPages.RENDER_x_OF_partials can have no template: partials/ is reserved` |
| A page route at the name of an error page (`RENDER_404`) | `invalid render: BookingPages.RENDER_404 resolves to 404.html, which is an error page, not a route` |
| An `.html` file that no route uses, such as `error.html` or a misspelled `bookings/detial.html` | `invalid render: bookings/detial.html has no RENDER_ route: add one, move it to partials/, or delete it` |
| A `SUBMIT_` without its `RENDER_` in the same controller | `invalid handler: BookingPages.SUBMIT_x needs BookingPages.RENDER_x: a SUBMIT_ posts to the page that shows its form` |
| `Path` or a targeted `Prefix` on a `SUBMIT_` | `invalid route: Path("/y") is bound to 'SUBMIT_x': a SUBMIT_ uses the path of its RENDER_` |
| `ANY`, `VERSION` or `FILE` in a page route | `invalid route: ANY in RENDER_files_ANY: a page route has no wildcard` |
| A page route whose path holds a `*` from `Path` or a prefix | `invalid route: BookingPages.RENDER_docs is a page route and its path /docs/* holds a wildcard` |
| `Render.Dir` or `Render.IsReload` without `Render.FS` | `invalid app: Render.Dir is set without Render.FS` |

A `SUBMIT_` posts to the page that shows its form, so `Path` and prefixes on the `RENDER_` move
both. An edit page and a delete button on one page share its `SUBMIT_`: tell them apart by the
`name` of the submit button.

A render that fails at run time answers the fixed `500` error page and logs one `Error` record
`RenderFailed`, with `template` and `cause`. No byte of the failed page is sent. It happens for a
template that fails while it runs (a field that the data's type doesn't have, a failed `dict` or
`asset`) or a page larger than 8 MiB. With `nil` data, `{{.X}}` renders nothing.

### Error pages

Where an error happened decides its format:

| Where | Format |
|---|---|
| A page route (`RENDER_`, `SUBMIT_`), from its first guard on: a guard that refuses (`403`), an exception or a panic in a guard, pipe, interceptor, handler or filter, and the `429` of `guards/throttler` | HTML |
| An API route, a `SERVE_…` route, the asset route or a public file, from its first guard on | JSON |
| Before any guard: a refusal of a pre-route stage (`csrf` `403`, `hostcheck` `421`, `loadshed` `503`) or of your own middleware, and no route (`404`, `405` with its `Allow` header, `410`) | HTML when a value of the request's `Accept` header contains `text/html`, else JSON. The response carries `Vary: Accept` |

- Browsers send `Accept: text/html,…`, so they get HTML. `*/*` alone, `application/xhtml+xml` alone,
  or no `Accept` header gets JSON. Owlkei Go doesn't read q-values.
- An HTML error with the code `C` renders the first file that exists at the root of `Dir`:
  `<C>.html`, then `4xx.html` or `5xx.html`, then a built-in page. It is rendered in the root
  `layout.html`, with `Content-Type: text/html; charset=utf-8` and the error's status. Headers that
  layers set, such as `Retry-After` and `Allow`, are kept.
- The built-in page is a complete HTML document with the code, the status text and the message,
  and no style, script or external resource, so it passes any CSP.
- When the chosen file or its layout fails to render, Owlkei Go sends the built-in page with the same
  code and logs `RenderFailed`. It doesn't try the next file. An error page is never empty and
  never half a page.
- Your exception filters run first on a page route, and can answer by themselves.
- Without `Render.FS`, every error is JSON, as in the rest of this README.

| Files at the root of `Dir` | 404 | 422 | 500 | 503 |
|---|---|---|---|---|
| none | built-in | built-in | built-in | built-in |
| `404.html` | `404.html` | built-in | built-in | built-in |
| `404.html`, `4xx.html`, `5xx.html` | `404.html` | `4xx.html` | `5xx.html` | `5xx.html` |
| `503.html`, `5xx.html` | built-in | built-in | `5xx.html` | `503.html` |

- Error page names are reserved at the root only: `admin/404.html` is the page of a route.
- A root file named like a status that is not an error stops `Create()`:
  `invalid render: 302.html is not an error page: use 400.html to 599.html, 4xx.html or 5xx.html`.
  So do `200.html`, `3xx.html` and `600.html`.
- `error.html` has no role. It stops `Create()` like any page without a route.

An error page gets this data:

| Key | Type | Example |
|---|---|---|
| `.Code` | `int` | `404` |
| `.Status` | `string` | `Not Found` |
| `.Message` | `string` | `No booking has this number` |
| `.RequestID` | `string` | The `X-Request-Id` of the response, for a user to quote |

The client sees the same three values in both formats, `code`, `error` and `message` in JSON, and
`.Code`, `.Status` and `.Message` in HTML:

- **No internal detail.** A cause's text never reaches the client: an exception with a cause sends
  the status text in `.Status` and logs the cause at `Debug` (`ExceptionCause`).
- **A fixed 500.** A `500` exception, a code outside 400 to 599, any other panic and a failed render
  all send `Internal Server Error` as the status and the message. The detail goes to the log.
- **Our own words.** A body or form over the size limit answers `413` `Request body too large`, and
  a broken file upload `400` `Invalid file upload`.

See [Exceptions](#exceptions) and [core › Error response](internal/core/README.md#error-response).

### Forms

A form is a pipe whose `Transform` takes `owlkei.Form`, as `BookingForm` above:

1. The page's form posts to its own URL: `<form method="post" action="/bookings/new">` reaches
   `SUBMIT_new_OF_bookings`.
2. `form.Bind(instance)` copies the fields by their `bind` tags (`application/x-www-form-urlencoded`
   and `multipart/form-data`, the same as on an API route) and returns one `owlkei.FieldLevel` per
   field.
3. `Transform` checks the fields and keeps its messages in an exported field of the DTO, such as
   `Errors map[string]string`.
4. When the DTO holds errors, the `SUBMIT_` handler returns it: the page is rendered again with
   `422`, the messages next to their fields and the submitted values kept, escaped.
5. When the form is valid, the handler saves it and calls `c.Redirect`, which answers `303`. The
   browser loads the next page with `GET`, so a reload never posts the form twice.

- A pipe's fields are exported. Owlkei Go injects providers into them, and an unexported field stops
  `Create()`: `dependency injection: can't set value to unexported 'nights' field of the main.BookingForm pipe`.
  Keep computed values out of the pipe, or compute them again where you need them.
- A malformed form body (`400`) or one over `HTTP.MaxBodyBytes` (`413`) answers the
  HTML error page.
- Set `CSRF: &owlkei.CSRFOptions{IsEnabled: true}`. It needs no token and no hidden field: it reads
  the `Sec-Fetch-Site` and `Origin` headers that browsers send. A cross-origin form post gets a
  `403` error page. See [Configuration](#configuration).
- A layer bound to `RENDER_x` only doesn't run for `SUBMIT_x`. Bind a guard to both handlers, or
  to the whole controller with `BindGuard(guard)`.

### Content Security Policy

The default policy of `Headers` allows no inline script or style. Give an inline `<script>` or `<style>`
the attribute `nonce="{{nonce}}"`, and it runs: on a page that calls `{{nonce}}`, the `headers` stage adds the
same value to `script-src` and `style-src`.

```
Content-Security-Policy: default-src 'self'; script-src 'self' 'nonce-<N>'; style-src 'self' 'nonce-<N>'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests
```

- Every response gets its own nonce, never derived from the request. Pages default to
  `Cache-Control: no-cache`, so a shared cache doesn't replay one.
- API routes, assets, public files and pages that don't call `{{nonce}}` draw no nonce and send
  the policy unchanged.
- The files that `{{pageAssets}}` links are same-origin files, allowed by `'self'`: they need no
  nonce.
- A custom policy gets the nonce in the directives that allow it; one with `'unsafe-inline'` or
  `'none'` is left as it is. See [Configuration](#configuration).
- Without `Headers`, `{{nonce}}` still writes a nonce and no policy is sent.
- `html/template` removes JavaScript comments from inline scripts. Write comments about a script in
  a template comment (`{{/* … */}}`) next to it.

### Assets

When `Dir` has an `assets` directory, Owlkei Go serves it at `GET /assets/*` with a built-in route.
Link a file with `{{asset "app.css"}}`: Owlkei Go hashes every asset once at `Create()`, so the URL
changes when the file does.

| Request | `Cache-Control` |
|---|---|
| `?v=` equals the file's hash | `public, max-age=31536000, immutable` |
| No `v`, or another value | `no-cache` |
| Any, with `IsReload` | `no-cache`, and no `ETag` |

- Without `IsReload`, every file carries `ETag: "<16 hex>"`, and `If-None-Match` with it answers
  `304`.
- The content type comes from the file's extension.
- A directory, a missing file, or a path with `..` or a segment that starts with `.` (except
  `.well-known`) answers `404` as JSON. Templates are not reachable through this route.
- Global middlewares and global guards run for the asset route. A route of your own at `/assets/*`
  stops `Create()` with `route conflict:`. Without an `assets` directory and without page CSS or
  JS, Owlkei Go adds no route.
- `assets/_pages` is reserved for page CSS and JS: a file or directory with that name stops
  `Create()`.

### Public files

Files that need a fixed URL at the root of the site, such as `robots.txt`, `favicon.ico` or
`.well-known/security.txt`, go in `Dir/public`. Each one is served by `GET` and `HEAD` at its path
below `public`: `public/robots.txt` answers `/robots.txt`.

- A response has the content type of the extension, `Cache-Control: no-cache` and
  `ETag: "<16 hex>"`, and `If-None-Match` with it answers `304`.
- Files are sent as they are: an `.html` file there is never parsed. A directory has no index, so
  `public/docs/index.html` answers `/docs/index.html` only.
- A file whose path matches a route stops `Create()`:
  `route conflict: public/robots.txt method is conflicted with READ_robots_txt_FILE method`.
- A segment that starts with `.` is never served, except `.well-known`. `//go:embed web` leaves
  out names that start with `.` or `_`. To ship `.well-known`, write `//go:embed all:web`, which
  also embeds every other dotfile under `web` in the binary. Owlkei Go never serves those.
- The list of files is fixed at `Create()`. With `IsReload`, a file is read on each request, with
  no `ETag`, and a file added later answers `404` until the restart.
- Errors are JSON. Global middlewares and global guards run for these routes, which appear in the
  startup route log as `public/<path>`.
- Each file is one route. Keep `public` to the few files that need a fixed URL, and put the rest in
  `assets`.

### Page CSS and JS

A stylesheet or script used by one page goes next to its template: `X.css`, `X.<name>.css`, `X.js`
or `X.<name>.js` next to `X.html` belongs to that page, layout or error page. The longest `X` wins.
`{{pageAssets}}`, usually in the layout's `<head>`, writes their tags: the layout's files, then the
page's, each group sorted by file name. For `/bookings/new`, with `web/layout.css`,
`web/bookings/new.css` and `web/bookings/new.form.js`:

```html
<link rel="stylesheet" href="/assets/_pages/layout.css?v=<hash>">
<link rel="stylesheet" href="/assets/_pages/bookings/new.css?v=<hash>">
<script src="/assets/_pages/bookings/new.form.js?v=<hash>" defer></script>
```

- The files are served at `/assets/_pages/<path>` with the caching of [Assets](#assets). Only these
  files are served there: a template or a partial answers `404`.
- Path segments are percent-encoded in the tags: `bookings/{id}.css` is
  `/assets/_pages/bookings/%7Bid%7D.css`.
- A page without CSS or JS gets nothing from `{{pageAssets}}`.
- A CSS or JS file that belongs to no template stops `Create()`:
  `invalid render: bookings/nw.css belongs to no template: name it after a page or layout of its directory (X.css, X.<name>.css), or move it to assets/`.
  So does one under `partials/`.
- A page that has such files, while none of its templates calls `{{pageAssets}}`, stops `Create()`:
  `invalid render: bookings/new.html has bookings/new.css, but no template of the page calls {{pageAssets}}: add it to layout.html`.
- The URLs show the template tree (`/assets/_pages/admin/…`), and these files are served without
  the page's guards. Keep secrets out of them.

### Develop with reload

With `IsReload: true`, each render checks the names, sizes and modification times of the
templates and the page CSS and JS files, and parses them again after a change. Assets and public
files are read from `FS` on each request. Point `FS` at the disk, as `main.go` does with
`PAGES_DEV=1`, and run it from the directory that holds `web`.

- No goroutine or file watcher runs: the check happens during a render.
- A reload binds every route again, so `bookings.html` moved to `bookings/index.html` keeps
  answering. A new `.html` file without a route is ignored until the restart that adds its
  handler.
- A file that fails to parse, or a route whose template is now missing, answers the fixed `500`
  page and logs `RenderReloadFailed` with `file` and `cause`, until you fix it. The error text is
  never in the response.
- Turn `IsReload` off in production: each render then walks the files, and assets are not cached.

### Cost

- **Without `Render.FS`:** nothing changes for API routes.
- **At `Create()`:** every template is parsed and escaped once, and every asset, public file and
  page CSS or JS file is hashed once.
- **Per page:** one lookup in a table, one pooled buffer, the template's execution and one write.
  A layout and a 40-row list take about 37 µs and 246 allocations, nearly all of them in
  `html/template` (Intel Core i7-9750H, Go 1.27.4, `BenchmarkRender_Nonce` in `internal/render`).
  Two `{{nonce}}` calls add about 2 µs and 14 allocations.
- **Idle:** no goroutine, timer or watcher, in production and with `IsReload`. Each public file is
  one route in memory.

See [core › Pages](internal/core/README.md#pages) and the full [`examples/03-pages`](examples/03-pages/README.md),
built with Owlkei's framework-free HTML components.

---

## WebSocket

Turn on the WebSocket server with `WS: &owlkei.WSOptions{Path: "ws"}` in `AppOptions`; an empty
`Path` leaves it off. Then write a controller that embeds `owlkei.WS`, with one
`SUBSCRIBE_…` method per event:

```go
package main

import "go.owlkei.com/owlkei"

type ChatMessage struct {
	Text string `bind:"text"`
}

type ChatController struct {
	owlkei.WS
}

func (instance ChatController) NewController() owlkei.Controller {
	return instance
}

func (instance ChatController) SUBSCRIBE_chat_ANY(payload owlkei.WSPayload) owlkei.Map {
	message := payload.BindValue(ChatMessage{}).(ChatMessage)
	return owlkei.Map{"received": message.Text}
}

func main() {
	app := owlkei.New(&owlkei.AppOptions{WS: &owlkei.WSOptions{Path: "ws"}})
	app.Create(owlkei.ModuleBuilder().Controllers(ChatController{}).Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

Event names come from the method name: the words after `SUBSCRIBE`, lower-cased and joined with
`.`. `ANY` is a wildcard `*` for one segment, and `ALL`, as the last word, a wildcard `**` for one
or more segments:

| Method | Event | Handles | Doesn't handle |
|---|---|---|---|
| `SUBSCRIBE_chat_message` | `chat.message` | `chat.message` | `chat.room1` |
| `SUBSCRIBE_chat_ANY` | `chat.*` | `chat.room1`, `chat.room2` | `chat.room1.typing`, `chat` |
| `SUBSCRIBE_chat_ANY_typing` | `chat.*.typing` | `chat.room1.typing` | `chat.room1` |
| `SUBSCRIBE_chat_ALL` | `chat.**` | `chat.room1`, `chat.room1.typing` | `chat` |

- An upper-case word that is not `ANY` or `ALL` stops `Create()`, as in route names: write
  `SUBSCRIBE_chat_v2`, not `SUBSCRIBE_chat_V2`. `PathStyle` doesn't apply to events.
- `ALL` anywhere but last stops `Create()`:
  `invalid route: ALL in SUBSCRIBE_chat_ALL_x must be the last word`.

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

Ask for a `owlkei.Publisher` in any handler, HTTP or WebSocket, to send a message to every
subscriber of a topic:

```go
package main

import "go.owlkei.com/owlkei"

type NewsController struct {
	owlkei.HTTP
}

func (instance NewsController) NewController() owlkei.Controller {
	return instance
}

func (instance NewsController) CREATE_news(body owlkei.Body, publisher owlkei.Publisher) string {
	if err := publisher.Publish("chat.news", owlkei.Map{"headline": body.Get("headline")}); err != nil {
		panic(err)
	}
	return "sent"
}

type ChatController struct {
	owlkei.WS
}

func (instance ChatController) NewController() owlkei.Controller {
	return instance
}

func (instance ChatController) SUBSCRIBE_chat_ANY(payload owlkei.WSPayload) owlkei.WSPayload {
	return payload
}

func main() {
	app := owlkei.New(&owlkei.AppOptions{WS: &owlkei.WSOptions{Path: "ws"}})
	app.Create(owlkei.ModuleBuilder().Controllers(NewsController{}, ChatController{}).Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`POST /news` with `{"headline":"Go 1.27"}` sends
`{"type":"event","topic":["chat.news"],"message":{"headline":"Go 1.27"}}` to every connection
subscribed to `chat.news`.

### Layers on WebSocket

- Guards, interceptors and exception filters work on WebSocket events: write their methods with
  `owlkei.WSContext` and bind them in the controller or globally.
- The handshake runs the load shedding, proxy and host stages that `AppOptions` turns on, then the
  Origin check: an `Origin` that is neither the same origin nor in `Origins` gets `403`. Then the
  middlewares bound with `BindGlobalMiddlewares` run, once per connection, and one that doesn't
  call `next()` refuses the connection. A controller's `BindMiddleware` doesn't run on the
  handshake.

---

## Versioning

Versioning lets one path answer differently per API version. Enable it before `Create`, then add a
`VERSION` token to each handler name:

```go
package main

import "go.owlkei.com/owlkei"

type UserController struct {
	owlkei.HTTP
}

func (instance UserController) NewController() owlkei.Controller {
	return instance
}

func (instance UserController) READ_users_VERSION_1() []string {
	return []string{"alice"}
}

func (instance UserController) READ_users_VERSION_2() owlkei.Map {
	return owlkei.Map{"users": []string{"alice"}, "total": 1}
}

func (instance UserController) READ_health_VERSION_NEUTRAL() string {
	return "ok"
}

func main() {
	app := owlkei.New(&owlkei.AppOptions{Versioning: &owlkei.VersioningOptions{
		Type:           owlkei.VersioningHeader,
		Key:            "X-Api-Version",
		DefaultVersion: "1",
	}})
	app.Create(owlkei.ModuleBuilder().Controllers(UserController{}).Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /users` answers `["alice"]`, and `GET /users` with `X-Api-Version: 2` answers
`{"total":1,"users":["alice"]}`. `GET /health` answers for any version.

| `Type` | Where the version is read | Example with the default `Key` (`v`) |
|---|---|---|
| `owlkei.VersioningQuery` | A query parameter | `GET /users?v=2` |
| `owlkei.VersioningHeader` | A request header | `v: 2` |
| `owlkei.VersioningMediaType` | A parameter of the `Accept` header | `Accept: application/json;v=2` |
| `owlkei.VersioningCustom` | Your `Extractor` function, `func(owlkei.HTTPContext) string` | |

- `DefaultVersion` is the version of a request that carries none.
- `VERSION_NEUTRAL` marks a route that answers for every version, and for requests without one.
- Once versioning is on, give every handler a `VERSION` token. A route without one no longer
  answers: requests get `404`, or `410 Gone` for a request without a version when
  `DefaultVersion` is empty.

See [versioning](internal/versioning/README.md).

---

## Static files

A `SERVE_…` handler returns the directory to serve. `SERVE_ANY_OF_assets` serves the route
`/assets/*`:

```go
package main

import "go.owlkei.com/owlkei"

type AssetController struct {
	owlkei.HTTP
}

func (instance AssetController) NewController() owlkei.Controller {
	return instance
}

func (instance AssetController) SERVE_ANY_OF_assets(c owlkei.HTTPContext) string {
	c.ResponseWriter.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	return "public/assets"
}

func main() {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(AssetController{}).Build())
	if err := app.Listen(); err != nil {
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

With `Render.FS` set, the `assets` directory of the templates' `FS` is served at `/assets/*` without
a handler, with hashed URLs and immutable caching, and the files of its `public` directory at the
root URL, such as `/robots.txt`. See [Pages › Assets](#assets) and
[Pages › Public files](#public-files).

See [core › Static files](internal/core/README.md#static-files).

---

## Built-in modules

### Config

The config module reads the environment and `.env` files into one `ConfigService`, with type
binding, variable expansion and validation hooks. It never prints a value. See the
[Config module documentation](modules/config/README.md).

```go
package main

import (
	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/modules/config"
)

type DatabaseService struct {
	ConfigService config.ConfigService
	Host          string
}

func (instance DatabaseService) NewProvider() owlkei.Provider {
	instance.Host, _ = instance.ConfigService.Get("DB_HOST").(string)
	return instance
}

type InfoController struct {
	owlkei.HTTP
	DatabaseService DatabaseService
}

func (instance InfoController) NewController() owlkei.Controller {
	return instance
}

func (instance InfoController) READ_info() string {
	return instance.DatabaseService.Host
}

func main() {
	app := owlkei.New(nil)
	app.Create(
		owlkei.ModuleBuilder().
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
	if err := app.Listen(); err != nil {
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
| `EnvFilePaths` | `[]string` | The `.env` files to read. The default is `.env` in the directory of the nearest `go.mod`. |
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

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/modules/cache"
)

type ProfileController struct {
	owlkei.HTTP
	Cache cache.CacheService
}

func (instance ProfileController) NewController() owlkei.Controller {
	return instance
}

func (instance ProfileController) READ_profiles_BY_id(c owlkei.HTTPContext) string {
	key := "profile:" + c.Param().Get("id")
	if data, ok := instance.Cache.Get(c.Context(), key); ok {
		return string(data)
	}

	profile := "profile of " + c.Param().Get("id")
	_ = instance.Cache.Set(c.Context(), key, []byte(profile), 5*time.Minute)
	return profile
}

func main() {
	app := owlkei.New(nil)
	app.Create(
		owlkei.ModuleBuilder().
			Imports(cache.Register(&cache.CacheModuleOptions{IsGlobal: true})).
			Controllers(ProfileController{}).
			Build(),
	)
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

**`CacheModuleOptions`:**

| Field | Type | Description |
|---|---|---|
| `IsGlobal` | `bool` | Makes `CacheService` available to every module. |
| `OnInit` | `cache.CacheOnInitFn` | Runs once when the app that imports the module is created. |
| `Backend` | `cache.Cache` | The store. The default is `cache.NewMemoryCache(nil)`. |

### Outbound HTTP calls

Owlkei Go ships no HTTP client module. Call other services with `net/http`, and wrap the transport
with `owlkei.TraceTransport` so the outgoing request continues the incoming request's trace
(`Traceparent`). See the [trace documentation](internal/trace/README.md#outbound-calls).

```go
package main

import (
	"net/http"
	"time"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/log"
)

var client = &http.Client{Transport: owlkei.TraceTransport(nil), Timeout: 10 * time.Second}

type AccountController struct {
	owlkei.HTTP
	Logger log.Logger
}

func (instance AccountController) NewController() owlkei.Controller {
	return instance
}

func (instance AccountController) READ_profile(c owlkei.HTTPContext) int {
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
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(AccountController{}).Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

The error is logged here with its context. An exception's cause never reaches the client either:
the framework logs it (`ExceptionCause`, at `Debug`) and sends only the status text in `error`.

---

## Configuration

The whole configuration of an app is one struct, `owlkei.AppOptions`, passed to `owlkei.New`. Code is
bound with the `Bind…` methods, and `app.Listen()` takes no argument.

- `New` reads the struct once and copies what it keeps. Changing it after `New` has no effect.
- Every check happens in `New`. A bad value panics with `invalid app: <Group>.<Field> …` before
  the app exists.
- An empty field means "off" or the default in the tables below. No field changes the meaning of
  another.
- **Nothing protects the app until you configure it.** Load shedding, the proxy headers, the host
  check, the HTTPS redirect, the security headers, CORS, CSRF and compression are all off by
  default. Copy the [production checklist](#production-checklist) and remove what you don't need.

### Start here

An app with no options is a complete app for development:

```go
package main

import "go.owlkei.com/owlkei"

func main() {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`New(nil)` and `New(&owlkei.AppOptions{})` give the same app:

| What | Value |
|---|---|
| Port | 3000, plain HTTP/1.1 |
| Request body | 10 MiB at most |
| Server timeouts | Fixed ([core › Timeouts and limits](internal/core/README.md#timeouts-and-limits)) |
| Graceful shutdown | 30 seconds for running requests |
| Startup timeout | 30 seconds |
| Logger | `log.NewLog(nil)` |
| Access log, devtool | Off |
| Pages, WebSocket, versioning | Off |
| Built-in protections | Off |

### Production checklist

One block to copy. Every protection an app on the internet needs is in it. The full app is
[`examples/05-production`](examples/05-production/README.md).

```go
package main

import (
	"time"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/guards/throttler"
	"go.owlkei.com/owlkei/log"
)

type HelloController struct {
	owlkei.HTTP
}

func (instance HelloController) NewController() owlkei.Controller {
	return instance
}

func (instance HelloController) READ_hello() string {
	return "hello"
}

func main() {
	app := owlkei.New(&owlkei.AppOptions{
		Logger: log.NewLog(&log.LogOptions{
			Format:     log.FormatJSON,
			MaskFields: []string{"password", "token"},
		}),
		IsAccessLog:   true,
		Server:        &owlkei.ServerOptions{Port: 8080},
		HTTP:          &owlkei.HTTPOptions{MaxBodyBytes: 1 * owlkei.MiB},
		Proxy:         &owlkei.ProxyOptions{TrustedProxies: []string{"10.0.0.0/8"}},
		Hosts:         []string{"example.com"},
		Origins:       []string{"https://app.example.com"},
		LoadShed:      &owlkei.LoadShedOptions{IsEnabled: true},
		HTTPSRedirect: &owlkei.HTTPSRedirectOptions{IsEnabled: true, Host: "example.com"},
		Headers:       &owlkei.HeadersOptions{IsEnabled: true},
		CORS:          &owlkei.CORSOptions{IsEnabled: true, IsAllowCredentials: true},
		CSRF:          &owlkei.CSRFOptions{IsEnabled: true},
		Compress:      &owlkei.CompressOptions{IsEnabled: true},
	})

	app.BindGlobalGuards(throttler.Throttler{Limit: 300, TTL: time.Minute})

	app.Create(
		owlkei.ModuleBuilder().
			Controllers(HelloController{}).
			Build(),
	)

	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

Behind a load balancer at `10.0.0.7`, a request for `https://example.com/hello` from
`198.51.100.20` answers `hello` with the security and rate limit headers, and logs one access line
with `"ip":"198.51.100.20"`. The same request over plain HTTP answers
`308 Location: https://example.com/hello`.

Go through the list once for your app:

1. **`Logger`**: JSON logs, with the fields to mask ([log](log/README.md)). Log through
   `Logger.From(c.Context())` in handlers and services, so every line carries the request's
   `trace_id` and `request` ID.
2. **`IsAccessLog`**: one line per request.
3. **`HTTP.MaxBodyBytes`**: the largest body your app really accepts.
4. **`Proxy`**: set it when the app runs behind a load balancer. Leave it out when clients connect
   directly.
5. **`Hosts`**: the host names your app serves.
6. **`LoadShed`**: on.
7. **`HTTPSRedirect`**: on, unless your proxy already redirects HTTP to HTTPS. Set `Host`.
8. **`Headers`**: on for every app that serves HTML pages.
9. **`Origins` and `CORS`**: only when browser pages on another origin call your API.
10. **`CSRF`**: on when browsers send cookies or HTTP authentication to your app (sessions, HTML
    forms).
11. **`Compress`**: on, unless your proxy already compresses responses.
12. **A rate limit**: bind the [`throttler`](guards/throttler/README.md) guard globally with a
    budget your real users never reach, and a tighter one on routes such as logins and password
    resets.

**The `SecurityCheck` warning.** At the end of `Create`, the app logs at most one `Warn` line,
`SecurityCheck`, with a `missing` list and a link to this section:

- `csrf`: the app has a `SUBMIT_` route and `CSRF` is not set.
- `headers`: the app has a `RENDER_` route and `Headers` is not set.

The warning changes no response and turns nothing on. Set `IsEnabled: true` on the group to turn
the protection on, or `IsDisabled: true` to turn it off on purpose. Both silence the warning. An
app without pages logs nothing.

### The built-in stages and their order

The eight protections are stages of the framework. They run before routing and before your
middlewares, always in this order:

`LoadShed` → `Proxy` → `Hosts` → `HTTPSRedirect` → `Headers` → `CORS` → `CSRF` → `Compress` →
your `BindGlobalMiddlewares` → routing → the [request lifecycle](#request-lifecycle)

- A stage that is off doesn't run and costs nothing.
- The order of the fields in `AppOptions` and of your `Bind…` calls never changes this order.
- A request refused by a stage gets the stage's answer, also on a path with no route: a wrong
  host answers `421`, never `404`.
- A WebSocket handshake runs `LoadShed`, `Proxy`, `Hosts`, then the
  [Origin check](#websocket-options), then your `BindGlobalMiddlewares`.
- With `IsAccessLog`, each stage that ran appears in the access line under `pre-route`.

### Who is the client: Proxy, Hosts, Origins

These three fields say where requests come from. The other stages rely on them.

**`Proxy`** trusts the forwarding headers of your load balancer. Without it, the client is the
peer of the TCP connection, which is your proxy when you have one. The stage is on when
`TrustedProxies` or `TrustedHops` is set. Set one of them, not both.

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `TrustedProxies` | empty | `OK_PROXY__TRUSTED_PROXIES` | You know the IPs or CIDRs of your proxies (`10.0.0.0/8`). |
| `TrustedHops` | `0` | `OK_PROXY__TRUSTED_HOPS` | Your platform doesn't publish its ranges: the number of proxies in front of the app, 1 to 16. |
| `Header` | `ProxyHeaderXForwarded` | `OK_PROXY__HEADER` (`x-forwarded`, `forwarded`) | Your proxy sends the standard `Forwarded` header instead of `X-Forwarded-*`. |
| `IsHostForwarded` | `false` | `OK_PROXY__IS_HOST_FORWARDED` | Your proxy rewrites `Host` and sends the client's host in the forwarding header. |

Every later stage, your middlewares, the throttler and the access log then see the real client.
Read it with `owlkei.ClientIP(r)`, `owlkei.ClientScheme(r)` and `owlkei.ClientHost(r)`.

**`Hosts`** lists the host names the app answers. A request for another host gets `421`.

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `Hosts` | empty: every host | `OK_HOSTS` | In production, always. At most 256 entries. `localhost` is not added for you. |

**`Origins`** is one list of browser origins, used by `CORS`, `CSRF` and the WebSocket handshake.

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `Origins` | empty: same origin only | `OK_ORIGINS` | Pages on another origin call your app: `https://app.example.com`, `https://*.example.com`, or `*` alone. |

- An entry is `scheme://host[:port]` in lower case, without a path. At most 256 entries.
- `*` can't be combined with `CORS.IsAllowCredentials` or with `CSRF`.
- `Origins` without `CORS`, `CSRF` or `WS` stops `New`: the list would have no effect.

### HTTP

`HTTP` holds what applies to every request, also when the `App` runs in your own `http.Server`.

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `MaxBodyBytes` | `10MiB` | `OK_HTTP__MAX_BODY_BYTES` | Lower it to the largest body you accept; raise it for uploads. A larger body answers `413`. A negative value removes the limit. |
| `ServeDirs` | empty | `OK_HTTP__SERVE_DIRS` | Reserved: the directories a `SERVE_` handler may return. It has no effect yet. |

`MaxBodyBytes` is a `ByteSize`. In code, write `16 * owlkei.KiB` or `2 * owlkei.MB`. In a variable,
write bytes (`1500`) or a whole number with a unit: `KB`, `MB`, `GB` are powers of 1000, and
`KiB`, `MiB`, `GiB` powers of 1024. Units are case-sensitive, without a space or a fraction:
`10Mb`, `10 MB` and `1.5MB` are refused.

The stages below also belong to HTTP requests. Each is on with `IsEnabled: true`. A field set
while `IsEnabled` is false stops `New`.

**`LoadShed`** refuses requests with `503` when the app has more work than it can do.

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `IsEnabled` | `false` | `OK_LOAD_SHED__IS_ENABLED` | In production. |
| `MaxInFlight` | `0`: adapts to the machine | `OK_LOAD_SHED__MAX_IN_FLIGHT` | You measured how many concurrent requests the app handles and want a fixed limit. |
| `MaxQueue` | `64` | `OK_LOAD_SHED__MAX_QUEUE` | How many requests wait up to 100 ms for a slot. A negative value: none wait. |

**`HTTPSRedirect`** answers plain HTTP requests with `308` to HTTPS.

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `IsEnabled` | `false` | `OK_HTTPS_REDIRECT__IS_ENABLED` | The app is reachable over plain HTTP and your proxy doesn't redirect. |
| `Host` | the request's host | `OK_HTTPS_REDIRECT__HOST` | Always, or set `Hosts`: the target then never comes from the client's `Host` header. |
| `Port` | `443` | `OK_HTTPS_REDIRECT__PORT` | HTTPS listens on another port. |

**`Headers`** sends the security headers.

| Field | Default | Variable |
|---|---|---|
| `IsEnabled` | `false` | `OK_HEADERS__IS_ENABLED` |
| `IsDisabled` | `false` | `OK_HEADERS__IS_DISABLED` |
| `ContentSecurityPolicy` | `default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests` | `OK_HEADERS__CONTENT_SECURITY_POLICY` |
| `CrossOriginEmbedderPolicy` | not sent | `OK_HEADERS__CROSS_ORIGIN_EMBEDDER_POLICY` |
| `CrossOriginOpenerPolicy` | `same-origin` | `OK_HEADERS__CROSS_ORIGIN_OPENER_POLICY` |
| `CrossOriginResourcePolicy` | `same-origin` | `OK_HEADERS__CROSS_ORIGIN_RESOURCE_POLICY` |
| `FrameOptions` | `DENY` | `OK_HEADERS__FRAME_OPTIONS` |
| `PermissionsPolicy` | every powerful feature off (camera, geolocation, microphone, payment…) | `OK_HEADERS__PERMISSIONS_POLICY` |
| `ReferrerPolicy` | `no-referrer` | `OK_HEADERS__REFERRER_POLICY` |
| `StrictTransportSecurity` | `max-age=63072000; includeSubDomains`, over HTTPS only | `OK_HEADERS__STRICT_TRANSPORT_SECURITY` |

- Turn it on for every app with pages. Change a header when a page needs more: a script from a
  CDN in `ContentSecurityPolicy`, `SAMEORIGIN` in `FrameOptions` to frame your own pages.
- For each header, an empty value sends the default, `"-"` sends nothing, and any other value is
  sent as written.
- `IsDisabled: true` says "off on purpose" and silences the `SecurityCheck` warning.
- Inline scripts and styles need a nonce. See
  [Pages › Content Security Policy](#content-security-policy).

**`CORS`** answers cross-origin browser requests from the origins in `Origins`.

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `IsEnabled` | `false` | `OK_CORS__IS_ENABLED` | Browser pages on another origin call your API. It needs `Origins`. |
| `AllowMethods` | `GET, HEAD, PUT, PATCH, POST, DELETE` | `OK_CORS__ALLOW_METHODS` | You want to allow fewer methods. |
| `AllowHeaders` | `Content-Type, Authorization` | `OK_CORS__ALLOW_HEADERS` | Pages send another request header. |
| `ExposeHeaders` | none | `OK_CORS__EXPOSE_HEADERS` | Pages read a response header such as `X-Total-Count`. |
| `MaxAge` | `10m` | `OK_CORS__MAX_AGE` | How long browsers cache a preflight. |
| `IsAllowCredentials` | `false` | `OK_CORS__IS_ALLOW_CREDENTIALS` | Pages send cookies or HTTP authentication. |
| `IsAllowPrivateNetwork` | `false` | `OK_CORS__IS_ALLOW_PRIVATE_NETWORK` | A public page calls an app on a private network. |

**`CSRF`** refuses cross-site `POST`, `PUT`, `PATCH` and `DELETE` requests whose origin is not in
`Origins`. It needs no token and no hidden field: it reads the `Sec-Fetch-Site` and `Origin`
headers that browsers send.

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `IsEnabled` | `false` | `OK_CSRF__IS_ENABLED` | Browsers send cookies or HTTP authentication to your app. |
| `IsDisabled` | `false` | `OK_CSRF__IS_DISABLED` | Off on purpose: silences the `SecurityCheck` warning. |

**`Compress`** gzips compressible responses.

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `IsEnabled` | `false` | `OK_COMPRESS__IS_ENABLED` | Your proxy doesn't compress responses. |
| `MinSize` | `1KiB` | `OK_COMPRESS__MIN_SIZE` | The smallest body worth compressing (a `ByteSize`). |

Don't compress routes that put a secret next to text from the request in one response (BREACH).

### Server

`Server` applies when Owlkei Go opens the listener (`Listen`, `Serve`). It has no effect when you
mount the `App` in your own `http.Server`. See [core › Server](internal/core/README.md#server).

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `Port` | `3000` | `OK_SERVER__PORT`, else `PORT` | Your platform gives the port. `Serve(listener)` ignores it. |
| `ShutdownTimeout` | `30s` | `OK_SERVER__SHUTDOWN_TIMEOUT` | Your platform kills the process sooner after `SIGTERM`: stay below its grace period. |
| `IsH2C` | `false` | `OK_SERVER__IS_H2C` | A proxy speaks HTTP/2 without TLS to the app. Not with `TLS`. |
| `TLS.CertFile` | none | `OK_SERVER__TLS__CERT_FILE` | The app terminates TLS itself: the PEM certificate chain. |
| `TLS.KeyFile` | none | `OK_SERVER__TLS__KEY_FILE` | The PEM private key of `CertFile`. Set both or neither. |
| `TLS.Config` | TLS 1.2 minimum | code only | You need your own `*tls.Config`, such as `GetCertificate` for automatic certificates. |

- With a certificate, `Listen()` serves HTTPS with HTTP/1.1 and HTTP/2.
- The files are loaded at `New`: a wrong path stops the app at start, not at the first handshake.
- The server timeouts are fixed and are not options
  ([core › Timeouts and limits](internal/core/README.md#timeouts-and-limits)).

### Pages (Render)

`Render` turns on server-rendered pages when `FS` is set. See [Pages](#pages).

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `FS` | none: pages off | code only | The app has `RENDER_` routes: an `embed.FS` or `os.DirFS` with your templates. |
| `Dir` | `web` | `OK_RENDER__DIR` | The templates are in another directory of `FS` (`.` for its root). |
| `IsReload` | `false` | `OK_RENDER__IS_RELOAD` | In development only: read the templates from disk and parse them again when a file changes. |

`Dir` or `IsReload` without `FS` stops `New`. An app with pages should also set `Headers` and
`CSRF`.

### WebSocket options

`WS` turns on the WebSocket endpoint when `Path` is set. See [WebSocket](#websocket).

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `Path` | none: WebSocket off | `OK_WS__PATH` | The app has `SUBSCRIBE_` handlers: the path of the endpoint, such as `ws`. |

The handshake always checks the `Origin` header. This check has no switch:

- No `Origin` header (a client that is not a browser) passes.
- The same origin as the request passes.
- An origin in `Origins` passes.
- Any other origin gets `403`, before the upgrade. This stops another site from opening a
  connection with your user's cookies.

### Versioning options

`Versioning` turns on route versioning when `Type` is set. See [Versioning](#versioning).

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `Type` | none: versioning off | `OK_VERSIONING__TYPE` (`query`, `header`, `media-type`, `custom`) | The app has `_VERSION_` routes: where the request carries its version. |
| `Key` | `v` | `OK_VERSIONING__KEY` | Another query parameter, header or media type parameter name. |
| `DefaultVersion` | empty | `OK_VERSIONING__DEFAULT_VERSION` | The version of a request that carries none. |
| `Extractor` | none | code only | With `VersioningCustom`: your function that reads the version. |

A field set without `Type` stops `New`.

### Operations

| Field | Default | Variable | When to change it |
|---|---|---|---|
| `Logger` | `log.NewLog(nil)` | code only | You want JSON, masked fields or your own `log.LogAdapter` ([log](log/README.md)). |
| `IsAccessLog` | `false` | `OK_IS_ACCESS_LOG` | In production: one line per request and WebSocket message, with the time of each stage ([accesslog](internal/accesslog/README.md)). |
| `StartupTimeout` | `30s` | `OK_STARTUP_TIMEOUT` | Module factories need more or less time ([Startup timeout](#startup-timeout)). |
| `IsDevtool` | `false` | `OK_IS_DEVTOOL` | In development: a snapshot of the routes, layers and stages for development tools. |

The built-in logger writes JSON when stdout is not a terminal, which is the case in a container.

### From the environment

`owlkei.OptionsFromEnv()` returns the same `*AppOptions`, filled from environment variables and
from a `.env` file. Without a call to it, Owlkei Go reads no variable and no file.

```go
package main

import (
	"embed"

	"go.owlkei.com/owlkei"
)

//go:embed all:web
var web embed.FS

func main() {
	opts := owlkei.OptionsFromEnv()
	opts.Render.FS = web
	opts.IsDevtool = false
	app := owlkei.New(opts)
	app.Create(owlkei.ModuleBuilder().Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

**Names.** A variable is `OK_`, then the field path in upper snake case, with `__` between
levels:

| Field | Variable |
|---|---|
| `Origins` | `OK_ORIGINS` |
| `IsAccessLog` | `OK_IS_ACCESS_LOG` |
| `HTTP.MaxBodyBytes` | `OK_HTTP__MAX_BODY_BYTES` |
| `Server.TLS.CertFile` | `OK_SERVER__TLS__CERT_FILE` |
| `HTTPSRedirect.Port` | `OK_HTTPS_REDIRECT__PORT` |
| `Server.Port` | `OK_SERVER__PORT`, else `PORT` |

`PORT` is the one name without `OK_`. It fills `Server.Port` when `OK_SERVER__PORT` is not set.
No other variable is read. Four fields are set in code only: `Logger`, `Render.FS`,
`Server.TLS.Config` and `Versioning.Extractor`.

**Values.**

| Type | Written as |
|---|---|
| Text, number | `example.com`, `8080` |
| Boolean | `true` or `false` only |
| Duration | A number with a unit: `30s`, `10m`. `30` is refused. |
| `ByteSize` | Bytes, or a whole number with `KB`, `MB`, `GB` (1000ⁿ) or `KiB`, `MiB`, `GiB` (1024ⁿ) |
| List | Elements joined by `,`: `a.com,b.com`. Spaces around an element are removed. |
| `Proxy.Header` | `x-forwarded` or `forwarded` |
| `Versioning.Type` | `query`, `header`, `media-type` or `custom` |

**Precedence.** From strongest to weakest:

1. Code written after the `OptionsFromEnv()` call, an explicit `false`, `0` or `""` included.
2. The process environment.
3. The file `.env` in the working directory. A missing file is skipped. Only its `OK_*` and `PORT`
   keys are read; the other keys are left to your app and to the [Config](#config) module.

`OptionsFromEnv` returns every group non-nil, so `opts.Render.FS = web` needs no check.

**Strict errors.** `OptionsFromEnv` never panics. `New` stops with every error at once, one per
line:

```text
invalid app: 3 environment errors:
  OK_HOST (environment): unknown variable; did you mean OK_HOSTS?
  OK_HTTP__MAX_BODY_BYTES (.env:3): "10Mb" is not a size: write bytes or a number with KB, MB, GB (1000ⁿ) or KiB, MiB, GiB (1024ⁿ); a lower-case b means bits
  OK_ORIGINS (environment): empty value; remove the variable to unset it
```

- An unknown `OK_*` name stops the app, with the closest known name. A typo never leaves a
  protection off in silence.
- An empty value stops the app. Remove the variable to unset it.
- A value that doesn't convert stops the app. One bad element refuses the whole list.
- A `.env` syntax error stops the app, with its line number and never the value.
- A value of the right type and wrong meaning (a bad CIDR, a missing TLS file) fails the check of
  `New`, which names the field and the variable it came from.

**Sources in the startup log.** With `OptionsFromEnv`, `New` logs one `AppOption` line per field
that is set, with its value and where it came from:

| `source` | Meaning |
|---|---|
| `OK_HOSTS` | The process environment |
| `.env:3` | Line 3 of `.env` |
| `PORT` | The `PORT` variable of the process environment |
| `code` | Set or changed in code after `OptionsFromEnv()` |

Code-only fields print `set` instead of their value.

**`.env` and `.env.example`.** Keep `.env` out of version control and commit an example instead:

```gitignore
.env
```

```sh
# .env.example: copy to .env and fill in
PORT=8080
OK_IS_ACCESS_LOG=true
OK_HTTP__MAX_BODY_BYTES=1MiB
OK_PROXY__TRUSTED_PROXIES=10.0.0.0/8
OK_HOSTS=example.com
OK_ORIGINS=https://app.example.com
OK_LOAD_SHED__IS_ENABLED=true
OK_HTTPS_REDIRECT__IS_ENABLED=true
OK_HTTPS_REDIRECT__HOST=example.com
OK_HEADERS__IS_ENABLED=true
OK_CORS__IS_ENABLED=true
OK_CORS__IS_ALLOW_CREDENTIALS=true
OK_CSRF__IS_ENABLED=true
OK_COMPRESS__IS_ENABLED=true
```

This file gives the same protections as the [production checklist](#production-checklist); the
logger stays in code. `OptionsFromEnv` reads `.env` from the working directory, and the
[Config](#config) module from the directory of the nearest `go.mod`. They are the same file when
you run `go run .` from the module root.

### Behavior changes

For apps written before `AppOptions`. The old API is removed, with no alias.

| Before | Now |
|---|---|
| `owlkei.New()` | `owlkei.New(nil)` or `owlkei.New(&owlkei.AppOptions{…})` |
| `SetLogger`, `SetStartupTimeout`, `EnableAccessLog`, `EnableDevtool` | `Logger`, `StartupTimeout`, `IsAccessLog`, `IsDevtool` |
| `EnableRender`, `EnableVersioning`, `EnableWS` | `Render`, `Versioning`, `WS` |
| `Listen(port, opts)`, `ListenTLS(port, cert, key, opts)` | `Server.Port`, `Server.TLS` and `Listen()` |
| `Serve(listener, opts)` | `Serve(listener)` |
| `ServerOptions.MaxRequestBodyBytes` | `HTTP.MaxBodyBytes`, a `ByteSize` |
| The `middlewares/*` packages bound with `BindGlobalMiddlewares` | The matching `AppOptions` field. The packages are no longer public. |
| An origin list for CORS and another for CSRF | One `Origins` list |
| `proxy.ClientIP`, `proxy.Scheme`, `proxy.Host` | `owlkei.ClientIP`, `owlkei.ClientScheme`, `owlkei.ClientHost` |

What behaves differently:

- **The built-in stages always run before your middlewares, in a fixed order.** The order of your
  `Bind…` calls no longer matters. A middleware of yours that ran before a built-in one now runs
  after it:
  - It no longer runs for a request refused under overload.
  - It reads the client resolved by `Proxy`, not the peer.
  - Its own refusals carry the security headers.
  - `Compress` also compresses what it writes.
- **The stages run before routing.** A request refused by a stage gets the stage's answer on an
  unknown path (`421`, not `404`) and on a route of a module that is not ready (not the `503` of
  [AsyncImports](#imports-and-asyncimports)).
- **Every stage is off by default.** An app that bound no built-in middleware behaves as before.
  Nothing is turned on for you.
- **The `SecurityCheck` warning** is logged once at `Create` when an app has `SUBMIT_` routes
  without `CSRF`, or `RENDER_` routes without `Headers`. It changes no response.
- **`Origins` also guards the WebSocket handshake.** A browser handshake from another site gets
  `403` unless its origin is listed. The handshake runs the stages its fields turn on, not a list
  of middlewares passed to `EnableWS`; your `BindGlobalMiddlewares` run on it, once per
  connection. CORS no longer runs on the handshake.
- **Wildcard origins work for CSRF.** `https://*.example.com` in `Origins` passes `CSRF` as it
  passes `CORS`.
- **Configuration errors come from `New`.** They read `invalid app: <Group>.<Field> …` in place
  of `invalid cors: …`, `invalid proxy: …` and the others. TLS files are loaded at `New`.
- **Sizes are `ByteSize` values.** `HTTP.MaxBodyBytes` and `Compress.MinSize` take
  `10 * owlkei.MiB` in code. A plain number is still bytes. In a variable, `KB`, `MB` and `GB` are
  powers of 1000, `KiB`, `MiB` and `GiB` powers of 1024, and a lower-case `b` (`10Mb`) is refused,
  because it means bits.

### Security notes
- `c.Redirect` sends the client to any URL: never pass a URL from the request unchecked
  ([ctx › Redirect](internal/ctx/README.md#redirect)).
- `c.JSON` and `c.Text` insert format arguments unescaped: pass request data in a map or struct
  instead ([ctx › JSON](internal/ctx/README.md#json)).
- `owlkei.TraceTransport` sends your trace ID and request ID to every host you call
  ([trace › Outbound calls](internal/trace/README.md#outbound-calls)).
- Change response headers with `Header().Set`, `Add` or `Del`, never by writing into a value slice
  in place ([core › Response writer](internal/core/README.md#response-writer)).
- A guard bound to a `RENDER_` handler doesn't cover its `SUBMIT_`: bind it to both, or to the
  controller ([Pages › Forms](#forms)).
- Turn `CSRF` on in every app with `SUBMIT_` routes or cookies: Owlkei Go doesn't add it for you.
- Files in `public/` are sent as they are, at the root URL, with no escaping and no nonce: never
  write user content there ([Pages › Public files](#public-files)).
- Page CSS and JS URLs show your template tree and are served without the page's guards
  ([Pages › Page CSS and JS](#page-css-and-js)).

---

## Exceptions

An exception carries a status code, a message for the client and an error text. Panic with one
from a guard, a pipe, a handler or an interceptor:

```go
package main

import (
	"database/sql"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
)

type InvoiceController struct {
	owlkei.HTTP
}

func (instance InvoiceController) NewController() owlkei.Controller {
	return instance
}

func (instance InvoiceController) READ_invoices() string {
	panic(exception.NotFoundException("Invoice not found", exception.ExceptionOptions{
		Description: "no invoice with this id",
		Cause:       sql.ErrNoRows,
	}))
}

func main() {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(InvoiceController{}).Build())
	if err := app.Listen(); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
```

`GET /invoices` answers `404` with
`{"code":404,"error":"Not Found","message":"Invoice not found"}` and logs the cause at `Debug`
(`ExceptionCause`).

- **Message:** the first argument, the `message` field. Write it for the client.
- **Error text:** the `error` field. It is the code's status text by default, a `string` option,
  or `ExceptionOptions.Description`. With a cause (an `error` option or `ExceptionOptions.Cause`)
  the client gets the status text and the cause is logged; `errors.Is` and `errors.As` find it.
- **Custom codes:** `exception.NewException("Too many attempts", 429)` builds an exception with
  any code. A code of 500 or outside 400 to 599 answers the fixed
  `{"code":500,"error":"Internal Server Error","message":"Internal Server Error"}` and logs the
  exception's message and cause (`InternalServerError`).
- **HTML pages:** on a page route the same three values fill the error page (`.Code`,
  `.Status`, `.Message`). See [Pages › Error pages](#error-pages).

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

## Package reference

An application imports `go.owlkei.com/owlkei`, plus the packages it uses among
[`exception`](exception/README.md), [`log`](log/README.md), `modules/*` and
`guards/*`. Every other package of the module is under `internal/` and can't be imported.

### Functions

| Function | Description |
|---|---|
| `New() App` | Creates an application. |
| `ModuleBuilder() *ModuleBuilder` | Starts a module: chain `Imports(...)`, `AsyncImports(...)`, `Providers(...)`, `Controllers(...)` and `PathStyle(style)`, then `Build()`. `Imports` and `AsyncImports` take Modules and module factories. See [Module lifecycle](#module-lifecycle). |
| `Retry(ctx context.Context, opts *RetryOptions, fn func(ctx context.Context) error) error` | Calls `fn` until it returns `nil`, with backoff and jitter, until `ctx` is done or `MaxAttempts` is reached. With a factory's `ctx`, the attempts show in the log, the devtool and `Retry-After`. See [Retry](#retry). |
| `TraceTransport(base http.RoundTripper) http.RoundTripper` | Wraps a transport so outgoing requests continue the request's trace. `nil` wraps `http.DefaultTransport`. See [Outbound HTTP calls](#outbound-http-calls). |

### Application and modules

| Type | Description |
|---|---|
| `App` | The application returned by `New(opts)`; its configuration is the `AppOptions` given to `New`. Bind app-wide layers with `BindGlobalMiddlewares`, `BindGlobalGuards`, `BindGlobalInterceptors` and `BindGlobalExceptionFilters`; wire it with `Create(Module)`; serve it with `Listen()` or `Serve(listener)`; stop it with `Stop()`. `Logger()` returns its logger, `Get(provider)` a constructed provider, and `ServeHTTP` makes it an `http.Handler`. |
| `Module` | A built module. Set its fields after `Build()`: `Name`, `IsGlobal`, `OnInit`, `OnReady`, `OnShutdown`. `Prefix(path)` adds a path prefix. |
| `RetryOptions` | The second argument of `Retry`; `nil` gives the defaults. `InitialDelay` (default 500 ms), `MaxDelay` (default 30 s) and `MaxAttempts` (default 0: until `ctx` is done). |
| `Provider` | The interface of a provider: `NewProvider() Provider`. |
| `Controller` | The interface of a controller: `NewController() Controller`. |
| `AppOptions` | The argument of `New`; `nil` gives the defaults. See [Configuration](#configuration). |
| `ServerOptions` | `AppOptions.Server`, used when Owlkei Go opens the listener: `Port` (default 3000), `ShutdownTimeout` (default 30 s), `TLS` (`TLSOptions`: `CertFile`, `KeyFile`, `Config`) and `IsH2C` (also accept unencrypted HTTP/2 without TLS). |
| `HTTPOptions` | `AppOptions.HTTP`: `MaxBodyBytes` (a `ByteSize`, default 10 MiB, negative turns the cap off) and `ServeDirs`. |
| `WSOptions` | `AppOptions.WS`. `Path` is the endpoint path; setting it turns WebSocket on. |
| `RenderOptions` | `AppOptions.Render`. `FS` holds the templates and the `assets` and `public` directories (required); `Dir` is the directory inside it (default `web`, `.` for the root); `IsReload` reads that directory from disk and parses it again when a file changes (development only). See [Pages](#pages). |
| `PathStyle` | The argument of `ModuleBuilder().PathStyle`: `PathStyleKebab` (the default, `READ_user_profile` → `/user-profile`) or `PathStyleAsWritten` (`/user_profile`). See [Routes from method names](#routes-from-method-names). |
| `VersioningOptions` | `AppOptions.Versioning`: `Type` (setting it turns versioning on), `Key` (default `v`), `DefaultVersion` and `Extractor`. See [Versioning](#versioning). |
| `VersioningType` | The type of `VersioningOptions.Type`: `VersioningQuery`, `VersioningHeader`, `VersioningMediaType` or `VersioningCustom`. |

### Controller embeds

| Type | Description |
|---|---|
| `HTTP` | Makes a controller serve HTTP routes and HTML pages (`RENDER_`, `SUBMIT_`). `Prefix(path, handlers...)` adds a route prefix, and `Path(path, handlers...)` gives handlers an exact path. |
| `WS` | Makes a controller handle WebSocket events. |
| `Middleware` | `BindMiddleware(middleware, handlers...)` binds a middleware to the controller's handlers. |
| `Guard` | `BindGuard(guard, handlers...)` binds a guard. |
| `Interceptor` | `BindInterceptor(interceptor, handlers...)` binds an interceptor. |
| `ExceptionFilter` | `BindExceptionFilter(filter, handlers...)` binds an exception filter. |

Without handlers, a layer applies to every handler of the controller.

### Layers

| Type | Description |
|---|---|
| `MiddlewareFn` | The interface of a middleware: `Use(*http.Request, http.ResponseWriter, Next)`. |
| `Aggregation` | The second parameter of `Intercept`: `Pipe(operators...)` runs the handler, and `Transform`, `Tap` and `Filter` build the operators. |
| `ArgumentMetadata` | The second parameter of a pipe's `Transform`: `ContextType` (`http` or `ws`) and `ParamType` (the source). |
| `Publisher` | `Publish(topic, message...) error` sends a message to the subscribers of a WebSocket topic. Ask for it as a handler parameter or a field. |

Guards, interceptors and exception filters are any value with the right method
(`CanActivate`, `Intercept` or `Catch`). `Create()` checks each method's shape and stops on a
wrong one, for example
`invalid guard: main.TokenGuard.CanActivate must be func(owlkei.HTTPContext) bool to be bound as a HTTP guard`.

### Contexts and request data

| Type | Description |
|---|---|
| `HTTPContext` | The request's context. It embeds `*http.Request` and `http.ResponseWriter`. `ID()` is the request ID; `Status(code)`, `Text`, `JSON`, `JSONP` and `Redirect(url)` write the response; `Body()`, `Form()`, `File()`, `Query()`, `Header()` and `Param()` read the request. |
| `WSContext` | A WebSocket message's context. It embeds `*websocket.Conn`. `ID()` is the message ID, `WSPayload()` the message, `Send(data)` answers the connection, and `Context()` / `SetContext(ctx)` carry values. |
| `Body` | `map[string]any`, the JSON body. `Get`, `Set`, `Del`, `Has`, `Bind` and `BindValue`. |
| `WSPayload` | `map[string]any`, the WebSocket message. `Bind` and `BindValue`. |
| `Query`, `Header`, `Param`, `Form` | `map[string][]string`. `Get`, `Set`, `Add`, `Del`, `Has`, `Bind` and `BindValue`. |
| `File` | The uploaded files. `Bind(dto)` binds them into fields of type `DataFile` or `[]DataFile`. |
| `DataFile` | One uploaded file: the embedded `*multipart.FileHeader`, `Index`, `Size`, `Total`, `Key`, `Filename`, `Type` and `Dest`. A file DTO may implement `IsValid(DataFile) bool` to refuse a file (`400`) and `Store(DataFile, multipart.File)` to save it. |
| `FieldLevel` | One bound field, returned by `Bind` for validation: `Tag`, `NestedTag`, `Namespace`, `Field`, `Index`, `Value`, `IsValue`, `Kind` and `Type`. |
| `Map` | `map[string]any`, the JSON object a handler returns. |
| `Next` | `func()`: continues the pipeline. |
| `Redirect` | `func(string)`: sends a redirect to the given URL. |

`Bind` returns the bound struct and its field levels. `BindValue` returns the struct only and
allocates nothing more.

---

## License

[MIT](LICENSE.md)
