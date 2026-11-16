package log

import (
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/test"
)

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
func (r *instanceRecorder) Info(msg string, _ ...any)  { r.add(msg) }
func (r *instanceRecorder) Warn(msg string, _ ...any)  { r.add(msg) }
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
	NewLog(&LogOptions{LogFormat: TextFormat})

	out := captureStdout(t, func() func() {
		logger := NewLog(&LogOptions{LogFormat: JSONFormat})
		return func() { logger.Info("hello", "k", "v") }
	})

	if !strings.HasPrefix(out, "{") || !strings.Contains(out, `"msg":"hello"`) || !strings.Contains(out, `"k":"v"`) {
		t.Error(test.DiffMessage(out, `a JSON line with "msg":"hello"`, "G5: the options of a later call apply to the logger it returns"))
	}
}

func TestNewLog_TwoLoggersBehaveDifferently(t *testing.T) {
	var textOut, jsonOut string
	textOut = captureStdout(t, func() func() {
		logger := NewLog(&LogOptions{LogFormat: TextFormat})
		return func() { logger.Info("hello") }
	})
	jsonOut = captureStdout(t, func() func() {
		logger := NewLog(&LogOptions{LogFormat: JSONFormat})
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
		logger := NewLog(&LogOptions{LogFormat: TextFormat, MaskFields: []string{"password"}})
		return func() { logger.Info("login", "password", "secret") }
	})
	plain := captureStdout(t, func() func() {
		logger := NewLog(&LogOptions{LogFormat: TextFormat})
		return func() { logger.Info("login", "password", "secret") }
	})

	if strings.Contains(masked, "secret") || !strings.Contains(masked, "[REDACTED]") {
		t.Error(test.DiffMessage(masked, "password masked", "G5: MaskFields of this call apply"))
	}
	if !strings.Contains(plain, "secret") {
		t.Error(test.DiffMessage(plain, "password visible", "G5: another call without MaskFields masks nothing"))
	}
}

func TestWrapLogger_NilFieldsOnWrappedReturnsSame(t *testing.T) {
	wrapped := WrapLogger(&instanceRecorder{}, nil)

	if again := WrapLogger(wrapped, nil); again != wrapped {
		t.Error(test.DiffMessage(again, wrapped, "G3: WrapLogger(l, nil) returns a wrapped logger unchanged"))
	}

	built := NewLog(nil)
	if again := WrapLogger(built, nil); again != built {
		t.Error(test.DiffMessage(again, built, "G3: WrapLogger(l, nil) returns a logger from NewLog unchanged"))
	}
}

func TestWrapLogger_PlainLoggerStillWrapped(t *testing.T) {
	rec := &instanceRecorder{}
	wrapped := WrapLogger(rec, nil)

	if any(wrapped) == any(rec) {
		t.Error(test.DiffMessage(wrapped, "a wrapper", "G3: a custom logger is wrapped"))
	}
	wrapped.Info("through")
	if len(rec.calls) != 1 || rec.calls[0] != "through" {
		t.Error(test.DiffMessage(rec.calls, []string{"through"}, "G3: the wrapper forwards to the custom logger"))
	}
}
