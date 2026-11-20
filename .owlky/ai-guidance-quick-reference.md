# AI Agent Quick Reference: Owlky Go Decision Trees

**Purpose**: Rapid decision-making for common AI code generation scenarios in Owlky Go.

---

## Decision Tree: "How do I implement this feature?"

```
Feature Request
  │
  ├─ "I need an HTTP endpoint"
  │   ├─ Embed owlky.HTTP in controller
  │   ├─ Create method: READ/CREATE/UPDATE/MODIFY/DELETE_BY_params
  │   ├─ Add parameters to method signature (built-in types and pipes, internal/core/README.md › Handler injection)
  │   ├─ Register controller in ModuleBuilder().Controllers(...)
  │   └─ Providers go in exported controller fields (injected at Create())
  │
  ├─ "I need a server-rendered HTML page"
  │   ├─ AppOptions{Render: &owlky.RenderOptions{FS: web}}
  │   ├─ Embed owlky.HTTP; RENDER_<route> = GET page, SUBMIT_<route> = its form POST (same words)
  │   ├─ The route picks the template: web/<path>.html or web/<path>/index.html ({id} for parameters)
  │   ├─ Return the template's data (any type); SUBMIT_ re-renders with 422 or c.Redirect → 303
  │   └─ No owlky.Page / owlky.View: they don't exist
  │
  ├─ "I need WebSocket support"
  │   ├─ Embed owlky.WS in controller
  │   ├─ Create method: SUBSCRIBE_<topic> (e.g. SUBSCRIBE_chat_ANY → chat.*, SUBSCRIBE_chat_ALL → chat.**)
  │   ├─ Inject owlky.WSContext, *websocket.Conn, owlky.Publisher
  │   ├─ Register controller in ModuleBuilder().Controllers(...)
  │   └─ Use publisher.Publish() for fanout
  │
  ├─ "I need to validate input"
  │   ├─ Option 1: Create Pipeable struct (Transform method)
  │   ├─ Option 2: Validate in the handler and panic exception.BadRequestException(...)
  │   ├─ (a middleware panic skips your exception filters: fixed error response)
  │   └─ Guards decide access, not input shape
  │
  ├─ "I need authentication"
  │   ├─ Create Guard struct with CanActivate()
  │   ├─ Guard checks JWT/session in httpCtx.Request.Header
  │   ├─ Return true if authorized, false for 403
  │   ├─ Or panic exception.UnauthorizedException(...) for 401 (reaches the exception filters)
  │   └─ Bind guard to specific handlers
  │
  ├─ "I need to modify request/response"
  │   ├─ Use Middleware for pre-processing
  │   ├─ Use Interceptor for post-processing
  │   ├─ Middleware can call next() or stop
  │   └─ Interceptor pipes operators over the handler result
  │
  ├─ "I need to handle errors"
  │   ├─ Panic with exception.BadRequestException(...) in handler
  │   ├─ Exception will be caught and exception filter called
  │   ├─ Or return error from handler
  │   └─ Custom exception filter can catch specific exceptions
  │
  ├─ "I need access to database/service"
  │   ├─ Create Provider with NewProvider()
  │   ├─ Register in ModuleBuilder().Providers(...)
  │   ├─ Inject into handler/controller/other-provider
  │   ├─ Framework resolves dependency automatically
  │   └─ DON'T instantiate manually
  │
  ├─ "I need to log something"
  │   ├─ Inject log.Logger into handler
  │   ├─ Call logger.Log(level, data)
  │   ├─ Or use global app.Logger
  │   └─ Logger is available everywhere
  │
  └─ "I need to send events (WebSocket)"
      ├─ Inject owlky.Publisher into handler
      ├─ Call publisher.Publish(topic, data)
      ├─ All WS clients subscribed to topic receive message
      └─ Use topic patterns for selective delivery
```

---

## Decision Tree: "What's the right place for this code?"

```
Piece of Code
  │
  ├─ "Runs for every request"
  │   └─ Use Middleware (BindGlobalMiddlewares or BindMiddleware)
  │
  ├─ "Checks if request is allowed"
  │   └─ Use Guard (BindGuard on controller)
  │       Note: Guard MUST return bool, NOT panic
  │
  ├─ "Transforms request or response"
  │   ├─ Pre-handler transformation → Interceptor (pre-phase)
  │   ├─ Post-handler transformation → Interceptor (post-phase)
  │   └─ Use interceptor.Intercept(httpCtx, aggregation)
  │
  ├─ "Handles errors/panics"
  │   └─ Use Exception Filter (BindExceptionFilter on controller)
  │       Note: Only place that can catch panics
  │
  ├─ "Business logic for specific handler"
  │   └─ Write handler method in Controller
  │       Parameters injected from DI container
  │
  ├─ "Business logic reused across handlers"
  │   └─ Create Provider
  │       Register in ModuleBuilder().Providers(...)
  │       Inject into handlers/other-providers
  │
  ├─ "Initialization/setup code"
  │   ├─ If per-handler: put in middleware
  │   ├─ If per-module: the module's OnInit / OnReady hook, or setup in NewProvider() (called once per app)
  │   └─ If global: set IsGlobal = true on the module, or BindGlobalMiddlewares
  │
  ├─ "Cleanup/teardown code"
  │   ├─ Per request: defer in the handler, or an interceptor's Pipe operators
  │   └─ Per app: the module's OnShutdown hook (runs after the drain, reverse init order)
  │
  └─ "Logging, tracing, monitoring"
      ├─ Per-request timing: AppOptions.IsAccessLog (the app's tracer has no public listener API)
      ├─ Use Logger for structured logging (inject log.Logger, or app.Logger())
      └─ Can go in middleware, handler, or provider
```

---

## Decision Tree: "What dependency should I inject?"

```
I need access to...
  │
  ├─ "Current HTTP request"
  │   └─ *http.Request (HTTP only)
  │
  ├─ "Response writer"
  │   └─ http.ResponseWriter (HTTP only)
  │
  ├─ "Full request context with helpers"
  │   ├─ HTTP: owlky.HTTPContext
  │   └─ WS: owlky.WSContext
  │
  ├─ "Request body (parsed JSON/form)"
  │   └─ owlky.Body (HTTP only)
  │
  ├─ "Query parameters"
  │   └─ owlky.Query (HTTP only)
  │
  ├─ "Path parameters (:id, :name, etc.)"
  │   └─ owlky.Param (HTTP only)
  │
  ├─ "HTTP headers"
  │   └─ owlky.Header (HTTP only)
  │
  ├─ "File uploads"
  │   └─ owlky.File (HTTP only)
  │
  ├─ "WebSocket connection"
  │   └─ *websocket.Conn (WS only)
  │
  ├─ "Incoming WebSocket message"
  │   └─ owlky.WSPayload (WS only)
  │
  ├─ "Send response/message"
  │   ├─ HTTP: httpCtx.JSON() or Text() method
  │   └─ WS: owlky.Publisher for fanout
  │
  ├─ "Custom service/provider"
  │   └─ Register as Provider, inject by type
  │       Framework resolves automatically
  │
  ├─ "Logger"
  │   └─ log.Logger (globally available)
  │
  ├─ "Event publisher"
  │   └─ owlky.Publisher (for WS fanout)
  │
  ├─ "Call next middleware"
  │   └─ owlky.Next (function injected in middleware)
  │
  └─ "Redirect response"
      └─ owlky.Redirect (function injected in handler)
```

---

## Decision Tree: "How do I handle this error?"

```
Error Scenario
  │
  ├─ "Input validation error (bad data)"
  │   ├─ Option 1: Panic with exception.BadRequestException
  │   ├─ Option 2: Return error type from handler
  │   ├─ Option 3: Validate in middleware, call next() or not
  │   └─ Exception filter writes response
  │
  ├─ "Authentication error (no token/invalid token)"
  │   ├─ Option 1: Return false from Guard
  │   ├─ Option 2: Middleware checks before guard
  │   ├─ Option 3: Exception filter for custom auth errors
  │   └─ Result: 403 Forbidden or custom error response
  │
  ├─ "Authorization error (user lacks permission)"
  │   ├─ Same as authentication
  │   └─ Guard returns false = 403 Forbidden
  │
  ├─ "Database error (connection failed)"
  │   ├─ Panic with exception.ServiceUnavailableException
  │   ├─ Or return custom error from handler
  │   └─ Exception filter writes 503 Service Unavailable
  │
  ├─ "Resource not found"
  │   ├─ Panic with exception.NotFoundException
  │   └─ Exception filter writes 404 Not Found
  │
  ├─ "Request timeout (handler takes too long)"
  │   ├─ NOT built in — there is no httpCtx.SetDeadline()/IsDeadlineExceeded(), no automatic 408
  │   ├─ Only server-level ReadTimeout/WriteTimeout exist (fixed, set by the serving call), unrelated to handler duration
  │   └─ To bound a slow handler yourself: wrap the slow call in your own context.WithTimeout and panic(exception.RequestTimeoutException(...)) on ctx.Err()
  │
  ├─ "Internal server error (unexpected panic)"
  │   ├─ Any non-exception panic answers a fixed 500 and is logged (UnhandledPanic)
  │   └─ Filters receive InternalServerErrorException("Internal Server Error"), never the value
  │
  ├─ "Not implemented"
  │   ├─ Panic with exception.NotImplementedException
  │   └─ Exception filter writes 501 Not Implemented
  │
  └─ "Panic in a middleware"
      ├─ Never reaches exception filters
      ├─ Exception → its code and message; anything else → fixed 500
      └─ To answer differently: write the response and return without next()
```

---

## Decision Tree: "How do I structure my application?"

```
Architecture Question
  │
  ├─ "How many modules should I have?"
  │   ├─ Minimum: 1 root module
  │   ├─ Recommended: 1 per feature (users, products, orders)
  │   ├─ Use imports to compose
  │   └─ Each module handles one responsibility
  │
  ├─ "How do I share code between modules?"
  │   ├─ Create shared Provider (register in imported module)
  │   ├─ Set m.IsGlobal = true on the built module for truly global services
  │   ├─ Otherwise import the module: its providers are visible to the importer
  │   └─ Inject as exported fields of controllers/providers in other modules
  │
  ├─ "Where should I put validation logic?"
  │   ├─ Option 1: Pipeable (custom transform for a specific param)
  │   ├─ Option 2: Interceptor pre-phase (runs before handler)
  │   └─ Guards are for auth/permissions
  │
  ├─ "Where should I put caching logic?"
  │   ├─ Create Provider with injected CacheService
  │   ├─ Cache wrapper around database queries
  │   ├─ Invalidate cache on writes
  │   └─ Use cache.NewMemoryCache or an external cache backend
  │
  ├─ "How do I structure database access?"
  │   ├─ Create Repository Provider (data access)
  │   ├─ Create Service Provider (business logic)
  │   ├─ Service depends on Repository
  │   ├─ Controller depends on Service (not directly on Repository)
  │   └─ All via dependency injection
  │
  ├─ "How do I handle cross-cutting concerns?"
  │   ├─ Logging → Middleware or interceptor
  │   ├─ Error handling → Exception filter
  │   ├─ Authentication → Guard or middleware
  │   ├─ Tracing → AppOptions.IsAccessLog, or an interceptor
  │   └─ Rate limiting → Guard
  │
  └─ "How do I test this application?"
      ├─ Create new app instance per test
      ├─ Modules may be shared: every owlky.New(opts) has its own routes, providers and layers
      ├─ Mock providers via dependency injection
      ├─ Use httptest.Server for HTTP testing
      ├─ For WS: use real websocket connection or mock interface
      └─ Never share state between tests
```

---

## Decision Tree: "Is this pattern safe?"

```
Pattern Question
  │
  ├─ "Should I hold onto context after handler?"
  │   └─ NO - context is pooled, reused in next request
  │       Result: stale data, wrong request
  │
  ├─ "Should I mutate context after guard?"
  │   └─ NO - guard made decisions based on original state
  │       Result: bypass guard logic, security issue
  │
  ├─ "Should I call next() multiple times?"
  │   └─ Harmless but pointless - a second call does nothing
  │       Code after next() runs before the next layer, not after the handler
  │
  ├─ "Should I panic in guard or interceptor?"
  │   └─ YES with an exception - it reaches the route's exception filters
  │       A non-exception value answers a fixed 500 and is logged
  │
  ├─ "Should I hold state in provider?"
  │   └─ DEPENDS - if mutable, protect with sync.Mutex
  │       Result: race conditions without mutex
  │
  ├─ "Should I use raw conn.Send() in WS handler?"
  │   └─ NO - concurrent with fanout writes
  │       Result: message corruption
  │
  ├─ "Should I store mutable state in module?"
  │   └─ NO - a module value may be given to several apps; providers are constructed once per app
  │       Result: race conditions
  │
  ├─ "Should I call NewProvider() manually?"
  │   └─ NO - the app constructs each provider once and injects its fields first
  │       Result: uninjected fields, a second instance
  │
  ├─ "Should I have circular dependencies?"
  │   └─ Can't with value providers - the recursive type doesn't compile
  │
  ├─ "Can I bind the same Middleware/Guard/Interceptor/ExceptionFilter type twice with different config?"
  │   └─ YES - the app keeps one instance per configuration (type + constructor + reflect.DeepEqual value)
  │       Equal configurations share one instance; different ones get their own
  │
  ├─ "Should I set CORS: &owlky.CORSOptions{IsEnabled: true} without Origins?"
  │   └─ NO - an empty AllowOrigins panics at app.Create(); "*" never with credentials
  │       Fix: Origins: []string{"https://app.example.com"}, CORS: &owlky.CORSOptions{IsEnabled: true, IsAllowCredentials: true}
  │
  ├─ "Should I read X-Forwarded-For / X-Real-IP for the client IP?"
  │   └─ NO - set Proxy: &owlky.ProxyOptions{TrustedProxies: ...} and call owlky.ClientIP(c.Request)
  │
  ├─ "Should I add a CSRF token or cookie?"
  │   └─ NO - CSRF: &owlky.CSRFOptions{IsEnabled: true} uses Sec-Fetch-Site and Origin; trusted origins come from AppOptions.Origins
  │
  └─ "Should I assume request bodies are unlimited or add app.SetMaxBodySize(...)?"
      └─ NO - 10 MiB cap by default; change it with AppOptions{HTTP: &owlky.HTTPOptions{MaxBodyBytes: 50 * owlky.MiB}} (negative = off)
          Result: SetMaxBodySize won't compile; bodies over the cap panic 413 from owlky.Body()
```

---

## Quick Rules (Memorize These)

1. **Context Lifecycle**: Never hold context after handler returns
2. **Dependency Injection**: Let framework inject, never call NewProvider()
3. **Guard Behavior**: Must return bool, never panic
4. **Exception Handling**: Only exception filter catches panics
5. **WebSocket**: Use Publisher, never raw conn access
6. **Middleware**: Call next() exactly once
7. **Pipeline Order**: Middleware → Guard → Interceptor → Handler → Filter
8. **Module Init**: NEVER reorder app.Create() steps
9. **Provider Injection**: Type MUST match exactly (pointer vs value)
10. **Performance**: Reflection cost ~5-10μs per dependency

---

## File Organization Pattern

```
myapp/
├── main.go
├── modules/
│   ├── users/
│   │   ├── user_controller.go
│   │   ├── user_service.go
│   │   ├── user_repository.go
│   │   └── user_module.go
│   ├── products/
│   │   ├── product_controller.go
│   │   ├── product_service.go
│   │   └── product_module.go
│   └── shared/
│       ├── config_service.go
│       ├── database.go
│       └── shared_module.go
├── common/
│   ├── middleware.go (global middleware)
│   ├── guards.go
│   └── filters.go
└── tests/
    └── ...
```

