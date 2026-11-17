package ginject_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
	"golang.org/x/net/websocket"
)

func serve(app http.Handler, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

type helloController struct {
	ginject.HTTP
}

func (instance helloController) NewController() ginject.Controller { return instance }

func (instance helloController) READ_hello() string { return "hello" }

func TestSurface_HTTPControllerRoutes(t *testing.T) {
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().Controllers(helloController{}).Build())

	w := serve(app, http.MethodGet, "/hello", nil, nil)
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B7: GET /hello status"))
	}
	if w.Body.String() != "hello" {
		t.Error(test.DiffMessage(w.Body.String(), "hello", "B7: GET /hello body"))
	}
	if w := serve(app, http.MethodGet, "/missing", nil, nil); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "B7: an undeclared route is 404"))
	}
}

var chatCalls atomic.Int32

type chatGateway struct {
	ginject.WS
}

func (instance chatGateway) NewController() ginject.Controller { return instance }

func (instance chatGateway) SUBSCRIBE_chat_ANY() string {
	chatCalls.Add(1)
	return "ok"
}

type wireMessage struct {
	Type  string   `json:"type"`
	ID    string   `json:"id"`
	Topic []string `json:"topic,omitempty"`
	Msg   any      `json:"message"`
}

func receiveUntil(t *testing.T, conn *websocket.Conn, id string) wireMessage {
	t.Helper()
	for {
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var m wireMessage
		if err := websocket.JSON.Receive(conn, &m); err != nil {
			t.Fatalf("receive reply %q: %v", id, err)
		}
		if m.ID == id && m.Type != "ping" && m.Type != "event" {
			return m
		}
	}
}

func TestSurface_WSControllerEvents(t *testing.T) {
	chatCalls.Store(0)
	app := ginject.New()
	app.EnableWS(&ginject.WSOptions{Path: "/ws"})
	app.Create(ginject.ModuleBuilder().Controllers(chatGateway{}).Build())

	server := httptest.NewServer(app)
	defer server.Close()

	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	var connected wireMessage
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatal(err)
	}
	if connected.Type != "connected" {
		t.Error(test.DiffMessage(connected.Type, "connected", "B7: the first frame"))
	}

	if err := websocket.JSON.Send(conn, wireMessage{Type: "subscribe", ID: "s1", Topic: []string{"chat.room1"}}); err != nil {
		t.Fatal(err)
	}
	if got := receiveUntil(t, conn, "s1"); got.Type != "ack" {
		t.Error(test.DiffMessage(got.Type, "ack", "B7: subscribe to chat.room1 is acknowledged"))
	}
	if err := websocket.JSON.Send(conn, wireMessage{Type: "publish", ID: "p1", Topic: []string{"chat.room1"}, Msg: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got := receiveUntil(t, conn, "p1"); got.Type != "ack" {
		t.Error(test.DiffMessage(got.Type, "ack", "B7: publish to chat.room1 is handled by SUBSCRIBE_chat_ANY"))
	}
	if chatCalls.Load() == 0 {
		t.Error(test.DiffMessage(chatCalls.Load(), "at least 1", "B7: SUBSCRIBE_chat_ANY ran"))
	}
}

var layerCalls = struct{ guard, interceptor, filter, middleware atomic.Int32 }{}

type layerGuard struct{}

func (instance layerGuard) CanActivate(c ginject.HTTPContext) bool {
	layerCalls.guard.Add(1)
	return c.Query().Get("deny") != "true"
}

type layerInterceptor struct{}

func (instance layerInterceptor) Intercept(c ginject.HTTPContext, a ginject.Aggregation) any {
	layerCalls.interceptor.Add(1)
	return a.Pipe(a.Transform(func(data any) any {
		return ginject.Map{"wrapped": data}
	}))
}

type layerFilter struct{}

func (instance layerFilter) Catch(c ginject.HTTPContext, e *exception.Exception) {
	layerCalls.filter.Add(1)
	c.Status(http.StatusTeapot)
	c.Text("filtered")
}

type layerMiddleware struct{}

func (instance layerMiddleware) Use(r *http.Request, w http.ResponseWriter, next ginject.Next) {
	layerCalls.middleware.Add(1)
	w.Header().Set("X-Layer", "on")
	next()
}

type layersController struct {
	ginject.HTTP
	ginject.Guard
	ginject.Interceptor
	ginject.ExceptionFilter
	ginject.Middleware
}

func (instance layersController) NewController() ginject.Controller {
	instance.BindGuard(layerGuard{}, instance.READ_bound)
	instance.BindInterceptor(layerInterceptor{}, instance.READ_bound)
	instance.BindExceptionFilter(layerFilter{}, instance.READ_bound)
	instance.BindMiddleware(layerMiddleware{}, instance.READ_bound)
	return instance
}

func (instance layersController) READ_bound() string {
	panic(exception.BadRequestException("boom"))
}

func (instance layersController) READ_plain() string { return "plain" }

func TestSurface_BindLayersThroughAliases(t *testing.T) {
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().Controllers(layersController{}).Build())

	plain := serve(app, http.MethodGet, "/plain?deny=true", nil, nil)
	if plain.Code != http.StatusOK || plain.Body.String() != "plain" {
		t.Error(test.DiffMessage(plain.Code, http.StatusOK, "B8: the unbound route is untouched: "+plain.Body.String()))
	}
	if plain.Header().Get("X-Layer") != "" {
		t.Error(test.DiffMessage(plain.Header().Get("X-Layer"), "", "B8: the middleware does not run on the other route"))
	}
	if layerCalls.guard.Load()+layerCalls.interceptor.Load()+layerCalls.filter.Load()+layerCalls.middleware.Load() != 0 {
		t.Error(test.DiffMessage([]int32{layerCalls.guard.Load(), layerCalls.interceptor.Load(), layerCalls.filter.Load(), layerCalls.middleware.Load()}, []int32{0, 0, 0, 0}, "B8: no layer ran for the unbound route"))
	}

	bound := serve(app, http.MethodGet, "/bound", nil, nil)
	if bound.Code != http.StatusTeapot || bound.Body.String() != "filtered" {
		t.Error(test.DiffMessage(bound.Code, http.StatusTeapot, "B8: the exception filter answers the bound route: "+bound.Body.String()))
	}
	if bound.Header().Get("X-Layer") != "on" {
		t.Error(test.DiffMessage(bound.Header().Get("X-Layer"), "on", "B8: the middleware runs on the bound route"))
	}
	if layerCalls.guard.Load() != 1 || layerCalls.middleware.Load() != 1 || layerCalls.filter.Load() != 1 {
		t.Error(test.DiffMessage([]int32{layerCalls.guard.Load(), layerCalls.middleware.Load(), layerCalls.filter.Load()}, []int32{1, 1, 1}, "B8: layers bound to the route ran"))
	}

	denied := serve(app, http.MethodGet, "/bound?deny=true", nil, nil)
	if denied.Code != http.StatusTeapot {
		t.Error(test.DiffMessage(denied.Code, http.StatusTeapot, "B8: the guard refuses the bound route"))
	}
}

type wrapController struct {
	ginject.HTTP
	ginject.Interceptor
}

func (instance wrapController) NewController() ginject.Controller {
	instance.BindInterceptor(layerInterceptor{}, instance.READ_wrapped)
	return instance
}

func (instance wrapController) READ_wrapped() string { return "x" }

func (instance wrapController) READ_bare() string { return "y" }

func TestSurface_BindInterceptorThroughAliasWrapsOneRoute(t *testing.T) {
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().Controllers(wrapController{}).Build())

	if got := serve(app, http.MethodGet, "/wrapped", nil, nil).Body.String(); !strings.Contains(got, "wrapped") {
		t.Error(test.DiffMessage(got, "a body with the wrapped key", "B8: the interceptor transforms the bound route"))
	}
	if got := serve(app, http.MethodGet, "/bare", nil, nil).Body.String(); got != "y" {
		t.Error(test.DiffMessage(got, "y", "B8: the interceptor leaves the other route"))
	}
}
