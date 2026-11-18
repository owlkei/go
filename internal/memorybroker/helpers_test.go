package memorybroker

import (
	"testing"
)

func newBroker(t *testing.T) Broker {
	t.Helper()
	b := NewMemoryBroker()
	t.Cleanup(func() { _ = b.Close() })
	return b
}

type fakeSubscription struct{}

func (fakeSubscription) ID() string { return "fake" }

func (fakeSubscription) Topic() string { return "fake" }

func (fakeSubscription) Unsubscribe() error { return nil }
