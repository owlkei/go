package log

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/test"
)

const (
	testRed   = "\x1b[31m"
	testReset = "\x1b[0m"
)

func prettyOnce(msg string, args ...any) string {
	logger, buf := newTestLog(PrettyFormat, LogOptions{})
	logger.Info(msg, args...)
	return buf.String()
}

func TestPretty_ContainsLevelLabels(t *testing.T) {
	saved := exit
	exit = func(int) {}
	defer func() { exit = saved }()
	for _, label := range []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"} {
		logger, buf := newTestLog(PrettyFormat, LogOptions{})
		switch label {
		case "DEBUG":
			logger.Debug("msg")
		case "INFO":
			logger.Info("msg")
		case "WARN":
			logger.Warn("msg")
		case "ERROR":
			logger.Error("msg")
		default:
			logger.Fatal("msg")
		}
		if !strings.Contains(buf.String(), label) {
			t.Error(test.DiffMessage(buf.String(), "contains "+label, "F1: level label"))
		}
	}
}

func TestPretty_HeaderHasTimeAndBracketedMessage(t *testing.T) {
	out := plain(prettyOnce("hello world"))
	if !regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`).MatchString(out) {
		t.Error(test.DiffMessage(out, "a 2006-01-02 15:04:05 time", "F1: the time layout"))
	}
	if !strings.Contains(out, "[hello world]") {
		t.Error(test.DiffMessage(out, "contains [hello world]", "F1: the message in brackets"))
	}
}

func TestPretty_EmptyMessageSkipped(t *testing.T) {
	out := prettyOnce("")
	if strings.Contains(out, "[]") {
		t.Error(test.DiffMessage(out, "no []", "F1: empty message must not produce [] block"))
	}
}

func TestPretty_SingleAttrUsesLastBranch(t *testing.T) {
	out := prettyOnce("msg", "k", "v")
	if !strings.Contains(out, "└──") || strings.Contains(out, "├──") {
		t.Error(test.DiffMessage(out, "only └──", "F1: single attr should use last-branch symbol"))
	}
}

func TestPretty_MultipleAttrsMiddleAndLastBranch(t *testing.T) {
	out := prettyOnce("msg", "k1", "v1", "k2", "v2")
	if !strings.Contains(out, "├──") || !strings.Contains(out, "└──") {
		t.Error(test.DiffMessage(out, "├── and └──", "F1: first attr uses the middle branch, last the end branch"))
	}
}

func TestPretty_StringValueNotQuoted(t *testing.T) {
	out := prettyOnce("msg", "mykey", "hello")
	if !strings.Contains(out, "mykey") || !strings.Contains(out, "hello") || strings.Contains(out, `"hello"`) {
		t.Error(test.DiffMessage(out, "key and unquoted value", "F1: string values are not quoted"))
	}
}

func TestPretty_ScalarValuesPresent(t *testing.T) {
	out := plain(prettyOnce("msg", "count", 42, "ok", true, "ratio", 1.5, "d", 2*time.Second))
	for _, want := range []string{"42", "true", "1.5", "2s"} {
		if !strings.Contains(out, want) {
			t.Error(test.DiffMessage(out, "contains "+want, "F1: scalar value"))
		}
	}
}

func TestPretty_NilAttrShowsNull(t *testing.T) {
	out := prettyOnce("msg", "ptr", nil)
	if !strings.Contains(out, "null") {
		t.Error(test.DiffMessage(out, "contains null", "F1: nil value displays as null"))
	}
}

func TestPretty_StartsAndEndsWithNewline(t *testing.T) {
	out := prettyOnce("msg")
	if !strings.HasPrefix(out, "\n") || !strings.HasSuffix(out, "\n") {
		t.Error(test.DiffMessage(out, "starts and ends with \\n", "F1: an empty line starts every record"))
	}
}

func TestPretty_SliceElementsRenderedWithoutIndexKey(t *testing.T) {
	out := prettyOnce("msg", "tags", []string{"admin", "beta"})
	for _, want := range []string{"admin", "beta"} {
		if !strings.Contains(out, want) {
			t.Error(test.DiffMessage(out, "contains "+want, "F1: slice elements are rendered"))
		}
	}
	for _, notWant := range []string{testRed + "0" + testReset, testRed + "1" + testReset} {
		if strings.Contains(out, notWant) {
			t.Error(test.DiffMessage(out, "no index keys", "F1: slice elements have no index key"))
		}
	}
}

func TestPretty_SliceOfStructsFieldsRenderedDirectlyUnderParent(t *testing.T) {
	type entry struct {
		Name     string
		Duration string
	}
	out := prettyOnce("msg", "stage", []entry{{Name: "M1", Duration: "10ms"}, {Name: "M2", Duration: "20ms"}})
	for _, want := range []string{"M1", "10ms", "M2", "20ms"} {
		if !strings.Contains(out, want) {
			t.Error(test.DiffMessage(out, "contains "+want, "F1: slice-of-structs elements are rendered"))
		}
	}
	if n := strings.Count(out, testRed+"Name"+testReset); n != 2 {
		t.Error(test.DiffMessage(n, 2, "F1: each element's own fields are spliced in directly"))
	}
}

func TestPretty_StructAttrFieldNameKeyedChildren(t *testing.T) {
	type addr struct{ City string }
	out := prettyOnce("msg", "addr", addr{City: "Hanoi"})
	if !strings.Contains(out, "City") || !strings.Contains(out, "Hanoi") {
		t.Error(test.DiffMessage(out, "contains City Hanoi", "F1: a struct renders its fields"))
	}
}

func TestPretty_MapAttrKeysSorted(t *testing.T) {
	out := prettyOnce("msg", "meta", map[string]int{"score": 42, "age": 30})
	a, s := strings.Index(out, "age"), strings.Index(out, "score")
	if a == -1 || s == -1 || a > s {
		t.Error(test.DiffMessage(out, "age before score", "F1: map keys are sorted"))
	}
}

func TestPretty_NestedContainerUsesContinuationBar(t *testing.T) {
	type wrapper struct{ Tags []string }
	out := prettyOnce("msg", "w", wrapper{Tags: []string{"a", "b"}}, "done", "yes")
	if !strings.Contains(out, "│") {
		t.Error(test.DiffMessage(out, "contains │", "F1: a non-last container indents under a continuation bar"))
	}
}

func TestPretty_StringerStructPrintedInline(t *testing.T) {
	out := prettyOnce("msg", "at", time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC))
	if strings.Contains(out, "wall") || strings.Contains(out, "ext") || !strings.Contains(out, "2024") {
		t.Error(test.DiffMessage(out, "String() inline, no internal fields", "F1: a Stringer struct is printed through String()"))
	}
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

func TestPretty_NestedValuesAlignAcrossDepths(t *testing.T) {
	type inner struct{ VeryLongFieldName string }
	out := prettyOnce("msg", "a", inner{VeryLongFieldName: "deep"}, "bb", "shallow")
	var deepCol, shallowCol int
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "VeryLongFieldName"):
			deepCol = valueColumn(t, line, "deep")
		case strings.Contains(line, testRed+"bb"):
			shallowCol = valueColumn(t, line, "shallow")
		}
	}
	if deepCol == 0 || shallowCol == 0 {
		t.Fatalf("did not find both lines: %d %d\n%s", deepCol, shallowCol, out)
	}
	if deepCol != shallowCol {
		t.Error(test.DiffMessage(shallowCol, deepCol, "F1: values align across depths"))
	}
}

func TestPretty_ColoredUsesItsColor(t *testing.T) {
	out := prettyOnce("msg", "dur", color.Colored{Text: "10ms (2%)", Color: color.Red})
	if !strings.Contains(out, string(color.Red)+"10ms (2%)"+testReset) {
		t.Error(test.DiffMessage(out, "red-colored \"10ms (2%)\"", "F4: a Colored value is printed in its own Color"))
	}
}

func TestPretty_ColoredInsideAStageList(t *testing.T) {
	out := prettyOnce("msg", "middleware", []map[string]color.Colored{{"CORS": {Text: "120µs (4%)", Color: color.Gray}}})
	if !strings.Contains(out, string(color.Gray)+"120µs (4%)"+testReset) || !strings.Contains(plain(out), "CORS") {
		t.Error(test.DiffMessage(out, "gray text under CORS", "F4: Colored values nested in the access log shape"))
	}
}

func TestPretty_ErrorValue(t *testing.T) {
	out := plain(prettyOnce("msg", "error", &errString{"boom"}))
	if !strings.Contains(out, "boom") {
		t.Error(test.DiffMessage(out, "contains boom", "V2: an error is shown in Pretty"))
	}
}

type errString struct{ text string }

func (e *errString) Error() string { return e.text }

func TestPretty_MessageControlCharsEscaped(t *testing.T) {
	out := prettyOnce("GET /a\n\x1b[2K FAKE [admin login ok]\r\tend\x7f")
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatal(test.DiffMessage(len(lines), 3, "F2: a message with a line feed stays on one output line"))
	}
	if !strings.Contains(out, `GET /a\n\x1b[2K FAKE [admin login ok]\r\tend\x7f`) {
		t.Error(test.DiffMessage(plain(out), `GET /a\n\x1b[2K FAKE …\r\tend\x7f`, "F2: control characters are written escaped"))
	}
	if stripped := plain(out); strings.ContainsAny(stripped, "\x1b\r\t\x7f") {
		t.Error(test.DiffMessage(stripped, "no raw control", "F2: no raw control character in the message"))
	}
}

func TestPretty_KeyControlCharsEscaped(t *testing.T) {
	out := prettyOnce("msg", "a\nb\x1b[2K", "v")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatal(test.DiffMessage(len(lines), 3, "F2: a key with a line feed stays on one row"))
	}
	if !strings.Contains(out, `a\nb\x1b[2K`) || strings.ContainsAny(plain(out), "\x1b") {
		t.Error(test.DiffMessage(plain(out), `a\nb\x1b[2K`, "F2: control characters of a key are written escaped"))
	}
}

func TestPretty_NestedKeyControlCharsEscaped(t *testing.T) {
	out := prettyOnce("msg", "m", map[string]string{"k\nERROR forged": "v"})
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n")[1:] {
		if strings.HasPrefix(plain(line), "ERROR") {
			t.Error(test.DiffMessage(line, "no forged line", "F2: map keys cannot start a line"))
		}
	}
	if strings.Count(out, "\n") != 4 {
		t.Error(test.DiffMessage(strings.Count(out, "\n"), 4, "F2: a map key with a line feed adds no line"))
	}
}

func TestPretty_ValueEscapeCharEscaped(t *testing.T) {
	out := prettyOnce("msg", "k", "x\x1b[2Jy\r\x00z")
	if !strings.Contains(out, `x\x1b[2Jy\r\x00z`) {
		t.Error(test.DiffMessage(plain(out), `x\x1b[2Jy\r\x00z`, "F3: ESC, CR and NUL in a value are written escaped"))
	}
	if strings.ContainsAny(plain(out), "\x1b\r\x00") {
		t.Error(test.DiffMessage(plain(out), "no raw control", "F3: no raw control character in a value"))
	}
}

func TestPretty_ValueNewlineIsIndented(t *testing.T) {
	out := prettyOnce("msg", "stack", "goroutine 1\nERROR forged\nline3", "after", "x")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var seenForged bool
	for _, line := range lines[2:] {
		text := plain(line)
		if text == "" {
			continue
		}
		if !strings.HasPrefix(text, "  ") {
			t.Error(test.DiffMessage(text, "a line indented by at least two spaces", "F3: no output line of a value starts at column 0"))
		}
		if strings.Contains(text, "ERROR forged") {
			seenForged = true
		}
	}
	if !seenForged {
		t.Error(test.DiffMessage(plain(out), "the stack text kept", "F3: a line feed in a value is kept, on a new indented line"))
	}
}

func TestPretty_ValueNewlineThenCarriageReturn(t *testing.T) {
	out := prettyOnce("msg", "k", "x\r\nERROR forged")
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n")[2:] {
		if text := plain(line); text != "" && !strings.HasPrefix(text, "  ") {
			t.Error(test.DiffMessage(text, "indented", "F3: CR LF cannot start a record-like line"))
		}
	}
	if strings.Contains(plain(out), "\r") {
		t.Error(test.DiffMessage(plain(out), "no raw CR", "F3: carriage return is escaped"))
	}
}

func TestPretty_ErrorTextEscaped(t *testing.T) {
	out := prettyOnce("msg", "error", &errString{"boom\x1b[2K\nERROR forged"})
	if strings.ContainsAny(plain(out), "\x1b") {
		t.Error(test.DiffMessage(plain(out), "no raw ESC", "F3: the text of an error is escaped"))
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n")[2:] {
		if text := plain(line); text != "" && !strings.HasPrefix(text, "  ") {
			t.Error(test.DiffMessage(text, "indented", "F3: the text of an error cannot start a line"))
		}
	}
}

func TestPretty_C1ControlEscaped(t *testing.T) {
	out := prettyOnce("a\u0085b\u009bc", "k\u0085", "v\u0085w\u009b")
	stripped := plain(out)
	if strings.ContainsAny(stripped, "\u0080\u0085\u0090\u009b\u009f") {
		t.Error(test.DiffMessage(stripped, "no raw C1 control", "F2/F3: U+0080 to U+009F are written escaped"))
	}
	if strings.Count(out, "\n") != 3 {
		t.Error(test.DiffMessage(strings.Count(out, "\n"), 3, "F2: a C1 control adds no line"))
	}
}

func TestPretty_NonControlUnicodeKept(t *testing.T) {
	out := prettyOnce("xin chào — 日本語", "k", "ñandú ✓")
	if !strings.Contains(out, "xin chào — 日本語") || !strings.Contains(out, "ñandú ✓") {
		t.Error(test.DiffMessage(plain(out), "unicode kept", "F2: only control characters are escaped"))
	}
}

func TestPretty_ForgedAccessLogPathIsOneRecord(t *testing.T) {
	logger, buf := newTestLog(PrettyFormat, LogOptions{})
	logger.Info("GET /a%0A%1B[2K FAKE [admin login ok]"+"\n\x1b[2K FAKE [admin login ok]", "id", "x")
	if n := strings.Count(buf.String(), "\n"); n != 3 {
		t.Error(test.DiffMessage(n, 3, "F2: one record is an empty line, the header, one row and the end"))
	}
}

func TestPretty_ConcurrentCalls_NoDataRace(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := newLog(buf, false, &LogOptions{LogFormat: PrettyFormat, Level: DebugLevel})
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func() {
			defer wg.Done()
			logger.Info("msg", "i", i, "meta", map[string]int{"a": i, "b": i * 2})
		}()
	}
	wg.Wait()
	if strings.Count(buf.String(), "[msg]") != goroutines {
		t.Error(test.DiffMessage(strings.Count(buf.String(), "[msg]"), goroutines, "L6: every record is written whole"))
	}
}

func TestPretty_LineSeparatorsAndBidiEscaped(t *testing.T) {
	spoof := "a\u2028b\u2029c\u202ad\u202ee\u2066f\u2069g"
	out := prettyOnce(spoof, "k\u202e", spoof)
	stripped := plain(out)
	if strings.ContainsAny(stripped, "\u2028\u2029\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069") {
		t.Error(test.DiffMessage(stripped, "no raw separator or bidi control", "L5: U+2028, U+2029 and bidi controls are written escaped"))
	}
	if !strings.Contains(stripped, "\\u2028") || !strings.Contains(stripped, "\\u202e") {
		t.Error(test.DiffMessage(stripped, "escaped separators", "L5: escaped as a backslash-u sequence"))
	}
	if !strings.Contains(prettyOnce("a\u202fb", "k", "v"), "a\u202fb") {
		t.Error(test.DiffMessage("escaped", "kept", "L5: U+202F is not a control and is kept"))
	}
}
