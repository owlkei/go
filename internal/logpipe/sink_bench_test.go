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

func BenchmarkSink_Write_Async_1(b *testing.B) {
	s := New(io.Discard)
	s.StartAsync()
	defer s.StopAsync(time.Minute)
	record := benchRecord()
	b.ReportAllocs()
	for b.Loop() {
		s.Write(Info, record, quietReport)
	}
	s.Flush(time.Minute)
}

func BenchmarkSink_Write_Async_12(b *testing.B) {
	s := New(io.Discard)
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
	s.Flush(time.Minute)
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
