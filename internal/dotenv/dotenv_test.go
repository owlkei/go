package dotenv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestParse_KeysAndValues(t *testing.T) {
	file, err := Parse(".env", []byte("# c\nA=1\n\nexport B = \"two words\"\nC='x'\nD=\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := file.Keys(); !slices.Equal(got, []string{"A", "B", "C", "D"}) {
		t.Error(test.DiffMessage(got, []string{"A", "B", "C", "D"}, "E7: Keys in file order"))
	}
	cases := []struct {
		key, value string
		line       int
	}{{"A", "1", 2}, {"B", "two words", 4}, {"C", "x", 5}, {"D", "", 6}}
	for _, c := range cases {
		value, line, ok := file.Value(c.key)
		if !ok || value != c.value || line != c.line {
			t.Error(test.DiffMessage([]any{value, line, ok}, []any{c.value, c.line, true}, "E7: Value of "+c.key))
		}
	}
	if _, _, ok := file.Value("MISSING"); ok {
		t.Error(test.DiffMessage(true, false, "E7: a missing key is not ok"))
	}
}

func TestParse_NoVariableExpansion(t *testing.T) {
	file, err := Parse(".env", []byte("A=1\nB=${A}-x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if value, _, _ := file.Value("B"); value != "${A}-x" {
		t.Error(test.DiffMessage(value, "${A}-x", "E7: Parse does not expand"))
	}
}

func TestParse_SyntaxErrorNamesLineNeverValue(t *testing.T) {
	_, err := Parse(".env", []byte("A=1\nB=2\nC=3\nD=\"topsecretvalue\n"))
	if err == nil {
		t.Fatal(test.DiffMessage(nil, "an error", "E8: an unterminated quote is an error"))
	}
	if !strings.Contains(err.Error(), ".env:4") || strings.Contains(err.Error(), "topsecretvalue") {
		t.Error(test.DiffMessage(err.Error(), ".env:4: … without the value", "E8: the line, never the value"))
	}
}

func TestLoad_ExpandsWhenAsked(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("A=1\nB=${A}-x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path, true)
	if err != nil || got["B"] != "1-x" {
		t.Error(test.DiffMessage([]any{got["B"], err}, "1-x", "E10: Load expands references"))
	}
	got, err = Load(path, false)
	if err != nil || got["B"] != "${A}-x" {
		t.Error(test.DiffMessage([]any{got["B"], err}, "${A}-x", "E10: Load keeps them literal"))
	}
}

func TestLoad_ErrorKeepsModuleFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("A=1\n=oops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path, false)
	if err == nil || !strings.Contains(err.Error(), "invalid config: "+path+":2:") {
		t.Error(test.DiffMessage(err, "invalid config: <path>:2: …", "E10: the error text of modules/config is unchanged"))
	}
}
