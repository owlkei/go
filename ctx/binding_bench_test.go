package ctx

import (
	"reflect"
	"testing"
)

type benchBindDTO struct {
	Name    string       `bind:"name"`
	Age     int          `bind:"age"`
	Active  bool         `bind:"active"`
	Tags    []string     `bind:"tags"`
	Address benchAddrDTO `bind:"address"`
}

type benchAddrDTO struct {
	Street string `bind:"street"`
	City   string `bind:"city"`
}

func benchBindData() map[string]any {
	return map[string]any{
		"name":   "John Doe",
		"age":    float64(30),
		"active": true,
		"tags":   []any{"a", "b", "c"},
		"address": map[string]any{
			"street": "Main St",
			"city":   "Metropolis",
		},
	}
}

func BenchmarkBindStruct(b *testing.B) {
	data := benchBindData()
	b.ReportAllocs()
	for b.Loop() {
		BindStruct(data, &[]FieldLevel{}, benchBindDTO{}, "", "")
	}
}

type benchPtrDTO struct {
	Name *string `bind:"name"`
	Age  *int    `bind:"age"`
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
