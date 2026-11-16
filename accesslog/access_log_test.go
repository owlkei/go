package accesslog

import (
	"fmt"
	"maps"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/trace"
)

type recordedLog struct {
	msg  string
	args []any
}

type fakeLogger struct {
	mu     sync.Mutex
	calls  int
	infos  []recordedLog
	warns  []recordedLog
	onInfo func(n int)
}

func newFakeLogger() *fakeLogger {
	return &fakeLogger{}
}

func (f *fakeLogger) Debug(msg string, args ...any) {}
func (f *fakeLogger) Fatal(msg string, args ...any) {}
func (f *fakeLogger) Error(msg string, args ...any) {}

func (f *fakeLogger) Info(msg string, args ...any) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	hook := f.onInfo
	f.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	f.mu.Lock()
	f.infos = append(f.infos, recordedLog{msg: msg, args: slices.Clone(args)})
	f.mu.Unlock()
}

func (f *fakeLogger) Warn(msg string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warns = append(f.warns, recordedLog{msg: msg, args: slices.Clone(args)})
}

func (f *fakeLogger) lines() []recordedLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.infos)
}

func (f *fakeLogger) dropped(t *testing.T) int {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for _, w := range f.warns {
		if w.msg != "AccessLog" || len(w.args) != 2 || w.args[0] != "dropped" {
			t.Errorf("A8: unexpected warning %+v", w)
			continue
		}
		n, err := strconv.Atoi(fmt.Sprint(w.args[1]))
		if err != nil {
			t.Errorf("A8: dropped count is not a number: %v", w.args[1])
		}
		total += n
	}
	return total
}

func setup(t *testing.T) (*trace.Tracer, *fakeLogger, *AccessLog) {
	t.Helper()
	tr := &trace.Tracer{}
	logger := newFakeLogger()
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	t.Cleanup(al.Stop)
	return tr, logger, al
}

func stageEvent(id, stage, name string, d time.Duration) trace.Event {
	return trace.Event{ID: id, Stage: stage, Name: name, Transport: trace.TransportHTTP, Duration: d}
}

func completeEvent(id string, code int, target string, d time.Duration) trace.Event {
	return trace.Event{ID: id, Stage: trace.StageComplete, Transport: trace.TransportHTTP, Operation: "GET", Target: target, Code: code, Duration: d}
}

func argValue(t *testing.T, args []any, key string) any {
	t.Helper()
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key {
			return args[i+1]
		}
	}
	t.Fatalf("argument %q not found in %v", key, args)
	return nil
}

func hasArg(args []any, key string) bool {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key {
			return true
		}
	}
	return false
}

func stageOf(t *testing.T, args []any, stage string) []map[string]color.Colored {
	t.Helper()
	list, ok := argValue(t, args, stage).([]map[string]color.Colored)
	if !ok {
		t.Fatalf("stage %q is not a []map[string]color.Colored: %T", stage, argValue(t, args, stage))
	}
	return list
}

func entryTexts(list []map[string]color.Colored) []string {
	out := make([]string, len(list))
	for i, entry := range list {
		for name, c := range entry {
			out[i] = name + "=" + c.Text
		}
	}
	return out
}

func percentsOf(t *testing.T, durations ...time.Duration) []int {
	t.Helper()
	tr, logger, al := setup(t)
	for i, d := range durations {
		tr.Emit(stageEvent("p", trace.StageMiddleware, "M"+strconv.Itoa(i), d))
	}
	tr.Emit(completeEvent("p", 200, "/", time.Second))
	al.Stop()
	lines := logger.lines()
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d", len(lines))
	}
	var out []int
	for _, text := range entryTexts(stageOf(t, lines[0].args, trace.StageMiddleware)) {
		open := strings.IndexByte(text, '(')
		closeIdx := strings.IndexByte(text, '%')
		p, err := strconv.Atoi(text[open+1 : closeIdx])
		if err != nil {
			t.Fatalf("cannot read a percent from %q", text)
		}
		out = append(out, p)
	}
	return out
}

func sum(vals []int) int {
	total := 0
	for _, v := range vals {
		total += v
	}
	return total
}

func TestAccessLog_LineArgumentsInOrder(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(stageEvent("req-1", trace.StageMiddleware, "M1", time.Millisecond))
	tr.Emit(stageEvent("req-1", trace.StageGuard, "G1", 2*time.Millisecond))
	tr.Emit(stageEvent("req-1", trace.StageHandler, "H1", 3*time.Millisecond))
	tr.Emit(completeEvent("req-1", 200, "/users", 10*time.Millisecond))
	al.Stop()

	lines := logger.lines()
	if len(lines) != 1 {
		t.Fatal(test.DiffMessage(len(lines), 1, "A1: one Logger.Info per complete event"))
	}
	got := lines[0]
	if got.msg != "/users" {
		t.Error(test.DiffMessage(got.msg, "/users", "A1: the message is the Target"))
	}
	keys := []string{}
	for i := 0; i < len(got.args); i += 2 {
		keys = append(keys, got.args[i].(string))
	}
	wantKeys := []string{"id", "transport", "code", "operation", "middleware", "guard", "handler", "total"}
	if !slices.Equal(keys, wantKeys) {
		t.Error(test.DiffMessage(keys, wantKeys, "A1: the argument keys in order"))
	}
	if got.args[1] != "req-1" || got.args[3] != trace.TransportHTTP || got.args[5] != 200 || got.args[7] != "GET" {
		t.Error(test.DiffMessage(got.args[:8], []any{"id", "req-1", "transport", "HTTP", "code", 200, "operation", "GET"}, "A1: the fixed pairs, code is an int"))
	}
	wantMiddleware := []map[string]color.Colored{{"M1": {Text: "1ms (17%)", Color: colorForDuration(time.Millisecond)}}}
	if !slices.EqualFunc(stageOf(t, got.args, trace.StageMiddleware), wantMiddleware, maps.Equal) {
		t.Error(test.DiffMessage(stageOf(t, got.args, trace.StageMiddleware), wantMiddleware, "A2: a stage is a list of one-entry maps"))
	}
	if got.args[len(got.args)-1] != "10ms" {
		t.Error(test.DiffMessage(got.args[len(got.args)-1], "10ms", "A3: total is the duration of the complete event"))
	}
}

func TestAccessLog_OneLinePerCompleteEvent(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(stageEvent("a", trace.StageMiddleware, "M", 0))
	tr.Emit(completeEvent("a", 200, "/a", 0))
	tr.Emit(completeEvent("b", 404, "/b", 0))
	al.Stop()
	if lines := logger.lines(); len(lines) != 2 || lines[0].msg != "/a" || lines[1].msg != "/b" {
		t.Error(test.DiffMessage(logger.lines(), "two lines /a and /b", "A1: one call per complete event, in order"))
	}
}

func TestAccessLog_LoggerCalledOffTheEmittingGoroutine(t *testing.T) {
	tr := &trace.Tracer{}
	logger, gate, entered := blockedLogger()
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	defer al.Stop()
	defer close(gate)
	emitWithin(t, "A1: the logger is called off the emitting goroutine", func() {
		tr.Emit(completeEvent("x", 200, "/", 0))
	})
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal(test.DiffMessage("the logger was never called", "called", "A1: the worker calls the logger"))
	}
}

func TestAccessLog_StagesInArrivalOrder(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(stageEvent("r", trace.StageHandler, "H", 0))
	tr.Emit(stageEvent("r", trace.StageMiddleware, "A", 0))
	tr.Emit(stageEvent("r", trace.StageGuard, "G", 0))
	tr.Emit(stageEvent("r", trace.StageMiddleware, "B", 0))
	tr.Emit(completeEvent("r", 200, "/", 0))
	al.Stop()

	args := logger.lines()[0].args
	var keys []string
	for i := 8; i < len(args)-2; i += 2 {
		keys = append(keys, args[i].(string))
	}
	want := []string{trace.StageHandler, trace.StageMiddleware, trace.StageGuard}
	if !slices.Equal(keys, want) {
		t.Error(test.DiffMessage(keys, want, "A2: stages appear in the order their first event arrived"))
	}
	if got := entryTexts(stageOf(t, args, trace.StageMiddleware)); len(got) != 2 || !strings.HasPrefix(got[0], "A=") || !strings.HasPrefix(got[1], "B=") {
		t.Error(test.DiffMessage(got, []string{"A=…", "B=…"}, "A2: events of one stage are in arrival order"))
	}
}

func TestAccessLog_PreservesExecutionOrderNotAlphabetical(t *testing.T) {
	tr, logger, al := setup(t)
	for _, n := range []string{"C", "B", "A"} {
		tr.Emit(stageEvent("r", trace.StagePreInterceptor, n, 0))
	}
	for _, n := range []string{"A", "B", "C"} {
		tr.Emit(stageEvent("r", trace.StagePostInterceptor, n, 0))
	}
	tr.Emit(completeEvent("r", 200, "/", 0))
	al.Stop()

	args := logger.lines()[0].args
	names := func(stage string) []string {
		var out []string
		for _, entry := range stageOf(t, args, stage) {
			for name := range entry {
				out = append(out, name)
			}
		}
		return out
	}
	if got := names(trace.StagePreInterceptor); !slices.Equal(got, []string{"C", "B", "A"}) {
		t.Error(test.DiffMessage(got, []string{"C", "B", "A"}, "A2: pre-interceptor entries in emission order"))
	}
	if got := names(trace.StagePostInterceptor); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Error(test.DiffMessage(got, []string{"A", "B", "C"}, "A2: post-interceptor entries in emission order"))
	}
}

func TestAccessLog_ShortName(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(stageEvent("r", trace.StageMiddleware, "cors.CORS", 0))
	tr.Emit(stageEvent("r", trace.StageMiddleware, "NoPackage", 0))
	tr.Emit(completeEvent("r", 200, "/", 0))
	al.Stop()
	got := entryTexts(stageOf(t, logger.lines()[0].args, trace.StageMiddleware))
	if len(got) != 2 || got[0] != "CORS=0s (0%)" || got[1] != "NoPackage=0s (0%)" {
		t.Error(test.DiffMessage(got, []string{"CORS=0s (0%)", "NoPackage=0s (0%)"}, "A2: the map key is the Name after its last dot"))
	}
}

func TestAccessLog_TotalIsCompleteDuration(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(stageEvent("r", trace.StageMiddleware, "M1", time.Millisecond))
	tr.Emit(completeEvent("r", 200, "/", 250*time.Millisecond))
	al.Stop()
	args := logger.lines()[0].args
	if args[len(args)-1] != "250ms" {
		t.Error(test.DiffMessage(args[len(args)-1], "250ms", "A3: total is the wall-clock duration of the complete event, not the sum of the stages"))
	}
}

func TestAccessLog_NoStagesLogsFixedPairsAndTotal(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(completeEvent("r", 404, "/nope", 1500*time.Microsecond))
	al.Stop()
	args := logger.lines()[0].args
	want := []any{"id", "r", "transport", "HTTP", "code", 404, "operation", "GET", "total", "1.5ms"}
	if !slices.Equal(args, want) {
		t.Error(test.DiffMessage(args, want, "A3: a request without stages logs the four pairs and total"))
	}
}

func TestAccessLog_LongTargetIsCut(t *testing.T) {
	tr, logger, al := setup(t)
	target := "/" + strings.Repeat("a", 2999)
	tr.Emit(completeEvent("r", 200, target, 0))
	al.Stop()
	msg := logger.lines()[0].msg
	if msg != target[:2048]+"..." {
		t.Error(test.DiffMessage(len(msg), 2051, "A1: a long target is cut to 2,048 bytes followed by ..."))
	}
}

func TestAccessLog_TargetOfExactLimitIsKept(t *testing.T) {
	tr, logger, al := setup(t)
	target := "/" + strings.Repeat("a", 2047)
	tr.Emit(completeEvent("r", 200, target, 0))
	al.Stop()
	if msg := logger.lines()[0].msg; msg != target {
		t.Error(test.DiffMessage(len(msg), 2048, "A1: a target of 2,048 bytes is not cut"))
	}
}

func TestAccessLog_TargetCutOnRuneBoundary(t *testing.T) {
	tr, logger, al := setup(t)
	target := "a" + strings.Repeat("é", 1500)
	tr.Emit(completeEvent("r", 200, target, 0))
	al.Stop()
	msg := logger.lines()[0].msg
	if !utf8.ValidString(msg) || !strings.HasSuffix(msg, "...") {
		t.Error(test.DiffMessage(msg[len(msg)-8:], "valid UTF-8 ending in ...", "A1: the cut falls on a rune boundary"))
	}
	if len(msg) != 2047+3 {
		t.Error(test.DiffMessage(len(msg), 2050, "A1: the cut keeps whole runes within 2,048 bytes"))
	}
}

func TestAccessLog_PercentagesSumToOneHundred(t *testing.T) {
	got := percentsOf(t, time.Millisecond, time.Millisecond, time.Millisecond)
	if sum(got) != 100 {
		t.Error(test.DiffMessage(sum(got), 100, "A2: percentages sum to exactly 100"))
	}
}

func TestAccessLog_PercentagesLargestRemainderFirst(t *testing.T) {
	got := percentsOf(t, 50, 30, 21)
	if want := []int{49, 30, 21}; !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "A2: the largest remainder receives the extra point first"))
	}
}

func TestAccessLog_PercentagesTiesBrokenByOrder(t *testing.T) {
	got := percentsOf(t, 1, 1, 1)
	if want := []int{34, 33, 33}; !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "A2: tied remainders go to the earlier entry"))
	}
}

func TestAccessLog_PercentagesZeroSum(t *testing.T) {
	got := percentsOf(t, 0, 0)
	if want := []int{0, 0}; !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "A2: all 0 when the sum is 0"))
	}
}

func TestAccessLog_PercentagesAreSharesOfTheStagesNotOfTotal(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(stageEvent("r", trace.StageMiddleware, "M1", 10*time.Millisecond))
	tr.Emit(stageEvent("r", trace.StageHandler, "H1", 90*time.Millisecond))
	tr.Emit(completeEvent("r", 200, "/", time.Second))
	al.Stop()
	args := logger.lines()[0].args
	if got := entryTexts(stageOf(t, args, trace.StageMiddleware)); got[0] != "M1=10ms (10%)" {
		t.Error(test.DiffMessage(got, "M1=10ms (10%)", "A2: a percent is the share of the sum of the stage durations"))
	}
}

func TestAccessLog_SeparatesInterleavedIDs(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(stageEvent("a", trace.StageMiddleware, "A1", 0))
	tr.Emit(stageEvent("b", trace.StageMiddleware, "B1", 0))
	tr.Emit(stageEvent("a", trace.StageGuard, "A2", 0))
	tr.Emit(stageEvent("b", trace.StageGuard, "B2", 0))
	tr.Emit(completeEvent("b", 200, "/b", 0))
	tr.Emit(completeEvent("a", 200, "/a", 0))
	al.Stop()
	lines := logger.lines()
	if len(lines) != 2 {
		t.Fatal(test.DiffMessage(len(lines), 2, "A5: one line per request"))
	}
	for _, line := range lines {
		id := strings.TrimPrefix(line.msg, "/")
		for _, stage := range []string{trace.StageMiddleware, trace.StageGuard} {
			for _, text := range entryTexts(stageOf(t, line.args, stage)) {
				if !strings.HasPrefix(strings.ToLower(text), id) {
					t.Error(test.DiffMessage(text, "an entry of "+id, "A5: events of different IDs never mix"))
				}
			}
		}
	}
}

func TestAccessLog_ConcurrentRequests_NoRace(t *testing.T) {
	tr, logger, al := setup(t)
	const goroutines = 8
	const perGoroutine = 500
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perGoroutine {
				id := fmt.Sprintf("g%d-r%d", g, i)
				tr.Emit(stageEvent(id, trace.StageMiddleware, "m."+id, time.Microsecond))
				tr.Emit(stageEvent(id, trace.StageHandler, "h."+id, time.Microsecond))
				tr.Emit(completeEvent(id, 200, "/"+id, time.Millisecond))
			}
		}()
	}
	wg.Wait()
	al.Stop()

	lines := logger.lines()
	for _, line := range lines {
		id := strings.TrimPrefix(line.msg, "/")
		if argValue(t, line.args, "id") != id {
			t.Fatal(test.DiffMessage(argValue(t, line.args, "id"), id, "A5: the line carries its own ID"))
		}
		for _, stage := range []string{trace.StageMiddleware, trace.StageHandler} {
			list := stageOf(t, line.args, stage)
			if len(list) != 1 {
				t.Fatal(test.DiffMessage(len(list), 1, "A5: each line has its own stages only"))
			}
			for name := range list[0] {
				if name != id {
					t.Fatal(test.DiffMessage(name, "an entry of "+id, "A5: no stage of another request"))
				}
			}
		}
	}
	if total := len(lines) + logger.dropped(t); total != goroutines*perGoroutine {
		t.Error(test.DiffMessage(total, goroutines*perGoroutine, "A8: lines logged plus lines reported as dropped equal the complete events"))
	}
}

func TestAccessLog_StagesCappedAt64(t *testing.T) {
	if maxStagesPerRequest != 64 {
		t.Fatal(test.DiffMessage(maxStagesPerRequest, 64, "A6: maxStagesPerRequest"))
	}
	tr, logger, al := setup(t)
	for i := range 100 {
		tr.Emit(stageEvent("r", trace.StageMiddleware, "M"+strconv.Itoa(i), 0))
	}
	tr.Emit(completeEvent("r", 200, "/", 0))
	al.Stop()
	args := logger.lines()[0].args
	if n := len(stageOf(t, args, trace.StageMiddleware)); n != 64 {
		t.Error(test.DiffMessage(n, 64, "A6: a request keeps at most 64 stage events"))
	}
	if args[len(args)-4] != "stages_dropped" || args[len(args)-3] != 36 || args[len(args)-2] != "total" {
		t.Error(test.DiffMessage(args[len(args)-4:], []any{"stages_dropped", 36, "total", "…"}, "A6: stages_dropped, n goes before total"))
	}
}

func TestAccessLog_StagesCapKeepsTheFirst(t *testing.T) {
	tr, logger, al := setup(t)
	for i := range 70 {
		tr.Emit(stageEvent("r", trace.StageMiddleware, "M"+strconv.Itoa(i), 0))
	}
	tr.Emit(completeEvent("r", 200, "/", 0))
	al.Stop()
	list := entryTexts(stageOf(t, logger.lines()[0].args, trace.StageMiddleware))
	if !strings.HasPrefix(list[0], "M0=") || !strings.HasPrefix(list[63], "M63=") {
		t.Error(test.DiffMessage([]string{list[0], list[63]}, []string{"M0=…", "M63=…"}, "A6: the first 64 events are the ones kept"))
	}
}

func TestAccessLog_ExactlySixtyFourStagesHasNoDropCount(t *testing.T) {
	tr, logger, al := setup(t)
	for i := range 64 {
		tr.Emit(stageEvent("r", trace.StageMiddleware, "M"+strconv.Itoa(i), 0))
	}
	tr.Emit(completeEvent("r", 200, "/", 0))
	al.Stop()
	if hasArg(logger.lines()[0].args, "stages_dropped") {
		t.Error(test.DiffMessage(logger.lines()[0].args, "no stages_dropped", "A6: nothing dropped, nothing reported"))
	}
}

func spreadID(i int) string {
	return fmt.Sprintf("%016x-%04x", uint64(i+1)*0x9E3779B97F4A7C15, i&0xffff)
}

func TestAccessLog_PendingIsBounded(t *testing.T) {
	if maxPending != 16384 {
		t.Fatal(test.DiffMessage(maxPending, 16384, "A7: maxPending"))
	}
	tr, _, al := setup(t)
	for i := range 100000 {
		tr.Emit(stageEvent(spreadID(i), trace.StageMiddleware, "M", 0))
	}
	if n := al.pendingLen(); n > maxPending || n == 0 {
		t.Error(test.DiffMessage(n, "1..16384", "A7: at most 16,384 requests are pending"))
	}
}

func TestAccessLog_PendingIsBoundedWithOneShard(t *testing.T) {
	tr, _, al := setup(t)
	for i := range 100000 {
		tr.Emit(stageEvent("same-prefix-"+strconv.Itoa(i), trace.StageMiddleware, "M", 0))
	}
	if n := al.pendingLen(); n > maxPending {
		t.Error(test.DiffMessage(n, "<= 16384", "A7: the bound holds whatever the IDs look like"))
	}
}

func TestAccessLog_PendingShrinksOnComplete(t *testing.T) {
	tr, _, al := setup(t)
	for i := range 100 {
		tr.Emit(stageEvent(spreadID(i), trace.StageMiddleware, "M", 0))
	}
	if n := al.pendingLen(); n != 100 {
		t.Fatal(test.DiffMessage(n, 100, "A7: one pending entry per request with a stage"))
	}
	for i := range 100 {
		tr.Emit(completeEvent(spreadID(i), 200, "/", 0))
	}
	if n := al.pendingLen(); n != 0 {
		t.Error(test.DiffMessage(n, 0, "A7: a complete event removes its pending request"))
	}
}

func TestAccessLog_CompleteWithoutStagesLeavesNothingPending(t *testing.T) {
	tr, _, al := setup(t)
	tr.Emit(completeEvent("solo", 404, "/", 0))
	if n := al.pendingLen(); n != 0 {
		t.Error(test.DiffMessage(n, 0, "A7: a request with no stage is not pending"))
	}
}

func TestAccessLog_EvictedRequestLogsWithoutStages(t *testing.T) {
	tr, logger, al := setup(t)
	first := spreadID(0)
	tr.Emit(stageEvent(first, trace.StageMiddleware, "M", time.Millisecond))
	for i := 1; i < 100000; i++ {
		tr.Emit(stageEvent(spreadID(i), trace.StageMiddleware, "M", 0))
	}
	tr.Emit(completeEvent(first, 200, "/evicted", 5*time.Millisecond))
	al.Stop()
	var found bool
	for _, line := range logger.lines() {
		if line.msg != "/evicted" {
			continue
		}
		found = true
		if hasArg(line.args, trace.StageMiddleware) {
			t.Error(test.DiffMessage(line.args, "no stages", "A7: an evicted request is logged without stages"))
		}
		if line.args[len(line.args)-1] != "5ms" {
			t.Error(test.DiffMessage(line.args[len(line.args)-1], "5ms", "A3: total of an evicted request"))
		}
	}
	if !found {
		t.Error(test.DiffMessage(false, true, "A7: an evicted request that completes is still logged"))
	}
}

func TestAccessLog_NewRequestStillTracedWhenFull(t *testing.T) {
	tr, logger, al := setup(t)
	for i := range 100000 {
		tr.Emit(stageEvent(spreadID(i), trace.StageMiddleware, "M", 0))
	}
	late := spreadID(1 << 30)
	tr.Emit(stageEvent(late, trace.StageGuard, "G-late", 0))
	tr.Emit(completeEvent(late, 200, "/late", 0))
	al.Stop()
	for _, line := range logger.lines() {
		if line.msg == "/late" {
			if got := entryTexts(stageOf(t, line.args, trace.StageGuard)); len(got) != 1 || !strings.HasPrefix(got[0], "G-late=") {
				t.Error(test.DiffMessage(got, []string{"G-late=…"}, "A7: a new request evicts an old one and keeps its own stages"))
			}
			return
		}
	}
	t.Error(test.DiffMessage(false, true, "A7: a new request is traced when the table is full"))
}

func blockedLogger() (*fakeLogger, chan struct{}, chan struct{}) {
	logger := newFakeLogger()
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	logger.onInfo = func(int) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-gate
	}
	return logger, gate, entered
}

func emitWithin(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal(test.DiffMessage("blocked for 10s", "returned", what))
	}
}

func TestAccessLog_FullQueueDropsAndCounts(t *testing.T) {
	if flushQueueSize != 1024 {
		t.Fatal(test.DiffMessage(flushQueueSize, 1024, "A8: flushQueueSize"))
	}
	tr := &trace.Tracer{}
	logger, gate, entered := blockedLogger()
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	tr.Emit(completeEvent("first", 200, "/first", 0))
	<-entered

	emitWithin(t, "A8: requests never wait for the logger", func() {
		for i := range 5000 {
			tr.Emit(completeEvent("r"+strconv.Itoa(i), 200, "/", 0))
		}
	})
	close(gate)
	al.Stop()

	logged := len(logger.lines())
	dropped := logger.dropped(t)
	if logged+dropped != 5001 {
		t.Error(test.DiffMessage(logged+dropped, 5001, "A8: lines logged plus lines reported as dropped equal the complete events"))
	}
	if dropped < 5001-flushQueueSize-1 {
		t.Error(test.DiffMessage(dropped, ">= 3976", "A8: a full queue drops the complete events"))
	}
	if logged > flushQueueSize+1 {
		t.Error(test.DiffMessage(logged, "<= 1025", "A8: the queue holds 1,024 lines"))
	}
}

func TestAccessLog_RequestNeverBlocksOnSlowLogger(t *testing.T) {
	tr := &trace.Tracer{}
	logger, gate, entered := blockedLogger()
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	defer al.Stop()
	defer close(gate)
	tr.Emit(completeEvent("first", 200, "/first", 0))
	<-entered
	emitWithin(t, "A8: the listener never blocks", func() {
		for i := range 3000 {
			id := "r" + strconv.Itoa(i)
			tr.Emit(stageEvent(id, trace.StageMiddleware, "M", 0))
			tr.Emit(completeEvent(id, 200, "/", 0))
		}
	})
}

func TestAccessLog_DroppedPlusLoggedEqualsCompleted(t *testing.T) {
	tr := &trace.Tracer{}
	logger := newFakeLogger()
	logger.onInfo = func(int) { runtime.Gosched() }
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	const total = 20000
	for i := range total {
		tr.Emit(completeEvent("r"+strconv.Itoa(i), 200, "/", 0))
	}
	al.Stop()
	if got := len(logger.lines()) + logger.dropped(t); got != total {
		t.Error(test.DiffMessage(got, total, "A8: nothing is lost without being counted"))
	}
}

func TestAccessLog_LoggerPanicIsRecovered(t *testing.T) {
	tr := &trace.Tracer{}
	logger := newFakeLogger()
	logger.onInfo = func(n int) {
		if n == 1 {
			panic("logger exploded")
		}
	}
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	tr.Emit(completeEvent("a", 200, "/a", 0))
	tr.Emit(completeEvent("b", 200, "/b", 0))
	tr.Emit(completeEvent("c", 200, "/c", 0))
	al.Stop()

	if got := len(logger.lines()); got != 2 {
		t.Error(test.DiffMessage(got, 2, "A9: the worker goes on after a panic of the logger"))
	}
	if got := logger.dropped(t); got != 1 {
		t.Error(test.DiffMessage(got, 1, "A9: the line whose logging panicked counts as dropped"))
	}
}

func TestAccessLog_StopFlushesQueue(t *testing.T) {
	tr := &trace.Tracer{}
	logger := newFakeLogger()
	logger.onInfo = func(int) { runtime.Gosched() }
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	for i := range 500 {
		tr.Emit(completeEvent("r"+strconv.Itoa(i), 200, "/"+strconv.Itoa(i), 0))
	}
	al.Stop()
	if got := len(logger.lines()) + logger.dropped(t); got != 500 {
		t.Error(test.DiffMessage(got, 500, "A10: Stop passes every queued line to the logger"))
	}
	lines := logger.lines()
	for i := 1; i < len(lines); i++ {
		a, _ := strconv.Atoi(lines[i-1].msg[1:])
		b, _ := strconv.Atoi(lines[i].msg[1:])
		if b <= a {
			t.Fatal(test.DiffMessage([]string{lines[i-1].msg, lines[i].msg}, "ascending", "A10: lines keep their order"))
		}
	}
}

func TestAccessLog_StopRemovesListener(t *testing.T) {
	tr := &trace.Tracer{}
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: newFakeLogger()})
	if !tr.Enabled() {
		t.Error(test.DiffMessage(false, true, "A10: the access log listens while it runs"))
	}
	al.Stop()
	if tr.Enabled() {
		t.Error(test.DiffMessage(true, false, "A10: Stop removes the listener"))
	}
}

func TestAccessLog_StopTwice(t *testing.T) {
	_, logger, al := setup(t)
	al.Stop()
	al.Stop()
	if len(logger.lines()) != 0 {
		t.Error(test.DiffMessage(len(logger.lines()), 0, "A10: a second Stop does nothing"))
	}
}

func TestAccessLog_EventsAfterStopIgnored(t *testing.T) {
	tr, logger, al := setup(t)
	al.Stop()
	tr.Emit(stageEvent("late", trace.StageMiddleware, "M", 0))
	tr.Emit(completeEvent("late", 200, "/late", 0))
	al.Stop()
	if len(logger.lines()) != 0 || logger.dropped(t) != 0 {
		t.Error(test.DiffMessage(logger.lines(), "nothing", "A10: events emitted after Stop are ignored"))
	}
}

func TestAccessLog_StopDiscardsPendingRequests(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(stageEvent("open", trace.StageMiddleware, "M", 0))
	al.Stop()
	if n := al.pendingLen(); n != 0 {
		t.Error(test.DiffMessage(n, 0, "A10: pending requests are discarded"))
	}
	if len(logger.lines()) != 0 {
		t.Error(test.DiffMessage(len(logger.lines()), 0, "A10: a request without complete is never logged"))
	}
}

func TestAccessLog_StopReturnsWhenLoggerBlocks(t *testing.T) {
	tr := &trace.Tracer{}
	logger, gate, entered := blockedLogger()
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	defer close(gate)
	tr.Emit(completeEvent("a", 200, "/a", 0))
	<-entered
	tr.Emit(completeEvent("b", 200, "/b", 0))

	done := make(chan struct{})
	go func() {
		al.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal(test.DiffMessage("Stop still waiting after 3s", "returned within 2s", "A10: Stop gives up after 2 seconds"))
	}
}

func goroutinesAtMost(base int) bool {
	for range 200000 {
		if runtime.NumGoroutine() <= base {
			return true
		}
		runtime.Gosched()
	}
	return runtime.NumGoroutine() <= base
}

func TestAccessLog_StopEndsWorker(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()
	logs := make([]*AccessLog, 50)
	for i := range logs {
		logs[i] = NewAccessLog(&AccessLogConfig{Tracer: &trace.Tracer{}, Logger: newFakeLogger()})
	}
	for _, al := range logs {
		al.Stop()
	}
	if !goroutinesAtMost(base) {
		t.Error(test.DiffMessage(runtime.NumGoroutine(), base, "A10: no worker goroutine survives Stop"))
	}
}

func TestAccessLog_StopDuringEmit_NoRace(t *testing.T) {
	tr := &trace.Tracer{}
	logger := newFakeLogger()
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := range 500 {
				id := fmt.Sprintf("g%d-%d", g, i)
				tr.Emit(stageEvent(id, trace.StageMiddleware, "M", 0))
				tr.Emit(completeEvent(id, 200, "/", 0))
			}
		}()
	}
	close(start)
	runtime.Gosched()
	al.Stop()
	wg.Wait()
	count := len(logger.lines())
	tr.Emit(completeEvent("after", 200, "/after", 0))
	if len(logger.lines()) != count {
		t.Error(test.DiffMessage(len(logger.lines()), count, "A10: nothing is logged after Stop returned"))
	}
}

func TestNewAccessLog_NilPanics(t *testing.T) {
	want := color.FmtRed("invalid access log: a tracer and a logger are required")
	cases := map[string]*AccessLogConfig{
		"nil config": nil,
		"nil tracer": {Logger: newFakeLogger()},
		"nil logger": {Tracer: &trace.Tracer{}},
		"empty":      {},
	}
	for name, cfg := range cases {
		var rec any
		func() {
			defer func() { rec = recover() }()
			NewAccessLog(cfg)
		}()
		if rec != want {
			t.Error(test.DiffMessage(rec, want, "A11: "+name+" panics with the bootstrap message"))
		}
	}
}

func TestAccessLog_WSLine(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(trace.Event{ID: "ws-1", Stage: trace.StageGuard, Name: "auth.Guard", Transport: trace.TransportWS, Duration: time.Millisecond})
	tr.Emit(trace.Event{ID: "ws-1", Stage: trace.StageComplete, Transport: trace.TransportWS, Operation: "PUBLISH", Target: "chat.to.user2", Duration: 2 * time.Millisecond})
	al.Stop()
	line := logger.lines()[0]
	if line.msg != "chat.to.user2" || argValue(t, line.args, "transport") != "WS" || argValue(t, line.args, "operation") != "PUBLISH" || argValue(t, line.args, "code") != 0 {
		t.Error(test.DiffMessage(line, "msg chat.to.user2, transport WS, operation PUBLISH", "A12: a WebSocket complete event produces the same line"))
	}
	if got := entryTexts(stageOf(t, line.args, trace.StageGuard)); len(got) != 1 || got[0] != "Guard=1ms (100%)" {
		t.Error(test.DiffMessage(got, []string{"Guard=1ms (100%)"}, "A12: WS stages are logged like HTTP ones"))
	}
}

func TestAccessLog_RequestSideAllocations(t *testing.T) {
	tr := &trace.Tracer{}
	logger, gate, entered := blockedLogger()
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	defer al.Stop()
	defer close(gate)
	tr.Emit(completeEvent("first", 200, "/first", 0))
	<-entered
	for i := range flushQueueSize + 8 {
		tr.Emit(completeEvent("fill"+strconv.Itoa(i), 200, "/", 0))
	}

	events := make([]trace.Event, 0, 7)
	for i := range 6 {
		events = append(events, stageEvent("alloc-id", trace.StageMiddleware, "cors.CORS"+strconv.Itoa(i), time.Microsecond))
	}
	events = append(events, completeEvent("alloc-id", 200, "/users", time.Millisecond))

	n := testing.AllocsPerRun(200, func() {
		for _, e := range events {
			tr.Emit(e)
		}
	})
	if n > 2 {
		t.Error(test.DiffMessage(n, "<= 2", "P2: the request goroutine allocates at most 2 times for 6 stages and complete"))
	}
}

func TestColorForDuration_Thresholds(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want color.Color
	}{
		{5 * time.Millisecond, color.Gray},
		{9999 * time.Microsecond, color.Gray},
		{10 * time.Millisecond, color.Green},
		{49 * time.Millisecond, color.Green},
		{50 * time.Millisecond, color.Blue},
		{99 * time.Millisecond, color.Blue},
		{100 * time.Millisecond, color.Yellow},
		{299 * time.Millisecond, color.Yellow},
		{300 * time.Millisecond, color.Orange},
		{999 * time.Millisecond, color.Orange},
		{1000 * time.Millisecond, color.Red},
		{2 * time.Second, color.Red},
	}
	for _, c := range cases {
		if got := colorForDuration(c.d); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "A2: colorForDuration("+c.d.String()+")"))
		}
	}
}

func TestFormatDuration_RoundsToTwoDecimals(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{1234 * time.Nanosecond, "1.23µs"},
		{1234567 * time.Nanosecond, "1.23ms"},
		{1234567891 * time.Nanosecond, "1.23s"},
		{500 * time.Nanosecond, "500ns"},
	}
	for _, c := range cases {
		if got := formatDuration(c.d); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "A2: formatDuration("+c.d.String()+")"))
		}
	}
}

func TestShortName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"cors.CORS", "CORS"},
		{"pkg.sub.Type", "Type"},
		{"NoPackage", "NoPackage"},
		{"", ""},
		{"trailing.", ""},
	}
	for _, c := range cases {
		if got := shortName(c.name); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "A2: shortName("+c.name+")"))
		}
	}
}
