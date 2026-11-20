package core

import (
	"testing"

	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/trace"
	"go.owlkei.com/owlkei/log"
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
