package ctx

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

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

func TestBinder_NamedScalarTypesFromBody(t *testing.T) {
	var got any
	var fls []FieldLevel
	if !noPanic(t, "B1 Body.Bind with named scalar fields", func() { got, fls = Body(namedScalarBody()).Bind(namedScalarDTO{}) }) {
		return
	}
	if !reflect.DeepEqual(got, namedScalarExpectedFromBody) {
		t.Error(test.DiffMessage(got, namedScalarExpectedFromBody, "B1: named basic types should bind like their underlying type"))
	}
	if len(fls) != 16 {
		t.Error(test.DiffMessage(len(fls), 16, "B1: one FieldLevel per named field"))
	}
	if fl := findFieldLevel(fls, "int"); fl == nil || fl.Value() != namedInt(7) {
		t.Error(test.DiffMessage(fl, namedInt(7), "B1: FieldLevel.Value() should hold the named type"))
	}
	if fl := findFieldLevel(fls, "string"); fl == nil || fl.Value() != namedString("s") {
		t.Error(test.DiffMessage(fl, namedString("s"), "B1: FieldLevel.Value() should hold the named string type"))
	}
}

func TestBinder_NamedScalarTypesFromQuery(t *testing.T) {
	q := Query{
		"bool": {"true"}, "int": {"7"}, "int8": {"7"}, "int16": {"7"}, "int32": {"7"}, "int64": {"7"},
		"uint": {"7"}, "uint8": {"7"}, "uint16": {"7"}, "uint32": {"7"}, "uint64": {"7"},
		"float32": {"1.5"}, "float64": {"1.5"}, "complex64": {"1+2i"}, "complex128": {"1+2i"}, "string": {"s"},
	}
	expected := namedScalarExpectedFromBody
	expected.Complex64 = 1 + 2i
	expected.Complex128 = 1 + 2i

	var got any
	var fls []FieldLevel
	if !noPanic(t, "B1 Query.Bind with named scalar fields", func() { got, fls = q.Bind(namedScalarDTO{}) }) {
		return
	}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "B1: named basic types should bind from strings like their underlying type"))
	}
	if fl := findFieldLevel(fls, "int"); fl == nil || fl.Value() != namedInt(7) {
		t.Error(test.DiffMessage(fl, namedInt(7), "B1: FieldLevel.Value() should hold the named type"))
	}
}

func TestBinder_SliceOfNamedTypeFromBody(t *testing.T) {
	body := Body{
		"bools":   []any{true, false},
		"ints":    []any{float64(1), float64(2)},
		"uint8s":  []any{float64(1), float64(255)},
		"floats":  []any{1.5, 2.5},
		"strings": []any{"a", "b"},
	}
	expected := namedSliceDTO{
		Bools: []namedBool{true, false}, Ints: []namedInt{1, 2}, Uint8s: []namedUint8{1, 255},
		Floats: []namedFloat64{1.5, 2.5}, Strings: []namedString{"a", "b"},
	}
	var got any
	if !noPanic(t, "B1 Body.Bind with slices of named types", func() { got, _ = body.Bind(namedSliceDTO{}) }) {
		return
	}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "B1: slices of named types should bind like slices of their underlying type"))
	}
}

func TestBinder_SliceOfNamedTypeFromQuery(t *testing.T) {
	q := Query{
		"bools":   {"true", "false"},
		"ints":    {"1", "2"},
		"uint8s":  {"1", "255"},
		"floats":  {"1.5", "2.5"},
		"strings": {"a", "b"},
	}
	expected := namedSliceDTO{
		Bools: []namedBool{true, false}, Ints: []namedInt{1, 2}, Uint8s: []namedUint8{1, 255},
		Floats: []namedFloat64{1.5, 2.5}, Strings: []namedString{"a", "b"},
	}
	var got any
	if !noPanic(t, "B1 Query.Bind with slices of named types", func() { got, _ = q.Bind(namedSliceDTO{}) }) {
		return
	}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "B1: slices of named types should bind from strings like slices of their underlying type"))
	}
}

func TestBinderOf_NonStruct(t *testing.T) {
	cases := map[string]reflect.Type{
		"nil":            nil,
		"pointer":        reflect.TypeOf(&fnCacheDTO{}),
		"int":            reflect.TypeOf(42),
		"slice":          reflect.TypeOf([]fnCacheDTO{}),
		"map":            reflect.TypeOf(map[string]any{}),
		"interface type": reflect.TypeFor[any](),
	}
	for name, typ := range cases {
		b, err := BinderOf(typ)
		if !errors.Is(err, ErrBindTarget) {
			t.Error(test.DiffMessage(err, ErrBindTarget, "B23: BinderOf("+name+") should return ErrBindTarget"))
		}
		if b != nil {
			t.Error(test.DiffMessage(b, nil, "B23: BinderOf("+name+") should return a nil Binder with the error"))
		}
	}
}

func TestBinderOf_Struct(t *testing.T) {
	b, err := BinderOf(reflect.TypeOf(fnCacheDTO{}))
	if err != nil {
		t.Fatal(err)
	}
	if b == nil {
		t.Fatal(test.DiffMessage(b, "<*Binder>", "BinderOf a struct type should return a Binder"))
	}
}

func TestBinderOf_ErrBindTargetMessage(t *testing.T) {
	if ErrBindTarget.Error() != "ctx: bind target must be a struct" {
		t.Error(test.DiffMessage(ErrBindTarget.Error(), "ctx: bind target must be a struct", "ErrBindTarget message"))
	}
}

func TestBinderOf_CachedPerType(t *testing.T) {
	typ := reflect.TypeOf(fnCacheDTO{})
	first, err := BinderOf(typ)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BinderOf(typ)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error(test.DiffMessage(second, first, "BinderOf should return the same *Binder for a repeated type"))
	}
}

type concurrentBinderDTO struct {
	Name  string         `bind:"name"`
	Count int            `bind:"count"`
	Tags  []string       `bind:"tags"`
	Attrs map[string]int `bind:"attrs"`
}

func TestBinderOf_ConcurrentAccess_NoDataRace(t *testing.T) {
	const goroutines = 32
	typ := reflect.TypeOf(concurrentBinderDTO{})
	binders := make([]*Binder, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			b, err := BinderOf(typ)
			if err != nil {
				t.Error(err)
				return
			}
			binders[i] = b
			got, _ := b.Struct(map[string]any{"name": "joe", "count": float64(i), "tags": []any{"a"}, "attrs": map[string]any{"k": float64(1)}})
			dto, ok := got.(concurrentBinderDTO)
			if !ok || dto.Name != "joe" || dto.Count != i || len(dto.Tags) != 1 || dto.Attrs["k"] != 1 {
				t.Error(test.DiffMessage(got, "<bound concurrentBinderDTO>", "concurrent Struct should bind its own input"))
			}
		}()
	}

	wg.Wait()
	for i := 1; i < goroutines; i++ {
		if binders[i] != binders[0] {
			t.Error(test.DiffMessage(binders[i], binders[0], "concurrent BinderOf calls should all return the one stored Binder"))
			return
		}
	}
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

func TestBinderOf_RecursiveType(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(recursiveNodeDTO{}), reflect.TypeOf(recursiveListDTO{})} {
		b, err := BinderOf(typ)
		if err != nil || b == nil {
			t.Fatal(test.DiffMessage(err, nil, "B25: BinderOf a self-referential type "+typ.Name()+" should return a Binder"))
		}
	}
}

func TestBinder_RecursiveTypeBindsToDataDepth(t *testing.T) {
	const depth = 100
	var got any
	if !noPanic(t, "B25 Body.Bind of a recursive type", func() { got, _ = Body(nestedNodeBody(depth)).Bind(recursiveNodeDTO{}) }) {
		return
	}
	node, ok := got.(recursiveNodeDTO)
	if !ok {
		t.Fatal(test.DiffMessage(got, recursiveNodeDTO{}, "B25: Bind should return a recursiveNodeDTO"))
	}
	levels := 0
	for len(node.Children) == 1 {
		node = node.Children[0]
		levels++
	}
	if levels != depth || node.Name != "leaf" {
		t.Error(test.DiffMessage([2]any{levels, node.Name}, [2]any{depth, "leaf"}, "B25: a recursive type should bind to any depth the body holds"))
	}
}

func TestBinder_RecursivePointerTypeBindsToDataDepth(t *testing.T) {
	data := map[string]any{"value": float64(0)}
	for i := 1; i <= 50; i++ {
		data = map[string]any{"value": float64(i), "next": data}
	}
	var got any
	if !noPanic(t, "B25 Body.Bind of a recursive pointer type", func() { got, _ = Body(data).Bind(recursiveListDTO{}) }) {
		return
	}
	list, ok := got.(recursiveListDTO)
	if !ok {
		t.Fatal(test.DiffMessage(got, recursiveListDTO{}, "B25: Bind should return a recursiveListDTO"))
	}
	levels := 0
	cur := &list
	for cur.Next != nil {
		cur = cur.Next
		levels++
	}
	if levels != 50 || list.Value != 50 || cur.Value != 0 {
		t.Error(test.DiffMessage([3]int{levels, list.Value, cur.Value}, [3]int{50, 50, 0}, "B25: a recursive pointer type should bind to any depth the body holds"))
	}
}

func bindTargetPanics(t *testing.T, name string, fn func()) {
	t.Helper()
	err := panicError(t, fn)
	if !errors.Is(err, ErrBindTarget) {
		t.Error(test.DiffMessage(err, ErrBindTarget, "B23: "+name+" should panic with an error wrapping ErrBindTarget"))
	}
}

func TestQuery_BindPanicsWithErrBindTarget(t *testing.T) {
	q := Query{"name": {"joe"}}
	bindTargetPanics(t, "Query.Bind(&dto)", func() { q.Bind(&fnCacheDTO{}) })
	bindTargetPanics(t, "Query.Bind(nil)", func() { q.Bind(nil) })
	bindTargetPanics(t, "Query.Bind(42)", func() { q.Bind(42) })
}

func TestForm_BindPanicsWithErrBindTarget(t *testing.T) {
	f := Form{"name": {"joe"}}
	bindTargetPanics(t, "Form.Bind(&dto)", func() { f.Bind(&fnCacheDTO{}) })
	bindTargetPanics(t, "Form.Bind(nil)", func() { f.Bind(nil) })
	bindTargetPanics(t, "Form.Bind(42)", func() { f.Bind(42) })
}

func TestHeader_BindPanicsWithErrBindTarget(t *testing.T) {
	h := Header{"Name": {"joe"}}
	bindTargetPanics(t, "Header.Bind(&dto)", func() { h.Bind(&fnCacheDTO{}) })
	bindTargetPanics(t, "Header.Bind(nil)", func() { h.Bind(nil) })
	bindTargetPanics(t, "Header.Bind(42)", func() { h.Bind(42) })
}

func TestParam_BindPanicsWithErrBindTarget(t *testing.T) {
	p := Param{"name": {"joe"}}
	bindTargetPanics(t, "Param.Bind(&dto)", func() { p.Bind(&fnCacheDTO{}) })
	bindTargetPanics(t, "Param.Bind(nil)", func() { p.Bind(nil) })
	bindTargetPanics(t, "Param.Bind(42)", func() { p.Bind(42) })
}

func TestBody_BindPanicsWithErrBindTarget(t *testing.T) {
	b := Body{"name": "joe"}
	bindTargetPanics(t, "Body.Bind(&dto)", func() { b.Bind(&fnCacheDTO{}) })
	bindTargetPanics(t, "Body.Bind(nil)", func() { b.Bind(nil) })
	bindTargetPanics(t, "Body.Bind(42)", func() { b.Bind(42) })
}

func TestWSPayload_BindPanicsWithErrBindTarget(t *testing.T) {
	p := WSPayload{"name": "joe"}
	bindTargetPanics(t, "WSPayload.Bind(&dto)", func() { p.Bind(&fnCacheDTO{}) })
	bindTargetPanics(t, "WSPayload.Bind(nil)", func() { p.Bind(nil) })
	bindTargetPanics(t, "WSPayload.Bind(42)", func() { p.Bind(42) })
}

func TestBindStruct_PanicsWithErrBindTarget(t *testing.T) {
	d := map[string]any{"name": "joe"}
	bindTargetPanics(t, "BindStruct(&dto)", func() { BindStruct(d, &[]FieldLevel{}, &fnCacheDTO{}, "", "") })
	bindTargetPanics(t, "BindStruct(nil)", func() { BindStruct(d, &[]FieldLevel{}, nil, "", "") })
	bindTargetPanics(t, "BindStruct(42)", func() { BindStruct(d, &[]FieldLevel{}, 42, "", "") })
}

func TestBindStruct_AcceptsReflectType(t *testing.T) {
	got, _ := BindStruct(map[string]any{"name": "joe", "age": float64(3)}, &[]FieldLevel{}, reflect.TypeOf(fnCacheDTO{}), "", "")
	if !reflect.DeepEqual(got, fnCacheDTO{Name: "joe", Age: 3}) {
		t.Error(test.DiffMessage(got, fnCacheDTO{Name: "joe", Age: 3}, "BindStruct should keep accepting a reflect.Type as the target"))
	}
}

func TestBindStrArr_PanicsWithErrBindTarget(t *testing.T) {
	d := map[string][]string{"name": {"joe"}}
	bindTargetPanics(t, "BindStrArr(&dto)", func() { BindStrArr(d, &[]FieldLevel{}, &fnCacheDTO{}) })
	bindTargetPanics(t, "BindStrArr(nil)", func() { BindStrArr(d, &[]FieldLevel{}, nil) })
	bindTargetPanics(t, "BindStrArr(42)", func() { BindStrArr(d, &[]FieldLevel{}, 42) })
}

func TestBindFile_PanicsWithErrBindTarget(t *testing.T) {
	bindTargetPanics(t, "BindFile(&dto)", func() { BindFile(File{}, &filePtrDTO{}) })
	bindTargetPanics(t, "BindFile(nil)", func() { BindFile(File{}, nil) })
	bindTargetPanics(t, "BindFile(42)", func() { BindFile(File{}, 42) })
}

func TestFile_BindPanicsWithErrBindTarget(t *testing.T) {
	bindTargetPanics(t, "File.Bind(&dto)", func() { File{}.Bind(&filePtrDTO{}) })
	bindTargetPanics(t, "File.Bind(nil)", func() { File{}.Bind(nil) })
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

func TestBinder_StrArrMatchesBindStrArr(t *testing.T) {
	b, err := BinderOf(reflect.TypeOf(matchDTO{}))
	if err != nil {
		t.Fatal(err)
	}
	d := matchStrArrData()
	wantVal, wantFls := BindStrArr(d, &[]FieldLevel{}, matchDTO{})
	gotVal, gotFls := b.StrArr(d)
	if !reflect.DeepEqual(gotVal, wantVal) {
		t.Error(test.DiffMessage(gotVal, wantVal, "Binder.StrArr value should equal BindStrArr's"))
	}
	if !reflect.DeepEqual(gotFls, wantFls) {
		t.Error(test.DiffMessage(gotFls, wantFls, "Binder.StrArr FieldLevels should equal BindStrArr's"))
	}
}

func TestBinder_StructMatchesBindStruct(t *testing.T) {
	b, err := BinderOf(reflect.TypeOf(matchDTO{}))
	if err != nil {
		t.Fatal(err)
	}
	d := matchBodyData()
	wantVal, wantFls := BindStruct(d, &[]FieldLevel{}, matchDTO{}, "", "")
	gotVal, gotFls := b.Struct(d)
	if !reflect.DeepEqual(gotVal, wantVal) {
		t.Error(test.DiffMessage(gotVal, wantVal, "Binder.Struct value should equal BindStruct's"))
	}
	if !reflect.DeepEqual(gotFls, wantFls) {
		t.Error(test.DiffMessage(gotFls, wantFls, "Binder.Struct FieldLevels should equal BindStruct's"))
	}
}

func TestBinder_StructMatchesBodyBind(t *testing.T) {
	b, err := BinderOf(reflect.TypeOf(matchDTO{}))
	if err != nil {
		t.Fatal(err)
	}
	d := matchBodyData()
	wantVal, wantFls := Body(d).Bind(matchDTO{})
	gotVal, gotFls := b.Struct(d)
	if !reflect.DeepEqual(gotVal, wantVal) || !reflect.DeepEqual(gotFls, wantFls) {
		t.Error(test.DiffMessage(gotVal, wantVal, "Binder.Struct should equal Body.Bind"))
	}
}

func TestBinder_HeaderMatchesHeaderBind(t *testing.T) {
	b, err := BinderOf(reflect.TypeOf(headerBindDTO{}))
	if err != nil {
		t.Fatal(err)
	}
	h := Header{"Authorization": {"Bearer token"}}
	wantVal, wantFls := h.Bind(headerBindDTO{})
	gotVal, gotFls := b.Header(h)
	if !reflect.DeepEqual(gotVal, wantVal) || !reflect.DeepEqual(gotFls, wantFls) {
		t.Error(test.DiffMessage(gotVal, wantVal, "Binder.Header should equal Header.Bind"))
	}
}

func TestBinder_FileMatchesBindFile(t *testing.T) {
	buf, contentType := buildMultipartFileBody(t, "avatar", "photo.png", "bytes")
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", buf)
	c.Request.Header.Set("Content-Type", contentType)
	if err := c.ParseMultipartForm(defaultMaxMemory); err != nil {
		t.Fatal(err)
	}
	files := c.File()

	b, err := BinderOf(reflect.TypeOf(filePtrDTO{}))
	if err != nil {
		t.Fatal(err)
	}
	wantFiltered, wantVal := BindFile(files, filePtrDTO{})
	gotFiltered, gotVal := b.File(files)
	if !reflect.DeepEqual(gotFiltered, wantFiltered) {
		t.Error(test.DiffMessage(gotFiltered, wantFiltered, "Binder.File filtered files should equal BindFile's"))
	}
	if !reflect.DeepEqual(gotVal, wantVal) {
		t.Error(test.DiffMessage(gotVal, wantVal, "Binder.File value should equal BindFile's"))
	}
}

type warmColdDTO struct {
	Name  string  `bind:"name"`
	Inner Address `bind:"inner"`
	List  []int   `bind:"list"`
}

func TestBinder_WarmEqualsCold(t *testing.T) {
	d := map[string]any{
		"name":  "joe",
		"inner": map[string]any{"street": "s"},
		"list":  []any{float64(1), float64(2)},
	}
	coldVal, coldFls := Body(d).Bind(warmColdDTO{})
	warmVal, warmFls := Body(d).Bind(warmColdDTO{})
	if !reflect.DeepEqual(coldVal, warmVal) {
		t.Error(test.DiffMessage(warmVal, coldVal, "binding a cached type should give the value of the first build"))
	}
	if !reflect.DeepEqual(coldFls, warmFls) {
		t.Error(test.DiffMessage(warmFls, coldFls, "binding a cached type should give the FieldLevels of the first build"))
	}
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

func TestBindStruct_FieldLevelsKeepOrderAndNames(t *testing.T) {
	d := map[string]any{
		"name":   "n",
		"addr":   map[string]any{"street": "s"},
		"phones": []any{map[string]any{"type": "h"}},
		"tags":   []any{float64(1), float64(2)},
		"attrs":  map[string]any{"a": float64(1)},
	}
	_, fls := Body(d).Bind(fieldLevelDTO{})

	type row struct {
		tag, nested, ns, field string
		kind                   reflect.Kind
		isVal                  bool
	}
	expected := []row{
		{"name", "name", "fieldLevelDTO.Name", "Name", reflect.String, true},
		{"street", "addr.street", "fieldLevelDTO.Addr.addrEntryDTO.Street", "Street", reflect.String, true},
		{"addr", "addr", "fieldLevelDTO.Addr", "Addr", reflect.Struct, true},
		{"type", "phones.0.type", "fieldLevelDTO.Phones.0.phoneEntryDTO.Type", "Type", reflect.String, true},
		{"phones", "phones", "fieldLevelDTO.Phones", "Phones", reflect.Slice, true},
		{"tags", "tags", "fieldLevelDTO.Tags", "Tags", reflect.Slice, true},
		{"attrs", "attrs", "fieldLevelDTO.Attrs", "Attrs", reflect.Map, true},
		{"miss", "miss", "fieldLevelDTO.Miss", "Miss", reflect.String, false},
	}
	actual := make([]row, len(fls))
	for i, fl := range fls {
		actual[i] = row{fl.Tag(), fl.NestedTag(), fl.Namespace(), fl.Field(), fl.Kind(), fl.IsValue()}
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Error(test.DiffMessage(actual, expected, "FieldLevels should keep declaration order, nested entries before their parent, and the same names"))
	}
}

func TestHeader_BindCanonicalizesTag(t *testing.T) {
	type dto struct {
		Token string `bind:"x-token"`
		Mixed string `bind:"X-REQUEST-id"`
	}
	h := Header{"X-Token": {"t"}, "X-Request-Id": {"r"}}

	got, fls := h.Bind(dto{})
	if !reflect.DeepEqual(got, dto{Token: "t", Mixed: "r"}) {
		t.Error(test.DiffMessage(got, dto{Token: "t", Mixed: "r"}, "B14: Header.Bind should look each tag up by its canonical header form"))
	}
	if fl := findFieldLevel(fls, "x-token"); fl == nil || !fl.IsValue() || fl.Value() != "t" {
		t.Error(test.DiffMessage(fl, "<value t with tag x-token>", "B14: FieldLevel.Tag() should keep the tag as written"))
	}

	b, err := BinderOf(reflect.TypeOf(dto{}))
	if err != nil {
		t.Fatal(err)
	}
	gotB, flsB := b.Header(h)
	if !reflect.DeepEqual(gotB, dto{Token: "t", Mixed: "r"}) {
		t.Error(test.DiffMessage(gotB, dto{Token: "t", Mixed: "r"}, "B14: Binder.Header should look each tag up by its canonical header form"))
	}
	if fl := findFieldLevel(flsB, "x-token"); fl == nil {
		t.Error(test.DiffMessage(fl, "<FieldLevel with tag x-token>", "B14: Binder.Header FieldLevel.Tag() should keep the tag as written"))
	}
}

func TestHTTPContext_HeaderBindFromRequest(t *testing.T) {
	type dto struct {
		Token string `bind:"x-token"`
	}
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("x-token", "abc")
	got, _ := c.Header().Bind(dto{})
	if got.(dto).Token != "abc" {
		t.Error(test.DiffMessage(got, dto{Token: "abc"}, "B14: a header set through Set should bind through a lower case tag"))
	}
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

func TestBinder_TypedSliceEqualsNamedSlice_Random(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, k := range randomKinds() {
		for i := 0; i < 200; i++ {
			n := r.IntN(6)
			bodyIn := make([]any, n)
			strIn := make([]string, n)
			for j := 0; j < n; j++ {
				bodyIn[j] = k.body(r)
				strIn[j] = k.strArr(r)
			}

			typed, ok1 := bindOneField(t, reflect.SliceOf(k.typed), false, bodyIn)
			named, ok2 := bindOneField(t, reflect.SliceOf(k.named), false, bodyIn)
			if !ok1 || !ok2 {
				return
			}
			if !reflect.DeepEqual(convertedElements(typed, k.typed), convertedElements(named, k.typed)) {
				t.Error(test.DiffMessage(named, typed, "P6: []"+k.name+" and a slice of its named type should bind equal elements from "+fmt.Sprint(bodyIn)))
				return
			}

			typedS, ok3 := bindOneField(t, reflect.SliceOf(k.typed), true, strIn)
			namedS, ok4 := bindOneField(t, reflect.SliceOf(k.named), true, strIn)
			if !ok3 || !ok4 {
				return
			}
			if !reflect.DeepEqual(convertedElements(typedS, k.typed), convertedElements(namedS, k.typed)) {
				t.Error(test.DiffMessage(convertedElements(namedS, k.typed), convertedElements(typedS, k.typed), "P6: []"+k.name+" and a slice of its named type should bind equal elements from "+strings.Join(strIn, ",")))
				return
			}
		}
	}
}

func TestBinder_TypedMapEqualsNamedMap_Random(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	keys := []string{"a", "b", "c"}
	for _, k := range randomKinds() {
		for i := 0; i < 200; i++ {
			in := map[string]any{}
			for _, key := range keys[:r.IntN(len(keys)+1)] {
				in[key] = k.body(r)
			}

			typed, ok1 := bindOneField(t, reflect.MapOf(reflect.TypeFor[string](), k.typed), false, in)
			named, ok2 := bindOneField(t, reflect.MapOf(reflect.TypeFor[string](), k.named), false, in)
			if !ok1 || !ok2 {
				return
			}
			if typed.Len() != named.Len() {
				t.Error(test.DiffMessage(named.Len(), typed.Len(), "P6: map[string]"+k.name+" and a map of its named type should bind the same keys"))
				return
			}
			for _, key := range typed.MapKeys() {
				tv := typed.MapIndex(key)
				nv := named.MapIndex(key)
				if !nv.IsValid() || !reflect.DeepEqual(tv.Interface(), nv.Convert(k.typed).Interface()) {
					t.Error(test.DiffMessage(nv, tv, "P6: map[string]"+k.name+" and a map of its named type should bind equal values"))
					return
				}
			}
		}
	}
}
