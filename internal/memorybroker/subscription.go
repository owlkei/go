package memorybroker

import (
	ptrn "go.owlky.com/owlky/internal/pattern"
)

type Subscription interface {
	ID() string
	Topic() string
	Unsubscribe() error
}

type subscription struct {
	id      string
	topic   string
	pattern ptrn.Pattern
	handler MessageHandler
	broker  *MemoryBroker
}

func (s *subscription) ID() string {
	return s.id
}

func (s *subscription) Topic() string {
	return s.topic
}

func (s *subscription) Unsubscribe() error {
	return s.broker.Unsubscribe(s)
}
