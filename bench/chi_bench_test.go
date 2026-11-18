package bench

import (
	"testing"
)

func BenchmarkChi(b *testing.B) { benchmarkHandler(b, NewChi()) }
