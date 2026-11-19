package ctx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/aggregation"
	"go.owlky.com/owlky/internal/test"
	"go.owlky.com/owlky/internal/tracecontext"
	"golang.org/x/net/websocket"
)

func panicError(t *testing.T, fn func()) (err error) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic, got none")
			return
		}
		e, ok := r.(error)
		if !ok {
			t.Fatal(test.DiffMessage(fmt.Sprintf("%T", r), "error", "the panic value should be an error"))
			return
		}
		err = e
	}()
	fn()
	return nil
}

func panicException(t *testing.T, fn func()) (exception.Exception, error) {
	t.Helper()
	err := panicError(t, fn)
	var exc exception.Exception
	if !errors.As(err, &exc) {
		t.Fatal(test.DiffMessage(fmt.Sprintf("%T", err), "exception.Exception", "the panic should carry an exception.Exception"))
	}
	return exc, err
}

func noPanic(t *testing.T, desc string, fn func()) (ok bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Error(test.DiffMessage(r, "<no panic>", desc+": must not panic"))
			ok = false
		}
	}()
	fn()
	return true
}

func findFieldLevel(fls []FieldLevel, tag string) *FieldLevel {
	for i := range fls {
		if fls[i].Tag() == tag {
			return &fls[i]
		}
	}
	return nil
}

type discardResponseWriter struct {
	header http.Header
}

func newDiscardResponseWriter() *discardResponseWriter {
	return &discardResponseWriter{header: http.Header{}}
}

func (d *discardResponseWriter) Header() http.Header { return d.header }

func (d *discardResponseWriter) Write(p []byte) (int, error) { return len(p), nil }

func (d *discardResponseWriter) WriteHeader(int) {}

type fnCacheDTO struct {
	Name string `bind:"name"`
	Age  int    `bind:"age"`
}

type (
	namedBool       bool
	namedInt        int
	namedInt8       int8
	namedInt16      int16
	namedInt32      int32
	namedInt64      int64
	namedUint       uint
	namedUint8      uint8
	namedUint16     uint16
	namedUint32     uint32
	namedUint64     uint64
	namedFloat32    float32
	namedFloat64    float64
	namedComplex64  complex64
	namedComplex128 complex128
	namedString     string
)

type namedScalarDTO struct {
	Bool       namedBool       `bind:"bool"`
	Int        namedInt        `bind:"int"`
	Int8       namedInt8       `bind:"int8"`
	Int16      namedInt16      `bind:"int16"`
	Int32      namedInt32      `bind:"int32"`
	Int64      namedInt64      `bind:"int64"`
	Uint       namedUint       `bind:"uint"`
	Uint8      namedUint8      `bind:"uint8"`
	Uint16     namedUint16     `bind:"uint16"`
	Uint32     namedUint32     `bind:"uint32"`
	Uint64     namedUint64     `bind:"uint64"`
	Float32    namedFloat32    `bind:"float32"`
	Float64    namedFloat64    `bind:"float64"`
	Complex64  namedComplex64  `bind:"complex64"`
	Complex128 namedComplex128 `bind:"complex128"`
	String     namedString     `bind:"string"`
}

type namedSliceDTO struct {
	Bools   []namedBool    `bind:"bools"`
	Ints    []namedInt     `bind:"ints"`
	Uint8s  []namedUint8   `bind:"uint8s"`
	Floats  []namedFloat64 `bind:"floats"`
	Strings []namedString  `bind:"strings"`
}

var namedScalarExpectedFromBody = namedScalarDTO{
	Bool: true, Int: 7, Int8: 7, Int16: 7, Int32: 7, Int64: 7,
	Uint: 7, Uint8: 7, Uint16: 7, Uint32: 7, Uint64: 7,
	Float32: 1.5, Float64: 1.5, Complex64: 2, Complex128: 2, String: "s",
}

func namedScalarBody() map[string]any {
	return map[string]any{
		"bool": true, "int": float64(7), "int8": float64(7), "int16": float64(7), "int32": float64(7), "int64": float64(7),
		"uint": float64(7), "uint8": float64(7), "uint16": float64(7), "uint32": float64(7), "uint64": float64(7),
		"float32": 1.5, "float64": 1.5, "complex64": float64(2), "complex128": float64(2), "string": "s",
	}
}

type concurrentBinderDTO struct {
	Name  string         `bind:"name"`
	Count int            `bind:"count"`
	Tags  []string       `bind:"tags"`
	Attrs map[string]int `bind:"attrs"`
}

type recursiveNodeDTO struct {
	Name     string             `bind:"name"`
	Children []recursiveNodeDTO `bind:"children"`
}

type recursiveListDTO struct {
	Value int               `bind:"value"`
	Next  *recursiveListDTO `bind:"next"`
}

func nestedNodeBody(depth int) map[string]any {
	node := map[string]any{"name": "leaf"}
	for i := depth - 1; i >= 0; i-- {
		node = map[string]any{"name": "n" + strconv.Itoa(i), "children": []any{node}}
	}
	return node
}

func bindTargetPanics(t *testing.T, name string, fn func()) {
	t.Helper()
	err := panicError(t, fn)
	if !errors.Is(err, ErrBindTarget) {
		t.Error(test.DiffMessage(err, ErrBindTarget, "B23: "+name+" should panic with an error wrapping ErrBindTarget"))
	}
}

type matchDTO struct {
	Name    string            `bind:"name"`
	Age     int               `bind:"age"`
	Score   float64           `bind:"score"`
	Active  bool              `bind:"active"`
	Tags    []string          `bind:"tags"`
	Counts  []int             `bind:"counts"`
	Second  int               `bind:"counts.1"`
	Opt     *string           `bind:"opt"`
	Missing string            `bind:"missing"`
	Page    int               `bind:"page"`
	Attrs   map[string]string `bind:"attrs"`
	Home    Address           `bind:"home"`
	Others  []Address         `bind:"others"`
	Plain   string
}

func matchBodyData() map[string]any {
	return map[string]any{
		"name":   "joe",
		"age":    float64(30),
		"score":  2.5,
		"active": true,
		"tags":   []any{"a", "b"},
		"counts": []any{float64(1), float64(2)},
		"opt":    "o",
		"page":   float64(2),
		"attrs":  map[string]any{"k": "v"},
		"home":   map[string]any{"street": "s", "city": "c", "zip_code": "z"},
		"others": []any{map[string]any{"street": "s1"}, map[string]any{"city": "c2"}},
	}
}

func matchStrArrData() map[string][]string {
	return map[string][]string{
		"name":   {"joe"},
		"age":    {"30"},
		"score":  {"2.5"},
		"active": {"true"},
		"tags":   {"a", "b"},
		"counts": {"1", "2"},
		"opt":    {"o"},
		"page":   {"2"},
	}
}

type warmColdDTO struct {
	Name  string  `bind:"name"`
	Inner Address `bind:"inner"`
	List  []int   `bind:"list"`
}

type phoneEntryDTO struct {
	Type string `bind:"type"`
}

type addrEntryDTO struct {
	Street string `bind:"street"`
}

type fieldLevelDTO struct {
	Name   string          `bind:"name"`
	Addr   addrEntryDTO    `bind:"addr"`
	Phones []phoneEntryDTO `bind:"phones"`
	Tags   []int           `bind:"tags"`
	Attrs  map[string]int  `bind:"attrs"`
	Miss   string          `bind:"miss"`
}

type randomKind struct {
	name   string
	typed  reflect.Type
	named  reflect.Type
	body   func(r *rand.Rand) any
	strArr func(r *rand.Rand) string
}

func randomKinds() []randomKind {
	numberBody := func(r *rand.Rand) any {
		switch r.IntN(9) {
		case 0:
			return "text"
		case 1:
			return nil
		case 2:
			return true
		case 3:
			return float64(r.IntN(600) - 300)
		case 4:
			return r.NormFloat64() * 1e6
		case 5:
			return 1e300
		case 6:
			return -1e300
		case 7:
			return float64(r.IntN(100)) + 0.5
		}
		return float64(r.IntN(100))
	}
	numberStr := func(r *rand.Rand) string {
		pool := []string{"0", "1", "-1", "7", "255", "256", "300", "-129", "1.5", "x", "", "1e3", "99999999999999999999", "true", "1+2i"}
		return pool[r.IntN(len(pool))]
	}
	return []randomKind{
		{"bool", reflect.TypeFor[bool](), reflect.TypeFor[namedBool](), func(r *rand.Rand) any {
			switch r.IntN(4) {
			case 0:
				return "yes"
			case 1:
				return float64(1)
			case 2:
				return true
			}
			return false
		}, func(r *rand.Rand) string {
			pool := []string{"true", "false", "TRUE", "1", "0", "x", ""}
			return pool[r.IntN(len(pool))]
		}},
		{"string", reflect.TypeFor[string](), reflect.TypeFor[namedString](), func(r *rand.Rand) any {
			switch r.IntN(4) {
			case 0:
				return float64(1)
			case 1:
				return nil
			}
			return strconv.Itoa(r.IntN(50))
		}, func(r *rand.Rand) string { return strconv.Itoa(r.IntN(50)) }},
		{"int", reflect.TypeFor[int](), reflect.TypeFor[namedInt](), numberBody, numberStr},
		{"int8", reflect.TypeFor[int8](), reflect.TypeFor[namedInt8](), numberBody, numberStr},
		{"int16", reflect.TypeFor[int16](), reflect.TypeFor[namedInt16](), numberBody, numberStr},
		{"int32", reflect.TypeFor[int32](), reflect.TypeFor[namedInt32](), numberBody, numberStr},
		{"int64", reflect.TypeFor[int64](), reflect.TypeFor[namedInt64](), numberBody, numberStr},
		{"uint", reflect.TypeFor[uint](), reflect.TypeFor[namedUint](), numberBody, numberStr},
		{"uint8", reflect.TypeFor[uint8](), reflect.TypeFor[namedUint8](), numberBody, numberStr},
		{"uint16", reflect.TypeFor[uint16](), reflect.TypeFor[namedUint16](), numberBody, numberStr},
		{"uint32", reflect.TypeFor[uint32](), reflect.TypeFor[namedUint32](), numberBody, numberStr},
		{"uint64", reflect.TypeFor[uint64](), reflect.TypeFor[namedUint64](), numberBody, numberStr},
		{"float32", reflect.TypeFor[float32](), reflect.TypeFor[namedFloat32](), numberBody, numberStr},
		{"float64", reflect.TypeFor[float64](), reflect.TypeFor[namedFloat64](), numberBody, numberStr},
		{"complex64", reflect.TypeFor[complex64](), reflect.TypeFor[namedComplex64](), numberBody, numberStr},
		{"complex128", reflect.TypeFor[complex128](), reflect.TypeFor[namedComplex128](), numberBody, numberStr},
	}
}

func bindOneField(t *testing.T, typ reflect.Type, strArr bool, input any) (reflect.Value, bool) {
	t.Helper()
	structType := oneFieldStruct(typ)
	b, err := BinderOf(structType)
	if err != nil {
		t.Fatal(err)
	}
	var result any
	ok := noPanic(t, "binding "+typ.String(), func() {
		if strArr {
			result, _ = b.StrArr(map[string][]string{"v": input.([]string)})
			return
		}
		result, _ = b.Struct(map[string]any{"v": input})
	})
	if !ok {
		return reflect.Value{}, false
	}
	return reflect.ValueOf(result).Field(0), true
}

func convertedElements(v reflect.Value, to reflect.Type) []any {
	out := make([]any, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		out = append(out, v.Index(i).Convert(to).Interface())
	}
	return out
}

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

type benchPtrDTO struct {
	Name *string `bind:"name"`
	Age  *int    `bind:"age"`
}

type fnArrStructDTO struct {
	Name string `bind:"name"`
}

func oneFieldStruct(typ reflect.Type) reflect.Type {
	return reflect.StructOf([]reflect.StructField{{
		Name: "V",
		Type: typ,
		Tag:  `bind:"v"`,
	}})
}

func bindBody[T any](t *testing.T, input any) (T, []FieldLevel) {
	t.Helper()
	var zero T
	structType := oneFieldStruct(reflect.TypeFor[T]())
	var result any
	var fls []FieldLevel
	ok := noPanic(t, "Body.Bind of "+reflect.TypeFor[T]().String(), func() {
		result, fls = Body{"v": input}.Bind(reflect.New(structType).Elem().Interface())
	})
	if !ok {
		return zero, nil
	}
	v, isT := reflect.ValueOf(result).Field(0).Interface().(T)
	if !isT {
		t.Fatal(test.DiffMessage(reflect.ValueOf(result).Field(0).Type(), reflect.TypeFor[T](), "bound field type"))
	}
	return v, fls
}

type metaDTO struct {
	Meta    any  `bind:"meta"`
	PtrMeta *any `bind:"ptr_meta"`
}

type nestedWrongTypeDTO struct {
	Addr Address `bind:"addr"`
}

type unsupportedElemDTO struct {
	Ptrs   []*Address          `bind:"ptrs"`
	PtrMap map[string]*Address `bind:"ptr_map"`
	Chans  []chan int          `bind:"chans"`
}

type mapOfMapDTO struct {
	M map[string]map[string]int `bind:"m"`
}

type nestedSliceDTO struct {
	G [][]int `bind:"g"`
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

type truncateDTO struct {
	Int   int            `bind:"int"`
	Neg   int8           `bind:"neg"`
	Uint8 uint8          `bind:"uint8"`
	Ints  []int          `bind:"ints"`
	Map   map[string]int `bind:"map"`
}

type sliceRangeDTO struct {
	Int8s   []int8          `bind:"int8s"`
	Uints   []uint          `bind:"uints"`
	Floats  []float32       `bind:"floats"`
	Map     map[string]int8 `bind:"map"`
	Nested  [][]int8        `bind:"nested"`
	Allowed []int8          `bind:"allowed"`
}

type bindValueDTO struct {
	Name string `bind:"name"`
	Age  int    `bind:"age"`
}

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

type TestStrArrDTO struct {
	Bool1 bool `bind:"bool_1"`
	Bool2 bool `bind:"bools_1.0"`
	Bool3 bool `bind:"bools_1.1"`

	String1 string `bind:"string_1"`
	String2 string `bind:"strings_1.1"`

	Integer1 int   `bind:"integer_1"`
	Integer2 int8  `bind:"integers_1.1"`
	Integer3 int16 `bind:"integers_1.2"`
	Integer4 int32 `bind:"integers_1.3"`
	Integer5 int64 `bind:"integers_1.4"`
	Integer6 int   `bind:"integers_1.5"`
	Integer7 int   `bind:"integers_1.100"`
	Integer8 int   `bind:"integers_1.6"`

	Uinteger1 uint   `bind:"uinteger_1"`
	Uinteger2 uint8  `bind:"uintegers_1.1"`
	Uinteger3 uint16 `bind:"uintegers_1.2"`
	Uinteger4 uint32 `bind:"uintegers_1.3"`
	Uinteger5 uint64 `bind:"uintegers_1.4"`
	Uinteger6 uint   `bind:"uintegers_1.5"`
	Uinteger7 uint   `bind:"uintegers_1.100"`
	Uinteger8 uint   `bind:"uintegers_1.6"`
	Uinteger9 uint   `bind:"uintegers_1.7"`

	Float1 float32 `bind:"float_1"`
	Float2 float64 `bind:"floats_1.1"`
	Float3 float32 `bind:"floats_1.5"`
	Float4 float64 `bind:"floats_1.100"`
	Float5 float64 `bind:"floats_1.6"`
	Float6 float64 `bind:"floats_1.7"`

	Complex1 complex64  `bind:"complex_1"`
	Complex2 complex128 `bind:"complexes_1.1"`
	Complex3 complex128 `bind:"complexes_1.2"`
	Complex4 complex128 `bind:"complexes_1.3"`
	Complex5 complex128 `bind:"complexes_1.4"`

	Array1 []string `bind:"array_1,limit=3"`
}

type strArrSliceDTO struct {
	Bools     []bool       `bind:"bools"`
	Ints      []int        `bind:"ints"`
	Int8s     []int8       `bind:"int8s"`
	Int16s    []int16      `bind:"int16s"`
	Int32s    []int32      `bind:"int32s"`
	Int64s    []int64      `bind:"int64s"`
	Uints     []uint       `bind:"uints"`
	Uint8s    []uint8      `bind:"uint8s"`
	Uint16s   []uint16     `bind:"uint16s"`
	Uint32s   []uint32     `bind:"uint32s"`
	Uint64s   []uint64     `bind:"uint64s"`
	Float32s  []float32    `bind:"float32s"`
	Float64s  []float64    `bind:"float64s"`
	Complex64 []complex64  `bind:"complex64s"`
	Complex   []complex128 `bind:"complex128s"`
	Strings   []string     `bind:"strings"`
	Anys      []any        `bind:"anys"`
	Missing   string       `bind:"missing_field"`
}

type Address struct {
	Street  string `bind:"street"`
	City    string `bind:"city"`
	ZipCode string `bind:"zip_code"`
}

type Person struct {
	Name    string  `bind:"name"`
	Age     int     `bind:"age"`
	Address Address `bind:"address"`
	Email   string  `bind:"email"`
}

type PhoneNumber struct {
	Type  string `bind:"type"`
	Value string `bind:"value"`
}

type EmbeddedStruct struct {
	Name         string            `bind:"name"`
	Age          int               `bind:"age"`
	IsMarried    bool              `bind:"is_married"`
	PhoneNumbers []PhoneNumber     `bind:"phone_numbers"`
	Address      map[string]string `bind:"address"`
}

type TestDTO struct {
	Bool1 bool `bind:"bool_1"`
	Bool2 bool `bind:"bool_2"`

	String1 string `bind:"string_1"`
	String2 string `bind:"string_2"`

	Integer1 int   `bind:"integer_1"`
	Integer2 int8  `bind:"integer_2"`
	Integer3 int16 `bind:"integer_3"`
	Integer4 int32 `bind:"integer_4"`
	Integer5 int64 `bind:"integer_5"`

	Uinteger1 int   `bind:"uinteger_1"`
	Uinteger2 int8  `bind:"uinteger_2"`
	Uinteger3 int16 `bind:"uinteger_3"`
	Uinteger4 int32 `bind:"uinteger_4"`
	Uinteger5 int64 `bind:"uinteger_5"`

	Float32 float32 `bind:"float_32"`
	Float64 float64 `bind:"float_64"`

	Complex64  complex64  `bind:"complex_64"`
	Complex128 complex128 `bind:"complex_128"`

	BoolArray []bool `bind:"bool_array"`

	StringArray []string `bind:"string_array"`

	IntArray   []int   `bind:"int_array"`
	Int8Array  []int8  `bind:"int8_array"`
	Int16Array []int16 `bind:"int16_array"`
	Int32Array []int32 `bind:"int32_array"`
	Int64Array []int64 `bind:"int64_array"`

	UintArray   []uint   `bind:"uint_array"`
	Uint8Array  []uint8  `bind:"uint8_array"`
	Uint16Array []uint16 `bind:"uint16_array"`
	Uint32Array []uint32 `bind:"uint32_array"`
	Uint64Array []uint64 `bind:"uint64_array"`

	Float32Array []float32 `bind:"float32_array"`
	Float64Array []float64 `bind:"float64_array"`

	Complex64Array  []complex64  `bind:"complex64_array"`
	Complex128Array []complex128 `bind:"complex128_array"`

	ThreeDimensionsStringArray [][][]string `bind:"3_dimensions_string_array"`

	MapStringStringArray []map[string]string `bind:"map_string_string_array"`

	MapPerson map[string]Person `bind:"map_person"`

	NestedStructArray []Person `bind:"nested_struct_array"`

	EmbeddedStruct    EmbeddedStruct  `bind:"struct"`
	EmbeddedStructPtr *EmbeddedStruct `bind:"struct"`
}

type PtrDTO struct {
	PtrBool       *bool              `bind:"ptr_bool"`
	PtrInt        *int               `bind:"ptr_int"`
	PtrInt8       *int8              `bind:"ptr_int8"`
	PtrInt16      *int16             `bind:"ptr_int16"`
	PtrInt32      *int32             `bind:"ptr_int32"`
	PtrInt64      *int64             `bind:"ptr_int64"`
	PtrUint       *uint              `bind:"ptr_uint"`
	PtrUint8      *uint8             `bind:"ptr_uint8"`
	PtrUint16     *uint16            `bind:"ptr_uint16"`
	PtrUint32     *uint32            `bind:"ptr_uint32"`
	PtrUint64     *uint64            `bind:"ptr_uint64"`
	PtrFloat32    *float32           `bind:"ptr_float32"`
	PtrFloat64    *float64           `bind:"ptr_float64"`
	PtrComplex64  *complex64         `bind:"ptr_complex64"`
	PtrComplex128 *complex128        `bind:"ptr_complex128"`
	PtrString     *string            `bind:"ptr_string"`
	PtrStruct     *Address           `bind:"ptr_struct"`
	PtrSlice      *[]string          `bind:"ptr_slice"`
	PtrMap        *map[string]string `bind:"ptr_map"`
	PtrMissing    *string            `bind:"ptr_missing"`
}

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

func int8Ptr(v int8) *int8 { return &v }

func int16Ptr(v int16) *int16 { return &v }

func int32Ptr(v int32) *int32 { return &v }

func int64Ptr(v int64) *int64 { return &v }

func uintPtr(v uint) *uint { return &v }

func uint8Ptr(v uint8) *uint8 { return &v }

func uint16Ptr(v uint16) *uint16 { return &v }

func uint32Ptr(v uint32) *uint32 { return &v }

func float32Ptr(v float32) *float32 { return &v }

func float64Ptr(v float64) *float64 { return &v }

func stringPtr(v string) *string { return &v }

type interfaceFieldDTO struct {
	Any    any  `bind:"any_field"`
	PtrAny *any `bind:"ptr_any_field"`
}

type missingFieldDTO struct {
	Present string `bind:"present"`
	Missing string `bind:"missing"`
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

type minimalContext struct{}

func (minimalContext) ID() string { return "" }

func (minimalContext) Context() context.Context { return context.Background() }

var (
	_ Context = minimalContext{}
	_ Context = (*HTTPContext)(nil)
	_ Context = (*WSContext)(nil)
)

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

type bodyBindDTO struct {
	Name string `bind:"name"`
}

func newBodyContext(contentType, payload string) *HTTPContext {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(payload))
	if contentType != "" {
		c.Request.Header.Set("Content-Type", contentType)
	}
	return c
}

type readCounter struct {
	r io.Reader
	n *int
}

func (rc readCounter) Read(p []byte) (int, error) {
	*rc.n++
	return rc.r.Read(p)
}

type benchJSONStruct struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Email string   `json:"email"`
	Tags  []string `json:"tags"`
}

func benchSlice50() []benchJSONStruct {
	items := make([]benchJSONStruct, 50)
	for i := range items {
		items[i] = benchJSONStruct{ID: i, Name: "John Doe", Email: "john.doe@example.com", Tags: []string{"a", "b"}}
	}
	return items
}

func benchResponseContext(target string) *HTTPContext {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", target, nil)
	c.ResponseWriter = newDiscardResponseWriter()
	return c
}

func newTestHTTPContext() *HTTPContext {
	return NewHTTPContext()
}

func dirtyHTTPContext(t *testing.T) *HTTPContext {
	t.Helper()
	buf, contentType := buildMultipartFileBody(t, "avatar", "photo.png", "bytes")
	c := newTestHTTPContext()
	r := httptest.NewRequest("POST", "/path?callback=cb&a=1", buf)
	r.Header.Set("Content-Type", contentType)
	HTTPInit(c, httptest.NewRecorder(), r)
	c.Status(http.StatusAccepted)
	HTTPSetParams(c, map[string][]int{"id": {0}}, append(make([]string, 0, 8), "1", "2"))
	_ = c.Param()
	_ = c.Query()
	_ = c.Header()
	_ = c.Form()
	_ = c.File()
	_ = c.Body()
	HTTPAddAggregation(c, aggregation.NewAggregation())
	HTTPAddAggregation(c, aggregation.NewAggregation())
	HTTPSetNext(c, func() {})
	return c
}

func buildMultipartFileBody(t *testing.T, field, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

type filePtrDTO struct {
	Avatar *DataFile `bind:"avatar"`
}

type fileSliceDTO struct {
	Photos []*DataFile `bind:"photos"`
}

type fileValidatorDTO struct {
	Avatar *DataFile `bind:"avatar"`
}

func (fileValidatorDTO) IsValid(f *DataFile) bool {
	return f.Filename == "allowed.png"
}

type fileHandlerDTO struct {
	Avatar *DataFile `bind:"avatar"`
}

var storedContent string

func (fileHandlerDTO) Store(f *DataFile, src multipart.File) {
	b, _ := io.ReadAll(src)
	storedContent = string(b)
}

func newUploadContext(t *testing.T, field, filename, content string) (*HTTPContext, *int) {
	t.Helper()
	buf, contentType := buildMultipartFileBody(t, field, filename, content)
	reads := 0
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", io.NopCloser(readCounter{r: buf, n: &reads}))
	c.Request.Header.Set("Content-Type", contentType)
	return c, &reads
}

type panickingStoreDTO struct {
	Avatar *DataFile `bind:"avatar"`
}

var panickingStoreSource multipart.File

func (panickingStoreDTO) Store(f *DataFile, src multipart.File) {
	panickingStoreSource = src
	panic("store failed")
}

type formBindDTO struct {
	Name string `bind:"name"`
}

func multipartFieldBody(t *testing.T, name, value string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField(name, value); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func newFormContext(contentType string, body io.Reader) *HTTPContext {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", body)
	if contentType != "" {
		c.Request.Header.Set("Content-Type", contentType)
	}
	return c
}

type headerBindDTO struct {
	Auth string `bind:"Authorization"`
}

type paramBindDTO struct {
	ID string `bind:"id"`
}

type queryBindDTO struct {
	Name string `bind:"name"`
	Age  int    `bind:"age"`
}

type queryRangeDTO struct {
	Int     int       `bind:"int"`
	Int64   int64     `bind:"int64"`
	Uint64  uint64    `bind:"uint64"`
	Float64 float64   `bind:"float64"`
	Ints    []int     `bind:"ints"`
	Floats  []float64 `bind:"floats"`
}

func newResponseContext(target string) (*HTTPContext, *httptest.ResponseRecorder) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	c.ResponseWriter = w
	return c, w
}

func jsonOK(t *testing.T, c *HTTPContext, data any, args ...any) {
	t.Helper()
	noPanic(t, "JSON", func() { c.JSON(data, args...) })
}

func jsonpOK(t *testing.T, c *HTTPContext, data any, args ...any) {
	t.Helper()
	noPanic(t, "JSONP", func() { c.JSONP(data, args...) })
}

func callbackTarget(callback string) string {
	return "/?callback=" + url.QueryEscape(callback)
}

type jsonStructDTO struct {
	Name string   `json:"name"`
	Age  int      `json:"age"`
	Tags []string `json:"tags"`
	Opt  *string  `json:"opt,omitempty"`
}

type jsonAppenderValue struct {
	body string
}

func (a jsonAppenderValue) AppendJSON(dst []byte) []byte {
	return append(dst, a.body...)
}

type jsonAppenderAndMarshaler struct{}

func (jsonAppenderAndMarshaler) AppendJSON(dst []byte) []byte {
	return append(dst, `"appender"`...)
}

func (jsonAppenderAndMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"marshaler"`), nil
}

var errMarshalFailed = errors.New("marshal failed")

type failingMarshaler struct{}

func (failingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errMarshalFailed
}

type randomPayloadStruct struct {
	A string         `json:"a"`
	B int            `json:"b"`
	C []any          `json:"c"`
	D map[string]any `json:"d"`
	E *string        `json:"e"`
	F float64        `json:"f,omitempty"`
}

func randomJSONString(r *rand.Rand) string {
	pieces := []string{"a", "b", "<", ">", "&", " ", " ", `"`, `\`, "\n", "\t", "\x00", "\xff", "\xc3", "é", "日本", "%s", "%", " ", "/", "\u007f"}
	var sb strings.Builder
	for i, n := 0, r.IntN(7); i < n; i++ {
		sb.WriteString(pieces[r.IntN(len(pieces))])
	}
	return sb.String()
}

func randomJSONValue(r *rand.Rand, depth int) any {
	kinds := 8
	if depth <= 0 {
		kinds = 5
	}
	switch r.IntN(kinds) {
	case 0:
		return nil
	case 1:
		return r.IntN(2) == 0
	case 2:
		return float64(r.IntN(2000) - 1000)
	case 3:
		return r.NormFloat64() * 1e7
	case 4:
		return randomJSONString(r)
	case 5:
		n := r.IntN(4)
		arr := make([]any, n)
		for i := range arr {
			arr[i] = randomJSONValue(r, depth-1)
		}
		return arr
	case 6:
		n := r.IntN(4)
		m := make(map[string]any, n)
		for i := 0; i < n; i++ {
			m[randomJSONString(r)] = randomJSONValue(r, depth-1)
		}
		return m
	}
	n := r.IntN(4)
	m := make(Map, n)
	for i := 0; i < n; i++ {
		m[randomJSONString(r)] = randomJSONValue(r, depth-1)
	}
	return m
}

func randomJSONPayload(r *rand.Rand) any {
	asMap := func() map[string]any {
		n := r.IntN(5)
		m := make(map[string]any, n)
		for i := 0; i < n; i++ {
			m[randomJSONString(r)] = randomJSONValue(r, 3)
		}
		return m
	}
	asStruct := func() randomPayloadStruct {
		s := randomPayloadStruct{A: randomJSONString(r), B: r.IntN(100), F: r.NormFloat64()}
		s.C, _ = randomJSONValue(r, 2).([]any)
		s.D, _ = randomJSONValue(r, 2).(map[string]any)
		if r.IntN(2) == 0 {
			e := randomJSONString(r)
			s.E = &e
		}
		return s
	}
	switch r.IntN(9) {
	case 0:
		return Map(asMap())
	case 1:
		m := Map(asMap())
		return &m
	case 2:
		return asMap()
	case 3:
		m := asMap()
		return &m
	case 4:
		return asStruct()
	case 5:
		s := asStruct()
		return &s
	case 6:
		return []any{randomJSONValue(r, 3), randomJSONValue(r, 3)}
	case 7:
		return []randomPayloadStruct{asStruct(), asStruct()}
	}
	return []string{randomJSONString(r), randomJSONString(r)}
}

const (
	scopeTraceID  = "4bf92f3577b34da6a3ce929d0e0e4736"
	scopeParentID = "00f067aa0ba902b7"
	scopeHeader   = "00-" + scopeTraceID + "-" + scopeParentID + "-01"
)

func serverConnWithScope(t *testing.T, traceparent string) (*websocket.Conn, *tracecontext.Scope, func()) {
	t.Helper()
	connCh := make(chan *websocket.Conn, 1)
	scopeCh := make(chan *tracecontext.Scope, 1)
	done := make(chan struct{})
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		scopeContext, scope := tracecontext.NewRequest(r.Context(), r.Header, time.Now())
		scopeCh <- scope
		websocket.Handler(func(c *websocket.Conn) {
			connCh <- c
			<-done
		}).ServeHTTP(w, r.WithContext(scopeContext))
	}))
	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http"), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if traceparent != "" {
		config.Header.Set("Traceparent", traceparent)
	}
	client, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	conn := <-connCh
	scope := <-scopeCh

	var once sync.Once
	release := func() {
		once.Do(func() { close(done) })
		<-finished
	}
	t.Cleanup(func() {
		release()
		_ = client.Close()
		server.Close()
	})
	return conn, scope, release
}

func newPlainConnPair(t *testing.T) (*websocket.Conn, *websocket.Conn, func()) {
	t.Helper()
	connCh := make(chan *websocket.Conn, 1)
	done := make(chan struct{})
	server := httptest.NewServer(websocket.Handler(func(c *websocket.Conn) {
		connCh <- c
		<-done
	}))
	client, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn := <-connCh
	return conn, client, func() {
		close(done)
		_ = client.Close()
		server.Close()
	}
}

type wsPayloadBindDTO struct {
	Name string `bind:"name"`
	Age  int    `bind:"age"`
}
