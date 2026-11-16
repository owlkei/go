package logpipe

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
