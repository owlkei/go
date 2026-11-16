package log

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

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

func logOnce(format logFormat, opts LogOptions, msg string, args ...any) string {
	logger, buf := newTestLog(format, opts)
	logger.Info(msg, args...)
	return buf.String()
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
			if format == JSONFormat && strings.Count(out, "null") != 3 {
				t.Error(test.DiffMessage(out, "three nulls", "V2: a nil pointer is null"))
			}
			if format == PrettyFormat && strings.Count(plain(out), "null") != 3 {
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
		if format == JSONFormat && strings.Count(out, "null") != 2 {
			t.Error(test.DiffMessage(out, "two nulls", "V2: nil errors are null"))
		}
	}
}

func TestLog_StructBecomesMap(t *testing.T) {
	out := logOnce(JSONFormat, LogOptions{MaskFields: []string{"zzz"}}, "x", "a", account{Name: "n", Password: "p"}, "pa", &account{Name: "m", Password: "q"})
	if !strings.Contains(out, `"a":{"Name":"n","Password":"p"}`) || !strings.Contains(out, `"pa":{"Name":"m","Password":"q"}`) {
		t.Error(test.DiffMessage(out, "structs as objects of their fields", "V3: a struct becomes a map, pointers are followed"))
	}
}

func TestLog_TagRenames(t *testing.T) {
	out := logOnce(JSONFormat, LogOptions{}, "x", "r", renamed{Name: "n", Plain: "p"})
	if !strings.Contains(out, `"name":"n"`) || strings.Contains(out, `"Name"`) {
		t.Error(test.DiffMessage(out, `"name":"n"`, "V3: log:\"name\" renames the field"))
	}
}

func TestLog_TagDash(t *testing.T) {
	out := logOnce(JSONFormat, LogOptions{}, "x", "r", renamed{Hidden: "nope", secret: "nope", Plain: "p"})
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
	out := logOnce(JSONFormat, LogOptions{}, "x", "m", map[int]account{2: {Name: "b"}, 1: {Name: "a"}})
	if !strings.Contains(out, `"m":{"1":{"Name":"a","Password":""},"2":{"Name":"b","Password":""}}`) {
		t.Error(test.DiffMessage(out, "integer keys in decimal", "V3: integer keys become decimal strings"))
	}
}

func TestLog_SliceAndArrayOfStructs(t *testing.T) {
	out := logOnce(JSONFormat, LogOptions{}, "x", "s", []account{{Name: "a"}}, "a", [1]account{{Name: "b"}})
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

func chainOf(links int) *chain {
	head := &chain{Leaf: "leaf-0"}
	cur := head
	for i := 1; i <= links; i++ {
		cur.Next = &chain{Leaf: "leaf-" + strconv.Itoa(i)}
		cur = cur.Next
	}
	return head
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
	out := logOnce(JSONFormat, LogOptions{}, "x", "c", chainOf(5))
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
	out := logOnce(JSONFormat, LogOptions{}, "x", "v", items)
	if !strings.Contains(out, `{"N":0}`) || strings.Contains(out, `{"N":1999}`) {
		t.Error(test.DiffMessage(out, "first elements shown, last replaced", "V5: the first 1,000 values are kept"))
	}
}

func panicProbes() map[string]any {
	return map[string]any{"Stringer": panicStringer{}, "Error": &panicError{}, "LogValuer": panicValuer{}}
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
				if format != PrettyFormat && strings.Count(out, "\n") != 1 {
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
	logger, buf := newTestLog(JSONFormat, LogOptions{})
	logger.Error("failed", "bad", panicStringer{})
	line := jsonLine(t, buf.String())
	if line["level"] != "ERROR" || line["msg"] != "failed" || line["log_error"] == nil {
		t.Error(test.DiffMessage(line, "ERROR failed log_error", "V6: the original level and message are kept"))
	}
}

func TestLog_BuiltInLoggerAfterRecoveryStillWorks(t *testing.T) {
	logger, buf := newTestLog(JSONFormat, LogOptions{})
	logger.Info("a", "bad", panicStringer{})
	buf.Reset()
	logger.Info("b", "k", "v")
	if jsonLine(t, buf.String())["msg"] != "b" {
		t.Error(test.DiffMessage(buf.String(), "a normal line", "V6: the logger keeps working after a recovered panic"))
	}
}
