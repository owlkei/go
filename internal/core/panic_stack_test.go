package core

import (
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestStackSampler_FirstOfKeyHasStack(t *testing.T) {
	installStackSeams(t)
	app, logger := sampleApp(t)
	w := panicAt(app, 0)
	entries := logger.named("UnhandledPanic")
	if len(entries) != 1 || !hasKey(entries[0].args, "stack") {
		t.Fatal(test.DiffMessage(entries, "one entry with stack", "S1: the first panic of a key in a second is logged with its stack"))
	}
	if st := lcText(entries[0].field("stack")); st == "" || len(st) > 4096 {
		t.Error(test.DiffMessage(len(st), "1..4096", "S1: the stack is logged as today, at most 4 KiB"))
	}
	lcExpectResponse(t, w, http.StatusInternalServerError, wantInternalErrorJSON, "S1: the response is unchanged")
}

func TestStackSampler_SameKeySameSecondNoStack(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	for range 3 {
		clock.advance(100 * time.Millisecond)
		panicAt(app, 4)
	}
	entries := logger.named("UnhandledPanic")
	if len(entries) != 3 {
		t.Fatal(test.DiffMessage(len(entries), 3, "S2: every panic is still logged"))
	}
	if !hasKey(entries[0].args, "stack") || hasKey(entries[1].args, "stack") || hasKey(entries[2].args, "stack") {
		t.Error(test.DiffMessage(stackCount(entries), 1, "S2: further panics of the key in the same second are logged without stack"))
	}
	want := slices.DeleteFunc(keysOf(entries[0].args), func(k string) bool { return k == "stack" })
	for _, e := range entries[1:] {
		if got := keysOf(e.args); !slices.Equal(got, want) || e.field("panic") != entries[0].field("panic") {
			t.Error(test.DiffMessage(got, want, "S2: every pair except stack is kept"))
		}
	}
}

func TestStackSampler_NewSecondAllowsAgain(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	panicAt(app, 2)
	clock.advance(700 * time.Millisecond)
	panicAt(app, 2)
	clock.advance(300 * time.Millisecond)
	panicAt(app, 2)
	clock.advance(300 * time.Millisecond)
	panicAt(app, 2)
	entries := logger.named("UnhandledPanic")
	got := []bool{hasKey(entries[0].args, "stack"), hasKey(entries[1].args, "stack"), hasKey(entries[2].args, "stack"), hasKey(entries[3].args, "stack")}
	if want := []bool{true, false, true, false}; !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "S1: seconds are Unix seconds, each second allows one stack per key"))
	}
}

func TestStackSampler_GlobalCap(t *testing.T) {
	installStackSeams(t)
	app, logger := sampleApp(t)
	for site := range 12 {
		panicAt(app, site)
	}
	entries := logger.named("UnhandledPanic")
	if len(entries) != 12 || stackCount(entries) != 10 {
		t.Fatal(test.DiffMessage([]int{len(entries), stackCount(entries)}, []int{12, 10}, "S3: never more than 10 records with stack per app per second, whatever the number of keys"))
	}
	if hasKey(entries[10].args, "stack") || hasKey(entries[11].args, "stack") {
		t.Error(test.DiffMessage(stackCount(entries[10:]), 0, "S2: past the cap the panic is logged without stack"))
	}
}

func TestStackSampler_RareKeyKeepsItsStackDuringFlood(t *testing.T) {
	installStackSeams(t)
	app, logger := sampleApp(t)
	for range 200 {
		panicAt(app, 0)
	}
	panicAt(app, 7)
	entries := logger.named("UnhandledPanic")
	if len(entries) != 201 || stackCount(entries) != 2 || !hasKey(entries[200].args, "stack") {
		t.Error(test.DiffMessage([]int{len(entries), stackCount(entries)}, []int{201, 2}, "S1: a flood of one key uses one stack, so a rarer panic keeps its own"))
	}
}

func TestStackSampler_TableBounded(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	const seconds = 100
	for range seconds {
		for site := range 12 {
			panicAt(app, site)
		}
		clock.advance(time.Second)
	}
	entries := logger.named("UnhandledPanic")
	if len(entries) != seconds*12 || stackCount(entries) != seconds*10 {
		t.Error(test.DiffMessage([]int{len(entries), stackCount(entries)}, []int{seconds * 12, seconds * 10}, "S3, S4: a fixed table and a per-second budget: every second logs 10 stacks, whatever happened before"))
	}
}

func TestStackSampler_AppsIndependent(t *testing.T) {
	installStackSeams(t)
	appA, loggerA := sampleApp(t)
	for site := range 12 {
		panicAt(appA, site)
	}
	appB, loggerB := sampleApp(t)
	panicAt(appB, 0)
	if stackCount(loggerA.named("UnhandledPanic")) != 10 {
		t.Error(test.DiffMessage(stackCount(loggerA.named("UnhandledPanic")), 10, "S3: the first app used its budget"))
	}
	if got := loggerB.named("UnhandledPanic"); len(got) != 1 || !hasKey(got[0].args, "stack") {
		t.Error(test.DiffMessage(got, "one entry with stack", "S3: two apps do not share a budget or a key table"))
	}
}

func TestStackSampler_ConcurrentNoRace(t *testing.T) {
	installStackSeams(t)
	app, logger := sampleApp(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := range 20 {
				panicAt(app, (g+i)%12)
			}
		}()
	}
	close(start)
	wg.Wait()
	entries := logger.named("UnhandledPanic")
	if len(entries) != 320 {
		t.Fatal(test.DiffMessage(len(entries), 320, "S2: every panic is logged"))
	}
	if n := stackCount(entries); n > 10 || n < 1 {
		t.Error(test.DiffMessage(n, "1..10", "S3: at most 10 stacks per second under concurrent panics"))
	}
}

func TestStackSampler_SuppressedLinePerSecond(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	panicAt(app, 0)
	if len(clock.armed()) != 0 {
		t.Fatal(test.DiffMessage(len(clock.armed()), 0, "S5: no timer without a suppression"))
	}
	panicAt(app, 0)
	panicAt(app, 0)
	timers := clock.armed()
	if len(timers) != 1 {
		t.Fatal(test.DiffMessage(len(timers), 1, "S5: the first suppression arms one timer, the next ones none"))
	}
	if d := timers[0].d; d <= 0 || d > time.Second {
		t.Error(test.DiffMessage(d, "(0, 1s]", "S5: the timer fires when the second ends"))
	}
	if len(logger.named("PanicStacksSuppressed")) != 0 {
		t.Error(test.DiffMessage(len(logger.named("PanicStacksSuppressed")), 0, "S5: the record comes when the second ends"))
	}
	timers[0].f()
	got := logger.named("PanicStacksSuppressed")
	if len(got) != 1 || got[0].level != "warn" || fmt.Sprint(got[0].field("stacks_suppressed")) != "2" {
		t.Fatal(test.DiffMessage(got, "one WARN with stacks_suppressed 2", "S5: one record per second with the count"))
	}

	clock.advance(time.Second)
	panicAt(app, 0)
	panicAt(app, 0)
	timers = clock.armed()
	if len(timers) != 2 {
		t.Fatal(test.DiffMessage(len(timers), 2, "S5: a new second with a suppression arms a new timer"))
	}
	timers[1].f()
	got = logger.named("PanicStacksSuppressed")
	if len(got) != 2 || fmt.Sprint(got[1].field("stacks_suppressed")) != "1" {
		t.Error(test.DiffMessage(got, "a second record with 1", "S5: the count restarts each second"))
	}
}

func TestStackSampler_CapSuppressionsAreCounted(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	for site := range 12 {
		panicAt(app, site)
	}
	timers := clock.armed()
	if len(timers) != 1 {
		t.Fatal(test.DiffMessage(len(timers), 1, "S5: suppressions past the cap arm the timer too"))
	}
	timers[0].f()
	got := logger.named("PanicStacksSuppressed")
	if len(got) != 1 || fmt.Sprint(got[0].field("stacks_suppressed")) != "2" {
		t.Error(test.DiffMessage(got, "stacks_suppressed 2", "S5: stacks suppressed by the cap are counted"))
	}
}

func TestStackSampler_NoTimerWithoutSuppression(t *testing.T) {
	clock := installStackSeams(t)
	app, _ := sampleApp(t)
	for site := range 5 {
		panicAt(app, site)
	}
	if n := len(clock.armed()); n != 0 {
		t.Error(test.DiffMessage(n, 0, "S5: no suppression, no timer"))
	}
}

func TestShutdown_ReportsSuppressedStacks(t *testing.T) {
	clock := installStackSeams(t)
	app, logger := sampleApp(t)
	panicAt(app, 0)
	panicAt(app, 0)
	panicAt(app, 0)
	if len(logger.named("PanicStacksSuppressed")) != 0 {
		t.Fatal(test.DiffMessage(1, 0, "setup: nothing reported yet"))
	}
	app.Stop()
	got := logger.named("PanicStacksSuppressed")
	if len(got) != 1 || fmt.Sprint(got[0].field("stacks_suppressed")) != "2" {
		t.Fatal(test.DiffMessage(got, "stacks_suppressed 2 at once", "S6: shutdown reports a pending count at once"))
	}
	timers := clock.armed()
	if len(timers) != 1 || !timers[0].stopped {
		t.Error(test.DiffMessage(timers, "the timer stopped", "S6: shutdown stops the timer"))
	}
	timers[0].f()
	if n := len(logger.named("PanicStacksSuppressed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "S6: nothing is reported twice"))
	}
}
