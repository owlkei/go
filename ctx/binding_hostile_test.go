package ctx

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

type metaDTO struct {
	Meta    any  `bind:"meta"`
	PtrMeta *any `bind:"ptr_meta"`
}

func TestBindStruct_InterfaceFieldNull(t *testing.T) {
	var got any
	var fls []FieldLevel
	if !noPanic(t, "B2 interface field with null", func() { got, fls = Body{"meta": nil}.Bind(metaDTO{}) }) {
		return
	}
	dto := got.(metaDTO)
	if dto.Meta != nil {
		t.Error(test.DiffMessage(dto.Meta, nil, "B2: an interface field bound from null should stay nil"))
	}
	fl := findFieldLevel(fls, "meta")
	if fl == nil || !fl.IsValue() || fl.Value() != nil {
		t.Error(test.DiffMessage(fl, "<FieldLevel IsValue true, Value nil>", "B2: null should report one value FieldLevel holding nil"))
	}
	count := 0
	for _, f := range fls {
		if f.Tag() == "meta" {
			count++
		}
	}
	if count != 1 {
		t.Error(test.DiffMessage(count, 1, "B2: exactly one FieldLevel for the null interface field"))
	}
}

func TestBindStruct_PointerToInterfaceNull(t *testing.T) {
	var got any
	var fls []FieldLevel
	if !noPanic(t, "B2 pointer to interface with null", func() { got, fls = Body{"ptr_meta": nil}.Bind(metaDTO{}) }) {
		return
	}
	dto := got.(metaDTO)
	if dto.PtrMeta == nil || *dto.PtrMeta != nil {
		t.Error(test.DiffMessage(dto.PtrMeta, "<pointer to a nil interface>", "B2: *any bound from null should point to a nil interface"))
	}
	fl := findFieldLevel(fls, "ptr_meta")
	if fl == nil || !fl.IsValue() {
		t.Error(test.DiffMessage(fl, "<FieldLevel IsValue true>", "B2: null should report a value FieldLevel for *any"))
	}
}

type nestedWrongTypeDTO struct {
	Addr Address `bind:"addr"`
}

func TestBindStruct_NestedStructWrongType(t *testing.T) {
	inputs := map[string]any{
		"string": "x",
		"number": float64(1),
		"array":  []any{},
		"null":   nil,
		"bool":   true,
	}
	for name, in := range inputs {
		var got any
		var fls []FieldLevel
		if !noPanic(t, "B3 nested struct bound from a "+name, func() { got, fls = Body{"addr": in}.Bind(nestedWrongTypeDTO{}) }) {
			continue
		}
		if !reflect.DeepEqual(got, nestedWrongTypeDTO{}) {
			t.Error(test.DiffMessage(got, nestedWrongTypeDTO{}, "B3: a "+name+" for a struct field should leave the field zero"))
		}
		if len(fls) != 0 {
			t.Error(test.DiffMessage(len(fls), 0, "B3: a "+name+" for a struct field should add no FieldLevel"))
		}
	}
}

type unsupportedElemDTO struct {
	Ptrs   []*Address          `bind:"ptrs"`
	PtrMap map[string]*Address `bind:"ptr_map"`
	Chans  []chan int          `bind:"chans"`
}

func TestBindStruct_UnsupportedSliceElem(t *testing.T) {
	data := Body{
		"ptrs":  []any{map[string]any{"street": "s"}},
		"chans": []any{float64(1)},
	}
	var got any
	var fls []FieldLevel
	if !noPanic(t, "B4 slices of unsupported element kinds", func() { got, fls = data.Bind(unsupportedElemDTO{}) }) {
		return
	}
	dto := got.(unsupportedElemDTO)
	if dto.Ptrs != nil || dto.Chans != nil {
		t.Error(test.DiffMessage(dto, unsupportedElemDTO{}, "B4: slices of unsupported element kinds should stay zero"))
	}
	for _, tag := range []string{"ptrs", "chans"} {
		if fl := findFieldLevel(fls, tag); fl != nil && fl.IsValue() {
			t.Error(test.DiffMessage(fl.Value(), nil, "B4: "+tag+" should add no value FieldLevel"))
		}
	}
}

func TestBindStruct_UnsupportedMapElem(t *testing.T) {
	data := Body{"ptr_map": map[string]any{"a": map[string]any{"street": "s"}}}
	var got any
	var fls []FieldLevel
	if !noPanic(t, "B4 map of an unsupported element kind", func() { got, fls = data.Bind(unsupportedElemDTO{}) }) {
		return
	}
	if got.(unsupportedElemDTO).PtrMap != nil {
		t.Error(test.DiffMessage(got.(unsupportedElemDTO).PtrMap, nil, "B4: a map of an unsupported element kind should stay zero"))
	}
	if fl := findFieldLevel(fls, "ptr_map"); fl != nil && fl.IsValue() {
		t.Error(test.DiffMessage(fl.Value(), nil, "B4: ptr_map should add no value FieldLevel"))
	}
}

type mapOfMapDTO struct {
	M map[string]map[string]int `bind:"m"`
}

func TestBindStruct_MapOfMapInnerNotObject(t *testing.T) {
	var got any
	if !noPanic(t, "B5 map of maps whose inner value is a number", func() { got, _ = Body{"m": map[string]any{"a": float64(1)}}.Bind(mapOfMapDTO{}) }) {
		return
	}
	if m := got.(mapOfMapDTO).M; len(m) != 0 {
		t.Error(test.DiffMessage(m, map[string]map[string]int{}, "B5: an inner value that is not an object should be skipped"))
	}

	mixed := Body{"m": map[string]any{
		"a": map[string]any{"x": float64(1)},
		"b": float64(2),
		"c": "text",
		"d": nil,
		"e": []any{},
	}}
	if !noPanic(t, "B5 map of maps with mixed inner values", func() { got, _ = mixed.Bind(mapOfMapDTO{}) }) {
		return
	}
	expected := map[string]map[string]int{"a": {"x": 1}}
	if m := got.(mapOfMapDTO).M; !reflect.DeepEqual(m, expected) {
		t.Error(test.DiffMessage(m, expected, "B5: other keys should still bind when an inner value is not an object"))
	}
}

type nestedSliceDTO struct {
	G [][]int `bind:"g"`
}

func TestBindStruct_NestedSliceNullElement(t *testing.T) {
	var got any
	if !noPanic(t, "B6 nested slice with a null element", func() { got, _ = Body{"g": []any{[]any{float64(1), nil}}}.Bind(nestedSliceDTO{}) }) {
		return
	}
	expected := [][]int{{1}}
	if g := got.(nestedSliceDTO).G; !reflect.DeepEqual(g, expected) {
		t.Error(test.DiffMessage(g, expected, "B6: null elements should be skipped"))
	}
}

func TestBindStruct_NestedSliceNullRow(t *testing.T) {
	var got any
	if !noPanic(t, "B6 nested slice with a null row", func() { got, _ = Body{"g": []any{nil}}.Bind(nestedSliceDTO{}) }) {
		return
	}
	if g := got.(nestedSliceDTO).G; len(g) != 0 {
		t.Error(test.DiffMessage(g, [][]int{}, "B6: null rows should be skipped"))
	}
}

type rangeDTO struct {
	Int     int       `bind:"int"`
	Int8    int8      `bind:"int8"`
	Int16   int16     `bind:"int16"`
	Int32   int32     `bind:"int32"`
	Int64   int64     `bind:"int64"`
	Uint    uint      `bind:"uint"`
	Uint8   uint8     `bind:"uint8"`
	Uint16  uint16    `bind:"uint16"`
	Uint32  uint32    `bind:"uint32"`
	Uint64  uint64    `bind:"uint64"`
	Float32 float32   `bind:"float32"`
	Complex complex64 `bind:"complex64"`
	PInt8   *int8     `bind:"pint8"`
	PUint8  *uint8    `bind:"puint8"`
	PFloat  *float32  `bind:"pfloat32"`
	PInt64  *int64    `bind:"pint64"`
}

func assertRangeFieldLevels(t *testing.T, desc, key string, fls []FieldLevel) {
	t.Helper()
	for _, fl := range fls {
		if fl.Tag() == key {
			if fl.IsValue() {
				t.Error(test.DiffMessage(fl.Value(), nil, desc+": the mismatched field should not report a value"))
			}
			continue
		}
		if fl.IsValue() {
			t.Error(test.DiffMessage(fl.Tag(), "<absent>", desc+": the other fields are absent from the data and should not report a value"))
		}
	}
}

func TestBindStruct_NumberOutOfRange(t *testing.T) {
	cases := []struct {
		key string
		val float64
	}{
		{"int", 1e300}, {"int", -1e300}, {"int", 9223372036854775808}, {"int", -1e19},
		{"int8", -129}, {"int8", 128}, {"int8", 300},
		{"int16", -32769}, {"int16", 32768},
		{"int32", -2147483649}, {"int32", 2147483648},
		{"int64", 9223372036854775808}, {"int64", -1e19}, {"int64", 1e300},
		{"uint", 18446744073709551616}, {"uint", 1e300},
		{"uint8", 256}, {"uint16", 65536}, {"uint32", 4294967296},
		{"uint64", 18446744073709551616}, {"uint64", 1e300},
		{"float32", 1e39}, {"float32", -1e39}, {"float32", 1e300},
		{"complex64", 1e39},
		{"pint8", 300}, {"pint8", -129},
		{"puint8", 256},
		{"pfloat32", 1e39},
		{"pint64", 1e19},
	}
	for _, c := range cases {
		var got any
		var fls []FieldLevel
		desc := "B7: " + c.key + " out of range"
		if !noPanic(t, desc, func() { got, fls = Body{c.key: c.val}.Bind(rangeDTO{}) }) {
			continue
		}
		if !reflect.DeepEqual(got, rangeDTO{}) {
			t.Error(test.DiffMessage(got, rangeDTO{}, desc+" ("+fmt.Sprint(c.val)+") should be a mismatch and leave the field zero, not a wrapped or saturated value"))
		}
		assertRangeFieldLevels(t, desc, c.key, fls)
	}
}

func TestBindStruct_NumberAtRangeBoundaryBinds(t *testing.T) {
	data := Body{
		"int8": float64(math.MinInt8), "int16": float64(math.MinInt16), "int32": float64(math.MinInt32),
		"int64": float64(math.MinInt64), "int": float64(9223372036854774784),
		"uint8": float64(math.MaxUint8), "uint16": float64(math.MaxUint16), "uint32": float64(math.MaxUint32),
		"uint64": float64(18446744073709549568), "uint": float64(0),
		"float32": float64(math.MaxFloat32), "complex64": -float64(math.MaxFloat32),
		"pint8": float64(math.MaxInt8), "puint8": float64(0), "pfloat32": -float64(math.MaxFloat32),
		"pint64": float64(9223372036854774784),
	}
	var got any
	if !noPanic(t, "B7 boundary values", func() { got, _ = data.Bind(rangeDTO{}) }) {
		return
	}
	dto := got.(rangeDTO)
	expected := rangeDTO{
		Int8: math.MinInt8, Int16: math.MinInt16, Int32: math.MinInt32, Int64: math.MinInt64, Int: 9223372036854774784,
		Uint8: math.MaxUint8, Uint16: math.MaxUint16, Uint32: math.MaxUint32, Uint64: 18446744073709549568,
		Float32: math.MaxFloat32, Complex: complex(-math.MaxFloat32, 0),
	}
	pi8, pu8, pf, pi64 := int8(math.MaxInt8), uint8(0), float32(-math.MaxFloat32), int64(9223372036854774784)
	expected.PInt8, expected.PUint8, expected.PFloat, expected.PInt64 = &pi8, &pu8, &pf, &pi64
	if !reflect.DeepEqual(dto, expected) {
		t.Error(test.DiffMessage(dto, expected, "B7: the smallest and largest values of each type should bind"))
	}
}

func TestBindStruct_NegativeIntoUnsigned(t *testing.T) {
	for _, key := range []string{"uint", "uint8", "uint16", "uint32", "uint64", "puint8"} {
		var got any
		var fls []FieldLevel
		desc := "B7: " + key + " bound from -1"
		if !noPanic(t, desc, func() { got, fls = Body{key: float64(-1)}.Bind(rangeDTO{}) }) {
			continue
		}
		if !reflect.DeepEqual(got, rangeDTO{}) {
			t.Error(test.DiffMessage(got, rangeDTO{}, desc+" should be a mismatch and leave the field zero"))
		}
		assertRangeFieldLevels(t, desc, key, fls)
	}
}

type truncateDTO struct {
	Int   int            `bind:"int"`
	Neg   int8           `bind:"neg"`
	Uint8 uint8          `bind:"uint8"`
	Ints  []int          `bind:"ints"`
	Map   map[string]int `bind:"map"`
}

func TestBindStruct_FractionTruncates(t *testing.T) {
	data := Body{
		"int":   1.9,
		"neg":   -1.9,
		"uint8": 1.9,
		"ints":  []any{1.9, 2.1},
		"map":   map[string]any{"a": 1.9},
	}
	var got any
	var fls []FieldLevel
	if !noPanic(t, "B7 fractions", func() { got, fls = data.Bind(truncateDTO{}) }) {
		return
	}
	expected := truncateDTO{Int: 1, Neg: -1, Uint8: 1, Ints: []int{1, 2}, Map: map[string]int{"a": 1}}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "B7: fractions should truncate toward zero"))
	}
	if fl := findFieldLevel(fls, "int"); fl == nil || fl.Value() != 1 {
		t.Error(test.DiffMessage(fl, 1, "B7: the FieldLevel should hold the truncated value"))
	}
}

type sliceRangeDTO struct {
	Int8s   []int8          `bind:"int8s"`
	Uints   []uint          `bind:"uints"`
	Floats  []float32       `bind:"floats"`
	Map     map[string]int8 `bind:"map"`
	Nested  [][]int8        `bind:"nested"`
	Allowed []int8          `bind:"allowed"`
}

func TestBindStruct_SliceNumberOutOfRange(t *testing.T) {
	data := Body{
		"int8s":   []any{float64(1), float64(300)},
		"uints":   []any{float64(1), float64(-1)},
		"floats":  []any{float64(1), 1e39},
		"map":     map[string]any{"a": float64(1), "b": float64(300)},
		"nested":  []any{[]any{float64(1), float64(300)}, []any{float64(2)}},
		"allowed": []any{float64(-128), float64(127)},
	}
	var got any
	if !noPanic(t, "B7 slices and maps with out-of-range numbers", func() { got, _ = data.Bind(sliceRangeDTO{}) }) {
		return
	}
	dto := got.(sliceRangeDTO)
	if len(dto.Int8s) != 0 {
		t.Error(test.DiffMessage(dto.Int8s, []int8{}, "B7: a slice with an out-of-range element should become empty"))
	}
	if len(dto.Uints) != 0 {
		t.Error(test.DiffMessage(dto.Uints, []uint{}, "B7: a slice with a negative element for an unsigned type should become empty"))
	}
	if len(dto.Floats) != 0 {
		t.Error(test.DiffMessage(dto.Floats, []float32{}, "B7: a float32 slice with an out-of-range element should become empty"))
	}
	if len(dto.Map) != 0 {
		t.Error(test.DiffMessage(dto.Map, map[string]int8{}, "B7: a map with an out-of-range value should become empty"))
	}
	expectedNested := [][]int8{{1}, {2}}
	if !reflect.DeepEqual(dto.Nested, expectedNested) {
		t.Error(test.DiffMessage(dto.Nested, expectedNested, "B7: an out-of-range element of a nested slice should be skipped"))
	}
	if !reflect.DeepEqual(dto.Allowed, []int8{-128, 127}) {
		t.Error(test.DiffMessage(dto.Allowed, []int8{-128, 127}, "B7: the limits of the element type are in range"))
	}
}

func TestBody_BindValueOutOfRangeIsMismatch(t *testing.T) {
	var got any
	if !noPanic(t, "BindValue with an out-of-range number", func() { got = Body{"int8": float64(300), "int16": float64(5)}.BindValue(rangeDTO{}) }) {
		return
	}
	dto := got.(rangeDTO)
	if dto.Int8 != 0 || dto.Int16 != 5 {
		t.Error(test.DiffMessage([2]any{dto.Int8, dto.Int16}, [2]any{0, 5}, "B7: BindValue should refuse the out-of-range field and bind the other"))
	}
}

func TestBindStruct_HostileScalarMismatchesNeverPanic(t *testing.T) {
	values := []any{nil, "text", true, float64(1), []any{}, []any{nil}, map[string]any{}, map[string]any{"a": nil}}
	typeCases := map[string]any{
		"bool": struct {
			V bool `bind:"v"`
		}{},
		"int": struct {
			V int `bind:"v"`
		}{},
		"string": struct {
			V string `bind:"v"`
		}{},
		"pointer": struct {
			V *int `bind:"v"`
		}{},
		"slice": struct {
			V []string `bind:"v"`
		}{},
		"slice2": struct {
			V [][]string `bind:"v"`
		}{},
		"map": struct {
			V map[string]string `bind:"v"`
		}{},
		"map2": struct {
			V map[string][]int `bind:"v"`
		}{},
		"struct": struct {
			V Address `bind:"v"`
		}{},
		"structs": struct {
			V []Address `bind:"v"`
		}{},
		"ptr tree": struct {
			V *Person `bind:"v"`
		}{},
		"iface": struct {
			V any `bind:"v"`
		}{},
	}
	for name, dto := range typeCases {
		for _, in := range values {
			noPanic(t, "B2-B6: "+name+" field bound from "+fmt.Sprintf("%T", in), func() {
				Body{"v": in}.Bind(dto)
			})
		}
	}
}

type bindValueDTO struct {
	Name string `bind:"name"`
	Age  int    `bind:"age"`
}

func TestQuery_BindValueEqualsBind(t *testing.T) {
	q := Query{"name": {"joe"}, "age": {"30"}}
	want, _ := q.Bind(bindValueDTO{})
	if got := q.BindValue(bindValueDTO{}); !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "BindValue should return Bind's value"))
	}
}

func TestForm_BindValueEqualsBind(t *testing.T) {
	f := Form{"name": {"joe"}, "age": {"30"}}
	want, _ := f.Bind(bindValueDTO{})
	if got := f.BindValue(bindValueDTO{}); !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "BindValue should return Bind's value"))
	}
}

func TestHeader_BindValueEqualsBind(t *testing.T) {
	type dto struct {
		Token string `bind:"x-token"`
	}
	h := Header{"X-Token": {"t"}}
	want, _ := h.Bind(dto{})
	got := h.BindValue(dto{})
	if !reflect.DeepEqual(got, want) || got.(dto).Token != "t" {
		t.Error(test.DiffMessage(got, want, "BindValue should return Bind's value, with canonical header tags"))
	}
}

func TestParam_BindValueEqualsBind(t *testing.T) {
	p := Param{"name": {"joe"}, "age": {"30"}}
	want, _ := p.Bind(bindValueDTO{})
	if got := p.BindValue(bindValueDTO{}); !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "BindValue should return Bind's value"))
	}
}

func TestBody_BindValueEqualsBind(t *testing.T) {
	b := Body{"name": "joe", "age": float64(30)}
	want, _ := b.Bind(bindValueDTO{})
	got := b.BindValue(bindValueDTO{})
	if !reflect.DeepEqual(got, want) || got.(bindValueDTO).Age != 30 {
		t.Error(test.DiffMessage(got, want, "BindValue should return Bind's value"))
	}
}

func TestWSPayload_BindValueEqualsBind(t *testing.T) {
	p := WSPayload{"name": "joe", "age": float64(30)}
	want, _ := p.Bind(bindValueDTO{})
	got := p.BindValue(bindValueDTO{})
	if !reflect.DeepEqual(got, want) || got.(bindValueDTO).Name != "joe" {
		t.Error(test.DiffMessage(got, want, "BindValue should return Bind's value"))
	}
}

func TestBindValue_PanicsWithErrBindTarget(t *testing.T) {
	bindTargetPanics(t, "Query.BindValue(&dto)", func() { Query{}.BindValue(&bindValueDTO{}) })
	bindTargetPanics(t, "Form.BindValue(nil)", func() { Form{}.BindValue(nil) })
	bindTargetPanics(t, "Header.BindValue(42)", func() { Header{}.BindValue(42) })
	bindTargetPanics(t, "Param.BindValue(&dto)", func() { Param{}.BindValue(&bindValueDTO{}) })
	bindTargetPanics(t, "Body.BindValue(nil)", func() { Body{}.BindValue(nil) })
	bindTargetPanics(t, "WSPayload.BindValue(42)", func() { WSPayload{}.BindValue(42) })
}

func TestBindValue_NamedTypeAndNull(t *testing.T) {
	var got any
	if !noPanic(t, "BindValue with a named type and null", func() {
		got = Body{"page": float64(2), "meta": nil}.BindValue(struct {
			Page namedInt `bind:"page"`
			Meta any      `bind:"meta"`
		}{})
	}) {
		return
	}
	v := reflect.ValueOf(got)
	if v.Field(0).Interface() != namedInt(2) || v.Field(1).Interface() != nil {
		t.Error(test.DiffMessage(got, "<Page(2), Meta nil>", "BindValue should follow the same rules as Bind"))
	}
}
