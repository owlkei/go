package logpipe

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

type gateWriter struct {
	mu          sync.Mutex
	gate        chan struct{}
	entered     chan struct{}
	calls       [][]byte
	ok          [][]byte
	failOn      map[int]error
	panicOn     map[int]bool
	inflight    int
	maxInflight int
	onWrite     func(n int, p []byte)
}

func newGateWriter() *gateWriter {
	return &gateWriter{
		entered: make(chan struct{}, 1<<16),
		failOn:  map[int]error{},
		panicOn: map[int]bool{},
	}
}

func (w *gateWriter) hold() {
	w.mu.Lock()
	w.gate = make(chan struct{})
	w.mu.Unlock()
}

func (w *gateWriter) openAll() {
	w.mu.Lock()
	if w.gate != nil {
		close(w.gate)
		w.gate = nil
	}
	w.mu.Unlock()
}

func (w *gateWriter) release() {
	w.mu.Lock()
	g := w.gate
	w.mu.Unlock()
	g <- struct{}{}
}

func (w *gateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n := len(w.calls) + 1
	w.calls = append(w.calls, bytes.Clone(p))
	w.inflight++
	w.maxInflight = max(w.maxInflight, w.inflight)
	gate := w.gate
	hook := w.onWrite
	w.mu.Unlock()
	w.entered <- struct{}{}
	if hook != nil {
		hook(n, p)
	}
	if gate != nil {
		<-gate
	}
	w.mu.Lock()
	w.inflight--
	err := w.failOn[n]
	pan := w.panicOn[n]
	if err == nil && !pan {
		w.ok = append(w.ok, bytes.Clone(p))
	}
	w.mu.Unlock()
	if pan {
		panic("output exploded")
	}
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *gateWriter) output() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(bytes.Join(w.ok, nil))
}

func (w *gateWriter) sizes() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]int, len(w.calls))
	for i, c := range w.calls {
		out[i] = len(c)
	}
	return out
}

func (w *gateWriter) maxConcurrent() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.maxInflight
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fakeClock struct{ ns atomic.Int64 }

func (c *fakeClock) now() time.Time { return time.Unix(0, c.ns.Load()) }

func (c *fakeClock) advance(d time.Duration) { c.ns.Add(int64(d)) }

type reportRecorder struct {
	mu    sync.Mutex
	calls [][4]uint64
	line  string
	first chan struct{}
	once  sync.Once
}

func newReportRecorder(line string) *reportRecorder {
	return &reportRecorder{line: line, first: make(chan struct{})}
}

func (r *reportRecorder) fn(dropped [4]uint64) []byte {
	r.mu.Lock()
	r.calls = append(r.calls, dropped)
	r.mu.Unlock()
	r.once.Do(func() { close(r.first) })
	return []byte(r.line)
}

func (r *reportRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *reportRecorder) all() [][4]uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][4]uint64(nil), r.calls...)
}

type fakeAfter struct {
	calls   atomic.Int64
	waiting chan struct{}
	timeout bool
}

func newFakeAfter(timeout bool) *fakeAfter {
	return &fakeAfter{waiting: make(chan struct{}, 1<<10), timeout: timeout}
}

func (f *fakeAfter) after(d time.Duration) <-chan time.Time {
	if d != fullWait {
		return time.After(d)
	}
	f.calls.Add(1)
	select {
	case f.waiting <- struct{}{}:
	default:
	}
	ch := make(chan time.Time, 1)
	if f.timeout {
		ch <- time.Time{}
	}
	return ch
}

func rec(n int, ch byte) []byte {
	if n < 1 {
		n = 1
	}
	b := bytes.Repeat([]byte{ch}, n)
	b[n-1] = '\n'
	return b
}

func numbered(i, n int) []byte {
	head := fmt.Sprintf("%07d ", i)
	if n < len(head)+1 {
		n = len(head) + 1
	}
	b := bytes.Repeat([]byte{'.'}, n)
	copy(b, head)
	b[n-1] = '\n'
	return b
}

func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal(test.DiffMessage("blocked for 30s", "returned", what))
	}
}

func goroutinesAtMost(base int) bool {
	for range 200000 {
		if runtime.NumGoroutine() <= base {
			return true
		}
		runtime.Gosched()
	}
	return runtime.NumGoroutine() <= base
}

func flushOK(t *testing.T, s *Sink, what string) {
	t.Helper()
	if !s.Flush(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, what))
	}
}

func countLines(out string, prefix byte) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 0 && line[0] == prefix {
			n++
		}
	}
	return n
}

type heldSink struct {
	s     *Sink
	w     *gateWriter
	clock *fakeClock
	q     int
}

func newHeldSink(t *testing.T) *heldSink {
	t.Helper()
	w := newGateWriter()
	w.hold()
	clock := &fakeClock{}
	clock.ns.Store(time.Now().UnixNano())
	s := New(w)
	s.now = clock.now
	s.StartAsync()
	t.Cleanup(func() {
		w.openAll()
		s.Flush(time.Minute)
	})
	return &heldSink{s: s, w: w, clock: clock}
}

func (h *heldSink) write(level Level, n int, ch byte, report func([4]uint64) []byte) {
	if report == nil {
		report = quietReport
	}
	h.s.Write(level, rec(n, ch), report)
	h.q += n
}

func (h *heldSink) fillTo(level Level, target int, ch byte) {
	for h.q < target {
		h.write(level, min(target-h.q, 60000), ch, nil)
	}
}

func (h *heldSink) startWriter() {
	h.write(Info, 1000, 'a', nil)
	<-h.w.entered
}

func quietReport([4]uint64) []byte { return []byte("R quiet\n") }
