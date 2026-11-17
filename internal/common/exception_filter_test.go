package common

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/routing"
	"github.com/dangduoc08/ginject/internal/test"
)

type mockExFilter struct{}

func (mockExFilter) Catch(_ *ctx.HTTPContext, _ *exception.Exception) {}

type mockWSExFilter struct{}

func (mockWSExFilter) Catch(_ *ctx.WSContext, _ *exception.Exception) {}

type noCatchExFilter struct{}

type wrongParamExFilter struct{}

func (wrongParamExFilter) Catch(_ int, _ *exception.Exception) {}

var noopCB = func(_ int, _ reflect.Type, _ reflect.Value, _ reflect.Value) {}

var testConstruct = func(obj any, constructor string) any {
	if method := reflect.ValueOf(obj).MethodByName(constructor); method.IsValid() {
		return method.Call(nil)[0].Interface()
	}
	return obj
}

func buildHTTP(fnToRoute map[string]string) *HTTP {
	r := &HTTP{
		patternToFuncNameMap: make(map[string]string, len(fnToRoute)),
		funcNameToPatternMap: make(map[string]string, len(fnToRoute)),
	}
	for fn, route := range fnToRoute {
		p := routing.MethodRouteVersionToPattern("GET", route, "")
		HTTPPatternToFuncNameMap(r)[p] = fn
		HTTPFuncNameToPatternMap(r)[fn] = p
	}
	return r
}

func buildWS(patternToFn map[string]string) *WS {
	ws := &WS{
		funcNameByEvent: make(map[string]string, len(patternToFn)),
	}
	for p, fn := range patternToFn {
		ws.funcNameByEvent[p] = fn
	}
	return ws
}

func TestBindExceptionFilter_Chaining(t *testing.T) {
	e := &ExceptionFilter{}
	ret := e.BindExceptionFilter(mockExFilter{})
	if ret != e {
		t.Error(test.DiffMessage(ret, e, "BindExceptionFilter should return self"))
	}
	if len(e.exceptionFilterHandlers) != 1 {
		t.Error(test.DiffMessage(len(e.exceptionFilterHandlers), 1, "one handler after one bind"))
	}
	e.BindExceptionFilter(mockExFilter{})
	if len(e.exceptionFilterHandlers) != 2 {
		t.Error(test.DiffMessage(len(e.exceptionFilterHandlers), 2, "two handlers after two binds"))
	}
}

func TestExceptionFilterShapeError_MessageContainsType(t *testing.T) {
	err := ExceptionFilterShapeError(noCatchExFilter{})
	if err == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil error", "ExceptionFilterShapeError must not return nil"))
	}
	if !strings.Contains(err.Error(), "noCatchExFilter") {
		t.Error(test.DiffMessage(err.Error(), "contains noCatchExFilter", "error message should name the offending type"))
	}
}

func TestNormalizeRecovered_ExceptionPassthrough(t *testing.T) {
	original := exception.ForbiddenException("nope")
	got := NormalizeRecovered(original)
	if got.Error() != original.Error() {
		t.Error(test.DiffMessage(got.Error(), original.Error(), "an *exception.Exception panic must pass through unchanged"))
	}
}

func TestNormalizeRecovered_ErrorValue(t *testing.T) {
	got := NormalizeRecovered(errors.New("boom"))
	if got.Message() != "boom" {
		t.Error(test.DiffMessage(got.Message(), "boom", "an error panic should use its Error() text as the response"))
	}
}

func TestNormalizeRecovered_StringValue(t *testing.T) {
	got := NormalizeRecovered("boom string")
	if got.Message() != "boom string" {
		t.Error(test.DiffMessage(got.Message(), "boom string", "a string panic should be used verbatim as the response"))
	}
}

func TestNormalizeRecovered_UnknownTypeUsesGenericMessage(t *testing.T) {
	got := NormalizeRecovered(42)
	want := http.StatusText(http.StatusInternalServerError)
	if got.Message() != want {
		t.Error(test.DiffMessage(got.Message(), want, "an unrecognized panic value should fall back to the generic 500 text"))
	}
}

type customError struct{ text string }

func (e *customError) Error() string { return e.text }

func TestNormalizeRecovered_PointerIsReturnedAsIs(t *testing.T) {
	original := exception.NotFoundException("nope")

	got := NormalizeRecovered(&original)

	if got != &original {
		t.Error(test.DiffMessage(got, &original, "B21: a re-thrown pointer is returned as is"))
	}
	if got.Code() != http.StatusNotFound {
		t.Error(test.DiffMessage(got.Code(), http.StatusNotFound, "B21: the status code is kept"))
	}
	if got.Message() != "nope" {
		t.Error(test.DiffMessage(got.Message(), "nope", "B21: the message is kept"))
	}
}

func TestNormalizeRecovered_NilPointerIsUnknown(t *testing.T) {
	var rec *exception.Exception

	var got *exception.Exception
	if text, panicked := recoverValue(func() { got = NormalizeRecovered(rec) }); panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "B21: a nil pointer must not panic"))
	}

	if got == nil {
		t.Fatal(test.DiffMessage(got, "an exception", "B21: a nil pointer becomes an exception"))
	}
	if got.Code() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(got.Code(), http.StatusInternalServerError, "B21: code"))
	}
	if got.Message() != "Internal Server Error" {
		t.Error(test.DiffMessage(got.Message(), "Internal Server Error", "B21: message"))
	}
}

func TestNormalizeRecovered_ValueKeepsFields(t *testing.T) {
	original := exception.ForbiddenException("nope")

	got := NormalizeRecovered(original)

	if got == &original {
		t.Error(test.DiffMessage(got, "a pointer to a copy", "B22: the value is copied"))
	}
	if got.Code() != original.Code() {
		t.Error(test.DiffMessage(got.Code(), original.Code(), "B22: code"))
	}
	if got.Message() != original.Message() {
		t.Error(test.DiffMessage(got.Message(), original.Message(), "B22: message"))
	}
	if got.Error() != original.Error() {
		t.Error(test.DiffMessage(got.Error(), original.Error(), "B22: Error()"))
	}
}

func TestNormalizeRecovered_ErrorBecomes500(t *testing.T) {
	got := NormalizeRecovered(errors.New("boom"))

	if got.Code() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(got.Code(), http.StatusInternalServerError, "B23: code"))
	}
	if got.Message() != "boom" {
		t.Error(test.DiffMessage(got.Message(), "boom", "B23: message is err.Error()"))
	}
	if got.Error() != "Internal Server Error" {
		t.Error(test.DiffMessage(got.Error(), "Internal Server Error", "B23: Error()"))
	}
}

func TestNormalizeRecovered_CustomErrorBecomes500(t *testing.T) {
	got := NormalizeRecovered(&customError{text: "driver: connection refused"})

	if got.Code() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(got.Code(), http.StatusInternalServerError, "B23: code"))
	}
	if got.Message() != "driver: connection refused" {
		t.Error(test.DiffMessage(got.Message(), "driver: connection refused", "B23: message"))
	}
}

func TestNormalizeRecovered_StringBecomes500(t *testing.T) {
	got := NormalizeRecovered("boom string")

	if got.Code() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(got.Code(), http.StatusInternalServerError, "B23: code"))
	}
	if got.Message() != "boom string" {
		t.Error(test.DiffMessage(got.Message(), "boom string", "B23: message is the string"))
	}
}

func TestNormalizeRecovered_NilIs500(t *testing.T) {
	got := NormalizeRecovered(nil)

	if got.Code() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(got.Code(), http.StatusInternalServerError, "B24: code"))
	}
	if got.Message() != "Internal Server Error" {
		t.Error(test.DiffMessage(got.Message(), "Internal Server Error", "B24: message"))
	}
}

func TestNormalizeRecovered_OtherValueIs500(t *testing.T) {
	type payload struct{ secret string }
	for _, rec := range []any{42, 3.5, []string{"a"}, payload{secret: "token"}, &payload{secret: "token"}} {
		got := NormalizeRecovered(rec)

		if got.Code() != http.StatusInternalServerError {
			t.Error(test.DiffMessage(got.Code(), http.StatusInternalServerError, fmt.Sprintf("B24: %T code", rec)))
		}
		if got.Message() != "Internal Server Error" {
			t.Error(test.DiffMessage(got.Message(), "Internal Server Error", fmt.Sprintf("B24: %T message", rec)))
		}
	}
}

func TestNormalizeRecovered_WrappedExceptionKeepsCode(t *testing.T) {
	wrapped := fmt.Errorf("load user: %w", exception.NotFoundException("x"))

	got := NormalizeRecovered(wrapped)

	if got.Code() != http.StatusNotFound {
		t.Error(test.DiffMessage(got.Code(), http.StatusNotFound, "Q4: a wrapped exception keeps its code"))
	}
}

func TestNormalizeRecovered_DeeplyWrappedExceptionKeepsCode(t *testing.T) {
	inner := exception.ForbiddenException("x")
	wrapped := fmt.Errorf("outer: %w", fmt.Errorf("middle: %w", inner))

	got := NormalizeRecovered(wrapped)

	if got.Code() != http.StatusForbidden {
		t.Error(test.DiffMessage(got.Code(), http.StatusForbidden, "Q4: errors.As reaches through several wrappers"))
	}
}

func TestNormalizeRecovered_WrappedPlainErrorBecomes500(t *testing.T) {
	wrapped := fmt.Errorf("load user: %w", errors.New("disk"))

	got := NormalizeRecovered(wrapped)

	if got.Code() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(got.Code(), http.StatusInternalServerError, "Q4: an error without an exception is a 500"))
	}
	if got.Message() != "load user: disk" {
		t.Error(test.DiffMessage(got.Message(), "load user: disk", "B23: message is err.Error()"))
	}
}
