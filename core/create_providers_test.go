package core

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

var pvBuilt atomic.Int64

type pvOnceProvider struct{}

func (p pvOnceProvider) NewProvider() Provider {
	pvBuilt.Add(1)
	return p
}

func TestProviders_ConstructedOncePerApp(t *testing.T) {
	pvBuilt.Store(0)

	shared := ModuleBuilder().Providers(pvOnceProvider{}).Build()
	left := ModuleBuilder().Imports(shared).Providers(pvOnceProvider{}).Build()
	right := ModuleBuilder().Imports(shared).Providers(pvOnceProvider{}).Build()
	mustCreate(t, New(), ModuleBuilder().Imports(left, right).Providers(pvOnceProvider{}).Build())

	if got := pvBuilt.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "P1: a provider type is constructed once per app, however many modules list it"))
	}
}

type pvTagProvider struct{ Value string }

func (p pvTagProvider) NewProvider() Provider {
	p.Value = "built"
	return p
}

type pvTagReader struct{ Tag pvTagProvider }

var pvSeen createRecorder

func (p pvTagReader) NewProvider() Provider {
	pvSeen.add("reader:" + p.Tag.Value)
	return p
}

func TestProviders_DependenciesFirst_SameModule(t *testing.T) {
	pvSeen = createRecorder{}

	mustCreate(t, New(), ModuleBuilder().Providers(pvTagReader{}, pvTagProvider{}).Build())

	if got := sprint(pvSeen.list()); got != "[reader:built]" {
		t.Error(test.DiffMessage(got, "[reader:built]", "P2: a dependency declared later is constructed first and injected as built"))
	}
}

type pvChainC struct{ B pvChainB }

type pvChainB struct{ A pvChainA }

type pvChainA struct{}

var pvChain createRecorder

func (p pvChainA) NewProvider() Provider { pvChain.add("A"); return p }
func (p pvChainB) NewProvider() Provider { pvChain.add("B"); return p }
func (p pvChainC) NewProvider() Provider { pvChain.add("C"); return p }

func TestProviders_DependenciesFirst_AcrossModules(t *testing.T) {
	pvChain = createRecorder{}

	leaf := ModuleBuilder().Providers(pvChainA{}).Build()
	middle := ModuleBuilder().Imports(leaf).Providers(pvChainB{}).Build()
	root := ModuleBuilder().Imports(middle).Providers(pvChainC{}).Build()
	mustCreate(t, New(), root)

	if got := sprint(pvChain.list()); got != "[A B C]" {
		t.Error(test.DiffMessage(got, "[A B C]", "P2: dependencies in imported modules are constructed before their dependents"))
	}
}

type pvGlobalDep struct{ Value string }

func (p pvGlobalDep) NewProvider() Provider {
	p.Value = "built"
	return p
}

type pvGlobalUser struct{ G pvGlobalDep }

var pvGlobalSeen createRecorder

func (p pvGlobalUser) NewProvider() Provider {
	pvGlobalSeen.add("user:" + p.G.Value)
	return p
}

func TestProviders_GlobalInjectedConstructed(t *testing.T) {
	pvGlobalSeen = createRecorder{}

	user := ModuleBuilder().Providers(pvGlobalUser{}).Build()
	global := ModuleBuilder().Providers(pvGlobalDep{}).Build()
	global.IsGlobal = true
	mustCreate(t, New(), ModuleBuilder().Imports(user, global).Build())

	if got := sprint(pvGlobalSeen.list()); got != "[user:built]" {
		t.Error(test.DiffMessage(got, "[user:built]", "P3: a global provider injected into a module that does not import it is the constructed one"))
	}
}

func TestProviders_RootProviderInjectedConstructed(t *testing.T) {
	pvGlobalSeen = createRecorder{}

	user := ModuleBuilder().Providers(pvGlobalUser{}).Build()
	mustCreate(t, New(), ModuleBuilder().Imports(user).Providers(pvGlobalDep{}).Build())

	if got := sprint(pvGlobalSeen.list()); got != "[user:built]" {
		t.Error(test.DiffMessage(got, "[user:built]", "P3: a root provider injected into an imported module is the constructed one"))
	}
}

type pvDeepDep struct{ Value string }

func (p pvDeepDep) NewProvider() Provider {
	p.Value = "built"
	return p
}

type pvDeepUser struct{ D pvDeepDep }

var pvDeepSeen createRecorder

func (p pvDeepUser) NewProvider() Provider {
	pvDeepSeen.add("user:" + p.D.Value)
	return p
}

func TestProviders_GlobalAtDepthTwo(t *testing.T) {
	pvDeepSeen = createRecorder{}

	global := ModuleBuilder().Providers(pvDeepDep{}).Build()
	global.IsGlobal = true
	holder := ModuleBuilder().Imports(global).Build()
	user := ModuleBuilder().Providers(pvDeepUser{}).Build()

	text, panicked := createPanicText(func() {
		New().Create(ModuleBuilder().Imports(holder, user).Build())
	})
	if panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "P4: a global module imported below the root is visible to every module"))
	}
	if got := sprint(pvDeepSeen.list()); got != "[user:built]" {
		t.Error(test.DiffMessage(got, "[user:built]", "P4: the global provider at depth two is injected, constructed"))
	}
}

type pvUnresolvedDep struct{}

func (p pvUnresolvedDep) NewProvider() Provider { return p }

type pvUnresolvedUser struct{ Missing pvUnresolvedDep }

func (p pvUnresolvedUser) NewProvider() Provider { return p }

func TestProviders_UnresolvedPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New().Create(ModuleBuilder().Providers(pvUnresolvedUser{}).Build())
	})

	want := "dependency injection: can't resolve dependency 'core.pvUnresolvedDep' of the provider"
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "P6: an unresolved dependency panics with today's text"))
	}
}

type pvHiddenUser struct {
	hidden pvUnresolvedDep //nolint:unused
}

func (p pvHiddenUser) NewProvider() Provider { return p }

func TestProviders_UnexportedFieldPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New().Create(ModuleBuilder().Providers(pvHiddenUser{}, pvUnresolvedDep{}).Build())
	})

	want := "dependency injection: can't set value to unexported 'hidden' field of the core.pvHiddenUser provider"
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "P6: an unexported field panics with today's text"))
	}
}
