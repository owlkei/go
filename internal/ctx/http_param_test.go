package ctx

import (
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestHTTPContext_Param(t *testing.T) {
	c := newTestHTTPContext()
	HTTPSetParams(c, map[string][]int{"id": {0}, "name": {1}}, []string{"42", "joe"})

	p := c.Param()
	if p.Get("id") != "42" {
		t.Error(test.DiffMessage(p.Get("id"), "42", "Param should map ParamKeys indexes into ParamValues"))
	}
	if p.Get("name") != "joe" {
		t.Error(test.DiffMessage(p.Get("name"), "joe", "Param should map ParamKeys indexes into ParamValues"))
	}
}

func TestHTTPContext_ParamIsCached(t *testing.T) {
	c := newTestHTTPContext()
	HTTPSetParams(c, map[string][]int{"id": {0}}, []string{"42"})

	p1 := c.Param()
	p1.Set("id", "mutated")
	p2 := c.Param()

	if p2.Get("id") != "mutated" {
		t.Error(test.DiffMessage(p2.Get("id"), "mutated", "Param should be cached across calls on the same context"))
	}
}

func TestParam_GetOnNilMap(t *testing.T) {
	var p Param
	if p.Get("missing") != "" {
		t.Error(test.DiffMessage(p.Get("missing"), "", "Get on a nil Param should return empty string"))
	}
}

func TestParam_GetMissingKey(t *testing.T) {
	p := Param{}
	if p.Get("missing") != "" {
		t.Error(test.DiffMessage(p.Get("missing"), "", "Get on missing key should return empty string"))
	}
}

func TestParam_SetAddDelHas(t *testing.T) {
	p := Param{}
	p.Set("id", "1")
	if p.Get("id") != "1" {
		t.Error(test.DiffMessage(p.Get("id"), "1", "Set should store the value"))
	}

	p.Add("id", "2")
	if got := p["id"]; len(got) != 2 || got[1] != "2" {
		t.Error(test.DiffMessage(got, []string{"1", "2"}, "Add should append to the existing values"))
	}

	if !p.Has("id") {
		t.Error(test.DiffMessage(p.Has("id"), true, "Has should be true after Set"))
	}

	p.Del("id")
	if p.Has("id") {
		t.Error(test.DiffMessage(p.Has("id"), false, "Has should be false after Del"))
	}
}

type paramBindDTO struct {
	ID string `bind:"id"`
}

func TestParam_Bind(t *testing.T) {
	p := Param{"id": {"42"}}
	result, fls := p.Bind(paramBindDTO{})

	dto, ok := result.(paramBindDTO)
	if !ok {
		t.Fatal(test.DiffMessage(result, paramBindDTO{}, "Bind should return a paramBindDTO"))
	}
	if dto.ID != "42" {
		t.Error(test.DiffMessage(dto.ID, "42", "Bind should populate fields from param values"))
	}
	if len(fls) != 1 {
		t.Error(test.DiffMessage(len(fls), 1, "Bind should report a FieldLevel per bound field"))
	}
}

func TestHTTPContext_ParamValuesAreCopied(t *testing.T) {
	c := newTestHTTPContext()
	HTTPSetParams(c, map[string][]int{"id": {0}, "tags": {1, 2}}, []string{"42", "a", "b"})

	p := c.Param()

	c.paramValues[0] = "changed"
	c.paramValues[1] = "changed"
	c.paramValues = c.paramValues[:0]
	if p.Get("id") != "42" || len(p["tags"]) != 2 || p["tags"][0] != "a" || p["tags"][1] != "b" {
		t.Error(test.DiffMessage(p, Param{"id": {"42"}, "tags": {"a", "b"}}, "B33: Param should stay valid when the router reuses its slice"))
	}

	p["tags"][0] = "mutated"
	if c.paramValues[:3][1] == "mutated" {
		t.Error(test.DiffMessage(c.paramValues[:3][1], "changed", "B33: writing to a Param value should not write to ParamValues"))
	}
}

func TestParam_AddDoesNotCorruptSiblingKey(t *testing.T) {
	c := newTestHTTPContext()
	HTTPSetParams(c, map[string][]int{"a": {0}, "b": {1}, "c": {2, 3}}, []string{"1", "2", "3", "4"})

	p := c.Param()
	p.Add("a", "x")
	p.Add("b", "y")
	p.Add("c", "z")

	expected := Param{"a": {"1", "x"}, "b": {"2", "y"}, "c": {"3", "4", "z"}}
	if !reflect.DeepEqual(p, expected) {
		t.Error(test.DiffMessage(p, expected, "P9: appending to one parameter should never overwrite another's values"))
	}
}

func TestHTTPContext_ParamMultipleValuesKeepOrder(t *testing.T) {
	c := newTestHTTPContext()
	HTTPSetParams(c, map[string][]int{"a": {2, 0}}, []string{"x", "y", "z"})
	got := c.Param()["a"]
	if !reflect.DeepEqual(got, []string{"z", "x"}) {
		t.Error(test.DiffMessage(got, []string{"z", "x"}, "B33: values should follow the order of ParamKeys"))
	}
}

func TestHTTPContext_ParamIndexOutOfRangeIsSkipped(t *testing.T) {
	c := newTestHTTPContext()
	HTTPSetParams(c, map[string][]int{"id": {0}, "bad": {5}, "neg": {-1}, "mix": {0, 9}}, []string{"42"})

	var p Param
	if !noPanic(t, "B35 Param with indexes outside ParamValues", func() { p = c.Param() }) {
		return
	}
	if p.Get("id") != "42" {
		t.Error(test.DiffMessage(p.Get("id"), "42", "B35: valid indexes should still bind"))
	}
	if p.Get("bad") != "" || p.Get("neg") != "" {
		t.Error(test.DiffMessage([2]string{p.Get("bad"), p.Get("neg")}, [2]string{"", ""}, "B35: an index outside ParamValues should be skipped"))
	}
	if !reflect.DeepEqual(p["mix"], []string{"42"}) {
		t.Error(test.DiffMessage(p["mix"], []string{"42"}, "B35: only the valid index of a mixed key should bind"))
	}
}

func TestHTTPContext_ParamWithoutKeysIsEmptyNotNil(t *testing.T) {
	c := newTestHTTPContext()
	p := c.Param()
	if p == nil || len(p) != 0 {
		t.Error(test.DiffMessage(p, Param{}, "B33: Param should never be nil"))
	}
	noPanic(t, "B33 Set on an empty Param", func() { p.Set("k", "v") })
}

func TestParam_BindValueFromRequest(t *testing.T) {
	c := newTestHTTPContext()
	HTTPSetParams(c, map[string][]int{"id": {0}}, []string{"42"})
	got := c.Param().BindValue(paramBindDTO{})
	if dto, ok := got.(paramBindDTO); !ok || dto.ID != "42" {
		t.Error(test.DiffMessage(got, paramBindDTO{ID: "42"}, "BindValue should bind the request's parameters"))
	}
}
