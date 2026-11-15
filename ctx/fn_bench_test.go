package ctx

import "testing"

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

type benchSliceIntDTO struct {
	V []int `bind:"v"`
}

type benchSliceStructDTO struct {
	V []fnArrStructDTO `bind:"v"`
}

type benchMapIntDTO struct {
	V map[string]int `bind:"v"`
}

type benchMapStructDTO struct {
	V map[string]fnArrStructDTO `bind:"v"`
}

func BenchmarkBodyBind_SliceInt(b *testing.B) {
	body := Body{"v": []any{float64(1), float64(2), float64(3), float64(4), float64(5)}}
	b.ReportAllocs()
	for b.Loop() {
		body.Bind(benchSliceIntDTO{})
	}
}

func BenchmarkBodyBind_SliceStruct(b *testing.B) {
	body := Body{"v": []any{
		map[string]any{"name": "joe"},
		map[string]any{"name": "ana"},
	}}
	b.ReportAllocs()
	for b.Loop() {
		body.Bind(benchSliceStructDTO{})
	}
}

func BenchmarkBodyBind_MapInt(b *testing.B) {
	body := Body{"v": map[string]any{"a": float64(1), "b": float64(2), "c": float64(3)}}
	b.ReportAllocs()
	for b.Loop() {
		body.Bind(benchMapIntDTO{})
	}
}

func BenchmarkBodyBind_MapStruct(b *testing.B) {
	body := Body{"v": map[string]any{"a": map[string]any{"name": "joe"}}}
	b.ReportAllocs()
	for b.Loop() {
		body.Bind(benchMapStructDTO{})
	}
}
