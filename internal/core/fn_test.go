package core

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/trace"
)

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
	if ctx.HTTPCode(c) != http.StatusOK {
		t.Error(test.DiffMessage(ctx.HTTPCode(c), http.StatusOK, "setStatusCode reflect.Int"))
	}
}

func TestSetStatusCodeInvalidInt(t *testing.T) {
	c := newHTTPContext()
	c.Status(http.StatusTeapot)
	setStatusCode(c, reflect.ValueOf(9999))
	if ctx.HTTPCode(c) != http.StatusTeapot {
		t.Error(test.DiffMessage(ctx.HTTPCode(c), http.StatusTeapot, "setStatusCode invalid int should not change status"))
	}
}

func TestSetStatusCodeInterfaceValid(t *testing.T) {
	c := newHTTPContext()
	var v any = http.StatusCreated
	setStatusCode(c, reflect.ValueOf(v))
	if ctx.HTTPCode(c) != http.StatusCreated {
		t.Error(test.DiffMessage(ctx.HTTPCode(c), http.StatusCreated, "setStatusCode interface valid int"))
	}
}

func TestSetStatusCodeInterfaceInvalid(t *testing.T) {
	c := newHTTPContext()
	c.Status(http.StatusTeapot)
	var v any = "not-an-int"
	setStatusCode(c, reflect.ValueOf(v))
	if ctx.HTTPCode(c) != http.StatusTeapot {
		t.Error(test.DiffMessage(ctx.HTTPCode(c), http.StatusTeapot, "setStatusCode interface non-int should not change status"))
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

func TestInjectDependencies_ResolvesFromLocalProviders(t *testing.T) {
	p := injectDepsTestProvider{}
	injectedProviders := map[string]Provider{
		genFieldKey(reflect.TypeOf(p)): p,
	}
	component := injectDepsComponent{Plain: "kept"}
	result, err := injectDependencies(component, "provider", injectedProviders, newContainer())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := result.Elem().Interface().(injectDepsComponent)
	if got.Plain != "kept" {
		t.Error(test.DiffMessage(got.Plain, "kept", "injectDependencies should pass through non-Provider fields unchanged"))
	}
}

func TestInjectDependencies_UnresolvedProviderReturnsError(t *testing.T) {
	_, err := injectDependencies(injectDepsUnresolvable{}, "provider", map[string]Provider{}, newContainer())
	if err == nil {
		t.Error(test.DiffMessage(nil, "error", "injectDependencies should error when a Provider field can't be resolved"))
	}
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

func TestApp_HandlerReturningPointerWritesJSON(t *testing.T) {
	w := serveResult(t, "/pointer")
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "bug 2, B19: status"))
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"name":"ann"}` {
		t.Error(test.DiffMessage(got, `{"name":"ann"}`, "bug 2, B19: a pointer result is JSON, never an address"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "bug 2, B19: content type"))
	}
}

func TestApp_HandlerReturningStatusAndPointerWritesJSON(t *testing.T) {
	w := serveResult(t, "/created")
	if w.Code != http.StatusAccepted {
		t.Error(test.DiffMessage(w.Code, http.StatusAccepted, "bug 2, B20: the first result sets the status"))
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"name":"bob"}` {
		t.Error(test.DiffMessage(got, `{"name":"bob"}`, "bug 2, B19: the second result is JSON"))
	}
}

func TestApp_HandlerReturningArrayWritesJSON(t *testing.T) {
	w := serveResult(t, "/array")
	if got := strings.TrimSpace(w.Body.String()); got != "[1,2,3]" {
		t.Error(test.DiffMessage(got, "[1,2,3]", "bug 4, B19: an array result is JSON"))
	}
}

func TestApp_HandlerReturningAnyStringWritesText(t *testing.T) {
	w := serveResult(t, "/anystring")
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "bug 3, B18: status"))
	}
	if got := w.Body.String(); got != "ok" {
		t.Error(test.DiffMessage(got, "ok", "bug 3, B18: an any holding a string is text"))
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Error(test.DiffMessage(got, "text/plain; charset=utf-8", "bug 3, B18: content type"))
	}
}

func TestApp_HandlerReturningAnyIntKeepsJSONContentType(t *testing.T) {
	w := serveResult(t, "/anyint")
	if got := w.Body.String(); got != "7" {
		t.Error(test.DiffMessage(got, "7", "B18 (Q2): an any holding an int keeps its body"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B18 (Q2): an any holding an int keeps its content type"))
	}
}

func TestCompleteEvent_ClientIP_Direct(t *testing.T) {
	events := &clientIPEvents{}
	app := clientIPApp(t, events, false)
	clientIPServe(app, "203.0.113.5:5555", "X-Forwarded-For", "198.51.100.1", "X-Real-IP", "198.51.100.2")
	got := events.complete(t).ClientIP
	if want := netip.MustParseAddr("203.0.113.5"); got != want {
		t.Error(test.DiffMessage(got, want, "A1: with no proxy the complete event carries the peer, whatever the headers say"))
	}
}

func TestCompleteEvent_ClientIP_BehindProxy(t *testing.T) {
	events := &clientIPEvents{}
	app := clientIPApp(t, events, true)
	clientIPServe(app, "10.0.0.9:80", "X-Forwarded-For", "198.51.100.20")
	got := events.complete(t).ClientIP
	if want := netip.MustParseAddr("198.51.100.20"); got != want {
		t.Error(test.DiffMessage(got, want, "A1: behind a trusted proxy the complete event carries the client"))
	}
}

func TestCompleteEvent_ClientIP_UntrustedPeerBehindProxyMiddleware(t *testing.T) {
	events := &clientIPEvents{}
	app := clientIPApp(t, events, true)
	clientIPServe(app, "203.0.113.5:5555", "X-Forwarded-For", "198.51.100.20")
	got := events.complete(t).ClientIP
	if want := netip.MustParseAddr("203.0.113.5"); got != want {
		t.Error(test.DiffMessage(got, want, "A1, P3: an untrusted peer's headers never reach the event"))
	}
}

func TestCompleteEvent_ClientIP_ZeroOnStageEvents(t *testing.T) {
	events := &clientIPEvents{}
	app := clientIPApp(t, events, false)
	clientIPServe(app, "203.0.113.5:5555")
	for _, e := range events.all() {
		if e.Stage != trace.StageComplete && e.ClientIP.IsValid() {
			t.Error(test.DiffMessage(e.ClientIP, netip.Addr{}, "A1: only complete events carry the client IP, not "+e.Stage))
		}
	}
}

func TestCompleteEvent_ClientIP_UnparsableRemoteAddr(t *testing.T) {
	events := &clientIPEvents{}
	app := clientIPApp(t, events, false)
	clientIPServe(app, "not-an-address")
	if got := events.complete(t).ClientIP; got.IsValid() {
		t.Error(test.DiffMessage(got, netip.Addr{}, "A1: the zero Addr when RemoteAddr is not ip:port"))
	}
}

func TestPanicLine_CarriesScopeFields(t *testing.T) {
	installStackSeams(t)
	app, logger := sampleApp(t)
	w := serveWithHeader(app, http.MethodGet, "/bare?s=1", http.Header{"Traceparent": {scopeHeader}})
	entries := logger.named("UnhandledPanic")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "S7: one UnhandledPanic line"))
	}
	e := entries[0]
	want := []string{"trace_id", "request", "upstream", "method", "path", "panic", "stack"}
	if got := keysOf(e.args); !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "S7: the scope's fields come first, then method, path, panic, stack, and no id"))
	}
	if e.field("trace_id") != scopeTraceID || e.field("request") != w.Header().Get("X-Request-Id") || e.field("upstream") != scopeParentID {
		t.Error(test.DiffMessage(e.args[:6], []any{"trace_id", scopeTraceID, "request", w.Header().Get("X-Request-Id"), "upstream", scopeParentID}, "S7: the fields are the request's scope"))
	}

	plainApp, plainLogger := sampleApp(t)
	panicAt(plainApp, 1)
	got := plainLogger.named("UnhandledPanic")[0]
	if keys := keysOf(got.args); !slices.Equal(keys, []string{"trace_id", "request", "method", "path", "panic", "stack"}) {
		t.Error(test.DiffMessage(keys, "trace_id request method path panic stack", "S7: without an incoming parent there is no upstream field"))
	}
}
