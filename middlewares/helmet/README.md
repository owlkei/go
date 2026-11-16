# Helmet Middleware

*`helmet` sets the HTTP security response headers that OWASP recommends, with safe defaults that suit both JSON APIs and server-rendered HTML pages.*

- [Helmet Middleware](#helmet-middleware)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [Headers and defaults](#headers-and-defaults)
  - [Constants](#constants)
  - [`Helmet` Struct](#helmet-struct)
    - [ContentSecurityPolicy](#contentsecuritypolicy)
    - [CrossOriginEmbedderPolicy](#crossoriginembedderpolicy)
    - [CrossOriginOpenerPolicy](#crossoriginopenerpolicy)
    - [CrossOriginResourcePolicy](#crossoriginresourcepolicy)
    - [FrameOptions](#frameoptions)
    - [PermissionsPolicy](#permissionspolicy)
    - [ReferrerPolicy](#referrerpolicy)
    - [StrictTransportSecurity](#stricttransportsecurity)
  - [`Helmet` Methods](#helmet-methods)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [Rules](#rules)
  - [Benchmarks](#benchmarks)

## Key Features
- A strict Content Security Policy by default (`'self'` only, no inline script attributes, no framing)
- `Strict-Transport-Security` only over HTTPS (RFC 6797 § 7.2), read through `proxy.Scheme`
- One rule for every field: `""` is the default, `helmet.Disabled` turns the header off, any other value is sent as written
- `Permissions-Policy` denies powerful browser features by default
- Every header value is built once at startup; no allocation per request

## Usage

```go
package main

import (
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/middlewares/helmet"
)

func main() {
	app := core.New()

	app.BindGlobalMiddlewares(helmet.Helmet{
		ContentSecurityPolicy: "default-src 'self'; img-src 'self' https: data:",
		FrameOptions:          helmet.Disabled,
	})

	app.Create(
		core.ModuleBuilder().Build(),
	)
}
```

## Headers and defaults

| Header | Default | Source |
|---|---|---|
| `Content-Security-Policy` | `default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests` | OWASP Secure Headers, CSP Cheat Sheet |
| `Cross-Origin-Embedder-Policy` | not sent | MDN: `require-corp` blocks every cross-origin image and script without CORP |
| `Cross-Origin-Opener-Policy` | `same-origin` | OWASP Secure Headers |
| `Cross-Origin-Resource-Policy` | `same-origin` | OWASP Secure Headers |
| `X-Frame-Options` | `DENY` | OWASP Secure Headers; matches `frame-ancestors 'none'` |
| `Permissions-Policy` | `accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=(), browsing-topics=()` | OWASP Secure Headers |
| `Referrer-Policy` | `no-referrer` | OWASP Secure Headers |
| `Strict-Transport-Security` | `max-age=63072000; includeSubDomains`, over HTTPS only, never `preload` by default | OWASP HSTS Cheat Sheet; RFC 6797 |
| `X-Content-Type-Options` | `nosniff` (always) | OWASP Secure Headers |
| `X-XSS-Protection` | `0` (always) | OWASP: the old auditor is removed or harmful |
| `X-DNS-Prefetch-Control` | `off` (always) | |
| `X-Permitted-Cross-Domain-Policies` | `none` (always) | OWASP Secure Headers |
| `Origin-Agent-Cluster` | `?1` (always) | |

`X-Download-Options` is no longer sent: only Internet Explorer read it.

The default CSP allows scripts, styles, images and fonts from your own origin only. A page with
inline `<style>` or `<script>`, or assets on a CDN, must set its own policy. Serve your assets
(Owlky's CSS and JavaScript included) from your own origin and the default works as is.
`upgrade-insecure-requests` makes the browser load `http:` subresources over HTTPS; when you test
plain HTTP from another device (not `localhost`), set your own policy without it.

## Constants

| Name | Value | Meaning |
|---|---|---|
| `Disabled` | `"-"` | Field value that turns one header off |

## `Helmet` Struct

Every field is a `string`, defaults to `""` (the default of the table above), is not required, and
follows the same rule: `""` sends the default, `helmet.Disabled` sends nothing, any other value is
sent as written.

### ContentSecurityPolicy
Type: `string`

Default: see [Headers and defaults](#headers-and-defaults)

Required: `false`

The `Content-Security-Policy` header: which sources the page may load scripts, styles, images,
frames and forms from.

```go
helmet.Helmet{ContentSecurityPolicy: "default-src 'self'; img-src 'self' https: data:"}
```

### CrossOriginEmbedderPolicy
Type: `string`

Default: not sent

Required: `false`

The `Cross-Origin-Embedder-Policy` header. Set `require-corp` or `credentialless` when the page
needs cross-origin isolation (`SharedArrayBuffer`).

### CrossOriginOpenerPolicy
Type: `string`

Default: `same-origin`

Required: `false`

The `Cross-Origin-Opener-Policy` header. `same-origin-allow-popups` keeps OAuth popups working.

### CrossOriginResourcePolicy
Type: `string`

Default: `same-origin`

Required: `false`

The `Cross-Origin-Resource-Policy` header. Set `cross-origin` for a public asset or API server
that other sites embed.

### FrameOptions
Type: `string`

Default: `DENY`

Required: `false`

The `X-Frame-Options` header, for browsers that do not read `frame-ancestors`.

### PermissionsPolicy
Type: `string`

Default: see [Headers and defaults](#headers-and-defaults)

Required: `false`

The `Permissions-Policy` header: which browser features (camera, geolocation, payment) the page may
use.

### ReferrerPolicy
Type: `string`

Default: `no-referrer`

Required: `false`

The `Referrer-Policy` header. `strict-origin-when-cross-origin` keeps the origin for analytics.

### StrictTransportSecurity
Type: `string`

Default: `max-age=63072000; includeSubDomains`

Required: `false`

The `Strict-Transport-Security` header, sent over HTTPS only. Add `preload` only after you have
read hstspreload.org: leaving the preload list takes months.

```go
helmet.Helmet{StrictTransportSecurity: "max-age=63072000; includeSubDomains; preload"}
```

## `Helmet` Methods

### NewMiddleware
```go
func (instance Helmet) NewMiddleware() common.MiddlewareFn
```

Builds every header value once; the framework calls it at `Create()`. It panics with
`invalid helmet: ...` for a value with a control character (a line break, `NUL`, `DEL`, an escape).

### Use
```go
func (instance Helmet) Use(r *http.Request, w http.ResponseWriter, next ctx.Next)
```

Makes `Helmet` a `common.MiddlewareFn`. It compiles the options on every call, so use it in tests
only; the framework always calls `NewMiddleware`.

## Rules
- `Strict-Transport-Security` is sent only when `proxy.Scheme(r)` is `https`: a direct TLS
  connection, or `https` forwarded by a trusted `proxy`. An untrusted client cannot claim HTTPS,
  and with no `proxy` bound no forwarding header is read. A custom value follows the same rule.
- The always-sent headers are on every response that passes `helmet`, a `404` or `405` included,
  even when every configurable header is `Disabled`.
- Headers are set before the next layer runs, so a later middleware or the handler may overwrite
  one.
- `Header().Add` on a header `helmet` set never changes what other responses send.
- The request and its context are passed on unchanged, and the body is never read.

## Benchmarks

Machine-dependent; captured on an Intel i7-9750H, Go 1.27.4, when this README was written
(`go test -run='^$' -bench . -benchmem -count 5 -cpu 1,2 ./middlewares/helmet`, median shown).

| Benchmark | GOMAXPROCS 1 | GOMAXPROCS 2 | B/op | allocs/op |
|---|---|---|---|---|
| `Helmet_HTTP` | 196 ns | 193 ns | 0 | 0 |
| `Helmet_HTTPS` | 213 ns | 210 ns | 0 | 0 |
