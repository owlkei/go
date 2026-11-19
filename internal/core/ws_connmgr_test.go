package core

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlky.com/owlky/internal/memorybroker"
	"go.owlky.com/owlky/internal/test"
	"go.owlky.com/owlky/log"
	"golang.org/x/net/websocket"
)

func TestWSConnection_TrySend_DeliversToClient(t *testing.T) {
	serverConn, clientConn, cleanup := newTestWSConnPair(t)
	defer cleanup()

	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	conn := connmgr.Register("conn-1", serverConn)
	defer connmgr.Unregister("conn-1")

	ok := conn.TrySend(WSPayload{Type: TypeEvent, Topic: []string{"chat.to.user2"}, Message: "hi"})
	if !ok {
		t.Error(test.DiffMessage(ok, true, "TrySend should succeed with room in the buffer"))
	}

	var got WSPayload
	if err := websocket.JSON.Receive(clientConn, &got); err != nil {
		t.Fatalf("client receive: %v", err)
	}

	if got.Type != TypeEvent || len(got.Topic) != 1 || got.Topic[0] != "chat.to.user2" {
		t.Error(test.DiffMessage(got, "event/chat.to.user2", "unexpected payload delivered to client"))
	}
}

func TestWSConnection_TrySend_ConcurrentSendsNoRace(t *testing.T) {
	serverConn, clientConn, cleanup := newTestWSConnPair(t)
	defer cleanup()

	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	conn := connmgr.Register("conn-1", serverConn)
	defer connmgr.Unregister("conn-1")

	const goroutines = 16
	const perGoroutine = 8
	total := goroutines * perGoroutine

	received := make(chan struct{}, total)
	stopReceiving := make(chan struct{})
	go func() {
		for {
			var got WSPayload
			if err := websocket.JSON.Receive(clientConn, &got); err != nil {
				return
			}
			select {
			case received <- struct{}{}:
			case <-stopReceiving:
				return
			}
		}
	}()

	var accepted int64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if conn.TrySend(WSPayload{Type: TypeEvent, Message: "x"}) {
					atomic.AddInt64(&accepted, 1)
				}
			}
		}()
	}
	wg.Wait()

	want := int(atomic.LoadInt64(&accepted))
	if want == 0 {
		t.Fatal("TrySend accepted 0 payloads out of 128 concurrent attempts — buffer size regression?")
	}

	deadline := time.After(2 * time.Second)
	count := 0
	for count < want {
		select {
		case <-received:
			count++
		case <-deadline:
			t.Fatalf("timed out waiting for deliveries, got %d/%d accepted", count, want)
		}
	}
	close(stopReceiving)
}

func TestWSConnection_TrySend_DropsWhenBufferFull(t *testing.T) {
	serverConn, _, cleanup := newTestWSConnPair(t)
	defer cleanup()

	connmgr := NewWSConnmgr(log.NewLog(nil), nil)

	conn := connmgr.Register("conn-1", serverConn)
	defer connmgr.Unregister("conn-1")

	done := make(chan bool)
	go func() {
		sawDrop := false
		for i := 0; i < sendBufferSize*4; i++ {
			if !conn.TrySend(WSPayload{Type: TypeEvent, Message: "x"}) {
				sawDrop = true
			}
		}
		done <- sawDrop
	}()

	select {
	case sawDrop := <-done:
		if !sawDrop {
			t.Error(test.DiffMessage(sawDrop, true, "TrySend should drop at least one payload once the buffer is full"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TrySend blocked instead of dropping — it must never block the caller")
	}
}

func TestWSConnmgr_UnregisterStopsWriterWithoutPanic(t *testing.T) {
	serverConn, _, cleanup := newTestWSConnPair(t)
	defer cleanup()

	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	conn := connmgr.Register("conn-1", serverConn)

	connmgr.Unregister("conn-1")

	conn.TrySend(WSPayload{Type: TypeEvent, Message: "after-unregister"})
}

func TestWSConnmgr_Get(t *testing.T) {
	serverConn, _, cleanup := newTestWSConnPair(t)
	defer cleanup()

	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	registered := connmgr.Register("conn-1", serverConn)
	defer connmgr.Unregister("conn-1")

	got, ok := connmgr.Get("conn-1")
	if !ok || got != registered {
		t.Error(test.DiffMessage(got, registered, "Get should return the connection registered under that id"))
	}

	if _, ok := connmgr.Get("missing"); ok {
		t.Error(test.DiffMessage(ok, false, "Get should report false for an unregistered id"))
	}
}

func TestWSConnmgr_Touch(t *testing.T) {
	serverConn, _, cleanup := newTestWSConnPair(t)
	defer cleanup()

	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	conn := connmgr.Register("conn-1", serverConn)
	defer connmgr.Unregister("conn-1")

	before := conn.LastSeen
	time.Sleep(time.Millisecond)
	connmgr.touch("conn-1")

	got, _ := connmgr.Get("conn-1")
	if !got.LastSeen.After(before) {
		t.Error(test.DiffMessage(got.LastSeen, "after "+before.String(), "touch should advance LastSeen"))
	}
}

func TestWSConnmgr_TouchUnknownConnIsNoop(t *testing.T) {
	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	connmgr.touch("missing")
}

func TestWSConnmgr_UnsubscribeRemovesOnlyMatchingTopic(t *testing.T) {
	connmgr := NewWSConnmgr(log.NewLog(nil), nil)

	if err := connmgr.Subscribe("conn-1", "topic.a", func(*memorybroker.Message) {}); err != nil {
		t.Fatal(err)
	}
	if err := connmgr.Subscribe("conn-1", "topic.b", func(*memorybroker.Message) {}); err != nil {
		t.Fatal(err)
	}

	if err := connmgr.Unsubscribe("conn-1", "topic.a"); err != nil {
		t.Fatal(err)
	}

	if connmgr.isSubscribed("conn-1", "topic.a") {
		t.Error(test.DiffMessage(true, false, "Unsubscribe should remove the given topic"))
	}
	if !connmgr.isSubscribed("conn-1", "topic.b") {
		t.Error(test.DiffMessage(false, true, "Unsubscribe should leave other topics untouched"))
	}
}

func TestWSConnmgr_UnsubscribeUnknownTopicIsNoop(t *testing.T) {
	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	if err := connmgr.Unsubscribe("conn-1", "never-subscribed"); err != nil {
		t.Error(test.DiffMessage(err, nil, "Unsubscribe on a topic never subscribed to should not error"))
	}
}

func TestWSConnmgr_ConcurrentSubscribeUnsubscribePublish_NoRace(t *testing.T) {
	connmgr := NewWSConnmgr(log.NewLog(nil), nil)
	topics := []string{"chat.1", "chat.2", "chat.3", "chat.*"}
	noop := func(*memorybroker.Message) {}

	const goroutines = 32
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			connID := "conn-" + topics[g%len(topics)] + "-" + string(rune('a'+g))
			for i := 0; i < iterations; i++ {
				topic := topics[i%len(topics)]
				if err := connmgr.Subscribe(connID, topic, noop); err != nil {
					t.Errorf("Subscribe: %v", err)
				}
				if err := (*connmgr.Broker).Publish(topic, "x"); err != nil {
					t.Errorf("Publish: %v", err)
				}
				if err := connmgr.Unsubscribe(connID, topic); err != nil {
					t.Errorf("Unsubscribe: %v", err)
				}
			}
		}(g)
	}
	wg.Wait()
}
