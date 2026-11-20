package core

import (
	"testing"

	"go.owlkei.com/owlkei/internal/memorybroker"
	"go.owlkei.com/owlkei/log"
)

func BenchmarkWSConnmgr_Get(b *testing.B) {
	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	connmgr.conns["conn-1"] = &WSConnection{ID: "conn-1"}

	b.ReportAllocs()
	for b.Loop() {
		connmgr.Get("conn-1")
	}
}

func BenchmarkWSConnmgr_Touch(b *testing.B) {
	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	connmgr.conns["conn-1"] = &WSConnection{ID: "conn-1"}

	b.ReportAllocs()
	for b.Loop() {
		connmgr.touch("conn-1")
	}
}

func BenchmarkWSConnmgr_IsSubscribed(b *testing.B) {
	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	noop := func(*memorybroker.Message) {}
	for _, topic := range []string{"a", "b", "c"} {
		if err := connmgr.Subscribe("conn-1", topic, noop); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		connmgr.isSubscribed("conn-1", "c")
	}
}
