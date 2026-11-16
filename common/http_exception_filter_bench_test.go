package common

import (
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
)

func BenchmarkInjectProvidersIntoHTTPExceptionFilters_ApplyAll(b *testing.B) {
	r := buildBenchHTTP(20)
	b.ReportAllocs()
	for b.Loop() {
		e := &ExceptionFilter{}
		e.BindExceptionFilter(mockExFilter{})
		e.BindExceptionFilter(mockExFilter{})
		e.BindExceptionFilter(mockExFilter{})
		e.InjectProvidersIntoHTTPExceptionFilters(r, benchCB)
	}
}

func BenchmarkAsHTTPExceptionFilter(b *testing.B) {
	exceptionFilterable := mockExFilter{}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = AsHTTPExceptionFilter(exceptionFilterable)
	}
}

func BenchmarkHTTPExceptionFilter_Call(b *testing.B) {
	fn, ok := AsHTTPExceptionFilter(mockExFilter{})
	if !ok {
		b.Fatal("mockExFilter must match HTTPCatch")
	}
	c := ctx.NewHTTPContext()
	ex := exception.NotFoundException("nope")
	b.ReportAllocs()
	for b.Loop() {
		fn(c, &ex)
	}
}
