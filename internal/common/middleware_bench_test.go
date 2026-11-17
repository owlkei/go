package common

import (
	"testing"
)

func BenchmarkInjectProvidersIntoHTTPMiddlewares_ApplyAll(b *testing.B) {
	r := buildBenchHTTP(20)
	b.ReportAllocs()
	for b.Loop() {
		m := &Middleware{}
		m.BindMiddleware(mockMiddlewareFn{})
		m.BindMiddleware(mockMiddlewareFn{})
		m.BindMiddleware(mockMiddlewareFn{})
		InjectProvidersIntoHTTPMiddlewares(m, r, benchCB, testConstruct)
	}
}
