package bench

import (
	"testing"
)

func BenchmarkNetHTTP(b *testing.B) { benchmarkHandler(b, NewNetHTTP()) }
