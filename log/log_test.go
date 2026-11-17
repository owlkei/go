package log

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/test"
)

var allFormats = []logFormat{JSONFormat, TextFormat, PrettyFormat}

func formatName(f logFormat) string {
	switch f {
	case JSONFormat:
		return "JSON"
	case TextFormat:
		return "Text"
	default:
		return "Pretty"
	}
}

func newTestLog(format logFormat, opts LogOptions) (Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	opts.LogFormat = format
	if opts.Level == InfoLevel {
		opts.Level = DebugLevel
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
	opts := &LogOptions{Level: -1, LogFormat: JSONFormat, AddSource: true, MaskFields: []string{"password", "token"}}
	want := &LogOptions{Level: -1, LogFormat: JSONFormat, AddSource: true, MaskFields: []string{"password", "token"}}
	NewLog(opts)
	newLog(io.Discard, false, opts)
	WrapLogger(&instanceRecorder{}, opts.MaskFields)
	if !reflect.DeepEqual(opts, want) {
		t.Error(test.DiffMessage(opts, want, "L2: NewLog and WrapLogger never modify their arguments"))
	}
}

func TestNewLog_LaterOptionChangesIgnored(t *testing.T) {
	buf := &bytes.Buffer{}
	opts := &LogOptions{LogFormat: JSONFormat, MaskFields: []string{"password"}}
	logger := newLog(buf, false, opts)
	opts.Level = ErrorLevel
	opts.MaskFields[0] = "other"
	opts.MaskFields = append(opts.MaskFields, "more")
	logger.Info("x", "password", "secret", "other", "kept")
	out := buf.String()
	if !strings.Contains(out, `"password":"[REDACTED]"`) || !strings.Contains(out, `"other":"kept"`) {
		t.Error(test.DiffMessage(out, "password masked, other kept, info shown", "L2: later changes to the options have no effect"))
	}
}

func TestWrapLogger_LaterMaskFieldChangesIgnored(t *testing.T) {
	cap := &capturingLogger{}
	fields := []string{"password"}
	l := WrapLogger(cap, fields)
	fields[0] = "other"
	l.Info("x", "password", "secret")
	if cap.args()[1] != maskPlaceholder {
		t.Error(test.DiffMessage(cap.args()[1], maskPlaceholder, "L2: WrapLogger copies maskFields"))
	}
}

func TestLog_JSONLine(t *testing.T) {
	logger, buf := newTestLog(JSONFormat, LogOptions{})
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
	logger, buf := newTestLog(TextFormat, LogOptions{})
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

	jsonLogger, jsonBuf := newTestLog(JSONFormat, LogOptions{})
	textLogger, textBuf := newTestLog(TextFormat, LogOptions{})
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
	newLog(buf, true, &LogOptions{LogFormat: JSONFormat}).Info("hello")
	jsonLine(t, buf.String())
	buf.Reset()
	newLog(buf, false, &LogOptions{LogFormat: PrettyFormat}).Info("hello")
	if !strings.HasPrefix(buf.String(), "\n") {
		t.Error(test.DiffMessage(buf.String(), "Pretty", "L4: an explicit format wins whatever the output"))
	}
}

func TestLog_BelowLevelWritesNothing(t *testing.T) {
	for _, format := range allFormats {
		buf := &bytes.Buffer{}
		logger := newLog(buf, false, &LogOptions{LogFormat: format, Level: WarnLevel})
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
			logger, buf := newTestLog(format, LogOptions{Level: ErrorLevel, MaskFields: rules})
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
			logger, _ := newTestLog(format, LogOptions{Level: InfoLevel + 1, MaskFields: rules})
			if n := testing.AllocsPerRun(50, func() { logger.Debug("x", args...) }); n != 0 {
				t.Error(test.DiffMessage(n, 0.0, "L3: a dropped line allocates nothing ("+formatName(format)+")"))
			}
		}
	}
}

func TestLog_AddSourceIsCaller(t *testing.T) {
	for _, format := range allFormats {
		logger, buf := newTestLog(format, LogOptions{AddSource: true})
		_, file, line, _ := runtime.Caller(0)
		logger.Info("here")
		where := file + ":" + strconv.Itoa(line+1)
		out := buf.String()
		switch format {
		case JSONFormat:
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
		logger, buf := newTestLog(format, LogOptions{AddSource: true})
		wrapped := WrapLogger(logger, []string{"password"})
		_, file, line, _ := runtime.Caller(0)
		wrapped.Info("here", "password", "s")
		where := file + ":" + strconv.Itoa(line+1)
		out := buf.String()
		if format == JSONFormat {
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
	logger, buf := newTestLog(PrettyFormat, LogOptions{AddSource: true})
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
		logger := newLog(w, false, &LogOptions{LogFormat: format, Level: DebugLevel})
		logger.Info("m", "a", 1, "b", map[string]int{"x": 1, "y": 2}, "c", benchUser{Name: "n", Password: "p"})
		logger.Error("m")
		if w.writes != 2 {
			t.Error(test.DiffMessage(w.writes, 2, "L6: one Write per record ("+formatName(format)+")"))
		}
	}
}

func TestLog_ConcurrentRecordsNeverInterleave(t *testing.T) {
	for _, format := range []logFormat{JSONFormat, TextFormat} {
		buf := &bytes.Buffer{}
		logger := newLog(buf, false, &LogOptions{LogFormat: format, MaskFields: []string{"password"}})
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
		if format == JSONFormat {
			for _, line := range lines {
				jsonLine(t, line)
			}
		}
	}
}

func TestLog_ConcurrentCalls_NoRace(t *testing.T) {
	for _, format := range allFormats {
		buf := &bytes.Buffer{}
		logger := newLog(buf, false, &LogOptions{LogFormat: format, MaskFields: []string{"password"}})
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
		logger := newLog(buf, false, &LogOptions{LogFormat: format})
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
	newLog(&bytes.Buffer{}, false, &LogOptions{LogFormat: JSONFormat, Level: FatalLevel}).Fatal("x")
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "L7: Fatal always exits"))
	}
}

func TestLog_ColoredIsPlainTextInJSON(t *testing.T) {
	logger, buf := newTestLog(JSONFormat, LogOptions{})
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
	logger, buf := newTestLog(TextFormat, LogOptions{})
	logger.Info("x", "d", color.Colored{Text: "120µs (4%)", Color: color.Red})
	out := buf.String()
	if strings.Contains(out, "\x1b") || strings.Contains(out, `\x1b`) || !strings.Contains(out, "120µs (4%)") {
		t.Error(test.DiffMessage(out, "the text without ANSI code", "A4: Text writes only the Text of a Colored"))
	}
}

func TestLog_JSONEscapesControlChars(t *testing.T) {
	logger, buf := newTestLog(JSONFormat, LogOptions{})
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
	logger, buf := newTestLog(TextFormat, LogOptions{})
	logger.Info("/a\n\x1b[2K FAKE\r\nERROR forged", "k", "v\n\x1b[2J")
	out := buf.String()
	if strings.Count(out, "\n") != 1 || strings.ContainsAny(out, "\x1b\r") {
		t.Error(test.DiffMessage(out, "one line, no raw control", "F4: Text escapes control characters"))
	}
}

func TestLog_AttrArgument(t *testing.T) {
	logger, buf := newTestLog(JSONFormat, LogOptions{})
	logger.Info("x", slog.String("a", "1"), "b", 2, slog.Int("c", 3))
	line := jsonLine(t, buf.String())
	if line["a"] != "1" || line["b"] != float64(2) || line["c"] != float64(3) {
		t.Error(test.DiffMessage(line, "a=1 b=2 c=3", "V1: a slog.Attr argument is one pair"))
	}
}

func TestLog_OddArguments(t *testing.T) {
	logger, buf := newTestLog(JSONFormat, LogOptions{})
	logger.Info("x", "a", 1, "dangling")
	line := jsonLine(t, buf.String())
	if line["a"] != float64(1) || line["!BADKEY"] != "dangling" {
		t.Error(test.DiffMessage(line, "a=1 !BADKEY=dangling", "V1: a trailing key is passed on as slog prints it"))
	}
}

func TestLog_NonStringKey(t *testing.T) {
	logger, buf := newTestLog(JSONFormat, LogOptions{MaskFields: []string{"password"}})
	logger.Info("x", 42, "value", "k", "v")
	out := buf.String()
	if !strings.Contains(out, `"!BADKEY":42,"value":"k","!BADKEY":"v"`) {
		t.Error(test.DiffMessage(out, `"!BADKEY":42,"value":"k","!BADKEY":"v"`, "V1, M3: a non-string key takes one argument, as slog pairs it"))
	}
}

type atomicCounter struct {
	mu sync.Mutex
	n  int
}

func (c *atomicCounter) add()      { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *atomicCounter) load() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }
