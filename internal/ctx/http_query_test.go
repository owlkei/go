package ctx

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestHTTPContext_Query(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", "/?foo=bar&foo=baz", nil)

	q := c.Query()
	if q.Get("foo") != "bar" {
		t.Error(test.DiffMessage(q.Get("foo"), "bar", "Query should parse the request URL query string"))
	}
}

func TestHTTPContext_QueryIsCached(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", "/?foo=bar", nil)

	q1 := c.Query()
	q1.Set("foo", "mutated")
	q2 := c.Query()

	if q2.Get("foo") != "mutated" {
		t.Error(test.DiffMessage(q2.Get("foo"), "mutated", "Query should be cached across calls on the same context"))
	}
}

func TestQuery_GetMissingKey(t *testing.T) {
	q := Query{}
	if q.Get("missing") != "" {
		t.Error(test.DiffMessage(q.Get("missing"), "", "Get on missing key should return empty string"))
	}
}

func TestQuery_SetAddDelHas(t *testing.T) {
	q := Query{}
	q.Set("a", "1")
	if q.Get("a") != "1" {
		t.Error(test.DiffMessage(q.Get("a"), "1", "Set should store the value"))
	}

	q.Add("a", "2")
	if len(q["a"]) != 2 || q["a"][1] != "2" {
		t.Error(test.DiffMessage(q["a"], []string{"1", "2"}, "Add should append to the existing values"))
	}

	if !q.Has("a") {
		t.Error(test.DiffMessage(q.Has("a"), true, "Has should be true after Set"))
	}

	q.Del("a")
	if q.Has("a") {
		t.Error(test.DiffMessage(q.Has("a"), false, "Has should be false after Del"))
	}
}

func TestQuery_Bind(t *testing.T) {
	q := Query{"name": {"joe"}, "age": {"30"}}
	result, fls := q.Bind(queryBindDTO{})

	dto, ok := result.(queryBindDTO)
	if !ok {
		t.Fatal(test.DiffMessage(result, queryBindDTO{}, "Bind should return a queryBindDTO"))
	}
	if dto.Name != "joe" || dto.Age != 30 {
		t.Error(test.DiffMessage(dto, queryBindDTO{Name: "joe", Age: 30}, "Bind should populate fields from query values"))
	}
	if len(fls) != 2 {
		t.Error(test.DiffMessage(len(fls), 2, "Bind should report a FieldLevel per bound field"))
	}
}

func TestQuery_Bind_OutOfRangeNumberIsZero(t *testing.T) {
	q := Query{
		"int":     {"99999999999999999999"},
		"int64":   {"-99999999999999999999"},
		"uint64":  {"99999999999999999999"},
		"float64": {"1e400"},
		"ints":    {"99999999999999999999", "7"},
		"floats":  {"1e400", "2.5"},
	}
	result, _ := q.Bind(queryRangeDTO{})
	dto := result.(queryRangeDTO)

	expected := queryRangeDTO{Ints: []int{0, 7}, Floats: []float64{0, 2.5}}
	if dto.Int != 0 || dto.Int64 != 0 || dto.Uint64 != 0 || dto.Float64 != 0 {
		t.Error(test.DiffMessage(dto, expected, "an out-of-range number must leave the field zero, never a saturated value"))
	}
	if len(dto.Ints) != 2 || dto.Ints[0] != 0 || dto.Ints[1] != 7 {
		t.Error(test.DiffMessage(dto.Ints, expected.Ints, "an out-of-range element must be zero and the others bound"))
	}
	if len(dto.Floats) != 2 || dto.Floats[0] != 0 || dto.Floats[1] != 2.5 {
		t.Error(test.DiffMessage(dto.Floats, expected.Floats, "an out-of-range float element must be zero, not +Inf"))
	}
}

func TestQuery_Bind_MalformedNumberIsZero(t *testing.T) {
	q := Query{"int": {"12abc"}, "float64": {"x"}}
	result, _ := q.Bind(queryRangeDTO{})
	dto := result.(queryRangeDTO)
	if dto.Int != 0 || dto.Float64 != 0 {
		t.Error(test.DiffMessage(dto, queryRangeDTO{}, "a malformed number must leave the field zero"))
	}
}

func TestQuery_BindValueEqualsBind(t *testing.T) {
	q := Query{"name": {"joe"}, "age": {"30"}}
	want, _ := q.Bind(bindValueDTO{})
	if got := q.BindValue(bindValueDTO{}); !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "BindValue should return Bind's value"))
	}
}
