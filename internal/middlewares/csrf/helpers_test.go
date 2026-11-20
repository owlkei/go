package csrf_test

import (
	"fmt"
	. "go.owlkei.com/owlkei/internal/middlewares/csrf"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/core"
	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/inspect"
	"go.owlkei.com/owlkei/internal/test"
)

func benchCSRF(b *testing.B, r *http.Request) {
	b.Helper()
	mw := CSRF{TrustedOrigins: []string{"https://app.example.com"}}.NewMiddleware()
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
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

const refusedBody = `{"code":403,"error":"Forbidden","message":"Cross-origin request refused"}`

var unsafeMethods = []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

func csrfRequest(method string, fetchSite, origin string) *http.Request {
	r := request(method, "/ok")
	if fetchSite != "" {
		r.Header.Set("Sec-Fetch-Site", fetchSite)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func expectPass(t *testing.T, w *httptest.ResponseRecorder, desc string) {
	t.Helper()
	if w.Code >= http.StatusBadRequest || w.Body.String() == refusedBody {
		t.Error(test.DiffMessage(w.Code, "<2xx>", desc+": the request passes"))
	}
}

func expectRefused(t *testing.T, w *httptest.ResponseRecorder, desc string) {
	t.Helper()
	if w.Code != http.StatusForbidden || w.Body.String() != refusedBody {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{403, refusedBody}, desc+": the request is refused"))
	}
}
