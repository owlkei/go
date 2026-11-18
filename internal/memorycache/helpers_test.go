package memorycache

import (
	"context"
	"encoding/binary"
	"strconv"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
)

var background = context.Background()

const shardCapacity = 8

func newCache(maxEntries int, maxBytes int64) *MemoryCache {
	return NewMemoryCache(&MemoryCacheOptions{MaxEntries: maxEntries, MaxBytes: maxBytes})
}

func stateOf(t *testing.T, mc *MemoryCache) inspect.CacheState {
	t.Helper()
	st, ok := inspect.CacheStateOf(mc)
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "M16: inspect.CacheStateOf(*MemoryCache) must report true"))
	}
	return st
}

func shardKeys(shard, n int, prefix string) []string {
	keys := make([]string, 0, n)
	for i := 0; len(keys) < n; i++ {
		k := prefix + strconv.Itoa(i)
		if int(hashKey(k)&63) == shard {
			keys = append(keys, k)
		}
	}
	return keys
}

func be(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

func fromBE(b []byte) int64 {
	return int64(binary.BigEndian.Uint64(b))
}

func mustSet(t *testing.T, mc *MemoryCache, key string, val []byte, ttl time.Duration) {
	t.Helper()
	if err := mc.Set(background, key, val, ttl); err != nil {
		t.Fatal(test.DiffMessage(err, nil, "Set "+key))
	}
}

func present(mc *MemoryCache, key string) bool {
	_, ok := mc.Get(background, key)
	return ok
}
