package ctx

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestHTTPContext_Header(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("X-Foo", "bar")

	h := c.Header()
	if h.Get("X-Foo") != "bar" {
		t.Error(test.DiffMessage(h.Get("X-Foo"), "bar", "Header should wrap the request headers"))
	}
}

func TestHTTPContext_HeaderIsCached(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", "/", nil)

	h1 := c.Header()
	h1.Set("X-Foo", "mutated")
	h2 := c.Header()

	if h2.Get("X-Foo") != "mutated" {
		t.Error(test.DiffMessage(h2.Get("X-Foo"), "mutated", "Header should be cached across calls on the same context"))
	}
}

func TestHeader_GetMissingKey(t *testing.T) {
	h := Header{}
	if h.Get("missing") != "" {
		t.Error(test.DiffMessage(h.Get("missing"), "", "Get on missing key should return empty string"))
	}
}

func TestHeader_SetAddDelHas(t *testing.T) {
	h := Header{}
	h.Set("X-Foo", "1")
	if h.Get("X-Foo") != "1" {
		t.Error(test.DiffMessage(h.Get("X-Foo"), "1", "Set should store the value"))
	}

	h.Add("X-Foo", "2")
	if got := h["X-Foo"]; len(got) != 2 || got[1] != "2" {
		t.Error(test.DiffMessage(got, []string{"1", "2"}, "Add should append to the existing values"))
	}

	if !h.Has("X-Foo") {
		t.Error(test.DiffMessage(h.Has("X-Foo"), true, "Has should be true after Set"))
	}

	h.Del("X-Foo")
	if h.Has("X-Foo") {
		t.Error(test.DiffMessage(h.Has("X-Foo"), false, "Has should be false after Del"))
	}
}

func TestHeader_HasCanonicalizesKey(t *testing.T) {
	h := Header{}
	h.Set("x-foo", "1")
	if !h.Has("X-Foo") {
		t.Error(test.DiffMessage(h.Has("X-Foo"), true, "Has should canonicalize the header key before lookup"))
	}
}

func TestHeader_Bind(t *testing.T) {
	h := Header{}
	h.Set("Authorization", "Bearer token")
	result, fls := h.Bind(headerBindDTO{})

	dto, ok := result.(headerBindDTO)
	if !ok {
		t.Fatal(test.DiffMessage(result, headerBindDTO{}, "Bind should return a headerBindDTO"))
	}
	if dto.Auth != "Bearer token" {
		t.Error(test.DiffMessage(dto.Auth, "Bearer token", "Bind should populate fields from header values"))
	}
	if len(fls) != 1 {
		t.Error(test.DiffMessage(len(fls), 1, "Bind should report a FieldLevel per bound field"))
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
