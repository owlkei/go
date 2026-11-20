package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/aggregation"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/test"
	"go.owlky.com/owlky/internal/trace"
	"go.owlky.com/owlky/internal/tracecontext"
	"go.owlky.com/owlky/internal/versioning"
	"go.owlky.com/owlky/log"
	"golang.org/x/net/websocket"
)

func TestNew(t *testing.T) {
	app := New(nil)
	if app == nil {
		t.Fatal(test.DiffMessage(nil, "*App", "New should not return nil"))
		return
	}
	if app.http.route == nil {
		t.Error(test.DiffMessage(nil, "router", "route not initialized"))
	}
	if app.Logger() == nil {
		t.Error(test.DiffMessage(nil, "a logger", "G1: Logger is never nil, even before Create"))
	}
}

func TestNewHasGlobalExceptionFilter(t *testing.T) {
	app := New(nil)
	if len(app.globalExceptionFilters) == 0 {
		t.Error(test.DiffMessage(0, ">0", "New should register default global exception filter"))
	}
}

func TestGetContextIDIgnoresRequestIDHeader(t *testing.T) {
	c := ctx.NewHTTPContext()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(ctx.RequestID, "test-id-123")
	ctx.HTTPInit(c, httptest.NewRecorder(), r)

	if c.ID() == "test-id-123" {
		t.Error(test.DiffMessage(c.ID(), "<generated UUID, not the header value>", "Init must always generate its own ID and ignore the X-Request-Id header"))
	}
	if c.ID() == "" {
		t.Error(test.DiffMessage(c.ID(), "<non-empty UUID>", "Init must still generate an ID when a header is present"))
	}
}

func TestGetContextIDGeneratesUUID(t *testing.T) {
	c1 := ctx.NewHTTPContext()
	ctx.HTTPInit(c1, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	c2 := ctx.NewHTTPContext()
	ctx.HTTPInit(c2, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if c1.ID() == "" {
		t.Error(test.DiffMessage(c1.ID(), "non-empty UUID", "should generate UUID when header absent"))
	}
	if c1.ID() == c2.ID() {
		t.Error(test.DiffMessage(c1.ID(), "different UUID", "each call should produce a unique ID"))
	}
}

func TestBindGlobalMiddlewaresChaining(t *testing.T) {
	app := New(nil)
	result := app.BindGlobalMiddlewares()
	if result != app {
		t.Error(test.DiffMessage(result, app, "BindGlobalMiddlewares should return *App"))
	}
}

func TestBindGlobalGuardsChaining(t *testing.T) {
	app := New(nil)
	result := app.BindGlobalGuards()
	if result != app {
		t.Error(test.DiffMessage(result, app, "BindGlobalGuards should return *App"))
	}
}

func TestBindGlobalInterceptorsChaining(t *testing.T) {
	app := New(nil)
	result := app.BindGlobalInterceptors()
	if result != app {
		t.Error(test.DiffMessage(result, app, "BindGlobalInterceptors should return *App"))
	}
}

func TestBindGlobalExceptionFiltersChaining(t *testing.T) {
	app := New(nil)
	result := app.BindGlobalExceptionFilters()
	if result != app {
		t.Error(test.DiffMessage(result, app, "BindGlobalExceptionFilters should return *App"))
	}
}

func TestServeHTTPNotFound(t *testing.T) {
	app := New(nil)
	app.Create(NewModuleBuilder().Build())

	r := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "unmatched route should return 404"))
	}
}

func TestServeHTTPSetsRequestIDHeader(t *testing.T) {
	app := New(nil)
	app.Create(NewModuleBuilder().Build())

	r := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if w.Header().Get(ctx.RequestID) == "" {
		t.Error(test.DiffMessage("", "non-empty", "ServeHTTP should set X-Request-Id response header"))
	}
}

func TestServeHTTPSetsGeneratedRequestID(t *testing.T) {
	app := New(nil)
	app.Create(NewModuleBuilder().Build())

	const clientID = "client-supplied-request-id"
	r := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	r.Header.Set(ctx.RequestID, clientID)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	got := w.Header().Get(ctx.RequestID)
	if got == "" {
		t.Error(test.DiffMessage(got, "<non-empty UUID>", "ServeHTTP should set a generated request ID on the response"))
	}
	if got == clientID {
		t.Error(test.DiffMessage(got, "<generated UUID, not the client-supplied one>", "ServeHTTP should not echo back a client-supplied X-Request-Id"))
	}
}

func TestNew_AppliesDefaultBodyLimit(t *testing.T) {
	app := New(nil)
	if app.maxBodyBytes != 10<<20 {
		t.Error(test.DiffMessage(app.maxBodyBytes, int64(10<<20), "O3: a new app must cap request bodies at 10 MiB, not read them unbounded"))
	}
}

func TestServeHTTPBodyOverLimit_Returns413(t *testing.T) {
	app := newBodyLimitApp(t, 64)

	payload := `{"k":"` + strings.Repeat("a", 4096) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.ServeHTTP(w, r)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(w.Code, http.StatusRequestEntityTooLarge, "a body past the configured cap must be rejected with 413, not buffered"))
	}
}

func TestServeHTTPBodyWithinLimit_Succeeds(t *testing.T) {
	app := newBodyLimitApp(t, 1<<20)

	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"k":"ok"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatal(test.DiffMessage(w.Code, http.StatusCreated, "a body within the cap must be served normally"))
	}
	if !strings.Contains(w.Body.String(), `"ok"`) {
		t.Error(test.DiffMessage(w.Body.String(), `"ok"`, "the handler must still receive the parsed body"))
	}
}

func TestServeHTTPBodyLimit_NegativeDisablesCap(t *testing.T) {
	app := newBodyLimitApp(t, -1)

	payload := `{"k":"` + strings.Repeat("a", 4096) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Error(test.DiffMessage(w.Code, http.StatusCreated, "an explicitly disabled cap must not reject large bodies"))
	}
}

func TestGetAfterCreate(t *testing.T) {
	app := New(nil)
	app.Create(NewModuleBuilder().Build())

	got := app.Get(&mockProvider{})
	if got != nil {
		t.Error(test.DiffMessage(got, nil, "Get for unregistered provider should return nil"))
	}
}

func TestServeHTTPConcurrent_NoDataRace(t *testing.T) {
	app := New(nil)
	app.Create(NewModuleBuilder().Build())

	const goroutines = 32
	const requestsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				r := httptest.NewRequest(http.MethodGet, "/notfound", nil)
				w := httptest.NewRecorder()
				app.ServeHTTP(w, r)

				if w.Code != http.StatusNotFound {
					t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "unmatched route should return 404 under concurrent load"))
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrentAppCreate_NoDataRace(t *testing.T) {

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()

			app := New(nil)
			app.BindGlobalMiddlewares(raceGlobalMiddleware{})
			app.Create(NewModuleBuilder().
				Providers(raceGlobalProvider{}).
				Build())
		}()
	}
	wg.Wait()
}

func TestGlobalMiddlewarePanic_CaughtByExceptionFilter(t *testing.T) {
	app := New(nil)
	app.BindGlobalMiddlewares(panicMiddleware{})
	app.Create(NewModuleBuilder().Controllers(panicMiddlewareController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/panicmiddleware", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "a middleware panic must still be caught by the exception filter"))
	}
}

func TestGlobalGuard_DeniesHTTPRequest(t *testing.T) {
	app := New(nil)
	app.BindGlobalGuards(denyGlobalGuard{})
	app.Create(NewModuleBuilder().Controllers(globalGuardController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/globalguard", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "a denying global guard must block the HTTP request"))
	}
}

func TestGlobalGuard_ShapelessGuardPanicsOnCreate(t *testing.T) {
	app := New(nil)
	app.BindGlobalGuards(shapelessGlobalGuard{})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "a global guard with no CanActivate method must panic at Create"))
		}
	}()
	app.Create(NewModuleBuilder().Controllers(globalGuardController{}).Build())
}

func TestTrace_EmitsPerStageAndComplete(t *testing.T) {
	app := New(nil)

	var mu sync.Mutex
	var stages []string
	var complete *trace.Event

	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		defer mu.Unlock()
		te := e
		if te.Stage == trace.StageComplete {
			d := te
			complete = &d
			return
		}
		stages = append(stages, te.Stage)
	})

	app.Create(NewModuleBuilder().Controllers(tracePipelineController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/tracepipeline", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	mu.Lock()
	defer mu.Unlock()

	wantStages := map[string]bool{
		trace.StageMiddleware:     false,
		trace.StageGuard:          false,
		trace.StagePreInterceptor: false,
		trace.StageHandler:        false,
	}
	for _, s := range stages {
		wantStages[s] = true
	}
	for s, seen := range wantStages {
		if !seen {
			t.Errorf("expected a %q trace event to fire, stages seen: %v", s, stages)
		}
	}
	if complete == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected a complete trace event to fire"))
	}
	if complete.Code != http.StatusOK {
		t.Error(test.DiffMessage(complete.Code, http.StatusOK, "complete trace event should carry the final status code"))
	}
}

func TestTrace_PipeStageExcludedFromHandlerDuration(t *testing.T) {
	app := New(nil)

	var mu sync.Mutex
	var pipeEvent, handlerEvent *trace.Event
	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		defer mu.Unlock()
		te := e
		switch te.Stage {
		case trace.StagePipe:
			d := te
			pipeEvent = &d
		case trace.StageHandler:
			d := te
			handlerEvent = &d
		}
	})

	app.Create(NewModuleBuilder().Controllers(tracePipeController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/tracepipe", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	mu.Lock()
	defer mu.Unlock()

	if pipeEvent == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected a pipe trace event to fire"))
	}
	if pipeEvent.Duration < traceSlowPipeSleep {
		t.Error(test.DiffMessage(pipeEvent.Duration, ">= "+traceSlowPipeSleep.String(), "pipe trace event should report the pipe's own execution time"))
	}
	if handlerEvent == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected a handler trace event to fire"))
	}
	if handlerEvent.Duration >= traceSlowPipeSleep {
		t.Error(test.DiffMessage(handlerEvent.Duration, "< "+traceSlowPipeSleep.String(), "handler trace duration should exclude the pipe's own execution time"))
	}
}

func TestTrace_ExceptionFilterStageFiresOnPanic(t *testing.T) {
	app := New(nil)

	var mu sync.Mutex
	var sawExceptionFilter bool
	var complete *trace.Event

	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		defer mu.Unlock()
		te := e
		if te.Stage == trace.StageExceptionFilter {
			sawExceptionFilter = true
		}
		if te.Stage == trace.StageComplete {
			d := te
			complete = &d
		}
	})

	app.Create(NewModuleBuilder().Controllers(tracePanicController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/tracepanic", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	mu.Lock()
	defer mu.Unlock()

	if !sawExceptionFilter {
		t.Error(test.DiffMessage(false, true, "expected an exceptionFilter trace event to fire for a panicking handler"))
	}
	if complete == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected a complete trace event to fire even on error"))
	}
	if complete.Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage(complete.Code, http.StatusInternalServerError, "complete trace event should carry the error status code"))
	}
}

func TestTrace_PipePanicStillEmitsPipeEventAndReachesExceptionFilter(t *testing.T) {
	app := New(nil)

	var mu sync.Mutex
	var pipeEvent *trace.Event
	var handlerEvent *trace.Event
	var sawExceptionFilter bool
	var complete *trace.Event

	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		defer mu.Unlock()
		te := e
		switch te.Stage {
		case trace.StagePipe:
			d := te
			pipeEvent = &d
		case trace.StageHandler:
			d := te
			handlerEvent = &d
		case trace.StageExceptionFilter:
			sawExceptionFilter = true
		case trace.StageComplete:
			d := te
			complete = &d
		}
	})

	app.Create(NewModuleBuilder().Controllers(tracePipePanicController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/tracepipepanic", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	mu.Lock()
	defer mu.Unlock()

	if pipeEvent == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected a pipe trace event even though Transform panicked"))
	}
	if pipeEvent.Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage(pipeEvent.Transport, trace.TransportHTTP, "pipe trace event transport"))
	}
	if handlerEvent != nil {
		t.Error(test.DiffMessage(handlerEvent, "<no handler event>", "no StageHandler event should fire when the panic happened before the handler body ever ran"))
	}
	if !sawExceptionFilter {
		t.Error(test.DiffMessage(false, true, "the pipe's panic should still reach the exception filter"))
	}
	if complete == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected a complete trace event to fire even when a pipe panics"))
	}
	if complete.Code != http.StatusBadRequest {
		t.Error(test.DiffMessage(complete.Code, http.StatusBadRequest, "the pipe's exception should determine the final status code"))
	}
}

func TestTrace_HandlerPanicAfterSuccessfulPipeStillExcludesPipeDuration(t *testing.T) {
	app := New(nil)

	var mu sync.Mutex
	var pipeEvent, handlerEvent *trace.Event
	var complete *trace.Event

	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		defer mu.Unlock()
		te := e
		switch te.Stage {
		case trace.StagePipe:
			d := te
			pipeEvent = &d
		case trace.StageHandler:
			d := te
			handlerEvent = &d
		case trace.StageComplete:
			d := te
			complete = &d
		}
	})

	app.Create(NewModuleBuilder().Controllers(traceHandlerPanicAfterPipeController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/tracehandlerpanicafterpipe", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	mu.Lock()
	defer mu.Unlock()

	if pipeEvent == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected the pipe trace event since the pipe itself succeeded"))
	}
	if pipeEvent.Duration < traceSlowPipeSleep {
		t.Error(test.DiffMessage(pipeEvent.Duration, ">= "+traceSlowPipeSleep.String(), "pipe trace event should report the pipe's own execution time"))
	}
	if handlerEvent == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected a handler trace event since the handler body started executing before it panicked"))
	}
	if handlerEvent.Duration >= traceSlowPipeSleep {
		t.Error(test.DiffMessage(handlerEvent.Duration, "< "+traceSlowPipeSleep.String(), "handler trace duration should still exclude the pipe's execution time even when the handler body itself panics"))
	}
	if complete == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected a complete trace event to fire"))
	}
	if complete.Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage(complete.Code, http.StatusInternalServerError, "the handler's exception should determine the final status code"))
	}
}

func TestTrace_InterceptorPrePostEventsMatchInCountAndOrder(t *testing.T) {
	app := New(nil)

	var mu sync.Mutex
	var pre []string
	var post []string

	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		defer mu.Unlock()
		te := e
		switch te.Stage {
		case trace.StagePreInterceptor:
			pre = append(pre, te.Name)
		case trace.StagePostInterceptor:
			post = append(post, te.Name)
		}
	})

	app.Create(NewModuleBuilder().Controllers(multiInterceptorController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/multiintercept", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	mu.Lock()
	defer mu.Unlock()

	if len(pre) != 3 {
		t.Fatalf("expected 3 pre-interceptor events, got %d: %v", len(pre), pre)
	}
	if len(post) != len(pre) {
		t.Fatalf("expected %d post-interceptor events to match the pre-interceptor count, got %d: %v", len(pre), len(post), post)
	}

	wantPost := make([]string, len(pre))
	for i, name := range pre {
		wantPost[len(pre)-1-i] = name
	}
	if post[0] != wantPost[0] || post[1] != wantPost[1] || post[2] != wantPost[2] {
		t.Error(test.DiffMessage(post, wantPost, "post-interceptor events must fire in reverse execution order of the pre-interceptor events"))
	}
}

func TestTrace_InterceptorEventsStopAtTheShortCircuit(t *testing.T) {
	app := New(nil)

	var mu sync.Mutex
	var pre, post []string

	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		defer mu.Unlock()
		te := e
		switch te.Stage {
		case trace.StagePreInterceptor:
			pre = append(pre, te.Name)
		case trace.StagePostInterceptor:
			post = append(post, te.Name)
		}
	})

	app.Create(NewModuleBuilder().Controllers(shortCircuitController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/shortcircuit", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	mu.Lock()
	defer mu.Unlock()

	if len(pre) != 2 {
		t.Fatal(test.DiffMessage(pre, []string{"core.multiInterceptorA", "core.shortCircuitInterceptor"}, "L12: only the interceptors that ran emit a pre-interceptor event"))
	}
	for _, name := range append(append([]string{}, pre...), post...) {
		if name == "core.multiInterceptorC" {
			t.Error(test.DiffMessage(name, "<no event>", "L12: an interceptor after the short-circuit never runs, so it emits no event"))
		}
	}
	if len(post) > len(pre) {
		t.Error(test.DiffMessage(len(post), len(pre), "L12: there are never more post-interceptor events than pre-interceptor events"))
	}
	if w.Body.String() != "short-circuited" {
		t.Error(test.DiffMessage(w.Body.String(), "short-circuited", "L12: the interceptor's return value is the response"))
	}
}

func TestTrace_InterceptorNoListener_NoPostEventOverhead(t *testing.T) {
	app := New(nil)
	app.Create(NewModuleBuilder().Controllers(multiInterceptorController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/multiintercept", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Body.String(), "ok", "request should complete normally with no trace listener attached"))
	}
}

func TestTrace_WSHandshakeEmitsStageCompleteWithHTTPTransport(t *testing.T) {
	app := New(&AppOptions{WS: &WSOptions{Path: "/ws"}})

	var mu sync.Mutex
	var complete *trace.Event
	var preMiddlewareTransport string
	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		defer mu.Unlock()
		te := e
		switch te.Stage {
		case trace.StageMiddleware:
			preMiddlewareTransport = te.Transport
		case trace.StageComplete:
			d := te
			complete = &d
		}
	})

	app.BindGlobalMiddlewares(traceMiddleware{})
	app.Create(NewModuleBuilder().Build())

	server := httptest.NewServer(app)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, err := websocket.Dial(wsURL, "", server.URL)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	mu.Lock()
	defer mu.Unlock()

	if preMiddlewareTransport != trace.TransportHTTP {
		t.Error(test.DiffMessage(preMiddlewareTransport, trace.TransportHTTP, "handshake middleware trace event must report HTTP transport"))
	}
	if complete == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected a StageComplete trace event for the WS handshake"))
	}
	if complete.Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage(complete.Transport, trace.TransportHTTP, "WS handshake StageComplete must report HTTP transport, not WS, since the connection has not upgraded yet"))
	}
}

func TestInitLogger_CustomLoggerAutomaticallyGetsTagBehavior(t *testing.T) {
	type secret struct {
		Value string `log:"value"`
		skip  string
	}

	capturing := &capturingMockLogger{}
	app := New(&AppOptions{Logger: capturing})
	app.Create(NewModuleBuilder().Build())

	app.Logger().Info("test", "s", secret{Value: "visible", skip: "hidden"})

	got, ok := capturing.lastArgs()[1].(map[string]any)
	if !ok {
		t.Fatalf("expected the struct to be expanded into a map[string]any, got %T", capturing.lastArgs()[1])
	}
	if got["value"] != "visible" {
		t.Error(test.DiffMessage(got["value"], "visible", "tagged field should be logged under its tag name"))
	}
	if _, exists := got["skip"]; exists {
		t.Error(test.DiffMessage(true, false, "untagged unexported field must not be logged"))
	}
}

func TestInitLogger_CustomLoggerAutomaticallyGetsMaskBehavior(t *testing.T) {
	capturing := &capturingMockLogger{}
	app := New(&AppOptions{Logger: log.WrapLogger(capturing, []string{"password"})})
	app.Create(NewModuleBuilder().Build())

	app.Logger().Info("test", "password", "secret")

	if capturing.lastArgs()[1] != "[REDACTED]" {
		t.Error(test.DiffMessage(capturing.lastArgs()[1], "[REDACTED]", "masking rules of log.WrapLogger should apply to the app logger"))
	}
}

func TestInitDevtool_DoesNotSpawnAServerGoroutine(t *testing.T) {

	before := runtime.NumGoroutine()

	app := New(&AppOptions{IsDevtool: true})
	app.Create(NewModuleBuilder().Build())

	if app.devtool == nil {
		t.Fatal("IsDevtool must still build the devtool snapshot")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Errorf("devtool must not leave a goroutine behind while its transport is unimplemented: %d before, %d after",
		before, runtime.NumGoroutine())
}

func TestTraceWSHandler_NoListener_StillCallsWrapped(t *testing.T) {
	ev := &trace.Tracer{}
	c := ctx.NewWSContext()
	var called bool
	h := traceWSHandler(ev, trace.StageGuard, "Test.Guard", func(c *ctx.WSContext) { called = true })
	h(c)
	if !called {
		t.Error(test.DiffMessage(false, true, "wrapped handler should still run when no listener is attached"))
	}
}

func TestTraceWSHandler_EmitsOnlyWhenListenerAttached(t *testing.T) {
	ev := &trace.Tracer{}
	c := ctx.NewWSContext()
	var got *trace.Event
	ev.Listen(func(e trace.Event) {
		te := e
		got = &te
	})
	h := traceWSHandler(ev, trace.StageGuard, "Test.Guard", func(c *ctx.WSContext) {})
	h(c)
	if got == nil {
		t.Fatal("expected a trace event to be emitted when a listener is attached")
	}
	if got.Stage != trace.StageGuard || got.Name != "Test.Guard" || got.Transport != trace.TransportWS {
		t.Error(test.DiffMessage([]any{got.Stage, got.Name, got.Transport}, []any{trace.StageGuard, "Test.Guard", trace.TransportWS}, "traceWSHandler emitted event fields"))
	}
}

func TestTraceWSCatch_NoListener_StillCallsWrapped(t *testing.T) {
	ev := &trace.Tracer{}
	c := ctx.NewWSContext()
	ex := exception.BadRequestException("bad")
	var called bool
	fn := traceWSCatch(ev, "Test.Filter", func(c *ctx.WSContext, ex *exception.Exception) { called = true })
	fn(c, &ex)
	if !called {
		t.Error(test.DiffMessage(false, true, "wrapped catch fn should still run when no listener is attached"))
	}
}

func TestTraceWSCatch_EmitsOnlyWhenListenerAttached(t *testing.T) {
	ev := &trace.Tracer{}
	c := ctx.NewWSContext()
	ex := exception.BadRequestException("bad")
	var got *trace.Event
	ev.Listen(func(e trace.Event) {
		te := e
		got = &te
	})
	fn := traceWSCatch(ev, "Test.Filter", func(c *ctx.WSContext, ex *exception.Exception) {})
	fn(c, &ex)
	if got == nil {
		t.Fatal("expected a trace event to be emitted when a listener is attached")
	}
	if got.Stage != trace.StageExceptionFilter || got.Name != "Test.Filter" || got.Transport != trace.TransportWS {
		t.Error(test.DiffMessage([]any{got.Stage, got.Name, got.Transport}, []any{trace.StageExceptionFilter, "Test.Filter", trace.TransportWS}, "traceWSCatch emitted event fields"))
	}
}

func TestNew_VersioningDefaultKey(t *testing.T) {
	app := New(&AppOptions{Versioning: &versioning.Options{Type: versioning.TypeQuery}})
	if !app.http.isVersioningEnabled || app.http.versioning == nil {
		t.Fatal(test.DiffMessage(app.http.isVersioningEnabled, true, "D7: Versioning.Type turns versioning on"))
	}
	if app.http.versioning.Key != "v" {
		t.Error(test.DiffMessage(app.http.versioning.Key, "v", "D7: an empty Key gets the default key"))
	}
}

func TestNew_VersioningPointerIsNotRetained(t *testing.T) {
	opts := &versioning.Options{Type: versioning.TypeHeader, Key: "X-V"}
	app := New(&AppOptions{Versioning: opts})
	opts.Key = "changed"
	if app.http.versioning.Key != "X-V" {
		t.Error(test.DiffMessage(app.http.versioning.Key, "X-V", "D7: a later change of the caller's options does not change the app"))
	}
}

func TestLogger_DefaultIsNotNil(t *testing.T) {
	app := New(nil)

	if app.Logger() == nil {
		t.Fatal(test.DiffMessage(nil, "a logger", "G1: Logger is never nil before Create"))
	}
	mustCreate(t, app, NewModuleBuilder().Build())
	if app.Logger() == nil {
		t.Fatal(test.DiffMessage(nil, "a logger", "G1: Logger is never nil after Create"))
	}
}

func TestLogger_SameValueEachCall(t *testing.T) {
	app := New(nil)
	first := app.Logger()
	if second := app.Logger(); second != first {
		t.Error(test.DiffMessage(second, first, "G1: Logger returns the same value on every call before Create"))
	}

	mustCreate(t, app, NewModuleBuilder().Build())
	if third := app.Logger(); third != first {
		t.Error(test.DiffMessage(third, first, "G1: Create does not replace the default logger"))
	}
}

func TestNew_LoggerLogsReachCustomLogger(t *testing.T) {
	rec := &createLogger{}
	app := New(&AppOptions{Logger: rec})
	mustCreate(t, app, NewModuleBuilder().Build())
	app.Logger().Info("hello", "k", "v")

	if got := len(rec.named("hello")); got != 1 {
		t.Error(test.DiffMessage(got, 1, "G2: what is logged through app.Logger() reaches the custom logger"))
	}
}

func TestNew_LoggerInjectedFieldsUseIt(t *testing.T) {
	rec := &createLogger{}
	app := New(&AppOptions{Logger: rec})
	mustCreate(t, app, NewModuleBuilder().Providers(isoLogProvider{}).Build())

	if got := len(rec.named("provider-ready")); got != 1 {
		t.Error(test.DiffMessage(got, 1, "G2: a log.Logger field is injected with the app's logger"))
	}
}

func TestNew_LoggerCustomLoggerGetsTagsAndMasking(t *testing.T) {
	type secret struct {
		Value string `log:"value"`
		skip  string
	}

	rec := &createLogger{}
	app := New(&AppOptions{Logger: rec})
	mustCreate(t, app, NewModuleBuilder().Build())

	app.Logger().Info("tagged", "s", secret{Value: "visible", skip: "hidden"})

	calls := rec.named("tagged")
	if len(calls) != 1 {
		t.Fatal(test.DiffMessage(len(calls), 1, "G3: the custom logger is called"))
	}
	got, ok := calls[0].args[1].(map[string]any)
	if !ok {
		t.Fatalf("G3: expected the struct to be expanded into a map[string]any, got %T", calls[0].args[1])
	}
	if got["value"] != "visible" {
		t.Error(test.DiffMessage(got["value"], "visible", "G3: a tagged field is logged under its tag"))
	}
	if _, exists := got["skip"]; exists {
		t.Error(test.DiffMessage(true, false, "G3: an untagged unexported field is not logged"))
	}
}

func TestNew_LoggerWrappedLoggerGetsMasking(t *testing.T) {
	rec := &createLogger{}
	app := New(&AppOptions{Logger: log.WrapLogger(rec, []string{"password"})})
	mustCreate(t, app, NewModuleBuilder().Build())

	app.Logger().Info("masked", "password", "secret")

	calls := rec.named("masked")
	if len(calls) != 1 || calls[0].args[1] != "[REDACTED]" {
		t.Error(test.DiffMessage(calls, "one call with [REDACTED]", "G6: the masking fields of log.WrapLogger apply"))
	}
}

func TestNew_LoggerWrappedLoggerNotWrappedAgain(t *testing.T) {
	rec := &createLogger{}
	wrapped := log.WrapLogger(rec, nil)
	app := New(&AppOptions{Logger: wrapped})
	mustCreate(t, app, NewModuleBuilder().Build())

	if got := app.Logger(); got != wrapped {
		t.Error(test.DiffMessage(got, wrapped, "G3: a logger from log.WrapLogger is used as it is"))
	}

	built := log.NewLog(nil)
	other := New(&AppOptions{Logger: built})
	mustCreate(t, other, NewModuleBuilder().Build())
	if got := other.Logger(); got != built {
		t.Error(test.DiffMessage(got, built, "G3: a logger from log.NewLog is used as it is"))
	}
}

func TestDevtool_WithoutVersioning(t *testing.T) {
	app := New(&AppOptions{IsDevtool: true})

	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().Controllers(dvController{}).Build())
	})
	if panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "D1: IsDevtool without Versioning does not panic"))
	}
	if app.devtool == nil || len(app.devtool.Controller.Http) != 1 {
		t.Fatal(test.DiffMessage(app.devtool, "a snapshot with one HTTP entry", "D1: the snapshot is built"))
	}

	v := app.devtool.Controller.Http[0].Versioning
	if v == nil {
		t.Fatal(test.DiffMessage(nil, "a versioning entry", "D1: the entry has versioning"))
	}
	if v.Value != "2" || v.Key != "" || v.Type != 0 {
		t.Error(test.DiffMessage([]any{v.Value, v.Key, v.Type}, []any{"2", "", int32(0)}, "D1: value is the route's version, key and type are empty"))
	}
}

func TestDevtool_WithVersioningUnchanged(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		app := New(&AppOptions{IsDevtool: true, Versioning: &versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"}})
		app.Create(NewModuleBuilder().Controllers(dvController{}).Build())

		if app.devtool == nil || len(app.devtool.Controller.Http) != 1 {
			t.Fatal(test.DiffMessage(app.devtool, "a snapshot with one HTTP entry", "D1: the snapshot is built"))
		}
		v := app.devtool.Controller.Http[0].Versioning
		if v.Value != "2" || v.Key != "X-Version" || v.Type != int32(versioning.TypeHeader) {
			t.Error(test.DiffMessage([]any{v.Value, v.Key, v.Type}, []any{"2", "X-Version", int32(versioning.TypeHeader)}, "D1: with versioning the snapshot is what it is today"))
		}
	})
}

func TestDevtool_SnapshotNamesUnchangedByMove(t *testing.T) {
	app := New(&AppOptions{IsDevtool: true})
	app.Create(NewModuleBuilder().Controllers(dvGuardedController{}).Build())

	if app.devtool == nil || len(app.devtool.Controller.Http) != 1 {
		t.Fatal(test.DiffMessage(app.devtool, "one HTTP entry", "B22: the snapshot is built"))
	}
	e := app.devtool.Controller.Http[0]
	got := []any{e.HttpMethod, e.Route, len(e.Guards)}
	want := []any{"GET", "/dv/", 1}
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatal(test.DiffMessage(got, want, "B22: method, route and guard count"))
	}
	if e.Guards[0].Name != "core.dvGuard" {
		t.Error(test.DiffMessage(e.Guards[0].Name, "core.dvGuard", "B22, D4: layer names keep the package name, not the path"))
	}
}

func TestHooks_OnReadySameOrderAfterWiring(t *testing.T) {
	rec := &createRecorder{}
	app := New(nil)
	root := hkDiamond(rec)
	var seen []bool
	root.OnReady = func() {
		rec.add("ready:root")
		seen = append(seen, app.Get(hkProvider{}) != nil)
	}
	rootWithProvider := NewModuleBuilder().Imports(root).Providers(hkProvider{}).Build()
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

func TestHooks_OnReadyPanicReachesCaller(t *testing.T) {
	rec := &createRecorder{}
	root := hkModule(rec, "root")
	root.OnReady = func() { panic("ready boom") }

	value, panicked := createPanicValue(func() { New(nil).Create(root) })

	if !panicked || value != "ready boom" {
		t.Error(test.DiffMessage(value, "ready boom", "K3: a panic in OnReady reaches the caller unchanged"))
	}
}

func TestHooks_OnReadySeesRoutes(t *testing.T) {
	app := New(nil)
	var code int
	root := NewModuleBuilder().Controllers(hkController{}).Build()
	root.OnReady = func() { code = createGet(app, "/hk").Code }
	mustCreate(t, app, root)

	if code != 200 {
		t.Error(test.DiffMessage(code, 200, "K2: OnReady runs after the routes exist"))
	}
}

func TestGet_BeforeCreateIsNil(t *testing.T) {
	if got := New(nil).Get(isoPlainProvider{}); got != nil {
		t.Error(test.DiffMessage(got, nil, "I9: Get before Create is nil"))
	}
}

func TestGet_UnknownTypeIsNil(t *testing.T) {
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Providers(isoPlainProvider{}).Build())

	if got := app.Get(isoGlobalProvider{}); got != nil {
		t.Error(test.DiffMessage(got, nil, "I9: Get of a type the app does not have is nil"))
	}
}

func TestGet_ReturnsThisAppsInstance(t *testing.T) {
	appA := New(nil)
	mustCreate(t, appA, NewModuleBuilder().Providers(isoGlobalProvider{Tag: "a"}).Build())
	appB := New(nil)
	mustCreate(t, appB, NewModuleBuilder().Providers(isoGlobalProvider{Tag: "b"}).Build())

	a, _ := appA.Get(isoGlobalProvider{}).(isoGlobalProvider)
	b, _ := appB.Get(isoGlobalProvider{}).(isoGlobalProvider)
	if a.Tag != "a" || b.Tag != "b" {
		t.Error(test.DiffMessage([]string{a.Tag, b.Tag}, []string{"a", "b"}, "I9: Get returns the instance this app constructed"))
	}
}

func TestGet_ConcurrentAfterCreate_NoRace(t *testing.T) {
	app := New(nil)
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

func TestCreate_NilModulePanics(t *testing.T) {
	msExpectPanic(t, "M1", "invalid module: Create needs a root module", func() {
		New(nil).Create(nil)
	})
}

func TestCreate_TwicePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M2", "invalid app: Create called twice", func() {
		app.Create(NewModuleBuilder().Build())
	})
}

func TestApp_BindGlobalMiddlewaresAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: BindGlobalMiddlewares must be called before Create", func() {
		app.BindGlobalMiddlewares(msMiddleware{})
	})
}

func TestApp_BindGlobalGuardsAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: BindGlobalGuards must be called before Create", func() {
		app.BindGlobalGuards(isoGuard{Role: "ms"})
	})
}

func TestApp_BindGlobalInterceptorsAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: BindGlobalInterceptors must be called before Create", func() {
		app.BindGlobalInterceptors(z1Interceptor{Name: "ms"})
	})
}

func TestApp_BindGlobalExceptionFiltersAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: BindGlobalExceptionFilters must be called before Create", func() {
		app.BindGlobalExceptionFilters(globalHTTPExceptionFilter{})
	})
}

func TestApp_ConfigurationBeforeCreateDoesNotPanic(t *testing.T) {
	app := New(&AppOptions{
		Versioning:  &versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"},
		IsDevtool:   true,
		IsAccessLog: true,
	})

	text, panicked := createPanicText(func() {
		app.BindGlobalMiddlewares(msMiddleware{}).
			BindGlobalGuards(isoGuard{Role: "ms-before"}).
			BindGlobalInterceptors(z1Interceptor{Name: "ms-before"})
		app.Create(NewModuleBuilder().Controllers(hkController{}).Build())
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "M3: configuration before Create is fine"))
	}
}

func TestBodyLimit_NoBodyNotWrapped(t *testing.T) {
	if got := bodyProbe(t, nil); got != "nobody" {
		t.Error(test.DiffMessage(got, "nobody", "bug 12, B1: a request with http.NoBody is not wrapped"))
	}
}

func TestBodyLimit_NilBodyNotWrapped(t *testing.T) {
	got := bodyProbe(t, func(r *http.Request) { r.Body = nil })
	if got != "nil" {
		t.Error(test.DiffMessage(got, "nil", "B1: a request with a nil body is not wrapped"))
	}
}

func TestBodyLimit_BodyIsWrapped(t *testing.T) {
	got := bodyProbe(t, func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader("data")) })
	if got != "wrapped" {
		t.Error(test.DiffMessage(got, "wrapped", "B1: a request with a body is capped"))
	}
}

func TestBodyLimit_DeclaredOverLimit413(t *testing.T) {
	app := newBodyLimitApp(t, 64)
	payload := `{"k":"` + strings.Repeat("a", 4096) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if r.ContentLength != int64(len(payload)) {
		t.Fatal(test.DiffMessage(r.ContentLength, len(payload), "fixture: the request declares its length"))
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(w.Code, http.StatusRequestEntityTooLarge, "B1: a declared length over the limit answers 413"))
	}
}

func TestBodyLimit_ChunkedOver413(t *testing.T) {
	app := newBodyLimitApp(t, 64)
	payload := `{"k":"` + strings.Repeat("a", 4096) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader(payload)))
	r.Header.Set("Content-Type", "application/json")
	r.ContentLength = -1
	r.TransferEncoding = []string{"chunked"}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(w.Code, http.StatusRequestEntityTooLarge, "B1: a chunked body over the limit answers 413"))
	}
}

func TestBodyLimit_BodyAtTheLimitIsServed(t *testing.T) {
	payload := `{"k":"ok"}`
	app := newBodyLimitApp(t, int64(len(payload)))
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Error(test.DiffMessage(w.Code, http.StatusCreated, "B1: a body of exactly the limit is served"))
	}
}

func TestNew_LoggerAcceptsFiveMethodAdapter(t *testing.T) {
	var adapter log.LogAdapter = &createLogger{}
	rec := adapter.(*createLogger)
	app := New(&AppOptions{Logger: adapter})
	mustCreate(t, app, NewModuleBuilder().Build())
	app.Logger().Info("hello", "k", "v")
	if len(rec.named("hello")) != 1 {
		t.Error(test.DiffMessage(len(rec.named("hello")), 1, "I1: a type with five methods and no From works as the app's logger"))
	}
}

func TestLogger_InjectedHasFrom(t *testing.T) {
	rec := &createLogger{}
	app := New(&AppOptions{Logger: rec})
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
			t.Error(test.DiffMessage(calls, want, "I2: the injected logger and app.Logger() implement From whatever was given as Logger, "+name))
		}
	}
}

func TestLogger_BuiltinAppLoggerHasFrom(t *testing.T) {
	w := redirectStdout(t)
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Build())
	app.Logger().From(scopedContext()).Info("scoped-builtin", "k", 1)
	app.Stop()
	if out := w.String(); !strings.Contains(out, "scoped-builtin") || !strings.Contains(out, scopeTraceID) {
		t.Error(test.DiffMessage(out, "a line with the trace ID", "I2: the default logger implements From"))
	}
}

func TestNew_LoggerAdapterCalledDirectly(t *testing.T) {
	w := redirectStdout(t)
	w.hold()
	rec := &createLogger{}
	app := New(&AppOptions{Logger: rec})
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
	app := New(nil)
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

func TestCreate_BootstrapPanicLosesNoLine(t *testing.T) {
	w := redirectStdout(t)
	app := New(nil)
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
	app := New(nil)
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

func TestServeHTTP_RequestIDAndTraceparentHeaders(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/ok")
	requestID, traceID, flags := expectScopeHeaders(t, w, "route")
	if flags != 0x03 {
		t.Error(test.DiffMessage(flags, byte(3), "S5, Q1: a trace we start is sampled and random"))
	}
	if want := fmt.Sprintf("%012x", time.Now().UnixMilli()); len(traceID) != 32 || traceID[:8] != want[:8] {
		t.Error(test.DiffMessage(traceID, "<a time-ordered trace ID>", "G2: the new trace ID starts with the time"))
	}

	other := lcServe(app, http.MethodGet, "/ok")
	otherID, otherTrace, _ := expectScopeHeaders(t, other, "second route")
	if otherID == requestID || otherTrace == traceID {
		t.Error(test.DiffMessage([]string{otherID, otherTrace}, "<new IDs>", "S2: every request has its own request ID and trace"))
	}
}

func TestServeHTTP_HeadersOn404(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/missing")
	if w.Code != http.StatusNotFound {
		t.Fatal(test.DiffMessage(w.Code, http.StatusNotFound, "fixture: unmatched route"))
	}
	expectScopeHeaders(t, w, "404")
}

func TestServeHTTP_HeadersOn405(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodDelete, "/readonly")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal(test.DiffMessage(w.Code, http.StatusMethodNotAllowed, "fixture: wrong method"))
	}
	expectScopeHeaders(t, w, "405")
}

func TestServeHTTP_HeadersOnError(t *testing.T) {
	app := lcNewApp(t, lcConfig{logger: &mockLogger{}})
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	w := lcServe(app, http.MethodGet, "/ok")
	if w.Code != http.StatusBadRequest {
		t.Fatal(test.DiffMessage(w.Code, http.StatusBadRequest, "fixture: the handler throws an exception"))
	}
	expectScopeHeaders(t, w, "exception")

	lcSetHandler(lcPanicWith("a plain panic"))
	w = lcServe(app, http.MethodGet, "/ok")
	if w.Code != http.StatusInternalServerError {
		t.Fatal(test.DiffMessage(w.Code, http.StatusInternalServerError, "fixture: the handler panics"))
	}
	expectScopeHeaders(t, w, "panic")
}

func TestServeHTTP_HeadersSetBeforeMiddleware(t *testing.T) {
	app := lcApp(t)
	var seenID, seenTraceparent string
	lcSetMiddleware("g1", func(w http.ResponseWriter, r *http.Request, next ctx.Next) {
		seenID = w.Header().Get(ctx.RequestID)
		seenTraceparent = w.Header().Get("Traceparent")
		next()
	})
	w := lcServe(app, http.MethodGet, "/ok")
	if seenID == "" || seenID != w.Header().Get(ctx.RequestID) {
		t.Error(test.DiffMessage(seenID, w.Header().Get(ctx.RequestID), "S7: X-Request-Id is set before any middleware runs"))
	}
	if seenTraceparent == "" || seenTraceparent != w.Header().Get("Traceparent") {
		t.Error(test.DiffMessage(seenTraceparent, w.Header().Get("Traceparent"), "S7: Traceparent is set before any middleware runs"))
	}
}

func TestServeHTTP_ContinuesIncomingTrace(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/ok", "Traceparent", scopeHeader)
	requestID, traceID, flags := expectScopeHeaders(t, w, "continued")
	if traceID != scopeTraceID {
		t.Error(test.DiffMessage(traceID, scopeTraceID, "S3: the response continues the incoming trace ID"))
	}
	if requestID == scopeParentID {
		t.Error(test.DiffMessage(requestID, "<our own request ID>", "S2: the incoming parent is not our request"))
	}
	if flags != 0x01 {
		t.Error(test.DiffMessage(flags, byte(1), "S5: the incoming flags are kept"))
	}

	w = lcServe(app, http.MethodGet, "/ok", "Traceparent", "00-"+scopeTraceID+"-"+scopeParentID+"-ff")
	if _, _, flags := expectScopeHeaders(t, w, "unknown flag bits"); flags != 0x03 {
		t.Error(test.DiffMessage(flags, byte(3), "S5: unknown bits are cleared"))
	}
}

func TestServeHTTP_HostileTraceparentIgnored(t *testing.T) {
	app := lcApp(t)
	hostile := []string{
		strings.ToUpper(scopeHeader),
		"00-" + strings.Repeat("0", 32) + "-" + scopeParentID + "-01",
		"00-" + scopeTraceID + "-" + strings.Repeat("0", 16) + "-01",
		"ff-" + scopeTraceID + "-" + scopeParentID + "-01",
		scopeHeader + "\r\nSet-Cookie: pwned=1",
		scopeHeader + "-extra",
		" " + scopeHeader,
		strings.Repeat("A", 1<<20),
		"",
		"00-" + scopeTraceID + "-" + scopeParentID,
	}
	for _, value := range hostile {
		w := serveWithHeader(app, http.MethodGet, "/ok", http.Header{"Traceparent": {value}})
		_, traceID, flags := expectScopeHeaders(t, w, "hostile traceparent")
		if traceID == scopeTraceID || strings.EqualFold(traceID, strings.ToLower(value)) {
			t.Error(test.DiffMessage(traceID, "<a new trace ID>", "S3: an invalid traceparent is ignored silently"))
		}
		if flags != 0x03 {
			t.Error(test.DiffMessage(flags, byte(3), "S5: an ignored traceparent starts a trace with 03"))
		}
		if w.Code != http.StatusOK {
			t.Error(test.DiffMessage(w.Code, http.StatusOK, "S3: an invalid traceparent is not an error response"))
		}
		if strings.ContainsAny(w.Header().Get("Traceparent")+w.Header().Get(ctx.RequestID), "\r\n") || w.Header().Get("Set-Cookie") != "" {
			t.Error(test.DiffMessage(w.Header(), "<no injected header>", "S3: nothing hostile reaches the response"))
		}
	}
}

func TestServeHTTP_TwoTraceparentHeadersIgnored(t *testing.T) {
	app := lcApp(t)
	second := "00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01"
	w := serveWithHeader(app, http.MethodGet, "/ok", http.Header{"Traceparent": {scopeHeader, second}})
	_, traceID, _ := expectScopeHeaders(t, w, "two traceparent headers")
	if traceID == scopeTraceID || traceID == strings.Repeat("a", 32) {
		t.Error(test.DiffMessage(traceID, "<a new trace ID>", "S3: two traceparent values are ignored"))
	}
}

func TestServeHTTP_ClientCannotSetRequestID(t *testing.T) {
	app := lcApp(t)
	for _, value := range []string{"client-chosen", scopeParentID, "evil\r\nSet-Cookie: x=1", strings.Repeat("x", 4096), ""} {
		w := serveWithHeader(app, http.MethodGet, "/ok", http.Header{ctx.RequestID: {value}, "Traceparent": {scopeHeader}})
		requestID, _, _ := expectScopeHeaders(t, w, "client request ID")
		if requestID == value || requestID == scopeParentID {
			t.Error(test.DiffMessage(requestID, "<our own ID>", "S2: no header can set the request ID"))
		}
		if w.Header().Get("Set-Cookie") != "" {
			t.Error(test.DiffMessage(w.Header(), "<no injected header>", "S4: the incoming ID is never echoed"))
		}
	}
}

func TestServeHTTP_HandlerSeesResponseIDs(t *testing.T) {
	app := lcApp(t)
	var handlerID, headerID string
	lcSetHandler(func(c *ctx.HTTPContext) any {
		handlerID = c.ID()
		headerID = c.ResponseWriter.Header().Get(ctx.RequestID)
		return "H"
	})
	w := lcServe(app, http.MethodGet, "/ok")
	if handlerID == "" || handlerID != headerID || handlerID != w.Header().Get(ctx.RequestID) {
		t.Error(test.DiffMessage([]string{handlerID, headerID}, w.Header().Get(ctx.RequestID), "S1, S7: c.ID() is the X-Request-Id of the response"))
	}
}

func TestScope_SameInEveryLayer(t *testing.T) {
	app := lcNewApp(t, lcConfig{logger: &mockLogger{}})

	var mu sync.Mutex
	got := map[string]*tracecontext.Scope{}
	record := func(label string, c context.Context) {
		mu.Lock()
		got[label] = tracecontext.ScopeOf(c)
		mu.Unlock()
	}
	service := func(c context.Context) { record("service", c) }

	for _, name := range []string{"g1", "g2"} {
		lcSetMiddleware(name, func(w http.ResponseWriter, r *http.Request, next ctx.Next) {
			record("middleware-global", r.Context())
			next()
		})
	}
	for _, name := range []string{"m1", "m2"} {
		lcSetMiddleware(name, func(w http.ResponseWriter, r *http.Request, next ctx.Next) {
			record("middleware-module", r.Context())
			next()
		})
	}
	for _, name := range []string{"g1", "m1"} {
		lcSetGuard(name, func(c *ctx.HTTPContext) bool {
			record("guard-"+name, c.Context())
			return true
		})
		lcSetIntercept(name, func(c *ctx.HTTPContext, agg *aggregation.Aggregation) any {
			record("interceptor-"+name, c.Context())
			return agg.Pipe()
		})
	}
	for _, name := range []string{"gf1", "gf2", "mf1", "mf2"} {
		lcSetFilter(name, func(c *ctx.HTTPContext, ex *exception.Exception) {
			record("filter", c.Context())
			c.Status(ex.Code()).Text("caught")
		})
	}
	lcSetHandler(func(c *ctx.HTTPContext) any {
		record("handler", c.Context())
		record("handler-request", c.Request.Context())
		service(c.Context())
		panic(exception.BadRequestException("boom"))
	})

	w := lcServe(app, http.MethodGet, "/ok")
	if w.Code != http.StatusBadRequest {
		t.Fatal(test.DiffMessage(w.Code, http.StatusBadRequest, "fixture: the handler throws so the filter runs"))
	}

	want := got["handler"]
	if want == nil {
		t.Fatal(test.DiffMessage(want, "<a scope>", "S1: c.Context() in the handler carries a scope"))
	}
	if want.Request != w.Header().Get(ctx.RequestID) {
		t.Error(test.DiffMessage(want.Request, w.Header().Get(ctx.RequestID), "S1: the scope is the request's"))
	}
	for _, label := range []string{
		"middleware-global", "middleware-module", "guard-g1", "guard-m1", "interceptor-g1", "interceptor-m1",
		"handler", "handler-request", "service", "filter",
	} {
		scope, ok := got[label]
		if !ok {
			t.Error(test.DiffMessage(label, "<recorded>", "S1: layer never ran: "+label))
			continue
		}
		if scope != want {
			t.Error(test.DiffMessage(scope, want, "Step 2: "+label+" sees the same *Scope as the handler"))
		}
	}
}

func TestScope_GoroutineAfterRequest(t *testing.T) {
	app := lcApp(t)
	release := make(chan struct{})
	result := make(chan [3]string, 1)
	var once sync.Once
	lcSetHandler(func(c *ctx.HTTPContext) any {
		once.Do(func() {
			kept := c.Context()
			go func() {
				<-release
				var out [3]string
				if s := tracecontext.ScopeOf(kept); s != nil {
					out = [3]string{s.Request, s.TraceID, s.Traceparent()}
				}
				result <- out
			}()
		})
		return "H"
	})

	w := lcServe(app, http.MethodGet, "/ok")
	wantID, wantTrace, _ := expectScopeHeaders(t, w, "first request")
	for range 1000 {
		lcServe(app, http.MethodGet, "/ok")
	}
	close(release)

	select {
	case got := <-result:
		want := [3]string{wantID, wantTrace, w.Header().Get("Traceparent")}
		if got != want {
			t.Error(test.DiffMessage(got, want, "S8: a goroutine started by the handler reads its own IDs after 1,000 requests reused the pool"))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the goroutine never answered")
	}
}

func TestScope_ConcurrentRequestsIsolated(t *testing.T) {
	app := lcApp(t)
	var wrong atomic.Int32
	lcSetHandler(func(c *ctx.HTTPContext) any {
		s := tracecontext.ScopeOf(c.Context())
		incoming := c.Request.Header.Get("Traceparent")
		if s == nil || s.Request != c.ID() || s.Request != c.ResponseWriter.Header().Get(ctx.RequestID) ||
			s.TraceID != incoming[3:35] || s.Parent != incoming[36:52] {
			wrong.Add(1)
		}
		return "H"
	})

	const goroutines = 16
	const perGoroutine = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]struct{}{}
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perGoroutine {
				traceID := strconv.FormatInt(int64(g*perGoroutine+i+1), 16)
				traceID = strings.Repeat("a", 32-len(traceID)) + traceID
				parentID := strings.Repeat("b", 16-len(strconv.Itoa(g))) + strconv.Itoa(g)
				w := lcServe(app, http.MethodGet, "/ok", "Traceparent", "00-"+traceID+"-"+parentID+"-01")
				id := w.Header().Get(ctx.RequestID)
				headerTrace, _, _, _ := tracecontext.ParseTraceparent(w.Header().Get("Traceparent"))
				if headerTrace != traceID || id == "" {
					wrong.Add(1)
				}
				mu.Lock()
				seen[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if wrong.Load() != 0 {
		t.Error(test.DiffMessage(wrong.Load(), int32(0), "S8: concurrent requests never read each other's scope"))
	}
	if len(seen) != goroutines*perGoroutine {
		t.Error(test.DiffMessage(len(seen), goroutines*perGoroutine, "S2: request IDs are unique under concurrency"))
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
	app := New(&AppOptions{Logger: &mockLogger{}, WS: &WSOptions{Path: "/ws"}})
	app.Create(NewModuleBuilder().Build())

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
	if created[0].ID() != "" || created[0].Request != nil {
		t.Error(test.DiffMessage(created[0].ID(), "", "F2: a refused upgrade releases the exchange (it is reset)"))
	}
}

func TestServeHTTP_UpgradeReleasesExchangeOnce(t *testing.T) {
	for name, version := range map[string]string{"accepted": "13", "refused": "99"} {
		app := New(&AppOptions{Logger: &mockLogger{}, WS: &WSOptions{Path: "/ws"}})
		app.Create(NewModuleBuilder().Build())
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

func TestTrace_EveryWSEventCarriesTransport(t *testing.T) {
	app := New(&AppOptions{WS: &WSOptions{Path: "/ws"}})
	rec := newTraceRecorder(app)
	app.Create(NewModuleBuilder().Controllers(traceWSAllController{}).Build())

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
		if e.Stage == trace.StagePreRoute {
			if e.Transport != trace.TransportHTTP {
				t.Error(test.DiffMessage(e.Transport, trace.TransportHTTP, "T7: a handshake stage ("+e.Name+") is an HTTP event"))
			}
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
	app := New(nil)
	var seen int
	stop := app.tracer.Listen(func(trace.Event) { seen++ })
	stop()
	if app.tracer.Enabled() {
		t.Fatal(test.DiffMessage(true, false, "setup: no listener"))
	}
	app.Create(NewModuleBuilder().Controllers(tracePipelineController{}).Build())

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
	app := New(nil)
	var mu sync.Mutex
	counts := map[string]int{}
	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		counts[e.ID]++
		mu.Unlock()
	})
	app.Create(NewModuleBuilder().Controllers(tracePipelineController{}).Build())

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

func TestAccessLog_NotEnabledMeansNoListener(t *testing.T) {
	app := New(nil)
	app.Create(NewModuleBuilder().Controllers(tracePipelineController{}).Build())
	if app.tracer.Enabled() {
		t.Error(test.DiffMessage(true, false, "T1: without IsAccessLog nobody listens, so requests are not traced"))
	}
}

func TestAccessLog_EnabledListensOnTheAppTracer(t *testing.T) {
	app := New(&AppOptions{IsAccessLog: true})
	app.Create(NewModuleBuilder().Controllers(tracePipelineController{}).Build())
	if !app.tracer.Enabled() {
		t.Error(test.DiffMessage(false, true, "T2: IsAccessLog listens on the app's tracer"))
	}
}

func TestTransport_FromHandlerContext(t *testing.T) {
	var seen string
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Traceparent")
	}))
	defer upstream.Close()

	app := lcApp(t)
	lcSetHandler(func(c *ctx.HTTPContext) any {
		r, err := http.NewRequestWithContext(c.Context(), http.MethodGet, upstream.URL, nil)
		if err != nil {
			return err.Error()
		}
		res, err := (&http.Client{Transport: trace.Transport(nil)}).Do(r)
		if err != nil {
			return err.Error()
		}
		_ = res.Body.Close()
		return "ok"
	})
	w := lcServe(app, http.MethodGet, "/ok")
	requestID, traceID, _ := expectScopeHeaders(t, w, "handler calling upstream")

	got, parentID, _, ok := tracecontext.ParseTraceparent(seen)
	if !ok {
		t.Fatal(test.DiffMessage(seen, "<a valid traceparent>", "T6: the upstream receives a traceparent"))
	}
	if got != traceID {
		t.Error(test.DiffMessage(got, traceID, "T6: the upstream sees the response's trace ID"))
	}
	if parentID != requestID {
		t.Error(test.DiffMessage(parentID, requestID, "T3: the upstream's parent is the request's span"))
	}
}

func TestApp_Devtool_ModulesLive(t *testing.T) {
	release := make(chan struct{})
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	sql.OnInit = func() { <-release }
	user := lfNamed("user", NewModuleBuilder().AsyncImports(sql).Providers(lfUserSvc{}).Controllers(lfUserCtl{}))
	root := lfNamed("root", NewModuleBuilder().Imports(user))

	app := New(&AppOptions{Logger: &lcRecordingLogger{}, IsDevtool: true})
	lfRelease(t, app, release)
	lfCreate(t, app, root)

	byName := func() map[string]string {
		resp, err := app.devtool.GetConfiguration(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, m := range resp.Modules {
			out[m.Name] = m.State
		}
		return out
	}

	before := byName()
	for name, want := range map[string]string{"root": "ready", "sql": "pending", "user": "pending"} {
		if before[name] != want {
			t.Error(test.DiffMessage(before[name], want, "V: devtool module "+name+" before the release"))
		}
	}

	close(release)
	lfWait(t, app.container.settled(user), "user")

	after := byName()
	for name, want := range map[string]string{"root": "ready", "sql": "ready", "user": "ready"} {
		if after[name] != want {
			t.Error(test.DiffMessage(after[name], want, "V: devtool module "+name+" after the release, read live"))
		}
	}

	resp, _ := app.devtool.GetConfiguration(context.Background(), nil)
	for _, m := range resp.Modules {
		if (m.Name == "sql" || m.Name == "user") && !m.IsAsync {
			t.Error(test.DiffMessage(m.IsAsync, true, "V: "+m.Name+" is async"))
		}
		if m.Name == "root" && m.IsAsync {
			t.Error(test.DiffMessage(m.IsAsync, false, "V: root is not async"))
		}
	}
}

func TestApp_Devtool_ModulesWaitingForAndLastError(t *testing.T) {
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	user := lfNamed("user", NewModuleBuilder().AsyncImports(sql).Providers(lfUserSvc{}).Controllers(lfUserCtl{}))
	root := lfNamed("root", NewModuleBuilder().Imports(user))
	hold := lfHold(sql)

	app := New(&AppOptions{Logger: &lcRecordingLogger{}, IsDevtool: true})
	lfCreate(t, app, root)
	lfRelease(t, app, hold)

	snapshot := func(name string) (waiting []string, state, lastError string) {
		resp, _ := app.devtool.GetConfiguration(context.Background(), nil)
		for _, m := range resp.Modules {
			if m.Name == name {
				return m.WaitingFor, m.State, m.LastError
			}
		}
		t.Fatal("no module " + name)
		return
	}

	waiting, state, _ := snapshot("user")
	if state != "pending" || !strings.Contains(strings.Join(waiting, ","), "sql") {
		t.Error(test.DiffMessage(waiting, "waiting for sql", "V: waiting_for lists the label of the pending entry"))
	}

	app.container.settle(sql, fmt.Errorf("lf-dsn-down"))
	_, state, lastError := snapshot("sql")
	if state != "failed" || !strings.Contains(lastError, "lf-dsn-down") {
		t.Error(test.DiffMessage(lastError, "lf-dsn-down", "V: last_error holds the cause once failed"))
	}
	waiting, state, _ = snapshot("user")
	if state != "failed" || len(waiting) != 0 {
		t.Error(test.DiffMessage(waiting, "no waiting_for", "V: a failed module no longer waits"))
	}
}
