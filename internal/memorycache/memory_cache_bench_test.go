package memorycache

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func BenchmarkGet_Hit(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	if err := mc.Set(ctx, "key", []byte("benchmark-value-data-here"), 0); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mc.Get(ctx, "key")
		}
	})
}

func BenchmarkGet_Miss(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mc.Get(ctx, "nonexistent-key")
		}
	})
}

func BenchmarkSet_NoTTL(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	val := []byte("benchmark-value-data-here")
	for i := 0; b.Loop(); i++ {
		_ = mc.Set(ctx, fmt.Sprintf("key-%d", i), val, 0)
	}
}

func BenchmarkSet_WithTTL(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	val := []byte("benchmark-value-data-here")
	for i := 0; b.Loop(); i++ {
		_ = mc.Set(ctx, fmt.Sprintf("key-%d", i), val, time.Minute)
	}
}

func BenchmarkSetNX_New(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	val := []byte("benchmark-value-data-here")
	for i := 0; b.Loop(); i++ {
		_, _ = mc.SetNX(ctx, fmt.Sprintf("key-%d", i), val, 0)
	}
}

func BenchmarkSetNX_Exists(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	val := []byte("benchmark-value-data-here")
	if err := mc.Set(ctx, "exists", val, 0); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = mc.SetNX(ctx, "exists", val, 0)
		}
	})
}

func BenchmarkDelete(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	val := []byte("v")
	for i := 0; b.Loop(); i++ {
		key := fmt.Sprintf("key-%d", i)
		b.StopTimer()
		_ = mc.Set(ctx, key, val, 0)
		b.StartTimer()
		_ = mc.Delete(ctx, key)
	}
}

func BenchmarkKeys_1000(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		if err := mc.Set(ctx, fmt.Sprintf("key-%d", i), []byte("v"), 0); err != nil {
			b.Fatal(err)
		}
	}
	for b.Loop() {
		mc.Keys(ctx)
	}
}

func BenchmarkTTL_Hit(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	if err := mc.Set(ctx, "key", []byte("v"), time.Minute); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mc.TTL(ctx, "key")
		}
	})
}

func BenchmarkHashKey(b *testing.B) {
	key := "some-realistic-cache-key-value"
	for b.Loop() {
		hashKey(key)
	}
}

func BenchmarkGetSetParallel(b *testing.B) {
	mc := NewMemoryCache(nil)

	ctx := context.Background()
	keys := make([]string, 4096)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i)
		_ = mc.Set(ctx, keys[i], []byte("value-payload"), time.Hour)
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			k := keys[i&4095]
			if i&7 == 0 {
				_ = mc.Set(ctx, k, []byte("value-payload"), time.Hour)
			} else {
				mc.Get(ctx, k)
			}
			i++
		}
	})
}

func BenchmarkGetParallel(b *testing.B) {
	mc := NewMemoryCache(nil)

	ctx := context.Background()
	keys := make([]string, 4096)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i)
		_ = mc.Set(ctx, keys[i], []byte("value-payload"), time.Hour)
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			mc.Get(ctx, keys[i&4095])
			i++
		}
	})
}

func BenchmarkGetParallel_LongKeys(b *testing.B) {
	mc := NewMemoryCache(nil)

	ctx := context.Background()
	keys := make([]string, 4096)
	for i := range keys {
		keys[i] = "tenant:acme-corp:user:session:" + strconv.Itoa(i) + ":profile-cache-entry"
		_ = mc.Set(ctx, keys[i], []byte("value-payload"), time.Hour)
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			mc.Get(ctx, keys[i&4095])
			i++
		}
	})
}

func BenchmarkIncr(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	keys := make([]string, 4096)
	for i := range keys {
		keys[i] = "counter:" + strconv.Itoa(i)
		if _, err := mc.Incr(ctx, keys[i], 1, time.Hour); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, err := mc.Incr(ctx, keys[i&4095], 1, time.Hour); err != nil {
				b.Error(err)
				return
			}
			i++
		}
	})
}

func BenchmarkIncr_OneKey(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := mc.Incr(ctx, "hot", 1, time.Hour); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func BenchmarkSet_Existing(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	val := []byte("benchmark-value-data-here")
	if err := mc.Set(ctx, "key", val, time.Hour); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = mc.Set(ctx, "key", val, time.Hour)
		}
	})
}

func BenchmarkSet_NewAtBound(b *testing.B) {
	mc := NewMemoryCache(nil)
	ctx := context.Background()
	val := []byte("12345678")
	keys := make([]string, 4*65536)
	for i := range keys {
		keys[i] = "tenant:acme:client:" + strconv.Itoa(i)
	}
	for _, k := range keys[:65536] {
		_ = mc.Set(ctx, k, val, 0)
	}
	b.ReportAllocs()
	for i := 65536; b.Loop(); i++ {
		_ = mc.Set(ctx, keys[i%len(keys)], val, 0)
	}
}

func BenchmarkHeapAtBound(b *testing.B) {
	ctx := context.Background()
	val := []byte("12345678")
	var perEntry float64
	for b.Loop() {
		b.StopTimer()
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		mc := NewMemoryCache(nil)
		for i := 0; i < 2*65536; i++ {
			_ = mc.Set(ctx, "tenant:acme:session:"+strconv.Itoa(i), val, 0)
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		perEntry = float64(after.HeapAlloc-before.HeapAlloc) / float64(len(mc.Keys(ctx)))
		runtime.KeepAlive(mc)
		b.StartTimer()
	}
	b.ReportMetric(perEntry, "B/entry")
}
