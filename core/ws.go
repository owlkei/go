package core

import (
	"errors"
	stdHTTP "net/http"
	"reflect"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/str"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"github.com/dangduoc08/ginject/memorybroker"
	"github.com/dangduoc08/ginject/trace"
	"github.com/dangduoc08/ginject/wsevent"
	"golang.org/x/net/websocket"
)

var errWSHandshakeRejected = errors.New("ws handshake rejected: middleware chain did not call next()")

type WSConfig struct {
	Path              string
	globalMiddlewares []common.MiddlewareFn
	injectedProviders map[string]Provider
	container         *container
	logger            common.Logger
	stacks            *stackSampler
	tracer            *trace.Tracer
	broker            *memorybroker.Broker

	resolveAndCallHandler func(pattern string, f any, c *ctx.WSContext) []reflect.Value
	newCtx                func() *ctx.WSContext
	releaseCtx            func(c *ctx.WSContext)
}

type WS struct {
	catchFnsByEvent       map[string][]common.WSCatch
	resolveAndCallHandler func(pattern string, f any, c *ctx.WSContext) []reflect.Value
	connmgr               *WSConnmgr
	path                  string
	globalMiddlewares     []ctx.HTTPHandler
	injectedProviders     map[string]Provider
	logger                common.Logger
	stacks                *stackSampler
	eventMatcher          *wsevent.WSEvent
	newCtx                func() *ctx.WSContext
	releaseCtx            func(c *ctx.WSContext)
	emitPostInterceptor   func(c *ctx.WSContext, name string, duration time.Duration)
	emitComplete          func(c *ctx.WSContext, operation, target string)
}

func NewWS(cfg *WSConfig) *WS {
	path := str.Enclose("ws", '/')
	if cfg.Path != "" {
		path = str.Enclose(cfg.Path, '/')
	}

	ws := WS{
		catchFnsByEvent:       make(map[string][]common.WSCatch),
		resolveAndCallHandler: cfg.resolveAndCallHandler,
		eventMatcher:          wsevent.NewWSEvent(),
		connmgr:               NewWSConnmgr(cfg.logger, cfg.broker),
		path:                  path,
		globalMiddlewares:     resolveGlobalMiddlewares(cfg.globalMiddlewares, cfg.injectedProviders, cfg.container, cfg.tracer),
		injectedProviders:     cfg.injectedProviders,
		logger:                cfg.logger,
		stacks:                cfg.stacks,
		newCtx:                cfg.newCtx,
		releaseCtx:            cfg.releaseCtx,
	}

	ws.connmgr.startDeadConnDetection(15*time.Second, 60*time.Second)

	return &ws
}

func resolveGlobalMiddlewares(middlewares []common.MiddlewareFn, injectedProviders map[string]Provider, c *container, ev *trace.Tracer) []ctx.HTTPHandler {
	resolved := make([]ctx.HTTPHandler, len(middlewares))
	for i, gm := range middlewares {
		resolved[i] = handshakeMiddleware(newHTTPMiddleware(gm, injectedProviders, c), ev)
	}

	return resolved
}

func handshakeMiddleware(m httpMiddleware, ev *trace.Tracer) ctx.HTTPHandler {
	return func(c *ctx.HTTPContext) {
		if !ev.Enabled() {
			m.use(c.Request, c.ResponseWriter, c.Next)
			return
		}
		start := time.Now()
		m.use(c.Request, c.ResponseWriter, c.Next)
		m.emit(ev, tracecontext.ScopeOf(c.Context()), c.GetID(), time.Since(start), 0)
	}
}

func (ws *WS) isWSPath(p string) bool {
	return isEnclosedEqual(p, ws.path)
}

func isEnclosedEqual(s, enclosed string) bool {
	j := 0
	if enclosed != "" && enclosed[0] == '/' {
		j = 1
	}
	prev := byte('/')
	hasContent := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isSpace(c) {
			continue
		}
		hasContent = true
		if c == prev && (c == '/' || c == '*') {
			continue
		}
		if j == 0 || j >= len(enclosed) || enclosed[j] != c {
			return false
		}
		j++
		prev = c
	}
	if !hasContent {
		return enclosed == ""
	}
	if prev != '/' {
		if j == 0 || j >= len(enclosed) || enclosed[j] != '/' {
			return false
		}
		j++
	}
	return j > 0 && j == len(enclosed)
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

func (ws *WS) upgrade(w stdHTTP.ResponseWriter, r *stdHTTP.Request, s websocket.Server) {
	s.ServeHTTP(w, r)
}

func (ws *WS) handshake(c *ctx.HTTPContext) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			if _, ok := asException(rec); !ok {
				logPanic(ws.logger, ws.stacks, "UnhandledPanic", c, rec)
			}
			err = errWSHandshakeRejected
		}
	}()

	isNext := true
	c.Next = func() {
		isNext = true
	}

	for _, gm := range ws.globalMiddlewares {
		if !isNext {
			break
		}
		isNext = false
		gm(c)
	}

	if isNext {
		return nil
	}

	return errWSHandshakeRejected
}

func (ws *WS) handleRequest(wsConn *websocket.Conn) {
	defer func() {
		_ = wsConn.Close()
	}()

	connID := connectionID(wsConn)
	conn := ws.connmgr.Register(connID, wsConn)
	defer ws.connmgr.Unregister(connID)

	if err := websocket.JSON.Send(wsConn, WSPayload{
		ID:   connID,
		Type: TypeConnected,
	}); err != nil {
		ws.logger.Error("WSHandshakeSendFailed", "error", err)
		return
	}

	done := make(chan struct{})
	go pingLoop(wsConn, conn, done, ws.logger)

	readLoop(conn, ws)
	close(done)
}

func connectionID(wsConn *websocket.Conn) string {
	if r := wsConn.Request(); r != nil {
		if s := tracecontext.ScopeOf(r.Context()); s != nil && s.Request != "" {
			return s.Request
		}
	}
	return tracecontext.NewSpanID()
}

func pingLoop(wsConn *websocket.Conn, conn *WSConnection, done <-chan struct{}, logger common.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if wsConn == nil {
				return
			}

			if err := wsConn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				logger.Error("WSSetWriteDeadlineFailed", "error", err)
				return
			}

			if err := websocket.JSON.Send(wsConn, WSPayload{Type: TypePing}); err != nil {
				logger.Error("WSPingSendFailed", "error", err)
				return
			}

			if err := wsConn.SetWriteDeadline(time.Time{}); err != nil {
				logger.Error("WSClearWriteDeadlineFailed", "error", err)
				return
			}
		}
	}
}
