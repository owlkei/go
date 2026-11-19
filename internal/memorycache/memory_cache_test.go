package memorycache

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"unsafe"

	"go.owlky.com/owlky/internal/inspect"
	"go.owlky.com/owlky/internal/test"
)

func TestNewMemoryCache_Defaults(t *testing.T) {
	mc := NewMemoryCache(&MemoryCacheOptions{})
	st := stateOf(t, mc)
	if st.MaxEntries != 65536 {
		t.Error(test.DiffMessage(st.MaxEntries, 65536, "M1: default MaxEntries"))
	}
	if st.MaxBytes != 16<<20 {
		t.Error(test.DiffMessage(st.MaxBytes, int64(16<<20), "M1: default MaxBytes"))
	}
}

func TestNewMemoryCache_NilOptions(t *testing.T) {
	nilState := stateOf(t, NewMemoryCache(nil))
	emptyState := stateOf(t, NewMemoryCache(&MemoryCacheOptions{}))
	if nilState != emptyState {
		t.Error(test.DiffMessage(nilState, emptyState, "M1: NewMemoryCache(nil) equals NewMemoryCache(&MemoryCacheOptions{})"))
	}
	if nilState.MaxEntries != 65536 || nilState.MaxBytes != 16<<20 {
		t.Error(test.DiffMessage(nilState, "65536 entries, 16 MiB", "M1: nil options take the defaults"))
	}
}

func TestNewMemoryCache_RoundsMaxEntries(t *testing.T) {
	cases := []struct{ in, want int }{
		{64, 64},
		{65, 64},
		{100, 64},
		{127, 64},
		{128, 128},
		{130, 128},
		{1000, 960},
		{32768, 32768},
		{1, 64},
		{63, 64},
		{0, 65536},
		{-1, 65536},
		{math.MinInt, 65536},
	}
	for _, c := range cases {
		st := stateOf(t, newCache(c.in, 0))
		if st.MaxEntries != c.want {
			t.Error(test.DiffMessage(st.MaxEntries, c.want, fmt.Sprintf("M1: MaxEntries %d", c.in)))
		}
	}
}

func TestNewMemoryCache_MinimumBounds(t *testing.T) {
	cases := []struct{ in, want int64 }{
		{1, 64 << 10},
		{1000, 64 << 10},
		{64<<10 - 1, 64 << 10},
		{64 << 10, 64 << 10},
		{1 << 20, 1 << 20},
		{0, 16 << 20},
		{-1, 16 << 20},
		{math.MinInt64, 16 << 20},
	}
	for _, c := range cases {
		st := stateOf(t, newCache(0, c.in))
		if st.MaxBytes != c.want {
			t.Error(test.DiffMessage(st.MaxBytes, c.want, fmt.Sprintf("M1: MaxBytes %d", c.in)))
		}
	}
}

func TestNewMemoryCache_StartsNoGoroutineOrTimer(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	caches := make([]*MemoryCache, 50)
	for i := range caches {
		caches[i] = NewMemoryCache(nil)
	}
	if after := runtime.NumGoroutine(); after != before {
		t.Error(test.DiffMessage(after, before, "M2: creating caches must start no goroutine"))
	}
	for _, mc := range caches {
		if stateOf(t, mc).IsSweeping {
			t.Error(test.DiffMessage(true, false, "M2: a new cache has no timer armed"))
		}
	}
	runtime.KeepAlive(caches)
}

func TestMemoryCache_ExpiresExactlyAtTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		mustSet(t, mc, "k", []byte("v"), 10*time.Second)

		time.Sleep(10*time.Second - time.Nanosecond)
		if !present(mc, "k") {
			t.Error(test.DiffMessage(false, true, "M3: one nanosecond before the ttl the entry is live"))
		}
		if d, ok := mc.TTL(background, "k"); !ok || d != time.Nanosecond {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "1ns true", "M3: remaining ttl one nanosecond before expiry"))
		}

		time.Sleep(time.Nanosecond)
		if present(mc, "k") {
			t.Error(test.DiffMessage(true, false, "M3: at now - t == ttl the entry is expired"))
		}
		if d, ok := mc.TTL(background, "k"); ok || d != 0 {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "0s false", "M3: TTL of an expired entry"))
		}
	})
}

func TestMemoryCache_ZeroTTLNeverExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		mustSet(t, mc, "k", []byte("v"), 0)
		time.Sleep(24 * 365 * time.Hour)
		if !present(mc, "k") {
			t.Error(test.DiffMessage(false, true, "M3: ttl 0 never expires"))
		}
	})
}

func TestMemoryCache_NegativeTTLRejected(t *testing.T) {
	mc := NewMemoryCache(nil)
	for _, ttl := range []time.Duration{-1, -time.Second, math.MinInt64} {
		if err := mc.Set(background, "s", []byte("v"), ttl); !errors.Is(err, ErrNegativeTTL) {
			t.Error(test.DiffMessage(err, ErrNegativeTTL, "M3: Set with negative ttl"))
		}
		if ok, err := mc.SetNX(background, "n", []byte("v"), ttl); ok || !errors.Is(err, ErrNegativeTTL) {
			t.Error(test.DiffMessage(fmt.Sprint(ok, err), "false "+ErrNegativeTTL.Error(), "M3: SetNX with negative ttl"))
		}
		if v, err := mc.Incr(background, "i", 1, ttl); v != 0 || !errors.Is(err, ErrNegativeTTL) {
			t.Error(test.DiffMessage(fmt.Sprint(v, err), "0 "+ErrNegativeTTL.Error(), "M3: Incr with negative ttl"))
		}
	}
	if keys := mc.Keys(background); len(keys) != 0 {
		t.Error(test.DiffMessage(keys, []string{}, "M3: a negative ttl writes nothing"))
	}
}

func TestMemoryCache_HugeTTLNeverExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		time.Sleep(time.Hour)
		mustSet(t, mc, "k", []byte("v"), math.MaxInt64)
		if _, err := mc.Incr(background, "c", 1, math.MaxInt64); err != nil {
			t.Fatal(test.DiffMessage(err, nil, "M3: Incr with a huge ttl"))
		}
		time.Sleep(24 * 365 * 100 * time.Hour)
		if !present(mc, "k") || !present(mc, "c") {
			t.Error(test.DiffMessage(false, true, "M3: a ttl whose deadline overflows never expires"))
		}
		if _, ok := mc.TTL(background, "k"); !ok {
			t.Error(test.DiffMessage(ok, true, "M3: TTL of a huge ttl entry is live"))
		}
	})
}

func TestMemoryCache_ExpiredEntryAbsentWithoutSweep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		for _, k := range []string{"get", "ttl", "keys", "nx", "incr"} {
			mustSet(t, mc, k, []byte("old-value"), time.Second)
		}
		time.Sleep(time.Second)

		if v, ok := mc.Get(background, "get"); ok || v != nil {
			t.Error(test.DiffMessage(fmt.Sprint(len(v), ok), "0 false", "M4: Get misses on an expired entry"))
		}
		if d, ok := mc.TTL(background, "ttl"); ok || d != 0 {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "0s false", "M4: TTL of an expired entry"))
		}
		keys := mc.Keys(background)
		for _, k := range []string{"get", "ttl", "keys", "nx", "incr"} {
			if slices.Contains(keys, k) {
				t.Error(test.DiffMessage(keys, "without "+k, "M4: Keys omits expired entries"))
			}
		}
		if ok, err := mc.SetNX(background, "nx", []byte("new"), 0); !ok || err != nil {
			t.Error(test.DiffMessage(fmt.Sprint(ok, err), "true <nil>", "M4: SetNX treats an expired key as new"))
		}
		if v, err := mc.Incr(background, "incr", 7, 0); v != 7 || err != nil {
			t.Error(test.DiffMessage(fmt.Sprint(v, err), "7 <nil>", "M4: Incr treats an expired key as new, even holding a non-counter"))
		}
	})
}

func TestMemoryCache_GetSet(t *testing.T) {
	mc := NewMemoryCache(nil)
	mustSet(t, mc, "key", []byte("val"), 0)
	got, ok := mc.Get(background, "key")
	if !ok {
		t.Error(test.DiffMessage(ok, true, "M5: Get after Set: ok"))
	}
	if string(got) != "val" {
		t.Error(test.DiffMessage(string(got), "val", "M5: Get after Set: value"))
	}
}

func TestMemoryCache_GetMiss(t *testing.T) {
	mc := NewMemoryCache(nil)
	got, ok := mc.Get(background, "missing")
	if ok || got != nil {
		t.Error(test.DiffMessage(fmt.Sprint(len(got), ok), "0 false", "M5: Get miss returns (nil, false)"))
	}
}

func TestMemoryCache_EmptyKey(t *testing.T) {
	mc := NewMemoryCache(nil)
	if v, ok := mc.Get(background, ""); ok || v != nil {
		t.Error(test.DiffMessage(fmt.Sprint(len(v), ok), "0 false", "M5: Get empty key"))
	}
	if err := mc.Set(background, "", nil, 0); !errors.Is(err, ErrEmptyKey) {
		t.Error(test.DiffMessage(err, ErrEmptyKey, "M6: Set empty key"))
	}
	if ok, err := mc.SetNX(background, "", nil, 0); ok || !errors.Is(err, ErrEmptyKey) {
		t.Error(test.DiffMessage(fmt.Sprint(ok, err), "false "+ErrEmptyKey.Error(), "M7: SetNX empty key"))
	}
	if v, err := mc.Incr(background, "", 1, 0); v != 0 || !errors.Is(err, ErrEmptyKey) {
		t.Error(test.DiffMessage(fmt.Sprint(v, err), "0 "+ErrEmptyKey.Error(), "M8: Incr empty key"))
	}
	if err := mc.Delete(background, ""); !errors.Is(err, ErrEmptyKey) {
		t.Error(test.DiffMessage(err, ErrEmptyKey, "M9: Delete empty key"))
	}
	if d, ok := mc.TTL(background, ""); ok || d != 0 {
		t.Error(test.DiffMessage(fmt.Sprint(d, ok), "0s false", "M10: TTL empty key"))
	}
}

func TestMemoryCache_NilValue(t *testing.T) {
	mc := NewMemoryCache(nil)
	mustSet(t, mc, "nil", nil, 0)
	mustSet(t, mc, "empty", []byte{}, 0)
	for _, k := range []string{"nil", "empty"} {
		got, ok := mc.Get(background, k)
		if !ok {
			t.Error(test.DiffMessage(ok, true, "M5: "+k+" value is live"))
		}
		if got == nil || len(got) != 0 {
			t.Error(test.DiffMessage(fmt.Sprintf("%#v", got), "[]byte{}", "M5: an empty value reads back as an empty non-nil slice ("+k+")"))
		}
	}
}

func TestMemoryCache_CopySemantics_Set(t *testing.T) {
	mc := NewMemoryCache(nil)
	val := []byte("original")
	mustSet(t, mc, "k", val, 0)
	copy(val, "MUTATED!")
	got, _ := mc.Get(background, "k")
	if string(got) != "original" {
		t.Error(test.DiffMessage(string(got), "original", "M5: changing the slice passed to Set must not change the stored value"))
	}
}

func TestMemoryCache_CopySemantics_Get(t *testing.T) {
	mc := NewMemoryCache(nil)
	mustSet(t, mc, "k", []byte("original"), 0)
	got, _ := mc.Get(background, "k")
	copy(got, "MUTATED!")
	again, _ := mc.Get(background, "k")
	if string(again) != "original" {
		t.Error(test.DiffMessage(string(again), "original", "M5: changing the slice from Get must not change the stored value"))
	}
}

func TestMemoryCache_CopySemantics_AfterOverwrite(t *testing.T) {
	mc := NewMemoryCache(nil)
	mustSet(t, mc, "k", []byte("aaaaaaaa"), 0)
	first, _ := mc.Get(background, "k")
	mustSet(t, mc, "k", []byte("bbbb"), 0)
	if string(first) != "aaaaaaaa" {
		t.Error(test.DiffMessage(string(first), "aaaaaaaa", "M5: a slice from Get is unchanged by a later shorter Set"))
	}
	second, _ := mc.Get(background, "k")
	mustSet(t, mc, "k", []byte("cccc"), 0)
	if string(second) != "bbbb" {
		t.Error(test.DiffMessage(string(second), "bbbb", "M5: a slice from Get is unchanged by a later same-size Set"))
	}

	mustSet(t, mc, "c", be(5), 0)
	snap, _ := mc.Get(background, "c")
	if _, err := mc.Incr(background, "c", 10, 0); err != nil {
		t.Fatal(err)
	}
	if fromBE(snap) != 5 {
		t.Error(test.DiffMessage(fromBE(snap), 5, "M5: a slice from Get is unchanged by a later Incr"))
	}
}

func TestMemoryCache_Set_ReplacesExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		mustSet(t, mc, "k", []byte("a"), 10*time.Second)
		mustSet(t, mc, "k", []byte("b"), 0)
		if d, ok := mc.TTL(background, "k"); !ok || d != 0 {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "0s true", "M6: Set with ttl 0 removes the expiry"))
		}
		time.Sleep(time.Minute)
		if !present(mc, "k") {
			t.Error(test.DiffMessage(false, true, "M6: the old expiry no longer applies"))
		}

		mustSet(t, mc, "k", []byte("c"), 5*time.Second)
		time.Sleep(2 * time.Second)
		mustSet(t, mc, "k", []byte("d"), 30*time.Second)
		if d, _ := mc.TTL(background, "k"); d != 30*time.Second {
			t.Error(test.DiffMessage(d, 30*time.Second, "M6: Set replaces the expiry with a new ttl"))
		}
	})
}

func TestMemoryCache_Set_ErrorOrder(t *testing.T) {
	mc := newCache(0, 64<<10)
	big := make([]byte, 2000)
	mustSet(t, mc, "keep", []byte("old"), 0)

	if err := mc.Set(background, "", big, -1); !errors.Is(err, ErrEmptyKey) {
		t.Error(test.DiffMessage(err, ErrEmptyKey, "M6: empty key is reported first"))
	}
	if err := mc.Set(background, "k", big, -1); !errors.Is(err, ErrNegativeTTL) {
		t.Error(test.DiffMessage(err, ErrNegativeTTL, "M6: negative ttl is reported before the size"))
	}
	if err := mc.Set(background, "k", big, 0); !errors.Is(err, ErrEntryTooLarge) {
		t.Error(test.DiffMessage(err, ErrEntryTooLarge, "M6: too large is reported last"))
	}
	if err := mc.Set(background, "keep", big, 0); !errors.Is(err, ErrEntryTooLarge) {
		t.Error(test.DiffMessage(err, ErrEntryTooLarge, "M6: too large on an existing key"))
	}
	if present(mc, "k") {
		t.Error(test.DiffMessage(true, false, "M6: a refused Set writes nothing"))
	}
	if got, _ := mc.Get(background, "keep"); string(got) != "old" {
		t.Error(test.DiffMessage(string(got), "old", "M6: a refused Set leaves the existing value"))
	}
}

func TestMemoryCache_Set_EntryTooLarge(t *testing.T) {
	mc := newCache(0, 64<<10)
	limit := (64 << 10) / 64

	if err := mc.Set(background, "k", make([]byte, limit-1), 0); err != nil {
		t.Error(test.DiffMessage(err, nil, "M6: len(key)+len(val) == MaxBytes/64 is accepted"))
	}
	if err := mc.Set(background, "k", make([]byte, limit), 0); !errors.Is(err, ErrEntryTooLarge) {
		t.Error(test.DiffMessage(err, ErrEntryTooLarge, "M6: len(key)+len(val) == MaxBytes/64 + 1 is refused"))
	}
	if err := mc.Set(background, strings.Repeat("k", limit+1), nil, 0); !errors.Is(err, ErrEntryTooLarge) {
		t.Error(test.DiffMessage(err, ErrEntryTooLarge, "M6: a huge key counts"))
	}
	if ok, err := mc.SetNX(background, "nx", make([]byte, limit), 0); ok || !errors.Is(err, ErrEntryTooLarge) {
		t.Error(test.DiffMessage(fmt.Sprint(ok, err), "false "+ErrEntryTooLarge.Error(), "M7: SetNX too large"))
	}
	if _, err := mc.Incr(background, strings.Repeat("k", limit-7), 1, 0); !errors.Is(err, ErrEntryTooLarge) {
		t.Error(test.DiffMessage(err, ErrEntryTooLarge, "M8: Incr whose key plus 8 bytes is too large"))
	}
	if present(mc, "nx") {
		t.Error(test.DiffMessage(true, false, "M7: a refused SetNX writes nothing"))
	}

	def := NewMemoryCache(nil)
	if err := def.Set(background, "k", make([]byte, 256<<10-1), 0); err != nil {
		t.Error(test.DiffMessage(err, nil, "M6: the default largest entry is 256 KiB"))
	}
	if err := def.Set(background, "k", make([]byte, 256<<10), 0); !errors.Is(err, ErrEntryTooLarge) {
		t.Error(test.DiffMessage(err, ErrEntryTooLarge, "M6: one byte over 256 KiB"))
	}
}

func TestMemoryCache_Set_FullCacheReturnsNil(t *testing.T) {
	mc := newCache(64, 0)
	for i := 0; i < 5000; i++ {
		if err := mc.Set(background, "key-"+strconv.Itoa(i), []byte("v"), time.Hour); err != nil {
			t.Fatal(test.DiffMessage(err, nil, "M6: Set on a full cache never fails (insert "+strconv.Itoa(i)+")"))
		}
		if ok, err := mc.SetNX(background, "nx-"+strconv.Itoa(i), []byte("v"), 0); err != nil {
			t.Fatal(test.DiffMessage(fmt.Sprint(ok, err), "<nil>", "M6: SetNX on a full cache never fails"))
		}
	}
	if st := stateOf(t, mc); st.Entries > 64 {
		t.Error(test.DiffMessage(st.Entries, 64, "M11: entries stay within the bound"))
	}
}

func TestMemoryCache_SetNX_New(t *testing.T) {
	mc := NewMemoryCache(nil)
	ok, err := mc.SetNX(background, "k", []byte("v"), time.Minute)
	if !ok || err != nil {
		t.Fatal(test.DiffMessage(fmt.Sprint(ok, err), "true <nil>", "M7: SetNX on a new key"))
	}
	if got, _ := mc.Get(background, "k"); string(got) != "v" {
		t.Error(test.DiffMessage(string(got), "v", "M7: SetNX stores the value"))
	}
	if d, ok := mc.TTL(background, "k"); !ok || d <= 0 || d > time.Minute {
		t.Error(test.DiffMessage(fmt.Sprint(d, ok), "(0, 1m] true", "M7: SetNX stores the ttl"))
	}
}

func TestMemoryCache_SetNX_Exists(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		mustSet(t, mc, "k", []byte("first"), 10*time.Second)
		time.Sleep(3 * time.Second)

		ok, err := mc.SetNX(background, "k", []byte("second"), time.Hour)
		if ok || err != nil {
			t.Error(test.DiffMessage(fmt.Sprint(ok, err), "false <nil>", "M7: SetNX on a live key"))
		}
		if got, _ := mc.Get(background, "k"); string(got) != "first" {
			t.Error(test.DiffMessage(string(got), "first", "M7: SetNX keeps the value"))
		}
		if d, _ := mc.TTL(background, "k"); d != 7*time.Second {
			t.Error(test.DiffMessage(d, 7*time.Second, "M7: SetNX keeps the expiry"))
		}
	})
}

func TestMemoryCache_SetNX_AfterExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		mustSet(t, mc, "k", []byte("old"), time.Second)
		time.Sleep(time.Second)
		ok, err := mc.SetNX(background, "k", []byte("new"), 0)
		if !ok || err != nil {
			t.Fatal(test.DiffMessage(fmt.Sprint(ok, err), "true <nil>", "M7: SetNX after expiry"))
		}
		if got, _ := mc.Get(background, "k"); string(got) != "new" {
			t.Error(test.DiffMessage(string(got), "new", "M7: SetNX after expiry stores the value"))
		}
	})
}

func TestMemoryCache_SetNX_ConcurrentOneWinner(t *testing.T) {
	const workers = 12
	for round := 0; round < 50; round++ {
		mc := NewMemoryCache(nil)
		key := "contended-" + strconv.Itoa(round)
		var wins atomic.Int64
		var start sync.WaitGroup
		start.Add(1)
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Go(func() {
				start.Wait()
				ok, err := mc.SetNX(background, key, []byte{byte(w)}, 0)
				if err != nil {
					t.Error(test.DiffMessage(err, nil, "M7: SetNX error"))
				}
				if ok {
					wins.Add(1)
				}
			})
		}
		start.Done()
		wg.Wait()
		if wins.Load() != 1 {
			t.Fatal(test.DiffMessage(wins.Load(), 1, "M7: concurrent SetNX of one key returns true exactly once"))
		}
	}
}

func TestMemoryCache_Incr_Creates(t *testing.T) {
	mc := NewMemoryCache(nil)
	v, err := mc.Incr(background, "c", 5, 0)
	if v != 5 || err != nil {
		t.Fatal(test.DiffMessage(fmt.Sprint(v, err), "5 <nil>", "M8: Incr on a missing key returns delta"))
	}
	got, ok := mc.Get(background, "c")
	if !ok || len(got) != 8 || fromBE(got) != 5 {
		t.Error(test.DiffMessage(got, be(5), "M8: the counter is 8 bytes, big-endian"))
	}
}

func TestMemoryCache_Incr_CreatesWithTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		if _, err := mc.Incr(background, "c", 1, 4*time.Second); err != nil {
			t.Fatal(err)
		}
		if d, ok := mc.TTL(background, "c"); !ok || d != 4*time.Second {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "4s true", "M8: a created counter takes ttl"))
		}
		time.Sleep(4 * time.Second)
		if present(mc, "c") {
			t.Error(test.DiffMessage(true, false, "M8: the counter expires at its ttl"))
		}
	})
}

func TestMemoryCache_Incr_Adds(t *testing.T) {
	mc := NewMemoryCache(nil)
	for i := int64(1); i <= 10; i++ {
		v, err := mc.Incr(background, "c", 3, 0)
		if err != nil || v != 3*i {
			t.Fatal(test.DiffMessage(fmt.Sprint(v, err), fmt.Sprint(3*i, " <nil>"), "M8: Incr adds delta"))
		}
	}
}

func TestMemoryCache_Incr_KeepsExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		if _, err := mc.Incr(background, "c", 1, 10*time.Second); err != nil {
			t.Fatal(err)
		}
		time.Sleep(4 * time.Second)
		v, err := mc.Incr(background, "c", 1, time.Hour)
		if v != 2 || err != nil {
			t.Fatal(test.DiffMessage(fmt.Sprint(v, err), "2 <nil>", "M8: Incr on a live counter"))
		}
		if d, _ := mc.TTL(background, "c"); d != 6*time.Second {
			t.Error(test.DiffMessage(d, 6*time.Second, "M8: Incr keeps the expiry and ignores ttl"))
		}

		mustSet(t, mc, "forever", be(1), 0)
		if _, err := mc.Incr(background, "forever", 1, time.Second); err != nil {
			t.Fatal(err)
		}
		if d, ok := mc.TTL(background, "forever"); !ok || d != 0 {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "0s true", "M8: a counter without expiry stays without one"))
		}
	})
}

func TestMemoryCache_Incr_AfterExpiryStartsOver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		for i := 0; i < 3; i++ {
			if _, err := mc.Incr(background, "c", 10, 2*time.Second); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(2 * time.Second)
		v, err := mc.Incr(background, "c", 1, 8*time.Second)
		if v != 1 || err != nil {
			t.Fatal(test.DiffMessage(fmt.Sprint(v, err), "1 <nil>", "M8: Incr after expiry starts over"))
		}
		if d, _ := mc.TTL(background, "c"); d != 8*time.Second {
			t.Error(test.DiffMessage(d, 8*time.Second, "M8: the new counter takes the new ttl"))
		}
	})
}

func TestMemoryCache_Incr_NotCounter(t *testing.T) {
	mc := NewMemoryCache(nil)
	for _, val := range [][]byte{[]byte("abc"), {}, make([]byte, 7), make([]byte, 9)} {
		mustSet(t, mc, "k", val, 0)
		v, err := mc.Incr(background, "k", 1, 0)
		if v != 0 || !errors.Is(err, ErrNotCounter) {
			t.Error(test.DiffMessage(fmt.Sprint(v, err), "0 "+ErrNotCounter.Error(), fmt.Sprintf("M8: Incr on a %d-byte value", len(val))))
		}
		if got, _ := mc.Get(background, "k"); len(got) != len(val) {
			t.Error(test.DiffMessage(got, val, "M8: a refused Incr changes nothing"))
		}
	}
}

func TestMemoryCache_Incr_NegativeAndZeroDelta(t *testing.T) {
	mc := NewMemoryCache(nil)
	if v, err := mc.Incr(background, "z", 0, 0); v != 0 || err != nil {
		t.Error(test.DiffMessage(fmt.Sprint(v, err), "0 <nil>", "M8: zero delta creates a counter"))
	}
	if got, ok := mc.Get(background, "z"); !ok || len(got) != 8 {
		t.Error(test.DiffMessage(got, be(0), "M8: a zero counter exists"))
	}
	if v, _ := mc.Incr(background, "n", -5, 0); v != -5 {
		t.Error(test.DiffMessage(v, -5, "M8: negative delta creates"))
	}
	if v, _ := mc.Incr(background, "n", -5, 0); v != -10 {
		t.Error(test.DiffMessage(v, -10, "M8: negative delta adds"))
	}
	if v, _ := mc.Incr(background, "n", 0, 0); v != -10 {
		t.Error(test.DiffMessage(v, -10, "M8: zero delta reads"))
	}
	if got, _ := mc.Get(background, "n"); fromBE(got) != -10 {
		t.Error(test.DiffMessage(fromBE(got), -10, "M8: two's complement"))
	}
}

func TestMemoryCache_Incr_Wraps(t *testing.T) {
	mc := NewMemoryCache(nil)
	if v, _ := mc.Incr(background, "c", math.MaxInt64, 0); v != math.MaxInt64 {
		t.Fatal(test.DiffMessage(v, int64(math.MaxInt64), "M8: MaxInt64"))
	}
	if v, _ := mc.Incr(background, "c", 1, 0); v != math.MinInt64 {
		t.Error(test.DiffMessage(v, int64(math.MinInt64), "M8: overflow wraps like int64"))
	}
	if v, _ := mc.Incr(background, "c", -1, 0); v != math.MaxInt64 {
		t.Error(test.DiffMessage(v, int64(math.MaxInt64), "M8: underflow wraps like int64"))
	}
}

func TestMemoryCache_Incr_ReadableByGet(t *testing.T) {
	mc := NewMemoryCache(nil)
	mustSet(t, mc, "c", be(41), 0)
	if v, err := mc.Incr(background, "c", 1, 0); v != 42 || err != nil {
		t.Error(test.DiffMessage(fmt.Sprint(v, err), "42 <nil>", "M8: Set with 8 bytes writes a counter"))
	}
	got, ok := mc.Get(background, "c")
	if !ok || !slices.Equal(got, be(42)) {
		t.Error(test.DiffMessage(got, be(42), "M8: Get on a counter returns its 8 bytes"))
	}
}

func TestMemoryCache_Incr_ConcurrentExact(t *testing.T) {
	const workers, each = 12, 1000
	mc := NewMemoryCache(nil)
	results := make([][]int64, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Go(func() {
			out := make([]int64, 0, each)
			for i := 0; i < each; i++ {
				v, err := mc.Incr(background, "hot", 1, time.Hour)
				if err != nil {
					t.Error(test.DiffMessage(err, nil, "M8: concurrent Incr error"))
					return
				}
				out = append(out, v)
			}
			results[w] = out
		})
	}
	wg.Wait()

	seen := make([]bool, workers*each+1)
	for _, out := range results {
		for _, v := range out {
			if v < 1 || v > workers*each || seen[v] {
				t.Fatal(test.DiffMessage(v, "a value of 1..12000 returned once", "M8: concurrent Incr returns each of 1..N exactly once"))
			}
			seen[v] = true
		}
	}
	got, _ := mc.Get(background, "hot")
	if fromBE(got) != workers*each {
		t.Error(test.DiffMessage(fromBE(got), workers*each, "M8: final value of the counter"))
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	mc := NewMemoryCache(nil)
	mustSet(t, mc, "k", []byte("v"), 0)
	if err := mc.Delete(background, "k"); err != nil {
		t.Error(test.DiffMessage(err, nil, "M9: Delete"))
	}
	if present(mc, "k") {
		t.Error(test.DiffMessage(true, false, "M9: Delete removes the entry"))
	}
	if _, ok := mc.TTL(background, "k"); ok {
		t.Error(test.DiffMessage(true, false, "M9: TTL after Delete"))
	}
}

func TestMemoryCache_DeleteMissing(t *testing.T) {
	mc := NewMemoryCache(nil)
	if err := mc.Delete(background, "never-set"); err != nil {
		t.Error(test.DiffMessage(err, nil, "M9: deleting a missing key is not an error"))
	}
}

func TestMemoryCache_TTLWithExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		mustSet(t, mc, "k", []byte("v"), 10*time.Second)
		if d, ok := mc.TTL(background, "k"); !ok || d != 10*time.Second {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "10s true", "M10: TTL right after Set"))
		}
		time.Sleep(3 * time.Second)
		if d, ok := mc.TTL(background, "k"); !ok || d != 7*time.Second {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "7s true", "M10: remaining ttl"))
		}
	})
}

func TestMemoryCache_TTLNoExpiry(t *testing.T) {
	mc := NewMemoryCache(nil)
	mustSet(t, mc, "k", []byte("v"), 0)
	if d, ok := mc.TTL(background, "k"); !ok || d != 0 {
		t.Error(test.DiffMessage(fmt.Sprint(d, ok), "0s true", "M10: TTL of an entry without expiry"))
	}
}

func TestMemoryCache_TTLMissing(t *testing.T) {
	mc := NewMemoryCache(nil)
	if d, ok := mc.TTL(background, "missing"); ok || d != 0 {
		t.Error(test.DiffMessage(fmt.Sprint(d, ok), "0s false", "M10: TTL of a missing key"))
	}
}

func TestMemoryCache_Keys(t *testing.T) {
	mc := NewMemoryCache(nil)
	want := []string{}
	for i := 0; i < 100; i++ {
		k := "key-" + strconv.Itoa(i)
		want = append(want, k)
		mustSet(t, mc, k, []byte("v"), time.Hour)
	}
	got := mc.Keys(background)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(len(got), len(want), "M10: Keys returns every live key"))
	}
}

func TestMemoryCache_KeysNeverNil(t *testing.T) {
	mc := NewMemoryCache(nil)
	if keys := mc.Keys(background); keys == nil || len(keys) != 0 {
		t.Error(test.DiffMessage(fmt.Sprintf("%#v", keys), "[]string{}", "M10: Keys of an empty cache is not nil"))
	}
}

func TestMemoryCache_NeverExceedsMaxEntries(t *testing.T) {
	total := 2_000_000
	if testing.Short() {
		total = 200_000
	}
	const workers = 12
	mc := NewMemoryCache(nil)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Go(func() {
			val := []byte("value")
			for i := 0; i < total/workers; i++ {
				_ = mc.Set(background, "tenant:"+strconv.Itoa(w)+":client:"+strconv.Itoa(i), val, 0)
				if i%20000 == 0 {
					if st, _ := inspect.CacheStateOf(mc); st.Entries > 65536 {
						t.Error(test.DiffMessage(st.Entries, 65536, "M11: entries never exceed MaxEntries at any instant"))
						return
					}
				}
			}
		})
	}
	wg.Wait()

	st := stateOf(t, mc)
	if st.Entries > 65536 || st.Entries == 0 {
		t.Error(test.DiffMessage(st.Entries, "1..65536", "M11: entries after distinct keys"))
	}
	if n := len(mc.Keys(background)); n > 65536 {
		t.Error(test.DiffMessage(n, 65536, "M11: Keys never exceeds MaxEntries"))
	}
	if st.Bytes > st.MaxBytes {
		t.Error(test.DiffMessage(st.Bytes, st.MaxBytes, "M11: bytes within MaxBytes"))
	}
	if st.Evictions == 0 {
		t.Error(test.DiffMessage(st.Evictions, "> 0", "M11: a flood of distinct keys evicts"))
	}
}

func TestMemoryCache_NeverExceedsMaxBytes(t *testing.T) {
	const maxBytes = 64 << 10
	mc := newCache(0, maxBytes)
	val := make([]byte, 180)
	for i := 0; i < 300_000; i++ {
		key := "session:" + strconv.Itoa(i)
		if err := mc.Set(background, key, val[:i%len(val)], 0); err != nil {
			t.Fatal(test.DiffMessage(err, nil, "M11: Set within the entry limit"))
		}
		if i%1000 == 0 {
			if st := stateOf(t, mc); st.Bytes > maxBytes {
				t.Fatal(test.DiffMessage(st.Bytes, int64(maxBytes), "M11: bytes never exceed MaxBytes at any instant"))
			}
		}
	}
	st := stateOf(t, mc)
	if st.Bytes > maxBytes || st.Bytes == 0 {
		t.Error(test.DiffMessage(st.Bytes, "1..65536", "M11: bytes after a flood"))
	}
	if st.Entries >= 65536 {
		t.Error(test.DiffMessage(st.Entries, "bytes bound reached first", "M11: the byte bound evicts before the entry bound"))
	}
}

func TestMemoryCache_UpdateAtBoundEvictsNothing(t *testing.T) {
	mc := newCache(64*shardCapacity, 0)
	keys := shardKeys(5, shardCapacity, "u")
	for _, k := range keys {
		mustSet(t, mc, k, []byte("value-1"), 0)
	}
	for round := 0; round < 3; round++ {
		for _, k := range keys {
			mustSet(t, mc, k, []byte("value-2"), 0)
		}
	}
	if _, err := mc.Incr(background, shardKeys(6, 1, "n")[0], 0, 0); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if !present(mc, k) {
			t.Error(test.DiffMessage(false, true, "M11: rewriting a key of a full shard evicts nothing ("+k+")"))
		}
	}
	if st := stateOf(t, mc); st.Evictions != 0 {
		t.Error(test.DiffMessage(st.Evictions, 0, "M11: no eviction for updates at the entry bound"))
	}

	bytesMC := newCache(0, 64<<10)
	byteKeys := shardKeys(9, 4, "b")
	for _, k := range byteKeys {
		mustSet(t, bytesMC, k, make([]byte, 256-len(k)), 0)
	}
	for _, k := range byteKeys {
		mustSet(t, bytesMC, k, make([]byte, 256-len(k)), 0)
		mustSet(t, bytesMC, k, make([]byte, 10), 0)
		mustSet(t, bytesMC, k, make([]byte, 256-len(k)), 0)
	}
	for _, k := range byteKeys {
		if !present(bytesMC, k) {
			t.Error(test.DiffMessage(false, true, "M11: rewriting without growth evicts nothing at the byte bound ("+k+")"))
		}
	}
	if st := stateOf(t, bytesMC); st.Evictions != 0 || st.Bytes != 1024 {
		t.Error(test.DiffMessage(fmt.Sprint(st.Evictions, st.Bytes), "0 1024", "M11: byte bound updates"))
	}
}

func TestMemoryCache_GrowingValueEvictsOthersNotItself(t *testing.T) {
	mc := newCache(0, 64<<10)
	keys := shardKeys(11, 4, "g")
	for _, k := range keys {
		mustSet(t, mc, k, make([]byte, 200), 0)
	}
	grown := make([]byte, 900-len(keys[0]))
	for i := range grown {
		grown[i] = 'g'
	}
	mustSet(t, mc, keys[0], grown, 0)

	got, ok := mc.Get(background, keys[0])
	if !ok || !slices.Equal(got, grown) {
		t.Error(test.DiffMessage(len(got), len(grown), "M11: the written key is never its own victim"))
	}
	for _, k := range keys[1:] {
		if present(mc, k) {
			t.Error(test.DiffMessage(true, false, "M11: growing a value evicts the others that no longer fit ("+k+")"))
		}
	}
	st := stateOf(t, mc)
	if st.Bytes > 1024 || st.Evictions != 3 {
		t.Error(test.DiffMessage(fmt.Sprint(st.Bytes, st.Evictions), "<=1024 3", "M11: bytes and evictions after growth"))
	}
}

func TestMemoryCache_EvictsExpiredFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := newCache(64*shardCapacity, 0)
		keys := shardKeys(3, shardCapacity+3, "e")
		base, extra := keys[:shardCapacity], keys[shardCapacity:]
		for i, k := range base {
			ttl := time.Duration(0)
			if i < 3 {
				ttl = time.Second
			}
			mustSet(t, mc, k, []byte("v"), ttl)
		}
		for _, k := range base[3:] {
			present(mc, k)
		}
		time.Sleep(time.Second)

		for _, k := range extra {
			mustSet(t, mc, k, []byte("n"), 0)
		}
		for _, k := range base[3:] {
			if !present(mc, k) {
				t.Error(test.DiffMessage(false, true, "M12: the next e inserts evict no live entry ("+k+")"))
			}
		}
		for _, k := range base[:3] {
			if present(mc, k) {
				t.Error(test.DiffMessage(true, false, "M4: the expired key is absent ("+k+")"))
			}
		}
		if st := stateOf(t, mc); st.Evictions != 0 {
			t.Error(test.DiffMessage(st.Evictions, 0, "M12: expired entries are removed first, none is counted as an eviction"))
		}
	})
}

func TestMemoryCache_KeepsReadEntries(t *testing.T) {
	mc := newCache(64*shardCapacity, 0)
	keys := shardKeys(7, shardCapacity+5, "r")
	for _, k := range keys[:shardCapacity] {
		mustSet(t, mc, k, []byte("v"), 0)
	}
	read := keys[:3]
	for _, k := range read {
		present(mc, k)
	}
	for _, k := range keys[shardCapacity:] {
		mustSet(t, mc, k, []byte("n"), 0)
	}
	for _, k := range read {
		if !present(mc, k) {
			t.Error(test.DiffMessage(false, true, "M12: a read entry survives the next n - r inserts ("+k+")"))
		}
	}
	if st := stateOf(t, mc); st.Entries != shardCapacity || st.Evictions != 5 {
		t.Error(test.DiffMessage(fmt.Sprint(st.Entries, st.Evictions), "8 5", "M12: entries and evictions"))
	}
}

func TestMemoryCache_KeepsRewrittenEntries(t *testing.T) {
	mc := newCache(64*shardCapacity, 0)
	keys := shardKeys(13, shardCapacity+5, "w")
	for _, k := range keys[:3] {
		if _, err := mc.Incr(background, k, 1, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range keys[3:shardCapacity] {
		mustSet(t, mc, k, []byte("v"), 0)
	}
	mustSet(t, mc, keys[3], []byte("w"), 0)
	mustSet(t, mc, keys[4], []byte("w"), 0)
	if _, err := mc.Incr(background, keys[0], 1, 0); err != nil {
		t.Fatal(err)
	}
	rewritten := []string{keys[0], keys[3], keys[4]}

	for _, k := range keys[shardCapacity:] {
		mustSet(t, mc, k, []byte("n"), 0)
	}
	for _, k := range rewritten {
		if !present(mc, k) {
			t.Error(test.DiffMessage(false, true, "M12: a rewritten entry survives the next n - r inserts ("+k+")"))
		}
	}
}

func TestMemoryCache_NewEntriesAreFirstVictims(t *testing.T) {
	mc := newCache(64*shardCapacity, 0)
	keys := shardKeys(17, shardCapacity+2, "f")
	old, fresh := keys[:shardCapacity], keys[shardCapacity:]
	for _, k := range old {
		mustSet(t, mc, k, []byte("v"), 0)
	}
	for _, k := range old {
		present(mc, k)
	}
	mustSet(t, mc, fresh[0], []byte("n"), 0)

	survivors := []string{}
	for _, k := range old {
		if present(mc, k) {
			survivors = append(survivors, k)
		}
	}
	if len(survivors) != shardCapacity-1 {
		t.Fatal(test.DiffMessage(len(survivors), shardCapacity-1, "M12: one insert evicts one entry"))
	}
	for _, k := range survivors {
		present(mc, k)
	}
	mustSet(t, mc, fresh[1], []byte("n"), 0)

	if present(mc, fresh[0]) {
		t.Error(test.DiffMessage(true, false, "M12: an entry never read since it was created is the first victim"))
	}
	for _, k := range survivors {
		if !present(mc, k) {
			t.Error(test.DiffMessage(false, true, "M12: read entries survive ("+k+")"))
		}
	}
	if !present(mc, fresh[1]) {
		t.Error(test.DiffMessage(false, true, "M11: the written key is never its own victim"))
	}
}

func TestMemoryCache_SweepNotArmedWithoutTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		for i := 0; i < 100; i++ {
			mustSet(t, mc, "k"+strconv.Itoa(i), []byte("v"), 0)
		}
		if _, err := mc.Incr(background, "c", 1, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := mc.SetNX(background, "nx", []byte("v"), 0); err != nil {
			t.Fatal(err)
		}
		if stateOf(t, mc).IsSweeping {
			t.Error(test.DiffMessage(true, false, "M13: entries without expiry never arm the timer"))
		}
		time.Sleep(time.Hour)
		if stateOf(t, mc).IsSweeping {
			t.Error(test.DiffMessage(true, false, "M13: still not armed after an hour"))
		}
	})
}

func TestMemoryCache_SweepArmsOnFirstTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for name, write := range map[string]func(mc *MemoryCache){
			"Set":   func(mc *MemoryCache) { _ = mc.Set(background, "k", []byte("v"), time.Hour) },
			"SetNX": func(mc *MemoryCache) { _, _ = mc.SetNX(background, "k", []byte("v"), time.Hour) },
			"Incr":  func(mc *MemoryCache) { _, _ = mc.Incr(background, "k", 1, time.Hour) },
		} {
			mc := NewMemoryCache(nil)
			if stateOf(t, mc).IsSweeping {
				t.Error(test.DiffMessage(true, false, "M13: not armed before any write"))
			}
			write(mc)
			if !stateOf(t, mc).IsSweeping {
				t.Error(test.DiffMessage(false, true, "M13: the first entry with an expiry arms the timer ("+name+")"))
			}
		}
	})
}

func TestMemoryCache_SweepRemovesExpired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		for i := 0; i < 500; i++ {
			mustSet(t, mc, "k"+strconv.Itoa(i), make([]byte, 100), time.Second)
		}
		time.Sleep(time.Second + sweepEvery)
		synctest.Wait()
		st := stateOf(t, mc)
		if st.Entries != 0 || st.Bytes != 0 {
			t.Error(test.DiffMessage(fmt.Sprint(st.Entries, st.Bytes), "0 0", "M13: at most d + 5s later no expiring entry remains"))
		}
		if st.Expirations != 500 {
			t.Error(test.DiffMessage(st.Expirations, uint64(500), "M16: the sweep counts every expired entry it removes"))
		}
		time.Sleep(sweepEvery)
		synctest.Wait()
		if stateOf(t, mc).IsSweeping {
			t.Error(test.DiffMessage(true, false, "M13: at most 5s after that the sweep is not running"))
		}
	})
}

func TestMemoryCache_SweepStopsWhenNoTTLLeft(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		mustSet(t, mc, "plain", []byte("v"), 0)
		mustSet(t, mc, "ttl", []byte("v"), time.Hour)
		if err := mc.Delete(background, "ttl"); err != nil {
			t.Fatal(err)
		}
		if !stateOf(t, mc).IsSweeping {
			t.Error(test.DiffMessage(false, true, "M13: deleting the last expiring entry stops the sweep at its next pass, not at once"))
		}
		time.Sleep(2 * sweepEvery)
		synctest.Wait()
		if stateOf(t, mc).IsSweeping {
			t.Error(test.DiffMessage(true, false, "M13: the timer is not re-armed once no expiring entry is left"))
		}
		if !present(mc, "plain") {
			t.Error(test.DiffMessage(false, true, "M13: entries without expiry stay"))
		}
	})
}

func TestMemoryCache_SweepRearmsAfterStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		mustSet(t, mc, "a", []byte("v"), time.Second)
		time.Sleep(3 * sweepEvery)
		synctest.Wait()
		if stateOf(t, mc).IsSweeping {
			t.Fatal(test.DiffMessage(true, false, "M13: the sweep has stopped"))
		}

		mustSet(t, mc, "b", []byte("v"), time.Second)
		if !stateOf(t, mc).IsSweeping {
			t.Error(test.DiffMessage(false, true, "M13: a new entry with an expiry arms the timer again"))
		}
		time.Sleep(time.Second + sweepEvery)
		synctest.Wait()
		if st := stateOf(t, mc); st.Entries != 0 {
			t.Error(test.DiffMessage(st.Entries, 0, "M13: the re-armed sweep removes the entry"))
		}
	})
}

func TestMemoryCache_SweepReleasesEmptyShard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		keys := shardKeys(21, 200, "s")
		for _, k := range keys {
			mustSet(t, mc, k, make([]byte, 50), time.Second)
		}
		time.Sleep(time.Second + sweepEvery)
		synctest.Wait()
		if st := stateOf(t, mc); st.Entries != 0 || st.Bytes != 0 {
			t.Error(test.DiffMessage(fmt.Sprint(st.Entries, st.Bytes), "0 0", "M13: a shard left with no entry is empty"))
		}
		mustSet(t, mc, keys[0], []byte("again"), 0)
		if got, ok := mc.Get(background, keys[0]); !ok || string(got) != "again" {
			t.Error(test.DiffMessage(string(got), "again", "M13: a released shard works again on its next write"))
		}
	})
}

func TestMemoryCache_SweepConcurrentWithWrites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(nil)
		var wg sync.WaitGroup
		for g := 0; g < 12; g++ {
			wg.Go(func() {
				for i := 0; i < 200; i++ {
					key := fmt.Sprintf("g%d-%d", g, i%20)
					ttl := time.Duration(1+i%3) * time.Second
					_ = mc.Set(background, key, []byte("v"), ttl)
					_, _ = mc.Incr(background, "c"+key, 1, ttl)
					_, _ = mc.SetNX(background, "nx"+key, []byte("v"), ttl)
					mc.Get(background, key)
					if i%25 == 0 {
						_ = mc.Delete(background, key)
						mc.Keys(background)
					}
					time.Sleep(100 * time.Millisecond)
				}
			})
		}
		wg.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		st := stateOf(t, mc)
		if st.Entries != 0 || st.IsSweeping {
			t.Error(test.DiffMessage(fmt.Sprint(st.Entries, st.IsSweeping), "0 false", "M13: after the writers stop every entry expires and the sweep stops"))
		}
	})
}

func TestMemoryCache_CollectedWithPendingTimer(t *testing.T) {
	collected := make(chan struct{})
	func() {
		mc := NewMemoryCache(nil)
		mustSet(t, mc, "k", []byte("v"), time.Hour)
		if !stateOf(t, mc).IsSweeping {
			t.Fatal(test.DiffMessage(false, true, "M14: the timer is pending"))
		}
		runtime.SetFinalizer(mc, func(*MemoryCache) { close(collected) })
	}()
	for i := 0; i < 200; i++ {
		runtime.GC()
		select {
		case <-collected:
			return
		default:
			runtime.Gosched()
		}
	}
	t.Error(test.DiffMessage("alive", "collected", "M14: a cache nobody references is collected even with a timer pending"))
}

func TestMemoryCache_KeyIsCopied(t *testing.T) {
	mc := NewMemoryCache(nil)
	buf := []byte("session-123")
	key := unsafe.String(&buf[0], len(buf))
	mustSet(t, mc, key, []byte("v"), 0)
	if _, err := mc.Incr(background, unsafe.String(&buf[0], 3), 1, 0); err != nil {
		t.Fatal(err)
	}
	copy(buf, "XXXXXXXXXXX")

	if !present(mc, "session-123") {
		t.Error(test.DiffMessage(false, true, "M15: the entry is found under the original key text"))
	}
	if present(mc, "XXXXXXXXXXX") {
		t.Error(test.DiffMessage(true, false, "M15: the cache must not alias the caller's key bytes"))
	}
	keys := mc.Keys(background)
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"session-123", "ses"}) && !slices.Equal(keys, []string{"ses", "session-123"}) {
		t.Error(test.DiffMessage(keys, []string{"ses", "session-123"}, "M15: Keys returns the copied keys"))
	}
}

func TestMemoryCache_SnapshotCounts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := newCache(64*shardCapacity, 0)
		st := stateOf(t, mc)
		if st != (inspect.CacheState{MaxEntries: 64 * shardCapacity, MaxBytes: 16 << 20}) {
			t.Error(test.DiffMessage(st, "only the bounds set", "M16: snapshot of an empty cache"))
		}

		mustSet(t, mc, "ab", []byte("xyz"), 0)
		mustSet(t, mc, "c", nil, 0)
		if st := stateOf(t, mc); st.Entries != 2 || st.Bytes != 6 {
			t.Error(test.DiffMessage(fmt.Sprint(st.Entries, st.Bytes), "2 6", "M16: Entries and Bytes are len(key)+len(val) sums"))
		}

		mc.Get(background, "ab")
		mc.Get(background, "ab")
		mc.Get(background, "missing")
		mc.Get(background, "")
		if st := stateOf(t, mc); st.Hits != 2 || st.Misses != 2 {
			t.Error(test.DiffMessage(fmt.Sprint(st.Hits, st.Misses), "2 2", "M16: Hits and Misses count Get calls, an empty key is a miss"))
		}
		mc.TTL(background, "ab")
		mc.Keys(background)
		if st := stateOf(t, mc); st.Hits != 2 || st.Misses != 2 {
			t.Error(test.DiffMessage(fmt.Sprint(st.Hits, st.Misses), "2 2", "M16: only Get counts"))
		}

		mustSet(t, mc, "e", []byte("v"), time.Second)
		time.Sleep(time.Second)
		st = stateOf(t, mc)
		if st.Entries != 3 || st.Bytes != 8 {
			t.Error(test.DiffMessage(fmt.Sprint(st.Entries, st.Bytes), "3 8", "M16: expired entries not yet removed are counted"))
		}
		mc.Get(background, "e")
		if st := stateOf(t, mc); st.Misses != 3 {
			t.Error(test.DiffMessage(st.Misses, uint64(3), "M16: an expired entry is a miss"))
		}
		mustSet(t, mc, "e", []byte("w"), 0)
		st = stateOf(t, mc)
		if st.Expirations != 1 || st.Entries != 3 || st.Evictions != 0 {
			t.Error(test.DiffMessage(fmt.Sprint(st.Expirations, st.Entries, st.Evictions), "1 3 0", "M16: an expired entry overwritten in place counts as an expiration"))
		}
	})
}

func TestMemoryCache_SnapshotCountsEvictions(t *testing.T) {
	mc := newCache(64*shardCapacity, 0)
	keys := shardKeys(30, shardCapacity+1, "v")
	for _, k := range keys {
		mustSet(t, mc, k, []byte("v"), 0)
	}
	st := stateOf(t, mc)
	if st.Evictions != 1 || st.Entries != shardCapacity || st.Expirations != 0 {
		t.Error(test.DiffMessage(fmt.Sprint(st.Evictions, st.Entries, st.Expirations), "1 8 0", "M16: Evictions counts live entries removed by the hand"))
	}
}

func TestMemoryCache_SnapshotOfOtherTypeIsFalse(t *testing.T) {
	for _, other := range []any{nil, "cache", 42, struct{}{}, &struct{ X int }{}, (*sync.Mutex)(nil)} {
		st, ok := inspect.CacheStateOf(other)
		if ok || st != (inspect.CacheState{}) {
			t.Error(test.DiffMessage(fmt.Sprint(st, ok), "{} false", fmt.Sprintf("M16: CacheStateOf(%T) is (zero, false)", other)))
		}
	}
}

func TestMemoryCache_ConcurrentMixedNoRace(t *testing.T) {
	mc := newCache(64*16, 0)
	var wg sync.WaitGroup
	for g := 0; g < 12; g++ {
		wg.Go(func() {
			for i := 0; i < 3000; i++ {
				k := "k" + strconv.Itoa((g*7+i)%300)
				c := "c" + strconv.Itoa(i%50)
				ttl := time.Duration(i%3) * time.Millisecond
				switch i % 9 {
				case 0:
					_ = mc.Set(background, k, []byte("value"), ttl)
				case 1:
					mc.Get(background, k)
				case 2:
					_, _ = mc.SetNX(background, k, []byte("value"), ttl)
				case 3:
					_, _ = mc.Incr(background, c, 1, ttl)
				case 4:
					_ = mc.Delete(background, k)
				case 5:
					mc.TTL(background, k)
				case 6:
					if i%90 == 6 {
						mc.Keys(background)
					}
				case 7:
					inspect.CacheStateOf(mc)
				default:
					_ = mc.Set(background, k, make([]byte, 1+i%40), ttl)
				}
			}
		})
	}
	wg.Wait()
	if st := stateOf(t, mc); st.Entries > st.MaxEntries {
		t.Error(test.DiffMessage(st.Entries, st.MaxEntries, "M11: bound holds under mixed concurrent use"))
	}
}
