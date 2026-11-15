package common

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestInjectProvidersIntoHTTPExceptionFilters_Empty(t *testing.T) {
	e := &ExceptionFilter{}
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	items := e.InjectProvidersIntoHTTPExceptionFilters(r, noopCB)
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

	items := e.InjectProvidersIntoHTTPExceptionFilters(r, noopCB)
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
	items := e.InjectProvidersIntoHTTPExceptionFilters(r, noopCB)

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
	items := e.InjectProvidersIntoHTTPExceptionFilters(r, noopCB)
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
	e.InjectProvidersIntoHTTPExceptionFilters(r, noopCB)
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
	e.InjectProvidersIntoHTTPExceptionFilters(r, noopCB)
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

func TestRunHTTPCatchChain_InvokesCatch(t *testing.T) {
	c := ctx.NewHTTPContext()

	var gotEx *exception.Exception
	RunHTTPCatchChain(c, []HTTPCatch{
		func(_ *ctx.HTTPContext, ex *exception.Exception) { gotEx = ex },
	}, "boom")

	if gotEx == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil exception", "RunHTTPCatchChain must invoke the catch function"))
	}
	if gotEx.GetMessage() != "boom" {
		t.Error(test.DiffMessage(gotEx.GetMessage(), "boom", "the recovered value must be normalized before being passed to Catch"))
	}
}

func TestRunHTTPCatchChain_FallsBackToNextOnPanic(t *testing.T) {
	c := ctx.NewHTTPContext()

	secondCalled := false
	RunHTTPCatchChain(c, []HTTPCatch{
		func(*ctx.HTTPContext, *exception.Exception) { panic("filter itself panics") },
		func(*ctx.HTTPContext, *exception.Exception) { secondCalled = true },
	}, "boom")

	if !secondCalled {
		t.Error(test.DiffMessage(secondCalled, true, "a panicking catch fn must fall back to the next one in the chain"))
	}
}

func TestRunHTTPCatchChain_EmptyIsNoop(t *testing.T) {
	c := ctx.NewHTTPContext()
	RunHTTPCatchChain(c, nil, "boom")
}

func TestRunHTTPCatchChain_EmptyDoesNothing(t *testing.T) {
	c := ctx.NewHTTPContext()
	if text, panicked := recoverValue(func() { RunHTTPCatchChain(c, nil, "boom") }); panicked {
		t.Error(test.DiffMessage(text, "no panic", "B17: nil chain"))
	}
	if text, panicked := recoverValue(func() { RunHTTPCatchChain(c, []HTTPCatch{}, "boom") }); panicked {
		t.Error(test.DiffMessage(text, "no panic", "B17: empty chain"))
	}
}

func TestRunHTTPCatchChain_FirstFilterHandles(t *testing.T) {
	c := ctx.NewHTTPContext()
	var gotCtx *ctx.HTTPContext
	var gotEx *exception.Exception
	secondCalls := 0

	RunHTTPCatchChain(c, []HTTPCatch{
		func(arg *ctx.HTTPContext, ex *exception.Exception) { gotCtx, gotEx = arg, ex },
		func(*ctx.HTTPContext, *exception.Exception) { secondCalls++ },
	}, exception.NotFoundException("nope"))

	if gotCtx != c {
		t.Error(test.DiffMessage(gotCtx, c, "B18: the filter receives the request context"))
	}
	if gotEx == nil || gotEx.GetCode() != http.StatusNotFound || gotEx.GetMessage() != "nope" {
		t.Error(test.DiffMessage(gotEx, "404 nope", "B18: the filter receives the normalized exception"))
	}
	if secondCalls != 0 {
		t.Error(test.DiffMessage(secondCalls, 0, "B18: a filter that returns handles the exception"))
	}
}

func TestRunHTTPCatchChain_PanicValueGoesToNextFilter(t *testing.T) {
	c := ctx.NewHTTPContext()
	var got *exception.Exception

	RunHTTPCatchChain(c, []HTTPCatch{
		func(_ *ctx.HTTPContext, ex *exception.Exception) { panic(*ex) },
		func(_ *ctx.HTTPContext, ex *exception.Exception) { got = ex },
	}, exception.NotFoundException("nope"))

	if got == nil || got.GetCode() != http.StatusNotFound || got.GetMessage() != "nope" {
		t.Error(test.DiffMessage(got, "404 nope", "B19: a re-thrown value keeps its code"))
	}
}

func TestRunHTTPCatchChain_PanicPointerKeepsCode(t *testing.T) {
	c := ctx.NewHTTPContext()
	var got *exception.Exception

	RunHTTPCatchChain(c, []HTTPCatch{
		func(_ *ctx.HTTPContext, ex *exception.Exception) { panic(ex) },
		func(_ *ctx.HTTPContext, ex *exception.Exception) { got = ex },
	}, exception.NotFoundException("nope"))

	if got == nil || got.GetCode() != http.StatusNotFound {
		t.Error(test.DiffMessage(got, "404", "B19, B21: a re-thrown pointer keeps its status code"))
	}
	if got != nil && got.GetMessage() != "nope" {
		t.Error(test.DiffMessage(got.GetMessage(), "nope", "B19, B21: a re-thrown pointer keeps its message"))
	}
}

func TestRunHTTPCatchChain_PanicErrorGoesToNextFilter(t *testing.T) {
	c := ctx.NewHTTPContext()
	var got *exception.Exception

	RunHTTPCatchChain(c, []HTTPCatch{
		func(*ctx.HTTPContext, *exception.Exception) { panic(errors.New("filter broke")) },
		func(_ *ctx.HTTPContext, ex *exception.Exception) { got = ex },
	}, "boom")

	if got == nil || got.GetCode() != http.StatusInternalServerError || got.GetMessage() != "filter broke" {
		t.Error(test.DiffMessage(got, "500 filter broke", "B19: an error panic becomes a 500 for the next filter"))
	}
}

func TestRunHTTPCatchChain_NilPanicGoesToNextFilter(t *testing.T) {
	c := ctx.NewHTTPContext()
	var got *exception.Exception

	RunHTTPCatchChain(c, []HTTPCatch{
		func(*ctx.HTTPContext, *exception.Exception) { panic(nil) },
		func(_ *ctx.HTTPContext, ex *exception.Exception) { got = ex },
	}, "boom")

	if got == nil || got.GetCode() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(got, "a 500", "B19: panic(nil) is still handed to the next filter"))
	}
}

func TestRunHTTPCatchChain_AllPanicReturns(t *testing.T) {
	c := ctx.NewHTTPContext()
	var order []int

	text, panicked := recoverValue(func() {
		RunHTTPCatchChain(c, []HTTPCatch{
			func(*ctx.HTTPContext, *exception.Exception) { order = append(order, 1); panic("one") },
			func(*ctx.HTTPContext, *exception.Exception) { order = append(order, 2); panic("two") },
			func(*ctx.HTTPContext, *exception.Exception) { order = append(order, 3); panic("three") },
		}, "boom")
	})

	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "B20: the chain never lets a panic out"))
	}
	if !reflect.DeepEqual(order, []int{1, 2, 3}) {
		t.Error(test.DiffMessage(order, []int{1, 2, 3}, "B20: each filter is called once, in order"))
	}
}

func TestRunHTTPCatchChain_DoesNotModifySlice(t *testing.T) {
	c := ctx.NewHTTPContext()
	calls := 0
	catchFns := []HTTPCatch{
		func(*ctx.HTTPContext, *exception.Exception) { calls++; panic("one") },
		func(*ctx.HTTPContext, *exception.Exception) { calls++; panic("two") },
		func(*ctx.HTTPContext, *exception.Exception) { calls++ },
	}
	before := make([]uintptr, len(catchFns))
	for i, fn := range catchFns {
		before[i] = reflect.ValueOf(fn).Pointer()
	}

	RunHTTPCatchChain(c, catchFns, "boom")
	RunHTTPCatchChain(c, catchFns, "boom")

	if len(catchFns) != 3 || cap(catchFns) != 3 {
		t.Error(test.DiffMessage([]int{len(catchFns), cap(catchFns)}, []int{3, 3}, "B20: the slice header is unchanged"))
	}
	for i, fn := range catchFns {
		if reflect.ValueOf(fn).Pointer() != before[i] {
			t.Error(test.DiffMessage(reflect.ValueOf(fn).Pointer(), before[i], "B20: filter slot is unchanged"))
		}
	}
	if calls != 6 {
		t.Error(test.DiffMessage(calls, 6, "B20: a second run behaves like the first"))
	}
}

func TestRunHTTPCatchChain_LongChainOfPanickingFiltersReturns(t *testing.T) {
	c := ctx.NewHTTPContext()
	calls := 0
	catchFns := make([]HTTPCatch, 20000)
	for i := range catchFns {
		catchFns[i] = func(*ctx.HTTPContext, *exception.Exception) { calls++; panic("again") }
	}

	if text, panicked := recoverValue(func() { RunHTTPCatchChain(c, catchFns, "boom") }); panicked {
		t.Error(test.DiffMessage(text, "no panic", "B20: a long chain returns normally"))
	}
	if calls != len(catchFns) {
		t.Error(test.DiffMessage(calls, len(catchFns), "B20: every filter is called once"))
	}
}
