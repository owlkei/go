# CSRF Middleware

*`csrf` refuses cross-origin state-changing requests from browsers, using the Fetch Metadata `Sec-Fetch-Site` header and the `Origin` header, with no token, no cookie and no body read.*

- [CSRF Middleware](#csrf-middleware)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [How it decides](#how-it-decides)
  - [`CSRF` Struct](#csrf-struct)
    - [TrustedOrigins](#trustedorigins)
  - [`CSRF` Methods](#csrf-methods)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [Rules](#rules)
  - [Limits](#limits)
  - [Benchmarks](#benchmarks)

## Key Features
- The decision is the standard library's `http.CrossOriginProtection` (Go 1.25+), maintained by the Go team
- Works for forms and `fetch` alike: nothing to add to your pages or your front end
- No token to leak, no cookie to fixate, nothing stored in the request context
- Requests from non-browser clients (curl, mobile apps, servers) pass untouched
- Behind a trusted `proxy` with `IsHostForwarded` on, the `Origin` is compared with the forwarded host
- No allocation on the common paths

## Usage

```go
package main

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/middlewares/csrf"
)

func main() {
	app := ginject.New()

	app.BindGlobalMiddlewares(csrf.CSRF{
		TrustedOrigins: []string{"https://app.example.com"},
	})

	app.Create(
		ginject.ModuleBuilder().Build(),
	)
}
```

A `POST` that a page on `https://evil.example` makes the browser send gets:

```json
{"code":403,"error":"Forbidden","message":"Cross-origin request refused"}
```

## How it decides

Modern browsers send `Sec-Fetch-Site` on every request; older ones send `Origin` on unsafe
requests. Non-browser clients send neither.

| Method | `Sec-Fetch-Site` | `Origin` | Result |
|---|---|---|---|
| `GET`, `HEAD`, `OPTIONS` | any | any | pass |
| other | `same-origin` or `none` | any | pass |
| other | any other value (`cross-site`, `same-site`, anything else) | any | `Origin` in `TrustedOrigins`: pass; else `403` |
| other | absent | absent | pass (not a browser) |
| other | absent | its host equals the request's host | pass (a browser from before 2023, or plain HTTP) |
| other | absent | another host, `null` or unparsable | `Origin` in `TrustedOrigins`: pass; else `403` |

## `CSRF` Struct

### TrustedOrigins
Type: `[]string`

Default: none

Required: `false`

Origins allowed to send cross-origin unsafe requests: a front end on another domain, or an
identity provider that posts back to your app. Each entry is `scheme://host[:port]`, at most 64. A
trusted origin matches exactly: another scheme, port or subdomain is not trusted, and `null` never
is.

```go
csrf.CSRF{TrustedOrigins: []string{"https://app.example.com", "https://idp.example.org:8443"}}
```

## `CSRF` Methods

### NewMiddleware
```go
func (instance CSRF) NewMiddleware() ginject.MiddlewareFn
```

Validates and compiles the options once; the framework calls it at `Create()`. It panics with
`invalid csrf: ...` for an origin that is not `scheme://host[:port]` (`example.com`,
`https://example.com/path`, `*`, `//example.com`, a query or a fragment) and for more than 64
origins.

### Use
```go
func (instance CSRF) Use(r *http.Request, w http.ResponseWriter, next ginject.Next)
```

Makes `CSRF` a `ginject.MiddlewareFn`. It compiles the options on every call, so use it in tests
only; the framework always calls `NewMiddleware`.

## Rules
- Every row of the table above holds for `POST`, `PUT`, `PATCH` and `DELETE`.
- `same-site` is refused: a sibling subdomain (`https://sub.example.com`) is another origin.
- The `Sec-Fetch-Site` value is compared exactly (`Cross-Site` is refused like `cross-site`).
- For an unsafe method, an `Origin` longer than 256 bytes is refused before any parsing, even when
  it equals the host.
- The origin's host and port must both equal the request's: `http://example.com:9090` is refused
  on `example.com:8080`.
- Behind a trusted `proxy` with `IsHostForwarded` on and a forwarded host, and without `Sec-Fetch-Site`, the origin's host
  is compared with `proxy.Host(r)`. With no `proxy` bound, `X-Forwarded-Host` and `Forwarded` are
  never read.
- No cookie and no token header is ever set, on a pass or a refusal. The body is never read; the
  request and its context are passed on unchanged.
- The refusal is the HTTP layer's error response; it never repeats the origin.

## Limits
- A state change behind `GET` is not protected: keep `GET` safe.
- Non-browser clients always pass: `csrf` is not authentication.
- The one browser case Fetch Metadata and `Origin` cannot tell apart, a plain HTTP page posting to
  the HTTPS site of the same host in a browser from before 2023, is closed by HSTS, which `helmet`
  sends.

## Benchmarks

Machine-dependent; captured on an Intel i7-9750H, Go 1.27.4, when this README was written
(`go test -run='^$' -bench . -benchmem -count 5 -cpu 1,2 ./middlewares/csrf`, median shown).

| Benchmark | GOMAXPROCS 1 | GOMAXPROCS 2 | B/op | allocs/op |
|---|---|---|---|---|
| `CSRF_SafeMethod` | 2.7 ns | 2.7 ns | 0 | 0 |
| `CSRF_SecFetchSiteSameOrigin` | 52.7 ns | 52.1 ns | 0 | 0 |
| `CSRF_OriginAgainstHost` | 295 ns | 270 ns | 144 | 1 |
| `CSRF_TrustedOrigin` | 103 ns | 102 ns | 0 | 0 |
