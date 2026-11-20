# CORS Middleware

Internal package: applications use `AppOptions.CORS` (`owlkei.CORSOptions`) with `AppOptions.Origins` instead; it is the `cors` pre-route stage. The allowed origins come from `Origins` (`AllowOrigins` below).

*`cors` lets the browser pages you list read your API from another origin, following the [Fetch Standard CORS protocol](https://fetch.spec.whatwg.org/#http-cors-protocol): actual requests, `OPTIONS` preflights, credentials and private network access.*

- [CORS Middleware](#cors-middleware)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`CORS` Struct](#cors-struct)
    - [AllowOrigins](#alloworigins)
    - [AllowMethods](#allowmethods)
    - [AllowHeaders](#allowheaders)
    - [ExposeHeaders](#exposeheaders)
    - [MaxAge](#maxage)
    - [IsAllowCredentials](#isallowcredentials)
    - [IsAllowPrivateNetwork](#isallowprivatenetwork)
  - [`CORS` Methods](#cors-methods)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [Rules](#rules)
  - [Limits](#limits)
  - [Benchmarks](#benchmarks)

## Key Features
- Nothing is allowed until you list origins; `*` never with credentials; `null` is refused
- Exact origins and `scheme://*.host[:port]` patterns, no regular expression
- `Vary: Origin` on every response, so a cache never serves one origin's answer to another
- Only `OPTIONS` with `Origin` and `Access-Control-Request-Method` is a preflight: your own `OPTIONS` routes keep working
- The request's `Access-Control-Request-Headers` is never echoed back
- Refuses a WebSocket handshake from an origin you did not list (cross-site WebSocket hijacking)
- Every header value is built once at startup; at most one allocation per request

## Usage

```go
package main

import (
	"time"

	"go.owlkei.com/owlkei"
)

func main() {
	app := owlkei.New(&owlkei.AppOptions{
		Origins: []string{"https://app.example.com", "https://*.example.com"},
		CORS: &owlkei.CORSOptions{
			IsEnabled:          true,
			ExposeHeaders:      []string{"X-Total-Count"},
			MaxAge:             time.Hour,
			IsAllowCredentials: true,
		},
	})

	app.Create(
		owlkei.ModuleBuilder().Build(),
	)
}
```

A preflight from `https://app.example.com` answers `204` with:

```console
Access-Control-Allow-Origin: https://app.example.com
Access-Control-Allow-Credentials: true
Access-Control-Allow-Methods: GET, HEAD, PUT, PATCH, POST, DELETE
Access-Control-Allow-Headers: Content-Type, Authorization
Access-Control-Expose-Headers: X-Total-Count
Access-Control-Max-Age: 3600
Vary: Origin
```

## `CORS` Struct

### AllowOrigins
Type: `[]string`

Default: none

Required: `true`

The origins whose pages may read your responses, at most 256:

- `https://app.example.com`, `http://localhost:3000`, `https://[::1]:8080`: one exact origin,
  in lower case, with no path and no trailing `/`.
- `https://*.example.com`, `https://*.example.com:8443`: any subdomain of `example.com` at any
  depth, with that scheme and port. Not `example.com` itself.
- `*` alone: every origin, answered with `Access-Control-Allow-Origin: *`. Not with
  `IsAllowCredentials`.

```go
cors.CORS{AllowOrigins: []string{"https://app.example.com"}}
```

### AllowMethods
Type: `[]string`

Default: `GET, HEAD, PUT, PATCH, POST, DELETE`

Required: `false`

The methods a preflight allows, at most 64 RFC 9110 tokens.

### AllowHeaders
Type: `[]string`

Default: `Content-Type, Authorization` (what a JSON API with a bearer token needs beyond the Fetch
safelist)

Required: `false`

The request headers a preflight allows, at most 64 RFC 9110 tokens. The list is always yours: the
headers the browser asks for are never copied.

### ExposeHeaders
Type: `[]string`

Default: none

Required: `false`

Response headers the page's script may read beyond the safelisted ones, at most 64 tokens.

### MaxAge
Type: `time.Duration`

Default: `10 * time.Minute`

Required: `false`

How long the browser may cache a preflight, sent in whole seconds. 600 seconds is accepted by every
browser (Chromium caps at 7,200, Firefox at 86,400). `0` or a negative value means the default.

### IsAllowCredentials
Type: `bool`

Default: `false`

Required: `false`

Lets pages send cookies and HTTP authentication with their requests and read the answer
(`Access-Control-Allow-Credentials: true`). Only for listed origins.

### IsAllowPrivateNetwork
Type: `bool`

Default: `false`

Required: `false`

Answers `Access-Control-Allow-Private-Network: true` to a preflight that asks for it with
`Access-Control-Request-Private-Network: true` (WICG Private Network Access), so a public page may
call your app on a private network (a device, an intranet service).

## `CORS` Methods

### NewMiddleware
```go
func (instance CORS) NewMiddleware() owlkei.MiddlewareFn
```

Validates and compiles the options once; the framework calls it at `Create()`. It panics with
`invalid app: CORS needs Origins` without `Origins`, and with `invalid app: ...` for `*`
together with other entries or with `IsAllowCredentials`, an origin that is not
`scheme://host[:port]` in lower case or `scheme://*.host[:port]`, the entry `null`, more than 256
origins, more than 64 methods or headers, and a method or header name that is not an RFC 9110
token.

### Use
```go
func (instance CORS) Use(r *http.Request, w http.ResponseWriter, next owlkei.Next)
```

Makes `CORS` a `owlkei.MiddlewareFn`. It compiles the options on every call, so use it in tests
only; the framework always calls `NewMiddleware`.

## Rules
- With a list (not `*`), every response carries `Vary: Origin`, added to an existing `Vary`: also
  without an `Origin` header and for an origin that does not match.
- A request without `Origin` reaches its route with no CORS header.
- **Actual request, matching origin:** `Access-Control-Allow-Origin` is the request's origin (`*`
  with `*`), plus `Access-Control-Allow-Credentials: true` with `IsAllowCredentials` and
  `Access-Control-Expose-Headers` when set; then the route runs.
- **Actual request, other origin:** no CORS header, and the route still runs. The browser blocks
  the page from reading the answer; CORS is not access control.
- **Preflight** (`OPTIONS` with `Origin` and `Access-Control-Request-Method`), matching origin:
  the headers above plus `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers` and
  `Access-Control-Max-Age`, status `204`, `Content-Length: 0`; the route does not run.
- **Preflight, other origin:** `204` with `Vary` only.
- Any other `OPTIONS` request is an actual request and reaches your `PREFLIGHT_…` route.
- An origin matches by exact string, or by a `*.` pattern whose wildcard part is only `a-z`, `0-9`,
  `-` and `.`: `https://evil.example/.example.com`, `https://a_b.example.com`,
  `https://A.example.com` and `https://a.example.com.` do not match `https://*.example.com`. An
  `Origin` longer than 256 bytes matches nothing.
- **WebSocket** (`Upgrade: websocket`, any case): an `Origin` that does not match is refused with
  `403`, message `Origin not allowed`; a matching or absent `Origin` continues. No CORS header is
  written. The WebSocket handshake has its own Origin check (the `origin` stage).
- Header values are shared between responses (built once at startup). Change one only with
  `Header().Set`, `Add` or `Del`, never by writing into the slice in place
  (`w.Header()["Access-Control-Allow-Methods"][0] = "…"`): that changes every other response and is a data race.
- The request and its context are passed on unchanged, and the body is never read.

## Limits
- CORS only controls what a browser page may read. It does not stop a request, and clients other
  than browsers ignore it. Protect state changes with `csrf` and your guards.

## Benchmarks

Machine-dependent; captured on an Intel i7-9750H, Go 1.27.4, when this README was written
(`go test -run='^$' -bench . -benchmem -count 5 -cpu 1,2 ./internal/middlewares/cors`, median shown).

| Benchmark | GOMAXPROCS 1 | GOMAXPROCS 2 | B/op | allocs/op |
|---|---|---|---|---|
| `CORS_NoOrigin` | 38.6 ns | 38.3 ns | 0 | 0 |
| `CORS_ActualListedOrigin` | 133 ns | 113 ns | 16 | 1 |
| `CORS_ActualWildcardOrigin` | 147 ns | 129 ns | 16 | 1 |
| `CORS_OriginNotAllowed` | 78.7 ns | 78.0 ns | 0 | 0 |
| `CORS_Preflight` | 214 ns | 177 ns | 16 | 1 |
