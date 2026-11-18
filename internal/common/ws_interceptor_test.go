package common

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestInjectProvidersIntoWSInterceptors_Empty(t *testing.T) {
	ic := &Interceptor{}
	ws := buildWS(map[string]string{"message": "ON_message"})

	items := InjectProvidersIntoWSInterceptors(ic, ws, noopCB, testConstruct)
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

	items := InjectProvidersIntoWSInterceptors(ic, ws, noopCB, testConstruct)
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
	items := InjectProvidersIntoWSInterceptors(ic, ws, noopCB, testConstruct)
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
	InjectProvidersIntoWSInterceptors(ic, ws, noopCB, testConstruct)
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
	ctx.WSSetNext(c, func() { called = true })

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
	if aggregation.InterceptorData(gotAgg) != "intercepted" {
		t.Error(test.DiffMessage(aggregation.InterceptorData(gotAgg), "intercepted", "the intercept fn's return value must be stored as InterceptorData"))
	}
}

func TestBuildWSInterceptMiddleware_AddsNamedAggregation(t *testing.T) {
	c := newInterceptorTestWSContext()
	ctx.WSSetNext(c, func() {})

	BuildWSInterceptMiddleware("audit", noopInterceptWS)(c)

	aggregations := ctx.WSAggregations(c)
	if len(aggregations) != 1 {
		t.Fatal(test.DiffMessage(len(aggregations), 1, "B13: one aggregation is added to the context"))
	}
	if aggregation.Name(aggregations[0]) != "audit" {
		t.Error(test.DiffMessage(aggregation.Name(aggregations[0]), "audit", "B13: the aggregation carries the interceptor name"))
	}
	if aggregation.IsMainHandlerCalled(aggregations[0]) {
		t.Error(test.DiffMessage(aggregation.IsMainHandlerCalled(aggregations[0]), false, "B13: IsMainHandlerCalled starts false"))
	}
}

func TestBuildWSInterceptMiddleware_StoresInterceptorData(t *testing.T) {
	c := newInterceptorTestWSContext()
	ctx.WSSetNext(c, func() {})

	BuildWSInterceptMiddleware("audit", func(*ctx.WSContext, *aggregation.Aggregation) any {
		return "payload"
	})(c)

	aggregations := ctx.WSAggregations(c)
	if len(aggregations) != 1 {
		t.Fatal(test.DiffMessage(len(aggregations), 1, "B13: one aggregation is added"))
	}
	if aggregation.InterceptorData(aggregations[0]) != "payload" {
		t.Error(test.DiffMessage(aggregation.InterceptorData(aggregations[0]), "payload", "B13: the result of fn is InterceptorData"))
	}
}

func TestBuildWSInterceptMiddleware_RunsInOrderAndPassesTheListedAggregation(t *testing.T) {
	c := newInterceptorTestWSContext()
	var events []string
	var given *aggregation.Aggregation
	var listedInsideFn []*aggregation.Aggregation
	ctx.WSSetNext(c, func() { events = append(events, "next") })

	BuildWSInterceptMiddleware("audit", func(arg *ctx.WSContext, a *aggregation.Aggregation) any {
		events = append(events, "intercept")
		given = a
		listedInsideFn = ctx.WSAggregations(arg)
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
	ctx.WSSetNext(c, func() { calls++ })

	BuildWSInterceptMiddleware("audit", noopInterceptWS)(c)

	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "B13: Next is called once"))
	}
}

func TestBuildWSInterceptMiddleware_CallsNextWithoutPipe(t *testing.T) {
	c := newInterceptorTestWSContext()
	calls := 0
	ctx.WSSetNext(c, func() { calls++ })

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
	ctx.WSSetNext(c, func() {
		ctx.WSSetNext(c, func() {})
		second(c)
	})

	first(c)

	aggregations := ctx.WSAggregations(c)
	if len(aggregations) != 2 {
		t.Fatal(test.DiffMessage(len(aggregations), 2, "B14: both aggregations are on the context"))
	}
	if aggregation.Name(aggregations[0]) != "first" || aggregation.Name(aggregations[1]) != "second" {
		t.Error(test.DiffMessage([]string{aggregation.Name(aggregations[0]), aggregation.Name(aggregations[1])}, []string{"first", "second"}, "B14: execution order"))
	}
	if aggregations[0] == aggregations[1] {
		t.Error(test.DiffMessage(aggregations[1], "a distinct aggregation", "B13: every interceptor gets a new aggregation"))
	}
}

func TestBuildWSInterceptMiddleware_SameNameTwiceStacksTwoAggregations(t *testing.T) {
	c := newInterceptorTestWSContext()
	ctx.WSSetNext(c, func() {})
	mw := BuildWSInterceptMiddleware("audit", noopInterceptWS)

	mw(c)
	mw(c)

	if len(ctx.WSAggregations(c)) != 2 {
		t.Error(test.DiffMessage(len(ctx.WSAggregations(c)), 2, "B14: a second run appends instead of overwriting"))
	}
}

func TestBuildWSInterceptMiddleware_PanicPropagates(t *testing.T) {
	c := newInterceptorTestWSContext()
	calls := 0
	ctx.WSSetNext(c, func() { calls++ })
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
	if len(ctx.WSAggregations(c)) != 1 {
		t.Error(test.DiffMessage(len(ctx.WSAggregations(c)), 1, "B14: the aggregation is already in the list"))
	}
}

func TestBuildWSInterceptMiddleware_ResetStartsEmpty(t *testing.T) {
	c := newInterceptorTestWSContext()
	ctx.WSSetNext(c, func() {})
	mw := BuildWSInterceptMiddleware("audit", noopInterceptWS)
	mw(c)

	ctx.WSReset(c)
	if len(ctx.WSAggregations(c)) != 0 {
		t.Error(test.DiffMessage(len(ctx.WSAggregations(c)), 0, "B16: a reset context has no aggregation"))
	}

	ctx.WSSetNext(c, func() {})
	mw(c)
	if len(ctx.WSAggregations(c)) != 1 {
		t.Error(test.DiffMessage(len(ctx.WSAggregations(c)), 1, "B16: a reused context starts a new list"))
	}
}

func TestAsWSInterceptor_MatchesReflectionLookup(t *testing.T) {
	c := &ctx.WSContext{}
	for _, x := range interceptorSubjects() {
		wantFn, wantOK := reflectWSInterceptor(x)
		gotFn, gotOK := AsWSInterceptor(x)
		desc := fmt.Sprintf("B11: %T", x)
		if gotOK != wantOK {
			t.Error(test.DiffMessage(gotOK, wantOK, desc+" ok"))
			continue
		}
		if !gotOK {
			continue
		}
		if gotFn == nil {
			t.Error(test.DiffMessage(nil, "a func", desc+" fn"))
			continue
		}
		got, want := gotFn(c, aggregation.NewAggregation()), wantFn(c, aggregation.NewAggregation())
		if got != want {
			t.Error(test.DiffMessage(got, want, desc+" result"))
		}
	}
}

func TestAsWSInterceptor_Nil(t *testing.T) {
	expectNoPanic(t, "B11: AsWSInterceptor(nil) must not panic", func() {
		fn, ok := AsWSInterceptor(nil)
		if fn != nil || ok {
			t.Error(test.DiffMessage(ok, false, "B11: nil is not an interceptor"))
		}
	})
}
