package ctx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"golang.org/x/net/websocket"
)

const (
	scopeTraceID  = "4bf92f3577b34da6a3ce929d0e0e4736"
	scopeParentID = "00f067aa0ba902b7"
	scopeHeader   = "00-" + scopeTraceID + "-" + scopeParentID + "-01"
)

func serverConnWithScope(t *testing.T, traceparent string) (*websocket.Conn, *tracecontext.Scope, func()) {
	t.Helper()
	connCh := make(chan *websocket.Conn, 1)
	scopeCh := make(chan *tracecontext.Scope, 1)
	done := make(chan struct{})
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		scopeContext, scope := tracecontext.NewRequest(r.Context(), r.Header, time.Now())
		scopeCh <- scope
		websocket.Handler(func(c *websocket.Conn) {
			connCh <- c
			<-done
		}).ServeHTTP(w, r.WithContext(scopeContext))
	}))
	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http"), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if traceparent != "" {
		config.Header.Set("Traceparent", traceparent)
	}
	client, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	conn := <-connCh
	scope := <-scopeCh

	var once sync.Once
	release := func() {
		once.Do(func() { close(done) })
		<-finished
	}
	t.Cleanup(func() {
		release()
		_ = client.Close()
		server.Close()
	})
	return conn, scope, release
}

func TestWSContext_Init_NilConn(t *testing.T) {
	c := NewWSContext()
	if !noPanic(t, "W4: Init(nil)", func() { WSInit(c, nil) }) {
		return
	}
	s := tracecontext.ScopeOf(c.Context())
	if s == nil {
		t.Fatal(test.DiffMessage(s, "<a message scope>", "W4: a nil connection still gets a scope"))
	}
	if len(s.TraceID) != 32 || len(s.Message) != 16 || c.ID() != s.Message {
		t.Error(test.DiffMessage([]any{s.TraceID, s.Message}, "<a new trace ID and a message ID>", "W4: a new trace and a message ID"))
	}
	if s.Connection != "" || s.Parent != "" {
		t.Error(test.DiffMessage([]string{s.Connection, s.Parent}, []string{"", ""}, "W4: no connection, no parent"))
	}
}

func TestWSContext_Init_ConnWithoutRequest(t *testing.T) {
	c := NewWSContext()
	if !noPanic(t, "W4: a connection built by hand has no request", func() { WSInit(c, &websocket.Conn{}) }) {
		return
	}
	s := tracecontext.ScopeOf(c.Context())
	if s == nil || s.Connection != "" || s.Parent != "" || len(s.TraceID) != 32 {
		t.Error(test.DiffMessage(s, "<a scope with a new trace and no connection>", "W4: a nil request gives a new trace ID"))
	}
}

func TestWSContext_Init_RequestWithoutScope(t *testing.T) {
	server, client, cleanup := newPlainConnPair(t)
	defer cleanup()
	_ = client
	c := NewWSContext()
	WSInit(c, server)
	s := tracecontext.ScopeOf(c.Context())
	if s == nil || s.Connection != "" || s.Parent != "" || len(s.TraceID) != 32 || len(s.Message) != 16 {
		t.Error(test.DiffMessage(s, "<a scope with a new trace and no connection>", "W4: a parent without a scope gives a new trace ID"))
	}
}

func newPlainConnPair(t *testing.T) (*websocket.Conn, *websocket.Conn, func()) {
	t.Helper()
	connCh := make(chan *websocket.Conn, 1)
	done := make(chan struct{})
	server := httptest.NewServer(websocket.Handler(func(c *websocket.Conn) {
		connCh <- c
		<-done
	}))
	client, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn := <-connCh
	return conn, client, func() {
		close(done)
		_ = client.Close()
		server.Close()
	}
}

func TestWSContext_Init_ContextCarriesMessageScope(t *testing.T) {
	conn, connScope, _ := serverConnWithScope(t, scopeHeader)
	c := NewWSContext()
	WSInit(c, conn)

	s := tracecontext.ScopeOf(c.Context())
	if s == nil {
		t.Fatal(test.DiffMessage(s, "<a message scope>", "W5: c.Context() carries the message scope"))
	}
	if s == connScope {
		t.Error(test.DiffMessage(s, "<a scope of its own>", "W4: the message scope is not the connection scope"))
	}
	if s.TraceID != scopeTraceID || s.TraceID != connScope.TraceID {
		t.Error(test.DiffMessage(s.TraceID, scopeTraceID, "W4: the message continues the connection's trace"))
	}
	if s.Connection != connScope.Request || s.Parent != connScope.Request {
		t.Error(test.DiffMessage([]string{s.Connection, s.Parent}, []string{connScope.Request, connScope.Request}, "W4: Connection and Parent are the connection ID"))
	}
	if c.ID() != s.Message || len(s.Message) != 16 || s.Message == connScope.Request {
		t.Error(test.DiffMessage(c.ID(), "<a message ID, not the connection ID>", "W5: GetID is Scope.Message"))
	}
	if s.Request != "" || s.Upstream != "" {
		t.Error(test.DiffMessage([]string{s.Request, s.Upstream}, []string{"", ""}, "W4: Request and Upstream are empty"))
	}
	if _, _, flags, ok := tracecontext.ParseTraceparent(s.Traceparent()); !ok || flags != 0x01 {
		t.Error(test.DiffMessage(s.Traceparent(), "<flags 01 of the connection>", "W4: the flags are the connection's"))
	}
}

func TestWSContext_Init_TwoMessagesTwoIDs(t *testing.T) {
	conn, _, _ := serverConnWithScope(t, "")
	a, b := NewWSContext(), NewWSContext()
	WSInit(a, conn)
	WSInit(b, conn)
	if a.ID() == b.ID() {
		t.Error(test.DiffMessage(a.ID(), "<a different ID>", "W6: every message has its own ID"))
	}
	if tracecontext.ScopeOf(a.Context()).TraceID != tracecontext.ScopeOf(b.Context()).TraceID {
		t.Error(test.DiffMessage(tracecontext.ScopeOf(b.Context()).TraceID, tracecontext.ScopeOf(a.Context()).TraceID, "W4: messages of one connection share its trace"))
	}
}

func TestWSContext_Init_ContextDoneWhenHandlerReturns(t *testing.T) {
	conn, _, release := serverConnWithScope(t, "")
	c := NewWSContext()
	WSInit(c, conn)
	kept := c.Context()
	select {
	case <-kept.Done():
		t.Fatal("W5: the message context is not done while the connection's handler runs")
	default:
	}
	release()
	select {
	case <-kept.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("W5: the message context is done when the connection's handler returns")
	}
	if kept.Err() != context.Canceled {
		t.Error(test.DiffMessage(kept.Err(), context.Canceled, "W5: Err after the handler returned"))
	}
}

func TestWSContext_ContextScopeSurvivesResetAndClose(t *testing.T) {
	conn, connScope, release := serverConnWithScope(t, "")
	c := NewWSContext()
	WSInit(c, conn)
	kept := c.Context()
	id := c.ID()

	WSReset(c)
	WSInit(c, conn)
	release()

	s := tracecontext.ScopeOf(kept)
	if s == nil || s.Message != id || s.Connection != connScope.Request {
		t.Error(test.DiffMessage(s, id, "W8: a kept message context reads its IDs after Reset, reuse and close"))
	}
	if c.ID() == id {
		t.Error(test.DiffMessage(c.ID(), "<the second message's ID>", "W8: the reused context has a new ID"))
	}
}

func TestWSContext_SetContextKeepsScope(t *testing.T) {
	conn, _, _ := serverConnWithScope(t, "")
	c := NewWSContext()
	WSInit(c, conn)
	scope := tracecontext.ScopeOf(c.Context())
	id := c.ID()

	type key struct{}
	replacement := context.WithValue(context.Background(), key{}, "v")
	c.SetContext(replacement)

	if tracecontext.ScopeOf(c.Context()) != scope {
		t.Error(test.DiffMessage(tracecontext.ScopeOf(c.Context()), scope, "W7: SetContext with a context that has no scope keeps the message scope"))
	}
	if c.Context().Value(key{}) != "v" {
		t.Error(test.DiffMessage(c.Context().Value(key{}), "v", "W7: the replacement's values stay readable"))
	}
	if c.ID() != id {
		t.Error(test.DiffMessage(c.ID(), id, "W7: GetID never changes after Init"))
	}
}

func TestWSContext_SetContextOtherScopeIsReplaced(t *testing.T) {
	conn, _, _ := serverConnWithScope(t, "")
	c := NewWSContext()
	WSInit(c, conn)
	scope := tracecontext.ScopeOf(c.Context())

	other, otherScope := tracecontext.NewRequest(context.Background(), http.Header{}, time.Now())
	c.SetContext(other)
	if got := tracecontext.ScopeOf(c.Context()); got != scope || got == otherScope {
		t.Error(test.DiffMessage(got, scope, "W7: a context with another scope is wrapped so this message's scope stays the nearest"))
	}
}

func TestWSContext_SetContextDerivedUnchanged(t *testing.T) {
	conn, _, _ := serverConnWithScope(t, "")
	c := NewWSContext()
	WSInit(c, conn)

	type key struct{}
	derived := context.WithValue(c.Context(), key{}, "v")
	c.SetContext(derived)
	if c.Context() != derived {
		t.Error(test.DiffMessage(c.Context(), derived, "W7: a context derived from the message context is stored as it is"))
	}
}

func TestWSContext_SetContextNilKeepsScope(t *testing.T) {
	conn, _, _ := serverConnWithScope(t, "")
	c := NewWSContext()
	WSInit(c, conn)
	scope := tracecontext.ScopeOf(c.Context())
	var nilContext context.Context
	if !noPanic(t, "W7: SetContext(nil)", func() { c.SetContext(nilContext) }) {
		return
	}
	if c.Context() == nil || tracecontext.ScopeOf(c.Context()) != scope {
		t.Error(test.DiffMessage(tracecontext.ScopeOf(c.Context()), scope, "W7: SetContext(nil) never loses the scope"))
	}
}

func TestWSContext_ContextBackgroundAfterReset(t *testing.T) {
	c := NewWSContext()
	if c.Context() != context.Background() {
		t.Error(test.DiffMessage(c.Context(), context.Background(), "W5: before Init Context is Background"))
	}
	WSInit(c, nil)
	if c.Context() == context.Background() {
		t.Error(test.DiffMessage(c.Context(), "<the message context>", "W5: after Init Context is no longer Background"))
	}
	WSReset(c)
	if c.Context() != context.Background() {
		t.Error(test.DiffMessage(c.Context(), context.Background(), "W5: after Reset Context is Background again"))
	}
}

func TestWSContext_Init_ConcurrentContextsIsolated(t *testing.T) {
	conn, _, _ := serverConnWithScope(t, "")
	const goroutines = 16
	const rounds = 200
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]struct{}{}
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := NewWSContext()
			var kept []context.Context
			var ids []string
			for i := 0; i < rounds; i++ {
				WSInit(c, conn)
				kept = append(kept, c.Context())
				ids = append(ids, c.ID())
				WSReset(c)
			}
			for i, k := range kept {
				if s := tracecontext.ScopeOf(k); s == nil || s.Message != ids[i] {
					t.Error(test.DiffMessage(s, ids[i], "W8: every kept message context reads its own ID"))
					return
				}
			}
			mu.Lock()
			for _, id := range ids {
				seen[id] = struct{}{}
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != goroutines*rounds {
		t.Error(test.DiffMessage(len(seen), goroutines*rounds, "W6: message IDs are unique"))
	}
}
