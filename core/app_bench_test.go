package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/event"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/trace"
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

func BenchmarkTraceHTTPHandler_NoListener(b *testing.B) {
	ev := event.NewEvent()
	c := newHTTPContext()
	h := traceHTTPHandler(ev, trace.StageGuard, "bench.Guard", func(c *ctx.HTTPContext) {})
	b.ReportAllocs()
	for b.Loop() {
		h(c)
	}
}

func BenchmarkTraceHTTPHandler_WithListener(b *testing.B) {
	ev := event.NewEvent()
	ev.On(trace.EventName, func(args ...any) {})
	c := newHTTPContext()
	h := traceHTTPHandler(ev, trace.StageGuard, "bench.Guard", func(c *ctx.HTTPContext) {})
	b.ReportAllocs()
	for b.Loop() {
		h(c)
	}
}

func BenchmarkTraceHTTPCatch_NoListener(b *testing.B) {
	ev := event.NewEvent()
	c := newHTTPContext()
	ex := exception.BadRequestException("bad")
	fn := traceHTTPCatch(ev, "bench.Filter", func(c *ctx.HTTPContext, ex *exception.Exception) {})
	b.ReportAllocs()
	for b.Loop() {
		fn(c, &ex)
	}
}
