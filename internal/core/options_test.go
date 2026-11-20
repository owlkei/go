package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.owlkei.com/owlkei/internal/color"
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/test"
	"go.owlkei.com/owlkei/internal/versioning"
)

func optRecover(fn func()) (msg string, isPanic bool) {
	defer func() {
		if r := recover(); r != nil {
			isPanic = true
			msg = fmt.Sprint(r)
		}
	}()
	fn()
	return "", false
}

func optCertFiles(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	certs := srvMakeCert(t)
	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certs.certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, certs.keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func optList(prefix string, n int, format string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf(format, prefix, i)
	}
	return out
}

type optBodyController struct{ common.HTTP }

func (c optBodyController) NewController() Controller { return c }

func (c optBodyController) CREATE_echo(body ctx.Body) any { return body }

func optServe(app *App, method, target string, body string, header map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range header {
		if k == "Host" {
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func TestNew_NilEqualsEmpty(t *testing.T) {
	for name, app := range map[string]*App{"nil": New(nil), "empty": New(&AppOptions{})} {
		app.Create(NewModuleBuilder().Controllers(optBodyController{}).Build())
		big := `{"k":"` + strings.Repeat("a", 10*1024*1024) + `"}`
		w := optServe(app, http.MethodPost, "/echo", big, map[string]string{"Content-Type": "application/json"})
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Error(test.DiffMessage(w.Code, 413, "A1: "+name+" has the 10MiB body cap"))
		}
		if app.Logger() == nil {
			t.Error(test.DiffMessage(nil, "a logger", "A1: "+name+" has a default logger"))
		}
		w = optServe(app, http.MethodGet, "/nothing", "", map[string]string{"Host": "x.com"})
		if w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, 404, "A1: "+name+" runs no stage"))
		}
	}
}

func TestNew_DefaultsMatchOldApp(t *testing.T) {
	app := New(&AppOptions{})
	if app.Logger() == nil {
		t.Error(test.DiffMessage(nil, "a logger", "A1: default logger"))
	}
	if app.tracer.Enabled() {
		t.Error(test.DiffMessage(true, false, "A1: no access log by default"))
	}
	app.Create(NewModuleBuilder().Build())
	if app.devtool != nil {
		t.Error(test.DiffMessage(app.devtool, nil, "A1: no devtool by default"))
	}
}

func TestNew_LoggerTurnsOnLogger(t *testing.T) {
	l := &srvLogger{}
	app := New(&AppOptions{Logger: l})
	app.Create(NewModuleBuilder().Controllers(srvController{}).Build())
	app.Logger().Info("Probe", "key", "value")
	entries := l.named("Probe")
	if len(entries) != 1 || entries[0].level != "info" || entries[0].field("key") != "value" {
		t.Error(test.DiffMessage(entries, "one info Probe line", "A1: Logger ≡ SetLogger, the app logs to the given logger"))
	}
}

func TestNew_StartupTimeoutTurnsOnDeadline(t *testing.T) {
	app := New(&AppOptions{StartupTimeout: 50 * time.Millisecond, Logger: &mockLogger{}})
	slow := func(c context.Context) (*Module, error) {
		<-c.Done()
		return nil, c.Err()
	}
	msg, isPanic := optRecover(func() { app.Create(NewModuleBuilder().Imports(slow).Build()) })
	if !isPanic || !strings.Contains(msg, "invalid module") {
		t.Error(test.DiffMessage(msg, "invalid module: …", "A1: StartupTimeout ≡ SetStartupTimeout"))
	}
}

func TestNew_IsAccessLogTurnsOnAccessLog(t *testing.T) {
	app := New(&AppOptions{IsAccessLog: true, Logger: &mockLogger{}})
	app.Create(NewModuleBuilder().Build())
	if !app.tracer.Enabled() {
		t.Error(test.DiffMessage(false, true, "A1: IsAccessLog ≡ EnableAccessLog"))
	}
}

func TestNew_IsDevtoolTurnsOnDevtool(t *testing.T) {
	app := New(&AppOptions{IsDevtool: true, Logger: &mockLogger{}})
	app.Create(NewModuleBuilder().Build())
	if app.devtool == nil {
		t.Error(test.DiffMessage(nil, "a devtool snapshot", "A1: IsDevtool ≡ EnableDevtool"))
	}
}

func TestNew_RenderTurnsOnPages(t *testing.T) {
	app := New(&AppOptions{Logger: &mockLogger{}, Render: &RenderOptions{FS: pgFiles(map[string]string{"rows.html": "<p>rows</p>"}), Dir: "."}})
	app.Create(NewModuleBuilder().Controllers(pgRowsOnly{}).Build())
	w := optServe(app, http.MethodGet, "/rows", "", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<p>rows</p>") {
		t.Error(test.DiffMessage(w.Code, 200, "A1: Render.FS ≡ EnableRender"))
	}
}

func TestNew_WSPathTurnsOnWebSocket(t *testing.T) {
	app := New(&AppOptions{Logger: &mockLogger{}, WS: &WSOptions{Path: "/ws"}})
	app.Create(NewModuleBuilder().Controllers(wsScopeController{}).Build())
	server := httptest.NewServer(app)
	defer server.Close()
	conn, _ := (&wsScopeEnv{app: app, server: server}).dial(t, nil)
	if conn == nil {
		t.Error(test.DiffMessage(nil, "a connection", "A1: WS.Path ≡ EnableWS"))
	}
}

func TestNew_VersioningTypeTurnsOnVersioning(t *testing.T) {
	app := New(&AppOptions{Logger: &mockLogger{}, Versioning: &versioning.Options{Type: versioning.TypeQuery, Key: "v"}})
	app.Create(NewModuleBuilder().Controllers(z1VersionedController{}).Build())
	w := optServe(app, http.MethodGet, "/report?v=2", "", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "report2") {
		t.Error(test.DiffMessage(w.Body.String(), "report2", "A1: Versioning.Type ≡ EnableVersioning"))
	}
}

func TestNew_CopiesOptions(t *testing.T) {
	certFile, keyFile := optCertFiles(t)
	origins := []string{"https://a.com"}
	hosts := []string{"a.com"}
	cfg := &tls.Config{MinVersion: tls.VersionTLS13}
	opts := &AppOptions{
		Origins: origins, Hosts: hosts, CORS: &CORSOptions{IsEnabled: true},
		Server: &ServerOptions{TLS: &TLSOptions{CertFile: certFile, KeyFile: keyFile, Config: cfg}},
		Logger: &mockLogger{},
	}
	app := New(opts)
	origins[0] = "https://evil.com"
	hosts[0] = "evil.com"
	opts.Origins = append(opts.Origins, "https://more.com")
	cfg.MinVersion = tls.VersionTLS10
	app.Create(NewModuleBuilder().Controllers(optBodyController{}).Build())

	w := optServe(app, http.MethodOptions, "/echo", "", map[string]string{
		"Host": "a.com", "Origin": "https://a.com", "Access-Control-Request-Method": "POST",
	})
	if w.Header().Get("Access-Control-Allow-Origin") != "https://a.com" {
		t.Error(test.DiffMessage(w.Header().Get("Access-Control-Allow-Origin"), "https://a.com", "A2: Origins copied at New"))
	}
	w = optServe(app, http.MethodGet, "/echo", "", map[string]string{"Host": "evil.com"})
	if w.Code != http.StatusMisdirectedRequest {
		t.Error(test.DiffMessage(w.Code, 421, "A2: Hosts copied at New"))
	}
	if cfg.MinVersion != tls.VersionTLS10 {
		t.Error(test.DiffMessage(cfg.MinVersion, tls.VersionTLS10, "A2: the caller's tls.Config is not modified"))
	}
}

func TestNew_InvalidOptionsPanic(t *testing.T) {
	certFile, keyFile := optCertFiles(t)
	badFile := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(badFile, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	origin := []string{"https://a.com"}
	rows := []struct {
		name   string
		opts   *AppOptions
		detail string
		prefix bool
	}{
		{"StartupTimeout", &AppOptions{StartupTimeout: -time.Second}, "StartupTimeout is -1s, it must be positive", false},
		{"PortNegative", &AppOptions{Server: &ServerOptions{Port: -1}}, "Server.Port is -1, it must be between 1 and 65535", false},
		{"PortTooBig", &AppOptions{Server: &ServerOptions{Port: 65536}}, "Server.Port is 65536, it must be between 1 and 65535", false},
		{"ShutdownTimeout", &AppOptions{Server: &ServerOptions{ShutdownTimeout: -time.Second}}, "Server.ShutdownTimeout is -1s, it must be positive", false},
		{"OnlyCert", &AppOptions{Server: &ServerOptions{TLS: &TLSOptions{CertFile: certFile}}}, "Server.TLS needs both CertFile and KeyFile", false},
		{"OnlyKey", &AppOptions{Server: &ServerOptions{TLS: &TLSOptions{KeyFile: keyFile}}}, "Server.TLS needs both CertFile and KeyFile", false},
		{"PairFails", &AppOptions{Server: &ServerOptions{TLS: &TLSOptions{CertFile: badFile, KeyFile: badFile}}}, "Server.TLS: load certificate: ", true},
		{"ConfigNoCertificate", &AppOptions{Server: &ServerOptions{TLS: &TLSOptions{Config: &tls.Config{}}}}, "Server.TLS.Config has no certificate", false},
		{"TLSWithH2C", &AppOptions{Server: &ServerOptions{IsH2C: true, TLS: &TLSOptions{CertFile: certFile, KeyFile: keyFile}}}, "Server.IsH2C applies to connections without TLS", false},
		{"ServeDirsEmptyEntry", &AppOptions{HTTP: &HTTPOptions{ServeDirs: []string{"a", ""}}}, "HTTP.ServeDirs has an empty entry", false},
		{"RenderDirWithoutFS", &AppOptions{Render: &RenderOptions{Dir: "web"}}, "Render.Dir is set without Render.FS", false},
		{"RenderReloadWithoutFS", &AppOptions{Render: &RenderOptions{IsReload: true}}, "Render.IsReload is set without Render.FS", false},
		{"VersioningKeyWithoutType", &AppOptions{Versioning: &versioning.Options{Key: "v"}}, "Versioning.Key is set without Versioning.Type", false},
		{"VersioningDefaultWithoutType", &AppOptions{Versioning: &versioning.Options{DefaultVersion: "1"}}, "Versioning.DefaultVersion is set without Versioning.Type", false},
		{"VersioningUnknownType", &AppOptions{Versioning: &versioning.Options{Type: 99}}, "Versioning.Type 99 is unknown", false},
		{"VersioningCustomWithoutExtractor", &AppOptions{Versioning: &versioning.Options{Type: versioning.TypeCustom}}, "Versioning.Type custom needs an Extractor", false},
		{"VersioningExtractorWithOtherType", &AppOptions{Versioning: &versioning.Options{Type: versioning.TypeQuery, Extractor: func(*ctx.HTTPContext) string { return "" }}}, "Versioning.Extractor needs Type VersioningCustom", false},
		{"LoadShedFieldWhileOff", &AppOptions{LoadShed: &LoadShedOptions{MaxInFlight: 5}}, "LoadShed.MaxInFlight is set while LoadShed.IsEnabled is false", false},
		{"CompressFieldWhileOff", &AppOptions{Compress: &CompressOptions{MinSize: 5}}, "Compress.MinSize is set while Compress.IsEnabled is false", false},
		{"HTTPSRedirectFieldWhileOff", &AppOptions{HTTPSRedirect: &HTTPSRedirectOptions{Port: 8443}}, "HTTPSRedirect.Port is set while HTTPSRedirect.IsEnabled is false", false},
		{"CORSFieldWhileOff", &AppOptions{Origins: origin, CSRF: &CSRFOptions{IsEnabled: true}, CORS: &CORSOptions{MaxAge: time.Minute}}, "CORS.MaxAge is set while CORS.IsEnabled is false", false},
		{"ProxyHeaderWithoutTrust", &AppOptions{Proxy: &ProxyOptions{Header: ProxyHeaderForwarded}}, "Proxy.Header is set without TrustedProxies or TrustedHops", false},
		{"ProxyHostWithoutTrust", &AppOptions{Proxy: &ProxyOptions{IsHostForwarded: true}}, "Proxy.IsHostForwarded is set without TrustedProxies or TrustedHops", false},
		{"HeadersBoth", &AppOptions{Headers: &HeadersOptions{IsEnabled: true, IsDisabled: true}}, "Headers.IsEnabled and Headers.IsDisabled are both set", false},
		{"CSRFBoth", &AppOptions{Origins: origin, CSRF: &CSRFOptions{IsEnabled: true, IsDisabled: true}}, "CSRF.IsEnabled and CSRF.IsDisabled are both set", false},
		{"OriginsWithoutConsumer", &AppOptions{Origins: origin}, "Origins is set, but CORS, CSRF and WS are off", false},
		{"CORSWithoutOrigins", &AppOptions{CORS: &CORSOptions{IsEnabled: true}}, "CORS needs Origins", false},
		{"StarNotAlone", &AppOptions{Origins: []string{"*", "https://a.com"}, CORS: &CORSOptions{IsEnabled: true}}, `"*" must be the only entry of Origins`, false},
		{"StarWithCredentials", &AppOptions{Origins: []string{"*"}, CORS: &CORSOptions{IsEnabled: true, IsAllowCredentials: true}}, `Origins "*" cannot be combined with CORS.IsAllowCredentials`, false},
		{"StarWithCSRF", &AppOptions{Origins: []string{"*"}, CSRF: &CSRFOptions{IsEnabled: true}}, `Origins "*" turns CSRF off: list the origins`, false},
		{"TooManyOrigins", &AppOptions{Origins: optList("https://o", 257, "%s%d.com"), CORS: &CORSOptions{IsEnabled: true}}, "Origins has 257 entries, at most 256 are allowed", false},
		{"BadOriginEntry", &AppOptions{Origins: []string{"https://A.com"}, CORS: &CORSOptions{IsEnabled: true}}, "Origins entry ", true},
		{"ProxyHops", &AppOptions{Proxy: &ProxyOptions{TrustedHops: 20}}, "Proxy.TrustedHops is 20, it must be between 1 and 16", false},
		{"TooManyHosts", &AppOptions{Hosts: optList("h", 300, "%s%d.com")}, "Hosts has 300 entries, at most 256 are allowed", false},
		{"HeadersControlChar", &AppOptions{Headers: &HeadersOptions{IsEnabled: true, FrameOptions: "a\nb"}}, "Headers.FrameOptions has a control character", false},
		{"CompressMinSize", &AppOptions{Compress: &CompressOptions{IsEnabled: true, MinSize: -1}}, "Compress.MinSize is -1, it must be 0 or more", false},
	}
	for _, row := range rows {
		msg, isPanic := optRecover(func() { New(row.opts) })
		want := color.FmtRed("invalid app: " + row.detail)
		switch {
		case !isPanic:
			t.Error(test.DiffMessage("no panic", want, "A3: "+row.name))
		case row.prefix && !strings.Contains(msg, "invalid app: "+row.detail):
			t.Error(test.DiffMessage(msg, want+"…", "A3: "+row.name))
		case !row.prefix && msg != want:
			t.Error(test.DiffMessage(msg, want, "A3: "+row.name))
		}
	}
}

func TestNew_ValidBoundariesDoNotPanic(t *testing.T) {
	cases := map[string]*AppOptions{
		"Port0":         {Server: &ServerOptions{Port: 0}},
		"Port1":         {Server: &ServerOptions{Port: 1}},
		"Port65535":     {Server: &ServerOptions{Port: 65535}},
		"Hops1":         {Proxy: &ProxyOptions{TrustedHops: 1}},
		"Hops16":        {Proxy: &ProxyOptions{TrustedHops: 16}},
		"Origins256":    {Origins: optList("https://o", 256, "%s%d.com"), CORS: &CORSOptions{IsEnabled: true}},
		"Hosts256":      {Hosts: optList("h", 256, "%s%d.com")},
		"OriginsStar":   {Origins: []string{"*"}, CORS: &CORSOptions{IsEnabled: true}},
		"OriginsWild":   {Origins: []string{"https://*.a.com"}, CSRF: &CSRFOptions{IsEnabled: true}},
		"OriginsWSOnly": {Origins: []string{"https://a.com"}, WS: &WSOptions{Path: "/ws"}},
		"IsDisabled":    {Headers: &HeadersOptions{IsDisabled: true}, CSRF: &CSRFOptions{IsDisabled: true}},
	}
	for name, opts := range cases {
		opts.Logger = &mockLogger{}
		if msg, isPanic := optRecover(func() { New(opts) }); isPanic {
			t.Error(test.DiffMessage(msg, "no panic", "A3: "+name+" is valid"))
		}
	}
}

func TestNew_ConfigurationErrorsComeBeforeCreate(t *testing.T) {
	msg, isPanic := optRecover(func() { New(&AppOptions{StartupTimeout: -1}) })
	if !isPanic || !strings.Contains(msg, "invalid app: StartupTimeout") {
		t.Error(test.DiffMessage(msg, "invalid app: StartupTimeout…", "A3: New checks, Create is not needed"))
	}
}

func TestCreate_TwiceStillPanics(t *testing.T) {
	app := New(&AppOptions{Logger: &mockLogger{}})
	app.Create(NewModuleBuilder().Build())
	msg, isPanic := optRecover(func() { app.Create(NewModuleBuilder().Build()) })
	want := color.FmtRed("invalid app: Create called twice")
	if !isPanic || msg != want {
		t.Error(test.DiffMessage(msg, want, "A4: Create twice"))
	}
}

func TestNew_IgnoresPORT(t *testing.T) {
	t.Setenv("PORT", "4567")
	l := &srvLogger{}
	app := New(&AppOptions{Logger: l})
	app.Create(NewModuleBuilder().Build())
	done := make(chan error, 1)
	go func() { done <- app.Listen() }()
	select {
	case <-l.seen("ServerListening"):
	case err := <-done:
		t.Skipf("port 3000 not available: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not start")
	}
	defer app.Stop()
	addr, _ := l.named("ServerListening")[0].field("address").(string)
	if !strings.HasSuffix(addr, ":3000") {
		t.Error(test.DiffMessage(addr, "…:3000", "A5: PORT is read only by OptionsFromEnv"))
	}
}

func TestNew_ServeDirsDeclared(t *testing.T) {
	dirs := []string{"public"}
	app := New(&AppOptions{HTTP: &HTTPOptions{ServeDirs: dirs}, Logger: &mockLogger{}})
	dirs[0] = "changed"
	if app == nil {
		t.Fatal("D1: New accepts ServeDirs")
	}
	msg, isPanic := optRecover(func() { New(&AppOptions{HTTP: &HTTPOptions{ServeDirs: []string{""}}}) })
	if !isPanic || !strings.Contains(msg, "HTTP.ServeDirs has an empty entry") {
		t.Error(test.DiffMessage(msg, "HTTP.ServeDirs has an empty entry", "D1: an empty entry panics"))
	}
}

func TestNew_TLSConfigCertificatesNotWrittenThrough(t *testing.T) {
	certFile, keyFile := optCertFiles(t)
	own := srvMakeCert(t).cert
	certs := make([]tls.Certificate, 1, 4)
	certs[0] = own
	cfg := &tls.Config{Certificates: certs}

	app := New(&AppOptions{
		Server: &ServerOptions{TLS: &TLSOptions{CertFile: certFile, KeyFile: keyFile, Config: cfg}},
		Logger: &mockLogger{},
	})

	if got := len(app.server.tlsConfig.Certificates); got != 2 {
		t.Fatal(test.DiffMessage(got, 2, "fixture: the app serves the caller's certificate and the file pair"))
	}
	if spare := certs[:2][1]; len(spare.Certificate) != 0 || spare.PrivateKey != nil {
		t.Error(test.DiffMessage(len(spare.Certificate), 0, "A2: the file pair is not written into the caller's Certificates backing array"))
	}
	if len(cfg.Certificates) != 1 || !reflect.DeepEqual(cfg.Certificates[0].Certificate, own.Certificate) {
		t.Error(test.DiffMessage(len(cfg.Certificates), 1, "A2: the caller's Certificates slice is unchanged"))
	}
}

func TestNew_TLSConfigCertificatesIndependentOfLaterWrites(t *testing.T) {
	certFile, keyFile := optCertFiles(t)
	own := srvMakeCert(t).cert
	cfg := &tls.Config{Certificates: []tls.Certificate{own}}

	app := New(&AppOptions{
		Server: &ServerOptions{TLS: &TLSOptions{CertFile: certFile, KeyFile: keyFile, Config: cfg}},
		Logger: &mockLogger{},
	})
	cfg.Certificates[0] = tls.Certificate{}

	if got := app.server.tlsConfig.Certificates[0].Certificate; !reflect.DeepEqual(got, own.Certificate) {
		t.Error(test.DiffMessage(len(got), len(own.Certificate), "A2: a later write to the caller's Certificates does not reach the app"))
	}
}
