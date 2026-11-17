# Versioning

*Package `versioning` reads the API version of a request from the query, a header, the `Accept` media type or a function of your own, so that the router can pick the route registered for that version.*

- [Versioning](#versioning)
  - [Usage](#usage)
  - [Constants](#constants)
  - [`Versioning` Fields](#versioning-fields)
    - [Type](#type)
    - [Key](#key)
    - [DefaultVersion](#defaultversion)
    - [Extractor](#extractor)
  - [`Versioning` Methods](#versioning-methods)
    - [GetVersion](#getversion)
    - [GetTypeString](#gettypestring)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

## Usage

Enable versioning on the app, then name handlers with a `VERSION` token (`READ_VERSION_2`):

```go
package main

import (
	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/internal/versioning"
)

func main() {
	app := core.New()
	app.EnableVersioning(versioning.Versioning{
		Type:           versioning.MediaType,
		Key:            "v",
		DefaultVersion: "1",
	})
}
```

A request with `Accept: application/json;v=2` is routed to version `2`; one without a version parameter to `1`. When nothing is registered for the request's version, `core` tries the routes of `NeutralVersion`.

## Constants

| Name | Type | Value |
|---|---|---|
| `QueryVersion` | `int` | `1`: the version is a query parameter |
| `HeaderVersion` | `int` | `2`: the version is a request header |
| `CustomVersion` | `int` | `3`: the version comes from `Extractor` |
| `MediaType` | `int` | `4`: the version is a parameter of the `Accept` header |
| `NeutralVersion` | `string` | `"NEUTRAL"`: routes that answer for any version |

`ExtractorHandler` is `func(*ctx.HTTPContext) string`.

## `Versioning` Fields

### Type
Type: `int`

Default: `0` (no type: every request gets `DefaultVersion`)

Required: `true`

Where the version is read: `QueryVersion`, `HeaderVersion`, `CustomVersion` or `MediaType`.

### Key
Type: `string`

Default: `"v"` (set by `core` when empty)

Required: `false`

The query key, the header name or the media type parameter name. Not used by `CustomVersion`.

### DefaultVersion
Type: `string`

Default: `""`

Required: `false`

The version used when the request carries none.

### Extractor
Type: `ExtractorHandler`

Default: `nil`

Required: only for `CustomVersion`

Returns the version of a request.

```go
versioning.Versioning{
	Type: versioning.CustomVersion,
	Extractor: func(c *ctx.HTTPContext) string {
		return c.Header().Get("X-Client-Version")
	},
}
```

## `Versioning` Methods

### GetVersion

Returns the version of a request.

#### Parameters
1. `c` `*ctx.HTTPContext`: the request context.

#### Returns
1. `string`: the version, or `DefaultVersion`.

#### Usage

```go
v := &versioning.Versioning{Type: versioning.HeaderVersion, Key: "X-Api-Version", DefaultVersion: "1"}
version := v.GetVersion(c)
```

#### Rules
- Query: the first value of the query key `Key`; empty or absent gives `DefaultVersion`.
- Header: the header `Key`, in any case; empty or absent gives `DefaultVersion`.
- Media type: `Accept` is read as media ranges separated by `,`, each with parameters separated by `;`. The version is the value of the first parameter, left to right, whose name equals `Key` (ASCII case-insensitive, spaces around the name and the value ignored) and whose value is not empty; double quotes around the value are removed. With `Key` `v`:

  | `Accept` | Version |
  |---|---|
  | `application/json;v=2` | `2` |
  | `application/json;v=2;q=0.9` | `2` |
  | `application/json;v=2, text/html` | `2` |
  | `application/json; v="2"` | `2` |
  | `text/html, application/json;V=3` | `3` |
  | `application/json;charset=utf-8; v = 4` | `4` |
  | `a/b;v=1, c/d;v=2` | `1` |
  | `application/json;v=;v=3` | `3` |
  | `application/json;rev=7`, `;xv=2`, `;vv=2`, `;q=0.9` | default |
  | `application/json,v=2` (a media type, not a parameter) | default |
  | `application/json;v=`, `application/json;v=""` | default |

- Custom: the result of `Extractor`; a nil `Extractor`, or one that returns `""`, gives `DefaultVersion`.
- An unknown `Type` gives `DefaultVersion`.
- No allocation for header, media type and custom; the query type allocates nothing once the context has parsed its query.

### GetTypeString

Returns the name of `Type`, for logs.

#### Returns
1. `string`: `query`, `header`, `custom`, `media_type`, or `""` for an unknown type.

#### Usage

```go
fmt.Println(v.GetTypeString())
```

## Performance notes

- The media type scan cuts the header in place and compares names with `strings.EqualFold`: no split, no `Key + "="` concatenation per request.
- `QueryVersion` makes the context parse the whole query on the first call of a request.

## Benchmarks

Results depend on the machine. They were captured when this README was generated, with `go test -run=^$ -bench=. -benchmem ./versioning`:

```console
goos: darwin
goarch: amd64
pkg: github.com/dangduoc08/ginject/internal/versioning
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkGetVersion_Query-12        	57787495	        20.96 ns/op	       0 B/op	       0 allocs/op
BenchmarkGetVersion_Header-12       	22535310	        53.95 ns/op	       0 B/op	       0 allocs/op
BenchmarkGetVersion_Custom-12       	268396484	         4.469 ns/op	       0 B/op	       0 allocs/op
BenchmarkGetVersion_MediaType-12    	14635394	        82.52 ns/op	       0 B/op	       0 allocs/op
```
