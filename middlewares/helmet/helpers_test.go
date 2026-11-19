package helmet

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/core"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/inspect"
	"go.owlky.com/owlky/internal/test"
)

func benchHelmet(b *testing.B, r *http.Request) {
	b.Helper()
	mw := Helmet{}.NewMiddleware()
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		mw.Use(r, w, noop)
	}
}

func noop() {}

type okController struct{ common.HTTP }

func (c okController) NewController() core.Controller { return c }

func (okController) READ_ok() string { return "ok" }

func (okController) CREATE_ok() string { return "ok" }

func (okController) UPDATE_ok() string { return "ok" }

func (okController) MODIFY_ok() string { return "ok" }

func (okController) DELETE_ok() string { return "ok" }

func (okController) PREFLIGHT_ok() string { return "options-route" }

type silentLogger struct{}

func (silentLogger) Debug(string, ...any) {}

func (silentLogger) Info(string, ...any) {}

func (silentLogger) Warn(string, ...any) {}

func (silentLogger) Error(string, ...any) {}

func (silentLogger) Fatal(string, ...any) {}

type probeMiddleware struct{ Name string }

type probed struct {
	request *http.Request
	context any
}

var probes sync.Map

func (p probeMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	probes.Store(p.Name, probed{request: r, context: r.Context()})
	next()
}

func probeOf(name string) probed {
	v, _ := probes.Load(name)
	p, _ := v.(probed)
	return p
}

type failBody struct{ t *testing.T }

func (b failBody) Read([]byte) (int, error) {
	b.t.Error("G3: the middleware read the request body")
	return 0, fmt.Errorf("body must not be read")
}

func (failBody) Close() error { return nil }

type discardWriter struct {
	header http.Header
	code   int
}

func (w *discardWriter) Header() http.Header { return w.header }

func (w *discardWriter) WriteHeader(code int) { w.code = code }

func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func newApp(t testing.TB, mws ...common.MiddlewareFn) *core.App {
	t.Helper()
	app := core.New()
	app.SetLogger(silentLogger{})
	app.BindGlobalMiddlewares(mws...)
	app.Create(owlky.ModuleBuilder().Controllers(okController{}).Build())
	return app
}

func serve(app *core.App, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func request(method, target string, headers ...string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	return r
}

func panicText(fn func()) (text string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			text, panicked = fmt.Sprint(r), true
		}
	}()
	fn()
	return "", false
}

func field(s inspect.Snapshot, key string) any {
	for _, f := range s.Fields {
		if f.Key == key {
			return f.Value
		}
	}
	return nil
}

const (
	nonceDefaultPolicy = "default-src 'self'; script-src 'self' 'nonce-%s'; style-src 'self' 'nonce-%s'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests"
	nonceForeign       = "default-src 'none'; img-src data:"
)

var (
	nonceHexValue = regexp.MustCompile(`^[0-9a-f]{32}$`)
	nonceInHeader = regexp.MustCompile(`'nonce-([^']*)'`)
	nonceInBody   = regexp.MustCompile(`nonce="([^"]*)"`)
)

type noncePages struct {
	common.HTTP
	common.Middleware
}

func (c noncePages) NewController() core.Controller { return c }

func (c noncePages) RENDER_nonce() {}

func (c noncePages) RENDER_plain() {}

func (c noncePages) RENDER_boom() { panic(exception.NotFoundException("missing")) }

func (c noncePages) RENDER_foreign(h *ctx.HTTPContext) {
	h.ResponseWriter.Header().Set("Content-Security-Policy", nonceForeign)
}

type nonceLaterMiddleware struct{}

func (nonceLaterMiddleware) Use(_ *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set("Content-Security-Policy", nonceForeign)
	next()
}

type nonceLaterPages struct {
	common.HTTP
	common.Middleware
}

func (c nonceLaterPages) NewController() core.Controller {
	c.BindMiddleware(nonceLaterMiddleware{})
	return c
}

func (c nonceLaterPages) RENDER_later() {}

type nonceAPI struct{ common.HTTP }

func (c nonceAPI) NewController() core.Controller { return c }

func (c nonceAPI) READ_api() string { return "api" }

func nonceFiles(errorPage bool) fstest.MapFS {
	const used = `<script nonce="{{nonce}}"></script><style nonce="{{nonce}}"></style><i nonce="{{nonce}}"></i>`
	files := fstest.MapFS{
		"nonce.html":     {Data: []byte(used)},
		"foreign.html":   {Data: []byte(used)},
		"later.html":     {Data: []byte(used)},
		"plain.html":     {Data: []byte(`<p>plain</p>`)},
		"boom.html":      {Data: []byte(`boom`)},
		"assets/app.css": {Data: []byte(`a{}`)},
	}
	if errorPage {
		files["404.html"] = &fstest.MapFile{Data: []byte(`<script nonce="{{nonce}}"></script><b>{{.Code}}</b>`)}
	}
	return files
}

func nonceApp(t testing.TB, h Helmet, errorPage bool) *core.App {
	t.Helper()
	app := core.New()
	app.SetLogger(silentLogger{})
	app.BindGlobalMiddlewares(h)
	app.EnableRender(&core.RenderOptions{FS: nonceFiles(errorPage), Dir: "."})
	app.Create(core.NewModuleBuilder().Controllers(noncePages{}, nonceLaterPages{}, nonceAPI{}).Build())
	return app
}

func nonceServe(app *core.App, target string, headers ...string) *httptest.ResponseRecorder {
	return serve(app, request(http.MethodGet, target, headers...))
}

func directives(policy string) map[string]string {
	out := map[string]string{}
	for _, d := range strings.Split(policy, ";") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		name, value, _ := strings.Cut(d, " ")
		out[name] = value
	}
	return out
}

func directiveOrder(policy string) []string {
	var names []string
	for _, d := range strings.Split(policy, ";") {
		if name, _, _ := strings.Cut(strings.TrimSpace(d), " "); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func bodyNonce(t testing.TB, body string) string {
	t.Helper()
	found := nonceInBody.FindAllStringSubmatch(body, -1)
	if len(found) == 0 {
		t.Fatal(test.DiffMessage(body, "nonce attributes", "R33: the page carries nonces"))
	}
	for _, m := range found {
		if m[1] != found[0][1] {
			t.Error(test.DiffMessage(m[1], found[0][1], "R33: the same nonce at every use in the body"))
		}
	}
	return found[0][1]
}

func nonceHeader(t *testing.T, policy string) (string, string) {
	t.Helper()
	app := nonceApp(t, Helmet{ContentSecurityPolicy: policy}, false)
	w := nonceServe(app, "/nonce")
	return w.Header().Get("Content-Security-Policy"), bodyNonce(t, w.Body.String())
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, v := range a {
		seen[v] = true
	}
	for _, v := range b {
		if !seen[v] {
			return false
		}
	}
	return true
}

const (
	defaultCSP         = "default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests"
	defaultPermissions = "accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=(), browsing-topics=()"
	defaultHSTS        = "max-age=63072000; includeSubDomains"
)

var defaultsOverHTTPS = map[string]string{
	"Content-Security-Policy":           defaultCSP,
	"Cross-Origin-Opener-Policy":        "same-origin",
	"Cross-Origin-Resource-Policy":      "same-origin",
	"Permissions-Policy":                defaultPermissions,
	"Referrer-Policy":                   "no-referrer",
	"Strict-Transport-Security":         defaultHSTS,
	"X-Frame-Options":                   "DENY",
	"X-Content-Type-Options":            "nosniff",
	"X-XSS-Protection":                  "0",
	"X-DNS-Prefetch-Control":            "off",
	"X-Permitted-Cross-Domain-Policies": "none",
	"Origin-Agent-Cluster":              "?1",
}

var fixedHeaders = []string{"X-Content-Type-Options", "X-XSS-Protection", "X-DNS-Prefetch-Control", "X-Permitted-Cross-Domain-Policies", "Origin-Agent-Cluster"}

func secure(target string) *http.Request {
	r := request(http.MethodGet, target)
	r.TLS = &tls.ConnectionState{}
	return r
}

func expectHeader(t *testing.T, w *httptest.ResponseRecorder, key, want, desc string) {
	t.Helper()
	got := w.Header().Values(key)
	if want == "" {
		if len(got) != 0 {
			t.Error(test.DiffMessage(got, []string{}, desc+": "+key+" is not sent"))
		}
		return
	}
	if len(got) != 1 || got[0] != want {
		t.Error(test.DiffMessage(got, []string{want}, desc+": "+key))
	}
}

type headerProbe struct{ Name string }

var seen sync.Map

func (p headerProbe) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	seen.Store(p.Name, w.Header().Clone())
	next()
}

type headerOverride struct{ Key, Value string }

func (o headerOverride) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set(o.Key, o.Value)
	next()
}
