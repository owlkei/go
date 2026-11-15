package common

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func newInterceptorTestContext() *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	c.Init(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	return c
}

func TestInjectProvidersIntoHTTPInterceptors_Empty(t *testing.T) {
	ic := &Interceptor{}
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	items := ic.InjectProvidersIntoHTTPInterceptors(r, noopCB)
	if len(items) != 0 {
		t.Error(test.DiffMessage(len(items), 0, "no bound interceptors → empty result"))
	}
}

func TestInjectProvidersIntoHTTPInterceptors_ApplyAll(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(mockInterceptable{})

	r := buildHTTP(map[string]string{
		"READ_users":    "/users/",
		"CREATE_orders": "/orders/",
	})

	items := ic.InjectProvidersIntoHTTPInterceptors(r, noopCB)
	if len(items) != 2 {
		t.Error(test.DiffMessage(len(items), 2, "interceptor with no handlers applies to all patterns"))
	}
	for _, item := range items {
		if item.HTTP.Method != "GET" {
			t.Error(test.DiffMessage(item.HTTP.Method, "GET", "method"))
		}
		if item.HTTP.Pattern == "" {
			t.Error(test.DiffMessage(item.HTTP.Pattern, "non-empty", "pattern"))
		}
		if item.HTTP.Common.Name == "" {
			t.Error(test.DiffMessage(item.HTTP.Common.Name, "non-empty", "name"))
		}
	}
}

func TestInjectProvidersIntoHTTPInterceptors_MainHandlerName(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(mockInterceptable{})

	r := buildHTTP(map[string]string{"READ_items": "/items/"})
	items := ic.InjectProvidersIntoHTTPInterceptors(r, noopCB)

	if len(items) != 1 {
		t.Error(test.DiffMessage(len(items), 1, "one pattern → one item"))
		return
	}
	if items[0].HTTP.Common.MainHandlerName != "READ_items" {
		t.Error(test.DiffMessage(items[0].HTTP.Common.MainHandlerName, "READ_items", "main handler name"))
	}
}

func TestInjectProvidersIntoHTTPInterceptors_HandlerIsCallableIntercept(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(mockInterceptable{})

	r := buildHTTP(map[string]string{"READ_users": "/users/"})
	items := ic.InjectProvidersIntoHTTPInterceptors(r, noopCB)
	if len(items) != 1 {
		t.Fatal(test.DiffMessage(len(items), 1, "one pattern → one item"))
	}

	if _, ok := items[0].HTTP.Common.Handler.(HTTPIntercept); !ok {
		t.Fatal(test.DiffMessage(items[0].HTTP.Common.Handler, "HTTPIntercept", "Handler must be callable as HTTPIntercept"))
	}
}

func TestInjectProvidersIntoHTTPInterceptors_NoIntercept_Panics(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(noInterceptMethod{})
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "interceptor with no Intercept method must panic"))
		}
	}()
	ic.InjectProvidersIntoHTTPInterceptors(r, noopCB)
}

func TestInjectProvidersIntoHTTPInterceptors_WrongParamType_Panics(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(wrongParamInterceptable{})
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "Intercept with a non-context first param must panic"))
		}
	}()
	ic.InjectProvidersIntoHTTPInterceptors(r, noopCB)
}

func TestAsHTTPInterceptor_Valid(t *testing.T) {
	fn, ok := AsHTTPInterceptor(mockInterceptable{})
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "mockInterceptable must match HTTPIntercept"))
	}
	fn(nil, aggregation.NewAggregation())
}

func TestAsHTTPInterceptor_NoMethod(t *testing.T) {
	_, ok := AsHTTPInterceptor(noInterceptMethod{})
	if ok {
		t.Error(test.DiffMessage(true, false, "an interceptable with no Intercept must not match"))
	}
}

func TestAsHTTPInterceptor_WrongShape(t *testing.T) {
	_, ok := AsHTTPInterceptor(wrongParamInterceptable{})
	if ok {
		t.Error(test.DiffMessage(true, false, "an Intercept with a non-context first param must not match"))
	}
}

func TestBuildHTTPInterceptMiddleware_CallsNextAndRunsIntercept(t *testing.T) {
	c := newInterceptorTestContext()
	called := false
	c.Next = func() { called = true }

	var gotAgg *aggregation.Aggregation
	mw := BuildHTTPInterceptMiddleware("test.key", func(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
		gotAgg = agg
		return "intercepted"
	})
	mw(c)

	if !called {
		t.Error(test.DiffMessage(called, true, "BuildHTTPInterceptMiddleware must call Next"))
	}
	if gotAgg == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil aggregation", "the intercept fn must receive an aggregation"))
	}
	if gotAgg.InterceptorData != "intercepted" {
		t.Error(test.DiffMessage(gotAgg.InterceptorData, "intercepted", "the intercept fn's return value must be stored as InterceptorData"))
	}
	if gotAgg.IsMainHandlerCalled {
		t.Error(test.DiffMessage(gotAgg.IsMainHandlerCalled, false, "IsMainHandlerCalled must default to false before the chain runs Pipe"))
	}
}

func noopInterceptHTTP(*ctx.HTTPContext, *aggregation.Aggregation) any { return nil }

func TestBuildHTTPInterceptMiddleware_AddsNamedAggregation(t *testing.T) {
	c := newInterceptorTestContext()
	c.Next = func() {}

	BuildHTTPInterceptMiddleware("audit", noopInterceptHTTP)(c)

	aggregations := c.Aggregations()
	if len(aggregations) != 1 {
		t.Fatal(test.DiffMessage(len(aggregations), 1, "B13: one aggregation is added to the context"))
	}
	if aggregations[0].Name != "audit" {
		t.Error(test.DiffMessage(aggregations[0].Name, "audit", "B13: the aggregation carries the interceptor name"))
	}
	if aggregations[0].IsMainHandlerCalled {
		t.Error(test.DiffMessage(aggregations[0].IsMainHandlerCalled, false, "B13: IsMainHandlerCalled starts false"))
	}
}

func TestBuildHTTPInterceptMiddleware_StoresInterceptorData(t *testing.T) {
	c := newInterceptorTestContext()
	c.Next = func() {}

	BuildHTTPInterceptMiddleware("audit", func(*ctx.HTTPContext, *aggregation.Aggregation) any {
		return "payload"
	})(c)

	aggregations := c.Aggregations()
	if len(aggregations) != 1 {
		t.Fatal(test.DiffMessage(len(aggregations), 1, "B13: one aggregation is added"))
	}
	if aggregations[0].InterceptorData != "payload" {
		t.Error(test.DiffMessage(aggregations[0].InterceptorData, "payload", "B13: the result of fn is InterceptorData"))
	}
}

func TestBuildHTTPInterceptMiddleware_RunsInOrderAndPassesTheListedAggregation(t *testing.T) {
	c := newInterceptorTestContext()
	var events []string
	var given *aggregation.Aggregation
	var listedInsideFn []*aggregation.Aggregation
	c.Next = func() { events = append(events, "next") }

	BuildHTTPInterceptMiddleware("audit", func(arg *ctx.HTTPContext, a *aggregation.Aggregation) any {
		events = append(events, "intercept")
		given = a
		listedInsideFn = arg.Aggregations()
		return "payload"
	})(c)

	if !reflect.DeepEqual(events, []string{"intercept", "next"}) {
		t.Error(test.DiffMessage(events, []string{"intercept", "next"}, "B13: fn runs once, then Next once"))
	}
	if len(listedInsideFn) != 1 || listedInsideFn[0] != given {
		t.Error(test.DiffMessage(listedInsideFn, "the aggregation given to fn", "B13: the aggregation is on the context before fn runs"))
	}
}

func TestBuildHTTPInterceptMiddleware_CallsNextOnce(t *testing.T) {
	c := newInterceptorTestContext()
	calls := 0
	c.Next = func() { calls++ }

	BuildHTTPInterceptMiddleware("audit", noopInterceptHTTP)(c)

	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "B13: Next is called once"))
	}
}

func TestBuildHTTPInterceptMiddleware_CallsNextWithoutPipe(t *testing.T) {
	c := newInterceptorTestContext()
	calls := 0
	c.Next = func() { calls++ }

	BuildHTTPInterceptMiddleware("audit", func(*ctx.HTTPContext, *aggregation.Aggregation) any {
		return "never piped"
	})(c)

	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "B13: Next is called even when fn never calls Pipe"))
	}
}

func TestBuildHTTPInterceptMiddleware_TwoInterceptorsKeepOrder(t *testing.T) {
	c := newInterceptorTestContext()
	first := BuildHTTPInterceptMiddleware("first", noopInterceptHTTP)
	second := BuildHTTPInterceptMiddleware("second", noopInterceptHTTP)
	c.Next = func() {
		c.Next = func() {}
		second(c)
	}

	first(c)

	aggregations := c.Aggregations()
	if len(aggregations) != 2 {
		t.Fatal(test.DiffMessage(len(aggregations), 2, "B14: both aggregations are on the context"))
	}
	if aggregations[0].Name != "first" || aggregations[1].Name != "second" {
		t.Error(test.DiffMessage([]string{aggregations[0].Name, aggregations[1].Name}, []string{"first", "second"}, "B14: execution order"))
	}
	if aggregations[0] == aggregations[1] {
		t.Error(test.DiffMessage(aggregations[1], "a distinct aggregation", "B13: every interceptor gets a new aggregation"))
	}
}

func TestBuildHTTPInterceptMiddleware_SameNameTwiceStacksTwoAggregations(t *testing.T) {
	c := newInterceptorTestContext()
	c.Next = func() {}
	mw := BuildHTTPInterceptMiddleware("audit", noopInterceptHTTP)

	mw(c)
	mw(c)

	if len(c.Aggregations()) != 2 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 2, "B14: a second run appends instead of overwriting"))
	}
}

func TestBuildHTTPInterceptMiddleware_PanicPropagates(t *testing.T) {
	c := newInterceptorTestContext()
	calls := 0
	c.Next = func() { calls++ }
	boom := errors.New("boom")

	text, panicked := recoverValue(func() {
		BuildHTTPInterceptMiddleware("audit", func(*ctx.HTTPContext, *aggregation.Aggregation) any {
			panic(boom)
		})(c)
	})

	if !panicked || text != boom {
		t.Error(test.DiffMessage(text, boom, "B14: a panic in fn propagates"))
	}
	if calls != 0 {
		t.Error(test.DiffMessage(calls, 0, "B14: Next is not called after a panic"))
	}
	if len(c.Aggregations()) != 1 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 1, "B14: the aggregation is already in the list"))
	}
}

func TestBuildHTTPInterceptMiddleware_ResetStartsEmpty(t *testing.T) {
	c := newInterceptorTestContext()
	c.Next = func() {}
	mw := BuildHTTPInterceptMiddleware("audit", noopInterceptHTTP)
	mw(c)

	c.Reset()
	if len(c.Aggregations()) != 0 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 0, "B16: a reset context has no aggregation"))
	}

	c.Init(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	c.Next = func() {}
	mw(c)
	if len(c.Aggregations()) != 1 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 1, "B16: a reused context starts a new list"))
	}
}

func TestBuildHTTPInterceptMiddleware_KeepsRequestPointer(t *testing.T) {
	c := newInterceptorTestContext()
	c.Next = func() {}
	before := c.Request

	BuildHTTPInterceptMiddleware("audit", noopInterceptHTTP)(c)

	if c.Request != before {
		t.Error(test.DiffMessage(c.Request, before, "B15: the middleware does not replace the request"))
	}
}
