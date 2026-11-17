package main

// Entry point the tests drive (the builder implements exactly this):
//
//	func run(dir string, allowed []string, write bool, out io.Writer) (code int)
//
// dir is the module root (go.mod). allowed lists module-relative import paths of the public
// packages: "." is the root, "log" is one package, "middlewares/*" is every package below
// middlewares/; nil means the tool's built-in list. write=true writes dir/api/ginject.txt and
// returns 0; write=false compares with it, prints differing lines ("+ line" / "- line", one per
// line) and the B1 message to out, and returns 1 on any difference. Lines name a package by the
// last element of the module path plus the relative path ("pkg fix/log, ..."). A B1 message is
// "api: package <full import path> is importable: move it under internal/ or add it to the allow list".

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

var fixtureFiles = map[string]string{
	"go.mod": "module example.com/fix\n\ngo 1.21\n",
	"fix.go": `package fix

import (
	"example.com/fix/internal/ctx"
	"example.com/fix/internal/lonely"
)

const Version = 2

type HTTPContext = *ctx.Ctx

func New(name string) int { return len(name) }

func Use(l lonelyAlias) {}

type lonelyAlias = int

var _ = lonely.Helper
`,
	"log/log.go": `package log

func Info(msg string) {}
`,
	"internal/ctx/ctx.go": `package ctx

import "net/http"

type Ctx struct {
	Request *http.Request
}

func (c *Ctx) Status(code int) *Result { return nil }

type Result struct{}

func (r *Result) Done() {}

func Helper() {}
`,
	"internal/lonely/lonely.go": `package lonely

func Helper() {}
`,
}

var fixtureAllowed = []string{".", "log", "middlewares/*"}

func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range fixtureFiles {
		writeFile(t, dir, name, content)
	}
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	old, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, name, string(old)+content)
}

func generate(t *testing.T, dir string) {
	t.Helper()
	var out bytes.Buffer
	if code := run(dir, fixtureAllowed, true, &out); code != 0 {
		t.Fatalf("write failed with %d: %s", code, out.String())
	}
}

func check(dir string) (int, []string) {
	var out bytes.Buffer
	code := run(dir, fixtureAllowed, false, &out)
	var lines []string
	for _, l := range strings.Split(out.String(), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return code, lines
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func TestAPICheck_WriteProducesSortedLines(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	data, err := os.ReadFile(filepath.Join(dir, "api", "ginject.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, want := range []string{
		"pkg fix, const Version untyped int = 2",
		"pkg fix, func New(string) int",
		"pkg fix, type HTTPContext = *ctx.Ctx",
		"pkg fix/log, func Info(string)",
		"reach ctx.Ctx, field Request *http.Request",
		"reach ctx.Ctx, method (*ctx.Ctx) Status(int) *ctx.Result",
		"reach ctx.Result, method (*ctx.Result) Done()",
	} {
		if !contains(lines, want) {
			t.Error(test.DiffMessage(lines, want, "B2: generated file lacks line"))
		}
	}
	sorted := append([]string(nil), lines...)
	for i := 1; i < len(sorted); i++ {
		if sorted[i-1] > sorted[i] {
			t.Error(test.DiffMessage(lines, "sorted", "B2: generated file is not sorted"))
			break
		}
	}
}

func TestAPICheck_UpToDatePasses(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	code, lines := check(dir)
	if code != 0 {
		t.Error(test.DiffMessage(code, 0, "B2: up-to-date exit code"))
	}
	if len(lines) != 0 {
		t.Error(test.DiffMessage(lines, []string(nil), "B2: up-to-date output"))
	}
}

func TestAPICheck_UnknownPublicPackageFails(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	writeFile(t, dir, "extra/extra.go", "package extra\n\nfunc X() {}\n")
	code, lines := check(dir)
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "B1: exit code"))
	}
	want := "api: package example.com/fix/extra is importable: move it under internal/ or add it to the allow list"
	if !contains(lines, want) {
		t.Error(test.DiffMessage(lines, want, "B1: message"))
	}
}

func TestAPICheck_AllowedPrefixAndExactPass(t *testing.T) {
	dir := writeFixture(t)
	writeFile(t, dir, "middlewares/cors/cors.go", "package cors\n\nfunc New() {}\n")
	generate(t, dir)
	code, lines := check(dir)
	if code != 0 || len(lines) != 0 {
		t.Error(test.DiffMessage(lines, []string(nil), "B1: allowed prefix package must pass"))
	}
}

func TestAPICheck_InternalSampleBenchAreNotPublic(t *testing.T) {
	dir := writeFixture(t)
	writeFile(t, dir, "sample/app/main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "bench/b/b.go", "package b\n")
	generate(t, dir)
	code, lines := check(dir)
	if code != 0 || len(lines) != 0 {
		t.Error(test.DiffMessage(lines, []string(nil), "B1: sample, bench and internal are not public"))
	}
}

func TestAPICheck_AddedFuncFails(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	appendFile(t, dir, "log/log.go", "\nfunc Foo() {}\n")
	code, lines := check(dir)
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "B2: exit code"))
	}
	want := "+ pkg fix/log, func Foo()"
	if !contains(lines, want) {
		t.Error(test.DiffMessage(lines, want, "B2: added func line"))
	}
}

func TestAPICheck_RemovedFuncFails(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	writeFile(t, dir, "log/log.go", "package log\n")
	code, lines := check(dir)
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "B2: exit code"))
	}
	want := "- pkg fix/log, func Info(string)"
	if !contains(lines, want) {
		t.Error(test.DiffMessage(lines, want, "B2: removed func line"))
	}
}

func TestAPICheck_ChangedSignatureShowsBothLines(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	writeFile(t, dir, "log/log.go", "package log\n\nfunc Info(msg string, level int) {}\n")
	code, lines := check(dir)
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "B2: exit code"))
	}
	for _, want := range []string{"- pkg fix/log, func Info(string)", "+ pkg fix/log, func Info(string, int)"} {
		if !contains(lines, want) {
			t.Error(test.DiffMessage(lines, want, "B2: changed signature line"))
		}
	}
}

func TestAPICheck_ParamRenamePasses(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	writeFile(t, dir, "log/log.go", "package log\n\n// Info logs.\nfunc Info(text string) {}\n")
	code, lines := check(dir)
	if code != 0 || len(lines) != 0 {
		t.Error(test.DiffMessage(lines, []string(nil), "B2: parameter rename and comment must pass"))
	}
}

func TestAPICheck_ReachableMethodFails(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	appendFile(t, dir, "internal/ctx/ctx.go", "\nfunc (c *Ctx) Foo() {}\n")
	code, lines := check(dir)
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "B3: exit code"))
	}
	want := "+ reach ctx.Ctx, method (*ctx.Ctx) Foo()"
	if !contains(lines, want) {
		t.Error(test.DiffMessage(lines, want, "B3: reach line"))
	}
}

func TestAPICheck_InternalFuncPasses(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	appendFile(t, dir, "internal/ctx/ctx.go", "\nfunc Another() {}\n")
	appendFile(t, dir, "internal/lonely/lonely.go", "\ntype T struct{ A int }\n")
	code, lines := check(dir)
	if code != 0 || len(lines) != 0 {
		t.Error(test.DiffMessage(lines, []string(nil), "B3: unreached internal names must pass"))
	}
}

func TestAPICheck_ReachIsTransitive(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	appendFile(t, dir, "internal/ctx/ctx.go", "\nfunc (r *Result) Foo() {}\n")
	code, lines := check(dir)
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "B3: exit code"))
	}
	want := "+ reach ctx.Result, method (*ctx.Result) Foo()"
	if !contains(lines, want) {
		t.Error(test.DiffMessage(lines, want, "B3: transitive reach line"))
	}
}

func TestAPICheck_ReachedFieldChangeFails(t *testing.T) {
	dir := writeFixture(t)
	generate(t, dir)
	appendFile(t, dir, "internal/ctx/ctx.go", "\ntype Extra struct{}\n\nfunc (c *Ctx) Hidden() Extra { return Extra{} }\n")
	code, lines := check(dir)
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "B3: exit code"))
	}
	want := "+ reach ctx.Ctx, method (*ctx.Ctx) Hidden() ctx.Extra"
	if !contains(lines, want) {
		t.Error(test.DiffMessage(lines, want, "B3: new reachable method line"))
	}
}

func TestAPIFile_MatchesCode(t *testing.T) {
	var out bytes.Buffer
	code := run("../..", nil, false, &out)
	if code != 0 {
		t.Error(test.DiffMessage(out.String(), "", "B2: api/ginject.txt matches the code"))
	}
}

func TestAPICheck_ReachedNamedTypeUnderlyingChangeFails(t *testing.T) {
	dir := writeFixture(t)
	appendFile(t, dir, "internal/ctx/ctx.go", "\ntype Level int\n\nfunc (c *Ctx) Level() Level { return 0 }\n")
	generate(t, dir)
	writeFile(t, dir, "internal/ctx/ctx.go", strings.Replace(fixtureFiles["internal/ctx/ctx.go"], "type Result struct{}", "type Result struct{}\n\ntype Level string\n\nfunc (c *Ctx) Level() Level { return \"\" }", 1))
	code, lines := check(dir)
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "a reached type's underlying type change: exit code"))
	}
	for _, want := range []string{"- reach ctx.Level, type int", "+ reach ctx.Level, type string"} {
		if !contains(lines, want) {
			t.Error(test.DiffMessage(lines, want, "the reached type's underlying type line"))
		}
	}
}

func TestAPICheck_ReachedAliasTargetChangeFails(t *testing.T) {
	dir := writeFixture(t)
	appendFile(t, dir, "internal/ctx/ctx.go", "\ntype Hook = func()\n\nfunc (c *Ctx) Hook() Hook { return nil }\n")
	generate(t, dir)
	writeFile(t, dir, "internal/ctx/ctx.go", fixtureFiles["internal/ctx/ctx.go"]+"\ntype Hook = func(int)\n\nfunc (c *Ctx) Hook() Hook { return nil }\n")
	code, lines := check(dir)
	if code != 1 {
		t.Error(test.DiffMessage(code, 1, "an internal alias's target change: exit code"))
	}
	for _, want := range []string{"- reach ctx.Hook, type = func()", "+ reach ctx.Hook, type = func(int)"} {
		if !contains(lines, want) {
			t.Error(test.DiffMessage(lines, want, "the reached alias's target line"))
		}
	}
}

func TestAPICheck_ReachedInterfaceUnexportedMethodFails(t *testing.T) {
	dir := writeFixture(t)
	appendFile(t, dir, "internal/ctx/ctx.go", "\ntype Role interface{ Name() string }\n\nfunc (c *Ctx) Role() Role { return nil }\n")
	generate(t, dir)
	writeFile(t, dir, "internal/ctx/ctx.go", fixtureFiles["internal/ctx/ctx.go"]+"\ntype Role interface {\n\tName() string\n\tseal()\n}\n\nfunc (c *Ctx) Role() Role { return nil }\n")
	code, lines := check(dir)
	if code != 1 || !contains(lines, "+ reach ctx.Role, unexported methods") {
		t.Error(test.DiffMessage(lines, "+ reach ctx.Role, unexported methods", "a sealed interface can't be implemented by users"))
	}
}
