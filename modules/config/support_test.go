package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

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
	return registerWith(t, &ConfigModuleOptions{ENVFilePaths: []string{path}, IsExpandVariables: expand})
}

func failEnv(t *testing.T, content string, expand bool) (path, text string) {
	t.Helper()
	path = writeEnv(t, content)
	text, panicked := panicText(func() {
		Register(&ConfigModuleOptions{ENVFilePaths: []string{path}, IsExpandVariables: expand})
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
