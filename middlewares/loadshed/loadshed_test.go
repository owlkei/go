package loadshed

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/core"
	"go.owlky.com/owlky/internal/test"
	"golang.org/x/net/websocket"
)

func TestNewLoadShed_Panics(t *testing.T) {
	for _, c := range []LoadShed{{MaxInFlight: -1}, {MaxInFlight: 1_000_001}, {MaxInFlight: 100_000_000}, {MaxQueue: 65_537}, {MaxQueue: 1 << 30}} {
		text, ok := panicText(func() { c.NewMiddleware() })
		if !ok || !strings.Contains(text, "invalid loadshed:") {
			t.Error(test.DiffMessage(text, "invalid loadshed: ...", "L5, G4: "+strings.TrimSpace(text)+" for "+describe(c)))
		}
	}
}

func TestNewLoadShed_ValidOptions(t *testing.T) {
	for _, c := range []LoadShed{{}, {MaxInFlight: 1}, {MaxInFlight: 1_000_000}, {MaxQueue: 65_536}, {MaxQueue: -1}, {MaxQueue: -1000}, {MaxInFlight: 8, MaxQueue: 0}} {
		if text, ok := panicText(func() { c.NewMiddleware() }); ok {
			t.Error(test.DiffMessage(text, "<no panic>", "L5: valid "+describe(c)))
		}
	}
}

func TestLoadShed_Fixed_NeverAboveMax(t *testing.T) {
	const max = 4
	mw := LoadShed{MaxInFlight: max, MaxQueue: -1}.NewMiddleware()
	var current, peak, admitted, refused atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 12; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				c, ok := tryAdmit(mw)
				if !ok {
					refused.Add(1)
					continue
				}
				admitted.Add(1)
				now := current.Add(1)
				for {
					seen := peak.Load()
					if now <= seen || peak.CompareAndSwap(seen, now) {
						break
					}
				}
				runtime.Gosched()
				current.Add(-1)
				c.ex.finish(time.Microsecond)
			}
		}()
	}
	wg.Wait()
	if peak.Load() > max {
		t.Error(test.DiffMessage(peak.Load(), "<= 4", "L1: at most MaxInFlight requests between admission and the end of their exchange"))
	}
	if admitted.Load() == 0 {
		t.Error(test.DiffMessage(admitted.Load(), "> 0", "fixture: some requests are admitted"))
	}
	s := snapshotter(t, mw).Snapshot()
	if got := intOf(s, "in_flight"); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "L4: every slot is released"))
	}
}

func TestLoadShed_Fixed_ExactlyMaxAdmitted(t *testing.T) {
	mw := LoadShed{MaxInFlight: 3, MaxQueue: -1}.NewMiddleware()
	var held []*call
	for i := 0; i < 3; i++ {
		held = append(held, admitNow(t, mw))
	}
	c, ok := tryAdmit(mw)
	if ok {
		t.Fatal(test.DiffMessage(ok, false, "L1: the fourth request is not admitted"))
	}
	expectBusy(t, c, "the request over the limit")
	held[0].ex.finish(time.Millisecond)
	if _, ok := tryAdmit(mw); !ok {
		t.Error(test.DiffMessage(ok, true, "L4: a finished request frees its slot"))
	}
	if got := intOf(snapshotter(t, mw).Snapshot(), "limit"); got != 3 {
		t.Error(test.DiffMessage(got, int64(3), "L1: fixed mode, the limit is MaxInFlight"))
	}
}

func TestLoadShed_QueueAdmitsInOrder(t *testing.T) {
	mw := LoadShed{MaxInFlight: 1, MaxQueue: 4}.NewMiddleware()
	s := snapshotter(t, mw)
	first := admitNow(t, mw)
	var queued []*call
	for i := 1; i <= 3; i++ {
		queued = append(queued, startCall(mw, request(http.MethodGet, "/ok")))
		want := int64(i)
		waitUntil(t, "a waiter is queued", func() bool { return intOf(s.Snapshot(), "waiting") == want })
	}
	first.ex.finish(time.Millisecond)
	for i, c := range queued {
		c.wait(t)
		if !c.admitted.Load() || c.refusal != nil {
			t.Fatal(test.DiffMessage([]any{c.admitted.Load(), c.refusal}, []any{true, nil}, "L2: the waiter is admitted when a slot frees"))
		}
		for _, later := range queued[i+1:] {
			if later.returned() {
				t.Error(test.DiffMessage(true, false, "L2: the longest waiter is admitted first"))
			}
		}
		if i < len(queued)-1 {
			c.ex.finish(time.Millisecond)
		}
	}
	queued[len(queued)-1].ex.finish(time.Millisecond)
	if got := intOf(s.Snapshot(), "in_flight"); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "L4: every slot released"))
	}
	if got := intOf(s.Snapshot(), "waiting"); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "L2: no waiter left"))
	}
}

func TestLoadShed_QueueFull_503(t *testing.T) {
	mw := LoadShed{MaxInFlight: 1, MaxQueue: 1}.NewMiddleware()
	s := snapshotter(t, mw)
	first := admitNow(t, mw)
	waiter := startCall(mw, request(http.MethodGet, "/ok"))
	waitUntil(t, "the waiter is queued", func() bool { return intOf(s.Snapshot(), "waiting") == 1 })

	full := startCall(mw, request(http.MethodGet, "/ok"))
	full.wait(t)
	if full.admitted.Load() {
		t.Fatal(test.DiffMessage(true, false, "L3: a request that finds the queue full is refused"))
	}
	expectBusy(t, full, "queue full")
	if got := intOf(s.Snapshot(), "refused_queue_full"); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "I4: refused, queue full"))
	}
	if waiter.returned() {
		t.Error(test.DiffMessage(true, false, "L3: the queued request keeps waiting"))
	}
	first.ex.finish(time.Millisecond)
	waiter.wait(t)
	if !waiter.admitted.Load() {
		t.Error(test.DiffMessage(false, true, "L2: the queued request is admitted"))
	}
	waiter.ex.finish(time.Millisecond)
}

func TestLoadShed_QueueTimeout_503(t *testing.T) {
	mw := LoadShed{MaxInFlight: 1, MaxQueue: 1}.NewMiddleware()
	s := snapshotter(t, mw)
	first := admitNow(t, mw)
	waiter := startCall(mw, request(http.MethodGet, "/ok"))
	waiter.wait(t)
	if waiter.admitted.Load() {
		t.Fatal(test.DiffMessage(true, false, "L3: a request that waits without a slot is refused"))
	}
	expectBusy(t, waiter, "a wait that timed out")
	snap := s.Snapshot()
	if intOf(snap, "refused_timeout") != 1 || intOf(snap, "waiting") != 0 {
		t.Error(test.DiffMessage([]int64{intOf(snap, "refused_timeout"), intOf(snap, "waiting")}, []int64{1, 0}, "I4: refused after the wait, nobody waiting"))
	}
	first.ex.finish(time.Millisecond)
	if _, ok := tryAdmit(mw); !ok {
		t.Error(test.DiffMessage(ok, true, "L3: a refused waiter takes no slot"))
	}
}

func TestLoadShed_ContextCancelledWhileWaiting(t *testing.T) {
	mw := LoadShed{MaxInFlight: 1, MaxQueue: 2}.NewMiddleware()
	s := snapshotter(t, mw)
	first := admitNow(t, mw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiter := startCall(mw, request(http.MethodGet, "/ok").WithContext(ctx))
	waitUntil(t, "the request is queued", func() bool { return intOf(s.Snapshot(), "waiting") == 1 })
	cancel()
	waiter.wait(t)
	if waiter.admitted.Load() {
		t.Error(test.DiffMessage(true, false, "L3: a cancelled request is not admitted"))
	}
	if waiter.refusal != nil {
		t.Error(test.DiffMessage(waiter.refusal, nil, "L3: a request whose context ended gets no response written, so nothing is thrown"))
	}
	if waiter.rec.Body.Len() != 0 || waiter.rec.Flushed {
		t.Error(test.DiffMessage(waiter.rec.Body.String(), "", "L3: no response is written"))
	}
	snap := s.Snapshot()
	if intOf(snap, "refused_cancelled") != 1 || intOf(snap, "waiting") != 0 {
		t.Error(test.DiffMessage([]int64{intOf(snap, "refused_cancelled"), intOf(snap, "waiting")}, []int64{1, 0}, "I4: refused, cancelled; the waiter removed itself"))
	}
	first.ex.finish(time.Millisecond)
	if _, ok := tryAdmit(mw); !ok {
		t.Error(test.DiffMessage(ok, true, "L4: no slot leaks through a cancelled waiter"))
	}
}

func TestLoadShed_NoQueue(t *testing.T) {
	mw := LoadShed{MaxInFlight: 1, MaxQueue: -1}.NewMiddleware()
	s := snapshotter(t, mw)
	first := admitNow(t, mw)
	c := startCall(mw, request(http.MethodGet, "/ok"))
	c.wait(t)
	expectBusy(t, c, "no queue")
	snap := s.Snapshot()
	if intOf(snap, "refused_queue_full") != 1 || intOf(snap, "refused_timeout") != 0 || intOf(snap, "waiting") != 0 {
		t.Error(test.DiffMessage([]int64{intOf(snap, "refused_queue_full"), intOf(snap, "refused_timeout"), intOf(snap, "waiting")}, []int64{1, 0, 0}, "L5: a negative MaxQueue never waits"))
	}
	if got := intOf(snap, "queue"); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "I4: queue size 0 when there is no queue"))
	}
	first.ex.finish(time.Millisecond)
}

func TestLoadShed_QueueDefaultsTo64(t *testing.T) {
	for _, c := range []LoadShed{{}, {MaxInFlight: 1}, {MaxInFlight: 1, MaxQueue: 0}} {
		if got := intOf(snapshotter(t, c.NewMiddleware()).Snapshot(), "queue"); got != 64 {
			t.Error(test.DiffMessage(got, int64(64), "L2: the default queue is 64: "+describe(c)))
		}
	}
}

func TestLoadShed_ReleasedOnEveryOutcome(t *testing.T) {
	app := newApp(t, LoadShed{MaxInFlight: 1, MaxQueue: -1}, terminalMiddleware(func(w http.ResponseWriter, r *http.Request, next func()) {
		switch r.Header.Get("X-Mode") {
		case "panic":
			panic("boom")
		case "refuse":
			panic(exception.ForbiddenException("refused by a later middleware"))
		case "abort":
			panic(http.ErrAbortHandler)
		case "stop":
			w.WriteHeader(http.StatusTeapot)
		default:
			next()
		}
	}))
	check := func(desc string) {
		t.Helper()
		w := serve(app, request(http.MethodGet, "/ok"))
		if w.Code != http.StatusOK || w.Body.String() != "ok" {
			t.Fatal(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{200, "ok"}, "L4: the slot was released after "+desc))
		}
	}
	check("nothing happened")
	check("the first check")

	serve(app, request(http.MethodGet, "/ok", "X-Mode", "panic"))
	check("a handler panic")
	serve(app, request(http.MethodGet, "/ok", "X-Mode", "refuse"))
	check("a refusal by a later middleware")
	serve(app, request(http.MethodGet, "/ok", "X-Mode", "stop"))
	check("a middleware that answered itself")
	if w := serve(app, request(http.MethodGet, "/missing")); w.Code != http.StatusNotFound {
		t.Fatal(test.DiffMessage(w.Code, http.StatusNotFound, "fixture: no route"))
	}
	check("a 404")
	func() {
		defer func() { _ = recover() }()
		serve(app, request(http.MethodGet, "/ok", "X-Mode", "abort"))
	}()
	check("http.ErrAbortHandler")
}

func TestLoadShed_ReleasedAtWSHandshake(t *testing.T) {
	app := core.New()
	app.SetLogger(silentLogger{})
	shed := LoadShed{MaxInFlight: 1, MaxQueue: -1}
	app.EnableWS(&core.WSOptions{Path: "/ws"}, shed)
	app.Create(owlky.ModuleBuilder().Build())
	server := httptest.NewServer(app)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	first, err := websocket.Dial(url, "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	second, err := websocket.Dial(url, "", server.URL)
	if err != nil {
		t.Fatal(test.DiffMessage(err, nil, "L4: the slot is released when the handshake ends, not when the connection closes"))
	}
	_ = second.Close()
}

func TestLoadShed_RefusedWSHandshake(t *testing.T) {
	app := core.New()
	app.SetLogger(silentLogger{})
	app.EnableWS(&core.WSOptions{Path: "/ws"}, LoadShed{MaxInFlight: 1, MaxQueue: -1}, terminalMiddleware(func(w http.ResponseWriter, r *http.Request, next func()) {
		panic(exception.ForbiddenException("no"))
	}))
	app.Create(owlky.ModuleBuilder().Build())
	server := httptest.NewServer(app)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	for i := 0; i < 3; i++ {
		if conn, err := websocket.Dial(url, "", server.URL); err == nil {
			_ = conn.Close()
			t.Fatal(test.DiffMessage(nil, "a refused handshake", "fixture: the handshake is refused"))
		}
	}
}

func TestLoadShed_Burst(t *testing.T) {
	const capacity = 2
	var current, peak atomic.Int64
	app := newApp(t, LoadShed{MaxInFlight: capacity}, terminalMiddleware(func(w http.ResponseWriter, r *http.Request, next func()) {
		now := current.Add(1)
		for {
			seen := peak.Load()
			if now <= seen || peak.CompareAndSwap(seen, now) {
				break
			}
		}
		for i := 0; i < 5; i++ {
			runtime.Gosched()
		}
		current.Add(-1)
		next()
	}))
	var wg sync.WaitGroup
	var ok, busy, other atomic.Int64
	for i := 0; i < 50*capacity; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := serve(app, request(http.MethodGet, "/ok"))
			switch {
			case w.Code == http.StatusOK && w.Body.String() == "ok":
				ok.Add(1)
			case isRefused(w):
				busy.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	wg.Wait()
	if other.Load() != 0 {
		t.Error(test.DiffMessage(other.Load(), int64(0), "L3: every response is 2xx or 503"))
	}
	if peak.Load() > capacity {
		t.Error(test.DiffMessage(peak.Load(), "<= 2", "L1: the count in flight never exceeds the maximum"))
	}
	if ok.Load() == 0 {
		t.Error(test.DiffMessage(ok.Load(), "> 0", "L2: the server keeps serving during the burst"))
	}
	if current.Load() != 0 {
		t.Error(test.DiffMessage(current.Load(), int64(0), "L4: the count in flight returns to 0"))
	}
	if w := serve(app, request(http.MethodGet, "/ok")); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "L4: after the burst every slot is free"))
	}
}

func TestLoadShed_RefusalIsHTTPLayerResponse(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	app := newApp(t, LoadShed{MaxInFlight: 1, MaxQueue: -1}, terminalMiddleware(func(w http.ResponseWriter, r *http.Request, next func()) {
		if r.Header.Get("X-Hold") != "" {
			entered <- struct{}{}
			<-release
		}
		next()
	}))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- serve(app, request(http.MethodGet, "/ok", "X-Hold", "1")) }()
	<-entered
	w := serve(app, request(http.MethodGet, "/ok"))
	expectRefusal(t, w, http.StatusServiceUnavailable, busyBody, "a request over the limit")
	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Error(test.DiffMessage(got, "1", "L3: Retry-After: 1"))
	}
	close(release)
	if held := <-done; held.Code != http.StatusOK {
		t.Error(test.DiffMessage(held.Code, http.StatusOK, "the held request completes"))
	}
}

func TestLoadShed_KeepsRequestAndContext(t *testing.T) {
	app := newApp(t, probeMiddleware{Name: "before"}, LoadShed{}, probeMiddleware{Name: "after"})
	serve(app, request(http.MethodGet, "/ok"))
	before, after := probeOf("before"), probeOf("after")
	if before.request == nil || before.request != after.request {
		t.Error(test.DiffMessage(after.request, before.request, "G2: the next layer receives the same *http.Request"))
	}
	if before.context != after.context {
		t.Error(test.DiffMessage(after.context, before.context, "G2: the next layer sees the same Context()"))
	}
}

func TestLoadShed_DoesNotReadBody(t *testing.T) {
	for _, c := range []LoadShed{{}, {MaxInFlight: 2}} {
		app := newApp(t, c)
		r := request(http.MethodPost, "/ok")
		r.Body = failBody{t}
		serve(app, r)
	}
}

func TestLoadShed_BodyReadTimeNotSampled(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	r := request(http.MethodPost, "/upload")
	r.Body = &scriptedBody{delay: 50 * time.Millisecond}
	if !g.admitRequest(r) {
		t.Fatal("fixture: the request is admitted")
	}
	if _, err := r.Body.Read(make([]byte, 8)); err != io.EOF {
		t.Fatal(test.DiffMessage(err, io.EOF, "fixture: the body is read"))
	}
	g.finishOldest(60 * time.Millisecond)
	g.sequential(10*time.Millisecond, g.window()-1)
	if got := durationOf(g.s.Snapshot(), "average"); got > 10*time.Millisecond {
		t.Error(test.DiffMessage(got, "<= 10ms", "L8: the time spent inside Read on the request body is not part of the sample"))
	}
}

func TestLoadShed_QueuedBodyRequestLeavesOutWaitAndBodyTime(t *testing.T) {
	withMachine(t, rigProcs, rigMemory)
	mw := LoadShed{MaxQueue: 1}.NewMiddleware()
	clock := &fakeClock{t: time.Now()}
	mw.(*compiledLoadShed).adaptive.now = clock.Now
	g := &rig{t: t, mw: mw, s: snapshotter(t, mw), clock: clock}
	g.sequential(5*time.Millisecond, g.window())
	g.fill(rigStart)
	r := request(http.MethodPost, "/upload")
	r.Body = &scriptedBody{delay: 30 * time.Millisecond}
	queued := startCall(mw, r)
	waitUntil(t, "the request is queued", func() bool { return intOf(g.s.Snapshot(), "waiting") == 1 })
	since := time.Now()
	waitUntil(t, "20 ms in the queue", func() bool { return time.Since(since) >= 20*time.Millisecond })
	g.finishOldest(5 * time.Millisecond)
	queued.wait(t)
	if !queued.admitted.Load() {
		t.Fatal(test.DiffMessage(queued.admitted.Load(), true, "fixture: the queued request is admitted when a slot frees"))
	}
	if _, err := r.Body.Read(make([]byte, 8)); err != io.EOF {
		t.Fatal(test.DiffMessage(err, io.EOF, "fixture: the body is read"))
	}
	queued.ex.finish(60 * time.Millisecond)
	for len(g.pool) > 0 {
		g.finishOldest(5 * time.Millisecond)
	}
	g.sequential(5*time.Millisecond, 4*rigStart-(rigStart+1))
	if got := durationOf(g.s.Snapshot(), "average"); got > 5050*time.Microsecond {
		t.Error(test.DiffMessage(got, "<= 5.05ms", "L8: neither the queue wait nor the body time of a queued request is part of the sample"))
	}
}

func TestLoadShed_BodyWrapperForwardsReadAndClose(t *testing.T) {
	boom := errors.New("boom")
	g := newRig(t)
	inner := &scriptedBody{
		steps:    []scriptedStep{{"abc", nil}, {"de", boom}, {"", io.EOF}},
		closeErr: boom,
	}
	r := request(http.MethodPost, "/upload")
	r.Body = inner
	if !g.admitRequest(r) {
		t.Fatal("fixture: the request is admitted")
	}
	if r.Body == io.ReadCloser(inner) {
		t.Error(test.DiffMessage(false, true, "L8: in adaptive mode an admitted request with a body gets a wrapper"))
	}
	buf := make([]byte, 8)
	for i, want := range []struct {
		n   int
		err error
	}{{3, nil}, {2, boom}, {0, io.EOF}} {
		n, err := r.Body.Read(buf)
		if n != want.n || err != want.err {
			t.Error(test.DiffMessage([]any{n, err}, []any{want.n, want.err}, "L8: Read forwards the byte count and the error of read "+strconv.Itoa(i)))
		}
	}
	if err := r.Body.Close(); err != boom || inner.closed != 1 {
		t.Error(test.DiffMessage([]any{err, inner.closed}, []any{boom, 1}, "L8: Close reaches the inner body once and returns its error"))
	}
	g.drain(time.Millisecond)
}

func TestLoadShed_BodyWrapperKeepsMaxBytesError(t *testing.T) {
	g := newRig(t)
	rec := httptest.NewRecorder()
	r := request(http.MethodPost, "/upload")
	r.Body = http.MaxBytesReader(rec, io.NopCloser(strings.NewReader("hello world")), 5)
	if !g.admitRequest(r) {
		t.Fatal("fixture: the request is admitted")
	}
	got, err := io.ReadAll(r.Body)
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) || tooLarge.Limit != 5 {
		t.Error(test.DiffMessage(err, "a *http.MaxBytesError with Limit 5", "L8: the body limit and its error are untouched"))
	}
	if string(got) != "hello" {
		t.Error(test.DiffMessage(string(got), "hello", "L8: the bytes read before the limit are the same"))
	}
	g.drain(time.Millisecond)
}

func TestLoadShed_BodyNotWrappedWhenFixed(t *testing.T) {
	mw := LoadShed{MaxInFlight: 4}.NewMiddleware()
	body := &scriptedBody{}
	r := request(http.MethodPost, "/upload")
	r.Body = body
	c := &call{rec: httptest.NewRecorder()}
	c.ex = newExchange(c.rec)
	mw.Use(r, c.ex, noop)
	if r.Body != io.ReadCloser(body) {
		t.Error(test.DiffMessage(false, true, "L8: fixed mode keeps r.Body"))
	}
	c.ex.finish(time.Millisecond)
}

func TestLoadShed_BodyNotWrappedWhenRefused(t *testing.T) {
	g := newRig(t)
	g.fill(rigStart)
	body := &scriptedBody{}
	r := request(http.MethodPost, "/upload")
	r.Body = body
	if g.admitRequest(r) {
		t.Fatal(test.DiffMessage(true, false, "fixture: a request over the limit is refused"))
	}
	if r.Body != io.ReadCloser(body) {
		t.Error(test.DiffMessage(false, true, "L8: a refused request keeps r.Body"))
	}
	g.drain(time.Millisecond)
}

func TestLoadShed_NoBodyLeftAlone(t *testing.T) {
	g := newRig(t)
	for _, body := range []io.ReadCloser{nil, http.NoBody} {
		r := request(http.MethodPost, "/upload")
		r.Body = body
		if !g.admitRequest(r) {
			t.Fatal("fixture: the request is admitted")
		}
		if r.Body != body {
			t.Error(test.DiffMessage(r.Body, body, "L8: a nil or empty body is not wrapped"))
		}
	}
	g.drain(time.Millisecond)
}

func TestLoadShed_OutsideAnApp_DegradesToNoop(t *testing.T) {
	for _, c := range []LoadShed{{}, {MaxInFlight: 1, MaxQueue: -1}} {
		mw := c.NewMiddleware()
		for i := 0; i < 3; i++ {
			called := false
			rec := httptest.NewRecorder()
			mw.Use(request(http.MethodGet, "/ok"), rec, func() { called = true })
			if !called || len(rec.Header()) != 0 {
				t.Error(test.DiffMessage([]any{called, len(rec.Header())}, []any{true, 0}, "K5: outside an app the middleware only calls next"))
			}
		}
	}
}

func TestLoadShed_IdleNoGoroutine(t *testing.T) {
	for _, c := range []LoadShed{{MaxInFlight: 4}, {}} {
		before := runtime.NumGoroutine()
		mw := c.NewMiddleware()
		for i := 0; i < 2000; i++ {
			call, ok := tryAdmit(mw)
			if !ok {
				t.Fatal(test.DiffMessage(ok, true, "fixture: a sequential request is admitted"))
			}
			call.ex.finish(time.Duration(i%7+1) * time.Millisecond)
		}
		if after := runtime.NumGoroutine(); after > before {
			t.Error(test.DiffMessage(after, before, "L6: an idle instance has no goroutine: "+describe(c)))
		}
	}
}

func TestLoadShed_NoGoroutineLeftAfterQueueing(t *testing.T) {
	before := runtime.NumGoroutine()
	mw := LoadShed{MaxInFlight: 1, MaxQueue: 4}.NewMiddleware()
	first := admitNow(t, mw)
	waiter := startCall(mw, request(http.MethodGet, "/ok"))
	waiter.wait(t)
	first.ex.finish(time.Millisecond)
	waitUntil(t, "goroutines return to the baseline", func() bool { return runtime.NumGoroutine() <= before })
}

func TestLoadShed_Snapshot_Fixed(t *testing.T) {
	mw := LoadShed{MaxInFlight: 2, MaxQueue: 3}.NewMiddleware()
	s := snapshotter(t, mw)
	snap := s.Snapshot()
	if snap.Name != "loadshed" {
		t.Error(test.DiffMessage(snap.Name, "loadshed", "I4: the snapshot name"))
	}
	if got := field(snap, "mode"); got != "fixed" {
		t.Error(test.DiffMessage(got, "fixed", "I4: mode"))
	}
	for key, want := range map[string]int64{"limit": 2, "queue": 3, "in_flight": 0, "waiting": 0, "refused_queue_full": 0, "refused_timeout": 0, "refused_cancelled": 0} {
		if got := field(snap, key); got != want {
			t.Error(test.DiffMessage(got, want, "I4: "+key))
		}
	}
	if got := field(snap, "wait"); got != 100*time.Millisecond {
		t.Error(test.DiffMessage(got, 100*time.Millisecond, "I4: wait is fixed at 100 ms"))
	}
	if got := field(snap, "retry_after"); got != time.Second {
		t.Error(test.DiffMessage(got, time.Second, "I4: Retry-After is fixed at 1 s"))
	}

	a := admitNow(t, mw)
	b := admitNow(t, mw)
	if got := intOf(s.Snapshot(), "in_flight"); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I4: in flight"))
	}
	c := startCall(mw, request(http.MethodGet, "/ok"))
	waitUntil(t, "one waiter", func() bool { return intOf(s.Snapshot(), "waiting") == 1 })
	a.ex.finish(time.Millisecond)
	c.wait(t)
	c.ex.finish(time.Millisecond)
	b.ex.finish(time.Millisecond)
	if got := intOf(s.Snapshot(), "in_flight"); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "I4: in flight after every exchange ended"))
	}
}

func TestLoadShed_Snapshot_ConcurrentWithTraffic(t *testing.T) {
	mw := LoadShed{MaxInFlight: 4, MaxQueue: -1}.NewMiddleware()
	s := snapshotter(t, mw)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.Snapshot()
			}
		}
	}()
	var traffic sync.WaitGroup
	for g := 0; g < 8; g++ {
		traffic.Add(1)
		go func() {
			defer traffic.Done()
			for i := 0; i < 300; i++ {
				if c, ok := tryAdmit(mw); ok {
					c.ex.finish(time.Microsecond)
				}
			}
		}()
	}
	traffic.Wait()
	close(stop)
	wg.Wait()
}

func TestLoadShed_Allocations_Fixed(t *testing.T) {
	mw := LoadShed{MaxInFlight: 64}.NewMiddleware()
	w := &discardWriter{header: http.Header{}}
	hook := newExchange(w)
	r := request(http.MethodGet, "/ok")
	got := testing.AllocsPerRun(200, func() {
		mw.Use(r, hook, noop)
		hook.finish(time.Millisecond)
	})
	if got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "L1: an admitted request costs no allocation"))
	}
}

func TestLoadShed_AdaptiveBodyAdmissionAllocatesAtMostOnce(t *testing.T) {
	step := bodyAdmission()
	if got := testing.AllocsPerRun(200, step); got > 1 {
		t.Error(test.DiffMessage(got, "<= 1", "L8: an admitted request with a body costs one allocation, the wrapper"))
	}
}

func TestLoadShed_AdaptiveAdmissionWithoutBodyAllocatesNothing(t *testing.T) {
	mw := LoadShed{}.NewMiddleware()
	ex := newExchange(&discardWriter{header: http.Header{}})
	r := request(http.MethodGet, "/ok")
	step := func() {
		mw.Use(r, ex, noop)
		ex.finish(time.Millisecond)
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "L8: a request without a body adds no allocation"))
	}
}

func TestLoadShed_QueuedNewcomerYieldsToWaiter(t *testing.T) {
	c, _, waiter := queuedLoadShed(t, 4)
	c.inFlight.Add(-1)
	newcomer := startEnqueue(c)
	waiter.wait(t)
	if !waiter.admitted.Load() || waiter.refusal != nil {
		t.Fatal(test.DiffMessage([]any{waiter.admitted.Load(), waiter.refusal}, []any{true, nil}, "L2: a slot freed for a waiter goes to it, not to a newcomer"))
	}
	waitUntil(t, "the newcomer is counted as waiting", func() bool { return intOf(c.Snapshot(), "waiting") == 1 })
	time.Sleep(20 * time.Millisecond)
	if newcomer.returned() {
		t.Fatal(test.DiffMessage(true, false, "L2: the newcomer is still waiting after 20 ms"))
	}
	c.wake()
	if newcomer.returned() || newcomer.admitted.Load() {
		t.Error(test.DiffMessage(true, false, "L2: wake admits nobody more"))
	}
	waiter.ex.finish(time.Millisecond)
	newcomer.wait(t)
	if !newcomer.admitted.Load() || newcomer.refusal != nil {
		t.Error(test.DiffMessage([]any{newcomer.admitted.Load(), newcomer.refusal}, []any{true, nil}, "L2: the newcomer is admitted when the waiter finishes"))
	}
	newcomer.ex.finish(time.Millisecond)
}

func TestLoadShed_FreeSlotsGoToWaitersBeforeQueueFull(t *testing.T) {
	c, _, waiter := queuedLoadShed(t, 1)
	c.inFlight.Add(-1)
	newcomer := startEnqueue(c)
	waiter.wait(t)
	if !waiter.admitted.Load() || waiter.refusal != nil {
		t.Fatal(test.DiffMessage([]any{waiter.admitted.Load(), waiter.refusal}, []any{true, nil}, "L2: the free slot goes to the waiter"))
	}
	waitUntil(t, "the newcomer waits", func() bool { return intOf(c.Snapshot(), "waiting") == 1 })
	if newcomer.returned() {
		t.Fatal(test.DiffMessage(newcomer.refusal, nil, "L2: the newcomer finds the queue full only after the free slots went to the waiters before it"))
	}
	waiter.ex.finish(time.Millisecond)
	newcomer.wait(t)
	if !newcomer.admitted.Load() {
		t.Error(test.DiffMessage(false, true, "L2: the newcomer is admitted in its turn"))
	}
	newcomer.ex.finish(time.Millisecond)
}

func TestLoadShed_ArrivalAtFreedSlotMayTakeIt(t *testing.T) {
	withWaitTimeout(t, 10*time.Second)
	c, _, waiter := queuedLoadShed(t, 4)
	c.inFlight.Add(-1)
	newcomer := startCall(c, request(http.MethodGet, "/ok"))
	newcomer.wait(t)
	if !newcomer.admitted.Load() || newcomer.refusal != nil {
		t.Fatal(test.DiffMessage([]any{newcomer.admitted.Load(), newcomer.refusal}, []any{true, nil}, "L2: an arrival at the instant a slot frees may take it"))
	}
	c.wake()
	if waiter.returned() || waiter.admitted.Load() {
		t.Error(test.DiffMessage(true, false, "L2: wake admits nobody while the newcomer holds the slot"))
	}
	newcomer.ex.finish(time.Millisecond)
	waiter.wait(t)
	if !waiter.admitted.Load() || waiter.refusal != nil {
		t.Error(test.DiffMessage([]any{waiter.admitted.Load(), waiter.refusal}, []any{true, nil}, "L2: the waiter is admitted one finish later"))
	}
	waiter.ex.finish(time.Millisecond)
}
