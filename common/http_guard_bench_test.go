package common

import (
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
)

func BenchmarkInjectProvidersIntoHTTPGuards_ApplyAll(b *testing.B) {
	r := buildBenchHTTP(20)
	b.ReportAllocs()
	for b.Loop() {
		g := &Guard{}
		g.BindGuard(mockGuarder{})
		g.BindGuard(mockGuarder{})
		g.BindGuard(mockGuarder{})
		g.InjectProvidersIntoHTTPGuards(r, benchCB)
	}
}

func BenchmarkAsHTTPGuard(b *testing.B) {
	guarder := mockGuarder{}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = AsHTTPGuard(guarder)
	}
}

func BenchmarkHTTPGuard_Call(b *testing.B) {
	fn, ok := AsHTTPGuard(mockGuarder{})
	if !ok {
		b.Fatal("mockGuarder must match HTTPCanActivate")
	}
	c := ctx.NewHTTPContext()
	b.ReportAllocs()
	for b.Loop() {
		fn(c)
	}
}

func BenchmarkBuildHTTPGuardMiddleware_Allow(b *testing.B) {
	c := ctx.NewHTTPContext()
	c.Next = func() {}
	mw := BuildHTTPGuardMiddleware(func(*ctx.HTTPContext) bool { return true })
	b.ReportAllocs()
	for b.Loop() {
		mw(c)
	}
}

func BenchmarkBuildHTTPGuardMiddleware_Reject(b *testing.B) {
	c := ctx.NewHTTPContext()
	c.Next = func() {}
	mw := BuildHTTPGuardMiddleware(func(*ctx.HTTPContext) bool { return false })
	catchFns := []HTTPCatch{func(*ctx.HTTPContext, *exception.Exception) {}}
	reject := func() {
		defer func() {
			if rec := recover(); rec != nil {
				RunHTTPCatchChain(c, catchFns, rec)
			}
		}()
		mw(c)
	}
	b.ReportAllocs()
	for b.Loop() {
		reject()
	}
}
