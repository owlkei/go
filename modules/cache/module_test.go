package cache

import (
	"fmt"
	"runtime"
	"testing"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/internal/core"
	"go.owlkei.com/owlkei/internal/inspect"
	"go.owlkei.com/owlkei/internal/memorycache"
	"go.owlkei.com/owlkei/internal/test"
)

func TestRegister_NilOptions(t *testing.T) {
	m := Register(nil)
	if m == nil {
		t.Fatal(test.DiffMessage(nil, "a module", "C2: Register(nil) returns a module"))
	}
	if m.IsGlobal || m.OnInit != nil {
		t.Error(test.DiffMessage(m.IsGlobal, false, "C2: nil options equal empty options"))
	}

	app := core.New(nil)
	app.Create(m)
	svc := serviceOf(t, app)
	if svc.Backend == nil {
		t.Error(test.DiffMessage(nil, "a backend", "C2: Register(nil) has a backend"))
	}
}

func TestRegister_DefaultBackend(t *testing.T) {
	app := core.New(nil)
	app.Create(Register(&CacheModuleOptions{}))
	svc := serviceOf(t, app)

	mc, ok := svc.Backend.(*memorycache.MemoryCache)
	if !ok {
		t.Fatalf("%v", test.DiffMessage(svc.Backend, "*memorycache.MemoryCache", "C2: the default backend is a memory cache"))
	}
	st, ok := inspect.CacheStateOf(mc)
	if !ok || st.MaxEntries != 65536 || st.MaxBytes != 16<<20 {
		t.Error(test.DiffMessage(st, "65536 entries, 16 MiB", "C2: the default backend is memorycache.NewMemoryCache(nil)"))
	}
}

func TestRegister_CustomBackend(t *testing.T) {
	rec := &recordingBackend{}
	app := core.New(nil)
	app.Create(Register(&CacheModuleOptions{Backend: rec}))
	svc := serviceOf(t, app)

	if svc.Backend != Cache(rec) {
		t.Error(test.DiffMessage(svc.Backend, rec, "C2: a Backend that is set is used as is"))
	}
	_, _ = svc.Get(background, "k")
	if len(rec.calls) != 1 || rec.calls[0].method != "Get" {
		t.Error(test.DiffMessage(rec.calls, "one Get call", "C2: the service forwards to the custom backend"))
	}
}

func TestRegister_IsGlobalAndOnInit(t *testing.T) {
	inits := 0
	m := Register(&CacheModuleOptions{IsGlobal: true, OnInit: func() { inits++ }})
	if !m.IsGlobal {
		t.Error(test.DiffMessage(m.IsGlobal, true, "C2: IsGlobal is copied to the module"))
	}
	if m.OnInit == nil {
		t.Fatal(test.DiffMessage(nil, "a function", "C2: OnInit is copied to the module"))
	}
	app := core.New(nil)
	app.Create(m)
	if inits != 1 {
		t.Error(test.DiffMessage(inits, 1, "C2: OnInit runs once when the app is created"))
	}

	local := Register(&CacheModuleOptions{})
	if local.IsGlobal {
		t.Error(test.DiffMessage(true, false, "C2: IsGlobal defaults to false"))
	}
}

func TestRegister_StartsNoGoroutine(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	modules := make([]*core.Module, 20)
	for i := range modules {
		modules[i] = Register(nil)
	}
	if after := runtime.NumGoroutine(); after != before {
		t.Error(test.DiffMessage(after, before, "C3: Register starts no goroutine"))
	}

	app := core.New(nil)
	app.Create(modules[0])
	if st, ok := inspect.CacheStateOf(serviceOf(t, app).Backend); !ok || st.IsSweeping {
		t.Error(test.DiffMessage(st.IsSweeping, false, "C3: Register arms no timer"))
	}
}

func TestRegister_InjectedServicesShareBackend(t *testing.T) {
	cacheModule := Register(&CacheModuleOptions{IsGlobal: true})
	root := owlkei.ModuleBuilder().
		Imports(cacheModule).
		Providers(consumerA{}, consumerB{}).
		Build()

	app := core.New(nil)
	app.Create(root)

	a, okA := app.Get(consumerA{}).(consumerA)
	b, okB := app.Get(consumerB{}).(consumerB)
	if !okA || !okB {
		t.Fatal(test.DiffMessage(fmt.Sprint(okA, okB), "true true", "C4: both consumers are constructed"))
	}
	if a.Cache.Backend == nil || a.Cache.Backend != b.Cache.Backend {
		t.Error(test.DiffMessage(a.Cache.Backend, b.Cache.Backend, "C4: injected copies share one backend"))
	}

	if err := a.Cache.Set(background, "shared", []byte("through-a"), 0); err != nil {
		t.Fatal(err)
	}
	if got, ok := b.Cache.Get(background, "shared"); !ok || string(got) != "through-a" {
		t.Error(test.DiffMessage(string(got), "through-a", "C4: a value set through one copy is read through another"))
	}
	own := serviceOf(t, app)
	if got, ok := own.Get(background, "shared"); !ok || string(got) != "through-a" {
		t.Error(test.DiffMessage(string(got), "through-a", "C4: the module's own service sees it too"))
	}
}

func TestRegister_TwoAppsShareBackend(t *testing.T) {
	m := Register(nil)

	first := core.New(nil)
	first.Create(m)
	second := core.New(nil)
	second.Create(m)

	one, two := serviceOf(t, first), serviceOf(t, second)
	if one.Backend != two.Backend {
		t.Error(test.DiffMessage(one.Backend, two.Backend, "C4: two apps created from one module value share its backend"))
	}
	if err := one.Set(background, "k", []byte("v"), 0); err != nil {
		t.Fatal(err)
	}
	if got, ok := two.Get(background, "k"); !ok || string(got) != "v" {
		t.Error(test.DiffMessage(string(got), "v", "C4: a value set in one app is read in the other"))
	}
}
