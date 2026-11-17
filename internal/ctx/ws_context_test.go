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
	c.Init(conn)

	if c.Conn != conn {
		t.Error(test.DiffMessage(c.Conn, conn, "Init should store the given connection"))
	}
	if c.Timestamp.IsZero() {
		t.Error(test.DiffMessage(c.Timestamp.IsZero(), false, "Init should set Timestamp"))
	}
	if len(c.GetID()) != 16 || !isLowerHex(c.GetID()) {
		t.Error(test.DiffMessage(c.GetID(), "<16 lowercase hexadecimal characters>", "W5: Init should generate the message id"))
	}
}

func TestWSContext_SetIDRemoved(t *testing.T) {
	if _, ok := reflect.TypeFor[*WSContext]().MethodByName("SetID"); ok {
		t.Error(test.DiffMessage("SetID", "<no such method>", "SetID is removed: the ID comes from the scope built in Init"))
	}
}

func TestWSContext_GetID(t *testing.T) {
	c := NewWSContext()
	if c.GetID() != "" {
		t.Error(test.DiffMessage(c.GetID(), "", "S10: GetID is empty before Init"))
	}
	c.Init(nil)
	s := tracecontext.ScopeOf(c.Context())
	if s == nil || c.GetID() != s.Message {
		t.Error(test.DiffMessage(c.GetID(), s, "W5: GetID is Scope.Message"))
	}
}

func TestWSContext_WSPayload(t *testing.T) {
	c := NewWSContext()
	if c.WSPayload() != nil {
		t.Error(test.DiffMessage(c.WSPayload(), nil, "WSPayload should be nil before SetWSPayload"))
	}
	p := WSPayload{"foo": "bar"}
	c.SetWSPayload(p)
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
	c.Init(nil)
	c.SetContext(context.WithValue(context.Background(), key{}, "v"))
	c.SetWSPayload(WSPayload{"foo": "bar"})
	c.Next = func() {}

	c.Reset()

	if c.GetID() != "" {
		t.Error(test.DiffMessage(c.GetID(), "", "Reset should clear id"))
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
	if c.Next != nil {
		t.Error(test.DiffMessage(c.Next, nil, "Reset should clear Next"))
	}
	if c.Context() != context.Background() {
		t.Error(test.DiffMessage(c.Context(), context.Background(), "Context should fall back to Background after Reset"))
	}
}

func TestWSContext_Reset_EveryFieldIsZeroAfterUse(t *testing.T) {
	type key struct{}
	c := NewWSContext()
	c.Init(&websocket.Conn{})
	c.SetContext(context.WithValue(context.Background(), key{}, "v"))
	c.SetWSPayload(WSPayload{"foo": "bar"})
	c.SetSend(func(any) {})
	c.AddAggregation(aggregation.NewAggregation())
	c.Next = func() {}

	c.Reset()

	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		if name == "Timestamp" {
			continue
		}
		if !v.Field(i).IsZero() {
			t.Error(test.DiffMessage(v.Field(i), "<zero>", "B27: after Reset the WS context should hold no reference to the connection or payload, but "+name+" is set"))
		}
	}
	if len(c.Aggregations()) != 0 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 0, "B36: Reset should drop the aggregations"))
	}
}

func TestWSContext_Aggregations_EmptyByDefault(t *testing.T) {
	c := NewWSContext()
	if len(c.Aggregations()) != 0 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 0, "B36: a new context should have no aggregation"))
	}
}

func TestWSContext_Aggregations_Order(t *testing.T) {
	c := NewWSContext()
	first := aggregation.NewAggregation()
	second := aggregation.NewAggregation()
	c.AddAggregation(first)
	c.AddAggregation(second)

	got := c.Aggregations()
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Error(test.DiffMessage(got, []*aggregation.Aggregation{first, second}, "B36: Aggregations should return the list in the order added"))
	}
}

func TestWSContext_ResetClearsAggregations(t *testing.T) {
	c := NewWSContext()
	c.Init(&websocket.Conn{})
	c.AddAggregation(aggregation.NewAggregation())
	c.Reset()
	if len(c.Aggregations()) != 0 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 0, "B36: Reset should empty the aggregation list"))
	}

	again := aggregation.NewAggregation()
	c.Init(&websocket.Conn{})
	c.AddAggregation(again)
	got := c.Aggregations()
	if len(got) != 1 || got[0] != again {
		t.Error(test.DiffMessage(got, []*aggregation.Aggregation{again}, "B36: a context reused after Reset should start with no aggregation"))
	}
}
