package logpipe

import (
	"io"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkSink_Write_Async_1(b *testing.B) {
	s := New(io.Discard)
	s.StartAsync()
	defer s.StopAsync(time.Minute)
	record := benchRecord()
	var drops dropCounter
	var ops int64
	b.ReportAllocs()
	for b.Loop() {
		s.Write(Info, record, drops.report)
		ops++
	}
	s.Flush(time.Minute)
	drops.reportPerOp(b, ops)
}

func BenchmarkSink_Write_Async_12(b *testing.B) {
	s := New(io.Discard)
	s.StartAsync()
	defer s.StopAsync(time.Minute)
	record := benchRecord()
	b.SetParallelism((12 + runtime.GOMAXPROCS(0) - 1) / runtime.GOMAXPROCS(0))
	var drops dropCounter
	var total atomic.Int64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var local int64
		for pb.Next() {
			s.Write(Info, record, drops.report)
			local++
		}
		total.Add(local)
	})
	s.Flush(time.Minute)
	drops.reportPerOp(b, total.Load())
}

func BenchmarkSink_Write_Sync_1(b *testing.B) {
	s := New(io.Discard)
	record := benchRecord()
	b.ReportAllocs()
	for b.Loop() {
		s.Write(Info, record, quietReport)
	}
}

func BenchmarkSink_Write_Async_SlowOutput_1(b *testing.B) {
	s := New(slowWriter{delay: 2 * time.Millisecond})
	s.StartAsync()
	defer s.StopAsync(time.Minute)
	record := benchRecord()
	b.ReportAllocs()
	for b.Loop() {
		s.Write(Info, record, quietReport)
	}
}

func BenchmarkSink_Write_Async_SlowOutput_12(b *testing.B) {
	s := New(slowWriter{delay: 2 * time.Millisecond})
	s.StartAsync()
	defer s.StopAsync(time.Minute)
	record := benchRecord()
	b.SetParallelism((12 + runtime.GOMAXPROCS(0) - 1) / runtime.GOMAXPROCS(0))
	var total atomic.Int64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var local int64
		for pb.Next() {
			s.Write(Info, record, quietReport)
			local++
		}
		total.Add(local)
	})
}

func BenchmarkSink_Write_Async_Burst(b *testing.B) {
	s := New(io.Discard)
	s.StartAsync()
	defer s.StopAsync(time.Minute)
	record := benchRecord()
	b.ReportAllocs()
	for b.Loop() {
		for range 20 {
			s.Write(Info, record, quietReport)
		}
		s.Flush(time.Minute)
	}
}
