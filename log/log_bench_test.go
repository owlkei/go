package log

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.owlkei.com/owlkei/internal/logpipe"
	"go.owlkei.com/owlkei/internal/tracecontext"
)

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

func BenchmarkLog_Info_Scalars_JSON_Async_1(b *testing.B) {
	l := asyncBenchLogger(b, io.Discard)
	b.ReportAllocs()
	for b.Loop() {
		l.Info("request", "id", "0f1e2d3c4b5a6978", "code", 200, "ok", true, "took", 1.5)
	}
}

func BenchmarkLog_Info_Scalars_JSON_Async_12(b *testing.B) {
	l := asyncBenchLogger(b, io.Discard)
	b.SetParallelism((12 + runtime.GOMAXPROCS(0) - 1) / runtime.GOMAXPROCS(0))
	var total atomic.Int64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var local int64
		for pb.Next() {
			l.Info("request", "id", "0f1e2d3c4b5a6978", "code", 200, "ok", true, "took", 1.5)
			local++
		}
		total.Add(local)
	})
}

func BenchmarkLog_Info_Scalars_JSON_Sync_1(b *testing.B) {
	l := newLogSink(logpipe.New(io.Discard), false, &LogOptions{Format: FormatJSON})
	b.ReportAllocs()
	for b.Loop() {
		l.Info("request", "id", "0f1e2d3c4b5a6978", "code", 200, "ok", true, "took", 1.5)
	}
}

func BenchmarkLog_Info_SlowOutput_1(b *testing.B) {
	l := asyncBenchLogger(b, slowOutput{delay: 2 * time.Millisecond})
	b.ReportAllocs()
	for b.Loop() {
		l.Info("request", "id", "0f1e2d3c4b5a6978", "code", 200, "ok", true, "took", 1.5)
	}
}

func BenchmarkLog_Info_SlowOutput_12(b *testing.B) {
	l := asyncBenchLogger(b, slowOutput{delay: 2 * time.Millisecond})
	b.SetParallelism((12 + runtime.GOMAXPROCS(0) - 1) / runtime.GOMAXPROCS(0))
	var total atomic.Int64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var local int64
		for pb.Next() {
			l.Info("request", "id", "0f1e2d3c4b5a6978", "code", 200, "ok", true, "took", 1.5)
			local++
		}
		total.Add(local)
	})
}

func BenchmarkFrom_NoScope(b *testing.B) {
	l := newLog(io.Discard, false, &LogOptions{Format: FormatJSON})
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		l.From(ctx)
	}
}

func BenchmarkFrom_First(b *testing.B) {
	l := newLog(io.Discard, false, &LogOptions{Format: FormatJSON})
	b.ReportAllocs()
	for b.Loop() {
		ctx := tracecontext.With(context.Background(), &tracecontext.Scope{TraceID: fromTraceID, Request: fromRequest})
		l.From(ctx)
	}
}

func BenchmarkFrom_Cached(b *testing.B) {
	l := newLog(io.Discard, false, &LogOptions{Format: FormatJSON})
	ctx := requestContext()
	l.From(ctx)
	b.ReportAllocs()
	for b.Loop() {
		l.From(ctx)
	}
}

func BenchmarkLog_From_Info_JSON(b *testing.B) {
	l := newLog(io.Discard, false, &LogOptions{Format: FormatJSON})
	ctx := requestContext()
	scoped := l.From(ctx)
	b.ReportAllocs()
	for b.Loop() {
		scoped.Info("request", "code", 200, "ok", true)
	}
}
