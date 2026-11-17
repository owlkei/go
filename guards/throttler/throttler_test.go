package throttler

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/memorycache"
	"github.com/dangduoc08/ginject/internal/test"
)

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

func TestThrottler_DefaultsAreAppliedByNewGuard(t *testing.T) {
	a := compile(t, Throttler{})
	c := newCtx("203.0.113.9:1000", nil)
	if ok, _ := try(a, c); !ok {
		t.Error(test.DiffMessage(ok, true, "T6: first request must pass"))
	}
	h := headersOf(c)
	if got := h.Get("X-RateLimit-Limit"); got != "100" {
		t.Error(test.DiffMessage(got, "100", "T6: default Limit"))
	}
	if got := h.Get("X-RateLimit-Remaining"); got != "99" {
		t.Error(test.DiffMessage(got, "99", "T6: remaining after one request"))
	}
	reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		t.Fatal(test.DiffMessage(h.Get("X-RateLimit-Reset"), "unix seconds", "T6: X-RateLimit-Reset"))
	}
	now := time.Now().Unix()
	if reset < now || reset > now+60 {
		t.Error(test.DiffMessage(reset, now+60, "T6: default TTL is one minute, so Reset is within 60 s"))
	}
}

func TestThrottler_DefaultStrategyIsFixedWindow(t *testing.T) {
	tc := newTestCache()
	a := compile(t, Throttler{Backend: tc, KeyFunc: fixedKeyFunc("k")})
	a.CanActivate(newCtx("203.0.113.9:1", nil))
	if got := tc.incrCalls.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "T3, T6: the zero Strategy is FixedWindow and counts with Incr"))
	}
}

func TestThrottler_RefusalIs429WithRetryAfter(t *testing.T) {
	for _, s := range allStrategies {
		a := compile(t, Throttler{Limit: 1, TTL: time.Hour, Strategy: s.strategy, Backend: newTestCache(), KeyFunc: fixedKeyFunc("k")})
		if ok, _ := try(a, newCtx("203.0.113.9:1", nil)); !ok {
			t.Fatal(test.DiffMessage(ok, true, "T6: "+s.name+" first request"))
		}
		c := newCtx("203.0.113.9:1", nil)
		ok, refusal := try(a, c)
		if ok {
			t.Error(test.DiffMessage(ok, false, "T6: "+s.name+" second request must be refused"))
		}
		e, isException := refusal.(exception.Exception)
		if !isException || e.Code() != http.StatusTooManyRequests {
			t.Error(test.DiffMessage(refusal, "exception 429", "T6: "+s.name+" refusal"))
		}
		h := headersOf(c)
		secs, err := strconv.ParseInt(h.Get("Retry-After"), 10, 64)
		if err != nil || secs < 0 || secs > 3601 {
			t.Error(test.DiffMessage(h.Get("Retry-After"), "seconds in [0, 3601]", "T6: "+s.name+" Retry-After"))
		}
		if h.Get("X-RateLimit-Remaining") != "0" {
			t.Error(test.DiffMessage(h.Get("X-RateLimit-Remaining"), "0", "T6: "+s.name+" Remaining on refusal"))
		}
		if h.Get("X-RateLimit-Limit") != "1" {
			t.Error(test.DiffMessage(h.Get("X-RateLimit-Limit"), "1", "T6: "+s.name+" Limit on refusal"))
		}
	}
}

func TestThrottler_AllowedRequestHasNoRetryAfter(t *testing.T) {
	a := compile(t, Throttler{Limit: 5, Backend: newTestCache(), KeyFunc: fixedKeyFunc("k")})
	c := newCtx("203.0.113.9:1", nil)
	a.CanActivate(c)
	if got := headersOf(c).Get("Retry-After"); got != "" {
		t.Error(test.DiffMessage(got, "", "T6: Retry-After only on a refusal"))
	}
}

func TestThrottler_RemainingDecreases(t *testing.T) {
	for _, s := range allStrategies {
		a := compile(t, Throttler{Limit: 5, TTL: 24 * time.Hour, Strategy: s.strategy, Backend: newTestCache(), KeyFunc: fixedKeyFunc("k")})
		for i := int64(1); i <= 5; i++ {
			c := newCtx("203.0.113.9:1", nil)
			if ok, _ := try(a, c); !ok {
				t.Fatal(test.DiffMessage(ok, true, "T3: "+s.name+" request "+strconv.FormatInt(i, 10)))
			}
			if got := headersOf(c).Get("X-RateLimit-Remaining"); got != strconv.FormatInt(5-i, 10) {
				t.Error(test.DiffMessage(got, strconv.FormatInt(5-i, 10), "T6: "+s.name+" remaining"))
			}
		}
		if ok, _ := try(a, newCtx("203.0.113.9:1", nil)); ok {
			t.Error(test.DiffMessage(ok, false, "T3: "+s.name+" the sixth request must be refused"))
		}
	}
}

func TestThrottler_DifferentKeysAreIndependent(t *testing.T) {
	for _, s := range allStrategies {
		a := compile(t, Throttler{
			Limit:    1,
			TTL:      24 * time.Hour,
			Strategy: s.strategy,
			Backend:  newTestCache(),
			KeyFunc:  func(c *ctx.HTTPContext) string { return c.Request.Header.Get("X-Key") },
		})
		for _, k := range []string{"a", "b", "c"} {
			if ok, _ := try(a, newCtx("203.0.113.9:1", map[string]string{"X-Key": k})); !ok {
				t.Error(test.DiffMessage(ok, true, "T3: "+s.name+" key "+k+" has its own budget"))
			}
		}
	}
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

func TestFixedWindow_ExactUnderConcurrency(t *testing.T) {
	exactnessRun(t, compile(t, Throttler{Limit: 1000, TTL: 24 * time.Hour, Strategy: StrategyFixedWindow, Backend: newTestCache(), KeyFunc: fixedKeyFunc("k")}), 1000, 12, 1000)
}

func TestFixedWindow_ExactUnderConcurrency_MemoryCache(t *testing.T) {
	exactnessRun(t, compile(t, Throttler{Limit: 1000, TTL: 24 * time.Hour, Strategy: StrategyFixedWindow, Backend: memorycache.NewMemoryCache(nil), KeyFunc: fixedKeyFunc("k")}), 1000, 12, 1000)
}

func TestFixedWindow_ExactUnderConcurrency_DefaultStore(t *testing.T) {
	exactnessRun(t, compile(t, Throttler{Limit: 1000, TTL: 24 * time.Hour, Strategy: StrategyFixedWindow, KeyFunc: fixedKeyFunc("k")}), 1000, 12, 1000)
}

func TestSlidingWindow_ExactUnderConcurrency(t *testing.T) {
	exactnessRun(t, compile(t, Throttler{Limit: 1000, TTL: 24 * time.Hour, Strategy: StrategySlidingWindow, Backend: newTestCache(), KeyFunc: fixedKeyFunc("k")}), 1000, 12, 1000)
}

func TestSlidingWindow_ExactUnderConcurrency_DefaultStore(t *testing.T) {
	exactnessRun(t, compile(t, Throttler{Limit: 1000, TTL: 24 * time.Hour, Strategy: StrategySlidingWindow, KeyFunc: fixedKeyFunc("k")}), 1000, 12, 1000)
}

func TestTokenBucket_ExactUnderConcurrency(t *testing.T) {
	exactnessRun(t, compile(t, Throttler{Limit: 1000, TTL: 24 * time.Hour, Strategy: StrategyTokenBucket, Backend: newTestCache(), KeyFunc: fixedKeyFunc("k")}), 1000, 12, 1000)
}

func TestTokenBucket_ExactUnderConcurrency_DefaultStore(t *testing.T) {
	exactnessRun(t, compile(t, Throttler{Limit: 1000, TTL: 24 * time.Hour, Strategy: StrategyTokenBucket, KeyFunc: fixedKeyFunc("k")}), 1000, 12, 1000)
}

func TestFixedWindow_CountsWithIncrOnly(t *testing.T) {
	tc := newTestCache()
	a := compile(t, Throttler{Limit: 10, Backend: tc, KeyFunc: fixedKeyFunc("k")})
	for range 25 {
		try(a, newCtx("203.0.113.9:1", nil))
	}
	if got := tc.incrCalls.Load(); got != 25 {
		t.Error(test.DiffMessage(got, int64(25), "T3: one Incr per request"))
	}
	if got := tc.setCalls.Load(); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "T3: the fixed window never does read-then-Set"))
	}
}

func TestDefaultKey_ClientIP(t *testing.T) {
	tc := newTestCache()
	a := compile(t, Throttler{Backend: tc})
	a.CanActivate(newCtx("203.0.113.9:4711", nil))
	if !storeHas(tc, "203.0.113.9") {
		t.Error(test.DiffMessage(tc.Keys(context.Background()), "a key holding 203.0.113.9", "T1: IPv4 key as is"))
	}
	if storeHas(tc, "4711") {
		t.Error(test.DiffMessage(tc.Keys(context.Background()), "no port in the key", "T1: the port is not part of the key"))
	}
}

func TestDefaultKey_PortsShareOneBudget(t *testing.T) {
	a := compile(t, Throttler{Limit: 2, TTL: 24 * time.Hour, Backend: newTestCache()})
	var allowed int
	for port := 1000; port < 1010; port++ {
		if ok, _ := try(a, newCtx("203.0.113.9:"+strconv.Itoa(port), nil)); ok {
			allowed++
		}
	}
	if allowed != 2 {
		t.Error(test.DiffMessage(allowed, 2, "T1: one client IP, one budget across source ports"))
	}
}

func TestDefaultKey_IgnoresHeaders(t *testing.T) {
	headers := []string{"X-Forwarded-For", "X-Real-IP", "Forwarded", "X-Client-IP", "True-Client-IP", "CF-Connecting-IP", "X-Forwarded-Host"}
	tc := newTestCache()
	a := compile(t, Throttler{Backend: tc})
	h := map[string]string{}
	for _, name := range headers {
		h[name] = "198.51.100.77"
	}
	a.CanActivate(newCtx("203.0.113.9:1", h))
	if storeHas(tc, "198.51.100.77") {
		t.Error(test.DiffMessage(tc.Keys(context.Background()), "no key from a request header", "T1: no request header is read"))
	}
	if !storeHas(tc, "203.0.113.9") {
		t.Error(test.DiffMessage(tc.Keys(context.Background()), "a key holding 203.0.113.9", "T1: the peer is the key"))
	}
}

func TestThrottler_SpoofedHeaderSameKey(t *testing.T) {
	for _, s := range allStrategies {
		a := compile(t, Throttler{Limit: 3, TTL: 24 * time.Hour, Strategy: s.strategy, Backend: newTestCache()})
		var allowed int
		for i := range 50 {
			ip := "198.51.100." + strconv.Itoa(i)
			h := map[string]string{"X-Forwarded-For": ip, "X-Real-IP": ip}
			if ok, _ := try(a, newCtx("203.0.113.9:1", h)); ok {
				allowed++
			}
		}
		if allowed != 3 {
			t.Error(test.DiffMessage(allowed, 3, "T1: "+s.name+" a rotating spoofed header does not buy a new budget"))
		}
	}
}

func TestDefaultKey_IPv6Slash64(t *testing.T) {
	tc := newTestCache()
	a := compile(t, Throttler{Backend: tc})
	a.CanActivate(newCtx("[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:443", nil))
	if !storeHas(tc, "2001:db8:1:2::") {
		t.Error(test.DiffMessage(tc.Keys(context.Background()), "a key holding 2001:db8:1:2::", "T1: IPv6 masked to its /64"))
	}
	if storeHas(tc, "aaaa") || storeHas(tc, "dddd") {
		t.Error(test.DiffMessage(tc.Keys(context.Background()), "no host bits in the key", "T1: low 64 bits dropped"))
	}
}

func TestDefaultKey_IPv6SameSlash64SharesBudget(t *testing.T) {
	a := compile(t, Throttler{Limit: 2, TTL: 24 * time.Hour, Backend: newTestCache()})
	var allowed int
	for i := range 20 {
		addr := "[2001:db8:1:2:" + strconv.FormatInt(int64(i)+1, 16) + "::" + strconv.FormatInt(int64(i)*7+1, 16) + "]:1"
		if ok, _ := try(a, newCtx(addr, nil)); ok {
			allowed++
		}
	}
	if allowed != 2 {
		t.Error(test.DiffMessage(allowed, 2, "T1: addresses of one /64 share a budget"))
	}
}

func TestDefaultKey_DifferentSlash64sAreIndependent(t *testing.T) {
	a := compile(t, Throttler{Limit: 1, TTL: 24 * time.Hour, Backend: newTestCache()})
	for _, addr := range []string{"[2001:db8:1:1::1]:1", "[2001:db8:1:2::1]:1", "[2001:db8:1:3::1]:1"} {
		if ok, _ := try(a, newCtx(addr, nil)); !ok {
			t.Error(test.DiffMessage(ok, true, "T1: "+addr+" has its own budget"))
		}
	}
}

func TestDefaultKey_IPv4MappedIPv6IsIPv4(t *testing.T) {
	a := compile(t, Throttler{Limit: 1, TTL: 24 * time.Hour, Backend: newTestCache()})
	if ok, _ := try(a, newCtx("203.0.113.9:1", nil)); !ok {
		t.Fatal(test.DiffMessage(ok, true, "T1: first request"))
	}
	if ok, _ := try(a, newCtx("[::ffff:203.0.113.9]:1", nil)); ok {
		t.Error(test.DiffMessage(ok, false, "T1: the mapped form is the same client"))
	}
}

func TestDefaultKey_Unknown(t *testing.T) {
	tc := newTestCache()
	a := compile(t, Throttler{Limit: 2, TTL: 24 * time.Hour, Backend: tc})
	var allowed int
	for _, addr := range []string{"", "garbage", "not-an-address:1", "@"} {
		if ok, _ := try(a, newCtx(addr, nil)); ok {
			allowed++
		}
	}
	if !storeHas(tc, "unknown") {
		t.Error(test.DiffMessage(tc.Keys(context.Background()), "a key holding unknown", "T1: the zero Addr is keyed unknown"))
	}
	if allowed != 2 {
		t.Error(test.DiffMessage(allowed, 2, "T1: every unresolvable peer shares the unknown budget"))
	}
}

func TestThrottler_KeyFuncOverridesDefault(t *testing.T) {
	tc := newTestCache()
	a := compile(t, Throttler{Backend: tc, KeyFunc: fixedKeyFunc("tenant-42")})
	a.CanActivate(newCtx("203.0.113.9:1", nil))
	if !storeHas(tc, "tenant-42") || storeHas(tc, "203.0.113.9") {
		t.Error(test.DiffMessage(tc.Keys(context.Background()), "key from KeyFunc only", "T1: KeyFunc replaces the default"))
	}
}

func TestThrottler_LongKeyHashed(t *testing.T) {
	hashed := regexp.MustCompile(`h:[0-9a-f]{16}(?:[^0-9a-f]|$)`)
	for _, s := range allStrategies {
		tc := newTestCache()
		long := strings.Repeat("x", 129)
		a := compile(t, Throttler{Backend: tc, Strategy: s.strategy, KeyFunc: fixedKeyFunc(long)})
		a.CanActivate(newCtx("203.0.113.9:1", nil))
		keys := tc.Keys(context.Background())
		if len(keys) == 0 {
			t.Fatal(test.DiffMessage(keys, "one key", "T2: "+s.name+" stored nothing"))
		}
		for _, k := range keys {
			if strings.Contains(k, long) || strings.Contains(k, long[:64]) {
				t.Error(test.DiffMessage(len(k), "a short hashed key", "T2: "+s.name+" 129 bytes are hashed"))
			}
			if !hashed.MatchString(k) {
				t.Error(test.DiffMessage(k, "h: and 16 hex digits", "T2: "+s.name+" hashed form"))
			}
			if len(k) > 128 {
				t.Error(test.DiffMessage(len(k), "at most 128 bytes", "T2: "+s.name+" stored key length"))
			}
		}
	}
}

func TestThrottler_Key128BytesNotHashed(t *testing.T) {
	tc := newTestCache()
	exact := strings.Repeat("y", 128)
	a := compile(t, Throttler{Backend: tc, KeyFunc: fixedKeyFunc(exact)})
	a.CanActivate(newCtx("203.0.113.9:1", nil))
	if !storeHas(tc, exact) {
		t.Error(test.DiffMessage(tc.Keys(context.Background()), "the key as is", "T2: 128 bytes is not longer than 128"))
	}
}

func TestThrottler_LongKeysStayDistinctAndStable(t *testing.T) {
	a := compile(t, Throttler{Limit: 1, TTL: 24 * time.Hour, Backend: newTestCache(), KeyFunc: func(c *ctx.HTTPContext) string {
		return strings.Repeat("z", 200) + c.Request.Header.Get("X-Suffix")
	}})
	if ok, _ := try(a, newCtx("203.0.113.9:1", map[string]string{"X-Suffix": "a"})); !ok {
		t.Error(test.DiffMessage(ok, true, "T2: first long key"))
	}
	if ok, _ := try(a, newCtx("203.0.113.9:1", map[string]string{"X-Suffix": "b"})); !ok {
		t.Error(test.DiffMessage(ok, true, "T2: a different long key has its own budget"))
	}
	if ok, _ := try(a, newCtx("203.0.113.9:1", map[string]string{"X-Suffix": "a"})); ok {
		t.Error(test.DiffMessage(ok, false, "T2: the same long key hashes to the same entry"))
	}
}

func TestThrottler_HostileKeysDoNotPanic(t *testing.T) {
	keys := []string{"\x00", "\xff\xfe", strings.Repeat("\x00", 1000), "a:b:c", "rl:fw:k:0", strings.Repeat("é", 100)}
	for _, s := range allStrategies {
		for _, k := range keys {
			a := compile(t, Throttler{Backend: newTestCache(), Strategy: s.strategy, KeyFunc: fixedKeyFunc(k)})
			c := newCtx("203.0.113.9:1", nil)
			ok, refusal := try(a, c)
			if !ok || refusal != nil {
				t.Error(test.DiffMessage(refusal, nil, "T2: "+s.name+" hostile key must be counted, not crash"))
			}
		}
	}
}

func TestThrottler_BackendErrorCountsAsAllowed(t *testing.T) {
	for _, s := range allStrategies {
		a := compile(t, Throttler{Limit: 1, Strategy: s.strategy, Backend: &failingCache{testCache: *newTestCache()}, KeyFunc: fixedKeyFunc("k")})
		for i := range 5 {
			c := newCtx("203.0.113.9:1", nil)
			ok, refusal := try(a, c)
			if !ok || refusal != nil {
				t.Error(test.DiffMessage(refusal, nil, "T7: "+s.name+" request "+strconv.Itoa(i)+" with a failing backend is allowed"))
			}
		}
	}
}

func TestThrottler_NoGoroutine(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	guards := make([]activator, 0, 30)
	for range 10 {
		for _, s := range allStrategies {
			guards = append(guards, compile(t, Throttler{Strategy: s.strategy}))
		}
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Error(test.DiffMessage(got, before, "T5: creating guards starts no goroutine"))
	}
	for i, g := range guards {
		try(g, newCtx("203.0.113.9:"+strconv.Itoa(i+1), nil))
	}
	runtime.Gosched()
	if got := runtime.NumGoroutine(); got > before {
		t.Error(test.DiffMessage(got, before, "T5: serving requests starts no goroutine"))
	}
}

func TestThrottler_ConcurrentMixedKeysUnderRace(t *testing.T) {
	for _, s := range allStrategies {
		a := compile(t, Throttler{Limit: 50, TTL: 24 * time.Hour, Strategy: s.strategy, KeyFunc: func(c *ctx.HTTPContext) string {
			return c.Request.Header.Get("X-Key")
		}})
		var wg sync.WaitGroup
		for g := range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range 500 {
					try(a, newCtx("203.0.113.9:1", map[string]string{"X-Key": strconv.Itoa((g + i) % 20)}))
				}
			}()
		}
		wg.Wait()
	}
}

func TestTokenBucket_RetryAfterNeverZeroOnRefusal(t *testing.T) {
	// 100 tokens per second: once the bucket is empty, the next token is 10 ms away.
	a := compile(t, Throttler{Limit: 100, TTL: time.Second, Strategy: StrategyTokenBucket, Backend: newTestCache(), KeyFunc: fixedKeyFunc("k")})
	for range 100 {
		try(a, newCtx("203.0.113.9:1", nil))
	}
	c := newCtx("203.0.113.9:1", nil)
	if ok, _ := try(a, c); ok {
		t.Fatal(test.DiffMessage(ok, false, "the 101st request within a second is refused"))
	}
	h := headersOf(c)
	if got := h.Get("Retry-After"); got != "1" {
		t.Error(test.DiffMessage(got, "1", "a wait under a second rounds up, so a client that obeys it is served"))
	}
	reset, _ := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if now := time.Now().Unix(); reset <= now-1 {
		t.Error(test.DiffMessage(reset, "after the current second", "X-RateLimit-Reset rounds up too"))
	}
}
