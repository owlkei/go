package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func BenchmarkServeHTTP_PanicFlood(b *testing.B) {
	app := New().SetLogger(&mockLogger{})
	app.Create(NewModuleBuilder().Controllers(panicBenchController{}).Build())
	defer app.Stop()
	r := httptest.NewRequest(http.MethodGet, "/boom", nil)
	w := &panicBenchWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		app.ServeHTTP(w, r)
	}
}

func BenchmarkServeHTTP_PanicFlood_Parallel(b *testing.B) {
	app := New().SetLogger(&mockLogger{})
	app.Create(NewModuleBuilder().Controllers(panicBenchController{}).Build())
	defer app.Stop()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		r := httptest.NewRequest(http.MethodGet, "/boom", nil)
		w := &panicBenchWriter{header: http.Header{}}
		for pb.Next() {
			clear(w.header)
			app.ServeHTTP(w, r)
		}
	})
}
