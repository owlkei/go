package ctx

import (
	"context"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
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
