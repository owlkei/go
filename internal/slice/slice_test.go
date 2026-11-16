package slice

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestMap_ReturnsMappedInOrder(t *testing.T) {
	var order []int
	got := Map([]string{"a", "b", "c"}, func(el string, i int) string {
		order = append(order, i)
		return el + el
	})
	if !reflect.DeepEqual(got, []string{"aa", "bb", "cc"}) {
		t.Error(test.DiffMessage(got, []string{"aa", "bb", "cc"}, "L1: Map result"))
	}
	if !reflect.DeepEqual(order, []int{0, 1, 2}) {
		t.Error(test.DiffMessage(order, []int{0, 1, 2}, "L1: Map call order"))
	}
}

func TestMap_NilInputReturnsNonNilEmpty(t *testing.T) {
	got := Map(nil, func(string, int) int { return 1 })
	if got == nil || len(got) != 0 {
		t.Error(test.DiffMessage(got, "non-nil empty slice", "L1: Map of nil"))
	}
}

func TestMap_EmptyInputReturnsNonNilEmpty(t *testing.T) {
	got := Map([]string{}, func(string, int) int { return 1 })
	if got == nil || len(got) != 0 {
		t.Error(test.DiffMessage(got, "non-nil empty slice", "L1: Map of empty"))
	}
}

func TestFilter_KeepsMatchingInOrder(t *testing.T) {
	got := Filter([]string{"John Doe", "Jane Doe", "The Rock"}, func(el string, i int) bool { return strings.Contains(el, "Doe") })
	want := []string{"John Doe", "Jane Doe"}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L1: Filter result"))
	}
}

func TestFilter_PassesIndex(t *testing.T) {
	got := Filter([]int{10, 11, 12, 13}, func(el int, i int) bool { return i%2 == 1 })
	want := []int{11, 13}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L1: Filter index"))
	}
}

func TestFilter_NilAndNoMatchAreEmpty(t *testing.T) {
	if got := Filter(nil, func(int, int) bool { return true }); len(got) != 0 {
		t.Error(test.DiffMessage(len(got), 0, "L1: Filter of nil"))
	}
	if got := Filter([]int{1, 2}, func(int, int) bool { return false }); len(got) != 0 {
		t.Error(test.DiffMessage(len(got), 0, "L1: Filter without match"))
	}
}

func TestGet_InRange(t *testing.T) {
	arr := []string{"John Doe", "Jane Doe"}
	for i, want := range arr {
		got, ok := Get(arr, i)
		if got != want || !ok {
			t.Error(test.DiffMessage([]any{got, ok}, []any{want, true}, "L1: Get in range"))
		}
	}
}

func TestGet_OutOfRange(t *testing.T) {
	arr := []string{"a", "b"}
	for _, i := range []int{2, 100} {
		got, ok := Get(arr, i)
		if got != "" || ok {
			t.Error(test.DiffMessage([]any{got, ok}, []any{"", false}, "L1: Get out of range"))
		}
	}
	got, ok := Get[int](nil, 0)
	if got != 0 || ok {
		t.Error(test.DiffMessage([]any{got, ok}, []any{0, false}, "L1: Get of nil"))
	}
}

func TestGet_Negative(t *testing.T) {
	got, ok := Get([]string{"a"}, -1)
	if got != "" || ok {
		t.Error(test.DiffMessage([]any{got, ok}, []any{"", false}, "L1: Get negative index"))
	}
}

func TestStrParseBool_Valid(t *testing.T) {
	got := StrParseBool([]string{"true", "false"})
	want := []bool{true, false}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseBool valid"))
	}
}

func TestStrParseBool_InvalidIsZero(t *testing.T) {
	got := StrParseBool([]string{"bad", ""})
	want := []bool{false, false}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseBool invalid or out of range"))
	}
}

func TestStrParseInt_Valid(t *testing.T) {
	got := StrParseInt([]string{"1", "-2"})
	want := []int{1, -2}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseInt valid"))
	}
}

func TestStrParseInt_InvalidIsZero(t *testing.T) {
	got := StrParseInt([]string{"bad", "", "9223372036854775808"})
	want := []int{0, 0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseInt invalid or out of range"))
	}
}

func TestStrParseInt8_Valid(t *testing.T) {
	got := StrParseInt8([]string{"127", "-128"})
	want := []int8{127, -128}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseInt8 valid"))
	}
}

func TestStrParseInt8_InvalidIsZero(t *testing.T) {
	got := StrParseInt8([]string{"bad", "300", "-129"})
	want := []int8{0, 0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseInt8 invalid or out of range"))
	}
}

func TestStrParseInt16_Valid(t *testing.T) {
	got := StrParseInt16([]string{"32767", "-32768"})
	want := []int16{32767, -32768}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseInt16 valid"))
	}
}

func TestStrParseInt16_InvalidIsZero(t *testing.T) {
	got := StrParseInt16([]string{"bad", "40000"})
	want := []int16{0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseInt16 invalid or out of range"))
	}
}

func TestStrParseInt32_Valid(t *testing.T) {
	got := StrParseInt32([]string{"2147483647", "-2147483648"})
	want := []int32{2147483647, -2147483648}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseInt32 valid"))
	}
}

func TestStrParseInt32_InvalidIsZero(t *testing.T) {
	got := StrParseInt32([]string{"bad", "2147483648"})
	want := []int32{0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseInt32 invalid or out of range"))
	}
}

func TestStrParseInt64_Valid(t *testing.T) {
	got := StrParseInt64([]string{"9223372036854775807", "-5"})
	want := []int64{9223372036854775807, -5}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseInt64 valid"))
	}
}

func TestStrParseInt64_InvalidIsZero(t *testing.T) {
	got := StrParseInt64([]string{"bad", "9223372036854775808"})
	want := []int64{0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseInt64 invalid or out of range"))
	}
}

func TestStrParseUint_Valid(t *testing.T) {
	got := StrParseUint([]string{"0", "7"})
	want := []uint{0, 7}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseUint valid"))
	}
}

func TestStrParseUint_InvalidIsZero(t *testing.T) {
	got := StrParseUint([]string{"bad", "-1", ""})
	want := []uint{0, 0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseUint invalid or out of range"))
	}
}

func TestStrParseUint8_Valid(t *testing.T) {
	got := StrParseUint8([]string{"255", "0"})
	want := []uint8{255, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseUint8 valid"))
	}
}

func TestStrParseUint8_InvalidIsZero(t *testing.T) {
	got := StrParseUint8([]string{"bad", "256", "-1"})
	want := []uint8{0, 0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseUint8 invalid or out of range"))
	}
}

func TestStrParseUint16_Valid(t *testing.T) {
	got := StrParseUint16([]string{"65535"})
	want := []uint16{65535}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseUint16 valid"))
	}
}

func TestStrParseUint16_InvalidIsZero(t *testing.T) {
	got := StrParseUint16([]string{"bad", "65536"})
	want := []uint16{0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseUint16 invalid or out of range"))
	}
}

func TestStrParseUint32_Valid(t *testing.T) {
	got := StrParseUint32([]string{"4294967295"})
	want := []uint32{4294967295}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseUint32 valid"))
	}
}

func TestStrParseUint32_InvalidIsZero(t *testing.T) {
	got := StrParseUint32([]string{"bad", "4294967296"})
	want := []uint32{0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseUint32 invalid or out of range"))
	}
}

func TestStrParseUint64_Valid(t *testing.T) {
	got := StrParseUint64([]string{"18446744073709551615"})
	want := []uint64{18446744073709551615}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseUint64 valid"))
	}
}

func TestStrParseUint64_InvalidIsZero(t *testing.T) {
	got := StrParseUint64([]string{"bad", "18446744073709551616", "-1"})
	want := []uint64{0, 0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseUint64 invalid or out of range"))
	}
}

func TestStrParseFloat32_Valid(t *testing.T) {
	got := StrParseFloat32([]string{"1.5", "-2.5"})
	want := []float32{1.5, -2.5}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseFloat32 valid"))
	}
}

func TestStrParseFloat32_InvalidIsZero(t *testing.T) {
	got := StrParseFloat32([]string{"bad", "1e40", ""})
	want := []float32{0, 0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseFloat32 invalid or out of range"))
	}
}

func TestStrParseFloat64_Valid(t *testing.T) {
	got := StrParseFloat64([]string{"1.5", "-2.5"})
	want := []float64{1.5, -2.5}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseFloat64 valid"))
	}
}

func TestStrParseFloat64_InvalidIsZero(t *testing.T) {
	got := StrParseFloat64([]string{"bad", "1e400"})
	want := []float64{0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseFloat64 invalid or out of range"))
	}
}

func TestStrParseComplex64_Valid(t *testing.T) {
	got := StrParseComplex64([]string{"1+2i", "3"})
	want := []complex64{complex(1, 2), 3}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseComplex64 valid"))
	}
}

func TestStrParseComplex64_InvalidIsZero(t *testing.T) {
	got := StrParseComplex64([]string{"bad", ""})
	want := []complex64{0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseComplex64 invalid or out of range"))
	}
}

func TestStrParseComplex128_Valid(t *testing.T) {
	got := StrParseComplex128([]string{"1+2i", "3"})
	want := []complex128{complex(1, 2), 3}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseComplex128 valid"))
	}
}

func TestStrParseComplex128_InvalidIsZero(t *testing.T) {
	got := StrParseComplex128([]string{"bad", ""})
	want := []complex128{0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseComplex128 invalid or out of range"))
	}
}

func TestStrParse_NilAndEmptyInput(t *testing.T) {
	if got := StrParseInt(nil); len(got) != 0 {
		t.Error(test.DiffMessage(len(got), 0, "L2: StrParseInt of nil"))
	}
	if got := StrParseBool([]string{}); len(got) != 0 {
		t.Error(test.DiffMessage(len(got), 0, "L2: StrParseBool of empty"))
	}
}

func TestStrParseInt8_OutOfRangeIsZero(t *testing.T) {
	got := StrParseInt8([]string{"300"})
	if !reflect.DeepEqual(got, []int8{0}) {
		t.Error(test.DiffMessage(got, []int8{0}, "L2: StrParseInt8 300"))
	}
}

func TestStrParseUint_NegativeIsZero(t *testing.T) {
	got := StrParseUint([]string{"-1"})
	if !reflect.DeepEqual(got, []uint{0}) {
		t.Error(test.DiffMessage(got, []uint{0}, "L2: StrParseUint -1"))
	}
}

func TestStrParseFloat32_OverflowIsZero(t *testing.T) {
	got := StrParseFloat32([]string{"1e40"})
	if !reflect.DeepEqual(got, []float32{0}) {
		t.Error(test.DiffMessage(got, []float32{0}, "L2: StrParseFloat32 1e40"))
	}
}

func TestStrParseComplex64_IgnoresSpaces(t *testing.T) {
	got := StrParseComplex64([]string{"1 + 2i"})
	if !reflect.DeepEqual(got, []complex64{complex(1, 2)}) {
		t.Error(test.DiffMessage(got, []complex64{complex(1, 2)}, "L2: StrParseComplex64 spaces"))
	}
}

func TestStrParseComplex128_IgnoresSpaces(t *testing.T) {
	got := StrParseComplex128([]string{"1 + 2i"})
	if !reflect.DeepEqual(got, []complex128{complex(1, 2)}) {
		t.Error(test.DiffMessage(got, []complex128{complex(1, 2)}, "L2: StrParseComplex128 spaces"))
	}
}

func TestStrParseAny_BoxesEachString(t *testing.T) {
	got := StrParseAny([]string{"a", "1"})
	want := []any{"a", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "L2: StrParseAny"))
	}
	if got := StrParseAny(nil); len(got) != 0 {
		t.Error(test.DiffMessage(len(got), 0, "L2: StrParseAny of nil"))
	}
}

func TestIter_VisitsLeavesDepthFirst(t *testing.T) {
	input := []any{
		[]any{
			[]any{1000, 2000},
			[]any{3000},
		},
		[]any{
			[]any{4000, 5000},
		},
	}
	var leaves []int
	Iter(input, 3, func(el any, d int) {
		if n, ok := el.(int); ok {
			leaves = append(leaves, n)
		}
	})
	want := []int{1000, 2000, 3000, 4000, 5000}
	if !reflect.DeepEqual(leaves, want) {
		t.Error(test.DiffMessage(leaves, want, "L3: Iter leaf order"))
	}
}

func TestIter_ReportsDimension(t *testing.T) {
	input := []any{1, []any{2, 3}, 4}
	var events []string
	Iter(input, 2, func(el any, d int) {
		events = append(events, fmt.Sprintf("%v@%d", el, d))
	})
	want := []string{"1@2", "2@1", "3@1", "[2 3]@2", "4@2"}
	if !reflect.DeepEqual(events, want) {
		t.Error(test.DiffMessage(events, want, "L3: Iter dimensions"))
	}
}

func TestIter_EmptyAndNil(t *testing.T) {
	calls := 0
	Iter(nil, 1, func(any, int) { calls++ })
	Iter([]any{}, 1, func(any, int) { calls++ })
	if calls != 0 {
		t.Error(test.DiffMessage(calls, 0, "L3: Iter of nil or empty"))
	}
}
