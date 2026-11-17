# Exported API inventory (roadmap 0007, step 1)

Generated 2026-11-18 from `master` at `2647b71` by a throwaway program (`go list -json ./...`, `go/parser`,
`go/types` with the standard library source importer; not committed). It is the input of
`docs/design/0017-public-api.md`. Regenerate it only if the review restarts: after step 3 the committed
API file (`api/ginject.txt`) is the living record.

## How to read it

- **Scope:** every package outside `internal/`, `sample/` and `bench/`, and the root package.
- **Name:** `Type.Member` for methods, fields and interface methods. Methods of the two unexported builder
  types that exported functions return (`core.moduleBuilder`, `devtool.devtoolBuilder`) are listed, because
  callers reach them. The other 66 exported methods of unexported types (compiled middlewares, loggers,
  response writers) only implement interfaces and are not part of the surface.
- **Used outside its package:** framework packages whose non-test code uses the name (type-checked uses,
  so promoted methods and fields count); `sample/`; tests of other packages; docs. Docs are a text match
  in the READMEs, `CLAUDE.md` and `.ginject/`: exact (`pkg.Name`) for package-level names, by member name
  for methods and fields (the package's own README, the main README and `.ginject/` only), so a common
  member name such as `Name` or `Get` can match by accident. Uses through reflection (a method found by
  name, an interface implemented by shape) are not counted; the class takes them into account.
- **Class:** *user API* (application code names or calls it), *extension point* (an interface or type
  applications implement or pass in), *framework internal* (only the framework needs it).
- **Design 0017:** what the design does with the name. "Reachable" means the name stays exported on a type
  that moves under `internal/` and is reached through the root package.

## Summary

| Package | Names | Package-level names and methods | User API | Extension point | Framework internal | Kept by 0017 |
|---|---|---|---|---|---|---|
| `ginject (root)` | 21 | 21 | 21 | 0 | 0 | 17 |
| `accesslog` | 6 | 4 | 0 | 0 | 6 | 0 |
| `aggregation` | 12 | 9 | 6 | 0 | 6 | 6 |
| `common` | 173 | 98 | 16 | 26 | 131 | 26 |
| `core` | 75 | 54 | 38 | 4 | 33 | 42 |
| `ctx` | 130 | 108 | 88 | 0 | 42 | 88 |
| `devtool` | 55 | 26 | 0 | 0 | 55 | 0 |
| `exception` | 43 | 41 | 43 | 0 | 0 | 43 |
| `guards/throttler` | 11 | 6 | 11 | 0 | 0 | 11 |
| `log` | 15 | 11 | 15 | 0 | 0 | 15 |
| `memorybroker` | 26 | 15 | 0 | 0 | 26 | 0 |
| `memorycache` | 16 | 14 | 4 | 0 | 12 | 4 |
| `middlewares/compress` | 4 | 3 | 4 | 0 | 0 | 4 |
| `middlewares/cors` | 10 | 3 | 10 | 0 | 0 | 10 |
| `middlewares/csrf` | 4 | 3 | 4 | 0 | 0 | 4 |
| `middlewares/helmet` | 12 | 4 | 12 | 0 | 0 | 12 |
| `middlewares/hostcheck` | 4 | 3 | 4 | 0 | 0 | 4 |
| `middlewares/httpsredirect` | 5 | 3 | 5 | 0 | 0 | 5 |
| `middlewares/loadshed` | 5 | 3 | 5 | 0 | 0 | 5 |
| `middlewares/proxy` | 13 | 9 | 13 | 0 | 0 | 13 |
| `modules/cache` | 28 | 17 | 20 | 8 | 0 | 28 |
| `modules/config` | 19 | 10 | 19 | 0 | 0 | 19 |
| `pattern` | 12 | 12 | 0 | 0 | 12 | 0 |
| `routing` | 27 | 16 | 0 | 0 | 27 | 0 |
| `trace` | 28 | 16 | 1 | 0 | 27 | 1 |
| `versioning` | 13 | 9 | 9 | 0 | 4 | 9 |
| `wsevent` | 9 | 7 | 0 | 0 | 9 | 0 |
| **Total** | 776 | 525 | 348 | 38 | 390 | 366 |

"Names" counts exported fields and interface methods too; "Package-level names and methods" is the count the
roadmap quotes (about 500). "Kept by 0017" counts the names that are still exported and reachable by an
application after the design, under their old or new name. The design adds 2 names that do not exist today
(`ginject.VersioningType`, `log.Format`), so the surface after it is 368 names, and the root package gains 22
spellings (aliases, 3 functions, 4 constants) of names counted here under their current package.

Findings that the numbers hide:

- 390 of 776 names are framework internals, and 223 of them are used by another framework package, so they
  cannot simply be unexported: Go has no way to share a name between two packages without exporting it.
  Hiding them means an `internal/` path.
- `sample/` imports `common` (26 files), `core` (16) and `ctx` (10) next to the root package (19): the 21 root
  aliases are not enough to write an application (no `New`, no module builder, no `HTTP` to embed).
- Three names the roadmap and the backlog cite as examples no longer exist on `master`: `common.Construct`,
  `common.BuildHTTPInterceptMiddleware` and `common.InsertedRoutes`. `ctx.BinderOf` and
  `common.BuildWSInterceptMiddleware` are still there.
- Exported names that no other package, no other package's test and no sample file uses: the root aliases
  `Router`, `Request`, `Response`, `App`, `Body`, `Form`, `File`, `FieldLevel`, `Publisher`, `Redirect`,
  `Versioning` and `WSPayload`; `core.NewWS`, `core.NewWSConnmgr`, the `core.WSConnmgr` methods,
  `core.Module.ID`; `ctx.ErrFunc`, `ctx.BindFile`, `ctx.BindStruct`, the `ctx.Binder` methods and the six
  `BindValue` methods (documented, never called by the framework); `common.GetFuncName`,
  `common.ToWSEventName`, the `common.Token*` constants and the operation maps; `routing.Router.Use`, `For`
  and `Add`.
- Behavior that depends on names or paths, found while reading the uses (design 0017 › Risk): the six
  embedded fields the container finds by field name (`HTTP`, `WS`, `Guard`, `Interceptor`,
  `ExceptionFilter`, `Middleware`), the layer and constructor methods found by name (`CanActivate`,
  `Intercept`, `Catch`, `Transform`, `NewGuard`, `NewInterceptor`, `NewExceptionFilter`, `NewMiddleware`),
  the dynamic-module test on the printed type `*core.Module`, and two injection keys written as string
  literals of the `common` import path (`loggerKey`, `publisherKey`).

## `ginject (root)`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `Aggregation` | alias | `sample/` | user API | stays |
| `App` | alias | nowhere | user API | stays |
| `Body` | alias | docs: README | user API | stays |
| `Exception` | alias | `sample/` | user API | removed (write *exception.Exception) |
| `FieldLevel` | alias | nowhere | user API | stays |
| `File` | alias | nowhere | user API | stays |
| `Form` | alias | nowhere | user API | stays |
| `HTTPContext` | alias | `sample/`; docs: README | user API | stays |
| `Header` | alias | `sample/` | user API | stays |
| `Map` | alias | `sample/`; docs: README | user API | stays |
| `Next` | alias | `sample/` | user API | stays |
| `Param` | alias | `sample/`; docs: README | user API | stays |
| `Publisher` | alias | nowhere | user API | stays |
| `Query` | alias | `sample/` | user API | stays |
| `Redirect` | alias | nowhere | user API | stays |
| `Request` | alias | nowhere | user API | removed (write *http.Request) |
| `Response` | alias | nowhere | user API | removed (write http.ResponseWriter) |
| `Router` | alias | nowhere | user API | removed (no API returns a router) |
| `Versioning` | alias | nowhere | user API | stays; renamed VersioningOptions |
| `WSContext` | alias | `sample/` | user API | stays |
| `WSPayload` | alias | nowhere | user API | stays |

## `accesslog`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `AccessLog` | struct | `core`; docs: .ginject | framework internal | moves to internal/accesslog |
| `AccessLog.Stop` | method | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/accesslog |
| `AccessLogConfig` | struct | `core`; docs: 1 package README | framework internal | moves to internal/accesslog; renamed AccessLogOptions |
| `AccessLogConfig.Tracer` | field | `core`; docs: .ginject, 1 package README | framework internal | moves to internal/accesslog |
| `AccessLogConfig.Logger` | field | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/accesslog |
| `NewAccessLog` | func | `core`; docs: 1 package README | framework internal | moves to internal/accesslog |

## `aggregation`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `Aggregation` | struct | `common`, `core`, `ctx`, `ginject (root)`; tests of common, core, ctx; docs: .ginject, CLAUDE.md, README, 3 package READMEs | user API | moves to internal/aggregation, reachable through the root alias |
| `Aggregation.InterceptorData` | field | `common`, `core`; tests of common; docs: .ginject | framework internal | hidden: package function or unexported field of internal/aggregation (design 0017, Hooks) |
| `Aggregation.Name` | field | `common`, `core`; tests of common; docs: .ginject, README | framework internal | hidden: package function or unexported field of internal/aggregation (design 0017, Hooks) |
| `Aggregation.IsMainHandlerCalled` | field | `core`; tests of common | framework internal | hidden: package function or unexported field of internal/aggregation (design 0017, Hooks) |
| `Aggregation.Pipe` | method | `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/aggregation, reachable through the root alias |
| `Aggregation.SetMainData` | method | `core`; docs: 1 package README | framework internal | hidden: package function or unexported field of internal/aggregation (design 0017, Hooks) |
| `Aggregation.Aggregate` | method | `core`; docs: 1 package README | framework internal | hidden: package function or unexported field of internal/aggregation (design 0017, Hooks) |
| `Aggregation.Transform` | method | `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/aggregation, reachable through the root alias |
| `Aggregation.Tap` | method | docs: README, 1 package README | user API | moves to internal/aggregation, reachable through the root alias |
| `Aggregation.Filter` | method | docs: .ginject, README, 1 package README | user API | moves to internal/aggregation, reachable through the root alias |
| `NewAggregation` | func | `common`, `core`; tests of common, ctx; docs: 1 package README | framework internal | moves to internal/aggregation |
| `Operator` | struct | nowhere | user API | moves to internal/aggregation, reachable through the root alias |

## `common`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `ArgumentMetadata` | struct | `core`; `sample/`; tests of core, devtool; docs: README, 2 package READMEs | user API | moves to internal/common, reachable through the root alias |
| `ArgumentMetadata.ContextType` | field | `core`; tests of core; docs: .ginject | user API | moves to internal/common, reachable through the root alias |
| `ArgumentMetadata.ParamType` | field | `core`; tests of core; docs: .ginject | user API | moves to internal/common, reachable through the root alias |
| `AsHTTPExceptionFilter` | func | `core` | framework internal | moves to internal/common |
| `AsHTTPGuard` | func | `core`; docs: 1 package README | framework internal | moves to internal/common |
| `AsHTTPInterceptor` | func | `core` | framework internal | moves to internal/common |
| `AsWSExceptionFilter` | func | `core` | framework internal | moves to internal/common |
| `AsWSGuard` | func | `core` | framework internal | moves to internal/common |
| `AsWSInterceptor` | func | `core` | framework internal | moves to internal/common |
| `BodyPipeable` | interface | `core`, `devtool`; tests of core; docs: README | extension point | moves to internal/common (implemented by shape, never named) |
| `BodyPipeable.Transform` | interface method | `core`; tests of core; docs: .ginject, README, 1 package README | extension point | moves to internal/common (implemented by shape, never named) |
| `BodyPipeableKey` | const | `core`, `devtool`; tests of core | framework internal | moves to internal/common |
| `BuildWSGuardMiddleware` | func | `core`; tests of core; docs: 1 package README | framework internal | moves to internal/common |
| `BuildWSInterceptMiddleware` | func | `core`; tests of core; docs: 1 package README | framework internal | moves to internal/common |
| `CommonItem` | struct | `core` | framework internal | moves to internal/common |
| `CommonItem.Name` | field | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/common |
| `CommonItem.Handler` | field | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/common |
| `CommonItem.MainHandlerName` | field | `core`; docs: 1 package README | framework internal | moves to internal/common |
| `ContextPipeable` | interface | `core`; tests of core; docs: README, 1 package README | extension point | moves to internal/common (implemented by shape, never named) |
| `ContextPipeable.Transform` | interface method | `core`; tests of core; docs: .ginject, README, 1 package README | extension point | moves to internal/common (implemented by shape, never named) |
| `ContextPipeableKey` | const | `core`; tests of core | framework internal | moves to internal/common |
| `ContextTypeHTTP` | const | `core`; tests of core; docs: 1 package README | framework internal | moves to internal/common |
| `ContextTypeWS` | const | `core`; tests of core; docs: 1 package README | framework internal | moves to internal/common |
| `ExceptionFilter` | struct | `core`; `sample/`; tests of core; docs: .ginject, CLAUDE.md, README, 1 package README | user API | moves to internal/common, reachable through the root alias |
| `ExceptionFilter.ExceptionFilterHandlers` | field | nowhere | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `ExceptionFilter.BindExceptionFilter` | method | `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/common, reachable through the root alias |
| `ExceptionFilter.InjectProvidersIntoHTTPExceptionFilters` | method | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `ExceptionFilter.InjectProvidersIntoWSExceptionFilters` | method | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `ExceptionFilterItem` | struct | nowhere | framework internal | moves to internal/common |
| `ExceptionFilterItem.HTTP` | field | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/common |
| `ExceptionFilterItem.WS` | field | `core`; docs: .ginject, README | framework internal | moves to internal/common |
| `ExceptionFilterMethodName` | const | nowhere | framework internal | moves to internal/common |
| `ExceptionFilterShapeError` | func | `core` | framework internal | moves to internal/common |
| `ExceptionFilterable` | interface | `core`, `devtool`; docs: .ginject, 2 package READMEs | framework internal | merged into any |
| `FilePipeable` | interface | `core`, `devtool`; tests of core; docs: README | extension point | moves to internal/common (implemented by shape, never named) |
| `FilePipeable.Transform` | interface method | `core`; tests of core; docs: .ginject, README, 1 package README | extension point | moves to internal/common (implemented by shape, never named) |
| `FilePipeableKey` | const | `core`, `devtool`; tests of core | framework internal | moves to internal/common |
| `FormPipeable` | interface | `core`, `devtool`; tests of core; docs: README | extension point | moves to internal/common (implemented by shape, never named) |
| `FormPipeable.Transform` | interface method | `core`; tests of core; docs: .ginject, README, 1 package README | extension point | moves to internal/common (implemented by shape, never named) |
| `FormPipeableKey` | const | `core`, `devtool`; tests of core | framework internal | moves to internal/common |
| `GetFuncName` | func | docs: 1 package README | framework internal | moves to internal/common |
| `Guard` | struct | `core`; `sample/`; tests of core; docs: .ginject, CLAUDE.md, README, 2 package READMEs | user API | moves to internal/common, reachable through the root alias |
| `Guard.GuardHandlers` | field | nowhere | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `Guard.BindGuard` | method | `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/common, reachable through the root alias |
| `Guard.InjectProvidersIntoHTTPGuards` | method | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `Guard.InjectProvidersIntoWSGuards` | method | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `GuardItem` | struct | nowhere | framework internal | moves to internal/common |
| `GuardItem.HTTP` | field | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/common |
| `GuardItem.WS` | field | `core`; docs: .ginject, README | framework internal | moves to internal/common |
| `GuardMethodName` | const | nowhere | framework internal | moves to internal/common |
| `GuardShapeError` | func | `core`; docs: .ginject | framework internal | moves to internal/common |
| `Guarder` | interface | `core`, `devtool`, `guards/throttler`; tests of devtool; docs: .ginject, CLAUDE.md, 3 package READMEs | framework internal | merged into any |
| `HTTP` | struct | `core`; `sample/`; tests of core, middlewares/* (8); docs: .ginject, CLAUDE.md, README, 9 package READMEs | user API | moves to internal/common, reachable through the root alias |
| `HTTP.PatternToFuncNameMap` | field | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `HTTP.FuncNameToPatternMap` | field | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `HTTP.RouterMap` | field | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `HTTP.GetPrefixes` | method | docs: 1 package README | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `HTTP.Prefix` | method | docs: README, 1 package README | user API | moves to internal/common, reachable through the root alias |
| `HTTP.AddHandlerToRouterMap` | method | `core`; docs: 1 package README | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `HTTPCanActivate` | alias | `core` | framework internal | moves to internal/common |
| `HTTPCatch` | alias | `core` | framework internal | moves to internal/common |
| `HTTPExceptionFilterItem` | struct | nowhere | framework internal | moves to internal/common |
| `HTTPExceptionFilterItem.Method` | field | `core`; docs: .ginject, README | framework internal | moves to internal/common |
| `HTTPExceptionFilterItem.Route` | field | `core` | framework internal | moves to internal/common |
| `HTTPExceptionFilterItem.Version` | field | `core`; docs: README | framework internal | moves to internal/common |
| `HTTPExceptionFilterItem.Pattern` | field | `core`; docs: .ginject | framework internal | moves to internal/common |
| `HTTPExceptionFilterItem.Common` | field | `core` | framework internal | moves to internal/common |
| `HTTPGuardItem` | struct | nowhere | framework internal | moves to internal/common |
| `HTTPGuardItem.Method` | field | `core`; docs: .ginject, README | framework internal | moves to internal/common |
| `HTTPGuardItem.Route` | field | `core` | framework internal | moves to internal/common |
| `HTTPGuardItem.Version` | field | `core`; docs: README | framework internal | moves to internal/common |
| `HTTPGuardItem.Pattern` | field | `core`; docs: .ginject | framework internal | moves to internal/common |
| `HTTPGuardItem.Common` | field | `core` | framework internal | moves to internal/common |
| `HTTPIntercept` | alias | `core` | framework internal | moves to internal/common |
| `HTTPInterceptorItem` | struct | nowhere | framework internal | moves to internal/common |
| `HTTPInterceptorItem.Method` | field | `core`; docs: .ginject, README | framework internal | moves to internal/common |
| `HTTPInterceptorItem.Route` | field | `core` | framework internal | moves to internal/common |
| `HTTPInterceptorItem.Version` | field | `core`; docs: README | framework internal | moves to internal/common |
| `HTTPInterceptorItem.Pattern` | field | `core`; docs: .ginject | framework internal | moves to internal/common |
| `HTTPInterceptorItem.Common` | field | `core` | framework internal | moves to internal/common |
| `HTTPLayer` | struct | `core`, `devtool`; tests of devtool; docs: 1 package README | framework internal | moves to internal/common |
| `HTTPLayer.Handler` | field | `core`, `devtool`; tests of devtool; docs: .ginject, README, 1 package README | framework internal | moves to internal/common |
| `HTTPLayer.ControllerPath` | field | `core`, `devtool`; tests of devtool | framework internal | moves to internal/common |
| `HTTPLayer.Name` | field | `core`, `devtool`; tests of devtool; docs: .ginject, README, 1 package README | framework internal | moves to internal/common |
| `HTTPLayer.Route` | field | `core`, `devtool`; tests of devtool | framework internal | moves to internal/common |
| `HTTPLayer.Version` | field | `core`, `devtool`; tests of devtool; docs: README | framework internal | moves to internal/common |
| `HTTPLayer.Method` | field | `core`, `devtool`; tests of devtool; docs: .ginject, README | framework internal | moves to internal/common |
| `HTTPLayer.Pattern` | field | `core`, `devtool`; tests of devtool; docs: .ginject | framework internal | moves to internal/common |
| `HTTPLayer.MainHandlerName` | field | `core`; docs: 1 package README | framework internal | moves to internal/common |
| `HTTPMiddlewareItem` | struct | nowhere | framework internal | moves to internal/common |
| `HTTPMiddlewareItem.Method` | field | `core`; docs: .ginject, README | framework internal | moves to internal/common |
| `HTTPMiddlewareItem.Route` | field | `core` | framework internal | moves to internal/common |
| `HTTPMiddlewareItem.Version` | field | `core`; docs: README | framework internal | moves to internal/common |
| `HTTPMiddlewareItem.Pattern` | field | `core`; docs: .ginject | framework internal | moves to internal/common |
| `HTTPMiddlewareItem.Common` | field | `core` | framework internal | moves to internal/common |
| `HTTPOperations` | var | nowhere | framework internal | moves to internal/common |
| `HTTPTokenMap` | var | nowhere | framework internal | moves to internal/common |
| `HeaderPipeable` | interface | `core`, `devtool`; tests of core; docs: README | extension point | moves to internal/common (implemented by shape, never named) |
| `HeaderPipeable.Transform` | interface method | `core`; tests of core; docs: .ginject, README, 1 package README | extension point | moves to internal/common (implemented by shape, never named) |
| `HeaderPipeableKey` | const | `core`, `devtool`; tests of core | framework internal | moves to internal/common |
| `Interceptable` | interface | `core`, `devtool`; docs: .ginject, 2 package READMEs | framework internal | merged into any |
| `Interceptor` | struct | `core`; `sample/`; tests of core; docs: .ginject, CLAUDE.md, README, 2 package READMEs | user API | moves to internal/common, reachable through the root alias |
| `Interceptor.InterceptorHandlers` | field | nowhere | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `Interceptor.InjectProvidersIntoHTTPInterceptors` | method | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `Interceptor.BindInterceptor` | method | `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/common, reachable through the root alias |
| `Interceptor.InjectProvidersIntoWSInterceptors` | method | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `InterceptorItem` | struct | nowhere | framework internal | moves to internal/common |
| `InterceptorItem.HTTP` | field | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/common |
| `InterceptorItem.WS` | field | `core`; docs: .ginject, README | framework internal | moves to internal/common |
| `InterceptorMethodName` | const | nowhere | framework internal | moves to internal/common |
| `InterceptorShapeError` | func | `core` | framework internal | moves to internal/common |
| `LogAdapter` | interface | `accesslog`, `core`, `log`; tests of accesslog, core, log; docs: .ginject, README, 3 package READMEs | extension point | moves to package log |
| `LogAdapter.Debug` | interface method | `log`; tests of log | extension point | moves to package log |
| `LogAdapter.Error` | interface method | `core`, `log`; tests of log; docs: .ginject, README, 1 package README | extension point | moves to package log |
| `LogAdapter.Fatal` | interface method | `log`; `sample/`; tests of log; docs: .ginject, README | extension point | moves to package log |
| `LogAdapter.Info` | interface method | `accesslog`, `core`, `log`; tests of accesslog, core, log; docs: .ginject, README, 1 package README | extension point | moves to package log |
| `LogAdapter.Warn` | interface method | `core`, `log`; tests of log | extension point | moves to package log |
| `Logger` | interface | `core`, `log`; `sample/`; tests of core, log; docs: .ginject, README, 3 package READMEs | extension point | moves to package log |
| `Logger.From` | interface method | `core`; tests of core, log; docs: .ginject, README, 1 package README | extension point | moves to package log |
| `Middleware` | struct | `core`; `sample/`; tests of core; docs: .ginject, CLAUDE.md, README | user API | moves to internal/common, reachable through the root alias |
| `Middleware.MiddlewareHandlers` | field | nowhere | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `Middleware.BindMiddleware` | method | `sample/`; tests of core; docs: .ginject, README | user API | moves to internal/common, reachable through the root alias |
| `Middleware.InjectProvidersIntoHTTPMiddlewares` | method | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `MiddlewareFn` | interface | `core`, `devtool`, middlewares/* (8); `sample/`; tests of core, middlewares/* (7); docs: .ginject, CLAUDE.md, 10 package READMEs | extension point | moves to internal/common, reachable through the root alias |
| `MiddlewareFn.Use` | interface method | `core`, middlewares/* (8); tests of middlewares/* (8); docs: .ginject | extension point | moves to internal/common, reachable through the root alias |
| `MiddlewareItem` | struct | nowhere | framework internal | moves to internal/common |
| `MiddlewareItem.HTTP` | field | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/common |
| `NormalizeRecovered` | func | `core`; docs: .ginject, 1 package README | framework internal | moves to internal/common |
| `ParamPipeable` | interface | `core`, `devtool`; tests of core; docs: README | extension point | moves to internal/common (implemented by shape, never named) |
| `ParamPipeable.Transform` | interface method | `core`; tests of core; docs: .ginject, README, 1 package README | extension point | moves to internal/common (implemented by shape, never named) |
| `ParamPipeableKey` | const | `core`, `devtool`; tests of core | framework internal | moves to internal/common |
| `ParseFuncNameToURL` | func | docs: 1 package README | framework internal | moves to internal/common |
| `ParseWSFuncNameToEvent` | func | `core`; docs: 1 package README | framework internal | moves to internal/common |
| `Prefix` | struct | nowhere | framework internal | moves to internal/common |
| `Prefix.Value` | field | docs: .ginject, 1 package README | framework internal | moves to internal/common |
| `Prefix.Handlers` | field | docs: .ginject | framework internal | moves to internal/common |
| `Publisher` | interface | `core`, `ginject (root)`; tests of core; docs: .ginject, CLAUDE.md, README, 1 package README | user API | moves to internal/common, reachable through the root alias |
| `Publisher.Publish` | interface method | tests of core; docs: .ginject, README | user API | moves to internal/common, reachable through the root alias |
| `QueryPipeable` | interface | `core`, `devtool`; tests of core; docs: README | extension point | moves to internal/common (implemented by shape, never named) |
| `QueryPipeable.Transform` | interface method | `core`; tests of core; docs: .ginject, README, 1 package README | extension point | moves to internal/common (implemented by shape, never named) |
| `QueryPipeableKey` | const | `core`, `devtool`; tests of core; docs: 1 package README | framework internal | moves to internal/common |
| `RunWSCatchChain` | func | `core`; docs: .ginject, 1 package README | framework internal | moves to internal/common |
| `ToWSEventName` | func | docs: 1 package README | framework internal | moves to internal/common |
| `TokenAnd` | const | nowhere | framework internal | moves to internal/common |
| `TokenAny` | const | nowhere | framework internal | moves to internal/common |
| `TokenBy` | const | nowhere | framework internal | moves to internal/common |
| `TokenFile` | const | nowhere | framework internal | moves to internal/common |
| `TokenOf` | const | nowhere | framework internal | moves to internal/common |
| `TokenVersion` | const | nowhere | framework internal | moves to internal/common |
| `Use` | alias | `core` | framework internal | moves to internal/common |
| `WS` | struct | `core`; `sample/`; tests of core; docs: .ginject, CLAUDE.md, README, 1 package README | user API | moves to internal/common, reachable through the root alias |
| `WS.EventMap` | field | `core` | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `WS.AddHandlerToEventMap` | method | `core`; docs: 1 package README | framework internal | hidden: package function or unexported field of internal/common (design 0017, Hooks) |
| `WSCanActivate` | alias | `core` | framework internal | moves to internal/common |
| `WSCatch` | alias | `core` | framework internal | moves to internal/common |
| `WSExceptionFilterItem` | struct | nowhere | framework internal | moves to internal/common |
| `WSExceptionFilterItem.EventName` | field | `core` | framework internal | moves to internal/common |
| `WSExceptionFilterItem.Common` | field | `core` | framework internal | moves to internal/common |
| `WSGuardItem` | struct | nowhere | framework internal | moves to internal/common |
| `WSGuardItem.EventName` | field | `core` | framework internal | moves to internal/common |
| `WSGuardItem.Common` | field | `core` | framework internal | moves to internal/common |
| `WSIntercept` | alias | `core`; tests of core | framework internal | moves to internal/common |
| `WSInterceptorItem` | struct | nowhere | framework internal | moves to internal/common |
| `WSInterceptorItem.EventName` | field | `core` | framework internal | moves to internal/common |
| `WSInterceptorItem.Common` | field | `core` | framework internal | moves to internal/common |
| `WSLayer` | struct | `core` | framework internal | moves to internal/common |
| `WSLayer.Handler` | field | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/common |
| `WSLayer.EventName` | field | `core` | framework internal | moves to internal/common |
| `WSLayer.Name` | field | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/common |
| `WSOperations` | var | nowhere | framework internal | moves to internal/common |
| `WSPayloadPipeable` | interface | `core`; tests of core; docs: .ginject, CLAUDE.md, README, 1 package README | extension point | moves to internal/common (implemented by shape, never named) |
| `WSPayloadPipeable.Transform` | interface method | `core`; tests of core; docs: .ginject, README, 1 package README | extension point | moves to internal/common (implemented by shape, never named) |
| `WSPayloadPipeableKey` | const | `core`; tests of core | framework internal | moves to internal/common |

## `core`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `App` | struct | `ginject (root)`; tests of modules/cache, sample/shop, middlewares/* (7); docs: .ginject | user API | moves to internal/core, reachable through the root package |
| `App.ServeHTTP` | method | tests of sample/benchmarks, sample/shop, middlewares/* (8); docs: .ginject, README | user API | moves to internal/core, reachable through the root package |
| `App.Create` | method | `sample/`; tests of modules/cache, modules/config, sample/benchmarks, sample/shop, middlewares/* (8); docs: .ginject, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `App.BindGlobalGuards` | method | `sample/`; docs: README | user API | moves to internal/core, reachable through the root package |
| `App.BindGlobalInterceptors` | method | `sample/`; docs: README | user API | moves to internal/core, reachable through the root package |
| `App.BindGlobalExceptionFilters` | method | `sample/`; docs: .ginject, README | user API | moves to internal/core, reachable through the root package |
| `App.BindGlobalMiddlewares` | method | `sample/`; tests of middlewares/* (8); docs: .ginject, README | user API | moves to internal/core, reachable through the root package |
| `App.EnableVersioning` | method | docs: .ginject, README | user API | moves to internal/core, reachable through the root package |
| `App.EnableDevtool` | method | nowhere | user API | moves to internal/core, reachable through the root package |
| `App.EnableAccessLog` | method | `sample/`; docs: .ginject, README | user API | moves to internal/core, reachable through the root package |
| `App.EnableWS` | method | `sample/`; tests of middlewares/* (3); docs: .ginject, README | user API | moves to internal/core, reachable through the root package |
| `App.SetLogger` | method | `sample/`; tests of middlewares/* (8); docs: .ginject, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `App.Logger` | method | `sample/`; docs: .ginject, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `App.Get` | method | tests of modules/cache, modules/config; docs: .ginject, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `App.Listen` | method | `sample/`; docs: .ginject, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `App.ListenTLS` | method | docs: .ginject, 1 package README | user API | moves to internal/core, reachable through the root package |
| `App.Serve` | method | docs: .ginject, 1 package README | user API | moves to internal/core, reachable through the root package |
| `App.Stop` | method | docs: .ginject, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `Controller` | interface | `sample/`; tests of middlewares/* (8); docs: .ginject, README, 9 package READMEs | extension point | moves to internal/core, reachable through the root package |
| `Controller.NewController` | interface method | nowhere | extension point | moves to internal/core, reachable through the root package |
| `HTTP` | struct | nowhere | framework internal | moves to internal/core |
| `Module` | struct | `modules/cache`, `modules/config`; `sample/`; tests of modules/cache; docs: .ginject, CLAUDE.md, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `Module.Name` | field | docs: .ginject, README | user API | moves to internal/core, reachable through the root package |
| `Module.IsGlobal` | field | `modules/cache`, `modules/config`; tests of modules/cache, modules/config; docs: .ginject, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `Module.OnInit` | field | `modules/cache`, `modules/config`; tests of modules/cache, modules/config; docs: .ginject | user API | moves to internal/core, reachable through the root package |
| `Module.OnReady` | field | nowhere | user API | moves to internal/core, reachable through the root package |
| `Module.OnShutdown` | field | docs: README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `Module.Prefix` | method | docs: README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `Module.ID` | method | nowhere | framework internal | removed (unused) |
| `ModuleBuilder` | func | `modules/cache`, `modules/config`; `sample/`; tests of modules/cache, sample/benchmarks, sample/shop, middlewares/* (8); docs: .ginject, CLAUDE.md, README, 19 package READMEs | user API | moves to internal/core, reachable through the root package |
| `New` | func | `sample/`; tests of modules/cache, modules/config, sample/benchmarks, sample/shop, middlewares/* (8); docs: .ginject, CLAUDE.md, README, 20 package READMEs | user API | moves to internal/core, reachable through the root package |
| `NewWS` | func | nowhere | framework internal | moves to internal/core |
| `NewWSConnmgr` | func | nowhere | framework internal | moves to internal/core |
| `Provider` | interface | `modules/cache`, `modules/config`; `sample/`; tests of modules/cache; docs: .ginject, README, 3 package READMEs | extension point | moves to internal/core, reachable through the root package |
| `Provider.NewProvider` | interface method | docs: .ginject | extension point | moves to internal/core, reachable through the root package |
| `ServerOptions` | struct | docs: .ginject, CLAUDE.md, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `ServerOptions.ShutdownTimeout` | field | docs: 1 package README | user API | moves to internal/core, reachable through the root package |
| `ServerOptions.MaxRequestBodyBytes` | field | docs: .ginject, 1 package README | user API | moves to internal/core, reachable through the root package |
| `ServerOptions.TLSConfig` | field | docs: .ginject, 1 package README | user API | moves to internal/core, reachable through the root package |
| `ServerOptions.H2C` | field | docs: 1 package README | user API | moves to internal/core, reachable through the root package; renamed IsH2C |
| `TypeAck` | const | nowhere | framework internal | moves to internal/core |
| `TypeConnected` | const | nowhere | framework internal | moves to internal/core |
| `TypeError` | const | nowhere | framework internal | moves to internal/core |
| `TypeEvent` | const | nowhere | framework internal | moves to internal/core |
| `TypePing` | const | nowhere | framework internal | moves to internal/core |
| `TypePong` | const | nowhere | framework internal | moves to internal/core |
| `TypePublish` | const | nowhere | framework internal | moves to internal/core |
| `TypeSubscribe` | const | nowhere | framework internal | moves to internal/core |
| `TypeUnsubscribe` | const | nowhere | framework internal | moves to internal/core |
| `WS` | struct | nowhere | framework internal | moves to internal/core |
| `WSConfig` | struct | `sample/`; tests of middlewares/* (3); docs: .ginject, CLAUDE.md, README, 2 package READMEs | user API | moves to internal/core, reachable through the root package; renamed ginject.WSOptions (core.WSOptions) |
| `WSConfig.Path` | field | `sample/`; tests of middlewares/* (3); docs: .ginject, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `WSConnection` | struct | nowhere | framework internal | moves to internal/core |
| `WSConnection.ID` | field | nowhere | framework internal | moves to internal/core |
| `WSConnection.Conn` | field | docs: .ginject, 1 package README | framework internal | moves to internal/core |
| `WSConnection.CreatedAt` | field | nowhere | framework internal | moves to internal/core |
| `WSConnection.LastSeen` | field | docs: .ginject | framework internal | moves to internal/core |
| `WSConnection.TrySend` | method | docs: .ginject | framework internal | moves to internal/core |
| `WSConnmgr` | struct | nowhere | framework internal | moves to internal/core |
| `WSConnmgr.Broker` | field | docs: .ginject | framework internal | moves to internal/core |
| `WSConnmgr.Register` | method | docs: .ginject, README, 1 package README | framework internal | moves to internal/core |
| `WSConnmgr.Unregister` | method | nowhere | framework internal | moves to internal/core |
| `WSConnmgr.Get` | method | docs: .ginject, README, 1 package README | framework internal | moves to internal/core |
| `WSConnmgr.Subscribe` | method | docs: .ginject | framework internal | moves to internal/core |
| `WSConnmgr.Unsubscribe` | method | docs: .ginject | framework internal | moves to internal/core |
| `WSPayload` | struct | nowhere | framework internal | moves to internal/core |
| `WSPayload.Message` | field | docs: .ginject, README | framework internal | moves to internal/core |
| `WSPayload.Type` | field | docs: .ginject, README, 1 package README | framework internal | moves to internal/core |
| `WSPayload.ID` | field | nowhere | framework internal | moves to internal/core |
| `WSPayload.Topic` | field | docs: .ginject | framework internal | moves to internal/core |
| `WSPayloadType` | type | nowhere | framework internal | moves to internal/core |
| `moduleBuilder.Imports` | method (via moduleBuilder) | `sample/`; tests of modules/cache, sample/benchmarks, sample/shop; docs: .ginject | user API | moves to internal/core, reachable through the root package |
| `moduleBuilder.Providers` | method (via moduleBuilder) | `modules/cache`, `modules/config`; `sample/`; tests of modules/cache; docs: .ginject, 1 package README | user API | moves to internal/core, reachable through the root package |
| `moduleBuilder.Controllers` | method (via moduleBuilder) | `sample/`; tests of middlewares/* (8); docs: .ginject, README, 1 package README | user API | moves to internal/core, reachable through the root package |
| `moduleBuilder.Build` | method (via moduleBuilder) | `modules/cache`, `modules/config`; `sample/`; tests of modules/cache, sample/benchmarks, sample/shop, middlewares/* (8); docs: .ginject, README, 1 package README | user API | moves to internal/core, reachable through the root package |

## `ctx`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `BindFile` | func | nowhere | framework internal | moves to internal/ctx |
| `BindStrArr` | func | `modules/config` | framework internal | moves to internal/ctx |
| `BindStruct` | func | nowhere | framework internal | moves to internal/ctx |
| `Binder` | struct | nowhere | framework internal | moves to internal/ctx |
| `Binder.StrArr` | method | docs: .ginject, 1 package README | framework internal | moves to internal/ctx |
| `Binder.Header` | method | docs: .ginject, README | framework internal | moves to internal/ctx |
| `Binder.Struct` | method | docs: .ginject, 1 package README | framework internal | moves to internal/ctx |
| `Binder.File` | method | docs: .ginject, README, 1 package README | framework internal | moves to internal/ctx |
| `BinderOf` | func | `core`; docs: .ginject, 2 package READMEs | framework internal | moves to internal/ctx |
| `Body` | type | `common`, `core`, `ginject (root)`; `sample/`; tests of common, core, devtool; docs: .ginject, CLAUDE.md, README, 3 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `Body.Set` | method | docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Body.Get` | method | docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Body.Del` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `Body.Has` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Body.Bind` | method | `sample/`; docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Body.BindValue` | method | docs: README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Context` | interface | `core`; docs: .ginject | framework internal | moves to internal/ctx |
| `Context.Context` | interface method | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/ctx |
| `Context.GetID` | interface method | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/ctx |
| `Context.Reset` | interface method | docs: .ginject, 1 package README | framework internal | moves to internal/ctx |
| `DataFile` | struct | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `DataFile.FileHeader` | embedded field | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `DataFile.Index` | field | docs: .ginject | user API | moves to internal/ctx, reachable through the root alias |
| `DataFile.Size` | field | docs: .ginject | user API | moves to internal/ctx, reachable through the root alias |
| `DataFile.Total` | field | docs: .ginject | user API | moves to internal/ctx, reachable through the root alias |
| `DataFile.Key` | field | docs: README | user API | moves to internal/ctx, reachable through the root alias |
| `DataFile.Filename` | field | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `DataFile.Type` | field | docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `DataFile.Dest` | field | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `ErrBindTarget` | var | docs: .ginject | framework internal | moves to internal/ctx |
| `ErrFunc` | func type | nowhere | framework internal | removed (unused) |
| `FieldLevel` | struct | `ginject (root)`, `modules/config`; docs: .ginject, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `FieldLevel.Tag` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `FieldLevel.NestedTag` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `FieldLevel.Namespace` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `FieldLevel.Field` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `FieldLevel.Index` | method | docs: .ginject | user API | moves to internal/ctx, reachable through the root alias |
| `FieldLevel.Value` | method | docs: .ginject, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `FieldLevel.IsValue` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `FieldLevel.Kind` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `FieldLevel.Type` | method | docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `File` | type | `common`, `core`, `ginject (root)`; tests of common, core; docs: .ginject, CLAUDE.md, README, 3 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `File.Bind` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Form` | type | `common`, `core`, `ginject (root)`; tests of common, core; docs: .ginject, CLAUDE.md, README, 2 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `Form.Get` | method | docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Form.Set` | method | docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Form.Add` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `Form.Del` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `Form.Has` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Form.Bind` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Form.BindValue` | method | docs: README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `GetTagParamIndex` | func | `devtool` | framework internal | moves to internal/ctx |
| `GetTagParams` | func | `devtool` | framework internal | moves to internal/ctx |
| `HTTPContext` | struct | `common`, `core`, `ginject (root)`, `guards/throttler`, `versioning`; `sample/`; tests of common, core, devtool, guards/throttler, middlewares/proxy, routing, versioning; docs: .ginject, CLAUDE.md, README, 10 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.Request` | embedded field | `core`, `guards/throttler`; `sample/`; tests of core, guards/throttler, middlewares/proxy, versioning; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.ResponseWriter` | embedded field | `core`, `guards/throttler`; tests of core, guards/throttler; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.ParamKeys` | field | `core`; tests of core; docs: .ginject | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `HTTPContext.ParamValues` | field | `core`; tests of core; docs: .ginject | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `HTTPContext.Next` | field | `core`; tests of core; docs: .ginject, README | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `HTTPContext.Code` | field | `core`; tests of core; docs: .ginject, README | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `HTTPContext.Timestamp` | field | `core` | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `HTTPContext.Body` | method | `core`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.Init` | method | `core`; tests of core; docs: .ginject, 1 package README | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `HTTPContext.GetID` | method | `core`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias; renamed ID |
| `HTTPContext.Reset` | method | `core`; docs: .ginject, 1 package README | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `HTTPContext.AddAggregation` | method | `core`; docs: .ginject | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `HTTPContext.Aggregations` | method | `core`; docs: .ginject | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `HTTPContext.Status` | method | `core`; `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.Text` | method | `core`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.JSON` | method | `core`; `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.JSONP` | method | docs: .ginject | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.Redirect` | method | `core`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.File` | method | `core`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.Form` | method | `core`; tests of core; docs: .ginject, README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.Header` | method | `core`, `versioning`; `sample/`; tests of core; docs: .ginject, README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.Param` | method | `core`; tests of core; docs: .ginject, README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPContext.Query` | method | `core`, `versioning`; `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `HTTPHandler` | alias | `core`, `routing`; tests of core, routing; docs: .ginject, 1 package README | framework internal | moves to internal/ctx |
| `Header` | type | `common`, `core`, `ginject (root)`; tests of common, core; docs: .ginject, CLAUDE.md, README, 2 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `Header.Get` | method | `versioning`; `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Header.Set` | method | docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Header.Add` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `Header.Del` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `Header.Has` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Header.Bind` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Header.BindValue` | method | docs: README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `JSONAppender` | interface | docs: .ginject, 1 package README | framework internal | moves to internal/ctx |
| `JSONAppender.AppendJSON` | interface method | nowhere | framework internal | moves to internal/ctx |
| `Map` | type | `core`, `ginject (root)`; `sample/`; tests of core; docs: .ginject, README, 3 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `NewHTTPContext` | func | `core`; tests of common, core, guards/throttler, routing, versioning; docs: 1 package README | framework internal | moves to internal/ctx |
| `NewWSContext` | func | `core`; tests of common, core; docs: .ginject | framework internal | moves to internal/ctx |
| `Next` | alias | `common`, `core`, `ginject (root)`, middlewares/* (8); tests of common, core, middlewares/* (7); docs: .ginject, CLAUDE.md, README, 11 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `Param` | type | `common`, `core`, `ginject (root)`; tests of common, core; docs: .ginject, CLAUDE.md, README, 3 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `Param.Get` | method | `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Param.Set` | method | docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Param.Add` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `Param.Del` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `Param.Has` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Param.Bind` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Param.BindValue` | method | docs: README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Query` | type | `common`, `core`, `ginject (root)`; `sample/`; tests of common, core; docs: .ginject, CLAUDE.md, README, 3 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `Query.Get` | method | `versioning`; `sample/`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Query.Set` | method | docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Query.Add` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `Query.Del` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `Query.Has` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Query.Bind` | method | `sample/`; docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Query.BindValue` | method | docs: README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `Redirect` | alias | `core`, `ginject (root)`; tests of core; docs: .ginject, CLAUDE.md, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `RequestID` | const | `core`; tests of core | framework internal | moves to internal/ctx |
| `TagBind` | const | `devtool` | framework internal | moves to internal/ctx |
| `WSContext` | struct | `common`, `core`, `ginject (root)`; tests of common, core, wsevent; docs: .ginject, CLAUDE.md, README, 2 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `WSContext.Conn` | embedded field | `core`; `sample/`; tests of core; docs: .ginject, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `WSContext.Next` | field | `common`, `core`; tests of common, core; docs: .ginject, README | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `WSContext.Timestamp` | field | `core`; tests of core | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `WSContext.Init` | method | `core`; tests of core; docs: .ginject, 1 package README | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `WSContext.GetID` | method | `core`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias; renamed ID |
| `WSContext.Reset` | method | `core`; tests of common, core; docs: .ginject, 1 package README | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `WSContext.AddAggregation` | method | `common`; docs: .ginject | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `WSContext.Aggregations` | method | `core`; tests of common; docs: .ginject | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `WSContext.SetSend` | method | `core`; tests of core | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `WSContext.Send` | method | `core`; `sample/`; tests of core; docs: .ginject | user API | moves to internal/ctx, reachable through the root alias |
| `WSContext.WSPayload` | method | `core`; tests of core; docs: .ginject, README | user API | moves to internal/ctx, reachable through the root alias |
| `WSContext.SetWSPayload` | method | `core`; tests of core | framework internal | hidden: package function or unexported field of internal/ctx (design 0017, Hooks) |
| `WSContext.Context` | method | `core`; tests of core; docs: .ginject, README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `WSContext.SetContext` | method | nowhere | user API | moves to internal/ctx, reachable through the root alias |
| `WSHandler` | alias | `common`, `core`, `wsevent`; docs: .ginject, 1 package README | framework internal | moves to internal/ctx |
| `WSPayload` | type | `common`, `core`, `ginject (root)`; tests of common, core; docs: .ginject, CLAUDE.md, README, 2 package READMEs | user API | moves to internal/ctx, reachable through the root alias |
| `WSPayload.Bind` | method | docs: 1 package README | user API | moves to internal/ctx, reachable through the root alias |
| `WSPayload.BindValue` | method | docs: README, 1 package README | user API | moves to internal/ctx, reachable through the root alias |

## `devtool`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `Controller` | struct | docs: 1 package README | framework internal | moves to internal/devtool |
| `Controller.Http` | field | tests of core; docs: .ginject, 1 package README | framework internal | moves to internal/devtool |
| `Devtool` | struct | `core` | framework internal | moves to internal/devtool |
| `Devtool.GetConfigurationResponse` | embedded field | nowhere | framework internal | moves to internal/devtool |
| `Devtool.GetConfiguration` | method | nowhere | framework internal | moves to internal/devtool |
| `DevtoolBuilder` | func | `core`; docs: 1 package README | framework internal | moves to internal/devtool |
| `GenerateSchema` | func | nowhere | framework internal | moves to internal/devtool |
| `GetConfigurationRequest` | struct | nowhere | framework internal | moves to internal/devtool |
| `GetConfigurationResponse` | struct | nowhere | framework internal | moves to internal/devtool |
| `GetConfigurationResponse.Controller` | field | tests of core; docs: .ginject, README, 1 package README | framework internal | moves to internal/devtool |
| `HTTPComponent` | struct | nowhere | framework internal | moves to internal/devtool |
| `HTTPComponent.Id` | field | nowhere | framework internal | moves to internal/devtool |
| `HTTPComponent.Handler` | field | docs: .ginject, README | framework internal | moves to internal/devtool |
| `HTTPComponent.HttpMethod` | field | nowhere | framework internal | moves to internal/devtool |
| `HTTPComponent.Route` | field | nowhere | framework internal | moves to internal/devtool |
| `HTTPComponent.ExceptionFilters` | field | nowhere | framework internal | moves to internal/devtool |
| `HTTPComponent.Middlewares` | field | nowhere | framework internal | moves to internal/devtool |
| `HTTPComponent.Guards` | field | nowhere | framework internal | moves to internal/devtool |
| `HTTPComponent.Interceptors` | field | nowhere | framework internal | moves to internal/devtool |
| `HTTPComponent.Versioning` | field | tests of core; docs: .ginject, README, 1 package README | framework internal | moves to internal/devtool |
| `HTTPComponent.Request` | field | docs: .ginject, README | framework internal | moves to internal/devtool |
| `HTTPRequest` | struct | nowhere | framework internal | moves to internal/devtool |
| `HTTPRequest.Body` | field | docs: .ginject, README, 1 package README | framework internal | moves to internal/devtool |
| `HTTPRequest.Form` | field | docs: .ginject, README | framework internal | moves to internal/devtool |
| `HTTPRequest.Query` | field | docs: .ginject, README | framework internal | moves to internal/devtool |
| `HTTPRequest.Header` | field | docs: .ginject, README | framework internal | moves to internal/devtool |
| `HTTPRequest.Param` | field | docs: .ginject, README | framework internal | moves to internal/devtool |
| `HTTPRequest.File` | field | docs: .ginject, README | framework internal | moves to internal/devtool |
| `HTTPVersioning` | struct | nowhere | framework internal | moves to internal/devtool |
| `HTTPVersioning.Type` | field | tests of core; docs: .ginject, README, 1 package README | framework internal | moves to internal/devtool |
| `HTTPVersioning.Value` | field | tests of core; docs: .ginject, 1 package README | framework internal | moves to internal/devtool |
| `HTTPVersioning.Key` | field | tests of core; docs: README, 1 package README | framework internal | moves to internal/devtool |
| `Layer` | struct | nowhere | framework internal | moves to internal/devtool |
| `Layer.Scope` | field | docs: .ginject | framework internal | moves to internal/devtool |
| `Layer.Name` | field | docs: .ginject, README | framework internal | moves to internal/devtool |
| `LayerScope` | type | nowhere | framework internal | moves to internal/devtool |
| `LayerScope.String` | method | docs: .ginject, README | framework internal | moves to internal/devtool |
| `LayerScope_GLOBAL_SCOPE` | const | nowhere | framework internal | moves to internal/devtool |
| `LayerScope_REQUEST_SCOPE` | const | nowhere | framework internal | moves to internal/devtool |
| `LayerScope_UNKNOWN` | const | nowhere | framework internal | moves to internal/devtool |
| `LayerScope_name` | var | nowhere | framework internal | moves to internal/devtool |
| `LayerScope_value` | var | nowhere | framework internal | moves to internal/devtool |
| `Schema` | struct | nowhere | framework internal | moves to internal/devtool |
| `Schema.Name` | field | docs: .ginject, README | framework internal | moves to internal/devtool |
| `Schema.Type` | field | docs: .ginject, README, 1 package README | framework internal | moves to internal/devtool |
| `Schema.Format` | field | nowhere | framework internal | moves to internal/devtool |
| `Schema.Item` | field | nowhere | framework internal | moves to internal/devtool |
| `Schema.Properties` | field | nowhere | framework internal | moves to internal/devtool |
| `devtoolBuilder.AddExceptionFilters` | method (via devtoolBuilder) | `core` | framework internal | moves to internal/devtool |
| `devtoolBuilder.AddMiddlewares` | method (via devtoolBuilder) | `core` | framework internal | moves to internal/devtool |
| `devtoolBuilder.AddGuarders` | method (via devtoolBuilder) | `core` | framework internal | moves to internal/devtool |
| `devtoolBuilder.AddInterceptors` | method (via devtoolBuilder) | `core` | framework internal | moves to internal/devtool |
| `devtoolBuilder.AddVersioning` | method (via devtoolBuilder) | `core` | framework internal | moves to internal/devtool |
| `devtoolBuilder.AddHTTPMainHandlers` | method (via devtoolBuilder) | `core` | framework internal | moves to internal/devtool |
| `devtoolBuilder.Build` | method (via devtoolBuilder) | `core`; docs: .ginject, README, 1 package README | framework internal | moves to internal/devtool |

## `exception`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `BadGatewayException` | func | docs: README | user API | stays |
| `BadRequestException` | func | `core`, `ctx`, `middlewares/hostcheck`, `middlewares/httpsredirect`; `sample/`; tests of core, middlewares/compress; docs: .ginject, README, 1 package README | user API | stays |
| `ConflictException` | func | `sample/`; tests of core | user API | stays |
| `Exception` | struct | `common`, `core`, `ctx`, `ginject (root)`; tests of common, core, ctx, guards/throttler, middlewares/loadshed; docs: .ginject, CLAUDE.md, README, 3 package READMEs | user API | stays |
| `Exception.Error` | method | `core`; `sample/`; tests of common; docs: .ginject, README, 1 package README | user API | stays |
| `Exception.Unwrap` | method | docs: 1 package README | user API | stays |
| `Exception.GetCode` | method | `core`; `sample/`; tests of common, core, ctx, guards/throttler, middlewares/loadshed; docs: .ginject, README, 1 package README | user API | stays; renamed Code |
| `Exception.GetMessage` | method | `core`; `sample/`; tests of common, core, ctx, middlewares/loadshed; docs: .ginject, README, 1 package README | user API | stays; renamed Message |
| `Exception.GetStatusText` | method | docs: README, 1 package README | user API | stays; renamed StatusText |
| `Exception.AppendJSON` | method | `core` | user API | stays |
| `Exception.MarshalJSON` | method | nowhere | user API | stays |
| `ExceptionOptions` | struct | tests of core; docs: README, 1 package README | user API | stays |
| `ExceptionOptions.Description` | field | tests of core; docs: README, 1 package README | user API | stays |
| `ExceptionOptions.Cause` | field | docs: README, 1 package README | user API | stays |
| `ForbiddenException` | func | `common`, `core`, `middlewares/cors`, `middlewares/csrf`; tests of common, core, middlewares/loadshed; docs: .ginject, 2 package READMEs | user API | stays |
| `GatewayTimeoutException` | func | nowhere | user API | stays |
| `GoingAwayException` | func | nowhere | user API | stays |
| `GoneException` | func | `core`; tests of core | user API | stays |
| `HTTPVersionNotSupportedException` | func | nowhere | user API | stays |
| `InternalServerErrorException` | func | `common`, `core`, `ctx`; `sample/`; tests of common, core; docs: .ginject, CLAUDE.md, 1 package README | user API | stays |
| `InvalidPayloadException` | func | nowhere | user API | stays |
| `MessageTooBigException` | func | nowhere | user API | stays |
| `MethodNotAllowedException` | func | `core` | user API | stays |
| `MisdirectedRequestException` | func | `middlewares/hostcheck` | user API | stays |
| `NewException` | func | `core`; tests of core; docs: README, 1 package README | user API | stays |
| `NotAcceptableException` | func | nowhere | user API | stays |
| `NotFoundException` | func | `core`; `sample/`; tests of common, core; docs: .ginject, README, 2 package READMEs | user API | stays |
| `NotImplementedException` | func | docs: .ginject | user API | stays |
| `NotSubscribedException` | func | `core` | user API | stays |
| `PolicyViolationException` | func | tests of core | user API | stays |
| `PreconditionFailedException` | func | nowhere | user API | stays |
| `ProtocolErrorException` | func | nowhere | user API | stays |
| `RequestEntityTooLargeException` | func | `ctx`; docs: .ginject | user API | stays |
| `RequestTimeoutException` | func | docs: .ginject | user API | stays |
| `ServiceUnavailableException` | func | `middlewares/loadshed`; tests of core; docs: .ginject | user API | stays |
| `TeapotException` | func | tests of core | user API | stays |
| `TooManyRequestsException` | func | `guards/throttler`; docs: 1 package README | user API | stays |
| `TopicNotFoundException` | func | `core`; docs: 1 package README | user API | stays |
| `UnauthorizedException` | func | `sample/`; docs: .ginject, README, 1 package README | user API | stays |
| `UnprocessableEntityException` | func | nowhere | user API | stays |
| `UnsupportedDataException` | func | nowhere | user API | stays |
| `UnsupportedMediaTypeException` | func | nowhere | user API | stays |
| `WSInternalErrorException` | func | `core` | user API | stays |

## `guards/throttler`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `FixedWindow` | const | nowhere | user API | stays; renamed StrategyFixedWindow |
| `SlidingWindow` | const | nowhere | user API | stays; renamed StrategySlidingWindow |
| `Strategy` | type | nowhere | user API | stays |
| `Throttler` | struct | docs: .ginject, README, 1 package README | user API | stays |
| `Throttler.Limit` | field | docs: README, 1 package README | user API | stays |
| `Throttler.TTL` | field | docs: README, 1 package README | user API | stays |
| `Throttler.Strategy` | field | docs: 1 package README | user API | stays |
| `Throttler.KeyFunc` | field | docs: 1 package README | user API | stays |
| `Throttler.Backend` | field | docs: .ginject, 1 package README | user API | stays |
| `Throttler.NewGuard` | method | nowhere | user API | stays |
| `TokenBucket` | const | docs: 1 package README | user API | stays; renamed StrategyTokenBucket |

## `log`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `DebugLevel` | const | `sample/`; tests of accesslog; docs: 1 package README | user API | stays; renamed LevelDebug |
| `ErrorLevel` | const | nowhere | user API | stays; renamed LevelError |
| `FatalLevel` | const | nowhere | user API | stays; renamed LevelFatal |
| `InfoLevel` | const | docs: 1 package README | user API | stays; renamed LevelInfo |
| `JSONFormat` | const | tests of accesslog; docs: README, 3 package READMEs | user API | stays; renamed FormatJSON |
| `LogOptions` | struct | `sample/`; tests of accesslog; docs: .ginject, README, 3 package READMEs | user API | stays |
| `LogOptions.Level` | field | `sample/`; tests of accesslog; docs: .ginject, 1 package README | user API | stays |
| `LogOptions.LogFormat` | field | `sample/`; tests of accesslog; docs: README, 1 package README | user API | stays; renamed Format |
| `LogOptions.AddSource` | field | nowhere | user API | stays; renamed IsAddSource |
| `LogOptions.MaskFields` | field | docs: README, 1 package README | user API | stays |
| `NewLog` | func | `core`; `sample/`; tests of accesslog, core; docs: .ginject, README, 5 package READMEs | user API | stays |
| `PrettyFormat` | const | `sample/`; tests of accesslog; docs: 1 package README | user API | stays; renamed FormatPretty |
| `TextFormat` | const | tests of accesslog; docs: 1 package README | user API | stays; renamed FormatText |
| `WarnLevel` | const | nowhere | user API | stays; renamed LevelWarn |
| `WrapLogger` | func | `core`; tests of core; docs: .ginject, 3 package READMEs | user API | stays |

## `memorybroker`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `Broker` | interface | `core`; docs: .ginject | framework internal | moves to internal/memorybroker |
| `Broker.Close` | interface method | docs: .ginject, README, 1 package README | framework internal | moves to internal/memorybroker |
| `Broker.Publish` | interface method | `core`; tests of core; docs: .ginject, README, 1 package README | framework internal | moves to internal/memorybroker |
| `Broker.PublishAsync` | interface method | docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `Broker.Subscribe` | interface method | `core`; tests of core; docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `Broker.Unsubscribe` | interface method | `core`; docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `ErrClosed` | var | nowhere | framework internal | moves to internal/memorybroker |
| `ErrEmptyTopic` | var | nowhere | framework internal | moves to internal/memorybroker |
| `ErrForeignSubscription` | var | nowhere | framework internal | moves to internal/memorybroker |
| `ErrNilHandler` | var | nowhere | framework internal | moves to internal/memorybroker |
| `MemoryBroker` | struct | nowhere | framework internal | moves to internal/memorybroker |
| `MemoryBroker.Publish` | method | docs: .ginject, README, 1 package README | framework internal | moves to internal/memorybroker |
| `MemoryBroker.PublishAsync` | method | docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `MemoryBroker.Subscribe` | method | docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `MemoryBroker.Unsubscribe` | method | docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `MemoryBroker.Close` | method | docs: .ginject, README, 1 package README | framework internal | moves to internal/memorybroker |
| `Message` | struct | `core`; tests of core; docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `Message.Topic` | field | `core`; docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `Message.Payload` | field | `core`; docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `Message.Timestamp` | field | nowhere | framework internal | moves to internal/memorybroker |
| `MessageHandler` | func type | `core` | framework internal | moves to internal/memorybroker |
| `NewMemoryBroker` | func | `core`; docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `Subscription` | interface | `core` | framework internal | moves to internal/memorybroker |
| `Subscription.ID` | interface method | nowhere | framework internal | moves to internal/memorybroker |
| `Subscription.Topic` | interface method | `core`; docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |
| `Subscription.Unsubscribe` | interface method | docs: .ginject, 1 package README | framework internal | moves to internal/memorybroker |

## `memorycache`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `ErrEmptyKey` | var | `modules/cache`; tests of modules/cache; docs: 1 package README | framework internal | moves to internal/memorycache (modules/cache re-exports the errors already) |
| `ErrEntryTooLarge` | var | `modules/cache`; tests of modules/cache; docs: 1 package README | framework internal | moves to internal/memorycache (modules/cache re-exports the errors already) |
| `ErrNegativeTTL` | var | `modules/cache`; tests of modules/cache; docs: 1 package README | framework internal | moves to internal/memorycache (modules/cache re-exports the errors already) |
| `ErrNotCounter` | var | `modules/cache`; tests of guards/throttler, modules/cache; docs: 1 package README | framework internal | moves to internal/memorycache (modules/cache re-exports the errors already) |
| `MemoryCache` | struct | tests of modules/cache; docs: .ginject, 2 package READMEs | framework internal | moves to internal/memorycache |
| `MemoryCache.Get` | method | docs: .ginject, README, 1 package README | framework internal | moves to internal/memorycache |
| `MemoryCache.Set` | method | docs: .ginject, README, 1 package README | framework internal | moves to internal/memorycache |
| `MemoryCache.SetNX` | method | docs: 1 package README | framework internal | moves to internal/memorycache |
| `MemoryCache.Incr` | method | docs: 1 package README | framework internal | moves to internal/memorycache |
| `MemoryCache.Delete` | method | docs: 1 package README | framework internal | moves to internal/memorycache |
| `MemoryCache.Keys` | method | docs: 1 package README | framework internal | moves to internal/memorycache |
| `MemoryCache.TTL` | method | docs: README, 1 package README | framework internal | moves to internal/memorycache |
| `MemoryCacheOptions` | struct | `guards/throttler`; docs: .ginject, 3 package READMEs | user API | moves to internal/memorycache, reachable through modules/cache |
| `MemoryCacheOptions.MaxEntries` | field | `guards/throttler`; docs: 1 package README | user API | moves to internal/memorycache, reachable through modules/cache |
| `MemoryCacheOptions.MaxBytes` | field | docs: 1 package README | user API | moves to internal/memorycache, reachable through modules/cache |
| `NewMemoryCache` | func | `guards/throttler`, `modules/cache`; tests of guards/throttler, modules/cache; docs: .ginject, README, 4 package READMEs | user API | moves to internal/memorycache, reachable through modules/cache |

## `middlewares/compress`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `Compress` | struct | docs: README, 1 package README | user API | stays |
| `Compress.MinSize` | field | docs: 1 package README | user API | stays |
| `Compress.NewMiddleware` | method | docs: .ginject | user API | stays |
| `Compress.Use` | method | docs: .ginject | user API | stays |

## `middlewares/cors`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `CORS` | struct | `sample/`; docs: .ginject, README, 2 package READMEs | user API | stays |
| `CORS.AllowOrigins` | field | `sample/`; docs: .ginject, README, 1 package README | user API | stays |
| `CORS.AllowMethods` | field | nowhere | user API | stays |
| `CORS.AllowHeaders` | field | nowhere | user API | stays |
| `CORS.ExposeHeaders` | field | docs: 1 package README | user API | stays |
| `CORS.MaxAge` | field | docs: 1 package README | user API | stays |
| `CORS.IsAllowCredentials` | field | docs: .ginject, README, 1 package README | user API | stays |
| `CORS.IsAllowPrivateNetwork` | field | nowhere | user API | stays |
| `CORS.NewMiddleware` | method | docs: .ginject | user API | stays |
| `CORS.Use` | method | docs: .ginject | user API | stays |

## `middlewares/csrf`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `CSRF` | struct | docs: .ginject, README, 1 package README | user API | stays |
| `CSRF.TrustedOrigins` | field | docs: .ginject, README, 1 package README | user API | stays |
| `CSRF.NewMiddleware` | method | docs: .ginject | user API | stays |
| `CSRF.Use` | method | docs: .ginject | user API | stays |

## `middlewares/helmet`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `Disabled` | const | docs: .ginject, 1 package README | user API | stays |
| `Helmet` | struct | `sample/`; docs: README, 1 package README | user API | stays |
| `Helmet.ContentSecurityPolicy` | field | docs: 1 package README | user API | stays |
| `Helmet.CrossOriginEmbedderPolicy` | field | nowhere | user API | stays |
| `Helmet.CrossOriginOpenerPolicy` | field | nowhere | user API | stays |
| `Helmet.CrossOriginResourcePolicy` | field | nowhere | user API | stays |
| `Helmet.FrameOptions` | field | docs: 1 package README | user API | stays |
| `Helmet.PermissionsPolicy` | field | nowhere | user API | stays |
| `Helmet.ReferrerPolicy` | field | nowhere | user API | stays |
| `Helmet.StrictTransportSecurity` | field | docs: 1 package README | user API | stays |
| `Helmet.NewMiddleware` | method | docs: .ginject | user API | stays |
| `Helmet.Use` | method | docs: .ginject | user API | stays |

## `middlewares/hostcheck`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `HostCheck` | struct | docs: README, 1 package README | user API | stays |
| `HostCheck.Hosts` | field | docs: README, 1 package README | user API | stays |
| `HostCheck.NewMiddleware` | method | docs: .ginject | user API | stays |
| `HostCheck.Use` | method | docs: .ginject | user API | stays |

## `middlewares/httpsredirect`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `HTTPSRedirect` | struct | docs: README, 1 package README | user API | stays |
| `HTTPSRedirect.Host` | field | docs: .ginject, README, 1 package README | user API | stays |
| `HTTPSRedirect.Port` | field | docs: 1 package README | user API | stays |
| `HTTPSRedirect.NewMiddleware` | method | docs: .ginject | user API | stays |
| `HTTPSRedirect.Use` | method | docs: .ginject | user API | stays |

## `middlewares/loadshed`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `LoadShed` | struct | docs: README, 1 package README | user API | stays |
| `LoadShed.MaxInFlight` | field | docs: 1 package README | user API | stays |
| `LoadShed.MaxQueue` | field | docs: 1 package README | user API | stays |
| `LoadShed.NewMiddleware` | method | docs: .ginject | user API | stays |
| `LoadShed.Use` | method | docs: .ginject | user API | stays |

## `middlewares/proxy`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `ClientIP` | func | `guards/throttler`; docs: .ginject, 5 package READMEs | user API | stays |
| `Forwarded` | const | docs: 1 package README | user API | stays; renamed HeaderForwarded |
| `Header` | type | docs: 1 package README | user API | stays |
| `Host` | func | docs: .ginject, 4 package READMEs | user API | stays |
| `Proxy` | struct | tests of core, middlewares/* (4); docs: .ginject, README, 5 package READMEs | user API | stays |
| `Proxy.TrustedProxies` | field | tests of core, middlewares/* (4); docs: .ginject, README, 1 package README | user API | stays |
| `Proxy.TrustedHops` | field | docs: 1 package README | user API | stays |
| `Proxy.Header` | field | docs: .ginject, README, 1 package README | user API | stays |
| `Proxy.IsHostForwarded` | field | tests of middlewares/* (3); docs: .ginject, 1 package README | user API | stays |
| `Proxy.NewMiddleware` | method | docs: .ginject | user API | stays |
| `Proxy.Use` | method | docs: .ginject | user API | stays |
| `Scheme` | func | docs: .ginject, 4 package READMEs | user API | stays |
| `XForwarded` | const | docs: 1 package README | user API | stays; renamed HeaderXForwarded |

## `modules/cache`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `Cache` | interface | `guards/throttler`; docs: .ginject, README, 4 package READMEs | extension point | stays |
| `Cache.Delete` | interface method | docs: 1 package README | extension point | stays |
| `Cache.Get` | interface method | `guards/throttler`; docs: .ginject, README, 1 package README | extension point | stays |
| `Cache.Incr` | interface method | `guards/throttler`; docs: 1 package README | extension point | stays |
| `Cache.Keys` | interface method | docs: 1 package README | extension point | stays |
| `Cache.Set` | interface method | `guards/throttler`; docs: .ginject, README, 1 package README | extension point | stays |
| `Cache.SetNX` | interface method | docs: 1 package README | extension point | stays |
| `Cache.TTL` | interface method | docs: README, 1 package README | extension point | stays |
| `CacheModuleOptions` | struct | `sample/`; docs: README, 1 package README | user API | stays |
| `CacheModuleOptions.IsGlobal` | field | docs: .ginject, README, 1 package README | user API | stays |
| `CacheModuleOptions.OnInit` | field | docs: .ginject, 1 package README | user API | stays |
| `CacheModuleOptions.Backend` | field | docs: .ginject, 1 package README | user API | stays |
| `CacheOnInitFn` | alias | docs: README | user API | stays |
| `CacheService` | struct | `sample/`; docs: README, 1 package README | user API | stays |
| `CacheService.Backend` | field | docs: .ginject, 1 package README | user API | stays |
| `CacheService.NewProvider` | method | docs: .ginject | user API | stays |
| `CacheService.Get` | method | `sample/`; docs: .ginject, README, 1 package README | user API | stays |
| `CacheService.Set` | method | `sample/`; docs: .ginject, README, 1 package README | user API | stays |
| `CacheService.SetNX` | method | docs: 1 package README | user API | stays |
| `CacheService.Incr` | method | docs: 1 package README | user API | stays |
| `CacheService.Delete` | method | `sample/`; docs: 1 package README | user API | stays |
| `CacheService.Keys` | method | docs: 1 package README | user API | stays |
| `CacheService.TTL` | method | docs: README, 1 package README | user API | stays |
| `ErrEmptyKey` | var | docs: .ginject, 1 package README | user API | stays |
| `ErrEntryTooLarge` | var | docs: 1 package README | user API | stays |
| `ErrNegativeTTL` | var | nowhere | user API | stays |
| `ErrNotCounter` | var | nowhere | user API | stays |
| `Register` | func | `sample/`; docs: .ginject, README, 1 package README | user API | stays |

## `modules/config`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `ConfigHookFn` | alias | `sample/`; docs: README, 1 package README | user API | stays |
| `ConfigLoadFn` | alias | docs: README, 1 package README | user API | stays |
| `ConfigModuleOptions` | struct | `sample/`; docs: CLAUDE.md, README, 1 package README | user API | stays |
| `ConfigModuleOptions.IsGlobal` | field | `sample/`; docs: .ginject, README, 1 package README | user API | stays |
| `ConfigModuleOptions.IsIgnoreEnvFile` | field | docs: README, 1 package README | user API | stays |
| `ConfigModuleOptions.IsOverride` | field | docs: 1 package README | user API | stays |
| `ConfigModuleOptions.IsExpandVariables` | field | `sample/`; docs: README, 1 package README | user API | stays |
| `ConfigModuleOptions.ENVFilePaths` | field | `sample/`; docs: 1 package README | user API | stays; renamed EnvFilePaths |
| `ConfigModuleOptions.Loads` | field | docs: 1 package README | user API | stays |
| `ConfigModuleOptions.Hooks` | field | `sample/`; docs: README, 1 package README | user API | stays |
| `ConfigModuleOptions.OnInit` | field | docs: .ginject | user API | stays |
| `ConfigOnInitFn` | alias | docs: README | user API | stays |
| `ConfigService` | struct | `sample/`; docs: README, 2 package READMEs | user API | stays |
| `ConfigService.State` | field | nowhere | user API | stays |
| `ConfigService.NewProvider` | method | docs: .ginject | user API | stays |
| `ConfigService.Get` | method | docs: .ginject, README, 1 package README | user API | stays |
| `ConfigService.Set` | method | `sample/`; docs: .ginject, README, 1 package README | user API | stays |
| `ConfigService.Transform` | method | `sample/`; docs: .ginject, README, 1 package README | user API | stays |
| `Register` | func | `sample/`; docs: .ginject, CLAUDE.md, README, 2 package READMEs | user API | stays |

## `pattern`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `Kind` | type | nowhere | framework internal | moves to internal/pattern |
| `KindComplex` | const | `memorybroker`, `wsevent` | framework internal | moves to internal/pattern |
| `KindExact` | const | `memorybroker` | framework internal | moves to internal/pattern |
| `KindGlobal` | const | `memorybroker`, `wsevent` | framework internal | moves to internal/pattern |
| `KindSuffixWildcard` | const | `memorybroker`, `wsevent` | framework internal | moves to internal/pattern |
| `NewPattern` | func | `core`, `log`, `memorybroker`, `wsevent`; docs: 1 package README | framework internal | moves to internal/pattern |
| `Pattern` | struct | `core`, `log`, `memorybroker`, `wsevent` | framework internal | moves to internal/pattern |
| `Pattern.Match` | method | `core`, `log`, `memorybroker`, `wsevent`; docs: .ginject, 1 package README | framework internal | moves to internal/pattern |
| `Pattern.Raw` | method | `wsevent` | framework internal | moves to internal/pattern |
| `Pattern.Kind` | method | `memorybroker`, `wsevent` | framework internal | moves to internal/pattern |
| `Pattern.SimplePrefix` | method | `memorybroker`, `wsevent`; docs: 1 package README | framework internal | moves to internal/pattern |
| `Pattern.IsExact` | method | `log` | framework internal | moves to internal/pattern |

## `routing`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `HTTPMethods` | var | nowhere | framework internal | moves to internal/routing |
| `MaxPathLength` | const | nowhere | framework internal | moves to internal/routing |
| `MaxPathSegments` | const | nowhere | framework internal | moves to internal/routing |
| `MethodRouteVersionToPattern` | func | `common`, `core`; tests of common; docs: 1 package README | framework internal | moves to internal/routing |
| `NewRouter` | func | `core`; docs: 1 package README | framework internal | moves to internal/routing |
| `OperationsMapHTTPMethods` | var | `common`, `core`, `devtool` | framework internal | moves to internal/routing |
| `ParseToParamKey` | func | `core`; docs: 1 package README | framework internal | moves to internal/routing |
| `PatternToMethodRouteVersion` | func | `common`, `core`; docs: 1 package README | framework internal | moves to internal/routing |
| `Router` | struct | `core`, `ginject (root)`; docs: .ginject | framework internal | moves to internal/routing |
| `Router.GlobalMiddlewares` | field | nowhere | framework internal | moves to internal/routing |
| `Router.InjectableHandlers` | field | nowhere | framework internal | moves to internal/routing |
| `Router.Use` | method | docs: .ginject, 1 package README | framework internal | moves to internal/routing |
| `Router.For` | method | docs: 1 package README | framework internal | moves to internal/routing |
| `Router.Add` | method | docs: 1 package README | framework internal | moves to internal/routing |
| `Router.AddInjectableHandler` | method | `core`; docs: 1 package README | framework internal | moves to internal/routing |
| `Router.Match` | method | `core`; docs: .ginject, 1 package README | framework internal | moves to internal/routing |
| `RouterItem` | struct | `core` | framework internal | moves to internal/routing |
| `RouterItem.Method` | field | docs: .ginject, README | framework internal | moves to internal/routing |
| `RouterItem.Version` | field | docs: README | framework internal | moves to internal/routing |
| `RouterItem.Pattern` | field | docs: .ginject, 1 package README | framework internal | moves to internal/routing |
| `RouterItem.Index` | field | `core`; docs: .ginject | framework internal | moves to internal/routing |
| `RouterItem.HandlerIndex` | field | nowhere | framework internal | moves to internal/routing |
| `RouterItem.Handlers` | field | docs: .ginject | framework internal | moves to internal/routing |
| `RouterItem.ParamKeys` | field | `core`; docs: .ginject, 1 package README | framework internal | moves to internal/routing |
| `RouterItem.ParamNames` | field | docs: .ginject, 1 package README | framework internal | moves to internal/routing |
| `RouterItem.Injectable` | field | docs: .ginject, 1 package README | framework internal | moves to internal/routing |
| `SERVE` | const | `common`, `core` | framework internal | moves to internal/routing |

## `trace`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `Event` | struct | `accesslog`, `core`; tests of accesslog, core; docs: .ginject, 2 package READMEs | framework internal | moves to internal/trace |
| `Event.ID` | field | `accesslog`, `core`; tests of accesslog, core; docs: 1 package README | framework internal | moves to internal/trace |
| `Event.TraceID` | field | `accesslog`, `core`; tests of accesslog, core | framework internal | moves to internal/trace |
| `Event.SpanID` | field | `core`; tests of accesslog, core | framework internal | moves to internal/trace |
| `Event.ParentID` | field | `accesslog`, `core`; tests of accesslog, core | framework internal | moves to internal/trace |
| `Event.Stage` | field | `accesslog`, `core`; tests of accesslog, core; docs: 1 package README | framework internal | moves to internal/trace |
| `Event.Name` | field | `accesslog`, `core`; tests of accesslog, core; docs: .ginject, README, 1 package README | framework internal | moves to internal/trace |
| `Event.Transport` | field | `accesslog`, `core`; tests of accesslog, core; docs: .ginject, README, 1 package README | framework internal | moves to internal/trace |
| `Event.Duration` | field | `accesslog`, `core`; tests of accesslog, core; docs: .ginject, 1 package README | framework internal | moves to internal/trace |
| `Event.Operation` | field | `accesslog`, `core`; tests of accesslog, core | framework internal | moves to internal/trace |
| `Event.Target` | field | `accesslog`, `core`; tests of accesslog, core | framework internal | moves to internal/trace |
| `Event.Code` | field | `accesslog`, `core`; tests of accesslog, core; docs: .ginject, README | framework internal | moves to internal/trace |
| `Event.ClientIP` | field | `accesslog`, `core`; tests of accesslog, core; docs: .ginject, 1 package README | framework internal | moves to internal/trace |
| `StageComplete` | const | `accesslog`, `core`; tests of accesslog, core | framework internal | moves to internal/trace |
| `StageExceptionFilter` | const | `core`; tests of accesslog, core | framework internal | moves to internal/trace |
| `StageGuard` | const | `core`; tests of accesslog, core; docs: 1 package README | framework internal | moves to internal/trace |
| `StageHandler` | const | `core`; tests of accesslog, core; docs: 1 package README | framework internal | moves to internal/trace |
| `StageMiddleware` | const | `core`; tests of accesslog, core | framework internal | moves to internal/trace |
| `StagePipe` | const | `core`; tests of core | framework internal | moves to internal/trace |
| `StagePostInterceptor` | const | `core`; tests of accesslog, core | framework internal | moves to internal/trace |
| `StagePreInterceptor` | const | `core`; tests of accesslog, core | framework internal | moves to internal/trace |
| `Tracer` | struct | `accesslog`, `core`; tests of accesslog, core; docs: .ginject, 3 package READMEs | framework internal | moves to internal/trace |
| `Tracer.Enabled` | method | `core`; tests of accesslog, core; docs: .ginject, 1 package README | framework internal | moves to internal/trace |
| `Tracer.Emit` | method | `core`; tests of accesslog, core; docs: 1 package README | framework internal | moves to internal/trace |
| `Tracer.Listen` | method | `accesslog`; tests of core; docs: .ginject, README, 1 package README | framework internal | moves to internal/trace |
| `Transport` | func | tests of core; docs: .ginject, CLAUDE.md, README, 4 package READMEs | user API | moves to internal/trace, reachable through the root package; renamed ginject.TraceTransport |
| `TransportHTTP` | const | `core`; tests of accesslog, core; docs: 1 package README | framework internal | moves to internal/trace |
| `TransportWS` | const | `accesslog`, `core`; tests of accesslog, core | framework internal | moves to internal/trace |

## `versioning`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `CustomVersion` | const | docs: README, 1 package README | user API | moves to internal/versioning, reachable through the root package; renamed ginject.VersioningCustom (versioning.TypeCustom) |
| `ExtractorHandler` | alias | nowhere | framework internal | moves to internal/versioning |
| `HeaderVersion` | const | tests of core, devtool; docs: README, 2 package READMEs | user API | moves to internal/versioning, reachable through the root package; renamed ginject.VersioningHeader (versioning.TypeHeader) |
| `MediaType` | const | docs: README, 1 package README | user API | moves to internal/versioning, reachable through the root package; renamed ginject.VersioningMediaType (versioning.TypeMediaType) |
| `NeutralVersion` | const | `core`; docs: .ginject | framework internal | moves to internal/versioning |
| `QueryVersion` | const | docs: README | user API | moves to internal/versioning, reachable through the root package; renamed ginject.VersioningQuery (versioning.TypeQuery) |
| `Versioning` | struct | `core`, `devtool`, `ginject (root)`; tests of core, devtool; docs: .ginject, README, 3 package READMEs | user API | moves to internal/versioning, reachable through the root package; renamed ginject.VersioningOptions (versioning.Options) |
| `Versioning.Type` | field | `devtool`; tests of core, devtool; docs: .ginject, README, 1 package README | user API | moves to internal/versioning, reachable through the root package |
| `Versioning.Key` | field | `core`, `devtool`; tests of core, devtool; docs: README, 1 package README | user API | moves to internal/versioning, reachable through the root package |
| `Versioning.DefaultVersion` | field | tests of core; docs: README, 1 package README | user API | moves to internal/versioning, reachable through the root package |
| `Versioning.Extractor` | field | docs: 1 package README | user API | moves to internal/versioning, reachable through the root package |
| `Versioning.GetTypeString` | method | docs: 1 package README | framework internal | hidden: package function of internal/versioning (design 0017, Hooks) |
| `Versioning.GetVersion` | method | `core`; docs: 1 package README | framework internal | hidden: package function of internal/versioning (design 0017, Hooks) |

## `wsevent`

| Name | Kind | Used outside its package | Class | Design 0017 |
|---|---|---|---|---|
| `NewWSEvent` | func | `core` | framework internal | moves to internal/wsevent |
| `WSEvent` | struct | `core` | framework internal | moves to internal/wsevent |
| `WSEvent.Add` | method | nowhere | framework internal | moves to internal/wsevent |
| `WSEvent.AddMiddlewares` | method | `core`; tests of core | framework internal | moves to internal/wsevent |
| `WSEvent.AddInjectableHandler` | method | `core`; tests of core | framework internal | moves to internal/wsevent |
| `WSEvent.Match` | method | `core`; docs: .ginject | framework internal | moves to internal/wsevent |
| `WSEventItem` | struct | `core` | framework internal | moves to internal/wsevent |
| `WSEventItem.Handler` | field | `core`; docs: .ginject, README | framework internal | moves to internal/wsevent |
| `WSEventItem.Middlewares` | field | `core` | framework internal | moves to internal/wsevent |

