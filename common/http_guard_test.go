package common

import (
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestInjectProvidersIntoHTTPGuards_Empty(t *testing.T) {
	g := &Guard{}
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	items := g.InjectProvidersIntoHTTPGuards(r, noopCB)
	if len(items) != 0 {
		t.Error(test.DiffMessage(len(items), 0, "no bound guards → empty result"))
	}
}

func TestInjectProvidersIntoHTTPGuards_ApplyAll(t *testing.T) {
	g := &Guard{}
	g.BindGuard(mockGuarder{})

	r := buildHTTP(map[string]string{
		"READ_users":    "/users/",
		"CREATE_orders": "/orders/",
	})

	items := g.InjectProvidersIntoHTTPGuards(r, noopCB)
	if len(items) != 2 {
		t.Error(test.DiffMessage(len(items), 2, "guard with no handlers applies to all patterns"))
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

func TestInjectProvidersIntoHTTPGuards_MainHandlerName(t *testing.T) {
	g := &Guard{}
	g.BindGuard(mockGuarder{})

	r := buildHTTP(map[string]string{"READ_items": "/items/"})
	items := g.InjectProvidersIntoHTTPGuards(r, noopCB)

	if len(items) != 1 {
		t.Error(test.DiffMessage(len(items), 1, "one pattern → one item"))
		return
	}
	if items[0].HTTP.Common.MainHandlerName != "READ_items" {
		t.Error(test.DiffMessage(items[0].HTTP.Common.MainHandlerName, "READ_items", "main handler name"))
	}
}

func TestInjectProvidersIntoHTTPGuards_HandlerIsCallableCanActivate(t *testing.T) {
	g := &Guard{}
	g.BindGuard(denyGuarder{})

	r := buildHTTP(map[string]string{"READ_users": "/users/"})
	items := g.InjectProvidersIntoHTTPGuards(r, noopCB)
	if len(items) != 1 {
		t.Fatal(test.DiffMessage(len(items), 1, "one pattern → one item"))
	}

	canActivate, ok := items[0].HTTP.Common.Handler.(HTTPCanActivate)
	if !ok {
		t.Fatal(test.DiffMessage(items[0].HTTP.Common.Handler, "HTTPCanActivate", "Handler must be callable as HTTPCanActivate"))
	}
	if canActivate(nil) != false {
		t.Error(test.DiffMessage(true, false, "Handler must call through to the bound guard's CanActivate"))
	}
}

func TestInjectProvidersIntoHTTPGuards_NoCanActivate_Panics(t *testing.T) {
	g := &Guard{}
	g.BindGuard(noCanActivateGuarder{})
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "guard with no CanActivate method must panic"))
		}
	}()
	g.InjectProvidersIntoHTTPGuards(r, noopCB)
}

func TestInjectProvidersIntoHTTPGuards_WrongReturnType_Panics(t *testing.T) {
	g := &Guard{}
	g.BindGuard(wrongReturnGuarder{})
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "CanActivate returning non-bool must panic"))
		}
	}()
	g.InjectProvidersIntoHTTPGuards(r, noopCB)
}

func TestInjectProvidersIntoHTTPGuards_WrongParamType_Panics(t *testing.T) {
	g := &Guard{}
	g.BindGuard(wrongParamGuarder{})
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "CanActivate with a non-context param must panic"))
		}
	}()
	g.InjectProvidersIntoHTTPGuards(r, noopCB)
}

func TestAsHTTPGuard_Valid(t *testing.T) {
	fn, ok := AsHTTPGuard(mockGuarder{})
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "mockGuarder must match HTTPCanActivate"))
	}
	if fn(nil) != true {
		t.Error(test.DiffMessage(false, true, "returned fn must call through to CanActivate"))
	}
}

func TestAsHTTPGuard_NoMethod(t *testing.T) {
	_, ok := AsHTTPGuard(noCanActivateGuarder{})
	if ok {
		t.Error(test.DiffMessage(true, false, "a guarder with no CanActivate must not match"))
	}
}

func TestAsHTTPGuard_WrongShape(t *testing.T) {
	_, ok := AsHTTPGuard(wrongReturnGuarder{})
	if ok {
		t.Error(test.DiffMessage(true, false, "a CanActivate returning non-bool must not match"))
	}
}

func TestBuildHTTPGuardMiddleware_AllowedCallsNext(t *testing.T) {
	called := false
	c := &ctx.HTTPContext{}
	c.Next = func() { called = true }

	mw := BuildHTTPGuardMiddleware(func(*ctx.HTTPContext) bool { return true })
	mw(c)

	if !called {
		t.Error(test.DiffMessage(called, true, "an allowing guard must call Next"))
	}
}

func TestBuildHTTPGuardMiddleware_DeniedPanics(t *testing.T) {
	c := &ctx.HTTPContext{}
	mw := BuildHTTPGuardMiddleware(func(*ctx.HTTPContext) bool { return false })

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "a denying guard must panic"))
		}
	}()
	mw(c)
}

func recoverGuardRejectionHTTP(mw func(*ctx.HTTPContext), c *ctx.HTTPContext) (rec any) {
	defer func() { rec = recover() }()
	mw(c)
	return nil
}

func TestBuildHTTPGuardMiddleware_AllowCallsNextOnce(t *testing.T) {
	calls := 0
	c := &ctx.HTTPContext{}
	c.Next = func() { calls++ }

	mw := BuildHTTPGuardMiddleware(func(*ctx.HTTPContext) bool { return true })
	if rec := recoverGuardRejectionHTTP(mw, c); rec != nil {
		t.Error(test.DiffMessage(rec, nil, "B12: an allowing guard does not panic"))
	}

	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "B12: an allowing guard calls Next once"))
	}
}

func TestBuildHTTPGuardMiddleware_PassesTheContextToTheGuard(t *testing.T) {
	c := &ctx.HTTPContext{}
	c.Next = func() {}
	var got *ctx.HTTPContext

	BuildHTTPGuardMiddleware(func(arg *ctx.HTTPContext) bool {
		got = arg
		return true
	})(c)

	if got != c {
		t.Error(test.DiffMessage(got, c, "B12: the guard receives the request context"))
	}
}

func TestBuildHTTPGuardMiddleware_RejectPanicsForbidden(t *testing.T) {
	c := &ctx.HTTPContext{}
	mw := BuildHTTPGuardMiddleware(func(*ctx.HTTPContext) bool { return false })

	rec := recoverGuardRejectionHTTP(mw, c)
	ex, ok := rec.(exception.Exception)
	if !ok {
		t.Fatal(test.DiffMessage(rec, "an exception.Exception value", "B12: a rejection panics with an exception value"))
	}
	if ex.GetCode() != 403 {
		t.Error(test.DiffMessage(ex.GetCode(), 403, "B12: code"))
	}
	if ex.GetMessage() != "Access denied" {
		t.Error(test.DiffMessage(ex.GetMessage(), "Access denied", "B12: message"))
	}
	if ex.Error() != "Forbidden" {
		t.Error(test.DiffMessage(ex.Error(), "Forbidden", "B12: Error()"))
	}
}

func TestBuildHTTPGuardMiddleware_RejectDoesNotCallNext(t *testing.T) {
	calls := 0
	c := &ctx.HTTPContext{}
	c.Next = func() { calls++ }

	recoverGuardRejectionHTTP(BuildHTTPGuardMiddleware(func(*ctx.HTTPContext) bool { return false }), c)

	if calls != 0 {
		t.Error(test.DiffMessage(calls, 0, "B12: a rejection never calls Next"))
	}
}

func TestBuildHTTPGuardMiddleware_RejectionsAreIndependentValues(t *testing.T) {
	c := &ctx.HTTPContext{}
	mw := BuildHTTPGuardMiddleware(func(*ctx.HTTPContext) bool { return false })

	first, ok1 := recoverGuardRejectionHTTP(mw, c).(exception.Exception)
	second, ok2 := recoverGuardRejectionHTTP(mw, c).(exception.Exception)

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
