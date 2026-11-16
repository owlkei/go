package core

import (
	"os"
	"strconv"
	"testing"
)

func BenchmarkIsDynamicModule(b *testing.B) {
	s := "func(pkg.Provider) *core.Module"
	b.ReportAllocs()
	for b.Loop() {
		isDynamicModule(s)
	}
}

func BenchmarkModuleBuilderBuild(b *testing.B) {
	p := &mockProvider{}
	c := &mockController{}
	child := ModuleBuilder().Build()
	for b.Loop() {
		ModuleBuilder().Imports(child).Providers(p).Controllers(c).Build()
	}
}

func BenchmarkGetModuleType(b *testing.B) {
	child1 := ModuleBuilder().Build()
	child2 := ModuleBuilder().Build()
	child3 := ModuleBuilder().Build()
	builder := ModuleBuilder().Imports(child1, child2, child3)
	for b.Loop() {
		builder.getModuleType()
	}
}

func BenchmarkParseModuleFile_DistinctFiles(b *testing.B) {
	const n = 500
	dir := b.TempDir()
	paths := make([]string, n)
	for i := range paths {
		path := dir + "/f" + strconv.Itoa(i) + ".go"
		src := "package pkg" + strconv.Itoa(i) + "\n\nvar Module = func() *int { return nil }\n"
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			b.Fatal(err)
		}
		paths[i] = path
	}

	for i := 0; b.Loop(); i++ {
		path := paths[i%n]

		b.StopTimer()
		moduleNameFileCache.Delete(path)
		b.StartTimer()

		parseModuleFile(path)
	}
}
