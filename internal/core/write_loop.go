package core

import (
	"go.owlky.com/owlky/log"
	"golang.org/x/net/websocket"
)

func writeLoop(wsConn *websocket.Conn, send <-chan WSPayload, done <-chan struct{}, logger log.Logger) {
	for {
		select {
		case payload := <-send:
			if err := websocket.JSON.Send(wsConn, payload); err != nil {
				logger.Error("WSWriteFailed", "error", err)
				return
			}
		case <-done:
			return
		}
	}
}
