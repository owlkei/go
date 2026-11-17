package core

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

type rvDep struct{ Value string }

func (p rvDep) NewProvider() Provider {
	p.Value = "built"
	return p
}

func rvGlobal() *Module {
	m := NewModuleBuilder().Providers(rvDep{}).Build()
	m.IsGlobal = true
	return m
}

func rvFuncModule(seen *createRecorder) func(d rvDep) *Module {
	return func(d rvDep) *Module {
		seen.add("func:" + d.Value)
		return NewModuleBuilder().Build()
	}
}

func rvCreate(t *testing.T, root *Module, rule string) {
	t.Helper()
	if text, panicked := createPanicText(func() { New().Create(root) }); panicked {
		t.Fatal(test.DiffMessage(text, "no panic", rule))
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

var rvOrder createRecorder

type rvOrderDep struct{}

func (p rvOrderDep) NewProvider() Provider {
	rvOrder.add("provider")
	return p
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

func rvRelease(t *testing.T, release chan struct{}) {
	t.Helper()
	select {
	case <-release:
	default:
		close(release)
	}
}

func rvStopWaitsForHooks(t *testing.T, app *App, entered, release chan struct{}, finished *atomic.Bool, rule string) {
	t.Helper()
	defer rvRelease(t, release)

	first := make(chan struct{})
	go func() {
		app.Stop()
		close(first)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal(test.DiffMessage("timeout", "hook entered", rule))
	}

	secondFinished := make(chan bool, 1)
	go func() {
		app.Stop()
		secondFinished <- finished.Load()
	}()

	select {
	case done := <-secondFinished:
		t.Error(test.DiffMessage(done, false, rule+": a second Stop returned while a hook was still running"))
	case <-time.After(100 * time.Millisecond):
	}

	rvRelease(t, release)
	select {
	case done := <-secondFinished:
		if !done {
			t.Error(test.DiffMessage(done, true, rule+": the second Stop returns only after the hooks finished"))
		}
	case <-time.After(2 * time.Second):
		t.Error(test.DiffMessage("timeout", "second Stop returns", rule))
	}
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Error(test.DiffMessage("timeout", "first Stop returns", rule))
	}
}

func TestStop_SecondCallerWaitsWhileHooksRun_NotServing(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool
	m := NewModuleBuilder().Build()
	m.OnShutdown = func() {
		close(entered)
		<-release
		finished.Store(true)
	}
	app := New()
	mustCreate(t, app, m)

	rvStopWaitsForHooks(t, app, entered, release, &finished, "G6/H2")
}

func TestStop_SecondCallerWaitsWhileHooksRun_Serving(t *testing.T) {
	h := newSrv(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool
	srvCur.mu.Lock()
	srvCur.childExtra = func() {
		close(entered)
		<-release
		finished.Store(true)
	}
	srvCur.mu.Unlock()
	h.start(nil)

	rvStopWaitsForHooks(t, h.app, entered, release, &finished, "G6/H2")
	_ = h.wait()
}

func TestStop_FromInsideShutdownHookReturnsAtOnce(t *testing.T) {
	returned := make(chan struct{})
	app := New()
	m := NewModuleBuilder().Build()
	m.OnShutdown = func() {
		app.Stop()
		close(returned)
	}
	mustCreate(t, app, m)

	go app.Stop()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Error(test.DiffMessage("timeout", "Stop returns", "G6: Stop called from inside an OnShutdown hook must not deadlock"))
	}
}

func TestStop_FromInsideShutdownHookReturnsAtOnce_Serving(t *testing.T) {
	h := newSrv(t)
	returned := make(chan struct{})
	srvCur.mu.Lock()
	srvCur.childExtra = func() {
		h.app.Stop()
		close(returned)
	}
	srvCur.mu.Unlock()
	h.start(nil)

	go h.app.Stop()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Error(test.DiffMessage("timeout", "Stop returns", "G6: Stop called from inside an OnShutdown hook must not deadlock while serving"))
	}
	_ = h.wait()
}
