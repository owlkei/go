package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/str"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/log"
	"github.com/dangduoc08/ginject/trace"
	"golang.org/x/net/websocket"
)

func TestNewWS_DefaultPath(t *testing.T) {
	ws := NewWS(&WSConfig{logger: log.NewLog(nil)})
	if !ws.isWSPath("/ws") {
		t.Error(test.DiffMessage(ws.isWSPath("/ws"), true, "NewWS with no configured Path should default to /ws"))
	}
}

func TestNewWS_CustomPath(t *testing.T) {
	ws := NewWS(&WSConfig{Path: "/live", logger: log.NewLog(nil)})
	if !ws.isWSPath("/live") {
		t.Error(test.DiffMessage(ws.isWSPath("/live"), true, "NewWS should honor a configured custom Path"))
	}
	if ws.isWSPath("/ws") {
		t.Error(test.DiffMessage(ws.isWSPath("/ws"), false, "a custom Path should not also match the default /ws"))
	}
}

func TestIsWSPath_NormalizesSlashes(t *testing.T) {
	ws := NewWS(&WSConfig{Path: "live", logger: log.NewLog(nil)})
	if !ws.isWSPath("/live") {
		t.Error(test.DiffMessage(ws.isWSPath("/live"), true, "isWSPath should normalize a Path configured without leading/trailing slashes"))
	}
}

func TestHandshake_Allowed(t *testing.T) {
	ws := NewWS(&WSConfig{logger: log.NewLog(nil)})

	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()

	if err := ws.handshake(c); err != nil {
		t.Error(test.DiffMessage(err, nil, "handshake with no middlewares should succeed"))
	}
}

func TestHandshake_RejectedWhenMiddlewareDoesNotCallNext(t *testing.T) {
	ws := NewWS(&WSConfig{logger: log.NewLog(nil)})
	ws.globalMiddlewares = []ctx.HTTPHandler{
		func(c *ctx.HTTPContext) {},
	}

	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()

	err := ws.handshake(c)
	if err != errWSHandshakeRejected {
		t.Error(test.DiffMessage(err, errWSHandshakeRejected, "handshake should be rejected when a middleware does not call next()"))
	}
}

type wsHandshakeTraceMiddleware struct{}

func (wsHandshakeTraceMiddleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	next()
}

func TestHandshake_EmitsMiddlewareTraceWithHTTPTransport(t *testing.T) {
	ev := &trace.Tracer{}
	var got []trace.Event
	ev.Listen(func(e trace.Event) {
		got = append(got, e)
	})

	ws := NewWS(&WSConfig{
		logger:            log.NewLog(nil),
		tracer:            ev,
		injectedProviders: map[string]Provider{},
		container:         newContainer(),
		globalMiddlewares: []common.MiddlewareFn{wsHandshakeTraceMiddleware{}},
	})

	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()

	if err := ws.handshake(c); err != nil {
		t.Fatal(test.DiffMessage(err, nil, "handshake should succeed when the middleware calls next()"))
	}

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 trace event, got %d: %+v", len(got), got)
	}
	if got[0].Stage != trace.StageMiddleware {
		t.Error(test.DiffMessage(got[0].Stage, trace.StageMiddleware, "handshake middleware trace event stage"))
	}
	if got[0].Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage(got[0].Transport, trace.TransportHTTP, "handshake middleware trace event must report HTTP transport, not WS, since the connection has not upgraded yet"))
	}
	if got[0].Name != "core.wsHandshakeTraceMiddleware" {
		t.Error(test.DiffMessage(got[0].Name, "core.wsHandshakeTraceMiddleware", "handshake middleware trace event name"))
	}
}

type wsHandshakeTraceMiddlewareB struct{}

func (wsHandshakeTraceMiddlewareB) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	next()
}

func TestHandshake_EmitsOneTraceEventPerMiddleware(t *testing.T) {
	ev := &trace.Tracer{}
	var got []trace.Event
	ev.Listen(func(e trace.Event) {
		got = append(got, e)
	})

	ws := NewWS(&WSConfig{
		logger:            log.NewLog(nil),
		tracer:            ev,
		injectedProviders: map[string]Provider{},
		container:         newContainer(),
		globalMiddlewares: []common.MiddlewareFn{wsHandshakeTraceMiddleware{}, wsHandshakeTraceMiddlewareB{}},
	})

	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()

	if err := ws.handshake(c); err != nil {
		t.Fatal(test.DiffMessage(err, nil, "handshake should succeed when every middleware calls next()"))
	}

	if len(got) != 2 {
		t.Fatalf("expected exactly 2 trace events, got %d: %+v", len(got), got)
	}
	wantNames := []string{"core.wsHandshakeTraceMiddleware", "core.wsHandshakeTraceMiddlewareB"}
	for i, want := range wantNames {
		if got[i].Name != want {
			t.Error(test.DiffMessage(got[i].Name, want, "handshake middleware trace events must fire in registration order"))
		}
		if got[i].Transport != trace.TransportHTTP {
			t.Error(test.DiffMessage(got[i].Transport, trace.TransportHTTP, "every handshake middleware trace event must report HTTP transport"))
		}
	}
}

func TestHandshake_NoListener_StillSucceeds(t *testing.T) {
	ev := &trace.Tracer{}

	ws := NewWS(&WSConfig{
		logger:            log.NewLog(nil),
		tracer:            ev,
		injectedProviders: map[string]Provider{},
		container:         newContainer(),
		globalMiddlewares: []common.MiddlewareFn{wsHandshakeTraceMiddleware{}},
	})

	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()

	if err := ws.handshake(c); err != nil {
		t.Fatal(test.DiffMessage(err, nil, "handshake should succeed when the middleware calls next()"))
	}
}

func TestWSHandleRequest_SendsConnectedPayloadAndRegisters(t *testing.T) {
	ws := newTestWSBare(t)
	serverConn, clientConn, cleanup := newTestWSConnPair(t)
	defer cleanup()

	done := make(chan struct{})
	go func() {
		ws.handleRequest(serverConn)
		close(done)
	}()

	var got WSPayload
	if err := websocket.JSON.Receive(clientConn, &got); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if got.Type != TypeConnected {
		t.Error(test.DiffMessage(got.Type, TypeConnected, "handleRequest should send a connected payload on handshake"))
	}
	if len(got.ID) != 16 || !isLowerHex(got.ID) {
		t.Error(test.DiffMessage(got.ID, "<16 lowercase hexadecimal characters>", "W2: a connection without a scoped request gets a span ID, not a UUID"))
	}

	if _, ok := ws.connmgr.Get(got.ID); !ok {
		t.Error(test.DiffMessage(ok, true, "the connection should be registered under the id sent in the connected payload"))
	}

	_ = clientConn.Close()
	<-done

	if _, ok := ws.connmgr.Get(got.ID); ok {
		t.Error(test.DiffMessage(ok, false, "the connection should be unregistered once handleRequest returns"))
	}
}

func TestIsWSPath_EqualsEnclosedComparison(t *testing.T) {
	for _, configured := range []string{"", "/ws", "live", "/live/", "a/b"} {
		ws := NewWS(&WSConfig{Path: configured, logger: log.NewLog(nil)})
		for _, candidate := range []string{"", "/", "/ws", "/ws/", "ws", "ws/", "/wsx", "/w", "//ws", "/ws//", "/live", "/live/", "live", "/a/b", "/a/b/", "/a/bc", "/WS", "/ws?x", "/ws/x"} {
			want := str.Enclose(candidate, '/') == ws.path
			if got := ws.isWSPath(candidate); got != want {
				t.Error(test.DiffMessage(got, want, "P10: isWSPath equals the enclosed comparison for path "+configured+" and request "+candidate))
			}
		}
	}
}

func TestIsWSPath_DoesNotAllocate(t *testing.T) {
	ws := NewWS(&WSConfig{Path: "/ws", logger: log.NewLog(nil)})

	for _, candidate := range []string{"/ws", "/ws/", "/other", "/", ""} {
		if got := testing.AllocsPerRun(100, func() { _ = ws.isWSPath(candidate) }); got != 0 {
			t.Error(test.DiffMessage(got, 0.0, "bug 13, P10: isWSPath allocates nothing for "+candidate))
		}
	}
}
