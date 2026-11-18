package trace

import (
	"net/netip"
	"time"
)

const (
	StageMiddleware      = "middleware"
	StageGuard           = "guard"
	StagePreInterceptor  = "pre_interceptor"
	StagePostInterceptor = "post_interceptor"
	StageExceptionFilter = "exception_filter"
	StagePipe            = "pipe"
	StageHandler         = "handler"
	StageRender          = "render"
	StageComplete        = "complete"
)

const (
	TransportHTTP = "HTTP"
	TransportWS   = "WS"
)

type Event struct {
	ID        string
	TraceID   string
	SpanID    string
	ParentID  string
	Stage     string
	Name      string
	Transport string
	Duration  time.Duration
	Operation string
	Target    string
	Code      int
	ClientIP  netip.Addr
}
