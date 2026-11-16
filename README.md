# Ginject

A NestJS-inspired dependency injection web framework for Go. Build structured, modular HTTP and WebSocket servers using the **Module → Controller → Provider** pattern.

- [Ginject](#ginject)
  - [Features](#features)
  - [Installation](#installation)
  - [Quick Start](#quick-start)
  - [Core Concepts](#core-concepts)
    - [Application](#application)
    - [Module](#module)
    - [Provider](#provider)
    - [Controller](#controller)
    - [Routing Convention](#routing-convention)
    - [Handler Parameter Injection](#handler-parameter-injection)
    - [Request Pipeline](#request-pipeline)
    - [Middleware](#middleware)
    - [Guard](#guard)
    - [Interceptor](#interceptor)
    - [Exception Filter](#exception-filter)
    - [Pipe / DTO](#pipe--dto)
  - [WebSocket](#websocket)
  - [Versioning](#versioning)
  - [Built-in Modules](#built-in-modules)
    - [Config](#config)
    - [Cache](#cache)
    - [Outbound HTTP calls](#outbound-http-calls)
  - [Built-in Middlewares](#built-in-middlewares)
  - [Production checklist](#production-checklist)
  - [Exception Types](#exception-types)

---

## Features

- Reflection-based dependency injection — no tags, no code generation
- Convention-over-configuration routing via method names
- Full request pipeline: middleware → guard → interceptor → handler → exception filter
- WebSocket support with event-based routing
- API versioning (query, header, media type, custom)
- Type-safe handler parameter injection
- Built-in Config and Cache modules
- Opt-in production middlewares: load shedding, trusted proxies, host check, HTTPS redirect, security headers, CORS, CSRF, compression, and a rate limit guard
- JSON logs with request and trace IDs, and an access log

---

## Installation

```bash
go get github.com/dangduoc08/ginject
```

---

## Quick Start

```go
package main

import (
    "github.com/dangduoc08/ginject/common"
    "github.com/dangduoc08/ginject/core"
)

type UserController struct {
    common.HTTP
}

func (c UserController) NewController() core.Controller { return c }

func (c UserController) READ() string {
    return "hello"
}

var UserModule = func() *core.Module {
    return core.ModuleBuilder().Controllers(UserController{}).Build()
}

func main() {
    app := core.New()
    app.Create(core.ModuleBuilder().Imports(UserModule).Build())
    if err := app.Listen(3000, nil); err != nil {
        app.Logger().Fatal("AppError", "error", err)
    }
}
```

`GET http://localhost:3000/` → `hello`

---

## Core Concepts

### Application

`core.New()` creates the application. Chain configuration before `Create`:

```go
app := core.New()

app.
    SetLogger(logger).
    BindGlobalMiddlewares(cors.CORS{AllowOrigins: []string{"https://app.example.com"}}).
    BindGlobalGuards(RateLimiterGuard{}).
    BindGlobalInterceptors(ResponseInterceptor{}).
    BindGlobalExceptionFilters(CustomExceptionFilter{})

app.EnableVersioning(versioning.Versioning{
    Type: versioning.HEADER,
    Key:  "X-Api-Version",
})

app.Create(RootModule)
if err := app.Listen(3000, nil); err != nil {
    app.Logger().Fatal("AppError", "error", err)
}
```

### Module

Modules group related controllers and providers. A module value is wired once per app (each `core.New()` owns its providers, routes and layers, so two apps in one process are independent); dynamic modules accept configuration arguments.

**Static module:**

```go
var UserModule = func() *core.Module {
    module := core.ModuleBuilder().
        Imports(DatabaseModule).
        Controllers(UserController{}).
        Providers(UserService{}).
        Build()

    module.Prefix("users")
    return module
}
```

**Dynamic module** (configurable):

```go
type DatabaseModule struct {
    Host string
    Port int
}

func (d DatabaseModule) New() *core.Module {
    return core.ModuleBuilder().
        Providers(DatabaseService{Host: d.Host, Port: d.Port}).
        Build()
}

// Usage
core.ModuleBuilder().
    Imports(DatabaseModule{Host: "localhost", Port: 5432}).
    Build()
```

**Global module** — providers available everywhere without explicit import:

```go
module := core.ModuleBuilder().Providers(ConfigService{}).Build()
module.IsGlobal = true
```

### Provider

Any struct implementing `NewProvider() Provider`. Providers are injected by field type — declare the field and the framework resolves it automatically.

```go
type UserService struct{}

func (s UserService) NewProvider() core.Provider { return s }

func (s UserService) FindAll() []string {
    return []string{"alice", "bob"}
}

// Inject into another provider or controller
type UserController struct {
    common.HTTP
    UserService UserService // resolved automatically
}
```

### Controller

Implements `NewController() Controller`. Embed `common.HTTP` for HTTP or `common.WS` for WebSocket. Bind per-controller layers in `NewController`:

```go
type ManufacturerController struct {
    common.HTTP
    common.Guard
    common.Middleware
    common.Interceptor
    common.ExceptionFilter
    UserService UserService
}

func (c ManufacturerController) NewController() core.Controller {
    c.BindGuard(AuthGuard{}, c.CREATE_VERSION_1, c.DELETE_VERSION_1)
    c.BindMiddleware(LogMiddleware{}, c.UPDATE_VERSION_1)
    return c
}
```

### Routing Convention

Method names are parsed into HTTP routes — no annotations needed.

**HTTP method tokens:**

| Token       | HTTP Method |
|-------------|-------------|
| `READ`      | GET         |
| `CREATE`    | POST        |
| `UPDATE`    | PUT         |
| `MODIFY`    | PATCH       |
| `DELETE`    | DELETE      |
| `PREFLIGHT` | OPTIONS     |

**Path tokens:**

| Token     | Effect                  | Example                                     |
|-----------|-------------------------|---------------------------------------------|
| `BY`      | Path parameter          | `READ_BY_ID` → `GET /:id`                  |
| `AND`     | Additional segment      | `READ_AND_PROFILE` → `GET /profile`        |
| `OF`      | Sub-resource            | `READ_OF_USERS` → `GET /users`             |
| `ANY`     | Wildcard `*`            | `READ_ANY` → `GET /*`                      |
| `FILE`    | File extension wildcard | `READ_ANY_FILE_HTML` → `GET /*.html`       |
| `VERSION` | API version suffix      | `READ_VERSION_1` → version `1`             |

**Examples:**

```go
func (c UserController) READ() []User {}                     // GET /
func (c UserController) READ_BY_ID() User {}                 // GET /:id
func (c UserController) READ_BY_ID_AND_PROFILE() Profile {}  // GET /:id/profile
func (c UserController) CREATE_VERSION_1(body Body) Map {}   // POST / (version 1)
func (c UserController) DELETE_BY_ID_VERSION_1() {}          // DELETE /:id (version 1)
```

Return values are serialized automatically: structs/maps → JSON, strings/numbers → plain text.

### Handler Parameter Injection

Declare parameters by type — the framework resolves them from the request context:

```go
func (c UserController) READ_BY_ID(
    ctx    ginject.HTTPContext,  // *ctx.HTTPContext
    req    ginject.Request,  // *http.Request
    param  ginject.Param,    // path parameters
    query  ginject.Query,    // query string
    header ginject.Header,   // request headers
) User {
    id := param.Get("id")
    // ...
}

func (c UserController) CREATE(
    body ginject.Body,     // request body (JSON)
    res  ginject.Response, // http.ResponseWriter
) Map {}

func (c UserController) UPLOAD(
    form ginject.Form, // multipart form
    file ginject.File, // uploaded files
) Map {}
```

All types are re-exported from the root `ginject` package.

### Request Pipeline

```
GlobalMiddlewares
    → ModuleMiddlewares
        → Guards
            → Interceptors (pre)
                → MainHandler
            ← Interceptors (post / Pipe)
    ← ExceptionFilters (on panic)
```

Each layer runs per-request. Exception filters catch panics from any layer.

### Middleware

Implement `Use(*http.Request, http.ResponseWriter, ctx.Next)`:

```go
type LogMiddleware struct {
    common.Logger
}

func (m LogMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
    m.From(r.Context()).Info("request", "path", r.URL.Path)
    next()
}
```

`From(ctx)` adds the request's `trace_id` and `request` ID to the line, with the built-in logger and with any logger given to `SetLogger`.

**Global** (all routes):

```go
app.BindGlobalMiddlewares(LogMiddleware{})
```

**Per-controller** (all or specific handlers):

```go
c.BindMiddleware(LogMiddleware{})                      // all handlers
c.BindMiddleware(LogMiddleware{}, c.CREATE_VERSION_1)  // specific handler only
```

### Guard

Implement `CanActivate(*ctx.HTTPContext) bool` for a REST guard, or `CanActivate(*ctx.WSContext) bool` for a WS guard. Ginject inspects the bound guard's `CanActivate` signature to tell which one it is — a REST-shaped guard only ever runs for REST routes, a WS-shaped guard only for WS events. Returning `false` responds with 403 Forbidden:

```go
type AuthGuard struct {
    common.Guard
    config.ConfigService
    Secret string
}

func (g AuthGuard) NewGuard() AuthGuard {
    g.Secret = g.ConfigService.Get("AUTH_SECRET").(string)
    return g
}

func (g AuthGuard) CanActivate(c *ctx.HTTPContext) bool {
    return c.Header().Get("Authorization") == g.Secret
}
```

**Global:**

```go
app.BindGlobalGuards(AuthGuard{})
```

**Per-controller:**

```go
c.BindGuard(AuthGuard{}, c.CREATE_VERSION_1, c.DELETE_VERSION_1)
```

### Interceptor

Implement `Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any` for a REST interceptor, or `Intercept(*ctx.WSContext, *aggregation.Aggregation) any` for a WS one. Ginject inspects the bound interceptor's `Intercept` signature to tell which one it is — a REST-shaped interceptor only ever wraps REST handlers, a WS-shaped one only WS handlers. Runs before and after the handler via `Pipe`:

```go
type ResponseInterceptor struct{}

func (i ResponseInterceptor) Intercept(c ginject.HTTPContext, agg ginject.Aggregation) any {
    return agg.Pipe(
        agg.Consume(func(c ginject.HTTPContext, data any) any {
            return ginject.Map{"data": data}
        }),
    )
}
```

**Global:**

```go
app.BindGlobalInterceptors(ResponseInterceptor{})
```

**Per-controller:**

```go
c.BindInterceptor(ResponseInterceptor{})
```

### Exception Filter

Implement `Catch(*ctx.HTTPContext, *exception.Exception)` for a REST exception filter, or `Catch(*ctx.WSContext, *exception.Exception)` for a WS one. Ginject inspects the bound filter's `Catch` signature to tell which one it is — a REST-shaped filter only ever catches REST panics, a WS-shaped filter only WS ones. Catches panics and unhandled exceptions:

```go
type AppExceptionFilter struct{}

func (f AppExceptionFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
    httpCode, _ := ex.GetHTTPStatus()
    c.Status(httpCode).JSON(ctx.Map{
        "code":    ex.GetCode(),
        "error":   ex.Error(),
        "message": ex.GetResponse(),
    })
}
```

**Global:**

```go
app.BindGlobalExceptionFilters(AppExceptionFilter{})
```

**Per-controller:**

```go
c.BindExceptionFilter(AppExceptionFilter{})
c.BindExceptionFilter(AppExceptionFilter{}, c.CREATE_VERSION_1)
```

Throw exceptions anywhere in handlers:

```go
func (c UserController) READ_BY_ID(param ginject.Param) User {
    user := findUser(param.Get("id"))
    if user == nil {
        panic(exception.NotFoundException("user not found"))
    }
    return *user
}
```

### Pipe / DTO

Pipes transform and validate handler parameters before injection. Implement the `Transform` method matching the parameter source:

```go
type CreateUserBody struct {
    Name  string `bind:"name"`
    Email string `bind:"email"`
}

func (dto CreateUserBody) Transform(body ginject.Body, meta common.ArgumentMetadata) any {
    result, _ := body.Bind(dto)
    return result
}

// Declare as handler parameter — automatically bound and transformed
func (c UserController) CREATE(body CreateUserBody) Map {
    return ginject.Map{"name": body.Name}
}
```

Supported pipe interfaces: `BodyPipeable`, `QueryPipeable`, `ParamPipeable`, `HeaderPipeable`, `FormPipeable`, `FilePipeable`, `ContextPipeable`, `WSPayloadPipeable`.

---

## WebSocket

Embed `common.WS` instead of `common.HTTP`. Method names map to subscribe event names:

```go
type ChatController struct {
    common.WS
}

func (c ChatController) NewController() core.Controller { return c }

func (c ChatController) MESSAGE(payload ginject.WSPayload) string {
    return payload.Get("text").(string)
}
```

The WS endpoint is available at `/ws`. Clients send JSON messages:

```json
{ "event": "message", "data": { "text": "hello" } }
```

---

## Versioning

Enable versioning before `Create`:

```go
app.EnableVersioning(versioning.Versioning{
    Type:           versioning.HEADER,
    Key:            "X-Api-Version",
    DefaultVersion: "1",
})
```

**Types:**

| Type         | Description                          | Example                                   |
|--------------|--------------------------------------|-------------------------------------------|
| `QUERY`      | Read version from query parameter    | `GET /users?v=1`                         |
| `HEADER`     | Read version from request header     | `X-Api-Version: 1`                       |
| `MEDIA_TYPE` | Read version from `Accept` header    | `Accept: application/json; version=1`    |
| `CUSTOM`     | Custom extractor function            | `Extractor: func(c *ctx.HTTPContext) string` |

Add `_VERSION_N` suffix to controller methods:

```go
func (c UserController) READ_VERSION_1() Map { /* v1 logic */ }
func (c UserController) READ_VERSION_2() Map { /* v2 logic */ }
```

---

## Built-in Modules

### Config

Reads `.env` files with type binding, variable expansion, and validation hooks. See the [Config module documentation](modules/config/README.md) for full details.

```go
import "github.com/dangduoc08/ginject/modules/config"

core.ModuleBuilder().
    Imports(
        config.Register(&config.ConfigModuleOptions{
            IsGlobal:          true,
            IsExpandVariables: true,
        }),
    ).
    Build()

// Inject ConfigService into any provider
type DBProvider struct {
    config.ConfigService
}

func (p DBProvider) NewProvider() core.Provider {
    host := p.Get("DB_HOST").(string)
    return p
}
```

**`ConfigModuleOptions`:**

| Field               | Type             | Description                                      |
|---------------------|------------------|--------------------------------------------------|
| `IsGlobal`          | `bool`           | Make providers available everywhere              |
| `IsIgnoreEnvFile`   | `bool`           | Use OS env vars only, skip `.env`                |
| `IsOverride`        | `bool`           | Override OS env vars with `.env` values          |
| `IsExpandVariables` | `bool`           | Enable `${VAR}` expansion in values              |
| `ENVFilePaths`      | `[]string`       | Custom `.env` file paths (default: `[".env"]`)   |
| `Loads`             | `[]ConfigLoadFn` | Custom loaders returning `map[string]any`        |
| `Hooks`             | `[]ConfigHookFn` | Hooks to transform/validate config at startup    |
| `OnInit`            | `func()`         | Lifecycle hook before module injection           |

### Cache

Key-value cache with expiry and an atomic counter, on a bounded in-memory store by default or any backend you write. See the [Cache module documentation](modules/cache/README.md) for full details.

```go
import (
    "context"
    "time"

    "github.com/dangduoc08/ginject/modules/cache"
)

core.ModuleBuilder().
    Imports(
        cache.Register(&cache.CacheModuleOptions{
            IsGlobal: true,
        }),
    ).
    Build()

// Inject CacheService into any provider or controller
type UserController struct {
    common.HTTP
    Cache cache.CacheService
}

func (instance UserController) READ_BY_ID(param ginject.Param) string {
    ctx := context.Background()
    key := "user:" + param.Get("ID")

    if data, ok := instance.Cache.Get(ctx, key); ok {
        return string(data)
    }

    data := fetchFromDB(param.Get("ID"))
    _ = instance.Cache.Set(ctx, key, []byte(data), 5*time.Minute)
    return data
}
```

Values are bytes. `Incr` counts atomically, and the default store is bounded (65,536 entries,
16 MiB) with no goroutine while it is empty.

**`CacheModuleOptions`:**

| Field      | Type          | Description                                          |
|------------|---------------|------------------------------------------------------|
| `IsGlobal` | `bool`        | Make `CacheService` available everywhere             |
| `OnInit`   | `func()`      | Runs once when the app that imports the module is created |
| `Backend`  | `cache.Cache` | The store; default `memorycache.NewMemoryCache(nil)` |

### Outbound HTTP calls

Ginject ships no HTTP client module. Call other services with `net/http` and wrap the transport with
`trace.Transport`, so the request continues the incoming request's trace (`Traceparent`). See the
[trace documentation](trace/README.md#outbound-calls) for the rules.

```go
import (
    "net/http"
    "time"

    "github.com/dangduoc08/ginject/trace"
)

var client = &http.Client{Transport: trace.Transport(nil), Timeout: 10 * time.Second}

func (instance UserController) READ_profile(c *ctx.HTTPContext) any {
    req, err := http.NewRequestWithContext(c.Context(), http.MethodGet, "https://accounts.example.com/me", nil)
    if err != nil {
        return err
    }
    res, err := client.Do(req)
    if err != nil {
        return err
    }
    defer res.Body.Close()
    return res.StatusCode
}
```

---

## Built-in Middlewares

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

## Exception Types

| Function                           | HTTP Status |
|------------------------------------|-------------|
| `BadRequestException`              | 400         |
| `UnauthorizedException`            | 401         |
| `ForbiddenException`               | 403         |
| `NotFoundException`                | 404         |
| `MethodNotAllowedException`        | 405         |
| `RequestTimeoutException`          | 408         |
| `ConflictException`                | 409         |
| `GoneException`                    | 410         |
| `RequestEntityTooLargeException`   | 413         |
| `UnprocessableEntityException`     | 422         |
| `InternalServerErrorException`     | 500         |
| `NotImplementedException`          | 501         |
| `BadGatewayException`              | 502         |
| `ServiceUnavailableException`      | 503         |
| `GatewayTimeoutException`          | 504         |

```go
panic(exception.NotFoundException("resource not found"))

panic(exception.BadRequestException("invalid input", exception.ExceptionOptions{
    Description: "field 'email' is required",
}))
```

---

## License

[MIT](LICENSE.md)
