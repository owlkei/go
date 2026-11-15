package num

import (
	"reflect"
	"testing"
)

func BenchmarkF64ToAnyNum_Int(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		F64ToAnyNum(42.9, reflect.Int)
	}
}

func BenchmarkF64ToAnyNum_Uint(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		F64ToAnyNum(42.9, reflect.Uint)
	}
}

func BenchmarkF64ToAnyNum_UintNeg(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		F64ToAnyNum(-1, reflect.Uint)
	}
}

func BenchmarkF64ToAnyNum_Float64(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		F64ToAnyNum(42.5, reflect.Float64)
	}
}

func BenchmarkF64ToAnyNum_Saturated(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		F64ToAnyNum(1e20, reflect.Int8)
	}
}
