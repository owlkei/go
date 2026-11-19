package core

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

type lfWorld struct {
	app                              *App
	logger                           *lcRecordingLogger
	sql, redis, user, booking, cache *Module
	root                             *Module
}

func lfNewWorld(t *testing.T) *lfWorld {
	t.Helper()
	w := &lfWorld{}
	w.sql = lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	w.redis = lfNamed("redis", NewModuleBuilder().Providers(lfRedis{}))
	w.user = lfNamed("user", NewModuleBuilder().AsyncImports(w.sql).Providers(lfUserSvc{}).Controllers(lfUserCtl{}))
	w.booking = lfNamed("booking", NewModuleBuilder().Imports(w.user).Controllers(lfBookingCtl{}))
	w.cache = lfNamed("cache", NewModuleBuilder().AsyncImports(w.redis).Controllers(lfRedisCtl{}))
	w.root = lfNamed("root", NewModuleBuilder().Imports(w.user, w.booking, w.cache).Controllers(lfPlainCtl{}))
	release := lfHold(w.sql, w.redis)
	w.app = New()
	w.logger = lfLogger(w.app)
	mustCreate(t, w.app, w.root)
	lfRelease(t, w.app, release)
	return w
}

func lfNeedApp(t *testing.T, ctl Controller, providers ...Provider) (*App, *Module, *Module) {
	t.Helper()
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	b := NewModuleBuilder().AsyncImports(sql).Providers(providers...)
	if ctl != nil {
		b.Controllers(ctl)
	}
	user := lfNamed("user", b)
	release := lfHold(sql)
	app := New()
	lfLogger(app)
	mustCreate(t, app, lfNamed("root", NewModuleBuilder().Imports(user)))
	lfRelease(t, app, release)
	return app, sql, user
}

func TestModuleState_StringNames(t *testing.T) {
	for state, want := range map[moduleStatus]string{modulePending: "pending", moduleReady: "ready", moduleFailed: "failed"} {
		if got := state.String(); got != want {
			t.Error(test.DiffMessage(got, want, "G1: state name"))
		}
	}
}

func TestModuleState_ReadyWithoutAsync(t *testing.T) {
	child := NewModuleBuilder().Controllers(lfPlainCtl{}).Build()
	root := NewModuleBuilder().Imports(child).Build()
	app := New()
	mustCreate(t, app, root)

	for name, m := range map[string]*Module{"root": root, "child": child} {
		if got := lfStatus(app, m); got != moduleReady {
			t.Error(test.DiffMessage(got.String(), "ready", "G1: "+name+" is ready when Create returns"))
		}
	}
}

func TestModuleState_EntryAndNeedersArePendingAtCreate(t *testing.T) {
	w := lfNewWorld(t)

	for name, m := range map[string]*Module{"sql": w.sql, "redis": w.redis, "user": w.user, "booking": w.booking, "cache": w.cache} {
		if got := lfStatus(w.app, m); got != modulePending {
			t.Error(test.DiffMessage(got.String(), "pending", "G1: "+name+" is pending until its entries settle"))
		}
	}
	if got := lfStatus(w.app, w.root); got != moduleReady {
		t.Error(test.DiffMessage(got.String(), "ready", "G2: the root imports everything and needs nothing"))
	}
}

func TestModuleState_DependentPendingThroughProvider(t *testing.T) {
	app, _, user := lfNeedApp(t, nil, lfUserSvc{})
	if got := lfStatus(app, user); got != modulePending {
		t.Error(test.DiffMessage(got.String(), "pending", "G2: a provider with a field of the entry's provider"))
	}
}

func TestModuleState_ThroughControllerField(t *testing.T) {
	app, _, user := lfNeedApp(t, lfSQLCtl{})
	if got := lfStatus(app, user); got != modulePending {
		t.Error(test.DiffMessage(got.String(), "pending", "G2: a controller with a field of the entry's provider"))
	}
}

func TestModuleState_ThroughBoundGuard(t *testing.T) {
	app, _, user := lfNeedApp(t, lfGuardCtl{})
	if got := lfStatus(app, user); got != modulePending {
		t.Error(test.DiffMessage(got.String(), "pending", "G2: a guard bound in NewController that injects the entry's provider"))
	}
}

func TestModuleState_ThroughPipe(t *testing.T) {
	app, _, user := lfNeedApp(t, lfPipeCtl{})
	if got := lfStatus(app, user); got != modulePending {
		t.Error(test.DiffMessage(got.String(), "pending", "G2: a pipe in a handler signature that injects the entry's provider"))
	}
}

func TestModuleState_ImportAloneIsNotANeed(t *testing.T) {
	app, _, user := lfNeedApp(t, lfPlainCtl{})

	if got := lfStatus(app, user); got != moduleReady {
		t.Error(test.DiffMessage(got.String(), "ready", "G2: importing an async module is not a need"))
	}
	if got := lfCode(app, "/lfplain/hello"); got != http.StatusOK {
		t.Error(test.DiffMessage(got, http.StatusOK, "G2: the route of a module that only imports serves"))
	}
}

func TestModuleState_FailurePropagates(t *testing.T) {
	w := lfNewWorld(t)

	w.app.container.settle(w.sql, errors.New("lf-sql-down"))

	for name, m := range map[string]*Module{"sql": w.sql, "user": w.user, "booking": w.booking} {
		if got := lfStatus(w.app, m); got != moduleFailed {
			t.Error(test.DiffMessage(got.String(), "failed", "G3: "+name+" fails with sql"))
		}
	}
	if got := lfStatus(w.app, w.cache); got != modulePending {
		t.Error(test.DiffMessage(got.String(), "pending", "G3: a module that needs only redis is untouched"))
	}
	if got := lfStatus(w.app, w.root); got != moduleReady {
		t.Error(test.DiffMessage(got.String(), "ready", "G3: the root is untouched"))
	}
	select {
	case <-w.app.container.settled(w.booking):
	default:
		t.Error(test.DiffMessage("open", "closed", "G1: settled(booking) is closed once it failed"))
	}
	select {
	case <-w.app.container.settled(w.cache):
		t.Error(test.DiffMessage("closed", "open", "G1: settled(cache) stays open while pending"))
	default:
	}
}

func TestModuleState_OneWay(t *testing.T) {
	w := lfNewWorld(t)

	w.app.container.settle(w.sql, errors.New("first"))
	w.app.container.settle(w.sql, nil)
	w.app.container.settle(w.sql, errors.New("second"))

	if got := lfStatus(w.app, w.sql); got != moduleFailed {
		t.Error(test.DiffMessage(got.String(), "failed", "G1: the first settle wins"))
	}
	if got := lfStatus(w.app, w.user); got != moduleFailed {
		t.Error(test.DiffMessage(got.String(), "failed", "G1: a dependent never goes back"))
	}
}

func TestModuleState_AppWideGuardOnAsyncPanics(t *testing.T) {
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	sql.IsGlobal = true
	app := New()
	app.BindGlobalGuards(lfSQLGuard{})

	text := lfCreatePanic(t, app, NewModuleBuilder().AsyncImports(sql).Build())

	for _, want := range []string{
		"dependency injection: app-wide guard '",
		"lfSQLGuard",
		"lfSQL' of 'sql', which is not ready at Create: app-wide layers can't depend on AsyncImports",
	} {
		if !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "G4: an app-wide layer that injects an async provider"))
		}
	}
}

func TestModuleState_AsyncCyclePanics(t *testing.T) {
	alpha := lfNamed("alpha", NewModuleBuilder().Providers(lfAlphaSvc{}).Controllers(lfNeedsBetaCtl{}))
	alpha.IsGlobal = true
	beta := lfNamed("beta", NewModuleBuilder().Providers(lfBetaSvc{}).Controllers(lfNeedsAlphaCtl{}))
	beta.IsGlobal = true

	text := lfCreatePanic(t, New(), NewModuleBuilder().AsyncImports(alpha, beta).Build())

	want := "invalid module: async modules need each other: 'alpha' → 'beta' → 'alpha'"
	if !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "G5: two entries that need each other"))
	}
}

func TestRouteProbe_ChecksRoutesAtCreate(t *testing.T) {
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	gated := lfNamed("gated", NewModuleBuilder().AsyncImports(sql).Controllers(lfDupGatedCtl{}))
	root := NewModuleBuilder().Imports(gated).Controllers(lfDupOpenCtl{}).Build()

	text := lfCreatePanic(t, New(), root)

	if !strings.Contains(text, "route conflict") || !strings.Contains(text, "READ_hello") {
		t.Error(test.DiffMessage(text, "route conflict: …READ_hello…", "G6: a gated route conflicts with an open one at Create"))
	}
}

func TestRouteProbe_NewControllerPanicNamesController(t *testing.T) {
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	user := lfNamed("user", NewModuleBuilder().AsyncImports(sql).Controllers(lfPanicCtl{}))

	text := lfCreatePanic(t, New(), NewModuleBuilder().Imports(user).Build())

	for _, want := range []string{
		"invalid module: NewController of '",
		"lfPanicCtl",
		"' failed before its module 'user' is ready: lf-newcontroller-boom. NewController must not use injected providers",
	} {
		if !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "G6: a panic in the probe's NewController"))
		}
	}
}

func TestRouteProbe_ChecksSubmitNeedsRender(t *testing.T) {
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	user := lfNamed("user", NewModuleBuilder().AsyncImports(sql).Controllers(lfSubmitOnlyCtl{}))

	text := lfCreatePanic(t, New(), NewModuleBuilder().Imports(user).Build())

	want := "invalid handler: lfSubmitOnlyCtl.SUBMIT_x needs lfSubmitOnlyCtl.RENDER_x: a SUBMIT_ posts to the page that shows its form"
	if !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "G6: the page checks run for a gated controller at Create"))
	}
}

func TestRouteProbe_NoHookRunsWhilePending(t *testing.T) {
	var inits, readies, providers atomic.Int64
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	user := lfNamed("user", NewModuleBuilder().AsyncImports(sql).Providers(lfCountingSvc{Count: &providers}).Controllers(lfUserCtl{}))
	user.OnInit = func() { inits.Add(1) }
	user.OnReady = func() { readies.Add(1) }
	release := lfHold(sql)

	app := New()
	lfLogger(app)
	mustCreate(t, app, NewModuleBuilder().Imports(user).Build())
	lfRelease(t, app, release)

	if inits.Load() != 0 || readies.Load() != 0 || providers.Load() != 0 {
		t.Error(test.DiffMessage([]int64{inits.Load(), readies.Load(), providers.Load()}, []int64{0, 0, 0}, "G14: OnInit, NewProvider and OnReady of a pending module do not run in Create"))
	}
}

type lfCountingSvc struct {
	DB    lfSQL
	Count *atomic.Int64
}

func (p lfCountingSvc) NewProvider() Provider { p.Count.Add(1); return p }

func TestApp_Get_NilWhilePending(t *testing.T) {
	app, _, _ := lfNeedApp(t, nil, lfUserSvc{})

	if got := app.Get(lfUserSvc{}); got != nil {
		t.Error(test.DiffMessage(got, nil, "G14: Get returns nil for a provider that is not built yet"))
	}
}

func TestModuleBuilder_AsyncImports_BothListsPanics(t *testing.T) {
	shared := lfNamed("shared", NewModuleBuilder())

	text := lfBuildPanic(t, NewModuleBuilder().Imports(shared).AsyncImports(shared))
	if want := "invalid module: 'shared' is in both Imports and AsyncImports"; !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "G13: the same Module in both lists"))
	}

	text = lfBuildPanic(t, NewModuleBuilder().Imports(lfCountedFactory).AsyncImports(lfCountedFactory))
	if want := "invalid module: 'core.lfCountedFactory' is in both Imports and AsyncImports"; !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "G13: the same function in both lists"))
	}
}

func TestModuleBuilder_AsyncImports_BothListsAcrossModulesPanics(t *testing.T) {
	shared := lfNamed("shared", NewModuleBuilder())
	x := lfNamed("x", NewModuleBuilder().Imports(shared))
	y := lfNamed("y", NewModuleBuilder().AsyncImports(shared))

	text := lfCreatePanic(t, New(), NewModuleBuilder().Imports(x, y).Build())

	want := "invalid module: 'shared' is imported with Imports by 'x' and with AsyncImports by 'y'"
	if !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "G13: across two modules"))
	}
}
