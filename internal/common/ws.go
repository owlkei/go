package common

var WSOperations = map[string]string{
	"SUBSCRIBE": "SUBSCRIBE",
}

type WSLayer struct {
	Handler   any
	EventName string
	Name      string
}

type WS struct {
	funcNameByEvent map[string]string
	EventMap        map[string]any
}

func (ws *WS) addToEventMap(fnName, event string, injectableHandler any) {
	if ws.EventMap == nil {
		ws.EventMap = make(map[string]any)
	}
	if ws.funcNameByEvent == nil {
		ws.funcNameByEvent = map[string]string{}
	}
	ws.funcNameByEvent[event] = fnName
	ws.EventMap[event] = injectableHandler
}

func (ws *WS) AddHandlerToEventMap(fnName string, handler any) {
	event, ok := ParseWSFuncNameToEvent(fnName)
	if !ok {
		return
	}

	ws.addToEventMap(fnName, event, handler)
}
