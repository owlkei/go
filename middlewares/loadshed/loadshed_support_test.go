package loadshed

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/httphook"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
)

const busyBody = `{"code":503,"error":"Service Unavailable","message":"Server is busy"}`

type exchange struct {
	inner http.ResponseWriter
	hooks []httphook.Hook
}

func newExchange(w http.ResponseWriter) *exchange { return &exchange{inner: w} }

func (e *exchange) Header() http.Header            { return e.inner.Header() }
func (e *exchange) Write(p []byte) (int, error)    { return e.inner.Write(p) }
func (e *exchange) WriteHeader(code int)           { e.inner.WriteHeader(code) }
func (e *exchange) Inner() http.ResponseWriter     { return e.inner }
func (e *exchange) SetInner(w http.ResponseWriter) { e.inner = w }
func (e *exchange) OnDone(h httphook.Hook)         { e.hooks = append(e.hooks, h) }
func (e *exchange) finish(elapsed time.Duration) {
	for i := len(e.hooks) - 1; i >= 0; i-- {
		e.hooks[i].Done(elapsed)
	}
	clear(e.hooks)
	e.hooks = e.hooks[:0]
}

type call struct {
	rec      *httptest.ResponseRecorder
	ex       *exchange
	done     chan struct{}
	admitted atomic.Bool
	refusal  any
}

func startCall(mw common.MiddlewareFn, r *http.Request) *call {
	c := &call{rec: httptest.NewRecorder(), done: make(chan struct{})}
	c.ex = newExchange(c.rec)
	go func() {
		defer close(c.done)
		defer func() { c.refusal = recover() }()
		mw.Use(r, c.ex, func() { c.admitted.Store(true) })
	}()
	return c
}

func (c *call) returned() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *call) wait(t *testing.T) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(30 * time.Second):
		t.Fatal("a call did not return")
	}
}

func admitNow(t *testing.T, mw common.MiddlewareFn) *call {
	t.Helper()
	c := startCall(mw, request(http.MethodGet, "/ok"))
	c.wait(t)
	if !c.admitted.Load() || c.refusal != nil {
		t.Fatal(test.DiffMessage([]any{c.admitted.Load(), c.refusal}, []any{true, nil}, "fixture: the request is admitted"))
	}
	return c
}

func tryAdmit(mw common.MiddlewareFn) (*call, bool) {
	c := &call{rec: httptest.NewRecorder(), done: make(chan struct{})}
	c.ex = newExchange(c.rec)
	func() {
		defer func() { c.refusal = recover() }()
		mw.Use(request(http.MethodGet, "/ok"), c.ex, func() { c.admitted.Store(true) })
	}()
	return c, c.admitted.Load()
}

func refusalOf(v any) (code int, message string, ok bool) {
	var e *exception.Exception
	switch x := v.(type) {
	case *exception.Exception:
		e = x
	case exception.Exception:
		e = &x
	}
	if e == nil {
		return 0, "", false
	}
	return e.GetCode(), e.GetMessage(), true
}

func expectBusy(t *testing.T, c *call, desc string) {
	t.Helper()
	code, message, ok := refusalOf(c.refusal)
	if !ok || code != http.StatusServiceUnavailable || message != "Server is busy" {
		t.Error(test.DiffMessage([]any{ok, code, message}, []any{true, 503, "Server is busy"}, "L3, G1: "+desc))
	}
	if got := c.rec.Header().Get("Retry-After"); got != "1" {
		t.Error(test.DiffMessage(got, "1", "L3: Retry-After on "+desc))
	}
}

func intOf(s inspect.Snapshot, key string) int64 {
	v, _ := field(s, key).(int64)
	return v
}

func durationOf(s inspect.Snapshot, key string) time.Duration {
	v, _ := field(s, key).(time.Duration)
	return v
}

func waitUntil(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for: " + desc)
		}
		runtime.Gosched()
	}
}

func snapshotter(t testing.TB, mw common.MiddlewareFn) inspect.Snapshotter {
	t.Helper()
	s, ok := mw.(inspect.Snapshotter)
	if !ok {
		t.Fatal(test.DiffMessage(mw, "an inspect.Snapshotter", "I1: the compiled loadshed implements Snapshotter"))
	}
	return s
}

func withMachine(t *testing.T, procs int, memoryLimit int64) {
	t.Helper()
	previousProcs := runtime.GOMAXPROCS(procs)
	previousLimit := debug.SetMemoryLimit(memoryLimit)
	t.Cleanup(func() {
		runtime.GOMAXPROCS(previousProcs)
		debug.SetMemoryLimit(previousLimit)
	})
}

type terminal struct{ ID int }

type terminalFunc func(w http.ResponseWriter, r *http.Request, next ctx.Next)

var (
	terminals    sync.Map
	nextTerminal atomic.Int64
)

func (t terminal) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	fn, _ := terminals.Load(t.ID)
	fn.(terminalFunc)(w, r, next)
}

func terminalMiddleware(fn terminalFunc) terminal {
	id := int(nextTerminal.Add(1))
	terminals.Store(id, fn)
	return terminal{ID: id}
}

func isRefused(w *httptest.ResponseRecorder) bool {
	return w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "Server is busy")
}
