package logpipe

import (
	"io"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

type slowWriter struct{ delay time.Duration }

func (w slowWriter) Write(p []byte) (int, error) {
	time.Sleep(w.delay)
	return len(p), nil
}

func benchRecord() []byte {
	return []byte(`{"time":"2026-11-16T10:00:00.123456789Z","level":"INFO","msg":"request","id":"0f1e2d3c4b5a6978","code":200,"ok":true}` + "\n")
}

type dropCounter struct{ n atomic.Uint64 }

func (d *dropCounter) report(dropped [4]uint64) []byte {
	d.n.Add(dropped[0] + dropped[1] + dropped[2] + dropped[3])
	return nil
}

func (d *dropCounter) reportPerOp(b *testing.B, ops int64) {
	if ops > 0 {
		b.ReportMetric(float64(d.n.Load())/float64(ops), "dropped/op")
	}
}

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
