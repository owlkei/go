package common

import (
	"fmt"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestInjectProvidersIntoHTTPExceptionFilters_Empty(t *testing.T) {
	e := &ExceptionFilter{}
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	items := InjectProvidersIntoHTTPExceptionFilters(e, r, noopCB, testConstruct)
	if len(items) != 0 {
		t.Error(test.DiffMessage(len(items), 0, "no bound filters → empty result"))
	}
}

func TestInjectProvidersIntoHTTPExceptionFilters_ApplyAll(t *testing.T) {
	e := &ExceptionFilter{}
	e.BindExceptionFilter(mockExFilter{})

	r := buildHTTP(map[string]string{
		"READ_users":    "/users/",
		"CREATE_orders": "/orders/",
	})

	items := InjectProvidersIntoHTTPExceptionFilters(e, r, noopCB, testConstruct)
	if len(items) != 2 {
		t.Error(test.DiffMessage(len(items), 2, "filter with no handlers applies to all patterns"))
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

func TestInjectProvidersIntoHTTPExceptionFilters_MainHandlerName(t *testing.T) {
	e := &ExceptionFilter{}
	e.BindExceptionFilter(mockExFilter{})

	r := buildHTTP(map[string]string{"READ_items": "/items/"})
	items := InjectProvidersIntoHTTPExceptionFilters(e, r, noopCB, testConstruct)

	if len(items) != 1 {
		t.Error(test.DiffMessage(len(items), 1, "one pattern → one item"))
		return
	}
	if items[0].HTTP.Common.MainHandlerName != "READ_items" {
		t.Error(test.DiffMessage(items[0].HTTP.Common.MainHandlerName, "READ_items", "main handler name"))
	}
}

func TestInjectProvidersIntoHTTPExceptionFilters_HandlerIsCallableCatch(t *testing.T) {
	e := &ExceptionFilter{}
	e.BindExceptionFilter(mockExFilter{})

	r := buildHTTP(map[string]string{"READ_users": "/users/"})
	items := InjectProvidersIntoHTTPExceptionFilters(e, r, noopCB, testConstruct)
	if len(items) != 1 {
		t.Fatal(test.DiffMessage(len(items), 1, "one pattern → one item"))
	}

	if _, ok := items[0].HTTP.Common.Handler.(HTTPCatch); !ok {
		t.Fatal(test.DiffMessage(items[0].HTTP.Common.Handler, "HTTPCatch", "Handler must be callable as HTTPCatch"))
	}
}

func TestInjectProvidersIntoHTTPExceptionFilters_NoCatch_Panics(t *testing.T) {
	e := &ExceptionFilter{}
	e.BindExceptionFilter(noCatchExFilter{})
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "filter with no Catch method must panic"))
		}
	}()
	InjectProvidersIntoHTTPExceptionFilters(e, r, noopCB, testConstruct)
}

func TestInjectProvidersIntoHTTPExceptionFilters_WrongParamType_Panics(t *testing.T) {
	e := &ExceptionFilter{}
	e.BindExceptionFilter(wrongParamExFilter{})
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "Catch with a non-context first param must panic"))
		}
	}()
	InjectProvidersIntoHTTPExceptionFilters(e, r, noopCB, testConstruct)
}

func TestAsHTTPExceptionFilter_Valid(t *testing.T) {
	fn, ok := AsHTTPExceptionFilter(mockExFilter{})
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "mockExFilter must match HTTPCatch"))
	}
	ex := exception.InternalServerErrorException("")
	fn(nil, &ex)
}

func TestAsHTTPExceptionFilter_NoMethod(t *testing.T) {
	_, ok := AsHTTPExceptionFilter(noCatchExFilter{})
	if ok {
		t.Error(test.DiffMessage(true, false, "a filter with no Catch must not match"))
	}
}

func TestAsHTTPExceptionFilter_WrongShape(t *testing.T) {
	_, ok := AsHTTPExceptionFilter(wrongParamExFilter{})
	if ok {
		t.Error(test.DiffMessage(true, false, "a Catch with a non-context first param must not match"))
	}
}

func TestAsHTTPExceptionFilter_MatchesReflectionLookup(t *testing.T) {
	c := &ctx.HTTPContext{}
	ex := exception.NotFoundException("x")
	for _, x := range filterSubjects() {
		_, wantOK := reflectHTTPFilter(x)
		gotFn, gotOK := AsHTTPExceptionFilter(x)
		desc := fmt.Sprintf("B11: %T", x)
		if gotOK != wantOK {
			t.Error(test.DiffMessage(gotOK, wantOK, desc+" ok"))
			continue
		}
		if gotOK && gotFn == nil {
			t.Error(test.DiffMessage(nil, "a func", desc+" fn"))
			continue
		}
		if gotOK {
			gotFn(c, &ex)
		}
	}
}

func TestAsHTTPExceptionFilter_CallsMethodOnTheInstance(t *testing.T) {
	f := &recordingFilter{}
	fn, ok := AsHTTPExceptionFilter(f)
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "B11: pointer receiver filter matches"))
	}
	ex := exception.NotFoundException("x")
	fn(&ctx.HTTPContext{}, &ex)

	if len(f.got) != 1 || f.got[0] != &ex {
		t.Error(test.DiffMessage(f.got, []*exception.Exception{&ex}, "B11: fn calls Catch on x with the same arguments"))
	}
}

func TestAsHTTPExceptionFilter_Nil(t *testing.T) {
	expectNoPanic(t, "B11: AsHTTPExceptionFilter(nil) must not panic", func() {
		fn, ok := AsHTTPExceptionFilter(nil)
		if fn != nil || ok {
			t.Error(test.DiffMessage(ok, false, "B11: nil is not an exception filter"))
		}
	})
}
