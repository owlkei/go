package core

import (
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

type hookBuilderProvider struct{}

func (p hookBuilderProvider) NewProvider() Provider { return p }

type hookBuilderController struct{}

func (c hookBuilderController) NewController() Controller { return c }

func TestNewModuleBuilder_ChainReturnsSameBuilder(t *testing.T) {
	rb := NewModuleBuilder()
	got := rb.Imports().Providers(hookBuilderProvider{}).Controllers(hookBuilderController{})
	if got != rb {
		t.Error(test.DiffMessage(got, rb, "B9: Imports, Providers and Controllers return the same *ModuleBuilder"))
	}
	m := rb.Build()
	if m == nil || len(m.providers) != 1 || len(m.controllers) != 1 {
		t.Error(test.DiffMessage(m, "<module with one provider and one controller>", "B9: Build keeps what the chain set"))
	}
}

func TestModuleBuilder_IsAnExportedStruct(t *testing.T) {
	typ := reflect.TypeFor[ModuleBuilder]()
	if typ.Kind() != reflect.Struct || typ.Name() != "ModuleBuilder" {
		t.Error(test.DiffMessage(typ.String(), "core.ModuleBuilder struct", "D3: the builder type is exported as core.ModuleBuilder"))
	}
}
