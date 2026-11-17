package loadshed

import (
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/internal/test"
)

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

func expectStable(t *testing.T, w workload, o outcome, label string) {
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

func formatRate(v float64) string { return fmt.Sprintf("%.0f requests per second", v) }

func stableRun(t *testing.T, procs int, kind string) {
	t.Helper()
	if testing.Short() {
		t.Skip("L12 runs about 25 s")
	}
	withMachine(t, procs, math.MaxInt64)
	var factor atomic.Int64
	factor.Store(1)
	w := newWorkload(kind, procs, &factor)
	mw := LoadShed{}.NewMiddleware()
	rate := 5 * w.capacity
	runLoad(t, mw, w.work, rate, stabilityWarmup, false, 1)
	o := runLoad(t, mw, w.work, rate, stabilityMeasure, false, 2)
	w.capacity = min(w.capacity, measureCapacity(w.work, w.workers, 2*time.Second))
	expectStable(t, w, o, kind+" GOMAXPROCS "+strconv.Itoa(procs))
}

func TestAdaptive_Stable_CPUBound_1CPU(t *testing.T) { stableRun(t, 1, "cpu") }
func TestAdaptive_Stable_CPUBound_2CPU(t *testing.T) { stableRun(t, 2, "cpu") }
func TestAdaptive_Stable_IOBound_1CPU(t *testing.T)  { stableRun(t, 1, "io") }
func TestAdaptive_Stable_IOBound_2CPU(t *testing.T)  { stableRun(t, 2, "io") }

func belowCapacityRun(t *testing.T, procs int) {
	t.Helper()
	if testing.Short() {
		t.Skip("L12 runs about 35 s")
	}
	withMachine(t, procs, math.MaxInt64)
	for _, kind := range []string{"cpu", "io"} {
		var factor atomic.Int64
		factor.Store(1)
		w := newWorkload(kind, procs, &factor)
		mw := LoadShed{}.NewMiddleware()
		rate := 0.7 * w.capacity
		runLoad(t, mw, w.work, rate, stabilityWarmup/2, true, 3)
		o := runLoad(t, mw, w.work, rate, stabilityMeasure, true, 4)
		if o.refused != 0 {
			t.Error(test.DiffMessage(o.refused, int64(0), "L12: "+kind+" GOMAXPROCS "+strconv.Itoa(procs)+": at 70% of capacity no request is refused"))
		}
		if o.served == 0 {
			t.Error(test.DiffMessage(o.served, "> 0", "L12: the requests are served"))
		}
	}
}

func TestAdaptive_NoRefusalBelowCapacity_1CPU(t *testing.T) { belowCapacityRun(t, 1) }
func TestAdaptive_NoRefusalBelowCapacity_2CPU(t *testing.T) { belowCapacityRun(t, 2) }

func recoversRun(t *testing.T, procs int) {
	t.Helper()
	if testing.Short() {
		t.Skip("L12 runs about 35 s")
	}
	withMachine(t, procs, math.MaxInt64)
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
		t.Error(test.DiffMessage(got, ">= "+formatRate(want), "L12: a handler that became 3 x slower keeps its throughput once the baseline is relearned"))
	}
	if o.served == 0 {
		t.Error(test.DiffMessage(o.served, "> 0", "L12: the slow handler is still served"))
	}
}

func TestAdaptive_RecoversWhenHandlerSlows_1CPU(t *testing.T) { recoversRun(t, 1) }
func TestAdaptive_RecoversWhenHandlerSlows_2CPU(t *testing.T) { recoversRun(t, 2) }

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
	if testing.Short() {
		t.Skip("L13 runs about 45 s")
	}
	withMachine(t, 2, math.MaxInt64)
	expectSteering(t, runSteering(t, attack), label, floorShare, wantServed, wantRecovery)
}

func TestAdaptive_Steering_FreeBursts(t *testing.T) {
	steeringRun(t, freeBursts, "free bursts", 0.5, true, true)
}

func TestAdaptive_Steering_SlowUploads(t *testing.T) {
	steeringRun(t, slowUploads, "slow uploads", 0.5, true, true)
}

func TestAdaptive_Steering_SlowRoute(t *testing.T) {
	steeringRun(t, slowRoute, "slow route", 0.45, false, false)
}

func BenchmarkLoadShed_L13_SlowRouteTuned(b *testing.B) {
	for b.Loop() {
		s := runSteering(b, slowRouteTuned)
		limits := s.limitsBetween(steerAttackAt, steerAttackTo)
		slices.Sort(limits)
		if len(limits) > 0 {
			b.ReportMetric(float64(limits[0]), "limit-min")
			b.ReportMetric(float64(limits[len(limits)/2]), "limit-median")
			b.ReportMetric(float64(limits[len(limits)-1]), "limit-max")
		}
		b.ReportMetric(s.servedShare(), "served/capacity")
	}
}
