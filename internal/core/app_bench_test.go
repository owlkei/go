package core

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"go.owlky.com/owlky/internal/trace"
)

func BenchmarkServeHTTP(b *testing.B) {
	app := New(nil)
	app.Create(NewModuleBuilder().Build())

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

func BenchmarkCreate_FiveControllers(b *testing.B) {
	m := NewModuleBuilder().
		Controllers(
			benchModuleController0{},
			benchModuleController1{},
			benchModuleController2{},
			benchModuleController3{},
			benchModuleController4{},
		).
		Build()
	b.ReportAllocs()
	for b.Loop() {
		New(nil).Create(m)
	}
}

func BenchmarkCreate_ChainedProviders(b *testing.B) {
	m := NewModuleBuilder().Providers(benchChainProviders()...).Build()
	b.ReportAllocs()
	for b.Loop() {
		New(nil).Create(m)
	}
}

func BenchmarkCreate_StaticSubmodules(b *testing.B) {
	const n = 100
	root := NewModuleBuilder().Imports(makeBenchStaticSubmodules(n)...).Build()
	b.ReportAllocs()
	for b.Loop() {
		New(nil).Create(root)
	}
}

func BenchmarkCreate_FactorySubmodules(b *testing.B) {
	const n = 100
	imports := make([]any, n)
	for i := range imports {
		imports[i] = benchCollectModulesFactory
	}
	root := NewModuleBuilder().Imports(imports...).Build()
	b.ReportAllocs()
	for b.Loop() {
		New(nil).Create(root)
	}
}
