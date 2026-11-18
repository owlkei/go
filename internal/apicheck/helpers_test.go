package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
