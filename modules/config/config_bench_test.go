package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func BenchmarkIsValidKey_Valid(b *testing.B) {
	for b.Loop() {
		isValidKey("DATABASE_URL")
	}
}

func BenchmarkIsValidKey_Invalid(b *testing.B) {
	for b.Loop() {
		isValidKey("2INVALID_KEY")
	}
}

func BenchmarkFlatten(b *testing.B) {
	for b.Loop() {
		org := map[string]any{
			"key1": "value1",
			"nested": map[string]any{
				"key2": "value2",
				"arr":  []any{"a", "b", "c"},
			},
		}
		flatten(org, make(map[string]any), "")
	}
}

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

func BenchmarkRegister_File(b *testing.B) {
	path := envFixture(b)
	opts := &ConfigModuleOptions{ENVFilePaths: []string{path}}
	b.ReportAllocs()
	for b.Loop() {
		Register(opts)
	}
}

func BenchmarkRegister_FileExpandVariables(b *testing.B) {
	path := envFixture(b)
	opts := &ConfigModuleOptions{ENVFilePaths: []string{path}, IsExpandVariables: true}
	b.ReportAllocs()
	for b.Loop() {
		Register(opts)
	}
}

func BenchmarkConfigService_Get(b *testing.B) {
	var svc ConfigService
	Register(&ConfigModuleOptions{IsIgnoreEnvFile: true, Hooks: []ConfigHookFn{func(s ConfigService) { svc = s }}})
	svc.Set("BENCH_GET", "value")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			svc.Get("BENCH_GET")
		}
	})
}

func BenchmarkConfigService_Set(b *testing.B) {
	var svc ConfigService
	Register(&ConfigModuleOptions{IsIgnoreEnvFile: true, Hooks: []ConfigHookFn{func(s ConfigService) { svc = s }}})
	b.ReportAllocs()
	for b.Loop() {
		svc.Set("BENCH_SET", "value")
	}
}
