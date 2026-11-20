package throttler

import (
	"context"
	"encoding/binary"
	"hash/maphash"
	"math"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/inspect"
	"go.owlky.com/owlky/internal/memorycache"
	"go.owlky.com/owlky/internal/tracecontext"
	"go.owlky.com/owlky/modules/cache"
)

type Strategy int

const (
	StrategyFixedWindow Strategy = iota
	StrategySlidingWindow
	StrategyTokenBucket
)

const (
	defaultLimit      = 100
	defaultTTL        = time.Minute
	defaultMaxEntries = 32768
	maxKeyLength      = 128
	numStripes        = 64
	stripeMask        = numStripes - 1
	unknownKey        = "unknown"
	hashedKeyPrefix   = "h:"
	hexDigits         = "0123456789abcdef"

	limitHeader     = "X-Ratelimit-Limit"
	remainingHeader = "X-Ratelimit-Remaining"
	resetHeader     = "X-Ratelimit-Reset"
	retryHeader     = "Retry-After"
)

type Throttler struct {
	Limit    int64
	TTL      time.Duration
	Strategy Strategy
	KeyFunc  func(*ctx.HTTPContext) string
	Backend  cache.Cache
}

type guard struct {
	limit          int64
	limitText      string
	ttl            time.Duration
	windowSec      int64
	strategy       Strategy
	prefix         string
	keyFunc        func(*ctx.HTTPContext) string
	backend        cache.Cache
	isDefaultStore bool
	seed           maphash.Seed
	rejected       atomic.Int64
	stripes        [numStripes]sync.Mutex
}

type result struct {
	isAllowed bool
	remaining int64
	resetAt   int64
}

func (instance Throttler) NewGuard() any {
	return loadThrottlerOptions(&instance)
}

func loadThrottlerOptions(t *Throttler) *guard {
	g := &guard{
		limit:    t.Limit,
		ttl:      t.TTL,
		strategy: t.Strategy,
		keyFunc:  t.KeyFunc,
		backend:  t.Backend,
		seed:     maphash.MakeSeed(),
	}
	if g.limit <= 0 {
		g.limit = defaultLimit
	}
	if g.ttl <= 0 {
		g.ttl = defaultTTL
	}
	switch g.strategy {
	case StrategyFixedWindow:
		g.prefix = "rl:fw:"
	case StrategySlidingWindow:
		g.prefix = "rl:sw:"
	case StrategyTokenBucket:
		g.prefix = "rl:tb:"
	default:
		panic(color.FmtRed("invalid throttler: unknown Strategy %d", int(g.strategy)))
	}
	if g.backend == nil {
		g.backend = memorycache.NewMemoryCache(&memorycache.MemoryCacheOptions{MaxEntries: defaultMaxEntries})
		g.isDefaultStore = true
	}
	g.limitText = strconv.FormatInt(g.limit, 10)
	g.windowSec = max(int64(g.ttl/time.Second), 1)
	return g
}

func (g *guard) CanActivate(c *ctx.HTTPContext) bool {
	var buf [64 + maxKeyLength]byte
	key := g.appendKey(append(buf[:0], g.prefix...), c)

	var res result
	switch g.strategy {
	case StrategySlidingWindow:
		res = g.slidingWindow(c.Context(), key)
	case StrategyTokenBucket:
		res = g.tokenBucket(c.Context(), key)
	default:
		res = g.fixedWindow(c.Context(), key)
	}

	h := c.ResponseWriter.Header()
	h[limitHeader] = []string{g.limitText}
	h[remainingHeader] = []string{strconv.FormatInt(res.remaining, 10)}
	h[resetHeader] = []string{strconv.FormatInt(res.resetAt, 10)}
	if res.isAllowed {
		return true
	}
	g.rejected.Add(1)
	h[retryHeader] = []string{strconv.FormatInt(max(res.resetAt-time.Now().Unix(), 0), 10)}
	panic(exception.TooManyRequestsException("Too Many Requests"))
}

func (g *guard) appendKey(dst []byte, c *ctx.HTTPContext) []byte {
	if g.keyFunc == nil {
		return appendClientIP(dst, tracecontext.ClientIP(c.Request))
	}
	key := g.keyFunc(c)
	if len(key) <= maxKeyLength {
		return append(dst, key...)
	}
	dst = append(dst, hashedKeyPrefix...)
	return appendHex(dst, maphash.String(g.seed, key))
}

func appendClientIP(dst []byte, ip netip.Addr) []byte {
	if !ip.IsValid() {
		return append(dst, unknownKey...)
	}
	ip = ip.Unmap()
	if ip.Is6() {
		prefix, _ := ip.Prefix(64)
		ip = prefix.Addr()
	}
	return ip.AppendTo(dst)
}

func appendHex(dst []byte, v uint64) []byte {
	for shift := 60; shift >= 0; shift -= 4 {
		dst = append(dst, hexDigits[v>>uint(shift)&0xf])
	}
	return dst
}

func (g *guard) fixedWindow(bgCtx context.Context, key []byte) result {
	nowSec := time.Now().Unix()
	windowID := nowSec / g.windowSec
	resetAt := (windowID + 1) * g.windowSec
	key = append(key, ':')
	key = strconv.AppendInt(key, windowID, 10)

	ttl := time.Duration(resetAt-nowSec) * time.Second
	count, err := g.backend.Incr(bgCtx, string(key), 1, ttl)
	if err != nil {
		count = 1
	}
	return result{
		isAllowed: count <= g.limit,
		remaining: max(g.limit-count, 0),
		resetAt:   resetAt,
	}
}

func (g *guard) slidingWindow(bgCtx context.Context, key []byte) result {
	nowSec := time.Now().Unix()
	windowID := nowSec / g.windowSec
	resetAt := (windowID + 1) * g.windowSec
	ratio := float64(nowSec-windowID*g.windowSec) / float64(g.windowSec)
	cacheKey := string(key)

	mu := g.stripeOf(key)
	mu.Lock()
	defer mu.Unlock()
	var prevCount, currCount int64
	if raw, ok := g.backend.Get(bgCtx, cacheKey); ok && len(raw) == 24 {
		switch int64(binary.BigEndian.Uint64(raw)) {
		case windowID:
			prevCount = int64(binary.BigEndian.Uint64(raw[8:]))
			currCount = int64(binary.BigEndian.Uint64(raw[16:]))
		case windowID - 1:
			prevCount = int64(binary.BigEndian.Uint64(raw[16:]))
		}
	}
	weighted := int64(math.Round(float64(prevCount)*(1-ratio))) + currCount + 1
	isAllowed := weighted <= g.limit
	if isAllowed {
		val := make([]byte, 24)
		binary.BigEndian.PutUint64(val, uint64(windowID))
		binary.BigEndian.PutUint64(val[8:], uint64(prevCount))
		binary.BigEndian.PutUint64(val[16:], uint64(currCount+1))
		_ = g.backend.Set(bgCtx, cacheKey, val, time.Duration(2*g.windowSec)*time.Second)
	}

	return result{
		isAllowed: isAllowed,
		remaining: max(g.limit-weighted, 0),
		resetAt:   resetAt,
	}
}

func (g *guard) tokenBucket(bgCtx context.Context, key []byte) result {
	refillRate := float64(g.limit) / float64(g.ttl)
	cacheKey := string(key)

	mu := g.stripeOf(key)
	mu.Lock()
	defer mu.Unlock()
	now := time.Now().UnixNano()
	tokens := float64(g.limit)
	if raw, ok := g.backend.Get(bgCtx, cacheKey); ok && len(raw) == 16 {
		stored := math.Float64frombits(binary.BigEndian.Uint64(raw))
		elapsed := float64(now - int64(binary.BigEndian.Uint64(raw[8:])))
		tokens = min(tokens, stored+max(elapsed, 0)*refillRate)
	}
	isAllowed := tokens >= 1
	if isAllowed {
		tokens--
		val := make([]byte, 16)
		binary.BigEndian.PutUint64(val, math.Float64bits(tokens))
		binary.BigEndian.PutUint64(val[8:], uint64(now))
		_ = g.backend.Set(bgCtx, cacheKey, val, 2*g.ttl)
	}

	wait := g.ttl
	if !isAllowed {
		wait = time.Duration((1 - tokens) / refillRate)
	}
	return result{
		isAllowed: isAllowed,
		remaining: int64(tokens),
		resetAt:   ceilSeconds(now + int64(wait)),
	}
}

// ceilSeconds rounds a Unix time in nanoseconds up to whole seconds, so a refused client that
// waits until the reset time finds the next token there.
func ceilSeconds(ns int64) int64 {
	return (ns + int64(time.Second) - 1) / int64(time.Second)
}

func (g *guard) stripeOf(key []byte) *sync.Mutex {
	return &g.stripes[maphash.Bytes(g.seed, key)&stripeMask]
}

func (g *guard) Snapshot() inspect.Snapshot {
	key, backend := "client-ip", "memorycache"
	if g.keyFunc != nil {
		key = "custom"
	}
	if !g.isDefaultStore {
		backend = "custom"
	}
	fields := []inspect.Field{
		{Key: "limit", Value: g.limit},
		{Key: "ttl", Value: g.ttl},
		{Key: "strategy", Value: strategyName(g.strategy)},
		{Key: "key", Value: key},
		{Key: "backend", Value: backend},
	}
	if g.isDefaultStore {
		if st, ok := inspect.CacheStateOf(g.backend); ok {
			fields = append(fields,
				inspect.Field{Key: "maxKeys", Value: int64(st.MaxEntries)},
				inspect.Field{Key: "keys", Value: int64(st.Entries)},
				inspect.Field{Key: "evictions", Value: st.Evictions},
			)
		}
	}
	return inspect.Snapshot{Name: "throttler", Fields: append(fields, inspect.Field{Key: "rejected", Value: g.rejected.Load()})}
}

func strategyName(s Strategy) string {
	switch s {
	case StrategySlidingWindow:
		return "sliding-window"
	case StrategyTokenBucket:
		return "token-bucket"
	}
	return "fixed-window"
}
