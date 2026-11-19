package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.owlky.com/owlky/internal/test"
	"golang.org/x/net/websocket"
)

func TestPublisher_SharesBrokerWithWSConnmgr(t *testing.T) {
	app := New()
	app.EnableWS(&WSOptions{Path: "/ws"})
	app.Create(NewModuleBuilder().Build())

	if *app.ws.connmgr.Broker != app.broker {
		t.Error(test.DiffMessage(*app.ws.connmgr.Broker, app.broker, "WSConnmgr must use the same broker instance as the app's Publisher, or fan-out across transports won't reach WS subscribers"))
	}
}

func TestPublisher_HTTPHandlerFansOutToWSSubscriber(t *testing.T) {
	app := New()
	app.EnableWS(&WSOptions{Path: "/ws"})
	app.Create(NewModuleBuilder().Controllers(publisherTestController{}).Build())

	server := httptest.NewServer(app)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, err := websocket.Dial(wsURL, "", server.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	var connected WSPayload
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatalf("receive connected: %v", err)
	}

	if err := websocket.JSON.Send(conn, WSPayload{Type: TypeSubscribe, ID: "1", Topic: []string{"chat.123"}}); err != nil {
		t.Fatalf("send subscribe: %v", err)
	}
	var ack WSPayload
	if err := websocket.JSON.Receive(conn, &ack); err != nil {
		t.Fatalf("receive ack: %v", err)
	}
	if ack.Type != TypeAck {
		t.Fatalf("expected ack, got %v", ack.Type)
	}

	r := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var evt WSPayload
	if err := websocket.JSON.Receive(conn, &evt); err != nil {
		t.Fatalf("receive event: %v", err)
	}
	if evt.Type != TypeEvent || len(evt.Topic) != 1 || evt.Topic[0] != "chat.123" {
		t.Error(test.DiffMessage(evt, "event chat.123", "the HTTP handler's Publish call should fan out to the WS subscriber"))
	}
}

func TestPublisher_WSHandlerFansOutToOtherWSSubscriber(t *testing.T) {
	app := New()
	app.EnableWS(&WSOptions{Path: "/ws"})
	app.Create(NewModuleBuilder().Controllers(publisherWSTestController{}).Build())

	server := httptest.NewServer(app)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	dial := func() *websocket.Conn {
		t.Helper()
		conn, err := websocket.Dial(wsURL, "", server.URL)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		var connected WSPayload
		if err := websocket.JSON.Receive(conn, &connected); err != nil {
			t.Fatalf("receive connected: %v", err)
		}
		return conn
	}
	subscribe := func(conn *websocket.Conn, topic string) {
		t.Helper()
		if err := websocket.JSON.Send(conn, WSPayload{Type: TypeSubscribe, ID: "sub-" + topic, Topic: []string{topic}}); err != nil {
			t.Fatalf("send subscribe: %v", err)
		}
		var ack WSPayload
		if err := websocket.JSON.Receive(conn, &ack); err != nil {
			t.Fatalf("receive ack: %v", err)
		}
		if ack.Type != TypeAck {
			t.Fatalf("expected ack for %s, got %v", topic, ack.Type)
		}
	}

	relayConn := dial()
	defer func() { _ = relayConn.Close() }()
	subscribe(relayConn, "relay.in")

	outConn := dial()
	defer func() { _ = outConn.Close() }()
	subscribe(outConn, "relay.out")

	if err := websocket.JSON.Send(relayConn, WSPayload{Type: TypePublish, ID: "pub-1", Topic: []string{"relay.in"}, Message: "hi"}); err != nil {
		t.Fatalf("send publish: %v", err)
	}

	var outEvt WSPayload
	if err := websocket.JSON.Receive(outConn, &outEvt); err != nil {
		t.Fatalf("receive event on relay.out: %v", err)
	}
	if outEvt.Type != TypeEvent || len(outEvt.Topic) != 1 || outEvt.Topic[0] != "relay.out" {
		t.Error(test.DiffMessage(outEvt, "event relay.out", "the WS handler's injected Publisher should fan out to a subscriber of a different topic"))
	}
}
