package core

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"unsafe"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/event"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/trace"
)

type mockProvider struct{}

func (m *mockProvider) NewProvider() Provider { return m }

type mockController struct{}

func (m *mockController) NewController() Controller { return m }

func TestGetPkgFromControllerKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"[123]github.com/foo/bar.Controller", "github.com/foo/bar.Controller"},
		{"[999]pkg.Type", "pkg.Type"},
		{"noBrackets", "noBrackets"},
		{"[a][b]pkg.Type", "pkg.Type"},
	}
	for _, c := range cases {
		got := getPkgFromControllerKey(c.in)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "getPkgFromControllerKey("+c.in+")"))
		}
	}
}

func TestGenFieldKey(t *testing.T) {
	t1 := reflect.TypeOf(mockProvider{})
	got := genFieldKey(t1)
	want := t1.PkgPath() + "/" + t1.String()
	if got != want {
		t.Error(test.DiffMessage(got, want, "genFieldKey"))
	}
}

func TestGenProviderKey(t *testing.T) {
	p := &mockProvider{}
	got := genProviderKey(p)
	want := genFieldKey(reflect.TypeOf(p))
	if got != want {
		t.Error(test.DiffMessage(got, want, "genProviderKey"))
	}
}

func TestIsInjectedProvider(t *testing.T) {
	providerType := reflect.TypeOf(mockProvider{})
	notProviderType := reflect.TypeOf(struct{}{})

	if !isInjectedProvider(providerType) {
		t.Error(test.DiffMessage(false, true, "mockProvider should be injectable"))
	}
	if isInjectedProvider(notProviderType) {
		t.Error(test.DiffMessage(true, false, "anonymous struct should not be injectable"))
	}
}

func TestSetStatusCodeInt(t *testing.T) {
	c := newHTTPContext()
	setStatusCode(c, reflect.ValueOf(http.StatusOK))
	if c.Code != http.StatusOK {
		t.Error(test.DiffMessage(c.Code, http.StatusOK, "setStatusCode reflect.Int"))
	}
}

func TestSetStatusCodeInvalidInt(t *testing.T) {
	c := newHTTPContext()
	c.Status(http.StatusTeapot)
	setStatusCode(c, reflect.ValueOf(9999))
	if c.Code != http.StatusTeapot {
		t.Error(test.DiffMessage(c.Code, http.StatusTeapot, "setStatusCode invalid int should not change status"))
	}
}

func TestSetStatusCodeInterfaceValid(t *testing.T) {
	c := newHTTPContext()
	var v any = http.StatusCreated
	setStatusCode(c, reflect.ValueOf(v))
	if c.Code != http.StatusCreated {
		t.Error(test.DiffMessage(c.Code, http.StatusCreated, "setStatusCode interface valid int"))
	}
}

func TestSetStatusCodeInterfaceInvalid(t *testing.T) {
	c := newHTTPContext()
	c.Status(http.StatusTeapot)
	var v any = "not-an-int"
	setStatusCode(c, reflect.ValueOf(v))
	if c.Code != http.StatusTeapot {
		t.Error(test.DiffMessage(c.Code, http.StatusTeapot, "setStatusCode interface non-int should not change status"))
	}
}

func TestReturnHTTPString(t *testing.T) {
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)
	returnHTTP(c, reflect.ValueOf("hello"))
	if w.Body.String() != "hello" {
		t.Error(test.DiffMessage(w.Body.String(), "hello", "returnHTTP string"))
	}
}

func TestReturnHTTPMap(t *testing.T) {
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)
	returnHTTP(c, reflect.ValueOf(map[string]any{"k": "v"}))
	if w.Body.Len() == 0 {
		t.Error(test.DiffMessage(0, ">0", "returnHTTP map should produce JSON body"))
	}
}

func TestReturnHTTPInt(t *testing.T) {
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)
	returnHTTP(c, reflect.ValueOf(42))
	if w.Body.String() != "42" {
		t.Error(test.DiffMessage(w.Body.String(), "42", "returnHTTP int"))
	}
}

func TestReturnHTTPBool(t *testing.T) {
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)
	returnHTTP(c, reflect.ValueOf(true))
	if w.Body.String() != "true" {
		t.Error(test.DiffMessage(w.Body.String(), "true", "returnHTTP bool"))
	}
}

func TestReturnHTTPSlice(t *testing.T) {
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)
	returnHTTP(c, reflect.ValueOf([]int{1, 2, 3}))
	if w.Body.Len() == 0 {
		t.Error(test.DiffMessage(0, ">0", "returnHTTP slice should produce JSON body"))
	}
}

type fnTestProvider struct{ Tag string }

func (p fnTestProvider) NewProvider() Provider { return p }

func TestReturnHTTPFloat(t *testing.T) {
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)
	returnHTTP(c, reflect.ValueOf(3.5))
	if w.Body.String() != "3.5" {
		t.Error(test.DiffMessage(w.Body.String(), "3.5", "returnHTTP float64"))
	}
}

func TestReturnHTTPUint(t *testing.T) {
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)
	returnHTTP(c, reflect.ValueOf(uint(7)))
	if w.Body.String() != "7" {
		t.Error(test.DiffMessage(w.Body.String(), "7", "returnHTTP uint"))
	}
}

func TestBuildUseMiddleware_NoListener_StillCallsNext(t *testing.T) {
	ev := event.NewEvent()
	c := newHTTPContext()
	var nextCalled bool
	c.Next = func() { nextCalled = true }
	mw := buildUseMiddleware(func(r *http.Request, w http.ResponseWriter, next ctx.Next) { next() }, ev, "Test.Middleware", trace.TransportHTTP)
	mw(c)
	if !nextCalled {
		t.Error(test.DiffMessage(false, true, "middleware should still call next when no listener is attached"))
	}
}

func TestBuildUseMiddleware_EmitsOnlyWhenListenerAttached(t *testing.T) {
	ev := event.NewEvent()
	c := newHTTPContext()
	c.Next = func() {}
	var got *trace.Event
	ev.On(trace.EventName, func(args ...any) {
		te := args[0].(trace.Event)
		got = &te
	})
	mw := buildUseMiddleware(func(r *http.Request, w http.ResponseWriter, next ctx.Next) { next() }, ev, "Test.Middleware", trace.TransportHTTP)
	mw(c)
	if got == nil {
		t.Fatal("expected a trace event to be emitted when a listener is attached")
	}
	if got.Stage != trace.StageMiddleware || got.Name != "Test.Middleware" || got.Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage([]any{got.Stage, got.Name, got.Transport}, []any{trace.StageMiddleware, "Test.Middleware", trace.TransportHTTP}, "buildUseMiddleware emitted event fields"))
	}
}

type injectDepsTestProvider struct{}

func (injectDepsTestProvider) NewProvider() Provider { return injectDepsTestProvider{} }

type injectDepsComponent struct {
	Provider injectDepsTestProvider
	Plain    string
}

func TestInjectDependencies_ResolvesFromLocalProviders(t *testing.T) {
	p := injectDepsTestProvider{}
	injectedProviders := map[string]Provider{
		genFieldKey(reflect.TypeOf(p)): p,
	}
	component := injectDepsComponent{Plain: "kept"}
	result, err := injectDependencies(component, "provider", injectedProviders)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := result.Elem().Interface().(injectDepsComponent)
	if got.Plain != "kept" {
		t.Error(test.DiffMessage(got.Plain, "kept", "injectDependencies should pass through non-Provider fields unchanged"))
	}
}

type injectDepsUnresolvable struct {
	Missing unresolvableProviderType
}

type unresolvableProviderType struct{}

func (unresolvableProviderType) NewProvider() Provider { return unresolvableProviderType{} }

func TestInjectDependencies_UnresolvedProviderReturnsError(t *testing.T) {
	_, err := injectDependencies(injectDepsUnresolvable{}, "provider", map[string]Provider{})
	if err == nil {
		t.Error(test.DiffMessage(nil, "error", "injectDependencies should error when a Provider field can't be resolved"))
	}
}

type returnUser struct {
	Name string `json:"name"`
}

func anyValue(v any) reflect.Value {
	return reflect.ValueOf(&v).Elem()
}

func writeReturn(t *testing.T, c *ctx.HTTPContext, v reflect.Value) {
	t.Helper()
	defer func() {
		if rec := recover(); rec != nil {
			t.Error(test.DiffMessage(rec, "no panic", "returnHTTP must not panic on a result a handler can return"))
		}
	}()
	returnHTTP(c, v)
}

func recorderOf(c *ctx.HTTPContext) *httptest.ResponseRecorder {
	return c.ResponseWriter.(*httptest.ResponseRecorder)
}

func TestReturnHTTP_InterfaceString(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, anyValue("ok"))
	w := recorderOf(c)
	if w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Body.String(), "ok", "B18: an any holding a string is written as text"))
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Error(test.DiffMessage(got, "text/plain; charset=utf-8", "B18: an any holding a string is text/plain"))
	}
}

func TestReturnHTTP_InterfaceStringThatIsNotJSON(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, anyValue("not json {"))
	w := recorderOf(c)
	if w.Body.String() != "not json {" {
		t.Error(test.DiffMessage(w.Body.String(), "not json {", "B18: text that is not JSON must not reach the JSON encoder"))
	}
}

func TestReturnHTTP_InterfaceInt(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, anyValue(42))
	w := recorderOf(c)
	if w.Body.String() != "42" {
		t.Error(test.DiffMessage(w.Body.String(), "42", "B18 (Q2): an any holding an int keeps its body"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B18 (Q2): an any holding an int keeps its JSON content type"))
	}
}

func TestReturnHTTP_InterfaceBool(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, anyValue(true))
	w := recorderOf(c)
	if w.Body.String() != "true" {
		t.Error(test.DiffMessage(w.Body.String(), "true", "B18 (Q2): an any holding a bool keeps its body"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B18 (Q2): an any holding a bool keeps its JSON content type"))
	}
}

func TestReturnHTTP_InterfaceFloat(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, anyValue(100000000.0))
	w := recorderOf(c)
	if w.Body.String() != "100000000" {
		t.Error(test.DiffMessage(w.Body.String(), "100000000", "B18 (Q2): an any holding a float keeps JSON digits"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B18 (Q2): an any holding a float keeps its JSON content type"))
	}
}

func TestReturnHTTP_InterfaceMap(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, anyValue(map[string]any{"k": "v"}))
	w := recorderOf(c)
	if w.Body.String() != `{"k":"v"}` {
		t.Error(test.DiffMessage(w.Body.String(), `{"k":"v"}`, "B18: an any holding a map is JSON"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B18: an any holding a map is application/json"))
	}
}

func TestReturnHTTP_InterfacePointer(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, anyValue(&returnUser{Name: "a"}))
	w := recorderOf(c)
	if w.Body.String() != `{"name":"a"}` {
		t.Error(test.DiffMessage(w.Body.String(), `{"name":"a"}`, "B18, B19: an any holding a pointer is the pointee's JSON"))
	}
}

func TestReturnHTTP_NilInterface(t *testing.T) {
	c := newHTTPContext()
	var v any
	writeReturn(t, c, reflect.ValueOf(&v).Elem())
	w := recorderOf(c)
	if w.Body.String() != "null" {
		t.Error(test.DiffMessage(w.Body.String(), "null", "B18: a nil interface writes null"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B18: a nil interface is application/json"))
	}
}

func TestReturnHTTP_InvalidValueWritesNothing(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, reflect.ValueOf(nil))
	w := recorderOf(c)
	if w.Body.Len() != 0 {
		t.Error(test.DiffMessage(w.Body.String(), "", "B18: an invalid Value writes no body"))
	}
	if got := w.Header().Get("Content-Type"); got != "" {
		t.Error(test.DiffMessage(got, "", "B18: an invalid Value sets no content type"))
	}
}

func TestReturnHTTP_Struct(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, reflect.ValueOf(returnUser{Name: "a"}))
	w := recorderOf(c)
	if w.Body.String() != `{"name":"a"}` {
		t.Error(test.DiffMessage(w.Body.String(), `{"name":"a"}`, "B19: a struct is JSON"))
	}
}

func TestReturnHTTP_Array(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, reflect.ValueOf([3]int{1, 2, 3}))
	w := recorderOf(c)
	if w.Body.String() != "[1,2,3]" {
		t.Error(test.DiffMessage(w.Body.String(), "[1,2,3]", "B19: an array is JSON"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B19: an array is application/json"))
	}
}

func TestReturnHTTP_PointerToStruct(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, reflect.ValueOf(&returnUser{Name: "a"}))
	w := recorderOf(c)
	if w.Body.String() != `{"name":"a"}` {
		t.Error(test.DiffMessage(w.Body.String(), `{"name":"a"}`, "B19: a pointer is the pointee's JSON, never an address"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B19: a pointer is application/json"))
	}
}

func TestReturnHTTP_NilPointer(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, reflect.ValueOf((*returnUser)(nil)))
	w := recorderOf(c)
	if w.Body.String() != "null" {
		t.Error(test.DiffMessage(w.Body.String(), "null", "B19: a nil pointer writes null"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B19: a nil pointer is application/json"))
	}
}

func TestReturnHTTP_Func(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, reflect.ValueOf(func() {}))
	w := recorderOf(c)
	if w.Body.String() != "func()" {
		t.Error(test.DiffMessage(w.Body.String(), "func()", "B19: a func writes its type"))
	}
}

func TestReturnHTTP_Complex(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, reflect.ValueOf(complex(1, 2)))
	w := recorderOf(c)
	if w.Body.String() != "(1+2i)" {
		t.Error(test.DiffMessage(w.Body.String(), "(1+2i)", "B19: a complex number is text"))
	}
}

func TestReturnHTTP_FloatUsesGFormat(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, reflect.ValueOf(100000000.0))
	w := recorderOf(c)
	if w.Body.String() != "1e+08" {
		t.Error(test.DiffMessage(w.Body.String(), "1e+08", "B19: a float is text in strconv 'g' format"))
	}
}

func TestReturnHTTP_UnsafePointerWritesNothing(t *testing.T) {
	c := newHTTPContext()
	x := 1
	writeReturn(t, c, reflect.ValueOf(unsafe.Pointer(&x)))
	w := recorderOf(c)
	if w.Body.Len() != 0 {
		t.Error(test.DiffMessage(w.Body.String(), "", "B19: an unsafe pointer writes nothing, never its address"))
	}
}

func TestReturnHTTP_ChanWritesNothing(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, reflect.ValueOf(make(chan int)))
	w := recorderOf(c)
	if w.Body.Len() != 0 {
		t.Error(test.DiffMessage(w.Body.String(), "", "B19: a chan writes nothing"))
	}
}

func TestReturnHTTP_UintptrWritesNothing(t *testing.T) {
	c := newHTTPContext()
	writeReturn(t, c, reflect.ValueOf(uintptr(1)))
	w := recorderOf(c)
	if w.Body.Len() != 0 {
		t.Error(test.DiffMessage(w.Body.String(), "", "B19: a uintptr writes nothing"))
	}
}
