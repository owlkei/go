package memorybroker

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestSubscription_IDAndTopic(t *testing.T) {
	b := newBroker(t)

	sub, err := b.Subscribe("my.topic", func(_ *Message) {})
	if err != nil {
		t.Fatal(err)
	}

	if sub.ID() == "" {
		t.Error(test.DiffMessage("", "non-empty UUID", "Subscription.ID() should be non-empty"))
	}
	if sub.Topic() != "my.topic" {
		t.Error(test.DiffMessage(sub.Topic(), "my.topic", "Subscription.Topic() mismatch"))
	}
}
