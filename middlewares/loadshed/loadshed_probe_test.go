package loadshed

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

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

func queuedLoadShed(t *testing.T, maxQueue int) (*compiledLoadShed, *call, *call) {
	t.Helper()
	mw := LoadShed{MaxInFlight: 1, MaxQueue: maxQueue}.NewMiddleware()
	s := snapshotter(t, mw)
	first := admitNow(t, mw)
	waiter := startCall(mw, request(http.MethodGet, "/ok"))
	waitUntil(t, "the waiter is queued", func() bool { return intOf(s.Snapshot(), "waiting") == 1 })
	return mw.(*compiledLoadShed), first, waiter
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
