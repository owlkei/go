package cors_test

import (
	"fmt"
	. "go.owlkei.com/owlkei/internal/middlewares/cors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/core"
	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/inspect"
	"go.owlkei.com/owlkei/internal/test"
)

func benchCORS(b *testing.B, r *http.Request) {
	b.Helper()
	mw := CORS{AllowOrigins: []string{appOrigin, "https://*.example.com"}, ExposeHeaders: []string{"X-Total"}}.NewMiddleware()
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
	app := core.New(&core.AppOptions{Logger: silentLogger{}})
	app.BindGlobalMiddlewares(mws...)
	app.Create(owlkei.ModuleBuilder().Controllers(okController{}).Build())
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

func expectRefusal(t *testing.T, w *httptest.ResponseRecorder, code int, body, desc string) {
	t.Helper()
	if w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, "G1: "+desc+": status"))
	}
	if w.Body.String() != body {
		t.Error(test.DiffMessage(w.Body.String(), body, "G1: "+desc+": body"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "G1: "+desc+": Content-Type"))
	}
}

const (
	appOrigin       = "https://app.example.com"
	defaultMethods  = "GET,HEAD,PUT,PATCH,POST,DELETE"
	defaultHeaders  = "Content-Type,Authorization"
	originRefusal   = `{"code":403,"error":"Forbidden","message":"Origin not allowed"}`
	optionsRouteOut = "options-route"
)

var listed = CORS{AllowOrigins: []string{appOrigin}}

func compact(s string) string { return strings.ReplaceAll(s, " ", "") }

func headerTokens(h http.Header, key string) []string {
	var tokens []string
	for _, value := range h.Values(key) {
		for _, token := range strings.Split(value, ",") {
			if token = strings.TrimSpace(token); token != "" {
				tokens = append(tokens, token)
			}
		}
	}
	return tokens
}

func hasToken(h http.Header, key, token string) bool {
	for _, t := range headerTokens(h, key) {
		if t == token {
			return true
		}
	}
	return false
}

func corsRequest(method, target, origin string, headers ...string) *http.Request {
	r := request(method, target, headers...)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func preflight(origin string, headers ...string) *http.Request {
	return corsRequest(http.MethodOptions, "/ok", origin, append([]string{"Access-Control-Request-Method", "POST"}, headers...)...)
}

func expectNoCORSHeaders(t *testing.T, w *httptest.ResponseRecorder, desc string) {
	t.Helper()
	for key := range w.Header() {
		if strings.HasPrefix(key, "Access-Control-") {
			t.Error(test.DiffMessage(key, "<no CORS header>", desc))
		}
	}
}

type varyBefore struct{ Value string }

func (v varyBefore) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Add("Vary", v.Value)
	next()
}
