package core

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"golang.org/x/net/websocket"
)

type wsAllController struct{ common.WS }

func (c wsAllController) NewController() Controller  { return c }
func (c wsAllController) SUBSCRIBE_chat_ALL() string { return "all" }

func TestWS_AllHandlesDeepTopic(t *testing.T) {
	app := New()
	app.EnableWS(&WSOptions{Path: "/ws"})
	app.Create(NewModuleBuilder().Controllers(wsAllController{}).Build())

	server := httptest.NewServer(app)
	defer server.Close()

	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	var connected WSPayload
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatalf("receive connected: %v", err)
	}

	for _, topic := range []string{"chat.room1.typing", "chat.room1"} {
		if err := websocket.JSON.Send(conn, WSPayload{Type: TypeSubscribe, ID: "s-" + topic, Topic: []string{topic}}); err != nil {
			t.Fatalf("send: %v", err)
		}
		var reply WSPayload
		if err := websocket.JSON.Receive(conn, &reply); err != nil {
			t.Fatalf("receive: %v", err)
		}
		if reply.Type != TypeAck {
			t.Error(test.DiffMessage(reply.Type, TypeAck, "B18: "+topic+" reaches SUBSCRIBE_chat_ALL"))
		}
	}

	if err := websocket.JSON.Send(conn, WSPayload{Type: TypeSubscribe, ID: "s-chat", Topic: []string{"chat"}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	var reply WSPayload
	if err := websocket.JSON.Receive(conn, &reply); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if reply.Type != TypeError {
		t.Error(test.DiffMessage(reply.Type, TypeError, "B18: chat alone does not reach SUBSCRIBE_chat_ALL (** is one or more segments)"))
	}
}

type wsAllNotLastController struct{ common.WS }

func (c wsAllNotLastController) NewController() Controller    { return c }
func (c wsAllNotLastController) SUBSCRIBE_chat_ALL_x() string { return "x" }

func TestCreate_AllNotLastPanics(t *testing.T) {
	app := New()
	app.EnableWS(&WSOptions{Path: "/ws"})
	want := "invalid route: ALL in SUBSCRIBE_chat_ALL_x must be the last word"
	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().Controllers(wsAllNotLastController{}).Build())
	})
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "B19: Create stops on ALL before the last word"))
	}
}

type httpAnyAfterParamController struct{ common.HTTP }

func (c httpAnyAfterParamController) NewController() Controller { return c }
func (c httpAnyAfterParamController) READ_files_BY_name_ANY(p ctx.Param) string {
	return "name=" + p.Get("name")
}

func TestHTTP_AnyAfterParamServes(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().Controllers(httpAnyAfterParamController{}).Build())

	w := createGet(app, "/files/a/b/c")
	if w.Code != 200 || w.Body.String() != "name=a" {
		t.Error(test.DiffMessage(w.Body.String(), "name=a", "B20: GET /files/a/b/c reaches READ_files_BY_name_ANY with name = a"))
	}
}
