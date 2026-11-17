package ctx

import (
	"testing"

	"golang.org/x/net/websocket"
)

func BenchmarkWSContext_Init_NoConnection(b *testing.B) {
	c := NewWSContext()
	b.ReportAllocs()
	for b.Loop() {
		WSInit(c, nil)
		WSReset(c)
	}
}

func BenchmarkWSContext_Init_ConnectionWithoutRequest(b *testing.B) {
	c := NewWSContext()
	conn := &websocket.Conn{}
	b.ReportAllocs()
	for b.Loop() {
		WSInit(c, conn)
		WSReset(c)
	}
}
