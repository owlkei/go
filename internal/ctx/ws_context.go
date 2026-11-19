package ctx

import (
	"context"
	"time"

	"go.owlky.com/owlky/internal/aggregation"
	"go.owlky.com/owlky/internal/tracecontext"
	"golang.org/x/net/websocket"
)

type WSContext struct {
	*websocket.Conn

	scope *tracecontext.Scope
	ctx   context.Context

	next Next

	payload      WSPayload
	aggregations []*aggregation.Aggregation
	timestamp    time.Time

	send func(data any)
}

func NewWSContext() *WSContext {
	return &WSContext{}
}

func WSInit(c *WSContext, conn *websocket.Conn) {
	c.timestamp = time.Now()
	c.Conn = conn
	c.ctx, c.scope = tracecontext.NewMessage(connectionContext(conn), c.timestamp)
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

func (c *WSContext) ID() string {
	if c.scope == nil {
		return ""
	}
	return c.scope.Message
}

func WSReset(c *WSContext) {
	c.scope = nil
	c.ctx = nil
	c.next = nil
	c.Conn = nil
	c.payload = nil
	c.aggregations = nil
	c.send = nil
}

func WSAddAggregation(c *WSContext, a *aggregation.Aggregation) {
	c.aggregations = append(c.aggregations, a)
}

func WSAggregations(c *WSContext) []*aggregation.Aggregation {
	return c.aggregations
}

func WSNext(c *WSContext) Next {
	return c.next
}

func WSSetNext(c *WSContext, next Next) {
	c.next = next
}

func WSTimestamp(c *WSContext) time.Time {
	return c.timestamp
}

func WSSetSend(c *WSContext, fn func(data any)) {
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

func WSSetPayload(c *WSContext, p WSPayload) {
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
