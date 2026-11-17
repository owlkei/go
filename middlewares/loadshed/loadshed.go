package loadshed

import (
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/httphook"
	"github.com/dangduoc08/ginject/internal/inspect"
)

const (
	maxInFlightLimit = 1_000_000
	maxQueueLimit    = 65_536
	defaultQueue     = 64
	waitTimeout      = 100 * time.Millisecond
	retryAfter       = time.Second
	retryAfterKey    = "Retry-After"
	retryAfterValue  = "1"
)

var busy = exception.ServiceUnavailableException("Server is busy")

type LoadShed struct {
	MaxInFlight int
	MaxQueue    int
}

type compiledLoadShed struct {
	admit      atomic.Int64
	inFlight   atomic.Int64
	waiting    atomic.Int32
	isAdaptive bool
	queue      int
	mu         sync.Mutex
	head       *waiter
	tail       *waiter
	listed     int
	probeDone  probeHook
	adaptive   adaptive

	refusedFull      atomic.Int64
	refusedTimeout   atomic.Int64
	refusedCancelled atomic.Int64
}

type waiter struct {
	c         *compiledLoadShed
	ch        chan struct{}
	prev      *waiter
	next      *waiter
	isListed  bool
	isProbe   bool
	waited    time.Duration
	startedAt time.Time
}

type probeHook struct{ c *compiledLoadShed }

type timedBody struct {
	inner    io.ReadCloser
	c        *compiledLoadShed
	readTime atomic.Int64
	waited   time.Duration
	isProbe  bool
}

func (instance LoadShed) NewMiddleware() common.MiddlewareFn {
	return loadLoadShedOptions(&instance)
}

func (instance LoadShed) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func loadLoadShedOptions(l *LoadShed) *compiledLoadShed {
	if l.MaxInFlight < 0 || l.MaxInFlight > maxInFlightLimit {
		panic(color.FmtRed("invalid loadshed: MaxInFlight is %d, it must be between 0 and %d", l.MaxInFlight, maxInFlightLimit))
	}
	if l.MaxQueue > maxQueueLimit {
		panic(color.FmtRed("invalid loadshed: MaxQueue is %d, at most %d are allowed", l.MaxQueue, maxQueueLimit))
	}
	c := &compiledLoadShed{queue: l.MaxQueue}
	c.probeDone.c = c
	switch {
	case l.MaxQueue == 0:
		c.queue = defaultQueue
	case l.MaxQueue < 0:
		c.queue = 0
	}
	if l.MaxInFlight > 0 {
		c.admit.Store(int64(l.MaxInFlight))
		return c
	}
	c.isAdaptive = true
	c.adaptive.init(machineBounds())
	c.admit.Store(c.adaptive.limit.Load())
	return c
}

func (c *compiledLoadShed) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	hw := httphook.Of(w)
	if hw == nil {
		next()
		return
	}
	v := c.admit.Load()
	if v < 0 {
		v = c.probeAdmitLimit(v)
	}
	if c.inFlight.Add(1) <= abs(v) {
		c.register(r, hw, v < 0)
		next()
		return
	}
	c.inFlight.Add(-1)
	c.enqueue(r, w, hw, next)
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func (c *compiledLoadShed) register(r *http.Request, hw httphook.Writer, isProbe bool) {
	if c.hasBody(r) {
		c.attachBody(r, hw, 0, isProbe)
		return
	}
	if isProbe {
		hw.OnDone(&c.probeDone)
		return
	}
	hw.OnDone(c)
}

func (c *compiledLoadShed) enqueue(r *http.Request, w http.ResponseWriter, hw httphook.Writer, next ctx.Next) {
	c.markPressure()
	if c.queue == 0 {
		c.refuse(w, &c.refusedFull)
	}
	c.mu.Lock()
	if c.listed >= c.queue {
		c.mu.Unlock()
		c.refuse(w, &c.refusedFull)
	}
	c.waiting.Add(1)
	v := c.admit.Load()
	if c.inFlight.Add(1) <= abs(v) {
		c.waiting.Add(-1)
		c.mu.Unlock()
		c.register(r, hw, v < 0)
		next()
		return
	}
	c.inFlight.Add(-1)
	wt := &waiter{c: c, ch: make(chan struct{}, 1), startedAt: time.Now()}
	c.pushLocked(wt)
	c.mu.Unlock()

	timer := time.NewTimer(waitTimeout)
	select {
	case <-wt.ch:
		timer.Stop()
		c.admitWaiter(wt, r, hw, next)
	case <-timer.C:
		if c.leaveQueue(wt) {
			c.refuse(w, &c.refusedTimeout)
		}
		c.admitWaiter(wt, r, hw, next)
	case <-r.Context().Done():
		timer.Stop()
		if !c.leaveQueue(wt) {
			c.release()
		}
		c.refusedCancelled.Add(1)
	}
}

func (c *compiledLoadShed) admitWaiter(wt *waiter, r *http.Request, hw httphook.Writer, next ctx.Next) {
	if c.hasBody(r) {
		c.attachBody(r, hw, wt.waited, wt.isProbe)
	} else {
		hw.OnDone(wt)
	}
	next()
}

func (c *compiledLoadShed) hasBody(r *http.Request) bool {
	return c.isAdaptive && r.Body != nil && r.Body != http.NoBody
}

func (c *compiledLoadShed) attachBody(r *http.Request, hw httphook.Writer, waited time.Duration, isProbe bool) {
	b := &timedBody{inner: r.Body, c: c, waited: waited, isProbe: isProbe}
	r.Body = b
	hw.OnDone(b)
}

func (b *timedBody) Read(p []byte) (int, error) {
	start := time.Now()
	n, err := b.inner.Read(p)
	b.readTime.Add(int64(time.Since(start)))
	return n, err
}

func (b *timedBody) Close() error {
	return b.inner.Close()
}

func (b *timedBody) Done(elapsed time.Duration) {
	b.c.finish(max(elapsed-b.waited-time.Duration(b.readTime.Load()), 0), b.isProbe)
}

func (c *compiledLoadShed) refuse(w http.ResponseWriter, counter *atomic.Int64) {
	counter.Add(1)
	w.Header().Set(retryAfterKey, retryAfterValue)
	panic(&busy)
}

func (c *compiledLoadShed) markPressure() {
	if c.isAdaptive && !c.adaptive.isPressure.Load() {
		c.adaptive.isPressure.Store(true)
	}
}

func (c *compiledLoadShed) pushLocked(wt *waiter) {
	wt.isListed = true
	wt.prev = c.tail
	if c.tail == nil {
		c.head = wt
	} else {
		c.tail.next = wt
	}
	c.tail = wt
	c.listed++
}

func (c *compiledLoadShed) removeLocked(wt *waiter) {
	if wt.prev == nil {
		c.head = wt.next
	} else {
		wt.prev.next = wt.next
	}
	if wt.next == nil {
		c.tail = wt.prev
	} else {
		wt.next.prev = wt.prev
	}
	wt.prev, wt.next, wt.isListed = nil, nil, false
	c.listed--
}

func (c *compiledLoadShed) leaveQueue(wt *waiter) bool {
	c.mu.Lock()
	isListed := wt.isListed
	if isListed {
		c.removeLocked(wt)
		c.waiting.Add(-1)
	}
	c.mu.Unlock()
	return isListed
}

func (c *compiledLoadShed) Done(elapsed time.Duration) {
	c.finish(elapsed, false)
}

func (p *probeHook) Done(elapsed time.Duration) {
	p.c.finish(elapsed, true)
}

func (wt *waiter) Done(elapsed time.Duration) {
	wt.c.finish(max(elapsed-wt.waited, 0), wt.isProbe)
}

func (c *compiledLoadShed) finish(elapsed time.Duration, isProbe bool) {
	n := c.inFlight.Add(-1)
	if c.isAdaptive {
		c.adaptive.sample(c, elapsed, n+1, isProbe)
	}
	if c.waiting.Load() > 0 {
		c.wake()
	}
}

func (c *compiledLoadShed) release() {
	c.inFlight.Add(-1)
	if c.waiting.Load() > 0 {
		c.wake()
	}
}

func (c *compiledLoadShed) wake() {
	c.mu.Lock()
	for c.head != nil {
		v := c.admit.Load()
		if c.inFlight.Add(1) > abs(v) {
			c.inFlight.Add(-1)
			break
		}
		wt := c.head
		c.removeLocked(wt)
		c.waiting.Add(-1)
		wt.isProbe = v < 0
		wt.waited = time.Since(wt.startedAt)
		wt.ch <- struct{}{}
	}
	c.mu.Unlock()
}

func (c *compiledLoadShed) Snapshot() inspect.Snapshot {
	mode := "fixed"
	if c.isAdaptive {
		mode = "adaptive"
	}
	limit := c.admit.Load()
	if c.isAdaptive {
		limit = c.adaptive.limit.Load()
	}
	fields := []inspect.Field{
		{Key: "mode", Value: mode},
		{Key: "limit", Value: limit},
		{Key: "queue", Value: int64(c.queue)},
		{Key: "wait", Value: waitTimeout},
		{Key: "retry_after", Value: retryAfter},
		{Key: "in_flight", Value: c.inFlight.Load()},
		{Key: "waiting", Value: int64(c.waiting.Load())},
		{Key: "refused_queue_full", Value: c.refusedFull.Load()},
		{Key: "refused_timeout", Value: c.refusedTimeout.Load()},
		{Key: "refused_cancelled", Value: c.refusedCancelled.Load()},
	}
	if c.isAdaptive {
		fields = append(fields, c.adaptive.fields(c)...)
	}
	return inspect.Snapshot{Name: "loadshed", Fields: fields}
}
