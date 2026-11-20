# Host Check Middleware

Internal package: applications use `AppOptions.Hosts` instead; it is the `hostcheck` pre-route stage, on when the list is not empty.

*`hostcheck` answers only for the host names you list and refuses every other `Host`, which closes DNS rebinding, host header injection and cache poisoning by host.*

- [Host Check Middleware](#host-check-middleware)
  - [Key Features](#key-features)
  - [Why you need it](#why-you-need-it)
  - [Usage](#usage)
  - [`HostCheck` Struct](#hostcheck-struct)
    - [Hosts](#hosts)
  - [`HostCheck` Methods](#hostcheck-methods)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [Rules](#rules)
  - [Benchmarks](#benchmarks)

## Key Features
- An allowlist of exact names, `*.` patterns and IP literals, each with an optional port
- `421 Misdirected Request` for an unknown host, `400 Bad Request` for an empty or overlong one (RFC 9110 § 15.5.20, § 7.2)
- Reads the host from `owlkei.ClientHost`, so a forwarded host counts only behind a trusted proxy with `IsHostForwarded` on
- No implicit entry: `localhost` passes only when you list it
- One map lookup per request, no allocation

## Why you need it

An attacker can point any domain at your server (DNS rebinding) or send any `Host` header. When
your app builds links, redirects or cache keys from the host, a forged host ends up in emails,
password reset links or a shared cache. OWASP WSTG-INPV-17 recommends validating `Host` against a
list: `hostcheck` does it before any guard or handler runs.

## Usage

```go
package main

import (
	"go.owlkei.com/owlkei"
)

func main() {
	app := owlkei.New(&owlkei.AppOptions{
		Proxy: &owlkei.ProxyOptions{TrustedProxies: []string{"10.0.0.0/8"}},
		Hosts: []string{"example.com", "*.example.com", "localhost"},
	})

	app.Create(
		owlkei.ModuleBuilder().Build(),
	)
}
```

A request with `Host: evil.example` gets:

```json
{"code":421,"error":"Misdirected Request","message":"Unknown host"}
```

## `HostCheck` Struct

### Hosts
Type: `[]string`

Default: none

Required: `true`

The hosts the app answers for, at most 256:

| Entry | Matches |
|---|---|
| `example.com` | `example.com` on any port, any case, with or without one trailing dot |
| `api.example.com:8443` | that host on port `8443` only |
| `*.example.com` | any subdomain of `example.com` at any depth (`a.example.com`, `a.b.example.com`), not `example.com` itself |
| `*.example.com:8443` | any subdomain on port `8443` only |
| `127.0.0.1`, `[::1]`, `[::1]:8080` | IP literals, with an optional port |

```go
hostcheck.HostCheck{Hosts: []string{"example.com", "www.example.com"}}
```

## `HostCheck` Methods

### NewMiddleware
```go
func (instance HostCheck) NewMiddleware() owlkei.MiddlewareFn
```

Validates and compiles `Hosts` once; the framework calls it at `Create()`. It panics with
`invalid app: Hosts has 300 entries, at most 256 are allowed` for more than 256 entries, and for an entry that is not a host
name, a `*.` pattern or an IP literal with an optional port (for example `*`, `*.*.example.com`,
`http://example.com`, `example.com/path`, `example.com:`, `-example.com`, `example..com`).

### Use
```go
func (instance HostCheck) Use(r *http.Request, w http.ResponseWriter, next owlkei.Next)
```

Makes `HostCheck` a `owlkei.MiddlewareFn`. It compiles the options on every call, so use it in
tests only; the framework always calls `NewMiddleware`.

## Rules
- The checked value is `owlkei.ClientHost(r)`: the forwarded host behind a trusted `proxy` with
  `IsHostForwarded` on, else `r.Host`. With no `proxy` bound, no forwarding header is read.
- A match calls the next layer; nothing else changes.
- No match answers `421` with message `Unknown host`. An empty host, or one longer than 255 bytes,
  answers `400` with message `Invalid host`. The response never repeats the host.
- A wildcard part may hold only letters, digits, `-` and `.`: `a_b.example.com`,
  `evil.com/.example.com` and `a@example.com` do not match `*.example.com`.
- On the WebSocket path (`AppOptions.WS`), a refusal refuses the handshake.
- The request and its context are passed on unchanged, and the body is never read.

## Benchmarks

Machine-dependent; captured on an Intel i7-9750H, Go 1.27.4, when this README was written
(`go test -run='^$' -bench . -benchmem -count 5 -cpu 1,2 ./internal/middlewares/hostcheck`, median shown).

| Benchmark | GOMAXPROCS 1 | GOMAXPROCS 2 | B/op | allocs/op |
|---|---|---|---|---|
| `HostCheck_Hit` | 36.0 ns | 36.0 ns | 0 | 0 |
| `HostCheck_HitWithPort` | 46.4 ns | 46.6 ns | 0 | 0 |
| `HostCheck_MixedCase` | 43.9 ns | 42.8 ns | 0 | 0 |
| `HostCheck_Wildcard` | 75.2 ns | 75.2 ns | 0 | 0 |
