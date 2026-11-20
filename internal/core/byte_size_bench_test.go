package core

import "testing"

func BenchmarkByteSize_String(b *testing.B) {
	sizes := []ByteSize{0, 1500, 10 * MiB, 3 * GB, 1024000}
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range sizes {
			_ = s.String()
		}
	}
}
