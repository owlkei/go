package accesslog

import (
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"go.owlkei.com/owlkei/internal/trace"
	"go.owlkei.com/owlkei/log"
)

func BenchmarkAccessLog_Request_1Stage(b *testing.B) {
	tr := &trace.Tracer{}
	al := NewAccessLog(&AccessLogOptions{Tracer: tr, Logger: &discardLogger{}})
	defer al.Stop()
	events := benchStages("0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0", 1)
	b.ReportAllocs()
	for b.Loop() {
		for _, e := range events {
			tr.Emit(e)
		}
	}
}

func BenchmarkAccessLog_Request_6Stages(b *testing.B) {
	tr := &trace.Tracer{}
	al := NewAccessLog(&AccessLogOptions{Tracer: tr, Logger: &discardLogger{}})
	defer al.Stop()
	events := benchStages("0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0", 6)
	b.ReportAllocs()
	for b.Loop() {
		for _, e := range events {
			tr.Emit(e)
		}
	}
}

func BenchmarkAccessLog_Request_6Stages_Parallel(b *testing.B) {
	tr := &trace.Tracer{}
	al := NewAccessLog(&AccessLogOptions{Tracer: tr, Logger: &discardLogger{}})
	defer al.Stop()
	var next atomic.Uint64
	var total atomic.Int64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var local int64
		for pb.Next() {
			id := strconv.FormatUint(next.Add(1)*0x9E3779B97F4A7C15, 16)
			for _, e := range benchStages(id, 6) {
				tr.Emit(e)
			}
			local++
		}
		total.Add(local)
	})
}

func BenchmarkAccessLog_Line_6Stages(b *testing.B) {
	benchBuiltinLine(b, &log.LogOptions{Format: log.FormatJSON})
}

func BenchmarkAccessLog_Line_6Stages_Pretty(b *testing.B) {
	benchBuiltinLine(b, &log.LogOptions{Format: log.FormatPretty})
}

func BenchmarkFormatDuration(b *testing.B) {
	durations := []time.Duration{
		500 * time.Nanosecond,
		1234 * time.Nanosecond,
		1234567 * time.Nanosecond,
		1234567891 * time.Nanosecond,
	}
	b.ReportAllocs()
	var i int
	for b.Loop() {
		formatDuration(durations[i%len(durations)])
		i++
	}
}

func BenchmarkColorForDuration(b *testing.B) {
	durations := []time.Duration{
		5 * time.Millisecond,
		40 * time.Millisecond,
		90 * time.Millisecond,
		250 * time.Millisecond,
		700 * time.Millisecond,
		2 * time.Second,
	}
	b.ReportAllocs()
	var i int
	for b.Loop() {
		colorForDuration(durations[i%len(durations)])
		i++
	}
}

func BenchmarkShortName(b *testing.B) {
	names := []string{"cors.CORS", "csrf.CSRF", "READ_users", "pkg.sub.Type"}
	b.ReportAllocs()
	var i int
	for b.Loop() {
		shortName(names[i%len(names)])
		i++
	}
}
