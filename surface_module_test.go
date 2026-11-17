package ginject_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/modules/cache"
)

type greeter struct {
	Word string
}

func (instance greeter) NewProvider() ginject.Provider { return instance }

type greetController struct {
	ginject.HTTP
	Greeter greeter
}

func (instance greetController) NewController() ginject.Controller { return instance }

func (instance greetController) READ_greet() string { return instance.Greeter.Word }

var greeterModule = func() ginject.Module {
	m := ginject.ModuleBuilder().Providers(greeter{Word: "hi"}).Build()
	m.IsGlobal = true
	return m
}

var funcModule = func(g greeter) ginject.Module {
	return ginject.ModuleBuilder().Controllers(greetController{}).Build()
}

var funcModuleCalls int

var countedModule = func() ginject.Module {
	funcModuleCalls++
	return ginject.ModuleBuilder().Build()
}

func TestSurface_ModuleFunc(t *testing.T) {
	funcModuleCalls = 0
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().Imports(greeterModule, funcModule, countedModule, countedModule).Build())

	w := serve(app, http.MethodGet, "/greet", nil, nil)
	if w.Code != http.StatusOK || w.Body.String() != "hi" {
		t.Error(test.DiffMessage(w.Body.String(), "hi", "B9: a func() ginject.Module is imported and its parameters are resolved from global providers"))
	}
	if funcModuleCalls != 1 {
		t.Error(test.DiffMessage(funcModuleCalls, 1, "B9: a module function is called once"))
	}
}

func TestSurface_ModuleValue(t *testing.T) {
	local := ginject.ModuleBuilder().Providers(greeter{Word: "value"}).Controllers(greetController{}).Build()
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().Imports(local, cache.Register(nil)).Build())

	w := serve(app, http.MethodGet, "/greet", nil, nil)
	if w.Body.String() != "value" {
		t.Error(test.DiffMessage(w.Body.String(), "value", "B9: a ginject.Module value (and a Register result) is imported"))
	}
}

func TestSurface_InvalidModulePanics(t *testing.T) {
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		ginject.New().Create(ginject.ModuleBuilder().Imports("not a module").Build())
	}()
	if recovered == nil {
		t.Fatal(test.DiffMessage("no panic", "a panic", "B9: an import that is not a module panics"))
	}
	got := fmt.Sprint(recovered)
	want := "invalid module: can't pass 'string' type as module"
	if !strings.Contains(got, want) {
		t.Error(test.DiffMessage(got, want, "B9: panic text"))
	}
}

var namedModule = func() ginject.Module {
	return ginject.ModuleBuilder().Build()
}

func TestSurface_ModuleNameResolved(t *testing.T) {
	m := namedModule()
	if m.Name != "ginject_test.namedModule" {
		t.Error(test.DiffMessage(m.Name, "ginject_test.namedModule", "B9: Name is <package>.<var> through ginject.ModuleBuilder()"))
	}
}
