package config

import (
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestParseDotEnv_Comments(t *testing.T) {
	svc := loadEnv(t, "# whole line\n   # indented = comment\n\nTT_C1=1\n\t\n#TT_C2=2\n", false)
	expectValues(t, svc, "E1", map[string]any{"TT_C1": "1"})
	expectAbsent(t, svc, "E1", "TT_C2")
}

func TestParseDotEnv_ExportPrefix(t *testing.T) {
	svc := loadEnv(t, "export TT_E1=1\n  export   TT_E2 = 2\nexportTT_E3=3\n", false)
	expectValues(t, svc, "E1", map[string]any{"TT_E1": "1", "TT_E2": "2", "exportTT_E3": "3"})
}

func TestParseDotEnv_BOM(t *testing.T) {
	svc := loadEnv(t, "\xef\xbb\xbfTT_BOM=1\nTT_BOM2=2\n", false)
	expectValues(t, svc, "E1", map[string]any{"TT_BOM": "1", "TT_BOM2": "2"})
	expectAbsent(t, svc, "E1", "\xef\xbb\xbfTT_BOM")
}

func TestParseDotEnv_CRLF(t *testing.T) {
	svc := loadEnv(t, "TT_CR1=one\r\nTT_CR2=\"two\"\r\nTT_CR3='three'\r\n# comment\r\nTT_CR4=\r\n", false)
	expectValues(t, svc, "E1", map[string]any{"TT_CR1": "one", "TT_CR2": "two", "TT_CR3": "three", "TT_CR4": ""})
}

func TestParseDotEnv_NoTrailingNewline(t *testing.T) {
	svc := loadEnv(t, "TT_NT1=1\nTT_NT2=last", false)
	expectValues(t, svc, "E1", map[string]any{"TT_NT1": "1", "TT_NT2": "last"})
}

func TestParseDotEnv_EmptyFile(t *testing.T) {
	svc := loadEnv(t, "", false)
	if svc.State == nil {
		t.Error(test.DiffMessage(nil, "a state", "E13: an empty file still gives a working service"))
	}
}

func TestParseDotEnv_LineWithoutEquals(t *testing.T) {
	path, text := failEnv(t, "TT_OK=1\nNOEQUALS\n", false)
	want := "invalid config: " + path + ":2: expected KEY=value"
	if text != want {
		t.Error(test.DiffMessage(text, want, "E1/E9: a line that is not KEY=value"))
	}
}

func TestParseDotEnv_InvalidKey(t *testing.T) {
	for _, line := range []string{"1TT=1", "TT-X=1", "ÜBER=1", "TT.X=1"} {
		path, text := failEnv(t, "TT_OK=1\n"+line+"\n", false)
		want := "invalid config: " + path + ":2: invalid key"
		if text != want {
			t.Error(test.DiffMessage(text, want, "E1/E9: key of "+line))
		}
	}
}

func TestParseDotEnv_EqualsInValue(t *testing.T) {
	svc := loadEnv(t, "TT_URL=postgres://h/db?sslmode=disable\n", false)
	expectValues(t, svc, "E2", map[string]any{"TT_URL": "postgres://h/db?sslmode=disable"})
}

func TestParseDotEnv_Base64Padding(t *testing.T) {
	svc := loadEnv(t, "TT_B64=YWJj==\n", false)
	expectValues(t, svc, "E2", map[string]any{"TT_B64": "YWJj=="})
}

func TestParseDotEnv_HashInValue(t *testing.T) {
	svc := loadEnv(t, "TT_H=pa#ss\n", false)
	expectValues(t, svc, "E2", map[string]any{"TT_H": "pa#ss"})
}

func TestParseDotEnv_InlineComment(t *testing.T) {
	svc := loadEnv(t, "TT_I1=pa #ss\nTT_I2=pa\t#tabbed\nTT_I3=   # nothing\nTT_I4=a  #b #c\n", false)
	expectValues(t, svc, "E2", map[string]any{"TT_I1": "pa", "TT_I2": "pa", "TT_I3": "", "TT_I4": "a"})
}

func TestParseDotEnv_BackslashKept(t *testing.T) {
	svc := loadEnv(t, "TT_BS=C:\\new\\table\nTT_BS2=a\\\\b\\\"c\n", false)
	expectValues(t, svc, "E2", map[string]any{"TT_BS": `C:\new\table`, "TT_BS2": `a\\b\"c`})
}

func TestParseDotEnv_EmptyValue(t *testing.T) {
	svc := loadEnv(t, "TT_EMPTY=\n", false)
	expectValues(t, svc, "E2", map[string]any{"TT_EMPTY": ""})
}

func TestParseDotEnv_TrimsBlanks(t *testing.T) {
	svc := loadEnv(t, "  TT_TRIM  =   a  b \t \n", false)
	expectValues(t, svc, "E2", map[string]any{"TT_TRIM": "a  b"})
}

func TestParseDotEnv_SingleQuotesRemoved(t *testing.T) {
	svc := loadEnv(t, "TT_SQ='plain'\n", false)
	expectValues(t, svc, "E4", map[string]any{"TT_SQ": "plain"})
}

func TestParseDotEnv_DoubleQuoted(t *testing.T) {
	svc := loadEnv(t, "TT_DQ=\"x y\"\nTT_DQ2=\"  padded  \"\nTT_DQ3=\"#not a comment\"\nTT_DQ4=\"a=b\"\nTT_DQ5=\"x\"  # trailing comment\nTT_DQ6=\"x\"   \nTT_DQ7=\"\"\n", false)
	expectValues(t, svc, "E3", map[string]any{
		"TT_DQ":  "x y",
		"TT_DQ2": "  padded  ",
		"TT_DQ3": "#not a comment",
		"TT_DQ4": "a=b",
		"TT_DQ5": "x",
		"TT_DQ6": "x",
		"TT_DQ7": "",
	})
}

func TestParseDotEnv_DoubleQuotedMultiline(t *testing.T) {
	svc := loadEnv(t, "TT_ML=\"line1\nline2\n  line3\"\nTT_AFTER=1\n", false)
	expectValues(t, svc, "E3", map[string]any{"TT_ML": "line1\nline2\n  line3", "TT_AFTER": "1"})
}

func TestParseDotEnv_Escapes(t *testing.T) {
	svc := loadEnv(t, `TT_ESC="a\nb\tc\rd\\e\"f\$g"`+"\n", false)
	expectValues(t, svc, "E3", map[string]any{"TT_ESC": "a\nb\tc\rd\\e\"f$g"})
}

func TestParseDotEnv_UnknownEscapeKept(t *testing.T) {
	svc := loadEnv(t, `TT_UE="a\qb\x41\u0041\0"`+"\n", false)
	expectValues(t, svc, "E3", map[string]any{"TT_UE": `a\qb\x41\u0041\0`})
}

func TestParseDotEnv_TextAfterQuote(t *testing.T) {
	for _, line := range []string{`TT_TA="x"y`, `TT_TA="x" y`} {
		path, text := failEnv(t, "TT_OK=1\n"+line+"\n", false)
		want := "invalid config: " + path + ":2: unexpected text after the closing quote"
		if text != want {
			t.Error(test.DiffMessage(text, want, "E3/E9: "+line))
		}
	}
}

func TestParseDotEnv_UnterminatedQuote(t *testing.T) {
	for name, content := range map[string]string{
		"double":      "TT_OK=1\nTT_UQ=\"abc\nTT_NEXT=3\n",
		"single":      "TT_OK=1\nTT_UQ='abc\nTT_NEXT=3\n",
		"escaped end": "TT_OK=1\nTT_UQ=\"abc\\\"\nTT_NEXT=3\n",
	} {
		path, text := failEnv(t, content, false)
		want := "invalid config: " + path + ":2: unterminated quote"
		if text != want {
			t.Error(test.DiffMessage(text, want, "E3/E9: unterminated "+name+" quote reports the line where it opens"))
		}
	}
}

func TestParseDotEnv_SingleQuotedLiteral(t *testing.T) {
	svc := loadEnv(t, `TT_SL='a\nb \$ ${TT_X} "q" # not comment'`+"\nTT_SL2=''\n", true)
	expectValues(t, svc, "E4", map[string]any{"TT_SL": `a\nb \$ ${TT_X} "q" # not comment`, "TT_SL2": ""})
}

func TestParseDotEnv_SingleQuotedMultiline(t *testing.T) {
	svc := loadEnv(t, "TT_SM='one\ntwo\\n'\nTT_AFTER=1\n", false)
	expectValues(t, svc, "E4", map[string]any{"TT_SM": "one\ntwo\\n", "TT_AFTER": "1"})
}

func TestParseDotEnv_DuplicateKeyLastWins(t *testing.T) {
	svc := loadEnv(t, "TT_DUP=first\nTT_OTHER=x\nTT_DUP=second\n", false)
	expectValues(t, svc, "E5", map[string]any{"TT_DUP": "second", "TT_OTHER": "x"})
}

func TestExpand_SameFile(t *testing.T) {
	svc := loadEnv(t, "TT_B=world\nTT_A=hello_${TT_B}!\nTT_C=\"${TT_B}-${TT_B}\"\n", true)
	expectValues(t, svc, "E6", map[string]any{"TT_A": "hello_world!", "TT_C": "world-world"})
}

func TestExpand_ForwardReference(t *testing.T) {
	svc := loadEnv(t, "TT_A=${TT_B}!\nTT_B=${TT_C}+\nTT_C=x\n", true)
	expectValues(t, svc, "E6", map[string]any{"TT_A": "x+!", "TT_B": "x+"})
}

func TestExpand_FromEnvironment(t *testing.T) {
	t.Setenv("TT_FROM_ENV", "fromenv")
	svc := loadEnv(t, "TT_A=${TT_FROM_ENV}/x\n", true)
	expectValues(t, svc, "E6", map[string]any{"TT_A": "fromenv/x"})
}

func TestExpand_FileBeforeEnvironment(t *testing.T) {
	t.Setenv("TT_BOTH", "env")
	svc := loadEnv(t, "TT_BOTH=file\nTT_A=${TT_BOTH}\n", true)
	expectValues(t, svc, "E6", map[string]any{"TT_A": "file", "TT_BOTH": "env"})
}

func TestExpand_Undefined(t *testing.T) {
	path, text := failEnv(t, "TT_OK=1\nTT_A=${TT_NOPE_UNDEFINED}\n", true)
	want := "invalid config: " + path + ":2: ${TT_NOPE_UNDEFINED} is not defined"
	if text != want {
		t.Error(test.DiffMessage(text, want, "E6/E9: undefined variable"))
	}
}

func TestExpand_SelfReference(t *testing.T) {
	path, text := failEnv(t, "TT_OK=1\nTT_A=x${TT_A}\n", true)
	want := "invalid config: " + path + ":2: ${TT_A} refers to itself"
	if text != want {
		t.Error(test.DiffMessage(text, want, "E6/E9: self reference"))
	}
}

func TestExpand_CycleOfThree(t *testing.T) {
	path, text := failEnv(t, "TT_A=${TT_B}\nTT_B=${TT_C}\nTT_C=${TT_A}\n", true)
	pattern := regexp.MustCompile("^invalid config: " + regexp.QuoteMeta(path) + `:[123]: \$\{TT_[ABC]\} refers to itself$`)
	if !pattern.MatchString(text) {
		t.Error(test.DiffMessage(text, pattern.String(), "E6/E9: a cycle of three is found"))
	}
}

func TestExpand_LongCycle(t *testing.T) {
	var b strings.Builder
	const n = 5000
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "TT_V%d=${TT_V%d}\n", i, (i+1)%n)
	}
	_, text := failEnv(t, b.String(), true)
	if !strings.Contains(text, "refers to itself") {
		t.Error(test.DiffMessage(text, "... refers to itself", "E7: a cycle of any length is found without deep recursion"))
	}
}

func TestExpand_SingleQuotedNotExpanded(t *testing.T) {
	svc := loadEnv(t, "TT_B=b\nTT_A='${TT_B}'\n", true)
	expectValues(t, svc, "E6", map[string]any{"TT_A": "${TT_B}"})
}

func TestExpand_EscapedDollar(t *testing.T) {
	svc := loadEnv(t, "TT_B=b\nTT_A=\"\\${TT_B} and ${TT_B}\"\n", true)
	expectValues(t, svc, "E6", map[string]any{"TT_A": "${TT_B} and b"})
}

func TestExpand_BareDollarNeverExpanded(t *testing.T) {
	svc := loadEnv(t, "TT_B=b\nTT_A=$TT_B and $\nTT_C=\"$TT_B\"\n", true)
	expectValues(t, svc, "E6", map[string]any{"TT_A": "$TT_B and $", "TT_C": "$TT_B"})
}

func TestExpand_OffKeepsText(t *testing.T) {
	svc := loadEnv(t, "TT_B=b\nTT_A=${TT_B}\nTT_C=\"${TT_B}\\$\"\nTT_D=${NOT_DEFINED_ANYWHERE}\n", false)
	expectValues(t, svc, "E6", map[string]any{"TT_A": "${TT_B}", "TT_C": "${TT_B}$", "TT_D": "${NOT_DEFINED_ANYWHERE}"})
}

func TestExpand_ValueTooLarge(t *testing.T) {
	var b strings.Builder
	b.WriteString("TT_V0=aaaaaaaaaa\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "TT_V%d=${TT_V%d}${TT_V%d}\n", i, i-1, i-1)
	}
	path, text := failEnv(t, b.String(), true)
	pattern := regexp.MustCompile("^invalid config: " + regexp.QuoteMeta(path) + `:\d+: value larger than 64 KiB$`)
	if !pattern.MatchString(text) {
		t.Error(test.DiffMessage(text, pattern.String(), "E7: a doubling chain stops at 64 KiB"))
	}
}

func TestExpand_ValueAtLimit(t *testing.T) {
	big := strings.Repeat("x", 64<<10)
	svc := loadEnv(t, "TT_BIG="+big+"\nTT_COPY=${TT_BIG}\n", true)
	if got, _ := svc.Get("TT_COPY").(string); len(got) != 64<<10 {
		t.Error(test.DiffMessage(len(got), 64<<10, "E7: a value of exactly 64 KiB after expansion is accepted"))
	}
}

func TestExpand_TotalTooLarge(t *testing.T) {
	var b strings.Builder
	b.WriteString("TT_BIG=" + strings.Repeat("x", 64<<10) + "\n")
	for i := range 300 {
		fmt.Fprintf(&b, "TT_C%d=${TT_BIG}\n", i)
	}
	path, text := failEnv(t, b.String(), true)
	pattern := regexp.MustCompile("^invalid config: " + regexp.QuoteMeta(path) + `:\d+: values larger than 16 MiB in total after expansion$`)
	if !pattern.MatchString(text) {
		t.Error(test.DiffMessage(text, pattern.String(), "E7: copies of one large value stop at 16 MiB in total"))
	}
}

func TestExpand_TotalCountsChainsWhenExpanded(t *testing.T) {
	// The first key expands the whole chain before its own turn: each link is counted as it is
	// expanded, so the cap stops the chain and names a line inside it.
	var b strings.Builder
	n := 2000
	for i := range n {
		fmt.Fprintf(&b, "TT_R%d=${TT_R%d}\n", i, i+1)
	}
	fmt.Fprintf(&b, "TT_R%d=%s\n", n, strings.Repeat("x", 64<<10))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	path, text := failEnv(t, b.String(), true)
	runtime.ReadMemStats(&after)
	pattern := regexp.MustCompile("^invalid config: " + regexp.QuoteMeta(path) + `:(\d+): values larger than 16 MiB in total after expansion$`)
	m := pattern.FindStringSubmatch(text)
	if m == nil {
		t.Fatal(test.DiffMessage(text, pattern.String(), "E7: a chain of references stops at 16 MiB in total"))
	}
	if line, _ := strconv.Atoi(m[1]); line < 2 || line > n+1 {
		t.Error(test.DiffMessage(line, "a line of the chain", "E7: the error names the value that crossed the limit"))
	}
	if got := (after.TotalAlloc - before.TotalAlloc) >> 20; got > 64 {
		t.Error(test.DiffMessage(got, "at most 64 MiB allocated", "E7: expansion stops near the cap"))
	}
}

func TestExpand_TotalAtLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("TT_BIG=" + strings.Repeat("x", 64<<10) + "\n")
	for i := range 255 {
		fmt.Fprintf(&b, "TT_C%d=${TT_BIG}\n", i)
	}
	svc := loadEnv(t, b.String(), true)
	if got, _ := svc.Get("TT_C254").(string); len(got) != 64<<10 {
		t.Error(test.DiffMessage(len(got), 64<<10, "E7: exactly 16 MiB in total after expansion is accepted"))
	}
}

func TestLoadDotEnv_ValueTooLarge(t *testing.T) {
	path, text := failEnv(t, "TT_OK=1\nTT_BIG="+strings.Repeat("x", 64<<10+1)+"\n", false)
	want := "invalid config: " + path + ":2: value larger than 64 KiB"
	if text != want {
		t.Error(test.DiffMessage(text, want, "E7: a value above 64 KiB"))
	}
}

func TestLoadDotEnv_TooLarge(t *testing.T) {
	path, text := failEnv(t, "TT_A=1\n#"+strings.Repeat("x", 1<<20), false)
	want := "invalid config: " + path + ": larger than 1 MiB"
	if text != want {
		t.Error(test.DiffMessage(text, want, "E7/E9: a file above 1 MiB"))
	}
}

func TestLoadDotEnv_AtSizeLimit(t *testing.T) {
	head := "TT_LIM=1\n#"
	content := head + strings.Repeat("x", 1<<20-len(head)-1) + "\n"
	if len(content) != 1<<20 {
		t.Fatal(len(content))
	}
	svc := loadEnv(t, content, false)
	expectValues(t, svc, "E7", map[string]any{"TT_LIM": "1"})
}

func TestLoadDotEnv_NotRegularFile(t *testing.T) {
	dir := t.TempDir()
	text, panicked := panicText(func() {
		Register(&ConfigModuleOptions{EnvFilePaths: []string{dir}})
	})
	want := "invalid config: " + dir + ": not a regular file"
	if !panicked || text != want {
		t.Error(test.DiffMessage(text, want, "E7/E9: a directory is not a regular file"))
	}
}

func TestLoadDotEnv_NULByte(t *testing.T) {
	path, text := failEnv(t, "TT_OK=1\nTT_NUL=a\x00b\n", false)
	want := "invalid config: " + path + ":2: NUL byte"
	if text != want {
		t.Error(test.DiffMessage(text, want, "E7/E9: a NUL byte"))
	}
}

func TestLoadDotEnv_MissingFile(t *testing.T) {
	path := t.TempDir() + "/missing.env"
	text, panicked := panicText(func() {
		Register(&ConfigModuleOptions{EnvFilePaths: []string{path}})
	})
	prefix := "invalid config: " + path + ": "
	if !panicked || !strings.HasPrefix(text, prefix) || !strings.Contains(text, "no such file") {
		t.Error(test.DiffMessage(text, prefix+"<the os error>", "E9: <path>: <the os error>"))
	}
}

func TestLoadDotEnv_UnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	path := writeEnv(t, "TT_X="+canary+"\n")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	text, panicked := panicText(func() {
		Register(&ConfigModuleOptions{EnvFilePaths: []string{path}})
	})
	prefix := "invalid config: " + path + ": "
	if !panicked || !strings.HasPrefix(text, prefix) || !strings.Contains(text, "permission denied") {
		t.Error(test.DiffMessage(text, prefix+"<the os error>", "E9: an unreadable file"))
	}
}

func TestConfig_PanicsNeverContainValues(t *testing.T) {
	fixtures := map[string]string{
		"unterminated double": "TT_A=\"" + canary + "\nTT_B=2\n",
		"unterminated single": "TT_A='" + canary + "\n",
		"text after quote":    "TT_A=\"" + canary + "\"junk\n",
		"invalid key":         "1TT_A=" + canary + "\n",
		"no equals":           "TT_NO_EQUALS " + canary + "\n",
		"nul byte":            "TT_A=" + canary + "\x00\n",
		"undefined":           "TT_A=" + canary + "${TT_UNDEFINED_ANYWHERE}\n",
		"self reference":      "TT_A=" + canary + "${TT_A}\n",
		"cycle":               "TT_A=" + canary + "${TT_B}\nTT_B=${TT_A}\n",
		"too large":           "TT_A=" + canary + strings.Repeat("x", 64<<10) + "\n",
		"too large expanded":  "TT_A=" + canary + strings.Repeat("x", 40<<10) + "\nTT_B=${TT_A}${TT_A}\n",
	}
	for name, content := range fixtures {
		path, text := failEnv(t, content, true)
		if strings.Contains(text, canary) {
			t.Error(test.DiffMessage(text, "no value", "E10: panic for "+name+" leaked a value"))
		}
		if strings.Contains(text, "TT_NO_EQUALS") {
			t.Error(test.DiffMessage(text, "no line text", "E10: panic for "+name+" leaked a line"))
		}
		if !strings.HasPrefix(text, "invalid config: "+path+":") {
			t.Error(test.DiffMessage(text, "invalid config: "+path+":<line>: ...", "E10: panic for "+name+" names the path and line"))
		}
	}
}
