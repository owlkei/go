package loadshed

import (
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/httphook"
	"go.owlky.com/owlky/internal/inspect"
	"go.owlky.com/owlky/internal/test"
)

const (
	rigProcs  = 4
	rigMemory = 64 << 20
	rigMin    = 4
	rigMax    = 64
	rigStart  = 32
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type rig struct {
	t     *testing.T
	mw    common.MiddlewareFn
	s     inspect.Snapshotter
	clock *fakeClock
	pool  []*call
}

func newRig(t *testing.T) *rig { return newRigProcs(t, rigProcs) }

func newRigProcs(t *testing.T, procs int) *rig {
	t.Helper()
	withMachine(t, procs, rigMemory)
	mw := LoadShed{MaxQueue: -1}.NewMiddleware()
	clock := &fakeClock{t: time.Now()}
	mw.(*compiledLoadShed).adaptive.now = clock.Now
	return &rig{t: t, mw: mw, s: snapshotter(t, mw), clock: clock}
}

func (g *rig) limit() int64 { return intOf(g.s.Snapshot(), "limit") }

func (g *rig) window() int { return 4 * int(g.limit()) }

func (g *rig) admit() bool {
	c, ok := tryAdmit(g.mw)
	if ok {
		g.pool = append(g.pool, c)
	}
	return ok
}

func (g *rig) finishOldest(elapsed time.Duration) {
	g.t.Helper()
	if len(g.pool) == 0 {
		g.t.Fatal("fixture: nothing in flight")
	}
	c := g.pool[0]
	g.pool = g.pool[1:]
	c.ex.finish(elapsed)
}

func (g *rig) fill(n int) {
	for len(g.pool) < n && g.admit() {
	}
}

func (g *rig) sequential(elapsed time.Duration, samples int) {
	g.t.Helper()
	for i := 0; i < samples; i++ {
		if !g.admit() {
			g.t.Fatal("fixture: a sequential request is refused")
		}
		g.finishOldest(elapsed)
	}
}

func (g *rig) saturated(elapsed time.Duration, windows int, keep func() int) {
	g.t.Helper()
	for i := 0; i < windows*g.window(); i++ {
		g.fill(keep())
		if len(g.pool) == 0 {
			g.t.Fatal("fixture: nothing to finish")
		}
		g.finishOldest(elapsed)
	}
}

func (g *rig) pressure() {
	g.t.Helper()
	g.fill(int(g.limit()))
	if g.admit() {
		g.t.Fatal(test.DiffMessage(true, false, "fixture: one request over the limit is refused"))
	}
}

func (g *rig) cycle(elapsed time.Duration, finished int) {
	g.t.Helper()
	for i := 0; i < finished; i++ {
		g.finishOldest(elapsed)
		if i < finished-1 {
			g.admit()
		}
	}
}

func (g *rig) pressureWindow(elapsed time.Duration) {
	g.t.Helper()
	size := g.window()
	g.pressure()
	g.cycle(elapsed, size)
}

func (g *rig) steady(keep int) func() int { return func() int { return keep } }

func (g *rig) atLimit() func() int { return func() int { return int(g.limit()) } }

func (g *rig) drain(elapsed time.Duration) {
	for len(g.pool) > 0 {
		g.finishOldest(elapsed)
	}
}

func cgroupMemoryLimited() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	for _, name := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(raw))
		if text == "max" {
			continue
		}
		if n, err := strconv.ParseInt(text, 10, 64); err == nil && n < 1<<60 {
			return true
		}
	}
	return false
}

func slowFirst(slow, base time.Duration) func(int) time.Duration {
	return func(i int) time.Duration {
		if i == 0 {
			return slow
		}
		return base
	}
}

func (g *rig) sequentialOf(elapsed func(int) time.Duration, samples int) {
	g.t.Helper()
	for i := 0; i < samples; i++ {
		if !g.admit() {
			g.t.Fatal("fixture: a sequential request is refused")
		}
		g.finishOldest(elapsed(i))
	}
}

func (g *rig) pressureWindowOf(elapsed func(int) time.Duration) {
	g.t.Helper()
	size := g.window()
	g.pressure()
	for i := 0; i < size; i++ {
		g.finishOldest(elapsed(i))
		if i < size-1 {
			g.admit()
		}
	}
}

func (g *rig) contended(elapsed func(int) time.Duration, samples int) {
	g.t.Helper()
	for i := 0; i < samples; i++ {
		g.fill(8)
		g.finishOldest(elapsed(i))
	}
}

func probesOf(g *rig) int64 { return intOf(g.s.Snapshot(), "probes") }

func floorRig(t *testing.T) *rig {
	t.Helper()
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	for i := 0; i < 200 && g.limit() < 40; i++ {
		g.pressureWindow(10 * time.Millisecond)
	}
	if g.limit() != 40 {
		t.Fatal(test.DiffMessage(g.limit(), int64(40), "fixture: the limit grew to 40"))
	}
	for i := 0; i < 200 && probesOf(g) == 0; i++ {
		g.pressureWindow(60 * time.Millisecond)
		if g.limit() < 20 {
			t.Fatal(test.DiffMessage(g.limit(), ">= 20", "L10: the limit never falls below half of the limit it earned"))
		}
	}
	if got := g.limit(); got != 20 {
		t.Fatal(test.DiffMessage(got, int64(20), "L10: the limit falls to the floor"))
	}
	if probesOf(g) != 1 || field(g.s.Snapshot(), "probing") != true {
		t.Fatal(test.DiffMessage([]any{probesOf(g), field(g.s.Snapshot(), "probing")}, []any{int64(1), true}, "L11: the close that holds the floor starts a probe"))
	}
	g.clock.Advance(1100 * time.Millisecond)
	g.admit()
	if got := field(g.s.Snapshot(), "probing"); got != false {
		t.Fatal(test.DiffMessage(got, false, "L11: a probe running for over 1 s ends at the next admission"))
	}
	return g
}

func benchAdmit(b *testing.B, c LoadShed) {
	b.Helper()
	mw := c.NewMiddleware()
	ex := newExchange(&discardWriter{header: http.Header{}})
	r := request(http.MethodGet, "/ok")
	b.ReportAllocs()
	for b.Loop() {
		mw.Use(r, ex, noop)
		ex.finish(time.Millisecond)
	}
}

type twoReads struct{ reads int }

func (b *twoReads) Read(p []byte) (int, error) {
	b.reads++
	if b.reads == 1 {
		return copy(p, "body"), nil
	}
	return 0, io.EOF
}

func (*twoReads) Close() error { return nil }

func bodyAdmission() (step func()) {
	mw := LoadShed{}.NewMiddleware()
	ex := newExchange(&discardWriter{header: http.Header{}})
	r := request(http.MethodPost, "/ok")
	body := &twoReads{}
	buf := make([]byte, 8)
	read := func() {
		_, _ = r.Body.Read(buf)
		_, _ = r.Body.Read(buf)
	}
	return func() {
		body.reads = 0
		r.Body = body
		mw.Use(r, ex, read)
		ex.finish(time.Millisecond)
	}
}

func noop() {}

type failBody struct{ t *testing.T }

func (b failBody) Read([]byte) (int, error) {
	b.t.Error("G3: the middleware read the request body")
	return 0, fmt.Errorf("body must not be read")
}

func (failBody) Close() error { return nil }

type discardWriter struct {
	header http.Header
	code   int
}

func (w *discardWriter) Header() http.Header { return w.header }

func (w *discardWriter) WriteHeader(code int) { w.code = code }

func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func request(method, target string, headers ...string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	return r
}

func panicText(fn func()) (text string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			text, panicked = fmt.Sprint(r), true
		}
	}()
	fn()
	return "", false
}

func field(s inspect.Snapshot, key string) any {
	for _, f := range s.Fields {
		if f.Key == key {
			return f.Value
		}
	}
	return nil
}

func benchLoad(b *testing.B, kind string, overload float64) {
	b.Helper()
	procs := runtime.GOMAXPROCS(0)
	var factor atomic.Int64
	factor.Store(1)
	w := newWorkload(kind, procs, &factor)
	for b.Loop() {
		mw := LoadShed{}.NewMiddleware()
		o := runLoad(b, mw, w.work, overload*w.capacity, 30*time.Second, overload < 1, 9)
		limits := slices.Clone(o.limits)
		slices.Sort(limits)
		if len(limits) > 0 {
			b.ReportMetric(float64(limits[0]), "limit-min")
			b.ReportMetric(float64(limits[len(limits)/2]), "limit-median")
			b.ReportMetric(float64(limits[len(limits)-1]), "limit-max")
		}
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		b.ReportMetric(float64(o.p99())/float64(time.Millisecond), "p99-ms")
		b.ReportMetric(float64(w.noLoad)/float64(time.Millisecond), "no-load-ms")
		b.ReportMetric(float64(stats.HeapInuse)/(1<<20), "heap-MiB")
		b.ReportMetric(float64(o.served), "status-2xx")
		b.ReportMetric(float64(o.refused), "status-503")
		b.ReportMetric(o.throughput()/w.capacity, "throughput/capacity")
	}
}

func (g *rig) holdOutside(n int) []*call {
	g.t.Helper()
	var held []*call
	for i := 0; i < n; i++ {
		c, ok := tryAdmit(g.mw)
		if !ok {
			g.t.Fatal(test.DiffMessage(false, true, "fixture: a request is admitted and held outside the pool"))
		}
		held = append(held, c)
	}
	return held
}

func (g *rig) admitOutside(desc string) *call {
	g.t.Helper()
	c, ok := tryAdmit(g.mw)
	if !ok {
		g.t.Fatal(test.DiffMessage(false, true, desc))
	}
	return c
}

func (g *rig) expectProbing(want bool, desc string) {
	g.t.Helper()
	if got := field(g.s.Snapshot(), "probing"); got != want {
		g.t.Fatal(test.DiffMessage(got, want, desc))
	}
}

func (g *rig) startProbeBeside(elapsed time.Duration) {
	g.t.Helper()
	g.pressure()
	for i := 0; i < 3*g.window() && field(g.s.Snapshot(), "probing") != true; i++ {
		g.finishOldest(elapsed)
		if field(g.s.Snapshot(), "probing") == true {
			break
		}
		g.admit()
		g.admit()
	}
	g.expectProbing(true, "fixture: a window under pressure with a stale baseline starts a probe")
}

func queuedLoadShed(t *testing.T, maxQueue int) (*compiledLoadShed, *call, *call) {
	t.Helper()
	mw := LoadShed{MaxInFlight: 1, MaxQueue: maxQueue}.NewMiddleware()
	s := snapshotter(t, mw)
	first := admitNow(t, mw)
	waiter := startCall(mw, request(http.MethodGet, "/ok"))
	waitUntil(t, "the waiter is queued", func() bool { return intOf(s.Snapshot(), "waiting") == 1 })
	return mw.(*compiledLoadShed), first, waiter
}

func withWaitTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := waitTimeout
	waitTimeout = d
	t.Cleanup(func() { waitTimeout = old })
}

func startEnqueue(c *compiledLoadShed) *call {
	n := &call{rec: httptest.NewRecorder(), done: make(chan struct{})}
	n.ex = newExchange(n.rec)
	go func() {
		defer close(n.done)
		defer func() { n.refusal = recover() }()
		r := request(http.MethodGet, "/ok")
		c.enqueue(r, n.ex, n.ex, func() { n.admitted.Store(true) })
	}()
	return n
}

const (
	stabilityWarmup  = 10 * time.Second
	stabilityMeasure = 10 * time.Second
	ioPool           = 50
	ioLatency        = 20 * time.Millisecond
	cpuLatency       = 5 * time.Millisecond
)

var spinSink atomic.Uint64

func spin(iterations int) {
	x := uint64(1)
	for i := 0; i < iterations; i++ {
		x = x*6364136223846793005 + 1442695040888963407
	}
	spinSink.Add(x)
}

func calibrateSpin(target time.Duration) int {
	n := 1 << 18
	for {
		start := time.Now()
		spin(n)
		if d := time.Since(start); d > 20*time.Millisecond {
			return int(float64(n) * float64(target) / float64(d))
		}
		n *= 2
	}
}

type workload struct {
	work      func()
	capacity  float64
	workers   int
	noLoad    time.Duration
	tolerance float64
	p99Factor float64
}

const cpuSteps = 5

var directRequest = request(http.MethodGet, "/work")

func cpuWork(iterations int, factor *atomic.Int64) func() {
	return func() {
		step := iterations * int(factor.Load()) / cpuSteps
		for i := 0; i < cpuSteps; i++ {
			spin(step)
			runtime.Gosched()
		}
	}
}

func cpuWorkNoYield(iterations int, factor *atomic.Int64) func() {
	return func() { spin(iterations * int(factor.Load())) }
}

func ioWork() func() {
	pool := make(chan struct{}, ioPool)
	return func() {
		pool <- struct{}{}
		time.Sleep(ioLatency)
		<-pool
	}
}

func measureCapacity(work func(), workers int, d time.Duration) float64 {
	var done atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					work()
					done.Add(1)
				}
			}
		}()
	}
	start := time.Now()
	time.Sleep(d)
	close(stop)
	wg.Wait()
	return float64(done.Load()) / time.Since(start).Seconds()
}

func measureNoLoad(work func()) time.Duration {
	samples := make([]time.Duration, 0, 40)
	for i := 0; i < 40; i++ {
		start := time.Now()
		work()
		samples = append(samples, time.Since(start))
	}
	slices.Sort(samples)
	return samples[len(samples)/2]
}

func newWorkload(kind string, procs int, factor *atomic.Int64) workload {
	if kind == "io" {
		work := ioWork()
		return workload{work: work, workers: 3 * ioPool, capacity: measureCapacity(work, 3*ioPool, 2*time.Second), noLoad: measureNoLoad(work), tolerance: 0.20, p99Factor: 3}
	}
	iterations := calibrateSpin(cpuLatency)
	work := cpuWork(iterations, factor)
	if kind == "cpu-noyield" {
		work = cpuWorkNoYield(iterations, factor)
	}
	return workload{work: work, workers: 4 * procs, capacity: measureCapacity(work, 4*procs, 2*time.Second), noLoad: measureNoLoad(work), tolerance: 0.35, p99Factor: 12}
}

type outcome struct {
	served    int64
	refused   int64
	latencies []time.Duration
	limits    []int64
	seconds   float64
}

func (o outcome) throughput() float64 { return float64(o.served) / o.seconds }

func (o outcome) p99() time.Duration {
	if len(o.latencies) == 0 {
		return 0
	}
	sorted := slices.Clone(o.latencies)
	slices.Sort(sorted)
	return sorted[min(len(sorted)-1, len(sorted)*99/100)]
}

func (o outcome) median() float64 {
	sorted := slices.Clone(o.limits)
	slices.Sort(sorted)
	if len(sorted) == 0 {
		return 0
	}
	return float64(sorted[len(sorted)/2])
}

func serveDirect(mw common.MiddlewareFn, work func(), deadline time.Time, served, refused *atomic.Int64, record func(time.Duration)) {
	start := time.Now()
	ex := newExchange(&discardWriter{header: http.Header{}})
	admitted := false
	func() {
		defer func() { _ = recover() }()
		mw.Use(directRequest, ex, func() { admitted = true })
	}()
	if !admitted {
		refused.Add(1)
		return
	}
	workStart := time.Now()
	work()
	record(time.Since(workStart))
	ex.finish(time.Since(start))
	if time.Now().Before(deadline) {
		served.Add(1)
	}
}

func runLoad(t testing.TB, mw common.MiddlewareFn, work func(), rate float64, d time.Duration, poisson bool, seed uint64) outcome {
	t.Helper()
	s := snapshotter(t, mw)
	var served, refused atomic.Int64
	var mu sync.Mutex
	var latencies []time.Duration
	var limits []int64
	record := func(l time.Duration) {
		mu.Lock()
		latencies = append(latencies, l)
		mu.Unlock()
	}

	stopSampling := make(chan struct{})
	var sampler sync.WaitGroup
	sampler.Add(1)
	go func() {
		defer sampler.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSampling:
				return
			case <-ticker.C:
				snap := s.Snapshot()
				if field(snap, "probing") == true {
					continue
				}
				mu.Lock()
				limits = append(limits, intOf(snap, "limit"))
				mu.Unlock()
			}
		}
	}()

	var wg sync.WaitGroup
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	start := time.Now()
	deadline := start.Add(d)
	due := 0.0
	for {
		elapsed := time.Since(start)
		if elapsed >= d {
			break
		}
		for due <= elapsed.Seconds() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				serveDirect(mw, work, deadline, &served, &refused, record)
			}()
			gap := 1 / rate
			if poisson {
				gap = rng.ExpFloat64() / rate
			}
			due += gap
		}
		time.Sleep(time.Millisecond)
	}
	seconds := d.Seconds()
	wg.Wait()
	close(stopSampling)
	sampler.Wait()
	return outcome{served: served.Load(), refused: refused.Load(), latencies: latencies, limits: limits, seconds: seconds}
}

func expectStable(t checker, w workload, o outcome, label string) {
	t.Helper()
	if len(o.limits) < 20 {
		t.Fatal(test.DiffMessage(len(o.limits), "> 20", "fixture: the limit was sampled outside probes"))
	}
	median := o.median()
	low, high := median*(1-w.tolerance), median*(1+w.tolerance)
	for _, l := range o.limits {
		if float64(l) < low-0.5 || float64(l) > high+0.5 {
			t.Errorf("L12: %s: the limit %d left %.0f%% around its median %.0f", label, l, w.tolerance*100, median)
			break
		}
	}
	if got, want := o.throughput(), 0.95*w.capacity; got < want {
		t.Error(test.DiffMessage(got, ">= "+formatRate(want), "L12: "+label+": throughput at least 95% of the capacity measured without loadshed"))
	}
	if got, want := o.p99(), time.Duration(float64(w.noLoad)*w.p99Factor); got > want {
		t.Error(test.DiffMessage(got, "<= "+want.String(), "L12: "+label+": p99 latency of admitted requests"))
	}
	if o.refused == 0 {
		t.Error(test.DiffMessage(o.refused, "> 0", "L12: "+label+": at 5 x capacity the extra requests are refused"))
	}
}

// checker is the part of *testing.T the acceptance checks use, so a failed attempt can be recorded
// and retried instead of failing the test.
type checker interface {
	Helper()
	Error(args ...any)
	Errorf(format string, args ...any)
	Fatal(args ...any)
}

// acceptanceAttempts is how many times a timing acceptance run (L12) is measured before it fails.
// The runs measure real throughput: another process on the machine (a browser, a second test run)
// can push one measurement under its floor, while a real regression fails every attempt.
const acceptanceAttempts = 3

type attempt struct{ msgs []string }

type attemptAbort struct{}

func (a *attempt) Helper()           {}
func (a *attempt) Error(args ...any) { a.msgs = append(a.msgs, fmt.Sprint(args...)) }
func (a *attempt) Errorf(format string, args ...any) {
	a.msgs = append(a.msgs, fmt.Sprintf(format, args...))
}
func (a *attempt) Fatal(args ...any) {
	a.Error(args...)
	panic(attemptAbort{})
}

// acceptance runs check up to acceptanceAttempts times and fails t with the last attempt's
// messages only when every attempt failed.
func acceptance(t *testing.T, check func(c checker)) {
	t.Helper()
	var last *attempt
	for i := 1; i <= acceptanceAttempts; i++ {
		last = &attempt{}
		func() {
			defer func() {
				if r := recover(); r != nil {
					if _, ok := r.(attemptAbort); !ok {
						panic(r)
					}
				}
			}()
			check(last)
		}()
		if len(last.msgs) == 0 {
			return
		}
		t.Logf("attempt %d of %d failed: %s", i, acceptanceAttempts, strings.Join(last.msgs, "; "))
	}
	for _, msg := range last.msgs {
		t.Error(msg)
	}
}

func formatRate(v float64) string { return fmt.Sprintf("%.0f requests per second", v) }

func stableRun(t *testing.T, procs int, kind string) {
	t.Helper()
	if testing.Short() || raceEnabled {
		t.Skip("L12 runs about 25 s")
	}
	withMachine(t, procs, math.MaxInt64)
	acceptance(t, func(c checker) {
		var factor atomic.Int64
		factor.Store(1)
		w := newWorkload(kind, procs, &factor)
		mw := LoadShed{}.NewMiddleware()
		rate := 5 * w.capacity
		runLoad(t, mw, w.work, rate, stabilityWarmup, false, 1)
		o := runLoad(t, mw, w.work, rate, stabilityMeasure, false, 2)
		w.capacity = min(w.capacity, measureCapacity(w.work, w.workers, 2*time.Second))
		expectStable(c, w, o, kind+" GOMAXPROCS "+strconv.Itoa(procs))
	})
}

func belowCapacityRun(t *testing.T, procs int) {
	t.Helper()
	if testing.Short() || raceEnabled {
		t.Skip("L12 runs about 35 s")
	}
	withMachine(t, procs, math.MaxInt64)
	for _, kind := range []string{"cpu", "io"} {
		acceptance(t, func(c checker) {
			var factor atomic.Int64
			factor.Store(1)
			w := newWorkload(kind, procs, &factor)
			mw := LoadShed{}.NewMiddleware()
			rate := 0.7 * w.capacity
			runLoad(t, mw, w.work, rate, stabilityWarmup/2, true, 3)
			o := runLoad(t, mw, w.work, rate, stabilityMeasure, true, 4)
			if o.refused != 0 {
				c.Error(test.DiffMessage(o.refused, int64(0), "L12: "+kind+" GOMAXPROCS "+strconv.Itoa(procs)+": at 70% of capacity no request is refused"))
			}
			if o.served == 0 {
				c.Error(test.DiffMessage(o.served, "> 0", "L12: the requests are served"))
			}
		})
	}
}

func recoversRun(t *testing.T, procs int) {
	t.Helper()
	if testing.Short() || raceEnabled {
		t.Skip("L12 runs about 35 s")
	}
	withMachine(t, procs, math.MaxInt64)
	acceptance(t, func(c checker) { recoversAttempt(t, c, procs) })
}

func recoversAttempt(t *testing.T, c checker, procs int) {
	var factor atomic.Int64
	factor.Store(1)
	fast := newWorkload("cpu", procs, &factor)
	factor.Store(3)
	slow := measureCapacity(fast.work, 4*procs, 2*time.Second)
	factor.Store(1)

	mw := LoadShed{}.NewMiddleware()
	rate := 5 * fast.capacity
	runLoad(t, mw, fast.work, rate, stabilityWarmup, false, 5)
	factor.Store(3)
	runLoad(t, mw, fast.work, rate, stabilityWarmup, false, 6)
	o := runLoad(t, mw, fast.work, rate, stabilityMeasure, false, 7)
	slow = min(slow, measureCapacity(fast.work, fast.workers, 2*time.Second))
	factor.Store(1)
	if got, want := o.throughput(), 0.95*slow; got < want {
		c.Error(test.DiffMessage(got, ">= "+formatRate(want), "L12: a handler that became 3 x slower keeps its throughput once the baseline is relearned"))
	}
	if o.served == 0 {
		c.Error(test.DiffMessage(o.served, "> 0", "L12: the slow handler is still served"))
	}
}

const (
	steerLength   = 40 * time.Second
	steerAttackAt = 6 * time.Second
	steerAttackTo = 30 * time.Second
	steerBurst    = 50 * time.Millisecond
	steerHold     = time.Second
)

type timedLimit struct {
	at    time.Duration
	limit int64
}

type steering struct {
	capacity float64
	limits   []timedLimit
	served   []int64
}

func (s steering) limitsBetween(from, to time.Duration) []int64 {
	var out []int64
	for _, l := range s.limits {
		if l.at >= from && l.at < to {
			out = append(out, l.limit)
		}
	}
	return out
}

func (s steering) servedShare() float64 {
	var total int64
	for _, n := range s.served[12:30] {
		total += n
	}
	return float64(total) / (s.capacity * 18)
}

type slowBody struct {
	delay time.Duration
	read  bool
}

func (b *slowBody) Read([]byte) (int, error) {
	if !b.read {
		b.read = true
		time.Sleep(b.delay)
	}
	return 0, io.EOF
}

func (*slowBody) Close() error { return nil }

func throughMiddleware(mw common.MiddlewareFn, r *http.Request, handler func(*http.Request)) bool {
	start := time.Now()
	ex := newExchange(&discardWriter{header: http.Header{}})
	admitted := false
	func() {
		defer func() { _ = recover() }()
		mw.Use(r, ex, func() { admitted = true })
	}()
	if !admitted {
		return false
	}
	handler(r)
	ex.finish(time.Since(start))
	return true
}

func spawnClients(wg *sync.WaitGroup, start time.Time, clients int, step func(r *http.Request)) {
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := request(http.MethodPost, "/attack")
			time.Sleep(time.Until(start.Add(steerAttackAt)))
			for time.Since(start) < steerAttackTo {
				step(r)
			}
		}()
	}
}

type launcher func(mw common.MiddlewareFn, w workload, start time.Time, wg *sync.WaitGroup)

func freeBursts(mw common.MiddlewareFn, _ workload, start time.Time, wg *sync.WaitGroup) {
	spawnClients(wg, start, 64, func(r *http.Request) {
		phase := time.Since(start) % time.Second
		if phase >= steerBurst {
			time.Sleep(time.Second - phase)
			return
		}
		if !throughMiddleware(mw, r, func(*http.Request) {}) {
			runtime.Gosched()
		}
	})
}

func slowUploads(mw common.MiddlewareFn, _ workload, start time.Time, wg *sync.WaitGroup) {
	spawnClients(wg, start, 40, func(r *http.Request) {
		r.Body = &slowBody{delay: steerHold}
		if !throughMiddleware(mw, r, func(r *http.Request) { _, _ = io.Copy(io.Discard, r.Body) }) {
			time.Sleep(time.Millisecond)
		}
	})
}

func slowRouteOf(hold func(workload) time.Duration) launcher {
	return func(mw common.MiddlewareFn, w workload, start time.Time, wg *sync.WaitGroup) {
		spawnClients(wg, start, 40, func(r *http.Request) {
			if !throughMiddleware(mw, r, func(*http.Request) { time.Sleep(hold(w)) }) {
				time.Sleep(time.Millisecond)
			}
		})
	}
}

var (
	slowRoute      = slowRouteOf(func(workload) time.Duration { return steerHold })
	slowRouteTuned = slowRouteOf(func(w workload) time.Duration { return 16 * w.noLoad })
)

func runSteering(t testing.TB, attack launcher) steering {
	t.Helper()
	var factor atomic.Int64
	factor.Store(1)
	w := newWorkload("io", runtime.GOMAXPROCS(0), &factor)
	mw := LoadShed{}.NewMiddleware()
	snap := snapshotter(t, mw)
	served := make([]atomic.Int64, int(steerLength/time.Second))
	start := time.Now()

	var mu sync.Mutex
	var limits []timedLimit
	stopSampling := make(chan struct{})
	var sampler sync.WaitGroup
	sampler.Add(1)
	go func() {
		defer sampler.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSampling:
				return
			case <-ticker.C:
				limit := intOf(snap.Snapshot(), "limit")
				mu.Lock()
				limits = append(limits, timedLimit{at: time.Since(start), limit: limit})
				mu.Unlock()
			}
		}
	}()

	var attackers sync.WaitGroup
	attack(mw, w, start, &attackers)

	var legitimate sync.WaitGroup
	rate := 1.2 * w.capacity
	due := 0.0
	for {
		elapsed := time.Since(start)
		if elapsed >= steerLength {
			break
		}
		for due <= elapsed.Seconds() {
			legitimate.Add(1)
			go func() {
				defer legitimate.Done()
				if throughMiddleware(mw, directRequest, func(*http.Request) { w.work() }) {
					if second := int(time.Since(start) / time.Second); second < len(served) {
						served[second].Add(1)
					}
				}
			}()
			due += 1 / rate
		}
		time.Sleep(time.Millisecond)
	}
	attackers.Wait()
	legitimate.Wait()
	close(stopSampling)
	sampler.Wait()

	result := steering{capacity: w.capacity, limits: limits, served: make([]int64, len(served))}
	for i := range served {
		result.served[i] = served[i].Load()
	}
	return result
}

func expectSteering(t *testing.T, s steering, label string, floorShare float64, wantServed, wantRecovery bool) {
	t.Helper()
	before := outcome{limits: s.limitsBetween(3*time.Second, steerAttackAt)}.median()
	if before == 0 {
		t.Fatal(test.DiffMessage(before, "> 0", "fixture: the limit was sampled between seconds 3 and 6"))
	}
	for _, l := range s.limitsBetween(steerAttackAt, steerAttackTo) {
		if float64(l) < floorShare*before {
			t.Error(test.DiffMessage(l, fmt.Sprintf(">= %.1f", floorShare*before), "L13: "+label+": the limit during the attack against "+strconv.FormatFloat(before, 'f', 0, 64)+" before"))
			break
		}
	}
	if wantServed {
		if got := s.servedShare(); got < 0.9 {
			t.Error(test.DiffMessage(got, ">= 0.9", "L13: "+label+": legitimate requests served between seconds 12 and 30 over the capacity"))
		}
	}
	if wantRecovery {
		after := outcome{limits: s.limitsBetween(steerLength-time.Second, steerLength)}.median()
		if after < 0.6*before {
			t.Error(test.DiffMessage(after, fmt.Sprintf(">= %.1f", 0.6*before), "L13: "+label+": the limit 10 s after the attack"))
		}
	}
}

func steeringRun(t *testing.T, attack launcher, label string, floorShare float64, wantServed, wantRecovery bool) {
	t.Helper()
	if testing.Short() || raceEnabled {
		t.Skip("L13 runs about 45 s")
	}
	withMachine(t, 2, math.MaxInt64)
	expectSteering(t, runSteering(t, attack), label, floorShare, wantServed, wantRecovery)
}

const (
	heldLength   = 45 * time.Second
	heldWarmup   = 10 * time.Second
	heldStart    = 2 * time.Second
	heldBucket   = 50 * time.Millisecond
	heldGap      = 500 * time.Millisecond
	heldInterval = 10 * time.Millisecond
)

type heldSnapshot struct {
	at       time.Duration
	probing  bool
	limit    int64
	baseline time.Duration
}

type heldRun struct {
	buckets   []int64
	snapshots []heldSnapshot
	latencies []time.Duration
	served    int64
}

func holdRequests(t *testing.T, mw common.MiddlewareFn, k int) []*exchange {
	t.Helper()
	held := make([]*exchange, 0, k)
	for i := 0; i < k; i++ {
		ex := newExchange(&discardWriter{header: http.Header{}})
		admitted := false
		func() {
			defer func() { _ = recover() }()
			mw.Use(directRequest, ex, func() { admitted = true })
		}()
		if !admitted {
			t.Fatal(test.DiffMessage(false, true, "fixture: a long-held request is admitted before the load"))
		}
		held = append(held, ex)
	}
	return held
}

func runHeld(t testing.TB, mw common.MiddlewareFn, w workload) heldRun {
	t.Helper()
	s := snapshotter(t, mw)
	counts := make([]atomic.Int64, int(heldLength/heldBucket))
	var served atomic.Int64
	var mu sync.Mutex
	var snapshots []heldSnapshot
	var latencies []time.Duration
	start := time.Now()

	stopSampling := make(chan struct{})
	var sampler sync.WaitGroup
	sampler.Add(1)
	go func() {
		defer sampler.Done()
		ticker := time.NewTicker(heldInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopSampling:
				return
			case <-ticker.C:
				snap := s.Snapshot()
				at := time.Since(start)
				mu.Lock()
				snapshots = append(snapshots, heldSnapshot{at: at, probing: field(snap, "probing") == true, limit: intOf(snap, "limit"), baseline: durationOf(snap, "baseline")})
				mu.Unlock()
			}
		}
	}()

	var wg sync.WaitGroup
	rate := 5 * w.capacity
	due := 0.0
	for {
		elapsed := time.Since(start)
		if elapsed >= heldLength {
			break
		}
		for due <= elapsed.Seconds() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var work time.Duration
				if !throughMiddleware(mw, directRequest, func(*http.Request) {
					begin := time.Now()
					w.work()
					work = time.Since(begin)
				}) {
					return
				}
				at := time.Since(start)
				if bucket := int(at / heldBucket); bucket < len(counts) {
					counts[bucket].Add(1)
				}
				if at >= heldWarmup && at < heldLength {
					served.Add(1)
					mu.Lock()
					latencies = append(latencies, work)
					mu.Unlock()
				}
			}()
			due += 1 / rate
		}
		time.Sleep(time.Millisecond)
	}
	wg.Wait()
	close(stopSampling)
	sampler.Wait()

	run := heldRun{snapshots: snapshots, latencies: latencies, served: served.Load()}
	for i := range counts {
		run.buckets = append(run.buckets, counts[i].Load())
	}
	return run
}

func (r heldRun) longestProbe() time.Duration {
	var longest time.Duration
	var begin time.Duration
	probing := false
	for _, s := range r.snapshots {
		switch {
		case s.probing && !probing:
			begin, probing = s.at, true
		case !s.probing && probing:
			probing = false
		}
		if probing {
			longest = max(longest, s.at-begin+heldInterval)
		}
	}
	return longest
}

func (r heldRun) longestGap() time.Duration {
	var longest, current int
	for i := int(heldStart / heldBucket); i < len(r.buckets); i++ {
		if r.buckets[i] == 0 {
			current++
			longest = max(longest, current)
		} else {
			current = 0
		}
	}
	return time.Duration(longest) * heldBucket
}

func (r heldRun) limitsOutsideProbes() []int64 {
	var out []int64
	for _, s := range r.snapshots {
		if !s.probing && s.at >= heldWarmup {
			out = append(out, s.limit)
		}
	}
	return out
}

func longHeldRun(t *testing.T, procs int, kind string, k int) {
	t.Helper()
	if testing.Short() || raceEnabled {
		t.Skip("L14 runs about 50 s")
	}
	withMachine(t, procs, math.MaxInt64)
	var factor atomic.Int64
	factor.Store(1)
	w := newWorkload(kind, procs, &factor)
	mw := LoadShed{}.NewMiddleware()
	s := snapshotter(t, mw)
	firstLimit := intOf(s.Snapshot(), "limit")
	held := holdRequests(t, mw, k)
	run := runHeld(t, mw, w)
	w.capacity = min(w.capacity, measureCapacity(w.work, w.workers, 2*time.Second))
	for _, ex := range held {
		ex.finish(heldLength)
	}
	label := kind + " GOMAXPROCS " + strconv.Itoa(procs) + " held " + strconv.Itoa(k)

	if got := run.longestProbe(); got >= heldGap {
		t.Error(test.DiffMessage(got, "< "+heldGap.String(), "L14 (a): "+label+": the longest probe"))
	}
	if got := run.longestGap(); got >= heldGap {
		t.Error(test.DiffMessage(got, "< "+heldGap.String(), "L14 (b): "+label+": the longest time without a served request after second 2"))
	}
	last := run.snapshots[len(run.snapshots)-1]
	if limit := 3 * w.noLoad; last.baseline <= 0 || last.baseline > limit {
		t.Error(test.DiffMessage(last.baseline, "0 < baseline <= "+limit.String(), "L14 (c): "+label+": the baseline at the end"))
	}
	window := (heldLength - heldWarmup).Seconds()
	if got, want := float64(run.served)/window, 0.85*w.capacity; got < want {
		t.Error(test.DiffMessage(got, ">= "+formatRate(want), "L14 (d): "+label+": throughput between seconds 10 and 45"))
	}
	if kind == "io" {
		median := outcome{limits: run.limitsOutsideProbes()}.median()
		if want := 4 * float64(firstLimit); median < want {
			t.Error(test.DiffMessage(median, fmt.Sprintf(">= %.0f", want), "L14 (e): "+label+": the median limit after second 10 outside probes"))
		}
	} else {
		got, want := outcome{latencies: run.latencies}.p99(), time.Duration(float64(w.noLoad)*w.p99Factor)
		if got > want {
			t.Error(test.DiffMessage(got, "<= "+want.String(), "L14 (e): "+label+": p99 latency of admitted requests"))
		}
	}
	if got := intOf(s.Snapshot(), "in_flight"); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "L14: "+label+": every slot is free after the held requests are released"))
	}
}

type exchange struct {
	inner http.ResponseWriter
	hooks []httphook.Hook
}

func newExchange(w http.ResponseWriter) *exchange { return &exchange{inner: w} }

func (e *exchange) Header() http.Header { return e.inner.Header() }

func (e *exchange) Write(p []byte) (int, error) { return e.inner.Write(p) }

func (e *exchange) WriteHeader(code int) { e.inner.WriteHeader(code) }

func (e *exchange) Inner() http.ResponseWriter { return e.inner }

func (e *exchange) SetInner(w http.ResponseWriter) { e.inner = w }

func (e *exchange) OnDone(h httphook.Hook) { e.hooks = append(e.hooks, h) }

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
	return e.Code(), e.Message(), true
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

func describe(c LoadShed) string {
	return "MaxInFlight " + strconv.Itoa(c.MaxInFlight) + ", MaxQueue " + strconv.Itoa(c.MaxQueue)
}

type scriptedStep struct {
	data string
	err  error
}

type scriptedBody struct {
	steps    []scriptedStep
	delay    time.Duration
	closeErr error
	closed   int
}

func (b *scriptedBody) Read(p []byte) (int, error) {
	if b.delay > 0 {
		time.Sleep(b.delay)
	}
	if len(b.steps) == 0 {
		return 0, io.EOF
	}
	step := b.steps[0]
	b.steps = b.steps[1:]
	return copy(p, step.data), step.err
}

func (b *scriptedBody) Close() error {
	b.closed++
	return b.closeErr
}

func (g *rig) admitRequest(r *http.Request) bool {
	c := &call{rec: httptest.NewRecorder(), done: make(chan struct{})}
	c.ex = newExchange(c.rec)
	func() {
		defer func() { c.refusal = recover() }()
		g.mw.Use(r, c.ex, func() { c.admitted.Store(true) })
	}()
	if c.admitted.Load() {
		g.pool = append(g.pool, c)
	}
	return c.admitted.Load()
}
