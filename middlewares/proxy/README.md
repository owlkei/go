# Proxy Middleware

*`proxy` tells your app who the client really is when it runs behind a load balancer or reverse proxy: the client IP, the scheme (`http` or `https`) and the host, read from proxy headers only when they come from a proxy you trust.*

- [Proxy Middleware](#proxy-middleware)
  - [Key Features](#key-features)
  - [Why you need it](#why-you-need-it)
  - [Usage](#usage)
  - [`Proxy` Struct](#proxy-struct)
    - [TrustedProxies](#trustedproxies)
    - [TrustedHops](#trustedhops)
    - [Header](#header)
    - [IsHostForwarded](#ishostforwarded)
  - [Constants](#constants)
  - [Functions](#functions)
    - [ClientIP](#clientip)
    - [Scheme](#scheme)
    - [Host](#host)
  - [`Proxy` Methods](#proxy-methods)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [Configure your proxy](#configure-your-proxy)
    - [nginx](#nginx)
    - [Caddy](#caddy)
  - [How the client is chosen](#how-the-client-is-chosen)
  - [Limits](#limits)
  - [Security](#security)
  - [Benchmarks](#benchmarks)

## Key Features
- No proxy header is ever trusted until you configure the proxies in front of the app
- Client = the first address from the right that is not one of your proxies (MDN "Selecting an IP address")
- `X-Forwarded-For` / `-Proto` / `-Host` or `Forwarded` (RFC 7239): one family, never both
- The scheme is the value your nearest proxy wrote; values a client added before it never count
- The forwarded host is off until you state that your proxies set it (`IsHostForwarded`)
- The result is always a parsed `netip.Addr`, never header text: it cannot inject into logs or rate limit keys
- Hostile headers are bounded: 16 entries, 512 bytes per entry, 255 bytes per host
- No allocation per request; never refuses a request and never changes it

## Why you need it

Behind a proxy, every request arrives from the proxy's address. Without `proxy`, the access log,
the throttler, `hostcheck`, `httpsredirect` and `helmet` all see the proxy, not the client. Reading
`X-Forwarded-For` yourself is unsafe: any client can send that header, so one header per request
gives a new identity (a rate limit bypass) or a fake one in your logs. `proxy` reads those headers
only from a peer you list, and only the entries your proxies wrote.

Without `proxy` bound, nothing in Ginject reads a forwarding header: the client is `r.RemoteAddr`.

## Usage

Bind `proxy` first, before every middleware that reads the client:

```go
package main

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/middlewares/proxy"
)

type WhoController struct {
	ginject.HTTP
}

func (instance WhoController) NewController() ginject.Controller {
	return instance
}

func (instance WhoController) READ_who(c ginject.HTTPContext) string {
	return proxy.ClientIP(c.Request).String() + " " + proxy.Scheme(c.Request) + " " + proxy.Host(c.Request)
}

func main() {
	app := ginject.New()

	app.BindGlobalMiddlewares(proxy.Proxy{
		TrustedProxies: []string{"10.0.0.0/8"},
	})

	app.Create(
		ginject.ModuleBuilder().
			Controllers(WhoController{}).
			Build(),
	)
}
```

A request for `example.com` from the load balancer `10.0.0.7` with
`X-Forwarded-For: 198.51.100.20`, `X-Forwarded-Proto: https` and
`X-Forwarded-Host: app.example.com` answers `198.51.100.20 https example.com`. The host is
`r.Host` because `IsHostForwarded` is off. With `IsHostForwarded: true`, the same request answers
`198.51.100.20 https app.example.com`. The same headers from `203.0.113.5` (not a trusted proxy)
answer `203.0.113.5 http example.com`.

## `Proxy` Struct

Set exactly one of `TrustedProxies` and `TrustedHops`.

### TrustedProxies
Type: `[]string`

Default: none

Required: one of `TrustedProxies` and `TrustedHops`

The IP addresses or CIDRs of the proxies in front of the app: `"10.0.0.0/8"`, `"203.0.113.7"`,
`"fd00::/8"`. A single IP trusts exactly that address. Prefer this mode when you know your proxies'
addresses (a VPC range, a container network). At most 256 entries.

```go
proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8", "172.16.0.0/12"}}
```

### TrustedHops
Type: `int`

Default: `0` (off)

Required: one of `TrustedProxies` and `TrustedHops`

How many proxies are in front of the app, when their addresses are unknown (a managed platform
that does not publish its ranges). With `TrustedHops: n`, the client is entry number `n` from the
right of the header. Use it only when every request really passes through exactly `n` proxies: a
client that reaches the app directly could otherwise choose its own address. Between 1 and 16.

```go
proxy.Proxy{TrustedHops: 1}
```

### Header
Type: `proxy.Header`

Default: `proxy.HeaderXForwarded`

Required: `false`

The header family your proxies write. `HeaderXForwarded` reads `X-Forwarded-For`, `X-Forwarded-Proto`
and `X-Forwarded-Host` (what nearly every proxy writes). `HeaderForwarded` reads the standard
`Forwarded` header of RFC 7239 (`for=`, `proto=`, `host=`). The other family is never read, because
mixing them lets a client add entries the proxy does not overwrite.

```go
proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}, Header: proxy.HeaderForwarded}
```

### IsHostForwarded
Type: `bool`

Default: `false`

Required: `false`

Reads the host the client asked for from `X-Forwarded-Host` (with `HeaderXForwarded`) or from `host=`
(with `HeaderForwarded`). Turn it on only when every trusted proxy sets that header itself and never
passes a client's value through. Many proxy setups don't: the common nginx configuration sets
`Host` and leaves `X-Forwarded-Host` to the client. A host the client chooses ends up in your
redirects, links and password reset mails, so it is off by default and `proxy.Host` returns
`r.Host`, the host your proxy routed on.

You need it when your proxy rewrites `Host` to an internal name (for example nginx without
`proxy_set_header Host $host`, or a platform router), and `hostcheck`, `csrf` or `httpsredirect`
must see the public host. Bind `hostcheck` after `proxy` to accept only your own host names.

```go
proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}
```

## Constants

| Name | Type | Meaning |
|---|---|---|
| `HeaderXForwarded` | `Header` | `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host`. The zero value |
| `HeaderForwarded` | `Header` | `Forwarded` (RFC 7239) |

## Functions

These three functions are the one way to read who the client is, in handlers, guards (a throttler
`KeyFunc`) and your own middlewares. They work with or without `proxy` bound.

### ClientIP
```go
func ClientIP(r *http.Request) netip.Addr
```

Returns the client's address: the one `proxy` resolved, else the IP of `r.RemoteAddr`. IPv4-mapped
IPv6 addresses are unmapped and zones removed. Returns the zero `netip.Addr` when `RemoteAddr` is
not `ip:port`.

```go
ip := proxy.ClientIP(c.Request)
if ip.IsValid() {
	fmt.Println(ip.String())
}
```

### Scheme
```go
func Scheme(r *http.Request) string
```

Returns `"https"` or `"http"`: the forwarded scheme when `proxy` resolved one, else `"https"` when
the connection itself is TLS (`r.TLS != nil`).

### Host
```go
func Host(r *http.Request) string
```

Returns the host the client asked for: `r.Host`, or the forwarded host when `IsHostForwarded` is on
and `proxy` resolved a valid one. A forwarded host is accepted only when it is at most 255 bytes of
letters, digits, `.`, `-` and an optional `:port`, or a bracketed IPv6 literal with an optional
port.

## `Proxy` Methods

### NewMiddleware
```go
func (instance Proxy) NewMiddleware() ginject.MiddlewareFn
```

Validates the options and compiles them once. The framework calls it for you at `Create()`. It
panics with `invalid proxy: set TrustedProxies or TrustedHops, not both`, `invalid proxy: set
TrustedProxies or TrustedHops`, for an entry that is neither an IP nor a CIDR, for more than 256
entries, and for `TrustedHops` outside 1-16.

### Use
```go
func (instance Proxy) Use(r *http.Request, w http.ResponseWriter, next ginject.Next)
```

Makes `Proxy` a `ginject.MiddlewareFn`. It compiles the options on every call, so use it in tests
only; the framework always calls `NewMiddleware`.

## Configure your proxy

The proxy nearest to the app must set `X-Forwarded-For` (append the peer it saw) and
`X-Forwarded-Proto` (overwrite it with the scheme it received). `proxy` reads only the last value
of `X-Forwarded-Proto`, so a value a client sends never decides the scheme once the nearest proxy
writes its own. Without that header, a TLS-terminating proxy makes every request look like plain
HTTP, and `httpsredirect` redirects forever. Set `X-Forwarded-Host` only when you turn on
`IsHostForwarded`, and then always overwrite it.

### nginx
nginx keeps the client's `X-Forwarded-Host` unless you set it, so overwrite every header you rely
on:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

```go
proxy.Proxy{TrustedProxies: []string{"127.0.0.1"}}
```

If nginx must send an internal `Host` to the app, also add
`proxy_set_header X-Forwarded-Host $host;` and use `IsHostForwarded: true`.

### Caddy
Caddy's `reverse_proxy` keeps `Host` and writes `X-Forwarded-For`, `X-Forwarded-Proto` and
`X-Forwarded-Host` itself. It replaces the values a client sends unless the client is in Caddy's
own `trusted_proxies`:

```caddy
app.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

```go
proxy.Proxy{TrustedProxies: []string{"127.0.0.1"}}
```

Because Caddy writes `X-Forwarded-Host` itself, you can turn on `IsHostForwarded` behind it. You
need it only when you change the upstream host, for example with
`header_up Host {upstream_hostport}`.

## How the client is chosen

- **Peer not trusted** (`TrustedProxies` mode): the headers are ignored, the client is the peer.
- **Peer trusted:** the entries are read from right to left, across repeated header lines (last
  line first). The client is the first entry that is not a trusted proxy. When every entry is
  trusted, it is the leftmost entry read; with no entry, the peer.
- **Hops mode:** the client is entry `n` from the right. With fewer than `n` entries, the peer.
- **Invalid entries** (`unknown`, `_hidden`, a host name, a zone, an empty entry, an entry over 512
  bytes) end the walk: the client is the last valid address read, or the peer. In hops mode an
  invalid entry at or right of position `n` gives the peer.
- **Scheme and host** are read only when the client came from a header entry. When the peer isn't
  trusted, the header has no valid entry, or hops mode finds too few entries, the connection's
  scheme and `r.Host` are kept.
- **Scheme:** with `XForwarded`, the last value of `X-Forwarded-Proto` (the last header line, the
  text after its last comma, spaces trimmed). Earlier values are never used. With `Forwarded`, the
  `proto` of the client's element or of the nearest element to its right. Only `http` and `https`
  (any case) are accepted; anything else (an empty last value, `ftp`, a value over 512 bytes) keeps
  the connection's scheme.
- **Host** (only with `IsHostForwarded`): the last value of `X-Forwarded-Host`, selected like the
  scheme, or the `host` of the client's `Forwarded` element or of the nearest element to its
  right. An invalid value keeps `r.Host`.
- Entries a client writes left of the real chain never move the result.

## Limits
- At most 16 entries are read; a seventeenth ends the walk.
- `proxy` never refuses a request, writes no response header and never changes `r.RemoteAddr`,
  `r.Host`, `r.URL` or `r.Header`. The peer stays in `r.RemoteAddr` for audits.
- The client IP is personal data in many jurisdictions: the access log prints it as `ip`.

## Security
- Configure the nearest proxy to overwrite `X-Forwarded-Proto`. If it passes the client's header
  through, a client can claim HTTPS on a plain connection.
- Turn on `IsHostForwarded` only when every trusted proxy overwrites `X-Forwarded-Host` (or
  `host=`). `proxy` can't tell a value your proxy wrote from one it passed through: only your proxy
  configuration can.
- Bind `proxy` before every reader. A reader bound before it sees the connection's values, which
  fails safe (for example `httpsredirect` keeps redirecting).
- `TrustedHops` doesn't check who the peer is: it trusts the last `n` entries of any request. Use
  it only when clients can't reach the app except through your proxies (the app listens on a
  private network or on `127.0.0.1`, or a firewall allows only the proxies). A client that
  connects directly sends its own `X-Forwarded-For` and chooses the address the access log and the
  throttler see. When you know the proxies' addresses, use `TrustedProxies` instead.
- `X-Real-IP`, `True-Client-IP` and similar headers are never read.

## Benchmarks

Machine-dependent; captured on an Intel i7-9750H, Go 1.27.4, when this README was written
(`go test -run='^$' -bench . -benchmem -count 5 -cpu 1,2 ./middlewares/proxy`, median shown).

| Benchmark | GOMAXPROCS 1 | GOMAXPROCS 2 | B/op | allocs/op |
|---|---|---|---|---|
| `Proxy_UntrustedPeer` | 58.8 ns | 59.0 ns | 0 | 0 |
| `Proxy_TrustedPeer_3Entries_4CIDRs` | 326 ns | 323 ns | 0 | 0 |
| `Proxy_Hops` | 146 ns | 146 ns | 0 | 0 |
| `Proxy_Forwarded` | 282 ns | 280 ns | 0 | 0 |
| `ClientIP_Resolved` | 4.2 ns | 4.2 ns | 0 | 0 |
| `ClientIP_Unresolved` | 43.1 ns | 42.9 ns | 0 | 0 |
