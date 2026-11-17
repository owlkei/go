package common

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

type mockGuarder struct{}

func (mockGuarder) CanActivate(_ *ctx.HTTPContext) bool { return true }

type mockWSGuarder struct{}

func (mockWSGuarder) CanActivate(_ *ctx.WSContext) bool { return true }

type denyGuarder struct{}

func (denyGuarder) CanActivate(_ *ctx.HTTPContext) bool { return false }

type denyWSGuarder struct{}

func (denyWSGuarder) CanActivate(_ *ctx.WSContext) bool { return false }

type noCanActivateGuarder struct{}

type wrongReturnGuarder struct{}

func (wrongReturnGuarder) CanActivate(_ *ctx.HTTPContext) string { return "" }

type wrongParamGuarder struct{}

func (wrongParamGuarder) CanActivate(_ int) bool { return true }

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
