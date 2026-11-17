package core

import (
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/trace"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"golang.org/x/net/websocket"
)

type spanPipe struct{}

func (spanPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return spanPipe{} }

type spanController struct {
	common.HTTP
	common.Middleware
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (ctl spanController) NewController() Controller {
	ctl.BindMiddleware(lcMiddleware{Name: "m1"})
	ctl.BindGuard(lcGuard{Name: "m1"})
	ctl.BindInterceptor(lcInterceptor{Name: "m1"})
	ctl.BindExceptionFilter(lcFilter{Name: "mf1"})
	return ctl
}

func (ctl spanController) READ_tree(spanPipe) string { return "tree" }

func (ctl spanController) READ_boom(*ctx.HTTPContext) any {
	panic(exception.BadRequestException("boom"))
}

func newSpanApp(t *testing.T) (*App, *traceRecorder) {
	t.Helper()
	lcReset(t)
	app := New()
	app.SetLogger(&mockLogger{})
	rec := newTraceRecorder(app)
	app.BindGlobalMiddlewares(lcMiddleware{Name: "g1"})
	app.BindGlobalGuards(lcGuard{Name: "g1"})
	app.BindGlobalInterceptors(lcInterceptor{Name: "g1"})
	app.BindGlobalExceptionFilters(lcFilter{Name: "gf1"})
	app.Create(ModuleBuilder().Controllers(spanController{}).Build())
	return app, rec
}

var stageRank = map[string]int{
	trace.StageMiddleware:      0,
	trace.StageGuard:           1,
	trace.StagePreInterceptor:  2,
	trace.StagePipe:            3,
	trace.StageHandler:         4,
	trace.StagePostInterceptor: 5,
	trace.StageExceptionFilter: 6,
	trace.StageComplete:        7,
}

func expectTree(t *testing.T, events []trace.Event, transport, id, traceID, rootParent string, wantStages ...string) {
	t.Helper()
	if len(events) < 2 {
		t.Fatal(test.DiffMessage(len(events), "> 1", "E4: a unit emits its stages and its root"))
	}
	root := events[len(events)-1]
	if root.Stage != trace.StageComplete {
		t.Fatal(test.DiffMessage(root.Stage, trace.StageComplete, "E4: the root is emitted last"))
	}
	if root.ID != id || root.SpanID != id || root.ParentID != rootParent {
		t.Error(test.DiffMessage([]string{root.ID, root.SpanID, root.ParentID}, []string{id, id, rootParent}, "E2: complete is the root span, SpanID == ID, ParentID is the scope's parent"))
	}
	spans := map[string]struct{}{}
	previous := -1
	seen := map[string]bool{}
	for _, e := range events {
		if e.Transport != transport {
			t.Error(test.DiffMessage(e.Transport, transport, "E1: Transport of "+e.Stage))
		}
		if e.ID != id || e.TraceID != traceID {
			t.Error(test.DiffMessage([]string{e.ID, e.TraceID}, []string{id, traceID}, "E1: every event carries the unit's ID and the trace ID ("+e.Stage+")"))
		}
		if len(e.SpanID) != 16 || !isLowerHex(e.SpanID) {
			t.Error(test.DiffMessage(e.SpanID, "<16 hex characters>", "E3: SpanID of "+e.Stage))
		}
		if _, dup := spans[e.SpanID]; dup {
			t.Error(test.DiffMessage(e.SpanID, "<unique within the unit>", "E4: all SpanIDs of one unit differ ("+e.Stage+")"))
		}
		spans[e.SpanID] = struct{}{}
		if e.Stage != trace.StageComplete {
			if e.ParentID != id || e.SpanID == id {
				t.Error(test.DiffMessage([]string{e.SpanID, e.ParentID}, []string{"<fresh>", id}, "E3: "+e.Stage+" is a child of the unit"))
			}
		}
		rank, known := stageRank[e.Stage]
		if !known || rank < previous {
			t.Error(test.DiffMessage(e.Stage, "<a stage not before an earlier one>", "E4: events arrive in lifecycle order"))
		}
		previous = rank
		seen[e.Stage] = true
	}
	for _, stage := range wantStages {
		if !seen[stage] {
			t.Error(test.DiffMessage(seen, "<has "+stage+">", "E3: the tree holds a "+stage+" span"))
		}
	}
}

func TestTrace_EventTree_HTTP(t *testing.T) {
	app, rec := newSpanApp(t)
	w := lcServe(app, http.MethodGet, "/tree")
	requestID, traceID, _ := expectScopeHeaders(t, w, "tree")
	if w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "fixture: /tree answers 200"))
	}

	expectTree(t, rec.all(), trace.TransportHTTP, requestID, traceID, "",
		trace.StageMiddleware, trace.StageGuard, trace.StagePreInterceptor, trace.StagePipe, trace.StageHandler, trace.StagePostInterceptor, trace.StageComplete)
}

func TestTrace_EventTree_HTTPException(t *testing.T) {
	app, rec := newSpanApp(t)
	w := lcServe(app, http.MethodGet, "/boom")
	requestID, traceID, _ := expectScopeHeaders(t, w, "boom")
	if w.Code != http.StatusBadRequest {
		t.Fatal(test.DiffMessage(w.Code, http.StatusBadRequest, "fixture: /boom answers 400"))
	}

	expectTree(t, rec.all(), trace.TransportHTTP, requestID, traceID, "",
		trace.StageGuard, trace.StageHandler, trace.StageExceptionFilter, trace.StageComplete)
}

func TestTrace_EventTree_HTTPNotFound(t *testing.T) {
	app, rec := newSpanApp(t)
	w := lcServe(app, http.MethodGet, "/nowhere")
	requestID, traceID, _ := expectScopeHeaders(t, w, "404")

	expectTree(t, rec.all(), trace.TransportHTTP, requestID, traceID, "", trace.StageMiddleware, trace.StageComplete)
}

func TestTrace_RootParentFromTraceparent(t *testing.T) {
	app, rec := newSpanApp(t)
	w := lcServe(app, http.MethodGet, "/tree", "Traceparent", scopeHeader)
	requestID, traceID, _ := expectScopeHeaders(t, w, "continued tree")
	if traceID != scopeTraceID {
		t.Fatal(test.DiffMessage(traceID, scopeTraceID, "fixture: the trace is continued"))
	}

	events := rec.all()
	expectTree(t, events, trace.TransportHTTP, requestID, scopeTraceID, scopeParentID, trace.StageHandler, trace.StageComplete)
	for _, e := range events {
		if e.Stage != trace.StageComplete && e.ParentID == scopeParentID {
			t.Error(test.DiffMessage(e.ParentID, requestID, "E3: stages hang under our request, not under the caller's span"))
		}
	}
}

func TestTrace_EventTree_ConcurrentRequestsKeepOwnTrees(t *testing.T) {
	app, rec := newSpanApp(t)
	const goroutines = 8
	const perGoroutine = 25
	var wg sync.WaitGroup
	ids := make(chan [2]string, goroutines*perGoroutine)
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perGoroutine {
				w := lcServe(app, http.MethodGet, "/tree")
				traceID, _, _, _ := tracecontext.ParseTraceparent(w.Header().Get("Traceparent"))
				ids <- [2]string{w.Header().Get(ctx.RequestID), traceID}
			}
		}()
	}
	wg.Wait()
	close(ids)

	byID := map[string][]trace.Event{}
	for _, e := range rec.all() {
		byID[e.ID] = append(byID[e.ID], e)
	}
	count := 0
	for pair := range ids {
		count++
		events := byID[pair[0]]
		expectTree(t, events, trace.TransportHTTP, pair[0], pair[1], "", trace.StageHandler, trace.StageComplete)
	}
	if count != goroutines*perGoroutine || len(byID) != count {
		t.Error(test.DiffMessage([]int{count, len(byID)}, []int{goroutines * perGoroutine, goroutines * perGoroutine}, "E4: one tree per request"))
	}
}

func TestTrace_ListenerAddedMidRequestGetsNoFilterEvent(t *testing.T) {
	app := lcNewApp(t, lcConfig{logger: &mockLogger{}})
	var mu sync.Mutex
	var late []trace.Event
	lcSetHandler(func(c *ctx.HTTPContext) any {
		app.tracer.Listen(func(e trace.Event) {
			mu.Lock()
			late = append(late, e)
			mu.Unlock()
		})
		panic(exception.BadRequestException("boom"))
	})

	w := lcServe(app, http.MethodGet, "/ok")
	if w.Code != http.StatusBadRequest {
		t.Fatal(test.DiffMessage(w.Code, http.StatusBadRequest, "fixture: the handler throws so a filter runs"))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(late) != 0 {
		t.Error(test.DiffMessage(late, []trace.Event(nil), "F1: filter events follow ex.traced, so a listener added mid-request sees no event of that request"))
	}
}

func TestTrace_ListenerRemovedMidRequestGetsNothingAfter(t *testing.T) {
	app := lcNewApp(t, lcConfig{logger: &mockLogger{}})
	var mu sync.Mutex
	var events []trace.Event
	stop := app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	lcSetHandler(func(c *ctx.HTTPContext) any {
		stop()
		panic(exception.BadRequestException("boom"))
	})

	lcServe(app, http.MethodGet, "/ok")
	mu.Lock()
	defer mu.Unlock()
	for _, e := range events {
		if e.Stage == trace.StageExceptionFilter || e.Stage == trace.StageComplete {
			t.Error(test.DiffMessage(e.Stage, "<no event after the listener left>", "F1: nothing is delivered to a stopped listener"))
		}
	}
}

func TestServeHTTP_RefusedUpgradeReleasesExchange(t *testing.T) {
	app := New()
	app.SetLogger(&mockLogger{})
	app.EnableWS(&WSConfig{Path: "/ws"})
	app.Create(ModuleBuilder().Build())

	var mu sync.Mutex
	var created []*httpExchange
	app.ctxPool.New = func() any {
		ex := newHTTPExchange()
		mu.Lock()
		created = append(created, ex)
		mu.Unlock()
		return ex
	}
	served := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.ServeHTTP(w, r)
		served <- struct{}{}
	}))
	defer server.Close()

	resp, closeResp := rawUpgrade(t, server.URL, map[string]string{"Sec-WebSocket-Version": "99"})
	defer closeResp()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatal(test.DiffMessage(resp.StatusCode, http.StatusBadRequest, "fixture: the library refuses the upgrade before calling Handshake"))
	}
	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Fatal("ServeHTTP never returned")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(created) != 1 {
		t.Fatal(test.DiffMessage(len(created), 1, "fixture: one exchange was taken from the pool"))
	}
	if created[0].GetID() != "" || created[0].Request != nil {
		t.Error(test.DiffMessage(created[0].GetID(), "", "F2: a refused upgrade releases the exchange (it is reset)"))
	}
}

func TestServeHTTP_UpgradeReleasesExchangeOnce(t *testing.T) {
	for name, version := range map[string]string{"accepted": "13", "refused": "99"} {
		app := New()
		app.SetLogger(&mockLogger{})
		app.EnableWS(&WSConfig{Path: "/ws"})
		app.Create(ModuleBuilder().Build())
		served := make(chan struct{}, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			app.ServeHTTP(w, r)
			served <- struct{}{}
		}))

		_, closeResp := rawUpgrade(t, server.URL, map[string]string{"Sec-WebSocket-Version": version})
		closeResp()
		server.Close()
		select {
		case <-served:
		case <-time.After(10 * time.Second):
			t.Fatal("ServeHTTP never returned")
		}

		first, second := app.ctxPool.Get(), app.ctxPool.Get()
		if first == second {
			t.Error(test.DiffMessage(first, "<two different exchanges>", "F2: an exchange is released exactly once ("+name+")"))
		}
	}
}

type countingReader struct {
	inner io.Reader
	bytes atomic.Int64
}

func (r *countingReader) Read(b []byte) (int, error) {
	r.bytes.Add(int64(len(b)))
	return r.inner.Read(b)
}

func countRandomBytes(t *testing.T, fn func()) int64 {
	t.Helper()
	counter := &countingReader{inner: rand.Reader}
	previous := rand.Reader
	rand.Reader = counter
	defer func() { rand.Reader = previous }()
	fn()
	return counter.bytes.Load()
}

func TestTrace_NoListenerGeneratesOneID(t *testing.T) {
	app := benchApp(benchPlainController{})
	r := httptest.NewRequest(http.MethodGet, "/bench", nil)
	w := &benchDiscardWriter{header: http.Header{}}
	app.ServeHTTP(w, r)

	const requests = 1000
	drawn := countRandomBytes(t, func() {
		for range requests {
			app.ServeHTTP(w, r)
		}
	})
	const perRequest = 18
	if drawn < requests*perRequest-512 {
		t.Error(test.DiffMessage(drawn, ">= "+"17,488", "G3: a request draws 18 random bytes (a trace ID tail and its own ID) from crypto/rand"))
	}
	if limit := int64(requests*perRequest*11/10 + 1024); !raceEnabled && drawn > limit {
		t.Error(test.DiffMessage(drawn, "<= "+"20,824", "E5: with no listener a request generates no ID beyond its own (a stage span ID is 8 more bytes)"))
	}
}

func TestTrace_ListenerGeneratesOneIDPerStage(t *testing.T) {
	app := benchApp(benchPlainController{})
	var stages, completes atomic.Int64
	app.tracer.Listen(func(e trace.Event) {
		if e.Stage == trace.StageComplete {
			completes.Add(1)
		} else {
			stages.Add(1)
		}
	})
	r := httptest.NewRequest(http.MethodGet, "/bench", nil)
	w := &benchDiscardWriter{header: http.Header{}}
	app.ServeHTTP(w, r)
	stages.Store(0)
	completes.Store(0)

	const requests = 500
	drawn := countRandomBytes(t, func() {
		for range requests {
			app.ServeHTTP(w, r)
		}
	})
	want := requests*18 + stages.Load()*8
	if completes.Load() != requests || stages.Load() == 0 {
		t.Fatal(test.DiffMessage([]int64{completes.Load(), stages.Load()}, "<500 completes and some stages>", "fixture: the listener saw every request"))
	}
	if drawn < want-512 || (!raceEnabled && drawn > want*11/10+1024) {
		t.Error(test.DiffMessage(drawn, want, "E3, E5: each stage span ID is 8 random bytes, only while a listener exists"))
	}
}

func TestServeHTTP_ScopeAddsOneAllocation(t *testing.T) {
	app := benchApp(benchPlainController{})
	for _, tc := range []struct {
		method, target string
		limit          float64
		desc           string
	}{
		{http.MethodGet, "/bench", 7, "a static route"},
		{http.MethodGet, "/does-not-exist", 4, "a 404"},
		{http.MethodDelete, "/bench", 5, "a 405"},
	} {
		if got := allocsOfServe(app, tc.method, tc.target); got > tc.limit {
			t.Error(test.DiffMessage(got, "<= "+strconv.Itoa(int(tc.limit)), "Performance: "+tc.desc+" gains at most one allocation for the scope"))
		}
	}
}

func TestServeHTTP_TraceparentAddsNoAllocation(t *testing.T) {
	app := benchApp(benchPlainController{})
	r := httptest.NewRequest(http.MethodGet, "/bench", nil)
	r.Header.Set("Traceparent", scopeHeader)
	w := &benchDiscardWriter{header: http.Header{}}
	got := testing.AllocsPerRun(200, func() { app.ServeHTTP(w, r) })
	if got > 7 {
		t.Error(test.DiffMessage(got, "<= 7", "Performance: continuing a trace costs no allocation beyond an own trace"))
	}
}

func TestTrace_EventTree_WSMessage(t *testing.T) {
	env := newWSScopeEnv(t)
	conn, connected := env.dial(t, map[string]string{"Traceparent": scopeHeader})
	env.send(t, conn, WSPayload{Type: TypeSubscribe, ID: "s1", Topic: []string{"traced"}})
	env.send(t, conn, WSPayload{Type: TypePublish, ID: "p1", Topic: []string{"traced"}, Message: "hi"})

	completes := env.waitCompletes(t, "traced", 2)
	subscribe, publish := completes[0], completes[1]
	if subscribe.ID == publish.ID {
		t.Fatal(test.DiffMessage(subscribe.ID, "<a different ID>", "W6: each frame is its own message"))
	}

	byID := map[string][]trace.Event{}
	for _, e := range env.rec.all() {
		if e.Transport == trace.TransportWS {
			byID[e.ID] = append(byID[e.ID], e)
		}
	}
	expectTree(t, byID[publish.ID], trace.TransportWS, publish.ID, scopeTraceID, connected.ID,
		trace.StageGuard, trace.StagePreInterceptor, trace.StagePipe, trace.StagePostInterceptor, trace.StageComplete)
	expectTree(t, byID[subscribe.ID], trace.TransportWS, subscribe.ID, scopeTraceID, connected.ID,
		trace.StageGuard, trace.StagePreInterceptor, trace.StageComplete)
}

func TestTrace_EventTree_WSMessageException(t *testing.T) {
	env := newWSScopeEnv(t)
	conn, connected := env.dial(t, nil)
	handshake := env.waitCompletes(t, "/ws", 1)[0]
	env.send(t, conn, WSPayload{Type: TypeSubscribe, ID: "s1", Topic: []string{"boom"}})
	if err := websocket.JSON.Send(conn, WSPayload{Type: TypePublish, ID: "p1", Topic: []string{"boom"}, Message: "hi"}); err != nil {
		t.Fatal(err)
	}
	completes := env.waitCompletes(t, "boom", 2)
	publish := completes[1]

	var events []trace.Event
	for _, e := range env.rec.all() {
		if e.Transport == trace.TransportWS && e.ID == publish.ID {
			events = append(events, e)
		}
	}
	expectTree(t, events, trace.TransportWS, publish.ID, handshake.TraceID, connected.ID, trace.StageExceptionFilter, trace.StageComplete)
}

func TestTrace_WSMessagesShareTheConnectionTrace(t *testing.T) {
	env := newWSScopeEnv(t)
	conn, connected := env.dial(t, nil)
	handshake := env.waitCompletes(t, "/ws", 1)[0]
	env.send(t, conn, WSPayload{Type: TypeSubscribe, ID: "s1", Topic: []string{"a"}})
	env.send(t, conn, WSPayload{Type: TypeSubscribe, ID: "s2", Topic: []string{"b"}})
	a := env.waitCompletes(t, "a", 1)[0]
	b := env.waitCompletes(t, "b", 1)[0]
	for _, e := range []trace.Event{a, b} {
		if e.TraceID != handshake.TraceID || e.ParentID != connected.ID || e.ParentID != handshake.ID {
			t.Error(test.DiffMessage([]string{e.TraceID, e.ParentID}, []string{handshake.TraceID, connected.ID}, "E2: a message is a root under the connection, in the handshake's trace"))
		}
	}
	if len(handshake.TraceID) != 32 || !isLowerHex(handshake.TraceID) {
		t.Error(test.DiffMessage(handshake.TraceID, "<32 hex characters>", "E1: the trace ID"))
	}
}
