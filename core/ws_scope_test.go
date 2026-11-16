package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"github.com/dangduoc08/ginject/trace"
	"golang.org/x/net/websocket"
)

type wsScopeFixture struct {
	mu   sync.Mutex
	hook func(*ctx.WSContext)
}

var wsScope wsScopeFixture

func wsScopeSetHook(fn func(*ctx.WSContext)) {
	wsScope.mu.Lock()
	defer wsScope.mu.Unlock()
	wsScope.hook = fn
}

func wsScopeCall(c *ctx.WSContext) {
	wsScope.mu.Lock()
	fn := wsScope.hook
	wsScope.mu.Unlock()
	if fn != nil {
		fn(c)
	}
}

type wsScopeController struct{ common.WS }

func (c wsScopeController) NewController() Controller { return c }

func (c wsScopeController) SUBSCRIBE_a(wc *ctx.WSContext) string {
	wsScopeCall(wc)
	return "a"
}

func (c wsScopeController) SUBSCRIBE_b(wc *ctx.WSContext) string {
	wsScopeCall(wc)
	return "b"
}

type wsScopeEnv struct {
	app    *App
	server *httptest.Server
	rec    *traceRecorder
}

func newWSScopeEnv(t *testing.T, middlewares ...common.MiddlewareFn) *wsScopeEnv {
	t.Helper()
	lcReset(t)
	wsScopeSetHook(nil)
	t.Cleanup(func() { wsScopeSetHook(nil) })
	app := New()
	app.SetLogger(&mockLogger{})
	rec := newTraceRecorder(app)
	app.EnableWS(&WSConfig{Path: "/ws"}, middlewares...)
	app.Create(ModuleBuilder().Controllers(wsScopeController{}, traceWSAllController{}).Build())
	server := httptest.NewServer(app)
	t.Cleanup(server.Close)
	return &wsScopeEnv{app: app, server: server, rec: rec}
}

func (e *wsScopeEnv) dial(t *testing.T, header map[string]string) (*websocket.Conn, WSPayload) {
	t.Helper()
	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(e.server.URL, "http")+"/ws", e.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		config.Header.Set(k, v)
	}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var connected WSPayload
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatal(err)
	}
	if connected.Type != TypeConnected {
		t.Fatal(test.DiffMessage(connected.Type, TypeConnected, "fixture: the first frame is connected"))
	}
	return conn, connected
}

func sendAndAwaitAck(conn *websocket.Conn, p WSPayload) error {
	if err := websocket.JSON.Send(conn, p); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	for {
		var reply WSPayload
		if err := websocket.JSON.Receive(conn, &reply); err != nil {
			return fmt.Errorf("waiting for the ack of %q: %w", p.ID, err)
		}
		if reply.ID == p.ID && reply.Type == TypeAck {
			return nil
		}
		if reply.ID == p.ID && reply.Type == TypeError {
			return fmt.Errorf("the frame %q was refused: %v", p.ID, reply.Message)
		}
	}
}

func (e *wsScopeEnv) send(t *testing.T, conn *websocket.Conn, p WSPayload) {
	t.Helper()
	if err := sendAndAwaitAck(conn, p); err != nil {
		t.Fatal(err)
	}
}

func (e *wsScopeEnv) waitCompletes(t *testing.T, target string, n int) []trace.Event {
	t.Helper()
	var out []trace.Event
	timeout := time.After(10 * time.Second)
	for len(out) < n {
		select {
		case ev := <-e.rec.ch:
			if ev.Stage == trace.StageComplete && ev.Target == target {
				out = append(out, ev)
			}
		case <-timeout:
			t.Fatalf("got %d of %d complete events for %q", len(out), n, target)
		}
	}
	return out
}

func rawUpgrade(t *testing.T, base string, header map[string]string) (*http.Response, func()) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", base)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	transport := &http.Transport{DisableKeepAlives: true}
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp, func() {
		_ = resp.Body.Close()
		transport.CloseIdleConnections()
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
		got = append(got, seen{scope: tracecontext.ScopeOf(c.Context()), id: c.GetID()})
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
		t.Error(test.DiffMessage(got[0].id, s.Message, "W5: c.GetID() is the message ID, which is not the connection's"))
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
		all = append(all, kept{ctx: c.Context(), id: c.GetID()})
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
		if s == nil || s.Message != c.GetID() || s.Connection == "" {
			wrong.Store(c.GetID(), true)
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
