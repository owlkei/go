package common

import (
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
)

func BenchmarkInjectProvidersIntoWSExceptionFilters_ApplyAll(b *testing.B) {
	ws := buildBenchWS(20)
	b.ReportAllocs()
	for b.Loop() {
		e := &ExceptionFilter{}
		e.BindExceptionFilter(mockWSExFilter{})
		e.BindExceptionFilter(mockWSExFilter{})
		e.BindExceptionFilter(mockWSExFilter{})
		e.InjectProvidersIntoWSExceptionFilters(ws, benchCB)
	}
}

func BenchmarkAsWSExceptionFilter(b *testing.B) {
	exceptionFilterable := mockWSExFilter{}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = AsWSExceptionFilter(exceptionFilterable)
	}
}

func BenchmarkWSExceptionFilter_Call(b *testing.B) {
	fn, ok := AsWSExceptionFilter(mockWSExFilter{})
	if !ok {
		b.Fatal("mockWSExFilter must match WSCatch")
	}
	c := ctx.NewWSContext()
	ex := exception.NotFoundException("nope")
	b.ReportAllocs()
	for b.Loop() {
		fn(c, &ex)
	}
}

func BenchmarkRunWSCatchChain(b *testing.B) {
	c := ctx.NewWSContext()
	catchFns := []WSCatch{func(*ctx.WSContext, *exception.Exception) {}}
	ex := exception.NotFoundException("nope")
	b.ReportAllocs()
	for b.Loop() {
		RunWSCatchChain(c, catchFns, ex)
	}
}
