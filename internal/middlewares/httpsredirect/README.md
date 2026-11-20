# HTTPS Redirect Middleware

Internal package: applications use `AppOptions.HTTPSRedirect` (`owlky.HTTPSRedirectOptions`) instead; it is the `httpsredirect` pre-route stage. It does not run on the WebSocket handshake.

*`httpsredirect` sends every plain HTTP request to the same address over HTTPS with a `308 Permanent Redirect`, and can never be turned into an open redirect.*

- [HTTPS Redirect Middleware](#https-redirect-middleware)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`HTTPSRedirect` Struct](#httpsredirect-struct)
    - [Host](#host)
    - [Port](#port)
  - [`HTTPSRedirect` Methods](#httpsredirect-methods)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [Rules](#rules)
  - [Benchmarks](#benchmarks)

## Key Features
- `308`, so browsers repeat the method and the body (RFC 9110 § 15.4.9)
- Path and query kept, re-encoded; the target always starts with `https://` and a validated host
- Reads the scheme from `owlky.ClientScheme`, so a TLS-terminating proxy that sends `X-Forwarded-Proto: https` passes
- A fixed `Host` for production closes redirects built from a client's `Host` header
- No allocation on HTTPS requests

## Usage

```go
package main

import (
	"go.owlky.com/owlky"
)

func main() {
	app := owlky.New(&owlky.AppOptions{
		Proxy:         &owlky.ProxyOptions{TrustedProxies: []string{"10.0.0.0/8"}},
		HTTPSRedirect: &owlky.HTTPSRedirectOptions{IsEnabled: true, Host: "www.example.com"},
	})

	app.Create(
		owlky.ModuleBuilder().Build(),
	)
}
```

`GET http://example.com/a/b?x=1` answers `308` with `Location: https://www.example.com/a/b?x=1`
and no body.

Pair it with `helmet`: its `Strict-Transport-Security` header (sent over HTTPS only) makes the
browser skip the plain HTTP request next time.

## `HTTPSRedirect` Struct

### Host
Type: `string`

Default: `""` (the request's host, without its port)

Required: `false`

The host to redirect to. Set it in production: the target is then never built from the client's
`Host` header. It must be a host name or an IP literal without a port.

```go
httpsredirect.HTTPSRedirect{Host: "www.example.com"}
```

### Port
Type: `int`

Default: `0` (port 443, so no port in the target)

Required: `false`

The HTTPS port, when it is not 443. `0` and `443` add no port. Between 0 and 65535.

```go
httpsredirect.HTTPSRedirect{Port: 8443}
```

## `HTTPSRedirect` Methods

### NewMiddleware
```go
func (instance HTTPSRedirect) NewMiddleware() owlky.MiddlewareFn
```

Validates the options once; the framework calls it at `Create()`. It panics with
`invalid app: HTTPSRedirect.Host ...` for a `Host` that is not a valid host or that has a port
(`example.com:8080`, `*.example.com`, `https://example.com`, `user@example.com`), and for a `Port`
outside 0-65535.

### Use
```go
func (instance HTTPSRedirect) Use(r *http.Request, w http.ResponseWriter, next owlky.Next)
```

Makes `HTTPSRedirect` a `owlky.MiddlewareFn`. It compiles the options on every call, so use it in
tests only; the framework always calls `NewMiddleware`.

## Rules
- When `owlky.ClientScheme(r)` is `https`, the next layer runs and nothing else happens.
- Otherwise the response is `308` with `Location: https://<host><:port><path>[?<query>]` and no
  body; the next layer does not run, for every method.
- `<host>` is `Host` when set, else the request host without its port (`[::1]:8080` becomes
  `[::1]`). `<:port>` appears only when `Port` is set and is not 443.
- With no `Host` set, a request host that is empty, longer than 255 bytes or not made of letters,
  digits, `.`, `-` and an optional port (or a bracketed IPv6 literal) answers `400` with message
  `Invalid host`, and no location is built.
- No path can change the target: `//evil.example`, `/\evil.example`, `/@evil.example` and encoded
  CR or LF stay in the path of `https://<host>/`.
- With no `proxy` bound, `X-Forwarded-Proto` and `Forwarded` are never read: an untrusted client
  cannot claim HTTPS.
- Behind a TLS-terminating proxy, bind `proxy` before `httpsredirect`, or every request looks like
  plain HTTP and redirects forever.
- Behind `proxy`, the scheme is the last `X-Forwarded-Proto` value, the one your nearest proxy
  wrote: a client's `https` before it doesn't turn the redirect off. Configure that proxy to
  overwrite the header.
- The redirect goes to the request's `Host` unless `proxy` has `IsHostForwarded` on. A
  `X-Forwarded-Host` that a client sends can't move the redirect to another host by default.

## Benchmarks

Machine-dependent; captured on an Intel i7-9750H, Go 1.27.4, when this README was written
(`go test -run='^$' -bench . -benchmem -count 5 -cpu 1,2 ./internal/middlewares/httpsredirect`, median shown).

| Benchmark | GOMAXPROCS 1 | GOMAXPROCS 2 | B/op | allocs/op |
|---|---|---|---|---|
| `HTTPSRedirect_HTTPS` | 5.8 ns | 5.6 ns | 0 | 0 |
| `HTTPSRedirect_Redirect` | 197 ns | 155 ns | 48 | 2 |
