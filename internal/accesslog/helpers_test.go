package accesslog

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlky.com/owlky/internal/logpipe"
	"go.owlky.com/owlky/internal/test"
	"go.owlky.com/owlky/internal/trace"
	"go.owlky.com/owlky/log"
)

type discardLogger struct {
	lines atomic.Int64
}

func (d *discardLogger) Debug(string, ...any) {}

func (d *discardLogger) Warn(string, ...any) {}

func (d *discardLogger) Error(string, ...any) {}

func (d *discardLogger) Fatal(string, ...any) {}

func (d *discardLogger) Info(string, ...any) { d.lines.Add(1) }

func benchStages(id string, n int) []trace.Event {
	names := []struct{ stage, name string }{
		{trace.StageMiddleware, "cors.CORS"},
		{trace.StageMiddleware, "csrf.CSRF"},
		{trace.StageMiddleware, "helmet.Helmet"},
		{trace.StageGuard, "auth.AuthGuard"},
		{trace.StagePreInterceptor, "log.LogInterceptor"},
		{trace.StageHandler, "READ_users"},
	}
	events := make([]trace.Event, 0, n+1)
	for i := range n {
		events = append(events, trace.Event{ID: id, TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", ParentID: id, Stage: names[i].stage, Name: names[i].name, Transport: trace.TransportHTTP, Duration: time.Duration(40+i*20) * time.Microsecond})
	}
	return append(events, trace.Event{ID: id, TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: id, Stage: trace.StageComplete, Transport: trace.TransportHTTP, Operation: "GET", Target: "/api/v1/users", Code: 200, Duration: 3 * time.Millisecond})
}

func benchBuiltinLine(b *testing.B, opts *log.LogOptions) {
	b.Helper()
	restore := logpipe.SetStdout(io.Discard)
	defer restore()
	tr := &trace.Tracer{}
	al := NewAccessLog(&AccessLogOptions{Tracer: tr, Logger: log.NewLog(opts)})
	defer al.Stop()
	events := benchStages("0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0", 6)
	b.ReportAllocs()
	for b.Loop() {
		for _, e := range events {
			tr.Emit(e)
		}
	}
}

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
	al := NewAccessLog(&AccessLogOptions{Tracer: tr, Logger: logger})
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
	opts := &log.LogOptions{Level: log.LevelDebug}
	switch format {
	case 0:
		opts.Format = log.FormatJSON
	case 1:
		opts.Format = log.FormatText
	default:
		opts.Format = log.FormatPretty
	}
	tr := &trace.Tracer{}
	al := NewAccessLog(&AccessLogOptions{Tracer: tr, Logger: log.NewLog(opts)})
	t.Cleanup(al.Stop)
	return tr, out
}

func spreadID(i int) string {
	return fmt.Sprintf("%016x-%04x", uint64(i+1)*0x9E3779B97F4A7C15, i&0xffff)
}

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")
