package common

import (
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

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

func TestHooks_EventMapAccessors(t *testing.T) {
	ws := &WS{}
	AddHandlerToEventMap(ws, "SUBSCRIBE_chat_ANY", nil)
	if _, ok := WSEventMap(ws)["chat.*"]; !ok {
		t.Error(test.DiffMessage(WSEventMap(ws), "<map with chat.*>", "B18: WSEventMap reads what AddHandlerToEventMap wrote"))
	}
}
