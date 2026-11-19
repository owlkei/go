package num

import (
	"math"
	"reflect"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func TestF64ToAnyNum_SignedInRange(t *testing.T) {
	cases := []struct {
		kind reflect.Kind
		want any
	}{
		{reflect.Int, int(42)},
		{reflect.Int8, int8(42)},
		{reflect.Int16, int16(42)},
		{reflect.Int32, int32(42)},
		{reflect.Int64, int64(42)},
	}
	for _, c := range cases {
		got := F64ToAnyNum(42, c.kind)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "N1: in range "+c.kind.String()))
		}
	}
}

func TestF64ToAnyNum_SignedSaturatesHigh(t *testing.T) {
	cases := []struct {
		in   float64
		kind reflect.Kind
		want any
	}{
		{300, reflect.Int8, int8(127)},
		{70000, reflect.Int16, int16(32767)},
		{5e9, reflect.Int32, int32(2147483647)},
		{1e20, reflect.Int, math.MaxInt},
		{1e20, reflect.Int64, int64(math.MaxInt64)},
		{9223372036854775808, reflect.Int64, int64(math.MaxInt64)},
		{math.Inf(1), reflect.Int32, int32(math.MaxInt32)},
	}
	for _, c := range cases {
		got := F64ToAnyNum(c.in, c.kind)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "N1: high saturation "+c.kind.String()))
		}
	}
}

func TestF64ToAnyNum_SignedSaturatesLow(t *testing.T) {
	cases := []struct {
		in   float64
		kind reflect.Kind
		want any
	}{
		{-129, reflect.Int8, int8(-128)},
		{-70000, reflect.Int16, int16(-32768)},
		{-5e9, reflect.Int32, int32(-2147483648)},
		{-1e20, reflect.Int, math.MinInt},
		{-1e20, reflect.Int64, int64(math.MinInt64)},
		{math.Inf(-1), reflect.Int8, int8(-128)},
	}
	for _, c := range cases {
		got := F64ToAnyNum(c.in, c.kind)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "N1: low saturation "+c.kind.String()))
		}
	}
}

func TestF64ToAnyNum_SignedNaNIsZero(t *testing.T) {
	for _, kind := range signedKinds {
		got := F64ToAnyNum(math.NaN(), kind)
		want := reflect.Zero(reflectType(kind)).Interface()
		if got != want {
			t.Error(test.DiffMessage(got, want, "N1: NaN "+kind.String()))
		}
	}
}

func TestF64ToAnyNum_SignedTruncates(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{{1.9, 1}, {-1.9, -1}, {0.99, 0}, {-0.99, 0}}
	for _, c := range cases {
		got := F64ToAnyNum(c.in, reflect.Int)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "N1: truncation toward zero"))
		}
		got8 := F64ToAnyNum(c.in, reflect.Int8)
		if got8 != int8(c.want) {
			t.Error(test.DiffMessage(got8, int8(c.want), "N1: truncation toward zero int8"))
		}
	}
}

func TestF64ToAnyNum_ReturnsKindType(t *testing.T) {
	kinds := append(append([]reflect.Kind{}, signedKinds...), unsignedKinds...)
	kinds = append(kinds, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128)
	for _, kind := range kinds {
		for _, in := range []float64{0, 42, 1e300, -1e300} {
			got := F64ToAnyNum(in, kind)
			if reflect.TypeOf(got).Kind() != kind {
				t.Error(test.DiffMessage(reflect.TypeOf(got).Kind(), kind, "N1: dynamic type"))
			}
		}
	}
}

func TestF64ToAnyNum_UnsignedInRange(t *testing.T) {
	cases := []struct {
		kind reflect.Kind
		want any
	}{
		{reflect.Uint, uint(42)},
		{reflect.Uint8, uint8(42)},
		{reflect.Uint16, uint16(42)},
		{reflect.Uint32, uint32(42)},
		{reflect.Uint64, uint64(42)},
	}
	for _, c := range cases {
		got := F64ToAnyNum(42.9, c.kind)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "N2: in range "+c.kind.String()))
		}
	}
}

func TestF64ToAnyNum_UnsignedNegativeIsZero(t *testing.T) {
	for _, kind := range unsignedKinds {
		for _, in := range []float64{-1, 0, math.Inf(-1)} {
			got := F64ToAnyNum(in, kind)
			want := reflect.Zero(reflectType(kind)).Interface()
			if got != want {
				t.Error(test.DiffMessage(got, want, "N2: non-positive "+kind.String()))
			}
		}
	}
}

func TestF64ToAnyNum_UnsignedSaturatesHigh(t *testing.T) {
	cases := []struct {
		in   float64
		kind reflect.Kind
		want any
	}{
		{256, reflect.Uint8, uint8(255)},
		{70000, reflect.Uint16, uint16(65535)},
		{5e9, reflect.Uint32, uint32(4294967295)},
		{1e30, reflect.Uint64, uint64(math.MaxUint64)},
		{1e30, reflect.Uint, uint(math.MaxUint)},
		{18446744073709551616, reflect.Uint64, uint64(math.MaxUint64)},
		{math.Inf(1), reflect.Uint8, uint8(255)},
	}
	for _, c := range cases {
		got := F64ToAnyNum(c.in, c.kind)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "N2: high saturation "+c.kind.String()))
		}
	}
}

func TestF64ToAnyNum_UnsignedNaNIsZero(t *testing.T) {
	for _, kind := range unsignedKinds {
		got := F64ToAnyNum(math.NaN(), kind)
		want := reflect.Zero(reflectType(kind)).Interface()
		if got != want {
			t.Error(test.DiffMessage(got, want, "N2: NaN "+kind.String()))
		}
	}
}

func TestF64ToAnyNum_Float32Saturates(t *testing.T) {
	if got := F64ToAnyNum(1e300, reflect.Float32); got != float32(math.MaxFloat32) {
		t.Error(test.DiffMessage(got, float32(math.MaxFloat32), "N3: float32 high"))
	}
	if got := F64ToAnyNum(-1e300, reflect.Float32); got != float32(-math.MaxFloat32) {
		t.Error(test.DiffMessage(got, float32(-math.MaxFloat32), "N3: float32 low"))
	}
	if got := F64ToAnyNum(42.5, reflect.Float32); got != float32(42.5) {
		t.Error(test.DiffMessage(got, float32(42.5), "N3: float32 in range"))
	}
	nan, ok := F64ToAnyNum(math.NaN(), reflect.Float32).(float32)
	if !ok || !math.IsNaN(float64(nan)) {
		t.Error(test.DiffMessage(nan, "NaN", "N3: float32 NaN stays NaN"))
	}
}

func TestF64ToAnyNum_Float64Unchanged(t *testing.T) {
	for _, in := range []float64{0, 42.5, -1e300, 1e300} {
		if got := F64ToAnyNum(in, reflect.Float64); got != in {
			t.Error(test.DiffMessage(got, in, "N3: float64"))
		}
	}
}

func TestF64ToAnyNum_Complex(t *testing.T) {
	if got := F64ToAnyNum(42, reflect.Complex128); got != complex(42.0, 0) {
		t.Error(test.DiffMessage(got, complex(42.0, 0), "N3: complex128"))
	}
	if got := F64ToAnyNum(1e300, reflect.Complex128); got != complex(1e300, 0) {
		t.Error(test.DiffMessage(got, complex(1e300, 0), "N3: complex128 large"))
	}
	if got := F64ToAnyNum(42, reflect.Complex64); got != complex64(complex(42.0, 0)) {
		t.Error(test.DiffMessage(got, complex64(complex(42.0, 0)), "N3: complex64"))
	}
	wantHigh := complex(float32(math.MaxFloat32), 0)
	if got := F64ToAnyNum(1e300, reflect.Complex64); got != wantHigh {
		t.Error(test.DiffMessage(got, wantHigh, "N3: complex64 high"))
	}
	wantLow := complex(float32(-math.MaxFloat32), 0)
	if got := F64ToAnyNum(-1e300, reflect.Complex64); got != wantLow {
		t.Error(test.DiffMessage(got, wantLow, "N3: complex64 low"))
	}
}

func TestF64ToAnyNum_UnknownKindIsZero(t *testing.T) {
	for _, kind := range []reflect.Kind{reflect.String, reflect.Bool, reflect.Invalid, reflect.Slice} {
		got := F64ToAnyNum(1, kind)
		if got != 0 {
			t.Error(test.DiffMessage(got, 0, "N4: unknown kind "+kind.String()))
		}
	}
}

func TestF64ToAnyNumOK_FitReportsTrue(t *testing.T) {
	cases := []struct {
		in   float64
		kind reflect.Kind
		want any
	}{
		{42, reflect.Int, int(42)},
		{127, reflect.Int8, int8(127)},
		{-128, reflect.Int8, int8(-128)},
		{2147483647, reflect.Int32, int32(2147483647)},
		{-2147483648, reflect.Int32, int32(-2147483648)},
		{255, reflect.Uint8, uint8(255)},
		{0, reflect.Uint8, uint8(0)},
		{4294967295, reflect.Uint32, uint32(4294967295)},
		{42.5, reflect.Float32, float32(42.5)},
		{1e300, reflect.Float64, float64(1e300)},
		{42, reflect.Complex128, complex(42.0, 0)},
	}
	for _, c := range cases {
		got, ok := F64ToAnyNumOK(c.in, c.kind)
		if got != c.want || !ok {
			t.Error(test.DiffMessage([]any{got, ok}, []any{c.want, true}, "Q3: fits "+c.kind.String()))
		}
	}
}

func TestF64ToAnyNumOK_OverflowReportsFalseWithSaturatedValue(t *testing.T) {
	cases := []struct {
		in   float64
		kind reflect.Kind
		want any
	}{
		{128, reflect.Int8, int8(127)},
		{-129, reflect.Int8, int8(-128)},
		{2147483648, reflect.Int32, int32(2147483647)},
		{-2147483649, reflect.Int32, int32(-2147483648)},
		{1e20, reflect.Int, math.MaxInt},
		{-1e20, reflect.Int64, int64(math.MinInt64)},
		{9223372036854775808, reflect.Int64, int64(math.MaxInt64)},
		{256, reflect.Uint8, uint8(255)},
		{5e9, reflect.Uint32, uint32(4294967295)},
		{18446744073709551616, reflect.Uint64, uint64(math.MaxUint64)},
		{-1, reflect.Uint8, uint8(0)},
		{-1, reflect.Uint64, uint64(0)},
		{1e300, reflect.Float32, float32(math.MaxFloat32)},
		{-1e300, reflect.Float32, float32(-math.MaxFloat32)},
		{math.Inf(1), reflect.Int16, int16(32767)},
	}
	for _, c := range cases {
		got, ok := F64ToAnyNumOK(c.in, c.kind)
		if got != c.want || ok {
			t.Error(test.DiffMessage([]any{got, ok}, []any{c.want, false}, "Q3: overflow "+c.kind.String()))
		}
	}
}

func TestF64ToAnyNumOK_NaNIntegerReportsFalse(t *testing.T) {
	for _, kind := range append(append([]reflect.Kind{}, signedKinds...), unsignedKinds...) {
		got, ok := F64ToAnyNumOK(math.NaN(), kind)
		want := reflect.Zero(reflectType(kind)).Interface()
		if got != want || ok {
			t.Error(test.DiffMessage([]any{got, ok}, []any{want, false}, "Q3: NaN "+kind.String()))
		}
	}
}

func TestF64ToAnyNumOK_ValueEqualsF64ToAnyNum(t *testing.T) {
	kinds := append(append([]reflect.Kind{}, signedKinds...), unsignedKinds...)
	kinds = append(kinds, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128)
	inputs := []float64{0, 1, -1, 1.9, -1.9, 127, 128, 255, 256, 70000, 5e9, -5e9, 1e20, -1e20, 1e30, 1e300, -1e300, math.Inf(1), math.Inf(-1)}
	for _, kind := range kinds {
		for _, in := range inputs {
			want := F64ToAnyNum(in, kind)
			got, _ := F64ToAnyNumOK(in, kind)
			if got != want {
				t.Error(test.DiffMessage(got, want, "Q3: value equals F64ToAnyNum "+kind.String()))
			}
		}
	}
}
