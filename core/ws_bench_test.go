package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/trace"
	"github.com/dangduoc08/ginject/log"
)

func benchHandshakeContext() *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()
	return c
}

func BenchmarkHandshake_NoListener(b *testing.B) {
	ws := NewWS(&WSConfig{
		logger:            log.NewLog(nil),
		tracer:            &trace.Tracer{},
		injectedProviders: map[string]Provider{},
		container:         newContainer(),
		globalMiddlewares: []common.MiddlewareFn{wsHandshakeTraceMiddleware{}, wsHandshakeTraceMiddlewareB{}},
	})

	b.ReportAllocs()
	for b.Loop() {
		c := benchHandshakeContext()
		_ = ws.handshake(c)
	}
}

func BenchmarkHandshake_WithListener(b *testing.B) {
	ev := &trace.Tracer{}
	ev.Listen(func(trace.Event) {})

	ws := NewWS(&WSConfig{
		logger:            log.NewLog(nil),
		tracer:            ev,
		injectedProviders: map[string]Provider{},
		container:         newContainer(),
		globalMiddlewares: []common.MiddlewareFn{wsHandshakeTraceMiddleware{}, wsHandshakeTraceMiddlewareB{}},
	})

	b.ReportAllocs()
	for b.Loop() {
		c := benchHandshakeContext()
		_ = ws.handshake(c)
	}
}
