package common

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

type ptrGuard struct{ allow bool }

func (g *ptrGuard) CanActivate(*ctx.HTTPContext) bool { return g != nil && g.allow }

type ptrWSGuard struct{ allow bool }

func (g *ptrWSGuard) CanActivate(*ctx.WSContext) bool { return g != nil && g.allow }

type embedGuard struct{ denyGuarder }

type embedWSGuard struct{ denyWSGuarder }

type unexportedGuard struct{}

func (unexportedGuard) canActivate(*ctx.HTTPContext) bool { return true }

type ptrInterceptor struct{ result any }

func (i *ptrInterceptor) Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any {
	if i == nil {
		return "nil"
	}
	return i.result
}

type ptrWSInterceptor struct{ result any }

func (i *ptrWSInterceptor) Intercept(*ctx.WSContext, *aggregation.Aggregation) any {
	if i == nil {
		return "nil"
	}
	return i.result
}

type embedInterceptor struct{ mockInterceptable }

type embedWSInterceptor struct{ mockWSInterceptable }

type unexportedInterceptor struct{}

func (unexportedInterceptor) intercept(*ctx.HTTPContext, *aggregation.Aggregation) any { return nil }

type recordingFilter struct{ got []*exception.Exception }

func (f *recordingFilter) Catch(_ *ctx.HTTPContext, ex *exception.Exception) {
	if f == nil {
		return
	}
	f.got = append(f.got, ex)
}

type recordingWSFilter struct{ got []*exception.Exception }

func (f *recordingWSFilter) Catch(_ *ctx.WSContext, ex *exception.Exception) {
	if f == nil {
		return
	}
	f.got = append(f.got, ex)
}

type embedFilter struct{ mockExFilter }

type embedWSFilter struct{ mockWSExFilter }

type unexportedFilter struct{}

func (unexportedFilter) catch(*ctx.HTTPContext, *exception.Exception) {}

func reflectHTTPGuard(x any) (HTTPCanActivate, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("CanActivate")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(HTTPCanActivate)
	return fn, ok
}

func reflectWSGuard(x any) (WSCanActivate, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("CanActivate")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(WSCanActivate)
	return fn, ok
}

func reflectHTTPInterceptor(x any) (HTTPIntercept, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("Intercept")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(HTTPIntercept)
	return fn, ok
}

func reflectWSInterceptor(x any) (WSIntercept, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("Intercept")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(WSIntercept)
	return fn, ok
}

func reflectHTTPFilter(x any) (HTTPCatch, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("Catch")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(HTTPCatch)
	return fn, ok
}

func reflectWSFilter(x any) (WSCatch, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("Catch")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(WSCatch)
	return fn, ok
}

func guardSubjects() []any {
	return []any{
		mockGuarder{}, denyGuarder{}, &mockGuarder{}, &denyGuarder{},
		ptrGuard{allow: true}, &ptrGuard{allow: true}, &ptrGuard{}, (*ptrGuard)(nil),
		embedGuard{}, &embedGuard{},
		ptrWSGuard{allow: true}, &ptrWSGuard{allow: true}, (*ptrWSGuard)(nil),
		embedWSGuard{}, &embedWSGuard{},
		mockWSGuarder{}, denyWSGuarder{},
		wrongReturnGuarder{}, wrongParamGuarder{}, noCanActivateGuarder{}, unexportedGuard{},
		42, "CanActivate", struct{}{}, []int{1}, func() {},
	}
}

func interceptorSubjects() []any {
	return []any{
		mockInterceptable{}, &mockInterceptable{},
		ptrInterceptor{result: 1}, &ptrInterceptor{result: 1}, (*ptrInterceptor)(nil),
		embedInterceptor{}, &embedInterceptor{},
		mockWSInterceptable{}, &mockWSInterceptable{},
		ptrWSInterceptor{result: 1}, &ptrWSInterceptor{result: 1}, (*ptrWSInterceptor)(nil),
		embedWSInterceptor{}, &embedWSInterceptor{},
		wrongParamInterceptable{}, noInterceptMethod{}, unexportedInterceptor{},
		42, "Intercept", struct{}{}, func() {},
	}
}

func filterSubjects() []any {
	return []any{
		mockExFilter{}, &mockExFilter{},
		recordingFilter{}, &recordingFilter{}, (*recordingFilter)(nil),
		embedFilter{}, &embedFilter{},
		mockWSExFilter{}, &mockWSExFilter{},
		recordingWSFilter{}, &recordingWSFilter{}, (*recordingWSFilter)(nil),
		embedWSFilter{}, &embedWSFilter{},
		wrongParamExFilter{}, noCatchExFilter{}, unexportedFilter{},
		42, "Catch", struct{}{}, func() {},
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

func TestAsHTTPInterceptor_MatchesReflectionLookup(t *testing.T) {
	c := &ctx.HTTPContext{}
	for _, x := range interceptorSubjects() {
		wantFn, wantOK := reflectHTTPInterceptor(x)
		gotFn, gotOK := AsHTTPInterceptor(x)
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

func expectNoPanic(t *testing.T, rule string, fn func()) {
	t.Helper()
	if text, panicked := recoverText(fn); panicked {
		t.Error(test.DiffMessage(text, "no panic", rule))
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

func TestAsWSGuard_Nil(t *testing.T) {
	expectNoPanic(t, "B11: AsWSGuard(nil) must not panic", func() {
		fn, ok := AsWSGuard(nil)
		if fn != nil || ok {
			t.Error(test.DiffMessage(ok, false, "B11: nil is not a guard"))
		}
	})
}

func TestAsHTTPInterceptor_Nil(t *testing.T) {
	expectNoPanic(t, "B11: AsHTTPInterceptor(nil) must not panic", func() {
		fn, ok := AsHTTPInterceptor(nil)
		if fn != nil || ok {
			t.Error(test.DiffMessage(ok, false, "B11: nil is not an interceptor"))
		}
	})
}

func TestAsWSInterceptor_Nil(t *testing.T) {
	expectNoPanic(t, "B11: AsWSInterceptor(nil) must not panic", func() {
		fn, ok := AsWSInterceptor(nil)
		if fn != nil || ok {
			t.Error(test.DiffMessage(ok, false, "B11: nil is not an interceptor"))
		}
	})
}

func TestAsHTTPExceptionFilter_Nil(t *testing.T) {
	expectNoPanic(t, "B11: AsHTTPExceptionFilter(nil) must not panic", func() {
		fn, ok := AsHTTPExceptionFilter(nil)
		if fn != nil || ok {
			t.Error(test.DiffMessage(ok, false, "B11: nil is not an exception filter"))
		}
	})
}

func TestAsWSExceptionFilter_Nil(t *testing.T) {
	expectNoPanic(t, "B11: AsWSExceptionFilter(nil) must not panic", func() {
		fn, ok := AsWSExceptionFilter(nil)
		if fn != nil || ok {
			t.Error(test.DiffMessage(ok, false, "B11: nil is not an exception filter"))
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
