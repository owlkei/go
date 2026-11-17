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
	eventMap        map[string]any
}

func (ws *WS) addToEventMap(fnName, event string, injectableHandler any) {
	if ws.eventMap == nil {
		ws.eventMap = make(map[string]any)
	}
	if ws.funcNameByEvent == nil {
		ws.funcNameByEvent = map[string]string{}
	}
	ws.funcNameByEvent[event] = fnName
	ws.eventMap[event] = injectableHandler
}

func AddHandlerToEventMap(ws *WS, fnName string, handler any) {
	event, ok := ParseWSFuncNameToEvent(fnName)
	if !ok {
		return
	}

	ws.addToEventMap(fnName, event, handler)
}

func WSEventMap(ws *WS) map[string]any {
	return ws.eventMap
}
