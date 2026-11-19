package common

import (
	"testing"

	"go.owlky.com/owlky/internal/aggregation"
	"go.owlky.com/owlky/internal/ctx"
)

func BenchmarkInjectProvidersIntoWSInterceptors_ApplyAll(b *testing.B) {
	ws := buildBenchWS(20)
	b.ReportAllocs()
	for b.Loop() {
		ic := &Interceptor{}
		ic.BindInterceptor(mockWSInterceptable{})
		ic.BindInterceptor(mockWSInterceptable{})
		ic.BindInterceptor(mockWSInterceptable{})
		InjectProvidersIntoWSInterceptors(ic, ws, benchCB, testConstruct)
	}
}

func BenchmarkAsWSInterceptor(b *testing.B) {
	interceptable := mockWSInterceptable{}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = AsWSInterceptor(interceptable)
	}
}

func BenchmarkWSInterceptor_Call(b *testing.B) {
	fn, ok := AsWSInterceptor(mockWSInterceptable{})
	if !ok {
		b.Fatal("mockWSInterceptable must match WSIntercept")
	}
	c := ctx.NewWSContext()
	a := aggregation.NewAggregation()
	b.ReportAllocs()
	for b.Loop() {
		fn(c, a)
	}
}

func BenchmarkBuildWSInterceptMiddleware(b *testing.B) {
	c := ctx.NewWSContext()
	next := func() {}
	mw := BuildWSInterceptMiddleware("bench", func(*ctx.WSContext, *aggregation.Aggregation) any {
		return nil
	})
	b.ReportAllocs()
	for b.Loop() {
		ctx.WSReset(c)
		ctx.WSSetNext(c, next)
		mw(c)
	}
}
