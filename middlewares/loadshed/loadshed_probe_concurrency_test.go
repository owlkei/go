package loadshed

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

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
