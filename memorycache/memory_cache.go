package memorycache

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"weak"
)

const (
	numShards         = 64
	shardMask         = numShards - 1
	sweepEvery        = 5 * time.Second
	defaultMaxEntries = 65536
	defaultMaxBytes   = 16 << 20
	minMaxBytes       = 64 << 10
	counterSize       = 8
)

var (
	ErrEmptyKey      = errors.New("memorycache: key must not be empty")
	ErrNegativeTTL   = errors.New("memorycache: ttl must not be negative")
	ErrEntryTooLarge = errors.New("memorycache: entry is larger than the cache allows")
	ErrNotCounter    = errors.New("memorycache: value is not a counter")
)

type MemoryCacheOptions struct {
	MaxEntries int
	MaxBytes   int64
}

type MemoryCache struct {
	shards             [numShards]shard
	epoch              time.Time
	maxEntriesPerShard int
	maxBytesPerShard   int64
	sweepMu            sync.Mutex
	isArmed            bool
	isDirty            bool
	timer              *time.Timer
}

func NewMemoryCache(opts *MemoryCacheOptions) *MemoryCache {
	o := loadMemoryCacheOptions(opts)
	return &MemoryCache{
		epoch:              time.Now(),
		maxEntriesPerShard: o.MaxEntries / numShards,
		maxBytesPerShard:   o.MaxBytes / numShards,
	}
}

func loadMemoryCacheOptions(opts *MemoryCacheOptions) MemoryCacheOptions {
	var o MemoryCacheOptions
	if opts != nil {
		o = *opts
	}
	switch {
	case o.MaxEntries <= 0:
		o.MaxEntries = defaultMaxEntries
	case o.MaxEntries < numShards:
		o.MaxEntries = numShards
	default:
		o.MaxEntries -= o.MaxEntries % numShards
	}
	switch {
	case o.MaxBytes <= 0:
		o.MaxBytes = defaultMaxBytes
	case o.MaxBytes < minMaxBytes:
		o.MaxBytes = minMaxBytes
	}
	return o
}

func (mc *MemoryCache) Get(_ context.Context, key string) ([]byte, bool) {
	if key == "" {
		mc.shards[0].misses.Add(1)
		return nil, false
	}
	s := mc.shardOf(key)
	now := mc.now()
	s.mu.RLock()
	i, ok := s.slotByKey[key]
	if !ok {
		s.mu.RUnlock()
		s.misses.Add(1)
		return nil, false
	}
	e := &s.entries[i]
	if e.isExpired(now) {
		s.mu.RUnlock()
		s.misses.Add(1)
		return nil, false
	}
	if atomic.LoadUint32(&e.ref) == 0 {
		atomic.StoreUint32(&e.ref, 1)
	}
	out := make([]byte, len(e.val))
	copy(out, e.val)
	s.mu.RUnlock()
	s.hits.Add(1)
	return out, true
}

func (mc *MemoryCache) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	if err := mc.checkWrite(key, len(val), ttl); err != nil {
		return err
	}
	s := mc.shardOf(key)
	now := mc.now()
	s.mu.Lock()
	isFirstExpiring := mc.storeLocked(s, key, val, deadline(now, ttl), now)
	s.mu.Unlock()
	if isFirstExpiring {
		mc.arm()
	}
	return nil
}

func (mc *MemoryCache) SetNX(_ context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	if err := mc.checkWrite(key, len(val), ttl); err != nil {
		return false, err
	}
	s := mc.shardOf(key)
	now := mc.now()
	s.mu.Lock()
	if i, ok := s.slotByKey[key]; ok && !s.entries[i].isExpired(now) {
		s.mu.Unlock()
		return false, nil
	}
	isFirstExpiring := mc.storeLocked(s, key, val, deadline(now, ttl), now)
	s.mu.Unlock()
	if isFirstExpiring {
		mc.arm()
	}
	return true, nil
}

func (mc *MemoryCache) Incr(_ context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	if err := mc.checkWrite(key, counterSize, ttl); err != nil {
		return 0, err
	}
	s := mc.shardOf(key)
	now := mc.now()
	s.mu.Lock()
	if i, ok := s.slotByKey[key]; ok && !s.entries[i].isExpired(now) {
		e := &s.entries[i]
		if len(e.val) != counterSize {
			s.mu.Unlock()
			return 0, ErrNotCounter
		}
		sum := int64(binary.BigEndian.Uint64(e.val)) + delta
		binary.BigEndian.PutUint64(e.val, uint64(sum))
		atomic.StoreUint32(&e.ref, 1)
		s.mu.Unlock()
		return sum, nil
	}
	var buf [counterSize]byte
	binary.BigEndian.PutUint64(buf[:], uint64(delta))
	isFirstExpiring := mc.storeLocked(s, key, buf[:], deadline(now, ttl), now)
	s.mu.Unlock()
	if isFirstExpiring {
		mc.arm()
	}
	return delta, nil
}

func (mc *MemoryCache) Delete(_ context.Context, key string) error {
	if key == "" {
		return ErrEmptyKey
	}
	s := mc.shardOf(key)
	s.mu.Lock()
	if i, ok := s.slotByKey[key]; ok {
		s.removeLocked(i)
	}
	s.mu.Unlock()
	return nil
}

func (mc *MemoryCache) Keys(_ context.Context) []string {
	now := mc.now()
	keys := make([]string, 0, 64)
	for i := range mc.shards {
		s := &mc.shards[i]
		s.mu.RLock()
		for j := range s.entries {
			e := &s.entries[j]
			if e.key != "" && !e.isExpired(now) {
				keys = append(keys, e.key)
			}
		}
		s.mu.RUnlock()
	}
	return keys
}

func (mc *MemoryCache) TTL(_ context.Context, key string) (time.Duration, bool) {
	if key == "" {
		return 0, false
	}
	s := mc.shardOf(key)
	now := mc.now()
	s.mu.RLock()
	i, ok := s.slotByKey[key]
	if !ok {
		s.mu.RUnlock()
		return 0, false
	}
	expiresAt := s.entries[i].expiresAt
	s.mu.RUnlock()
	switch {
	case expiresAt == 0:
		return 0, true
	case now >= expiresAt:
		return 0, false
	default:
		return time.Duration(expiresAt - now), true
	}
}

func (mc *MemoryCache) shardOf(key string) *shard {
	return &mc.shards[hashKey(key)&shardMask]
}

func (mc *MemoryCache) now() int64 {
	return int64(time.Since(mc.epoch))
}

func (mc *MemoryCache) checkWrite(key string, valLen int, ttl time.Duration) error {
	switch {
	case key == "":
		return ErrEmptyKey
	case ttl < 0:
		return ErrNegativeTTL
	case int64(len(key))+int64(valLen) > mc.maxBytesPerShard:
		return ErrEntryTooLarge
	}
	return nil
}

func deadline(now int64, ttl time.Duration) int64 {
	if ttl == 0 || int64(ttl) > math.MaxInt64-now {
		return 0
	}
	return now + int64(ttl)
}

func (mc *MemoryCache) storeLocked(s *shard, key string, val []byte, expiresAt, now int64) bool {
	size := int64(len(key) + len(val))
	if i, ok := s.slotByKey[key]; ok {
		e := &s.entries[i]
		if e.isExpired(now) {
			s.expirations++
			atomic.StoreUint32(&e.ref, 0)
		} else {
			atomic.StoreUint32(&e.ref, 1)
		}
		growth := int64(len(val) - len(e.val))
		for s.bytes+growth > mc.maxBytesPerShard {
			s.evictOneLocked(now, i)
		}
		s.bytes += growth
		e.val = reuseOrCopy(e.val, val)
		return s.setExpiryLocked(e, expiresAt)
	}
	for len(s.slotByKey) >= mc.maxEntriesPerShard || s.bytes+size > mc.maxBytesPerShard {
		s.evictOneLocked(now, -1)
	}
	if s.slotByKey == nil {
		s.slotByKey = make(map[string]int32)
	}
	var i int32
	if n := len(s.free); n > 0 {
		i = s.free[n-1]
		s.free = s.free[:n-1]
	} else {
		i = int32(len(s.entries))
		s.entries = append(s.entries, entry{})
	}
	e := &s.entries[i]
	e.key = strings.Clone(key)
	e.val = reuseOrCopy(nil, val)
	s.slotByKey[e.key] = i
	s.bytes += size
	return s.setExpiryLocked(e, expiresAt)
}

func reuseOrCopy(stored, val []byte) []byte {
	if len(val) <= cap(stored) && cap(stored) <= 2*len(val) {
		stored = stored[:len(val)]
		copy(stored, val)
		return stored
	}
	out := make([]byte, len(val))
	copy(out, val)
	return out
}

func (mc *MemoryCache) arm() {
	mc.sweepMu.Lock()
	mc.isDirty = true
	if !mc.isArmed {
		mc.isArmed = true
		if mc.timer == nil {
			mc.timer = time.AfterFunc(sweepEvery, sweepOf(weak.Make(mc)))
		} else {
			mc.timer.Reset(sweepEvery)
		}
	}
	mc.sweepMu.Unlock()
}

func sweepOf(cache weak.Pointer[MemoryCache]) func() {
	return func() {
		if mc := cache.Value(); mc != nil {
			mc.sweep()
		}
	}
}

func (mc *MemoryCache) sweep() {
	mc.sweepMu.Lock()
	mc.isDirty = false
	mc.sweepMu.Unlock()

	now := mc.now()
	isExpiring := false
	for i := range mc.shards {
		s := &mc.shards[i]
		s.mu.Lock()
		s.sweepLocked(now)
		if s.expiring > 0 {
			isExpiring = true
		}
		s.mu.Unlock()
	}

	mc.sweepMu.Lock()
	if isExpiring || mc.isDirty {
		mc.timer.Reset(sweepEvery)
	} else {
		mc.isArmed = false
	}
	mc.sweepMu.Unlock()
}

type entry struct {
	key       string
	val       []byte
	expiresAt int64
	ref       uint32
}

func (e *entry) isExpired(now int64) bool {
	return e.expiresAt != 0 && now >= e.expiresAt
}

type shard struct {
	mu          sync.RWMutex
	slotByKey   map[string]int32
	entries     []entry
	free        []int32
	hand        int
	bytes       int64
	expiring    int
	evictions   uint64
	expirations uint64
	hits        atomic.Uint64
	misses      atomic.Uint64
	_           [56]byte
}

func (s *shard) setExpiryLocked(e *entry, expiresAt int64) bool {
	wasExpiring := e.expiresAt != 0
	e.expiresAt = expiresAt
	switch isExpiring := expiresAt != 0; {
	case isExpiring && !wasExpiring:
		s.expiring++
		return s.expiring == 1
	case !isExpiring && wasExpiring:
		s.expiring--
	}
	return false
}

func (s *shard) evictOneLocked(now int64, protected int32) {
	for {
		i := s.hand
		s.hand++
		if s.hand >= len(s.entries) {
			s.hand = 0
		}
		e := &s.entries[i]
		switch {
		case e.key == "" || int32(i) == protected:
		case e.isExpired(now):
			s.removeLocked(int32(i))
			s.expirations++
			return
		case atomic.LoadUint32(&e.ref) != 0:
			atomic.StoreUint32(&e.ref, 0)
		default:
			s.removeLocked(int32(i))
			s.evictions++
			return
		}
	}
}

func (s *shard) removeLocked(i int32) {
	e := &s.entries[i]
	delete(s.slotByKey, e.key)
	s.bytes -= int64(len(e.key) + len(e.val))
	if e.expiresAt != 0 {
		s.expiring--
	}
	*e = entry{}
	s.free = append(s.free, i)
}

func (s *shard) sweepLocked(now int64) {
	if s.expiring > 0 {
		for i := range s.entries {
			if e := &s.entries[i]; e.key != "" && e.isExpired(now) {
				s.removeLocked(int32(i))
				s.expirations++
			}
		}
	}
	if len(s.slotByKey) == 0 && s.entries != nil {
		s.slotByKey = nil
		s.entries = nil
		s.free = nil
		s.hand = 0
	}
}
