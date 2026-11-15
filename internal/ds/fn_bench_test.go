package ds

import (
	"testing"
)

func BenchmarkMatchGlob(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		matchGlob("index.html", "in*.html")
	}
}
