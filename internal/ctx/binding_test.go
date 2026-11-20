package ctx

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestBindStruct(t *testing.T) {
	testData := make(map[string]any)
	err := json.Unmarshal([]byte(`{
		"bool_1": true,
		"bool_2": false,
		"string_1": "string 1",
		"string_2": "string 2",
		"integer_1": 9223372036854775807,
		"integer_2": -128,
		"integer_3": -32768,
		"integer_4": -2147483648,
		"integer_5": -19223372036854775808,
		"uinteger_1": 18446744073709551615,
		"uinteger_2": 255,
		"uinteger_3": 65535,
		"uinteger_4": 4294967295,
		"uinteger_5": 18446744073709551615,
		"float_32": 1.401298464e-45,
		"float_64": 1.7976931348623157e+308,
		"complex_64": 1.401298464e-45,
		"complex_128": 18446744073709551615,
		"bool_array": [
			true,
			false
		],
		"string_array": [
			"string 1",
			"string 2"
		],
		"int_array": [
			9223372036854775807
		],
		"int8_array": [
			-128
		],
		"int16_array": [
			-32768
		],
		"int32_array": [
			-2147483648
		],
		"int64_array": [
			-19223372036854775808
		],
		"uint_array": [
			18446744073709551615
		],
		"uint8_array": [
			255
		],
		"uint16_array": [
			65535
		],
		"uint32_array": [
			4294967295
		],
		"uint64_array": [
			18446744073709551615
		],
		"float32_array": [
			1.401298464e-45
		],
		"float64_array": [
			1.7976931348623157e+308
		],
		"complex64_array": [
			1.401298464e-45
		],
		"complex128_array": [
			18446744073709551615
		],
		"3_dimensions_string_array": [
			[
				[
					"string 0 0 0",
					"string 0 0 1"
				],
				[
					"string 0 1 0",
					"string 0 1 1"
				]
			],
			[
				[
					"string 1 0 0",
					"string 1 0 1"
				],
				[
					"string 1 1 0",
					"string 1 1 1"
				]
			]
		],
		"map_string_string_array": [
			{
				"name": "John Doe",
				"gender": "Male",
				"dob": "1994-08-20"
			},
			{
				"name": "Jane Doe",
				"gender": "Female",
				"dob": "1994-08-20"
			}
		],
		"nested_struct_array": [
			{
				"name": "John Doe",
				"age": 30,
				"address": {
					"street": "123 Main St",
					"city": "Anytown",
					"zip_code": "12345"
				},
				"email": "john.doe@example.com"
			},
			{
				"name": "Alice Smith",
				"age": 25,
				"address": {
					"street": "456 Elm St",
					"city": "Sometown",
					"zip_code": "54321"
				},
				"email": "alice.smith@example.com"
			}
		],
		"struct": {
			"name": "Bob Johnson",
			"age": 35,
			"is_married": true,
			"phone_numbers": [
				{
					"type": "home",
					"value": "123-456-7890"
				},
				{
					"type": "work",
					"value": "987-654-3210"
				}
			],
			"address": {
				"street": "789 Oak St",
				"city": "Villagetown",
				"zip_code": "67890"
			}
		},
		"map_person": {
			"person_1": {
				"name": "John Doe",
				"age": 30,
				"address": {
					"street": "123 Main St",
					"city": "Anytown",
					"zip_code": "12345"
				},
				"email": "john.doe@example.com"
			},
			"person_2": {
				"name": "Jane Doe",
				"age": 32,
				"address": {
					"street": "123 Main St",
					"city": "Anytown",
					"zip_code": "12345"
				},
				"email": "john.doe@example.com"
			}
		}
	}`), &testData)

	if err != nil {
		panic(err)
	}

	d, _ := BindStruct(testData, &[]FieldLevel{}, TestDTO{}, "", "")
	bindedDTO := d.(TestDTO)

	expected1 := true
	if bindedDTO.Bool1 != expected1 {
		t.Errorf("Bool1 should %v but got %v", expected1, bindedDTO.Bool1)
	}

	expected2 := false
	if bindedDTO.Bool2 != expected2 {
		t.Errorf("Bool2 should %v but got %v", expected2, bindedDTO.Bool2)
	}
}

func TestBindStruct_Pointers(t *testing.T) {
	testData := make(map[string]any)
	err := json.Unmarshal([]byte(`{
		"ptr_bool": true,
		"ptr_int": 42,
		"ptr_int8": -12,
		"ptr_int16": -1000,
		"ptr_int32": -100000,
		"ptr_int64": -9223372036854775808,
		"ptr_uint": 99,
		"ptr_uint8": 255,
		"ptr_uint16": 65535,
		"ptr_uint32": 4294967295,
		"ptr_uint64": 12345678901234,
		"ptr_float32": 3.14,
		"ptr_float64": 1.7976931348623157e+308,
		"ptr_complex64": 2.5,
		"ptr_complex128": 9.9,
		"ptr_string": "hello",
		"ptr_struct": {"street": "1 Main St", "city": "Anytown", "zip_code": "00001"},
		"ptr_slice": ["a", "b", "c"],
		"ptr_map": {"k1": "v1", "k2": "v2"}
	}`), &testData)
	if err != nil {
		t.Fatal(err)
	}

	d, _ := BindStruct(testData, &[]FieldLevel{}, PtrDTO{}, "", "")
	dto := d.(PtrDTO)

	if dto.PtrBool == nil || *dto.PtrBool != true {
		t.Error(test.DiffMessage(dto.PtrBool, boolPtr(true), "*bool"))
	}
	if dto.PtrInt == nil || *dto.PtrInt != 42 {
		t.Error(test.DiffMessage(dto.PtrInt, intPtr(42), "*int"))
	}
	if dto.PtrInt8 == nil || *dto.PtrInt8 != -12 {
		t.Error(test.DiffMessage(dto.PtrInt8, int8Ptr(-12), "*int8"))
	}
	if dto.PtrInt16 == nil || *dto.PtrInt16 != -1000 {
		t.Error(test.DiffMessage(dto.PtrInt16, int16Ptr(-1000), "*int16"))
	}
	if dto.PtrInt32 == nil || *dto.PtrInt32 != -100000 {
		t.Error(test.DiffMessage(dto.PtrInt32, int32Ptr(-100000), "*int32"))
	}
	if dto.PtrInt64 == nil || *dto.PtrInt64 != -9223372036854775808 {
		t.Error(test.DiffMessage(dto.PtrInt64, int64Ptr(-9223372036854775808), "*int64"))
	}
	if dto.PtrUint == nil || *dto.PtrUint != 99 {
		t.Error(test.DiffMessage(dto.PtrUint, uintPtr(99), "*uint"))
	}
	if dto.PtrUint8 == nil || *dto.PtrUint8 != 255 {
		t.Error(test.DiffMessage(dto.PtrUint8, uint8Ptr(255), "*uint8"))
	}
	if dto.PtrUint16 == nil || *dto.PtrUint16 != 65535 {
		t.Error(test.DiffMessage(dto.PtrUint16, uint16Ptr(65535), "*uint16"))
	}
	if dto.PtrUint32 == nil || *dto.PtrUint32 != 4294967295 {
		t.Error(test.DiffMessage(dto.PtrUint32, uint32Ptr(4294967295), "*uint32"))
	}
	if dto.PtrUint64 == nil {
		t.Error(test.DiffMessage(dto.PtrUint64, new(uint64), "*uint64 should be non-nil"))
	}
	if dto.PtrFloat32 == nil {
		t.Error(test.DiffMessage(dto.PtrFloat32, float32Ptr(3.14), "*float32"))
	}
	if dto.PtrFloat64 == nil || *dto.PtrFloat64 != 1.7976931348623157e+308 {
		t.Error(test.DiffMessage(dto.PtrFloat64, float64Ptr(1.7976931348623157e+308), "*float64"))
	}
	if dto.PtrComplex64 == nil || *dto.PtrComplex64 != complex64(complex(2.5, 0)) {
		t.Error(test.DiffMessage(dto.PtrComplex64, new(complex64), "*complex64"))
	}
	if dto.PtrComplex128 == nil || *dto.PtrComplex128 != complex(9.9, 0) {
		t.Error(test.DiffMessage(dto.PtrComplex128, new(complex128), "*complex128"))
	}
	if dto.PtrString == nil || *dto.PtrString != "hello" {
		t.Error(test.DiffMessage(dto.PtrString, stringPtr("hello"), "*string"))
	}
	if dto.PtrStruct == nil || dto.PtrStruct.City != "Anytown" || dto.PtrStruct.Street != "1 Main St" {
		t.Error(test.DiffMessage(dto.PtrStruct, &Address{Street: "1 Main St", City: "Anytown", ZipCode: "00001"}, "*Struct"))
	}
	if dto.PtrSlice == nil || len(*dto.PtrSlice) != 3 || (*dto.PtrSlice)[0] != "a" {
		t.Error(test.DiffMessage(dto.PtrSlice, &[]string{"a", "b", "c"}, "*[]string"))
	}
	if dto.PtrMap == nil {
		t.Error(test.DiffMessage(dto.PtrMap, &map[string]string{"k1": "v1", "k2": "v2"}, "*map"))
	} else {
		m := *dto.PtrMap
		if m["k1"] != "v1" || m["k2"] != "v2" {
			t.Error(test.DiffMessage(m, map[string]string{"k1": "v1", "k2": "v2"}, "*map values"))
		}
	}
	if dto.PtrMissing != nil {
		t.Error(test.DiffMessage(dto.PtrMissing, nil, "*string missing key should be nil"))
	}
}

func TestBindStruct_Pointers_NilOnWrongType(t *testing.T) {
	testData := make(map[string]any)
	err := json.Unmarshal([]byte(`{
		"ptr_bool": "not-a-bool",
		"ptr_int": "not-a-number",
		"ptr_string": 123,
		"ptr_struct": "not-an-object",
		"ptr_slice": "not-an-array",
		"ptr_map": "not-an-object"
	}`), &testData)
	if err != nil {
		t.Fatal(err)
	}

	d, _ := BindStruct(testData, &[]FieldLevel{}, PtrDTO{}, "", "")
	dto := d.(PtrDTO)

	if dto.PtrBool != nil {
		t.Error(test.DiffMessage(dto.PtrBool, nil, "*bool wrong type should be nil"))
	}
	if dto.PtrInt != nil {
		t.Error(test.DiffMessage(dto.PtrInt, nil, "*int wrong type should be nil"))
	}
	if dto.PtrString != nil {
		t.Error(test.DiffMessage(dto.PtrString, nil, "*string wrong type should be nil"))
	}
	if dto.PtrStruct != nil {
		t.Error(test.DiffMessage(dto.PtrStruct, nil, "*struct wrong type should be nil"))
	}
	if dto.PtrSlice != nil {
		t.Error(test.DiffMessage(dto.PtrSlice, nil, "*slice wrong type should be nil"))
	}
	if dto.PtrMap != nil {
		t.Error(test.DiffMessage(dto.PtrMap, nil, "*map wrong type should be nil"))
	}
}

func TestBindStruct_InterfaceFields(t *testing.T) {
	data := map[string]any{
		"any_field":     "hello",
		"ptr_any_field": "world",
	}
	res, _ := BindStruct(data, &[]FieldLevel{}, interfaceFieldDTO{}, "", "")
	dto, ok := res.(interfaceFieldDTO)
	if !ok {
		t.Fatal(test.DiffMessage(res, interfaceFieldDTO{}, "BindStruct should return an interfaceFieldDTO"))
	}
	if dto.Any != "hello" {
		t.Error(test.DiffMessage(dto.Any, "hello", "interface field should be bound as-is"))
	}
	if dto.PtrAny == nil || *dto.PtrAny != "world" {
		t.Error(test.DiffMessage(dto.PtrAny, "world", "*interface field should be bound as a pointer to the value"))
	}
}

func TestBindStruct_FieldNotInData(t *testing.T) {
	data := map[string]any{"present": "value"}
	_, fls := BindStruct(data, &[]FieldLevel{}, missingFieldDTO{}, "", "")

	var found bool
	for _, fl := range fls {
		if fl.Tag() == "missing" {
			found = true
			if fl.IsValue() {
				t.Error(test.DiffMessage(fl.IsValue(), false, "a field absent from the data map should be reported as not a value"))
			}
		}
	}
	if !found {
		t.Error("expected a FieldLevel entry for the missing tag")
	}
}

func TestBodyBind_SliceTypeMismatchResetsToEmpty(t *testing.T) {
	got, _ := bindBody[[]int](t, []any{float64(1), "not-a-number", float64(3)})
	if len(got) != 0 {
		t.Error(test.DiffMessage(got, []int{}, "a type mismatch mid-array should reset the result to empty rather than partially fill it"))
	}
}

func TestBodyBind_SliceOfInterface(t *testing.T) {
	arr := []any{"a", float64(1), true}
	got, _ := bindBody[[]any](t, arr)
	if !reflect.DeepEqual(got, arr) {
		t.Error(test.DiffMessage(got, arr, "interface slices should pass through unchanged"))
	}
}

func TestBodyBind_NestedSlice(t *testing.T) {
	got, _ := bindBody[[][]int](t, []any{
		[]any{float64(1), float64(2)},
		[]any{float64(3)},
	})
	expected := [][]int{{1, 2}, {3}}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "nested slice dimensions should bind recursively"))
	}
}

func TestBodyBind_SliceOfStruct(t *testing.T) {
	got, _ := bindBody[[]fnArrStructDTO](t, []any{
		map[string]any{"name": "joe"},
		map[string]any{"name": "ana"},
	})
	expected := []fnArrStructDTO{{Name: "joe"}, {Name: "ana"}}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "each struct element should bind from its object"))
	}
}

func TestBodyBind_SliceOfMap(t *testing.T) {
	got, _ := bindBody[[]map[string]int](t, []any{map[string]any{"a": float64(1)}})
	expected := []map[string]int{{"a": 1}}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "a slice of maps should bind"))
	}
}

func TestBodyBind_SliceUnsupportedKindStaysNil(t *testing.T) {
	got, _ := bindBody[[]func()](t, []any{})
	if got != nil {
		t.Error(test.DiffMessage(got, nil, "B4: an unsupported element kind should leave the field nil"))
	}
}

func TestBodyBind_MapOfBool(t *testing.T) {
	got, _ := bindBody[map[string]bool](t, map[string]any{"a": true})
	if !reflect.DeepEqual(got, map[string]bool{"a": true}) {
		t.Error(test.DiffMessage(got, map[string]bool{"a": true}, "a bool map should bind"))
	}
}

func TestBodyBind_MapOfInt(t *testing.T) {
	got, _ := bindBody[map[string]int](t, map[string]any{"a": float64(42)})
	if !reflect.DeepEqual(got, map[string]int{"a": 42}) {
		t.Error(test.DiffMessage(got, map[string]int{"a": 42}, "an int map should bind"))
	}
}

func TestBodyBind_MapTypeMismatchResetsToEmpty(t *testing.T) {
	got, _ := bindBody[map[string]int](t, map[string]any{"a": float64(1), "b": "not-a-number"})
	if len(got) != 0 {
		t.Error(test.DiffMessage(got, map[string]int{}, "a type mismatch anywhere in the map should reset the result to empty"))
	}
}

func TestBodyBind_MapOfFloat64(t *testing.T) {
	got, _ := bindBody[map[string]float64](t, map[string]any{"a": 1.5})
	if !reflect.DeepEqual(got, map[string]float64{"a": 1.5}) {
		t.Error(test.DiffMessage(got, map[string]float64{"a": 1.5}, "a float64 map should bind"))
	}
}

func TestBodyBind_MapOfComplex64(t *testing.T) {
	got, _ := bindBody[map[string]complex64](t, map[string]any{"a": float64(2)})
	if !reflect.DeepEqual(got, map[string]complex64{"a": complex(2, 0)}) {
		t.Error(test.DiffMessage(got, map[string]complex64{"a": complex(2, 0)}, "a complex64 map should bind"))
	}
}

func TestBodyBind_MapOfString(t *testing.T) {
	got, _ := bindBody[map[string]string](t, map[string]any{"a": "hi"})
	if !reflect.DeepEqual(got, map[string]string{"a": "hi"}) {
		t.Error(test.DiffMessage(got, map[string]string{"a": "hi"}, "a string map should bind"))
	}
}

func TestBodyBind_MapOfInterface(t *testing.T) {
	obj := map[string]any{"a": "hi"}
	got, _ := bindBody[map[string]any](t, obj)
	if !reflect.DeepEqual(got, obj) {
		t.Error(test.DiffMessage(got, obj, "an interface-valued map should pass through unchanged"))
	}
}

func TestBodyBind_MapOfSlice(t *testing.T) {
	got, _ := bindBody[map[string][]int](t, map[string]any{"a": []any{float64(1), float64(2)}})
	expected := map[string][]int{"a": {1, 2}}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "a map of slices should bind"))
	}
}

func TestBodyBind_MapOfStruct(t *testing.T) {
	got, _ := bindBody[map[string]fnArrStructDTO](t, map[string]any{"a": map[string]any{"name": "joe"}})
	expected := map[string]fnArrStructDTO{"a": {Name: "joe"}}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "a map of structs should bind"))
	}
}

func TestBodyBind_NestedMapOfBool(t *testing.T) {
	got, _ := bindBody[map[string]map[string]bool](t, map[string]any{"a": map[string]any{"x": true}})
	expected := map[string]map[string]bool{"a": {"x": true}}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "a nested map of maps should bind"))
	}
}

func TestBodyBind_NestedMapOfSlice(t *testing.T) {
	got, _ := bindBody[map[string]map[string][]int](t, map[string]any{"a": map[string]any{"x": []any{float64(1)}}})
	expected := map[string]map[string][]int{"a": {"x": {1}}}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "a nested map of slices should bind"))
	}
}

func TestBodyBind_NestedMapOfStruct(t *testing.T) {
	got, _ := bindBody[map[string]map[string]fnArrStructDTO](t, map[string]any{"a": map[string]any{"x": map[string]any{"name": "joe"}}})
	expected := map[string]map[string]fnArrStructDTO{"a": {"x": {Name: "joe"}}}
	if !reflect.DeepEqual(got, expected) {
		t.Error(test.DiffMessage(got, expected, "a nested map of structs should bind"))
	}
}

func TestBodyBind_MapUnsupportedKindStaysNil(t *testing.T) {
	got, _ := bindBody[map[string]func()](t, map[string]any{})
	if got != nil {
		t.Error(test.DiffMessage(got, nil, "B4: an unsupported element kind should leave the field nil"))
	}
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
