package log

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

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
func (a *fromAdapter) Info(msg string, args ...any)  { a.record("info", msg, args) }
func (a *fromAdapter) Warn(msg string, args ...any)  { a.record("warn", msg, args) }
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

func TestLogger_ExtendsLogAdapterWithFrom(t *testing.T) {
	adapter := new(fromAdapter)
	var _ LogAdapter = adapter
	logger := reflect.TypeFor[Logger]()
	base := reflect.TypeFor[LogAdapter]()
	if base.NumMethod() != 5 || logger.NumMethod() != 6 {
		t.Error(test.DiffMessage([]int{base.NumMethod(), logger.NumMethod()}, []int{5, 6}, "I1: LogAdapter has the five methods, Logger adds From"))
	}
	if !logger.Implements(base) {
		t.Error(test.DiffMessage(false, true, "I1: a Logger is a LogAdapter"))
	}
	if reflect.TypeOf(adapter).Implements(logger) || !reflect.TypeOf(adapter).Implements(base) {
		t.Error(test.DiffMessage(true, false, "I1: a type with the five methods and no From is a LogAdapter, not a Logger"))
	}
}

func TestWrapLogger_AcceptsFiveMethodAdapter(t *testing.T) {
	adapter := &fromAdapter{}
	logger := WrapLogger(adapter, nil)
	logger.Info("hello", "k", "v")
	if calls := adapter.all(); len(calls) != 1 || calls[0].msg != "hello" {
		t.Error(test.DiffMessage(calls, "one call", "I1: WrapLogger accepts a type with five methods"))
	}
	var nilAdapter LogAdapter
	defer func() {
		if recover() == nil {
			t.Error(test.DiffMessage("no panic", "panic", "I1: WrapLogger(nil, ...) panics as today"))
		}
	}()
	WrapLogger(nilAdapter, nil)
}

func TestLogger_WithoutFromUnchanged(t *testing.T) {
	l, buf := newTestLog(FormatJSON, LogOptions{})
	l.Info("hello", "a", 1, "b", "two")
	got := jsonTime.ReplaceAllString(buf.String(), `"time":""`)
	want := `{"time":"","level":"INFO","msg":"hello","a":1,"b":"two"}` + "\n"
	if got != want {
		t.Error(test.DiffMessage(got, want, "I3: a call that does not use From writes the same bytes"))
	}
	adapter := &fromAdapter{}
	WrapLogger(adapter, []string{"password"}).Info("m", "password", "x", "k", 1)
	if calls := adapter.all(); len(calls) != 1 || !slices.Equal(calls[0].args, []any{"password", maskPlaceholder, "k", 1}) {
		t.Error(test.DiffMessage(calls, "[password [REDACTED] k 1]", "I3: the same arguments reach an adapter"))
	}
}

func TestFrom_NilContextReturnsReceiver(t *testing.T) {
	var nilCtx context.Context
	builtin, _ := newTestLog(FormatJSON, LogOptions{})
	wrapped := WrapLogger(&fromAdapter{}, nil)
	for name, l := range map[string]Logger{"built-in": builtin, "adapter": wrapped} {
		if got := l.From(nilCtx); got != l {
			t.Error(test.DiffMessage(got, l, "F1: From(nil) returns the receiver, "+name))
		}
	}
}

func TestFrom_NoScopeReturnsReceiver(t *testing.T) {
	builtin, _ := newTestLog(FormatJSON, LogOptions{})
	wrapped := WrapLogger(&fromAdapter{}, nil)
	for name, l := range map[string]Logger{"built-in": builtin, "adapter": wrapped} {
		if got := l.From(context.Background()); got != l {
			t.Error(test.DiffMessage(got, l, "F1: a context without a scope returns the receiver, "+name))
		}
	}
}

func TestFrom_EmptyFieldsReturnsReceiver(t *testing.T) {
	builtin, _ := newTestLog(FormatJSON, LogOptions{})
	wrapped := WrapLogger(&fromAdapter{}, nil)
	ctx := scopeContext(tracecontext.Scope{})
	for name, l := range map[string]Logger{"built-in": builtin, "adapter": wrapped} {
		if got := l.From(ctx); got != l {
			t.Error(test.DiffMessage(got, l, "F1: a scope with no fields returns the receiver, "+name))
		}
	}
}

func TestFrom_BuiltinAddsFieldsAfterArgs_JSON(t *testing.T) {
	l, buf := newTestLog(FormatJSON, LogOptions{})
	l.From(requestContext()).Info("hello", "a", 1, "b", "two")
	got := withoutTime(buf.String())
	want := `{"time":"","level":"INFO","msg":"hello","a":1,"b":"two","trace_id":"` + fromTraceID + `","request":"` + fromRequest + `"}` + "\n"
	if got != want {
		t.Error(test.DiffMessage(got, want, "F3: the fields follow the call's own attributes, in the order of the scope"))
	}
}

func TestFrom_BuiltinAddsFieldsAfterArgs_Text(t *testing.T) {
	l, buf := newTestLog(FormatText, LogOptions{})
	l.From(requestContext()).Info("hello", "a", 1)
	got := withoutTime(buf.String())
	want := "time= level=INFO msg=hello a=1 trace_id=" + fromTraceID + " request=" + fromRequest + "\n"
	if got != want {
		t.Error(test.DiffMessage(got, want, "F3: Text format, fields after the arguments"))
	}
}

func TestFrom_BuiltinAddsFieldsAfterArgs_Pretty(t *testing.T) {
	scoped, scopedBuf := newTestLog(FormatPretty, LogOptions{})
	scoped.From(requestContext()).Info("hello", "a", 1)
	plainLogger, plainBuf := newTestLog(FormatPretty, LogOptions{})
	plainLogger.Info("hello", "a", 1, "trace_id", fromTraceID, "request", fromRequest)
	if got, want := withoutTime(scopedBuf.String()), withoutTime(plainBuf.String()); got != want {
		t.Error(test.DiffMessage(got, want, "F3: Pretty prints the fields as rows after the arguments"))
	}
	out := plain(scopedBuf.String())
	if strings.Index(out, "── a ") >= strings.Index(out, "── trace_id") || strings.Index(out, "── trace_id") >= strings.Index(out, "── request") {
		t.Error(test.DiffMessage(out, "a, trace_id, request in order", "F3: row order"))
	}
}

func TestFrom_AllFieldsInScopeOrder(t *testing.T) {
	l, buf := newTestLog(FormatJSON, LogOptions{})
	ctx := scopeContext(tracecontext.Scope{TraceID: fromTraceID, Request: fromRequest, Connection: "c0c0c0c0c0c0c0c0", Message: "m1m1m1m1", Upstream: "up-1"})
	l.From(ctx).Info("m")
	got := withoutTime(buf.String())
	want := `{"time":"","level":"INFO","msg":"m","trace_id":"` + fromTraceID + `","request":"` + fromRequest + `","connection":"c0c0c0c0c0c0c0c0","message":"m1m1m1m1","upstream":"up-1"}` + "\n"
	if got != want {
		t.Error(test.DiffMessage(got, want, "F3: the order of design 0011 L1, empty values skipped"))
	}
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

func TestFrom_FastPathEqualsSlowPath(t *testing.T) {
	alphabet := []string{"a", "Z", "0", ".", ":", "-", "_", " ", "\"", "\n", "é", "\x1b", "\\", "/", "日"}
	for _, format := range allFormats {
		r := rand.New(rand.NewSource(12))
		for iter := range 250 {
			scope := tracecontext.Scope{
				TraceID:    fromRandomString(r, alphabet, 40),
				Request:    fromRandomString(r, alphabet[:6], 20),
				Connection: fromRandomString(r, alphabet[:6], 20),
				Message:    fromRandomString(r, alphabet, 150),
				Upstream:   fromRandomString(r, alphabet[:7], 70),
			}
			if iter%5 == 0 {
				scope.TraceID, scope.Request = fromTraceID, fromRequest
			}
			ctx := scopeContext(scope)
			fields := scope.AppendArgs(nil)
			args := fromRandomArgs(r)
			opts := LogOptions{}
			if iter%2 == 1 {
				opts.MaskFields = []string{"request"}
			}
			scoped, scopedBuf := newTestLog(format, opts)
			plainLogger, plainBuf := newTestLog(format, opts)
			scoped.From(ctx).Info("msg", args...)
			plainLogger.Info("msg", append(slices.Clone(args), fields...)...)
			if got, want := withoutTime(scopedBuf.String()), withoutTime(plainBuf.String()); got != want {
				t.Fatal(test.DiffMessage(got, want, fmt.Sprintf("F3, H3: From equals the arguments with the fields appended, %s iteration %d", formatName(format), iter)))
			}
		}
	}
}

func TestFrom_MalformedArgsKeepFields(t *testing.T) {
	l, buf := newTestLog(FormatJSON, LogOptions{})
	ctx := requestContext()
	l.From(ctx).Info("m", "lonely")
	l.From(ctx).Info("m", 5, "x")
	lines := strings.Split(strings.TrimSuffix(withoutTime(buf.String()), "\n"), "\n")
	fields := `"trace_id":"` + fromTraceID + `","request":"` + fromRequest + `"}`
	if len(lines) != 2 || !strings.HasSuffix(lines[0], `"!BADKEY":"lonely",`+fields) {
		t.Error(test.DiffMessage(lines, `... "!BADKEY":"lonely", fields`, "F3: a dangling key is !BADKEY and the fields never pair with it"))
	}
	if len(lines) == 2 && (!strings.Contains(lines[1], `"!BADKEY":5`) || !strings.HasSuffix(lines[1], fields)) {
		t.Error(test.DiffMessage(lines[1], "!BADKEY pairs then the fields", "F3: a non-string key is handled as without From, the fields stay pairs"))
	}
}

func TestFrom_MaskRuleMasksField(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatText, FormatPretty} {
		l, buf := newTestLog(format, LogOptions{MaskFields: []string{"request"}})
		l.From(requestContext()).Info("m", "k", "v")
		out := plain(buf.String())
		if strings.Contains(out, fromRequest) || !strings.Contains(out, maskPlaceholder) || !strings.Contains(out, fromTraceID) {
			t.Error(test.DiffMessage(out, "request masked, trace_id kept", "F3: a field whose key matches a mask rule is [REDACTED], "+formatName(format)))
		}
	}
}

func TestFrom_AdapterGetsFieldsFirst(t *testing.T) {
	adapter := &fromAdapter{}
	l := WrapLogger(adapter, nil)
	l.From(requestContext()).Warn("hello", "a", 1, "lonely")
	calls := adapter.all()
	if len(calls) != 1 {
		t.Fatal(test.DiffMessage(len(calls), 1, "F4: the adapter's method is called once"))
	}
	want := []any{"trace_id", fromTraceID, "request", fromRequest, "a", 1, "lonely"}
	if calls[0].level != "warn" || calls[0].msg != "hello" || !slices.Equal(calls[0].args, want) {
		t.Error(test.DiffMessage(calls[0], want, "F4: msg, then the fields, then the converted arguments"))
	}
}

func TestFrom_AdapterMasksFields(t *testing.T) {
	adapter := &fromAdapter{}
	l := WrapLogger(adapter, []string{"request", "password"})
	l.From(requestContext()).Info("m", "password", "secret")
	want := []any{"trace_id", fromTraceID, "request", maskPlaceholder, "password", maskPlaceholder}
	if calls := adapter.all(); len(calls) != 1 || !slices.Equal(calls[0].args, want) {
		t.Error(test.DiffMessage(calls, want, "F4: the fields and the arguments are masked by the wrapper's rules"))
	}
}

func TestFrom_AdapterChainOfWrappersKeepsRules(t *testing.T) {
	adapter := &fromAdapter{}
	l := WrapLogger(WrapLogger(adapter, []string{"password"}), []string{"request"})
	l.From(requestContext()).Info("m", "password", "secret")
	want := []any{"trace_id", fromTraceID, "request", maskPlaceholder, "password", maskPlaceholder}
	if calls := adapter.all(); len(calls) != 1 || !slices.Equal(calls[0].args, want) {
		t.Error(test.DiffMessage(calls, want, "F4: rules of every wrapper apply to a scoped adapter"))
	}
}

func TestFrom_DuplicateKeyKept(t *testing.T) {
	adapter := &fromAdapter{}
	WrapLogger(adapter, nil).From(requestContext()).Info("m", "trace_id", "mine")
	want := []any{"trace_id", fromTraceID, "request", fromRequest, "trace_id", "mine"}
	if calls := adapter.all(); len(calls) != 1 || !slices.Equal(calls[0].args, want) {
		t.Error(test.DiffMessage(calls, want, "F5: a key the caller also passes is not deduplicated"))
	}
	l, buf := newTestLog(FormatJSON, LogOptions{})
	l.From(requestContext()).Info("m", "trace_id", "mine")
	if n := strings.Count(buf.String(), `"trace_id"`); n != 2 {
		t.Error(test.DiffMessage(n, 2, "F5: the built-in logger keeps both"))
	}
}

func TestFrom_SecondScopeReplaces(t *testing.T) {
	a := scopeContext(tracecontext.Scope{TraceID: "aaaa", Request: "a1"})
	b := scopeContext(tracecontext.Scope{TraceID: "bbbb", Request: "b1"})
	l, buf := newTestLog(FormatJSON, LogOptions{})
	l.From(a).From(b).Info("m")
	l.From(b).Info("m")
	lines := strings.Split(strings.TrimSuffix(withoutTime(buf.String()), "\n"), "\n")
	if len(lines) != 2 || lines[0] != lines[1] || strings.Contains(lines[0], "aaaa") {
		t.Error(test.DiffMessage(lines, "two equal lines with b only", "F6: scopes replace, they do not stack"))
	}
	if got := l.From(a).From(context.Background()); got != l.From(a) {
		t.Error(test.DiffMessage(got, l.From(a), "F6: From without a scope after From(a) is From(a)"))
	}
	adapter := &fromAdapter{}
	w := WrapLogger(adapter, nil)
	w.From(a).From(b).Info("m")
	if calls := adapter.all(); len(calls) != 1 || !slices.Equal(calls[0].args, []any{"trace_id", "bbbb", "request", "b1"}) {
		t.Error(test.DiffMessage(calls, "b fields only", "F6: scopes replace for an adapter too"))
	}
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

func TestFrom_BelowLevelDoesNoWork(t *testing.T) {
	buf := &syncBuf{}
	l := newLog(buf, false, &LogOptions{Format: FormatJSON, Level: LevelWarn, MaskFields: []string{"k"}})
	scoped := l.From(requestContext())
	scoped.Debug("m", "k", failingValue{t}, "nested", map[string]any{"v": failingValue{t}})
	scoped.Info("m", "k", failingValue{t})
	if buf.String() != "" {
		t.Error(test.DiffMessage(buf.String(), "", "F7: nothing is written below the level"))
	}
}

func TestFrom_AddSourceIsCaller(t *testing.T) {
	l, buf := newTestLog(FormatJSON, LogOptions{IsAddSource: true})
	_, _, line, _ := runtime.Caller(0)
	l.From(requestContext()).Info("m", "a", 1)
	var out struct {
		Source struct {
			Function string `json:"function"`
			File     string `json:"file"`
			Line     int    `json:"line"`
		} `json:"source"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.Source.File, "log_from_test.go") || !strings.Contains(out.Source.Function, "TestFrom_AddSourceIsCaller") || out.Source.Line != line+1 {
		t.Error(test.DiffMessage(out.Source, fmt.Sprintf("log_from_test.go:%d in TestFrom_AddSourceIsCaller", line+1), "F8: the source is the caller of Info, as without From"))
	}
}

func TestFrom_DoesNotModifyReceiver(t *testing.T) {
	l, buf := newTestLog(FormatJSON, LogOptions{})
	_ = l.From(requestContext())
	l.Info("m")
	if strings.Contains(buf.String(), "trace_id") {
		t.Error(test.DiffMessage(buf.String(), "no fields", "F2: the receiver is not modified"))
	}
}

func TestFrom_ConcurrentSameScopeNoRace(t *testing.T) {
	buf := &syncBuf{}
	l := newLog(buf, false, &LogOptions{Format: FormatJSON})
	ctx := requestContext()
	adapter := &fromAdapter{}
	w := WrapLogger(adapter, nil)
	const goroutines, per = 32, 50
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range per {
				l.From(ctx).Info("m", "k", 1)
				w.From(ctx).Info("m", "k", 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != goroutines*per {
		t.Fatal(test.DiffMessage(len(lines), goroutines*per, "F9: every call writes its line"))
	}
	first := withoutTime(lines[0])
	for _, line := range lines {
		if withoutTime(line) != first {
			t.Fatal(test.DiffMessage(line, first, "F9: the output is the same whichever goroutine built the scoped logger"))
		}
	}
	for _, c := range adapter.all() {
		if !slices.Equal(c.args, []any{"trace_id", fromTraceID, "request", fromRequest, "k", 1}) {
			t.Fatal(test.DiffMessage(c.args, "fields then k 1", "F9: the adapter gets the same arguments"))
		}
	}
}

func TestFrom_AfterRequestStillHasFields(t *testing.T) {
	buf := &syncBuf{}
	l := newLog(buf, false, &LogOptions{Format: FormatJSON})
	base := requestContext()
	ctx, cancel := context.WithCancel(base)
	scoped := l.From(ctx)
	cancel()
	scoped.Info("late")
	l.From(ctx).Info("later")
	for _, line := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		if !strings.Contains(line, `"request":"`+fromRequest+`"`) {
			t.Error(test.DiffMessage(line, "a line with the request", "F9: a scoped logger keeps its fields after the request ended"))
		}
	}
}

func TestFrom_CachedPerScope(t *testing.T) {
	ctx := requestContext()
	l, _ := newTestLog(FormatJSON, LogOptions{})
	first := l.From(ctx)
	if second := l.From(ctx); second != first {
		t.Error(test.DiffMessage(second, first, "C5, F9: From on the same scope and logger returns the cached logger"))
	}
	if tracecontext.ScopeOf(ctx).LogSlot.Load() == nil {
		t.Error(test.DiffMessage(nil, "the scoped logger", "C5: the scoped logger is stored in Scope.LogSlot"))
	}
	if allocs := testing.AllocsPerRun(200, func() { _ = l.From(ctx) }); allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0, "C5: a later From on the scope allocates nothing"))
	}
	w := WrapLogger(&fromAdapter{}, nil)
	_ = w.From(ctx)
	if allocs := testing.AllocsPerRun(200, func() { _ = w.From(ctx) }); allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0, "C5: a later From on an adapter wrapper allocates nothing"))
	}
}

func TestFrom_NoScopeAllocatesNothing(t *testing.T) {
	l, _ := newTestLog(FormatJSON, LogOptions{})
	ctx := context.Background()
	if allocs := testing.AllocsPerRun(200, func() { _ = l.From(ctx) }); allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0, "C1: From on a context without a scope allocates nothing"))
	}
}

func TestFrom_DifferentLoggersSameScopeDoNotMix(t *testing.T) {
	ctx := requestContext()
	jsonLogger, jsonBuf := newTestLog(FormatJSON, LogOptions{})
	textLogger, textBuf := newTestLog(FormatText, LogOptions{})
	maskedLogger, maskedBuf := newTestLog(FormatJSON, LogOptions{MaskFields: []string{"request"}})
	adapter := &fromAdapter{}
	wrapped := WrapLogger(adapter, nil)
	for range 3 {
		jsonLogger.From(ctx).Info("m")
		textLogger.From(ctx).Info("m")
		maskedLogger.From(ctx).Info("m")
		wrapped.From(ctx).Info("m")
	}
	if !strings.HasPrefix(jsonBuf.String(), "{") || strings.Contains(jsonBuf.String(), "level=") {
		t.Error(test.DiffMessage(jsonBuf.String(), "JSON only", "F9: a scope shared by two loggers never mixes their output"))
	}
	if strings.HasPrefix(textBuf.String(), "{") || !strings.Contains(textBuf.String(), "request="+fromRequest) {
		t.Error(test.DiffMessage(textBuf.String(), "text only", "F9: the text logger keeps its format"))
	}
	if strings.Contains(maskedBuf.String(), fromRequest) || !strings.Contains(maskedBuf.String(), maskPlaceholder) {
		t.Error(test.DiffMessage(maskedBuf.String(), "request masked", "F9: a logger's rules never leak to another's scoped logger"))
	}
	if n := len(adapter.all()); n != 3 {
		t.Error(test.DiffMessage(n, 3, "F9: the adapter received its own calls only"))
	}
}

func TestFrom_FatalWritesFieldsThenExits(t *testing.T) {
	buf := &syncBuf{}
	l := newLog(buf, false, &LogOptions{Format: FormatJSON})
	saved := exit
	var code int
	var atExit string
	exit = func(c int) {
		code = c
		atExit = buf.String()
	}
	defer func() { exit = saved }()
	l.From(requestContext()).Fatal("bye", "k", 1)
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "F10: Fatal exits with 1"))
	}
	if !strings.Contains(atExit, `"level":"FATAL"`) || !strings.Contains(atExit, `"k":1,"trace_id":"`+fromTraceID+`","request":"`+fromRequest+`"`) {
		t.Error(test.DiffMessage(atExit, "a FATAL record with the fields, written before exit", "F10: the record is written with the fields, then X1"))
	}
}
