package ctx

import (
	"reflect"
	"testing"
)

func BenchmarkBindStruct(b *testing.B) {
	data := benchBindData()
	b.ReportAllocs()
	for b.Loop() {
		BindStruct(data, &[]FieldLevel{}, benchBindDTO{}, "", "")
	}
}

func BenchmarkBindStruct_PointerFields(b *testing.B) {
	data := map[string]any{"name": "John Doe", "age": float64(30)}
	b.ReportAllocs()
	for b.Loop() {
		BindStruct(data, &[]FieldLevel{}, benchPtrDTO{}, "", "")
	}
}

func BenchmarkBinder_Struct(b *testing.B) {
	data := benchBindData()
	binder, err := BinderOf(reflect.TypeOf(benchBindDTO{}))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		binder.Struct(data)
	}
}

func BenchmarkBody_Bind(b *testing.B) {
	body := Body(benchBindData())
	b.ReportAllocs()
	for b.Loop() {
		body.Bind(benchBindDTO{})
	}
}

func BenchmarkBody_BindValue(b *testing.B) {
	body := Body(benchBindData())
	b.ReportAllocs()
	for b.Loop() {
		body.BindValue(benchBindDTO{})
	}
}

func BenchmarkBinderOf_Warm(b *testing.B) {
	typ := reflect.TypeOf(benchBindDTO{})
	if _, err := BinderOf(typ); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = BinderOf(typ)
	}
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
