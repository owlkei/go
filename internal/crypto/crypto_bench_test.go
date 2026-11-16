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
