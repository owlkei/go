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
	app := New(&AppOptions{Logger: &mockLogger{}})
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
	app := New(&AppOptions{Logger: &mockLogger{}})
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

func BenchmarkPreRoute_None(b *testing.B) {
	app := New(&AppOptions{Logger: &mockLogger{}})
	app.Create(NewModuleBuilder().Controllers(preController{}).Build())
	r := httptest.NewRequest(http.MethodGet, "/hello", nil)
	w := &benchDiscardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		app.ServeHTTP(w, r)
	}
}

func BenchmarkPreRoute_All(b *testing.B) {
	app := New(&AppOptions{
		Logger:        &mockLogger{},
		Origins:       []string{"https://a.com"},
		LoadShed:      &LoadShedOptions{IsEnabled: true},
		Proxy:         &ProxyOptions{TrustedProxies: []string{"192.0.2.1"}},
		Hosts:         []string{"example.com"},
		HTTPSRedirect: &HTTPSRedirectOptions{IsEnabled: true},
		Headers:       &HeadersOptions{IsEnabled: true},
		CORS:          &CORSOptions{IsEnabled: true},
		CSRF:          &CSRFOptions{IsEnabled: true},
		Compress:      &CompressOptions{IsEnabled: true},
	})
	app.Create(NewModuleBuilder().Controllers(preController{}).Build())
	r := httptest.NewRequest(http.MethodGet, "https://example.com/hello", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	w := &benchDiscardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		app.ServeHTTP(w, r)
	}
}

func BenchmarkPreRoute_LoadShed(b *testing.B) {
	app := New(&AppOptions{Logger: &mockLogger{}, LoadShed: &LoadShedOptions{IsEnabled: true}})
	app.Create(NewModuleBuilder().Controllers(preController{}).Build())
	r := httptest.NewRequest(http.MethodGet, "/hello", nil)
	w := &benchDiscardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		app.ServeHTTP(w, r)
	}
}
