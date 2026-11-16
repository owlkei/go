package loadshed

import (
	"math"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
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
func (g *rig) window() int  { return 4 * int(g.limit()) }

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
func (g *rig) atLimit() func() int        { return func() int { return int(g.limit()) } }

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

func TestAdaptive_Bounds(t *testing.T) {
	cases := []struct {
		procs                     int
		memory                    int64
		wantMin, wantMax, wantLim int64
	}{
		{1, 512 << 20, 1, 128, 8},
		{2, 2 << 30, 2, 512, 16},
		{2, 64 << 20, 2, 64, 16},
		{4, 16 << 20, 4, 64, 32},
		{8, 16 << 20, 8, 64, 64},
		{1, 100 << 30, 1, 4096, 8},
		{1, math.MaxInt64, 1, 256, 8},
		{2, math.MaxInt64, 2, 512, 16},
		{32, math.MaxInt64, 32, 4096, 256},
	}
	for _, c := range cases {
		withMachine(t, c.procs, c.memory)
		snap := snapshotter(t, LoadShed{}.NewMiddleware()).Snapshot()
		if got := field(snap, "mode"); got != "adaptive" {
			t.Error(test.DiffMessage(got, "adaptive", "L1: MaxInFlight 0 is the adaptive mode"))
		}
		min, max, limit := intOf(snap, "min"), intOf(snap, "max"), intOf(snap, "limit")
		if min != c.wantMin {
			t.Error(test.DiffMessage(min, c.wantMin, "L7: min is GOMAXPROCS "+strconv.Itoa(c.procs)))
		}
		if cgroupMemoryLimited() && c.wantMax > 64 {
			if max < 64 || max > c.wantMax {
				t.Error(test.DiffMessage(max, "64.."+strconv.FormatInt(c.wantMax, 10), "L7: a cgroup limit may lower max"))
			}
			continue
		}
		if max != c.wantMax {
			t.Error(test.DiffMessage(max, c.wantMax, "L7: max for GOMAXPROCS "+strconv.Itoa(c.procs)+" and memory "+strconv.FormatInt(c.memory, 10)))
		}
		if limit != c.wantLim {
			t.Error(test.DiffMessage(limit, c.wantLim, "L7: the first limit is 8 x GOMAXPROCS clamped to min..max"))
		}
	}
}

func TestAdaptive_BoundsReadOnce(t *testing.T) {
	withMachine(t, 2, rigMemory)
	s := snapshotter(t, LoadShed{}.NewMiddleware())
	runtime.GOMAXPROCS(4)
	snap := s.Snapshot()
	if intOf(snap, "min") != 2 || intOf(snap, "max") != 64 {
		t.Error(test.DiffMessage([]int64{intOf(snap, "min"), intOf(snap, "max")}, []int64{2, 64}, "L7: the bounds are read once, in NewMiddleware"))
	}
}

func TestAdaptive_NoBaseline_LimitHolds(t *testing.T) {
	g := newRig(t)
	if g.limit() != rigStart {
		t.Fatal(test.DiffMessage(g.limit(), int64(rigStart), "fixture: the first limit"))
	}
	g.saturated(100*time.Millisecond, 8, g.steady(8))
	if got := g.limit(); got != rigStart {
		t.Error(test.DiffMessage(got, int64(rigStart), "L9: with no baseline the limit does not move"))
	}
	if got := durationOf(g.s.Snapshot(), "baseline"); got != 0 {
		t.Error(test.DiffMessage(got, time.Duration(0), "L9: contended samples never make a baseline"))
	}
	if got := field(g.s.Snapshot(), "probing"); got != false {
		t.Error(test.DiffMessage(got, false, "L11: nothing was queued or refused, no probe"))
	}
	g.drain(100 * time.Millisecond)
}

func TestAdaptive_BaselineFromQuietSamples(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	if got := durationOf(g.s.Snapshot(), "baseline"); got != 10*time.Millisecond {
		t.Error(test.DiffMessage(got, 10*time.Millisecond, "L9: the average of the quiet samples"))
	}
	if got := durationOf(g.s.Snapshot(), "average"); got != 10*time.Millisecond {
		t.Error(test.DiffMessage(got, 10*time.Millisecond, "I4: the last window's average"))
	}
	g.sequential(20*time.Millisecond, g.window())
	if got := durationOf(g.s.Snapshot(), "baseline"); got != 15*time.Millisecond {
		t.Error(test.DiffMessage(got, 15*time.Millisecond, "L9: new quiet samples are blended half and half with the previous baseline"))
	}
	if got := g.limit(); got != rigStart {
		t.Error(test.DiffMessage(got, int64(rigStart), "L10: a quiet, app-limited load leaves the limit"))
	}
}

func TestAdaptive_BaselineLoweredByFasterWindow(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.saturated(6*time.Millisecond, 1, g.steady(8))
	if got := durationOf(g.s.Snapshot(), "baseline"); got != 6*time.Millisecond {
		t.Error(test.DiffMessage(got, 6*time.Millisecond, "L9: the baseline is lowered to a window's average when that is smaller"))
	}
	if got := g.limit(); got != rigStart {
		t.Error(test.DiffMessage(got, int64(rigStart), "L10: a fast, app-limited window leaves the limit"))
	}
	g.drain(6 * time.Millisecond)
}

func TestAdaptive_ShrinksWhenSlow(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.pressureWindow(60 * time.Millisecond)
	if got := g.limit(); got != 27 {
		t.Error(test.DiffMessage(got, int64(27), "L10: gradient 0.25, useful 8, target 10.83, limit 0.8 x 32 + 0.2 x target = 27.77, floored"))
	}
	previous := g.limit()
	for i := 0; i < 200 && g.limit() > rigMin; i++ {
		g.pressureWindow(60 * time.Millisecond)
		now := g.limit()
		if now > previous {
			t.Fatal(test.DiffMessage(now, "<= "+strconv.FormatInt(previous, 10), "L10: a slow window never grows the limit"))
		}
		if floor := max(previous*4/5, rigMin); now < floor {
			t.Fatal(test.DiffMessage(now, ">= "+strconv.FormatInt(floor, 10), "L10: never more than 20% off per window"))
		}
		previous = now
	}
	if got := g.limit(); got != rigMin {
		t.Error(test.DiffMessage(got, int64(rigMin), "L10: slow window after slow window ends at min"))
	}
	g.drain(60 * time.Millisecond)
}

func TestAdaptive_WindowIsFourTimesTheLimit(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	size := g.window()
	if size != 4*rigStart {
		t.Fatal(test.DiffMessage(size, 4*rigStart, "fixture: four times the first limit"))
	}
	g.pressure()
	g.cycle(60*time.Millisecond, size-1)
	if got := g.limit(); got != rigStart {
		t.Fatal(test.DiffMessage(got, int64(rigStart), "L8: the window is still open one sample before 4 x limit"))
	}
	g.finishOldest(60 * time.Millisecond)
	if got := g.limit(); got != 27 {
		t.Error(test.DiffMessage(got, int64(27), "L8: the window closes at its 4 x limit-th sample"))
	}
	g.drain(60 * time.Millisecond)
}

func TestAdaptive_NeverBelowMin(t *testing.T) {
	g := newRigProcs(t, 6)
	if got := intOf(g.s.Snapshot(), "min"); got != 6 {
		t.Fatal(test.DiffMessage(got, int64(6), "fixture: min is GOMAXPROCS"))
	}
	g.sequential(1*time.Millisecond, g.window())
	for i := 0; i < 200; i++ {
		g.pressureWindow(10 * time.Second)
		if g.limit() < 6 {
			t.Fatal(test.DiffMessage(g.limit(), ">= 6", "L7: the limit never goes below GOMAXPROCS"))
		}
	}
	g.drain(10 * time.Second)
}

func TestAdaptive_GrowsWhenFast(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.pressureWindow(10 * time.Millisecond)
	if got := g.limit(); got != 33 {
		t.Error(test.DiffMessage(got, int64(33), "L10: gradient 1, target 37.66, limit 0.8 x 32 + 0.2 x target = 33.13, floored"))
	}
	previous := g.limit()
	windows := 1
	for ; windows < 300 && g.limit() < rigMax; windows++ {
		g.pressureWindow(10 * time.Millisecond)
		now := g.limit()
		if now < previous {
			t.Fatal(test.DiffMessage(now, ">= "+strconv.FormatInt(previous, 10), "L10: a fast window under pressure never shrinks the limit"))
		}
		if now > rigMax {
			t.Fatal(test.DiffMessage(now, "<= 64", "L7: never above max"))
		}
		previous = now
	}
	if got := g.limit(); got != rigMax {
		t.Error(test.DiffMessage(got, int64(rigMax), "L10: with room to grow the limit reaches max within 300 windows"))
	}
	for i := 0; i < 10; i++ {
		g.pressureWindow(10 * time.Millisecond)
		if g.limit() > rigMax {
			t.Fatal(test.DiffMessage(g.limit(), "<= 64", "L7: never above max"))
		}
	}
	g.drain(10 * time.Millisecond)
}

func TestAdaptive_HoldsWithoutPressure(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.saturated(100*time.Millisecond, 10, g.steady(8))
	g.saturated(100*time.Millisecond, 10, g.atLimit())
	g.saturated(10*time.Millisecond, 10, g.steady(8))
	g.saturated(10*time.Millisecond, 10, g.atLimit())
	if got := g.limit(); got != rigStart {
		t.Error(test.DiffMessage(got, int64(rigStart), "L10: with no request queued or refused the limit never moves, whatever the average and the number in flight"))
	}
	g.drain(10 * time.Millisecond)
}

func TestAdaptive_HoldsDuringProbeWindow(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.clock.Advance(31 * time.Second)
	g.pressureWindow(10 * time.Millisecond)
	if got := field(g.s.Snapshot(), "probing"); got != true {
		t.Fatal(test.DiffMessage(got, true, "L11: a window under pressure with a stale baseline starts a probe"))
	}
	held := g.limit()
	size := 4 * int(held)
	drained := len(g.pool)
	g.drain(100 * time.Millisecond)
	for i := 0; i < rigMin; i++ {
		if !g.admit() {
			t.Fatal("fixture: a probe admits min requests")
		}
	}
	if g.admit() {
		t.Fatal(test.DiffMessage(true, false, "fixture: a request over min is refused during the probe, the window is under pressure"))
	}
	g.drain(100 * time.Millisecond)
	g.sequential(100*time.Millisecond, 8-rigMin)
	if got := field(g.s.Snapshot(), "probing"); got != false {
		t.Fatal(test.DiffMessage(got, false, "fixture: the probe ended inside the window"))
	}
	g.sequential(100*time.Millisecond, size-drained-8-1)
	if got := g.limit(); got != held {
		t.Fatal(test.DiffMessage(got, held, "fixture: the window is still open"))
	}
	g.sequential(100*time.Millisecond, 1)
	if got := g.limit(); got != held {
		t.Error(test.DiffMessage(got, held, "L10: a window in which a probe ran leaves the limit, whatever its average"))
	}
}

func TestAdaptive_IgnoresLongSamples(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.saturated(31*time.Second, 6, g.steady(8))
	if got := g.limit(); got != rigStart {
		t.Error(test.DiffMessage(got, int64(rigStart), "L8: a sample longer than 30 s is ignored"))
	}
	if got := durationOf(g.s.Snapshot(), "baseline"); got != 10*time.Millisecond {
		t.Error(test.DiffMessage(got, 10*time.Millisecond, "L8: and never reaches the baseline"))
	}
	g.drain(31 * time.Second)
	g.sequential(10*time.Millisecond, g.window())
	if got := g.limit(); got != rigStart {
		t.Error(test.DiffMessage(got, int64(rigStart), "L8: long samples left no trace in the window"))
	}
}

func TestAdaptive_ProbeOnlyWhenSaturatedAndStale(t *testing.T) {
	g := newRig(t)
	g.pressureWindow(100 * time.Millisecond)
	snap := g.s.Snapshot()
	if got := field(snap, "probing"); got != true {
		t.Fatal(test.DiffMessage(got, true, "L11: a window with a refusal and no baseline starts a probe"))
	}
	if got := intOf(snap, "probes"); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "I4: probes started"))
	}
	g.drain(5 * time.Millisecond)

	for i := 0; i < rigMin; i++ {
		if !g.admit() {
			t.Fatal(test.DiffMessage(false, true, "L11: a probe admits min requests"))
		}
	}
	if g.admit() {
		t.Error(test.DiffMessage(true, false, "L11: during a probe admission uses min instead of limit"))
	}
	for i := 0; i < rigMin; i++ {
		g.finishOldest(5 * time.Millisecond)
	}
	if got := field(g.s.Snapshot(), "probing"); got != true {
		t.Error(test.DiffMessage(got, true, "L11: the probe lasts until 8 requests admitted after it started have finished"))
	}
	g.sequential(5*time.Millisecond, 8-rigMin-1)
	if got := field(g.s.Snapshot(), "probing"); got != true {
		t.Error(test.DiffMessage(got, true, "L11: seven finished, still probing"))
	}
	g.sequential(5*time.Millisecond, 1)
	snap = g.s.Snapshot()
	if got := field(snap, "probing"); got != false {
		t.Error(test.DiffMessage(got, false, "L11: the probe ends after 8 finished requests"))
	}
	if got := durationOf(snap, "baseline"); got != 5*time.Millisecond {
		t.Error(test.DiffMessage(got, 5*time.Millisecond, "L11: the probe's average is the baseline input"))
	}
	if got := intOf(snap, "probes"); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "I4: one probe"))
	}
	g.fill(16)
	if len(g.pool) != 16 {
		t.Error(test.DiffMessage(len(g.pool), 16, "L11: admission returns to limit"))
	}
	g.drain(5 * time.Millisecond)
}

func TestAdaptive_NoProbeWhileNothingQueuedOrRefused(t *testing.T) {
	g := newRig(t)
	g.saturated(100*time.Millisecond, 3, g.steady(8))
	snap := g.s.Snapshot()
	if got := field(snap, "probing"); got != false || intOf(snap, "probes") != 0 {
		t.Error(test.DiffMessage([]any{got, intOf(snap, "probes")}, []any{false, int64(0)}, "L11: no probe ever starts while nothing is being queued or refused"))
	}
	g.drain(100 * time.Millisecond)
}

func TestAdaptive_NoProbeWhileBaselineFresh(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.pressureWindow(100 * time.Millisecond)
	snap := g.s.Snapshot()
	if got := field(snap, "probing"); got != false || intOf(snap, "probes") != 0 {
		t.Error(test.DiffMessage([]any{got, intOf(snap, "probes")}, []any{false, int64(0)}, "L11: a baseline younger than 30 s is not probed"))
	}
	if got := durationOf(snap, "baseline"); got != 10*time.Millisecond {
		t.Error(test.DiffMessage(got, 10*time.Millisecond, "L9: slow contended samples leave the baseline"))
	}
	g.drain(100 * time.Millisecond)
}

func TestAdaptive_QueueWaitNotSampled(t *testing.T) {
	withMachine(t, rigProcs, rigMemory)
	mw := LoadShed{MaxQueue: 1}.NewMiddleware()
	clock := &fakeClock{t: time.Now()}
	mw.(*compiledLoadShed).adaptive.now = clock.Now
	g := &rig{t: t, mw: mw, s: snapshotter(t, mw), clock: clock}
	g.sequential(5*time.Millisecond, g.window())
	g.fill(rigStart)
	if len(g.pool) != rigStart {
		t.Fatal(test.DiffMessage(len(g.pool), rigStart, "fixture: the limit is reached"))
	}
	queued := startCall(mw, request(http.MethodGet, "/ok"))
	waitUntil(t, "the request is queued", func() bool { return intOf(g.s.Snapshot(), "waiting") == 1 })
	since := time.Now()
	waitUntil(t, "20 ms in the queue", func() bool { return time.Since(since) >= 20*time.Millisecond })
	g.finishOldest(5 * time.Millisecond)
	queued.wait(t)
	if !queued.admitted.Load() {
		t.Fatal(test.DiffMessage(queued.admitted.Load(), true, "fixture: the queued request is admitted when a slot frees"))
	}
	queued.ex.finish(25 * time.Millisecond)
	for len(g.pool) > 0 {
		g.finishOldest(5 * time.Millisecond)
	}
	g.sequential(5*time.Millisecond, 4*rigStart-(rigStart+1))
	if got := durationOf(g.s.Snapshot(), "average"); got > 5*time.Millisecond {
		t.Error(test.DiffMessage(got, "<= 5ms", "L8: the time spent in loadshed's queue is not part of the sample"))
	}
}

func TestAdaptive_ProbeNotRepeatedWithin30s(t *testing.T) {
	g := newRig(t)
	g.pressureWindow(100 * time.Millisecond)
	snap := g.s.Snapshot()
	if got := field(snap, "probing"); got != true || intOf(snap, "probes") != 1 {
		t.Fatal(test.DiffMessage([]any{got, intOf(snap, "probes")}, []any{true, int64(1)}, "L11: a window under pressure with no baseline starts probe 1"))
	}
	g.clock.Advance(1100 * time.Millisecond)
	if !g.admit() {
		t.Fatal("fixture: the next admission is accepted")
	}
	snap = g.s.Snapshot()
	if got := field(snap, "probing"); got != false {
		t.Error(test.DiffMessage(got, false, "L11: a probe still running after 1 s ends at the next admission"))
	}
	if got := durationOf(snap, "baseline"); got != 0 {
		t.Error(test.DiffMessage(got, time.Duration(0), "L11: a probe with no finished request leaves the baseline as it was"))
	}
	g.pressureWindow(100 * time.Millisecond)
	snap = g.s.Snapshot()
	if got := field(snap, "probing"); got != false || intOf(snap, "probes") != 1 {
		t.Error(test.DiffMessage([]any{got, intOf(snap, "probes")}, []any{false, int64(1)}, "L11: no probe starts within 30 s of the last one"))
	}
	g.clock.Advance(30 * time.Second)
	g.pressureWindow(100 * time.Millisecond)
	snap = g.s.Snapshot()
	if got := field(snap, "probing"); got != true || intOf(snap, "probes") != 2 {
		t.Error(test.DiffMessage([]any{got, intOf(snap, "probes")}, []any{true, int64(2)}, "L11: probe 2 starts once 30 s have passed"))
	}
	g.drain(100 * time.Millisecond)
}

func TestAdaptive_NeverAboveMaxUnderConcurrency(t *testing.T) {
	withMachine(t, rigProcs, rigMemory)
	mw := LoadShed{MaxQueue: -1}.NewMiddleware()
	s := snapshotter(t, mw)
	var current, peak atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 600; i++ {
				c, ok := tryAdmit(mw)
				if !ok {
					continue
				}
				now := current.Add(1)
				for {
					seen := peak.Load()
					if now <= seen || peak.CompareAndSwap(seen, now) {
						break
					}
				}
				runtime.Gosched()
				current.Add(-1)
				c.ex.finish(time.Duration(g*i%50+1) * time.Microsecond)
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	if peak.Load() > intOf(snap, "max") {
		t.Error(test.DiffMessage(peak.Load(), "<= "+strconv.FormatInt(intOf(snap, "max"), 10), "L1: never above the limit, whatever the limit does under load"))
	}
	if got := intOf(snap, "in_flight"); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "L4: every slot released"))
	}
	if got := intOf(snap, "limit"); got < intOf(snap, "min") || got > intOf(snap, "max") {
		t.Error(test.DiffMessage(got, "min..max", "L7: the limit stays within its bounds"))
	}
}

func TestAdaptive_IdleNoGoroutineNoTimer(t *testing.T) {
	g := newRig(t)
	before := runtime.NumGoroutine()
	g.sequential(10*time.Millisecond, 10*g.window())
	g.saturated(100*time.Millisecond, 5, g.steady(8))
	g.drain(100 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Error(test.DiffMessage(after, before, "L6: the limit is recomputed in the Done of a request, never by a background task"))
	}
}

func TestAdaptive_Snapshot(t *testing.T) {
	g := newRig(t)
	snap := g.s.Snapshot()
	for key, want := range map[string]int64{"min": rigMin, "max": rigMax, "limit": rigStart, "queue": 0, "probes": 0, "in_flight": 0, "waiting": 0} {
		if got := field(snap, key); got != want {
			t.Error(test.DiffMessage(got, want, "I4: "+key))
		}
	}
	if got := field(snap, "probing"); got != false {
		t.Error(test.DiffMessage(got, false, "I4: probing"))
	}
	if got := durationOf(snap, "baseline"); got != 0 {
		t.Error(test.DiffMessage(got, time.Duration(0), "I4: baseline unknown"))
	}
}
