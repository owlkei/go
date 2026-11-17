package cache

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dangduoc08/ginject/internal/memorycache"
	"github.com/dangduoc08/ginject/internal/test"
)

var background = context.Background()

func newService() CacheService {
	return CacheService{Backend: memorycache.NewMemoryCache(nil)}
}

func be(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

type recordedCall struct {
	method string
	ctx    context.Context
	key    string
	val    []byte
	ttl    time.Duration
	delta  int64
}

type recordingBackend struct {
	calls []recordedCall
}

type ctxKey struct{}

func (r *recordingBackend) Get(ctx context.Context, key string) ([]byte, bool) {
	r.calls = append(r.calls, recordedCall{method: "Get", ctx: ctx, key: key})
	return []byte("get-result"), true
}

func (r *recordingBackend) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	r.calls = append(r.calls, recordedCall{method: "Set", ctx: ctx, key: key, val: val, ttl: ttl})
	return errors.New("set-error")
}

func (r *recordingBackend) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	r.calls = append(r.calls, recordedCall{method: "SetNX", ctx: ctx, key: key, val: val, ttl: ttl})
	return true, errors.New("setnx-error")
}

func (r *recordingBackend) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	r.calls = append(r.calls, recordedCall{method: "Incr", ctx: ctx, key: key, delta: delta, ttl: ttl})
	return 4242, errors.New("incr-error")
}

func (r *recordingBackend) Delete(ctx context.Context, key string) error {
	r.calls = append(r.calls, recordedCall{method: "Delete", ctx: ctx, key: key})
	return errors.New("delete-error")
}

func (r *recordingBackend) Keys(ctx context.Context) []string {
	r.calls = append(r.calls, recordedCall{method: "Keys", ctx: ctx})
	return []string{"keys-result"}
}

func (r *recordingBackend) TTL(ctx context.Context, key string) (time.Duration, bool) {
	r.calls = append(r.calls, recordedCall{method: "TTL", ctx: ctx, key: key})
	return 77 * time.Second, true
}

var _ Cache = (*recordingBackend)(nil)
var _ Cache = (*memorycache.MemoryCache)(nil)

func TestCacheService_ForwardsToBackend(t *testing.T) {
	rec := &recordingBackend{}
	svc := CacheService{Backend: rec}
	ctx := context.WithValue(background, ctxKey{}, "marker")

	if v, ok := svc.Get(ctx, "gk"); string(v) != "get-result" || !ok {
		t.Error(test.DiffMessage(fmt.Sprint(string(v), ok), "get-result true", "C5: Get returns the backend's results"))
	}
	if err := svc.Set(ctx, "sk", []byte("sv"), 3*time.Second); err == nil || err.Error() != "set-error" {
		t.Error(test.DiffMessage(err, "set-error", "C5: Set returns the backend's error"))
	}
	if ok, err := svc.SetNX(ctx, "nk", []byte("nv"), 4*time.Second); !ok || err == nil || err.Error() != "setnx-error" {
		t.Error(test.DiffMessage(fmt.Sprint(ok, err), "true setnx-error", "C5: SetNX returns the backend's results"))
	}
	if v, err := svc.Incr(ctx, "ik", -9, 5*time.Second); v != 4242 || err == nil || err.Error() != "incr-error" {
		t.Error(test.DiffMessage(fmt.Sprint(v, err), "4242 incr-error", "C5: Incr returns the backend's results"))
	}
	if err := svc.Delete(ctx, "dk"); err == nil || err.Error() != "delete-error" {
		t.Error(test.DiffMessage(err, "delete-error", "C5: Delete returns the backend's error"))
	}
	if keys := svc.Keys(ctx); len(keys) != 1 || keys[0] != "keys-result" {
		t.Error(test.DiffMessage(keys, []string{"keys-result"}, "C5: Keys returns the backend's result"))
	}
	if d, ok := svc.TTL(ctx, "tk"); d != 77*time.Second || !ok {
		t.Error(test.DiffMessage(fmt.Sprint(d, ok), "1m17s true", "C5: TTL returns the backend's results"))
	}

	want := []recordedCall{
		{method: "Get", key: "gk"},
		{method: "Set", key: "sk", val: []byte("sv"), ttl: 3 * time.Second},
		{method: "SetNX", key: "nk", val: []byte("nv"), ttl: 4 * time.Second},
		{method: "Incr", key: "ik", delta: -9, ttl: 5 * time.Second},
		{method: "Delete", key: "dk"},
		{method: "Keys"},
		{method: "TTL", key: "tk"},
	}
	if len(rec.calls) != len(want) {
		t.Fatal(test.DiffMessage(len(rec.calls), len(want), "C5: every method forwards exactly one call"))
	}
	for i, w := range want {
		got := rec.calls[i]
		if got.method != w.method || got.key != w.key || string(got.val) != string(w.val) || got.ttl != w.ttl || got.delta != w.delta {
			t.Error(test.DiffMessage(fmt.Sprintf("%+v", got), fmt.Sprintf("%+v", w), "C5: arguments of "+w.method))
		}
		if got.ctx.Value(ctxKey{}) != "marker" {
			t.Error(test.DiffMessage(got.ctx.Value(ctxKey{}), "marker", "C5: ctx of "+w.method))
		}
	}
}

func TestCacheService_GetSet(t *testing.T) {
	svc := newService()
	if err := svc.Set(background, "k", []byte("v"), 0); err != nil {
		t.Fatal(err)
	}
	got, ok := svc.Get(background, "k")
	if !ok || string(got) != "v" {
		t.Error(test.DiffMessage(fmt.Sprint(string(got), ok), "v true", "C1: Get after Set"))
	}
	if got, ok := svc.Get(background, "missing"); ok || got != nil {
		t.Error(test.DiffMessage(fmt.Sprint(len(got), ok), "0 false", "C1: Get of a missing key"))
	}
	if err := svc.Set(background, "empty", nil, 0); err != nil {
		t.Fatal(err)
	}
	if got, ok := svc.Get(background, "empty"); !ok || got == nil || len(got) != 0 {
		t.Error(test.DiffMessage(fmt.Sprintf("%#v %v", got, ok), "[]byte{} true", "C1: a nil value reads back as an empty slice"))
	}
}

func TestCacheService_CopySemantics(t *testing.T) {
	svc := newService()
	val := []byte("original")
	if err := svc.Set(background, "k", val, 0); err != nil {
		t.Fatal(err)
	}
	copy(val, "MUTATED!")
	got, _ := svc.Get(background, "k")
	copy(got, "CHANGED!")
	again, _ := svc.Get(background, "k")
	if string(again) != "original" {
		t.Error(test.DiffMessage(string(again), "original", "C1: values are copied in and out"))
	}
}

func TestCacheService_Errors(t *testing.T) {
	svc := newService()
	if err := svc.Set(background, "", nil, 0); !errors.Is(err, ErrEmptyKey) {
		t.Error(test.DiffMessage(err, ErrEmptyKey, "C1: Set with an empty key"))
	}
	if err := svc.Delete(background, ""); !errors.Is(err, ErrEmptyKey) {
		t.Error(test.DiffMessage(err, ErrEmptyKey, "C1: Delete with an empty key"))
	}
	if _, err := svc.SetNX(background, "", nil, 0); !errors.Is(err, ErrEmptyKey) {
		t.Error(test.DiffMessage(err, ErrEmptyKey, "C1: SetNX with an empty key"))
	}
	if _, err := svc.Incr(background, "", 1, 0); !errors.Is(err, ErrEmptyKey) {
		t.Error(test.DiffMessage(err, ErrEmptyKey, "C1: Incr with an empty key"))
	}
	if err := svc.Set(background, "k", nil, -time.Second); !errors.Is(err, ErrNegativeTTL) {
		t.Error(test.DiffMessage(err, ErrNegativeTTL, "C1: Set with a negative ttl"))
	}
	if _, err := svc.SetNX(background, "k", nil, -time.Second); !errors.Is(err, ErrNegativeTTL) {
		t.Error(test.DiffMessage(err, ErrNegativeTTL, "C1: SetNX with a negative ttl"))
	}
	if _, err := svc.Incr(background, "k", 1, -time.Second); !errors.Is(err, ErrNegativeTTL) {
		t.Error(test.DiffMessage(err, ErrNegativeTTL, "C1: Incr with a negative ttl"))
	}
	if err := svc.Set(background, "k", make([]byte, 1<<20), 0); !errors.Is(err, ErrEntryTooLarge) {
		t.Error(test.DiffMessage(err, ErrEntryTooLarge, "C1: Set of a value above the entry limit"))
	}
	if err := svc.Set(background, "text", []byte("not a counter"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Incr(background, "text", 1, 0); !errors.Is(err, ErrNotCounter) {
		t.Error(test.DiffMessage(err, ErrNotCounter, "C1: Incr on a value of another size"))
	}
	if len(svc.Keys(background)) != 1 {
		t.Error(test.DiffMessage(svc.Keys(background), []string{"text"}, "C1: refused writes store nothing"))
	}
}

func TestCacheService_Expiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc := newService()
		if err := svc.Set(background, "k", []byte("v"), 10*time.Second); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10*time.Second - time.Nanosecond)
		if _, ok := svc.Get(background, "k"); !ok {
			t.Error(test.DiffMessage(false, true, "C1: live one nanosecond before the ttl"))
		}
		if d, ok := svc.TTL(background, "k"); !ok || d != time.Nanosecond {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "1ns true", "C1: TTL"))
		}
		time.Sleep(time.Nanosecond)
		if _, ok := svc.Get(background, "k"); ok {
			t.Error(test.DiffMessage(true, false, "C1: gone at the ttl"))
		}
		if len(svc.Keys(background)) != 0 {
			t.Error(test.DiffMessage(svc.Keys(background), []string{}, "C1: Keys omits expired entries"))
		}
		if ok, _ := svc.SetNX(background, "k", []byte("again"), 0); !ok {
			t.Error(test.DiffMessage(false, true, "C1: SetNX treats an expired key as new"))
		}
		if d, ok := svc.TTL(background, "k"); !ok || d != 0 {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "0s true", "C1: TTL without expiry"))
		}
		if d, ok := svc.TTL(background, "missing"); ok || d != 0 {
			t.Error(test.DiffMessage(fmt.Sprint(d, ok), "0s false", "C1: TTL of a missing key"))
		}
	})
}

func TestCacheService_SetNX(t *testing.T) {
	svc := newService()
	if ok, err := svc.SetNX(background, "k", []byte("first"), 0); !ok || err != nil {
		t.Fatal(test.DiffMessage(fmt.Sprint(ok, err), "true <nil>", "C1: SetNX of a new key"))
	}
	if ok, err := svc.SetNX(background, "k", []byte("second"), 0); ok || err != nil {
		t.Error(test.DiffMessage(fmt.Sprint(ok, err), "false <nil>", "C1: SetNX of a live key"))
	}
	if got, _ := svc.Get(background, "k"); string(got) != "first" {
		t.Error(test.DiffMessage(string(got), "first", "C1: SetNX keeps the first value"))
	}

	wins := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			if ok, _ := svc.SetNX(background, "race", []byte{byte(i)}, 0); ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if wins != 1 {
		t.Error(test.DiffMessage(wins, 1, "C1: concurrent SetNX returns true exactly once"))
	}
}

func TestCacheService_Incr(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc := newService()
		if v, err := svc.Incr(background, "c", 5, 10*time.Second); v != 5 || err != nil {
			t.Fatal(test.DiffMessage(fmt.Sprint(v, err), "5 <nil>", "C1: Incr creates"))
		}
		time.Sleep(4 * time.Second)
		if v, _ := svc.Incr(background, "c", -2, time.Hour); v != 3 {
			t.Error(test.DiffMessage(v, 3, "C1: Incr adds"))
		}
		if d, _ := svc.TTL(background, "c"); d != 6*time.Second {
			t.Error(test.DiffMessage(d, 6*time.Second, "C1: Incr keeps the expiry"))
		}
		time.Sleep(6 * time.Second)
		if v, _ := svc.Incr(background, "c", 1, time.Minute); v != 1 {
			t.Error(test.DiffMessage(v, 1, "C1: Incr after expiry starts over"))
		}
		if v, _ := svc.Incr(background, "w", math.MaxInt64, 0); v != math.MaxInt64 {
			t.Fatal(v)
		}
		if v, _ := svc.Incr(background, "w", 1, 0); v != math.MinInt64 {
			t.Error(test.DiffMessage(v, int64(math.MinInt64), "C1: Incr wraps like int64"))
		}
	})
}

func TestCacheService_IncrMixesWithGetAndSet(t *testing.T) {
	svc := newService()
	if err := svc.Set(background, "c", be(41), 0); err != nil {
		t.Fatal(err)
	}
	if v, err := svc.Incr(background, "c", 1, 0); v != 42 || err != nil {
		t.Error(test.DiffMessage(fmt.Sprint(v, err), "42 <nil>", "C1: Set with 8 bytes writes a counter"))
	}
	got, _ := svc.Get(background, "c")
	if binary.BigEndian.Uint64(got) != 42 || len(got) != 8 {
		t.Error(test.DiffMessage(got, be(42), "C1: Get on a counter returns 8 bytes, big-endian"))
	}
}

func TestCacheService_IncrConcurrentExact(t *testing.T) {
	const workers, each = 12, 500
	svc := newService()
	seen := make([]int, workers*each+1)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Go(func() {
			for i := 0; i < each; i++ {
				v, err := svc.Incr(background, "hot", 1, time.Hour)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				if v >= 1 && v <= workers*each {
					seen[v]++
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	for v := 1; v <= workers*each; v++ {
		if seen[v] != 1 {
			t.Fatal(test.DiffMessage(seen[v], 1, "C1: Incr returns "+strconv.Itoa(v)+" exactly once"))
		}
	}
}

func TestCacheService_DeleteTTLKeys(t *testing.T) {
	svc := newService()
	for i := 0; i < 10; i++ {
		if err := svc.Set(background, "k"+strconv.Itoa(i), []byte("v"), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if keys := svc.Keys(background); len(keys) != 10 {
		t.Error(test.DiffMessage(len(keys), 10, "C1: Keys"))
	}
	if err := svc.Delete(background, "k3"); err != nil {
		t.Error(test.DiffMessage(err, nil, "C1: Delete"))
	}
	if err := svc.Delete(background, "never-set"); err != nil {
		t.Error(test.DiffMessage(err, nil, "C1: deleting a missing key is not an error"))
	}
	if _, ok := svc.Get(background, "k3"); ok {
		t.Error(test.DiffMessage(true, false, "C1: Delete removes the entry"))
	}
	if keys := svc.Keys(background); keys == nil || len(keys) != 9 {
		t.Error(test.DiffMessage(len(keys), 9, "C1: Keys after Delete"))
	}
	fresh := newService()
	if keys := fresh.Keys(background); keys == nil || len(keys) != 0 {
		t.Error(test.DiffMessage(fmt.Sprintf("%#v", keys), "[]string{}", "C1: Keys is never nil"))
	}
}

func TestErrors_AreMemoryCacheErrors(t *testing.T) {
	pairs := []struct {
		name         string
		got, backend error
	}{
		{"ErrEmptyKey", ErrEmptyKey, memorycache.ErrEmptyKey},
		{"ErrNegativeTTL", ErrNegativeTTL, memorycache.ErrNegativeTTL},
		{"ErrEntryTooLarge", ErrEntryTooLarge, memorycache.ErrEntryTooLarge},
		{"ErrNotCounter", ErrNotCounter, memorycache.ErrNotCounter},
	}
	for _, p := range pairs {
		if p.got == nil || p.got != p.backend {
			t.Error(test.DiffMessage(p.got, p.backend, "C6: cache."+p.name+" is memorycache."+p.name))
		}
	}
}
