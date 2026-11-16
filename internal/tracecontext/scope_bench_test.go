package tracecontext

import (
	"context"
	"net/http"
	"testing"
)

func BenchmarkScopeOf(b *testing.B) {
	type key struct{}
	c, _ := NewRequest(context.Background(), http.Header{}, fixedNow)
	c = context.WithValue(c, key{}, 1)
	c, cancel := context.WithCancel(c)
	defer cancel()
	c = context.WithValue(c, key{}, 2)
	b.ReportAllocs()
	for b.Loop() {
		ScopeOf(c)
	}
}

func BenchmarkNewRequest_OwnTrace(b *testing.B) {
	h := http.Header{}
	b.ReportAllocs()
	for b.Loop() {
		NewRequest(context.Background(), h, fixedNow)
	}
}

func BenchmarkNewRequest_ContinuesTrace(b *testing.B) {
	h := headerOf("Traceparent", validHeader, "X-Request-Id", "req-1")
	b.ReportAllocs()
	for b.Loop() {
		NewRequest(context.Background(), h, fixedNow)
	}
}

func BenchmarkNewMessage(b *testing.B) {
	connCtx, _ := NewRequest(context.Background(), http.Header{}, fixedNow)
	b.ReportAllocs()
	for b.Loop() {
		NewMessage(connCtx, fixedNow)
	}
}

func BenchmarkScope_AppendArgs(b *testing.B) {
	_, s := NewRequest(context.Background(), headerOf("Traceparent", validHeader), fixedNow)
	dst := make([]any, 0, 8)
	b.ReportAllocs()
	for b.Loop() {
		dst = s.AppendArgs(dst[:0])
	}
}
