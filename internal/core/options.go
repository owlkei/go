package core

import (
	"crypto/tls"
	"fmt"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"time"

	"go.owlkei.com/owlkei/internal/color"
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/inspect"
	"go.owlkei.com/owlkei/internal/middlewares/compress"
	"go.owlkei.com/owlkei/internal/middlewares/cors"
	"go.owlkei.com/owlkei/internal/middlewares/csrf"
	"go.owlkei.com/owlkei/internal/middlewares/helmet"
	"go.owlkei.com/owlkei/internal/middlewares/hostcheck"
	"go.owlkei.com/owlkei/internal/middlewares/httpsredirect"
	"go.owlkei.com/owlkei/internal/middlewares/loadshed"
	"go.owlkei.com/owlkei/internal/middlewares/origin"
	"go.owlkei.com/owlkei/internal/middlewares/proxy"
	"go.owlkei.com/owlkei/internal/render"
	"go.owlkei.com/owlkei/internal/versioning"
	"go.owlkei.com/owlkei/log"
)

const (
	defaultPort         = 3000
	defaultMaxBodyBytes = 10 * MiB
	maxPort             = 65535
)

type AppOptions struct {
	// Logger receives every framework log line. Default: log.NewLog(nil).
	Logger log.LogAdapter
	// StartupTimeout bounds Create: module factories, Imports and OnInit hooks. Default: 30s.
	StartupTimeout time.Duration
	// IsAccessLog writes one access line per request and WebSocket message. Default: false.
	IsAccessLog bool
	// IsDevtool builds the devtool snapshot at Create. Default: false.
	IsDevtool bool
	// Server applies when Owlkei opens the listener (Listen, Serve). Default: port 3000, HTTP/1.1, no TLS.
	Server *ServerOptions
	// HTTP holds the settings of every HTTP request, also when App runs in your own http.Server.
	HTTP *HTTPOptions
	// Render turns on server-rendered pages when FS is set. Default: off.
	Render *RenderOptions
	// WS turns on the WebSocket endpoint when Path is set. Default: off.
	WS *WSOptions
	// Versioning turns on route versioning when Type is set. Default: off.
	Versioning *versioning.Options
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

	env *envRecord
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
	// ServeDirs lists the directories SERVE_ handlers may return. A SERVE_ route needs at least one. Default: empty.
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

type ProxyHeader int

const (
	ProxyHeaderXForwarded ProxyHeader = ProxyHeader(proxy.HeaderXForwarded)
	ProxyHeaderForwarded  ProxyHeader = ProxyHeader(proxy.HeaderForwarded)
)

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

type preRouteStage interface {
	common.MiddlewareFn
	inspect.Snapshotter
}

type namedStage struct {
	name  string
	stage preRouteStage
}

type appConfig struct {
	logger          log.Logger
	startupTimeout  time.Duration
	isAccessLog     bool
	isDevtool       bool
	port            int
	shutdownTimeout time.Duration
	tlsConfig       *tls.Config
	isH2C           bool
	maxBodyBytes    int64
	serveDirs       []string
	workDir         string
	render          *render.Engine
	renderDir       string
	ws              *WSOptions
	versioning      *versioning.Options
	stages          []namedStage
	wsStages        []namedStage
	isCSRFSet       bool
	isHeadersSet    bool
}

type optionError struct {
	path   string
	detail string
}

func (e *optionError) Error() string {
	return e.detail
}

func failOption(path, format string, args ...any) {
	panic(&optionError{path: path, detail: fmt.Sprintf(format, args...)})
}

func groupError(group string, err error) {
	text := err.Error()
	field, _, _ := strings.Cut(text, " ")
	if group == "" {
		panic(&optionError{path: field, detail: text})
	}
	if text != "" && text[0] >= 'A' && text[0] <= 'Z' {
		panic(&optionError{path: group + "." + field, detail: group + "." + text})
	}
	panic(&optionError{path: group, detail: group + ": " + text})
}

func loadAppOptions(opts *AppOptions) (cfg *appConfig) {
	if opts == nil {
		opts = &AppOptions{}
	}
	if env := opts.env; env != nil && len(env.errors) > 0 {
		noun := "errors"
		if len(env.errors) == 1 {
			noun = "error"
		}
		panic(color.FmtRed("invalid app: %d environment %s:\n  %s", len(env.errors), noun, strings.Join(env.errors, "\n  ")))
	}
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		e, ok := rec.(*optionError)
		if !ok {
			panic(rec)
		}
		panic(color.FmtRed("invalid app: " + e.detail + opts.env.suffix(opts, e.path)))
	}()

	cfg = &appConfig{
		logger:          log.NewLog(nil),
		startupTimeout:  defaultStartupTimeout,
		isAccessLog:     opts.IsAccessLog,
		isDevtool:       opts.IsDevtool,
		port:            defaultPort,
		shutdownTimeout: defaultShutdownTimeout,
		maxBodyBytes:    int64(defaultMaxBodyBytes),
	}
	if opts.Logger != nil {
		cfg.logger = log.WrapLogger(opts.Logger, nil)
	}
	switch {
	case opts.StartupTimeout < 0:
		failOption("StartupTimeout", "StartupTimeout is %v, it must be positive", opts.StartupTimeout)
	case opts.StartupTimeout > 0:
		cfg.startupTimeout = opts.StartupTimeout
	}
	cfg.loadServer(opts.Server)
	cfg.loadHTTP(opts.HTTP)
	cfg.loadRender(opts.Render)
	if opts.WS != nil && opts.WS.Path != "" {
		cfg.ws = &WSOptions{Path: opts.WS.Path}
	}
	cfg.loadVersioning(opts.Versioning)
	cfg.loadStages(opts)

	return cfg
}

func (cfg *appConfig) loadServer(server *ServerOptions) {
	if server == nil {
		return
	}
	if server.Port < 0 || server.Port > maxPort {
		failOption("Server.Port", "Server.Port is %d, it must be between 1 and %d", server.Port, maxPort)
	}
	if server.Port != 0 {
		cfg.port = server.Port
	}
	switch {
	case server.ShutdownTimeout < 0:
		failOption("Server.ShutdownTimeout", "Server.ShutdownTimeout is %v, it must be positive", server.ShutdownTimeout)
	case server.ShutdownTimeout > 0:
		cfg.shutdownTimeout = server.ShutdownTimeout
	}
	cfg.isH2C = server.IsH2C
	cfg.tlsConfig = loadTLS(server.TLS)
	if cfg.tlsConfig != nil && cfg.isH2C {
		failOption("Server.IsH2C", "Server.IsH2C applies to connections without TLS")
	}
}

func loadTLS(t *TLSOptions) *tls.Config {
	if t == nil {
		return nil
	}
	hasFiles := t.CertFile != "" || t.KeyFile != ""
	if !hasFiles && t.Config == nil {
		return nil
	}
	if hasFiles && (t.CertFile == "" || t.KeyFile == "") {
		failOption("Server.TLS", "Server.TLS needs both CertFile and KeyFile")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if t.Config != nil {
		cfg = t.Config.Clone()
		cfg.Certificates = slices.Clone(cfg.Certificates)
	}
	if hasFiles {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			failOption("Server.TLS", "Server.TLS: load certificate: %v", err)
		}
		cfg.Certificates = append(cfg.Certificates, cert)
		return cfg
	}
	if len(cfg.Certificates) == 0 && cfg.GetCertificate == nil && cfg.GetConfigForClient == nil {
		failOption("Server.TLS.Config", "Server.TLS.Config has no certificate")
	}
	return cfg
}

func (cfg *appConfig) loadHTTP(h *HTTPOptions) {
	if h == nil {
		return
	}
	switch {
	case h.MaxBodyBytes < 0:
		cfg.maxBodyBytes = -1
	case h.MaxBodyBytes > 0:
		cfg.maxBodyBytes = int64(h.MaxBodyBytes)
	}
	cfg.serveDirs, cfg.workDir = loadServeDirs(h.ServeDirs)
}

func (cfg *appConfig) loadRender(r *RenderOptions) {
	if r == nil {
		return
	}
	if r.FS == nil {
		switch {
		case r.Dir != "":
			failOption("Render.Dir", "Render.Dir is set without Render.FS")
		case r.IsReload:
			failOption("Render.IsReload", "Render.IsReload is set without Render.FS")
		}
		return
	}
	var fsys fs.FS
	fsys, cfg.renderDir = renderFS(r)
	cfg.render = render.New(fsys, r.IsReload)
	cfg.render.SetNamePolicy(isServableName)
}

func (cfg *appConfig) loadVersioning(v *versioning.Options) {
	if v == nil {
		return
	}
	if v.Type == 0 {
		switch {
		case v.Key != "":
			failOption("Versioning.Key", "Versioning.Key is set without Versioning.Type")
		case v.DefaultVersion != "":
			failOption("Versioning.DefaultVersion", "Versioning.DefaultVersion is set without Versioning.Type")
		case v.Extractor != nil:
			failOption("Versioning.Extractor", "Versioning.Extractor is set without Versioning.Type")
		}
		return
	}
	switch v.Type {
	case versioning.TypeQuery, versioning.TypeHeader, versioning.TypeMediaType:
		if v.Extractor != nil {
			failOption("Versioning.Extractor", "Versioning.Extractor needs Type VersioningCustom")
		}
	case versioning.TypeCustom:
		if v.Extractor == nil {
			failOption("Versioning.Type", "Versioning.Type custom needs an Extractor")
		}
	default:
		failOption("Versioning.Type", "Versioning.Type %v is unknown", v.Type)
	}
	copied := *v
	if copied.Key == "" {
		copied.Key = "v"
	}
	cfg.versioning = &copied
}

func (cfg *appConfig) loadStages(opts *AppOptions) {
	checkGroupOff("LoadShed", opts.LoadShed)
	checkGroupOff("HTTPSRedirect", opts.HTTPSRedirect)
	checkGroupOff("Headers", opts.Headers)
	checkGroupOff("CORS", opts.CORS)
	checkGroupOff("CSRF", opts.CSRF)
	checkGroupOff("Compress", opts.Compress)

	isProxy := opts.Proxy != nil && (len(opts.Proxy.TrustedProxies) > 0 || opts.Proxy.TrustedHops != 0)
	if opts.Proxy != nil && !isProxy {
		switch {
		case opts.Proxy.Header != ProxyHeaderXForwarded:
			failOption("Proxy.Header", "Proxy.Header is set without TrustedProxies or TrustedHops")
		case opts.Proxy.IsHostForwarded:
			failOption("Proxy.IsHostForwarded", "Proxy.IsHostForwarded is set without TrustedProxies or TrustedHops")
		}
	}
	isHeaders := opts.Headers != nil && opts.Headers.IsEnabled
	if isHeaders && opts.Headers.IsDisabled {
		failOption("Headers.IsEnabled", "Headers.IsEnabled and Headers.IsDisabled are both set")
	}
	isCSRF := opts.CSRF != nil && opts.CSRF.IsEnabled
	if isCSRF && opts.CSRF.IsDisabled {
		failOption("CSRF.IsEnabled", "CSRF.IsEnabled and CSRF.IsDisabled are both set")
	}
	cfg.isHeadersSet = isHeaders || (opts.Headers != nil && opts.Headers.IsDisabled)
	cfg.isCSRFSet = isCSRF || (opts.CSRF != nil && opts.CSRF.IsDisabled)
	isCORS := opts.CORS != nil && opts.CORS.IsEnabled
	origins := loadOrigins(opts.Origins, isCORS, isCSRF, cfg.ws != nil)
	if isCORS && origins.IsAny() && opts.CORS.IsAllowCredentials {
		failOption("Origins", `Origins "*" cannot be combined with CORS.IsAllowCredentials`)
	}
	if isCSRF && origins.IsAny() {
		failOption("Origins", `Origins "*" turns CSRF off: list the origins`)
	}

	var shed, trust, hosts namedStage
	if opts.LoadShed != nil && opts.LoadShed.IsEnabled {
		stage, err := loadshed.Compile(&loadshed.LoadShed{MaxInFlight: opts.LoadShed.MaxInFlight, MaxQueue: opts.LoadShed.MaxQueue})
		mustStage("LoadShed", err)
		shed = cfg.addStage("loadshed", stage)
	}
	if isProxy {
		p := opts.Proxy
		stage, err := proxy.Compile(&proxy.Proxy{TrustedProxies: p.TrustedProxies, TrustedHops: p.TrustedHops, Header: proxy.Header(p.Header), IsHostForwarded: p.IsHostForwarded})
		mustStage("Proxy", err)
		trust = cfg.addStage("proxy", stage)
	}
	if len(opts.Hosts) > 0 {
		stage, err := hostcheck.Compile(&hostcheck.HostCheck{Hosts: opts.Hosts})
		mustStage("", err)
		hosts = cfg.addStage("hostcheck", stage)
	}
	if h := opts.HTTPSRedirect; h != nil && h.IsEnabled {
		stage, err := httpsredirect.Compile(&httpsredirect.HTTPSRedirect{Host: h.Host, Port: h.Port})
		mustStage("HTTPSRedirect", err)
		cfg.addStage("httpsredirect", stage)
	}
	if isHeaders {
		h := opts.Headers
		stage, err := helmet.Compile(&helmet.Helmet{
			ContentSecurityPolicy:     h.ContentSecurityPolicy,
			CrossOriginEmbedderPolicy: h.CrossOriginEmbedderPolicy,
			CrossOriginOpenerPolicy:   h.CrossOriginOpenerPolicy,
			CrossOriginResourcePolicy: h.CrossOriginResourcePolicy,
			FrameOptions:              h.FrameOptions,
			PermissionsPolicy:         h.PermissionsPolicy,
			ReferrerPolicy:            h.ReferrerPolicy,
			StrictTransportSecurity:   h.StrictTransportSecurity,
		})
		mustStage("Headers", err)
		cfg.addStage("headers", stage)
	}
	if isCORS {
		c := opts.CORS
		stage, err := cors.Compile(&cors.CORS{
			AllowMethods:          c.AllowMethods,
			AllowHeaders:          c.AllowHeaders,
			ExposeHeaders:         c.ExposeHeaders,
			MaxAge:                c.MaxAge,
			IsAllowCredentials:    c.IsAllowCredentials,
			IsAllowPrivateNetwork: c.IsAllowPrivateNetwork,
		}, origins)
		mustStage("CORS", err)
		cfg.addStage("cors", stage)
	}
	if isCSRF {
		stage, err := csrf.Compile(&csrf.CSRF{}, origins)
		mustStage("CSRF", err)
		cfg.addStage("csrf", stage)
	}
	if c := opts.Compress; c != nil && c.IsEnabled {
		stage, err := compress.Compile(&compress.Compress{MinSize: int(c.MinSize)})
		mustStage("Compress", err)
		cfg.addStage("compress", stage)
	}

	if cfg.ws == nil {
		return
	}
	for _, s := range []namedStage{shed, trust, hosts} {
		if s.stage != nil {
			cfg.wsStages = append(cfg.wsStages, s)
		}
	}
	cfg.wsStages = append(cfg.wsStages, namedStage{name: "origin", stage: origin.NewCheck(origins)})
}

func (cfg *appConfig) addStage(name string, stage preRouteStage) namedStage {
	s := namedStage{name: name, stage: stage}
	cfg.stages = append(cfg.stages, s)
	return s
}

func mustStage(group string, err error) {
	if err != nil {
		groupError(group, err)
	}
}

func loadOrigins(entries []string, isCORS, isCSRF, isWS bool) *origin.Set {
	if len(entries) > 0 && !isCORS && !isCSRF && !isWS {
		failOption("Origins", "Origins is set, but CORS, CSRF and WS are off")
	}
	if isCORS && len(entries) == 0 {
		failOption("Origins", "CORS needs Origins")
	}
	set, err := origin.Parse("Origins", entries)
	if err != nil {
		failOption("Origins", "%s", err.Error())
	}
	return set
}

func checkGroupOff(group string, opts any) {
	v := reflect.ValueOf(opts)
	if v.IsNil() {
		return
	}
	v = v.Elem()
	if v.FieldByName("IsEnabled").Bool() {
		return
	}
	t := v.Type()
	for i := range t.NumField() {
		name := t.Field(i).Name
		if name == "IsEnabled" || name == "IsDisabled" || v.Field(i).IsZero() {
			continue
		}
		failOption(group+"."+name, "%s.%s is set while %s.IsEnabled is false", group, name, group)
	}
}
