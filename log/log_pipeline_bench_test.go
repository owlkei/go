package log

import (
	"context"
	"io"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

type slowOutput struct{ delay time.Duration }

func (w slowOutput) Write(p []byte) (int, error) {
	time.Sleep(w.delay)
	return len(p), nil
}

func asyncBenchLogger(b *testing.B, out io.Writer) Logger {
	b.Helper()
	s := logpipe.New(out)
	s.StartAsync()
	b.Cleanup(func() { s.StopAsync(time.Minute) })
	return newLogSink(s, false, &LogOptions{Format: FormatJSON})
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

func BenchmarkLog_From_Info_JSON_Adapter(b *testing.B) {
	l := WrapLogger(noopLogger{}, nil)
	ctx := requestContext()
	scoped := l.From(ctx)
	b.ReportAllocs()
	for b.Loop() {
		scoped.Info("request", "code", 200, "ok", true)
	}
}
