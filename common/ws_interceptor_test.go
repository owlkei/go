package common

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func newInterceptorTestWSContext() *ctx.WSContext {
	return ctx.NewWSContext()
}

func TestInjectProvidersIntoWSInterceptors_Empty(t *testing.T) {
	ic := &Interceptor{}
	ws := buildWS(map[string]string{"message": "ON_message"})

	items := ic.InjectProvidersIntoWSInterceptors(ws, noopCB, testConstruct)
	if len(items) != 0 {
		t.Error(test.DiffMessage(len(items), 0, "no bound interceptors → empty result"))
	}
}

func TestInjectProvidersIntoWSInterceptors_ApplyAll(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(mockWSInterceptable{})

	ws := buildWS(map[string]string{
		"message": "ON_message",
		"status":  "ON_status",
	})

	items := ic.InjectProvidersIntoWSInterceptors(ws, noopCB, testConstruct)
	if len(items) != 2 {
		t.Error(test.DiffMessage(len(items), 2, "interceptor with no handlers applies to all WS patterns"))
	}
	for _, item := range items {
		if item.WS.EventName == "" {
			t.Error(test.DiffMessage(item.WS.EventName, "non-empty", "event name"))
		}
		if item.WS.Common.Name == "" {
			t.Error(test.DiffMessage(item.WS.Common.Name, "non-empty", "name"))
		}
	}
}

func TestInjectProvidersIntoWSInterceptors_HandlerIsCallableIntercept(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(mockWSInterceptable{})

	ws := buildWS(map[string]string{"message": "ON_message"})
	items := ic.InjectProvidersIntoWSInterceptors(ws, noopCB, testConstruct)
	if len(items) != 1 {
		t.Fatal(test.DiffMessage(len(items), 1, "one pattern → one item"))
	}

	if _, ok := items[0].WS.Common.Handler.(WSIntercept); !ok {
		t.Fatal(test.DiffMessage(items[0].WS.Common.Handler, "WSIntercept", "Handler must be callable as WSIntercept"))
	}
}

func TestInjectProvidersIntoWSInterceptors_NoIntercept_Panics(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(noInterceptMethod{})
	ws := buildWS(map[string]string{"message": "ON_message"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "interceptor with no Intercept method must panic"))
		}
	}()
	ic.InjectProvidersIntoWSInterceptors(ws, noopCB, testConstruct)
}

func TestAsWSInterceptor_Valid(t *testing.T) {
	fn, ok := AsWSInterceptor(mockWSInterceptable{})
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "mockWSInterceptable must match WSIntercept"))
	}
	fn(nil, aggregation.NewAggregation())
}

func TestAsWSInterceptor_NoMethod(t *testing.T) {
	_, ok := AsWSInterceptor(noInterceptMethod{})
	if ok {
		t.Error(test.DiffMessage(true, false, "an interceptable with no Intercept must not match"))
	}
}

func TestAsWSInterceptor_WrongShape(t *testing.T) {
	_, ok := AsWSInterceptor(wrongParamInterceptable{})
	if ok {
		t.Error(test.DiffMessage(true, false, "an Intercept with a non-context first param must not match"))
	}
}

func TestBuildWSInterceptMiddleware_CallsNextAndRunsIntercept(t *testing.T) {
	c := newInterceptorTestWSContext()
	called := false
	c.Next = func() { called = true }

	var gotAgg *aggregation.Aggregation
	mw := BuildWSInterceptMiddleware("test.event", func(_ *ctx.WSContext, agg *aggregation.Aggregation) any {
		gotAgg = agg
		return "intercepted"
	})
	mw(c)

	if !called {
		t.Error(test.DiffMessage(called, true, "BuildWSInterceptMiddleware must call Next"))
	}
	if gotAgg == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil aggregation", "the intercept fn must receive an aggregation"))
	}
	if gotAgg.InterceptorData != "intercepted" {
		t.Error(test.DiffMessage(gotAgg.InterceptorData, "intercepted", "the intercept fn's return value must be stored as InterceptorData"))
	}
}

func noopInterceptWS(*ctx.WSContext, *aggregation.Aggregation) any { return nil }

func TestBuildWSInterceptMiddleware_AddsNamedAggregation(t *testing.T) {
	c := newInterceptorTestWSContext()
	c.Next = func() {}

	BuildWSInterceptMiddleware("audit", noopInterceptWS)(c)

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

func TestBuildWSInterceptMiddleware_StoresInterceptorData(t *testing.T) {
	c := newInterceptorTestWSContext()
	c.Next = func() {}

	BuildWSInterceptMiddleware("audit", func(*ctx.WSContext, *aggregation.Aggregation) any {
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

func TestBuildWSInterceptMiddleware_RunsInOrderAndPassesTheListedAggregation(t *testing.T) {
	c := newInterceptorTestWSContext()
	var events []string
	var given *aggregation.Aggregation
	var listedInsideFn []*aggregation.Aggregation
	c.Next = func() { events = append(events, "next") }

	BuildWSInterceptMiddleware("audit", func(arg *ctx.WSContext, a *aggregation.Aggregation) any {
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

func TestBuildWSInterceptMiddleware_CallsNextOnce(t *testing.T) {
	c := newInterceptorTestWSContext()
	calls := 0
	c.Next = func() { calls++ }

	BuildWSInterceptMiddleware("audit", noopInterceptWS)(c)

	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "B13: Next is called once"))
	}
}

func TestBuildWSInterceptMiddleware_CallsNextWithoutPipe(t *testing.T) {
	c := newInterceptorTestWSContext()
	calls := 0
	c.Next = func() { calls++ }

	BuildWSInterceptMiddleware("audit", func(*ctx.WSContext, *aggregation.Aggregation) any {
		return "never piped"
	})(c)

	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "B13: Next is called even when fn never calls Pipe"))
	}
}

func TestBuildWSInterceptMiddleware_TwoInterceptorsKeepOrder(t *testing.T) {
	c := newInterceptorTestWSContext()
	first := BuildWSInterceptMiddleware("first", noopInterceptWS)
	second := BuildWSInterceptMiddleware("second", noopInterceptWS)
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

func TestBuildWSInterceptMiddleware_SameNameTwiceStacksTwoAggregations(t *testing.T) {
	c := newInterceptorTestWSContext()
	c.Next = func() {}
	mw := BuildWSInterceptMiddleware("audit", noopInterceptWS)

	mw(c)
	mw(c)

	if len(c.Aggregations()) != 2 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 2, "B14: a second run appends instead of overwriting"))
	}
}

func TestBuildWSInterceptMiddleware_PanicPropagates(t *testing.T) {
	c := newInterceptorTestWSContext()
	calls := 0
	c.Next = func() { calls++ }
	boom := errors.New("boom")

	text, panicked := recoverValue(func() {
		BuildWSInterceptMiddleware("audit", func(*ctx.WSContext, *aggregation.Aggregation) any {
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

func TestBuildWSInterceptMiddleware_ResetStartsEmpty(t *testing.T) {
	c := newInterceptorTestWSContext()
	c.Next = func() {}
	mw := BuildWSInterceptMiddleware("audit", noopInterceptWS)
	mw(c)

	c.Reset()
	if len(c.Aggregations()) != 0 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 0, "B16: a reset context has no aggregation"))
	}

	c.Next = func() {}
	mw(c)
	if len(c.Aggregations()) != 1 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 1, "B16: a reused context starts a new list"))
	}
}
