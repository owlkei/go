package bench

import (
	"testing"
)

func BenchmarkEcho(b *testing.B) { benchmarkHandler(b, NewEcho()) }
