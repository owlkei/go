package common

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"runtime"
	"testing"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/test"
)

func TestInjectProvidersIntoWSExceptionFilters_Empty(t *testing.T) {
	e := &ExceptionFilter{}
	ws := buildWS(map[string]string{"message": "ON_message"})

	items := InjectProvidersIntoWSExceptionFilters(e, ws, noopCB, testConstruct)
	if len(items) != 0 {
		t.Error(test.DiffMessage(len(items), 0, "no bound filters → empty result"))
	}
}

func TestInjectProvidersIntoWSExceptionFilters_ApplyAll(t *testing.T) {
	e := &ExceptionFilter{}
	e.BindExceptionFilter(mockWSExFilter{})

	ws := buildWS(map[string]string{
		"message": "ON_message",
		"status":  "ON_status",
	})

	items := InjectProvidersIntoWSExceptionFilters(e, ws, noopCB, testConstruct)
	if len(items) != 2 {
		t.Error(test.DiffMessage(len(items), 2, "filter with no handlers applies to all WS patterns"))
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

func TestInjectProvidersIntoWSExceptionFilters_HandlerIsCallableCatch(t *testing.T) {
	e := &ExceptionFilter{}
	e.BindExceptionFilter(mockWSExFilter{})

	ws := buildWS(map[string]string{"message": "ON_message"})
	items := InjectProvidersIntoWSExceptionFilters(e, ws, noopCB, testConstruct)
	if len(items) != 1 {
		t.Fatal(test.DiffMessage(len(items), 1, "one pattern → one item"))
	}

	if _, ok := items[0].WS.Common.Handler.(WSCatch); !ok {
		t.Fatal(test.DiffMessage(items[0].WS.Common.Handler, "WSCatch", "Handler must be callable as WSCatch"))
	}
}

func TestInjectProvidersIntoWSExceptionFilters_NoCatch_Panics(t *testing.T) {
	e := &ExceptionFilter{}
	e.BindExceptionFilter(noCatchExFilter{})
	ws := buildWS(map[string]string{"message": "ON_message"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "filter with no Catch method must panic"))
		}
	}()
	InjectProvidersIntoWSExceptionFilters(e, ws, noopCB, testConstruct)
}

func TestAsWSExceptionFilter_Valid(t *testing.T) {
	fn, ok := AsWSExceptionFilter(mockWSExFilter{})
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "mockWSExFilter must match WSCatch"))
	}
	ex := exception.InternalServerErrorException("")
	fn(nil, &ex)
}

func TestAsWSExceptionFilter_NoMethod(t *testing.T) {
	_, ok := AsWSExceptionFilter(noCatchExFilter{})
	if ok {
		t.Error(test.DiffMessage(true, false, "a filter with no Catch must not match"))
	}
}

func TestAsWSExceptionFilter_WrongShape(t *testing.T) {
	_, ok := AsWSExceptionFilter(wrongParamExFilter{})
	if ok {
		t.Error(test.DiffMessage(true, false, "a Catch with a non-context first param must not match"))
	}
}

func TestRunWSCatchChain_InvokesCatch(t *testing.T) {
	c := ctx.NewWSContext()

	var gotEx *exception.Exception
	RunWSCatchChain(c, []WSCatch{
		func(_ *ctx.WSContext, ex *exception.Exception) { gotEx = ex },
	}, "boom")

	if gotEx == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil exception", "RunWSCatchChain must invoke the catch function"))
	}
	if gotEx.Message() != "boom" {
		t.Error(test.DiffMessage(gotEx.Message(), "boom", "the recovered value must be normalized before being passed to Catch"))
	}
}

func TestRunWSCatchChain_FallsBackToNextOnPanic(t *testing.T) {
	c := ctx.NewWSContext()

	secondCalled := false
	RunWSCatchChain(c, []WSCatch{
		func(*ctx.WSContext, *exception.Exception) { panic("filter itself panics") },
		func(*ctx.WSContext, *exception.Exception) { secondCalled = true },
	}, "boom")

	if !secondCalled {
		t.Error(test.DiffMessage(secondCalled, true, "a panicking catch fn must fall back to the next one in the chain"))
	}
}

func TestRunWSCatchChain_EmptyIsNoop(t *testing.T) {
	c := ctx.NewWSContext()
	RunWSCatchChain(c, nil, "boom")
}

func TestRunWSCatchChain_EmptyDoesNothing(t *testing.T) {
	c := ctx.NewWSContext()
	if text, panicked := recoverValue(func() { RunWSCatchChain(c, nil, "boom") }); panicked {
		t.Error(test.DiffMessage(text, "no panic", "B17: nil chain"))
	}
	if text, panicked := recoverValue(func() { RunWSCatchChain(c, []WSCatch{}, "boom") }); panicked {
		t.Error(test.DiffMessage(text, "no panic", "B17: empty chain"))
	}
}

func TestRunWSCatchChain_FirstFilterHandles(t *testing.T) {
	c := ctx.NewWSContext()
	var gotCtx *ctx.WSContext
	var gotEx *exception.Exception
	secondCalls := 0

	RunWSCatchChain(c, []WSCatch{
		func(arg *ctx.WSContext, ex *exception.Exception) { gotCtx, gotEx = arg, ex },
		func(*ctx.WSContext, *exception.Exception) { secondCalls++ },
	}, exception.NotFoundException("nope"))

	if gotCtx != c {
		t.Error(test.DiffMessage(gotCtx, c, "B18: the filter receives the request context"))
	}
	if gotEx == nil || gotEx.Code() != http.StatusNotFound || gotEx.Message() != "nope" {
		t.Error(test.DiffMessage(gotEx, "404 nope", "B18: the filter receives the normalized exception"))
	}
	if secondCalls != 0 {
		t.Error(test.DiffMessage(secondCalls, 0, "B18: a filter that returns handles the exception"))
	}
}

func TestRunWSCatchChain_PanicValueGoesToNextFilter(t *testing.T) {
	c := ctx.NewWSContext()
	var got *exception.Exception

	RunWSCatchChain(c, []WSCatch{
		func(_ *ctx.WSContext, ex *exception.Exception) { panic(*ex) },
		func(_ *ctx.WSContext, ex *exception.Exception) { got = ex },
	}, exception.NotFoundException("nope"))

	if got == nil || got.Code() != http.StatusNotFound || got.Message() != "nope" {
		t.Error(test.DiffMessage(got, "404 nope", "B19: a re-thrown value keeps its code"))
	}
}

func TestRunWSCatchChain_PanicPointerKeepsCode(t *testing.T) {
	c := ctx.NewWSContext()
	var got *exception.Exception

	RunWSCatchChain(c, []WSCatch{
		func(_ *ctx.WSContext, ex *exception.Exception) { panic(ex) },
		func(_ *ctx.WSContext, ex *exception.Exception) { got = ex },
	}, exception.NotFoundException("nope"))

	if got == nil || got.Code() != http.StatusNotFound {
		t.Error(test.DiffMessage(got, "404", "B19, B21: a re-thrown pointer keeps its status code"))
	}
	if got != nil && got.Message() != "nope" {
		t.Error(test.DiffMessage(got.Message(), "nope", "B19, B21: a re-thrown pointer keeps its message"))
	}
}

func TestRunWSCatchChain_PanicErrorGoesToNextFilter(t *testing.T) {
	c := ctx.NewWSContext()
	var got *exception.Exception

	RunWSCatchChain(c, []WSCatch{
		func(*ctx.WSContext, *exception.Exception) { panic(errors.New("filter broke")) },
		func(_ *ctx.WSContext, ex *exception.Exception) { got = ex },
	}, "boom")

	if got == nil || got.Code() != http.StatusInternalServerError || got.Message() != "filter broke" {
		t.Error(test.DiffMessage(got, "500 filter broke", "B19: an error panic becomes a 500 for the next filter"))
	}
}

func TestRunWSCatchChain_NilPanicGoesToNextFilter(t *testing.T) {
	c := ctx.NewWSContext()
	var got *exception.Exception

	RunWSCatchChain(c, []WSCatch{
		func(*ctx.WSContext, *exception.Exception) { panic(new(runtime.PanicNilError)) },
		func(_ *ctx.WSContext, ex *exception.Exception) { got = ex },
	}, "boom")

	if got == nil || got.Code() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(got, "a 500", "B19: panic(nil) is still handed to the next filter"))
	}
}

func TestRunWSCatchChain_AllPanicReturns(t *testing.T) {
	c := ctx.NewWSContext()
	var order []int

	text, panicked := recoverValue(func() {
		RunWSCatchChain(c, []WSCatch{
			func(*ctx.WSContext, *exception.Exception) { order = append(order, 1); panic("one") },
			func(*ctx.WSContext, *exception.Exception) { order = append(order, 2); panic("two") },
			func(*ctx.WSContext, *exception.Exception) { order = append(order, 3); panic("three") },
		}, "boom")
	})

	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "B20: the chain never lets a panic out"))
	}
	if !reflect.DeepEqual(order, []int{1, 2, 3}) {
		t.Error(test.DiffMessage(order, []int{1, 2, 3}, "B20: each filter is called once, in order"))
	}
}

func TestRunWSCatchChain_DoesNotModifySlice(t *testing.T) {
	c := ctx.NewWSContext()
	calls := 0
	catchFns := []WSCatch{
		func(*ctx.WSContext, *exception.Exception) { calls++; panic("one") },
		func(*ctx.WSContext, *exception.Exception) { calls++; panic("two") },
		func(*ctx.WSContext, *exception.Exception) { calls++ },
	}
	before := make([]uintptr, len(catchFns))
	for i, fn := range catchFns {
		before[i] = reflect.ValueOf(fn).Pointer()
	}

	RunWSCatchChain(c, catchFns, "boom")
	RunWSCatchChain(c, catchFns, "boom")

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

func TestRunWSCatchChain_LongChainOfPanickingFiltersReturns(t *testing.T) {
	c := ctx.NewWSContext()
	calls := 0
	catchFns := make([]WSCatch, 20000)
	for i := range catchFns {
		catchFns[i] = func(*ctx.WSContext, *exception.Exception) { calls++; panic("again") }
	}

	if text, panicked := recoverValue(func() { RunWSCatchChain(c, catchFns, "boom") }); panicked {
		t.Error(test.DiffMessage(text, "no panic", "B20: a long chain returns normally"))
	}
	if calls != len(catchFns) {
		t.Error(test.DiffMessage(calls, len(catchFns), "B20: every filter is called once"))
	}
}

func TestAsWSExceptionFilter_MatchesReflectionLookup(t *testing.T) {
	c := &ctx.WSContext{}
	ex := exception.NotFoundException("x")
	for _, x := range filterSubjects() {
		_, wantOK := reflectWSFilter(x)
		gotFn, gotOK := AsWSExceptionFilter(x)
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

func TestAsWSExceptionFilter_CallsMethodOnTheInstance(t *testing.T) {
	f := &recordingWSFilter{}
	fn, ok := AsWSExceptionFilter(f)
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "B11: pointer receiver filter matches"))
	}
	ex := exception.NotFoundException("x")
	fn(&ctx.WSContext{}, &ex)

	if len(f.got) != 1 || f.got[0] != &ex {
		t.Error(test.DiffMessage(f.got, []*exception.Exception{&ex}, "B11: fn calls Catch on x with the same arguments"))
	}
}

func TestAsWSExceptionFilter_Nil(t *testing.T) {
	expectNoPanic(t, "B11: AsWSExceptionFilter(nil) must not panic", func() {
		fn, ok := AsWSExceptionFilter(nil)
		if fn != nil || ok {
			t.Error(test.DiffMessage(ok, false, "B11: nil is not an exception filter"))
		}
	})
}
