package core

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/memorybroker"
	"go.owlkei.com/owlkei/internal/test"
	"go.owlkei.com/owlkei/internal/versioning"
	"go.owlkei.com/owlkei/log"
)

func TestApp_SameGuardTypeDifferentConfig(t *testing.T) {
	app := New(nil)
	app.Create(NewModuleBuilder().Controllers(roleAlphaController{}, roleBetaController{}).Build())

	cases := []struct {
		path, role string
		want       int
	}{
		{"/alpha", "admin", http.StatusOK},
		{"/alpha", "user", http.StatusForbidden},
		{"/beta", "user", http.StatusOK},
		{"/beta", "admin", http.StatusForbidden},
	}
	for _, c := range cases {
		if got := serveWithRole(app, c.path, c.role); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "bug 1, B1: "+c.path+" with role "+c.role+" must use its own controller's guard configuration"))
		}
	}
}

func TestApp_GuardBoundToNonRoutePanics(t *testing.T) {
	app := New(nil)

	var rec any
	func() {
		defer func() { rec = recover() }()
		app.Create(NewModuleBuilder().Controllers(nonRouteGuardController{}).Build())
	}()

	if rec == nil {
		t.Fatal(test.DiffMessage(nil, "a panic at Create()", "bug 4, B10: a guard bound to a non-route must not bind to every route"))
	}
	text, _ := rec.(error)
	if text == nil || !strings.Contains(text.Error(), "invalid guard:") || !strings.Contains(text.Error(), "Helper") {
		t.Error(test.DiffMessage(rec, "invalid guard: ... Helper", "B10: the panic names the layer and the handler"))
	}
}

func TestContainer_InterfaceKeysAreReflected(t *testing.T) {
	app := New(nil)
	app.Create(NewModuleBuilder().Build())

	got, ok := app.container.interfaceFor(genFieldKey(reflect.TypeFor[log.Logger]()))
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "B21: the app's logger is stored under genFieldKey(log.Logger)"))
	}
	if got != any(app.logger) {
		t.Error(test.DiffMessage(got, app.logger, "B21: the value under the logger key is the app's logger"))
	}
	if _, ok := app.container.interfaceFor(genFieldKey(reflect.TypeFor[common.Publisher]())); !ok {
		t.Error(test.DiffMessage(ok, true, "B21: the app's publisher is stored under genFieldKey(common.Publisher)"))
	}
}

func TestContainer_NoImportPathLiteralKeys(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{"owlkei/internal/common/common.", "owlkei/common/common.", "owlkei/log/log."} {
			if strings.Contains(string(raw), needle) {
				t.Error(test.DiffMessage(file, "no import path literal", "B21: "+file+" holds the literal "+needle))
			}
		}
	}
}

func TestCreate_Characterization_Routes(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		app := New(nil)
		app.Create(NewModuleBuilder().Controllers(z1Controller{}).Build())

		cases := []struct {
			method, target string
			code           int
			body           string
		}{
			{http.MethodGet, "/users", http.StatusOK, "list"},
			{http.MethodPost, "/users", http.StatusCreated, "created"},
			{http.MethodGet, "/users/7", http.StatusOK, "user:7"},
			{http.MethodDelete, "/users/7", http.StatusOK, "deleted"},
			{http.MethodGet, "/filesabc", http.StatusOK, "glob"},
			{http.MethodGet, "/nothing", http.StatusNotFound, wantNotFoundJSON},
		}
		for _, c := range cases {
			w := createRequest(app, c.method, c.target)
			if w.Code != c.code {
				t.Error(test.DiffMessage(w.Code, c.code, "Z1: status of "+c.method+" "+c.target))
			}
			if w.Body.String() != c.body {
				t.Error(test.DiffMessage(w.Body.String(), c.body, "Z1: body of "+c.method+" "+c.target))
			}
		}
	})
}

func TestCreate_Characterization_Versions(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		app := New(&AppOptions{Versioning: &versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"}})
		app.Create(NewModuleBuilder().Controllers(z1VersionedController{}).Build())

		cases := []struct {
			target, version string
			code            int
			body            string
		}{
			{"/report", "1", http.StatusOK, "report1"},
			{"/report", "2", http.StatusOK, "report2"},
			{"/report/5", "2", http.StatusOK, "byid2"},
			{"/report", "3", http.StatusNotFound, wantNotFoundJSON},
			{"/report/5", "1", http.StatusNotFound, wantNotFoundJSON},
		}
		for _, c := range cases {
			w := createRequest(app, http.MethodGet, c.target, "X-Version", c.version)
			if w.Code != c.code {
				t.Error(test.DiffMessage(w.Code, c.code, "Z1: status of GET "+c.target+" v"+c.version))
			}
			if w.Body.String() != c.body {
				t.Error(test.DiffMessage(w.Body.String(), c.body, "Z1: body of GET "+c.target+" v"+c.version))
			}
		}
	})
}

func TestCreate_Characterization_NestedPrefixes(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		child := NewModuleBuilder().Controllers(z1PrefixedController{}).Build()
		child.Prefix("v1")
		root := NewModuleBuilder().Imports(child).Controllers(z1OwnController{}).Build()
		root.Prefix("api")

		app := New(nil)
		app.Create(root)

		cases := []struct {
			target string
			code   int
			body   string
		}{
			{"/api/v1/ping", http.StatusOK, "pong"},
			{"/api/own", http.StatusOK, "own"},
			{"/ping", http.StatusNotFound, wantNotFoundJSON},
			{"/v1/ping", http.StatusNotFound, wantNotFoundJSON},
			{"/own", http.StatusNotFound, wantNotFoundJSON},
		}
		for _, c := range cases {
			w := createGet(app, c.target)
			if w.Code != c.code {
				t.Error(test.DiffMessage(w.Code, c.code, "Z1: status of GET "+c.target))
			}
			if w.Body.String() != c.body {
				t.Error(test.DiffMessage(w.Body.String(), c.body, "Z1: body of GET "+c.target))
			}
		}
	})
}

func TestCreate_Characterization_LayerOrderPerRoute(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		app := New(nil)
		app.BindGlobalMiddlewares(z1Middleware{Name: "g1"}, z1Middleware{Name: "g2"})
		app.BindGlobalGuards(z1Guard{Name: "g1"})
		app.BindGlobalInterceptors(z1Interceptor{Name: "g1"})
		app.Create(NewModuleBuilder().Controllers(z1LayeredController{}).Build())

		w := createGet(app, "/layered")
		if w.Code != http.StatusOK || w.Body.String() != "ok" {
			t.Fatal(test.DiffMessage(w.Body.String(), "ok", "Z1: the layered route answers"))
		}

		want := "[mw:g1 mw:g2 mw:c1 mw:c2 guard:g1 guard:c1 guard:c2 intercept:g1 intercept:c1 intercept:c2 handler]"
		if got := z1Trace.list(); len(got) == 0 || sprint(got) != want {
			t.Error(test.DiffMessage(sprint(got), want, "Z1: the layers of one route run in this order"))
		}
	})
}

func TestCreate_Characterization_ProviderConstructionOrder(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		shared := NewModuleBuilder().Providers(z1ProvShared{}).Build()
		modA := NewModuleBuilder().Imports(shared).Providers(z1ProvLeafA{}).Build()
		modB := NewModuleBuilder().Imports(shared).Providers(z1ProvLeafB{}).Build()
		root := NewModuleBuilder().Imports(modA, modB).Providers(z1ProvRoot{}, z1ProvRootDep{}).Build()

		New(nil).Create(root)

		want := "[shared leafA leafB root rootDep]"
		if got := sprint(z1Built.list()); got != want {
			t.Error(test.DiffMessage(got, want, "Z1: providers are constructed imports first, in import order, then the module's own"))
		}
	})
}

func TestCreate_SameShapeDifferentParamNamesConflicts(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		text, panicked := createPanicText(func() {
			New(nil).Create(NewModuleBuilder().Controllers(cfByIDController{}).Build())
		})
		if !panicked || !strings.Contains(text, "route conflict") || !strings.Contains(text, "READ_cf_BY_id") || !strings.Contains(text, "READ_cf_BY_name") {
			t.Error(test.DiffMessage(text, "route conflict: READ_cf_BY_… ... READ_cf_BY_…", "I6: two routes that differ only in parameter names conflict at bootstrap"))
		}
	})
}

func TestCreate_DifferentShapesDoNotConflict(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		text, panicked := createPanicText(func() {
			New(nil).Create(NewModuleBuilder().Controllers(cfShapesController{}).Build())
		})
		if panicked {
			t.Error(test.DiffMessage(text, "no panic", "I6: routes with a different shape, method or static segment do not conflict"))
		}
	})
}

func TestHooks_OnInitDependenciesFirst(t *testing.T) {
	rec := &createRecorder{}
	mustCreate(t, New(nil), hkDiamond(rec))

	got := sprint(hkFilter(rec.list(), "init:"))
	if want := "[B A C root]"; got != want {
		t.Error(test.DiffMessage(got, want, "K1: OnInit runs for a module's imports first, in import order"))
	}
}

func TestHooks_DiamondImportOnce(t *testing.T) {
	rec := &createRecorder{}
	mustCreate(t, New(nil), hkDiamond(rec))

	for _, name := range []string{"B", "A", "C", "root"} {
		if got := rec.count("init:" + name); got != 1 {
			t.Error(test.DiffMessage(got, 1, "K1: OnInit of "+name+" runs once per app even when imported twice"))
		}
		if got := rec.count("ready:" + name); got != 1 {
			t.Error(test.DiffMessage(got, 1, "K2: OnReady of "+name+" runs once per app even when imported twice"))
		}
	}
}

func TestHooks_OnInitBeforeOwnProviders(t *testing.T) {
	hkEvents = createRecorder{}

	child := NewModuleBuilder().Providers(hkProvider{}).Build()
	child.OnInit = func() { hkEvents.add("init:child") }
	root := NewModuleBuilder().Imports(child).Build()
	root.OnInit = func() { hkEvents.add("init:root") }
	mustCreate(t, New(nil), root)

	got := sprint(hkEvents.list())
	if want := "[init:child provider init:root]"; got != want {
		t.Error(test.DiffMessage(got, want, "K1: a module's OnInit runs, then its providers are constructed, before the next module"))
	}
}

func TestHooks_OnInitPanicStopsCreate(t *testing.T) {
	rec := &createRecorder{}
	child := hkModule(rec, "child")
	child.OnInit = func() { panic("init boom") }
	root := hkModule(rec, "root", child)

	value, panicked := createPanicValue(func() { New(nil).Create(root) })

	if !panicked || value != "init boom" {
		t.Error(test.DiffMessage(value, "init boom", "K3: a panic in OnInit reaches the caller unchanged"))
	}
	if rec.count("init:root") != 0 || rec.count("ready:root") != 0 {
		t.Error(test.DiffMessage(rec.list(), "no hook of the importing module", "K3: Create stops at the panic"))
	}
}

func TestHooks_SharedModuleHooksPerApp(t *testing.T) {
	rec := &createRecorder{}
	shared := hkModule(rec, "shared")

	appA := New(nil)
	mustCreate(t, appA, shared)
	appB := New(nil)
	mustCreate(t, appB, shared)
	appA.Stop()
	appB.Stop()

	for _, hook := range []string{"init:", "ready:", "shutdown:"} {
		if got := rec.count(hook + "shared"); got != 2 {
			t.Error(test.DiffMessage(got, 2, "K5: "+hook+" of a module shared by two apps runs once per app"))
		}
	}
}

func TestTwoApps_EachAnswersItsRoutes(t *testing.T) {
	appA := New(nil)
	mustCreate(t, appA, NewModuleBuilder().Controllers(isoAlphaController{}).Build())
	appB := New(nil)
	mustCreate(t, appB, NewModuleBuilder().Controllers(isoBetaController{}).Build())

	isoExpect(t, appA, "/alpha", http.StatusOK, "I1: the first app answers its route")
	isoExpect(t, appA, "/beta", http.StatusNotFound, "I1: the first app does not answer the other app's route")
	isoExpect(t, appB, "/beta", http.StatusOK, "I1: the second app answers its route")
	isoExpect(t, appB, "/alpha", http.StatusNotFound, "I1: the second app does not answer the first app's route")
}

func TestTwoApps_PrefixNotDoubled(t *testing.T) {
	appA := New(nil)
	mustCreate(t, appA, isoPrefixedRoot())
	appB := New(nil)
	mustCreate(t, appB, isoPrefixedRoot())
	appC := New(nil)
	mustCreate(t, appC, isoPrefixedRoot())

	for name, app := range map[string]*App{"first": appA, "second": appB, "third": appC} {
		isoExpect(t, app, "/api/v1/ping", http.StatusOK, "I1: the "+name+" app has the prefixed route")
		isoExpect(t, app, "/api/v1/api/v1/ping", http.StatusNotFound, "I1: the "+name+" app has no doubled prefix")
		isoExpect(t, app, "/v1/ping", http.StatusNotFound, "I1: the "+name+" app has no route without the root prefix")
	}
}

func TestTwoApps_ProvidersConstructedPerApp(t *testing.T) {
	isoBuilt.Store(0)

	mustCreate(t, New(nil), NewModuleBuilder().Providers(isoStateProvider{}).Build())
	mustCreate(t, New(nil), NewModuleBuilder().Providers(isoStateProvider{}).Build())

	if got := isoBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I2: NewProvider runs once per app for a provider type present in both"))
	}
}

func TestTwoApps_ProviderStateNotShared(t *testing.T) {
	appA := New(nil)
	mustCreate(t, appA, NewModuleBuilder().Providers(isoStateProvider{}).Build())
	appB := New(nil)
	mustCreate(t, appB, NewModuleBuilder().Providers(isoStateProvider{}).Build())

	a, okA := appA.Get(isoStateProvider{}).(isoStateProvider)
	b, okB := appB.Get(isoStateProvider{}).(isoStateProvider)
	if !okA || !okB {
		t.Fatal(test.DiffMessage([]bool{okA, okB}, []bool{true, true}, "I2, I9: both apps hold the provider"))
	}
	if a.State == nil || b.State == nil {
		t.Fatal(test.DiffMessage([]bool{a.State != nil, b.State != nil}, []bool{true, true}, "I2: NewProvider's state is in the instance"))
	}
	if a.State == b.State {
		t.Error(test.DiffMessage(a.State, "a different pointer", "I2: state made in NewProvider is not shared between apps"))
	}
	*a.State = 5
	if *b.State != 0 {
		t.Error(test.DiffMessage(*b.State, 0, "I2: a change through one app's state is not seen by the other"))
	}
}

func TestTwoApps_SameRootModule(t *testing.T) {
	isoControllerBuilt.Store(0)
	root := NewModuleBuilder().Controllers(isoCountedController{}).Build()

	appA := New(nil)
	mustCreate(t, appA, root)
	appB := New(nil)
	mustCreate(t, appB, root)

	isoExpect(t, appA, "/counted", http.StatusOK, "I3: the first app answers the shared root's route")
	isoExpect(t, appB, "/counted", http.StatusOK, "I3: the second app answers the shared root's route")
	if got := isoControllerBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I3: NewController runs once per controller type per app"))
	}
}

func TestTwoApps_SharedImportedModuleValue(t *testing.T) {
	shared := NewModuleBuilder().Controllers(isoAlphaController{}).Build()

	appA := New(nil)
	mustCreate(t, appA, NewModuleBuilder().Imports(shared).Controllers(isoBetaController{}).Build())
	appB := New(nil)
	mustCreate(t, appB, NewModuleBuilder().Imports(shared).Build())

	isoExpect(t, appA, "/alpha", http.StatusOK, "I3: the first app answers the shared module's route")
	isoExpect(t, appA, "/beta", http.StatusOK, "I3: the first app answers its own route")
	isoExpect(t, appB, "/alpha", http.StatusOK, "I3: the second app answers the shared module's route")
	isoExpect(t, appB, "/beta", http.StatusNotFound, "I3: the second app has only what it imports")
}

func TestTwoApps_ModuleFuncCalledOncePerApp(t *testing.T) {
	var calls atomic.Int64
	moduleFunc := func() *Module {
		calls.Add(1)
		return NewModuleBuilder().Controllers(isoAlphaController{}).Build()
	}

	appA := New(nil)
	mustCreate(t, appA, NewModuleBuilder().Imports(moduleFunc).Build())
	if got := calls.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "I3: the module func is called once by the first app"))
	}

	appB := New(nil)
	left := NewModuleBuilder().Imports(moduleFunc).Build()
	right := NewModuleBuilder().Imports(moduleFunc).Build()
	mustCreate(t, appB, NewModuleBuilder().Imports(left, right).Build())
	if got := calls.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I3: the module func is called once more by the second app, once however many modules import it"))
	}

	isoExpect(t, appA, "/alpha", http.StatusOK, "I3: the first app answers the module func's route")
	isoExpect(t, appB, "/alpha", http.StatusOK, "I3: the second app answers the module func's route")
}

func TestCreate_DoesNotWriteModule(t *testing.T) {
	child := NewModuleBuilder().Providers(isoStateProvider{}).Controllers(isoAlphaController{}).Build()
	child.Prefix("v1")
	root := NewModuleBuilder().Imports(child).Providers(isoPlainProvider{}).Controllers(isoBetaController{}).Build()
	root.Prefix("api")

	beforeRoot := createDump(reflect.ValueOf(root))
	beforeChild := createDump(reflect.ValueOf(child))

	mustCreate(t, New(nil), root)

	if afterRoot := createDump(reflect.ValueOf(root)); afterRoot != beforeRoot {
		t.Error(test.DiffMessage(afterRoot, beforeRoot, "I3: Create does not write to the root module"))
	}
	if afterChild := createDump(reflect.ValueOf(child)); afterChild != beforeChild {
		t.Error(test.DiffMessage(afterChild, beforeChild, "I3: Create does not write to an imported module"))
	}
}

func TestTwoApps_LoggerInjectedPerApp(t *testing.T) {
	recA, recB := &createLogger{}, &createLogger{}

	appA := New(&AppOptions{Logger: recA})
	mustCreate(t, appA, NewModuleBuilder().Providers(isoLogProvider{}).Build())
	appB := New(&AppOptions{Logger: recB})
	mustCreate(t, appB, NewModuleBuilder().Providers(isoLogProvider{}).Build())

	if got := len(recA.named("provider-ready")); got != 1 {
		t.Error(test.DiffMessage(got, 1, "I4: the first app's provider logs through the first app's logger"))
	}
	if got := len(recB.named("provider-ready")); got != 1 {
		t.Error(test.DiffMessage(got, 1, "I4: the second app's provider logs through the second app's logger"))
	}
}

func TestTwoApps_PublisherPerApp(t *testing.T) {
	var seenA, seenB atomic.Int64

	appA := New(nil)
	mustCreate(t, appA, NewModuleBuilder().Controllers(isoPubController{}).Build())
	appB := New(nil)
	mustCreate(t, appB, NewModuleBuilder().Controllers(isoPubController{}).Build())

	if _, err := appA.broker.Subscribe("iso.topic", func(*memorybroker.Message) { seenA.Add(1) }); err != nil {
		t.Fatal(err)
	}
	if _, err := appB.broker.Subscribe("iso.topic", func(*memorybroker.Message) { seenB.Add(1) }); err != nil {
		t.Fatal(err)
	}

	if w := createGet(appA, "/publish"); w.Body.String() != "published" {
		t.Fatal(test.DiffMessage(w.Body.String(), "published", "I4: the first app answers"))
	}
	if seenA.Load() != 1 || seenB.Load() != 0 {
		t.Error(test.DiffMessage([]int64{seenA.Load(), seenB.Load()}, []int64{1, 0}, "I4: a publish through the first app reaches only the first app's subscriber"))
	}

	if w := createGet(appB, "/publish"); w.Body.String() != "published" {
		t.Fatal(test.DiffMessage(w.Body.String(), "published", "I4: the second app answers"))
	}
	if seenA.Load() != 1 || seenB.Load() != 1 {
		t.Error(test.DiffMessage([]int64{seenA.Load(), seenB.Load()}, []int64{1, 1}, "I4: a publish through the second app reaches only the second app's subscriber"))
	}
}

func TestTwoApps_GlobalProviderNotVisibleAcross(t *testing.T) {
	appA := New(nil)
	mustCreate(t, appA, NewModuleBuilder().Providers(isoGlobalProvider{Tag: "a"}).Build())

	appB := New(nil)
	text, panicked := createPanicText(func() {
		appB.Create(NewModuleBuilder().Providers(isoNeedsGlobalProvider{}).Build())
	})

	if !panicked {
		t.Fatal(test.DiffMessage("no panic", "a panic", "I5: a dependency only another app could satisfy panics"))
	}
	if !strings.Contains(text, "dependency injection: can't resolve dependency") {
		t.Error(test.DiffMessage(text, "dependency injection: can't resolve dependency …", "I5: the panic text is the one of a single app"))
	}
}

func TestTwoApps_SameRouteNoConflict(t *testing.T) {
	appA := New(nil)
	appB := New(nil)

	text, panicked := createPanicText(func() {
		appA.Create(NewModuleBuilder().Controllers(isoDupController{}).Build())
		appB.Create(NewModuleBuilder().Controllers(isoDupController{}).Build())
	})
	if panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "I6: two apps may declare the same route"))
	}
	isoExpect(t, appA, "/dup", http.StatusOK, "I6: the first app answers the route")
	isoExpect(t, appB, "/dup", http.StatusOK, "I6: the second app answers the route")
}

func TestTwoApps_SameEventNoConflict(t *testing.T) {
	text, panicked := createPanicText(func() {
		for range 2 {
			app := New(&AppOptions{WS: &WSOptions{Path: "/ws"}})
			app.Create(NewModuleBuilder().Controllers(isoWSController{}).Build())
		}
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "I6: two apps may declare the same WebSocket event"))
	}
}

func TestCreate_RouteConflictInsideOneApp(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		text, panicked := createPanicText(func() {
			New(nil).Create(NewModuleBuilder().Controllers(isoConflictA{}, isoConflictB{}).Build())
		})
		want := "route conflict: READ_clash method is conflicted with READ_clash method"
		if !panicked || !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "I6: inside one app the route conflict panic stays"))
		}
	})
}

func TestCreate_EventConflictInsideOneApp(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		text, panicked := createPanicText(func() {
			app := New(&AppOptions{WS: &WSOptions{Path: "/ws"}})
			app.Create(NewModuleBuilder().Controllers(isoEventA{}, isoEventB{}).Build())
		})
		want := "event conflict: SUBSCRIBE_clash event is conflicted with SUBSCRIBE_clash event"
		if !panicked || !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "I6: inside one app the event conflict panic stays"))
		}
	})
}

func TestTwoApps_LayersConstructedPerApp(t *testing.T) {
	isoGuardBuilt.Store(0)

	appA := New(nil)
	mustCreate(t, appA, NewModuleBuilder().Controllers(isoGuardedController{}).Build())
	appB := New(nil)
	mustCreate(t, appB, NewModuleBuilder().Controllers(isoGuardedController{}).Build())

	if got := isoGuardBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I7: NewGuard runs once per app for one type and configuration"))
	}
}

func TestTwoApps_GlobalLayersConstructedPerApp(t *testing.T) {
	isoGuardBuilt.Store(0)

	for range 2 {
		app := New(nil)
		app.BindGlobalGuards(isoGuard{Role: "iso-global-per-app"})
		mustCreate(t, app, NewModuleBuilder().Build())
	}

	if got := isoGuardBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I7: a global guard is constructed once per app"))
	}
}

func TestCreate_EqualLayerConfigurationsShareInstance(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		isoGuardBuilt.Store(0)

		New(nil).Create(NewModuleBuilder().Controllers(isoGuardedSame{}, isoGuardedOther{}).Build())

		if got := isoGuardBuilt.Load(); got != 2 {
			t.Error(test.DiffMessage(got, int64(2), "I7: inside one app equal configurations share one instance and different ones get their own"))
		}
	})
}

func TestCreate_TwoAppsConcurrently_NoRace(t *testing.T) {
	shared := NewModuleBuilder().Providers(isoConcurrentProvider{}).Controllers(isoConcurrentController{}).Build()
	root := NewModuleBuilder().Imports(shared).Build()
	other := NewModuleBuilder().Imports(shared).Build()

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()

			module := root
			if i%2 == 1 {
				module = other
			}
			app := New(nil)
			if text, panicked := createPanicText(func() { app.Create(module) }); panicked {
				t.Error(test.DiffMessage(text, "no panic", "I8: concurrent Create of apps from shared modules"))
				return
			}
			if w := createGet(app, "/concurrent"); w.Code != http.StatusOK {
				t.Error(test.DiffMessage(w.Code, http.StatusOK, "I8: every concurrently created app answers"))
			}
		}()
	}
	wg.Wait()
}

func TestLayers_DifferentConfigurationsGetOwnInstance(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyGuard{Role: "admin"}, lyGuard{Role: "user"})
	})

	if got := lyBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I7: two configurations of one type are constructed separately"))
	}
	if got := sprint(lyRec.list()); got != "[admin user]" {
		t.Error(test.DiffMessage(got, "[admin user]", "I7: each configuration keeps its own role"))
	}
}

func TestLayers_EqualConfigurationsShareInstance(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyGuard{Role: "same"}, lyGuard{Role: "same"})
	})

	if got := lyBuilt.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "I7: equal configurations run the constructor once"))
	}
}

func TestLayers_WithoutConstructorKeepsEachValue(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyPlainGuard{Role: "a"}, lyPlainGuard{Role: "b"})
	})

	if got := sprint(lyRec.list()); got != "[plain:a plain:b]" {
		t.Error(test.DiffMessage(got, "[plain:a plain:b]", "a layer with no constructor is used as given, one per configuration"))
	}
}

func TestLayers_ConstructorNameIsPartOfKey(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyBoth{Role: "both"})
		app.BindGlobalMiddlewares(lyBoth{Role: "both"})
	})

	if got := lyBuilt.Load(); got != 101 {
		t.Error(test.DiffMessage(got, int64(101), "I7: NewGuard and NewMiddleware each run once for one value"))
	}
	if got := sprint(lyRec.list()); got != "[middleware:both guard:both]" {
		t.Error(test.DiffMessage(got, "[middleware:both guard:both]", "I7: the guard and the middleware are different instances"))
	}
}

func TestLayers_ValuesWithFuncFieldsAreNeverShared(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyFuncGuard{Role: "func", Hook: func() {}}, lyFuncGuard{Role: "func", Hook: func() {}})
	})

	if got := lyBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I7: a value holding a func is never equal to another"))
	}
}

func TestLayers_ValuesWithDifferentChannelsAreNeverShared(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyChanGuard{Role: "chan", Ch: make(chan int)}, lyChanGuard{Role: "chan", Ch: make(chan int)})
	})

	if got := lyBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I7: values with different channels are not equal"))
	}
}

func TestPath_ReplacesNamePath(t *testing.T) {
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Controllers(pathTokenController{}).Build())

	w := createRequest(app, http.MethodPost, "/oauth2/token")
	if w.Body.String() != "token" {
		t.Error(test.DiffMessage(w.Body.String(), "token", "B22: POST /oauth2/token reaches CREATE_token"))
	}
	if w := createRequest(app, http.MethodPost, "/token"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "B22: the name's own path is gone"))
	}
	psExpect(t, app, "B22", "/payment-intents", http.StatusOK)
}

func TestPath_KeepsPrefixesMethodVersion(t *testing.T) {
	app := New(&AppOptions{Versioning: &versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"}})
	mustCreate(t, app, NewModuleBuilder().Controllers(pathPrefixController{}).Build().Prefix("api"))

	if w := createRequest(app, http.MethodPost, "/api/v1/oauth2/token"); w.Body.String() != "token" {
		t.Error(test.DiffMessage(w.Body.String(), "token", "B22: module and controller prefixes apply to an exact path"))
	}
	if w := createRequest(app, http.MethodGet, "/api/v1/oauth2/token", "X-Version", "2"); w.Body.String() != "v2" {
		t.Error(test.DiffMessage(w.Body.String(), "v2", "B22: the version comes from VERSION and the method from READ"))
	}
	if w := createRequest(app, http.MethodGet, "/api/v1/oauth2/token", "X-Version", "3"); w.Code != http.StatusMethodNotAllowed {
		t.Error(test.DiffMessage(w.Code, http.StatusMethodNotAllowed, "B22: another version is not routed; only the neutral POST answers that path"))
	}
	if w := createRequest(app, http.MethodPost, "/oauth2/token"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "B22: no route without the prefixes"))
	}
}

func TestPath_NotRewrittenByStyle(t *testing.T) {
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Controllers(pathStyleController{}).Build())

	w := createGet(app, "/a_b/7")
	if w.Code != http.StatusOK || w.Body.String() != "id=7" {
		t.Error(test.DiffMessage(w.Body.String(), "id=7", "B22: Path under kebab keeps _, and {id} is a parameter"))
	}
	psExpect(t, app, "B22", "/a-b/7", http.StatusNotFound)
}

func TestPath_RootPath(t *testing.T) {
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Controllers(pathRootController{}).Build())

	if w := createGet(app, "/"); w.Body.String() != "home" {
		t.Error(test.DiffMessage(w.Body.String(), "home", "B22: Path(\"/\") is the controller root"))
	}
}

func TestPath_NameStillChecked(t *testing.T) {
	want := "invalid route: FB in READ_orgs_FB is upper case but not a route word"
	text, panicked := createPanicText(func() {
		New(nil).Create(NewModuleBuilder().Controllers(pathBadNameController{}).Build())
	})
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "B23: a handler with an exact path is still parsed"))
	}
}

func TestPath_Panics(t *testing.T) {
	cases := []struct {
		name       string
		controller Controller
		want       string
	}{
		{"no handler", pathNoHandlerController{}, `invalid route: Path("/x") needs at least one handler`},
		{"helper", pathHelperController{}, `invalid route: Path("/x") is bound to 'Helper', which is not a handler of the controller`},
		{"subscribe", pathWSController{}, `invalid route: Path("/x") is bound to 'SUBSCRIBE_chat', which is not a handler of the controller`},
		{"not a function", pathNotFuncController{}, `invalid route: Path("/x") is bound to 'string', which is not a handler of the controller`},
		{"twice", pathTwiceController{}, `invalid route: READ_ok has two exact paths: "/one" and "/two"`},
		{"query", pathQueryController{}, `invalid route: Path("/x?y=1") holds '?': a path has no query or fragment`},
		{"fragment", pathFragmentController{}, `invalid route: Path("/x#top") holds '#': a path has no query or fragment`},
	}
	for _, c := range cases {
		text, panicked := createPanicText(func() {
			New(nil).Create(NewModuleBuilder().Controllers(c.controller).Build())
		})
		if !panicked || !strings.Contains(text, c.want) {
			t.Error(test.DiffMessage(text, c.want, "B24: "+c.name))
		}
	}
}

func TestPath_TwoOperationsOnePath(t *testing.T) {
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Controllers(pathTwoOpsController{}).Build())

	if w := createGet(app, "/token"); w.Body.String() != "read" {
		t.Error(test.DiffMessage(w.Body.String(), "read", "B25: GET /token"))
	}
	if w := createRequest(app, http.MethodPost, "/token"); w.Body.String() != "create" {
		t.Error(test.DiffMessage(w.Body.String(), "create", "B25: POST /token"))
	}
}

func TestPath_SameOperationTwiceConflicts(t *testing.T) {
	text, panicked := createPanicText(func() {
		New(nil).Create(NewModuleBuilder().Controllers(pathSameOpController{}).Build())
	})
	if !panicked || !strings.Contains(text, "route conflict") {
		t.Error(test.DiffMessage(text, "route conflict: …", "B25: one operation twice on one path"))
	}
}

func TestPath_ConflictWithAnotherNameStillDetected(t *testing.T) {
	text, panicked := createPanicText(func() {
		New(nil).Create(NewModuleBuilder().Controllers(pathNameConflictController{}).Build())
	})
	if !panicked || !strings.Contains(text, "route conflict") {
		t.Error(test.DiffMessage(text, "route conflict: …", "security: two spellings of one URL"))
	}
}

func TestPath_LayersAndTargetedPrefix(t *testing.T) {
	pathGuardCalls = 0
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Controllers(pathLayerController{}).Build())

	w := createRequest(app, http.MethodPost, "/v1/oauth2/token")
	if w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "B26: the guard bound to the handler runs on the exact path"))
	}
	if pathGuardCalls != 1 {
		t.Error(test.DiffMessage(pathGuardCalls, 1, "B26: guard calls"))
	}
	psExpect(t, app, "B26", "/open", http.StatusOK)
}

func TestPath_RoutesAreSeparatePerApp(t *testing.T) {
	appA := New(nil)
	mustCreate(t, appA, NewModuleBuilder().Controllers(pathTokenController{}).Build())
	appB := New(nil)
	mustCreate(t, appB, NewModuleBuilder().Controllers(pathTokenController{}).Build())

	for name, app := range map[string]*App{"first": appA, "second": appB} {
		if w := createRequest(app, http.MethodPost, "/oauth2/token"); w.Body.String() != "token" {
			t.Error(test.DiffMessage(w.Body.String(), "token", "B22: the "+name+" app serves the exact path"))
		}
	}
}

func TestPathStyle_DefaultKebab(t *testing.T) {
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Controllers(psRootController{}).Build())

	psExpect(t, app, "B17b", "/root-page", http.StatusOK)
	psExpect(t, app, "B12", "/root_page", http.StatusNotFound)
}

func TestPathStyle_OldSpellingIs404(t *testing.T) {
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Controllers(psRootController{}).Build())

	psExpect(t, app, "B12: the route exists under its kebab spelling", "/root-page", http.StatusOK)
	lcExpectResponse(t, createGet(app, "/root_page"), http.StatusNotFound, wantNotFoundJSON, "B12: the underscore spelling is not a route under kebab")
}

func TestPathStyle_RootAsWrittenReachesImports(t *testing.T) {
	leaf := NewModuleBuilder().Controllers(psLeafController{}).Build()
	dynamic := func() *Module { return NewModuleBuilder().Controllers(psDynamicController{}).Build() }
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleAsWritten).
		Imports(leaf, dynamic).
		Controllers(psRootController{}).
		Build())

	psExpect(t, app, "B17c", "/root_page", http.StatusOK)
	psExpect(t, app, "B17c", "/leaf_page", http.StatusOK)
	psExpect(t, app, "B17c", "/dynamic_page", http.StatusOK)
	psExpect(t, app, "B17c", "/root-page", http.StatusNotFound)
	psExpect(t, app, "B17c", "/leaf-page", http.StatusNotFound)
	psExpect(t, app, "B17c", "/dynamic-page", http.StatusNotFound)
}

func TestPathStyle_ExplicitKebabWins(t *testing.T) {
	leaf := NewModuleBuilder().PathStyle(common.PathStyleKebab).Controllers(psLeafController{}).Build()
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleAsWritten).
		Imports(leaf).
		Controllers(psRootController{}).
		Build())

	psExpect(t, app, "B17d", "/leaf-page", http.StatusOK)
	psExpect(t, app, "B17d", "/leaf_page", http.StatusNotFound)
	psExpect(t, app, "B17d", "/root_page", http.StatusOK)
}

func TestPathStyle_ExplicitAsWrittenInsideKebabRoot(t *testing.T) {
	leaf := NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Controllers(psLeafController{}).Build()
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleKebab).
		Imports(leaf).
		Controllers(psRootController{}).
		Build())

	psExpect(t, app, "B17b", "/leaf_page", http.StatusOK)
	psExpect(t, app, "B17b", "/root-page", http.StatusOK)
}

func TestPathStyle_NearestSettingWins(t *testing.T) {
	leaf := NewModuleBuilder().Controllers(psLeafController{}).Build()
	mid := NewModuleBuilder().PathStyle(common.PathStyleKebab).Imports(leaf).Controllers(psMidController{}).Build()
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleAsWritten).
		Imports(mid).
		Controllers(psRootController{}).
		Build())

	psExpect(t, app, "B17d", "/root_page", http.StatusOK)
	psExpect(t, app, "B17d", "/mid-page", http.StatusOK)
	psExpect(t, app, "B17d", "/leaf-page", http.StatusOK)
	psExpect(t, app, "B17d", "/leaf_page", http.StatusNotFound)
}

func TestPathStyle_ModuleStyleAppliesToItsOwnControllersOnly(t *testing.T) {
	styled := NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Controllers(psLeafController{}).Build()
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Imports(styled).Controllers(psRootController{}).Build())

	psExpect(t, app, "B17b", "/leaf_page", http.StatusOK)
	psExpect(t, app, "B17b", "/root-page", http.StatusOK)
	psExpect(t, app, "B17b", "/root_page", http.StatusNotFound)
}

func TestPathStyle_TwoParentsFirstDiscoveredWins(t *testing.T) {
	appAB := psTwoParents("A", "B")
	psExpect(t, appAB, "B17e", "/leaf_page", http.StatusOK)
	psExpect(t, appAB, "B17e", "/leaf-page", http.StatusNotFound)

	appBA := psTwoParents("B", "A")
	psExpect(t, appBA, "B17e", "/leaf-page", http.StatusOK)
	psExpect(t, appBA, "B17e", "/leaf_page", http.StatusNotFound)
}

func TestPathStyle_TwoParentsRegisterTheRouteOnce(t *testing.T) {
	text, panicked := createPanicText(func() { psTwoParents("A", "B") })
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "B17e: a module imported twice binds its controller once"))
	}
}

func TestPathStyle_PrefixNotRewritten(t *testing.T) {
	kebab := NewModuleBuilder().Controllers(psPrefixController{}).Build().Prefix("pay_ments")
	app := New(nil)
	mustCreate(t, app, kebab)
	psExpect(t, app, "B11", "/pay_ments/my_ctl/root-page", http.StatusOK)

	written := NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Controllers(psPrefixController{}).Build().Prefix("my-api")
	app = New(nil)
	mustCreate(t, app, written)
	psExpect(t, app, "B11", "/my-api/my_ctl/root_page", http.StatusOK)
}

func TestPathStyle_OnlyResourceWords(t *testing.T) {
	for _, style := range []common.PathStyle{common.PathStyleKebab, common.PathStyleAsWritten} {
		app := New(&AppOptions{Versioning: &versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"}})
		builder := NewModuleBuilder().PathStyle(style).Controllers(psParamController{})
		mustCreate(t, app, builder.Build())

		profile, version := "/user-profile/42", "/ver-sion"
		if style == common.PathStyleAsWritten {
			profile, version = "/user_profile/42", "/ver_sion"
		}
		w := createGet(app, profile)
		if w.Code != http.StatusOK || w.Body.String() != "42" {
			t.Error(test.DiffMessage(w.Body.String(), "42", "B17f: the parameter keeps its name, GET "+profile))
		}
		w = createRequest(app, http.MethodGet, version, "X-Version", "v1_v1")
		if w.Code != http.StatusOK || w.Body.String() != "versioned" {
			t.Error(test.DiffMessage(w.Code, http.StatusOK, "B17f: the version keeps its text, GET "+version))
		}
	}
}

func TestPathStyle_DoesNotTouchEvents(t *testing.T) {
	app := New(&AppOptions{WS: &WSOptions{Path: "/ws"}})
	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Controllers(psWSController{}).Build())
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "B17f: an event is the same under either style"))
	}
	if got := app.ws.eventMatcher; got == nil {
		t.Fatal(test.DiffMessage("nil", "an event matcher", "B17f: events registered"))
	}
	if _, _, ok := app.ws.eventMatcher.Match("chat.room"); !ok {
		t.Error(test.DiffMessage(false, true, "B17f: SUBSCRIBE_chat_room is the event chat.room under as written"))
	}
}

func TestTwoApps_PathStyleNotShared(t *testing.T) {
	leaf := NewModuleBuilder().Controllers(psLeafController{}).Build()
	asWritten := NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Imports(leaf).Build()
	plain := NewModuleBuilder().Imports(leaf).Build()

	appA := New(nil)
	mustCreate(t, appA, asWritten)
	appB := New(nil)
	mustCreate(t, appB, plain)
	appC := New(nil)
	mustCreate(t, appC, asWritten)

	psExpect(t, appA, "B17g", "/leaf_page", http.StatusOK)
	psExpect(t, appB, "B17g", "/leaf-page", http.StatusOK)
	psExpect(t, appB, "B17g", "/leaf_page", http.StatusNotFound)
	psExpect(t, appC, "B17g", "/leaf_page", http.StatusOK)
	psExpect(t, appC, "B17g", "/leaf-page", http.StatusNotFound)
}

func TestCreate_UpperCaseRoutePanics(t *testing.T) {
	want := "invalid route: FB in READ_orgs_FB is upper case but not a route word; write it in lower case or camelCase"
	text, panicked := createPanicText(func() {
		New(nil).Create(NewModuleBuilder().Controllers(psUpperController{}).Build())
	})
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "B9: Create stops on READ_orgs_FB"))
	}
}

func TestCreate_NonRouteMethodIsNeverChecked(t *testing.T) {
	app := New(nil)
	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().Controllers(psHelperController{}).Build())
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "B10: Helper_FB is not a route"))
	}
	psExpect(t, app, "B10", "/ok", http.StatusOK)
}

func TestCreate_MalformedRoutePanics(t *testing.T) {
	want := "invalid route: BY in READ_users_BY has no parameter name"
	text, panicked := createPanicText(func() {
		New(nil).Create(NewModuleBuilder().Controllers(psMalformedController{}).Build())
	})
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "B13: Create stops on a malformed name"))
	}
}

func TestProviders_ConstructedOncePerApp(t *testing.T) {
	pvBuilt.Store(0)

	shared := NewModuleBuilder().Providers(pvOnceProvider{}).Build()
	left := NewModuleBuilder().Imports(shared).Providers(pvOnceProvider{}).Build()
	right := NewModuleBuilder().Imports(shared).Providers(pvOnceProvider{}).Build()
	mustCreate(t, New(nil), NewModuleBuilder().Imports(left, right).Providers(pvOnceProvider{}).Build())

	if got := pvBuilt.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "P1: a provider type is constructed once per app, however many modules list it"))
	}
}

func TestProviders_DependenciesFirst_SameModule(t *testing.T) {
	pvSeen = createRecorder{}

	mustCreate(t, New(nil), NewModuleBuilder().Providers(pvTagReader{}, pvTagProvider{}).Build())

	if got := sprint(pvSeen.list()); got != "[reader:built]" {
		t.Error(test.DiffMessage(got, "[reader:built]", "P2: a dependency declared later is constructed first and injected as built"))
	}
}

func TestProviders_DependenciesFirst_AcrossModules(t *testing.T) {
	pvChain = createRecorder{}

	leaf := NewModuleBuilder().Providers(pvChainA{}).Build()
	middle := NewModuleBuilder().Imports(leaf).Providers(pvChainB{}).Build()
	root := NewModuleBuilder().Imports(middle).Providers(pvChainC{}).Build()
	mustCreate(t, New(nil), root)

	if got := sprint(pvChain.list()); got != "[A B C]" {
		t.Error(test.DiffMessage(got, "[A B C]", "P2: dependencies in imported modules are constructed before their dependents"))
	}
}

func TestProviders_GlobalInjectedConstructed(t *testing.T) {
	pvGlobalSeen = createRecorder{}

	user := NewModuleBuilder().Providers(pvGlobalUser{}).Build()
	global := NewModuleBuilder().Providers(pvGlobalDep{}).Build()
	global.IsGlobal = true
	mustCreate(t, New(nil), NewModuleBuilder().Imports(user, global).Build())

	if got := sprint(pvGlobalSeen.list()); got != "[user:built]" {
		t.Error(test.DiffMessage(got, "[user:built]", "P3: a global provider injected into a module that does not import it is the constructed one"))
	}
}

func TestProviders_RootProviderInjectedConstructed(t *testing.T) {
	pvGlobalSeen = createRecorder{}

	user := NewModuleBuilder().Providers(pvGlobalUser{}).Build()
	mustCreate(t, New(nil), NewModuleBuilder().Imports(user).Providers(pvGlobalDep{}).Build())

	if got := sprint(pvGlobalSeen.list()); got != "[user:built]" {
		t.Error(test.DiffMessage(got, "[user:built]", "P3: a root provider injected into an imported module is the constructed one"))
	}
}

func TestProviders_GlobalAtDepthTwo(t *testing.T) {
	pvDeepSeen = createRecorder{}

	global := NewModuleBuilder().Providers(pvDeepDep{}).Build()
	global.IsGlobal = true
	holder := NewModuleBuilder().Imports(global).Build()
	user := NewModuleBuilder().Providers(pvDeepUser{}).Build()

	text, panicked := createPanicText(func() {
		New(nil).Create(NewModuleBuilder().Imports(holder, user).Build())
	})
	if panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "P4: a global module imported below the root is visible to every module"))
	}
	if got := sprint(pvDeepSeen.list()); got != "[user:built]" {
		t.Error(test.DiffMessage(got, "[user:built]", "P4: the global provider at depth two is injected, constructed"))
	}
}

func TestProviders_UnresolvedPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New(nil).Create(NewModuleBuilder().Providers(pvUnresolvedUser{}).Build())
	})

	want := "dependency injection: can't resolve dependency 'core.pvUnresolvedDep' of the provider"
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "P6: an unresolved dependency panics with today's text"))
	}
}

func TestProviders_UnexportedFieldPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New(nil).Create(NewModuleBuilder().Providers(pvHiddenUser{}, pvUnresolvedDep{}).Build())
	})

	want := "dependency injection: can't set value to unexported 'hidden' field of the core.pvHiddenUser provider"
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "P6: an unexported field panics with today's text"))
	}
}

func TestModuleFunc_SeesGlobalImportedAfterIt(t *testing.T) {
	rec := &createRecorder{}
	root := NewModuleBuilder().Imports(rvFuncModule(rec), rvGlobal()).Build()

	rvCreate(t, root, "H1: a module func sees a global module imported after it")

	if got := sprint(rec.list()); got != "[func:built]" {
		t.Error(test.DiffMessage(got, "[func:built]", "H1: the func receives the constructed global provider"))
	}
}

func TestModuleFunc_SeesGlobalImportedBeforeIt(t *testing.T) {
	rec := &createRecorder{}
	root := NewModuleBuilder().Imports(rvGlobal(), rvFuncModule(rec)).Build()

	rvCreate(t, root, "H1: a module func sees a global module imported before it")

	if got := sprint(rec.list()); got != "[func:built]" {
		t.Error(test.DiffMessage(got, "[func:built]", "H1: the func receives the constructed global provider"))
	}
}

func TestModuleFunc_SeesGlobalNestedInSiblingImports(t *testing.T) {
	rec := &createRecorder{}
	sibling := NewModuleBuilder().Imports(NewModuleBuilder().Imports(rvGlobal()).Build()).Build()
	root := NewModuleBuilder().Imports(rvFuncModule(rec), sibling).Build()

	rvCreate(t, root, "H1: a module func sees a global nested deep in a sibling's imports")

	if got := sprint(rec.list()); got != "[func:built]" {
		t.Error(test.DiffMessage(got, "[func:built]", "H1: the func receives the constructed nested global provider"))
	}
}

func TestModuleFunc_SeesGlobalNestedInSiblingImportedFirst(t *testing.T) {
	rec := &createRecorder{}
	sibling := NewModuleBuilder().Imports(NewModuleBuilder().Imports(rvGlobal()).Build()).Build()
	root := NewModuleBuilder().Imports(sibling, rvFuncModule(rec)).Build()

	rvCreate(t, root, "H1: a module func sees a global nested in an earlier sibling")

	if got := sprint(rec.list()); got != "[func:built]" {
		t.Error(test.DiffMessage(got, "[func:built]", "H1: the func receives the constructed nested global provider"))
	}
}

func TestModuleFunc_RootProviderBuiltAfterRootOnInit(t *testing.T) {
	rvOrder = createRecorder{}
	f := func(_ rvOrderDep) *Module {
		rvOrder.add("func")
		return NewModuleBuilder().Build()
	}
	root := NewModuleBuilder().Imports(f).Providers(rvOrderDep{}).Build()
	root.OnInit = rvOrder.hook("root")

	rvCreate(t, root, "M2: Create succeeds")

	if got := sprint(rvOrder.list()); got != "[root provider func]" {
		t.Error(test.DiffMessage(got, "[root provider func]", "K1/M2: a root provider a module func needs is built after the root's OnInit"))
	}
}

func TestModuleFunc_GlobalProviderBuiltAfterOwnerOnInit(t *testing.T) {
	rvOrder = createRecorder{}
	f := func(_ rvOrderDep) *Module {
		rvOrder.add("func")
		return NewModuleBuilder().Build()
	}
	global := NewModuleBuilder().Providers(rvOrderDep{}).Build()
	global.IsGlobal = true
	global.OnInit = rvOrder.hook("global")
	root := NewModuleBuilder().Imports(global, f).Build()

	rvCreate(t, root, "M2: Create succeeds")

	if got := sprint(rvOrder.list()); got != "[global provider func]" {
		t.Error(test.DiffMessage(got, "[global provider func]", "K1/M2: a global provider a module func needs is built after its module's OnInit"))
	}
}

func TestBuildFieldInjectionCallback_ResolvesFromLocalProviders(t *testing.T) {
	p := injectDepsTestProvider{}
	injectedProviders := map[string]Provider{
		genFieldKey(reflect.TypeOf(p)): p,
	}
	cb := buildFieldInjectionCallback("guarder", injectedProviders, newContainer())

	ownerType := reflect.TypeOf(injectDepsComponent{})
	ownerValue := reflect.ValueOf(injectDepsComponent{Plain: "kept"})
	newInstance := reflect.New(ownerType)

	cb(0, ownerType, ownerValue, newInstance)
	cb(1, ownerType, ownerValue, newInstance)

	got := newInstance.Elem().Interface().(injectDepsComponent)
	if got.Provider != p {
		t.Error(test.DiffMessage(got.Provider, p, "buildFieldInjectionCallback should resolve the Provider field"))
	}
	if got.Plain != "kept" {
		t.Error(test.DiffMessage(got.Plain, "kept", "buildFieldInjectionCallback should pass through non-Provider fields unchanged"))
	}
}

func TestBuildFieldInjectionCallback_UnresolvedProviderPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error(test.DiffMessage(nil, "panic", "buildFieldInjectionCallback should panic when a Provider field can't be resolved"))
		}
	}()

	cb := buildFieldInjectionCallback("guarder", map[string]Provider{}, newContainer())
	ownerType := reflect.TypeOf(injectDepsUnresolvable{})
	ownerValue := reflect.ValueOf(injectDepsUnresolvable{})
	newInstance := reflect.New(ownerType)
	cb(0, ownerType, ownerValue, newInstance)
}

func TestCreate_ProviderHoisting_DependencyConstructedBeforeDependent(t *testing.T) {
	mtConstructOrder = nil

	m := NewModuleBuilder().
		Providers(mtOrderedProviderB{}, mtOrderedProviderA{}).
		Build()

	mustCreate(t, New(nil), m)

	if len(mtConstructOrder) != 2 || mtConstructOrder[0] != "A" || mtConstructOrder[1] != "B" {
		t.Error(test.DiffMessage(mtConstructOrder, []string{"A", "B"}, "a provider's dependency must construct before the dependent, regardless of declaration order"))
	}
}

func TestCreate_ProviderConstructedOnce_AcrossDuplicateStaticImports(t *testing.T) {
	mtOnceConstructCount = 0

	child := NewModuleBuilder().Providers(mtOnceProvider{}).Build()
	parentA := NewModuleBuilder().Imports(child).Build()
	parentB := NewModuleBuilder().Imports(child).Build()
	root := NewModuleBuilder().Imports(parentA, parentB).Build()

	mustCreate(t, New(nil), root)

	if mtOnceConstructCount != 1 {
		t.Error(test.DiffMessage(mtOnceConstructCount, 1, "a provider reachable via two static-module paths must construct exactly once"))
	}
}

func TestCreate_StaticSubmoduleGlobalProvider_VisibleToOtherModules(t *testing.T) {
	global := NewModuleBuilder().Providers(mtGlobalSubProvider{}).Build()
	global.IsGlobal = true
	consumer := NewModuleBuilder().Providers(mtGlobalSubConsumer{}).Build()
	root := NewModuleBuilder().Imports(global, consumer).Build()

	app := New(nil)
	mustCreate(t, app, root)

	got, ok := app.Get(mtGlobalSubConsumer{}).(mtGlobalSubConsumer)
	if !ok {
		t.Fatal(test.DiffMessage(app.Get(mtGlobalSubConsumer{}), "a mtGlobalSubConsumer", "the consumer of an IsGlobal provider is constructed"))
	}
	if got.G.Source != "built" {
		t.Error(test.DiffMessage(got.G.Source, "built", "a provider of an IsGlobal static submodule is global and injected as constructed"))
	}
}

func TestCreate_ModuleFactory_MissingGlobalDependencyPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error(test.DiffMessage(nil, "panic", "a module factory whose argument has no global provider must panic during bootstrap"))
		}
	}()

	root := NewModuleBuilder().Imports(mtModuleFactoryNeedingProvider).Build()
	New(nil).Create(root)
}

func TestCreate_ProviderInjection_UnexportedFieldPanics(t *testing.T) {
	m := NewModuleBuilder().Providers(mtUnexportedFieldProvider{}).Build()

	text, panicked := createPanicText(func() { New(nil).Create(m) })
	if !panicked || !strings.Contains(text, "dependency injection: can't set value to unexported 'hidden' field") {
		t.Error(test.DiffMessage(text, "can't set value to unexported 'hidden' field", "a provider with an unexported field should panic during injection"))
	}
}

func TestCreate_StaticModuleControllerDedup_AcrossSharedImport(t *testing.T) {
	child := NewModuleBuilder().Controllers(mtDedupController{}).Build()
	parentA := NewModuleBuilder().Imports(child).Build()
	parentB := NewModuleBuilder().Imports(child).Build()
	root := NewModuleBuilder().Imports(parentA, parentB).Build()

	app := New(nil)
	text, panicked := createPanicText(func() { app.Create(root) })
	if panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "a controller reachable via two static-module paths is registered once, so its routes do not conflict"))
	}
	if w := createGet(app, "/mtdedup"); w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "the deduped controller answers"))
	}
}

func TestCreate_HTTPMainHandler_RegisteredWithModulePrefix(t *testing.T) {
	m := NewModuleBuilder().Controllers(mtPrefixedController{}).Build()
	m.Prefix("v1")

	app := New(nil)
	mustCreate(t, app, m)

	if w := createGet(app, "/v1/mtprefixtarget"); w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "the route carries the module prefix and the resource name"))
	}
	if w := createGet(app, "/mtprefixtarget"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "the route is not served without the module prefix"))
	}
}

func TestCreate_HTTPGuardInjection_PriorityChain(t *testing.T) {
	mtPriorityGuardSeen = mtPriorityGuard{}

	root, _ := mtPriorityRoot(mtGuardPriorityController{})
	app := New(nil)
	mustCreate(t, app, root)

	if w := createGet(app, "/mtguardpriority"); w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "the guarded route answers"))
	}
	mtAssertPriorityChain(t, mtPriorityGuardSeen, app, "HTTP guard")
}

func TestCreate_HTTPGuardInjection_UnexportedFieldPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New(nil).Create(NewModuleBuilder().Controllers(mtGuardUnexportedController{}).Build())
	})
	if !panicked {
		t.Error(test.DiffMessage(text, "panic", "guard with an unexported field should panic during injection"))
	}
}

func TestCreate_HTTPGuardInjection_UnresolvedDependencyPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New(nil).Create(NewModuleBuilder().Controllers(mtGuardUnresolvedController{}).Build())
	})
	if !panicked {
		t.Error(test.DiffMessage(text, "panic", "guard field with an unresolved provider dependency should panic"))
	}
}

func TestCreate_HTTPMiddlewareInjection_LocalProviderRegistered(t *testing.T) {
	mtSimpleMiddlewareSeen = mtSimpleMiddleware{}

	m := NewModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtMiddlewareController{}).
		Build()
	app := New(nil)
	mustCreate(t, app, m)

	w := createGet(app, "/mtmiddleware")
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "HTTP middleware should call next()"))
	}
	if mtSimpleMiddlewareSeen.P.Source != "local" {
		t.Error(test.DiffMessage(mtSimpleMiddlewareSeen.P.Source, "local", "HTTP middleware field should resolve from local module providers"))
	}
}

func TestCreate_HTTPInterceptorInjection_LocalProviderRegistered(t *testing.T) {
	mtSimpleInterceptorSeen = mtSimpleInterceptor{}

	m := NewModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtInterceptorController{}).
		Build()
	app := New(nil)
	mustCreate(t, app, m)
	createGet(app, "/mtinterceptor")

	if mtSimpleInterceptorSeen.P.Source != "local" {
		t.Error(test.DiffMessage(mtSimpleInterceptorSeen.P.Source, "local", "HTTP interceptor field should resolve from local module providers"))
	}
}

func TestCreate_HTTPExceptionFilter_SingleController_Registered(t *testing.T) {
	mtExFilterSeen = createRecorder{}

	m := NewModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtExFilterSingleController{}).
		Build()
	app := New(nil)
	mustCreate(t, app, m)
	createGet(app, "/mtexfiltersingle")

	if got := sprint(mtExFilterSeen.list()); got != "[local]" {
		t.Error(test.DiffMessage(got, "[local]", "exceptionFilter field should resolve from local module providers and the filter should run"))
	}
}

func TestCreate_HTTPExceptionFilter_TwoControllers_CorrectFieldIndex(t *testing.T) {
	mtExFilterSeen = createRecorder{}

	m := NewModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtExFilterMultiController0{}, mtExFilterMultiController1{}).
		Build()
	app := New(nil)
	mustCreate(t, app, m)
	createGet(app, "/mtexfilterbugc0")
	createGet(app, "/mtexfilterbugc1")

	if got := sprint(mtExFilterSeen.list()); got != "[local local]" {
		t.Error(test.DiffMessage(got, "[local local]", "each controller's filter resolves its provider field and runs"))
	}
}

func TestCreate_WSMainHandler_Registered(t *testing.T) {
	app := mtWSApp(t, NewModuleBuilder().Controllers(mtWSMainController{}).Build())

	if reply := mtWSSubscribe(t, app, "mtwsmainhandler"); reply.Type != TypeAck {
		t.Error(test.DiffMessage(reply.Type, TypeAck, "the event of the WS main handler can be subscribed"))
	}
	if reply := mtWSSubscribe(t, app, "mtwsunknown"); reply.Type != TypeError {
		t.Error(test.DiffMessage(reply.Type, TypeError, "an event nothing handles is refused"))
	}
}

func TestCreate_WSGuardInjection_PriorityChain(t *testing.T) {
	mtPriorityGuardWSSeen = mtPriorityGuardWS{}

	root, _ := mtPriorityRoot(mtWSGuardPriorityController{})
	app := mtWSApp(t, root)
	mtWSSubscribe(t, app, "mtwsguardpriority")

	mtAssertPriorityChain(t, mtPriorityGuard(mtPriorityGuardWSSeen), app, "WS guard")
}

func TestCreate_WSGuardInjection_UnexportedFieldPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		app := New(&AppOptions{WS: &WSOptions{Path: "/ws"}})
		app.Create(NewModuleBuilder().Controllers(mtWSGuardUnexportedController{}).Build())
	})
	if !panicked {
		t.Error(test.DiffMessage(text, "panic", "WS guard with an unexported field should panic during injection"))
	}
}

func TestCreate_WSGuardInjection_UnresolvedDependencyPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		app := New(&AppOptions{WS: &WSOptions{Path: "/ws"}})
		app.Create(NewModuleBuilder().Controllers(mtWSGuardUnresolvedController{}).Build())
	})
	if !panicked {
		t.Error(test.DiffMessage(text, "panic", "WS guard field with an unresolved provider dependency should panic"))
	}
}

func TestCreate_WSInterceptorInjection_LocalProviderRegistered(t *testing.T) {
	mtSimpleInterceptorWSSeen = mtSimpleInterceptorWS{}

	app := mtWSApp(t, NewModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtWSInterceptorController{}).
		Build())
	mtWSSubscribe(t, app, "mtwsinterceptor")

	if mtSimpleInterceptorWSSeen.P.Source != "local" {
		t.Error(test.DiffMessage(mtSimpleInterceptorWSSeen.P.Source, "local", "WS interceptor field should resolve from local module providers"))
	}
}

func TestCreate_WSExceptionFilter_TwoControllers_CorrectFieldIndex(t *testing.T) {
	mtWSExFilterSeen = createRecorder{}

	app := mtWSApp(t, NewModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtWSExFilterController0{}, mtWSExFilterController1{}).
		Build())
	mtWSSend(t, app, "mtwsexfilterc0")
	mtWSSend(t, app, "mtwsexfilterc1")

	if got := sprint(mtWSExFilterSeen.list()); got != "[local local]" {
		t.Error(test.DiffMessage(got, "[local local]", "each controller's WS filter resolves its provider field and runs"))
	}
}

func TestCreate_ConcurrentInvocation_NoDataRace(t *testing.T) {
	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()

			m := NewModuleBuilder().
				Providers(mtConcurrentProvider{}).
				Controllers(mtConcurrentController{}).
				Build()
			if text, panicked := createPanicText(func() { New(nil).Create(m) }); panicked {
				t.Error(test.DiffMessage(text, "no panic", "concurrent Create"))
			}
		}()
	}
	wg.Wait()
}

func TestCreate_Hooks_ReachNestedModuleAndFactoryImportsAtAnyDepth(t *testing.T) {
	rec := &createRecorder{}

	grandchild := hkModule(rec, "grandchild")
	dynamicChild := func() *Module { return hkModule(rec, "dynamic") }
	child := hkModule(rec, "child", grandchild, dynamicChild)
	root := hkModule(rec, "root", child)

	mustCreate(t, New(nil), root)

	for _, name := range []string{"root", "child", "grandchild", "dynamic"} {
		if got := rec.count("ready:" + name); got != 1 {
			t.Error(test.DiffMessage(got, 1, name+" should be initialized and reported ready exactly once"))
		}
	}
}

func TestCreate_Hooks_SharedImportRunsOnce(t *testing.T) {
	rec := &createRecorder{}

	shared := hkModule(rec, "shared")
	childA := hkModule(rec, "a", shared)
	childB := hkModule(rec, "b", shared)
	root := hkModule(rec, "root", childA, childB)

	mustCreate(t, New(nil), root)

	if got := rec.count("ready:shared"); got != 1 {
		t.Error(test.DiffMessage(got, 1, "a module imported by two different parents should only be initialized once"))
	}
}

func TestCreate_AllNotLastPanics(t *testing.T) {
	app := New(&AppOptions{WS: &WSOptions{Path: "/ws"}})
	want := "invalid route: ALL in SUBSCRIBE_chat_ALL_x must be the last word"
	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().Controllers(wsAllNotLastController{}).Build())
	})
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "B19: Create stops on ALL before the last word"))
	}
}
