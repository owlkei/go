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
	"net/url"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/logpipe"
	"go.owlky.com/owlky/internal/test"
	"go.owlky.com/owlky/internal/tracecontext"
)

func TestNewLog_NilOptions(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := newLog(buf, false, nil)
	logger.Debug("hidden")
	logger.Info("shown", "password", "visible")
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Error(test.DiffMessage(out, "no debug line", "L1: nil options mean level Info"))
	}
	if !strings.Contains(out, "shown") || !strings.Contains(out, "visible") {
		t.Error(test.DiffMessage(out, "the info line, nothing masked", "L1: nil options mean no mask rule"))
	}
	if NewLog(nil) == nil {
		t.Error(test.DiffMessage(nil, "a logger", "L1: NewLog(nil) returns a logger"))
	}
}

func TestNewLog_DoesNotModifyOptions(t *testing.T) {
	opts := &LogOptions{Level: -1, Format: FormatJSON, IsAddSource: true, MaskFields: []string{"password", "token"}}
	want := &LogOptions{Level: -1, Format: FormatJSON, IsAddSource: true, MaskFields: []string{"password", "token"}}
	NewLog(opts)
	newLog(io.Discard, false, opts)
	WrapLogger(&instanceRecorder{}, opts.MaskFields)
	if !reflect.DeepEqual(opts, want) {
		t.Error(test.DiffMessage(opts, want, "L2: NewLog and WrapLogger never modify their arguments"))
	}
}

func TestNewLog_LaterOptionChangesIgnored(t *testing.T) {
	buf := &bytes.Buffer{}
	opts := &LogOptions{Format: FormatJSON, MaskFields: []string{"password"}}
	logger := newLog(buf, false, opts)
	opts.Level = LevelError
	opts.MaskFields[0] = "other"
	opts.MaskFields = append(opts.MaskFields, "more")
	logger.Info("x", "password", "secret", "other", "kept")
	out := buf.String()
	if !strings.Contains(out, `"password":"[REDACTED]"`) || !strings.Contains(out, `"other":"kept"`) {
		t.Error(test.DiffMessage(out, "password masked, other kept, info shown", "L2: later changes to the options have no effect"))
	}
}

func TestLog_JSONLine(t *testing.T) {
	logger, buf := newTestLog(FormatJSON, LogOptions{})
	logger.Info("hello", "k", "v", "n", 3)
	line := jsonLine(t, buf.String())
	if line["msg"] != "hello" || line["level"] != "INFO" || line["k"] != "v" || line["n"] != float64(3) {
		t.Error(test.DiffMessage(line, "msg hello, level INFO, k v, n 3", "L4: JSON uses slog's keys"))
	}
	if _, ok := line["time"]; !ok {
		t.Error(test.DiffMessage(line, "a time key", "L4: JSON has time"))
	}
	if !strings.HasSuffix(buf.String(), "\n") || strings.Count(buf.String(), "\n") != 1 {
		t.Error(test.DiffMessage(buf.String(), "exactly one line", "L4: one JSON object per line"))
	}
}

func TestLog_TextLine(t *testing.T) {
	logger, buf := newTestLog(FormatText, LogOptions{})
	logger.Warn("hello", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "msg=hello") || !strings.Contains(out, "k=v") || !strings.Contains(out, "time=") {
		t.Error(test.DiffMessage(out, "time=… level=WARN msg=hello k=v", "L4: Text is slog's key=value line"))
	}
}

func TestLog_LevelLabels(t *testing.T) {
	saved := exit
	exit = func(int) {}
	defer func() { exit = saved }()

	for _, format := range allFormats {
		logger, buf := newTestLog(format, LogOptions{})
		logger.Debug("m")
		logger.Info("m")
		logger.Warn("m")
		logger.Error("m")
		logger.Fatal("m")
		out := buf.String()
		for _, label := range []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"} {
			if !strings.Contains(out, label) {
				t.Error(test.DiffMessage(out, "contains "+label, "L4: "+formatName(format)+" writes the level "+label))
			}
		}
	}
}

func TestLog_LevelLabelsJSONAndText(t *testing.T) {
	saved := exit
	exit = func(int) {}
	defer func() { exit = saved }()

	jsonLogger, jsonBuf := newTestLog(FormatJSON, LogOptions{})
	textLogger, textBuf := newTestLog(FormatText, LogOptions{})
	for _, logger := range []Logger{jsonLogger, textLogger} {
		logger.Debug("m")
		logger.Info("m")
		logger.Warn("m")
		logger.Error("m")
		logger.Fatal("m")
	}
	for _, label := range []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"} {
		if !strings.Contains(jsonBuf.String(), `"level":"`+label+`"`) {
			t.Error(test.DiffMessage(jsonBuf.String(), `"level":"`+label+`"`, "L4: JSON level label"))
		}
		if !strings.Contains(textBuf.String(), "level="+label+" ") {
			t.Error(test.DiffMessage(textBuf.String(), "level="+label, "L4: Text level label"))
		}
	}
}

func TestLog_DefaultFormatTerminal(t *testing.T) {
	buf := &bytes.Buffer{}
	newLog(buf, true, nil).Info("hello", "k", "v")
	out := buf.String()
	if !strings.HasPrefix(out, "\n") || !strings.Contains(out, "└──") || strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Error(test.DiffMessage(out, "the Pretty tree", "L4: with LogFormat zero, a terminal gets Pretty"))
	}
}

func TestLog_DefaultFormatNotTerminal(t *testing.T) {
	buf := &bytes.Buffer{}
	newLog(buf, false, nil).Info("hello", "k", "v")
	line := jsonLine(t, buf.String())
	if line["msg"] != "hello" || strings.Contains(buf.String(), "\x1b") {
		t.Error(test.DiffMessage(buf.String(), "a JSON line without ANSI codes", "L4: with LogFormat zero, anything else gets JSON"))
	}
}

func TestLog_ExplicitFormatBeatsTerminal(t *testing.T) {
	buf := &bytes.Buffer{}
	newLog(buf, true, &LogOptions{Format: FormatJSON}).Info("hello")
	jsonLine(t, buf.String())
	buf.Reset()
	newLog(buf, false, &LogOptions{Format: FormatPretty}).Info("hello")
	if !strings.HasPrefix(buf.String(), "\n") {
		t.Error(test.DiffMessage(buf.String(), "Pretty", "L4: an explicit format wins whatever the output"))
	}
}

func TestLog_BelowLevelWritesNothing(t *testing.T) {
	for _, format := range allFormats {
		buf := &bytes.Buffer{}
		logger := newLog(buf, false, &LogOptions{Format: format, Level: LevelWarn})
		logger.Debug("d", "k", "v")
		logger.Info("i", "k", "v")
		if buf.Len() != 0 {
			t.Error(test.DiffMessage(buf.String(), "", "L3: "+formatName(format)+" writes nothing below the level"))
		}
		logger.Warn("w")
		if buf.Len() == 0 {
			t.Error(test.DiffMessage("", "a line", "L3: "+formatName(format)+" writes at the level"))
		}
	}
}

func TestLog_BelowLevelCallsNoMethod(t *testing.T) {
	var calls atomicCounter
	args := []any{
		"s", countingStringer{&calls},
		"e", &countingError{&calls},
		"v", countingValuer{&calls},
		"ps", &ptrCountingStringer{&calls},
		"st", countingHolder{Inner: countingStringer{&calls}},
	}
	for _, format := range allFormats {
		for _, rules := range [][]string{nil, {"password"}} {
			logger, buf := newTestLog(format, LogOptions{Level: LevelError, MaskFields: rules})
			logger.Debug("x", args...)
			logger.Info("x", args...)
			logger.Warn("x", args...)
			if calls.load() != 0 || buf.Len() != 0 {
				t.Error(test.DiffMessage(calls.load(), 0, "L3: no method of an argument is called below the level ("+formatName(format)+")"))
			}
		}
	}
}

func TestLog_BelowLevelAllocatesNothing(t *testing.T) {
	var calls atomicCounter
	args := []any{"user", &ptrCountingStringer{&calls}, "n", 1, "p", benchUser{Name: "a", Password: "b"}}
	for _, format := range allFormats {
		for _, rules := range [][]string{nil, {"password"}} {
			logger, _ := newTestLog(format, LogOptions{Level: LevelInfo + 1, MaskFields: rules})
			if n := testing.AllocsPerRun(50, func() { logger.Debug("x", args...) }); n != 0 {
				t.Error(test.DiffMessage(n, 0.0, "L3: a dropped line allocates nothing ("+formatName(format)+")"))
			}
		}
	}
}

func TestLog_AddSourceIsCaller(t *testing.T) {
	for _, format := range allFormats {
		logger, buf := newTestLog(format, LogOptions{IsAddSource: true})
		_, file, line, _ := runtime.Caller(0)
		logger.Info("here")
		where := file + ":" + strconv.Itoa(line+1)
		out := buf.String()
		switch format {
		case FormatJSON:
			parsed := jsonLine(t, out)
			src, _ := parsed["source"].(map[string]any)
			if src == nil || src["file"] != file || src["line"] != float64(line+1) {
				t.Error(test.DiffMessage(parsed["source"], where, "L5: JSON source is the caller"))
			}
		default:
			if !strings.Contains(plain(out), where) {
				t.Error(test.DiffMessage(plain(out), "contains "+where, "L5: "+formatName(format)+" names the caller"))
			}
		}
	}
}

func TestLog_AddSourceThroughWrapLogger(t *testing.T) {
	for _, format := range allFormats {
		logger, buf := newTestLog(format, LogOptions{IsAddSource: true})
		wrapped := WrapLogger(logger, []string{"password"})
		_, file, line, _ := runtime.Caller(0)
		wrapped.Info("here", "password", "s")
		where := file + ":" + strconv.Itoa(line+1)
		out := buf.String()
		if format == FormatJSON {
			src, _ := jsonLine(t, out)["source"].(map[string]any)
			if src == nil || src["file"] != file || src["line"] != float64(line+1) {
				t.Error(test.DiffMessage(src, where, "L5: JSON source is the caller through WrapLogger"))
			}
		} else if !strings.Contains(plain(out), where) {
			t.Error(test.DiffMessage(plain(out), "contains "+where, "L5: "+formatName(format)+" names the caller through WrapLogger"))
		}
	}
}

func TestLog_PrettySourceIsLastRow(t *testing.T) {
	logger, buf := newTestLog(FormatPretty, LogOptions{IsAddSource: true})
	logger.Info("here", "k", "v")
	lines := strings.Split(strings.TrimRight(plain(buf.String()), "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "└──") || !strings.Contains(last, "source") || !strings.Contains(last, "log_test.go:") {
		t.Error(test.DiffMessage(last, "└── source … log_test.go:<line>", "L5: Pretty prints source as the last row"))
	}
}

func TestLog_NoSourceWithoutAddSource(t *testing.T) {
	for _, format := range allFormats {
		logger, buf := newTestLog(format, LogOptions{})
		logger.Info("here", "k", "v")
		if strings.Contains(buf.String(), "source") || strings.Contains(buf.String(), "log_test.go") {
			t.Error(test.DiffMessage(buf.String(), "no source", "L5: "+formatName(format)+" without AddSource has no source"))
		}
	}
}

func TestLog_OneWritePerRecord(t *testing.T) {
	for _, format := range allFormats {
		w := &countingWriter{}
		logger := newLog(w, false, &LogOptions{Format: format, Level: LevelDebug})
		logger.Info("m", "a", 1, "b", map[string]int{"x": 1, "y": 2}, "c", benchUser{Name: "n", Password: "p"})
		logger.Error("m")
		if w.writes != 2 {
			t.Error(test.DiffMessage(w.writes, 2, "L6: one Write per record ("+formatName(format)+")"))
		}
	}
}

func TestLog_ConcurrentRecordsNeverInterleave(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatText} {
		buf := &bytes.Buffer{}
		logger := newLog(buf, false, &LogOptions{Format: format, MaskFields: []string{"password"}})
		var wg sync.WaitGroup
		for i := range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range 50 {
					logger.Info("line", "i", i, "j", j, "user", benchUser{Name: "n", Password: "p"})
				}
			}()
		}
		wg.Wait()
		lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
		if len(lines) != 800 {
			t.Fatal(test.DiffMessage(len(lines), 800, "L6: every record is one line ("+formatName(format)+")"))
		}
		if format == FormatJSON {
			for _, line := range lines {
				jsonLine(t, line)
			}
		}
	}
}

func TestLog_ConcurrentCalls_NoRace(t *testing.T) {
	for _, format := range allFormats {
		buf := &bytes.Buffer{}
		logger := newLog(buf, false, &LogOptions{Format: format, MaskFields: []string{"password"}})
		var wg sync.WaitGroup
		for i := range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 50 {
					logger.Info("line", "i", i, "user", benchUser{Name: "n", Password: "p"}, "m", map[string]any{"password": "x"}, "err", errors.New("e"))
				}
			}()
		}
		wg.Wait()
	}
}

func TestLog_FatalLogsThenExits(t *testing.T) {
	saved := exit
	defer func() { exit = saved }()

	for _, format := range allFormats {
		buf := &bytes.Buffer{}
		logger := newLog(buf, false, &LogOptions{Format: format})
		var code = -1
		var written string
		exit = func(c int) {
			code = c
			written = buf.String()
		}
		logger.Fatal("going down", "k", "v")
		if code != 1 {
			t.Error(test.DiffMessage(code, 1, "L7: Fatal exits with 1 ("+formatName(format)+")"))
		}
		if !strings.Contains(written, "FATAL") || !strings.Contains(written, "going down") {
			t.Error(test.DiffMessage(written, "the FATAL record", "L7: the record is written before exit ("+formatName(format)+")"))
		}
	}
}

func TestLog_FatalBelowNothingStillExits(t *testing.T) {
	saved := exit
	defer func() { exit = saved }()
	var code = -1
	exit = func(c int) { code = c }
	newLog(&bytes.Buffer{}, false, &LogOptions{Format: FormatJSON, Level: LevelFatal}).Fatal("x")
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "L7: Fatal always exits"))
	}
}

func TestLog_ColoredIsPlainTextInJSON(t *testing.T) {
	logger, buf := newTestLog(FormatJSON, LogOptions{})
	logger.Info("x", "middleware", []map[string]color.Colored{{"CORS": {Text: "120µs (4%)", Color: color.Red}}})
	out := buf.String()
	if strings.Contains(out, `\u001b`) || strings.Contains(out, "\x1b") || strings.Contains(out, "Color") {
		t.Error(test.DiffMessage(out, "no ANSI code and no Color field", "A4: JSON writes only the Text of a Colored"))
	}
	if !strings.Contains(out, `"middleware":[{"CORS":"120µs (4%)"}]`) {
		t.Error(test.DiffMessage(out, `"middleware":[{"CORS":"120µs (4%)"}]`, "A4: the JSON shape"))
	}
}

func TestLog_ColoredIsPlainTextInText(t *testing.T) {
	logger, buf := newTestLog(FormatText, LogOptions{})
	logger.Info("x", "d", color.Colored{Text: "120µs (4%)", Color: color.Red})
	out := buf.String()
	if strings.Contains(out, "\x1b") || strings.Contains(out, `\x1b`) || !strings.Contains(out, "120µs (4%)") {
		t.Error(test.DiffMessage(out, "the text without ANSI code", "A4: Text writes only the Text of a Colored"))
	}
}

func TestLog_JSONEscapesControlChars(t *testing.T) {
	logger, buf := newTestLog(FormatJSON, LogOptions{})
	msg := "/a\n\x1b[2K FAKE\r\nERROR forged\u0085"
	logger.Info(msg, "k\n\x1b", "v\n\x1b[2J")
	out := buf.String()
	if strings.Count(out, "\n") != 1 || strings.ContainsAny(out, "\x1b\r\u0085") {
		t.Error(test.DiffMessage(out, "one line, no raw control", "F4: JSON escapes control characters"))
	}
	if jsonLine(t, out)["msg"] != msg {
		t.Error(test.DiffMessage(jsonLine(t, out)["msg"], msg, "F4: the message survives the round trip"))
	}
}

func TestLog_TextEscapesControlChars(t *testing.T) {
	logger, buf := newTestLog(FormatText, LogOptions{})
	logger.Info("/a\n\x1b[2K FAKE\r\nERROR forged", "k", "v\n\x1b[2J")
	out := buf.String()
	if strings.Count(out, "\n") != 1 || strings.ContainsAny(out, "\x1b\r") {
		t.Error(test.DiffMessage(out, "one line, no raw control", "F4: Text escapes control characters"))
	}
}

func TestLog_AttrArgument(t *testing.T) {
	logger, buf := newTestLog(FormatJSON, LogOptions{})
	logger.Info("x", slog.String("a", "1"), "b", 2, slog.Int("c", 3))
	line := jsonLine(t, buf.String())
	if line["a"] != "1" || line["b"] != float64(2) || line["c"] != float64(3) {
		t.Error(test.DiffMessage(line, "a=1 b=2 c=3", "V1: a slog.Attr argument is one pair"))
	}
}

func TestLog_OddArguments(t *testing.T) {
	logger, buf := newTestLog(FormatJSON, LogOptions{})
	logger.Info("x", "a", 1, "dangling")
	line := jsonLine(t, buf.String())
	if line["a"] != float64(1) || line["!BADKEY"] != "dangling" {
		t.Error(test.DiffMessage(line, "a=1 !BADKEY=dangling", "V1: a trailing key is passed on as slog prints it"))
	}
}

func TestLog_NonStringKey(t *testing.T) {
	logger, buf := newTestLog(FormatJSON, LogOptions{MaskFields: []string{"password"}})
	logger.Info("x", 42, "value", "k", "v")
	out := buf.String()
	if !strings.Contains(out, `"!BADKEY":42,"value":"k","!BADKEY":"v"`) {
		t.Error(test.DiffMessage(out, `"!BADKEY":42,"value":"k","!BADKEY":"v"`, "V1, M3: a non-string key takes one argument, as slog pairs it"))
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

func TestNewLog_NewLoggerPerCall(t *testing.T) {
	first := NewLog(nil)
	second := NewLog(nil)

	if first == nil || second == nil {
		t.Fatal(test.DiffMessage([]any{first, second}, "two loggers", "G5: NewLog never returns nil"))
	}
	if first == second {
		t.Error(test.DiffMessage(first, "a different logger", "G5: NewLog returns a new logger on every call"))
	}
}

func TestNewLog_OptionsOfSecondCallApply(t *testing.T) {
	NewLog(&LogOptions{Format: FormatText})

	out := captureStdout(t, func() func() {
		logger := NewLog(&LogOptions{Format: FormatJSON})
		return func() { logger.Info("hello", "k", "v") }
	})

	if !strings.HasPrefix(out, "{") || !strings.Contains(out, `"msg":"hello"`) || !strings.Contains(out, `"k":"v"`) {
		t.Error(test.DiffMessage(out, `a JSON line with "msg":"hello"`, "G5: the options of a later call apply to the logger it returns"))
	}
}

func TestNewLog_TwoLoggersBehaveDifferently(t *testing.T) {
	var textOut, jsonOut string
	textOut = captureStdout(t, func() func() {
		logger := NewLog(&LogOptions{Format: FormatText})
		return func() { logger.Info("hello") }
	})
	jsonOut = captureStdout(t, func() func() {
		logger := NewLog(&LogOptions{Format: FormatJSON})
		return func() { logger.Info("hello") }
	})

	if !strings.Contains(textOut, "msg=hello") || strings.HasPrefix(textOut, "{") {
		t.Error(test.DiffMessage(textOut, "a text line with msg=hello", "G5: the text logger writes text"))
	}
	if !strings.HasPrefix(jsonOut, "{") {
		t.Error(test.DiffMessage(jsonOut, "a JSON line", "G5: the JSON logger writes JSON"))
	}
}

func TestNewLog_MaskFieldsOfEachCallApply(t *testing.T) {
	masked := captureStdout(t, func() func() {
		logger := NewLog(&LogOptions{Format: FormatText, MaskFields: []string{"password"}})
		return func() { logger.Info("login", "password", "secret") }
	})
	plain := captureStdout(t, func() func() {
		logger := NewLog(&LogOptions{Format: FormatText})
		return func() { logger.Info("login", "password", "secret") }
	})

	if strings.Contains(masked, "secret") || !strings.Contains(masked, "[REDACTED]") {
		t.Error(test.DiffMessage(masked, "password masked", "G5: MaskFields of this call apply"))
	}
	if !strings.Contains(plain, "secret") {
		t.Error(test.DiffMessage(plain, "password visible", "G5: another call without MaskFields masks nothing"))
	}
}

func TestPipeline_ValueChangedAfterCallNotSeen(t *testing.T) {
	w := newHoldWriter()
	t.Cleanup(w.openAll)
	l, s := asyncLogger(t, w, LogOptions{Format: FormatJSON})
	m := map[string]any{"k": "before"}
	list := []string{"before"}
	text := "before"
	l.Info("m", "map", m, "list", list, "ptr", &text)
	m["k"] = "after"
	list[0] = "after"
	text = "after"
	w.openAll()
	if !s.Flush(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, "P12: Flush returns true"))
	}
	out := w.String()
	if strings.Contains(out, "after") || strings.Count(out, "before") != 3 {
		t.Error(test.DiffMessage(out, "three before, no after", "P1: values are read during the call only"))
	}
}

func TestPipeline_LogValueResolvedDuringCall(t *testing.T) {
	w := newHoldWriter()
	t.Cleanup(w.openAll)
	l, _ := asyncLogger(t, w, LogOptions{Format: FormatJSON})
	calls := 0
	l.Info("m", "v", pipeCountingValuer{&calls})
	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "P1: a LogValuer is resolved on the caller, before Info returns"))
	}
}

func TestPipeline_TwoLoggersShareOrder(t *testing.T) {
	w := newHoldWriter()
	t.Cleanup(w.openAll)
	s := logpipe.New(w)
	s.StartAsync()
	t.Cleanup(func() {
		w.openAll()
		s.StopAsync(time.Minute)
	})
	a := newLogSink(s, false, &LogOptions{Format: FormatJSON})
	b := newLogSink(s, false, &LogOptions{Format: FormatText})
	for i := range 200 {
		if i%2 == 0 {
			a.Info("n", "n", i)
		} else {
			b.Info("n", "n", i)
		}
	}
	w.openAll()
	if !s.Flush(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, "P12: Flush returns true"))
	}
	lines := strings.Split(strings.TrimSuffix(w.String(), "\n"), "\n")
	if len(lines) != 200 {
		t.Fatal(test.DiffMessage(len(lines), 200, "P2: loggers on one sink share its queue"))
	}
	for i, line := range lines {
		if !strings.HasSuffix(line, `"n":`+strconv.Itoa(i)+"}") && !strings.HasSuffix(line, "n="+strconv.Itoa(i)) {
			t.Fatal(test.DiffMessage(line, "record "+strconv.Itoa(i), "O1: records of one goroutine are written in call order across loggers"))
		}
	}
}

func TestPipeline_ConcurrentWritersWholeRecords(t *testing.T) {
	w := newHoldWriter()
	t.Cleanup(w.openAll)
	l, s := asyncLogger(t, w, LogOptions{Format: FormatJSON})
	const goroutines, per = 16, 250
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range per {
				l.Info("n", "g", g, "i", i, "pad", strings.Repeat("p", (g+i)%20))
			}
		}()
	}
	wg.Wait()
	w.openAll()
	if !s.Flush(time.Minute) {
		t.Fatal(test.DiffMessage(false, true, "P12: Flush returns true"))
	}
	seen := map[[2]int]bool{}
	last := make([]int, goroutines)
	for i := range last {
		last[i] = -1
	}
	for _, line := range strings.Split(strings.TrimSuffix(w.String(), "\n"), "\n") {
		var rec struct{ G, I int }
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatal(test.DiffMessage(line, "one JSON object", "P6: records are written whole"))
		}
		rec.G, rec.I = int(raw["g"].(float64)), int(raw["i"].(float64))
		if seen[[2]int{rec.G, rec.I}] || rec.I <= last[rec.G] {
			t.Fatal(test.DiffMessage(rec, "once and in order per goroutine", "O1: no duplicate, per-goroutine order kept"))
		}
		seen[[2]int{rec.G, rec.I}] = true
		last[rec.G] = rec.I
	}
	if len(seen) != goroutines*per {
		t.Error(test.DiffMessage(len(seen), goroutines*per, "P5: every record written"))
	}
}

func TestPipeline_RecordAtLimitNotTruncated(t *testing.T) {
	const maxRecord = 64 << 10
	for _, total := range []int{maxRecord - 1, maxRecord} {
		out := textLineOfLength(t, total)
		if len(out) != total || strings.Contains(out, "[TRUNCATED]") {
			t.Error(test.DiffMessage(len(out), total, "P3: a record of "+strconv.Itoa(total)+" bytes is not cut"))
		}
	}
}

func TestPipeline_LongRecordTruncated(t *testing.T) {
	const maxRecord = 64 << 10
	out := textLineOfLength(t, maxRecord+1)
	if len(out) > maxRecord || !strings.HasSuffix(out, "...[TRUNCATED]\n") || len(out) < maxRecord-64 {
		t.Error(test.DiffMessage(len(out), "<= 65536 and ends with ...[TRUNCATED]", "P3: maxRecord + 1 is cut to at most maxRecord bytes"))
	}
	if !strings.HasPrefix(out, "time=") || !strings.Contains(out, "level=INFO msg=aaaa") {
		t.Error(test.DiffMessage(out[:60], "the head survives", "P3: the head of the record is kept"))
	}

	buf := &bytes.Buffer{}
	jsonLogger := newLogSink(logpipe.New(buf), false, &LogOptions{Format: FormatJSON})
	jsonLogger.Info("m", "k", strings.Repeat("y", 200000))
	jsonLogger.Info("next")
	lines := strings.SplitAfter(buf.String(), "\n")
	if len(lines) != 3 || len(lines[0]) > maxRecord || !strings.HasSuffix(lines[0], "...[TRUNCATED]\n") || json.Valid([]byte(lines[0])) {
		t.Fatal(test.DiffMessage(len(lines[0]), "<= 65536, ends with ...[TRUNCATED], not valid JSON", "P3: a cut JSON record"))
	}
	if !strings.HasPrefix(lines[0], `{"time":"`) || !strings.Contains(lines[0], `"level":"INFO","msg":"m"`) {
		t.Error(test.DiffMessage(lines[0][:80], "time, level and message", "P3: the head with time, level and message survives"))
	}
	if !strings.Contains(lines[1], `"msg":"next"`) || !json.Valid([]byte(lines[1])) {
		t.Error(test.DiffMessage(lines[1], "the next record, whole", "P3: the record after a cut one is whole"))
	}

	prettyBuf := &bytes.Buffer{}
	pretty := newLogSink(logpipe.New(prettyBuf), false, &LogOptions{Format: FormatPretty})
	pretty.Info("m", "k", strings.Repeat("y", 200000))
	if got := prettyBuf.String(); len(got) > maxRecord || !strings.HasSuffix(got, ansiReset+"...[TRUNCATED]\n") {
		t.Error(test.DiffMessage(len(got), "<= 65536, a color reset then ...[TRUNCATED]", "P3: Pretty resets the color before the marker"))
	}
}

func TestPipeline_TruncateOnRuneBoundary(t *testing.T) {
	const maxRecord = 64 << 10
	for pad := range 8 {
		for _, format := range []Format{FormatJSON, FormatText, FormatPretty} {
			buf := &bytes.Buffer{}
			l := newLogSink(logpipe.New(buf), false, &LogOptions{Format: format})
			l.Info(strings.Repeat("a", pad) + strings.Repeat("😀", 30000))
			out := buf.String()
			if !utf8.ValidString(out) || len(out) > maxRecord || !strings.HasSuffix(out, "...[TRUNCATED]\n") {
				t.Fatal(test.DiffMessage(len(out), "valid UTF-8, <= 65536, truncated", fmt.Sprintf("P3: the cut falls on a UTF-8 boundary, %s pad %d", formatName(format), pad)))
			}
		}
	}
}

func TestPipeline_DropReportRecord(t *testing.T) {
	big := strings.Repeat("x", 60000)
	for _, format := range []Format{FormatJSON, FormatText} {
		w := newHoldWriter()
		t.Cleanup(w.openAll)
		l, s := asyncLogger(t, w, LogOptions{Format: format, Level: LevelDebug})
		const total = 26
		for i := range total {
			if i%2 == 0 {
				l.Debug("big", "pad", big)
			} else {
				l.Info("big", "pad", big)
			}
		}
		w.openAll()
		if !s.Flush(time.Minute) {
			t.Fatal(test.DiffMessage(false, true, "P12: Flush returns true"))
		}
		out := w.String()
		var report *regexp.Regexp
		if format == FormatJSON {
			report = regexp.MustCompile(`"level":"WARN","msg":"LogDropped","debug":(\d+),"info":(\d+)\}`)
		} else {
			report = regexp.MustCompile(`level=WARN msg=LogDropped debug=(\d+) info=(\d+)`)
		}
		m := report.FindAllStringSubmatch(out, -1)
		if len(m) != 1 {
			t.Fatal(test.DiffMessage(len(m), 1, "P10: one WARN LogDropped record with a pair per level, debug then info, "+formatName(format)))
		}
		debug, _ := strconv.Atoi(m[0][1])
		info, _ := strconv.Atoi(m[0][2])
		accepted := strings.Count(out, "big")
		if accepted+debug+info != total || debug == 0 || info == 0 {
			t.Error(test.DiffMessage(accepted+debug+info, total, "P10: written plus reported equal submitted, "+formatName(format)))
		}
	}
}

func TestNewLog_StdoutSinkSynchronousWithoutApp(t *testing.T) {
	buf := &syncBuf{}
	restore := logpipe.SetStdout(buf)
	defer restore()
	runtime.GC()
	base := runtime.NumGoroutine()
	a := NewLog(&LogOptions{Format: FormatJSON})
	b := NewLog(&LogOptions{Format: FormatText})
	for i := range 100 {
		a.Info("a", "i", i)
		if !strings.Contains(buf.String(), `"i":`+strconv.Itoa(i)+"}") {
			t.Fatal(test.DiffMessage(buf.String(), "the line just logged", "M5, P4: a logger used without an app writes before the call returns"))
		}
		b.Info("b", "i", i)
	}
	if !goroutinesAtMost(base) {
		t.Error(test.DiffMessage(runtime.NumGoroutine(), base, "M5: no goroutine is started without an app"))
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 200 || !strings.HasPrefix(lines[0], "{") || !strings.HasPrefix(lines[1], "time=") {
		t.Error(test.DiffMessage(len(lines), 200, "P2: every logger built by NewLog writes to the one stdout sink, in order"))
	}
}

func TestNewLog_AsyncOnceStdoutSinkStarted(t *testing.T) {
	w := newHoldWriter()
	restore := logpipe.SetStdout(w)
	defer restore()
	logpipe.Stdout().StartAsync()
	defer logpipe.Stdout().StopAsync(time.Minute)
	defer w.openAll()
	l := NewLog(&LogOptions{Format: FormatJSON})
	within(t, "P5: a built-in logger does not wait for stdout in asynchronous mode", func() {
		for i := range 20 {
			l.Info("held", "i", i)
		}
	})
	if w.String() != "" {
		t.Error(test.DiffMessage(w.String(), "", "P5: nothing reached the held stdout"))
	}
	w.openAll()
	if !logpipe.Stdout().Flush(time.Minute) || strings.Count(w.String(), `"msg":"held"`) != 20 {
		t.Error(test.DiffMessage(strings.Count(w.String(), `"msg":"held"`), 20, "P2: the lines reach stdout through the process-wide sink"))
	}
}

func TestFatal_FlushesThenExits(t *testing.T) {
	w := &syncBuf{}
	l, _ := asyncLogger(t, w, LogOptions{Format: FormatJSON})
	probe := newExitProbe(w.String)
	probe.install(t)
	l.Info("one")
	l.Info("two")
	l.Fatal("bye", "k", 1)
	if len(probe.codes) != 1 || probe.codes[0] != 1 {
		t.Fatal(test.DiffMessage(probe.codes, []int{1}, "X1: Fatal exits with 1 once"))
	}
	at := probe.seen[0]
	i, j, k := strings.Index(at, `"msg":"one"`), strings.Index(at, `"msg":"two"`), strings.Index(at, `"level":"FATAL","msg":"bye"`)
	if i < 0 || j < i || k < j {
		t.Error(test.DiffMessage(at, "one, two, FATAL bye, all written before exit", "X1: the sink is flushed before exit"))
	}
}

func TestFatal_StuckOutputExitsAfterTimeout_Sync(t *testing.T) {
	fatalOnStuckOutput(t, false)
}

func TestFatal_StuckOutputExitsAfterTimeout_Async(t *testing.T) {
	fatalOnStuckOutput(t, true)
}

func TestLog_ErrorShowsText(t *testing.T) {
	errs := []error{errors.New("boom"), fmt.Errorf("wrapped: %w", errors.New("boom"))}
	for _, format := range allFormats {
		for _, err := range errs {
			out := plain(logOnce(format, LogOptions{}, "failed", "error", err))
			if !strings.Contains(out, err.Error()) {
				t.Error(test.DiffMessage(out, "contains "+err.Error(), "V2: an error is shown by Error() ("+formatName(format)+")"))
			}
		}
	}
}

func TestLog_ErrorShowsTextThroughMaskLayer(t *testing.T) {
	for _, format := range allFormats {
		out := plain(logOnce(format, LogOptions{MaskFields: []string{"password"}}, "failed", "error", errors.New("boom")))
		if !strings.Contains(out, "boom") {
			t.Error(test.DiffMessage(out, "contains boom", "V2: an error with a mask rule is shown by Error() ("+formatName(format)+")"))
		}
	}
}

func TestLog_PointerStringerNotExpanded(t *testing.T) {
	u, err := url.Parse("https://user:pw@host/path")
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range allFormats {
		for _, rules := range [][]string{nil, {"password"}} {
			out := plain(logOnce(format, LogOptions{MaskFields: rules}, "x", "url", u))
			if !strings.Contains(out, u.String()) {
				t.Error(test.DiffMessage(out, "contains "+u.String(), "V2: a *url.URL is written through String() ("+formatName(format)+")"))
			}
			if strings.Contains(out, "Scheme") || strings.Contains(out, "User:") || strings.Contains(out, "{user pw") {
				t.Error(test.DiffMessage(out, "no expanded fields", "V2: a pointer Stringer is never expanded ("+formatName(format)+")"))
			}
		}
	}
}

func TestLog_PointerStringerReachesCustomLoggerUnchanged(t *testing.T) {
	u := &url.URL{Scheme: "https", Host: "h"}
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"password"}).Info("x", "url", u)
	if cap.args()[1] != u {
		t.Error(test.DiffMessage(cap.args()[1], u, "V2: a wrapped custom logger receives the Stringer unchanged"))
	}
}

func TestLog_LogValuerResolved(t *testing.T) {
	for _, format := range allFormats {
		for _, v := range []any{resolvedValuer{}, &derefValuer{v: "resolved"}} {
			out := plain(logOnce(format, LogOptions{}, "x", "v", v))
			if !strings.Contains(out, "resolved") {
				t.Error(test.DiffMessage(out, "contains resolved", fmt.Sprintf("V2: a LogValuer is resolved (%s, %T)", formatName(format), v)))
			}
		}
	}
}

func TestLog_NilPointerIsNull(t *testing.T) {
	var s *derefStringer
	var v *derefValuer
	var u *url.URL
	for _, format := range allFormats {
		for _, rules := range [][]string{nil, {"password"}} {
			out := logOnce(format, LogOptions{MaskFields: rules}, "x", "s", s, "v", v, "u", u)
			if strings.Contains(out, "log_error") {
				t.Error(test.DiffMessage(out, "no log_error", "V2: no method is called on a nil pointer ("+formatName(format)+")"))
			}
			if format == FormatJSON && strings.Count(out, "null") != 3 {
				t.Error(test.DiffMessage(out, "three nulls", "V2: a nil pointer is null"))
			}
			if format == FormatPretty && strings.Count(plain(out), "null") != 3 {
				t.Error(test.DiffMessage(plain(out), "three nulls", "V2: a nil pointer is null"))
			}
		}
	}
}

func TestLog_TypedNilError(t *testing.T) {
	var e *derefError
	var nilErr error
	for _, format := range allFormats {
		out := logOnce(format, LogOptions{}, "x", "e", error(e), "n", nilErr)
		if strings.Contains(out, "log_error") {
			t.Error(test.DiffMessage(out, "no log_error", "V2: a typed nil error is not called ("+formatName(format)+")"))
		}
		if format == FormatJSON && strings.Count(out, "null") != 2 {
			t.Error(test.DiffMessage(out, "two nulls", "V2: nil errors are null"))
		}
	}
}

func TestLog_StructBecomesMap(t *testing.T) {
	out := logOnce(FormatJSON, LogOptions{MaskFields: []string{"zzz"}}, "x", "a", account{Name: "n", Password: "p"}, "pa", &account{Name: "m", Password: "q"})
	if !strings.Contains(out, `"a":{"Name":"n","Password":"p"}`) || !strings.Contains(out, `"pa":{"Name":"m","Password":"q"}`) {
		t.Error(test.DiffMessage(out, "structs as objects of their fields", "V3: a struct becomes a map, pointers are followed"))
	}
}

func TestLog_TagRenames(t *testing.T) {
	out := logOnce(FormatJSON, LogOptions{}, "x", "r", renamed{Name: "n", Plain: "p"})
	if !strings.Contains(out, `"name":"n"`) || strings.Contains(out, `"Name"`) {
		t.Error(test.DiffMessage(out, `"name":"n"`, "V3: log:\"name\" renames the field"))
	}
}

func TestLog_TagDash(t *testing.T) {
	out := logOnce(FormatJSON, LogOptions{}, "x", "r", renamed{Hidden: "nope", secret: "nope", Plain: "p"})
	if strings.Contains(out, "nope") || strings.Contains(out, "Hidden") || strings.Contains(out, "secret") {
		t.Error(test.DiffMessage(out, "no hidden or untagged unexported field", "V3: log:\"-\" and untagged unexported fields are left out"))
	}
	if !strings.Contains(out, `"Plain":"p"`) {
		t.Error(test.DiffMessage(out, `"Plain":"p"`, "V3: an untagged exported field keeps its Go name"))
	}
}

func TestLog_TaggedUnexportedField(t *testing.T) {
	for _, format := range allFormats {
		out := plain(logOnce(format, LogOptions{}, "x", "r", renamed{token: "tkn"}))
		if !strings.Contains(out, "tok") || !strings.Contains(out, "tkn") {
			t.Error(test.DiffMessage(out, "contains tok and tkn", "V3: a tagged unexported field is read ("+formatName(format)+")"))
		}
	}
}

func TestLog_IntegerKeyMap(t *testing.T) {
	out := logOnce(FormatJSON, LogOptions{}, "x", "m", map[int]account{2: {Name: "b"}, 1: {Name: "a"}})
	if !strings.Contains(out, `"m":{"1":{"Name":"a","Password":""},"2":{"Name":"b","Password":""}}`) {
		t.Error(test.DiffMessage(out, "integer keys in decimal", "V3: integer keys become decimal strings"))
	}
}

func TestLog_SliceAndArrayOfStructs(t *testing.T) {
	out := logOnce(FormatJSON, LogOptions{}, "x", "s", []account{{Name: "a"}}, "a", [1]account{{Name: "b"}})
	if !strings.Contains(out, `"s":[{"Name":"a","Password":""}]`) || !strings.Contains(out, `"a":[{"Name":"b","Password":""}]`) {
		t.Error(test.DiffMessage(out, "arrays of objects", "V3: slices and arrays become lists"))
	}
}

func TestLog_UnchangedValueIsSameValue(t *testing.T) {
	p := new(int)
	s := []string{"a", "b"}
	m := map[string]int{"a": 1}
	when := time.Unix(1, 0)
	for _, rules := range [][]string{nil, {"password"}} {
		cap := &capturingLogger{}
		WrapLogger(cap, rules).Info("x", "p", p, "s", s, "m", m, "t", when)
		got := cap.args()
		if got[1] != p {
			t.Error(test.DiffMessage(got[1], p, "V4: a pointer needing no conversion is passed on as it is"))
		}
		if gs, ok := got[3].([]string); !ok || &gs[0] != &s[0] {
			t.Error(test.DiffMessage(got[3], s, "V4: a slice needing no conversion is the same slice"))
		}
		if gm, ok := got[5].(map[string]int); !ok || reflect.ValueOf(gm).Pointer() != reflect.ValueOf(m).Pointer() {
			t.Error(test.DiffMessage(got[5], m, "V4: a map needing no conversion is the same map"))
		}
		if got[7] != when {
			t.Error(test.DiffMessage(got[7], when, "V4: a time needing no conversion is passed on as it is"))
		}
	}
}

func TestLog_ScalarArgsNotCopied(t *testing.T) {
	args := []any{"a", 1, "b", "two", "c", 3.5, "d", true, "e", errors.New("e")}
	for _, rules := range [][]string{nil, {"password"}, {"user.*"}} {
		cap := &capturingLogger{}
		WrapLogger(cap, rules).Info("x", args...)
		got := cap.args()
		if len(got) != len(args) || &got[0] != &args[0] {
			t.Error(test.DiffMessage(&got[0], &args[0], "V4: when nothing needs conversion the argument slice itself is passed on"))
		}
	}
}

func TestLog_ArgsCopiedWhenAStructNeedsConversion(t *testing.T) {
	args := []any{"a", 1, "u", account{Name: "n"}}
	cap := &capturingLogger{}
	WrapLogger(cap, nil).Info("x", args...)
	if _, ok := args[3].(account); !ok {
		t.Error(test.DiffMessage(args[3], "account", "V4: the caller's arguments are never modified"))
	}
	if _, ok := cap.args()[3].(map[string]any); !ok {
		t.Error(test.DiffMessage(cap.args()[3], "map[string]any", "V3: the converted copy goes on"))
	}
}

func TestLog_SelfReferenceEnds(t *testing.T) {
	for _, format := range allFormats {
		for _, rules := range [][]string{nil, {"password"}} {
			var out string
			finishesWithin(t, "V5: a value that contains itself ends ("+formatName(format)+")", func() {
				out = logOnce(format, LogOptions{MaskFields: rules}, "x", "n", selfReferencing())
			})
			if !strings.Contains(out, "[MAX_DEPTH]") && !strings.Contains(out, "[TRUNCATED]") {
				t.Error(test.DiffMessage(plain(out), "a [MAX_DEPTH] marker", "V5: the cycle is cut ("+formatName(format)+")"))
			}
		}
	}
}

func TestLog_SelfReferenceEndsThroughCustomLogger(t *testing.T) {
	cap := &capturingLogger{}
	finishesWithin(t, "V5: a cycle ends through a wrapped custom logger", func() {
		WrapLogger(cap, []string{"password"}).Info("x", "n", selfReferencing())
	})
}

func TestLog_DepthLimit(t *testing.T) {
	for _, format := range allFormats {
		out := plain(logOnce(format, LogOptions{}, "x", "c", chainOf(20)))
		if !strings.Contains(out, "[MAX_DEPTH]") {
			t.Error(test.DiffMessage(out, "contains [MAX_DEPTH]", "V5: nesting below 16 levels is replaced ("+formatName(format)+")"))
		}
		if strings.Contains(out, "leaf-20") || strings.Contains(out, "leaf-19") {
			t.Error(test.DiffMessage(out, "no leaf below the limit", "V5: values below the limit are not followed ("+formatName(format)+")"))
		}
		if !strings.Contains(out, "leaf-0") || !strings.Contains(out, "leaf-10") {
			t.Error(test.DiffMessage(out, "the first levels", "V5: the first levels are shown ("+formatName(format)+")"))
		}
	}
}

func TestLog_ShallowNestingIsNotCut(t *testing.T) {
	out := logOnce(FormatJSON, LogOptions{}, "x", "c", chainOf(5))
	if strings.Contains(out, "[MAX_DEPTH]") || !strings.Contains(out, "leaf-5") {
		t.Error(test.DiffMessage(out, "all 6 levels", "V5: a shallow value is shown whole"))
	}
}

func TestLog_NodeLimit(t *testing.T) {
	items := make([]item, 2000)
	for i := range items {
		items[i] = item{N: i}
	}
	byKey := make(map[string]item, 2000)
	for i := range items {
		byKey["k"+strconv.Itoa(10000+i)] = items[i]
	}
	for _, format := range allFormats {
		for _, v := range []any{items, byKey} {
			out := plain(logOnce(format, LogOptions{}, "x", "v", v))
			n := strings.Count(out, "[TRUNCATED]")
			if n == 0 || n >= 2000 {
				t.Error(test.DiffMessage(n, "between 1 and 1999 markers", fmt.Sprintf("V5: after 1,000 values the rest is replaced (%s, %T)", formatName(format), v)))
			}
		}
	}
}

func TestLog_NodeLimitKeepsTheFirstValues(t *testing.T) {
	items := make([]item, 2000)
	for i := range items {
		items[i] = item{N: i}
	}
	out := logOnce(FormatJSON, LogOptions{}, "x", "v", items)
	if !strings.Contains(out, `{"N":0}`) || strings.Contains(out, `{"N":1999}`) {
		t.Error(test.DiffMessage(out, "first elements shown, last replaced", "V5: the first 1,000 values are kept"))
	}
}

func TestLog_PanickingMethodRecovered(t *testing.T) {
	for name, probe := range panicProbes() {
		for _, format := range allFormats {
			for _, rules := range [][]string{nil, {"password"}} {
				logger, buf := newTestLog(format, LogOptions{MaskFields: rules})
				func() {
					defer func() {
						if rec := recover(); rec != nil {
							t.Error(test.DiffMessage(rec, "no panic", "V6: a panicking "+name+" does not panic the call ("+formatName(format)+")"))
						}
					}()
					logger.Warn("original message", "bad", probe, "k", "v")
				}()
				out := plain(buf.String())
				if !strings.Contains(out, "original message") || !strings.Contains(out, "log_error") || !strings.Contains(out, "kaboom") || !strings.Contains(out, "WARN") {
					t.Error(test.DiffMessage(out, "WARN, the message, log_error kaboom", "V6: one line with the level, the message and log_error ("+name+", "+formatName(format)+")"))
				}
				if format != FormatPretty && strings.Count(out, "\n") != 1 {
					t.Error(test.DiffMessage(out, "one line", "V6: exactly one line is written ("+name+", "+formatName(format)+")"))
				}
			}
		}
	}
}

func TestLog_PanickingMethodReachesCustomLoggerUnchanged(t *testing.T) {
	for name, probe := range panicProbes() {
		cap := &capturingLogger{}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Error(test.DiffMessage(rec, "no panic", "V6: the wrapper does not call "+name))
				}
			}()
			WrapLogger(cap, []string{"password"}).Info("x", "bad", probe)
		}()
		if cap.args() == nil || cap.args()[1] != probe {
			t.Error(test.DiffMessage(cap.args(), probe, "V2: a wrapped custom logger receives the "+name+" unchanged"))
		}
	}
}

func TestLog_ErrorLevelRecordedAfterRecovery(t *testing.T) {
	logger, buf := newTestLog(FormatJSON, LogOptions{})
	logger.Error("failed", "bad", panicStringer{})
	line := jsonLine(t, buf.String())
	if line["level"] != "ERROR" || line["msg"] != "failed" || line["log_error"] == nil {
		t.Error(test.DiffMessage(line, "ERROR failed log_error", "V6: the original level and message are kept"))
	}
}

func TestLog_BuiltInLoggerAfterRecoveryStillWorks(t *testing.T) {
	logger, buf := newTestLog(FormatJSON, LogOptions{})
	logger.Info("a", "bad", panicStringer{})
	buf.Reset()
	logger.Info("b", "k", "v")
	if jsonLine(t, buf.String())["msg"] != "b" {
		t.Error(test.DiffMessage(buf.String(), "a normal line", "V6: the logger keeps working after a recovered panic"))
	}
}
