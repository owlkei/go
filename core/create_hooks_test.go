package core

import (
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/internal/test"
)

func hkModule(rec *createRecorder, name string, imports ...any) *Module {
	m := ModuleBuilder().Imports(imports...).Build()
	m.OnInit = rec.hook("init:" + name)
	m.OnReady = rec.hook("ready:" + name)
	m.OnShutdown = rec.hook("shutdown:" + name)
	return m
}

func hkDiamond(rec *createRecorder) *Module {
	b := hkModule(rec, "B")
	a := hkModule(rec, "A", b)
	c := hkModule(rec, "C", b)
	return hkModule(rec, "root", a, c)
}

func hkFilter(items []string, prefix string) []string {
	var out []string
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			out = append(out, strings.TrimPrefix(item, prefix))
		}
	}
	return out
}

func TestHooks_OnInitDependenciesFirst(t *testing.T) {
	rec := &createRecorder{}
	mustCreate(t, New(), hkDiamond(rec))

	got := sprint(hkFilter(rec.list(), "init:"))
	if want := "[B A C root]"; got != want {
		t.Error(test.DiffMessage(got, want, "K1: OnInit runs for a module's imports first, in import order"))
	}
}

func TestHooks_DiamondImportOnce(t *testing.T) {
	rec := &createRecorder{}
	mustCreate(t, New(), hkDiamond(rec))

	for _, name := range []string{"B", "A", "C", "root"} {
		if got := rec.count("init:" + name); got != 1 {
			t.Error(test.DiffMessage(got, 1, "K1: OnInit of "+name+" runs once per app even when imported twice"))
		}
		if got := rec.count("ready:" + name); got != 1 {
			t.Error(test.DiffMessage(got, 1, "K2: OnReady of "+name+" runs once per app even when imported twice"))
		}
	}
}

type hkProvider struct{}

var hkEvents createRecorder

func (p hkProvider) NewProvider() Provider {
	hkEvents.add("provider")
	return p
}

func TestHooks_OnInitBeforeOwnProviders(t *testing.T) {
	hkEvents = createRecorder{}

	child := ModuleBuilder().Providers(hkProvider{}).Build()
	child.OnInit = func() { hkEvents.add("init:child") }
	root := ModuleBuilder().Imports(child).Build()
	root.OnInit = func() { hkEvents.add("init:root") }
	mustCreate(t, New(), root)

	got := sprint(hkEvents.list())
	if want := "[init:child provider init:root]"; got != want {
		t.Error(test.DiffMessage(got, want, "K1: a module's OnInit runs, then its providers are constructed, before the next module"))
	}
}

func TestHooks_OnReadySameOrderAfterWiring(t *testing.T) {
	rec := &createRecorder{}
	app := New()
	root := hkDiamond(rec)
	var seen []bool
	root.OnReady = func() {
		rec.add("ready:root")
		seen = append(seen, app.Get(hkProvider{}) != nil)
	}
	rootWithProvider := ModuleBuilder().Imports(root).Providers(hkProvider{}).Build()
	rootWithProvider.OnReady = func() {
		rec.add("ready:outer")
		seen = append(seen, app.Get(hkProvider{}) != nil)
	}
	mustCreate(t, app, rootWithProvider)

	got := sprint(hkFilter(rec.list(), "ready:"))
	if want := "[B A C root outer]"; got != want {
		t.Error(test.DiffMessage(got, want, "K2: OnReady runs in the order of K1"))
	}
	if sprint(seen) != "[true true]" {
		t.Error(test.DiffMessage(sprint(seen), "[true true]", "K2: app.Get works inside OnReady, the wiring is done"))
	}
}

func TestHooks_OnInitPanicStopsCreate(t *testing.T) {
	rec := &createRecorder{}
	child := hkModule(rec, "child")
	child.OnInit = func() { panic("init boom") }
	root := hkModule(rec, "root", child)

	value, panicked := createPanicValue(func() { New().Create(root) })

	if !panicked || value != "init boom" {
		t.Error(test.DiffMessage(value, "init boom", "K3: a panic in OnInit reaches the caller unchanged"))
	}
	if rec.count("init:root") != 0 || rec.count("ready:root") != 0 {
		t.Error(test.DiffMessage(rec.list(), "no hook of the importing module", "K3: Create stops at the panic"))
	}
}

func TestHooks_OnReadyPanicReachesCaller(t *testing.T) {
	rec := &createRecorder{}
	root := hkModule(rec, "root")
	root.OnReady = func() { panic("ready boom") }

	value, panicked := createPanicValue(func() { New().Create(root) })

	if !panicked || value != "ready boom" {
		t.Error(test.DiffMessage(value, "ready boom", "K3: a panic in OnReady reaches the caller unchanged"))
	}
}

func TestHooks_OnShutdownReverseOrder(t *testing.T) {
	rec := &createRecorder{}
	app := New()
	mustCreate(t, app, hkDiamond(rec))

	app.Stop()

	got := sprint(hkFilter(rec.list(), "shutdown:"))
	if want := "[root C A B]"; got != want {
		t.Error(test.DiffMessage(got, want, "K4: OnShutdown runs in the exact reverse of the initialization order"))
	}
	app.Stop()
	if got := len(hkFilter(rec.list(), "shutdown:")); got != 4 {
		t.Error(test.DiffMessage(got, 4, "K4: OnShutdown runs once per app"))
	}
}

func TestHooks_NoShutdownWithoutCreate(t *testing.T) {
	rec := &createRecorder{}
	app := New()
	hkDiamond(rec)

	app.Stop()

	if got := hkFilter(rec.list(), "shutdown:"); len(got) != 0 {
		t.Error(test.DiffMessage(got, []string{}, "K4: an app that never ran Create runs no OnShutdown"))
	}
}

func TestHooks_NoShutdownWhenCreateDidNotFinish(t *testing.T) {
	rec := &createRecorder{}
	app := New()
	root := hkDiamond(rec)
	root.OnReady = func() { panic("ready boom") }

	_, panicked := createPanicValue(func() { app.Create(root) })
	if !panicked {
		t.Fatal(test.DiffMessage("no panic", "a panic", "K3: the panic stops Create"))
	}
	app.Stop()

	if got := hkFilter(rec.list(), "shutdown:"); len(got) != 0 {
		t.Error(test.DiffMessage(got, []string{}, "K4: an app whose Create did not finish runs no OnShutdown"))
	}
}

func TestHooks_SharedModuleHooksPerApp(t *testing.T) {
	rec := &createRecorder{}
	shared := hkModule(rec, "shared")

	appA := New()
	mustCreate(t, appA, shared)
	appB := New()
	mustCreate(t, appB, shared)
	appA.Stop()
	appB.Stop()

	for _, hook := range []string{"init:", "ready:", "shutdown:"} {
		if got := rec.count(hook + "shared"); got != 2 {
			t.Error(test.DiffMessage(got, 2, "K5: "+hook+" of a module shared by two apps runs once per app"))
		}
	}
}

type hkController struct{ common.HTTP }

func (c hkController) NewController() Controller { return c }
func (c hkController) READ_hk() string           { return "hk" }

func TestHooks_OnReadySeesRoutes(t *testing.T) {
	app := New()
	var code int
	root := ModuleBuilder().Controllers(hkController{}).Build()
	root.OnReady = func() { code = createGet(app, "/hk").Code }
	mustCreate(t, app, root)

	if code != 200 {
		t.Error(test.DiffMessage(code, 200, "K2: OnReady runs after the routes exist"))
	}
}
