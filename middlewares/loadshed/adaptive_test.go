package loadshed

import (
	"math"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

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

func TestAdaptive_BaselineLoweredAtMostToTwoThirds(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.saturated(8*time.Millisecond, 1, g.steady(8))
	if got := durationOf(g.s.Snapshot(), "baseline"); got != 8*time.Millisecond {
		t.Error(test.DiffMessage(got, 8*time.Millisecond, "L9: a window at 8 ms lowers the baseline to 8 ms"))
	}
	g.saturated(3*time.Millisecond, 1, g.steady(8))
	got := durationOf(g.s.Snapshot(), "baseline")
	if got < 6660*time.Microsecond || got > 6670*time.Microsecond {
		t.Error(test.DiffMessage(got, "6.66ms..6.67ms", "L9: a window at 3 ms lowers the baseline only to learned / 1.5"))
	}
	if got := g.limit(); got != rigStart {
		t.Error(test.DiffMessage(got, int64(rigStart), "L10: a fast, app-limited window leaves the limit"))
	}
	g.drain(3 * time.Millisecond)
}

func TestAdaptive_LearningResetsLoweredBaseline(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.saturated(8*time.Millisecond, 1, g.steady(8))
	g.saturated(3*time.Millisecond, 1, g.steady(8))
	// The 7 requests still in flight finish in the next window: drain them at 10 ms and fill
	// the rest of that window, so it is a quiet window at 10 ms.
	g.drain(10 * time.Millisecond)
	g.sequential(10*time.Millisecond, g.window()-7)
	if got := durationOf(g.s.Snapshot(), "baseline"); got != 10*time.Millisecond {
		t.Error(test.DiffMessage(got, 10*time.Millisecond, "L9: a quiet window at 10 ms learns 10 ms again"))
	}
	g.sequential(4*time.Millisecond, g.window())
	got := durationOf(g.s.Snapshot(), "baseline")
	if got < 4660*time.Microsecond || got > 4670*time.Microsecond {
		t.Error(test.DiffMessage(got, "4.66ms..4.67ms", "L9: learned 7 ms, lowered by its own average to learned / 1.5"))
	}
}

func TestAdaptive_LoweringDoesNotRefreshBaselineAge(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.clock.Advance(20 * time.Second)
	g.saturated(6*time.Millisecond, 1, g.steady(8))
	g.clock.Advance(11 * time.Second)
	g.pressureWindow(7 * time.Millisecond)
	if got := field(g.s.Snapshot(), "probing"); got != true {
		t.Error(test.DiffMessage(got, true, "L9, L11: lowering the baseline leaves its age, so a baseline learned 31 s ago is probed"))
	}
	g.drain(7 * time.Millisecond)
}

func TestAdaptive_SampleCappedAt32Baselines(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.pressureWindowOf(slowFirst(20*time.Second, 10*time.Millisecond))
	got := durationOf(g.s.Snapshot(), "average")
	if got < 12400*time.Microsecond || got > 12500*time.Microsecond {
		t.Error(test.DiffMessage(got, "12.4ms..12.5ms", "L8: a sample counts for at most 32 x the baseline (320 ms)"))
	}
	if got := g.limit(); got != 33 {
		t.Error(test.DiffMessage(got, int64(33), "L8, L10: the window is fast, the limit grows"))
	}
	g.drain(10 * time.Millisecond)
}

func TestAdaptive_NoCapWithoutBaseline(t *testing.T) {
	g := newRig(t)
	g.contended(slowFirst(20*time.Second, 10*time.Millisecond), g.window())
	if got := durationOf(g.s.Snapshot(), "average"); got < 166*time.Millisecond {
		t.Error(test.DiffMessage(got, ">= 166ms", "L8: with no baseline a sample is not capped"))
	}
	g.drain(10 * time.Millisecond)
}

func TestAdaptive_SlowQuietSampleBounded(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.sequentialOf(slowFirst(20*time.Second, 10*time.Millisecond), g.window())
	got := durationOf(g.s.Snapshot(), "baseline")
	if got < 11200*time.Microsecond || got > 11300*time.Microsecond {
		t.Error(test.DiffMessage(got, "11.2ms..11.3ms", "L8, L9: one slow quiet sample teaches the baseline at most 32 baselines"))
	}
}

func TestAdaptive_NoGrowthFromFarFasterWindow(t *testing.T) {
	g := newRig(t)
	g.sequential(10*time.Millisecond, g.window())
	g.pressureWindow(3 * time.Millisecond)
	if got := g.limit(); got != rigStart {
		t.Error(test.DiffMessage(got, int64(rigStart), "L10: a window under half of the baseline does not grow the limit"))
	}
	got := durationOf(g.s.Snapshot(), "baseline")
	if got < 6660*time.Microsecond || got > 6670*time.Microsecond {
		t.Error(test.DiffMessage(got, "6.66ms..6.67ms", "L9: the baseline is lowered to two thirds"))
	}
	g.pressureWindow(4 * time.Millisecond)
	if got := g.limit(); got != 33 {
		t.Error(test.DiffMessage(got, int64(33), "L10: the next window at 4 ms grows the limit"))
	}
	g.drain(4 * time.Millisecond)
}

func TestAdaptive_FloorIsHalfOfEarnedLimit(t *testing.T) {
	g := floorRig(t)
	for i := 0; i < 20; i++ {
		g.pressureWindow(60 * time.Millisecond)
		if got := g.limit(); got != 20 {
			t.Fatal(test.DiffMessage(got, int64(20), "L10: an empty probe leaves the floor where it is"))
		}
	}
	if got := probesOf(g); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "L11: no probe within 30 s of the last one"))
	}
	g.drain(60 * time.Millisecond)
}

func TestAdaptive_ProbeRearmsFloor(t *testing.T) {
	g := floorRig(t)
	g.clock.Advance(31 * time.Second)
	g.pressureWindow(60 * time.Millisecond)
	if probesOf(g) != 2 || field(g.s.Snapshot(), "probing") != true {
		t.Fatal(test.DiffMessage([]any{probesOf(g), field(g.s.Snapshot(), "probing")}, []any{int64(2), true}, "L11: 31 s later a window under pressure starts probe 2"))
	}
	g.drain(60 * time.Millisecond)
	for round := 0; round < 2; round++ {
		for i := 0; i < rigMin; i++ {
			if !g.admit() {
				t.Fatal("fixture: a probe admits min requests")
			}
		}
		for i := 0; i < rigMin; i++ {
			g.finishOldest(10 * time.Millisecond)
		}
	}
	if got := field(g.s.Snapshot(), "probing"); got != false {
		t.Fatal(test.DiffMessage(got, false, "L11: the probe ends after 8 finished requests"))
	}
	for i := 0; i < 200 && g.limit() > 10; i++ {
		g.pressureWindow(60 * time.Millisecond)
		if g.limit() < 10 {
			t.Fatal(test.DiffMessage(g.limit(), ">= 10", "L10, L11: a finished probe moves the floor to half of the limit, 10"))
		}
	}
	if got := g.limit(); got != 10 {
		t.Error(test.DiffMessage(got, int64(10), "L10, L11: the limit goes down to the new floor"))
	}
	for i := 0; i < 10; i++ {
		g.pressureWindow(60 * time.Millisecond)
		if got := g.limit(); got != 10 {
			t.Fatal(test.DiffMessage(got, int64(10), "L10: and stays there"))
		}
	}
	g.drain(60 * time.Millisecond)
}

func TestAdaptive_EmptyProbeKeepsFloor(t *testing.T) {
	g := floorRig(t)
	g.clock.Advance(31 * time.Second)
	g.pressureWindow(60 * time.Millisecond)
	if probesOf(g) != 2 || field(g.s.Snapshot(), "probing") != true {
		t.Fatal(test.DiffMessage([]any{probesOf(g), field(g.s.Snapshot(), "probing")}, []any{int64(2), true}, "L11: 31 s later a window under pressure starts probe 2"))
	}
	g.clock.Advance(1100 * time.Millisecond)
	g.admit()
	if got := field(g.s.Snapshot(), "probing"); got != false {
		t.Fatal(test.DiffMessage(got, false, "L11: the probe ends empty"))
	}
	for i := 0; i < 20; i++ {
		g.pressureWindow(60 * time.Millisecond)
		if got := g.limit(); got != 20 {
			t.Fatal(test.DiffMessage(got, int64(20), "L11: a probe that ends with no request changes nothing, the floor stays"))
		}
	}
	g.drain(60 * time.Millisecond)
}

func TestAdaptive_WindowSumHoldsLargestWindow(t *testing.T) {
	withMachine(t, 4, 16<<30)
	mw := LoadShed{MaxQueue: -1}.NewMiddleware()
	c := mw.(*compiledLoadShed)
	if intOf(c.Snapshot(), "max") != 4096 {
		t.Fatal(test.DiffMessage(intOf(c.Snapshot(), "max"), int64(4096), "fixture: max for 16 GiB"))
	}
	c.adaptive.limit.Store(4096)
	c.admit.Store(4096)
	g := &rig{t: t, mw: mw, s: snapshotter(t, mw), clock: &fakeClock{t: time.Now()}}
	const samples = 4 * 4096
	for i := 0; i < samples; i++ {
		for len(g.pool) < 8 {
			if !g.admit() {
				t.Fatal("fixture: no request is refused")
			}
		}
		g.finishOldest(29 * time.Second)
		if i == samples-2 {
			if got := durationOf(g.s.Snapshot(), "average"); got != 0 {
				t.Fatal(test.DiffMessage(got, time.Duration(0), "M4: the window is still open one sample before its end"))
			}
		}
	}
	if got := durationOf(g.s.Snapshot(), "average"); got != 29*time.Second {
		t.Error(test.DiffMessage(got, 29*time.Second, "M4, L8: the sum of the largest window does not overflow"))
	}
	g.drain(29 * time.Second)
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

func TestAdaptive_NeverAboveLimitDuringProbe(t *testing.T) {
	withMachine(t, 1, rigMemory)
	mw := LoadShed{MaxQueue: -1}.NewMiddleware()
	c := mw.(*compiledLoadShed)
	s := snapshotter(t, mw)
	var current, peak, heldViolations atomic.Int64
	observe := func() {
		now := current.Add(1)
		for {
			seen := peak.Load()
			if now <= seen || peak.CompareAndSwap(seen, now) {
				break
			}
		}
		if c.admit.Load() < 0 {
			if record := c.adaptive.current.Load(); record != nil && record.held.Load() > c.adaptive.min {
				runtime.Gosched()
				if c.admit.Load() < 0 && record.held.Load() > c.adaptive.min {
					heldViolations.Add(1)
				}
			}
		}
		runtime.Gosched()
		current.Add(-1)
	}
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 600; i++ {
				call, ok := tryAdmit(mw)
				if !ok {
					runtime.Gosched()
					continue
				}
				observe()
				call.ex.finish(time.Duration(g*i%50+1) * time.Microsecond)
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	if peak.Load() > c.adaptive.max {
		t.Error(test.DiffMessage(peak.Load(), c.adaptive.max, "L1: never more requests in flight than max, during a probe too"))
	}
	if got := heldViolations.Load(); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "L1: a probe never holds more than min of its own requests"))
	}
	if got := intOf(snap, "in_flight"); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "L4: every slot released"))
	}
	if intOf(snap, "probes") == 0 {
		t.Error(test.DiffMessage(intOf(snap, "probes"), "> 0", "fixture: the run went through a probe"))
	}
}

func TestAdaptive_ProbeAdmitsBesideHeldRequests(t *testing.T) {
	g := newRig(t)
	g.holdOutside(rigMin)
	g.startProbeBeside(100 * time.Millisecond)
	g.drain(100 * time.Millisecond)
	for i := 0; i < rigMin; i++ {
		if !g.admit() {
			t.Fatal(test.DiffMessage(false, true, "L1, L11: a probe admits min requests of its own beside requests held from before it"))
		}
	}
	if g.admit() {
		t.Error(test.DiffMessage(true, false, "L1, L11: the request after the probe's min is refused"))
	}
	for i := 0; i < rigMin; i++ {
		g.finishOldest(5 * time.Millisecond)
	}
	g.sequential(5*time.Millisecond, 8-rigMin)
	snap := g.s.Snapshot()
	if got := field(snap, "probing"); got != false {
		t.Error(test.DiffMessage(got, false, "L11: the probe ends after 8 probe samples"))
	}
	if got := durationOf(snap, "baseline"); got != 5*time.Millisecond {
		t.Error(test.DiffMessage(got, 5*time.Millisecond, "L11: the probe learns the baseline beside held requests"))
	}
}

func TestAdaptive_ProbeSlotsBelongToOneProbe(t *testing.T) {
	g := newRig(t)
	g.startProbeBeside(100 * time.Millisecond)
	g.drain(5 * time.Millisecond)
	g.admitOutside("fixture: probe 1 admits a request")
	g.sequential(5*time.Millisecond, 8)
	g.expectProbing(false, "fixture: probe 1 ended after 8 samples")
	g.pressureWindow(100 * time.Millisecond)
	g.clock.Advance(31 * time.Second)
	g.startProbeBeside(100 * time.Millisecond)
	if probesOf(g) != 2 {
		t.Fatal(test.DiffMessage(probesOf(g), int64(2), "fixture: probe 2 started"))
	}
	g.drain(5 * time.Millisecond)
	for i := 0; i < rigMin; i++ {
		if !g.admit() {
			t.Fatalf("L1: probe 2 admits min requests beside the request held from probe 1 (admitted %d of %d)", i, rigMin)
		}
	}
	if g.admit() {
		t.Error(test.DiffMessage(true, false, "L1: probe 2 admits no more than min of its own"))
	}
	g.drain(5 * time.Millisecond)
}

func TestAdaptive_ProbeKeepsLimit(t *testing.T) {
	g := newRig(t)
	held := g.holdOutside(30)
	g.startProbeBeside(100 * time.Millisecond)
	g.drain(100 * time.Millisecond)
	limit := g.limit()
	if limit != rigStart {
		t.Fatal(test.DiffMessage(limit, int64(rigStart), "fixture: the limit is unchanged by the probe's window"))
	}
	for i := 0; i < 2; i++ {
		if !g.admit() {
			t.Fatal(test.DiffMessage(false, true, "L1, L11: the probe admits while the total stays within the limit and below its min"))
		}
	}
	if g.admit() {
		t.Error(test.DiffMessage(true, false, "L1: the total in flight never passes the limit during a probe"))
	}
	held[0].ex.finish(5 * time.Millisecond)
	if !g.admit() {
		t.Error(test.DiffMessage(false, true, "L1: a finished request frees one slot of the limit during a probe"))
	}
	if g.admit() {
		t.Error(test.DiffMessage(true, false, "L1: and only one"))
	}
}

func TestAdaptive_ProbeSampleOverlappingOutsideFinishIgnored(t *testing.T) {
	g := newRig(t)
	g.startProbeBeside(100 * time.Millisecond)
	probe := g.admitOutside("L1, L11: the probe admits into the slot the window left free")
	g.finishOldest(100 * time.Millisecond)
	probe.ex.finish(50 * time.Millisecond)
	g.drain(100 * time.Millisecond)
	g.expectProbing(true, "L11: a probe sample that overlapped an old request's finish does not count toward the 8")
	g.sequential(5*time.Millisecond, 8)
	snap := g.s.Snapshot()
	if got := field(snap, "probing"); got != false {
		t.Error(test.DiffMessage(got, false, "L11: the probe ends after 8 clean samples"))
	}
	if got := durationOf(snap, "baseline"); got != 5*time.Millisecond {
		t.Error(test.DiffMessage(got, 5*time.Millisecond, "L11: the overlapping 50 ms sample is ignored"))
	}
}

func TestAdaptive_ProbeOfOverlappingSamplesOnlyIsEmpty(t *testing.T) {
	g := newRig(t)
	g.startProbeBeside(100 * time.Millisecond)
	limit := g.limit()
	for i := 0; i < 8; i++ {
		probe := g.admitOutside("L1, L11: the probe admits one request beside the old ones")
		g.finishOldest(100 * time.Millisecond)
		probe.ex.finish(5 * time.Millisecond)
	}
	g.expectProbing(true, "L11: samples that overlap old finishes do not end the probe")
	g.clock.Advance(1100 * time.Millisecond)
	g.admit()
	snap := g.s.Snapshot()
	if got := field(snap, "probing"); got != false {
		t.Error(test.DiffMessage(got, false, "L11: the next admission after 1 s ends the probe"))
	}
	if got := durationOf(snap, "baseline"); got != 0 {
		t.Error(test.DiffMessage(got, time.Duration(0), "L11: a probe with no clean sample leaves the baseline"))
	}
	if got := intOf(snap, "probes"); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "I4: one probe"))
	}
	if got := g.limit(); got != limit {
		t.Error(test.DiffMessage(got, limit, "L11: an empty probe leaves the limit"))
	}
}

func TestAdaptive_ProbeRequestAfterProbeIsOrdinary(t *testing.T) {
	g := newRig(t)
	g.startProbeBeside(100 * time.Millisecond)
	g.drain(5 * time.Millisecond)
	earlier := g.admitOutside("fixture: probe 1 admits a request")
	g.sequential(5*time.Millisecond, 8)
	g.expectProbing(false, "fixture: probe 1 ended")
	g.pressureWindow(100 * time.Millisecond)
	g.clock.Advance(31 * time.Second)
	g.startProbeBeside(100 * time.Millisecond)
	g.drain(5 * time.Millisecond)
	running := g.admitOutside("L1, L11: probe 2 admits a request")
	earlier.ex.finish(7 * time.Millisecond)
	running.ex.finish(50 * time.Millisecond)
	g.sequential(5*time.Millisecond, 7)
	g.expectProbing(true, "L11: a request of probe 1 finishing during probe 2 is no sample of probe 2, and the sample it spoiled is ignored")
	g.sequential(5*time.Millisecond, 1)
	snap := g.s.Snapshot()
	if got := field(snap, "probing"); got != false {
		t.Error(test.DiffMessage(got, false, "L11: probe 2 ends with its own 8th clean sample"))
	}
	if got := durationOf(snap, "baseline"); got != 5*time.Millisecond {
		t.Error(test.DiffMessage(got, 5*time.Millisecond, "L11: only probe 2's clean samples are learned"))
	}
}

func TestAdaptive_ProbeEndingOnTimeWakesWaiters(t *testing.T) {
	withMachine(t, rigProcs, rigMemory)
	mw := LoadShed{MaxQueue: 4}.NewMiddleware()
	c := mw.(*compiledLoadShed)
	s := snapshotter(t, mw)
	clock := &fakeClock{t: time.Now()}
	c.adaptive.now = clock.Now
	c.adaptive.mu.Lock()
	c.adaptive.startProbeLocked(c, clock.Now())
	c.adaptive.mu.Unlock()
	var held []*call
	for i := int64(0); i < c.adaptive.min; i++ {
		held = append(held, admitNow(t, mw))
	}
	waiter := startCall(mw, request(http.MethodGet, "/ok"))
	waitUntil(t, "the waiter is queued behind the probe's full slots", func() bool { return intOf(s.Snapshot(), "waiting") == 1 })

	clock.Advance(2 * time.Second)
	newcomer, ok := tryAdmit(mw)
	if !ok {
		t.Fatal(test.DiffMessage(false, true, "L11: the first admission after 1 s ends the probe and is admitted"))
	}
	waiter.wait(t)
	if !waiter.admitted.Load() || waiter.refusal != nil {
		t.Error(test.DiffMessage([]any{waiter.admitted.Load(), waiter.refusal}, []any{true, nil}, "L2: a probe that ends on time hands the freed slots to the waiters"))
	}
	for _, h := range append(held, newcomer, waiter) {
		h.ex.finish(time.Millisecond)
	}
}

func TestAdaptive_Stable_CPUBound_1CPU(t *testing.T) { stableRun(t, 1, "cpu") }

func TestAdaptive_Stable_CPUBound_2CPU(t *testing.T) { stableRun(t, 2, "cpu") }

func TestAdaptive_Stable_IOBound_1CPU(t *testing.T) { stableRun(t, 1, "io") }

func TestAdaptive_Stable_IOBound_2CPU(t *testing.T) { stableRun(t, 2, "io") }

func TestAdaptive_NoRefusalBelowCapacity_1CPU(t *testing.T) { belowCapacityRun(t, 1) }

func TestAdaptive_NoRefusalBelowCapacity_2CPU(t *testing.T) { belowCapacityRun(t, 2) }

func TestAdaptive_RecoversWhenHandlerSlows_1CPU(t *testing.T) { recoversRun(t, 1) }

func TestAdaptive_RecoversWhenHandlerSlows_2CPU(t *testing.T) { recoversRun(t, 2) }

func TestAdaptive_Steering_FreeBursts(t *testing.T) {
	steeringRun(t, freeBursts, "free bursts", 0.5, true, true)
}

func TestAdaptive_Steering_SlowUploads(t *testing.T) {
	steeringRun(t, slowUploads, "slow uploads", 0.5, true, true)
}

func TestAdaptive_Steering_SlowRoute(t *testing.T) {
	steeringRun(t, slowRoute, "slow route", 0.45, false, false)
}

func TestAdaptive_LongHeld_OneStream_1CPU(t *testing.T) { longHeldRun(t, 1, "cpu", 1) }

func TestAdaptive_LongHeld_TwoStreams_1CPU(t *testing.T) { longHeldRun(t, 1, "cpu", 2) }

func TestAdaptive_LongHeld_ThreeStreams_2CPU(t *testing.T) { longHeldRun(t, 2, "cpu", 3) }

func TestAdaptive_LongHeld_OneStream_IOBound_1CPU(t *testing.T) {
	longHeldRun(t, 1, "io", 1)
}
