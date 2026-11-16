package config

import (
	"path/filepath"
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

func BenchmarkMatchParams_Found(b *testing.B) {
	v := "prefix_${KEY1}_middle_${KEY2}_suffix_${KEY3}"
	for b.Loop() {
		matchParams(v)
	}
}

func BenchmarkMatchParams_None(b *testing.B) {
	v := "no_params_here_at_all"
	for b.Loop() {
		matchParams(v)
	}
}

func BenchmarkUnmarshal(b *testing.B) {
	data := append([]byte(`KEY_1=1
KEY2=22
key_3=333
KEY_5=666666
KEY_6="#999=99 10"
KEY_8=88888888
`), newline)
	for b.Loop() {
		dotENV := &DotENV{
			data:           data,
			valuesByEnvKey: make(map[string]any, 8),
		}
		dotENV.Unmarshal()
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

func BenchmarkLoadOSEnv(b *testing.B) {
	for b.Loop() {
		loadOSEnv()
	}
}

func BenchmarkLoadDotENV(b *testing.B) {
	envFilePath, _ := findRootDir(".env.test")
	path := filepath.Join(envFilePath, ".env.test")
	for b.Loop() {
		loadDotENV(path, false)
	}
}
