package tracecontext

import (
	"strings"
	"testing"
)

func BenchmarkParseTraceparent(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ParseTraceparent(validHeader)
	}
}

func BenchmarkParseTraceparent_FutureVersionLong(b *testing.B) {
	value := "01-" + validTraceID + "-" + validParentID + "-01-" + strings.Repeat("x", 512)
	b.ReportAllocs()
	for b.Loop() {
		ParseTraceparent(value)
	}
}

func BenchmarkParseTraceparent_Invalid(b *testing.B) {
	value := strings.ToUpper(validHeader)
	b.ReportAllocs()
	for b.Loop() {
		ParseTraceparent(value)
	}
}

func BenchmarkParseTraceparent_OneMegabyte(b *testing.B) {
	value := strings.Repeat("a", 1<<20)
	b.ReportAllocs()
	for b.Loop() {
		ParseTraceparent(value)
	}
}
