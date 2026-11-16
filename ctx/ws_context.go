package ctx

import (
	"context"
	"time"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"golang.org/x/net/websocket"
)

type WSContext struct {
	*websocket.Conn

	scope *tracecontext.Scope
	ctx   context.Context

	Next Next

	payload      WSPayload
	aggregations []*aggregation.Aggregation
	Timestamp    time.Time

	send func(data any)
}

func NewWSContext() *WSContext {
	return &WSContext{}
}

func (c *WSContext) Init(conn *websocket.Conn) {
	c.Timestamp = time.Now()
	c.Conn = conn
	c.ctx, c.scope = tracecontext.NewMessage(connectionContext(conn), c.Timestamp)
}

func connectionContext(conn *websocket.Conn) context.Context {
	if conn == nil {
		return context.Background()
	}
	r := conn.Request()
	if r == nil {
		return context.Background()
	}
	return r.Context()
}

func (c *WSContext) GetID() string {
	if c.scope == nil {
		return ""
	}
	return c.scope.Message
}

func (c *WSContext) Reset() {
	c.scope = nil
	c.ctx = nil
	c.Next = nil
	c.Conn = nil
	c.payload = nil
	c.aggregations = nil
	c.send = nil
}

func (c *WSContext) AddAggregation(a *aggregation.Aggregation) {
	c.aggregations = append(c.aggregations, a)
}

func (c *WSContext) Aggregations() []*aggregation.Aggregation {
	return c.aggregations
}

func (c *WSContext) SetSend(fn func(data any)) {
	c.send = fn
}

func (c *WSContext) Send(data any) {
	if c.send != nil {
		c.send(data)
	}
}

func (c *WSContext) WSPayload() WSPayload {
	return c.payload
}

func (c *WSContext) SetWSPayload(p WSPayload) {
	c.payload = p
}

func (c *WSContext) Context() context.Context {
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

func (c *WSContext) SetContext(ctx context.Context) {
	if ctx == nil {
		return
	}
	if c.scope != nil && tracecontext.ScopeOf(ctx) != c.scope {
		ctx = tracecontext.With(ctx, c.scope)
	}
	c.ctx = ctx
}
