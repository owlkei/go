package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/trace"
)

type traceRecorder struct {
	mu     sync.Mutex
	events []trace.Event
	ch     chan trace.Event
}

func newTraceRecorder(app *App) *traceRecorder {
	r := &traceRecorder{ch: make(chan trace.Event, 256)}
	app.tracer.Listen(func(e trace.Event) {
		r.mu.Lock()
		r.events = append(r.events, e)
		r.mu.Unlock()
		select {
		case r.ch <- e:
		default:
		}
	})
	return r
}

func (r *traceRecorder) all() []trace.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]trace.Event(nil), r.events...)
}

func (r *traceRecorder) stages() map[string]bool {
	seen := map[string]bool{}
	for _, e := range r.all() {
		seen[e.Stage] = true
	}
	return seen
}

func (r *traceRecorder) waitComplete(t *testing.T, target string) trace.Event {
	t.Helper()
	for {
		select {
		case e := <-r.ch:
			if e.Stage == trace.StageComplete && e.Target == target {
				return e
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("no complete event for %q", target)
		}
	}
}

func TestTrace_EveryEventCarriesTransport(t *testing.T) {
	var rec *traceRecorder
	app := lcNewApp(t, lcConfig{setup: func(app *App) { rec = newTraceRecorder(app) }})

	lcServe(app, http.MethodGet, "/ok")
	lcSetHandler(func(*ctx.HTTPContext) any { panic(exception.BadRequestException("boom")) })
	lcServe(app, http.MethodGet, "/ok")
	lcSetHandler(nil)
	lcServe(app, http.MethodGet, "/pipe")
	lcServe(app, http.MethodGet, "/missing")

	want := []string{
		trace.StageMiddleware, trace.StageGuard, trace.StagePreInterceptor, trace.StagePostInterceptor,
		trace.StageHandler, trace.StageExceptionFilter, trace.StagePipe, trace.StageComplete,
	}
	seen := rec.stages()
	for _, stage := range want {
		if !seen[stage] {
			t.Errorf("T7: no %q event was emitted, stages seen: %v", stage, seen)
		}
	}
	for _, e := range rec.all() {
		if e.Transport != trace.TransportHTTP {
			t.Error(test.DiffMessage(e.Transport, trace.TransportHTTP, "T7: the "+e.Stage+" event ("+e.Name+") carries its Transport"))
		}
		if e.ID == "" {
			t.Error(test.DiffMessage(e.ID, "<non-empty>", "T7: the "+e.Stage+" event carries an ID"))
		}
	}
}

type traceWSAllGuard struct{}

func (traceWSAllGuard) CanActivate(*ctx.WSContext) bool { return true }

type traceWSAllInterceptor struct{}

func (traceWSAllInterceptor) Intercept(_ *ctx.WSContext, agg *aggregation.Aggregation) any {
	return agg.Pipe()
}

type traceWSAllPipe struct{}

func (traceWSAllPipe) Transform(ctx.WSPayload, common.ArgumentMetadata) any { return traceWSAllPipe{} }

type traceWSAllController struct {
	common.WS
	common.Guard
	common.Interceptor
}

func (c traceWSAllController) NewController() Controller {
	c.BindGuard(traceWSAllGuard{})
	c.BindInterceptor(traceWSAllInterceptor{})
	return c
}

func (c traceWSAllController) SUBSCRIBE_traced(traceWSAllPipe) string { return "ok" }
func (c traceWSAllController) SUBSCRIBE_boom() string {
	panic(exception.BadRequestException("boom"))
}

func TestTrace_EveryWSEventCarriesTransport(t *testing.T) {
	app := New()
	rec := newTraceRecorder(app)
	app.EnableWS(&WSConfig{Path: "/ws"})
	app.Create(ModuleBuilder().Controllers(traceWSAllController{}).Build())

	server := httptest.NewServer(app)
	defer server.Close()
	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	var reply WSPayload
	if err := websocket.JSON.Receive(conn, &reply); err != nil {
		t.Fatal(err)
	}
	for i, topic := range []string{"traced", "boom"} {
		if err := websocket.JSON.Send(conn, WSPayload{Type: TypeSubscribe, ID: "s" + string(rune('0'+i)), Topic: []string{topic}}); err != nil {
			t.Fatal(err)
		}
		if err := websocket.JSON.Receive(conn, &reply); err != nil {
			t.Fatal(err)
		}
		rec.waitComplete(t, topic)
		if err := websocket.JSON.Send(conn, WSPayload{Type: TypePublish, ID: "p" + string(rune('0'+i)), Topic: []string{topic}, Message: "hi"}); err != nil {
			t.Fatal(err)
		}
		rec.waitComplete(t, topic)
	}

	want := []string{
		trace.StageGuard, trace.StagePreInterceptor, trace.StagePostInterceptor,
		trace.StageExceptionFilter, trace.StagePipe, trace.StageComplete,
	}
	seen := rec.stages()
	for _, stage := range want {
		if !seen[stage] {
			t.Errorf("T7: no WS %q event was emitted, stages seen: %v", stage, seen)
		}
	}
	for _, e := range rec.all() {
		if e.Stage == trace.StageComplete && e.Operation == http.MethodGet {
			continue
		}
		if e.Transport != trace.TransportWS {
			t.Error(test.DiffMessage(e.Transport, trace.TransportWS, "T7: the WS "+e.Stage+" event ("+e.Name+") carries its Transport"))
		}
	}
}

func TestTrace_WSHandlerPlanEventsCarryTransport(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	planWS(t, &planWSCtl{}, "WPipe", nil).invoke(planWSContext(), ev)
	got := log.all()
	if len(got) == 0 {
		t.Fatal(test.DiffMessage(0, "> 0", "T7: the WS plan emits its pipe event"))
	}
	for _, e := range got {
		if e.Transport != trace.TransportWS {
			t.Error(test.DiffMessage(e.Transport, trace.TransportWS, "T7: the WS "+e.Stage+" event carries its Transport"))
		}
	}
	if n := len(log.byStage(trace.StageHandler)); n != 0 {
		t.Error(test.DiffMessage(n, 0, "B15: a WS handler emits no handler event"))
	}
}

func TestTrace_CompleteTargetHasNoQuery(t *testing.T) {
	var rec *traceRecorder
	app := lcNewApp(t, lcConfig{setup: func(app *App) { rec = newTraceRecorder(app) }})
	lcServe(app, http.MethodGet, "/ok?token=secret&a=1")

	var complete *trace.Event
	for _, e := range rec.all() {
		if e.Stage == trace.StageComplete {
			complete = &e
		}
	}
	if complete == nil {
		t.Fatal(test.DiffMessage(nil, "a complete event", "T7: one complete event per request"))
	}
	if complete.Target != "/ok" || complete.Operation != http.MethodGet || complete.Code != http.StatusOK {
		t.Error(test.DiffMessage([]any{complete.Target, complete.Operation, complete.Code}, []any{"/ok", "GET", 200}, "T7: Target is the path without the query string"))
	}
	if strings.Contains(complete.Target, "secret") {
		t.Error(test.DiffMessage(complete.Target, "no query", "T7/Security: the query never reaches the trace"))
	}
}

func TestTrace_NoListenerNoEvents(t *testing.T) {
	app := New()
	var seen int
	stop := app.tracer.Listen(func(trace.Event) { seen++ })
	stop()
	if app.tracer.Enabled() {
		t.Fatal(test.DiffMessage(true, false, "setup: no listener"))
	}
	app.Create(ModuleBuilder().Controllers(tracePipelineController{}).Build())

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tracepipeline", nil))
	if w.Body.String() != "ok" || seen != 0 {
		t.Error(test.DiffMessage(seen, 0, "T7: nothing is emitted when there is no listener, and the request still succeeds"))
	}
}

func TestTrace_ListenerAddedMidRequestSeesNothingOfIt(t *testing.T) {
	app := lcNewApp(t, lcConfig{})
	var seen []trace.Event
	lcSetHandler(func(*ctx.HTTPContext) any {
		app.tracer.Listen(func(e trace.Event) { seen = append(seen, e) })
		return "H"
	})
	lcServe(app, http.MethodGet, "/ok")
	if len(seen) != 0 {
		t.Error(test.DiffMessage(seen, "no event", "T7: Enabled is asked once per request, a request that started untraced emits nothing"))
	}
	lcSetHandler(nil)
	lcServe(app, http.MethodGet, "/ok")
	if len(seen) == 0 {
		t.Error(test.DiffMessage(0, "> 0", "T7: the next request is traced"))
	}
}

func TestTrace_ConcurrentRequestsWithListener_NoRace(t *testing.T) {
	app := New()
	var mu sync.Mutex
	counts := map[string]int{}
	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		counts[e.ID]++
		mu.Unlock()
	})
	app.Create(ModuleBuilder().Controllers(tracePipelineController{}).Build())

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tracepipeline", nil))
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(counts) != 400 {
		t.Error(test.DiffMessage(len(counts), 400, "T5: every request is traced under its own ID"))
	}
}

func TestShutdown_AccessLogLinesFlushedAfterHooks(t *testing.T) {
	rec := &lcRecordingLogger{}
	app := New().SetLogger(rec).EnableAccessLog()
	root := ModuleBuilder().Controllers(tracePipelineController{}).Build()
	root.OnShutdown = func() {
		app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tracepipeline?hook=1", nil))
	}
	app.Create(root)

	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tracepipeline", nil))
	app.Stop()

	var infos []lcLogEntry
	for _, e := range rec.named("/tracepipeline") {
		if e.level == "info" {
			infos = append(infos, e)
		}
	}
	if len(infos) != 2 {
		t.Fatal(test.DiffMessage(len(infos), 2, "A10: the request before Stop and the one made by an OnShutdown hook are both logged once Stop returned"))
	}
	if infos[0].field("code") != 200 || infos[0].field("operation") != "GET" || infos[0].field("transport") != "HTTP" {
		t.Error(test.DiffMessage(infos[0].args, "code 200, GET, HTTP", "A1: the line carries the request"))
	}
}

func TestShutdown_AccessLogStoppedOnce(t *testing.T) {
	rec := &lcRecordingLogger{}
	app := New().SetLogger(rec).EnableAccessLog()
	app.Create(ModuleBuilder().Controllers(tracePipelineController{}).Build())
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tracepipeline", nil))
	app.Stop()
	app.Stop()
	if app.tracer.Enabled() {
		t.Error(test.DiffMessage(true, false, "A10: after shutdown the access log no longer listens"))
	}
}

func TestAccessLog_NotEnabledMeansNoListener(t *testing.T) {
	app := New()
	app.Create(ModuleBuilder().Controllers(tracePipelineController{}).Build())
	if app.tracer.Enabled() {
		t.Error(test.DiffMessage(true, false, "T1: without EnableAccessLog nobody listens, so requests are not traced"))
	}
}

func TestAccessLog_EnabledListensOnTheAppTracer(t *testing.T) {
	app := New().EnableAccessLog()
	app.Create(ModuleBuilder().Controllers(tracePipelineController{}).Build())
	if !app.tracer.Enabled() {
		t.Error(test.DiffMessage(false, true, "T2: EnableAccessLog listens on the app's tracer"))
	}
}
