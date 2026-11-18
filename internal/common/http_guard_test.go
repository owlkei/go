package common

import (
	"fmt"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestInjectProvidersIntoHTTPGuards_Empty(t *testing.T) {
	g := &Guard{}
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	items := InjectProvidersIntoHTTPGuards(g, r, noopCB, testConstruct)
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

	items := InjectProvidersIntoHTTPGuards(g, r, noopCB, testConstruct)
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
	items := InjectProvidersIntoHTTPGuards(g, r, noopCB, testConstruct)

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
	items := InjectProvidersIntoHTTPGuards(g, r, noopCB, testConstruct)
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
	InjectProvidersIntoHTTPGuards(g, r, noopCB, testConstruct)
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
	InjectProvidersIntoHTTPGuards(g, r, noopCB, testConstruct)
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
	InjectProvidersIntoHTTPGuards(g, r, noopCB, testConstruct)
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

func TestAsHTTPGuard_MatchesReflectionLookup(t *testing.T) {
	c := &ctx.HTTPContext{}
	for _, x := range guardSubjects() {
		wantFn, wantOK := reflectHTTPGuard(x)
		gotFn, gotOK := AsHTTPGuard(x)
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

func TestAsHTTPGuard_Nil(t *testing.T) {
	expectNoPanic(t, "B11: AsHTTPGuard(nil) must not panic", func() {
		fn, ok := AsHTTPGuard(nil)
		if fn != nil || ok {
			t.Error(test.DiffMessage(ok, false, "B11: nil is not a guard"))
		}
	})
}

func TestAs_UnexportedMethodsAreNotLayers(t *testing.T) {
	ex := exception.NotFoundException("x")
	if !(unexportedGuard{}).canActivate(nil) {
		t.Fatal(test.DiffMessage(false, true, "fixture: the unexported guard method exists"))
	}
	if (unexportedInterceptor{}).intercept(nil, nil) != nil {
		t.Fatal(test.DiffMessage(true, false, "fixture: the unexported interceptor method exists"))
	}
	unexportedFilter{}.catch(nil, &ex)

	if _, ok := AsHTTPGuard(unexportedGuard{}); ok {
		t.Error(test.DiffMessage(ok, false, "B11: canActivate is not CanActivate"))
	}
	if _, ok := AsHTTPInterceptor(unexportedInterceptor{}); ok {
		t.Error(test.DiffMessage(ok, false, "B11: intercept is not Intercept"))
	}
	if _, ok := AsHTTPExceptionFilter(unexportedFilter{}); ok {
		t.Error(test.DiffMessage(ok, false, "B11: catch is not Catch"))
	}
}
