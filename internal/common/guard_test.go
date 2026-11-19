package common

import (
	"reflect"
	"strings"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func TestBindGuard_Chaining(t *testing.T) {
	g := &Guard{}
	ret := g.BindGuard(mockGuarder{})
	if ret != g {
		t.Error(test.DiffMessage(ret, g, "BindGuard should return self"))
	}
	if len(g.guardHandlers) != 1 {
		t.Error(test.DiffMessage(len(g.guardHandlers), 1, "one handler after one bind"))
	}
	g.BindGuard(mockGuarder{})
	if len(g.guardHandlers) != 2 {
		t.Error(test.DiffMessage(len(g.guardHandlers), 2, "two handlers after two binds"))
	}
}

func TestGuardShapeError_MessageContainsType(t *testing.T) {
	err := GuardShapeError(noCanActivateGuarder{})
	if err == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil error", "GuardShapeError must not return nil"))
	}
	if !strings.Contains(err.Error(), "noCanActivateGuarder") {
		t.Error(test.DiffMessage(err.Error(), "contains noCanActivateGuarder", "error message should name the offending type"))
	}
}

func TestBindGuard_AcceptsAnyShape(t *testing.T) {
	anyType := reflect.TypeFor[any]()
	g := &Guard{}
	if got := reflect.TypeOf(g.BindGuard).In(0); got != anyType {
		t.Error(test.DiffMessage(got.String(), "interface {}", "D7: BindGuard takes any, not a named type"))
	}
	if got := reflect.TypeOf((&Interceptor{}).BindInterceptor).In(0); got != anyType {
		t.Error(test.DiffMessage(got.String(), "interface {}", "D7: BindInterceptor takes any, not a named type"))
	}
	if got := reflect.TypeOf((&ExceptionFilter{}).BindExceptionFilter).In(0); got != anyType {
		t.Error(test.DiffMessage(got.String(), "interface {}", "D7: BindExceptionFilter takes any, not a named type"))
	}
	g.BindGuard(42)
	g.BindGuard(nil)
	if len(g.guardHandlers) != 2 {
		t.Error(test.DiffMessage(len(g.guardHandlers), 2, "D7: any shape is stored; Create() checks the shape"))
	}
}
