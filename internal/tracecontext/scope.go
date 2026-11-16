package tracecontext

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	KeyTraceID    = "trace_id"
	KeyRequest    = "request"
	KeyConnection = "connection"
	KeyMessage    = "message"
	KeyUpstream   = "upstream"
)

const (
	traceparentHeader = "Traceparent"
	requestIDHeader   = "X-Request-Id"
	ownFlags          = 0x03
	knownFlags        = 0x03
	maxUpstreamLength = 64
	hexDigits         = "0123456789abcdef"
)

type Scope struct {
	TraceID    string
	Request    string
	Connection string
	Message    string
	Upstream   string
	Parent     string
	LogSlot    atomic.Value

	traceparent string
	headers     [2]string
}

type scopeKey struct{}

type scopeContext struct {
	context.Context
	scope *Scope
}

func (c *scopeContext) Value(key any) any {
	if _, ok := key.(scopeKey); ok {
		return c.scope
	}
	return c.Context.Value(key)
}

type ownedScope struct {
	context.Context
	value Scope
}

func (c *ownedScope) Value(key any) any {
	if _, ok := key.(scopeKey); ok {
		return &c.value
	}
	return c.Context.Value(key)
}

func NewRequest(parent context.Context, h http.Header, now time.Time) (context.Context, *Scope) {
	o := &ownedScope{}
	s := &o.value
	var text [traceparentLength]byte
	flags := byte(ownFlags)
	if values := h[traceparentHeader]; len(values) == 1 {
		if traceID, parentID, incoming, ok := ParseTraceparent(values[0]); ok {
			copy(text[traceIDStart:traceIDEnd], traceID)
			s.Parent = parentID
			flags = incoming & knownFlags
		}
	}
	if s.Parent == "" {
		putTraceID(text[traceIDStart:traceIDEnd], now)
	}
	putSpanID(text[parentIDStart:parentIDEnd])
	s.setTraceparent(&text, flags)
	s.Request = s.traceparent[parentIDStart:parentIDEnd]
	s.Upstream = s.Parent
	if s.Upstream == "" {
		s.Upstream = upstreamRequestID(h[requestIDHeader])
	}
	s.headers = [2]string{s.Request, s.traceparent}
	o.Context = parent
	return o, s
}

func NewMessage(parent context.Context, now time.Time) (context.Context, *Scope) {
	o := &ownedScope{}
	s := &o.value
	var text [traceparentLength]byte
	flags := byte(ownFlags)
	if conn := ScopeOf(parent); conn != nil && len(conn.traceparent) == traceparentLength {
		copy(text[traceIDStart:traceIDEnd], conn.TraceID)
		flags, _ = hexByte(conn.traceparent[flagsStart], conn.traceparent[flagsStart+1])
		s.Connection = conn.Request
		s.Parent = conn.Request
	} else {
		putTraceID(text[traceIDStart:traceIDEnd], now)
		parent = context.Background()
	}
	putSpanID(text[parentIDStart:parentIDEnd])
	s.setTraceparent(&text, flags)
	s.Message = s.traceparent[parentIDStart:parentIDEnd]
	o.Context = parent
	return o, s
}

func (s *Scope) setTraceparent(text *[traceparentLength]byte, flags byte) {
	text[0], text[1], text[2] = '0', '0', '-'
	text[traceIDEnd] = '-'
	text[parentIDEnd] = '-'
	text[flagsStart] = hexDigits[flags>>4]
	text[flagsStart+1] = hexDigits[flags&0x0f]
	s.traceparent = string(text[:])
	s.TraceID = s.traceparent[traceIDStart:traceIDEnd]
}

func upstreamRequestID(values []string) string {
	if len(values) != 1 {
		return ""
	}
	v := values[0]
	if len(v) == 0 || len(v) > maxUpstreamLength {
		return ""
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return ""
		}
	}
	return v
}

func With(parent context.Context, s *Scope) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return &scopeContext{Context: parent, scope: s}
}

func ScopeOf(c context.Context) *Scope {
	if c == nil {
		return nil
	}
	s, _ := c.Value(scopeKey{}).(*Scope)
	return s
}

func (s *Scope) AppendArgs(dst []any) []any {
	if s == nil {
		return dst
	}
	dst = appendArg(dst, KeyTraceID, s.TraceID)
	dst = appendArg(dst, KeyRequest, s.Request)
	dst = appendArg(dst, KeyConnection, s.Connection)
	dst = appendArg(dst, KeyMessage, s.Message)
	return appendArg(dst, KeyUpstream, s.Upstream)
}

func appendArg(dst []any, key, value string) []any {
	if value == "" {
		return dst
	}
	return append(dst, key, value)
}

func (s *Scope) Traceparent() string {
	return s.traceparent
}

func (s *Scope) SpanID() string {
	if s.Request != "" {
		return s.Request
	}
	return s.Message
}

func (s *Scope) Headers() (requestID, traceparent []string) {
	return s.headers[0:1:1], s.headers[1:2:2]
}
