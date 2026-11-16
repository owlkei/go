package tracecontext

import (
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkNewSpanID(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		NewSpanID()
	}
}

func BenchmarkNewSpanID_Parallel(b *testing.B) {
	b.ReportAllocs()
	var total atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		n := 0
		for pb.Next() {
			n += len(NewSpanID())
		}
		total.Add(int64(n))
	})
}

func BenchmarkNewTraceID(b *testing.B) {
	now := time.Now()
	b.ReportAllocs()
	for b.Loop() {
		NewTraceID(now)
	}
}

func BenchmarkNewTraceID_Parallel(b *testing.B) {
	now := time.Now()
	b.ReportAllocs()
	var total atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		n := 0
		for pb.Next() {
			n += len(NewTraceID(now))
		}
		total.Add(int64(n))
	})
}
