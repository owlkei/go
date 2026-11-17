package core

import (
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/memorybroker"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/log"
)

type isoAlphaController struct{ common.HTTP }

func (c isoAlphaController) NewController() Controller { return c }
func (c isoAlphaController) READ_alpha() string        { return "alpha" }

type isoBetaController struct{ common.HTTP }

func (c isoBetaController) NewController() Controller { return c }
func (c isoBetaController) READ_beta() string         { return "beta" }

func isoExpect(t *testing.T, app *App, target string, code int, rule string) {
	t.Helper()
	if w := createGet(app, target); w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, rule+": status of GET "+target))
	}
}

func TestTwoApps_EachAnswersItsRoutes(t *testing.T) {
	appA := New()
	mustCreate(t, appA, NewModuleBuilder().Controllers(isoAlphaController{}).Build())
	appB := New()
	mustCreate(t, appB, NewModuleBuilder().Controllers(isoBetaController{}).Build())

	isoExpect(t, appA, "/alpha", http.StatusOK, "I1: the first app answers its route")
	isoExpect(t, appA, "/beta", http.StatusNotFound, "I1: the first app does not answer the other app's route")
	isoExpect(t, appB, "/beta", http.StatusOK, "I1: the second app answers its route")
	isoExpect(t, appB, "/alpha", http.StatusNotFound, "I1: the second app does not answer the first app's route")
}

type isoPingController struct{ common.HTTP }

func (c isoPingController) NewController() Controller { return c }
func (c isoPingController) READ_ping() string         { return "pong" }

func isoPrefixedRoot() *Module {
	child := NewModuleBuilder().Controllers(isoPingController{}).Build()
	child.Prefix("v1")
	root := NewModuleBuilder().Imports(child).Build()
	root.Prefix("api")
	return root
}

func TestTwoApps_PrefixNotDoubled(t *testing.T) {
	appA := New()
	mustCreate(t, appA, isoPrefixedRoot())
	appB := New()
	mustCreate(t, appB, isoPrefixedRoot())
	appC := New()
	mustCreate(t, appC, isoPrefixedRoot())

	for name, app := range map[string]*App{"first": appA, "second": appB, "third": appC} {
		isoExpect(t, app, "/api/v1/ping", http.StatusOK, "I1: the "+name+" app has the prefixed route")
		isoExpect(t, app, "/api/v1/api/v1/ping", http.StatusNotFound, "I1: the "+name+" app has no doubled prefix")
		isoExpect(t, app, "/v1/ping", http.StatusNotFound, "I1: the "+name+" app has no route without the root prefix")
	}
}

var isoBuilt atomic.Int64

type isoStateProvider struct{ State *int }

func (p isoStateProvider) NewProvider() Provider {
	isoBuilt.Add(1)
	p.State = new(int)
	return p
}

func TestTwoApps_ProvidersConstructedPerApp(t *testing.T) {
	isoBuilt.Store(0)

	mustCreate(t, New(), NewModuleBuilder().Providers(isoStateProvider{}).Build())
	mustCreate(t, New(), NewModuleBuilder().Providers(isoStateProvider{}).Build())

	if got := isoBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I2: NewProvider runs once per app for a provider type present in both"))
	}
}

func TestTwoApps_ProviderStateNotShared(t *testing.T) {
	appA := New()
	mustCreate(t, appA, NewModuleBuilder().Providers(isoStateProvider{}).Build())
	appB := New()
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

var isoControllerBuilt atomic.Int64

type isoCountedController struct{ common.HTTP }

func (c isoCountedController) NewController() Controller {
	isoControllerBuilt.Add(1)
	return c
}
func (c isoCountedController) READ_counted() string { return "counted" }

func TestTwoApps_SameRootModule(t *testing.T) {
	isoControllerBuilt.Store(0)
	root := NewModuleBuilder().Controllers(isoCountedController{}).Build()

	appA := New()
	mustCreate(t, appA, root)
	appB := New()
	mustCreate(t, appB, root)

	isoExpect(t, appA, "/counted", http.StatusOK, "I3: the first app answers the shared root's route")
	isoExpect(t, appB, "/counted", http.StatusOK, "I3: the second app answers the shared root's route")
	if got := isoControllerBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I3: NewController runs once per controller type per app"))
	}
}

func TestTwoApps_SharedImportedModuleValue(t *testing.T) {
	shared := NewModuleBuilder().Controllers(isoAlphaController{}).Build()

	appA := New()
	mustCreate(t, appA, NewModuleBuilder().Imports(shared).Controllers(isoBetaController{}).Build())
	appB := New()
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

	appA := New()
	mustCreate(t, appA, NewModuleBuilder().Imports(moduleFunc).Build())
	if got := calls.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "I3: the module func is called once by the first app"))
	}

	appB := New()
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

	mustCreate(t, New(), root)

	if afterRoot := createDump(reflect.ValueOf(root)); afterRoot != beforeRoot {
		t.Error(test.DiffMessage(afterRoot, beforeRoot, "I3: Create does not write to the root module"))
	}
	if afterChild := createDump(reflect.ValueOf(child)); afterChild != beforeChild {
		t.Error(test.DiffMessage(afterChild, beforeChild, "I3: Create does not write to an imported module"))
	}
}

type isoPlainProvider struct{}

func (p isoPlainProvider) NewProvider() Provider { return p }

type isoLogProvider struct{ Log log.Logger }

func (p isoLogProvider) NewProvider() Provider {
	p.Log.Info("provider-ready")
	return p
}

func TestTwoApps_LoggerInjectedPerApp(t *testing.T) {
	recA, recB := &createLogger{}, &createLogger{}

	appA := New()
	appA.SetLogger(recA)
	mustCreate(t, appA, NewModuleBuilder().Providers(isoLogProvider{}).Build())
	appB := New()
	appB.SetLogger(recB)
	mustCreate(t, appB, NewModuleBuilder().Providers(isoLogProvider{}).Build())

	if got := len(recA.named("provider-ready")); got != 1 {
		t.Error(test.DiffMessage(got, 1, "I4: the first app's provider logs through the first app's logger"))
	}
	if got := len(recB.named("provider-ready")); got != 1 {
		t.Error(test.DiffMessage(got, 1, "I4: the second app's provider logs through the second app's logger"))
	}
}

type isoPubController struct{ common.HTTP }

func (c isoPubController) NewController() Controller { return c }
func (c isoPubController) READ_publish(pub common.Publisher) string {
	_ = pub.Publish("iso.topic")
	return "published"
}

func TestTwoApps_PublisherPerApp(t *testing.T) {
	var seenA, seenB atomic.Int64

	appA := New()
	mustCreate(t, appA, NewModuleBuilder().Controllers(isoPubController{}).Build())
	appB := New()
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

type isoGlobalProvider struct{ Tag string }

func (p isoGlobalProvider) NewProvider() Provider { return p }

type isoNeedsGlobalProvider struct{ G isoGlobalProvider }

func (p isoNeedsGlobalProvider) NewProvider() Provider { return p }

func TestTwoApps_GlobalProviderNotVisibleAcross(t *testing.T) {
	appA := New()
	mustCreate(t, appA, NewModuleBuilder().Providers(isoGlobalProvider{Tag: "a"}).Build())

	appB := New()
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

type isoDupController struct{ common.HTTP }

func (c isoDupController) NewController() Controller { return c }
func (c isoDupController) READ_dup() string          { return "dup" }

func TestTwoApps_SameRouteNoConflict(t *testing.T) {
	appA := New()
	appB := New()

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

type isoWSController struct{ common.WS }

func (c isoWSController) NewController() Controller  { return c }
func (c isoWSController) SUBSCRIBE_dupevent() string { return "ok" }

func TestTwoApps_SameEventNoConflict(t *testing.T) {
	text, panicked := createPanicText(func() {
		for range 2 {
			app := New()
			app.EnableWS(&WSOptions{Path: "/ws"})
			app.Create(NewModuleBuilder().Controllers(isoWSController{}).Build())
		}
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "I6: two apps may declare the same WebSocket event"))
	}
}

type isoConflictA struct{ common.HTTP }

func (c isoConflictA) NewController() Controller { return c }
func (c isoConflictA) READ_clash() string        { return "a" }

type isoConflictB struct{ common.HTTP }

func (c isoConflictB) NewController() Controller { return c }
func (c isoConflictB) READ_clash() string        { return "b" }

func TestCreate_RouteConflictInsideOneApp(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		text, panicked := createPanicText(func() {
			New().Create(NewModuleBuilder().Controllers(isoConflictA{}, isoConflictB{}).Build())
		})
		want := "route conflict: READ_clash method is conflicted with READ_clash method"
		if !panicked || !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "I6: inside one app the route conflict panic stays"))
		}
	})
}

type isoEventA struct{ common.WS }

func (c isoEventA) NewController() Controller { return c }
func (c isoEventA) SUBSCRIBE_clash() string   { return "a" }

type isoEventB struct{ common.WS }

func (c isoEventB) NewController() Controller { return c }
func (c isoEventB) SUBSCRIBE_clash() string   { return "b" }

func TestCreate_EventConflictInsideOneApp(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		text, panicked := createPanicText(func() {
			app := New()
			app.EnableWS(&WSOptions{Path: "/ws"})
			app.Create(NewModuleBuilder().Controllers(isoEventA{}, isoEventB{}).Build())
		})
		want := "event conflict: SUBSCRIBE_clash event is conflicted with SUBSCRIBE_clash event"
		if !panicked || !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "I6: inside one app the event conflict panic stays"))
		}
	})
}

var isoGuardBuilt atomic.Int64

type isoGuard struct{ Role string }

func (g isoGuard) NewGuard() any {
	isoGuardBuilt.Add(1)
	return isoGuard{Role: g.Role}
}

func (g isoGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type isoGuardedController struct {
	common.HTTP
	common.Guard
}

func (c isoGuardedController) NewController() Controller {
	c.BindGuard(isoGuard{Role: "iso-per-app"})
	return c
}
func (c isoGuardedController) READ_guarded() string { return "guarded" }

func TestTwoApps_LayersConstructedPerApp(t *testing.T) {
	isoGuardBuilt.Store(0)

	appA := New()
	mustCreate(t, appA, NewModuleBuilder().Controllers(isoGuardedController{}).Build())
	appB := New()
	mustCreate(t, appB, NewModuleBuilder().Controllers(isoGuardedController{}).Build())

	if got := isoGuardBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I7: NewGuard runs once per app for one type and configuration"))
	}
}

func TestTwoApps_GlobalLayersConstructedPerApp(t *testing.T) {
	isoGuardBuilt.Store(0)

	for range 2 {
		app := New()
		app.BindGlobalGuards(isoGuard{Role: "iso-global-per-app"})
		mustCreate(t, app, NewModuleBuilder().Build())
	}

	if got := isoGuardBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I7: a global guard is constructed once per app"))
	}
}

type isoGuardedOther struct {
	common.HTTP
	common.Guard
}

func (c isoGuardedOther) NewController() Controller {
	c.BindGuard(isoGuard{Role: "iso-equal"})
	c.BindGuard(isoGuard{Role: "iso-different"})
	return c
}
func (c isoGuardedOther) READ_other() string { return "other" }

type isoGuardedSame struct {
	common.HTTP
	common.Guard
}

func (c isoGuardedSame) NewController() Controller {
	c.BindGuard(isoGuard{Role: "iso-equal"})
	return c
}
func (c isoGuardedSame) READ_same() string { return "same" }

func TestCreate_EqualLayerConfigurationsShareInstance(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		isoGuardBuilt.Store(0)

		New().Create(NewModuleBuilder().Controllers(isoGuardedSame{}, isoGuardedOther{}).Build())

		if got := isoGuardBuilt.Load(); got != 2 {
			t.Error(test.DiffMessage(got, int64(2), "I7: inside one app equal configurations share one instance and different ones get their own"))
		}
	})
}

type isoConcurrentProvider struct{}

func (p isoConcurrentProvider) NewProvider() Provider { return p }

type isoConcurrentController struct{ common.HTTP }

func (c isoConcurrentController) NewController() Controller { return c }
func (c isoConcurrentController) READ_concurrent() string   { return "ok" }

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
			app := New()
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

func TestGet_BeforeCreateIsNil(t *testing.T) {
	if got := New().Get(isoPlainProvider{}); got != nil {
		t.Error(test.DiffMessage(got, nil, "I9: Get before Create is nil"))
	}
}

func TestGet_UnknownTypeIsNil(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().Providers(isoPlainProvider{}).Build())

	if got := app.Get(isoGlobalProvider{}); got != nil {
		t.Error(test.DiffMessage(got, nil, "I9: Get of a type the app does not have is nil"))
	}
}

func TestGet_ReturnsThisAppsInstance(t *testing.T) {
	appA := New()
	mustCreate(t, appA, NewModuleBuilder().Providers(isoGlobalProvider{Tag: "a"}).Build())
	appB := New()
	mustCreate(t, appB, NewModuleBuilder().Providers(isoGlobalProvider{Tag: "b"}).Build())

	a, _ := appA.Get(isoGlobalProvider{}).(isoGlobalProvider)
	b, _ := appB.Get(isoGlobalProvider{}).(isoGlobalProvider)
	if a.Tag != "a" || b.Tag != "b" {
		t.Error(test.DiffMessage([]string{a.Tag, b.Tag}, []string{"a", "b"}, "I9: Get returns the instance this app constructed"))
	}
}

func TestGet_ConcurrentAfterCreate_NoRace(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().Providers(isoGlobalProvider{Tag: "x"}).Controllers(isoConcurrentController{}).Build())

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			for range 50 {
				if got, _ := app.Get(isoGlobalProvider{}).(isoGlobalProvider); got.Tag != "x" {
					t.Error(test.DiffMessage(got.Tag, "x", "I9: concurrent Get after Create"))
					return
				}
				if w := createGet(app, "/concurrent"); w.Code != http.StatusOK {
					t.Error(test.DiffMessage(w.Code, http.StatusOK, "I9: concurrent requests after Create"))
					return
				}
			}
		}()
	}
	wg.Wait()
}
