package trace

import (
	"sync"
	"sync/atomic"
)

type Tracer struct {
	mu        sync.Mutex
	nextID    uint64
	listeners atomic.Pointer[[]listener]
}

type listener struct {
	id uint64
	fn func(Event)
}

func (t *Tracer) Enabled() bool {
	return t.listeners.Load() != nil
}

func (t *Tracer) Emit(e Event) {
	ls := t.listeners.Load()
	if ls == nil {
		return
	}
	for _, l := range *ls {
		l.fn(e)
	}
}

func (t *Tracer) Listen(fn func(Event)) (stop func()) {
	if fn == nil {
		return func() {}
	}
	t.mu.Lock()
	t.nextID++
	id := t.nextID
	var next []listener
	if current := t.listeners.Load(); current != nil {
		next = make([]listener, len(*current), len(*current)+1)
		copy(next, *current)
	}
	next = append(next, listener{id: id, fn: fn})
	t.listeners.Store(&next)
	t.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() { t.remove(id) })
	}
}

func (t *Tracer) remove(id uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.listeners.Load()
	if current == nil {
		return
	}
	next := make([]listener, 0, len(*current))
	for _, l := range *current {
		if l.id != id {
			next = append(next, l)
		}
	}
	if len(next) == 0 {
		t.listeners.Store(nil)
		return
	}
	t.listeners.Store(&next)
}
