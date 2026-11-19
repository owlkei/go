package ctx

import (
	"context"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"go.owlky.com/owlky/internal/aggregation"
	"go.owlky.com/owlky/internal/test"
	"go.owlky.com/owlky/internal/tracecontext"
	"golang.org/x/net/websocket"
)

func TestWSContext_Init(t *testing.T) {
	c := NewWSContext()
	conn := &websocket.Conn{}
	WSInit(c, conn)

	if c.Conn != conn {
		t.Error(test.DiffMessage(c.Conn, conn, "Init should store the given connection"))
	}
	if WSTimestamp(c).IsZero() {
		t.Error(test.DiffMessage(WSTimestamp(c).IsZero(), false, "Init should set Timestamp"))
	}
	if len(c.ID()) != 16 || !isLowerHex(c.ID()) {
		t.Error(test.DiffMessage(c.ID(), "<16 lowercase hexadecimal characters>", "W5: Init should generate the message id"))
	}
}

func TestWSContext_SetIDRemoved(t *testing.T) {
	if _, ok := reflect.TypeFor[*WSContext]().MethodByName("SetID"); ok {
		t.Error(test.DiffMessage("SetID", "<no such method>", "SetID is removed: the ID comes from the scope built in Init"))
	}
}

func TestWSContext_GetID(t *testing.T) {
	c := NewWSContext()
	if c.ID() != "" {
		t.Error(test.DiffMessage(c.ID(), "", "S10: GetID is empty before Init"))
	}
	WSInit(c, nil)
	s := tracecontext.ScopeOf(c.Context())
	if s == nil || c.ID() != s.Message {
		t.Error(test.DiffMessage(c.ID(), s, "W5: GetID is Scope.Message"))
	}
}

func TestWSContext_WSPayload(t *testing.T) {
	c := NewWSContext()
	if c.WSPayload() != nil {
		t.Error(test.DiffMessage(c.WSPayload(), nil, "WSPayload should be nil before SetWSPayload"))
	}
	p := WSPayload{"foo": "bar"}
	WSSetPayload(c, p)
	if c.WSPayload()["foo"] != "bar" {
		t.Error(test.DiffMessage(c.WSPayload(), p, "WSPayload should return what SetWSPayload stored"))
	}
}

func TestWSContext_ContextDefaultsToBackground(t *testing.T) {
	c := NewWSContext()
	if c.Context() != context.Background() {
		t.Error(test.DiffMessage(c.Context(), context.Background(), "Context should default to context.Background()"))
	}
}

func TestWSContext_SetContext(t *testing.T) {
	c := NewWSContext()
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "v")
	c.SetContext(ctx)
	if c.Context() != ctx {
		t.Error(test.DiffMessage(c.Context(), ctx, "Context should return the context set by SetContext"))
	}
}

func TestWSContext_Reset(t *testing.T) {
	c := NewWSContext()
	type key struct{}
	WSInit(c, nil)
	c.SetContext(context.WithValue(context.Background(), key{}, "v"))
	WSSetPayload(c, WSPayload{"foo": "bar"})
	WSSetNext(c, func() {})

	WSReset(c)

	if c.ID() != "" {
		t.Error(test.DiffMessage(c.ID(), "", "Reset should clear id"))
	}
	if c.ctx != nil {
		t.Error(test.DiffMessage(c.ctx, nil, "Reset should clear ctx"))
	}
	if c.Conn != nil {
		t.Error(test.DiffMessage(c.Conn, nil, "Reset should clear Conn"))
	}
	if c.payload != nil {
		t.Error(test.DiffMessage(c.payload, nil, "Reset should clear payload"))
	}
	if WSNext(c) != nil {
		t.Error(test.DiffMessage(WSNext(c), nil, "Reset should clear Next"))
	}
	if c.Context() != context.Background() {
		t.Error(test.DiffMessage(c.Context(), context.Background(), "Context should fall back to Background after Reset"))
	}
}

func TestWSContext_Reset_EveryFieldIsZeroAfterUse(t *testing.T) {
	type key struct{}
	c := NewWSContext()
	WSInit(c, &websocket.Conn{})
	c.SetContext(context.WithValue(context.Background(), key{}, "v"))
	WSSetPayload(c, WSPayload{"foo": "bar"})
	WSSetSend(c, func(any) {})
	WSAddAggregation(c, aggregation.NewAggregation())
	WSSetNext(c, func() {})

	WSReset(c)

	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		if name == "timestamp" {
			continue
		}
		if !v.Field(i).IsZero() {
			t.Error(test.DiffMessage(v.Field(i), "<zero>", "B27: after Reset the WS context should hold no reference to the connection or payload, but "+name+" is set"))
		}
	}
	if len(WSAggregations(c)) != 0 {
		t.Error(test.DiffMessage(len(WSAggregations(c)), 0, "B36: Reset should drop the aggregations"))
	}
}

func TestWSContext_Aggregations_EmptyByDefault(t *testing.T) {
	c := NewWSContext()
	if len(WSAggregations(c)) != 0 {
		t.Error(test.DiffMessage(len(WSAggregations(c)), 0, "B36: a new context should have no aggregation"))
	}
}

func TestWSContext_Aggregations_Order(t *testing.T) {
	c := NewWSContext()
	first := aggregation.NewAggregation()
	second := aggregation.NewAggregation()
	WSAddAggregation(c, first)
	WSAddAggregation(c, second)

	got := WSAggregations(c)
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Error(test.DiffMessage(got, []*aggregation.Aggregation{first, second}, "B36: Aggregations should return the list in the order added"))
	}
}

func TestWSContext_ResetClearsAggregations(t *testing.T) {
	c := NewWSContext()
	WSInit(c, &websocket.Conn{})
	WSAddAggregation(c, aggregation.NewAggregation())
	WSReset(c)
	if len(WSAggregations(c)) != 0 {
		t.Error(test.DiffMessage(len(WSAggregations(c)), 0, "B36: Reset should empty the aggregation list"))
	}

	again := aggregation.NewAggregation()
	WSInit(c, &websocket.Conn{})
	WSAddAggregation(c, again)
	got := WSAggregations(c)
	if len(got) != 1 || got[0] != again {
		t.Error(test.DiffMessage(got, []*aggregation.Aggregation{again}, "B36: a context reused after Reset should start with no aggregation"))
	}
}

func TestWSContext_ID(t *testing.T) {
	c := NewWSContext()
	if c.ID() != "" {
		t.Error(test.DiffMessage(c.ID(), "", "B16: ID is empty before WSInit"))
	}
	WSInit(c, nil)
	if len(c.ID()) != 16 || !isLowerHex(c.ID()) {
		t.Error(test.DiffMessage(c.ID(), "<16 lowercase hexadecimal characters>", "B16: ID returns what GetID returned"))
	}
}

func TestWSHooks(t *testing.T) {
	c := NewWSContext()
	WSInit(c, &websocket.Conn{})
	if WSTimestamp(c).IsZero() {
		t.Error(test.DiffMessage(WSTimestamp(c).IsZero(), false, "B18: WSInit sets the timestamp"))
	}
	var sent any
	WSSetSend(c, func(data any) { sent = data })
	c.Send("x")
	if sent != "x" {
		t.Error(test.DiffMessage(sent, "x", "B18: WSSetSend sets the send function"))
	}
	WSSetPayload(c, WSPayload{"k": "v"})
	if !reflect.DeepEqual(c.WSPayload(), WSPayload{"k": "v"}) {
		t.Error(test.DiffMessage(c.WSPayload(), WSPayload{"k": "v"}, "B18: WSSetPayload sets the payload"))
	}
	WSSetNext(c, func() {})
	if WSNext(c) == nil {
		t.Error(test.DiffMessage(nil, "<func>", "B18: WSSetNext is read back by WSNext"))
	}
	a := aggregation.NewAggregation()
	WSAddAggregation(c, a)
	if got := WSAggregations(c); len(got) != 1 || got[0] != a {
		t.Error(test.DiffMessage(got, []*aggregation.Aggregation{a}, "B18: WSAddAggregation and WSAggregations"))
	}
	WSReset(c)
	if WSNext(c) != nil || len(WSAggregations(c)) != 0 || c.ID() != "" || c.WSPayload() != nil {
		t.Error(test.DiffMessage(c, NewWSContext(), "B18: WSReset leaves the context as a new one"))
	}
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
