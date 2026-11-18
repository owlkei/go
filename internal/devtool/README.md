# Devtool

Internal package: applications use `app.EnableDevtool()` instead.

*Package `devtool` builds an in-memory snapshot of an app's HTTP routes: for each route its handler, method, path, version, the layers that run on it and the shape of its request.*

- [Devtool](#devtool)
  - [Usage](#usage)
  - [`Devtool`](#devtool-1)
    - [GetConfiguration](#getconfiguration)
  - [`DevtoolBuilder`](#devtoolbuilder)
    - [Build](#build)
  - [Render](#render)
  - [Snapshot types](#snapshot-types)

## Usage

You don't call this package yourself: `core` builds the snapshot at `Create()` when `EnableDevtool()` was called.

```go
package main

import (
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/core"
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
	app := core.New()
	app.EnableDevtool()
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
| `AddMiddlewares([]common.MiddlewareFn, []common.HTTPLayer)` | global and per-route middlewares |
| `AddGuarders([]any, []common.HTTPLayer)` | global and per-route guards |
| `AddInterceptors([]any, []common.HTTPLayer)` | global and per-route interceptors |
| `AddVersioning(*versioning.Options)` | the app's versioning, or `nil` without versioning |
| `AddHTTPMainHandlers([]common.HTTPLayer)` | the routes, sorted by path |
| `AddRender(*Render)` | the render section, or `nil` without `EnableRender` |

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
- A page route (`HTTPLayer.IsPage`) has `Kind` `page` (JSON `kind`); an API route has none, so `kind` is omitted.
- A global layer is listed with `Scope` `GLOBAL_SCOPE` and its type name (`devtool.snapshotGlobalGuard`).
- A body pipe's fields are listed in `Request.Body`.
- With versioning, `Versioning` holds the route's version, the versioning `Key` and its `Type`.
- Without versioning (`AddVersioning` not called, or called with `nil`), `Build` does not panic: `Versioning.Value` is the route's version, `Key` is `""` and `Type` is `0`.

## Render

With `EnableRender`, `core` adds a `render` section (JSON keys in parentheses); without it the key is
omitted:

| Field | Type | Description |
|---|---|---|
| `IsReload` (`is_reload`) | `bool` | `RenderOptions.IsReload` |
| `Pages` (`pages`) | `[]*RenderPage` | Every page, sorted by name: `Name` (`name`, `bookings/list`) and `Layout` (`layout`, the layout file such as `b/layout.html`, omitted without one) |
| `Partials` (`partials`) | `[]string` | Partial names, sorted (`partials/field`) |
| `ErrorTemplate` (`error_template`) | `string` | `error.html`, or `built-in` without one |
| `Assets` (`assets`) | `int` | Files under `assets/`, hidden ones not counted |
| `Renders`, `Failures` (`renders`, `failures`) | `int64` | Renders that succeeded and failed so far |

`Renders` and `Failures` are read from `Counters` each time the section is encoded, so the same
snapshot reports live counts. `Counters` itself is not encoded; a decoded `Render` holds the counts
as plain numbers.

## Snapshot types

- `GetConfigurationRequest` (no fields) and `GetConfigurationResponse` (`Controller *Controller`, `Render *Render`).
- `Controller` (`Http []*HTTPComponent`).
- `HTTPComponent`: `Id`, `Kind`, `Handler`, `HttpMethod`, `Route` (`string`), `ExceptionFilters`, `Middlewares`, `Guards`, `Interceptors` (`[]*Layer`), `Versioning *HTTPVersioning`, `Request *HTTPRequest`.
- `Render` (see [Render](#render)) and `RenderPage` (`Name`, `Layout`). `PageKind` is `"page"`.
- `Layer`: `Scope LayerScope`, `Name string`.
- `HTTPVersioning`: `Type int32`, `Value string`, `Key string`.
- `HTTPRequest`: `Body`, `Form`, `Query`, `Header`, `Param`, `File` (`[]*Schema`).
- `Schema`: `Name`, `Type`, `Format` (`string`), `Item *Schema`, `Properties []*Schema`.
- `LayerScope` (`int32`): `LayerScope_UNKNOWN`, `LayerScope_REQUEST_SCOPE`, `LayerScope_GLOBAL_SCOPE`, with the `LayerScope_name` and `LayerScope_value` maps. `String()` returns the name, and `UNKNOWN` for any other value.
