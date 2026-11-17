package ctx

import (
	"reflect"
	"testing"
)

type benchStrArrDTO struct {
	Bool1   bool    `bind:"bool_1"`
	String1 string  `bind:"string_1"`
	Int1    int     `bind:"int_1"`
	Int2    int8    `bind:"ints_1.1"`
	Uint1   uint    `bind:"uint_1"`
	Float1  float64 `bind:"float_1"`
}

func benchStrArrData() map[string][]string {
	return map[string][]string{
		"bool_1":   {"true"},
		"string_1": {"hello"},
		"int_1":    {"42"},
		"ints_1":   {"0", "7"},
		"uint_1":   {"99"},
		"float_1":  {"3.14"},
	}
}

func BenchmarkBindStrArr(b *testing.B) {
	d := benchStrArrData()
	var fls []FieldLevel
	s := benchStrArrDTO{}
	b.ReportAllocs()
	for b.Loop() {
		fls = fls[:0]
		BindStrArr(d, &fls, s)
	}
}

func BenchmarkBinder_StrArr(b *testing.B) {
	d := benchStrArrData()
	binder, err := BinderOf(reflect.TypeOf(benchStrArrDTO{}))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		binder.StrArr(d)
	}
}
