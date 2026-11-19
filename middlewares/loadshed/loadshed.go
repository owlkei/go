package loadshed

import (
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/httphook"
	"go.owlky.com/owlky/internal/inspect"
)

const (
	maxInFlightLimit = 1_000_000
	maxQueueLimit    = 65_536
	defaultQueue     = 64
	retryAfter       = time.Second
	retryAfterKey    = "Retry-After"
	retryAfterValue  = "1"
)

var waitTimeout = 100 * time.Millisecond

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
	probe     *probe
	mark      int64
	waited    time.Duration
	startedAt time.Time
}

type probe struct {
	c        *compiledLoadShed
	held     atomic.Int64
	sum      atomic.Int64
	count    atomic.Int64
	finished atomic.Int64
}

type probeTicket struct {
	probe *probe
	mark  int64
}

type timedBody struct {
	inner    io.ReadCloser
	c        *compiledLoadShed
	readTime atomic.Int64
	waited   time.Duration
	probe    *probe
	mark     int64
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
	// An arrival at the instant a slot frees may take it (L2); once the fast path fails, enqueue
	// serves the waiting requests first.
	if v >= 0 {
		if c.inFlight.Add(1) <= v {
			c.register(r, hw, nil, 0)
			next()
			return
		}
		c.inFlight.Add(-1)
	} else if p, mark, ok := c.acquireProbe(); ok {
		c.register(r, hw, p, mark)
		next()
		return
	}
	c.enqueue(r, w, hw, next)
}

func (c *compiledLoadShed) acquire(v int64) (*probe, int64, bool) {
	if v < 0 {
		return c.acquireProbe()
	}
	if c.inFlight.Add(1) <= v {
		return nil, 0, true
	}
	c.inFlight.Add(-1)
	return nil, 0, false
}

func (c *compiledLoadShed) acquireProbe() (*probe, int64, bool) {
	p := c.adaptive.current.Load()
	if p.held.Add(1) > c.adaptive.min {
		p.held.Add(-1)
		return nil, 0, false
	}
	if c.inFlight.Add(1) > c.adaptive.limit.Load() {
		c.inFlight.Add(-1)
		p.held.Add(-1)
		return nil, 0, false
	}
	return p, p.finished.Load(), true
}

func (c *compiledLoadShed) register(r *http.Request, hw httphook.Writer, p *probe, mark int64) {
	if c.hasBody(r) {
		c.attachBody(r, hw, 0, p, mark)
		return
	}
	if p != nil {
		hw.OnDone(&probeTicket{probe: p, mark: mark})
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
	if c.head != nil {
		c.wakeLocked()
	}
	if c.listed >= c.queue {
		c.mu.Unlock()
		c.refuse(w, &c.refusedFull)
	}
	c.waiting.Add(1)
	if c.head == nil {
		if p, mark, ok := c.acquire(c.admit.Load()); ok {
			c.waiting.Add(-1)
			c.mu.Unlock()
			c.register(r, hw, p, mark)
			next()
			return
		}
	}
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
			c.release(wt.probe)
		}
		c.refusedCancelled.Add(1)
	}
}

func (c *compiledLoadShed) admitWaiter(wt *waiter, r *http.Request, hw httphook.Writer, next ctx.Next) {
	if c.hasBody(r) {
		c.attachBody(r, hw, wt.waited, wt.probe, wt.mark)
	} else {
		hw.OnDone(wt)
	}
	next()
}

func (c *compiledLoadShed) hasBody(r *http.Request) bool {
	return c.isAdaptive && r.Body != nil && r.Body != http.NoBody
}

func (c *compiledLoadShed) attachBody(r *http.Request, hw httphook.Writer, waited time.Duration, p *probe, mark int64) {
	b := &timedBody{inner: r.Body, c: c, waited: waited, probe: p, mark: mark}
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
	b.c.finish(max(elapsed-b.waited-time.Duration(b.readTime.Load()), 0), b.probe, b.mark)
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
	c.finish(elapsed, nil, 0)
}

func (t *probeTicket) Done(elapsed time.Duration) {
	t.probe.c.finish(elapsed, t.probe, t.mark)
}

func (wt *waiter) Done(elapsed time.Duration) {
	wt.c.finish(max(elapsed-wt.waited, 0), wt.probe, wt.mark)
}

func (c *compiledLoadShed) finish(elapsed time.Duration, p *probe, mark int64) {
	if p != nil {
		p.held.Add(-1)
	}
	n := c.inFlight.Add(-1)
	if c.isAdaptive {
		c.adaptive.sample(c, elapsed, n+1, p, mark)
	}
	if c.waiting.Load() > 0 {
		c.wake()
	}
}

func (c *compiledLoadShed) release(p *probe) {
	if p != nil {
		p.held.Add(-1)
	}
	c.inFlight.Add(-1)
	if c.waiting.Load() > 0 {
		c.wake()
	}
}

func (c *compiledLoadShed) wake() {
	c.mu.Lock()
	c.wakeLocked()
	c.mu.Unlock()
}

func (c *compiledLoadShed) wakeLocked() {
	for c.head != nil {
		p, mark, ok := c.acquire(c.admit.Load())
		if !ok {
			return
		}
		wt := c.head
		c.removeLocked(wt)
		c.waiting.Add(-1)
		wt.probe, wt.mark = p, mark
		wt.waited = time.Since(wt.startedAt)
		wt.ch <- struct{}{}
	}
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
