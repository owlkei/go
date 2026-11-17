package common

import "testing"

func BenchmarkAddHandlerToEventMap(b *testing.B) {
	fns := []string{
		"SUBSCRIBE_chat_message", "SUBSCRIBE_chat_status", "SUBSCRIBE_chat_ANY",
		"SUBSCRIBE_room_join", "SUBSCRIBE_room_leave", "SUBSCRIBE_room_ALL",
		"SUBSCRIBE_user_typing", "SUBSCRIBE_user_presence", "SUBSCRIBE_notification",
		"SUBSCRIBE_ALL",
	}
	b.ReportAllocs()
	for b.Loop() {
		ws := &WS{}
		for _, fn := range fns {
			AddHandlerToEventMap(ws, fn, nil)
		}
	}
}

func BenchmarkWSFnToSubject(b *testing.B) {
	fns := []string{
		"SUBSCRIBE_chat_message", "SUBSCRIBE_chat_ANY", "SUBSCRIBE_chat_ALL", "SUBSCRIBE_ALL",
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, fn := range fns {
			ParseWSFuncNameToEvent(fn)
		}
	}
}
