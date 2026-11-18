package logpipe

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestSink_SyncWriteReturnsAfterOutput(t *testing.T) {
	buf := &syncBuffer{}
	s := New(buf)
	for i := range 200 {
		s.Write(Info, numbered(i, 40), quietReport)
		if !strings.HasSuffix(buf.String(), string(numbered(i, 40))) {
			t.Fatal(test.DiffMessage(buf.String(), "the record just written", "P4: Write returns after the record was passed to the output"))
		}
	}
}

func TestSink_SyncStartsNoGoroutine(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()
	buf := &syncBuffer{}
	s := New(buf)
	for i := range 500 {
		s.Write(Info, numbered(i, 40), quietReport)
	}
	s.Flush(time.Minute)
	if !goroutinesAtMost(base) {
		t.Error(test.DiffMessage(runtime.NumGoroutine(), base, "P4, M5: a synchronous sink starts no goroutine"))
	}
}

func TestSink_SyncConcurrentNoLoss(t *testing.T) {
	w := newGateWriter()
	s := New(w)
	const goroutines, per = 16, 200
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range per {
				s.Write(Info, numbered(g*per+i, 30), quietReport)
			}
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSuffix(w.output(), "\n"), "\n")
	if len(lines) != goroutines*per {
		t.Fatal(test.DiffMessage(len(lines), goroutines*per, "P4: a synchronous sink drops nothing"))
	}
	seen := map[string]bool{}
	for _, l := range lines {
		if len(l) != 29 || seen[l] {
			t.Fatal(test.DiffMessage(l, "a whole record once", "P4: records are written whole"))
		}
		seen[l] = true
	}
	if w.maxConcurrent() != 1 {
		t.Error(test.DiffMessage(w.maxConcurrent(), 1, "P6: one Write call at a time on the output in synchronous mode"))
	}
}

func TestSink_AsyncDoesNotWaitForOutput(t *testing.T) {
	h := newHeldSink(t)
	within(t, "P5: Write returns without waiting for the output", func() {
		for i := range 50 {
			h.s.Write(Info, numbered(i, 40), quietReport)
		}
	})
	if got := h.w.output(); got != "" {
		t.Error(test.DiffMessage(got, "", "P5: nothing reached the held output"))
	}
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true once everything was written")
	var want strings.Builder
	for i := range 50 {
		want.Write(numbered(i, 40))
	}
	if got := h.w.output(); got != want.String() {
		t.Error(test.DiffMessage(len(got), want.Len(), "P5, O1: every record is written, in order"))
	}
}

func TestSink_BatchesWholeRecordsUpToBatchSize(t *testing.T) {
	h := newHeldSink(t)
	h.startWriter()
	const count = 300
	for i := range count {
		h.s.Write(Info, numbered(i, 1000), quietReport)
	}
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	sizes := h.w.sizes()
	if len(sizes) < 4 {
		t.Fatal(test.DiffMessage(len(sizes), ">= 4 batches", "P6: 300 KB does not fit one batch"))
	}
	for i, n := range sizes {
		if n > int(batchSize) {
			t.Error(test.DiffMessage(n, "<= "+strconv.Itoa(int(batchSize)), "P6: a batch is at most batchSize bytes"))
		}
		if i > 0 && i < len(sizes)-1 && n < int(batchSize)-1000 {
			t.Error(test.DiffMessage(n, ">= "+strconv.Itoa(int(batchSize)-1000), "P6: a batch takes whole records until the next one does not fit"))
		}
	}
	for _, c := range h.w.calls {
		if len(c)%1000 != 0 {
			t.Fatal(test.DiffMessage(len(c), "a multiple of 1000", "P6: a batch holds whole records only"))
		}
	}
	got := h.w.output()
	for i := range count {
		if !strings.Contains(got, fmt.Sprintf("%07d ", i)) {
			t.Fatal(test.DiffMessage(i, "present", "P6: no record lost"))
		}
	}
}

func TestSink_BatchOfLargeRecordsIsOneRecordEach(t *testing.T) {
	h := newHeldSink(t)
	h.startWriter()
	for i := range 20 {
		h.s.Write(Info, numbered(i, 40000), quietReport)
	}
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	sizes := h.w.sizes()
	for i, n := range sizes[1:] {
		if n != 40000 {
			t.Error(test.DiffMessage(n, 40000, "P6: a record that does not fit what is left starts its own batch, batch "+strconv.Itoa(i+1)))
		}
	}
	if len(sizes) != 21 {
		t.Error(test.DiffMessage(len(sizes), 21, "P6: one Write call per large record"))
	}
}

func TestSink_OneWriterAtATime(t *testing.T) {
	w := newGateWriter()
	s := New(w)
	s.StartAsync()
	defer s.StopAsync(time.Minute)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 500 {
				s.Write(Info, numbered(g*500+i, 30), quietReport)
			}
		}()
	}
	wg.Wait()
	flushOK(t, s, "P12: Flush returns true")
	if got := w.maxConcurrent(); got != 1 {
		t.Error(test.DiffMessage(got, 1, "P6: at most one goroutine writes to the output"))
	}
	if n := strings.Count(w.output(), "\n"); n != 4000 {
		t.Error(test.DiffMessage(n, 4000, "P6: every record written"))
	}
}

func TestSink_WriterEndsWhenEmpty(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()
	w := newGateWriter()
	s := New(w)
	s.StartAsync()
	if !goroutinesAtMost(base) {
		t.Error(test.DiffMessage(runtime.NumGoroutine(), base, "P6: StartAsync alone starts no goroutine"))
	}
	for burst := range 3 {
		for i := range 100 {
			s.Write(Info, numbered(burst*100+i, 40), quietReport)
		}
		flushOK(t, s, "P12: Flush returns true")
		if !goroutinesAtMost(base) {
			t.Error(test.DiffMessage(runtime.NumGoroutine(), base, "P6: the writer ends when the queue is empty, burst "+strconv.Itoa(burst)))
		}
	}
	s.StopAsync(time.Minute)
}

func TestSink_InfoAcceptedAtSoftLimit(t *testing.T) {
	h := newHeldSink(t)
	h.fillTo(Info, int(softLimit)-2, 'a')
	h.s.Write(Info, rec(2, 'x'), quietReport)
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	if !strings.HasSuffix(h.w.output(), "x\n") {
		t.Error(test.DiffMessage(h.w.output()[len(h.w.output())-3:], "x newline", "P7: a record that makes the queue exactly softLimit is accepted"))
	}
}

func TestSink_DebugInfoDroppedAboveSoftLimit(t *testing.T) {
	h := newHeldSink(t)
	rep := newReportRecorder("R levels\n")
	h.fillTo(Info, int(softLimit), 'a')
	within(t, "P7: a dropped record does not wait", func() {
		h.s.Write(Info, rec(2, 'x'), rep.fn)
		h.s.Write(Debug, rec(5, 'y'), rep.fn)
		h.s.Write(Info, rec(3000, 'x'), rep.fn)
	})
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	out := h.w.output()
	if strings.ContainsAny(strings.ReplaceAll(out, "R levels", ""), "xy") {
		t.Error(test.DiffMessage(true, false, "P7: Debug and Info above softLimit are dropped"))
	}
	if calls := rep.all(); len(calls) != 1 || calls[0] != [4]uint64{1, 2, 0, 0} {
		t.Error(test.DiffMessage(calls, [][4]uint64{{1, 2, 0, 0}}, "P7, P10: the drops are counted per level"))
	}
}

func TestSink_WarnAcceptedUntilFull(t *testing.T) {
	h := newHeldSink(t)
	fa := newFakeAfter(true)
	h.s.after = fa.after
	h.fillTo(Info, int(softLimit), 'a')
	for h.q < int(queueLimit) {
		h.write(Warn, min(int(queueLimit)-h.q, 9000), 'w', nil)
	}
	if fa.calls.Load() != 0 {
		t.Error(test.DiffMessage(fa.calls.Load(), 0, "P8: a Warn that fits queueLimit is accepted without waiting"))
	}
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	if got := len(h.w.output()); got != int(queueLimit) {
		t.Error(test.DiffMessage(got, int(queueLimit), "P8: every accepted Warn is written"))
	}
}

func TestSink_WarnWaitsThenDrops(t *testing.T) {
	h := newHeldSink(t)
	fa := newFakeAfter(true)
	h.s.after = fa.after
	rep := newReportRecorder("R warn\n")
	h.fillTo(Info, int(softLimit), 'a')
	for h.q < int(queueLimit) {
		h.write(Warn, min(int(queueLimit)-h.q, 9000), 'w', nil)
	}
	within(t, "P8: a Warn that finds no room returns after its wait", func() {
		h.s.Write(Warn, rec(2, 'z'), rep.fn)
	})
	if fa.calls.Load() != 1 {
		t.Error(test.DiffMessage(fa.calls.Load(), 1, "P8: the caller waited once"))
	}
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	if strings.Contains(strings.ReplaceAll(h.w.output(), "R warn", ""), "z") {
		t.Error(test.DiffMessage(true, false, "P8: the Warn that timed out is dropped"))
	}
	if calls := rep.all(); len(calls) != 1 || calls[0] != [4]uint64{0, 0, 1, 0} {
		t.Error(test.DiffMessage(calls, [][4]uint64{{0, 0, 1, 0}}, "P8, P10: the drop is counted for warn"))
	}
}

func TestSink_WarnWaitsForFullWait(t *testing.T) {
	h := newHeldSink(t)
	var waited atomic.Int64
	h.s.after = func(d time.Duration) <-chan time.Time {
		if d == fullWait {
			waited.Add(1)
			ch := make(chan time.Time, 1)
			ch <- time.Time{}
			return ch
		}
		return time.After(d)
	}
	h.fillTo(Info, int(softLimit), 'a')
	for h.q < int(queueLimit) {
		h.write(Error, min(int(queueLimit)-h.q, 9000), 'e', nil)
	}
	h.s.Write(Error, rec(2, 'z'), quietReport)
	if waited.Load() != 1 || fullWait != 100*time.Millisecond {
		t.Error(test.DiffMessage([]any{waited.Load(), fullWait}, []any{1, 100 * time.Millisecond}, "P8: Error waits at most fullWait (100 ms)"))
	}
}

func TestSink_WarnAcceptedWhenRoomAppears(t *testing.T) {
	h := newHeldSink(t)
	fa := newFakeAfter(false)
	h.s.after = fa.after
	h.startWriter()
	h.fillTo(Info, int(softLimit), 'a')
	for h.q < int(queueLimit) {
		h.write(Warn, min(int(queueLimit)-h.q, 9000), 'w', nil)
	}
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		h.s.Write(Warn, rec(800, 'z'), quietReport)
	}()
	<-fa.waiting
	h.w.release()
	within(t, "P8: a waiting Warn is accepted when the output frees room", func() { <-accepted })
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	out := h.w.output()
	if !strings.Contains(out, strings.Repeat("z", 799)+"\n") {
		t.Error(test.DiffMessage(false, true, "P8: the Warn that waited is written once there is room"))
	}
	if strings.Contains(out, "R quiet") {
		t.Error(test.DiffMessage(true, false, "P8: nothing was dropped, so no report"))
	}
}

func TestSink_NoWaitAfterTimeoutUntilProgress(t *testing.T) {
	h := newHeldSink(t)
	fa := newFakeAfter(true)
	h.s.after = fa.after
	h.startWriter()
	h.fillTo(Info, int(softLimit), 'a')
	for h.q < int(queueLimit) {
		h.write(Warn, min(int(queueLimit)-h.q, 9000), 'w', nil)
	}
	h.s.Write(Warn, rec(2, 'z'), quietReport)
	if fa.calls.Load() != 1 {
		t.Fatal(test.DiffMessage(fa.calls.Load(), 1, "P8: the first Warn waits"))
	}
	for range 50 {
		h.s.Write(Warn, rec(2, 'z'), quietReport)
		h.s.Write(Error, rec(2, 'z'), quietReport)
	}
	if fa.calls.Load() != 1 {
		t.Error(test.DiffMessage(fa.calls.Load(), 1, "P8: after a timed-out wait, Warn and Error are dropped at once until the output moves"))
	}
	h.w.release()
	<-h.w.entered
	h.s.Write(Warn, rec(1001, 'z'), quietReport)
	if fa.calls.Load() != 2 {
		t.Error(test.DiffMessage(fa.calls.Load(), 2, "P8: once the output completed a Write, a full queue makes the caller wait again"))
	}
}

func TestSink_FatalAlwaysAccepted(t *testing.T) {
	h := newHeldSink(t)
	fa := newFakeAfter(true)
	h.s.after = fa.after
	h.fillTo(Info, int(softLimit), 'a')
	for h.q < int(queueLimit) {
		h.write(Warn, min(int(queueLimit)-h.q, 9000), 'w', nil)
	}
	within(t, "P9: Fatal never waits", func() {
		h.s.Write(Fatal, rec(500, 'F'), quietReport)
	})
	if fa.calls.Load() != 0 {
		t.Error(test.DiffMessage(fa.calls.Load(), 0, "P9: Fatal is accepted without a wait"))
	}
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	if countLines(h.w.output(), 'F') != 1 {
		t.Error(test.DiffMessage(countLines(h.w.output(), 'F'), 1, "P9: the Fatal record is written although the queue was full"))
	}
}

func TestSink_DropReportPerLevel(t *testing.T) {
	h := newHeldSink(t)
	fa := newFakeAfter(true)
	h.s.after = fa.after
	rep := newReportRecorder("R levels\n")
	h.fillTo(Info, int(softLimit), 'a')
	for h.q < int(queueLimit) {
		h.write(Warn, min(int(queueLimit)-h.q, 9000), 'w', nil)
	}
	for range 2 {
		h.s.Write(Debug, rec(10, 'D'), rep.fn)
	}
	for range 3 {
		h.s.Write(Info, rec(10, 'I'), rep.fn)
	}
	h.s.Write(Warn, rec(10, 'W'), rep.fn)
	for range 4 {
		h.s.Write(Error, rec(10, 'E'), rep.fn)
	}
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true and reports the pending drops")
	want := [][4]uint64{{2, 3, 1, 4}}
	if got := rep.all(); len(got) != 1 || got[0] != want[0] {
		t.Error(test.DiffMessage(got, want, "P10: one count per level in the order debug, info, warn, error"))
	}
	if strings.Count(h.w.output(), "R levels\n") != 1 {
		t.Error(test.DiffMessage(strings.Count(h.w.output(), "R levels\n"), 1, "P10: the report is written once"))
	}
}

func TestSink_ReportUsesLastDroppedLoggerEncoder(t *testing.T) {
	h := newHeldSink(t)
	h.fillTo(Info, int(softLimit), 'a')
	first := newReportRecorder("R first\n")
	second := newReportRecorder("R second\n")
	h.s.Write(Info, rec(10, 'I'), first.fn)
	h.s.Write(Debug, rec(10, 'D'), second.fn)
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	if first.count() != 0 || second.count() != 1 {
		t.Error(test.DiffMessage([]int{first.count(), second.count()}, []int{0, 1}, "P10: the report is encoded by the logger whose record was dropped last"))
	}
	if second.all()[0] != [4]uint64{1, 1, 0, 0} {
		t.Error(test.DiffMessage(second.all()[0], [4]uint64{1, 1, 0, 0}, "P10: the counts of every logger are in the one report"))
	}
}

func TestSink_DropReportNotCalledWithoutDrops(t *testing.T) {
	h := newHeldSink(t)
	rep := newReportRecorder("R none\n")
	for i := range 20 {
		h.s.Write(Info, numbered(i, 100), rep.fn)
	}
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	if rep.count() != 0 || strings.Contains(h.w.output(), "R none") {
		t.Error(test.DiffMessage(rep.count(), 0, "P10: no report without a drop"))
	}
}

func TestSink_DropReportOncePerInterval(t *testing.T) {
	run := func(t *testing.T, advance bool) (snapshots []int, rep *reportRecorder) {
		t.Helper()
		h := newHeldSink(t)
		rep = newReportRecorder("R drops\n")
		var mu sync.Mutex
		h.w.onWrite = func(n int, p []byte) {
			mu.Lock()
			snapshots = append(snapshots, rep.count())
			mu.Unlock()
			if advance {
				h.clock.advance(2 * reportInterval)
			}
		}
		h.fillTo(Info, int(softLimit), 'a')
		h.s.Write(Info, rec(10, 'x'), rep.fn)
		h.w.openAll()
		flushOK(t, h.s, "P12: Flush returns true")
		mu.Lock()
		defer mu.Unlock()
		return append([]int(nil), snapshots...), rep
	}

	snaps, rep := run(t, false)
	if len(snaps) < 2 {
		t.Fatal(test.DiffMessage(len(snaps), ">= 2", "P10: several batches"))
	}
	for i, n := range snaps[:len(snaps)-1] {
		if n != 0 {
			t.Error(test.DiffMessage(n, 0, "P10: no report before reportInterval passed while the queue is not empty, write "+strconv.Itoa(i)))
		}
	}
	if rep.count() != 1 {
		t.Error(test.DiffMessage(rep.count(), 1, "P10: the report comes when the queue runs empty"))
	}

	snaps, rep = run(t, true)
	if len(snaps) < 3 || snaps[1] != 1 {
		t.Error(test.DiffMessage(snaps, "a report before the second write", "P10: the report is written once reportInterval passed, queue not empty"))
	}
	if rep.count() != 1 {
		t.Error(test.DiffMessage(rep.count(), 1, "P10: counts already reported are not reported again"))
	}
}

func TestSink_DropReportWhenIdle(t *testing.T) {
	h := newHeldSink(t)
	rep := newReportRecorder("R idle\n")
	h.fillTo(Info, int(softLimit), 'a')
	h.s.Write(Info, rec(10, 'x'), rep.fn)
	h.w.openAll()
	within(t, "P10: the writer reports when the queue runs empty", func() { <-rep.first })
	if got := rep.all()[0]; got != [4]uint64{0, 1, 0, 0} {
		t.Error(test.DiffMessage(got, [4]uint64{0, 1, 0, 0}, "P10: the idle report carries the count"))
	}
}

func TestSink_ReportPosition(t *testing.T) {
	h := newHeldSink(t)
	rep := newReportRecorder("R position\n")
	h.fillTo(Info, int(softLimit)-5, 'a')
	h.s.Write(Info, rec(10, 'x'), rep.fn)
	h.s.Write(Info, rec(5, 'b'), rep.fn)
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	out := h.w.output()
	if !strings.HasSuffix(out, "R position\n") || strings.Contains(out, "x") {
		t.Error(test.DiffMessage(out[len(out)-20:], "...R position", "O3: the report is written where the writer is, after the queued records, with no marker where the drop happened"))
	}
	if !strings.Contains(out, "bbbb\n") {
		t.Error(test.DiffMessage(false, true, "O3: records accepted after a drop are written"))
	}
}

func TestSink_DropAccounting(t *testing.T) {
	h := newHeldSink(t)
	fa := newFakeAfter(true)
	h.s.after = fa.after
	var mu sync.Mutex
	var reported [4]uint64
	report := func(d [4]uint64) []byte {
		mu.Lock()
		for i, n := range d {
			reported[i] += n
		}
		mu.Unlock()
		return []byte("R accounting\n")
	}
	var submitted [4]atomic.Int64
	letters := [4]byte{'D', 'I', 'W', 'E'}
	levels := [4]Level{Debug, Info, Warn, Error}
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 1500 {
				l := (g + i) % 4
				submitted[l].Add(1)
				h.s.Write(levels[l], rec(1000, letters[l]), report)
			}
		}()
	}
	within(t, "P7, P8: no caller blocks on a held output", wg.Wait)
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	out := h.w.output()
	mu.Lock()
	defer mu.Unlock()
	for l := range 4 {
		written := countLines(out, letters[l])
		if int64(written)+int64(reported[l]) != submitted[l].Load() {
			t.Error(test.DiffMessage(int64(written)+int64(reported[l]), submitted[l].Load(), "P10: records written plus counts reported equal records submitted, level "+string(letters[l])))
		}
	}
	if reported[0] == 0 || reported[1] == 0 {
		t.Error(test.DiffMessage(reported, "debug and info drops", "P7: 6 MB into a 1 MiB queue drops Debug and Info"))
	}
}

func TestSink_WriteErrorDiscardsBatch(t *testing.T) {
	h := newHeldSink(t)
	h.w.failOn[1] = errors.New("disk full")
	h.s.Write(Info, rec(50, 'a'), quietReport)
	<-h.w.entered
	h.s.Write(Info, rec(50, 'b'), quietReport)
	h.s.Write(Info, rec(50, 'c'), quietReport)
	h.w.openAll()
	flushOK(t, h.s, "P11: Flush returns true after a failed batch")
	if got, want := h.w.output(), string(rec(50, 'b'))+string(rec(50, 'c')); got != want {
		t.Error(test.DiffMessage(got, want, "P11: the failed batch is discarded and never retried, the next one is written"))
	}
	if n := len(h.w.sizes()); n != 2 {
		t.Error(test.DiffMessage(n, 2, "P11: one Write call per batch, no retry"))
	}
}

func TestSink_OutputPanicRecovered(t *testing.T) {
	h := newHeldSink(t)
	h.w.panicOn[1] = true
	h.s.Write(Info, rec(50, 'a'), quietReport)
	<-h.w.entered
	h.s.Write(Info, rec(50, 'b'), quietReport)
	h.w.openAll()
	flushOK(t, h.s, "P11: Flush returns true after the output panicked")
	if got, want := h.w.output(), string(rec(50, 'b')); got != want {
		t.Error(test.DiffMessage(got, want, "P11: a panic of the output is recovered and treated as an error"))
	}
	h.s.Write(Info, rec(50, 'c'), quietReport)
	flushOK(t, h.s, "P11: the sink still works")
	if !strings.HasSuffix(h.w.output(), string(rec(50, 'c'))) {
		t.Error(test.DiffMessage(h.w.output(), "ends with c", "P11: the writer goes on after a panic"))
	}
}

func TestSink_SyncOutputErrorAndPanicDoNotEscape(t *testing.T) {
	w := newGateWriter()
	w.failOn[1] = errors.New("broken pipe")
	w.panicOn[2] = true
	s := New(w)
	s.Write(Info, rec(20, 'a'), quietReport)
	s.Write(Info, rec(20, 'b'), quietReport)
	s.Write(Info, rec(20, 'c'), quietReport)
	if got, want := w.output(), string(rec(20, 'c')); got != want {
		t.Error(test.DiffMessage(got, want, "P11: in synchronous mode an error or a panic of the output never reaches the caller"))
	}
}

func TestSink_FlushWaitsForAccepted(t *testing.T) {
	h := newHeldSink(t)
	for i := range 10 {
		h.s.Write(Info, numbered(i, 100), quietReport)
	}
	if h.s.Flush(10 * time.Millisecond) {
		t.Error(test.DiffMessage(true, false, "P12: Flush returns false when the output is held"))
	}
	h.w.openAll()
	if !h.s.Flush(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, "P12: Flush returns true once the output is free"))
	}
	var want strings.Builder
	for i := range 10 {
		want.Write(numbered(i, 100))
	}
	if got := h.w.output(); got != want.String() {
		t.Error(test.DiffMessage(len(got), want.Len(), "P12: at return, everything accepted before the call is written, nothing was dropped by the timed-out Flush"))
	}
}

func TestSink_FlushEmptyReturnsTrue(t *testing.T) {
	s := New(newGateWriter())
	if !s.Flush(time.Millisecond) {
		t.Error(test.DiffMessage(false, true, "P12: Flush on a sink never used returns true"))
	}
	s.StartAsync()
	if !s.Flush(time.Millisecond) {
		t.Error(test.DiffMessage(false, true, "P12: Flush on an empty asynchronous sink returns true"))
	}
	s.StopAsync(time.Minute)
}

func TestSink_FlushReportsPendingDrops(t *testing.T) {
	h := newHeldSink(t)
	rep := newReportRecorder("R flush\n")
	h.fillTo(Info, int(softLimit), 'a')
	h.s.Write(Info, rec(10, 'x'), rep.fn)
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	if rep.count() != 1 || !strings.HasSuffix(h.w.output(), "R flush\n") {
		t.Error(test.DiffMessage(rep.count(), 1, "P12: Flush reports the pending drop counts before it returns"))
	}
}

func TestSink_StuckOutputBoundsCallersAndMemory(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()
	h := newHeldSink(t)
	fa := newFakeAfter(true)
	h.s.after = fa.after
	levels := []Level{Debug, Info, Warn, Error}
	letters := []byte{'D', 'I', 'W', 'E'}
	within(t, "P13: with the output blocked forever no caller blocks", func() {
		for i := range 10000 {
			l := i % 4
			h.s.Write(levels[l], rec(300, letters[l]), quietReport)
		}
	})
	if !goroutinesAtMost(base + 1) {
		t.Error(test.DiffMessage(runtime.NumGoroutine(), base+1, "P13: exactly one goroutine is blocked"))
	}
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	total := 0
	for _, line := range strings.SplitAfter(h.w.output(), "\n") {
		if len(line) > 0 && line[0] != 'R' {
			total += len(line)
		}
	}
	if total > int(queueLimit)+300 {
		t.Error(test.DiffMessage(total, "<= "+strconv.Itoa(int(queueLimit)+300), "P13: queued bytes never exceed queueLimit plus one record"))
	}
	if fa.calls.Load() > 2 {
		t.Error(test.DiffMessage(fa.calls.Load(), "<= 2", "P8: a stuck output costs the callers one wait"))
	}
}

func TestSink_AsyncCountedPerStart(t *testing.T) {
	w := newGateWriter()
	w.hold()
	s := New(w)
	s.StartAsync()
	s.StartAsync()
	within(t, "M3: asynchronous after two starts", func() { s.Write(Info, rec(20, 'a'), quietReport) })
	w.openAll()
	if !s.StopAsync(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, "M3: StopAsync flushes and returns true"))
	}
	if got := w.output(); got != string(rec(20, 'a')) {
		t.Error(test.DiffMessage(got, string(rec(20, 'a')), "M3: StopAsync flushes"))
	}
	w.hold()
	within(t, "M3: still asynchronous after one StopAsync of two StartAsync", func() { s.Write(Info, rec(20, 'b'), quietReport) })
	w.openAll()
	if !s.StopAsync(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, "M3: the second StopAsync flushes and returns true"))
	}
	for i := range 300 {
		s.Write(Info, numbered(i, 30), quietReport)
		if !strings.HasSuffix(w.output(), string(numbered(i, 30))) {
			t.Fatal(test.DiffMessage(w.output()[len(w.output())-30:], string(numbered(i, 30)), "M2, M3: after the last StopAsync the sink is synchronous"))
		}
	}
}

func TestSink_SameGoroutineOrder(t *testing.T) {
	h := newHeldSink(t)
	levels := []Level{Debug, Info, Warn, Error}
	const count = 5000
	for i := range count {
		h.s.Write(levels[i%4], numbered(i, 30), quietReport)
	}
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	lines := strings.Split(strings.TrimSuffix(h.w.output(), "\n"), "\n")
	if len(lines) != count {
		t.Fatal(test.DiffMessage(len(lines), count, "O1: nothing lost"))
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, fmt.Sprintf("%07d ", i)) {
			t.Fatal(test.DiffMessage(l, fmt.Sprintf("%07d ...", i), "O1: records of one goroutine are written in call order"))
		}
	}
}

func TestSink_HappensBeforeOrder(t *testing.T) {
	w := newGateWriter()
	s := New(w)
	s.StartAsync()
	defer s.StopAsync(time.Minute)
	ping, pong := make(chan int), make(chan int)
	const rounds = 500
	go func() {
		for i := range pong {
			s.Write(Info, numbered(2*i+1, 30), quietReport)
			ping <- i
		}
		close(ping)
	}()
	for i := range rounds {
		s.Write(Info, numbered(2*i, 30), quietReport)
		pong <- i
		<-ping
	}
	close(pong)
	flushOK(t, s, "P12: Flush returns true")
	lines := strings.Split(strings.TrimSuffix(w.output(), "\n"), "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, fmt.Sprintf("%07d ", i)) {
			t.Fatal(test.DiffMessage(l, fmt.Sprintf("%07d ...", i), "O2: a call that returned before another started is written first"))
		}
	}
}

func TestSink_ConcurrentWritersWholeRecords(t *testing.T) {
	h := newHeldSink(t)
	const goroutines, per = 16, 1000
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range per {
				h.s.Write(Info, numbered(g*per+i, 20+(g+i)%40), quietReport)
			}
		}()
	}
	wg.Wait()
	h.w.openAll()
	flushOK(t, h.s, "P12: Flush returns true")
	lines := strings.Split(strings.TrimSuffix(h.w.output(), "\n"), "\n")
	if len(lines) != goroutines*per {
		t.Fatal(test.DiffMessage(len(lines), goroutines*per, "O2: every record written"))
	}
	last := make([]int, goroutines)
	for i := range last {
		last[i] = -1
	}
	for _, l := range lines {
		n, err := strconv.Atoi(l[:7])
		if err != nil || !strings.HasPrefix(l[7:], " ") || strings.Trim(l[8:], ".") != "" {
			t.Fatal(test.DiffMessage(l, "an intact record", "P6: records are written whole, never interleaved"))
		}
		g, i := n/per, n%per
		if want := 20 + (g+i)%40 - 1; len(l) != want {
			t.Fatal(test.DiffMessage(len(l), want, "P6: a record keeps its length"))
		}
		if i <= last[g] {
			t.Fatal(test.DiffMessage(i, "> "+strconv.Itoa(last[g]), "O1: records of one goroutine stay in order"))
		}
		last[g] = i
	}
}

func TestStdout_IsOneSink(t *testing.T) {
	first, second := Stdout(), Stdout()
	if first != second {
		t.Error(test.DiffMessage(false, true, "P2: Stdout returns the process-wide sink"))
	}
	a, b := New(&syncBuffer{}), New(&syncBuffer{})
	if a == b {
		t.Error(test.DiffMessage(true, false, "New returns a private sink each time"))
	}
}

func TestSetStdout_RedirectsAndRestores(t *testing.T) {
	a, b := &syncBuffer{}, &syncBuffer{}
	restore := SetStdout(a)
	Stdout().Write(Info, rec(10, 'a'), quietReport)
	if a.String() != string(rec(10, 'a')) {
		t.Error(test.DiffMessage(a.String(), string(rec(10, 'a')), "SetStdout points the process-wide sink at the writer"))
	}
	restore()
	restoreB := SetStdout(b)
	defer restoreB()
	Stdout().Write(Info, rec(10, 'b'), quietReport)
	if a.String() != string(rec(10, 'a')) || b.String() != string(rec(10, 'b')) {
		t.Error(test.DiffMessage([]string{a.String(), b.String()}, []string{string(rec(10, 'a')), string(rec(10, 'b'))}, "SetStdout: restore undoes the redirection"))
	}
}

func TestSink_IdleAsyncHasNoWriterNoTimer(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()
	f := newFakedSink(t)
	for burst := range 3 {
		for i := range 20 {
			f.s.Write(Info, numbered(burst*20+i, 40), quietReport)
		}
		flushOK(t, f.s, "P12: Flush returns true")
		if !goroutinesAtMost(base) {
			t.Error(test.DiffMessage(runtime.NumGoroutine(), base, "W1: no writer goroutine after burst "+strconv.Itoa(burst)))
		}
		if f.ft.isArmed() {
			t.Error(test.DiffMessage(true, false, "W1: no armed timer after burst "+strconv.Itoa(burst)))
		}
		if got := snapValue(t, f.s, "writing"); got != false {
			t.Error(test.DiffMessage(got, false, "W1: Snapshot writing is false"))
		}
		if got := snapValue(t, f.s, "timer_armed"); got != false {
			t.Error(test.DiffMessage(got, false, "W1: Snapshot timer_armed is false"))
		}
	}
}

func TestSink_FirstRecordArmsTimerNoWrite(t *testing.T) {
	f := newFakedSink(t)
	f.s.Write(Info, rec(40, 'a'), quietReport)
	if got := f.w.sizes(); len(got) != 0 {
		t.Error(test.DiffMessage(got, []int{}, "W2: no output Write before the timer fires"))
	}
	if got := f.ft.delays(); len(got) != 1 || got[0] != time.Millisecond {
		t.Error(test.DiffMessage(got, []time.Duration{time.Millisecond}, "W2: the first record arms the timer once with 1 ms"))
	}
	if got := snapValue(t, f.s, "writing"); got != false {
		t.Error(test.DiffMessage(got, false, "W2: no writer runs before the fire"))
	}
	if got := snapValue(t, f.s, "timer_armed"); got != true {
		t.Error(test.DiffMessage(got, true, "W2: Snapshot reports the armed timer"))
	}
	f.ft.fire()
	if got := f.w.sizes(); len(got) != 1 || got[0] != 40 {
		t.Error(test.DiffMessage(got, []int{40}, "W2: the fire writes the record in one Write"))
	}
	if got := f.w.output(); got != string(rec(40, 'a')) {
		t.Error(test.DiffMessage(got, string(rec(40, 'a')), "W2: the fire writes the record"))
	}
}

func TestSink_FireWritesEveryQueuedRecordInOrder(t *testing.T) {
	f := newFakedSink(t)
	var want strings.Builder
	for i := range 50 {
		f.s.Write(Info, numbered(i, 30+i%7), quietReport)
		want.Write(numbered(i, 30+i%7))
	}
	f.ft.fire()
	if got := f.w.output(); got != want.String() {
		t.Error(test.DiffMessage(len(got), want.Len(), "W2: the fire writes every queued record in order"))
	}
	if got := f.w.sizes(); len(got) != 1 {
		t.Error(test.DiffMessage(len(got), 1, "W2: one Write holds every record accepted so far"))
	}
}

func TestSink_ArmedTimerNotResetByLaterRecords(t *testing.T) {
	f := newFakedSink(t)
	for i := range 10 {
		f.s.Write(Info, numbered(i, 40), quietReport)
	}
	if got := f.ft.armCount(); got != 1 {
		t.Error(test.DiffMessage(got, 1, "W3: the timer is armed once per burst"))
	}
	if got := f.w.sizes(); len(got) != 0 {
		t.Error(test.DiffMessage(got, []int{}, "W3: the deadline holds, nothing written before the fire"))
	}
}

func TestSink_BelowWakeSizeWaitsForTimer(t *testing.T) {
	f := newFakedSink(t)
	total := 0
	for range 15 {
		f.s.Write(Info, rec(1024, 'a'), quietReport)
		total += 1024
	}
	f.s.Write(Info, rec(wakeSize-1-total, 'b'), quietReport)
	if got := snapValue(t, f.s, "writing"); got != false {
		t.Error(test.DiffMessage(got, false, "W4: wakeSize-1 bytes start no writer"))
	}
	if got := snapValue(t, f.s, "wakes"); got != int64(0) {
		t.Error(test.DiffMessage(got, int64(0), "W4: wakeSize-1 bytes count no wake"))
	}
	if got := f.w.sizes(); len(got) != 0 {
		t.Error(test.DiffMessage(got, []int{}, "W4: no Write below wakeSize before a fire"))
	}
	f.ft.fire()
	if got := f.w.sizes(); len(got) != 1 || got[0] != wakeSize-1 {
		t.Error(test.DiffMessage(got, []int{wakeSize - 1}, "W4: the fire writes the queued bytes"))
	}
}

func TestSink_SizeStartsWriterAtWakeSize(t *testing.T) {
	f := newFakedSink(t)
	for range wakeSize / 1024 {
		f.s.Write(Info, rec(1024, 'a'), quietReport)
	}
	<-f.w.entered
	if got := f.w.sizes(); len(got) == 0 || got[0] != wakeSize {
		t.Error(test.DiffMessage(got, []int{wakeSize}, "W4: wakeSize bytes start the writer without a fire"))
	}
	flushOK(t, f.s, "P12: Flush returns true")
	if got := snapValue(t, f.s, "wakes"); got != int64(1) {
		t.Error(test.DiffMessage(got, int64(1), "W4, W13: the size wake is counted"))
	}
}

func TestSink_SizeStartsWriterWhileTimerArmed(t *testing.T) {
	f := newFakedSink(t)
	f.s.Write(Info, rec(100, 'a'), quietReport)
	if !f.ft.isArmed() {
		t.Fatal(test.DiffMessage(false, true, "W2: armed by the first record"))
	}
	f.s.Write(Info, rec(wakeSize, 'b'), quietReport)
	<-f.w.entered
	flushOK(t, f.s, "P12: Flush returns true")
	if got := f.w.output(); got != string(rec(100, 'a'))+string(rec(wakeSize, 'b')) {
		t.Error(test.DiffMessage(len(got), 100+wakeSize, "W4: the size rule starts the writer whether or not the timer is armed"))
	}
}

func TestSink_FatalStartsWriterAtOnce(t *testing.T) {
	f := newFakedSink(t)
	f.s.Write(Fatal, rec(30, 'F'), quietReport)
	<-f.w.entered
	flushOK(t, f.s, "P12: Flush returns true")
	if got := f.w.output(); got != string(rec(30, 'F')) {
		t.Error(test.DiffMessage(got, string(rec(30, 'F')), "W5: a Fatal record reaches the output without a fire"))
	}

	w := newGateWriter()
	s := New(w)
	ft := &fakeTimer{}
	s.newTimer = ft.new
	s.Write(Fatal, rec(30, 'G'), quietReport)
	<-w.entered
	flushOK(t, s, "P12: Flush returns true")
	if got := w.output(); got != string(rec(30, 'G')) {
		t.Error(test.DiffMessage(got, string(rec(30, 'G')), "W5: a Fatal record reaches the output in synchronous mode"))
	}
	if ft.wasCreated() {
		t.Error(test.DiffMessage(true, false, "W9: Fatal in synchronous mode arms no timer"))
	}
}

func TestSink_FlushWritesWithoutTimer(t *testing.T) {
	f := newFakedSink(t)
	var want strings.Builder
	for i := range 5 {
		f.s.Write(Info, numbered(i, 40), quietReport)
		want.Write(numbered(i, 40))
	}
	within(t, "W6: Flush starts the writer at once", func() {
		if !f.s.Flush(5 * time.Second) {
			t.Error(test.DiffMessage(false, true, "W6: Flush returns true without the timer firing"))
		}
	})
	if got := f.w.output(); got != want.String() {
		t.Error(test.DiffMessage(got, want.String(), "W6: Flush wrote everything"))
	}
}

func TestSink_FlushDrainsPastDelay(t *testing.T) {
	f := newFakedSink(t)
	f.w.hold()
	f.s.Write(Info, rec(40, 'a'), quietReport)
	result := make(chan bool, 1)
	go func() { result <- f.s.Flush(10 * time.Second) }()
	<-f.w.entered
	for i := range 3 {
		f.s.Write(Info, numbered(i, 40), quietReport)
	}
	f.w.openAll()
	if ok := <-result; !ok {
		t.Error(test.DiffMessage(false, true, "W6: Flush stays true while records arrive during its pass"))
	}
	want := string(rec(40, 'a')) + string(numbered(0, 40)) + string(numbered(1, 40)) + string(numbered(2, 40))
	if got := f.w.output(); got != want {
		t.Error(test.DiffMessage(got, want, "W6: the writer does not stop for the delay while a Flush waits"))
	}
}

func TestSink_StopAsyncWritesWithoutTimer(t *testing.T) {
	f := newFakedSink(t)
	f.s.Write(Info, rec(40, 'a'), quietReport)
	within(t, "W6: StopAsync flushes at once", func() {
		if !f.s.StopAsync(5 * time.Second) {
			t.Error(test.DiffMessage(false, true, "W6: StopAsync returns true without the timer firing"))
		}
	})
	if got := f.w.output(); got != string(rec(40, 'a')) {
		t.Error(test.DiffMessage(got, string(rec(40, 'a')), "W6: StopAsync wrote everything"))
	}
}

func TestSink_EndOfPassRearmsBelowWakeSize(t *testing.T) {
	f := newFakedSink(t)
	fired := heldPass(t, f)
	for i := range 3 {
		f.s.Write(Info, numbered(i, 40), quietReport)
	}
	f.w.openAll()
	<-fired
	if got := f.w.sizes(); len(got) != 1 {
		t.Error(test.DiffMessage(got, []int{40}, "W7: the writer ends without taking the 3 small records"))
	}
	if got := snapValue(t, f.s, "writing"); got != false {
		t.Error(test.DiffMessage(got, false, "W7: the writer ended"))
	}
	if !f.ft.isArmed() {
		t.Error(test.DiffMessage(false, true, "W7: the timer is armed for what is left"))
	}
	if got := f.ft.armCount(); got != 2 {
		t.Error(test.DiffMessage(got, 2, "W7: one arm for the burst, one at the end of the pass"))
	}
	f.ft.fire()
	want := string(numbered(0, 40)) + string(numbered(1, 40)) + string(numbered(2, 40))
	if got := f.w.sizes(); len(got) != 2 || got[1] != len(want) {
		t.Error(test.DiffMessage(got, []int{40, len(want)}, "W7: the next fire writes the 3 records in one Write"))
	}
}

func TestSink_EndOfPassContinuesAtWakeSize(t *testing.T) {
	f := newFakedSink(t)
	fired := heldPass(t, f)
	for range wakeSize / 1024 {
		f.s.Write(Info, rec(1024, 'b'), quietReport)
	}
	f.w.openAll()
	<-fired
	if got := f.w.sizes(); len(got) != 2 || got[1] != wakeSize {
		t.Error(test.DiffMessage(got, []int{40, wakeSize}, "W7: wakeSize bytes queued are written by the same writer at once"))
	}
	if got := snapValue(t, f.s, "writing"); got != false {
		t.Error(test.DiffMessage(got, false, "W7: the writer ended on an empty queue"))
	}
}

func TestSink_FireOnEmptyQueueDoesNothing(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()
	f := newFakedSink(t)
	for i := range 3 {
		f.s.Write(Info, numbered(i, 40), quietReport)
	}
	flushOK(t, f.s, "P12: Flush returns true")
	before := len(f.w.sizes())
	f.ft.fire()
	if got := len(f.w.sizes()); got != before {
		t.Error(test.DiffMessage(got, before, "W8: a fire on an empty queue writes nothing"))
	}
	if !goroutinesAtMost(base) {
		t.Error(test.DiffMessage(runtime.NumGoroutine(), base, "W8: no goroutine left behind"))
	}
	if got := snapValue(t, f.s, "wakes"); got != int64(1) {
		t.Error(test.DiffMessage(got, int64(1), "W8: the empty fire is not a wake"))
	}
}

func TestSink_FireWhileWritingDoesNothing(t *testing.T) {
	f := newFakedSink(t)
	f.w.hold()
	f.s.Write(Fatal, rec(30, 'F'), quietReport)
	<-f.w.entered
	f.s.Write(Info, rec(40, 'a'), quietReport)
	f.ft.fire()
	f.ft.fire()
	if got := f.w.sizes(); len(got) != 1 {
		t.Error(test.DiffMessage(len(got), 1, "W8: a fire while the writer runs starts no second Write"))
	}
	if got := f.w.maxConcurrent(); got != 1 {
		t.Error(test.DiffMessage(got, 1, "W8, P6: one writer at a time"))
	}
	f.w.openAll()
	flushOK(t, f.s, "P12: Flush returns true")
	if got := f.w.output(); got != string(rec(30, 'F'))+string(rec(40, 'a')) {
		t.Error(test.DiffMessage(got, string(rec(30, 'F'))+string(rec(40, 'a')), "W8: nothing lost"))
	}
	if got := f.w.maxConcurrent(); got != 1 {
		t.Error(test.DiffMessage(got, 1, "P6: one writer at a time"))
	}
}

func TestSink_StaleFireNoSecondWriter(t *testing.T) {
	f := newFakedSink(t)
	for i := range 3 {
		f.s.Write(Info, numbered(i, 40), quietReport)
	}
	f.ft.fire()
	f.ft.fire()
	if got := f.w.sizes(); len(got) != 1 {
		t.Error(test.DiffMessage(len(got), 1, "W8: two fires in a row write once"))
	}
	if got := f.w.maxConcurrent(); got != 1 {
		t.Error(test.DiffMessage(got, 1, "W8: never two writers"))
	}
	before := f.ft.armCount()
	f.s.Write(Info, rec(40, 'z'), quietReport)
	if got := f.ft.armCount(); got != before+1 {
		t.Error(test.DiffMessage(got, before+1, "W8: the next record still arms the timer"))
	}
	f.ft.fire()
	if got := f.w.sizes(); len(got) != 2 {
		t.Error(test.DiffMessage(len(got), 2, "W8: the next deadline is not lost"))
	}
}

func TestSink_SyncNeverArmsTimer(t *testing.T) {
	w := newGateWriter()
	s := New(w)
	ft := &fakeTimer{}
	s.newTimer = ft.new
	levels := []Level{Debug, Info, Warn, Error}
	for i := range 100 {
		s.Write(levels[i%4], numbered(i, 40), quietReport)
		if !strings.HasSuffix(w.output(), string(numbered(i, 40))) {
			t.Fatal(test.DiffMessage(w.output(), "the record just written", "W9: Write returns after the record was passed to the output"))
		}
	}
	if ft.wasCreated() || ft.armCount() != 0 || ft.stopCount() != 0 {
		t.Error(test.DiffMessage(ft.armCount(), 0, "W9: synchronous mode never creates, arms or stops the timer"))
	}
}

func TestSink_LastStopAsyncStopsTimer(t *testing.T) {
	f := newFakedSink(t)
	f.s.StartAsync()
	f.s.Write(Info, rec(40, 'a'), quietReport)
	if !f.s.StopAsync(5 * time.Second) {
		t.Fatal(test.DiffMessage(false, true, "M3: StopAsync flushes and returns true"))
	}
	if got := snapValue(t, f.s, "async"); got != 1 {
		t.Error(test.DiffMessage(got, 1, "W10: the first StopAsync of two keeps the sink asynchronous"))
	}
	f.s.Write(Info, rec(40, 'b'), quietReport)
	if !f.s.StopAsync(5 * time.Second) {
		t.Fatal(test.DiffMessage(false, true, "M3: the last StopAsync flushes and returns true"))
	}
	if f.ft.stopCount() < 1 {
		t.Error(test.DiffMessage(f.ft.stopCount(), ">= 1", "W10: the last StopAsync stops the timer"))
	}
	if f.ft.isArmed() {
		t.Error(test.DiffMessage(true, false, "W10: the timer is not armed after the last StopAsync"))
	}
	if got := snapValue(t, f.s, "timer_armed"); got != false {
		t.Error(test.DiffMessage(got, false, "W10: Snapshot timer_armed is false"))
	}
	if got := snapValue(t, f.s, "async"); got != 0 {
		t.Error(test.DiffMessage(got, 0, "M2: the sink is synchronous"))
	}
}

func TestSink_StopAsyncWritesRecordAcceptedDuringFlush(t *testing.T) {
	f := newFakedSink(t)
	f.w.hold()
	var once sync.Once
	f.w.onWrite = func(int, []byte) {
		once.Do(func() { f.s.Write(Info, rec(50, 'b'), quietReport) })
	}
	f.s.Write(Info, rec(40, 'a'), quietReport)
	result := make(chan bool, 1)
	go func() { result <- f.s.StopAsync(10 * time.Second) }()
	<-f.w.entered
	f.w.openAll()
	if ok := <-result; !ok {
		t.Fatal(test.DiffMessage(false, true, "M2: StopAsync returns true"))
	}
	if got := f.w.output(); got != string(rec(40, 'a'))+string(rec(50, 'b')) {
		t.Error(test.DiffMessage(got, string(rec(40, 'a'))+string(rec(50, 'b')), "W10: StopAsync returns after the record accepted during its flush is written (a clean shutdown loses nothing)"))
	}
	within(t, "W10: Flush returns at once", func() {
		if !f.s.Flush(5 * time.Second) {
			t.Error(test.DiffMessage(false, true, "W10: Flush returns true"))
		}
	})
	if got := f.w.output(); got != string(rec(40, 'a'))+string(rec(50, 'b')) {
		t.Error(test.DiffMessage(got, string(rec(40, 'a'))+string(rec(50, 'b')), "W10: the record accepted during the flush is written"))
	}
	if f.ft.stopCount() < 1 || f.ft.isArmed() {
		t.Error(test.DiffMessage(f.ft.isArmed(), false, "W10: the timer was stopped"))
	}
}

func TestSetStdout_StopsTimer(t *testing.T) {
	s := Stdout()
	ft := &fakeTimer{}
	oldNew := s.newTimer
	s.newTimer = ft.new
	a, b := &syncBuffer{}, &syncBuffer{}
	restoreA := SetStdout(a)
	defer func() {
		restoreA()
		s.mu.Lock()
		s.newTimer = oldNew
		s.timer = nil
		s.armed = false
		s.mu.Unlock()
	}()
	s.StartAsync()
	s.Write(Info, rec(40, 'a'), quietReport)
	if !ft.isArmed() {
		t.Fatal(test.DiffMessage(false, true, "W2: the record armed the timer"))
	}
	restoreB := SetStdout(b)
	if a.String() != string(rec(40, 'a')) {
		t.Error(test.DiffMessage(a.String(), string(rec(40, 'a')), "W10: SetStdout flushes the queue"))
	}
	if ft.stopCount() < 1 || ft.isArmed() {
		t.Error(test.DiffMessage(ft.isArmed(), false, "W10: SetStdout stops the timer"))
	}
	if got := snapValue(t, s, "timer_armed"); got != false {
		t.Error(test.DiffMessage(got, false, "W10: Snapshot timer_armed is false"))
	}
	restoreB()
	s.StopAsync(time.Minute)
}

func TestSink_RealTimerWritesWithoutFlush(t *testing.T) {
	w := newGateWriter()
	s := New(w)
	s.StartAsync()
	defer s.StopAsync(time.Minute)
	s.Write(Info, rec(40, 'a'), quietReport)
	select {
	case <-w.entered:
	case <-time.After(2 * time.Second):
		t.Fatal(test.DiffMessage("no Write within 2s", "a Write", "W11: one Info record reaches the output without a Flush"))
	}
	if got := w.sizes(); len(got) != 1 || got[0] != 40 {
		t.Error(test.DiffMessage(got, []int{40}, "W11: the record was written"))
	}
}

func TestSink_WriteWhileArmedAllocatesNothing(t *testing.T) {
	f := newFakedSink(t)
	record := rec(100, 'a')
	for range 2 {
		for range 200 {
			f.s.Write(Info, record, quietReport)
		}
		f.ft.fire()
	}
	f.s.Write(Info, record, quietReport)
	allocs := testing.AllocsPerRun(50, func() { f.s.Write(Info, record, quietReport) })
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "W12: a record accepted while the timer is armed allocates nothing"))
	}
}

func TestSink_Snapshot_Fields(t *testing.T) {
	var _ inspect.Snapshotter = (*Sink)(nil)
	s := New(newGateWriter())
	s.StartAsync()
	want := inspect.Snapshot{Name: "logpipe", Fields: []inspect.Field{
		{Key: "wake_delay", Value: time.Millisecond},
		{Key: "wake_size", Value: 16 << 10},
		{Key: "batch_size", Value: 64 << 10},
		{Key: "queue_limit", Value: 1 << 20},
		{Key: "soft_limit", Value: (1 << 20) * 8 / 10},
		{Key: "async", Value: 1},
		{Key: "queued", Value: int64(0)},
		{Key: "writing", Value: false},
		{Key: "timer_armed", Value: false},
		{Key: "wakes", Value: int64(0)},
		{Key: "writes", Value: int64(0)},
		{Key: "dropped_debug", Value: int64(0)},
		{Key: "dropped_info", Value: int64(0)},
		{Key: "dropped_warn", Value: int64(0)},
		{Key: "dropped_error", Value: int64(0)},
	}}
	if got := s.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "W13: Snapshot keys, order, types and values"))
	}
	if wakeDelay != time.Millisecond || wakeSize != 16<<10 {
		t.Error(test.DiffMessage([]any{wakeDelay, wakeSize}, []any{time.Millisecond, 16 << 10}, "Open question 1: 1 ms and 16 KiB"))
	}
}

func TestSink_Snapshot_CountsWakesAndWrites(t *testing.T) {
	f := newFakedSink(t)
	for i := range 10 {
		f.s.Write(Info, numbered(i, 40), quietReport)
	}
	f.ft.fire()
	if got := snapValue(t, f.s, "wakes"); got != int64(1) {
		t.Error(test.DiffMessage(got, int64(1), "W13: one wake after one fire"))
	}
	if got := snapValue(t, f.s, "writes"); got != int64(1) {
		t.Error(test.DiffMessage(got, int64(1), "W13: one output Write after one fire"))
	}
	f.s.Write(Info, rec(40, 'a'), quietReport)
	flushOK(t, f.s, "P12: Flush returns true")
	if got := snapValue(t, f.s, "wakes"); got != int64(2) {
		t.Error(test.DiffMessage(got, int64(2), "W13: a Flush wake is counted"))
	}
	if got := snapValue(t, f.s, "writes"); got != int64(2) {
		t.Error(test.DiffMessage(got, int64(2), "W13: the Flush Write is counted"))
	}
}

func TestSink_Snapshot_ConcurrentWithWrites(t *testing.T) {
	s := New(&syncBuffer{})
	s.StartAsync()
	var wg sync.WaitGroup
	var stop atomic.Bool
	snapped := make(chan struct{})
	go func() {
		defer close(snapped)
		for !stop.Load() {
			snap := s.Snapshot()
			if snap.Name != "logpipe" || len(snap.Fields) != 15 {
				t.Error(test.DiffMessage(len(snap.Fields), 15, "W13: a complete snapshot at any time"))
				return
			}
			runtime.Gosched()
		}
	}()
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 500 {
				s.Write(Info, numbered(g*500+i, 40), quietReport)
			}
		}()
	}
	wg.Wait()
	flushOK(t, s, "P12: Flush returns true")
	stop.Store(true)
	<-snapped
	if got := snapValue(t, s, "queued"); got != int64(0) {
		t.Error(test.DiffMessage(got, int64(0), "W13: nothing queued after Flush"))
	}
	if got := snapValue(t, s, "writes"); got.(int64) < 1 {
		t.Error(test.DiffMessage(got, ">= 1", "W13: writes counted"))
	}
	s.StopAsync(time.Minute)
}

func TestSink_StuckOutputFireAddsNoGoroutine(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()
	h := newHeldSink(t)
	ft := &fakeTimer{}
	h.s.newTimer = ft.new
	h.s.Write(Fatal, rec(30, 'F'), quietReport)
	<-h.w.entered
	for i := range 3 {
		h.s.Write(Info, numbered(i, 40), quietReport)
	}
	for range 5 {
		ft.fire()
	}
	if !goroutinesAtMost(base + 1) {
		t.Error(test.DiffMessage(runtime.NumGoroutine(), base+1, "P13: fires add no goroutine to the blocked writer"))
	}
	if got := h.w.maxConcurrent(); got != 1 {
		t.Error(test.DiffMessage(got, 1, "P13: one writer at a time"))
	}
}

func TestSink_RandomFiresNoLossNoReorder(t *testing.T) {
	f := newFakedSink(t)
	const goroutines, per = 16, 1000
	var stop atomic.Bool
	fired := make(chan struct{})
	go func() {
		defer close(fired)
		r := rand.New(rand.NewPCG(1, 2))
		for !stop.Load() {
			for range r.IntN(4) {
				runtime.Gosched()
			}
			f.ft.fire()
		}
	}()
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range per {
				f.s.Write(Info, numbered(g*per+i, 20+(g+i)%40), quietReport)
			}
		}()
	}
	wg.Wait()
	stop.Store(true)
	<-fired
	flushOK(t, f.s, "P12: Flush returns true")
	lines := strings.Split(strings.TrimSuffix(f.w.output(), "\n"), "\n")
	if len(lines) != goroutines*per {
		t.Fatal(test.DiffMessage(len(lines), goroutines*per, "O2: every record written once"))
	}
	last := make([]int, goroutines)
	for i := range last {
		last[i] = -1
	}
	for _, l := range lines {
		n, err := strconv.Atoi(l[:7])
		if err != nil {
			t.Fatal(test.DiffMessage(l, "an intact record", "P6: records are written whole"))
		}
		g, i := n/per, n%per
		if want := 20 + (g+i)%40 - 1; len(l) != want {
			t.Fatal(test.DiffMessage(len(l), want, "P6: a record keeps its length"))
		}
		if i <= last[g] {
			t.Fatal(test.DiffMessage(i, "> "+strconv.Itoa(last[g]), "O1: records of one goroutine stay in order"))
		}
		last[g] = i
	}
}

func TestSink_StopAsyncWaitsForLeftoverWrite(t *testing.T) {
	f := newFakedSink(t)
	f.w.hold()
	var once sync.Once
	f.w.onWrite = func(int, []byte) {
		once.Do(func() { f.s.Write(Info, rec(50, 'b'), quietReport) })
	}
	f.s.Write(Info, rec(40, 'a'), quietReport)
	result := make(chan bool, 1)
	go func() { result <- f.s.StopAsync(10 * time.Second) }()
	<-f.w.entered
	f.w.release() // a is written; b, accepted during that write, is held at the output
	<-f.w.entered
	select {
	case <-result:
		t.Fatal(test.DiffMessage("returned", "waiting", "W10: StopAsync waits while the record accepted during its flush is still being written"))
	case <-time.After(50 * time.Millisecond):
	}
	f.w.release()
	if ok := <-result; !ok {
		t.Error(test.DiffMessage(false, true, "W10: StopAsync returns true once everything is written"))
	}
	if got := f.w.output(); got != string(rec(40, 'a'))+string(rec(50, 'b')) {
		t.Error(test.DiffMessage(got, string(rec(40, 'a'))+string(rec(50, 'b')), "W10: both records are written before StopAsync returns"))
	}
}
