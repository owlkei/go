package memorycache

import (
	"testing"
)

func BenchmarkHashKey(b *testing.B) {
	key := "some-realistic-cache-key-value"
	for b.Loop() {
		hashKey(key)
	}
}
