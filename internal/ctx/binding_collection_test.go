package ctx

import (
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

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
