package trace

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlkei.com/owlkei/internal/test"
)

func TestTracer_ZeroValueDisabled(t *testing.T) {
	var tr Tracer
	if tr.Enabled() {
		t.Error(test.DiffMessage(true, false, "T1: the zero Tracer has no listener"))
	}
	tr.Emit(Event{ID: "x"})
}

func TestTracer_ListenEnablesAndReceives(t *testing.T) {
	var tr Tracer
	var got []Event
	stop := tr.Listen(func(e Event) { got = append(got, e) })
	if !tr.Enabled() {
		t.Error(test.DiffMessage(false, true, "T2: Enabled is true after Listen"))
	}
	want := Event{ID: "a", Stage: StageHandler, Name: "h", Transport: TransportHTTP, Duration: time.Second, Operation: "GET", Target: "/", Code: 200}
	tr.Emit(want)
	if len(got) != 1 || got[0] != want {
		t.Error(test.DiffMessage(got, []Event{want}, "T2: Emit calls the listener with the event by value"))
	}
	stop()
}

func TestTracer_ListenersRunInOrder(t *testing.T) {
	var tr Tracer
	var order []int
	for i := range 5 {
		tr.Listen(func(Event) { order = append(order, i) })
	}
	tr.Emit(Event{})
	want := []int{0, 1, 2, 3, 4}
	for i := range want {
		if len(order) != len(want) || order[i] != want[i] {
			t.Fatal(test.DiffMessage(order, want, "T2: listeners run in registration order"))
		}
	}
}

func TestTracer_ListenersRunOnEmittingGoroutineSynchronously(t *testing.T) {
	var tr Tracer
	var called bool
	tr.Listen(func(Event) { called = true })
	tr.Emit(Event{})
	if !called {
		t.Error(test.DiffMessage(called, true, "T2: the listener has run when Emit returns"))
	}
}

func TestTracer_StopRemovesOnlyItsRegistration(t *testing.T) {
	var tr Tracer
	var count int
	fn := func(Event) { count++ }
	stopFirst := tr.Listen(fn)
	tr.Listen(fn)
	tr.Emit(Event{})
	if count != 2 {
		t.Fatal(test.DiffMessage(count, 2, "T3: the same function registered twice is called twice"))
	}
	stopFirst()
	count = 0
	tr.Emit(Event{})
	if count != 1 {
		t.Error(test.DiffMessage(count, 1, "T3: stop of the first registration keeps the second"))
	}
	if !tr.Enabled() {
		t.Error(test.DiffMessage(false, true, "T3: still enabled while one registration remains"))
	}
}

func TestTracer_StopRemovesTheRightOneWhenOrderDiffers(t *testing.T) {
	var tr Tracer
	var got []string
	tr.Listen(func(Event) { got = append(got, "a") })
	stopB := tr.Listen(func(Event) { got = append(got, "b") })
	tr.Listen(func(Event) { got = append(got, "c") })
	stopB()
	tr.Emit(Event{})
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Error(test.DiffMessage(got, []string{"a", "c"}, "T3: the middle registration is removed, order of the rest kept"))
	}
}

func TestTracer_StopTwice(t *testing.T) {
	var tr Tracer
	var first, second int
	stop := tr.Listen(func(Event) { first++ })
	tr.Listen(func(Event) { second++ })
	stop()
	stop()
	tr.Emit(Event{})
	if first != 0 || second != 1 {
		t.Error(test.DiffMessage([]int{first, second}, []int{0, 1}, "T3: a second stop does nothing (does not remove another registration)"))
	}
}

func TestTracer_LastStopDisables(t *testing.T) {
	var tr Tracer
	stopA := tr.Listen(func(Event) {})
	stopB := tr.Listen(func(Event) {})
	stopA()
	stopB()
	if tr.Enabled() {
		t.Error(test.DiffMessage(true, false, "T3: Enabled is false after the last stop"))
	}
}

func TestTracer_ListenNil(t *testing.T) {
	var tr Tracer
	stop := tr.Listen(nil)
	if stop == nil {
		t.Fatal(test.DiffMessage(nil, "a func", "T4: Listen(nil) returns a stop"))
	}
	if tr.Enabled() {
		t.Error(test.DiffMessage(true, false, "T4: Listen(nil) registers nothing"))
	}
	stop()
	tr.Emit(Event{})
}

func TestTracer_EnabledAllocatesNothing(t *testing.T) {
	var tr Tracer
	tr.Listen(func(Event) {})
	if n := testing.AllocsPerRun(100, func() { _ = tr.Enabled() }); n != 0 {
		t.Error(test.DiffMessage(n, 0.0, "T5: Enabled allocates nothing"))
	}
}

func TestTracer_EmitAllocatesNothing(t *testing.T) {
	var tr Tracer
	var sink int
	tr.Listen(func(e Event) { sink += e.Code })
	e := Event{ID: "id", Stage: StageHandler, Name: "n", Code: 1}
	if n := testing.AllocsPerRun(100, func() { tr.Emit(e) }); n != 0 {
		t.Error(test.DiffMessage(n, 0.0, "T5: Emit allocates nothing"))
	}
	var empty Tracer
	if n := testing.AllocsPerRun(100, func() { empty.Emit(e) }); n != 0 {
		t.Error(test.DiffMessage(n, 0.0, "T5: Emit without a listener allocates nothing"))
	}
}

func TestTracer_ListenInsideListener(t *testing.T) {
	var tr Tracer
	var inner int
	var once sync.Once
	tr.Listen(func(Event) {
		once.Do(func() {
			tr.Listen(func(Event) { inner++ })
		})
	})
	tr.Emit(Event{})
	if inner != 0 {
		t.Error(test.DiffMessage(inner, 0, "T5: a listener registered during an Emit is not called by that Emit"))
	}
	tr.Emit(Event{})
	if inner != 1 {
		t.Error(test.DiffMessage(inner, 1, "T5: a listener registered during an Emit is called by the next Emit"))
	}
}

func TestTracer_StopInsideListener(t *testing.T) {
	var tr Tracer
	var calls int
	var stop func()
	stop = tr.Listen(func(Event) {
		calls++
		stop()
	})
	tr.Emit(Event{})
	tr.Emit(Event{})
	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "T5: a listener may stop itself; it is not called again"))
	}
	if tr.Enabled() {
		t.Error(test.DiffMessage(true, false, "T5: disabled after the only listener stopped itself"))
	}
}

func TestTracer_EmitInsideListener(t *testing.T) {
	var tr Tracer
	var seen []string
	tr.Listen(func(e Event) {
		seen = append(seen, e.ID)
		if e.ID == "outer" {
			tr.Emit(Event{ID: "inner"})
		}
	})
	tr.Emit(Event{ID: "outer"})
	if len(seen) != 2 || seen[0] != "outer" || seen[1] != "inner" {
		t.Error(test.DiffMessage(seen, []string{"outer", "inner"}, "T5: a listener may Emit"))
	}
}

func TestTracer_ListenerPanicPropagates(t *testing.T) {
	var tr Tracer
	tr.Listen(func(Event) { panic("boom") })
	var rec any
	func() {
		defer func() { rec = recover() }()
		tr.Emit(Event{})
	}()
	if rec != "boom" {
		t.Error(test.DiffMessage(rec, "boom", "T6: Emit does not recover a listener panic"))
	}
}

func TestTracer_ListenerPanicSkipsLaterListeners(t *testing.T) {
	var tr Tracer
	var later bool
	tr.Listen(func(Event) { panic("boom") })
	tr.Listen(func(Event) { later = true })
	func() {
		defer func() { _ = recover() }()
		tr.Emit(Event{})
	}()
	if later {
		t.Error(test.DiffMessage(later, false, "T6: nothing recovers, so the later listener is not reached"))
	}
}

func TestTracer_ConcurrentListenStopEmit_NoRace(t *testing.T) {
	var tr Tracer
	var total atomic.Int64
	tr.Listen(func(Event) { total.Add(1) })

	const goroutines = 8
	const rounds = 300
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range rounds {
				stop := tr.Listen(func(Event) { total.Add(1) })
				_ = tr.Enabled()
				stop()
			}
		}()
		go func() {
			defer wg.Done()
			for range rounds {
				tr.Emit(Event{ID: "x"})
			}
		}()
	}
	wg.Wait()
	if total.Load() < goroutines*rounds {
		t.Error(test.DiffMessage(total.Load(), goroutines*rounds, "T5: the permanent listener saw every Emit"))
	}
	if !tr.Enabled() {
		t.Error(test.DiffMessage(false, true, "T3: the permanent listener is still registered"))
	}
}

func TestEvent_SpanFieldsReachListeners(t *testing.T) {
	var tr Tracer
	var got Event
	tr.Listen(func(e Event) { got = e })
	want := Event{
		ID:        "0f1e2d3c4b5a6978",
		TraceID:   "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:    "00f067aa0ba902b7",
		ParentID:  "0f1e2d3c4b5a6978",
		Stage:     StageHandler,
		Name:      "n",
		Transport: TransportHTTP,
		Code:      200,
	}
	tr.Emit(want)
	if got != want {
		t.Error(test.DiffMessage(got, want, "E1-E3: TraceID, SpanID and ParentID are delivered unchanged"))
	}
}

func TestEvent_SpanFieldsAreStrings(t *testing.T) {
	e := Event{}
	got := []string{e.TraceID, e.SpanID, e.ParentID}
	if got[0] != "" || got[1] != "" || got[2] != "" {
		t.Error(test.DiffMessage(got, []string{"", "", ""}, "Q3: the span fields are plain strings, empty by default"))
	}
}

func TestTracer_EmitWithSpanFieldsAllocatesNothing(t *testing.T) {
	var tr Tracer
	var sink int
	tr.Listen(func(e Event) { sink += len(e.SpanID) })
	e := Event{ID: "id", TraceID: "t", SpanID: "s", ParentID: "p", Stage: StageHandler}
	if n := testing.AllocsPerRun(100, func() { tr.Emit(e) }); n != 0 {
		t.Error(test.DiffMessage(n, 0.0, "E5: events with span fields still cost no allocation to emit"))
	}
}
