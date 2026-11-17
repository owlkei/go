package common

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestParseWSFnNameToEvent_Exact(t *testing.T) {
	cases := []struct {
		fn, want string
	}{
		{"SUBSCRIBE_message", "message"},
		{"SUBSCRIBE_chat_message", "chat.message"},
		{"SUBSCRIBE_room_events", "room.events"},
		{"SUBSCRIBE_a_b_c", "a.b.c"},
	}
	for _, c := range cases {
		got, ok := ParseWSFuncNameToEvent(c.fn)
		if !ok {
			t.Error(test.DiffMessage(ok, true, c.fn+" should be valid WS fn"))
		}
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, c.fn+" pattern"))
		}
	}
}

func TestParseWSFnNameToEvent_Wildcards(t *testing.T) {
	cases := []struct {
		fn, want string
	}{
		{"SUBSCRIBE_chat_ANY", "chat.*"},
		{"SUBSCRIBE_chat_ANY_message", "chat.*.message"},
		{"SUBSCRIBE_ANY", "*"},
	}
	for _, c := range cases {
		got, ok := ParseWSFuncNameToEvent(c.fn)
		if !ok {
			t.Error(test.DiffMessage(ok, true, c.fn+" should be valid WS fn"))
		}
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, c.fn+" pattern"))
		}
	}
}

func TestParseWSFnNameToEvent_Invalid(t *testing.T) {
	cases := []string{
		"READ_users",
		"CREATE_orders",
		"",
		"SUBSCRIBE",
		"SUBSCRIBE_",
		"lowercase_message",
	}
	for _, fn := range cases {
		_, ok := ParseWSFuncNameToEvent(fn)
		if ok {
			t.Error(test.DiffMessage(ok, false, fn+" should not be valid WS fn"))
		}
	}
}

func TestParseWSFnNameToEvent_CaseNormalization(t *testing.T) {
	got, ok := ParseWSFuncNameToEvent("SUBSCRIBE_Chat_Message")
	if !ok {
		t.Error(test.DiffMessage(ok, true, "SUBSCRIBE_Chat_Message should parse"))
	}
	if got != "chat.message" {
		t.Error(test.DiffMessage(got, "chat.message", "tokens lowercased"))
	}
}

func TestAddToEventMap_InitMaps(t *testing.T) {
	ws := &WS{}
	ws.addToEventMap("ON_message", "message", nil)
	if WSEventMap(ws) == nil {
		t.Error(test.DiffMessage(WSEventMap(ws), "non-nil", "EventMap initialized"))
	}
	if ws.funcNameByEvent == nil {
		t.Error(test.DiffMessage(ws.funcNameByEvent, "non-nil", "funcNameByEvent initialized"))
	}
}

func TestAddToEventMap_StoresEntries(t *testing.T) {
	ws := &WS{}
	ws.addToEventMap("ON_message", "message", "handler")
	if WSEventMap(ws)["message"] != "handler" {
		t.Error(test.DiffMessage(WSEventMap(ws)["message"], "handler", "EventMap entry"))
	}
	if ws.funcNameByEvent["message"] != "ON_message" {
		t.Error(test.DiffMessage(ws.funcNameByEvent["message"], "ON_message", "funcNameByEvent entry"))
	}
}

func TestAddHandlerToEventMap_Stores(t *testing.T) {
	ws := &WS{}
	AddHandlerToEventMap(ws, "SUBSCRIBE_message", nil)
	if WSEventMap(ws) == nil {
		t.Error(test.DiffMessage(WSEventMap(ws), "non-nil", "EventMap created"))
		return
	}
	eventName := "message"
	if _, ok := WSEventMap(ws)[eventName]; !ok {
		t.Error(test.DiffMessage("missing", eventName, "event stored in EventMap"))
	}
	if ws.funcNameByEvent[eventName] != "SUBSCRIBE_message" {
		t.Error(test.DiffMessage(ws.funcNameByEvent[eventName], "SUBSCRIBE_message", "function name stored by event"))
	}
}

func TestAddHandlerToEventMap_WildcardStores(t *testing.T) {
	ws := &WS{}
	AddHandlerToEventMap(ws, "SUBSCRIBE_chat_ANY", nil)
	if _, ok := WSEventMap(ws)["chat.*"]; !ok {
		t.Error(test.DiffMessage("missing", "chat.*", "wildcard event stored"))
	}
	if ws.funcNameByEvent["chat.*"] != "SUBSCRIBE_chat_ANY" {
		t.Error(test.DiffMessage(ws.funcNameByEvent["chat.*"], "SUBSCRIBE_chat_ANY", "function name stored by wildcard event"))
	}
}

func TestAddHandlerToEventMap_IgnoresNonWS(t *testing.T) {
	ws := &WS{}
	AddHandlerToEventMap(ws, "READ_users", nil)
	if len(WSEventMap(ws)) != 0 {
		t.Error(test.DiffMessage(len(WSEventMap(ws)), 0, "non-WS fn should not be stored"))
	}
}

func TestAddHandlerToEventMap_SameEventDoesNotPanic(t *testing.T) {
	ws := &WS{}
	AddHandlerToEventMap(ws, "SUBSCRIBE_message", nil)
	defer func() {
		if r := recover(); r != nil {
			t.Error(test.DiffMessage(r, "no panic", "conflicts are checked per app by core, AddHandlerToEventMap no longer panics"))
		}
	}()
	AddHandlerToEventMap(ws, "SUBSCRIBE_message", nil)
}
