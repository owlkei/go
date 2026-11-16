package loadshed

import (
	"fmt"
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
