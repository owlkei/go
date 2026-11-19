package core

import (
	"runtime"
	"sync"
	"time"

	"go.owlky.com/owlky/log"
)

const (
	panicStackLimit     = 4 << 10
	stackFrames         = 16
	stackSlots          = 32
	stacksPerSecond     = 10
	stacksSuppressedMsg = "PanicStacksSuppressed"
)

var (
	stackNow   = time.Now
	stackAfter = func(d time.Duration, f func()) (stop func() bool) {
		return time.AfterFunc(d, f).Stop
	}
)

type stackSampler struct {
	mu         sync.Mutex
	logger     log.Logger
	second     int64
	logged     int
	suppressed int
	slots      [stackSlots]stackSlot
	stop       func() bool
	closed     bool
}

type stackSlot struct {
	key    uint64
	second int64
}

func newStackSampler(logger log.Logger) *stackSampler {
	return &stackSampler{logger: logger}
}

func (s *stackSampler) stack() (string, bool) {
	if s == nil {
		return panicStack(), true
	}
	var pcs [stackFrames]uintptr
	key := stackKey(pcs[:runtime.Callers(3, pcs[:])])
	now := stackNow()
	second := now.Unix()
	s.mu.Lock()
	if second != s.second {
		s.second, s.logged = second, 0
	}
	slot := &s.slots[key%stackSlots]
	if (slot.key != key || slot.second != second) && s.logged < stacksPerSecond {
		slot.key, slot.second = key, second
		s.logged++
		s.mu.Unlock()
		return panicStack(), true
	}
	s.suppressed++
	if s.stop == nil && !s.closed {
		s.stop = stackAfter(time.Unix(second+1, 0).Sub(now), s.report)
	}
	s.mu.Unlock()
	return "", false
}

func stackKey(pcs []uintptr) uint64 {
	h := uint64(14695981039346656037)
	for _, pc := range pcs {
		h ^= uint64(pc)
		h *= 1099511628211
	}
	return h
}

func panicStack() string {
	buf := make([]byte, panicStackLimit)
	return string(buf[:runtime.Stack(buf, false)])
}

func (s *stackSampler) report() {
	s.mu.Lock()
	n := s.suppressed
	s.suppressed, s.stop = 0, nil
	s.mu.Unlock()
	if n > 0 {
		s.logger.Warn(stacksSuppressedMsg, "stacks_suppressed", n)
	}
}

func (s *stackSampler) shutdown() {
	if s == nil {
		return
	}
	s.mu.Lock()
	n, stop := s.suppressed, s.stop
	s.suppressed, s.stop, s.closed = 0, nil, true
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	if n > 0 {
		s.logger.Warn(stacksSuppressedMsg, "stacks_suppressed", n)
	}
}
