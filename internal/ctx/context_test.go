package ctx

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

type minimalContext struct{}

func (minimalContext) Reset()                   {}
func (minimalContext) GetID() string            { return "" }
func (minimalContext) Context() context.Context { return context.Background() }

var (
	_ Context = minimalContext{}
	_ Context = (*HTTPContext)(nil)
	_ Context = (*WSContext)(nil)
)

func TestContext_InterfaceMethods(t *testing.T) {
	typ := reflect.TypeFor[Context]()
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		names = append(names, typ.Method(i).Name)
	}
	slices.Sort(names)
	want := []string{"Context", "GetID", "Reset"}
	if !slices.Equal(names, want) {
		t.Error(test.DiffMessage(names, want, "ctx.Context is Reset, GetID and Context, SetID is removed"))
	}
}

func TestContext_Context_ReturnType(t *testing.T) {
	method, ok := reflect.TypeFor[Context]().MethodByName("Context")
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "ctx.Context declares Context()"))
	}
	if method.Type.NumOut() != 1 || method.Type.Out(0) != reflect.TypeFor[context.Context]() || method.Type.NumIn() != 0 {
		t.Error(test.DiffMessage(method.Type, "func() context.Context", "ctx.Context declares Context() context.Context"))
	}
}
