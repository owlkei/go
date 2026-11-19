package ctx

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func TestContext_InterfaceMethods(t *testing.T) {
	typ := reflect.TypeFor[Context]()
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		names = append(names, typ.Method(i).Name)
	}
	slices.Sort(names)
	want := []string{"Context", "ID"}
	if !slices.Equal(names, want) {
		t.Error(test.DiffMessage(names, want, "B18: ctx.Context is ID and Context; Reset is a hook function"))
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
