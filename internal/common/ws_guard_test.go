package common

import (
	"fmt"
	"testing"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/test"
)

func TestInjectProvidersIntoWSGuards_Empty(t *testing.T) {
	g := &Guard{}
	ws := buildWS(map[string]string{"message": "ON_message"})

	items := InjectProvidersIntoWSGuards(g, ws, noopCB, testConstruct)
	if len(items) != 0 {
		t.Error(test.DiffMessage(len(items), 0, "no bound guards → empty result"))
	}
}

func TestInjectProvidersIntoWSGuards_ApplyAll(t *testing.T) {
	g := &Guard{}
	g.BindGuard(mockWSGuarder{})

	ws := buildWS(map[string]string{
		"message": "ON_message",
		"status":  "ON_status",
	})

	items := InjectProvidersIntoWSGuards(g, ws, noopCB, testConstruct)
	if len(items) != 2 {
		t.Error(test.DiffMessage(len(items), 2, "guard with no handlers applies to all WS patterns"))
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

func TestInjectProvidersIntoWSGuards_HandlerIsCallableCanActivate(t *testing.T) {
	g := &Guard{}
	g.BindGuard(denyWSGuarder{})

	ws := buildWS(map[string]string{"message": "ON_message"})
	items := InjectProvidersIntoWSGuards(g, ws, noopCB, testConstruct)
	if len(items) != 1 {
		t.Fatal(test.DiffMessage(len(items), 1, "one pattern → one item"))
	}

	canActivate, ok := items[0].WS.Common.Handler.(WSCanActivate)
	if !ok {
		t.Fatal(test.DiffMessage(items[0].WS.Common.Handler, "WSCanActivate", "Handler must be callable as WSCanActivate"))
	}
	if canActivate(nil) != false {
		t.Error(test.DiffMessage(true, false, "Handler must call through to the bound guard's CanActivate"))
	}
}

func TestInjectProvidersIntoWSGuards_NoCanActivate_Panics(t *testing.T) {
	g := &Guard{}
	g.BindGuard(noCanActivateGuarder{})
	ws := buildWS(map[string]string{"message": "ON_message"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "guard with no CanActivate method must panic"))
		}
	}()
	InjectProvidersIntoWSGuards(g, ws, noopCB, testConstruct)
}

func TestInjectProvidersIntoWSGuards_WrongReturnType_Panics(t *testing.T) {
	g := &Guard{}
	g.BindGuard(wrongReturnGuarder{})
	ws := buildWS(map[string]string{"message": "ON_message"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "CanActivate returning non-bool must panic"))
		}
	}()
	InjectProvidersIntoWSGuards(g, ws, noopCB, testConstruct)
}

func TestAsWSGuard_Valid(t *testing.T) {
	fn, ok := AsWSGuard(mockWSGuarder{})
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "mockWSGuarder must match WSCanActivate"))
	}
	if fn(nil) != true {
		t.Error(test.DiffMessage(false, true, "returned fn must call through to CanActivate"))
	}
}

func TestAsWSGuard_NoMethod(t *testing.T) {
	_, ok := AsWSGuard(noCanActivateGuarder{})
	if ok {
		t.Error(test.DiffMessage(true, false, "a guarder with no CanActivate must not match"))
	}
}

func TestAsWSGuard_WrongShape(t *testing.T) {
	_, ok := AsWSGuard(wrongParamGuarder{})
	if ok {
		t.Error(test.DiffMessage(true, false, "a CanActivate with a non-context param must not match"))
	}
}

func TestBuildWSGuardMiddleware_AllowedCallsNext(t *testing.T) {
	called := false
	c := &ctx.WSContext{}
	ctx.WSSetNext(c, func() { called = true })

	mw := BuildWSGuardMiddleware(func(*ctx.WSContext) bool { return true })
	mw(c)

	if !called {
		t.Error(test.DiffMessage(called, true, "an allowing guard must call Next"))
	}
}

func TestBuildWSGuardMiddleware_DeniedPanics(t *testing.T) {
	c := &ctx.WSContext{}
	mw := BuildWSGuardMiddleware(func(*ctx.WSContext) bool { return false })

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "a denying guard must panic"))
		}
	}()
	mw(c)
}

func TestBuildWSGuardMiddleware_AllowCallsNextOnce(t *testing.T) {
	calls := 0
	c := &ctx.WSContext{}
	ctx.WSSetNext(c, func() { calls++ })

	mw := BuildWSGuardMiddleware(func(*ctx.WSContext) bool { return true })
	if rec := recoverGuardRejectionWS(mw, c); rec != nil {
		t.Error(test.DiffMessage(rec, nil, "B12: an allowing guard does not panic"))
	}

	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "B12: an allowing guard calls Next once"))
	}
}

func TestBuildWSGuardMiddleware_PassesTheContextToTheGuard(t *testing.T) {
	c := &ctx.WSContext{}
	ctx.WSSetNext(c, func() {})
	var got *ctx.WSContext

	BuildWSGuardMiddleware(func(arg *ctx.WSContext) bool {
		got = arg
		return true
	})(c)

	if got != c {
		t.Error(test.DiffMessage(got, c, "B12: the guard receives the request context"))
	}
}

func TestBuildWSGuardMiddleware_RejectPanicsForbidden(t *testing.T) {
	c := &ctx.WSContext{}
	mw := BuildWSGuardMiddleware(func(*ctx.WSContext) bool { return false })

	rec := recoverGuardRejectionWS(mw, c)
	ex, ok := rec.(exception.Exception)
	if !ok {
		t.Fatal(test.DiffMessage(rec, "an exception.Exception value", "B12: a rejection panics with an exception value"))
	}
	if ex.Code() != 403 {
		t.Error(test.DiffMessage(ex.Code(), 403, "B12: code"))
	}
	if ex.Message() != "Access denied" {
		t.Error(test.DiffMessage(ex.Message(), "Access denied", "B12: message"))
	}
	if ex.Error() != "Forbidden" {
		t.Error(test.DiffMessage(ex.Error(), "Forbidden", "B12: Error()"))
	}
}

func TestBuildWSGuardMiddleware_RejectDoesNotCallNext(t *testing.T) {
	calls := 0
	c := &ctx.WSContext{}
	ctx.WSSetNext(c, func() { calls++ })

	recoverGuardRejectionWS(BuildWSGuardMiddleware(func(*ctx.WSContext) bool { return false }), c)

	if calls != 0 {
		t.Error(test.DiffMessage(calls, 0, "B12: a rejection never calls Next"))
	}
}

func TestBuildWSGuardMiddleware_RejectionsAreIndependentValues(t *testing.T) {
	c := &ctx.WSContext{}
	mw := BuildWSGuardMiddleware(func(*ctx.WSContext) bool { return false })

	first, ok1 := recoverGuardRejectionWS(mw, c).(exception.Exception)
	second, ok2 := recoverGuardRejectionWS(mw, c).(exception.Exception)

	if !ok1 || !ok2 {
		t.Fatal(test.DiffMessage([]bool{ok1, ok2}, []bool{true, true}, "B12: every rejection panics with a value"))
	}
	if first != second {
		t.Error(test.DiffMessage(second, first, "B12: two rejections are equal values"))
	}
	if copyOf := NormalizeRecovered(first); copyOf == NormalizeRecovered(second) {
		t.Error(test.DiffMessage(copyOf, "distinct copies", "B12: each recovered value is its own copy"))
	}
}

func TestAsWSGuard_MatchesReflectionLookup(t *testing.T) {
	c := &ctx.WSContext{}
	for _, x := range guardSubjects() {
		wantFn, wantOK := reflectWSGuard(x)
		gotFn, gotOK := AsWSGuard(x)
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
		if gotFn(c) != wantFn(c) {
			t.Error(test.DiffMessage(gotFn(c), wantFn(c), desc+" result"))
		}
	}
}

func TestAsWSGuard_Nil(t *testing.T) {
	expectNoPanic(t, "B11: AsWSGuard(nil) must not panic", func() {
		fn, ok := AsWSGuard(nil)
		if fn != nil || ok {
			t.Error(test.DiffMessage(ok, false, "B11: nil is not a guard"))
		}
	})
}
