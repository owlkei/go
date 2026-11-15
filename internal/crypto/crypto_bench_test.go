package crypto

import (
	"sync/atomic"
	"testing"
)

func BenchmarkUUID(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = UUID()
	}
}

func BenchmarkUUID_Parallel(b *testing.B) {
	b.ReportAllocs()
	var total atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		n := 0
		for pb.Next() {
			id, _ := UUID()
			n += len(id)
		}
		total.Add(int64(n))
	})
}

func BenchmarkRandomHex(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		RandomHex(8)
	}
}

func BenchmarkRandomHex_Parallel(b *testing.B) {
	b.ReportAllocs()
	var total atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		n := 0
		for pb.Next() {
			n += len(RandomHex(8))
		}
		total.Add(int64(n))
	})
}

func BenchmarkAppendRandomHex(b *testing.B) {
	buf := make([]byte, 0, 16)
	b.ReportAllocs()
	for b.Loop() {
		buf = AppendRandomHex(buf[:0], 8)
	}
}
