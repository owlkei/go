package memorycache

import (
	"strconv"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestHashKey_DistributesAcrossShards(t *testing.T) {
	const keys = numShards * 40

	counts := map[uint64]int{}
	for i := 0; i < keys; i++ {
		counts[hashKey("tenant:acme:user:session:"+strconv.Itoa(i))&shardMask]++
	}

	maxCount := 0
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	empty := numShards - len(counts)

	if empty > numShards/10 {
		t.Error(test.DiffMessage(empty, 0, "too many shards received no key; the hash is not spreading keys"))
	}
	if maxCount > 40*6 {
		t.Error(test.DiffMessage(maxCount, 40, "one shard absorbed far more keys than its fair share"))
	}
}

func TestHashKey_Deterministic(t *testing.T) {
	const key = "tenant:acme:user:session:42"

	first := hashKey(key)
	for i := 0; i < 100; i++ {
		if got := hashKey(key); got != first {
			t.Fatal(test.DiffMessage(got, first, "hashKey must be stable for the same key within a process"))
		}
	}
}

func TestHashKey_EmptyKeyDoesNotPanic(t *testing.T) {
	_ = hashKey("")
}
