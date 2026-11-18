package log

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

type benchUser struct {
	Name     string
	Password string
	Age      int
	City     string
}

type benchTagPerson struct {
	Age    int    `log:"age"`
	name   string `log:"name"`
	School string
	policy string
}

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}

func (noopLogger) Info(string, ...any) {}

func (noopLogger) Warn(string, ...any) {}

func (noopLogger) Error(string, ...any) {}

func (noopLogger) Fatal(string, ...any) {}

const (
	fromTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	fromRequest = "00f067aa0ba902b7"
)

type fromCall struct {
	level string
	msg   string
	args  []any
}

type fromAdapter struct {
	mu    sync.Mutex
	calls []fromCall
}

func (a *fromAdapter) record(level, msg string, args []any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, fromCall{level: level, msg: msg, args: slices.Clone(args)})
}

func (a *fromAdapter) Debug(msg string, args ...any) { a.record("debug", msg, args) }

func (a *fromAdapter) Info(msg string, args ...any) { a.record("info", msg, args) }

func (a *fromAdapter) Warn(msg string, args ...any) { a.record("warn", msg, args) }

func (a *fromAdapter) Error(msg string, args ...any) { a.record("error", msg, args) }

func (a *fromAdapter) Fatal(msg string, args ...any) { a.record("fatal", msg, args) }

func (a *fromAdapter) all() []fromCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.calls)
}

func scopeContext(fields tracecontext.Scope) context.Context {
	return tracecontext.With(context.Background(), &fields)
}

func requestContext() context.Context {
	return scopeContext(tracecontext.Scope{TraceID: fromTraceID, Request: fromRequest})
}

var (
	jsonTime   = regexp.MustCompile(`"time":"[^"]*"`)
	textTime   = regexp.MustCompile(`time=\S+`)
	prettyTime = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)
)

func withoutTime(s string) string {
	s = jsonTime.ReplaceAllString(s, `"time":""`)
	s = textTime.ReplaceAllString(s, "time=")
	return prettyTime.ReplaceAllString(s, "0000-00-00 00:00:00")
}

func fromRandomString(r *rand.Rand, alphabet []string, max int) string {
	var b strings.Builder
	for range r.Intn(max + 1) {
		b.WriteString(alphabet[r.Intn(len(alphabet))])
	}
	return b.String()
}

type fromRandomStringer struct{ n int }

func (s fromRandomStringer) String() string { return fmt.Sprintf("stringer-%d", s.n) }

type fromRandomStruct struct {
	A int
	B string `log:"bee"`
}

func fromRandomArgs(r *rand.Rand) []any {
	keys := []string{"k", "a b", "q\"uote", "ñ", "new\nline", "password", "token"}
	values := []func() any{
		func() any { return r.Intn(1000) },
		func() any {
			return fromRandomString(r, []string{"a", "b", " ", "\"", "\n", "é", "日", "\x1b", "\\"}, 12)
		},
		func() any { return r.Intn(2) == 0 },
		func() any { return r.Float64() },
		func() any { return nil },
		func() any { return errors.New("boom " + fromRandomString(r, []string{"x", " "}, 4)) },
		func() any { return time.Duration(r.Intn(1e9)) },
		func() any { return map[string]any{"x": r.Intn(5), "y": "z"} },
		func() any { return []int{r.Intn(3), r.Intn(3)} },
		func() any { return fromRandomStruct{A: r.Intn(9), B: "s"} },
		func() any { return fromRandomStringer{r.Intn(9)} },
		func() any { var p *int; return p },
	}
	var args []any
	for range r.Intn(5) {
		if r.Intn(6) == 0 {
			args = append(args, slog.String(keys[r.Intn(len(keys))], "attr"))
			continue
		}
		args = append(args, keys[r.Intn(len(keys))], values[r.Intn(len(values))]())
	}
	return args
}

type failingValue struct{ t *testing.T }

func (f failingValue) String() string {
	f.t.Error(test.DiffMessage("String called", "never called", "F7: no work on the arguments below the level"))
	return "x"
}

func (f failingValue) Error() string {
	f.t.Error(test.DiffMessage("Error called", "never called", "F7: no work on the arguments below the level"))
	return "x"
}

func (f failingValue) LogValue() slog.Value {
	f.t.Error(test.DiffMessage("LogValue called", "never called", "F7: no work on the arguments below the level"))
	return slog.StringValue("x")
}

const (
	testRed   = "\x1b[31m"
	testReset = "\x1b[0m"
)

func prettyOnce(msg string, args ...any) string {
	logger, buf := newTestLog(FormatPretty, LogOptions{})
	logger.Info(msg, args...)
	return buf.String()
}

func valueColumn(t *testing.T, line, value string) int {
	t.Helper()
	line = plain(line)
	i := strings.Index(line, value)
	if i == -1 {
		t.Fatalf("value %q not found in line %q", value, line)
	}
	return len([]rune(line[:i]))
}

type errString struct{ text string }

func (e *errString) Error() string { return e.text }

type instanceRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *instanceRecorder) add(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, msg)
}

func (r *instanceRecorder) Debug(msg string, _ ...any) { r.add(msg) }

func (r *instanceRecorder) Info(msg string, _ ...any) { r.add(msg) }

func (r *instanceRecorder) Warn(msg string, _ ...any) { r.add(msg) }

func (r *instanceRecorder) Error(msg string, _ ...any) { r.add(msg) }

func (r *instanceRecorder) Fatal(msg string, _ ...any) { r.add(msg) }

func captureStdout(t *testing.T, build func() func()) string {
	t.Helper()
	buf := &syncBuf{}
	restore := logpipe.SetStdout(buf)
	defer restore()
	emit := build()
	emit()
	return buf.String()
}

type groupValuer struct{}

func (groupValuer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("name", "duoc"), slog.String("password", "s3cret-group"))
}

type maskUser struct {
	Name     string
	Password string
}

type maskUserTagged struct {
	Name     string `log:"name"`
	Password string `log:"password"`
}

func maskedJSON(rules []string, msg string, args ...any) string {
	logger, buf := newTestLog(FormatJSON, LogOptions{MaskFields: rules})
	logger.Info(msg, args...)
	return buf.String()
}

type callCounter struct {
	capturingLogger
	mu    sync.Mutex
	calls int
}

func (c *callCounter) Info(msg string, args ...any) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	c.capturingLogger.Info(msg, args...)
}

func randomArgs(r *rand.Rand) []any {
	pool := []func() any{
		func() any { return r.Intn(1000) },
		func() any { return "s" + strconv.Itoa(r.Intn(100)) },
		func() any { return r.Float64() > 0.5 },
		func() any { return maskUser{Name: "n" + strconv.Itoa(r.Intn(9)), Password: "p"} },
		func() any { return &maskUser{Name: "m"} },
		func() any { return map[string]any{"a": r.Intn(5), "Password": "x", "n": []int{1, 2}} },
		func() any { return map[string]int{"a": 1, "b": 2} },
		func() any { return map[int]string{1: "a", 2: "b"} },
		func() any { return []string{"a", "b"} },
		func() any { return []maskUser{{Name: "a"}, {Name: "b", Password: "z"}} },
		func() any { return [2]int{1, 2} },
		func() any { return errors.New("err" + strconv.Itoa(r.Intn(9))) },
		func() any { return time.Duration(r.Intn(1e6)) },
		func() any { return slog.String("password", "attr") },
		func() any { return slog.Group("g", "password", "p", "k", 1) },
		func() any { return (*maskUser)(nil) },
		func() any { return nil },
	}
	n := 1 + r.Intn(5)
	args := make([]any, 0, n*2)
	for i := range n {
		pick := pool[r.Intn(len(pool))]()
		if _, isAttr := pick.(slog.Attr); isAttr {
			args = append(args, pick)
			continue
		}
		args = append(args, "key"+strconv.Itoa(i), pick)
	}
	return args
}

type slowOutput struct{ delay time.Duration }

func (w slowOutput) Write(p []byte) (int, error) {
	time.Sleep(w.delay)
	return len(p), nil
}

func asyncBenchLogger(b *testing.B, out io.Writer) Logger {
	b.Helper()
	s := logpipe.New(out)
	s.StartAsync()
	b.Cleanup(func() { s.StopAsync(time.Minute) })
	return newLogSink(s, false, &LogOptions{Format: FormatJSON})
}

type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuf) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

type holdWriter struct {
	mu      sync.Mutex
	gate    chan struct{}
	entered chan struct{}
	buf     bytes.Buffer
}

func newHoldWriter() *holdWriter {
	return &holdWriter{entered: make(chan struct{}, 1<<12), gate: make(chan struct{})}
}

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
	select {
	case w.entered <- struct{}{}:
	default:
	}
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

func asyncLogger(t *testing.T, w io.Writer, opts LogOptions) (Logger, *logpipe.Sink) {
	t.Helper()
	s := logpipe.New(w)
	s.StartAsync()
	t.Cleanup(func() {
		if h, ok := w.(*holdWriter); ok {
			h.openAll()
		}
		s.StopAsync(time.Minute)
	})
	return newLogSink(s, false, &opts), s
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

type pipeCountingValuer struct{ calls *int }

func (c pipeCountingValuer) LogValue() slog.Value {
	*c.calls++
	return slog.StringValue("resolved")
}

func textOverhead(t *testing.T) int {
	t.Helper()
	buf := &bytes.Buffer{}
	l := newLogSink(logpipe.New(buf), false, &LogOptions{Format: FormatText})
	l.Info("a")
	return buf.Len() - 1
}

func textLineOfLength(t *testing.T, total int) string {
	t.Helper()
	buf := &bytes.Buffer{}
	l := newLogSink(logpipe.New(buf), false, &LogOptions{Format: FormatText})
	l.Info(strings.Repeat("a", total-textOverhead(t)))
	return buf.String()
}

type exitProbe struct {
	mu    sync.Mutex
	codes []int
	seen  []string
	on    func() string
	ch    chan int
}

func newExitProbe(on func() string) *exitProbe {
	return &exitProbe{on: on, ch: make(chan int, 4)}
}

func (e *exitProbe) install(t *testing.T) {
	t.Helper()
	saved := exit
	exit = func(code int) {
		e.mu.Lock()
		e.codes = append(e.codes, code)
		if e.on != nil {
			e.seen = append(e.seen, e.on())
		}
		e.mu.Unlock()
		e.ch <- code
	}
	t.Cleanup(func() { exit = saved })
}

func fatalOnStuckOutput(t *testing.T, async bool) {
	t.Helper()
	w := newHoldWriter()
	t.Cleanup(w.openAll)
	s := logpipe.New(w)
	if async {
		s.StartAsync()
	}
	t.Cleanup(func() {
		w.openAll()
		if async {
			s.StopAsync(time.Minute)
		}
	})
	l := newLogSink(s, false, &LogOptions{Format: FormatJSON})
	probe := newExitProbe(nil)
	probe.install(t)
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		l.Fatal("bye")
	}()
	select {
	case code := <-probe.ch:
		if code != 1 {
			t.Error(test.DiffMessage(code, 1, "X1: exit code"))
		}
	case <-time.After(30 * time.Second):
		t.Fatal(test.DiffMessage("Fatal still blocked after 30s", "exit(1) after the flush timeout", "bug 1, X1: Fatal never hangs on a stuck output"))
	}
	<-returned
}

type fatalAdapter struct {
	*fromAdapter
	onFatal func()
	fatals  int
}

func (f *fatalAdapter) Fatal(msg string, args ...any) {
	f.fatals++
	if f.onFatal != nil {
		f.onFatal()
	}
	f.fromAdapter.Fatal(msg, args...)
}

type tagPerson struct {
	Age    int    `log:"age"`
	name   string `log:"name"`
	School string
	policy string
}

type tagSkipped struct {
	Keep    string `log:"keep"`
	Skipped string `log:"-"`
}

type tagAddress struct {
	City    string `log:"city"`
	country string `log:"country"`
}

type tagPersonWithAddress struct {
	Name string     `log:"name"`
	Addr tagAddress `log:"addr"`
}

type capturingLogger struct {
	mu       sync.Mutex
	lastArgs []any
}

func (c *capturingLogger) capture(args []any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastArgs = args
}

func (c *capturingLogger) args() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastArgs
}

func (c *capturingLogger) Debug(_ string, args ...any) { c.capture(args) }

func (c *capturingLogger) Info(_ string, args ...any) { c.capture(args) }

func (c *capturingLogger) Warn(_ string, args ...any) { c.capture(args) }

func (c *capturingLogger) Error(_ string, args ...any) { c.capture(args) }

func (c *capturingLogger) Fatal(_ string, args ...any) { c.capture(args) }

type fnLogValuerStub struct {
	secret string
}

func (fnLogValuerStub) LogValue() slog.Value {
	return slog.StringValue("resolved")
}

var allFormats = []Format{FormatJSON, FormatText, FormatPretty}

func formatName(f Format) string {
	switch f {
	case FormatJSON:
		return "JSON"
	case FormatText:
		return "Text"
	default:
		return "Pretty"
	}
}

func newTestLog(format Format, opts LogOptions) (Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	opts.Format = format
	if opts.Level == LevelInfo {
		opts.Level = LevelDebug
	}
	return newLog(buf, false, &opts), buf
}

func jsonLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(strings.TrimRight(line, "\n")), &out); err != nil {
		t.Fatalf("not one JSON object: %v\n%q", err, line)
	}
	return out
}

var ansiCodes = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiCodes.ReplaceAllString(s, "") }

type countingWriter struct {
	buf    bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.buf.Write(p)
}

type atomicCounter struct {
	mu sync.Mutex
	n  int
}

func (c *atomicCounter) add() { c.mu.Lock(); c.n++; c.mu.Unlock() }

func (c *atomicCounter) load() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

type countingStringer struct{ calls *atomicCounter }

func (c countingStringer) String() string { c.calls.add(); return "s" }

type ptrCountingStringer struct{ calls *atomicCounter }

func (c *ptrCountingStringer) String() string { c.calls.add(); return "ps" }

type countingError struct{ calls *atomicCounter }

func (c *countingError) Error() string { c.calls.add(); return "e" }

type countingValuer struct{ calls *atomicCounter }

func (c countingValuer) LogValue() slog.Value { c.calls.add(); return slog.StringValue("v") }

type countingHolder struct{ Inner countingStringer }

type panicStringer struct{}

func (panicStringer) String() string { panic("kaboom") }

type panicError struct{}

func (*panicError) Error() string { panic("kaboom") }

type panicValuer struct{}

func (panicValuer) LogValue() slog.Value { panic("kaboom") }

type derefStringer struct{ s string }

func (d *derefStringer) String() string { return d.s }

type derefError struct{ msg string }

func (e *derefError) Error() string { return e.msg }

type derefValuer struct{ v string }

func (d *derefValuer) LogValue() slog.Value { return slog.StringValue(d.v) }

type resolvedValuer struct{}

func (resolvedValuer) LogValue() slog.Value { return slog.StringValue("resolved") }

type account struct {
	Name     string
	Password string
}

type renamed struct {
	Name   string `log:"name"`
	Hidden string `log:"-"`
	secret string
	token  string `log:"tok"`
	Plain  string
}

type chain struct {
	Leaf string
	Next *chain
}

type item struct{ N int }

func logOnce(format Format, opts LogOptions, msg string, args ...any) string {
	logger, buf := newTestLog(format, opts)
	logger.Info(msg, args...)
	return buf.String()
}

func selfReferencing() *chain {
	n := &chain{Leaf: "x"}
	n.Next = n
	return n
}

func finishesWithin(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal(test.DiffMessage("still running after 2s", "returned", what))
	}
}

func chainOf(links int) *chain {
	head := &chain{Leaf: "leaf-0"}
	cur := head
	for i := 1; i <= links; i++ {
		cur.Next = &chain{Leaf: "leaf-" + strconv.Itoa(i)}
		cur = cur.Next
	}
	return head
}

func panicProbes() map[string]any {
	return map[string]any{"Stringer": panicStringer{}, "Error": &panicError{}, "LogValuer": panicValuer{}}
}
