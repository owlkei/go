package common

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/ctx"
)

func BenchmarkInjectProvidersIntoWSGuards_ApplyAll(b *testing.B) {
	ws := buildBenchWS(20)
	b.ReportAllocs()
	for b.Loop() {
		g := &Guard{}
		g.BindGuard(mockWSGuarder{})
		g.BindGuard(mockWSGuarder{})
		g.BindGuard(mockWSGuarder{})
		g.InjectProvidersIntoWSGuards(ws, benchCB, testConstruct)
	}
}

func BenchmarkAsWSGuard(b *testing.B) {
	guarder := mockWSGuarder{}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = AsWSGuard(guarder)
	}
}

func BenchmarkWSGuard_Call(b *testing.B) {
	fn, ok := AsWSGuard(mockWSGuarder{})
	if !ok {
		b.Fatal("mockWSGuarder must match WSCanActivate")
	}
	c := ctx.NewWSContext()
	b.ReportAllocs()
	for b.Loop() {
		fn(c)
	}
}

func BenchmarkBuildWSGuardMiddleware_Allow(b *testing.B) {
	c := ctx.NewWSContext()
	c.Next = func() {}
	mw := BuildWSGuardMiddleware(func(*ctx.WSContext) bool { return true })
	b.ReportAllocs()
	for b.Loop() {
		mw(c)
	}
}
