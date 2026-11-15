package slice

import "testing"

var benchStrings = func() []string {
	s := make([]string, 1024)
	for i := range s {
		if i%2 == 0 {
			s[i] = "keep"
		} else {
			s[i] = "drop"
		}
	}
	return s
}()

var benchInts = func() []int {
	s := make([]int, 1024)
	for i := range s {
		s[i] = i
	}
	return s
}()

var benchNumStrs = func() []string {
	s := make([]string, 1024)
	for i := range s {
		s[i] = "42"
	}
	return s
}()

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
