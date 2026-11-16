# Memory Cache

*Package `memorycache` is a bounded, thread-safe in-memory byte cache with exact expiry and an atomic counter. It is the default backend of the cache module (`modules/cache`) and of the throttler guard.*

- [Memory Cache](#memory-cache)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`MemoryCacheOptions` Parameters](#memorycacheoptions-parameters)
    - [MaxEntries](#maxentries)
    - [MaxBytes](#maxbytes)
  - [Functions](#functions)
    - [NewMemoryCache](#newmemorycache)
  - [`MemoryCache` Methods](#memorycache-methods)
    - [Get](#get)
    - [Set](#set)
    - [SetNX](#setnx)
    - [Incr](#incr)
    - [Delete](#delete)
    - [Keys](#keys)
    - [TTL](#ttl)
  - [Errors](#errors)
  - [Expiry](#expiry)
  - [Bounds and eviction](#bounds-and-eviction)
  - [Idle cost and memory](#idle-cost-and-memory)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

## Key Features

- **Bounded:** a cache never holds more than `MaxEntries` entries or `MaxBytes` key and value bytes, whatever keys it is given. A full cache forgets old entries; a write never fails or blocks for lack of room.
- **Free while idle:** creating a cache starts no goroutine and no timer. A timer runs only while the cache holds entries that expire.
- **Exact expiry:** an entry is gone the instant its time-to-live has passed, on the monotonic clock, whether or not a cleanup has run.
- **Atomic counter:** `Incr` adds to an 8-byte counter in one step, so concurrent requests never lose a count.
- **Isolated values:** values are copied in and out; changing a slice you passed or received never changes the cache.

## Usage

Most apps use the cache through the cache module (`modules/cache`), which creates a `MemoryCache` for
you. Create one yourself when a component needs its own store:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/dangduoc08/ginject/memorycache"
)

func main() {
	ctx := context.Background()
	mc := memorycache.NewMemoryCache(nil)

	_ = mc.Set(ctx, "greeting", []byte("hello"), time.Minute)
	if val, ok := mc.Get(ctx, "greeting"); ok {
		fmt.Println(string(val))
	}

	for range 3 {
		visits, _ := mc.Incr(ctx, "visits", 1, time.Hour)
		fmt.Println("visits:", visits)
	}
}
```

Output:

```console
hello
visits: 1
visits: 2
visits: 3
```

## `MemoryCacheOptions` Parameters

Pass `nil` (or an empty struct) for the defaults, which suit a small server.

### MaxEntries

Type: `int`

Default: `65536`

Required: `false`

The most entries the cache holds. The cache is split into 64 shards that each hold
`MaxEntries / 64` entries, so the value is rounded down to a multiple of 64. Values from 1 to 63
become 64; zero or a negative value takes the default.

```go
memorycache.NewMemoryCache(&memorycache.MemoryCacheOptions{MaxEntries: 32768})
```

### MaxBytes

Type: `int64`

Default: `16 << 20` (16 MiB)

Required: `false`

The most key bytes plus value bytes the cache holds (`len(key) + len(val)` per entry). Each shard
holds `MaxBytes / 64` bytes, which is also the largest single entry: 256 KiB with the default. A
value below 64 KiB becomes 64 KiB; zero or a negative value takes the default.

```go
memorycache.NewMemoryCache(&memorycache.MemoryCacheOptions{MaxEntries: 32768, MaxBytes: 4 << 20})
```

## Functions

### NewMemoryCache

```go
func NewMemoryCache(opts *MemoryCacheOptions) *MemoryCache
```

Creates an empty cache. `NewMemoryCache(nil)` equals `NewMemoryCache(&MemoryCacheOptions{})`. The
cache needs no cleanup: there is no `Stop`, and a cache nobody references is garbage collected,
even while its sweep timer is pending.

#### Rules
- Allocates no shard storage and starts no goroutine or timer; storage is allocated by a shard's first write.
- Bounds follow the rules of [MaxEntries](#maxentries) and [MaxBytes](#maxbytes): for example 1000 entries become 960, 1 becomes 64, 1000 bytes become 64 KiB.

## `MemoryCache` Methods

Every method is safe to call from any number of goroutines. `ctx` is accepted to satisfy
`cache.Cache` and is not used.

### Get

```go
func (mc *MemoryCache) Get(ctx context.Context, key string) ([]byte, bool)
```

#### Parameters
- 1st parameter: `context.Context`
- 2nd parameter: `string`, the key

#### Returns
- 1st value: `[]byte`, a copy of the stored value
- 2nd value: `bool`, `true` when a live entry was found

#### Usage

```go
val, ok := mc.Get(ctx, "user:1")
if !ok {
	val = loadUser(1)
}
```

#### Rules
- Returns `(nil, false)` for a missing key, an expired entry and an empty key.
- A value stored as `nil` or empty reads back as an empty, non-nil slice.
- Changing the returned slice never changes the stored value, and a later write of the key never changes a slice already returned.
- Marks the entry as used, which protects it from the next eviction (see [Bounds and eviction](#bounds-and-eviction)).

### Set

```go
func (mc *MemoryCache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
```

#### Parameters
- 1st parameter: `context.Context`
- 2nd parameter: `string`, the key
- 3rd parameter: `[]byte`, the value; the cache keeps its own copy
- 4th parameter: `time.Duration`, the time-to-live; `0` means the entry never expires

#### Returns
- 1st value: `error`, `nil` or one of the [errors](#errors)

#### Usage

```go
if err := mc.Set(ctx, "session:abc", token, 30*time.Minute); err != nil {
	return err
}
```

#### Rules
- Stores the value and replaces the expiry: `Set` with `ttl` 0 removes an earlier expiry, and a new `ttl` restarts the countdown.
- Errors are checked in this order, and each writes nothing: an empty key returns `ErrEmptyKey`, a negative `ttl` returns `ErrNegativeTTL`, an entry with `len(key) + len(val)` above `MaxBytes / 64` returns `ErrEntryTooLarge` (also for an existing key, whose value stays as it was).
- On a full cache it returns `nil`: it evicts older entries of the same shard instead of failing.
- Changing `val` after the call never changes the stored value.

### SetNX

```go
func (mc *MemoryCache) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)
```

Writes the value only when the key has no live entry. Use it for locks and run-once markers.

#### Parameters
Same as [Set](#set).

#### Returns
- 1st value: `bool`, `true` when the value was written
- 2nd value: `error`, the same errors as `Set`

#### Usage

```go
ok, err := mc.SetNX(ctx, "job:nightly", []byte("1"), time.Hour)
if err == nil && !ok {
	return
}
```

#### Rules
- A live key returns `(false, nil)` and keeps its value and expiry.
- An expired key counts as missing: the new value is written.
- The check and the write are one atomic step: concurrent `SetNX` calls of one key return `true` exactly once.

### Incr

```go
func (mc *MemoryCache) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
```

Adds `delta` to a counter and returns the new value. A counter is an 8-byte, big-endian, two's
complement value, so `Get` and `Set` read and write the same counter.

#### Parameters
- 1st parameter: `context.Context`
- 2nd parameter: `string`, the key
- 3rd parameter: `int64`, the amount to add; zero and negative amounts work
- 4th parameter: `time.Duration`, the time-to-live of a counter that `Incr` creates

#### Returns
- 1st value: `int64`, the counter after the addition
- 2nd value: `error`, `nil` or one of the [errors](#errors)

#### Usage

```go
hits, err := mc.Incr(ctx, "rate:203.0.113.7", 1, time.Minute)
if err == nil && hits > 100 {
	return errTooManyRequests
}
```

#### Rules
- No live entry (missing or expired): stores `delta` with `ttl` and returns `delta`.
- A live counter: adds `delta`, returns the sum and **keeps its expiry**; `ttl` is ignored. A counter without expiry stays without one.
- Overflow wraps like Go's `int64`: `MaxInt64 + 1` is `MinInt64`.
- A live entry whose value is not 8 bytes returns `(0, ErrNotCounter)` and changes nothing.
- Errors as `Set`: empty key, negative `ttl`, and a key longer than `MaxBytes / 64 - 8` bytes.
- Atomic: N concurrent `Incr(key, 1, ttl)` calls return each of `1` to `N` exactly once.

### Delete

```go
func (mc *MemoryCache) Delete(ctx context.Context, key string) error
```

#### Returns
- 1st value: `error`, `ErrEmptyKey` for an empty key, else `nil`

#### Usage

```go
_ = mc.Delete(ctx, "session:abc")
```

#### Rules
- Deleting a missing key is not an error.

### Keys

```go
func (mc *MemoryCache) Keys(ctx context.Context) []string
```

Returns the live keys, in no particular order. It visits every entry, so use it in tools and tests,
not on a request path.

#### Usage

```go
fmt.Println(len(mc.Keys(ctx)), "entries")
```

#### Rules
- Expired entries are omitted; an empty cache returns an empty, non-nil slice.

### TTL

```go
func (mc *MemoryCache) TTL(ctx context.Context, key string) (time.Duration, bool)
```

#### Returns
- `(remaining, true)` with `0 < remaining <= ttl` for an entry that expires
- `(0, true)` for an entry that never expires
- `(0, false)` for a missing key, an expired entry and an empty key

#### Usage

```go
if left, ok := mc.TTL(ctx, "session:abc"); ok && left > 0 {
	fmt.Println("expires in", left)
}
```

## Errors

| Error | Returned by | When |
|---|---|---|
| `ErrEmptyKey` | `Set`, `SetNX`, `Incr`, `Delete` | The key is `""`. |
| `ErrNegativeTTL` | `Set`, `SetNX`, `Incr` | `ttl` is below zero. Nothing is written. |
| `ErrEntryTooLarge` | `Set`, `SetNX`, `Incr` | `len(key) + len(val)` is above `MaxBytes / 64`. |
| `ErrNotCounter` | `Incr` | The key holds a live value that is not 8 bytes. |

Compare with `errors.Is`. The cache module exports the same values as `cache.ErrEmptyKey` and so on.

## Expiry

- An entry written at time `t` with `ttl > 0` is live while less than `ttl` has passed, and expired from the instant `ttl` has passed.
- `ttl == 0` never expires. A `ttl` so large that the deadline would overflow never expires either.
- Time is the monotonic clock: changing the system clock moves no deadline.
- An expired entry is absent for every method at once, whether or not the sweep has removed it yet.
- The sweep removes expired entries in the background. It is one timer per cache, armed only while
  the cache holds an entry that expires, firing every 5 seconds. After a pass that leaves no
  expiring entry, the timer is not armed again; a later entry with a `ttl` arms it again. A shard
  left with no entry releases its storage.

## Bounds and eviction

Keys are spread over 64 shards by a hash with a per-process random seed, so a client cannot aim its
keys at one shard. Each shard holds at most `MaxEntries / 64` entries and `MaxBytes / 64` bytes.

When a write would pass a bound, the shard first removes entries until it fits, walking its entries
in a fixed circular order (the CLOCK policy):

- an expired entry is removed first, without counting as an eviction;
- a live entry that was read (`Get`) or rewritten (`Set`, `SetNX`, `Incr` of an existing key) since the walk last passed it is kept, and its mark is cleared;
- a live entry without that mark is evicted.

What this guarantees:

- A key that is read or written at least once between two passes of the walk is never evicted.
- New entries that are never read are the first victims.
- Rewriting an existing key with a value that is not larger evicts nothing.
- The key being written is never its own victim: growing one value evicts others of its shard.

No policy can protect a key when a whole shard's worth of new keys arrives between two of its uses.

## Idle cost and memory

- An empty cache: no goroutine, no timer, no wakeup; 64 shard headers (about 14 KB of heap, measured).
- With only entries that never expire: the same, plus the entries.
- With entries that expire: one timer that wakes every 5 seconds while they exist.
- Each entry costs its key and value bytes plus about 120 bytes of bookkeeping (158 bytes of heap measured for a 30-byte key and an 8-byte value). The keys are copied, so a key that is a slice of a large string never keeps that string alive.

## Performance notes

- `Get`, `TTL`, `Keys` take a shard's read lock; writes take its write lock. No operation holds two shard locks.
- `Get` copies the value while it holds the read lock, so a write can reuse the stored buffer in place. A `Set` of an existing key with a value of about the same size, and every `Incr` of an existing counter, allocate nothing.
- Each shard is padded to its own cache lines, and its hit and miss counters are atomics in that shard, so threads that use different shards share no cache line.

## Benchmarks

Captured on darwin/amd64 (Intel Core i7-9750H, 12 threads, Go 1.27.4) with
`go test -run='^$' -bench . -benchmem -count 5 -cpu 1,2,12 ./memorycache/`, medians. The machine
was running other work at the same time, so treat differences under about 15 % as noise. Numbers
are machine-dependent and were captured when this page was written (2026-11-17).

| Benchmark | ns/op at 1 / 2 / 12 threads | allocs/op |
|---|---|---|
| `Get_Hit` (one key, every thread) | 128 / 113 / 76 | 1 (the returned copy) |
| `GetParallel` (4,096 keys) | 165 / 91 / 32 | 1 |
| `Get_Miss` | 77 / 71 / 64 | 0 |
| `TTL_Hit` | 82 / 64 / 64 | 0 |
| `GetSetParallel` (7 reads : 1 write, 4,096 keys) | 142 / 84 / 52 | 0 |
| `Set_Existing` (one key, every thread) | 120 / 126 / 249 | 0 |
| `Set_NewAtBound` (a new key into a full cache, evicts one) | 409 / 381 / 355 | 2 (key and value copies) |
| `Incr` (4,096 counters) | 150 / 90 / 33 | 0 |
| `Incr_OneKey` (one counter, every thread) | 113 / 123 / 195 | 0 |
| `HashKey` | 10 | 0 |
| `HeapAtBound` | 158 B of heap per entry (30-byte key, 8-byte value) | |

For comparison, counting with `Get` then `Set` on the previous, unbounded cache measured
452 / 245 / 76 ns and 2 allocations per count (and could lose counts); `Incr` is about three times
faster and allocates nothing. Through the default cache, 5.2 million distinct 30-byte keys with
8-byte values leave 65,536 entries and 9.8 MB of heap (the unbounded cache grew to about 680 MB).
