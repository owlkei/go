package core

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.owlky.com/owlky/internal/test"
)

func lfFailFactory() (*Module, error) { return nil, errors.New("lf-factory-boom") }

func lfPanicFactory() *Module { panic("lf-factory-kaboom") }

func lfNilFactory() *Module { return nil }

func lfNilErrFactory() (*Module, error) { return nil, nil }

var lfCountedCalls atomic.Int64

func lfCountedFactory() *Module {
	lfCountedCalls.Add(1)
	return NewModuleBuilder().Build()
}

func lfNeedsAFactory(lfMissingA) *Module { return nil }

func lfNeedsBFactory(lfMissingB) *Module { return nil }

func lfNeedsRepoFactory(lfRepo) *Module { return NewModuleBuilder().Build() }

func TestModuleFactory_CtxCarriesStartupDeadline(t *testing.T) {
	var got atomic.Value
	factory := func(c context.Context) (*Module, error) {
		got.Store(c)
		return NewModuleBuilder().Build(), nil
	}

	app := New().SetStartupTimeout(2 * time.Second)
	before := time.Now()
	mustCreate(t, app, NewModuleBuilder().Imports(factory).Build())
	after := time.Now()

	c, _ := got.Load().(context.Context)
	if c == nil {
		t.Fatal(test.DiffMessage(nil, "a context", "F4: the factory received a context"))
	}
	deadline, ok := c.Deadline()
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "F4: the context has a deadline"))
	}
	if deadline.Before(before.Add(2*time.Second)) || deadline.After(after.Add(2*time.Second)) {
		t.Error(test.DiffMessage(deadline, "Create start + 2s", "F4: the deadline is Create's start plus the timeout"))
	}
}

func TestModuleFactory_CtxCanceledAfterCreate(t *testing.T) {
	var got atomic.Value
	factory := func(c context.Context) (*Module, error) {
		got.Store(c)
		return NewModuleBuilder().Build(), nil
	}

	mustCreate(t, New(), NewModuleBuilder().Imports(factory).Build())

	c, _ := got.Load().(context.Context)
	if c == nil || c.Err() == nil {
		t.Error(test.DiffMessage(c, "a canceled context", "F4: the context is canceled when Create returns"))
	}
}

func TestModuleFactory_CtxCanceledWhenCreatePanics(t *testing.T) {
	var got atomic.Value
	factory := func(c context.Context) (*Module, error) {
		got.Store(c)
		return NewModuleBuilder().Build(), nil
	}

	lfCreatePanic(t, New(), NewModuleBuilder().Imports(factory, lfFailFactory).Build())

	c, _ := got.Load().(context.Context)
	if c == nil || c.Err() == nil {
		t.Error(test.DiffMessage(c, "a canceled context", "F4: the context is canceled when Create panics"))
	}
}

func TestModuleFactory_ErrorStopsCreate(t *testing.T) {
	text := lfCreatePanic(t, New(), NewModuleBuilder().Imports(lfFailFactory).Build())
	want := "invalid module: module factory 'core.lfFailFactory' failed: lf-factory-boom"
	if !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "F6: the label and the error are in the panic"))
	}
}

func TestModuleFactory_PanicStopsCreate(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)

	text := lfCreatePanic(t, app, NewModuleBuilder().Imports(lfPanicFactory).Build())

	want := "invalid module: module factory 'core.lfPanicFactory' panicked: lf-factory-kaboom"
	if !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "F7: Create panics with the factory's label and value"))
	}
	entries := logger.named("ModuleFactoryPanic")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "F7: one ModuleFactoryPanic line"))
	}
	if entries[0].level != "error" {
		t.Error(test.DiffMessage(entries[0].level, "error", "F7: level"))
	}
	if got := lcText(entries[0].field("panic")); !strings.Contains(got, "lf-factory-kaboom") {
		t.Error(test.DiffMessage(got, "lf-factory-kaboom", "F7: panic field"))
	}
	if got := lcText(entries[0].field("module")); !strings.Contains(got, "lfPanicFactory") {
		t.Error(test.DiffMessage(got, "core.lfPanicFactory", "F7: module field"))
	}
	if got := lcText(entries[0].field("stack")); got == "" || got == "<nil>" {
		t.Error(test.DiffMessage(got, "a stack", "F7: stack field"))
	}
}

func TestModuleFactory_NeverReturnsStopsAtDeadline(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	shapes := map[string]any{
		"module":      func() *Module { <-release; return nil },
		"with error":  func() (*Module, error) { <-release; return nil, nil },
		"with ctx":    func(context.Context) (*Module, error) { <-release; return nil, nil },
		"with params": func(lfRepo) *Module { <-release; return nil },
	}
	for name, factory := range shapes {
		app := New().SetStartupTimeout(10 * time.Millisecond)
		sibling := NewModuleBuilder().Providers(lfRepo{}).Build()
		text := lfCreatePanic(t, app, NewModuleBuilder().Imports(sibling, factory).Build())
		if !strings.Contains(text, "invalid module: module factory 'core.") ||
			!strings.Contains(text, "did not return within the startup timeout (10ms)") {
			t.Error(test.DiffMessage(text, "module factory '…' did not return within the startup timeout (10ms)", "F8: "+name+" factory"))
		}
	}
}

func TestModuleFactory_NilModuleStopsCreate(t *testing.T) {
	for _, factory := range []any{lfNilFactory, lfNilErrFactory} {
		text := lfCreatePanic(t, New(), NewModuleBuilder().Imports(factory).Build())
		if !strings.Contains(text, "invalid module: module factory 'core.lfNil") || !strings.Contains(text, "returned a nil module") {
			t.Error(test.DiffMessage(text, "module factory '…' returned a nil module", "F9: a nil module with a nil error"))
		}
	}
}

func TestModuleFactory_CalledOnce(t *testing.T) {
	lfCountedCalls.Store(0)
	first := NewModuleBuilder().Imports(lfCountedFactory).Build()
	second := NewModuleBuilder().Imports(lfCountedFactory).Build()

	mustCreate(t, New(), NewModuleBuilder().Imports(first, second).Build())

	if got := lfCountedCalls.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "F10: one function value is one call per app"))
	}
}

func TestModuleFactory_ResolvesSiblingProvider(t *testing.T) {
	var got atomic.Value
	factory := func(r lfRepo) *Module {
		got.Store(r.ID)
		return NewModuleBuilder().Build()
	}
	db := NewModuleBuilder().Providers(lfRepo{}).Build()

	mustCreate(t, New(), NewModuleBuilder().Imports(db, factory).Build())

	if got.Load() != "built" {
		t.Error(test.DiffMessage(got.Load(), "built", "S1: the factory receives the sibling's built provider"))
	}
}

func TestModuleFactory_ResolvesSiblingReExport(t *testing.T) {
	var got atomic.Value
	factory := func(r lfRepo) *Module {
		got.Store(r.ID)
		return NewModuleBuilder().Build()
	}
	inner := NewModuleBuilder().Providers(lfRepo{}).Build()
	outer := NewModuleBuilder().Imports(inner).Build()

	mustCreate(t, New(), NewModuleBuilder().Imports(outer, factory).Build())

	if got.Load() != "built" {
		t.Error(test.DiffMessage(got.Load(), "built", "S1: a provider visible through the sibling's imports"))
	}
}

func TestModuleFactory_SiblingListedAfter(t *testing.T) {
	var got atomic.Value
	factory := func(r lfRepo) *Module {
		got.Store(r.ID)
		return NewModuleBuilder().Build()
	}
	db := NewModuleBuilder().Providers(lfRepo{}).Build()

	mustCreate(t, New(), NewModuleBuilder().Imports(factory, db).Build())

	if got.Load() != "built" {
		t.Error(test.DiffMessage(got.Load(), "built", "S3: order does not matter, the factory is listed first"))
	}
}

func TestModuleFactory_FactoryAfterFactory(t *testing.T) {
	provider := func() *Module { return NewModuleBuilder().Providers(lfCache{}).Build() }
	for _, providerFirst := range []bool{true, false} {
		var got atomic.Value
		consumer := func(c lfCache) *Module {
			got.Store(c.ID)
			return NewModuleBuilder().Build()
		}
		imports := []any{consumer, provider}
		if providerFirst {
			imports = []any{provider, consumer}
		}

		mustCreate(t, New(), NewModuleBuilder().Imports(imports...).Build())

		if got.Load() != "built" {
			t.Error(test.DiffMessage(got.Load(), "built", "S3: a factory resolves from another factory's module, provider first: "+map[bool]string{true: "yes", false: "no"}[providerFirst]))
		}
	}
}

func TestModuleFactory_GlobalFromReturnedModuleAnyOrder(t *testing.T) {
	provider := func() *Module {
		m := NewModuleBuilder().Providers(lfCache{}).Build()
		m.IsGlobal = true
		return m
	}
	var got atomic.Value
	consumer := func(c lfCache) *Module {
		got.Store(c.ID)
		return NewModuleBuilder().Build()
	}

	mustCreate(t, New(), NewModuleBuilder().Imports(consumer, provider).Build())

	if got.Load() != "built" {
		t.Error(test.DiffMessage(got.Load(), "built", "S2: a global of a returned module is found whatever the order"))
	}
}

func TestModuleFactory_GlobalAtAnyDepth(t *testing.T) {
	deep := NewModuleBuilder().Providers(lfRepo{}).Build()
	deep.IsGlobal = true
	mid := NewModuleBuilder().Imports(NewModuleBuilder().Imports(deep).Build()).Build()
	var got atomic.Value
	factory := func(r lfRepo) *Module {
		got.Store(r.ID)
		return NewModuleBuilder().Build()
	}

	mustCreate(t, New(), NewModuleBuilder().Imports(factory, mid).Build())

	if got.Load() != "built" {
		t.Error(test.DiffMessage(got.Load(), "built", "S2: a global of a static module at any depth"))
	}
}

func TestModuleFactory_OwnProvidersNotInScope(t *testing.T) {
	factory := func(lfRepo) *Module { return NewModuleBuilder().Build() }
	parent := NewModuleBuilder().Providers(lfRepo{}).Imports(factory).Build()

	text := lfCreatePanic(t, New(), NewModuleBuilder().Imports(parent).Build())

	if !strings.Contains(text, "dependency injection: can't resolve argument of 'func(core.lfRepo)") {
		t.Error(test.DiffMessage(text, "dependency injection: can't resolve argument of …", "S1: the parent's own providers are not in scope"))
	}
}

func TestModuleFactory_OwnerOnInitRunsFirst(t *testing.T) {
	events := &lfEvents{}
	db := NewModuleBuilder().Providers(lfRepo{}).Build()
	db.IsGlobal = true
	db.OnInit = func() { events.add("init") }
	factory := func(lfRepo) *Module {
		events.add("factory")
		return NewModuleBuilder().Build()
	}

	mustCreate(t, New(), NewModuleBuilder().Imports(factory, db).Build())

	got := events.all()
	if len(got) < 2 || got[0] != "init" || got[1] != "factory" {
		t.Error(test.DiffMessage(got, []string{"init", "factory"}, "S2: the owner's OnInit runs before the factory"))
	}
}

func TestModuleFactory_OneUnresolvedKeepsMessage(t *testing.T) {
	factory := func(lfMissing) *Module { return NewModuleBuilder().Build() }

	text := lfCreatePanic(t, New(), NewModuleBuilder().Imports(factory).Build())

	key := genFieldKey(reflect.TypeFor[lfMissing]())
	want := "dependency injection: can't resolve argument of 'func(core.lfMissing)'. Please make sure that the argument '" +
		key + "' at index [0] is available in the injected providers"
	if !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "S4: one unresolved factory keeps today's message"))
	}
}

func TestModuleFactory_TwoUnresolvedListsChain(t *testing.T) {
	text := lfCreatePanic(t, New(), NewModuleBuilder().Imports(lfNeedsAFactory, lfNeedsBFactory).Build())

	prefix := "dependency injection: module factories can't be ordered: "
	suffix := ". Nobody provides these types, or the factories need each other"
	if !strings.Contains(text, prefix) || !strings.Contains(text, suffix) {
		t.Error(test.DiffMessage(text, prefix+"…"+suffix, "S5: the message frame"))
	}
	a := strings.Index(text, "'core.lfNeedsAFactory' needs '")
	b := strings.Index(text, "'core.lfNeedsBFactory' needs '")
	if a < 0 || b < 0 || a > b || !strings.Contains(text, " → ") {
		t.Error(test.DiffMessage(text, "'core.lfNeedsAFactory' needs '…' → 'core.lfNeedsBFactory' needs '…'", "S5: the chain is in discovery order"))
	}
	if !strings.Contains(text, "lfMissingA") || !strings.Contains(text, "lfMissingB") {
		t.Error(test.DiffMessage(text, "the missing types", "S5: each link names its type"))
	}
}

func TestModuleFactory_SyncNeedsAsyncProviderPanics(t *testing.T) {
	storage := lfNamed("storage", NewModuleBuilder().Providers(lfRepo{}))
	root := NewModuleBuilder().Imports(lfNeedsRepoFactory).AsyncImports(storage).Build()

	text := lfCreatePanic(t, New(), root)

	for _, want := range []string{
		"invalid module: module factory 'core.lfNeedsRepoFactory' is in Imports and needs '",
		"lfRepo' of 'storage', which is not ready at Create: import it with AsyncImports",
	} {
		if !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "S6: a sync factory needs a provider of an async module"))
		}
	}
}
