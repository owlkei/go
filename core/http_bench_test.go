package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
)

type benchPlainController struct {
	common.HTTP
}

func (c benchPlainController) NewController() Controller { return c }
func (c benchPlainController) READ_bench() string        { return "ok" }
func (c benchPlainController) CREATE_bench() string      { return "ok" }
func (c benchPlainController) READ_benchexception() string {
	panic(exception.BadRequestException("bad input"))
}
func (c benchPlainController) READ_benchpanic() string { panic("boom") }

type benchDiscardWriter struct {
	header http.Header
}

func (w *benchDiscardWriter) Header() http.Header         { return w.header }
func (w *benchDiscardWriter) WriteHeader(int)             {}
func (w *benchDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }

func benchHTTPContext(urlPath string) *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, urlPath, nil)
	c.ResponseWriter = httptest.NewRecorder()
	return c
}

func benchServe(b *testing.B, app *App, method, target string) {
	b.Helper()
	r := httptest.NewRequest(method, target, nil)
	w := &benchDiscardWriter{header: http.Header{}}

	b.ReportAllocs()
	for b.Loop() {
		app.ServeHTTP(w, r)
	}
}

func benchApp(controllers ...Controller) *App {
	resetModuleGlobals()
	app := New()
	app.UseLogger(&mockLogger{})
	app.Create(ModuleBuilder().Controllers(controllers...).Build())
	return app
}

func BenchmarkServeHTTP_Static(b *testing.B) {
	benchServe(b, benchApp(benchPlainController{}), http.MethodGet, "/bench")
}

func BenchmarkServeHTTP_Chain(b *testing.B) {
	benchServe(b, benchApp(tracePipelineController{}), http.MethodGet, "/tracepipeline")
}

func BenchmarkServeHTTP_NotFound(b *testing.B) {
	benchServe(b, benchApp(benchPlainController{}), http.MethodGet, "/does-not-exist")
}

func BenchmarkServeHTTP_MethodNotAllowed(b *testing.B) {
	benchServe(b, benchApp(benchPlainController{}), http.MethodDelete, "/bench")
}

func BenchmarkServeHTTP_Exception(b *testing.B) {
	benchServe(b, benchApp(benchPlainController{}), http.MethodGet, "/benchexception")
}

func BenchmarkServeHTTP_PanicString(b *testing.B) {
	benchServe(b, benchApp(benchPlainController{}), http.MethodGet, "/benchpanic")
}

func BenchmarkServeHTTP_MultiInterceptor(b *testing.B) {
	benchServe(b, benchApp(multiInterceptorController{}), http.MethodGet, "/multiintercept")
}
