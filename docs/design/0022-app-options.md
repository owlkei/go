# 0022: App options

Status: Approved
Packages: root package (`aliases.go`, new `options.go`), `internal/core` (`app.go`, `server.go`, `http.go`,
`ws.go`, new `options.go`, `options_env.go`, `byte_size.go`, `security_check.go`), `middlewares/*` →
`internal/middlewares/*`, new `internal/middlewares/origin`, new `internal/dotenv`, `modules/config`,
`guards/throttler`, `internal/trace`, `internal/devtool`, `api/owlky.txt`

Covers steps 1-8 of `docs/roadmap/0013-app-options.md`, plus the declaration of roadmap 0012 step 1c(a).
The roadmap's Decisions and `docs/roadmap/backlog.md` › "App options" and "Less configuration in `main`"
(the "`AppOptions` from the environment" part) are taken as given and not repeated here.

## Goal
`owlky.New(&owlky.AppOptions{…})` holds the whole configuration of an app; `owlky.OptionsFromEnv()` fills
the same struct from `OK_*` variables and `.env`. The eight built-in middlewares become internal
pre-route stages that run in a fixed order, each off until configured. One `Origins` list serves CORS,
CSRF and the WebSocket Origin check. `app.Listen()` takes no argument. A startup warning names a missing
CSRF or security headers protection.

## Public API

### Root package

```go
type (
	AppOptions           = core.AppOptions
	ServerOptions        = core.ServerOptions         // changed
	TLSOptions           = core.TLSOptions
	HTTPOptions          = core.HTTPOptions
	ProxyOptions         = core.ProxyOptions
	ProxyHeader          = core.ProxyHeader
	LoadShedOptions      = core.LoadShedOptions
	HTTPSRedirectOptions = core.HTTPSRedirectOptions
	HeadersOptions       = core.HeadersOptions
	CORSOptions          = core.CORSOptions
	CSRFOptions          = core.CSRFOptions
	CompressOptions      = core.CompressOptions
	ByteSize             = core.ByteSize
	// unchanged: RenderOptions, WSOptions, VersioningOptions, VersioningType
)

const (
	ProxyHeaderXForwarded = core.ProxyHeaderXForwarded // 0
	ProxyHeaderForwarded  = core.ProxyHeaderForwarded  // 1
)

const (
	KB  = core.KB  // 1000
	MB  = core.MB  // 1000²
	GB  = core.GB  // 1000³
	KiB = core.KiB // 1024
	MiB = core.MiB // 1024²
	GiB = core.GiB // 1024³
)

func New(opts *AppOptions) App            // was New()
func OptionsFromEnv() *AppOptions

func ClientIP(r *http.Request) netip.Addr // was proxy.ClientIP
func ClientScheme(r *http.Request) string // was proxy.Scheme
func ClientHost(r *http.Request) string   // was proxy.Host
```

`ClientIP`, `ClientScheme`, `ClientHost` keep the behavior of the `proxy` functions (design 0014 P2-P9);
they move because `middlewares/proxy` leaves the public surface and `throttler.KeyFunc`, handlers and user
middlewares still need "who is the client" (open question 1).

### `App` methods

```go
func (app *App) Listen() error                // was Listen(port int, opts *ServerOptions)
func (app *App) Serve(l net.Listener) error   // was Serve(l, opts *ServerOptions)
```

Removed: `SetLogger`, `SetStartupTimeout`, `EnableAccessLog`, `EnableDevtool`, `EnableRender`,
`EnableVersioning`, `EnableWS`, `ListenTLS`. Kept: `Create`, `BindGlobal…`, `ServeHTTP`, `Stop`,
`Logger`, `Get`.

### `AppOptions` and its groups (`internal/core/options.go`)

Godoc as written below goes on every field (roadmap decision; the no-comment rule of
`.owlky/conventions.md` yields here). A pointer group left `nil` is the same as an empty one.

```go
type AppOptions struct {
	// Logger receives every framework log line. Default: log.NewLog(nil).
	Logger log.LogAdapter
	// StartupTimeout bounds Create: module factories, Imports and OnInit hooks. Default: 30s.
	StartupTimeout time.Duration
	// IsAccessLog writes one access line per request and WebSocket message. Default: false.
	IsAccessLog bool
	// IsDevtool builds the devtool snapshot at Create. Default: false.
	IsDevtool bool
	// Server applies when Owlky opens the listener (Listen, Serve). Default: port 3000, HTTP/1.1, no TLS.
	Server *ServerOptions
	// HTTP holds the settings of every HTTP request, also when App runs in your own http.Server.
	HTTP *HTTPOptions
	// Render turns on server-rendered pages when FS is set. Default: off.
	Render *RenderOptions
	// WS turns on the WebSocket endpoint when Path is set. Default: off.
	WS *WSOptions
	// Versioning turns on route versioning when Type is set. Default: off.
	Versioning *VersioningOptions
	// Proxy trusts the forwarding headers of your load balancer. Default: off (the peer is the client).
	Proxy *ProxyOptions
	// Hosts lists the host names the app answers; others get 421. Default: empty, every host.
	Hosts []string
	// Origins lists the browser origins allowed by CORS, CSRF and the WebSocket handshake. Default: empty, same origin only.
	Origins []string
	// LoadShed refuses requests with 503 under overload. Default: off.
	LoadShed *LoadShedOptions
	// HTTPSRedirect answers plain HTTP requests with 308 to HTTPS. Default: off.
	HTTPSRedirect *HTTPSRedirectOptions
	// Headers sends security headers (CSP, HSTS, frame and referrer policies). Default: off.
	Headers *HeadersOptions
	// CORS answers cross-origin browser requests from Origins. Default: off.
	CORS *CORSOptions
	// CSRF refuses cross-site POST, PUT, PATCH and DELETE requests not from Origins. Default: off.
	CSRF *CSRFOptions
	// Compress gzips compressible responses. Default: off.
	Compress *CompressOptions

	env *envRecord // set by OptionsFromEnv only
}

type ServerOptions struct {
	// Port is the TCP port Listen opens. Default: PORT when OptionsFromEnv read it, else 3000.
	Port int
	// ShutdownTimeout bounds draining requests and OnShutdown hooks after a signal or Stop. Default: 30s.
	ShutdownTimeout time.Duration
	// TLS serves HTTPS with HTTP/2 when it holds a certificate. Default: no TLS.
	TLS *TLSOptions
	// IsH2C serves HTTP/2 without TLS (behind a proxy that speaks h2c). Default: false.
	IsH2C bool
}

type TLSOptions struct {
	// CertFile is the PEM certificate chain, loaded at New. Default: none.
	CertFile string
	// KeyFile is the PEM private key of CertFile, loaded at New. Default: none.
	KeyFile string
	// Config is the base TLS configuration (code only); CertFile and KeyFile are added to it. Default: TLS 1.2 minimum.
	Config *tls.Config
}

type HTTPOptions struct {
	// MaxBodyBytes caps every request body; a larger body fails to read with 413. Negative: no limit. Default: 10MiB.
	MaxBodyBytes ByteSize
	// ServeDirs lists the directories a SERVE_ handler may return. Default: empty (enforced by roadmap 0012).
	ServeDirs []string
}

type ProxyOptions struct {
	// TrustedProxies lists the IPs and CIDRs of your proxies; turns the stage on. Default: empty.
	TrustedProxies []string
	// TrustedHops counts the proxies in front of the app; turns the stage on. Default: 0.
	TrustedHops int
	// Header picks X-Forwarded-* or Forwarded. Default: ProxyHeaderXForwarded.
	Header ProxyHeader
	// IsHostForwarded takes the client's host from the forwarding header. Default: false.
	IsHostForwarded bool
}

type ProxyHeader int // ProxyHeaderXForwarded ("x-forwarded"), ProxyHeaderForwarded ("forwarded")

type LoadShedOptions struct {
	// IsEnabled turns load shedding on. Default: false.
	IsEnabled bool
	// MaxInFlight caps concurrent requests; 0 adapts the cap to the machine. Default: 0.
	MaxInFlight int
	// MaxQueue is how many requests wait up to 100ms for a slot; negative: none wait. Default: 64.
	MaxQueue int
}

type HTTPSRedirectOptions struct {
	// IsEnabled turns the redirect on. Default: false.
	IsEnabled bool
	// Host is the redirect's host name. Default: the request's host.
	Host string
	// Port is the HTTPS port in the Location; 443 is left out. Default: 443.
	Port int
}

type HeadersOptions struct {
	// IsEnabled sends the security headers. Default: false.
	IsEnabled bool
	// IsDisabled turns the headers off on purpose and silences the SecurityCheck warning. Default: false.
	IsDisabled bool
	// ContentSecurityPolicy, "-" to omit. Default: default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests.
	ContentSecurityPolicy string
	// CrossOriginEmbedderPolicy, "-" to omit. Default: omitted.
	CrossOriginEmbedderPolicy string
	// CrossOriginOpenerPolicy, "-" to omit. Default: same-origin.
	CrossOriginOpenerPolicy string
	// CrossOriginResourcePolicy, "-" to omit. Default: same-origin.
	CrossOriginResourcePolicy string
	// FrameOptions is X-Frame-Options, "-" to omit. Default: DENY.
	FrameOptions string
	// PermissionsPolicy, "-" to omit. Default: every powerful feature off (camera, geolocation, microphone, payment…).
	PermissionsPolicy string
	// ReferrerPolicy, "-" to omit. Default: no-referrer.
	ReferrerPolicy string
	// StrictTransportSecurity, sent over HTTPS only, "-" to omit. Default: max-age=63072000; includeSubDomains.
	StrictTransportSecurity string
}

type CORSOptions struct {
	// IsEnabled answers CORS for the origins in AppOptions.Origins. Default: false.
	IsEnabled bool
	// AllowMethods for preflights. Default: GET, HEAD, PUT, PATCH, POST, DELETE.
	AllowMethods []string
	// AllowHeaders for preflights. Default: Content-Type, Authorization.
	AllowHeaders []string
	// ExposeHeaders readable by the page. Default: none.
	ExposeHeaders []string
	// MaxAge browsers cache a preflight. Default: 10m.
	MaxAge time.Duration
	// IsAllowCredentials lets the page send cookies and read the answer. Default: false.
	IsAllowCredentials bool
	// IsAllowPrivateNetwork answers Private Network Access preflights. Default: false.
	IsAllowPrivateNetwork bool
}

type CSRFOptions struct {
	// IsEnabled refuses cross-site unsafe requests whose Origin is not in AppOptions.Origins. Default: false.
	IsEnabled bool
	// IsDisabled turns CSRF off on purpose and silences the SecurityCheck warning. Default: false.
	IsDisabled bool
}

type CompressOptions struct {
	// IsEnabled gzips compressible responses. Default: false.
	IsEnabled bool
	// MinSize is the smallest body compressed. Default: 1024 (1KiB).
	MinSize ByteSize
}
```

`RenderOptions` (`FS`, `Dir`, `IsReload`), `WSOptions` (`Path`) and `VersioningOptions` (`Type`, `Key`,
`DefaultVersion`, `Extractor`) keep their fields; their godoc gains the default (`Dir`: `web`; `Path`:
none, setting it turns WebSocket on; `Key`: `v`). The `CORS`, `Headers`, `LoadShed` and `Compress`
defaults are today's package defaults (`AllowMethods` in today's order: GET, HEAD, PUT, PATCH, POST,
DELETE).

How the old options map:

| Built-in (before) | Field (after) | Turned on by |
|---|---|---|
| `loadshed.LoadShed{MaxInFlight, MaxQueue}` | `LoadShed{MaxInFlight, MaxQueue}` | `IsEnabled` |
| `proxy.Proxy{TrustedProxies, TrustedHops, Header, IsHostForwarded}` | `Proxy{…}` same names, `Header` typed `ProxyHeader` | `TrustedProxies` or `TrustedHops` |
| `hostcheck.HostCheck{Hosts}` | `Hosts` | a non-empty list |
| `httpsredirect.HTTPSRedirect{Host, Port}` | `HTTPSRedirect{Host, Port}` | `IsEnabled` |
| `helmet.Helmet{8 header fields}`, `helmet.Disabled` | `Headers{…}` same names; `"-"` literal, no constant | `IsEnabled` |
| `cors.CORS{AllowOrigins, …}` | `Origins` + `CORS{AllowMethods, AllowHeaders, ExposeHeaders, MaxAge, IsAllowCredentials, IsAllowPrivateNetwork}` | `IsEnabled` |
| `csrf.CSRF{TrustedOrigins}` | `Origins` + `CSRF{}` | `IsEnabled` |
| `compress.Compress{MinSize int}` | `Compress{MinSize ByteSize}` | `IsEnabled` |

Rule of thumb, used by every group: a group with a required field is on when that field is set
(`Render.FS`, `WS.Path`, `Versioning.Type`, `Proxy.TrustedProxies`/`TrustedHops`, `Hosts`); a group whose
fields are all optional has `IsEnabled`. This keeps a group non-nil and off, which `OptionsFromEnv` needs.

### `ByteSize` (`internal/core/byte_size.go`)

```go
type ByteSize int64
const (KB ByteSize = 1000; MB = 1000 * KB; GB = 1000 * MB; KiB ByteSize = 1024; MiB = 1024 * KiB; GiB = 1024 * MiB)
func (s ByteSize) String() string
func parseByteSize(text string) (ByteSize, error) // unexported: OptionsFromEnv only
```

### Removed and migration

| Before | After |
|---|---|
| `owlky.New()` | `owlky.New(nil)` or `owlky.New(&owlky.AppOptions{…})` |
| `app.SetLogger(l)` | `Logger: l` |
| `app.SetStartupTimeout(d)` | `StartupTimeout: d` |
| `app.EnableAccessLog()` / `EnableDevtool()` | `IsAccessLog: true` / `IsDevtool: true` |
| `app.EnableRender(&owlky.RenderOptions{…})` | `Render: &owlky.RenderOptions{…}` |
| `app.EnableVersioning(&owlky.VersioningOptions{…})` | `Versioning: &owlky.VersioningOptions{…}` (`Type` required) |
| `app.EnableWS(&owlky.WSOptions{Path: "ws"}, mws…)` | `WS: &owlky.WSOptions{Path: "ws"}`; built-ins from their fields; user `mws` via `BindGlobalMiddlewares` (B-W6) |
| `app.Listen(port, &owlky.ServerOptions{…})` | `Server: &owlky.ServerOptions{Port: port, …}` + `app.Listen()` |
| `app.ListenTLS(port, cert, key, opts)` | `Server.TLS: &owlky.TLSOptions{CertFile: cert, KeyFile: key}` + `app.Listen()` |
| `app.Serve(l, opts)` | `app.Serve(l)` |
| `ServerOptions.MaxRequestBodyBytes` (int64) | `HTTP.MaxBodyBytes` (`ByteSize`) |
| `ServerOptions.TLSConfig` | `Server.TLS.Config` |
| `app.BindGlobalMiddlewares(loadshed.LoadShed{}, …)` (any of the eight) | the matching `AppOptions` field (table above) |
| `proxy.ClientIP` / `Scheme` / `Host` | `owlky.ClientIP` / `ClientScheme` / `ClientHost` |
| `cors.CORS{AllowOrigins: o}` + `csrf.CSRF{TrustedOrigins: o}` | `Origins: o` |

## Behavior

### Step 1: `AppOptions`, `New`, validation

- **A1.** `New(nil)` and `New(&AppOptions{})` give the same app: default logger, `StartupTimeout` 30s, no
  access log, no devtool, no pages, no WebSocket, no versioning, no stage, `MaxBodyBytes` 10MiB, port 3000,
  `ShutdownTimeout` 30s, no TLS. Each feature behaves as its old `Enable…` / `Set…` call did when its field
  is set (`Render.FS` set ≡ `EnableRender`, `WS.Path` set ≡ `EnableWS`, `Versioning.Type` set ≡
  `EnableVersioning`, `Logger` ≡ `SetLogger`, …).
- **A2.** `New` reads `opts` once and copies what it keeps (slices, `*tls.Config` cloned): changing `opts`
  after `New` has no effect on the app.
- **A3.** Every check happens in `New`, before it returns; `Create` no longer panics for a configuration
  value (template loading stays in `Create`, with its `invalid render:` messages). Panics are
  `color.FmtRed("invalid app: <detail>")`. The details, field by field (`<v>` is the value as Go prints it):

  | Input | Detail |
  |---|---|
  | `StartupTimeout < 0` | `StartupTimeout is <v>, it must be positive` |
  | `Server.Port` outside 0-65535 | `Server.Port is <v>, it must be between 1 and 65535` |
  | `Server.ShutdownTimeout < 0` | `Server.ShutdownTimeout is <v>, it must be positive` |
  | one of `CertFile`, `KeyFile` | `Server.TLS needs both CertFile and KeyFile` |
  | the pair fails to load | `Server.TLS: load certificate: <error of tls.LoadX509KeyPair>` |
  | `TLS.Config` without files, certificates, `GetCertificate` or `GetConfigForClient` | `Server.TLS.Config has no certificate` |
  | TLS with `IsH2C` | `Server.IsH2C applies to connections without TLS` |
  | an empty `HTTP.ServeDirs` entry | `HTTP.ServeDirs has an empty entry` |
  | `Render.Dir` or `IsReload` without `FS` | `Render.<Field> is set without Render.FS` |
  | `Versioning.Key`, `DefaultVersion` or `Extractor` without `Type` | `Versioning.<Field> is set without Versioning.Type` |
  | `Versioning.Type` not one of the four constants | `Versioning.Type <v> is unknown` |
  | `VersioningCustom` without `Extractor` / `Extractor` with another type | `Versioning.Type custom needs an Extractor` / `Versioning.Extractor needs Type VersioningCustom` |
  | a non-zero field of a group whose `IsEnabled` is false (`IsDisabled` excepted) | `<Group>.<Field> is set while <Group>.IsEnabled is false` |
  | `Proxy.Header` or `IsHostForwarded` with neither `TrustedProxies` nor `TrustedHops` | `Proxy.<Field> is set without TrustedProxies or TrustedHops` |
  | `IsEnabled` and `IsDisabled` both true (`Headers`, `CSRF`) | `<Group>.IsEnabled and <Group>.IsDisabled are both set` |
  | `Origins` set, with `CORS`, `CSRF` and `WS` all off | `Origins is set, but CORS, CSRF and WS are off` |
  | `CORS.IsEnabled` with empty `Origins` | `CORS needs Origins` |
  | `"*"` not alone in `Origins` / with `CORS.IsAllowCredentials` / with `CSRF.IsEnabled` | `"*" must be the only entry of Origins` / `Origins "*" cannot be combined with CORS.IsAllowCredentials` / `Origins "*" turns CSRF off: list the origins` |
  | more than 256 `Origins`; a bad entry | `Origins has <n> entries, at most 256 are allowed`; `Origins entry <q> must be scheme://host[:port] in lower case, scheme://*.host[:port] or "*" alone` |
  | each check of the eight packages today | the same text with `invalid <pkg>: <Field>` rewritten `invalid app: <Group>.<Field>` (`invalid app: Proxy.TrustedHops is 20, it must be between 1 and 16`, `invalid app: Hosts has 300 entries, at most 256 are allowed`, `invalid app: Headers.FrameOptions has a control character`, `invalid app: Compress.MinSize is -1, it must be 0 or more`, …) |

- **A4.** `Create` called twice still panics `invalid app: Create called twice`; the
  `… must be called before Create` messages remain only for `BindGlobal…`.
- **A5.** Port 0 means the default; `PORT` is read only by `OptionsFromEnv` (E-rules), never by `New` or
  `Listen`.

### Step 2: `ByteSize`

- **B1.** Parse grammar: an optional `-`, decimal digits, then nothing or exactly one of `KB MB GB KiB MiB
  GiB` (case-sensitive, no space, no fraction). `"0"` → 0, `"1500"` → 1500, `"10KB"` → 10000, `"10KiB"` →
  10240, `"1GiB"` → 1073741824, `"-1"` → -1.
- **B2.** Refused (error text `<q> is not a size: write bytes or a number with KB, MB, GB (1000ⁿ) or KiB,
  MiB, GiB (1024ⁿ)`, plus `; a lower-case b means bits` when the unit ends in `b`): `""`, `"10b"`,
  `"10Mb"`, `"10kb"`, `"10B"`, `"1.5MB"`, `"10 MB"`, `"MB"`, `"0x10"`, a value that overflows `int64`.
- **B3.** `String()`: the largest unit of `GiB GB MiB MB KiB KB` (that order) that divides the value
  exactly, else plain digits; a `-` for negatives. `0` → `"0"`, `1000` → `"1KB"`, `1024` → `"1KiB"`,
  `1500` → `"1500"`, `10<<20` → `"10MiB"`, `1024000` → `"1000KiB"`, `-1` → `"-1"`.
- **B4.** Round trip: `parseByteSize(s.String()) == s` for every `ByteSize`.

### Step 3: Server and HTTP

- **S1.** `Listen()` opens `:<Server.Port>` (3000 when 0) and serves HTTPS when `Server.TLS` holds a
  certificate (HTTP/1.1 and HTTP/2, unless `TLS.Config.NextProtos` leaves out `h2`), HTTP/1.1 otherwise
  (`h2c` too with `IsH2C`). The `ServerListening` line and the terminal banner say `https` with TLS.
- **S2.** `Serve(l)` serves on `l` with the same TLS and protocol rules; it ignores `Port`, and logs
  `Info "ServerPortIgnored" "port" <n>` once when `Port` is non-zero.
- **S3.** `Listen` and `Serve` keep their errors for state (`owlky: Create must be called before serving`,
  `owlky: app is already serving`, `owlky: app is stopped`) and for the socket (`owlky: listen on :<p>:
  …`); the TLS errors `errTLSConfigOnListen`, `errH2CWithTLS`, `errNoCertificate` are gone (A3).
- **S4.** `ShutdownTimeout` bounds draining and hooks after a signal or `Stop()`; `Stop()` before
  serving uses it too.
- **S5.** `HTTP.MaxBodyBytes` is applied in `ServeHTTP` to every request with a body, whoever serves the
  app: an `App` mounted in the user's own `http.Server` (or called through `httptest`) with
  `MaxBodyBytes: 1 * KiB` answers 413 to a 2KiB JSON body; 0 means 10MiB; negative means no limit. The
  value is fixed at `New` (a plain `int64` field, no atomic: nothing writes it after `New`).
- **S6.** `Server` has no effect when the app is only used as an `http.Handler` (documented; nothing can
  detect it).

### Step 4: pre-route stages

- **P1.** `New` compiles the configured stages once, in this fixed order: `loadshed` → `proxy` →
  `hostcheck` → `httpsredirect` → `headers` → `cors` → `csrf` → `compress`. A stage that is off is not in
  the list. User middlewares (`BindGlobalMiddlewares`, then module and controller `BindMiddleware`) run
  after the last stage, then guards and the rest of the pipeline. The order of `AppOptions` fields or of
  `BindGlobalMiddlewares` calls never changes it.
- **P2.** Stages run in `ServeHTTP`'s request path before route matching, the async gate and user
  middlewares: a request refused by a stage gets the stage's answer, never 404, 405, 410 or the gate's
  503 (`Hosts: {"a.com"}`, `Host: b.com`, unknown path → 421).
- **P3.** Each stage keeps the behavior its package has today, with the option values of A-table
  (design 0014 and the packages' READMEs); its tests move with it. A refusal is answered like a middleware
  refusal today (the fixed early error: JSON, or HTML when `Render` is on and `Accept` has `text/html`).
- **P4.** `cors` and `csrf` read one compiled origin set built from `Origins` (`internal/middlewares/origin`:
  exact entries in a map, `scheme://*.host` wildcards, `"*"`). `csrf` passes a request whose `Origin`
  matches the set, then applies `http.CrossOriginProtection` with no trusted origin; for exact entries this
  equals today's `AddTrustedOrigin`, and wildcard entries now work for CSRF too.
- **P5.** WebSocket handshake (`WS.Path` requests): the stages that apply run in this order: `loadshed` →
  `proxy` → `hostcheck` → `origin` (step 5), then user `BindGlobalMiddlewares` (W6). `httpsredirect`,
  `headers`, `csrf` and `compress` don't run on the handshake (a 101 can't be redirected or compressed;
  `csrf` passes `GET`; open question 5). `cors` doesn't run either: `origin` replaces its WebSocket branch.
- **P6.** `middlewares/<name>` moves to `internal/middlewares/<name>` with its option struct,
  `NewMiddleware`, compiled type, `Use(r, w, next)` and `Snapshot()`; core builds each stage directly
  (no `injectDependencies`, no reflection per stage). The public `middlewares/` directory is gone.
- **P7.** Behavior changes for apps that bound the built-ins themselves (README › Behavior changes):
  a user middleware bound before `loadshed` no longer runs under overload; before `proxy` it now reads the
  resolved client (`ClientIP`), not the peer; before `helmet` its own refusals now carry the security
  headers; `compress` now also compresses what user middlewares write; gated (`AsyncImports`) routes and
  404/405/410 now pass the stages first (P2); the handshake now runs the stages its fields turn on, not a
  list given to `EnableWS`.
- **P8.** The devtool snapshot (`IsDevtool`) lists the stages in a `pre_route` array, in order, each with
  its `Snapshot()` (name and fields, counters included); `middlewares` no longer lists them.

### Step 5: WebSocket Origin check

- **W1.** With `WS.Path` set, the `origin` stage runs on every handshake; it has no switch (open
  question 6).
- **W2.** No `Origin` header (non-browser client) → passes.
- **W3.** Same origin passes: the `Origin` value, lower-cased, equals `<scheme>://<host>` where `<scheme>`
  is `https` when `ClientScheme(r)` is `https`, else `http`, and `<host>` is `ClientHost(r)` lower-cased;
  a default port (`:443` for `https`, `:80` for `http`) is dropped on both sides before comparing.
- **W4.** An `Origin` in the origin set (P4) passes (`"*"` passes every origin).
- **W5.** Anything else, `Origin: null` and an `Origin` longer than 256 bytes included, is refused with
  `403 Origin not allowed` before the upgrade; the stage's `refused` counter grows.
- **W6.** `BindGlobalMiddlewares` run on the handshake after the stages, in bind order, once per
  connection; one that does not call `next()` refuses the connection (today's `EnableWS` middleware rule).
  Controller `BindMiddleware` doesn't run on the handshake (no route).

### Step 6: `SecurityCheck`

- **C1.** At the end of `Create`, once, the app logs at most one line:
  `Warn "SecurityCheck" "missing" <list> "docs" <url>`, with `<list>` the comma-joined names of:
  - `csrf` when the app has at least one `SUBMIT_` route and neither `CSRF.IsEnabled` nor
    `CSRF.IsDisabled` is true;
  - `headers` when it has at least one `RENDER_` route and neither `Headers.IsEnabled` nor
    `Headers.IsDisabled` is true.

  `<url>` is a constant pointing at README › Configuration › Production checklist (open question 9).
  Routes of `AsyncImports` modules count (they are registered at `Create`).
- **C2.** No line when `<list>` is empty: an API-only app, or every protection on or `IsDisabled`.
- **C3.** Nothing else changes: no stage is turned on, no request is refused.

### Step 7: trace

- **T1.** `trace.StagePreRoute = "pre-route"`. Each stage that runs on a traced request emits one event:
  `Stage` `pre-route`, `Name` the stage (`loadshed`, `proxy`, `hostcheck`, `httpsredirect`, `headers`,
  `cors`, `csrf`, `compress`, `origin`), `Transport` `HTTP` (the handshake included), `Duration` the time
  of its `Use` call (for `loadshed`, queue wait included), `Code` the status it answered with when it ended
  the request without `next()` (a refusal's code: 503, 421, 400, 403; 308 for `httpsredirect`; 204 for a
  CORS preflight), else 0.
- **T2.** A request ended by a stage emits no later stage, middleware, guard or handler event: its trace
  ends at that stage, then `complete`.
- **T3.** `StageMiddleware` is emitted for user middlewares only (HTTP and handshake).
- **T4.** No listener → no event and no clock read (as today); a stage that is off emits nothing.
- **T5.** The access log groups stage events by `Stage` as it does today, so a line carries a
  `pre-route` attribute (`headers 23.05µs (7%), csrf 422ns (0%)`) before `middleware`; the pretty log shows
  it as a `pre-route` group. Percentages and `other` stay as they are (backlog › Accurate trace timings).

### Step 8: `OptionsFromEnv`

- **E1.** `OptionsFromEnv()` returns an `*AppOptions` with every pointer group non-nil (`Server.TLS`
  included), each field set from its variable or left zero. Without a call to it, Owlky reads no variable
  and no file.
- **E2.** Names: `OK_` + the field path from `AppOptions`, levels joined by `__`, each field name in upper
  snake case (a `_` before an upper-case letter that follows a lower-case letter, and before the last
  upper-case letter of a run followed by a lower-case one; digits never split): `HTTP.MaxBodyBytes` →
  `OK_HTTP__MAX_BODY_BYTES`, `Server.TLS.CertFile` → `OK_SERVER__TLS__CERT_FILE`, `Server.IsH2C` →
  `OK_SERVER__IS_H2C`, `HTTPSRedirect.Port` → `OK_HTTPS_REDIRECT__PORT`, `LoadShed.MaxInFlight` →
  `OK_LOAD_SHED__MAX_IN_FLIGHT`, `Origins` → `OK_ORIGINS`. Names are generated from the struct by
  reflection at the call; a test checks the full list (table below) and that no two fields share a name.
- **E3.** Code-only fields have no variable: `Logger`, `Render.FS`, `Server.TLS.Config`,
  `Versioning.Extractor`. Setting `OK_LOGGER`, `OK_RENDER__FS`, `OK_SERVER__TLS__CONFIG` or
  `OK_VERSIONING__EXTRACTOR` is an error: `<VAR>: <Path> is set in code only`.
- **E4.** `PORT` fills `Server.Port` when `OK_SERVER__PORT` is not set in either source; both set →
  `OK_SERVER__PORT` wins, no error. No other name without `OK_` is read (`HTTP_PROXY` and friends never).
- **E5.** Types come from the field: `string` as is; `int` by `strconv.Atoi`; `bool` only `true` or
  `false`; `time.Duration` by `time.ParseDuration` and must contain a unit (`"15"` and `"0"` refused,
  `"0s"` accepted); `ByteSize` by B1; enums by their lower-case names (`ProxyHeader`: `x-forwarded`,
  `forwarded`; `VersioningType`: `query`, `header`, `media-type`, `custom`); `[]string` split on `,`, each
  element trimmed of spaces and tabs. A future slice of another element type parses each element as that
  type.
- **E6.** Errors (each makes `New` panic, E8): a value that doesn't convert
  (`OK_HTTP__MAX_BODY_BYTES (.env:3): "10Mb" is not a size: …`); one bad or empty element of a list, which
  refuses the whole variable (`OK_ORIGINS (environment): element 2 of 3 is empty`); an empty value
  (`OK_ORIGINS (environment): empty value; remove the variable to unset it`); an unknown `OK_*` name, with
  the closest known name when its Levenshtein distance is at most 3 (`OK_HOST (environment): unknown
  variable; did you mean OK_HOSTS?`), else without the hint. A value of the right type but wrong meaning (a
  bad CIDR, an origin without a scheme, a missing TLS file) fails A3's check in `New`, which names the
  field and, for a value from a variable, appends ` (from <VAR>, <source>)`.
- **E7.** Sources: the process environment, then `./.env` in the working directory (skipped when missing,
  read with the shared parser, no variable expansion). A name in both: the environment wins. Only `OK_*`
  and `PORT` keys of `.env` are considered; others are left to the app and `modules/config`. Code written
  after `OptionsFromEnv()` wins by statement order, explicit `false`, `0` and `""` included.
- **E8.** `OptionsFromEnv` never panics; it records every error, the source of each value it set and the
  value it set in the unexported `env` field. `New` panics once with every recorded error, one per line:
  `invalid app: <n> environment errors:\n  <error>\n  <error>`. A `.env` syntax error is one of them, in
  the parser's words (`.env:4: …`, never the value).
- **E9.** When `opts.env` is set, `New` logs one `Info "AppOption"` line per field that is non-zero after
  the code ran: `"option" <Path> "value" <v> "source" <src>`, `<src>` being `<VAR>` (environment),
  `.env:<line>`, `PORT`, or `code` when the field differs from what `OptionsFromEnv` set. Values print as
  in A3 (`ByteSize` via `String()`, durations via `String()`, lists comma-joined); code-only fields print
  `set`. A field in the internal `secretFields` set prints `***` in logs and errors; the set is empty
  today (no field holds secret material: `KeyFile` is a path). No line without `OptionsFromEnv`.
- **E10.** `modules/config` behaves exactly as before: same `.env` syntax, expansion, errors (`invalid
  config: <path>:<line>: …`), precedence and binding; only its parser now lives in `internal/dotenv`.

Variables (the generated list, E2):

```text
OK_STARTUP_TIMEOUT  OK_IS_ACCESS_LOG  OK_IS_DEVTOOL  OK_HOSTS  OK_ORIGINS
OK_SERVER__PORT (and PORT)  OK_SERVER__SHUTDOWN_TIMEOUT  OK_SERVER__IS_H2C
OK_SERVER__TLS__CERT_FILE  OK_SERVER__TLS__KEY_FILE
OK_HTTP__MAX_BODY_BYTES  OK_HTTP__SERVE_DIRS
OK_RENDER__DIR  OK_RENDER__IS_RELOAD  OK_WS__PATH
OK_VERSIONING__TYPE  OK_VERSIONING__KEY  OK_VERSIONING__DEFAULT_VERSION
OK_PROXY__TRUSTED_PROXIES  OK_PROXY__TRUSTED_HOPS  OK_PROXY__HEADER  OK_PROXY__IS_HOST_FORWARDED
OK_LOAD_SHED__IS_ENABLED  OK_LOAD_SHED__MAX_IN_FLIGHT  OK_LOAD_SHED__MAX_QUEUE
OK_HTTPS_REDIRECT__IS_ENABLED  OK_HTTPS_REDIRECT__HOST  OK_HTTPS_REDIRECT__PORT
OK_HEADERS__IS_ENABLED  OK_HEADERS__IS_DISABLED  OK_HEADERS__CONTENT_SECURITY_POLICY
OK_HEADERS__CROSS_ORIGIN_EMBEDDER_POLICY  OK_HEADERS__CROSS_ORIGIN_OPENER_POLICY
OK_HEADERS__CROSS_ORIGIN_RESOURCE_POLICY  OK_HEADERS__FRAME_OPTIONS  OK_HEADERS__PERMISSIONS_POLICY
OK_HEADERS__REFERRER_POLICY  OK_HEADERS__STRICT_TRANSPORT_SECURITY
OK_CORS__IS_ENABLED  OK_CORS__ALLOW_METHODS  OK_CORS__ALLOW_HEADERS  OK_CORS__EXPOSE_HEADERS
OK_CORS__MAX_AGE  OK_CORS__IS_ALLOW_CREDENTIALS  OK_CORS__IS_ALLOW_PRIVATE_NETWORK
OK_CSRF__IS_ENABLED  OK_CSRF__IS_DISABLED  OK_COMPRESS__IS_ENABLED  OK_COMPRESS__MIN_SIZE
```

`internal/dotenv` holds today's `modules/config/dotenv.go` unchanged in behavior, exporting
`Load(path string, isExpand bool) (map[string]any, error)` for the module and `Parse(path string, data
[]byte) (*File, error)` with `File.Keys()` and `File.Value(key) (value string, line int, ok bool)` for
`OptionsFromEnv`. Its tests move with it. Typed binding: `modules/config` keeps `ctx.BindStrArr`
(lenient, per-field `FieldLevel` errors); `OptionsFromEnv` uses its own strict converter
(`internal/core/options_env.go`), because `BindStrArr` has no `ByteSize`, enum or "whole list refused"
rule and reports errors instead of stopping (open question 11).

### Roadmap 0012 step 1c(a): `HTTP.ServeDirs`

- **D1.** Declaration only: `HTTP.ServeDirs []string` exists, is copied at `New`, refuses an empty entry
  (A3), binds from `OK_HTTP__SERVE_DIRS`, and changes nothing else. Roadmap 0012 decides what an empty
  list means and enforces the roots, with its tests.

## Performance

Cold path: `New` (validation, compiling stages, a TLS pair load), `OptionsFromEnv` (one `os.Environ`, one
`.env` read capped at 1MiB by the parser, reflection over ~55 fields), the `AppOption` and `SecurityCheck`
lines. No measurable change to `Create` (baseline: 64-275 µs).

Hot path:
- **No stage configured:** one length check of the stage slice before matching; `ServeHTTP`'s body limit
  becomes a plain field load instead of an atomic load. Expected: within noise of the baseline
  (`Static` 1.21 µs / 8 allocs, `Chain` 3.28 µs / 24 allocs, `NotFound` 0.98 µs / 5 allocs); zero added
  allocations.
- **Stages configured:** each stage costs what the same middleware costs today bound globally, minus the
  reflective construction (done once anyway). Stages and user middlewares share one `next` closure per
  request: at most the one allocation today's global middlewares already pay, none added. A handshake pays
  the same as today's `EnableWS` list.
- **Idle:** the compiled stages only (as today when bound), plus nothing when `AppOptions` is empty; `New`
  keeps no reference to `opts`. `OptionsFromEnv`'s `env` record lives as long as the caller's `opts`.
- No heavy technique is introduced. The check-only stages keep the middleware shape; a flat check loop
  is allowed later only with a benchmark (roadmap decision).

Benchmarks: `BenchmarkPreRoute_None` and `BenchmarkPreRoute_All` (every stage on, a request that passes)
in `internal/core`, compared with `benchstat` (`-count 5`) against the same stages bound through
`BindGlobalMiddlewares` before step 4; `bench/` scenarios and the cold-path table rerun in step 11.

## Concurrency

`AppOptions` is read only inside `New` and `OptionsFromEnv` on the caller's goroutine. The compiled stages
are immutable after `New` except their counters and `loadshed`'s state, which stay `atomic` / mutex-guarded
as today. `maxBodyBytes`, the stage slice and the origin set are written in `New` and read-only after,
published to serving goroutines by `Listen`/`Serve` (goroutine start) or by the user handing the `App` to
an `http.Server` after `New` returns. No goroutine is added.

## Security

- Every stage is off by default (decided); `SecurityCheck` points at what a page app lacks.
- WebSocket handshakes from another site are refused unless listed (W5): closes cross-site WebSocket
  hijacking for cookie-authenticated apps, which today relies on the user passing `cors` to `EnableWS`.
- Fixed order removes the order bugs P7 lists (security headers skipped, peer IP taken for the client,
  work done under overload).
- Environment: only `OK_*` and `PORT` are read (no `HTTP_*`: httpoxy); unknown `OK_*` stops the app; an
  empty value stops it; a value is never logged or printed when its field is secret (E9), and `.env`
  parser errors never contain values (unchanged). `.env` is read from the working directory only.
- `Origins` validation keeps CORS's strict form (lower case, scheme and host, no path); `"*"` can't be
  combined with credentials or CSRF.
- TLS files are loaded at `New`, so a bad path fails at start, not at the first handshake.

## Impact

- Every caller of `New()`, the removed methods and the eight packages (about 370 `New()` and 600
  configuration calls, mostly tests; `examples/`, `bench/`, READMEs): roadmap step 9.
- `guards/throttler` reads `tracecontext.ClientIP` directly (public packages don't import the root).
- Docs (step 10): README Configuration section and production checklist (the `SecurityCheck` link target),
  behavior changes of P7, `.owlky/request-pipeline.md` (the pre-route stages before the pipeline),
  CLAUDE.md (package map without `middlewares/*`; "Safe defaults" → "Explicit protection"; the
  `invalid app:` list; App line), the moved packages' READMEs (become internal docs), `internal/trace` and
  `internal/core` READMEs (`pre-route`, `ClientIP` naming).
- `api/owlky.txt`:
  - removed: the eight `pkg owlky/middlewares/*` blocks (57 lines), `func New() owlky.App`,
    `EnableAccessLog`, `EnableDevtool`, `EnableRender`, `EnableVersioning`, `EnableWS`, `ListenTLS`,
    `SetLogger`, `SetStartupTimeout`, `Listen(int, *core.ServerOptions)`, `Serve(net.Listener,
    *core.ServerOptions)`, `ServerOptions` fields `MaxRequestBodyBytes`, `TLSConfig`;
  - added: `func New(*core.AppOptions) owlky.App`, `func OptionsFromEnv() *core.AppOptions`, `ClientIP`,
    `ClientScheme`, `ClientHost`, 12 type aliases, 2 `ProxyHeader` and 6 `ByteSize` constants,
    `Listen() error`, `Serve(net.Listener) error`, `method (core.ByteSize) String() string`, `reach` lines
    for every field above, `ServerOptions` fields `Port`, `TLS`.

## Test plan

Internal test packages, `Test<Subject>_<Behavior>`, one function per behavior.

| Step | Rules | Tests (cases) |
|---|---|---|
| 1 | A1 | `TestNew_NilEqualsEmpty`, `TestNew_DefaultsMatchOldApp` (logger, 30s, 10MiB, no stage); one `TestNew_<Field>TurnsOn<Feature>` per feature (Render, WS, Versioning, Logger, StartupTimeout, IsAccessLog, IsDevtool) asserting the old `Enable…` behavior |
| 1 | A2 | `TestNew_CopiesOptions` (mutate `Origins`, `Hosts`, `TLS.Config` after `New`) |
| 1 | A3 | table test `TestNew_InvalidOptionsPanic`: one row per A3 line with the exact message; boundaries (Port 0, 1, 65535, 65536, -1; TrustedHops 0, 1, 16, 17; 256 and 257 origins and hosts) |
| 1 | A4, A5 | `TestCreate_TwiceStillPanics`, `TestNew_IgnoresPORT` (PORT set, `New(nil)` listens on 3000) |
| 2 | B1-B4 | `TestByteSize_Parse` (each B1 case), `TestByteSize_ParseRefuses` (each B2 case, bits hint), `TestByteSize_String` (each B3 case), `TestByteSize_RoundTrip` (fuzz `FuzzByteSize_RoundTrip`) |
| 3 | S1-S4 | `TestListen_DefaultPort3000`, `TestListen_ServesTLSFromOptions` (temp self-signed pair, HTTP/2 negotiated), `TestListen_H2C`, `TestServe_IgnoresPortAndLogs`, `TestServe_TLS`, `TestListen_StateErrors`, `TestStop_UsesShutdownTimeout` |
| 3 | S5 | `TestServeHTTP_MaxBodyBytesInOwnServer` (`httptest.NewServer(app)`, 1KiB limit, 2KiB body → 413; 1KiB body passes), `TestServeHTTP_MaxBodyBytesDefault` (10MiB+1 → 413), `TestServeHTTP_MaxBodyBytesNegativeNoLimit` |
| 4 | P1, P2 | `TestPreRoute_FixedOrder` (all stages on, a user middleware bound first records what it sees: resolved IP, headers present, gzip writer), `TestPreRoute_NoneConfiguredNoStage`, `TestPreRoute_RefusalBeats404`, `…Beats405`, `…BeatsGate503` |
| 4 | P3 | the moved packages' existing tests, unchanged except imports; `TestPreRoute_<Stage>FromOptions` per stage (one pass, one refusal) |
| 4 | P4 | `TestOrigins_SharedByCORSAndCSRF`, `TestCSRF_WildcardOrigin`, `TestCSRF_ExactOriginEqualsTrustedOrigin` (old vs new on the same request set) |
| 4 | P5 | `TestHandshake_StagesRun` (loadshed full → 503, untrusted host → 421), `TestHandshake_SkipsHeadersCSRFCompressRedirect` |
| 4 | P8 | `TestDevtool_PreRouteSnapshot` |
| 5 | W1-W5 | `TestWSOrigin_NoOriginPasses`, `…SameOriginPasses` (default port both sides, upper case, behind `Proxy` with `IsHostForwarded`), `…ListedPasses`, `…WildcardPasses`, `…StarPasses`, `…OtherSiteRefused403`, `…NullRefused`, `…TooLongRefused`, `…SchemeMismatchRefused` (`http://` origin on an HTTPS request) |
| 5 | W6 | `TestHandshake_GlobalMiddlewaresRunAfterStages`, `TestHandshake_MiddlewareWithoutNextRefuses` |
| 6 | C1-C3 | `TestSecurityCheck_SubmitWithoutCSRF`, `…RenderWithoutHeaders`, `…BothInOneLine`, `…SilencedByIsDisabled` (each), `…QuietWhenEnabled`, `…QuietForAPIOnly`, `…AsyncModuleRoutesCount`, `…ChangesNoResponse` |
| 7 | T1-T5 | `TestTrace_PreRouteEvents` (order, names, codes 0), `TestTrace_RefusedRequestEndsAtStage` (each refusing stage: code 503 / 421 / 400 / 403 / 308 / 204), `TestTrace_LoadShedDurationIncludesWait`, `TestTrace_UserMiddlewareStillMiddleware`, `TestTrace_NoListenerNoEvent`, `TestAccessLog_PreRouteGroup` (JSON key and pretty group) |
| 8 | E1, E2 | `TestOptionsFromEnv_AllGroupsNonNil`, `TestOptionsFromEnv_Names` (golden list above, uniqueness) |
| 8 | E3-E5 | `TestOptionsFromEnv_CodeOnlyFields`, `…PORTFallback`, `…OKServerPortBeatsPORT`, `…IgnoresHTTPProxy`, one `…Parses<Type>` per type (string, int, bool, Duration, ByteSize, enums, list with spaces) |
| 8 | E6, E8 | `TestOptionsFromEnv_BadValue`, `…BadElementRefusesList` (`1,a,2`), `…EmptyElement`, `…EmptyValue`, `…UnknownWithHint` (`OK_HOST`), `…UnknownWithoutHint`, `…AllErrorsInOnePanic`, `…DotEnvSyntaxErrorNoValue`, `…MeaningErrorNamesVariable` (bad CIDR from `OK_PROXY__TRUSTED_PROXIES`) |
| 8 | E7 | `TestOptionsFromEnv_EnvBeatsDotEnv`, `…DotEnvMissingSkipped`, `…DotEnvOtherKeysIgnored`, `…CodeWinsAfter` (explicit `false`, `0`, `""`) |
| 8 | E9 | `TestOptionsFromEnv_StartupLogSources` (env, `.env:3`, `PORT`, `code`), `…SecretFieldMasked` (with a test-only secret field set), `…NoLogWithoutEnv` |
| 8 | E10 | `modules/config`'s existing tests pass unchanged; `internal/dotenv` keeps the moved parser tests |
| 8 | D1 | `TestNew_ServeDirsDeclared` (copied, empty entry panics, bound from env) |
| — | perf | `BenchmarkPreRoute_None`, `BenchmarkPreRoute_All`, `BenchmarkByteSize_String` |

## Risks

- **Mass migration (step 9):** ~1000 call sites; a mechanical rewrite can silently drop a stage (a test
  that bound `helmet` but now forgets `Headers.IsEnabled`). Mitigation: the builder greps every removed
  name to zero, and the moved packages' own tests cover each stage.
- **Stages before matching (P2)** changes answers for refused requests on unknown paths and gated routes
  (421 instead of 404 or 503); intended, listed in P7.
- **`.env` location:** `OptionsFromEnv` reads the working directory (decided), `modules/config` the
  nearest `go.mod` directory; they are the same file under `go run .` from the module root, and differ
  when a binary runs elsewhere with a `go.mod` above it. Documented.
- **Global middlewares on the handshake (W6)** may surprise a middleware that assumes a normal response
  (open question 4).
- **TLS at `New`** reads key files during construction, so tests that build an app with TLS need real
  files (a test helper writes a self-signed pair).

## Open questions

1. **Home of the client helpers.** Recommendation: root `owlky.ClientIP`, `ClientScheme`, `ClientHost`
   (`owlky.Host` / `owlky.Scheme` read as properties of Owlky). Alternative: methods on `HTTPContext`,
   which user middlewares (they get `*http.Request`) can't use.
2. **`IsAccessLog` / `IsDevtool`** (D-032 `Is…` booleans) instead of the roadmap's working names
   `AccessLog` / `Devtool`. Recommendation: `Is…`; groups later if they grow options.
3. **Explicit off** as `IsDisabled` on `Headers` and `CSRF` only. Recommendation: keep; an enum
   (`StageUnset/On/Off`) on every group costs more API for two warnings.
4. **User middlewares on the handshake:** `BindGlobalMiddlewares` also run there (W6, replacing
   `EnableWS`'s list) vs a new `BindWSMiddlewares`. Recommendation: W6, one way to bind an app-wide
   middleware.
5. **`httpsredirect` on the handshake:** not run (P5). Recommendation: keep for this roadmap; roadmap 0012
   decides whether a plain `ws://` handshake is refused when `HTTPSRedirect` is on.
6. **No switch for the WebSocket Origin check (W1).** Recommendation: none; an app that needs other sites
   lists them, or `"*"`.
7. **`Origins` with no consumer panics (A3).** Recommendation: keep (strict, like an unknown `OK_*`).
8. **`ByteSize` methods:** only `String()`. Recommendation: keep; add `MarshalText`/`UnmarshalText` when
   a user needs JSON or `flag`.
9. **`SecurityCheck` link:** the README anchor on the repository until the website exists.
   Recommendation: a single unexported constant, switched to the website in its roadmap.
10. **Startup option log level:** `Info` per non-zero field (E9). Recommendation: `Info`; ~5-15 lines on a
    typical app, only with `OptionsFromEnv`.
11. **Typed binding not shared:** the backlog said "reuse the parser and typed binding"; only the parser is
    shared, because the module's lenient binding can't give E5-E6's strict errors. Recommendation: accept;
    the module's binding may move to the strict converter later, which would change its behavior.
