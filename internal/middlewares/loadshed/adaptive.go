package loadshed

import (
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.owlky.com/owlky/internal/inspect"
)

const (
	windowPerLimit   = 4
	quietSamples     = 8
	probeSamples     = 8
	tolerance        = 1.5
	smoothing        = 0.8
	longSample       = 30 * time.Second
	baselineMaxAge   = 30 * time.Second
	probeMaxDuration = time.Second
	probeSpacing     = 30 * time.Second
	bytesPerSlot     = 4 << 20
	lowestMax        = 64
	highestMax       = 4096
	slotsPerProc     = 256
	startPerProc     = 8
	cgroupV2Memory   = "/sys/fs/cgroup/memory.max"
	cgroupV1Memory   = "/sys/fs/cgroup/memory/memory.limit_in_bytes"
	unlimitedMemory  = 1 << 60
	countShift       = 48
	oneSample        = 1 << countShift
	sumMask          = oneSample - 1
	sumShift         = 6
	sampleCap        = 32
)

type adaptive struct {
	min, max int64
	now      func() time.Time

	limit      atomic.Int64
	samples    atomic.Uint64
	quiet      atomic.Uint64
	isPressure atomic.Bool

	probes     atomic.Int64
	probeStart atomic.Int64
	current    atomic.Pointer[probe]

	baseline atomic.Int64
	average  atomic.Int64

	mu          sync.Mutex
	limitValue  float64
	baselineNs  float64
	learned     float64
	earned      int64
	hasBaseline bool
	isFloorHeld bool
	baselineAt  time.Time
	probedAt    time.Time
	hasProbed   bool
	isProbeSeen bool
}

type bounds struct{ min, max, start int64 }

func machineBounds() bounds {
	procs := int64(runtime.GOMAXPROCS(0))
	maxSlots := slotsPerProc * procs
	if memory, ok := memoryLimit(); ok {
		maxSlots = memory / bytesPerSlot
	}
	b := bounds{max: min(max(maxSlots, lowestMax), highestMax)}
	b.min = min(procs, b.max)
	b.start = min(max(startPerProc*procs, b.min), b.max)
	return b
}

func memoryLimit() (int64, bool) {
	smallest, ok := int64(math.MaxInt64), false
	if limit := debug.SetMemoryLimit(-1); limit > 0 && limit < math.MaxInt64 {
		smallest, ok = limit, true
	}
	if runtime.GOOS != "linux" {
		return smallest, ok
	}
	for _, name := range []string{cgroupV2Memory, cgroupV1Memory} {
		if limit, found := readCgroupLimit(name); found && limit < smallest {
			smallest, ok = limit, true
		}
	}
	return smallest, ok
}

func readCgroupLimit(name string) (int64, bool) {
	raw, err := os.ReadFile(name)
	if err != nil {
		return 0, false
	}
	limit, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || limit <= 0 || limit >= unlimitedMemory {
		return 0, false
	}
	return limit, true
}

func (a *adaptive) init(b bounds) {
	a.min, a.max = b.min, b.max
	a.now = time.Now
	a.limitValue = float64(b.start)
	a.limit.Store(b.start)
}

func (c *compiledLoadShed) probeAdmitLimit(v int64) int64 {
	a := &c.adaptive
	if a.now().UnixNano()-a.probeStart.Load() <= int64(probeMaxDuration) || !a.mu.TryLock() {
		return v
	}
	if c.admit.Load() < 0 {
		a.endProbeLocked(c)
	}
	a.mu.Unlock()
	// The slots the probe kept closed are free now: they go to the waiters first (L2), as after
	// a probe that ends on its 8th sample.
	if c.waiting.Load() > 0 {
		c.wake()
	}
	return c.admit.Load()
}

func (a *adaptive) sample(c *compiledLoadShed, elapsed time.Duration, inFlight int64, p *probe, mark int64) {
	var clean *probe
	if c.admit.Load() < 0 {
		if current := a.current.Load(); current != p {
			current.finished.Add(1)
		} else if current.finished.Load() == mark {
			clean = current
		}
	}
	if elapsed > longSample {
		return
	}
	ns := int64(max(elapsed, 0))
	if baseline := a.baseline.Load(); baseline > 0 {
		ns = min(ns, sampleCap*baseline)
	}
	if clean != nil {
		clean.sum.Add(ns)
		if clean.count.Add(1) == probeSamples {
			a.mu.Lock()
			if c.admit.Load() < 0 && a.current.Load() == clean {
				a.endProbeLocked(c)
			}
			a.mu.Unlock()
		}
	}
	packed := oneSample | uint64(ns>>sumShift)
	if inFlight <= a.min {
		a.quiet.Add(packed)
	}
	if a.samples.Add(packed)>>countShift >= a.windowSize() && a.mu.TryLock() {
		if a.samples.Load()>>countShift >= a.windowSize() {
			a.closeWindowLocked(c)
		}
		a.mu.Unlock()
	}
}

func (a *adaptive) windowSize() uint64 {
	return uint64(windowPerLimit * a.limit.Load())
}

func (a *adaptive) closeWindowLocked(c *compiledLoadShed) {
	samples, quiet := a.samples.Swap(0), a.quiet.Swap(0)
	n, sum := samples>>countShift, (samples&sumMask)<<sumShift
	quietCount, quietSum := quiet>>countShift, (quiet&sumMask)<<sumShift
	isPressure := a.isPressure.Swap(false)
	isProbeSeen := a.isProbeSeen
	a.isProbeSeen = c.admit.Load() < 0
	now := a.now()

	average := float64(sum) / float64(n)
	a.average.Store(int64(average))
	if quietCount >= quietSamples {
		a.learnLocked(float64(quietSum)/float64(quietCount), now)
	}
	if a.hasBaseline && average < a.baselineNs {
		a.storeBaselineLocked(max(average, a.learned/tolerance))
	}
	if !isPressure {
		return
	}
	if a.hasBaseline && !isProbeSeen {
		a.adjustLocked(c, average)
	}
	if c.admit.Load() >= 0 && (a.isBaselineStale(now) || a.isFloorHeld) && a.isProbeDue(now) {
		a.startProbeLocked(c, now)
	}
}

func (a *adaptive) isBaselineStale(now time.Time) bool {
	return !a.hasBaseline || now.Sub(a.baselineAt) > baselineMaxAge
}

func (a *adaptive) isProbeDue(now time.Time) bool {
	return !a.hasProbed || now.Sub(a.probedAt) > probeSpacing
}

func (a *adaptive) adjustLocked(c *compiledLoadShed, average float64) {
	gradient := 1.0
	if average > 0 {
		gradient = min(tolerance*a.baselineNs/average, 1)
	}
	useful := a.limitValue * gradient
	target := useful + math.Sqrt(useful)
	next := smoothing*a.limitValue + (1-smoothing)*target
	if next > a.limitValue && average < a.baselineNs/2 {
		return
	}
	if floor := float64(max(a.min, (a.earned+1)/2)); next < floor {
		next, a.isFloorHeld = floor, true
	}
	limit := min(max(next, float64(a.min)), float64(a.max))
	a.limitValue = limit
	if admitted := int64(limit); admitted > a.limit.Load() {
		a.earned = max(a.earned, admitted)
	}
	a.limit.Store(int64(limit))
	if c.admit.Load() >= 0 {
		c.admit.Store(int64(limit))
	}
}

func (a *adaptive) learnLocked(input float64, now time.Time) {
	if a.hasBaseline {
		input = (a.learned + input) / 2
	}
	a.learned, a.hasBaseline, a.baselineAt = input, true, now
	a.storeBaselineLocked(input)
}

func (a *adaptive) storeBaselineLocked(ns float64) {
	a.baselineNs = ns
	a.baseline.Store(int64(ns))
}

func (a *adaptive) startProbeLocked(c *compiledLoadShed, now time.Time) {
	a.probes.Add(1)
	a.current.Store(&probe{c: c})
	a.probeStart.Store(now.UnixNano())
	a.probedAt, a.hasProbed, a.isProbeSeen = now, true, true
	c.admit.Store(-a.min)
}

func (a *adaptive) endProbeLocked(c *compiledLoadShed) {
	p := a.current.Load()
	if n := p.count.Load(); n > 0 {
		a.learnLocked(float64(p.sum.Load())/float64(n), a.now())
		a.earned = min(a.earned, a.limit.Load())
		a.isFloorHeld = false
	}
	c.admit.Store(a.limit.Load())
}

func (a *adaptive) fields(c *compiledLoadShed) []inspect.Field {
	return []inspect.Field{
		{Key: "min", Value: a.min},
		{Key: "max", Value: a.max},
		{Key: "probes", Value: a.probes.Load()},
		{Key: "probing", Value: c.admit.Load() < 0},
		{Key: "baseline", Value: time.Duration(a.baseline.Load())},
		{Key: "average", Value: time.Duration(a.average.Load())},
	}
}
