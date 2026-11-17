package core

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/test"
	"golang.org/x/net/websocket"
)

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

var mtConstructOrder []string

type mtOrderedProviderA struct{}

func (p mtOrderedProviderA) NewProvider() Provider {
	mtConstructOrder = append(mtConstructOrder, "A")
	return p
}

type mtOrderedProviderB struct{ A mtOrderedProviderA }

func (p mtOrderedProviderB) NewProvider() Provider {
	mtConstructOrder = append(mtConstructOrder, "B")
	return p
}

func TestCreate_ProviderHoisting_DependencyConstructedBeforeDependent(t *testing.T) {
	mtConstructOrder = nil

	m := ModuleBuilder().
		Providers(mtOrderedProviderB{}, mtOrderedProviderA{}).
		Build()

	mustCreate(t, New(), m)

	if len(mtConstructOrder) != 2 || mtConstructOrder[0] != "A" || mtConstructOrder[1] != "B" {
		t.Error(test.DiffMessage(mtConstructOrder, []string{"A", "B"}, "a provider's dependency must construct before the dependent, regardless of declaration order"))
	}
}

var mtOnceConstructCount int

type mtOnceProvider struct{}

func (p mtOnceProvider) NewProvider() Provider {
	mtOnceConstructCount++
	return p
}

func TestCreate_ProviderConstructedOnce_AcrossDuplicateStaticImports(t *testing.T) {
	mtOnceConstructCount = 0

	child := ModuleBuilder().Providers(mtOnceProvider{}).Build()
	parentA := ModuleBuilder().Imports(child).Build()
	parentB := ModuleBuilder().Imports(child).Build()
	root := ModuleBuilder().Imports(parentA, parentB).Build()

	mustCreate(t, New(), root)

	if mtOnceConstructCount != 1 {
		t.Error(test.DiffMessage(mtOnceConstructCount, 1, "a provider reachable via two static-module paths must construct exactly once"))
	}
}

type mtGlobalSubProvider struct{ Source string }

func (p mtGlobalSubProvider) NewProvider() Provider { p.Source = "built"; return p }

type mtGlobalSubConsumer struct{ G mtGlobalSubProvider }

func (p mtGlobalSubConsumer) NewProvider() Provider { return p }

func TestCreate_StaticSubmoduleGlobalProvider_VisibleToOtherModules(t *testing.T) {
	global := ModuleBuilder().Providers(mtGlobalSubProvider{}).Build()
	global.IsGlobal = true
	consumer := ModuleBuilder().Providers(mtGlobalSubConsumer{}).Build()
	root := ModuleBuilder().Imports(global, consumer).Build()

	app := New()
	mustCreate(t, app, root)

	got, ok := app.Get(mtGlobalSubConsumer{}).(mtGlobalSubConsumer)
	if !ok {
		t.Fatal(test.DiffMessage(app.Get(mtGlobalSubConsumer{}), "a mtGlobalSubConsumer", "the consumer of an IsGlobal provider is constructed"))
	}
	if got.G.Source != "built" {
		t.Error(test.DiffMessage(got.G.Source, "built", "a provider of an IsGlobal static submodule is global and injected as constructed"))
	}
}

type mtDynMissingProvider struct{}

func (p mtDynMissingProvider) NewProvider() Provider { return p }

func mtDynamicModuleNeedingProvider(_ mtDynMissingProvider) *Module {
	return ModuleBuilder().Build()
}

func TestCreate_DynamicModule_MissingGlobalDependencyPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error(test.DiffMessage(nil, "panic", "a dynamic module factory whose argument has no global provider must panic during bootstrap"))
		}
	}()

	root := ModuleBuilder().Imports(mtDynamicModuleNeedingProvider).Build()
	New().Create(root)
}

type mtLocalProvider struct{ Source string }

func (p mtLocalProvider) NewProvider() Provider { p.Source = "local"; return p }

type mtUnexportedFieldProvider struct {
	hidden mtLocalProvider //nolint:unused
}

func (p mtUnexportedFieldProvider) NewProvider() Provider { return p }

func TestCreate_ProviderInjection_UnexportedFieldPanics(t *testing.T) {
	m := ModuleBuilder().Providers(mtUnexportedFieldProvider{}).Build()

	text, panicked := createPanicText(func() { New().Create(m) })
	if !panicked || !strings.Contains(text, "dependency injection: can't set value to unexported 'hidden' field") {
		t.Error(test.DiffMessage(text, "can't set value to unexported 'hidden' field", "a provider with an unexported field should panic during injection"))
	}
}

type mtDedupController struct{ common.HTTP }

func (c mtDedupController) NewController() Controller { return c }
func (c mtDedupController) READ_mtdedup() string      { return "ok" }

func TestCreate_StaticModuleControllerDedup_AcrossSharedImport(t *testing.T) {
	child := ModuleBuilder().Controllers(mtDedupController{}).Build()
	parentA := ModuleBuilder().Imports(child).Build()
	parentB := ModuleBuilder().Imports(child).Build()
	root := ModuleBuilder().Imports(parentA, parentB).Build()

	app := New()
	text, panicked := createPanicText(func() { app.Create(root) })
	if panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "a controller reachable via two static-module paths is registered once, so its routes do not conflict"))
	}
	if w := createGet(app, "/mtdedup"); w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "the deduped controller answers"))
	}
}

type mtGlobalOnlyProvider struct{ Source string }

func (p mtGlobalOnlyProvider) NewProvider() Provider { return p }

type mtPassthroughValue struct{ Tag string }

type mtUnresolvedProvider struct{}

func (p mtUnresolvedProvider) NewProvider() Provider { return p }

type mtPriorityGuard struct {
	Local       mtLocalProvider
	GlobalOnly  mtGlobalOnlyProvider
	Log         common.Logger
	Pub         common.Publisher
	Passthrough mtPassthroughValue
}

var mtPriorityGuardSeen mtPriorityGuard

func (g mtPriorityGuard) CanActivate(_ *ctx.HTTPContext) bool {
	mtPriorityGuardSeen = g
	return true
}

type mtUnexportedFieldGuard struct {
	hidden mtLocalProvider //nolint:unused
}

func (g mtUnexportedFieldGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type mtUnresolvedFieldGuard struct{ Missing mtUnresolvedProvider }

func (g mtUnresolvedFieldGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type mtSimpleMiddleware struct{ P mtLocalProvider }

var mtSimpleMiddlewareSeen mtSimpleMiddleware

func (mw mtSimpleMiddleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	mtSimpleMiddlewareSeen = mw
	next()
}

type mtSimpleInterceptor struct{ P mtLocalProvider }

var mtSimpleInterceptorSeen mtSimpleInterceptor

func (ic mtSimpleInterceptor) Intercept(_ *ctx.HTTPContext, _ *aggregation.Aggregation) any {
	mtSimpleInterceptorSeen = ic
	return nil
}

var mtExFilterSeen createRecorder

type mtExFilterOneField struct{ P mtLocalProvider }

func (e mtExFilterOneField) Catch(_ *ctx.HTTPContext, _ *exception.Exception) {
	mtExFilterSeen.add(e.P.Source)
}

func mtPriorityRoot(controller Controller) (*Module, *Module) {
	global := ModuleBuilder().Providers(mtGlobalOnlyProvider{Source: "global"}).Build()
	global.IsGlobal = true
	consumer := ModuleBuilder().Providers(mtLocalProvider{}).Controllers(controller).Build()
	return ModuleBuilder().Imports(global, consumer).Build(), global
}

func mtAssertPriorityChain(t *testing.T, seen mtPriorityGuard, app *App, label string) {
	t.Helper()
	if seen.Local.Source != "local" {
		t.Error(test.DiffMessage(seen.Local.Source, "local", label+": a field with a module provider is injected as constructed"))
	}
	if seen.GlobalOnly.Source != "global" {
		t.Error(test.DiffMessage(seen.GlobalOnly.Source, "global", label+": a field with no local provider falls back to the global providers"))
	}
	if seen.Log != app.Logger() {
		t.Error(test.DiffMessage(seen.Log, app.Logger(), label+": a common.Logger field falls back to the app's logger"))
	}
	if seen.Pub == nil {
		t.Error(test.DiffMessage(nil, "the app's publisher", label+": a common.Publisher field falls back to the app's publisher"))
	}
	if seen.Passthrough.Tag != "original" {
		t.Error(test.DiffMessage(seen.Passthrough.Tag, "original", label+": a non-Provider field passes through the bound instance's original value"))
	}
}

type mtPrefixedController struct{ common.HTTP }

func (c mtPrefixedController) NewController() Controller   { return c }
func (c mtPrefixedController) READ_mtprefixtarget() string { return "ok" }

func TestCreate_HTTPMainHandler_RegisteredWithModulePrefix(t *testing.T) {
	m := ModuleBuilder().Controllers(mtPrefixedController{}).Build()
	m.Prefix("v1")

	app := New()
	mustCreate(t, app, m)

	if w := createGet(app, "/v1/mtprefixtarget"); w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "the route carries the module prefix and the resource name"))
	}
	if w := createGet(app, "/mtprefixtarget"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "the route is not served without the module prefix"))
	}
}

type mtGuardPriorityController struct {
	common.HTTP
	common.Guard
}

func (c mtGuardPriorityController) NewController() Controller {
	c.BindGuard(mtPriorityGuard{Passthrough: mtPassthroughValue{Tag: "original"}})
	return c
}
func (c mtGuardPriorityController) READ_mtguardpriority() string { return "ok" }

func TestCreate_HTTPGuardInjection_PriorityChain(t *testing.T) {
	mtPriorityGuardSeen = mtPriorityGuard{}

	root, _ := mtPriorityRoot(mtGuardPriorityController{})
	app := New()
	mustCreate(t, app, root)

	if w := createGet(app, "/mtguardpriority"); w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "the guarded route answers"))
	}
	mtAssertPriorityChain(t, mtPriorityGuardSeen, app, "HTTP guard")
}

type mtGuardUnexportedController struct {
	common.HTTP
	common.Guard
}

func (c mtGuardUnexportedController) NewController() Controller {
	c.BindGuard(mtUnexportedFieldGuard{})
	return c
}
func (c mtGuardUnexportedController) READ_mtguardunexported() string { return "ok" }

func TestCreate_HTTPGuardInjection_UnexportedFieldPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New().Create(ModuleBuilder().Controllers(mtGuardUnexportedController{}).Build())
	})
	if !panicked {
		t.Error(test.DiffMessage(text, "panic", "guard with an unexported field should panic during injection"))
	}
}

type mtGuardUnresolvedController struct {
	common.HTTP
	common.Guard
}

func (c mtGuardUnresolvedController) NewController() Controller {
	c.BindGuard(mtUnresolvedFieldGuard{})
	return c
}
func (c mtGuardUnresolvedController) READ_mtguardunresolved() string { return "ok" }

func TestCreate_HTTPGuardInjection_UnresolvedDependencyPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New().Create(ModuleBuilder().Controllers(mtGuardUnresolvedController{}).Build())
	})
	if !panicked {
		t.Error(test.DiffMessage(text, "panic", "guard field with an unresolved provider dependency should panic"))
	}
}

type mtMiddlewareController struct {
	common.HTTP
	common.Middleware
}

func (c mtMiddlewareController) NewController() Controller {
	c.BindMiddleware(mtSimpleMiddleware{})
	return c
}
func (c mtMiddlewareController) READ_mtmiddleware() string { return "ok" }

func TestCreate_HTTPMiddlewareInjection_LocalProviderRegistered(t *testing.T) {
	mtSimpleMiddlewareSeen = mtSimpleMiddleware{}

	m := ModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtMiddlewareController{}).
		Build()
	app := New()
	mustCreate(t, app, m)

	w := createGet(app, "/mtmiddleware")
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "HTTP middleware should call next()"))
	}
	if mtSimpleMiddlewareSeen.P.Source != "local" {
		t.Error(test.DiffMessage(mtSimpleMiddlewareSeen.P.Source, "local", "HTTP middleware field should resolve from local module providers"))
	}
}

type mtInterceptorController struct {
	common.HTTP
	common.Interceptor
}

func (c mtInterceptorController) NewController() Controller {
	c.BindInterceptor(mtSimpleInterceptor{})
	return c
}
func (c mtInterceptorController) READ_mtinterceptor() string { return "ok" }

func TestCreate_HTTPInterceptorInjection_LocalProviderRegistered(t *testing.T) {
	mtSimpleInterceptorSeen = mtSimpleInterceptor{}

	m := ModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtInterceptorController{}).
		Build()
	app := New()
	mustCreate(t, app, m)
	createGet(app, "/mtinterceptor")

	if mtSimpleInterceptorSeen.P.Source != "local" {
		t.Error(test.DiffMessage(mtSimpleInterceptorSeen.P.Source, "local", "HTTP interceptor field should resolve from local module providers"))
	}
}

type mtExFilterSingleController struct {
	common.HTTP
	common.ExceptionFilter
}

func (c mtExFilterSingleController) NewController() Controller {
	c.BindExceptionFilter(mtExFilterOneField{})
	return c
}
func (c mtExFilterSingleController) READ_mtexfiltersingle() string { panic("boom") }

func TestCreate_HTTPExceptionFilter_SingleController_Registered(t *testing.T) {
	mtExFilterSeen = createRecorder{}

	m := ModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtExFilterSingleController{}).
		Build()
	app := New()
	mustCreate(t, app, m)
	createGet(app, "/mtexfiltersingle")

	if got := sprint(mtExFilterSeen.list()); got != "[local]" {
		t.Error(test.DiffMessage(got, "[local]", "exceptionFilter field should resolve from local module providers and the filter should run"))
	}
}

type mtExFilterMultiController0 struct {
	common.HTTP
	common.ExceptionFilter
}

func (c mtExFilterMultiController0) NewController() Controller {
	c.BindExceptionFilter(mtExFilterOneField{})
	return c
}
func (c mtExFilterMultiController0) READ_mtexfilterbugc0() string { panic("boom") }

type mtExFilterMultiController1 struct {
	common.HTTP
	common.ExceptionFilter
}

func (c mtExFilterMultiController1) NewController() Controller {
	c.BindExceptionFilter(mtExFilterOneField{})
	return c
}
func (c mtExFilterMultiController1) READ_mtexfilterbugc1() string { panic("boom") }

func TestCreate_HTTPExceptionFilter_TwoControllers_CorrectFieldIndex(t *testing.T) {
	mtExFilterSeen = createRecorder{}

	m := ModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtExFilterMultiController0{}, mtExFilterMultiController1{}).
		Build()
	app := New()
	mustCreate(t, app, m)
	createGet(app, "/mtexfilterbugc0")
	createGet(app, "/mtexfilterbugc1")

	if got := sprint(mtExFilterSeen.list()); got != "[local local]" {
		t.Error(test.DiffMessage(got, "[local local]", "each controller's filter resolves its provider field and runs"))
	}
}

func mtWSSubscribe(t *testing.T, app *App, topic string) WSPayload {
	t.Helper()
	server := httptest.NewServer(app)
	defer server.Close()

	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	var connected WSPayload
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(conn, WSPayload{Type: TypeSubscribe, ID: "1", Topic: []string{topic}}); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var reply WSPayload
	if err := websocket.JSON.Receive(conn, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Type == TypePing {
		t.Fatal(test.DiffMessage(reply.Type, "a reply to the subscribe", "mtWSSubscribe got a keep-alive ping instead of a reply"))
	}
	return reply
}

func mtWSSend(t *testing.T, app *App, topic string) {
	t.Helper()
	server := httptest.NewServer(app)
	defer server.Close()

	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var connected WSPayload
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(conn, WSPayload{Type: TypeSubscribe, ID: "1", Topic: []string{topic}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-mtWSExFilterSignal:
	case <-time.After(2 * time.Second):
		t.Fatal(test.DiffMessage("timeout", "the filter ran", "the WS exception filter must run for "+topic))
	}
}

func mtWSApp(t *testing.T, root *Module) *App {
	t.Helper()
	app := New()
	app.EnableWS(&WSConfig{Path: "/ws"})
	mustCreate(t, app, root)
	return app
}

type mtWSMainController struct{ common.WS }

func (c mtWSMainController) NewController() Controller         { return c }
func (c mtWSMainController) SUBSCRIBE_mtwsmainhandler() string { return "ok" }

func TestCreate_WSMainHandler_Registered(t *testing.T) {
	app := mtWSApp(t, ModuleBuilder().Controllers(mtWSMainController{}).Build())

	if reply := mtWSSubscribe(t, app, "mtwsmainhandler"); reply.Type != TypeAck {
		t.Error(test.DiffMessage(reply.Type, TypeAck, "the event of the WS main handler can be subscribed"))
	}
	if reply := mtWSSubscribe(t, app, "mtwsunknown"); reply.Type != TypeError {
		t.Error(test.DiffMessage(reply.Type, TypeError, "an event nothing handles is refused"))
	}
}

type mtPriorityGuardWS struct {
	Local       mtLocalProvider
	GlobalOnly  mtGlobalOnlyProvider
	Log         common.Logger
	Pub         common.Publisher
	Passthrough mtPassthroughValue
}

var mtPriorityGuardWSSeen mtPriorityGuardWS

func (g mtPriorityGuardWS) CanActivate(_ *ctx.WSContext) bool {
	mtPriorityGuardWSSeen = g
	return true
}

type mtWSGuardPriorityController struct {
	common.WS
	common.Guard
}

func (c mtWSGuardPriorityController) NewController() Controller {
	c.BindGuard(mtPriorityGuardWS{Passthrough: mtPassthroughValue{Tag: "original"}})
	return c
}
func (c mtWSGuardPriorityController) SUBSCRIBE_mtwsguardpriority() string { return "ok" }

func TestCreate_WSGuardInjection_PriorityChain(t *testing.T) {
	mtPriorityGuardWSSeen = mtPriorityGuardWS{}

	root, _ := mtPriorityRoot(mtWSGuardPriorityController{})
	app := mtWSApp(t, root)
	mtWSSubscribe(t, app, "mtwsguardpriority")

	mtAssertPriorityChain(t, mtPriorityGuard(mtPriorityGuardWSSeen), app, "WS guard")
}

type mtWSGuardUnexportedController struct {
	common.WS
	common.Guard
}

func (c mtWSGuardUnexportedController) NewController() Controller {
	c.BindGuard(mtUnexportedFieldGuard{})
	return c
}
func (c mtWSGuardUnexportedController) SUBSCRIBE_mtwsguardunexported() string { return "ok" }

func TestCreate_WSGuardInjection_UnexportedFieldPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		app := New()
		app.EnableWS(&WSConfig{Path: "/ws"})
		app.Create(ModuleBuilder().Controllers(mtWSGuardUnexportedController{}).Build())
	})
	if !panicked {
		t.Error(test.DiffMessage(text, "panic", "WS guard with an unexported field should panic during injection"))
	}
}

type mtWSGuardUnresolvedController struct {
	common.WS
	common.Guard
}

func (c mtWSGuardUnresolvedController) NewController() Controller {
	c.BindGuard(mtUnresolvedFieldGuard{})
	return c
}
func (c mtWSGuardUnresolvedController) SUBSCRIBE_mtwsguardunresolved() string { return "ok" }

func TestCreate_WSGuardInjection_UnresolvedDependencyPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		app := New()
		app.EnableWS(&WSConfig{Path: "/ws"})
		app.Create(ModuleBuilder().Controllers(mtWSGuardUnresolvedController{}).Build())
	})
	if !panicked {
		t.Error(test.DiffMessage(text, "panic", "WS guard field with an unresolved provider dependency should panic"))
	}
}

type mtSimpleInterceptorWS struct{ P mtLocalProvider }

var mtSimpleInterceptorWSSeen mtSimpleInterceptorWS

func (ic mtSimpleInterceptorWS) Intercept(_ *ctx.WSContext, _ *aggregation.Aggregation) any {
	mtSimpleInterceptorWSSeen = ic
	return nil
}

type mtWSInterceptorController struct {
	common.WS
	common.Interceptor
}

func (c mtWSInterceptorController) NewController() Controller {
	c.BindInterceptor(mtSimpleInterceptorWS{})
	return c
}
func (c mtWSInterceptorController) SUBSCRIBE_mtwsinterceptor() string { return "ok" }

func TestCreate_WSInterceptorInjection_LocalProviderRegistered(t *testing.T) {
	mtSimpleInterceptorWSSeen = mtSimpleInterceptorWS{}

	app := mtWSApp(t, ModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtWSInterceptorController{}).
		Build())
	mtWSSubscribe(t, app, "mtwsinterceptor")

	if mtSimpleInterceptorWSSeen.P.Source != "local" {
		t.Error(test.DiffMessage(mtSimpleInterceptorWSSeen.P.Source, "local", "WS interceptor field should resolve from local module providers"))
	}
}

var mtWSExFilterSeen createRecorder

type mtExFilterOneFieldWS struct{ P mtLocalProvider }

var mtWSExFilterSignal = make(chan struct{}, 8)

func (e mtExFilterOneFieldWS) Catch(_ *ctx.WSContext, _ *exception.Exception) {
	mtWSExFilterSeen.add(e.P.Source)
	mtWSExFilterSignal <- struct{}{}
}

type mtDenyGuardWS struct{}

func (mtDenyGuardWS) CanActivate(_ *ctx.WSContext) bool { return false }

type mtWSExFilterController0 struct {
	common.WS
	common.Guard
	common.ExceptionFilter
}

func (c mtWSExFilterController0) NewController() Controller {
	c.BindGuard(mtDenyGuardWS{})
	c.BindExceptionFilter(mtExFilterOneFieldWS{})
	return c
}
func (c mtWSExFilterController0) SUBSCRIBE_mtwsexfilterc0() string { return "ok" }

type mtWSExFilterController1 struct {
	common.WS
	common.Guard
	common.ExceptionFilter
}

func (c mtWSExFilterController1) NewController() Controller {
	c.BindGuard(mtDenyGuardWS{})
	c.BindExceptionFilter(mtExFilterOneFieldWS{})
	return c
}
func (c mtWSExFilterController1) SUBSCRIBE_mtwsexfilterc1() string { return "ok" }

func TestCreate_WSExceptionFilter_TwoControllers_CorrectFieldIndex(t *testing.T) {
	mtWSExFilterSeen = createRecorder{}

	app := mtWSApp(t, ModuleBuilder().
		Providers(mtLocalProvider{}).
		Controllers(mtWSExFilterController0{}, mtWSExFilterController1{}).
		Build())
	mtWSSend(t, app, "mtwsexfilterc0")
	mtWSSend(t, app, "mtwsexfilterc1")

	if got := sprint(mtWSExFilterSeen.list()); got != "[local local]" {
		t.Error(test.DiffMessage(got, "[local local]", "each controller's WS filter resolves its provider field and runs"))
	}
}

type mtConcurrentProvider struct{}

func (p mtConcurrentProvider) NewProvider() Provider { return p }

type mtConcurrentController struct{ common.HTTP }

func (c mtConcurrentController) NewController() Controller         { return c }
func (c mtConcurrentController) READ_mtconcurrentresource() string { return "ok" }

func TestCreate_ConcurrentInvocation_NoDataRace(t *testing.T) {
	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()

			m := ModuleBuilder().
				Providers(mtConcurrentProvider{}).
				Controllers(mtConcurrentController{}).
				Build()
			if text, panicked := createPanicText(func() { New().Create(m) }); panicked {
				t.Error(test.DiffMessage(text, "no panic", "concurrent Create"))
			}
		}()
	}
	wg.Wait()
}

func TestCreate_Hooks_ReachNestedStaticAndDynamicImportsAtAnyDepth(t *testing.T) {
	rec := &createRecorder{}

	grandchild := hkModule(rec, "grandchild")
	dynamicChild := func() *Module { return hkModule(rec, "dynamic") }
	child := hkModule(rec, "child", grandchild, dynamicChild)
	root := hkModule(rec, "root", child)

	mustCreate(t, New(), root)

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

	mustCreate(t, New(), root)

	if got := rec.count("ready:shared"); got != 1 {
		t.Error(test.DiffMessage(got, 1, "a module imported by two different parents should only be initialized once"))
	}
}
