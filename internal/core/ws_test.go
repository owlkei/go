package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/str"
	"go.owlkei.com/owlkei/internal/test"
	"go.owlkei.com/owlkei/internal/trace"
	"go.owlkei.com/owlkei/internal/tracecontext"
	"go.owlkei.com/owlkei/log"
	"golang.org/x/net/websocket"
)

func TestNewWS_DefaultPath(t *testing.T) {
	ws := NewWS(&WSOptions{logger: log.NewLog(nil)})
	if !ws.isWSPath("/ws") {
		t.Error(test.DiffMessage(ws.isWSPath("/ws"), true, "NewWS with no configured Path should default to /ws"))
	}
}

func TestNewWS_CustomPath(t *testing.T) {
	ws := NewWS(&WSOptions{Path: "/live", logger: log.NewLog(nil)})
	if !ws.isWSPath("/live") {
		t.Error(test.DiffMessage(ws.isWSPath("/live"), true, "NewWS should honor a configured custom Path"))
	}
	if ws.isWSPath("/ws") {
		t.Error(test.DiffMessage(ws.isWSPath("/ws"), false, "a custom Path should not also match the default /ws"))
	}
}

func TestIsWSPath_NormalizesSlashes(t *testing.T) {
	ws := NewWS(&WSOptions{Path: "live", logger: log.NewLog(nil)})
	if !ws.isWSPath("/live") {
		t.Error(test.DiffMessage(ws.isWSPath("/live"), true, "isWSPath should normalize a Path configured without leading/trailing slashes"))
	}
}

func TestHandshake_Allowed(t *testing.T) {
	ws := NewWS(&WSOptions{logger: log.NewLog(nil)})

	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()

	if err := ws.handshake(c); err != nil {
		t.Error(test.DiffMessage(err, nil, "handshake with no middlewares should succeed"))
	}
}

func TestHandshake_RejectedWhenMiddlewareDoesNotCallNext(t *testing.T) {
	ws := NewWS(&WSOptions{logger: log.NewLog(nil)})
	ws.globalMiddlewares = []ctx.HTTPHandler{
		func(c *ctx.HTTPContext) {},
	}

	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()

	err := ws.handshake(c)
	if err != errWSHandshakeRejected {
		t.Error(test.DiffMessage(err, errWSHandshakeRejected, "handshake should be rejected when a middleware does not call next()"))
	}
}

func TestHandshake_EmitsMiddlewareTraceWithHTTPTransport(t *testing.T) {
	ev := &trace.Tracer{}
	var got []trace.Event
	ev.Listen(func(e trace.Event) {
		got = append(got, e)
	})

	ws := NewWS(&WSOptions{
		logger:            log.NewLog(nil),
		tracer:            ev,
		injectedProviders: map[string]Provider{},
		container:         newContainer(),
		globalMiddlewares: []common.MiddlewareFn{wsHandshakeTraceMiddleware{}},
	})

	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()

	if err := ws.handshake(c); err != nil {
		t.Fatal(test.DiffMessage(err, nil, "handshake should succeed when the middleware calls next()"))
	}

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 trace event, got %d: %+v", len(got), got)
	}
	if got[0].Stage != trace.StageMiddleware {
		t.Error(test.DiffMessage(got[0].Stage, trace.StageMiddleware, "handshake middleware trace event stage"))
	}
	if got[0].Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage(got[0].Transport, trace.TransportHTTP, "handshake middleware trace event must report HTTP transport, not WS, since the connection has not upgraded yet"))
	}
	if got[0].Name != "core.wsHandshakeTraceMiddleware" {
		t.Error(test.DiffMessage(got[0].Name, "core.wsHandshakeTraceMiddleware", "handshake middleware trace event name"))
	}
}

func TestHandshake_EmitsOneTraceEventPerMiddleware(t *testing.T) {
	ev := &trace.Tracer{}
	var got []trace.Event
	ev.Listen(func(e trace.Event) {
		got = append(got, e)
	})

	ws := NewWS(&WSOptions{
		logger:            log.NewLog(nil),
		tracer:            ev,
		injectedProviders: map[string]Provider{},
		container:         newContainer(),
		globalMiddlewares: []common.MiddlewareFn{wsHandshakeTraceMiddleware{}, wsHandshakeTraceMiddlewareB{}},
	})

	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()

	if err := ws.handshake(c); err != nil {
		t.Fatal(test.DiffMessage(err, nil, "handshake should succeed when every middleware calls next()"))
	}

	if len(got) != 2 {
		t.Fatalf("expected exactly 2 trace events, got %d: %+v", len(got), got)
	}
	wantNames := []string{"core.wsHandshakeTraceMiddleware", "core.wsHandshakeTraceMiddlewareB"}
	for i, want := range wantNames {
		if got[i].Name != want {
			t.Error(test.DiffMessage(got[i].Name, want, "handshake middleware trace events must fire in registration order"))
		}
		if got[i].Transport != trace.TransportHTTP {
			t.Error(test.DiffMessage(got[i].Transport, trace.TransportHTTP, "every handshake middleware trace event must report HTTP transport"))
		}
	}
}

func TestHandshake_NoListener_StillSucceeds(t *testing.T) {
	ev := &trace.Tracer{}

	ws := NewWS(&WSOptions{
		logger:            log.NewLog(nil),
		tracer:            ev,
		injectedProviders: map[string]Provider{},
		container:         newContainer(),
		globalMiddlewares: []common.MiddlewareFn{wsHandshakeTraceMiddleware{}},
	})

	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()

	if err := ws.handshake(c); err != nil {
		t.Fatal(test.DiffMessage(err, nil, "handshake should succeed when the middleware calls next()"))
	}
}

func TestWSHandleRequest_SendsConnectedPayloadAndRegisters(t *testing.T) {
	ws := newTestWSBare(t)
	serverConn, clientConn, cleanup := newTestWSConnPair(t)
	defer cleanup()

	done := make(chan struct{})
	go func() {
		ws.handleRequest(serverConn)
		close(done)
	}()

	var got WSPayload
	if err := websocket.JSON.Receive(clientConn, &got); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if got.Type != TypeConnected {
		t.Error(test.DiffMessage(got.Type, TypeConnected, "handleRequest should send a connected payload on handshake"))
	}
	if len(got.ID) != 16 || !isLowerHex(got.ID) {
		t.Error(test.DiffMessage(got.ID, "<16 lowercase hexadecimal characters>", "W2: a connection without a scoped request gets a span ID, not a UUID"))
	}

	if _, ok := ws.connmgr.Get(got.ID); !ok {
		t.Error(test.DiffMessage(ok, true, "the connection should be registered under the id sent in the connected payload"))
	}

	_ = clientConn.Close()
	<-done

	if _, ok := ws.connmgr.Get(got.ID); ok {
		t.Error(test.DiffMessage(ok, false, "the connection should be unregistered once handleRequest returns"))
	}
}

func TestIsWSPath_EqualsEnclosedComparison(t *testing.T) {
	for _, configured := range []string{"", "/ws", "live", "/live/", "a/b"} {
		ws := NewWS(&WSOptions{Path: configured, logger: log.NewLog(nil)})
		for _, candidate := range []string{"", "/", "/ws", "/ws/", "ws", "ws/", "/wsx", "/w", "//ws", "/ws//", "/live", "/live/", "live", "/a/b", "/a/b/", "/a/bc", "/WS", "/ws?x", "/ws/x"} {
			want := str.Enclose(candidate, '/') == ws.path
			if got := ws.isWSPath(candidate); got != want {
				t.Error(test.DiffMessage(got, want, "P10: isWSPath equals the enclosed comparison for path "+configured+" and request "+candidate))
			}
		}
	}
}

func TestIsWSPath_DoesNotAllocate(t *testing.T) {
	ws := NewWS(&WSOptions{Path: "/ws", logger: log.NewLog(nil)})

	for _, candidate := range []string{"/ws", "/ws/", "/other", "/", ""} {
		if got := testing.AllocsPerRun(100, func() { _ = ws.isWSPath(candidate) }); got != 0 {
			t.Error(test.DiffMessage(got, 0.0, "bug 13, P10: isWSPath allocates nothing for "+candidate))
		}
	}
}

func TestWS_ConnectionIDIsHandshakeRequestID(t *testing.T) {
	env := newWSScopeEnv(t)
	_, connected := env.dial(t, map[string]string{"Traceparent": scopeHeader})

	if len(connected.ID) != 16 || !isLowerHex(connected.ID) {
		t.Fatal(test.DiffMessage(connected.ID, "<16 lowercase hexadecimal characters>", "W2: the connected payload carries the handshake's request ID, not a UUID"))
	}
	handshake := env.waitCompletes(t, "/ws", 1)[0]
	if handshake.ID != connected.ID || handshake.Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage(handshake.ID, connected.ID, "W2: the connection ID is the handshake request's ID"))
	}
	if _, ok := env.app.ws.connmgr.Get(connected.ID); !ok {
		t.Error(test.DiffMessage(ok, true, "W2: the connection is registered under that ID"))
	}
}

func TestWS_ClientCannotChooseConnectionID(t *testing.T) {
	env := newWSScopeEnv(t)
	for _, value := range []string{"client-chosen", scopeParentID, "evil\tid"} {
		_, connected := env.dial(t, map[string]string{ctx.RequestID: value, "Traceparent": scopeHeader})
		if connected.ID == value || connected.ID == scopeParentID || len(connected.ID) != 16 {
			t.Error(test.DiffMessage(connected.ID, "<our own 16-hex ID>", "S2, W2: the connection ID is never chosen by the client"))
		}
	}
}

func TestWS_HandshakeResponseHeaders(t *testing.T) {
	env := newWSScopeEnv(t)
	resp, closeResp := rawUpgrade(t, env.server.URL, map[string]string{"Traceparent": scopeHeader})
	defer closeResp()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatal(test.DiffMessage(resp.StatusCode, http.StatusSwitchingProtocols, "fixture: the upgrade succeeds"))
	}
	ids := resp.Header.Values(ctx.RequestID)
	parents := resp.Header.Values("Traceparent")
	if len(ids) != 1 || len(parents) != 1 {
		t.Fatal(test.DiffMessage([]int{len(ids), len(parents)}, []int{1, 1}, "W3: the 101 response carries X-Request-Id and Traceparent"))
	}
	if len(ids[0]) != 16 || !isLowerHex(ids[0]) {
		t.Error(test.DiffMessage(ids[0], "<16 hex characters>", "W3: X-Request-Id on the 101"))
	}
	traceID, parentID, flags, ok := tracecontext.ParseTraceparent(parents[0])
	if !ok || traceID != scopeTraceID || parentID != ids[0] || flags != 0x01 {
		t.Error(test.DiffMessage([]any{traceID, parentID, flags, ok}, []any{scopeTraceID, ids[0], byte(1), true}, "W3: the 101 continues the trace with the handshake request as span"))
	}
	if handshake := env.waitCompletes(t, "/ws", 1)[0]; handshake.ID != ids[0] {
		t.Error(test.DiffMessage(ids[0], handshake.ID, "W3: the header carries the same ID as the handshake request"))
	}
}

func TestWS_HandshakeResponseHeadersOwnTrace(t *testing.T) {
	env := newWSScopeEnv(t)
	resp, closeResp := rawUpgrade(t, env.server.URL, map[string]string{"Traceparent": "garbage", ctx.RequestID: "client-chosen"})
	defer closeResp()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatal(test.DiffMessage(resp.StatusCode, http.StatusSwitchingProtocols, "fixture: the upgrade succeeds"))
	}
	id := resp.Header.Get(ctx.RequestID)
	traceID, parentID, flags, ok := tracecontext.ParseTraceparent(resp.Header.Get("Traceparent"))
	if !ok || parentID != id || flags != 0x03 || traceID == scopeTraceID || id == "client-chosen" {
		t.Error(test.DiffMessage([]any{traceID, parentID, flags, ok, id}, "<own trace, flags 03, our request as span>", "W3, S3: an invalid traceparent is ignored on the handshake"))
	}
}

func TestWS_MessageScope(t *testing.T) {
	env := newWSScopeEnv(t)
	type seen struct {
		scope *tracecontext.Scope
		id    string
	}
	var mu sync.Mutex
	var got []seen
	wsScopeSetHook(func(c *ctx.WSContext) {
		mu.Lock()
		got = append(got, seen{scope: tracecontext.ScopeOf(c.Context()), id: c.ID()})
		mu.Unlock()
	})

	conn, connected := env.dial(t, map[string]string{"Traceparent": scopeHeader})
	env.send(t, conn, WSPayload{Type: TypeSubscribe, ID: "s1", Topic: []string{"a"}})
	env.send(t, conn, WSPayload{Type: TypePublish, ID: "p1", Topic: []string{"a"}, Message: "hi"})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].scope == nil {
		t.Fatal(test.DiffMessage(len(got), 1, "W5: the handler ran once and its context carries a scope"))
	}
	s := got[0].scope
	if s.TraceID != scopeTraceID {
		t.Error(test.DiffMessage(s.TraceID, scopeTraceID, "W4: the message continues the handshake's trace"))
	}
	if s.Connection != connected.ID || s.Parent != connected.ID {
		t.Error(test.DiffMessage([]string{s.Connection, s.Parent}, []string{connected.ID, connected.ID}, "W4: the connection is the connected payload's id"))
	}
	if len(s.Message) != 16 || !isLowerHex(s.Message) || s.Message == connected.ID || got[0].id != s.Message {
		t.Error(test.DiffMessage(got[0].id, s.Message, "W5: c.ID() is the message ID, which is not the connection's"))
	}
	if s.Request != "" || s.Upstream != "" {
		t.Error(test.DiffMessage([]string{s.Request, s.Upstream}, []string{"", ""}, "W4: Request and Upstream are empty on a message"))
	}
	if _, parentID, flags, ok := tracecontext.ParseTraceparent(s.Traceparent()); !ok || parentID != s.Message || flags != 0x01 {
		t.Error(test.DiffMessage(s.Traceparent(), "<flags 01, the message as span>", "W4: flags are the connection's"))
	}
}

func TestWS_TwoTopicsTwoMessages(t *testing.T) {
	env := newWSScopeEnv(t)
	var mu sync.Mutex
	var scopes []*tracecontext.Scope
	wsScopeSetHook(func(c *ctx.WSContext) {
		mu.Lock()
		scopes = append(scopes, tracecontext.ScopeOf(c.Context()))
		mu.Unlock()
	})

	conn, connected := env.dial(t, nil)
	env.send(t, conn, WSPayload{Type: TypeSubscribe, ID: "s1", Topic: []string{"a", "b"}})
	completes := append(env.waitCompletes(t, "a", 1), env.waitCompletes(t, "b", 1)...)
	if completes[0].ID == completes[1].ID || completes[0].ID == connected.ID || completes[1].ID == connected.ID {
		t.Error(test.DiffMessage([]string{completes[0].ID, completes[1].ID}, "<two message IDs, none the connection's>", "W6: a subscribe frame naming two topics is two messages"))
	}
	for _, e := range completes {
		if len(e.ID) != 16 || e.TraceID != completes[0].TraceID || e.ParentID != connected.ID || e.SpanID != e.ID {
			t.Error(test.DiffMessage(e, "<16-hex ID, same trace, parent = connection, SpanID = ID>", "W6: each message is a root span under the connection"))
		}
	}

	env.send(t, conn, WSPayload{Type: TypePublish, ID: "p1", Topic: []string{"a", "b"}, Message: "hi"})
	mu.Lock()
	defer mu.Unlock()
	if len(scopes) != 2 || scopes[0] == nil || scopes[1] == nil || scopes[0] == scopes[1] || scopes[0].Message == scopes[1].Message {
		t.Error(test.DiffMessage(scopes, "<two distinct message scopes>", "W6: a publish frame naming two topics is two messages"))
	}
}

func TestWS_FramesWithoutDispatchHaveNoMessage(t *testing.T) {
	env := newWSScopeEnv(t)
	conn, _ := env.dial(t, nil)
	env.waitCompletes(t, "/ws", 1)

	if err := websocket.JSON.Send(conn, WSPayload{Type: TypePing, ID: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(conn, WSPayload{Type: TypePong, ID: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(conn, WSPayload{Type: TypeUnsubscribe, ID: "u1", Topic: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(conn, WSPayload{Type: "mystery", ID: "m1"}); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(conn, WSPayload{Type: TypeSubscribe, ID: "s0", Topic: []string{"no.such.topic"}}); err != nil {
		t.Fatal(err)
	}
	env.send(t, conn, WSPayload{Type: TypeSubscribe, ID: "s1", Topic: []string{"a"}})
	env.waitCompletes(t, "a", 1)

	var completes int
	for _, e := range env.rec.all() {
		if e.Stage == trace.StageComplete && e.Transport == trace.TransportWS {
			completes++
		}
	}
	if completes != 1 {
		t.Error(test.DiffMessage(completes, 1, "W6: ping, pong, unsubscribe, an unknown type and an unknown topic have no message ID"))
	}
}

func TestWS_ContextDoneAfterClose(t *testing.T) {
	env := newWSScopeEnv(t)
	kept := make(chan context.Context, 1)
	var once sync.Once
	wsScopeSetHook(func(c *ctx.WSContext) {
		once.Do(func() { kept <- c.Context() })
	})

	conn, _ := env.dial(t, nil)
	env.send(t, conn, WSPayload{Type: TypeSubscribe, ID: "s1", Topic: []string{"a"}})
	env.send(t, conn, WSPayload{Type: TypePublish, ID: "p1", Topic: []string{"a"}, Message: "hi"})
	messageCtx := <-kept

	select {
	case <-messageCtx.Done():
		t.Fatal("W5: the message context is not done while the connection lives")
	default:
	}
	_ = conn.Close()
	select {
	case <-messageCtx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("W5: the message context is done when the connection's handler returns")
	}
}

func TestWS_ScopeAfterClose(t *testing.T) {
	env := newWSScopeEnv(t)
	type kept struct {
		ctx context.Context
		id  string
	}
	var mu sync.Mutex
	var all []kept
	wsScopeSetHook(func(c *ctx.WSContext) {
		mu.Lock()
		all = append(all, kept{ctx: c.Context(), id: c.ID()})
		mu.Unlock()
	})

	conn, connected := env.dial(t, nil)
	env.send(t, conn, WSPayload{Type: TypeSubscribe, ID: "s1", Topic: []string{"a"}})
	for i := range 200 {
		env.send(t, conn, WSPayload{Type: TypePublish, ID: "p" + string(rune('A'+i%26)) + string(rune('a'+i/26)), Topic: []string{"a"}, Message: "hi"})
	}
	mu.Lock()
	first := all[0].ctx
	mu.Unlock()
	_ = conn.Close()
	select {
	case <-first.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the connection never ended")
	}

	mu.Lock()
	defer mu.Unlock()
	seen := map[string]struct{}{}
	for _, k := range all {
		s := tracecontext.ScopeOf(k.ctx)
		if s == nil || s.Message != k.id || s.Connection != connected.ID {
			t.Fatal(test.DiffMessage(s, k.id, "W8: a message context kept after the dispatch and the close reads its own IDs"))
		}
		seen[k.id] = struct{}{}
	}
	if len(all) != 200 || len(seen) != 200 {
		t.Error(test.DiffMessage([]int{len(all), len(seen)}, []int{200, 200}, "W6: 200 publish frames, 200 distinct message IDs"))
	}
}

func TestWS_ConcurrentConnectionsIsolated(t *testing.T) {
	env := newWSScopeEnv(t)
	var wrong sync.Map
	wsScopeSetHook(func(c *ctx.WSContext) {
		s := tracecontext.ScopeOf(c.Context())
		if s == nil || s.Message != c.ID() || s.Connection == "" {
			wrong.Store(c.ID(), true)
		}
	})

	const clients = 8
	conns := make([]*websocket.Conn, clients)
	ids := make([]string, clients)
	for i := range clients {
		conn, connected := env.dial(t, nil)
		conns[i], ids[i] = conn, connected.ID
	}
	var wg sync.WaitGroup
	for i := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sendAndAwaitAck(conns[i], WSPayload{Type: TypeSubscribe, ID: "s", Topic: []string{"a"}}); err != nil {
				t.Error(err)
				return
			}
			for j := range 20 {
				if err := sendAndAwaitAck(conns[i], WSPayload{Type: TypePublish, ID: "p" + string(rune('a'+j)), Topic: []string{"a"}, Message: "hi"}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	seen := map[string]struct{}{}
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	if len(seen) != clients {
		t.Error(test.DiffMessage(len(seen), clients, "S2: every connection has its own ID"))
	}
	wrong.Range(func(k, _ any) bool {
		t.Error(test.DiffMessage(k, "<a message scope with its own ID and a connection>", "W8: concurrent messages never mix scopes"))
		return true
	})
}

func TestWSHandleRequest_UsesScopedRequestID(t *testing.T) {
	ws := newTestWSBare(t)
	var scope *tracecontext.Scope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scopeContext, s := tracecontext.NewRequest(r.Context(), r.Header, time.Now())
		scope = s
		websocket.Handler(ws.handleRequest).ServeHTTP(w, r.WithContext(scopeContext))
	}))
	defer server.Close()

	client, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	var connected WSPayload
	if err := websocket.JSON.Receive(client, &connected); err != nil {
		t.Fatal(err)
	}
	if scope == nil || connected.ID != scope.Request {
		t.Fatal(test.DiffMessage(connected.ID, scope, "W2: handleRequest uses the handshake scope's Request as the connection ID"))
	}
	if _, ok := ws.connmgr.Get(scope.Request); !ok {
		t.Error(test.DiffMessage(ok, true, "W2: WSConnmgr is keyed by that ID"))
	}
}

func TestWS_HandshakeEvents(t *testing.T) {
	lcReset(t)
	env := newWSScopeEnv(t, lcMiddleware{Name: "handshake"})
	resp, closeResp := rawUpgrade(t, env.server.URL, map[string]string{"Traceparent": scopeHeader})
	defer closeResp()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatal(test.DiffMessage(resp.StatusCode, http.StatusSwitchingProtocols, "fixture: the upgrade succeeds"))
	}
	requestID := resp.Header.Get(ctx.RequestID)

	root := env.waitCompletes(t, "/ws", 1)[0]
	if root.ID != requestID || root.SpanID != requestID || root.TraceID != scopeTraceID || root.ParentID != scopeParentID {
		t.Error(test.DiffMessage(root, "<ID = SpanID = X-Request-Id, incoming trace, parent = incoming parent>", "E2: the handshake complete event is the root span of the connection"))
	}
	if root.Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage(root.Transport, trace.TransportHTTP, "E1: the handshake is HTTP"))
	}

	var middleware []trace.Event
	for _, e := range env.rec.all() {
		if e.Stage == trace.StageMiddleware {
			middleware = append(middleware, e)
		}
	}
	if len(middleware) != 1 {
		t.Fatal(test.DiffMessage(len(middleware), 1, "fixture: one handshake middleware event"))
	}
	m := middleware[0]
	if m.ID != requestID || m.TraceID != scopeTraceID || m.ParentID != requestID || len(m.SpanID) != 16 || m.SpanID == requestID {
		t.Error(test.DiffMessage(m, "<ID = request, fresh SpanID, parent = request>", "E3: a handshake stage is a child of the request"))
	}
}

func TestWS_HandshakeEventsOwnTrace(t *testing.T) {
	env := newWSScopeEnv(t)
	_, connected := env.dial(t, nil)
	root := env.waitCompletes(t, "/ws", 1)[0]
	if root.ID != connected.ID || root.ParentID != "" || len(root.TraceID) != 32 || root.SpanID != root.ID {
		t.Error(test.DiffMessage(root, "<own trace, no parent, SpanID = ID>", "E2: without a traceparent the root has no parent"))
	}
}

type hsServer struct {
	app *App
	url string
	rec *traceRecorder
}

func newHandshake(t *testing.T, opts *AppOptions, mws ...common.MiddlewareFn) *hsServer {
	t.Helper()
	lcReset(t)
	srvReset()
	if opts == nil {
		opts = &AppOptions{}
	}
	opts.Logger = &mockLogger{}
	if opts.WS == nil {
		opts.WS = &WSOptions{Path: "/ws"}
	}
	app := New(opts)
	if len(mws) > 0 {
		app.BindGlobalMiddlewares(mws...)
	}
	rec := newTraceRecorder(app)
	app.Create(NewModuleBuilder().Controllers(wsScopeController{}, srvController{}).Build())
	server := httptest.NewServer(app)
	t.Cleanup(func() {
		srvCur.open()
		server.Close()
	})
	return &hsServer{app: app, url: server.URL, rec: rec}
}

func (s *hsServer) handshake(t *testing.T, header map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.url+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	for k, v := range header {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func (s *hsServer) host() string { return strings.TrimPrefix(s.url, "http://") }

func hsExpect(t *testing.T, resp *http.Response, want int, what string) {
	t.Helper()
	if resp.StatusCode != want {
		t.Error(test.DiffMessage(resp.StatusCode, want, what))
	}
}

func TestWSOrigin_NoOriginPasses(t *testing.T) {
	s := newHandshake(t, nil)
	hsExpect(t, s.handshake(t, nil), http.StatusSwitchingProtocols, "W2: no Origin header passes")
}

func TestWSOrigin_SameOriginPasses(t *testing.T) {
	s := newHandshake(t, nil)
	hsExpect(t, s.handshake(t, map[string]string{"Origin": "http://" + s.host()}), http.StatusSwitchingProtocols, "W3: same origin")
	hsExpect(t, s.handshake(t, map[string]string{"Origin": "HTTP://" + strings.ToUpper(s.host())}), http.StatusSwitchingProtocols, "W3: the Origin is lower-cased")
	hsExpect(t, s.handshake(t, map[string]string{"Origin": "http://example.com", "Host": "example.com:80"}), http.StatusSwitchingProtocols, "W3: default port dropped from the host")
	hsExpect(t, s.handshake(t, map[string]string{"Origin": "http://example.com:80", "Host": "example.com"}), http.StatusSwitchingProtocols, "W3: default port dropped from the origin")
}

func TestWSOrigin_SameOriginPassesBehindProxy(t *testing.T) {
	s := newHandshake(t, &AppOptions{Proxy: &ProxyOptions{TrustedProxies: []string{"127.0.0.1"}, IsHostForwarded: true}})
	header := map[string]string{"X-Forwarded-For": "203.0.113.9", "X-Forwarded-Proto": "https", "X-Forwarded-Host": "example.com:443", "Origin": "https://example.com"}
	hsExpect(t, s.handshake(t, header), http.StatusSwitchingProtocols, "W3: scheme and host come from ClientScheme and ClientHost")
}

func TestWSOrigin_SchemeMismatchRefused(t *testing.T) {
	s := newHandshake(t, &AppOptions{Proxy: &ProxyOptions{TrustedProxies: []string{"127.0.0.1"}, IsHostForwarded: true}})
	header := map[string]string{"X-Forwarded-For": "203.0.113.9", "X-Forwarded-Proto": "https", "X-Forwarded-Host": "example.com", "Origin": "http://example.com"}
	hsExpect(t, s.handshake(t, header), http.StatusForbidden, "W5: an http:// origin on an HTTPS request")
}

func TestWSOrigin_ListedPasses(t *testing.T) {
	s := newHandshake(t, &AppOptions{Origins: []string{"https://app.example.com"}})
	hsExpect(t, s.handshake(t, map[string]string{"Origin": "https://app.example.com"}), http.StatusSwitchingProtocols, "W4: a listed origin")
}

func TestWSOrigin_WildcardPasses(t *testing.T) {
	s := newHandshake(t, &AppOptions{Origins: []string{"https://*.example.com"}})
	hsExpect(t, s.handshake(t, map[string]string{"Origin": "https://app.example.com"}), http.StatusSwitchingProtocols, "W4: a wildcard entry")
	hsExpect(t, s.handshake(t, map[string]string{"Origin": "https://evil.com"}), http.StatusForbidden, "W5: outside the wildcard")
}

func TestWSOrigin_StarPasses(t *testing.T) {
	s := newHandshake(t, &AppOptions{Origins: []string{"*"}, CORS: &CORSOptions{IsEnabled: true}})
	hsExpect(t, s.handshake(t, map[string]string{"Origin": "https://anything.example"}), http.StatusSwitchingProtocols, "W4: * passes every origin")
}

func TestWSOrigin_OtherSiteRefused403(t *testing.T) {
	s := newHandshake(t, nil)
	resp := s.handshake(t, map[string]string{"Origin": "https://evil.com"})
	hsExpect(t, resp, http.StatusForbidden, "W5: another site is refused")
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Origin not allowed") {
		t.Error(test.DiffMessage(string(body), "…Origin not allowed…", "W5: the body says why"))
	}
}

func TestWSOrigin_NullRefused(t *testing.T) {
	s := newHandshake(t, nil)
	hsExpect(t, s.handshake(t, map[string]string{"Origin": "null"}), http.StatusForbidden, "W5: Origin null is refused")
}

func TestWSOrigin_TooLongRefused(t *testing.T) {
	s := newHandshake(t, nil)
	hsExpect(t, s.handshake(t, map[string]string{"Origin": "https://" + strings.Repeat("a", 300) + ".com"}), http.StatusForbidden, "W5: an Origin over 256 bytes")
}

func TestWSOrigin_RefusedCounterGrows(t *testing.T) {
	s := newHandshake(t, &AppOptions{IsDevtool: true})
	s.handshake(t, map[string]string{"Origin": "https://evil.com"})
	events := traceStages(s.rec.all(), trace.StagePreRoute)
	var got []trace.Event
	for _, e := range events {
		if e.Name == "origin" {
			got = append(got, e)
		}
	}
	if len(got) != 1 || got[0].Code != http.StatusForbidden {
		t.Error(test.DiffMessage(got, "one origin event with code 403", "W5: the stage reports its refusal"))
	}
}

func TestHandshake_StagesRun(t *testing.T) {
	s := newHandshake(t, &AppOptions{Hosts: []string{"127.0.0.1"}})
	resp := s.handshake(t, map[string]string{"Host": "evil.example"})
	hsExpect(t, resp, http.StatusMisdirectedRequest, "P5: hostcheck refuses the handshake")
	hsExpect(t, s.handshake(t, nil), http.StatusSwitchingProtocols, "P5: an allowed host passes")
}

func TestHandshake_LoadShedRefuses(t *testing.T) {
	s := newHandshake(t, &AppOptions{LoadShed: &LoadShedOptions{IsEnabled: true, MaxInFlight: 1, MaxQueue: -1}})
	res := srvFetchAsync(srvClient(), s.url+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")
	hsExpect(t, s.handshake(t, nil), http.StatusServiceUnavailable, "P5: loadshed refuses the handshake when full")
	srvCur.open()
	<-res
}

func TestHandshake_SkipsHeadersCSRFCompressRedirect(t *testing.T) {
	s := newHandshake(t, &AppOptions{
		Headers:       &HeadersOptions{IsEnabled: true},
		HTTPSRedirect: &HTTPSRedirectOptions{IsEnabled: true},
		Compress:      &CompressOptions{IsEnabled: true},
		CSRF:          &CSRFOptions{IsEnabled: true},
		Origins:       []string{"https://a.com"},
	})
	resp := s.handshake(t, map[string]string{"Accept-Encoding": "gzip"})
	hsExpect(t, resp, http.StatusSwitchingProtocols, "P5: no redirect, no csrf refusal on the handshake")
	if resp.Header.Get("X-Frame-Options") != "" || resp.Header.Get("Content-Encoding") != "" {
		t.Error(test.DiffMessage(resp.Header, "no headers or compress", "P5: headers and compress do not run"))
	}
	names := map[string]bool{}
	for _, e := range traceStages(s.rec.all(), trace.StagePreRoute) {
		names[e.Name] = true
	}
	for _, skipped := range []string{"headers", "csrf", "compress", "httpsredirect", "cors"} {
		if names[skipped] {
			t.Error(test.DiffMessage(skipped, "no event", "P5: stage "+skipped+" does not run on the handshake"))
		}
	}
}

func TestHandshake_GlobalMiddlewaresRunAfterStages(t *testing.T) {
	var order []string
	var mu sync.Mutex
	mark := func(name string) common.MiddlewareFn {
		return &preSpy{override: func(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			next()
		}}
	}
	s := newHandshake(t, &AppOptions{Hosts: []string{"127.0.0.1"}}, mark("first"), mark("second"))
	snapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), order...)
	}
	hsExpect(t, s.handshake(t, nil), http.StatusSwitchingProtocols, "W6: middlewares let the handshake through")
	if got := snapshot(); !slices.Equal(got, []string{"first", "second"}) {
		t.Error(test.DiffMessage(got, []string{"first", "second"}, "W6: in bind order, once per connection"))
	}
	hsExpect(t, s.handshake(t, map[string]string{"Host": "evil.example"}), http.StatusMisdirectedRequest, "W6: a stage refusal")
	if got := snapshot(); len(got) != 2 {
		t.Error(test.DiffMessage(got, []string{"first", "second"}, "W6: user middlewares run after the stages, not on a refused handshake"))
	}
}

func TestHandshake_MiddlewareWithoutNextRefuses(t *testing.T) {
	deny := &preSpy{override: func(_ *http.Request, w http.ResponseWriter, _ ctx.Next) { w.WriteHeader(http.StatusTeapot) }}
	s := newHandshake(t, nil, deny)
	hsExpect(t, s.handshake(t, nil), http.StatusTeapot, "W6: a middleware that does not call next refuses the connection")
}

func TestHandshake_LoadShedReleasedWhenHandshakeEnds(t *testing.T) {
	s := newHandshake(t, &AppOptions{LoadShed: &LoadShedOptions{IsEnabled: true, MaxInFlight: 1, MaxQueue: -1}})
	hsExpect(t, s.handshake(t, nil), http.StatusSwitchingProtocols, "fixture: the first connection is open")
	hsExpect(t, s.handshake(t, nil), http.StatusSwitchingProtocols, "L4: the slot is released when the handshake ends, not when the connection closes")
}

func TestHandshake_LoadShedReleasedWhenMiddlewareRefuses(t *testing.T) {
	spy := &preSpy{override: func(*http.Request, http.ResponseWriter, ctx.Next) {
		panic(exception.ForbiddenException("no"))
	}}
	s := newHandshake(t, &AppOptions{LoadShed: &LoadShedOptions{IsEnabled: true, MaxInFlight: 1, MaxQueue: -1}}, spy)
	for i := 0; i < 3; i++ {
		hsExpect(t, s.handshake(t, nil), http.StatusForbidden, "L4: a handshake refused by a middleware releases its slot")
	}
}
