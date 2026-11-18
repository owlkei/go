package core

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/trace"
	"github.com/dangduoc08/ginject/log"
)

func BenchmarkHandshake_NoListener(b *testing.B) {
	ws := NewWS(&WSOptions{
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

	ws := NewWS(&WSOptions{
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
