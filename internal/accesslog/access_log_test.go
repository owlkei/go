package accesslog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/trace"
	"github.com/dangduoc08/ginject/log"
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

func (f *fakeLogger) warnings() []recordedLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.warns)
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
	return trace.Event{ID: id, TraceID: "trace-" + id, Stage: stage, Name: name, Transport: trace.TransportHTTP, Duration: d}
}

func completeEvent(id string, code int, target string, d time.Duration) trace.Event {
	return trace.Event{ID: id, TraceID: "trace-" + id, Stage: trace.StageComplete, Transport: trace.TransportHTTP, Operation: "GET", Target: target, Code: code, Duration: d}
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

func stageOf(t *testing.T, args []any, stage string) string {
	t.Helper()
	text, ok := argValue(t, args, stage).(string)
	if !ok {
		t.Fatalf("stage %q is not a string: %T", stage, argValue(t, args, stage))
	}
	return text
}

func entriesOf(t *testing.T, args []any, stage string) []string {
	t.Helper()
	return strings.Split(stageOf(t, args, stage), ", ")
}

func argKeys(args []any) []string {
	var keys []string
	for i := 0; i+1 < len(args); i += 2 {
		keys = append(keys, args[i].(string))
	}
	return keys
}

func percentsOf(t *testing.T, durations ...time.Duration) []int {
	t.Helper()
	tr, logger, _ := setup(t)
	for i, d := range durations {
		tr.Emit(stageEvent("p", trace.StageMiddleware, "M"+strconv.Itoa(i), d))
	}
	tr.Emit(completeEvent("p", 200, "/", time.Second))
	lines := logger.lines()
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d", len(lines))
	}
	var out []int
	for _, text := range entriesOf(t, lines[0].args, trace.StageMiddleware) {
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

func goroutineID() int {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	fields := strings.Fields(string(buf[:n]))
	id, _ := strconv.Atoi(fields[1])
	return id
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

func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal(test.DiffMessage("blocked for 30s", "returned", what))
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

type holdWriter struct {
	mu   sync.Mutex
	gate chan struct{}
	buf  bytes.Buffer
}

func newHoldWriter() *holdWriter { return &holdWriter{gate: make(chan struct{})} }

func (w *holdWriter) openAll() {
	w.mu.Lock()
	if w.gate != nil {
		close(w.gate)
		w.gate = nil
	}
	w.mu.Unlock()
}

func (w *holdWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	gate := w.gate
	w.mu.Unlock()
	if gate != nil {
		<-gate
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *holdWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

type outBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *outBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *outBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func builtinAccessLog(t *testing.T, format int) (*trace.Tracer, *outBuf) {
	t.Helper()
	out := &outBuf{}
	restore := logpipe.SetStdout(out)
	t.Cleanup(restore)
	opts := &log.LogOptions{Level: log.DebugLevel}
	switch format {
	case 0:
		opts.LogFormat = log.JSONFormat
	case 1:
		opts.LogFormat = log.TextFormat
	default:
		opts.LogFormat = log.PrettyFormat
	}
	tr := &trace.Tracer{}
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: log.NewLog(opts)})
	t.Cleanup(al.Stop)
	return tr, out
}

func TestAccessLog_AcceptsLogAdapter(t *testing.T) {
	var adapter log.LogAdapter = newFakeLogger()
	al := NewAccessLog(&AccessLogConfig{Tracer: &trace.Tracer{}, Logger: adapter})
	al.Stop()
}

func TestAccessLog_LineArgumentsInOrder(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(stageEvent("req-1", trace.StageMiddleware, "M1", time.Millisecond))
	tr.Emit(stageEvent("req-1", trace.StageGuard, "G1", 2*time.Millisecond))
	tr.Emit(stageEvent("req-1", trace.StageHandler, "H1", 3*time.Millisecond))
	tr.Emit(completeEvent("req-1", 200, "/users", 10*time.Millisecond))

	lines := logger.lines()
	if len(lines) != 1 {
		t.Fatal(test.DiffMessage(len(lines), 1, "A1: one Logger.Info per complete event"))
	}
	got := lines[0]
	if got.msg != "/users" {
		t.Error(test.DiffMessage(got.msg, "/users", "A1: the message is the Target"))
	}
	wantArgs := []any{
		"trace_id", "trace-req-1", "request", "req-1", "transport", "HTTP", "code", 200, "operation", "GET",
		"middleware", "M1 1ms (17%)", "guard", "G1 2ms (33%)", "handler", "H1 3ms (50%)", "total", "10ms",
	}
	if !slices.Equal(got.args, wantArgs) {
		t.Error(test.DiffMessage(got.args, wantArgs, "A1, A2: the fixed pairs, one string per stage, total; an adapter gets no extra pair"))
	}
}

func TestAccessLog_OneLinePerCompleteEvent(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(stageEvent("a", trace.StageMiddleware, "M", 0))
	tr.Emit(completeEvent("a", 200, "/a", 0))
	tr.Emit(completeEvent("b", 404, "/b", 0))
	if lines := logger.lines(); len(lines) != 2 || lines[0].msg != "/a" || lines[1].msg != "/b" {
		t.Error(test.DiffMessage(logger.lines(), "two lines /a and /b", "A1: one call per complete event, in order, already made when Emit returns"))
	}
}

func TestAccessLog_LineOnEmittingGoroutine(t *testing.T) {
	tr := &trace.Tracer{}
	logger := newFakeLogger()
	var loggerGoroutine int
	logger.onInfo = func(int) { loggerGoroutine = goroutineID() }
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	defer al.Stop()
	emitter := make(chan int, 1)
	go func() {
		tr.Emit(completeEvent("x", 200, "/", 0))
		emitter <- goroutineID()
	}()
	within(t, "A1: the line is logged by the emitting goroutine", func() {
		if got := <-emitter; got != loggerGoroutine || len(logger.lines()) != 1 {
			t.Error(test.DiffMessage(loggerGoroutine, got, "A1: Logger.Info runs on the emitting goroutine, before Emit returns"))
		}
	})
}

func TestAccessLog_StartsNoGoroutine(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()
	logs := make([]*AccessLog, 50)
	trs := make([]*trace.Tracer, 50)
	for i := range logs {
		trs[i] = &trace.Tracer{}
		logs[i] = NewAccessLog(&AccessLogConfig{Tracer: trs[i], Logger: newFakeLogger()})
	}
	for _, tr := range trs {
		tr.Emit(stageEvent("r", trace.StageMiddleware, "M", 0))
		tr.Emit(completeEvent("r", 200, "/", 0))
	}
	if !goroutinesAtMost(base) {
		t.Error(test.DiffMessage(runtime.NumGoroutine(), base, "A5: no worker, no ticker: a running access log starts no goroutine"))
	}
	for _, al := range logs {
		al.Stop()
	}
	if !goroutinesAtMost(base) {
		t.Error(test.DiffMessage(runtime.NumGoroutine(), base, "A5: nothing to end at Stop"))
	}
}

func TestAccessLog_StageValueIsOneString(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(stageEvent("r", trace.StageMiddleware, "cors.CORS", 40*time.Microsecond))
	tr.Emit(stageEvent("r", trace.StageMiddleware, "csrf.CSRF", 60*time.Microsecond))
	tr.Emit(completeEvent("r", 200, "/", time.Millisecond))
	args := logger.lines()[0].args
	if got, want := argValue(t, args, trace.StageMiddleware), any("CORS 40µs (40%), CSRF 60µs (60%)"); got != want {
		t.Error(test.DiffMessage(got, want, "A2: the events of a stage are joined by a comma and a space, each short name, duration and percent"))
	}
}

func TestAccessLog_StageValueRepeatedNamesKeptInOrder(t *testing.T) {
	tr, logger, _ := setup(t)
	for range 3 {
		tr.Emit(stageEvent("r", trace.StageMiddleware, "cors.CORS", time.Millisecond))
	}
	tr.Emit(completeEvent("r", 200, "/", time.Second))
	if got := entriesOf(t, logger.lines()[0].args, trace.StageMiddleware); len(got) != 3 || got[0] != "CORS 1ms (34%)" || got[1] != "CORS 1ms (33%)" || got[2] != "CORS 1ms (33%)" {
		t.Error(test.DiffMessage(got, []string{"CORS 1ms (34%)", "CORS 1ms (33%)", "CORS 1ms (33%)"}, "A2: repeated names are kept, one entry per event"))
	}
}

func TestAccessLog_StageValuesForEveryCount(t *testing.T) {
	for _, n := range []int{0, 1, 6, 64} {
		tr, logger, _ := setup(t)
		for i := range n {
			tr.Emit(stageEvent("r", trace.StageMiddleware, "M"+strconv.Itoa(i), time.Microsecond))
		}
		tr.Emit(completeEvent("r", 200, "/", time.Millisecond))
		args := logger.lines()[0].args
		if n == 0 {
			if hasArg(args, trace.StageMiddleware) {
				t.Error(test.DiffMessage(args, "no stage pair", "A2: no stage, no pair"))
			}
			continue
		}
		if got := len(entriesOf(t, args, trace.StageMiddleware)); got != n {
			t.Error(test.DiffMessage(got, n, "A2: one entry per event, "+strconv.Itoa(n)+" stages"))
		}
	}
}

func TestAccessLog_AllValuesScalar(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(stageEvent("r", trace.StageMiddleware, "cors.CORS", time.Millisecond))
	tr.Emit(stageEvent("r", trace.StageGuard, "auth.Guard", time.Millisecond))
	for i := range 70 {
		tr.Emit(stageEvent("r", trace.StageHandler, "H"+strconv.Itoa(i), 0))
	}
	tr.Emit(completeEvent("r", 200, "/", time.Millisecond))
	args := logger.lines()[0].args
	for i := 1; i < len(args); i += 2 {
		switch args[i].(type) {
		case string, int:
		default:
			t.Errorf("A2: the value of %v is %T, every value of the line is a scalar", args[i-1], args[i])
		}
	}
	for _, key := range []string{trace.StageMiddleware, trace.StageGuard, trace.StageHandler, "total"} {
		if _, ok := argValue(t, args, key).(string); !ok {
			t.Errorf("A2: %s is not a string", key)
		}
	}
	for _, a := range args {
		if _, ok := a.(color.Colored); ok {
			t.Error(test.DiffMessage("color.Colored", "scalar values", "A2: color.Colored is no longer used by the access log"))
		}
	}
}

func TestAccessLog_StagesInArrivalOrder(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(stageEvent("r", trace.StageHandler, "H", 0))
	tr.Emit(stageEvent("r", trace.StageMiddleware, "A", 0))
	tr.Emit(stageEvent("r", trace.StageGuard, "G", 0))
	tr.Emit(stageEvent("r", trace.StageMiddleware, "B", 0))
	tr.Emit(completeEvent("r", 200, "/", 0))

	args := logger.lines()[0].args
	var keys []string
	for i := 10; i < len(args)-2; i += 2 {
		keys = append(keys, args[i].(string))
	}
	want := []string{trace.StageHandler, trace.StageMiddleware, trace.StageGuard}
	if !slices.Equal(keys, want) {
		t.Error(test.DiffMessage(keys, want, "A2: stages appear in the order their first event arrived"))
	}
	if got := entriesOf(t, args, trace.StageMiddleware); len(got) != 2 || !strings.HasPrefix(got[0], "A ") || !strings.HasPrefix(got[1], "B ") {
		t.Error(test.DiffMessage(got, []string{"A …", "B …"}, "A2: events of one stage are in arrival order"))
	}
}

func TestAccessLog_PreservesExecutionOrderNotAlphabetical(t *testing.T) {
	tr, logger, _ := setup(t)
	for _, n := range []string{"C", "B", "A"} {
		tr.Emit(stageEvent("r", trace.StagePreInterceptor, n, 0))
	}
	for _, n := range []string{"A", "B", "C"} {
		tr.Emit(stageEvent("r", trace.StagePostInterceptor, n, 0))
	}
	tr.Emit(completeEvent("r", 200, "/", 0))

	args := logger.lines()[0].args
	names := func(stage string) []string {
		var out []string
		for _, entry := range entriesOf(t, args, stage) {
			out = append(out, strings.Fields(entry)[0])
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
	tr, logger, _ := setup(t)
	tr.Emit(stageEvent("r", trace.StageMiddleware, "cors.CORS", 0))
	tr.Emit(stageEvent("r", trace.StageMiddleware, "NoPackage", 0))
	tr.Emit(completeEvent("r", 200, "/", 0))
	got := entriesOf(t, logger.lines()[0].args, trace.StageMiddleware)
	if len(got) != 2 || got[0] != "CORS 0s (0%)" || got[1] != "NoPackage 0s (0%)" {
		t.Error(test.DiffMessage(got, []string{"CORS 0s (0%)", "NoPackage 0s (0%)"}, "A2: the name is the Name after its last dot"))
	}
}

func TestAccessLog_TotalIsCompleteDuration(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(stageEvent("r", trace.StageMiddleware, "M1", time.Millisecond))
	tr.Emit(completeEvent("r", 200, "/", 250*time.Millisecond))
	args := logger.lines()[0].args
	if args[len(args)-1] != "250ms" {
		t.Error(test.DiffMessage(args[len(args)-1], "250ms", "A1: total is the wall-clock duration of the complete event, not the sum of the stages"))
	}
}

func TestAccessLog_NoStagesLogsFixedPairsAndTotal(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(completeEvent("r", 404, "/nope", 1500*time.Microsecond))
	args := logger.lines()[0].args
	want := []any{"trace_id", "trace-r", "request", "r", "transport", "HTTP", "code", 404, "operation", "GET", "total", "1.5ms"}
	if !slices.Equal(args, want) {
		t.Error(test.DiffMessage(args, want, "A1: a request without stages logs the fixed pairs and total"))
	}
}

func TestAccessLog_LongTargetIsCut(t *testing.T) {
	tr, logger, _ := setup(t)
	target := "/" + strings.Repeat("a", 2999)
	tr.Emit(completeEvent("r", 200, target, 0))
	msg := logger.lines()[0].msg
	if msg != target[:2048]+"..." {
		t.Error(test.DiffMessage(len(msg), 2051, "A1: a long target is cut to 2,048 bytes followed by ..."))
	}
}

func TestAccessLog_TargetOfExactLimitIsKept(t *testing.T) {
	tr, logger, _ := setup(t)
	target := "/" + strings.Repeat("a", 2047)
	tr.Emit(completeEvent("r", 200, target, 0))
	if msg := logger.lines()[0].msg; msg != target {
		t.Error(test.DiffMessage(len(msg), 2048, "A1: a target of 2,048 bytes is not cut"))
	}
}

func TestAccessLog_TargetCutOnRuneBoundary(t *testing.T) {
	tr, logger, _ := setup(t)
	target := "a" + strings.Repeat("é", 1500)
	tr.Emit(completeEvent("r", 200, target, 0))
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
	tr, logger, _ := setup(t)
	tr.Emit(stageEvent("r", trace.StageMiddleware, "M1", 10*time.Millisecond))
	tr.Emit(stageEvent("r", trace.StageHandler, "H1", 90*time.Millisecond))
	tr.Emit(completeEvent("r", 200, "/", time.Second))
	args := logger.lines()[0].args
	if got := entriesOf(t, args, trace.StageMiddleware); got[0] != "M1 10ms (10%)" {
		t.Error(test.DiffMessage(got, "M1 10ms (10%)", "A2: a percent is the share of the sum of the stage durations"))
	}
}

func TestAccessLog_SeparatesInterleavedIDs(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(stageEvent("a", trace.StageMiddleware, "A1", 0))
	tr.Emit(stageEvent("b", trace.StageMiddleware, "B1", 0))
	tr.Emit(stageEvent("a", trace.StageGuard, "A2", 0))
	tr.Emit(stageEvent("b", trace.StageGuard, "B2", 0))
	tr.Emit(completeEvent("b", 200, "/b", 0))
	tr.Emit(completeEvent("a", 200, "/a", 0))
	lines := logger.lines()
	if len(lines) != 2 {
		t.Fatal(test.DiffMessage(len(lines), 2, "A5: one line per request"))
	}
	for _, line := range lines {
		id := strings.TrimPrefix(line.msg, "/")
		for _, stage := range []string{trace.StageMiddleware, trace.StageGuard} {
			for _, text := range entriesOf(t, line.args, stage) {
				if !strings.HasPrefix(strings.ToLower(text), id) {
					t.Error(test.DiffMessage(text, "an entry of "+id, "A5: events of different IDs never mix"))
				}
			}
		}
	}
}

func TestAccessLog_ConcurrentRequests_NoRace(t *testing.T) {
	tr, logger, _ := setup(t)
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

	lines := logger.lines()
	if len(lines) != goroutines*perGoroutine {
		t.Fatal(test.DiffMessage(len(lines), goroutines*perGoroutine, "A1: one line per complete event, none dropped by the access log itself"))
	}
	for _, line := range lines {
		id := strings.TrimPrefix(line.msg, "/")
		if argValue(t, line.args, "request") != id {
			t.Fatal(test.DiffMessage(argValue(t, line.args, "request"), id, "A5: the line carries its own ID"))
		}
		for _, stage := range []string{trace.StageMiddleware, trace.StageHandler} {
			list := entriesOf(t, line.args, stage)
			if len(list) != 1 || !strings.HasSuffix(strings.Fields(list[0])[0], id) {
				t.Fatal(test.DiffMessage(list, "one entry of "+id, "A5: each line has its own stages only"))
			}
		}
	}
}

func TestAccessLog_StagesCappedAt64(t *testing.T) {
	if maxStagesPerRequest != 64 {
		t.Fatal(test.DiffMessage(maxStagesPerRequest, 64, "A6: maxStagesPerRequest"))
	}
	tr, logger, _ := setup(t)
	for i := range 100 {
		tr.Emit(stageEvent("r", trace.StageMiddleware, "M"+strconv.Itoa(i), 0))
	}
	tr.Emit(completeEvent("r", 200, "/", 0))
	args := logger.lines()[0].args
	if n := len(entriesOf(t, args, trace.StageMiddleware)); n != 64 {
		t.Error(test.DiffMessage(n, 64, "A6: a request keeps at most 64 stage events"))
	}
	if args[len(args)-4] != "stages_dropped" || args[len(args)-3] != 36 || args[len(args)-2] != "total" {
		t.Error(test.DiffMessage(args[len(args)-4:], []any{"stages_dropped", 36, "total", "…"}, "A1, A6: stages_dropped, n goes before total"))
	}
}

func TestAccessLog_StagesCapKeepsTheFirst(t *testing.T) {
	tr, logger, _ := setup(t)
	for i := range 70 {
		tr.Emit(stageEvent("r", trace.StageMiddleware, "M"+strconv.Itoa(i), 0))
	}
	tr.Emit(completeEvent("r", 200, "/", 0))
	list := entriesOf(t, logger.lines()[0].args, trace.StageMiddleware)
	if !strings.HasPrefix(list[0], "M0 ") || !strings.HasPrefix(list[63], "M63 ") {
		t.Error(test.DiffMessage([]string{list[0], list[63]}, []string{"M0 …", "M63 …"}, "A6: the first 64 events are the ones kept"))
	}
}

func TestAccessLog_ExactlySixtyFourStagesHasNoDropCount(t *testing.T) {
	tr, logger, _ := setup(t)
	for i := range 64 {
		tr.Emit(stageEvent("r", trace.StageMiddleware, "M"+strconv.Itoa(i), 0))
	}
	tr.Emit(completeEvent("r", 200, "/", 0))
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
	tr, logger, _ := setup(t)
	first := spreadID(0)
	tr.Emit(stageEvent(first, trace.StageMiddleware, "M", time.Millisecond))
	for i := 1; i < 100000; i++ {
		tr.Emit(stageEvent(spreadID(i), trace.StageMiddleware, "M", 0))
	}
	tr.Emit(completeEvent(first, 200, "/evicted", 5*time.Millisecond))
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
			t.Error(test.DiffMessage(line.args[len(line.args)-1], "5ms", "A1: total of an evicted request"))
		}
	}
	if !found {
		t.Error(test.DiffMessage(false, true, "A7: an evicted request that completes is still logged"))
	}
}

func TestAccessLog_NewRequestStillTracedWhenFull(t *testing.T) {
	tr, logger, _ := setup(t)
	for i := range 100000 {
		tr.Emit(stageEvent(spreadID(i), trace.StageMiddleware, "M", 0))
	}
	late := spreadID(1 << 30)
	tr.Emit(stageEvent(late, trace.StageGuard, "G-late", 0))
	tr.Emit(completeEvent(late, 200, "/late", 0))
	for _, line := range logger.lines() {
		if line.msg == "/late" {
			if got := entriesOf(t, line.args, trace.StageGuard); len(got) != 1 || !strings.HasPrefix(got[0], "G-late ") {
				t.Error(test.DiffMessage(got, []string{"G-late …"}, "A7: a new request evicts an old one and keeps its own stages"))
			}
			return
		}
	}
	t.Error(test.DiffMessage(false, true, "A7: a new request is traced when the table is full"))
}

func TestAccessLog_LoggerPanicRecovered(t *testing.T) {
	tr := &trace.Tracer{}
	logger := newFakeLogger()
	logger.onInfo = func(n int) {
		if n == 1 {
			panic("logger exploded")
		}
	}
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	defer al.Stop()
	tr.Emit(completeEvent("a", 200, "/a", 0))
	tr.Emit(completeEvent("b", 200, "/b", 0))
	tr.Emit(completeEvent("c", 200, "/c", 0))

	lines := logger.lines()
	if len(lines) != 2 || lines[0].msg != "/b" || lines[1].msg != "/c" {
		t.Error(test.DiffMessage(lines, "lines /b and /c", "A4: a panic of the logger is recovered in the listener, the line is lost, the next ones are logged"))
	}
	if len(logger.warnings()) != 0 {
		t.Error(test.DiffMessage(logger.warnings(), "no warning", "A5: no dropped record of the access log"))
	}
}

func TestAccessLog_StopRemovesListener(t *testing.T) {
	tr := &trace.Tracer{}
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: newFakeLogger()})
	if !tr.Enabled() {
		t.Error(test.DiffMessage(false, true, "A5: the access log listens while it runs"))
	}
	al.Stop()
	if tr.Enabled() {
		t.Error(test.DiffMessage(true, false, "A5: Stop removes the listener"))
	}
}

func TestAccessLog_StopTwice(t *testing.T) {
	_, logger, al := setup(t)
	al.Stop()
	al.Stop()
	if len(logger.lines()) != 0 {
		t.Error(test.DiffMessage(len(logger.lines()), 0, "A5: a second Stop does nothing"))
	}
}

func TestAccessLog_EventsAfterStopIgnored(t *testing.T) {
	tr, logger, al := setup(t)
	al.Stop()
	tr.Emit(stageEvent("late", trace.StageMiddleware, "M", 0))
	tr.Emit(completeEvent("late", 200, "/late", 0))
	al.Stop()
	if len(logger.lines()) != 0 || len(logger.warnings()) != 0 {
		t.Error(test.DiffMessage(logger.lines(), "nothing", "A5: events emitted after Stop are ignored"))
	}
}

func TestAccessLog_StopDiscardsPendingRequests(t *testing.T) {
	tr, logger, al := setup(t)
	tr.Emit(stageEvent("open", trace.StageMiddleware, "M", 0))
	al.Stop()
	if n := al.pendingLen(); n != 0 {
		t.Error(test.DiffMessage(n, 0, "A5: pending requests are discarded"))
	}
	if len(logger.lines()) != 0 {
		t.Error(test.DiffMessage(len(logger.lines()), 0, "A5: a request without complete is never logged"))
	}
}

func TestAccessLog_StopReturnsAtOnce(t *testing.T) {
	tr := &trace.Tracer{}
	logger, gate, entered := blockedLogger()
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: logger})
	emitted := make(chan struct{})
	go func() {
		defer close(emitted)
		tr.Emit(completeEvent("a", 200, "/a", 0))
	}()
	<-entered
	within(t, "A5: Stop returns at once, whatever the logger of an in-flight line does", al.Stop)
	close(gate)
	<-emitted
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
		t.Error(test.DiffMessage(len(logger.lines()), count, "A5: nothing is logged after Stop returned"))
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
	tr, logger, _ := setup(t)
	tr.Emit(trace.Event{ID: "ws-1", Stage: trace.StageGuard, Name: "auth.Guard", Transport: trace.TransportWS, Duration: time.Millisecond})
	tr.Emit(trace.Event{ID: "ws-1", Stage: trace.StageComplete, Transport: trace.TransportWS, Operation: "PUBLISH", Target: "chat.to.user2", Duration: 2 * time.Millisecond})
	line := logger.lines()[0]
	if line.msg != "chat.to.user2" || argValue(t, line.args, "transport") != "WS" || argValue(t, line.args, "operation") != "PUBLISH" || argValue(t, line.args, "code") != 0 {
		t.Error(test.DiffMessage(line, "msg chat.to.user2, transport WS, operation PUBLISH", "A12: a WebSocket complete event produces the same line"))
	}
	if got := entriesOf(t, line.args, trace.StageGuard); len(got) != 1 || got[0] != "Guard 1ms (100%)" {
		t.Error(test.DiffMessage(got, []string{"Guard 1ms (100%)"}, "A12: WS stages are logged like HTTP ones"))
	}
}

func TestAccessLog_SixStageLineAllocations(t *testing.T) {
	restore := logpipe.SetStdout(io.Discard)
	defer restore()
	tr := &trace.Tracer{}
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: log.NewLog(&log.LogOptions{LogFormat: log.JSONFormat})})
	defer al.Stop()
	names := []struct{ stage, name string }{
		{trace.StageMiddleware, "cors.CORS"},
		{trace.StageMiddleware, "csrf.CSRF"},
		{trace.StageMiddleware, "helmet.Helmet"},
		{trace.StageGuard, "auth.AuthGuard"},
		{trace.StagePreInterceptor, "log.LogInterceptor"},
		{trace.StageHandler, "READ_users"},
	}
	events := make([]trace.Event, 0, 7)
	for i, n := range names {
		e := stageEvent("0f1e2d3c4b5a6978", n.stage, n.name, time.Duration(40+i*20)*time.Microsecond)
		e.TraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		events = append(events, e)
	}
	done := completeEvent("0f1e2d3c4b5a6978", 200, "/api/v1/users", 3*time.Millisecond)
	done.TraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	events = append(events, done)
	emit := func() {
		for _, e := range events {
			tr.Emit(e)
		}
	}
	emit()
	allocs := testing.AllocsPerRun(200, emit)
	if allocs > 10 {
		t.Error(test.DiffMessage(allocs, "<= 10", "A2: a 6-stage access line through the built-in JSON logger allocates at most 10 times (design measured 26)"))
	}
}

func TestAccessLog_BuiltinJSONLineIsPlain(t *testing.T) {
	tr, out := builtinAccessLog(t, 0)
	tr.Emit(stageEvent("r1", trace.StageMiddleware, "cors.CORS", 40*time.Microsecond))
	tr.Emit(stageEvent("r1", trace.StageMiddleware, "csrf.CSRF", 60*time.Microsecond))
	tr.Emit(completeEvent("r1", 200, "/users", 2*time.Second))
	line := out.String()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatal(test.DiffMessage(line, "one JSON object", "A2: JSON gets the plain string"))
	}
	if parsed["middleware"] != "CORS 40µs (40%), CSRF 60µs (60%)" || parsed["total"] != "2s" || parsed["msg"] != "/users" {
		t.Error(test.DiffMessage(parsed, "string values", "A2: stage and total are plain strings in JSON"))
	}
	if strings.Contains(line, "\x1b") {
		t.Error(test.DiffMessage(line, "no ANSI escape", "A2: no color outside Pretty"))
	}
}

func TestAccessLog_BuiltinTextLineIsPlain(t *testing.T) {
	tr, out := builtinAccessLog(t, 1)
	tr.Emit(stageEvent("r1", trace.StageGuard, "auth.Guard", 5*time.Millisecond))
	tr.Emit(completeEvent("r1", 200, "/users", 2*time.Second))
	line := out.String()
	if !strings.Contains(line, `guard="Guard 5ms (100%)"`) || !strings.Contains(line, "total=2s") || strings.Contains(line, "\x1b") {
		t.Error(test.DiffMessage(line, `guard="Guard 5ms (100%)" total=2s, no ANSI escape`, "A2: Text gets the plain string"))
	}
}

func TestAccessLog_PrettyColorsDurations(t *testing.T) {
	tr, out := builtinAccessLog(t, 2)
	tr.Emit(stageEvent("r1", trace.StageMiddleware, "cors.CORS", 5*time.Millisecond))
	tr.Emit(stageEvent("r1", trace.StageGuard, "auth.Guard", 20*time.Millisecond))
	tr.Emit(stageEvent("r1", trace.StagePreInterceptor, "a.Pre", 70*time.Millisecond))
	tr.Emit(stageEvent("r1", trace.StageHandler, "h.Handler", 150*time.Millisecond))
	tr.Emit(stageEvent("r1", trace.StagePostInterceptor, "a.Post", 500*time.Millisecond))
	tr.Emit(stageEvent("r1", trace.StageExceptionFilter, "f.Filter", 2*time.Second))
	tr.Emit(completeEvent("r1", 200, "/users", 350*time.Millisecond))
	got := out.String()
	for _, c := range []struct {
		color color.Color
		text  string
	}{
		{color.Gray, "5ms"},
		{color.Green, "20ms"},
		{color.Blue, "70ms"},
		{color.Yellow, "150ms"},
		{color.Orange, "500ms"},
		{color.Red, "2s"},
		{color.Orange, "350ms"},
	} {
		if !strings.Contains(got, string(c.color)+c.text) {
			t.Error(test.DiffMessage(got, fmt.Sprintf("%q colored", c.text), "A2: Pretty colors each duration by its threshold, total included"))
		}
	}
	for _, name := range []string{"CORS", "Guard", "Pre", "Handler", "Post", "Filter"} {
		if !strings.Contains(got, name) {
			t.Error(test.DiffMessage(got, name, "A2: the stage text is still printed"))
		}
	}
}

func TestAccessLog_PrettyForgedMarkerInUserRecordNotColored(t *testing.T) {
	out := &outBuf{}
	restore := logpipe.SetStdout(out)
	defer restore()
	logger := log.NewLog(&log.LogOptions{LogFormat: log.PrettyFormat})
	logger.Info("GET /forged 5ms (4%)", "middleware", "CORS 5ms (4%)", "total", "2s")
	if strings.Contains(out.String(), string(color.Gray)) || strings.Contains(out.String(), string(color.Red)+"2s") {
		t.Error(test.DiffMessage(out.String(), "no duration color", "A2: a user record is never colored"))
	}
}

func TestAccessLog_BuiltinLineDroppedUnderOverloadIsCounted(t *testing.T) {
	w := newHoldWriter()
	restore := logpipe.SetStdout(w)
	defer restore()
	logpipe.Stdout().StartAsync()
	defer logpipe.Stdout().StopAsync(time.Minute)
	defer w.openAll()
	tr := &trace.Tracer{}
	al := NewAccessLog(&AccessLogConfig{Tracer: tr, Logger: log.NewLog(&log.LogOptions{LogFormat: log.JSONFormat})})
	defer al.Stop()
	const total = 5000
	target := "/overload-" + strings.Repeat("x", 200)
	within(t, "A3: the request goroutine never waits for stdout", func() {
		for i := range total {
			id := "r" + strconv.Itoa(i)
			tr.Emit(stageEvent(id, trace.StageMiddleware, "cors.CORS", time.Microsecond))
			tr.Emit(completeEvent(id, 200, target, time.Millisecond))
		}
	})
	w.openAll()
	if !logpipe.Stdout().Flush(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, "P12: Flush returns true"))
	}
	written, dropped := 0, 0
	for _, line := range strings.Split(strings.TrimSuffix(w.String(), "\n"), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(test.DiffMessage(line, "one JSON object", "P6: records are written whole"))
		}
		switch msg, _ := rec["msg"].(string); {
		case strings.HasPrefix(msg, "/overload-"):
			written++
		case msg == "LogDropped":
			if rec["level"] != "WARN" {
				t.Error(test.DiffMessage(rec["level"], "WARN", "P10: LogDropped is a WARN record"))
			}
			n, _ := rec["info"].(float64)
			dropped += int(n)
		}
	}
	if dropped == 0 || written+dropped != total {
		t.Error(test.DiffMessage([]int{written, dropped}, "written + dropped = 5000, dropped > 0", "A3: an access line is an Info record, dropped under overload and counted in LogDropped"))
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

func TestAccessLine_HTTPKeys(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(trace.Event{
		ID: "0f1e2d3c4b5a6978", TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "0f1e2d3c4b5a6978", ParentID: "00f067aa0ba902b7",
		Stage: trace.StageComplete, Transport: trace.TransportHTTP, Operation: "GET", Target: "/users", Code: 200, Duration: 10 * time.Millisecond,
	})
	line := logger.lines()[0]
	if line.msg != "/users" {
		t.Error(test.DiffMessage(line.msg, "/users", "E7: the message is still the target"))
	}
	want := []any{
		"trace_id", "4bf92f3577b34da6a3ce929d0e0e4736", "request", "0f1e2d3c4b5a6978",
		"transport", "HTTP", "code", 200, "operation", "GET", "total", "10ms",
	}
	if !slices.Equal(line.args, want) {
		t.Error(test.DiffMessage(line.args, want, "E7: an HTTP line starts with trace_id and request, the incoming parent is not printed"))
	}
	if slices.Contains(argKeys(line.args), "id") {
		t.Error(test.DiffMessage(argKeys(line.args), "<no id key>", "E7: the key id is gone"))
	}
}

func TestAccessLine_HTTPKeysKeepStagesAfterOperation(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(stageEvent("r1", trace.StageGuard, "auth.Guard", time.Microsecond))
	done := completeEvent("r1", 200, "/", time.Millisecond)
	done.ParentID = "00f067aa0ba902b7"
	tr.Emit(done)
	want := []string{"trace_id", "request", "transport", "code", "operation", "guard", "total"}
	if got := argKeys(logger.lines()[0].args); !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "E7: the rest of the line is unchanged"))
	}
}

func TestAccessLine_WSKeys(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(trace.Event{
		ID: "aaaaaaaaaaaaaaaa", TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "aaaaaaaaaaaaaaaa", ParentID: "bbbbbbbbbbbbbbbb",
		Stage: trace.StageComplete, Transport: trace.TransportWS, Operation: "PUBLISH", Target: "chat.to.user2", Duration: 2 * time.Millisecond,
	})
	want := []any{
		"trace_id", "4bf92f3577b34da6a3ce929d0e0e4736", "connection", "bbbbbbbbbbbbbbbb", "message", "aaaaaaaaaaaaaaaa",
		"transport", "WS", "code", 0, "operation", "PUBLISH", "total", "2ms",
	}
	if got := logger.lines()[0].args; !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "E7: a WS line starts with trace_id, connection and message"))
	}
}

func TestAccessLine_WSWithoutConnection(t *testing.T) {
	tr, logger, _ := setup(t)
	tr.Emit(trace.Event{
		ID: "aaaaaaaaaaaaaaaa", TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "aaaaaaaaaaaaaaaa",
		Stage: trace.StageComplete, Transport: trace.TransportWS, Operation: "SUBSCRIBE", Target: "t", Duration: time.Millisecond,
	})
	want := []string{"trace_id", "message", "transport", "code", "operation", "total"}
	if got := argKeys(logger.lines()[0].args); !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "E7: connection is skipped when ParentID is empty"))
	}
}

func TestAccessLog_CorrelatesByEventIDNotSpanID(t *testing.T) {
	tr, logger, _ := setup(t)
	stage := stageEvent("req-1", trace.StageGuard, "G1", time.Millisecond)
	stage.SpanID = "1111111111111111"
	stage.ParentID = "req-1"
	other := stageEvent("req-2", trace.StageGuard, "G2", time.Millisecond)
	other.SpanID = "2222222222222222"
	other.ParentID = "req-2"
	done := completeEvent("req-1", 200, "/a", time.Millisecond)
	done.SpanID = "req-1"
	tr.Emit(stage)
	tr.Emit(other)
	tr.Emit(done)

	lines := logger.lines()
	if len(lines) != 1 {
		t.Fatal(test.DiffMessage(len(lines), 1, "E7: one line per complete event"))
	}
	if got := entriesOf(t, lines[0].args, trace.StageGuard); len(got) != 1 || !strings.HasPrefix(got[0], "G1 ") {
		t.Error(test.DiffMessage(got, []string{"G1 …"}, "E7: stages are joined by Event.ID, so span IDs don't matter"))
	}
}

func TestAccessLog_PrettyStagesAsTree(t *testing.T) {
	tr, out := builtinAccessLog(t, 2)
	tr.Emit(stageEvent("r1", trace.StageMiddleware, "cors.CORS", 5*time.Millisecond))
	tr.Emit(stageEvent("r1", trace.StageMiddleware, "helmet.LongHelmetName", 20*time.Millisecond))
	tr.Emit(stageEvent("r1", trace.StageHandler, "h.READ_ping", 75*time.Millisecond))
	tr.Emit(completeEvent("r1", 200, "/ping", 100*time.Millisecond))
	got := out.String()
	plain := ansiPattern.ReplaceAllString(got, "")
	lines := strings.Split(plain, "\n")
	find := func(sub string) string {
		for _, l := range lines {
			if strings.Contains(l, sub) {
				return l
			}
		}
		return ""
	}
	if l := find("── middleware"); strings.TrimSpace(strings.SplitN(l, "middleware", 2)[1]) != "" {
		t.Error(test.DiffMessage(l, "middleware with no value on its row", "A2 (2026-11-17): a stage is a branch in Pretty"))
	}
	cors, helmet, ping := find("├── CORS"), find("└── LongHelmetName"), find("└── READ_ping")
	if !strings.HasPrefix(cors, "  │   ├── CORS ") || !strings.HasSuffix(cors, "5ms (5%)") {
		t.Error(test.DiffMessage(cors, "  │   ├── CORS … 5ms (5%)", "A2 (2026-11-17): one child per stage event"))
	}
	if !strings.HasSuffix(helmet, "20ms (20%)") || !strings.HasSuffix(ping, "75ms (75%)") {
		t.Error(test.DiffMessage(plain, "LongHelmetName 20ms (20%), READ_ping 75ms (75%)", "A2 (2026-11-17): children carry duration and percent"))
	}
	column := func(line, sub string) int {
		i := strings.Index(line, sub)
		if i < 0 {
			return -1
		}
		return utf8.RuneCountInString(line[:i])
	}
	col := column(cors, "5ms")
	if col < 0 || column(helmet, "20ms") != col || column(ping, "75ms") != col || column(find("── total"), "100ms") != col {
		t.Error(test.DiffMessage(plain, "durations in one column", "A2 (2026-11-17): children align with the other values"))
	}
	if !strings.Contains(got, string(color.Gray)+"5ms (5%)") || !strings.Contains(got, string(color.Blue)+"75ms (75%)") {
		t.Error(test.DiffMessage(got, "children colored by duration", "A2: Pretty colors each duration by its threshold"))
	}
}

func TestAccessLog_PrettyUserStageLikeValueStaysString(t *testing.T) {
	out := &outBuf{}
	restore := logpipe.SetStdout(out)
	defer restore()
	logger := log.NewLog(&log.LogOptions{LogFormat: log.PrettyFormat})
	logger.Info("GET /forged", "middleware", "CORS 5ms (4%), CSRF 2s (96%)")
	if !strings.Contains(out.String(), "CORS 5ms (4%), CSRF 2s (96%)") {
		t.Error(test.DiffMessage(out.String(), "one string", "A2 (2026-11-17): user records are never drawn as a stage tree"))
	}
}

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")
