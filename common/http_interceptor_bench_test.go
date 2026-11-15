package common

import (
	"testing"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/ctx"
)

func BenchmarkInjectProvidersIntoHTTPInterceptors_ApplyAll(b *testing.B) {
	r := buildBenchHTTP(20)
	b.ReportAllocs()
	for b.Loop() {
		ic := &Interceptor{}
		ic.BindInterceptor(mockInterceptable{})
		ic.BindInterceptor(mockInterceptable{})
		ic.BindInterceptor(mockInterceptable{})
		ic.InjectProvidersIntoHTTPInterceptors(r, benchCB)
	}
}

func BenchmarkAsHTTPInterceptor(b *testing.B) {
	interceptable := mockInterceptable{}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = AsHTTPInterceptor(interceptable)
	}
}

func BenchmarkHTTPInterceptor_Call(b *testing.B) {
	fn, ok := AsHTTPInterceptor(mockInterceptable{})
	if !ok {
		b.Fatal("mockInterceptable must match HTTPIntercept")
	}
	c := ctx.NewHTTPContext()
	a := aggregation.NewAggregation()
	b.ReportAllocs()
	for b.Loop() {
		fn(c, a)
	}
}

func BenchmarkBuildHTTPInterceptMiddleware(b *testing.B) {
	c := ctx.NewHTTPContext()
	next := func() {}
	mw := BuildHTTPInterceptMiddleware("bench", func(*ctx.HTTPContext, *aggregation.Aggregation) any {
		return nil
	})
	b.ReportAllocs()
	for b.Loop() {
		c.Reset()
		c.Next = next
		mw(c)
	}
}
