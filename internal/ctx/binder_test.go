package ctx

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

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
	if ErrBindTarget.Error() != "owlkei: bind target must be a struct" {
		t.Error(test.DiffMessage(ErrBindTarget.Error(), "owlkei: bind target must be a struct", "ErrBindTarget message"))
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
