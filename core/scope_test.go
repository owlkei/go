package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

const (
	scopeTraceID  = "4bf92f3577b34da6a3ce929d0e0e4736"
	scopeParentID = "00f067aa0ba902b7"
	scopeHeader   = "00-" + scopeTraceID + "-" + scopeParentID + "-01"
)

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func serveWithHeader(app *App, method, target string, header http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for k, values := range header {
		r.Header[k] = values
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func expectScopeHeaders(t *testing.T, w *httptest.ResponseRecorder, desc string) (requestID, traceID string, flags byte) {
	t.Helper()
	ids := w.Header().Values(ctx.RequestID)
	parents := w.Header().Values("Traceparent")
	if len(ids) != 1 || len(parents) != 1 {
		t.Fatal(test.DiffMessage([]int{len(ids), len(parents)}, []int{1, 1}, "S7: "+desc+": one X-Request-Id and one Traceparent"))
	}
	requestID = ids[0]
	if len(requestID) != 16 || !isLowerHex(requestID) {
		t.Error(test.DiffMessage(requestID, "<16 lowercase hexadecimal characters>", "S7: "+desc+": X-Request-Id is a span ID"))
	}
	if len(parents[0]) != 55 {
		t.Error(test.DiffMessage(len(parents[0]), 55, "S7: "+desc+": the traceparent is 55 bytes"))
	}
	traceID, parentID, flags, ok := tracecontext.ParseTraceparent(parents[0])
	if !ok {
		t.Fatal(test.DiffMessage(parents[0], "<a valid traceparent>", "S7: "+desc+": the response traceparent is valid"))
	}
	if parentID != requestID {
		t.Error(test.DiffMessage(parentID, requestID, "S7: "+desc+": the response traceparent names our request as the span"))
	}
	return requestID, traceID, flags
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
		handlerID = c.GetID()
		headerID = c.ResponseWriter.Header().Get(ctx.RequestID)
		return "H"
	})
	w := lcServe(app, http.MethodGet, "/ok")
	if handlerID == "" || handlerID != headerID || handlerID != w.Header().Get(ctx.RequestID) {
		t.Error(test.DiffMessage([]string{handlerID, headerID}, w.Header().Get(ctx.RequestID), "S1, S7: c.GetID() is the X-Request-Id of the response"))
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
			c.Status(ex.GetCode()).Text("caught")
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
		if s == nil || s.Request != c.GetID() || s.Request != c.ResponseWriter.Header().Get(ctx.RequestID) ||
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
