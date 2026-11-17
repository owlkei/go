package trace

import (
	"sync/atomic"
	"testing"
)

func BenchmarkTracer_Enabled(b *testing.B) {
	var tr Tracer
	tr.Listen(func(Event) {})
	b.ReportAllocs()
	for b.Loop() {
		_ = tr.Enabled()
	}
}

func BenchmarkTracer_Emit(b *testing.B) {
	var tr Tracer
	var n int
	tr.Listen(func(e Event) { n += e.Code })
	e := Event{ID: "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0", Stage: StageHandler, Name: "READ_users", Transport: TransportHTTP, Duration: 1000}
	b.ReportAllocs()
	for b.Loop() {
		tr.Emit(e)
	}
}

func BenchmarkTracer_Emit_Parallel(b *testing.B) {
	var tr Tracer
	var total atomic.Int64
	tr.Listen(func(e Event) { _ = e.Code })
	e := Event{ID: "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0", Stage: StageHandler, Name: "READ_users", Transport: TransportHTTP, Duration: 1000}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var local int64
		for pb.Next() {
			tr.Emit(e)
			local++
		}
		total.Add(local)
	})
}
