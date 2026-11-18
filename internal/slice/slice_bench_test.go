package slice

import (
	"testing"
)

func BenchmarkMap(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		Map(benchInts, func(el int, _ int) int { return el * 2 })
	}
}

func BenchmarkFilter(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		Filter(benchStrings, func(el string, _ int) bool { return el == "keep" })
	}
}

func BenchmarkStrParseInt(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		StrParseInt(benchNumStrs)
	}
}

func BenchmarkStrParseInt_One(b *testing.B) {
	one := []string{"42"}
	b.ReportAllocs()
	for b.Loop() {
		StrParseInt(one)
	}
}
