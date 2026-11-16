# 0014: HTTP middlewares, static file caching and the throttler

Status: Approved
Packages: `middlewares/proxy` (new), `middlewares/hostcheck` (new), `middlewares/httpsredirect` (new),
`middlewares/compress` (new), `middlewares/loadshed` (new), `middlewares/csrf`, `middlewares/cors`,
`middlewares/helmet`, `guards/throttler`, `core`, `internal/tracecontext`, `internal/httphook` (new),
`internal/inspect` (new), `trace`, `accesslog`

Roadmap: `docs/roadmap/0005-middlewares-modules-security.md`, steps 3-10. Its Decisions are taken as
given. Design 0013 (request scope, parallel) and 0015 (config and cache, parallel) meet this one in
"Coordination".

## Goal
An app turns on the middlewares a production server needs (`loadshed`, `proxy`, `hostcheck`,
`httpsredirect`, `helmet`, `cors`, `csrf`, `compress`) with one line each and gets current-standard,
safe defaults. The client IP, scheme and host come from one place that trusts proxy headers only
from configured proxies; the throttler, the access log and the other middlewares read that place.
Static files get a cache policy, and the throttler is exact, bounded and has no idle goroutine.

## Findings in today's code
These drive the design; each is fixed by a rule below.

| # | Where | Finding |
|---|---|---|
| 1 | `core/http.go` `runMiddleware` | The chain is linear: `next()` only sets a flag, the handler runs after `Use` returns. A middleware cannot run code after the handler or see its writes. `compress` and `loadshed` need both (K rules) |
| 2 | `guards/throttler/throttler.go:191` | `X-Real-IP`, then `X-Forwarded-For`, are trusted from any peer: one header per request gives a new key, which bypasses the limit and grows the store without bound |
| 3 | `guards/throttler` strategies | `Backend.Get` then `Backend.Set` is not atomic: concurrent requests of one key lose counts (the limit is not exact) |
| 4 | `memorycache.NewMemoryCache` | Starts a sweeper goroutine per cache; `Throttler.NewGuard` creates one cache per guard and never stops it |
| 5 | `middlewares/csrf` | `*r = *r.WithContext(...)` per request, `r.FormValue` reads the body, cookie without `Secure` / `SameSite` |
| 6 | `middlewares/cors` | The zero value allows every origin; any `OPTIONS` with `Origin` is answered as a preflight (a real `OPTIONS` route never runs); no `Vary: Origin` on a response to a request without `Origin`; an empty `AllowHeaders` reflects whatever the client asks; `*regexp.Regexp` runs per request; `MaxAge` 5 s |
| 7 | `middlewares/helmet` | HSTS is sent over plain HTTP; no `Permissions-Policy`; no way to turn one header off; twelve `Header.Set` allocations per request |
| 8 | `(instance X) Use` on the option structs | Rebuilds the options on every call when used without `NewMiddleware` |

## Public API

Every option struct follows the existing layout (`middlewares/cors`): an exported struct with exported
fields only (`core.injectDependencies` refuses unexported fields), `NewMiddleware() common.MiddlewareFn`
that validates and compiles once, and a `Use` method so the struct is a `common.MiddlewareFn`. `Use` on
the option struct calls `instance.NewMiddleware().Use(r, w, next)` and is documented as "for tests";
the framework always calls `NewMiddleware` (`core.newHTTPMiddleware`). `container.construct` gives two
bindings with equal options one shared instance, so a `LoadShed` bound twice with the same values is
one limiter.

```go
package proxy // middlewares/proxy

type Header int

const (
    XForwarded Header = iota // X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host
    Forwarded                // Forwarded (RFC 7239)
)

type Proxy struct {
    TrustedProxies []string // IPs or CIDRs of the proxies in front: "10.0.0.0/8", "203.0.113.7"
    TrustedHops    int      // or: how many proxies are in front, when their addresses are unknown
    Header         Header   // which header family the proxies write; default XForwarded
    IsHostForwarded bool    // the proxies set the forwarded host: read it (Revision 2026-11-17)
}

func (instance Proxy) NewMiddleware() common.MiddlewareFn
func (instance Proxy) Use(r *http.Request, w http.ResponseWriter, next ctx.Next)

func ClientIP(r *http.Request) netip.Addr // the client's address; the zero Addr when unknown
func Scheme(r *http.Request) string       // "https" or "http"
func Host(r *http.Request) string         // the host the client asked for

package hostcheck // middlewares/hostcheck

type HostCheck struct {
    Hosts []string // "example.com", "api.example.com:8443", "*.example.com", "127.0.0.1", "[::1]"
}

func (instance HostCheck) NewMiddleware() common.MiddlewareFn
func (instance HostCheck) Use(r *http.Request, w http.ResponseWriter, next ctx.Next)

package httpsredirect // middlewares/httpsredirect

type HTTPSRedirect struct {
    Host string // redirect to this host instead of the request's; "" keeps the request's host
    Port int    // HTTPS port when it is not 443
}

func (instance HTTPSRedirect) NewMiddleware() common.MiddlewareFn
func (instance HTTPSRedirect) Use(r *http.Request, w http.ResponseWriter, next ctx.Next)

package compress // middlewares/compress

type Compress struct {
    MinSize int // smallest body that is compressed, in bytes; default 1024
}

func (instance Compress) NewMiddleware() common.MiddlewareFn
func (instance Compress) Use(r *http.Request, w http.ResponseWriter, next ctx.Next)

package loadshed // middlewares/loadshed

type LoadShed struct {
    MaxInFlight int // 0: the limit adapts to the machine and the measured latency (default); > 0: a fixed limit
    MaxQueue    int // requests that may wait for a slot; default 64; negative: no queue
}

func (instance LoadShed) NewMiddleware() common.MiddlewareFn
func (instance LoadShed) Use(r *http.Request, w http.ResponseWriter, next ctx.Next)

package csrf // middlewares/csrf (rebuilt)

type CSRF struct {
    TrustedOrigins []string // origins allowed to send cross-origin unsafe requests: "https://app.example.com"
}

func (instance CSRF) NewMiddleware() common.MiddlewareFn
func (instance CSRF) Use(r *http.Request, w http.ResponseWriter, next ctx.Next)

package cors // middlewares/cors (changed)

type CORS struct {
    AllowOrigins          []string      // "https://app.example.com", "https://*.example.com", or "*" alone
    AllowMethods          []string      // default GET, HEAD, PUT, PATCH, POST, DELETE
    AllowHeaders          []string      // default Content-Type, Authorization
    ExposeHeaders         []string
    MaxAge                time.Duration // default 10 minutes
    IsAllowCredentials    bool
    IsAllowPrivateNetwork bool
}

package helmet // middlewares/helmet (changed)

const Disabled = "-" // field value that turns one header off

type Helmet struct {
    ContentSecurityPolicy     string
    CrossOriginEmbedderPolicy string
    CrossOriginOpenerPolicy   string
    CrossOriginResourcePolicy string
    FrameOptions              string
    PermissionsPolicy         string
    ReferrerPolicy            string
    StrictTransportSecurity   string
}

package throttler // guards/throttler (changed)

func (g Throttler) NewGuard() common.Guarder // was: Throttler; returns the compiled guard

package trace

type Event struct {
    // ... existing fields
    ClientIP netip.Addr // added: set on `complete` events, the zero Addr elsewhere
}
```

| Exported name | Why users need it |
|---|---|
| `proxy.Proxy`, its three fields, `Header`, `XForwarded`, `Forwarded` | the trust configuration of roadmap step 3; mixing header families is a spoofing hole, so the family is named, not guessed |
| `proxy.ClientIP`, `Scheme`, `Host` | the one way to read who the client is, for handlers (`proxy.ClientIP(c.Request)`), user middlewares and `Throttler.KeyFunc` (Q1) |
| `hostcheck.HostCheck.Hosts` | the allowed hosts; nothing else is configurable |
| `httpsredirect.HTTPSRedirect.Host`, `Port` | a canonical host closes host-header redirects; the port for non-443 deployments |
| `compress.Compress.MinSize` | the one value apps tune; level, types and writer count are fixed (Q6) |
| `loadshed.LoadShed.MaxInFlight`, `MaxQueue` | the zero value adapts by itself (decided by the maintainer, 2026-11-17); a fixed limit is the explicit override; the wait, `Retry-After` and every constant of the adaptive limit are fixed |
| `csrf.CSRF.TrustedOrigins` | cross-origin front ends and identity providers that post back |
| `cors.CORS.IsAllowPrivateNetwork` | roadmap step 5 (private network access) |
| `helmet.Disabled`, `PermissionsPolicy`, `StrictTransportSecurity` | one way to turn any header off; the missing header; HSTS as one string like the others |
| `trace.Event.ClientIP` | the access log (a trace listener) prints it; `netip.Addr` is 24 B and needs no allocation |

Removed (breaking, allowed by the roadmap):

| Name | Why |
|---|---|
| `csrf.CSRF.TokenLength`, `CookieName`, `HeaderName`, `ContextKey`, `csrf.GenerateCSRFToken`, `csrf.CompareTokensSecurely` | no token, no cookie (step 4). The design finds no need for the token fallback: see S rules |
| `cors.CORS.AllowOrigin` (`any`), `AllowHeaders` / `ExposeHeaders` as `any`, `IsPreflightContinue`, `OptionsSuccessStatus` | typed lists replace `any`; no regexp on the request path; a non-preflight `OPTIONS` now continues by itself; `204` is right for every supported browser |
| `helmet.Helmet.DNSPrefetchControl`, `PermittedCrossDomainPolicies`, `HSTSMaxAge`, `HSTSExcludeSubDomains`, `HSTSPreload`, `DisableHSTS` | fixed values, or folded into `StrictTransportSecurity` |

Internal (not exported to users):

```go
package httphook // internal/httphook

type Hook interface{ Done(elapsed time.Duration) } // elapsed: since the request started

type Writer interface {
    Inner() http.ResponseWriter     // the writer the response goes to
    SetInner(w http.ResponseWriter) // put a writer in front of it
    OnDone(h Hook)                  // run h.Done once when the exchange ends
}

func Of(w http.ResponseWriter) Writer // nil when w is not the framework's writer

package tracecontext // internal/tracecontext

type Scope struct {
    // ... existing fields
    ClientIP       netip.Addr // written by proxy
    ClientHost     string     // "" means r.Host
    IsClientHTTPS  bool
    IsClientSet    bool       // proxy resolved this request
}

func ClientIP(r *http.Request) netip.Addr
func IsHTTPS(r *http.Request) bool
func Host(r *http.Request) string

package inspect // internal/inspect

type Field struct {
    Key   string
    Value any // string, bool, int64, float64, time.Duration or []string
}

type Snapshot struct {
    Name   string // "loadshed", "proxy", "throttler", ...
    Fields []Field
}

type Snapshotter interface{ Snapshot() Snapshot }
```

## Behavior

Shared rules:

- **G1** A refusal is `panic(<exception>)`, so the response is the HTTP layer's error response (core
  README › "Errors in middleware"): `Content-Type: application/json`, body
  `{"code":<n>,"error":"<status text>","message":"<message>"}`. No guard, interceptor, handler or
  exception filter runs. Messages are fixed strings; nothing from the request is ever in a response
  body or header, except an allowed `Origin` echoed by `cors` and the redirect target of R2.
- **G2** No middleware here replaces or copies the request: the `*http.Request` and its `Context()`
  seen by the next layer are the ones this middleware received (pointer-equal).
- **G3** No middleware here reads the request body.
- **G4** Invalid options panic in `NewMiddleware` (so at `Create()`), with `color.FmtRed`, as
  `"invalid <package>: <detail>"` (the format `cors` already uses).
- **G5** A request to the WebSocket path runs only the middlewares given to `EnableWS`, once, at the
  handshake; a refusal refuses the handshake. `compress` passes a request with an `Upgrade` header
  through untouched.

### Core: exchange hooks (K)
The lifecycle order does not change; `next()` still does not wrap the handler. The framework's
response writer (`core.responseWriter`, the `w` every middleware receives) implements
`httphook.Writer`.

- **K1** `SetInner(x)` makes every later `Header`, `WriteHeader`, `Write`, `Flush`, `ReadFrom`,
  `Hijack` and `Unwrap` of the framework's writer go to `x`. The status the writer records
  (`status()`) is still the one the layer passed to `WriteHeader`.
- **K2** Hooks registered with `OnDone` run exactly once per exchange, last registered first, each
  with the time since the request started (`time.Since(c.Timestamp)`, read once and only when a
  hook exists):
  (a) after the request lifecycle returned (response written, error response included) and before
  the `complete` trace event; (b) for a WebSocket path request, when the handshake ends, accepted or
  refused; (c) before `http.ErrAbortHandler` is re-panicked.
- **K3** After the hooks ran the writer is reset: the next request on the pooled exchange has no
  hook and its inner writer is its own `http.ResponseWriter`.
- **K4** With no hook registered the added cost is one length check per request and no allocation.
- **K5** `httphook.Of(w)` returns nil for any other `http.ResponseWriter`; `compress` and `loadshed`
  then call `next()` and do nothing else (a middleware called outside an app degrades to a no-op).

### `proxy` (P)
Sources: RFC 7239 (Forwarded); MDN `X-Forwarded-For` › "Security and privacy concerns" and "Selecting
an IP address"; OWASP Cheat Sheet "HTTP Headers" and WSTG "Testing for Host Header Injection".

- **P1** `NewMiddleware` panics (G4) unless exactly one of `TrustedProxies` and `TrustedHops` is
  set: `invalid proxy: set TrustedProxies or TrustedHops, not both`, `invalid proxy: set
  TrustedProxies or TrustedHops`; also for an entry that is neither an IP nor a CIDR, for more than
  256 entries, and for `TrustedHops` outside 1-16.
- **P2** Without `proxy` bound, or when `proxy` did not resolve: `proxy.ClientIP(r)` is the IP of
  `r.RemoteAddr` (IPv4-mapped IPv6 unmapped, zone removed; the zero `Addr` when `RemoteAddr` is not
  `ip:port`), `proxy.Scheme(r)` is `"https"` when `r.TLS != nil`, else `"http"`, `proxy.Host(r)` is
  `r.Host`. No `Forwarded`, `X-Forwarded-*`, `X-Real-IP` or other header is read by anything.
- **P3** Trusted-proxies mode: when the peer (P2's IP) is not inside `TrustedProxies`, the request
  resolves to P2's values whatever headers it carries.
- **P4** Trusted-proxies mode, trusted peer: the entries of the configured header family are read
  right to left, across repeated header lines (last line first). The client IP is the first entry
  that is not inside `TrustedProxies`. If every entry is trusted, it is the leftmost entry read; if
  there is no entry, the peer.
- **P5** Hops mode (`TrustedHops: n`): the client IP is entry number n from the right. With fewer
  than n entries the request resolves to P2's values.
- **P6** An entry is an IPv4 or IPv6 address, optionally with a port (`1.2.3.4:5`, `[2001:db8::1]:5`)
  or brackets; IPv4-mapped addresses are unmapped. Anything else (`unknown`, `_hidden`, a zone, a
  host name, an empty entry, an entry longer than 512 bytes) ends the walk: the client IP is the
  last valid address read (the proxy that reported it), or the peer when none was read. In hops
  mode an invalid entry at or right of position n resolves to P2's values.
- **P7** At most 16 entries are read; a seventeenth trusted entry ends the walk as in P6.
- **P8** (revised 2026-11-17, see "Revision 2026-11-17") Scheme, read only when the client IP came
  from a header entry (P13). `XForwarded`: the last value of `X-Forwarded-Proto` (the last header
  line, the text after its last comma, spaces and tabs trimmed), whatever the client's position and
  however many values the header holds. No earlier value is ever used. `Forwarded`: the `proto` of
  the client's element, or of the nearest element to its right that has one. Only `http` and
  `https` (any case) are accepted; anything else (an empty last value, a value over 512 bytes, no
  header) keeps P2's scheme.
- **P9** (revised 2026-11-17) Host. With `IsHostForwarded: false` (the default), `proxy.Host(r)` is
  `r.Host` in every case: `X-Forwarded-Host` and `host=` are not read, in either family and either
  mode. With `IsHostForwarded: true`, and only when the client IP came from a header entry (P13):
  `XForwarded` reads the last value of `X-Forwarded-Host` (selected as in P8); `Forwarded` reads the
  `host` of the client's element, or of the nearest element to its right that has one. A value is
  accepted when it is at most 255 bytes of letters, digits, `.`, `-`, an optional `:port`, or a
  bracketed IPv6 literal with an optional port; otherwise P2's host is kept.
- **P10** `Forwarded` elements are `;`-separated `name=value` pairs, names case-insensitive, values
  optionally quoted; unknown names are ignored; an element without `for`, or that does not parse, is
  an invalid entry (P6).
- **P11** The other header family is never read: with `Header: Forwarded`, `X-Forwarded-For` changes
  nothing, and the reverse.
- **P12** `proxy` writes its result into the request's scope and calls `next()`. It never refuses a
  request, writes no response header and does not change `r.RemoteAddr`, `r.Host`, `r.URL` or
  `r.Header`.
- **P13** (2026-11-17, states what the code already did) Scheme and host are read from headers only
  when P4 or P5 took the client IP from a header entry. When the peer is untrusted (P3), when the
  walk read no valid entry (no header, or an invalid rightmost entry), or when hops mode fell back
  (P5, P6), the request keeps P2's scheme and host whatever `X-Forwarded-Proto`,
  `X-Forwarded-Host`, `proto=` or `host=` say.

### `hostcheck` (H)
Sources: RFC 9110 § 7.2 (Host) and § 15.5.20 (421); OWASP WSTG-INPV-17 (Host header injection);
MDN "DNS rebinding" guidance (validate `Host`).

- **H1** `NewMiddleware` panics when `Hosts` is empty (`invalid hostcheck: Hosts is empty`), has more
  than 256 entries, or an entry is not a host name, `*.`-pattern or IP literal with an optional port.
- **H2** The checked value is `proxy.Host(r)`. Matching ignores case and one trailing dot. An entry
  without a port matches any port; with a port, that port only. `*.example.com` matches any
  subdomain depth of `example.com` made of letters, digits, `-` and `.`, and not `example.com`.
- **H3** A match calls `next()`. No match answers `421` with message `Unknown host` (G1). An empty
  host, or one longer than 255 bytes, answers `400` with message `Invalid host`.
- **H4** There is no implicit entry: `localhost` passes only when listed.

### `httpsredirect` (R)
Sources: RFC 9110 § 15.4.9 (308); OWASP Cheat Sheets "Transport Layer Security" and "Unvalidated
Redirects and Forwards"; RFC 6797 § 7.2.

- **R1** `proxy.Scheme(r) == "https"`: `next()`, nothing else.
- **R2** Otherwise: `308` with `Location: https://<host><:port><request URI>` and no body; `next()`
  is not called. `<host>` is `Host` when set, else `proxy.Host(r)` without its port; `<:port>` only
  when `Port` is set and not 443; `<request URI>` is `r.URL.RequestURI()` (path and query kept, both
  re-encoded).
- **R3** When `Host` is empty and the request host fails P9's syntax: `400`, message `Invalid host`.
  The location is never built from an unvalidated value, and always starts with `https://` followed
  by a validated host, so no path (`//evil.example`, `/\evil.example`) changes the target.
- **R4** `NewMiddleware` panics for a `Host` that fails P9's syntax or has a port, and for a `Port`
  outside 0-65535.

### `csrf` (S)
Sources: Go `net/http.CrossOriginProtection` (Go 1.25+); Fetch Metadata Request Headers (W3C);
OWASP Cheat Sheet "Cross-Site Request Forgery Prevention" › Fetch Metadata; MDN `Sec-Fetch-Site`.

`csrf` wraps the standard library: the decision is `(*http.CrossOriginProtection).Check(r)`, with
`TrustedOrigins` added through `AddTrustedOrigin`. The Go team maintains the browser table; Ginject
adds S4 and S5. `SetDenyHandler`, `AddInsecureBypassPattern` and `Handler` are not used.

| # | Method | `Sec-Fetch-Site` | `Origin` | Result |
|---|---|---|---|---|
| S1 | `GET`, `HEAD`, `OPTIONS` | any | any | pass |
| S2 | other | `same-origin` or `none` | any | pass |
| S3 | other | any other value (`cross-site`, `same-site`, junk) | any | `Origin` in `TrustedOrigins` → pass; else `403` |
| S4 | other | absent | absent | pass (not a browser) |
| S5 | other | absent | host equals `proxy.Host(r)` | pass (a browser before 2023, or plain HTTP) |
| S6 | other | absent | another host, `null`, unparsable | `Origin` in `TrustedOrigins` → pass; else `403` |

- **S7** The refusal is `403`, message `Cross-origin request refused` (G1).
- **S8** S5 with a forwarded host: when the scope says the host was forwarded and
  `Sec-Fetch-Site` is absent, the origin's host is compared with `proxy.Host(r)` before `Check`
  runs; otherwise `Check` alone decides (it compares with `r.Host`).
- **S9** For an unsafe method, an `Origin` longer than 256 bytes is refused (S7) before any parsing.
- **S10** `NewMiddleware` panics for an origin `AddTrustedOrigin` refuses (not
  `scheme://host[:port]`) and for more than 64 origins.
- **S11** No cookie is set, no token is issued, nothing is stored in the request context.
- **S12** Limits, stated in the README: state changes behind `GET` are not protected; a same-site
  subdomain is treated as cross-origin (S3); non-browser clients always pass, so `csrf` is not
  authentication.

Token fallback: not added. The only browsers S5 cannot tell apart (HTTP page posting to the HTTPS
site of the same host, before `Sec-Fetch-Site` existed) are closed by HSTS, which `helmet` sends.

### `cors` (C)
Sources: Fetch Standard § 3.2 (CORS protocol, "CORS protocol and HTTP caches"); MDN "Cross-Origin
Resource Sharing" and `Access-Control-Max-Age`; OWASP Cheat Sheet "HTML5 Security" › CORS; WICG
"Private Network Access".

- **C1** `NewMiddleware` panics for: empty `AllowOrigins` (`invalid cors: AllowOrigins is empty`);
  `"*"` together with other entries or with `IsAllowCredentials`; an entry that is not
  `scheme://host[:port]` in lower case, or `scheme://*.host[:port]`; the entry `null`; more than 256
  origins; more than 64 methods, allowed headers or exposed headers; a method or header name that is
  not an RFC 9110 token.
- **C2** Origin match: exact string equality with an entry, or a `*.`-pattern whose scheme and
  suffix match and whose wildcard part is one or more characters of `a-z`, `0-9`, `-`, `.`. An
  `Origin` longer than 256 bytes matches nothing. With `"*"` every origin matches.
- **C3** Every response that passes `cors` with a list (not `"*"`) carries `Vary: Origin`, added to
  an existing `Vary`: also without an `Origin` header and for an origin that does not match.
- **C4** A request without `Origin`: C3, then `next()`.
- **C5** Preflight is exactly: `OPTIONS` with `Origin` and `Access-Control-Request-Method`. Any other
  `OPTIONS` is an actual request (C6) and reaches its route.
- **C6** Actual request, matching origin: `Access-Control-Allow-Origin` is the request's origin
  (`*` with `"*"`), `Access-Control-Allow-Credentials: true` with `IsAllowCredentials`,
  `Access-Control-Expose-Headers` when set; then `next()`. Not matching: no CORS header, `next()`
  (the browser blocks the read; CORS is not access control).
- **C7** Preflight, matching origin: C6's headers plus `Access-Control-Allow-Methods`,
  `Access-Control-Allow-Headers`, `Access-Control-Max-Age` (seconds), all three fixed strings built
  once from the options; `Access-Control-Allow-Private-Network: true` only with
  `IsAllowPrivateNetwork` and a request header `Access-Control-Request-Private-Network: true`;
  status `204`, `Content-Length: 0`, `next()` not called. The request's
  `Access-Control-Request-Headers` is never parsed or echoed.
- **C8** Preflight, origin not matching: `204` with `Vary` only; `next()` not called.
- **C9** A WebSocket upgrade (`Upgrade: websocket`) with an `Origin` that does not match is refused
  with `403`, message `Origin not allowed`; matching or absent: `next()`. No CORS header is written.

### `helmet` (M)
Sources: OWASP Secure Headers Project; OWASP Cheat Sheet "HTTP Security Response Headers"; MDN per
header; RFC 6797 (HSTS).

- **M1** Each field: `""` sends the default, `helmet.Disabled` sends nothing, any other value is
  sent as written. `NewMiddleware` panics for a value with a control character.
- **M2** `Strict-Transport-Security` is sent only when `proxy.Scheme(r) == "https"` (RFC 6797
  § 7.2).
- **M3** Fixed headers, always sent: `X-Content-Type-Options: nosniff`, `X-XSS-Protection: 0`,
  `X-DNS-Prefetch-Control: off`, `X-Permitted-Cross-Domain-Policies: none`,
  `Origin-Agent-Cluster: ?1`. `X-Download-Options` is no longer sent (Internet Explorer only).
- **M4** Headers are set before `next()`; a later layer may overwrite one.

### `compress` (Z)
Sources: RFC 9110 § 8.4 (Content-Encoding), § 12.5.3 (Accept-Encoding), § 12.5.5 (Vary), § 8.8.3
(ETag); RFC 9111 § 5.2.2.6 (`no-transform`); MDN "Compression in HTTP"; BREACH (Security).

- **Z1** Every response that passes `compress` gets `Vary: Accept-Encoding` added.
- **Z2** A request is a candidate when: the method is not `HEAD`; it has no `Range` and no
  `Upgrade` header; `Accept-Encoding` names `gzip` with a q-value other than 0, within its first 256
  bytes. Others get `next()` and no hook.
- **Z3** For a candidate the decision is taken at the first `Write`, `Flush`, `ReadFrom` or at the
  end of the exchange. The response is compressed when all hold: status is not 1xx, `204`, `206`,
  `304`; no `Content-Encoding` and no `Content-Range` header is set; `Cache-Control` has no
  `no-transform`; the media type (lower case, parameters removed) is compressible (Z4); the body is
  at least `MinSize` bytes (a declared `Content-Length`, or the bytes written before the decision);
  a writer is free (Z8).
- **Z4** Compressible: `text/*` except `text/event-stream`; `application/json`,
  `application/javascript`, `application/xml`, `application/wasm`, `image/svg+xml`; any type ending
  in `+json` or `+xml`. Without a `Content-Type`, it is set from `http.DetectContentType` of the
  buffered bytes first (as `net/http` would have).
- **Z5** With an unknown length, up to `MinSize` bytes are held back; a response that ends below
  `MinSize` is sent as written, with its `Content-Length`.
- **Z6** A compressed response: `Content-Encoding: gzip`; `Content-Length` and `Accept-Ranges`
  removed; a strong `ETag` gets the `W/` prefix; the body is one gzip stream that decodes to the
  bytes written, at `gzip.BestSpeed`.
- **Z7** `Flush` takes the decision with what is buffered (the `MinSize` condition is skipped),
  then flushes the gzip stream and the connection. An uncompressed response forwards `ReadFrom`, so
  `http.ServeFile` keeps `sendfile`. `Hijack` is forwarded until a body byte was compressed.
- **Z8** At most 8 gzip writers are in use at once per `Compress` instance; a response that finds
  none free is sent uncompressed.
- **Z9** `NewMiddleware` panics for a negative `MinSize`; 0 means 1024.

### `loadshed` (L)
Sources: RFC 9110 § 15.6.4 (503) and § 10.2.3 (Retry-After); Google SRE Book ch. 21-22 (handling
overload, load shedding before queues grow); Netflix `concurrency-limits` (Gradient2) and Envoy's
adaptive concurrency filter (gradient controller with a periodic minimum-latency probe).

The maintainer decided (2026-11-17) that the default limit adapts: the users this is for will not
tune a number. A fixed limit is the explicit override.

- **L1** At any instant at most `limit` requests are between admission and the end of their
  exchange (K2). Fixed mode (`MaxInFlight > 0`): `limit` is `MaxInFlight`, forever. Adaptive mode
  (`MaxInFlight == 0`): `limit` moves between `min` and `max` (L7-L11).
- **L2** A request arriving when all slots are taken waits, when fewer than `MaxQueue` requests are
  already waiting, for at most 100 ms; a slot freed in that time admits the longest waiter first.
- **L3** A request that finds the queue full, waits 100 ms without a slot, or whose
  `r.Context()` ends while waiting, is refused: `Retry-After: 1`, `503`, message `Server is busy`
  (G1). A request whose context ended gets no response written.
- **L4** A slot is released exactly once, whatever the request did: returned, panicked, was refused
  by a later middleware, answered 404, or aborted with `http.ErrAbortHandler`. For a WebSocket
  handshake the slot is released when the handshake ends, not when the connection closes.
- **L5** `NewMiddleware` panics for `MaxInFlight` < 0 or > 1,000,000 and `MaxQueue` > 65,536.
- **L6** Idle, an instance has no goroutine and no timer in either mode; a timer exists only for a
  waiting request. The adaptive limit is recomputed inside the `Done` of a finishing request, never
  by a background task.

Adaptive mode. Constants are unexported and fixed.

- **L7** Bounds, read once in `NewMiddleware`: `min` = `GOMAXPROCS`. `max` = memory / 4 MiB, clamped
  to 64-4,096, where memory is the smallest of `GOMEMLIMIT` (`debug.SetMemoryLimit(-1)`, when set),
  the cgroup v2 `memory.max` and the cgroup v1 `memory.limit_in_bytes` (Linux; a missing or
  unlimited file is skipped); with no memory limit known, `max` = 256 × `GOMAXPROCS`, same clamp.
  The first limit is 16 × `GOMAXPROCS`, clamped to `min`-`max`. Examples: 1 vCPU and 512 MiB:
  1 / 16 / 128; 2 vCPU and 2 GiB: 2 / 32 / 512.
- **L8** (revised 2026-11-17, see "Revision 2026-11-17: `loadshed`") Samples. Every finished
  exchange adds a sample to the current window: its `elapsed` minus the time it waited in
  `loadshed`'s own queue (L2), except a sample longer than 30 s (a stream or a download). A window
  closes at its (4 × the current limit)th sample (Revision 2026-11-17). A sample that finished with at most `min` requests in flight is also a
  *quiet* sample. A window is *under pressure* when, since it opened, a request found every slot
  taken (it was queued or refused).
- **L9** Baseline: the latency without contention. It is the average of 8 quiet samples, blended
  half and half with the previous baseline, and it is lowered to a window's average when that is
  smaller. With no baseline yet, the limit does not move.
- **L10** (revised 2026-11-17) At the close of a window under pressure, when a baseline is known
  and no probe ran during the window, with `avg` the window's average:
  `gradient = min(1.5 × baseline / avg, 1)`;
  `useful = limit × gradient`;
  `target = useful + sqrt(useful)`;
  `limit = clamp(0.8 × limit + 0.2 × target, min, max)`, kept as a fraction and floored to an
  integer for admission. Every other window leaves the limit as it is: without pressure the limit
  bounded nothing, and a window with a probe measured `min`, not `limit`. So one window never
  takes more than 20 % off the limit, and the limit grows only while requests are held back.
  Examples (limit 32, baseline 10 ms, under pressure): `avg` 10 ms gives 33 (33.13); `avg` 60 ms
  gives 27 (27.77); `avg` 60 ms window after window ends at `min`; a server that shares c cores
  evenly (`avg` = `limit` × baseline / c) settles at 1.5 c + sqrt(1.5 c): 2.72 for one core, 4.73
  for two.
- **L11** (revised 2026-11-17: the 30 s spacing) Probe. When a window under pressure closes, the
  baseline is unknown or older than 30 s, and no probe started in the last 30 s: admission uses
  `min` instead of `limit` until 8 requests admitted after that moment have finished, or 1 s has
  passed (checked at the next admission, against the clock, no timer); their average is the new
  baseline input (L9) and admission returns to `limit`. A probe that ends with no finished request
  changes nothing, and the next one waits its 30 s (handlers slower than 1 s would otherwise hold
  admission at `min` after every window). No probe ever starts while nothing is being queued or
  refused. The clock of L9-L11 is the unexported field `now` (default `time.Now`), read only at a
  window close and during a probe.
- **L12** (thresholds unchanged; how they are measured is revised 2026-11-17) Stability, asserted
  by the tests below on real `GOMAXPROCS` 1 and 2: under a steady overload of 5 × capacity, after
  10 s, outside probes, the limit stays within ± 35 % of its median for a CPU-bound handler and
  ± 20 % for an I/O-bound one; throughput is at least 95 % of the capacity measured without
  `loadshed`; the p99 latency of admitted requests is at most 12 × the no-load latency (CPU-bound)
  and 3 × (I/O-bound). At 70 % of capacity no request is refused. Measured as follows: the
  CPU-bound handler computes 5 ms in five 1 ms steps with `runtime.Gosched()` between them (a
  handler that reaches the scheduler, as one that waits on a socket or a lock does); the capacity
  is the smaller of two measurements, one before and one after the run; every arrival reuses one
  `*http.Request`. A CPU-bound handler that never reaches the scheduler is a known limit (see the
  revision), recorded by a benchmark, not asserted.

Why the gradient and not AIMD (the model below assumed processor sharing, which Go does not do
for short handlers: see "Revision 2026-11-17: `loadshed`" for the measurements and the revised
L8, L10, L11). A model of the server (processor sharing on c cores, an optional
I/O stage with a pool, Poisson arrivals, 120 s, both rules with the same baseline and probe) gave,
at 5 × capacity:

| Workload | Cores | Gradient: limit min / median / max | AIMD (+1, × 0.9) | Throughput | p99 (no-load latency) |
|---|---|---|---|---|---|
| CPU 5 ms | 1 | 4 / 6 / 8 | 3 / 5 / 7 | 100 % | 51 ms (5 ms) |
| CPU 5 ms | 2 | 6 / 7 / 9 | 4 / 7 / 9 | 100 % | 31 ms (5 ms) |
| I/O 20 ms, pool of 50 | 1 | 59 / 66 / 75 | 41 / 53 / 67 | 99 % | 50 ms (20 ms) |
| I/O 20 ms, pool of 50 | 2 | 68 / 73 / 80 | 43 / 61 / 75 | 100 % | 41 ms (20 ms) |
| CPU 2 ms + I/O 20 ms | 1 | 21 / 22 / 24 | 13 / 16 / 19 | 99 % | 67 ms (22 ms) |
| CPU 2 ms + I/O 20 ms | 2 | 33 / 38 / 43 | 26 / 32 / 40 | 100 % | 58 ms (22 ms) |

The gradient settles on a fixed point (spread ± 7-14 % where the limit is above 20; ± 2 around 6);
AIMD saws by design (± 20-25 %) and sits lower, because one slow window costs it 10 %. At 70 % of
capacity the gradient refused 0 % in every case, AIMD up to 5 %. A handler that becomes 3 × slower
mid-run kept its throughput (the limit collapses, a probe relearns the baseline). Probes took
0.1-0.7 % of the time. Without a real no-load baseline (the first prototype took the first window's
average) the same rule held 28 in flight on one core with a p50 of 136 ms for a 5 ms handler:
that is why L9 and L11 exist. These are model numbers, not measurements: L12 is the acceptance
test of step 8. **If the implementation cannot meet L12 on `GOMAXPROCS` 1 and 2, step 8 stops and
reports: the default falls back to a fixed limit derived from the machine (`max` of L7 halved),
flagged for the maintainer.**

Known limits, stated in the README: one limit for all routes, so a mix of very fast and very slow
routes blurs the signal (bind a fixed `LoadShed` per module for those); the limit follows latency,
so a dependency that slows down lowers it, which is the intent.

### Static files (F), `core`
Sources: RFC 9111 § 4.2.2 (heuristic freshness) and § 5.2.2 (`no-cache`, `immutable` is RFC 8246);
MDN "HTTP caching" › "Cache Busting"; RFC 8615 (`.well-known`, already fixed in `serveFromRoot`).

- **F1** A `SERVE_…` route that serves a file (`200`, `206`, `304`, and the `index.html` of a
  directory) sends `Cache-Control: no-cache` when no layer set `Cache-Control` before: the browser
  revalidates with `If-Modified-Since` on every use and never guesses a lifetime.
- **F2** A `Cache-Control` set by a middleware, guard, interceptor or the handler is kept. Marking
  hashed assets is the handler's line:
  `c.ResponseWriter.Header().Set("Cache-Control", "public, max-age=31536000, immutable")`.
- **F3** A `SERVE_…` route that answers its own `404` or `400` removes `Cache-Control` first, so an
  `immutable` set for assets never lands on an error (`net/http` already does this for the errors
  of `ServeFile`).
- No new API. No file name is treated as "hashed" automatically: a wrong guess caches a file for a
  year with no way back.

### Throttler (T), `guards/throttler`
Sources: RFC 6585 § 4 (429); RFC 9110 § 10.2.3 (Retry-After); OWASP API Security Top 10 2023 API4
(unrestricted resource consumption); RFC 6177 (an end site holds at least a /64).

- **T1** The default key is `proxy.ClientIP(c.Request)`: an IPv4 address as is, an IPv6 address
  masked to its /64, `unknown` for the zero `Addr`. No request header is read.
- **T2** A key (default or from `KeyFunc`) longer than 128 bytes is replaced by `h:` and 16 hex
  digits of a `hash/maphash` of it, seeded per guard.
- **T3** Exactness: with one key and one window, any number of concurrent requests get exactly
  `Limit` allowed (fixed window; sliding window with an empty previous window; token bucket within
  one refill interval). The read-change-write of a key runs under one of 64 mutexes chosen by the
  key's hash. With `FixedWindow` the count is one `Incr` on the store (0015), which is atomic per
  key; the stripe mutex is used by the sliding window and the token bucket only.
- **T4** The default store (`Backend == nil`) is one `memorycache` per compiled guard, bounded to
  32,768 entries (what 0015 provides, see Coordination): with any number of distinct keys the
  entry count never exceeds it.
- **T5** Creating a guard starts no goroutine; with no live entry the guard has no goroutine and no
  timer.
- **T6** `NewGuard` returns the compiled guard; defaults stay `Limit` 100, `TTL` one minute,
  `FixedWindow`. Response headers stay `X-RateLimit-Limit`, `-Remaining`, `-Reset` and, on a
  refusal, `Retry-After` with `429` (Q7).
- **T7** A backend error counts the request as allowed (unchanged; stated in the README).

### Access log (A), `accesslog` and `core`
- **A1** The `complete` event of an HTTP request carries `ClientIP` = `tracecontext.ClientIP(r)`;
  the access log line prints it as `ip` (omitted for the zero `Addr`). With no trace listener
  nothing is resolved.

### Defaults against their sources

| Package | Default | Source |
|---|---|---|
| `proxy` | no header trusted without configuration; client = first untrusted address from the right | roadmap decision; MDN `X-Forwarded-For` › "Selecting an IP address"; RFC 7239 § 8.1 (header is not authenticated) |
| `proxy` | one header family, default `X-Forwarded-*` | MDN `Forwarded` (few proxies write it); RFC 7239 § 7.4 on mixing |
| `hostcheck` | `421` for an unknown host, `400` for none | RFC 9110 § 15.5.20, § 7.2 |
| `httpsredirect` | `308`, same host and URI | RFC 9110 § 15.4.9; OWASP TLS Cheat Sheet (redirect all HTTP) |
| `csrf` | `Sec-Fetch-Site`, then `Origin` against host; safe methods pass; no token | Go `CrossOriginProtection`; OWASP CSRF Cheat Sheet › Fetch Metadata |
| `cors` | no origin allowed until listed; `*` never with credentials; `null` refused | Fetch § 3.2.5; OWASP HTML5 Security › CORS |
| `cors` | `Vary: Origin` on every response | Fetch "CORS protocol and HTTP caches" |
| `cors` | `Access-Control-Max-Age: 600` | MDN (Fetch default 5 s; Chromium caps at 7,200 s, Firefox at 86,400 s; 600 s is accepted everywhere) |
| `cors` | methods `GET, HEAD, PUT, PATCH, POST, DELETE`; headers `Content-Type, Authorization` | Fetch safelists (what a JSON API with a bearer token needs beyond them) |
| `cors` | `Access-Control-Allow-Private-Network` not sent | WICG Private Network Access (opt-in by the target server) |
| `helmet` | `Content-Security-Policy: default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests` | OWASP Secure Headers (CSP), CSP Cheat Sheet "basic strict" (Q3) |
| `helmet` | `Strict-Transport-Security: max-age=63072000; includeSubDomains`, HTTPS only, `preload` never by default | OWASP HSTS Cheat Sheet; RFC 6797 § 7.2; hstspreload.org (preload is an explicit request) |
| `helmet` | `X-Frame-Options: DENY` | OWASP Secure Headers; matches `frame-ancestors 'none'` |
| `helmet` | `X-Content-Type-Options: nosniff` | OWASP Secure Headers; MDN |
| `helmet` | `Referrer-Policy: no-referrer` | OWASP Secure Headers |
| `helmet` | `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin` | OWASP Secure Headers |
| `helmet` | `Cross-Origin-Embedder-Policy` not sent | deviates from OWASP (`require-corp`): it blocks every cross-origin image and script without CORP and protects nothing by itself (MDN) (Q3) |
| `helmet` | `Permissions-Policy: accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=(), browsing-topics=()` | OWASP Secure Headers (deny powerful features) |
| `helmet` | `X-XSS-Protection: 0` | OWASP (the auditor is removed or harmful); roadmap |
| `compress` | gzip only, `MinSize` 1024, type allowlist, skips `Range` and encoded responses | RFC 9110 § 8.4; MDN; roadmap step 7 |
| `loadshed` | `503` + `Retry-After: 1`, queue of 64 for 100 ms | RFC 9110 § 15.6.4; SRE Book ch. 22 (short queues, fail fast) |
| `loadshed` | adaptive limit: gradient on latency against a probed no-load baseline, tolerance 1.5, moved only while requests are held back (Revision 2026-11-17), bounds from `GOMAXPROCS` and the memory limit | Netflix `concurrency-limits` Gradient2; Envoy adaptive concurrency (minRTT probe); maintainer's decision |
| static files | `Cache-Control: no-cache` | RFC 9111 § 4.2.2; MDN "HTTP caching" |
| throttler | key = client IP, IPv6 by /64, `429` + `Retry-After` | RFC 6585 § 4; RFC 6177 |

## Performance
All of this is hot path (every request, and the refusals are what an attacker triggers at will).
Allocation budgets below are rules: tests assert them with `testing.AllocsPerRun` on a response
header map that already has room (header map growth is `net/http`'s). Times are expectations from
measurements on this machine (i7-9750H, Go 1.27.4) and are measured again per step with
`-bench . -benchmem -count 5`.

| Path | Allocations | Expected time | Basis |
|---|---|---|---|
| hooks unused (K4) | 0 | about 1 ns | one `len` check |
| `proxy`, untrusted peer | 0 | about 60 ns | `netip.ParseAddrPort` measured 51 ns, 0 allocs |
| `proxy`, trusted peer, 3 entries, 4 CIDRs | 0 | about 220 ns | prototype walk measured 157 ns, 0 allocs |
| `proxy.ClientIP` and friends | 0 | under 60 ns | one context lookup; one parse when unresolved |
| `hostcheck` | 0 | under 50 ns | one map lookup; lower-casing into a stack array only on a miss |
| `httpsredirect`, HTTPS | 0 | under 10 ns | one scope read |
| `httpsredirect`, redirect | at most 3 | under 500 ns | one string build |
| `csrf`, safe method or `Sec-Fetch-Site` | 0 | about 50 ns | `Check` measured 48 ns, 0 allocs |
| `csrf`, no `Sec-Fetch-Site`, with `Origin` | at most 2 | about 350 ns | `Check` measured 334 ns, 1 alloc (`url.Parse`) |
| `cors`, no `Origin` | 0 | under 30 ns | prebuilt `Vary` slice |
| `cors`, actual request, listed origin | at most 1 | under 100 ns | the echoed origin's one-element slice |
| `cors`, preflight | at most 1 | under 200 ns | fixed strings |
| `helmet` | 0 | under 150 ns | prebuilt one-element slices assigned to the header map (today: 12 allocations) |
| `compress`, not a candidate | 0 | under 40 ns | header scan, prebuilt `Vary` |
| `compress`, compressed 16 KiB JSON | 0 steady state | about 25 µs | reused writer measured 24.9 µs (659 MB/s); level 6 would be 76.5 µs |
| `compress`, compressed 2 KiB | 0 steady state | about 10 µs | measured 10.2 µs: resetting the writer dominates, hence `MinSize` |
| `loadshed`, admitted, fixed limit | 0 | under 20 ns | one atomic add in, one add and one load out; plus K2's clock read (about 45 ns) shared by all hooks |
| `loadshed`, admitted, adaptive | 0 | under 60 ns | in: one load (limit), one add; out: one add (in flight), one add (count and sum packed in one word), one more for a quiet sample, one load (waiters): 5-6 atomics, no peak tracking (Revision 2026-11-17). Every 32nd request also closes the window (one `TryLock`, a clock read, a square root: about 150 ns, 5 ns amortized). Prototype of the revision: 40-41 ns at `GOMAXPROCS` 1 and 2 |
| any refusal (G1) | as core's exception path | about 0.7 µs over a served request | core README: `ServeHTTP_Exception` 1,645 ns against `Static` 940 ns |
| static file `Cache-Control` | 0 | under 20 ns | a prebuilt slice, one map read |
| throttler, default key | 1 (the key string) plus today's | measured in step 10 | `netip.Addr.AppendTo` into a stack buffer |

Memory, idle and peak:

| Component | Idle | Peak | Bound |
|---|---|---|---|
| `proxy` | compiled prefixes (at most 256 × 32 B) | same | fixed |
| `hostcheck`, `cors`, `csrf` | their maps (at most 256, 256, 64 entries) | same | fixed |
| `helmet` | about 1 KiB of strings | same | fixed |
| `compress` | nothing after two GC cycles | 8 writers × about 0.8 MiB (measured 813,861 B per `gzip.BestSpeed` writer) plus `MinSize` bytes per candidate in flight | 8 writers; candidates bounded by `loadshed` |
| `loadshed` | about 200 B: counters, the window, one mutex, an empty list; no goroutine, no timer | `MaxQueue` waiters × one timer and one channel (about 200 B) | `MaxQueue` |
| throttler | 64 mutexes (512 B), an empty store | 32,768 entries (expected under 8 MiB, measured in step 10) | entry bound of 0015 |
| scope | +48 B inside the existing scope allocation, no new allocation | same | per request |

Moved to `Create()`: CIDR parsing, host and origin maps, every response header value and its
one-element slice, the stdlib `CrossOriginProtection`, the refusal exceptions.

Required benchmarks (the "done when" of the roadmap):
- `compress` at `GOMAXPROCS` 1 and 2: `BenchmarkCompress_JSON2K`, `_JSON16K`, `_JSON256K`,
  `_NotCandidate`, `_TooSmall`, `_NoWriterFree`; reported with bytes out per byte in and process
  CPU per response.
- `loadshed` at `GOMAXPROCS` 1 and 2: `BenchmarkLoadShed_Admitted_Fixed`, `_Admitted_Adaptive`,
  `_Shed`, and the L12 runs (a CPU-bound and an I/O-bound handler, 5 × and 0.7 × capacity, 30 s
  each) that record the limit over time, the p99 latency of admitted requests, the heap in use and
  the number of responses per status (sheds counted, so dropped work does not look cheap).

Heavy techniques, approved only by approving this design:

| # | Technique | Where | Why | Benchmark |
|---|---|---|---|---|
| 1 | `sync.Pool` of gzip writers, guarded by an atomic counter capped at 8 | `compress` | a new writer costs 179 µs and 814 KB (measured) against 0 B reused; the counter bounds what the pool can hold in use, the GC frees idle writers | `BenchmarkCompress_JSON16K` with and without |
| 2 | `sync.Pool` of the per-response state (the `MinSize` hold-back buffer, about 1.1 KiB) | `compress` | one 1.1 KiB allocation per candidate otherwise | same |
| 3 | Prebuilt one-element `[]string` (capacity 1) assigned into the response header map | `helmet`, `cors`, `compress`, static files | removes one allocation per header; `Header.Add` on them reallocates instead of writing the shared slice. `core` already does this for `X-Request-Id` | `BenchmarkHelmet` before and after |

No `unsafe`, no manual inlining.

## Concurrency
- Compiled middlewares are immutable after `NewMiddleware`, except the state below.
- **Scope fields** (`ClientIP`, `ClientHost`, `IsClientHTTPS`, `IsClientSet`) are written by `proxy`
  on the request's goroutine before any later layer runs, and never after. Readers on other
  goroutines (a goroutine that kept `c.Context()`) start after the handler, so they read settled
  values. No lock.
- **Hooks** live in the pooled exchange's writer and are touched only by the request's goroutine.
- **`loadshed`**: `inFlight atomic.Int64`, `waiting atomic.Int32`, one `sync.Mutex` guarding a FIFO
  of waiters (each a `chan struct{}` of capacity 1). `max` below is the current limit: a constant
  in fixed mode, one `atomic.Int64` load in adaptive mode (`min` during a probe).
  - Adaptive window: `sum`, `count`, `quietSum`, `quietCount` are atomics added in `Done`; the
    request that brings `count` to 32 takes a second mutex with `TryLock` (a loser just returns),
    swaps the four to zero, computes L9-L11 and stores the limit. Samples that land between the
    swaps fall into either window; the rule tolerates it. A limit that drops below `inFlight`
    interrupts nothing: admissions stop until enough requests finish. A limit that rises wakes
    waiters through the Leave path of the next finishing request.
  - Pressure (Revision 2026-11-17): one `atomic.Bool`, set on the queue-or-refuse path (a load, and
    a store only when it is not set yet), swapped to false at the window close. "A probe ran
    during this window" and "when the last probe started" are plain fields under the window mutex.
    The window mutex and the waiters' mutex are never held together.
  - Enter: `inFlight.Add(1) <= max` admits. Otherwise `inFlight.Add(-1)`, lock; if the list holds
    `MaxQueue` waiters, unlock and refuse; else `waiting.Add(1)`, try `inFlight.Add(1) <= max` once
    more (admitted: `waiting.Add(-1)`, unlock), else undo, append the waiter, unlock, and wait on
    the channel, a 100 ms timer and `r.Context().Done()`.
  - Leave (the hook): `inFlight.Add(-1)`; if `waiting.Load() > 0`: lock; if a waiter exists and
    `inFlight.Add(1) <= max`, pop it, `waiting.Add(-1)` and send; else undo the add; unlock.
  - A waiter that times out or is cancelled locks and removes itself; if its channel was already
    signalled it owns a slot and releases it (Leave). The two mutexes are never held together.
  - The store-then-load on both sides (a leaver subtracts, then reads `waiting`; a waiter adds to
    `waiting`, then retries `inFlight`) means a freed slot is never missed. Admission only ever
    follows `Add(1) <= max`, so L1 holds at every instant.
- **`compress`**: an `atomic.Int32` counts writers in use (`Add(1) > 8` → undo, send uncompressed);
  a writer returns to the pool in the hook. Per-response state belongs to one request.
- **Throttler**: 64 `sync.Mutex`, index = key hash & 63, held across `Backend.Get` and
  `Backend.Set` of that key. Lock order: stripe, then whatever the backend locks inside; the
  throttler never holds two stripes.
- No goroutine is started by anything in this design. Timers exist only for queued `loadshed`
  requests and are stopped when the wait ends.

## Security

| Threat | Decision |
|---|---|
| Spoofed `X-Forwarded-For` / `Forwarded` / `X-Real-IP` | read only by `proxy`, only from a trusted peer or at the configured hop (P2-P5); the leftmost (client-written) entries are never used; one header family (P11) |
| Forged chain from inside the trusted range | 16-entry cap (P7), 512-byte entries (P6); the result is always a parsed `netip.Addr`, never header text, so it cannot inject into logs or keys |
| IPv4-mapped IPv6 to dodge a CIDR | unmapped before matching (P2, P6) |
| Forwarded host used for redirects, links, checks | not read unless `IsHostForwarded` (P9, Revision 2026-11-17); then the nearest proxy's value, syntax-validated; `hostcheck` allowlists it; `httpsredirect` takes a fixed `Host` in production (R2) |
| Client-supplied `X-Forwarded-Proto` / `-Host` passed through by the proxy (security M2) | only the last value is read (P8, P9), so a value left of the proxy's own never counts; the host is off by default; the proxy must set (overwrite) every header the app reads, as it must for `X-Forwarded-For` |
| DNS rebinding, host header injection, cache poisoning by host | `hostcheck` (H); responses never echo the host |
| Open redirect | R2, R3: scheme and host are fixed or validated, the path cannot change them |
| CSRF | S table; no token to leak, no cookie to fixate; `same-site` is refused, so a hijacked subdomain cannot post |
| CORS misconfiguration | no default origin; `*` with credentials and `null` panic at startup; wildcard part restricted to host characters (C2), so `https://evil.example/.example.com` cannot match; no reflection of request headers (C7); `Vary: Origin` always (C3) |
| Cross-site WebSocket hijacking | `cors` given to `EnableWS` checks `Origin` (C9); the full WebSocket review is roadmap 0006 |
| HSTS over HTTP, missing headers | M2, defaults table |
| BREACH (compressed secrets next to reflected input, over TLS) | the usual secret, a CSRF token in the page, no longer exists; the README says not to bind `compress` to routes that reflect input next to secrets; no random padding by default (Q6) |
| Compression as a memory or CPU attack | 8 writers (Z8), `gzip.BestSpeed`, `MinSize`, allowlisted types; a slow client holding a writer only makes others go uncompressed; `Accept-Encoding` scan capped at 256 bytes |
| Overload | `loadshed` bounds requests in flight and waiters; refusals carry no request data; the cost of a refusal is the exception path, below a served request |
| Steering the adaptive limit | a client can only add latency samples by being served. Slow requests it sends lower the limit (the intended reaction) but never below `min`; fast ones (404s) cannot raise it above what L10 allows per window, nor above `max`; samples over 30 s are ignored; the baseline only comes from quiet samples or a probe, so a flood cannot teach it a slow baseline |
| Throttler bypass and memory growth | key from `proxy` (T1), /64 for IPv6, 128-byte keys (T2), bounded store (T4). Under more than 32,768 live keys the oldest-written counters are evicted first, which resets idle clients before active ones (Q7) |
| Cached errors, stale assets | F1, F3 |
| Header sizes | the server's 64 KiB `MaxHeaderBytes` is the outer bound; inside it: `Origin` 256 bytes, host 255, forwarded entry 512, `Accept-Encoding` 256; `Access-Control-Request-Headers` is not read |
| Information in refusals | fixed messages (G1); nothing is logged per refusal (an attacker cannot flood the log through these middlewares) |
| Client IP in logs | personal data in many jurisdictions; the README says so and points at the log's masking (Q8) |

Order matters and is documented in the production checklist: `loadshed`, `proxy`, `hostcheck`,
`httpsredirect`, `helmet`, `cors`, `csrf`, `compress`. `proxy` must precede every reader; a reader
bound before it uses the connection's values (P2), which fails safe: `httpsredirect` behind a
TLS-terminating proxy then redirects forever instead of serving over what it thinks is HTTP.

## Impact
- **Breaking:** `cors.CORS{}` panics at `Create()` (list `AllowOrigins`); `sample/main.go` binds
  `cors.CORS{}` and must change. `csrf` front ends stop sending the token (nothing to change on the
  server but the removed fields). `helmet` field removals. Apps behind a proxy must bind `proxy`,
  or the throttler and access log see the proxy's address (roadmap decision). `throttler.NewGuard`
  returns `common.Guarder`.
- `core`: `responseWriter` gains the inner writer and the hook list; `ServeHTTP`, `serveWS` and
  `recoverRequest` run the hooks; `serveContent` / `serveFromRoot` apply F1-F3; `completeEvent`
  fills `ClientIP`.
- `accesslog`: one more attribute (`ip`).
- Docs: a README for each new package and for `internal/httphook`; rewritten READMEs for `csrf`,
  `cors`, `helmet`, `guards/throttler` (with their `README.vi.md`); core README › Request lifecycle
  (hooks are internal: one sentence), Static files, Performance notes; main README production
  checklist (step 14); `.ginject/request-pipeline.md`, `package-reference.md`,
  `anti-patterns-gotchas.md` (read the client IP with `proxy.ClientIP`, never from headers).

## Test plan
Middleware tests run through an app (`core.New()` + `ServeHTTP` with `httptest`) unless noted, since
the scope and the hooks come from `core`. Hostile cases are part of each group.

| Rules | Tests (one function per behavior) |
|---|---|
| G1, G2, G3 | per package: `Test<Name>_RefusalIsHTTPLayerResponse` (status, body bytes, `Content-Type`); `Test<Name>_KeepsRequestAndContext` (pointer equality in the next middleware); `Test<Name>_DoesNotReadBody` (a body that fails the test when read) |
| G4 | `TestNew<Name>_Panics_<Reason>` for every panic listed in P1, H1, R4, S10, C1, M1, Z9, L5 |
| K1-K5 | `core`: `TestResponseWriter_SetInner_ForwardsEveryMethod`, `TestHooks_RunOnceAfterLifecycle`, `_RunInReverseOrder`, `_RunOnMiddlewarePanic`, `_RunOnNoRoute`, `_RunOnAbortHandler`, `_RunAtWSHandshake`, `_ClearedForNextRequest`, `_BeforeCompleteEvent`, `TestServeHTTP_NoHook_NoAllocation` |
| P2, P3 | `TestProxy_Unbound_UsesConnection`, `TestProxy_UntrustedPeer_IgnoresHeaders` (every header family, `X-Real-IP`, both lines repeated), `TestProxy_UnparsableRemoteAddr` |
| P4-P7 | `TestProxy_RightmostUntrusted` (1, 2, 3 proxies), `_AllTrusted_Leftmost`, `_NoHeader_Peer`, `_MultipleHeaderLines`, `_Hops` (n = 1, 2; too few entries), `_EntryForms` (ports, brackets, mapped IPv6, zone, `unknown`, `_hidden`, empty, 513 bytes), `_InvalidEntryStops`, `_SeventeenEntries`, `_SpoofedPrefixCannotMoveResult` (client-written entries left of the real chain) |
| P8-P11, P13 (names revised 2026-11-17: see "Revision 2026-11-17 › Tester") | `TestProxy_Scheme_LastValue`, `_Scheme_SingleValue`, `_Scheme_Invalid`, `TestProxy_Host_OffByDefault`, `_Host_LastValue`, `TestProxy_NoEntry_KeepsConnection`, `TestProxy_Host_Validated` (CR/LF cannot occur; spaces, `/`, `@`, 256 bytes), `TestProxy_Forwarded_Parse` (quoted, case, unknown pairs, IPv6), `TestProxy_OtherFamilyIgnored` |
| P12 | `TestProxy_NeverRefuses_NeverMutatesRequest`; allocations: `TestProxy_Allocations` (0) |
| H1-H4 | `TestHostCheck_Match` (case, trailing dot, port rules, wildcard depth, apex not matched, IP literals), `_Unknown_421`, `_Empty_400`, `_TooLong_400`, `_UsesForwardedHostOnlyBehindProxy`, `_LocalhostNotImplicit`, `_Allocations` |
| R1-R3 | `TestHTTPSRedirect_HTTPS_Passes`, `_Redirects308_KeepsPathAndQuery`, `_FixedHost`, `_Port`, `_StripsRequestPort`, `_NoOpenRedirect` (`//evil`, `/\evil`, `@evil`, encoded CR/LF), `_InvalidHost_400`, `_SchemeFromProxy` |
| S1-S12 | `TestCSRF_Table` (one subtest per row, each unsafe method), `_TrustedOrigin`, `_SameSiteRefused`, `_NullOrigin`, `_ForwardedHost` (S8), `_LongOrigin` (S9), `_SetsNoCookie`, `_Allocations` |
| C1-C9 | `TestCORS_VaryOnEveryResponse`, `_NoOrigin`, `_Actual_Match`, `_Actual_NoMatch_StillServed`, `_Wildcard` (depth, bad characters, suffix tricks), `_Preflight_Exact` (headers and values), `_OptionsWithoutRequestMethod_ReachesRoute`, `_Preflight_NoMatch`, `_Credentials`, `_PrivateNetwork` (both sides of the condition), `_RequestHeadersNeverEchoed`, `_LongOrigin`, `_WebSocketOrigin`, `_Allocations` |
| M1-M4 | `TestHelmet_Defaults` (the table, byte for byte), `_Disabled` (each field), `_Custom`, `_HSTSOnlyOverHTTPS` (direct TLS, forwarded https, plain), `_Allocations` (0) |
| Z1-Z9 | `TestCompress_RoundTrip` (2 KiB, 1 MiB, written in one and in many writes), `_Vary`, `_NotCandidate` (HEAD, Range, Upgrade, no gzip, `gzip;q=0`, gzip after byte 256), `_SkipsStatus`, `_SkipsEncoded`, `_SkipsContentRange`, `_SkipsType` (image, event stream), `_NoTransform`, `_BelowMinSize_Untouched` (with `Content-Length`), `_DetectsContentType`, `_WeakensETag`, `_Flush`, `_ServeFileKeepsReadFrom`, `_NoWriterFree_Uncompressed`, `_ErrorResponseAfterBind`, `_Concurrent` (64 goroutines, `-race`, every body round-trips) |
| L1-L6 (fixed limit) | `TestLoadShed_NeverAboveMax` (blocked handlers, 12 goroutines, `-race`), `_QueueAdmitsInOrder`, `_QueueFull_503` (`Retry-After`), `_QueueTimeout_503`, `_ContextCancelledWhileWaiting`, `_ReleasedOnPanic`, `_ReleasedOnAbort`, `_ReleasedOnNoRoute`, `_ReleasedAtWSHandshake`, `_NoQueue`, `_IdleNoGoroutine` (`runtime.NumGoroutine`), `_Burst` (50 × capacity: every response is 2xx or 503, the count in flight never exceeds the maximum, and it returns to 0) |
| L7-L11 (adaptive, deterministic: the bounds, the clock (`now`) and the samples are injected through unexported fields; revised 2026-11-17, the full list is in the revision's "Tester" section) | `TestAdaptive_Bounds` (table of `GOMAXPROCS`, `GOMEMLIMIT`, cgroup files: present, `max`, missing, garbage), `_NoBaseline_LimitHolds`, `_BaselineFromQuietSamples`, `_BaselineLoweredByFasterWindow`, `_ShrinksWhenSlow` (under pressure; never more than 20 % per window, ends at `min`), `_NeverBelowMin`, `_GrowsWhenFast` (under pressure; never above `max`), `_HoldsWithoutPressure` (slow and fast windows), `_HoldsDuringProbeWindow`, `_QueueWaitNotSampled`, `_IgnoresLongSamples`, `_ProbeOnlyWhenSaturatedAndStale`, `_ProbeEndsAfter8OrOneSecond`, `_ProbeNotRepeatedWithin30s`, `_NeverAboveCurrentLimit` (`-race`, limit changing under load), `_IdleNoGoroutineNoTimer` |
| L12 (acceptance, `GOMAXPROCS` 1 and 2, skipped with `-short`; measured as L12 says) | `TestAdaptive_Stable_CPUBound`, `_Stable_IOBound`, `_NoRefusalBelowCapacity`, `_RecoversWhenHandlerSlows`; the non-yielding CPU-bound handler only in `BenchmarkLoadShed_L12_CPUBoundNoYield_5x` |
| I1-I4 | per package `Test<Name>_Snapshot` (effective options and counters, values after traffic); `core`: `TestDevtool_Snapshots_OnlyWhenEnabled`, `TestSnapshot_NotBuiltOnRequestPath` (0 allocations per request with devtools on) |
| F1-F3 | `core`: `TestServe_DefaultCacheControl` (file, index, `304`, `206`, wildcard and fixed routes), `_HandlerCacheControlKept`, `_ErrorDropsCacheControl` (missing file, dotfile, path outside) |
| T1-T7 | `TestDefaultKey_ClientIP`, `_IgnoresHeaders` (the four tests that today assert header trust are replaced), `_IPv6Slash64`, `_Unknown`, `TestThrottler_LongKeyHashed`, `TestFixedWindow_ExactUnderConcurrency`, `TestSlidingWindow_…`, `TestTokenBucket_…` (12 goroutines × 1,000 requests, `-race`, exactly `Limit` allowed), `TestThrottler_StoreBounded` (1,000,000 distinct keys), `TestThrottler_NoGoroutine`, `TestThrottler_SpoofedHeaderSameKey` |
| A1 | `core`: `TestCompleteEvent_ClientIP` (direct, behind `proxy`); `accesslog`: `TestAccessLog_IP`, `_IPOmittedWhenUnknown` |

## Coordination

**With 0013 (request scope).** The resolved client lives in the request's `tracecontext.Scope`, not
in a new context value and not in the request: no copy, no allocation, one owner. 0014 needs:
1. The scope reachable from the `*http.Request` a middleware receives, in O(1) and without
   allocation (`tracecontext.ScopeOf(r.Context())` today), for HTTP requests and WebSocket handshakes.
2. Four fields on it (`ClientIP netip.Addr`, `ClientHost string`, `IsClientHTTPS`, `IsClientSet`,
   about 48 B), zeroed per request, writable by a middleware on the request's goroutine. If 0013
   pools or moves the scope, the fields move with it and the three reader functions keep their
   signatures.
3. `completeEvent` reads the scope after the lifecycle (A1); if 0013 changes that function, it keeps
   the `ClientIP` line.
4. Both designs touch `ServeHTTP` and `core.responseWriter`: 0013 the request copy and the scope,
   0014 the hook calls (K2) and `SetInner`. Step 1 lands first (the roadmap's order); step 3 adds
   the fields, steps 7 and 8 the hooks.

**With 0015 (`memorycache`).** The throttler keeps `Backend cache.Cache` and needs no new method on
the interface beyond 0015's `Incr` (fixed window counts with it; the other strategies keep their
own stripes). From the default store it needs:
1. A constructor that takes an entry bound (the throttler asks for 32,768) and starts no goroutine;
   no `Stop` call required for a store that is dropped.
2. `Set` and `Incr` on a full store never fail and never block: they evict an expired entry, else
   one chosen by 0015's CLOCK policy (an approximation of least recently used; exact
   least-recently-written order is not required). Eviction by nearest expiry is not enough:
   fixed-window keys of one window all expire together.
3. Expiry on access, so a counter is never read after its TTL even if no sweep ran.
4. A way for a test to read the entry count (`Keys` works).
5. The per-entry memory figure, so step 10 can state the bound in bytes.

**Access log.** Reads `trace.Event.ClientIP` only; it never touches headers or the scope.

**With the devtools (later roadmap).** Today `devtool` shows only what `Create()` knew: the layer
names per route (`DevtoolBuilder().AddMiddlewares(...)`, `GetConfiguration`); it reads no option and
no runtime state, and `core` keeps only each middleware's `Use` method value. 0014 adds the seam,
not the screens:

- **I1** Every compiled middleware of this design and the compiled throttler implement
  `inspect.Snapshotter`. `Snapshot()` is safe from any goroutine at any time, builds its result
  when called (atomic loads, and the short mutex for queue lengths), and nothing calls it on the
  request path.
- **I2** Counters are atomics the request path already updates, or that only a refusal updates
  (one `Add` on a path that already costs an exception). No counter is added to an admitted,
  passing request. With devtools off the cost of all this is zero: no snapshot is built, no extra
  field is written.
- **I3** `core` keeps the constructed layer value next to its `Use` / `CanActivate` (one interface
  word pair per layer, at `Create()`), and, only when `EnableDevtool` was called, hands
  `devtool` the list of layers that are `Snapshotter`s. How `devtool` exposes them is the devtools
  roadmap's design; no public API is added here.
- **I4** Contents:

| Component | Effective configuration | Runtime state |
|---|---|---|
| `loadshed` | mode (adaptive or fixed), `min`, `max`, queue size, wait, `Retry-After` | current limit, in flight, waiting, refused (queue full / timed out / cancelled), baseline latency, last window's average latency, probes, whether a probe is running |
| throttler | limit, TTL, strategy, whether `KeyFunc` and `Backend` are custom, the store's bound | keys (the store's count), evictions (from the store), rejected |
| `proxy` | mode, trusted prefixes, hops, header family | requests resolved from headers, requests from untrusted peers |
| `hostcheck` | hosts | refused |
| `httpsredirect` | host, port | redirected |
| `csrf` | trusted origins | refused |
| `cors` | every option after defaults | preflights answered, origins refused |
| `helmet` | every header as sent (`Disabled` shown) | none |
| `compress` | `MinSize`, level, writer cap | writers in use, responses compressed, sent uncompressed for lack of a writer, bytes in and out |

- From 0015 the throttler's snapshot needs a cheap entry count and an eviction counter on the
  store (two atomics there); with a custom `Backend` those two fields are absent.
- `proxy`'s two counters and `cors`'s preflight counter are the only ones on a passing request
  (one `Add` each, about 5 ns); if the maintainer wants I2 without exception they are dropped.

## Open questions
1. **Where the client is read.** Recommended: three functions in `proxy` taking `*http.Request`
   (work in middlewares, guards and handlers; validated types; the peer address stays in
   `r.RemoteAddr` for audits). Alternatives: methods on `*ctx.HTTPContext` (`c.ClientIP()`; not
   reachable from a middleware, and a `Host` method would shadow the promoted `r.Host`), or `proxy`
   rewriting `r.RemoteAddr` in place (naive code becomes right, but the peer is lost, a port must be
   invented and the scheme has no field).
2. **Exchange hooks in `core` (K), for approval.** The linear chain cannot compress a response or
   release a slot after the handler. Recommended: the internal `httphook` seam (no public API, no
   lifecycle change, about 1 ns when unused). Alternative: ship `compress` and `loadshed` as
   `func(http.Handler) http.Handler` around `App`; that is a second way to add an HTTP layer and
   does not work with `app.Listen`.
3. **`helmet`'s CSP and COEP.** Recommended CSP is stricter than today's (no `https:` and no
   `'unsafe-inline'` for styles, `frame-ancestors 'none'`): pages with inline styles or CDN assets
   must set their own policy. `upgrade-insecure-requests` is kept per OWASP; it can break plain-HTTP
   testing from another device (not `localhost`). COEP is no longer sent by default, against OWASP's
   list, because it breaks cross-origin images and scripts. Approve both, or keep today's values.
4. **`cors` strictness.** Recommended: empty `AllowOrigins` panics (today: allows all), regexp
   origins are removed in favor of `*.` patterns, default allowed headers are `Content-Type,
   Authorization` (today: whatever the browser asks).
5. **`loadshed` constants (the adaptive default itself is decided).** Recommended: tolerance 1.5,
   windows of 32 samples, probe at most every 30 s and only while saturated, `min` = `GOMAXPROCS`,
   `max` = memory / 4 MiB (64-4,096), queue 64 for 100 ms, `Retry-After: 1`; none configurable; no
   exemption for health probes. To confirm: (a) L12's thresholds as the bar for "no oscillation";
   (b) the fallback if step 8 misses them (fixed limit = half of L7's `max`); (c) reading the cgroup
   files (two small reads at `Create()`, Linux only).
6. **`compress` fixed choices and techniques, for approval.** `gzip.BestSpeed` (3 × the throughput
   of the default level on 16 KiB JSON), 8 writers (about 6.5 MiB at peak), no level or type option,
   no BREACH padding; the two `sync.Pool`s of the Performance table.
7. **Throttler.** (a) Keep `X-RateLimit-*` (recommended: the IETF `RateLimit` fields are still a
   draft whose syntax changed between versions) or move to the draft. (b) A full default store
   evicts the oldest-written counter, which fails open for that client; the alternative, refusing
   new keys, lets a key flood lock out new clients. Recommended: evict. (c) 32,768 entries as the
   default bound, with no option (a larger app passes its own `Backend`).
8. **Access log prints the client IP** (`ip`) by default. Recommended: yes, as every access log
   format does; the README names it as personal data.
9. **Status line.** `docs/design/README.md` defines `Proposed`; this file says `Draft` as the
   roadmap run asked while 0013 and 0015 are written in parallel. Switch to `Proposed` once the three
   are reconciled.

## Decisions (2026-11-17)

Approved by the user. All open questions answered as recommended (Q1-Q8), with the CSP note: Owlky's HTML snippets carry no inline styles, and apps should serve Owlky's assets from their own origin (`'self'`); per-request nonces for inline scripts come with the server-side rendering roadmap. Reconciled with 0015: the default store evicts by CLOCK (T4, Coordination 2) and the fixed window counts with `Incr` (T3). The user's condition: the public API stays as small and simple as the Product principles require, and every name is documented clearly (the docs will feed a website later).

## Revision 2026-11-17: forwarded scheme and host (security M2)

Status of this revision: Approved by the orchestrator under the roadmap run's escalation rule (2026-11-17). It replaces P8 and P9, adds P13
and one option. Source: finding M2 in `docs/review/security-0005.md`, and the failed first fix
(roadmap 0005, step 13).

### Why the count rule is withdrawn
The first fix accepted `X-Forwarded-Proto` / `-Host` only when they held as many values as
`X-Forwarded-For` had entries. It is wrong in both directions:

- It refuses the normal setup. Proxies overwrite the scheme and host with one value (nginx
  `proxy_set_header X-Forwarded-Proto $scheme`, Go `ProxyRequest.SetXForwarded`, Caddy, Traefik,
  Envoy, cloud load balancers) while `X-Forwarded-For` grows by one entry per hop, and a client can
  add entries of its own. One value against two entries is the common case, not an attack.
- It does not stop the attack. A client that sends as many values as there will be entries passes
  the check.

No rule can tell "the proxy wrote this value" from "the proxy passed the client's value through" by
looking at the request: only the operator knows which headers the proxy sets. Every mature
implementation settles it the same way, by configuration:

| Implementation | Scheme | Host |
|---|---|---|
| Django | off until `SECURE_PROXY_SSL_HEADER` is set | off until `USE_X_FORWARDED_HOST`; `ALLOWED_HOSTS` on top |
| ASP.NET `ForwardedHeaders` | one flag per header, none by default; values read from the right, `ForwardLimit` 1 | its own flag, plus `AllowedHosts`; `RequireHeaderSymmetry` (the count rule) is off by default since 2.1 because real proxies fail it |
| Express `trust proxy` | read once the peer is trusted (leftmost value) | read once the peer is trusted; the documented source of host poisoning when the proxy does not overwrite |
| Rails / Rack 3 | read from the right | read; `config.hosts` allowlist on top |
| Caddy, Traefik (as proxies) | overwrite from an untrusted client, keep from a trusted proxy: one value reaches the app | same |
| Go `httputil.ReverseProxy` | `SetXForwarded` overwrites with one value | same |

### Decision
1. **The host is opt-in** (`IsHostForwarded`, default false; P9). The common nginx snippet sets
   `Host`, `X-Forwarded-For` and `X-Forwarded-Proto` and leaves `X-Forwarded-Host` to the client;
   the host is also the high-impact half of M2 (a cacheable `308` to another site, poisoned links
   and reset mails). By default `proxy.Host(r)` is `r.Host`, which the proxy routed on. The option
   is the operator's statement "my proxies set the forwarded host".
2. **The scheme stays on, read from the last value only** (P8). Behind a TLS-terminating proxy the
   scheme is needed for `httpsredirect` and `helmet` to work at all, so an opt-in would make the
   first deployment of every user loop on redirects. The last value is the one the nearest trusted
   proxy wrote or kept: a client's value left of it never counts, whatever the counts are. The
   position alignment of the old P8 is dropped: it assumed every proxy appends to the scheme header,
   which almost none does, and its "leftmost when fewer" fallback is what picked the client's value.
3. **`Forwarded` keeps its element rule.** Each element is written by one proxy, so `proto` and
   `host` are bound to the hop that wrote them. The host is still behind `IsHostForwarded`, so one
   option means the same thing in both families.
4. **`httpsredirect` does not change** (R1-R4). With the default it redirects to `r.Host` or to its
   fixed `Host`.

### Public API
One addition: the field `IsHostForwarded bool` on `proxy.Proxy` (zero value: off).

- Why a field: without it there is no safe way to use a forwarded host, and apps whose proxy
  rewrites `Host` to an internal name (nginx without `proxy_set_header Host $host`, most platform
  routers) need the public host for `hostcheck`, `csrf` (S5, S8) and redirects.
- Why a boolean and not a set of header flags (ASP.NET): the client IP is the reason to bind
  `proxy`, and the scheme must work by default, so only the host needs a choice.
- Why no allowlist of hosts on `proxy`: `hostcheck` is that allowlist (one obvious way).
- The name follows the `Is…` rule of `.ginject/conventions.md` and reads as the operator's
  statement about the proxies.

No other exported name changes. Unexported: the compiled option gains one boolean; the count helpers
of the first fix go away.

### Behavior
P8, P9 and P13 in "Behavior › `proxy`" above are the contract. Worked cases (peer `10.0.0.1`,
`TrustedProxies: 10.0.0.0/8`, `r.Host` `example.com`, plain connection):

| `X-Forwarded-For` | `X-Forwarded-Proto` | `X-Forwarded-Host` | `IsHostForwarded` | `Scheme` | `Host` |
|---|---|---|---|---|---|
| `1.1.1.1` | `https` | `app.example.com` | false | `https` | `example.com` |
| `1.1.1.1` | `https` | `app.example.com` | true | `https` | `app.example.com` |
| `9.9.9.9, 1.1.1.1` | `https` | none | either | `https` | `example.com` |
| `9.9.9.9, 1.1.1.1` | `http, https` | `evil.example, app.example.com` | true | `https` | `app.example.com` |
| `9.9.9.9, 1.1.1.1` | `https, http` | `app.example.com, evil.example/x` | true | `http` | `example.com` |
| `1.1.1.1, 10.0.0.2` | `https, http` | none | either | `http` | `example.com` |
| `1.1.1.1` | `https, ` or `https, ftp` | none | either | `http` | `example.com` |
| none | `https` | `app.example.com` | true | `http` | `example.com` |

Two header lines `X-Forwarded-Proto: https` then `X-Forwarded-Proto: http` give `http` (the last
line). Hops mode selects the same last value. Snapshot (I4): one more field, `hostForwarded`
(`bool`), after `header`.

### Performance
Hot path, cheaper than before: the last value is one backward scan for a comma in the last line (at
most 513 bytes), with no position loop and no counting; without `IsHostForwarded` the host header is
not looked up. Still 0 allocations; the existing `BenchmarkProxy_*` must not regress.

### Concurrency
Unchanged: the new boolean is written once in `NewMiddleware` and only read afterwards.

### Security
- What the fix guarantees: a header value the nearest trusted proxy did not put last is never used;
  a forwarded host is never used unless the operator turned it on.
- What stays the operator's job (README, "Security"): the proxy must **set**, not pass through,
  every header the app reads: `X-Forwarded-For` (as before), `X-Forwarded-Proto`, and
  `X-Forwarded-Host` when `IsHostForwarded` is on. A proxy that sets none of them lets a client claim
  `https` for its own request (no redirect, HSTS sent on plain HTTP) or `http` (a `308` to the same
  URL, which a shared cache in front could store). The README gives the lines for nginx, Caddy and
  Traefik.
- With `IsHostForwarded`, bind `hostcheck` after `proxy`, and give `httpsredirect` a fixed `Host`.
- Hops mode does not check the peer (finding L2, documented): there the same statements hold only
  when the app is unreachable except through the proxies.

### Impact
- `middlewares/proxy/proxy.go`: the only code that changes (list below). `httpsredirect.go`,
  `hostcheck`, `csrf`, `helmet`, `internal/tracecontext`: no code change.
- Behavior change for users of the unreleased 0014 batch: a forwarded host needs
  `IsHostForwarded: true`; a multi-value `X-Forwarded-Proto` is read from the right end.
- Docs: `middlewares/proxy/README.md` (features, options table, `Host`, "Scheme and host",
  "Security" with the proxy lines), `middlewares/httpsredirect/README.md` (drop the count
  paragraph), `middlewares/hostcheck/README.md` lines 20 and 106, `middlewares/csrf/README.md` and
  `README.vi.md` (forwarded host only with the option), `internal/tracecontext/README.md`,
  `.ginject/package-reference.md`, `.ginject/request-pipeline.md`, `.ginject/anti-patterns-gotchas.md`.

### Builder: changes in `middlewares/proxy/proxy.go`
1. `Proxy` gains `IsHostForwarded bool`; `compiledProxy` gains `isHostForwarded`, set in
   `loadProxyOptions`. No new panic.
2. Delete `selectAligned` and `countEntries` (the uncommitted first fix).
3. Replace `selectValue(lines, position)` with a last-value helper: the first entry the existing
   `entryIterator` yields, `""` when there is none or it is over `maxEntryLength`, trimmed.
4. `scheme`: for `XForwarded`, the last value of `X-Forwarded-Proto`; `Forwarded` unchanged.
5. `host`: return `""` first when `isHostForwarded` is false (both families); for `XForwarded` the
   last value of `X-Forwarded-Host`; `Forwarded` unchanged; then `IsValidHost` as today.
6. `resolve` keeps its early return when `res.position == 0` (P13).
7. `Snapshot`: add `{Key: "hostForwarded", Value: c.isHostForwarded}` after `header`.
8. READMEs and knowledge base as listed in Impact, replacing the count-rule text now in the working
   tree.

### Tester: test changes
`middlewares/proxy/proxy_count_test.go`: delete the file (all four tests assert the count rule).

`middlewares/proxy/proxy_test.go`:

| Test | Change |
|---|---|
| `TestProxy_Scheme_AlignedWithClient` | rename `TestProxy_Scheme_LastValue`; expectations become: `1.1.1.1, 10.0.0.2` + `https, http` → `http`; + `http, https` → `https`; hops 2 + `https, http` → `http`; hops 1 unchanged (`http`); add two header lines `https` then `http` → `http` |
| `TestProxy_Scheme_SingleValue` | expectations unchanged (this is the nginx case); new message "P8: one value written by the proxy, whatever the number of entries"; add `9.9.9.9, 1.1.1.1` + `https` → `https` |
| `TestProxy_Scheme_Invalid` | unchanged; add `"https, "` → `http` |
| `TestProxy_Host_Validated` | the proxy is `Proxy{TrustedProxies: 10.0.0.0/8, IsHostForwarded: true}`; lists unchanged |
| `TestProxy_Host_AlignedWithClient` | rename `TestProxy_Host_LastValue`, with the option: `1.1.1.1, 10.0.0.2` + `evil.example, app.example.com` → `app.example.com`; `app.example.com, evil.example/x` → `example.com` (no fallback to an earlier value) |
| new `TestProxy_Host_OffByDefault` | without the option, `Host(r)` is `example.com` and `Scheme(r)` is still `https` for: `XForwarded` trusted mode, `XForwarded` hops mode, and `Header: Forwarded` with `for=1.1.1.1;proto=https;host=app.example.com` |
| new `TestProxy_XForwarded_ClientValuesLeftOfProxyValueIgnored` | replaces `…ExtraClientValueCannotChooseHost`: `9.9.9.9, 1.1.1.1`, host `evil.example, app.example.com`, proto `http, https` → with the option `app.example.com` / `https`; without it `example.com` / `https` |
| new `TestProxy_NoEntry_KeepsConnection` | P13, with the option: trusted peer, no `X-Forwarded-For`, proto `https`, host `app.example.com` → `http` / `example.com`; same for `X-Forwarded-For: unknown`; same for hops 2 with one entry |
| `TestProxy_Forwarded_Parse`, `TestProxy_OtherFamilyIgnored` | add `IsHostForwarded: true` to the proxies; expectations unchanged |
| `TestProxy_InApp_HandlerReadsClient` | proxy with the option; send `X-Forwarded-Proto: https` and `X-Forwarded-Host: app.example.com` (one value each); expected body unchanged |
| `TestProxy_Snapshot` | `hostForwarded` is `false` by default and `true` with the option |
| `TestNewProxy_ValidConfigs` | add the option with each mode |

`middlewares/httpsredirect/httpsredirect_test.go`:

| Test | Change |
|---|---|
| `TestHTTPSRedirect_SchemeFromProxy`, second case | default proxy → `https://internal.local/ok`; add the same request on an app whose proxy has `IsHostForwarded: true` → `https://app.example.com/ok` |
| `…ProxyForwardedHostCountMismatchCannotRedirectElsewhere` | rename `TestHTTPSRedirect_ForwardedHostIgnoredByDefault`; body and expectation unchanged |
| `…ProxyForwardedProtoCountMismatchCannotTurnOffRedirect` | delete (its request is the legitimate one-value case and now passes without a redirect); add `TestHTTPSRedirect_ClientProtoLeftOfProxyValueStillRedirects`: proto `https, http` → `308` to `https://example.com/x` |

`middlewares/hostcheck/hostcheck_test.go`, `TestHostCheck_UsesForwardedHostOnlyBehindProxy`: the
proxy gets `IsHostForwarded: true`; add a default-proxy case: `r.Host` `internal.local:8080`,
`X-Forwarded-Host: app.example.com` from the trusted peer → `421`.

`middlewares/csrf/csrf_test.go`, `TestCSRF_ForwardedHost`: the proxy gets `IsHostForwarded: true`;
add a default-proxy case: origin `https://app.example.com` with that forwarded host → refused.

`middlewares/helmet`, `guards/throttler`, the proxy benchmarks: no change.

### Open questions (this revision)
1. **`IsHostForwarded` also gates `Forwarded`'s `host=`.** Recommended: yes (one option, one
   meaning, safe default). Alternative: gate `X-Forwarded-Host` only, since `host=` is bound to its
   element; that makes the option's effect depend on `Header`.
2. **Last value instead of alignment for `X-Forwarded-Proto`.** Recommended: yes. Cost: a chain in
   which every proxy appends its own scheme (`https, http`) now reports the inner hop, and
   `httpsredirect` would loop; the fix on that setup is to let the inner proxy keep the edge's value
   (what Caddy and Traefik `trusted_proxies` and nginx `$http_x_forwarded_proto` do), and the loop
   shows on the first request rather than as a silent hole.
3. **The scheme is not opt-in.** Recommended: keep it on. Alternative: a second boolean, at the price
   of a redirect loop in every default deployment behind a TLS-terminating proxy.

## Revision 2026-11-17: `loadshed` adaptive limit (roadmap 0005, step 8)

Status of this revision: Approved by the orchestrator under the roadmap run's escalation rule (2026-11-17), with the recommended answers: L12's CPU-bound handler yields and a never-yielding handler is a documented known limit with a benchmark; the first limit is 8 × `GOMAXPROCS`; the window is 4 × the current limit samples (what the passing L12 runs used). It replaces L8 and L10, amends
L11 and the way L12 is measured. No public API change. The maintainer's decision stands: the default
adapts, within bounds from the machine, with a fixed N as the override. Source: the builder's report
on step 8 (L12 passed 6 of 8) and the measurements below.

Measurements: a prototype of the rules in a copy of the working tree, go1.27.4 darwin/amd64, a
shared machine with 12 logical CPUs (hyperthreads; a capacity measured twice in one process ranged
from 84 to 223 requests per second). Each run: 10 s of warm-up, then 10 s measured at 5 × capacity.
Limits are min / median / max; p99 is a multiple of the no-load latency.

### What was wrong
1. **The model, not the thresholds.** The table of "Why the gradient" assumed processor sharing: n
   requests on one core each take n times longer. Go does not interleave a handler that computes
   for 5 ms: a goroutine keeps its P for up to 10 ms. Requests overlap only when one is preempted
   (the GC, an inherited time slice), and that one then waits behind the run queue. With fixed
   limits, 1 CPU, the tester's 5 ms handler:

   | Fixed limit | Median latency | p99 | Window average (p10 / p50 / p90 of the 32-sample windows) |
   |---|---|---|---|
   | 2 | 1.00 × | 2.1-4.1 × | 0.98 / 1.00 / 1.59 × |
   | 3 | 1.00 × | 6.9 × | 1.00 / 1.00 / 4.05 × |
   | 4 | 1.00 × | 7.4-8.9 × (the builder saw 21-23 ×) | 1.00 / 1.00 / 2.28 × |
   | 8 | 1.06 × | 70 × | 1.03 / 1.06 / 4.73 × |
   | 16 | 1.10 × | 170 × | 1.03 / 1.10 / 11.3 × |

   More than half of the windows show no slowdown at any limit; the damage is in a few requests.
2. **L10 could not settle low.** With the gradient floored at 0.5 and `sqrt(limit)` of headroom, the
   fixed point is `sqrt(limit) = 1 / (1 - gradient)`: never below 4 (the builder's finding, confirmed).
   And a window with gradient 1 grew the limit whenever half of it was in use, so the limit climbed
   between the rare slow windows: the implemented rule gave 8 / 12 / 19 with a p99 of 1.53 s (316 ×)
   in one run and 4 / 5 / 8 with 15 × in another.
3. **L8 fed the limit its own effect.** A queued request's `elapsed` holds its wait in `loadshed`'s
   queue, which grows when the limit shrinks. The builder's deviation (leave the wait out) is right
   and is now the rule.
4. **The fallback is not an answer.** Half of `max` is 128 on the small box; a fixed 16 already has a
   p99 of 170 ×.
5. **The harness, for the throughput failure.** `RecoversWhenHandlerSlows_1CPU` (60.7 against 63)
   fails with any limit: a fixed limit of 1 served 80.6 % of the capacity, and 108.6 % with
   `GOGC=off`. Each arrival built an `httptest.NewRequest` (about 6 KB, 1,000 per second), and the
   GC worker took the only P. The capacity itself, measured once for 2 s, moved by a factor of two
   between runs.

### Decision
The rule stays the gradient on latency against the baseline, with four changes (L8, L10, L11 above):

1. The limit moves only at the close of a window **under pressure** (a request was queued or
   refused in it). This replaces the "half of the limit in use" test and its peak counter. Without
   pressure the limit bounds nothing, so there is nothing to learn; growth between slow windows is
   gone, and an idle or lightly loaded server never loses slots.
2. The gradient has no floor, and the headroom is `sqrt(limit × gradient)`, not `sqrt(limit)`. The
   smoothing already bounds a window's loss to 20 %. The fixed point for c evenly shared cores is
   1.5 c + sqrt(1.5 c) instead of "at least 4".
3. A sample leaves out the wait in `loadshed`'s queue.
4. A window during which a probe ran does not move the limit (it measured `min`), and probes are at
   least 30 s apart. Without the spacing, handlers slower than 1 s end every probe empty, the
   baseline stays unknown, and admission falls to `min` for 1 s after every window.

Rejected: a limit from the completion rate (`samples × baseline / window time`, Little's law). It
was the steadiest signal measured (useful concurrency 0.89-1.11 on one core, window after window)
and gave 2 / 2 / 3 on 1 CPU, but an idle gap inside a window reads as a slow server, so bursts
separated by silence erode the limit to `min`. Rejected: moving the limit without pressure when
latency rises; a window that happens to hold the slow routes of a mixed app would shrink the limit
on a quiet server.

### Results (prototype of the revised rules, first limit 16 × `GOMAXPROCS`)
| Run | Limit | p99 | Throughput / capacity |
|---|---|---|---|
| CPU-bound (L12's handler), 1 CPU | 3 / 4 / 4 | 4.5 × | 99 % |
| CPU-bound (L12's handler), 2 CPUs | 4 / 5 / 5 | 3.0 × | 99 % |
| I/O-bound (20 ms, pool of 50), 1 and 2 CPUs, four runs | 80-81 / 83 / 86-87 (± 5 %) | 1.8-1.9 × | 106 % |

The eight L12 tests, changed as "Tester" says, passed 16 of 16 in two consecutive passes (an earlier
variant of the rule, three passes with a second test process running beside it: 23 of 24, the miss
being 107 against 108 requests per second). In those passes the window was 4 × limit samples; the
I/O rows above are with the 32-sample window, which is what L8 keeps. Admitted cost:
`BenchmarkLoadShed_Admitted_Adaptive` 40-41 ns, 0 allocations, at `GOMAXPROCS` 1 and 2 (the
builder's code in the same run, `GOMAXPROCS` 1: 42-43 ns; its 66 ns was measured under load).

### Known limit: a handler that never reaches the scheduler
With the tester's original handler (5 ms of arithmetic, no allocation, no wait) overload can hide
from any in-flight limit, fixed or adaptive: requests run one after another, the count in flight
stays below the limit, and the excess waits in Go's run queue. `loadshed` sheds once enough requests
have been preempted to fill the limit. Measured with the revised rule:

| First limit | 1 CPU | 2 CPUs |
|---|---|---|
| 16 × `GOMAXPROCS` (L7) | 2 / 3 / 4, 3 / 3 / 5, 2 / 3 / 3; p99 2.7-9.1 × | 4 / 5 / 5 once; 7 / 8 / 33 and 7 / 8 / 32 with p99 20-28 × (the limit was still leaving 32 when the measure began) |
| 8 × | 2 / 4 / 5, 3 / 3 / 5, 2 / 3 / 3; p99 1.1-7.1 × | 5 / 5 / 6, 4 / 5 / 5, 4 / 5 / 6; p99 5.0-5.7 × |
| 2 × | 2 / 3 / 3, 2 / 3 / 5, 2 / 3 / 3; p99 2.8-5.0 × | 4 / 4 / 5, 4 / 4 / 5, 6 / 6 / 7; p99 4.0-6.0 × |

(The 8 × row is with this rule; the 2 × row with the earlier variant.) Once pressure exists the
limit converges; how long that takes depends on the first limit. The signal is sparse, so on 1 CPU
the limit sometimes leaves ± 35 % (2 to 5 around 3). That is why L12 asserts the handler that
yields, and this one is recorded by a benchmark and stated in the README with the workaround
(`LoadShed{MaxInFlight: 2 × CPUs}`: a fixed 2 on 1 CPU refused the excess with a p99 of 2.1-4.1 ×).

### Performance
One atomic load less per finished request (no peak). No new goroutine, timer or allocation; idle
cost unchanged. `now` is called at a window close and during a probe only.

### Concurrency
As the "Concurrency" section, with the pressure flag and the two plain fields added there.

### Security
Unchanged. A client cannot push the limit up without the server being fast (growth needs pressure
and `avg` under 1.5 × baseline), and the limit stays within `min`-`max` (L7).

### Builder: changes in `middlewares/loadshed`
1. `adaptive.go`, L10: delete `minGradient`, `peak` and `raisePeak`. At a window close, adjust only
   when the window was under pressure, a baseline exists and no probe ran during the window;
   `useful = limit × min(1.5 × baseline / avg, 1)`, `target = useful + sqrt(useful)`, then the
   smoothing and clamp that exist today.
2. "A probe ran during this window": a field under the window mutex, set when a probe starts,
   read at the close together with `admit < 0`, then set to whether the probe still runs.
3. L11: remember when the last probe started; start one only when that is more than 30 s ago (or
   never). An empty probe leaves the baseline as it was.
4. L11: an unexported field `now func() time.Time` on `adaptive`, set to `time.Now` in `init`;
   every clock read in `adaptive.go` goes through it. The queue wait in `loadshed.go` keeps
   `time.Now` / `time.Since`.
5. L8: keep `waiter.Done` as it is (`elapsed - waited`); it is now the rule.
6. README: "How the limit adapts" (the update rule and its conditions), "Known limits" (the
   paragraph above with the workaround), "Benchmarks" (rerun at `GOMAXPROCS` 1 and 2).
7. No change to `loadshed.go` admission, the queue, the snapshot fields or the public API.

### Tester: test changes
1. `loadshed_bench_test.go`: `BenchmarkLoadShed_Admitted_Fixed` uses `MaxInFlight: 1_000_000`
   (`1 << 20` is above L5's maximum and panics).
2. Delete the nine unused declarations: `processCPU` (both `cpu_*_bench_test.go` files),
   `(*rig).inFlight`, `outcome.noLoadMillis`, `trackWriter` with `newTrackWriter` and its three
   methods, `raceEnabled` (both `race_*_test.go` files).
3. `loadshed_stability_test.go`: `serveDirect` reuses one package-level `*http.Request`; `cpuWork`
   runs five steps of `iterations / 5` with `runtime.Gosched()` after each; `stableRun` and
   `recoversRun` measure the capacity again after the measured run (same workers, same factor) and
   compare with the smaller value. Thresholds stay as they are.
4. `loadshed_l12_bench_test.go`: add `BenchmarkLoadShed_L12_CPUBoundNoYield_5x` with today's
   `cpuWork` (one uninterrupted spin), reporting the same metrics.
5. `loadshed_adaptive_test.go`, with a rig helper that puts a window under pressure (fill to the
   limit, then one more request that must be refused, `MaxQueue: -1`) and a fake clock set on
   `mw.(*compiledLoadShed).adaptive.now`:
   - `_ShrinksWhenSlow`: baseline 10 ms, one window under pressure at 60 ms gives 27; further such
     windows never grow the limit, never go below `previous × 4 / 5`, and end at `min` (2).
   - `_GrowsWhenFast`: baseline 10 ms, one window under pressure at 10 ms gives 33; further such
     windows never shrink it and reach `max` (64) within 300.
   - `_NeverBelowMin`: as today, under pressure.
   - `_HoldsWithoutPressure` (replaces `_HoldsWhenAppLimited`): with a baseline, 20 windows at
     100 ms and 20 at 10 ms, none with a refusal, any number in flight: the limit stays 32.
   - `_HoldsDuringProbeWindow`: a window in which a probe started, ran or ended leaves the limit,
     whatever its average.
   - `_QueueWaitNotSampled`: a request that waited at least 20 ms in the queue and finishes with
     `elapsed` = 25 ms, in a window whose 31 other samples are 5 ms: the snapshot's `average` is at
     most 5 ms.
   - `_ProbeNotRepeatedWithin30s`: no baseline, a window under pressure starts probe 1; the clock
     moves 1.1 s with no probe request finished and the next admission ends it (`probing` false,
     `baseline` 0); another window under pressure starts no probe; after 30 s more it starts probe 2.
   - `_NoBaseline_LimitHolds`, `_BaselineFromQuietSamples`, `_BaselineLoweredByFasterWindow`,
     `_IgnoresLongSamples`, the probe tests, `_NeverAboveMaxUnderConcurrency`,
     `_IdleNoGoroutineNoTimer` and `_Snapshot` keep their assertions.

### Open questions (this revision)
1. **L12's CPU-bound handler.** Recommended: the handler that yields (five 1 ms steps), as L12 now
   says, with the non-yielding one as a documented known limit and a benchmark. Alternative: keep
   the non-yielding handler as the bar; then no in-flight limit, adaptive or fixed above about
   2 × `GOMAXPROCS`, meets it reliably, and the only default that does is a small fixed one, which
   refuses I/O-bound apps far below their capacity (the I/O runs need about 80).
2. **The first limit (L7).** Recommended: 8 × `GOMAXPROCS` instead of 16 ×. It converged in 12 of
   12 non-yielding runs (six with this rule, six with the earlier variant), where 16 × left the limit at 32 for the whole warm-up twice in three runs
   on 2 CPUs. Price: an I/O-bound app sees its first bursts above 8 per CPU queue for up to 100 ms
   until the limit has grown (about `0.2 × sqrt(limit)` per 32 held-back requests). L7 keeps 16 ×
   until this is decided; nothing else depends on it.
3. **The rule is still "the gradient"** approved on 2026-11-17 (same inputs, tolerance, smoothing,
   baseline and probe), with the four changes of "Decision". Confirm, since the formula text changed.
