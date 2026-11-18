package log

import (
	"testing"
)

func BenchmarkWrapLogger_Info_TaggedStruct(b *testing.B) {
	l := WrapLogger(noopLogger{}, nil)
	p := benchTagPerson{Age: 10, name: "Joh", School: "MIT", policy: "ok"}
	b.ReportAllocs()
	for b.Loop() {
		l.Info("test", "person", p)
	}
}

func BenchmarkWrapLogger_Info_TaggedStruct_WithMaskRule(b *testing.B) {
	l := WrapLogger(noopLogger{}, []string{"person.name"})
	p := benchTagPerson{Age: 10, name: "Joh", School: "MIT", policy: "ok"}
	b.ReportAllocs()
	for b.Loop() {
		l.Info("test", "person", p)
	}
}

func BenchmarkWrapLogger_Info_NoStructArgs(b *testing.B) {
	l := WrapLogger(noopLogger{}, nil)
	args := []any{"url", "/api/v1/users", "status", 200}
	b.ReportAllocs()
	for b.Loop() {
		l.Info("test", args...)
	}
}

func BenchmarkLog_From_Info_JSON_Adapter(b *testing.B) {
	l := WrapLogger(noopLogger{}, nil)
	ctx := requestContext()
	scoped := l.From(ctx)
	b.ReportAllocs()
	for b.Loop() {
		scoped.Info("request", "code", 200, "ok", true)
	}
}
