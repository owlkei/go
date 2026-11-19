package core

import (
	"testing"

	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/trace"
	"go.owlky.com/owlky/log"
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
