package throttler

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/core"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/inspect"
	"go.owlky.com/owlky/internal/memorycache"
	"go.owlky.com/owlky/internal/test"
)

func benchGuard(b *testing.B, strategy Strategy) activator {
	return compile(b, Throttler{
		Limit:    1 << 40,
		TTL:      time.Minute,
		Strategy: strategy,
		KeyFunc:  func(*ctx.HTTPContext) string { return "benchkey" },
		Backend:  memorycache.NewMemoryCache(nil),
	})
}

func benchCtx() *ctx.HTTPContext {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	c := ctx.NewHTTPContext()
	c.Request = req
	c.ResponseWriter = httptest.NewRecorder()
	return c
}

func pagedThrottler() Throttler {
	return Throttler{Limit: 1, TTL: time.Minute, KeyFunc: func(c *ctx.HTTPContext) string { return c.Request.URL.Path }}
}

type pagedPage struct {
	common.HTTP
	common.Guard
}

func (c pagedPage) NewController() core.Controller {
	c.Prefix("page")
	c.BindGuard(pagedThrottler())
	return c
}

func (c pagedPage) RENDER_hit() {}

type pagedAPI struct {
	common.HTTP
	common.Guard
}

func (c pagedAPI) NewController() core.Controller {
	c.Prefix("api")
	c.BindGuard(pagedThrottler())
	return c
}

func (c pagedAPI) READ_hit() string { return "ok" }

func pagedGet(app *core.App, target, accept string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	if accept != "" {
		r.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func snapshotOf(t testing.TB, a activator) map[string]any {
	t.Helper()
	s, ok := a.(inspect.Snapshotter)
	if !ok {
		t.Fatal(test.DiffMessage(a, "an inspect.Snapshotter", "I1: the compiled throttler implements Snapshotter"))
	}
	snap := s.Snapshot()
	if snap.Name != "throttler" {
		t.Error(test.DiffMessage(snap.Name, "throttler", "I4: snapshot name"))
	}
	fields := make(map[string]any, len(snap.Fields))
	for _, f := range snap.Fields {
		fields[strings.ToLower(f.Key)] = f.Value
	}
	return fields
}

func numberOf(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case uint64:
		return int64(n), true
	}
	return 0, false
}

type activator interface {
	CanActivate(*ctx.HTTPContext) bool
}

type testCache struct {
	mu        sync.RWMutex
	items     map[string][]byte
	incrCalls atomic.Int64
	setCalls  atomic.Int64
}

func newTestCache() *testCache {
	return &testCache{items: make(map[string][]byte)}
}

func (tc *testCache) Get(_ context.Context, key string) ([]byte, bool) {
	tc.mu.RLock()
	v, ok := tc.items[key]
	tc.mu.RUnlock()
	return v, ok
}

func (tc *testCache) Set(_ context.Context, key string, val []byte, _ time.Duration) error {
	tc.setCalls.Add(1)
	tc.mu.Lock()
	tc.items[key] = append([]byte(nil), val...)
	tc.mu.Unlock()
	return nil
}

func (tc *testCache) SetNX(_ context.Context, key string, val []byte, _ time.Duration) (bool, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if _, ok := tc.items[key]; ok {
		return false, nil
	}
	tc.items[key] = append([]byte(nil), val...)
	return true, nil
}

func (tc *testCache) Incr(_ context.Context, key string, delta int64, _ time.Duration) (int64, error) {
	tc.incrCalls.Add(1)
	tc.mu.Lock()
	defer tc.mu.Unlock()
	var cur int64
	if v, ok := tc.items[key]; ok {
		if len(v) != 8 {
			return 0, memorycache.ErrNotCounter
		}
		cur = int64(binary.BigEndian.Uint64(v))
	}
	cur += delta
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(cur))
	tc.items[key] = b
	return cur, nil
}

func (tc *testCache) Delete(_ context.Context, key string) error {
	tc.mu.Lock()
	delete(tc.items, key)
	tc.mu.Unlock()
	return nil
}

func (tc *testCache) Keys(_ context.Context) []string {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	keys := make([]string, 0, len(tc.items))
	for k := range tc.items {
		keys = append(keys, k)
	}
	return keys
}

func (tc *testCache) TTL(_ context.Context, _ string) (time.Duration, bool) {
	return 0, false
}

type failingCache struct {
	testCache
}

func (fc *failingCache) Get(context.Context, string) ([]byte, bool) {
	return nil, false
}

func (fc *failingCache) Set(context.Context, string, []byte, time.Duration) error {
	return errors.New("backend down")
}

func (fc *failingCache) SetNX(context.Context, string, []byte, time.Duration) (bool, error) {
	return false, errors.New("backend down")
}

func (fc *failingCache) Incr(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, errors.New("backend down")
}

var allStrategies = []struct {
	name     string
	strategy Strategy
}{
	{"FixedWindow", StrategyFixedWindow},
	{"SlidingWindow", StrategySlidingWindow},
	{"TokenBucket", StrategyTokenBucket},
}

func compile(t testing.TB, g Throttler) activator {
	t.Helper()
	guarder := g.NewGuard()
	a, ok := guarder.(activator)
	if !ok {
		t.Fatal(test.DiffMessage(guarder, "a value with CanActivate(*ctx.HTTPContext) bool", "T6: NewGuard must return a guard"))
	}
	return a
}

func newCtx(remoteAddr string, headers map[string]string) *ctx.HTTPContext {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c := ctx.NewHTTPContext()
	c.Request = req
	c.ResponseWriter = httptest.NewRecorder()
	return c
}

func try(a activator, c *ctx.HTTPContext) (allowed bool, refusal any) {
	defer func() {
		if r := recover(); r != nil {
			allowed = false
			refusal = r
		}
	}()
	return a.CanActivate(c), nil
}

func headersOf(c *ctx.HTTPContext) http.Header {
	return c.ResponseWriter.(*httptest.ResponseRecorder).Header()
}

func fixedKeyFunc(key string) func(*ctx.HTTPContext) string {
	return func(*ctx.HTTPContext) string { return key }
}

func storeHas(tc *testCache, part string) bool {
	for _, k := range tc.Keys(context.Background()) {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}

func exactnessRun(t *testing.T, a activator, limit int64, goroutines, perGoroutine int) {
	t.Helper()
	var allowed atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range perGoroutine {
				if ok, _ := try(a, newCtx("203.0.113.9:1", nil)); ok {
					allowed.Add(1)
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := allowed.Load(); got != limit {
		t.Error(test.DiffMessage(got, limit, "T3: exactly Limit requests allowed out of "+strconv.Itoa(goroutines*perGoroutine)))
	}
}
