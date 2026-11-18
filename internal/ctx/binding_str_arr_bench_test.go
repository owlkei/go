package ctx

import (
	"reflect"
	"testing"
)

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
