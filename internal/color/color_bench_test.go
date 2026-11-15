package color

import "testing"

func BenchmarkFmtGreen(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		FmtGreen("status: %s %d", "ok", 200)
	}
}

func BenchmarkFmtRed(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		FmtRed("error: %s", "something went wrong")
	}
}

func BenchmarkFmtBGDim(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		FmtBGDim("label: %s", "info")
	}
}

func BenchmarkFmtPurple(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		FmtPurple("value: %d", 42)
	}
}
