package core

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/dangduoc08/ginject/internal/trace"
)

func BenchmarkServeHTTP(b *testing.B) {
	app := New()
	app.Create(ModuleBuilder().Build())

	r := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	b.ReportAllocs()
	for b.Loop() {
		app.ServeHTTP(w, r)
	}
}

func BenchmarkServeHTTP_StaticWithTraceparent(b *testing.B) {
	app := benchApp(benchPlainController{})
	r := httptest.NewRequest(http.MethodGet, "/bench", nil)
	r.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	w := &benchDiscardWriter{header: http.Header{}}

	b.ReportAllocs()
	for b.Loop() {
		app.ServeHTTP(w, r)
	}
}

func BenchmarkServeHTTP_StaticWithListener(b *testing.B) {
	app := benchApp(benchPlainController{})
	var events atomic.Int64
	app.tracer.Listen(func(e trace.Event) { events.Add(1) })
	benchServe(b, app, http.MethodGet, "/bench")
}

func BenchmarkServeHTTP_StaticParallel(b *testing.B) {
	app := benchApp(benchPlainController{})
	r := httptest.NewRequest(http.MethodGet, "/bench", nil)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		w := &benchDiscardWriter{header: http.Header{}}
		for pb.Next() {
			app.ServeHTTP(w, r)
		}
	})
}
