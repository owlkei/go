package ctx

import (
	"testing"
)

func BenchmarkGetTagParams_Single(b *testing.B) {
	v := "field_name"
	b.ReportAllocs()
	for b.Loop() {
		GetTagParams(v)
	}
}

func BenchmarkGetTagParams_Multiple(b *testing.B) {
	v := "field_name, required, min=1, max=100"
	b.ReportAllocs()
	for b.Loop() {
		GetTagParams(v)
	}
}

func BenchmarkGetTagParamIndex_WithDot(b *testing.B) {
	v := "integers_1.3"
	b.ReportAllocs()
	for b.Loop() {
		GetTagParamIndex(v)
	}
}

func BenchmarkGetTagParamIndex_NoDot(b *testing.B) {
	v := "field_name"
	b.ReportAllocs()
	for b.Loop() {
		GetTagParamIndex(v)
	}
}
