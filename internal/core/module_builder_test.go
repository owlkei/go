package core

import (
	"go/parser"
	"go/token"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestIsDynamicModule(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"func(pkg.Provider) *core.Module", true},
		{"func(pkg.A, pkg.B) *core.Module", true},
		{"func() *core.Module", true},
		{"*core.Module", false},
		{"func() error", false},
		{"", false},
	}
	for _, c := range cases {
		got := isDynamicModule(c.in)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "isDynamicModule("+c.in+")"))
		}
	}
}

func TestModuleBuilderImports(t *testing.T) {
	child := NewModuleBuilder().Build()
	b := NewModuleBuilder().Imports(child)
	if len(b.imports) != 1 {
		t.Error(test.DiffMessage(len(b.imports), 1, "Imports length"))
	}
}

func TestModuleBuilderProviders(t *testing.T) {
	p := &mockProvider{}
	b := NewModuleBuilder().Providers(p)
	if len(b.providers) != 1 {
		t.Error(test.DiffMessage(len(b.providers), 1, "Providers length"))
	}
}

func TestModuleBuilderControllers(t *testing.T) {
	c := &mockController{}
	b := NewModuleBuilder().Controllers(c)
	if len(b.controllers) != 1 {
		t.Error(test.DiffMessage(len(b.controllers), 1, "Controllers length"))
	}
}

func TestModuleBuilderBuild(t *testing.T) {
	p := &mockProvider{}
	c := &mockController{}
	child := NewModuleBuilder().Build()

	m := NewModuleBuilder().
		Imports(child).
		Providers(p).
		Controllers(c).
		Build()

	if m == nil {
		t.Fatal(test.DiffMessage(nil, "*Module", "Build returned nil"))
		return
	}
	if m.id == "" {
		t.Error(test.DiffMessage("", "non-empty id", "module ID should not be empty"))
	}
	if len(m.providers) != 1 {
		t.Error(test.DiffMessage(len(m.providers), 1, "module providers length"))
	}
	if len(m.controllers) != 1 {
		t.Error(test.DiffMessage(len(m.controllers), 1, "module controllers length"))
	}
	if len(m.staticModules) != 1 {
		t.Error(test.DiffMessage(len(m.staticModules), 1, "module staticModules length"))
	}
}

func TestGetModuleTypeStaticOnly(t *testing.T) {
	child1 := NewModuleBuilder().Build()
	child2 := NewModuleBuilder().Build()
	b := NewModuleBuilder().Imports(child1, child2)

	statics, dynamics := b.getModuleType()
	if len(statics) != 2 {
		t.Error(test.DiffMessage(len(statics), 2, "static modules count"))
	}
	if len(dynamics) != 0 {
		t.Error(test.DiffMessage(len(dynamics), 0, "dynamic modules count"))
	}
}

func TestGetModuleTypeInvalidPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error(test.DiffMessage(nil, "panic", "invalid import should panic"))
		}
	}()
	b := NewModuleBuilder().Imports("not a module")
	b.getModuleType()
}

func TestModuleBuilderBuildResolvesNameFromVarFuncLiteral(t *testing.T) {
	m := testStaticModule()
	if m.Name != "core.testStaticModule" {
		t.Error(test.DiffMessage(m.Name, "core.testStaticModule", "resolved module name"))
	}
}

func TestModuleBuilderBuildResolvesNameInGroupedVarDecl(t *testing.T) {
	a := testGroupedModuleA()
	b := testGroupedModuleB()
	if a.Name != "core.testGroupedModuleA" {
		t.Error(test.DiffMessage(a.Name, "core.testGroupedModuleA", "resolved module name"))
	}
	if b.Name != "core.testGroupedModuleB" {
		t.Error(test.DiffMessage(b.Name, "core.testGroupedModuleB", "resolved module name"))
	}
}

func TestModuleBuilderBuildResolvesNameFromVarAssignedFactoryCall(t *testing.T) {
	if testFactoryBackedModule.Name != "core.testFactoryBackedModule" {
		t.Error(test.DiffMessage(testFactoryBackedModule.Name, "core.testFactoryBackedModule", "resolved module name"))
	}
}

func TestModuleBuilderBuildFallsBackToFunctionNameWhenNoEnclosingVar(t *testing.T) {
	m := testModuleFactory()
	if m.Name != "core.testModuleFactory" {
		t.Error(test.DiffMessage(m.Name, "core.testModuleFactory", "resolved module name"))
	}
}

func TestModuleBuilderBuildDoesNotMisattributeNestedFactoryCallToOuterVar(t *testing.T) {
	outer := testOuterModuleWithNestedFactoryImport()
	if outer.Name != "core.testOuterModuleWithNestedFactoryImport" {
		t.Error(test.DiffMessage(outer.Name, "core.testOuterModuleWithNestedFactoryImport", "resolved outer module name"))
	}
	if len(outer.staticModules) != 1 {
		t.Fatalf("expected 1 static submodule, got %d", len(outer.staticModules))
	}

	nested := outer.staticModules[0]
	if nested.Name != "core.testModuleFactory" {
		t.Error(test.DiffMessage(nested.Name, "core.testModuleFactory", "nested module built as an import argument must resolve to its own factory, not the outer var"))
	}
	if nested.Name == outer.Name {
		t.Error(test.DiffMessage(nested.Name, "<different from outer.Name>", "outer and nested modules must not collapse to the same name"))
	}
}

func TestModuleBuilderBuildResolvesNameFromEnclosingTestFunc(t *testing.T) {
	m := NewModuleBuilder().Build()
	if m.Name != "core.TestModuleBuilderBuildResolvesNameFromEnclosingTestFunc" {
		t.Error(test.DiffMessage(m.Name, "core.TestModuleBuilderBuildResolvesNameFromEnclosingTestFunc", "resolved module name"))
	}
}

func TestModuleNameCandidatesAtLine(t *testing.T) {
	src := `package sample

var Module = func() *core.Module {
	return nil
}

var (
	A = func() *core.Module { return nil }
	B = func() *core.Module { return nil }
)

var ConfModule = Register(&Options{})

func Direct() *core.Module {
	return nil
}
`

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, "sample.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		line              int
		allowFuncLitMatch bool
		wantVarName       string
		wantFuncName      string
	}{
		{4, true, "Module", ""},
		{4, false, "", ""},
		{8, true, "A", ""},
		{8, false, "", ""},
		{9, true, "B", ""},
		{12, true, "ConfModule", ""},
		{12, false, "ConfModule", ""},
		{16, true, "", "Direct"},
		{16, false, "", "Direct"},
	}
	for _, c := range cases {
		gotVar, gotFunc := moduleNameCandidatesAtLine(fset, astFile, c.line, c.allowFuncLitMatch)
		if gotVar != c.wantVarName || gotFunc != c.wantFuncName {
			t.Error(test.DiffMessage([2]string{gotVar, gotFunc}, [2]string{c.wantVarName, c.wantFuncName}, "moduleNameCandidatesAtLine"))
		}
	}
}

func TestParseModuleFileUnreadableFile(t *testing.T) {
	_, _, ok := parseModuleFile("/nonexistent/file/does/not/exist.go")
	if ok {
		t.Error(test.DiffMessage(ok, false, "parseModuleFile with unreadable file"))
	}
}

func TestModuleBuilderChaining(t *testing.T) {
	p1 := &mockProvider{}
	p2 := &mockProvider{}
	c1 := &mockController{}

	b := NewModuleBuilder().Providers(p1).Providers(p2).Controllers(c1)
	if len(b.providers) != 2 {
		t.Error(test.DiffMessage(len(b.providers), 2, "chained providers"))
	}
	if len(b.controllers) != 1 {
		t.Error(test.DiffMessage(len(b.controllers), 1, "chained controllers"))
	}
}

func TestPathStyle_InvalidValuePanics(t *testing.T) {
	want := "invalid module: PathStyle needs ginject.PathStyleKebab or ginject.PathStyleAsWritten"
	for _, style := range []common.PathStyle{0, 9, -1} {
		text, panicked := createPanicText(func() { NewModuleBuilder().PathStyle(style) })
		if !panicked || !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "B17a: PathStyle value"))
		}
	}
}

func TestPathStyle_ReturnsTheBuilder(t *testing.T) {
	b := NewModuleBuilder()
	if b.PathStyle(common.PathStyleKebab) != b {
		t.Error(test.DiffMessage("another builder", "the same builder", "B17a: PathStyle returns the builder"))
	}
}

func TestPathStyle_LastCallWins(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleAsWritten).
		PathStyle(common.PathStyleKebab).
		Controllers(psRootController{}).
		Build())
	psExpect(t, app, "B17a", "/root-page", http.StatusOK)

	app = New()
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleKebab).
		PathStyle(common.PathStyleAsWritten).
		Controllers(psRootController{}).
		Build())
	psExpect(t, app, "B17a", "/root_page", http.StatusOK)
}

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
