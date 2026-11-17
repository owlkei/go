package core

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"github.com/dangduoc08/ginject/log"
)

type coreHoldWriter struct {
	mu      sync.Mutex
	gate    chan struct{}
	entered chan struct{}
	buf     bytes.Buffer
}

func (w *coreHoldWriter) hold() {
	w.mu.Lock()
	if w.gate == nil {
		w.gate = make(chan struct{})
	}
	w.mu.Unlock()
}

func (w *coreHoldWriter) openAll() {
	w.mu.Lock()
	if w.gate != nil {
		close(w.gate)
		w.gate = nil
	}
	w.mu.Unlock()
}

func (w *coreHoldWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	gate := w.gate
	w.mu.Unlock()
	select {
	case w.entered <- struct{}{}:
	default:
	}
	if gate != nil {
		<-gate
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *coreHoldWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func redirectStdout(t *testing.T) *coreHoldWriter {
	t.Helper()
	w := &coreHoldWriter{entered: make(chan struct{}, 1<<12)}
	restore := logpipe.SetStdout(w)
	t.Cleanup(func() {
		w.openAll()
		restore()
	})
	return w
}

func withinCore(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal(test.DiffMessage("blocked for 30s", "returned", what))
	}
}

func scopedContext() context.Context {
	return tracecontext.With(context.Background(), &tracecontext.Scope{TraceID: scopeTraceID, Request: "00f067aa0ba902b7"})
}

type fromProvider struct{ Log log.Logger }

var fromProviderSeen log.Logger

func (p fromProvider) NewProvider() Provider {
	fromProviderSeen = p.Log
	return p
}

func TestSetLogger_AcceptsFiveMethodAdapter(t *testing.T) {
	var adapter log.LogAdapter = &createLogger{}
	rec := adapter.(*createLogger)
	app := New()
	if got := app.SetLogger(adapter); got != app {
		t.Error(test.DiffMessage(got, app, "I1: SetLogger accepts a log.LogAdapter and returns the app"))
	}
	mustCreate(t, app, NewModuleBuilder().Build())
	app.Logger().Info("hello", "k", "v")
	if len(rec.named("hello")) != 1 {
		t.Error(test.DiffMessage(len(rec.named("hello")), 1, "I1: a type with five methods and no From works as the app's logger"))
	}
}

func TestSetLogger_NilStillPanics(t *testing.T) {
	var adapter log.LogAdapter
	text, panicked := createPanicText(func() { New().SetLogger(adapter) })
	if !panicked {
		t.Error(test.DiffMessage(text, "a panic", "I1: SetLogger(nil) panics as today"))
	}
}

func TestLogger_InjectedHasFrom(t *testing.T) {
	rec := &createLogger{}
	app := New()
	app.SetLogger(rec)
	fromProviderSeen = nil
	mustCreate(t, app, NewModuleBuilder().Providers(fromProvider{}).Build())
	if fromProviderSeen == nil {
		t.Fatal(test.DiffMessage(nil, "an injected logger", "I2: a log.Logger field is injected"))
	}
	fromProviderSeen.From(scopedContext()).Info("scoped", "k", 1)
	app.Logger().From(scopedContext()).Info("scoped-app", "k", 1)
	want := []any{"trace_id", scopeTraceID, "request", "00f067aa0ba902b7", "k", 1}
	for _, name := range []string{"scoped", "scoped-app"} {
		calls := rec.named(name)
		if len(calls) != 1 || !slices.Equal(calls[0].args, want) {
			t.Error(test.DiffMessage(calls, want, "I2: the injected logger and app.Logger() implement From whatever was given to SetLogger, "+name))
		}
	}
}

func TestLogger_BuiltinAppLoggerHasFrom(t *testing.T) {
	w := redirectStdout(t)
	app := New()
	mustCreate(t, app, NewModuleBuilder().Build())
	app.Logger().From(scopedContext()).Info("scoped-builtin", "k", 1)
	app.Stop()
	if out := w.String(); !strings.Contains(out, "scoped-builtin") || !strings.Contains(out, scopeTraceID) {
		t.Error(test.DiffMessage(out, "a line with the trace ID", "I2: the default logger implements From"))
	}
}

func TestSetLogger_AdapterCalledDirectly(t *testing.T) {
	w := redirectStdout(t)
	w.hold()
	rec := &createLogger{}
	app := New().SetLogger(rec)
	mustCreate(t, app, NewModuleBuilder().Build())
	defer app.Stop()
	defer w.openAll()
	app.Logger().Info("direct")
	if len(rec.named("direct")) != 1 {
		t.Error(test.DiffMessage(len(rec.named("direct")), 1, "O4: an adapter is called on the caller, before the call returns, in every mode"))
	}
}

func TestCreate_BootstrapLinesSynchronous(t *testing.T) {
	w := redirectStdout(t)
	app := New()
	var seen string
	root := NewModuleBuilder().Build()
	root.OnReady = func() {
		app.Logger().Info("from-ready")
		seen = w.String()
	}
	mustCreate(t, app, root)
	defer app.Stop()
	if !strings.Contains(seen, "from-ready") {
		t.Error(test.DiffMessage(seen, "the line already written", "M1: before Create's last step every line is synchronous, OnReady hooks included"))
	}
}

type bootstrapPanicProvider struct{ Log log.Logger }

func (p bootstrapPanicProvider) NewProvider() Provider {
	p.Log.Info("before-bootstrap-panic")
	panic("provider exploded")
}

func TestCreate_BootstrapPanicLosesNoLine(t *testing.T) {
	w := redirectStdout(t)
	app := New()
	_, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().Providers(bootstrapPanicProvider{}).Build())
	})
	if !panicked {
		t.Fatal(test.DiffMessage(false, true, "setup: the provider panics"))
	}
	if !strings.Contains(w.String(), "before-bootstrap-panic") {
		t.Error(test.DiffMessage(w.String(), "the line logged before the panic", "M1: a bootstrap panic loses nothing"))
	}
}

func TestCreate_SwitchesToAsync(t *testing.T) {
	w := redirectStdout(t)
	app := New()
	mustCreate(t, app, NewModuleBuilder().Build())
	w.hold()
	withinCore(t, "M1: after Create a log call does not wait for stdout", func() {
		for i := range 10 {
			app.Logger().Info("after-create", "i", i)
		}
	})
	if w.String() != "" {
		t.Error(test.DiffMessage(w.String(), "", "M1: the lines are queued, not written yet"))
	}
	w.openAll()
	app.Stop()
	if got := strings.Count(w.String(), "after-create"); got != 10 {
		t.Error(test.DiffMessage(got, 10, "M2: Stop flushes the queued lines"))
	}
}

func openWhenEntered(w *coreHoldWriter) {
	go func() {
		<-w.entered
		w.openAll()
	}()
}

func TestShutdown_FlushesAfterHooks(t *testing.T) {
	w := redirectStdout(t)
	app := New()
	root := NewModuleBuilder().Build()
	root.OnShutdown = func() { app.Logger().Info("from-shutdown-hook") }
	mustCreate(t, app, root)
	w.hold()
	openWhenEntered(w)
	app.Stop()
	if !strings.Contains(w.String(), "from-shutdown-hook") {
		t.Error(test.DiffMessage(w.String(), "the hook's line written when Stop returns", "M2: the sink is flushed after the OnShutdown hooks"))
	}
}

func TestStop_WithoutServeFlushes(t *testing.T) {
	w := redirectStdout(t)
	app := New()
	mustCreate(t, app, NewModuleBuilder().Build())
	w.hold()
	app.Logger().Info("embedded")
	openWhenEntered(w)
	app.Stop()
	if !strings.Contains(w.String(), "embedded") {
		t.Error(test.DiffMessage(w.String(), "the line written when Stop returns", "X4: Stop on an app that never served flushes"))
	}
}

func TestShutdown_LinesAfterStopSynchronous(t *testing.T) {
	w := redirectStdout(t)
	app := New()
	mustCreate(t, app, NewModuleBuilder().Build())
	app.Stop()
	for i := range 20 {
		line := "after-stop-" + strconv.Itoa(i)
		app.Logger().Info(line)
		if !strings.Contains(w.String(), line) {
			t.Fatal(test.DiffMessage(w.String(), line, "M2: lines logged after Stop returned are synchronous"))
		}
	}
}

func TestShutdown_AsyncCountedPerApp(t *testing.T) {
	w := redirectStdout(t)
	first, second := New(), New()
	mustCreate(t, first, NewModuleBuilder().Build())
	mustCreate(t, second, NewModuleBuilder().Build())
	first.Stop()
	w.hold()
	withinCore(t, "M3: still asynchronous while one of two apps runs", func() {
		first.Logger().Info("one-app-left")
	})
	w.openAll()
	second.Stop()
	first.Logger().Info("none-left")
	if !strings.Contains(w.String(), "none-left") || !strings.Contains(w.String(), "one-app-left") {
		t.Error(test.DiffMessage(w.String(), "both lines written", "M3: after both apps shut down the sink is synchronous and nothing is lost"))
	}
}

var panicSites = [...]func(){
	func() { panic("site 0") },
	func() { panic("site 1") },
	func() { panic("site 2") },
	func() { panic("site 3") },
	func() { panic("site 4") },
	func() { panic("site 5") },
	func() { panic("site 6") },
	func() { panic("site 7") },
	func() { panic("site 8") },
	func() { panic("site 9") },
	func() { panic("site 10") },
	func() { panic("site 11") },
}

func sampleHandler(c *ctx.HTTPContext) any {
	n, _ := strconv.Atoi(c.URL.Query().Get("s"))
	panicSites[n]()
	return nil
}

type fakeStackTimer struct {
	d       time.Duration
	f       func()
	stopped bool
}

type fakeStackClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeStackTimer
}

func installStackSeams(t *testing.T) *fakeStackClock {
	t.Helper()
	c := &fakeStackClock{now: time.Unix(1_000_000, 250_000_000)}
	savedNow, savedAfter := stackNow, stackAfter
	stackNow = func() time.Time {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.now
	}
	stackAfter = func(d time.Duration, f func()) func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		timer := &fakeStackTimer{d: d, f: f}
		c.timers = append(c.timers, timer)
		return func() bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			was := !timer.stopped
			timer.stopped = true
			return was
		}
	}
	t.Cleanup(func() { stackNow, stackAfter = savedNow, savedAfter })
	return c
}

func (c *fakeStackClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *fakeStackClock) armed() []*fakeStackTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.timers)
}

func sampleApp(t *testing.T) (*App, *lcRecordingLogger) {
	t.Helper()
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{noGlobalFilters: true, logger: logger})
	lcSetHandler(sampleHandler)
	return app, logger
}

func serveFresh(app *App, target string) *httptest.ResponseRecorder {
	var w *httptest.ResponseRecorder
	done := make(chan struct{})
	go func() {
		defer close(done)
		w = lcServe(app, http.MethodGet, target)
	}()
	<-done
	return w
}

func panicAt(app *App, site int) *httptest.ResponseRecorder {
	return serveFresh(app, "/bare?s="+strconv.Itoa(site))
}

func hasKey(args []any, key string) bool {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key {
			return true
		}
	}
	return false
}

func keysOf(args []any) []string {
	var keys []string
	for i := 0; i+1 < len(args); i += 2 {
		keys = append(keys, args[i].(string))
	}
	return keys
}

func stackCount(entries []lcLogEntry) int {
	n := 0
	for _, e := range entries {
		if hasKey(e.args, "stack") {
			n++
		}
	}
	return n
}

func TestStackSampler_FirstOfKeyHasStack(t *testing.T) {
	installStackSeams(t)
	app, logger := sampleApp(t)
	w := panicAt(app, 0)
	entries := logger.named("UnhandledPanic")
	if len(entries) != 1 || !hasKey(entries[0].args, "stack") {
		t.Fatal(test.DiffMessage(entries, "one entry with stack", "S1: the first panic of a key in a second is logged with its stack"))
	}
	if st := lcText(entries[0].field("stack")); st == "" || len(st) > 4096 {
		t.Error(test.DiffMessage(len(st), "1..4096", "S1: the stack is logged as today, at most 4 KiB"))
	}
	lcExpectResponse(t, w, http.StatusInternalServerError, wantInternalErrorJSON, "S1: the response is unchanged")
}

func TestStackSampler_SameKeySameSecondNoStack(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	for range 3 {
		clock.advance(100 * time.Millisecond)
		panicAt(app, 4)
	}
	entries := logger.named("UnhandledPanic")
	if len(entries) != 3 {
		t.Fatal(test.DiffMessage(len(entries), 3, "S2: every panic is still logged"))
	}
	if !hasKey(entries[0].args, "stack") || hasKey(entries[1].args, "stack") || hasKey(entries[2].args, "stack") {
		t.Error(test.DiffMessage(stackCount(entries), 1, "S2: further panics of the key in the same second are logged without stack"))
	}
	want := slices.DeleteFunc(keysOf(entries[0].args), func(k string) bool { return k == "stack" })
	for _, e := range entries[1:] {
		if got := keysOf(e.args); !slices.Equal(got, want) || e.field("panic") != entries[0].field("panic") {
			t.Error(test.DiffMessage(got, want, "S2: every pair except stack is kept"))
		}
	}
}

func TestStackSampler_NewSecondAllowsAgain(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	panicAt(app, 2)
	clock.advance(700 * time.Millisecond)
	panicAt(app, 2)
	clock.advance(300 * time.Millisecond)
	panicAt(app, 2)
	clock.advance(300 * time.Millisecond)
	panicAt(app, 2)
	entries := logger.named("UnhandledPanic")
	got := []bool{hasKey(entries[0].args, "stack"), hasKey(entries[1].args, "stack"), hasKey(entries[2].args, "stack"), hasKey(entries[3].args, "stack")}
	if want := []bool{true, false, true, false}; !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "S1: seconds are Unix seconds, each second allows one stack per key"))
	}
}

func TestStackSampler_GlobalCap(t *testing.T) {
	installStackSeams(t)
	app, logger := sampleApp(t)
	for site := range 12 {
		panicAt(app, site)
	}
	entries := logger.named("UnhandledPanic")
	if len(entries) != 12 || stackCount(entries) != 10 {
		t.Fatal(test.DiffMessage([]int{len(entries), stackCount(entries)}, []int{12, 10}, "S3: never more than 10 records with stack per app per second, whatever the number of keys"))
	}
	if hasKey(entries[10].args, "stack") || hasKey(entries[11].args, "stack") {
		t.Error(test.DiffMessage(stackCount(entries[10:]), 0, "S2: past the cap the panic is logged without stack"))
	}
}

func TestStackSampler_RareKeyKeepsItsStackDuringFlood(t *testing.T) {
	installStackSeams(t)
	app, logger := sampleApp(t)
	for range 200 {
		panicAt(app, 0)
	}
	panicAt(app, 7)
	entries := logger.named("UnhandledPanic")
	if len(entries) != 201 || stackCount(entries) != 2 || !hasKey(entries[200].args, "stack") {
		t.Error(test.DiffMessage([]int{len(entries), stackCount(entries)}, []int{201, 2}, "S1: a flood of one key uses one stack, so a rarer panic keeps its own"))
	}
}

func TestStackSampler_TableBounded(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	const seconds = 100
	for range seconds {
		for site := range 12 {
			panicAt(app, site)
		}
		clock.advance(time.Second)
	}
	entries := logger.named("UnhandledPanic")
	if len(entries) != seconds*12 || stackCount(entries) != seconds*10 {
		t.Error(test.DiffMessage([]int{len(entries), stackCount(entries)}, []int{seconds * 12, seconds * 10}, "S3, S4: a fixed table and a per-second budget: every second logs 10 stacks, whatever happened before"))
	}
}

func TestStackSampler_AppsIndependent(t *testing.T) {
	installStackSeams(t)
	appA, loggerA := sampleApp(t)
	for site := range 12 {
		panicAt(appA, site)
	}
	appB, loggerB := sampleApp(t)
	panicAt(appB, 0)
	if stackCount(loggerA.named("UnhandledPanic")) != 10 {
		t.Error(test.DiffMessage(stackCount(loggerA.named("UnhandledPanic")), 10, "S3: the first app used its budget"))
	}
	if got := loggerB.named("UnhandledPanic"); len(got) != 1 || !hasKey(got[0].args, "stack") {
		t.Error(test.DiffMessage(got, "one entry with stack", "S3: two apps do not share a budget or a key table"))
	}
}

func TestStackSampler_ConcurrentNoRace(t *testing.T) {
	installStackSeams(t)
	app, logger := sampleApp(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := range 20 {
				panicAt(app, (g+i)%12)
			}
		}()
	}
	close(start)
	wg.Wait()
	entries := logger.named("UnhandledPanic")
	if len(entries) != 320 {
		t.Fatal(test.DiffMessage(len(entries), 320, "S2: every panic is logged"))
	}
	if n := stackCount(entries); n > 10 || n < 1 {
		t.Error(test.DiffMessage(n, "1..10", "S3: at most 10 stacks per second under concurrent panics"))
	}
}

func TestStackSampler_SuppressedLinePerSecond(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	panicAt(app, 0)
	if len(clock.armed()) != 0 {
		t.Fatal(test.DiffMessage(len(clock.armed()), 0, "S5: no timer without a suppression"))
	}
	panicAt(app, 0)
	panicAt(app, 0)
	timers := clock.armed()
	if len(timers) != 1 {
		t.Fatal(test.DiffMessage(len(timers), 1, "S5: the first suppression arms one timer, the next ones none"))
	}
	if d := timers[0].d; d <= 0 || d > time.Second {
		t.Error(test.DiffMessage(d, "(0, 1s]", "S5: the timer fires when the second ends"))
	}
	if len(logger.named("PanicStacksSuppressed")) != 0 {
		t.Error(test.DiffMessage(len(logger.named("PanicStacksSuppressed")), 0, "S5: the record comes when the second ends"))
	}
	timers[0].f()
	got := logger.named("PanicStacksSuppressed")
	if len(got) != 1 || got[0].level != "warn" || fmt.Sprint(got[0].field("stacks_suppressed")) != "2" {
		t.Fatal(test.DiffMessage(got, "one WARN with stacks_suppressed 2", "S5: one record per second with the count"))
	}

	clock.advance(time.Second)
	panicAt(app, 0)
	panicAt(app, 0)
	timers = clock.armed()
	if len(timers) != 2 {
		t.Fatal(test.DiffMessage(len(timers), 2, "S5: a new second with a suppression arms a new timer"))
	}
	timers[1].f()
	got = logger.named("PanicStacksSuppressed")
	if len(got) != 2 || fmt.Sprint(got[1].field("stacks_suppressed")) != "1" {
		t.Error(test.DiffMessage(got, "a second record with 1", "S5: the count restarts each second"))
	}
}

func TestStackSampler_CapSuppressionsAreCounted(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	for site := range 12 {
		panicAt(app, site)
	}
	timers := clock.armed()
	if len(timers) != 1 {
		t.Fatal(test.DiffMessage(len(timers), 1, "S5: suppressions past the cap arm the timer too"))
	}
	timers[0].f()
	got := logger.named("PanicStacksSuppressed")
	if len(got) != 1 || fmt.Sprint(got[0].field("stacks_suppressed")) != "2" {
		t.Error(test.DiffMessage(got, "stacks_suppressed 2", "S5: stacks suppressed by the cap are counted"))
	}
}

func TestStackSampler_NoTimerWithoutSuppression(t *testing.T) {
	clock := installStackSeams(t)
	app, _ := sampleApp(t)
	for site := range 5 {
		panicAt(app, site)
	}
	if n := len(clock.armed()); n != 0 {
		t.Error(test.DiffMessage(n, 0, "S5: no suppression, no timer"))
	}
}

func TestShutdown_ReportsSuppressedStacks(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	panicAt(app, 0)
	panicAt(app, 0)
	panicAt(app, 0)
	if len(logger.named("PanicStacksSuppressed")) != 0 {
		t.Fatal(test.DiffMessage(1, 0, "setup: nothing reported yet"))
	}
	app.Stop()
	got := logger.named("PanicStacksSuppressed")
	if len(got) != 1 || fmt.Sprint(got[0].field("stacks_suppressed")) != "2" {
		t.Fatal(test.DiffMessage(got, "stacks_suppressed 2 at once", "S6: shutdown reports a pending count at once"))
	}
	timers := clock.armed()
	if len(timers) != 1 || !timers[0].stopped {
		t.Error(test.DiffMessage(timers, "the timer stopped", "S6: shutdown stops the timer"))
	}
	timers[0].f()
	if n := len(logger.named("PanicStacksSuppressed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "S6: nothing is reported twice"))
	}
}

func TestPanicLine_CarriesScopeFields(t *testing.T) {
	installStackSeams(t)
	app, logger := sampleApp(t)
	w := serveWithHeader(app, http.MethodGet, "/bare?s=1", http.Header{"Traceparent": {scopeHeader}})
	entries := logger.named("UnhandledPanic")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "S7: one UnhandledPanic line"))
	}
	e := entries[0]
	want := []string{"trace_id", "request", "upstream", "method", "path", "panic", "stack"}
	if got := keysOf(e.args); !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "S7: the scope's fields come first, then method, path, panic, stack, and no id"))
	}
	if e.field("trace_id") != scopeTraceID || e.field("request") != w.Header().Get("X-Request-Id") || e.field("upstream") != scopeParentID {
		t.Error(test.DiffMessage(e.args[:6], []any{"trace_id", scopeTraceID, "request", w.Header().Get("X-Request-Id"), "upstream", scopeParentID}, "S7: the fields are the request's scope"))
	}

	plainApp, plainLogger := sampleApp(t)
	panicAt(plainApp, 1)
	got := plainLogger.named("UnhandledPanic")[0]
	if keys := keysOf(got.args); !slices.Equal(keys, []string{"trace_id", "request", "method", "path", "panic", "stack"}) {
		t.Error(test.DiffMessage(keys, "trace_id request method path panic stack", "S7: without an incoming parent there is no upstream field"))
	}
}

func TestErrorAfterResponse_Sampled(t *testing.T) {
	clock := installStackSeams(t)
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{noGlobalFilters: true, logger: logger})
	lcSetHandler(func(c *ctx.HTTPContext) any {
		c.Text("first")
		panic("late")
	})
	w := serveFresh(app, "/bare")
	serveFresh(app, "/bare")
	lcExpectResponse(t, w, http.StatusOK, "first", "S7: the response stays")
	entries := logger.named("ErrorAfterResponse")
	if len(entries) != 2 || !hasKey(entries[0].args, "stack") || hasKey(entries[1].args, "stack") {
		t.Fatal(test.DiffMessage(stackCount(entries), 1, "S7: ErrorAfterResponse is sampled like UnhandledPanic"))
	}
	want := []string{"trace_id", "request", "method", "path", "code", "panic", "stack"}
	if got := keysOf(entries[0].args); !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "S7: ErrorAfterResponse goes through From and no longer passes id"))
	}
	if len(clock.armed()) != 1 {
		t.Error(test.DiffMessage(len(clock.armed()), 1, "S5: the suppressed ErrorAfterResponse arms the timer"))
	}
}
