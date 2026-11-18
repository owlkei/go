package config

import (
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

func BenchmarkRegister_File(b *testing.B) {
	path := envFixture(b)
	opts := &ConfigModuleOptions{EnvFilePaths: []string{path}}
	b.ReportAllocs()
	for b.Loop() {
		Register(opts)
	}
}

func BenchmarkRegister_FileExpandVariables(b *testing.B) {
	path := envFixture(b)
	opts := &ConfigModuleOptions{EnvFilePaths: []string{path}, IsExpandVariables: true}
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
