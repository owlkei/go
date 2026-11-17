package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
)

type panicBenchController struct{ common.HTTP }

func (c panicBenchController) NewController() Controller { return c }

func (c panicBenchController) READ_boom(*ctx.HTTPContext) any { panic("boom") }

type panicBenchWriter struct{ header http.Header }

func (w *panicBenchWriter) Header() http.Header         { return w.header }
func (w *panicBenchWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *panicBenchWriter) WriteHeader(int)             {}

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
