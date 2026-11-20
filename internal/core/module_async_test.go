package core

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlky.com/owlky/internal/test"
)

func lfWatchApp(t *testing.T) (*App, *lfWatchLog) {
	t.Helper()
	logger := newLFWatchLog()
	app := New(&AppOptions{Logger: logger})
	return app, logger
}

func lfIndex(list []string, item string) int { return slices.Index(list, item) }

func TestAsync_CreateReturnsWhileFactoryBlocks(t *testing.T) {
	release := make(chan struct{})
	var hasDeadline atomic.Bool
	factory := func(c context.Context) (*Module, error) {
		_, ok := c.Deadline()
		hasDeadline.Store(ok)
		<-release
		return lfNamed("storage", NewModuleBuilder().Providers(lfSQL{})), nil
	}
	user := lfNamed("user", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	app, _ := lfWatchApp(t)
	lfRelease(t, app, release)

	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())

	if got := lfStatus(app, user); got != modulePending {
		t.Error(test.DiffMessage(got.String(), "pending", "A1: Create returns while the factory blocks"))
	}
	if got := lfCode(app, "/lfsql/hello"); got != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage(got, http.StatusServiceUnavailable, "A1: the app serves, the route answers 503"))
	}
	close(release)
	lfWait(t, app.container.settled(user), "user")
	if hasDeadline.Load() {
		t.Error(test.DiffMessage(true, false, "A2: an async factory's context has no deadline"))
	}
}

func TestAsync_ReadySwitchesRoutesOn(t *testing.T) {
	ent := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}).Controllers(lfSQLCtl{}))
	release := lfHold(ent)
	app, _ := lfWatchApp(t)
	lfRelease(t, app, release)
	lfCreate(t, app, NewModuleBuilder().AsyncImports(ent).Build())

	if got := lfCode(app, "/lfsql/hello"); got != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage(got, http.StatusServiceUnavailable, "A3: 503 while the module is pending"))
	}

	close(release)
	lfWait(t, app.container.settled(ent), "sql")

	w := createGet(app, "/lfsql/hello")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ok") {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "A3: the route switches on by itself, with the built provider injected"))
	}
}

func TestAsync_OrderOnInitProvidersControllersOnReady(t *testing.T) {
	events := &lfEvents{}
	ent := lfNamed("ent", NewModuleBuilder().Providers(lfRecProvider{Ev: events}).Controllers(lfRecCtl{Ev: events}))
	ready := make(chan struct{})
	release := make(chan struct{})
	ent.OnInit = func() { events.add("init"); <-release }
	ent.OnReady = func() { events.add("ready"); close(ready) }
	app, _ := lfWatchApp(t)
	lfRelease(t, app, release)
	lfCreate(t, app, NewModuleBuilder().AsyncImports(ent).Build())

	close(release)
	lfWait(t, ready, "OnReady")

	want := []string{"controller", "init", "provider", "controller", "ready"}
	if got := events.all(); !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "A2: probe, then OnInit, NewProvider, NewController, OnReady"))
	}
}

func TestAsync_DependentsInDiscoveryOrder(t *testing.T) {
	events := &lfEvents{}
	ent := lfNamed("ent", NewModuleBuilder().Providers(lfSQL{}))
	release := lfHold(ent)
	a := lfNamed("a", NewModuleBuilder().AsyncImports(ent).Providers(lfUserSvc{}))
	b := lfNamed("b", NewModuleBuilder().AsyncImports(ent).Providers(lfOtherSvc{}))
	readyB := make(chan struct{})
	a.OnInit = func() { events.add("init:a") }
	b.OnInit = func() { events.add("init:b") }
	a.OnReady = func() { events.add("ready:a") }
	b.OnReady = func() { events.add("ready:b"); close(readyB) }
	app, _ := lfWatchApp(t)
	lfRelease(t, app, release)
	lfCreate(t, app, NewModuleBuilder().Imports(a, b).Build())

	close(release)
	lfWait(t, readyB, "OnReady of b")

	got := events.all()
	if lfIndex(got, "init:a") < 0 || lfIndex(got, "init:a") >= lfIndex(got, "init:b") {
		t.Error(test.DiffMessage(got, "init:a before init:b", "A3: OnInit in discovery order"))
	}
	if lfIndex(got, "ready:a") < 0 || lfIndex(got, "ready:a") >= lfIndex(got, "ready:b") {
		t.Error(test.DiffMessage(got, "ready:a before ready:b", "A3: OnReady in discovery order"))
	}
	if lfIndex(got, "init:b") > lfIndex(got, "ready:a") {
		t.Error(test.DiffMessage(got, "both built before OnReady", "A3: OnReady follows the building of the dependents"))
	}
}

func lfOrderApp(t *testing.T, async bool) (*App, *lfEvents, func()) {
	t.Helper()
	log := &lfEvents{}
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	b := NewModuleBuilder().Controllers(lfOrderCtl{Log: log})
	var release chan struct{}
	if async {
		release = lfHold(sql)
		b.AsyncImports(sql)
	} else {
		b.Imports(sql)
	}
	user := lfNamed("user", b)
	app, _ := lfWatchApp(t)
	app.BindGlobalMiddlewares(lfOrderMW{Tag: "global-mw", Log: log})
	app.BindGlobalGuards(lfOrderGuard{Tag: "global-guard", Log: log})
	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())
	if !async {
		return app, log, func() { lfRelease(t, app) }
	}
	lfRelease(t, app, release)
	return app, log, func() {
		close(release)
		lfWait(t, app.container.settled(user), "user")
	}
}

func TestAsync_OpenRouteHasSameLayersAsSync(t *testing.T) {
	syncApp, syncLog, syncReady := lfOrderApp(t, false)
	syncReady()
	if got := lfCode(syncApp, "/lforder/hello"); got != http.StatusOK {
		t.Fatal(test.DiffMessage(got, http.StatusOK, "A4: the sync app serves"))
	}
	want := syncLog.all()

	asyncApp, asyncLog, open := lfOrderApp(t, true)
	open()
	if got := lfCode(asyncApp, "/lforder/hello"); got != http.StatusOK {
		t.Fatal(test.DiffMessage(got, http.StatusOK, "A4: the opened route serves"))
	}

	if got := asyncLog.all(); !slices.Equal(got, want) || len(want) != 4 {
		t.Error(test.DiffMessage(got, want, "A4: the same layers in the same order as with Imports"))
	}
}

func TestAsync_StaticAsyncModuleWithControllers(t *testing.T) {
	ent := lfNamed("ent", NewModuleBuilder().Providers(lfSQL{}).Controllers(lfSQLCtl{}))
	release := lfHold(ent)
	app, _ := lfWatchApp(t)
	lfRelease(t, app, release)
	lfCreate(t, app, NewModuleBuilder().AsyncImports(ent).Build())

	if got := lfCode(app, "/lfsql/hello"); got != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage(got, http.StatusServiceUnavailable, "A8: controllers of a static async module are gated at Create"))
	}
	close(release)
	lfWait(t, app.container.settled(ent), "ent")
	if got := lfCode(app, "/lfsql/hello"); got != http.StatusOK {
		t.Error(test.DiffMessage(got, http.StatusOK, "A8: and open once ready"))
	}
}

func TestAsync_FactoryWaitsForAsyncSibling(t *testing.T) {
	release := make(chan struct{})
	var aDone, sawA, sawBuilt atomic.Bool
	a := func() (*Module, error) {
		<-release
		aDone.Store(true)
		return lfNamed("a", NewModuleBuilder().Providers(lfSQL{})), nil
	}
	b := func(db lfSQL) (*Module, error) {
		sawA.Store(aDone.Load())
		sawBuilt.Store(db.Ready)
		return lfNamed("b", NewModuleBuilder().Providers(lfCache{})), nil
	}
	holder := lfNamed("holder", NewModuleBuilder().AsyncImports(b, a))
	app, _ := lfWatchApp(t)
	lfRelease(t, app, release)
	lfCreate(t, app, NewModuleBuilder().Imports(holder).Build())

	close(release)
	lfWait(t, app.container.settledFactory(b), "factory b")

	if !sawA.Load() || !sawBuilt.Load() {
		t.Error(test.DiffMessage([]bool{sawA.Load(), sawBuilt.Load()}, []bool{true, true}, "A2: a factory waits for the sibling that owns its parameter, which arrives built"))
	}
	if got := app.container.statusOfFactory(b); got != moduleReady {
		t.Error(test.DiffMessage(got.String(), "ready", "A2: the waiting factory becomes ready"))
	}
}

func TestAsync_WSEventSwitchesOn(t *testing.T) {
	_, ent, conn := lfWSAsyncApp(t)

	if reply := lfWSSend(t, conn, TypeSubscribe, "s1", "lfgated"); reply.Type != TypeError {
		t.Fatal(test.DiffMessage(reply.Type, TypeError, "G12: not ready before the release"))
	}

	ent.release()
	lfWait(t, ent.settled, "ent")

	if reply := lfWSSend(t, conn, TypeSubscribe, "s2", "lfgated"); reply.Type != TypeAck {
		t.Error(test.DiffMessage(reply.Type, TypeAck, "A3: the event switches on by itself"))
	}
}

func TestAsync_FactoryErrorFails(t *testing.T) {
	factory := func() (*Module, error) { return nil, errors.New("lf-dsn-refused") }
	user := lfNamed("user", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	app, logger := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())

	lfWait(t, app.container.settled(user), "user")

	if got := lfStatus(app, user); got != moduleFailed {
		t.Error(test.DiffMessage(got.String(), "failed", "A5: the module that needs the failed entry fails"))
	}
	var found bool
	for _, e := range logger.named("ModuleFailed") {
		found = found || strings.Contains(lfCause(e), "lf-dsn-refused")
		if e.level != "error" {
			t.Error(test.DiffMessage(e.level, "error", "A5: ModuleFailed is an Error line"))
		}
	}
	if !found {
		t.Error(test.DiffMessage(logger.named("ModuleFailed"), "a cause with lf-dsn-refused", "A5: ModuleFailed carries the factory's error"))
	}
	w := createGet(app, "/lfsql/hello")
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "" {
		t.Error(test.DiffMessage(w.Code, http.StatusServiceUnavailable, "A5: the route stays 503, without Retry-After"))
	}
}

func TestAsync_FactoryPanicFails(t *testing.T) {
	factory := func() (*Module, error) { panic("lf-async-kaboom") }
	user := lfNamed("user", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	app, logger := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())

	lfWait(t, app.container.settled(user), "user")

	if got := lfStatus(app, user); got != moduleFailed {
		t.Error(test.DiffMessage(got.String(), "failed", "A5: a panic in a factory fails the module"))
	}
	var withStack bool
	for _, e := range logger.named("ModuleFailed") {
		if strings.Contains(lfCause(e), "lf-async-kaboom") && lcText(e.field("stack")) != "" && e.field("stack") != nil {
			withStack = true
		}
	}
	if !withStack {
		t.Error(test.DiffMessage(logger.named("ModuleFailed"), "a cause and a stack", "A5: the panic is recovered and its stack logged"))
	}
}

func TestAsync_ProviderPanicFails(t *testing.T) {
	bad := lfNamed("bad", NewModuleBuilder().Providers(lfBoomProvider{}).Controllers(lfPlainCtl{}))
	app, logger := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().AsyncImports(bad).Build())

	lfWait(t, app.container.settled(bad), "bad")

	if got := lfStatus(app, bad); got != moduleFailed {
		t.Error(test.DiffMessage(got.String(), "failed", "A5: a panic in NewProvider fails the module"))
	}
	entries := lfFailedFor(logger.lcRecordingLogger, "bad")
	if len(entries) != 1 || !strings.Contains(lfCause(entries[0]), "lf-provider-boom") || entries[0].field("stack") == nil {
		t.Error(test.DiffMessage(entries, "one ModuleFailed with the cause and a stack", "A5"))
	}
	if got := lfCode(app, "/lfplain/hello"); got != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage(got, http.StatusServiceUnavailable, "A5: a failed module's gate never opens"))
	}
}

func TestAsync_SQLFailsUserAndBooking503RedisBookingWorks(t *testing.T) {
	sqlFn := func() (*Module, error) { return nil, errors.New("lf-sql-refused") }
	redisFn := func() (*Module, error) { return lfNamed("redis", NewModuleBuilder().Providers(lfRedis{})), nil }
	user := lfNamed("user", NewModuleBuilder().AsyncImports(sqlFn).Providers(lfUserSvc{}).Controllers(lfUserCtl{}))
	booking := lfNamed("booking", NewModuleBuilder().Imports(user).Controllers(lfBookingCtl{}))
	cache := lfNamed("cache", NewModuleBuilder().AsyncImports(redisFn).Controllers(lfRedisCtl{}))
	root := lfNamed("root", NewModuleBuilder().Imports(user, booking, cache).Controllers(lfPlainCtl{}))
	app, _ := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, root)

	for _, m := range []*Module{user, booking, cache} {
		lfWait(t, app.container.settled(m), m.Name)
	}

	for path, want := range map[string]int{
		"/lfuser/hello":    http.StatusServiceUnavailable,
		"/lfbooking/hello": http.StatusServiceUnavailable,
		"/lfredis/hello":   http.StatusOK,
		"/lfplain/hello":   http.StatusOK,
	} {
		if got := lfCode(app, path); got != want {
			t.Error(test.DiffMessage(got, want, "backlog scenario: "+path))
		}
	}
	for m, want := range map[*Module]moduleStatus{user: moduleFailed, booking: moduleFailed, cache: moduleReady} {
		if got := lfStatus(app, m); got != want {
			t.Error(test.DiffMessage(got.String(), want.String(), "backlog scenario: state of "+m.Name))
		}
	}
}

func TestAsync_DependentPanicFailsOnlyIt(t *testing.T) {
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	a := lfNamed("a", NewModuleBuilder().AsyncImports(sql).Providers(lfBoomSvc{}).Controllers(lfSQLCtl{}))
	b := lfNamed("b", NewModuleBuilder().AsyncImports(sql).Providers(lfUserSvc{}).Controllers(lfUserCtl{}))
	app, _ := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().Imports(a, b).Build())

	lfWait(t, app.container.settled(a), "a")
	lfWait(t, app.container.settled(b), "b")

	for m, want := range map[*Module]moduleStatus{sql: moduleReady, a: moduleFailed, b: moduleReady} {
		if got := lfStatus(app, m); got != want {
			t.Error(test.DiffMessage(got.String(), want.String(), "A6: state of "+m.Name))
		}
	}
	if got := lfCode(app, "/lfuser/hello"); got != http.StatusOK {
		t.Error(test.DiffMessage(got, http.StatusOK, "A6: the module that built stays ready"))
	}
	if got := lfCode(app, "/lfsql/hello"); got != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage(got, http.StatusServiceUnavailable, "A6: the failed module's route is 503"))
	}
}

func TestAsync_UnknownProviderPendingThenFailed(t *testing.T) {
	release := make(chan struct{})
	factory := func() (*Module, error) { <-release; return lfNamed("empty", NewModuleBuilder()), nil }
	user := lfNamed("user", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	app, logger := lfWatchApp(t)
	lfRelease(t, app, release)
	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())

	if got := lfStatus(app, user); got != modulePending {
		t.Error(test.DiffMessage(got.String(), "pending", "A7: an unknown provider does not panic at Create, the module is pending"))
	}

	close(release)
	lfWait(t, app.container.settled(user), "user")

	if got := lfStatus(app, user); got != moduleFailed {
		t.Error(test.DiffMessage(got.String(), "failed", "A7: failed once the last factory returned without it"))
	}
	entries := lfFailedFor(logger.lcRecordingLogger, "user")
	want := "can't resolve " + reflect.TypeFor[lfSQL]().String()
	if len(entries) != 1 || lfCause(entries[0]) != want {
		t.Error(test.DiffMessage(entries, "cause "+want, "A7"))
	}
}

func TestAsync_UnknownProviderArrivesFromFactory(t *testing.T) {
	release := make(chan struct{})
	factory := func() (*Module, error) {
		<-release
		return lfNamed("storage", NewModuleBuilder().Providers(lfSQL{})), nil
	}
	user := lfNamed("user", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	app, _ := lfWatchApp(t)
	lfRelease(t, app, release)
	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())

	close(release)
	lfWait(t, app.container.settled(user), "user")

	if got := lfStatus(app, user); got != moduleReady {
		t.Error(test.DiffMessage(got.String(), "ready", "A7: the factory's module provides it, the entry became a need"))
	}
	if w := createGet(app, "/lfsql/hello"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ok") {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "A7: the route serves with the provider injected"))
	}
}

func TestAsync_UnknownProviderWithoutAsyncFactoryStillPanics(t *testing.T) {
	redis := lfNamed("redis", NewModuleBuilder().Providers(lfRedis{}))
	needy := NewModuleBuilder().Controllers(lfMissingCtl{}).Build()
	root := NewModuleBuilder().Imports(needy).AsyncImports(redis).Build()

	text := lfCreatePanic(t, New(nil), root)

	if !strings.Contains(text, "dependency injection: can't resolve dependency '") {
		t.Error(test.DiffMessage(text, "dependency injection: can't resolve dependency '…' of the …", "A7: without an async factory the app panics as today"))
	}
}

func TestAsync_FactoryModuleWithControllersRefused(t *testing.T) {
	factory := func() (*Module, error) {
		return lfNamed("withctl", NewModuleBuilder().Controllers(lfPlainCtl{})), nil
	}
	app, logger := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().AsyncImports(factory).Build())

	logger.waitCount(t, "ModuleFailed", 1)

	cause := lfCause(logger.named("ModuleFailed")[0])
	for _, want := range []string{
		"module 'withctl' declares controllers (", "lfPlainCtl",
		") but comes from an async factory: put them in a static module that imports it",
	} {
		if !strings.Contains(cause, want) {
			t.Error(test.DiffMessage(cause, want, "A8: a factory's module with controllers"))
		}
	}
	if got := lfCode(app, "/lfplain/hello"); got != http.StatusNotFound {
		t.Error(test.DiffMessage(got, http.StatusNotFound, "A8: the refused controllers are never registered"))
	}
}

func TestAsync_RoutesChangedFails(t *testing.T) {
	lfChangingCalls.Store(0)
	chg := lfNamed("chg", NewModuleBuilder().Controllers(lfChangingCtl{}))
	app, logger := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().AsyncImports(chg).Build())

	lfWait(t, app.container.settled(chg), "chg")

	if got := lfStatus(app, chg); got != moduleFailed {
		t.Error(test.DiffMessage(got.String(), "failed", "A9: routes that differ from the probe fail the module"))
	}
	entries := lfFailedFor(logger.lcRecordingLogger, "chg")
	if len(entries) != 1 || !strings.Contains(lfCause(entries[0]), "changed between Create and construction") ||
		!strings.Contains(lfCause(entries[0]), "routes of '") {
		t.Error(test.DiffMessage(entries, "routes of '…' changed between Create and construction", "A9"))
	}
	if got := lfCode(app, "/lfchg1/hello"); got != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage(got, http.StatusServiceUnavailable, "A9: the probed route stays 503"))
	}
}

func TestAsync_FactoryCalledOnce(t *testing.T) {
	var calls atomic.Int64
	factory := func() (*Module, error) { calls.Add(1); return nil, errors.New("lf-once") }
	one := lfNamed("one", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	two := lfNamed("two", NewModuleBuilder().AsyncImports(factory).Providers(lfUserSvc{}))
	app, _ := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().Imports(one, two).Build())

	lfWait(t, app.container.settled(one), "one")
	lfWait(t, app.container.settled(two), "two")

	if got := calls.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "A5: the framework never calls a factory twice, nor from two modules"))
	}
}

func TestAsync_ShutdownWhilePendingCancelsCtx(t *testing.T) {
	var shutdowns atomic.Int64
	seen := make(chan struct{})
	factory := func(c context.Context) (*Module, error) {
		<-c.Done()
		close(seen)
		return nil, c.Err()
	}
	user := lfNamed("user", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	user.OnShutdown = func() { shutdowns.Add(1) }
	app, _ := lfWatchApp(t)
	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())

	lfStop(t, app)

	lfWait(t, seen, "the factory to see ctx.Done")
	if shutdowns.Load() != 0 {
		t.Error(test.DiffMessage(shutdowns.Load(), int64(0), "A10, A12: no OnShutdown for a module that never became ready"))
	}
}

func TestAsync_ShutdownTimeoutAbandons(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	factory := func(context.Context) (*Module, error) {
		close(started)
		<-release
		return nil, errors.New("late")
	}
	user := lfNamed("user", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	logger := newLFListenLogger()
	app := New(&AppOptions{Logger: logger, Server: &ServerOptions{ShutdownTimeout: 50 * time.Millisecond}})
	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())
	done := lfServe(t, app, logger)
	lfWait(t, started, "the factory to start")
	t.Cleanup(func() { close(release) })

	lfStop(t, app)
	if err := <-done; err != nil {
		t.Log(err)
	}

	entries := logger.named("ModuleShutdownTimeout")
	if len(entries) != 1 || entries[0].level != "warn" || entries[0].field("module") == nil {
		t.Error(test.DiffMessage(entries, "one Warn ModuleShutdownTimeout with a module", "A11: the entry that ignores ctx is logged and abandoned"))
	}
}

func TestAsync_LateResultDropped(t *testing.T) {
	events := &lfEvents{}
	factory := func(c context.Context) (*Module, error) {
		<-c.Done()
		late := lfNamed("late", NewModuleBuilder().Providers(lfSQL{}))
		late.OnInit = func() { events.add("init") }
		return late, nil
	}
	user := lfNamed("user", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	app, logger := lfWatchApp(t)
	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())

	lfStop(t, app)

	if got := events.all(); len(got) != 0 {
		t.Error(test.DiffMessage(got, []string(nil), "A11: a module returned after the cancel is not wired"))
	}
	if got := lfStatus(app, user); got != modulePending {
		t.Error(test.DiffMessage(got.String(), "pending", "A11: no module changes state after the cancel"))
	}
	if n := len(logger.named("ModuleReady")); n != 0 {
		t.Error(test.DiffMessage(n, 0, "A11: no ModuleReady after the cancel"))
	}
}

func TestAsync_OnShutdownOnlyReadyReverseOrder(t *testing.T) {
	events := &lfEvents{}
	hook := func(name string) func() { return func() { events.add(name) } }
	a := lfNamed("a", NewModuleBuilder())
	b := lfNamed("b", NewModuleBuilder())
	ok := lfNamed("ok", NewModuleBuilder())
	bad := lfNamed("bad", NewModuleBuilder().Providers(lfBoomProvider{}))
	factory := func(c context.Context) (*Module, error) {
		<-c.Done()
		return nil, c.Err()
	}
	dep := lfNamed("dep", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	root := lfNamed("root", NewModuleBuilder().Imports(a, b, dep).AsyncImports(ok, bad))
	for name, m := range map[string]*Module{"a": a, "b": b, "ok": ok, "bad": bad, "dep": dep, "root": root} {
		m.OnShutdown = hook(name)
	}
	app, _ := lfWatchApp(t)
	lfCreate(t, app, root)
	lfWait(t, app.container.settled(ok), "ok")
	lfWait(t, app.container.settled(bad), "bad")

	lfStop(t, app)

	want := []string{"ok", "root", "b", "a"}
	if got := events.all(); !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "A12: only ready modules, the reverse of the order they became ready"))
	}
}

func TestAsync_StopFromOnReadyDoesNotDeadlock(t *testing.T) {
	ent := lfNamed("ent", NewModuleBuilder())
	stopped := make(chan struct{})
	app, _ := lfWatchApp(t)
	ent.OnReady = func() { app.Stop(); close(stopped) }
	lfCreate(t, app, NewModuleBuilder().AsyncImports(ent).Build())

	lfWait(t, stopped, "Stop to return inside OnReady")
}

type lfEntrySet struct {
	app      *App
	sql, rds *Module
	cch      *Module
	releases [3]chan struct{}
}

func lfNewEntrySet(t *testing.T) *lfEntrySet {
	t.Helper()
	s := &lfEntrySet{}
	s.sql = lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}).Controllers(lfSQLCtl{}))
	s.rds = lfNamed("redis", NewModuleBuilder().Providers(lfRedis{}).Controllers(lfRedisCtl{}))
	s.cch = lfNamed("cache", NewModuleBuilder().Providers(lfCache{}).Controllers(lfCacheCtl{}))
	for i, m := range []*Module{s.sql, s.rds, s.cch} {
		s.releases[i] = lfHold(m)
	}
	s.app = New(&AppOptions{Logger: newLFWatchLog(), IsDevtool: true})
	lfRelease(t, s.app, s.releases[:]...)
	lfCreate(t, s.app, NewModuleBuilder().AsyncImports(s.sql, s.rds, s.cch).Controllers(lfPlainCtl{}).Build())
	return s
}

func TestAsync_ConcurrentRequestsDuringSwitch_NoRace(t *testing.T) {
	s := lfNewEntrySet(t)
	expected := map[string]string{"/lfsql/hello": "ok", "/lfredis/hello": "redis", "/lfcache/hello": "cache", "/lfplain/hello": "plain"}

	var stop atomic.Bool
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				for path, body := range expected {
					w := createGet(s.app, path)
					switch w.Code {
					case http.StatusOK:
						if !strings.Contains(w.Body.String(), body) {
							t.Error(test.DiffMessage(w.Body.String(), body, "A4: a 200 carries the real handler's answer: "+path))
						}
					case http.StatusServiceUnavailable:
						if w.Body.String() != lfWant503JSON {
							t.Error(test.DiffMessage(w.Body.String(), lfWant503JSON, "G8: a 503 carries the gate's body: "+path))
						}
					default:
						t.Error(test.DiffMessage(w.Code, "200 or 503", "A4: "+path))
					}
				}
			}
		}()
	}

	for i, m := range []*Module{s.sql, s.rds, s.cch} {
		close(s.releases[i])
		lfWait(t, s.app.container.settled(m), m.Name)
	}
	stop.Store(true)
	wg.Wait()

	for path := range expected {
		if got := lfCode(s.app, path); got != http.StatusOK {
			t.Error(test.DiffMessage(got, http.StatusOK, "A4: every route is open at the end: "+path))
		}
	}
}

func TestAsync_ConcurrentGetAndDevtool_NoRace(t *testing.T) {
	s := lfNewEntrySet(t)

	var stop atomic.Bool
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				_ = s.app.Get(lfSQL{})
				_ = s.app.Get(lfRedis{})
			}
		}()
		go func() {
			defer wg.Done()
			for !stop.Load() {
				resp, err := s.app.devtool.GetConfiguration(context.Background(), nil)
				if err != nil || resp == nil {
					t.Error(test.DiffMessage(err, nil, "C3: the devtool snapshot is readable during the switch"))
					return
				}
			}
		}()
	}
	for i, m := range []*Module{s.sql, s.rds, s.cch} {
		close(s.releases[i])
		lfWait(t, s.app.container.settled(m), m.Name)
	}
	stop.Store(true)
	wg.Wait()

	if got := s.app.Get(lfSQL{}); got == nil {
		t.Error(test.DiffMessage(got, "a built provider", "G14: Get returns the provider once built"))
	}
}

func TestModuleLog_PendingReadyFailed(t *testing.T) {
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	release := lfHold(sql)
	user := lfNamed("user", NewModuleBuilder().AsyncImports(sql).Providers(lfUserSvc{}))
	badm := lfNamed("badm", NewModuleBuilder().Providers(lfBoomProvider{}))
	victim := lfNamed("victim", NewModuleBuilder().AsyncImports(badm).Providers(lfNeedsBoom{}))
	app, logger := lfWatchApp(t)
	lfRelease(t, app, release)
	lfCreate(t, app, NewModuleBuilder().Imports(user, victim).Build())

	pending := map[string]lcLogEntry{}
	for _, e := range logger.named("ModulePending") {
		pending[lcText(e.field("module"))] = e
		if e.level != "info" {
			t.Error(test.DiffMessage(e.level, "info", "ModulePending is an Info line"))
		}
	}
	if e, ok := pending["user"]; !ok || !strings.Contains(lcText(e.field("waiting_for")), "sql") {
		t.Error(test.DiffMessage(pending, "user waiting_for sql", "V: ModulePending names what the module waits for"))
	}

	close(release)
	logger.waitCount(t, "ModuleReady", 2)
	logger.waitCount(t, "ModuleFailed", 2)

	ready := map[string]lcLogEntry{}
	for _, e := range logger.named("ModuleReady") {
		ready[lcText(e.field("module"))] = e
		if _, ok := e.field("after").(time.Duration); !ok || e.level != "info" {
			t.Error(test.DiffMessage(e.field("after"), "a time.Duration", "V: ModuleReady has after, as an Info line"))
		}
	}
	if _, ok := ready["sql"]; !ok {
		t.Error(test.DiffMessage(ready, "sql", "V: ModuleReady for the entry"))
	}
	if _, ok := ready["user"]; !ok {
		t.Error(test.DiffMessage(ready, "user", "V: ModuleReady for the dependent"))
	}

	bad := lfFailedFor(logger.lcRecordingLogger, "badm")
	if len(bad) != 1 || bad[0].level != "error" || !strings.Contains(lfCause(bad[0]), "lf-provider-boom") ||
		bad[0].field("stack") == nil || bad[0].field("after") == nil {
		t.Error(test.DiffMessage(bad, "Error ModuleFailed with cause, after and stack", "V: a panic"))
	}
	dependent := lfFailedFor(logger.lcRecordingLogger, "victim")
	if len(dependent) != 1 || lfCause(dependent[0]) != "needs 'badm'" {
		t.Error(test.DiffMessage(dependent, "cause needs 'badm'", "V: a dependent names what it needs"))
	}
}

func lfSettledWithin(t *testing.T, ch <-chan struct{}, what string) bool {
	t.Helper()
	select {
	case <-ch:
		return true
	case <-time.After(time.Second):
		t.Error(test.DiffMessage("still pending", "settled", what))
		return false
	}
}

type lfMissingProvA struct{}

func (p lfMissingProvA) NewProvider() Provider { return p }

type lfMissingProvB struct{}

func (p lfMissingProvB) NewProvider() Provider { return p }

func TestAsync_UnknownParamOfOnlyFactoryFails(t *testing.T) {
	factory := func(m lfMissingProv) (*Module, error) { return lfNamed("never", NewModuleBuilder()), nil }
	app, logger := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().AsyncImports(factory).Build())

	if !lfSettledWithin(t, app.container.settledFactory(factory), "A7: the only factory's unknown parameter fails it") {
		return
	}
	if got := app.container.statusOfFactory(factory); got != moduleFailed {
		t.Error(test.DiffMessage(got.String(), "failed", "A7: the factory's entry is failed"))
	}
	want := "can't resolve " + reflect.TypeFor[lfMissingProv]().String()
	entries := logger.named("ModuleFailed")
	if len(entries) != 1 || lfCause(entries[0]) != want {
		t.Error(test.DiffMessage(entries, "one ModuleFailed with cause "+want, "A7"))
	}
}

func TestAsync_UnknownParamsOfTwoFactoriesFail(t *testing.T) {
	factoryA := func(m lfMissingProvA) (*Module, error) { return lfNamed("a", NewModuleBuilder()), nil }
	factoryB := func(m lfMissingProvB) (*Module, error) { return lfNamed("b", NewModuleBuilder()), nil }
	app, logger := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().AsyncImports(factoryA, factoryB).Build())

	okA := lfSettledWithin(t, app.container.settledFactory(factoryA), "A7: factory A fails")
	okB := lfSettledWithin(t, app.container.settledFactory(factoryB), "A7: factory B fails")
	if !okA || !okB {
		return
	}
	for name, fn := range map[string]any{"A": factoryA, "B": factoryB} {
		if got := app.container.statusOfFactory(fn); got != moduleFailed {
			t.Error(test.DiffMessage(got.String(), "failed", "A7: factory "+name+" is failed"))
		}
	}
	causes := []string{}
	for _, e := range logger.named("ModuleFailed") {
		causes = append(causes, lfCause(e))
	}
	slices.Sort(causes)
	want := []string{
		"can't resolve " + reflect.TypeFor[lfMissingProvA]().String(),
		"can't resolve " + reflect.TypeFor[lfMissingProvB]().String(),
	}
	slices.Sort(want)
	if !slices.Equal(causes, want) {
		t.Error(test.DiffMessage(causes, want, "A7: each factory fails with its own cause"))
	}
}

func TestAsync_ShutdownBetweenGateAndReadyNeverServes(t *testing.T) {
	var shutdowns atomic.Int32
	ent := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}).Controllers(lfSQLCtl{}))
	ent.OnShutdown = func() { shutdowns.Add(1) }
	app, _ := lfWatchApp(t)
	lfRelease(t, app)
	app.container.afterGateOpen = func() { app.beginAsyncShutdown() }
	lfCreate(t, app, NewModuleBuilder().AsyncImports(ent).Build())

	entry := app.container.entryByImport[importKey(ent)]
	lfWait(t, entry.done, "the entry to end")

	status := lfStatus(app, ent)
	code := lfCode(app, "/lfsql/hello")
	app.container.mu.RLock()
	inOrder := slices.Contains(app.container.readyOrder, ent)
	app.container.mu.RUnlock()
	lfStop(t, app)

	if code == http.StatusOK && status != moduleReady {
		t.Error(test.DiffMessage(status.String(), "ready", "A11: a route answered 200 while the module is not ready"))
	}
	if status == moduleReady && (!inOrder || shutdowns.Load() != 1) {
		t.Error(test.DiffMessage([]any{inOrder, shutdowns.Load()}, []any{true, int32(1)}, "A12: a ready module is in readyOrder and gets OnShutdown"))
	}
	if status != moduleReady && code == http.StatusOK {
		t.Error(test.DiffMessage(code, http.StatusServiceUnavailable, "A11: a module that never got ready keeps its gate closed"))
	}
}

type lfLateSpy struct {
	called atomic.Int32
}

func lfLateApp(t *testing.T, unitImportsHolder bool) (*App, *lfLateSpy, any) {
	t.Helper()
	spy := &lfLateSpy{}
	late := func(db lfSQL) (*Module, error) {
		spy.called.Add(1)
		_ = db
		return lfNamed("late", NewModuleBuilder()), nil
	}
	holder := lfNamed("holder", NewModuleBuilder().Providers(lfSQL{Ready: true}))
	unitBuilder := NewModuleBuilder().AsyncImports(late)
	if unitImportsHolder {
		unitBuilder = unitBuilder.Imports(holder)
	}
	unit := lfNamed("unit", unitBuilder)
	first := func() (*Module, error) { return unit, nil }
	app, _ := lfWatchApp(t)
	lfRelease(t, app)
	lfCreate(t, app, NewModuleBuilder().Imports(holder).AsyncImports(first).Build())
	return app, spy, first
}

func TestAsync_LateFactoryDoesNotSeeInvisibleProvider(t *testing.T) {
	app, spy, first := lfLateApp(t, false)

	lfSettledWithin(t, app.container.settledFactory(first), "S1: the unit settles")

	if n := spy.called.Load(); n != 0 {
		t.Error(test.DiffMessage(n, 0, "S1: a late factory is never given a provider its importer can't see"))
	}
	if got := app.container.statusOfFactory(first); got == moduleReady {
		t.Error(test.DiffMessage(got.String(), "failed", "S1: the unresolved parameter fails the unit"))
	}
}

func TestAsync_LateFactoryResolvesVisibleSiblingProvider(t *testing.T) {
	app, spy, first := lfLateApp(t, true)

	lfSettledWithin(t, app.container.settledFactory(first), "S1: the unit settles")

	if n := spy.called.Load(); n != 1 {
		t.Error(test.DiffMessage(n, 1, "S1: a visible provider is injected into the late factory"))
	}
	if got := app.container.statusOfFactory(first); got != moduleReady {
		t.Error(test.DiffMessage(got.String(), "ready", "S1: the unit becomes ready"))
	}
}
