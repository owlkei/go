package common

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/ctx"
)

func BenchmarkInjectProvidersIntoHTTPGuards_ApplyAll(b *testing.B) {
	r := buildBenchHTTP(20)
	b.ReportAllocs()
	for b.Loop() {
		g := &Guard{}
		g.BindGuard(mockGuarder{})
		g.BindGuard(mockGuarder{})
		g.BindGuard(mockGuarder{})
		g.InjectProvidersIntoHTTPGuards(r, benchCB, testConstruct)
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
