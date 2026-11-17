package log

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type benchUser struct {
	Name     string
	Password string
	Age      int
	City     string
}

type benchTagPerson struct {
	Age    int    `log:"age"`
	name   string `log:"name"`
	School string
	policy string
}

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}
func (noopLogger) Fatal(string, ...any) {}

func BenchmarkLog_Info_Scalars_JSON(b *testing.B) {
	logger := newLog(io.Discard, false, &LogOptions{Format: FormatJSON})
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("request", "url", "/api/v1/users", "status", 200, "ok", true, "took", 1.5)
	}
}

func BenchmarkLog_Info_Struct_JSON(b *testing.B) {
	logger := newLog(io.Discard, false, &LogOptions{Format: FormatJSON})
	user := benchUser{Name: "duoc", Password: "secret", Age: 30, City: "Hanoi"}
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("login", "user", user)
	}
}

func BenchmarkLog_Info_Struct_JSON_WithMaskRule(b *testing.B) {
	logger := newLog(io.Discard, false, &LogOptions{Format: FormatJSON, MaskFields: []string{"password"}})
	user := benchUser{Name: "duoc", Password: "secret", Age: 30, City: "Hanoi"}
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("login", "user", user)
	}
}

func BenchmarkLog_BelowLevel(b *testing.B) {
	logger := newLog(io.Discard, false, &LogOptions{Format: FormatJSON, Level: LevelInfo})
	user := benchUser{Name: "duoc", Password: "secret", Age: 30, City: "Hanoi"}
	args := []any{"user", user}
	b.ReportAllocs()
	for b.Loop() {
		logger.Debug("x", args...)
	}
}

func BenchmarkLog_ErrorWithStack_JSON(b *testing.B) {
	logger := newLog(io.Discard, false, &LogOptions{Format: FormatJSON})
	stack := strings.Repeat("goroutine 1 [running]:\nmain.handler(0x1)\n\t/app/main.go:42 +0x1d\n", 20)
	err := errors.New("boom")
	b.ReportAllocs()
	for b.Loop() {
		logger.Error("UnhandledPanic", "id", "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0", "method", "GET", "path", "/api/v1/users", "panic", err, "stack", stack)
	}
}

func BenchmarkLog_AccessLine_Pretty(b *testing.B) {
	logger := newLog(io.Discard, false, &LogOptions{Format: FormatPretty})
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("/api/v1/users",
			"trace_id", "4bf92f3577b34da6a3ce929d0e0e4736",
			"request", "0f1e2d3c4b5a6978",
			"transport", "HTTP",
			"code", 200,
			"operation", "GET",
			"middleware", "CORS 120µs (4%), CSRF 80µs (3%), Helmet 40µs (1%)",
			"handler", "READ_users 2ms (68%)",
			"total", "2.9ms",
		)
	}
}

func BenchmarkLog_Info_LargeMap_Pretty(b *testing.B) {
	logger := newLog(io.Discard, false, &LogOptions{Format: FormatPretty})
	meta := make(map[string]int, 20)
	for i := range 20 {
		meta[string(rune('a'+i%26))+string(rune('A'+i))] = i
	}
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("meta", "meta", meta)
	}
}

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
