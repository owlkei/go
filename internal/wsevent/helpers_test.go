package wsevent_test

import (
	"fmt"

	"github.com/dangduoc08/ginject/internal/wsevent"
)

func newBenchWSEvent(exactPatterns, wildcardPatterns int) *wsevent.WSEvent {
	m := wsevent.NewWSEvent()
	for i := 0; i < exactPatterns; i++ {
		m.Add(fmt.Sprintf("chat.room%d.message", i), wsevent.WSEventItem{Handler: i})
	}
	for i := 0; i < wildcardPatterns; i++ {
		m.Add(fmt.Sprintf("chat.room%d.*", i), wsevent.WSEventItem{Handler: i})
	}
	return m
}
