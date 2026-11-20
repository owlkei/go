# Devtool

Internal package: applications set `IsDevtool: true` in `owlky.AppOptions` instead.

*Package `devtool` builds an in-memory snapshot of an app's HTTP routes: for each route its handler, method, path, version, the layers that run on it and the shape of its request.*

- [Devtool](#devtool)
  - [Usage](#usage)
  - [`Devtool`](#devtool-1)
    - [GetConfiguration](#getconfiguration)
  - [`DevtoolBuilder`](#devtoolbuilder)
    - [Build](#build)
  - [Render](#render)
  - [Modules](#modules)
  - [Snapshot types](#snapshot-types)

## Usage

You don't call this package yourself: `core` builds the snapshot at `Create()` when `AppOptions.IsDevtool` is true.

```go
package main

import (
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/core"
)

type UserController struct {
	common.HTTP
}

func (instance UserController) NewController() core.Controller {
	return instance
}

func (instance UserController) READ_users() string {
	return "users"
}

func main() {
	app := core.New(&core.AppOptions{IsDevtool: true})
	app.Create(core.NewModuleBuilder().Controllers(UserController{}).Build())
}
```

The snapshot is built in memory only: no server, port or goroutine is started.

## `Devtool`

`Devtool` embeds `GetConfigurationResponse`, so `devtool.Controller.Http` lists one `*HTTPComponent` per route.

### GetConfiguration

`GetConfiguration(context.Context, *GetConfigurationRequest) (*GetConfigurationResponse, error)` returns the snapshot.

#### Rules
- The response's `Controller` and `Render` are the snapshot's, and the error is `nil`.
- The response encodes to JSON and decodes back unchanged.

## `DevtoolBuilder`

`DevtoolBuilder()` returns a builder. Each `Add…` method returns the builder, so calls chain:

| Method | Adds |
|---|---|
| `AddExceptionFilters([]any, []common.HTTPLayer)` | global and per-route exception filters |
| `AddPreRoute([]*PreRouteStage)` | the `pre_route` array: the built-in stages in their run order, each with its `name` and the `fields` of its `Snapshot()` (counters included, read when the snapshot is marshaled) |
| `AddMiddlewares([]common.MiddlewareFn, []common.HTTPLayer)` | global and per-route user middlewares |
| `AddGuarders([]any, []common.HTTPLayer)` | global and per-route guards |
| `AddInterceptors([]any, []common.HTTPLayer)` | global and per-route interceptors |
| `AddVersioning(*versioning.Options)` | the app's versioning, or `nil` without versioning |
| `AddHTTPMainHandlers([]common.HTTPLayer)` | the routes, sorted by path |
| `AddRender(*Render)` | the render section, or `nil` without `Render.FS` |
| `AddModules(func() []*ModuleStatus)` | the module states, read at each `GetConfiguration` call |

### Build

`Build() *Devtool` returns the snapshot.

#### Usage

```go
snapshot := devtool.DevtoolBuilder().
	AddVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"}).
	AddHTTPMainHandlers(handlers).
	Build()
```

#### Rules
- Each route gives one `HTTPComponent` with its handler name, HTTP method, route and a non-empty `Id`.
- A page route (`HTTPLayer.IsPage`: a `RENDER_` or `SUBMIT_` route) has `Kind` `page` (JSON `kind`); an API route has none, so `kind` is omitted.
- A global layer is listed with `Scope` `GLOBAL_SCOPE` and its type name (`devtool.snapshotGlobalGuard`).
- A body pipe's fields are listed in `Request.Body`.
- With versioning, `Versioning` holds the route's version, the versioning `Key` and its `Type`.
- Without versioning (`AddVersioning` not called, or called with `nil`), `Build` does not panic: `Versioning.Value` is the route's version, `Key` is `""` and `Type` is `0`.

## Render

With `Render.FS` set, `core` adds a `render` section (JSON keys in parentheses); without it the key is
omitted:

| Field | Type | Description |
|---|---|---|
| `IsReload` (`is_reload`) | `bool` | `RenderOptions.IsReload` |
| `Pages` (`pages`) | `[]*RenderPage` | One per page route, sorted by name: `Name` (`name`, the template path without `.html`, `bookings/{id}`), `Route` (`route`, `/bookings/{id}`), `Layout` (`layout`, the layout file such as `b/layout.html`, omitted without one) and `Assets` (`assets`, the page set's CSS and JS files, layout first, omitted without any) |
| `Partials` (`partials`) | `[]string` | Partial names, sorted (`partials/field`) |
| `ErrorTemplates` (`error_templates`) | `[]string` | Error page files, sorted (`404.html`, `5xx.html`); omitted when only the built-in page exists |
| `Assets` (`assets`) | `int` | Files under `assets/`, hidden ones not counted |
| `Public` (`public`) | `int` | Files under `public/`, hidden ones not counted |
| `Renders`, `Failures` (`renders`, `failures`) | `int64` | Renders that succeeded and failed so far |

`Renders` and `Failures` are read from `Counters` each time the section is encoded, so the same
snapshot reports live counts. `Counters` itself is not encoded; a decoded `Render` holds the counts
as plain numbers.

## Modules

`core` passes a function to `AddModules`; `GetConfiguration` calls it each time, so the `modules` list
(JSON `modules`) shows live states. Without `AddModules` the key is omitted. One `ModuleStatus` per
module, in discovery order:

| Field | Type | Description |
|---|---|---|
| `Name` (`name`) | `string` | The module's name |
| `State` (`state`) | `string` | `pending`, `ready` or `failed` |
| `IsAsync` (`is_async`) | `bool` | An `AsyncImports` entry, a module of its unit, or a module not ready at `Create` |
| `WaitingFor` (`waiting_for`) | `[]string` | While pending: the entries it waits for, or `can't resolve <T>` items |
| `Attempts` (`attempts`) | `int` | Failed `owlky.Retry` attempts of the entry |
| `LastError` (`last_error`) | `string` | The last attempt's error, or the cause once failed |
| `ReadyAfterMs` (`ready_after_ms`) | `int64` | Milliseconds from the start of `Create` to ready |

`LastError` may hold a driver's error text, such as a host name: the devtool is for development only.

## Snapshot types

- `GetConfigurationRequest` (no fields) and `GetConfigurationResponse` (`Controller *Controller`, `Render *Render`, `Modules []*ModuleStatus`).
- `Controller` (`Http []*HTTPComponent`).
- `HTTPComponent`: `Id`, `Kind`, `Handler`, `HttpMethod`, `Route` (`string`), `ExceptionFilters`, `Middlewares`, `Guards`, `Interceptors` (`[]*Layer`), `Versioning *HTTPVersioning`, `Request *HTTPRequest`.
- `Render` (see [Render](#render)) and `RenderPage` (`Name`, `Route`, `Layout`, `Assets`). `PageKind` is `"page"`.
- `Layer`: `Scope LayerScope`, `Name string`.
- `HTTPVersioning`: `Type int32`, `Value string`, `Key string`.
- `HTTPRequest`: `Body`, `Form`, `Query`, `Header`, `Param`, `File` (`[]*Schema`).
- `Schema`: `Name`, `Type`, `Format` (`string`), `Item *Schema`, `Properties []*Schema`.
- `LayerScope` (`int32`): `LayerScope_UNKNOWN`, `LayerScope_REQUEST_SCOPE`, `LayerScope_GLOBAL_SCOPE`, with the `LayerScope_name` and `LayerScope_value` maps. `String()` returns the name, and `UNKNOWN` for any other value.
