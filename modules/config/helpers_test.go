package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func envFixture(b *testing.B) string {
	b.Helper()
	var sb strings.Builder
	sb.WriteString("# bench fixture\n")
	for i := 0; i < 40; i++ {
		n := strconv.Itoa(i)
		sb.WriteString("BENCH_KEY_" + n + "=value-" + n + " # comment\n")
		sb.WriteString("BENCH_URL_" + n + "=postgres://host/db?sslmode=disable&n=" + n + "\n")
		sb.WriteString("BENCH_QUOTED_" + n + "=\"line one\\nline two ${BENCH_KEY_" + n + "}\"\n")
		sb.WriteString("BENCH_SINGLE_" + n + "='literal ${BENCH_KEY_" + n + "}'\n")
	}
	path := filepath.Join(b.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func projectDir(t *testing.T, envContent string, withEnv bool) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if withEnv {
		if err := os.WriteFile(filepath.Join(root, ".env"), []byte(envContent), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func nested(depth int) map[string]any {
	root := map[string]any{}
	current := root
	for i := 0; i < depth; i++ {
		next := map[string]any{}
		current["n"] = next
		current = next
	}
	current["leaf"] = canary
	return root
}

type boundConfig struct {
	Name  string  `bind:"TT_TR_NAME"`
	Port  int     `bind:"TT_TR_PORT"`
	Big   int64   `bind:"TT_TR_BIG"`
	Small uint    `bind:"TT_TR_SMALL"`
	On    bool    `bind:"TT_TR_ON"`
	Ratio float64 `bind:"TT_TR_RATIO"`
	Half  float32 `bind:"TT_TR_HALF"`
}

func newService(t *testing.T) ConfigService {
	t.Helper()
	return registerWith(t, &ConfigModuleOptions{IsIgnoreEnvFile: true})
}

type holder struct {
	Label  string
	Config ConfigService
}

const canary = "S3CR3T-canary-4f9a"

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

func panicText(fn func()) (text string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			text = ansiPattern.ReplaceAllString(fmt.Sprint(r), "")
		}
	}()
	fn()
	return "", false
}

func writeEnv(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func registerWith(t *testing.T, opts *ConfigModuleOptions) ConfigService {
	t.Helper()
	var svc ConfigService
	o := *opts
	o.Hooks = append(slices.Clone(opts.Hooks), func(s ConfigService) { svc = s })
	Register(&o)
	return svc
}

func loadEnv(t *testing.T, content string, expand bool) ConfigService {
	t.Helper()
	path := writeEnv(t, content)
	return registerWith(t, &ConfigModuleOptions{EnvFilePaths: []string{path}, IsExpandVariables: expand})
}

func failEnv(t *testing.T, content string, expand bool) (path, text string) {
	t.Helper()
	path = writeEnv(t, content)
	text, panicked := panicText(func() {
		Register(&ConfigModuleOptions{EnvFilePaths: []string{path}, IsExpandVariables: expand})
	})
	if !panicked {
		t.Fatal(test.DiffMessage("no panic", "a panic", "Register must panic for this file"))
	}
	return path, text
}

func expectValues(t *testing.T, svc ConfigService, rule string, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if got := svc.Get(key); got != value {
			t.Error(test.DiffMessage(fmt.Sprintf("%#v", got), fmt.Sprintf("%#v", value), rule+": value of "+key))
		}
	}
}

func expectAbsent(t *testing.T, svc ConfigService, rule string, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if got := svc.Get(key); got != nil {
			t.Error(test.DiffMessage(fmt.Sprintf("%#v", got), "nil", rule+": "+key+" must not be set"))
		}
	}
}
