package bench

import (
	"testing"
)

func BenchmarkGin(b *testing.B) { benchmarkHandler(b, NewGin()) }
