package wsevent_test

import (
	"fmt"
	"testing"

	"go.owlkei.com/owlkei/internal/wsevent"
)

func BenchmarkWSEvent_Add(b *testing.B) {
	m := wsevent.NewWSEvent()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		m.Add(fmt.Sprintf("chat.room%d.message", i%2000), wsevent.WSEventItem{Handler: i})
	}
}

func BenchmarkWSEvent_MatchExact(b *testing.B) {
	m := newBenchWSEvent(1000, 1000)
	b.ReportAllocs()
	for b.Loop() {
		m.Match("chat.room500.message")
	}
}

func BenchmarkWSEvent_MatchWildcard(b *testing.B) {
	m := newBenchWSEvent(1000, 1000)
	b.ReportAllocs()
	for b.Loop() {
		m.Match("chat.room1500.anything")
	}
}

func BenchmarkWSEvent_MatchNoMatch(b *testing.B) {
	m := newBenchWSEvent(1000, 1000)
	b.ReportAllocs()
	for b.Loop() {
		m.Match("unregistered.topic.here")
	}
}
